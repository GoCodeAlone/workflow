# Plugin Modules on IaC — extending `ServeIaCPlugin` to also serve module + step factories

**Date:** 2026-05-15
**Status:** Design — pre adversarial-design-review
**Owner:** autonomous pipeline (workflow#TBD)

## Relationship to the locked B/C/D plan

The locked B/C/D plan `docs/plans/2026-05-14-cloud-sdk-extraction-bcd.md` (on workflow `origin/main` since #677) is **partially executed**: PR 1 (Configure RPC) + PR 2 (azure retrofit) + PR 3 (aws `s3` IaCStateBackend) + PR 7 (ADR 0037) are MERGED; PR 5 (DO `spaces`) + PR 9 (gke wiring) are open with CI in flight.

Mid-execution, an **assumed seam** in the locked plan was grep-verified to not exist (per `decisions/0035`'s lesson). The plan's Tasks 8 + 9 (PR 4 — aws `storage.s3` + `step.s3_upload` + `aws.credentials`) and Task 23 (PR 8 tail — gcp `storage.gcs` + `gcp.credentials`) presume that plugins served via `sdk.ServeIaCPlugin` can register `ModuleFactories`/`StepFactories` to the engine. The current SDK does not expose that — `ServeIaCPlugin`'s `iacPluginServiceBridge` (`plugin/external/sdk/iacserver.go`) registers a `pb.PluginServiceServer` that implements only `GetContractRegistry` + `GetManifest`; the Module/Step lifecycle RPCs go through `UnimplementedPluginServiceServer` → `codes.Unimplemented` → `ExternalPluginAdapter.ModuleFactories()` returns nil → no plugin-native modules.

This design addresses the gap and **absorbs the blocked locked-plan tasks** so the locked plan does not need an unlock dance:
- The new plan ships the SDK extension first.
- The new plan re-implements the equivalent of B/C/D's Task 8 / Task 9 / Task 10 / Task 23 / Task 24.
- The new plan also absorbs the downstream-blocked B/C/D Tasks 14–18 (PR 6 — Phase B core deletion, depends on aws plugin release tag) and Tasks 27–29 (PR 10 — Phase C core deletion, depends on gcp plugin release tag + the new SDK extension's PR).

The locked B/C/D plan stays as-is for what's already shipped and in-flight (PRs 1/2/3/5/7/9 + PR 8 through Task 22). No re-amend, no re-lock.

## Problem

Two related problems, both surfaced by mid-execution implementation:

1. **No path for a single plugin process to serve both IaC services and module/step factories.** A plugin can call `sdk.Serve` (legacy — the `pb.PluginServiceServer` in `grpc_server.go` with full Module/Step lifecycle RPCs + handle state) OR `sdk.ServeIaCPlugin` (typed-IaC services + a minimal bridge with only `GetContractRegistry` + `GetManifest`). They're separate top-level entrypoints; the bridge has a "skip if already registered" guard hinting at the mixed-plugin scenario, but nothing wires it together cleanly. Plugin authors who want both face an architectural fork the SDK doesn't resolve.

2. **The B/C/D plan's §3 standalone-modules extraction depends on (1).** `storage.s3` / `storage.gcs` / `step.s3_upload` (and the in-plugin `aws.credentials` / `gcp.credentials` DRY modules with `credref` registry) need a serve path on plugins that already serve IaC. Without (1) they cannot be plugin-native; the in-core modules persist; aws-sdk-go-v2/service/s3 and cloud.google.com/go/storage stay in workflow's go.mod for the in-core paths.

## Inventory provenance — grep-verified

Per `decisions/0035` (assumed-seam-must-be-grep-verified), every claim below is grep-verified against the actual code on `origin/main` HEAD `45cf66cf`:

- `plugin/external/sdk/iacserver.go:71-83` — `registerAllIaCProviderServicesWithOpts` registers the IaC services THEN registers `iacPluginServiceBridge` as `pb.PluginServiceServer`. The bridge's guard (`if _, alreadyRegistered := s.GetServiceInfo()[pb.PluginService_ServiceDesc.ServiceName]`) acknowledges the mixed-plugin scenario but punts on it.
- `plugin/external/sdk/iacserver.go:155-200` — `iacPluginServiceBridge` implements `GetContractRegistry` + `GetManifest`. Embeds `pb.UnimplementedPluginServiceServer` for everything else (ModuleType/Step/Trigger lifecycle).
- `plugin/external/sdk/iacserver.go:195-208` — `IaCServeOptions` has only `PluginInfo` + `ManifestProvider`. Comment line 196: "exists as a forward-extension point so future metadata fields (PluginInfo) can be added without breaking the API." — the precedent for adding fields is established.
- `plugin/external/sdk/grpc_server.go` (22KB) — the legacy `sdk.Serve` PluginService implementation: full Module/Step/Trigger lifecycle RPCs + handle-ID state management. Real, mature code; the hard part (handle state, lifecycle errors, factory dispatch) is already written.
- `plugin/external/sdk/serve.go` + `serve_full.go` — the legacy `sdk.Serve` entrypoint that wires `grpc_server.go`'s impl onto a gRPC server.
- `plugin/external/adapter.go:463-540` — `ExternalPluginAdapter.ModuleFactories()` calls `GetModuleTypes` then `CreateModule` per type; `.StepFactories()` calls `GetStepTypes` then `CreateStep` per type. Both use the typed `CreateModuleRequest`/`CreateStepRequest` (no `structpb`); `RemoteModule` wraps the resulting handle for engine-side lifecycle.
- `plugin/external/proto/plugin.proto:13-78` — `PluginService` is the canonical surface; the relevant RPCs for module-factory hosting are: `GetModuleTypes`, `CreateModule`, `InitModule`, `StartModule`, `StopModule`, `DestroyModule`, `GetModuleSchemas` (optional UI), `GetStepTypes`, `CreateStep`, `ExecuteStep`, `DestroyStep`. Triggers are out of scope here; `InvokeService` was deprecated by the strict-contracts cutover and stays unimplemented.

## Goals

1. **Single serve entrypoint** — `sdk.ServeIaCPlugin` continues to be the canonical entrypoint; plugins that want module/step factories supply them via `IaCServeOptions` and the SDK wires the rest. Backwards compatible: zero-value options = current behavior.
2. **Reuse the existing `grpc_server.go` PluginService implementation** for Module/Step lifecycle. The hard parts (handle state, error wrapping, typed config decoding) are already in `grpc_server.go`; do not reimplement.
3. **Preserve the strict-contracts cutover invariants** — no return of `structpb`/`Any`; no `InvokeService` string-dispatch; the new bridge surface uses the existing typed `CreateModule`/`CreateStep` Request/Response shape.
4. **Absorb the locked-plan's blocked tasks** — the new plan delivers `aws storage.s3 + step.s3_upload + aws.credentials + credref`, `gcp storage.gcs + gcp.credentials + credref`, both releases, and the workflow-core deletions (PR 6 + PR 10 of the locked plan), so the locked plan needs no amendment.

## Non-goals

- **Trigger factories** — out. The blocked work needs no `step.trigger_*` types.
- **Reviving `InvokeService`** — out. Strict-contracts cutover removed it deliberately; the new bridge does not surface it.
- **Refactoring `grpc_server.go`** — out. We extend, not refactor. Any cleanup is a follow-up.
- **Schemas / UI metadata for the new modules** — `GetModuleSchemas` stays unwired in v1 of this design unless an in-flight UI requirement surfaces; `storage.s3`/`storage.gcs`/`step.s3_upload` work without it (matching their in-core ancestors).
- **`godo` extraction, out-of-`module/` AWS surface, IaC state at-rest format** — same Non-Goals as the B/C/D design, inherited.

## Approaches considered

### Approach A (chosen) — Extend `iacPluginServiceBridge` to delegate Module/Step methods to the legacy `grpc_server.go` impl

Add `Modules` + `Steps` (and optional `ModuleSchemas`) factory maps to `IaCServeOptions`. `ServeIaCPlugin` constructs a "hybrid" `pb.PluginServiceServer` whose `GetContractRegistry`/`GetManifest` come from the existing IaC bridge logic, and whose `GetModuleTypes`/`CreateModule`/`InitModule`/`StartModule`/`StopModule`/`DestroyModule`/`GetStepTypes`/`CreateStep`/`ExecuteStep`/`DestroyStep` come from the **existing** `grpc_server.go` PluginService implementation, parameterized over the supplied factory maps. A single `pb.PluginServiceServer` is registered; no double-registration; no entrypoint fork; no proto change.

**Pros:**
- Smallest user-facing API delta (3 optional fields on `IaCServeOptions`).
- Reuses `grpc_server.go`'s mature Module/Step lifecycle + handle-state code; doesn't reimplement.
- Backwards compatible: zero-value options = current behavior; existing plugins unaffected.
- Single entrypoint preserved (`ServeIaCPlugin`); no plugin-author confusion about which to call.
- No proto change; no contract surface change.
**Cons:**
- `iacPluginServiceBridge` becomes a delegating type rather than a thin one. Mitigated by extracting the legacy impl into a function/struct that both the legacy `sdk.Serve` and the new bridge call.

**This design picks Approach A.**

### Approach B — New `sdk.ServeHybridPlugin(IaC, Modules, Steps)` entrypoint

A new top-level function that combines IaC + module-factory paths. `ServeIaCPlugin` stays as-is; `ServeHybridPlugin` is the new entrypoint plugin authors call when they want both surfaces.

**Pros:** clean named separation; "hybrid" signals intent; existing IaC-only plugins are not retrofit-able by accident.
**Cons:** more SDK surface; plugin authors face a third entrypoint to choose from (after `Serve` + `ServeIaCPlugin`); two parallel call sites to maintain. The "third entrypoint" cost is real — the strict-contracts cutover deliberately collapsed plugin entrypoints; this would partially undo that.

Rejected because Approach A delivers the same capability behind one entrypoint with a smaller surface change.

### Approach C — Have plugin authors call `sdk.Serve` AND `RegisterAllIaCProviderServices`

The bridge's "skip if already registered" guard already supports this; have plugin authors do the wiring manually. No SDK change.

**Pros:** zero SDK delta.
**Cons:** plugin authors must understand the dual-call dance; no compile-time enforcement; the strict-contracts cutover's whole rationale ("plugin authors write ONE call; they cannot omit registration for a capability they implemented" — `iacserver.go:35-42`) is undermined. Splits the registration surface and re-creates the very class of bugs the cutover deleted (`InvokeService` case-string-typo bug class).

Rejected because it regresses the cutover's UX guarantee.

## Architecture (Approach A)

### SDK extension

`plugin/external/sdk/iacserver.go`:

```go
type IaCServeOptions struct {
    PluginInfo       *PluginInfo
    ManifestProvider *pluginpkg.PluginManifest

    // Modules supplies plugin-native module factories. When non-nil, the
    // bridge's GetModuleTypes / CreateModule / InitModule / StartModule /
    // StopModule / DestroyModule are wired to a delegate that reuses the
    // legacy grpc_server.go PluginService implementation. Zero-value =
    // current behavior (Unimplemented for those RPCs).
    Modules map[string]pluginpkg.ModuleFactory

    // Steps supplies plugin-native step factories. When non-nil, the
    // bridge's GetStepTypes / CreateStep / ExecuteStep / DestroyStep are
    // wired similarly.
    Steps map[string]pluginpkg.StepFactory
}
```

`iacPluginServiceBridge` gains an embedded `*moduleStepDelegate` (the extracted-from-`grpc_server.go` PluginService Module/Step implementation). When the delegate is set, the bridge's Module/Step methods forward; when it is nil, they fall through to `pb.UnimplementedPluginServiceServer`.

The legacy `grpc_server.go`'s Module/Step implementation is **extracted** into a small public-or-internal helper struct/constructor (e.g. `func newModuleStepHandler(modules map[string]ModuleFactory, steps map[string]StepFactory) *moduleStepHandler`). The legacy `sdk.Serve` continues to use it via the existing wrapper. The new IaC path uses it via the bridge. **Single source of truth for handle state + lifecycle dispatch**.

### Engine-side: zero change

`ExternalPluginAdapter.ModuleFactories()` / `.StepFactories()` (`adapter.go:463-540`) already call `GetModuleTypes`/`CreateModule` and `GetStepTypes`/`CreateStep` against `pb.PluginServiceClient`. With the new bridge wired, the IaC plugin now answers those RPCs with real factories instead of `Unimplemented`. **No engine-side code change required.**

### Plugin-author UX

```go
func main() {
    sdk.ServeIaCPlugin(provider, sdk.IaCServeOptions{
        ManifestProvider: sdk.MustEmbedManifest(manifestJSON),
        Modules: map[string]plugin.ModuleFactory{
            "storage.s3":      modules.NewS3StorageFactory(),
            "aws.credentials": modules.NewAWSCredentialsFactory(),
        },
        Steps: map[string]plugin.StepFactory{
            "step.s3_upload": steps.NewS3UploadFactory(),
        },
    })
}
```

Zero new entrypoints; one call; explicit declaration of what the plugin serves.

### Re-homed work — what this plan absorbs from B/C/D

| Locked-plan task | Re-homed equivalent | Notes |
|---|---|---|
| B/C/D Task 7 (aws `BuildAWSConfig` + `credential_source` marker) | New plan Task — aws plugin, can ship in a precursor PR before SDK extension since it doesn't depend on the SDK extension (it's the plugin's IaC-provider credential helper). |
| B/C/D Task 8 (aws `storage.s3` + `aws.credentials` DRY + `credref`) | New plan Task — aws plugin, depends on SDK extension PR. |
| B/C/D Task 9 (aws `step.s3_upload`) | New plan Task — aws plugin, depends on SDK extension PR. |
| B/C/D Task 10 (aws plugin release) | New plan Task — depends on the above tasks merged. |
| B/C/D Task 23 (gcp `storage.gcs` + `gcp.credentials` + `credref` + release) | New plan Task — gcp plugin, depends on SDK extension PR. |
| B/C/D Task 24 (gcp capability parity audit) | New plan Task — gcp plugin. |
| B/C/D Tasks 14–18 (PR 6 — Phase B workflow-core deletion: `cloud_account_aws.go` + resolvers rewrite + `iac_state_spaces.go` + `s3_storage.go` + `pipeline_step_s3_upload.go` + go.mod tidy + `.phase-b-complete` + migration doc) | Absorbed wholesale into the new plan — the deletions become unblocked once the aws plugin release ships. |
| B/C/D Tasks 27–29 (PR 10 — Phase C workflow-core deletion: GCS files + `platform_kubernetes_gke.go` + `gcs` switch case + GCP SDK go.mod drop + permanent CI gate + Phase C migration doc) | Absorbed wholesale — depends on gcp plugin release + workflow PR 9 (gke wiring) merged. |

The new plan's PR count is therefore: **1 SDK extension PR (workflow core) + plugin PRs (aws, gcp) + 2 workflow-core deletion PRs (Phase B + Phase C)**. Roughly 5 PRs / ~15 tasks. Concrete decomposition is `writing-plans`'s job.

## Assumptions (load-bearing)

1. `grpc_server.go`'s legacy PluginService Module/Step implementation is **extractable** into a helper that both `sdk.Serve` and the IaC bridge can call without behavioral change. (To be confirmed by reading `grpc_server.go` during plan-writing — code is on disk, ~22KB.)
2. The host-side `ExternalPluginAdapter.ModuleFactories()` / `.StepFactories()` will accept the new factories on a plugin that ALSO advertises IaC services via `ContractRegistry`. (Read of `adapter.go:463-540` shows it dispatches purely on `pb.PluginServiceClient` — no IaC-vs-non-IaC branching; this assumption is high-confidence.)
3. The pinned `workflow v0.52.0` (released 2026-05-15) is the floor every plugin in this plan pins to via `minEngineVersion`. The SDK extension PR ships in `v0.53.0` (or later); plugins consuming the extension pin to that floor.
4. Adding a typed `Modules`/`Steps` field on `IaCServeOptions` is API-additive and does not break existing IaC-only plugins (azure/DO/aws-without-modules). Backwards-compat invariant.
5. The legacy `sdk.Serve` path is still in use by other plugins in this workspace (workflow-plugin-payments, etc.). The extracted helper must not regress them.
6. `plugin.json capabilities.moduleTypes` / `stepTypes` is the canonical declaration site for what a plugin advertises (already standardized by the registry / wfctl).

## Rollback

This design changes a **plugin SDK API surface** + a **plugin loading path** + workflow core deletions — runtime-affecting per the `runtime-launch-validation` trigger list.

- **SDK extension PR (workflow core, additive):** revert is clean — removes `Modules`/`Steps` fields from `IaCServeOptions` + the bridge's delegate logic. Plugins that have not started using the new fields are unaffected. Plugins that have started using it will fail to build against the reverted SDK; they can pin the prior workflow version.
- **Plugin PRs (aws, gcp standalone-modules + releases):** additive plugin features. Revert is harmless to a workflow core that still has the in-core modules. On a defect, prefer a forward patch release over deleting a tag.
- **Workflow-core deletion PRs (Phase B / Phase C):** identical rollback story to the locked B/C/D plan's PR 6 / PR 10 — revert restores the in-core paths + re-tidies go.mod. The `spaces` clean-break (already shipped via the locked plan's PR 5 + PR 6) is unaffected by this design's deletions; this design ships PR 6 (the Phase B core deletion) and PR 10 (Phase C). The clean-break rolls back as a matched pair with the relevant plugin release.
- **Forward-fix preferred:** each core deletion PR removes the in-core path only AFTER the plugin replacement is released and the dispatch is wired. A broken phase fails at PR CI (image-launch + audit-script gates), not in production.

## Migration (user-facing)

For users of `iac.state` / `storage.s3` / `storage.gcs` / `step.s3_upload`:
- `iac.state backend: s3 / spaces / azure_blob / gcs / postgres` — no yaml change. After this plan's Phase B/C core-deletion PRs merge, `s3`/`spaces`/`gcs` require their respective plugin loaded (already true for `s3`/`spaces` after the locked plan's PR 6).
- `storage.s3` / `storage.gcs` / `step.s3_upload` — load `workflow-plugin-aws` / `workflow-plugin-gcp`. `credentials:` block moves inline (or `credentials_ref:` an in-plugin `aws.credentials` / `gcp.credentials` module). yaml step/module type names unchanged. **This is the only yaml-shape change** (matching the locked B/C/D plan's design §3 stated migration).
- Plugin authors who currently call `sdk.ServeIaCPlugin` with zero-value options: no change required.
- Plugin authors who want to add module/step factories to an IaC plugin: populate `IaCServeOptions.Modules` / `.Steps`.

## Open Items

- **Whether to wire `GetModuleSchemas`** (UI metadata) in v1 of the SDK extension: deferred. The blocked modules don't need it. Add later if a UI requirement surfaces.
- **Whether `grpc_server.go`'s legacy implementation extracts cleanly** vs. needing minor refactor: confirmed during plan-writing (`writing-plans` will read `grpc_server.go` end-to-end before specifying tasks).
- **Whether the `credref` registry should live in a workflow-published shared library** (so aws + gcp + future plugins all import the same impl) or be duplicated per-plugin: the locked B/C/D plan's design said per-plugin (mirrors the deliberately-diverging `S3IaCStateStore`/`SpacesIaCStateStore` copies); this design inherits that until/unless the duplication causes pain. Note as a deferred consolidation candidate.

## Recording decisions

This design introduces a non-trivial API choice on a previously-removed surface (PluginService Module/Step methods on IaC-served plugins). Per `recording-decisions` skill condition 2 (non-trivial trade-off ≥2 plausible approaches), an ADR will be recorded:

- `decisions/0038-plugin-modules-on-iac-serve-bridge.md` — captures Approach A vs B vs C, the reuse-of-`grpc_server.go` decision, and the assumption-set.

The ADR will be written + committed alongside this design.

## Self-challenge transcript

(Required by the brainstorming skill. Each item: what I asked myself, what I found, what survived.)

1. **Laziest plausible solution?** Keep `storage.s3`/`storage.gcs`/`step.s3_upload` in workflow core; ship the partial extraction. Survived consideration but rejected — defeats the design intent and the user-stated goal of "support module factories holistically." User explicitly wants the SDK extended.
2. **Most fragile assumption?** Assumption 1 — that `grpc_server.go`'s legacy impl extracts cleanly. If the legacy impl is tightly coupled to `sdk.Serve`'s top-level wiring, the "extract a helper" design point becomes a refactor instead of a passthrough. Mitigation: `writing-plans` reads `grpc_server.go` end-to-end before the SDK extension task is specified; if the impl resists extraction, the plan documents the minor refactor and includes it as a sub-task.
3. **YAGNI?** GetModuleSchemas (UI), GetTriggerTypes/CreateTrigger, InvokeService, DeliverMessage, GetAsset, GetConfigFragment, ConfigureCallback — all left unimplemented in the bridge. Only the 10 RPCs the blocked tasks actually need are wired. Documented.
4. **Failure-first under partial failure?** Plugin process crashes mid-Init; handle state in `grpc_server.go` is per-process and dies with the process. Engine reconnects via go-plugin re-spawn; modules are re-Created on the new connection — same as the current `sdk.Serve` plugins. No new failure mode.
5. **Repo-precedent conflicts?** None found. The strict-contracts cutover removed `InvokeService` string-dispatch; this design does not revive it. The `Type string` field on `CreateModuleRequest`/`CreateStepRequest` is the SAME pattern non-IaC plugins use today — established precedent.

Top 3 doubts surfaced (for adversarial review to scrutinize):

- Doubt 1: assumption-1 (`grpc_server.go` extracts cleanly) is the load-bearing claim; if false, the SDK extension PR is bigger than this design implies.
- Doubt 2: per-plugin `credref` duplication is a deferred consolidation that compounds over time (azure / aws / gcp / DO each carry a copy).
- Doubt 3: the migration sentence "this is the only yaml-shape change" is true only if `credentials_ref:` lookup is contained inside the plugin process — verifiable but implicit. The plan task for `credref` will pin this with a test.
