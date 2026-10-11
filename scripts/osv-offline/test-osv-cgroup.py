"""Offline helper controls, never hosted/Linux/systemd acceptance.

Real temporary files, directories, symlinks and pipes are used. Linux identity,
root/systemd ownership, ACL writability and every systemd response are explicitly
substituted. Popen is forbidden except for a fake process backed by local pipes;
no service, cgroup, privilege, controller or network operation is performed.
"""
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
import types
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('osv_cgroup',Path(__file__).with_name('osv_cgroup.py'))
cg=importlib.util.module_from_spec(spec)
spec.loader.exec_module(cg)

class CgroupTests(unittest.TestCase):
    UID=1234
    GID=1235

    def setUp(self):
        self.stack=contextlib.ExitStack(); self.addCleanup(self.stack.close)
        self.temp=self.stack.enter_context(tempfile.TemporaryDirectory())
        self.base=Path(self.temp).resolve()
        env=dict(cg.IDENTITY_FIXED,GITHUB_RUN_ID='123456',GITHUB_RUN_ATTEMPT='2',
            GITHUB_SHA='a'*40,ImageVersion='20261004.327.1')
        self.stack.enter_context(patch.dict(os.environ,env,clear=True))
        for target,value in (('system','Linux'),('machine','x86_64')):
            self.stack.enter_context(patch.object(cg.platform,target,return_value=value))
        for target,value in (('getuid',self.UID),('geteuid',self.UID),('getgid',self.GID),('getegid',self.GID)):
            self.stack.enter_context(patch.object(cg.os,target,return_value=value))
        self.no_process=self.stack.enter_context(patch.object(cg.subprocess,'Popen',side_effect=AssertionError('real process forbidden')))
        self.no_signal=self.stack.enter_context(patch.object(cg.os,'killpg',side_effect=AssertionError('real signal forbidden')))
        self.unit=cg.unit_name()
        self.cgroup=self.base/'cgroup'; self.cgroup.mkdir()
        self.slice=self.cgroup/'system.slice'; self.slice.mkdir()
        for path in (self.cgroup,self.slice):
            self.put(path/'cgroup.controllers','cpu io memory pids\n')
            self.put(path/'cgroup.subtree_control','cpu io memory pids\n')
        self.mountinfo=self.put(self.base/'mountinfo','1 0 0:1 / '+str(self.cgroup)+' rw - cgroup2 cgroup rw\n')
        self.membership=self.put(self.base/'membership','0::/system.slice/'+self.unit+'/supervisor\n')
        self.lookup=self.base/'systemd-units'; self.lookup.mkdir()
        self.toolpaths={}
        for name in cg.TOOLS:
            self.toolpaths[name]=self.put(self.base/name,'inert '+name+' fixture\n')
            self.toolpaths[name].chmod(0o755)
        for target,value in (('CGROUP_ROOT',self.cgroup),('MOUNTINFO',self.mountinfo),
                ('MEMBERSHIP',self.membership),('TOOLS',self.toolpaths)):
            self.stack.enter_context(patch.object(cg,target,value))
        self.writable=set(); self.owners={}; self.denied=set(); self.modes={}
        self.ancestor_metadata={}
        original=Path.lstat
        def metadata(path,*args,**kwargs):
            if path in self.denied: raise PermissionError('fake metadata denial')
            info=original(path,*args,**kwargs)
            fields={name:getattr(info,name) for name in ('st_mode','st_uid','st_gid','st_dev','st_ino','st_size','st_mtime_ns','st_ctime_ns')}
            # Shared host temp ancestors are part of the projected fixture,
            # not its owned live namespace. Concurrent suites may mutate them.
            # Freeze only those ancestors; self.base and every owned child
            # retain actual inode/size/times and the production drift veto.
            if path in self.base.parents:
                stable=('st_ino','st_size','st_mtime_ns','st_ctime_ns')
                fields.update(self.ancestor_metadata.setdefault(path,{name:fields[name] for name in stable}))
            # Only the fixture's delegated subtree projects runner ownership;
            # its quota files project systemd/root ownership. Ancestors/tools
            # project root ownership without any chown or privileged operation.
            runner=(self.slice/self.unit) in path.parents or path==self.slice/self.unit
            fields['st_uid'],fields['st_gid']=((self.UID,self.GID) if runner and path.name not in {'memory.max','memory.swap.max','pids.max'} else (0,0))
            if path not in self.base.parents and path!=self.base and self.base not in path.parents:
                fields['st_mode']=stat.S_IFMT(fields['st_mode'])|0o755
            if path in self.owners: fields['st_uid'],fields['st_gid']=self.owners[path]
            if path in self.modes: fields['st_mode']=stat.S_IFMT(fields['st_mode'])|self.modes[path]
            return types.SimpleNamespace(**fields)
        self.stack.enter_context(patch.object(Path,'lstat',new=metadata))
        self.stack.enter_context(patch.object(cg,'_writable',side_effect=lambda path:path in self.writable))
        self.defaults={name:False for name in cg.DEFAULTS}
        self.paths=[str(self.lookup)]; self.raw_defaults={}; self.raw_paths=None
        self.version='systemd 255 (255.4-1ubuntu8)\n'
        self.actual={}; self.root=None
        self.query=self.stack.enter_context(patch.object(cg,'run_readonly',side_effect=self.response))

    def put(self,path,text):
        path.write_text(text); return path

    def response(self,argv,budget,**kwargs):
        budget.left()
        if argv[1]=='--version': return self.version
        if argv[1]=='--system':
            key=argv[-1]
            if key=='UnitPath': return self.raw_paths if self.raw_paths is not None else 'as '+str(len(self.paths))+' '+' '.join(json.dumps(p) for p in self.paths)
            name=key.removeprefix('Default').removesuffix('Accounting')
            return self.raw_defaults.get(name,'b '+str(self.defaults[name]).lower())
        self.assertEqual(argv[1:3],['show','--all']); self.assertEqual(argv[-1],self.unit)
        names=argv[3].removeprefix('--property=').split(',')
        absent={'Id':self.unit,'Names':self.unit,'LoadState':'not-found','ActiveState':'inactive',
            'FragmentPath':'','DropInPaths':'','UnitFileState':'','ControlGroup':'',
            'MainPID':'0','InvocationID':'','User':'','Group':''}
        values=absent|self.actual
        return ''.join(name+'='+values[name]+'\n' for name in names)

    def make_unit(self):
        self.root=self.slice/self.unit; self.root.mkdir()
        supervisor=self.root/'supervisor'; supervisor.mkdir()
        for path in (self.root,supervisor):
            self.put(path/'cgroup.type','domain\n')
            self.put(path/'cgroup.procs','' if path==self.root else str(os.getpid())+'\n')
            self.put(path/'cgroup.subtree_control','')
            self.put(path/'cgroup.controllers','memory pids\n')
        for name,value in {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}.items():
            self.put(self.root/name,value+'\n')
        self.actual=dict(cg.service_properties())
        self.actual.update(Id=self.unit,Names=self.unit,LoadState='loaded',ActiveState='active',
            SubState='running',Transient='yes',MainPID=str(os.getpid()),ControlGroup='/system.slice/'+self.unit,
            InvocationID='b'*32,Delegate='yes',DelegateControllers='memory pids',RuntimeMaxUSec='2min 30s',
            TimeoutStartUSec='15s',TimeoutStopUSec='5s')
        return self.root,supervisor

    def test_exact_identity_and_properties(self):
        self.assertEqual(self.unit,'wf-osv-proof-123456-2.service')
        properties=cg.service_properties()
        for name in cg.DEFAULTS:
            self.assertEqual(properties[name+'Accounting'],'yes' if name in {'Memory','Tasks'} else 'no')
        self.assertEqual(properties['MemoryMax'],'2147483648')
        self.assertEqual(properties['Delegate'],'memory pids')
        self.assertEqual(properties['RuntimeMaxSec'],'150s')
        self.assertEqual(properties['TimeoutStartSec'],'15s'); self.assertEqual(properties['TimeoutStopSec'],'5s')
        self.assertEqual(properties['TasksMax'],'256'); self.assertEqual(properties['MemorySwapMax'],'0')

    def test_identity_refusals(self):
        mutations={key:'unexpected' for key in cg.IDENTITY_FIXED}
        mutations.update(GITHUB_RUN_ID='01',GITHUB_RUN_ATTEMPT='2;exit',GITHUB_SHA='a'*39,ImageVersion='latest')
        for key,value in mutations.items():
            with self.subTest(key=key),patch.dict(os.environ,{key:value}),self.assertRaises(cg.Refusal): cg.unit_name()
        for target,value in (('getuid',0),('getgid',0),('geteuid',0),('getegid',0)):
            with self.subTest(target=target),patch.object(cg.os,target,return_value=value),self.assertRaises(cg.Refusal): cg.unit_name()
        with patch.object(cg.platform,'system',return_value='Darwin'),self.assertRaises(cg.Refusal): cg.unit_name()

    def test_preflight_all_defaults_complete_bits_and_digest(self):
        self.defaults.update(IO=True,BlockIO=True,CPU=True,IP=True)
        result=cg.preflight()
        self.assertEqual(len(result['manager_defaults']),6)
        self.assertTrue(result['manager_defaults']['DefaultIOAccounting'])
        self.assertTrue(result['manager_defaults']['DefaultBlockIOAccounting'])
        self.assertEqual(result['service_properties']['IOAccounting'],'no')
        self.assertEqual(result['service_properties']['BlockIOAccounting'],'no')
        self.assertEqual(result['ancestors'][str(self.slice)]['enabled'],['cpu','io','memory','pids'])
        self.assertEqual(result['effective_controller_demand'],['memory','pids'])
        self.assertTrue(result['unit_absent']); self.assertEqual(len(result['tools']),8)
        self.assertEqual(result['snapshot_sha256'],cg.preflight()['snapshot_sha256'])
        self.put(self.slice/'cgroup.subtree_control','io memory pids\n')
        self.assertNotEqual(result['snapshot_sha256'],cg.preflight()['snapshot_sha256'])
        before=cg.preflight()['snapshot_sha256']; self.defaults['BlockIO']=False
        self.assertNotEqual(before,cg.preflight()['snapshot_sha256'])
        before=cg.preflight()['snapshot_sha256']; self.put(self.lookup/'unrelated.service','metadata only\n')
        self.assertNotEqual(before,cg.preflight()['snapshot_sha256'])
        before=cg.preflight()['snapshot_sha256']; self.put(self.toolpaths['systemctl'],'changed inert fixture\n')
        self.assertNotEqual(before,cg.preflight()['snapshot_sha256'])

    def test_missing_unknown_defaults_or_model_refused(self):
        for name in cg.DEFAULTS:
            for value in ('','b unknown','s true','b true extra'):
                with self.subTest(name=name,value=value):
                    self.raw_defaults[name]=value
                    with self.assertRaises(cg.Refusal): cg.preflight()
                    self.raw_defaults.clear()
        self.version='systemd 256\n'
        with self.assertRaises(cg.Refusal): cg.preflight()

    def test_unitpath_type_bounds_and_canonical_path(self):
        for value in ('','as 0','as 01 "/a"','as 1 "unterminated','s "/etc/systemd/system"','as 65 '+' '.join('"/a'+str(i)+'"' for i in range(65)),
                'as 1 "/a/../b"','as 2 '+json.dumps(str(self.lookup))+' '+json.dumps(str(self.lookup))):
            with self.subTest(value=value):
                self.raw_paths=value
                with self.assertRaises(cg.Refusal): cg.preflight()

    def test_lookup_ownership_writability_symlink_and_denial(self):
        self.owners[self.lookup]=(self.UID,self.GID)
        with self.assertRaises(cg.Refusal): cg.preflight()
        self.owners.clear(); self.writable.add(self.lookup)
        with self.assertRaises(cg.Refusal): cg.preflight()
        self.writable.clear(); self.denied.add(self.lookup)
        with self.assertRaisesRegex(cg.Refusal,'uninspectable'): cg.preflight()
        self.denied.clear(); alias=self.base/'lookup-alias'; alias.symlink_to(self.lookup,target_is_directory=True)
        self.paths=[str(alias)]
        with self.assertRaisesRegex(cg.Refusal,'symlink'): cg.preflight()

    def test_fragment_mask_alias_and_dependencies_refused(self):
        for suffix in ('','.wants','.requires','.upholds'):
            with self.subTest(suffix=suffix):
                target=self.lookup/(self.unit+suffix)
                if suffix: target.mkdir()
                else: self.put(target,'never read this configuration')
                with self.assertRaisesRegex(cg.Refusal,'collision'): cg.preflight()
                if suffix: target.rmdir()
                else: target.unlink()
        mask=self.lookup/self.unit; mask.symlink_to('/dev/null')
        with self.assertRaises(cg.Refusal): cg.preflight()
        mask.unlink(); alias=self.lookup/'other.service'; alias.symlink_to(self.unit)
        with self.assertRaisesRegex(cg.Refusal,'alias collision'): cg.preflight()

    def test_every_applicable_dropin_refused_without_contents(self):
        names=('service.d',self.unit+'.d','wf-.service.d','wf-osv-.service.d',
            'wf-osv-proof-.service.d','wf-osv-proof-123456-.service.d')
        original=cg._read
        def no_config_read(path,*args,**kwargs):
            self.assertNotIn(self.lookup,path.parents,'configuration contents must never be read')
            return original(path,*args,**kwargs)
        with patch.object(cg,'_read',side_effect=no_config_read):
            for name in names:
                with self.subTest(name=name):
                    directory=self.lookup/name; directory.mkdir()
                    target=self.put(directory/'override.conf','unread configuration')
                    with self.assertRaisesRegex(cg.Refusal,'drop-in refused'): cg.preflight()
                    target.unlink(); directory.rmdir()
            directory=self.lookup/'service.d'; directory.mkdir()
            (directory/'mask.conf').symlink_to('/dev/null')
            with self.assertRaises(cg.Refusal): cg.preflight()

    def test_nearest_missing_lookup_ancestor_must_be_trusted(self):
        self.paths=[str(self.lookup/'missing'/'nested')]
        self.assertTrue(cg.preflight()['unit_absent'])
        self.writable.add(self.lookup)
        with self.assertRaises(cg.Refusal): cg.preflight()

    def test_compact_namespace_conserves_metadata_and_bounds_result(self):
        for index in range(300): self.put(self.lookup/('unrelated-'+str(index)+'.service'),'never read\n')
        result=cg.preflight()
        self.assertEqual(result['metadata'][str(self.lookup)]['entries_count'],300)
        self.assertLess(len(json.dumps(result,sort_keys=True,separators=(',',':')).encode()),32768)
        target=self.lookup/'unrelated-1.service'
        self.assertNotIn(str(target),result['metadata'])
        self.put(target,'changed metadata, still never read\n')
        changed=cg.preflight()
        self.assertNotEqual(result['metadata'][str(self.lookup)]['entries_sha256'],changed['metadata'][str(self.lookup)]['entries_sha256'])
        with self.assertRaises(cg.Refusal): cg._bounded_result({'too_large':'x'*100},cg.Budget(),100)

    def test_canonical_tool_ownership_modes_hashes_and_symlink(self):
        target=self.toolpaths['sudo']
        self.owners[target]=(self.UID,self.GID)
        with self.assertRaises(cg.Refusal): cg.preflight()
        self.owners.clear()
        target.chmod(0o644)
        with self.assertRaises(cg.Refusal): cg.preflight()
        # Host filesystems may clear set-id on unprivileged temporary files;
        # this source refusal control explicitly projects the metadata bit.
        target.chmod(0o755)
        self.modes[target]=0o4755
        self.assertIn('sudo',cg.preflight()['tools'])
        for mode in (0o2755,0o6755):
            self.modes[target]=mode
            with self.subTest(tool='sudo',mode=oct(mode)),self.assertRaises(cg.Refusal): cg.preflight()
        self.modes.clear()
        for name in ('env','systemctl','systemd-run','busctl','python3','git','timeout'):
            for mode in (0o4755,0o2755):
                other=self.toolpaths[name]; self.modes[other]=mode
                with self.subTest(tool=name,mode=oct(mode)),self.assertRaises(cg.Refusal): cg.preflight()
                self.modes.clear()
        self.writable.add(target)
        with self.assertRaises(cg.Refusal): cg.preflight()
        self.writable.clear()
        alias=self.base/'sudo-alias'; alias.symlink_to(target)
        self.toolpaths['sudo']=alias
        result=cg.preflight()
        self.assertEqual(result['tools']['sudo']['canonical_path'],str(target))
        self.assertEqual(result['tools']['sudo']['path'],str(alias))

    def test_snapshot_metadata_changes_and_disappearance_refused(self):
        path=self.put(self.base/'changing','initial\n'); records={}; budget=cg.Budget()
        cg._meta(path,budget,records)
        self.put(path,'changed length\n')
        with self.assertRaisesRegex(cg.Refusal,'changed'): cg._meta(path,budget,records)
        path.unlink()
        with self.assertRaisesRegex(cg.Refusal,'disappeared'): cg._meta(path,budget,records)

    def test_projected_shared_ancestor_stays_stable_when_sibling_created(self):
        ancestor=self.base.parent; records={}; budget=cg.Budget()
        cg._meta(ancestor,budget,records); before=dict(records[str(ancestor)])
        with tempfile.TemporaryDirectory(dir=ancestor):
            cg._meta(ancestor,budget,records)
            self.assertEqual(before,records[str(ancestor)])
        cg._meta(ancestor,budget,records)
        self.assertEqual(before,records[str(ancestor)])

    def test_ancestors_require_both_memory_and_pids_no_activation(self):
        for path in (self.cgroup,self.slice):
            for name,text in (('cgroup.controllers','cpu io memory'),('cgroup.subtree_control','cpu io pids'),
                    ('cgroup.subtree_control','memory pids rogue'),('cgroup.subtree_control','memory pids pids')):
                target=path/name; original=target.read_text(); self.put(target,text)
                with self.subTest(path=path,name=name,text=text),self.assertRaises(cg.Refusal): cg.preflight()
                self.put(target,original)
        self.put(self.mountinfo,'malformed - \n')
        with self.assertRaisesRegex(cg.Refusal,'cgroup-v2 mount'): cg.preflight()

    def test_exact_unit_must_be_absent_before_submission(self):
        for key,value in (('LoadState','loaded'),('ActiveState','active'),('FragmentPath','/x'),
                ('DropInPaths','/x'),('Names',self.unit+' alias.service'),('UnitFileState','masked')):
            with self.subTest(key=key):
                self.actual={key:value}
                with self.assertRaises(cg.Refusal): cg.preflight()

    def test_attestation_returns_actual_paths_instance_and_quotas(self):
        root,supervisor=self.make_unit(); result=cg.attest_unit()
        self.assertEqual(result['root'],str(root)); self.assertEqual(result['supervisor'],str(supervisor))
        self.assertEqual(result['properties']['InvocationID'],'b'*32)
        self.assertEqual(result['properties']['MainPID'],str(os.getpid()))
        self.assertEqual(result['limits'],{'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'})
        self.assertEqual(result['root_enabled'],[])
        self.assertEqual(result['ancestors'][str(self.slice)]['enabled'],['cpu','io','memory','pids'])

    def test_attestation_actual_fixed_properties_refuse_mismatch(self):
        self.make_unit()
        keys=('MainPID','User','Group','Id','Names','Type','Slice','Delegate','DelegateSubgroup',
            'MemoryMax','MemorySwapMax','TasksMax','KillMode','SendSIGKILL','Restart','NoNewPrivileges',
            'UMask','CollectMode','Transient','LoadState','ActiveState','SubState','InvocationID',
            'RuntimeMaxUSec','TimeoutStartUSec','TimeoutStopUSec')+tuple(name+'Accounting' for name in cg.DEFAULTS)
        for key in keys:
            with self.subTest(key=key):
                original=self.actual[key]; self.actual[key]='unexpected'
                with self.assertRaises(cg.Refusal): cg.attest_unit()
                self.actual[key]=original
        for value in ('memory','memory pids io','memory pids pids'):
            self.actual['DelegateControllers']=value
            with self.assertRaises(cg.Refusal): cg.attest_unit()
        with self.assertRaises(cg.Refusal): cg.attest_unit('other.service')
        with patch.dict(os.environ,{'WF_OSV_UNIT':'other.service'}),self.assertRaises(cg.Refusal): cg.attest_unit()

    def test_attestation_actual_controlgroup_never_reconstructed(self):
        self.make_unit()
        for value in ('','/user.slice/'+self.unit,'/system.slice/other.service','/system.slice/'+self.unit+'/../x'):
            self.actual['ControlGroup']=value
            with self.subTest(value=value),self.assertRaises(cg.Refusal): cg.attest_unit()

    def test_attestation_real_children_membership_and_ownership(self):
        root,supervisor=self.make_unit()
        for path in (root,supervisor):
            extra=path/'worker'; extra.mkdir()
            with self.assertRaises(cg.Refusal): cg.attest_unit()
            extra.rmdir()
        link=root/'alias'; link.symlink_to(supervisor,target_is_directory=True)
        with self.assertRaises(cg.Refusal): cg.attest_unit()
        link.unlink()
        for target,text in ((root/'cgroup.procs',str(os.getpid())),(supervisor/'cgroup.procs','999999'),
                (root/'cgroup.type','threaded'),(supervisor/'cgroup.subtree_control','memory'),
                (root/'cgroup.subtree_control','io'),(root/'cgroup.controllers','memory'),
                (self.membership,'0::/wrong/supervisor')):
            original=target.read_text(); self.put(target,text)
            with self.subTest(target=target),self.assertRaises(cg.Refusal): cg.attest_unit()
            self.put(target,original)
        for target in (root,supervisor,root/'cgroup.procs',supervisor/'cgroup.subtree_control'):
            self.owners[target]=(0,0)
            with self.subTest(target=target),self.assertRaises(cg.Refusal): cg.attest_unit()
            self.owners.clear()

    def test_outer_quotas_exact_root_owned_immutable_and_no_symlink(self):
        root,_=self.make_unit()
        for name in ('memory.max','memory.swap.max','pids.max'):
            target=root/name; original=target.read_text()
            for fault in ('value','owner','acl','mode','symlink'):
                with self.subTest(name=name,fault=fault):
                    if fault=='value': self.put(target,'max\n')
                    elif fault=='owner': self.owners[target]=(self.UID,self.GID)
                    elif fault=='acl': self.writable.add(target)
                    elif fault=='mode': target.chmod(0o666)
                    else:
                        target.unlink(); target.symlink_to(self.put(self.base/'quota-target',original))
                    with self.assertRaises(cg.Refusal): cg.attest_unit()
                    self.owners.clear(); self.writable.clear()
                    if target.is_symlink(): target.unlink()
                    self.put(target,original); target.chmod(0o644)

    def test_status_absence_and_actual_instance(self):
        result=cg.unit_status()
        self.assertEqual(result,{'load_state':'not-found','active_state':'inactive','control_group':'',
            'main_pid':'0','invocation_id':'','user':'','group':''})
        self.make_unit(); result=cg.unit_status()
        self.assertEqual(result['control_group'],'/system.slice/'+self.unit)
        self.assertEqual(result['invocation_id'],'b'*32)
        self.actual.update(ActiveState='inactive',MainPID='0',ControlGroup='')
        self.assertEqual(cg.unit_status()['main_pid'],'0')
        with self.assertRaises(cg.Refusal): cg.unit_status('other.service')

    def test_status_refuses_alias_unknown_instance_and_false_absence(self):
        self.make_unit()
        for key,value in (('Names','alias.service'),('InvocationID','0'*32),('User','0'),('Group','0'),
                ('ControlGroup','/other'),('MainPID','01'),('LoadState','masked'),('ActiveState','unknown')):
            original=self.actual[key]; self.actual[key]=value
            with self.subTest(key=key),self.assertRaises(cg.Refusal): cg.unit_status()
            self.actual[key]=original
        self.actual={key:value for key,value in self.actual.items() if key in {'Id','Names'}}
        self.actual['ControlGroup']='/system.slice/'+self.unit
        with self.assertRaises(cg.Refusal): cg.unit_status()

    def test_entry_read_and_shared_deadline_bounds(self):
        directory=self.base/'bounded'; directory.mkdir()
        for i in range(4): self.put(directory/str(i),'x')
        with patch.object(cg,'MAX_ENTRIES',3),self.assertRaises(cg.Refusal): cg._entries(directory,cg.Budget(),{})
        target=self.put(self.base/'bytes','12345')
        with self.assertRaises(cg.Refusal): cg._read(target,cg.Budget(),4)
        budget=cg.Budget(); budget.deadline=0
        with self.assertRaises(cg.Refusal): cg._read(target,budget)
        with patch.object(cg.time,'monotonic',side_effect=[0,21]),self.assertRaises(cg.Refusal): cg.preflight()

    def test_readonly_query_whitelist_forbids_credentials_and_mutation(self):
        # Restore the actual helper, but keep Popen forbidden on every refusal.
        for argv in ([],[str(self.toolpaths['systemctl']),'start',self.unit],
                [str(self.toolpaths['systemctl']),'show','--all','--property=Environment','--',self.unit],
                [str(self.toolpaths['systemctl']),'show','--all','--property=MainPID','--','other.service'],
                [str(self.toolpaths['busctl']),'--system','get-property','org.freedesktop.systemd1',
                    '/org/freedesktop/systemd1','org.freedesktop.systemd1.Manager','Environment']):
            with self.subTest(argv=argv),self.assertRaises(cg.Refusal): REAL_QUERY(argv,cg.Budget())
        self.no_process.assert_not_called()

    def test_exact_show_output_must_have_every_field_once(self):
        for raw in ('','MainPID=1\nMainPID=1\n','MainPID=1\nEnvironment=credential\n','1\n'):
            with patch.object(cg,'run_readonly',return_value=raw),self.assertRaises(cg.Refusal):
                cg._show(self.unit,('MainPID','User'),cg.Budget())

    def test_readonly_query_closed_execution_and_stream_bounds(self):
        class FakeProcess:
            pid=987654321
            def __init__(self,stdout=b'fixture\n',stderr=b''):
                self.stdout=self.pipe(stdout); self.stderr=self.pipe(stderr); self.waits=[]
            @staticmethod
            def pipe(data):
                reader,writer=os.pipe(); os.write(writer,data); os.close(writer)
                return os.fdopen(reader,'rb')
            def wait(self,timeout): self.waits.append(timeout); return 0
        process=FakeProcess()
        with patch.object(cg.subprocess,'Popen',return_value=process) as launch:
            self.assertEqual(REAL_QUERY([str(self.toolpaths['systemctl']),'--version'],cg.Budget()),'fixture\n')
        args,kwargs=launch.call_args
        self.assertEqual(args[0][:2],[str(self.toolpaths['env']),'-i'])
        self.assertNotIn('GITHUB_SHA',' '.join(args[0]))
        self.assertEqual(kwargs['env'],{}); self.assertEqual(kwargs['cwd'],'/')
        self.assertEqual(kwargs['stdin'],cg.subprocess.DEVNULL)
        self.assertTrue(kwargs['close_fds']); self.assertTrue(kwargs['start_new_session'])
        self.assertLessEqual(process.waits[0],3)
        process=FakeProcess(b'12345')
        with patch.object(cg,'MAX_READ',4),patch.object(cg.subprocess,'Popen',return_value=process),patch.object(cg.os,'killpg') as terminate:
            with self.assertRaisesRegex(cg.Refusal,'stream limit'): REAL_QUERY([str(self.toolpaths['systemctl']),'--version'],cg.Budget())
            terminate.assert_called_once_with(process.pid,cg.signal.SIGKILL)
        process=FakeProcess(stderr=b'x'*4097)
        with patch.object(cg.subprocess,'Popen',return_value=process),patch.object(cg.os,'killpg'):
            with self.assertRaisesRegex(cg.Refusal,'stream limit'): REAL_QUERY([str(self.toolpaths['systemctl']),'--version'],cg.Budget())

    def test_readonly_capture_failures_and_postwait_deadline(self):
        class FakeProcess:
            pid=987654321
            def __init__(self,data):
                self.stdout=self.pipe(data); self.stderr=self.pipe(b''); self.calls=0
            @staticmethod
            def pipe(data):
                reader,writer=os.pipe(); os.write(writer,data); os.close(writer)
                return os.fdopen(reader,'rb')
            def wait(self,timeout):
                self.calls+=1
                if self.calls==1 and getattr(self,'timeout',False): raise cg.subprocess.TimeoutExpired('inert-fixture',timeout)
                if hasattr(self,'budget'): self.budget.deadline=0
                return getattr(self,'code',0)
        for fault in ('nonascii','exit','wait_timeout','late_wait'):
            with self.subTest(fault=fault):
                process=FakeProcess(b'\xff' if fault=='nonascii' else b'fixture\n')
                budget=cg.Budget()
                if fault=='exit': process.code=2
                if fault=='wait_timeout': process.timeout=True
                if fault=='late_wait': process.budget=budget
                with patch.object(cg.subprocess,'Popen',return_value=process),patch.object(cg.os,'killpg') as terminate:
                    with self.assertRaises(cg.Refusal): REAL_QUERY([str(self.toolpaths['systemctl']),'--version'],budget)
                    self.assertEqual(terminate.call_count,1 if fault=='wait_timeout' else 0)
                    self.assertTrue(process.stdout.closed); self.assertTrue(process.stderr.closed)


REAL_QUERY=cg.run_readonly
if __name__=='__main__': unittest.main()
