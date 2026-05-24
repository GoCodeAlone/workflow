# wfctl plugin registry-sync + Layer 3b/c sweep + template-repo modernization — design

Issue: GoCodeAlone/workflow#762
Date: 2026-05-23
Mode: autonomous execution authorized

## Problem

Three coupled gaps surfaced after workflow#758 pilot landed:

1. **`workflow-registry/scripts/sync-versions.sh` is 228 lines of bash** + jq + `gh` CLI. workflow#758 Layer 2 added a strict-semver tag gate to it, but that regex is now duplicated with `wfctl plugin validate-contract --for-publish`. Two implementations of the same rule → drift. No test harness in workflow-registry. Cannot reach the deferred binary-vs-file capability gate (cycle 4-A1 I3) because it requires plugin-binary spawn.
2. **Layer 3b sweep blocked by ldflag gap.** Audit (5 of 56 sampled) shows NONE have `-X .*\.Version=` ldflag in goreleaser AND none declare `var Version = "dev"`. Pilot 5 (DO/AWS/GCP/Azure/github) were the exception. Per-repo migration is heavier than the canonical template — needs ldflag + Version-var added BEFORE the canonical sweep can wire `sdk.ResolveBuildVersion`.
3. **Template repos are treated as plugins.** `workflow-plugin-template` is in `workflow-registry/plugins/template/manifest.json` as `type: external` — operators could `wfctl plugin install workflow-plugin-template` and get an empty scaffold. Name `template` is wasted on the scaffold. Template content predates workflow#758, so any repo created from it starts non-compliant. Same problem in private template.

## Proposed design

Single issue, four composable layers. Sequencing: (a) → (a') → (d) → (c) → (b).

### Layer (a): `wfctl plugin registry-sync` subcommand

New subcommand under existing `wfctl plugin` family (cycle 4-P1 naming rationale; avoids collision with OCI `wfctl registry`).

Surface:

```
wfctl plugin registry-sync [--fix] [--plugin <name>] [--verify-capabilities] [--registry-dir <path>]
```

- Default dry-run; `--fix` writes back.
- `--plugin <name>` filters to single plugin manifest.
- `--registry-dir` defaults to `.` (the cwd, typically a workflow-registry checkout in CI).
- `--verify-capabilities` (optional, registry-side only): downloads upstream release tarball; extracts plugin binary; spawns via `plugin/external/manager.go` machinery; calls `GetContractRegistry` RPC; diffs vs committed `plugin.json.capabilities`; with `--fix` auto-rewrites.

Implementation lives in `cmd/wfctl/plugin_registry_sync.go` + `_test.go`. Shared strict-semver regex extracted into `cmd/wfctl/plugin_release_grade_semver.go` (constant sourced by `validate-contract --for-publish` AND `registry-sync`). Logic ports `sync-versions.sh` 1:1 with fixture-backed parity tests.

`workflow-registry/.github/workflows/sync-registry-manifests.yml` swaps `bash scripts/sync-versions.sh --fix` for `wfctl plugin registry-sync --fix`. Bash script kept alongside for **one parity-verification cycle**, then deleted in a follow-up PR.

### Layer (a'): workflow-registry switch + parity cycle

PR in `workflow-registry`:

1. Add `wfctl plugin registry-sync --fix` to `sync-registry-manifests.yml` as a NEW step running AFTER the existing `bash scripts/sync-versions.sh --fix`.
2. In dry-run mode for both, log the diff between bash and Go outputs into the workflow artifact.
3. After one weekly cron cycle confirms zero output diff, ship the **followup PR** that deletes `sync-versions.sh` + removes the bash step.

This belts-and-suspenders pattern addresses self-challenge doubt D2 (bash → Go translation parity risk).

### Layer (d): template-repo modernization

**Renames:**
- `workflow-plugin-template` → `scaffold-workflow-plugin` (public; prefix-first naming so it doesn't look like a plugin family member; per user)
- `workflow-plugin-template-private` → `scaffold-workflow-plugin-private` (private; suffix `-private` keeps both scaffolds alphabetically adjacent in org browse vs `private-scaffold-workflow-plugin`)

**Per-repo steps (one PR per scaffold):**

1. `gh repo rename` (GitHub keeps old-URL redirect for 1 year+).
2. GitHub repo settings: enable `template_repository: true` (makes the repo selectable under "Use this template" dropdown when creating a new repo).
3. Content updates (single PR per scaffold):
   - `plugin.json`: `name`: `scaffold-workflow-plugin` (the scaffold itself); `version`: `"0.0.0"`; `minEngineVersion`: `0.61.0`; capabilities populated with placeholder shape (`moduleTypes: ["TEMPLATE.module"]`, `stepTypes: ["TEMPLATE.step"]`, `triggerTypes: []`, `iacProvider: {resourceTypes: ["TEMPLATE.resource"]}`) — shows the expected shape so instantiators see what to fill.
   - `cmd/workflow-plugin-TEMPLATE/main.go` → rename to `cmd/scaffold-workflow-plugin/main.go`. The README explicitly instructs instantiators to rename this dir to `cmd/workflow-plugin-<their-name>/` immediately after instantiation.
   - main.go uses `sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{BuildVersion: sdk.ResolveBuildVersion(internal.Version)})` — covers BOTH module/step (`IaCServeOptions.Modules + Steps`) AND IaC dispatch in a single entrypoint. Plugins that don't need IaC pass empty maps; plugins that don't need modules/steps leave them unset. One canonical entrypoint per user direction.
   - `internal/version.go`: `var Version = "dev"`.
   - `.goreleaser.yaml`: `-X github.com/GoCodeAlone/scaffold-workflow-plugin/internal.Version={{.Version}}` ldflag.
   - `.github/workflows/release.yml`: setup-wfctl@v1 + pre-build + post-build `wfctl plugin validate-contract` gates.
   - **No** `sync-plugin-version.yml` (defunct workflow not shipped in new scaffolds).
   - `README.md`: documents the post-instantiation ritual:
     - Rename `cmd/scaffold-workflow-plugin/` → `cmd/workflow-plugin-<your-name>/`
     - Edit `plugin.json` (name, description, capabilities, minEngineVersion)
     - `go mod edit -module github.com/<org>/workflow-plugin-<your-name>`; `find . -name '*.go' -exec sed -i.bak 's|scaffold-workflow-plugin|workflow-plugin-<your-name>|g' {} \;`
     - First `git commit` + first tag
4. Delete `workflow-registry/plugins/template/` (scaffold is not an installable plugin); commit in workflow-registry PR.
5. Add registry-side defense: `wfctl plugin registry-sync` emits a `WARN` if it encounters a registered manifest whose `repository` field points at `*-scaffold-*` or contains `scaffold-workflow-plugin` (catches accidental re-registration).
6. Update workflow#760 sweep list: drop `workflow-plugin-template` + `workflow-plugin-template-private`. 56 → 54.

### Layer (c): ldflag + Version var bootstrap (54 repos)

One PR per repo. Mechanical 3-file edit:

1. Create `internal/version.go` with `var Version = "dev"` (or add to existing internal package).
2. Edit `.goreleaser.yaml`: add `-X github.com/<full-import-path>/internal.Version={{.Version}}` to `builds[].ldflags`. If `ldflags` block absent, add it.
3. Verify `go build ./cmd/...` clean (no behavior change; binary keeps shipping "dev" default until Layer (b) lands).

Per-repo gating: skip repos with no release in the last 90 days (self-challenge D3) — file separate "stale-repo evaluation" issue for those. Default: include the repo.

### Layer (b): canonical sweep (54 repos, parallel)

Same template as workflow#758 pilot (DO PR #165). Mechanical 6-file PR per repo:

1. `git rm .github/workflows/sync-plugin-version.yml`
2. Edit main.go to call `sdk.ResolveBuildVersion(<plugin's Version var>)` + wire via `IaCServeOptions.BuildVersion` (IaC) or `sdk.WithBuildVersion` (non-IaC `sdk.Serve`).
3. `plugin.json.version` → `"0.0.0"`; `minEngineVersion` → `0.61.0`.
4. For repos with null/missing `capabilities`: run `wfctl plugin registry-sync --verify-capabilities --fix` against a local workflow-registry checkout to auto-populate from the binary's `GetContractRegistry` response. This is the deferred I3 fix from #758.
5. release.yml: add setup-wfctl + pre+post wfctl plugin validate-contract gates.
6. Bump workflow pin to v0.61.0 (or current latest).

Fans out via parallel sub-agents post-Layer (c).

## Assumptions

A1. `wfctl` can be installed in workflow-registry's GitHub Action runner via `setup-wfctl@v1` (verified 2026-05-23; pilot Layer 3 used this).
A2. `plugin/external/manager.go`'s plugin-spawn machinery is reusable from a wfctl subcommand. The existing `wfctl plugin install` path exercises some of this; the new `--verify-capabilities` flow needs to spawn the binary in a host-managed lifecycle. **Per-plan verify** that the spawn API is exported and usable from outside the engine boot path.
A3. Plugin binaries' `GetContractRegistry` RPC reliably returns the same capabilities the plugin author intends to expose (no per-plugin runtime conditionals affecting capability enumeration). True for pilot 5; **Layer (b) verifies per-plugin** during the migration.
A4. Layer (c) ldflag addition is additive: binaries built without `-X` keep the `"dev"` default; binaries built with `-X` pick up the injected tag. Verified by Go's standard `-X` semantics.
A5. The 56 remaining repos all ship a buildable plugin binary in their release tarball that wfctl can spawn (i.e., goreleaser archive contains the binary alongside plugin.json). True for all goreleaser-managed plugins per audit.
A6. `gh repo rename` keeps GitHub URL redirects for old URL → new URL (verified per GitHub docs; redirects persist indefinitely unless a new repo claims the old name).
A7. GitHub's `template_repository: true` flag makes the repo selectable under the "Use this template" UI button; new repos created this way get a fresh git history seeded from the template's HEAD. Verified per GitHub feature docs.
A8. The single `sdk.ServeIaCPlugin` entrypoint (with `IaCServeOptions.{Modules, Steps, TypedModules, TypedSteps}` maps) supports BOTH module/step contracts AND IaC dispatch in one plugin — verified by reading workflow#758 cycle-3 SDK changes. Scaffold uses this single entrypoint.
A9. Layer (c) PR per repo doesn't break existing CI for repos that currently work without the ldflag (binary keeps building; "dev" default keeps shipping if the PR's release.yml isn't yet updated). True per A4.
A10. workflow-registry's `sync-registry-manifests.yml` runs on a schedule that allows a 1-week parity cycle between bash + Go (per Layer a' D2 mitigation).

## Self-challenge — top 3 doubts

D1. **Capability-verify chicken-and-egg.** Layer (b) PR lands → release.yml's pre-build gate runs `wfctl plugin validate-contract --for-publish` — but `--verify-capabilities` wants a built binary, which doesn't exist pre-tag. Mitigation: `--verify-capabilities` is a registry-sync flag (post-release), NOT a pre-build validate-contract flag. The pre-build validate-contract continues to do file-only checks; registry-sync (which runs after release publish) does the binary spawn. Documented in §3.

D2. **bash → Go translation parity risk.** 228 lines of accumulated bash edge cases (URL normalization, `normalize_repo`, `downloads_match_version`, mismatch warnings, capability nested-vs-flat shape handling). Mitigation: Layer (a') runs bash + Go in parallel for one cron cycle and asserts zero diff before deleting bash. Bash kept in repo until parity confirmed.

D3. **Layer (c) bootstrap may be wasted work for stale repos.** If some of the 56 repos are deprecated/abandoned, adding ldflag is churn. Mitigation: per-repo gate on "last release within 90 days OR explicit maintainer ack." Stale repos get skipped + filed for archival evaluation (separate issue). Default = include.

## Rollback

- Layer (a): single revert of wfctl subcommand addition. No state, no contract change.
- Layer (a'): revert the workflow YAML step swap; bash continues being authoritative (which it currently is). Bash never deleted until parity verified.
- Layer (d): GitHub repo rename is reversible via `gh repo rename` back to old name; URL redirects work in reverse. Template-repository flag is a settings toggle. Content reverts are git revert.
- Layer (c): per-repo git revert restores pre-ldflag goreleaser + drops the new `internal/version.go` file. Binary still builds.
- Layer (b): per-repo git revert restores sync workflow + reverts main.go + restores committed version. Same as workflow#758 rollback story.

No state migrations, no breaking SDK contract changes (all additive), no cross-repo coordination required for rollback.

## Out of scope (cross-linked)

- **SemVer 2.0.0 prerelease support** — separate design; touches `ParseSemver` + `wfctl install` + registry. Tracked via workflow#762 reference; not in this design.
- **Gap-repos** (~8 plugin repos without release pipelines: agent, cms, compute, cloud-ui, data-protection, edge-compute, sandbox, waf) — separate per-repo "establish release pipeline" issues.
- **OCI catalog (`wfctl registry push/pull/login`)** — unrelated subcommand family; not touched.
- **Stale-repo archival decisions** for plugins with no release in 90+ days — filed as separate issues during Layer (c) per-repo audit.

## Migration ordering

1. **PR 1 (workflow)**: Layer (a) — `wfctl plugin registry-sync` subcommand + tests + shared regex extraction.
2. **PR 2 (workflow-registry)**: Layer (a') — add wfctl step alongside existing bash; parity-diff logging.
3. **PR 3+4 (scaffold-workflow-plugin + private)**: Layer (d) — rename + content modernization + `template_repository: true`.
4. **PR 5 (workflow-registry)**: delete `plugins/template/` entry.
5. **Wait 1 cron cycle** for Layer (a') parity verification.
6. **PR 6 (workflow-registry)**: delete `scripts/sync-versions.sh` + remove bash step from workflow YAML.
7. **PRs 7-N (54 repos)**: Layer (c) ldflag bootstrap — parallel sub-agent fan-out.
8. **PRs N+1-M (54 repos)**: Layer (b) canonical sweep — parallel sub-agent fan-out (after Layer c lands per-repo).
9. **PR final (workflow)**: retro doc.

Layer (a) blocks (a'). Layer (a') blocks the bash-delete. Layer (d) is independent of (a)/(a') and can run in parallel. Layer (c) blocks (b) per-repo (PR-pairing per repo: c first, then b).

## Adversarial cycles expected

- Design cycle 1: probably fail on bash→Go parity surface (every edge case in `sync-versions.sh` becomes a fixture); design clarifications around plugin-spawn API usability from wfctl context (A2 verification); Layer (d)'s post-instantiation rename ritual completeness.
- Design cycle 2: revisions; likely pass.
- Plan cycle 1: granularity + per-repo skip-gate operationalization (D3); test fixture enumeration.
