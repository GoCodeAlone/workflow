"""Prepare an exact receiver from an already delivered local file. No network."""
import hashlib
import os
from pathlib import Path
import re
import sys

HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import candidate_contract as c

def stage(incoming,output,transport,transport_sha,token,current,budget):
    if not re.fullmatch('[0-9a-f]{64}',token):raise c.Refusal('fresh receive invocation required')
    receipt=c.read_receipt(transport,transport_sha,budget)
    expected={'schema':'workflow.GO5932.authenticated-candidate-delivery.v1',
        'complete':True,'accepted':False,'candidate_execution':False,
        'identity':current,'artifact_sha256':c.SHA,'artifact_bytes':c.SIZE,'source_commit':c.SOURCE}
    if any(receipt.get(k)!=v for k,v in expected.items()):
        raise c.Refusal('trusted authenticated-delivery receipt differs')
    output=c.owned(output,True);incoming=Path(incoming)
    original=c.hash_artifact(incoming,budget,mode=0o400)
    folder=output/'candidate';folder.mkdir(mode=0o700)
    target=folder/'wfctl'
    fd=os.open(incoming,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        before=os.fstat(fd)
        if before.st_ino!=original['inode'] or before.st_dev!=original['device']:
            raise c.Refusal('delivered object changed before copy')
        h=hashlib.sha256();total=0
        with target.open('xb') as dest:
            os.fchmod(dest.fileno(),0o600)
            while True:
                budget.left();block=os.read(fd,1048576)
                if not block:break
                total+=len(block)
                if total>c.SIZE:raise c.Refusal('receiver copy overflow')
                h.update(block);dest.write(block)
            if total!=c.SIZE or h.hexdigest()!=c.SHA or c.fingerprint(os.fstat(fd))!=c.fingerprint(before):
                raise c.Refusal('delivered bytes changed during receiver copy')
            os.fchmod(dest.fileno(),0o500);dest.flush();os.fsync(dest.fileno())
    finally:os.close(fd)
    received=c.hash_artifact(target,budget,elf=True)
    value={'schema':'workflow.GO5932.exact-candidate-receiver.v1','complete':True,
        'accepted':False,'candidate_execution':False,'identity':current,
        'invocation_id':token,'source_commit':c.SOURCE,'source_artifact':original,
        'receiver_artifact':received,'transport_receipt_sha256':transport_sha,
        'provisional_until_actual_step_success':True}
    c.seal(output/'receiver-proof.json',value,budget)
    return value

def main():
    if (len(sys.argv)!=7 or sys.argv[6]!='--prepare-receiver' or
        not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode):
        raise c.Refusal('fixed isolated receiver invocation required')
    incoming,output,transport,sha,token=sys.argv[1:6]
    stage(Path(incoming),Path(output),Path(transport),sha,token,c.identity(),c.Budget(30))
if __name__=='__main__':
    try:main()
    except BaseException:os._exit(2)
    os._exit(0)
