# IaC Deferred-Work Cleanup + C-1 Wrap-Up — Design

**Author**: autonomous brainstorming pipeline (Claude Opus 4.7)
**Date**: 2026-05-05
**Status**: rev1 — pre-adversarial-review

## Problem

The IaC conformance plan (Phases 1-6 merged) surfaced **9 deferred work items** across 3 repos that didn't fit any prior cluster. One of them (workflow#541) now blocks core-dump's C-1 cutover (PR #190 cannot cleanly merge). User has authorized addressing all deferred items in a unified plan, then closing C-1 (TC1 + TC2).

## Goal

One sentence: ship 5 PRs that close the 9 deferred items + cut workflow v0.21.2 + merge core-dump #190 + execute TC2 cascade-replace on coredump-staging.

## The 9 deferred items

| # | Repo | Issue | Class | Summary |
|---|------|-------|-------|---------|
| 1 | workflow | #536 | feature | `wfctl infra cleanup --tag <name>` subcommand |
| 2 | workflow | #537 | bug | plugin/external/convert.go silent-drop in mapToStruct |
| 3 | workflow | #539 | precision | iac-codemod `AssertDiffSetsNeedsReplaceForForceNew` accumulator-pattern false-positive |
| 4 | workflow | #540 | enforcement | Plugin SDK manifest schema `additionalProperties:false` |
| 5 | workflow | #541 | precision | R-A4 align rule consult top-level `secrets.generate` keys (BLOCKER for C-1) |
| 6 | workflow-plugin-digitalocean | #62 | bug | DOProvider.Initialize ignores ctx |
| 7 | workflow-plugin-digitalocean | #63 | refactor | DOProvider.Plan → platform.ComputePlan |
| 8 | workflow | (file at design-time) | refactor | cmd/wfctl/deploy_providers.go remoteIaCProvider non-canonicality |
| 9 | workflow | (file at design-time) | ADR | Platform-vs-provider scenario classification |

## Architecture

### Cluster shape (Approach C: blocker-first)

- **Phase 1 (sequential, blocks Phase 3)**: workflow PR for #541 only. Cuts v0.21.1.
- **Phase 2 (parallel, all run after Phase 1)**:
  - **W-Precision** PR: #537 + #539 + #540 (workflow correctness fixes)
  - **W-Cleanup** PR: #536 (`wfctl infra cleanup --tag` subcommand)
  - **W-Refactor** PR: backlog 8 (deploy_providers refactor) + backlog 9 (ADR)
  - **DO-Plugin** PR: #62 + #63 (DO plugin fixes; runs in workflow-plugin-digitalocean repo)
- **Phase 3 (sequential, closes the work)**:
  - Cut workflow v0.21.2 (rolls up Phase 2)
  - Cut DO plugin v0.10.1 (rolls up DO-Plugin PR)
  - Bump core-dump's pins to v0.21.2 + v0.10.1
  - Merge core-dump PR #190 (TC1)
  - Execute TC2 cascade-replace + /healthz verify
  - Final core-dump PR (TC2)

### Why this clustering

- **#541 is the critical-path blocker**. Carving it out lets us merge it + cut the patch tag in <30 min, freeing C-1 to start its closing-step queue early.
- **Phase 2 is fully independent**. Precision fixes don't depend on each other; new feature is standalone; refactor is standalone; DO plugin is in a different repo. All can run in parallel.
- **Phase 3 is sequential** because tag cutting + pin bumping has hard ordering.

### PR Grouping

| PR | Repo | Branch | Items | Tag-cut after merge |
|----|------|--------|-------|---------------------|
| W-541 | workflow | feat/r-a4-toplevel-secrets | #541 | v0.21.1 |
| W-Precision | workflow | feat/iac-precision-fixes | #537 #539 #540 | (rolls into v0.21.2) |
| W-Cleanup | workflow | feat/wfctl-infra-cleanup | #536 | (rolls into v0.21.2) |
| W-Refactor | workflow | refactor/remote-iac-provider | bl-8 bl-9 | (rolls into v0.21.2) |
| DO-Plugin | workflow-plugin-digitalocean | fix/initialize-ctx-and-plan-canonical | #62 #63 | v0.10.1 |
| C-1-Close | core-dump | feat/c1-staging-pg-cutover (existing PR #190 + new TC2 commits) | TC1 + TC2 | n/a (app repo) |

## Components

### Phase 1: W-541

**Files**:
- Modify: `cmd/wfctl/infra_align_rules.go` — `buildAlignContext` populates `ctx.secretKeys` from `cfg.Secrets.Generate` (and `cfg.Secrets.Requires` if present) alongside existing `ctx.secretGens` population.
- Test: `cmd/wfctl/infra_align_test.go` — new test pinning R-A4 success on top-level `secrets.generate` case.

**Behavior**: ~5 line additive fix. Pre-fix: only module-form `secrets.generate` populates `ctx.secretKeys`. Post-fix: top-level `secrets:` block also populates it, matching `secretGens`.

### Phase 2 PRs

#### W-Precision: 3 fixes in one PR

**#537 — plugin/external/convert.go silent-drop**:
- Modify `mapToStruct` to propagate the structpb.NewStruct error rather than fall back to empty Struct.
- Update callers to handle the error (likely already in error-return paths since they're gRPC handlers).
- Add regression test: provide a map with an unrepresentable type (chan, func), confirm error propagation.

**#539 — iac-codemod accumulator pattern**:
- Modify `cmd/iac-codemod/lint.go` `AssertDiffSetsNeedsReplaceForForceNew` analyzer to recognize local-accumulator pattern: `var acc bool; ... acc = true; ... result.NeedsReplace = acc`.
- AST traversal: detect assignment to a local `bool` variable that is later assigned to `result.NeedsReplace`.
- Add golden-file test: VolumeDriver-style accumulator pattern should NOT report a finding.

**#540 — manifest schema additionalProperties enforcement**:
- Investigate jsonschema library behavior in `plugin/sdk/manifest.go`.
- Likely fix: explicit `Strict: true` flag on validator, OR upgrading the library to one that honors draft-2020-12 correctly.
- Regression test: a manifest with extra keys in `iacProvider` block should fail validation.

#### W-Cleanup: `wfctl infra cleanup --tag` subcommand (#536)

**Files**:
- Create: `cmd/wfctl/infra_cleanup.go` — subcommand implementation.
- Create: `cmd/wfctl/infra_cleanup_test.go` — unit tests using fake provider.
- Modify: `cmd/wfctl/main.go` — register subcommand.
- Modify: `docs/WFCTL.md` — command reference.
- Modify: `.github/workflows/conformance-smoke.yml` — replace doctl-stub with `wfctl infra cleanup --tag`.
- Modify: `docs/conformance-runbook.md` — update "Known follow-ups" section.

**Behavior**: per #536 spec — list resources by tag across all loaded providers, delete via driver Delete path, honors `--dry-run`, returns non-zero on partial failure with structured stdout summary.

**Discovery question**: which providers expose a "list resources by tag" capability today? The DO plugin uses `godo.Tag`; AWS/GCP/Azure plugins each have native tag APIs. The interface needs a new method (e.g., `ListByTag(ctx, tag) ([]ResourceRef, error)`) on `IaCProvider` OR on `ResourceDriver`. **Decision: extend `IaCProvider` with optional `ResourceLister` interface** (similar pattern to `ProviderValidator`); plugins that don't implement it are skipped with a log line.

#### W-Refactor: deploy_providers refactor + ADR

**Files**:
- Modify: `cmd/wfctl/deploy_providers.go` — refactor `remoteIaCProvider` to use canonical `wfctlhelpers.Plan` + `wfctlhelpers.ApplyPlan` dispatch.
- Create: `docs/adr/010-platform-vs-provider-conformance-scenarios.md` — ADR codifying that 4 of 12 conformance scenarios are platform-level (test cross-provider-shared surfaces) vs the other 8 which are provider-level. Documents the bypass-cfg.Provider() pattern.
- Tests: existing `cmd/wfctl/deploy_providers_*_test.go` should still pass after refactor (interface-level behavior unchanged).

#### DO-Plugin: thread ctx + collapse Plan

**Files**:
- Modify: `internal/provider.go` — `DOProvider.Initialize` threads `ctx` to godo client constructor.
- Modify: `internal/provider.go` — `DOProvider.Plan` collapses to `return platform.ComputePlan(ctx, p, desired, current)`.
- Update tests if any rely on the old non-canonical Plan implementation.

### Phase 3: closing C-1

1. Wait for all Phase 2 PRs to merge to workflow + DO plugin.
2. Cut `workflow v0.21.2` at workflow main HEAD.
3. Cut `workflow-plugin-digitalocean v0.10.1` at DO main HEAD.
4. In core-dump worktree: bump `wfctl.yaml` + `.wfctl-lock.yaml` + `.github/workflows/*.yml` setup-wfctl version to v0.21.2 + DO plugin to v0.10.1.
5. Re-run core-dump CI; confirm `wfctl infra align --strict` passes WITHOUT the env-var stopgap (proves W-541 fix is live).
6. Admin-merge core-dump PR #190 (TC1).
7. Execute TC2: `wfctl infra apply --allow-replace=<the 4 protected resources by name>` against coredump-staging. Capture pre/post resource IDs.
8. Post-cutover: poll `https://staging-coredump-app.<...>/healthz` until 200; capture transcript.
9. Open + admin-merge core-dump TC2 PR.

## Assumptions

1. **workflow#541 is a 5-line additive fix.** Verified via direct read of `cmd/wfctl/infra_align_rules.go:33-66`. The buildAlignContext function already has the cfg.Secrets.Generate slice in scope; the fix is iterating it and populating secretKeys.

2. **workflow + DO plugin tag-driven Release CI is reliable.** Verified by v0.21.0 + DO v0.10.0 builds completing earlier today.

3. **`setup-wfctl@v1` picks up new tags within minutes.** Verified by C-1 TC1's bump to v0.21.0 succeeding.

4. **Existing test infrastructure catches most precision-fix regressions, but #540 needs a NEW test.** Each precision PR adds a regression-pin test for its specific failure mode (the discipline from W-7/W-8 reviews).

5. **Standalone background-agent pattern continues to work.** Proven across W-7 (R5 cycles), W-8 (12-round Copilot review), P-DO (5 rounds), C-1 (in flight). Team-tmux infrastructure is unreliable.

6. **Copilot will keep finding real bugs in 1-5+ rounds per PR.** Plan for it; budget time accordingly.

7. **coredump-staging deploy pipeline exercises W-541 fix.** Once core-dump pins bump to v0.21.1+, the existing deploy.yml `align --strict` step IS the verification.

8. **TC2 staging blast radius is bounded.** W-6 `--allow-replace=<names>` requires explicit per-resource opt-in. Plan §C-1 has a documented git-revertible rollback path. Staging /healthz is the verification gate.

9. **The 4 protected resources in coredump-staging** are: VPC + Database + Redis + App (or similar shape). Names will be discovered at TC2 time from `infra.yaml`.

## Rollback

This design affects runtime change classes (build configuration, version pins on runtime components, deploy pipeline behavior). Per-PR rollback paths:

- **W-541**: revert commit; the fallback is the env-var stopgap (re-add the `STAGING_PG_PASSWORD` and `STAGING_VPC_UUID` env exports to deploy.yml's align step). Documented in commit body.
- **W-Precision**: revert commit; tests still pass (regressions are caught at CI). Each fix is independent.
- **W-Cleanup**: revert commit; conformance-smoke.yml falls back to T7.14 leak scrubber (the existing safety net).
- **W-Refactor**: revert commit; existing deploy_providers behavior is preserved (the refactor is interface-equivalent).
- **DO-Plugin**: revert commit; existing v0.10.0 behavior continues; no breaking changes.
- **TC1 (core-dump #190)**: git-revertible; deploy pipeline reverts to env-var stopgap form. Plan §C-1 documents this.
- **TC2 (cascade-replace)**: per W-6 `--allow-replace`, the plan output before apply shows the cascade explicitly. If the apply fails mid-cascade, infra.yaml + state in DB are still consistent (W-3a's drift postcondition + ApplyResult.ReplaceIDMap track the partial-progress state). Recovery path: `wfctl infra apply` again with the partial-applied resources skipped (or `infra apply --refresh` if drift was detected).

## Top doubts (self-challenge surfaces)

1. **Phase 2 parallelism may produce merge conflicts** in workflow main if W-Precision + W-Cleanup + W-Refactor all touch overlapping files (e.g., main.go subcommand registration + docs/WFCTL.md command reference). Mitigation: each PR's file scope is disjoint by design (see "Files" sections); rebase-as-needed at merge time.

2. **#540 schema-enforcement fix may have wider blast radius** than expected. If the jsonschema library has been silently accepting extra keys for a long time, MANY plugin manifests in the registry may have extra keys that suddenly fail validation. Mitigation: investigate scope first; if wider than 1-2 plugins, add a deprecation warning step (warn → error in next minor version) rather than instant-fail.

3. **TC2 cascade-replace assumes `wfctl infra apply --allow-replace` is bug-free** — never used in production before. Mitigation: the staging blast radius bounds the failure cost; existing W-6 unit tests in workflow main cover the cascade path; if it fails, plan §C-1 rollback is available.

## Decision Records

This design will create:
- ADR 010: Platform-vs-provider scenario classification (W-Refactor PR; backlog item 9).

The other items don't trigger recording-decisions criteria (they're bug fixes / precision improvements / a new subcommand / a refactor that's interface-equivalent).

## Pipeline expectation

Standalone background agents per cluster (per IaC conformance plan operational pattern). Each agent:
- Operates in its own worktree.
- Self-paces via ScheduleWakeup + Monitor + bash watchdog.
- Handles Copilot review cycle independently.
- Admin-merges per workspace memory `feedback_admin_override_pr_merge` once Copilot resolved + non-Lint CI green.
- Reports back with merge SHA + summary.

Phase 1 + Phase 3 are coordinated by the orchestrator (this session). Phase 2's 4 PRs run in parallel with independent agents.
