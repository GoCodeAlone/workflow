# 0043. Derive IaC Through Provider Requirements

**Status:** Accepted
**Date:** 2026-05-25
**Decision-makers:** Workflow maintainers, autonomous pipeline
**Related:** `docs/plans/2026-05-25-iac-derived-requirements-design.md`

## Context

Workflow needs to derive infrastructure for higher-level application and
observability declarations without hard-coding DigitalOcean, AWS, GCP, Azure,
Datadog, Grafana, Prometheus, or Loki behavior in core. Existing
`moduleInfraRequirements` is useful but static and manifest-only. Existing IaC
provider plugins already expose strict typed gRPC services, with optional
services advertised by registration. The user also requires explicit YAML keys
for user-provided overrides and strict proto compatibility where possible.

## Decision

We will add a core requirement model and `wfctl infra derive`, but provider
plugins will own requirement-to-resource mapping through an optional strict-proto
IaC service. Generated modules will include `satisfies` keys, and manually
written modules can use the same keys to suppress derivation. We reject a
provider-specific CLI plugin command because YAML mutation and cross-provider
plugin discovery belong in `wfctl` core. We reject apply-time derivation because
it hides generated infrastructure from review and CI. We reject core-owned
provider mapping because it would recreate provider-specific assumptions in the
framework.

## Consequences

Derivation becomes reviewable, idempotent, and reusable for observability, web
apps, message brokers, databases, caches, and storage. Provider plugins gain a
small but real new compatibility surface and must test their mappings. Workflow
core must maintain a YAML node editor and a stable requirement proto. Older
provider plugins keep working for explicit `infra.*` YAML but cannot derive
resources until they implement the optional mapper service.

