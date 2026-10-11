"""Finite dispatch evidence collector; cannot admit the automatic scanner."""
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent
IDENTITY = ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')

def manager():
    manifest_path=HERE/'bootstrap-manifest.json'
    if not stat.S_ISREG(manifest_path.lstat().st_mode) or manifest_path.stat().st_size>1024*1024:
        raise RuntimeError('regular bounded bootstrap manifest required before import')
    manifest=json.loads(manifest_path.read_bytes())
    source=HERE/'run.py'
    if (not stat.S_ISREG(source.lstat().st_mode) or source.stat().st_size>1024*1024 or
        hashlib.sha256(source.read_bytes()).hexdigest()!=manifest['trusted_files']['run.py']):
        raise RuntimeError('trusted manager source differs before import')
    spec = importlib.util.spec_from_file_location('scanner_manager', HERE / 'run.py')
    module = importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

def run(output, supplier_root, compiler_root, gopath, module_cache, build_cache):
    m = manager();manifest, profile, bounded, Budget = m.trusted()
    plan = m.load(HERE / 'manual-proof-plan.json')
    info = output.lstat();parent = Path(os.environ['RUNNER_TEMP'])
    if (output.parent != parent or output.resolve() != output or not output.name.startswith('osv-production-') or
        not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode)!=0o700 or list(output.iterdir())):
        raise m.Refusal('fresh exclusively owned finite proof output required')
    supplier_proof = m.load(supplier_root.parent / 'supplier-delivery-proof.json')
    if (supplier_proof.get('accepted') is not False or supplier_proof.get('complete') is not True or
        supplier_proof.get('identity') != {key: os.environ[key] for key in IDENTITY}):
        raise m.Refusal('actual same-run supplier delivery proof required')
    pins = {name: m.digest(supplier_root/name) for name in ('osv-scanner','osv-reporter','govulncheck')}
    if supplier_proof['binary_pins'] != pins:
        raise m.Refusal('delivered supplier bytes differ from actual build proof')
    runtime = {'accepted': False, 'identity': {key: os.environ[key] for key in IDENTITY},
        'core_contract_sha256': manifest['core_contract_sha256'],
        'compiler_root': str(compiler_root), 'compiler_sha256': m.digest(compiler_root/'bin/go'),
        'supplier_root': str(supplier_root), 'supplier_pins': pins,
        'supplier_delivery_proof_sha256': m.digest(supplier_root.parent/'supplier-delivery-proof.json'),
        'supplier_delivery_proof_path': str(supplier_root.parent/'supplier-delivery-proof.json'),
        'existing_gopath': str(gopath), 'existing_module_cache': str(module_cache), 'existing_build_cache': str(build_cache)}
    m.write(output/'runtime.json',runtime)
    m.manual_admission(manifest,output/'runtime.json')
    deadline = Budget(plan['total_seconds'])
    result = {'schema': 'workflow.offline-scanner.finite-production-proof.v1',
        'accepted': False, 'installation_admitted': False, 'normal_event_delivery_proved': False,
        'candidate_execution': False, 'finding_policy_changed': False,
        'identity': runtime['identity'], 'core_contract_sha256': manifest['core_contract_sha256'],
        'bootstrap_manifest_sha256': m.digest(HERE/'bootstrap-manifest.json'),
        'runtime_sha256': m.digest(output/'runtime.json'), 'plan_sha256': m.digest(HERE/'manual-proof-plan.json'),
        'source_refs': plan['source_refs'], 'phases': [], 'complete': False}
    child_env = {key: os.environ[key] for key in (*IDENTITY,'GITHUB_REF','GITHUB_EVENT_NAME','GITHUB_WORKFLOW_REF',
        'RUNNER_ENVIRONMENT','RUNNER_TEMP','GITHUB_OUTPUT')}
    child_env.update(PATH='/usr/bin:/bin',LANG='C',LC_ALL='C',HOME='/nonexistent')
    successes = {}
    try:
        for phase in plan['phases']:
            if phase.startswith(('report-','gate-')) and not all(successes.get(label) for label in ('baseline','candidate')):
                result['phases'].append({'phase': phase, 'executed': False, 'reason': 'required actual scans are incomplete'})
                continue
            startup = Path(tempfile.mkdtemp(prefix='osv-python-',dir=parent));startup.chmod(0o700)
            started = time.monotonic()
            args = [sys.executable,'-I','-S','-B','-X','pycache_prefix='+str(startup/'absent-cache'),
                str(HERE/'run.py'),'--manual-proof',phase,str(output/'runtime.json')]
            seconds = plan['prepare_seconds'] if phase.startswith('prepare-') else (
                plan['report_seconds'] if phase.startswith('report-') else plan['scan_seconds'] if phase.startswith('scan-') else 60)
            available=deadline.left()-120
            if available<=0:raise m.Refusal('finite collector reserves final owned cleanup')
            actual = bounded(args,HERE,child_env,min(seconds+30,available),deadline)
            for suffix, raw in (('stdout',actual.stdout),('stderr',actual.stderr)):
                with (output/(phase+'.'+suffix)).open('xb') as f:
                    os.fchmod(f.fileno(),0o600);f.write(raw.encode());f.flush();os.fsync(f.fileno())
            row = {'phase': phase, 'executed': True, 'exit': actual.returncode,
                'elapsed_seconds': round(time.monotonic()-started,3),
                'stdout_sha256': m.digest(output/(phase+'.stdout')), 'stderr_sha256': m.digest(output/(phase+'.stderr'))}
            result['phases'].append(row)
            if startup.exists():
                m.owned(startup);startup.rmdir()
                row['startup_cleanup'] = True
            else:
                row['startup_cleanup'] = True
            m.trusted();deadline.left()
            if phase.startswith('prepare-') and actual.returncode != 0:break
            if phase.startswith('scan-'):
                label = m.scan_label(phase)
                successes[label] = actual.returncode == 0
                if actual.returncode == 0:
                    current = m.load(m.state_path()/(label+'-current.json'))
                    child_env[label.upper()+'_SCAN_TOKEN'] = current['token']
                    child_env[label.upper()+'_SCAN_OUTCOME'] = 'success'
            if phase.startswith(('report-','gate-')) and actual.returncode not in (0,1):break
    except Exception as error:
        result['error'] = {'type':type(error).__name__, 'reason':str(error)[:512] if isinstance(error,m.Refusal) else 'finite proof failed closed'}
    finally:
        state = m.state_path()
        try:
            if state.exists():
                m.owned(state)
                m.close_all_containers(state,runtime,deadline.deadline)
            result['retirement_verified'] = True
        except Exception as error:
            result['retirement_verified'] = False
            result['retirement_error'] = {'type':type(error).__name__, 'reason':'owned full-ID retirement remains unproved'}
    try:
        if result['retirement_verified'] is not True:
            raise m.Refusal('finite proof publication limited to fixed failure metadata')
        if state.exists():
            m.require_retired_containers(state)
            if (state/'prepared.json').exists():m.verify_prepared_inputs(state,manifest,runtime)
            for private in sorted(state.glob('worker-*')):
                m.owned(private)
                target = output/private.name;target.mkdir(mode=0o700)
                for name in ('container-lifecycle.json','runtime-witness.json','worker-result.json','container-recovery.json'):
                    source = private/name
                    if source.exists():
                        m.regular(source);shutil.copyfile(source,target/name);(target/name).chmod(0o600)
            for name in ('prepared.json','public-database-receipts.json'):
                source = state/name
                if source.exists():
                    m.regular(source);shutil.copyfile(source,output/name);(output/name).chmod(0o600)
        evidence = m.REPOSITORY/'offline-evidence'
        if evidence.exists():
            total = 0
            for path in evidence.rglob('*'):
                if path.is_file():total += m.regular(path).st_size
            if total > 128*1024*1024:raise m.Refusal('finite raw evidence aggregate bound exceeded')
            shutil.copytree(evidence,output/'offline-evidence',symlinks=False)
        result['complete'] = 'error' not in result and all(successes.get(label) for label in ('baseline','candidate')) and len(result['phases'])==5 and all(
            row.get('executed') and row.get('exit') in (0,1) for row in result['phases'])
        result['raw_findings_preserved'] = True
        m.trusted();deadline.left()
    except Exception as error:
        result['publication_error'] = {'type':type(error).__name__, 'reason':str(error)[:512] if isinstance(error,m.Refusal) else 'finite proof publication failed closed'}
        result['complete'] = False
    m.write_completion(output/'production-proof.json',result,deadline)
    return 0 if result['complete'] else 2

def interrupted(signum, frame):raise RuntimeError('finite production proof interrupted')

if __name__=='__main__':
    signal.signal(signal.SIGTERM,interrupted)
    if len(sys.argv)!=8 or sys.argv[7]!='--execute':raise SystemExit(2)
    raise SystemExit(run(*[Path(raw) for raw in sys.argv[1:7]]))
