# Retro: IaC ErrResourceNotFound sentinel translation

**PR:** workflow#788 — feat(interfaces): IsErrResourceNotFound helper + struct Is method + 11 call-site migrations
**Merged:** 2026-05-27
**Branch:** feat/iac-not-found-sentinel-translation-2026-05-27
**Design:** docs/plans/2026-05-27-iac-not-found-sentinel-translation-design.md (lives on design branch; not merged to main — same pattern as DNS cascade. scope-lock-publish helper shipped as autodev v6.1.1 specifically addresses this)
**Plan:** docs/plans/2026-05-27-iac-not-found-sentinel-translation.md (same)
**Trigger:** DNS provider cascade retro 2026-05-27 plugin-level follow-up: workflow-side `classifyCreate` didn't recognize gRPC-marshalled sentinel.

## Adversarial-review findings, scored

### Design phase (3 cycles)

| Cycle | Finding | Severity | Outcome |
|---|---|---|---|
| 1 | Target SDK files (`resourcedriver_server.go`, `resourcedriver_client.go`) don't exist | Critical | Prescient — cycle 2 pivoted away from SDK encoding |
| 1 | Proposed `codes.NotFound` + reason-prefix encoding conflicts w/ ErrImageNotInRegistry string-match precedent | Critical | Prescient — cycle 2 adopted the existing precedent |
| 1 | A3 (in-process `sdk.ServeIaCPlugin` test) false — requires subprocess | Critical | Prescient — cycle 2 removed integration-test assumption |
| 2 | `isIaCNotFound` delegation drops `errors.As(*platform.ResourceNotFoundError)` branch (behavioral regression) | Critical | Prescient — cycle 3 added `Is` method on the struct |
| 2 | A2 falsified — 11 total bare `errors.Is` sites, not 3 | Critical | Prescient — cycle 3 enumerated all 11 |
| 2 | Import-cycle risk for `interfaces → platform` | Important | Resolved upfront — verified one-way `platform → interfaces` |
| 3 | Nitpicks (3) — incomplete table, missing `strings` import note, SQL-callers comment | Minor | Resolved upfront — cycle-3.5 fixes |

### Plan phase (2 cycles)

| Cycle | Finding | Outcome |
|---|---|---|
| 1 | Task 4 integration regression test had 3 Criticals (unexported `classifyCreate`, wrong `ResourceDriver` interface shape, wrong driver-vs-provider passing) | Resolved by dropping Task 4 — Task 2 unit tests already cover the gRPC-stringified case |
| 1 | Task 1 duplicate `fmt` import + missing test package qualifiers + Task 3 step-boundary ambiguity | Resolved upfront |
| 2 | Nitpick: "mirrors precedent" phrasing inaccurate (no `IsErrImageNotInRegistry` helper exists) | Minor — pattern is correct |

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Cycle-1 design proposed SDK encoding that didn't follow established `ErrImageNotInRegistry` precedent — should have caught from project-design-guidance | brainstorming self-challenge | Self-challenge didn't ask "is there an existing precedent for cross-wire error translation in this codebase?" | Add "existing precedent for the cross-cutting concern this design solves" to self-challenge checklist |
| Cycle-2 design missed `errors.As(*platform.ResourceNotFoundError)` branch of existing `isIaCNotFound` | adversarial-design-review (cycle 1) | Cycle-1 reviewer caught the file-target errors but not the branch-completeness issue | When the design proposes "collapse helper to delegation," reviewer should diff old vs new branch coverage |
| Plan cycle-1 Task 4 had 3 Criticals — wrong unexported function, wrong interface shape, wrong driver-vs-provider passing | writing-plans | Plan author hadn't read `platform/differ.go` to verify exported vs unexported, hadn't read `interfaces/iac_resource_driver.go` to verify the full interface method set | Plan author should always read target files BEFORE writing the test code block |
| Design + plan + lock file not on main post-merge | finishing-a-development-branch | Lock file lived on unmerged design branch (same pattern as DNS cascade) | RESOLVED in same release cycle — autodev v6.1.1 ships `hooks/scope-lock-publish` to address |

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| project-design-guidance | ✓ | Cited workspace design-guidance for typed gRPC + cross-driver parity |
| brainstorming | ✗ | Skipped — design written directly per user "no approval needed" + scope was retro-defined |
| adversarial-design-review (design) | ✓ (3 cycles) | Cycles 1+2 caught real Critical issues; cycle 3 converged |
| writing-plans | ✓ | Initial draft + 1 revision |
| adversarial-design-review (plan) | ✓ (2 cycles) | Cycle 1 caught Task 4 Criticals → dropped; cycle 2 converged |
| alignment-check | ✓ | PASS first try (no drift) |
| scope-lock | ✓ | Applied at lock time (lock file stayed on design branch — same pattern as DNS cascade; scope-lock-publish from autodev v6.1.1 addresses for future) |
| subagent-driven-development | ✓ | Single implementer dispatched; 4 tasks completed |
| finishing-a-development-branch | partial | PR auto-merged via implementer's background monitor + manual admin-merge confirmation |
| post-merge-retrospective | ✓ (this doc) | |

## What worked

- **Adversarial design loop forced complete pivot from SDK wire-encoding to existing string-match precedent.** Cycle-1 reviewer's verification-against-actual-code caught that proposed target files don't exist + the existing `ErrImageNotInRegistry` precedent uses string-match. Pivot saved an SDK PR + plugin-rebuild cascade.
- **Adversarial design loop caught behavioral regression at cycle 2.** Cycle-2 reviewer noticed `isIaCNotFound` had THREE branches not two — delegation would silently drop `errors.As(*platform.ResourceNotFoundError)` and break local-state-store paths. Cycle-3 added the `Is(target)` method on the struct so `errors.Is` natively catches it.
- **Dropping Task 4 (integration regression) was the right call.** Unit tests in Task 2 cover the gRPC-stringified case; the integration test would have required a stub driver satisfying the full `interfaces.ResourceDriver` interface (8 methods) + a stub `IaCProvider` wrapper. Excess complexity for redundant coverage.
- **Single-implementer execution worked cleanly for 4-task scope.** Full subagent-team overhead unnecessary for ~50 LOC. `golangci-lint --new-from-rev=origin/main` ran clean pre-push (autodev v6.1.1 pre-emptively applied — same recurrence pattern the retro identified).

## What didn't

- **Skipped brainstorming.** User said "proceed autonomously" — interpretation was license to skip the Q&A gate, not the design gate. Design loop caught issues that brainstorming self-challenge could have surfaced earlier (e.g., "is there existing precedent for cross-wire error translation?"). One adversarial cycle saved if self-challenge had a "search-for-precedent" prompt.
- **Lock file stranded on design branch (recurrence).** Identical to DNS cascade pattern. The fix shipped in autodev v6.1.1 (scope-lock-publish helper) just landed; future cascades will use it. This was the last time this gap should bite.

## Plugin-level follow-ups

- **None new.** All structural follow-ups from the DNS cascade retro were addressed in autodev v6.1.1 (which shipped in the same release window as this PR):
  1. Plugin-loader runtime layout + config-validation schema bug classes in adversarial-design-review ✓
  2. `golangci-lint run` in verification gates ✓ (applied pre-emptively in this PR's pre-push)
  3. `hooks/scope-lock-publish` for lock-file-on-main ✓
  4. Spec-reviewer end-to-end scenario execution ✓
  5. `tests/cascade-preflight.sh` Release-pipeline-green preflight ✓

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| `/Users/jon/workspace/docs/design-guidance.md` | no change | No durable cross-design lesson beyond what DNS cascade already drove into rev 3 |
