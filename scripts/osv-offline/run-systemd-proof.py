"""Finite prerequisite launcher; --execute requires separate owner authorization.

Only this fixed launcher requests one transient unit. It never executes a scanner,
installs a package, changes an ancestor controller or changes OS policy.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import signal
import stat
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent
PROOF_SHA = '9ce3a0db7df0a6df5010ccde48208e1b1f6a852f0666203706973dc9205cbcd3'
BASE_ENV = {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C'}
PROPERTIES = {
    'Delegate': 'memory pids', 'DelegateSubgroup': 'supervisor',
    'MemoryMax': '2147483648', 'MemorySwapMax': '0', 'TasksMax': '256',
    'CPUAccounting': 'no', 'IOAccounting': 'no', 'BlockIOAccounting': 'no',
    'IPAccounting': 'no', 'MemoryAccounting': 'yes', 'TasksAccounting': 'yes',
    'RuntimeMaxSec': '150s', 'TimeoutStartSec': '15s', 'TimeoutStopSec': '5s',
    'KillMode': 'control-group', 'SendSIGKILL': 'yes', 'Restart': 'no',
    'NoNewPrivileges': 'yes', 'UMask': '0077',
}


def proof_module():
    path = HERE / 'prove-runtime.py'
    if (not stat.S_ISREG(path.lstat().st_mode) or path.is_symlink() or
            hashlib.sha256(path.read_bytes()).hexdigest() != PROOF_SHA):
        raise RuntimeError('reviewed proof source binding changed')
    spec = importlib.util.spec_from_file_location('reviewed_prerequisite_proof', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def controller_bits():
    result = {}
    for path in ('/sys/fs/cgroup', '/sys/fs/cgroup/system.slice'):
        text = (Path(path) / 'cgroup.subtree_control').read_text()
        bits = text.split()
        if len(text) > 4096 or len(bits) > 64 or len(set(bits)) != len(bits) or any(
                not re.fullmatch(r'[a-z][a-z0-9_]{0,31}', bit) for bit in bits):
            raise RuntimeError('unknown ancestor controller bitset')
        if not {'memory', 'pids'} <= set(bits):
            raise RuntimeError('approved ancestor controllers not already enabled')
        result[path] = sorted(bits)
    return result


def service_argv(unit, checkout, output, startup, binary, receipt, identity):
    if os.getuid() == 0 or os.getuid() != os.geteuid() or os.getgid() != os.getegid():
        raise RuntimeError('proof must be launched by the actual unprivileged runner')
    argv = ['/usr/bin/sudo', '-n', '/usr/bin/systemd-run', '--unit=' + unit,
            '--service-type=exec', '--uid=' + str(os.getuid()), '--gid=' + str(os.getgid()),
            '--slice=system.slice', '--wait', '--pipe', '--collect', '--quiet',
            '--expand-environment=no', '--working-directory=' + str(checkout)]
    argv += ['--property=' + key + '=' + value for key, value in PROPERTIES.items()]
    child_env = {**BASE_ENV, 'RUNNER_TEMP': os.environ['RUNNER_TEMP'], **identity,
                 'GITHUB_REF': os.environ['GITHUB_REF'], 'WF_OSV_UNIT': unit}
    argv += ['/usr/bin/env', '-i'] + [key + '=' + value for key, value in child_env.items()]
    argv += ['/usr/bin/python3', '-I', '-S', '-B', '-X',
             'pycache_prefix=' + str(startup / 'absent-cache'),
             str(HERE / 'prove-runtime.py'), str(output), '--bubblewrap', str(binary),
             '--supplier-receipt', str(receipt)]
    return argv


def namespace_argv(binary):
    return [str(binary), '--unshare-all', '--die-with-parent', '--new-session',
            '--cap-drop', 'ALL', '--clearenv', '--ro-bind', '/usr', '/usr',
            '--ro-bind', '/lib', '/lib', '--ro-bind', '/lib64', '/lib64',
            '--proc', '/proc', '--dev', '/dev', '--', '/usr/bin/true']


def owned_attestation(output, proof, identity):
    path = output / 'unit-attestation.json'
    info = path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or
            stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size > 65536):
        raise RuntimeError('unit attestation is not one bounded private file')
    value = json.loads(path.read_text())
    if (value.get('schema') != 'workflow.offline-scanner.unit-attestation.v1' or
            value.get('identity') != identity or value.get('github_ref') != os.environ['GITHUB_REF'] or
            value.get('manager_sha256') != proof.MANAGER_SHA or value.get('launcher_sha256') != PROOF_SHA):
        raise RuntimeError('unit attestation is not this exact proof invocation')
    binding = value['delegation']
    root = Path(binding['root'])
    expected_unit = 'wf-osv-proof-' + identity['GITHUB_RUN_ID'] + '-' + identity['GITHUB_RUN_ATTEMPT'] + '.service'
    if (not root.is_absolute() or '..' in root.parts or
            root.parent != Path('/sys/fs/cgroup/system.slice') or
            root.name != expected_unit or binding.get('unit') != expected_unit or
            binding['supervisor'] != str(root / 'supervisor')):
        raise RuntimeError('unit attestation has an unbound control path')
    return binding


def cleanup(unit, output, proof, identity, cg, bounded_run, Budget):
    binding = owned_attestation(output, proof, identity)
    current = cg.unit_status(unit)
    if current['load_state'] != 'not-found':
        props = binding['properties']
        if current['control_group'] != '/' + str(Path(binding['root']).relative_to('/sys/fs/cgroup')):
            raise RuntimeError('cleanup unit control group changed')
        if (current['invocation_id'] != props['InvocationID'] or
                current['user'] != str(os.getuid()) or current['group'] != str(os.getgid()) or
                int(current['main_pid']) not in (0, int(props['MainPID']))):
            raise RuntimeError('cleanup unit instance or owner changed')
        stopped = bounded_run(['/usr/bin/sudo', '-n', '/usr/bin/systemctl', 'stop', '--', unit],
                              HERE, BASE_ENV, 10, Budget(10), stdout_limit=65536, stderr_limit=65536)
        if stopped.returncode != 0:
            raise RuntimeError('owned transient unit stop failed')
        current = cg.unit_status(unit)
    if current['load_state'] != 'not-found' and current['active_state'] not in ('inactive', 'failed'):
        raise RuntimeError('owned transient unit is still active')
    root = Path(binding['root'])
    if root.exists():
        if root.is_symlink():
            raise RuntimeError('owned unit control path became an alias')
        # Inspect only the captured owned subtree; never signal a reaped PID or
        # mutate ancestors. An unexpected child prevents successful cleanup.
        queue = [root]
        checked = 0
        deadline = time.monotonic() + 5
        while queue:
            current_group = queue.pop()
            checked += 1
            if checked > 256 or time.monotonic() >= deadline:
                raise RuntimeError('owned subtree exceeds bounded empty readback')
            if current_group.is_symlink() or (current_group / 'cgroup.procs').read_text().splitlines():
                raise RuntimeError('owned subtree still contains processes or aliases')
            for child in current_group.iterdir():
                if child.is_symlink():
                    raise RuntimeError('owned subtree contains an alias')
                if child.is_dir():
                    queue.append(child)
    return {'unit_inactive_or_absent': True, 'captured_subtree_absent_or_empty': True,
            'captured_root': str(root)}


def run(output, binary, receipt, execute=False):
    record = {'schema': 'workflow.offline-scanner.systemd-prerequisite.v1',
              'accepted': False, 'runtime_boundary_pass': False,
              'scanner_execution': False, 'candidate_execution': False,
              'installation_admitted': False, 'root_submission_attempted': False}
    code = 2
    proof = cg = budget = None
    submitted = False
    cleanup_attempted = False
    output_owned = False
    started = time.monotonic()
    stage = 'explicit-execution-gate'
    try:
        if not execute:
            raise RuntimeError('separately authorized --execute is required')
        if platform.system() != 'Linux' or platform.machine() != 'x86_64':
            raise RuntimeError('same ephemeral Ubuntu24/x86_64 runner required')
        stage = 'reviewed-source'
        proof = proof_module()
        proof.owned_output(output)
        output_owned = True
        m = proof.manager()
        m.close_input()
        _, _, bounded_run, Budget = m.trusted()
        budget = Budget(230, 1024 * 1024)
        cg = m.cgroup_helpers()
        unit = cg.unit_name()
        expected_properties = {'Type': 'exec', 'Slice': 'system.slice',
            'User': str(os.getuid()), 'Group': str(os.getgid()),
            'CollectMode': 'inactive-or-failed', **PROPERTIES}
        if cg.service_properties() != expected_properties:
            raise RuntimeError('reviewed helper and submission properties differ')
        identity = {key: os.environ.get(key, '') for key in proof.IDENTITY}
        record.update(identity=identity, github_ref=os.environ['GITHUB_REF'], unit=unit)
        stage = 'authenticated-supplier'
        _, supplied_sha = proof.supplied_bubblewrap(binary, receipt, m)
        record['supplier_receipt_sha256'] = supplied_sha
        stage = 'exact-source-commit'
        git = bounded_run(['/usr/bin/git', '--no-optional-locks', 'rev-parse', '--verify', 'HEAD'],
                          HERE.parents[1], {**BASE_ENV, 'GIT_CONFIG_GLOBAL': '/dev/null',
                          'GIT_CONFIG_SYSTEM': '/dev/null'}, 5, budget,
                          stdout_limit=65536, stderr_limit=65536)
        if (git.returncode != 0 or not re.fullmatch(r'[0-9a-f]{40}', identity['GITHUB_SHA']) or
                git.stdout.strip() != identity['GITHUB_SHA']):
            raise RuntimeError('actual checkout differs from current dispatched commit')
        stage = 'read-only-preflight'
        before = cg.preflight()
        ancestor_bits = controller_bits()
        record['preflight'] = before
        record['ancestor_bits_before'] = ancestor_bits
        if len(json.dumps(record).encode()) > 32768:
            raise RuntimeError('preflight receipt exceeds its reserved finite output budget')
        stage = 'unprivileged-namespace-eligibility'
        eligibility = bounded_run(namespace_argv(binary), HERE, BASE_ENV, 10, budget,
                                  stdout_limit=65536, stderr_limit=65536)
        record['namespace_eligibility'] = {'exit': eligibility.returncode,
            'stdout_sha256': hashlib.sha256(eligibility.stdout.encode()).hexdigest(),
            'stderr_sha256': hashlib.sha256(eligibility.stderr.encode()).hexdigest(),
            'stderr': eligibility.stderr[:2048]}
        if eligibility.returncode != 0:
            raise RuntimeError('existing namespace policy denies eligibility; no policy fallback')
        startup = Path(tempfile.mkdtemp(prefix='osv-service-startup-', dir=os.environ['RUNNER_TEMP']))
        startup.chmod(0o700)
        argv = service_argv(unit, HERE.parents[1], output, startup, binary, receipt, identity)
        stage = 'final-pre-submission-recheck'
        if cg.preflight() != before or controller_bits() != ancestor_bits:
            raise RuntimeError('read-only preflight changed before privileged submission')
        proof.manager()
        _, after_supplied_sha = proof.supplied_bubblewrap(binary, receipt, m)
        if after_supplied_sha != supplied_sha:
            raise RuntimeError('supplier changed before privileged submission')
        budget.left()
        stage = 'one-transient-proof-service'
        submitted = True
        record['root_submission_attempted'] = True
        timed_argv = ['/usr/bin/timeout', '--signal=TERM', '--kill-after=10s', '180s'] + argv
        actual = bounded_run(timed_argv, HERE.parents[1], BASE_ENV, 190, budget,
                             stdout_limit=65536, stderr_limit=65536)
        record['actual_service_exit'] = actual.returncode
        if actual.returncode != 0:
            raise RuntimeError('actual transient proof service did not succeed')
        inner = m.load(output / 'runtime-prerequisite-proof.json')
        if (inner.get('schema') != 'workflow.offline-scanner.runtime-prerequisite-proof.v2' or
                'failure' in inner or 'cleanup_failure' in inner or inner.get('identity') != identity or
                inner.get('runtime_boundary_pass') is not True or inner.get('accepted') is not False or
                inner.get('scanner_execution') is not False or inner.get('candidate_execution') is not False or
                inner.get('installation_admitted') is not False or inner.get('manager_sha256') != proof.MANAGER_SHA or
                inner.get('launcher_sha256') != PROOF_SHA or inner.get('supplier_receipt_sha256') != supplied_sha):
            raise RuntimeError('actual inner receipt contradicts this prerequisite invocation')
        record['runtime_receipt_sha256'] = proof.sha(output / 'runtime-prerequisite-proof.json')
        if inner.get('delegation') != owned_attestation(output, proof, identity):
            raise RuntimeError('runtime receipt differs from the captured unit attestation')
        stage = 'owned-unit-cleanup'
        cleanup_attempted = True
        record['cleanup'] = cleanup(unit, output, proof, identity, cg, bounded_run, Budget)
        submitted = False
        if controller_bits() != ancestor_bits:
            raise RuntimeError('ancestor controller bits changed; no restore is authorized')
        proof.manager()
        _, final_supplied_sha = proof.supplied_bubblewrap(binary, receipt, m)
        if final_supplied_sha != supplied_sha:
            raise RuntimeError('supplier changed after actual proof')
        budget.left()
        record['runtime_boundary_pass'] = True
        code = 0
    except Exception as error:
        record['runtime_boundary_pass'] = False
        record['failure'] = {'stage': stage, 'type': type(error).__name__,
                             'message': str(error)[:2048], 'errno': getattr(error, 'errno', None)}
    finally:
        if submitted and not cleanup_attempted:
            try:
                record['cleanup'] = cleanup(unit, output, proof, identity, cg, bounded_run, Budget)
            except Exception as error:
                record['cleanup_failure'] = {'type': type(error).__name__, 'message': str(error)[:2048]}
            record['runtime_boundary_pass'] = False
            code = 2
        record['elapsed_seconds'] = round(time.monotonic() - started, 3)
        if proof is not None and output_owned:
            proof.write(output / 'systemd-prerequisite-proof.json', record)
            if budget is not None:
                try:
                    budget.left()
                except Exception:
                    code = 2
    return code


def interrupted(signum, frame):
    raise RuntimeError('finite launcher interrupted')


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    parser.add_argument('--bubblewrap', type=Path, required=True)
    parser.add_argument('--supplier-receipt', type=Path, required=True)
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    sys.exit(run(args.output, args.bubblewrap, args.supplier_receipt, args.execute))
