# Retro: wfctl doctor Lifecycle Diagnostics

**PR:** #992 - Add wfctl doctor lifecycle diagnostics
**Merged:** 2026-07-04
**Branch:** feat/wfctl-lifecycle-evaluation
**Design:** docs/plans/2026-07-04-wfctl-doctor-design.md
**Plan:** docs/plans/2026-07-04-wfctl-doctor.md
**Related ADRs:** decisions/0052-top-level-wfctl-doctor.md

## Adversarial-review findings, scored

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design | Keep first slice read-only; resist auto-repair growth. | Minor | Resolved upfront |
| design | Normalize plugin install names consistently with install/list/remove layout. | Minor | Resolved upfront |
| design | Add real CLI smoke, not only command-function tests. | Minor | Resolved upfront |
| plan | JSON smoke should assert useful report fields, not only parseability. | Minor | Resolved upfront |
| plan | Final verification task bundles multiple gates. | Minor | False positive |
| plan | Online update test uses release URL override instead of live GitHub. | Minor | False positive |
| plan | Task heading format must satisfy scope-lock parser. | Minor | Resolved upfront |

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Copilot caught the OK plugin diagnostic saying "installed at" followed by a version string. | requesting-code-review | Local review checked behavior and failure modes but did not read user-facing success text for semantic clarity. | Add explicit "operator-facing diagnostic wording" to local review notes when CLI output is a feature surface. |

No CI failures slipped past local verification. Both PR CI runs and the main-branch merge-commit CI completed green.

## Missed skill activations

Activation log note: `in-progress.jsonl` exists but is stale for this Codex run, so this table uses transcript and committed artifact evidence.

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | Produced the read-only top-level doctor slice. |
| project-design-guidance | yes | Portfolio and repo guidance were read before design. |
| adversarial-design-review (design) | yes | Committed at docs/plans/2026-07-04-wfctl-doctor-design-review.md. |
| writing-plans | yes | Committed at docs/plans/2026-07-04-wfctl-doctor.md. |
| adversarial-design-review (plan) | yes | Committed at docs/plans/2026-07-04-wfctl-doctor-plan-review.md. |
| alignment-check | yes | Committed at docs/plans/2026-07-04-wfctl-doctor-alignment-check.md. |
| scope-lock | yes | Scope was locked before implementation and completed before PR. |
| test-driven-development | yes | Doctor tests were written before implementation; missing-manifest and review-fix regressions were added with fixes. |
| finishing Step 1e (doc-reconciliation) | yes | PR body recorded `Doc-reconciliation: clean`. |
| pr-monitoring | yes | CI was watched, Copilot feedback was addressed, thread was resolved, and PR was admin-merged after green checks. |
| post-merge-retrospective | yes | This file. |

## What worked

- Scope lock prevented the plan parser issue from hiding until PR time; the heading format was corrected before implementation.
- Runtime smoke caught wrapped not-found errors turning a missing plugin manifest into `ERROR` instead of `WARN`.
- Copilot review found a real user-facing wording defect, and the fix was small, tested, and re-verified.
- Main-branch CI on the squash merge completed green before the retro was written.

## What didn't

- The local adversarial review was too behavior-focused and missed a confusing success message.
- The activation log did not capture the Codex run, so skill-fire evidence had to be reconstructed from committed artifacts and transcript state.
- The first PR merge command completed the server-side merge but failed local cleanup because another worktree owned `main`.

## Plugin-level follow-ups

No plugin-level change yet. The output-wording miss is useful signal, but one instance does not justify changing the shared review checklist.

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| docs/design-guidance.md | no change | No durable cross-design lesson; the follow-up is local review discipline for CLI diagnostic wording. |
