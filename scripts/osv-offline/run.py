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
import types
import ast

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
HOST_PROC = Path('/proc')
HOST_CGROUP = Path('/sys/fs/cgroup')
TRUSTED_FILES = {'run.py', 'osv_producer.py', 'osv_trusted.py', 'osv_completion.py',
    'osv_io.py', 'scanner-profile.json', 'osv_container.py', 'container-worker.py',
    'container-seccomp.json', 'manual-proof-plan.json', 'run-production-proof.py', 'prepare-supplier.py'}
TRUSTED_FILES.update({'osv_supplier.py', 'application-metadata-catalog.json'})
TRUSTED_FILES.add('osv_resources.py')
TRUSTED_FILES.update({'run-docker-headroom-proof.py','headroom-worker.py','docker-headroom-manifest.json'})
MANUAL_PHASES = {'prepare-differential', 'scan-baseline', 'scan-candidate', 'report-differential', 'gate-differential'}
CORE_FIELDS = {'trusted_files', 'linux_pins', 'public_preload_inventory_sha256',
    'prepared_source_refs', 'resource_contract', 'reviewed_protocol_binding_sha256',
    'container_image', 'container_image_config', 'finite_baseline_projection_sha256'}

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
    if manifest.get('finite_baseline_projection_sha256') is not None:
        raise Refusal('finite derived baseline cannot supply normal-event admission')
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
    finite_projection(manifest, manual=True)
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

def git_tree(files):
    """Compute an exact Git tree identity without creating objects or refs."""
    root = {}
    for name, row in files.items():
        rel = PurePosixPath(name)
        if (rel.is_absolute() or not rel.parts or rel.as_posix() != name or
            any(part in {'.', '..'} or '\0' in part for part in rel.parts) or
            row['git_mode'] not in {'100644', '100755'} or not COMMIT.fullmatch(row['git_blob'])):
            raise Refusal('invalid projected Git inventory')
        tree = root
        for part in rel.parts[:-1]:
            if part in tree and not isinstance(tree[part], dict):
                raise Refusal('projected Git file/directory collision')
            tree = tree.setdefault(part, {})
        if rel.parts[-1] in tree:
            raise Refusal('duplicate projected Git path')
        tree[rel.parts[-1]] = (row['git_mode'], row['git_blob'])
    def identity(tree):
        entries = []
        for name, value in tree.items():
            directory = isinstance(value, dict)
            mode, blob = ('40000', identity(value)) if directory else value
            entries.append(((name + ('/' if directory else '')).encode(),
                mode.encode() + b' ' + name.encode() + b'\0' + bytes.fromhex(blob)))
        raw = b''.join(value for _, value in sorted(entries))
        return hashlib.sha1(b'tree ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()
    return identity(root)

def source_hashes(path):
    # Link both digest algorithms to the same bounded, stable owned source FD.
    before = regular(path, 32 * 1024 * 1024)
    def identity(info):
        return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)
    if before.st_uid != os.getuid() or before.st_nlink != 1:
        raise Refusal('source file must remain exclusively owned')
    sha256 = hashlib.sha256()
    blob = hashlib.sha1(b'blob ' + str(before.st_size).encode() + b'\0')
    count = 0
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        if identity(os.fstat(fd)) != identity(before):
            raise Refusal('source input changed during open')
        for raw in iter(lambda: stream.read(1024 * 1024), b''):
            count += len(raw)
            if count > before.st_size:
                raise Refusal('source input grew during hashing')
            sha256.update(raw); blob.update(raw)
        if (count != before.st_size or identity(os.fstat(fd)) != identity(before) or
            identity(path.lstat()) != identity(before)):
            raise Refusal('source input changed during hashing')
    return sha256.hexdigest(), blob.hexdigest(), stat.S_IMODE(before.st_mode)

def finite_projection(manifest, *, manual):
    expected = manifest.get('finite_baseline_projection_sha256')
    if expected is None:
        return None
    if not manual:
        raise Refusal('finite derived baseline is restricted to reviewed manual proof')
    value = load(HERE / 'manual-proof-plan.json').get('baseline_projection')
    keys = {'schema', 'accepted', 'manual_only', 'finding_waiver', 'disposition_sha256',
        'proposal_sha256', 'patch_sha256', 'fixture_proof_sha256', 'prior_failed_handoff_sha256',
        'original_ref', 'original_tree', 'derived_tree', 'file_count', 'files', 'payloads',
        'original_fixture_catalog', 'derived_fixture_catalog'}
    paths = {'cmd/wfctl/testdata/conformance/' + fixture + '/' + filename
        for fixture in ('iac-pass', 'no-iac') for filename in ('go.mod', 'go.sum')}
    if (not isinstance(value, dict) or set(value) != keys or
        value['schema'] != 'workflow.finite-baseline-fixture-projection.v1' or
        value['accepted'] is not False or value['manual_only'] is not True or value['finding_waiver'] is not False or
        value['disposition_sha256'] != '9f7422cb51dc4179d52b01467cc26a79be2b6ecc49e13bdff18350f38cc80eff' or
        value['proposal_sha256'] != '794671b8c5b3d709fc3d8f378eb3c2ac2c3aa29d1d174a3aa941842b8fe98086' or
        value['patch_sha256'] != '9785b6b5e45876f9306ba505852704316c1c7bd5aee4fb9e10ad81cb187f5dfe' or
        value['fixture_proof_sha256'] != 'c4b347ed89d5ac2d8f19027499745a5ecdd9ceac425175aab840e72cd9652bec' or
        value['prior_failed_handoff_sha256'] != 'ed89430e897661b36171d664995d1dfdbaa9ad1ebc09e9c052cfe6581ddfca14' or
        value['original_ref'] != '2fcb9baf4f03d599efb1fc4c462455a978620ae4' or
        value['original_ref'] != manifest['prepared_source_refs']['baseline'] or
        manifest['prepared_source_refs']['candidate'] != '81b9b1df2386f7d44c8464c999a4ea0919813526' or
        value['original_tree'] != '0a664bef6a3480f9caa6d85a01550135f2103e71' or
        value['derived_tree'] != 'abcceb1d8098fab0c735e03a149313dc8b645312' or
        type(value['file_count']) is not int or value['file_count'] != 3525 or set(value['files']) != paths or
        hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest() != expected):
        raise Refusal('finite baseline projection differs from exact authorized subject')
    for name, change in value['files'].items():
        if set(change) != {'old', 'new'}:
            raise Refusal('unknown fixture substitution field')
        for row in change.values():
            if (set(row) != {'git_mode', 'git_blob', 'sha256', 'bytes'} or row['git_mode'] != '100644' or
                not COMMIT.fullmatch(row['git_blob']) or not SHA.fullmatch(row['sha256']) or
                type(row['bytes']) is not int or not 0 < row['bytes'] <= 64 * 1024):
                raise Refusal('invalid finite fixture metadata identity')
        raw = value['payloads'][change['new']['sha256']].encode()
        if (len(raw) != change['new']['bytes'] or hashlib.sha256(raw).hexdigest() != change['new']['sha256'] or
            hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest() != change['new']['git_blob']):
            raise Refusal('finite replacement bytes differ from authorized metadata')
    if set(value['payloads']) != {row['new']['sha256'] for row in value['files'].values()}:
        raise Refusal('unknown finite fixture payload')
    manifests = {name for name in paths if name.endswith('go.mod')}
    if any(set(value[field]) != manifests for field in ('original_fixture_catalog', 'derived_fixture_catalog')):
        raise Refusal('finite fixture declaration catalogs are incomplete')
    for name in manifests:
        for key, kind in [('original_fixture_catalog', 'old'), ('derived_fixture_catalog', 'new')]:
            if value[key][name]['sha256'] != value['files'][name][kind]['sha256']:
                raise Refusal('finite declared catalog is not source-authenticated')
    return value

def projection_summary(value):
    return {key: item for key, item in value.items()
        if key not in {'payloads', 'original_fixture_catalog', 'derived_fixture_catalog'}}

def apply_fixture_projection(state, source, value):
    # export_source has first authenticated the complete pristine archive.
    if (source['commit'] != value['original_ref'] or len(source['files']) != value['file_count'] or
        source['tree'] != value['original_tree'] or git_tree(source['files']) != value['original_tree']):
        raise Refusal('original baseline export differs from authorized pristine source')
    original = dict(source, files={name: dict(row) for name, row in source['files'].items()})
    root = state / 'baseline-source'
    originals = state / 'baseline-original-fixture-metadata'
    originals.mkdir(mode=0o700)
    for name, change in value['files'].items():
        old = {key: change['old'][key] for key in ('git_mode', 'git_blob', 'sha256')}
        if source['files'].get(name) != old:
            raise Refusal('original fixture metadata differs from exact old identity')
        path = root / name
        before = path.lstat()
        def identity(info):
            return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_nlink,
                info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        if (not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) != 0o644 or
            before.st_uid != os.getuid() or before.st_nlink != 1 or before.st_size != change['old']['bytes']):
            raise Refusal('fixture substitution requires exact regular original bytes')
        fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
        with os.fdopen(fd, 'r+b') as stream:
            if identity(os.fstat(fd)) != identity(before):
                raise Refusal('fixture input changed during open')
            raw = stream.read(64 * 1024 + 1)
            if (identity(os.fstat(fd)) != identity(before) or identity(path.lstat()) != identity(before) or
                len(raw) != before.st_size or hashlib.sha256(raw).hexdigest() != old['sha256']):
                raise Refusal('fixture old bytes changed before substitution')
            if name.endswith('go.sum'):
                target = originals / (PurePosixPath(name).parent.name + '.go.sum')
                with target.open('xb') as preserved:
                    os.fchmod(preserved.fileno(), 0o600); preserved.write(raw)
                    preserved.flush(); os.fsync(preserved.fileno())
            raw = value['payloads'][change['new']['sha256']].encode()
            stream.seek(0); stream.write(raw); stream.truncate(); stream.flush(); os.fsync(fd)
            if (identity(path.lstat()) != identity(os.fstat(fd)) or digest(path) != change['new']['sha256']):
                raise Refusal('fixture substitution changed or rebound')
        source['files'][name] = {key: change['new'][key] for key in ('git_mode', 'git_blob', 'sha256')}
    source.update(tree=git_tree(source['files']), original_git_export=original,
        fixture_projection=projection_summary(value), scan_subject='finite-metadata-derived-baseline',
        bytes=original['bytes'] + sum(row['new']['bytes'] - row['old']['bytes'] for row in value['files'].values()))
    if source['tree'] != value['derived_tree']:
        raise Refusal('finite derived baseline tree differs from exact four-file repair')

def verify_fixture_projection(state, label, source, manifest, *, manual):
    value = finite_projection(manifest, manual=manual)
    if label != 'baseline' or value is None:
        if 'fixture_projection' in source or 'original_git_export' in source:
            raise Refusal('unexpected derived source projection')
        return
    original = source['original_git_export']
    if (source['fixture_projection'] != projection_summary(value) or source['scan_subject'] != 'finite-metadata-derived-baseline' or
        original['commit'] != value['original_ref'] or original['tree'] != value['original_tree'] or
        len(original['files']) != value['file_count'] or git_tree(original['files']) != value['original_tree'] or
        source['commit'] != original['commit'] or source['archive_sha256'] != original['archive_sha256'] or
        digest(state / 'baseline.tar') != original['archive_sha256'] or source['manifests'] != original['manifests']):
        raise Refusal('finite source confused pristine archive with derived scan subject')
    files = {name: dict(row) for name, row in original['files'].items()}
    for name, change in value['files'].items():
        if files.get(name) != {key: change['old'][key] for key in ('git_mode', 'git_blob', 'sha256')}:
            raise Refusal('pristine fixture inventory changed')
        files[name] = {key: change['new'][key] for key in ('git_mode', 'git_blob', 'sha256')}
    if (source['files'] != files or source['tree'] != value['derived_tree'] or git_tree(files) != value['derived_tree'] or
        source['bytes'] != original['bytes'] + sum(row['new']['bytes'] - row['old']['bytes'] for row in value['files'].values())):
        raise Refusal('finite source has changes beyond exact four metadata files')
    from osv_producer import read_owned
    originals = owned(state / 'baseline-original-fixture-metadata')
    if {path.name for path in originals.iterdir()} != {'iac-pass.go.sum', 'no-iac.go.sum'}:
        raise Refusal('original fixture checksum provenance is incomplete')
    for name, change in value['files'].items():
        if name.endswith('go.sum'):
            raw = read_owned(originals / (PurePosixPath(name).parent.name + '.go.sum'), 64 * 1024)
            if len(raw) != change['old']['bytes'] or hashlib.sha256(raw).hexdigest() != change['old']['sha256']:
                raise Refusal('original fixture checksum provenance changed')

def source_subject(source):
    return {'original_commit': source['commit'], 'scan_tree': source['tree'],
        'kind': source.get('scan_subject', 'pristine-git-export'),
        'fixture_projection': source.get('fixture_projection')}

def finite_catalogs(source, manifests, manifest):
    if 'fixture_projection' not in source:
        return None
    value = finite_projection(manifest, manual=True)
    if (set(manifests) != set(source['manifests']) or
        any(row.get('sha256') != source['files'][name]['sha256'] for name, row in manifests.items()) or
        any(manifests.get(name) != row for name, row in value['derived_fixture_catalog'].items())):
        raise Refusal('actual derived declaration catalog differs from authenticated source')
    original = dict(manifests, **value['original_fixture_catalog'])
    if any(row.get('sha256') != source['original_git_export']['files'][name]['sha256'] for name, row in original.items()):
        raise Refusal('original declaration provenance differs from pristine source')
    return {'derived_native_manifest_catalog': manifests, 'original_declared_manifest_inventory': original,
        'original_fixture_catalog_basis': 'source-authenticated frozen declarations; no pristine native PASS inferred',
        'original_native_root2_failure_preserved': True}

def state_path():
    parent = Path(os.environ['RUNNER_TEMP'])
    if not parent.is_absolute() or parent.is_symlink() or not parent.is_dir():
        raise Refusal('invalid runner state parent')
    run, attempt, job = [os.environ[name] for name in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB')]
    if not run.isdecimal() or not attempt.isdecimal() or job not in {'scan-pr', 'scan-scheduled', 'osv-production-proof'}:
        raise Refusal('unbound GitHub execution identity')
    return parent / ('wf-osv-offline-' + run + '-' + attempt + '-' + job)

def env(go, state, runtime, *, online=False, analysis=False):
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
    if analysis:result.update(GOMEMLIMIT='512MiB',GOGC='50')
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
    return root, {'commit': ref, 'tree': git_tree(seen), 'archive_sha256': digest(archive), 'files': seen,
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
    projection = finite_projection(manifest, manual=manual)
    if projection is not None and phase != 'prepare-differential':
        raise Refusal('finite derived baseline requires complete differential proof')
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
        if label == 'baseline' and projection is not None:
            apply_fixture_projection(state, sources[label], projection)
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
                    exact_derived = (projection is not None and manual and label == 'baseline' and
                        name in projection['files'] and sources[label]['files'][name]['sha256'] ==
                        projection['files'][name]['new']['sha256'])
                    if digest(path) not in allowed.get(name, set()) and not (
                        exact_derived and digest(path) == projection['files'][name]['new']['sha256']):
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
        sums = sorted({str(PurePosixPath(p).with_name('go.sum')) for p in profile['inputs'] if p.endswith('go.mod')})
        inputs = [(name, root / name, source['files'].get(name)) for name in sums]
        if 'fixture_projection' in source:
            inputs += [('pristine-original/' + name, state / 'baseline-original-fixture-metadata' /
                (PurePosixPath(name).parent.name + '.go.sum'), source['original_git_export']['files'][name])
                for name in source['fixture_projection']['files'] if name.endswith('go.sum')]
        for name, path, identity in inputs:
            if identity is None:continue
            regular(path)
            if digest(path) != identity['sha256']:
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
        'source_subjects':{label:source_subject(source) for label,source in sources.items()},
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
        receipt.get('catalog_sha256') != digest(HERE / 'application-metadata-catalog.json') or
        receipt.get('source_subjects') != {label:source_subject(source) for label,source in prepared['sources'].items()}):
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

def host_memory_snapshot(budget):
    """Fixed read-only hosted memory evidence; neither reservation nor authority."""
    def read(path):
        budget.left();before=path.lstat()
        key=lambda s:(s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_nlink,s.st_mtime_ns,s.st_ctime_ns)
        if not stat.S_ISREG(before.st_mode):raise Refusal('host memory input must be regular')
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
        with os.fdopen(fd,'rb') as stream:
            if key(os.fstat(fd))!=key(before):raise Refusal('host memory input rebound during open')
            raw=stream.read(16385)
            if len(raw)>16384 or key(os.fstat(fd))!=key(before) or key(path.lstat())!=key(before):
                raise Refusal('host memory input exceeded bound or rebound')
        return raw.decode('ascii')
    raw=read(HOST_PROC/'meminfo');physical={}
    for line in raw.splitlines():
        field,sep,value=line.partition(':')
        if field not in {'MemTotal','MemAvailable'}:continue
        pieces=value.split()
        if not sep or field in physical or len(pieces)!=2 or pieces[1]!='kB' or not re.fullmatch('[0-9]{1,20}',pieces[0]):
            raise Refusal('invalid host physical memory fields')
        physical[field]=int(pieces[0])*1024
    if set(physical)!={'MemTotal','MemAvailable'} or physical['MemTotal']<=0 or physical['MemAvailable']>physical['MemTotal']:
        raise Refusal('host physical memory fields absent or contradictory')
    group=read(HOST_PROC/'self/cgroup');match=re.fullmatch(r'0::(/[^\n]*)\n',group)
    if not match:raise Refusal('host memory measurement requires unified cgroup')
    rel=PurePosixPath(match[1])
    if '..' in rel.parts or rel.as_posix()!=match[1] or len(rel.parts)>32 or HOST_CGROUP.resolve()!=HOST_CGROUP:
        raise Refusal('unsafe host cgroup memory path')
    current=HOST_CGROUP.joinpath(*rel.parts[1:])
    if current.resolve()!=current or str(os.getpid()) not in read(current/'cgroup.procs').split():
        raise Refusal('host memory snapshot is not current owned job group')
    levels=[];maxima=[];headroom=[physical['MemAvailable']]
    while True:
        try:
            maximum=read(current/'memory.max');usage=read(current/'memory.current')
        except FileNotFoundError:
            if current!=HOST_CGROUP:raise Refusal('nonroot host memory controller fields absent')
            for name in ('memory.max','memory.current'):
                try:(current/name).lstat()
                except FileNotFoundError:continue
                raise Refusal('partial or substituted root memory controller fields')
            levels.append({'group':'.','memory_max_bytes':None,'memory_current_bytes':None,
                'uncontrolled_root_fields_observed_absent':True})
            break
        if not re.fullmatch(r'(?:max|[1-9][0-9]{0,19})\n',maximum) or not re.fullmatch(r'[0-9]{1,20}\n',usage):
            raise Refusal('invalid host cgroup memory fields')
        limit=None if maximum=='max\n' else int(maximum);used=int(usage)
        if limit is not None:maxima.append(limit);headroom.append(max(0,limit-used))
        levels.append({'group':str(current.relative_to(HOST_CGROUP)),'memory_max_bytes':limit,'memory_current_bytes':used,
            'memory_max_sha256':hashlib.sha256(maximum.encode()).hexdigest(),'memory_current_sha256':hashlib.sha256(usage.encode()).hexdigest()})
        if current==HOST_CGROUP:break
        current=current.parent
    return {'schema':'workflow.offline-scanner.host-memory.v1','accepted':False,
        'physical_bytes':physical,'physical_meminfo_sha256':hashlib.sha256(raw.encode()).hexdigest(),
        'group_sha256':hashlib.sha256(group.encode()).hexdigest(),'ancestor_cgroup_memory':levels,
        'effective_cgroup_memory_max_bytes':min(maxima) if maxima else None,
        'current_group_memory_current_bytes':levels[0]['memory_current_bytes'],
        'estimated_physical_and_cgroup_headroom_bytes':min(headroom),
        'measured_cgroup_scope':'current-runner-process ancestors; Docker parent ancestry is not proved equivalent',
        'docker_parent_effective_headroom_bytes':None,'docker_parent_headroom_status':'UNKNOWN/HOLD',
        'scope':'point-in-time host physical/current-runner ancestor observations; no reservation, resource expansion or admission'}

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
        resource_phase=row.get('resource_phase');resource_authority=row.get('resource_authority')
        if resource_phase is not None:
            from osv_resources import authorize_phase,requested_memory
            limits=authorize_phase(resource_phase,resource_authority)
            if (row.get('kernel_limits')!=limits or row.get('requested_memory_bytes')!=requested_memory(resource_phase) or
                resource_authority is not None and resource_authority['core_contract_sha256']!=load(HERE/'bootstrap-manifest.json')['core_contract_sha256']):
                raise Refusal('recovery resource authority or quota differs')
        elif resource_authority is not None:
            raise Refusal('default recovery has no expansion authority')
        container=Container(private,HERE,mounts=mounts,seconds=row['seconds'],production=True,
            carveouts=writable,carveout_parent=state,deadline=deadline,
            resource_phase=resource_phase,resource_authority=resource_authority)
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
        verify_fixture_projection(state, label, source, manifest,
            manual=prepared.get('event') == 'workflow_dispatch' and prepared.get('automatic_admission') is False)
        root = state / (label + '-source')
        paths = {str(path.relative_to(root)): path for path in root.rglob('*') if not path.is_dir()}
        if set(paths) != set(source['files']):
            raise Refusal('exact source file set changed')
        for name, row in source['files'].items():
            sha256, blob, mode = source_hashes(paths[name])
            if sha256 != row['sha256'] or blob != row['git_blob'] or mode != (0o755 if row['git_mode']=='100755' else 0o644):
                raise Refusal('exact source mode or bytes changed')
        if git_tree(source['files']) != source['tree']:
            raise Refusal('prepared scan tree identity changed')
    return digest(state / 'prepared.json')

def boundary(phase, state, manifest, runtime, bounded_run, Budget):
    from osv_container import Container
    total = 240 if phase.startswith('report-') else load(HERE / 'scanner-profile.json')['resource_envelope']['total_seconds']
    budget = Budget(total)
    prepared_sha = verify_prepared_inputs(state, manifest, runtime)
    selection = resource_phase_selection(phase,manifest,state,budget)
    private = Path(tempfile.mkdtemp(prefix='worker-', dir=state));private.chmod(0o700)
    mounts, writable = worker_mounts(state, private, runtime)
    request = {'phase': phase, 'state': str(state), 'private': str(private), 'runtime': runtime,
        'resource_phase':selection['phase'],'resource_authority':selection['authority'],
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
    ledgers=state/'container-ledgers'
    if not ledgers.exists():ledgers.mkdir(mode=0o700)
    owned(ledgers)
    container = Container(private, HERE, mounts=mounts, seconds=max(1,int(budget.left())-180),
                          production=True, carveouts=writable, carveout_parent=state,
                          deadline=budget.deadline, ledger=ledgers/private.name,
                          resource_phase=selection['phase'],resource_authority=selection['authority'])
    request['resource_nonce']=container.nonce
    try:
        container.create()
        request['resource_container_id']=container.identifier
        request_sha=write_request_resource(private/'request.json',request)
        observer=None
        if selection['authority'] is not None:
            observer=lambda row: resource_startup_observer(container,row,private,request,request_sha,selection,manifest,budget)
        code, row = container.start_wait(live_observer=observer) if observer is not None else container.start_wait()
    finally:
        try:
            cleanup = container.cleanup()
            write(ledgers/(private.name+'.retired'), {'name':container.name,'container_id':container.identifier,
                'labels':container.labels,'retired':True,'cleanup':cleanup})
        finally:
            write(private / 'container-lifecycle.json', container.lifecycle_evidence())
            if selection['authority'] is not None and container.retired:
                resource_probe_export(Path(selection['probe']['output']),phase,headroom_module(manifest),state,manifest,private)
    if not container.retired or cleanup.get('absent') is not True:
        raise Refusal('production container retirement is unproved')
    trusted();budget.left()
    result = load(private / 'worker-result.json')
    witness = load(private / 'runtime-witness.json')
    if (code not in [0,1] or result.get('complete') is not True or result.get('exit') != code or
        witness.get('identity') != request['identity'] or witness.get('core_contract_sha256') != manifest['core_contract_sha256'] or
        witness.get('complete') is not True):
        raise Refusal('actual production container did not complete its witnessed scan')
    from osv_resources import validate_witness
    validate_witness(selection['phase'],selection['authority'],witness['witness'])
    if selection['authority'] is not None and (not SHA.fullmatch(str(witness.get('resource_ready_sha256',''))) or
        result.get('resource_authority')!=selection['authority'] or result.get('resource_phase')!=phase):
        raise Refusal('completed expanded worker/ready authority differs')
    if phase.startswith('scan-'):
        proof = Path(result['proof'])
        if proof.parent != state / 'runs' or proof.is_symlink():
            raise Refusal('worker proof outside managed output')
        producer_receipt=load(proof / 'receipt.json')
        report_sha256=digest(proof / 'results.json')
        if producer_receipt.get('report_sha256')!=report_sha256:
            raise Refusal('published report differs from authenticated native buffer')
        result.update(report_sha256=report_sha256, receipt_sha256=digest(proof / 'receipt.json'))
        source = load(state / 'prepared.json')['sources'][result['label']]
        result.update(source_subject=source_subject(source),
            declared_catalogs=finite_catalogs(source, producer_receipt['manifests'], manifest))
    else:
        sarif = Path(result['sarif'])
        if sarif != private / 'reporter.sarif' or digest(sarif) != result['sarif_sha256']:
            raise Refusal('reporter output binding changed')
        regular(private / 'reporter.stderr', 4 * 1024 * 1024)
        result['annotations_sha256'] = digest(private / 'reporter.stderr')
    if verify_prepared_inputs(state, manifest, runtime) != prepared_sha:
        raise Refusal('prepared input changed after container retirement')
    result.update(container_id=container.identifier, container_retired=True,
        resource_phase=selection['phase'],resource_authority=selection['authority'],kernel_limits=selection['limits'],
        runtime_witness_sha256=digest(private / 'runtime-witness.json'),
        lifecycle_sha256=digest(private / 'container-lifecycle.json'))
    if selection['authority'] is not None:
        result['resource_export_sha256']=seal_resource_export(state,phase,manifest,result)
    trusted();budget.left()
    cleanup_startup_directory()
    trusted();budget.left()
    return code, result, budget

def resource_phase_selection(phase,manifest,state=None,budget=None):
    from osv_resources import source_proposal,authorize_phase,authorize_owner,requested_memory,SCAN_MEMORY,OWNER_APPROVAL_SHA256
    plan=load(HERE/'manual-proof-plan.json');profile=load(HERE/'scanner-profile.json')
    expected=source_proposal()
    if plan.get('phase_memory_proposal') != expected or profile.get('phase_memory_proposal') != expected:
        raise Refusal('immutable source/plan/profile phase proposal differs')
    owner=plan.get('phase_resource_owner_approval_sha256') if phase in ('scan-baseline','scan-candidate') else None
    selected=authorize_owner(phase,owner)
    if requested_memory(phase)==SCAN_MEMORY:
        if state is None or budget is None:raise Refusal('enlarged phase lacks fresh owned controller context')
        authority,probe=resource_probe(phase,manifest,state,budget)
        authorize_phase(phase,authority)
        return {'phase':phase,'authority':authority,'limits':selected,'probe':probe}
    return {'phase':phase,'authority':None,'limits':selected}

def headroom_module(manifest):
    path=HERE/'run-docker-headroom-proof.py'
    # Trusted read-only source; compile the exact bounded buffer being hashed.
    info=regular(path,1048576)
    with os.fdopen(os.open(path,os.O_RDONLY|os.O_NOFOLLOW),'rb') as stream:
        raw=stream.read(1048577)
    if len(raw)!=info.st_size or hashlib.sha256(raw).hexdigest()!=manifest['trusted_files'][path.name]:raise Refusal('headroom guard source differs')
    module=types.ModuleType('owned_resource_headroom');module.__file__=str(path)
    exec(compile(raw,str(path),'exec'),module.__dict__);return module

def resource_source_guard(raw,headroom_sha):
    # AST supplies only stable source locations; no version-dependent AST dump or fields.
    if type(raw) is not bytes or len(raw)>1048576 or type(headroom_sha) is not str or not SHA.fullmatch(headroom_sha):
        raise Refusal('invalid bounded resource source frame')
    text=raw.decode('utf-8');tree=ast.parse(text);lines=raw.splitlines(keepends=True)
    names={'headroom_module','resource_source_guard','resource_guard_digest','write_request_resource','resource_headroom',
        'resource_host_identity','resource_probe_export','validate_resource_probe','resource_probe','resource_startup_observer','wait_resource_ready',
        'resource_export_snapshot','resource_export_context','resource_export_buffers','validate_resource_export','seal_resource_export','copy_resource_export'}
    blocks={};intervals=[]
    for node in tree.body:
        if not isinstance(node,(ast.FunctionDef,ast.AsyncFunctionDef)) or node.name not in names:continue
        if not isinstance(node,ast.FunctionDef) or node.name in blocks or node.col_offset!=0:
            raise Refusal('duplicate or unsupported resource source definition')
        locations=[node.lineno]+[decorator.lineno for decorator in node.decorator_list]
        first=min(locations);last=node.end_lineno
        if (type(first) is not int or type(last) is not int or not 1<=first<=node.lineno<=last<=len(lines) or
            any(type(line) is not int or not first<=line<=node.lineno for line in locations) or
            any(not (last<lo or first>hi) for lo,hi in intervals)):
            raise Refusal('ambiguous resource source location bounds')
        intervals.append((first,last));blocks[node.name]=b''.join(lines[first-1:last])
    if set(blocks)!=names:raise Refusal('incomplete resource startup source guard')
    framed=hashlib.sha256();framed.update(b'workflow.osv.resource-guard.source-bytes.v1\0')
    framed.update(len(names).to_bytes(4,'big'))
    for name in sorted(names):
        label=name.encode('ascii');block=blocks[name]
        framed.update(len(label).to_bytes(2,'big'));framed.update(label)
        framed.update(len(block).to_bytes(8,'big'));framed.update(block)
    framed.update(bytes.fromhex(headroom_sha));return framed.hexdigest()

def resource_guard_digest(manifest):
    helper=headroom_module(manifest)
    raw,_=helper.file_bytes(HERE/'run.py',1048576,uid=os.getuid())
    if hashlib.sha256(raw).hexdigest()!=manifest['trusted_files']['run.py']:
        raise Refusal('authenticated resource guard source differs')
    return resource_source_guard(raw,manifest['trusted_files']['run-docker-headroom-proof.py'])

def write_request_resource(path,value,*,committed=False):
    raw=(json.dumps(value,sort_keys=True,indent=2)+'\n').encode()
    if len(raw)>MAX_JSON:raise Refusal('request byte bound exceeded')
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(),0o600);stream.write(raw);stream.flush();os.fsync(stream.fileno())
    if committed:
        fd=os.open(path.with_name(path.name+'.committed'),os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        os.close(fd)
    return hashlib.sha256(raw).hexdigest()

def resource_headroom(observation,helper,expected_limits):
    from osv_resources import REQUIRED_HOST_PARENT_HEADROOM,require_page_size,SCAN_KERNEL_MEMORY,SCAN_MEMORY,DEFAULT_MEMORY
    require_page_size()
    physical=observation.get('physical_bytes',{});parents=observation.get('docker_parent_ancestors',[])
    if (type(physical) is not dict or set(physical)!={'MemTotal','MemAvailable'} or
        type(physical['MemTotal']) is not int or type(physical['MemAvailable']) is not int or
        not 0<=physical['MemAvailable']<=physical['MemTotal'] or physical['MemTotal']==0 or
        type(parents) is not list or not 1<=len(parents)<=32 or parents[-1].get('group')!='.' or
        observation.get('own_container_memory_cap_excluded_from_parent_estimate') is not True or
        observation.get('memory_page_size')!=4096 or
        observation.get('requested_memory_bytes')!=(SCAN_MEMORY if expected_limits['memory.max']==str(SCAN_KERNEL_MEMORY) else DEFAULT_MEMORY) or
        {key:observation.get('own_container_limits',{}).get(key) for key in expected_limits}!=expected_limits):
        raise Refusal('actual Docker-parent/physical/quota witness differs')
    cpu=observation['own_container_limits'].get('cpu.max','').split()
    if len(cpu)!=2 or not all(x.isdecimal() for x in cpu) or int(cpu[0])!=2*int(cpu[1]) or int(cpu[1])<=0:
        raise Refusal('actual resource CPU quota differs')
    groups=[]
    for parent in parents:
        group=parent.get('group');identity=parent.get('identity')
        if (type(group) is not str or group in groups or type(identity) is not list or len(identity)!=2 or
            any(type(x) is not int or x<0 for x in identity)):
            raise Refusal('actual Docker ancestry identity differs')
        groups.append(group)
    relative=PurePosixPath(observation.get('host_init_cgroup',''))
    if relative.is_absolute() or '..' in relative.parts or [PurePosixPath(x) for x in groups]!=list(relative.parents):
        raise Refusal('actual Docker ancestry path differs')
    for name in ('host_init','host_init_final'):
        native=observation.get(name,{})
        if native.get('state') not in helper.STAT_STATES-helper.DEAD_STAT_STATES or type(native.get('pid')) is not int or native['pid']<=0:
            raise Refusal('actual live init witness differs')
    if helper.process_identity(observation['host_init'])!=helper.process_identity(observation['host_init_final']):
        raise Refusal('actual init rebound within observation')
    estimate,maximum=helper.capacity(physical['MemAvailable'],parents)
    if (estimate!=observation.get('estimated_physical_and_docker_parent_headroom_bytes') or
        maximum!=observation.get('docker_parent_effective_memory_max_bytes') or estimate<REQUIRED_HOST_PARENT_HEADROOM):
        raise Refusal('actual Docker parent has insufficient requested3GB plus2GiB reserve')
    return [(x['group'],x['identity']) for x in parents]

def resource_host_identity(helper,output,name):
    raw,identity=helper.file_bytes(Path('/proc/sys/kernel/random/boot_id'),64,uid=0,pseudo=True)
    if not re.fullmatch(b'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\n',raw):raise Refusal('host boot identity malformed')
    helper.retain(output/'raw',name+'-host-boot-id.raw',raw,'/proc/sys/kernel/random/boot_id',identity)
    return hashlib.sha256(raw).hexdigest()

def resource_export_snapshot(root,helper,*,published=False):
    """Capture one bounded owned export tree through checked directory/file FDs."""
    root=Path(root);owned(root);files={};directories=[];count=0;total=0
    def walk(path,prefix=''):
        nonlocal count,total
        directory=helper.Directory(path,os.getuid())
        try:
            names=sorted(os.listdir(directory.fd))
            if len(names)>64:raise Refusal('resource export directory count limit')
            for name in names:
                count+=1
                if count>128 or not re.fullmatch(r'[a-zA-Z0-9_.-]+',name):raise Refusal('resource export entry/name limit')
                info=os.stat(name,dir_fd=directory.fd,follow_symlinks=False);relative=prefix+name
                if stat.S_ISDIR(info.st_mode):
                    owned(path/name);directories.append(relative);walk(path/name,relative+'/')
                elif stat.S_ISREG(info.st_mode):
                    if published and stat.S_IMODE(info.st_mode)!=0o600:raise Refusal('resource export private mode differs')
                    raw,_=helper.file_bytes(Path(name),1048576,uid=os.getuid(),directory_fd=directory.fd)
                    total+=len(raw)
                    if total>8*1048576:raise Refusal('resource export aggregate byte limit')
                    files[relative]=raw
                else:raise Refusal('resource export nonregular entry')
                directory.check()
        finally:directory.close()
    walk(root)
    return files,sorted(directories)

def resource_export_context(state,phase,manifest,helper):
    if phase not in ('scan-baseline','scan-candidate'):raise Refusal('unrequested resource export phase')
    owned(state);label=phase.removeprefix('scan-')
    raw,_=helper.file_bytes(state/(label+'-current.json'),65536,uid=os.getuid());current=helper.decode(raw)
    if (set(current)!={'token','label','phase'} or current['label']!=label or current['phase']!=phase or
        not SHA.fullmatch(str(current['token']))):raise Refusal('resource export current phase/token differs')
    identity={key:os.environ[key] for key in helper.IDENTITY}
    if (identity['GITHUB_REPOSITORY']!='GoCodeAlone/workflow' or identity['GITHUB_JOB']!='osv-production-proof' or
        identity['GITHUB_EVENT_NAME']!='workflow_dispatch' or identity['GITHUB_REF']!='refs/heads/prep/osv-prerequisites-20261011-r1' or
        not COMMIT.fullmatch(identity['GITHUB_SHA']) or not identity['GITHUB_RUN_ID'].isdecimal() or identity['GITHUB_RUN_ATTEMPT']!='1'):
        raise Refusal('resource export finite identity differs')
    return {'phase':phase,'token':current['token'],'identity':identity,
        'core_contract_sha256':manifest['core_contract_sha256'],'bootstrap_manifest_sha256':digest(HERE/'bootstrap-manifest.json'),
        'prepared_sha256':digest(state/'prepared.json')}

def resource_probe_export(output,phase,helper,state,manifest,private=None):
    # Diagnostics survive failure. Only the later host-owned eligibility seal admits reports.
    context=resource_export_context(state,phase,manifest,helper);require_retired_containers(state);output=Path(output);owned(output)
    if output.parent!=Path(os.environ['RUNNER_TEMP']) or not output.name.startswith('osv-headroom-'):
        raise Refusal('resource export source is not current owned probe output')
    files,directories=resource_export_snapshot(output,helper)
    fixed={'docker-parent-headroom-proof.json','host-docker-parent-before.json','host-docker-parent-after.json',
        'ownership.json','ownership.json.created','worker/container.id','worker/headroom-worker.json','scan-start/actual-parent.json'}
    rawname=r'(?:before|after)-(?:owned-live-inspect\.json|init-(?:stat|cgroup)-(?:first|final)\.raw|leaf-(?:memory\.max|memory\.swap\.max|pids\.max|cpu\.max)\.raw|physical-meminfo\.raw|parent-(?:[0-9]|[12][0-9]|3[01])-(?:memory\.max|memory\.current)\.raw)'
    for name in files:
        if name in fixed:continue
        if name=='raw/probe-host-boot-id.raw' or name=='scan-start/raw/startup-host-boot-id.raw':continue
        if re.fullmatch(r'raw/'+rawname,name) or re.fullmatch(r'scan-start/raw/'+rawname,name):continue
        raise Refusal('resource export contains unrequested probe file')
    if set(directories)-{'raw','worker','scan-start','scan-start/raw'}:raise Refusal('resource export contains unrelated directory')
    proof=helper.decode(files.get('docker-parent-headroom-proof.json',b''))
    if (proof.get('retirement_verified') is not True or proof.get('lifecycle',{}).get('retired') is not True or
        proof.get('identity')!=context['identity'] or proof.get('parent_phase')!=phase or
        proof.get('source_manifest_sha256')!=manifest['trusted_files']['docker-headroom-manifest.json']):
        raise Refusal('resource diagnostic probe provenance/retirement differs')
    # Preserve only fixed data handoffs, including incomplete ones, under existing bounds.
    if private is not None:
        private=Path(private);owned(private)
        if private.parent!=state or not private.name.startswith('worker-'):raise Refusal('resource export worker source differs')
        directories.append('startup')
        for name in ('request.json','resource-waiting.json','resource-waiting.json.committed','resource-ready.json','resource-ready.json.committed'):
            source=private/name
            if source.exists() or source.is_symlink():
                raw,_=helper.file_bytes(source,0 if name.endswith('.committed') else 65536,uid=os.getuid())
                if name=='request.json':
                    request=helper.decode(raw)
                    fields={'phase','state','private','runtime','resource_phase','resource_authority','core_contract_sha256',
                        'prepared_sha256','identity','uid','gid','host_namespaces','readonly_targets','forbidden_paths',
                        'compiler_path','compiler_sha256','resource_nonce','resource_container_id'}
                    runtime_fields={'accepted','identity','core_contract_sha256','compiler_root','compiler_sha256','supplier_root',
                        'supplier_pins','supplier_delivery_proof_sha256','supplier_delivery_proof_path','existing_gopath',
                        'existing_module_cache','existing_build_cache'}
                    if set(request)!=fields or type(request['runtime']) is not dict or set(request['runtime'])!=runtime_fields:
                        raise Refusal('resource raw request contains unrequested fields')
                files['startup/'+name]=raw
    if len(files)+len(directories)+2>128 or sum(map(len,files.values()))>8*1048576-131072:
        raise Refusal('resource handoff export aggregate bound exceeded')
    index={'schema':'workflow.offline-scanner.resource-export.v1','accepted':False,'report_eligible':False,
        'context':context,'worker_private':private.name if private is not None else None,
        'probe_output':output.name,'probe_sha256':hashlib.sha256(files['docker-parent-headroom-proof.json']).hexdigest(),
        'directories':sorted(directories),'files':{name:{'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()} for name,raw in sorted(files.items())}}
    index_path=state/('resource-export-'+phase+'.json');write(index_path,index)
    raw,_=helper.file_bytes(index_path,65536,uid=os.getuid())
    destination=REPOSITORY/'offline-evidence'
    if not destination.exists():destination.mkdir(mode=0o700)
    owned(destination);target=destination/('resource-guard-'+phase);target.mkdir(mode=0o700)
    for name in sorted(directories,key=lambda name:(name.count('/'),name)):(target/name).mkdir(mode=0o700)
    for name,data in dict(files,**{'export-index.json':raw}).items():
        with (target/name).open('xb') as stream:os.fchmod(stream.fileno(),0o600);stream.write(data);stream.flush();os.fsync(stream.fileno())
    return hashlib.sha256(raw).hexdigest()

def resource_export_buffers(state,phase,manifest):
    helper=headroom_module(manifest);context=resource_export_context(state,phase,manifest,helper)
    path=state/('resource-export-'+phase+'.json');raw,opened=helper.file_bytes(path,65536,uid=os.getuid());index=helper.decode(raw)
    if opened['mode']!=0o600:raise Refusal('resource export host index mode differs')
    if (set(index)!={'schema','accepted','report_eligible','context','worker_private','probe_output','probe_sha256','directories','files'} or
        index['schema']!='workflow.offline-scanner.resource-export.v1' or index['accepted'] is not False or
        index['report_eligible'] is not False or index['context']!=context or type(index['files']) is not dict or
        type(index['directories']) is not list or not SHA.fullmatch(str(index['probe_sha256']))):
        raise Refusal('resource export index/current phase differs')
    target=REPOSITORY/'offline-evidence'/('resource-guard-'+phase)
    files,directories=resource_export_snapshot(target,helper,published=True)
    eligible=state/('resource-export-'+phase+'.eligible.json');extra={}
    if eligible.exists() or eligible.is_symlink():
        value,opened=helper.file_bytes(eligible,65536,uid=os.getuid())
        if opened['mode']!=0o600:raise Refusal('resource eligible host index mode differs')
        extra['eligible-index.json']=value
    expected=set(index['files'])|{'export-index.json'}|set(extra)
    if set(files)!=expected or directories!=index['directories'] or files['export-index.json']!=raw:
        raise Refusal('resource export exact membership/index differs')
    for name,row in index['files'].items():
        if (type(row) is not dict or set(row)!={'bytes','sha256'} or type(row['bytes']) is not int or
            row['bytes']!=len(files[name]) or row['sha256']!=hashlib.sha256(files[name]).hexdigest()):
            raise Refusal('resource export bytes/hash differ')
    for name,value in extra.items():
        if files[name]!=value:raise Refusal('resource eligible index buffer differs')
    if hashlib.sha256(files['docker-parent-headroom-proof.json']).hexdigest()!=index['probe_sha256']:
        raise Refusal('resource export probe hash differs')
    return helper,index,raw,files

def validate_resource_export(state,phase,manifest,result,*,sealed=True):
    from osv_resources import authorize_phase,validate_witness,ACTUAL_PARENT_STARTUP_GUARD_SHA256
    helper,index,index_raw,files=resource_export_buffers(state,phase,manifest)
    private_name=index['worker_private']
    if type(private_name) is not str or not re.fullmatch('worker-[a-zA-Z0-9_-]+',private_name):
        raise Refusal('resource export lacks exact completed worker')
    private=owned(state/private_name);native={}
    names=('request.json','resource-waiting.json','resource-waiting.json.committed','resource-ready.json','resource-ready.json.committed',
        'runtime-witness.json','worker-result.json','container-lifecycle.json')
    for name in names:
        raw,_=helper.file_bytes(private/name,0 if name.endswith('.committed') else 65536,uid=os.getuid());native[name]=raw
        if name.startswith('resource-') or name=='request.json':
            if files.get('startup/'+name)!=raw:raise Refusal('resource startup exported/current buffer differs')
    request=helper.decode(native['request.json']);waiting=helper.decode(native['resource-waiting.json']);ready=helper.decode(native['resource-ready.json'])
    identity={key:index['context']['identity'][key] for key in ('GITHUB_REPOSITORY','GITHUB_SHA','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','GITHUB_JOB')}
    authority=request.get('resource_authority');limits=authorize_phase(phase,authority)
    cid=request.get('resource_container_id');nonce=request.get('resource_nonce');request_sha=hashlib.sha256(native['request.json']).hexdigest()
    if (request.get('phase')!=phase or request.get('resource_phase')!=phase or request.get('state')!=str(state) or
        request.get('private')!=str(private) or request.get('identity')!=identity or
        request.get('core_contract_sha256')!=manifest['core_contract_sha256'] or
        request.get('prepared_sha256')!=index['context']['prepared_sha256'] or
        not SHA.fullmatch(str(cid)) or not re.fullmatch('[0-9a-f]{32}',str(nonce))):
        raise Refusal('resource export worker request/context differs')
    joined={'resource_phase':phase,'resource_authority':authority,'core_contract_sha256':manifest['core_contract_sha256'],
        'nonce':nonce,'container_id':cid,'request_sha256':request_sha}
    if (set(waiting)!=set(joined)|{'schema','native_started','worker_private_pid'} or
        waiting.get('schema')!='workflow.offline-scanner.resource-waiting.v1' or waiting.get('native_started') is not False or
        type(waiting.get('worker_private_pid')) is not int or waiting['worker_private_pid']<=0 or
        set(ready)!=set(joined)|{'schema','actual_parent_observation_sha256','actual_kernel_limits','requested_memory_bytes','native_execution_permitted'} or
        ready.get('schema')!='workflow.offline-scanner.resource-ready.v1' or ready.get('native_execution_permitted') is not True or
        ready.get('requested_memory_bytes')!=3000000000 or ready.get('actual_kernel_limits')!=limits or
        any(waiting.get(key)!=value or ready.get(key)!=value for key,value in joined.items())):
        raise Refusal('resource export waiting/ready closed binding differs')
    proof=helper.decode(files['docker-parent-headroom-proof.json'])
    original=validate_resource_probe(proof,phase,manifest,index['context']['identity'],ACTUAL_PARENT_STARTUP_GUARD_SHA256)
    if authority['actual_headroom_receipt_sha256']!=index['probe_sha256']:
        raise Refusal('resource export authority/probe differs')
    probe_owner=helper.decode(files.get('ownership.json',b''));probe_created=helper.decode(files.get('ownership.json.created',b''))
    probe_worker=helper.decode(files.get('worker/headroom-worker.json',b''))
    probe_labels={'workflow.osv.owner':proof['nonce'],'workflow.osv.commit':identity['GITHUB_SHA']}
    probe_name='wf-osv-'+identity['GITHUB_RUN_ID']+'-'+identity['GITHUB_RUN_ATTEMPT']+'-'+proof['nonce']
    if (probe_owner.get('nonce')!=proof['nonce'] or probe_owner.get('labels')!=probe_labels or
        probe_owner.get('name')!=probe_name or probe_created!={'container_id':proof['container_id'],'name':probe_name,'labels':probe_labels} or
        proof['lifecycle'].get('name')!=probe_name or files.get('worker/container.id',b'').decode('ascii').strip()!=proof['container_id'] or
        probe_worker!=proof.get('worker') or hashlib.sha256(files['worker/headroom-worker.json']).hexdigest()!=proof.get('worker_sha256')):
        raise Refusal('resource export probe ownership/worker custody differs')
    actual_raw=files.get('scan-start/actual-parent.json',b'');actual=helper.decode(actual_raw)
    if (hashlib.sha256(actual_raw).hexdigest()!=ready['actual_parent_observation_sha256'] or
        actual.get('container_id')!=cid or actual.get('nonce')!=nonce or
        resource_headroom(actual,helper,limits)!=resource_headroom(original,helper,{'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}) or
        files.get('raw/probe-host-boot-id.raw')!=files.get('scan-start/raw/startup-host-boot-id.raw') or
        not re.fullmatch(b'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\n',files.get('raw/probe-host-boot-id.raw',b''))):
        raise Refusal('resource export actual parent/host identity differs')
    expected={'docker-parent-headroom-proof.json','ownership.json','ownership.json.created','worker/container.id',
        'worker/headroom-worker.json','raw/probe-host-boot-id.raw','scan-start/actual-parent.json','scan-start/raw/startup-host-boot-id.raw'}
    expected.update('startup/'+name for name in names[:5])
    for observation,prefix in [(row,'') for row in proof['observations']]+[(actual,'scan-start/')]:
        if prefix=='':
            name='host-docker-parent-'+observation['moment']+'.json';expected.add(name)
            if helper.decode(files.get(name,b''))!=observation:raise Refusal('resource export observation buffer differs')
        rows=observation.get('raw_custody');seen=set()
        if type(rows) is not list or not rows:raise Refusal('resource export raw native custody absent')
        moment=observation['moment']
        required={'raw/'+moment+'-'+name for name in ('owned-live-inspect.json','init-stat-first.raw','init-stat-final.raw',
            'init-cgroup-first.raw','init-cgroup-final.raw','physical-meminfo.raw',
            'leaf-memory.max.raw','leaf-memory.swap.max.raw','leaf-pids.max.raw','leaf-cpu.max.raw')}
        for n,parent in enumerate(observation['docker_parent_ancestors']):
            if not parent.get('uncontrolled_root_fields_observed_absent'):
                required.update('raw/'+moment+'-parent-'+str(n)+'-'+field+'.raw' for field in ('memory.max','memory.current'))
        for row in rows:
            name=row.get('relative_path');path=PurePosixPath(name) if type(name) is str else PurePosixPath('/')
            if (name in seen or name not in required or path.is_absolute() or '..' in path.parts or
                type(row.get('bytes')) is not int or not 0<=row['bytes']<=65536 or not SHA.fullmatch(str(row.get('sha256')))):
                raise Refusal('resource export raw custody membership differs')
            seen.add(name);data=files.get(prefix+name)
            if data is None or len(data)!=row['bytes'] or hashlib.sha256(data).hexdigest()!=row['sha256']:
                raise Refusal('resource export raw native custody hash differs')
            expected.add(prefix+name)
        if seen!=required:raise Refusal('resource export raw native custody incomplete')
    if set(index['files'])!=expected:raise Refusal('resource completed export contains missing/extra raw input')
    witness=helper.decode(native['runtime-witness.json']);worker=helper.decode(native['worker-result.json']);lifecycle=helper.decode(native['container-lifecycle.json'])
    validate_witness(phase,authority,witness.get('witness',{}))
    if (witness.get('complete') is not True or witness.get('identity')!=identity or
        witness.get('core_contract_sha256')!=manifest['core_contract_sha256'] or
        witness.get('resource_ready_sha256')!=hashlib.sha256(native['resource-ready.json']).hexdigest() or
        witness['witness'].get('manager_pid')!=waiting['worker_private_pid'] or
        worker.get('complete') is not True or type(worker.get('exit')) is not int or worker['exit'] not in (0,1) or
        worker.get('resource_phase')!=phase or worker.get('resource_authority')!=authority or
        lifecycle.get('retired') is not True or lifecycle.get('container_id')!=cid or
        result.get('container_retired') is not True or result.get('container_id')!=cid or
        result.get('resource_phase')!=phase or result.get('resource_authority')!=authority or result.get('kernel_limits')!=limits or
        result.get('runtime_witness_sha256')!=hashlib.sha256(native['runtime-witness.json']).hexdigest() or
        result.get('lifecycle_sha256')!=hashlib.sha256(native['container-lifecycle.json']).hexdigest()):
        raise Refusal('resource export native witness/result/retirement differs')
    created_raw,_=helper.file_bytes(state/'container-ledgers'/(private_name+'.created'),65536,uid=os.getuid())
    created=helper.decode(created_raw)
    if (created.get('container_id')!=cid or created.get('labels')!={'workflow.osv.owner':nonce,'workflow.osv.commit':identity['GITHUB_SHA']}):
        raise Refusal('resource export full CID/owner ledger differs')
    require_retired_containers(state)
    value={'schema':'workflow.offline-scanner.resource-export-eligibility.v1','accepted':False,'report_eligible':True,
        'context':index['context'],'index_sha256':hashlib.sha256(index_raw).hexdigest(),'worker_private':private_name,
        'container_id':cid,'resource_authority':authority,'native_files':{name:hashlib.sha256(raw).hexdigest() for name,raw in native.items()}}
    if sealed:
        raw=files.get('eligible-index.json',b'')
        if not raw or helper.decode(raw)!=value or result.get('resource_export_sha256')!=hashlib.sha256(raw).hexdigest():
            raise Refusal('resource export lacks current sealed eligibility')
    return value,files

def seal_resource_export(state,phase,manifest,result):
    value,files=validate_resource_export(state,phase,manifest,result,sealed=False)
    path=state/('resource-export-'+phase+'.eligible.json');write(path,value)
    helper=headroom_module(manifest);raw,_=helper.file_bytes(path,65536,uid=os.getuid())
    target=REPOSITORY/'offline-evidence'/('resource-guard-'+phase)/'eligible-index.json'
    with target.open('xb') as stream:os.fchmod(stream.fileno(),0o600);stream.write(raw);stream.flush();os.fsync(stream.fileno())
    return hashlib.sha256(raw).hexdigest()

def copy_resource_export(state,phase,manifest,destination,*,completed=False):
    require_retired_containers(state)
    helper,index,index_raw,files=resource_export_buffers(state,phase,manifest)
    # A failed current attempt may retain diagnostic buffers; never promote it.
    eligible=state/('resource-export-'+phase+'.eligible.json')
    if type(completed) is not bool:raise Refusal('resource collector outcome must be exact')
    if completed:
        result,_=complete_result(state,phase.removeprefix('scan-'),manifest,index['context']['token'])
        _,files=validate_resource_export(state,phase,manifest,result)
    destination.mkdir(mode=0o700)
    for name in sorted(index['directories'],key=lambda name:(name.count('/'),name)):(destination/name).mkdir(mode=0o700)
    for name,raw in files.items():
        with (destination/name).open('xb') as stream:os.fchmod(stream.fileno(),0o600);stream.write(raw);stream.flush();os.fsync(stream.fileno())

def validate_resource_probe(value,phase,manifest,identity,guard):
    from osv_resources import ACTUAL_PARENT_STARTUP_GUARD_SHA256,limits_for_phase
    helper=headroom_module(manifest)
    if (value.get('accepted') is not False or value.get('measurement_complete') is not True or
        value.get('retirement_verified') is not True or value.get('exit')!=0 or value.get('actual_exit')!=0 or
        value.get('parent_phase')!=phase or value.get('identity')!=identity or
        identity.get('GITHUB_JOB')!='osv-production-proof' or value.get('input_retired') is not True or
        value.get('source_manifest_sha256')!=manifest['trusted_files']['docker-headroom-manifest.json'] or
        not SHA.fullmatch(str(value.get('container_id',''))) or not re.fullmatch('[0-9a-f]{32}',str(value.get('nonce',''))) or
        value.get('lifecycle',{}).get('retired') is not True or value.get('lifecycle',{}).get('container_id')!=value['container_id']):
        raise Refusal('fresh same-job owned2GiB headroom proof differs')
    moments=value.get('observations',[])
    if len(moments)!=2 or [x.get('moment') for x in moments]!=['before','after']:raise Refusal('partial resource parent observations')
    keys=[]
    for row in moments:
        if row.get('container_id')!=value['container_id'] or row.get('nonce')!=value['nonce']:
            raise Refusal('owned probe CID/nonce differs')
        helper.cgroup_path(('0::/'+row.get('host_init_cgroup','')+'\n').encode(),value['container_id'])
        keys.append(resource_headroom(row,helper,limits_for_phase()))
    if (keys[0]!=keys[1] or helper.process_identity(moments[0]['host_init'])!=helper.process_identity(moments[1]['host_init']) or
        moments[0]['own_container_cgroup_identity']!=moments[1]['own_container_cgroup_identity']):
        raise Refusal('owned probe parent or init rebound')
    if guard!=ACTUAL_PARENT_STARTUP_GUARD_SHA256 or guard!=resource_guard_digest(manifest):raise Refusal('startup guard source binding differs')
    return moments[-1]

def resource_probe(phase,manifest,state,budget):
    from osv_resources import OWNER_APPROVAL_SHA256,ACTUAL_PARENT_STARTUP_GUARD_SHA256,SCAN_MEMORY
    helper=headroom_module(manifest);owned(state)
    output=Path(tempfile.mkdtemp(prefix='osv-headroom-',dir=Path(os.environ['RUNNER_TEMP'])));output.chmod(0o700)
    started=time.monotonic();budget.left();code=helper.run(output,True,parent_phase=phase,phase_deadline=budget.deadline);budget.left()
    try:
        if code!=0:raise Refusal('fresh owned headroom probe did not pass')
        raw,_=helper.file_bytes(output/'docker-parent-headroom-proof.json',1048576,uid=os.getuid());value=helper.decode(raw)
        guard=ACTUAL_PARENT_STARTUP_GUARD_SHA256
        observation=validate_resource_probe(value,phase,manifest,{key:os.environ[key] for key in helper.IDENTITY},guard)
        host_identity=resource_host_identity(helper,output,'probe')
        if time.monotonic()-started>=180:raise Refusal('headroom probe exceeded existing phase budget')
        return {'phase':phase,'memory_bytes':SCAN_MEMORY,'owner_approval_sha256':OWNER_APPROVAL_SHA256,
            'startup_guard_sha256':guard,'actual_headroom_receipt_sha256':hashlib.sha256(raw).hexdigest(),
            'core_contract_sha256':manifest['core_contract_sha256']}, {'output':str(output),'observation':observation,
            'host_identity_sha256':host_identity,'recorded_monotonic':time.monotonic(),'receipt_sha256':hashlib.sha256(raw).hexdigest()}
    except Exception:
        resource_probe_export(output,phase,helper,state,manifest);raise

def resource_startup_observer(container,row,private,request,request_sha,selection,manifest,budget):
    helper=headroom_module(manifest);probe=selection['probe'];ready=private/'resource-ready.json'
    if ready.exists() or ready.is_symlink() or ready.with_name(ready.name+'.committed').exists():raise Refusal('resource handoff was precreated')
    waiting=private/'resource-waiting.json';waiting_commit=waiting.with_name(waiting.name+'.committed');until=time.monotonic()+10
    while not waiting_commit.exists():
        budget.left()
        if time.monotonic()>=until:raise Refusal('resource worker readiness deadline exceeded')
        time.sleep(0.05)
    helper.file_bytes(waiting_commit,0,uid=os.getuid())
    raw,_=helper.file_bytes(waiting,65536,uid=os.getuid());value=helper.decode(raw)
    if (value.get('schema')!='workflow.offline-scanner.resource-waiting.v1' or value.get('nonce')!=container.nonce or
        value.get('request_sha256')!=request_sha or value.get('resource_phase')!=selection['phase'] or
        value.get('resource_authority')!=selection['authority'] or value.get('container_id')!=container.identifier or
        request.get('resource_container_id')!=container.identifier or value.get('core_contract_sha256')!=manifest['core_contract_sha256'] or
        selection['authority']['core_contract_sha256']!=manifest['core_contract_sha256'] or
        value.get('native_started') is not False or type(value.get('worker_private_pid')) is not int or value['worker_private_pid']<=0):
        raise Refusal('worker waiting source/request binding differs')
    if not 0<=time.monotonic()-probe['recorded_monotonic']<=30:raise Refusal('resource probe is stale before actual startup')
    host=Path(probe['output'])/'scan-start';host.mkdir(mode=0o700);(host/'raw').mkdir(mode=0o700)
    if resource_host_identity(helper,host,'startup')!=probe['host_identity_sha256']:raise Refusal('resource proof belongs to a different VM')
    current=container.live_identity()
    if current['State']['Pid']!=row['State']['Pid']:raise Refusal('resource init PID rebound')
    observed=helper.capture(container,current,host,'before')
    after=container.live_identity()
    if after['State']['Pid']!=current['State']['Pid']:raise Refusal('resource init PID rebound after observation')
    original=probe['observation']
    if (resource_headroom(observed,helper,container.limits)!=resource_headroom(original,helper,{'memory.max':'2147483648','memory.swap.max':'0','pids.max':'256'}) or
        observed['container_id']!=container.identifier or observed['nonce']!=container.nonce or
        observed['host_init']['pid']!=current['State']['Pid']):
        raise Refusal('enlarged actual Docker parent identity/headroom differs from fresh probe')
    helper.cgroup_path(('0::/'+observed['host_init_cgroup']+'\n').encode(),container.identifier)
    observed_sha=helper.write(host/'actual-parent.json',observed);budget.left()
    handoff={'schema':'workflow.offline-scanner.resource-ready.v1','resource_phase':selection['phase'],
        'resource_authority':selection['authority'],'core_contract_sha256':manifest['core_contract_sha256'],
        'nonce':container.nonce,'container_id':container.identifier,'request_sha256':request_sha,
        'actual_parent_observation_sha256':observed_sha,'actual_kernel_limits':container.limits,
        'requested_memory_bytes':container.requested_memory,'native_execution_permitted':True}
    write_request_resource(ready,handoff,committed=True);budget.left()

def wait_resource_ready(request,request_sha,manifest):
    from osv_resources import SCAN_MEMORY,authorize_phase,requested_memory
    expected=authorize_phase(request['resource_phase'],request.get('resource_authority'))
    if requested_memory(request['resource_phase'])!=SCAN_MEMORY:return None
    if request['resource_authority']['core_contract_sha256']!=manifest['core_contract_sha256']:raise Refusal('worker phase/core authority differs')
    helper=headroom_module(manifest);ready=Path('/out/resource-ready.json');waiting=Path('/out/resource-waiting.json')
    ready_commit=ready.with_name(ready.name+'.committed');waiting_commit=waiting.with_name(waiting.name+'.committed')
    if any(p.exists() or p.is_symlink() for p in (ready,waiting,ready_commit,waiting_commit)):raise Refusal('resource startup handoff was precreated')
    write_request_resource(waiting,{'schema':'workflow.offline-scanner.resource-waiting.v1','nonce':request['resource_nonce'],
        'request_sha256':request_sha,'container_id':request['resource_container_id'],
        'resource_phase':request['resource_phase'],'resource_authority':request['resource_authority'],
        'core_contract_sha256':manifest['core_contract_sha256'],'native_started':False,'worker_private_pid':os.getpid()},committed=True)
    until=time.monotonic()+30
    while not ready_commit.exists():
        if time.monotonic()>=until:raise Refusal('resource host-ready deadline exceeded')
        time.sleep(0.05)
    helper.file_bytes(ready_commit,0,uid=os.getuid())
    raw,_=helper.file_bytes(ready,65536,uid=os.getuid());value=helper.decode(raw)
    if (value.get('schema')!='workflow.offline-scanner.resource-ready.v1' or value.get('nonce')!=request['resource_nonce'] or
        value.get('request_sha256')!=request_sha or value.get('resource_phase')!=request['resource_phase'] or
        value.get('resource_authority')!=request['resource_authority'] or value.get('core_contract_sha256')!=manifest['core_contract_sha256'] or
        value.get('container_id')!=request['resource_container_id'] or
        value.get('actual_kernel_limits')!=expected or value.get('requested_memory_bytes')!=SCAN_MEMORY or value.get('native_execution_permitted') is not True or
        not SHA.fullmatch(str(value.get('container_id',''))) or not SHA.fullmatch(str(value.get('actual_parent_observation_sha256','')))):
        raise Refusal('resource host-ready binding differs')
    return hashlib.sha256(raw).hexdigest()

def worker(request_path, manifest, profile):
    helper=headroom_module(manifest)
    request_raw,_=helper.file_bytes(request_path,MAX_JSON,uid=os.getuid());request=helper.decode(request_raw)
    request_sha=hashlib.sha256(request_raw).hexdigest();state = owned(Path(request['state']))
    private = owned(Path(request['private']))
    if (request_path != Path('/out/request.json') or private.parent != state or not private.name.startswith('worker-') or
        request['core_contract_sha256'] != manifest['core_contract_sha256'] or
        request['prepared_sha256'] != digest(state / 'prepared.json')):
        raise Refusal('production worker request binding differs')
    from osv_resources import authorize_phase,source_proposal
    if profile.get('phase_memory_proposal')!=source_proposal() or request.get('resource_phase')!=request['phase']:
        raise Refusal('worker immutable phase resource proposal differs')
    selected_limits=authorize_phase(request['resource_phase'],request.get('resource_authority'))
    ready_sha=wait_resource_ready(request,request_sha,manifest)
    selection={'phase':request['resource_phase'],'authority':request.get('resource_authority'),'limits':selected_limits}
    import importlib.util
    spec = importlib.util.spec_from_file_location('container_worker', HERE / 'container-worker.py')
    witness_module = importlib.util.module_from_spec(spec);spec.loader.exec_module(witness_module)
    observed = witness_module.witness(request)
    write(private / 'runtime-witness.json', {'complete': True, 'identity': request['identity'],
        'core_contract_sha256': manifest['core_contract_sha256'], 'witness': observed,'resource_ready_sha256':ready_sha})
    resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_JSON, MAX_JSON))
    from osv_producer import run_one
    from osv_io import bounded_run, Budget
    runtime = request['runtime']
    tools, go = supplier(state, manifest, runtime)
    phase = request['phase']; label = 'baseline' if phase == 'scan-baseline' else 'candidate'
    child_env = env(go, state, runtime, analysis=phase.startswith('scan-'))
    if phase.startswith('scan-'):
        out, blocking = run_one(state / (label + '-source'), profile['inputs'], tools, go,
            state / 'public-db', load(state / 'public-database-receipts.json'), datetime.now(timezone.utc), state / 'runs', child_env,
            admitted_supplier=supplier_contract(manifest, runtime), cgroup_witness=observed,
            core_contract_sha256=manifest['core_contract_sha256'])
        write(private / 'worker-result.json', {'complete': True, 'proof': str(out),
            'blocking': sorted(blocking), 'label': label, 'exit': 1 if blocking else 0,
            'socket_denial': 'EPERM', 'kernel_limits': observed['kernel_limits'],
            'resource_phase':selection['phase'],'resource_authority':selection['authority']})
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
    source = prepared['sources'][label]
    verify_fixture_projection(state, label, source, manifest,
        manual=prepared.get('event') == 'workflow_dispatch' and prepared.get('automatic_admission') is False)
    if (result.get('source_subject') != source_subject(source) or
        result.get('declared_catalogs') != finite_catalogs(source, receipt['manifests'], manifest)):
        raise Refusal('completed result conflates original and derived source/catalogs')
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
    completed={label:complete_result(state,label,manifest) for label in load(state/'prepared.json')['sources']}
    resource_exports={};resource_bytes=0
    for label,(result,proof) in completed.items():
        if result.get('resource_authority') is not None:
            phase='scan-'+label
            if phase not in ('scan-baseline','scan-candidate') or result.get('resource_phase')!=phase:
                raise Refusal('unrequested completed resource export phase')
            _,buffers=validate_resource_export(state,phase,manifest,result)
            resource_bytes+=sum(map(len,buffers.values()))
            resource_exports['resource-guard-'+phase]=phase
    destination = REPOSITORY / 'offline-evidence'
    if destination.exists() or destination.is_symlink():
        owned(destination)
        for item in destination.iterdir():
            if item.name in resource_exports:
                owned(item)
                continue
            if not re.fullmatch(r'failure-(?:prepare|scan|report|gate)-(?:scheduled|differential|baseline|candidate)\.json', item.name):
                raise Refusal('refuse existing unrelated evidence publication path')
            regular(item)
    else:
        destination.mkdir(mode=0o700)
    total = resource_bytes
    for name in ['prepared.json', 'public-database-receipts.json', 'application-metadata-receipt.json']:
        source = state / name; regular(source)
        total+=source.stat().st_size
        if total>128*1024*1024:raise Refusal('evidence publication aggregate bound exceeded')
        shutil.copyfile(source, destination / name); (destination / name).chmod(0o600)
    for label,(result,proof) in completed.items():
        output = destination / label; output.mkdir(mode=0o700)
        write(output / 'result.json', result)
        total+=(output/'result.json').stat().st_size
        for source in proof.iterdir():
            if not re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner|database-validation|analysis-validation)\.(?:stdout|stderr|rc|memory-(?:before|after)\.json)|(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.native-memory\.jsonl|receipt\.json|results\.json|database-validation\.json', source.name):
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
    if phase == '--database-worker' and len(sys.argv) == 3:
        from osv_producer import database_worker
        return database_worker(Path(sys.argv[2]),manifest['core_contract_sha256'])
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
            if re.fullmatch(r'(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner|database-validation|analysis-validation)\.(?:stdout|stderr|rc|memory-(?:before|after)\.json)|(?:root[0-9]+-(?:source-catalog|govulncheck)|scanner)\.native-memory\.jsonl|receipt\.json|results\.json|database-validation\.json', source.name):
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
