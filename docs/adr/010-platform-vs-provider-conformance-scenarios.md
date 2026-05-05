# ADR 010: Platform-vs-Provider Conformance Scenario Classification

**Status:** Accepted
**Date:** 2026-05-05
**Context:** W-7 (workflow PR #535) shipped 12 conformance scenarios in `iac/conformance/`. During implementation, 4 scenarios diverged from the typical pattern of asserting against `cfg.Provider()`; instead they exercise platform-shared surfaces directly.

## Decision

We codify TWO classes of conformance scenario:

**Provider-level scenarios** (8 of 12): assert against `cfg.Provider()`. Exercise per-provider behavior (Diff, Apply, ResourceDriver lookup, etc.). Scenarios:
- Scenario_NeedsReplaceTriggersReplaceAction
- Scenario_DeleteActionInApplyInvokesDriverDelete
- Scenario_DiffSurvivesGRPCRoundTrip
- Scenario_OutputsRefreshDetectsNewFields
- Scenario_CrossResourceConstraintRejection
- Scenario_OutputsConsistencyAcrossReadCycles
- Scenario_ReplaceCascadePreservesDependents
- Scenario_UpsertOnAlreadyExists

**Platform-level scenarios** (4 of 12): bypass `cfg.Provider()`; exercise platform-shared surfaces (`inputsnapshot`, `jitsubst`, `wfctlhelpers`). cfg.Provider is required by Run's validateConfig precondition but intentionally NOT invoked. Scenarios:
- Scenario_PlanStaleDiagnostic — exercises `inputsnapshot.NewStaleError`
- Scenario_InfraOutputCrossModuleResolution — exercises `jitsubst.ResolveSpec`
- Scenario_ProtectedReplaceWithoutOverride — exercises `wfctlhelpers.ValidateAllowReplaceProtected`
- Scenario_ProtectedReplaceWithOverride — exercises `wfctlhelpers.ValidateAllowReplaceProtected`

Each platform-level scenario carries a body comment naming the platform surface it exercises and explaining why cfg.Provider is unused.

## Rationale

Some root-cause issues from the IaC conformance plan (e.g. plan-stale-diagnostic, JIT secret resolution, --allow-replace gate) live at the platform layer (cross-provider-shared code), not at the per-provider layer. Conformance scenarios for those issues SHOULD test the platform surface directly — wrapping in `cfg.Provider()` calls would be vestigial.

The 4-of-12 ratio is acceptable; the boundary is non-arbitrary (each platform-level scenario tests code that lives in `iac/` or `iac/wfctlhelpers/`, not in any specific provider).

## Consequences

- Future contributors adding conformance scenarios must classify their scenario at design time and document the choice in the body comment.
- The Run dispatcher does not need code changes for this classification; it already validates `cfg.Provider != nil` regardless. Platform-level scenarios pass a NoopProvider to satisfy validateConfig.
- If future iteration wants typed enforcement, a `Scenario.Platform bool` field could be added and consulted by Run for richer reporting. Out of scope for this ADR.

## Alternatives Considered

- **Bypass validateConfig for platform-level scenarios.** Rejected: the precondition is universal; making it conditional adds complexity without benefit.
- **Move platform-level scenarios out of conformance/ into a separate package.** Rejected: scenarios are conceptually about provider-conformance to the IaC contract; platform-shared surfaces are part of that contract.

## References

- Workflow PR #535 (W-7) — original implementation
- Workflow PR #538 (W-8) — codemod that surfaced the pattern in lint reports
- IaC conformance plan: `docs/plans/2026-05-03-iac-conformance-and-replace.md`
- Deferred cleanup design: `docs/plans/2026-05-05-iac-deferred-cleanup-design.md`
