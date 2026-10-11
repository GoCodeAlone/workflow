"""Bounded, separately approved same-runner proof; never scanner acceptance."""
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import sys
import time

HERE=Path(__file__).resolve().parent

def write(path,value):
    with path.open('x') as f:
        os.fchmod(f.fileno(),0o600);json.dump(value,f,indent=2,sort_keys=True)
        f.write('\n');f.flush();os.fsync(f.fileno())

def verify_source(expected_digest=None):
    path=HERE/'container-source-bindings.json'
    if path.is_symlink() or not path.is_file():raise RuntimeError('regular source bindings required')
    raw=path.read_bytes();digest=hashlib.sha256(raw).hexdigest()
    if expected_digest is not None and digest!=expected_digest:
        raise RuntimeError('source binding changed during proof')
    bindings=json.loads(raw)
    if (bindings.get('schema')!='workflow.offline-scanner.container-source-bindings.v1' or
        set(bindings.get('files',{}))!={'osv_container.py','container-worker.py','container-seccomp.json','run-container-proof.py'}):
        raise RuntimeError('exact source inventory required')
    for name,expected in bindings['files'].items():
        p=HERE/name
        if p.is_symlink() or not p.is_file() or hashlib.sha256(p.read_bytes()).hexdigest()!=expected:
            raise RuntimeError('container source binding mismatch: '+name)
    return bindings,digest

def load_launcher():
    spec=importlib.util.spec_from_file_location('osv_container',HERE/'osv_container.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

def run(output,compiler_root,execute=False):
    started=time.monotonic();container=None;cleanup=None;fixture=None
    result={'schema':'workflow.offline-scanner.container-runtime-proof.v1','accepted':False,
        'runtime_boundary_pass':False,'scanner_execution':False,'candidate_execution':False,
        'installation_admitted':False,'recorded_at':datetime.now(timezone.utc).isoformat()}
    code=2;stage='authorization'
    try:
        if not execute:raise RuntimeError('explicit approved execution required')
        stage='source-verification'
        bindings,binding_digest=verify_source()
        module=load_launcher()
        if bindings['image']!=module.IMAGE or bindings['image_config']!=module.IMAGE_CONFIG:
            raise RuntimeError('image source binding mismatch')
        module.eligible()
        stage='owned-output'
        parent=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
        info=output.lstat()
        if output.parent!=parent or not stat.S_ISDIR(info.st_mode) or info.st_uid!=os.getuid() or stat.S_IMODE(info.st_mode)!=0o700 or list(output.iterdir()):
            raise RuntimeError('fresh owned proof directory required')
        if compiler_root.is_symlink() or not compiler_root.is_absolute() or not (compiler_root/'bin/go').is_file():
            raise RuntimeError('installed inspected compiler root required')
        identity={k:os.environ[k] for k in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')}
        if identity['GITHUB_REPOSITORY']!='GoCodeAlone/workflow' or not identity['GITHUB_RUN_ID'].isdecimal() or not identity['GITHUB_RUN_ATTEMPT'].isdecimal():
            raise RuntimeError('unbound hosted identity')
        # RO inputs cannot sit beneath a writable output bind: that would leave
        # a second writable alias despite a read-only /fixture mount.
        fixture=parent/(output.name+'-inputs');fixture.mkdir(mode=0o700)
        (fixture/'input').write_text('owned read-only proof fixture\n')
        request={'uid':os.getuid(),'gid':os.getgid(),'identity':identity,
            'host_namespaces':{n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')},
            'readonly_targets':['/wf','/go','/fixture'],
            'compiler_sha256':hashlib.sha256((compiler_root/'bin/go').read_bytes()).hexdigest()}
        write(output/'request.json',request)
        result.update(identity=identity,image=module.IMAGE,image_config=module.IMAGE_CONFIG,
            source_binding_sha256=binding_digest,
            seccomp_sha256=bindings['files']['container-seccomp.json'],daemon_trust='existing provider daemon on ephemeral GitHub VM')
        stage='container-create'
        container=module.Container(output,HERE,mounts=[(compiler_root,'/go',True),(fixture,'/fixture',True)],seconds=90)
        row=container.create();result['container_id']=row['Id']
        stage='actual-container-witness'
        exit_code,row=container.start_wait();result['actual_exit']=exit_code
        path=output/'worker-result.json'
        if path.is_symlink() or not path.is_file() or path.stat().st_size>1024*1024:
            raise RuntimeError('actual worker receipt absent or malformed')
        worker=json.loads(path.read_text());result['worker']=worker
        if exit_code!=0 or worker.get('complete') is not True or worker.get('exit')!=0 or worker.get('identity')!=identity:
            raise RuntimeError('actual worker did not establish boundary')
        stage='cleanup';cleanup=container.cleanup()
        stage='source-conservation';verify_source(binding_digest)
        if time.monotonic()-started>=180:raise RuntimeError('overall proof deadline exceeded')
        result.update(runtime_boundary_pass=True,cleanup=cleanup);code=0
    except Exception as error:
        result['failure']={'stage':stage,'type':type(error).__name__,'message':str(error)[:400]}
    finally:
        if container is not None and not container.retired:
            try:cleanup=container.cleanup();result['cleanup']=cleanup
            except Exception as error:
                result['cleanup_failure']={'type':type(error).__name__,'message':str(error)[:300]};code=2;result['runtime_boundary_pass']=False
        if fixture is not None and container is not None and container.retired:
            try:
                if (fixture/'input').read_text()!='owned read-only proof fixture\n':raise RuntimeError('proof input changed')
                (fixture/'input').unlink();fixture.rmdir()
                result['input_cleanup']=True
            except Exception as error:
                result['input_cleanup_failure']=type(error).__name__;code=2;result['runtime_boundary_pass']=False
        result['lifecycle']=container.lifecycle_evidence() if container is not None else {
            'constructed':False,'creation_attempted':False,'container_id':None,'retired':False,'inspections':[]}
    result['elapsed_seconds']=round(time.monotonic()-started,3)
    if result['elapsed_seconds']>=180:code=2;result['runtime_boundary_pass']=False
    write(output/'container-runtime-proof.json',result)
    if time.monotonic()-started>=180:
        result.update(runtime_boundary_pass=False,publication_deadline_exceeded=True)
        # A late receipt cannot retain a positive result even though the actual
        # step outcome is independently mandatory for accepting this proof.
        with (output/'container-runtime-proof.json').open('w') as f:
            json.dump(result,f,indent=2,sort_keys=True);f.write('\n');f.flush();os.fsync(f.fileno())
        return 2
    return code

def interrupted(signum,frame):raise RuntimeError('bounded proof interrupted')

if __name__=='__main__':
    signal.signal(signal.SIGTERM,interrupted)
    if len(sys.argv)!=4 or sys.argv[3]!='--execute':raise SystemExit(2)
    raise SystemExit(run(Path(sys.argv[1]),Path(sys.argv[2]),True))
