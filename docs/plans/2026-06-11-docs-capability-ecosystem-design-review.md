### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-06-11-docs-capability-ecosystem-design.md`
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- `D1` [user-intent drift] [§4 lines 128-145]: initial design did not explicitly preserve the prior requirement that public Go docs generate from released versions and support version navigation. Recommendation: add release-vs-local source metadata and website version-navigation requirements. _Resolution: fixed in design lines 136-140 and 163-164._

**Findings (Minor):**
- `D2` [over-broad scope risk] [§Phasing lines 196-222]: full user ask spans Workflow, website, and ~80 plugin repos; executing it under one lock would make review/release boundaries unmanageable. Recommendation: lock Phase 1 only, then create follow-on locked phases. _Resolution: accepted; Phase 1 plan excludes website/plugin edits and records follow-up handoff tasks._
- `D3` [missing failure mode] [§4 lines 142-144]: Go-doc generator could accidentally scan stale worktrees nested in plugin repos. Recommendation: explicit ignore list for `.worktrees`, `_worktrees`, `.git`, `vendor`, `node_modules`, generated docs. _Resolution: included in design and plan Task 3._

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Design follows `docs/AGENT_GUIDE.md` and `docs/REPO_LAYOUT.md` boundaries. |
| Assumptions under attack | Finding | D1 addressed released-version assumption. |
| Repo-precedent conflicts | Clean | Extends existing `capability/inventory` and `cmd/wfctl` surfaces from PR #906. |
| Artifact-class precedent | Clean | Generated docs remain under `docs/generated`; plans stay under `docs/plans`. |
| YAGNI violations | Clean | Crossrefs and Go-docs are directly requested and feed website docs. |
| Missing failure modes | Finding | D3 addressed stale nested worktree scan risk. |
| Security / privacy at architecture level | Clean | Generator reads manifests/source docs only; no plugin binary execution or secret reads. |
| Infrastructure impact | Clean | Phase 1 has no production deploy/resource changes. |
| Multi-component validation | Clean | Design separates Workflow emitter from website consumer and requires later website integration proof. |
| Rollback story | Clean | Phase 1 is CLI/generated artifacts only; rollback is revert PR/release. |
| Simpler alternative not considered | Clean | Linking only to pkg.go.dev was considered insufficient because docs need Workflow-specific capability crossrefs. |
| User-intent drift | Finding | D1 fixed; phasing preserves broad ask without pretending one PR completes all repos. |
| Existence / runtime-validity | Clean | Design extends existing `wfctl capability` and docs-generation surfaces; plan verifies commands. |

**Options the author may not have considered:**
1. Website-only parsing: simpler short-term, rejected because it duplicates Workflow semantics and cannot safely guide agents/apps.
2. Hand-curated plugin matrix: easy to write once, rejected because it drifts immediately across ~80 plugins.

**Verdict reasoning:** PASS after D1 design patch. Remaining issues are managed by Phase 1 scope and explicit follow-on phases.

