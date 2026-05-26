# 0040. V2 action lifecycle — provider compatibility expectations + Phase 2 hard-cutover

**Status:** Accepted
**Date:** 2026-05-16
**Decision-makers:** autonomous pipeline (cloud-sdk-bcd shutdown; new fresh team for Phase 2/3 execution per user mandate 2026-05-16), Jon (operator — direction 2026-05-16: "#640 is worth tracking as well")
**Related:** GoCodeAlone/workflow#640, PR #639 (v2 hooks engine landing), `docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md`, `decisions/0024-iac-typed-force-cutover.md` (no-compat-shim mandate), `feedback_force_strict_contracts_no_compat`

## Context

PR #639 landed `wfctlhelpers.ApplyPlanWithHooks` — the v2 action lifecycle hook path. wfctl persists state at each successful cloud-mutation boundary instead of waiting for whole-plan completion. The pre-existing `wfctlhelpers.ApplyPlan` is preserved as `ApplyPlanWithHooks(ctx, p, plan, ApplyPlanHooks{})` (empty-hooks path) for backwards compatibility.

#640 tracks the migration of all v1 callers to v2 + eventual removal of v1. Phase 1 (this ADR + inventory doc) defines the provider-side compatibility expectations + records the cutover constraint for Phase 2.

**Critical Phase 1 finding (verified inline 2026-05-16):** 3 of 4 IaC plugins (workflow-plugin-aws / -gcp / -azure) do NOT use the iac-codemod canonical pattern (`return wfctlhelpers.ApplyPlan(ctx, p, plan)`). Only workflow-plugin-digitalocean does. The 3 non-canonical plugins implement their own `IaCProvider.Apply` loop, dispatching per-action via `provider.ResourceDriver(...)` directly. Phase 2 v2 contract design must accommodate both implementation paths.

**Anti-pattern guard (per ADR 0024):** the strict-contracts cutover (`decisions/0024-iac-typed-force-cutover.md`) explicitly mandates "no compat shim, no build-tag dual-path" — graceful-fallback proto evolution would re-introduce the bug-class surface ADR 0024 forbids.

## Decision

**Adopt 5 normative provider-side compatibility expectations.** Plugins shipping Phase 2-conformant `IaCProviderRequiredServer.Apply` RPC MUST satisfy:

1. **Per-action success evidence.** `ApplyResponse` MUST include per-action outcome (success / error per `PlanAction`), not aggregated whole-plan result. Wire format: extend `ApplyResponse` proto with `repeated ActionResult actions`, where `ActionResult { uint32 action_index; ActionStatus status; map<string,string> output_keys; string error; }`. Required to fire wfctl-side `OnResourceApplied` hooks at correct boundary.

2. **Failed-delete preservation.** Apply MUST flag failed-delete actions distinctly (e.g., `ActionStatus = DELETE_FAILED`) so wfctl-side `OnResourceDeleted` hook does NOT fire and state is preserved. Today this is detected via `ApplyResult.Errors` array with no per-action granularity — invariant requires action-level tagging.

3. **Compensation evidence.** Apply MUST include the compensation outcome when create/replace persistence/routing fails — wfctl needs to know whether the cloud-side resource was successfully torn down (state DOESN'T leak) or whether compensation itself failed (state SHOULD be preserved with operator alert). Today this is opaque.

4. **Update-failure non-deletion.** Plugins MUST NOT pre-emptively delete existing managed resources on update failure. Engine-side already enforces this (per #639); the contract requires plugins to confirm they don't override with custom cleanup.

5. **ResourceReplacer advertisement.** Plugin manifest MUST advertise `ResourceReplacer` interface usage at the resource-type level so wfctl pre-mutation can decide whether to abort (delete-hook-active case) or allow (no-hook case). Today wfctl finds out at dispatch time, which is too late for safe rejection.

**Phase 2 ships as a coordinated PR cascade per ADR 0024.** No compat shim, no graceful proto fallback. Workflow + workflow-plugin-aws/gcp/azure/digitalocean all ship Phase 2-conformant `ApplyResponse` shape simultaneously. Plugins on workflow ≥ Phase-2-tag receive the new contract; plugins on older workflow tags are NOT supported (operator upgrades workflow + all 4 plugins together).

**Phase 3 plugin migration is bifurcated.** workflow-plugin-digitalocean (canonical-form delegate) gets codemod-driven rewrite once iac-codemod's `applyCanonicalCallExpr` constant + `rewriteApplyBody` + `isAlreadyDelegatedApplyBody` + `runAssertApplyDelegatesToHelper` AST functions are updated in lockstep (Phase 3 work). The other 3 plugins (aws/gcp/azure) need MANUAL migration since their custom Apply loops are not codemod-rewritable.

## Consequences

- **Phase 2 design pass MUST cite this ADR as a constraint.** The Phase 2 writing-plans agent MUST read ADR 0040 first and reject any compat-shim or graceful-fallback approach. Recorded explicitly here so the consequence survives team rotation per user mandate "recreate your agent team for each task."

- **Phase 2 release coordination cost is real.** Same shape as the strict-contracts cutover (referenced in `feedback_force_strict_contracts_no_compat`): 5 repos must release in coordination, each pinned to the same workflow tag. Operator workflow during the cutover: install/upgrade workflow Phase-2 tag → install/upgrade aws+gcp+azure+DO Phase-2 tags simultaneously.

- **Manifest validation gap on `cmd/wfctl/deploy_providers.go::findIaCPluginDir`** (per Phase 1 design Assumption 8) MUST be addressed in Phase 2 scope. `dispatch.go`'s own warning ("DO NOT rely on the manifest-validation guarantee in callers") means a typo in plugin's manifest `computePlanVersion` field SILENTLY falls to v1 dispatch. Phase 2 adds runtime validation.

- **Phase 3 plugin migration is bifurcated** (codemod for DO, manual for aws/gcp/azure). Phase 3 design must split into 4 sub-plans, not assume uniform codemod-fix-it run.

- **Cost of the per-action ActionResult proto field** is small (additive proto change) but the COORDINATION cost (5 repos simultaneous release) is large. Prior cloud-SDK extraction + plugin sweep precedents (this session) show the team can execute coordinated multi-repo cascades in a single autonomous pipeline run.

- **Phase 4 conformance scenario migration SHIPPED in the same PR as this ADR** (4 call sites total: 3 conformance scenarios + 1 test file in cmd/wfctl). Folded into Phase 1 because staticcheck SA1019 (from the new godoc Deprecated marker) required immediate migration. Removes the Phase 5 prerequisite gating.

- **Phase 5 ApplyPlan removal** now gates ONLY on Phase 2 + Phase 3 (since Phase 4 shipped with this PR). Plugin canonical-form propagation is the remaining blocker: DO already canonical; aws/gcp/azure must migrate to either canonical OR Phase-2-direct path.

## Alternatives rejected

- **Graceful proto-evolution fallback** — DIRECTLY contradicts ADR 0024. Would silently degrade plugin Apply outcomes to "all succeeded" when extended fields absent, exact failure mode ADR 0024 forbids.
- **Per-plugin opt-in via manifest flag** — re-introduces the dual-path bug class. ADR 0024 explicitly rejects build-tag/feature-flag dual-paths.
- **Single codemod-fix-it run across all 4 plugins** — would silently rewrite aws/gcp/azure custom Apply loops INCORRECTLY (codemod assumes canonical pattern). Phase 1 finding rules this out.
