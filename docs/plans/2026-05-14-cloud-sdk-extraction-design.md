# Cloud-SDK Extraction: workflow core → strict-contract plugins

**Date:** 2026-05-14
**Status:** Design — approved in brainstorm, pending adversarial review
**Owner:** autonomous pipeline (workflow#TBD)

## Problem

Workflow core's `module/` package imports three cloud SDK trees directly:

| SDK | go.mod weight | Files (real imports) |
|-----|---------------|----------------------|
| `github.com/aws/aws-sdk-go-v2/*` | ~15 service packages | 13 |
| `github.com/Azure/azure-sdk-for-go/sdk/*` | azcore + azblob | 2 |
| `cloud.google.com/go/storage` + `google.golang.org/api/*` | storage + container | 3 |

Every dependabot bump of a cloud SDK (PRs #400/#419/#421/#635 as of this writing) churns workflow core's `go.sum`, inflates the binary, and couples core release cadence to vendor SDK release cadence. The `workflow-plugin-{aws,azure,gcp,digitalocean}` plugins already exist and already carry these SDKs for their IaC *resource provider* role — core's direct usage is redundant surface.

Precedent: workflow#617 removed the legacy DigitalOcean IaC modules + `godo` from core; IaC resource provisioning moved entirely to `workflow-plugin-digitalocean`. This design extends the same principle to the *remaining* cloud functionality that never went through that extraction: IaC **state backends**, managed-service **platform** provisioners, and a handful of standalone modules/steps.

## Goals

- workflow core `go.mod` drops `aws-sdk-go-v2/*`, `Azure/azure-sdk-for-go/*`, `cloud.google.com/go/*`, `google.golang.org/api/*` entirely.
- Cloud functionality remains available, loaded via strict-contract gRPC plugins (the existing sidecar model).
- `kind` Kubernetes backend (no SDK) stays in core — local-dev/test path must not require a plugin.

## Non-Goals

- Re-homing the IaC *resource provider* contract (`IaCProviderRequired`) — already extracted, not touched here.
- Changing how plugins are discovered/installed (`wfctl plugin install` flow unchanged).
- Backwards-compatible yaml — this is a **clean break** with a migration guide (per workflow#617 precedent).

## Architecture

Three extension surfaces, three handling strategies:

### 1. IaC state backends → new `IaCStateBackend` strict proto contract

`iac.state` **stays a core module type**. The state store is engine infrastructure — the orchestrator reads/writes it during every plan/apply cycle — so it keeps a stable core seam. What changes: `config.backend` no longer dispatches a hardcoded `switch` in `module/iac_module.go`; instead core resolves an `IaCStateBackend` gRPC client from whichever loaded plugin registered that backend name.

```proto
// workflow/plugin/external/proto/iac_state.proto
service IaCStateBackend {
  rpc GetState   (GetStateRequest)   returns (GetStateResponse);
  rpc PutState   (PutStateRequest)   returns (PutStateResponse);
  rpc DeleteState(DeleteStateRequest) returns (DeleteStateResponse);
  rpc ListStates (ListStatesRequest)  returns (ListStatesResponse);
  rpc AcquireLease(AcquireLeaseRequest) returns (AcquireLeaseResponse);
  rpc ReleaseLease(ReleaseLeaseRequest) returns (ReleaseLeaseResponse);
}
message GetStateResponse  { bytes state = 1; bool exists = 2; }
message PutStateRequest   { string key = 1; bytes state = 2; string content_type = 3; }
// ... lease messages carry lease_id + duration_seconds
```

Backend ownership — every cloud plugin implements the contract for its native storage:

| backend name | plugin | storage |
|--------------|--------|---------|
| `s3`         | workflow-plugin-aws | AWS S3 |
| `azure_blob` | workflow-plugin-azure | Azure Blob |
| `gcs`        | workflow-plugin-gcp | Google Cloud Storage |
| `spaces`     | workflow-plugin-digitalocean | DO Spaces (S3-compatible) |

`memory`, `filesystem`, `postgres` backends stay **in core** — no cloud SDK, no reason to extract.

**Unary GET+PUT vs streaming:** decided by benchmark, not assumption. The writing-plans phase includes a task that drives a 1 MB synthetic state blob through a full plan→apply cycle (Get + Put + AcquireLease + ReleaseLease per resource batch) over unary RPC, measures p50/p99 added latency vs the in-process baseline, and only adopts chunked streaming if unary clears no acceptable bar. Default build target: **unary**, because (a) gRPC's default 4 MB message cap covers typical state files, (b) streaming adds protocol complexity that must be justified by data, and (c) the in-process baseline this replaces was itself a single blob read/write.

### 2. Managed-service platform provisioners → new `PlatformBackend` strict proto contract

The `platform.*` module family (`platform.kubernetes`, `platform.ecs`, `platform.networking`, `platform.dns`, `platform.autoscaling`) keeps its module types **and its `provider:` config key** in core — no yaml UX break. Each `platform.*` module currently dispatches to a provider-specific backend via an in-process interface (`kubernetesBackend`, etc.). The cloud-backed implementations (EKS, GKE, ECS, Route53, EC2, ApplicationAutoScaling) move behind the `PlatformBackend` gRPC contract; the `kind` backend stays in-core.

```proto
// workflow/plugin/external/proto/platform_backend.proto
service PlatformBackend {
  rpc Plan   (PlatformPlanRequest)   returns (PlatformPlanResponse);
  rpc Apply  (PlatformApplyRequest)  returns (PlatformApplyResponse);
  rpc Destroy(PlatformDestroyRequest) returns (PlatformDestroyResponse);
}
// Request carries: platform_type (kubernetes|ecs|...), provider (eks|gke|...),
//   desired-state struct, current-state struct.
// Response carries: plan actions / applied state / errors.
```

When `provider != kind` (or any other in-core backend), core's `platform.*` module resolves a `PlatformBackend` client from the plugin that registered `(platform_type, provider)`.

### 3. Standalone modules / steps → plugin-native types (existing SDK surface, no new contract)

These are user-facing pipeline functionality, not engine infrastructure. They become **plugin-native module/step types** via the existing `ModuleFactories` / `StepFactories` plugin SDK — which is *already* a gRPC sidecar path (`RemoteModule`). No new contract.

| core file | becomes | plugin |
|-----------|---------|--------|
| `aws_api_gateway.go` | `aws.apigateway` module | aws |
| `codebuild.go` | `aws.codebuild` module | aws |
| `nosql_dynamodb.go` | `nosql.dynamodb` module | aws |
| `pipeline_step_s3_upload.go` | `step.s3_upload` | aws |
| `s3_storage.go` / `storage_artifact_s3.go` | `storage.s3` module | aws |
| `storage_gcs.go` | `storage.gcs` module | gcp |

Credential handling (Option 1, approved): the deleted `cloud_account_aws.go` + `_creds.go` (`AWSConfigProvider` / `AWSConfig()`) is **not** replaced by a core contract. Each plugin-native AWS module carries its own `credentials:` config block and builds `aws.Config` in-process via a shared in-plugin `buildAWSConfig` helper — exactly the workflow-plugin-digitalocean model. To avoid yaml redundancy when a config declares many AWS modules, each plugin offers an optional in-plugin `aws.credentials` (resp. `gcp.credentials`) module + a `credentials_ref:` key — DRY handled entirely inside the plugin, still no core contract. `cloud_account_azure.go` and `cloud_account_gcp.go` have **no SDK imports** (pure config-map parsing) and stay in core untouched.

## Phases

Each phase is one workflow-core PR (deleting files + wiring the contract dispatch) plus one PR per affected plugin. Phases are independent — a plugin can ship its half ahead of core deleting its half, because core keeps the old in-process path until the plugin contract is wired.

**Phase A — Azure** (smallest, validates the `IaCStateBackend` contract end-to-end):
- New `IaCStateBackend` proto contract in core.
- workflow-plugin-azure implements `azure_blob` backend.
- Core: delete `iac_state_azure.go`, strip the `azure_blob` case + `newAzureSharedKeyCredential` from `iac_module.go`, drop `Azure/azure-sdk-for-go` from go.mod.

**Phase B — AWS** (largest — 13 files, 3 surfaces):
- New `PlatformBackend` proto contract in core.
- workflow-plugin-aws implements: `IaCStateBackend` (`s3`), `PlatformBackend` (eks/ecs/networking/dns/autoscaling), and plugin-native `aws.apigateway` / `aws.codebuild` / `nosql.dynamodb` / `step.s3_upload` / `storage.s3` types.
- Core: delete the 13 AWS files (+ the EKS half of `platform_kubernetes_kind.go`), drop `aws-sdk-go-v2` from go.mod.

**Phase C — GCP** (3 files):
- workflow-plugin-gcp implements `IaCStateBackend` (`gcs`), `PlatformBackend` (gke), plugin-native `storage.gcs`.
- Core: delete `iac_state_gcs.go`, `storage_gcs.go`, the GKE half of `platform_kubernetes_kind.go`; drop `cloud.google.com/go` + `google.golang.org/api`.

**Phase D — DigitalOcean compat:**
- workflow-plugin-digitalocean implements `IaCStateBackend` for `spaces` (S3-compatible — pulls `aws-sdk-go-v2/service/s3`, the one service package, not the whole tree).
- **Minor version bump** (compatibility break marker) + migration doc: wfctl users with `iac.state` `backend: spaces` must now have workflow-plugin-digitalocean ≥ that minor loaded. No app.yaml change for them — the backend name `spaces` is unchanged — but the plugin must be present in `data/plugins/` / `wfctl.yaml`.

`platform_kubernetes_kind.go` is touched by both B (EKS) and C (GKE) — it gets split: `kind` backend stays, EKS+GKE backends extracted. Phase B and C must coordinate on that file (B lands first, leaves a GKE shim; C removes the shim).

## Migration (user-facing)

Published in each plugin's CHANGELOG + a consolidated `docs/migrations/2026-05-14-cloud-sdk-extraction.md`:

- `iac.state` with `backend: s3|azure_blob|gcs|spaces` → load the matching plugin (`wfctl plugin install workflow-plugin-{aws,azure,gcp,digitalocean}`). yaml `backend:` value unchanged.
- `platform.kubernetes` `provider: eks|gke` etc. → load the matching plugin. yaml `provider:` value unchanged.
- `aws.apigateway` / former `cloud.account`-brokered AWS modules → module type renamed to plugin-native form; `credentials:` block moves inline (or `credentials_ref:` an `aws.credentials` module). **This is the only yaml-shape change.**
- `memory` / `filesystem` / `postgres` state backends, `kind` k8s backend → no change, still core.

## Assumptions

1. **gRPC's 4 MB default message cap covers real-world IaC state files.** If a deployment's state exceeds 4 MB the unary `IaCStateBackend` contract needs streaming — the benchmark task validates the typical case but a hostile-large state is out of initial scope (documented limitation, not a silent failure: `PutState` returns a clear "state exceeds transport limit" error).
2. **The `platform.*` backend interfaces are cleanly provider-separable.** The design assumes `kubernetesBackend` / `ecsBackend` / etc. are already interface-segregated such that the `kind` impl can stay while cloud impls extract. If a backend interface leaks SDK types into the core module shell, that shell needs an interface-extraction refactor first.
3. **Plugins may ship ahead of core.** A plugin implementing `IaCStateBackend` against the published proto is harmless to load on a core version that doesn't yet dispatch to it — the contract is additive, core ignores unknown backend registrations until its own half lands.
4. **`aws-sdk-go-v2/service/s3` in workflow-plugin-digitalocean is acceptable.** DO Spaces is S3-API; there is no godo-native Spaces client. The DO plugin already carries `godo`; adding one AWS service package is the minimal cost of self-contained `spaces` state support (vs. forcing DO users to also load workflow-plugin-aws).
5. **`cloud_account_azure.go` / `cloud_account_gcp.go` genuinely have zero SDK imports.** Verified by grep at design time; if a future change adds an SDK import there, that file joins its phase's extraction.
6. **No core code outside `module/` imports these SDKs.** Verified: the only `aws-sdk-go-v2` / `azure-sdk-for-go` / `cloud.google.com` imports are under `module/`. `cmd/`, `engine.go`, `schema/`, `plugin/` are clean.

## Rollback

This design changes **plugin loading paths** and **go.mod dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list.

- **Per-phase revert:** each phase is an isolated core PR + plugin PR(s). Reverting the core PR restores the in-process backend `switch` / `platform.*` cloud backends and re-adds the SDK to `go.mod` — the deleted files are recoverable from git. The plugin PRs are additive (new contract impls / module types) and can stay merged harmlessly even if core reverts.
- **Forward-fix preferred over revert:** because core keeps the old in-process path until the contract dispatch is wired *in the same PR*, a broken phase fails at PR CI (image-launch / strict-contracts gates), not in production. The revert path exists but the gate is the primary safety.
- **DO `spaces` break (Phase D):** the only change with an external-user-visible compat break. Rollback = revert the DO plugin minor bump; `spaces` users on the prior plugin version are unaffected because the prior version still has the in-core path's expectations. The break only bites users who upgrade core past the phase-D core PR *without* upgrading the DO plugin — the migration doc + `minEngineVersion` bump in the plugin manifest is the guard.

## Self-challenge — top doubts surfaced

1. **`PlatformBackend` may be over-general.** Five `platform.*` types (kubernetes/ecs/networking/dns/autoscaling) behind one `Plan/Apply/Destroy` contract with a `platform_type` discriminator — this risks a lowest-common-denominator contract that fits none of them well. *Mitigation:* writing-plans should validate the contract shape against all five backend interfaces before locking the proto; if they don't unify cleanly, split into per-family contracts or fold the cloud platform provisioners into the existing `IaCProviderRequired` resource model instead.
2. **Assumption 2 (clean provider-separability) is the most fragile.** If `platform_kubernetes_kind.go`'s `kubernetesBackend` interface returns SDK-typed values, "keep kind, extract EKS/GKE" requires a non-trivial interface refactor that this design hand-waves. *Mitigation:* the first task of Phase B/C is an interface-audit spike — if it fails, the phase re-scopes.
3. **The state-backend benchmark could come back "streaming required"** and invalidate the unary default, reshaping the `IaCStateBackend` proto after Phase A has already shipped it. *Mitigation:* run the benchmark *before* finalizing the Phase A proto — it's a writing-plans task ordered ahead of the contract lock, not after.

## Open items deferred to writing-plans

- Exact proto field layouts for all three contracts (sketches above are directional).
- Whether `PlatformBackend` is one contract or per-family (gated on self-challenge doubt #1).
- The `platform.*` backend interface-audit spike (gated on self-challenge doubt #2).
- Benchmark harness location + acceptance threshold (gated on self-challenge doubt #3).
- Per-plugin CHANGELOG + the consolidated migration doc wording.
