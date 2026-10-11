"""Offline source/file controls ONLY. No native syscalls, Docker or target exec."""
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import struct
import sys
import tempfile
import unittest
from unittest.mock import patch
HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import candidate_contract as c
import candidate_enforcement as e
import candidate_controls as probes

def module(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file)
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result);return result
receiver=module('receiver','receive-candidate.py')
launcher=module('launcher','run-candidate-runtime.py')
worker=module('worker','container-worker.py')
IDENTITY={'GITHUB_REPOSITORY':'GoCodeAlone/workflow','GITHUB_SHA':'a'*40,
    'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','GITHUB_JOB':'exact-candidate-runtime'}
TOKEN='b'*64
CORE=('candidate_contract.py','candidate_enforcement.py','candidate_controls.py','container-worker.py',
    'run-candidate-runtime.py','receive-candidate.py','osv_container.py','container-seccomp.json')
class Fixture(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.base=Path(self.temp.name).resolve()
    def tearDown(self):self.temp.cleanup()
    def directory(self,name):
        path=self.base/name;path.mkdir(mode=0o700);return path
    def receipt(self,name,value):
        path=self.base/name;raw=(json.dumps(value,sort_keys=True)+'\n').encode()
        path.write_bytes(raw);path.chmod(0o400)
        return path,hashlib.sha256(raw).hexdigest()
    def event(self,pid=100,nr=59):
        return {'id':1,'pid':pid,'nr':nr,'arch':e.AUDIT_X86_64,'flags':0}
    def source(self):
        root=self.directory('source');rows={}
        for name in CORE:
            raw=(HERE/name).read_bytes();(root/name).write_bytes(raw);(root/name).chmod(0o400)
            rows[name]={'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
        raw=(json.dumps({'files':rows,'native_control_names':['MOCK-not-native']},sort_keys=True)+'\n').encode()
        (root/'source-manifest.json').write_bytes(raw);(root/'source-manifest.json').chmod(0o400)
        digest=hashlib.sha256(raw).hexdigest()
        admission={'schema':'workflow.GO5932.source-review-admission.v1','complete':True,
            'accepted':False,'source_manifest_sha256':digest,'security_critical':0,'security_important':0,
            'antagonistic_critical':0,'antagonistic_important':0,'allows_native_controls':True,
            'workflow_ref':launcher.WORKFLOW_REF,'security_decision_sha256':'c'*64,
            'antagonistic_decision_sha256':'d'*64,'candidate_runtime_authorized':True,
            'artifact_sha256':c.SHA,'operation':['/candidate/wfctl','schema']}
        review,review_sha=self.receipt('MOCK-source-admission.json',admission)
        return root,review,review_sha,digest
    def fake_artifact(self,name='input'):
        raw=probes.elf(probes.exit_code(0))
        path=self.base/name;path.write_bytes(raw);path.chmod(0o400)
        return path,raw
    def test_static_abi_layouts_not_runtime_admission(self):
        self.assertEqual((ctypes.sizeof(e.Filter),ctypes.sizeof(e.Program),ctypes.sizeof(e.Data),
            ctypes.sizeof(e.Notification),ctypes.sizeof(e.Response),ctypes.sizeof(e.LandlockPath)),(8,16,64,80,24,12))
    def test_only_exact_first_owned_exec_and_irreversible_refusal(self):
        policy=e.FirstExec(100);self.assertTrue(policy.decide(self.event()))
        self.assertFalse(policy.decide(self.event()));self.assertFalse(policy.decide(self.event(101)))
        policy=e.FirstExec(100);self.assertFalse(policy.decide(self.event(101)))
        self.assertFalse(policy.decide(self.event()))
    def test_execveat_and_non_native_first_never_continue(self):
        for change in ({'nr':322},{'arch':0x40000003},{'flags':1}):
            event=self.event();event.update(change)
            self.assertFalse(e.FirstExec(100).decide(event))
    def test_broker_native_exec_notify_and_wrong_abi_kills(self):
        rows=e.broker_filter()
        self.assertEqual(e.evaluate(rows,59),e.NOTIFY);self.assertEqual(e.evaluate(rows,322),e.NOTIFY)
        self.assertEqual(e.evaluate(rows,1),e.ALLOW)
        self.assertEqual(e.evaluate(rows,0x40000000|59),e.KILL)
        self.assertEqual(e.evaluate(rows,59,arch=0x40000003),e.KILL)
    def test_clone_threads_allowed_processes_denied(self):
        rows=e.target_filter()
        self.assertEqual(e.evaluate(rows,56,(0x50f00,)),e.ALLOW)
        for nr,args in ((56,(17,)),(57,()),(58,())):
            self.assertEqual(e.evaluate(rows,nr,args),e.ERRNO|1)
    def test_executable_memory_denials_and_ordinary_mapping_allowed(self):
        rows=e.target_filter()
        for nr in (9,10,329):
            self.assertEqual(e.evaluate(rows,nr,(0,4096,4)),e.ERRNO|1)
            self.assertEqual(e.evaluate(rows,nr,(0,4096,3)),e.ALLOW)
        for nr in (25,216,30,323,134,135):self.assertEqual(e.evaluate(rows,nr),e.ERRNO|1)
    def test_broker_tampering_and_filter_interposition_denied(self):
        rows=e.target_filter()
        for nr in (311,438,317):self.assertEqual(e.evaluate(rows,nr),e.ERRNO|1)
        self.assertEqual(e.evaluate(rows,157,(22,2)),e.ERRNO|1)
        self.assertEqual(e.evaluate(rows,157,(0x53564d41,0)),e.ALLOW)
    def test_probe_emitter_static_native_non_wx_no_interpreter(self):
        cases=probes.definitions(Path('/out/probes'))
        self.assertGreater(len(cases),30);self.assertEqual(len({v[0] for v in cases}),len(cases))
        for name,code,data,oracle,control in cases:
            raw=probes.elf(code,data);h=struct.unpack('<16sHHIQQQIHHHHHH',raw[:64])
            self.assertEqual(h[1:4],(2,62,1));self.assertEqual(h[4],probes.BASE+4096)
            for offset in (64,120,176):
                seg=struct.unpack('<IIQQQQQQ',raw[offset:offset+56])
                self.assertNotEqual(seg[0],3);self.assertFalse(seg[1]&3==3)
    def test_source_gate_missing_refuses_before_factory(self):
        output=self.directory('out');called=[]
        with self.assertRaises(c.Refusal):
            launcher.run(output,'controls-only',self.base/'absent','-','-','-',TOKEN,IDENTITY,
                factory=lambda *a,**k:called.append(True))
        self.assertFalse(called)
    def test_source_manifest_drift_refused(self):
        root,review,sha,digest=self.source()
        (root/'candidate_enforcement.py').chmod(0o600)
        (root/'candidate_enforcement.py').write_bytes(b'changed')
        with patch.object(launcher,'HERE',root),self.assertRaises(c.Refusal):
            launcher.verify_bundle(c.Budget(2),review,sha,IDENTITY,'controls-only')
    def test_identity_ref_and_dispatch_exact(self):
        env={**IDENTITY,'GITHUB_REF':launcher.WORKFLOW_REF,'RUNNER_ENVIRONMENT':'github-hosted'}
        with patch.dict(os.environ,env,clear=True):self.assertEqual(c.identity(),IDENTITY)
        for key,value in (('GITHUB_REF','refs/heads/main'),('GITHUB_RUN_ID','01'),('GITHUB_SHA','f'*39)):
            with patch.dict(os.environ,{**env,key:value},clear=True),self.assertRaises(c.Refusal):c.identity()
    def test_receiver_authenticated_digest_and_modes_real_files(self):
        incoming,raw=self.fake_artifact();out=self.directory('received')
        transport,sha=self.receipt('MOCK-transport.json',{'schema':'workflow.GO5932.authenticated-candidate-delivery.v1',
            'complete':True,'accepted':False,'candidate_execution':False,'identity':IDENTITY,
            'artifact_sha256':hashlib.sha256(raw).hexdigest(),'artifact_bytes':len(raw),'source_commit':c.SOURCE})
        with patch.object(c,'SHA',hashlib.sha256(raw).hexdigest()),patch.object(c,'SIZE',len(raw)):
            result=receiver.stage(incoming,out,transport,sha,TOKEN,IDENTITY,c.Budget(2))
            self.assertEqual(result['receiver_artifact']['sha256'],c.SHA)
        self.assertEqual(stat.S_IMODE(incoming.stat().st_mode),0o400)
        self.assertEqual(stat.S_IMODE((out/'candidate/wfctl').stat().st_mode),0o500)
        self.assertEqual((out/'candidate/wfctl').read_bytes(),raw)
        self.assertFalse(result['candidate_execution']);self.assertFalse(result['accepted'])
    def test_receiver_wrong_transport_never_copies(self):
        incoming,raw=self.fake_artifact();out=self.directory('received')
        transport,sha=self.receipt('wrong.json',{'complete':True})
        with self.assertRaises(c.Refusal):receiver.stage(incoming,out,transport,sha,TOKEN,IDENTITY,c.Budget(2))
        self.assertEqual(list(out.iterdir()),[])
    def test_receiver_wrong_bytes_hardlink_and_symlink_refused(self):
        incoming,raw=self.fake_artifact()
        with patch.object(c,'SIZE',len(raw)),patch.object(c,'SHA','0'*64),self.assertRaises(c.Refusal):
            c.hash_artifact(incoming,c.Budget(2),mode=0o400)
        linked=self.base/'link';os.link(incoming,linked)
        with patch.object(c,'SIZE',len(raw)),self.assertRaises(c.Refusal):c.hash_artifact(incoming,c.Budget(2),mode=0o400)
        symlink=self.base/'sym';symlink.symlink_to(incoming)
        with self.assertRaises(c.Refusal):c.hash_artifact(symlink,c.Budget(2),mode=0o400)
    def test_elf_interpreter_and_wx_refused(self):
        for kind,flags in ((3,4),(1,7),(0x6474e551,7)):
            path,raw=self.fake_artifact('elf-'+str(kind)+'-'+str(flags))
            altered=bytearray(raw);struct.pack_into('<II',altered,64,kind,flags)
            path.chmod(0o600);path.write_bytes(altered);path.chmod(0o500)
            with patch.object(c,'SHA',hashlib.sha256(altered).hexdigest()),patch.object(c,'SIZE',len(altered)),self.assertRaises(c.Refusal):
                c.hash_artifact(path,c.Budget(2),elf=True)
    def test_schema_duplicates_nonfinite_and_wrong_contract_refused(self):
        good={'$schema':'https://json-schema.org/draft/2020-12/schema','title':'Workflow Configuration',
            'type':'object','required':['modules'],'properties':dict.fromkeys(('modules','workflows','triggers','pipelines'),{})}
        self.assertEqual(c.schema(json.dumps(good).encode()),good)
        for raw in (b'{"x":1,"x":2}',b'{"x":NaN}',b'{}',json.dumps(good).encode()+b'{}'):
            with self.assertRaises((c.Refusal,ValueError)):c.schema(raw)
    def test_real_late_seal_refuses_and_drops_positive(self):
        class Clock:
            def __init__(self):self.expired=False
            def left(self):
                if self.expired:raise c.Refusal('deadline')
                return 1
        clock=Clock();rename=os.rename;path=self.base/'positive.json'
        def delayed(a,b):rename(a,b);clock.expired=True
        with patch.object(c.os,'rename',side_effect=delayed),self.assertRaises(c.Refusal):
            c.seal(path,{'complete':True},clock)
        self.assertFalse(path.exists())
    def fake_factory(self,phase,stale=False,cleanup_fail=False):
        class Fake:
            def __init__(obj,output,helper_root,**kwargs):
                obj.output=output;obj.helper=helper_root;obj.cleanup_mode=False;obj.retired=False
                obj.runner=kwargs['runner'];obj.kwargs=kwargs
            def create(obj,args):
                self.assertEqual(args,('/out/request.json',));self.assertIsNone(obj.kwargs['deadline'])
            def start_wait(obj):
                request=c.decode((obj.output/'request.json').read_bytes())
                controls={'complete':True,'all_native_pass':True,'identity':IDENTITY,
                    'invocation_id':TOKEN,'source_manifest_sha256':request['source_manifest_sha256'],
                    'rows':[{'pass':True,'name':'MOCK-not-native'}]}
                cr=(json.dumps(controls)+'\n').encode();(obj.output/'native-controls.json').write_bytes(cr)
                (obj.output/'native-controls.json').chmod(0o400)
                result={'schema':'workflow.GO5932.exact-runtime-worker.v1','complete':True,'accepted':False,
                    'identity':IDENTITY,'invocation_id':'e'*64 if stale else TOKEN,'mode':'controls-only',
                    'source_manifest_sha256':request['source_manifest_sha256'],'candidate_execution':False,
                    'native_controls_sha256':hashlib.sha256(cr).hexdigest()}
                (obj.output/'worker-result.json').write_text(json.dumps(result));(obj.output/'worker-result.json').chmod(0o400)
                return phase,{}
            def cleanup(obj):
                obj.cleanup_mode=True
                if cleanup_fail:raise c.Refusal('actual cleanup refused')
                obj.retired=True;return {'absent':True}
            def lifecycle_evidence(obj):return {'retired':obj.retired,'MOCK':True}
        return Fake
    def launch_mock(self,phase=0,stale=False,cleanup_fail=False):
        root,review,sha,_=self.source();output=self.directory('out')
        with patch.object(launcher,'HERE',root),patch.object(launcher.os,'readlink',return_value='MOCK-namespace'):
            result=launcher.run(output,'controls-only',review,sha,'-','-',TOKEN,IDENTITY,
                factory=self.fake_factory(phase,stale,cleanup_fail))
        return output,result
    def test_mock_success_is_provisional_not_native_acceptance(self):
        out,result=self.launch_mock()
        self.assertTrue(result['complete']);self.assertFalse(result['accepted'])
        self.assertTrue((out/'controller-proof.json').exists())
    def test_failed_actual_step_cannot_use_complete_receipt(self):
        with self.assertRaises(c.Refusal):self.launch_mock(phase=2)
        self.assertFalse((self.base/'out/controller-proof.json').exists())
    def test_stale_success_token_refused(self):
        with self.assertRaises(c.Refusal):self.launch_mock(stale=True)
        self.assertFalse((self.base/'out/controller-proof.json').exists())
    def test_real_cleanup_failure_cannot_publish_positive(self):
        with self.assertRaises(c.Refusal):self.launch_mock(cleanup_fail=True)
        self.assertFalse((self.base/'out/controller-proof.json').exists())
    def test_closed_budget_reserve_before_command(self):
        # Exercise the actual injected runner through a mocked lifecycle factory.
        root,review,sha,_=self.source();output=self.directory('out')
        commands=[]
        class Budget:
            def left(self):return 20
        class Calls(self.fake_factory(0)):
            def create(obj,args):obj.runner(['MOCK'],15)
        with patch.object(launcher,'HERE',root),patch.object(launcher.os,'readlink',return_value='MOCK'),self.assertRaises(c.Refusal):
            launcher.run(output,'controls-only',review,sha,'-','-',TOKEN,IDENTITY,
                factory=Calls,command=lambda *a:commands.append(a),budget=Budget())
        self.assertFalse(commands)
    def test_worker_controls_failure_never_reaches_candidate(self):
        req={'mode':'candidate','identity':IDENTITY,'invocation_id':TOKEN,'source_manifest_sha256':'a'*64}
        with patch.object(worker,'verify_bundle'),patch.object(worker,'witness'),patch.object(worker.probes,'controls',side_effect=c.Refusal('native control failed')),patch.object(worker.e,'broker') as target,self.assertRaises(c.Refusal):
            worker.execute(req,c.Budget(2))
        target.assert_not_called()
    def test_native_control_oracles_fail_closed(self):
        result={'native_exit':0,'complete_process_enforcement':True,'stdout':b'OK','events':[]}
        self.assertTrue(probes.evaluate_result('first_owned_exec','exit0',result,None))
        self.assertFalse(probes.evaluate_result('x','denied-exec',result,None))
        self.assertFalse(probes.evaluate_result('x','SIGSYS',result,None))
        self.assertFalse(probes.evaluate_result('x','receive-deadline',result,'operation deadline'))
if __name__=='__main__':unittest.main()
