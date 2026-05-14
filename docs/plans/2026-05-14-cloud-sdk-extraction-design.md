# Cloud-SDK Extraction: workflow core → strict-contract plugins

**Date:** 2026-05-14
**Status:** Design — revised after adversarial review cycle 2
**Owner:** autonomous pipeline (workflow#TBD)

## Problem

Workflow core's `module/` package imports three cloud SDK trees directly. File counts are grep-verified (`awk` over import blocks, comment-only matches excluded). "Files" = files with a real import — not all are *deleted* (e.g. `iac_module.go` is *edited* to strip a `case`, not deleted; see Phases):

| SDK | Files (real imports) | how core sheds it |
|-----|----------------------|-------------------|
| `github.com/aws/aws-sdk-go-v2/*` | **13** | 11 deleted, `iac_module.go` edited (strip `spaces` case), `platform_kubernetes_eks.go` deleted (post Phase-0 split) |
| `github.com/Azure/azure-sdk-for-go/sdk/*` (azcore + azblob) | **2** | `iac_state_azure.go` deleted, `iac_module.go` edited (strip `azure_blob` case) |
| `cloud.google.com/go/storage` + `google.golang.org/api/*` | **3** | `iac_state_gcs.go` + `storage_gcs.go` deleted, `platform_kubernetes_gke.go` deleted (post Phase-0 split) |

Every dependabot bump of a cloud SDK (PRs #400/#419/#421/#635 as of this writing) churns workflow core's `go.sum`, inflates the binary, and couples core release cadence to vendor SDK release cadence. The `workflow-plugin-{aws,azure,gcp,digitalocean}` plugins already exist and already carry these SDKs for their IaC *resource provider* role — core's direct usage is redundant surface.

Precedent: workflow#617 removed the legacy DigitalOcean IaC modules + `godo` from core; IaC resource provisioning moved entirely to `workflow-plugin-digitalocean`. This design extends the same principle to the *remaining* cloud functionality that never went through that extraction: IaC **state backends**, managed-service **platform** provisioners, and a handful of standalone modules/steps.

## Goals

- workflow core `go.mod` drops `aws-sdk-go-v2/*`, `Azure/azure-sdk-for-go/*`, `cloud.google.com/go/*`, `google.golang.org/api/*` **entirely** — verified by a `go list -deps` gate in the final phase's CI.
- Cloud functionality remains available, loaded via strict-contract gRPC plugins (the existing sidecar model).
- `kind` Kubernetes backend (no SDK) stays in core — local-dev/test path must not require a plugin.

## Non-Goals

- Re-homing the IaC *resource provider* contract (`IaCProviderRequired`) — already extracted, not touched here.
- Changing how plugins are discovered/installed (`wfctl plugin install` flow unchanged).
- Backwards-compatible yaml — this is a **clean break** with a migration guide (per workflow#617 precedent).
- **Removing `aws-sdk-go-v2/service/kinesis`.** The user's original ask said "kinesis and azcore." `go mod why github.com/aws/aws-sdk-go-v2/service/kinesis` resolves to `workflow → workflow/module → github.com/GoCodeAlone/modular/modules/eventbus/v2 → kinesis` — kinesis is a **transitive dependency of `modular/modules/eventbus/v2`**, not a direct workflow import (the only `module/` reference is a string literal `"kinesis-provider"` in a test). Removing it is an upstream `modular` concern, not addressable by extracting workflow's own SDK usage. Out of scope here; tracked separately if `modular` eventbus ever needs the same treatment.

## Architecture

Three extension surfaces, three handling strategies:

### 1. IaC state backends → new `IaCStateBackend` strict proto contract

`iac.state` **stays a core module type**. The state store is engine infrastructure — the orchestrator reads/writes it during every plan/apply cycle — so it keeps a stable core seam. What changes: `config.backend` no longer dispatches a hardcoded `switch` in `module/iac_module.go`; instead core resolves an `IaCStateBackend` gRPC client from whichever loaded plugin registered that backend name.

The contract maps **1:1 onto the existing `module.IaCStateStore` interface** (`module/iac_state.go:21`) — six methods, no speculative surface:

```proto
// Added as a new service INSIDE plugin/external/proto/iac.proto — matches the
// established precedent (iac.proto already holds 8 services / 598 lines;
// state + platform contracts version alongside the resource-provider contract).
service IaCStateBackend {
  rpc GetState   (GetStateRequest)    returns (GetStateResponse);   // → IaCStateStore.GetState
  rpc SaveState  (SaveStateRequest)   returns (SaveStateResponse);  // → IaCStateStore.SaveState
  rpc ListStates (ListStatesRequest)  returns (ListStatesResponse); // → IaCStateStore.ListStates(filter)
  rpc DeleteState(DeleteStateRequest) returns (DeleteStateResponse);// → IaCStateStore.DeleteState
  rpc Lock       (LockRequest)        returns (LockResponse);       // → IaCStateStore.Lock
  rpc Unlock     (UnlockRequest)      returns (UnlockResponse);     // → IaCStateStore.Unlock
}
message GetStateResponse  { IaCState state = 1; bool exists = 2; }
message SaveStateRequest  { IaCState state = 1; }  // idempotent: full-state replace, last-writer-wins
message ListStatesRequest { map<string,string> filter = 1; }
message LockRequest       { string resource_id = 1; int64 lease_ttl_seconds = 2; }  // TTL: plugin-backed locks self-clear on orphan; in-core backends ignore it
// IaCState mirrors module.IaCState. lease_ttl_seconds is contract-only — the
// module.IaCStateStore interface gains no method; the core dispatcher defaults it.
```

Backend ownership — every cloud plugin implements the contract for its native storage:

| backend name | plugin | storage | core file deleted |
|--------------|--------|---------|-------------------|
| `s3`         | workflow-plugin-aws | AWS S3 | `iac_state_spaces.go` (the S3-compatible store; also the `spaces` impl) |
| `azure_blob` | workflow-plugin-azure | Azure Blob | `iac_state_azure.go` |
| `gcs`        | workflow-plugin-gcp | Google Cloud Storage | `iac_state_gcs.go` |
| `spaces`     | workflow-plugin-digitalocean | DO Spaces (S3-compatible) | (shares `iac_state_spaces.go` deletion — see Phase D) |

`memory`, `filesystem`, `postgres` backends stay **in core** — no cloud SDK, no reason to extract.

**Unary GET+SAVE vs streaming:** decided by benchmark, not assumption. The writing-plans phase includes a task that drives a 1 MB synthetic state blob through a full plan→apply cycle (GetState + SaveState + Lock + Unlock per resource batch) over unary RPC, measures p50/p99 added latency vs the in-process baseline, and only adopts chunked streaming if unary clears no acceptable bar. Default build target: **unary**, because (a) gRPC's default 4 MB message cap covers typical state files, (b) streaming adds protocol complexity that must be justified by data, and (c) the in-process baseline this replaces was itself a single blob read/write. This task is ordered **before** the Phase A proto is locked (per self-challenge doubt #3).

### 2. Managed-service platform provisioners → new `PlatformBackend` strict proto contract

The `platform.*` module family (`platform.kubernetes`, `platform.ecs`, `platform.networking`, `platform.dns`, `platform.autoscaling`) keeps its module types **and its `provider:` config key** in core — no yaml UX break. Each `platform.*` module currently dispatches to a provider-specific backend via an in-process interface (`kubernetesBackend`, etc.). The cloud-backed implementations (EKS, GKE, AKS, ECS, Route53, EC2, ApplicationAutoScaling) move behind the `PlatformBackend` gRPC contract; the `kind` backend stays in-core.

```proto
// Added as a new service INSIDE plugin/external/proto/iac.proto (same rationale
// as IaCStateBackend — co-versioned with the resource-provider contract).
service PlatformBackend {
  rpc Plan   (PlatformPlanRequest)    returns (PlatformPlanResponse);
  rpc Apply  (PlatformApplyRequest)   returns (PlatformApplyResponse);
  rpc Destroy(PlatformDestroyRequest) returns (PlatformDestroyResponse);
}
// Request carries: platform_type (kubernetes|ecs|...), provider (eks|gke|aks|...),
//   desired-state struct, current-state struct.
// Response carries: plan actions / applied state / errors.
```

When `provider != kind` (or any other in-core backend), core's `platform.*` module resolves a `PlatformBackend` client from the plugin that registered `(platform_type, provider)`.

**The `PlatformBackend` shape is gated** — see Alternatives Considered #1 and self-challenge doubt #1. The first writing-plans task for Phase B is an interface-audit spike that validates one unified `Plan/Apply/Destroy` contract against all five `platform.*` backend interfaces *before* the proto is locked. If they don't unify cleanly, the fallback is folding the cloud platform provisioners into the existing `IaCProviderRequired` / `ResourceDriver` model instead of inventing `PlatformBackend`.

### 3. Standalone modules / steps → plugin-native types (existing SDK surface, no new contract)

These are user-facing pipeline functionality, not engine infrastructure. They become **plugin-native module/step types** via the existing `ModuleFactories` / `StepFactories` plugin SDK — which is *already* a gRPC sidecar path (`RemoteModule`). No new contract.

| core file | becomes | plugin |
|-----------|---------|--------|
| `aws_api_gateway.go` (`AWSAPIGateway` — route-sync module) | `aws.apigateway` module | aws |
| `platform_apigateway.go` (`Platform*Gateway*` — provisioner) | folds into `PlatformBackend` (`platform.apigateway` provider) **or** `aws.apigateway` — resolved by the interface-audit spike | aws |
| `codebuild.go` | `aws.codebuild` module | aws |
| `nosql_dynamodb.go` | `nosql.dynamodb` module | aws |
| `pipeline_step_s3_upload.go` | `step.s3_upload` | aws |
| `s3_storage.go` | `storage.s3` module | aws |
| `storage_gcs.go` | `storage.gcs` module | gcp |

(`storage_artifact_s3.go` references the AWS SDK only in comments — verified comment-only, **not** a real import, stays in core.)

Credential handling (Option 1, approved): the deleted `cloud_account_aws.go` + `_creds.go` (`AWSConfigProvider` / `AWSConfig()`) is **not** replaced by a core contract. Each plugin-native AWS module carries its own `credentials:` config block and builds `aws.Config` in-process via a shared in-plugin `buildAWSConfig` helper — exactly the workflow-plugin-digitalocean model. To avoid yaml redundancy when a config declares many AWS modules, each plugin offers an optional in-plugin `aws.credentials` (resp. `gcp.credentials`) module + a `credentials_ref:` key — DRY handled entirely inside the plugin, still no core contract. `cloud_account_azure.go` and `cloud_account_gcp.go` reference the SDKs **only in comments** (verified — they are pure config-map parsing) and stay in core untouched.

## Security

Option 1 moves raw cloud secrets (`accessKey`/`secretKey`/`account_key`/etc.) inline into every plugin-native module's `credentials:` config block — multiplying the number of config sites holding plaintext secrets versus today's single `cloud.account` module. This is not unprecedented (`iac_module.go`'s current `spaces` case already inlines `accessKey`/`secretKey`), but the multiplication needs explicit handling:

- **Config-version store + execution tracing.** Workflow's config-version store (SHA-256 content-addressed) and execution-tracing layer marshal module config. Plugin-native module config carrying inline credentials MUST be redacted before persistence/tracing. Writing-plans task: extend the existing PII/secret redaction (already per-tenant-toggleable per `workflow-cloud`) to recognise the `credentials:` / `credentials_ref:` keys on plugin module config, OR confirm the existing redaction already covers any key matching a secret-pattern. This is a **blocking** task — it ships in the same phase as the first plugin-native AWS module, not after.
- **gRPC sidecar request logging.** The `IaCStateBackend` / `PlatformBackend` requests cross the engine↔plugin gRPC boundary, and `credentials:` blocks ride in `CreateModule` requests. **Verified at design time:** `plugin/external/grpc_plugin.go:39` constructs the server as `grpc.NewServer(opts...)` with `opts` passed straight through from the go-plugin broker — workflow's plugin SDK adds **no body-logging interceptor**. The only request-body logging anywhere in `plugin/external/` is `callback_server.go:85,118` (the plugin→host callback path: a `Log` RPC's `req.Message`, and a subscribe RPC's topic byte-count) — neither touches module config. `CreateModule` is dispatched at `adapter.go:477` with no logging of the request. **Conclusion: no redacting interceptor is needed today.** Writing-plans adds a guard test asserting no interceptor logs `CreateModule` bodies, so a future SDK change that adds one fails CI rather than silently leaking.
- **`credentials_ref:` blast radius.** A `credentials_ref` resolves to an in-plugin `aws.credentials` module within the *same plugin process* — it does not broaden which process can read the secret (engine never sees the resolved `aws.Config`, only the plugin does). This is strictly *narrower* than today's `cloud.account` (which builds `aws.Config` in the engine process). Documented as an improvement, not a risk.

## Failure modes

Moving the IaC state store behind a gRPC sidecar introduces a partial-failure surface on the engine's hottest path (every plan/apply does `Lock` → `GetState` → ... → `SaveState` → `Unlock`). The in-process store had none of these:

- **Plugin crashes between `Lock` and `Unlock` → orphaned lock.** An in-process lock dies with the process; a gRPC-plugin lock can outlive a plugin crash if the plugin persisted it (S3/Blob lock objects do persist). Mitigation, **wired into the contract**: `LockRequest` carries an optional `lease_ttl_seconds` field. Plugin-backed implementations write the lock with that TTL (S3 object with expiry metadata, Blob lease duration, etc.) so an orphaned lock self-clears. In-core backends (`memory`/`filesystem`/`postgres`) ignore the field — their `Lock` is process-scoped or transactional and cannot orphan across a crash. The `module.IaCStateStore` interface gains no new method; `lease_ttl_seconds` is contract-only, defaulted by the core dispatcher.
- **`SaveState` succeeds plugin-side but the gRPC response is lost → engine retries → double-write.** `SaveState` MUST be idempotent: it is a full-state replace keyed by `resource_id` (the existing `IaCStateStore.SaveState` is already "insert or replace"), so a retried identical `SaveState` is a no-op-equivalent. The contract documents `SaveState` as idempotent; the plugin implementations use unconditional PUT (overwrite), not append. No sequence number needed — IaC state is last-writer-wins by design.
- **Plugin unreachable at plan/apply start.** Core's `iac.state` dispatch returns a clear `"iac.state backend %q: plugin unreachable"` error and the plan/apply aborts *before* mutating anything — no partial state. This matches today's behavior when a misconfigured backend fails to construct in `IaCModule.Init()`.
- **`PlatformBackend` plugin crash mid-`Apply`.** A `platform.*` apply that crashes mid-flight leaves real cloud resources in an indeterminate state — but this is **identical to today's in-process risk** (an in-process `eksBackend.apply()` panic leaves the same indeterminate cloud state). The gRPC boundary does not worsen it; the next `Plan` reconciles against live cloud state as it does today. No new mitigation needed — documented as unchanged.

## Per-file import ownership (verified)

`module/platform_kubernetes_kind.go` is the one file shared across phases. Verified import ownership (`grep` per SDK symbol against the single import block at lines 3-19):

| backend | cloud SDK imports it owns | extracted in |
|---------|--------------------------|--------------|
| `kindBackend` | none (in-memory) | — stays in core |
| `eksBackend` | `aws-sdk-go-v2/aws`, `service/eks`, `eks/types`; also **calls `awsProviderFrom` + `AWSConfig()` from `cloud_account_aws.go`** | Phase B |
| `gkeBackend` | `google.golang.org/api/container/v1`, `google.golang.org/api/option` | Phase C |
| `aksBackend` | **none** — uses raw `net/http` REST against the Azure management API (the file header comment "Requires the Azure SDK" is stale; verified no `azure-sdk-for-go` symbol in the `aksBackend` region) | Phase A |

Two consequences this corrects from earlier drafts:
- **`aksBackend` extraction does NOT drop the Azure SDK** — `aksBackend` never imported it. The Azure go.mod drop is achieved entirely by deleting `iac_state_azure.go` + editing `iac_module.go`. Moving `aksBackend` to the plugin is still done (cloud-platform code belongs in the plugin, and `PlatformBackend` needs an Azure impl) but it is a *code-organisation* change, not a *dependency* change.
- **`eksBackend` has a hard call-graph edge to `cloud_account_aws.go`** — they MUST be removed in the same commit, or core fails to compile.

## Phase 0 — precursor: split `platform_kubernetes_kind.go` by backend

A pure mechanical refactor, no behavior change, landed **before** Phase A. Split the one shared file into four:
- `platform_kubernetes_kind.go` — `kindBackend` + the shared `kubernetesBackend` interface + `PlatformKubernetes` module shell. No cloud SDK imports after the split.
- `platform_kubernetes_eks.go` — `eksBackend` only; owns the `aws-sdk-go-v2` imports.
- `platform_kubernetes_gke.go` — `gkeBackend` only; owns the `google.golang.org/api` imports.
- `platform_kubernetes_aks.go` — `aksBackend` only; owns `net/http` (no cloud SDK).

After Phase 0, each subsequent phase deletes *its own backend file* with its own self-contained import block — the "always compiles" property is then structural, not asserted. Verification: `go build ./... && go test ./module/...` green, zero behavior diff (the split moves code, touches no logic). This is the single cheapest de-risking move in the plan — it converts the fragile "extract-from-shared-file-in-place" path into four trivially-reviewable deletions.

## Phases

Each phase is one workflow-core PR (deleting files + wiring the contract dispatch) plus one PR per affected plugin. Within a phase, the plugin PR may merge ahead of the core PR — core keeps the old in-process path until the contract dispatch is wired in the core PR, so a plugin implementing the published proto is harmless to load early. **Atomicity rule:** within a core PR, a deleted file and every file that references its symbols are removed in the *same commit* (the build gate enforces this — a dangling reference fails CI).

**Phase A — Azure** (smallest, validates BOTH new contracts end-to-end):
- Run the state-backend benchmark task; lock the `IaCStateBackend` proto shape.
- Run the `platform.*` interface-audit spike; lock or re-scope the `PlatformBackend` proto shape (Alternatives Considered #1).
- Add `IaCStateBackend` + `PlatformBackend` services to `plugin/external/proto/iac.proto`.
- Add the secret-redaction task + the gRPC-interceptor guard test (security tasks, blocking).
- workflow-plugin-azure implements `azure_blob` `IaCStateBackend` + `aks` `PlatformBackend`.
- Core PR: delete `iac_state_azure.go`; strip the `azure_blob` case + `newAzureSharedKeyCredential` from `iac_module.go` **(this + the deletion is what drops `Azure/azure-sdk-for-go` from go.mod)**; delete `platform_kubernetes_aks.go` (from Phase 0) and wire its `PlatformBackend` dispatch.

**Phase B — AWS** (largest — 13 files, 3 surfaces). Complete file inventory + destination:

| core file | destination | atomicity note |
|-----------|-------------|----------------|
| `iac_state_spaces.go` | aws plugin — `s3` `IaCStateBackend` (DELETE from core) | shared with `spaces` — see Phase D |
| `cloud_account_aws.go` | DELETE (Option 1 — no replacement) | **same commit as `platform_kubernetes_eks.go`** (call-graph edge) |
| `cloud_account_aws_creds.go` | DELETE (Option 1 — no replacement) | same commit as above |
| `platform_kubernetes_eks.go` (from Phase 0) | aws plugin — `eks` `PlatformBackend` | **same commit as `cloud_account_aws*.go`** |
| `aws_api_gateway.go` | aws plugin — `aws.apigateway` module | — |
| `platform_apigateway.go` | aws plugin — `PlatformBackend` or `aws.apigateway` (gated on interface-audit spike) | — |
| `codebuild.go` | aws plugin — `aws.codebuild` module | — |
| `pipeline_step_s3_upload.go` | aws plugin — `step.s3_upload` | — |
| `s3_storage.go` | aws plugin — `storage.s3` module | — |
| `platform_autoscaling.go` | aws plugin — `PlatformBackend` (`autoscaling`) | — |
| `platform_dns_backends.go` | aws plugin — `PlatformBackend` (`dns`/route53) | — |
| `platform_ecs.go` | aws plugin — `PlatformBackend` (`ecs`) | — |
| `platform_networking.go` | aws plugin — `PlatformBackend` (`networking`/ec2) | — |

- Core PR also: **strip the `spaces` case from `iac_module.go`** (it calls `NewSpacesIaCStateStore` from the deleted `iac_state_spaces.go` — same compile-dependency pattern as Phase A's `azure_blob` strip). Drop `aws-sdk-go-v2` from go.mod.

**Phase C — GCP** (3 files):
- workflow-plugin-gcp implements `IaCStateBackend` (`gcs`), `PlatformBackend` (`gke`), plugin-native `storage.gcs`.
- Core PR: delete `iac_state_gcs.go`, `storage_gcs.go`, `platform_kubernetes_gke.go` (from Phase 0); drop `cloud.google.com/go` + `google.golang.org/api`. After Phase C, `go list -deps ./...` shows zero cloud-SDK packages — the permanent CI gate is added here.

**Phase D — DigitalOcean (`spaces` clean-break):**
- workflow-plugin-digitalocean implements `IaCStateBackend` for `spaces` (S3-compatible — pulls `aws-sdk-go-v2/service/s3`, the one service package, not the whole tree).
- **This is a clean break, not soft-compat.** `iac_state_spaces.go` + the `spaces` case in `iac_module.go` are deleted by **Phase B's core PR** (`iac_state_spaces.go` is the one S3-compatible store backing *both* `s3` and `spaces`). After Phase B's core PR merges, `iac.state` with `backend: spaces` fails to build unless the DO plugin version implementing `IaCStateBackend` is loaded.
- **Minor version bump** on workflow-plugin-digitalocean (compatibility-break marker) + `minEngineVersion` set to the core version that drops the in-core `spaces` case + migration doc.
- **Sequencing:** the DO plugin PR (implementing `spaces` `IaCStateBackend`) MUST merge + release before Phase B's core PR merges — otherwise there is a window where `backend: spaces` has no implementation anywhere. Writing-plans orders the DO plugin PR as a Phase-B blocker.

## Migration (user-facing)

Published in each plugin's CHANGELOG + a consolidated `docs/migrations/2026-05-14-cloud-sdk-extraction.md`:

- `iac.state` with `backend: s3|azure_blob|gcs|spaces` → load the matching plugin (`wfctl plugin install workflow-plugin-{aws,azure,gcp,digitalocean}`). yaml `backend:` value unchanged. **Hard requirement after the relevant phase merges** — the in-core backend is deleted, not deprecated.
- `platform.kubernetes` / `platform.ecs` / etc. with a cloud `provider:` → load the matching plugin. yaml `provider:` value unchanged. Hard requirement after the relevant phase.
- `aws.apigateway` and other former `cloud.account`-brokered AWS modules → module type renamed to plugin-native form; `credentials:` block moves inline (or `credentials_ref:` an `aws.credentials` module). **This is the only yaml-shape change.**
- `memory` / `filesystem` / `postgres` state backends, `kind` k8s backend → no change, still core.

## Assumptions

1. **gRPC's 4 MB default message cap covers real-world IaC state files.** If a deployment's state exceeds 4 MB the unary `IaCStateBackend` contract needs streaming — the benchmark task validates the typical case but a hostile-large state is out of initial scope (documented limitation, not a silent failure: `SaveState` returns a clear "state exceeds transport limit" error). The benchmark runs before the proto is locked.
2. **The `platform.*` backend interfaces are cleanly provider-separable.** The design assumes `kubernetesBackend` / `ecsBackend` / etc. are interface-segregated such that the `kind` impl can stay while cloud impls extract. **This is the most fragile assumption** — the Phase 0/A interface-audit spike (first writing-plans task) validates this that validates it; if a backend interface leaks SDK types into the core module shell, that shell needs an interface-extraction refactor first and the phase re-scopes.
3. **Plugins may ship ahead of core.** A plugin implementing `IaCStateBackend`/`PlatformBackend` against the published proto is harmless to load on a core version that doesn't yet dispatch to it — the contract is additive, core ignores unknown backend registrations until its own half lands.
4. **`aws-sdk-go-v2/service/s3` in workflow-plugin-digitalocean is acceptable.** DO Spaces is S3-API; there is no godo-native Spaces client. The DO plugin already carries `godo`; adding one AWS service package is the minimal cost of self-contained `spaces` state support (vs. forcing DO users to also load workflow-plugin-aws).
5. **`cloud_account_azure.go` / `cloud_account_gcp.go` genuinely have zero real SDK imports.** Verified by `awk` over import blocks at design time — they reference the SDKs only in comments. If a future change adds a real SDK import there, that file joins its phase's extraction.
6. **No core code outside `module/` imports these SDKs.** Verified: the only real `aws-sdk-go-v2` / `azure-sdk-for-go` / `cloud.google.com` imports are under `module/`. `cmd/`, `engine.go`, `schema/`, `plugin/` are clean. A `go list -deps` CI gate in the final phase enforces this permanently.

## Rollback

This design changes **plugin loading paths** and **go.mod dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list.

- **Per-phase revert:** each phase is an isolated core PR + plugin PR(s). Reverting the **core PR** restores the in-process backend `switch` / `platform.*` cloud backends and re-adds the SDK to `go.mod` — the deleted files are recoverable from git. The plugin PRs are additive (new contract impls / module types) and can stay merged harmlessly even if core reverts. **Phase D has no separate core PR** — its core deletion *is* Phase B's core PR — so a Phase D rollback means reverting Phase B's core PR + the DO plugin PR together.
- **Forward-fix preferred over revert:** because core keeps the old in-process path until the contract dispatch is wired *in the same core PR*, a broken phase fails at PR CI (image-launch / strict-contracts gates), not in production. The revert path exists but the gate is the primary safety.
- **`spaces` clean-break (Phase B core PR + Phase D plugin PR):** the only change with an external-user-visible compat break. Rollback = revert Phase B's core PR (restores `iac_state_spaces.go` + the `spaces` case) **and** revert the DO plugin minor bump, together — they are a matched pair. The migration doc + the DO plugin's `minEngineVersion` bump is the forward guard: a user on a core version past Phase B without the new DO plugin gets a clear build-time "backend spaces requires workflow-plugin-digitalocean ≥ X" error, not a silent failure.

## Alternatives Considered

1. **Fold cloud platform provisioners into the existing `IaCProviderRequired` / `ResourceDriver` contracts instead of inventing `PlatformBackend`.** An EKS/GKE/AKS cluster — and arguably an ECS service, a Route53 zone, an EC2 VPC — is structurally a managed resource with create/plan/apply/destroy/status, which is exactly what the battle-tested `ResourceDriver` contract already models (8 services in `iac.proto`, multiple ADRs through the strict-contracts cutover). Inventing `PlatformBackend` risks the lowest-common-denominator problem (self-challenge doubt #1). **Rejected as the default** because the `platform.*` modules have a distinct plan/apply *lifecycle surface* (they sync against live cloud state continuously, not just declaratively reconcile) and a distinct `provider:` UX the user explicitly asked to preserve — but **retained as the gated fallback**: the Phase 0/A interface-audit spike decides. If the five `platform.*` backend interfaces don't unify behind one `Plan/Apply/Destroy`, the implementation folds them into `ResourceDriver` rather than shipping a bad `PlatformBackend`.
2. **Leave `iac_state_spaces.go` in core, accept one `aws-sdk-go-v2/service/s3` dependency.** Downgrades the Goal from "core drops `aws-sdk-go-v2/*` entirely" to "drops the AWS *service-provider* tree, keeps one S3 client." The S3 client is small and stable; DO Spaces + AWS S3 are the same API; keeping one shared S3-compatible store in core avoids forcing *both* the AWS and DO plugins to each carry an S3 client and avoids a clean-break for existing `spaces` users. **Rejected** because it leaves dependabot churning one AWS package indefinitely and weakens the "core has zero cloud SDKs" invariant the `go list -deps` gate is meant to enforce — a partial extraction is a maintenance trap. The cost (both aws + DO plugins carry an S3 client) is real but bounded: it's one service package, and each plugin is independently versioned anyway.
3. **A shared `s3compat` Go module consumed by both the aws and DO plugins** (instead of each independently re-implementing the S3-compatible state store + `buildAWSConfig`). Keeps "core has zero cloud SDKs" intact while eliminating the cross-plugin duplication Alternative #2 dismisses as "bounded." **Deferred, not rejected:** it is a *plugin-side* optimisation that doesn't affect the core contract or any phase boundary, so it can land as a follow-up after the extraction is proven. Forcing it into the critical path now couples the aws and DO plugin release cadences; the duplication is a small, well-understood `buildAWSConfig` + thin S3 wrapper. Writing-plans logs it as a post-extraction cleanup candidate.
4. **In-process Go-module plugin loading (build-tag imports) instead of gRPC sidecars.** Rejected in brainstorm by explicit user decision — strict gRPC sidecar model only.

## Self-challenge — top doubts surfaced (carried forward, with mitigations now wired into phases)

Two distinct mitigations cover three doubts (#1 and #2 share the interface-audit spike — that is intentional, not redundant coverage theatre):

1. **`PlatformBackend` may be over-general** AND **2. clean provider-separability (Assumption 2) is fragile.** Both are settled by the *one* interface-audit spike — Phase 0/A task 1, ordered before the proto lock. If the five `platform.*` backend interfaces don't unify behind one `Plan/Apply/Destroy`, the fallback is folding cloud platform provisioners into `ResourceDriver` (Alternatives Considered #1); if a backend interface leaks SDK types into its core module shell, the phase re-scopes to do the interface-extraction refactor first. Phase 0's mechanical file-split also de-risks #2 structurally — each backend's imports are isolated before any extraction.
3. **The state-backend benchmark could come back "streaming required"** and reshape the `IaCStateBackend` proto. Mitigation: benchmark is a Phase A task ordered *before* the proto lock — the proto is not committed until the benchmark result is in.

## Open items deferred to writing-plans

- Exact proto field layouts for both new contracts (sketches above are directional; field-level layout follows the benchmark + interface-audit results).
- Whether `PlatformBackend` ships as designed or folds into `ResourceDriver` (gated on the interface-audit spike — Alternatives Considered #1).
- Benchmark harness location + the concrete acceptance threshold (p99 added latency bar).
- Exact wording of the secret-redaction extension + whether existing redaction already covers `credentials:` keys.
- The `s3compat` shared-module cleanup (Alternatives Considered #3) — logged as a post-extraction follow-up candidate, not in the critical path.
- Per-plugin CHANGELOG entries + the consolidated migration doc wording.
