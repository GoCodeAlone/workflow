"""Fixed hosted Docker-parent observation; no scanner execution or admission."""
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import sys
import time
import types

HERE = Path(__file__).resolve().parent
MINIMUM_OBSERVED_HEADROOM = 4831838208  # proposed2.5GiB plus2GiB reserve; not sufficient fit
IDENTITY = ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_REF','GITHUB_EVENT_NAME','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')
FILES = {'osv_container.py','headroom-worker.py','container-seccomp.json','run-docker-headroom-proof.py'}
STAT_STATES = frozenset('RSDZTtWXxKPI')  # proc_pid_stat(5), including documented historical states
DEAD_STAT_STATES = frozenset('ZXx')

class Refusal(RuntimeError): pass

def require(condition, reason):
    if not condition: raise Refusal(reason)

def pairs(rows):
    value = {}
    for key, item in rows:
        require(key not in value,'duplicate JSON field'); value[key] = item
    return value

def decode(raw):
    return json.loads(raw,object_pairs_hook=pairs,parse_constant=lambda _: (_ for _ in ()).throw(Refusal('nonfinite JSON')))

def digest(raw): return hashlib.sha256(raw).hexdigest()

def fingerprint(info):
    return (info.st_dev,info.st_ino,info.st_mode,info.st_uid,info.st_nlink,info.st_size,info.st_mtime_ns,info.st_ctime_ns)

def file_bytes(path, limit, *, uid=None, pseudo=False, directory_fd=None):
    target = path if directory_fd is None else path.name
    before = os.stat(target,dir_fd=directory_fd,follow_symlinks=False)
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= limit,'read file shape differs')
    require(uid is None or before.st_uid == uid,'read owner differs')
    fd = os.open(target,os.O_RDONLY|os.O_NOFOLLOW,dir_fd=directory_fd)
    with os.fdopen(fd,'rb') as stream:
        opened = os.fstat(stream.fileno()); raw = stream.read(limit+1); after = os.fstat(stream.fileno())
    final = os.stat(target,dir_fd=directory_fd,follow_symlinks=False)
    require(fingerprint(before) == fingerprint(opened) == fingerprint(after) == fingerprint(final),'opened read identity changed')
    require(len(raw) <= limit and (pseudo or len(raw) == before.st_size),'read length differs')
    return raw, {'device':opened.st_dev,'inode':opened.st_ino,'mode':stat.S_IMODE(opened.st_mode),
        'uid':opened.st_uid,'nlink':opened.st_nlink,'reported_size':opened.st_size,
        'mtime_ns':opened.st_mtime_ns,'ctime_ns':opened.st_ctime_ns}

class Directory:
    def __init__(self, path, uid):
        self.path = Path(path); before = self.path.lstat()
        require(stat.S_ISDIR(before.st_mode) and before.st_uid == uid and self.path.resolve() == self.path,'directory source differs')
        self.fd = os.open(self.path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        self.opened = os.fstat(self.fd)
        try:
            require((before.st_dev,before.st_ino,before.st_uid,before.st_mode) ==
                    (self.opened.st_dev,self.opened.st_ino,self.opened.st_uid,self.opened.st_mode),'directory rebound on open')
        except Exception: os.close(self.fd); raise
    def check(self):
        current = self.path.lstat(); opened = os.fstat(self.fd)
        identity = lambda value:(value.st_dev,value.st_ino,value.st_uid,value.st_mode)
        require(identity(current) == identity(opened) == identity(self.opened),'directory identity changed')
    def read(self, name, limit=4096, uid=0):
        require('/' not in name and name not in ('.','..'),'unbound anchored file')
        self.check(); result = file_bytes(Path(name),limit,uid=uid,pseudo=True,directory_fd=self.fd); self.check()
        return result
    def close(self): os.close(self.fd)

def write(path, value):
    raw = (json.dumps(value,sort_keys=True,indent=2)+'\n').encode()
    require(len(raw) <= 1048576,'receipt byte bound exceeded')
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(),0o600); stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    return digest(raw)

def retain(directory, name, raw, source_path, identity):
    require(len(raw) <= 65536 and re.fullmatch(r'[a-z0-9.-]+',name),'raw custody name/bound differs')
    path = directory/name
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(),0o600); stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    return {'relative_path':'raw/'+name,'bytes':len(raw),'sha256':digest(raw),
        'host_read_path':str(source_path),'opened_file_identity':identity}

def sources(expected=None):
    raw,_ = file_bytes(HERE/'docker-headroom-manifest.json',65536,uid=os.getuid())
    sha = digest(raw); require(expected is None or expected == sha,'measurement source manifest changed')
    manifest = decode(raw)
    require(manifest['schema'] == 'workflow.offline-scanner.docker-headroom-source.v1' and
        set(manifest['files']) == FILES and manifest['memory_bytes'] == 2147483648 and
        manifest['probe_timeout_seconds'] == 25 and manifest['kill_after_seconds'] == 5,
        'fixed measurement source contract differs')
    require(manifest['required_observed_headroom_bytes'] == MINIMUM_OBSERVED_HEADROOM and
        manifest['measurement_only'] is True and manifest['resource_expansion_authorized'] is False,'headroom audience/threshold differs')
    captured = {}
    for name, expected_hash in manifest['files'].items():
        require(re.fullmatch(r'[0-9a-f]{64}',expected_hash),'unbound source digest')
        data,_ = file_bytes(HERE/name,1048576,uid=os.getuid()); require(digest(data) == expected_hash,'measurement source bytes differ')
        captured[name] = data
    module = types.ModuleType('owned_headroom_container'); module.__file__ = str(HERE/'osv_container.py')
    # Compile the authenticated buffer rather than reopening it after hashing.
    exec(compile(captured['osv_container.py'],module.__file__,'exec'),module.__dict__)
    require(module.IMAGE == manifest['image'] and module.IMAGE_CONFIG == manifest['image_config'],'pinned image differs')
    return module,sha,manifest

def process_stat(raw, pid):
    text = raw.decode('ascii'); end = text.rfind(')')
    require(text.startswith(str(pid)+' (') and end > 0,'owned init stat PID differs')
    fields = text[end+2:].split(); require(len(fields) >= 20,'short process stat')
    require(fields[0] in STAT_STATES and fields[0] not in DEAD_STAT_STATES,'owned init is dead/zombie or has unknown stat state')
    value = {'pid':pid,'state':fields[0],'comm':text[len(str(pid))+2:end],'ppid':int(fields[1]),
        'pgrp':int(fields[2]),'session':int(fields[3]),'starttime_ticks':int(fields[19])}
    require(value['starttime_ticks'] > 0 and all(value[key] >= 0 for key in ('ppid','pgrp','session')),'invalid init stat identity')
    return value

def process_identity(value):
    # Every read separately proves a documented live state. Scheduling changes
    # can alter that state without rebinding the process or its owned cgroup.
    return tuple(value[key] for key in ('pid','starttime_ticks','ppid','pgrp','session','comm'))

def cgroup_path(raw, identifier):
    text = raw.decode('ascii'); require(text.startswith('0::/') and text.endswith('\n') and len(text.splitlines()) == 1,'unified init cgroup absent')
    path = PurePosixPath(text[3:-1]); require(path.is_absolute() and '..' not in path.parts and path.as_posix() == text[3:-1],'noncanonical init cgroup')
    require(path.name in (identifier,'docker-'+identifier+'.scope'),'owned Docker cgroup leaf differs')
    return Path(*path.parts[1:])

def capacity(available, parents):
    require(type(available) is int and available >= 0,'invalid physical available bytes')
    values = [available]; maxima = []
    for row in parents:
        maximum,current = row['memory_max_bytes'],row['memory_current_bytes']
        if row.get('uncontrolled_root_fields_observed_absent'):
            require(row['group'] == '.' and maximum is None and current is None,'root absence differs'); continue
        require(type(current) is int and current >= 0 and (maximum is None or type(maximum) is int and maximum > 0),'invalid parent capacity')
        if maximum is not None: maxima.append(maximum); values.append(max(0,maximum-current))
    return min(values), min(maxima) if maxima else None

def capture(container, row, output, moment, *, proc_root=Path('/proc'), cgroup_root=Path('/sys/fs/cgroup')):
    started = time.monotonic(); require(moment in ('before','after'),'unbound observation moment')
    container.owned(row); container.validate(row)
    pid = row['State']['Pid']; require(type(pid) is int and pid > 0 and row['State']['Running'] is True,'owned live init absent')
    live_raw = container.last_inspection_raw
    require(live_raw is not None and digest(live_raw) == container.inspection_receipts[-1]['stdout_sha256'] and
        decode(live_raw) == [row],'live row is not the authenticated same inspect buffer')
    directory = Directory(proc_root/str(pid),os.getuid()); rows = [retain(output/'raw',moment+'-owned-live-inspect.json',live_raw,
        'bounded owned full-CID Docker inspect',{'container_id':container.identifier,'stdout_sha256':digest(live_raw)})]
    def read_process(name, suffix):
        raw,identity = directory.read(name,65536,uid=os.getuid())
        rows.append(retain(output/'raw',moment+'-init-'+name+'-'+suffix+'.raw',raw,proc_root/str(pid)/name,identity))
        return raw
    try:
        first = process_stat(read_process('stat','first'),pid)
        group = read_process('cgroup','first'); relative = cgroup_path(group,container.identifier)
        leaf = Directory(cgroup_root/relative,0)
        try:
            limits = {}
            for field in ('memory.max','memory.swap.max','pids.max','cpu.max'):
                raw,identity = leaf.read(field)
                rows.append(retain(output/'raw',moment+'-leaf-'+field+'.raw',raw,cgroup_root/relative/field,identity))
                limits[field] = raw.decode('ascii').strip()
            require({k:limits[k] for k in ('memory.max','memory.swap.max','pids.max')} ==
                {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'},'actual owned2GiB probe quota differs')
            cpu = limits['cpu.max'].split(); require(len(cpu) == 2 and int(cpu[0]) == 2*int(cpu[1]),'actual owned CPU quota differs')
            leaf_identity = [leaf.opened.st_dev,leaf.opened.st_ino]
        finally: leaf.close()
        raw,identity = file_bytes(proc_root/'meminfo',65536,uid=0,pseudo=True)
        rows.append(retain(output/'raw',moment+'-physical-meminfo.raw',raw,proc_root/'meminfo',identity))
        physical = {}
        for line in raw.decode('ascii').splitlines():
            key,_,value = line.partition(':')
            if key in ('MemTotal','MemAvailable'):
                require(key not in physical,'duplicate physical field'); fields = value.split()
                require(len(fields) == 2 and fields[0].isdigit() and fields[1] == 'kB','physical memory unit differs')
                physical[key] = int(fields[0])*1024
        require(set(physical) == {'MemTotal','MemAvailable'} and physical['MemTotal'] > 0 and
            physical['MemTotal'] >= physical['MemAvailable'],'physical memory fields differ')
        parents = []; parent = relative.parent
        for level in range(32):
            root = Directory(cgroup_root/parent,0)
            try:
                observed = {}; absent = []
                for field in ('memory.max','memory.current'):
                    try: raw,identity = root.read(field)
                    except FileNotFoundError: absent.append(field); continue
                    rows.append(retain(output/'raw',moment+'-parent-'+str(level)+'-'+field+'.raw',raw,cgroup_root/parent/field,identity))
                    text = raw.decode('ascii').strip(); require(text == 'max' and field == 'memory.max' or text.isdigit(),'parent memory field differs')
                    observed[field] = None if text == 'max' else int(text)
                require(not absent or parent == Path('.') and set(absent) == {'memory.max','memory.current'},'unproved parent controller fields')
                value = {'group':parent.as_posix(),'identity':[root.opened.st_dev,root.opened.st_ino],
                    'memory_max_bytes':observed.get('memory.max'),'memory_current_bytes':observed.get('memory.current')}
                if absent: value['uncontrolled_root_fields_observed_absent'] = True
                parents.append(value)
            finally: root.close()
            if parent == Path('.'): break
            parent = parent.parent
        require(parents[-1]['group'] == '.','parent ancestry bound exceeded')
        final_group = read_process('cgroup','final'); last = process_stat(read_process('stat','final'),pid)
        require(process_identity(first) == process_identity(last) and final_group == group,'owned init identity/cgroup changed during observation')
        directory.check(); require(time.monotonic()-started < 10,'host observation deadline exceeded')
        estimated,maximum = capacity(physical['MemAvailable'],parents)
        return {'schema':'workflow.offline-scanner.docker-parent-memory.v1','accepted':False,
            'recorded_at':datetime.now(timezone.utc).isoformat(),'moment':moment,'container_id':container.identifier,
            'nonce':container.nonce,'image':container.spec['Image'],'host_init':first,'host_init_final':last,
            'host_init_cgroup':relative.as_posix(),'own_container_cgroup_identity':leaf_identity,
            'own_container_limits':limits,'own_container_memory_cap_excluded_from_parent_estimate':True,
            'physical_bytes':physical,'docker_parent_ancestors':parents,'docker_parent_effective_memory_max_bytes':maximum,
            'estimated_physical_and_docker_parent_headroom_bytes':estimated,
            'observed_4_5GiB_prerequisite':estimated >= MINIMUM_OBSERVED_HEADROOM,'required_observed_bytes':MINIMUM_OBSERVED_HEADROOM,
            'raw_custody':rows,'scope':'point-in-time observed physical/Docker-init-parent capacity estimate; no runner equivalence, reservation, minimum/sufficient fit, resource expansion or installation authority'}
    finally: directory.close()

def run(output, execute=False):
    started = time.monotonic(); container = None; fixture = None; code = 2
    result = {'schema':'workflow.offline-scanner.docker-parent-headroom-proof.v1','accepted':False,
        'measurement_only':True,'measurement_complete':False,'headroom_prerequisite_observed':False,
        'resource_expansion_authorized':False,'scanner_or_compiler_execution':False,'retirement_verified':False}
    try:
        require(execute,'explicit reviewed measurement execution required')
        module,source_sha,manifest = sources(); module.eligible()
        identity = {key:os.environ[key] for key in IDENTITY}
        require(identity['GITHUB_REPOSITORY'] == 'GoCodeAlone/workflow' and identity['GITHUB_EVENT_NAME'] == 'workflow_dispatch' and
            identity['GITHUB_REF'] == 'refs/heads/prep/osv-prerequisites-20261011-r1' and identity['GITHUB_JOB'] == 'osv-docker-parent-headroom' and
            re.fullmatch(r'[0-9a-f]{40}',identity['GITHUB_SHA']) and identity['GITHUB_RUN_ID'].isdecimal() and identity['GITHUB_RUN_ATTEMPT'] == '1','unbound finite measurement identity')
        parent = Path(os.environ['RUNNER_TEMP']); info = output.lstat()
        require(output.resolve() == output and output.parent == parent.resolve() == parent and output.name.startswith('osv-headroom-') and
            stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700 and not list(output.iterdir()),'fresh owned output required')
        (output/'raw').mkdir(mode=0o700); worker_output = output/'worker'; worker_output.mkdir(mode=0o700)
        fixture = parent/(output.name+'-inputs'); fixture.mkdir(mode=0o700)
        container = module.Container(worker_output,HERE,mounts=[(fixture,'/fixture',True)],seconds=25,
            measurement_only=True,deadline=started+180,ledger=output/'ownership.json')
        request = {'schema':'workflow.offline-scanner.headroom-request.v1','measurement_only':True,
            'uid':os.getuid(),'gid':os.getgid(),'identity':identity,'nonce':container.nonce,
            'host_namespaces':{n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')}}
        request_sha = write(fixture/'request.json',request)
        result.update(identity=identity,source_manifest_sha256=source_sha,image=manifest['image'],image_config=manifest['image_config'],
            request_sha256=request_sha,probe_seconds=25,kill_after_seconds=5)
        container.create(); result['container_id'] = container.identifier; result['nonce'] = container.nonce
        def observe(row):
            observations = []
            for moment in ('before','after'):
                current = container.live_identity()
                require(current['State']['Pid'] == row['State']['Pid'],'live init PID rebound before observation')
                observed = capture(container,current,output,moment)
                checked = container.live_identity()
                require(checked['State']['Pid'] == current['State']['Pid'],'live init PID rebound after observation')
                observed['live_inspection_sha256'] = digest(json.dumps(current,sort_keys=True,separators=(',',':')).encode())
                observations.append(observed); write(output/('host-docker-parent-'+moment+'.json'),observed)
            require(process_identity(observations[0]['host_init']) == process_identity(observations[1]['host_init']) and
                observations[0]['host_init_cgroup'] == observations[1]['host_init_cgroup'] and
                observations[0]['own_container_cgroup_identity'] == observations[1]['own_container_cgroup_identity'],'owned host init/cgroup identity changed')
            result['observations'] = observations
        exit_code,row = container.start_wait(live_observer=observe); result['actual_exit'] = exit_code
        raw,_ = file_bytes(worker_output/'headroom-worker.json',65536,uid=os.getuid()); worker = decode(raw)
        require(exit_code == 0 and worker['complete'] is True and worker['exit'] == 0 and worker['identity'] == identity and
            worker['nonce'] == container.nonce and worker['request_sha256'] == request_sha and worker['witness']['kernel_limits'] == module.LIMITS,'actual fixed worker boundary failed')
        result['worker'] = worker; result['worker_sha256'] = digest(raw)
        container.cleanup(); require(container.retired,'owned retirement absent')
        result['retirement_verified'] = True
        sources(source_sha)
        require(digest(file_bytes(fixture/'request.json',65536,uid=os.getuid())[0]) == request_sha,'readonly request changed')
        result['measurement_complete'] = True
        result['headroom_prerequisite_observed'] = all(value['observed_4_5GiB_prerequisite'] for value in result['observations'])
        code = 0 if result['headroom_prerequisite_observed'] else 2
    except Exception as error: result['failure_type'] = type(error).__name__
    finally:
        if container is not None and not container.retired:
            try: result['cleanup'] = container.cleanup(); result['retirement_verified'] = container.retired
            except Exception as error: result['cleanup_failure_type'] = type(error).__name__; code = 2
        result['lifecycle'] = container.lifecycle_evidence() if container is not None else {'constructed':False,'retired':False}
        if fixture is not None and container is not None and container.retired:
            try: (fixture/'request.json').unlink(); fixture.rmdir(); result['input_retired'] = True
            except Exception as error: result['input_retirement_failure_type'] = type(error).__name__; code = 2
    result['elapsed_seconds'] = round(time.monotonic()-started,3)
    if not result['retirement_verified'] or result['elapsed_seconds'] >= 180: code = 2
    result['exit'] = code; write(output/'docker-parent-headroom-proof.json',result)
    if time.monotonic()-started >= 180: return 2
    return code

if __name__ == '__main__':
    signal.signal(signal.SIGTERM,lambda *_: (_ for _ in ()).throw(Refusal('bounded measurement interrupted')))
    require(len(sys.argv) == 3 and sys.argv[2] == '--execute','fixed measurement arguments required')
    raise SystemExit(run(Path(sys.argv[1]),True))
