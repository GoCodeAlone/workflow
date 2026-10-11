"""ABI witness fault controls; never executes probe instructions or syscalls."""
import ast
import importlib.util
from pathlib import Path
import signal
import subprocess
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('worker',Path(__file__).with_name('container-worker.py'))
worker=importlib.util.module_from_spec(spec);spec.loader.exec_module(worker)

class AbiTests(unittest.TestCase):
    def runner(self,codes):
        def run(argv,**options):
            self.assertEqual(argv[:5],[worker.sys.executable,'-I','-S','-B','-c'])
            self.assertEqual(options['timeout'],10)
            self.assertEqual(options['stdin'],subprocess.DEVNULL)
            self.assertIs(options['capture_output'],True)
            self.assertIs(options['start_new_session'],True)
            ast.parse(argv[5])
            return subprocess.CompletedProcess(argv,next(codes),b'',b'')
        return run

    def test_both_distinct_abi_paths_require_actual_refusal(self):
        for codes in ((77,77),(-signal.SIGSYS,77),(77,-signal.SIGSYS)):
            with self.subTest(codes=codes),patch.object(worker.subprocess,'run',side_effect=self.runner(iter(codes))) as run:
                result=worker.abi_denials()
                self.assertEqual(result,{'x32':codes[0],'i386':codes[1]});self.assertEqual(run.call_count,2)
                self.assertIn('0x40000000|41',run.call_args_list[0].args[0][5])
                self.assertIn('cd 80',run.call_args_list[1].args[0][5])

    def test_ordinary_fault_unsupported_abi_success_and_other_signals_are_not_proof(self):
        for name in ('x32','i386'):
            for exit_code in (0,78,-signal.SIGSEGV,-signal.SIGILL):
                codes=(exit_code,) if name=='x32' else (77,exit_code)
                with self.subTest(name=name,exit_code=exit_code),patch.object(worker.subprocess,'run',side_effect=self.runner(iter(codes))):
                    with self.assertRaisesRegex(RuntimeError,name+' syscall denial not established'):worker.abi_denials()

    def test_incomplete_or_timed_out_child_does_not_credit_abi_refusal(self):
        with patch.object(worker.subprocess,'run',side_effect=subprocess.TimeoutExpired('owned child',10)):
            with self.assertRaises(subprocess.TimeoutExpired):worker.abi_denials()

if __name__=='__main__':unittest.main()
