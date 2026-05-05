# Adversarial Review #2 — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Reviewer:** adversarial-design-review skill (Claude Opus 4.7)
**Target:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` @ `2cf141d2`
**Prior review:** `2026-05-05-iac-deferred-cleanup-design.adversarial-review-1.md` @ `58974299`
**Date:** 2026-05-05
**Cycle:** 2 of 2
**Verdict:** **FAIL** — 2 Critical findings (NEW), 3 Important findings (NEW), 1 Important regression. All 11 cycle-1 findings substantively closed.

---

## Summary

rev2 closes every cycle-1 finding with substantive (not cosmetic) edits, but the closure of I-1 (rename to `EnumerateByTag` + commit to "ALL providers must implement, not optional") introduces a NEW Critical: a breaking change to `interfaces.IaCProvider` that **will fail the `cross-plugin-build-test.yml` gate the moment the W-Cleanup PR is pushed**, because that gate `go build`s AWS/GCP/Azure plugins against the PR's workflow checkout via `replace`. The "stub PRs in parallel" mitigation cannot resolve the chicken-and-egg: the gate fails on the workflow PR before stubs can be merged.

Also: rev2's TC2 §"Cascade command" implies `wfctl infra plan -o /tmp/tc2-plan.json`, which is at risk of triggering the JIT-rejection guard at `cmd/wfctl/infra.go:292` if any cascade-replace child resource has a `${MODULE.field}` ref in its config (one near-miss confirmed in core-dump infra.yaml; not currently triggered, but the design should explicitly state the precondition).

---

## Cycle-1 finding closure transcript

| ID | Status | Verification |
|---|---|---|
| C-1 | **CLOSED** | rev2 §PR Grouping splits into C-1-TC1 row (existing PR #190, branch `feat/c1-staging-pg-cutover`) + C-1-TC2 row (NEW branch `feat/c2-staging-pg-cutover-nyc1`). Phase 3 step 8 reads "Branch fresh `feat/c2-staging-pg-cutover-nyc1` from core-dump main (post-TC1-merge)" — verbatim match. PR #190 body confirms `feat/c2-staging-pg-cutover-nyc1` (`gh pr view 190` 2026-05-05). |
| C-2 | **CLOSED** | rev2 acknowledges v0.10.0 IS tagged. Verified: `git -C /Users/jon/workspace/workflow-plugin-digitalocean tag -l v0.10.0` returns `v0.10.0`. (Reviewer cycle-1 was wrong — v0.10.0 was tagged AFTER PR #190 was opened but BEFORE rev1; the gap was real for ~hours, now closed.) Phase 3 step 3 cuts v0.10.1 atop v0.10.0, sequencing is correct. |
| C-3 | **CLOSED** | rev2 §TC2 Execution names exactly the 4 protected resources `core-dump-vpc, coredump-staging-pg-data, coredump-staging-pg, coredump-staging-pg-fw`. Verified against PR #190 body (verbatim match). Database-tier blast-radius reasoning added. |
| C-4 | **CLOSED** | rev2 §"TC1.5 explicit SKIPPED" cites the exact rationale verbatim from PR #190 body: "dedicated DO conformance account adds tracking + billing overhead the team does not want to absorb. Staging IS the test environment". Safety-belt rationale enumerated (W-6 + W-3a/b + W-541 + git-revert + healthz). |
| C-5 | **CLOSED** | rev2 drops `cfg.Secrets.Requires` speculation; uses `cfg.Secrets.Generate[i].Key` AND `cfg.Secrets.Entries[i].Name`. Both fields verified on `git show origin/main:config/secrets_config.go`: `SecretGen.Key` (line 14) + `SecretEntry.Name` (line 47). |
| I-1 | **CLOSED (with new risk introduced — see Critical N1)** | rev2 names the interface `EnumerateByTag` (verified zero matches via `git grep -n "EnumerateByTag" origin/main -- '*.go'`). Naming is clean. BUT: rev2 commits to "REQUIRED method (not optional). All 4 active providers (DO, AWS, GCP, Azure) must implement it" — this opens a NEW Critical (see N1 below). |
| I-2 | **PARTIALLY CLOSED** | rev2 §Pipeline expectation now has Parallel Set A / Parallel Set B sequencing. But the file-overlap risk on `docs/WFCTL.md` (W-Cleanup + W-Refactor both touch it per their Files lists) is acknowledged in passing only ("MAY merge in parallel since its file scope is disjoint"). Set B's W-Cleanup + W-Refactor are NOT explicitly serialized on the WFCTL.md file. See Important N3 below. |
| I-3 | **CLOSED (the spec is honest about deferring the fix)** | rev2 splits #540 into W-Diagnose-540 with explicit "observation-only (failing test that captures current laxness)" framing. Caveat in Important N5 below — diagnostic test design needs to be fail-CI-immediately, not silent-skip. |
| I-4 | **CLOSED** | rev2 §Phase 1 §Estimated diff: "~5 lines production + ~30-50 lines test = ~35-55 lines total". Verified by reading `cmd/wfctl/infra_align_rules.go:33-66` (~12 lines for the buildAlignContext switch arm — change is ~5 net new lines for top-level secrets handling). Existing test pattern (`cmd/wfctl/infra_align_test.go:421-448`, `TestInfraAlign_RA4_SecretsGenerate_DoesNotFire`) is ~30 lines; two new tests = ~50-60 lines. Estimate range is accurate. |
| I-5 | **CLOSED** | rev2 §Excluded items section cites: "out-of-scope. Per user direction (2026-05-05): the dedicated DO conformance account adds tracking + billing overhead the team does not want to absorb. Staging IS the test environment; Token provisioning is operator-side ops work, not engineering work; #542 stays open as a deferred-not-required tracking issue." Rationale matches user direction. |
| I-6 | **CLOSED** | rev2 §Rollback W-Cleanup: "revert BOTH `infra_cleanup.go` AND `conformance-smoke.yml` edit AND the `EnumerateByTag` interface addition AND the 3 plugin-stub PRs … Reversion order: revert plugin stubs first (so workflow's cross-plugin gate stays green), then revert workflow's interface addition." Now actionable. |

---

## NEW Critical findings (introduced by rev2)

### N1 — `EnumerateByTag` as a REQUIRED `IaCProvider` method breaks the cross-plugin-build-test gate

**Verified via `git show origin/main:.github/workflows/cross-plugin-build-test.yml`**: the gate runs `go build ./...` against `[workflow-plugin-aws, workflow-plugin-gcp, workflow-plugin-azure]` with `go mod edit -replace github.com/GoCodeAlone/workflow=../workflow` pointing each plugin at the PR's checkout of workflow. Critical line:

```
go mod edit -replace github.com/GoCodeAlone/workflow=../workflow
go mod tidy
go build ./...
```

The moment W-Cleanup pushes a workflow PR adding `EnumerateByTag` as a REQUIRED method on `interfaces.IaCProvider`, **all three plugins fail compile** because their `*Provider` types don't implement the new method yet. The cross-plugin gate fails immediately.

rev2 §W-Cleanup §"Plugin coordination" tries to mitigate via "after adding it on workflow main, the AWS/GCP/Azure plugins need lightweight stub PRs … 3 stub PRs run in parallel with the main W-Cleanup PR but block its merge". This **doesn't solve the chicken-and-egg**:

- Stub PRs in AWS/GCP/Azure plugins reference workflow's new interface method via `IaCProvider` satisfaction. Their CI runs `go build` against workflow MAIN (latest tagged release), not the W-Cleanup PR's branch. The plugin-side stub PR cannot be CI-green because it implements a method that doesn't exist on workflow main yet.
- The workflow-side W-Cleanup PR cannot pass the cross-plugin gate because no plugin has the method yet.
- Both repos are blocked on each other.

The workspace memory `feedback_workflow_plugin_structpb_boundary` and `feedback_local_ci_validation_for_ci_touching_tasks` both call out the cross-plugin gate as load-bearing.

**Better shape**: make `EnumerateByTag` a NEW interface (`Enumerator`) that providers OPT INTO via type assertion — exactly the established `ProviderValidator` pattern in `interfaces/iac_provider.go:48-62` (verified via `git show origin/main:interfaces/iac_provider.go`):

```go
// ProviderValidator is an optional interface for v2 plugins that need custom
// plan logic … Reserved as an extension hook … Plugins implementing this
// interface are accepted by the loader; the implementation is not yet
// exercised by core code.
type ProviderPlanner interface {
    PlanV2(ctx context.Context, ...) (IaCPlan, error)
}
```

Apply the same pattern:

```go
// Enumerator is an optional interface for providers that can list
// resources by tag across the cloud account. Used by `wfctl infra
// cleanup --tag <name>`.  Providers without a tag-query API (or where
// tag listing is operationally unsafe) simply do not implement it;
// `wfctl infra cleanup` skips them with a structured log line.
type Enumerator interface {
    EnumerateByTag(ctx context.Context, tag string) ([]ResourceRef, error)
}
```

Then `wfctl infra cleanup --tag` does `if enum, ok := provider.(interfaces.Enumerator); ok { ... } else { logSkip(provider, "no Enumerator") }`. No interface break, no cross-plugin gate failure, no chicken-and-egg, no "3 stub PRs" requirement. Stub PRs become OPTIONAL per-plugin work that lands on each provider's own cycle.

The cycle-1 reviewer's I-1 fix proposed three options: (a) ALL providers implement (rev2 chose this); (b) startup error if missing; or (c) rename to `EnumerateByTag`. rev2 took a + c but missed that option (a) violates the same workspace pattern that the codebase already uses for ProviderValidator. The "silent-skip leak" risk that motivated option (a) is real but is solved by `wfctl infra cleanup` printing "skipped provider X (no Enumerator interface)" — the operator sees the skip in stdout.

**Fix**: change rev2 §W-Cleanup to make `Enumerator` an OPT-IN interface; drop the "3 plugin-stub PRs" scope creep; rewrite §Rollback W-Cleanup accordingly (no plugin-stub revert needed); drop §Top-doubt #1 cross-repo coupling concern.

### N2 — `--plan /tmp/tc2-plan.json` flow is at risk of JIT-rejection guard if any TC2 resource config gains a `${MODULE.field}` ref

**Verified via `git show origin/main:cmd/wfctl/infra.go:292`**: `wfctl infra plan -o <file>` checks `planRequiresJITSubstitution` and rejects with literal error `"this plan requires JIT resolution; persisted plan.json is not supported. Run 'wfctl infra apply' directly without -o/--plan."` if any plan action's `Resource.Config` contains a `${MODULE.field}` ref (regex: `\$\{[^}.]+\.[^}]+\}` — verified at `iac/jitsubst/jitsubst.go:113`).

I scanned `git show origin/main:infra.yaml` from the core-dump repo for `${MODULE.field}`-shape refs in Resource.Configs. Current state is SAFE:
- All `${...}` refs in resource configs are env-var form (no dot): `${STAGING_VPC_UUID}`, `${STAGING_PG_PASSWORD}`, `${STAGING_PG_HOST}`, `${IMAGE_SHA}`, `${NATS_AUTH_TOKEN}`.
- The top-level `secrets.generate` block at line 48-50 uses `infra_output: coredump-staging-pg.private_ip` syntax which is NOT a `${...}` literal — it's a structured field that wfctl resolves before substitution.

But: rev2's design is brittle to future infra.yaml edits. If a contributor adds (e.g.) `vpc_uuid: "${core-dump-vpc.id}"` literal to ANY of the 4 protected resources' configs (which is the canonical W-5 pattern for cross-resource refs without env-var indirection), the next TC2 plan-persist will fail with the JIT-rejection error and the operator will have to drop the persisted-plan flow and run `wfctl infra apply` directly (without `--plan`). The rev2 §TC2 Execution flow will halt mid-procedure.

**Fix**: add a §TC2 Execution precondition: "Verify TC2 plan does not require JIT (the persisted-plan flow forbids it). If `wfctl infra plan -o /tmp/tc2-plan.json` returns the JIT-rejection error, switch to direct apply: `wfctl infra apply -c infra.yaml --env staging --allow-replace=...` (no `--plan`). The drift-postcondition + ReplaceIDMap protections still hold under direct apply." Also document that "commit the plan file to TC2 PR for operator-side audit" (rev2 §Top-doubt #3) is OPTIONAL: the audit-trail value is captured in the PR body's pre/post resource ID transcript.

---

## NEW Important findings (introduced by rev2)

### N3 — `docs/WFCTL.md` overlap between W-Cleanup + W-Refactor not addressed

rev2 §Pipeline expectation says "W-Refactor MAY merge in parallel with W-Precision/DO-Plugin since its file scope is disjoint (deploy_providers.go + new ADR file, no overlap with #537/#539/#62/#63)." But the original concern from cycle-1 I-2 was the W-Cleanup ↔ W-Refactor overlap on `docs/WFCTL.md`. rev2 §Components confirms BOTH PRs touch that file:
- W-Cleanup §Files: "Modify: `docs/WFCTL.md` — command reference."
- W-Refactor §Files: "Modify: `docs/WFCTL.md` if the refactor surfaces any user-visible CLI change (likely none)."

The "likely none" hedge isn't strict enough. If W-Refactor does touch WFCTL.md (even for adding cross-references), the merge-order between W-Cleanup and W-Refactor matters; whichever lands second will need a WFCTL.md rebase. rev2's solution (Set B's serialization "if W-Cleanup spans 5+ Copilot rounds") is conditional on a runtime metric, not a file-overlap rule.

**Fix**: explicit rule: "if W-Refactor touches `docs/WFCTL.md`, it must rebase on W-Cleanup's merge before push. Implementer pre-flight check: `git fetch origin main && git diff HEAD..origin/main -- docs/WFCTL.md` — if non-empty, rebase before push."

### N4 — Inconsistent error sentinel in W-Cleanup plugin-stub spec

rev2 §W-Cleanup §"Plugin coordination": "lightweight stub PRs (return `nil, ErrNotSupported` if the provider doesn't have a tag API …)" then §"Decision": "stub PRs in AWS/GCP/Azure plugins (3 small additional PRs returning `(nil, errors.ErrUnsupported)` …)".

**`ErrNotSupported` is not a Go stdlib symbol.** `errors.ErrUnsupported` IS the Go 1.21+ stdlib symbol (`var ErrUnsupported = New("unsupported operation")` per `go doc errors.ErrUnsupported`). The two paragraphs in the same section name different things. An implementer reading just the first paragraph will write `ErrNotSupported` and burn 5 minutes finding it doesn't exist.

If N1 is adopted (Enumerator becomes opt-in interface), this whole section becomes moot. If N1 is rejected and rev2's required-method shape stays, **fix**: pick one (`errors.ErrUnsupported`) and use it in both paragraphs.

### N5 — W-Diagnose-540's "observation-only (failing test)" is at risk of being silently skipped

rev2 §W-Diagnose-540 §Behavior: "This PR is observation-only (failing test that captures current laxness). The actual fix lands in a follow-up PR with bounded blast-radius."

A test that REPORTS a bug but is intentionally GREEN at HEAD is a `t.Skip("BUG: ...")`-style test that silently lies — until the fix lands, the CI sees nothing wrong. The proper TDD shape for "diagnostic-first" is `t.Errorf("BUG: extra iacProvider key not rejected — see #540")` so the test fails CI on every push from the moment it lands. Otherwise W-Diagnose-540 is observability-of-nothing: nobody notices when the fix lands; nobody notices when a future regression undoes the fix.

The cycle-1 I-3 fix asked for "phase the W-Precision PR's #540 work as: (1) write a failing test that proves current behavior is lax; (2) bisect the library version where strictness was honored; (3) either pin the right version OR upgrade." Step (1) was "write a failing test." rev2 implemented it as a passing test that reports the bug — **opposite intent**.

**Fix**: change rev2 §W-Diagnose-540 §Files to: "failing test in `plugin/sdk/manifest_test.go` — load a manifest with an extra key (`iacProvider.bogusKey`); assert validation FAILS (i.e. test calls `t.Errorf` because validation currently SUCCEEDS — bug per #540). The fix follow-up flips the test from FAIL→PASS (no test edit, just behavior change)."

---

## Important regression introduced by rev2 closure of cycle-1 finding

### R1 — Scope creep: 3 plugin-stub PRs added to W-Cleanup without ADR

The original cycle-1 finding I-1 ranked "naming overlaps with existing `ListResources`; silent-skip on missing impl is a leak risk; 'list+delete in one call' alternative not considered" as Important. rev2 closes by escalating from "optional `ResourceLister`" to "REQUIRED `EnumerateByTag` on IaCProvider + 3 plugin-stub PRs in AWS/GCP/Azure". This is a non-trivial scope expansion (from 1 PR to 4 PRs across 4 repos) for a closure of an Important finding.

The rev2 §Top doubt #1 acknowledges "cross-repo coupling that wasn't in rev1's analysis" but doesn't gate-check the scope expansion against the original "9 deferred items" framing — the new shape is "9 items + 3 cross-repo stubs". Per `feedback_implementer_scope_bleed`: scope expansions should be cleanly described and ADR'd.

**Fix**: if N1 is rejected, this scope expansion needs an ADR (e.g. ADR 011: "Tag-based resource enumeration: required vs optional interface tradeoff"). If N1 is adopted, the regression evaporates.

---

## Bug-class scan transcript (cycle 2)

| Class | Cycle-1 status | Cycle-2 status | Notes |
|---|---|---|---|
| Unstated assumptions | C-2, C-3, C-5 | **Clean** | All 5 cycle-1 facts re-verified against origin/main. |
| Repo-precedent conflicts | I-1, C-1 | **N1** | rev2's "REQUIRED interface method" violates the established opt-in-via-type-assertion precedent (ProviderValidator/ProviderPlanner). |
| YAGNI violations | Clean | **R1** (regression) | 3 plugin-stub PRs added because of an interface-design choice that could have been opt-in (zero stubs needed). |
| Missing failure modes | I-1, I-3 | **N2** | Persisted-plan JIT-rejection precondition missing from §TC2 Execution. |
| Security / privacy | Clean | **Clean** | Plan-file-on-disk concern raised in cycle-1 review-prompt is real but contained to operator's machine; resource IDs are not secrets. |
| Rollback story | I-6 | **Clean** | Rev2 made W-Cleanup rollback explicit with file paths + revert order. |
| Simpler alternative not considered | C-5 | **N1** | Opt-in `Enumerator` interface IS the simpler alternative and was not considered. |
| User-intent drift | Clean | **Clean** | Excluded items §#542 explicitly tied to user direction; TC2 separation matches PR #190 commitment. |
| Plan-literal-vs-reality | Not scanned cycle-1 | **Clean** | rev2's §Plan-literal-vs-reality surfaces section enumerates 7 risky surfaces; each was independently verified against origin/main during this review. The `cfg.Secrets.Generate[i].Key` and `cfg.Secrets.Entries[i].Name` paths verified against `git show origin/main:config/secrets_config.go`. The `--allow-replace` and `--plan` flag names verified against `cmd/wfctl/infra.go`. |
| Cycle-1-finding closure | N/A | **All 11 closed** | C-1..C-5 fully closed; I-1 introduces N1; I-2 partially closed (N3); I-3..I-6 fully closed. |

---

## Per-attack-prompt-question evaluation (cycle 2 specific)

1. **C-1 closure verified** — branch name `feat/c2-staging-pg-cutover-nyc1` matches PR #190; Phase 3 step 8 verbatim match.

2. **C-2 closure verified** — `git tag -l v0.10.0` returns `v0.10.0` on workflow-plugin-digitalocean.

3. **C-3 closure verified** — 4 named resources match PR #190 verbatim (`core-dump-vpc, coredump-staging-pg-data, coredump-staging-pg, coredump-staging-pg-fw`).

4. **C-4 closure verified** — TC1.5 SKIPPED rationale matches PR #190 body verbatim.

5. **C-5 closure verified** — `cfg.Secrets.Generate[i].Key` (line 14 of secrets_config.go) and `cfg.Secrets.Entries[i].Name` (line 47) both exist on origin/main; no `Requires` field anywhere in the file.

6. **I-1 closure** — `EnumerateByTag` is unique on origin/main (`git grep` returns zero matches). Naming clean. **BUT introduces N1.**

7. **I-2 closure (partial)** — Set A / Set B sequencing is added but file-overlap on docs/WFCTL.md still soft (N3).

8. **I-3 closure** — split into W-Diagnose-540 IS the correct shape, but the test design (silent-skip vs fail-CI) needs N5 fix.

9. **I-4 closure** — ~35-55 line estimate matches my independent calculation.

10. **I-5 closure** — Excluded items §#542 rationale verbatim matches user direction (verified via `gh issue view 542` body which references the W-7 budget-check no-op fallback).

11. **I-6 closure** — Rollback now actionable with specific file paths + revert order.

12. **NEW Critical risk #14 (REQUIRED method on IaCProvider)** — **CONFIRMED**. cross-plugin-build-test.yml gate WILL fail; proper fix is opt-in interface per ProviderValidator precedent.

13. **NEW Critical risk #14 (--plan operator-machine-local file)** — `commit the plan file to TC2 PR` is moderate-risk: plan.json contains resource IDs + `DesiredHash` + `InputSnapshot` keys (env-var names — NOT values). Resource IDs (UUIDs) are not secrets. `InputSnapshot` only stores key NAMES + key-fingerprints (not values). So committing plan.json IS safe per current schema. The deeper risk is N2 (JIT rejection) — operator may not be able to plan-persist at all if any resource gains a JIT ref.

14. **NEW Critical risk #14 (BREAKING interface change for EnumerateByTag)** — **CONFIRMED**. See N1.

15. **NEW Important risk #15 (W-Diagnose-540 test that lies)** — **CONFIRMED**. See N5.

---

## Options the author may not have considered (cycle 2)

### Option A — Make `Enumerator` an opt-in interface

Per N1 fix above. Net change: 1 workflow PR (interface + DO impl + cleanup subcommand using type assertion); zero plugin-stub PRs; no cross-plugin gate failure; matches established ProviderValidator pattern. Smaller scope, fewer PRs, less coordination. **Strongly recommend over rev2's REQUIRED-method design.**

### Option B — Defer #540 fix entirely; ship W-Diagnose-540 as `t.Errorf`-style test only

If the team doesn't have library-bisection bandwidth this cycle, ship the test that REPORTS the bug (fails CI) and accept a red CI on the schema-strictness check until a fix lands. The downstream consumers (P-DO PR #61 already merged) are not blocked because schema-strictness was never enforced anyway. The red CI is a visible "needs fix" signal.

### Option C — Drop `--plan` from §TC2 Execution; use direct apply

The `wfctl infra plan -o /tmp/tc2-plan.json && wfctl infra apply --plan ...` flow has no value over `wfctl infra apply -c infra.yaml --env staging --allow-replace=...` for a one-shot operator-driven cutover. The `--plan` flow's purpose is "preview first, apply against the same plan later", which adds operational risk (input-snapshot drift between the two commands) without operational benefit for a one-shot cascade. **Recommend dropping the `--plan` step entirely**; keep the pre-flight `wfctl infra plan -c infra.yaml --env staging` (no `-o`) as a stdout preview, then run `wfctl infra apply` directly. Eliminates N2.

---

## Verdict + next step

**FAIL** — 2 NEW Critical (N1, N2) + 3 NEW Important (N3, N4, N5) + 1 Important regression (R1, contingent on N1).

All 11 cycle-1 findings (5 Critical + 6 Important) substantively closed.

Cycle 1's verdict promised "If rev2 lands all of the Critical fixes, the next cycle should PASS." rev2 DID land all Critical fixes. But the I-1 closure (the only Important rev1 escalated to a NEW Critical-level scope expansion) re-introduced a foundational defect (N1) that didn't exist in rev1.

Per autonomous mandate, this is cycle 2 of 2. Two paths:

**Path 1 (recommended): apply Options A + C as a rev3 hotfix** — Both are localized edits (replace REQUIRED-method spec with opt-in interface; drop `--plan` from TC2 cascade command). N3, N4, N5, R1 all evaporate or become trivial. The result would PASS a notional cycle-3 review. Cost: 30-60 min of design edits + a 4th adversarial review (out-of-cycle).

**Path 2: escalate to user** per the cycle-2 escalation rule, with this report's N1 + N2 as the unresolved findings. The user can decide whether to (a) accept rev2 as-is and absorb the cross-plugin gate failure + JIT-rejection risk during execution, OR (b) authorize a rev3.

My recommendation: Path 1. The fixes are mechanical; both are codebase-precedent-aligned (ProviderValidator pattern; the existing direct-apply flow that core-dump's deploy.yml already uses); both REDUCE plan complexity rather than expanding it. The user's autonomous mandate ("get all the deferred issues addressed") presumes the deferred-cleanup plan executes cleanly; rev2 as-is will halt at the cross-plugin gate on first push.

If escalating: surface N1 (cross-plugin gate breakage) as the must-decide; N2 as a runtime preflight concern that operator can navigate around; N3-N5+R1 as cleanup nits.
