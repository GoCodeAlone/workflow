"""Private draft launcher. No Docker commands on import.

Native controls-only mode has no candidate mount. Candidate mode runs all real
controls first, then the exact fixed operation within this same 90s invocation.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import time
HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import candidate_contract as c
import osv_container as container

WORKFLOW_REF='refs/heads/prep/osv-prerequisites-20261011-r1'
def verify_bundle(budget,review,review_sha,current,mode):
    admission=c.read_receipt(review,review_sha,budget)
    manifest_path=HERE/'source-manifest.json'
    raw=c.read_regular(manifest_path,65536,budget)
    digest=hashlib.sha256(raw).hexdigest();manifest=c.decode(raw)
    required={'schema':'workflow.GO5932.source-review-admission.v1',
        'complete':True,'accepted':False,'source_manifest_sha256':digest,
        'security_critical':0,'security_important':0,'antagonistic_critical':0,
        'antagonistic_important':0,'allows_native_controls':True,'workflow_ref':WORKFLOW_REF}
    if any(admission.get(key)!=value for key,value in required.items()):
        raise c.Refusal('exact reviewed source/control admission missing')
    for name in ('security_decision_sha256','antagonistic_decision_sha256'):
        if not isinstance(admission.get(name),str) or not re.fullmatch('[0-9a-f]{64}',admission[name]):
            raise c.Refusal('exact source review seals missing')
    if mode=='candidate' and (admission.get('candidate_runtime_authorized') is not True or
        admission.get('artifact_sha256')!=c.SHA or admission.get('operation')!=['/candidate/wfctl','schema']):
        raise c.Refusal('existing exact candidate operation authority differs')
    if not isinstance(manifest.get('files'),dict) or set(manifest['files'])!={
        'candidate_contract.py','candidate_enforcement.py','candidate_controls.py',
        'container-worker.py','run-candidate-runtime.py','receive-candidate.py',
        'osv_container.py','container-seccomp.json'}:
        raise c.Refusal('complete exact source inventory required')
    for name,row in manifest['files'].items():
        data=c.read_regular(HERE/name,row['bytes'],budget)
        if len(data)!=row['bytes'] or hashlib.sha256(data).hexdigest()!=row['sha256']:
            raise c.Refusal('source bundle differs')
    return digest
def receiver_binding(path,digest,token,current,budget):
    result=c.read_receipt(path,digest,budget)
    expected={'schema':'workflow.GO5932.exact-candidate-receiver.v1','complete':True,
        'accepted':False,'candidate_execution':False,'identity':current,
        'invocation_id':token,'source_commit':c.SOURCE}
    if any(result.get(k)!=v for k,v in expected.items()):
        raise c.Refusal('fresh current receiver binding differs')
    artifact=Path(path).parent/'candidate'/'wfctl'
    actual=c.hash_artifact(artifact,budget,elf=True)
    if actual!=result['receiver_artifact']:raise c.Refusal('receiver metadata/bytes changed')
    return artifact,actual

def run(output,mode,review,review_sha,receiver,receiver_sha,token,current,*,receiver_outcome=None,factory=container.Container,command=container.command,budget=None):
    if mode not in ('controls-only','candidate') or not re.fullmatch('[0-9a-f]{64}',token):
        raise c.Refusal('fixed mode and fresh invocation required')
    budget=budget or c.Budget(90);output=c.owned(output,True)
    bundle=verify_bundle(budget,review,review_sha,current,mode)
    artifact=None;before=None
    if mode=='candidate':
        if receiver_outcome!='success':raise c.Refusal('actual current receiver step did not succeed')
        artifact,before=receiver_binding(receiver,receiver_sha,token,current,budget)
    elif receiver!='-' or receiver_sha!='-':raise c.Refusal('control-only mode has no candidate binding')
    child=output/'container';child.mkdir(mode=0o700)
    operations=output/'operation';operations.mkdir(mode=0o700)
    for name in ('work','home','tmp','no-external-plugins'):(operations/name).mkdir(mode=0o700)
    request={'schema':'workflow.GO5932.exact-runtime-request.v1','mode':mode,'identity':current,
        'workflow_ref':WORKFLOW_REF,'invocation_id':token,'source_manifest_sha256':bundle,
        'artifact_sha256':c.SHA if artifact else None,'uid':os.getuid(),'gid':os.getgid(),
        'host_namespaces':{n:os.readlink('/proc/self/ns/'+n) for n in ('net','mnt','pid','ipc','cgroup','user')},
        'readonly_targets':['/wf']+(['/candidate/wfctl'] if artifact else [])}
    c.fresh_write(child/'request.json',(json.dumps(request,sort_keys=True)+'\n').encode(),budget)
    manager=None
    def bounded(argv,seconds=15):
        # The frozen general helper's built-in reserve is 90s, so its optional
        # deadline is intentionally unset. This replacement runner enforces the
        # REAL shared 90s budget and a 30s candidate-only cleanup reserve.
        remaining=budget.left()-(0 if manager is not None and manager.cleanup_mode else 30)
        if remaining<=0:raise c.Refusal('shared 90s budget leaves no 30s cleanup reserve')
        return command(argv,min(seconds,remaining))
    mounts=[(operations,'/operation',False)]
    if artifact:mounts.append((artifact,'/candidate/wfctl',True))
    manager=factory(child,HERE,mounts=mounts,seconds=55,runner=bounded,
        deadline=None,ledger=output/'container-ownership.json')
    native=None;failure=None;retired=None
    try:
        manager.create(('/out/request.json',))
        native,state=manager.start_wait()
        if native!=0:raise c.Refusal('actual worker phase did not succeed')
        raw=c.read_regular(child/'worker-result.json',65536,budget)
        worker=c.decode(raw)
        exact={'schema':'workflow.GO5932.exact-runtime-worker.v1','complete':True,
            'accepted':False,'identity':current,'invocation_id':token,'mode':mode,
            'source_manifest_sha256':bundle,'candidate_execution':mode=='candidate'}
        if any(worker.get(key)!=value for key,value in exact.items()):
            raise c.Refusal('exact actual-success-bound worker receipt differs')
        controls=c.read_receipt(child/'native-controls.json',worker['native_controls_sha256'],budget)
        expected_names=c.decode(c.read_regular(HERE/'source-manifest.json',65536,budget))['native_control_names']
        if (controls.get('all_native_pass') is not True or controls.get('complete') is not True or
            controls.get('identity')!=current or controls.get('invocation_id')!=token or
            controls.get('source_manifest_sha256')!=bundle or not controls.get('rows') or
            any(row.get('pass') is not True for row in controls['rows']) or
            [row.get('name') for row in controls['rows']]!=expected_names):
            raise c.Refusal('actual current native controls are incomplete')
    except BaseException as error:failure=error
    finally:
        try:retired=manager.cleanup()
        except BaseException as error:
            failure=error
        # No usable success receipt until exact-ID container retirement.
    if failure is not None:
        value={'schema':'workflow.GO5932.exact-runtime-controller.v1','complete':False,'accepted':False,
            'identity':current,'invocation_id':token,'mode':mode,'worker_native_exit':native,
            'source_manifest_sha256':bundle,'lifecycle':manager.lifecycle_evidence(),
            'error_type':type(failure).__name__,'error':str(failure)[:240]}
        try:c.seal(output/'controller-failure.json',value,budget)
        except BaseException:pass
        raise failure
    if not manager.retired or retired.get('absent') is not True:
        raise c.Refusal('exact-ID removal/absence unproved')
    if artifact and c.hash_artifact(artifact,budget,elf=True)!=before:
        raise c.Refusal('receiver changed across retired operation')
    for name in ('work','home','tmp','no-external-plugins'):c.owned(operations/name,True)
    if verify_bundle(budget,review,review_sha,current,mode)!=bundle:
        raise c.Refusal('source custody changed')
    value={'schema':'workflow.GO5932.exact-runtime-controller.v1','complete':True,'accepted':False,
        'identity':current,'workflow_ref':WORKFLOW_REF,'invocation_id':token,'mode':mode,
        'source_manifest_sha256':bundle,'worker_native_exit':native,
        'worker_receipt_sha256':hashlib.sha256(raw).hexdigest(),
        'native_controls_sha256':worker['native_controls_sha256'],
        'receiver_receipt_sha256':receiver_sha if artifact else None,
        'artifact':before,'lifecycle':manager.lifecycle_evidence(),
        'retirement':retired,'finding_disposition':'GO-2026-5932 remains HOLD',
        'provisional_until_actual_controller_step_success':True}
    c.seal(output/'controller-proof.json',value,budget)
    budget.left()
    return value
def main():
    if (len(sys.argv)!=10 or sys.argv[9]!='--prepare-runtime' or
        not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode):
        raise c.Refusal('fixed isolated launcher invocation required')
    output,mode,review,review_sha,receiver,receiver_sha,token,receiver_outcome=sys.argv[1:9]
    run(output,mode,review,review_sha,receiver,receiver_sha,token,c.identity(),receiver_outcome=receiver_outcome)
if __name__=='__main__':
    try:main()
    except BaseException:os._exit(2)
    os._exit(0)
