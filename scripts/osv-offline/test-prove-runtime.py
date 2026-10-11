import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('proof', Path(__file__).with_name('prove-runtime.py'))
proof = importlib.util.module_from_spec(spec)
spec.loader.exec_module(proof)


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
                with self.assertRaisesRegex(RuntimeError, 'frozen R4 source binding changed'):
                    proof.manager()

    def test_receipt_cannot_replace_prior_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'receipt'
            proof.write(path, {'accepted': False})
            with self.assertRaises(FileExistsError):
                proof.write(path, {'accepted': True})
            self.assertEqual(json.loads(path.read_text()), {'accepted': False})


if __name__ == '__main__':
    unittest.main()
