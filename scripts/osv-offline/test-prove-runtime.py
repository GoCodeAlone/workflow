import importlib.util
import json
import os
from pathlib import Path
import subprocess
import stat
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('proof', Path(__file__).with_name('prove-runtime.py'))
proof = importlib.util.module_from_spec(spec)
spec.loader.exec_module(proof)
manager_spec = importlib.util.spec_from_file_location('runtime_manager', Path(__file__).with_name('run.py'))
manager = importlib.util.module_from_spec(manager_spec)
manager_spec.loader.exec_module(manager)


class RuntimeProofTests(unittest.TestCase):
    def test_rejects_output_alias_and_nonempty_directory(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            output = root / 'proof'
            output.mkdir(mode=0o700)
            alias = root / 'alias'
            alias.symlink_to(output, target_is_directory=True)
            with patch.dict(os.environ, {'RUNNER_TEMP': temp}):
                with self.assertRaises(RuntimeError):
                    proof.owned_output(alias)
                self.assertEqual(proof.owned_output(output), output)
                (output / 'stale').write_text('stale proof')
                with self.assertRaises(RuntimeError):
                    proof.owned_output(output)

    def test_nonlinux_failure_never_calls_manager_or_admits_installation(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            with patch.object(proof.platform, 'system', return_value='Darwin'), \
                    patch.object(proof, 'manager', side_effect=AssertionError('must remain inert')):
                self.assertEqual(proof.prove(output), 2)
            receipt = json.loads((output / 'runtime-prerequisite-proof.json').read_text())
            self.assertFalse(receipt['accepted'])
            self.assertFalse(receipt['installation_admitted'])
            self.assertFalse(receipt['runtime_boundary_pass'])
            self.assertEqual(receipt['failure']['stage'], 'platform')
            self.assertNotIn('tools', receipt)

    def test_frozen_manager_drift_rejected_before_import(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'run.py').write_text('raise AssertionError("must never execute")')
            with patch.object(proof, 'HERE', root):
                with self.assertRaisesRegex(RuntimeError, 'reviewed runtime source binding changed'):
                    proof.manager()

    def test_receipt_cannot_replace_prior_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'receipt'
            proof.write(path, {'accepted': False})
            with self.assertRaises(FileExistsError):
                proof.write(path, {'accepted': True})
            self.assertEqual(json.loads(path.read_text()), {'accepted': False})

    def test_actual_bootstrap_pins_evaluate_as_shell_assignments(self):
        lines = Path(__file__).with_name('bootstrap.sh').read_text().splitlines()
        assignments = []
        for key, target in [('runner_sha256', 'run.py'), ('manifest_sha256', 'bootstrap-manifest.json')]:
            matches = [line for line in lines if line.startswith(key)]
            self.assertEqual(len(matches), 1)
            self.assertRegex(matches[0], '^' + key + "='[0-9a-f]{64}'$")
            assignments.append(matches[0])
        script = 'set -eu\n' + '\n'.join(assignments) + '\n' + 'printf "%s\\n%s\\n" "$runner_sha256" "$manifest_sha256"\n'
        result = subprocess.run(['/bin/bash', '--noprofile', '--norc', '-c', script],
            env={'PATH':'/usr/bin:/bin', 'LANG':'C', 'LC_ALL':'C'}, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=3, check=True)
        self.assertEqual(result.stdout.splitlines(), [proof.MANAGER_SHA, proof.MANIFEST_SHA])

    def test_closed_input_reaches_spawned_reader_despite_hostile_inherited_stdin(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(__file__).with_name('run.py').resolve()
            code = ('import importlib.util,subprocess,sys\n'
                's=importlib.util.spec_from_file_location("m",' + repr(str(path)) + ')\n'
                'm=importlib.util.module_from_spec(s);s.loader.exec_module(m)\n'
                'm.close_input()\n'
                'r=subprocess.run([sys.executable,"-I","-S","-B","-c",'
                '"import os;print(len(os.read(0,4096)))"],capture_output=True,check=True,timeout=3)\n'
                'sys.stdout.buffer.write(r.stdout)\n')
            result = subprocess.run([sys.executable, '-I', '-S', '-B', '-X',
                'pycache_prefix=' + str(Path(temp)/'absent-cache'), '-c', code],
                env={'PATH':'/usr/bin:/bin', 'LANG':'C', 'LC_ALL':'C'},
                input=b'HOSTILE_INPUT_CONTROL\n', capture_output=True, timeout=5, check=True)
            self.assertEqual(result.stdout, b'0\n')

    def test_null_device_metadata_mismatch_refuses_before_fd0_replacement(self):
        opened = SimpleNamespace(st_mode=stat.S_IFCHR|0o666, st_uid=0,
                                 st_dev=1, st_ino=2, st_rdev=3)
        for mutation in ({'st_uid': 501}, {'st_mode': stat.S_IFREG|0o600}, {'st_ino': 99}):
            before = SimpleNamespace(**(vars(opened)|mutation))
            with self.subTest(mutation=mutation), \
                    patch.object(manager.os, 'lstat', return_value=before), \
                    patch.object(manager.os, 'open', return_value=11), \
                    patch.object(manager.os, 'fstat', return_value=opened), \
                    patch.object(manager.os, 'dup2', side_effect=AssertionError('unbound stdin replacement')) as duplicated, \
                    patch.object(manager.os, 'close') as closed:
                with self.assertRaisesRegex(manager.Refusal, 'device identity changed'):
                    manager.close_input()
                duplicated.assert_not_called()
                closed.assert_called_once_with(11)

    def test_inner_manager_closes_input_before_attestation_or_worker_launch(self):
        class Budget:
            def __init__(self, *args): pass
            def left(self): return 120
        calls = []
        def attestation():
            calls.append('attest')
            raise RuntimeError('offline boundary stop')
        m = SimpleNamespace(close_input=lambda: calls.append('close'),
            trusted=lambda: (None, None, None, Budget),
            cgroup_helpers=lambda: SimpleNamespace(attest_unit=attestation))
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); binary=root/'bwrap'; binary.write_bytes(b'inert fixture')
            with patch.object(proof.platform, 'system', return_value='Linux'), \
                    patch.object(proof.platform, 'machine', return_value='x86_64'), \
                    patch.object(proof.sys, 'version_info', (3, 11)), \
                    patch.dict(os.environ, {'RUNNER_ENVIRONMENT': 'github-hosted'}), \
                    patch.object(proof, 'manager', return_value=m), \
                    patch.object(proof, 'supplied_bubblewrap', return_value=({'package': {}}, 'supplier-pin')):
                self.assertEqual(proof.prove(root, binary, root/'supplier.json'), 2)
            self.assertEqual(calls, ['close', 'attest'])
            receipt=json.loads((root/'runtime-prerequisite-proof.json').read_text())
            self.assertFalse(receipt['runtime_boundary_pass'])
            self.assertEqual(receipt['failure']['stage'], 'attested-systemd-delegation')

    def topology(self, root):
        supervisor = root / 'supervisor'; supervisor.mkdir()
        (root / 'cgroup.procs').write_text('')
        (root / 'cgroup.controllers').write_text('memory pids')
        (root / 'cgroup.subtree_control').write_text('')
        (supervisor / 'cgroup.procs').write_text(str(os.getpid()))
        binding = {'root': str(root), 'supervisor': str(supervisor)}
        runtime = {'delegated_cgroup_parent': str(root), 'delegated_supervisor': str(supervisor)}
        return supervisor, binding, runtime

    def test_worker_is_sibling_and_restoration_never_populates_root(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            supervisor, binding, runtime = self.topology(root)
            original_read = Path.read_text

            def kernel_read(path, *args, **kwargs):
                value = original_read(path, *args, **kwargs)
                return value.replace('+', '') if path == root / 'cgroup.subtree_control' else value

            with patch.object(manager, 'cgroup_helpers', return_value=SimpleNamespace(attest_unit=lambda: binding)), \
                    patch.object(Path, 'read_text', kernel_read):
                group, restored = manager.enter_group(runtime)
            self.assertEqual(restored, supervisor)
            self.assertEqual(group.parent, supervisor.parent)
            self.assertEqual((root / 'cgroup.subtree_control').read_text(), '+memory +pids')
            (group / 'cgroup.procs').write_text('')
            (supervisor / 'cgroup.procs').write_text('')
            with patch.object(Path, 'rmdir') as removed:
                manager.release_group(group, supervisor)
            removed.assert_called_once_with()
            self.assertEqual((supervisor / 'cgroup.procs').read_text(), str(os.getpid()))
            self.assertEqual((root / 'cgroup.procs').read_text(), '')

    def test_populated_root_or_wrong_supervisor_refuses_before_activation(self):
        for cause in ('root', 'supervisor'):
            with self.subTest(cause=cause), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                supervisor, binding, runtime = self.topology(root)
                target = root if cause == 'root' else supervisor
                (target / 'cgroup.procs').write_text('42' if cause == 'root' else '')
                with patch.object(manager, 'cgroup_helpers', return_value=SimpleNamespace(attest_unit=lambda: binding)):
                    with self.assertRaises(manager.Refusal):
                        manager.enter_group(runtime)
                self.assertEqual((root / 'cgroup.subtree_control').read_text(), '')
                self.assertFalse((root / ('wf-osv-' + str(os.getpid()))).exists())

    def test_cleanup_refuses_a_different_worker_before_any_membership_write(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            supervisor, _, _ = self.topology(root)
            with patch.object(Path, 'write_text', side_effect=AssertionError('unowned membership write')):
                with self.assertRaisesRegex(manager.Refusal, 'unbound owned cgroup cleanup'):
                    manager.release_group(root / 'wf-osv-other', supervisor)


if __name__ == '__main__':
    unittest.main()
