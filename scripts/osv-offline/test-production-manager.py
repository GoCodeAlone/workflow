"""Offline admission/retirement fault controls; no Docker or Go invocation."""
import copy
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
def module(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file)
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result);return result
m=module('manager','run.py');collector=module('collector','run-production-proof.py')
supplier=module('supplier','osv_supplier.py')

class ProductionTests(unittest.TestCase):
    def fixture(self,root):
        root=root.resolve();state=root/'state';state.mkdir(mode=0o700)
        private=state/'worker-owned';private.mkdir(mode=0o700)
        ledgers=state/'container-ledgers';ledgers.mkdir(mode=0o700)
        env={'GITHUB_SHA':'c'*40,'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1'}
        nonce='e'*32;labels={'workflow.osv.owner':nonce,'workflow.osv.commit':env['GITHUB_SHA']}
        name='wf-osv-123-1-'+nonce
        owner={'schema':'workflow.offline-scanner.container-ownership.v1','nonce':nonce,'name':name,
            'labels':labels,'output':str(private),'helper_root':str(m.HERE),'production':True}
        created={'container_id':'a'*64,'name':name,'labels':labels}
        retired={**created,'retired':True,'cleanup':{'container_id':'a'*64,'absent':True,'forced_removal':False}}
        m.write(ledgers/private.name,owner);m.write(ledgers/(private.name+'.created'),created)
        return state,private,ledgers,env,owner,created,retired

    def test_unretired_failure_publishes_no_raw_protocol(self):
        with tempfile.TemporaryDirectory() as temp:
            state,private,ledgers,env,*_=self.fixture(Path(temp))
            runs=state/'runs';runs.mkdir(mode=0o700)
            proof=runs/'osv-proof-owned';proof.mkdir(mode=0o700);(proof/'scanner.stderr').write_text('raw observation')
            destination=Path(temp).resolve()/'evidence';destination.mkdir(mode=0o700)
            with patch.dict(os.environ,env),patch.object(m,'state_path',return_value=state):
                with self.assertRaises(FileNotFoundError):m.preserve_incomplete_protocols('scan-candidate',destination)
            self.assertEqual(list(destination.iterdir()),[])

    def test_retirement_requires_full_id_owner_and_exact_absence(self):
        for change in ({'container_id':'b'*64},{'retired':False},{'cleanup':{'absent':False}},{'labels':{}}):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as temp:
                state,private,ledgers,env,owner,created,retired=self.fixture(Path(temp))
                m.write(ledgers/(private.name+'.retired'),dict(retired,**change))
                with patch.dict(os.environ,env):
                    with self.assertRaises(m.Refusal):m.require_retired_containers(state)
        with tempfile.TemporaryDirectory() as temp:
            state,private,ledgers,env,owner,created,retired=self.fixture(Path(temp))
            m.write(ledgers/(private.name+'.retired'),retired)
            with patch.dict(os.environ,env):self.assertTrue(m.require_retired_containers(state))

    def test_positive_worker_without_host_creation_ledger_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            state,private,ledgers,env,*_=self.fixture(Path(temp))
            (ledgers/private.name).unlink();(private/'worker-result.json').write_text('{"complete":true}')
            with patch.dict(os.environ,env):
                with self.assertRaises(m.Refusal):m.require_retired_containers(state)

    def test_core_contract_does_not_depend_on_receipt_index_but_binds_every_core_field(self):
        original={key:{'fixed':key} for key in m.CORE_FIELDS}
        first=m.core_contract(original)
        self.assertEqual(first,m.core_contract(dict(original,linux_runtime_acceptance_sha256='a'*64)))
        for key in m.CORE_FIELDS:
            changed=copy.deepcopy(original);changed[key]={'changed':True}
            self.assertNotEqual(first,m.core_contract(changed))

    def test_public_admission_cannot_use_missing_receipts(self):
        manifest={'permanent_install_authorized':True,'linux_runtime_acceptance_sha256':None}
        with patch.object(m.platform,'system',return_value='Linux'),patch.object(m.platform,'machine',return_value='x86_64'),patch.object(m,'validate_runtime') as runtime:
            with self.assertRaisesRegex(m.Refusal,'is unbound'):m.admission(manifest)
            runtime.assert_not_called()

    def test_collector_authenticates_manager_before_import(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp).resolve();(source/'run.py').write_text('raise RuntimeError("must never execute")')
            (source/'bootstrap-manifest.json').write_text(json.dumps({'trusted_files':{'run.py':'a'*64}}))
            with patch.object(collector,'HERE',source),patch.object(collector.importlib.util,'spec_from_file_location') as loader:
                with self.assertRaisesRegex(RuntimeError,'before import'):collector.manager()
                loader.assert_not_called()

    def test_recovery_proves_absence_of_host_recorded_id_without_retrying_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            state,private,ledgers,env,owner,created,retired=self.fixture(Path(temp))
            runtime={key:str(Path(temp).resolve()/key) for key in ('compiler_root','supplier_root','existing_module_cache','existing_build_cache')}
            runtime['supplier_delivery_proof_path']=str(Path(temp).resolve()/'supplier-delivery-proof.json')
            mounts,writable=m.worker_mounts(state,private,runtime)
            image='fixed image';config='fixed config'
            owner.update(image=image,image_config=config,creation_attempted=True,seconds=100,
                mounts=[[str(m.HERE),'/wf',True],[str(private),'/out',False],*[list((str(s),t,ro)) for s,t,ro in mounts]],
                carveouts=sorted(str(p) for p in writable),carveout_parent=str(state))
            (ledgers/private.name).write_text(json.dumps(owner))
            calls=[]
            class Fake:
                def __init__(self,*args,**kwargs):self.retired=False
                def inspect(self,identifier):calls.append(('inspect',identifier));return None
                def lifecycle_evidence(self):return {'container_id':self.identifier,'retired':self.retired}
                def cleanup(self):raise AssertionError('known absent ID needs no new create, signal or removal')
            fake=SimpleNamespace(Container=Fake,IMAGE=image,IMAGE_CONFIG=config)
            with patch.dict(os.environ,env),patch.dict(sys.modules,{'osv_container':fake}):
                self.assertTrue(m.close_all_containers(state,runtime,1000))
            self.assertEqual(calls,[('inspect','a'*64)])
            self.assertTrue(m.load(ledgers/(private.name+'.retired'))['cleanup']['absent'])

if __name__=='__main__':unittest.main()
