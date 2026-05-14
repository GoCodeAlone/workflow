# Cloud-SDK Extraction: workflow core → strict-contract plugins

**Date:** 2026-05-14
**Status:** Design — revised after adversarial review cycle 4
**Owner:** autonomous pipeline (workflow#TBD)

## Problem

Workflow core's `module/` package imports three cloud SDK trees directly. File counts are grep-verified (`awk` over import blocks, comment-only matches excluded). "Files" = files with a real import — not all are *deleted* (e.g. `iac_module.go` is *edited* to strip a `case`, not deleted; see Phases):

| SDK | Files (real imports) | how core sheds it |
|-----|----------------------|-------------------|
| `github.com/aws/aws-sdk-go-v2/*` | **13** | 11 deleted, `iac_module.go` edited (strip `spaces` case), `platform_kubernetes_eks.go` deleted (post Phase-0 split) |
| `github.com/Azure/azure-sdk-for-go/sdk/*` (azcore + azblob) | **2** | `iac_state_azure.go` deleted, `iac_module.go` edited (strip `azure_blob` case) |
| `cloud.google.com/go/storage` + `google.golang.org/api/*` | **3** | `iac_state_gcs.go` + `storage_gcs.go` deleted, `platform_kubernetes_gke.go` deleted (post Phase-0 split) |

Every dependabot bump of a cloud SDK (PRs #400/#419/#421/#635 as of this writing) churns workflow core's `go.sum`, inflates the binary, and couples core release cadence to vendor SDK release cadence. The `workflow-plugin-{aws,azure,gcp,digitalocean}` plugins already exist and already carry these SDKs for their IaC *resource provider* role — core's direct usage is redundant surface.

Precedent: workflow#617 removed the legacy DigitalOcean IaC *resource* modules + `godo` from those; IaC resource provisioning moved to `workflow-plugin-digitalocean`. This design extends the same principle to the *remaining* cloud functionality that never went through that extraction: IaC **state backends**, managed-service **platform** provisioners, and a handful of standalone modules/steps.

**A fourth tree — `github.com/digitalocean/godo` — is still in core but out of scope here.** `module/cloud_account_do.go` + five `module/platform_do_*.go` files (`platform_do_app.go`, `platform_do_dns.go`, `platform_do_networking.go`, `platform_doks.go`, `platform_do_database.go`) still import `godo` — workflow#617 removed the DO *IaC resource* path but these `platform.do_*` modules survived it. The user's ask scoped this work to three SDK trees (aws/azure/gcp); `godo` extraction is a structurally-identical follow-up (the `platform.do_*` modules would extract via the same `PlatformBackend` contract this design introduces) but is **not** in this design's scope. Consequence: the `go list -deps` CI gate added in the final phase asserts **zero `aws-sdk-go-v2` / `azure-sdk-for-go` / `cloud.google.com` / `google.golang.org/api` packages** — it does *not* assert "zero cloud SDKs" while `godo` remains. The design's phrasing is corrected throughout to "the three in-scope SDK trees," not "all cloud SDKs."

## Goals

- workflow core `go.mod` drops `aws-sdk-go-v2/*`, `Azure/azure-sdk-for-go/*`, `cloud.google.com/go/*`, `google.golang.org/api/*` (the three in-scope trees) **entirely** — verified by a `go list -deps` gate in the final phase's CI asserting zero packages from those three trees. `godo` is out of scope (see Problem).
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
message LockRequest       { string resource_id = 1; }  // 1:1 with IaCStateStore.Lock — no TTL field (see Failure modes)
// IaCState mirrors module.IaCState. The proto is exactly the 6-method interface,
// nothing speculative — a lock-lease/TTL field is a planned ADDITIVE follow-up
// (Failure modes §), deferred until the first plugin backend implements honored
// expiry so it ships with a conformance test instead of as a no-op field.
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
// Each request carries: platform_type (kubernetes|ecs|...), provider (eks|gke|aks|...),
//   desired-state struct, current-state struct, AND a CloudCredentials message.
// Response carries: plan actions / applied state / errors.
// (remaining request/response message field layouts: deferred to writing-plans.)
```

**Credential flow across the boundary.** Every cloud `platform.*` backend today reaches credentials via `k.provider.GetCredentials()` returning a `module.CloudCredentials` struct (`module/cloud_account.go:18`) — `eksBackend`, `gkeBackend`, and `aksBackend` all bind to it; `aksBackend.azureToken` even takes `*CloudCredentials` directly. `cloud_account.go` (`CloudCredentials` / `CloudCredentialProvider` / `CloudAccount`) **stays in core** — it is the provider-agnostic credential abstraction, not cloud-SDK code. When a backend moves to a plugin, the engine resolves `k.provider.GetCredentials()` *in-core* (no SDK needed — it's config-map parsing) and serialises the resulting `CloudCredentials` into a proto `CloudCredentials` message carried on every `PlatformBackend` request. The plugin builds its cloud SDK client from that message. **This is the same shape as the §Architecture-3 `credentials:` story** — one `CloudCredentials` proto message serves both the `PlatformBackend` contract and the plugin-native module path, so the secret-redaction task (§Security) has exactly one shape to redact, not two.

When `provider != kind` (and `!= k3s` — `k3s` also maps to the in-core `kindBackend`), core's `platform.*` module resolves a `PlatformBackend` client from the plugin that registered `(platform_type, provider)`.

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

- **Plugin crashes between `Lock` and `Unlock` → orphaned lock.** An in-process lock dies with the process; a gRPC-plugin lock can outlive a plugin crash if the plugin persisted it (S3/Blob lock objects do persist). **Initial scope:** this is a *documented limitation*, not silently broken. The `IaCStateBackend` contract ships as exactly the 6-method `IaCStateStore` interface — no TTL field — because no plugin backend in Phases A–D implements honored expiry yet, and a no-op TTL field is worse than none (it implies a guarantee that isn't enforced). Recovery for an orphaned lock is operator-side: delete the backend's lock object directly (it is a plain object/blob in the user's own bucket — `aws s3 rm`, `az storage blob delete`, etc.; the lock key format is documented per backend). **Planned additive follow-up:** once the first plugin backend implements honored expiry (S3 object-expiry metadata, Blob lease duration), `LockRequest` gains an optional `lease_ttl_seconds` field *paired with a contract conformance test* that asserts the plugin's lock object actually carries expiry — shipped with semantics, not as a field. Tracked as an open item.
- **`Lock` contention against a still-held lock.** Core's `iac.state` dispatch returns an immediate error on `Lock` contention — it does **not** block waiting for the lock to free. This matches today's in-process `IaCStateStore.Lock` ("Returns an error if the resource is already locked"). The gRPC boundary does not change this: a held lock — whether held by a live plan or orphaned by a dead plugin — surfaces the same immediate "resource locked" error, and orphaned-lock recovery is the operator-side delete above. No new waiting/lease-timeout state is introduced.
- **`SaveState` succeeds plugin-side but the gRPC response is lost → engine retries → double-write.** `SaveState` MUST be idempotent: it is a full-state replace keyed by `resource_id` (the existing `IaCStateStore.SaveState` is already "insert or replace"), so a retried identical `SaveState` is a no-op-equivalent. The contract documents `SaveState` as idempotent; the plugin implementations use unconditional PUT (overwrite), not append. No sequence number needed — IaC state is last-writer-wins by design.
- **Plugin unreachable at plan/apply start.** Core's `iac.state` dispatch returns a clear `"iac.state backend %q: plugin unreachable"` error and the plan/apply aborts *before* mutating anything — no partial state. This matches today's behavior when a misconfigured backend fails to construct in `IaCModule.Init()`.
- **`PlatformBackend` plugin crash mid-`Apply`.** A `platform.*` apply that crashes mid-flight leaves real cloud resources in an indeterminate state — but this is **identical to today's in-process risk** (an in-process `eksBackend.apply()` panic leaves the same indeterminate cloud state). The gRPC boundary does not worsen it; the next `Plan` reconciles against live cloud state as it does today. No new mitigation needed — documented as unchanged.

## Cross-file coupling: the symbol-ownership audit is a Phase 0 deliverable, not a design-doc table

Three prior review cycles each found a hand-maintained per-file ownership table in this design *wrong* — the design doc is the wrong place for a precise symbol map, because the map is derived data that rots on every edit. **The map is therefore a Phase 0 build artifact, not a design claim.** What the design commits to is the *method* and the *known shape*:

**Known shape (the parts that survive any audit):**
- `module/platform_kubernetes_kind.go` currently holds **four** backends (`kindBackend`, `eksBackend`, `gkeBackend`, `aksBackend`) plus one shared `func init()` registering five names — `kind`, `k3s`, `eks`, `gke`, `aks` (`k3s` reuses `kindBackend`). `module/platform_kubernetes.go` is a **separate, already-existing** file holding the `PlatformKubernetes` module shell + the `kubernetesBackend` interface — untouched by the split.
- **All three cloud backends bind to `module/cloud_account.go`** via `k.provider.GetCredentials() → CloudCredentials`. `cloud_account.go` (`CloudCredentials` / `CloudCredentialProvider` / `CloudAccount`) is the provider-agnostic credential abstraction — **it stays in core**, is never deleted by any phase, and is the symbol home all cloud platform code binds to. The `PlatformBackend` contract carries `CloudCredentials` across the boundary (§Architecture-2).
- `eksBackend` *additionally* binds to `cloud_account_aws.go` (`awsProviderFrom`, `AWSConfig`, `parseStringSlice`) — and `cloud_account_aws.go` is **deleted by Phase B**. `eksBackend` and `cloud_account_aws.go` therefore leave core in the same Phase B commit.
- `aksBackend` imports **no cloud SDK** — raw `net/http` REST against the Azure management API (the stale file-header comment "Requires the Azure SDK" notwithstanding). Its extraction is code-organisation, not a dependency change; the Azure go.mod drop comes entirely from `iac_state_azure.go` deletion + `iac_module.go` edit.

**The method — `scripts/audit-cloud-symbols.sh`, produced as Phase 0 task 1:** a script that, for each backend region and each plugin-bound `module/*.go` file, greps every cross-file function/type reference and emits the authoritative ownership map. Its output is committed alongside Phase 0 and re-run in CI on every subsequent phase PR. The design does not transcribe its output — the script *is* the source of truth, eliminating the recurring transcription defect. Two helper funcs are already known to need relocation (below); the script catches any the eye missed.

## Phase 0 — precursor: split `platform_kubernetes_kind.go`, partition `init()`, relocate shared helpers

A mechanical, behavior-equivalent refactor landed **before** Phase A. Three moves:

**1. Split the one shared backend file into four.** `platform_kubernetes_kind.go` (currently all four backends) → `platform_kubernetes_kind.go` (`kindBackend` only), `platform_kubernetes_eks.go`, `platform_kubernetes_gke.go`, `platform_kubernetes_aks.go`. Each new file owns its own import block.

**2. Partition the shared `func init()` per-file.** The one `init()` registering `kind`/`k3s`/`eks`/`gke`/`aks` **cannot** be split untouched — each new file gets its own `init()` registering only its backend(s) (`kind` *and* `k3s` both register from `platform_kubernetes_kind.go`, since `k3s` reuses `kindBackend`). This is a *distribution* of the registration, not a behavior change — the same five names are registered after the split — but it is **not** "zero logic change," and the design says so plainly. The payoff: when Phase A deletes `platform_kubernetes_aks.go`, the `aks` registration goes with it; no dangling `RegisterKubernetesBackend("aks", …)` is left behind for the build gate to catch as a late surprise.

**3. Relocate the two shared pure helpers into a new SDK-free core file** `module/cloud_helpers.go`:
- `parseStringSlice` moves out of `cloud_account_aws.go` (Phase B deletes that file) — its plugin-bound consumers (`platform_ecs.go`, `platform_kubernetes_eks.go`) would otherwise lose it.
- `safeIntToInt32` moves out of `platform_kubernetes.go` — used by `platform_autoscaling.go`, `platform_ecs.go`, `platform_networking.go`, `platform_kubernetes_eks.go` (all plugin-bound) *and* by core-resident `platform_kubernetes.go`. A neutral home keeps both sides compiling.

Both helpers are tiny pure functions (no SDK, no state). `cloud_helpers.go` stays in core permanently. When a plugin-bound file moves to its plugin, that plugin gets its own copy of whichever helpers it uses (≤15 lines each — duplicating a pure stdlib-only helper across a process boundary is correct, not a smell; the shared plugin-side util module is the Alternatives-Considered-#3 follow-up).

**Phase 0 acceptance criteria:** `go build ./... && go vet ./... && go test ./module/...` green; `scripts/audit-cloud-symbols.sh` committed and its output shows zero cross-file symbol dep from any plugin-bound file into a to-be-deleted file *except* the known `eksBackend → cloud_account_aws.go` edge (which Phase B handles atomically); `git diff` is pure code movement + the mechanical `init()` partition, no logic edits. After Phase 0, each subsequent phase deletes *its own* backend file — self-contained at import-block AND symbol level.

**Phase 0 rollback:** a file-split + `init()` partition + helper-relocation with no behavior diff — revert is a single `git revert`, no contract, no go.mod, no runtime impact. The one phase with a trivial rollback story.

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
2. **The `platform.*` backend interfaces are cleanly provider-separable.** The design assumes `kubernetesBackend` / `ecsBackend` / etc. are interface-segregated such that the `kind` impl can stay while cloud impls extract. **This is the most fragile assumption** — the Phase 0/A interface-audit spike (first writing-plans task) validates it; if a backend interface leaks SDK types into the core module shell, that shell needs an interface-extraction refactor first and the phase re-scopes. Phase 0's mechanical split + helper relocation de-risks this structurally: after Phase 0, the audit operates on already-separated files, not an assertion about an unsplit one.
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
2. **Leave `iac_state_spaces.go` in core, accept one `aws-sdk-go-v2/service/s3` dependency.** Downgrades the Goal from "core drops `aws-sdk-go-v2/*` entirely" to "drops the AWS *service-provider* tree, keeps one S3 client." The S3 client is small and stable; DO Spaces + AWS S3 are the same API; keeping one shared S3-compatible store in core avoids forcing *both* the AWS and DO plugins to each carry an S3 client and avoids a clean-break for existing `spaces` users. **Rejected** because it leaves dependabot churning one AWS package indefinitely and weakens the "core drops the three in-scope SDK trees" invariant the `go list -deps` gate enforces — a partial extraction is a maintenance trap. The cost (both aws + DO plugins carry an S3 client) is real but bounded: it's one service package, and each plugin is independently versioned anyway.
3. **A shared `s3compat` Go module consumed by both the aws and DO plugins** (instead of each independently re-implementing the S3-compatible state store + `buildAWSConfig`). Keeps the three-in-scope-trees invariant intact while eliminating the cross-plugin duplication Alternative #2 dismisses as "bounded." **Deferred, not rejected:** it is a *plugin-side* optimisation that doesn't affect the core contract or any phase boundary, so it can land as a follow-up after the extraction is proven. Forcing it into the critical path now couples the aws and DO plugin release cadences; the duplication is a small, well-understood `buildAWSConfig` + thin S3 wrapper. Writing-plans logs it as a post-extraction cleanup candidate.
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
