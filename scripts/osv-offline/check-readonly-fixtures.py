"""Sequential hosted checks of the committed readonly conformance fixtures."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
FIXTURES = ('iac-pass','no-iac')

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def run(output, go, module_cache, build_cache, gopath):
    if (os.environ.get('GITHUB_JOB') != 'osv-production-proof' or
        os.environ.get('GITHUB_EVENT_NAME') != 'workflow_dispatch' or
        os.environ.get('GITHUB_REPOSITORY') != 'GoCodeAlone/workflow' or
        os.environ.get('GITHUB_REF') != 'refs/heads/prep/osv-prerequisites-20261011-r1'):
        raise RuntimeError('exact finite fixture proof job required')
    info=output.lstat()
    if output.parent != Path(os.environ['RUNNER_TEMP']) or output.resolve()!=output or not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode)!=0o700 or info.st_uid!=os.getuid() or list(output.iterdir()):
        raise RuntimeError('fresh owned output required')
    for path in (go,module_cache,build_cache,gopath):
        if not path.is_absolute() or path.resolve()!=path or not path.exists():raise RuntimeError('existing canonical compiler/cache required')
    spec=importlib.util.spec_from_file_location('manager',HERE/'run.py')
    manager=importlib.util.module_from_spec(spec);spec.loader.exec_module(manager)
    _,_,bounded,Budget=manager.trusted()
    budget=Budget(600)
    home=output/'home';home.mkdir(mode=0o700)
    temporary=output/'tmp';temporary.mkdir(mode=0o700)
    env={'PATH':str(go.parent)+':/usr/bin:/bin','HOME':str(home),'TMPDIR':str(temporary),
        'LANG':'C','LC_ALL':'C','GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off',
        'GOVCS':'*:off','GOPRIVATE':'','GONOPROXY':'none','GONOSUMDB':'none','GOFLAGS':'-mod=readonly',
        'CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','GOMAXPROCS':'2','GOMEMLIMIT':'768MiB',
        'GOPATH':str(gopath),'GOMODCACHE':str(module_cache),'GOCACHE':str(build_cache)}
    paths=[ROOT/'go.mod',ROOT/'go.sum',*[ROOT/'cmd/wfctl/testdata/conformance'/name/file for name in FIXTURES for file in ('go.mod','go.sum')]]
    before={str(path.relative_to(ROOT)):sha(path) for path in paths}
    result={'complete':False,'accepted':False,'identity':{key:os.environ[key] for key in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')},
        'compiler_sha256':sha(go),'before':before,'checks':[],'scope':'committed fixture readonly load/tidy/tests, then focused current trusted-source conformance tests'}
    code=2
    try:
        commands=[]
        for name in FIXTURES:
            cwd=ROOT/'cmd/wfctl/testdata/conformance'/name
            commands += [(name+'-load',cwd,['list','-mod=readonly','-deps','./...']),
                (name+'-tidy',cwd,['mod','tidy','-diff']), (name+'-test',cwd,['test','-mod=readonly','-p=2','./...'])]
        commands.append(('root-conformance',ROOT,['test','-mod=readonly','-p=2','./cmd/wfctl','-run',
            '^(TestPluginConformanceLocalJSONPass|TestPluginConformanceNoTypedIaCServiceFails)$','-count=1']))
        for name,cwd,args in commands:
            actual=bounded([str(go),*args],cwd,env,min(240,budget.left()),budget)
            for suffix,text in (('stdout',actual.stdout),('stderr',actual.stderr)):
                with (output/(name+'.'+suffix)).open('xb') as f:
                    os.fchmod(f.fileno(),0o600);f.write(text.encode());f.flush();os.fsync(f.fileno())
            result['checks'].append({'name':name,'argv':[str(go),*args],'exit':actual.returncode,
                'stdout_sha256':sha(output/(name+'.stdout')),'stderr_sha256':sha(output/(name+'.stderr')),
                'tidy_empty':not actual.stdout and not actual.stderr if name.endswith('-tidy') else None})
            budget.left()
        after={str(path.relative_to(ROOT)):sha(path) for path in paths}
        result['after']=after
        result['complete']=before==after and all(row['exit']==0 and row['tidy_empty'] is not False for row in result['checks'])
        manager.trusted();budget.left()
        code=0 if result['complete'] else 2
    except Exception as error:
        result['error_type']=type(error).__name__
        result['complete']=False
    manager.write_completion(output/'readonly-fixture-proof.json',result,budget)
    return code

if __name__=='__main__':
    if len(sys.argv)!=7 or sys.argv[6]!='--execute':raise SystemExit(2)
    raise SystemExit(run(*[Path(value) for value in sys.argv[1:6]]))
