# 0052. Add top-level wfctl doctor

**Status:** Accepted
**Date:** 2026-07-04
**Decision-makers:** Codex
**Related:** `docs/plans/2026-07-04-wfctl-doctor-design.md`, `docs/plans/2026-07-04-wfctl-doctor.md`

## Context

`wfctl` already has lifecycle commands for validation, plugin lock/install,
updates, capabilities, infra, secrets, CI, and audit. Operators still need to
know which command to run next when a checkout is partially configured,
lockfiles drift, or plugins are missing. Adding provider-specific work to core
would violate the plugin boundary; adding only docs repeats the recent install
doc fix without improving CLI discoverability.

## Decision

Add a top-level read-only `wfctl doctor` command. It will inspect installation
state, project config, `wfctl.yaml`, `.wfctl-lock.yaml`, installed project
plugins, optional global plugins, and optional online update state. Rejected:
plugin-only `wfctl plugin status`, because it ignores binary/project setup;
new lifecycle orchestrator, because mutating repair flows are premature.

## Consequences

Users get one diagnostic entry point with concrete repair commands. Existing
commands remain authoritative for mutation. The command must avoid network
access unless explicitly requested and must not hide provider/plugin
responsibilities inside core. Rollback is simple: remove the command wiring,
docs, and tests; existing lifecycle commands continue to work.
