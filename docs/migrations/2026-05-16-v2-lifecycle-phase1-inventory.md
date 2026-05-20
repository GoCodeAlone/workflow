# V2 Action Lifecycle — Migration Inventory And Final State

**Status:** Closed 2026-05-20 after workflow#699 and workflow#743.
**Tracking issue:** GoCodeAlone/workflow#640
**Phase 1 design:** `docs/plans/2026-05-16-v2-lifecycle-phase1-design.md`
**ADR:** `decisions/0040-v2-action-lifecycle-provider-compatibility.md`

## Background

PR #639 introduced `wfctlhelpers.ApplyPlanWithHooks`, allowing wfctl to persist
state at each successful cloud-mutation boundary instead of waiting for
whole-plan completion. The migration is now complete: `wfctlhelpers.ApplyPlan`
has been removed, the v1 `provider.Apply` dispatch path was removed in
workflow#699, and `ApplyPlanWithHooks` is the only wfctl-side plan-execution
helper.

## Shipped Phases

| Phase | Final state |
| --- | --- |
| Phase 1 | Inventory, provider-compatibility ADR, and deprecation marker shipped 2026-05-16. |
| Phase 2 | Typed lifecycle hard-cutover shipped in workflow#699; loaders reject non-v2 providers. |
| Phase 3 | Plugin migrations shipped as part of the v2 lifecycle cutover. |
| Phase 4 | Conformance and in-tree test callers migrated to `ApplyPlanWithHooks(..., ApplyPlanHooks{})`. |
| Phase 5 | workflow#743 removed the legacy `wfctlhelpers.ApplyPlan` wrapper and cleaned remaining helper callers. |

## Final Caller Inventory

| Surface | Final state |
| --- | --- |
| `wfctlhelpers.ApplyPlan` | Removed. No production or test callers remain. |
| `wfctlhelpers.ApplyPlanWithHooks` | Sole exported wfctl-side helper for v2 IaC plan execution. |
| `cmd/wfctl` provider dispatch | Uses the strict typed v2 lifecycle path; legacy `provider.Apply` dispatch was removed in workflow#699. |
| `cmd/iac-codemod` | Removed in workflow#699 because plugin-side `Apply` implementations no longer exist in the runtime contract. |
| Plugin capability gate | `CapabilitiesResponse.compute_plan_version` must be `"v2"`; empty, `"v1"`, and unrecognized values are rejected at load time. |

## Compatibility Outcome

Per `decisions/0040-v2-action-lifecycle-provider-compatibility.md`, providers
must satisfy the v2 lifecycle invariants at the typed IaCProvider RPC boundary:

1. Per-action success evidence is present for hook replay.
2. Failed deletes do not trigger `OnResourceDeleted`.
3. Compensation evidence is available when persistence/routing fails.
4. Update failures are not treated as delete success.
5. ResourceReplacer usage is advertised so pre-mutation gates can run.

## References

- ADR 0024 (`decisions/0024-iac-typed-force-cutover.md`) — strict-contracts cutover precedent.
- ADR 0040 (`decisions/0040-v2-action-lifecycle-provider-compatibility.md`) — provider-side compatibility contract.
- PR #639 — v2 hooks engine landing.
- PR #699 — strict v2 lifecycle and provider.Apply removal.
- PR #743 — `wfctlhelpers.ApplyPlan` removal.
- `iac/wfctlhelpers/apply.go` — current `ApplyPlanWithHooks` implementation.
- Prior product design: `docs/plans/2026-04-25-wfctl-lifecycle-product-design.md`
