// Package wfctlhelpers exposes the IaC plan-execution engine that wfctl and
// IaC plugin providers share for dispatching plan actions to ResourceDrivers
// in a consistent, hook-aware way.
//
// # Action lifecycle versions
//
// Two callers can drive plan execution today:
//
//   - [ApplyPlan] (legacy, marked Deprecated) — runs the plan with empty
//     ApplyPlanHooks. Equivalent to [ApplyPlanWithHooks] with no hooks
//     registered. State persistence happens at whole-plan completion only,
//     not at per-action boundary.
//
//   - [ApplyPlanWithHooks] (v2, recommended) — runs the plan with caller-
//     supplied per-action hooks (OnResourceApplied / OnResourceDeleted) so
//     callers can persist state at each successful cloud-mutation boundary.
//     Required for invariants in workflow#640 (failed-delete-no-prune,
//     compensation-on-create-failure, etc.).
//
// # Migration status (#640)
//
// Phase 1 (this commit): Deprecated marker on ApplyPlan + per-caller
// inventory + provider-compatibility ADR. See:
//
//   - docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md (inventory)
//   - decisions/0040-v2-action-lifecycle-provider-compatibility.md (contract)
//   - decisions/0024-iac-typed-force-cutover.md (no-compat-shim mandate
//     that Phase 2 ships under)
//
// Phases 2–5: gRPC contract extension (HARD-CUTOVER per ADR 0024) → plugin
// migration → conformance scenario migration → v1 ApplyPlan removal.
//
// # For provider authors
//
// IaCProvider.Apply implementations have two shapes today:
//
//   - Canonical delegate (workflow-plugin-digitalocean only):
//
//     return wfctlhelpers.ApplyPlan(ctx, p, plan)
//
//     Phase 3 codemod will rewrite this to ApplyPlanWithHooks lockstep with
//     the iac-codemod constant + AST function bumps.
//
//   - Custom loop (workflow-plugin-aws / -gcp / -azure):
//
//     for _, action := range plan.Actions { drv := p.ResourceDriver(...); ... }
//
//     Phase 3 manual migration: the loop must emit per-action outcome via
//     the Phase 2-extended ApplyResponse proto so wfctl-side reconstructs
//     the hook events.
//
// See the inventory document for per-plugin file:line references.
package wfctlhelpers
