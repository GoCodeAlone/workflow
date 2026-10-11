"""Local-only trusted input/database preparation. No fetching or vulnerability query."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import zipfile
from osv_completion import IncompleteScan, _object
from osv_io import bounded_run

GO_VERSION = '1.27.2'
DATABASE_ARCHIVE_BYTES = {'Go':128*1024*1024,'npm':256*1024*1024}
DATABASE_MEMBERS = {'Go':200000,'npm':262144}
CONFIG = 'ScanGoModVersion = true\nGoVersionOverride = "1.27.2"\n'


def strict_json(text):
    return json.loads(text, object_pairs_hook=_object,
                      parse_constant=lambda value: (_ for _ in ()).throw(IncompleteScan('invalid JSON constant')))



class DatabaseRecords(dict):
    """Retain every validated record and names with unknown active applicability."""
    def __init__(self):
        super().__init__()
        self.unknown_applicability={}


def check_database_applicability(databases,expected):
    """Never complete a scanned package whose active record lacks version data."""
    for ecosystem,records in databases.items():
        if not isinstance(records,DatabaseRecords):raise IncompleteScan('unbound database inventory')
        for packages in expected.values():
            for eco,name,_ in packages:
                if eco==ecosystem and (name in records.unknown_applicability or '*' in records.unknown_applicability):
                    raise IncompleteScan('database lacks applicability for a scanned package')


def database(path, ecosystem, digest, receipt, now, budget=None):
    """Validate all members before a pinned loader can silently skip one.

    receipt is produced by a future trusted fixed-URL acquisition, never a PR.
    SHA256 authenticates the bound bytes; Google HTTPS/GCS metadata is provenance,
    not a signature. No data is downloaded here.
    """
    path = Path(path)
    if ecosystem not in ('Go','npm') or receipt.get('url') !=             'https://osv-vulnerabilities.storage.googleapis.com/'+ecosystem+'/all.zip':
        raise IncompleteScan('wrong database origin/ecosystem')
    if not re.fullmatch(r'[0-9]+',str(receipt.get('generation',''))) or not re.fullmatch(r'[0-9a-f]{64}',digest):
        raise IncompleteScan('missing database generation/digest')
    try:
        fetched = datetime.fromisoformat(receipt['fetched_at'].replace('Z','+00:00'))
        if fetched.tzinfo is None or now.tzinfo is None or not 0 <= (now-fetched).total_seconds() <= 86400:
            raise IncompleteScan('stale or future database receipt')
        if path.is_symlink() or not path.is_file() or path.stat().st_size > DATABASE_ARCHIVE_BYTES[ecosystem]:
            raise IncompleteScan('missing/oversize/symlink database')
        hashed=hashlib.sha256();captured=0
        with path.open('rb') as source:
            for chunk in iter(lambda:source.read(1024*1024),b''):
                if budget is not None:budget.left()
                captured+=len(chunk)
                if captured>DATABASE_ARCHIVE_BYTES[ecosystem]:raise IncompleteScan('growing/oversize database')
                hashed.update(chunk)
        if hashed.hexdigest() != digest:
            raise IncompleteScan('database digest mismatch')
        result = DatabaseRecords(); total = 0
        with zipfile.ZipFile(path) as archive:
            infos = archive.infolist()
            names = [i.filename for i in infos]
            if not infos or len(infos)>DATABASE_MEMBERS[ecosystem] or len(names)!=len(set(names)):
                raise IncompleteScan('empty/duplicate/oversize database inventory')
            for info in infos:
                if budget is not None:budget.left()
                if info.is_dir() or PurePosixPath(info.filename).name != info.filename or not info.filename.endswith('.json')                         or info.flag_bits & 1 or (info.external_attr>>16)&0o170000 == 0o120000:
                    raise IncompleteScan('invalid database member path/type')
                total += info.file_size
                if info.file_size>2*1024*1024 or total>({'Go':64,'npm':512}[ecosystem])*1024*1024:
                    raise IncompleteScan('database decompression limit')
                advisory = strict_json(archive.read(info).decode('utf-8'))
                aid = advisory.get('id') if isinstance(advisory,dict) else None
                if not isinstance(aid,str) or not aid or aid+'.json' != info.filename or aid in result:
                    raise IncompleteScan('database member identity mismatch')
                schema = advisory.get('schema_version','1.0.0')
                if not isinstance(schema,str) or not re.fullmatch(r'1\.[0-9]\.[0-9]+',schema):
                    raise IncompleteScan('unsupported database schema')
                if not isinstance(advisory.get('modified'),str) or not re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})',advisory['modified']):
                    raise IncompleteScan('invalid modification timestamp')
                modified = datetime.fromisoformat(advisory['modified'].replace('Z','+00:00'))
                if modified.tzinfo is None or modified>now:
                    raise IncompleteScan('invalid database modification timestamp')
                if 'database_specific' in advisory and not isinstance(advisory['database_specific'],dict):raise IncompleteScan('invalid database-specific object')
                for key in ['summary','details','schema_version']:
                    if key in advisory and not isinstance(advisory[key],str):raise IncompleteScan('invalid advisory string')
                for key in ['aliases','upstream','related']:
                    if key in advisory and (not isinstance(advisory[key],list) or any(not isinstance(v,str) or not v for v in advisory[key])):
                        raise IncompleteScan('invalid advisory identifier list')
                for key,required_keys in [('references',['type','url']),('credits',['name']),('severity',['type','score'])]:
                    if key in advisory:
                        if not isinstance(advisory[key],list) or any(not isinstance(v,dict) or any(not isinstance(v.get(k),str) or not v[k] for k in required_keys) for v in advisory[key]):
                            raise IncompleteScan('invalid typed advisory metadata')
                        for item in advisory[key]:
                            if key=='severity' and 'source' in item and not isinstance(item['source'],str):raise IncompleteScan('invalid severity source')
                            if key=='credits':
                                if 'contact' in item and (not isinstance(item['contact'],list) or any(not isinstance(v,str) or not v for v in item['contact'])):raise IncompleteScan('invalid credit contact')
                                if 'type' in item and not isinstance(item['type'],str):raise IncompleteScan('invalid credit type')
                for key in ['published','withdrawn']:
                    if key in advisory:
                        if not isinstance(advisory[key],str) or not re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})',advisory[key]):
                            raise IncompleteScan('invalid advisory timestamp')
                        datetime.fromisoformat(advisory[key].replace('Z','+00:00'))
                affected = advisory.get('affected',[])
                withdrawn=advisory.get('withdrawn')
                if withdrawn and datetime.fromisoformat(withdrawn.replace('Z','+00:00'))>now:
                    raise IncompleteScan('future withdrawal timestamp')
                if not isinstance(affected,list) or not affected and not withdrawn:
                    raise IncompleteScan('database missing affected inventory')
                requested=False
                for a in affected:
                    if not isinstance(a,dict):raise IncompleteScan('invalid affected object')
                    p = a.get('package')
                    if 'package' not in a:
                        if not isinstance(a.get('ranges'),list) or not a['ranges'] or any(not isinstance(v,dict) or v.get('type')!='GIT' for v in a['ranges']):
                            raise IncompleteScan('package-less affected entry must be Git-only')
                    elif not isinstance(p,dict) or not isinstance(p.get('ecosystem'),str) or not p['ecosystem'] or not isinstance(p.get('name'),str) or not p['name']:
                        raise IncompleteScan('wrong database package ecosystem/identity')
                    requested=requested or isinstance(p,dict) and p['ecosystem']==ecosystem
                    if 'ecosystem_specific' in a and not isinstance(a['ecosystem_specific'],dict):raise IncompleteScan('invalid ecosystem-specific object')
                    if isinstance(p,dict) and p['ecosystem']=='Go':
                        imports=a.get('ecosystem_specific',{}).get('imports',[])
                        if not isinstance(imports,list):raise IncompleteScan('invalid Go imports')
                        for imported in imports:
                            if not isinstance(imported,dict) or not isinstance(imported.get('path'),str) or not imported['path']:
                                raise IncompleteScan('invalid Go import identity')
                            for key in ['symbols','goos','goarch']:
                                if key in imported and (not isinstance(imported[key],list) or any(not isinstance(v,str) or not v for v in imported[key])):
                                    raise IncompleteScan('invalid typed Go import metadata')
                    if 'database_specific' in a and not isinstance(a['database_specific'],dict):raise IncompleteScan('invalid affected database-specific object')
                    if isinstance(p,dict) and 'purl' in p and not isinstance(p['purl'],str):raise IncompleteScan('invalid package purl')
                    ranges = a.get('ranges',[]); versions = a.get('versions',[])
                    if not isinstance(ranges,list) or not isinstance(versions,list) or any(not isinstance(v,str) or not v for v in versions) :
                        raise IncompleteScan('database missing affected versions')
                    version_range=any(isinstance(v,dict) and v.get('type') in ('SEMVER','ECOSYSTEM') for v in ranges)
                    if not version_range and not versions and not withdrawn and isinstance(p,dict) and p['ecosystem']==ecosystem:
                        result.unknown_applicability.setdefault(p['name'],[]).append(aid)
                    for vrange in ranges:
                        if not isinstance(vrange,dict):raise IncompleteScan('invalid database affected range')
                        if vrange.get('type')=='GIT' and (not isinstance(vrange.get('repo'),str) or not vrange['repo']):raise IncompleteScan('missing Git range repository')
                        events = vrange.get('events') if isinstance(vrange,dict) else None
                        if vrange.get('type') not in ('SEMVER','ECOSYSTEM','GIT') or not isinstance(events,list) or not events:
                            raise IncompleteScan('invalid database affected range')
                        for event in events:
                            if not isinstance(event,dict) or len(event)!=1 or not set(event)<= {'introduced','fixed','last_affected','limit'}                                     or not all(isinstance(v,str) and v for v in event.values()):
                                raise IncompleteScan('invalid database affected event')
                if affected and not requested and not withdrawn:raise IncompleteScan('database record lacks requested ecosystem')
                result[aid] = advisory if ecosystem=='Go' else None
        return result
    except IncompleteScan:
        raise
    except (OSError,ValueError,KeyError,TypeError,AttributeError,zipfile.BadZipFile,RuntimeError) as error:
        raise IncompleteScan('malformed or unreadable database') from error


def catalog(root, paths, go_binary, env, budget=None):
    """Independent exact public Go/npm lockfile inventory, including local closure."""
    root = Path(root).resolve(); paths = sorted(paths)
    if not paths or len(paths)!=len(set(paths)):
        raise IncompleteScan('empty/duplicate lockfile contract')
    expected = {}; manifests = {}; locals_to_cover = set()
    for relative in paths:
        if budget is not None:budget.left()
        file = root/relative
        if file.is_symlink() or not file.is_file() or not file.resolve().is_relative_to(root):
            raise IncompleteScan('input escapes source root')
        if any(parent.is_symlink() for parent in file.parents if parent.is_relative_to(root)):
            raise IncompleteScan('symlinked input parent')
        if file.stat().st_size > 16*1024*1024:
            raise IncompleteScan('manifest exceeds input limit')
        raw = file.read_bytes()
        if file.name == 'go.mod':
            run = bounded_run([str(go_binary),'mod','edit','-json'],file.parent,env,30,budget)
            if run.returncode or run.stderr:
                raise IncompleteScan('Go manifest parser failed')
            mod = strict_json(run.stdout)
            declared=mod.get('Go','')
            if not re.fullmatch(r'1\.[0-9]+(?:\.[0-9]+)?',declared) or int(declared.split('.')[1]) < 17:
                raise IncompleteScan('unsupported pre-1.17 manifest')
            # Preserve original identities even when the scanner reports replacements.
            module_names = {mod['Module']['Path']} | {r['Path'] for r in mod.get('Require') or []}
            for replacement in mod.get('Replace') or []:
                module_names.update((replacement['Old']['Path'], replacement['New']['Path']))
            if any(not isinstance(name, str) or not name for name in module_names):
                raise IncompleteScan('invalid manifest module identity')
            packages = {(r['Path'],r['Version'].removeprefix('v')) for r in mod.get('Require') or []}
            for replacement in mod.get('Replace') or []:
                old,new = replacement['Old'],replacement['New']
                keys = [key for key in packages if key[0]==old['Path'] and
                        (not old.get('Version') or key[1]==old['Version'].removeprefix('v'))]
                for key in keys:
                    packages.remove(key)
                    if not new.get('Version'):
                        target = (file.parent/new['Path']).resolve()
                        if not target.is_relative_to(root) or not (target/'go.mod').is_file():
                            raise IncompleteScan('unbound local replacement')
                        locals_to_cover.add(str((target/'go.mod').relative_to(root)))
                    packages.add((new['Path'],new.get('Version','').removeprefix('v')))
            packages.add(('stdlib',GO_VERSION))
            local_references = sorted((name,str((file.parent/name).resolve().relative_to(root))) for name,version in packages if not version)
            expected[str(file)] = sorted(('Go',name,version) for name,version in packages)
            manifests[relative] = {'sha256':hashlib.sha256(raw).hexdigest(),'module':mod['Module']['Path'],
                                   'declared_go':mod['Go'],'toolchain':mod.get('Toolchain'),'packages':expected[str(file)],
                                   'local_references':local_references,'module_names':sorted(module_names)}
        elif file.name == 'package-lock.json':
            lock = strict_json(raw)
            if lock.get('lockfileVersion') != 3 or not isinstance(lock.get('packages'),dict):
                raise IncompleteScan('unsupported npm lockfile')
            packages = set()
            for location, p in lock['packages'].items():
                if not location: continue
                name = p.get('name')
                if not name:
                    parts = PurePosixPath(location).parts
                    name = '/'.join(parts[-2:]) if len(parts)>1 and parts[-2].startswith('@') else parts[-1]
                version = p.get('version')
                if not isinstance(version,str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?',version)                         or p.get('link') or (re.match(r'^(?:git(?:\+[^:]+)?|github|gitlab|bitbucket):',p.get('resolved','')) or '#' in p.get('resolved','')) or not isinstance(name,str) or not name:
                    raise IncompleteScan('unsupported npm local/Git/nonversion input')
                packages.add(('npm',name,version))
            expected[str(file)] = sorted(packages)
            manifests[relative] = {'sha256':hashlib.sha256(raw).hexdigest(),'packages':expected[str(file)]}
        else:
            raise IncompleteScan('unsupported named input')
        if file.read_bytes()!=raw:
            raise IncompleteScan('input changed during inventory')
    if not locals_to_cover <= set(paths):
        raise IncompleteScan('local replacement lacks an explicit scanned root')
    return expected, manifests

