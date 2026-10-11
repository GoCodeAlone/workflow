"""Finite prerequisite witness; never invokes a scanner or admits installation."""
import ctypes
from datetime import datetime, timezone
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import signal
import socket
import stat
import sys
import time

HERE = Path(__file__).resolve().parent
MANAGER_SHA = 'aaddbfd9e20cfe705da50fc7d6d343298a7f1eff5611b6d6d1ebf87a7781f911'
MANIFEST_SHA = 'bd66229470c06e769a56b48106bf552e0cbc783c4779fc19c54cae86934ed209'
PROFILE_SHA = '2deb233b43cf4fdc5babfe481c88bafe1bc8413062ea1e93041039e01b4c9997'
IDENTITY = ('GITHUB_REPOSITORY', 'GITHUB_SHA', 'GITHUB_RUN_ID',
            'GITHUB_RUN_ATTEMPT', 'GITHUB_EVENT_NAME', 'GITHUB_JOB',
            'RUNNER_OS', 'RUNNER_ARCH', 'ImageOS', 'ImageVersion', 'RUNNER_ENVIRONMENT')


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def write(path, value):
    data = (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()
    if len(data) > 65536:
        raise RuntimeError('bounded proof exceeded 64KiB')
    with path.open('xb') as f:
        os.fchmod(f.fileno(), 0o600)
        f.write(data)
        f.flush()
        os.fsync(f.fileno())


def owned_output(path):
    parent = Path(os.environ['RUNNER_TEMP'])
    info = path.lstat()
    if (not path.is_absolute() or path.is_symlink() or
            path.parent.resolve() != parent.resolve() or
            not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or
            stat.S_IMODE(info.st_mode) != 0o700 or any(path.iterdir())):
        raise RuntimeError('proof output must be fresh owned mode0700 under RUNNER_TEMP')
    return path


def manager():
    for name, expected in [('run.py', MANAGER_SHA), ('bootstrap-manifest.json', MANIFEST_SHA)]:
        p = HERE / name
        if not stat.S_ISREG(p.lstat().st_mode) or sha(p) != expected:
            raise RuntimeError('frozen R4 source binding changed: ' + name)
    spec = importlib.util.spec_from_file_location('reviewed_runtime_boundary', HERE / 'run.py')
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    result.trusted()  # Verifies transitive helpers/profile before their import.
    return result


def child(request_path):
    request = json.loads(request_path.read_text())
    result = {}
    for name in ('net', 'mnt', 'pid', 'user'):
        current = os.readlink('/proc/self/ns/' + name)
        if current == request['host_namespaces'][name]:
            raise RuntimeError('namespace not isolated: ' + name)
        result[name + '_isolated'] = True
    for family in (socket.AF_INET, socket.AF_INET6, socket.AF_UNIX):
        try:
            s = socket.socket(family, socket.SOCK_STREAM)
        except OSError as e:
            if e.errno != errno.EPERM:
                raise
        else:
            s.close()
            raise RuntimeError('seccomp socket prohibition ineffective')
    result['all_socket_families_eperm'] = True
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(0x00020000) != -1 or ctypes.get_errno() != errno.EPERM:
        raise RuntimeError('namespace escape denial ineffective')
    result['unshare_eperm'] = True
    for path in ('/source-readonly/escape', request['group'] + '/memory.max'):
        try:
            with open(path, 'w') as f:
                f.write('unreachable')
        except OSError as e:
            if e.errno != errno.EROFS:
                raise
        else:
            raise RuntimeError('readonly bind was writable')
    result['source_and_cgroup_readonly'] = True
    for path in ('/run', '/home', '/Users', '/var/run/docker.sock'):
        if os.path.exists(path):
            raise RuntimeError('host service/home mount exposed')
    result['host_service_and_home_mounts_absent'] = True
    for name in os.listdir('/proc/self/fd'):
        try:
            target = os.readlink('/proc/self/fd/' + name)
        except FileNotFoundError:
            continue
        if int(name) > 2 or target.startswith('socket:'):
            raise RuntimeError('uncontrolled inherited descriptor')
    result['only_standard_descriptors'] = True
    for name, expected in request['limits'].items():
        if (Path(request['group']) / name).read_text().strip() != expected:
            raise RuntimeError('worker cgroup limit differs: ' + name)
    result['kernel_limits'] = request['limits']
    pid = os.fork()
    if pid == 0:
        try:
            socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        except OSError as e:
            os._exit(0 if e.errno == errno.EPERM else 3)
        os._exit(4)
    if os.waitpid(pid, 0)[1] != 0:
        raise RuntimeError('descendant did not inherit socket filter')
    result['descendant_filter_and_reap'] = True
    write(Path('/out/child-proof.json'), result)


def supplied_bubblewrap(binary, receipt, manager):
    root = binary.parent
    info = root.lstat()
    if (binary.name != 'bwrap' or receipt != root/'bubblewrap-supplier-receipt.json' or
            not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or
            stat.S_IMODE(info.st_mode) != 0o700 or
            root.parent.resolve() != Path(os.environ['RUNNER_TEMP']).resolve() or
            set(p.name for p in root.iterdir()) != {'bwrap', 'bubblewrap.deb', receipt.name}):
        raise RuntimeError('supplier must be one owned temporary prerequisite file set')
    for path in (binary, receipt, root/'bubblewrap.deb'):
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or
                info.st_mode & (0o077 | stat.S_ISUID | stat.S_ISGID) or info.st_nlink != 1 or
                info.st_size > (65536 if path == receipt else 1024 * 1024)):
            raise RuntimeError('supplier input must be a private owned regular file')
    if receipt.stat().st_size > 65536:
        raise RuntimeError('supplier receipt exceeds finite bound')
    supplied = manager.load(receipt)
    if (supplied.get('schema') != 'workflow.offline-scanner.bubblewrap-supplier.v1' or
            'failure' in supplied or 'cleanup_failure' in supplied or
            supplied.get('supplied') is not True or supplied.get('installation_admitted') is not False or
            supplied.get('package_hooks_executed') is not False or
            supplied.get('policy_settings_changed') is not False or
            supplied.get('identity') != {k: os.environ.get(k, '') for k in IDENTITY} or
            supplied.get('binary_path') != str(binary) or
            sha(binary) != supplied.get('binary_sha256') or
            sha(root/'bubblewrap.deb') != supplied.get('package', {}).get('sha256')):
        raise RuntimeError('current-run supplier identity/bytes mismatch')
    return supplied, sha(receipt)


def prove(output, bubblewrap=None, supplier_receipt=None):
    record = {'schema': 'workflow.offline-scanner.runtime-prerequisite-proof.v1',
              'accepted': False, 'profile_sha256': PROFILE_SHA,
              'candidate_execution': False, 'scanner_execution': False,
              'runtime_boundary_pass': False, 'installation_admitted': False,
              'identity': {k: os.environ.get(k, '') for k in IDENTITY},
              'recorded_at': datetime.now(timezone.utc).isoformat()}
    started = time.monotonic()
    group = parent = None
    m = None
    stage = 'platform'
    tools = {}
    code = 2
    try:
        if platform.system() != 'Linux' or platform.machine() != 'x86_64' or sys.version_info < (3, 11):
            raise RuntimeError('actual Linux/x86_64 Python>=3.11 required')
        stage = 'frozen-source-verification'
        m = manager()
        _, _, _, Budget = m.trusted()
        budget = Budget(120)
        record['manager_sha256'] = MANAGER_SHA
        record['launcher_sha256'] = sha(Path(__file__))
        record['kernel'] = platform.release()
        record['python_version'] = platform.python_version()
        bubblewrap = Path('/usr/bin/bwrap') if bubblewrap is None else bubblewrap
        if supplier_receipt is not None:
            if os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted':
                raise RuntimeError('temporary supplier is restricted to ephemeral GitHub-hosted runners')
            stage = 'authenticated-owned-supplier'
            supplied, receipt_sha = supplied_bubblewrap(bubblewrap, supplier_receipt, m)
            record['supplier_receipt_sha256'] = receipt_sha
            record['package'] = supplied['package']
        elif bubblewrap != Path('/usr/bin/bwrap'):
            raise RuntimeError('arbitrary runtime tool path is forbidden')
        stage = 'existing-tools'
        for name, path in [('bubblewrap', bubblewrap), ('python', Path('/usr/bin/python3'))]:
            actual = path.resolve(strict=True)
            if not stat.S_ISREG(actual.stat().st_mode):
                raise RuntimeError('existing tool is not a regular file: ' + str(path))
            tools[name] = {'path': str(path), 'resolved_path': str(actual), 'sha256': sha(actual)}
        record['tools'] = tools
        stage = 'existing-cgroup-delegation'
        rows = Path('/proc/self/cgroup').read_text().splitlines()
        unified = [line[3:] for line in rows if line.startswith('0::/')]
        if len(unified) != 1 or '..' in Path(unified[0]).parts:
            raise RuntimeError('one existing cgroup-v2 membership required')
        current = Path('/sys/fs/cgroup') / unified[0].lstrip('/')
        record['requested_delegated_parent'] = str(current)
        # Uses exactly R4's existing-parent admission; no sudo, controller
        # activation, chmod, parent migration, installation or fallback.
        group, parent = m.enter_group({'delegated_cgroup_parent': str(current)})
        stage = 'kernel-quotas'
        limits = {'memory.max': '2147483648', 'memory.swap.max': '0', 'pids.max': '256'}
        for name, value in limits.items():
            (group / name).write_text(value)
            if (group / name).read_text().strip() != value:
                raise RuntimeError('kernel quota readback differs')
        (group / 'cgroup.procs').write_text(str(os.getpid()))
        readonly = output / 'readonly'
        readonly.mkdir(mode=0o700)
        writable = output / 'worker-output'
        writable.mkdir(mode=0o700)
        write(readonly / 'request.json', {'group': str(group), 'limits': limits,
              'host_namespaces': {name: os.readlink('/proc/self/ns/' + name)
                                  for name in ('net', 'mnt', 'pid', 'user')}})
        filter_path = output / 'filter.bpf'
        with filter_path.open('xb') as f:
            os.fchmod(f.fileno(), 0o600)
            f.write(m.socket_filter())
        stage = 'exact-r4-kernel-boundary'
        args = [str(bubblewrap), '--unshare-all', '--die-with-parent', '--new-session',
                '--cap-drop', 'ALL', '--clearenv']
        for path in ('/usr', '/lib', '/lib64'):
            if not Path(path).exists():
                raise RuntimeError('required runtime mount absent: ' + path)
            args += ['--ro-bind', path, path]
        args += ['--ro-bind', str(Path(__file__).resolve()), '/proof/prove-runtime.py',
                 '--ro-bind', str(readonly), '/source-readonly',
                 '--ro-bind', str(group), str(group), '--bind', str(writable), '/out',
                 '--proc', '/proc', '--dev', '/dev', '--chdir', '/proof']
        with filter_path.open('rb') as f:
            args += ['--seccomp', str(f.fileno()), '--', '/usr/bin/python3',
                     '-I', '-S', '-B', '-X', 'pycache_prefix=/out/absent-worker-cache',
                     '/proof/prove-runtime.py', '--worker', '/source-readonly/request.json']
            rc, stdout, stderr = m.capture_worker(args, HERE,
                {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C'}, f.fileno(), budget, 60)
        record['child_exit'] = rc
        record['stdout_sha256'] = hashlib.sha256(stdout).hexdigest()
        record['stderr_sha256'] = hashlib.sha256(stderr).hexdigest()
        # Only fixed probe code emits these streams; limit raw retention further.
        if len(stdout) > 65536 or len(stderr) > 65536:
            raise RuntimeError('probe stream exceeds 64KiB')
        for name, raw in [('launcher.stdout', stdout), ('launcher.stderr', stderr)]:
            with (output / name).open('xb') as f:
                os.fchmod(f.fileno(), 0o600)
                f.write(raw)
        if rc != 0:
            raise RuntimeError('actual R4 kernel boundary probe failed; inspect bounded stderr')
        record['worker'] = json.loads((writable / 'child-proof.json').read_text())
        stage = 'owned-cleanup-and-recheck'
        m.release_group(group, parent)
        group = None
        m.trusted()
        for row in tools.values():
            if sha(Path(row['resolved_path'])) != row['sha256']:
                raise RuntimeError('runtime tool changed')
        if supplier_receipt is not None:
            _, after_sha = supplied_bubblewrap(bubblewrap, supplier_receipt, m)
            if after_sha != receipt_sha:
                raise RuntimeError('supplier receipt changed')
        budget.left()
        record['runtime_boundary_pass'] = True
        code = 0
    except Exception as e:
        record['failure'] = {'stage': stage, 'type': type(e).__name__,
                             'message': str(e)[:2048], 'errno': getattr(e, 'errno', None)}
    finally:
        if group is not None:
            try:
                m.release_group(group, parent)
            except Exception as e:
                record['cleanup_failure'] = {'type': type(e).__name__, 'message': str(e)[:2048]}
                record['runtime_boundary_pass'] = False
                code = 2
        record['elapsed_seconds'] = round(time.monotonic() - started, 3)
        # A kernel proof is only one prerequisite. It deliberately cannot pass
        # R4's five-receipt installation admission, even if every probe passed.
        write(output / 'runtime-prerequisite-proof.json', record)
    return code


def interrupted(signum, frame):
    raise RuntimeError('finite probe interrupted')


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        child(Path(sys.argv[2]))
    else:
        import argparse
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument('output', type=Path)
        parser.add_argument('--bubblewrap', type=Path)
        parser.add_argument('--supplier-receipt', type=Path)
        args = parser.parse_args()
        if bool(args.bubblewrap) != bool(args.supplier_receipt):
            parser.error('owned binary and authenticated supplier receipt must be supplied together')
        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        sys.exit(prove(owned_output(args.output), args.bubblewrap, args.supplier_receipt))
