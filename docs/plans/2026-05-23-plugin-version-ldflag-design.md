# Plugin version sync hardening + ldflag contract + publish-tag semver gate — design

Issue: GoCodeAlone/workflow#758
Date: 2026-05-23 (cycle 3 — restored ldflag contract piece per cycle-2 NC1/NC2)
Mode: design-only handoff (autonomous brainstorm; user is away)

## Problem

Today every plugin repo carries a `plugin.json.version` field on `main`. After each release tag fires, `sync-plugin-version.yml` opens a PR to bump that field AND rewrite every `downloads[].url` to point at the new tag. Those PRs:

- Pile up if not actively merged (13 stale PRs found on `workflow-plugin-digitalocean` 2026-05-23 — closed in a sweep).
- Are necessary — `workflow-registry/scripts/sync-versions.sh:122` reads `.version` AND asserts `downloads[].url` contains `/releases/download/v${version}/`. The committed file IS the source of truth for the registry sync. Cannot be eliminated.
- Add review surface for purely mechanical bot churn.

Additionally, the user wants protection against non-semver versions getting into the registry. Branch / test / `-dirty` builds should install locally but be rejected from publish.

## What the adversarial cycle taught us

Cycle 1 proposed dropping `version` from committed plugin.json and surfacing it via ldflag-injected runtime value. That design had three critical defects: (a) the pre-spawn `LoadManifest+Validate` site at `plugin/external/manager.go:134-140` makes "defer to GetManifest" structurally impossible at the named code path; (b) `sync-plugin-version.yml` also rewrites `downloads[].url` (`sync-plugin-version.yml:47-52`) — not just version — and `workflow-registry/scripts/sync-versions.sh:122-138` depends on that URL being version-correct, so dropping the workflow without compensating produces unbuildable registry manifests; (c) `wfctl plugin validate --for-publish` has no source for the version string it would validate against once the disk plugin.json no longer carries it.

The cycle-1 reviewer surfaced four simpler alternatives. The combination of Options 1 + 4 (with a smaller Option 3 piece) reaches the user's actual goal — stop the PR churn AND gate non-semver publishes — without dropping the committed version field at all.

## Cycle-2 direction

**Keep `version` field in committed plugin.json.** The cycle-1 root cause is sync-mechanism, not field-presence. Fix the sync mechanism; leave the field alone.

Three composable pieces:

### 1. Replace PR-opening sync with direct-push-to-main

`sync-plugin-version.yml` today: tag fires → workflow checks out main → rewrites plugin.json → opens PR. The PR sits unmerged.

New: tag fires → workflow checks out main → rewrites plugin.json + downloads URLs (exactly today's logic, both substitutions preserved) → commits directly to `main` as `github-actions[bot]`, signed-off with the triggering tag in the commit message. No PR. No review queue.

Requires either (a) branch protection on `main` to allow `enforce_admins: false` + the bot to push (DO plugin's branch protection is `enforce_admins: false` already; admins can bypass — bot uses an admin PAT, OR the GITHUB_TOKEN gets a one-time bypass via a "linear history" / "allow bot pushes" rule), or (b) a dedicated "release-bot" branch protection exemption.

For repos where direct push is undesirable (some private plugins may have stricter rules), the workflow stays PR-opening but ALSO calls `gh pr merge --auto --squash --delete-branch` immediately after creation, with `--admin` if the token has permission. The PR still gets opened but is automerged on creation — no human review queue.

### 2. Tag-level semver gate in release workflow

`release.yml` today: triggered by tag push, runs goreleaser. No tag-format validation. A malformed tag (`v1.2`, `v1.2.3-dirty`, `release-2026-05`, etc.) just builds and publishes a malformed release.

Add a first step in `release.yml`:

```yaml
- name: Validate tag is strict semver
  run: |
    TAG="${{ github.ref_name }}"
    # Allow both -rc1 (no dot, k8s/Go convention) and -rc.1 (semver-canonical).
    if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$ ]]; then
      echo "::error::Tag $TAG is not a release-grade semver (allowed: vN.N.N or vN.N.N-(alpha|beta|rc)[.]N)"
      exit 1
    fi
```

The whitelist `alpha|beta|rc` is narrow on purpose (cycle 1 I4 finding): bare semver pre-release syntax (`-feat-foo.deadbeef`) satisfies semver but is exactly what we want to reject from publish. The optional `\.?` between `rc` and the digit accommodates both `v1.2.3-rc1` and `v1.2.3-rc.1` (cycle-2 NI1). If teams need a different pre-release vocabulary, the regex is the place to change it (single line).

**Concurrency safety** (cycle-2 NI2): both this `release.yml` and the `sync-plugin-version.yml` workflow declare:

```yaml
concurrency:
  group: plugin-version-sync-${{ github.repository }}
  cancel-in-progress: false
```

Two tags fired in quick succession queue serially; the second cannot overwrite an in-flight first. The direct-push variant additionally does `git fetch origin main && git pull --ff-only origin main` before its commit so a concurrent push is detected as a non-fast-forward and fails loudly rather than racing.

Local / branch / test builds via `goreleaser --snapshot` or plain `go build` (which produce `(devel)` / SNAPSHOT tags) are NOT triggered by tag push, so they skip the gate entirely. They install via `wfctl plugin install --local <dir>` which doesn't go through release.yml. That's the answer to "people should be able to generate a plugin from a custom/test branch."

### 3. Independent semver gate in `workflow-registry` ingest — gates the release tag, not the manifest field

Defense in depth. `workflow-registry/scripts/sync-versions.sh:125` already calls `gh release view --json tagName` to discover `$latest_tag`. The gate runs against the **tag string** (same source release.yml's gate uses), eliminating cycle-2 NI3's gate-string asymmetry:

```bash
# Validate the upstream release tag, not the manifest's .version field.
# Same regex as plugin release.yml so both gates fail identically.
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver"
  continue
fi
```

This catches: (a) plugins that bypass release.yml (self-hosted runner, manual tarball upload), and (b) plugins where the manifest `.version` field has drifted from the tag (older sync-PR mechanism failure, manual edit). Both surfaces are inspected — JSON `.version` continues to be validated by `downloads_match_version` for URL correctness; the tag is independently validated for publishability.

### 4. Make ldflag-injected runtime version a plugin contract

Cycle-2 NC1 surfaced that the user's verbatim ask was conditional: *"Removing version tag from json is fine, **but then we need to ensure ldflags version tag as a plugin contract requirement**."* The cycle-2 pivot kept the JSON field (good) but declined the contract requirement (NC1 drift). This piece restores it as a small additive SDK change that does NOT require dropping the JSON field.

Add to `sdk` package:

```go
// IaCServeOptions adds:
//   BuildVersion string   // runtime version, typically internal.Version (ldflag-injected)

// ResolveBuildVersion returns the operator-visible build-version string.
// When declared is non-empty and not a known dev sentinel ("", "dev",
// "(devel)"), returns declared as-is. Otherwise consults
// runtime/debug.ReadBuildInfo() and returns a string like
//   "(devel) [VCS-branch @ shortsha]"
// when VCS info is available, else "(devel)".
//
// This is the supported contract surface for plugin authors to plumb their
// goreleaser-injected version into GetManifest in a way that also degrades
// gracefully for local/test builds — addressing the user's stated branch-
// build-test-nature requirement (workflow#758 cycle-2 NC2).
func ResolveBuildVersion(declared string) string { ... }
```

Plugin authors then write:

```go
// cmd/plugin/main.go
sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
    BuildVersion: sdk.ResolveBuildVersion(internal.Version),
})
```

`internal.Version` already exists in every plugin (set by `-X internal.Version=...` ldflag). For tagged release builds, the resolved string is the canonical semver (e.g. `v1.2.3`). For test/branch builds, it's `(devel) [feat/foo @ abc1234]` — operator-visible, branch-nature reflected, never accidentally publishable.

Engine `iacPluginServiceBridge.GetManifest` (currently at `plugin/external/sdk/iacserver.go:300-312`) augments its returned Manifest.Version: if `BuildVersion` is non-empty, use it; else fall back to `diskManifest.Version` (existing behavior). The engine continues to log the disk Version at load time AND the runtime BuildVersion when they differ, so any drift is visible without being fatal.

**Contract enforcement** (not engine-side; plugin-author-side via lint):
- `docs/PLUGIN_RELEASE_GATES.md` documents the convention.
- Add `scripts/check-plugin-contract.sh` to the workflow repo. The script greps a plugin repo's `.goreleaser.yaml` for `-X .*Version=` and its `cmd/plugin/main.go` (or equivalent) for `sdk.ResolveBuildVersion(`. Each plugin's `release.yml` runs this script as a first step.
- Verifier failure exits non-zero with a clear "missing ldflag contract" error and a link to the convention doc. The contract is enforced at release time, not engine load time — a stricter check would block legacy plugins.

## Migration plan

Each plugin repo is independent. The audit (cycle-2 NI4 — user explicitly asked for it) is **in scope** below.

1. **workflow (PR 1, this repo):** add `sdk.ResolveBuildVersion` + `IaCServeOptions.BuildVersion` (§4); engine-side `iacPluginServiceBridge.GetManifest` prefers BuildVersion when set, logs disk-vs-runtime divergence. Add `scripts/check-plugin-contract.sh`. Document in new `docs/PLUGIN_RELEASE_GATES.md`. No removal of existing fields. Tag workflow v0.61.0.
2. **workflow-registry (PR 2):** add the tag-string regex gate in `scripts/sync-versions.sh` (§3). Reject malformed versions with clear errors.
3. **Per-plugin migration PR (N repos):** in each plugin repo:
   - Replace `sync-plugin-version.yml`'s `gh pr create` with direct-push-to-main (or auto-merge variant if branch protection forbids direct push). Add `concurrency:` group.
   - Add tag-format gate step to `release.yml`. Add `concurrency:` group. Add `scripts/check-plugin-contract.sh` invocation as the first build step.
   - Update `cmd/plugin/main.go` (or equivalent) to call `sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{BuildVersion: sdk.ResolveBuildVersion(internal.Version)})`.
   - Verify `.goreleaser.yaml` has `-X internal.Version=...` (most do; verify per repo).
   - Bump `minEngineVersion` to `0.61.0` so older engines that don't support BuildVersion-aware GetManifest fall back to disk Version gracefully (which is still present and correct).
   - Tag next minor for the plugin.

Each plugin PR is ~30 lines across 3 files. Individually reviewable + mergeable.

**Plugin audit inventory** (per workspace memory + DO plugin's actual layout):

| Repo | Source | sync-plugin-version.yml? | ldflag wiring? | Variant |
|---|---|---|---|---|
| workflow-plugin-admin | private | Y | needs verify | auto-merge |
| workflow-plugin-agent | public | Y | needs verify | direct-push |
| workflow-plugin-auth | public | Y | needs verify | direct-push |
| workflow-plugin-authz | public | Y | needs verify | direct-push |
| workflow-plugin-authz-ui | private | Y | needs verify | auto-merge |
| workflow-plugin-aws | public | Y | needs verify | direct-push |
| workflow-plugin-azure | public | Y | needs verify | direct-push |
| workflow-plugin-bento | private | Y | needs verify | auto-merge |
| workflow-plugin-ci-generator | public | Y | needs verify | direct-push |
| workflow-plugin-cloud-ui | private | Y | needs verify | auto-merge |
| workflow-plugin-cms | public | Y | needs verify | direct-push |
| workflow-plugin-compute | public | Y | needs verify | direct-push |
| workflow-plugin-data-protection | private | Y | needs verify | auto-merge |
| workflow-plugin-digitalocean | public | Y | YES (verified) | direct-push |
| workflow-plugin-edge-compute | public | Y | needs verify | direct-push |
| workflow-plugin-edge-risk | public (scenarios) | likely N | likely N | (excluded — contract-only) |
| workflow-plugin-gcp | public | Y | needs verify | direct-push |
| workflow-plugin-github | public | Y | needs verify | direct-push |
| workflow-plugin-payments | public | Y | needs verify | direct-push |
| workflow-plugin-sandbox | private | Y | needs verify | auto-merge |
| workflow-plugin-security | private | Y | needs verify | auto-merge |
| workflow-plugin-supply-chain | private | Y | needs verify | auto-merge |
| workflow-plugin-tofu | public | Y | needs verify | direct-push |
| workflow-plugin-waf | private | Y | needs verify | auto-merge |

The first per-repo migration PR includes the verification: does `sync-plugin-version.yml` exist; does goreleaser have the ldflag; does main.go currently call `sdk.ServeIaCPlugin` (or another `Serve` variant). Plan task per repo enumerates these gates.

The migration order is workflow → registry → plugins. Plugin migrations can run in parallel (24 PRs, each ~30 lines).

## Verified API surface

- `workflow-registry/scripts/sync-versions.sh:122` — reads `.version` from manifest. Stays as-is.
- `workflow-registry/scripts/sync-versions.sh:138` — `downloads_match_version` requires `downloads[].url` contains `/releases/download/v${version}/`. Stays as-is.
- `workflow-plugin-digitalocean/.github/workflows/sync-plugin-version.yml:47-52` — rewrites `downloads[].url` via Python sed; this LOGIC is preserved in the direct-push variant, only the `gh pr create` is replaced by `git push origin main`.
- `workflow-plugin-digitalocean/.github/workflows/release.yml` — tag-fired today; receives new validate-tag step at the top.
- `workflow-plugin-digitalocean/.goreleaser.yaml:7-9` — before-hook stays as-is.

## Assumptions

A1. Every plugin repo has branch protection set such that an admin PAT can push to `main` directly (enforce_admins: false). **Verified for DO plugin via `gh api repos/.../branches/main/protection` in chat 2026-05-23. Assumed similar for others; per-repo verification step in each plugin migration PR.**
A2. Auto-merge `--admin --squash --delete-branch` is available on GitHub for repos with branch protection. **Verified by prior session work (multiple admin-merges in 2026-05).**
A3. The user's "branch / test build" use case is locally-installed via `wfctl plugin install --local <dir>` and never goes through release.yml. **Plausible from existing wfctl support; per-plugin migration verifies no other publish path exists.**
A4. The tag-format gate regex `^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)(\.[0-9]+)?)?$` matches the user's intent (no `-feat-foo`, no `-dirty`, no `+meta`). **User said "valid semver" and "reject branch versions"; explicit pre-release whitelist enforces the latter.**
A5. workflow-registry's sync-versions.sh is the canonical publish ingest. If another path exists (webhook, manual upload) it needs the same gate. **Per registry PR audit; flag in plan.**
A6. The direct-push-to-main mechanism doesn't break CI in any plugin repo (e.g., a "PRs only" branch protection rule). **Per-repo verification step.**

## Self-challenge — top 3 doubts

D1. **Is direct-push-to-main actually safer than auto-merging a sync PR?** Direct-push: bot has commit-to-main perm forever. Auto-merge: bot opens PR → merges immediately, audit trail in PR history, perm scoped to "create + merge own PRs." The latter is the better default for security-conscious repos. Cycle-2 design supports BOTH (per-repo choice) — direct-push for fast-moving plugin repos, auto-merge for privates.

D2. **What about the 13 stale PRs that piled up on DO before the sweep?** Those PRs were closed manually 2026-05-23. Going forward, the new mechanism prevents new accumulation. There is no general-purpose "close stale sync PRs" automation in this design — that's a one-time cleanup that already happened.

D3. **Does the tag-format regex's whitelist (`alpha|beta|rc`) hard-code a release-vocabulary choice?** Yes. Some plugin authors might want `dev`, `preview`, `experimental`, etc. The regex is the place to change it. Documenting the supported list in `docs/PLUGIN_RELEASE_GATES.md` makes the choice visible.

## Rollback

- §1 (direct-push or auto-merge in sync-plugin-version.yml): per-repo, revert the workflow file change. Old PR-opening behavior returns; sync PRs accumulate again. Cheap revert; no state.
- §2 (tag-format gate in release.yml): per-repo, remove the step. Malformed tags can publish again. Cheap revert; no state.
- §3 (registry-side gate): single revert in `workflow-registry/scripts/sync-versions.sh`. Cheap; no state.

Cross-repo rollback risk: very low. None of the changes break the engine/SDK contract; they only affect the release pipeline. Worst case is a single bad release that the existing tag-delete + re-tag flow already handles.

## Out of scope

- Dropping `plugin.json.version` field entirely (deferred; needs solving cycle-1 C1/C2/C3 in a separate design).
- Replacing goreleaser.
- Cleaning up the existing 13 stale sync PRs (already done manually 2026-05-23).
- Changing the engine's pre-spawn `LoadManifest+Validate` semantics (cycle-1 C1 untouched).

## Adversarial cycle 2 — addressed

- NC1 (ldflag not a contract requirement; user-intent drift): **addressed** — §4 adds `sdk.ResolveBuildVersion` + `IaCServeOptions.BuildVersion` SDK contract surface + `scripts/check-plugin-contract.sh` lint enforced in each plugin's release.yml.
- NC2 (branch/test build version-string surface): **addressed** — §4's `ResolveBuildVersion` consults `runtime/debug.ReadBuildInfo()` for local/test builds; reports `(devel) [feat/foo @ shortsha]` so operator + engine + log all show branch nature.
- NI1 (regex too narrow for `rc1`/`alpha1`): **addressed** — §2 regex updated to `(alpha|beta|rc)\.?[0-9]+` accepting both `rc1` (k8s/Go convention) and `rc.1` (semver-canonical).
- NI2 (concurrent two-tag race): **addressed** — §2 mandates `concurrency:` group on both release.yml + sync-plugin-version.yml; direct-push variant does `git fetch origin main && git pull --ff-only` before commit so concurrent pushes fail loudly.
- NI3 (gate-string asymmetry between release.yml and registry): **addressed** — §3 registry gate now validates the **tag string** (same source as release.yml's gate), not the manifest's `.version` field; eliminates divergence.
- NI4 (per-plugin audit deferred to out-of-scope despite user request): **addressed** — Migration plan §3 now contains a 24-row plugin audit table; per-repo migration PR includes verification steps; private vs public + variant choice (direct-push vs auto-merge) enumerated.

## Decision points reserved for user return

- **Approve overall design + execute pipeline** — design → plan → adversarial-design-review (plan) → alignment-check → scope-lock → dispatch workflow PR 1 only, pause for review before registry PR 2 and per-plugin PRs.
- **Approve §1+§2 only** (defer §3 registry gate); ship sync hardening + tag-format gate first, registry-side defense in depth as a follow-up.
- **Re-scope** (e.g., go back and revive the cycle-1 ldflag direction; the cycle-1 review explicitly notes Option 3 — `runtime/debug.ReadBuildInfo()` — as a cleaner alternative that bypasses both the disk-version AND ldflag-coordination problems).
