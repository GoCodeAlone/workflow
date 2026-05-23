# Plugin version from build-time ldflags — design

Issue: GoCodeAlone/workflow#758
Date: 2026-05-23
Mode: design-only handoff (autonomous brainstorm; user is away)

## Problem

Today every plugin repo carries a duplicated `plugin.json.version` field on the committed `main` branch. After each release tag fires, `sync-plugin-version.yml` opens a PR to bump that field. Those PRs:

- Pile up if not actively merged (13 stale PRs found on `workflow-plugin-digitalocean` 2026-05-23).
- Are redundant — the actually-shipped `plugin.json` in each release tarball is already correct because `goreleaser.before:` rewrites `.release/plugin.json` with `{{ .Version }}` from the git tag.
- Add review surface for purely mechanical bot churn.

The whole class of drift is preventable by not committing the version field at all.

## Verified current state

- `workflow-plugin-digitalocean/cmd/plugin/main.go` calls `sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{})` — no `ManifestProvider`.
- `sdk.IaCServeOptions.ManifestProvider` (workflow/plugin/external/sdk/iacserver.go:326) is the only knob today; when nil, `GetManifest` RPC returns `Unimplemented` and engine falls back to disk `plugin.json`.
- `PluginManifest.Validate()` (workflow/plugin/manifest.go:194-206) **requires** `Version` non-empty + parseable as semver. Run by `EmbedManifest` at plugin process start when `MustEmbedManifest` is used; also run by the engine's `manager.go` on disk plugin.json load.
- `workflow-plugin-digitalocean/internal/provider.go:34` already has `var Version = "dev"` set via `-X internal.Version={{.Version}}` ldflag in `.goreleaser.yaml:25`. The ldflag wiring exists but is not surfaced to the engine.
- `.goreleaser.yaml:7-8` before-hook copies `plugin.json` → `.release/plugin.json` and rewrites the `version` field from `{{ .Version }}`. The shipped binary's adjacent disk plugin.json carries the correct version.

So today: ldflag-injected version exists in every plugin binary but is never read by the engine. Disk plugin.json carries the version, which the engine reads. Committed main's disk plugin.json drifts, hence the sync-PR mechanism.

## Direction (per user)

User stated direction verbatim (chat 2026-05-23):

> Removing version tag from json is fine, but then we need to ensure ldflags version tag as a plugin contract requirement. We'll also need workflow-registry (and/or wfctl?) to validate that only valid versions are published following a semver type of scheme. People should be able to generate a plugin from a custom/test branch and reflect the test nature in the version string (by reflecting branch name or something), but plugins with versions like that should be rejected by the registry. If we validate from wfctl, maybe it can have an additional for-publish flag or something that would validate the version string. Then we'll need to audit all public and private plugins to adhere them to this new approach.

## Proposed design

### 1. Plugin contract: build-time version surface

Add a new option to `sdk.IaCServeOptions`:

```go
type IaCServeOptions struct {
    // ... existing fields ...

    // BuildVersion is the plugin's release version, typically injected at
    // build time via `-ldflags "-X main.version=<tag>"` (or per-plugin
    // equivalent). When non-empty, takes precedence over any ManifestProvider
    // .Version field for GetManifest's Version response. Required for plugins
    // that omit the version field from their committed plugin.json.
    BuildVersion string
}
```

`sdk.ServeIaCPlugin` populates `iacPluginServiceBridge.runtimeVersion` from `opts.BuildVersion`. `GetManifest` returns:

1. `opts.BuildVersion` if set (the build-time-injected value); else
2. `b.diskManifest.Version` if `ManifestProvider` set + has version; else
3. Engine-side fallback to disk plugin.json (existing behavior).

Mirror the same option on `sdk.Serve` (for non-IaC plugins) via `WithBuildVersion(string)` to keep the surface symmetric.

### 2. Engine: accept missing `version` in disk plugin.json when binary surfaces it

`PluginManifest.Validate()` today fails outright on empty `Version`. Make it tolerant when a known sentinel (e.g., `""` literally) is present AND the plugin has surfaced a version via `GetManifest` RPC. Concrete change:

- `PluginManifest.Validate()` continues to require Version for disk-only loads (no behavior change to consumers that parse plugin.json from disk in tooling).
- `engine/manager.go` (load path): if disk plugin.json's `Version == ""`, defer validation until `GetManifest` returns the runtime version; reject only if both are missing.
- Pre-validate the runtime version returned by `GetManifest` against `ParseSemver` (the same check `Validate()` uses today).

Rejection at engine load when binary fails to surface a version preserves the contract: every running plugin still has a known, semver-parseable version.

### 3. wfctl: `plugin validate --for-publish` gate

`wfctl plugin validate` today reads the disk plugin.json and runs `PluginManifest.Validate()`. Extend:

- `wfctl plugin validate --file <plugin.json>` keeps current behavior for local/dev validation. If `version` field is empty, the validator treats it as "build-time-injected; OK for local install" rather than a fatal error.
- `wfctl plugin validate --file <plugin.json> --for-publish` opts into strict publish-time validation: requires `version` populated AND strictly matching `^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?$` (release semver, no dirty/branch suffixes like `-dirty`, `+commit`, or `feat-foo`).
- `--for-publish` is the gate the registry sync workflow + manual publish call before pushing.

### 4. workflow-registry: publish-time semver gate

`workflow-registry`'s ingest workflow (today blind-trusts whatever version it pulls from the plugin manifest) calls `wfctl plugin validate --for-publish` against the staged manifest before accepting the publish. Reject with a clear error if the version is non-strict semver. Branch builds (`v0.0.0-feat-foo.deadbeef`) install locally fine but never make it into the registry.

### 5. goreleaser before-hook: keep, but as the *source* of truth

The `before:` hook already writes the correct version into `.release/plugin.json`. That stays. The shipped tarball's plugin.json carries the version. After the engine change in §2, the committed `plugin.json` on main can have `"version": ""` (or omit the field entirely; both must be tolerated). The shipped one always has the real version because goreleaser fills it.

### 6. Drop `sync-plugin-version.yml` from every plugin repo

Once §1-§5 land in a plugin repo, that repo's `sync-plugin-version.yml` workflow is dead weight. Delete it in the same PR that drops the committed `version` field.

### 7. All-plugin migration

Audit + migrate (drop committed version, verify ldflag wiring) every plugin repo:

**Public plugins** (verified via memory):
- workflow-plugin-admin, agent, auth, authz, authz-ui, aws, azure, bento, ci-generator, cloud-ui, compute, cms, data-protection, digitalocean, edge-compute, edge-risk, gcp, github, payments, sandbox, security, supply-chain, tofu, waf

**Private plugins** (separate access required): same list as above plus any private-only repos.

Per repo: a 1-PR migration that drops the version field from plugin.json, deletes sync-plugin-version.yml, verifies the goreleaser ldflag wiring, and bumps to a new minor version on tag.

### 8. Engine-version pin order of operations

Engine change (§2) ships first as a new minor (e.g., workflow v0.61.0). Plugin migrations bump `minEngineVersion` to that version in their committed plugin.json. Plugins built against pre-v0.61 engines continue to require the committed `version` field — old engines load disk-only and don't speak the new `BuildVersion` contract. No flag-day; old plugins keep working on new engines, new plugins require new engine via `minEngineVersion`.

## Assumptions

A1. `goreleaser.before:` hook already writes the correct version into the shipped tarball's plugin.json for every plugin repo. **Verified for DO plugin; assumed for others. The migration PR per repo verifies.**
A2. Every plugin's `cmd/plugin/main.go` already has a Go var consumable by `-X` ldflags (DO plugin uses `internal.Version`). **Migration PR per repo verifies / wires if missing.**
A3. The `sdk.IaCServeOptions.BuildVersion` change is backward-compatible: existing plugins that don't set it keep current behavior. **Confirmed by reading iacserver.go — option is additive.**
A4. `wfctl plugin validate --for-publish` is a new flag, not a behavior change to the default `validate` invocation. **Confirmed; existing wfctl callers unaffected.**
A5. `workflow-registry`'s publish workflow runs `wfctl` (or has access to a wfctl binary) and can call `--for-publish`. **Needs verification in registry repo; if not, design a different gate (in-registry semver regex).**
A6. The "branch-tagged version" use case (e.g., `v0.0.0-feat-foo.<sha>`) is something users actually want — installable locally, registry-rejected. **User stated this requirement verbatim.**
A7. Existing plugins parsing `plugin.json.version` for tooling/display purposes (outside engine load) can tolerate an empty field for one release cycle while migration completes. **Needs audit; flag in plan.**

## Self-challenge — top 3 doubts

D1. **Is the engine-side fallback chain too clever?** Three layers: BuildVersion → ManifestProvider.Version → disk fallback. Future debugging when "version is wrong" gets murky. Mitigation: log which source provided the version at plugin load.

D2. **Does the `--for-publish` flag put the gate in the wrong place?** The registry should be authoritative; relying on wfctl-side validation means anyone with a fork of wfctl can bypass. Mitigation: registry runs the same check independently (defense in depth) — wfctl is the *operator-facing* error surface so the operator sees the rejection locally before pushing.

D3. **Will all-plugin migration take longer than the sync-PR churn it eliminates?** ~25 plugin repos × 1 PR each = 25 PRs to land. Mitigation: it's a one-time cost; the sync-PR pile is forever otherwise. Plus the migration PRs can be batched (file all, merge as CI greens).

## Rollback

- §1 (BuildVersion option) is additive on `IaCServeOptions`; revert is a single file change in workflow SDK.
- §2 (engine manager tolerance) is the riskier change — if revert is needed, engine reverts to requiring `version` on disk, plugins that already dropped it break. Rollback path: re-add `version` field to each migrated plugin.json (cheap; one line per repo) AND revert engine + bump engine minor.
- §3-§5 (wfctl flag, registry gate, goreleaser hook) are independently revertible.
- §7 migration PRs are individually revertible per plugin.

The cross-repo migration is the heaviest blast radius. Recommendation: ship engine §2 + SDK §1 first as workflow v0.61.0; live one release cycle with both behaviors supported; then begin plugin migrations one at a time.

## Out of scope

- Replacing goreleaser entirely.
- Changing how DEPLOYMENT_STRATEGIES-style metadata propagates from binary to manifest.
- Pinning specific minEngineVersion ranges in plugins beyond the migration bump.
- Webhook-based publish gate (the registry sync is workflow-dispatch today; keep that).
- Backporting the contract to engine versions < v0.61.

## Migration ordering

1. **PR 1 (workflow):** add `IaCServeOptions.BuildVersion` + `sdk.Serve` `WithBuildVersion` option. Engine `manager.go` tolerates empty disk version when GetManifest returns a parseable one. Adversarial review + tests. Tag workflow v0.61.0.
2. **PR 2 (wfctl, in workflow repo):** add `wfctl plugin validate --for-publish` flag. Tag workflow v0.61.1.
3. **PR 3 (workflow-registry):** publish workflow calls `wfctl plugin validate --for-publish`. Reject non-semver publishes.
4. **PR 4-N (each plugin repo):** drop committed `version` field; delete `sync-plugin-version.yml`; verify goreleaser ldflag wiring; bump `minEngineVersion` to v0.61.0; tag next minor.

Each plugin PR is independent of the others; can run in parallel.

## Decision points reserved for user return

- **Approve overall design + execute pipeline** (this design + plan + alignment-check + scope-lock; then dispatch PR 1 only, pause for review before PR 2-N).
- **Approve §1-§3 only** (engine + wfctl), defer §4-§7 (registry + migration) to a separate brainstorm.
- **Re-scope** (e.g., skip the `--for-publish` flag in favor of a registry-side regex; or batch all plugin migrations into a single sweep PR per repo).
