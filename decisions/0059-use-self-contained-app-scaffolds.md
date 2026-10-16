# 0059. Keep default application scaffolds self-contained

**Status:** Accepted
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0057/0058; source Task2C; PR1022.

## Context

Actual Go1.27.1 wfctl-generated API application compiles with the disclosed
local source replacement, then exits1: unknown observability.telemetry and
observability.collector module types. Both belong to the external observability
plugin, not plugins/all. API/full-stack default YAML includes them without
plugin installation/discovery; event-processor does not. The source lock is
2026-10-04T05:30:50Z, SHA prefix cf1acc282b28, three tasks/one PR.

## Decision

Under standing explicit autonomous-change approval, Task2 additionally owns
API/full-stack workflow.yaml templates. Remove only their optional external
observability modules from default YAML; retain HTTP/UI/health/application
behavior. State the opt-in external-plugin boundary in existing template
READMEs. Add a real generated-config assertion that every default application
module is provided by the same existing default-plugin registry used in main.
Keep three tasks/one PR; recheck alignment/relock before edits and rerun the
actual generated API config without removing modules only in the proof.

## Alternatives And Consequences

Installing a plugin only in the test would hide a broken default app. Embedding
a new external-plugin installation/loading system in Go maintenance is broader
than this repair. Substituting different telemetry types would change semantics.
Core scaffolds start without an undocumented plugin prerequisite; advanced
telemetry remains available through its owning plugin and explicit application
composition. No observability-plugin feature change, protected CI write,
production deployment, or broader scaffold dependency modernization.
