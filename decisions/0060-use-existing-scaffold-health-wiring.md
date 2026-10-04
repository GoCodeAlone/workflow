# 0060. Use existing health wiring in app defaults

**Status:** Accepted
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0058/0059; source Task2C; PR1022.

## Context

Fresh real API startup now passes module initialization but rejects the default
GET /health route: health.checker registers a health service, not an HTTP
handler under the module name. All three application templates use that invalid
route. The existing observability hook already mounts health/readiness/liveness
handlers from HealthChecker paths. Current source lock is
2026-10-04T05:51:09Z, SHA prefix698e28dd8c0e, three tasks/one PR.

## Decision

Preserve the intended /health endpoint by configuring healthPath: /health on
the existing health.checker and removing only its invalid manual route. Use the
already loaded observability wiring, not a fake generic handler or new core API.
Under standing autonomous-change approval, add the event-processor YAML owner
to Task2 alongside its two existing YAML owners. Assert all generated health
paths/routes first, then rerun actual API/health/shutdown. Three tasks/one PR and
protected exclusions remain; no application feature is removed.

## Consequences

Generated defaults use the same real health behavior as the existing plugin;
their documented endpoint remains /health. Test-only route deletion is rejected.
No new health framework, direct module service alias, external-plugin loader,
or native Windows graceful-lifecycle claim is introduced.
