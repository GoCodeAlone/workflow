# Plugin Conformance Compatibility Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add artifact-bound plugin conformance evidence to `wfctl` and use it during plugin install/update/lock resolution.

**Architecture:** `wfctl plugin conformance` produces one strict typed-IaC evidence record for a local plugin dir or release archive. Registry sources fetch/synthesize compatibility indexes, and shared resolver code ranks or rejects plugin versions using trusted archive-digest-bound evidence. Lockfiles record platform-scoped compatibility metadata without breaking older fields.

**Tech Stack:** Go CLI (`flag`), `gopkg.in/yaml.v3`, stdlib JSON/tar/gzip/sha256/process APIs, existing `go-plugin` external plugin protocol, existing wfctl registry/install/lock code.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 7
**Estimated Lines of Change:** ~1800

**Out of scope:**
- Plugin repo adoption PRs before the `wfctl plugin conformance` command lands. Follow-up PRs should update `workflow-plugin-digitalocean`, `workflow-plugin-aws`, and other typed-IaC plugins to call this command.
- Signed third-party compatibility evidence.
- Live provider acceptance tests that call cloud APIs or require credentials.
- Hosted compatibility service.
- Cross-source compatibility index pointers unless current manifests already require them.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add wfctl plugin conformance compatibility | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7 | codex/plugin-conformance-compat |

**Status:** Locked 2026-05-11T04:43:54Z

## Task 1: Evidence Model And Semver Utilities

**Files:**
- Create: `cmd/wfctl/plugin_compat_model.go`
- Create: `cmd/wfctl/plugin_compat_model_test.go`
- Modify: `cmd/wfctl/registry_config.go`

**Step 1: Write failing tests**

Add tests for:
- optional leading `v` canonicalization: `0.51.2` and `v0.51.2` both emit `v0.51.2`
- invalid version rejection: `main`, `v0.0.0-20260510`, `1.2`
- `evidenceDigest` canonical JSON excludes only `evidenceDigest`
- `archiveSHA256` and `binarySHA256` validation require lowercase/uppercase hex acceptance but normalized lowercase output
- `RegistrySourceConfig.compatibilityEvidence.trust` parses `first_party` and `advisory`, rejects `signed`

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginCompat(Model|Digest|Version|Trust)' -count=1`

Expected: FAIL with missing types/functions.

**Step 2: Implement model helpers**

Create:
- `PluginVersionIndex`
- `PluginVersionRecord`
- `PluginCompatibilityEvidence`
- `PluginCompatibilityRange`
- `CompatibilityEvidencePolicy`
- `CompatibilityTrustMode`
- `CanonicalPluginVersion`
- `CanonicalEngineVersion`
- `NormalizeSHA256Hex`
- `ComputeEvidenceDigest`
- `ValidateCompatibilityEvidence`

Extend `RegistrySourceConfig`:

```go
CompatibilityEvidence RegistryCompatibilityEvidenceConfig `yaml:"compatibilityEvidence,omitempty" json:"compatibilityEvidence,omitempty"`
```

Use strict enums:
- `first_party`
- `advisory`

Reject `signed` with an error until the signature ADR exists.

**Step 3: Verify**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginCompat(Model|Digest|Version|Trust)' -count=1`

Expected: PASS.

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin_compat_model.go cmd/wfctl/plugin_compat_model_test.go cmd/wfctl/registry_config.go
git commit -m "feat(wfctl): add plugin compat evidence model"
```

Rollback: revert this commit; no runtime paths use the new model yet.

## Task 2: Registry Version Index Fetching

**Files:**
- Modify: `cmd/wfctl/registry_source.go`
- Modify: `cmd/wfctl/multi_registry.go`
- Modify: `cmd/wfctl/registry_source_test.go`
- Modify: `cmd/wfctl/multi_registry_test.go`

**Step 1: Write failing tests**

Add tests for:
- GitHub source fetches `compatibility/<plugin>/index.json`
- Static source fetches `{baseURL}/compatibility/<plugin>/index.json`
- missing index synthesizes a single-version index from `FetchManifest`
- `MultiRegistry.FetchVersionIndex` returns index from same source that resolved manifest
- original-name then normalized-name lookup matches `FetchManifest`
- default GoCodeAlone registry entries are `first_party`; user registries default `advisory`

Run: `GOWORK=off go test ./cmd/wfctl -run 'Test(GitHub|Static|MultiRegistry).*VersionIndex|TestRegistryCompatibilityTrustDefaults' -count=1`

Expected: FAIL with missing `FetchVersionIndex`.

**Step 2: Implement source API**

Add `FetchVersionIndex(name string) (*PluginVersionIndex, error)` to `RegistrySource`.

Implement:
- `GitHubRegistrySource.FetchVersionIndex`
- `StaticRegistrySource.FetchVersionIndex`
- `MultiRegistry.FetchVersionIndex`
- synthetic single-version fallback from manifest when native index is missing
- trust derivation from `RegistrySourceConfig`

Keep same-source invariant: once a manifest resolves from source `S`, index lookup for install/update/lock uses `S` unless there is no native index and synthetic fallback is needed.

**Step 3: Verify**

Run: `GOWORK=off go test ./cmd/wfctl -run 'Test(GitHub|Static|MultiRegistry).*VersionIndex|TestRegistryCompatibilityTrustDefaults' -count=1`

Expected: PASS.

**Step 4: Commit**

```bash
git add cmd/wfctl/registry_source.go cmd/wfctl/multi_registry.go cmd/wfctl/registry_source_test.go cmd/wfctl/multi_registry_test.go
git commit -m "feat(wfctl): fetch plugin version indexes"
```

Rollback: revert this commit plus Task 1 if interface churn blocks builds.

## Task 3: `wfctl plugin conformance`

**Files:**
- Modify: `cmd/wfctl/plugin.go`
- Create: `cmd/wfctl/plugin_conformance.go`
- Create: `cmd/wfctl/plugin_conformance_test.go`
- Create: `cmd/wfctl/testdata/conformance/iac-pass/go.mod`
- Create: `cmd/wfctl/testdata/conformance/iac-pass/main.go`
- Create: `cmd/wfctl/testdata/conformance/iac-pass/plugin.json`
- Create: `cmd/wfctl/testdata/conformance/iac-hang/go.mod`
- Create: `cmd/wfctl/testdata/conformance/iac-hang/main.go`
- Create: `cmd/wfctl/testdata/conformance/iac-hang/plugin.json`

**Step 1: Write failing tests**

Add tests for:
- `wfctl plugin conformance --help` lists `--artifact`, `--mode`, `--engine-version`, `--timeout`
- exactly one of `<plugin-dir>` or `--artifact` is required
- fake typed-IaC plugin passes and emits JSON with `status:"pass"`, `mode:"typed-iac"`, `binarySHA256`, `pluginManifestSHA256`, `evidenceDigest`
- `--output <path>` writes the JSON evidence file and stdout stays concise
- `--format text` emits human-readable pass/fail without JSON
- archive mode emits `archiveSHA256` matching the tarball and uses extracted contents, not source dir
- local-dir mode emits no `archiveSHA256` and is marked advisory for registry enforcement
- provider fixture implementing `SupportedCanonicalKeys` is called, but resource `Read`, `Plan`, `Apply`, `Destroy`, bootstrap, and credential methods are not called
- fake plugin with no typed-IaC service fails
- hanging plugin is killed on timeout and emits bounded stderr/stdout tails
- no provider credential/env data appears in JSON output

Fixture `go.mod` files must use:

```go
replace github.com/GoCodeAlone/workflow => ../../../../..
```

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginConformance' -count=1`

Expected: FAIL with unknown subcommand.

**Step 2: Implement command**

Add `conformance` to `runPlugin` and usage.

Implement:
- local-dir staging
- `--artifact` tar.gz extraction and archive hashing
- installed layout staging under temp dir
- build fallback with `go build -o <tmp>/plugins/<name>/<name> .`
- conformance-specific launcher with timeout and process kill
- typed-IaC service discovery through existing plugin protocol/contract registry
- metadata-only RPC checks; no resource/credential calls
- JSON/text output

Document `--engine-version local` as an advisory sentinel, or remove sentinel wording and make non-semver engine versions advisory through `WFCTL_ENGINE_VERSION`.

**Step 3: Verify CLI behavior**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginConformance' -count=1`

Expected: PASS.

Run: `GOWORK=off go run ./cmd/wfctl plugin conformance --mode typed-iac --format json ./cmd/wfctl/testdata/conformance/iac-pass`

Expected: JSON contains `"status":"pass"` and no environment variable values.

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin.go cmd/wfctl/plugin_conformance.go cmd/wfctl/plugin_conformance_test.go cmd/wfctl/testdata/conformance
git commit -m "feat(wfctl): add plugin conformance command"
```

Rollback: revert this commit; plugin loading runtime falls back to existing `ExternalPluginManager` behavior.

## Task 4: Registry Compatibility Update Command

**Files:**
- Modify: `cmd/wfctl/registry_cmd.go`
- Create: `cmd/wfctl/registry_compatibility.go`
- Create: `cmd/wfctl/registry_compatibility_test.go`

**Step 1: Write failing tests**

Add tests for:
- `wfctl plugin-registry compatibility update --help`
- update reads `plugins/<plugin>/manifest.json`
- update validates evidence plugin/version/mode/status/os/arch/engine
- update rejects evidence whose `archiveSHA256` does not match a manifest download
- update writes `compatibility/<plugin>/index.json` atomically
- update sorts versions descending and evidence by engine/mode/os/arch
- update leaves existing index untouched on validation failure
- range derivation only occurs when min/latest pass and no explicit fail exists inside enumerated range
- stale marker is set when `--latest-engine` is newer than the newest evidence for that plugin

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRegistryCompatibilityUpdate' -count=1`

Expected: FAIL with unknown subcommand.

**Step 2: Implement command**

Add `compatibility update` under `wfctl plugin-registry` because this command edits the plugin catalog registry. Do not add it under `wfctl registry`; that surface now owns container registry login/push/prune/logout.

Implement flags:
- `--registry-dir`
- `--plugin`
- `--version`
- repeatable `--evidence`
- optional `--derive-ranges`
- optional `--latest-engine`

Use temp-file write, fsync where supported, and atomic rename.

**Step 3: Verify CLI behavior**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRegistryCompatibilityUpdate' -count=1`

Expected: PASS.

Run against a temp test registry fixture and inspect stable JSON diff.

Expected: index file contains one version record with validated evidence.

**Step 4: Commit**

```bash
git add cmd/wfctl/registry_cmd.go cmd/wfctl/registry_compatibility.go cmd/wfctl/registry_compatibility_test.go
git commit -m "feat(wfctl): update registry compat indexes"
```

Rollback: revert this commit; generated indexes can still be produced manually from evidence JSON but should not be trusted for install enforcement.

## Task 5: Compatibility Resolver For Install And Update

**Files:**
- Create: `cmd/wfctl/plugin_compat_resolver.go`
- Create: `cmd/wfctl/plugin_compat_resolver_test.go`
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_update_test.go`
- Modify: `cmd/wfctl/plugin_install_test.go`

**Step 1: Write failing tests**

Add tests for:
- newest exact trusted pass wins
- newer exact trusted fail is skipped in favor of older pass
- requested `<name>@<version>` fails on exact trusted fail in enforce mode
- `--compat-mode warn` permits known-fail and records forced reason
- `--force` permits known-fail while still enforcing checksum unless `--skip-checksum`
- missing required first-party evidence blocks at/after `requiredFromEngine`
- missing advisory evidence falls back to `minEngineVersion` with warning
- pseudo local `wfctl version` makes evidence advisory unless `WFCTL_ENGINE_VERSION` or `--engine-version` supplies semver

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginCompatResolver|TestRunPluginInstall.*Compat|TestRunPluginUpdate.*Compat' -count=1`

Expected: FAIL with missing resolver and flags.

**Step 2: Implement resolver**

Implement shared resolver:
- candidate collection from version index
- semver filter by `minEngineVersion`
- current platform evidence match by `archiveSHA256`
- exact fail/pass/range precedence
- `enforce|warn` mode from CLI > env > config > default
- `--engine-version` and `WFCTL_ENGINE_VERSION`
- force reason values: `force-install`, `force-update`, `compat-mode=warn`

Wire into:
- `runPluginInstall`
- `runPluginUpdate`

Keep direct `--url`, `--local`, and GitHub fallback paths outside compatibility enforcement because they are not registry-index-backed.

**Step 3: Verify**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginCompatResolver|TestRunPluginInstall.*Compat|TestRunPluginUpdate.*Compat' -count=1`

Expected: PASS.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginInstallE2E|TestRunPluginUpdate' -count=1`

Expected: PASS.

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin_compat_resolver.go cmd/wfctl/plugin_compat_resolver_test.go cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go cmd/wfctl/plugin_update_test.go
git commit -m "feat(wfctl): resolve plugins by compat evidence"
```

Rollback: set `WFCTL_PLUGIN_COMPAT_MODE=warn` for emergency rollout; revert this commit to restore manifest-only install/update selection.

## Task 6: Lockfile Compatibility Metadata

**Files:**
- Modify: `config/wfctl_lockfile.go`
- Modify: `config/wfctl_lockfile_test.go`
- Modify: `cmd/wfctl/plugin_lock.go`
- Modify: `cmd/wfctl/plugin_lock_test.go`

**Step 1: Write failing tests**

Add tests for:
- lockfile writes platform compatibility metadata under `platforms.<os-arch>.compatibility`
- lockfile round-trips additive fields without dropping URL/SHA256
- `plugin lock` chooses newest compatible version when manifest omits version
- explicit manifest version fails on known-fail evidence in enforce mode
- warn mode keeps known-fail with `forced:true` and `reason: compat-mode=warn`
- project-local registry config still controls lock registry enrichment; user-global config must not silently replace it unless explicitly tested and intended
- older lockfile fixtures without compatibility metadata still load

Run: `GOWORK=off go test ./config ./cmd/wfctl -run 'Test(WfctlLockfile|PluginLock).*Compat|TestPluginLock_FromManifest' -count=1`

Expected: FAIL with missing compatibility fields.

**Step 2: Implement lock metadata**

Add `WfctlLockCompatibility` to `config`.

Extend `WfctlLockPlatform` with:

```go
Compatibility *WfctlLockCompatibility `yaml:"compatibility,omitempty"`
```

Update deterministic YAML writer to include compatibility fields in stable order.

Wire `runPluginLockFromManifest` through the shared compatibility resolver, preserving the project-local registry lookup behavior already covered by tests.

**Step 3: Verify**

Run: `GOWORK=off go test ./config ./cmd/wfctl -run 'Test(WfctlLockfile|PluginLock).*Compat|TestPluginLock_FromManifest' -count=1`

Expected: PASS.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestPluginLock' -count=1`

Expected: PASS.

**Step 4: Commit**

```bash
git add config/wfctl_lockfile.go config/wfctl_lockfile_test.go cmd/wfctl/plugin_lock.go cmd/wfctl/plugin_lock_test.go
git commit -m "feat(wfctl): lock plugin compat metadata"
```

Rollback: revert this commit; generated lockfiles can be regenerated without compatibility metadata.

## Task 7: Documentation, Broad Verification, And Runtime Validation

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `docs/plans/2026-05-11-plugin-conformance-compat.md`

**Step 1: Document commands**

Add concise docs for:
- `wfctl plugin conformance`
- `wfctl plugin-registry compatibility update`
- registry `compatibilityEvidence.trust`
- install/update/lock `--compat-mode`
- `WFCTL_PLUGIN_COMPAT_MODE`
- `WFCTL_ENGINE_VERSION`
- plugin CI adoption sketch using `setup-wfctl`

**Step 2: Focused verification**

Run:

```bash
GOWORK=off go test ./cmd/wfctl ./config ./plugin/external ./plugin/external/sdk -count=1
```

Expected: PASS.

**Step 3: Broader verification**

Run:

```bash
GOWORK=off go test ./... -count=1
```

Expected: PASS, or documented unrelated existing failures.

**Step 4: Runtime validation**

Run:

```bash
GOWORK=off go build -o /tmp/wfctl-compat ./cmd/wfctl
/tmp/wfctl-compat plugin conformance --mode typed-iac --format json ./cmd/wfctl/testdata/conformance/iac-pass
tar -czf /tmp/wfctl-iac-pass.tar.gz -C ./cmd/wfctl/testdata/conformance/iac-pass .
/tmp/wfctl-compat plugin conformance --mode typed-iac --artifact /tmp/wfctl-iac-pass.tar.gz --format json
/tmp/wfctl-compat plugin-registry compatibility update --registry-dir /tmp/wfctl-test-registry --plugin workflow-plugin-test --version v0.1.0 --evidence /tmp/wfctl-evidence.json
```

Expected:
- build exits 0
- conformance JSON includes `status:"pass"`
- archive conformance JSON includes matching `archiveSHA256`
- registry update writes `compatibility/workflow-plugin-test/index.json`

**Step 5: Commit**

```bash
git add docs/WFCTL.md docs/plans/2026-05-11-plugin-conformance-compat.md
git commit -m "docs(wfctl): document plugin compat conformance"
```

Rollback: docs-only revert.

## Final PR Checklist

- Run `git status --short`.
- Run `git log --oneline origin/main..HEAD`.
- Run `GOWORK=off go test ./cmd/wfctl ./config ./plugin/external ./plugin/external/sdk -count=1`.
- Run runtime validation from Task 7.
- Open PR against `GoCodeAlone/workflow`.
- Start PR monitoring and address CI/review findings.
- After merge and release, create follow-up plugin PRs to replace repo-local conformance scripts with `wfctl plugin conformance --artifact`.
