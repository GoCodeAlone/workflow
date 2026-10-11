"""Offline source/file controls ONLY. No native syscalls, Docker or target exec."""
import ctypes
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import struct
import sys
import tempfile
import types
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
    def notification_mocks(self,api,clock):
        self.timers=[];self.old_handler=object();self.handlers=[]
        def handler(signum,value):
            self.handlers.append((signum,value));return self.old_handler
        self.addCleanup(patch.stopall)
        patch.object(e,'libc',return_value=api).start()
        patch.object(e.time,'monotonic',side_effect=clock).start()
        patch.object(e.signal,'signal',side_effect=handler).start()
        patch.object(e.signal,'siginterrupt').start()
        patch.object(e.signal,'setitimer',side_effect=lambda kind,seconds:self.timers.append((kind,seconds))).start()
    def test_ioctl_failure_preserves_typed_errno_and_operation(self):
        def failed(*args):ctypes.set_errno(errno.EBADF);return -1
        with patch.object(e,'libc',return_value=types.SimpleNamespace(ioctl=failed)):
            with self.assertRaises(e.IoctlRefusal) as caught:e.ioctl(9,0xc0502100,e.Notification(),'notification receive')
        self.assertIsInstance(caught.exception,c.Refusal)
        self.assertEqual(caught.exception.errno,errno.EBADF)
        self.assertEqual(caught.exception.operation,'notification receive')
    def test_recv_retries_only_cancellations_with_fresh_zero_buffers(self):
        buffers=[];observed=[]
        def receive(fd,number,pointer):
            self.assertEqual(number.value,0xc0502100)
            n=pointer._obj;buffers.append(n)
            observed.append(bytes(n));self.assertEqual(bytes(n),b'\0'*80)
            self.assertEqual(ctypes.get_errno(),0)
            if len(buffers)<3:
                ctypes.memset(ctypes.byref(n),255,80);ctypes.set_errno(errno.ENOENT);return -1
            n.id=27;n.pid=100;n.data.arch=e.AUDIT_X86_64;n.data.nr=59;return 0
        self.notification_mocks(types.SimpleNamespace(ioctl=receive),lambda:10)
        result=e.receive(9,0.1)
        self.assertIs(result,buffers[-1]);self.assertEqual(len({id(n) for n in buffers}),3)
        self.assertEqual(len(observed),3);self.assertEqual(len(self.timers),2)
        self.assertEqual(self.timers[0][0],e.signal.ITIMER_REAL);self.assertAlmostEqual(self.timers[0][1],0.1)
        self.assertEqual(self.timers[-1],(e.signal.ITIMER_REAL,0))
        self.assertEqual(self.handlers[-1],(e.signal.SIGALRM,self.old_handler))
        event={'id':result.id,'pid':result.pid,'nr':result.data.nr,'arch':result.data.arch,'flags':result.flags}
        self.assertTrue(e.FirstExec(100).decide(event))
    def test_persistent_canceled_recv_hits_one_absolute_deadline(self):
        calls=[];now=[0]
        def clock():now[0]+=0.01;return now[0]
        def canceled(fd,number,pointer):
            calls.append(bytes(pointer._obj));ctypes.memset(pointer,255,80)
            ctypes.set_errno(errno.ENOENT);return -1
        self.notification_mocks(types.SimpleNamespace(ioctl=canceled),clock)
        with self.assertRaisesRegex(c.Refusal,'notification receive deadline'):e.receive(9,0.07)
        self.assertGreaterEqual(len(calls),2);self.assertTrue(all(raw==b'\0'*80 for raw in calls))
        self.assertEqual(len(self.timers),2);self.assertGreater(self.timers[0][1],0);self.assertLess(self.timers[0][1],0.07)
        self.assertEqual(self.timers[-1],(e.signal.ITIMER_REAL,0))
        self.assertEqual(self.handlers[-1],(e.signal.SIGALRM,self.old_handler))
    def test_other_recv_errno_never_retries_or_grants(self):
        for code in (errno.EPERM,errno.EINVAL,errno.EINTR,errno.EAGAIN,errno.EBADF):
            calls=[]
            def refused(fd,number,pointer):calls.append(pointer._obj);ctypes.set_errno(code);return -1
            self.notification_mocks(types.SimpleNamespace(ioctl=refused),lambda:10)
            with self.assertRaises(e.IoctlRefusal) as caught:e.receive(9,0.1)
            self.assertEqual(caught.exception.errno,code);self.assertEqual(len(calls),1)
            self.assertEqual(len(self.timers),2)
            patch.stopall()
    def test_recovered_notification_still_rejects_flags_arch_and_number(self):
        for flags,arch,nr in ((1,e.AUDIT_X86_64,59),(0,0x40000003,59),(0,e.AUDIT_X86_64,1)):
            calls=[]
            def receive(fd,number,pointer):
                calls.append(pointer._obj)
                if len(calls)==1:ctypes.set_errno(errno.ENOENT);return -1
                n=pointer._obj;n.flags=flags;n.data.arch=arch;n.data.nr=nr;return 0
            self.notification_mocks(types.SimpleNamespace(ioctl=receive),lambda:10)
            with self.assertRaisesRegex(c.Refusal,'unexpected exec notification'):e.receive(9,0.1)
            self.assertEqual(len(calls),2);patch.stopall()
    def test_late_successful_recv_cannot_escape_original_deadline(self):
        now=[0];returned=[]
        def receive(fd,number,pointer):
            n=pointer._obj;n.pid=100;n.data.arch=e.AUDIT_X86_64;n.data.nr=59
            returned.append(n);now[0]=1;return 0
        self.notification_mocks(types.SimpleNamespace(ioctl=receive),lambda:now[0])
        with self.assertRaisesRegex(c.Refusal,'notification receive deadline'):e.receive(9,0.1)
        self.assertEqual(len(returned),1);self.assertEqual(self.timers,[(e.signal.ITIMER_REAL,0.1),(e.signal.ITIMER_REAL,0)])
    def test_id_validation_and_send_enoent_are_not_retried(self):
        for failed_number,expected_calls in ((0x40082102,1),(0xc0182101,2)):
            calls=[]
            def ioctl(fd,number,pointer):
                calls.append(number.value)
                if number.value==failed_number:ctypes.set_errno(errno.ENOENT);return -1
                return 0
            n=e.Notification();n.id=27
            with patch.object(e,'libc',return_value=types.SimpleNamespace(ioctl=ioctl)):
                with self.assertRaises(e.IoctlRefusal) as caught:e.respond(9,n,True)
            self.assertEqual(caught.exception.errno,errno.ENOENT);self.assertEqual(len(calls),expected_calls)
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
    def readiness_json(self):
        return json.dumps({'$schema':'https://json-schema.org/draft/2020-12/schema',
            'title':'Workflow Configuration','type':'object','required':['modules'],
            'properties':dict.fromkeys(('modules','workflows','triggers','pipelines'),{})}).encode()
    def test_exact_schema_notice_or_empty_retains_diagnostic(self):
        subject=c.expected_schema_subject()
        self.assertEqual(len(c.SONIC_NOTICE),165)
        self.assertEqual(hashlib.sha256(c.SONIC_NOTICE).hexdigest(),
            '9362b032bcb7342f2ad38091d3fd48f63a03c5da6d72dbcd9ac3d86e0a5ec53c')
        for raw,label in ((b'','empty'),(c.SONIC_NOTICE,'sonic_optional_stdlib_fallback')):
            result=c.schema_diagnostic(0,self.readiness_json(),raw,subject)
            self.assertEqual(result['classification'],label)
            self.assertEqual(result['bytes'],len(raw))
            self.assertEqual(result['sha256'],hashlib.sha256(raw).hexdigest())
            self.assertTrue(result['raw_stderr_retained']);self.assertEqual(result['subject'],subject)
    def test_schema_notice_subject_and_manifest_drift_refused(self):
        subject=c.expected_schema_subject()
        for key in subject:
            for value in ('other',None):
                changed=dict(subject);changed[key]=value
                with self.assertRaises(c.Refusal):c.schema_diagnostic(0,self.readiness_json(),c.SONIC_NOTICE,changed)
            changed=dict(subject);del changed[key]
            with self.assertRaises(c.Refusal):c.schema_diagnostic(0,self.readiness_json(),c.SONIC_NOTICE,changed)
        for changed in ({**subject,'extra':True},{**subject,'artifact_bytes':float(c.SIZE)}):
            with self.assertRaises(c.Refusal):c.schema_diagnostic(0,self.readiness_json(),c.SONIC_NOTICE,changed)
        manifest=json.loads((HERE/'source-manifest.json').read_bytes())
        self.assertEqual(c.schema_subject(manifest),subject)
        for key in ('source_commit','artifact_sha256','artifact_bytes','schema_diagnostic_subject'):
            changed=dict(manifest);changed[key]='changed'
            with self.assertRaises(c.Refusal):c.schema_subject(changed)
    def test_schema_notice_exact_bytes_no_prefix_suffix_normalization(self):
        good=c.SONIC_NOTICE
        for raw in (good[:-1],good+b'x',b'x'+good,good+b'\n',good+good,
            b'another message\n',good.replace(b'sonic',b'Sonic'),
            good.replace(b'1.26',b'1.25'),good.replace(b'WARNING',b'WARN\xe2\x80\x8bING')):
            with self.assertRaises(c.Refusal):c.schema_diagnostic(0,self.readiness_json(),raw,c.expected_schema_subject())
    def test_schema_notice_native_exit_invalid_duplicate_nonfinite_wrong_schema(self):
        for code in (1,-9,125,False,'0',None):
            with self.assertRaises(c.Refusal):c.schema_diagnostic(code,self.readiness_json(),c.SONIC_NOTICE,c.expected_schema_subject())
        for raw in (b'{}',b'{"x":1,"x":2}',b'{"x":NaN}',b'{"x":Infinity}',
            self.readiness_json()[:-1],self.readiness_json()+b'{}',
            self.readiness_json().replace(b'Workflow Configuration',b'Other'),
            self.readiness_json().replace(b'"modules"',b'"other"')):
            with self.assertRaises((c.Refusal,ValueError)):
                c.schema_diagnostic(0,raw,c.SONIC_NOTICE,c.expected_schema_subject())
    def worker_candidate_mock(self,stderr=None,*,code=0,after=None,dirty=False,expired=False,manifest=None):
        # Actual Python worker/strict parser/raw file writes/conservation flow;
        # substitutes only native witness/controls/broker/artifact reads. No Linux
        # syscall, binary or container execution is credited by this fixture.
        out=self.directory('worker-'+str(len(list(self.base.iterdir()))))
        operations=tuple(str(self.directory('operation-'+str(len(list(self.base.iterdir()))))) for _ in range(4))
        budget=c.Budget(5)
        if expired:
            class Clock:
                expired=False
                def left(obj):
                    if obj.expired:raise c.Refusal('MOCK terminal deadline')
                    return 10
            budget=Clock()
        actual_path=Path
        def path(raw):
            value=str(raw)
            return out/value[5:] if value.startswith('/out/') else actual_path(raw)
        real_seal=c.seal
        def seal(raw,value,clock):return real_seal(path(raw),value,clock)
        before={'sha256':c.SHA,'bytes':c.SIZE,'mode':'0o500','MOCK':True};checks=[]
        def artifact(raw,clock,*,elf):
            self.assertEqual(raw,'/candidate/wfctl');self.assertTrue(elf);checks.append(raw)
            if len(checks)==2:
                if expired:budget.expired=True
                return before if after is None else after
            return before
        native={'native_exit':code,'stdout':self.readiness_json(),
            'stderr':c.SONIC_NOTICE if stderr is None else stderr,
            'complete_process_enforcement':True,'MOCK_not_native':True}
        def target(*args,**kwargs):
            if dirty:(actual_path(operations[0])/'unexpected').write_bytes(b'drift')
            return native
        request={'mode':'candidate','identity':IDENTITY,'invocation_id':TOKEN,
            'source_manifest_sha256':hashlib.sha256((HERE/'source-manifest.json').read_bytes()).hexdigest(),
            'artifact_sha256':c.SHA}
        controls={'complete':True,'all_native_pass':True,'MOCK_not_native':True,
            'rows':[{'name':n,'pass':True,'MOCK':True} for n in
                json.loads((HERE/'source-manifest.json').read_bytes())['native_control_names']]}
        result={}
        with patch.object(worker,'Path',side_effect=path),patch.object(worker,'OPERATIONS',operations),\
            patch.object(worker,'witness',return_value={'MOCK':True}),\
            patch.object(worker.probes,'controls',return_value=controls),\
            patch.object(worker.e,'broker',side_effect=target) as broker,\
            patch.object(c,'hash_artifact',side_effect=artifact),patch.object(c,'seal',side_effect=seal):
            if manifest is None:
                self.result=result;self.worker_out=out;self.artifact_checks=checks
                worker.execute(request,budget,result)
            else:
                with patch.object(worker,'verify_bundle',return_value=manifest):
                    with self.assertRaises(c.Refusal):worker.execute(request,budget,result)
                    broker.assert_not_called()
                    self.assertFalse(checks)
        return result,out,checks
    def test_worker_exact_notice_preserves_raw_and_final_conservation(self):
        result,out,checks=self.worker_candidate_mock()
        self.assertTrue(result['complete']);self.assertFalse(result['accepted'])
        self.assertTrue(result['candidate_process_exit_observed']);self.assertTrue(result['candidate_execution'])
        self.assertEqual(len(checks),2)
        self.assertEqual((out/'candidate.stderr').read_bytes(),c.SONIC_NOTICE)
        self.assertEqual(result['schema_diagnostic']['sha256'],result['stderr']['sha256'])
        self.assertEqual(result['schema_diagnostic']['bytes'],result['stderr']['bytes'])
        self.assertEqual(result['schema_diagnostic']['classification'],'sonic_optional_stdlib_fallback')
    def test_worker_empty_stderr_preserves_original_readiness(self):
        result,out,checks=self.worker_candidate_mock(b'')
        self.assertTrue(result['complete']);self.assertEqual(len(checks),2)
        self.assertEqual(result['schema_diagnostic']['classification'],'empty')
        self.assertEqual((out/'candidate.stderr').read_bytes(),b'')
    def test_worker_other_stderr_or_exit_refuses_and_preserves_raw(self):
        for raw,code in ((c.SONIC_NOTICE+b'x',0),(c.SONIC_NOTICE,1)):
            with self.assertRaises(c.Refusal):self.worker_candidate_mock(raw,code=code)
            self.assertFalse(self.result['complete']);self.assertTrue(self.result['candidate_process_exit_observed'])
            self.assertEqual((self.worker_out/'candidate.stderr').read_bytes(),raw)
            self.assertEqual(len(self.artifact_checks),1)
    def test_worker_notice_still_refuses_post_execution_artifact_drift(self):
        with self.assertRaisesRegex(c.Refusal,'read-only candidate metadata changed'):
            self.worker_candidate_mock(after={'sha256':'0'*64})
        self.assertFalse(self.result['complete']);self.assertEqual(len(self.artifact_checks),2)
        self.assertEqual((self.worker_out/'candidate.stderr').read_bytes(),c.SONIC_NOTICE)
    def test_worker_notice_still_refuses_owned_operation_drift(self):
        with self.assertRaisesRegex(c.Refusal,'fresh exclusively owned canonical directory required'):
            self.worker_candidate_mock(dirty=True)
        self.assertFalse(self.result['complete']);self.assertEqual(len(self.artifact_checks),2)
    def test_worker_notice_bad_provenance_refuses_before_candidate(self):
        manifest=json.loads((HERE/'source-manifest.json').read_bytes())
        manifest['schema_diagnostic_subject']['sonic_h1']='h1:other'
        self.worker_candidate_mock(manifest=manifest)
    def test_worker_notice_still_refuses_post_conservation_deadline(self):
        with self.assertRaisesRegex(c.Refusal,'MOCK terminal deadline'):
            self.worker_candidate_mock(expired=True)
        self.assertFalse(self.result['complete']);self.assertEqual(len(self.artifact_checks),2)
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
