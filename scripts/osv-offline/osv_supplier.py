"""Independent supplier source/build/digest joins for each actual hosted run."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import stat

METADATA_COUNT = 399
METADATA_SHA256 = '86669b0754b3ec99dfa7bfd2db3ecdfcad5fc2915d39528c195f0173bbf96f79'

def verify_metadata(receipt, cache):
    metadata=receipt.get('metadata_only', {})
    rows=metadata.get('records', [])
    if (metadata.get('complete') is not True or metadata.get('conserved') is not True or
        metadata.get('catalog_count')!=METADATA_COUNT or metadata.get('catalog_sha256')!=METADATA_SHA256 or
        not isinstance(rows,list) or len(rows)!=METADATA_COUNT):
        raise RuntimeError('supplier metadata completion/catalog differs')
    catalog=[]
    for row in rows:
        module=row['module'];version=row['version'];digest=row['go_mod_h1']
        if (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._~!/+\-]{0,250}', module) or
            not all(p and p not in {'.','..'} for p in module.split('/')) or
            not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.\-]+)?(?:\+incompatible)?',version) or
            not re.fullmatch(r'h1:[A-Za-z0-9+/]{43}=',digest) or
            not re.fullmatch(r'[0-9a-f]{64}',row['go_mod_sha256']) or
            type(row['bytes']) is not int or not 0<row['bytes']<=1024*1024 or
            row['origin'] not in {'authenticated-existing-cache','fixed-public-proxy-mod-only'}):
            raise RuntimeError('supplier metadata record differs')
        catalog.append([module,version,digest])
    raw=json.dumps(sorted(catalog),separators=(',',':'),ensure_ascii=True).encode()
    if (len({tuple(row[:2]) for row in catalog})!=METADATA_COUNT or
        hashlib.sha256(raw).hexdigest()!=METADATA_SHA256):
        raise RuntimeError('supplier authenticated metadata identities differ')
    for row in rows:
        escape=lambda value: ''.join('!'+c.lower() if 'A'<=c<='Z' else c for c in value)
        path=Path(cache)/'cache/download'/escape(row['module'])/'@v'/(escape(row['version'])+'.mod')
        if path!=path.resolve(strict=True):
            raise RuntimeError('supplier metadata cache path differs')
        with os.fdopen(os.open(path,os.O_RDONLY|os.O_NOFOLLOW),'rb') as f:
            before=os.fstat(f.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_size!=row['bytes']:
                raise RuntimeError('supplier actual metadata size/type differs')
            data=f.read(1024*1024+1);after=os.fstat(f.fileno())
        full_hash=hashlib.sha256(data).hexdigest()
        go_hash='h1:'+base64.b64encode(hashlib.sha256((full_hash+'  go.mod\n').encode()).digest()).decode()
        if (len(data)!=row['bytes'] or full_hash!=row['go_mod_sha256'] or go_hash!=row['go_mod_h1'] or
            (before.st_size,before.st_mtime_ns,before.st_ctime_ns)!=(after.st_size,after.st_mtime_ns,after.st_ctime_ns)):
            raise RuntimeError('supplier actual metadata changed')

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
    verify_metadata(receipt,contract['existing_module_cache'])
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
