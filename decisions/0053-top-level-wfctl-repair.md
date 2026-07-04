# 0053. Add guarded wfctl repair

**Status:** Accepted
**Date:** 2026-07-04
**Decision-makers:** Codex
**Related:** `decisions/0050-plugin-lock-install-modes.md`, `decisions/0052-top-level-wfctl-doctor.md`, `docs/plans/2026-07-04-wfctl-repair-design.md`, `docs/plans/2026-07-04-wfctl-repair.md`

## Context

`wfctl doctor` now identifies project/plugin lifecycle drift and prints concrete
fix commands. Repeating those commands manually is useful but still leaves a
partial-checkout footgun: users must infer ordering when a manifest, lockfile,
and installed plugin directory disagree. Provider-specific repair remains out
of scope for core per the plugin boundary.

## Decision

Add top-level `wfctl repair` as a guarded orchestrator over existing lifecycle
commands. Default mode is dry-run and emits the planned commands. `--apply`
executes only safe project plugin repairs: regenerate `.wfctl-lock.yaml` with
`wfctl plugin lock` when manifest/lock provenance is stale or incomplete, then
sync installed project plugins with `wfctl plugin install`. Binary self-update,
workflow generation, parse fixes, global plugin mutation, and provider-specific
repairs stay as suggestions.

## Consequences

Users get one command for the common doctor fix path while existing commands
remain authoritative for mutation. The CLI adds a small orchestration layer and
tests must guard dry-run/apply behavior. Rollback is simple: remove the command,
docs, and tests; `doctor`, `plugin lock`, and `plugin install` continue to work.
