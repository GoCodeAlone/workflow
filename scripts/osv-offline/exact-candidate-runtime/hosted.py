"""Fixed hosted caller; no action on import. Helpers load only after all pins.

The workflow independently pins this file before execution. GH_TOKEN is used
only by the fixed public-repository GitHub CLI read phase, never a container.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import secrets
import selectors
import signal
import stat
import subprocess
import sys
import time

HERE=Path(__file__).resolve().parent
CORE={'candidate_contract.py','candidate_enforcement.py','candidate_controls.py','container-worker.py',
    'run-candidate-runtime.py','receive-candidate.py','osv_container.py','container-seccomp.json'}
REPOSITORY='GoCodeAlone/workflow';REF='refs/heads/prep/osv-prerequisites-20261011-r1'
SHA='9819a5b582e1f0b6ab135b75c5113baf36f7507a6904e64e2984c318447e8bdf'
SIZE=115900576;RELEASE=409874491;ASSET=631609441
NAME='wfctl-candidate81b9-linux-amd64';TAG='unpublished-validation-9819a5-20261011-r1'
GH='/usr/bin/gh'
class Refusal(RuntimeError):pass
def strict(pairs):
    obj={}
    for key,value in pairs:
        if key in obj:raise Refusal('duplicate JSON key')
        obj[key]=value
    return obj
def parse(raw):
    return json.loads(raw,object_pairs_hook=strict,
        parse_constant=lambda v:(_ for _ in ()).throw(Refusal('nonfinite JSON')))
def read_source(path,limit):
    before=path.lstat()
    if (path.resolve()!=path or not stat.S_ISREG(before.st_mode) or
        before.st_uid!=os.getuid() or before.st_mode&0o6022 or before.st_size>limit):
        raise Refusal('bounded owned source read required')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        opened=os.fstat(fd)
        if (opened.st_dev,opened.st_ino)!=(before.st_dev,before.st_ino):raise Refusal('source rebound')
        raw=bytearray()
        while True:
            chunk=os.read(fd,65536)
            if not chunk:break
            raw.extend(chunk)
            if len(raw)>limit:raise Refusal('source bound exceeded')
        after=os.fstat(fd)
        attrs=('st_dev','st_ino','st_mode','st_uid','st_gid','st_size','st_mtime_ns','st_ctime_ns')
        if any(getattr(before,k)!=getattr(after,k) for k in attrs):raise Refusal('source changed')
        return bytes(raw)
    finally:os.close(fd)
def cold_guard():
    if (not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode or
        os.environ.get('GITHUB_EVENT_NAME')!='workflow_dispatch' or
        os.environ.get('GITHUB_REPOSITORY')!=REPOSITORY or os.environ.get('GITHUB_REF')!=REF or
        os.environ.get('GITHUB_JOB')!='exact-candidate-runtime' or
        os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted' or
        platform.system()!='Linux' or platform.machine()!='x86_64' or os.geteuid()==0):
        raise Refusal('exact approved hosted event/identity required')
    raw=os.environ.get('OSV_RUNTIME_SOURCE_REVIEW','').encode()
    digest=os.environ.get('OSV_RUNTIME_SOURCE_REVIEW_SHA','')
    if len(raw)>65536 or not re.fullmatch('[0-9a-f]{64}',digest) or hashlib.sha256(raw).hexdigest()!=digest:
        raise Refusal('bounded exact source review JSON/hash required')
    review=parse(raw)
    expected_keys={'schema','complete','accepted','source_manifest_sha256','cold_wrapper_sha256','workflow_sha256',
        'security_decision_sha256','antagonistic_decision_sha256','security_critical','security_important',
        'security_advisory','antagonistic_critical','antagonistic_important','antagonistic_advisory',
        'allows_native_controls','candidate_runtime_authorized','artifact_sha256','operation','workflow_ref'}
    if set(review)!=expected_keys:raise Refusal('review receipt must contain only exact public admission fields')
    for key in ('security_critical','security_important','security_advisory',
        'antagonistic_critical','antagonistic_important','antagonistic_advisory'):
        if type(review.get(key)) is not int or review[key]!=0:raise Refusal('both current zero-finding source reviews required')
    for key in ('security_decision_sha256','antagonistic_decision_sha256','source_manifest_sha256',
        'cold_wrapper_sha256','workflow_sha256'):
        if not isinstance(review.get(key),str) or not re.fullmatch('[0-9a-f]{64}',review[key]):
            raise Refusal('complete current source review pins required')
    manifest_raw=read_source(HERE/'source-manifest.json',65536)
    if hashlib.sha256(manifest_raw).hexdigest()!=review['source_manifest_sha256']:
        raise Refusal('source manifest differs')
    manifest=parse(manifest_raw)
    if set(manifest['files'])!=CORE:raise Refusal('eight-helper inventory differs')
    for name,row in manifest['files'].items():
        content=read_source(HERE/name,row['bytes'])
        if len(content)!=row['bytes'] or hashlib.sha256(content).hexdigest()!=row['sha256']:
            raise Refusal('helper source pin differs')
    if hashlib.sha256(read_source(HERE/'hosted.py',65536)).hexdigest()!=review['cold_wrapper_sha256']:
        raise Refusal('cold wrapper differs')
    repo=HERE.parents[2]
    workflow=repo/'.github/workflows/copilot-setup-steps.yml'
    if hashlib.sha256(read_source(workflow,65536)).hexdigest()!=review['workflow_sha256']:
        raise Refusal('complete workflow pin differs')
    # This is the FIRST helper import, after manifest and all helper hashes.
    sys.path.insert(0,str(HERE))
    contract=__import__('candidate_contract')
    contract.identity()
    temporary=write_review_temp(raw)
    try:
        load('launcher','run-candidate-runtime.py').verify_bundle(contract.Budget(5),
            temporary,digest,contract.identity(),'candidate')
    finally:temporary.unlink()
    return contract,raw,digest,review
def write_review_temp(raw):
    # Verification needs an owned regular receipt. This temporary root-owned
    # input is private, fresh and contains only supplied review metadata.
    parent=Path(os.environ['RUNNER_TEMP'])
    import tempfile
    fd,name=tempfile.mkstemp(prefix='exact-review-',suffix='.json',dir=parent)
    try:
        os.fchmod(fd,0o400);os.write(fd,raw);os.fsync(fd)
    finally:os.close(fd)
    return Path(name)
def load(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);return module
def host_env():
    return {'PATH':'/usr/bin:/bin','HOME':'/nonexistent','LANG':'C','LC_ALL':'C',
        'GIT_CONFIG_GLOBAL':'/dev/null','GIT_CONFIG_SYSTEM':'/dev/null',
        'GIT_CONFIG_NOSYSTEM':'1','GIT_TERMINAL_PROMPT':'0'}
def native(argv,env,seconds,limit,sink=None):
    proc=subprocess.Popen(argv,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,close_fds=True,start_new_session=True)
    selector=selectors.DefaultSelector();output=bytearray();h=hashlib.sha256()
    stderr_h=hashlib.sha256();counts=[0,0];deadline=time.monotonic()+seconds;reaped=False
    try:
        for i,p in enumerate((proc.stdout,proc.stderr)):
            os.set_blocking(p.fileno(),False);selector.register(p,selectors.EVENT_READ,i)
        while selector.get_map():
            left=deadline-time.monotonic()
            if left<=0:raise Refusal('fixed native child deadline')
            for key,_ in selector.select(min(left,0.25)):
                raw=os.read(key.fileobj.fileno(),65536)
                if not raw:selector.unregister(key.fileobj);continue
                i=key.data;counts[i]+=len(raw)
                if counts[i]>(limit if i==0 else 65536):raise Refusal('fixed native child output overflow')
                if i:stderr_h.update(raw)
                else:
                    h.update(raw)
                    if sink is not None:sink.write(raw)
                    else:output.extend(raw)
        left=deadline-time.monotonic()
        if left<=0:raise Refusal('fixed native final deadline')
        code=proc.wait(timeout=left);reaped=True
        evidence={'native_exit':code,'stdout_bytes':counts[0],'stderr_bytes':counts[1],
            'stdout_sha256':h.hexdigest(),'stderr_sha256':stderr_h.hexdigest(),
            'elapsed_seconds':seconds-(deadline-time.monotonic())}
        if code!=0 or counts[1]!=0:
            failure=Refusal('fixed native child exit/stderr refused');failure.native=evidence;raise failure
        return bytes(output),evidence
    except BaseException as fault:
        if not reaped:
            try:os.killpg(proc.pid,signal.SIGKILL)
            except ProcessLookupError:pass
            proc.wait(timeout=5);reaped=True
        fault.native={'native_exit':proc.returncode,'stdout_bytes':counts[0],'stderr_bytes':counts[1],
            'stdout_sha256':h.hexdigest(),'stderr_sha256':stderr_h.hexdigest(),
            'elapsed_seconds':seconds-(deadline-time.monotonic()),'complete':False}
        raise
    finally:
        selector.close()
        if not reaped:
            try:os.killpg(proc.pid,signal.SIGKILL)
            except ProcessLookupError:pass
            proc.wait(timeout=5)
        proc.stdout.close();proc.stderr.close()
def preflight(root,c,review_raw,review_sha,review):
    c.owned(root,True);budget=c.Budget(15)
    commit,native_git=native(['/usr/bin/git','-C',str(HERE.parents[2]),'rev-parse','HEAD'],host_env(),5,128)
    if commit.decode().strip()!=os.environ['GITHUB_SHA']:raise Refusal('actual checkout commit differs')
    for name in ('incoming','receiver','runtime','gh-config'):(root/name).mkdir(mode=0o700)
    c.fresh_write(root/'review.json',review_raw,budget)
    state={'schema':'workflow.GO5932.hosted-state.v1','identity':c.identity(),
        'invocation_id':secrets.token_hex(32),'review_sha256':review_sha,
        'source_manifest_sha256':review['source_manifest_sha256'],'accepted':False,
        'native_checkout':native_git,'artifact_sha256':SHA,'artifact_bytes':SIZE}
    c.seal(root/'state.json',state,budget)
def state(root,c,review_sha):
    c.owned(root);obj=c.decode(c.read_regular(root/'state.json',65536,c.Budget(5)))
    if obj['identity']!=c.identity() or obj['review_sha256']!=review_sha:
        raise Refusal('current invocation state differs')
    if not re.fullmatch('[0-9a-f]{64}',obj['invocation_id']):raise Refusal('fresh token required')
    return obj
def deliver(root,c,state,token):
    budget=c.Budget(200);path=Path(GH);meta=path.lstat()
    if (path.resolve()!=path or not stat.S_ISREG(meta.st_mode) or
        meta.st_uid!=0 or meta.st_mode&0o6022 or not meta.st_mode&0o111 or not token):
        raise Refusal('canonical trusted GitHub CLI/token required')
    env=host_env();env.update(GH_TOKEN=token,GH_HOST='github.com',GH_CONFIG_DIR=str(root/'gh-config'),
        GH_PROMPT_DISABLED='1')
    c.owned(root/'gh-config',True)
    calls=[]
    raw,entry=native([GH,'api','--method','GET','repos/'+REPOSITORY+'/releases/'+str(RELEASE)],
        env,min(15,budget.left()),65536);calls.append(entry);release=c.decode(raw)
    if (release.get('id')!=RELEASE or release.get('draft') is not True or
        release.get('published_at') is not None or release.get('tag_name')!=TAG):
        raise Refusal('exact unpublished approved draft differs')
    endpoint='repos/'+REPOSITORY+'/releases/assets/'+str(ASSET)
    raw,entry=native([GH,'api','--method','GET',endpoint],env,min(15,budget.left()),65536)
    calls.append(entry);asset=c.decode(raw)
    if (asset.get('id')!=ASSET or asset.get('size')!=SIZE or asset.get('digest')!='sha256:'+SHA or
        asset.get('name')!=NAME or asset.get('state')!='uploaded'):
        raise Refusal('exact approved asset metadata differs')
    incoming=root/'incoming'/'wfctl'
    with incoming.open('xb') as f:
        os.fchmod(f.fileno(),0o600)
        _,entry=native([GH,'api','--method','GET','-H','Accept: application/octet-stream',endpoint],
            env,min(180,budget.left()),SIZE,sink=f)
        calls.append(entry);f.flush();os.fsync(f.fileno());os.fchmod(f.fileno(),0o400)
    if entry['stdout_bytes']!=SIZE or entry['stdout_sha256']!=SHA:raise Refusal('native downloaded bytes differ')
    actual=c.hash_artifact(incoming,budget,mode=0o400,elf=True)
    # Recheck exact remote lifecycle after download; a published/changed draft
    # is not silently admitted by an earlier metadata observation.
    raw,entry=native([GH,'api','--method','GET','repos/'+REPOSITORY+'/releases/'+str(RELEASE)],
        env,min(15,budget.left()),65536);calls.append(entry);after=c.decode(raw)
    if after.get('id')!=RELEASE or after.get('draft') is not True or after.get('published_at') is not None:
        raise Refusal('draft state changed during delivery')
    value={'schema':'workflow.GO5932.authenticated-candidate-delivery.v1','complete':True,
        'accepted':False,'candidate_execution':False,'identity':c.identity(),'invocation_id':state['invocation_id'],
        'source_commit':c.SOURCE,'artifact_sha256':SHA,'artifact_bytes':SIZE,'actual_artifact':actual,
        'release_id':RELEASE,'asset_id':ASSET,'draft':True,'published':False,
        'native_calls':calls,'github_cli_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),
        'provisional_until_actual_delivery_step_success':True}
    c.seal(root/'transport-receipt.json',value,budget)
def main():
    started=time.monotonic()
    token=os.environ.pop('GH_TOKEN',None)
    if len(sys.argv)!=4 or sys.argv[3]!='--execute' or sys.argv[1] not in ('preflight','deliver','receive','runtime'):
        raise Refusal('fixed phase invocation required')
    phase=sys.argv[1];root=Path(sys.argv[2])
    c,raw,digest,review=cold_guard()
    if phase=='preflight':preflight(root,c,raw,digest,review);return
    obj=state(root,c,digest)
    try:
        if phase=='deliver':deliver(root,c,obj,token)
        elif phase=='receive':
            if os.environ.get('OSV_DELIVERY_OUTCOME')!='success':raise Refusal('actual delivery step refused')
            receipt=root/'transport-receipt.json';sha=hashlib.sha256(c.read_regular(receipt,65536,c.Budget(5))).hexdigest()
            load('receiver','receive-candidate.py').stage(root/'incoming'/'wfctl',root/'receiver',receipt,sha,
                obj['invocation_id'],c.identity(),c.Budget(30))
        else:
            if os.environ.get('OSV_RECEIVER_OUTCOME')!='success':raise Refusal('actual receiver step refused')
            receipt=root/'receiver'/'receiver-proof.json';sha=hashlib.sha256(c.read_regular(receipt,65536,c.Budget(5))).hexdigest()
            shared=c.Budget(90);shared.deadline=started+90
            load('launcher','run-candidate-runtime.py').run(root/'runtime','candidate',root/'review.json',digest,
                receipt,sha,obj['invocation_id'],c.identity(),receiver_outcome='success',budget=shared)
    except BaseException as error:
        failure={'schema':'workflow.GO5932.hosted-phase-failure.v1','complete':False,'accepted':False,
            'identity':c.identity(),'invocation_id':obj['invocation_id'],'phase':phase,
            'error_type':type(error).__name__}
        if hasattr(error,'native'):failure['native_child']=error.native
        try:c.seal(root/(phase+'-failure.json'),failure,c.Budget(2))
        except BaseException:pass
        raise
if __name__=='__main__':
    try:main()
    except BaseException:os._exit(2)
    os._exit(0)
