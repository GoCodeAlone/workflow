"""Finite split protocol controls; no GH/network/Docker/candidate execution."""
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import tempfile
import types
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('split_under_test', HERE / 'split-hosted.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
REPO_ROOT = HERE.parents[2]

class SplitControls(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='split-protocol-controls-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.current = {'GITHUB_REPOSITORY': m.REPO, 'GITHUB_SHA': 'a' * 40,
            'GITHUB_RUN_ID': '1234', 'GITHUB_RUN_ATTEMPT': '1', 'GITHUB_JOB': 'exact-candidate-runtime'}
        self.rows = {name: {'bytes': len((REPO_ROOT/name).read_bytes()),
            'sha256': m.sha((REPO_ROOT/name).read_bytes())} for name in m.BUNDLE}
        self.review = {'schema': 'workflow.GO5932.source-review-admission.v1', 'complete': True,
            'accepted': False, 'source_manifest_sha256': self.rows[m.PREFIX+'source-manifest.json']['sha256'],
            'cold_wrapper_sha256': self.rows[m.PREFIX+'hosted.py']['sha256'],
            'workflow_sha256': self.rows[m.WORKFLOW]['sha256'],
            'security_decision_sha256': 'b'*64, 'antagonistic_decision_sha256': 'c'*64,
            'security_critical': 0, 'security_important': 0, 'security_advisory': 0,
            'antagonistic_critical': 0, 'antagonistic_important': 0, 'antagonistic_advisory': 0,
            'allows_native_controls': True, 'candidate_runtime_authorized': True,
            'artifact_sha256': m.ASSET_SHA, 'operation': ['/candidate/wfctl','schema'], 'workflow_ref': m.REF,
            'split_wrapper_sha256': self.rows[m.PREFIX+'split-hosted.py']['sha256'],
            'source_binding_sha256': 'd'*64, 'source_rows': self.rows}
        now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
        self.trial = {'schema': 'workflow.GO5932.single-trial.v1', 'owner_approval_sha256': m.OWNER,
            'trial_id': 'e'*64, 'not_before_utc': (now-datetime.timedelta(minutes=1)).strftime('%Y-%m-%dT%H:%M:%SZ'),
            'expires_utc': (now+datetime.timedelta(minutes=30)).strftime('%Y-%m-%dT%H:%M:%SZ'),
            'head': self.current['GITHUB_SHA'], 'review_sha256': m.sha(self.raw(self.review)),
            'issuer_ledger_sha256': 'f'*64, 'dispatches': 1, 'accepted': False}
        self.environment()
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(m.platform, 'system', return_value='Linux').start()
        mock.patch.object(m.platform, 'machine', return_value='x86_64').start()

    @staticmethod
    def raw(value):
        return json.dumps(value, sort_keys=True, separators=(',',':')).encode()

    def environment(self):
        self.trial['review_sha256'] = m.sha(self.raw(self.review))
        env = {**self.current, 'GITHUB_EVENT_NAME':'workflow_dispatch', 'GITHUB_REF':m.REF,
            'RUNNER_ENVIRONMENT':'github-hosted', 'RUNNER_TEMP':str(self.base),
            'OSV_RUNTIME_SOURCE_REVIEW':self.raw(self.review).decode(),
            'OSV_RUNTIME_SOURCE_REVIEW_SHA':m.sha(self.raw(self.review)),
            'OSV_CANDIDATE_TRIAL':self.raw(self.trial).decode(),
            'OSV_CANDIDATE_TRIAL_SHA':m.sha(self.raw(self.trial))}
        if hasattr(self,'env_patch'):
            self.env_patch.stop()
        self.env_patch=mock.patch.dict(os.environ, env, clear=True);self.env_patch.start()
        self.addCleanup(self.env_patch.stop)

    def fixture_carrier(self):
        root=self.base/'carrier';root.mkdir(mode=0o700)
        for name in m.BUNDLE:
            target=root/'bundle'/name;target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
            target.write_bytes((REPO_ROOT/name).read_bytes());target.chmod(0o400)
        (root/'wfctl').write_bytes(b'explicit synthetic placeholder; never executed')
        (root/'wfctl').chmod(0o400)
        for name,value in [('review.json',self.review),('trial.json',self.trial)]:
            (root/name).write_bytes(self.raw(value));(root/name).chmod(0o400)
        receipt={'schema':'workflow.GO5932.split-acquisition.v1','complete':True,'accepted':False,
            'candidate_execution':False,'identity':dict(self.current,GITHUB_JOB='exact-candidate-acquisition'),
            'invocation_id':self.trial['trial_id'],'trial_sha256':m.sha(self.raw(self.trial)),
            'review_sha256':m.sha(self.raw(self.review)),'source_binding_sha256':self.review['source_binding_sha256'],
            'source_commit':m.SOURCE,'artifact_sha256':m.ASSET_SHA,'artifact_bytes':m.ASSET_SIZE,
            'release_id':m.RELEASE,'asset_id':m.ASSET,'draft':True,'published':False,
            'carrier_name':'candidate9819-carrier-1234-1','source_rows':self.rows}
        self.receipt=receipt
        self.rebind_receipt(root)
        os.environ.update(OSV_PRODUCER_OUTCOME='success',OSV_CARRIER_ID='9988',
            OSV_CARRIER_DIGEST='a'*64,OSV_INVOCATION_ID=self.trial['trial_id'])
        return root

    def rebind_receipt(self,root):
        f=root/'acquisition.json'
        if f.exists():f.chmod(0o600)
        f.write_bytes(self.raw(self.receipt));f.chmod(0o400)
        os.environ['OSV_PRODUCER_RECEIPT_SHA']=m.sha(self.raw(self.receipt))

    def check_carrier(self,root):
        return m.carrier(root,self.current,self.raw(self.review),m.sha(self.raw(self.review)),
            self.review,self.raw(self.trial),m.sha(self.raw(self.trial)),self.trial)

    def test_binary_escape_flag_only_for_supported_fixed_sink(self):
        endpoint='repos/'+m.REPO+'/releases/assets/'+str(m.ASSET)
        current=b'  --allow-escape-sequences   Allow printing terminal escape sequences\n'
        old=b'Usage: gh api <endpoint> [flags]\n'
        self.assertIn('--allow-escape-sequences',m.gh_get_arguments(endpoint,True,current))
        self.assertNotIn('--allow-escape-sequences',m.gh_get_arguments(endpoint,False,current))
        self.assertNotIn('--allow-escape-sequences',m.gh_get_arguments(endpoint,True,old))
        self.assertEqual(m.gh_get_arguments(endpoint,True,current)[-1],endpoint)

    def test_native_denial_is_attributed_to_exact_second_get(self):
        root=self.base/'acquisition';root.mkdir(mode=0o700)
        current=dict(self.current,GITHUB_JOB='exact-candidate-acquisition')
        state={'identity':current,'review_sha256':m.sha(self.raw(self.review)),
            'trial_sha256':m.sha(self.raw(self.trial)),'invocation_id':self.trial['trial_id'],
            'started_monotonic':m.time.monotonic()}
        (root/'state.json').write_bytes(self.raw(state));(root/'state.json').chmod(0o400)
        calls=[]
        fault=m.Refusal('synthetic native denial');fault.native={'native_exit':1,'stderr_bytes':54,
            'stderr_sha256':'a'*64,'stdout_bytes':0,'stdout_sha256':m.sha(b'')}
        def native(argv,env,seconds,limit,sink=None):
            calls.append((argv,dict(env)))
            if len(calls)==1:return b'  --allow-escape-sequences raw output\n',{'native_exit':0}
            if len(calls)==2:return self.raw({'id':m.RELEASE,'draft':True,'published_at':None,'tag_name':m.TAG}),{'native_exit':0}
            raise fault
        c=types.SimpleNamespace(Budget=m.Budget)
        h=types.SimpleNamespace(native=native,host_env=lambda:{'PATH':'/usr/bin:/bin','HOME':'/nonexistent'})
        original=Path.lstat
        def lstat(path,*args,**kwargs):
            if path==Path('/usr/bin/gh'):return types.SimpleNamespace(st_mode=0o100755,st_uid=0)
            return original(path,*args,**kwargs)
        with mock.patch.object(m,'helpers',return_value=(c,h)),mock.patch.object(Path,'lstat',lstat):
            with self.assertRaises(m.Refusal) as caught:
                m.acquire(root,current,self.raw(self.review),m.sha(self.raw(self.review)),self.review,
                    self.raw(self.trial),m.sha(self.raw(self.trial)),self.trial,'explicit-test-token')
        self.assertIs(caught.exception,fault)
        self.assertEqual(fault.native['fixed_get_endpoint'],'repos/'+m.REPO+'/releases/assets/'+str(m.ASSET))
        self.assertEqual(fault.native['fixed_operation'],'metadata')
        self.assertEqual(fault.native['completed_fixed_gets'],1)
        self.assertNotIn('GH_TOKEN',calls[0][1]);self.assertEqual(calls[1][1]['GH_TOKEN'],'explicit-test-token')
        self.assertEqual(calls[2][0][-1],fault.native['fixed_get_endpoint'])
        self.assertNotIn('explicit-test-token',str(fault.native))
        with self.assertRaises(m.Refusal):m.gh_get_arguments('repos/other/release',False,b'')

    def test_original_runtime_outer_bound_and_early_clock_preserved(self):
        workflow=(REPO_ROOT/m.WORKFLOW).read_text()
        self.assertIn('--kill-after=5s 90s',workflow)
        self.assertNotIn('--kill-after=5s 95s',workflow)
        source=(HERE/'split-hosted.py').read_text()
        self.assertLess(source.index('ENTRY_STARTED = time.monotonic()'),source.index('import datetime'))

    def test_complete_admission_both_exact_job_roles(self):
        for job in ['exact-candidate-runtime','exact-candidate-acquisition']:
            os.environ['GITHUB_JOB']=job
            self.assertEqual(m.admission(job)[0]['GITHUB_JOB'],job)

    def test_wrong_role_or_attempt_refuses(self):
        for name,value in [('GITHUB_JOB','other'),('GITHUB_RUN_ATTEMPT','2'),('GITHUB_REF','refs/heads/main')]:
            with self.subTest(name=name),mock.patch.dict(os.environ,{name:value}),self.assertRaises(m.Refusal):
                m.admission('exact-candidate-runtime')

    def test_expired_or_future_or_overlong_trial_refuses(self):
        for begin,end in [('2000-01-01T00:00:00Z','2000-01-01T01:00:00Z'),
                          ('2100-01-01T00:00:00Z','2100-01-01T01:00:00Z'),
                          ('2000-01-01T00:00:00Z','2100-01-01T00:00:00Z')]:
            self.trial.update(not_before_utc=begin,expires_utc=end);self.environment()
            with self.assertRaises(m.Refusal):m.admission('exact-candidate-runtime')

    def test_wrong_owner_head_ledger_or_trial_refuses(self):
        original=dict(self.trial)
        for key,value in [('owner_approval_sha256','0'*64),('head','b'*40),('issuer_ledger_sha256',''),('trial_id','')]:
            self.trial=dict(original,**{key:value});self.environment()
            with self.assertRaises(m.Refusal):m.admission('exact-candidate-runtime')

    def test_typed_dispatch_and_finding_counts_refuse(self):
        self.trial['dispatches']=True;self.environment()
        with self.assertRaises(m.Refusal):m.admission('exact-candidate-runtime')
        self.trial['dispatches']=1;self.review['security_critical']=False;self.environment()
        with self.assertRaises(m.Refusal):m.admission('exact-candidate-runtime')

    def test_unbound_metadata_or_helper_rows_refuse(self):
        self.review['source_rows']=dict(self.rows);self.review['source_rows'].pop(m.PREFIX+'candidate_controls.py');self.environment()
        with self.assertRaises(m.Refusal):m.admission('exact-candidate-runtime')

    def test_existing_bytecode_prefix_refuses_before_import(self):
        with mock.patch.object(m.os.path,'lexists',return_value=True),self.assertRaises(m.Refusal):
            m.admission('exact-candidate-runtime')

    def test_complete_carrier_preserves_acquisition_identity(self):
        root=self.fixture_carrier();result=self.check_carrier(root)
        self.assertEqual(result['identity']['GITHUB_JOB'],'exact-candidate-acquisition')
        self.assertFalse(result['candidate_execution'] or result['accepted'])

    def test_failed_producer_or_missing_service_provenance_refuses(self):
        root=self.fixture_carrier()
        for key,value in [('OSV_PRODUCER_OUTCOME','failure'),('OSV_CARRIER_ID',''),('OSV_CARRIER_DIGEST','bad')]:
            with mock.patch.dict(os.environ,{key:value}),self.assertRaises(m.Refusal):self.check_carrier(root)

    def test_changed_run_attempt_head_or_role_refuses(self):
        root=self.fixture_carrier();original=dict(self.receipt['identity'])
        for key,value in [('GITHUB_RUN_ID','1235'),('GITHUB_RUN_ATTEMPT','2'),('GITHUB_SHA','b'*40),('GITHUB_JOB','exact-candidate-runtime')]:
            self.receipt['identity']=dict(original,**{key:value});self.rebind_receipt(root)
            with self.assertRaises(m.Refusal):self.check_carrier(root)

    def test_wrong_invocation_or_receipt_digest_refuses(self):
        root=self.fixture_carrier()
        for key,value in [('OSV_INVOCATION_ID','0'*64),('OSV_PRODUCER_RECEIPT_SHA','0'*64)]:
            with mock.patch.dict(os.environ,{key:value}),self.assertRaises(m.Refusal):self.check_carrier(root)

    def test_extra_directory_file_symlink_or_missing_file_refuses(self):
        root=self.fixture_carrier()
        extra=root/'unbound';extra.mkdir()
        with self.assertRaises(m.Refusal):self.check_carrier(root)
        extra.rmdir();extra.write_bytes(b'unbound')
        with self.assertRaises(m.Refusal):self.check_carrier(root)
        extra.unlink();extra.symlink_to(root/'review.json')
        with self.assertRaises(m.Refusal):self.check_carrier(root)
        extra.unlink();(root/'trial.json').unlink()
        with self.assertRaises(m.Refusal):self.check_carrier(root)

    def test_changed_source_refuses_before_first_helper_import(self):
        root=self.fixture_carrier();f=root/'bundle'/m.PREFIX/'candidate_contract.py'
        f.chmod(0o600);f.write_bytes(f.read_bytes()+b'\n# tamper\n')
        with mock.patch.object(m,'load') as load,self.assertRaises(m.Refusal):
            m.helpers(root/'bundle',self.review)
        load.assert_not_called()

    def test_actual_numeric_step_output_is_written(self):
        path=self.base/'github-output';path.write_bytes(b'');path.chmod(0o600)
        with mock.patch.dict(os.environ,GITHUB_OUTPUT=str(path)):
            m.output({'receipt_sha256':'a'*64,'invocation_id':'b'*64})
        self.assertEqual(path.read_text(),'receipt_sha256='+'a'*64+'\ninvocation_id='+'b'*64+'\n')

    def test_no_arbitrary_public_source_endpoint(self):
        with mock.patch.object(m.http.client,'HTTPSConnection') as client,self.assertRaises(m.Refusal):
            m.public_source('a'*40,'unbound.py',m.Budget(5))
        client.assert_not_called()

    def test_ascii_utc_syntax_and_duplicate_json_refuse(self):
        for text in ['٢٠٢٦-10-11T00:00:00Z','2026-10-11T00:00:00+00:00']:
            with self.assertRaises(m.Refusal):m.utc(text)
        with self.assertRaises(m.Refusal):m.parse('{"id":1,"id":2}')

    def test_workflow_token_isolation_and_retention(self):
        text=(REPO_ROOT/m.WORKFLOW).read_text()
        acquisition=text.split('  exact-candidate-acquisition:',1)[1].split('  exact-candidate-runtime:',1)[0]
        runtime=text.split('  exact-candidate-runtime:',1)[1]
        self.assertIn('timeout-minutes: 6',acquisition);self.assertIn('contents: write',acquisition)
        self.assertIn('permissions: {}',runtime)
        self.assertNotIn('GH_TOKEN',runtime);self.assertNotIn('github.token',runtime)
        self.assertNotIn('actions/checkout',acquisition+runtime)
        self.assertEqual((acquisition+runtime).count('retention-days: 1'),3)
        self.assertIn('include-hidden-files: true',acquisition)
        self.assertIn('digest-mismatch: error',runtime);self.assertIn('merge-multiple: true',runtime)
        self.assertIn('3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c',runtime)

if __name__=='__main__':unittest.main()
