"""Prepare authenticated public scanner suppliers; never grant admission.

Only --execute on the exact approved hosted dispatch downloads or compiles.
No candidate source, install hooks, scanner execution, private caches or VCS.
The receipt is provisional and additionally requires the actual step success.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import selectors
import signal
import stat
import subprocess
import sys
import time
import zipfile

SCANNER = ('github.com/google/osv-scanner/v2', 'v2.6.0')
ORIGIN = 'e840a6e8adb14b7777c78e26cfbf6e2abc1d1fc6'
SCANNER_SUM = 'h1:hqtNLANWaKcqOC6znJX8J/23PtzV82RX23oMS9/ab4I='
SCANNER_MOD_SUM = 'h1:3XUKJWYB7D2fWvFDMTZg9OBrlCEf/WBGZVc4IFjTljs='
VULN = ('golang.org/x/vuln', 'v1.6.0')
XNET = ('golang.org/x/net', 'v0.60.0')
EXTRA_SUMS = {
    VULN: ('h1:FeMO9Rm/HwyduOztbvKcOw+zvDEPr4I4aQNSfevFcKY=',
           'h1:bWlG2493/sjR7ksvicBgMrznH3eYQEyK8ifUYBrqUbg='),
    XNET: ('h1:79p50tfZlm0J9YfoDsSi639qSXNGVwEzOPLCxM2FsYU=',
           'h1:2DA/G1UfVbCpQPeWTmMPGY7Cs2PkBkwu743bVX5PIVg=')}
UPSTREAM_SHA = {'go.mod': '47db8d4f9e4b49922097899d76692a37a84e29143f439cc47fdf925b4feab615',
                'go.sum': 'd8310b422c284f42751042d5d66ca661981f5c855dd2e19e1173090f7fcc2989'}
PATCHED_SHA = {'go.mod': '36ab7af2571e78311029b84b4577c917de4cb4610055561694fb24b9f96b7569',
               'go.sum': 'd89ac8ea219e18828f9f01346111e5ab36c71b3556c1739d158e97e5eaa566e0'}
IDENTITY = {'GITHUB_REPOSITORY': 'GoCodeAlone/workflow',
            'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_JOB': 'osv-production-proof',
            'GITHUB_REF': 'refs/heads/prep/osv-prerequisites-20261011-r1',
            'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'X64'}
TOTAL_SECONDS = 1800
DOWNLOAD_SECONDS = 180
BUILD_SECONDS = 600
OUTPUT_BYTES = 4 * 1024 * 1024
ZIP_BYTES = 500 * 1024 * 1024
FILE_BYTES = 64 * 1024 * 1024
SOURCE_BYTES = 500 * 1024 * 1024
SOURCE_FILES = 20000
AGGREGATE_SOURCE_BYTES = 4 * 1024 * 1024 * 1024
H1 = re.compile(r'h1:[A-Za-z0-9+/]{43}=\Z')
MODULE = re.compile(r'[A-Za-z0-9][A-Za-z0-9._~!/+\-]{0,250}\Z')
VERSION = re.compile(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.\-]+)?(?:\+incompatible)?\Z')


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


class Budget:
    def __init__(self, seconds=TOTAL_SECONDS):
        self.end = time.monotonic() + seconds

    def left(self):
        left = self.end - time.monotonic()
        require(left > 0, 'supplier shared deadline exceeded')
        return left


def canonical(path, directory=False):
    path = Path(path)
    require(path.is_absolute() and path == path.resolve(strict=True), 'noncanonical supplier path')
    info = path.lstat()
    require((stat.S_ISDIR if directory else stat.S_ISREG)(info.st_mode) and
            not info.st_mode & 0o6022, 'unsafe supplier path type or mode')
    return path


def read_file(path, maximum, budget):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 <= info.st_size <= maximum,
            'supplier file type/size refused')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        require((opened.st_dev, opened.st_ino) == (info.st_dev, info.st_ino), 'supplier file replaced')
        chunks = []; count = 0
        while True:
            budget.left()
            chunk = stream.read(65536)
            if not chunk:
                break
            count += len(chunk)
            require(count <= maximum, 'supplier file grew past limit')
            chunks.append(chunk)
        after = os.fstat(stream.fileno())
    require((info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
            (after.st_size, after.st_mtime_ns, after.st_ctime_ns) and count == info.st_size,
            'supplier file changed while reading')
    return b''.join(chunks)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def hash1(files):
    # Exact Go x/mod v0.41.0 sumdb/dirhash.Hash1 format; no ZIP metadata normalization.
    summary = ''.join(f'{sha(raw)}  {name}\n' for name, raw in sorted(files.items()))
    return 'h1:' + base64.b64encode(hashlib.sha256(summary.encode()).digest()).decode()


def write_fresh(path, raw, mode=0o600):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), 'wb') as f:
        f.write(raw); f.flush(); os.fsync(f.fileno()); os.fchmod(f.fileno(), mode)


def run(argv, cwd, env, budget, seconds, logdir, label, commands):
    """Closed streams/environment and one owned unreaped POSIX child group."""
    require(re.fullmatch(r'[a-z0-9-]{1,80}', label), 'invalid supplier phase label')
    start = time.monotonic(); limit = min(seconds, budget.left())
    command = {'phase': label, 'argv': list(argv), 'elapsed_seconds': None, 'exit': None,
               'stdout_bytes': 0, 'stderr_bytes': 0, 'complete': False}
    commands.append(command)
    raw = [bytearray(), bytearray()]
    process = subprocess.Popen(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               close_fds=True, start_new_session=True)
    failure = None
    try:
        with selectors.DefaultSelector() as ready:
            for index, stream in enumerate((process.stdout, process.stderr)):
                os.set_blocking(stream.fileno(), False)
                ready.register(stream, selectors.EVENT_READ, index)
            while ready.get_map():
                remaining = min(limit - (time.monotonic() - start), budget.left())
                require(remaining > 0, 'supplier child deadline exceeded')
                for key, _ in ready.select(min(remaining, 0.1)):
                    chunk = os.read(key.fd, 65536)
                    if not chunk:
                        ready.unregister(key.fileobj)
                        continue
                    available = OUTPUT_BYTES - len(raw[key.data])
                    raw[key.data].extend(chunk[:available])
                    require(len(chunk) <= available, 'supplier child output exceeded limit')
            # Observe exit without reaping: the leader PID cannot be reused before group retirement.
            while os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is None:
                require(time.monotonic() - start < limit, 'supplier child deadline exceeded')
                budget.left(); time.sleep(0.01)
    except Exception as exc:
        failure = exc
    finally:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        code = process.wait(timeout=5)
        process.stdout.close(); process.stderr.close()
        command.update(exit=code, elapsed_seconds=round(time.monotonic() - start, 6),
                       stdout_bytes=len(raw[0]), stderr_bytes=len(raw[1]))
        for kind, data in zip(('stdout', 'stderr'), raw):
            write_fresh(logdir / f'{label}.{kind}', bytes(data))
            command[kind + '_sha256'] = sha(data)
    if failure is not None:
        raise failure
    budget.left()
    require(code == 0, f'supplier native phase failed: {label} (exit {code})')
    command['complete'] = True
    return bytes(raw[0])


def environment(go, home, caches, temporary, config, online=False, binaries=None):
    result = {'PATH': f'{go.parent}:/usr/bin:/bin', 'HOME': str(home), 'LANG': 'C.UTF-8',
              'LC_ALL': 'C.UTF-8', 'TZ': 'UTC', 'TMPDIR': str(temporary),
              'XDG_CONFIG_HOME': str(config),
              'GOPATH': str(caches['GOPATH']), 'GOMODCACHE': str(caches['GOMODCACHE']),
              'GOCACHE': str(caches['GOCACHE']), 'GOENV': 'off', 'GOWORK': 'off',
              'GOTOOLCHAIN': 'local', 'GOAUTH': 'off', 'GOVCS': '*:off',
              'GOPRIVATE': '', 'GONOPROXY': 'none', 'GONOSUMDB': 'none', 'GOINSECURE': '',
              'GOPROXY': 'https://proxy.golang.org' if online else 'off',
              'GOSUMDB': 'sum.golang.org' if online else 'off',
              'GOFLAGS': '-mod=readonly', 'CGO_ENABLED': '0', 'GOOS': 'linux',
              'GOARCH': 'amd64', 'GOAMD64': 'v1', 'GOMAXPROCS': '2',
              'GOMEMLIMIT': '768MiB'}
    if binaries is not None:
        result['GOBIN'] = str(binaries)
    return result


def json_records(raw):
    decoder = json.JSONDecoder(object_pairs_hook=unique_pairs)
    text = raw.decode('utf-8'); records = []
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        require(isinstance(value, dict), 'supplier JSON object required')
        records.append(value); text = text[end:]
        require(len(records) <= 256, 'too many supplier download records')
    return records


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate supplier JSON field')
        result[key] = value
    return result


def escape(value):
    return ''.join('!' + c.lower() if 'A' <= c <= 'Z' else c for c in value)


def archive(path, module, version, expected_sum, budget, destination=None):
    require(MODULE.fullmatch(module) and VERSION.fullmatch(version), 'invalid fixed module identity')
    raw = read_file(path, ZIP_BYTES, budget)
    prefix = f'{module}@{version}/'; files = {}; hashes = {}; total = 0
    # Open the already read, immutable bounded bytes, never ZipFile.extract/extractall.
    import io
    with zipfile.ZipFile(io.BytesIO(raw)) as zipped:
        members = zipped.infolist()
        require(0 < len(members) <= SOURCE_FILES, 'supplier ZIP member count refused')
        folded = set()
        for info in members:
            budget.left()
            name = info.filename
            require(name.startswith(prefix), 'supplier ZIP module prefix mismatch')
            rel = name[len(prefix):]; parts = PurePosixPath(rel).parts
            mode = info.external_attr >> 16
            require(rel and rel == '/'.join(parts) and not any(p in {'.', '..', '.git', '.hg', '.svn'} for p in parts)
                    and '\\' not in rel and not any(ord(c) < 32 or ord(c) == 127 for c in rel)
                    and info.orig_filename == name and len(rel.encode()) <= 512 and len(parts) <= 32
                    and not info.is_dir() and not info.flag_bits & 1
                    and stat.S_IFMT(mode) in {0, stat.S_IFREG} and not mode & 0o6000,
                    'supplier ZIP unsafe member')
            require(name not in files and rel.casefold() not in folded, 'supplier ZIP duplicate/case collision')
            folded.add(rel.casefold())
            require(0 <= info.file_size <= FILE_BYTES, 'supplier ZIP member size refused')
            total += info.file_size
            require(total <= SOURCE_BYTES, 'supplier ZIP expansion exceeded limit')
            with zipped.open(info) as stream:
                data = stream.read(FILE_BYTES + 1)
            require(len(data) == info.file_size and len(data) <= FILE_BYTES, 'supplier ZIP size mismatch')
            files[name] = sha(data)
            hashes[rel] = {'sha256': sha(data), 'bytes': len(data)}
        summary = ''.join(f'{digest}  {name}\n' for name, digest in sorted(files.items()))
        computed = 'h1:' + base64.b64encode(hashlib.sha256(summary.encode()).digest()).decode()
        require(computed == expected_sum, 'supplier ZIP h1 mismatch')
        require(prefix + 'go.mod' in files, 'supplier source go.mod missing')
        go_mod = zipped.read(prefix + 'go.mod')
        if destination is not None:
            destination.mkdir(mode=0o700)
            for name in sorted(files):
                target = destination / name[len(prefix):]
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                write_fresh(target, zipped.read(name))
    return {'module': module, 'version': version, 'h1': expected_sum,
            'zip_sha256': sha(raw), 'zip_bytes': len(raw), 'source_bytes': total,
            'files': hashes, 'go_mod': go_mod}


def verify_download(row, identity, sums, cache, budget, destination=None):
    module, version = identity
    require(row.get('Path') == module and row.get('Version') == version and not row.get('Error'),
            'supplier download identity/error refused')
    expected_zip, expected_mod = sums
    require(row.get('Sum') == expected_zip and row.get('GoModSum') == expected_mod,
            'supplier download checksum identity mismatch')
    prefix = cache / 'cache/download' / escape(module) / '@v' / escape(version)
    paths = {field: Path(str(prefix) + suffix) for field, suffix in
             (('Zip', '.zip'), ('GoMod', '.mod'), ('Info', '.info'))}
    for field, path in paths.items():
        require(row.get(field) == str(path) and path == path.resolve(strict=True),
                'supplier cache path escaped admitted cache')
    require(row.get('Dir') == str(cache / (escape(module) + '@' + escape(version))),
            'supplier extracted cache path mismatch')
    mod = read_file(paths['GoMod'], 1024 * 1024, budget)
    require(hash1({'go.mod': mod}) == expected_mod, 'supplier go.mod h1 mismatch')
    result = archive(paths['Zip'], module, version, expected_zip, budget, destination)
    require(result['go_mod'] == mod, 'supplier ZIP and proxy go.mod disagree')
    info = json.loads(read_file(paths['Info'], 65536, budget), object_pairs_hook=unique_pairs)
    require(info.get('Version') == version, 'supplier info version mismatch')
    if identity == SCANNER:
        origin = row.get('Origin') or info.get('Origin')
        require(isinstance(origin, dict) and origin.get('VCS') == 'git' and
                origin.get('URL') == 'https://github.com/google/osv-scanner' and
                origin.get('Hash') == ORIGIN and origin.get('Subdir', '') == '',
                'scanner exact authenticated origin metadata missing/mismatched')
        result['origin_commit'] = ORIGIN
    result['go_mod_sha256'] = sha(mod)
    result['go_mod_h1'] = expected_mod
    return result


def requirements(raw):
    result = []; block = False
    for line in raw.decode('utf-8').splitlines():
        line = line.split('//', 1)[0].strip()
        if not line:
            continue
        if line == 'require (':
            require(not block, 'nested supplier requirement block'); block = True; continue
        if line == ')':
            require(block, 'unexpected supplier directive block'); block = False; continue
        if block or line.startswith('require '):
            parts = line.split() if block else line.split()[1:]
            require(len(parts) == 2 and MODULE.fullmatch(parts[0]) and VERSION.fullmatch(parts[1]),
                    'unsupported supplier requirement')
            result.append(tuple(parts))
        else:
            require(re.fullmatch(r'(?:module [A-Za-z0-9._/+\-]+|go [0-9.]+)', line),
                    'supplier replace/toolchain/unsupported directive refused')
    require(not block and 0 < len(result) <= 256 and len(result) == len(set(result)),
            'supplier requirement list incomplete/ambiguous')
    return result


def checksum_rows(raw):
    result = {}
    for line in raw.decode().splitlines():
        fields = line.split()
        require(len(fields) == 3 and MODULE.fullmatch(fields[0]) and H1.fullmatch(fields[2]),
                'malformed authenticated supplier checksum row')
        key = tuple(fields[:2])
        require(key not in result, 'duplicate authenticated supplier checksum row')
        result[key] = fields[2]
    return result


def expected_sums(identity, rows):
    module, version = identity
    require(identity in rows and (module, version + '/go.mod') in rows,
            'supplier requirement lacks authenticated full/go.mod sums')
    return rows[identity], rows[module, version + '/go.mod']


def patch_source(source, budget):
    for name, expected in UPSTREAM_SHA.items():
        require(sha(read_file(source / name, 1024 * 1024, budget)) == expected,
                'reviewed scanner upstream source manifest changed')
    mod = (source / 'go.mod').read_bytes()
    replacements = [(b'\ngo 1.27.0\n', b'\ngo 1.27.2\n'),
                    (b'\tgolang.org/x/net v0.58.0 // indirect\n',
                     b'\tgolang.org/x/net v0.60.0 // indirect\n')]
    for old, new in replacements:
        require(mod.count(old) == 1, 'scanner patch context missing/duplicated')
        mod = mod.replace(old, new)
    sums = (source / 'go.sum').read_bytes()
    anchor = b'golang.org/x/net v0.58.0/go.mod h1:YwCddHnFlT7eLQqVprV19OnhLGtc5xOKgE0RyqgfWAU=\n'
    additions = ('golang.org/x/net v0.60.0 ' + EXTRA_SUMS[XNET][0] + '\n' +
                 'golang.org/x/net v0.60.0/go.mod ' + EXTRA_SUMS[XNET][1] + '\n').encode()
    require(sums.count(anchor) == 1 and b'golang.org/x/net v0.60.0 ' not in sums,
            'scanner checksum patch context missing/duplicated')
    sums = sums.replace(anchor, anchor + additions)
    require(sha(mod) == PATCHED_SHA['go.mod'] and sha(sums) == PATCHED_SHA['go.sum'],
            'scanner patch differs from reviewed exact manifests')
    for name, data in (('go.mod', mod), ('go.sum', sums)):
        with os.fdopen(os.open(source / name, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW), 'wb') as f:
            f.write(data); f.flush(); os.fsync(f.fileno())


def inventory(source, budget):
    result = {}; total = 0
    for parent, dirs, files in os.walk(source, followlinks=False):
        budget.left()
        for name in dirs:
            require(stat.S_ISDIR((Path(parent) / name).lstat().st_mode), 'supplier source symlink directory')
        for name in files:
            path = Path(parent) / name
            require(len(result) < SOURCE_FILES, 'supplier inventory count exceeded')
            data = read_file(path, FILE_BYTES, budget)
            total += len(data); require(total <= SOURCE_BYTES, 'supplier inventory bytes exceeded')
            result[path.relative_to(source).as_posix()] = {'sha256': sha(data), 'bytes': len(data)}
    return result


def buildinfo(raw, name):
    text = raw.decode('utf-8'); lines = text.splitlines()
    require(lines and lines[0].endswith(': go1.27.2'), 'supplier binary compiler mismatch')
    entries = [line.strip().split('\t') for line in lines[1:]]
    settings = {}; paths = []
    for entry in entries:
        if entry[0] == 'build':
            key, separator, value = entry[1].partition('=')
            require(separator and key not in settings, 'ambiguous supplier binary build settings')
            settings[key] = value
        elif entry[0] == 'path':
            paths.append(entry[1])
    expected_path = (VULN[0] + '/cmd/govulncheck' if name == 'govulncheck' else
                     SCANNER[0] + '/cmd/' + name)
    require(paths == [expected_path], 'supplier binary command path mismatch')
    for key, value in {'-trimpath': 'true', 'CGO_ENABLED': '0', 'GOOS': 'linux',
                       'GOARCH': 'amd64', 'GOAMD64': 'v1'}.items():
        require(settings.get(key) == value, 'supplier binary build setting mismatch')
    require(not any(key.startswith('vcs') for key in settings), 'ambient VCS stamping refused')
    module_entries = [entry for entry in entries if entry[0] == 'mod']
    if name == 'govulncheck':
        require(module_entries == [['mod', VULN[0], VULN[1], EXTRA_SUMS[VULN][0]]],
                'govulncheck exact version/checksum mismatch')
    else:
        require(module_entries == [['mod', SCANNER[0], '(devel)']], 'scanner rebuilt module mismatch')
        require(['dep', XNET[0], XNET[1], EXTRA_SUMS[XNET][0]] in entries,
                'scanner patched x/net dependency missing')
    return {'command_path': expected_path, 'compiler': 'go1.27.2', 'settings': settings,
            'buildinfo_sha256': sha(raw)}


def hosted_identity(budget):
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64' and
            os.getuid() != 0 and os.geteuid() == os.getuid(), 'nonroot Linux amd64 supplier required')
    require({key: os.environ.get(key, '') for key in IDENTITY} == IDENTITY,
            'supplier exact hosted dispatch identity mismatch')
    result = {'GITHUB_REPOSITORY': os.environ['GITHUB_REPOSITORY'], 'GITHUB_JOB': os.environ['GITHUB_JOB']}
    for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'):
        value = os.environ.get(key, '')
        require(re.fullmatch(r'[1-9][0-9]{0,19}', value), 'supplier run identity malformed')
        result[key] = value
    require(re.fullmatch(r'[0-9a-f]{40}', os.environ.get('GITHUB_SHA', '')), 'supplier checkout identity malformed')
    result['GITHUB_SHA'] = os.environ['GITHUB_SHA']
    release = read_file(canonical(Path('/etc/os-release').resolve(strict=True)), 16384, budget).decode()
    require(re.search(r'^ID=ubuntu$', release, re.M) and
            re.search(r'^VERSION_ID="24\.04"$', release, re.M), 'fixed Ubuntu24.04 supplier required')
    return result


def runner_home():
    return canonical('/home/runner', True)


def supplier(args):
    budget = Budget(); commands = []
    output = Path(args.output)
    temporary = canonical(os.environ['RUNNER_TEMP'], True)
    require(output.is_absolute() and output.parent == temporary and
            re.fullmatch(r'osv-supplier-[A-Za-z0-9]{10}', output.name), 'fixed supplier output path required')
    info = output.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
            stat.S_IMODE(info.st_mode) == 0o700 and output == output.resolve(strict=True) and
            not any(output.iterdir()), 'fresh owned private supplier output required')
    receipt = {'schema': 'workflow.offline-scanner.supplier-delivery.v1', 'accepted': False,
               'supplied': False, 'complete': False, 'actual_step_success_required': True,
               'installation_admitted': False, 'commands': commands}
    code = 2; stage = 'hosted-admission'
    try:
        receipt['identity'] = hosted_identity(budget)
        receipt['dispatch_contract'] = IDENTITY
        receipt['supplier_script_sha256'] = sha(read_file(Path(__file__).resolve(), 1024 * 1024, budget))
        go = canonical(args.go)
        require(go.name == 'go' and os.access(go, os.X_OK), 'installed Go executable required')
        caches = {key: canonical(value, True) for key, value in
                  (('GOPATH', args.gopath), ('GOMODCACHE', args.gomodcache), ('GOCACHE', args.gocache))}
        require(caches['GOMODCACHE'] == caches['GOPATH'] / 'pkg/mod' and
                len(set(caches.values())) == 3 and not any(output in p.parents for p in caches.values()),
                'existing standard allocated Go caches required')
        home = runner_home()
        require(caches['GOPATH'] == home / 'go' and caches['GOCACHE'] == home / '.cache/go-build',
                'only existing standard hosted Go cache allocations are admitted')
        receipt['cache_paths'] = {key: str(value) for key, value in caches.items()}
        logs = output / 'logs'; logs.mkdir(mode=0o700)
        work = output / 'work'; work.mkdir(mode=0o700)
        binaries = output / 'binaries'; binaries.mkdir(mode=0o700)
        config = work / 'config'
        telemetry = config / 'go/telemetry/mode'
        telemetry.parent.mkdir(mode=0o700, parents=True)
        # Go1.27.2 reads this mode through os.UserConfigDir; GOTELEMETRY is only a readback.
        write_fresh(telemetry, b'off\n')
        env = environment(go, home, caches, temporary, config)
        version = run([str(go), 'version'], work, env, budget, 10, logs, 'go-version', commands)
        require(version == b'go version go1.27.2 linux/amd64\n', 'installed Go1.27.2 required; no auto toolchain')
        goenv = json.loads(run([str(go), 'env', '-json', 'GOVERSION', 'GOROOT', 'GOPATH', 'GOMODCACHE', 'GOCACHE', 'GOTELEMETRY'],
                              work, env, budget, 10, logs, 'go-environment', commands), object_pairs_hook=unique_pairs)
        require(goenv.get('GOVERSION') == 'go1.27.2' and goenv.get('GOTELEMETRY') == 'off' and
                all(goenv.get(k) == str(v) for k, v in caches.items()),
                'actual toolchain/cache environment differs')
        goroot = canonical(goenv['GOROOT'], True)
        require(go == goroot / 'bin/go', 'Go executable/GOROOT mismatch')
        receipt['toolchain'] = {'version': 'go1.27.2', 'goroot': str(goroot),
            'tool_sha256': {name: sha(read_file(goroot / rel, 128 * 1024 * 1024, budget)) for name, rel in
                           (('go', 'bin/go'), ('compile', 'pkg/tool/linux_amd64/compile'),
                            ('link', 'pkg/tool/linux_amd64/link'), ('asm', 'pkg/tool/linux_amd64/asm'))}}
        stage = 'authenticated-scanner-download'
        online = environment(go, home, caches, temporary, config, online=True)
        source = work / 'scanner-source'
        rows = json_records(run([str(go), 'mod', 'download', '-json', '@'.join(SCANNER)], work, online,
                                budget, DOWNLOAD_SECONDS, logs, 'download-scanner', commands))
        require(len(rows) == 1, 'scanner download record count mismatch')
        scanner = verify_download(rows[0], SCANNER, (SCANNER_SUM, SCANNER_MOD_SUM),
                                  caches['GOMODCACHE'], budget, source)
        original = scanner.pop('files'); scanner.pop('go_mod')
        require(inventory(source, budget) == original, 'scanner extraction conservation failed')
        patch_source(source, budget)
        expected = dict(original)
        for name in PATCHED_SHA:
            expected[name] = {'sha256': PATCHED_SHA[name], 'bytes': (source / name).stat().st_size}
        require(inventory(source, budget) == expected, 'scanner patch changed unapproved source')
        checksum = checksum_rows(read_file(source / 'go.sum', 1024 * 1024, budget))
        dependencies = requirements(read_file(source / 'go.mod', 1024 * 1024, budget))
        require(len(dependencies) == 206 and VULN in dependencies and XNET in dependencies,
                'reviewed supplier dependency graph changed')
        records = []; total = scanner['source_bytes']
        stage = 'authenticated-public-supplier-dependencies'
        for batch_index, offset in enumerate(range(0, len(dependencies), 16)):
            batch = dependencies[offset:offset+16]
            downloaded = json_records(run([str(go), 'mod', 'download', '-json'] + ['@'.join(i) for i in batch],
                work, online, budget, DOWNLOAD_SECONDS, logs, f'download-dependencies-{batch_index}', commands))
            mapped = {(r.get('Path'), r.get('Version')): r for r in downloaded}
            require(len(mapped) == len(downloaded) and set(mapped) == set(batch), 'supplier batch identities differ')
            for identity in batch:
                record = verify_download(mapped[identity], identity, expected_sums(identity, checksum),
                                         caches['GOMODCACHE'], budget)
                total += record['source_bytes']
                require(total <= AGGREGATE_SOURCE_BYTES, 'aggregate supplier source bytes exceeded')
                record.pop('files'); record.pop('go_mod'); records.append(record)
        receipt['scanner_source'] = scanner
        receipt['patch'] = {'changed': ['go.mod', 'go.sum'], 'extra': [], 'missing': [],
                            'upstream_sha256': UPSTREAM_SHA, 'patched_sha256': PATCHED_SHA}
        receipt['dependencies'] = records
        stage = 'offline-cache-verification'
        run([str(go), 'mod', 'verify'], source, env, budget, 180, logs, 'cache-verify-before', commands)
        stage = 'serial-offline-supplier-builds'
        supplied = {}
        for name in ('osv-scanner', 'osv-reporter', 'govulncheck'):
            if name == 'govulncheck':
                argv = [str(go), 'install', '-trimpath', '-buildvcs=false', '-p=2', VULN[0] + '/cmd/govulncheck@' + VULN[1]]
                build_env = environment(go, home, caches, temporary, config, binaries=binaries)
                cwd = work
            else:
                argv = [str(go), 'build', '-mod=readonly', '-trimpath', '-buildvcs=false', '-p=2',
                        '-o', str(binaries / name), './cmd/' + name]
                build_env = env; cwd = source
            run(argv, cwd, build_env, budget, BUILD_SECONDS, logs, 'build-' + name, commands)
            raw = read_file(binaries / name, 256 * 1024 * 1024, budget)
            require(len(raw) >= 20 and raw[:6] == b'\x7fELF\x02\x01' and raw[18:20] == b'\x3e\x00',
                    'supplier build is not Linux amd64 ELF')
            metadata = run([str(go), 'version', '-m', str(binaries / name)], work, env, budget, 10,
                           logs, 'buildinfo-' + name, commands)
            supplied[name] = {'sha256': sha(raw), 'bytes': len(raw), **buildinfo(metadata, name)}
        stage = 'source-and-cache-closeout'
        run([str(go), 'mod', 'verify'], source, env, budget, 180, logs, 'cache-verify-after', commands)
        require(inventory(source, budget) == expected, 'supplier build modified authenticated source')
        require(read_file(telemetry, 16, budget) == b'off\n', 'supplier telemetry mode changed')
        receipt['ephemeral_telemetry_mode'] = {'path': str(telemetry), 'sha256': sha(b'off\n'), 'actual_mode': 'off'}
        for name, pin in supplied.items():
            require(sha(read_file(binaries / name, 256 * 1024 * 1024, budget)) == pin['sha256'],
                    'supplier binary changed at closeout')
        for name, rel in (('go', 'bin/go'), ('compile', 'pkg/tool/linux_amd64/compile'),
                          ('link', 'pkg/tool/linux_amd64/link'), ('asm', 'pkg/tool/linux_amd64/asm')):
            require(sha(read_file(goroot / rel, 128 * 1024 * 1024, budget)) ==
                    receipt['toolchain']['tool_sha256'][name], 'installed toolchain changed during supplier build')
        receipt['binaries'] = supplied
        receipt['binary_pins'] = {name: pin['sha256'] for name, pin in supplied.items()}
        receipt['elapsed_seconds'] = round(TOTAL_SECONDS - budget.left(), 6)
        receipt['supplied'] = True
        receipt['complete'] = True
        stage = 'provisional-receipt'
        pending = output / '.supplier-delivery-proof.pending'
        write_fresh(pending,
                    (json.dumps(receipt, indent=2, sort_keys=True) + '\n').encode())
        budget.left()
        os.rename(pending, output / 'supplier-delivery-proof.json')
        budget.left()
        code = 0
    except Exception as exc:
        receipt.update(supplied=False, complete=False, failed_stage=stage, error=str(exc)[:1000])
        positive = output / 'supplier-delivery-proof.json'
        # A failed final fsync/deadline must not retain a usable positive receipt.
        if positive.exists():
            positive.unlink()
        if not positive.exists():
            write_fresh(output / 'supplier-delivery-proof.json',
                        (json.dumps(receipt, indent=2, sort_keys=True) + '\n').encode())
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute', action='store_true', help='explicitly perform approved supplier phase')
    for flag in ('output', 'go', 'gopath', 'gomodcache', 'gocache'):
        parser.add_argument('--' + flag, required=True)
    args = parser.parse_args()
    require(args.execute, 'supplier execution requires explicit --execute')
    return supplier(args)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as exc:
        print('supplier refused: ' + str(exc)[:1000], file=sys.stderr)
        sys.exit(2)
