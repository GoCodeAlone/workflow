"""Offline authority/receipt regressions: no real systemd, sudo or namespace call."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('launcher', Path(__file__).with_name('run-systemd-proof.py'))
launcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launcher)


class Budget:
    def __init__(self, *args):
        self.expired = False

    def left(self):
        if self.expired:
            raise RuntimeError('deadline')
        return 200


class LauncherTests(unittest.TestCase):
    def fixture(self, output, *, changed=False, namespace_exit=0, service_exit=0,
                contradiction=False, late=False, property_mismatch=False, delegation_mismatch=False):
        identity = {key: os.environ.get(key, '') for key in ('GITHUB_SHA', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT')}
        cg = SimpleNamespace(unit_name=lambda: 'wf-osv-proof-123-1.service',
                             preflight=Mock(side_effect=[{'snapshot': 'one'}, {'snapshot': 'changed' if changed else 'one'}]))
        properties = {'Type': 'exec', 'Slice': 'system.slice', 'User': '501',
                      'Group': str(os.getgid()), 'CollectMode': 'inactive-or-failed', **launcher.PROPERTIES}
        if property_mismatch:
            properties['BlockIOAccounting'] = 'yes'
        cg.service_properties = lambda: properties
        calls = []
        manager = SimpleNamespace(load=lambda path: json.loads(path.read_text()), cgroup_helpers=lambda: cg,
                                  close_input=Mock())
        receipt = {'schema': 'workflow.offline-scanner.runtime-prerequisite-proof.v2',
                   'identity': identity, 'runtime_boundary_pass': not contradiction,
                   'accepted': False, 'scanner_execution': False, 'candidate_execution': False,
                   'installation_admitted': False, 'manager_sha256': 'manager-pin',
                   'launcher_sha256': launcher.PROOF_SHA, 'supplier_receipt_sha256': 'supplier-pin',
                   'delegation': {'unit': 'wf-osv-proof-other.service' if delegation_mismatch else 'wf-osv-proof-123-1.service'}}

        def run(argv, *args, **kwargs):
            manager.close_input.assert_called_once_with()
            calls.append(argv)
            if argv[0] == '/usr/bin/git':
                return subprocess.CompletedProcess(argv, 0, identity['GITHUB_SHA'] + '\n', '')
            if '/usr/bin/sudo' in argv:
                (output / 'runtime-prerequisite-proof.json').write_text(json.dumps(receipt))
                return subprocess.CompletedProcess(argv, service_exit, '', '')
            return subprocess.CompletedProcess(argv, namespace_exit, '', 'policy denied' if namespace_exit else '')

        budgets = []

        def budget_factory(*args):
            value = Budget(); budgets.append(value); return value

        def write(path, value):
            with path.open('x') as file:
                file.write(json.dumps(value))
            if late:
                budgets[0].expired = True

        manager.trusted = lambda: (None, None, run, budget_factory)
        proof = SimpleNamespace(IDENTITY=tuple(identity), MANAGER_SHA='manager-pin',
            owned_output=lambda p: p, manager=lambda: manager,
            supplied_bubblewrap=lambda *args: ({}, 'supplier-pin'), write=write,
            sha=lambda p: hashlib.sha256(p.read_bytes()).hexdigest())
        return proof, calls

    def invoke(self, **options):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'output'; output.mkdir(mode=0o700)
            environment = {'RUNNER_TEMP': temp, 'GITHUB_SHA': 'a' * 40,
                           'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1',
                           'GITHUB_REF': 'refs/heads/prep/osv-prerequisites-20261011-r1'}
            with patch.dict(os.environ, environment):
                proof, calls = self.fixture(output, **options)
                with patch.object(launcher.platform, 'system', return_value='Linux'), \
                     patch.object(launcher.platform, 'machine', return_value='x86_64'), \
                     patch.object(launcher, 'proof_module', return_value=proof), \
                     patch.object(launcher, 'controller_bits', return_value={'root': ['memory', 'pids']}), \
                     patch.object(launcher, 'cleanup', return_value={'owned_cleanup': True}), \
                     patch.object(launcher, 'owned_attestation', return_value={'unit': 'wf-osv-proof-123-1.service'}), \
                     patch.object(launcher.os, 'getuid', return_value=501), \
                     patch.object(launcher.os, 'geteuid', return_value=501):
                    code = launcher.run(output, Path(temp)/'bwrap', Path(temp)/'supplier.json', True)
                receipt = json.loads((output/'systemd-prerequisite-proof.json').read_text())
                return code, receipt, calls

    def test_disabled_or_nonlinux_gate_never_loads_source_or_requests_root(self):
        with patch.object(launcher, 'proof_module', side_effect=AssertionError('inert gate')):
            self.assertEqual(launcher.run(Path('/unused'), Path('/unused'), Path('/unused')), 2)
            with patch.object(launcher.platform, 'system', return_value='Darwin'):
                self.assertEqual(launcher.run(Path('/unused'), Path('/unused'), Path('/unused'), True), 2)

    def test_changed_snapshot_refuses_before_sudo(self):
        code, receipt, calls = self.invoke(changed=True)
        self.assertEqual(code, 2)
        self.assertFalse(receipt['root_submission_attempted'])
        self.assertFalse(any('/usr/bin/sudo' in call for call in calls))

    def test_namespace_denial_never_requests_root_or_relaxes_policy(self):
        code, receipt, calls = self.invoke(namespace_exit=1)
        self.assertEqual(code, 2)
        self.assertFalse(receipt['root_submission_attempted'])
        self.assertIn('policy denied', receipt['namespace_eligibility']['stderr'])
        self.assertFalse(any('/usr/bin/sudo' in call for call in calls))

    def test_helper_submission_property_mismatch_refuses_before_root(self):
        code, receipt, calls = self.invoke(property_mismatch=True)
        self.assertEqual(code, 2)
        self.assertFalse(receipt['root_submission_attempted'])
        self.assertFalse(any('/usr/bin/sudo' in call for call in calls))

    def test_success_still_never_admits_scanner_or_installation(self):
        code, receipt, calls = self.invoke()
        self.assertEqual(code, 0)
        self.assertTrue(receipt['runtime_boundary_pass'])
        for key in ('accepted', 'scanner_execution', 'candidate_execution', 'installation_admitted'):
            self.assertFalse(receipt[key])
        root_calls = [call for call in calls if '/usr/bin/sudo' in call]
        self.assertEqual(len(root_calls), 1)
        argv = root_calls[0]
        self.assertEqual(argv[:4], ['/usr/bin/timeout', '--signal=TERM', '--kill-after=10s', '180s'])
        self.assertIn('--property=IOAccounting=no', argv)
        self.assertIn('--property=BlockIOAccounting=no', argv)
        self.assertIn('--property=MemoryMax=2147483648', argv)
        self.assertIn('--property=RuntimeMaxSec=150s', argv)
        self.assertNotIn('/bin/sh', argv)

    def test_actual_service_failure_and_receipt_contradiction_veto_success(self):
        for options in ({'service_exit': 2}, {'contradiction': True}, {'delegation_mismatch': True}):
            code, receipt, _ = self.invoke(**options)
            self.assertEqual(code, 2)
            self.assertFalse(receipt['runtime_boundary_pass'])
            self.assertIn('failure', receipt)

    def test_receipt_close_deadline_vetoes_actual_step_success(self):
        code, receipt, _ = self.invoke(late=True)
        self.assertEqual(code, 2)
        self.assertTrue(receipt['runtime_boundary_pass'])  # provisional bytes cannot authorize the failed step

    def test_changed_unit_instance_cannot_be_stopped(self):
        binding = {'root': '/sys/fs/cgroup/system.slice/wf-osv-proof-123-1.service',
                   'properties': {'InvocationID': 'a'*32, 'MainPID': 77}}
        cg = SimpleNamespace(unit_status=lambda unit: {'load_state': 'loaded', 'active_state': 'active',
             'control_group': '/system.slice/wf-osv-proof-123-1.service', 'invocation_id': 'b'*32,
             'main_pid': '77', 'user': str(os.getuid()), 'group': str(os.getgid())})
        command = Mock(side_effect=AssertionError('must not stop a different instance'))
        with patch.object(launcher, 'owned_attestation', return_value=binding):
            with self.assertRaisesRegex(RuntimeError, 'instance or owner changed'):
                launcher.cleanup('wf-osv-proof-123-1.service', Path('/unused'), None, {}, cg, command, Budget)
        command.assert_not_called()

    def test_owned_cleanup_accepts_string_pid_and_stops_only_captured_unit(self):
        unit = 'wf-osv-proof-123-1.service'
        binding = {'root': '/sys/fs/cgroup/system.slice/' + unit,
                   'properties': {'InvocationID': 'a'*32, 'MainPID': '77'}}
        statuses = [{'load_state': 'loaded', 'active_state': 'active',
            'control_group': '/system.slice/' + unit, 'invocation_id': 'a'*32,
            'main_pid': '77', 'user': str(os.getuid()), 'group': str(os.getgid())},
            {'load_state': 'not-found', 'active_state': 'inactive'}]
        cg = SimpleNamespace(unit_status=Mock(side_effect=statuses))
        command = Mock(return_value=subprocess.CompletedProcess([], 0, '', ''))
        with patch.object(launcher, 'owned_attestation', return_value=binding), \
                patch.object(Path, 'exists', return_value=False):
            result = launcher.cleanup(unit, Path('/unused'), None, {}, cg, command, Budget)
        self.assertTrue(result['captured_subtree_absent_or_empty'])
        self.assertEqual(command.call_args.args[0], ['/usr/bin/sudo', '-n', '/usr/bin/systemctl', 'stop', '--', unit])
        self.assertEqual(command.call_count, 1)


if __name__ == '__main__':
    unittest.main()
