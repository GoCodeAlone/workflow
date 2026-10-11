import hashlib
from contextlib import ExitStack
import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def module(name):
    spec = importlib.util.spec_from_file_location(name, HERE/(name+'.py'))
    loaded = importlib.util.module_from_spec(spec); spec.loader.exec_module(loaded)
    return loaded


supplier = module('supply-bubblewrap')
proof = module('prove-runtime')


class SupplierTests(unittest.TestCase):
    def row(self, **changes):
        row = {'Package':'bubblewrap','Version':'0.9.0-1ubuntu0.3','Architecture':'amd64',
               'SHA256':'a'*64,'Filename':'pool/main/b/bubblewrap/bubblewrap_0.9.0-1ubuntu0.3_amd64.deb',
               'Size':'50000','Installed-Size':'130'}
        row.update(changes)
        return '\n'.join(k+': '+v for k,v in row.items())

    def test_metadata_requires_exact_package_version_architecture_hash_and_bound(self):
        version = '0.9.0-1ubuntu0.3'
        self.assertEqual(supplier.package_record(self.row(), version)['Package'], 'bubblewrap')
        for change in ({'Package':'other'}, {'Architecture':'arm64'}, {'SHA256':'invalid'},
                       {'Version':'1'}, {'Size':'2000000'}, {'Filename':'../../escape'}):
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                supplier.package_record(self.row(**change), version)
        with self.assertRaises(RuntimeError):
            supplier.package_record(self.row()+'\n\n'+self.row(SHA256='b'*64), version)

    def test_download_source_is_exact_official_https_ubuntu_path(self):
        filename = 'pool/main/b/bubblewrap/example.deb'
        supplier.source_url('https://security.ubuntu.com/ubuntu/'+filename, filename)
        for value in ('http://security.ubuntu.com/ubuntu/'+filename,
                      'https://security.ubuntu.com.evil/ubuntu/'+filename,
                      'https://security.ubuntu.com/ubuntu/other.deb',
                      'https://archive.ubuntu.com/ubuntu/'+filename+'?token=unexpected'):
            with self.assertRaises(RuntimeError):
                supplier.source_url(value, filename)

    def test_binary_is_regular_nonprivileged_amd64_elf(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary)/'bwrap'
            raw = bytearray(64); raw[:6] = b'\x7fELF\x02\x01'; raw[18:20] = b'\x3e\x00'
            binary.write_bytes(raw); binary.chmod(0o700)
            self.assertEqual(supplier.regular_binary(binary), bytes(raw))
            privileged = SimpleNamespace(st_mode=stat.S_IFREG | stat.S_ISUID | 0o700, st_size=len(raw))
            with patch.object(Path, 'lstat', return_value=privileged), self.assertRaises(RuntimeError):
                supplier.regular_binary(binary)
            binary.write_text('not a binary')
            with self.assertRaises(RuntimeError): supplier.regular_binary(binary)

    def test_persistent_or_nonlinux_runner_never_loads_manager(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)/'supplier'; output.mkdir(mode=0o700)
            with patch.dict(os.environ, {'RUNNER_ENVIRONMENT':'self-hosted', 'RUNNER_TEMP':temporary}):
                self.assertEqual(supplier.supply(output), 2)
            data = json.loads((output/'bubblewrap-supplier-receipt.json').read_text())
            self.assertFalse(data['supplied']); self.assertFalse(data['policy_settings_changed'])
            self.assertEqual(data['failure']['stage'], 'ephemeral-platform')

    def test_cleanup_and_its_elapsed_budget_must_pass_before_success(self):
        # Exercise the real supply control flow and real private-file cleanup;
        # commands are fixture callbacks, never subprocesses or network calls.
        original_directory = tempfile.TemporaryDirectory
        original_read = Path.read_text
        original_open = Path.open
        package = b'authenticated-package-fixture'
        raw = bytearray(64); raw[:6] = b'\x7fELF\x02\x01'; raw[18:20] = b'\x3e\x00'
        version = '0.9.0-1ubuntu0.3'
        filename = 'pool/main/b/bubblewrap/bubblewrap_'+version+'_amd64.deb'
        for outcome in ('success', 'cleanup-error', 'cleanup-deadline', 'receipt-deadline'):
            with self.subTest(outcome=outcome), original_directory() as temporary:
                output = Path(temporary)/'supplier'; output.mkdir(mode=0o700)
                state = {'cleaned':False, 'auth_isolated':False}
                class CleanupDirectory:
                    def __init__(self, **kwargs):
                        self.directory = original_directory(**kwargs)
                    def __enter__(self):
                        state['work'] = Path(self.directory.__enter__())
                        return str(state['work'])
                    def __exit__(self, *args):
                        self.directory.__exit__(*args)
                        state['cleaned'] = True
                        if outcome == 'cleanup-error':
                            raise OSError('fixture mandatory cleanup failed')
                class Budget:
                    def __init__(self, *args): pass
                    def left(self):
                        if state.get('receipt_closed') and outcome == 'receipt-deadline':
                            raise RuntimeError('fixture receipt close consumed deadline')
                        if state['cleaned'] and outcome == 'cleanup-deadline':
                            raise RuntimeError('fixture cleanup consumed deadline')
                        return 1
                def bounded(argv, cwd, env, *args):
                    work = state['work']; text = ''
                    if argv[1:] == ['update']:
                        config = Path(env['APT_CONFIG']).read_text()
                        self.assertIn('Dir::Etc::netrc "/dev/null";', config)
                        self.assertIn('Dir::Etc::netrcparts "'+str(work/'empty')+'";', config)
                        self.assertEqual(list((work/'empty').iterdir()), [])
                        self.assertEqual(set(env), {'PATH','LANG','LC_ALL','APT_CONFIG'})
                        state['auth_isolated'] = True
                        (work/'lists'/'fixture-index').write_bytes(b'signed-index-fixture')
                    elif argv[1] == 'policy': text = 'Candidate: '+version+'\n'
                    elif argv[1] == 'show':
                        text = self.row(Size=str(len(package)), Filename=filename,
                                        SHA256=hashlib.sha256(package).hexdigest())
                    elif argv[1] == '--print-uris':
                        text = "'https://archive.ubuntu.com/ubuntu/"+filename+"' fixture.deb 29 SHA256:fixture\n"
                    elif argv[1] == 'download': (Path(cwd)/'fixture.deb').write_bytes(package)
                    elif argv[1] == '--contents': text = 'fixture public package file list\n'
                    elif argv[1] == '--extract':
                        binary = Path(argv[3])/'usr'/'bin'/'bwrap'
                        binary.parent.mkdir(parents=True); binary.write_bytes(raw); binary.chmod(0o700)
                    else: raise AssertionError('unexpected fixture command')
                    return SimpleNamespace(returncode=0, stdout=text, stderr='')
                def digest(path):
                    return 'a'*64 if str(path).startswith(('/usr/bin/', '/usr/share/')) else proof.sha(path)
                manager = SimpleNamespace(trusted=lambda:(None, None, bounded, Budget))
                trusted_proof = SimpleNamespace(IDENTITY=proof.IDENTITY, owned_output=proof.owned_output,
                                               manager=lambda:manager, sha=digest)
                spec = SimpleNamespace(loader=SimpleNamespace(exec_module=lambda loaded:None))
                def read(path, *args, **kwargs):
                    if str(path) == '/etc/os-release': return 'ID=ubuntu\nVERSION_ID="24.04"\n'
                    return original_read(path, *args, **kwargs)
                def open_file(path, *args, **kwargs):
                    f = original_open(path, *args, **kwargs)
                    if outcome == 'receipt-deadline' and path == output/'bubblewrap-supplier-receipt.json':
                        class FinalReceipt:
                            def __enter__(self): return f.__enter__()
                            def __exit__(self, *args):
                                result = f.__exit__(*args)
                                state['receipt_closed'] = True
                                return result
                        return FinalReceipt()
                    return f
                environment = {k:'fixture' for k in proof.IDENTITY}
                environment.update(RUNNER_TEMP=temporary, RUNNER_ENVIRONMENT='github-hosted',
                                   GITHUB_REPOSITORY='GoCodeAlone/workflow',
                                   GITHUB_JOB='osv-runtime-prerequisite', GITHUB_EVENT_NAME='workflow_dispatch')
                with ExitStack() as patches:
                    patches.enter_context(patch.dict(os.environ, environment, clear=True))
                    patches.enter_context(patch.object(supplier.platform, 'system', return_value='Linux'))
                    patches.enter_context(patch.object(supplier.platform, 'machine', return_value='x86_64'))
                    patches.enter_context(patch.object(supplier.os, 'geteuid', return_value=501))
                    patches.enter_context(patch.object(Path, 'read_text', read))
                    patches.enter_context(patch.object(Path, 'open', open_file))
                    patches.enter_context(patch.object(supplier.tempfile, 'TemporaryDirectory', CleanupDirectory))
                    patches.enter_context(patch.object(supplier.importlib.util, 'spec_from_file_location', return_value=spec))
                    patches.enter_context(patch.object(supplier.importlib.util, 'module_from_spec', return_value=trusted_proof))
                    code = None
                    if outcome == 'receipt-deadline':
                        with self.assertRaisesRegex(RuntimeError, 'receipt close consumed deadline'):
                            supplier.supply(output)
                    else:
                        code = supplier.supply(output)
                data = json.loads((output/'bubblewrap-supplier-receipt.json').read_text())
                self.assertTrue(state['auth_isolated']); self.assertTrue(state['cleaned'])
                self.assertFalse(state['work'].exists())
                self.assertEqual((output/'bwrap').read_bytes(), raw)
                self.assertFalse(data['installation_admitted'])
                if outcome == 'receipt-deadline':
                    self.assertIsNone(code); self.assertTrue(state['receipt_closed'])
                    # The retained receipt is provisional: the process raised,
                    # so the job's actual-outcome gate must skip its consumer.
                    self.assertTrue(data['supplied']); self.assertNotIn('failure', data)
                elif outcome == 'success':
                    self.assertEqual(code, 0); self.assertTrue(data['supplied']); self.assertNotIn('failure', data)
                else:
                    self.assertEqual(code, 2); self.assertFalse(data['supplied'])
                    self.assertEqual(data['failure']['stage'], 'owned-cleanup-and-recheck')

    def test_supplier_receipt_joins_current_identity_and_owned_bytes(self):
        class Manager:
            @staticmethod
            def load(path): return json.loads(path.read_text())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)/'supplier'; root.mkdir(mode=0o700)
            binary = root/'bwrap'; binary.write_bytes(b'fixture'); binary.chmod(0o700)
            package = root/'bubblewrap.deb'; package.write_bytes(b'package'); package.chmod(0o600)
            receipt = root/'bubblewrap-supplier-receipt.json'
            data = {'schema':'workflow.offline-scanner.bubblewrap-supplier.v1', 'supplied':True,
                    'installation_admitted':False, 'package_hooks_executed':False, 'policy_settings_changed':False,
                    'identity':{k:os.environ.get(k,'') for k in proof.IDENTITY},'binary_path':str(binary),
                    'binary_sha256':hashlib.sha256(b'fixture').hexdigest(),
                    'package':{'sha256':hashlib.sha256(b'package').hexdigest()}}
            receipt.write_text(json.dumps(data)); receipt.chmod(0o600)
            with patch.dict(os.environ, {'RUNNER_TEMP':temporary}):
                proof.supplied_bubblewrap(binary, receipt, Manager)
                for key in ('failure', 'cleanup_failure'):
                    receipt.write_text(json.dumps(dict(data, **{key:None})))
                    with self.assertRaises(RuntimeError): proof.supplied_bubblewrap(binary, receipt, Manager)
                receipt.write_text(json.dumps(data))
                package.write_bytes(b'changed')
                with self.assertRaises(RuntimeError): proof.supplied_bubblewrap(binary, receipt, Manager)


if __name__ == '__main__':
    unittest.main()
