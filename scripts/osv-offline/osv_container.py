"""Owned Docker lifecycle for the offline scanner on ephemeral GitHub runners.

The provider daemon is trusted. No daemon socket, credentials or host namespace
is exposed to the nonroot scanner. The manager and its subprocesses share the
container's hard resource group; client retirement is never container cleanup.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import selectors
import signal
import subprocess
import time

IMAGE = 'docker.io/library/python@sha256:f040863673aea2570c3ff6a5c3fb4c673a016cbc5375005ad145915922b6b78a'
IMAGE_CONFIG = 'sha256:4143a73d5a9804c1bb2f93cc823a7c173dbc62964d4a5ccabcb1167229b0e2b9'
IMAGE_ENV = ['PATH=/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
    'GPG_KEY=7169605F62C751356D054A26A821E680E5FA6305', 'PYTHON_VERSION=3.13.16',
    'PYTHON_SHA256=f4b1bfb3c79b5bb11b8d228a12504163b4c0dab4d679828d8f5f26b6cb6ab35d']
DOCKER = '/usr/bin/docker'
ID = re.compile(r'^[0-9a-f]{64}$')
LIMITS = {'memory.max': '2147483648', 'memory.swap.max': '0', 'pids.max': '256'}
ENV = {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C', 'HOME': '/nonexistent'}

class BoundaryError(RuntimeError):
    pass

def confirmed_absence(result, identifier):
    # Docker28 passes the engine-client prefix through unchanged. Only a
    # complete, exact-target not-found response with no inspect rows qualifies.
    messages=('Error: No such container: '+identifier,
              'Error: No such object: '+identifier,
              'Error response from daemon: No such container: '+identifier)
    if result.returncode!=1 or result.stderr.strip() not in messages:
        return False
    try:return json.loads(result.stdout)==[]
    except (ValueError,TypeError):return False

def eligible():
    if (platform.system() != 'Linux' or platform.machine() != 'x86_64' or
        os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or os.geteuid() == 0):
        raise BoundaryError('container launcher requires a nonroot ephemeral GitHub Linux/amd64 runner')

def command(argv, seconds=15, *, stdout_limit=1024*1024):
    # Docker-client output is bounded; container logs are disabled separately.
    proc = subprocess.Popen([DOCKER, *argv], env=ENV, stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True,
        start_new_session=True)
    selector = selectors.DefaultSelector(); buffers = [bytearray(), bytearray()]
    deadline = time.monotonic()+seconds
    reaped = False
    try:
        for index, pipe in enumerate((proc.stdout,proc.stderr)):
            os.set_blocking(pipe.fileno(),False); selector.register(pipe,selectors.EVENT_READ,index)
        while selector.get_map():
            left = deadline-time.monotonic()
            if left <= 0:
                raise BoundaryError('Docker client deadline exceeded')
            for key,_ in selector.select(min(left,0.25)):
                block = os.read(key.fileobj.fileno(),65536)
                if not block:
                    selector.unregister(key.fileobj);continue
                index=key.data
                if len(buffers[index])+len(block) > (stdout_limit if index==0 else 1024*1024):
                    raise BoundaryError('Docker client output exceeded bound')
                buffers[index].extend(block)
        left = deadline-time.monotonic()
        if left <= 0:
            raise BoundaryError('Docker client deadline exceeded')
        rc=proc.wait(timeout=left);reaped=True
        return subprocess.CompletedProcess(argv,rc,buffers[0].decode(),buffers[1].decode())
    finally:
        selector.close()
        if not reaped:
            try:os.killpg(proc.pid,signal.SIGKILL)
            except ProcessLookupError:pass
            proc.wait(timeout=5)
        proc.stdout.close();proc.stderr.close()

class Container:
    def __init__(self, output, helper_root, *, mounts=(), seconds=150, runner=command):
        eligible()
        self.output, self.helper_root = Path(output), Path(helper_root)
        self.run = runner
        self.seconds = seconds
        self.identifier = None
        self.creation_attempted = False
        self.inspection_receipts = []
        self.nonce = secrets.token_hex(16)
        self.name = 'wf-osv-' + os.environ['GITHUB_RUN_ID'] + '-' + os.environ['GITHUB_RUN_ATTEMPT'] + '-' + self.nonce
        self.labels = {'workflow.osv.owner': self.nonce, 'workflow.osv.commit': os.environ['GITHUB_SHA']}
        self.mounts = [(self.helper_root, '/wf', True), (self.output, '/out', False), *mounts]
        self.cidfile = self.output / 'container.id'
        self.tmpfs = '/tmp:rw,nosuid,nodev,noexec,size=1073741824,mode=700,uid='+str(os.getuid())+',gid='+str(os.getgid())
        self.spec = None
        self.retired = False

    def inspect(self, identifier):
        if len(self.inspection_receipts)>=16:raise BoundaryError('inspection evidence bound exceeded')
        r = self.run(['container', 'inspect', identifier], 15)
        absent=confirmed_absence(r,identifier)
        classification='confirmed-absence' if absent else ('inspect-success' if not r.returncode else 'unclassified-error')
        if r.returncode and 'permission denied' in r.stderr.lower():classification='permission-denied'
        if r.returncode and 'cannot connect to the docker daemon' in r.stderr.lower():classification='connection-failed'
        self.inspection_receipts.append({'target':identifier,'exit_code':r.returncode,
            'classification':classification,'stdout_bytes':len(r.stdout.encode()),'stderr_bytes':len(r.stderr.encode()),
            'stdout_sha256':hashlib.sha256(r.stdout.encode()).hexdigest(),
            'stderr_sha256':hashlib.sha256(r.stderr.encode()).hexdigest()})
        if r.returncode:
            if absent:return None
            raise BoundaryError('Docker inspection failed; absence is unproved')
        rows = json.loads(r.stdout)
        if len(rows) != 1:
            raise BoundaryError('ambiguous container identity')
        return rows[0]

    def lifecycle_evidence(self):
        return {'constructed':True,'creation_attempted':self.creation_attempted,
            'container_id':self.identifier,'name':self.name,'retired':self.retired,
            'inspections':[dict(row) for row in self.inspection_receipts]}

    def owned(self, row):
        if (row is None or not ID.fullmatch(row.get('Id', '')) or row.get('Image') != IMAGE_CONFIG or
            row.get('Name') != '/' + self.name or row['Config']['Image'] != IMAGE or
            any(row['Config']['Labels'].get(k) != v for k, v in self.labels.items())):
            raise BoundaryError('container ownership does not match exact creation')
        if self.identifier is not None and row['Id'] != self.identifier:
            raise BoundaryError('container ID rebound')
        return row

    def create(self, worker_args=()):
        if self.inspect(self.name) is not None:
            raise BoundaryError('fresh container name already exists')
        uid, gid = os.getuid(), os.getgid()
        self.payload = ['/usr/bin/env', '-i', 'PATH=/usr/local/bin:/usr/bin:/bin',
            'LANG=C', 'LC_ALL=C', 'HOME=/tmp', 'RUNNER_ENVIRONMENT=github-hosted',
            '/usr/bin/timeout', '--signal=TERM', '--kill-after=5s', str(self.seconds)+'s',
            '/usr/local/bin/python3', '-I', '-S', '-B', '-X',
            'pycache_prefix=/tmp/absent-python-cache', '/wf/container-worker.py', *worker_args]
        argv = ['create', '--name', self.name, '--cidfile', str(self.cidfile),
            '--platform', 'linux/amd64', '--network', 'none', '--ipc', 'private',
            '--cgroupns', 'private', '--user', str(uid)+':'+str(gid),
            '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges=true',
            '--security-opt', 'seccomp='+str(self.helper_root/'container-seccomp.json'),
            '--read-only', '--memory', '2147483648', '--memory-swap', '2147483648',
            '--pids-limit', '256', '--cpus', '2', '--restart', 'no', '--no-healthcheck',
            '--shm-size', '67108864',
            '--log-driver', 'none', '--entrypoint', self.payload[0],
            '--tmpfs', self.tmpfs]
        for k, v in self.labels.items():
            argv += ['--label', k+'='+v]
        for source, target, readonly in self.mounts:
            source = Path(source)
            if not source.is_absolute() or source.is_symlink() or not source.exists() or ',' in str(source):
                raise BoundaryError('unbound container mount')
            argv += ['--mount', 'type=bind,src='+str(source)+',dst='+target+(',readonly,bind-recursive=readonly' if readonly else '')]
        argv += [IMAGE, *self.payload[1:]]
        self.creation_attempted = True
        r = self.run(argv, 30)
        # Even a failed Docker client may have created a daemon-owned resource.
        # Recover only an exact label/image/name-bound ID, never start by name.
        row = self.inspect(self.name)
        if row is not None:
            self.identifier = self.owned(row)['Id']
        if r.returncode or self.identifier is None or not self.cidfile.is_file() or self.cidfile.is_symlink():
            raise BoundaryError('container creation did not complete')
        cid = self.cidfile.read_text().strip()
        if cid != self.identifier or r.stdout.strip() != cid:
            raise BoundaryError('container creation ID mismatch')
        self.validate(row)
        self.spec = row
        return row

    def validate(self, row):
        self.owned(row)
        h, c = row['HostConfig'], row['Config']
        expected = {'NetworkMode':'none','IpcMode':'private','CgroupnsMode':'private',
            'ReadonlyRootfs':True,'Privileged':False,'Memory':2147483648,
            'MemorySwap':2147483648,'PidsLimit':256,'NanoCpus':2000000000,
            'PidMode':'','UTSMode':'','UsernsMode':'','ShmSize':67108864,
            'Tmpfs':{'/tmp':self.tmpfs.split(':',1)[1]}}
        if any(h.get(k) != v for k,v in expected.items()):
            raise BoundaryError('container isolation/resource configuration mismatch')
        security=h.get('SecurityOpt',[])
        nnp=[v for v in security if v in {'no-new-privileges','no-new-privileges=true','no-new-privileges:true'}]
        filters=[v[8:] for v in security if v.startswith('seccomp=')]
        # Docker CLI sends the profile JSON to the daemon, not its client-side
        # pathname. Inspect must bind the actual effective policy contents.
        if len(security)!=2 or len(nnp)!=1 or len(filters)!=1:
            raise BoundaryError('container security policy inventory mismatch')
        if json.loads(filters[0])!=json.loads((self.helper_root/'container-seccomp.json').read_text()):
            raise BoundaryError('container effective seccomp policy differs')
        if (h.get('CapAdd') or h.get('CapDrop') != ['ALL'] or h.get('Devices') or
            h.get('RestartPolicy',{}).get('Name') != 'no' or h['LogConfig']['Type'] != 'none' or
            c.get('User') != str(os.getuid())+':'+str(os.getgid()) or
            c.get('Entrypoint') != [self.payload[0]] or c.get('Cmd') != self.payload[1:] or
            c.get('Env') != IMAGE_ENV or c.get('Volumes') or c.get('Healthcheck',{}).get('Test') != ['NONE']):
            raise BoundaryError('container privilege/command/default mismatch')
        actual = {(r['Source'],r['Destination'],r['RW']) for r in row['Mounts'] if r['Type']=='bind'}
        expected_mounts = {(str(s), t, not ro) for s,t,ro in self.mounts}
        if actual != expected_mounts:
            raise BoundaryError('container mount inventory mismatch')
        if any(r['Type'] not in ('bind','tmpfs') or
               (r['Type']=='tmpfs' and r['Destination']!='/tmp') for r in row['Mounts']):
            raise BoundaryError('unexpected writable container mount')

    def start_wait(self):
        self.validate(self.inspect(self.identifier))
        r = self.run(['start', self.identifier], 15)
        if r.returncode or r.stdout.strip() != self.identifier:
            raise BoundaryError('owned container did not start')
        r = self.run(['wait', self.identifier], self.seconds+10)
        if r.returncode or not r.stdout.strip().isdecimal():
            raise BoundaryError('owned container wait did not complete')
        row = self.owned(self.inspect(self.identifier)); self.validate(row)
        if row['State']['Running'] or row['State']['Pid'] != 0 or row['State']['ExitCode'] != int(r.stdout.strip()) or row.get('RestartCount') != 0:
            raise BoundaryError('container termination/exit mismatch')
        return row['State']['ExitCode'], row

    def cleanup(self):
        if self.identifier is None:
            row = self.inspect(self.name)
            if row is None:
                if self.creation_attempted:
                    raise BoundaryError('daemon creation completion is uncertain; absence cannot prove retirement')
                self.retired = True
                return {'created':False,'absent':True}
            self.identifier = self.owned(row)['Id']
        row = self.owned(self.inspect(self.identifier))
        if row['State']['Running']:
            r = self.run(['kill', '--signal', 'KILL', self.identifier], 10)
            if r.returncode:
                raise BoundaryError('owned container kill failed')
            r = self.run(['wait', self.identifier], 10)
            if r.returncode:
                raise BoundaryError('owned container retirement failed')
        row = self.owned(self.inspect(self.identifier))
        if row['State']['Running'] or row['State']['Pid'] != 0:
            raise BoundaryError('owned container remains live')
        r = self.run(['rm', self.identifier], 10)
        if r.returncode or r.stdout.strip() != self.identifier or self.inspect(self.identifier) is not None:
            raise BoundaryError('owned container removal/absence failed')
        self.retired = True
        return {'container_id':self.identifier,'absent':True,'forced_removal':False}
