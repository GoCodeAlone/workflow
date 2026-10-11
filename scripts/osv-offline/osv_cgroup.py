"""Read-only preflight/attestation for the reviewed, unapproved transient unit.

No service creation, controller activation, ownership change or OS probe occurs
here. Callers own any separately authorized submission and must compare fresh
preflight snapshots immediately before it. Configuration inspection is metadata
only; no unit/drop-in contents or manager environment are read.
"""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import selectors
import shlex
import signal
import stat
import subprocess
import time

CGROUP_ROOT = Path('/sys/fs/cgroup')
MOUNTINFO = Path('/proc/self/mountinfo')
MEMBERSHIP = Path('/proc/self/cgroup')
TOOLS = {name: Path('/usr/bin') / name for name in
         ('env', 'systemctl', 'systemd-run', 'busctl', 'python3', 'sudo', 'git', 'timeout')}
DEFAULTS = ('CPU', 'IO', 'BlockIO', 'IP', 'Memory', 'Tasks')
SHOW_FIELDS = frozenset(('Id', 'Names', 'LoadState', 'ActiveState', 'SubState',
    'Transient', 'MainPID', 'User', 'Group', 'ControlGroup', 'InvocationID',
    'Type', 'Slice', 'Delegate', 'DelegateControllers', 'DelegateSubgroup',
    'MemoryMax', 'MemorySwapMax', 'TasksMax', 'RuntimeMaxUSec', 'TimeoutStartUSec',
    'TimeoutStopUSec', 'KillMode', 'SendSIGKILL', 'Restart', 'NoNewPrivileges',
    'UMask', 'CollectMode', 'FragmentPath', 'DropInPaths', 'UnitFileState')) | frozenset(
        name+'Accounting' for name in DEFAULTS)
MAX_ENTRIES = 8192
MAX_READ = 65536
DECIMAL = re.compile(r'^[1-9][0-9]{0,19}$')
VERSION = re.compile(r'^[0-9]{8}\.[0-9]{1,8}\.[0-9]{1,8}$')
IDENTITY_FIXED = {
    'GITHUB_REPOSITORY': 'GoCodeAlone/workflow',
    'GITHUB_REF': 'refs/heads/prep/osv-prerequisites-20261011-r1',
    'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_JOB': 'osv-runtime-prerequisite',
    'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'X64',
    'RUNNER_ENVIRONMENT': 'github-hosted', 'ImageOS': 'ubuntu24',
}

class Refusal(RuntimeError):
    pass

class Budget:
    def __init__(self):
        self.deadline = time.monotonic() + 20
        self.entries = 0
    def left(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise Refusal('read-only snapshot deadline exceeded')
        return remaining
    def charge(self):
        self.left(); self.entries += 1
        if self.entries > MAX_ENTRIES:
            raise Refusal('read-only metadata entry limit exceeded')

def _identity():
    if platform.system() != 'Linux' or platform.machine() != 'x86_64':
        raise Refusal('actual Linux/x86_64 required')
    if os.getuid() <= 0 or os.getgid() <= 0 or os.geteuid() != os.getuid() or os.getegid() != os.getgid():
        raise Refusal('actual unprivileged nonzero runner UID/GID required')
    for key, expected in IDENTITY_FIXED.items():
        if os.environ.get(key) != expected:
            raise Refusal('current dispatch identity mismatch: ' + key)
    result = {key: os.environ[key] for key in IDENTITY_FIXED}
    for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'):
        value = os.environ.get(key, '')
        if not DECIMAL.fullmatch(value):
            raise Refusal('invalid current dispatch decimal: ' + key)
        result[key] = value
    if not re.fullmatch(r'[0-9a-f]{40}', os.environ.get('GITHUB_SHA', '')):
        raise Refusal('invalid current dispatch commit')
    image = os.environ.get('ImageVersion', '')
    if not VERSION.fullmatch(image):
        raise Refusal('invalid current hosted image version')
    result.update(GITHUB_SHA=os.environ['GITHUB_SHA'], ImageVersion=image)
    return result

def unit_name():
    identity = _identity()
    return 'wf-osv-proof-' + identity['GITHUB_RUN_ID'] + '-' + identity['GITHUB_RUN_ATTEMPT'] + '.service'

def service_properties():
    _identity()
    return {'Type':'exec', 'Slice':'system.slice', 'User':str(os.getuid()), 'Group':str(os.getgid()),
        'Delegate':'memory pids', 'DelegateSubgroup':'supervisor',
        'MemoryMax':'2147483648', 'MemorySwapMax':'0', 'TasksMax':'256',
        'CPUAccounting':'no', 'IOAccounting':'no', 'BlockIOAccounting':'no',
        'IPAccounting':'no', 'MemoryAccounting':'yes', 'TasksAccounting':'yes',
        'RuntimeMaxSec':'150s', 'TimeoutStartSec':'15s', 'TimeoutStopSec':'5s',
        'KillMode':'control-group', 'SendSIGKILL':'yes', 'Restart':'no',
        'NoNewPrivileges':'yes', 'UMask':'0077', 'CollectMode':'inactive-or-failed'}

def _writable(path):
    return os.access(path, os.W_OK, effective_ids=True)

def _meta(path, budget, metadata):
    budget.charge()
    try:
        info = path.lstat()
    except FileNotFoundError:
        if metadata.get(str(path),{}).get('exists'):
            raise Refusal('metadata disappeared during snapshot: ' + str(path)) from None
        metadata[str(path)] = {'exists':False}
        return None
    except OSError:
        raise Refusal('uninspectable metadata target: ' + str(path)) from None
    if not (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)):
        raise Refusal('unsupported metadata type: ' + str(path))
    observed = {'exists':True, 'mode':stat.S_IMODE(info.st_mode),
        'type':stat.S_IFMT(info.st_mode), 'uid':info.st_uid, 'gid':info.st_gid,
        'device':info.st_dev, 'inode':info.st_ino, 'size':info.st_size,
        'mtime_ns':info.st_mtime_ns, 'ctime_ns':info.st_ctime_ns}
    previous=metadata.get(str(path),{})
    if previous and any(previous.get(key)!=value for key,value in observed.items()):
        raise Refusal('metadata changed during snapshot: '+str(path))
    metadata[str(path)] = previous | observed
    return info

def _root_chain(path, budget, metadata, *, symlinks=False):
    if not path.is_absolute() or str(path) != os.path.normpath(str(path)):
        raise Refusal('noncanonical trusted metadata path: ' + str(path))
    current = Path('/')
    for part in ('',) + path.parts[1:]:
        if part: current /= part
        info = _meta(current, budget, metadata)
        if info is None:
            return False  # The nearest existing ancestor has already been checked.
        if info.st_uid != 0:
            raise Refusal('lookup path is not root owned: ' + str(current))
        if stat.S_ISLNK(info.st_mode):
            if not symlinks:
                raise Refusal('symlink in trusted lookup path: ' + str(current))
        elif info.st_mode & 0o022 or _writable(current):
            raise Refusal('runner-writable trusted lookup path: ' + str(current))
        if current != path and not (stat.S_ISDIR(info.st_mode) or (symlinks and stat.S_ISLNK(info.st_mode))):
            raise Refusal('non-directory lookup ancestor: ' + str(current))
    return True

def _read(path, budget, limit=MAX_READ):
    budget.left()
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                raise Refusal('nonregular read-only target: ' + str(path))
            raw = source.read(limit + 1)
    except OSError:
        raise Refusal('uninspectable read-only target: ' + str(path)) from None
    if len(raw) > limit:
        raise Refusal('read-only target byte limit: ' + str(path))
    budget.left()
    try: return raw.decode('ascii')
    except UnicodeError: raise Refusal('non-ASCII typed target: ' + str(path)) from None

def _canonical_tools(budget, metadata):
    result = {}
    for name, original in TOOLS.items():
        if not _root_chain(original, budget, metadata, symlinks=True):
            raise Refusal('missing canonical tool: ' + str(original))
        try: path = original.resolve(strict=True)
        except OSError: raise Refusal('unresolvable canonical tool: ' + str(original)) from None
        if not _root_chain(path, budget, metadata):
            raise Refusal('missing canonical executable: ' + str(path))
        try: info = path.lstat()
        except OSError: raise Refusal('uninspectable canonical executable: ' + str(path)) from None
        forbidden_id_bits = 0o2000 if name == 'sudo' else 0o6000
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or not info.st_mode & 0o111 or info.st_mode & forbidden_id_bits or info.st_size > 128*1024*1024:
            raise Refusal('unsupported canonical executable: ' + str(path))
        h = hashlib.sha256()
        count = 0
        try:
            with path.open('rb') as source:
                for block in iter(lambda: source.read(1024*1024), b''):
                    budget.left(); count += len(block)
                    if count > 128*1024*1024:
                        raise Refusal('canonical executable byte limit: ' + str(path))
                    h.update(block)
            after = path.lstat()
        except OSError:
            raise Refusal('uninspectable canonical executable: ' + str(path)) from None
        if (info.st_dev,info.st_ino,info.st_size,info.st_mtime_ns,info.st_ctime_ns) != (
            after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns):
            raise Refusal('canonical tool changed during hash: ' + str(path))
        result[name] = {'path':str(original), 'canonical_path':str(path), 'sha256':h.hexdigest()}
    return result

def run_readonly(argv, budget, *, allowed_codes=(0,)):
    """Only fixed read-only systemctl/busctl queries; bounded closed execution."""
    if not isinstance(argv,list) or not all(isinstance(value,str) for value in argv):
        raise Refusal('malformed read-only query')
    valid = argv == [str(TOOLS['systemctl']), '--version']
    if isinstance(argv, list) and len(argv) == 6 and argv[:3] == [str(TOOLS['systemctl']), 'show', '--all'] and argv[4] == '--' and argv[5] == unit_name():
        fields = argv[3].removeprefix('--property=').split(',')
        valid = argv[3].startswith('--property=') and len(fields) == len(set(fields)) and set(fields).issubset(SHOW_FIELDS)
    if isinstance(argv, list) and len(argv) == 7 and argv[:6] == [str(TOOLS['busctl']), '--system', 'get-property',
            'org.freedesktop.systemd1', '/org/freedesktop/systemd1', 'org.freedesktop.systemd1.Manager']:
        valid = argv[6] in {'UnitPath'} | {'Default'+name+'Accounting' for name in DEFAULTS}
    if not valid or allowed_codes not in ((0,), (0,4)):
        raise Refusal('non-read-only subprocess target rejected')
    command=[str(TOOLS['env']),'-i','PATH=/usr/bin:/bin','LANG=C','LC_ALL=C',
             'SYSTEMD_PAGER=','SYSTEMD_COLORS=0'] + list(argv)
    deadline=time.monotonic()+min(3,budget.left())
    selector=selectors.DefaultSelector(); streams=[bytearray(),bytearray()]
    process=None; reaped=False
    try:
        process=subprocess.Popen(command,cwd='/',env={},stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,close_fds=True,start_new_session=True)
        for index,pipe in enumerate((process.stdout,process.stderr)):
            os.set_blocking(pipe.fileno(),False); selector.register(pipe,selectors.EVENT_READ,index)
        while selector.get_map():
            left=min(deadline-time.monotonic(),budget.left())
            if left<=0: raise Refusal('read-only query deadline exceeded')
            for key,_ in selector.select(min(left,0.1)):
                raw=os.read(key.fileobj.fileno(),8192)
                if not raw: selector.unregister(key.fileobj); continue
                index=key.data; streams[index].extend(raw)
                if len(streams[index]) > (MAX_READ if index==0 else 4096):
                    raise Refusal('read-only query stream limit exceeded')
        left=min(deadline-time.monotonic(),budget.left())
        if left<=0: raise Refusal('read-only query completion deadline exceeded')
        code=process.wait(timeout=left); reaped=True
        if code not in allowed_codes:
            raise Refusal('read-only query failed: ' + argv[-1])
        budget.left()
        return bytes(streams[0]).decode('ascii')
    except (OSError,UnicodeError,subprocess.TimeoutExpired):
        raise Refusal('read-only query capture failed') from None
    finally:
        selector.close()
        if process is not None:
            if not reaped:
                try: os.killpg(process.pid,signal.SIGKILL)
                except ProcessLookupError: pass
                try: process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    raise Refusal('owned read-only query retirement deadline exceeded') from None
            for pipe in (process.stdout,process.stderr):
                if pipe is not None: pipe.close()

def _manager_property(name,budget):
    try:
        return shlex.split(run_readonly([str(TOOLS['busctl']),'--system','get-property',
            'org.freedesktop.systemd1','/org/freedesktop/systemd1',
            'org.freedesktop.systemd1.Manager',name],budget))
    except ValueError:
        raise Refusal('malformed typed SYSTEM manager property: '+name) from None

def _show(unit,names,budget,*,absent=False):
    raw=run_readonly([str(TOOLS['systemctl']),'show','--all',
        '--property='+','.join(names),'--',unit],budget,allowed_codes=(0,4) if absent else (0,))
    result={}
    for row in raw.splitlines():
        key,separator,value=row.partition('=')
        if not separator or key not in names or key in result:
            raise Refusal('malformed exact unit property output')
        result[key]=value
    if set(result)!=set(names): raise Refusal('missing exact unit property output')
    return result

def _bits(path,budget):
    values=_read(path,budget).split()
    if len(values)!=len(set(values)) or any(not re.fullmatch(r'[a-z][a-z0-9_]{0,31}',v) for v in values):
        raise Refusal('malformed complete controller bitset: '+str(path))
    return sorted(values)

def _ancestors(budget,metadata):
    raw=_read(MOUNTINFO,budget)
    mounts=[row.split(' - ',1) for row in raw.splitlines() if ' - ' in row]
    if not any(len(head.split())>=5 and head.split()[4]==str(CGROUP_ROOT) and tail.split() and tail.split()[0]=='cgroup2' for head,tail in mounts):
        raise Refusal('actual cgroup-v2 mount missing')
    result={}
    for path in (CGROUP_ROOT,CGROUP_ROOT/'system.slice'):
        if not _root_chain(path,budget,metadata): raise Refusal('missing controller ancestor: '+str(path))
        for name in ('cgroup.controllers','cgroup.subtree_control'):
            info=_meta(path/name,budget,metadata)
            if info is None or not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or _writable(path/name):
                raise Refusal('untrusted ancestor controller target: '+str(path/name))
        available=_bits(path/'cgroup.controllers',budget); enabled=_bits(path/'cgroup.subtree_control',budget)
        if not {'memory','pids'}.issubset(available) or not {'memory','pids'}.issubset(enabled) or not set(enabled).issubset(available):
            raise Refusal('memory+pids not already enabled at ancestor: '+str(path))
        result[str(path)]={'available':available,'enabled':enabled}
    return result

def _entries(path,budget,metadata,*,compact=False):
    entries=[]; observed={} if compact else metadata
    try:
        for entry in path.iterdir():
            budget.left()
            if len(entries)>=MAX_ENTRIES:
                raise Refusal('configuration directory entry limit: '+str(path))
            info=_meta(entry,budget,observed)
            if info is None: raise Refusal('namespace changed during metadata observation: '+str(entry))
            if compact and stat.S_ISLNK(info.st_mode):
                target=os.readlink(entry)
                if len(target)>4096: raise Refusal('unit alias metadata bound exceeded: '+str(entry))
                observed[str(entry)]['link_target']=target
            entries.append(entry)
    except OSError: raise Refusal('uninspectable configuration directory: '+str(path)) from None
    entries.sort()
    if compact:
        digest=hashlib.sha256()
        for entry in entries:
            budget.left()
            digest.update(json.dumps({'name':entry.name,**observed[str(entry)]},sort_keys=True,separators=(',',':')).encode()+b'\n')
        metadata[str(path)].update(entries_count=len(entries),entries_sha256=digest.hexdigest())
    return entries

def _config(unit,paths,budget,metadata):
    stem=unit[:-len('.service')]
    drops={'service.d',unit+'.d'} | {stem[:i+1]+'.service.d' for i,c in enumerate(stem) if c=='-'}
    collisions={unit} | {unit+suffix for suffix in ('.wants','.requires','.upholds')}
    for path in paths:
        if not _root_chain(path,budget,metadata): continue
        if not path.is_dir(): raise Refusal('non-directory actual UnitPath: '+str(path))
        for entry in _entries(path,budget,metadata,compact=True):
            info=entry.lstat()
            if entry.name in collisions: raise Refusal('exact unit configuration collision: '+str(entry))
            if stat.S_ISLNK(info.st_mode):
                try: target=os.readlink(entry)
                except OSError: raise Refusal('uninspectable unit alias: '+str(entry)) from None
                if len(target)>4096: raise Refusal('unit alias metadata bound exceeded: '+str(entry))
                if PurePosixPath(target).name==unit: raise Refusal('exact unit alias collision: '+str(entry))
        for name in sorted(drops):
            directory=path/name
            if not _root_chain(directory,budget,metadata): continue
            if not directory.is_dir(): raise Refusal('unsupported applicable drop-in directory: '+str(directory))
            for entry in _entries(directory,budget,metadata,compact=True):
                if entry.name.endswith('.conf'):
                    raise Refusal('applicable unit drop-in refused: '+str(entry))

def preflight():
    budget=Budget(); identity=_identity(); unit=unit_name(); metadata={}
    tools=_canonical_tools(budget,metadata)
    version=run_readonly([str(TOOLS['systemctl']),'--version'],budget)
    # The reviewed inherited-controller model is v255, including BlockIO.
    if not re.match(r'^systemd 255(?:\s|\()',version):
        raise Refusal('unreviewed systemd controller-demand model')
    typed=_manager_property('UnitPath',budget)
    if len(typed)<2 or typed[0]!='as' or not re.fullmatch(r'[1-9][0-9]?',typed[1]) or int(typed[1])!=len(typed)-2 or not int(typed[1])<=64:
        raise Refusal('missing typed actual SYSTEM manager UnitPath')
    paths=[]
    for value in typed[2:]:
        if not re.fullmatch(r'/[A-Za-z0-9._/@+-]+',value) or value!=os.path.normpath(value) or value in {str(p) for p in paths}:
            raise Refusal('unsupported actual SYSTEM manager UnitPath')
        paths.append(Path(value))
    defaults={}
    for name in DEFAULTS:
        key='Default'+name+'Accounting'; value=_manager_property(key,budget)
        if len(value)!=2 or value[0]!='b' or value[1] not in {'true','false'}:
            raise Refusal('unknown typed SYSTEM manager default: '+key)
        defaults[key]=value[1]=='true'
    _config(unit,paths,budget,metadata)
    ancestors=_ancestors(budget,metadata)
    names=('LoadState','ActiveState','FragmentPath','DropInPaths','Names','UnitFileState')
    absent=_show(unit,names,budget,absent=True)
    if absent['LoadState']!='not-found' or absent['ActiveState']!='inactive' or absent['FragmentPath'] or absent['DropInPaths'] or absent['UnitFileState'] not in {'','not-found'} or absent['Names'] not in {'',unit}:
        raise Refusal('exact current transient unit is not absent: '+unit)
    payload={'unit':unit,'identity':identity,'systemd_version':255,'tools':tools,
        'manager_unit_path':[str(p) for p in paths],'manager_defaults':defaults,
        'service_properties':service_properties(),'effective_controller_demand':['memory','pids'],
        'ancestors':ancestors,'metadata':metadata,'unit_absent':True}
    budget.left()
    payload['snapshot_sha256']=hashlib.sha256(json.dumps(payload,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    return _bounded_result(payload,budget,32768)

def _bounded_result(payload,budget,limit):
    if len(json.dumps(payload,sort_keys=True,separators=(',',':')).encode())>limit:
        raise Refusal('read-only result serialization byte limit exceeded')
    budget.left()
    return payload

def unit_status(unit=None):
    """Bounded status of the exact dispatch unit; never infer a missing root."""
    budget=Budget(); _identity(); expected=unit_name()
    if unit is not None and unit!=expected:
        raise Refusal('requested status differs from exact dispatch unit')
    metadata={}; _canonical_tools(budget,metadata)
    names=('Id','Names','LoadState','ActiveState','ControlGroup','MainPID',
        'InvocationID','User','Group')
    actual=_show(expected,names,budget,absent=True)
    if actual['Id']!=expected or actual['Names'] not in {'',expected}:
        raise Refusal('status unit identity differs')
    if actual['LoadState']=='not-found':
        if actual['ActiveState']!='inactive' or actual['ControlGroup'] or actual['MainPID']!='0' or actual['InvocationID'] or actual['User'] or actual['Group']:
            raise Refusal('inconsistent absent unit status')
    else:
        if actual['LoadState']!='loaded' or actual['ActiveState'] not in {'active','activating','deactivating','inactive','failed'}:
            raise Refusal('unsupported exact unit status')
        if not re.fullmatch(r'[0-9a-f]{32}',actual['InvocationID']) or actual['InvocationID']=='0'*32 or actual['User']!=str(os.getuid()) or actual['Group']!=str(os.getgid()):
            raise Refusal('status instance or runner identity differs')
        if not re.fullmatch(r'0|[1-9][0-9]{0,19}',actual['MainPID']):
            raise Refusal('invalid status MainPID')
        if actual['ControlGroup'] not in {'','/system.slice/'+expected}:
            raise Refusal('status actual ControlGroup differs')
        if actual['ActiveState']=='active' and (not actual['ControlGroup'] or actual['MainPID']=='0'):
            raise Refusal('inconsistent active unit status')
    budget.left()
    return {key:actual[name] for key,name in zip(
        ('load_state','active_state','control_group','main_pid','invocation_id','user','group'),
        ('LoadState','ActiveState','ControlGroup','MainPID','InvocationID','User','Group'))}

def attest_unit(unit=None):
    budget=Budget(); identity=_identity(); expected=unit_name()
    if unit is not None and unit!=expected:
        raise Refusal('requested unit differs from exact dispatch unit')
    if os.environ.get('WF_OSV_UNIT',expected)!=expected:
        raise Refusal('submitted unit differs from exact dispatch unit')
    metadata={}; tools=_canonical_tools(budget,metadata)
    names=('Id','Names','LoadState','ActiveState','SubState','Transient','MainPID','User','Group',
        'ControlGroup','InvocationID','Type','Slice','Delegate','DelegateControllers','DelegateSubgroup',
        'MemoryMax','MemorySwapMax','TasksMax','RuntimeMaxUSec','TimeoutStartUSec','TimeoutStopUSec',
        'KillMode','SendSIGKILL','Restart','NoNewPrivileges','UMask','CollectMode')+tuple(name+'Accounting' for name in DEFAULTS)
    actual=_show(expected,names,budget)
    fixed={'Id':expected,'Names':expected,'LoadState':'loaded','ActiveState':'active','SubState':'running',
        'Transient':'yes','MainPID':str(os.getpid()),'User':str(os.getuid()),'Group':str(os.getgid()),
        'Type':'exec','Slice':'system.slice','Delegate':'yes','DelegateSubgroup':'supervisor',
        'MemoryMax':'2147483648','MemorySwapMax':'0','TasksMax':'256','KillMode':'control-group',
        'SendSIGKILL':'yes','Restart':'no','NoNewPrivileges':'yes','UMask':'0077','CollectMode':'inactive-or-failed'}
    fixed.update({name+'Accounting':'yes' if name in {'Memory','Tasks'} else 'no' for name in DEFAULTS})
    for key,value in fixed.items():
        if actual[key]!=value: raise Refusal('actual unit property differs: '+key)
    if not re.fullmatch(r'[0-9a-f]{32}',actual['InvocationID']) or actual['InvocationID']=='0'*32:
        raise Refusal('invalid actual owned InvocationID')
    if set(actual['DelegateControllers'].split())!={'memory','pids'} or len(actual['DelegateControllers'].split())!=2:
        raise Refusal('unexpected actual delegated controller demand')
    for key,allowed in {'RuntimeMaxUSec':{'2min 30s','150s','150000000'},
        'TimeoutStartUSec':{'15s','15000000'},'TimeoutStopUSec':{'5s','5000000'}}.items():
        if actual[key] not in allowed: raise Refusal('actual finite unit lifetime differs: '+key)
    observed=PurePosixPath(actual['ControlGroup'])
    if observed.parts!=('/','system.slice',expected) or str(observed)!=actual['ControlGroup']:
        raise Refusal('actual ControlGroup does not bind this exact unit')
    ancestors=_ancestors(budget,metadata)
    root=CGROUP_ROOT/str(observed).lstrip('/'); supervisor=root/'supervisor'
    for path in (root,supervisor):
        info=_meta(path,budget,metadata)
        if info is None or not stat.S_ISDIR(info.st_mode) or info.st_uid!=os.getuid() or info.st_gid!=os.getgid() or path.resolve(strict=True)!=path:
            raise Refusal('actual delegated directory ownership/path differs: '+str(path))
    children=_entries(root,budget,metadata)
    if [p.name for p in children if p.is_dir()]!=['supervisor'] or any(p.is_symlink() for p in children):
        raise Refusal('unexpected actual unit-root child or symlink')
    children=_entries(supervisor,budget,metadata)
    if any(p.is_dir() or p.is_symlink() for p in children):
        raise Refusal('unexpected supervisor child or symlink')
    for path in (root,supervisor):
        for name in ('cgroup.procs','cgroup.subtree_control'):
            target=path/name; info=_meta(target,budget,metadata)
            if info is None or not stat.S_ISREG(info.st_mode) or info.st_uid!=os.getuid() or info.st_gid!=os.getgid():
                raise Refusal('delegated control ownership differs: '+str(target))
        if _read(path/'cgroup.type',budget).strip()!='domain':
            raise Refusal('actual delegated cgroup is not a domain: '+str(path))
    if _read(root/'cgroup.procs',budget).split(): raise Refusal('actual delegated root is populated')
    if _read(supervisor/'cgroup.procs',budget).split()!=[str(os.getpid())]:
        raise Refusal('actual manager is not alone in supervisor')
    if _read(MEMBERSHIP,budget).splitlines()!=['0::'+str(observed)+'/supervisor']:
        raise Refusal('actual process cgroup membership differs from supervisor')
    if _bits(supervisor/'cgroup.subtree_control',budget):
        raise Refusal('unexpected supervisor controller activation')
    available=_bits(root/'cgroup.controllers',budget); enabled=_bits(root/'cgroup.subtree_control',budget)
    if not {'memory','pids'}.issubset(available) or not set(enabled).issubset({'memory','pids'}):
        raise Refusal('unexpected root controller availability/activation')
    limits={}
    for name,value in {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}.items():
        path=root/name; info=_meta(path,budget,metadata)
        if info is None or not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode & 0o022 or _writable(path):
            raise Refusal('outer quota is not immutable systemd-owned: '+str(path))
        limits[name]=_read(path,budget).strip()
        if limits[name]!=value: raise Refusal('actual outer quota differs: '+str(path))
    budget.left()
    return _bounded_result({'unit':expected,'identity':identity,'root':str(root),'supervisor':str(supervisor),
        'properties':actual,'limits':limits,'root_controllers':available,'root_enabled':enabled,
        'ancestors':ancestors,'metadata':metadata,'tools':tools,'manager_pid':os.getpid(),
        'uid':os.getuid(),'gid':os.getgid()},budget,65536)
