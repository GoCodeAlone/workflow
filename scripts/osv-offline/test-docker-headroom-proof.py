"""Offline bounded source controls; mocked Docker/root metadata, no runtime proof."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
def module(name, path):
    spec = importlib.util.spec_from_file_location(name,path)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value); return value
m = module('headroom_proof',HERE/'run-docker-headroom-proof.py')
lifecycle = module('headroom_lifecycle_controls',HERE/'test-osv-container.py')

class HeadroomControls(unittest.TestCase):
    def stat_bytes(self,pid=123,start=99,state='S'):
        values = [state,'42',str(pid),str(pid)] + ['0']*15 + [str(start)] + ['0']*32
        return (str(pid)+' (timeout) '+' '.join(values)+'\n').encode()

    def fixture(self,root):
        c,state = lifecycle.LifecycleTests().fixture(root)
        c.measurement_only = True; c.seconds = 25
        fixture = root/'inputs'; fixture.mkdir(mode=0o700)
        c.mounts.append((fixture,'/fixture',True)); return c,state

    def test_exact_sources_and_same_buffer_module_import(self):
        loaded,digest,manifest = m.sources()
        self.assertEqual(loaded.LIMITS,{'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'})
        self.assertEqual(set(manifest['files']),m.FILES)
        with self.assertRaises(m.Refusal): m.sources('0'*64)

    def test_dispatch_is_exclusive_default_off_and_has_no_build_or_compiler(self):
        workflow = (HERE.parents[1]/'.github/workflows/copilot-setup-steps.yml').read_text()
        job = workflow.split('  osv-docker-parent-headroom:\n',1)[1]
        self.assertIn('default: false',workflow.split('osv_docker_parent_headroom_proof:',1)[1].split('osv_container_proof:',1)[0])
        self.assertIn("github.event_name == 'workflow_dispatch'",job)
        self.assertIn('inputs.osv_production_proof == false',job)
        self.assertIn('inputs.osv_container_proof == false',job)
        self.assertIn('inputs.exact_candidate_runtime_proof == false',job)
        self.assertIn('contents: read',job); self.assertNotIn('contents: write',job)
        self.assertNotIn('setup-go',job); self.assertNotIn('prepare-supplier.py',job); self.assertNotIn('go build',job)
        for name in ('osv-container-prerequisite','osv-production-proof','exact-candidate-acquisition','exact-candidate-runtime'):
            guard = workflow.split('  '+name+':\n',1)[1].split('    runs-on:',1)[0]
            self.assertIn('inputs.osv_docker_parent_headroom_proof == false',guard)

    def test_measurement_constructor_rejects_production_quota_duration_or_mount_authority(self):
        with patch.object(lifecycle.m,'eligible'):
            for kwargs in ({'production':True,'seconds':25,'mounts':[(Path('/unused'),'/fixture',True)]},
                           {'seconds':30,'mounts':[(Path('/unused'),'/fixture',True)]},
                           {'seconds':25,'mounts':[]},
                           {'seconds':25,'mounts':[(Path('/unused'),'/go',True)]}):
                with self.subTest(kwargs=kwargs), self.assertRaises(lifecycle.m.BoundaryError):
                    lifecycle.m.Container('/unused','/unused',measurement_only=True,**kwargs)

    def test_live_callback_binds_full_id_and_retirement_without_host_pid_signals(self):
        with tempfile.TemporaryDirectory() as temp:
            c,state = self.fixture(Path(temp).resolve()); c.create(); seen=[]
            code,row = c.start_wait(live_observer=lambda row:seen.append((row['Id'],row['State']['Pid'])))
            self.assertEqual((code,seen),(0,[('a'*64,123)])); c.cleanup(); self.assertTrue(c.retired)
            self.assertIn('/wf/headroom-worker.py',c.payload); self.assertNotIn('/go',c.payload)
            self.assertEqual(sum(argv[0]=='start' for argv in state['calls']),1)
            self.assertFalse(any(argv[0]=='kill' for argv in state['calls']))

    def test_missing_callback_or_rebound_live_pid_fails_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            c,state = self.fixture(Path(temp).resolve()); c.create()
            with self.assertRaises(lifecycle.m.BoundaryError): c.start_wait()
            self.assertFalse(any(argv[0]=='start' for argv in state['calls']))
            with self.assertRaises(lifecycle.m.BoundaryError):
                c.start_wait(live_observer=lambda _:state['row']['State'].update(Pid=456))
            c.cleanup(); self.assertTrue(c.retired)
            self.assertIn(['kill','--signal','KILL','a'*64],state['calls'])

    def test_foreign_live_id_never_removed_or_signalled(self):
        with tempfile.TemporaryDirectory() as temp:
            c,state = self.fixture(Path(temp).resolve()); c.create()
            with self.assertRaises(lifecycle.m.BoundaryError):
                c.start_wait(live_observer=lambda _:state['row'].update(Id='b'*64))
            with self.assertRaises(lifecycle.m.BoundaryError): c.cleanup()
            self.assertFalse(any(argv[0] in ('kill','rm') for argv in state['calls']))

    def test_checked_file_refuses_symlink_hardlink_growth_and_owner_drift(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp).resolve()/'raw'; path.write_bytes(b'original')
            self.assertEqual(m.file_bytes(path,64,uid=os.getuid())[0],b'original')
            with self.assertRaises(m.Refusal): m.file_bytes(path,64,uid=os.getuid()+1)
            link = Path(temp).resolve()/'link'; link.symlink_to(path)
            with self.assertRaises(m.Refusal): m.file_bytes(link,64)
            link.unlink(); os.link(path,link)
            with self.assertRaises(m.Refusal): m.file_bytes(path,64)
            link.unlink()
            original = m.os.fdopen
            class ChangedRead:
                def __init__(self,stream): self.stream=stream
                def __enter__(self): return self
                def __exit__(self,*args): self.stream.close()
                def fileno(self): return self.stream.fileno()
                def read(self,limit):
                    raw=self.stream.read(limit); path.write_bytes(b'growing-after-read'); return raw
            with patch.object(m.os,'fdopen',side_effect=lambda *a,**k:ChangedRead(original(*a,**k))):
                with self.assertRaises(m.Refusal): m.file_bytes(path,64)

    def test_directory_fd_rejects_path_rebinding(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp).resolve()/'root'; path.mkdir(); directory=m.Directory(path,os.getuid())
            try:
                path.rename(Path(temp).resolve()/'old');path.mkdir()
                with self.assertRaises(m.Refusal): directory.check()
            finally: directory.close()

    def test_proc_pid_starttime_and_exact_cgroup_name(self):
        self.assertEqual(m.process_stat(self.stat_bytes(),123)['starttime_ticks'],99)
        self.assertNotEqual(m.process_stat(self.stat_bytes(start=100),123)['starttime_ticks'],99)
        self.assertEqual(m.cgroup_path(('0::/system.slice/docker-'+'a'*64+'.scope\n').encode(),'a'*64),Path('system.slice')/('docker-'+'a'*64+'.scope'))
        for raw in (b'0::/\n',b'0::/docker/../foreign\n',('0::/docker/'+'b'*64+'\n').encode()):
            with self.subTest(raw=raw),self.assertRaises(m.Refusal):m.cgroup_path(raw,'a'*64)
        with self.assertRaises(m.Refusal):m.process_stat(self.stat_bytes(pid=456),123)

    def test_valid_byte_dead_or_unknown_stat_states_refuse_even_with_running_inspect(self):
        for state in ('Z','X','x','Q','RR',''):
            with self.subTest(state=state),self.assertRaises(m.Refusal):m.process_stat(self.stat_bytes(state=state),123)

    def test_live_states_retained_but_scheduling_transitions_preserve_stable_identity(self):
        sleeping=m.process_stat(self.stat_bytes(state='S'),123)
        for state in sorted(m.STAT_STATES-m.DEAD_STAT_STATES):
            with self.subTest(state=state):
                live=m.process_stat(self.stat_bytes(state=state),123)
                self.assertEqual(live['state'],state);self.assertEqual(m.process_identity(live),m.process_identity(sleeping))
        running=m.process_stat(self.stat_bytes(state='R'),123)
        disk_wait=m.process_stat(self.stat_bytes(state='D'),123)
        self.assertNotEqual(running,sleeping);self.assertNotEqual(sleeping,disk_wait)
        self.assertEqual(m.process_identity(running),m.process_identity(disk_wait))

    def test_physical_parent_guard_excludes_own_cap_but_never_finite_parent_limit(self):
        parents=[{'group':'system.slice','memory_max_bytes':None,'memory_current_bytes':9*1024**3},
                 {'group':'.','memory_max_bytes':None,'memory_current_bytes':None,'uncontrolled_root_fields_observed_absent':True}]
        self.assertEqual(m.capacity(15*1024**3,parents),(15*1024**3,None))
        constrained=[{'group':'system.slice','memory_max_bytes':6*1024**3,'memory_current_bytes':3*1024**3}]
        estimate,maximum=m.capacity(15*1024**3,constrained)
        self.assertEqual((estimate,maximum),(3*1024**3,6*1024**3));self.assertLess(estimate,m.MINIMUM_OBSERVED_HEADROOM)
        self.assertLess(m.capacity(4*1024**3,parents)[0],m.MINIMUM_OBSERVED_HEADROOM)
        with self.assertRaises(m.Refusal):m.capacity(10,[{'group':'bad','memory_max_bytes':None,'memory_current_bytes':None}])

    def test_mocked_host_capture_preserves_raws_and_excludes_2g_leaf(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve(); c,state=self.fixture(root);c.create();c.run(['start',c.identifier]);row=c.live_identity()
            proc=root/'proc';(proc/'123').mkdir(parents=True);(proc/'123'/'stat').write_bytes(self.stat_bytes());(proc/'123'/'cgroup').write_text('0::/docker/'+'a'*64+'\n')
            (proc/'meminfo').write_text('MemTotal: 16777216 kB\nMemAvailable: 15728640 kB\n')
            cg=root/'cgroup';leaf=cg/'docker'/('a'*64);leaf.mkdir(parents=True)
            for name,value in {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256','cpu.max':'200000 100000'}.items():(leaf/name).write_text(value+'\n')
            for name,value in {'memory.max':'max','memory.current':'100'}.items():(cg/'docker'/name).write_text(value+'\n')
            out=root/'host-output';out.mkdir();(out/'raw').mkdir()
            real_dir=m.Directory;real_file=m.file_bytes
            next_state={'value':'R'}
            class MockRootUID(real_dir):
                def __init__(self,path,uid):super().__init__(path,os.getuid())
                def read(self,name,limit=4096,uid=0):
                    result=super().read(name,limit,uid=os.getuid())
                    if self.path==proc/'123' and name=='stat':
                        (proc/'123'/'stat').write_bytes(HeadroomControls().stat_bytes(state=next_state['value']))
                    return result
            def mocked_file(path,limit,**kwargs):
                if kwargs.get('uid')==0:kwargs['uid']=os.getuid()
                return real_file(path,limit,**kwargs)
            with patch.object(m,'Directory',MockRootUID),patch.object(m,'file_bytes',side_effect=mocked_file):
                value=m.capture(c,row,out,'before',proc_root=proc,cgroup_root=cg)
                next_state['value']='D'
                after=m.capture(c,row,out,'after',proc_root=proc,cgroup_root=cg)
                self.assertEqual((value['host_init']['state'],value['host_init_final']['state']),('S','R'))
                self.assertEqual((after['host_init']['state'],after['host_init_final']['state']),('R','D'))
                self.assertEqual(m.process_identity(value['host_init']),m.process_identity(after['host_init']))
                # A valid-byte zombie after the first checked read must refuse.
                bad=root/'bad-output';bad.mkdir();(bad/'raw').mkdir();next_state['value']='Z'
                with self.assertRaises(m.Refusal):m.capture(c,row,bad,'before',proc_root=proc,cgroup_root=cg)
            self.assertEqual(value['estimated_physical_and_docker_parent_headroom_bytes'],15*1024**3)
            self.assertTrue(value['observed_4_5GiB_prerequisite']);self.assertFalse(value['accepted'])
            self.assertTrue(value['own_container_memory_cap_excluded_from_parent_estimate'])
            self.assertTrue(all((out/item['relative_path']).read_bytes() and item['sha256'] for item in value['raw_custody']))
            c.cleanup();self.assertTrue(c.retired)

if __name__ == '__main__': unittest.main()
