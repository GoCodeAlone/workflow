"""Offline lifecycle fault controls; never calls Docker or a namespace API."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('boundary',Path(__file__).with_name('osv_container.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class LifecycleTests(unittest.TestCase):
    def test_host_platform_gate_precedes_any_docker_call(self):
        with patch.object(m.platform,'system',return_value='Darwin'):
            with self.assertRaises(m.BoundaryError):m.Container(Path('/unused'),Path('/unused'))

    def fixture(self,root):
        helper=root/'helpers';helper.mkdir();(helper/'container-seccomp.json').write_text('{}')
        output=root/'output';output.mkdir(mode=0o700)
        state={'row':None,'calls':[],'fail_wait':False,'fail_create':False,'wrong_id':False,'foreign':False,'create_exception':False}
        holder={}
        def run(argv,seconds=15):
            state['calls'].append(argv[:]);c=holder['container']
            if argv[:2]==['container','inspect']:
                if state['row'] is None:return subprocess.CompletedProcess(argv,1,'','Error: No such container: '+argv[2]+'\n')
                return subprocess.CompletedProcess(argv,0,json.dumps([state['row']]),'')
            if argv[0]=='create':
                i='a'*64
                mounts=[{'Type':'bind','Source':str(s),'Destination':t,'RW':not ro} for s,t,ro in c.mounts]
                state['row']={'Id':i,'Name':'/'+c.name,'Image':m.IMAGE_CONFIG,'Config':{'Image':m.IMAGE,'Labels':dict(c.labels),
                    'User':str(os.getuid())+':'+str(os.getgid()),'Entrypoint':[c.payload[0]],'Cmd':c.payload[1:],
                    'Volumes':None,'Env':m.IMAGE_ENV,'Healthcheck':{'Test':['NONE']}},'Mounts':mounts,
                    'HostConfig':{'NetworkMode':'none','IpcMode':'private','CgroupnsMode':'private','ReadonlyRootfs':True,
                        'Privileged':False,'Memory':2147483648,'MemorySwap':2147483648,'PidsLimit':256,'NanoCpus':2000000000,
                        'PidMode':'','UTSMode':'','UsernsMode':'','CapAdd':None,'CapDrop':['ALL'],'Devices':[],
                        'ShmSize':67108864,'Tmpfs':{'/tmp':c.tmpfs.split(':',1)[1]},
                        'RestartPolicy':{'Name':'no'},'LogConfig':{'Type':'none'},
                        'SecurityOpt':['no-new-privileges=true','seccomp='+json.dumps({})]},
                    'State':{'Running':False,'Pid':0,'ExitCode':0},'RestartCount':0}
                if state['create_exception']:
                    state['late_row']=state['row'];state['row']=None
                    raise m.BoundaryError('create client timed out before daemon completion')
                c.cidfile.write_text(('b'*64 if state['wrong_id'] else i)+'\n')
                return subprocess.CompletedProcess(argv,1 if state['fail_create'] else 0,i+'\n','')
            if argv[0]=='start':
                state['row']['State'].update(Running=True,Pid=123)
                return subprocess.CompletedProcess(argv,0,argv[1]+'\n','')
            if argv[0]=='wait':
                if state['fail_wait']:
                    state['fail_wait']=False;raise m.BoundaryError('client timeout')
                state['row']['State'].update(Running=False,Pid=0)
                return subprocess.CompletedProcess(argv,0,'0\n','')
            if argv[0]=='kill':
                state['row']['State'].update(Running=False,Pid=0)
                return subprocess.CompletedProcess(argv,0,argv[-1]+'\n','')
            if argv[0]=='rm':
                state['row']=None
                return subprocess.CompletedProcess(argv,0,argv[1]+'\n','')
            raise AssertionError(argv)
        with patch.object(m,'eligible'),patch.dict(os.environ,{'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','GITHUB_SHA':'c'*40}):
            c=m.Container(output,helper,runner=run)
        holder['container']=c
        return c,state

    def test_success_joins_creation_exit_and_exact_id_nonforcing_removal(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create();code,row=c.start_wait()
            self.assertEqual(code,0);result=c.cleanup()
            self.assertTrue(result['absent']);self.assertFalse(result['forced_removal']);self.assertTrue(c.retired)
            self.assertIn(['rm','a'*64],s['calls'])
            self.assertFalse(any('-f' in a for a in s['calls']))

    def test_client_timeout_requires_daemon_container_kill_and_retirement(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create();s['fail_wait']=True
            with self.assertRaises(m.BoundaryError):c.start_wait()
            self.assertTrue(s['row']['State']['Running'])
            c.cleanup()
            self.assertIn(['kill','--signal','KILL','a'*64],s['calls'])
            self.assertIn(['rm','a'*64],s['calls'])

    def test_failed_create_with_resource_is_still_owned_and_removed_without_start(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));s['fail_create']=True
            with self.assertRaises(m.BoundaryError):c.create()
            self.assertEqual(c.identifier,'a'*64);c.cleanup()
            self.assertFalse(any(a[0]=='start' for a in s['calls']))
            self.assertIn(['rm','a'*64],s['calls'])

    def test_cid_mismatch_does_not_start_and_closes_only_authenticated_created_id(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));s['wrong_id']=True
            with self.assertRaises(m.BoundaryError):c.create()
            c.cleanup();self.assertIn(['rm','a'*64],s['calls'])
            self.assertFalse(any('b'*64 in a for a in s['calls']))

    def test_foreign_or_rebound_identity_never_signalled_or_removed(self):
        for field,value in (('Id','b'*64),('Image','sha256:'+'b'*64),('Name','/foreign')):
            with self.subTest(field=field),tempfile.TemporaryDirectory() as temp:
                c,s=self.fixture(Path(temp));c.create();s['row'][field]=value
                with self.assertRaises(m.BoundaryError):c.cleanup()
                self.assertFalse(c.retired)
                self.assertFalse(any(a[0] in ('kill','rm') for a in s['calls']))

    def test_resource_network_privilege_or_mount_drift_prevents_start(self):
        mutations=[('MemorySwap',4294967296),('NetworkMode','host'),('PidMode','host'),('Privileged',True),
                   ('ShmSize',2147483648),('Tmpfs',{'/tmp':'rw,size=8589934592'})]
        for key,value in mutations:
            with self.subTest(key=key),tempfile.TemporaryDirectory() as temp:
                c,s=self.fixture(Path(temp));c.create();s['row']['HostConfig'][key]=value
                with self.assertRaises(m.BoundaryError):c.start_wait()
                self.assertFalse(any(a[0]=='start' for a in s['calls']))
                c.cleanup()

    def test_missing_inspection_is_not_treated_as_successful_cleanup(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create();s['row']=None
            with self.assertRaises(m.BoundaryError):c.cleanup()
            self.assertFalse(c.retired)

    def test_effective_seccomp_contents_not_client_path_are_bound(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create()
            s['row']['HostConfig']['SecurityOpt']=['no-new-privileges=true','seccomp='+json.dumps({'defaultAction':'SCMP_ACT_ALLOW'})]
            with self.assertRaises(m.BoundaryError):c.start_wait()
            self.assertFalse(any(a[0]=='start' for a in s['calls']))
            c.cleanup()

    def test_uncertain_create_first_absence_cannot_credit_retirement(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));s['create_exception']=True
            with self.assertRaises(m.BoundaryError):c.create()
            with self.assertRaisesRegex(m.BoundaryError,'creation completion is uncertain'):c.cleanup()
            self.assertFalse(c.retired);self.assertIsNone(c.identifier)
            self.assertFalse(any(a[0] in ('start','kill','rm') for a in s['calls']))
            # The original daemon request can appear after the first absence.
            # Only then can its authenticated ID be retired, without a retry.
            s['row']=s['late_row'];receipt=c.cleanup()
            self.assertTrue(c.retired);self.assertEqual(receipt['container_id'],'a'*64)
            self.assertEqual(sum(a[0]=='create' for a in s['calls']),1)
            self.assertFalse(any(a[0]=='start' for a in s['calls']))

if __name__=='__main__':unittest.main()
