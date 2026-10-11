"""Local hosted replacement candidate; not installed or execution-reviewed.

Only the base-owned bootstrap invokes public phases. All admission fields remain
HOLD until actual Linux/supplier/first-install evidence is reviewed and bound.
No application tests, binaries or npm scripts are executed by this manager.
"""
from datetime import datetime, timezone
import errno
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import resource
import secrets
import selectors
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

HERE = Path(__file__).resolve().parent
REPOSITORY = HERE.parents[1]
PHASES = {
    'prepare-scheduled', 'prepare-differential', 'scan-scheduled', 'scan-baseline',
    'scan-candidate', 'report-scheduled', 'report-differential',
    'gate-scheduled', 'gate-differential',
}
SHA = re.compile(r'^[0-9a-f]{64}$')
COMMIT = re.compile(r'^[0-9a-f]{40}$')
MAX_JSON = 16 * 1024 * 1024
TRUSTED_FILES = {'run.py', 'osv_producer.py', 'osv_trusted.py', 'osv_completion.py',
    'osv_io.py', 'scanner-profile.json', 'osv_container.py', 'container-worker.py',
    'container-seccomp.json', 'manual-proof-plan.json', 'run-production-proof.py', 'prepare-supplier.py'}
TRUSTED_FILES.update({'osv_supplier.py', 'application-metadata-catalog.json'})
MANUAL_PHASES = {'prepare-differential', 'scan-baseline', 'scan-candidate', 'report-differential', 'gate-differential'}
CORE_FIELDS = {'trusted_files', 'linux_pins', 'public_preload_inventory_sha256',
    'prepared_source_refs', 'resource_contract', 'reviewed_protocol_binding_sha256',
    'container_image', 'container_image_config'}

class Refusal(RuntimeError):
    pass

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()

def regular(path, limit=MAX_JSON):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
        raise Refusal('untrusted file shape or size')
    return info

def strict_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refusal('duplicate JSON field')
        result[key] = value
    return result

def load(path):
    regular(path)
    return json.loads(path.read_text(), object_pairs_hook=strict_object,
        parse_constant=lambda _: (_ for _ in ()).throw(Refusal('nonfinite JSON')))

def owned(path):
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise Refusal('state directory must be regular exclusively owned mode0700')
    return path

def write(path, value):
    data = (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()
    if len(data) > MAX_JSON:
        raise Refusal('receipt size limit')
    created = False
    try:
        f = path.open('xb')
        created = True
        with f:
            os.fchmod(f.fileno(), 0o600)
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
    except Exception:
        if created:
            path.unlink(missing_ok=True)
        raise

def trusted():
    manifest = load(HERE / 'bootstrap-manifest.json')
    if manifest.get('version') != 2:
        raise Refusal('unsupported bootstrap manifest')
    for name, expected in manifest['trusted_files'].items():
        if name not in TRUSTED_FILES:
            raise Refusal('unknown trusted source path')
        regular(HERE / name)
        if not SHA.fullmatch(expected) or digest(HERE / name) != expected:
            raise Refusal('reviewed source/profile hash mismatch')
    if set(manifest['trusted_files']) != TRUSTED_FILES:
        raise Refusal('incomplete trusted source inventory')
    if core_contract(manifest) != manifest.get('core_contract_sha256'):
        raise Refusal('immutable scanner core contract differs')
    inventory = HERE / 'public-module-preload-inventory.json'
    if digest(inventory) != manifest['public_preload_inventory_sha256']:
        raise Refusal('preload inventory hash mismatch')
    # Fresh absent pycache_prefix is established by the shell before interpreter
    # startup. Worker invocations establish their own fresh prefix as well.
    if not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode:
        raise Refusal('unbound Python startup flags')
    prefix = sys.pycache_prefix
    if not prefix or Path(prefix).exists() or Path(prefix).is_symlink():
        raise Refusal('Python cache prefix must remain fresh and absent')
    sys.path.insert(0, str(HERE))
    from osv_io import bounded_run, Budget
    return manifest, load(HERE / 'scanner-profile.json'), bounded_run, Budget

def core_contract(manifest):
    # Receipt hashes and their index are intentionally outside this immutable
    # code/runtime/profile contract, avoiding a self-referential receipt cycle.
    core = {key: manifest[key] for key in sorted(CORE_FIELDS)}
    return hashlib.sha256(json.dumps(core, sort_keys=True, separators=(',', ':')).encode()).hexdigest()

def validate_runtime(manifest, runtime):
    from osv_container import IMAGE, IMAGE_CONFIG, eligible
    eligible()
    if (manifest['container_image'] != IMAGE or manifest['container_image_config'] != IMAGE_CONFIG or
        runtime.get('core_contract_sha256') != manifest['core_contract_sha256']):
        raise Refusal('runtime belongs to another immutable container contract')
    paths = ['compiler_root', 'supplier_root', 'existing_gopath', 'existing_module_cache', 'existing_build_cache']
    for key in paths:
        path = Path(runtime[key])
        if not path.is_absolute() or not path.is_dir() or path.resolve() != path:
            raise Refusal('runtime requires exact existing canonical allocated paths')
    if set(runtime['supplier_pins']) != {'govulncheck', 'osv-scanner', 'osv-reporter'}:
        raise Refusal('runtime supplier inventory differs')
    if not all(SHA.fullmatch(str(value)) for value in runtime['supplier_pins'].values()):
        raise Refusal('runtime supplier digests are unbound')
    cache=Path(runtime['existing_build_cache'])
    for immutable in (HERE, Path(runtime['compiler_root']), Path(runtime['supplier_root']), Path(runtime['existing_module_cache'])):
        if cache==immutable or cache in immutable.parents or immutable in cache.parents:
            raise Refusal('existing build cache overlaps an immutable input tree')
    from osv_supplier import verify_delivery
    verify_delivery(supplier_contract(manifest, runtime))
    supplier(None, manifest, runtime)
    return runtime

def supplier_contract(manifest, runtime):
    return {'delivery_receipt_path': runtime['supplier_delivery_proof_path'],
        'delivery_receipt_sha256': runtime['supplier_delivery_proof_sha256'],
        'script_sha256': manifest['trusted_files']['prepare-supplier.py'],
        'identity': runtime['identity'], 'pins': runtime['supplier_pins'],
        'compiler_sha256': runtime['compiler_sha256'], 'supplier_root': runtime['supplier_root'],
        **{key:runtime[key] for key in ('compiler_root','existing_gopath','existing_module_cache','existing_build_cache')}}

def admission(manifest):
    if platform.system() != 'Linux' or platform.machine() != 'x86_64' or sys.version_info < (3, 11):
        raise Refusal('actual native Linux/amd64 Python>=3.11 required')
    if manifest['permanent_install_authorized'] is not True:
        raise Refusal('permanent automatic workflow installation remains unapproved')
    for field, name in [
        ('linux_runtime_acceptance_sha256', 'linux-hosted-acceptance.json'),
        ('supplier_delivery_receipt_sha256', 'supplier-delivery-receipt.json'),
        ('first_install_execution_receipt_sha256', 'first-install-execution-receipt.json'),
        ('hosted_job_timing_acceptance_sha256', 'hosted-job-timing-acceptance.json'),
        ('source_object_delivery_acceptance_sha256', 'source-object-delivery-acceptance.json'),
    ]:
        expected = manifest.get(field)
        if not isinstance(expected, str) or not SHA.fullmatch(expected):
            raise Refusal('reviewed ' + field + ' is unbound')
        path = HERE / name
        if digest(path) != expected:
            raise Refusal('admission receipt hash differs')
        receipt = load(path)
        if receipt.get('accepted') is not True:
            raise Refusal('admission receipt does not accept this exact runtime')
        if receipt.get('profile_sha256') != manifest['trusted_files']['scanner-profile.json']:
            raise Refusal('admission receipt belongs to another exact resource/profile')
        if receipt.get('core_contract_sha256') != manifest['core_contract_sha256']:
            raise Refusal('admission receipt belongs to another immutable code/runtime contract')
    delivery = load(HERE / 'source-object-delivery-acceptance.json')
    if (delivery.get('repository') != 'GoCodeAlone/workflow' or
        delivery.get('trusted_checkout_action') != 'actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10' or
        delivery.get('candidate_execution') is not False or
        set(delivery.get('events', {})) != {'pull_request','merge_group','push','schedule'}):
        raise Refusal('exact normal-event source object delivery admission missing')
    for event, evidence in delivery['events'].items():
        if not isinstance(evidence, dict) or evidence.get('accepted') is not True or not SHA.fullmatch(str(evidence.get('proof_sha256', ''))):
            raise Refusal('normal-event source object delivery lacks authentic bound proof')
    # This independently reviewed prerequisite never substitutes for each
    # actual event's exact rev-parse/tree/blob checks below. No dynamic fetch.
    timing = load(HERE / 'hosted-job-timing-acceptance.json')
    scan_seconds = load(HERE / 'scanner-profile.json')['resource_envelope']['total_seconds']
    for job, scans in [('scan-scheduled', 1), ('scan-pr', 2)]:
        row = timing['jobs'][job]
        numbers = [row[key] for key in ('job_timeout_seconds', 'prepare_total_seconds',
                                        'report_total_seconds', 'checkout_setup_upload_reserve_seconds')]
        if any(type(value) is not int or value <= 0 for value in numbers):
            raise Refusal('actual hosted job timing admission is incomplete')
        if row['report_total_seconds'] != 240 or sum(numbers[1:]) + scans * scan_seconds >= row['job_timeout_seconds']:
            raise Refusal('exact offline phases exceed witnessed hosted job budget')
    # Decisions bind constraints, while each fresh job supplies its own observed
    # paths and exact build receipt. Historical ephemeral paths never execute.
    path = Path(os.environ.get('OSV_RUNTIME_RECEIPT', '/nonexistent'))
    if path.parent.parent != Path(os.environ['RUNNER_TEMP']) or not path.parent.name.startswith('osv-runtime-'):
        raise Refusal('fresh same-job actual runtime receipt is missing')
    owned(path.parent)
    runtime = validate_runtime(manifest, load(path))
    if runtime.get('accepted') is not False or runtime.get('identity') != {
        key: os.environ[key] for key in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')}:
        raise Refusal('actual runtime does not belong to this fresh job')
    if runtime['supplier_pins'] != manifest['linux_pins']:
        raise Refusal('accepted runtime differs from permanent supplier pins')
    return runtime

def manual_admission(manifest, runtime_path):
    # Explicit finite evidence collection only. No accepted normal-event receipt
    # or automatic installation is minted by this path.
    plan = load(HERE / 'manual-proof-plan.json')
    expected = {'GITHUB_REPOSITORY': plan['repository'], 'GITHUB_REF': plan['ref'],
        'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_JOB': plan['job'],
        'GITHUB_WORKFLOW_REF': plan['workflow_ref']}
    if any(os.environ.get(key) != value for key, value in expected.items()):
        raise Refusal('finite manual proof identity differs from reviewed plan')
    if (not COMMIT.fullmatch(os.environ.get('GITHUB_SHA', '')) or
        not os.environ.get('GITHUB_RUN_ID', '').isdecimal() or
        not os.environ.get('GITHUB_RUN_ATTEMPT', '').isdecimal() or
        plan['source_refs'] != manifest['prepared_source_refs'] or
        set(plan['phases']) != MANUAL_PHASES):
        raise Refusal('finite manual proof inputs or run identity are unbound')
    path = Path(runtime_path)
    parent = Path(os.environ['RUNNER_TEMP'])
    if path.parent.parent != parent or not path.parent.name.startswith('osv-production-'):
        raise Refusal('finite proof runtime is outside its owned runner state')
    owned(path.parent)
    runtime = load(path)
    if runtime.get('accepted') is not False or runtime.get('identity') != {
        key: os.environ[key] for key in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')}:
        raise Refusal('finite proof runtime must remain provisional and exact-run bound')
    return validate_runtime(manifest, runtime)

def state_path():
    parent = Path(os.environ['RUNNER_TEMP'])
    if not parent.is_absolute() or parent.is_symlink() or not parent.is_dir():
        raise Refusal('invalid runner state parent')
    run, attempt, job = [os.environ[name] for name in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB')]
    if not run.isdecimal() or not attempt.isdecimal() or job not in {'scan-pr', 'scan-scheduled', 'osv-production-proof'}:
        raise Refusal('unbound GitHub execution identity')
    return parent / ('wf-osv-offline-' + run + '-' + attempt + '-' + job)

def env(go, state, runtime, *, online=False):
    result = {
        'PATH': str(go.parent) + ':/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C',
        'HOME': str(state / 'home'), 'TMPDIR': str(state / 'tmp'),
        'XDG_CONFIG_HOME': str(state / 'home/.config'),
        'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
        'GOPROXY': 'https://proxy.golang.org' if online else 'off',
        'GOSUMDB': 'sum.golang.org' if online else 'off',
        'GOFLAGS': '-mod=readonly', 'CGO_ENABLED': '0', 'GOVCS': '*:off',
        'GOPRIVATE': '', 'GONOPROXY': 'none', 'GONOSUMDB': 'none',
        'GOOS': 'linux', 'GOARCH': 'amd64', 'GOMAXPROCS': '2', 'GOMEMLIMIT': '768MiB',
        'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null',
        'GIT_CONFIG_NOSYSTEM': '1', 'GIT_TERMINAL_PROMPT': '0', 'GIT_ASKPASS': '/bin/false',
    }
    # Caches are existing allocated runner caches, never a new host private Go cache.
    result.update(GOPATH=runtime['existing_gopath'], GOMODCACHE=runtime['existing_module_cache'],
                  GOCACHE=runtime['existing_build_cache'])
    return result

def export_source(ref, label, state, child_env, bounded_run, Budget, prepare_budget):
    if not COMMIT.fullmatch(ref):
        raise Refusal('invalid exact source commit')
    budget = Budget(min(90, prepare_budget.left()))
    resolved = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'rev-parse', ref + '^{commit}'],
                           REPOSITORY, child_env, 30, budget)
    if resolved.returncode or resolved.stderr or resolved.stdout.strip() != ref:
        raise Refusal('exact source object absent; no dynamic fetch fallback')
    tree = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'ls-tree', '-r', '-z', ref],
                       REPOSITORY, child_env, 30, budget)
    if tree.returncode or tree.stderr:
        raise Refusal('Git source inventory failed')
    expected = {}
    for row in tree.stdout.split('\0'):
        if not row:
            continue
        meta, name = row.split('\t', 1)
        mode, kind, blob = meta.split()
        rel = PurePosixPath(name)
        if mode not in {'100644', '100755'} or kind != 'blob' or not COMMIT.fullmatch(blob) or rel.is_absolute() or '..' in rel.parts:
            raise Refusal('unsupported Git object/path')
        expected[name] = (mode, blob)
    if not expected or len(expected) > 10000:
        raise Refusal('source file limit')
    archive, root = state / (label + '.tar'), state / (label + '-source')
    root.mkdir(mode=0o700)
    result = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'archive', '--format=tar', '--output=' + str(archive), ref],
                         REPOSITORY, child_env, 30, budget)
    if result.returncode or result.stderr:
        raise Refusal('Git export failed')
    regular(archive, 1024 * 1024 * 1024)
    seen, total, entries = {}, 0, 0
    with tarfile.open(archive, mode='r:') as tar:
        for member in tar:
            budget.left(); entries += 1
            rel = PurePosixPath(member.name)
            if rel.is_absolute() or '..' in rel.parts or not rel.parts or entries > 20000:
                raise Refusal('unsafe exported source path/entry limit')
            target = root.joinpath(*rel.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            total += member.size
            if not member.isfile() or member.name not in expected or member.name in seen or member.size > 32 * 1024 * 1024 or total > 1024 * 1024 * 1024:
                raise Refusal('unsafe, substituted or oversized source export')
            target.parent.mkdir(parents=True, exist_ok=True)
            h = hashlib.sha1(('blob ' + str(member.size) + '\0').encode())
            with tar.extractfile(member) as source, target.open('xb') as dest:
                for block in iter(lambda: source.read(1024 * 1024), b''):
                    budget.left(); dest.write(block); h.update(block)
            mode, blob = expected[member.name]
            target.chmod(0o755 if mode == '100755' else 0o644)
            if h.hexdigest() != blob or member.mode & 0o111 != (0o111 if mode == '100755' else 0):
                raise Refusal('exported mode/blob differs from exact Git tree')
            seen[member.name] = {'git_mode': mode, 'git_blob': blob, 'sha256': digest(target)}
    if set(seen) != set(expected):
        raise Refusal('export omitted Git-tracked source')
    discovered = {str(path.relative_to(root)) for path in root.rglob('*')
                  if path.is_file() and path.name in {'go.mod', 'package-lock.json'}}
    return root, {'commit': ref, 'archive_sha256': digest(archive), 'files': seen,
                  'manifests': sorted(discovered), 'bytes': total}

class FixedRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if newurl != req.full_url:
            raise Refusal('fixed database URL redirect refused')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def databases(state, profile, prepare_budget):
    result = {}; opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), FixedRedirect())
    for ecosystem in ['Go', 'npm']:
        prepare_budget.left()
        url = 'https://osv-vulnerabilities.storage.googleapis.com/' + ecosystem + '/all.zip'
        limit = profile['resource_envelope']['database_archive_bytes'][ecosystem]
        with opener.open(urllib.request.Request(url, method='HEAD'), timeout=30) as response:
            length = int(response.headers['Content-Length'])
            etag, generation = response.headers['ETag'], response.headers['x-goog-generation']
        if length <= 0 or length > limit or not generation.isdecimal():
            raise Refusal('database metadata exceeds approved profile')
        path = state / 'public-db/osv-scalibr' / ecosystem / 'all.zip'
        path.parent.mkdir(parents=True, mode=0o700)
        h, count, started = hashlib.sha256(), 0, time.monotonic()
        request = urllib.request.Request(url, headers={'If-Match': etag})
        with opener.open(request, timeout=30) as response, path.open('xb') as f:
            if int(response.headers['Content-Length']) != length or response.headers['ETag'] != etag or response.headers['x-goog-generation'] != generation:
                raise Refusal('database generation changed during acquisition')
            os.fchmod(f.fileno(), 0o600)
            for block in iter(lambda: response.read(1024 * 1024), b''):
                prepare_budget.left()
                count += len(block)
                if count > length or count > limit or time.monotonic() - started > 90:
                    raise Refusal('database capture limit/deadline')
                f.write(block); h.update(block)
            f.flush(); os.fsync(f.fileno())
        if count != length:
            raise Refusal('partial database body')
        result[ecosystem] = {'url': url, 'sha256': h.hexdigest(), 'bytes': count,
                            'etag': etag, 'generation': generation,
                            'fetched_at': datetime.now(timezone.utc).isoformat()}
    write(state / 'public-database-receipts.json', result)
    prepare_budget.left()
    return result

def supplier(state, manifest, runtime):
    tools, go = Path(runtime['supplier_root']), Path(runtime['compiler_root']) / 'bin/go'
    for name, expected in runtime['supplier_pins'].items():
        regular(tools / name, 256 * 1024 * 1024)
        if digest(tools / name) != expected:
            raise Refusal('native supplier binary differs')
    if not go.is_file() or digest(go) != runtime['compiler_sha256']:
        raise Refusal('actual compiler supplier differs')
    return tools, go

def prepare(phase, state, manifest, profile, runtime, bounded_run, Budget, *, manual=False):
    job = 'scan-pr' if phase == 'prepare-differential' else 'scan-scheduled'
    prepare_budget = Budget(load(HERE / 'manual-proof-plan.json')['prepare_seconds'] if manual else
        load(HERE / 'hosted-job-timing-acceptance.json')['jobs'][job]['prepare_total_seconds'])
    state.mkdir(mode=0o700)
    for name in ['home', 'tmp', 'evidence', 'runs']:
        (state / name).mkdir(mode=0o700)
    telemetry=state/'home/.config/go/telemetry';telemetry.mkdir(mode=0o700,parents=True)
    (telemetry/'mode').write_text('off\n');(telemetry/'mode').chmod(0o600)
    if manual:
        event = 'workflow_dispatch'
        candidate = manifest['prepared_source_refs']['candidate']
        trusted_sha = os.environ['GITHUB_SHA']
    else:
        event, candidate, trusted_sha = [os.environ[name] for name in ('EVENT_NAME', 'CANDIDATE_SHA', 'TRUSTED_SHA')]
    differential = phase == 'prepare-differential'
    if not manual and event not in ({'pull_request', 'merge_group'} if differential else {'push', 'schedule'}):
        raise Refusal('event does not match exact job')
    if not all(COMMIT.fullmatch(value) for value in [candidate, trusted_sha]):
        raise Refusal('invalid candidate/trusted SHA')
    tools, go = supplier(state, manifest, runtime)
    child_env = env(go, state, runtime)
    current = bounded_run(['/usr/bin/git', '-C', str(REPOSITORY), 'rev-parse', 'HEAD'], REPOSITORY, child_env, 30)
    if current.returncode or current.stderr or current.stdout.strip() != trusted_sha:
        raise Refusal('bootstrap checkout is not exact trusted event base')
    refs = {'candidate': candidate}
    if differential:
        base = manifest['prepared_source_refs']['baseline'] if manual else os.environ['BASE_SHA']
        if not manual and base != trusted_sha:
            raise Refusal('baseline must equal exact trusted event base')
        refs['baseline'] = base
    sources = {}
    for label, ref in refs.items():
        _, sources[label] = export_source(ref, label, state, child_env, bounded_run, Budget, prepare_budget)
        prepare_budget.left()
        if set(sources[label]['manifests']) != set(profile['inputs']):
            raise Refusal('source manifest set differs from complete reviewed profile')
    inventory = load(HERE / 'public-module-preload-inventory.json')
    # Admit the dependency declarations themselves before requesting any module.
    # Slots may change on code-only commits; exact reviewed path/hash must remain.
    allowed = {}
    for row in inventory['sources'] + inventory['source_go_mod_admission']:
        allowed.setdefault(row['path'], set()).add(row['sha256'])
    module_dirs = {str(PurePosixPath(name).parent) for name in profile['inputs'] if name.endswith('go.mod')}
    for label in sources:
        source = state / (label + '-source')
        for directory in module_dirs:
            for filename in ['go.mod', 'go.sum']:
                name = str(PurePosixPath(directory) / filename)
                path = source / name
                if path.exists():
                    regular(path)
                    if digest(path) not in allowed.get(name, set()):
                        raise Refusal('dependency declaration is outside exact reviewed public inventory')
                elif filename == 'go.mod' or name in allowed:
                    raise Refusal('required source module declaration is missing')
    preload_public_modules(inventory, go, state, env(go, state, runtime, online=True), bounded_run, prepare_budget)
    metadata_sha = preload_application_metadata(state, sources, profile, runtime, prepare_budget)
    db = databases(state, profile, prepare_budget)
    write_completion(state / 'prepared.json', {'complete': True, 'sources': sources, 'databases': db,
        'application_metadata_receipt_sha256': metadata_sha,
        'profile_sha256': manifest['trusted_files']['scanner-profile.json'],
        'supplier_pins': runtime['supplier_pins'], 'compiler_sha256': runtime['compiler_sha256'],
        'event': event, 'automatic_admission': not manual,
        'bootstrap_manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'created_at': datetime.now(timezone.utc).isoformat()}, prepare_budget)


def application_metadata_supplier():
    # Already hash-bound by trusted(); import only the existing approved supplier
    # receiver primitives, never its CLI/build/download orchestration.
    import importlib.util
    spec = importlib.util.spec_from_file_location('application_mod_receiver', HERE / 'prepare-supplier.py')
    module = importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

def application_metadata_rows(state, sources, profile, receiver):
    catalog_path = HERE / 'application-metadata-catalog.json'
    if digest(catalog_path) != '13a73207ad91ef10c6279cf714cbb758c75116261b345c599275789dacef0ee5':
        raise Refusal('application metadata catalog changed')
    rows = load(catalog_path); catalog = {}
    if not isinstance(rows, list) or len(rows) != 576:
        raise Refusal('application metadata catalog count differs')
    for row in rows:
        if (not isinstance(row, dict) or set(row) != {'module','version','go_mod_h1'} or
            not receiver.MODULE.fullmatch(row['module']) or not receiver.VERSION.fullmatch(row['version']) or
            not receiver.H1.fullmatch(row['go_mod_h1']) or
            not all(p and p not in {'.','..'} for p in row['module'].split('/'))):
            raise Refusal('application metadata identity differs')
        identity = row['module'], row['version']
        if identity in catalog:raise Refusal('duplicate application metadata identity')
        catalog[identity] = row['go_mod_h1']
    required = {}; declarations = {}
    for label, source in sources.items():
        root = state / (label + '-source')
        for name in sorted({str(PurePosixPath(p).with_name('go.sum')) for p in profile['inputs'] if p.endswith('go.mod')}):
            if name not in source['files']:continue
            path = root / name;regular(path)
            if digest(path) != source['files'][name]['sha256']:
                raise Refusal('application metadata source sum changed')
            declarations[label + '/' + name] = digest(path)
            for line in path.read_text().splitlines():
                pieces = line.split()
                if len(pieces) != 3:raise Refusal('invalid application checksum row')
                module, version, h1 = pieces
                if not version.endswith('/go.mod'):continue
                identity = module, version[:-7]
                if catalog.get(identity) != h1 or identity in required and required[identity] != h1:
                    raise Refusal('application metadata is outside reviewed exact source sums')
                required[identity] = h1
    if not required or set(sources) == {'baseline','candidate'} and required != catalog:
        raise Refusal('application metadata complete source union differs')
    return [[module, version, h1] for (module, version), h1 in sorted(required.items())], declarations

def preload_application_metadata(state, sources, profile, runtime, budget):
    receiver = application_metadata_supplier()
    rows, declarations = application_metadata_rows(state, sources, profile, receiver)
    # Same standard allocated cache, fixed .mod-only TLS receiver and source H1.
    # Existing MAX_JSON bounds the extra aggregate bytes; shared deadline unchanged.
    cache = receiver.canonical(Path(runtime['existing_module_cache']), True)
    result = {'schema':'workflow.offline-scanner.application-metadata.v1',
        'complete':False, 'accepted':False, 'conserved':False, 'records':[],
        'catalog_sha256':digest(HERE / 'application-metadata-catalog.json'),
        'catalog_count':576, 'selected_count':len(rows), 'source_sums':declarations,
        'source_refs':{label:source['commit'] for label,source in sources.items()},
        'metadata_only':True, 'extra_zip_acquisition':False}
    path = state / 'application-metadata-receipt.json'
    try:
        result['bytes'] = receiver.preload_metadata(rows, cache, budget, result['records'], maximum=MAX_JSON)
        receiver.conserve_metadata(result['records'], cache, budget)
        result.update(conserved=True, complete=True)
    finally:
        write_completion(path, result, budget)
    return digest(path)

def verify_application_metadata(state, prepared, runtime, budget):
    path = state / 'application-metadata-receipt.json'
    receipt = load(path)
    if (digest(path) != prepared['application_metadata_receipt_sha256'] or
        receipt.get('complete') is not True or receipt.get('accepted') is not False or
        receipt.get('conserved') is not True or receipt.get('metadata_only') is not True or
        receipt.get('extra_zip_acquisition') is not False or
        receipt.get('catalog_sha256') != digest(HERE / 'application-metadata-catalog.json')):
        raise Refusal('application metadata receipt is incomplete or changed')
    application_metadata_supplier().conserve_metadata(receipt['records'],
        Path(runtime['existing_module_cache']), budget)

def preload_public_modules(inventory, go, state, online, bounded_run, budget):
    items = inventory['items']
    if not items or not all(row['public_identity_reviewed'] is True for row in items):
        raise Refusal('public module identity admission remains incomplete')
    expected = {(row['module'], row['version']): row['h1'] for row in items}
    if len(expected) != len(items):raise Refusal('duplicate public module inventory identity')
    # Only explicit reviewed identities; no implicit graph, direct/private fallback,
    # candidate command or npm install. Small batches share the unchanged budget.
    logs = state / 'public-preload-logs';logs.mkdir(mode=0o700)
    result = {'complete': False, 'accepted': False, 'expected_count': len(items),
        'inventory_sha256': digest(HERE / 'public-module-preload-inventory.json'),
        'commands': [], 'authenticated_count': 0}
    total_raw = 0
    decoder = json.JSONDecoder(object_pairs_hook=strict_object,
        parse_constant=lambda _: (_ for _ in ()).throw(Refusal('nonfinite preload JSON')))
    try:
        for start in range(0, len(items), 16):
            batch = items[start:start + 16]
            targets = [row['module'] + '@' + row['version'] for row in batch]
            actual = bounded_run([str(go), 'mod', 'download', '-json', *targets], state, online, 90, budget)
            row = {'batch': start // 16, 'targets': targets, 'exit': actual.returncode}
            for suffix, raw in (('stdout', actual.stdout), ('stderr', actual.stderr)):
                data = raw.encode();total_raw += len(data)
                if total_raw > MAX_JSON:raise Refusal('public preload aggregate raw bound exceeded')
                path = logs / (str(start // 16) + '.' + suffix)
                with path.open('xb') as stream:
                    os.fchmod(stream.fileno(), 0o600);stream.write(data);stream.flush();os.fsync(stream.fileno())
                row[suffix + '_sha256'] = digest(path)
            result['commands'].append(row)
            if actual.returncode or actual.stderr:
                raise Refusal('authenticated public preload batch ' + str(start // 16) + ' failed')
            remaining = actual.stdout;records = {}
            try:
                while remaining.strip():
                    remaining = remaining.lstrip();record, consumed = decoder.raw_decode(remaining)
                    remaining = remaining[consumed:]
                    if not isinstance(record, dict):raise Refusal('public preload record is not an object')
                    identity = (record.get('Path'), record.get('Version'))
                    if identity in records:raise Refusal('duplicate public preload response identity')
                    records[identity] = record
            except (ValueError, TypeError):
                raise Refusal('public preload JSON stream is incomplete') from None
            batch_expected = {(item['module'], item['version']): item['h1'] for item in batch}
            if set(records) != set(batch_expected) or any(records[k].get('Sum') != h or records[k].get('Error') for k,h in batch_expected.items()):
                raise Refusal('public module preload identity/checksum mismatch')
            result['authenticated_count'] += len(records);budget.left()
        result['complete'] = result['authenticated_count'] == len(items)
    finally:
        write_completion(state / 'public-module-preload-receipt.json', result, budget)


def socket_filter():
    # x86_64 seccomp_data arch/nr offsets; all socket syscalls and namespace,
    # mount/privilege/debug/io_uring escape paths refuse. Wrong arch kills.
    rows = [(0x20, 0, 0, 4), (0x15, 1, 0, 0xc000003e), (0x06, 0, 0, 0x80000000), (0x20, 0, 0, 0),
            (0x45, 0, 1, 0x40000000), (0x06, 0, 0, 0x80000000)]  # x32 ABI must not bypass native syscall numbers.
    denied = {41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,101,155,165,166,272,288,299,307,308,321,426,427,428,435}
    for nr in sorted(denied):
        rows.extend([(0x15, 0, 1, nr), (0x06, 0, 0, 0x00050000 | errno.EPERM)])
    # clone flags argument0: Go thread creation remains, namespace creation refuses.
    rows.extend([(0x15, 0, 3, 56), (0x20, 0, 0, 16),
                 (0x45, 0, 1, 0x7e020000), (0x06, 0, 0, 0x00050000 | errno.EPERM),
                 (0x06, 0, 0, 0x7fff0000)])
    return b''.join(struct.pack('<HBBI', *row) for row in rows)

def write_completion(path, value, budget):
    # Completion cannot survive a post-write deadline refusal.
    budget.left()
    written = False
    try:
        write(path, value)
        written = True
        budget.left()
    except Exception:
        if written:
            path.unlink()
        raise

def scan_label(phase):
    return 'baseline' if phase == 'scan-baseline' else 'candidate'

def start_scan(state, phase):
    # A later invocation cannot inherit a prior positive receipt. The independent
    # current GitHub step output token is additionally required by consumers,
    # even if failure prevents replacing the prior on-disk current pointer.
    label = scan_label(phase)
    token = secrets.token_hex(32)
    directory = state / ('scan-attempt-' + label + '-' + token)
    directory.mkdir(mode=0o700)
    write(directory / 'started.json', {'complete': False, 'label': label,
        'phase': phase, 'token': token, 'prepared_sha256': digest(state / 'prepared.json')})
    pointer = state / (label + '-current.pending-' + token)
    write(pointer, {'token': token, 'label': label, 'phase': phase})
    os.replace(pointer, state / (label + '-current.json'))
    return directory, token

def github_scan_output(token):
    # This is the current scan step's private runner output file, never a source
    # or worker-selected path. No public artifact substitutes for step success.
    path = Path(os.environ['GITHUB_OUTPUT'])
    if not path.is_absolute() or path.is_symlink():
        raise Refusal('unbound current GitHub step output')
    info = regular(path, 1024 * 1024)
    if info.st_uid != os.getuid() or info.st_mode & 0o022:
        raise Refusal('GitHub step output is not exclusively writable by runner owner')
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
    with os.fdopen(fd, 'ab') as output:
        current = os.fstat(output.fileno())
        if (current.st_dev, current.st_ino) != (info.st_dev, info.st_ino) or not stat.S_ISREG(current.st_mode):
            raise Refusal('GitHub step output changed during opening')
        output.write(('scan-token=' + token + '\n').encode())
        output.flush(); os.fsync(output.fileno())

def seal_scan(state, phase, attempt, token, result, budget):
    # All cleanup has already completed. A result without an atomic final seal
    # is unusable. Write/fsync/chmod/deadline failure can leave partial/private
    # files even when unlink fails; no reader recognizes the pending filename.
    result_path = attempt / 'result.json'
    write(result_path, result)
    budget.left()
    value = {'complete': True, 'label': scan_label(phase), 'phase': phase,
        'token': token, 'result_sha256': digest(result_path),
        'prepared_sha256': digest(state / 'prepared.json'),
        'manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'profile_sha256': digest(HERE / 'scanner-profile.json'),
        'finding_exit': result['exit'], 'phase_exit': 0}
    pending = attempt / 'outcome.pending.json'
    write(pending, value)
    github_scan_output(token)
    trusted()
    budget.left()
    # Rename can itself block. Its final shared-deadline veto must precede
    # actual step success. A retained seal alone cannot authorize consumption:
    # readers also require the current runner step's successful phase outcome.
    os.replace(pending, attempt / 'outcome.json')
    budget.left()
    return 0

def step_scan_identity(label):
    prefix = label.upper() + '_SCAN_'
    token, outcome = os.environ.get(prefix + 'TOKEN', ''), os.environ.get(prefix + 'OUTCOME', '')
    if not SHA.fullmatch(token) or outcome != 'success':
        raise Refusal('actual current scan step did not finish successfully')
    return token

def worker_mounts(state, private, runtime):
    # Preserve every absolute path used by the trusted producer. State is RO;
    # only these explicit private output/cache carveouts are admitted writable.
    mounts = [(HERE, str(HERE), True), (state, str(state), True),
        (Path(runtime['compiler_root']), runtime['compiler_root'], True),
        (Path(runtime['supplier_root']), runtime['supplier_root'], True),
        (Path(runtime['existing_module_cache']), runtime['existing_module_cache'], True)]
    mounts.append((Path(runtime['supplier_delivery_proof_path']), runtime['supplier_delivery_proof_path'], True))
    writable = [private, state / 'runs', state / 'home', state / 'tmp']
    mounts += [(path, str(path), False) for path in writable]
    cache=Path(runtime['existing_build_cache']);mounts.append((cache,str(cache),False))
    return mounts, writable

def require_retired_containers(state):
    # The ledger is under the RO state bind, outside every worker RW carveout.
    # Public diagnostics, like positive results, require actual owned retirement.
    ledgers=state/'container-ledgers'
    for private in sorted(state.glob('worker-*')):
        owned(private)
        ledger=ledgers/private.name
        if not ledger.exists():
            if any((private/name).exists() for name in ('runtime-witness.json','worker-result.json','container.id')):
                raise Refusal('worker observations have no owned creation ledger')
            continue
        owned(ledgers)
        record=load(ledger)
        nonce=record.get('nonce','')
        expected_labels={'workflow.osv.owner':nonce,'workflow.osv.commit':os.environ['GITHUB_SHA']}
        expected_name='wf-osv-'+os.environ['GITHUB_RUN_ID']+'-'+os.environ['GITHUB_RUN_ATTEMPT']+'-'+nonce
        if (not re.fullmatch('[0-9a-f]{32}',nonce) or record.get('name')!=expected_name or
            record.get('labels')!=expected_labels or record.get('output')!=str(private) or
            record.get('helper_root')!=str(HERE) or record.get('production') is not True):
            raise Refusal('actual container ownership ledger is unbound')
        created=load(ledger.with_name(ledger.name+'.created'))
        retired=load(ledger.with_name(ledger.name+'.retired'))
        if (not SHA.fullmatch(str(created.get('container_id',''))) or
            created.get('labels')!=expected_labels or created.get('name')!=expected_name or
            retired.get('labels')!=expected_labels or retired.get('name')!=expected_name or
            retired.get('container_id')!=created['container_id'] or retired.get('retired') is not True or
            retired.get('cleanup',{}).get('absent') is not True):
            raise Refusal('actual authenticated full-ID retirement is unproved')
    return True

def close_all_containers(state, runtime, deadline):
    from osv_container import Container, IMAGE, IMAGE_CONFIG
    ledgers=state/'container-ledgers'
    if not ledgers.exists():return require_retired_containers(state)
    owned(ledgers)
    for private in sorted(state.glob('worker-*')):
        ledger=ledgers/private.name
        if not ledger.exists():continue
        row=load(ledger);nonce=row.get('nonce','')
        labels={'workflow.osv.owner':nonce,'workflow.osv.commit':os.environ['GITHUB_SHA']}
        name='wf-osv-'+os.environ['GITHUB_RUN_ID']+'-'+os.environ['GITHUB_RUN_ATTEMPT']+'-'+nonce
        mounts,writable=worker_mounts(state,private,runtime)
        expected_mounts=[(str(HERE),'/wf',True),(str(private),'/out',False),
            *[(str(source),target,ro) for source,target,ro in mounts]]
        if (not re.fullmatch('[0-9a-f]{32}',nonce) or row.get('name')!=name or row.get('labels')!=labels or
            row.get('image')!=IMAGE or row.get('image_config')!=IMAGE_CONFIG or
            row.get('output')!=str(private) or row.get('helper_root')!=str(HERE) or
            row.get('mounts')!=[list(item) for item in expected_mounts] or
            row.get('carveouts')!=sorted(str(path) for path in writable) or row.get('carveout_parent')!=str(state) or
            row.get('production') is not True or row.get('creation_attempted') is not True):
            raise Refusal('recovery refuses a foreign or altered container ledger')
        container=Container(private,HERE,mounts=mounts,seconds=row['seconds'],production=True,
            carveouts=writable,carveout_parent=state,deadline=deadline)
        container.name=name;container.nonce=nonce;container.labels=labels;container.creation_attempted=True
        created_path=ledger.with_name(ledger.name+'.created')
        if created_path.exists():
            created=load(created_path)
            if created.get('labels')!=labels or created.get('name')!=name or not SHA.fullmatch(str(created.get('container_id',''))):
                raise Refusal('recovery refuses an unauthenticated previous full container ID')
            container.identifier=created['container_id'];container.cleanup_mode=True
            observed=container.inspect(container.identifier)
            if observed is None:
                cleanup={'container_id':container.identifier,'absent':True,'forced_removal':False,'recovery':True}
                container.retired=True
            else:
                container.owned(observed);cleanup=container.cleanup()
        else:
            container.cleanup_mode=True
            observed=container.inspect(name)
            if observed is None:raise Refusal('uncertain create cannot prove full-ID retirement')
            container.identifier=container.owned(observed)['Id']
            write(created_path,{'container_id':container.identifier,'name':name,'labels':labels})
            cleanup=container.cleanup()
        retired_path=ledger.with_name(ledger.name+'.retired')
        if not retired_path.exists():write(retired_path,{'name':name,'container_id':container.identifier,'labels':labels,'retired':True,'cleanup':cleanup})
        write(private/'container-recovery.json',container.lifecycle_evidence())
    return require_retired_containers(state)

def verify_prepared_inputs(state, manifest, runtime):
    prepared = load(state / 'prepared.json')
    if (prepared['bootstrap_manifest_sha256'] != digest(HERE / 'bootstrap-manifest.json') or
        prepared['profile_sha256'] != manifest['trusted_files']['scanner-profile.json'] or
        prepared['supplier_pins'] != runtime['supplier_pins']):
        raise Refusal('prepared immutable inputs changed')
    supplier(state, manifest, runtime)
    from osv_io import Budget
    verify_application_metadata(state, prepared, runtime, Budget(30))
    if load(state / 'public-database-receipts.json') != prepared['databases']:
        raise Refusal('prepared database identity changed')
    for ecosystem, row in prepared['databases'].items():
        archive = state / 'public-db/osv-scalibr' / ecosystem / 'all.zip'
        regular(archive, load(HERE / 'scanner-profile.json')['resource_envelope']['database_archive_bytes'][ecosystem])
        if archive.stat().st_size != row['bytes'] or digest(archive) != row['sha256']:
            raise Refusal('prepared database archive changed')
    for label, source in prepared['sources'].items():
        root = state / (label + '-source')
        paths = {str(path.relative_to(root)): path for path in root.rglob('*') if not path.is_dir()}
        if set(paths) != set(source['files']):
            raise Refusal('exact source file set changed')
        for name, row in source['files'].items():
            path = paths[name];info = regular(path, 32 * 1024 * 1024)
            if digest(path) != row['sha256'] or stat.S_IMODE(info.st_mode) != (0o755 if row['git_mode']=='100755' else 0o644):
                raise Refusal('exact source mode or bytes changed')
    return digest(state / 'prepared.json')

def boundary(phase, state, manifest, runtime, bounded_run, Budget):
    from osv_container import Container
    total = 240 if phase.startswith('report-') else load(HERE / 'scanner-profile.json')['resource_envelope']['total_seconds']
    budget = Budget(total)
    prepared_sha = verify_prepared_inputs(state, manifest, runtime)
    private = Path(tempfile.mkdtemp(prefix='worker-', dir=state));private.chmod(0o700)
    mounts, writable = worker_mounts(state, private, runtime)
    request = {'phase': phase, 'state': str(state), 'private': str(private), 'runtime': runtime,
        'core_contract_sha256': manifest['core_contract_sha256'], 'prepared_sha256': prepared_sha,
        'identity': {key: os.environ[key] for key in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')},
        'uid': os.getuid(), 'gid': os.getgid(),
        'host_namespaces': {name: os.readlink('/proc/self/ns/'+name) for name in ('net','mnt','pid','ipc','cgroup','user')},
        'readonly_targets': ['/wf', *[target for _, target, ro in mounts if ro]],
        'forbidden_paths': ['/var/run/docker.sock','/run/docker.sock','/github/home',
            '/home/runner/.aws','/home/runner/.ssh','/home/runner/.docker','/home/runner/.codex'],
        'compiler_path': str(Path(runtime['compiler_root'])/'bin/go'), 'compiler_sha256': runtime['compiler_sha256']}
    if phase.startswith('report-'):
        labels = ['candidate','baseline'] if phase=='report-differential' else ['candidate']
        request['scan_tokens'] = {label: step_scan_identity(label) for label in labels}
    write(private / 'request.json', request)
    ledgers=state/'container-ledgers'
    if not ledgers.exists():ledgers.mkdir(mode=0o700)
    owned(ledgers)
    container = Container(private, HERE, mounts=mounts, seconds=max(1,int(budget.left())-180),
                          production=True, carveouts=writable, carveout_parent=state,
                          deadline=budget.deadline, ledger=ledgers/private.name)
    try:
        container.create()
        code, row = container.start_wait()
    finally:
        try:
            cleanup = container.cleanup()
            write(ledgers/(private.name+'.retired'), {'name':container.name,'container_id':container.identifier,
                'labels':container.labels,'retired':True,'cleanup':cleanup})
        finally:
            write(private / 'container-lifecycle.json', container.lifecycle_evidence())
    if not container.retired or cleanup.get('absent') is not True:
        raise Refusal('production container retirement is unproved')
    trusted();budget.left()
    result = load(private / 'worker-result.json')
    witness = load(private / 'runtime-witness.json')
    if (code not in [0,1] or result.get('complete') is not True or result.get('exit') != code or
        witness.get('identity') != request['identity'] or witness.get('core_contract_sha256') != manifest['core_contract_sha256'] or
        witness.get('complete') is not True):
        raise Refusal('actual production container did not complete its witnessed scan')
    if phase.startswith('scan-'):
        proof = Path(result['proof'])
        if proof.parent != state / 'runs' or proof.is_symlink():
            raise Refusal('worker proof outside managed output')
        result.update(report_sha256=digest(proof / 'results.json'), receipt_sha256=digest(proof / 'receipt.json'))
    else:
        sarif = Path(result['sarif'])
        if sarif != private / 'reporter.sarif' or digest(sarif) != result['sarif_sha256']:
            raise Refusal('reporter output binding changed')
        regular(private / 'reporter.stderr', 4 * 1024 * 1024)
        result['annotations_sha256'] = digest(private / 'reporter.stderr')
    if verify_prepared_inputs(state, manifest, runtime) != prepared_sha:
        raise Refusal('prepared input changed after container retirement')
    result.update(container_id=container.identifier, container_retired=True,
        runtime_witness_sha256=digest(private / 'runtime-witness.json'),
        lifecycle_sha256=digest(private / 'container-lifecycle.json'))
    trusted();budget.left()
    cleanup_startup_directory()
    trusted();budget.left()
    return code, result, budget

def worker(request_path, manifest, profile):
    request = load(request_path); state = owned(Path(request['state']))
    private = owned(Path(request['private']))
    if (request_path != Path('/out/request.json') or private.parent != state or not private.name.startswith('worker-') or
        request['core_contract_sha256'] != manifest['core_contract_sha256'] or
        request['prepared_sha256'] != digest(state / 'prepared.json')):
        raise Refusal('production worker request binding differs')
    import importlib.util
    spec = importlib.util.spec_from_file_location('container_worker', HERE / 'container-worker.py')
    witness_module = importlib.util.module_from_spec(spec);spec.loader.exec_module(witness_module)
    observed = witness_module.witness(request)
    write(private / 'runtime-witness.json', {'complete': True, 'identity': request['identity'],
        'core_contract_sha256': manifest['core_contract_sha256'], 'witness': observed})
    resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_JSON, MAX_JSON))
    from osv_producer import run_one
    from osv_io import bounded_run, Budget
    runtime = request['runtime']
    tools, go = supplier(state, manifest, runtime)
    child_env = env(go, state, runtime)
    phase = request['phase']; label = 'baseline' if phase == 'scan-baseline' else 'candidate'
    if phase.startswith('scan-'):
        out, blocking = run_one(state / (label + '-source'), profile['inputs'], tools, go,
            state / 'public-db', load(state / 'public-database-receipts.json'), datetime.now(timezone.utc), state / 'runs', child_env,
            admitted_supplier=supplier_contract(manifest, runtime), cgroup_witness=observed)
        write(private / 'worker-result.json', {'complete': True, 'proof': str(out),
            'blocking': sorted(blocking), 'label': label, 'exit': 1 if blocking else 0,
            'socket_denial': 'EPERM', 'kernel_limits': observed['kernel_limits']})
        return 1 if blocking else 0
    if phase in {'report-scheduled', 'report-differential'}:
        _, new = complete_result(state, 'candidate', manifest, request['scan_tokens']['candidate'])
        sarif = private / 'reporter.sarif'
        args = [str(tools / 'osv-reporter'), '--output-files=sarif:' + str(sarif),
                '--new=' + str(new / 'results.json'), '--fail-on-vuln=true']
        if phase == 'report-differential':
            _, old = complete_result(state, 'baseline', manifest, request['scan_tokens']['baseline'])
            args += ['--old=' + str(old / 'results.json')]
            args[1] += ',gh-annotations:#stderr'
        report = bounded_run(args, HERE, child_env, 240, Budget(240))
        regular(sarif)
        if report.returncode not in [0, 1]:
            raise Refusal('reporter incomplete or unsupported exit')
        for suffix, content in [('stdout', report.stdout), ('stderr', report.stderr)]:
            path = private / ('reporter.' + suffix)
            with path.open('xb') as f:
                os.fchmod(f.fileno(), 0o600); f.write(content.encode())
        write(private / 'worker-result.json', {'complete': True, 'exit': report.returncode,
            'sarif': str(sarif), 'sarif_sha256': digest(sarif), 'socket_denial': 'EPERM',
            'kernel_limits': observed['kernel_limits']})
        return report.returncode
    raise Refusal('unsupported worker phase')

def complete_result(state, label, manifest, worker_token=None):
    token = step_scan_identity(label) if worker_token is None else worker_token
    if not SHA.fullmatch(token):
        raise Refusal('invalid trusted report-worker scan identity')
    current = load(state / (label + '-current.json'))
    if current.get('label') != label or current.get('token') != token:
        raise Refusal('stale scan invocation is not current runner step')
    if current.get('phase') not in ({'scan-baseline'} if label == 'baseline' else {'scan-candidate', 'scan-scheduled'}):
        raise Refusal('current scan phase does not match exact result label')
    attempt = owned(state / ('scan-attempt-' + label + '-' + token))
    outcome = load(attempt / 'outcome.json')
    if outcome.get('complete') is not True or type(outcome.get('phase_exit')) is not int or type(outcome.get('finding_exit')) is not int:
        raise Refusal('malformed success-bound scan outcome')
    result_path = attempt / 'result.json'
    expected = {'complete': True, 'label': label, 'phase': current.get('phase'),
        'token': token, 'prepared_sha256': digest(state / 'prepared.json'),
        'manifest_sha256': digest(HERE / 'bootstrap-manifest.json'),
        'profile_sha256': digest(HERE / 'scanner-profile.json'),
        'phase_exit': 0, 'result_sha256': digest(result_path)}
    if any(outcome.get(key) != value for key, value in expected.items()):
        raise Refusal('scan success outcome differs from exact result/prepared/profile state')
    result = load(result_path)
    if outcome.get('finding_exit') != result.get('exit'):
        raise Refusal('scan phase outcome differs from completed finding exit')
    if result.get('label') != label or result.get('complete') is not True or type(result.get('exit')) is not int or result.get('exit') not in [0, 1] or result.get('socket_denial') != 'EPERM':
        raise Refusal('missing or incomplete actual scan result')
    proof = Path(result['proof'])
    if proof.parent != state / 'runs' or proof.is_symlink():
        raise Refusal('scan output is outside producer-owned root')
    receipt = load(proof / 'receipt.json')
    if receipt.get('complete') is not True or receipt.get('blocking') != result['blocking'] or result['exit'] != (1 if result['blocking'] else 0):
        raise Refusal('producer completion/finding contradiction')
    if digest(proof / 'results.json') != result['report_sha256'] or digest(proof / 'receipt.json') != result['receipt_sha256']:
        raise Refusal('producer report or receipt changed')
    prepared = load(state / 'prepared.json')
    if prepared['bootstrap_manifest_sha256'] != digest(HERE / 'bootstrap-manifest.json') or prepared['profile_sha256'] != manifest['trusted_files']['scanner-profile.json']:
        raise Refusal('prepared trusted manifest/profile changed')
    expected_source = {name: [0o755 if row['git_mode'] == '100755' else 0o644, row['sha256']]
                       for name, row in prepared['sources'][label]['files'].items()}
    if receipt['source'] != expected_source or receipt['compiler'] != 'go version go1.27.2 linux/amd64':
        raise Refusal('completed source/compiler differs from exact admitted Git export')
    if receipt['databases'] != {key: row['sha256'] for key, row in prepared['databases'].items()} or receipt['supplier_pins'] != {k:v for k,v in prepared['supplier_pins'].items() if k != 'osv-reporter'}:
        raise Refusal('scan database/supplier identity differs')
    return result, proof

def publish(state, name, source):
    if name not in {'results.sarif','old-results.json','new-results.json'}:
        raise Refusal('unknown public output path')
    regular(source)
    destination = REPOSITORY / name
    if destination.exists() or destination.is_symlink():
        raise Refusal('refuse preexisting output publication path')
    with source.open('rb') as src, destination.open('xb') as out:
        os.fchmod(out.fileno(), 0o600)
        shutil.copyfileobj(src, out, 1024 * 1024)

def evidence(state, manifest):
    require_retired_containers(state)
    destination = REPOSITORY / 'offline-evidence'
    if destination.exists() or destination.is_symlink():
        owned(destination)
        for item in destination.iterdir():
            if not re.fullmatch(r'failure-(?:prepare|scan|report|gate)-(?:scheduled|differential|baseline|candidate)\.json', item.name):
                raise Refusal('refuse existing unrelated evidence publication path')
            regular(item)
    else:
        destination.mkdir(mode=0o700)
    total = 0
    for name in ['prepared.json', 'public-database-receipts.json', 'application-metadata-receipt.json']:
        source = state / name; regular(source)
        shutil.copyfile(source, destination / name); (destination / name).chmod(0o600)
    for label in load(state / 'prepared.json')['sources']:
        result, proof = complete_result(state, label, manifest)
        output = destination / label; output.mkdir(mode=0o700)
        write(output / 'result.json', result)
        for source in proof.iterdir():
            if not re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.(?:stdout|stderr|rc)|receipt\.json|results\.json|root13-govulncheck\.memory-(?:before|after)\.json', source.name):
                continue
            info = regular(source); total += info.st_size
            if total > 128 * 1024 * 1024:
                raise Refusal('evidence publication aggregate bound exceeded')
            shutil.copyfile(source, output / source.name); (output / source.name).chmod(0o600)

def reporting(phase, state, manifest, runtime, bounded_run, Budget):
    candidate, new = complete_result(state, 'candidate', manifest)
    if phase == 'report-differential':
        _, old = complete_result(state, 'baseline', manifest)
    code, result, budget = boundary(phase, state, manifest, runtime, bounded_run, Budget)
    # The launcher and all owned descendants are retired, startup cleanup has
    # succeeded, and trusted bytes/deadline were rechecked before publication.
    candidate, new = complete_result(state, 'candidate', manifest)
    if phase == 'report-differential':
        _, old = complete_result(state, 'baseline', manifest)
    evidence(state, manifest)
    sarif = Path(result['sarif'])
    annotations = sarif.parent / 'reporter.stderr'
    if digest(sarif) != result['sarif_sha256'] or digest(annotations) != result['annotations_sha256']:
        raise Refusal('sealed reporter output changed after retirement')
    trusted(); budget.left()
    if phase == 'report-differential':
        publish(state, 'old-results.json', old / 'results.json')
        publish(state, 'new-results.json', new / 'results.json')
    publish(state, 'results.sarif', sarif)
    if phase == 'report-differential':
        sys.stderr.write(annotations.read_text())
        sys.stderr.flush()
    # Publication and its final annotation flush consume the same report budget.
    # A late write leaves retired diagnostics but never a successful phase exit.
    budget.left()
    return code

def gate(phase, state, manifest):
    candidate, _ = complete_result(state, 'candidate', manifest)
    if phase == 'gate-differential':
        complete_result(state, 'baseline', manifest)
    # Differential report status cannot subtract or waive candidate findings.
    return 1 if candidate['blocking'] else 0

def main():
    phase = sys.argv[1] if len(sys.argv) > 1 else ''
    manifest, profile, bounded_run, Budget = trusted()
    if phase == '--worker' and len(sys.argv) == 3:
        return worker(Path(sys.argv[2]), manifest, profile)
    manual = phase == '--manual-proof' and len(sys.argv) == 4
    if manual:
        phase = sys.argv[2]
        if phase not in MANUAL_PHASES:
            raise Refusal('unknown finite proof phase')
        runtime = manual_admission(manifest, sys.argv[3])
        sys.argv = [sys.argv[0], phase]
    else:
        runtime = admission(manifest)
    if phase not in PHASES or len(sys.argv) != 2:
        raise Refusal('unknown offline scanner phase')
    state = state_path()
    if phase.startswith('prepare-'):
        cleanup_startup_directory()
        prepare(phase, state, manifest, profile, runtime, bounded_run, Budget, manual=manual)
        return 0
    owned(state)
    if phase.startswith('scan-'):
        attempt, token = start_scan(state, phase)
        code, result, budget = boundary(phase, state, manifest, runtime, bounded_run, Budget)
        if result['exit'] != code:
            raise Refusal('completed scan/worker exit mismatch')
        # Finding exit1 remains exact in result and final gate; a completed scan
        # wrapper exits0 so GitHub success distinguishes it from any exit2.
        return seal_scan(state, phase, attempt, token, result, budget)
    elif phase.startswith('report-'):
        return reporting(phase, state, manifest, runtime, bounded_run, Budget)
    else:
        cleanup_startup_directory()
        return gate(phase, state, manifest)
    raise Refusal('uncompleted scanner phase')

def failure_receipt(phase, error):
    # No arbitrary exception text, source paths, supplier response body or secret
    # enters a public artifact. Failure remains exit2, never an empty clean scan.
    reason = str(error)[:512] if isinstance(error, Refusal) else 'manager failed closed; no complete scan verdict'
    value = {'complete': False, 'phase': phase, 'error_type': type(error).__name__,
             'error': reason, 'exit': 2}
    if phase in PHASES:
        try:
            destination = REPOSITORY / 'offline-evidence'
            if destination.exists() or destination.is_symlink():
                owned(destination)
            else:
                destination.mkdir(mode=0o700)
            write(destination / ('failure-' + phase + '.json'), value)
            if phase.startswith(('scan-', 'report-')):
                preserve_incomplete_protocols(phase, destination)
        except Exception:
            # Publication failure cannot convert the original refusal to success.
            value['failure_evidence_publication'] = 'unavailable'
    return value

def preserve_incomplete_protocols(phase, destination):
    # A conservative gate can refuse after genuine raw findings were emitted.
    # Retain bounded producer outputs as incomplete evidence, never as accepted
    # scan/report state. The flat DB, source/config/cache and unrelated files are
    # excluded. No producer-supplied path or symlink selects the publication.
    state = state_path()
    if not state.exists():
        return
    owned(state)
    require_retired_containers(state)
    runs = state / 'runs'
    if not runs.exists():
        return
    owned(runs)
    output = destination / ('incomplete-' + phase)
    output.mkdir(mode=0o700)
    total, rows = 0, []
    def copy(source, target):
        nonlocal total
        info = regular(source)
        total += info.st_size
        if total > 128 * 1024 * 1024:
            raise Refusal('incomplete protocol publication aggregate limit')
        before = digest(source)
        with source.open('rb') as src, target.open('xb') as out:
            os.fchmod(out.fileno(), 0o600)
            shutil.copyfileobj(src, out, 1024 * 1024)
        if digest(source) != before or digest(target) != before:
            raise Refusal('incomplete producer output changed during capture')
        rows.append({'path': str(target.relative_to(output)), 'sha256': before, 'bytes': info.st_size})
    for name in ['prepared.json', 'public-database-receipts.json', 'application-metadata-receipt.json']:
        source = state / name
        if source.exists():
            copy(source, output / name)
    for private in sorted(state.glob('worker-*')):
        owned(private)
        target = output / private.name;target.mkdir(mode=0o700)
        for name in ('runtime-witness.json','container-lifecycle.json','worker-result.json'):
            source = private / name
            if source.exists():copy(source, target / name)
    for proof in sorted(runs.iterdir()):
        if not re.fullmatch(r'osv-proof-[a-z0-9_]+', proof.name):
            raise Refusal('unknown producer proof directory')
        owned(proof)
        target = output / proof.name
        target.mkdir(mode=0o700)
        for source in sorted(proof.iterdir()):
            if re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.(?:stdout|stderr|rc)|receipt\.json|results\.json|root13-govulncheck\.memory-(?:before|after)\.json', source.name):
                copy(source, target / source.name)
    write(output / 'incomplete-publication.json', {'complete': False, 'phase': phase,
        'scope': 'unaccepted retained producer outputs; source slot and completeness cannot be inferred without a verified manager completion receipt',
        'inputs': rows, 'bytes': total, 'exit': 2})

def cleanup_startup_directory():
    # Public bootstrap's fresh directory is empty because -B forbids writes.
    # Worker parents also contain proofs and requests and must remain untouched.
    if len(sys.argv) == 2 and sys.argv[1] in PHASES and sys.pycache_prefix:
        parent = Path(sys.pycache_prefix).parent
        runner_parent = Path(os.environ.get('RUNNER_TEMP', '/nonexistent'))
        if parent.parent == runner_parent and parent.name.startswith('osv-python-'):
            try:parent.lstat()
            except FileNotFoundError:return  # Already removed before this failed phase.
            owned(parent)
            parent.rmdir()

def entry():
    try:
        code = main()
    except Exception as error:
        # Preserve incomplete vs complete-findings distinction. No fabricated
        # empty report, skipped success, new check or approval is written.
        phase = sys.argv[1] if len(sys.argv) > 1 else ''
        if phase == '--worker':
            try:
                write(Path('/out/worker-result.json'), {'complete': False, 'exit': 2,
                    'error_type': type(error).__name__, 'error': str(error)[:512] if isinstance(error, Refusal) else 'producer refused incomplete scan'})
            except Exception:
                pass
        print(json.dumps(failure_receipt(phase, error)), file=sys.stderr)
        try:
            cleanup_startup_directory()
        except Exception:
            print(json.dumps({'complete': False, 'error': 'fresh startup cleanup refused', 'exit': 2}), file=sys.stderr)
        sys.stderr.flush()
        os._exit(2)
    # No fallible cleanup, stream flush, finally or atexit after a positive seal.
    # Worker output files are explicit; reporter stderr was flushed before here.
    os._exit(code)

if __name__ == '__main__':
    entry()
