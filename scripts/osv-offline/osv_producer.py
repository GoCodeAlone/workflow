"""Inert trusted producer recipe. This module never fetches or uploads data.

Standalone Workflow source experiments have separate explicit authorization.
Permanent Workflow installation still needs separate permission, fresh public
database receipts, and a trusted hosted runner boundary.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import stat
import subprocess
import sys
import tempfile
from osv_completion import IncompleteScan, parse_analysis, read_report, validate
from osv_trusted import CONFIG, GO_VERSION, catalog, database, strict_json, check_database_applicability
from osv_io import bounded_run, Budget, STDOUT_LIMIT

PINS = {
    ('Darwin','arm64'): {'osv-scanner':'c770f6e98fe247f74aade57b2d8fdde25a79b582e6f00981414ef0da8be8d140',
        'govulncheck':'660e2b83df56694f181a8b7c80b2d5f666c92d1f037368cd0f0d2f9adbdb8379'},
    ('Linux','x86_64'): {'osv-scanner':'c810df66004c75f734eb4502ea5ff44115e4e3444e18d8b38bd87ff33094d69d',
        'govulncheck':'55425f57ab2501c257efd0fe4abe361974849fab3e54dd0e8f04d4d3c57a2d65'}
}


def fingerprint(root, budget=None):
    """Freeze all source bytes/modes, including ignored input, without following links."""
    result={}; total=0; entries=0; pending=[root]
    while pending:
        directory=pending.pop()
        with os.scandir(directory) as scanned:
            for entry in scanned:
                if entry.name=='.git':continue
                if budget is not None:budget.left()
                entries+=1
                if entries>20000:raise IncompleteScan('source entry limit exceeded')
                path=Path(entry.path);info=entry.stat(follow_symlinks=False);mode=info.st_mode
                if stat.S_ISLNK(mode):raise IncompleteScan('source symlink outside reviewed contract')
                if stat.S_ISDIR(mode):pending.append(path);continue
                if not stat.S_ISREG(mode):raise IncompleteScan('nonregular source input')
                size=info.st_size;total+=size
                if size>32*1024*1024 or total>1024*1024*1024 or len(result)>=10000:
                    raise IncompleteScan('source inventory exceeds bounded contract')
                result[str(path.relative_to(root))]=[stat.S_IMODE(mode),hashlib.sha256(path.read_bytes()).hexdigest()]
    if not result:raise IncompleteScan('empty source root')
    return result


def protocol(text):
    decoder=json.JSONDecoder(object_pairs_hook=lambda pairs: strict_json(json.dumps(dict(pairs))))
    # Exact duplicate-key and NaN checks are applied to every raw record separately.
    result=[];remaining=text
    while remaining.strip():
        _,end=decoder.raw_decode(remaining.lstrip())
        raw=remaining.lstrip()[:end];result.append(strict_json(raw));remaining=remaining.lstrip()[end:]
    return result


def execute(argv, cwd, env, out, label, timeout=240, budget=None):
    """Fresh producer-owned stdout/stderr/rc, never candidate supplied files."""
    try:
        run=bounded_run(argv,cwd,env,timeout,budget)
    except (OSError,subprocess.TimeoutExpired) as error:
        raise IncompleteScan('producer process unavailable or timed out') from error
    for suffix,text in [('stdout',run.stdout),('stderr',run.stderr),('rc',str(run.returncode)+'\n')]:
        path=out/(label+'.'+suffix);path.write_text(text);path.chmod(0o600)
    return run


def source_contract(root, go_binary, env, out, label, budget):
    listed=execute([str(go_binary),'list','-deps','-json','./...'],root,env,out,label+'-source-catalog',budget=budget)
    if listed.returncode or listed.stderr:
        raise IncompleteScan('independent source/package loading failed')
    modules={}; roots=[]
    for package in protocol(listed.stdout):
        if package.get('Error') or package.get('DepsErrors'):
            raise IncompleteScan('independent source/package error')
        if not package.get('DepOnly'): roots.append(package['ImportPath'])
        mod=package.get('Module')
        if mod:
            if not isinstance(mod.get('Path'),str) or not mod['Path']:
                raise IncompleteScan('invalid source module identity')
            path=mod['Path'];modules[path]=mod
            replacement=mod.get('Replace')
            if replacement:
                if not isinstance(replacement.get('Path'),str) or not replacement['Path']:
                    raise IncompleteScan('invalid source replacement identity')
                modules[replacement['Path']]=replacement
    if not roots or not modules: raise IncompleteScan('empty source/module analysis contract')
    versions=[]
    for mod in modules.values():
        effective=mod.get('Replace',mod)
        versions.append((effective['Path'],effective.get('Version','')))
    versions.append(('stdlib','v'+GO_VERSION))
    return roots,versions,set(modules)


def run_one(root, paths, tools, go_binary, db_base, receipts, now, output_parent, env, *, admitted_supplier=None):
    """Execute only after the caller verifies authority for this exact source root.

    No command-line entrypoint intentionally: the approved profile supplies the
    exact paths, receipts, compiled assets, sanitized environment and source.
    """
    if sys.version_info<(3,11):raise IncompleteScan('trusted producer requires Python >=3.11')
    root=Path(root).resolve();parent=Path(output_parent).resolve();tools=Path(tools).resolve()
    if parent.is_relative_to(root) or not parent.is_dir() or parent.is_symlink():
        raise IncompleteScan('output parent must be trusted and outside source')
    pins=PINS.get((platform.system(),platform.machine()))
    if admitted_supplier is not None:
        if (platform.system(),platform.machine())!=('Linux','x86_64'):
            raise IncompleteScan('hosted supplier admission requires native Linux/amd64')
        from osv_supplier import verify_delivery
        verified=verify_delivery(admitted_supplier)
        pins={name:verified[name] for name in ('osv-scanner','govulncheck')}
    if not pins: raise IncompleteScan('unreviewed producer platform')
    for name,digest in pins.items():
        path=tools/name
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=digest:
            raise IncompleteScan('supplier binary pin mismatch')
    required={'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOFLAGS':'-mod=readonly',
              'CGO_ENABLED':'0','GOVCS':'*:off','GOPRIVATE':'','GONOPROXY':'none','GONOSUMDB':'none'}
    if any(env.get(k)!=v for k,v in required.items()) or env['PATH'].split(os.pathsep)[0]!=str(Path(go_binary).parent):
        raise IncompleteScan('unbound compiler/network environment')
    budget=Budget()
    version=bounded_run([str(go_binary),'version'],root,env,30,budget)
    if version.returncode or version.stderr or not version.stdout.startswith('go version go'+GO_VERSION+' '):
        raise IncompleteScan('actual analysis compiler mismatch')
    before=fingerprint(root,budget)
    expected,manifests=catalog(root,paths,go_binary,env,budget)
    out=Path(tempfile.mkdtemp(prefix='osv-proof-',dir=parent));out.chmod(0o700)
    config=out/'trusted.toml';config.write_text(CONFIG);config.chmod(0o600)
    db_identity={};advisories={}
    for ecosystem in sorted({key[0] for packages in expected.values() for key in packages}):
        receipt=receipts[ecosystem];archive=Path(db_base)/'osv-scalibr'/ecosystem/'all.zip'
        digest=receipt['sha256'];advisories[ecosystem]=database(archive,ecosystem,digest,receipt,now,budget)
        db_identity[ecosystem]=digest
    check_database_applicability(advisories,expected)
    flat=out/'go-db';flat.mkdir(mode=0o700)
    for aid,a in advisories.get('Go',{}).items():
        path=flat/(aid+'.json');path.write_text(json.dumps(a));path.chmod(0o600)
    analysis={}
    for n,(path,packages) in enumerate(expected.items()):
        if not any(k[0]=='Go' for k in packages): continue
        source=Path(path).parent;module=manifests[str(Path(path).relative_to(root))]['module']
        roots,versions,source_names=source_contract(source,go_binary,env,out,'root'+str(n),budget)
        manifest=manifests[str(Path(path).relative_to(root))]
        identity_names=source_names | set(manifest['module_names']) | {name for ecosystem,name,version in packages if ecosystem=='Go'}
        independent=execute([str(tools/'govulncheck'),'-db',flat.as_uri(),'-C',str(source),'-json','./...'],
                            source,env,out,'root'+str(n)+'-govulncheck',budget=budget)
        analysis[path]=parse_analysis(independent.stdout,independent.returncode,independent.stderr,
            source,module,GO_VERSION,flat.as_uri(),advisories['Go'],versions,roots,identity_names)
    report=out/'results.json'
    argv=[str(tools/'osv-scanner'),'scan','source','--offline','--local-db-path='+str(Path(db_base).resolve()),
          '--no-resolve','--config='+str(config),'--all-packages','--call-analysis=go','--format=json']
    for path in expected: argv.extend(['-L',path])
    scanned=execute(argv,root,env,out,'scanner',timeout=720,budget=budget)
    report.write_text(scanned.stdout);report.chmod(0o600)
    local_count=sum(len(m.get('local_references',[])) for m in manifests.values())
    blocking=validate(read_report(report),expected,scanned.returncode,scanned.stderr,analysis,local_count,budget)
    if fingerprint(root,budget)!=before or config.read_text()!=CONFIG:
        raise IncompleteScan('source or trusted configuration changed during proof')
    for ecosystem,digest in db_identity.items():
        if hashlib.sha256((Path(db_base)/'osv-scalibr'/ecosystem/'all.zip').read_bytes()).hexdigest()!=digest:
            raise IncompleteScan('database changed during proof')
    for name,digest in pins.items():
        if hashlib.sha256((tools/name).read_bytes()).hexdigest()!=digest:
            raise IncompleteScan('supplier changed during proof')
    receipt={'source':before,'manifests':manifests,'databases':db_identity,'compiler':version.stdout.strip(),
             'supplier_pins':pins,'blocking':sorted(blocking),'complete':True,'execution':'local',
             'unverified_called_observations':{str(Path(path).relative_to(root)):[
                 {'module':name,'advisory_id':aid,'function_trace_count':count,
                  'classification':'no raw Go symbol metadata; not independent version applicability proof'}
                 for name,aid,count in evidence.unverified_called] for path,evidence in analysis.items()}}
    encoded=json.dumps(receipt,sort_keys=True,indent=2)+'\n'
    if len(encoded.encode())>STDOUT_LIMIT:raise IncompleteScan('producer receipt size limit exceeded')
    budget.charge(len(encoded.encode()));budget.left()
    path=out/'receipt.json';path.write_text(encoded);path.chmod(0o600)
    budget.left()
    return out,blocking

