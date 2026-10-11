"""Manager and child witnesses inside the aggregate Docker resource group."""
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import signal
import socket
import subprocess
import sys

LIMITS = {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}

def require(condition, reason):
    if not condition:raise RuntimeError(reason)

def write(path, value):
    with path.open('x') as f:
        os.fchmod(f.fileno(),0o600)
        json.dump(value,f,sort_keys=True,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())

def abi_denials():
    # Two distinct entry paths: x32 numbering retains the x86_64 audit arch;
    # int 0x80 uses AUDIT_ARCH_I386. Null compat socketcall arguments cannot
    # create a socket even if the filter fails. EFAULT/ENOSYS are not proof.
    probes={
        'x32':'import ctypes,os;lib=ctypes.CDLL(None,use_errno=True);r=lib.syscall(0x40000000|41,2,1,0);os._exit(0 if r>=0 else (77 if ctypes.get_errno()==1 else 78))',
        'i386':
            'import ctypes,mmap,os;'
            'page=mmap.mmap(-1,4096,prot=mmap.PROT_READ|mmap.PROT_WRITE|mmap.PROT_EXEC);'
            # push rbx; eax=102(socketcall), ebx=1(socket), ecx=0; int80; pop; ret
            'page.write(bytes.fromhex("53 b8 66 00 00 00 bb 01 00 00 00 31 c9 cd 80 5b c3"));'
            'call=ctypes.CFUNCTYPE(ctypes.c_int)(ctypes.addressof(ctypes.c_char.from_buffer(page)));'
            'r=call();os._exit(77 if r==-1 else 78)'}
    exits={}
    for name,code in probes.items():
        child=subprocess.run([sys.executable,'-I','-S','-B','-c',code],
            stdin=subprocess.DEVNULL,capture_output=True,timeout=10,start_new_session=True)
        require(child.returncode in (-signal.SIGSYS,77),name+' syscall denial not established')
        exits[name]=child.returncode
    return exits

def witness(request):
    require(platform.system()=='Linux' and platform.machine()=='x86_64','wrong native platform')
    require(os.getuid()==request['uid'] and os.getgid()==request['gid'] and os.geteuid()!=0,'wrong nonroot identity')
    require(sys.flags.isolated and sys.flags.no_site and sys.dont_write_bytecode,'unsealed Python startup')
    status=dict(line.split(':',1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    require(status['NoNewPrivs'].strip()=='1' and status['Seccomp'].strip()=='2','kernel privilege/filter witness absent')
    require(all(int(status[k].strip(),16)==0 for k in ('CapInh','CapPrm','CapEff','CapBnd','CapAmb')),'worker capabilities remain')
    namespaces={n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')}
    require(all(namespaces[n]!=request['host_namespaces'][n] for n in ('net','mnt','pid','ipc','cgroup')),'host namespace inherited')
    cg=Path('/sys/fs/cgroup')
    actual={n:(cg/n).read_text().strip() for n in LIMITS}
    require(actual==LIMITS,'actual aggregate cgroup quota mismatch')
    require(str(os.getpid()) in (cg/'cgroup.procs').read_text().split(),'manager outside quota group')
    cpu=(cg/'cpu.max').read_text().split();require(len(cpu)==2 and int(cpu[0])==2*int(cpu[1]),'actual CPU quota mismatch')
    fds={}
    for p in Path('/proc/self/fd').iterdir():
        try:target=os.readlink(p)
        except FileNotFoundError:continue
        require(not target.startswith('socket:'),'inherited socket descriptor')
        if int(p.name)>2:raise RuntimeError('unexpected inherited descriptor')
        fds[p.name]=target
    require(os.read(0,1)==b'','stdin is not closed')
    for family in (socket.AF_INET,socket.AF_INET6,socket.AF_UNIX):
        try:s=socket.socket(family,socket.SOCK_STREAM)
        except OSError as e:require(e.errno==errno.EPERM,'socket failed without required EPERM')
        else:s.close();raise RuntimeError('socket syscall permitted')
    libc=ctypes.CDLL(None,use_errno=True)
    for nr,args in ((272,(0,)),(165,(0,0,0,0,0)),(101,(0,0,0,0)),(321,(0,0,0)),(425,(0,0))):
        ctypes.set_errno(0);r=libc.syscall(nr,*args)
        require(r==-1 and ctypes.get_errno()==errno.EPERM,'escape syscall was not denied')
    readonly_flags={};readonly_errors={}
    for raw in request['readonly_targets']:
        readonly_flags[raw]=os.statvfs(raw).f_flag
        require(readonly_flags[raw]&os.ST_RDONLY,'actual read-only mount flag absent')
        p=Path(raw)
        directory=p.is_dir()
        if directory:p=p/'.osv-readonly-witness'
        try:
            fd=os.open(p,os.O_WRONLY | (os.O_CREAT|os.O_EXCL if directory else 0) | os.O_NOFOLLOW,0o600)
            os.close(fd)
        except OSError as e:
            require(e.errno in (errno.EROFS,errno.EACCES),'unexpected read-only witness failure')
            require(raw!='/fixture' or e.errno==errno.EROFS,'owned fixture did not prove kernel write denial')
            readonly_errors[raw]=e.errno
        else:
            if directory:p.unlink()
            raise RuntimeError('read-only mount writable')
    for p in request.get('forbidden_paths',('/var/run/docker.sock','/run/docker.sock','/github/home','/home/runner')):
        require(not Path(p).exists(),'host socket/home was mounted')
    # Exercise an actual owned child in the same kernel group. Go version starts
    # ordinary Go runtime threads without compiling or executing project code.
    child=subprocess.run([sys.executable,'-I','-S','-B','-c',
        'import os,json;from pathlib import Path;print(json.dumps({"pid":os.getpid(),"group":Path("/proc/self/cgroup").read_text(),"memory":Path("/sys/fs/cgroup/memory.max").read_text().strip()}))'],
        stdin=subprocess.DEVNULL,capture_output=True,text=True,timeout=10,check=True,start_new_session=True)
    child=json.loads(child.stdout)
    require(child['group']==Path('/proc/self/cgroup').read_text() and child['memory']==LIMITS['memory.max'],'child escaped aggregate quota')
    abi=abi_denials()
    go=Path(request.get('compiler_path','/go/bin/go'));require(hashlib.sha256(go.read_bytes()).hexdigest()==request['compiler_sha256'],'compiler digest differs')
    version=subprocess.run([str(go),'version'],env={'PATH':str(go.parent)+':/usr/bin:/bin','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOVCS':'*:off','GOMAXPROCS':'2'},
        stdin=subprocess.DEVNULL,capture_output=True,text=True,timeout=15,check=True,start_new_session=True)
    require(version.stdout.strip()=='go version go1.27.2 linux/amd64' and not version.stderr,'actual compiler/thread witness failed')
    return {'uid':os.getuid(),'gid':os.getgid(),'manager_pid':os.getpid(),'namespaces':namespaces,
        'fresh_user_namespace_required':False,'kernel_limits':actual,'cpu_max':cpu,
        'aggregate_scope':'container manager, scanner workers and all descendants',
        'child':child,'descriptors':fds,'socket_denial':'EPERM','escape_denial':'EPERM',
        'abi_exits':abi,'compiler':version.stdout.strip(),'compiler_sha256':request['compiler_sha256'],
        'readonly_flags':readonly_flags,'readonly_errors':readonly_errors,
        'readonly_targets':request['readonly_targets'],'no_new_privileges':True,'capabilities':0}

def main():
    request=json.loads(Path('/out/request.json').read_text())
    resource.setrlimit(resource.RLIMIT_FSIZE,(16*1024*1024,16*1024*1024))
    result={'schema':'workflow.offline-scanner.container-worker.v1','complete':False,
        'scanner_execution':False,'candidate_execution':False,'identity':request['identity']}
    code=2
    try:
        result['witness']=witness(request)
        result.update(complete=True,exit=0);code=0
    except Exception as error:
        result.update(exit=2,error_type=type(error).__name__,error=str(error)[:300])
    write(Path('/out/worker-result.json'),result)
    os._exit(code)

if __name__=='__main__':main()
