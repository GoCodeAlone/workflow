# ADR 007: T3.9 runtime-launch-validation via loader-seam, not external gRPC binary

## Status

Accepted (with extensive deliberation history — see Decision history section).

## Context

W-3b Task T3.9 (`docs/plans/2026-05-03-iac-conformance-and-replace.md`, lines 1941-1953) specified the runtime-launch-validation as:

> Step 2: Build a real gRPC-loaded stub provider plugin in `internal/testdata/stub-provider/` whose `plugin.json` sets `iacProvider.computePlanVersion: "v2"`.
>
> Step 3: Run `/tmp/wfctl infra apply -c /tmp/stub-config.yaml --env test` against a state where the stub provider plugin is registered.

Strict reading: build a separate Go program with `sdk.Serve()`, ship the plugin.json + binary, run `wfctl` against it as a real cross-process subprocess, and capture the manual transcript in the commit body.

The implementer began on the strict reading and produced a working Option-A artifact (out-of-tree commit, intentionally not retained on this branch — see "Considered alternatives" below). The team-lead's direction-setting on Option A vs Option B subsequently churned through four transitions before settling; the full record lives in the Decision history section below.

## Decision

Implement T3.9 via in-tree Go integration test (`cmd/wfctl/infra_apply_v2_loader_test.go`) that exercises the full v2 dispatch chain — config parse → state load → provider load → ComputePlan Diff dispatch → wfctlhelpers.ApplyPlan → Replace decomposition into Delete + Create — by injecting a Go in-process provider through the `resolveIaCProvider` package-level seam (lifted in T3.6c). The test substitutes a Go provider that satisfies `interfaces.IaCProvider` AND `wfctlhelpers.ComputePlanVersionDeclarer` (returning "v2") for what would otherwise be a `discoverAndLoadIaCProvider`-loaded gRPC plugin.

A second test pins the `printDriftReportIfAny` wiring end-to-end via the same loader-seam injection plus an `applyV2ApplyPlanFn` substitution that returns a non-empty `InputDriftReport`.

No standalone Go binary or `plugin.json` ships under `internal/testdata/`.

## Reasoning

1. **The plan's own precedent reference is `plugin/sdk/iaclint/`** — a Go test-helper package, not a runnable binary. The strict reading of T3.9's text contradicts the precedent the same task body cites. When the precedent and the literal text conflict, the precedent reflects the documented project convention.

2. **Real-gRPC runtime validation lands in P-DO** when DigitalOcean's plugin sets `iacProvider.computePlanVersion: "v2"` in its own `plugin.json`. The recurring guard for v2 dispatch in production is the actual consumer + the W-7 conformance suite. Building a synthetic in-tree gRPC stub adds plumbing (`sdk.Serve()` boilerplate, all-method `InvokeMethod` dispatch, build infrastructure) that runs once for this PR's validation step and then never again — the artifact has no consumer after merge.

3. **v2 dispatch wiring is already verified at multiple levels:**
   - T3.6e tests cover the Replace classification at the platform level (NeedsReplace / ForceNew → "replace" action).
   - T3.6f tests cover the cache integration + parallel dispatch.
   - T3.7 tests cover the manifest-driven branch + drift-report wiring at the cmd/wfctl level via the `applyV2ApplyPlanFn` seam.
   - The loader-seam test in this ADR exercises the end-to-end flow from `applyInfraModules` (the production entrypoint runInfraApply uses) through every layer to driver dispatch — without the cross-process gRPC step that production providers use.

   The marginal coverage from also exercising the gRPC roundtrip on the synthetic stub doesn't catch a meaningfully different bug class. Production gRPC bugs surface in P-DO; pre-production gRPC bugs surface in `plugin/sdk/iaclint/` matchers (which are themselves Go test helpers, not runnable plugins).

4. **One bug surfaced by Option A is preserved as a separate fix.** The Option-A draft surfaced a real wfctl bug in `cmd/wfctl/deploy_providers.go::remoteResourceDriver.Diff`: passing `current.Sensitive` (`map[string]bool`) directly into the args map silently dropped the entire args struct at the gRPC boundary because `structpb.NewStruct` rejects `map[string]bool` and the upstream `mapToStruct` returns `&structpb.Struct{}` on err. Fix shipped as `fix(iac): map[string]bool drops gRPC args silently — sensitiveToAny conversion` (commit `40e07a1`); will surface in T3.10's PR description as a third W-3b incidentally-fixed bug. The ADR notes this so future readers don't lose the bug-discovery context when the Option-A code is absent.

## Consequences

### Positive

- W-3b PR ships without an in-tree `internal/testdata/stub-provider/` artifact that would have no future consumer.
- T3.9 integration test runs as part of `go test ./cmd/wfctl/...` — no special build step, no plugin-dir setup, no manual binary placement. CI catches dispatch regressions automatically.
- The test exercises the same `applyInfraModules` entrypoint production callers reach, so the chain coverage is end-to-end despite the in-process substitution.

### Negative

- The gRPC roundtrip itself (structpb (de)serialization, cross-process plugin lifecycle) is not exercised by W-3b's own tests. Risk: a v2 dispatch regression that only manifests over the gRPC boundary (e.g., a typed value that fails `structpb.NewStruct`) wouldn't surface until P-DO's own tests run against W-3b's tip.
  - **Mitigation 1**: the bug class above (typed map silently drops entire args struct) is now closed by the `sensitiveToAny` fix in commit `40e07a1`, so the most-recent observed instance of this risk is already addressed.
  - **Mitigation 2**: P-DO sets `computePlanVersion: "v2"` in a follow-up PR after W-3b merges. Its own CI exercises the gRPC roundtrip against W-3b's tip immediately on rebase.
  - **Mitigation 3**: the W-7 conformance suite ships a recurring cross-PR test harness that exercises the gRPC dispatch.

### Neutral

- The `resolveIaCProvider` var-seam (established in T3.6c for the apply path's test seam) gains a second test consumer beyond the T3.7 v1/v2 routing tests. Reinforces the seam as the canonical injection point for cross-package provider-load substitution; future tests that need to inject a provider over the loader path should follow the same pattern.

## Considered alternatives

### Alternative A: full external gRPC binary (the plan-literal reading)

Build `internal/testdata/stub-provider/` with `main.go`, `plugin.go`, and `plugin.json`; ship the binary built at `data/plugins/stub-provider/stub-provider`; run wfctl as a subprocess against it for the validation transcript.

**Why rejected**: see Reasoning #1-#3 above. The implementer produced a working Option-A draft mid-execution; team-lead authorized switching to Option B before merge. The Option-A draft surfaced one real bug (preserved as commit `40e07a1`) and is documented in this ADR for context but is not retained on the branch.

### Alternative C: keep both A AND B (belt-and-suspenders)

Ship both the Option-A binary stub AND the Option-B loader-seam test.

**Why rejected**: doubles the review surface for no marginal coverage (the loader-seam test exercises every layer the binary stub exercises, except the gRPC roundtrip itself, which Mitigations 1-3 above cover). The binary stub would have no consumer after this PR.

## Decision history

This ADR's final decision (Option B / loader-seam) settled after four team-lead direction transitions during W-3b execution. Recording the verbatim quotes here so the durable record captures both the final choice and the reasoning behind each transition — not just the last one. Spec-reviewer's adversarial review of the bare keeps-grpc-stub variant of this ADR (since superseded) explicitly called out that the prior single-reversal record violated the recording-decisions skill's durability invariant; this section closes that gap.

| # | Direction | Verbatim team-lead quote | Implementer action |
|---|-----------|--------------------------|---------------------|
| 1 | **Option B** (initial) | "Option B + ADR. Build the in-tree Go integration test that exercises the cross-process dispatch path through wfctl's loader seams (`resolveIaCProvider`); do NOT build a full external gRPC binary. ... Plan precedent cite is `plugin/sdk/iaclint/` — a Go-level test helper, not a runnable binary." | Implementer had already shipped Option A as commit `290243c` mid-execution before this guidance arrived; bug-surfacing during Option A development produced commit `40e07a1` (sensitiveToAny). |
| 2 | **Option A reversal** | "Path #1 — keep A. The bug-surfacing alone justifies the work; my Option B reasoning is invalidated in hindsight. Specifically: 'synthetic stub never runs again' — false; the stub is now a regression test for v2 dispatch in CI forever. 'hours of plumbing for proportional confidence' — false; you got bonus bug surface (the structpb.NewStruct silent-drop on `current.Sensitive map[string]bool` is exactly the kind of v2-only regression Option B would have missed)." | Implementer cherry-picked the Option A files back, producing `297d826` with a `keeps-grpc-stub` variant of this ADR documenting the reversal. |
| 3 | **Option B again** | "Stand down on the cherry-pick — Option B as shipped at `92f060e` is fine. My Path #1 message was a partial reversal based on assuming the bug-surfacing depended on the in-tree stub binary. But the sensitiveToAny bug is already independently captured at `40e07a1`, so the marginal value of restoring `290243c` is just durable subprocess regression coverage — which the loader-seam test in `92f060e` substantially provides without the binary maintenance burden." | Implementer reset the Option A commit + cherry-picked `92f060e` onto `40e07a1`, producing `c9101ba` (current branch state) with the loader-seam variant of this ADR. |
| 4 | **Option A again (then withdrawn)** | "Accepted. The branch is clean at SHA `297d826`. ... your final landing on Option A + ADR 007 is defensible (better integration confidence + captured bug-surface). One outstanding fix from code-reviewer's pre-review of `290243c`: Slice typing IMPORTANT: change to `[]string{"subnet-x", ...}` so the marshal goes through the typed-slice → structpb conversion that's the regression class T3.9 is meant to catch." | Implementer flagged the branch state mismatch (current was c9101ba = Option B, not 297d826 = Option A) and refused to flip again without explicit disambiguation. Spec-reviewer parallel-DM'd team-lead. |
| 5 | **FINAL = B** | "FINAL = B (c9101ba). Reviewers re-confirmed. Proceed to T3.10 (PR description text — 3 incidentally-fixed bugs)." | Final disposition. ADR amended with this Decision history section to preserve the deliberation record per spec-reviewer's amplification. |

The pattern across transitions 1–4 is the same in both directions: each subsequent message proposed reasoning that the previous message's argument hadn't accounted for. Final landing (transition 5) reverts to the original direction (#1) but the bug-surfacing benefit (#2) is preserved independently as commit `40e07a1`. Both reviewers (spec-reviewer + code-reviewer) explicitly endorsed the implementer's "do not act without team-lead disambiguation" hold during transition 4, and the strict-interpretation invariant from `using-superpowers` ("ambiguity is resolved upward, never sideways") was the operative rule.

Lesson: when a team-lead path-flips more than once mid-execution, the reviewing-agent + implementing-agent BOTH should refuse to proceed and force explicit disambiguation. Each transition's reasoning was individually defensible; collectively they were oscillating because no single reasoning captured all the relevant trade-offs. The final landing didn't introduce a new argument — it picked the position with the best independent secondary-coverage story (CI auto-runs + lower binary maintenance burden) once both bug-surfacing arguments had been satisfied separately.
