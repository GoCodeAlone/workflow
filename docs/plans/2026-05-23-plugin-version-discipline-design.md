# Plugin version discipline: delete sync mechanism + wfctl contract gate — design

Issue: GoCodeAlone/workflow#758
Date: 2026-05-23 (cycle 4-revC — restored ldflag-arg pattern per cycle-A2 N-C1 verifying go-build vs go-install Main.Version behavior; swept §6 vocabulary; matched actual *grpcServer shape)
Mode: autonomous execution authorized

## Problem

Per session evidence:

1. `sync-plugin-version.yml` opens unmerged PRs that pile up (13 stale on DO plugin swept manually 2026-05-23).
2. Cycle-1 ldflag-replacement design (`2026-05-23-plugin-version-ldflag-design.md`) failed adversarial with 3 Critical defects.
3. Cycle-3 restored-contract design passed but plan-cycle-1 found audit factually wrong on 8 of 23 repos + curl|bash supply-chain risk + sdk.Serve API gap.
4. User-direction post plan-cycle-1: lint script should be wfctl functionality; sync mechanism should be eliminated entirely (release tarball already carries correct version via goreleaser `before:` hook; nothing in the release path actually requires committed plugin.json's `version` field).

## Verified ground truth (re-audited 2026-05-23)

`workflow-registry/scripts/sync-versions.sh`:

- Line 122 reads `manifest_version` from registry's own `manifest.json` copy (`$PLUGINS_DIR/<name>/manifest.json`), not from plugin repo's committed plugin.json.
- Line 125 derives `latest_version` from `gh release view --json tagName` (upstream git tag).
- Lines 169-183 (`--fix` mode) overwrite registry manifest's `.version` and `.downloads` from tag-derived values, not from plugin repo's committed plugin.json.
- Line 99 `fetch_plugin_json` reads committed plugin.json at the tagged commit for `capabilities + minEngineVersion + iacProvider` ONLY (closes workflow#703). The `.version` field of committed plugin.json is never read.

**Conclusion:** committed `plugin.json.version` has no consumer in the release/registry pipeline. The sync-plugin-version.yml workflow only synchronizes that field aesthetically; eliminating it changes no observable behavior except removing the PR pileup.

The same audit confirms: `capabilities`, `minEngineVersion`, `iacProvider` at the tagged commit MUST be correct (registry reads them at tag time). The shipped tarball's plugin.json `.version` MUST be correct — goreleaser `before:` hook already handles this via `{{ .Version }}` template (`.goreleaser.yaml:7-8` in DO plugin). Binary's `internal.Version` MUST be correct — goreleaser `ldflags -X` already handles (`.goreleaser.yaml:25` in DO plugin).

## Proposed design

### 1. Delete sync-plugin-version.yml from every plugin repo; sentinel committed version

The committed `plugin.json.version` becomes a sentinel: `"0.0.0"`.

**Why `0.0.0` and not `v0.0.0-dev`** (cycle 4-A1 C1): empirically verified that this repo's `PluginManifest.ParseSemver` (`plugin/manifest.go:283-303`) does strict `M.m.p` parsing via `strconv.Atoi` on each dot-split segment. Pre-release tags (`v0.0.0-dev`, `v1.2.3-rc.1`) are rejected — `Atoi("0-dev")` fails. The flat `0.0.0` parses cleanly and passes `Validate()` without any engine-side change. Operator-visible test-build nature is delivered via `sdk.BuildVersion()` at runtime (see §2), not via the disk sentinel string.

For release builds, goreleaser's `before:` hook continues to rewrite the shipped plugin.json with `{{ .Version }}` from the tag (per-repo verification step in Layer 3 confirms the invariant; ~50 plugin repos use in-place sed against `plugin.json`, ~4 use `.release/plugin.json` — both patterns satisfy the invariant). Shipped tarball carries the correct version.

Registry sync derives version from tag (`workflow-registry/scripts/sync-versions.sh:125,132,169`), unchanged. The tag-arrival heartbeat that previously came via sync-plugin-version.yml PR-opening is already replaced by the G1 chain shipped 2026-05-21 (plugin tag → notify dispatch → workflow-registry sync); see workspace memory `project_g1234_plugin_release_chain_complete.md`. Heartbeat is not lost.

**No engine change. No manifest schema change.** Workflow-file deletion + one-line plugin.json edit per plugin repo.

### 2. Plugin contract surface: SDK changes

Goal: plugin binary surfaces its build-injected version through `GetManifest` so engine, operator, and observability tools see runtime-truth.

**Cycle 4-A2 N-C1 correction:** goreleaser invokes `go build`, not `go install`. `runtime/debug.ReadBuildInfo().Main.Version` returns a pseudo-version (`v0.0.0-<ts>-<sha>`) or `"(devel)"` for goreleaser-built binaries — NOT the release tag. The ldflag pattern `-X <pkg>.Version={{.Version}}` is the only mechanism that delivers the tag string at runtime. Restore the cycle-3 arg-taking helper.

**Symbol-name variance is accepted.** Plugin authors name ANY package-level var (`internal.Version`, `provider.ProviderVersion`, `main.version`). `wfctl plugin validate-contract` greps the goreleaser config for the ldflag pattern, not a specific symbol path.

Add new file `plugin/external/sdk/buildversion.go`:

```go
// ResolveBuildVersion returns the operator-visible build-version string.
// declared non-empty + not a known dev sentinel → returned as-is (typical
// for goreleaser-built binaries where the ldflag injects the release tag).
// Otherwise consults runtime/debug.ReadBuildInfo() as fallback:
//   - "(devel) [@ shortsha[.dirty]]" when vcs.revision is set
//   - "(devel)" when no VCS info
//
// Intended call sites (plugin author chooses ANY package-level var name):
//
//   var Version = "dev"   // somewhere in plugin code; ldflag-injected at release
//
//   sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
//       BuildVersion: sdk.ResolveBuildVersion(internal.Version),
//   })
//   // OR for non-IaC:
//   sdk.Serve(p, sdk.WithManifestProvider(m),
//       sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))
//
// Goreleaser config provides the tag:
//   ldflags:
//     - -X github.com/<...>/internal.Version={{.Version}}
//
// Dev sentinels (declared values that trigger BuildInfo fallback): `""`,
// `"dev"`, `"(devel)"`. Mirrors the pattern wfctl itself uses
// (cmd/wfctl/main.go:37-50 — `var version = "dev"` with ldflag override
// primary + BuildInfo fallback secondary).
func ResolveBuildVersion(declared string) string { ... }
```

Add `BuildVersion string` field to `IaCServeOptions` (current `plugin/external/sdk/iacserver.go:320`).

Add `WithBuildVersion` ServeOption to `plugin/external/sdk/serve.go`. Existing ServeOption shape is `func(*grpcServer)` (verified at `serve.go:16`); add a `buildVersion string` field to the `grpcServer` struct (current `grpc_server.go:21-39` — alongside the existing `diskManifest *pluginpkg.PluginManifest` field):

```go
// In grpc_server.go's grpcServer struct:
type grpcServer struct {
    // ... existing fields ...
    diskManifest *pluginpkg.PluginManifest
    buildVersion string  // workflow#758: runtime version override via WithBuildVersion
}

// In serve.go:
func WithBuildVersion(v string) ServeOption {
    return func(s *grpcServer) {
        s.buildVersion = v
    }
}
```

Single channel: `grpc_server.go:142-162` `GetManifest` body augmented as below; `iacPluginServiceBridge.GetManifest` (`iacserver.go:300-312`) gets the identical augmentation. BuildVersion always wins when non-empty; precedence explicit and unit-tested.

```go
// grpc_server.go GetManifest, after existing diskManifest-or-provider branch:
m := /* existing computed *pb.Manifest from diskManifest or provider.Manifest() */
if s.buildVersion != "" {
    m.Version = s.buildVersion
}
return m, nil
```

```go
// iacserver.go GetManifest, replacing current line 300-312:
if b.diskManifest == nil {
    return nil, status.Error(codes.Unimplemented, "manifest not embedded; engine falls back to disk plugin.json")
}
out := &pb.Manifest{
    Name:           b.diskManifest.Name,
    Version:        b.diskManifest.Version,
    Author:         b.diskManifest.Author,
    Description:    b.diskManifest.Description,
    ConfigMutable:  b.diskManifest.ConfigMutable,
    SampleCategory: b.diskManifest.SampleCategory,
}
if b.buildVersion != "" {
    out.Version = b.buildVersion
}
return out, nil
```

Add `buildVersion string` field to `iacPluginServiceBridge`; wire from `IaCServeOptions.BuildVersion` at construction in `ServeIaCPlugin`.

Engine-side: optional one-shot warning log in `plugin/external/adapter.go` when post-spawn GetManifest's Version differs from `diskManifest.Version`. Pure observability; no behavior change.

### 3. `wfctl plugin validate-contract` subcommand

New subcommand under existing `wfctl plugin` family. Replaces the cycle-3 plan's separate `check-plugin-contract.sh` (eliminates curl|bash supply-chain risk; collapses tooling into the binary plugin authors already install via `setup-wfctl`).

Surface:

```
wfctl plugin validate-contract <plugin-dir>
wfctl plugin validate-contract --for-publish <plugin-dir>
wfctl plugin validate-contract --for-publish --tag <vX.Y.Z> <plugin-dir>
```

Checks (always):

1. `<dir>/plugin.json` exists, parses, passes `PluginManifest.Validate()`. Sentinel `0.0.0` allowed (parses cleanly through current ParseSemver; emits "dev sentinel" info note).
2. `capabilities` populated (non-empty).
3. `minEngineVersion` populated (parses as semver constraint).
4. Any `cmd/**/main.go` (or other Go file under repo root) contains a call to `sdk.ResolveBuildVersion(` AND a `sdk.IaCServeOptions{...BuildVersion:...}` literal OR a `sdk.WithBuildVersion(` call.
5. Goreleaser config (`.goreleaser.yaml` or `.goreleaser.yml`) at repo root contains an ldflags line matching `-X .*\.Version=` (any package path; e.g. `-X github.com/.../internal.Version={{.Version}}`). This is the mandatory mechanism that delivers the release tag into the binary at build time (cycle 4-A2 N-C1: `debug.ReadBuildInfo` alone returns pseudo-version, not the tag).

Additional checks (`--for-publish`):

6. Tag from `--tag <vX.Y.Z>` flag (if provided) OR from `$GITHUB_REF_NAME` env (if set) OR from `git describe --tags --exact-match HEAD` matches strict-release-semver regex: `^v[0-9]+\.[0-9]+\.[0-9]+$` (cycle 4-A1 C2/I5 fix — no prerelease branch; engine's ParseSemver rejects prereleases. Prerelease publishing deferred to a separate design that updates ParseSemver + sync-versions + all consumers in concert).
7. Committed plugin.json's `.version` is allowed to disagree with `--tag` (sentinel `0.0.0` is the documented norm; tarball-shipped version is what matters).

Exit non-zero on any failure with operator-friendly error referencing `docs/PLUGIN_RELEASE_GATES.md`.

**Tarball-postcheck (`--release-dir <path>`, optional):** when invoked with `--release-dir .release` after goreleaser's before-hook has run, additionally asserts `<path>/plugin.json`'s `.version` field equals the `--tag` value (strips leading `v`). This closes cycle 4-A2 N-I3 by giving Layer 3 release.yml a place to run the tarball-invariant assertion. Layer 3 step 8 in §6 below uses this flag.

### 4. Tag-format gate in each plugin's `release.yml`

Two steps in every plugin's release.yml — first (pre-build) gates the tag + static contract; second (post-build) verifies the tarball.

```yaml
# 1. Pre-build gate: static contract + tag format
- uses: GoCodeAlone/setup-wfctl@v1
  with:
    version: v0.61.0  # bump when workflow ships a new validate-contract iteration
- name: Validate plugin contract for publish (pre-build)
  run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .

# 2. (goreleaser runs here, mutating plugin.json in-place or writing .release/plugin.json)

# 3. Post-build gate: tarball carries the tag
- name: Verify shipped plugin.json carries tag (post-build)
  run: |
    # Find shipped plugin.json (goreleaser pattern variance: in-place or .release/)
    if [ -f .release/plugin.json ]; then
      wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir .release .
    else
      wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir . .
    fi
```

Malformed tag, incomplete contract, or tarball-without-tag → release halts before publish.

### 5. Registry-side semver gate (defense in depth)

`workflow-registry/scripts/sync-versions.sh` adds the same tag regex check after `latest_tag` is set:

```bash
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver (engine ParseSemver requires flat M.m.p)"
  continue
fi
```

Catches plugins that bypass release.yml (self-hosted runner, manual tarball, force-push). Same regex source as `wfctl plugin validate-contract --for-publish`'s rule 6.

### 6. Migration ordering

- **Layer 1 (workflow repo, single PR)**: SDK changes (§2) + `wfctl plugin validate-contract` subcommand (§3) + `docs/PLUGIN_RELEASE_GATES.md`. Tag workflow `v0.61.0`. Update `setup-wfctl` action's version (or rely on `latest`).
- **Layer 2 (workflow-registry repo, single PR)**: tag-string semver gate in `sync-versions.sh` (§5). Can ship in parallel with Layer 1.
- **Layer 3 (per-plugin PRs, parallel)**: in each plugin repo with a release pipeline today:
  1. `git rm .github/workflows/sync-plugin-version.yml`
  2. Add pre-build + post-build gate steps to `.github/workflows/release.yml` per §4
  3. Update plugin main.go (or equivalent — any Go file under repo root that calls `sdk.ServeIaCPlugin`/`sdk.Serve`) to pass `sdk.ResolveBuildVersion(<plugin's existing Version var>)` to `IaCServeOptions.BuildVersion` or `WithBuildVersion`. The "Version var" name varies per repo (DO: `internal.Version`; AWS: `provider.ProviderVersion`; etc.); use whichever already exists. If no such var exists yet, add `var Version = "dev"` in the package the goreleaser ldflag targets (per `.goreleaser.yaml`).
  4. Set `plugin.json.version` to `"0.0.0"` (sentinel — flat M.m.p that ParseSemver accepts; cycle 4-A1 C1 fix)
  5. Verify `.goreleaser.yaml` has an ldflag line matching `-X .*\.Version=` (most do; verify per repo; the cycle-3 audit table covers this for DO, AWS, others)
  6. Local: `wfctl plugin validate-contract .` must pass (covers contract rules 1-5 from §3)
  7. Open PR; CI runs `wfctl plugin validate-contract .` again (rule 1-5); on tag push, release.yml's pre-build + post-build gates fire
  8. Admin-merge after CI green

Each Layer 3 PR is independent and can run in parallel via per-repo worktree-isolated agents.

### 7. Gap-repo handling (deferred)

Repos lacking a release pipeline get one filed issue each: "Establish release pipeline (workflow#758 prerequisite)." Not in Layer 3 scope.

## Assumptions

A1. `goreleaser`'s `before:` hook writes the correct version into the shipped plugin.json (either in-place or via `.release/plugin.json`) for every plugin repo with a release.yml. ~50 repos use in-place sed; ~4 use `.release/`. Per-repo verification step in each Layer 3 PR asserts the invariant: tarball plugin.json carries `{{ .Version }}`-stamped value.
A2. `setup-wfctl` GitHub Action exists and pins to a wfctl version. Verified by workspace memory; verified by the action being used in DO plugin release.yml today via prior session work.
A3. `PluginManifest.ParseSemver` (`plugin/manifest.go:283-303`) accepts flat `0.0.0` (verified empirically in cycle 4-A1 review). Pre-release tags are rejected by current parser — the sentinel choice + tag regex BOTH avoid prerelease syntax. Full SemVer 2.0.0 support is a deferred follow-up.
A4. `wfctl plugin install --local` reads committed plugin.json; reports the sentinel `0.0.0` to operators. The intended dev-install signal (test-build branch nature) comes from `sdk.BuildVersion()`'s runtime output via GetManifest, not from the disk sentinel.
A5. `workflow-registry/scripts/sync-versions.sh` already derives the registry-visible version from upstream git tag. Verified at lines 122, 125, 132, 169. The `MISMATCH` warning compares registry's local manifest copy against upstream tag — NOT against plugin repo's committed plugin.json (cycle 4-A1 I2 correction).
A6. Layer 3 scope ≈ 15 plugin repos with release pipelines today (drops the ~8 gap-repos identified in plan-cycle-1 audit; per-repo Layer 3 auditor agent confirms gap or proceeds).
A7. Tag-arrival heartbeat (sync-plugin-version.yml PR was the prior signal) is already replaced by the G1 chain shipped 2026-05-21 (plugin tag → notify dispatch → workflow-registry sync). Heartbeat not lost (cycle 4-A1 I6 dismissed).
A8. Goreleaser-built binaries populate `runtime/debug.ReadBuildInfo().Main.Version` correctly (verified by precedent: `cmd/wfctl/main.go:45-50` uses this pattern for wfctl's own version surface).

## Self-challenge — top 3 doubts

D1. **Sentinel `0.0.0` looks alarming to consumers that compare versions numerically.** Mitigation: documented in PLUGIN_RELEASE_GATES.md as the intentional dev-sentinel. `sync-versions.sh` is unaffected (it reads the tag, not the committed file — cycle 4-A1 I2 correction). Operator-visible dev nature comes from `sdk.BuildVersion()` runtime output, not the disk value.
D2. **Losing git-history audit of version progression.** Yes, but git tag log is the authoritative version history; the committed plugin.json changing was redundant ceremony. Heartbeat preserved via existing G1 notify-dispatch chain.
D3. **No binary-vs-file capability freshness gate.** Acknowledged out-of-scope per cycle 4-A1 I3. The existing `fetch_plugin_json` path (`sync-versions.sh:99`) reads capabilities from the committed plugin.json at the tag commit; if maintainers forget to update capabilities pre-tag, registry inherits stale values. A future contract-check enhancement could spawn the built binary and diff its `GetContractRegistry` RPC against the committed file — separate design.

## Rollback

- §1 (delete sync-plugin-version.yml + sentinel): per-repo revert restores the workflow + reverts plugin.json. PR pileup returns; no other regression.
- §2 (SDK changes): purely additive on `IaCServeOptions` + `sdk.Serve` ServeOption. Plugins that don't set `BuildVersion` keep existing behavior. Revert is single workflow-repo file change.
- §3 (`wfctl plugin validate-contract`): additive subcommand. Existing `wfctl plugin validate` unchanged.
- §4 (tag-format gate in release.yml): per-repo revert removes the step.
- §5 (registry-side gate): single revert in `sync-versions.sh`.

No state migrations, no plugin-contract breakages, no engine-version cliffs.

## Test plan

- workflow Layer 1: unit tests for `sdk.ResolveBuildVersion`, `IaCServeOptions.BuildVersion`, `sdk.WithBuildVersion`, `wfctl plugin validate-contract` (table-driven against testdata fixtures); existing PluginManifest + GetManifest test suites must stay green.
- workflow-registry Layer 2: shell test fixtures for tag-format regex (good + bad cases).
- Per-plugin Layer 3: each repo's existing CI + `wfctl plugin validate-contract .` invocation in release.yml gates the next tag.

No live infra validation required for this PR set.

## Out of scope

- Dropping `plugin.json.version` field entirely (sentinel keeps the field; full removal needs separate design dealing with PluginManifest.Validate's required-field invariant).
- Replacing goreleaser.
- Establishing release pipelines in gap-repos (deferred to per-repo follow-up issues).
- Engine-side hard-blocking minEngineVersion mismatches (existing soft-warn behavior is fine).
- Full SemVer 2.0.0 pre-release tag support (requires concerted ParseSemver + sync-versions + wfctl install update; deferred to separate design).
- Binary-vs-file capability freshness gate at contract-check time (cycle 4-A1 I3; deferred to separate design).

## Cycle 4-A2 — addressed

- N-C1 (debug.ReadBuildInfo returns pseudo-version, not tag, for goreleaser-built binaries): **addressed** — restored `sdk.ResolveBuildVersion(declared string)` arg-taking helper; goreleaser ldflag mandatory in contract-check rule 5; symbol-name variance accepted (validate-contract greps `.goreleaser.yaml` for ldflag pattern, not a specific Go symbol).
- N-C2 (§6 inconsistent vocabulary): **addressed** — §6 swept; sentinel `0.0.0` everywhere, `ResolveBuildVersion(declared)` everywhere, ldflag verify present, Version-var-name flexibility documented.
- N-I1 (serveConfig struct doesn't exist): **addressed** — design now references actual `*grpcServer` struct + adds `buildVersion string` field there directly (mirroring existing `diskManifest *pluginpkg.PluginManifest` pattern at `grpc_server.go:39`).
- N-I2 (Serve-path precedence shown only in prose): **addressed** — explicit GetManifest code block for both `grpc_server.go` (non-IaC) and `iacserver.go` (IaC) shown in §2, both demonstrating single-channel "BuildVersion wins when non-empty" precedence.
- N-I3 (no tarball-postcheck step): **addressed** — `wfctl plugin validate-contract --release-dir <path>` flag added; Layer 3 release.yml gains a post-goreleaser verification step asserting `.release/plugin.json` or in-place plugin.json carries the tag.

## Cycle 4-A1 — addressed

- C1 (ParseSemver rejects `v0.0.0-dev`): **addressed** — sentinel changed to flat `0.0.0` which the strict parser accepts; verified empirically. Tag regex tightened to `^v\d+\.\d+\.\d+$` only.
- C2 (regex permits engine-unparseable prerelease tags): **addressed** — prerelease branch dropped from both wfctl validate-contract regex and registry-side regex.
- C3 (`internal.Version` symbol path non-uniform): **addressed** — `sdk.BuildVersion()` no-arg helper uses `runtime/debug.ReadBuildInfo()` directly; plugin authors don't name any specific package-level variable. Contract-check rule reframed to grep for `sdk.BuildVersion(` call site.
- I1 (goreleaser before-hook variance): **addressed** — A1 acknowledges both in-place sed (~50 repos) and `.release/` (~4 repos) patterns; per-repo Layer 3 verification asserts the tarball-invariant, not the path-convention.
- I2 (D1 wrong about MISMATCH lighting up): **addressed** — D1 rewritten; sync-versions.sh's MISMATCH compares against tag, not committed file; sentinel choice has zero observable effect on registry output.
- I3 (binary-vs-file capability freshness gate): **acknowledged out-of-scope**; recorded in "Out of scope" + D3 for future design pickup.
- I4 (two-channel BuildVersion ambiguity): **addressed** — single channel; BuildVersion always wins over diskManifest.Version when non-empty; precedence explicit + unit-tested.
- I5 (unexercised prerelease regex branch): **addressed** — prerelease branch dropped (also addresses C2).
- I6 (lost tag-arrival heartbeat): **addressed** — A7 confirms G1 chain (shipped 2026-05-21) already provides the dispatch path; no replacement signal needed.
