# Adversarial Plan Review #2 — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Reviewer:** adversarial-design-review skill (Claude Opus 4.7), plan-phase cycle 2
**Target plan:** `docs/plans/2026-05-05-iac-deferred-cleanup.md` @ `2dbee68e` (rev2)
**Inherited design:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` @ `f112d206` (rev3, design-phase PASS)
**Cycle 1 review:** `docs/plans/2026-05-05-iac-deferred-cleanup.adversarial-plan-review-1.md` @ `263ecd50`
**Date:** 2026-05-05
**Cycle:** 2 (cycle-cap waived by user 2026-05-05)
**Verdict:** **PASS** — all 4 Critical + 5 Important findings from cycle 1 closed; 1 NEW Important finding (stale §Rollback cross-reference). No blockers.

---

## Cycle-1 finding closure

### Critical

| Finding | Status | Evidence |
|---|---|---|
| **C-1** PR 6 hidden serial dep on PR 4 Task 4.1 | **CLOSED** | rev2 split into PR 6 (Tasks 6.1+6.2; Set A; independent) + PR 6b (Enumerator impl; **BLOCKS on PR 4**). §PR Grouping table line 37 explicitly annotates the gate. PR 6b §Sequencing line 1162 makes the dep unambiguous. |
| **C-2** Tag-cuts misclassified as PR 7 | **CLOSED** | rev2 added §Coordination tasks table (lines 41-49) explicitly stating "orchestrator-side; NOT part of any PR branch". Hard gate at line 49 ("Task 7.1 starts ONLY after both Coord-2 AND Coord-3 complete") + reinforcement at line 1296 inside Task 7.0 block. |
| **C-3** Stdout sketch unverified | **CLOSED** | rev2 dropped sketch (line 1427 explicitly says "The plan does NOT pre-specify a sketch ... sketches that aren't verified ... are a credibility-erosion class"). Replacement instruction is actionable: capture FULL stdout transcript verbatim into TC2 cutover doc, verify 4 Delete+Create pairs + ReplaceIDMap. |
| **C-4** runInfraCleanup signature | **CLOSED** | rev2 Task 4.2 Step 3 line 684 changed to `func runInfraCleanup(args []string) error`. Matches canonical pattern verified at `git show origin/main:cmd/wfctl/infra.go:200` (`func runInfraPlan(args []string) error`). Task explicitly directs implementer to verify the canonical pattern (line 663-668) before writing. |

### Important

| Finding | Status | Evidence |
|---|---|---|
| **I-1** Line-number drift on SecretEntry.Name | **CLOSED** | rev2 Task 1.1 Step 1 (lines 74-81) drops line numbers; uses field-name grep (`grep -n "^	Generate \|^	Entries \|type Secret"` etc). Verified field names against origin/main: Key:14, Name:44 (rev1's "line 47" was wrong — and now irrelevant). |
| **I-2** smoke.yml no-op gate | **CLOSED** | rev2 Task 4.4 Step 1 line 838 emits `::notice::` (not `::warning::`) + line 844 mandates explicit PR-body call-out: "Smoke-gate cleanup wiring activates only after workflow#542 ... Unit tests in Task 4.2 ARE the integration verification; Task 4.4 is wiring-only." |
| **I-3** Task 4.1 pre-DM | **CLOSED** | rev2 Task 4.1 line 518: "**Pre-DM team-lead** before this task per workspace policy (new public interface in `interfaces/iac_provider.go` is a cross-package contract change; same trigger as Task 4.4 + Task 7.1)." |
| **I-4** Release CI escalation on Task 7.0a/b/c | **CLOSED** | rev2 Task 7.0a Step 5 (line 1284-1286) is explicit. Task 7.0b (line 1290) and 7.0c (line 1294) both reference "Identical shape ... INCLUDING Step 5 escalation" — covers all three. |
| **I-5** W-Diagnose-540 ADR or alt-shape | **CLOSED** | rev2 Task 3.1 Step 2 flipped to `t.Skip` alt-shape (lines 432-453) + runbook entry at `docs/test-skips.md` (lines 480-488). PR 3 body notes the alt-shape choice (lines 501-504). Avoids the red-CI-on-main problem entirely. |

---

## NEW findings introduced by rev2

### IMPORTANT

#### NI-1. PR 4 §Rollback (line 863) has a stale cross-reference to "PR 6 Task 6.3" for the Enumerator impl

**Location:** plan line 863 (PR 4 Task 4.5 §Rollback).

**Defect.** The rollback paragraph reads:

> No plugin-stub revert required because Enumerator is opt-in. **DO plugin's Enumerator impl is a separate PR (PR 6 Task 6.3)**; if PR 4 is reverted, DO plugin's impl simply doesn't get used (no compile break).

But rev2 split the Enumerator impl out of PR 6 into PR 6b. Task 6.3 is now PR 6's PR-merge orchestration task (line 1149), NOT the Enumerator impl. The Enumerator impl lives in PR 6b Task 6b.1 (line 1165-1236).

This is a stale rev1 cross-reference left over after the C-1 split. An implementer reverting PR 4 and consulting this paragraph for "what PR contains the Enumerator impl I might need to coordinate with" gets pointed at the wrong PR.

**Severity reasoning.** Important (not Critical) because: (a) the rollback semantics are correct (opt-in interface means no compile break either way), so the operational outcome is unchanged; (b) but the cross-reference is wrong and an implementer or reviewer auditing the rollback path will be confused; (c) recurring class — workspace memory `feedback_check_versions_actively` and the plan-phase plan-literal-vs-reality discipline both target stale cross-references.

**Fix option (single-line edit).** Change line 863 from `(PR 6 Task 6.3)` to `(PR 6b Task 6b.1)`.

---

## Bug-class scan — focused (rev2)

| Class | Result |
|---|---|
| **Hidden serial deps among PR 6/6b/7/8** | None new. PR 6b → PR 4 gate explicit; Coord-2 → PR 6b gate explicit; Coord-3 → PRs 2/3/4/5 explicit; Task 7.1 → Coord-2 + Coord-3 explicit. Coord-2 implicitly waits for Coord-3 only if v0.21.2 must precede DO v0.10.1 — it doesn't (DO go.mod can pin a workflow main HEAD per Task 6b.1 Step 1b). |
| **Verification-class mismatch (TC2 runtime-launch-validation)** | Task 8.2 Step 3 has the /healthz poll loop + 200 verification + 5-min budget + DM-team-lead-on-failure. Step 4 commits a transcript + ReplaceIDMap audit doc. Matches `superpowers:runtime-launch-validation`. ✓ |
| **Missing rollback wiring** | Each PR has a §Rollback section. NI-1 above is the only stale cross-reference. ✓ |
| **Missing failure modes** | Task 7.0a/b/c Step 5 escalation closed I-4. Task 8.2 §Recovery references design rev3 §TC2 Execution table (5 failure modes documented). Task 6b.1 Step 1 has explicit STOP-and-DM if PR 4 hasn't merged. ✓ |
| **User-intent drift** | Plan covers all 9 deferred items: #536 (W-Cleanup PR 4 + PR 6b), #537 + #539 (W-Precision PR 2), #538 (W-Refactor PR 5 implicit via canonical helper delegation), #540 (W-Diagnose PR 3), #541 (W-541 PR 1), #62 + #63 (DO PR 6), TC1 (PR 7), TC2 (PR 8). #542 explicitly out-of-scope per user direction. ✓ |
| **Plan-literal-vs-reality (sample re-verify)** | Spot-checked: `runInfraPlan` signature at `cmd/wfctl/infra.go:200` matches plan claim ✓. `--allow-replace` flag at `cmd/wfctl/infra_apply.go:61-99` matches ✓. `SecretGen.Key` at line 14 + `SecretEntry.Name` at line 44 verified by name (line numbers dropped per I-1 fix) ✓. |

---

## NEW Critical findings

**None.**

---

## Final verdict

**PASS.** All 4 cycle-1 Critical findings closed. All 5 cycle-1 Important findings closed. One NEW Important finding (NI-1, stale cross-reference in PR 4 §Rollback at line 863) — a single-line edit. Per user authorization ("avoid needless nitpicking"), NI-1 is recommended-fix-but-not-blocking; the implementer following PR 4 rollback steps gets the correct operational outcome regardless.

**Recommended path:** plan author may apply NI-1 single-line fix (one-line change, 30 seconds) before scope-lock or hand off to alignment-check / scope-lock as-is. Either way, plan is PASS-quality and ready to execute.
