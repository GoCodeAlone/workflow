"""Supply one authenticated Ubuntu executable in owned temporary storage.

No root installer, package hooks, system configuration or namespace policy is
changed. This supplies a prerequisite, never scanner/installation admission.
A positive receipt remains provisional until the current supplier process exits
successfully; the proof job requires that actual outcome before consuming it.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import tempfile
from urllib.parse import urlsplit

HERE = Path(__file__).resolve().parent
PROOF_SHA = '7c81e5ada833caa4fe46abfb38bc3aadc6ff04077bd44b1349ea3ae5a7a69cb4'
SHA = re.compile(r'^[0-9a-f]{64}$')
VERSION = re.compile(r'^[0-9][0-9A-Za-z.+~:-]{0,100}$')


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def rows(text):
    result = []
    for stanza in text.strip().split('\n\n'):
        row = {}; previous = None
        for line in stanza.splitlines():
            if line[:1].isspace():
                require(previous is not None, 'malformed package continuation')
                row[previous] += '\n' + line
                continue
            key, separator, value = line.partition(':')
            require(separator and key not in row, 'malformed or duplicate package field')
            row[key] = value.strip(); previous = key
        result.append(row)
    return result


def package_record(text, version):
    candidates = rows(text)
    require(candidates and len(candidates) <= 4, 'ambiguous package metadata')
    keys = ('Package', 'Version', 'Architecture', 'SHA256', 'Filename', 'Size', 'Installed-Size')
    records = [{k: row[k] for k in keys} for row in candidates]
    require(all(row == records[0] for row in records), 'conflicting signed package records')
    row = records[0]
    require(row['Package'] == 'bubblewrap' and row['Version'] == version and
            row['Architecture'] == 'amd64' and SHA.fullmatch(row['SHA256']), 'unexpected package identity')
    require(re.fullmatch(r'pool/main/b/bubblewrap/bubblewrap_[0-9A-Za-z.+~:%-]+_amd64\.deb',
                         row['Filename']), 'unexpected package archive path')
    require(0 < int(row['Size']) <= 1024 * 1024 and
            0 < int(row['Installed-Size']) <= 2048, 'package exceeds bounded prerequisite size')
    return row


def source_url(value, filename):
    url = urlsplit(value)
    require(url.scheme == 'https' and url.netloc in {'archive.ubuntu.com', 'security.ubuntu.com'} and
            url.path == '/ubuntu/' + filename and not url.query and not url.fragment,
            'package source is not the fixed official Ubuntu archive')
    return value


def regular_binary(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_size <= 1024 * 1024 and
            not info.st_mode & (stat.S_ISUID | stat.S_ISGID), 'unexpected privileged/nonregular executable')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as f:
        raw = f.read(1024 * 1024 + 1)
    require(len(raw) == info.st_size and len(raw) >= 20 and raw[:6] == b'\x7fELF\x02\x01' and
            raw[18:20] == b'\x3e\x00', 'expected bounded Linux amd64 ELF executable')
    return raw


def supply(output):
    parent = Path(os.environ['RUNNER_TEMP'])
    info = output.lstat()
    require(output.is_absolute() and stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
            stat.S_IMODE(info.st_mode) == 0o700 and output.parent.resolve() == parent.resolve() and
            not any(output.iterdir()), 'fresh private owned output under RUNNER_TEMP required')
    record = {'schema': 'workflow.offline-scanner.bubblewrap-supplier.v1',
              'supplied': False, 'installation_admitted': False,
              'package_hooks_executed': False, 'policy_settings_changed': False}
    stage = 'ephemeral-platform'
    code = 2
    proof = None
    try:
        require(platform.system() == 'Linux' and platform.machine() == 'x86_64' and os.geteuid() != 0 and
                os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted', 'nonroot ephemeral GitHub Linux runner required')
        release = dict(line.split('=', 1) for line in Path('/etc/os-release').read_text().splitlines()
                       if '=' in line)
        require(release.get('ID', '').strip('"') == 'ubuntu' and
                release.get('VERSION_ID', '').strip('"') == '24.04', 'fixed Ubuntu24.04 prerequisite platform required')
        require(HERE.joinpath('prove-runtime.py').is_file() and
                hashlib.sha256(HERE.joinpath('prove-runtime.py').read_bytes()).hexdigest() == PROOF_SHA,
                'reviewed prerequisite manager identity changed')
        spec = importlib.util.spec_from_file_location('reviewed_prerequisite', HERE/'prove-runtime.py')
        proof = importlib.util.module_from_spec(spec); spec.loader.exec_module(proof)
        proof.owned_output(output)
        record['identity'] = {k: os.environ.get(k, '') for k in proof.IDENTITY}
        require(record['identity']['GITHUB_REPOSITORY'] == 'GoCodeAlone/workflow' and
                record['identity']['GITHUB_JOB'] == 'osv-runtime-prerequisite' and
                record['identity']['GITHUB_EVENT_NAME'] == 'workflow_dispatch', 'fixed diagnostic job required')
        manager = proof.manager()
        _, _, bounded, Budget = manager.trusted()
        budget = Budget(180, 8 * 1024 * 1024)
        tool_paths = ['/usr/bin/apt-get', '/usr/bin/apt-cache', '/usr/bin/dpkg-deb',
                      '/usr/share/keyrings/ubuntu-archive-keyring.gpg']
        record['host_tool_sha256'] = {p: proof.sha(Path(p)) for p in tool_paths}
        stage = 'signed-ubuntu-index'
        with tempfile.TemporaryDirectory(prefix='osv-ubuntu-source-', dir=os.environ['RUNNER_TEMP']) as temporary:
            work = Path(temporary)
            for name in ('empty', 'lists', 'archives', 'logs', 'downloads', 'unpacked'):
                (work/name).mkdir(mode=0o700)
            sources = work/'ubuntu.sources'
            sources.write_text('Types: deb\nURIs: https://archive.ubuntu.com/ubuntu\n'
                               'Suites: noble noble-updates\nComponents: main\nArchitectures: amd64\n'
                               'Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n\n'
                               'Types: deb\nURIs: https://security.ubuntu.com/ubuntu\n'
                               'Suites: noble-security\nComponents: main\nArchitectures: amd64\n'
                               'Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n')
            config = work/'apt.conf'
            (work/'empty-status').write_text('')
            config.write_text('\n'.join([
                'Dir::Etc::main "/dev/null";', 'Dir::Etc::parts "'+str(work/'empty')+'";',
                'Dir::Etc::sourcelist "'+str(sources)+'";', 'Dir::Etc::sourceparts "'+str(work/'empty')+'";',
                'Dir::Etc::preferences "/dev/null";', 'Dir::Etc::preferencesparts "'+str(work/'empty')+'";',
                'Dir::Etc::netrc "/dev/null";', 'Dir::Etc::netrcparts "'+str(work/'empty')+'";',
                'Dir::State::status "'+str(work/'empty-status')+'";',
                'Dir::State::lists "'+str(work/'lists')+'";', 'Dir::Cache::archives "'+str(work/'archives')+'";',
                'Dir::Cache::pkgcache "";', 'Dir::Cache::srcpkgcache "";', 'Dir::Log "'+str(work/'logs')+'";',
                'Acquire::Languages "none";', 'Acquire::Retries "0";',
                'Acquire::http::Timeout "20";', 'Acquire::https::Timeout "20";',
                'Acquire::AllowInsecureRepositories "false";', 'Acquire::AllowDowngradeToInsecureRepositories "false";',
                'APT::Get::AllowUnauthenticated "false";', 'APT::Update::Error-Mode "any";',
                'APT::Architecture "amd64";', 'APT::Architectures { "amd64"; };',
                'Acquire::IndexTargets::deb::DEP-11::DefaultEnabled "false";',
                'Acquire::IndexTargets::deb::CNF::DefaultEnabled "false";']) + '\n')
            env = {'PATH':'/usr/bin:/bin', 'LANG':'C', 'LC_ALL':'C', 'APT_CONFIG':str(config)}
            def run(argv, cwd=work, seconds=60):
                result = bounded(argv, cwd, env, seconds, budget, 1024 * 1024, 1024 * 1024)
                require(result.returncode == 0, 'bounded Ubuntu prerequisite command failed: ' + Path(argv[0]).name)
                require('Failed to fetch' not in result.stderr and '\nE:' not in '\n'+result.stderr,
                        'Ubuntu repository authentication/fetch failed')
                return result.stdout
            run(['/usr/bin/apt-get', 'update'], seconds=90)
            policy = run(['/usr/bin/apt-cache', 'policy', 'bubblewrap'])
            match = re.search(r'^\s*Candidate: (\S+)\s*$', policy, re.M)
            require(match and VERSION.fullmatch(match[1]), 'one signed Ubuntu package version required')
            version = match[1]
            row = package_record(run(['/usr/bin/apt-cache', 'show', '--no-all-versions',
                                      'bubblewrap='+version]), version)
            import shlex
            uris = run(['/usr/bin/apt-get', '--print-uris', 'download', 'bubblewrap='+version])
            lines = [shlex.split(line) for line in uris.splitlines() if line.startswith("'")]
            require(len(lines) == 1 and len(lines[0]) == 4, 'one explicit package download required')
            url = source_url(lines[0][0], row['Filename'])
            stage = 'authenticated-package-bytes'
            run(['/usr/bin/apt-get', 'download', 'bubblewrap='+version], cwd=work/'downloads')
            packages = list((work/'downloads').iterdir())
            require(len(packages) == 1 and stat.S_ISREG(packages[0].lstat().st_mode) and
                    packages[0].stat().st_size == int(row['Size']) and
                    proof.sha(packages[0]) == row['SHA256'], 'signed package size/hash mismatch')
            package = packages[0]
            record['package'] = {'name':'bubblewrap', 'version':version, 'architecture':'amd64',
                                 'source_url':url, 'sha256':row['SHA256'], 'bytes':int(row['Size'])}
            record['signed_index_sha256'] = {p.name:proof.sha(p) for p in (work/'lists').iterdir()
                                             if p.is_file() and p.name != 'lock'}
            stage = 'owned-binary-install'
            # dpkg-deb extracts data only into our fresh private directory. It
            # executes no maintainer scripts or hooks and modifies no OS path.
            contents = run(['/usr/bin/dpkg-deb', '--contents', str(package)])
            require(len(contents.encode()) <= 65536, 'package file list exceeds bound')
            record['package_file_list'] = contents.splitlines()
            run(['/usr/bin/dpkg-deb', '--extract', str(package), str(work/'unpacked')])
            raw = regular_binary(work/'unpacked'/'usr'/'bin'/'bwrap')
            with (output/'bwrap').open('xb') as f:
                os.fchmod(f.fileno(), 0o700); f.write(raw)
            record['binary_path'] = str(output/'bwrap')
            record['binary_sha256'] = hashlib.sha256(raw).hexdigest()
            shutil.copyfile(package, output/'bubblewrap.deb')
            os.chmod(output/'bubblewrap.deb', 0o600)
            stage = 'owned-cleanup-and-recheck'
        # Mandatory cleanup is part of supply, including its elapsed time.
        # Publish success only after the context has actually exited.
        budget.left()
        require(all(proof.sha(Path(p)) == sha for p,sha in record['host_tool_sha256'].items()),
                'host supplier tool changed')
        budget.left()
        record['supplied'] = True
        code = 0
    except Exception as error:
        record['supplied'] = False
        code = 2
        record['failure'] = {'stage':stage, 'type':type(error).__name__, 'message':str(error)[:2048],
                             'errno':getattr(error, 'errno', None)}
    finally:
        # Fresh exclusive output: do not rewrite previous evidence or publish
        # downloaded index caches, host environment or source checkout files.
        raw = (json.dumps(record, indent=2, sort_keys=True)+'\n').encode()
        require(len(raw) <= 65536, 'bounded supplier receipt exceeded')
        with (output/'bubblewrap-supplier-receipt.json').open('xb') as f:
            os.fchmod(f.fileno(), 0o600); f.write(raw)
    if code == 0:
        # Receipt write/close is inside the finite process budget too. A late
        # write must fail the actual step; do not rewrite provisional evidence.
        budget.left()
    return code


if __name__ == '__main__':
    import sys
    require(len(sys.argv) == 2, 'usage: supply-bubblewrap.py OWNED_OUTPUT')
    raise SystemExit(supply(Path(sys.argv[1])))
