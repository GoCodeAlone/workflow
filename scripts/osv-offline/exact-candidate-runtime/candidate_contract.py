"""Exact existing-artifact contract; no transport or target execution methods."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import struct
import time

SHA = '9819a5b582e1f0b6ab135b75c5113baf36f7507a6904e64e2984c318447e8bdf'
SIZE = 115900576
SOURCE = '81b9b1df2386f7d44c8464c999a4ea0919813526'
IDENTITY = ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')
TARGET_ENV = {'CI':'1','HOME':'/operation/home','LANG':'C','LC_ALL':'C',
    'PATH':'/nonexistent','TMPDIR':'/operation/tmp','WFCTL_NO_UPDATE_CHECK':'1',
    'WFCTL_PLUGIN_DIR':'/operation/no-external-plugins'}
CONTROLS = ('first_owned_exec','same_binary_reexec','thread_reexec',
    'wrong_pid_notification','fork_vfork','clone_process','clone_thread',
    'executable_mmap_mprotect','executable_remap_shmat','userfaultfd',
    'broker_memory_fd_protection','notification_filter_interposition',
    'landlock_other_code_read_exec','landlock_empty_operations',
    'listener_or_child_death','notification_deadline','overflow_and_cleanup')
class Refusal(RuntimeError):pass
class Budget:
    def __init__(self,seconds):self.deadline=time.monotonic()+seconds
    def left(self):
        left=self.deadline-time.monotonic()
        if left<=0:raise Refusal('shared proof deadline exceeded')
        return left

def strict(pairs):
    result={}
    for key,value in pairs:
        if key in result:raise Refusal('duplicate receipt or schema key')
        result[key]=value
    return result
def decode(raw):
    return json.loads(raw,object_pairs_hook=strict,
        parse_constant=lambda x:(_ for _ in ()).throw(Refusal('nonfinite JSON')))
def identity():
    result={key:os.environ[key] for key in IDENTITY}
    if (result['GITHUB_REPOSITORY']!='GoCodeAlone/workflow' or
        result['GITHUB_JOB']!='exact-candidate-runtime' or
        not re.fullmatch('[0-9a-f]{40}',result['GITHUB_SHA']) or
        any(not re.fullmatch('[1-9][0-9]*',result[key]) for key in ('GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT')) or
        os.environ.get('GITHUB_REF')!='refs/heads/prep/osv-prerequisites-20261011-r1' or
        os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted'):
        raise Refusal('exact current hosted identity required')
    return result
def owned(path,empty=False):
    path=Path(path);s=path.lstat()
    if (path.resolve()!=path or not stat.S_ISDIR(s.st_mode) or
        s.st_uid!=os.getuid() or stat.S_IMODE(s.st_mode)!=0o700 or
        (empty and any(path.iterdir()))):
        raise Refusal('fresh exclusively owned canonical directory required')
    return path
def fingerprint(s):
    return (s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_gid,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
def read_regular(path,limit,budget):
    path=Path(path);s=path.lstat()
    if (path.resolve()!=path or not stat.S_ISREG(s.st_mode) or s.st_uid!=os.getuid() or
        s.st_mode&0o6022 or s.st_size>limit):
        raise Refusal('bounded owned regular read required')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        if fingerprint(os.fstat(fd))!=fingerprint(s):raise Refusal('read object rebound')
        parts=[];total=0
        while True:
            budget.left();chunk=os.read(fd,65536)
            if not chunk:break
            total+=len(chunk)
            if total>limit:raise Refusal('bounded read overflow')
            parts.append(chunk)
        if fingerprint(os.fstat(fd))!=fingerprint(s):raise Refusal('read object changed')
        budget.left();return b''.join(parts)
    finally:os.close(fd)
def read_receipt(path,digest,budget):
    if not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest):
        raise Refusal('exact trusted receipt binding is missing')
    raw=read_regular(path,65536,budget)
    if hashlib.sha256(raw).hexdigest()!=digest:raise Refusal('trusted receipt digest differs')
    return decode(raw)
def hash_artifact(path,budget,mode=0o500,elf=False):
    path=Path(path);s=path.lstat()
    if (path.resolve()!=path or not stat.S_ISREG(s.st_mode) or
        s.st_uid!=os.getuid() or s.st_nlink!=1 or stat.S_IMODE(s.st_mode)!=mode or s.st_size!=SIZE):
        raise Refusal('exact artifact owner/mode/size required')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        if fingerprint(os.fstat(fd))!=fingerprint(s):raise Refusal('artifact rebound')
        h=hashlib.sha256();total=0
        while True:
            budget.left();chunk=os.read(fd,1048576)
            if not chunk:break
            total+=len(chunk)
            if total>SIZE:raise Refusal('artifact grew')
            h.update(chunk)
        if total!=SIZE or h.hexdigest()!=SHA:raise Refusal('exact artifact digest differs')
        if elf:
            header=os.pread(fd,64,0)
            if len(header)!=64:raise Refusal('short ELF')
            h64=struct.unpack('<16sHHIQQQIHHHHHH',header)
            if (h64[0][:7]!=b'\x7fELF\x02\x01\x01' or h64[1]!=2 or h64[2]!=62 or
                h64[8]!=64 or h64[9]!=56 or not 0<h64[10]<=128 or h64[5]+56*h64[10]>SIZE):
                raise Refusal('static native ELF64 required')
            for n in range(h64[10]):
                row=struct.unpack('<IIQQQQQQ',os.pread(fd,56,h64[5]+56*n))
                if row[0]==3:raise Refusal('PT_INTERP is excluded')
                if row[0]==1 and row[1]&3==3:raise Refusal('writable executable ELF segment is excluded')
                if row[0]==0x6474e551 and row[1]&1:raise Refusal('executable ELF stack is excluded')
                if row[2]+row[5]>SIZE:raise Refusal('ELF segment overflow')
        if fingerprint(os.fstat(fd))!=fingerprint(s):raise Refusal('artifact changed')
        budget.left()
        return {'sha256':SHA,'bytes':SIZE,'mode':oct(mode),'uid':s.st_uid,'gid':s.st_gid,
            'device':s.st_dev,'inode':s.st_ino,'mtime_ns':s.st_mtime_ns,'ctime_ns':s.st_ctime_ns}
    finally:os.close(fd)
def fresh_write(path,raw,budget,mode=0o400):
    if len(raw)>65536:raise Refusal('receipt bound exceeded')
    with Path(path).open('xb') as f:
        os.fchmod(f.fileno(),mode);f.write(raw);f.flush();os.fsync(f.fileno())
    budget.left()
def seal(path,value,budget):
    path=Path(path);pending=path.with_name(path.name+'.pending')
    if path.exists() or pending.exists():raise Refusal('fresh receipt paths required')
    try:
        fresh_write(pending,(json.dumps(value,sort_keys=True,indent=2)+'\n').encode(),budget)
        budget.left();os.rename(pending,path)
        fd=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(fd)
        finally:os.close(fd)
        budget.left()
    except BaseException:
        # Even if unlink fails, callers must require actual success and a fresh
        # invocation ID. Existing complete bytes alone never admit a failed step.
        for p in (path,pending):
            try:p.unlink(missing_ok=True)
            except OSError:pass
        raise
def schema(raw):
    if len(raw)>4194304:raise Refusal('schema output bound exceeded')
    d=decode(raw)
    if (not isinstance(d,dict) or d.get('$schema')!='https://json-schema.org/draft/2020-12/schema' or
        d.get('title')!='Workflow Configuration' or d.get('type')!='object' or
        not isinstance(d.get('required'),list) or 'modules' not in d['required'] or
        not isinstance(d.get('properties'),dict) or
        not {'modules','workflows','triggers','pipelines'}<=d['properties'].keys()):
        raise Refusal('exact schema readiness response required')
    return d
