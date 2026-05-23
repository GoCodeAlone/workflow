# Plugin version discipline: delete sync mechanism + wfctl contract gate — design

Issue: GoCodeAlone/workflow#758
Date: 2026-05-23 (cycle 4 — simplification per user direction after plan-cycle-1)
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

The committed `plugin.json.version` becomes a sentinel: `"v0.0.0-dev"`. Parseable semver (zero + pre-release tag), so `PluginManifest.Validate()` passes today with no engine change. Local-install paths (`wfctl plugin install --local <dir>`) report the sentinel; operators see the test-build nature.

For release builds, goreleaser's `before:` hook continues to rewrite `.release/plugin.json` with `{{ .Version }}` from the tag. Shipped tarball carries the correct version.

Registry sync derives version from tag, unchanged.

**No code change to engine, SDK, registry script, or wfctl for this piece.** Pure workflow-file deletion + one-line plugin.json edit per plugin repo.

### 2. Plugin contract surface: SDK changes

Goal: plugin binary surfaces its build-injected version through `GetManifest` so engine, operator, and observability tools see runtime-truth (not stale disk sentinel).

Add to `plugin/external/sdk/iacserver.go`:

```go
type IaCServeOptions struct {
    // ... existing fields ...
    BuildVersion string
}
```

Add to `plugin/external/sdk/serve.go`:

```go
type serveConfig struct {
    // ... existing fields ...
    buildVersion string
}

func WithBuildVersion(v string) ServeOption {
    return func(c *serveConfig) { c.buildVersion = v }
}
```

Add new file `plugin/external/sdk/buildversion.go`:

```go
// ResolveBuildVersion returns the operator-visible build-version string.
// declared non-empty + not a dev sentinel → returned as-is.
// Otherwise consults runtime/debug.ReadBuildInfo() and returns
//   "(devel) [@ shortsha[.dirty]]"
// when VCS info is available, else "(devel)".
//
// Intended call sites:
//   sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
//       BuildVersion: sdk.ResolveBuildVersion(internal.Version),
//   })
//   sdk.Serve(p, sdk.WithManifestProvider(m), sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))
func ResolveBuildVersion(declared string) string { ... }
```

Dev sentinels: `""`, `"dev"`, `"(devel)"`, `"v0.0.0-dev"`. If declared matches any, fall through to BuildInfo.

`iacPluginServiceBridge.GetManifest` (current `plugin/external/sdk/iacserver.go:300`) augmentation:

```go
out := &pb.Manifest{}
if b.diskManifest != nil { /* existing copy */ }
if b.buildVersion != "" {
    out.Version = b.buildVersion  // augment / override
}
return out, nil
```

`Serve` (non-IaC) bridge similarly: where `GetManifest` returns the manifest, prefer `c.buildVersion` over disk Version when non-empty.

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

1. `<dir>/plugin.json` exists, parses, passes `PluginManifest.Validate()`. Sentinel `v0.0.0-dev` allowed; emits "dev sentinel" info note.
2. `capabilities` populated (non-empty).
3. `minEngineVersion` populated (parses as semver constraint).
4. `.goreleaser.yaml` or `.goreleaser.yml` at repo root contains a line matching regex `-X .*\.Version=` (any package path).
5. Any `cmd/**/main.go` contains a call to `sdk.ResolveBuildVersion(` OR `sdk.WithBuildVersion(`.

Additional checks (`--for-publish`):

6. Tag from `--tag <vX.Y.Z>` flag (if provided) OR from `$GITHUB_REF_NAME` env (if set) OR from `git describe --tags --exact-match HEAD` matches strict-release-semver regex: `^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$`.
7. Committed plugin.json's `.version` is allowed to disagree with `--tag` (dev sentinel is the documented norm).

Exit non-zero on any failure with operator-friendly error referencing `docs/PLUGIN_RELEASE_GATES.md`.

### 4. Tag-format gate in each plugin's `release.yml`

First steps of every plugin's release.yml:

```yaml
- uses: GoCodeAlone/setup-wfctl@v1
  with:
    version: v0.61.0  # SHA-pinned via setup-wfctl action's release; bump on workflow release
- name: Validate plugin contract for publish
  run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .
```

Malformed tag or incomplete contract → release halts before goreleaser runs. No bypass mechanism.

### 5. Registry-side semver gate (defense in depth)

`workflow-registry/scripts/sync-versions.sh` adds the same tag regex check after `latest_tag` is set:

```bash
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver"
  continue
fi
```

Catches plugins that bypass release.yml (self-hosted runner, manual tarball, force-push). Same regex source as `wfctl plugin validate-contract --for-publish`'s rule 6.

### 6. Migration ordering

- **Layer 1 (workflow repo, single PR)**: SDK changes (§2) + `wfctl plugin validate-contract` subcommand (§3) + `docs/PLUGIN_RELEASE_GATES.md`. Tag workflow `v0.61.0`. Update `setup-wfctl` action's version (or rely on `latest`).
- **Layer 2 (workflow-registry repo, single PR)**: tag-string semver gate in `sync-versions.sh` (§5). Can ship in parallel with Layer 1.
- **Layer 3 (per-plugin PRs, parallel)**: in each plugin repo with a release pipeline today:
  1. `git rm .github/workflows/sync-plugin-version.yml`
  2. Add tag-format gate step to `.github/workflows/release.yml` per §4
  3. Update plugin main.go (or equivalent) to pass `sdk.ResolveBuildVersion(internal.Version)` to `IaCServeOptions.BuildVersion` or `WithBuildVersion`
  4. Set `plugin.json.version` to `"v0.0.0-dev"` (sentinel)
  5. Verify `.goreleaser.yaml` has `-X .*\.Version=` (most do; verify per repo)
  6. Local: `wfctl plugin validate-contract .` must pass before opening PR
  7. Open PR, CI must pass, admin-merge

Each Layer 3 PR is independent and can run in parallel via per-repo worktree-isolated agents.

### 7. Gap-repo handling (deferred)

Repos lacking a release pipeline get one filed issue each: "Establish release pipeline (workflow#758 prerequisite)." Not in Layer 3 scope.

## Assumptions

A1. `goreleaser`'s `before:` hook writes the correct version into `.release/plugin.json` for every plugin repo with a release.yml. Verified for DO plugin; per-repo verification step in each Layer 3 PR.
A2. `setup-wfctl` GitHub Action exists and pins to a wfctl version. Verified by workspace memory.
A3. `PluginManifest.Validate()` accepts `v0.0.0-dev` as valid pre-release semver. Verified by reading `plugin/manifest.go:308-355` (`ParseSemver` accepts `0.0.0` + arbitrary `-prerelease` segment).
A4. `wfctl plugin install --local` reads committed plugin.json; reports the sentinel. This is the intended dev-install behavior.
A5. `workflow-registry/scripts/sync-versions.sh` already derives the registry-visible version from upstream git tag. Verified at line 125, 132, 169.
A6. Layer 3 scope ≈ 15 plugin repos with release pipelines today (drops the ~8 gap-repos identified in plan-cycle-1 audit).
A7. The auditor agent in Layer 3 verifies per-repo: release.yml exists, .goreleaser.{yaml,yml} exists, cmd/**/main.go exists, branch-protection allows admin-merge. If any check fails, the agent files a "gap-repo" issue and skips the migration for that repo.

## Self-challenge — top 3 doubts

D1. **Sentinel `v0.0.0-dev` fragile to tooling that compares versions numerically.** Mitigation: documented in PLUGIN_RELEASE_GATES.md; registry's sync-versions.sh MISMATCH warning intentionally lights up to remind maintainers (informational, not blocking).
D2. **Losing git-history audit of version progression.** Yes, but git tag log is the authoritative version history; the committed plugin.json changing was redundant ceremony.
D3. **Some non-registry consumer might read committed plugin.json.version.** Audit confirms no such consumer exists today (wfctl validators read from installed-manifest or registry-manifest, both correctly derived from tag). Safe to delete.

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
