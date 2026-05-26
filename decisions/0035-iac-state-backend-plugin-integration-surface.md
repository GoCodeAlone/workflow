# 0035. IaCStateBackend plugin-serve + host-resolve integration surface

**Status:** Accepted
**Date:** 2026-05-14
**Decision-makers:** Jon (operator), autonomous pipeline
**Related:** docs/plans/2026-05-14-cloud-sdk-extraction.md (PRs 4–5, + new PRs 7–8), docs/plans/2026-05-14-cloud-sdk-extraction-design.md, decisions/0033, decisions/0034

## Context

PRs 1/2/3/6 of the cloud-SDK-extraction plan merged to `workflow` main: the `IaCStateBackend` proto contract, the host-side scaffolding (`grpcIaCStateStore`, `iacStateBackendServer`, `module.iacStateBackendRegistry` + `IaCModule.Init` dispatch), and the `ctx`-widened `module.IaCStateStore`. The remaining plan work — PR 4 (`workflow-plugin-azure` serves `azure_blob`) and PR 5 (core deletes the in-core backend) — was then blocked by a gap the design and both adversarial-review passes missed: they vetted the design/plan *documents*, not the plugin-SDK and engine-plugin-loader *internals*.

A focused read of `plugin/external/sdk/`, `plugin/external/adapter.go`, `engine.go`, and `plugin/manifest.go` surfaced three concrete gaps:

1. **Plugin-serve.** `ServeIaCPlugin` → `registerIaCServicesOnly` is a fixed type-assertion cascade over the 7 `IaCProvider*` services + `ResourceDriver`. It has **zero `IaCStateBackend` awareness** — a plugin author has no hook to register that service. The plan's Task 11 ("register the IaCStateBackend service on the plugin's gRPC server") assumed a seam that does not exist.
2. **Backend-name advertisement.** The gRPC `ContractRegistry` tells the host a plugin serves *the `IaCStateBackend` service*, but not *which config-facing backend names* (`azure_blob`) it answers for. No path — manifest or RPC — carries that today. (The analogous IaC-*provider* name is read ad-hoc from `plugin.json` by a wfctl-side disk scan, not a host/engine mechanism.)
3. **Host-resolve.** `module.iacStateBackendRegistry` has **no exported `RegisterIaCStateBackend`** wrapper; `ExternalPluginAdapter` has the gRPC `Conn()` + `ContractRegistry()` accessors but no state-backend-names accessor; `engine.go`'s `loadPluginInternal` has no seam populating the registry. The plan's Task 14 was scoped as "wire `engine.go` to populate the registry" — that under-counts the real work (exported wrapper + adapter accessor + service-advertisement check + the loader seam).

## Decision

**Backend-name advertisement (operator decision).** `plugin.json` `capabilities.iacStateBackends: ["azure_blob", ...]` is the single authoring point; it is exposed at runtime via a **new `ListBackendNames` RPC on the `IaCStateBackend` service** (not on `IaCProviderRequired` — it is a state-backend concern and a future pure-storage plugin must still answer it). The engine calls the RPC for live truth and cross-checks the `ContractRegistry` that the `IaCStateBackend` service is actually registered. Rejected: manifest-only (a static file can drift/lie); RPC-only without a manifest authoring point (duplicates the value, no single source).

**One type carries both concerns (operator decision).** `ServeIaCPlugin(provider any)` takes a single object and type-asserts it. The Azure plugin's `azureIaCServer` will *also* implement `pb.IaCStateBackendServer` (delegating to a ported `AzureBlobIaCStateStore`) — one type carrying both the Azure-provider and the Azure-blob-state-backend concerns. Defensible: the Azure plugin genuinely is both. Rejected: refactoring `ServeIaCPlugin` to accept multiple served objects — a larger SDK change touching every plugin's `main.go`. Consequence: a *pure* storage plugin (no IaC provider) cannot use `ServeIaCPlugin` today, because `registerIaCServicesOnly` hard-requires `pb.IaCProviderRequiredServer` — recorded as a deferred limitation, not addressed here.

**Plan restructure.** The original Tasks 11–14 / PRs 4–5 were under-scoped. The plan is amended to:
- **PR 7 (workflow):** SDK serve hook (type-assert `pb.IaCStateBackendServer` in `registerIaCServicesOnly`) + `ListBackendNames` RPC added to `iac.proto` (regenerate) + `plugin.PluginManifest.iacStateBackends` field + the engine's manifest-read path. **Must merge before PR 4.**
- **PR 8 (workflow):** engine host-wiring — exported `module.RegisterIaCStateBackend` + `ExternalPluginAdapter.IaCStateBackendNames()` accessor + the `loadPluginInternal` optional-interface seam that calls `ListBackendNames`, cross-checks the `ContractRegistry`, and registers each name. May land in parallel with / just after PR 4.
- **PR 4 (cross-repo, `workflow-plugin-azure`):** unchanged in intent (port `AzureBlobIaCStateStore`, implement `pb.IaCStateBackendServer` on `azureIaCServer`, declare `capabilities.iacStateBackends: ["azure_blob"]`, release a plugin tag) — now depends on PR 7.
- **PR 5 (workflow):** core deletes `iac_state_azure.go` + strips the `azure_blob` case + drops `azure-sdk-for-go` from `go.mod` + the migration doc — depends on PR 4's plugin tag and PR 8.

## Consequences

- **Unblocks PR 4/5** with a fully-specified, gap-free integration path — no more "assumed seam doesn't exist" surprises.
- **Cost:** the plan grows 6→8 PRs / 15→~18 tasks; another scope-lock amendment cycle (manifest update → re-alignment → re-lock). One additive RPC on the already-merged `iac.proto` (more proto churn, but additive and small).
- **`iac.proto` is touched again** — the `ListBackendNames` RPC. Additive; the strict-contracts invariants (no structpb, etc.) are unaffected.
- **Deferred limitation:** pure-storage plugins (no IaC provider) cannot use `ServeIaCPlugin` until it is refactored for multiple served objects — out of scope; revisit if/when such a plugin is needed.
- **Process lesson:** adversarial design/plan review vets *documents*; it did not catch that a load-bearing seam (`ServeIaCPlugin` serving a non-provider service) did not exist in the SDK. Future designs that assume an extension point on existing infrastructure should grep-verify that point exists before the plan locks. (No skill change made here — recorded as a lesson.)
