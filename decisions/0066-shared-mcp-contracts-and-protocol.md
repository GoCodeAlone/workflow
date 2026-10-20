# 0066. Correct MCP contracts before upgrading the protocol SDK

**Status:** Accepted
**Date:** 2026-10-20
**Related:** [Design](../docs/plans/2026-10-19-shared-mcp-design.md),
[locked two-PR/six-task plan](../docs/plans/2026-10-19-shared-mcp.md),
[independent review](../docs/plans/2026-10-19-shared-mcp-review.md).

## Context

At the PR1 baseline, Workflow already served local MCP through wfctl and
mcp-go v0.54.1, reflecting earlier SDK maintenance. Legacy clients initialized
successfully, but permissive tool inputs, discarded adapter
errors, engine failures lacking isError, and SDK-default effect annotations
weakened the advertised contract. Catalog validation is not runtime acceptance.
The assistant packaging work is independent and must remain unchanged.

## Decision

Keep two reviewable PRs. PR1 enforces schemas and string arrays, preserves
adapter/engine errors, sorts inventories and supplies explicit effect annotations.
Successful text outputs and analytical negatives retain their semantics. Reuse
the existing SDK dispatcher and stdio transport.

Prove the four legacy revisions through a newly built CLI. Prove cooperative
cancellation separately with an entered blocking engine-provider test double
through real SDK stdio. SIGTERM cancellation exits 1 and must terminate within
the test deadline. PR1 makes no July support claim. The second PR upgrades the
existing SDK to v1.3.0, preserving the four legacy initialization revisions while
adding the stateless 2026-07-28 model. July requests carry namespaced protocol and
capability metadata in `params._meta`, without initialization. Ordinary results
carry complete result metadata; discovery/list/read results use zero-TTL private
cache hints, including local full-reference content. Private/zero values are a
local-data policy, distinct from required protocol field presence.

## Verification

Task 5 proved both eras with the clean source build at
`3de76e814725a15b7789abdea5d5d1d0056b20e7`, SDK v1.3.0, executable SHA256
`a810dbdb2b58fc5f5f702dfe4e03e6795fb1e38df17bc9ced9e5d9f85f964c73`.
The built-CLI matrix passed with isolated public fixtures; independent spec and
quality reviews passed. No real engine workflow was executed: deterministic
cooperative cancellation used a controlled entered-handler double, while the
production CLI proved cancellation-notification robustness and continued service.

Artifact rollback also passed: candidate to the PR1 source at `38fa0d07`
(SDK v0.54.1; four legacy revisions pass, stateless July discovery returns
`-32601`), then back to candidate July/mixed requests. This used isolated
binaries without resetting branches or changing installed sessions.

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

Protocol metadata requirements, OpenAI directory annotation policy and optional
capabilities are separate concerns. Text-only results remain; no directory/host,
HTTP/authentication, tasks/UI or Compute certification follows from wire proof.
Further input/output budgets, filesystem confinement, rate limits and general
redaction remain deferred rather than claimed as generic security compliance.
