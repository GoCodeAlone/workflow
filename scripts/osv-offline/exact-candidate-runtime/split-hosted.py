"""One approved split-job trial. Stdlib-only until complete cold source verification.

Acquisition never executes the candidate. Runtime receives no GitHub credential.
The unchanged receiver/launcher consume an explicitly derived transport receipt.
"""
import time
ENTRY_STARTED = time.monotonic()

import datetime
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import signal
import stat
import sys

HERE = Path(__file__).resolve().parent
PREFIX = 'scripts/osv-offline/exact-candidate-runtime/'
WORKFLOW = '.github/workflows/copilot-setup-steps.yml'
CORE = {'candidate_contract.py', 'candidate_enforcement.py', 'candidate_controls.py',
        'container-worker.py', 'run-candidate-runtime.py', 'receive-candidate.py',
        'osv_container.py', 'container-seccomp.json'}
BUNDLE = {PREFIX + n for n in CORE | {'hosted.py', 'source-manifest.json', 'split-hosted.py'}} | {WORKFLOW}
REPO = 'GoCodeAlone/workflow'
REF = 'refs/heads/prep/osv-prerequisites-20261011-r1'
OWNER = '8234a43109f6fc1c76eb10cff8e06f2d3391556c891cf06d2b19223f505beb9c'
ASSET_SHA = '9819a5b582e1f0b6ab135b75c5113baf36f7507a6904e64e2984c318447e8bdf'
ASSET_SIZE = 115900576
SOURCE = '81b9b1df2386f7d44c8464c999a4ea0919813526'
RELEASE = 409874491
ASSET = 631609441
TAG = 'unpublished-validation-9819a5-20261011-r1'
NAME = 'wfctl-candidate81b9-linux-amd64'
PUBLIC_HOST = 'raw.githubusercontent.com'
MAX_FILE = 65536
MAX_BUNDLE = 524288

class Refusal(RuntimeError):
    pass

class Budget:
    def __init__(self, seconds):
        self.deadline = time.monotonic() + seconds
    def left(self):
        left = self.deadline - time.monotonic()
        if left <= 0:
            raise Refusal('fixed split phase deadline')
        return left

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def pairs(values):
    result = {}
    for key, value in values:
        if key in result:
            raise Refusal('duplicate JSON key')
        result[key] = value
    return result

def parse(raw):
    return json.loads(raw, object_pairs_hook=pairs,
        parse_constant=lambda _: (_ for _ in ()).throw(Refusal('nonfinite JSON')))

def require(test, reason):
    if not test:
        raise Refusal(reason)

def fingerprint(s):
    return tuple(getattr(s, k) for k in ('st_dev', 'st_ino', 'st_mode', 'st_uid',
        'st_gid', 'st_size', 'st_mtime_ns', 'st_ctime_ns'))

def regular(path, limit):
    info = path.lstat()
    require(path.resolve() == path and stat.S_ISREG(info.st_mode) and
        info.st_uid == os.getuid() and info.st_nlink == 1 and
        not info.st_mode & 0o6022 and info.st_size <= limit, 'unsafe bounded input')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        require(fingerprint(os.fstat(fd)) == fingerprint(info), 'input rebound')
        raw = bytearray()
        while True:
            block = os.read(fd, 65536)
            if not block:
                break
            raw.extend(block)
            require(len(raw) <= limit, 'input size bound')
        require(fingerprint(os.fstat(fd)) == fingerprint(info), 'input changed')
        return bytes(raw)
    finally:
        os.close(fd)

def write(path, raw, mode=0o400):
    require(len(raw) <= MAX_BUNDLE, 'owned output bound')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())

def seal(path, value, budget):
    write(path, (json.dumps(value, sort_keys=True, indent=2) + '\n').encode())
    budget.left()

def identity(job):
    require(os.environ.get('GITHUB_EVENT_NAME') == 'workflow_dispatch' and
        os.environ.get('GITHUB_REPOSITORY') == REPO and
        os.environ.get('GITHUB_REF') == REF and os.environ.get('GITHUB_JOB') == job and
        os.environ.get('GITHUB_RUN_ATTEMPT') == '1' and
        re.fullmatch('[1-9][0-9]*', os.environ.get('GITHUB_RUN_ID', '')) and
        re.fullmatch('[0-9a-f]{40}', os.environ.get('GITHUB_SHA', '')) and
        os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted' and
        platform.system() == 'Linux' and platform.machine() == 'x86_64' and
        os.geteuid() != 0, 'exact finite hosted identity required')
    return {k: os.environ[k] for k in ('GITHUB_REPOSITORY', 'GITHUB_SHA',
        'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB')}

def utc(text):
    require(isinstance(text, str) and re.fullmatch('[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z', text),
        'literal ASCII UTC time required')
    return datetime.datetime.strptime(text, '%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=datetime.timezone.utc)

def admission(job):
    require(sys.flags.isolated and sys.flags.no_site and sys.dont_write_bytecode and
        sys.pycache_prefix and not os.path.lexists(sys.pycache_prefix), 'isolated absent bytecode prefix required')
    current = identity(job)
    raw = os.environ.get('OSV_RUNTIME_SOURCE_REVIEW', '').encode()
    digest = os.environ.get('OSV_RUNTIME_SOURCE_REVIEW_SHA', '')
    trial_raw = os.environ.get('OSV_CANDIDATE_TRIAL', '').encode()
    trial_sha = os.environ.get('OSV_CANDIDATE_TRIAL_SHA', '')
    require(len(raw) <= MAX_FILE and re.fullmatch('[0-9a-f]{64}', digest) and sha(raw) == digest,
        'exact bounded source review required')
    require(len(trial_raw) <= MAX_FILE and re.fullmatch('[0-9a-f]{64}', trial_sha) and sha(trial_raw) == trial_sha,
        'exact bounded trial required')
    review = parse(raw); trial = parse(trial_raw)
    keys = {'schema', 'complete', 'accepted', 'source_manifest_sha256', 'cold_wrapper_sha256',
        'workflow_sha256', 'security_decision_sha256', 'antagonistic_decision_sha256',
        'security_critical', 'security_important', 'security_advisory', 'antagonistic_critical',
        'antagonistic_important', 'antagonistic_advisory', 'allows_native_controls',
        'candidate_runtime_authorized', 'artifact_sha256', 'operation', 'workflow_ref',
        'split_wrapper_sha256', 'source_binding_sha256', 'source_rows'}
    require(set(review) == keys and review['schema'] == 'workflow.GO5932.source-review-admission.v1' and
        review['complete'] is True and review['accepted'] is False and
        review['allows_native_controls'] is True and review['candidate_runtime_authorized'] is True and
        review['artifact_sha256'] == ASSET_SHA and review['operation'] == ['/candidate/wfctl', 'schema'] and
        review['workflow_ref'] == REF, 'exact operation source admission differs')
    for name in ('security_critical', 'security_important', 'security_advisory',
                 'antagonistic_critical', 'antagonistic_important', 'antagonistic_advisory'):
        require(type(review[name]) is int and review[name] == 0, 'two zero-finding source reviews required')
    for name in ('source_manifest_sha256', 'cold_wrapper_sha256', 'workflow_sha256',
                 'security_decision_sha256', 'antagonistic_decision_sha256', 'split_wrapper_sha256', 'source_binding_sha256'):
        require(isinstance(review[name], str) and re.fullmatch('[0-9a-f]{64}', review[name]), 'source pin absent')
    rows = review['source_rows']
    require(isinstance(rows, dict) and set(rows) == BUNDLE, 'complete fixed cold bundle required')
    total = 0
    for row in rows.values():
        require(isinstance(row, dict) and set(row) == {'bytes', 'sha256'} and
            type(row['bytes']) is int and 0 < row['bytes'] <= MAX_FILE and
            isinstance(row['sha256'], str) and re.fullmatch('[0-9a-f]{64}', row['sha256']), 'invalid source row')
        total += row['bytes']
    require(total <= MAX_BUNDLE and rows[WORKFLOW]['sha256'] == review['workflow_sha256'] and
        rows[PREFIX + 'split-hosted.py']['sha256'] == review['split_wrapper_sha256'] and
        rows[PREFIX + 'hosted.py']['sha256'] == review['cold_wrapper_sha256'] and
        rows[PREFIX + 'source-manifest.json']['sha256'] == review['source_manifest_sha256'], 'cold guard/source joins differ')
    required_trial = {'schema', 'owner_approval_sha256', 'trial_id', 'not_before_utc', 'expires_utc',
        'head', 'review_sha256', 'issuer_ledger_sha256', 'dispatches', 'accepted'}
    require(set(trial) == required_trial and trial['schema'] == 'workflow.GO5932.single-trial.v1' and
        trial['owner_approval_sha256'] == OWNER and trial['head'] == current['GITHUB_SHA'] and
        trial['review_sha256'] == digest and type(trial['dispatches']) is int and trial['dispatches'] == 1 and trial['accepted'] is False and
        re.fullmatch('[0-9a-f]{64}', trial['trial_id']) and re.fullmatch('[0-9a-f]{64}', trial['issuer_ledger_sha256']),
        'exact approved one-invocation trial differs')
    start, end = utc(trial['not_before_utc']), utc(trial['expires_utc'])
    require(0 < (end-start).total_seconds() <= 7200 and
        start <= datetime.datetime.now(datetime.timezone.utc) < end, 'operational trial window expired/not begun')
    require(sha(regular(HERE / 'split-hosted.py', MAX_FILE)) == review['split_wrapper_sha256'], 'cold wrapper differs')
    return current, raw, digest, review, trial_raw, trial_sha, trial

def verify_bundle(repo, review):
    for name, row in review['source_rows'].items():
        raw = regular(repo / name, MAX_FILE)
        require(len(raw) == row['bytes'] and sha(raw) == row['sha256'], 'whole source bundle differs')
    man = parse(regular(repo / PREFIX / 'source-manifest.json', MAX_FILE))
    require(set(man['files']) == CORE, 'unchanged eight-helper manifest differs')
    for name, row in man['files'].items():
        require(set(row) == {'bytes', 'mode', 'sha256'} and row['mode'] == '0644' and
            review['source_rows'][PREFIX + name] == {'bytes': row['bytes'], 'sha256': row['sha256']},
            'transitive helper pin differs')

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module

def helpers(repo, review):
    verify_bundle(repo, review)
    sys.path.insert(0, str(repo / PREFIX))
    # FIRST subject imports occur after every bundle byte and transitive pin.
    c = load('split_contract', repo / PREFIX / 'candidate_contract.py')
    h = load('split_old_native', repo / PREFIX / 'hosted.py')
    return c, h

def public_source(head, name, budget):
    # No token, proxy/netrc, redirect, dynamic URL, retry or executable acquisition.
    require(head == os.environ['GITHUB_SHA'] and name in BUNDLE, 'fixed public source only')
    require(signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0), 'existing alarm refused')
    previous = signal.getsignal(signal.SIGALRM)
    def expire(_signum, _frame):
        raise Refusal('public source deadline')
    connection = None
    signal.signal(signal.SIGALRM, expire)
    try:
        signal.setitimer(signal.ITIMER_REAL, min(8, budget.left()))
        connection = http.client.HTTPSConnection(PUBLIC_HOST, timeout=min(6, budget.left()))
        connection.request('GET', '/' + REPO + '/' + head + '/' + name,
            headers={'Accept': 'application/octet-stream', 'Connection': 'close'})
        response = connection.getresponse()
        require(response.status == 200, 'public source HTTP status refused')
        require(response.headers.get_all('Content-Encoding', []) in ([], ['identity']), 'source encoding refused')
        raw = response.read(MAX_FILE + 1)
        require(0 < len(raw) <= MAX_FILE, 'public source bound exceeded')
        budget.left(); return raw
    finally:
        try:
            if connection is not None:
                connection.close()
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0); signal.signal(signal.SIGALRM, previous)

def output(values):
    path = Path(os.environ['GITHUB_OUTPUT']); before = path.lstat(); regular(path, MAX_FILE)
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
    with os.fdopen(fd, 'ab') as stream:
        require(fingerprint(os.fstat(stream.fileno())) == fingerprint(before), 'current step output rebound')
        for key, value in values.items():
            require(re.fullmatch('[a-z][a-z0-9_]*', key) and '\n' not in str(value), 'fixed step output required')
            stream.write((key + '=' + str(value) + '\n').encode())
        stream.flush(); os.fsync(stream.fileno())

def fetch(root, current, review_raw, review_sha, review, trial_raw, trial_sha, trial):
    budget = Budget(60)
    started = time.monotonic()
    require(root.resolve() == root and root.parent == Path(os.environ['RUNNER_TEMP']) and not root.exists(), 'fresh fixed temporary source root required')
    root.mkdir(mode=0o700); repo = root / 'bundle'; repo.mkdir(mode=0o700)
    for name, row in sorted(review['source_rows'].items()):
        raw = public_source(current['GITHUB_SHA'], name, budget)
        require(len(raw) == row['bytes'] and sha(raw) == row['sha256'], 'authenticated public source differs')
        target = repo / name; target.parent.mkdir(mode=0o700, parents=True, exist_ok=True); write(target, raw)
    verify_bundle(repo, review)
    for name, raw in [('review.json', review_raw), ('trial.json', trial_raw)]:
        write(root / name, raw)
    seal(root / 'state.json', {'identity': current, 'trial_sha256': trial_sha, 'review_sha256': review_sha,
        'invocation_id': trial['trial_id'], 'started_monotonic': started, 'accepted': False}, budget)
    output({'evidence': str(root)})
    budget.left()

def gh_get_arguments(endpoint, binary_sink, help_raw):
    require(endpoint in {'repos/' + REPO + '/releases/' + str(RELEASE),
        'repos/' + REPO + '/releases/assets/' + str(ASSET)}, 'fixed approved GET endpoint required')
    args = ['/usr/bin/gh', 'api', '--method', 'GET']
    if binary_sink:
        if re.search(rb'(?m)^\s*--allow-escape-sequences(?:\s|$)', help_raw):
            args += ['--allow-escape-sequences']
        args += ['-H', 'Accept: application/octet-stream']
    return args + [endpoint]

def acquire(root, current, review_raw, review_sha, review, trial_raw, trial_sha, trial, token):
    c, h = helpers(root / 'bundle', review)
    state = parse(regular(root / 'state.json', MAX_FILE))
    require(state['identity'] == current and state['review_sha256'] == review_sha and
        state['trial_sha256'] == trial_sha and state['invocation_id'] == trial['trial_id'], 'fresh acquisition state differs')
    budget = c.Budget(200); budget.deadline = min(budget.deadline, state['started_monotonic'] + 270)
    gh = Path('/usr/bin/gh'); meta = gh.lstat()
    require(gh.resolve() == gh and stat.S_ISREG(meta.st_mode) and meta.st_uid == 0 and
        not meta.st_mode & 0o6022 and meta.st_mode & 0o111 and token, 'fixed trusted CLI/token required')
    config = root / 'gh-config'; config.mkdir(mode=0o700)
    env = h.host_env(); env.update(GH_TOKEN=token, GH_HOST='github.com', GH_CONFIG_DIR=str(config), GH_PROMPT_DISABLED='1')
    help_raw, help_evidence = h.native(['/usr/bin/gh', 'api', '--help'],
        h.host_env(), min(5, budget.left()), MAX_FILE)
    calls = [help_evidence]
    def get(endpoint, limit, sink=None):
        args = gh_get_arguments(endpoint, sink is not None, help_raw)
        try:
            raw, evidence = h.native(args, env, min(180 if sink else 15, budget.left()), limit, sink)
        except BaseException as error:
            if hasattr(error, 'native'):
                error.native.update(fixed_get_endpoint=endpoint,
                    fixed_operation='binary-bytes' if sink is not None else 'metadata',
                    completed_fixed_gets=len(calls)-1)
            raise
        calls.append(evidence); return raw, evidence
    endpoint = 'repos/' + REPO + '/releases/' + str(RELEASE)
    release, _ = get(endpoint, MAX_FILE); release = parse(release)
    require(release.get('id') == RELEASE and release.get('draft') is True and
        release.get('published_at') is None and release.get('tag_name') == TAG, 'approved unpublished draft differs')
    asset_endpoint = 'repos/' + REPO + '/releases/assets/' + str(ASSET)
    asset, _ = get(asset_endpoint, MAX_FILE); asset = parse(asset)
    require(asset.get('id') == ASSET and asset.get('size') == ASSET_SIZE and
        asset.get('digest') == 'sha256:' + ASSET_SHA and asset.get('name') == NAME and asset.get('state') == 'uploaded', 'approved asset differs')
    carrier = root / 'carrier'; carrier.mkdir(mode=0o700)
    with (carrier / 'wfctl').open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        _, evidence = get(asset_endpoint, ASSET_SIZE, stream)
        stream.flush(); os.fsync(stream.fileno()); os.fchmod(stream.fileno(), 0o400)
    require(evidence['stdout_bytes'] == ASSET_SIZE and evidence['stdout_sha256'] == ASSET_SHA, 'exact downloaded bytes differ')
    actual = c.hash_artifact(carrier / 'wfctl', budget, mode=0o400, elf=True)
    after, _ = get(endpoint, MAX_FILE); after = parse(after)
    require(after.get('id') == RELEASE and after.get('draft') is True and
        after.get('published_at') is None and after.get('tag_name') == TAG, 'draft lifecycle drift')
    c.owned(config, True); config.rmdir()
    verify_bundle(root / 'bundle', review)
    for name, row in review['source_rows'].items():
        target = carrier / 'bundle' / name; target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        write(target, regular(root / 'bundle' / name, row['bytes']))
    write(carrier / 'review.json', review_raw); write(carrier / 'trial.json', trial_raw)
    receipt = {'schema': 'workflow.GO5932.split-acquisition.v1', 'complete': True, 'accepted': False,
        'candidate_execution': False, 'identity': current, 'invocation_id': trial['trial_id'],
        'trial_sha256': trial_sha, 'review_sha256': review_sha, 'source_binding_sha256': review['source_binding_sha256'],
        'source_commit': SOURCE, 'artifact_sha256': ASSET_SHA, 'artifact_bytes': ASSET_SIZE,
        'release_id': RELEASE, 'asset_id': ASSET, 'draft': True, 'published': False,
        'carrier_name': 'candidate9819-carrier-' + current['GITHUB_RUN_ID'] + '-1',
        'source_rows': review['source_rows'], 'actual_artifact': actual, 'native_calls': calls,
        'github_cli_sha256': sha(gh.read_bytes()),
        'binary_escape_flag_supported': bool(re.search(rb'(?m)^\s*--allow-escape-sequences(?:\s|$)', help_raw)),
        'provisional_until_actual_acquisition_job_success': True}
    c.seal(carrier / 'acquisition.json', receipt, budget)
    output({'carrier_directory': str(carrier), 'receipt_sha256': sha(regular(carrier / 'acquisition.json', MAX_FILE)),
        'invocation_id': trial['trial_id']})
    budget.left()

def finalize(root, current, review_sha, review, trial_sha, trial):
    state = parse(regular(root / 'state.json', MAX_FILE)); budget = Budget(10)
    require(time.monotonic() < state['started_monotonic'] + 270 and
        state['identity'] == current and state['review_sha256'] == review_sha and
        state['trial_sha256'] == trial_sha and os.environ.get('OSV_CARRIER_OUTCOME') == 'success', 'actual producer upload/deadline refused')
    require(re.fullmatch('[1-9][0-9]*', os.environ.get('OSV_CARRIER_ID', '')) and
        re.fullmatch('(?:sha256:)?[0-9a-f]{64}', os.environ.get('OSV_CARRIER_DIGEST', '')), 'actual artifact output absent')
    verify_bundle(root / 'carrier' / 'bundle', review)
    require(parse(regular(root / 'carrier/acquisition.json', MAX_FILE))['invocation_id'] == trial['trial_id'], 'producer final receipt differs')
    budget.left(); require(time.monotonic() < state['started_monotonic'] + 270, 'producer final deadline')

def carrier(root, current, review_raw, review_sha, review, trial_raw, trial_sha, trial):
    require(os.environ.get('OSV_PRODUCER_OUTCOME') == 'success' and
        re.fullmatch('[1-9][0-9]*', os.environ.get('OSV_CARRIER_ID', '')) and
        re.fullmatch('(?:sha256:)?[0-9a-f]{64}', os.environ.get('OSV_CARRIER_DIGEST', '')), 'actual same-run producer/service outputs required')
    required = {'wfctl', 'acquisition.json', 'review.json', 'trial.json'} | {'bundle/' + n for n in BUNDLE}
    directories = {str(parent) for name in required for parent in Path(name).parents if str(parent) != '.'}
    names = set()
    require(root.resolve() == root and root.is_dir() and not root.is_symlink(), 'canonical carrier root required')
    for path in root.rglob('*'):
        meta = path.lstat()
        require(meta.st_uid == os.getuid() and not meta.st_mode & 0o6022 and path.resolve() == path, 'unsafe delivered object')
        if stat.S_ISDIR(meta.st_mode):
            require(path.relative_to(root).as_posix() in directories, 'unexpected carrier directory')
            continue
        require(stat.S_ISREG(meta.st_mode) and meta.st_nlink == 1, 'nonregular delivered object')
        names.add(path.relative_to(root).as_posix())
        require(len(names) <= len(required), 'unexpected carrier member')
    require(names == required, 'complete exact carrier file set required')
    require(regular(root / 'review.json', MAX_FILE) == review_raw and
        regular(root / 'trial.json', MAX_FILE) == trial_raw, 'carrier/current admission differs')
    verify_bundle(root / 'bundle', review)
    receipt_raw = regular(root / 'acquisition.json', MAX_FILE)
    require(sha(receipt_raw) == os.environ.get('OSV_PRODUCER_RECEIPT_SHA'), 'actual producer receipt digest differs')
    receipt = parse(receipt_raw); expected_identity = dict(current, GITHUB_JOB='exact-candidate-acquisition')
    expected = {'schema': 'workflow.GO5932.split-acquisition.v1', 'complete': True, 'accepted': False,
        'candidate_execution': False, 'identity': expected_identity, 'invocation_id': trial['trial_id'],
        'trial_sha256': trial_sha, 'review_sha256': review_sha, 'source_binding_sha256': review['source_binding_sha256'],
        'source_commit': SOURCE, 'artifact_sha256': ASSET_SHA, 'artifact_bytes': ASSET_SIZE,
        'release_id': RELEASE, 'asset_id': ASSET, 'draft': True, 'published': False,
        'carrier_name': 'candidate9819-carrier-' + current['GITHUB_RUN_ID'] + '-1', 'source_rows': review['source_rows']}
    require(all(receipt.get(k) == v for k, v in expected.items()), 'cross-job receipt provenance differs')
    require(os.environ.get('OSV_INVOCATION_ID') == trial['trial_id'], 'current invocation output differs')
    return receipt

def receive(root, proof, current, review_raw, review_sha, review, trial_raw, trial_sha, trial):
    acquisition = carrier(root, current, review_raw, review_sha, review, trial_raw, trial_sha, trial)
    require(proof.parent == Path(os.environ['RUNNER_TEMP']) and not proof.exists(), 'fresh fixed runtime proof root required')
    proof.mkdir(mode=0o700); (proof / 'receiver').mkdir(mode=0o700); (proof / 'runtime').mkdir(mode=0o700)
    c, h = helpers(root / 'bundle', review)
    fd = os.open(root / 'wfctl', os.O_RDONLY | os.O_NOFOLLOW)
    try:
        require(stat.S_ISREG(os.fstat(fd).st_mode) and os.fstat(fd).st_uid == os.getuid(), 'unsafe delivered candidate')
        os.fchmod(fd, 0o400)
    finally:
        os.close(fd)
    budget = c.Budget(30); actual = c.hash_artifact(root / 'wfctl', budget, mode=0o400, elf=True)
    # A derived adapter does not relabel the original acquisition identity.
    derived = {'schema': 'workflow.GO5932.authenticated-candidate-delivery.v1', 'complete': True,
        'accepted': False, 'candidate_execution': False, 'identity': current, 'invocation_id': trial['trial_id'],
        'source_commit': SOURCE, 'artifact_sha256': ASSET_SHA, 'artifact_bytes': ASSET_SIZE,
        'derivation': 'verified same-run split acquisition plus actual needs producer success',
        'producer_identity': acquisition['identity'], 'producer_receipt_sha256': os.environ['OSV_PRODUCER_RECEIPT_SHA'],
        'artifact_id': os.environ['OSV_CARRIER_ID'], 'artifact_digest': os.environ['OSV_CARRIER_DIGEST'],
        'actual_artifact': actual}
    c.seal(proof / 'derived-transport.json', derived, budget)
    c.fresh_write(proof / 'review.json', review_raw, budget)
    c.fresh_write(proof / 'trial.json', trial_raw, budget)
    source = root / 'bundle' / PREFIX
    load('split_receiver', source / 'receive-candidate.py').stage(root / 'wfctl', proof / 'receiver',
        proof / 'derived-transport.json', sha(regular(proof / 'derived-transport.json', MAX_FILE)),
        trial['trial_id'], current, budget)
    output({'evidence': str(proof)}); budget.left()

def runtime(root, proof, current, review_raw, review_sha, review, trial_raw, trial_sha, trial):
    carrier(root, current, review_raw, review_sha, review, trial_raw, trial_sha, trial)
    require(os.environ.get('OSV_RECEIVER_OUTCOME') == 'success', 'actual receiver outcome required')
    require(regular(proof / 'review.json', MAX_FILE) == review_raw and regular(proof / 'trial.json', MAX_FILE) == trial_raw,
        'fresh runtime admission changed')
    c, h = helpers(root / 'bundle', review); budget = c.Budget(90)
    budget.deadline = ENTRY_STARTED + 90
    source = root / 'bundle' / PREFIX; receiver = proof / 'receiver/receiver-proof.json'
    load('split_runtime', source / 'run-candidate-runtime.py').run(proof / 'runtime', 'candidate',
        proof / 'review.json', review_sha, receiver, sha(regular(receiver, MAX_FILE)), trial['trial_id'],
        current, receiver_outcome='success', budget=budget)
    verify_bundle(root / 'bundle', review); budget.left()
    admission('exact-candidate-runtime'); budget.left()

def main():
    token = os.environ.pop('GH_TOKEN', None)
    require(len(sys.argv) == 4 and sys.argv[3] == '--execute' and
        sys.argv[1] in {'fetch', 'acquire', 'finalize', 'receive', 'runtime'}, 'fixed split phase required')
    phase = sys.argv[1]; root = Path(sys.argv[2])
    job = 'exact-candidate-acquisition' if phase in {'fetch', 'acquire', 'finalize'} else 'exact-candidate-runtime'
    current, raw, digest, review, traw, tdigest, trial = admission(job)
    require((phase == 'acquire' and token) or (phase != 'acquire' and token is None), 'token belongs only to acquisition read phase')
    if phase == 'fetch':
        fetch(root, current, raw, digest, review, traw, tdigest, trial)
    elif phase == 'acquire':
        acquire(root, current, raw, digest, review, traw, tdigest, trial, token)
    elif phase == 'finalize':
        finalize(root, current, digest, review, tdigest, trial)
    else:
        proof = Path(os.environ['OSV_PROOF_ROOT'])
        if phase == 'receive':
            receive(root, proof, current, raw, digest, review, traw, tdigest, trial)
        else:
            runtime(root, proof, current, raw, digest, review, traw, tdigest, trial)

def failure():
    # Fixed public metadata and hash/count-only native evidence; never stderr text.
    phase = sys.argv[1] if len(sys.argv) > 1 else 'unknown'
    if phase not in {'fetch', 'acquire', 'finalize', 'receive', 'runtime'} or len(sys.argv) != 4:
        return
    root = Path(sys.argv[2])
    try:
        require(root.parent == Path(os.environ['RUNNER_TEMP']) and root.resolve() == root and
            root.is_dir() and root.stat().st_uid == os.getuid(), 'owned fixed failure root')
        error = sys.exc_info()[1]
        value = {'schema': 'workflow.GO5932.split-failure.v1', 'complete': False,
            'accepted': False, 'phase': phase, 'error_type': type(error).__name__,
            'GO_2026_5932': 'HOLD'}
        if hasattr(error, 'native'):
            value['native_child'] = error.native
        seal(root / (phase + '-failure.json'), value, Budget(2))
    except BaseException:
        pass

if __name__ == '__main__':
    try:
        main()
    except BaseException:
        failure()
        os._exit(2)
    os._exit(0)
