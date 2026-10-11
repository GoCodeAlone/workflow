"""Offline completion fault controls; no Docker, Go or namespace execution."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('proof',HERE/'run-container-proof.py')
proof=importlib.util.module_from_spec(spec);spec.loader.exec_module(proof)

class ProofTests(unittest.TestCase):
    def fixture(self,root):
        source=root/'source';source.mkdir()
        names=('osv_container.py','container-worker.py','container-seccomp.json','run-container-proof.py')
        for name in names:(source/name).write_bytes((HERE/name).read_bytes())
        original=json.loads((HERE/'container-source-bindings.json').read_text())
        bindings=dict(original,files={n:hashlib.sha256((source/n).read_bytes()).hexdigest() for n in names})
        (source/'container-source-bindings.json').write_text(json.dumps(bindings))
        output=root/'output';output.mkdir(mode=0o700)
        compiler=root/'compiler';(compiler/'bin').mkdir(parents=True);(compiler/'bin/go').write_bytes(b'fixed compiler fixture')
        state={'cleanup_calls':0,'started':False,'mode':'success','clock':0}
        class FakeContainer:
            def __init__(self,*args,**kwargs):self.retired=False
            def create(self):return {'Id':'a'*64}
            def start_wait(self):
                state['started']=True
                request=json.loads((output/'request.json').read_text())
                receipt={'complete':True,'exit':0,'identity':request['identity']}
                code=0
                if state['mode']=='exit':code=2
                if state['mode']=='incomplete':receipt['complete']=False
                if state['mode']=='identity':receipt['identity']['GITHUB_SHA']='d'*40
                if state['mode']=='deadline':state['clock']=181
                if state['mode']!='missing':(output/'worker-result.json').write_text(json.dumps(receipt))
                return code,{}
            def cleanup(self):
                state['cleanup_calls']+=1
                if state['mode']=='cleanup':raise RuntimeError('owned container absence unproved')
                self.retired=True
                if state['mode']=='source':(source/'osv_container.py').write_bytes(b'changed source')
                if state['mode']=='input':(root/'output-inputs/input').write_text('changed input')
                return {'container_id':'a'*64,'absent':True,'forced_removal':False}
        module=SimpleNamespace(IMAGE=bindings['image'],IMAGE_CONFIG=bindings['image_config'],
            eligible=lambda:None,Container=FakeContainer)
        env={'RUNNER_TEMP':str(root),'GITHUB_REPOSITORY':'GoCodeAlone/workflow','GITHUB_SHA':'c'*40,
             'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','GITHUB_JOB':'osv-container-prerequisite'}
        return source,output,compiler,state,module,env

    def execute(self,root,mode='success',authorized=True):
        root=root.resolve(strict=True)
        source,output,compiler,state,module,env=self.fixture(root);state['mode']=mode
        real_write=proof.write
        real_readlink=os.readlink
        def readlink(path,*args,**kwargs):
            if str(path).startswith('/proc/self/ns/'):return 'host namespace'
            return real_readlink(path,*args,**kwargs)
        def write(path,value):
            real_write(path,value)
            if mode=='late-publication' and path.name=='container-runtime-proof.json':state['clock']=181
        with (patch.object(proof,'HERE',source),patch.object(proof,'load_launcher',return_value=module) as load,
             patch.dict(os.environ,env),patch.object(proof.os,'readlink',side_effect=readlink),
             patch.object(proof.time,'monotonic',side_effect=lambda:state['clock']),patch.object(proof,'write',side_effect=write)):
            code=proof.run(output,compiler,authorized)
        receipt=json.loads((output/'container-runtime-proof.json').read_text())
        return code,receipt,state,load

    def test_success_requires_cleanup_and_never_grants_scanner_admission(self):
        with tempfile.TemporaryDirectory() as temp:
            code,r,s,load=self.execute(Path(temp))
            self.assertEqual(code,0);self.assertTrue(r['runtime_boundary_pass'])
            self.assertEqual(s['cleanup_calls'],1);self.assertTrue(r['cleanup']['absent'])
            for key in ('accepted','installation_admitted','scanner_execution','candidate_execution'):
                self.assertIs(r[key],False)

    def test_actual_exit_receipt_completeness_and_identity_are_independent_gates(self):
        for mode in ('exit','incomplete','identity','missing'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                code,r,s,load=self.execute(Path(temp),mode)
                self.assertEqual(code,2);self.assertFalse(r['runtime_boundary_pass'])
                self.assertEqual(s['cleanup_calls'],1);self.assertTrue(r['cleanup']['absent'])

    def test_cleanup_failure_vetoes_positive_worker(self):
        with tempfile.TemporaryDirectory() as temp:
            code,r,s,load=self.execute(Path(temp),'cleanup')
            self.assertEqual(code,2);self.assertFalse(r['runtime_boundary_pass'])
            self.assertIn('cleanup_failure',r);self.assertGreaterEqual(s['cleanup_calls'],1)

    def test_source_or_input_mutation_after_execution_vetoes_success(self):
        for mode in ('source','input'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                code,r,s,load=self.execute(Path(temp),mode)
                self.assertEqual(code,2);self.assertFalse(r['runtime_boundary_pass'])
                self.assertEqual(s['cleanup_calls'],1)

    def test_deadline_including_publication_cannot_leave_positive_receipt(self):
        for mode in ('deadline','late-publication'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                code,r,s,load=self.execute(Path(temp),mode)
                self.assertEqual(code,2);self.assertFalse(r['runtime_boundary_pass'])
                self.assertEqual(s['cleanup_calls'],1)

    def test_execution_authorization_precedes_loading_or_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            code,r,s,load=self.execute(Path(temp),authorized=False)
            self.assertEqual(code,2);load.assert_not_called()
            self.assertFalse(s['started']);self.assertEqual(s['cleanup_calls'],0)

    def test_exact_inventory_and_source_digest_required_before_import(self):
        with tempfile.TemporaryDirectory() as temp:
            source,output,compiler,state,module,env=self.fixture(Path(temp))
            (source/'container-source-bindings.json').write_text('{"schema":"workflow.offline-scanner.container-source-bindings.v1","files":{}}')
            with patch.object(proof,'HERE',source):
                with self.assertRaisesRegex(RuntimeError,'exact source inventory'):proof.verify_source()
            bindings=json.loads((HERE/'container-source-bindings.json').read_text())
            (source/'container-source-bindings.json').write_text(json.dumps(bindings))
            (source/'container-worker.py').write_bytes(b'tampered worker')
            with patch.object(proof,'HERE',source):
                with self.assertRaisesRegex(RuntimeError,'source binding mismatch'):proof.verify_source()

if __name__=='__main__':unittest.main()
