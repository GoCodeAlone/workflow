# Registry Sync Release Fixture

`TestPluginRegistrySyncTarget_ActualCLI` executes the real public CLI against a
temporary committed Git registry. It checks forward sync, two byte-identical
no-ops, rollback, and two byte-identical repeated rollbacks, including canonical
report tree/path/diff bindings and exact-target selection without `latest`.

`TestPluginRegistrySyncHostNegatives` uses one real host binary for the entire
group and independent committed registries. A successful public-CLI control
must produce a correctly bound report before any denial is tested. The 30
negative cases cover changes between release reads (tag, source, release ID,
asset ID, checksum, source metadata), mutable releases, invalid stable targets,
plugin/report path escapes and symlinks, blocked report parents, extra generated
paths, and all eight existing malformed-report cases, including unknown top-level
and nested fields. Each denial requires the specific validation error and exit
code 1, checks whether API access occurred at the expected validation phase,
and verifies unchanged manifest/report bytes, modes and file identities, path
sets, Git HEAD, index entries, and worktree status. A command timeout is a test
failure, not an accepted denial. Report fixtures are seeded through the actual
CLI, never through the in-process registry-sync function.

For Task 35 step 10, verify the downloaded release archive and checksum first,
then check out that release's exact source tag for the tests. Run on Linux:

```sh
GOWORK=off GOTOOLCHAIN=go1.27.2 \
  WFCTL_REGISTRY_SYNC_HOST_BINARY=/absolute/path/to/verified/wfctl \
  go test -p=2 ./cmd/wfctl \
    -run '^TestPluginRegistrySync(Target_ActualCLI|HostNegatives)$' -count=1 -v
```

The explicit file is executed directly, without rebuilding, copying, patching,
or linker overrides. Empty, missing, non-executable, directory, and symlink
selections fail; no source-build fallback is permitted. The selector does not
verify a release checksum or substitute for that prerequisite.

The dependency fixture leaves the binary's `https://api.github.com` endpoint
unchanged. A loopback `HTTPS_PROXY` accepts only `CONNECT api.github.com:443`
and tunnels to a local TLS server with an ephemeral certificate for that DNS
name. `SSL_CERT_FILE` points to its ephemeral CA; `SSL_CERT_DIR` points to an
empty directory. No system trust store, hosts file, or production endpoint is
modified. API responses reuse the structured registry fixture handler and
declare immutable releases; checksum/asset URLs remain loopback HTTP. These
are mock GitHub dependencies, not live release immutability evidence.

Inherited `RELEASES_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN`, proxy settings, and CA
settings are removed from the CLI environment. The proxy rejects other hosts
and proxy credentials; the TLS API rejects authorization headers. Fixture
reports and registry files contain no credentials. All listeners, tunnels,
certificates, and registry files are test-local and cleaned up afterwards.

With the selector unset, ordinary Linux CI builds `wfctl` without linker
overrides once per host-test group (not per negative case) and runs this same
default-endpoint proxy path. Both host groups reuse the same transport fixture
and credential-free environment. On Darwin the baseline
still builds with the existing loopback API linker override. Requested
downloaded-binary proof on Darwin fails explicitly: Go does not use
`SSL_CERT_FILE` there. Darwin's custom-client proxy tests prove only the
dependency fixture, not downloaded-binary conformance:

```sh
GOWORK=off GOTOOLCHAIN=go1.27.2 \
  go test -p=2 ./cmd/wfctl \
    -run '^TestPluginRegistrySync(HostBinary|ReleaseProxy)_' -count=1 -v
```
