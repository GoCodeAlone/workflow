# ADR 008: JIT substitution lives at the ApplyPlan dispatch loop, not inside per-action helpers

## Status

Accepted

## Context

W-5 Task T5.3 (`docs/plans/2026-05-03-iac-conformance-and-replace.md`, line 2078) specified that the Replace-cascade contract — dependents of a just-replaced parent must see the parent's new ProviderID — be implemented by adding an explicit `jitsubst.ResolveSpec` call inside `wfctlhelpers.doReplace`, right before the post-Delete `driver.Create`:

> ### Task T5.3: Wire JIT substitution into Replace action
>
> **Files:** Modify: `iac/wfctlhelpers/apply.go::doReplace`

By the time T5.3 was up for implementation, T5.2 (committed as `424f8e1`) had already wired `jitsubst.ResolveSpec` at the dispatch-loop level — once per action, immediately before `dispatchAction`. The cascade contract was already satisfied: `doReplace` populates `result.ReplaceIDMap[name] = newProviderID` in iteration N; the loop's pre-dispatch `ResolveSpec` call in iteration N+1 sees the fresh entry and resolves `${parent.id}` against it before the dependent's driver call.

The plan-spec defect is real: T5.2 absorbed T5.3's intended substitution work. Two ways to honor T5.3 in code remain:

1. **Loop-level only (test-only T5.3)** — accept that the cascade behavior already works; ship verification tests + godoc clarification on `doReplace`; zero functional change to `apply.go`.
2. **Inner-resolve (defense-in-depth T5.3)** — also call `jitsubst.ResolveSpec` inside `doReplace` against the freshest `result.ReplaceIDMap`, requiring `syncedOutputs` (currently a local of `applyPlanWithEnvProvider`) to be threaded through `dispatchAction → doReplace` — a one-caller-only signature change.

Spec-reviewer flagged option 1 as an architectural reinterpretation of a locked plan task and escalated to team-lead for arbitration.

## Decision

JIT substitution lives at exactly one site — the `applyPlanWithEnvProvider` dispatch loop, immediately before `dispatchAction`. Per-action helpers (`doCreate`, `doUpdate`, `doReplace`, `doDelete`) stay JIT-naive: they receive an already-resolved `action.Resource.Config` and call drivers directly. The Replace-cascade contract is honored by the temporal ordering of `result.ReplaceIDMap` writes (inside `doReplace`) versus `ResolveSpec` reads (top of next iteration), NOT by an explicit substitution call inside `doReplace`.

T5.3 ships as `apply_replace_cascade_test.go` (two scenarios: `Replace+Create` and `Replace+Replace` cascades) plus a "# Cascade contract (T5.3)" section added to `doReplace`'s godoc explaining the loop-ordering invariant.

## Consequences

**Positive:**

- Single substitution site (DRY): adding a new per-action helper (e.g., a future `doImport`) does not require duplicating `ResolveSpec` plumbing.
- `dispatchAction`'s signature stays narrow: `(ctx, driver, action, result)`. Threading `syncedOutputs` through the dispatch tier would have made the helper boundary leaky for one call site.
- Per-action helpers stay testable in isolation against a fake driver without a JIT fixture — the existing `apply_test.go` patterns continue to work.
- Cascade behavior is verifiable by black-box assertion against `ApplyPlan` (the apply_replace_cascade_test.go scenarios), which is closer to operator-observable contract than per-helper white-box assertions.

**Negative:**

- The cascade contract relies on a subtle invariant — `result.ReplaceIDMap` writes inside `doReplace` MUST happen before the next iteration's `ResolveSpec` read. A future refactor that batches `doReplace` calls or moves substitution out of the iteration loop would silently break the cascade. Mitigated by `apply_replace_cascade_test.go::TestApplyPlan_ReplaceCascade_DependentCreateGetsNewParentID` and `..._DependentReplaceGetsNewParentID`, which fail loudly if either of those refactors lands.
- Plan-deviation: the implementation diverges from `docs/plans/2026-05-03-iac-conformance-and-replace.md` §T5.3's "Modify `iac/wfctlhelpers/apply.go::doReplace`" instruction. This ADR records the rejected alternative (inner-resolve) and the reasoning so future contributors see the trade-off rather than rediscovering it via `git blame` archaeology. A future plan revision should reflect the actual implementation; out of scope for this ADR.
- Operators reading the apply log have no per-action breadcrumb that JIT resolution ran for a given action — substitution happens transparently. Mitigated by the per-action `jit substitution: <reason>` ActionError surfaced when resolution fails (apply_jit_test.go locks the prefix).

**Provenance:** decided by Claude (autonomous-pipeline team-lead) after spec-reviewer / implementer / team-lead consensus on 2026-05-04. spec-reviewer flagged the structural divergence as a meaningful PlanDiagnostic worth escalating; implementer surfaced the redundant-with-T5.2 observation; team-lead chose option-1 (test-only T5.3 + this ADR) over option-2 (inner-resolve rework) because the threaded-`syncedOutputs` API change was more invasive than the documentation cost of recording the choice durably.

## References

- Plan task spec: `docs/plans/2026-05-03-iac-conformance-and-replace.md` §T5.3 (line 2078)
- Loop-level substitution implementation: `iac/wfctlhelpers/apply.go::applyPlanWithEnvProvider` (commit `424f8e1`)
- Cascade verification tests: `iac/wfctlhelpers/apply_replace_cascade_test.go` (commit `3aecc97`)
- Cascade godoc: `iac/wfctlhelpers/apply.go::doReplace` "# Cascade contract (T5.3)" section
- Failure-path test that locks the substitution-skips-dispatch contract: `iac/wfctlhelpers/apply_jit_test.go::TestApplyPlan_JIT_UnresolvedRef_RecordsActionErrorAndSkipsDispatch`
