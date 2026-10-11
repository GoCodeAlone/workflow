"""Independent supplier source/build/digest joins for each actual hosted run."""
import hashlib
import json
from pathlib import Path
import re
import stat

def verify_delivery(contract):
    path=Path(contract['delivery_receipt_path']);info=path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size>16*1024*1024:
        raise RuntimeError('supplier delivery receipt shape differs')
    raw=path.read_bytes()
    if hashlib.sha256(raw).hexdigest()!=contract['delivery_receipt_sha256']:
        raise RuntimeError('supplier delivery receipt digest differs')
    def pairs(rows):
        result={}
        for key,value in rows:
            if key in result:raise RuntimeError('duplicate supplier delivery field')
            result[key]=value
        return result
    receipt=json.loads(raw,object_pairs_hook=pairs)
    if (receipt.get('schema')!='workflow.offline-scanner.supplier-delivery.v1' or
        receipt.get('accepted') is not False or receipt.get('complete') is not True or
        receipt.get('supplied') is not True or receipt.get('identity')!=contract['identity'] or
        receipt.get('supplier_script_sha256')!=contract['script_sha256'] or
        receipt.get('binary_pins')!=contract['pins']):
        raise RuntimeError('supplier delivery does not join this exact source/run/pins')
    source=receipt['scanner_source']
    expected={'module':'github.com/google/osv-scanner/v2','version':'v2.6.0',
        'h1':'h1:hqtNLANWaKcqOC6znJX8J/23PtzV82RX23oMS9/ab4I=',
        'go_mod_h1':'h1:3XUKJWYB7D2fWvFDMTZg9OBrlCEf/WBGZVc4IFjTljs=',
        'origin_commit':'e840a6e8adb14b7777c78e26cfbf6e2abc1d1fc6'}
    if any(source.get(key)!=value for key,value in expected.items()):
        raise RuntimeError('supplier authenticated upstream identity differs')
    expected_patch={'changed':['go.mod','go.sum'],'extra':[],'missing':[],
        'upstream_sha256':{'go.mod':'47db8d4f9e4b49922097899d76692a37a84e29143f439cc47fdf925b4feab615',
            'go.sum':'d8310b422c284f42751042d5d66ca661981f5c855dd2e19e1173090f7fcc2989'},
        'patched_sha256':{'go.mod':'36ab7af2571e78311029b84b4577c917de4cb4610055561694fb24b9f96b7569',
            'go.sum':'d89ac8ea219e18828f9f01346111e5ab36c71b3556c1739d158e97e5eaa566e0'}}
    if receipt.get('patch')!=expected_patch:
        raise RuntimeError('supplier source patch conservation differs')
    tools=receipt['toolchain']
    if tools.get('version')!='go1.27.2' or tools['tool_sha256'].get('go')!=contract['compiler_sha256']:
        raise RuntimeError('supplier actual compiler identity differs')
    if tools.get('goroot')!=contract['compiler_root'] or receipt.get('cache_paths')!={
        'GOPATH':contract['existing_gopath'],'GOMODCACHE':contract['existing_module_cache'],'GOCACHE':contract['existing_build_cache']}:
        raise RuntimeError('supplier actual compiler/cache paths differ')
    pins=contract['pins']
    if set(pins)!={'osv-scanner','osv-reporter','govulncheck'} or any(not re.fullmatch('[0-9a-f]{64}',str(value)) for value in pins.values()):
        raise RuntimeError('supplier admitted pin inventory differs')
    for name,value in pins.items():
        if receipt['binaries'][name]['sha256']!=value:
            raise RuntimeError('supplier recorded ELF pin differs')
        binary=Path(contract['supplier_root'])/name
        if not stat.S_ISREG(binary.lstat().st_mode) or hashlib.sha256(binary.read_bytes()).hexdigest()!=value:
            raise RuntimeError('supplier actual ELF pin differs')
    if not receipt['commands'] or any(row.get('exit')!=0 or row.get('complete') is not True for row in receipt['commands']):
        raise RuntimeError('supplier actual native command is incomplete')
    return dict(pins)
