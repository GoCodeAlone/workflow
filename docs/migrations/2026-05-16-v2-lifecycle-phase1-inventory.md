# V2 Action Lifecycle — Phase 1 Inventory

**Status:** Phase 1 complete 2026-05-16
**Tracking issue:** GoCodeAlone/workflow#640
**Phase 1 design:** `docs/plans/2026-05-16-v2-lifecycle-phase1-design.md`
**ADR:** `decisions/0040-v2-action-lifecycle-provider-compatibility.md`

## Background

PR #639 landed the v2 action lifecycle hook path (`wfctlhelpers.ApplyPlanWithHooks`) — wfctl can persist state at each successful cloud-mutation boundary instead of waiting for whole-plan completion. The pre-existing `wfctlhelpers.ApplyPlan` is preserved for backwards compatibility but is now legacy debt.

#640 tracks the multi-phase migration:
- **Phase 1 (this document)**: inventory + provider-compatibility ADR + Deprecated marker
- **Phase 2**: design + ship v2-hooks-over-gRPC contract (HARD-CUTOVER per ADR 0024)
- **Phase 3**: migrate plugins to v2 contract (per-plugin manual + codemod for canonical-form plugins)
- **Phase 4**: migrate 3 conformance scenarios from `ApplyPlan` to `ApplyPlanWithHooks`
- **Phase 5**: remove `wfctlhelpers.ApplyPlan` entirely

## Workflow-side caller inventory

### v1 `wfctlhelpers.ApplyPlan` callers (production)

| File:Line | Classification | Notes |
|-----------|----------------|-------|
| `iac/conformance/scenario_upsert_on_already_exists.go:88` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Conformance scenario; `ApplyPlanWithHooks(..., ApplyPlanHooks{})` rename suffices. **Phase 5 prerequisite** — Phase 5 removes ApplyPlan, so Phase 4 must precede. |
| `iac/conformance/scenario_delete_action.go:74` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Same |
| `iac/conformance/scenario_replace_cascade_preserves_dependents.go:92` | MIGRATE-NEEDED (Phase 4) — TRIVIAL | Same |

### iac-codemod tool references (NOT runtime callers)

| File:Line | Notes |
|-----------|-------|
| `cmd/iac-codemod/refactor_apply.go:29` (`applyCanonicalCallExpr` constant, `//nolint:unused`) | **Documentation-only constant; NOT consumed by AST rewriter.** Phase 3 lockstep update bumps this constant TOGETHER with `rewriteApplyBody` (line 1231 hardcoded `ast.NewIdent("ApplyPlan")`) + `isAlreadyDelegatedApplyBody` (line 630 hardcoded `sel.Sel.Name != "ApplyPlan"`) + `runAssertApplyDelegatesToHelper` + `refactor_apply_test.go:593`. Phase 1 does NOT touch this — bumping just the constant would create internal inconsistency. |
| `cmd/iac-codemod/refactor_apply.go:1208` (doc comment) | Same Phase 3 lockstep update |
| `cmd/iac-codemod/lint.go:54` (comment) + `lint.go:641` (matcher consumer) | Same |

### v1 `provider.Apply(ctx, &plan)` direct callers (workflow side, NOT through wfctlhelpers)

| File:Line | Classification | Notes |
|-----------|----------------|-------|
| `cmd/wfctl/infra_apply.go:486` | MIGRATE-NEEDED (Phase 2 — v1 dispatch path removal) | `else` branch sister to L473 `applyV2ApplyPlanWithHooksFn`. Selected by `DispatchVersionFor(provider) == DispatchVersionV2` predicate. v2 path uses `provider.ResourceDriver(action.Resource.Type)` per action — NOT `provider.Apply`. Phase 2 removes this v1 path entirely after all 4 plugins ship Phase 2-conformant Apply RPC + manifest declaration. |
| `cmd/wfctl/infra_apply.go:1615` | MIGRATE-NEEDED (Phase 2) | Same — second occurrence (likely refresh path). |
| `cmd/wfctl/iac_typed_adapter.go:350` | NOT IN V2 HOT PATH (Phase 2 wire-format work) | `typedIaCAdapter.Apply` is called ONLY on the v1 dispatch path. Per-action wire format change happens in `applyResultFromPB` (the typed adapter's response decoder) when Phase 2 extends `ApplyResponse` proto with per-action `Actions []ActionResult` field. Adapter dispatch shape doesn't need to change; response decoder does. |

## Plugin-side IaCProvider.Apply implementation inventory (verified 2026-05-16 via `gh api`)

| Plugin | File:Line | Pattern | Phase 3 path |
|--------|-----------|---------|--------------|
| workflow-plugin-aws | `provider/provider.go:237` `AWSProvider.Apply` | **NON-CANONICAL** — own loop with `p.mu.RLock` + init check + custom dispatch | **MIGRATE-NEEDED (Phase 3 MANUAL)** — codemod cannot rewrite |
| workflow-plugin-gcp | `provider/provider.go:226` `GCPProvider.Apply` | **NON-CANONICAL** — own `for _, action := range plan.Actions` with `p.ResourceDriver(action.Resource.Type)` per-action | **MIGRATE-NEEDED (Phase 3 MANUAL)** — codemod cannot rewrite |
| workflow-plugin-azure | `internal/provider.go:138` `AzureProvider.Apply` | **NON-CANONICAL** — own loop with `p.mu.RLock` | **MIGRATE-NEEDED (Phase 3 MANUAL)** — codemod cannot rewrite |
| workflow-plugin-digitalocean | `internal/provider.go:274-275` `DOProvider.Apply` | **CANONICAL** — `result, err := wfctlhelpers.ApplyPlan(ctx, p, plan)` delegate (with custom post-flush wrapper) | LEAVE-AS-IS for Phase 1; Phase 3 codemod CAN rewrite (after AST functions updated in lockstep with constant) |

## Major architectural finding

**3 of 4 IaC plugins do NOT use the iac-codemod canonical pattern (`return wfctlhelpers.ApplyPlan(ctx, p, plan)`).** The canonical-form constant in `cmd/iac-codemod/refactor_apply.go:29` is aspirational, not reality.

Phase 2 + Phase 3 implications:
1. **Phase 3 is NOT a single codemod-fix-it run.** 3 plugins need MANUAL migration; 1 plugin (DO) can be codemod-rewritten.
2. **Phase 2 v2 hooks contract design** must accommodate two plugin implementation paths:
   - **(a) Canonical delegate**: `provider.Apply` → `wfctlhelpers.ApplyPlan(ctx, p, plan)` → `applyPlanWithEnvProviderAndHooks(ctx, p, plan, nil, hooks)` — wfctl-side hooks fire automatically at each `dispatchAction` boundary
   - **(b) Custom loop**: `provider.Apply` runs its own `for _, action := range plan.Actions` loop, calling `p.ResourceDriver(action.Resource.Type)` per-action. The plugin must EMIT per-action outcome via the Phase 2 extended `ApplyResponse` proto so wfctl-side reconstructs the hook events
3. **Phase 2 contract MUST be a hard-cutover per ADR 0024.** No graceful proto fallback — workflow + 4 plugin repos ship the new ApplyResponse shape simultaneously, same coordination pattern as the strict-contracts cutover.

## Provider compatibility expectations (Phase 2 contract preview)

Per `decisions/0040-v2-action-lifecycle-provider-compatibility.md`, plugins MUST satisfy these 5 invariants at the IaCProviderRequiredServer.Apply RPC boundary:

1. **Per-action success evidence** — ApplyResponse MUST include per-action outcome (success/error per `PlanAction`)
2. **Failed-delete preservation** — Apply MUST flag failed-delete actions distinctly so wfctl `OnResourceDeleted` does NOT fire
3. **Compensation evidence** — Apply MUST include compensation outcome when create/replace persistence/routing fails
4. **Update-failure non-deletion** — Plugins MUST NOT pre-emptively delete on update failure (engine-side enforced; plugin must not override)
5. **ResourceReplacer advertisement** — Plugin manifest MUST advertise ResourceReplacer usage so wfctl pre-mutation gates correctly

## Out of scope for Phase 1

- Phase 2 gRPC contract design + implementation — separate design pass
- Phase 3 plugin migration — separate per-plugin design + execution passes
- Phase 4 conformance scenario migration — separate trivial PR
- Phase 5 ApplyPlan removal — gates on Phase 4 completion + plugin canonical-form propagation

## References

- ADR 0024 (`decisions/0024-iac-typed-force-cutover.md`) — strict-contracts cutover precedent (no compat shim)
- ADR 0040 (`decisions/0040-v2-action-lifecycle-provider-compatibility.md`) — provider-side compatibility contract
- PR #639 — v2 hooks engine landing
- `iac/wfctlhelpers/apply.go` — engine source (with `// Deprecated:` marker on `ApplyPlan` per Phase 1)
- `iac/wfctlhelpers/doc.go` — in-package contract pointer
- Prior product design: `docs/plans/2026-04-25-wfctl-lifecycle-product-design.md`
