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

### Layer (a): `wfctl plugin registry-sync` subcommand — full registry-sync port

New subcommand under existing `wfctl plugin` family (avoids collision with OCI `wfctl registry`).

Surface:

```
wfctl plugin registry-sync [--fix] [--plugin <name>] [--verify-capabilities] [--registry-dir <path>]
wfctl plugin registry-sync core [--fix] [--workflow-repo <path>] [--registry-dir <path>]
wfctl plugin registry-sync readme [--check] [--registry-dir <path>]
```

Three sub-modes covering ALL three current scripts (cycle 1 C1: `sync-versions.sh` is one of three scripts the same CI step runs; porting only one regresses registry coverage and breaks parity-diff isolation):

1. **Default mode (ports `sync-versions.sh`):** walks `<registry-dir>/plugins/*/manifest.json`; for each:
   - Reads `repository`/`source`; derives `gh_repo` via `normalize_repo` equivalent.
   - `gh release view` for latest tag.
   - **Strict-semver gate** (shared regex constant — see below).
   - Compares against committed `manifest.version`; with `--fix` rewrites version + downloads URLs.
   - `gh api .../contents/plugin.json?ref=<tag>` to sync `capabilities + minEngineVersion + iacProvider`.
   - **NEW (--verify-capabilities)**: registry-side only, NOT per-PR. See "Capability verification flow" below.
2. **`core` mode (ports `sync-core-manifests.sh`):** runs against a workflow checkout (`--workflow-repo`); compiles + runs the inspect program; diffs against registry's `plugins/<core-plugin>/manifest.json`; with `--fix` rewrites.
3. **`readme` mode (ports `generate-readme.sh`):** regenerates README plugin/template indexes from registry source data; `--check` is dry-run.

Implementation files:
- `cmd/wfctl/plugin_registry_sync.go` — root subcommand dispatch + default-mode logic
- `cmd/wfctl/plugin_registry_sync_core.go` — `core` mode (inspect-program embed + workflow-repo build)
- `cmd/wfctl/plugin_registry_sync_readme.go` — `readme` mode (README mutate)
- `cmd/wfctl/plugin_registry_sync_test.go` — table-driven fixtures
- `cmd/wfctl/testdata/plugin_registry_sync/{good,stale-version,stale-caps,non-semver-tag,empty-assets,fetch-plugin-json-missing,prerelease-tag-vs-stable,...}/` — fixtures pinned against current bash behavior

Shared strict-semver regex extracted into `cmd/wfctl/plugin_release_grade_semver.go` (constant sourced by `validate-contract --for-publish` AND `registry-sync`).

### Layer (a'): workflow-registry parity cycle

PR in `workflow-registry`:

1. Add `wfctl plugin registry-sync` (and `core` + `readme` sub-modes) calls to `sync-registry-manifests.yml` running **in DRY-RUN MODE alongside the existing bash** (no `--fix`).
2. Both bash + Go write their proposed manifest diffs to workflow artifacts.
3. CI job compares the two artifacts; non-zero diff fails the workflow.
4. The actual registry mutations continue coming from the bash scripts during the parity window. **Bash remains authoritative; Go is observation-only.**
5. After one weekly cron cycle (or operator-triggered manual cycle) confirms zero diff for `sync-versions.sh` + `sync-core-manifests.sh` + `generate-readme.sh`, ship the **followup PR** that:
   - Swaps `--fix` mode from bash to Go for all three.
   - Deletes the three bash scripts.

This addresses cycle 1 C1 (parity must cover all three scripts) + D2 (translation risk) + I6 (bash gh-api retains the authoritative path during the window so any rename redirects can't break it).

### Capability verification flow (--verify-capabilities)

**Per cycle 1 C3:** the existing `wfctl plugin install --local <dir>` pipeline already handles binary rename (`ensurePluginBinary`) + lockfile/integrity checks. Rather than spawning via raw `manager.go` machinery, `--verify-capabilities` reuses the install path:

1. `gh release download <tag> --repo <gh_repo> --pattern '<plugin-name>-<os>-<arch>.tar.gz' -O /tmp/<plugin>.tar.gz`
2. Extract to `/tmp/<plugin>-extracted/`
3. `wfctl plugin install --local /tmp/<plugin>-extracted/ --plugin-dir /tmp/<plugin>-installed/` (existing pipeline; handles rename + lockfile + integrity)
4. Use the installed plugin's spawn path (also already exists for `wfctl plugin info`) to call `GetContractRegistry` RPC
5. Diff against committed `plugin.json.capabilities`; with `--fix` rewrite

**Per cycle 1 I4:** `--verify-capabilities` is registry-side only (runs on the periodic cron). Layer (b) per-PR migrations do NOT use this flag — capabilities are auto-populated on the next cron sync after the release lands. Documented in §Layer (b).

### Layer (a'): workflow-registry switch + parity cycle

PR in `workflow-registry`:

1. Add `wfctl plugin registry-sync --fix` to `sync-registry-manifests.yml` as a NEW step running AFTER the existing `bash scripts/sync-versions.sh --fix`.
2. In dry-run mode for both, log the diff between bash and Go outputs into the workflow artifact.
3. After one weekly cron cycle confirms zero output diff, ship the **followup PR** that deletes `sync-versions.sh` + removes the bash step.

This belts-and-suspenders pattern addresses self-challenge doubt D2 (bash → Go translation parity risk).

### Layer (d): template-repo modernization

**Renames:**
- `workflow-plugin-template` → `scaffold-workflow-plugin` (public; prefix-first naming so it doesn't look like a plugin family member; per user)
- `workflow-plugin-template-private` → `scaffold-workflow-plugin-private` (private; suffix `-private` keeps both scaffolds alphabetically adjacent in org browse — see I7 mitigation below)

**Cycle 1 fixes baked in:**
- **C5 + I1** (parallel scaffolding mechanism drift + unsafe sed ritual): the scaffold ships `scripts/rename-from-scaffold.sh` (TESTED in scaffold CI: runs `bash scripts/rename-from-scaffold.sh test-plugin` against a tmp copy of itself + `go build ./...` to assert the rename produces a buildable plugin). README points to the script. `wfctl plugin init` is REPLACED by `wfctl plugin init --from-scaffold [scaffold-workflow-plugin|scaffold-workflow-plugin-private]` which clones the scaffold + runs the rename script + git-init. Existing `sdk.NewTemplateGenerator` deprecated + removed in the same workflow PR series.
- **I8** (ServeIaCPlugin requires IaC surface): scaffold ships TWO main.go files in `cmd/`:
  - `cmd/scaffold-workflow-plugin/main.go` — uses `sdk.Serve` + `sdk.WithBuildVersion` (non-IaC default).
  - `cmd/scaffold-workflow-plugin-iac/main.go` — uses `sdk.ServeIaCPlugin` + `IaCServeOptions.BuildVersion` + stub `IaCProviderRequiredServer` implementation.
  - The rename script takes a `--mode iac|non-iac` flag (default non-iac) and deletes the other main.go before renaming.
- **I7** (`-private` ambiguity): README on the private scaffold opens with a paragraph: "This repo's `-private` suffix refers to its GitHub repo visibility (only org members can clone). It is NOT related to `plugin.json.private: true` semantics (which control marketplace listing). A plugin instantiated from this scaffold can choose either repo visibility independently."

**Per-repo steps (one PR per scaffold):**

1. `gh repo rename` (GitHub keeps old-URL redirect indefinitely unless a new repo claims the old name).
2. GitHub repo settings: enable `template_repository: true`. **I2 mitigation:** README on both scaffolds opens with a section "After creating a new repo from this template: enable GitHub Actions under Settings → Actions → 'I understand my workflows, enable them' before tagging your first release."
3. Content updates (single PR per scaffold):
   - `plugin.json`: `name`: `scaffold-workflow-plugin` (or `-private`); `version`: `"0.0.0"`; `minEngineVersion`: `0.61.0`; capabilities populated with placeholder shape (`moduleTypes: ["TEMPLATE.module"]`, `stepTypes: ["TEMPLATE.step"]`, `triggerTypes: []`, `iacProvider: {resourceTypes: ["TEMPLATE.resource"]}`) — shows expected shape.
   - Two main.go files per I8 above.
   - `internal/version.go`: `var Version = "dev"`.
   - `.goreleaser.yaml`: `-X github.com/GoCodeAlone/scaffold-workflow-plugin/internal.Version={{.Version}}` ldflag.
   - `.github/workflows/release.yml`: setup-wfctl@v1 + pre-build + post-build `wfctl plugin validate-contract` gates.
   - `.github/workflows/scaffold-rename-test.yml` (NEW): scaffold CI runs `bash scripts/rename-from-scaffold.sh testplugin --mode iac` and `--mode non-iac` against tmp copies; verifies `go build ./...` clean in both. Catches C5 silent-corruption regressions.
   - **No** `sync-plugin-version.yml` (defunct workflow not shipped in new scaffolds).
   - `scripts/rename-from-scaffold.sh`: TESTED rename script — enumerates every file containing `scaffold-workflow-plugin`; renames `cmd/scaffold-workflow-plugin/` → `cmd/workflow-plugin-<your-name>/`; `go mod edit`; `sed` (bounded to specific file globs, not `find . -name '*.go'`); `git add` + commit-ready state.
   - `README.md`: documents "Use this template" flow → enable Actions → run `bash scripts/rename-from-scaffold.sh <your-name> --mode {iac|non-iac}` → edit plugin.json capabilities → first commit + tag. References `wfctl plugin init --from-scaffold` as the alternative path.
4. Delete `workflow-registry/plugins/template/` (scaffold is not an installable plugin); commit in workflow-registry PR. **I3 mitigation:** the workflow-registry PR body explicitly states "operators with `template` in their `.wfctl-lock.yaml` must remove it; the entry was a non-functional stub."
5. Add registry-side defense: `wfctl plugin registry-sync` rejects (not just warns) any manifest with `repository` field in the exact-allowlist `{"https://github.com/GoCodeAlone/scaffold-workflow-plugin", "https://github.com/GoCodeAlone/scaffold-workflow-plugin-private"}` — cycle 1 C4 fix (allowlist not regex). Plugins legitimately containing "scaffold" in their name (e.g., `workflow-plugin-scaffold-tool`) pass through unchanged.
6. Update workflow#760 sweep list: drop `workflow-plugin-template` + `workflow-plugin-template-private`. 56 → 54.
7. **I6 verification task:** Layer (a') parity-cycle window confirms bash + Go both handle the (now-renamed) scaffold URLs correctly. If `gh api repos/<old-name>/contents/...` fails to redirect, the bash script's `fetch_plugin_json` returns empty string and silently falls back (which is the current behavior for any missing-plugin-json case). Go port replicates this fallback per cycle 1 C2 fix below.

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
4. For repos with null/missing `capabilities`: **populated by the agent during the PR via local-build introspection** (NOT via `--verify-capabilities` against a released binary, which has the cycle 1 D1/I4 chicken-and-egg). Specifically the agent runs `GOWORK=off go build -o /tmp/<plugin> ./cmd/...` against the WIP migration locally, then exec's the binary + GetContractRegistry RPC to populate capabilities. **Per cycle 1 I4 + I3 (deferred from #758)**: registry-side cron `wfctl plugin registry-sync --verify-capabilities --fix` is the ongoing safety net for capability drift detection AFTER releases land; the per-PR populate is a one-time bootstrap.
5. release.yml: add setup-wfctl + pre+post wfctl plugin validate-contract gates.
6. Bump workflow pin to v0.61.0 (or current latest).

Fans out via parallel sub-agents post-Layer (c). **Per cycle 1 I5:** the lead agent pre-computes the (repo, last-release-date) list via `gh api repos/GoCodeAlone/<repo>/releases?per_page=1` BEFORE fan-out and passes the pre-computed skip list (repos with no release in 90 days) to each sub-agent. No per-agent rate-limit cost.

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

## Cycle 1 — addressed

- **C1 (sync-core-manifests + generate-readme ignored)**: addressed — Layer (a) now ports all three scripts via `wfctl plugin registry-sync` + `core` + `readme` sub-modes; Layer (a') parity-cycle covers all three with bash-authoritative + Go-observation in dry-run.
- **C2 (bash → Go parity edge cases)**: addressed — fixture set explicitly enumerated (empty-assets, fetch-plugin-json-missing, prerelease-tag-vs-stable, sort-V-vs-semver); Go implementation must replicate bash output byte-for-byte during the parity window; `version_gt` uses the same `sort -V` semantics as bash for the comparator (sub-optimal vs true semver but parity-correct) — a separate follow-up can swap to semver-correct after parity is established.
- **C3 (plugin-spawn binary-rename + lockfile contract)**: addressed — `--verify-capabilities` reuses the `wfctl plugin install --local` pipeline (which already handles binary rename + lockfile + integrity); does NOT spawn via raw `manager.go`.
- **C4 (regex over-match for scaffold defense)**: addressed — exact-URL allowlist for two specific repos, not regex/substring.
- **C5 (sed ritual unsafe + incomplete)**: addressed — scaffold ships TESTED `scripts/rename-from-scaffold.sh` (CI runs against tmp copies); rename is now a single command, not a sed-in-README. `wfctl plugin init --from-scaffold` clones + runs the script as the canonical scaffolding path (replaces `sdk.NewTemplateGenerator`).
- **I1 (parallel scaffolding mechanism drift)**: addressed via C5 — `wfctl plugin init` is reworked to consume the scaffold repo, eliminating the parallel mechanism.
- **I2 (template_repository workflow-enablement gotcha)**: addressed — README on both scaffolds documents the post-instantiation "enable Actions" step.
- **I3 (`template` lockfile pin break)**: addressed — workflow-registry PR body explicitly states the break + provides mitigation.
- **I4 (per-PR capability auto-populate chicken-and-egg)**: addressed — per-PR uses local-build introspection (build WIP main.go locally + spawn binary + GetContractRegistry); registry-side `--verify-capabilities` is the ongoing safety net for drift, not per-PR.
- **I5 (90-day stale gate operationalization)**: addressed — lead agent pre-computes skip list via single `gh api` batch; sub-agents receive pre-computed list.
- **I6 (gh repo rename + bash gh api redirect)**: addressed — bash remains authoritative during parity window; rename happens AFTER parity-cycle PR ships. Go port replicates the fetch_plugin_json silent-fallback so rename-redirect failures are tolerated (current bash behavior).
- **I7 (`-private` suffix ambiguity)**: addressed — README on private scaffold opens with explicit clarification.
- **I8 (`ServeIaCPlugin` requires IaC surface — single-entrypoint claim was wrong)**: addressed — scaffold ships TWO main.go files (IaC + non-IaC); rename script picks one via `--mode` flag.

## Adversarial cycles expected

- Cycle 2: likely pass; outstanding risks are around fixture exhaustiveness (Layer a) and the precise shape of the post-instantiation script (Layer d).
- Plan cycle 1: granularity, per-repo skip-gate operationalization (already addressed via lead-agent pre-compute), test fixture enumeration explicit per task.
