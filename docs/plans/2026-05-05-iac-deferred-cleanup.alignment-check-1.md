# Alignment Check 1 — IaC Deferred-Work Cleanup + C-1 Wrap-Up

**Design:** `docs/plans/2026-05-05-iac-deferred-cleanup-design.md` (rev3, commit f112d206)
**Plan:** `docs/plans/2026-05-05-iac-deferred-cleanup.md` (rev2, commit b8edc0b)
**Date:** 2026-05-04

### Alignment Report

**Status:** PASS

**Coverage:**
| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| workflow#541 (R-A4 align rule consult top-level secrets) | PR 1 Task 1.1 + Task 1.2 | Covered |
| workflow#537 (mapToStruct silent-drop fix) | PR 2 Task 2.1 | Covered |
| workflow#539 (iac-codemod accumulator pattern) | PR 2 Task 2.2 | Covered |
| workflow#540 (manifest schema additionalProperties:false) | PR 3 Task 3.1 (t.Skip alt-shape) | Covered |
| workflow#536 (wfctl infra cleanup --tag subcommand) | PR 4 Tasks 4.1-4.5 + PR 6b Task 6b.1 | Covered |
| workflow-plugin-digitalocean#62 (Initialize ctx threading) | PR 6 Task 6.1 | Covered |
| workflow-plugin-digitalocean#63 (Plan -> platform.ComputePlan) | PR 6 Task 6.2 | Covered |
| Backlog 8 (deploy_providers.remoteIaCProvider refactor) | PR 5 Task 5.1 | Covered |
| Backlog 9 (ADR 010 platform-vs-provider scenarios) | PR 5 Task 5.2 | Covered |
| TC1 closure on existing PR #190 (pin bump amend) | PR 7 Task 7.1 + Task 7.2 | Covered |
| TC2 closure on NEW branch `feat/c2-staging-pg-cutover-nyc1` | PR 8 Task 8.1 (explicit fresh branch from main post-TC1) | Covered |
| TC1.5 SKIPPED (operator decision) | Plan §Out of scope (no tasks) | Covered (correctly excluded) |
| workflow#542 excluded (operator-side ops, deferred) | Plan §Out of scope (no tasks) | Covered (correctly excluded) |
| Phase 1 sequential (W-541 only -> v0.21.1) | PR 1 + Coord-1 | Covered |
| Phase 2 parallel sets A + B (W-Precision/Diagnose-540/DO-Plugin / W-Cleanup+W-Refactor) | PRs 2,3,6 (Set A) + PRs 4,5 (Set B) | Covered |
| Phase 3 sequential (cut tags -> bump pins -> TC1 merge -> TC2 branch -> TC2 apply) | Coord-2/3 + PR 7 + PR 8 | Covered |
| Tag cut: workflow v0.21.1 after PR 1 | Coord-1 / Task 7.0a | Covered |
| Tag cut: DO v0.10.1 after PR 6b | Coord-2 / Task 7.0b | Covered |
| Tag cut: workflow v0.21.2 after PRs 2-5 | Coord-3 / Task 7.0c | Covered |
| Opt-in Enumerator interface (rev3 N1: type assertion at use site, no required iface) | Task 4.1 (interfaces/iac_provider.go opt-in iface) + Task 4.2 (use-site type assertion + visible skip log) + Task 6b.1 (DO opt-in impl) | Covered |
| TC2 direct apply (rev3 N2: NO `--plan` flow, stdout-only preview) | Task 8.1 Step 2 (`wfctl infra plan` stdout-only) + Task 8.2 Step 2 (`wfctl infra apply --allow-replace` direct, no `-o /tmp/...json`) | Covered |
| W-Diagnose-540 alt-shape (`t.Skip` instead of failing-CI) | Task 3.1 explicit (`t.Skip` + runbook entry citing adversarial plan review I-5) | Covered (design rev3 §W-Diagnose-540 §Behavior accommodates) |
| Plan-literal-vs-reality verification (7 surfaces) | Plan §Plan-literal-vs-reality verification + per-task Step 1 verifications | Covered |
| Per-PR rollback paths | Each PR §Rollback section | Covered |
| TC2 §Recovery procedures (4-failure-mode table) | Task 8.2 Step 3 (/healthz fail -> DM team-lead + design rev3 reference) + PR 8 §Rollback | Covered |
| 4 protected resources verified (vpc/pg-data/pg/pg-fw) | Task 8.1 Step 3 + Task 8.2 Step 2 | Covered |
| Pre-DM team-lead policy on `.github/workflows/` + cross-package contract changes | Task 4.1 + Task 4.4 + Task 7.1 explicit pre-DM | Covered |

**Scope Check:**
| Plan Task | Design Requirement | Status |
|---|---|---|
| PR 1 Task 1.1 / 1.2 | §Phase 1 W-541 + §Components Phase 1 | Justified |
| PR 2 Task 2.1 | §W-Precision #537 | Justified |
| PR 2 Task 2.2 | §W-Precision #539 | Justified |
| PR 2 Task 2.3 | Standard PR-orchestration shape (Copilot cycle + admin-merge) | Justified |
| PR 3 Task 3.1 / 3.2 | §W-Diagnose-540 + rev3 §Behavior alt-shape allowance | Justified |
| PR 4 Task 4.1 (Enumerator iface) | §W-Cleanup Interface design (rev3 N1 opt-in) | Justified |
| PR 4 Task 4.2 (subcommand impl) | §W-Cleanup §Files: cmd/wfctl/infra_cleanup.go + main.go | Justified |
| PR 4 Task 4.3 (docs) | §W-Cleanup §Files: docs/WFCTL.md + docs/conformance-runbook.md | Justified |
| PR 4 Task 4.4 (smoke.yml wiring) | §W-Cleanup §Files: .github/workflows/conformance-smoke.yml | Justified |
| PR 4 Task 4.5 | Standard PR-orchestration | Justified |
| PR 5 Task 5.1 | §W-Refactor backlog 8 (deploy_providers refactor) | Justified |
| PR 5 Task 5.2 | §W-Refactor backlog 9 (ADR 010) + §Decision Records | Justified |
| PR 5 Task 5.3 | Standard PR-orchestration | Justified |
| PR 6 Task 6.1 | §DO-Plugin #62 ctx threading | Justified |
| PR 6 Task 6.2 | §DO-Plugin #63 platform.ComputePlan | Justified |
| PR 6 Task 6.3 | Standard PR-orchestration | Justified |
| PR 6b Task 6b.1 (Enumerator impl) | §DO-Plugin §Files: implement opt-in `interfaces.Enumerator.EnumerateByTag` | Justified (split into 6b is plan refinement of design's "rolled into DO-Plugin PR" — split required because PR 4 must land first per opt-in Enumerator dependency, surfaced in design §Sequencing) |
| PR 6b Task 6b.2 | Standard PR-orchestration | Justified |
| PR 7 Task 7.0a/b/c (tag cuts) | §Phase 3 + §Workflow tag-cut sequencing (per-attack #10) | Justified (orchestrator-side coordination per adversarial plan review C-2) |
| PR 7 Task 7.1 (pin bumps) | §Phase 3 step 4 | Justified |
| PR 7 Task 7.2 (re-run CI + admin-merge) | §Phase 3 step 5 + step 6 | Justified |
| PR 8 Task 8.1 (branch + plan preview) | §Phase 3 step 8 + §TC2 Execution §Pre-flight | Justified |
| PR 8 Task 8.2 (cascade-replace + /healthz verify + transcript marker) | §Phase 3 step 9 + §TC2 Execution §Cascade command + §Post-cutover verification | Justified (the `docs/cutovers/...md` marker file refines design's "with pre/post resource ID capture in body" by storing audit trail in-repo — within scope) |
| PR 8 Task 8.3 (PR + admin-merge) | §Phase 3 step 10 | Justified |
| Coord-1 / Coord-2 / Coord-3 | §Phase 3 tag cuts + §Workflow tag-cut sequencing | Justified |

**Drift Items:** none

**Notes (non-drift):**
- Plan rev2 splits design's single "DO-Plugin PR" into PR 6 (#62 + #63) and PR 6b (Enumerator impl) because PR 6b depends on PR 4 Task 4.1 (Enumerator interface) reaching workflow main. This split is consistent with design rev3 §DO-Plugin §Sequencing ("opt-in interface -> no cross-plugin gate failure when W-Cleanup workflow PR pushes; DO-Plugin PR can land independently of W-Cleanup PR; the type-assertion at use site means `wfctl infra cleanup --tag` works against DO once both PRs land in any order") and refines rather than rescopes. Tag-cut Coord-2 follows PR 6b merge per design's "Cut DO plugin v0.10.1 (rolls up DO-Plugin PR)".
- Plan correctly inverts design's "fail-CI shape recommended; t.Skip alt-shape documented" to ship `t.Skip` as the chosen shape, citing adversarial plan review I-5 + design rev3 §W-Diagnose-540 §Behavior allowance. Design accommodates this inversion.
- Task 8.2 omits the design's §Expected stdout sketch deliberately, citing adversarial plan review C-3 (sketches not verified against actual binary erode credibility). Replacement: capture full verbatim transcript into `docs/cutovers/...md`. Within design §Post-cutover verification scope.

Plan is ALIGNED with design. Locked for subagent-driven-development.
