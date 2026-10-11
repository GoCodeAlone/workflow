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
                if state['row'] is None:return subprocess.CompletedProcess(argv,1,'[]\n','Error response from daemon: No such container: '+argv[2]+'\n')
                return subprocess.CompletedProcess(argv,0,json.dumps([state['row']]),'')
            if argv[0]=='create':
                i='a'*64
                mounts=[{'Type':'bind','Source':str(s),'Destination':t,'RW':not ro,'Propagation':'rprivate'} for s,t,ro in c.mounts]
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

    def test_documented_absence_formats_require_exact_target_and_empty_inspect_rows(self):
        identifier='a'*64
        for prefix in ('Error: No such container: ','Error: No such object: ','Error response from daemon: No such container: '):
            with self.subTest(prefix=prefix):
                self.assertTrue(m.confirmed_absence(subprocess.CompletedProcess([],1,'[]\n',prefix+identifier+'\n'),identifier))
        for rc,stdout,stderr in (
            (1,'[]','Error response from daemon: No such container: '+'b'*64),
            (1,'[]','permission denied while trying to connect to the Docker daemon socket'),
            (1,'[]','Cannot connect to the Docker daemon at unix:///var/run/docker.sock'),
            (1,'[]','Warning: diagnostic\nError response from daemon: No such container: '+identifier),
            (125,'[]','Error response from daemon: No such container: '+identifier),
            (1,'[{}]','Error response from daemon: No such container: '+identifier),
            (1,'null','Error response from daemon: No such container: '+identifier),
            (1,'','Error response from daemon: No such container: '+identifier)):
            with self.subTest(rc=rc,stdout=stdout,stderr=stderr):
                self.assertFalse(m.confirmed_absence(subprocess.CompletedProcess([],rc,stdout,stderr),identifier))

    def test_lifecycle_receipt_binds_real_attempt_id_retirement_and_inspection_hashes(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create();c.start_wait();c.cleanup();r=c.lifecycle_evidence()
            self.assertTrue(r['creation_attempted']);self.assertTrue(r['retired']);self.assertEqual(r['container_id'],'a'*64)
            self.assertEqual(r['inspections'][0]['classification'],'confirmed-absence')
            self.assertEqual(r['inspections'][-1]['target'],'a'*64)
            self.assertEqual(r['inspections'][-1]['exit_code'],1)
            self.assertEqual(len(r['inspections'][-1]['stderr_sha256']),64)
            self.assertNotIn('stderr',r['inspections'][-1])
            self.assertIn(['rm','a'*64],s['calls'])

    def test_seventeenth_inspection_is_refused_before_call_or_collection(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp))
            for _ in range(16):self.assertIsNone(c.inspect(c.name))
            with self.assertRaises(m.BoundaryError):c.inspect(c.name)
            self.assertEqual(len(s['calls']),16)
            self.assertEqual(len(c.lifecycle_evidence()['inspections']),16)

    def test_create_response_precedes_failing_inspect_and_publishes_no_raw_diagnostic(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));original=c.run
            stderr="option 'bind-recursive=readonly' requires 'bind-propagation=rprivate' to be specified in conjunction\n"+'\x1b[31mprivate daemon text'*10000
            def run(argv,seconds=15):
                if argv[0]=='create':return subprocess.CompletedProcess(argv,125,'',stderr)
                if c.creation_attempted:raise m.BoundaryError('post-create inspection failed')
                return original(argv,seconds)
            c.run=run
            with self.assertRaises(m.BoundaryError):c.create()
            r=c.lifecycle_evidence()['creation_response']
            self.assertTrue(r['response_observed']);self.assertEqual(r['exit_code'],125)
            self.assertEqual(r['stderr_bytes'],len(stderr.encode()))
            self.assertEqual(r['diagnostic_code'],'recursive-readonly-propagation-required')
            self.assertNotIn('stderr',r);self.assertNotIn('private daemon text',json.dumps(r))
            self.assertFalse(c.retired);self.assertIsNone(c.identifier)

    def test_create_timeout_is_unobserved_and_diagnostics_never_credit_retirement(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));s['create_exception']=True
            with self.assertRaises(m.BoundaryError):c.create()
            r=c.lifecycle_evidence()['creation_response']
            self.assertFalse(r['response_observed']);self.assertEqual(r['exception_type'],'BoundaryError')
            self.assertNotIn('exit_code',r)
            with self.assertRaises(m.BoundaryError):c.cleanup()
            self.assertFalse(c.retired)
        for text,code in [('permission denied','permission-denied'),('operation not permitted','operation-not-permitted'),('Cannot connect to the Docker daemon','connection-failed'),('unknown flag: --example','argument-error'),('arbitrary daemon text','unclassified')]:
            self.assertEqual(m.create_diagnostic(text),code)

    def test_recursive_readonly_requires_explicit_private_argument_and_actual_propagation(self):
        with tempfile.TemporaryDirectory() as temp:
            c,s=self.fixture(Path(temp));c.create()
            args=next(a for a in s['calls'] if a[0]=='create')
            ro_args=[args[i+1] for i,v in enumerate(args[:-1]) if v=='--mount' and ',readonly' in args[i+1]]
            self.assertTrue(ro_args);self.assertTrue(all(',bind-propagation=rprivate,bind-recursive=readonly' in v for v in ro_args))
            s['row']['Mounts'][0]['Propagation']='rshared'
            with self.assertRaises(m.BoundaryError):c.start_wait()
            self.assertFalse(any(a[0]=='start' for a in s['calls']))

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
