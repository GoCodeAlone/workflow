# Workflow Assistant release smoke plan

**Goal:** Classify the verified non-sensitive Sonic startup diagnostic while
preserving strict smoke-client guards and truthful release evidence.

**Design:** `2026-10-20-workflow-assistant-smoke-design.md`.

## Scope Manifest

**PR Count:** 1
**Tasks:** 1
**Estimated Lines of Change:** ~150

**Out of scope:**
- Engine/SDK/dependency, transport, skill, workflow, permission or deployment changes.
- Retagging, restarting or republishing the v0.87.0 engine release.
- Blanket stderr suppression, source-version relabeling or expanded acceptance claims.
- Changes to completed MCP plans, unrelated locks or installed plugins/sessions.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Handle known release startup diagnostic in Workflow Assistant smoke | Task 1 | fix/workflow-assistant-release-smoke |

**Status:** Locked

### Task 1: Correct the bounded smoke diagnostic classification

Files: package `tests/main.go`, `tests/smoke_test.go`, `plugin.json`, `README.md`,
and this design/plan. Add no engine or workflow changes.

1. Extract the existing exact-deprecation-only stderr classification without
   changing behavior; confirm existing tests pass. Add adversarial tests and
   observe the Sonic-positive case fail before support is added.
2. Add exact Sonic classification and explicit symbolic statuses. Reject unknown,
   suffixed/prefixed, injected and malformed lines without echoing their content.
3. Bump package/client metadata to 0.1.1. Document both actually tested released
   CLIs, diagnostic handling and unchanged host/runtime limits. Attribute the
   existing Codex/Autodev host probe to historical package 0.1.0 evidence;
   package 0.1.1 smoke does not establish fresh host activation.
4. Run standalone race/vet checks; run real released v0.86.2 and v0.87.0 smokes;
   verify strict wrong-release/source-pseudo-version failures. Preserve the
   existing actual v0.87.0 five-version wire evidence; runtime source is unchanged.
5. Complete independent spec/quality review, privacy/scope audit, clean package
   export and ordinary draft-PR CI. Keep the original 0.1.0 export immutable.

Rollback: revert the package-only follow-up. No installed or engine release state
is changed by this task.
