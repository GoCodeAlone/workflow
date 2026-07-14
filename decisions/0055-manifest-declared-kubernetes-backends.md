# 0055. Declare Kubernetes backends in plugin manifests

**Status:** Accepted
**Date:** 2026-07-14
**Decision-makers:** Jon (operator), Codex autonomous pipeline
**Related:** `decisions/0037-gke-cross-process-contract.md`, `config/plugin_manifest.go`, `module/platform_kubernetes_grpc.go`

## Context

ADR 0037 established `ResourceDriver` as the cross-process contract, but made
GKE-specific host choices: one fixed backend/resource type, host-built GCP
addressing, and host credential injection. That shape cannot support multiple
providers without teaching the framework each provider's names and semantics.
Workflow must remain a provider-neutral framework while retaining its existing
`platform.kubernetes` lifecycle and typed state. We also need a staged migration
that does not strand current AKS/EKS users before provider plugins publish and
prove their replacements.

## Decision

Plugins declare Kubernetes bindings as exact `{name, resourceType}` manifest
entries and provide a `ResourceDriverClient` for every declared name, with no
missing or extra runtime clients. An external plugin must advertise the
`ResourceDriver` service and every exact declared resource type in live
capabilities before the adapter yields a complete `{name, resourceType, client}`
binding. The engine validates parity and collisions before loader mutation,
stores bindings in an engine-scoped registry, and publishes only a read-only
resolver. `kind` and `k3s` remain reserved core-only backends. SDK-free AKS/EKS
implementations remain temporary compatibility fallbacks and are used only when
no plugin owns the exact name.

The generic host maps plan/status to `Read`, apply to `Create`, and destroy to
`Delete`, carrying the declared resource type in each request, ref, and spec. It
passes user-authored config unchanged, performs no credential resolution or
provider-specific `ProviderId` construction, and projects generic output fields
into `KubernetesClusterState`. Provider plugins own credentials, addressing,
defaults, validation, and provider-specific output production. We reject a
fixed host provider table and a new Kubernetes-specific proto because both
duplicate ownership or the existing `ResourceDriver` seam.

This decision supersedes ADR 0037.

## Consequences

New backend names require no Workflow code change, and malformed or conflicting
plugins fail before becoming visible. Existing `kind`/`k3s` behavior is stable;
AKS/EKS fallback removal waits for provider-side migration and proof.

Release order is deliberate: Workflow v0.86 publishes the manifest, verifier,
and provider-neutral host contract; provider repositories then release matching
declarations and runtime drivers; Workflow v0.87 may complete the compatibility
cutover after those releases are consumable. GKE follows the same window, with
provider migration/proof owned by Task 14. Mixing versions before the provider
release can leave a configured backend unavailable, so downstreams must upgrade
in that order. Rollback pins the matched Workflow/provider versions; it does not
restore provider-specific behavior to core.
