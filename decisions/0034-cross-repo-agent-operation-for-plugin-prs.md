# 0034. Plugin-repo PRs run as autonomous cross-repo agent work, not human gates

**Status:** Accepted
**Date:** 2026-05-14
**Decision-makers:** Jon (operator), autonomous pipeline
**Related:** docs/plans/2026-05-14-cloud-sdk-extraction.md (PR 4), docs/plans/2026-05-14-cloud-sdk-extraction-design.md, decisions/0033-add-ctx-to-module-iac-state-store.md

## Context

The cloud-SDK-extraction plan's PR 4 (`workflow-plugin-azure` implements the `azure_blob` IaCStateBackend) lands in a *different git repository* than the worktree the subagent-driven pipeline runs in. The plan originally marked PR 4 a "HUMAN-GATE": the pipeline would pause and hand Tasks 11–12 to a human operator, on the conservative assumption that worktree-scoped subagents should not autonomously branch/commit/push/PR/tag in a second repo.

The operator rejected that framing. The whole extraction effort is inherently multi-repo — Phases B/C/D each touch `workflow-plugin-{aws,gcp,digitalocean}`, and the design already assumes "one PR per affected plugin." Treating every plugin PR as a human gate would make the autonomous pipeline barely autonomous. The operator's directive: agents should operate in those other repo contexts directly; the real requirement is not a human gate but **prompt clarity** — each cross-repo agent must be told unambiguously which repository it is working in.

## Decision

We will treat plugin-repo PRs (PR 4 here, and the analogous plugin PRs in the deferred B/C/D plan) as **normal autonomous cross-repo agent work**, not human gates. The plan's PR 4 row, its "human-action gate" paragraph, and the executor notes are updated accordingly.

The replacement requirement: every agent dispatched to do cross-repo work MUST receive, explicitly in its prompt, (a) the absolute path of the repository it operates in, (b) a statement that it is a *different* repo than the worktree, and (c) which repo each file path belongs to. The push + PR-creation steps still follow normal review discipline (feature branch, PR for review — never direct-to-default-branch), and a published release tag is still a deliberate, called-out step — but none of that requires pausing for a human to *perform* the work.

Alternatives rejected:
- **Keep the human gate.** Rejected by the operator — it defeats the autonomous pipeline for an inherently multi-repo effort.
- **A single mega-worktree spanning all repos.** Rejected — the repos are independently versioned and released; conflating them breaks per-repo PR/review/tag boundaries.

## Consequences

- **Easier:** PR 4 (and B/C/D plugin PRs) execute autonomously; no operator hand-off mid-pipeline. The pipeline is genuinely autonomous end-to-end.
- **Easier:** consistent pattern for every plugin repo across all phases — no per-PR "is this a gate?" judgment.
- **Harder / risk:** an agent operating in the wrong repo is now a live failure mode. Mitigated by the mandatory prompt-clarity requirement (absolute repo path + explicit "different repo" callout in every cross-repo dispatch) and by the orchestrator verifying `git -C <repo> log` after cross-repo commits.
- **New constraint:** cross-repo agent prompts have a fixed preamble obligation (repo path + scope). The orchestrator owns enforcing it.
- **Unchanged:** push/PR still go through review; a published plugin release tag is still an explicit, deliberate step (PR 5 depends on PR 4's tag) — autonomy here means the agent *performs* the steps, not that review/release discipline is skipped.
