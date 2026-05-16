# V2 Action Lifecycle — Phase 1 Inventory + Provider Compatibility Expectations — Design

**Status:** Draft
**Date:** 2026-05-16
**Operator:** Jon (autonomous-mode mandate 2026-05-16: "continue with follow-ups, you'll probably need a new brainstorm/design pass before implementation to ensure the accuracy of your plans. continue autonomously" + "#640 is worth tracking as well")
**Tracking issue:** GoCodeAlone/workflow#640
**Related:** PR #639 (v2 hook path landed), `iac/wfctlhelpers/apply.go` (engine), `interfaces/iac.go` (IaCProvider), `docs/plans/2026-04-25-wfctl-lifecycle-product-design.md` (prior product design)

## Goal

Phase 1 of #640. Produce a docs-only deliverable that:
1. Inventories every v1 ApplyPlan caller across workflow + all IaC plugins (4 repos: aws/gcp/azure/DO).
2. Classifies each caller as MIGRATE-NEEDED (must adopt v2 hooks for #640's invariants) or KEEP-ON-V1 (legitimate no-hook caller).
3. Defines provider compatibility expectations for v2 — what plugins must do at the gRPC IaCProvider.Apply boundary so wfctl-side hooks fire correctly.
4. Lands an ADR recording the per-caller classification + the provider-side expectation contract.

**Explicitly out of scope for Phase 1** (deferred to Phase 2-5 design passes):
- Phase 2: design + ship the v2-hooks-over-gRPC contract (extending `IaCProviderRequiredServer.Apply` to surface action-boundary success evidence)
- Phase 3: migrate plugins to emit hooks-aware Apply responses
- Phase 4: migrate the 3 conformance scenarios that use v1 ApplyPlan
- Phase 5: deprecate + remove v1 ApplyPlan

Phase 1 is the keystone — no engine code changes, just analysis + docs + ADR.

## Architecture

Single-repo (workflow), 1-PR docs-only deliverable. Three artifacts:
- `docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md` — the inventory document (per-caller table + classification rationale)
- `decisions/0040-v2-action-lifecycle-provider-compatibility.md` — ADR recording the provider-side compatibility contract decision
- `iac/wfctlhelpers/doc.go` (or similar) — Go doc.go file embedding a SHORT version of the inventory + provider-expectation pointer to the migration doc, so future contributors discover the contract in-package

No production code changes. No tests beyond `go vet` confirming the doc.go compiles.

## Inventory (preliminary — verified during Task 1)

**v1 ApplyPlan callers in workflow (origin/main since c68a56cc):**

| # | File:Line | Classification | Rationale |
|---|-----------|----------------|-----------|
| 1 | `cmd/iac-codemod/refactor_apply.go:29` (`applyCanonicalCallExpr` constant) | TOOL — not a runtime caller | iac-codemod's canonical-form constant for AST-rewriting providers' IaCProvider.Apply method bodies. Phase 1 decision: KEEP for now; Phase 2 design decides if canonical form switches to ApplyPlanWithHooks |
| 2 | `cmd/iac-codemod/refactor_apply.go:1208` (doc comment) | DOC | Same |
| 3 | `cmd/iac-codemod/lint.go:54, 641` (lint matchers) | TOOL — lint matcher | Same — Phase 2 decision |
| 4 | `iac/conformance/scenario_upsert_on_already_exists.go:88` | KEEP-ON-V1 | Conformance scenario verifying provider behavior; doesn't need state-persistence hooks. Run in isolation; no external state. Decision: KEEP. (Phase 4 may revisit if conformance gains hook-coverage scenarios.) |
| 5 | `iac/conformance/scenario_delete_action.go:74` | KEEP-ON-V1 | Same — delete-action behavior verification, doesn't need OnResourceDeleted hook fired |
| 6 | `iac/conformance/scenario_replace_cascade_preserves_dependents.go:92` | KEEP-ON-V1 | Same — replace-cascade verification |

**v1 callers in workflow via `provider.Apply` (NOT wfctlhelpers.ApplyPlan):**

| # | File:Line | Classification | Rationale |
|---|-----------|----------------|-----------|
| 7 | `cmd/wfctl/infra_apply.go:486` | MIGRATE-NEEDED (Phase 2) | wfctl's secondary apply path — calls `provider.Apply(ctx, &plan)` directly (bypasses wfctlhelpers entirely). Sister branch to L473 which uses ApplyPlanWithHooks. Picked when v2 hooks-over-gRPC isn't yet supported. Phase 2 needs to make this path hooks-aware. |
| 8 | `cmd/wfctl/infra_apply.go:1615` | MIGRATE-NEEDED (Phase 2) | Same as above — second occurrence in the same file. |
| 9 | `cmd/wfctl/iac_typed_adapter.go:350` | MIGRATE-NEEDED (Phase 2) | Typed-strict-contracts adapter calling gRPC `IaCProviderRequiredServer.Apply`. The Apply gRPC contract itself doesn't surface action-boundary hook events today — Phase 2 must extend the contract. |

**v1 callers in IaC plugin repos (per ADR 0034 cross-repo inventory — verified during Task 1):**

| # | Plugin | File | Caller pattern | Classification |
|---|--------|------|----------------|----------------|
| 10 | workflow-plugin-aws | `internal/provider.go` (or similar) — IaCProvider.Apply impl | Canonical `return wfctlhelpers.ApplyPlan(ctx, p, plan)` | KEEP-ON-V1-FOR-NOW (Phase 2 contract decides) |
| 11 | workflow-plugin-gcp | Same pattern | Same | Same |
| 12 | workflow-plugin-azure | Same pattern | Same | Same |
| 13 | workflow-plugin-digitalocean | Same pattern | Same | Same |

(Plugin inventory verified via per-plugin grep during Task 1 — table populated with actual file:line.)

## Provider compatibility expectations (Phase 1 ADR scope)

Document what plugins MUST do at the IaCProviderRequiredServer.Apply boundary so wfctl-side v2 hooks fire correctly. This is the contract Phase 2 will implement.

**Required behaviors (per #640's 5 invariants):**

1. **Per-action success evidence:** Apply RPC response MUST include per-action outcome (success / error per `PlanAction`), not just whole-plan result. Today `ApplyResponse` likely returns `ApplyResult` with aggregated `Errors []` field — invariant #1 requires per-action granularity for the wfctl-side `OnResourceApplied` hook to fire correctly. Phase 2 contract decision: add `Actions []ActionResult` to `ApplyResponse` proto, where each ActionResult has `{action_index, status, output_keys, error?}`.

2. **Failed-delete must NOT prune state:** Apply RPC response MUST flag failed-delete actions distinctly so wfctl's `OnResourceDeleted` hook does not fire (and state is preserved). Today this is detected via `ApplyResult.Errors` array but with no per-action granularity — invariant #2 requires action-level tagging.

3. **Compensation evidence on create/replace failure:** Apply RPC response MUST include the COMPENSATION result when create/replace persistence/routing fails — wfctl needs to know whether the cloud-side resource was successfully torn down (so state DOESN'T leak) or whether compensation itself failed (so state SHOULD leak with operator alert). Today this is opaque.

4. **Update-failure DOES NOT delete:** This is engine-side behavior (already enforced by ApplyPlanWithHooks per #639). Phase 2 contract just needs to confirm plugins don't override this behavior with their own pre-emptive cleanup.

5. **Provider-owned-replace gating:** Provider's `ResourceReplacer` interface usage MUST be advertised in plugin manifest so wfctl can decide pre-mutation whether to abort (delete-hook-active case) or allow (no-hook case). Today provider just implements ResourceReplacer; wfctl finds out at dispatch time.

**ADR 0040 records:** these 5 expectations are normative; Phase 2 implements; plugins ship updated `IaCProviderRequiredServer.Apply` per the contract.

## Self-challenge round (top doubts)

1. **Is "Phase 1 = docs-only" actually useful or theater?** Real risk: docs that don't bind future work get ignored. Mitigation: the ADR creates load-bearing precedent that Phase 2 design references; future plans that diverge from the ADR's classification need ADR amendment (per `decisions/` discipline). Plus: doc.go in iac/wfctlhelpers/ embeds the contract in-package so contributors discover it during normal code reading, not by digging into docs/.

2. **Per-action gRPC contract extension is the LARGE work, not the inventory.** Phase 2 is ~10× the scope of Phase 1. Risk: spending pipeline cycles on Phase 1 trivializes the actual hard work. Counter: Phase 2 design needs Phase 1's classification table to know WHICH plugins ship updated Apply gRPC vs which delegate; without inventory, Phase 2 either over-scopes (ship to all) or under-scopes (ship to wrong ones). Phase 1 IS prerequisite.

3. **The 3 conformance scenarios might be "KEEP-ON-V1" prematurely.** If conformance gains delete-state-pruning verification scenarios, those WILL need v2 hooks. Decision recorded as "KEEP-ON-V1 for current scenarios; revisit during Phase 4 if scenario coverage expands."

## Assumptions

1. **iac-codemod's `applyCanonicalCallExpr` is the canonical pattern every plugin uses to delegate Apply to the engine.** Verified by grepping the constant value in `cmd/iac-codemod/refactor_apply.go:29`. If false, plugin-side migration may diverge per-plugin instead of being a single AST rewrite.

2. **All 4 IaC plugins (aws/gcp/azure/DO) implement `IaCProvider.Apply` via the canonical delegation form.** Verified during Task 1 via per-repo grep. If a plugin has a non-canonical implementation, it gets called out in the inventory table + adds a Phase 2 sub-task.

3. **`docs/plans/2026-04-25-wfctl-lifecycle-product-design.md` is the prior-product-design source of truth.** Phase 1 doc.go should cite it. If it's stale or contradicts #639's invariants, Phase 1 surfaces the conflict for resolution.

4. **No external (non-GoCodeAlone-org) consumers depend on `wfctlhelpers.ApplyPlan` being exported.** This is a workflow-internal helper; external consumers go through `IaCProvider.Apply` (gRPC). If false, deprecation in Phase 5 needs cross-org coordination.

5. **`IaCProviderRequiredServer.Apply` proto can be extended in a backwards-compatible way (add fields to `ApplyResponse` rather than replace).** Standard proto evolution rule — old plugins continue working; new wfctl reads new fields if present, falls back to legacy aggregation if absent. If the proto needs a breaking change, Phase 2 scope grows substantially.

6. **The 3 conformance scenarios using v1 ApplyPlan don't need state-persistence semantics.** Read of the scenario code in Task 1 confirms (or refutes) — if a scenario actually verifies hook-fired behavior implicitly, classification flips to MIGRATE-NEEDED.

7. **`cmd/wfctl/infra_apply.go:486 + :1615` (provider.Apply direct calls) are the SECOND-PATH dual to the v2 ApplyPlanWithHooks at L473 + L1557 — picked by some condition (probably "is plugin v2-hooks-aware?" check).** Verified during Task 1 by reading the if/else context. If the dual-path is more complex (e.g., feature-flagged, fallback-on-error), classification is more nuanced.

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
