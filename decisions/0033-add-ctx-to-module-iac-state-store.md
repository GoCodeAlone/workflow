# 0033. Add context.Context to module.IaCStateStore mid-extraction

**Status:** Accepted
**Date:** 2026-05-14
**Decision-makers:** Jon (operator), autonomous pipeline
**Related:** docs/plans/2026-05-14-cloud-sdk-extraction.md (PR 6 / Task 15), docs/plans/2026-05-14-cloud-sdk-extraction-design.md, decisions/0031-strict-contracts-ergonomics.md

## Context

The cloud-SDK-extraction plan was scope-locked at 5 PRs / 14 tasks. During PR 3 / Task 7, the new host-side `grpcIaCStateStore` had to hardcode `context.Background()` on every RPC because `module.IaCStateStore`'s 6 methods (`module/iac_state.go:21`) take no `context.Context` — the call sites have no caller context to plumb. Task 7 shipped with a code comment flagging this as a known follow-up. The operator observed that, since the extraction is already rewriting that exact interface boundary, deferring the ctx change means a second cross-cutting PR later that touches the same files again.

Investigation established the blast radius is bounded and entirely within `module/`: the interface + its 7 implementations (`memory`/`fs`/`postgres`/`spaces`/`gcs`/`azure`/`grpc_client`) + the one caller file `module/pipeline_step_iac.go` (whose pipeline steps already hold a `PipelineContext`). The separate, unrelated `interfaces.IaCStateStore` (`interfaces/iac_state.go:14`) already takes `context.Context` on every method and is **not** touched. Adding scope to a locked plan is "intentional friction" per `skills/scope-lock/SKILL.md`; the operator gave explicit approval after reviewing the scoped blast-radius analysis.

## Decision

We will add `ctx context.Context` as the first parameter to all 6 `module.IaCStateStore` methods now, as a new dedicated PR (PR 6 / Task 15) appended to the locked manifest — not deferred, and not folded into PR 3's existing tasks.

Alternatives rejected:
- **Fold into PR 3's Task 7/8.** Rejected — it stretches those tasks' definitions past their locked scope and erodes per-PR review/revert granularity; the change is cohesive enough to stand alone.
- **Keep deferred (the original plan's posture).** Rejected by the operator — doing it post-extraction is a second cross-cutting PR re-touching the same files, and the Phase B/C/D plugin-side backend implementations would otherwise be built against a ctx-less interface and need their own follow-up retrofit.

## Consequences

- **Easier:** `grpcIaCStateStore` plumbs the caller's real context; `iacStateBackendServer` forwards its gRPC-received context into the store instead of discarding it; cancellation/deadline propagation works through the new contract. Phase B/C/D plugin backends are written ctx-ful from the start.
- **Easier:** removes the `context.Background()` wart and its apologetic comment from Task 7's code.
- **Harder / cost:** the locked plan grows to 6 PRs / 15 tasks; the manifest is amended, re-aligned, and re-locked (a new lock hash). PR 6 must land before PR 3 is finalized so Task 7's `grpcIaCStateStore` is amended in place rather than shipped ctx-less then re-touched.
- **New constraint:** every future `module.IaCStateStore` implementation (the four cloud plugins in Phase B/C/D) must accept and honor `ctx`. This is the intended outcome but is now a hard contract, not a nicety.
- **Bounded undo cost:** reverting is a single-PR revert of a mechanical signature widening; no data-format or wire-contract change is involved (the `IaCStateBackend` proto already carries gRPC's context implicitly).
