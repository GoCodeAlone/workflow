"""Offline admission/retirement fault controls; no Docker or Go invocation."""
import copy
from collections import Counter
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
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
    def test_bootstrap_and_workflow_guards_match_exact_sources(self):
        manifest=json.loads((HERE/'bootstrap-manifest.json').read_text())
        bootstrap=(HERE/'bootstrap.sh').read_text()
        workflow=(HERE.parent.parent/'.github/workflows/copilot-setup-steps.yml').read_text()
        for variable,file in [('runner_sha256','run.py'),('manifest_sha256','bootstrap-manifest.json')]:
            pins=re.findall(r"^"+variable+r"='([0-9a-f]{64})'$",bootstrap,re.MULTILINE)
            self.assertEqual(pins,[hashlib.sha256((HERE/file).read_bytes()).hexdigest()])
        for file,pin in manifest['trusted_files'].items():
            self.assertEqual(hashlib.sha256((HERE/file).read_bytes()).hexdigest(),pin,file)
        guards=re.findall(r'actual="\$\(/usr/bin/sha256sum (scripts/osv-offline/[^ )]+)\)"\n\s+test "\$\{actual%% \*\}" = ([0-9a-f]{64})',workflow)
        expected={'scripts/osv-offline/'+file for file in ('container-source-bindings.json','run-container-proof.py','prepare-supplier.py','bootstrap-manifest.json','run-production-proof.py','check-readonly-fixtures.py','run.py')}
        self.assertEqual({file for file,pin in guards},expected)
        self.assertEqual(Counter(file for file,pin in guards),
            Counter({file:1 for file in expected}))
        wrapper_pins=re.findall(r'actual="\$\(/usr/bin/sha256sum "\$wrapper"\)"\n\s+test "\$\{actual%% \*\}" = ([0-9a-f]{64})',workflow)
        wrapper=HERE/'exact-candidate-runtime/split-hosted.py'
        self.assertEqual(workflow.count('wrapper="$entry/scripts/osv-offline/exact-candidate-runtime/split-hosted.py"'),1)
        self.assertEqual(workflow.count('wrapper="$root/bundle/scripts/osv-offline/exact-candidate-runtime/split-hosted.py"'),4)
        self.assertEqual(wrapper_pins,[hashlib.sha256(wrapper.read_bytes()).hexdigest()]*5)
        for file,pin in guards:
            self.assertEqual(hashlib.sha256((HERE.parent.parent/file).read_bytes()).hexdigest(),pin,file)

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

    def test_public_preload_batches_only_declared_identities_and_retains_native_outputs(self):
        with tempfile.TemporaryDirectory() as temp:
            state=Path(temp).resolve()
            items=[{'module':'public.example/m'+str(i),'version':'v1.0.0','h1':'h1:'+str(i),'public_identity_reviewed':True} for i in range(17)]
            calls=[];online={'GOPROXY':'https://proxy.golang.org','GOSUMDB':'sum.golang.org'}
            def bounded(argv,cwd,env,seconds,budget):
                calls.append(argv);self.assertEqual(cwd,state);self.assertEqual(env,online);self.assertEqual(seconds,90)
                rows=[{'Path':r['module'],'Version':r['version'],'Sum':r['h1']} for r in items if r['module']+'@'+r['version'] in argv[4:]]
                return SimpleNamespace(returncode=0,stderr='',stdout='\n'.join(json.dumps(r) for r in rows))
            m.preload_public_modules({'items':items},Path('/fixed/go'),state,online,bounded,SimpleNamespace(left=lambda:100))
            self.assertEqual([len(c)-4 for c in calls],[16,1]);self.assertTrue(all(c[:4]==['/fixed/go','mod','download','-json'] for c in calls))
            proof=m.load(state/'public-module-preload-receipt.json');self.assertTrue(proof['complete']);self.assertEqual(proof['authenticated_count'],17)
            for row in proof['commands']:
                for stream in ('stdout','stderr'):self.assertEqual(row[stream+'_sha256'],m.digest(state/'public-preload-logs'/(str(row['batch'])+'.'+stream)))

    def test_public_preload_refuses_unadmitted_or_incomplete_native_identity_evidence(self):
        item={'module':'public.example/m','version':'v1.0.0','h1':'h1:fixed','public_identity_reviewed':True}
        good=json.dumps({'Path':item['module'],'Version':item['version'],'Sum':item['h1']})
        bad=[(1,'',good),(0,'warning',good),(0,'',''),(0,'',good+good),(0,'',good+'trailing'),
             (0,'',good.replace('h1:fixed','h1:wrong')),(0,'',good.replace('public.example/m','public.example/extra')),
             (0,'','{"Path":"public.example/m","Path":"public.example/m","Version":"v1.0.0","Sum":"h1:fixed"}')]
        for code,err,out in bad:
            with self.subTest(code=code,err=err,out=out),tempfile.TemporaryDirectory() as temp:
                state=Path(temp).resolve();fake=lambda *args:SimpleNamespace(returncode=code,stderr=err,stdout=out)
                with self.assertRaises(m.Refusal):m.preload_public_modules({'items':[item]},Path('/fixed/go'),state,{},fake,SimpleNamespace(left=lambda:100))
                self.assertFalse(m.load(state/'public-module-preload-receipt.json')['complete'])
        with tempfile.TemporaryDirectory() as temp:
            state=Path(temp).resolve()
            with self.assertRaises(m.Refusal):m.preload_public_modules({'items':[dict(item,public_identity_reviewed=False)]},Path('/fixed/go'),state,{},lambda *args:self.fail('must refuse before network'),SimpleNamespace(left=lambda:100))
            self.assertFalse((state/'public-preload-logs').exists())

    def test_startup_cleanup_is_idempotent_but_rejects_dirty_or_replaced_directory(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp).resolve();parent=root/'osv-python-owned';parent.mkdir(mode=0o700)
            with patch.dict(os.environ,{'RUNNER_TEMP':str(root)}),patch.object(m.sys,'argv',['run.py','prepare-differential']),patch.object(m.sys,'pycache_prefix',str(parent/'absent-cache')):
                m.cleanup_startup_directory();m.cleanup_startup_directory();self.assertFalse(parent.exists())
                parent.mkdir(mode=0o700);(parent/'unexpected').write_text('preserve')
                with self.assertRaises(OSError):m.cleanup_startup_directory()
                self.assertEqual((parent/'unexpected').read_text(),'preserve')
                (parent/'unexpected').unlink();parent.rmdir();parent.symlink_to(root,target_is_directory=True)
                with self.assertRaises(m.Refusal):m.cleanup_startup_directory()
                self.assertTrue(parent.is_symlink())

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



class MemoryLifetimeTests(unittest.TestCase):
    """Mocked native/owned-cgroup controls; no scanner, Go, Docker or network."""
    def setUp(self):
        import weakref
        import zipfile
        from datetime import datetime, timezone
        sys.path.insert(0,str(HERE))
        self.p=module('memory_producer','osv_producer.py')
        self.temp=tempfile.TemporaryDirectory(prefix='scanner-memory-controls-')
        self.addCleanup(self.temp.cleanup);self.addCleanup(patch.stopall)
        self.base=Path(self.temp.name).resolve();self.out=self.base/'runs';self.out.mkdir(mode=0o700)
        self.root=self.base/'source';self.root.mkdir();(self.root/'go.mod').write_text('module example.test/app\n')
        self.lock=self.root/'ui/package-lock.json';self.lock.parent.mkdir();self.lock.write_text('{}')
        self.tools=self.base/'tools';self.tools.mkdir()
        for name in ('osv-scanner','govulncheck'):(self.tools/name).write_bytes(name.encode())
        self.go=self.base/'compiler/go';self.go.parent.mkdir();self.go.write_bytes(b'compiler')
        self.core='a'*64;self.now=datetime.now(timezone.utc)
        self.advisory={'id':'GO-2026-5932','modified':'2026-10-10T00:00:00Z','affected':[
            {'package':{'ecosystem':'Go','name':'example.test/dependency'},
             'ranges':[{'type':'SEMVER','events':[{'introduced':'0'},{'fixed':'1.0.1'}]}]}]}
        self.db=self.base/'db';self.receipts={}
        for eco,advisory in [('Go',self.advisory),('npm',{'id':'NPM-CONTROL','modified':'2026-10-10T00:00:00Z',
            'affected':[{'package':{'ecosystem':'npm','name':'control-package'},'versions':['1.0.0']}]})]:
            file=self.db/'osv-scalibr'/eco/'all.zip';file.parent.mkdir(parents=True)
            with zipfile.ZipFile(file,'w') as archive:archive.writestr(advisory['id']+'.json',json.dumps(advisory))
            self.receipts[eco]={'url':'https://osv-vulnerabilities.storage.googleapis.com/'+eco+'/all.zip',
                'generation':'1','fetched_at':self.now.isoformat(),'sha256':hashlib.sha256(file.read_bytes()).hexdigest()}
        self.cg=self.base/'fake-cgroup';self.cg.mkdir()
        limits={'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}
        for name,value in limits.items():(self.cg/name).write_text(value+'\n')
        (self.cg/'cgroup.procs').write_text(str(os.getpid())+'\n')
        (self.cg/'memory.events').write_text('low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n')
        (self.cg/'memory.current').write_text('1048576\n');(self.cg/'memory.peak').write_text('2097152\n')
        (self.cg/'memory.stat').write_text('anon 524288\nfile 524288\nshmem 0\n')
        self.group=self.base/'fake-group';self.group.write_text('0::/\n')
        self.status=self.base/'fake-status';self.status.write_text('Name:\tcontrol\nVmRSS:\t1024 kB\nRssAnon:\t512 kB\n')
        patch.object(self.p,'CGROUP_ROOT',self.cg).start();patch.object(self.p,'PROC_CGROUP',self.group).start()
        patch.object(self.p,'PROC_STATUS',self.status).start()
        self.witness={'manager_pid':os.getpid(),'kernel_limits':limits,'child':{'group':'0::/\n'}}
        self.expected={str(self.root/'go.mod'):[('Go','example.test/dependency','1.0.0'),('Go','stdlib','1.27.2')],
            str(self.lock):[('npm','control-package','1.0.0')]}
        self.manifests={'go.mod':{'module':'example.test/app','module_names':['example.test/app','example.test/dependency']},
            'ui/package-lock.json':{}}
        self.env={'PATH':str(self.go.parent)+':/usr/bin:/bin','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off',
            'GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly','CGO_ENABLED':'0','GOVCS':'*:off',
            'GOPRIVATE':'','GONOPROXY':'none','GONOSUMDB':'none','GOMAXPROCS':'2','GOMEMLIMIT':'512MiB','GOGC':'50'}
        self.events=[];self.database_refs=[];self.native_done=False;self.mutation=None;self.child_code=0;self.native_code=0;self.scanner_exit=1;self.catalog_code=0
        original=self.p.database
        def database(*args):
            self.events.append(('database',self.native_done));result=original(*args)
            self.database_refs.append(weakref.ref(result));return result
        patch.object(self.p,'database',side_effect=database).start()
        patch.object(self.p,'catalog',return_value=(self.expected,self.manifests)).start()
        pins={n:hashlib.sha256((self.tools/n).read_bytes()).hexdigest() for n in ('osv-scanner','govulncheck')}
        patch.object(self.p,'PINS',{(self.p.platform.system(),self.p.platform.machine()):pins}).start()
        patch.object(self.p,'bounded_run',side_effect=self.native).start()

    def native(self,argv,cwd,env,timeout,budget,*,observer=None):
        from subprocess import CompletedProcess
        if argv[:2]==[str(self.go),'version']:
            return CompletedProcess(argv,0,'go version go1.27.2 linux/amd64\n','')
        self.assertEqual(env,self.env)
        if '--database-worker' in argv:
            self.assertEqual(argv[1:4],['-I','-S','-B']);self.events.append(('validator',False))
            if self.child_code:return CompletedProcess(argv,self.child_code,'','refused')
            if self.mutation=='request-core':
                request=Path(argv[-1]);value=json.loads(request.read_text());value['core_contract_sha256']='b'*64;request.write_text(json.dumps(value))
            with patch.object(self.p.os,'getppid',return_value=os.getpid()):
                self.p.database_worker(Path(argv[-1]),self.core)
            self.assertTrue(all(ref() is None for ref in self.database_refs))
            if self.mutation=='receipt':
                path=Path(argv[-1]).with_name('database-validation.json');value=json.loads(path.read_text());value['accepted']=True;path.write_text(json.dumps(value))
            return CompletedProcess(argv,0,'','')
        # No full Go dictionary or npm ID index may survive into any native child.
        self.assertTrue(all(ref() is None for ref in self.database_refs))
        if argv[0]==str(self.go):
            self.assertEqual(argv,[str(self.go),'list','-deps','-json','./...'])
            self.events.append(('catalog',False))
            if self.catalog_code:return CompletedProcess(argv,self.catalog_code,'','go: updates to go.mod needed, disabled by -mod=readonly')
            rows=[{'ImportPath':'example.test/app','Module':{'Path':'example.test/app'}},
                {'ImportPath':'example.test/dependency','DepOnly':True,'Module':{'Path':'example.test/dependency','Version':'v1.0.0'}}]
            return CompletedProcess(argv,0,'\n'.join(json.dumps(row) for row in rows),'')
        if argv[0]==str(self.tools/'govulncheck'):
            self.assertEqual(argv[1::2][:2],['-db','-C']);self.assertEqual(argv[-2:],['-json','./...'])
            self.events.append(('govulncheck',False))
            if self.mutation=='oom':(self.cg/'memory.events').write_text('low 0\nhigh 0\nmax 1\noom 1\noom_kill 1\noom_group_kill 0\n')
            config={'protocol_version':'v1.0.0','scanner_name':'govulncheck','scanner_version':'v1.6.0',
                'go_version':'go1.27.2','scan_level':'symbol','scan_mode':'source','db':argv[2]}
            sbom={'roots':['example.test/app'],'go_version':'go1.27.2','modules':[
                {'path':'example.test/app'},{'path':'example.test/dependency','version':'v1.0.0'},
                {'path':'stdlib','version':'v1.27.2'}]}
            return CompletedProcess(argv,self.native_code,json.dumps({'config':config})+json.dumps({'SBOM':sbom}),'')
        self.assertEqual(argv[0],str(self.tools/'osv-scanner'));self.events.append(('scanner',False))
        self.assertIn('--call-analysis=go',argv);self.assertIn('--all-packages',argv)
        self.assertEqual(argv[-4:],['-L',str(self.root/'go.mod'),'-L',str(self.lock)])
        proof=next(self.out.glob('osv-proof-*'))
        if self.mutation=='capture':(proof/'root0-govulncheck.stdout').write_text('{}')
        if self.mutation=='flat':(proof/'go-db/GO-2026-5932.json').write_text('{}')
        self.native_done=True
        packages=[{'package':{'ecosystem':'Go','name':'stdlib','version':'1.27.2'}},
            {'package':{'ecosystem':'Go','name':'example.test/dependency','version':'1.0.0'},
             'vulnerabilities':[self.advisory],'groups':[{'ids':['GO-2026-5932'],
             'experimental_analysis':{'GO-2026-5932':{'called':False}}}]}]
        if self.mutation=='metadata':packages[1]['vulnerabilities']=[dict(self.advisory,summary='changed metadata')]
        report={'results':[{'source':{'type':'lockfile','path':str(self.root/'go.mod')},'packages':packages},
            {'source':{'type':'lockfile','path':str(self.lock)},'packages':[
            {'package':{'ecosystem':'npm','name':'control-package','version':'1.0.0'}}]}]}
        return CompletedProcess(argv,self.scanner_exit,json.dumps(report),'')

    def run_producer(self):
        return self.p.run_one(self.root,['go.mod','ui/package-lock.json'],self.tools,self.go,
            self.db,self.receipts,self.now,self.out,self.env,cgroup_witness=self.witness,
            core_contract_sha256=self.core)

    def test_complete_pipeline_defers_raw_records_and_retains_advisory_only_block(self):
        out,blocking=self.run_producer()
        self.assertEqual(blocking,{('Go','example.test/dependency','1.0.0','GO-2026-5932')})
        self.assertEqual([row[0] for row in self.events],['validator','database','database','catalog','govulncheck','scanner','database','database'])
        self.assertTrue(all(flag for kind,flag in self.events[-2:]))
        proof=json.loads((out/'database-validation.json').read_text())
        self.assertFalse(proof['accepted']);self.assertEqual(proof['retained']['counts'],{'Go':1,'npm':1})
        self.assertTrue(proof['retained']['npm_values_are_None'])
        self.assertEqual(proof['retained']['validated_memory']['manager_memory_bytes']['VmRSS'],1048576)
        for name in ['database-validation','root0-source-catalog','root0-govulncheck','scanner','analysis-validation']:
            after=json.loads((out/(name+'.memory-after.json')).read_text())
            self.assertFalse(after['accepted']);self.assertEqual(after['snapshot']['measurements_bytes']['memory.current'],1048576)
        self.assertTrue(json.loads((out/'receipt.json').read_text())['complete'])

    def test_failed_validation_never_starts_go_or_scanner(self):
        self.child_code=2
        with self.assertRaisesRegex(Exception,'validation child failed'):self.run_producer()
        self.assertEqual([row[0] for row in self.events],['validator'])

    def test_native_failure_never_rehydrates_or_launches_full_scanner(self):
        self.native_code=-9
        with self.assertRaisesRegex(Exception,'govulncheck failed'):self.run_producer()
        self.assertNotIn('scanner',[row[0] for row in self.events])
        self.assertEqual(sum(row[0]=='database' for row in self.events),2)
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_deferred_capture_tamper_prevents_completion(self):
        self.mutation='capture'
        with self.assertRaisesRegex(Exception,'capture changed'):self.run_producer()
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_export_tamper_prevents_rehydration_and_completion(self):
        self.mutation='flat'
        with self.assertRaisesRegex(Exception,'database changed during'):self.run_producer()
        self.assertEqual(sum(row[0]=='database' for row in self.events),2)
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_measurements_are_bound_and_malformed_fields_refuse(self):
        from osv_io import Budget
        first=self.p.memory_event_snapshot(self.witness,Budget(5))
        self.assertEqual(first['memory_stat_bytes'],{'anon':524288,'file':524288,'shmem':0})
        for path,raw in [(self.cg/'memory.current','-1\n'),(self.cg/'memory.peak','0\n'),
            (self.cg/'memory.stat','anon 1\nanon 2\nfile 0\nshmem 0\n'),(self.status,'VmRSS: 1 MB\nRssAnon: 1 kB\n')]:
            old=path.read_bytes();path.write_text(raw)
            with self.subTest(path=path),self.assertRaises(Exception):self.p.memory_event_snapshot(self.witness,Budget(5))
            path.write_bytes(old)

    def test_measurement_links_owner_quota_and_deadline_refuse_before_native(self):
        from osv_io import Budget
        current=self.cg/'memory.current';raw=current.read_bytes();current.unlink();current.symlink_to(self.status)
        with self.assertRaises(OSError):self.p.memory_event_snapshot(self.witness,Budget(5))
        current.unlink();current.write_bytes(raw)
        for witness in [dict(self.witness,manager_pid=-1),dict(self.witness,kernel_limits={})]:
            with self.subTest(witness=witness),self.assertRaises(Exception):self.p.memory_event_snapshot(witness,Budget(5))
        budget=Budget(-1)
        with self.assertRaises(Exception):self.p.memory_event_snapshot(self.witness,budget)
        self.assertEqual(self.events,[])

    def test_database_unknown_applicability_is_not_waived(self):
        import zipfile
        path=self.db/'osv-scalibr/Go/all.zip';advisory=copy.deepcopy(self.advisory)
        del advisory['affected'][0]['ranges']
        with zipfile.ZipFile(path,'w') as archive:archive.writestr(advisory['id']+'.json',json.dumps(advisory))
        self.receipts['Go']['sha256']=hashlib.sha256(path.read_bytes()).hexdigest()
        with self.assertRaisesRegex(Exception,'lacks applicability'):self.run_producer()
        self.assertNotIn('govulncheck',[row[0] for row in self.events])
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_wrong_core_refuses_before_any_native_child(self):
        with self.assertRaisesRegex(Exception,'exact trusted core'):
            self.p.run_one(self.root,[],self.tools,self.go,self.db,self.receipts,self.now,self.out,self.env)
        self.assertEqual(self.events,[])

    def test_validation_receipt_tamper_and_wrong_core_stop_before_go(self):
        for kind in ('receipt','request-core'):
            self.mutation=kind
            with self.subTest(kind=kind),self.assertRaises(Exception):self.run_producer()
            self.assertNotIn('govulncheck',[row[0] for row in self.events])
            self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_advisory_only_scanner_exit_zero_is_not_accepted(self):
        self.scanner_exit=0
        with self.assertRaisesRegex(Exception,'exit contradicts'):self.run_producer()
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_full_raw_go_metadata_join_still_rejects_changed_advisory(self):
        self.mutation='metadata'
        with self.assertRaisesRegex(Exception,'metadata differs'):self.run_producer()
        self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_readonly_source_catalog_error_remains_blocking(self):
        self.catalog_code=1
        with self.assertRaisesRegex(Exception,'source/package loading failed'):self.run_producer()
        self.assertNotIn('govulncheck',[row[0] for row in self.events])
        self.assertEqual(sum(row[0]=='database' for row in self.events),2)

    def test_oom_measurement_retains_failed_native_exit_without_acceptance(self):
        self.mutation='oom';self.native_code=-9
        with self.assertRaisesRegex(Exception,'govulncheck failed'):self.run_producer()
        path=next(self.out.glob('*/root0-govulncheck.memory-after.json'))
        value=json.loads(path.read_text());self.assertEqual(value['native_exit'],-9)
        self.assertEqual(value['delta']['oom_kill'],1);self.assertFalse(value['accepted'])
        self.assertIn('specific killed PID is not established',value['scope'])

    def test_owned_deferred_files_reject_links_and_hardlink_aliases(self):
        f=self.base/'owned.json';f.write_text('{}');f.chmod(0o600)
        alias=self.base/'alias.json';os.link(f,alias)
        with self.assertRaises(Exception):self.p.read_owned(f)
        alias.unlink();alias.symlink_to(f)
        with self.assertRaises(Exception):self.p.read_owned(alias)

    def test_decreasing_memory_peak_preserves_incomplete_diagnostic(self):
        from osv_io import Budget
        out=self.out/'metric';out.mkdir(mode=0o700);budget=Budget(5)
        before=self.p.begin_memory(out,'scanner',self.witness,budget,['fixed'])
        (self.cg/'memory.peak').write_text('1048576\n')
        with self.assertRaisesRegex(Exception,'peak decreased'):
            self.p.end_memory(out,'scanner',self.witness,budget,before,0,True)
        self.assertFalse(json.loads((out/'scanner.memory-after.json').read_text())['complete'])

    def test_memory_before_receipt_change_refuses_without_positive_diagnostic(self):
        from osv_io import Budget
        out=self.out/'metric';out.mkdir(mode=0o700);budget=Budget(5)
        before=self.p.begin_memory(out,'scanner',self.witness,budget,['fixed'])
        (out/'scanner.memory-before.json').write_text('{}')
        with self.assertRaisesRegex(Exception,'before receipt changed'):
            self.p.end_memory(out,'scanner',self.witness,budget,before,0,True)
        value=json.loads((out/'scanner.memory-after.json').read_text())
        self.assertFalse(value['complete'] or value['accepted'])

    def test_capture_check_use_race_consumes_only_the_verified_buffer(self):
        out=self.out/'race';out.mkdir(mode=0o700);label='root0-govulncheck'
        for suffix,text in [('stdout','authenticated'),('rc','0\n'),('stderr','')]:
            f=out/(label+'.'+suffix);f.write_text(text);f.chmod(0o600)
        binding=self.p.capture_binding(out,label);original=self.p.read_owned;fired=[]
        def mutate_after_read(path,*args):
            raw=original(path,*args)
            if path.name.endswith('.stdout') and not fired:
                fired.append(True);path.write_text('unhashed replacement')
            return raw
        with patch.object(self.p,'read_owned',side_effect=mutate_after_read):
            result=self.p.deferred_capture(out,label,binding)
        self.assertEqual(fired,[True]);self.assertEqual(result,('authenticated','0\n',''))

    def test_report_check_use_race_never_accepts_a_reopened_buffer(self):
        original_text=Path.read_text;original_owned=self.p.read_owned;fired=[]
        def mutate(path,raw):
            if path.name=='results.json' and not fired:
                fired.append(True);value=json.loads(raw);value['unhashed_race_marker']=True
                path.write_text(json.dumps(value))
        def text(path,*args,**kwargs):
            raw=original_text(path,*args,**kwargs);mutate(path,raw);return raw
        def owned(path,*args):
            raw=original_owned(path,*args);mutate(path,raw);return raw
        with patch.object(Path,'read_text',text),patch.object(self.p,'read_owned',side_effect=owned):
            with self.assertRaisesRegex(Exception,'report changed after analysis'):self.run_producer()
        self.assertEqual(fired,[True]);self.assertFalse(any(self.out.glob('*/receipt.json')))

    def test_owned_fd_growth_replacement_and_content_mutation_refuse(self):
        original=self.p.os.fdopen
        for kind in ('growth','replacement','same-size'):
            with self.subTest(kind=kind):
                f=self.base/('fd-'+kind);f.write_bytes(b'original');f.chmod(0o600)
                class Proxy:
                    def __init__(proxy,file):proxy.file=file
                    def __enter__(proxy):proxy.file.__enter__();return proxy
                    def __exit__(proxy,*args):return proxy.file.__exit__(*args)
                    def fileno(proxy):return proxy.file.fileno()
                    def read(proxy,count):
                        raw=proxy.file.read(count)
                        if kind=='growth':f.write_bytes(b'original extra')
                        elif kind=='same-size':f.write_bytes(b'changed!')
                        else:
                            other=f.with_suffix('.replacement');other.write_bytes(b'original');other.chmod(0o600)
                            os.replace(other,f)
                        return raw
                with patch.object(self.p.os,'fdopen',side_effect=lambda *args:Proxy(original(*args))):
                    with self.assertRaisesRegex(Exception,'changed during read'):self.p.read_owned(f)

    def test_same_buffer_report_parser_keeps_duplicate_nonfinite_and_shape_controls(self):
        from osv_completion import parse_report_text, IncompleteScan
        self.assertEqual(parse_report_text('{"results":[]}'),{'results':[]})
        for raw in ('{"results":[],"results":[]}','{"results":NaN}','{bad'):
            with self.subTest(raw=raw),self.assertRaises(IncompleteScan):parse_report_text(raw)

class FiniteBaselineTests(unittest.TestCase):
    """Source/provenance controls only; no Git/Go/scanner/network children."""
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.state=Path(self.temp.name).resolve();self.state.chmod(0o700)
        self.plan=json.loads((HERE/'manual-proof-plan.json').read_text())
        self.manifest=json.loads((HERE/'bootstrap-manifest.json').read_text())
        sys.path.insert(0,str(HERE));self.addCleanup(lambda:sys.path.remove(str(HERE)))

    def config(self,change=None):
        plan=copy.deepcopy(self.plan);manifest=copy.deepcopy(self.manifest)
        if change is not None:change(plan['baseline_projection'],manifest)
        manifest['finite_baseline_projection_sha256']=hashlib.sha256(json.dumps(
            plan['baseline_projection'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
        with patch.object(m,'load',return_value=plan):return m.finite_projection(manifest,manual=True)

    def fixture(self):
        value=copy.deepcopy(self.plan['baseline_projection'])
        root=self.state/'baseline-source';root.mkdir(mode=0o700)
        files={};old_bytes={};total=0
        for name,change in value['files'].items():
            raw=('original fixture '+name+'\n').encode();old_bytes[name]=raw
            path=root/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw);path.chmod(0o644)
            change['old']={'git_mode':'100644','git_blob':hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest(),
                'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
            files[name]={key:change['old'][key] for key in ('git_mode','git_blob','sha256')};total+=len(raw)
            if name.endswith('go.mod'):value['original_fixture_catalog'][name]['sha256']=change['old']['sha256']
        for name,raw,mode in [('go.mod',b'module public.example/unchanged\n',0o644),('main.sh',b'#!/bin/sh\nexit 0\n',0o755)]:
            path=root/name;path.write_bytes(raw);path.chmod(mode);total+=len(raw)
            files[name]={'git_mode':'100755' if mode==0o755 else '100644',
                'git_blob':hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest(),'sha256':hashlib.sha256(raw).hexdigest()}
        value['file_count']=len(files);value['original_tree']=m.git_tree(files)
        derived=copy.deepcopy(files)
        for name,row in value['files'].items():derived[name]={key:row['new'][key] for key in ('git_mode','git_blob','sha256')}
        value['derived_tree']=m.git_tree(derived)
        archive=self.state/'baseline.tar';archive.write_bytes(b'authenticated pristine archive stand-in')
        source={'commit':value['original_ref'],'tree':value['original_tree'],'archive_sha256':m.digest(archive),
            'files':files,'manifests':sorted([name for name in files if name.endswith('go.mod')]),'bytes':total}
        return value,source,old_bytes

    def test_exact_finite_plan_binds_payloads_two_trees_and_authority(self):
        self.assertEqual(self.config(),self.plan['baseline_projection'])
        for change in (
            lambda v,_:v.update(extra=True),lambda v,_:v.update(accepted=True),
            lambda v,_:v.update(original_ref='a'*40),lambda v,_:v.update(derived_tree='b'*40),
            lambda v,_:v.update(patch_sha256='c'*64),lambda v,_:v.update(disposition_sha256='d'*64),
            lambda v,_:v.update(file_count=3524),lambda v,_:v['files'].update({'go.mod':next(iter(v['files'].values()))}),
            lambda v,_:v['files'].pop(next(iter(v['files']))),lambda v,man:man['prepared_source_refs'].update(candidate='e'*40),
            lambda v,_:v['payloads'].update({next(iter(v['payloads'])):'substituted'}),
            lambda v,_:next(iter(v['files'].values()))['new'].update(git_mode='100755'),
            lambda v,_:v['original_fixture_catalog'].clear()):
            with self.subTest(change=change),self.assertRaises(Exception):self.config(change)

    def test_finite_subject_refuses_normal_event_and_install_paths(self):
        with self.assertRaisesRegex(m.Refusal,'restricted'):m.finite_projection(self.manifest,manual=False)
        with self.assertRaisesRegex(m.Refusal,'normal-event'):m.admission(self.manifest)

    def test_only_four_substitutions_keep_original_inventory_and_executable_mode(self):
        value,source,old=self.fixture();original=copy.deepcopy(source)
        m.apply_fixture_projection(self.state,source,value)
        self.assertEqual(source['original_git_export'],original)
        self.assertEqual(set(source['files']),set(original['files']))
        self.assertEqual([name for name in source['files'] if source['files'][name]!=original['files'][name]],list(value['files']))
        self.assertEqual((self.state/'baseline-source/main.sh').stat().st_mode&0o777,0o755)
        self.assertNotEqual(source['tree'],original['tree'])
        for name,raw in old.items():
            if name.endswith('go.sum'):
                self.assertEqual((self.state/'baseline-original-fixture-metadata'/(Path(name).parent.name+'.go.sum')).read_bytes(),raw)
        with patch.object(m,'finite_projection',return_value=value):
            m.verify_fixture_projection(self.state,'baseline',source,self.manifest,manual=True)

    def test_partial_mutation_extra_path_or_pristine_conflation_refuses(self):
        value,source,_=self.fixture();m.apply_fixture_projection(self.state,source,value)
        bad=[]
        v=copy.deepcopy(source);v['tree']=v['original_git_export']['tree'];bad.append(v)
        v=copy.deepcopy(source);v['files']['main.sh']['sha256']='f'*64;bad.append(v)
        v=copy.deepcopy(source);v['original_git_export']['files']['go.mod']['git_blob']='f'*40;bad.append(v)
        v=copy.deepcopy(source);name=next(iter(value['files']));v['files'][name]=v['original_git_export']['files'][name];bad.append(v)
        v=copy.deepcopy(source);v['files']['fifth']=v['files']['go.mod'];bad.append(v)
        with patch.object(m,'finite_projection',return_value=value):
            for changed in bad:
                with self.subTest(changed=changed['tree']),self.assertRaises(m.Refusal):
                    m.verify_fixture_projection(self.state,'baseline',changed,self.manifest,manual=True)
            with self.assertRaises(m.Refusal):m.verify_fixture_projection(self.state,'candidate',source,self.manifest,manual=True)

    def test_substitution_requires_old_bytes_regular_single_link_and_mode(self):
        for kind in ('bytes','mode','symlink','hardlink'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as temp:
                self.state=Path(temp).resolve();self.state.chmod(0o700);value,source,_=self.fixture()
                name=next(iter(value['files']));path=self.state/'baseline-source'/name
                if kind=='bytes':path.write_bytes(b'corrupt')
                elif kind=='mode':path.chmod(0o755)
                elif kind=='hardlink':os.link(path,self.state/'linked')
                else:
                    original=self.state/'original';os.replace(path,original);path.symlink_to(original)
                with self.assertRaises(m.Refusal):m.apply_fixture_projection(self.state,source,value)

    def test_source_hashes_join_exact_blob_bytes_and_mode_and_refuse_rebind(self):
        path=self.state/'source';raw=b'owned source';path.write_bytes(raw);path.chmod(0o755)
        self.assertEqual(m.source_hashes(path),(hashlib.sha256(raw).hexdigest(),
            hashlib.sha1(b'blob 12\0'+raw).hexdigest(),0o755))
        os.link(path,self.state/'linked')
        with self.assertRaisesRegex(m.Refusal,'exclusively'):m.source_hashes(path)

    def test_original_checksum_provenance_tamper_is_blocking(self):
        value,source,_=self.fixture();m.apply_fixture_projection(self.state,source,value)
        (self.state/'baseline-original-fixture-metadata/iac-pass.go.sum').write_bytes(b'tampered')
        with patch.object(m,'finite_projection',return_value=value),self.assertRaisesRegex(m.Refusal,'checksum provenance'):
            m.verify_fixture_projection(self.state,'baseline',source,self.manifest,manual=True)

    def test_source_fd_content_growth_or_path_change_after_read_refuses(self):
        original=m.os.fdopen
        for kind in ('content','growth','replacement'):
            with self.subTest(kind=kind):
                path=self.state/('source-'+kind);path.write_bytes(b'owned source');path.chmod(0o644)
                fired=[]
                class Proxy:
                    def __init__(proxy,file):proxy.file=file
                    def __enter__(proxy):proxy.file.__enter__();return proxy
                    def __exit__(proxy,*args):return proxy.file.__exit__(*args)
                    def read(proxy,count):
                        raw=proxy.file.read(count)
                        if raw and not fired:
                            fired.append(True)
                            if kind=='content':path.write_bytes(b'mutant bytes')
                            elif kind=='growth':path.write_bytes(b'owned source extra')
                            else:
                                replacement=path.with_suffix('.new');replacement.write_bytes(b'owned source')
                                replacement.chmod(0o644);os.replace(replacement,path)
                        return raw
                with patch.object(m.os,'fdopen',side_effect=lambda *args:Proxy(original(*args))):
                    with self.assertRaises(m.Refusal):m.source_hashes(path)
                self.assertEqual(fired,[True])

    def test_duplicate_manual_projection_fields_fail_strict_json(self):
        path=self.state/'duplicate.json';path.write_text('{"baseline_projection":{},"baseline_projection":{}}')
        with self.assertRaisesRegex(m.Refusal,'duplicate'):m.load(path)

    def test_native_derived_catalog_join_preserves_distinct_original_declarations(self):
        value,source,_=self.fixture();m.apply_fixture_projection(self.state,source,value)
        manifests=copy.deepcopy(value['derived_fixture_catalog']);manifests['go.mod']={'sha256':source['files']['go.mod']['sha256']}
        with patch.object(m,'finite_projection',return_value=value):
            catalogs=m.finite_catalogs(source,manifests,self.manifest)
            self.assertEqual(catalogs['derived_native_manifest_catalog'],manifests)
            self.assertNotEqual(catalogs['original_declared_manifest_inventory'],manifests)
            self.assertTrue(catalogs['original_native_root2_failure_preserved'])
            manifests[next(iter(value['derived_fixture_catalog']))]['packages'].pop()
            with self.assertRaisesRegex(m.Refusal,'actual derived'):m.finite_catalogs(source,manifests,self.manifest)

    def test_report_reader_preserves_string_path_and_pathlike_api(self):
        from osv_completion import read_report, IncompleteScan
        path=self.state/'report.json';path.write_text('{"results":[]}')
        class PathLike:
            def __fspath__(self):return str(path)
        for argument in (path,str(path),PathLike()):self.assertEqual(read_report(argument),{'results':[]})
        path.write_text('{"results":[],"results":[]}')
        with self.assertRaises(IncompleteScan):read_report(str(path))

class NativeSamplingTests(unittest.TestCase):
    """Mocked causal/ownership controls; never native PID or host memory credit."""
    def setUp(self):
        self.f=MemoryLifetimeTests('test_complete_pipeline_defers_raw_records_and_retains_advisory_only_block')
        self.f.setUp();self.addCleanup(self.f.doCleanups);self.p=self.f.p
        self.proc=self.f.base/'fake-proc';self.proc.mkdir()
        patch.object(self.p,'PROC_ROOT',self.proc).start()
        self.child=SimpleNamespace(pid=42,returncode=None)
        self.member(42,os.getpid(),42,'govulncheck',100,64)
        self.member(43,42,42,'compile',200,96)
        (self.f.cg/'cgroup.procs').write_text(str(os.getpid())+'\n42\n43\n')
        from osv_io import Budget
        self.budget=Budget();self.before=self.p.begin_memory(self.f.out,'root2-govulncheck',self.f.witness,self.budget,['fixed'])
        self.sampler=self.p.NativeMemorySamples(self.f.out,'root2-govulncheck',self.f.witness,self.budget,self.before)
        self.addCleanup(lambda:self.sampler.file.close() if not self.sampler.file.closed else None)
    def member(self,pid,ppid,pgrp,name,hwm,rss,start=100):
        root=self.proc/str(pid);root.mkdir(exist_ok=True)
        fields=['S',str(ppid),str(pgrp),str(pgrp)]+['0']*15+[str(start)]
        (root/'stat').write_text(str(pid)+' ('+name+') '+' '.join(fields)+'\n')
        (root/'cgroup').write_text('0::/\n')
        (root/'status').write_text('Name:\t'+name+'\nVmRSS:\t'+str(rss)+' kB\nVmHWM:\t'+str(hwm)+' kB\nRssAnon:\t'+str(rss)+' kB\n')
    def records(self):return [json.loads(line) for line in self.sampler.path.read_text().splitlines()]
    def test_sample_distinguishes_native_and_compiler_pressure_without_victim_claim(self):
        self.sampler(self.child)
        sample=self.records()[1];self.assertEqual([p['role'] for p in sample['processes']],['native-leader','go-compiler'])
        self.assertEqual(sample['processes'][0]['memory_bytes']['VmRSS'],64*1024)
        self.member(42,os.getpid(),42,'govulncheck',1800000,1700000)
        self.member(43,42,42,'compile',100,64);self.sampler.next_sample=0;self.sampler(self.child)
        second=self.records()[2];self.assertGreater(second['processes'][0]['memory_bytes']['VmRSS'],1700000*1000)
        binding=self.sampler.close(-9,True);self.assertFalse(binding['accepted'])
        self.assertEqual(binding['sha256'],hashlib.sha256(self.sampler.path.read_bytes()).hexdigest())
        self.assertFalse(self.records()[-1]['continuous_measurement_or_victim_attribution'])
    def test_no_proc_reads_after_reap_or_closed_and_no_other_child_rebinding(self):
        self.child.returncode=0
        with patch.object(self.p,'proc_bytes') as read,self.assertRaisesRegex(Exception,'after reap'):
            self.sampler(self.child)
        read.assert_not_called();self.child.returncode=None;self.sampler(self.child)
        with self.assertRaisesRegex(Exception,'another child'):self.sampler(SimpleNamespace(pid=99,returncode=None))
        self.sampler.close(0,True)
        with patch.object(self.p,'proc_bytes') as read,self.assertRaisesRegex(Exception,'after reap'):self.sampler(self.child)
        read.assert_not_called()
    def test_reused_leader_wrong_parent_session_cgroup_and_status_refuse(self):
        self.sampler(self.child)
        self.member(42,os.getpid(),42,'govulncheck',100,64,start=101);self.sampler.next_sample=0
        with self.assertRaisesRegex(Exception,'reused'):self.sampler(self.child)
        self.member(42,99,42,'govulncheck',100,64);self.sampler.next_sample=0
        with self.assertRaisesRegex(Exception,'direct-child'):self.sampler(self.child)
        self.member(42,os.getpid(),99,'govulncheck',100,64);self.sampler.next_sample=0
        with self.assertRaisesRegex(Exception,'direct-child'):self.sampler(self.child)
        self.member(42,os.getpid(),42,'govulncheck',100,64);self.sampler.next_sample=0
        (self.proc/'42/cgroup').write_text('0::/foreign\n')
        with self.assertRaisesRegex(Exception,'left owned cgroup'):self.sampler(self.child)
        (self.proc/'42/cgroup').write_text('0::/\n');(self.proc/'42/status').write_text('VmRSS: 1 kB\n');self.sampler.next_sample=0
        with self.assertRaisesRegex(Exception,'fields missing'):self.sampler(self.child)
    def test_owned_proc_virtual_files_reject_symlink_and_growth(self):
        path=self.proc/'42/status';other=self.proc/'private';os.replace(path,other);path.symlink_to(other)
        with self.assertRaisesRegex(Exception,'owned regular'):self.p.proc_bytes(path)
        path.unlink();path.write_bytes(b'x'*16385)
        with self.assertRaisesRegex(Exception,'bound'):self.p.proc_bytes(path)
    def test_diagnostic_limit_is_explicit_and_shared_budget_remains_enforced(self):
        self.sampler.LIMIT=self.sampler.bytes+4096
        self.sampler(self.child);self.assertTrue(self.sampler.truncated)
        self.sampler.close(0,True);self.assertTrue(self.records()[-1]['truncated'])
        self.assertFalse(self.records()[-1]['accepted'])
        self.assertEqual(self.records()[-2]['record'],'stop')
        self.assertEqual(self.records()[-1]['diagnostic_status'],'HOLD')
    def test_shared_native_stream_reserve_stops_only_sampling(self):
        self.budget.limit=self.budget.used+20*1024*1024+4096
        self.sampler(self.child);self.assertTrue(self.sampler.truncated)
        self.assertEqual(self.sampler.stop_reason,'existing-aggregate-budget-native-stream-reserve')
        self.sampler.next_sample=0
        with patch.object(self.p,'proc_bytes') as read:self.sampler(self.child)
        read.assert_not_called();self.assertEqual(self.sampler.unobserved_intervals,1)
        binding=self.sampler.close(0,True);self.assertEqual(binding['diagnostic_status'],'HOLD')
    def test_closed_tuning_environment_and_original_go_flags(self):
        runtime=dict(existing_gopath='/old/go',existing_module_cache='/old/mod',existing_build_cache='/old/build')
        env=m.env(Path('/fixed/go/bin/go'),self.f.base,runtime,analysis=True)
        self.assertEqual((env['GOGC'],env['GOMEMLIMIT'],env['GOMAXPROCS'],env['GOFLAGS']),('50','512MiB','2','-mod=readonly'))
        normal=m.env(Path('/fixed/go/bin/go'),self.f.base,runtime)
        self.assertEqual(normal['GOMEMLIMIT'],'768MiB');self.assertNotIn('GOGC',normal)
        for key,value in [('GOGC','100'),('GOMEMLIMIT','768MiB'),('GOMAXPROCS','1'),('GOFLAGS','-mod=readonly -p=1')]:
            self.f.env[key]=value
            with self.assertRaisesRegex(Exception,'unbound compiler'):self.f.run_producer()
            self.f.env[key]=env[key]
    def test_native_sample_same_size_mutation_after_capture_refuses(self):
        self.sampler(self.child)
        raw=self.sampler.path.read_bytes();changed=raw.replace(b'native-leader',b'corrupt-value')
        self.assertEqual(len(raw),len(changed));self.assertNotEqual(raw,changed)
        self.sampler.path.write_bytes(changed)
        with self.assertRaisesRegex(Exception,'bytes changed after capture'):self.sampler.close(0,True)

class HostMemoryTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve();self.proc=self.root/'proc';self.proc.mkdir();(self.proc/'self').mkdir()
        self.cg=self.root/'cgroup';self.cg.mkdir();(self.cg/'job').mkdir()
        (self.proc/'self/cgroup').write_text('0::/job\n')
        (self.proc/'meminfo').write_text('MemTotal: 8388608 kB\nMemAvailable: 6291456 kB\n')
        for root,limit,current in [(self.cg,'4294967296','1073741824'),(self.cg/'job','max','536870912')]:
            (root/'memory.max').write_text(limit+'\n');(root/'memory.current').write_text(current+'\n');(root/'cgroup.procs').write_text(str(os.getpid())+'\n')
        self.addCleanup(patch.stopall);patch.object(m,'HOST_PROC',self.proc).start();patch.object(m,'HOST_CGROUP',self.cg).start()
        from osv_io import Budget
        self.budget=Budget()
    def test_real_values_derive_effective_ancestor_headroom_and_never_admit_expansion(self):
        value=m.host_memory_snapshot(self.budget)
        self.assertEqual(value['effective_cgroup_memory_max_bytes'],4294967296)
        self.assertEqual(value['current_group_memory_current_bytes'],536870912)
        self.assertEqual(value['estimated_physical_and_cgroup_headroom_bytes'],3221225472)
        self.assertIsNone(value['docker_parent_effective_headroom_bytes'])
        self.assertEqual(value['docker_parent_headroom_status'],'UNKNOWN/HOLD')
        self.assertFalse(value['accepted']);self.assertIn('no reservation',value['scope'])
    def test_uncontrolled_root_absence_is_explicit_not_zero_usage(self):
        (self.cg/'memory.max').unlink();(self.cg/'memory.current').unlink()
        value=m.host_memory_snapshot(self.budget);self.assertIsNone(value['effective_cgroup_memory_max_bytes'])
        self.assertIsNone(value['ancestor_cgroup_memory'][-1]['memory_current_bytes'])
        self.assertTrue(value['ancestor_cgroup_memory'][-1]['uncontrolled_root_fields_observed_absent'])
    def test_missing_partial_controller_unsafe_group_invalid_memory_and_alias_refuse(self):
        (self.cg/'job/memory.max').unlink()
        with self.assertRaisesRegex(m.Refusal,'nonroot'):m.host_memory_snapshot(self.budget)
        (self.cg/'job/memory.max').write_text('max\n');(self.proc/'self/cgroup').write_text('0::/../escape\n')
        with self.assertRaises(m.Refusal):m.host_memory_snapshot(self.budget)
        (self.proc/'self/cgroup').write_text('0::/job\n');(self.proc/'meminfo').write_text('MemTotal: 1 kB\nMemAvailable: 2 kB\n')
        with self.assertRaisesRegex(m.Refusal,'contradictory'):m.host_memory_snapshot(self.budget)

class NativeObserverLifecycleTests(unittest.TestCase):
    def fixture(self):
        sys.path.insert(0,str(HERE));self.addCleanup(lambda:sys.path.remove(str(HERE)))
        io=module('owned_observer_io','osv_io.py');events=[]
        class Pipe:
            def __init__(self,fd):self.fd=fd
            def fileno(self):return self.fd
            def close(self):events.append(('close',self.fd))
        class Proc:
            pid=42;returncode=None
            def __init__(self):self.stdout=Pipe(10);self.stderr=Pipe(11);self.reaped=False
            def wait(self,timeout):events.append(('wait',timeout));self.reaped=True;self.returncode=0;return 0
        proc=Proc()
        class Selector:
            def __init__(self):self.mapping={}
            def register(self,pipe,event,index):self.mapping[pipe.fd]=SimpleNamespace(fileobj=pipe,data=index)
            def unregister(self,pipe):self.mapping.pop(pipe.fd)
            def get_map(self):return self.mapping
            def select(self,seconds):return [(next(iter(self.mapping.values())),1)]
            def close(self):events.append(('selector-close',None))
        with patch.object(io.subprocess,'Popen',return_value=proc),patch.object(io.selectors,'DefaultSelector',Selector),\
            patch.object(io.os,'set_blocking'),patch.object(io.os,'read',return_value=b''),patch.object(io.os,'killpg') as kill:
            yield io,proc,events,kill
    def test_observer_only_before_owned_reap_and_native_capture_semantics_unchanged(self):
        fixture=self.fixture();io,proc,events,kill=next(fixture)
        def observe(current):self.assertIs(current,proc);self.assertFalse(current.reaped);events.append(('observe',current.pid))
        result=io.bounded_run(['owned'],HERE,{},1,observer=observe)
        self.assertEqual(result.returncode,0);self.assertEqual(result.stdout,'');self.assertEqual(result.stderr,'')
        self.assertEqual([name for name,_ in events].count('observe'),2);self.assertTrue(proc.reaped);kill.assert_not_called()
        fixture.close()
    def test_observer_refusal_or_shared_budget_failure_kills_only_owned_unreaped_group(self):
        for kind in ('refusal','budget'):
            with self.subTest(kind=kind):
                fixture=self.fixture();io,proc,events,kill=next(fixture);budget=io.Budget(limit=1)
                def observe(current):
                    self.assertFalse(current.reaped)
                    if kind=='budget':budget.charge(2)
                    else:raise RuntimeError('mocked observer refuses')
                with self.assertRaises(Exception):io.bounded_run(['owned'],HERE,{},1,budget,observer=observe)
                kill.assert_called_once_with(42,io.signal.SIGKILL);self.assertTrue(proc.reaped)
                self.assertEqual(len([x for x in events if x[0]=='wait']),1);fixture.close()

if __name__=='__main__':unittest.main()
