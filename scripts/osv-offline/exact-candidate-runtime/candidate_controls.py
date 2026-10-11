"""Tiny trusted static ELF probes; source-only until reviewed hosted execution.

No compiler, downloaded code, shell, interpreter or candidate is used by these
controls. Each Linux syscall probe exits 0 only on its exact expected result.
"""
import errno
import hashlib
import os
from pathlib import Path
import struct
import time
import candidate_contract as c
import candidate_enforcement as e

BASE=0x400000;DATA=BASE+0x2000
MOV=(b'\x48\xb8',b'\x48\xbf',b'\x48\xbe',b'\x48\xba',
    b'\x49\xba',b'\x49\xb8',b'\x49\xb9')
def mov(index,value):return MOV[index]+struct.pack('<Q',value&((1<<64)-1))
def call(number,args=()):
    return mov(0,number)+b''.join(mov(n+1,v) for n,v in enumerate(args))+b'\x0f\x05'
def exit_code(code):return call(231,(code,))
def expect(number,args=(),value=-errno.EPERM):
    body=call(number,args)+b'\x48\x3d'+struct.pack('<i',value)
    success=exit_code(0);failure=exit_code(91)
    return body+b'\x75'+bytes([len(success)])+success+failure
def elf(code,data=b''):
    if len(code)>3000 or len(data)>3000:raise c.Refusal('probe emitter bound')
    header=struct.pack('<16sHHIQQQIHHHHHH',b'\x7fELF\x02\x01\x01'+b'\0'*9,
        2,62,1,BASE+0x1000,64,0,0,64,56,3,0,0,0)
    # RX code and RW non-executable data/stack, never a writable code segment.
    segments=b''.join([
        struct.pack('<IIQQQQQQ',1,5,0,BASE,BASE,0x1000+len(code),0x1000+len(code),4096),
        struct.pack('<IIQQQQQQ',1,6,0x2000,DATA,DATA,len(data),8192,4096),
        struct.pack('<IIQQQQQQ',0x6474e551,6,0,0,0,0,0,16)])
    return (header+segments).ljust(0x1000,b'\0')+code+(b'\0'*(0x1000-len(code)))+data
def self_exec(path,thread=False,at=False):
    raw=path.encode()+b'\0';argv=DATA+256
    data=raw.ljust(256,b'\0')+struct.pack('<QQ',DATA,0)
    execute=call(322,(-100,DATA,argv,0,0)) if at else call(59,(DATA,argv,0))
    if not thread:return execute+exit_code(92),data
    # Exact normal thread flags and fresh BSS stack; child requests re-exec,
    # parent waits. The broker must deny the child TID, then retire the group.
    prefix=call(56,(0x50f00,DATA+8192-16,0,0,0))+b'\x48\x85\xc0'
    parent=call(35,(DATA+512,0))+exit_code(93)
    child=execute+exit_code(94)
    code=prefix+b'\x74'+bytes([len(parent)])+parent+child
    data=data.ljust(512,b'\0')+struct.pack('<qq',0,100000000)
    return code,data
def ordinary_thread():
    prefix=call(56,(0x50f00,DATA+8192-16,0,0,0))+b'\x48\x85\xc0'
    # Child exits only itself, parent exits after a finite 10ms wait.
    parent=call(35,(DATA,0))+exit_code(0)
    child=call(60,(0,))
    fail=exit_code(91)
    # Negative clone return fails; zero jumps to child.
    return (prefix+b'\x78'+bytes([2+len(parent)+len(child)])+
        b'\x74'+bytes([len(parent)])+parent+child+fail,
        struct.pack('<qq',0,10000000))
def path_test(path,flags,expected=-errno.EACCES):
    return expect(257,(-100,DATA,flags,0o600),expected),path.encode()+b'\0'
def overflow(fd,bytes_needed):
    # Repeat bounded fixed 65536-byte writes; final iteration exceeds bound.
    count=bytes_needed//65536+1
    head=mov(5,count);body=call(1,(fd,DATA,65536))
    loop=body+b'\x49\xff\xc8'
    back=-(len(loop)+2)
    return head+loop+b'\x75'+struct.pack('b',back)+exit_code(0),b'x'*64

def definitions(folder):
    cases=[]
    def add(name,code,data=b'',outcome='exit0',control=None):
        cases.append((name,code,data,outcome,control))
    add('first_owned_exec',call(1,(1,DATA,2))+exit_code(0),b'OK')
    for name,thread,at in [('same_binary_reexec',False,False),('thread_reexec',True,False),
                            ('execveat_reexec',False,True)]:
        code,data=self_exec(str(folder/name),thread,at)
        add(name,code,data,'denied-exec')
    add('wrong_pid_notification',exit_code(0),outcome='denied-exec',control='wrong-pid')
    for name,nr,args in [
        ('fork',57,()),('vfork',58,()),('clone_process',56,(17,0)),
        ('executable_mmap',9,(0,4096,4,0x22,-1,0)),('executable_mprotect',10,(DATA,4096,4)),
        ('executable_pkey_mprotect',329,(DATA,4096,4,0)),
        ('mremap',25,()),('remap_file_pages',216,()),('shmat',30,()),
        ('userfaultfd',323,()),('process_vm_writev',311,()),('pidfd_getfd',438,()),
        ('new_seccomp_filter',317,(1,8,0)),('prctl_seccomp',157,(22,2,0)),
        ('personality',135,()),('legacy_uselib',134,())]:
        add(name,expect(nr,args))
    code,data=ordinary_thread();add('clone_thread',code,data)
    code,data=path_test('/wf/candidate_contract.py',0)
    add('landlock_other_read',code,data)
    for directory in ('work','home','tmp','no-external-plugins'):
        code,data=path_test('/operation/'+directory+'/new',65)
        add('landlock_empty_'+directory,code,data)
    add('landlock_exec',exit_code(0),outcome='landlock-exec',control='landlock-exec')
    add('listener_death',exit_code(0),outcome='listener-death',control='listener-death')
    add('notification_deadline',exit_code(0),outcome='deadline',control='preexec-delay')
    add('canceled_notification_deadline',exit_code(0),outcome='receive-deadline',control='canceled-notification')
    add('x32',call(0x40000000|39)+exit_code(91),outcome='SIGSYS')
    add('i386',b'\xb8\x14\x00\x00\x00\xcd\x80'+exit_code(91),outcome='SIGSYS')
    # Data memory is 8192 bytes; repeat valid fixed pages rather than reading
    # beyond the segment. Every successful write contributes exactly 4096.
    for fd,name,bound in [(1,'stdout_overflow',4194304),(2,'stderr_overflow',1048576)]:
        count=bound//4096+1;body=call(1,(fd,DATA,4096))+b'\x49\xff\xc8'
        code=mov(5,count)+body+b'\x75'+struct.pack('b',-(len(body)+2))+exit_code(0)
        add(name,code,b'X'*64,'overflow')
    return cases

def evaluate_result(name,outcome,result,error):
    events=result.get('events',[]);native=result.get('native_exit')
    if outcome=='exit0':
        ok=error is None and native==0 and result.get('complete_process_enforcement') is True
        if name=='first_owned_exec':ok=ok and result.get('stdout')==b'OK'
        return ok
    if outcome=='denied-exec':
        return error is not None and any(v.get('decision')=='EPERM' for v in events) and native is not None
    if outcome=='landlock-exec':
        return error is None and native==125 and result.get('stderr')==b'preexec errno=13\n'
    if outcome=='listener-death':return error is None and native==125 and not events
    if outcome=='deadline':return error is not None and 'deadline' in error and native is not None
    if outcome=='receive-deadline':return error is not None and 'notification receive deadline' in error and native is not None
    if outcome=='SIGSYS':return error is None and native==-31
    if outcome=='overflow':return error is not None and 'stream bound' in error and native is not None
    return False

def run_case(path,operations,control,seconds):
    # A fresh trusted manager isolates each listener/filter without relying on
    # stacking multiple notification listeners in the same process.
    r,w=os.pipe2(os.O_CLOEXEC);manager=os.fork()
    if manager==0:
        os.close(r);error=None
        try:result=e.broker(str(path),operations,seconds=seconds,control=control)
        except BaseException as fault:
            error=str(fault);result=getattr(fault,'partial',{})
        for key in ('stdout','stderr'):
            raw=result.pop(key,b'')
            result[key+'_bytes']=len(raw);result[key+'_sha256']=hashlib.sha256(raw).hexdigest()
            # Preserve tiny control stdout/stderr for exact oracle only; never
            # put overflow streams in the small receipt.
            result[key+'_hex']=raw.hex() if len(raw)<=256 else None
        raw=(__import__('json').dumps({'result':result,'error':error},sort_keys=True)+'\n').encode()
        if len(raw)<=32768:os.write(w,raw)
        os.close(w);os._exit(0)
    os.close(w);os.set_blocking(r,False);raw=bytearray();deadline=time.monotonic()+seconds+1
    try:
        while True:
            if time.monotonic()>=deadline:raise c.Refusal('control manager deadline')
            import select
            if not select.select([r],[],[],min(0.05,deadline-time.monotonic()))[0]:continue
            block=os.read(r,4096)
            if not block:break
            raw.extend(block)
            if len(raw)>32768:raise c.Refusal('control manager receipt bound')
        _,code=os.waitpid(manager,0);manager=None
        if code!=0:raise c.Refusal('control manager native exit')
        value=c.decode(bytes(raw));result=value['result']
        for key in ('stdout','stderr'):
            result[key]=bytes.fromhex(result[key+'_hex']) if result[key+'_hex'] is not None else b''
        return result,value['error']
    finally:
        os.close(r)
        if manager is not None:
            try:os.kill(manager,9)
            except ProcessLookupError:pass
            os.waitpid(manager,0)

def controls(output,operations,budget):
    folder=Path(output)/'probes';folder.mkdir(mode=0o700)
    # Sole allowed object for the deliberate disallowed initial-exec control.
    denied=folder/'not-the-executed-probe'
    denied.write_bytes(elf(exit_code(0)));denied.chmod(0o500)
    rows=[]
    for name,code,data,outcome,control in definitions(folder):
        budget.left();path=folder/name;raw=elf(code,data)
        with path.open('xb') as file:
            os.fchmod(file.fileno(),0o500);file.write(raw);file.flush();os.fsync(file.fileno())
        result,error=run_case(path,operations,control,min(0.1 if outcome in ('deadline','receive-deadline') else 1,budget.left()))
        ok=evaluate_result(name,outcome,result,error)
        result.pop('stdout',None);result.pop('stderr',None)
        row={'name':name,'pass':ok,'probe_sha256':hashlib.sha256(raw).hexdigest(),
            'probe_bytes':len(raw),'expected':outcome,'actual':result,'refusal':error}
        rows.append(row)
        c.seal(folder.parent/('native-control-'+name+'.json'),row,budget)
        if not ok:raise c.Refusal('native control failed: '+name)
    budget.left()
    return {'schema':'workflow.GO5932.native-controls.v1','complete':True,'accepted':False,
        'candidate_execution':False,'rows':rows,'all_native_pass':True}
