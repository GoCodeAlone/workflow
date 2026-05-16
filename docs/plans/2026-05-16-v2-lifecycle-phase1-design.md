# V2 Action Lifecycle — Phase 1 Inventory + Provider Compatibility Expectations — Design

**Status:** Draft
**Date:** 2026-05-16
**Operator:** Jon (autonomous-mode mandate 2026-05-16: "continue with follow-ups, you'll probably need a new brainstorm/design pass before implementation to ensure the accuracy of your plans. continue autonomously" + "#640 is worth tracking as well")
**Tracking issue:** GoCodeAlone/workflow#640
**Related:** PR #639 (v2 hook path landed), `iac/wfctlhelpers/apply.go` (engine), `interfaces/iac.go` (IaCProvider), `docs/plans/2026-04-25-wfctl-lifecycle-product-design.md` (prior product design)

## Goal

Phase 1 of #640. Produces an analysis-and-deprecation-signal deliverable:
1. Inventories every v1 ApplyPlan caller across workflow + all IaC plugins (4 repos: aws/gcp/azure/DO) — verified via per-repo grep at Task 0 (NOT speculative).
2. Classifies each caller as MIGRATE-NEEDED (must adopt v2 hooks for #640's invariants) or LEAVE-AS-IS (no semantic difference; trivial empty-hooks rename suffices).
3. Defines provider compatibility expectations for v2 — what plugins must do at the gRPC IaCProvider.Apply boundary so wfctl-side hooks fire correctly.
4. Lands ADR 0040 recording the per-caller classification, the provider-side expectation contract, **and the consequence that Phase 2 is a coordinated hard-cutover per ADR 0024 (no compat-shim path).**
5. **Adds `// Deprecated: use ApplyPlanWithHooks` godoc marker to `ApplyPlan` in `iac/wfctlhelpers/apply.go`** — surfaced by `gopls`/`staticcheck` to every caller; mechanically delivers #640's "Add deprecation warnings" milestone.
6. **Updates `iac-codemod`'s `applyCanonicalCallExpr` constant from `wfctlhelpers.ApplyPlan(ctx, p, plan)` to `wfctlhelpers.ApplyPlanWithHooks(ctx, p, plan, wfctlhelpers.ApplyPlanHooks{})`** — codemod becomes the migration driver for Phase 3 plugin AST rewrites.

**Explicitly out of scope for Phase 1** (deferred to Phase 2-5 design passes):
- Phase 2: design + ship the v2-hooks-over-gRPC contract — must be a HARD-CUTOVER coordinated PR cascade across workflow + 4 plugin repos per ADR 0024 (no compat shim, no graceful fallback)
- Phase 3: migrate plugins to emit hooks-aware Apply responses (per-repo PRs); Phase 1's iac-codemod canonical-form bump is the lever
- Phase 4: migrate the 3 conformance scenarios from `ApplyPlan(...)` to `ApplyPlanWithHooks(..., ApplyPlanHooks{})` (trivial mechanical rename; HARD PREREQUISITE for Phase 5 since Phase 5 removes ApplyPlan)
- Phase 5: remove `wfctlhelpers.ApplyPlan` entirely (after Phase 4 + plugin canonical-form propagation)

Phase 1 ships engine docs + the deprecation marker + the codemod canonical-form bump. Light mechanical changes; no runtime behavior change.

## Architecture

Single-repo (workflow), 1-PR deliverable. Five artifacts:
- `docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md` — the inventory document (per-caller table + classification rationale)
- `decisions/0040-v2-action-lifecycle-provider-compatibility.md` — ADR recording: (a) the provider-side compatibility contract; (b) the explicit consequence that Phase 2 is a coordinated hard-cutover per ADR 0024
- `iac/wfctlhelpers/doc.go` — Go doc.go file embedding a SHORT version of the inventory + provider-expectation pointer to the migration doc
- **`iac/wfctlhelpers/apply.go` — add `// Deprecated: use ApplyPlanWithHooks` godoc to `ApplyPlan` (line 78); add same to `applyPlanWithEnvProvider` (line 103, internal but exported-styled) only if accessible**
- **`cmd/iac-codemod/refactor_apply.go:29` — bump `applyCanonicalCallExpr` constant from `"wfctlhelpers.ApplyPlan(ctx, p, plan)"` to `"wfctlhelpers.ApplyPlanWithHooks(ctx, p, plan, wfctlhelpers.ApplyPlanHooks{})"`. Plus matching update in `cmd/iac-codemod/lint.go:54` lint matcher.**

Production code changes are minimal: 1 godoc comment + 1 constant string + 1 lint matcher string + same-file doc.go. Build clean + iac-codemod tests verify the canonical-form change doesn't break the AST rewriter.

No runtime behavior change. The deprecation marker surfaces through static analysis; the codemod constant bump only affects future codemod-driven rewrites (operator-triggered).

## Inventory (preliminary — verified during Task 1)

**v1 ApplyPlan callers in workflow (origin/main since c68a56cc):**

| # | File:Line | Classification | Rationale |
|---|-----------|----------------|-----------|
| 1 | `cmd/iac-codemod/refactor_apply.go:29` (`applyCanonicalCallExpr` constant) | TOOL — not a runtime caller | iac-codemod's canonical-form constant for AST-rewriting providers' IaCProvider.Apply method bodies. Phase 1 decision: KEEP for now; Phase 2 design decides if canonical form switches to ApplyPlanWithHooks |
| 2 | `cmd/iac-codemod/refactor_apply.go:1208` (doc comment) | DOC | Same |
| 3 | `cmd/iac-codemod/lint.go:54, 641` (lint matchers) | TOOL — lint matcher | Same — Phase 2 decision |
| 4 | `iac/conformance/scenario_upsert_on_already_exists.go:88` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Calls `wfctlhelpers.ApplyPlan(ctx, p, plan)`. ApplyPlan is `ApplyPlanWithHooks(ctx, p, plan, ApplyPlanHooks{})` — empty-hooks rename has zero semantic difference. **HARD PREREQUISITE for Phase 5 (Phase 5 removes ApplyPlan; if scenarios still call it, build breaks).** Phase 4 mechanical work: rename 3 call sites. |
| 5 | `iac/conformance/scenario_delete_action.go:74` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Same |
| 6 | `iac/conformance/scenario_replace_cascade_preserves_dependents.go:92` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Same |

**v1 callers in workflow via `provider.Apply` (NOT wfctlhelpers.ApplyPlan):**

| # | File:Line | Classification | Rationale |
|---|-----------|----------------|-----------|
| 7 | `cmd/wfctl/infra_apply.go:486` | MIGRATE-NEEDED (Phase 2 — v1 dispatch path) | wfctl's v1 dispatch path — calls `provider.Apply(ctx, &plan)` directly. Sister `else` branch to L473 (v2 path uses `applyV2ApplyPlanWithHooksFn` which internally dispatches via `provider.ResourceDriver(action.Resource.Type)` per action — NOT through `provider.Apply`). Branch selection: `DispatchVersionFor(provider) == DispatchVersionV2` (or similar `dispatch.go` predicate). Phase 2 removes this v1 path entirely after all 4 plugins ship Phase 2-conformant Apply RPC + manifest declaration. |
| 8 | `cmd/wfctl/infra_apply.go:1615` | MIGRATE-NEEDED (Phase 2) | Same as above — second occurrence (likely refresh path). |
| 9 | `cmd/wfctl/iac_typed_adapter.go:350` | NOT IN V2 HOT PATH (Phase 2 wire-format work) | `typedIaCAdapter.Apply` is called ONLY on the v1 dispatch path (Item #7 above). Per-action wire format change happens in `applyResultFromPB` (the typed adapter's response decoder) when Phase 2 extends `ApplyResponse` proto with per-action `Actions []ActionResult` field. The adapter's Apply RPC dispatch shape doesn't need to change; the response decoder does. **Reclassified from cycle 1 to fix architectural misunderstanding** — v2 dispatch is per-resource via ResourceDriver, not per-plan via provider.Apply. |

**v1 callers in IaC plugin repos (per ADR 0034 cross-repo inventory):**

**Task 0 (PRE-PLAN-AUTHORING)**: actually grep the 4 plugin repos via `gh api repos/GoCodeAlone/<plugin>/contents/...` or local clone to populate this table with REAL file:line. The cycle 1 reviewer correctly flagged the bootstrap problem: design + ADR should not commit speculative entries. Task 0 result template:

| # | Plugin | File:Line | Caller pattern | Classification |
|---|--------|-----------|----------------|----------------|
| 10 | workflow-plugin-aws | (Task 0 result) | Canonical via iac-codemod | LEAVE-AS-IS until Phase 3 codemod-driven rewrite |
| 11 | workflow-plugin-gcp | (Task 0 result) | Same | Same |
| 12 | workflow-plugin-azure | (Task 0 result) | Same | Same |
| 13 | workflow-plugin-digitalocean | (Task 0 result) | Same | Same |

If Task 0 finds a plugin with a NON-canonical Apply implementation (e.g., custom logic that doesn't delegate to wfctlhelpers), that plugin's row gets `MIGRATE-NEEDED (Phase 3 manual)` and the plan grows by 1 task. Otherwise Phase 3 is a single iac-codemod-fix-it run across all 4 plugins after Phase 2 ships.

## Provider compatibility expectations (Phase 1 ADR scope)

Document what plugins MUST do at the IaCProviderRequiredServer.Apply boundary so wfctl-side v2 hooks fire correctly. This is the contract Phase 2 will implement.

**Required behaviors (per #640's 5 invariants):**

1. **Per-action success evidence:** Apply RPC response MUST include per-action outcome (success / error per `PlanAction`), not just whole-plan result. Today `ApplyResponse` likely returns `ApplyResult` with aggregated `Errors []` field — invariant #1 requires per-action granularity for the wfctl-side `OnResourceApplied` hook to fire correctly. Phase 2 contract decision: add `Actions []ActionResult` to `ApplyResponse` proto, where each ActionResult has `{action_index, status, output_keys, error?}`.

2. **Failed-delete must NOT prune state:** Apply RPC response MUST flag failed-delete actions distinctly so wfctl's `OnResourceDeleted` hook does not fire (and state is preserved). Today this is detected via `ApplyResult.Errors` array but with no per-action granularity — invariant #2 requires action-level tagging.

3. **Compensation evidence on create/replace failure:** Apply RPC response MUST include the COMPENSATION result when create/replace persistence/routing fails — wfctl needs to know whether the cloud-side resource was successfully torn down (so state DOESN'T leak) or whether compensation itself failed (so state SHOULD leak with operator alert). Today this is opaque.

4. **Update-failure DOES NOT delete:** This is engine-side behavior (already enforced by ApplyPlanWithHooks per #639). Phase 2 contract just needs to confirm plugins don't override this behavior with their own pre-emptive cleanup.

5. **Provider-owned-replace gating:** Provider's `ResourceReplacer` interface usage MUST be advertised in plugin manifest so wfctl can decide pre-mutation whether to abort (delete-hook-active case) or allow (no-hook case). Today provider just implements ResourceReplacer; wfctl finds out at dispatch time.

**ADR 0040 records:** these 5 expectations are normative; Phase 2 implements; plugins ship updated `IaCProviderRequiredServer.Apply` per the contract.

## Self-challenge round + cycle 1 adversarial findings (addressed in revision)

**Cycle 1 adversarial-design-review findings — all addressed:**

- **C-1 (Assumption 5 contradicts ADR 0024):** REVOKED Assumption 5; replaced with "Phase 2 is hard-cutover per ADR 0024" recorded as ADR 0040 consequence + scope expansion documented.
- **C-2 (typed_adapter.Apply misclassified):** Fixed Item #9 reclassification — typed_adapter.Apply is in v1 hot path only; v2 dispatch via ResourceDriver. Phase 2 wire-format work happens in `applyResultFromPB` decoder, not adapter dispatch.
- **I-1 (KEEP-ON-V1 conformance scenarios block Phase 5):** Reclassified all 3 scenarios as MIGRATE-NEEDED (Phase 4) — TRIVIAL. Empty-hooks rename. Phase 5 prerequisite documented.
- **I-2 (plugin inventory bootstrap problem):** Added Task 0 (PRE-PLAN-AUTHORING grep across 4 plugin repos) before ADR commits speculative file:line. Plugin rows show `(Task 0 result)` placeholders that the actual Task 0 work fills in.
- **I-3 (manifest-validation gap on deploy_providers path):** New Assumption 8 records the silent-v1-fallback risk; Phase 2 scope includes adding the validation gate.
- **Minor m-1 (ADR number gap):** Verified at execution: 0039 is highest; 0040 is correct.
- **Minor m-2 (KEEP-ON-V1 terminology confusion):** Term replaced by MIGRATE-NEEDED-TRIVIAL / LEAVE-AS-IS in the table.
- **Minor m-3 (4th doubt missing):** Added: "Phase 2 hard-cutover coordination cost" — surfaced in revoked-Assumption-5 + Phase 2 scope statement.

**Top remaining doubts:**

1. **Phase 1's mechanical scope (godoc Deprecated marker + iac-codemod constant bump) is correct only if the codemod's existing call sites in `cmd/iac-codemod/refactor_apply.go:1208` + `cmd/iac-codemod/lint.go:54+641` (doc/comment/lint refs) update consistently.** Risk: missing one of those leaves the codemod inconsistent. Mitigation: Task 2 (codemod constant bump) explicitly enumerates ALL `applyCanonicalCallExpr` references for the rewrite.

2. **Phase 4 conformance scenario migration is technically TRIVIAL but discovery-coupled to Phase 5 (must precede ApplyPlan removal).** Phase 4 + Phase 5 should be planned together, OR Phase 4 ships standalone now (it's a 6-line change across 3 files). Decision: defer — Phase 4 design pass (separate followup) decides; Phase 1's contribution is recording the dependency clearly.

3. **The `iac-codemod` canonical-form bump might break already-rewritten plugins** — if a plugin already shipped using the OLD canonical form (`wfctlhelpers.ApplyPlan(ctx, p, plan)`), running the new codemod against it would change the call to `wfctlhelpers.ApplyPlanWithHooks(ctx, p, plan, wfctlhelpers.ApplyPlanHooks{})`. That's actually the desired behavior — but the codemod must be idempotent (re-running against already-bumped code shouldn't keep adding ApplyPlanHooks{} args). Verification: Task 2 includes idempotency test.

## Assumptions

1. **iac-codemod's `applyCanonicalCallExpr` is the canonical pattern every plugin uses to delegate Apply to the engine.** Verified by grepping the constant value in `cmd/iac-codemod/refactor_apply.go:29`. If false, plugin-side migration may diverge per-plugin instead of being a single AST rewrite.

2. **All 4 IaC plugins (aws/gcp/azure/DO) implement `IaCProvider.Apply` via the canonical delegation form.** Verified during Task 1 via per-repo grep. If a plugin has a non-canonical implementation, it gets called out in the inventory table + adds a Phase 2 sub-task.

3. **`docs/plans/2026-04-25-wfctl-lifecycle-product-design.md` is the prior-product-design source of truth.** Phase 1 doc.go should cite it. If it's stale or contradicts #639's invariants, Phase 1 surfaces the conflict for resolution.

4. **No external (non-GoCodeAlone-org) consumers depend on `wfctlhelpers.ApplyPlan` being exported.** This is a workflow-internal helper; external consumers go through `IaCProvider.Apply` (gRPC). If false, deprecation in Phase 5 needs cross-org coordination.

5. **REVOKED — Phase 2 is a HARD-CUTOVER per ADR 0024.** Cycle-1 adversarial review correctly flagged: a "graceful proto fallback" approach is exactly the compat-shim ADR 0024 forbids ("preserves the bug-class surface"). The CONSEQUENCE recorded in ADR 0040: **Phase 2 ships as a coordinated PR cascade across workflow + 4 plugin repos, all in same release window** — same shape as the original strict-contracts cutover (referenced in `feedback_force_strict_contracts_no_compat`). No graceful degradation path. wfctl Phase 2 release must require all 4 plugins ship Phase 2-conformant Apply RPC simultaneously. This expands Phase 2 scope but eliminates the silent-fallback failure mode.

6. **The 3 conformance scenarios using v1 ApplyPlan don't need state-persistence semantics.** Read of the scenario code in Task 1 confirms (or refutes) — if a scenario actually verifies hook-fired behavior implicitly, classification flips to MIGRATE-NEEDED.

7. **`cmd/wfctl/infra_apply.go:486 + :1615` (provider.Apply direct calls) are the v1 dispatch dual to the v2 ApplyPlanWithHooks at L473 + L1557 — selected by `DispatchVersionFor(provider) == DispatchVersionV2`** (see `dispatch.go`). Verified during Task 0/1.

8. **`computePlanVersion` manifest field has no runtime validation gate on the `cmd/wfctl/deploy_providers.go::findIaCPluginDir` path** — uses `json.Unmarshal` without schema validation. Per dispatch.go's own warning ("DO NOT rely on the manifest-validation guarantee in callers"), a typo in a plugin's manifest `computePlanVersion` field SILENTLY falls to v1 dispatch. Phase 2 scope must include adding manifest validation on the deploy_providers path to prevent silent-v1-fallback. Recorded in ADR 0040 as a Phase 2 prerequisite.

## Tech Stack

- Markdown (inventory doc + ADR)
- Go doc.go (in-package documentation embedding)
- No production code changes; no tests beyond `go build ./... && go vet ./...`

## Base branch

`main` (workflow repo only — Phase 1 is workflow-side docs-only)

## Rollback

Trivial: revert the docs PR. No runtime side effects. ADR 0040 deletion via PR (stays in git history per ADR convention but loses Accepted status).

## Decisions to record

ADR 0040 — "v2 action lifecycle provider compatibility expectations":
- Status: Accepted
- Context: PR #639 landed v2 hooks engine-side; #640 tracks migration; provider-side contract not yet defined
- Decision: 5 normative expectations (per-action success evidence, failed-delete-no-prune, compensation evidence, update-failure-no-delete, provider-owned-replace-advertised)
- Consequences: Phase 2 implements; plugins must ship updated Apply gRPC per contract; wfctl can route accordingly

## Out of scope (intentional non-goals — separate future design passes)

- Phase 2: design + ship `IaCProviderRequiredServer.Apply` proto extension carrying action-boundary outcomes
- Phase 3: migrate plugins to emit v2-aware Apply responses (per-repo PRs)
- Phase 4: migrate the 3 conformance scenarios (if needed per Phase 4 evaluation)
- Phase 5: deprecate `wfctlhelpers.ApplyPlan` (log warning on call) → remove after compatibility window

## Memory updates (post-Phase-1 execution)

Update MEMORY.md / `project_cloud_sdk_extraction_complete.md` Deferred section: mark "#640 v2 action lifecycle Phase 1" COMPLETE; flag Phase 2 (gRPC contract extension) as the next #640 design pass.
