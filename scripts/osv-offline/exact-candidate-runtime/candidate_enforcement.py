"""Target-only Linux enforcement draft. No syscall occurs on import.

The initial exec CONTINUE is only for our trusted single-threaded fork child.
No target pointer/argv is read or approved via CONTINUE after that first exec.
"""
import ctypes as ct
import errno
import fcntl
import os
from pathlib import Path
import platform
import select
import signal
import struct
import time
import candidate_contract as c

AUDIT_X86_64=0xc000003e
X32=0x40000000
ALLOW=0x7fff0000
ERRNO=0x00050000
KILL=0x80000000
NOTIFY=0x7fc00000
LD=0x20; JEQ=0x15; JSET=0x45; RET=0x06
EXEC=(59,322)
PROT_EXEC=4
CLONE_THREAD=0x10000
class Filter(ct.Structure):
    _fields_=[('code',ct.c_ushort),('jt',ct.c_ubyte),('jf',ct.c_ubyte),('k',ct.c_uint32)]
class Program(ct.Structure):
    _fields_=[('length',ct.c_ushort),('filters',ct.POINTER(Filter))]
class Sizes(ct.Structure):
    _fields_=[('notif',ct.c_ushort),('response',ct.c_ushort),('data',ct.c_ushort)]
class Data(ct.Structure):
    _fields_=[('nr',ct.c_int),('arch',ct.c_uint32),('ip',ct.c_uint64),('args',ct.c_uint64*6)]
class Notification(ct.Structure):
    _fields_=[('id',ct.c_uint64),('pid',ct.c_uint32),('flags',ct.c_uint32),('data',Data)]
class Response(ct.Structure):
    _fields_=[('id',ct.c_uint64),('value',ct.c_int64),('error',ct.c_int32),('flags',ct.c_uint32)]
class LandlockPath(ct.Structure):
    _pack_=1
    _fields_=[('allowed',ct.c_uint64),('fd',ct.c_int32)]

def ins(code,k,jt=0,jf=0):return (code,jt,jf,k)
def prefix():
    return [ins(LD,4),ins(JEQ,AUDIT_X86_64,1,0),ins(RET,KILL),
        ins(LD,0),ins(JSET,X32,0,1),ins(RET,KILL)]
def broker_filter():
    rows=prefix()
    for nr in EXEC:rows += [ins(JEQ,nr,0,1),ins(RET,NOTIFY)]
    return rows+[ins(RET,ALLOW)]
def target_filter():
    rows=prefix()
    # Causal target-only restrictions, in addition to the unchanged base filter.
    # 311/438 prevent tampering with the broker or taking its listener FD.
    for nr in (57,58,25,216,30,323,311,438,317,134,135):
        rows += [ins(JEQ,nr,0,1),ins(RET,ERRNO|errno.EPERM)]
    # clone: only a thread in this process, never another process.
    rows += [ins(JEQ,56,0,4),ins(LD,16),ins(JSET,CLONE_THREAD,1,0),
        ins(RET,ERRNO|errno.EPERM),ins(RET,ALLOW)]
    # mmap/mprotect/pkey_mprotect argument 2 is protection.
    for nr in (9,10,329):
        rows += [ins(JEQ,nr,0,4),ins(LD,32),ins(JSET,PROT_EXEC,0,1),
            ins(RET,ERRNO|errno.EPERM),ins(RET,ALLOW)]
    # Disallow another notification filter via either API.
    rows += [ins(JEQ,157,0,4),ins(LD,16),ins(JEQ,22,0,1),
        ins(RET,ERRNO|errno.EPERM),ins(RET,ALLOW)]
    return rows+[ins(RET,ALLOW)]
def evaluate(rows,nr,args=(),arch=AUDIT_X86_64):
    """Offline evaluator of this deliberately small classic-BPF subset."""
    data=struct.pack('<iIQQQQQQQ',nr,arch,0,*(tuple(args)+(0,)*6)[:6])
    pc=0;a=0
    while pc<len(rows):
        op,jt,jf,k=rows[pc]
        if op==LD:a=struct.unpack_from('<I',data,k)[0]
        elif op==JEQ:pc+=jt if a==k else jf
        elif op==JSET:pc+=jt if a&k else jf
        elif op==RET:return k
        else:raise c.Refusal('unexpected BPF opcode')
        pc+=1
    raise c.Refusal('BPF program fell through')

def libc():
    obj=ct.CDLL(None,use_errno=True);obj.syscall.restype=ct.c_long
    obj.ioctl.restype=ct.c_int
    return obj
def checked(value,what):
    if value<0:raise c.Refusal(what+' refused, errno='+str(ct.get_errno()))
    return value
def install(rows,listener=False):
    array=(Filter*len(rows))(*(Filter(*r) for r in rows))
    program=Program(len(rows),array)
    return checked(libc().syscall(ct.c_long(317),ct.c_int(1),ct.c_uint(8 if listener else 0),
        ct.byref(program)),'seccomp filter')
def abi():
    if platform.system()!='Linux' or platform.machine()!='x86_64':
        raise c.Refusal('native Linux x86_64 required')
    sizes=Sizes();api=libc()
    checked(api.syscall(ct.c_long(317),ct.c_int(3),ct.c_uint(0),ct.byref(sizes)),'notification sizes')
    action=ct.c_uint32(NOTIFY)
    checked(api.syscall(ct.c_long(317),ct.c_int(2),ct.c_uint(0),ct.byref(action)),'notification action')
    version=checked(api.syscall(ct.c_long(444),ct.c_void_p(),ct.c_size_t(0),ct.c_uint(1)),'Landlock ABI')
    if (sizes.notif,sizes.response,sizes.data)!=(80,24,64) or version<3:
        raise c.Refusal('required notification sizes or Landlock ABI>=3 unavailable')
    if (ct.sizeof(Filter),ct.sizeof(Program),ct.sizeof(LandlockPath))!=(8,16,12):
        raise c.Refusal('unsupported native ABI layouts')
    return {'notification_bytes':80,'response_bytes':24,'data_bytes':64,
        'landlock_abi':version,'architecture':'x86_64'}

def restrict_paths(candidate,operations):
    api=libc();mask=ct.c_uint64((1<<15)-1)
    ruleset=checked(api.syscall(ct.c_long(444),ct.byref(mask),ct.c_size_t(8),ct.c_uint(0)),'Landlock ruleset')
    try:
        for path,access in [(Path(candidate),1|4),*[(Path(p),4|8) for p in operations]]:
            fd=os.open(path,os.O_PATH|os.O_NOFOLLOW|os.O_CLOEXEC)
            try:
                rule=LandlockPath(access,fd)
                checked(api.syscall(ct.c_long(445),ct.c_int(ruleset),ct.c_int(1),
                    ct.byref(rule),ct.c_uint(0)),'Landlock path rule')
            finally:os.close(fd)
        checked(api.syscall(ct.c_long(446),ct.c_int(ruleset),ct.c_uint(0)),'Landlock restriction')
    finally:os.close(ruleset)

class FirstExec:
    def __init__(self,pid):
        self.pid=pid;self.consumed=False;self.events=[];self.refused=False
    def decide(self,event):
        if len(self.events)>=64:raise c.Refusal('notification evidence bound exceeded')
        allow=(not self.consumed and not self.refused and event['pid']==self.pid and event['nr']==59 and
            event['arch']==AUDIT_X86_64 and event['flags']==0)
        # Consume BEFORE sending CONTINUE. No retry can grant another exec.
        if allow:self.consumed=True
        else:self.refused=True
        self.events.append({**event,'decision':'initial-trusted-continue' if allow else 'EPERM'})
        return allow

def ioctl(fd,number,obj,what):
    checked(libc().ioctl(ct.c_int(fd),ct.c_ulong(number),ct.byref(obj)),what)
def receive(fd,seconds):
    # A canceled notification can leave RECV blocked even after poll. The real
    # timer interrupts the ioctl; do not rely on nonblocking listener semantics.
    if seconds<=0:raise c.Refusal('notification deadline')
    def expired(signum,frame):raise c.Refusal('notification receive deadline')
    old=signal.signal(signal.SIGALRM,expired)
    signal.siginterrupt(signal.SIGALRM,True)
    signal.setitimer(signal.ITIMER_REAL,seconds)
    try:
        n=Notification();ioctl(fd,0xc0502100,n,'notification receive')
        if n.flags or n.data.arch!=AUDIT_X86_64 or n.data.nr not in EXEC:
            raise c.Refusal('unexpected exec notification')
        return n
    finally:
        signal.setitimer(signal.ITIMER_REAL,0);signal.signal(signal.SIGALRM,old)
def respond(fd,n,allow):
    ident=ct.c_uint64(n.id);ioctl(fd,0x40082102,ident,'notification ID validation')
    response=Response(n.id,0,0 if allow else -errno.EPERM,1 if allow else 0)
    ioctl(fd,0xc0182101,response,'notification response')

def broker(candidate,operations,*,seconds=30,control=None):
    """Real only on explicit worker entry; never used by offline controls."""
    if not 0<seconds<=30:raise c.Refusal('bounded target/control duration required')
    deadline=time.monotonic()+seconds
    if list(Path('/proc/self/task').iterdir())!=[Path('/proc/self/task')/str(os.getpid())]:
        raise c.Refusal('trusted broker must be single-threaded before fork')
    witness=abi()
    # A READ_IMPLIES_EXEC personality could turn an allowed readable mapping
    # into executable code. Keep the native inherited personality and forbid
    # changes in the target; the ELF contract also excludes an executable stack.
    if Path('/proc/self/personality').read_text().strip()!='00000000':
        raise c.Refusal('native zero personality required')
    listener=install(broker_filter(),True)
    os.set_inheritable(listener,False)
    read0,write0=os.pipe2(os.O_CLOEXEC);read1,write1=os.pipe2(os.O_CLOEXEC)
    readw,writew=os.pipe2(os.O_CLOEXEC)
    child=None;selector=None;reaped=False;failure=None
    buffers=[bytearray(),bytearray()]
    try:
        child=os.fork()
        if child==0:
            try:
                os.setsid();os.close(listener)
                fd=os.open('/dev/null',os.O_RDONLY|os.O_CLOEXEC)
                os.dup2(fd,0);os.dup2(write0,1);os.dup2(write1,2)
                # Linux fixed preexec code only; directory was empty/owned.
                os.chdir('/operation/work')
                import resource
                resource.setrlimit(resource.RLIMIT_CORE,(0,0))
                resource.setrlimit(resource.RLIMIT_FSIZE,(16777216,16777216))
                resource.setrlimit(resource.RLIMIT_NOFILE,(64,64))
                child_witness={'pid':os.getpid(),'uid':os.getuid(),'gid':os.getgid(),
                    'group':Path('/proc/self/cgroup').read_text(),
                    'memory':Path('/sys/fs/cgroup/memory.max').read_text().strip(),
                    'pids':Path('/sys/fs/cgroup/pids.max').read_text().strip(),
                    'no_new_privileges':next(line.split(':',1)[1].strip() for line in
                        Path('/proc/self/status').read_text().splitlines() if line.startswith('NoNewPrivs:'))}
                import json
                raw=json.dumps(child_witness,sort_keys=True).encode()
                if len(raw)>4096:raise c.Refusal('preexec witness bound')
                os.write(writew,raw);os.close(writew)
                os.closerange(3,1048576)
                if control=='preexec-delay':time.sleep(0.25)
                restrict_paths('/out/probes/not-the-executed-probe' if control=='landlock-exec' else candidate,operations)
                install(target_filter())
                os.execve(candidate,[candidate] if control else [candidate,'schema'],dict(c.TARGET_ENV))
            except OSError as error:
                os.write(2,('preexec errno='+str(error.errno)+'\n').encode());os._exit(125)
            except BaseException:os._exit(125)
        os.close(write0);os.close(write1);os.close(writew);write0=write1=writew=None
        decision=FirstExec(child+1 if control=='wrong-pid' else child)
        if control=='listener-death':
            os.close(listener);listener=None
            _,code=os.waitpid(child,0);reaped=True
            return {'abi':witness,'initial_pid':child,'events':[],
                'native_exit':os.waitstatus_to_exitcode(code),'stdout':b'',
                'stderr':os.read(read1,1048576),'complete_process_enforcement':False}
        for fd in (read0,read1,readw):os.set_blocking(fd,False)
        import selectors
        selector=selectors.DefaultSelector()
        selector.register(listener,selectors.EVENT_READ,'notification')
        selector.register(read0,selectors.EVENT_READ,0);selector.register(read1,selectors.EVENT_READ,1)
        selector.register(readw,selectors.EVENT_READ,'witness');witness_bytes=bytearray()
        status=None
        while status is None or any(k.data!='notification' for k in selector.get_map().values()):
            left=deadline-time.monotonic()
            if left<=0:raise c.Refusal('target operation deadline')
            for key,_ in selector.select(min(left,0.1)):
                if key.data=='notification':
                    if control=='canceled-notification':
                        os.kill(child,signal.SIGKILL)
                        # Reap only after the broker exception captures native exit.
                        while os.waitid(os.P_PID,child,os.WEXITED|os.WNOHANG|os.WNOWAIT) is None:
                            if time.monotonic()>=deadline:raise c.Refusal('notification cancellation deadline')
                            time.sleep(0.001)
                    n=receive(listener,min(deadline-time.monotonic(),1))
                    event={'id':n.id,'pid':n.pid,'nr':n.data.nr,'arch':n.data.arch,'flags':n.flags}
                    respond(listener,n,decision.decide(event))
                    if decision.refused:raise c.Refusal('additional or unowned exec refused')
                else:
                    block=os.read(key.fd,65536)
                    if not block:selector.unregister(key.fd);continue
                    if key.data=='witness':
                        if len(witness_bytes)+len(block)>4096:raise c.Refusal('preexec witness overflow')
                        witness_bytes.extend(block);continue
                    bound=(4194304,1048576)[key.data]
                    if len(buffers[key.data])+len(block)>bound:raise c.Refusal('target stream bound exceeded')
                    buffers[key.data].extend(block)
            if status is None:
                observed=os.waitid(os.P_PID,child,os.WEXITED|os.WNOHANG|os.WNOWAIT)
                if observed is not None:status=observed
        if not decision.consumed or decision.refused:raise c.Refusal('exact initial exec unproved')
        # Keep the leader unreaped until group retirement; PID cannot be reused.
        try:os.killpg(child,signal.SIGKILL)
        except ProcessLookupError:pass
        _,code=os.waitpid(child,0);reaped=True
        native=os.waitstatus_to_exitcode(code)
        child_witness=c.decode(bytes(witness_bytes))
        if (child_witness['pid']!=child or child_witness['uid']!=os.getuid() or
            child_witness['gid']!=os.getgid() or child_witness['group']!=Path('/proc/self/cgroup').read_text() or
            child_witness['memory']!='2147483648' or child_witness['pids']!='256' or
            child_witness['no_new_privileges']!='1'):
            raise c.Refusal('actual owned child resource/privilege witness differs')
        if time.monotonic()>=deadline:raise c.Refusal('target final retirement/witness deadline')
        return {'abi':witness,'initial_pid':child,'events':decision.events,
            'child_witness':child_witness,
            'native_exit':native,'stdout':bytes(buffers[0]),'stderr':bytes(buffers[1]),
            'elapsed_seconds':seconds-(deadline-time.monotonic()),
            'complete_process_enforcement':True}
    except BaseException as error:
        failure=error
        # Evidence does not bless a failed native operation. Partial streams
        # survive via a private exception object for the worker's refusal record.
        error.partial={'stdout':bytes(buffers[0]),'stderr':bytes(buffers[1]),
            'initial_pid':child,'events':decision.events if 'decision' in locals() else [],
            'complete_process_enforcement':False}
        raise
    finally:
        if child is not None and child!=0 and not reaped:
            try:os.killpg(child,signal.SIGKILL)
            except ProcessLookupError:
                try:os.kill(child,signal.SIGKILL)
                except ProcessLookupError:pass
            # Outer exact-ID container retirement bounds a pathological wait.
            _,code=os.waitpid(child,0)
            if failure is not None and hasattr(failure,'partial'):
                failure.partial['native_exit']=os.waitstatus_to_exitcode(code)
        if selector is not None:selector.close()
        for fd in (listener,read0,read1,write0,write1,readw,writew):
            if fd is not None:
                try:os.close(fd)
                except OSError:pass
