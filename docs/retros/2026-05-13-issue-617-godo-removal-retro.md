# Retro: Issue #617 — Remove godo (DigitalOcean SDK) from workflow core

**PR:** [#654](https://github.com/GoCodeAlone/workflow/pull/654) — feat(#617): remove godo (DigitalOcean SDK) from workflow core
**Merged:** 2026-05-13 (sha `c55a56e5`)
**Branch:** `feat/issue-617-godo-removal`
**Design:** `docs/plans/2026-05-13-issue-617-godo-removal-design.md` (3 adversarial cycles, PASS at cycle 3)
**Plan:** `docs/plans/2026-05-13-issue-617-godo-removal.md` (8 adversarial cycles, PASS at cycle 8)
**Related ADRs:** none new (force-cutover precedent cited from `feedback_force_strict_contracts_no_compat.md`)

## Adversarial-review findings, scored

### Design phase (3 cycles → PASS)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design c1 | C-1 step.do_* migration error gap | Critical | **Prescient** — without it, step types would have hit generic schema error post-merge |
| design c1 | I-1 step.do_logs/scale capability gap | Important | **Prescient** — workaround documented; 2 follow-up plugin issues filed pre-merge |
| design c1 | I-2 migration error misleading for plugin-loaded users | Important | **Prescient** — branching now correct; field-tested in tests |
| design c2 | I-1 platform_doks_test.go missing from inventory | Important | **Resolved upfront** — single-line fix prevented build break |
| design c2 | m-1 wfctl modernize flag `--write` vs `--apply` | Minor | **Resolved upfront** — would have shipped broken docs |
| design c2 | m-2 example/go.mod tidy missing | Minor | **Resolved upfront** — would have left transitive godo in example sub-module |
| design c3 | m-1 grep gate `\|\| true` silent no-op | Minor | **Resolved upfront** — CI gate would not have failed |
| design c3 | m-2 plugin-loaded detection over-specified | Minor | **Resolved upfront** — implementation simplified |

### Plan phase (8 cycles → PASS)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| plan c1 | C-1 invented `workflow.NewEngine` / `RegisterModuleFactory` APIs | Critical | **Resolved upfront** — would not have compiled |
| plan c1 | C-2 invented `module.CreateStep` API | Critical | **Resolved upfront** — would not have compiled |
| plan c1 | I-1 invented `buildTypeRegistry()` helper | Important | **Resolved upfront** — caught before T2 test |
| plan c1 | I-2 atomic.Bool global race risk | Important | **Resolved upfront** — switched to per-registry instance field |
| plan c1 | I-3 do_networking gap test missing | Important | **Resolved upfront** — added |
| plan c2 | C-1 modernize Fix doesn't inject `config.provider` | Critical | **Prescient** — addressed via manual provider-add migration guide step + scope-limit (Option 2) |
| plan c2 | C-2 deploy.go platform.* collector missed | Critical | **Resolved upfront** — would have left dead path |
| plan c3 | C-1 mockLogger redeclaration | Critical | **Resolved upfront** — package conflict prevented |
| plan c3 | C-2 falsely claimed import cycle for version constant | Critical | **Resolved upfront** — single source of truth |
| plan c4 | C-1 ACTUAL import cycle module→plugin→modernize | Critical | **Prescient** — without it, T5 would not compile; led to creation of `internal/legacydo` leaf package |
| plan c5 | C-1 schema validation fires before factory guard | Critical | **Prescient** — without `WithExtraModuleTypes` injection, migration error unreachable from `BuildFromConfig` |
| plan c5 | I-1 `e.stepRegistry.SetIaCProviderLoaded` interface mismatch | Important | **Resolved upfront** — type-assertion pattern matches precedent |
| plan c6 | C-1a phantom `schema.WithExtraStepTypes` | Critical | **Resolved upfront** — step types don't need schema injection |
| plan c6 | C-1b wfctl validate.go + ci_validate.go unhooked | Critical | **Prescient** — AC3 satisfied across all 3 wfctl entry points (engine, validate, ci validate) |
| plan c7 | C-1 cfg.Pipelines is map[string]any not slice | Critical | **Resolved upfront** — yaml marshal/unmarshal pattern matches engine.go |
| plan c7 | I-1 missing automated test for validate-path migration error | Important | **Resolved upfront** — added two tests |
| plan c8 | (none) | — | **PASS** — minor signature-stub note acknowledged |

## Gate misses

Two distinct correctness bugs surfaced by Copilot after PR creation. Both should have been caught by adversarial-design-review --phase=plan but weren't.

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Modernize rule's `step.do_*` → `step.iac_*` auto-rewrite produced invalid configs because `step.iac_apply` requires `platform:` + `state_store:` keys, not the legacy `app:` key. 8 plan cycles flagged the module-side equivalent (cycle-2 C-1: provider key not auto-injected) but never extended the same scrutiny to step config-shape compatibility. | adversarial-design-review --phase=plan | The plan reviewer treated step migration as a "rename only" pattern parallel to module migration, but step configuration schemas differ across step types in this codebase. Cycle-2's manual-add solution was scope-limited to module config keys without the symmetric audit of step config shapes. | Add to plan-phase checklist: "for each auto-rewritten type, diff the source vs target type's required config keys; if non-overlap, the rewrite must either inject required keys or be marked non-fixable." |
| `module/godo_absent_test.go` used `filepath.Glob("*.go")` (non-recursive) — would not catch godo imports added in subdirectories of `module/`. The plan's T1 test stub was copied verbatim through 8 cycles. | adversarial-design-review --phase=plan | The plan reviewer verified the test compiled against actual APIs (go/parser/go/token correct) but did not verify the test's coverage breadth. "Does the test fire on the actual regression surface?" was outside the structural checklist. | Add to plan-phase checklist: "for each regression-gate test, verify the file traversal covers the full failure surface (recursive vs flat, includes subdirs the original symbols could re-enter from)." |

Both fixes shipped in Copilot review rounds 1 and 2 (commits 407c6a9f, d52429b2, 3f904517, d868c055). The PR still merged within ~50 min of CI start.

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | autonomous mode — skipped question rounds per user mandate |
| adversarial-design-review (design) | yes | 3 cycles |
| adversarial-design-review (plan) | yes | 8 cycles |
| alignment-check | yes | PASS first run after H2→H3 task heading fix |
| scope-lock | yes | applied; hash `7fcc5df5…` held through implementation |
| subagent-driven-development | yes | team of 3 (implementer + spec + code reviewer); all 5 tasks shipped clean |
| finishing-a-development-branch | yes | 1d scope check + test pass + runtime sanity (wfctl + server build) |
| pr-monitoring | yes | 2 cycles before merge (Copilot round-1 + round-2) |
| post-merge-retrospective | yes | this document |

No missed activations. Pipeline fired end-to-end.

## What worked

- **Iteration discipline.** 11 total adversarial cycles before code ran. Every cycle caught at least one real bug — none were rubber-stamps. The user mandate "cycle as many times as necessary" gave space for cycles 4-8 of plan-phase, which caught the real `module→plugin→modernize` import cycle (cycle 4 C-1) that earlier cycles asserted didn't exist.
- **internal/legacydo as a defensive refactor.** The leaf-package design surfaced by plan-cycle 4 turned out to be the right architecture; it survived 4 subsequent cycles + 2 Copilot rounds without churn.
- **Scope-lock held through Copilot rounds.** Both Copilot fixes were correctness changes (rule semantics, test coverage breadth), not new features. The manifest hash didn't drift.
- **`!`-prefixed CI grep gate worked first try.** "Verify godo is not imported" check passed on first PR run; no regression risk for future godo bumps.

## What didn't

- **Step config-shape symmetry overlooked.** The plan invested heavily in module config-key handling (cycle-2 manual-provider-add) but didn't audit step config-shape compatibility. Copilot round 1 caught it. Recurrence risk: every modernize rule that rewrites types across schemas.
- **Regression-test coverage breadth not verified.** `module/godo_absent_test.go` was reviewed for API correctness (go/parser usage) but not coverage breadth. Recurrence risk: any regression-gate test in a multi-level package.
- **Branch carried 2 pre-existing in-flight commits from local main.** Not a bug, but the PR was 22 commits vs. expected 20. Future autonomous branches should rebase onto `origin/main` before push if local `main` is ahead.

## Plugin-level follow-ups

Single retro; no plugin-level changes warranted yet. **Tentatively** record these patterns for the next 1-2 retros:

1. **Cross-schema rewrite audit** — if the next migration-rule retro also flags "auto-rewrite produced invalid config because source/target schemas differ," add a bug class to `adversarial-design-review --phase=plan` checklist.
2. **Regression-test coverage breadth** — if the next "missed import" retro recurs, add a bug class similarly.

Single occurrence of each. Trend, not action.
