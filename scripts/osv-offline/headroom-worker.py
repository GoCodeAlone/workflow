"""One fixed Python-only live worker; no Go, supplier, scanner or cache access."""
from datetime import datetime, timezone
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import socket
import stat
import sys
import time

LIMITS = {'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}

def require(condition):
    if not condition: raise RuntimeError('fixed measurement boundary differs')

def pairs(rows):
    result = {}
    for key, value in rows:
        require(key not in result); result[key] = value
    return result

def request():
    path = Path('/fixture/request.json'); before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid() and before.st_nlink == 1 and before.st_size <= 65536)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno()); raw = stream.read(65537); after = os.fstat(stream.fileno())
    identity = lambda s: (s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
    require(identity(before) == identity(opened) == identity(after) == identity(path.lstat()) and len(raw) == before.st_size)
    value = json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda _: (_ for _ in ()).throw(RuntimeError('nonfinite request')))
    require(value['schema'] == 'workflow.offline-scanner.headroom-request.v1' and value['measurement_only'] is True)
    return value, hashlib.sha256(raw).hexdigest()

def witness(value):
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64')
    require(os.getuid() == value['uid'] and os.getgid() == value['gid'] and os.geteuid() != 0)
    require(sys.flags.isolated and sys.flags.no_site and sys.dont_write_bytecode)
    status = dict(line.split(':',1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    require(status['NoNewPrivs'].strip() == '1' and status['Seccomp'].strip() == '2')
    require(all(int(status[k].strip(),16) == 0 for k in ('CapInh','CapPrm','CapEff','CapBnd','CapAmb')))
    namespaces = {n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')}
    require(all(namespaces[n] != value['host_namespaces'][n] for n in ('net','mnt','pid','ipc','cgroup')))
    group = Path('/proc/self/cgroup').read_text(); require(group == '0::/\n')
    cg = Path('/sys/fs/cgroup'); actual = {name:(cg/name).read_text().strip() for name in LIMITS}
    require(actual == LIMITS and str(os.getpid()) in (cg/'cgroup.procs').read_text().split())
    cpu = (cg/'cpu.max').read_text().split(); require(len(cpu) == 2 and int(cpu[0]) == 2*int(cpu[1]))
    fds = {}
    for path in Path('/proc/self/fd').iterdir():
        try: target = os.readlink(path)
        except FileNotFoundError: continue
        require(int(path.name) <= 2 and not target.startswith('socket:')); fds[path.name] = target
    require(os.read(0,1) == b'')
    for family in (socket.AF_INET,socket.AF_INET6,socket.AF_UNIX):
        try: connection = socket.socket(family,socket.SOCK_STREAM)
        except OSError as error: require(error.errno == errno.EPERM)
        else: connection.close(); raise RuntimeError('socket was permitted')
    libc = ctypes.CDLL(None,use_errno=True)
    for number,args in ((272,(0,)),(165,(0,0,0,0,0)),(101,(0,0,0,0)),(321,(0,0,0)),(425,(0,0))):
        ctypes.set_errno(0); result = libc.syscall(number,*args)
        require(result == -1 and ctypes.get_errno() == errno.EPERM)
    readonly = {}
    for name in ('/wf','/fixture'):
        require(os.statvfs(name).f_flag & os.ST_RDONLY)
        try: fd = os.open(name+'/.headroom-write-denial',os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        except OSError as error: require(error.errno == errno.EROFS); readonly[name] = error.errno
        else: os.close(fd); raise RuntimeError('read-only mount was writable')
    require(all(not Path(name).exists() for name in ('/var/run/docker.sock','/run/docker.sock','/github/home','/home/runner','/go')))
    return {'uid':os.getuid(),'gid':os.getgid(),'private_pid':os.getpid(),'namespaces':namespaces,
        'group_sha256':hashlib.sha256(group.encode()).hexdigest(),'kernel_limits':actual,'cpu_max':cpu,
        'descriptors':fds,'readonly_errno':readonly,'socket_and_escape_denial':'EPERM',
        'no_new_privileges':True,'capabilities':0,'compiler_or_scanner_execution':False,
        'ABI_policy':'unchanged digest-bound seccomp; no additional ABI/child probe'}

def main():
    resource.setrlimit(resource.RLIMIT_FSIZE,(1048576,1048576))
    value, digest = request()
    result = {'schema':'workflow.offline-scanner.headroom-worker.v1','accepted':False,'complete':False,
        'identity':value['identity'],'nonce':value['nonce'],'request_sha256':digest,
        'recorded_at':datetime.now(timezone.utc).isoformat(),'measurement_only':True,'Go_or_scanner_execution':False}
    code = 2
    try:
        result['witness'] = witness(value)
        # Keep the single owned Python worker live for bounded host observation.
        time.sleep(20)
        result.update(complete=True,exit=0); code = 0
    except Exception as error: result.update(exit=2,error_type=type(error).__name__)
    with Path('/out/headroom-worker.json').open('x') as stream:
        os.fchmod(stream.fileno(),0o600); json.dump(result,stream,sort_keys=True); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
    os._exit(code)

if __name__ == '__main__': main()
