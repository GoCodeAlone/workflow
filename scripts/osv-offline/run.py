"""Local hosted replacement candidate; not installed or execution-reviewed.

Only the base-owned bootstrap invokes public phases. All admission fields remain
HOLD until actual Linux/supplier/first-install evidence is reviewed and bound.
No application tests, binaries or npm scripts are executed by this manager.
"""
from datetime import datetime, timezone
import errno
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import resource
import secrets
import selectors
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[1]
PHASES = {
    'prepare-scheduled', 'prepare-differential', 'scan-scheduled', 'scan-baseline',
    'scan-candidate', 'report-scheduled', 'report-differential',
    'gate-scheduled', 'gate-differential',
}
SHA = re.compile(r'^[0-9a-f]{64}$')
COMMIT = re.compile(r'^[0-9a-f]{40}$')
MAX_JSON = 16 * 1024 * 1024

class Refusal(RuntimeError):
    pass

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()

def regular(path, limit=MAX_JSON):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
        raise Refusal('untrusted file shape or size')
    return info

def strict_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refusal('duplicate JSON field')
        result[key] = value
    return result

def load(path):
    regular(path)
    return json.loads(path.read_text(), object_pairs_hook=strict_object,
        parse_constant=lambda _: (_ for _ in ()).throw(Refusal('nonfinite JSON')))

def owned(path):
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise Refusal('state directory must be regular exclusively owned mode0700')
    return path

def write(path, value):
    data = (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()
    if len(data) > MAX_JSON:
        raise Refusal('receipt size limit')
    created = False
    try:
        f = path.open('xb')
        created = True
        with f:
            os.fchmod(f.fileno(), 0o600)
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
    except Exception:
        if created:
            path.unlink(missing_ok=True)
        raise

def trusted():
    manifest = load(HERE / 'bootstrap-manifest.json')
    if manifest.get('version') != 1:
        raise Refusal('unsupported bootstrap manifest')
    for name, expected in manifest['trusted_files'].items():
        if name not in {'osv_producer.py', 'osv_trusted.py', 'osv_completion.py', 'osv_io.py', 'osv_cgroup.py', 'scanner-profile.json'}:
            raise Refusal('unknown trusted source path')
        regular(HERE / name)
        if not SHA.fullmatch(expected) or digest(HERE / name) != expected:
            raise Refusal('reviewed source/profile hash mismatch')
    if set(manifest['trusted_files']) != {'osv_producer.py', 'osv_trusted.py', 'osv_completion.py', 'osv_io.py', 'osv_cgroup.py', 'scanner-profile.json'}:
        raise Refusal('incomplete trusted source inventory')
    inventory = HERE / 'public-module-preload-inventory.json'
    if digest(inventory) != manifest['public_preload_inventory_sha256']:
        raise Refusal('preload inventory hash mismatch')
    # Fresh absent pycache_prefix is established by the shell before interpreter
    # startup. Worker invocations establish their own fresh prefix as well.
    if not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode:
        raise Refusal('unbound Python startup flags')
    prefix = sys.pycache_prefix
    if not prefix or Path(prefix).exists() or Path(prefix).is_symlink():
        raise Refusal('Python cache prefix must remain fresh and absent')
    sys.path.insert(0, str(HERE))
    from osv_io import bounded_run, Budget
    return manifest, load(HERE / 'scanner-profile.json'), bounded_run, Budget

def admission(manifest):
    if platform.system() != 'Linux' or platform.machine() != 'x86_64' or sys.version_info < (3, 11):
        raise Refusal('actual native Linux/amd64 Python>=3.11 required')
    if manifest['permanent_install_authorized'] is not True:
        raise Refusal('permanent automatic workflow installation remains unapproved')
    for field, name in [
        ('linux_runtime_acceptance_sha256', 'linux-hosted-acceptance.json'),
        ('supplier_delivery_receipt_sha256', 'supplier-delivery-receipt.json'),
        ('first_install_execution_receipt_sha256', 'first-install-execution-receipt.json'),
        ('hosted_job_timing_acceptance_sha256', 'hosted-job-timing-acceptance.json'),
        ('source_object_delivery_acceptance_sha256', 'source-object-delivery-acceptance.json'),
    ]:
        expected = manifest.get(field)
        if not isinstance(expected, str) or not SHA.fullmatch(expected):
            raise Refusal('reviewed ' + field + ' is unbound')
        path = HERE / name
        if digest(path) != expected:
            raise Refusal('admission receipt hash differs')
        receipt = load(path)
        if receipt.get('accepted') is not True:
            raise Refusal('admission receipt does not accept this exact runtime')
        if receipt.get('profile_sha256') != manifest['trusted_files']['scanner-profile.json']:
            raise Refusal('admission receipt belongs to another exact resource/profile')
    for key in ['bubblewrap', 'python']:
        path = Path(manifest['linux_tools'][key + '_path'])
        expected = manifest['linux_tools'][key + '_sha256']
        if not isinstance(expected, str) or not SHA.fullmatch(expected) or digest(path) != expected:
            raise Refusal('unbound Linux launcher/interpreter supplier')
    delivery = load(HERE / 'source-object-delivery-acceptance.json')
    if (delivery.get('repository') != 'GoCodeAlone/workflow' or
        delivery.get('trusted_checkout_action') != 'actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10' or
        delivery.get('candidate_execution') is not False or
        set(delivery.get('events', {})) != {'pull_request','merge_group','push','schedule'}):
        raise Refusal('exact normal-event source object delivery admission missing')
    for event, evidence in delivery['events'].items():
        if not isinstance(evidence, dict) or evidence.get('accepted') is not True or not SHA.fullmatch(str(evidence.get('proof_sha256', ''))):
            raise Refusal('normal-event source object delivery lacks authentic bound proof')
    # This independently reviewed prerequisite never substitutes for each
    # actual event's exact rev-parse/tree/blob checks below. No dynamic fetch.
    timing = load(HERE / 'hosted-job-timing-acceptance.json')
    scan_seconds = load(HERE / 'scanner-profile.json')['resource_envelope']['total_seconds']
    for job, scans in [('scan-scheduled', 1), ('scan-pr', 2)]:
        row = timing['jobs'][job]
        numbers = [row[key] for key in ('job_timeout_seconds', 'prepare_total_seconds',
                                        'report_total_seconds', 'checkout_setup_upload_reserve_seconds')]
        if any(type(value) is not int or value <= 0 for value in numbers):
            raise Refusal('actual hosted job timing admission is incomplete')
        if row['report_total_seconds'] != 240 or sum(numbers[1:]) + scans * scan_seconds >= row['job_timeout_seconds']:
            raise Refusal('exact offline phases exceed witnessed hosted job budget')
    return load(HERE / 'linux-hosted-acceptance.json')

def state_path():
    parent = Path(os.environ['RUNNER_TEMP'])
    if not parent.is_absolute() or parent.is_symlink() or not parent.is_dir():
        raise Refusal('invalid runner state parent')
    run, attempt, job = [os.environ[name] for name in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB')]
    if not run.isdecimal() or not attempt.isdecimal() or job not in {'scan-pr', 'scan-scheduled'}:
        raise Refusal('unbound GitHub execution identity')
    return parent / ('wf-osv-offline-' + run + '-' + attempt + '-' + job)

def env(go, state, *, online=False):
    result = {
        'PATH': str(go.parent) + ':/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C',
        'HOME': str(state / 'home'), 'TMPDIR': str(state / 'tmp'),
        'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
        'GOPROXY': 'https://proxy.golang.org' if online else 'off',
        'GOSUMDB': 'sum.golang.org' if online else 'off',
        'GOFLAGS': '-mod=readonly', 'CGO_ENABLED': '0', 'GOVCS': '*:off',
        'GOPRIVATE': '', 'GONOPROXY': 'none', 'GONOSUMDB': 'none',
        'GOOS': 'linux', 'GOARCH': 'amd64', 'GOMAXPROCS': '2', 'GOMEMLIMIT': '768MiB',
        'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null',
        'GIT_CONFIG_NOSYSTEM': '1', 'GIT_TERMINAL_PROMPT': '0', 'GIT_ASKPASS': '/bin/false',
    }
    # Caches are existing allocated runner caches, never a new host private Go cache.
    runtime = load(HERE / 'linux-hosted-acceptance.json')
    result.update(GOPATH=runtime['existing_gopath'], GOMODCACHE=runtime['existing_module_cache'],
                  GOCACHE=runtime['existing_build_cache'])
    return result

def export_source(ref, label, state, child_env, bounded_run, Budget, prepare_budget):
    if not COMMIT.fullmatch(ref):
        raise Refusal('invalid exact source commit')
    budget = Budget(min(90, prepare_budget.left()))
    resolved = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'rev-parse', ref + '^{commit}'],
                           REPOSITORY, child_env, 30, budget)
    if resolved.returncode or resolved.stderr or resolved.stdout.strip() != ref:
        raise Refusal('exact source object absent; no dynamic fetch fallback')
    tree = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'ls-tree', '-r', '-z', ref],
                       REPOSITORY, child_env, 30, budget)
    if tree.returncode or tree.stderr:
        raise Refusal('Git source inventory failed')
    expected = {}
    for row in tree.stdout.split('\0'):
        if not row:
            continue
        meta, name = row.split('\t', 1)
        mode, kind, blob = meta.split()
        rel = PurePosixPath(name)
        if mode not in {'100644', '100755'} or kind != 'blob' or not COMMIT.fullmatch(blob) or rel.is_absolute() or '..' in rel.parts:
            raise Refusal('unsupported Git object/path')
        expected[name] = (mode, blob)
    if not expected or len(expected) > 10000:
        raise Refusal('source file limit')
    archive, root = state / (label + '.tar'), state / (label + '-source')
    root.mkdir(mode=0o700)
    result = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'archive', '--format=tar', '--output=' + str(archive), ref],
                         REPOSITORY, child_env, 30, budget)
    if result.returncode or result.stderr:
        raise Refusal('Git export failed')
    regular(archive, 1024 * 1024 * 1024)
    seen, total, entries = {}, 0, 0
    with tarfile.open(archive, mode='r:') as tar:
        for member in tar:
            budget.left(); entries += 1
            rel = PurePosixPath(member.name)
            if rel.is_absolute() or '..' in rel.parts or not rel.parts or entries > 20000:
                raise Refusal('unsafe exported source path/entry limit')
            target = root.joinpath(*rel.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            total += member.size
            if not member.isfile() or member.name not in expected or member.name in seen or member.size > 32 * 1024 * 1024 or total > 1024 * 1024 * 1024:
                raise Refusal('unsafe, substituted or oversized source export')
            target.parent.mkdir(parents=True, exist_ok=True)
            h = hashlib.sha1(('blob ' + str(member.size) + '\0').encode())
            with tar.extractfile(member) as source, target.open('xb') as dest:
                for block in iter(lambda: source.read(1024 * 1024), b''):
                    budget.left(); dest.write(block); h.update(block)
            mode, blob = expected[member.name]
            target.chmod(0o755 if mode == '100755' else 0o644)
            if h.hexdigest() != blob or member.mode & 0o111 != (0o111 if mode == '100755' else 0):
                raise Refusal('exported mode/blob differs from exact Git tree')
            seen[member.name] = {'git_mode': mode, 'git_blob': blob, 'sha256': digest(target)}
    if set(seen) != set(expected):
        raise Refusal('export omitted Git-tracked source')
    discovered = {str(path.relative_to(root)) for path in root.rglob('*')
                  if path.is_file() and path.name in {'go.mod', 'package-lock.json'}}
    return root, {'commit': ref, 'archive_sha256': digest(archive), 'files': seen,
                  'manifests': sorted(discovered), 'bytes': total}

class FixedRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if newurl != req.full_url:
            raise Refusal('fixed database URL redirect refused')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def databases(state, profile, prepare_budget):
    result = {}; opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), FixedRedirect())
    for ecosystem in ['Go', 'npm']:
        prepare_budget.left()
        url = 'https://osv-vulnerabilities.storage.googleapis.com/' + ecosystem + '/all.zip'
        limit = profile['resource_envelope']['database_archive_bytes'][ecosystem]
        with opener.open(urllib.request.Request(url, method='HEAD'), timeout=30) as response:
            length = int(response.headers['Content-Length'])
            etag, generation = response.headers['ETag'], response.headers['x-goog-generation']
        if length <= 0 or length > limit or not generation.isdecimal():
            raise Refusal('database metadata exceeds approved profile')
        path = state / 'public-db/osv-scalibr' / ecosystem / 'all.zip'
        path.parent.mkdir(parents=True, mode=0o700)
        h, count, started = hashlib.sha256(), 0, time.monotonic()
        request = urllib.request.Request(url, headers={'If-Match': etag})
        with opener.open(request, timeout=30) as response, path.open('xb') as f:
            if int(response.headers['Content-Length']) != length or response.headers['ETag'] != etag or response.headers['x-goog-generation'] != generation:
                raise Refusal('database generation changed during acquisition')
            os.fchmod(f.fileno(), 0o600)
            for block in iter(lambda: response.read(1024 * 1024), b''):
                prepare_budget.left()
                count += len(block)
                if count > length or count > limit or time.monotonic() - started > 90:
                    raise Refusal('database capture limit/deadline')
                f.write(block); h.update(block)
            f.flush(); os.fsync(f.fileno())
        if count != length:
            raise Refusal('partial database body')
        result[ecosystem] = {'url': url, 'sha256': h.hexdigest(), 'bytes': count,
                            'etag': etag, 'generation': generation,
                            'fetched_at': datetime.now(timezone.utc).isoformat()}
    write(state / 'public-database-receipts.json', result)
    prepare_budget.left()
    return result

def supplier(state, manifest, runtime):
    tools, go = Path(runtime['supplier_root']), Path(runtime['compiler_root']) / 'bin/go'
    for name, expected in manifest['linux_pins'].items():
        regular(tools / name, 256 * 1024 * 1024)
        if digest(tools / name) != expected:
            raise Refusal('native supplier binary differs')
    if not go.is_file() or digest(go) != runtime['compiler_sha256']:
        raise Refusal('actual compiler supplier differs')
    return tools, go

def prepare(phase, state, manifest, profile, runtime, bounded_run, Budget):
    job = 'scan-pr' if phase == 'prepare-differential' else 'scan-scheduled'
    prepare_budget = Budget(load(HERE / 'hosted-job-timing-acceptance.json')['jobs'][job]['prepare_total_seconds'])
    state.mkdir(mode=0o700)
    for name in ['home', 'tmp', 'evidence', 'runs']:
        (state / name).mkdir(mode=0o700)
    event, candidate, trusted_sha = [os.environ[name] for name in ('EVENT_NAME', 'CANDIDATE_SHA', 'TRUSTED_SHA')]
    differential = phase == 'prepare-differential'
    if event not in ({'pull_request', 'merge_group'} if differential else {'push', 'schedule'}):
        raise Refusal('event does not match exact job')
    if not all(COMMIT.fullmatch(value) for value in [candidate, trusted_sha]):
        raise Refusal('invalid candidate/trusted SHA')
    tools, go = supplier(state, manifest, runtime)
    child_env = env(go, state)
    current = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'rev-parse', 'HEAD'], REPOSITORY, child_env, 30)
    if current.returncode or current.stderr or current.stdout.strip() != trusted_sha:
        raise Refusal('bootstrap checkout is not exact trusted event base')
    refs = {'candidate': candidate}
    if differential:
        base = os.environ['BASE_SHA']
        if base != trusted_sha:
            raise Refusal('baseline must equal exact trusted event base')
        refs['baseline'] = base
    sources = {}
    for label, ref in refs.items():
        _, sources[label] = export_source(ref, label, state, child_env, bounded_run, Budget, prepare_budget)
        prepare_budget.left()
        if set(sources[label]['manifests']) != set(profile['inputs']):
            raise Refusal('source manifest set differs from complete reviewed profile')
    inventory = load(HERE / 'public-module-preload-inventory.json')
    # Admit the dependency declarations themselves before requesting any module.
    # Slots may change on code-only commits; exact reviewed path/hash must remain.
    allowed = {}
    for row in inventory['sources'] + inventory['source_go_mod_admission']:
        allowed.setdefault(row['path'], set()).add(row['sha256'])
    module_dirs = {str(PurePosixPath(name).parent) for name in profile['inputs'] if name.endswith('go.mod')}
    for label in sources:
        source = state / (label + '-source')
        for directory in module_dirs:
            for filename in ['go.mod', 'go.sum']:
                name = str(PurePosixPath(directory) / filename)
                path = source / name
                if path.exists():
                    regular(path)
                    if digest(path) not in allowed.get(name, set()):
                        raise Refusal('dependency declaration is outside exact reviewed public inventory')
                elif filename == 'go.mod' or name in allowed:
                    raise Refusal('required source module declaration is missing')
    if not all(row['public_identity_reviewed'] is True for row in inventory['items']):
        raise Refusal('public module identity admission remains incomplete')
    # Preload only explicit reviewed path/version arguments; no candidate command,
    # npm install, direct VCS, private proxy fallback or implicit module graph call.
    online = env(go, state, online=True)
    for item in inventory['items']:
        target = item['module'] + '@' + item['version']
        downloaded = bounded_run([str(go), 'mod', 'download', '-json', target], state, online, 90, prepare_budget)
        if downloaded.returncode or downloaded.stderr:
            raise Refusal('authenticated explicit public module preload failed')
        record = json.loads(downloaded.stdout, object_pairs_hook=strict_object)
        if record.get('Path') != item['module'] or record.get('Version') != item['version'] or record.get('Sum') != item['h1'] or record.get('Error'):
            raise Refusal('public module preload identity/checksum mismatch')
    db = databases(state, profile, prepare_budget)
    write_completion(state / 'prepared.json', {'complete': True, 'sources': sources, 'databases': db,
        'profile_sha256': manifest['trusted_files']['scanner-profile.json'],
        'supplier_pins': manifest['linux_pins'], 'compiler_sha256': runtime['compiler_sha256'],
        'bootstrap_manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'created_at': datetime.now(timezone.utc).isoformat()}, prepare_budget)

def socket_filter():
    # x86_64 seccomp_data arch/nr offsets; all socket syscalls and namespace,
    # mount/privilege/debug/io_uring escape paths refuse. Wrong arch kills.
    rows = [(0x20, 0, 0, 4), (0x15, 1, 0, 0xc000003e), (0x06, 0, 0, 0x80000000), (0x20, 0, 0, 0),
            (0x45, 0, 1, 0x40000000), (0x06, 0, 0, 0x80000000)]  # x32 ABI must not bypass native syscall numbers.
    denied = {41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,101,155,165,166,272,288,299,307,308,321,426,427,428,435}
    for nr in sorted(denied):
        rows.extend([(0x15, 0, 1, nr), (0x06, 0, 0, 0x00050000 | errno.EPERM)])
    # clone flags argument0: Go thread creation remains, namespace creation refuses.
    rows.extend([(0x15, 0, 3, 56), (0x20, 0, 0, 16),
                 (0x45, 0, 1, 0x7e020000), (0x06, 0, 0, 0x00050000 | errno.EPERM),
                 (0x06, 0, 0, 0x7fff0000)])
    return b''.join(struct.pack('<HBBI', *row) for row in rows)

def capture_worker(args, cwd, child_env, filter_fd, budget, timeout):
    """Only one explicit filter FD inherited; capture/reap ownership is conserved."""
    proc = None; reaped = False; selector = selectors.DefaultSelector()
    streams = [bytearray(), bytearray()]
    deadline = time.monotonic() + min(timeout, budget.left())
    try:
        proc = subprocess.Popen(args, cwd=cwd, env=child_env,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True,
            pass_fds=(filter_fd,), start_new_session=True)
        for index, pipe in enumerate([proc.stdout, proc.stderr]):
            os.set_blocking(pipe.fileno(), False)
            selector.register(pipe, selectors.EVENT_READ, index)
        while selector.get_map():
            remaining = min(deadline - time.monotonic(), budget.left())
            if remaining <= 0:
                raise Refusal('Linux worker capture deadline exceeded')
            for key, _ in selector.select(min(remaining, 0.25)):
                block = os.read(key.fileobj.fileno(), 65536)
                if not block:
                    selector.unregister(key.fileobj); continue
                index = key.data
                if len(streams[index]) + len(block) > [16 * 1024 * 1024, 4 * 1024 * 1024][index]:
                    raise Refusal('Linux worker stream bound exceeded')
                budget.charge(len(block)); streams[index].extend(block)
        remaining = min(deadline - time.monotonic(), budget.left())
        if remaining <= 0:
            raise Refusal('Linux worker completion deadline exceeded')
        code = proc.wait(timeout=remaining)
        reaped = True  # Never signal or wait again after this successful reap.
        return code, bytes(streams[0]), bytes(streams[1])
    finally:
        selector.close()
        if proc is not None:
            if not reaped:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                proc.wait(timeout=5)
            for pipe in [proc.stdout, proc.stderr]:
                if pipe is not None:
                    pipe.close()

def write_completion(path, value, budget):
    # Completion cannot survive a post-write deadline refusal.
    budget.left()
    written = False
    try:
        write(path, value)
        written = True
        budget.left()
    except Exception:
        if written:
            path.unlink()
        raise

def scan_label(phase):
    return 'baseline' if phase == 'scan-baseline' else 'candidate'

def start_scan(state, phase):
    # A later invocation cannot inherit a prior positive receipt. The independent
    # current GitHub step output token is additionally required by consumers,
    # even if failure prevents replacing the prior on-disk current pointer.
    label = scan_label(phase)
    token = secrets.token_hex(32)
    directory = state / ('scan-attempt-' + label + '-' + token)
    directory.mkdir(mode=0o700)
    write(directory / 'started.json', {'complete': False, 'label': label,
        'phase': phase, 'token': token, 'prepared_sha256': digest(state / 'prepared.json')})
    pointer = state / (label + '-current.pending-' + token)
    write(pointer, {'token': token, 'label': label, 'phase': phase})
    os.replace(pointer, state / (label + '-current.json'))
    return directory, token

def github_scan_output(token):
    # This is the current scan step's private runner output file, never a source
    # or worker-selected path. No public artifact substitutes for step success.
    path = Path(os.environ['GITHUB_OUTPUT'])
    if not path.is_absolute() or path.is_symlink():
        raise Refusal('unbound current GitHub step output')
    info = regular(path, 1024 * 1024)
    if info.st_uid != os.getuid() or info.st_mode & 0o022:
        raise Refusal('GitHub step output is not exclusively writable by runner owner')
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
    with os.fdopen(fd, 'ab') as output:
        current = os.fstat(output.fileno())
        if (current.st_dev, current.st_ino) != (info.st_dev, info.st_ino) or not stat.S_ISREG(current.st_mode):
            raise Refusal('GitHub step output changed during opening')
        output.write(('scan-token=' + token + '\n').encode())
        output.flush(); os.fsync(output.fileno())

def seal_scan(state, phase, attempt, token, result, budget):
    # All cleanup has already completed. A result without an atomic final seal
    # is unusable. Write/fsync/chmod/deadline failure can leave partial/private
    # files even when unlink fails; no reader recognizes the pending filename.
    result_path = attempt / 'result.json'
    write(result_path, result)
    budget.left()
    value = {'complete': True, 'label': scan_label(phase), 'phase': phase,
        'token': token, 'result_sha256': digest(result_path),
        'prepared_sha256': digest(state / 'prepared.json'),
        'manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'profile_sha256': digest(HERE / 'scanner-profile.json'),
        'finding_exit': result['exit'], 'phase_exit': 0}
    pending = attempt / 'outcome.pending.json'
    write(pending, value)
    github_scan_output(token)
    trusted()
    budget.left()
    # Rename can itself block. Its final shared-deadline veto must precede
    # actual step success. A retained seal alone cannot authorize consumption:
    # readers also require the current runner step's successful phase outcome.
    os.replace(pending, attempt / 'outcome.json')
    budget.left()
    return 0

def step_scan_identity(label):
    prefix = label.upper() + '_SCAN_'
    token, outcome = os.environ.get(prefix + 'TOKEN', ''), os.environ.get(prefix + 'OUTCOME', '')
    if not SHA.fullmatch(token) or outcome != 'success':
        raise Refusal('actual current scan step did not finish successfully')
    return token

def cgroup_helpers():
    trusted()  # The new topology helper is pinned before any import/query.
    import osv_cgroup
    return osv_cgroup

def close_input():
    # Bind fd0 before any child spawn. This also closes the input forwarded by
    # systemd-run --pipe and the stdin inherited by the namespace worker.
    before = os.lstat('/dev/null')
    fd = os.open('/dev/null', os.O_RDONLY | os.O_NOFOLLOW)
    try:
        opened = os.fstat(fd)
        identity = lambda info: (info.st_dev, info.st_ino, info.st_rdev, info.st_uid)
        if (not stat.S_ISCHR(before.st_mode) or not stat.S_ISCHR(opened.st_mode) or
                before.st_uid != 0 or identity(before) != identity(opened) or
                (platform.system() == 'Linux' and opened.st_rdev != os.makedev(1, 3))):
            raise Refusal('canonical null input device identity changed')
        os.dup2(fd, 0, inheritable=True)
        if identity(os.fstat(0)) != identity(opened):
            raise Refusal('null input descriptor binding failed')
    finally:
        if fd != 0:
            os.close(fd)

def release_group(group, supervisor):
    # This group was exclusively created by this manager; workers cannot mount
    # its writable controls or move themselves elsewhere. Do not signal a reaped
    # PID/process group. The owned cgroup covers every remaining descendant.
    if (supervisor.name != 'supervisor' or group.parent != supervisor.parent or
            group.name != 'wf-osv-' + str(os.getpid()) or
            group.is_symlink() or supervisor.is_symlink()):
        raise Refusal('unbound owned cgroup cleanup')
    # The controller-distributing root must stay empty. Restore the trusted
    # manager to its recorded supervisor leaf, never to that root.
    (supervisor / 'cgroup.procs').write_text(str(os.getpid()))
    if str(os.getpid()) not in (supervisor / 'cgroup.procs').read_text().splitlines():
        raise Refusal('manager cgroup restoration failed')
    if (group.parent / 'cgroup.procs').read_text().splitlines():
        raise Refusal('delegated cgroup root became populated')
    descendants = bool((group / 'cgroup.procs').read_text().splitlines())
    if descendants:
        (group / 'cgroup.kill').write_text('1')
        deadline = time.monotonic() + 5
        while (group / 'cgroup.procs').read_text().splitlines():
            if time.monotonic() >= deadline:
                raise Refusal('owned descendant cgroup did not empty after bounded kill')
            time.sleep(0.01)
    group.rmdir()
    if descendants:
        raise Refusal('unexpected live descendants after launcher reap; owned group killed')

def enter_group(runtime):
    # Acceptance is hash-pinned by the reviewed trusted manifest, then each actual
    # worker must additionally witness kernel limits/namespace/socket denial.
    binding = cgroup_helpers().attest_unit()
    cgroup_parent, supervisor = Path(binding['root']), Path(binding['supervisor'])
    if (runtime.get('delegated_cgroup_parent') != str(cgroup_parent) or
            runtime.get('delegated_supervisor') != str(supervisor)):
        raise Refusal('runtime differs from the actual systemd root/supervisor binding')
    if (cgroup_parent / 'cgroup.procs').read_text().splitlines():
        raise Refusal('delegated cgroup root must remain empty')
    if str(os.getpid()) not in (supervisor / 'cgroup.procs').read_text().splitlines():
        raise Refusal('manager is outside the witnessed supervisor leaf')
    if not {'memory', 'pids'} <= set((cgroup_parent / 'cgroup.controllers').read_text().split()):
        raise Refusal('delegated root lacks approved controllers')
    # This is the sole explicit controller activation, inside the attested NEW
    # owned empty unit root. No ancestor or existing service is modified.
    (cgroup_parent / 'cgroup.subtree_control').write_text('+memory +pids')
    if set((cgroup_parent / 'cgroup.subtree_control').read_text().split()) != {'memory', 'pids'}:
        raise Refusal('delegated controller activation readback differs')
    group = cgroup_parent / ('wf-osv-' + str(os.getpid()))
    group.mkdir()
    return group, supervisor

def worker_mounts(state, private, group, runtime):
    mounts = [(Path('/usr'), False), (Path('/lib'), False), (Path('/lib64'), False),
              (HERE, False), (Path(runtime['compiler_root']), False), (Path(runtime['supplier_root']), False),
              (Path(runtime['existing_module_cache']), False), (Path(runtime['existing_build_cache']), True)]
    mounts += [(state / (label + '-source'), False) for label in load(state / 'prepared.json')['sources']]
    mounts.insert(0, (state, False))
    mounts += [(group, False), (state / 'public-db', False), (state / 'public-database-receipts.json', False),
               (state / 'prepared.json', False), (private, True), (state / 'runs', True),
               (state / 'home', True), (state / 'tmp', True)]
    return mounts

def boundary(phase, state, manifest, runtime, bounded_run, Budget):
    profile = load(HERE / 'scanner-profile.json')
    total_limit = profile['resource_envelope']['total_seconds']
    budget = Budget(240 if phase.startswith('report-') else total_limit)
    group, cgroup_parent = enter_group(runtime)
    try:
        for name, value in [('memory.max', '2147483648'), ('memory.swap.max', '0'), ('pids.max', '256')]:
            (group / name).write_text(value)
            if (group / name).read_text().strip() != value:
                raise Refusal('kernel resource limit not applied')
        (group / 'cgroup.procs').write_text(str(os.getpid()))
        private = Path(tempfile.mkdtemp(prefix='worker-', dir=state)); private.chmod(0o700)
        root_cache = private / 'pycache'
        if root_cache.exists():
            raise Refusal('worker cache prefix already exists')
        request = {'phase': phase, 'state': str(state), 'host_net_ns': os.readlink('/proc/self/ns/net'),
                   'host_mount_ns': os.readlink('/proc/self/ns/mnt'), 'limits': {
                       name: (group / name).read_text().strip() for name in ['memory.max','memory.swap.max','pids.max']},
                   'cgroup': str(group)}
        if phase.startswith('report-'):
            labels = ['candidate', 'baseline'] if phase == 'report-differential' else ['candidate']
            request['scan_tokens'] = {label: step_scan_identity(label) for label in labels}
        write(private / 'request.json', request)
        filter_path = private / 'filter.bpf'; filter_path.write_bytes(socket_filter()); filter_path.chmod(0o600)
        bwrap = manifest['linux_tools']['bubblewrap_path']
        python = manifest['linux_tools']['python_path']
        args = [bwrap, '--unshare-all', '--die-with-parent', '--new-session', '--cap-drop', 'ALL', '--clearenv']
        for path, writable in worker_mounts(state, private, group, runtime):
            if not path.exists():
                raise Refusal('required trusted Linux mount missing')
            args += ['--bind' if writable else '--ro-bind', str(path), str(path)]
        args += ['--proc', '/proc', '--dev', '/dev', '--chdir', str(HERE)]
        with filter_path.open('rb') as filter_file:
            args += ['--seccomp', str(filter_file.fileno()), '--', python, '-I', '-S', '-B', '-X',
                     'pycache_prefix=' + str(root_cache), str(HERE / 'run.py'), '--worker', str(private / 'request.json')]
            code, stdout, stderr = capture_worker(args, HERE,
                {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C'},
                filter_file.fileno(), budget, 240 if phase.startswith('report-') else total_limit)
        for suffix, raw in [('stdout', stdout), ('stderr', stderr)]:
            path = private / ('launcher.' + suffix)
            with path.open('xb') as f:
                os.fchmod(f.fileno(), 0o600); f.write(raw)
        if code not in [0, 1] or not (private / 'worker-result.json').exists():
            raise Refusal('actual Linux worker did not complete')
        result = load(private / 'worker-result.json')
        if result.get('complete') is not True or result.get('exit') != code:
            raise Refusal('Linux worker completion/exit contradiction')
        if phase.startswith('scan-'):
            label = 'baseline' if phase == 'scan-baseline' else 'candidate'
            proof = Path(result['proof'])
            if proof.parent != state / 'runs' or proof.is_symlink():
                raise Refusal('worker proof outside managed output')
            result.update(report_sha256=digest(proof / 'results.json'),
                          receipt_sha256=digest(proof / 'receipt.json'))
        else:
            sarif = Path(result['sarif'])
            if sarif != private / 'reporter.sarif' or digest(sarif) != result['sarif_sha256']:
                raise Refusal('reporter output binding changed')
            annotations = private / 'reporter.stderr'
            regular(annotations, 4 * 1024 * 1024)
            result['annotations_sha256'] = digest(annotations)
        trusted()  # Recheck all trusted helper/profile bytes before completion.
        budget.left()
    finally:
        release_group(group, cgroup_parent)
    trusted()
    budget.left()
    # This real owned empty-directory cleanup must precede any positive scan
    # outcome, public SARIF/JSON or reporter annotation publication.
    cleanup_startup_directory()
    trusted()
    budget.left()
    return code, result, budget

def worker(request_path, manifest, profile):
    request = load(request_path); state = owned(Path(request['state']))
    if os.readlink('/proc/self/ns/net') == request['host_net_ns'] or os.readlink('/proc/self/ns/mnt') == request['host_mount_ns']:
        raise Refusal('Linux namespaces were not isolated')
    try:
        socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    except OSError as error:
        if error.errno != errno.EPERM:
            raise Refusal('socket denial is not enforced EPERM')
    else:
        raise Refusal('network socket creation unexpectedly available')
    for name, value in request['limits'].items():
        if (Path(request['cgroup']) / name).read_text().strip() != value:
            raise Refusal('actual worker kernel limit differs')
    if str(os.getpid()) not in (Path(request['cgroup']) / 'cgroup.procs').read_text().splitlines():
        raise Refusal('actual worker is outside accepted cgroup')
    resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_JSON, MAX_JSON))
    from osv_producer import run_one
    from osv_io import bounded_run, Budget
    runtime = load(HERE / 'linux-hosted-acceptance.json')
    tools, go = supplier(state, manifest, runtime)
    child_env = env(go, state)
    phase = request['phase']; label = 'baseline' if phase == 'scan-baseline' else 'candidate'
    if phase.startswith('scan-'):
        out, blocking = run_one(state / (label + '-source'), profile['inputs'], tools, go,
            state / 'public-db', load(state / 'public-database-receipts.json'), datetime.now(timezone.utc), state / 'runs', child_env)
        write(request_path.parent / 'worker-result.json', {'complete': True, 'proof': str(out),
            'blocking': sorted(blocking), 'label': label, 'exit': 1 if blocking else 0,
            'socket_denial': 'EPERM', 'kernel_limits': request['limits']})
        return 1 if blocking else 0
    if phase in {'report-scheduled', 'report-differential'}:
        _, new = complete_result(state, 'candidate', manifest, request['scan_tokens']['candidate'])
        sarif = request_path.parent / 'reporter.sarif'
        args = [str(tools / 'osv-reporter'), '--output-files=sarif:' + str(sarif),
                '--new=' + str(new / 'results.json'), '--fail-on-vuln=true']
        if phase == 'report-differential':
            _, old = complete_result(state, 'baseline', manifest, request['scan_tokens']['baseline'])
            args += ['--old=' + str(old / 'results.json')]
            args[1] += ',gh-annotations:#stderr'
        report = bounded_run(args, HERE, child_env, 240, Budget(240))
        regular(sarif)
        if report.returncode not in [0, 1]:
            raise Refusal('reporter incomplete or unsupported exit')
        for suffix, content in [('stdout', report.stdout), ('stderr', report.stderr)]:
            path = request_path.parent / ('reporter.' + suffix)
            with path.open('xb') as f:
                os.fchmod(f.fileno(), 0o600); f.write(content.encode())
        write(request_path.parent / 'worker-result.json', {'complete': True, 'exit': report.returncode,
            'sarif': str(sarif), 'sarif_sha256': digest(sarif), 'socket_denial': 'EPERM',
            'kernel_limits': request['limits']})
        return report.returncode
    raise Refusal('unsupported worker phase')

def complete_result(state, label, manifest, worker_token=None):
    token = step_scan_identity(label) if worker_token is None else worker_token
    if not SHA.fullmatch(token):
        raise Refusal('invalid trusted report-worker scan identity')
    current = load(state / (label + '-current.json'))
    if current.get('label') != label or current.get('token') != token:
        raise Refusal('stale scan invocation is not current runner step')
    if current.get('phase') not in ({'scan-baseline'} if label == 'baseline' else {'scan-candidate', 'scan-scheduled'}):
        raise Refusal('current scan phase does not match exact result label')
    attempt = owned(state / ('scan-attempt-' + label + '-' + token))
    outcome = load(attempt / 'outcome.json')
    if outcome.get('complete') is not True or type(outcome.get('phase_exit')) is not int or type(outcome.get('finding_exit')) is not int:
        raise Refusal('malformed success-bound scan outcome')
    result_path = attempt / 'result.json'
    expected = {'complete': True, 'label': label, 'phase': current.get('phase'),
        'token': token, 'prepared_sha256': digest(state / 'prepared.json'),
        'manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'profile_sha256': digest(HERE / 'scanner-profile.json'),
        'phase_exit': 0, 'result_sha256': digest(result_path)}
    if any(outcome.get(key) != value for key, value in expected.items()):
        raise Refusal('scan success outcome differs from exact result/prepared/profile state')
    result = load(result_path)
    if outcome.get('finding_exit') != result.get('exit'):
        raise Refusal('scan phase outcome differs from completed finding exit')
    if result.get('label') != label or result.get('complete') is not True or type(result.get('exit')) is not int or result.get('exit') not in [0, 1] or result.get('socket_denial') != 'EPERM':
        raise Refusal('missing or incomplete actual scan result')
    proof = Path(result['proof'])
    if proof.parent != state / 'runs' or proof.is_symlink():
        raise Refusal('scan output is outside producer-owned root')
    receipt = load(proof / 'receipt.json')
    if receipt.get('complete') is not True or receipt.get('blocking') != result['blocking'] or result['exit'] != (1 if result['blocking'] else 0):
        raise Refusal('producer completion/finding contradiction')
    if digest(proof / 'results.json') != result['report_sha256'] or digest(proof / 'receipt.json') != result['receipt_sha256']:
        raise Refusal('producer report or receipt changed')
    prepared = load(state / 'prepared.json')
    if prepared['bootstrap_manifest_sha256'] != digest(HERE / 'bootstrap-manifest.json') or prepared['profile_sha256'] != manifest['trusted_files']['scanner-profile.json']:
        raise Refusal('prepared trusted manifest/profile changed')
    expected_source = {name: [0o755 if row['git_mode'] == '100755' else 0o644, row['sha256']]
                       for name, row in prepared['sources'][label]['files'].items()}
    if receipt['source'] != expected_source or receipt['compiler'] != 'go version go1.27.2 linux/amd64':
        raise Refusal('completed source/compiler differs from exact admitted Git export')
    if receipt['databases'] != {key: row['sha256'] for key, row in prepared['databases'].items()} or receipt['supplier_pins'] != {k:v for k,v in manifest['linux_pins'].items() if k != 'osv-reporter'}:
        raise Refusal('scan database/supplier identity differs')
    return result, proof

def publish(state, name, source):
    if name not in {'results.sarif','old-results.json','new-results.json'}:
        raise Refusal('unknown public output path')
    regular(source)
    destination = REPOSITORY / name
    if destination.exists() or destination.is_symlink():
        raise Refusal('refuse preexisting output publication path')
    with source.open('rb') as src, destination.open('xb') as out:
        os.fchmod(out.fileno(), 0o600)
        shutil.copyfileobj(src, out, 1024 * 1024)

def evidence(state, manifest):
    destination = REPOSITORY / 'offline-evidence'
    if destination.exists() or destination.is_symlink():
        owned(destination)
        for item in destination.iterdir():
            if not re.fullmatch(r'failure-(?:prepare|scan|report|gate)-(?:scheduled|differential|baseline|candidate)\.json', item.name):
                raise Refusal('refuse existing unrelated evidence publication path')
            regular(item)
    else:
        destination.mkdir(mode=0o700)
    total = 0
    for name in ['prepared.json', 'public-database-receipts.json']:
        source = state / name; regular(source)
        shutil.copyfile(source, destination / name); (destination / name).chmod(0o600)
    for label in load(state / 'prepared.json')['sources']:
        result, proof = complete_result(state, label, manifest)
        output = destination / label; output.mkdir(mode=0o700)
        write(output / 'result.json', result)
        for source in proof.iterdir():
            if not re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.(?:stdout|stderr|rc)|receipt\.json|results\.json', source.name):
                continue
            info = regular(source); total += info.st_size
            if total > 128 * 1024 * 1024:
                raise Refusal('evidence publication aggregate bound exceeded')
            shutil.copyfile(source, output / source.name); (output / source.name).chmod(0o600)

def reporting(phase, state, manifest, runtime, bounded_run, Budget):
    candidate, new = complete_result(state, 'candidate', manifest)
    if phase == 'report-differential':
        _, old = complete_result(state, 'baseline', manifest)
    code, result, budget = boundary(phase, state, manifest, runtime, bounded_run, Budget)
    # The launcher and all owned descendants are retired, startup cleanup has
    # succeeded, and trusted bytes/deadline were rechecked before publication.
    candidate, new = complete_result(state, 'candidate', manifest)
    if phase == 'report-differential':
        _, old = complete_result(state, 'baseline', manifest)
    evidence(state, manifest)
    sarif = Path(result['sarif'])
    annotations = sarif.parent / 'reporter.stderr'
    if digest(sarif) != result['sarif_sha256'] or digest(annotations) != result['annotations_sha256']:
        raise Refusal('sealed reporter output changed after retirement')
    trusted(); budget.left()
    if phase == 'report-differential':
        publish(state, 'old-results.json', old / 'results.json')
        publish(state, 'new-results.json', new / 'results.json')
    publish(state, 'results.sarif', sarif)
    if phase == 'report-differential':
        sys.stderr.write(annotations.read_text())
        sys.stderr.flush()
    # Publication and its final annotation flush consume the same report budget.
    # A late write leaves retired diagnostics but never a successful phase exit.
    budget.left()
    return code

def gate(phase, state, manifest):
    candidate, _ = complete_result(state, 'candidate', manifest)
    if phase == 'gate-differential':
        complete_result(state, 'baseline', manifest)
    # Differential report status cannot subtract or waive candidate findings.
    return 1 if candidate['blocking'] else 0

def main():
    phase = sys.argv[1] if len(sys.argv) > 1 else ''
    manifest, profile, bounded_run, Budget = trusted()
    runtime = admission(manifest)
    if phase == '--worker' and len(sys.argv) == 3:
        return worker(Path(sys.argv[2]), manifest, profile)
    if phase not in PHASES or len(sys.argv) != 2:
        raise Refusal('unknown offline scanner phase')
    state = state_path()
    if phase.startswith('prepare-'):
        cleanup_startup_directory()
        prepare(phase, state, manifest, profile, runtime, bounded_run, Budget)
        return 0
    owned(state)
    if phase.startswith('scan-'):
        attempt, token = start_scan(state, phase)
        code, result, budget = boundary(phase, state, manifest, runtime, bounded_run, Budget)
        if result['exit'] != code:
            raise Refusal('completed scan/worker exit mismatch')
        # Finding exit1 remains exact in result and final gate; a completed scan
        # wrapper exits0 so GitHub success distinguishes it from any exit2.
        return seal_scan(state, phase, attempt, token, result, budget)
    elif phase.startswith('report-'):
        return reporting(phase, state, manifest, runtime, bounded_run, Budget)
    else:
        cleanup_startup_directory()
        return gate(phase, state, manifest)
    raise Refusal('uncompleted scanner phase')

def failure_receipt(phase, error):
    # No arbitrary exception text, source paths, supplier response body or secret
    # enters a public artifact. Failure remains exit2, never an empty clean scan.
    reason = str(error)[:512] if isinstance(error, Refusal) else 'manager failed closed; no complete scan verdict'
    value = {'complete': False, 'phase': phase, 'error_type': type(error).__name__,
             'error': reason, 'exit': 2}
    if phase in PHASES:
        try:
            destination = REPOSITORY / 'offline-evidence'
            if destination.exists() or destination.is_symlink():
                owned(destination)
            else:
                destination.mkdir(mode=0o700)
            write(destination / ('failure-' + phase + '.json'), value)
            if phase.startswith(('scan-', 'report-')):
                preserve_incomplete_protocols(phase, destination)
        except Exception:
            # Publication failure cannot convert the original refusal to success.
            value['failure_evidence_publication'] = 'unavailable'
    return value

def preserve_incomplete_protocols(phase, destination):
    # A conservative gate can refuse after genuine raw findings were emitted.
    # Retain bounded producer outputs as incomplete evidence, never as accepted
    # scan/report state. The flat DB, source/config/cache and unrelated files are
    # excluded. No producer-supplied path or symlink selects the publication.
    state = state_path()
    if not state.exists():
        return
    owned(state)
    runs = state / 'runs'
    if not runs.exists():
        return
    owned(runs)
    output = destination / ('incomplete-' + phase)
    output.mkdir(mode=0o700)
    total, rows = 0, []
    def copy(source, target):
        nonlocal total
        info = regular(source)
        total += info.st_size
        if total > 128 * 1024 * 1024:
            raise Refusal('incomplete protocol publication aggregate limit')
        before = digest(source)
        with source.open('rb') as src, target.open('xb') as out:
            os.fchmod(out.fileno(), 0o600)
            shutil.copyfileobj(src, out, 1024 * 1024)
        if digest(source) != before or digest(target) != before:
            raise Refusal('incomplete producer output changed during capture')
        rows.append({'path': str(target.relative_to(output)), 'sha256': before, 'bytes': info.st_size})
    for name in ['prepared.json', 'public-database-receipts.json']:
        source = state / name
        if source.exists():
            copy(source, output / name)
    for proof in sorted(runs.iterdir()):
        if not re.fullmatch(r'osv-proof-[a-z0-9_]+', proof.name):
            raise Refusal('unknown producer proof directory')
        owned(proof)
        target = output / proof.name
        target.mkdir(mode=0o700)
        for source in sorted(proof.iterdir()):
            if re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.(?:stdout|stderr|rc)|receipt\.json|results\.json', source.name):
                copy(source, target / source.name)
    write(output / 'incomplete-publication.json', {'complete': False, 'phase': phase,
        'scope': 'unaccepted retained producer outputs; source slot and completeness cannot be inferred without a verified manager completion receipt',
        'inputs': rows, 'bytes': total, 'exit': 2})

def cleanup_startup_directory():
    # Public bootstrap's fresh directory is empty because -B forbids writes.
    # Worker parents also contain proofs and requests and must remain untouched.
    if len(sys.argv) == 2 and sys.argv[1] in PHASES and sys.pycache_prefix:
        parent = Path(sys.pycache_prefix).parent
        runner_parent = Path(os.environ.get('RUNNER_TEMP', '/nonexistent'))
        if parent.parent == runner_parent and parent.name.startswith('osv-python-'):
            owned(parent)
            parent.rmdir()

def entry():
    try:
        code = main()
    except Exception as error:
        # Preserve incomplete vs complete-findings distinction. No fabricated
        # empty report, skipped success, new check or approval is written.
        phase = sys.argv[1] if len(sys.argv) > 1 else ''
        print(json.dumps(failure_receipt(phase, error)), file=sys.stderr)
        try:
            cleanup_startup_directory()
        except Exception:
            print(json.dumps({'complete': False, 'error': 'fresh startup cleanup refused', 'exit': 2}), file=sys.stderr)
        sys.stderr.flush()
        os._exit(2)
    # No fallible cleanup, stream flush, finally or atexit after a positive seal.
    # Worker output files are explicit; reporter stderr was flushed before here.
    os._exit(code)

if __name__ == '__main__':
    entry()
