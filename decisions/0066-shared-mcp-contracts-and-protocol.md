# 0066. Correct MCP contracts before upgrading the protocol SDK

**Status:** Accepted
**Date:** 2026-10-20
**Related:** [Design](../docs/plans/2026-10-19-shared-mcp-design.md),
[locked two-PR/six-task plan](../docs/plans/2026-10-19-shared-mcp.md),
[independent review](../docs/plans/2026-10-19-shared-mcp-review.md).

## Context

Workflow already serves local MCP through wfctl and mcp-go v0.54.1. Legacy
clients initialize successfully, but permissive tool inputs, discarded adapter
errors, engine failures lacking isError, and SDK-default effect annotations
weaken the advertised contract. Catalog validation is not runtime acceptance.
The assistant packaging work is independent and must remain unchanged.

## Decision

Keep two separately reviewable PRs. First enforce existing input schemas and
string arrays, preserve protocol/tool errors through the in-process adapter,
mark engine failures as errors, sort inventories, and describe actual effects
with explicit annotations. Preserve successful text outputs and analytical
negative reports. Reuse the existing SDK dispatcher and stdio transport rather
than introducing another server or protocol implementation.

Prove the four legacy revisions through a newly built CLI. Prove cooperative
cancellation separately with an entered blocking engine-provider test double
through real SDK stdio. Current SIGTERM cancellation exits 1 and must terminate
within the test deadline. The second PR upgrades the existing SDK and separately
proves July 2026 plus legacy behavior; this PR makes no July support claim.

## Consequences

Contract corrections can reject previously accepted malformed calls; valid
payloads retain their behavior. Read-only previews include modernize fixes
returned as text. Path/import readers remain open-world, and engine embedders
own authorization. Tests use public fixtures, minimal environments and isolated
processes; they neither execute a real engine workflow nor certify a host.

A combined upgrade was rejected because contract regressions and protocol
regressions need independent review and rollback. A new framework, hosted
transport, tasks/auth/UI, structured output migration and packaging changes
remain outside scope. Revert each PR independently and rebuild to roll back;
no installed session or persistent state migration is involved.
