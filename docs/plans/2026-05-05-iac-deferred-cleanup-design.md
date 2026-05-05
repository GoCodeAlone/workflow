# IaC Deferred-Work Cleanup + C-1 Wrap-Up — Design

**Author**: autonomous brainstorming pipeline (Claude Opus 4.7)
**Date**: 2026-05-05
**Status**: rev3 — addresses adversarial review #2 findings (2 Critical + 3 Important + 1 regression)
**Adversarial review #1**: `2026-05-05-iac-deferred-cleanup-design.adversarial-review-1.md` (commit 5897429)
**Adversarial review #2**: `2026-05-05-iac-deferred-cleanup-design.adversarial-review-2.md` (commit 4c6ed67)

## Problem

The IaC conformance plan (Phases 1-6 merged) surfaced **9 deferred work items** across 3 repos that didn't fit any prior cluster. One of them (workflow#541) blocks core-dump's C-1 cutover (PR #190 cannot cleanly merge). User has authorized addressing all deferred items in a unified plan, then closing C-1 (TC1 + TC2 as separate PRs per PR #190's operator commitment).

## Goal

One sentence: ship 6 PRs (5 deferred-cleanup + 1 separate TC2 PR) that close the 9 deferred items + cut workflow v0.21.1 + cut workflow-plugin-digitalocean v0.10.1 + merge core-dump #190 (TC1) + execute TC2 cascade-replace on coredump-staging via a fresh PR on `feat/c2-staging-pg-cutover-nyc1`.

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

## Excluded items (reviewer-flagged, deliberate)

- **workflow#542 (DO_CONFORMANCE_API_TOKEN provisioning)** — out-of-scope. Per user direction (2026-05-05): the dedicated DO conformance account adds tracking + billing overhead the team does not want to absorb. Staging IS the test environment. Token provisioning is operator-side ops work, not engineering work; #542 stays open as a deferred-not-required tracking issue. The W-7 conformance-budget-check.yml already handles unset token as "kill-switch unarmed" (silent no-op).

## Architecture

### Cluster shape (Approach C: blocker-first)

- **Phase 1 (sequential, blocks Phase 3)**: workflow PR for #541 only. Cuts v0.21.1.
- **Phase 2 (parallel, all run after Phase 1)**:
  - **W-Precision** PR: #537 + #539 (workflow correctness fixes; #540 split out per I-3)
  - **W-Diagnose-540** PR: #540 diagnostic-first (failing test that proves current laxness; fix may follow as separate PR depending on root cause)
  - **W-Cleanup** PR: #536 (`wfctl infra cleanup --tag` subcommand)
  - **W-Refactor** PR: backlog 8 (deploy_providers refactor) + backlog 9 (ADR 010)
  - **DO-Plugin** PR: #62 + #63 (DO plugin fixes; runs in workflow-plugin-digitalocean repo)
- **Phase 3 (sequential, closes the work)**:
  - Cut workflow v0.21.2 (rolls up Phase 2 workflow PRs)
  - Cut DO plugin v0.10.1 (rolls up DO-Plugin PR)
  - Bump core-dump's pins to v0.21.2 + v0.10.1 (amend PR #190 head)
  - Re-run core-dump CI; confirm `wfctl infra align --strict` passes WITHOUT env-var stopgap
  - Admin-merge core-dump PR #190 (TC1)
  - Branch fresh `feat/c2-staging-pg-cutover-nyc1` from core-dump main; open new PR for TC2
  - Execute TC2 cascade-replace + /healthz verify
  - Admin-merge TC2 PR

### TC1.5 explicit SKIPPED

Per PR #190 body + user direction (2026-05-05):
> "TC1.5 ⏭️ SKIPPED (operator decision) — Per jon@langevin.me 2026-05-05: dedicated DO conformance account adds tracking + billing overhead the team does not want to absorb. Staging IS the test environment; W-6 `--allow-replace=<names>` per-resource opt-in + W-3a/b unit-tested cascade is the safety belt."

Safety belt replacing TC1.5:
- W-6 `--allow-replace=<names>` requires explicit per-resource opt-in (no blanket replace)
- W-3a/b unit tests cover cascade dispatch + JIT-substitution + ApplyResult.ReplaceIDMap tracking
- workflow#541 fix (Phase 1) ensures align-time validation passes without env-var stopgap
- Plan §C-1 git-revertible rollback if TC2 partially fails
- Live `/healthz` 200 verification gates TC2 declaration

Reference: workflow#542 stays open as the upstream block that would have made TC1.5 dry-run runnable on a separate account.

### PR Grouping

| PR | Repo | Branch | Items | Tag-cut after merge |
|----|------|--------|-------|---------------------|
| W-541 | workflow | feat/r-a4-toplevel-secrets | #541 | v0.21.1 |
| W-Precision | workflow | feat/iac-precision-fixes | #537 #539 | (rolls into v0.21.2) |
| W-Diagnose-540 | workflow | test/manifest-schema-strict-diagnostic | #540 (test only; fix follows) | (rolls into v0.21.2 if fix lands; otherwise diagnostic only) |
| W-Cleanup | workflow | feat/wfctl-infra-cleanup | #536 | (rolls into v0.21.2) |
| W-Refactor | workflow | refactor/remote-iac-provider | bl-8 bl-9 | (rolls into v0.21.2) |
| DO-Plugin | workflow-plugin-digitalocean | fix/initialize-ctx-and-plan-canonical | #62 #63 | v0.10.1 |
| C-1-TC1 | core-dump | feat/c1-staging-pg-cutover (existing PR #190) | TC1 | n/a |
| C-1-TC2 | core-dump | feat/c2-staging-pg-cutover-nyc1 (NEW, branched from main AFTER TC1 lands) | TC2 | n/a |

## Components

### Phase 1: W-541 (R-A4 align rule consult top-level secrets)

**Files**:
- Modify: `cmd/wfctl/infra_align_rules.go` — `buildAlignContext` populates `ctx.secretKeys` from `cfg.Secrets.Generate[i].Key` AND from `cfg.Secrets.Entries[i].Name`. Drop the prior speculation about `cfg.Secrets.Requires` (verified non-existent on `SecretsConfig` per `config/secrets_config.go:21-33`).
- Test: `cmd/wfctl/infra_align_test.go` — TWO new tests pinning R-A4 success: (a) top-level `secrets.generate` case using `Generate[i].Key`; (b) top-level `secrets.entries` case using `Entries[i].Name`.

**Estimated diff**: ~5 lines production + ~30-50 lines test = ~35-55 lines total. Not the "5-line additive fix" rev1 claimed.

**Behavior**: Pre-fix: only module-form `secrets.generate` (e.g. `infra.modules[].type == "secrets.generate"`) populates `ctx.secretKeys`. Top-level `secrets:` block populates `secretGens` (used by R-A9) but NOT `secretKeys`. Post-fix: top-level `secrets.generate` AND `secrets.entries` also populate `secretKeys`, matching the existing `secretGens` population path.

### Phase 2 PRs

#### W-Precision: 2 fixes in one PR (was 3; #540 split per I-3)

**#537 — plugin/external/convert.go silent-drop**:
- Modify `mapToStruct` to propagate the structpb.NewStruct error rather than fall back to empty Struct.
- Update callers to handle the error (likely already in error-return paths since they're gRPC handlers).
- Add regression test: provide a map with an unrepresentable type (chan, func), confirm error propagation.

**#539 — iac-codemod accumulator pattern**:
- Modify `cmd/iac-codemod/lint.go` `AssertDiffSetsNeedsReplaceForForceNew` analyzer to recognize local-accumulator pattern: `var acc bool; ... acc = true; ... result.NeedsReplace = acc`.
- AST traversal: detect assignment to a local `bool` variable that is later assigned to `result.NeedsReplace`.
- Add golden-file test: VolumeDriver-style accumulator pattern should NOT report a finding.

#### W-Diagnose-540: schema strictness diagnostic (split per I-3)

**Why split**: rev1 bundled #540 into W-Precision under the assumption that the schema needed a "Strict flag". Per I-3, the schema ALREADY declares `additionalProperties: false` on the `iacProvider` sub-object. The bug is library-version or library-config, not schema-text. Without diagnosis first, the fix could miss the root cause.

**Files** (revised per N5: test must FAIL CI from day one):
- Add: a test in `plugin/sdk/manifest_test.go` — load a manifest with an extra key (`iacProvider.bogusKey`); call validation; if validation does NOT return an error, call `t.Errorf("BUG: extra iacProvider key not rejected — see workflow#540; schema declares additionalProperties:false but library accepts extra keys")`.
- The fix follows in a separate PR after diagnosis identifies the root cause (likely santhosh-tekuri/jsonschema/v6 minor-version regression OR validator config flag). The fix flips the test from FAIL→PASS (no test edit; just behavior change).

**Behavior**: This PR is FAIL-CI from the moment it lands. CI red is the visible "needs fix" signal — a passing test reporting a bug is a lie. The fix follow-up PR makes CI green by fixing the validator to honor additionalProperties:false. Trade-off: red CI on workflow main until fix lands; this is acceptable per `feedback_proper_fixes_over_workarounds` (visible-pending-fix beats invisible-bug). If red CI on main is operationally unacceptable, alternative shape is `t.Skip("BUG: ...; see workflow#540")` with a runbook entry to track the skip — but the fail-CI shape is the recommended option.

**Phase 3 inclusion**: This PR does NOT need to roll into v0.21.2 unless the fix follow-up also lands by then. If fix follow-up lands → include both in v0.21.2. If only diagnostic lands → include diagnostic in v0.21.2, defer fix to v0.21.3.

#### W-Cleanup: `wfctl infra cleanup --tag` subcommand (#536)

**Files**:
- Create: `cmd/wfctl/infra_cleanup.go` — subcommand implementation.
- Create: `cmd/wfctl/infra_cleanup_test.go` — unit tests using fake provider.
- Modify: `cmd/wfctl/main.go` — register subcommand.
- Modify: `docs/WFCTL.md` — command reference.
- Modify: `.github/workflows/conformance-smoke.yml` — replace doctl-stub with `wfctl infra cleanup --tag`.
- Modify: `docs/conformance-runbook.md` — update "Known follow-ups" section.

**Behavior**: per #536 spec — list resources by tag across all loaded providers, delete via driver Delete path, honors `--dry-run`, returns non-zero on partial failure with structured stdout summary.

**Interface design (revised per N1: opt-in, NOT required)**: define a NEW optional interface `Enumerator` in `interfaces/iac_provider.go`, following the established `ProviderValidator` / `ProviderPlanner` pattern (verified at `interfaces/iac_provider.go:48-62` on origin/main):

```go
// Enumerator is an optional interface for providers that can list
// resources by tag across the cloud account. Used by `wfctl infra
// cleanup --tag <name>`. Providers without a tag-query API simply
// do not implement it; cleanup skips them with a structured log line.
type Enumerator interface {
    EnumerateByTag(ctx context.Context, tag string) ([]ResourceRef, error)
}
```

`wfctl infra cleanup` does the type assertion at the use site:
```go
if enum, ok := provider.(interfaces.Enumerator); ok {
    refs, err := enum.EnumerateByTag(ctx, tag)
    // ... delete via driver Delete path
} else {
    log.Printf("cleanup: provider %s does not implement Enumerator; skipping", provider.Name())
}
```

**Why opt-in**: matches workspace precedent (ProviderValidator/ProviderPlanner are both opt-in via type assertion). Avoids breaking the cross-plugin-build-test gate (which `go build`s AWS/GCP/Azure plugins against the W-Cleanup PR's workflow checkout). No "3 plugin-stub PR" scope creep. AWS/GCP/Azure plugins implement Enumerator on their own per-plugin cycles; until then, the cleanup subcommand prints `skipped <provider>: no Enumerator interface` and the operator sees the explicit skip in stdout (no silent leak — the message is visible).

**Plugin coordination**: only DO plugin needs the implementation in this cycle (rolled into DO-Plugin PR — see updated DO-Plugin §Files below). AWS/GCP/Azure stubs are NOT required by this design.

#### W-Refactor: deploy_providers refactor + ADR

**Files**:
- Modify: `cmd/wfctl/deploy_providers.go` — refactor `remoteIaCProvider` to use canonical `wfctlhelpers.Plan` + `wfctlhelpers.ApplyPlan` dispatch.
- Create: `docs/adr/010-platform-vs-provider-conformance-scenarios.md` — ADR codifying that 4 of 12 conformance scenarios are platform-level (test cross-provider-shared surfaces) vs the other 8 which are provider-level. Documents the bypass-cfg.Provider() pattern as a SHIPPED design choice (not introducing new pattern).
- Tests: existing `cmd/wfctl/deploy_providers_*_test.go` should still pass after refactor (interface-level behavior unchanged).
- Modify: `docs/WFCTL.md` if the refactor surfaces any user-visible CLI change (likely none).

#### DO-Plugin: thread ctx + collapse Plan + EnumerateByTag impl

**Files**:
- Modify: `internal/provider.go` — `DOProvider.Initialize` threads `ctx` to godo client constructor.
- Modify: `internal/provider.go` — `DOProvider.Plan` collapses to `return platform.ComputePlan(ctx, p, desired, current)`. Verify `platform.ComputePlan` signature against `origin/main` of workflow before drafting (recurring plan-literal-vs-reality risk).
- Modify: `internal/provider.go` — implement opt-in `interfaces.Enumerator.EnumerateByTag(ctx, tag) ([]interfaces.ResourceRef, error)` using `godo.Tags.List(ctx, name, ...)` to query DO API for tagged resources. Returns ResourceRef list; cleanup subcommand handles deletion via driver Delete path.
- Add: `internal/provider_enumerator_test.go` — pin Enumerator type assertion + EnumerateByTag behavior with godo mock.
- Update tests if any rely on the old non-canonical Plan implementation.

**Sequencing**: opt-in interface → no cross-plugin gate failure when W-Cleanup workflow PR pushes (other plugins simply don't implement Enumerator, no compile break). DO-Plugin PR can land independently of W-Cleanup PR; the type-assertion at use site means `wfctl infra cleanup --tag` works against DO once both PRs land in any order.

### Phase 3: closing C-1

1. Wait for all Phase 2 PRs to merge to workflow + DO plugin.
2. Cut `workflow v0.21.2` at workflow main HEAD.
3. Cut `workflow-plugin-digitalocean v0.10.1` at DO main HEAD.
4. In core-dump worktree (`feat/c1-staging-pg-cutover`): bump `wfctl.yaml` + `.wfctl-lock.yaml` + `.github/workflows/*.yml` setup-wfctl version to v0.21.2 + DO plugin to v0.10.1. Force-push to existing PR #190.
5. Re-run core-dump CI; confirm `wfctl infra align --strict` passes WITHOUT the env-var stopgap (proves W-541 fix is live).
6. Admin-merge core-dump PR #190 (TC1).
7. **TC1.5 SKIPPED** per operator decision (see "TC1.5 explicit SKIPPED" section above).
8. Branch fresh `feat/c2-staging-pg-cutover-nyc1` from core-dump main (post-TC1-merge).
9. **§TC2 Execution** (see dedicated subsection below).
10. Open PR for TC2 (with pre/post resource ID capture in body); admin-merge.

### §TC2 Execution (detailed)

**The 4 protected resources** (verified against PR #190 body + conformance plan §TC2):
- `core-dump-vpc` (VPC)
- `coredump-staging-pg-data` (Postgres data volume)
- `coredump-staging-pg` (Postgres database)
- `coredump-staging-pg-fw` (Postgres firewall)

All 4 are **database-tier** resources. Blast radius: **5-15 min downtime per resource** during cascade-replace (NOT app-tier ~30s blue-green). This is staging, not prod, so user-impact is bounded to "developers see staging-down for ~30-60 min total".

**Pre-flight** (revised per N2: drop persisted-plan flow):
1. `wfctl infra plan -c infra.yaml --env staging` — stdout-only preview; confirm plan matches expected cascade.
2. Verify plan output enumerates exactly the 4 resources above + their dependent recreations.
3. Snapshot pg state if appropriate (operator decision; outside automation scope).

**Why no `-o /tmp/tc2-plan.json`**: the persisted-plan flow at `cmd/wfctl/infra.go:292` rejects plans containing `${MODULE.field}` JIT refs (verified). Any future infra.yaml edit adding such a ref (canonical W-5 pattern for cross-resource refs) would halt this procedure mid-flight. Direct apply (without `--plan`) avoids the JIT-rejection precondition entirely. The drift-postcondition + ApplyResult.ReplaceIDMap protections in W-3a/b apply identically under direct apply.

**Cascade command** (literal):
```
wfctl infra apply -c infra.yaml --env staging \
  --allow-replace=core-dump-vpc,coredump-staging-pg-data,coredump-staging-pg,coredump-staging-pg-fw
```

**Expected stdout (sketch)**:
```
Loading plan from /tmp/tc2-plan.json ...
Replace cascade: 4 protected resources will be replaced + N dependents recreated.
Allow-list verified: 4/4 protected resources opted-in.
[1/4] core-dump-vpc: Delete + Create ... (region: nyc1, ID: vpc-XXX → vpc-YYY)
[2/4] coredump-staging-pg-data: Delete + Create ... (volume-XXX → volume-YYY)
[3/4] coredump-staging-pg: Delete + Create ... (db-cluster-XXX → db-cluster-YYY)
[4/4] coredump-staging-pg-fw: Delete + Create ... (fw-XXX → fw-YYY)
Cascade complete in N minutes; ApplyResult.ReplaceIDMap captured.
```

**Recovery procedures (per partial-failure mode)**:

| Failure mode | Recovery |
|---|---|
| VPC create succeeds but pg-data create fails | `wfctl infra apply` again to retry pg-data; W-3a's drift postcondition tracks the partial state |
| pg create succeeds but pg-fw create fails | Same; retry. fw is fast (seconds) so recovery is quick |
| All 4 succeed but app fails to reconnect | App config refresh: `wfctl infra refresh-outputs` (W-2) re-resolves connection strings; restart app pod |
| Cascade aborts mid-flight | Plan §C-1 rollback: `git revert` the TC2 commit; re-run `wfctl infra apply` to revert to pre-TC2 resource shape |
| Region constraint violation (nyc → only nyc1 VPCs) | Should be caught at align/plan time by W-4 ProviderValidator (P-DO TP3); if it fires post-apply, file follow-up bug |

**Post-cutover verification**:
```
# Wait for staging app to come up + reconnect
sleep 30
# Poll /healthz until 200 (max 5 min)
for i in $(seq 1 30); do
  status=$(curl -s -o /dev/null -w "%{http_code}" https://staging.coredump.<...>/healthz)
  echo "[$(date)] healthz: $status"
  [ "$status" = "200" ] && break
  sleep 10
done
```

Capture transcript in TC2 PR commit body for audit trail.

## Plan-literal-vs-reality surfaces (proactive enumeration per per-attack #6)

The recurring defect class across W-4 / W-5 / W-9 / W-7 / P-DO / W-8 was plan-literal references to symbols/paths/fields that don't exist or have wrong names. Surfaces likely to bite this plan:

1. **W-541**: `cfg.Secrets.Requires` doesn't exist (rev1 hedged around it; rev2 drops the speculation). Verify `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name` field names exactly match against `config/secrets_config.go` on origin/main.
2. **W-Cleanup**: `Enumerator` interface + `EnumerateByTag` method name are NEW; check `interfaces/iac_provider.go` on origin/main for any conflicting symbols. Verify no existing `Enumerator` or `EnumerateByTag` symbol elsewhere. Pattern: `git -C /Users/jon/workspace/workflow grep -n "Enumerator\|EnumerateByTag" origin/main -- '*.go'` (must return zero hits).
3. **W-Refactor**: `wfctlhelpers.Plan` and `wfctlhelpers.ApplyPlan` signatures must be verified against origin/main (not against this design worktree which is 11+ commits behind).
4. **DO-Plugin #63**: `platform.ComputePlan` signature must be verified — collapse `DOProvider.Plan` to `return platform.ComputePlan(ctx, p, desired, current)` only after confirming the actual function takes those exact args.
5. **#540 schema test**: verify `plugin/sdk/manifest_test.go` exists on origin/main; if not, the test creation goes in a new file that the implementer must scaffold.
6. **TC1 pin bump (Phase 3 step 4)**: verify the EXACT setup-wfctl action pin format. `version: v0.21.2` matches the v0.21.0 precedent that already merged in PR #190.
7. **TC2 cascade command** (Phase 3 §TC2 Execution): verify `wfctl infra apply --allow-replace=<csv>` flag name exactly against `cmd/wfctl/infra_apply.go` on origin/main. (Per N2 fix, no `--plan` flag is used in TC2.)

Each implementer must, as the FIRST step of their PR, run `git show origin/main:<file>` for every plan-referenced file/symbol to confirm reality matches the design.

## Assumptions (revised per Critical findings)

1. **workflow#541 PR diff ~35-55 lines total** (~5 prod + ~30-50 test). Adversarial review #1 I-4 corrected rev1's underestimate. Each Copilot review round adds typically 5-30 lines.

2. **workflow + DO plugin tag-driven Release CI is reliable.** Verified by v0.21.0 + DO v0.10.0 builds completing earlier today.

3. **`setup-wfctl@v1` picks up new tags within minutes.** Verified by C-1 TC1's bump to v0.21.0 succeeding.

4. **Existing test infrastructure catches most precision-fix regressions, but #540 needs root-cause diagnosis FIRST** (per I-3). #540 split into W-Diagnose-540 (test that proves laxness) + later fix PR (after diagnosis).

5. **Standalone background-agent pattern continues to work.** Proven across W-7 / W-8 / P-DO / C-1.

6. **Copilot will keep finding real bugs in 1-12 rounds per PR.** W-8 took 12 rounds; budget Phase 2 wall-clock accordingly. Per-attack #8 mitigation: keep W-Precision + DO-Plugin parallel (low Copilot surface); serialize W-Cleanup → W-Refactor (new feature + interface refactor; higher Copilot-cycle risk).

7. **coredump-staging deploy pipeline exercises W-541 fix.** Once core-dump pins bump to v0.21.1+ in TC1, the existing deploy.yml `align --strict` step IS the verification.

8. **TC2 staging blast radius is bounded.** 4 database-tier resources cascade-replaced; ~30-60 min total staging downtime. W-6 `--allow-replace=<names>` per-resource opt-in. Plan §C-1 rollback is git-revert.

9. **The 4 protected resources in coredump-staging are NAMED + DATABASE-TIER**: `core-dump-vpc, coredump-staging-pg-data, coredump-staging-pg, coredump-staging-pg-fw`. Verified against PR #190 + conformance plan §TC2 line 2839. Adversarial review #1 C-3 corrected rev1's wrong shape ("VPC + Database + Redis + App" — there is no Redis).

10. **TC2 is a SEPARATE PR** on a NEW branch `feat/c2-staging-pg-cutover-nyc1` cut from core-dump main AFTER TC1 merges. Per PR #190's operator commitment. Adversarial review #1 C-1 corrected rev1's "extend existing branch" assumption.

11. **TC1.5 is SKIPPED by operator decision** (PR #190 body + user direction 2026-05-05). Safety belt = W-6 + W-3a/b unit tests + workflow#541 fix + git-revert rollback + /healthz verification. Adversarial review #1 C-4 made this explicit.

## Rollback

Per-PR rollback paths:

- **W-541**: revert commit; fallback is the env-var stopgap (re-add `STAGING_PG_PASSWORD` and `STAGING_VPC_UUID` env exports to deploy.yml's align step — exactly what TC1 removed, restoring the workaround).
- **W-Precision**: revert commit; tests still pass at CI. Each fix is independent (#537 + #539).
- **W-Diagnose-540**: revert is no-op (test-only PR; no behavior change). Fix follow-up has independent rollback.
- **W-Cleanup**: revert BOTH `cmd/wfctl/infra_cleanup.go` (subcommand) AND `cmd/wfctl/main.go` (subcommand registration) AND `.github/workflows/conformance-smoke.yml` (cleanup-step edit) AND `docs/WFCTL.md` (command reference) AND `interfaces/iac_provider.go` (Enumerator interface addition — opt-in, no plugin coordination needed for revert). The leak-scrubber hourly job continues to clean orphans. **No plugin-stub revert required** because Enumerator is opt-in (per N1 fix) — plugins that haven't implemented it are unaffected by the revert.
- **W-Refactor**: revert commit; existing deploy_providers behavior is preserved (the refactor is interface-equivalent). ADR 010 is doc-only; revert is no-op.
- **DO-Plugin**: revert commit; v0.10.0 behavior continues; no breaking changes. EnumerateByTag-impl revert is independent of W-Cleanup (opt-in interface; if Enumerator is reverted on workflow-side, DO's implementation simply is not satisfied — no compile break, the type assertion at the cleanup subcommand call site sees `ok=false`).
- **TC1 (core-dump #190)**: git-revertible; deploy pipeline reverts to env-var stopgap form. Plan §C-1 documents this.
- **TC2 (cascade-replace)**: per W-6 `--allow-replace`, the plan output before apply shows the cascade explicitly. If the apply fails mid-cascade, infra.yaml + state in DB are still consistent (W-3a's drift postcondition + ApplyResult.ReplaceIDMap track the partial-progress state). Recovery path: `wfctl infra apply` again with the partial-applied resources skipped (or `infra apply --refresh` if drift was detected). Catastrophic recovery: git revert TC2 commit + manual cleanup of nyc1 resources + restart from pre-TC2 state.

## Top doubts (self-challenge surfaces, revised per rev3 N1+N2 fixes)

1. **`Enumerator` opt-in vs required tradeoff**: rev3 chose opt-in per workspace precedent (ProviderValidator/ProviderPlanner). Trade-off: until AWS/GCP/Azure plugins implement Enumerator, `wfctl infra cleanup --tag` against multi-provider deployments will print `skipped <provider>: no Enumerator interface` and leave that provider's tagged resources untouched (operator must clean those manually). Acceptable because: (a) DO is the primary use case (W-7 conformance gate cleanup), (b) the skip is visible in stdout, (c) per-plugin Enumerator implementations land on each plugin's own cycle without cross-plugin gate breakage. The opposite (required interface) would have forced 3 plugin-stub PRs as a chicken-and-egg blocker.

2. **#540 root cause still unknown** post-rev3. W-Diagnose-540 explicitly fails CI from the moment it lands (red CI on workflow main is the visible "needs fix" signal per N5 fix). The fix follow-up PR turns CI green. If red-CI-on-main is operationally unacceptable, the alt-shape (`t.Skip` + runbook) is documented in W-Diagnose-540 §Behavior.

3. **TC2 cascade uses direct apply** (no `--plan` file) per N2 fix. Trade-off: no operator-side persisted-plan audit trail. Mitigation: PR body captures pre/post resource IDs (operator runs `wfctl infra plan -c infra.yaml --env staging` first as stdout-only preview; copies the cascade summary into PR body before running apply).

## Decision Records

This design will create:
- **ADR 010** (W-Refactor PR): Platform-vs-provider scenario classification. Documents that 4 of 12 W-7 conformance scenarios bypass `cfg.Provider()` because they exercise platform-shared surfaces (inputsnapshot, jitsubst, wfctlhelpers). Codifies the SHIPPED pattern; doesn't introduce new pattern.

The other items don't trigger recording-decisions criteria (bug fixes / precision improvements / new subcommand / refactor that's interface-equivalent).

## Pipeline expectation

Standalone background agents per cluster. Each agent:
- Operates in its own worktree.
- Self-paces via ScheduleWakeup + Monitor + bash watchdog.
- Handles Copilot review cycle independently.
- Admin-merges per workspace memory `feedback_admin_override_pr_merge` once Copilot resolved + non-Lint CI green.
- Reports back with merge SHA + summary.

Phase 1 + Phase 3 are coordinated by the orchestrator (this session).

**Phase 2 sequencing (revised per per-attack #8 + N3)**:
- **Parallel set A**: W-Precision + DO-Plugin + W-Diagnose-540 (low Copilot surface; can run concurrent without serializing reviewer attention)
- **Parallel set B**: W-Cleanup + W-Refactor (higher Copilot surface; new opt-in interface + refactor)
- **Hard rule (per N3)**: if W-Refactor touches `docs/WFCTL.md` (e.g. for cross-references to ADR 010 or to deploy_providers refactor), it MUST rebase on W-Cleanup's merge before push. Implementer pre-flight check: `git fetch origin main && git diff HEAD..origin/main -- docs/WFCTL.md` — if non-empty, rebase before push and resolve any WFCTL.md conflicts in favor of additive sections (W-Cleanup adds `infra cleanup` command reference; W-Refactor's docs are sub-page or appended).
- W-Refactor MAY merge in parallel with W-Precision/DO-Plugin since its non-WFCTL.md file scope is disjoint.

## Workflow tag-cut sequencing (per-attack #10)

Two tag cuts: v0.21.1 (Phase 1, single PR) + v0.21.2 (Phase 3, rolls up all Phase 2 workflow PRs). Each takes ~10 min GoReleaser CI. Combining (deferring v0.21.1 until Phase 2 lands) trades wall-clock for risk: if any Phase 2 PR fails CI, ALL Phase 2 PRs block + C-1 stays blocked behind W-541 fix that's stuck in workflow main.

**Decision: keep two-tag plan.** v0.21.1 unblocks C-1 immediately (TC1 amend can proceed once v0.21.1 is live); Phase 2 + v0.21.2 close the rest. ~10 min extra wall-clock is acceptable.
