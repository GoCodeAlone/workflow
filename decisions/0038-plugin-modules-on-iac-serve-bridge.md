# 0038. Plugin modules + steps on the IaC serve bridge — extend `iacPluginServiceBridge`, reuse `grpc_server.go`

**Status:** Accepted
**Date:** 2026-05-15
**Decision-makers:** autonomous pipeline (design-authoring), Jon (operator — direction 2026-05-15)
**Related:** docs/plans/2026-05-15-plugin-modules-on-iac-design.md, docs/plans/2026-05-14-cloud-sdk-extraction-bcd.md (the locked B/C/D plan whose blocked tasks this absorbs), decisions/0035 (assumed-seam-must-be-grep-verified), decisions/0036 (Configure RPC — same recurrence pattern)

## Context

The locked B/C/D plan's §3 (standalone modules) presumed plugins served via `sdk.ServeIaCPlugin` could register `ModuleFactories`/`StepFactories`. They cannot today — `iacPluginServiceBridge` (`plugin/external/sdk/iacserver.go:155-200`) implements only `GetContractRegistry` + `GetManifest`; everything else returns `Unimplemented` via `pb.UnimplementedPluginServiceServer`. `ExternalPluginAdapter.ModuleFactories()` (`plugin/external/adapter.go:463-540`) calls `GetModuleTypes` then `CreateModule` per type and gets `Unimplemented` from IaC plugins — so the adapter returns nil. Three approaches were weighed: (A) extend the bridge with delegate-to-`grpc_server.go`-impl Module/Step methods; (B) introduce a third top-level entrypoint `sdk.ServeHybridPlugin`; (C) plugin authors call `sdk.Serve` AND `RegisterAllIaCProviderServices` manually. (B) adds a third entrypoint to the deliberately-collapsed plugin-author UX. (C) requires the dual-call dance the strict-contracts cutover removed (the rationale: "plugin authors write ONE call; they cannot omit registration for a capability they implemented" — `iacserver.go:35-42`).

## Decision

**Adopt Approach A.** Add `Modules map[string]sdk.ModuleProvider` + `Steps map[string]sdk.StepProvider` to `IaCServeOptions`. `ServeIaCPlugin` constructs a "hybrid" `pb.PluginServiceServer` whose `GetContractRegistry`/`GetManifest` come from the existing IaC bridge logic, and whose `GetModuleTypes`/`CreateModule`/`InitModule`/`StartModule`/`StopModule`/`DestroyModule`/`GetStepTypes`/`CreateStep`/`ExecuteStep`/`DestroyStep` come from the **existing** `plugin/external/sdk/grpc_server.go` PluginService implementation, parameterized over the supplied provider maps. The map value types are the same `sdk.ModuleProvider` / `sdk.StepProvider` interfaces non-IaC plugins already implement — no parallel factory shape, no adapter shim between the bridge and the existing handle-state code. Single registered `pb.PluginServiceServer`; no double-registration; no proto change; one entrypoint. Backwards compatible (zero-value options = current behavior). Approach (B) rejected — adds a third entrypoint and partially undoes the cutover's UX consolidation. Approach (C) rejected — re-creates the registration-omission bug class the cutover deleted.

**v1 scope limits (sub-decisions):** the IaC-bridge Module path uses only the legacy `sdk.ModuleProvider` (config-Struct) interface; `sdk.TypedModuleProvider` (STRICT_PROTO contracts) is **out for v1**. Modules registered via this path do **not** get `MessagePublisher` / `MessageSubscriber` capability — `iacGRPCPlugin.GRPCServer` discards the `*goplugin.GRPCBroker` parameter, so `callbackClient` would be nil. The blocked plan tasks (`storage.s3` / `storage.gcs` / `step.s3_upload` / `aws.credentials` / `gcp.credentials`) need neither capability. Lifting either limit is a follow-up SDK change.

## Consequences

- **Single source of truth for handle state + lifecycle dispatch** — both `sdk.Serve` and the IaC bridge call the same extracted helper.
- **Backwards compatible** — existing IaC-only plugins (azure v1.1.1, DO v1.1.0, aws v…+s3-only) unaffected; the new fields are optional.
- **Engine-side: zero change** — `ExternalPluginAdapter.ModuleFactories()` already dispatches purely on `pb.PluginServiceClient`; the new bridge answers what it currently sends to `Unimplemented`.
- **Cost** — `iacPluginServiceBridge` grows from ~50 LOC to ~150-300 LOC (delegating ~10 methods). Mitigated by routing all real lifecycle logic through the existing `grpc_server.go` helper.
- **Risk** — load-bearing assumption: `grpc_server.go`'s legacy impl extracts into a helper without behavioral change. If false, the SDK extension PR includes a minor refactor; documented as an Open Item to resolve at plan-writing time (read `grpc_server.go` end-to-end before specifying the task).
- **Re-homed scope** — the new plan absorbs B/C/D's blocked Tasks 8/9/10/23/24 + the downstream-blocked Tasks 14-18 (PR 6) + Tasks 27-29 (PR 10). The locked B/C/D plan needs no unlock dance.
