# ADR 009: ProviderPlanner Interface Included Per User Override

## Status
Accepted

## Context

Cycle-3 → cycle-7 adversarial reviews of the implementation plan oscillated between "drop entirely" (YAGNI) and "ship as definition-only" (preserve extension hook); rev6 surfaced the ratification ask to the user via plan § Open Questions.

The user explicitly ratified Option C ("Override — W-9 expands to include the `ProviderPlanner` interface definition") on 2026-05-03 (jon@langevin.me, direct chat reply). User reasoning paraphrased: the design pass mandate "don't defer any fixes" + "build these fixes the right way" applies to the extension hook as well; the workspace roadmap includes future Tofu/Pulumi adapter work and the cost of shipping the interface now is bounded (~30 min plan revision + ~1 hour implementation).

**Provenance:** Decided by jon@langevin.me 2026-05-03 via direct chat reply ("option C"). Recorded in plan § Open Questions § "ProviderPlanner deferral".

## Decision

The optional `ProviderPlanner` interface ships as part of W-9's T9.1 task (`interfaces/iac_provider.go`). The interface is purely additive (plugins that don't implement it remain valid `IaCProvider` implementations). Core wfctl's `platform.ComputePlan` + `wfctlhelpers.ApplyPlan` do NOT type-assert against `ProviderPlanner` in v0.21.0 — the type-assertion at the dispatch site is reserved for future adapter PRs (Tofu/Pulumi-style) which will add it alongside their concrete consumer + design discussion.

## Consequences

**Positive:** Extension hook is in the public API surface from v0.21.0; future adapter PRs do not need to make the interface change a separate prerequisite PR. Type-assertion pattern is documented in the interface comment for adapter authors. Cross-plugin-build CI gate (T9.2 here) verifies the interface addition does not break AWS/GCP/Azure compile compatibility.

**Negative:** A speculative interface ships without a concrete in-tree consumer. If the future Tofu/Pulumi adapter design surfaces a different shape (different signature, different doc semantics), this interface will need a backwards-incompatible revision (or a sibling interface like `ProviderPlannerV2`). The user accepted this risk via the ratification.

**Operational notes:** the interface's `PlanV2` signature mirrors `platform.ComputePlan` (ctx + desired + current → IaCPlan, error) so the future adapter can type-assert and call cleanly. No tests beyond T9.1's compile-time type-assertion test ship in this plan series; the first concrete consumer ships its own integration tests.
