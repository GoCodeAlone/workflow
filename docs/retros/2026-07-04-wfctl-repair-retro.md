# Retro: wfctl Repair

**PR:** #994 - feat(wfctl): add guarded repair command
**Merged:** 2026-07-04
**Branch:** feat/wfctl-repair-lifecycle
**Design:** docs/plans/2026-07-04-wfctl-repair-design.md
**Plan:** docs/plans/2026-07-04-wfctl-repair.md
**Related ADRs:** decisions/0053-top-level-wfctl-repair.md

## Adversarial-review findings, scored

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design | D1: keep repair from growing into a general repair registry | Minor | Resolved upfront |
| design | D2: relock succeeds but install fails can leave partial local state | Minor | Resolved upfront |
| design | D3: downstream editor/IDE validation belongs in a separate phase | Minor | Prescient |
| plan | P1: docs verification used grep rather than rendered preview | Minor | False positive |
| plan | P2: docs depended on Task 2 flag names | Minor | Prescient |
| plan | P3: local plugin cache rollback cannot recover deleted/corrupt caches | Minor | Resolved upfront |

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Corrupt `.wfctl-lock.yaml` initially planned no automatic repair | adversarial-design-review (plan) | The failure-mode scan covered missing/stale/incomplete locks but not unparseable locks. | Include corrupt/unparseable state in lifecycle repair test matrices. |
| `--workflow` help text implied a fallback repair behavior not implemented | finishing Step 1e / doc-reconciliation | Docs and CLI text changed, but the PR body did not include the required `Doc-reconciliation:` accountability line. | Treat help text as docs and require identifier/behavior reconciliation before PR body publication. |

CI did not expose additional gate misses. PR CI, post-merge main CI, Benchmark, CodeQL, OSV, Pre-release Snapshot, and the `v0.84.6` release workflow completed successfully.

## Missed skill activations

The canonical activation log exists at `.claude/autodev-state/in-progress.jsonl`, but it does not contain this 2026-07-04 run. Artifact evidence below is from committed docs, PR metadata, and observed workflow runs.

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes | Design artifact includes approach comparison and self-challenge. |
| adversarial-design-review (design) | yes | `docs/plans/2026-07-04-wfctl-repair-design-review.md` committed. |
| writing-plans | yes | `docs/plans/2026-07-04-wfctl-repair.md` committed. |
| adversarial-design-review (plan) | yes | `docs/plans/2026-07-04-wfctl-repair-plan-review.md` committed. |
| alignment-check | yes | `docs/plans/2026-07-04-wfctl-repair-alignment-check.md` committed. |
| scope-lock | yes | Scope lock was verified and completed before PR. |
| finishing-a-development-branch | partial | PR was opened with verification evidence and monitored, but Step 1e accountability line was missing. |
| finishing Step 1e (doc-reconciliation) | unverified | Diff touched README/docs; PR body lacks `Doc-reconciliation:` line. |
| pr-monitoring | yes | Copilot comments were addressed, replied to, resolved, and CI was re-run green before merge. |
| post-merge-retrospective | yes | This retro. |

## What worked

- The command stayed narrow: `repair` orchestrates existing `plugin lock` and `plugin install` rather than adding provider-specific repair behavior.
- Dry-run and injected-runner tests made apply ordering easy to verify.
- Copilot review found two concrete issues before merge, and both were fixed with focused tests/docs.
- Downstream release automation propagated `v0.84.6` through Homebrew, workflow-editor, VS Code, JetBrains, registry, and scenarios.

## What didn't

- The initial test matrix did not include corrupt lockfiles even though `doctor` already reports lock parse failures.
- The PR body missed the explicit doc-reconciliation marker, making the doc/help-text gate harder to audit later.
- The activation log did not capture this run, so the retro had to infer skill activation from committed artifacts.

## Plugin-level follow-ups

No plugin-level changes are warranted from this single PR. The useful durable practice is local to lifecycle repair features: include missing, stale, incomplete, and corrupt state in planner tests.

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| `docs/design-guidance.md` | no change | No central guidance file exists, and this PR produced no durable cross-design lesson beyond existing wfctl lifecycle boundaries. |
