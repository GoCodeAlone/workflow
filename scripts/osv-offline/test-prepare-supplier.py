"""Offline source controls: synthetic modules/fake Go, no downloads or builds.

Hosted/platform admission is explicitly substituted in orchestration controls.
Temporary files and small Python children do not establish Linux acceptance.
"""
import argparse
import base64
import copy
from email.message import Message
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import sys
import tempfile
import unittest
from unittest import mock
import zipfile

SOURCE = Path(__file__).with_name('prepare-supplier.py')
spec = importlib.util.spec_from_file_location('supplier_under_test', SOURCE)
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)
delivery_spec = importlib.util.spec_from_file_location('supplier_delivery_under_test', SOURCE.with_name('osv_supplier.py'))
delivery = importlib.util.module_from_spec(delivery_spec)
delivery_spec.loader.exec_module(delivery)


def reference_h1(files):
    lines = [hashlib.sha256(data).hexdigest() + '  ' + name + '\n'
             for name, data in sorted(files.items())]
    return 'h1:' + base64.b64encode(hashlib.sha256(''.join(lines).encode()).digest()).decode()


def metadata(name, vcs=False):
    module = s.VULN[0] if name == 'govulncheck' else s.SCANNER[0]
    version = s.VULN[1] + '\t' + s.EXTRA_SUMS[s.VULN][0] if name == 'govulncheck' else '(devel)\t'
    result = f'/owned/binaries/{name}: go1.27.2\n\tpath\t{module}/cmd/{name}\n\tmod\t{module}\t{version}\n'
    if name != 'govulncheck':
        result += '\tdep\tgolang.org/x/net\tv0.60.0\t' + s.EXTRA_SUMS[s.XNET][0] + '\n'
    for key, value in (('-trimpath', 'true'), ('CGO_ENABLED', '0'), ('GOOS', 'linux'),
                        ('GOARCH', 'amd64'), ('GOAMD64', 'v1')):
        result += f'\tbuild\t{key}={value}\n'
    if vcs:
        result += '\tbuild\tvcs.revision=da445b7c2743db52c1370fab3357949202f66557\n'
    return result.encode()


class SupplierControls(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='osv-supplier-offline-controls-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()

    def module(self, module='example.com/supplier', version='v1.0.0', extra=None):
        prefix = f'{module}@{version}/'
        files = {'go.mod': f'module {module}\n\ngo 1.27.0\n'.encode(), 'package.go': b'package supplier\n'}
        files.update(extra or {})
        path = self.base / ('module-' + str(len(list(self.base.iterdir()))) + '.zip')
        with zipfile.ZipFile(path, 'w') as z:
            for name, raw in files.items():
                z.writestr(prefix + name, raw)
        return path, reference_h1({prefix + name: raw for name, raw in files.items()}), files

    def test_go_hash1_primary_source_vector(self):
        # Go x/mod v0.41.0 hash_test.go TestHash1 uses these exact two names/data.
        files = {'xyz': b'data for xyz', 'abc': b'data for abc'}
        self.assertEqual(s.hash1(files), reference_h1(files))
        self.assertNotEqual(s.hash1(files), s.hash1({'abc': files['abc'] + b'!', 'xyz': files['xyz']}))

    def test_owned_sdk_root_is_tightened_without_replacing_identity(self):
        root = self.base / 'sdk'; root.mkdir(mode=0o700)
        root.chmod(0o777); before = root.stat()
        receipt = {}
        self.assertEqual(s.admit_toolchain_root(root, root / 'bin/go', s.Budget(5), receipt), root)
        after = root.stat()
        self.assertEqual((after.st_dev, after.st_ino), (before.st_dev, before.st_ino))
        self.assertEqual(stat.S_IMODE(after.st_mode), 0o755)
        self.assertEqual(receipt['toolchain_root_permissions']['before'], '0o777')
        self.assertEqual(receipt['toolchain_root_permissions']['after'], '0o755')

    def test_unowned_writable_sdk_root_is_refused_without_permission_change(self):
        root = self.base / 'sdk'; root.mkdir(mode=0o700); root.chmod(0o777)
        receipt = {}
        with mock.patch.object(s.os, 'getuid', return_value=root.stat().st_uid + 1):
            with self.assertRaisesRegex(RuntimeError, 'unowned writable'):
                s.admit_toolchain_root(root, root / 'bin/go', s.Budget(5), receipt)
        self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o777)
        self.assertEqual(receipt, {})

    def test_zip_identity_and_exact_extraction(self):
        path, h1, files = self.module()
        destination = self.base / 'source'
        receipt = s.archive(path, 'example.com/supplier', 'v1.0.0', h1, s.Budget(5), destination)
        self.assertEqual(receipt['files'], s.inventory(destination, s.Budget(5)))
        self.assertEqual((destination / 'go.mod').read_bytes(), files['go.mod'])

    def legacy_download(self, identity=('example.com/legacy', 'v1.0.0'), mod=None):
        module, version = identity
        cache = self.base / ('synthetic-cache-' + str(len(list(self.base.iterdir()))))
        prefix = cache / 'cache/download' / s.escape(module) / '@v' / s.escape(version)
        prefix.parent.mkdir(mode=0o700, parents=True)
        zip_path = Path(str(prefix) + '.zip')
        files = {f'{module}@{version}/legacy.go': b'package legacy\n'}
        with zipfile.ZipFile(zip_path, 'w') as z:
            for name, raw in files.items():
                z.writestr(name, raw)
        mod = f'module {module}\n'.encode() if mod is None else mod
        Path(str(prefix) + '.mod').write_bytes(mod)
        Path(str(prefix) + '.info').write_text(json.dumps({'Version': version}))
        sums = (reference_h1(files), reference_h1({'go.mod': mod}))
        row = {'Path': module, 'Version': version, 'Sum': sums[0], 'GoModSum': sums[1],
               'Zip': str(zip_path), 'GoMod': str(prefix) + '.mod', 'Info': str(prefix) + '.info',
               'Dir': str(cache / (s.escape(module) + '@' + s.escape(version)))}
        return row, identity, sums, cache

    def test_legacy_zip_requires_independently_authenticated_exact_synthetic_mod(self):
        row, identity, sums, cache = self.legacy_download()
        result = s.verify_download(row, identity, sums, cache, s.Budget(5))
        self.assertEqual(result['go_mod_origin'], 'authenticated-proxy-synthesized')
        self.assertEqual(result['go_mod'], b'module example.com/legacy\n')
        self.assertEqual(result['go_mod_h1'], sums[1])
        self.assertNotIn('go.mod', result['files'])
        self.assertFalse(Path(row['Dir']).exists())
        Path(row['GoMod']).write_bytes(b'module example.com/legacy\nchanged\n')
        with self.assertRaisesRegex(RuntimeError, 'go.mod h1 mismatch'):
            s.verify_download(row, identity, sums, cache, s.Budget(5))

    def test_legacy_extra_directive_wrong_module_and_scanner_missing_mod_refuse(self):
        for identity, mod in ((('example.com/legacy', 'v1.0.0'), b'module wrong.example/path\n'),
                (('example.com/legacy', 'v1.0.0'), b'module example.com/legacy\nrequire evil.example/code v1.0.0\n'),
                (s.SCANNER, None)):
            with self.subTest(identity=identity, mod=mod):
                row, identity, sums, cache = self.legacy_download(identity, mod)
                with self.assertRaisesRegex(RuntimeError, 'source go.mod missing'):
                    s.verify_download(row, identity, sums, cache, s.Budget(5))

    def test_missing_archive_mod_has_no_unauthenticated_or_extraction_fallback(self):
        row, identity, sums, cache = self.legacy_download()
        for kwargs in ({}, {'destination': self.base / 'extracted',
                'authenticated_mod': b'module example.com/legacy\n'}):
            with self.subTest(kwargs=kwargs), self.assertRaisesRegex(RuntimeError, 'source go.mod missing'):
                s.archive(Path(row['Zip']), *identity, sums[0], s.Budget(5), **kwargs)
        self.assertFalse((self.base / 'extracted').exists())

    def test_changed_zip_content_refuses_before_extraction(self):
        path, h1, _ = self.module()
        with zipfile.ZipFile(path, 'w') as z:
            z.writestr('example.com/supplier@v1.0.0/go.mod', b'changed source')
        destination = self.base / 'source'
        with self.assertRaisesRegex(RuntimeError, 'h1 mismatch'):
            s.archive(path, 'example.com/supplier', 'v1.0.0', h1, s.Budget(5), destination)
        self.assertFalse(destination.exists())

    def test_zip_traversal_symlink_duplicate_and_wrong_prefix_refused(self):
        for kind in ('traversal', 'symlink', 'duplicate', 'prefix'):
            with self.subTest(kind=kind):
                path = self.base / (kind + '.zip')
                with zipfile.ZipFile(path, 'w') as z:
                    if kind == 'symlink':
                        row = zipfile.ZipInfo('example.com/supplier@v1.0.0/link')
                        row.external_attr = (stat.S_IFLNK | 0o777) << 16
                        z.writestr(row, '/private/secret')
                    elif kind == 'duplicate':
                        z.writestr('example.com/supplier@v1.0.0/a', b'a')
                        z.writestr('example.com/supplier@v1.0.0/A', b'b')
                    else:
                        z.writestr('other@v1.0.0/x' if kind == 'prefix' else
                                   'example.com/supplier@v1.0.0/../escape', b'a')
                with self.assertRaises(RuntimeError):
                    s.archive(path, 'example.com/supplier', 'v1.0.0', 'irrelevant', s.Budget(5))
        self.assertFalse((self.base.parent / 'escape').exists())

    def test_zip_file_expansion_and_count_limits_refused(self):
        path, h1, _ = self.module(extra={'large': b'large-data'})
        for constant, value in (('FILE_BYTES', 2), ('SOURCE_BYTES', 2), ('SOURCE_FILES', 1)):
            with self.subTest(constant=constant), mock.patch.object(s, constant, value):
                with self.assertRaises(RuntimeError):
                    s.archive(path, 'example.com/supplier', 'v1.0.0', h1, s.Budget(5))

    def test_closed_environment_no_credentials_hooks_or_proxy_fallback(self):
        ambient = {'AWS_SECRET_ACCESS_KEY': 'test-secret', 'HTTP_PROXY': 'http://private-proxy',
                   'GOFLAGS': '-toolexec=/unverified/helper', 'GOPRIVATE': '*', 'GOTOOLCHAIN': 'auto'}
        with mock.patch.dict(os.environ, ambient):
            env = s.environment(Path('/installed/bin/go'), Path('/home/runner'),
                 {'GOPATH': Path('/home/runner/go'), 'GOMODCACHE': Path('/home/runner/go/pkg/mod'),
                  'GOCACHE': Path('/home/runner/.cache/go-build')}, self.base, self.base / 'config', online=True)
        self.assertNotIn('AWS_SECRET_ACCESS_KEY', env)
        self.assertNotIn('HTTP_PROXY', env)
        self.assertEqual(env['GOAUTH'], 'off')
        self.assertEqual(env['GOPROXY'], 'https://proxy.golang.org')
        self.assertEqual(env['GOSUMDB'], 'sum.golang.org')
        self.assertEqual(env['GOVCS'], '*:off')
        self.assertEqual(env['GOTOOLCHAIN'], 'local')
        self.assertEqual(env['GOFLAGS'], '-mod=readonly')
        self.assertEqual(env['XDG_CONFIG_HOME'], str(self.base / 'config'))
        self.assertNotIn('GOTELEMETRY', env)
        offline = s.environment(Path('/installed/bin/go'), Path('/home/runner'),
                  {k: Path(env[k]) for k in ('GOPATH', 'GOMODCACHE', 'GOCACHE')}, self.base, self.base / 'config')
        self.assertEqual((offline['GOPROXY'], offline['GOSUMDB']), ('off', 'off'))

    def test_json_duplicate_and_unexpected_cache_identity_refused(self):
        with self.assertRaisesRegex(RuntimeError, 'duplicate'):
            s.json_records(b'{"Path":"a","Path":"b"}')
        with self.assertRaisesRegex(RuntimeError, 'identity'):
            s.verify_download({'Path': 'wrong', 'Version': 'v1.0.0'}, ('example.com/supplier', 'v1.0.0'),
                              ('zip', 'mod'), self.base, s.Budget(5))
        row = {'Path': 'example.com/supplier', 'Version': 'v1.0.0', 'Sum': 'zip', 'GoModSum': 'mod',
               'Zip': '/private/credential-file'}
        with self.assertRaisesRegex(RuntimeError, 'cache path'):
            s.verify_download(row, ('example.com/supplier', 'v1.0.0'), ('zip', 'mod'), self.base, s.Budget(5))

    def test_source_replacements_and_unpinned_graph_are_refused(self):
        for directive in ('replace example.com/a => /candidate', 'toolchain go1.99.0', 'tool malicious/helper'):
            with self.subTest(directive=directive), self.assertRaises(RuntimeError):
                s.requirements(('module example.com/trusted\ngo 1.27.0\n' + directive + '\n').encode())
        with self.assertRaisesRegex(RuntimeError, 'lacks authenticated'):
            s.expected_sums(('example.com/a', 'v1.0.0'), {})

    def test_metadata_catalog_only_exact_reviewed_manifest_rows(self):
        rows = {('example.com/metadata', 'v1.0.0/go.mod'): reference_h1({'go.mod': b'module example.com/metadata\n'})}
        expected = [['example.com/metadata', 'v1.0.0', rows['example.com/metadata', 'v1.0.0/go.mod']]]
        digest = s.sha(json.dumps(expected, separators=(',', ':'), ensure_ascii=True).encode())
        with mock.patch.object(s, 'MOD_CATALOG_COUNT', 1), mock.patch.object(s, 'MOD_CATALOG_SHA', digest):
            self.assertEqual(s.metadata_catalog(rows), expected)
            for change in ({}, {('evil.example/code', 'v1.0.0/go.mod'): next(iter(rows.values()))},
                    {('example.com/metadata', 'v1.0.0/go.mod'): reference_h1({'go.mod': b'changed'})}):
                with self.subTest(change=change), self.assertRaisesRegex(RuntimeError, 'catalog changed'):
                    s.metadata_catalog(change)

    def test_metadata_only_preload_authenticates_before_publication_and_conserves(self):
        cache = self.base / 'standard-synthetic-cache'; cache.mkdir()
        raw = b'module example.com/old\n'; digest = reference_h1({'go.mod': raw})
        catalog = [['example.com/old', 'v1.0.0', digest]]; records = []
        with mock.patch.object(s, 'fetch_mod', return_value=raw) as fetch:
            self.assertEqual(s.preload_metadata(catalog, cache, s.Budget(5), records), len(raw))
            fetch.assert_called_once()
        path = cache / 'cache/download/example.com/old/@v/v1.0.0.mod'
        self.assertEqual(path.read_bytes(), raw); self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o444)
        self.assertEqual(list(path.parent.iterdir()), [path])
        with mock.patch.object(s, 'fetch_mod', side_effect=AssertionError('network must not be used')):
            second = []; s.preload_metadata(catalog, cache, s.Budget(5), second)
        self.assertEqual(second[0]['origin'], 'authenticated-existing-cache')
        s.conserve_metadata(records, cache, s.Budget(5))
        path.chmod(0o600); path.write_bytes(b'changed')
        with self.assertRaisesRegex(RuntimeError, 'changed at closeout'):
            s.conserve_metadata(records, cache, s.Budget(5))

    def test_wrong_metadata_h1_cached_or_downloaded_has_no_overwrite_fallback(self):
        raw = b'module example.com/old\n'; catalog = [['example.com/old', 'v1.0.0', reference_h1({'go.mod': raw})]]
        for cached in (False, True):
            with self.subTest(cached=cached):
                cache = self.base / str(cached); cache.mkdir()
                path = s.metadata_path(cache, *catalog[0][:2], create=True)
                if cached:path.write_bytes(b'wrong')
                with mock.patch.object(s, 'fetch_mod', return_value=b'wrong') as fetch:
                    with self.assertRaisesRegex(RuntimeError, 'h1 mismatch'):
                        s.preload_metadata(catalog, cache, s.Budget(5), [])
                    if cached:fetch.assert_not_called()
                self.assertEqual(path.read_bytes(), b'wrong') if cached else self.assertFalse(path.exists())

    def test_metadata_symlink_parent_and_file_are_refused(self):
        raw = b'module example.com/old\n'; catalog = [['example.com/old', 'v1.0.0', reference_h1({'go.mod': raw})]]
        for kind in ('parent', 'file'):
            with self.subTest(kind=kind):
                cache = self.base / kind; cache.mkdir()
                outside = self.base / ('outside-' + kind); outside.mkdir()
                if kind == 'parent':(cache / 'cache').symlink_to(outside, target_is_directory=True)
                else:
                    target = outside / 'mod'; target.write_bytes(raw)
                    s.metadata_path(cache, *catalog[0][:2], create=True).symlink_to(target)
                with mock.patch.object(s, 'fetch_mod', side_effect=AssertionError('no network')):
                    with self.assertRaises(RuntimeError):s.preload_metadata(catalog, cache, s.Budget(5), [])
                self.assertEqual(list(outside.iterdir()), [] if kind == 'parent' else [outside / 'mod'])

    def response(self, status=200, body=b'module example.com/Old\n', headers=None):
        response = mock.Mock(); response.status = status; response.headers = Message()
        for key, value in (headers or []):response.headers.add_header(key, value)
        parts = [body, b'']; response.read.side_effect = lambda size: parts.pop(0)
        connection = mock.Mock(); connection.getresponse.return_value = response
        return connection

    def test_metadata_tls_request_has_one_origin_no_credentials_redirect_or_proxy(self):
        connection = self.response()
        context = mock.Mock()
        with mock.patch.dict(os.environ, {'HTTPS_PROXY':'http://private.invalid','SSL_CERT_FILE':'/private/key','SSLKEYLOGFILE':'/private/keylog'}), \
                mock.patch.object(s.ssl, 'SSLContext', return_value=context), \
                mock.patch.object(s.http.client, 'HTTPSConnection', return_value=connection) as create:
            self.assertEqual(s.fetch_mod('example.com/Old', 'v1.0.0+incompatible', s.Budget(5)), b'module example.com/Old\n')
        context.load_verify_locations.assert_called_once_with(cafile='/etc/ssl/certs/ca-certificates.crt')
        self.assertEqual(create.call_args.args, ('proxy.golang.org', 443))
        self.assertEqual(connection.request.call_args.args, ('GET', '/example.com/!old/@v/v1.0.0%2Bincompatible.mod'))
        self.assertEqual(set(connection.request.call_args.kwargs['headers']), {'Accept','User-Agent','Connection'})
        connection.close.assert_called_once();self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_metadata_redirect_size_encoding_deadline_and_final_io_refuse(self):
        for connection in (self.response(status=302), self.response(headers=[('Content-Length','1048577')]),
                self.response(headers=[('Content-Length','1'),('Content-Length','1')]),
                self.response(headers=[('Content-Encoding','gzip')]), self.response(body=b''),
                self.response(headers=[('Content-Length','1')])):
            with self.subTest(connection=connection), mock.patch.object(s.ssl, 'SSLContext'), \
                    mock.patch.object(s.http.client, 'HTTPSConnection', return_value=connection):
                with self.assertRaises(RuntimeError):s.fetch_mod('example.com/old', 'v1.0.0', s.Budget(5))
                connection.close.assert_called_once()
        connection = self.response(body=b'overlimit')
        with mock.patch.object(s, 'MOD_BYTES', 2), mock.patch.object(s.ssl, 'SSLContext'), \
                mock.patch.object(s.http.client, 'HTTPSConnection', return_value=connection):
            with self.assertRaisesRegex(RuntimeError, 'body exceeded'):
                s.fetch_mod('example.com/old', 'v1.0.0', s.Budget(5))
        connection = self.response(); previous = signal.getsignal(signal.SIGALRM)
        connection.getresponse.side_effect = lambda: signal.raise_signal(signal.SIGALRM)
        with mock.patch.object(s.ssl, 'SSLContext'), mock.patch.object(s.http.client, 'HTTPSConnection', return_value=connection):
            with self.assertRaisesRegex(RuntimeError, 'request deadline'):
                s.fetch_mod('example.com/old', 'v1.0.0', s.Budget(5))
        self.assertEqual(signal.getsignal(signal.SIGALRM), previous)
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))
        raw=b'module example.com/old\n';cache=self.base/'cache';cache.mkdir()
        catalog=[['example.com/old','v1.0.0',reference_h1({'go.mod':raw})]]
        with mock.patch.object(s, 'fetch_mod', return_value=raw), mock.patch.object(s, 'write_fresh', side_effect=OSError('controlled I/O')):
            with self.assertRaisesRegex(OSError, 'controlled I/O'):s.preload_metadata(catalog,cache,s.Budget(5),[])

    def test_independent_metadata_consumer_requires_catalog_completion_and_actual_bytes(self):
        raw=b'module example.com/old\n';cache=self.base/'cache';cache.mkdir()
        catalog=[['example.com/old','v1.0.0',reference_h1({'go.mod':raw})]]
        digest=s.sha(json.dumps(catalog,separators=(',',':'),ensure_ascii=True).encode());records=[]
        with mock.patch.object(s,'fetch_mod',return_value=raw):s.preload_metadata(catalog,cache,s.Budget(5),records)
        receipt={'metadata_only':{'complete':True,'conserved':True,'catalog_count':1,'catalog_sha256':digest,'records':records}}
        with mock.patch.object(delivery,'METADATA_COUNT',1),mock.patch.object(delivery,'METADATA_SHA256',digest):
            delivery.verify_metadata(receipt,cache)
            for key,value in (('complete',False),('conserved',False),('catalog_sha256','0'*64),('catalog_count',2),('records',records*2)):
                bad=copy.deepcopy(receipt);bad['metadata_only'][key]=value
                with self.subTest(key=key),self.assertRaises(RuntimeError):delivery.verify_metadata(bad,cache)
            bad=copy.deepcopy(receipt);bad['metadata_only']['records'][0]['module']='evil.example/unpinned'
            with self.assertRaisesRegex(RuntimeError,'identities differ'):delivery.verify_metadata(bad,cache)
            path=s.metadata_path(cache,'example.com/old','v1.0.0');path.chmod(0o600);path.write_bytes(raw.replace(b'old',b'bad'))
            with self.assertRaisesRegex(RuntimeError,'actual metadata changed'):delivery.verify_metadata(receipt,cache)

    def test_metadata_connection_close_failure_restores_alarm_and_remains_failed(self):
        connection=self.response();connection.close.side_effect=OSError('controlled close failure')
        previous=signal.getsignal(signal.SIGALRM)
        with mock.patch.object(s.ssl,'SSLContext'),mock.patch.object(s.http.client,'HTTPSConnection',return_value=connection):
            with self.assertRaisesRegex(OSError,'controlled close failure'):
                s.fetch_mod('example.com/old','v1.0.0',s.Budget(5))
        connection.close.assert_called_once()
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL),(0.0,0.0))
        self.assertEqual(signal.getsignal(signal.SIGALRM),previous)

    def test_buildinfo_vcs_wrong_compiler_and_dependency_refused(self):
        for name in ('osv-scanner', 'osv-reporter', 'govulncheck'):
            self.assertEqual(s.buildinfo(metadata(name), name)['compiler'], 'go1.27.2')
            for bad in (metadata(name, vcs=True), metadata(name).replace(b'go1.27.2', b'go1.26.5'),
                        metadata(name).replace(b'v0.60.0', b'v0.58.0') if name != 'govulncheck' else
                        metadata(name).replace(b'v1.6.0', b'v1.5.0')):
                with self.subTest(name=name), self.assertRaises(RuntimeError):
                    s.buildinfo(bad, name)

    def test_explicit_execute_required_before_any_supplier_operation(self):
        argv = ['prepare-supplier.py', '--output', '/not-used', '--go', '/not-used', '--gopath', '/not-used',
                '--gomodcache', '/not-used', '--gocache', '/not-used']
        with mock.patch.object(sys, 'argv', argv), mock.patch.object(s, 'supplier') as supplier:
            with self.assertRaisesRegex(RuntimeError, 'explicit --execute'):
                s.main()
        supplier.assert_not_called()

    def test_hosted_identity_scope_and_wrong_dispatch_refusal(self):
        env = {**s.IDENTITY, 'GITHUB_SHA': 'a' * 40, 'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1'}
        with mock.patch.dict(os.environ, env), mock.patch.object(s.platform, 'system', return_value='Linux'), \
             mock.patch.object(s.platform, 'machine', return_value='x86_64'), \
             mock.patch.object(s.Path, 'resolve', return_value=self.base / 'os-release'), \
             mock.patch.object(s, 'canonical', return_value=self.base / 'os-release'), \
             mock.patch.object(s, 'read_file', return_value=b'ID=ubuntu\nVERSION_ID="24.04"\n'):
            identity = s.hosted_identity(s.Budget(5))
            self.assertEqual(set(identity), {'GITHUB_REPOSITORY', 'GITHUB_SHA', 'GITHUB_RUN_ID',
                                             'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB'})
            for key, value in (('GITHUB_REF', 'refs/heads/main'), ('GITHUB_JOB', 'other-job'),
                               ('GITHUB_EVENT_NAME', 'pull_request'), ('GITHUB_RUN_ID', '01'),
                               ('GITHUB_SHA', 'unbound')):
                with self.subTest(key=key), mock.patch.dict(os.environ, {key: value}):
                    with self.assertRaises(RuntimeError):
                        s.hosted_identity(s.Budget(5))

    def test_real_python_child_output_overflow_is_bounded_and_incomplete(self):
        commands = []
        with mock.patch.object(s, 'OUTPUT_BYTES', 2048):
            with self.assertRaisesRegex(RuntimeError, 'output exceeded'):
                s.run([sys.executable, '-I', '-S', '-B', '-c', 'import sys;sys.stdout.write("x"*8192);sys.stdout.flush()'],
                      self.base, {'PATH': '/usr/bin:/bin'}, s.Budget(5), 3, self.base, 'overflow', commands)
        self.assertLessEqual(len((self.base / 'overflow.stdout').read_bytes()), 2048)
        self.assertFalse(commands[0]['complete'])

    def test_real_python_child_timeout_is_retired_and_incomplete(self):
        commands = []
        with self.assertRaisesRegex(RuntimeError, 'deadline'):
            s.run([sys.executable, '-I', '-S', '-B', '-c', 'import time;time.sleep(5)'], self.base,
                  {'PATH': '/usr/bin:/bin'}, s.Budget(5), 0.05, self.base, 'timeout', commands)
        self.assertIsNotNone(commands[0]['exit'])
        self.assertFalse(commands[0]['complete'])


class OrchestrationControls(unittest.TestCase):
    # Synthetic ZIPs and a fake command adapter exercise real receipt/source files.
    # The actual hosted/platform/compiler admission and commands are NOT executed.
    module = SupplierControls.module

    def setUp(self):
        SupplierControls.setUp(self)
        self.home = self.base / 'home'; self.home.mkdir()
        self.caches = {'GOPATH': self.home / 'go', 'GOMODCACHE': self.home / 'go/pkg/mod',
                       'GOCACHE': self.home / '.cache/go-build'}
        for path in self.caches.values():
            path.mkdir(parents=True, exist_ok=True)
        self.root = self.base / 'go-root'
        for rel in ('bin/go', 'pkg/tool/linux_amd64/compile', 'pkg/tool/linux_amd64/link', 'pkg/tool/linux_amd64/asm'):
            path = self.root / rel; path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'fake installed compiler; never executed'); path.chmod(0o700)
        self.output = self.base / 'osv-supplier-0123456789'; self.output.mkdir(mode=0o700)
        self.args = argparse.Namespace(output=str(self.output), go=str(self.root / 'bin/go'),
                                      **{k.lower(): str(v) for k, v in self.caches.items()})
        self.rows = {}; self.sums = {}
        others = [('example.com/supplier/m' + str(i), 'v1.0.0') for i in range(204)]
        dependencies = others + [s.VULN, s.XNET]
        for identity in dependencies:
            self.cache_module(identity)
        oldnet_identity = (s.XNET[0], 'v0.58.0')
        self.cache_module(oldnet_identity)
        upstream_mod = ('module ' + s.SCANNER[0] + '\n\ngo 1.27.0\n\nrequire (\n' +
                        ''.join('\t' + a + ' ' + b + ' // indirect\n' for a, b in others +
                                [s.VULN, (s.XNET[0], 'v0.58.0')]) + ')\n').encode()
        oldnet = ('golang.org/x/net v0.58.0 ' + self.sums[oldnet_identity] + '\n' +
                  'golang.org/x/net v0.58.0/go.mod ' + self.sums[oldnet_identity[0], oldnet_identity[1] + '/go.mod'] + '\n').encode()
        upstream_sum = ''.join(a + ' ' + b + ' ' + value + '\n'
                               for (a, b), value in self.sums.items() if a != s.XNET[0]).encode() + oldnet
        additions = (s.XNET[0] + ' ' + s.XNET[1] + ' ' + self.sums[s.XNET] + '\n' +
                     s.XNET[0] + ' ' + s.XNET[1] + '/go.mod ' + self.sums[s.XNET[0], s.XNET[1] + '/go.mod'] + '\n').encode()
        patched_mod = upstream_mod.replace(b'go 1.27.0\n', b'go 1.27.2\n', 1).replace(
                      b'golang.org/x/net v0.58.0 // indirect', b'golang.org/x/net v0.60.0 // indirect')
        catalog = sorted([a, b[:-7], value] for (a,b),value in
                         s.checksum_rows(upstream_sum + additions).items() if b.endswith('/go.mod'))
        self.cache_module(s.SCANNER, {'go.mod': upstream_mod, 'go.sum': upstream_sum,
                                    'cmd/osv-scanner/main.go': b'package main\n',
                                    'cmd/osv-reporter/main.go': b'package main\n'})
        self.patches = [mock.patch.dict(os.environ, {'RUNNER_TEMP': str(self.base)}),
          mock.patch.object(s, 'hosted_identity', return_value={'GITHUB_REPOSITORY': 'GoCodeAlone/workflow',
            'GITHUB_SHA': 'a' * 40, 'GITHUB_JOB': 'osv-production-proof', 'GITHUB_RUN_ID': '1', 'GITHUB_RUN_ATTEMPT': '1'}),
          mock.patch.object(s, 'runner_home', return_value=self.home),
          mock.patch.object(s, 'SCANNER_SUM', self.sums[s.SCANNER]),
          mock.patch.object(s, 'SCANNER_MOD_SUM', self.sums[s.SCANNER[0], s.SCANNER[1] + '/go.mod']),
          mock.patch.object(s, 'UPSTREAM_XNET_MOD_SUM', self.sums[oldnet_identity[0], oldnet_identity[1] + '/go.mod']),
          mock.patch.object(s, 'MOD_CATALOG_COUNT', len(catalog)),
          mock.patch.object(s, 'MOD_CATALOG_SHA', s.sha(json.dumps(catalog, separators=(',', ':'), ensure_ascii=True).encode())),
          mock.patch.object(s, 'EXTRA_SUMS', {i: (self.sums[i], self.sums[i[0], i[1] + '/go.mod']) for i in (s.VULN, s.XNET)}),
          mock.patch.object(s, 'UPSTREAM_SHA', {'go.mod': s.sha(upstream_mod), 'go.sum': s.sha(upstream_sum)}),
          mock.patch.object(s, 'PATCHED_SHA', {'go.mod': s.sha(patched_mod), 'go.sum': s.sha(upstream_sum + additions)}),
          mock.patch.object(s, 'run', side_effect=self.fake_run)]
        for patch in self.patches:
            patch.start(); self.addCleanup(patch.stop)
        self.calls = []; self.fail_phase = None; self.corrupt_source = False; self.corrupt_tool = False

    def cache_module(self, identity, extra=None):
        module, version = identity
        path, h1, files = self.module(module, version, extra)
        root = self.caches['GOMODCACHE']
        prefix = root / 'cache/download' / s.escape(module) / '@v' / s.escape(version)
        prefix.parent.mkdir(parents=True, exist_ok=True)
        values = {'Zip': path.read_bytes(), 'GoMod': files['go.mod'],
                  'Info': json.dumps({'Version': version}).encode()}
        row = {'Path': module, 'Version': version, 'Sum': h1,
               'GoModSum': reference_h1({'go.mod': files['go.mod']}),
               'Dir': str(root / (s.escape(module) + '@' + s.escape(version)))}
        for field, suffix in (('Zip', '.zip'), ('GoMod', '.mod'), ('Info', '.info')):
            target = Path(str(prefix) + suffix); target.write_bytes(values[field]); row[field] = str(target)
        if identity == s.SCANNER:
            row['Origin'] = {'VCS': 'git', 'URL': 'https://github.com/google/osv-scanner', 'Hash': s.ORIGIN}
        self.rows[identity] = row
        self.sums[identity] = h1
        self.sums[module, version + '/go.mod'] = row['GoModSum']

    def fake_run(self, argv, cwd, env, budget, seconds, logs, label, commands):
        self.calls.append((argv, cwd, env, label))
        commands.append({'phase': label, 'exit': 9 if self.fail_phase == label else 0,
                         'complete': self.fail_phase != label, 'elapsed_seconds': 0.001})
        if self.fail_phase == label:
            raise RuntimeError('supplier native phase failed: ' + label + ' (exit 9)')
        if label == 'go-version':
            return b'go version go1.27.2 linux/amd64\n'
        if label == 'go-environment':
            self.assertEqual((Path(env['XDG_CONFIG_HOME']) / 'go/telemetry/mode').read_bytes(), b'off\n')
            return json.dumps({'GOVERSION': 'go1.27.2', 'GOROOT': str(self.root), 'GOTELEMETRY': 'off',
                               **{k: str(v) for k, v in self.caches.items()}}).encode()
        if label.startswith('download-'):
            self.assertEqual(env['GOPROXY'], 'https://proxy.golang.org')
            self.assertEqual(env['GOSUMDB'], 'sum.golang.org')
            return b'\n'.join(json.dumps(self.rows[tuple(i.rsplit('@', 1))]).encode() for i in argv[4:])
        self.assertEqual((env['GOPROXY'], env['GOSUMDB']), ('off', 'off'))
        if label.startswith('build-'):
            self.assertIn('-buildvcs=false', argv)
            self.assertIn('-trimpath', argv)
            name = label[len('build-'):]
            target = self.output / 'binaries' / name
            target.write_bytes(b'\x7fELF\x02\x01' + b'\0' * 12 + b'\x3e\x00' + name.encode())
            target.chmod(0o755)
        if label == 'cache-verify-after' and self.corrupt_source:
            (self.output / 'work/scanner-source/cmd/osv-scanner/main.go').write_bytes(b'tampered')
        if label == 'cache-verify-after' and self.corrupt_tool:
            (self.root / 'bin/go').write_bytes(b'changed compiler')
        return metadata(label[len('buildinfo-'):]) if label.startswith('buildinfo-') else b'all modules verified\n'

    def receipt(self):
        return json.loads((self.output / 'supplier-delivery-proof.json').read_bytes())

    def test_fake_supplier_complete_is_provisional_and_exactly_three_builds(self):
        self.assertEqual(s.supplier(self.args), 0)
        receipt = self.receipt()
        self.assertTrue(receipt['complete']); self.assertTrue(receipt['supplied'])
        self.assertFalse(receipt['accepted']); self.assertFalse(receipt['installation_admitted'])
        self.assertEqual(set(receipt['binary_pins']), {'osv-scanner', 'osv-reporter', 'govulncheck'})
        self.assertEqual(set(receipt['identity']), {'GITHUB_REPOSITORY', 'GITHUB_SHA', 'GITHUB_JOB',
                                                   'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'})
        self.assertEqual([c[3] for c in self.calls if c[3].startswith('build-')],
                         ['build-osv-scanner', 'build-osv-reporter', 'build-govulncheck'])
        source = self.output / 'work/scanner-source'
        self.assertIn(b'golang.org/x/net v0.58.0/go.mod ', (source / 'go.sum').read_bytes())
        self.assertIn(b'golang.org/x/net v0.60.0/go.mod ', (source / 'go.sum').read_bytes())

    def test_failed_native_build_is_incomplete_and_never_runs_next_target(self):
        self.fail_phase = 'build-osv-reporter'
        self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete']); self.assertFalse(self.receipt()['supplied'])
        self.assertNotIn('build-govulncheck', [c[3] for c in self.calls])
        self.assertEqual(self.receipt()['commands'][-1]['exit'], 9)

    def test_real_source_mutation_at_closeout_refuses_completion(self):
        self.corrupt_source = True
        self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete'])
        self.assertIn('modified authenticated source', self.receipt()['error'])

    def test_real_toolchain_mutation_at_closeout_refuses_completion(self):
        self.corrupt_tool = True
        self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete'])
        self.assertIn('toolchain changed', self.receipt()['error'])

    def test_missing_exact_scanner_origin_refuses_before_builds(self):
        del self.rows[s.SCANNER]['Origin']
        self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete'])
        self.assertIn('origin metadata missing', self.receipt()['error'])
        self.assertFalse(any(c[3].startswith('build-') for c in self.calls))

    def test_real_final_rename_deadline_fault_leaves_only_incomplete_receipt(self):
        real_rename = os.rename
        def late_rename(old, new):
            real_rename(old, new)
            # The next shared-budget check must veto the now-real publication.
            budget.end = 0
        budget = s.Budget(30)
        with mock.patch.object(s, 'Budget', return_value=budget), mock.patch.object(s.os, 'rename', side_effect=late_rename):
            self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete']); self.assertFalse(self.receipt()['supplied'])
        self.assertFalse(self.receipt()['accepted'])

    def test_real_positive_receipt_fsync_fault_leaves_only_incomplete_receipt(self):
        real_write = s.write_fresh
        def failure(path, raw, mode=0o600):
            real_write(path, raw, mode)
            if path.name == '.supplier-delivery-proof.pending':
                raise OSError('controlled final receipt I/O failure')
        with mock.patch.object(s, 'write_fresh', side_effect=failure):
            self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(self.receipt()['complete']); self.assertFalse(self.receipt()['supplied'])

    def test_cache_missing_refuses_without_creation_or_download(self):
        self.args.gocache = str(self.base / 'missing-cache')
        self.assertEqual(s.supplier(self.args), 2)
        self.assertFalse(Path(self.args.gocache).exists())
        self.assertFalse(self.calls)
        self.assertFalse(self.receipt()['complete'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
