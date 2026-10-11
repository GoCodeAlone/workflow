"""Fixed controls-first operation worker. Import never executes native controls."""
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import socket
import sys
HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import candidate_contract as c
import candidate_enforcement as e
import candidate_controls as probes

OPERATIONS=tuple('/operation/'+name for name in ('work','home','tmp','no-external-plugins'))
def verify_bundle(budget,expected):
    manifest=c.read_receipt(HERE/'source-manifest.json',expected,budget)
    for name,row in manifest['files'].items():
        if '/' in name or name in ('.','..'):raise c.Refusal('unbounded helper path')
        raw=c.read_regular(HERE/name,row['bytes'],budget)
        if len(raw)!=row['bytes'] or hashlib.sha256(raw).hexdigest()!=row['sha256']:
            raise c.Refusal('source bundle differs')
    return manifest
def witness(request):
    if platform.system()!='Linux' or platform.machine()!='x86_64':
        raise c.Refusal('Linux x86_64 kernel required')
    if (os.getuid()!=request['uid'] or os.getgid()!=request['gid'] or os.geteuid()==0 or
        not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode):
        raise c.Refusal('nonroot isolated startup differs')
    status=dict(line.split(':',1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    if (status['NoNewPrivs'].strip()!='1' or status['Seccomp'].strip()!='2' or
        any(int(status[key].strip(),16) for key in ('CapInh','CapPrm','CapEff','CapBnd','CapAmb'))):
        raise c.Refusal('actual kernel privilege boundary differs')
    namespaces={n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')}
    if any(namespaces[n]==request['host_namespaces'][n] for n in ('net','mnt','pid','ipc','cgroup')):
        raise c.Refusal('private namespaces unproved')
    cg=Path('/sys/fs/cgroup')
    limits={name:(cg/name).read_text().strip() for name in ('memory.max','memory.swap.max','pids.max')}
    if limits!={'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}:
        raise c.Refusal('actual aggregate limits differ')
    cpu=(cg/'cpu.max').read_text().split()
    if len(cpu)!=2 or int(cpu[0])!=2*int(cpu[1]) or str(os.getpid()) not in (cg/'cgroup.procs').read_text().split():
        raise c.Refusal('actual manager/CPU group differs')
    fds={}
    for path in Path('/proc/self/fd').iterdir():
        try:target=os.readlink(path)
        except FileNotFoundError:continue
        if int(path.name)>2 or target.startswith('socket:'):raise c.Refusal('uncontrolled inherited FD')
        fds[path.name]=target
    if os.read(0,1)!=b'':raise c.Refusal('stdin must be closed')
    for family in (socket.AF_INET,socket.AF_INET6,socket.AF_UNIX):
        try:opened=socket.socket(family,socket.SOCK_STREAM)
        except OSError as fault:
            if fault.errno!=errno.EPERM:raise c.Refusal('socket EPERM unproved')
        else:opened.close();raise c.Refusal('socket permitted')
    api=e.libc();denials={}
    for nr,args in ((272,(0,)),(165,(0,0,0,0,0)),(101,(0,0,0,0)),(321,(0,0,0)),(425,(0,0))):
        ctypes.set_errno(0);result=api.syscall(nr,*args)
        if result!=-1 or ctypes.get_errno()!=errno.EPERM:raise c.Refusal('base escape denial unproved')
        denials[str(nr)]=errno.EPERM
    readonly={}
    for raw in request['readonly_targets']:
        flags=os.statvfs(raw).f_flag
        if not flags&os.ST_RDONLY:raise c.Refusal('actual read-only mount absent')
        path=Path(raw);probe=path/'.readonly-witness' if path.is_dir() else path
        try:
            fd=os.open(probe,os.O_WRONLY|(os.O_CREAT|os.O_EXCL if path.is_dir() else 0)|os.O_NOFOLLOW,0o600)
            os.close(fd)
        except OSError as fault:
            if fault.errno not in (errno.EROFS,errno.EACCES):raise c.Refusal('write refusal differs')
            readonly[raw]={'flags':flags,'write_errno':fault.errno}
        else:
            if path.is_dir():probe.unlink()
            raise c.Refusal('read-only input writable')
    if request['mode']=='candidate' and os.statvfs('/candidate/wfctl').f_flag&os.ST_NOEXEC:
        raise c.Refusal('candidate mount is not executable')
    for raw in ('/var/run/docker.sock','/run/docker.sock','/github/home','/home/runner'):
        if Path(raw).exists():raise c.Refusal('unexpected host socket/home')
    for path in OPERATIONS:c.owned(path,True)
    return {'uid':os.getuid(),'gid':os.getgid(),'manager_pid':os.getpid(),'namespaces':namespaces,
        'kernel_limits':limits,'cpu_max':cpu,'descriptors':fds,'readonly':readonly,
        'escape_denials':denials,'socket_denial':'EPERM','capabilities':0,'no_new_privileges':True}
def raw_write(path,raw,budget,limit):
    if len(raw)>limit:raise c.Refusal('raw bound exceeded')
    with Path(path).open('xb') as file:
        os.fchmod(file.fileno(),0o400);file.write(raw);file.flush();os.fsync(file.fileno())
    budget.left()
    return {'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}

def execute(request,budget,result=None):
    if request.get('mode') not in ('controls-only','candidate'):
        raise c.Refusal('fixed worker mode required')
    manifest=verify_bundle(budget,request['source_manifest_sha256'])
    if result is None:result={}
    result.update({'schema':'workflow.GO5932.exact-runtime-worker.v1','complete':False,'accepted':False,
        'candidate_execution':False,'candidate_process_exit_observed':False,'identity':request['identity'],'invocation_id':request['invocation_id'],
        'source_manifest_sha256':request['source_manifest_sha256'],'mode':request['mode']})
    result['witness']=witness(request)
    # Real controls run before any candidate launch in this same container.
    controls=probes.controls('/out',OPERATIONS,c.Budget(min(15,budget.left())))
    controls.update(identity=request['identity'],invocation_id=request['invocation_id'],
        source_manifest_sha256=request['source_manifest_sha256'])
    c.seal('/out/native-controls.json',controls,budget)
    result['native_controls_sha256']=hashlib.sha256(Path('/out/native-controls.json').read_bytes()).hexdigest()
    if request['mode']=='candidate':
        subject=c.schema_subject(manifest)
        before=c.hash_artifact('/candidate/wfctl',budget,elf=True)
        if before['sha256']!=request['artifact_sha256']:raise c.Refusal('candidate bytes differ')
        result.update(candidate_execution_attempted=True,artifact=before,argv=['/candidate/wfctl','schema'],
            environment=dict(c.TARGET_ENV),cwd='/operation/work',target_seconds=30)
        try:native=e.broker('/candidate/wfctl',OPERATIONS,seconds=min(30,budget.left()-5))
        except BaseException as fault:
            partial=getattr(fault,'partial',{})
            for stream,limit in (('stdout',4194304),('stderr',1048576)):
                result[stream]=raw_write('/out/candidate.'+stream,partial.pop(stream,b''),budget,limit)
            result['execution']=partial;fault.worker_result=result
            raise
        result['execution']={k:v for k,v in native.items() if k not in ('stdout','stderr')}
        result['candidate_process_exit_observed']=True
        result['stdout']=raw_write('/out/candidate.stdout',native['stdout'],budget,4194304)
        result['stderr']=raw_write('/out/candidate.stderr',native['stderr'],budget,1048576)
        result['schema_diagnostic']=c.schema_diagnostic(
            native['native_exit'],native['stdout'],native['stderr'],subject)
        result['candidate_execution']=True
        if c.hash_artifact('/candidate/wfctl',budget,elf=True)!=before:
            raise c.Refusal('read-only candidate metadata changed')
        for path in OPERATIONS:c.owned(path,True)
    budget.left();result['complete']=True
    return result
def main():
    if len(sys.argv)!=2 or sys.argv[1]!='/out/request.json':raise c.Refusal('fixed request path required')
    resource.setrlimit(resource.RLIMIT_FSIZE,(16777216,16777216))
    budget=c.Budget(50);request=c.decode(c.read_regular('/out/request.json',65536,budget))
    result={}
    try:
        result=execute(request,budget,result)
        c.seal('/out/worker-result.json',result,budget)
    except BaseException as error:
        result=getattr(error,'worker_result',result or {'schema':'workflow.GO5932.exact-runtime-worker.v1',
            'identity':request.get('identity'),'invocation_id':request.get('invocation_id'),
            'candidate_execution':False})
        result.update(complete=False,accepted=False,error_type=type(error).__name__,error=str(error)[:240])
        try:c.seal('/out/worker-failure.json',result,c.Budget(2))
        except BaseException:pass
        os._exit(2)
    budget.left();os._exit(0)
if __name__=='__main__':main()
