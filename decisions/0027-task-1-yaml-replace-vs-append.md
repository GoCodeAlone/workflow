# 0027 — Task 1 YAML frontmatter: replace existing keys, do not append duplicates

**Status:** Accepted
**Date:** 2026-05-10
**Plan:** docs/plans/2026-05-10-strict-contracts-force-cutover.md (rev5, scope-locked at e82b7e0c)

## Context

Plan §Task 1 step 1 instructed prepending a new supersession status line and a populated `superseded_by: [...]` to two existing plan-doc frontmatter blocks (`2026-04-26-strict-grpc-plugin-contracts-design.md` and `…contracts.md`). The instruction did not account for these files already containing `status:` and `superseded_by:` keys at other lines (line 1 + line 57 respectively).

Faithful execution of the plan introduced YAML duplicate keys, which `wfctl audit plans` (gopkg.in/yaml.v3 in `cmd/wfctl/plan_audit.go`) rejects with `mapping key already defined at line N` errors. Code-reviewer + Copilot independently flagged this on PR #596.

## Decision

For Task 1's two frontmatter edits: REPLACE the existing `status:` and `superseded_by:` lines in place rather than appending duplicates at the top.

## Consequences

- PR #596 ships with single-occurrence keys, satisfying yaml.v3.
- `wfctl audit plans` returns clean (zero ERROR + zero WARN on the changed files).
- The plan body (§Task 1 step 1) describes the intent (mark old plan/design as superseded with pointer to cutover artifacts) but the technique (replace, not append) deviates from the literal step text. Documented here so future readers don't repeat the bug.
- Manifest hash unchanged (scope-lock §Scope Manifest section is untouched). No scope-lock unlock required per `skills/scope-lock/SKILL.md`.

## Alternatives rejected

- **(A) Plan-unlock + manifest amendment**: heavyweight for a doc-correction whose intent is preserved.
- **(C) Override + admin-merge with broken audit**: leaves an ERROR baseline in the canonical plan inspector. Unacceptable per `feedback_local_ci_validation_for_ci_touching_tasks`.

## Scope: bonus normalizations beyond Task 1's literal Files

This PR also normalizes frontmatter on:
- `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (status draft→approved, area plugins/iac→plugins, supersedes string→list)
- `docs/plans/2026-05-10-strict-contracts-force-cutover.md` (frontmatter added)
- 8 adversarial-review files (status complete→approved)

These were not in Task 1's Files section but were necessary to achieve the zero-WARN audit baseline that Task 1's verify command checks. Treated as fix-forward since the manifest is unchanged. Documented here for traceability.

The status values `superseded_partial` and `notice` from the original directives were not in `cmd/wfctl/plan_audit.go`'s allowed status set; substituted with `superseded` + custom `supersession_scope:` field (audit accepts unknown fields).

## Related

- PR #596 (docs/supersede-2026-04-26-design)
- Code-review comment chain on PR #596
- Copilot review comments 3214401586 + 3214401594
