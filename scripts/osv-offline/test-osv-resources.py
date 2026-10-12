"""Bounded offline policy/startup/lifecycle controls; no native or Docker calls."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch,Mock
HERE=Path(__file__).resolve().parent;sys.path.insert(0,str(HERE))
import osv_resources as r

def module(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file);value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value);return value
m=module('resource_manager','run.py');h=module('resource_headroom','run-docker-headroom-proof.py')
cmod=module('resource_container','osv_container.py');lifecycle=module('resource_lifecycle','test-osv-container.py')
old=module('resource_memory_controls','test-production-manager.py')
MANIFEST=json.loads((HERE/'bootstrap-manifest.json').read_text())

class ResourceControls(unittest.TestCase):
    def authority(self):
        return {'phase':'scan-candidate','memory_bytes':r.SCAN_MEMORY,'owner_approval_sha256':r.OWNER_APPROVAL_SHA256,
            'startup_guard_sha256':r.ACTUAL_PARENT_STARTUP_GUARD_SHA256,'actual_headroom_receipt_sha256':'e'*64,
            'core_contract_sha256':MANIFEST['core_contract_sha256']}
    def observation(self,cid='a'*64,enlarged=False):
        native={'pid':123,'starttime_ticks':99,'ppid':42,'pgrp':123,'session':123,'comm':'timeout','state':'S'}
        limits=r.limits_for_phase('scan-candidate' if enlarged else None)
        return {'container_id':cid,'nonce':'b'*32,'moment':'before','host_init':native,'host_init_final':dict(native,state='R'),
            'host_init_cgroup':'system.slice/docker-'+cid+'.scope','own_container_cgroup_identity':[1,50],
            'own_container_limits':dict(limits,**{'cpu.max':'200000 100000'}),'memory_page_size':4096,
            'requested_memory_bytes':r.SCAN_MEMORY if enlarged else r.DEFAULT_MEMORY,
            'own_container_memory_cap_excluded_from_parent_estimate':True,
            'physical_bytes':{'MemTotal':16*1024**3,'MemAvailable':r.REQUIRED_HOST_PARENT_HEADROOM},
            'docker_parent_ancestors':[{'group':'system.slice','identity':[1,2],'memory_max_bytes':None,'memory_current_bytes':123},
                {'group':'.','identity':[1,1],'memory_max_bytes':None,'memory_current_bytes':None,'uncontrolled_root_fields_observed_absent':True}],
            'estimated_physical_and_docker_parent_headroom_bytes':r.REQUIRED_HOST_PARENT_HEADROOM,'docker_parent_effective_memory_max_bytes':None,
            'observed_4_5GiB_prerequisite':True}
    def proof(self):
        identity={key:str(i) for i,key in enumerate(h.IDENTITY)};identity['GITHUB_JOB']='osv-production-proof'
        a=self.observation();b=copy.deepcopy(a);b['moment']='after'
        return {'accepted':False,'measurement_complete':True,'retirement_verified':True,'exit':0,'actual_exit':0,
            'parent_phase':'scan-candidate','identity':identity,'input_retired':True,'container_id':'a'*64,'nonce':'b'*32,
            'source_manifest_sha256':MANIFEST['trusted_files']['docker-headroom-manifest.json'],
            'lifecycle':{'retired':True,'container_id':'a'*64},'observations':[a,b]}
    def check_probe(self,p,identity=None):
        with patch.object(m,'headroom_module',return_value=h),patch.object(r.os,'sysconf',return_value=4096):
            return m.validate_resource_probe(p,'scan-candidate',MANIFEST,identity or self.proof()['identity'],r.ACTUAL_PARENT_STARTUP_GUARD_SHA256)

    def test_expired_short_nonfinite_or_extended_parent_deadline_refuses_before_create(self):
        # Valid parent-owner fixture; only the inherited deadline is adverse.
        source=Mock();source.eligible=Mock();source.Container=Mock()
        with tempfile.TemporaryDirectory() as temp:
            env={'GITHUB_REPOSITORY':'GoCodeAlone/workflow','GITHUB_SHA':'c'*40,'GITHUB_REF':'refs/heads/prep/osv-prerequisites-20261011-r1',
                'GITHUB_EVENT_NAME':'workflow_dispatch','GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','GITHUB_JOB':'osv-production-proof','RUNNER_TEMP':str(Path(temp).resolve())}
            for i,value in enumerate((None,99,100,279.99,1901,float('nan'),float('inf'),True)):
                output=Path(temp).resolve()/('osv-headroom-negative-'+str(i));output.mkdir(mode=0o700)
                with patch.dict(os.environ,env),patch.object(h,'sources',return_value=(source,'e'*64,{})),patch.object(h.time,'monotonic',return_value=100):
                    self.assertEqual(h.run(output,True,parent_phase='scan-candidate',phase_deadline=value),2)
                source.Container.assert_not_called()
                self.assertFalse(json.loads((output/'docker-parent-headroom-proof.json').read_text())['retirement_verified'])
            # Same valid source/identity/output provider reaches construction at the
            # exact180s reserve boundary; invalid controls were not earlier identity refusals.
            output=Path(temp).resolve()/'osv-headroom-positive';output.mkdir(mode=0o700)
            source.Container.side_effect=RuntimeError('construction checkpoint')
            with patch.dict(os.environ,env),patch.object(h,'sources',return_value=(source,'e'*64,{})),patch.object(h.time,'monotonic',return_value=100):
                self.assertEqual(h.run(output,True,parent_phase='scan-candidate',phase_deadline=280),2)
            self.assertEqual(source.Container.call_count,1)
            self.assertEqual(source.Container.call_args.kwargs['deadline'],280)
    def test_controller_passes_its_existing_phase_deadline_to_probe(self):
        with tempfile.TemporaryDirectory() as temp,patch.dict(os.environ,{'RUNNER_TEMP':str(Path(temp).resolve())}):
            state=Path(temp)/'state';state.mkdir(mode=0o700);helper=Mock();helper.run.side_effect=RuntimeError('sentinel stop before work')
            budget=Mock();budget.deadline=1234.5
            with patch.object(m,'headroom_module',return_value=helper):
                with self.assertRaisesRegex(RuntimeError,'sentinel'):m.resource_probe('scan-candidate',MANIFEST,state,budget)
            self.assertEqual(helper.run.call_args.kwargs,{'parent_phase':'scan-candidate','phase_deadline':1234.5})
    def test_source_policy_and_guard_exact(self):
        self.assertEqual(m.resource_guard_digest(MANIFEST),r.ACTUAL_PARENT_STARTUP_GUARD_SHA256)
        for name in ('manual-proof-plan.json','scanner-profile.json'):
            self.assertEqual(json.loads((HERE/name).read_text())['phase_memory_proposal'],r.source_proposal())
        self.assertEqual(r.SCAN_KERNEL_MEMORY,2999996416);self.assertEqual(r.SCAN_MEMORY-r.SCAN_KERNEL_MEMORY,3584)
        self.assertEqual(r.REQUIRED_HOST_PARENT_HEADROOM,5147483648)
        for phase,value in r.PHASE_MEMORY.items():
            self.assertEqual(value,r.SCAN_MEMORY if phase in ('scan-baseline','scan-candidate') else r.DEFAULT_MEMORY)
    def test_null_owner_refuses_before_any_create_or_probe(self):
        calls=Mock()
        with patch.object(r,'OWNER_APPROVAL_SHA256',None),patch.object(cmod,'eligible'),patch.object(m,'resource_probe',calls):
            # Explicit trusted provider fixture preserves source/profile joins under NULL owner;
            # no earlier hash-mismatch refusal is credited as the semantic control.
            plan={'phase_memory_proposal':r.source_proposal(),'phase_resource_owner_approval_sha256':None}
            with patch.object(m,'load',return_value=plan):
                with self.assertRaisesRegex(r.ResourceRefusal,'owner3GB phase authority is unbound'):
                    m.resource_phase_selection('scan-candidate',MANIFEST,Path('/unused'),Mock())
            with self.assertRaises(r.ResourceRefusal):cmod.Container(Path('/unused'),HERE,production=True,
                resource_phase='scan-candidate',resource_authority=self.authority(),runner=calls)
        calls.assert_not_called()
    def test_unbound_guard_and_unknown_phase_refuse(self):
        with patch.object(r,'ACTUAL_PARENT_STARTUP_GUARD_SHA256',None):
            with self.assertRaises(r.ResourceRefusal):r.authorize_phase('scan-candidate',self.authority())
        for value in ('scan-other',True,3,None):
            if value is None:continue
            with self.subTest(value=value),self.assertRaises(r.ResourceRefusal):r.requested_memory(value)
    def test_binary3GiB_old2point5_and_freeform_authority_refuse(self):
        for value in (3221225472,2684354560,2999996416,3000000001,True,'3000000000'):
            a=self.authority();a['memory_bytes']=value
            with self.subTest(value=value),self.assertRaises(r.ResourceRefusal):r.authorize_phase('scan-candidate',a)
        for change in ({'phase':'scan-baseline'},{'owner_approval_sha256':'0'*64},{'startup_guard_sha256':'0'*64},{'extra':1}):
            with self.subTest(change=change),self.assertRaises(r.ResourceRefusal):r.authorize_phase('scan-candidate',dict(self.authority(),**change))
        with self.assertRaises(r.ResourceRefusal):r.authorize_phase('report-differential',self.authority())
    def test_requested_decimal_flags_and_inspect_binary_rejection(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=lifecycle.LifecycleTests().fixture(Path(temp).resolve());c.resource_phase='scan-candidate';c.resource_authority=self.authority()
            c.limits=r.authorize_phase(c.resource_phase,c.resource_authority);c.requested_memory=r.requested_memory(c.resource_phase)
            original=c.run
            def fake(argv,seconds=15):
                result=original(argv,seconds)
                if argv[0]=='create':s['row']['HostConfig'].update(Memory=3000000000,MemorySwap=3000000000)
                return result
            c.run=fake;c.create();args=next(x for x in s['calls'] if x[0]=='create')
            for flag in ('--memory','--memory-swap'):self.assertEqual(args[args.index(flag)+1],'3000000000')
            c.validate(s['row'])
            for bad in (3221225472,2684354560,2999996416):
                row=copy.deepcopy(s['row']);row['HostConfig']['Memory']=bad
                with self.subTest(bad=bad),self.assertRaises(lifecycle.m.BoundaryError):c.validate(row)
            c.cleanup();self.assertTrue(c.retired)
    def test_default_envelope_no_authority_and_unsupported_page_refuses(self):
        self.assertEqual(r.authorize_phase('report-differential',None),r.limits_for_phase())
        with patch.object(r.os,'sysconf',return_value=16384),self.assertRaises(r.ResourceRefusal):r.require_page_size()
    def test_probe_exact_threshold_admitted_lower_raw_physical_refuses(self):
        self.check_probe(self.proof())
        p=self.proof();p['observations'][1]['physical_bytes']['MemAvailable']-=1
        p['observations'][1]['estimated_physical_and_docker_parent_headroom_bytes']-=1
        with self.assertRaises(m.Refusal):self.check_probe(p)
    def test_probe_raw_parent_limit_cannot_be_hidden_by_claimed_boolean(self):
        p=self.proof();row=p['observations'][1];row['docker_parent_ancestors'][0]['memory_max_bytes']=r.REQUIRED_HOST_PARENT_HEADROOM
        with self.assertRaises(m.Refusal):self.check_probe(p)
    def test_probe_wrong_job_source_run_retirement_and_partial_refuse(self):
        for change in ({'identity':dict(self.proof()['identity'],GITHUB_RUN_ID='different-VM-run')},
            {'source_manifest_sha256':'0'*64},{'retirement_verified':False},{'input_retired':False},{'actual_exit':2},{'observations':[]}):
            with self.subTest(change=change),self.assertRaises(m.Refusal):self.check_probe(dict(self.proof(),**change))
    def test_probe_cid_nonce_parent_pid_start_swap_cpu_rebind_refuse(self):
        changes=[('container_id','f'*64),('nonce','f'*32),('own_container_cgroup_identity',[1,99]),
            ('host_init_cgroup','other/docker-'+'a'*64+'.scope'),('memory_page_size',16384),('requested_memory_bytes',3221225472)]
        for key,value in changes:
            p=self.proof();p['observations'][1][key]=value
            with self.subTest(key=key),self.assertRaises(Exception):self.check_probe(p)
        for field,value in [('pid',124),('starttime_ticks',100),('state','Z')]:
            p=self.proof();p['observations'][1]['host_init'][field]=value
            with self.subTest(field=field),self.assertRaises(Exception):self.check_probe(p)
        for field,value in [('memory.swap.max','1'),('memory.max','3000000000'),('cpu.max','100000 100000')]:
            p=self.proof();p['observations'][1]['own_container_limits'][field]=value
            with self.subTest(field=field),self.assertRaises(Exception):self.check_probe(p)
    def startup(self,root,change=None):
        private=root/'worker-owned';private.mkdir(mode=0o700);output=root/'probe';output.mkdir(mode=0o700);(output/'raw').mkdir(mode=0o700)
        original=self.observation();observed=self.observation('c'*64,True)
        authority=self.authority();selection={'phase':'scan-candidate','authority':authority,'limits':r.limits_for_phase('scan-candidate'),
            'probe':{'output':str(output),'observation':original,'host_identity_sha256':'d'*64,'recorded_monotonic':time.monotonic()}}
        request={'resource_nonce':'b'*32,'resource_container_id':'c'*64};request_sha='f'*64
        c=SimpleNamespace(identifier='c'*64,nonce='b'*32,requested_memory=r.SCAN_MEMORY,limits=r.limits_for_phase('scan-candidate'))
        row={'State':{'Pid':123}};c.live_identity=lambda:row
        waiting={'schema':'workflow.offline-scanner.resource-waiting.v1','nonce':c.nonce,'request_sha256':request_sha,'container_id':c.identifier,
            'resource_phase':selection['phase'],'resource_authority':authority,'core_contract_sha256':MANIFEST['core_contract_sha256'],
            'native_started':False,'worker_private_pid':2}
        if change:change(selection,observed,waiting,c,row)
        m.write_request_resource(private/'resource-waiting.json',waiting,committed=True)
        with patch.object(m,'headroom_module',return_value=h),patch.object(h,'capture',return_value=observed),\
            patch.object(m,'resource_host_identity',return_value='d'*64),patch.object(r.os,'sysconf',return_value=4096):
            m.resource_startup_observer(c,row,private,request,request_sha,selection,MANIFEST,Mock())
        return private
    def test_live_enlarged_parent_guard_writes_exact_ready_only_after_checks(self):
        with tempfile.TemporaryDirectory() as temp:
            private=self.startup(Path(temp).resolve());ready=json.loads((private/'resource-ready.json').read_text())
            self.assertTrue(ready['native_execution_permitted']);self.assertEqual(ready['requested_memory_bytes'],3000000000)
            self.assertEqual(ready['actual_kernel_limits']['memory.max'],'2999996416')
    def test_stale_different_VM_parent_CID_PID_swap_and_waiting_refuse_before_ready(self):
        changes=[lambda s,o,w,c,row:s['probe'].update(recorded_monotonic=time.monotonic()-31),
            lambda s,o,w,c,row:s['probe'].update(host_identity_sha256='0'*64),
            lambda s,o,w,c,row:o['docker_parent_ancestors'][0].update(identity=[1,99]),
            lambda s,o,w,c,row:o.update(container_id='f'*64),lambda s,o,w,c,row:o['host_init'].update(pid=999),
            lambda s,o,w,c,row:o['own_container_limits'].update({'memory.swap.max':'1'}),
            lambda s,o,w,c,row:w.update(request_sha256='0'*64),lambda s,o,w,c,row:w.update(native_started=True),
            lambda s,o,w,c,row:w.update(resource_phase='scan-baseline'),lambda s,o,w,c,row:w.update(core_contract_sha256='0'*64),
            lambda s,o,w,c,row:o['physical_bytes'].update(MemAvailable=r.REQUIRED_HOST_PARENT_HEADROOM-1)]
        for i,change in enumerate(changes):
            with self.subTest(i=i),tempfile.TemporaryDirectory() as temp:
                with self.assertRaises(Exception):self.startup(Path(temp).resolve(),change)
                self.assertFalse((Path(temp)/'worker-owned/resource-ready.json').exists())
    def test_precreated_ready_and_absent_waiting_never_reach_capture(self):
        with tempfile.TemporaryDirectory() as temp:
            p=Path(temp);m.write(p/'resource-ready.json',{});capture=Mock()
            with patch.object(m,'headroom_module',return_value=h),patch.object(h,'capture',capture):
                with self.assertRaises(m.Refusal):m.resource_startup_observer(Mock(),{},p,{},'f'*64,{'probe':{}},MANIFEST,Mock())
            capture.assert_not_called()
        with tempfile.TemporaryDirectory() as temp:
            capture=Mock()
            with patch.object(m,'headroom_module',return_value=h),patch.object(h,'capture',capture),patch.object(m.time,'monotonic',side_effect=[0,11]):
                with self.assertRaises(m.Refusal):m.resource_startup_observer(Mock(),{},Path(temp),{},'f'*64,{'probe':{}},MANIFEST,Mock())
            capture.assert_not_called()
    def test_start_observer_failure_preserves_owned_kill_and_retirement(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=lifecycle.LifecycleTests().fixture(Path(temp).resolve());c.measurement_only=True;c.create()
            with self.assertRaisesRegex(RuntimeError,'handoff failed'):
                c.start_wait(live_observer=lambda _:(_ for _ in ()).throw(RuntimeError('handoff failed')))
            c.cleanup();self.assertTrue(c.retired);self.assertIn(['kill','--signal','KILL','a'*64],s['calls'])
            self.assertIn(['rm','a'*64],s['calls']);self.assertLess(s['calls'].index(['kill','--signal','KILL','a'*64]),s['calls'].index(['wait','a'*64]))
    def test_worker_and_producer_reject_old_caps_and_preserve_database_child_binding(self):
        f=old.MemoryLifetimeTests();f.setUp()
        try:
            a=self.authority();f.witness.update(resource_phase='scan-candidate',resource_authority=a,
                requested_memory_bytes=r.SCAN_MEMORY,memory_page_size=4096,kernel_limits=r.limits_for_phase('scan-candidate'))
            f.witness['child']['memory']='2999996416'
            for name,value in r.limits_for_phase('scan-candidate').items():(f.cg/name).write_text(value+'\n')
            from osv_io import Budget
            with patch.object(r.os,'sysconf',return_value=4096):
                r.validate_witness('scan-candidate',a,f.witness);f.p.memory_event_snapshot(f.witness,Budget(5))
                for bad in ('3000000000','3221225472','2684354560','2147483648'):
                    (f.cg/'memory.max').write_text(bad+'\n')
                    with self.subTest(bad=bad),self.assertRaises(Exception):f.p.memory_event_snapshot(f.witness,Budget(5))
                (f.cg/'memory.max').write_text('2999996416\n')
                f.witness['child']['memory']='2684354560'
                with self.assertRaises(r.ResourceRefusal):r.validate_witness('scan-candidate',a,f.witness)
            # Exercise the full mocked validator -> native catalogs/Gov/scanner -> strict
            # report join, retaining GO5932 blocking under the same phase witness.
            f.witness['child']['memory']='2999996416';f.core=MANIFEST['core_contract_sha256']
            with patch.object(r.os,'sysconf',return_value=4096):
                out,blocking=f.run_producer()
            self.assertEqual(blocking,{('Go','example.test/dependency','1.0.0','GO-2026-5932')})
            req=json.loads((out/'database-request.json').read_text())
            self.assertEqual(req['memory_witness']['resource_authority'],a)
            self.assertEqual(req['memory_witness']['requested_memory_bytes'],3000000000)
        finally:f.doCleanups()
    def test_request_digest_binds_exact_written_bytes(self):
        with tempfile.TemporaryDirectory() as temp:
            p=Path(temp)/'request.json';sha=m.write_request_resource(p,{'phase':'scan-candidate'})
            self.assertEqual(sha,hashlib.sha256(p.read_bytes()).hexdigest())
            with self.assertRaises(FileExistsError):m.write_request_resource(p,{})
    def ready_worker(self,root,change=None,precreated=False):
        request={'resource_phase':'scan-candidate','resource_authority':self.authority(),'resource_nonce':'b'*32,'resource_container_id':'c'*64}
        handoff={'schema':'workflow.offline-scanner.resource-ready.v1','nonce':'b'*32,'container_id':'c'*64,
            'request_sha256':'f'*64,'resource_phase':'scan-candidate','resource_authority':self.authority(),
            'core_contract_sha256':MANIFEST['core_contract_sha256'],'actual_kernel_limits':r.limits_for_phase('scan-candidate'),
            'requested_memory_bytes':r.SCAN_MEMORY,'native_execution_permitted':True,'actual_parent_observation_sha256':'a'*64}
        if change:change(handoff)
        ready=root/'resource-ready.json'
        if precreated:m.write(ready,handoff)
        def path(raw):return root/Path(raw).name if str(raw).startswith('/out/') else Path(raw)
        def host(_):
            waiting=json.loads((root/'resource-waiting.json').read_text());self.assertFalse(waiting['native_started'])
            self.assertEqual(waiting['request_sha256'],'f'*64);m.write_request_resource(ready,handoff,committed=True)
        with patch.object(m,'headroom_module',return_value=h),patch.object(m,'Path',side_effect=path),patch.object(m.time,'sleep',side_effect=host):
            return m.wait_resource_ready(request,'f'*64,MANIFEST)
    def test_worker_ready_barrier_consumes_exact_buffer_only_after_waiting(self):
        with tempfile.TemporaryDirectory() as temp:
            p=Path(temp);sha=self.ready_worker(p)
            self.assertEqual(sha,hashlib.sha256((p/'resource-ready.json').read_bytes()).hexdigest())
    def test_worker_precreated_malformed_incomplete_wrong_phase_and_kernel_handoffs_refuse(self):
        with tempfile.TemporaryDirectory() as temp:
            with self.assertRaises(m.Refusal):self.ready_worker(Path(temp),precreated=True)
        changes=[lambda v:v.update(native_execution_permitted=False),lambda v:v.update(request_sha256='0'*64),
            lambda v:v.update(nonce='c'*32),lambda v:v.update(resource_phase='scan-baseline'),lambda v:v.update(container_id='bad'),lambda v:v.update(container_id='d'*64),
            lambda v:v.update(requested_memory_bytes=3221225472),lambda v:v.update(actual_parent_observation_sha256='bad'),
            lambda v:v.update(core_contract_sha256='0'*64),lambda v:v['actual_kernel_limits'].update({'memory.max':'3000000000'}),
            lambda v:v['actual_kernel_limits'].update({'memory.max':'3221225472'}),lambda v:v['actual_kernel_limits'].update({'memory.max':'2684354560'}),
            lambda v:v['actual_kernel_limits'].update({'memory.swap.max':'1'})]
        for i,change in enumerate(changes):
            with self.subTest(i=i),tempfile.TemporaryDirectory() as temp:
                with self.assertRaises(m.Refusal):self.ready_worker(Path(temp),change)
    def test_worker_absent_commit_has_bounded_refusal_without_native(self):
        request={'resource_phase':'scan-candidate','resource_authority':self.authority(),'resource_nonce':'b'*32,'resource_container_id':'c'*64}
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            def path(raw):return root/Path(raw).name if str(raw).startswith('/out/') else Path(raw)
            with patch.object(m,'headroom_module',return_value=h),patch.object(m,'Path',side_effect=path),patch.object(m.time,'monotonic',side_effect=[0,31]):
                with self.assertRaisesRegex(m.Refusal,'host-ready deadline'):m.wait_resource_ready(request,'f'*64,MANIFEST)
            self.assertFalse((root/'resource-ready.json').exists())
    def test_worker_duplicate_nonfinite_and_check_use_mutation_use_one_buffer(self):
        request={'resource_phase':'scan-candidate','resource_authority':self.authority(),'resource_nonce':'b'*32,'resource_container_id':'c'*64}
        for raw in (b'{"schema":1,"schema":2}',b'{"schema":NaN}'):
            with self.subTest(raw=raw),tempfile.TemporaryDirectory() as temp:
                root=Path(temp)
                def path(value):return root/Path(value).name if str(value).startswith('/out/') else Path(value)
                def host(_):
                    (root/'resource-ready.json').write_bytes(raw);(root/'resource-ready.json.committed').touch()
                with patch.object(m,'headroom_module',return_value=h),patch.object(m,'Path',side_effect=path),patch.object(m.time,'sleep',side_effect=host):
                    with self.assertRaises(h.Refusal):m.wait_resource_ready(request,'f'*64,MANIFEST)
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);original=h.file_bytes;bound=[]
            def changed_after_read(path,*args,**kwargs):
                raw,identity=original(path,*args,**kwargs)
                if Path(path).name=='resource-ready.json':bound.append(raw);Path(path).write_bytes(b'{"native_execution_permitted":false}')
                return raw,identity
            with patch.object(h,'file_bytes',side_effect=changed_after_read):sha=self.ready_worker(root)
            self.assertEqual(sha,hashlib.sha256(bound[0]).hexdigest())
            self.assertNotEqual(sha,hashlib.sha256((root/'resource-ready.json').read_bytes()).hexdigest())
    def test_closed_constructor_binds_decimal_request_and_page_effective_cap(self):
        with tempfile.TemporaryDirectory() as temp,patch.object(cmod,'eligible'),patch.object(r.os,'sysconf',return_value=4096),patch.dict(os.environ,{'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','GITHUB_SHA':'c'*40}):
            c=cmod.Container(Path(temp),HERE,production=True,resource_phase='scan-candidate',resource_authority=self.authority(),runner=Mock())
            self.assertEqual(c.requested_memory,3000000000);self.assertEqual(c.limits['memory.max'],'2999996416')
            c.runner.assert_not_called()
    def test_probe_export_owned_retirement_and_same_buffer_file_custody(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();output=root/'probe';output.mkdir(mode=0o700);(output/'raw').mkdir(mode=0o700)
            proof=self.proof();m.write(output/'docker-parent-headroom-proof.json',proof);(output/'raw/observed.raw').write_bytes(b'actual mocked bytes')
            with patch.object(m,'REPOSITORY',root):
                receipt=m.resource_probe_export(output,'scan-candidate',h)
            self.assertEqual((root/'offline-evidence/resource-guard-scan-candidate/raw/observed.raw').read_bytes(),b'actual mocked bytes')
            self.assertGreater(receipt['bytes'],0)
            bad=root/'bad';bad.mkdir(mode=0o700);m.write(bad/'docker-parent-headroom-proof.json',dict(proof,retirement_verified=False))
            with patch.object(m,'REPOSITORY',root),self.assertRaises(m.Refusal):m.resource_probe_export(bad,'scan-baseline',h)
    def test_source_framing_hashes_decorators_exact_bytes_and_all_function_names(self):
        raw=(HERE/'run.py').read_bytes();head=MANIFEST['trusted_files']['run-docker-headroom-proof.py']
        digest=m.resource_source_guard(raw,head)
        decorated=raw.replace(b'def resource_host_identity(',b'@owned_source_control\ndef resource_host_identity(')
        self.assertNotEqual(m.resource_source_guard(decorated,head),digest)
        with self.assertRaises(m.Refusal):m.resource_source_guard(raw.replace(b'def resource_host_identity(',b'def renamed_identity('),head)
        # Comments inside selected source are part of its exact authenticated bytes.
        commented=raw.replace(b'def resource_host_identity(helper,output,name):\n',b'def resource_host_identity(helper,output,name):\n    # exact source control\n')
        self.assertNotEqual(m.resource_source_guard(commented,head),digest)
    def test_source_framing_duplicate_async_invalid_utf8_and_hash_refuse(self):
        raw=(HERE/'run.py').read_bytes();head=MANIFEST['trusted_files']['run-docker-headroom-proof.py']
        for bad in (raw+b'\ndef resource_host_identity():\n    pass\n',raw.replace(b'def resource_host_identity(',b'async def resource_host_identity(')):
            with self.subTest(length=len(bad)),self.assertRaises(m.Refusal):m.resource_source_guard(bad,head)
        with self.assertRaises(UnicodeDecodeError):m.resource_source_guard(b'\xff',head)
        with self.assertRaises(m.Refusal):m.resource_source_guard(raw,'not-a-hash')
    def test_source_framing_rejects_invalid_bounds_and_duplicate_locations(self):
        import ast
        raw=(HERE/'run.py').read_bytes();tree=ast.parse(raw)
        node=next(x for x in tree.body if isinstance(x,ast.FunctionDef) and x.name=='resource_host_identity')
        original_last=node.end_lineno
        for change in ('past-end','overlap'):
            if change=='past-end':node.end_lineno=len(raw.splitlines())+1
            else:node.lineno=next(x for x in tree.body if isinstance(x,ast.FunctionDef) and x.name=='resource_headroom').lineno;node.end_lineno=original_last
            with self.subTest(change=change),patch.object(m.ast,'parse',return_value=tree),self.assertRaises(m.Refusal):
                m.resource_source_guard(raw,MANIFEST['trusted_files']['run-docker-headroom-proof.py'])
    def test_guard_digest_authenticates_and_consumes_one_same_owned_buffer(self):
        raw=(HERE/'run.py').read_bytes();head=MANIFEST['trusted_files']['run-docker-headroom-proof.py']
        helper=SimpleNamespace(file_bytes=Mock(return_value=(raw,{})))
        with patch.object(m,'headroom_module',return_value=helper),patch.object(m.Path,'read_bytes',side_effect=AssertionError('no reopen')):
            self.assertEqual(m.resource_guard_digest(MANIFEST),m.resource_source_guard(raw,head))
        helper.file_bytes.assert_called_once_with(HERE/'run.py',1048576,uid=os.getuid())
        helper.file_bytes.return_value=(raw+b'\n',{})
        with patch.object(m,'headroom_module',return_value=helper),self.assertRaisesRegex(m.Refusal,'authenticated resource guard source differs'):
            m.resource_guard_digest(MANIFEST)
    def test_five_actual_admission_pins_remain_null(self):
        for field in ('linux_runtime_acceptance_sha256','hosted_job_timing_acceptance_sha256','supplier_delivery_receipt_sha256',
            'source_object_delivery_acceptance_sha256','first_install_execution_receipt_sha256'):
            self.assertIsNone(MANIFEST[field])
        self.assertEqual(json.loads((HERE/'manual-proof-plan.json').read_text())['scan_seconds'],1800)

if __name__=='__main__':unittest.main()
