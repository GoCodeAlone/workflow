# Retro — MCP tool metadata accuracy (#856)

**Date:** 2026-06-04
**PR:** GoCodeAlone/workflow#856 (merged be86ad60f)
**Design:** docs/plans/2026-06-04-mcp-tool-metadata-accuracy-design.md
**Plan:** docs/plans/2026-06-04-mcp-tool-metadata-accuracy.md

## What shipped

The two #854 follow-ups, one PR, no behavior change, no release:
- **F1** — declared `phase_config_yaml` + `wfctl_version` (optional) on the `generate_github_actions` tool schema (the handler already read them; now MCP clients can discover/pass them). Schema-properties test locks all 5 params.
- **F2** — corrected the drifted `scaffold_environment` (`target`/`config` → real `provider`/`environments`/`secrets_provider`/`exposure`) and `scaffold_infra` (fake `opentofu/terraform` provider → real cloud `aws/gcp/azure/digitalocean`) entries in `docs/mcp-tools-reference.md`.

## What the gates caught

- **Plan adversarial** executed every step live before approving (find blocks match, test fails-then-passes, runtime `tools/list` returns the params) — highest-confidence plan review of the session.
- **Copilot** found 2 real omissions: (1) I changed the `generate_github_actions` *schema* (F1) but left its *ref-doc entry* listing only 3 params — schema↔doc inconsistency I introduced; (2) my `scaffold_environment` doc purpose said "region" (copied from the def's stale `WithDescription`), but `handleScaffoldEnvironment` emits no region field. Both fixed.

## Lessons

- **When you change a tool's schema, update its doc entry in the same PR.** F1 added params to the schema; the matching `docs/mcp-tools-reference.md` entry (added in #854) wasn't in the plan's task list, so it went stale the moment F1 landed. A "schema change" implicitly includes its human-doc twin. → the plan should have listed the gha ref entry alongside Task 2.
- **Don't copy a def's description prose without checking the handler.** The `scaffold_environment` def `WithDescription` claims "region"; the handler emits none. I propagated the inaccuracy into the doc. Same root cause as #854's lesson ([[feedback_describe_mcp_tool_not_cli]]): describe what the handler *does*, verified against the handler.
- **Compound `git add && commit && push` tripped the force-push guard hook** (false match on the long command containing `git push`) and blocked the whole call before anything ran. Run commit and push as separate Bash calls under the autonomous pipeline. ([[feedback_force_push_blocked_use_forward_fix]] — adjacent: the hook is conservative about `git push` in compound commands.)

## Follow-ups (logged)

- `scaffold_environment` def `WithDescription` (`mcp/scaffold_tools.go:44`) still says "region" — the handler emits no region field. One-word def fix (left out of this PR's locked scope; the ref-doc was the flagged surface).
- Broader `config`-vs-`yaml_content` ref-doc drift persists for `api_extract`/`detect_project_features`/etc. (out of scope here).

## Gate tally

design adversarial 1 cycle PASS (1 advisory nit: exposure "for local") + plan adversarial 1 cycle PASS (ran every step live) + alignment PASS + scope-lock + code review + Copilot 2 findings addressed. 21/21 CI green → admin-merge.
