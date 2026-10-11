"""Inert trusted producer recipe. This module never fetches or uploads data.

Standalone Workflow source experiments have separate explicit authorization.
Permanent Workflow installation still needs separate permission, fresh public
database receipts, and a trusted hosted runner boundary.
"""
from contextlib import contextmanager
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import stat
import subprocess
import sys
import tempfile
from osv_completion import IncompleteScan, parse_analysis, parse_report_text, validate
from osv_trusted import CONFIG, GO_VERSION, catalog, database, strict_json, check_database_applicability
from osv_io import bounded_run, Budget, STDOUT_LIMIT

PINS = {
    ('Darwin','arm64'): {'osv-scanner':'c770f6e98fe247f74aade57b2d8fdde25a79b582e6f00981414ef0da8be8d140',
        'govulncheck':'660e2b83df56694f181a8b7c80b2d5f666c92d1f037368cd0f0d2f9adbdb8379'},
    ('Linux','x86_64'): {'osv-scanner':'c810df66004c75f734eb4502ea5ff44115e4e3444e18d8b38bd87ff33094d69d',
        'govulncheck':'55425f57ab2501c257efd0fe4abe361974849fab3e54dd0e8f04d4d3c57a2d65'}
}


def fingerprint(root, budget=None):
    """Freeze all source bytes/modes, including ignored input, without following links."""
    result={}; total=0; entries=0; pending=[root]
    while pending:
        directory=pending.pop()
        with os.scandir(directory) as scanned:
            for entry in scanned:
                if entry.name=='.git':continue
                if budget is not None:budget.left()
                entries+=1
                if entries>20000:raise IncompleteScan('source entry limit exceeded')
                path=Path(entry.path);info=entry.stat(follow_symlinks=False);mode=info.st_mode
                if stat.S_ISLNK(mode):raise IncompleteScan('source symlink outside reviewed contract')
                if stat.S_ISDIR(mode):pending.append(path);continue
                if not stat.S_ISREG(mode):raise IncompleteScan('nonregular source input')
                size=info.st_size;total+=size
                if size>32*1024*1024 or total>1024*1024*1024 or len(result)>=10000:
                    raise IncompleteScan('source inventory exceeds bounded contract')
                result[str(path.relative_to(root))]=[stat.S_IMODE(mode),hashlib.sha256(path.read_bytes()).hexdigest()]
    if not result:raise IncompleteScan('empty source root')
    return result


def protocol(text):
    decoder=json.JSONDecoder(object_pairs_hook=lambda pairs: strict_json(json.dumps(dict(pairs))))
    # Exact duplicate-key and NaN checks are applied to every raw record separately.
    result=[];remaining=text
    while remaining.strip():
        _,end=decoder.raw_decode(remaining.lstrip())
        raw=remaining.lstrip()[:end];result.append(strict_json(raw));remaining=remaining.lstrip()[end:]
    return result



CGROUP_ROOT = Path('/sys/fs/cgroup')
PROC_CGROUP = Path('/proc/self/cgroup')
PROC_STATUS = Path('/proc/self/status')

def memory_event_snapshot(witness, budget):
    """Readonly evidence in the existing witnessed private aggregate group."""
    if (witness.get('manager_pid') != os.getpid() or
        witness.get('kernel_limits') != {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}):
        raise IncompleteScan('memory diagnostic lacks exact owned container witness')
    info = CGROUP_ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or CGROUP_ROOT.resolve(strict=True) != CGROUP_ROOT:
        raise IncompleteScan('memory diagnostic cgroup path differs')
    def read(path):
        budget.left()
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as f:
            opened = os.fstat(f.fileno())
            if not stat.S_ISREG(opened.st_mode):raise IncompleteScan('nonregular cgroup diagnostic')
            raw = f.read(16385)
            if len(raw) > 16384:raise IncompleteScan('cgroup diagnostic size limit')
        budget.left()
        return raw.decode('ascii')
    group = read(PROC_CGROUP)
    if group != '0::/\n' or group != witness['child']['group']:
        raise IncompleteScan('memory diagnostic escaped witnessed private cgroup')
    if str(os.getpid()) not in read(CGROUP_ROOT / 'cgroup.procs').split():
        raise IncompleteScan('memory diagnostic manager outside owned group')
    limits = {name:read(CGROUP_ROOT / name).strip() for name in witness['kernel_limits']}
    if limits != witness['kernel_limits']:raise IncompleteScan('memory diagnostic quota changed')
    raw = read(CGROUP_ROOT / 'memory.events');counters = {}
    for line in raw.splitlines():
        pieces = line.split()
        if (len(pieces) != 2 or not re.fullmatch('[a-z_]{1,64}', pieces[0]) or
            not re.fullmatch('[0-9]{1,20}', pieces[1]) or pieces[0] in counters):
            raise IncompleteScan('malformed cgroup memory.events')
        counters[pieces[0]] = int(pieces[1])
    if not {'oom','oom_kill'} <= counters.keys():
        raise IncompleteScan('cgroup memory.events OOM counters absent')
    measurements = {}
    for name in ('memory.current', 'memory.peak'):
        value = read(CGROUP_ROOT / name).strip()
        if not re.fullmatch('[0-9]{1,20}', value):
            raise IncompleteScan('malformed cgroup memory measurement')
        measurements[name] = int(value)
    raw_stat = read(CGROUP_ROOT / 'memory.stat'); memory_stat = {}
    for line in raw_stat.splitlines():
        pieces = line.split()
        if (len(pieces) != 2 or not re.fullmatch('[a-z_]{1,64}', pieces[0]) or
            not re.fullmatch('[0-9]{1,20}', pieces[1]) or pieces[0] in memory_stat):
            raise IncompleteScan('malformed cgroup memory.stat')
        memory_stat[pieces[0]] = int(pieces[1])
    if not {'anon', 'file', 'shmem'} <= memory_stat.keys():
        raise IncompleteScan('cgroup memory.stat fields absent')
    status = read(PROC_STATUS); manager_memory = {}
    for line in status.splitlines():
        field, sep, value = line.partition(':')
        if field not in ('VmRSS', 'RssAnon'):
            continue
        pieces = value.split()
        if (not sep or field in manager_memory or len(pieces) != 2 or pieces[1] != 'kB' or
            not re.fullmatch('[0-9]{1,20}', pieces[0])):
            raise IncompleteScan('malformed manager memory status')
        manager_memory[field] = int(pieces[0]) * 1024
    if set(manager_memory) != {'VmRSS', 'RssAnon'}:
        raise IncompleteScan('manager memory status absent')
    if measurements['memory.peak'] < measurements['memory.current']:
        raise IncompleteScan('cgroup memory peak contradicts current')
    return {'measurements_bytes': measurements,
        'memory_stat_bytes': {key:memory_stat[key] for key in ('anon','file','shmem')},
        'memory_stat_sha256':hashlib.sha256(raw_stat.encode()).hexdigest(),
        'manager_memory_bytes':manager_memory,
        'manager_status_sha256':hashlib.sha256(status.encode()).hexdigest(),
        'group':group, 'cgroup_identity':[info.st_dev,info.st_ino],
        'manager_pid':os.getpid(), 'limits':limits, 'raw':raw,
        'raw_sha256':hashlib.sha256(raw.encode()).hexdigest(), 'counters':counters}

def write_memory_evidence(out, name, value, budget):
    raw = (json.dumps(value,sort_keys=True,indent=2)+'\n').encode()
    if len(raw) > 65536:raise IncompleteScan('memory evidence size limit')
    budget.charge(len(raw));budget.left()
    with (out / name).open('xb') as f:
        os.fchmod(f.fileno(),0o600);f.write(raw);f.flush();os.fsync(f.fileno())
    budget.left()
    return hashlib.sha256(raw).hexdigest()

def begin_memory(out, label, witness, budget, argv):
    if witness is None:return None
    if not re.fullmatch(r'root[0-9]+-(?:source-catalog|govulncheck)|scanner|database-validation|analysis-validation',label) or budget is None:
        raise IncompleteScan('unreviewed memory diagnostic label/budget')
    before=memory_event_snapshot(witness,budget)
    receipt_sha=write_memory_evidence(out,label+'.memory-before.json',
        {'schema':'workflow.offline-scanner.memory-before.v1','complete':False,'accepted':False,
         'label':label,'argv':argv,'snapshot':before},budget)
    before['_receipt_sha256']=receipt_sha
    return before


def end_memory(out, label, witness, budget, before, native_exit, captured):
    if before is None:return
    result={'schema':'workflow.offline-scanner.memory-after.v1','complete':False,'accepted':False,
        'label':label,'native_exit':native_exit,'native_capture_complete':captured,
        'before_sha256':before['_receipt_sha256'],
        'scope':'aggregate container counters; a specific killed PID is not established'}
    try:
        if hashlib.sha256(read_owned(out/(label+'.memory-before.json'),65536)).hexdigest()!=before['_receipt_sha256']:
            raise IncompleteScan('memory before receipt changed')
        after=memory_event_snapshot(witness,budget)
        if (after['cgroup_identity']!=before['cgroup_identity'] or after['group']!=before['group'] or
            after['limits']!=before['limits'] or set(after['counters'])!=set(before['counters'])):
            raise IncompleteScan('memory diagnostic group/counter set changed')
        delta={key:after['counters'][key]-value for key,value in before['counters'].items()}
        if any(value<0 for value in delta.values()):raise IncompleteScan('memory diagnostic counter decreased')
        if after['measurements_bytes']['memory.peak']<before['measurements_bytes']['memory.peak']:
            raise IncompleteScan('memory diagnostic peak decreased')
        result.update(snapshot=after,delta=delta,complete=True,
            observed_oom_kill_increment=delta['oom_kill']>0)
    except Exception as error:
        result['diagnostic_error_type']=type(error).__name__;raise
    finally:
        write_memory_evidence(out,label+'.memory-after.json',result,budget)


@contextmanager
def validation_memory(out, witness, budget):
    before=begin_memory(out,'analysis-validation',witness,budget,[])
    try:
        yield
    finally:
        end_memory(out,'analysis-validation',witness,budget,before,None,False)


def execute(argv, cwd, env, out, label, timeout=240, budget=None, *, memory_witness=None):
    """Fresh producer-owned stdout/stderr/rc, never candidate supplied files."""
    before=begin_memory(out,label,memory_witness,budget,argv);run=None;captured=False
    try:
        try:
            run=bounded_run(argv,cwd,env,timeout,budget)
        except (OSError,subprocess.TimeoutExpired) as error:
            raise IncompleteScan('producer process unavailable or timed out') from error
        for suffix,text in [('stdout',run.stdout),('stderr',run.stderr),('rc',str(run.returncode)+'\n')]:
            write_owned(out/(label+'.'+suffix),text.encode())
        captured=True
        return run
    finally:
        end_memory(out,label,memory_witness,budget,before,None if run is None else run.returncode,captured)


def source_contract(root, go_binary, env, out, label, budget, memory_witness=None):
    listed=execute([str(go_binary),'list','-deps','-json','./...'],root,env,out,label+'-source-catalog',budget=budget,memory_witness=memory_witness)
    if listed.returncode or listed.stderr:
        raise IncompleteScan('independent source/package loading failed')
    modules={}; roots=[]
    for package in protocol(listed.stdout):
        if package.get('Error') or package.get('DepsErrors'):
            raise IncompleteScan('independent source/package error')
        if not package.get('DepOnly'): roots.append(package['ImportPath'])
        mod=package.get('Module')
        if mod:
            if not isinstance(mod.get('Path'),str) or not mod['Path']:
                raise IncompleteScan('invalid source module identity')
            path=mod['Path'];modules[path]=mod
            replacement=mod.get('Replace')
            if replacement:
                if not isinstance(replacement.get('Path'),str) or not replacement['Path']:
                    raise IncompleteScan('invalid source replacement identity')
                modules[replacement['Path']]=replacement
    if not roots or not modules: raise IncompleteScan('empty source/module analysis contract')
    versions=[]
    for mod in modules.values():
        effective=mod.get('Replace',mod)
        versions.append((effective['Path'],effective.get('Version','')))
    versions.append(('stdlib','v'+GO_VERSION))
    return roots,versions,set(modules)



def read_owned(path, limit=STDOUT_LIMIT):
    """Read once from a bounded owned FD; reject metadata/content-size races."""
    def identity(info):
        return (info.st_dev,info.st_ino,info.st_mode,info.st_uid,info.st_nlink,
            info.st_size,info.st_mtime_ns,info.st_ctime_ns)
    info=path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid!=os.getuid() or
        stat.S_IMODE(info.st_mode)!=0o600 or info.st_nlink!=1 or info.st_size>limit):
        raise IncompleteScan('deferred record file ownership/shape differs')
    with os.fdopen(os.open(path,os.O_RDONLY|os.O_NOFOLLOW),'rb') as f:
        opened=os.fstat(f.fileno())
        if identity(opened)!=identity(info):
            raise IncompleteScan('deferred record changed during open')
        raw=f.read(limit+1)
        closed=os.fstat(f.fileno())
        if identity(closed)!=identity(opened) or identity(path.lstat())!=identity(closed):
            raise IncompleteScan('deferred record changed during read')
    if len(raw)>limit or len(raw)!=opened.st_size:
        raise IncompleteScan('deferred record byte bound/size differs')
    return raw


def write_owned(path, raw):
    if len(raw)>STDOUT_LIMIT:raise IncompleteScan('owned record byte bound exceeded')
    with path.open('xb') as f:
        os.fchmod(f.fileno(),0o600);f.write(raw);f.flush();os.fsync(f.fileno())


def flat_identity(flat, budget):
    info=flat.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid!=os.getuid() or
        stat.S_IMODE(info.st_mode)!=0o700 or flat.resolve()!=flat):
        raise IncompleteScan('derived Go database ownership differs')
    identity=hashlib.sha256();total=0;count=0
    for path in sorted(flat.iterdir()):
        budget.left();count+=1
        if count>200000 or not path.name.endswith('.json'):
            raise IncompleteScan('derived Go database member inventory differs')
        raw=read_owned(path,2*1024*1024);total+=len(raw)
        if total>64*1024*1024:raise IncompleteScan('derived Go database byte bound exceeded')
        identity.update(path.name.encode()+b'\0'+hashlib.sha256(raw).digest())
    if not count:raise IncompleteScan('derived Go database empty')
    return {'sha256':identity.hexdigest(),'count':count,'bytes':total}


def load_databases(db_base, receipts, now, expected, budget):
    records={};identity={}
    for ecosystem in sorted({key[0] for packages in expected.values() for key in packages}):
        receipt=receipts[ecosystem];digest=receipt['sha256']
        records[ecosystem]=database(Path(db_base)/'osv-scalibr'/ecosystem/'all.zip',
            ecosystem,digest,receipt,now,budget)
        identity[ecosystem]=digest
    check_database_applicability(records,expected)
    return identity,records


def database_worker(request_path, core_contract):
    """Trusted validation only. Its dictionaries die when this owned child is reaped."""
    raw=read_owned(request_path);request=strict_json(raw)
    out=request_path.parent;info=out.lstat()
    if (request_path.name!='database-request.json' or not re.fullmatch('osv-proof-[a-z0-9_]+',out.name) or
        not stat.S_ISDIR(info.st_mode) or info.st_uid!=os.getuid() or stat.S_IMODE(info.st_mode)!=0o700 or
        out.resolve()!=out or request.get('owner_pid')!=os.getppid() or
        request.get('core_contract_sha256')!=core_contract):
        raise IncompleteScan('database child lacks exact owned request')
    if (set(request)!={'owner_pid','core_contract_sha256','db_base','receipts','now','expected','deadline',
        'remaining_bytes','memory_witness','cgroup_identity'} or type(request['deadline']) not in (int,float) or
        type(request['remaining_bytes']) is not int or not 0<request['remaining_bytes']<=128*1024*1024):
        raise IncompleteScan('database child request shape/budget differs')
    budget=Budget();budget.deadline=request['deadline'];budget.limit=request['remaining_bytes'];budget.left()
    witness=request['memory_witness']
    if witness is not None:
        witness=dict(witness,manager_pid=os.getpid())
        observed=memory_event_snapshot(witness,budget)
        if observed['cgroup_identity']!=request['cgroup_identity']:
            raise IncompleteScan('database child escaped owned quota group')
    flat=out/'go-db'
    info=flat.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid!=os.getuid() or
        stat.S_IMODE(info.st_mode)!=0o700 or flat.resolve()!=flat or list(flat.iterdir())):
        raise IncompleteScan('derived Go database must begin owned and empty')
    identity,records=load_databases(request['db_base'],request['receipts'],
        datetime.fromisoformat(request['now']),request['expected'],budget)
    retained={'scope':'shallow dictionary and key bytes, not full retained RSS',
        'counts':{eco:len(values) for eco,values in records.items()},
        'shallow_bytes':{eco:sys.getsizeof(values)+sum(sys.getsizeof(key) for key in values)
            +sys.getsizeof(values.unknown_applicability) for eco,values in records.items()},
        'Go_full_records_shared_once':True,'npm_values_are_None':all(v is None for v in records.get('npm',{}).values())}
    retained['validated_memory']=memory_event_snapshot(witness,budget) if witness is not None else None
    for aid,advisory in records.get('Go',{}).items():
        budget.left()
        write_owned(flat/(aid+'.json'),json.dumps(advisory,ensure_ascii=False,separators=(',',':')).encode())
    result={'schema':'workflow.offline-scanner.database-validation.v1','accepted':False,'complete':True,
        'request_sha256':hashlib.sha256(raw).hexdigest(),'databases':identity,
        'flat':flat_identity(flat,budget),'retained':retained}
    encoded=(json.dumps(result,sort_keys=True,indent=2)+'\n').encode()
    budget.charge(len(encoded));budget.left();write_owned(out/'database-validation.json',encoded);budget.left()
    return 0


def prepare_databases(out, db_base, receipts, now, expected, env, core_contract, witness, budget):
    flat=out/'go-db';flat.mkdir(mode=0o700)
    request={'owner_pid':os.getpid(),'core_contract_sha256':core_contract,
        'db_base':str(Path(db_base).resolve()),'receipts':receipts,'now':now.isoformat(),'expected':expected,
        'deadline':budget.deadline,'remaining_bytes':budget.limit-budget.used,'memory_witness':witness,
        'cgroup_identity':memory_event_snapshot(witness,budget)['cgroup_identity'] if witness is not None else None}
    raw=(json.dumps(request,sort_keys=True,indent=2)+'\n').encode()
    budget.charge(len(raw));write_owned(out/'database-request.json',raw);budget.left()
    result=execute([sys.executable,'-I','-S','-B','-X','pycache_prefix='+str(out/'absent-database-cache'),
        str(Path(__file__).with_name('run.py')),'--database-worker',str(out/'database-request.json')],
        out,env,out,'database-validation',budget=budget,memory_witness=witness)
    if result.returncode or result.stdout or result.stderr:
        raise IncompleteScan('owned database validation child failed')
    raw_result=read_owned(out/'database-validation.json');budget.charge(len(raw_result))
    proof=strict_json(raw_result)
    expected_identity={eco:receipts[eco]['sha256'] for eco in {key[0] for rows in expected.values() for key in rows}}
    if (proof.get('schema')!='workflow.offline-scanner.database-validation.v1' or proof.get('accepted') is not False or
        proof.get('complete') is not True or proof.get('request_sha256')!=hashlib.sha256(raw).hexdigest() or
        proof.get('databases')!=expected_identity or proof.get('flat')!=flat_identity(flat,budget)):
        raise IncompleteScan('database validation child receipt differs')
    return expected_identity,proof['flat']


def capture_binding(out, label):
    return {suffix:hashlib.sha256(read_owned(out/(label+'.'+suffix))).hexdigest()
        for suffix in ('stdout','stderr','rc')}


def deferred_capture(out, label, binding):
    values=[]
    for suffix in ('stdout','rc','stderr'):
        raw=read_owned(out/(label+'.'+suffix))
        if hashlib.sha256(raw).hexdigest()!=binding[suffix]:
            raise IncompleteScan('deferred native capture changed')
        values.append(raw.decode('utf-8'))
    return tuple(values)


def run_one(root, paths, tools, go_binary, db_base, receipts, now, output_parent, env, *, admitted_supplier=None, cgroup_witness=None, core_contract_sha256=None):
    """Execute only after the caller verifies authority for this exact source root.

    No command-line entrypoint intentionally: the approved profile supplies the
    exact paths, receipts, compiled assets, sanitized environment and source.
    """
    if sys.version_info<(3,11):raise IncompleteScan('trusted producer requires Python >=3.11')
    if not isinstance(core_contract_sha256,str) or not re.fullmatch('[0-9a-f]{64}',core_contract_sha256):
        raise IncompleteScan('database validation requires exact trusted core contract')
    root=Path(root).resolve();parent=Path(output_parent).resolve();tools=Path(tools).resolve()
    if parent.is_relative_to(root) or not parent.is_dir() or parent.is_symlink():
        raise IncompleteScan('output parent must be trusted and outside source')
    pins=PINS.get((platform.system(),platform.machine()))
    if admitted_supplier is not None:
        if (platform.system(),platform.machine())!=('Linux','x86_64'):
            raise IncompleteScan('hosted supplier admission requires native Linux/amd64')
        from osv_supplier import verify_delivery
        verified=verify_delivery(admitted_supplier)
        pins={name:verified[name] for name in ('osv-scanner','govulncheck')}
    if not pins: raise IncompleteScan('unreviewed producer platform')
    for name,digest in pins.items():
        path=tools/name
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=digest:
            raise IncompleteScan('supplier binary pin mismatch')
    required={'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly',
              'CGO_ENABLED':'0','GOVCS':'*:off','GOPRIVATE':'','GONOPROXY':'none','GONOSUMDB':'none'}
    if any(env.get(k)!=v for k,v in required.items()) or env['PATH'].split(os.pathsep)[0]!=str(Path(go_binary).parent):
        raise IncompleteScan('unbound compiler/network environment')
    budget=Budget()
    version=bounded_run([str(go_binary),'version'],root,env,30,budget)
    if version.returncode or version.stderr or not version.stdout.startswith('go version go'+GO_VERSION+' '):
        raise IncompleteScan('actual analysis compiler mismatch')
    before=fingerprint(root,budget)
    expected,manifests=catalog(root,paths,go_binary,env,budget)
    out=Path(tempfile.mkdtemp(prefix='osv-proof-',dir=parent));out.chmod(0o700)
    config=out/'trusted.toml';config.write_text(CONFIG);config.chmod(0o600)
    db_identity,derived_identity=prepare_databases(out,db_base,receipts,now,expected,env,
        core_contract_sha256,cgroup_witness,budget)
    flat=out/'go-db';deferred=[]
    for n,(path,packages) in enumerate(expected.items()):
        if not any(k[0]=='Go' for k in packages): continue
        source=Path(path).parent;module=manifests[str(Path(path).relative_to(root))]['module']
        roots,versions,source_names=source_contract(source,go_binary,env,out,'root'+str(n),budget,cgroup_witness)
        manifest=manifests[str(Path(path).relative_to(root))]
        identity_names=source_names | set(manifest['module_names']) | {name for ecosystem,name,version in packages if ecosystem=='Go'}
        if n==13 and cgroup_witness is not None and str(Path(path).relative_to(root))!='example/go.mod':
            raise IncompleteScan('memory diagnostic root13 manifest identity changed')
        if flat_identity(flat,budget)!=derived_identity:
            raise IncompleteScan('derived Go database changed before native analysis')
        independent=execute([str(tools/'govulncheck'),'-db',flat.as_uri(),'-C',str(source),'-json','./...'],
                            source,env,out,'root'+str(n)+'-govulncheck',budget=budget,
                            memory_witness=cgroup_witness)
        if independent.returncode or independent.stderr.strip():
            raise IncompleteScan('independent govulncheck failed')
        deferred.append((path,source,module,versions,roots,identity_names,'root'+str(n)+'-govulncheck',
            capture_binding(out,'root'+str(n)+'-govulncheck')))
        del independent
    report=out/'results.json'
    argv=[str(tools/'osv-scanner'),'scan','source','--offline','--local-db-path='+str(Path(db_base).resolve()),
          '--no-resolve','--config='+str(config),'--all-packages','--call-analysis=go','--format=json']
    for path in expected: argv.extend(['-L',path])
    scanned=execute(argv,root,env,out,'scanner',timeout=720,budget=budget,memory_witness=cgroup_witness)
    report.write_text(scanned.stdout);report.chmod(0o600)
    scanner_binding=capture_binding(out,'scanner');del scanned
    if flat_identity(flat,budget)!=derived_identity:
        raise IncompleteScan('derived Go database changed during native analysis')
    with validation_memory(out,cgroup_witness,budget):
        _,advisories=load_databases(db_base,receipts,now,expected,budget)
        analysis={}
        for path,source,module,versions,roots,identity_names,label,binding in deferred:
            stdout,rc,stderr=deferred_capture(out,label,binding)
            analysis[path]=parse_analysis(stdout,int(rc),stderr,source,module,GO_VERSION,flat.as_uri(),
                advisories['Go'],versions,roots,identity_names)
        scanner_stdout,scanner_rc,scanner_stderr=deferred_capture(out,'scanner',scanner_binding)
        if read_owned(report).decode('utf-8')!=scanner_stdout:
            raise IncompleteScan('deferred native report changed')
        local_count=sum(len(m.get('local_references',[])) for m in manifests.values())
        blocking=validate(parse_report_text(scanner_stdout),expected,int(scanner_rc),scanner_stderr,analysis,local_count,budget)
    if fingerprint(root,budget)!=before or config.read_text()!=CONFIG:
        raise IncompleteScan('source or trusted configuration changed during proof')
    for ecosystem,digest in db_identity.items():
        hashed=hashlib.sha256()
        with (Path(db_base)/'osv-scalibr'/ecosystem/'all.zip').open('rb') as archive:
            for block in iter(lambda:archive.read(1024*1024),b''):
                budget.left();hashed.update(block)
        if hashed.hexdigest()!=digest:raise IncompleteScan('database changed during proof')
    for name,digest in pins.items():
        if hashlib.sha256((tools/name).read_bytes()).hexdigest()!=digest:
            raise IncompleteScan('supplier changed during proof')
    report_sha256=hashlib.sha256(scanner_stdout.encode()).hexdigest()
    if hashlib.sha256(read_owned(report)).hexdigest()!=report_sha256:
        raise IncompleteScan('native report changed after analysis')
    receipt={'report_sha256':report_sha256,'source':before,'manifests':manifests,'databases':db_identity,'compiler':version.stdout.strip(),
             'supplier_pins':pins,'blocking':sorted(blocking),'complete':True,'execution':'local',
             'unverified_called_observations':{str(Path(path).relative_to(root)):[
                 {'module':name,'advisory_id':aid,'function_trace_count':count,
                  'classification':'no raw Go symbol metadata; not independent version applicability proof'}
                 for name,aid,count in evidence.unverified_called] for path,evidence in analysis.items()}}
    encoded=json.dumps(receipt,sort_keys=True,indent=2)+'\n'
    if len(encoded.encode())>STDOUT_LIMIT:raise IncompleteScan('producer receipt size limit exceeded')
    budget.charge(len(encoded.encode()));budget.left()
    path=out/'receipt.json';path.write_text(encoded);path.chmod(0o600)
    budget.left()
    return out,blocking
