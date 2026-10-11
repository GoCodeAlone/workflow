"""Inert proposed completion gate; the producer owns fresh out-of-root files.

Inventory expectations come from trusted Go/npm manifest parsing before scanning.
This module validates real output; it does not query vulnerabilities or run a scan.
"""
from collections import Counter
from dataclasses import dataclass
import json
from datetime import datetime, timezone
import copy
import re
from urllib.parse import unquote
from pathlib import Path


class IncompleteScan(ValueError):
    pass


@dataclass(frozen=True)
class AnalysisEvidence:
    root: str
    go_version: str
    called: frozenset
    advisories: dict
    modules: frozenset
    unverified_called: tuple = ()


def advisory_equal(left, right):
    """Only the actual Go protocol's absent/empty details default is normalized."""
    def canonical(value):
        if not isinstance(value, dict):
            return None
        value = dict(value)
        if value.get('details') == '':
            del value['details']
        return value
    return canonical(left) == canonical(right)


def _timestamp_identity(value):
    """Compare RFC3339 instants without discarding any nanosecond precision."""
    if not isinstance(value, str):
        raise IncompleteScan('invalid Go protocol timestamp')
    match = re.fullmatch(r'([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?:\.([0-9]{1,9}))?(Z|[+-][0-9]{2}:[0-9]{2})', value)
    if not match:
        raise IncompleteScan('invalid Go protocol timestamp')
    if match[3] != 'Z' and (int(match[3][1:3]) > 23 or int(match[3][4:6]) > 59):
        raise IncompleteScan('invalid Go protocol timestamp')
    try:
        dt = datetime.fromisoformat(match[1] + ('+00:00' if match[3] == 'Z' else match[3]))
        delta = dt.astimezone(timezone.utc) - datetime(1970, 1, 1, tzinfo=timezone.utc)
    except ValueError as error:
        raise IncompleteScan('invalid Go protocol timestamp') from error
    return (delta.days * 86400 + delta.seconds, int((match[2] or '').ljust(9, '0')))


def _known_fields(value, allowed, label, required=()):
    if not isinstance(value, dict) or set(value) - set(allowed) or not set(required).issubset(value):
        raise IncompleteScan('unsupported Go protocol ' + label)
    return value


def go_symbol_packages(raw):
    """Use every trusted raw slice; an omitted emitted slice cannot downgrade it."""
    packages = set()
    for entry in raw.get('affected', []):
        package = entry.get('package', {})
        if package.get('ecosystem') != 'Go':
            continue
        for imported in (entry.get('ecosystem_specific') or {}).get('imports', []):
            if (isinstance(imported, dict) and isinstance(imported.get('path'), str)
                    and imported['path'] and isinstance(imported.get('symbols'), list)
                    and imported['symbols'] and all(isinstance(s, str) and s for s in imported['symbols'])):
                packages.add(package['name'])
    return packages


def go_protocol_projection(raw, module_names=None):
    """Expected serialization of the pinned x/vuln1.6.0 internal/osv.Entry.

    Only this independently executed Go protocol uses the typed projection.
    Full scanner-to-database comparison remains strict and retains raw metadata.
    Unsupported semantics of a symbol-aware Go package reject. Known typed
    omissions on advisory-only Go/foreign slices can never clear a Go finding;
    full raw advisory equality and the unconditional advisory-only block remain.
    """
    raw = _known_fields(raw, {
        'schema_version', 'id', 'modified', 'published', 'withdrawn', 'aliases',
        'summary', 'details', 'affected', 'references', 'credits',
        'database_specific', 'related', 'upstream', 'severity'}, 'entry', ('id', 'modified', 'affected'))
    out = {'id': raw['id'], 'details': raw.get('details', '')}
    for key in ('schema_version', 'aliases', 'summary', 'references'):
        if raw.get(key):
            out[key] = copy.deepcopy(raw[key])
    for value in raw.get('references', []):
        _known_fields(value, {'type', 'url'}, 'reference', ('type', 'url'))
    # Go time.Time emits the zero timestamp even with the omitempty tag.
    for key in ('modified', 'published'):
        out[key] = raw.get(key, '0001-01-01T00:00:00Z')
    if raw.get('withdrawn') is not None:
        out['withdrawn'] = raw['withdrawn']
    affected = []
    symbol_packages = go_symbol_packages(raw)
    for entry in raw['affected']:
        entry = _known_fields(entry, {
            'package', 'ranges', 'ecosystem_specific', 'database_specific',
            'severity', 'versions'}, 'affected entry')
        package = _known_fields(entry['package'], {'name', 'ecosystem', 'purl'}, 'package', ('name', 'ecosystem'))
        scanned = package['ecosystem'] == 'Go' and (module_names is None or package['name'] in module_names)
        symbol_aware = package['ecosystem'] == 'Go' and package['name'] in symbol_packages
        if entry.get('versions') and symbol_aware:
            raise IncompleteScan('Go protocol cannot represent symbol-aware affected versions')
        if package.get('purl') and package['ecosystem'] == 'Go':
            purl = package['purl']
            if not isinstance(purl, str) or not purl.startswith('pkg:golang/') or package['ecosystem'] != 'Go':
                raise IncompleteScan('Go package URL contradicts package identity')
            purl_name = unquote(purl[len('pkg:golang/'):].split('#', 1)[0].split('?', 1)[0].split('@', 1)[0])
            if purl_name != package['name']:
                raise IncompleteScan('Go package URL contradicts package identity')
        item = {'package': {'name': package['name'], 'ecosystem': package['ecosystem']}}
        ranges = []
        for value in entry.get('ranges', []):
            value = _known_fields(value, {'type', 'events', 'repo'}, 'version range', ('type', 'events'))
            if value['type'] not in ('SEMVER', 'ECOSYSTEM', 'GIT'):
                raise IncompleteScan('unknown Go protocol range type')
            if symbol_aware and (value['type'] != 'SEMVER' or 'repo' in value):
                raise IncompleteScan('Go protocol cannot represent symbol-aware range semantics')
            projected_events = []
            for event in value['events']:
                event = _known_fields(event, {'introduced', 'fixed', 'last_affected', 'limit'}, 'range event')
                if len(event) != 1:
                    raise IncompleteScan('invalid Go protocol range event')
                if symbol_aware and set(event) - {'introduced', 'fixed'}:
                    raise IncompleteScan('Go protocol cannot represent symbol-aware range endpoint')
                projected_events.append({k: v for k, v in event.items() if k in ('introduced', 'fixed') and v})
            ranges.append({'type': value['type'], 'events': projected_events})
        if ranges:
            item['ranges'] = ranges
        ecosystem = _known_fields(entry.get('ecosystem_specific') or {}, {'imports', 'custom_ranges'}, 'symbol metadata')
        if 'custom_ranges' in ecosystem:
            custom = ecosystem['custom_ranges']
            if not isinstance(custom, list):
                raise IncompleteScan('invalid custom range metadata')
            for value in custom:
                value = _known_fields(value, {'type', 'events'}, 'custom range', ('type', 'events'))
                if value['type'] != 'ECOSYSTEM' or not isinstance(value['events'], list) or not value['events']:
                    raise IncompleteScan('unsupported custom range metadata')
                for event in value['events']:
                    event = _known_fields(event, {'introduced', 'fixed', 'last_affected', 'limit'}, 'custom range event')
                    if len(event) != 1 or any(not isinstance(v, str) or not v for v in event.values()):
                        raise IncompleteScan('invalid custom range event')
            if custom and scanned:
                raise IncompleteScan('Go protocol cannot represent scanned custom range semantics')
        projected_ecosystem = {}
        if ecosystem.get('imports'):
            imports = []
            for value in ecosystem['imports']:
                value = _known_fields(value, {'path', 'goos', 'goarch', 'symbols'}, 'import metadata')
                imports.append({k: copy.deepcopy(v) for k, v in value.items() if v})
            projected_ecosystem['imports'] = imports
        item['ecosystem_specific'] = projected_ecosystem
        affected.append(item)
    out['affected'] = affected
    if raw.get('credits'):
        credits = []
        for value in raw['credits']:
            value = _known_fields(value, {'name', 'type', 'contact'}, 'credit', ('name',))
            credits.append({'name': value['name']})
        out['credits'] = credits
    if raw.get('database_specific') is not None:
        value = _known_fields(raw['database_specific'], {
            'url', 'review_status', 'cwe_ids', 'github_reviewed',
            'github_reviewed_at', 'nvd_published_at', 'severity'}, 'database metadata')
        out['database_specific'] = {k: value[k] for k in ('url', 'review_status') if value.get(k)}
    return out


def go_protocol_advisory_equal(emitted, raw, module_names=None):
    expected = go_protocol_projection(raw, module_names)
    if not isinstance(emitted, dict):
        return False
    actual = copy.deepcopy(emitted)
    for record in (expected, actual):
        for key in ('modified', 'published', 'withdrawn'):
            if key in record:
                record[key] = _timestamp_identity(record[key])
    return actual == expected


def parse_analysis(stdout, exit_code, stderr, root, module, go_version, db_uri, advisories, module_versions, root_packages=None, identity_names=None):
    """Consume a fresh independently executed source/symbol JSON protocol."""
    if type(exit_code) is not int or exit_code != 0 or stderr.strip():
        raise IncompleteScan('independent govulncheck failed')
    messages = []
    decoder = json.JSONDecoder(object_pairs_hook=_object, parse_constant=lambda value: (_ for _ in ()).throw(
        IncompleteScan('invalid JSON constant: ' + value)))
    try:
        remaining = stdout
        while remaining.strip():
            message, end = decoder.raw_decode(remaining.lstrip())
            messages.append(message)
            remaining = remaining.lstrip()[end:]
    except (ValueError, TypeError) as error:
        raise IncompleteScan('malformed independent analysis protocol') from error
    configs = [m['config'] for m in messages if isinstance(m, dict) and 'config' in m]
    sboms = [m['SBOM'] for m in messages if isinstance(m, dict) and 'SBOM' in m]
    if len(configs) != 1 or len(sboms) != 1 or messages[0] != {'config':configs[0]}:
        raise IncompleteScan('incomplete independent analysis protocol')
    config, sbom = configs[0], sboms[0]
    if not isinstance(config, dict) or any(config.get(k) != v for k,v in {
            'protocol_version':'v1.0.0', 'scanner_name':'govulncheck',
            'scanner_version':'v1.6.0', 'go_version':'go'+go_version,
            'scan_level':'symbol', 'scan_mode':'source', 'db':db_uri}.items()):
        raise IncompleteScan('independent analysis identity, compiler, mode, or database mismatch')
    roots = [module] if root_packages is None else root_packages
    if not roots or len(roots) != len(set(roots)) or not isinstance(sbom, dict) or \
            not isinstance(sbom.get('roots'),list) or Counter(sbom['roots']) != Counter(roots) or sbom.get('go_version') != 'go'+go_version:
        raise IncompleteScan('independent analysis root mismatch')
    modules = sbom.get('modules')
    if not isinstance(modules, list) or any(not isinstance(m,dict) or not isinstance(m.get('path'),str)
            or not m['path'] or not isinstance(m.get('version',''),str) for m in modules):
        raise IncompleteScan('invalid independent module inventory')
    inventory = [(m['path'],m.get('version','')) for m in modules]
    wanted = list(module_versions.items()) if isinstance(module_versions,dict) else list(module_versions)
    if Counter(inventory) != Counter(wanted) or ('stdlib','v'+go_version) not in wanted or (module,'') not in wanted:
        raise IncompleteScan('independent analysis module inventory mismatch')
    # Context comes only from the trusted producer's independently parsed manifests
    # and source catalog, including original and effective replacement identities.
    # Validate the SBOM/source join before using any absence-based omission.
    module_names = None
    if identity_names is not None:
        if not isinstance(identity_names, (set, frozenset)) or not identity_names or any(
                not isinstance(name, str) or not name for name in identity_names):
            raise IncompleteScan('invalid trusted module identity inventory')
        module_names = identity_names | {path for path, version in wanted}
    called = set()
    pending_called = []
    emitted_ids = set()
    admitted = {'config','SBOM','progress','osv','finding'}
    for message in messages:
        if not isinstance(message, dict) or len(message) != 1 or not set(message).issubset(admitted):
            raise IncompleteScan('unknown independent analysis message')
        if 'finding' in message:
            finding = message['finding']
            trace = finding.get('trace') if isinstance(finding, dict) else None
            advisory_id = finding.get('osv') if isinstance(finding, dict) else None
            if (not isinstance(trace, list) or not trace or not isinstance(trace[0], dict)
                    or not isinstance(advisory_id, str) or advisory_id not in advisories):
                raise IncompleteScan('invalid independent finding')
            if 'function' in trace[0] and not isinstance(trace[0]['function'], str):
                raise IncompleteScan('independent called finding has invalid module/function')
            if trace[0].get('function'):
                if (not isinstance(trace[0].get('module'), str) or not trace[0]['module']
                        or not isinstance(trace[0]['function'], str)):
                    raise IncompleteScan('independent called finding has invalid module/function')
                pending_called.append((trace[0]['module'],finding['osv']))
        if 'osv' in message:
            advisory = message['osv']
            if not isinstance(advisory, dict) or advisory.get('id') not in advisories or not go_protocol_advisory_equal(
                    advisory, advisories[advisory['id']], module_names):
                raise IncompleteScan('independent advisory differs from trusted database')
            emitted_ids.add(advisory['id'])
    diagnostics = Counter()
    known_modules = {path for path, version in wanted} | (module_names or set())
    for name, advisory_id in pending_called:
        raw = advisories[advisory_id]
        if (advisory_id not in emitted_ids or name not in known_modules or not any(
                a['package']['ecosystem'] == 'Go' and a['package']['name'] == name
                for a in raw['affected'])):
            raise IncompleteScan('independent called finding identity or metadata is unbound')
        # The pinned Go evaluator treats an ECOSYSTEM-only slice as every version
        # and missing package symbols as all functions. These are retained as
        # observations, never used as an advisory-only version-presence oracle.
        # Complete raw aggregation and prior strict typed joins prevent downgrades.
        if name in go_symbol_packages(raw):
            called.add((name, advisory_id))
        else:
            diagnostics[(name, advisory_id)] += 1
    return AnalysisEvidence(str(Path(root).resolve()), go_version, frozenset(called), advisories,
                            frozenset(inventory), tuple((name, aid, count) for (name, aid), count in sorted(diagnostics.items())))


def _object(pairs):
    out = {}
    for key, value in pairs:
        if key in out:
            raise IncompleteScan('duplicate JSON key: ' + key)
        out[key] = value
    return out


def read_report(path):
    try:
        return json.loads(Path(path).read_text(), object_pairs_hook=_object,
                          parse_constant=lambda value: (_ for _ in ()).throw(
                              IncompleteScan('invalid JSON constant: ' + value)))
    except (OSError, json.JSONDecodeError) as error:
        raise IncompleteScan('missing or malformed scanner report') from error


def _package(info):
    if not isinstance(info, dict):
        raise IncompleteScan('package is not an object')
    if info.get('commit'):
        raise IncompleteScan('commit-only/Git inventory is outside the offline contract')
    values = (info.get('ecosystem'), info.get('name'), info.get('version',''))
    local = values[0] == 'Go' and isinstance(values[1],str) and values[1].startswith('.') and values[2] == ''
    if values[0] not in ('Go', 'npm') or (not local and not all(isinstance(x, str) and x for x in values)):
        raise IncompleteScan('unsupported or incomplete package identity')
    return values


def validate(report, expected, exit_code, stderr, analysis_complete, filtered_local=0, budget=None):
    """Return full candidate blocking findings after exact completion validation.

    analysis_complete is produced by the independent offline govulncheck process,
    one successful protocol/exit receipt for every expected Go source root.
    An old valid rc1 is a completed baseline with findings, never an empty scan.
    """
    if not expected or any(not packages for packages in expected.values()):
        raise IncompleteScan('empty input/inventory contract')
    if type(exit_code) is not int or exit_code not in (0, 1):
        raise IncompleteScan('scanner did not finish with rc0/rc1')
    # Pinned source emits these exact errors before returning partial JSON.
    # Only documented progress lines are allowed; new diagnostics fail closed.
    allowed = ('Starting filesystem walk for root: ', 'Scanned ', 'End status: ',
               'Loaded Go local db from ', 'Loaded npm local db from ')
    local_line = 'Filtered '+str(filtered_local)+' local/unscannable package/s from the scan.'
    local_lines = [line for line in stderr.splitlines() if line.startswith('Filtered ')]
    if local_lines != ([local_line] if filtered_local else []):
        raise IncompleteScan('unbound filtered local package count')
    if any(line and not line.startswith(allowed) and line != local_line for line in stderr.splitlines()):
        raise IncompleteScan('unadmitted scanner diagnostic')
    if not isinstance(report, dict) or not isinstance(report.get('results'), list):
        raise IncompleteScan('results is missing, null, or not an array')
    if report.get('experimental_generic_findings'):
        raise IncompleteScan('unreviewed generic finding')
    actual = {}
    blocking = set()
    for source in report['results']:
        if budget is not None:budget.left()
        if not isinstance(source, dict) or not isinstance(source.get('source'), dict):
            raise IncompleteScan('invalid result source')
        identity = source['source']
        path = identity.get('path')
        if identity.get('type') != 'lockfile' or path not in expected or path in actual:
            raise IncompleteScan('unexpected, duplicate, or mismatched source')
        packages = source.get('packages')
        if not isinstance(packages, list):
            raise IncompleteScan('invalid package array')
        independent = analysis_complete.get(path)
        go_versions = [key[2] for key in expected[path] if key[0]=='Go' and key[1]=='stdlib']
        if any(key[0]=='Go' for key in expected[path]) and (len(go_versions)!=1 or not isinstance(independent, AnalysisEvidence)
                or independent.root != str(Path(path).resolve().parent)
                or independent.go_version != go_versions[0]):
            raise IncompleteScan('independent Go root/protocol receipt mismatch')
        inventory = Counter()
        for package in packages:
            if budget is not None:budget.left()
            if not isinstance(package, dict):
                raise IncompleteScan('invalid package result')
            key = _package(package.get('package'))
            inventory[key] += 1
            vulnerabilities = package.get('vulnerabilities', [])
            groups = package.get('groups', [])
            if not isinstance(vulnerabilities, list) or not isinstance(groups, list):
                raise IncompleteScan('invalid vulnerability/group array')
            ids = []
            for vulnerability in vulnerabilities:
                if not isinstance(vulnerability, dict) or not isinstance(vulnerability.get('id'), str):
                    raise IncompleteScan('invalid advisory identity')
                ids.append(vulnerability['id'])
            if len(ids) != len(set(ids)):
                raise IncompleteScan('duplicate advisory')
            grouped = {}
            for group in groups:
                if not isinstance(group, dict) or not isinstance(group.get('ids'), list):
                    raise IncompleteScan('invalid advisory group')
                analysis = group.get('experimental_analysis', {})
                if not isinstance(analysis, dict):
                    raise IncompleteScan('invalid analysis map')
                for advisory_id in group['ids']:
                    if budget is not None:budget.left()
                    if advisory_id not in ids or advisory_id in grouped:
                        raise IncompleteScan('missing or duplicate grouped advisory')
                    evidence = analysis.get(advisory_id)
                    if evidence is not None:
                        if not isinstance(evidence, dict) or type(evidence.get('called')) is not bool:
                            raise IncompleteScan('called must be an explicit boolean')
                        if evidence.get('unimportant', False) is not False:
                            raise IncompleteScan('unimportant suppression is outside the contract')
                    grouped[advisory_id] = evidence
            if set(grouped) != set(ids):
                raise IncompleteScan('not every advisory is preserved in its group')
            for vulnerability in vulnerabilities:
                advisory_id = vulnerability['id']
                if key[0] == 'Go' and (not isinstance(independent, AnalysisEvidence)
                        or not advisory_equal(independent.advisories.get(advisory_id), vulnerability)):
                    raise IncompleteScan('Go applicability metadata differs from trusted database')
                symbol_aware = key[0] == 'Go' and any(
                    isinstance(affected, dict)
                    and affected.get('package', {}).get('name') == key[1]
                    and affected.get('package', {}).get('ecosystem') == 'Go'
                    and isinstance(affected.get('ecosystem_specific'), dict)
                    and any(isinstance(entry, dict) and isinstance(entry.get('path'), str)
                            and entry.get('path') and isinstance(entry.get('symbols'), list)
                            and entry['symbols'] and all(isinstance(symbol,str) and symbol for symbol in entry['symbols'])
                            for entry in affected['ecosystem_specific'].get('imports', []))
                    for affected in vulnerability.get('affected', []))
                evidence = grouped[advisory_id]
                if symbol_aware and evidence is None:
                    raise IncompleteScan('symbol-aware Go applicability is unknown')
                if symbol_aware:
                    if not evidence['called'] and (key[1],advisory_id) in independent.called:
                        raise IncompleteScan('scanner false contradicts independent called finding')
                # No allowlist: called:true and advisory-only/unknown findings block.
                # called:false is retained in JSON and accepted only after real analysis.
                if not symbol_aware or evidence is None or evidence['called']:
                    blocking.add(key + (advisory_id,))
        actual[path] = inventory
        if any(key[0] == 'Go' for key in inventory) and not isinstance(independent, AnalysisEvidence):
            raise IncompleteScan('independent Go analysis did not complete')
        if isinstance(independent, AnalysisEvidence) and any(
                not any(key[1]==module and key[3]==advisory_id for key in blocking)
                for module,advisory_id in independent.called):
            raise IncompleteScan('independently called advisory is dropped from scanner output')
    if set(actual) != set(expected):
        raise IncompleteScan('missing expected input source')
    for path, packages in expected.items():
        if actual[path] != Counter(tuple(key) for key in packages):
            raise IncompleteScan('package inventory differs from the trusted manifest: ' + path)
    if exit_code != int(bool(blocking)):
        raise IncompleteScan('scanner exit contradicts raw finding disposition')
    return blocking


def candidate_allowed(old, new):
    """Both scans must be complete; any full candidate finding still blocks.

    Differential SARIF remains presentation. It cannot hide a false->true change,
    a moved source, an existing finding, or a changed alias.
    """
    return not new

