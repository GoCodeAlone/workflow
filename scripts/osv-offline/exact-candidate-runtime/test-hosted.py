"""Hosted cold-start/delivery source controls: mock native calls only."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('hosted',HERE/'hosted.py')
host=importlib.util.module_from_spec(spec);spec.loader.exec_module(host)
sys.path.insert(0,str(HERE))
import candidate_contract as c

class Tests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.base=Path(self.temp.name).resolve()
        self.repo=self.base/'repo';self.source=self.repo/'scripts/osv-offline/exact-candidate-runtime'
        self.source.mkdir(parents=True)
        for name in host.CORE|{'source-manifest.json','hosted.py'}:
            (self.source/name).write_bytes((HERE/name).read_bytes());(self.source/name).chmod(0o644)
        workflow=self.repo/'.github/workflows/copilot-setup-steps.yml'
        workflow.parent.mkdir(parents=True);workflow.write_bytes(b'MOCK workflow, not an accepted source')
        manifest=hashlib.sha256((self.source/'source-manifest.json').read_bytes()).hexdigest()
        self.review={'schema':'workflow.GO5932.source-review-admission.v1','complete':True,'accepted':False,
            'source_manifest_sha256':manifest,'cold_wrapper_sha256':hashlib.sha256((self.source/'hosted.py').read_bytes()).hexdigest(),
            'workflow_sha256':hashlib.sha256(workflow.read_bytes()).hexdigest(),
            'security_decision_sha256':'a'*64,'antagonistic_decision_sha256':'b'*64,
            'security_critical':0,'security_important':0,'security_advisory':0,
            'antagonistic_critical':0,'antagonistic_important':0,'antagonistic_advisory':0,
            'allows_native_controls':True,'candidate_runtime_authorized':True,'artifact_sha256':c.SHA,
            'operation':['/candidate/wfctl','schema'],'workflow_ref':host.REF}
        self.env={'GITHUB_EVENT_NAME':'workflow_dispatch','GITHUB_REPOSITORY':host.REPOSITORY,'GITHUB_REF':host.REF,
            'GITHUB_JOB':'exact-candidate-runtime','RUNNER_ENVIRONMENT':'github-hosted','GITHUB_SHA':'c'*40,
            'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','RUNNER_TEMP':str(self.base)}
    def tearDown(self):self.temp.cleanup()
    def guarded(self):
        raw=json.dumps(self.review,sort_keys=True).encode();self.env.update(
            OSV_RUNTIME_SOURCE_REVIEW=raw.decode(),OSV_RUNTIME_SOURCE_REVIEW_SHA=hashlib.sha256(raw).hexdigest())
        return (patch.object(host,'HERE',self.source),patch.dict(os.environ,self.env,clear=True),
            patch.object(host.platform,'system',return_value='Linux'),patch.object(host.platform,'machine',return_value='x86_64'),
            patch.object(host.os,'geteuid',return_value=501))
    def test_all_eight_pins_before_loader_and_temporary_receipt_cleanup(self):
        loader=SimpleNamespace(verify_bundle=lambda *args:None)
        a,b,d,e,f=self.guarded()
        with a,b,d,e,f,patch.object(host,'load',return_value=loader) as load:
            contract,raw,digest,review=host.cold_guard()
        load.assert_called_once_with('launcher','run-candidate-runtime.py')
        self.assertFalse(list(self.base.glob('exact-review-*.json')))
    def test_corrupted_last_helper_refused_before_any_loader(self):
        (self.source/'osv_container.py').write_bytes(b'corrupt')
        a,b,d,e,f=self.guarded()
        with a,b,d,e,f,patch.object(host,'load') as load,self.assertRaises(host.Refusal):host.cold_guard()
        load.assert_not_called()
    def test_extra_token_field_never_retained_or_imported(self):
        self.review['GH_TOKEN']='MOCK-secret-never-retain'
        a,b,d,e,f=self.guarded()
        with a,b,d,e,f,patch.object(host,'load') as load,self.assertRaises(host.Refusal):host.cold_guard()
        load.assert_not_called();self.assertFalse(list(self.base.glob('exact-review-*.json')))
    def test_nonzero_advisory_and_wrong_workflow_refused(self):
        for key,value in (('security_advisory',1),('workflow_sha256','0'*64)):
            old=self.review[key];self.review[key]=value;a,b,d,e,f=self.guarded()
            with a,b,d,e,f,patch.object(host,'load') as load,self.assertRaises(host.Refusal):host.cold_guard()
            load.assert_not_called();self.review[key]=old
    def test_wrong_event_job_and_input_hash_refused(self):
        for key,value in (('GITHUB_EVENT_NAME','push'),('GITHUB_JOB','osv-production-proof')):
            a,b,d,e,f=self.guarded()
            with a,b,d,e,f,patch.dict(os.environ,{key:value}),patch.object(host,'load') as load,self.assertRaises(host.Refusal):host.cold_guard()
            load.assert_not_called()
        a,b,d,e,f=self.guarded()
        with a,b,d,e,f,patch.dict(os.environ,{'OSV_RUNTIME_SOURCE_REVIEW_SHA':'0'*64}),self.assertRaises(host.Refusal):host.cold_guard()
    def test_published_release_refused_before_asset_download(self):
        root=self.base/'evidence';root.mkdir(mode=0o700);(root/'gh-config').mkdir(mode=0o700)
        fake=SimpleNamespace(st_mode=0o100755,st_uid=0)
        with patch.object(host.Path,'lstat',return_value=fake),patch.object(host.Path,'resolve',lambda x:x),patch.object(host.c if hasattr(host,'c') else c,'owned'),patch.object(host,'native',return_value=(json.dumps({'id':host.RELEASE,'draft':False,'published_at':'now','tag_name':host.TAG}).encode(),{'native_exit':0})) as native,self.assertRaises(host.Refusal):
            host.deliver(root,c,{},'MOCK-token')
        native.assert_called_once()
    def test_real_file_stable_read_and_size_symlink_controls(self):
        path=self.base/'source.py';path.write_bytes(b'code');path.chmod(0o644)
        self.assertEqual(host.read_source(path,4),b'code')
        with self.assertRaises(host.Refusal):host.read_source(path,3)
        link=self.base/'link';link.symlink_to(path)
        with self.assertRaises(host.Refusal):host.read_source(link,4)
if __name__=='__main__':unittest.main()
