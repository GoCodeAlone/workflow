# Cloud-SDK Extraction: workflow core → strict-contract plugins

**Date:** 2026-05-14
**Status:** Design — re-baselined after adversarial review cycle 8 (design verified against current `origin/main`, post-#653)
**Owner:** autonomous pipeline (workflow#TBD)

## Relationship to issue #653 (predecessor — read this first)

Issue **#653** ("Audit AWS SDK usage in workflow core") closed 2026-05-13. Its three merged PRs already did a large slice of AWS-side extraction:

- **PR #657** — removed the AWS *IaC modules* from workflow core.
- **PR #659** — stripped the AWS SDK from the `codebuild` and `EKS` backends (replaced `codebuildAWSBackend` and `eksBackend` with SDK-free migration-error stubs).
- **PR #662** — tombstoned `platform/providers/aws/` and promoted the EKS absent-package CI gate.

Consequence: files this design's *earlier drafts* (cycles 1–7) enumerated — `aws_api_gateway.go`, `codebuild.go`, `platform_apigateway.go`, `platform_autoscaling.go`, `platform_ecs.go`, `platform_networking.go`, the `awsProviderFrom` helper, the `platform_*_aws` backends — **no longer exist in `module/`**. #653 explicitly scoped *out* "RBAC/secrets/artifact" AWS usage ("RBAC/secrets/artifact stay"). **This design is #653's successor: it extracts the AWS SDK surface #653 deliberately left, plus the entirely-untouched Azure and GCP surfaces.** Every file/symbol claim below is grep-verified against `origin/main` HEAD (the worktree branch is 0 commits behind `origin/main`).

## Problem

Workflow core's `module/` package still imports three cloud SDK trees directly. Real-import counts (comment-only matches excluded — e.g. `storage_artifact_s3.go` *names* `aws-sdk-go-v2` only in a doc comment and stays in core):

| SDK tree | Files with a real import | how core sheds it |
|----------|--------------------------|-------------------|
| `github.com/aws/aws-sdk-go-v2/*` | **6** — `cloud_account_aws.go`, `cloud_account_aws_creds.go`, `iac_state_spaces.go`, `nosql_dynamodb.go`, `pipeline_step_s3_upload.go`, `s3_storage.go` | 4 deleted, `cloud_account_aws_creds.go` edited (resolver bodies rewritten SDK-free, stays in core), `iac_module.go` edited (strip `spaces` case) |
| `github.com/Azure/azure-sdk-for-go/sdk/*` (azcore + azblob) | **3** — `iac_state_azure.go`, `iac_module.go`, `platform_kubernetes_kind.go` (the `aksBackend`) | `iac_state_azure.go` deleted, `iac_module.go` edited (strip `azure_blob` case + `newAzureSharedKeyCredential`), `platform_kubernetes_aks.go` deleted (post Phase-0 split) |
| `cloud.google.com/go/storage` + `google.golang.org/api/*` | **3** — `iac_state_gcs.go`, `storage_gcs.go`, `platform_kubernetes_kind.go` (the `gkeBackend`) | `iac_state_gcs.go` + `storage_gcs.go` deleted, `platform_kubernetes_gke.go` deleted (post Phase-0 split) |

`cloud_account_azure.go` and `cloud_account_gcp.go` are **already SDK-free** (verified: 0 SDK imports — they are pure declare-don't-resolve resolver files). Only the AWS credential resolvers carry SDK.

Every dependabot bump of a cloud SDK churns workflow core's `go.sum`, inflates the binary, and couples core release cadence to vendor SDK release cadence. The `workflow-plugin-{aws,azure,gcp,digitalocean}` plugins already exist and already carry these SDKs for their IaC *resource provider* role — core's direct usage is redundant surface.

Precedent: workflow#617 removed the legacy DigitalOcean IaC *resource* modules + `godo`; #653 removed the AWS IaC *resource* modules + AWS `platform/providers/`. This design extends the same principle to the *remaining* cloud functionality neither extraction touched: IaC **state backends**, the managed-Kubernetes **platform** provisioners, and a handful of standalone modules/steps.

**A fourth tree — `github.com/digitalocean/godo` — is still in core but out of scope here.** `module/cloud_account_do.go` + the `module/platform_do_*.go` files still import `godo`; workflow#617 removed the DO *IaC resource* path but the `platform.do_*` modules survived it. The user's ask scoped this work to three SDK trees (aws/azure/gcp); `godo` extraction is a structurally-identical follow-up but is **not** in this design's scope. Consequence: the `go list -deps` CI gate added in the final phase asserts **zero `aws-sdk-go-v2` / `azure-sdk-for-go` / `cloud.google.com` / `google.golang.org/api` packages** — it does *not* assert "zero cloud SDKs" while `godo` remains.

## Goals

- workflow core `go.mod` drops `aws-sdk-go-v2/*`, `Azure/azure-sdk-for-go/*`, `cloud.google.com/go/*`, `google.golang.org/api/*` (the three in-scope trees) **entirely** — verified by a `go list -deps` gate in the final phase's CI asserting zero packages from those three trees. `godo` is out of scope.
- Cloud functionality remains available, loaded via strict-contract gRPC plugins (the existing sidecar model).
- `kind` / `k3s` Kubernetes backends (no SDK) stay in core — local-dev/test path must not require a plugin. The `eks` backend is *already* an SDK-free migration-error stub (`eksErrorBackend`, courtesy #653) and stays in core unchanged.

## Non-Goals

- Re-homing the IaC *resource provider* contract (`IaCProviderRequired`) — already extracted (#617, #653), not touched here.
- Changing how plugins are discovered/installed (`wfctl plugin install` flow unchanged).
- Backwards-compatible yaml — this is a **clean break** with a migration guide (per workflow#617 / #653 precedent).
- **Removing `aws-sdk-go-v2/service/kinesis`.** `go mod why` resolves it to `workflow → workflow/module → modular/modules/eventbus/v2 → kinesis` — a **transitive dependency of `modular`**, not a direct workflow import. Out of scope; an upstream `modular` concern.
- **Re-doing #653's work.** The AWS IaC modules, `platform/providers/aws/`, and the codebuild/EKS backends are already gone. This design does not re-extract them.

## Architecture

Three extension surfaces, three handling strategies:

### 1. IaC state backends → new `IaCStateBackend` strict proto contract

`iac.state` **stays a core module type**. The state store is engine infrastructure — the orchestrator reads/writes it during every plan/apply cycle — so it keeps a stable core seam. What changes: `config.backend` no longer dispatches a hardcoded `switch` in `module/iac_module.go`; instead core resolves an `IaCStateBackend` gRPC client from whichever loaded plugin registered that backend name.

The contract maps **1:1 onto the existing `module.IaCStateStore` interface** (`module/iac_state.go`) — six methods, no speculative surface:

```proto
// Added as a new service INSIDE plugin/external/proto/iac.proto — matches the
// established precedent (iac.proto already holds multiple services; state +
// platform contracts version alongside the resource-provider contract).
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
// IaCState mirrors module.IaCState. A lock-lease/TTL field is a planned ADDITIVE
// follow-up (Failure modes §), deferred until the first plugin backend implements
// honored expiry so it ships with a conformance test instead of as a no-op field.
```

Backend ownership — every cloud plugin implements the contract for its native storage:

| backend name | plugin | storage | core file deleted |
|--------------|--------|---------|-------------------|
| `s3`         | workflow-plugin-aws | AWS S3 | `iac_state_spaces.go` (the `SpacesIaCStateStore` — an S3-compatible store backing *both* `s3` and `spaces`) |
| `azure_blob` | workflow-plugin-azure | Azure Blob | `iac_state_azure.go` |
| `gcs`        | workflow-plugin-gcp | Google Cloud Storage | `iac_state_gcs.go` |
| `spaces`     | workflow-plugin-digitalocean | DO Spaces (S3-compatible) | (shares `iac_state_spaces.go` deletion — see Phase D) |

`memory`, `filesystem`, `postgres` backends stay **in core** — no cloud SDK, no reason to extract.

**Unary GET+SAVE vs streaming:** decided by benchmark, not assumption. The writing-plans phase includes a task that drives a 1 MB synthetic state blob through a full plan→apply cycle (GetState + SaveState + Lock + Unlock per resource batch) over unary RPC, measures p50/p99 added latency vs the in-process baseline, and only adopts chunked streaming if unary clears no acceptable bar. Default build target: **unary** — (a) gRPC's default 4 MB message cap covers typical state files, (b) streaming adds protocol complexity that must be justified by data, (c) the in-process baseline this replaces was itself a single blob read/write. This task is ordered **before** the Phase A proto is locked.

### 2. Managed-Kubernetes platform provisioners → new `PlatformBackend` strict proto contract

**Post-#653 this surface is small.** The only `module/platform_*.go` file that still imports a cloud SDK is `platform_kubernetes_kind.go`, which holds **four** `kubernetesBackend` implementations behind one shared `init()`:

| backend | SDK | disposition |
|---------|-----|-------------|
| `kindBackend` (serves `kind` *and* `k3s`) | none | **stays in core** |
| `eksErrorBackend` (serves `eks`) | none (already a #653 migration-error stub) | **stays in core** unchanged |
| `gkeBackend` (serves `gke`) | `google.golang.org/api/container` | → workflow-plugin-gcp |
| `aksBackend` (serves `aks`) | `Azure/azure-sdk-for-go` | → workflow-plugin-azure |

There is **no** `platform.ecs` / `platform.networking` / `platform.autoscaling` / `platform.dns` / `platform.apigateway` cloud-SDK surface left — #653 removed it. `platform_dns.go` / `platform_dns_backends.go` still exist but carry **no cloud SDK import** (verified). So `PlatformBackend` serves exactly two cloud backends: `aks` and `gke`.

`platform.kubernetes` keeps its module type **and its `provider:` config key** in core — no yaml UX break. The `kubernetesBackend` interface (`module/platform_kubernetes.go`) stays in core; the cloud impls move behind the `PlatformBackend` gRPC contract.

```proto
// Added as a new service INSIDE plugin/external/proto/iac.proto (same rationale
// as IaCStateBackend — co-versioned with the resource-provider contract).
service PlatformBackend {
  rpc Plan   (PlatformPlanRequest)    returns (PlatformPlanResponse);
  rpc Apply  (PlatformApplyRequest)   returns (PlatformApplyResponse);
  rpc Destroy(PlatformDestroyRequest) returns (PlatformDestroyResponse);
}
// Each request carries: platform_type (currently always "kubernetes"),
//   provider (gke|aks), desired-state struct, current-state struct, AND a
//   CloudCredentials message. Response: plan actions / applied state / errors.
// (remaining message field layouts: deferred to writing-plans.)
```

**The `PlatformBackend` shape is gated, but the gate is now nearly trivial** (cycle-8 note): the design's earlier draft worried about unifying *five* `platform.*` backend interfaces. Post-#653 there is **one** interface to audit — `kubernetesBackend` (4 methods: `plan`/`apply`/`status`/`destroy`) — and only two cloud impls behind it (`gkeBackend`, `aksBackend`), both managed-Kubernetes clusters. The Phase 0/A interface-audit spike validates that `kubernetesBackend`'s 4 methods map cleanly onto `Plan/Apply/Destroy/Status` *before* the proto is locked. The risk that drove Alternatives Considered #1 (lowest-common-denominator across heterogeneous platform families) is largely gone with the heterogeneous families gone — but the fallback (fold into `ResourceDriver`) is retained for the spike's decision.

**Credential flow across the boundary — in-core resolvers *declare*, the plugin *resolves*.** The cloud backends reach credentials via `module.CloudCredentials` (`module/cloud_account.go`); `aksBackend.azureToken(creds *CloudCredentials)` takes it directly (verified). Verified shape of the existing pieces:
- `CloudCredentials` is a **plain-field struct** — `Provider/Region/AccessKey/SecretKey/SessionToken/RoleARN/ProjectID/TenantID/ClientID/.../Token` plus `Extra map[string]string`. No `Profile` field; `profile` lives in `Extra["profile"]`. Cleanly proto-serialisable as-is — **no struct change needed**.
- The AWS credential *resolvers* split two ways. `awsStaticResolver` / `awsEnvResolver` are **already SDK-free**. `awsProfileResolver` and `awsRoleARNResolver` (verified, `cloud_account_aws_creds.go`) have an **SDK-bearing block** (`config.LoadDefaultConfig(WithSharedConfigProfile)`, `sts.AssumeRole`) that resolves the profile/role into `AccessKey/SecretKey` *in-core*. The azure/gcp resolvers (`cloud_account_azure.go`, `cloud_account_gcp.go`) are **already SDK-free**.

The model: make **every** in-core resolver uniformly *declare, don't resolve*. Phase B **rewrites** the two SDK-bearing AWS resolver bodies — a deliberate `Resolve()` body rewrite, **not** a one-line "snip the tail":
- `awsProfileResolver.Resolve` — its SDK calls (`config.LoadDefaultConfig(WithSharedConfigProfile)`, `cfg.Credentials.Retrieve`) *are* a clean contiguous tail after the marker-record (`m.creds.Extra["profile"] = profile`); the rewrite ends the method right after the marker-record.
- `awsRoleARNResolver.Resolve` — the SDK block (base-config build + `sts.NewFromConfig` + `AssumeRole` + result-record) is contiguous *after* the declared-input recording (`RoleARN`, `Extra["external_id"]`, `roleArn`-required validation, `sessionName` parse) but is the **larger half** of the method. The rewrite **deletes that entire block** and ends the method after the declared-input recording + a `credential_source` marker. Calling this "remove a tail" understates it — it is a body rewrite.

After both rewrites, `cloud_account_aws_creds.go` imports **no** `aws-sdk-go-v2` package (verified: the 4 SDK imports — `aws`, `config`, `credentials`, `sts` — are used *only* by those two resolver bodies; `init()` + `awsStaticResolver` + `awsEnvResolver` are SDK-free) and **stays in core**. **Phase B CI invariant:** an import-block grep (folded into `scripts/audit-cloud-symbols.sh`) asserts `cloud_account_aws_creds.go` has zero `aws-sdk-go-v2` imports post-rewrite.

The engine serialises the resolver-populated `CloudCredentials` struct into a proto `CloudCredentials` message on every `PlatformBackend` request. The **plugin** performs any SDK-bearing resolution (profile-chain, STS AssumeRole, managed-identity, ADC) in-process.

When `provider ∉ {kind, k3s, eks}` core's `platform.kubernetes` module resolves a `PlatformBackend` client from the plugin that registered `(kubernetes, provider)`.

### 3. Standalone modules / steps → plugin-native types (existing SDK surface, no new contract)

These are user-facing pipeline functionality, not engine infrastructure. They become **plugin-native module/step types** via the existing `ModuleFactories` / `StepFactories` plugin SDK — already a gRPC sidecar path (`RemoteModule`). No new contract. **Note on current registration site:** these types are today registered by *built-in in-process engine plugins* under `plugins/` (which import `module.*` directly), not by `engine.go`. Extracting each one means the built-in plugin's factory map drops that entry and the impl moves to the external gRPC plugin.

| core file | current built-in registration | becomes | plugin |
|-----------|-------------------------------|---------|--------|
| `nosql_dynamodb.go` (`DynamoDBNoSQL`) | `plugins/datastores/plugin.go` `"nosql.dynamodb"` | `nosql.dynamodb` module | aws |
| `pipeline_step_s3_upload.go` (`S3UploadStep`) | `plugins/pipelinesteps/plugin.go` `"step.s3_upload"` | `step.s3_upload` | aws |
| `s3_storage.go` (`S3Storage`) | `plugins/storage/plugin.go` `"storage.s3"` (factory at :90) | `storage.s3` module | aws |
| `storage_gcs.go` (`GCSStorage`) | `plugins/storage/plugin.go` `"storage.gcs"` (factory at :109) | `storage.gcs` module | gcp |

`storage_artifact_s3.go` references the AWS SDK **only in a doc comment** (verified — its actual imports are `context`/`fmt`/`io`/`modular`; the real impl is a filesystem fallback) — **not a real import, stays in core untouched.**

`cloud_account_aws.go` — defines `AWSConfigProvider` interface + `AWSConfig()` method + `ValidateCredentials()` method, all pure SDK — is **dead code**: a repo-wide grep for `AWSConfigProvider` / `awsProviderFrom` / `.AWSConfig(` returns **zero non-test consumers** (the `awsProviderFrom` helper and every consumer were removed by #653). It is **deleted outright by Phase B with no consumer rewrite and no core replacement** — this is a trivial dead-code deletion, not the multi-consumer refactor earlier drafts described.

Credential handling (Option 1, approved): each plugin-native AWS module carries its own `credentials:` config block and resolves it in-process via a shared in-plugin `buildAWSConfig` helper that owns the static/env/profile/role_arn logic — exactly the workflow-plugin-digitalocean model. To avoid yaml redundancy when a config declares many AWS modules, each plugin offers an optional in-plugin `aws.credentials` (resp. `gcp.credentials`) module + a `credentials_ref:` key — DRY handled entirely inside the plugin, still no core contract.

**Resolvers emit *markers*, not always plain values.** For credential types `static` / `env`, the in-core resolver records concrete declared values into `CloudCredentials`. For `profile` / `role_arn` (AWS) and `managed_identity` / `client_credentials` / `cli` (azure) and the gcp equivalents, the resolver records the *declared inputs* (`Extra["profile"]`, `RoleARN`, etc.) **plus** an `Extra["credential_source"]` marker — it does **not** resolve to concrete keys. The plugin reads the marker and performs the SDK-bearing resolution. This is not a "no-op passthrough": the plugin **must** implement marker handling for every deferred type.

## Security

Option 1 moves raw cloud secrets (`accessKey`/`secretKey`/`account_key`/etc.) inline into every plugin-native module's `credentials:` config block — multiplying the number of config sites holding plaintext secrets versus today's single `cloud.account` module. Not unprecedented (`iac_module.go`'s current `spaces` case already inlines `accessKey`/`secretKey`), but the multiplication needs explicit handling:

- **Config-version store + execution tracing.** Workflow's config-version store (SHA-256 content-addressed) and execution-tracing layer marshal module config. Plugin-native module config carrying inline credentials MUST be redacted before persistence/tracing. Writing-plans task: extend the existing PII/secret redaction (already per-tenant-toggleable per `workflow-cloud`) to recognise `credentials:` / `credentials_ref:` keys on plugin module config, OR confirm the existing redaction already covers any key matching a secret-pattern. **Blocking** — ships in the same phase as the first plugin-native AWS module.
- **gRPC sidecar request logging.** `IaCStateBackend` / `PlatformBackend` requests cross the engine↔plugin gRPC boundary, and `credentials:` blocks ride in `CreateModule` requests. **Verified at design time:** `plugin/external/grpc_plugin.go:39` constructs the server as `grpc.NewServer(opts...)` with `opts` passed straight through from the go-plugin broker — workflow's plugin SDK adds **no body-logging interceptor**. The only request-body logging in `plugin/external/` is `callback_server.go:85,118` (plugin→host callback path) — neither touches module config. `CreateModule` is dispatched at `adapter.go:477` with no logging. **Conclusion: no redacting interceptor needed today.** Writing-plans adds a guard test asserting no interceptor logs `CreateModule` bodies, so a future SDK change that adds one fails CI.
- **`credentials_ref:` blast radius.** A `credentials_ref` resolves to an in-plugin `aws.credentials` module within the *same plugin process* — it does not broaden which process can read the secret (engine never sees the resolved `aws.Config`, only the plugin does). Strictly *narrower* than today's `cloud.account` (which builds `aws.Config` in the engine process). Documented as an improvement.

## Failure modes

Moving the IaC state store behind a gRPC sidecar introduces a partial-failure surface on the engine's hottest path (every plan/apply does `Lock` → `GetState` → ... → `SaveState` → `Unlock`):

- **Plugin crashes between `Lock` and `Unlock` → orphaned lock.** An in-process lock dies with the process; a gRPC-plugin lock can outlive a plugin crash if persisted (S3/Blob lock objects persist). **Initial scope:** documented limitation, not silently broken. The contract ships as exactly the 6-method `IaCStateStore` interface — no TTL field — because no Phase A–D plugin backend implements honored expiry yet, and a no-op TTL field implies a guarantee that isn't enforced. Recovery: operator deletes the backend's lock object directly (a plain object/blob in the user's own bucket; lock key format documented per backend). **Planned additive follow-up:** once a backend implements honored expiry, `LockRequest` gains an optional `lease_ttl_seconds` field *paired with a conformance test*. Tracked as an open item.
- **`Lock` contention against a still-held lock.** Core's `iac.state` dispatch returns an immediate error — it does not block. Matches today's in-process `IaCStateStore.Lock`. The gRPC boundary doesn't change this; orphaned-lock recovery is the operator-side delete above.
- **`SaveState` succeeds plugin-side but the gRPC response is lost → engine retries → double-write.** `SaveState` MUST be idempotent: full-state replace keyed by `resource_id` (existing `IaCStateStore.SaveState` is already insert-or-replace), so a retried identical `SaveState` is no-op-equivalent. Plugin implementations use unconditional PUT (overwrite), not append. IaC state is last-writer-wins by design.
- **Plugin unreachable at plan/apply start.** Core's `iac.state` dispatch returns a clear `"iac.state backend %q: plugin unreachable"` error and the plan/apply aborts *before* mutating anything. Matches today's behavior when a misconfigured backend fails to construct in `IaCModule.Init()`.
- **`PlatformBackend` plugin crash mid-`Apply`.** A `platform.kubernetes` apply crashing mid-flight leaves a real cloud cluster in an indeterminate state — but this is **identical to today's in-process risk** (an in-process `aksBackend.apply()` panic leaves the same indeterminate state). The next `Plan` reconciles against live cloud state as today. Documented as unchanged.
- **A plugin registers a backend/provider name colliding with a core-reserved one.** Core-registered names (`iac.state`: `memory`/`filesystem`/`postgres`; `platform.kubernetes`: `kind`/`k3s`/`eks`; the `mock` backend of every `platform.*` family) are **reserved**. A colliding plugin registration is a **load-time error** — core fails to start with `"plugin %q registered reserved backend name %q"` rather than silently shadowing.

## Cross-file coupling: the symbol-ownership map is a Phase 0 build artifact, not a design-doc claim

Prior review cycles each found a hand-maintained per-file ownership claim in this design *wrong* — and cycle 8 found the whole inventory stale because it predated #653. The lesson is structural: **a precise symbol map is derived data; it rots on every upstream merge and the design doc is the wrong place for it.** The design commits to a *method* and a small set of *invariants*, and delegates the exact map to a script that runs in CI.

**Invariants (load-bearing; the script verifies them, it doesn't discover them):**
- `module/cloud_account.go` (`CloudCredentials` / `CloudCredentialProvider` / `CloudAccount`) is the provider-agnostic *declared-config* holder — **it stays in core, is never deleted by any phase**, and is the credential symbol-home all cloud platform code binds to. The `PlatformBackend` contract carries the declared `CloudCredentials` across the boundary.
- `module/platform_kubernetes_kind.go` co-locates **core-staying** backends (`kindBackend` serving kind+k3s, `eksErrorBackend` serving eks) and **plugin-bound** cloud backends (`gkeBackend`, `aksBackend`) behind a *single shared `func init()`* (verified — `init()` registers all five names). Splitting it requires partitioning that `init()`. **Phase 0 does exactly this** — and it is the *only* `platform.*` file needing a split, because #653 already removed the rest.
- `cloud_account_aws.go` is **dead code, deleted outright by Phase B.** It defines `AWSConfig()` / `AWSConfigProvider` / `ValidateCredentials` — all pure SDK — and a repo-wide grep confirms **zero non-test consumers** of any of them (`awsProviderFrom` and its consumers were removed by #653). No consumer rewrite, no helper relocation: earlier drafts' "8-consumer rewrite" and "`parseStringSlice` relocation" are obsolete — `parseStringSlice` and `safeIntToInt32` **no longer exist anywhere in `module/`** (verified). There is no shared-helper-relocation work in this design.

**The method — `scripts/audit-cloud-symbols.sh`, Phase 0 task 1:** for the `platform_kubernetes_kind.go` split and each plugin-bound `module/*.go` file, it greps every cross-file function/type reference and asserts (a) no `init()` registers a *mix* of core-staying and plugin-bound factories, (b) no cross-file symbol edge from a core-staying file into a to-be-deleted file, (c) `cloud_account_aws_creds.go` has zero `aws-sdk-go-v2` imports after the Phase B resolver rewrite. Committed with Phase 0, re-run in CI on every subsequent phase PR. The design never transcribes its output — the script *is* the source of truth.

## Phase 0 — precursor: split the one remaining mixed cloud-backend file

Post-#653, Phase 0 is small: a mechanical, behavior-equivalent split of the **single** `module/` file that still co-locates core-staying and plugin-bound cloud backends.

**1. Split `platform_kubernetes_kind.go`** into:
- `platform_kubernetes_core.go` — holds `kindBackend` (serves `kind` + `k3s`), `eksErrorBackend` (serves `eks`), and an `init()` registering **only** those three core-staying names.
- `platform_kubernetes_gke.go` — holds `gkeBackend` + its `google.golang.org/api/container` import + an `init()` registering only `gke`.
- `platform_kubernetes_aks.go` — holds `aksBackend` (incl. `azureToken`) + its `Azure/azure-sdk-for-go` import + an `init()` registering only `aks`.

After the split, no `init()` registers both a core-staying and a plugin-bound factory, and no file holds both a core-staying and a plugin-bound backend impl. `platform_kubernetes.go` (the `PlatformKubernetes` shell, `kubernetesBackend` interface, `RegisterKubernetesBackend`, `intFromAny` helper) is untouched and stays in core.

**2. Create `scripts/audit-cloud-symbols.sh`** (the Cross-file-coupling method above). No shared-helper-relocation step — there are no shared helpers to relocate (verified).

This is **not** "zero logic change" — partitioning a shared `init()` distributes registration calls across files. It is *behavior-equivalent*: the same five backend names are registered after the split as before.

**Phase 0 acceptance criteria:** `go build ./... && go vet ./... && go test ./module/...` green; `scripts/audit-cloud-symbols.sh` committed, output shows no mixed `init()` and no cross-file edge into a to-be-deleted file; `git diff` is pure code movement + mechanical `init()` partition, no logic edits.

**Phase 0 rollback:** a file-split + `init()` partition with no behavior diff — revert is a single `git revert`, no contract, no go.mod, no runtime impact.

## Phases

Each phase is one workflow-core PR (deleting/editing files + wiring the contract dispatch) plus one PR per affected plugin. Within a phase, the plugin PR may merge ahead of the core PR — core keeps the old in-process path until the contract dispatch is wired in the core PR. **Atomicity rule:** within a core PR, a deleted file and every file referencing its symbols are removed in the *same commit* (the build gate enforces this).

**Phase A — Azure** (smallest; validates BOTH new contracts end-to-end):
- Run the state-backend benchmark task; lock the `IaCStateBackend` proto shape.
- Run the `kubernetesBackend` interface-audit spike; lock or re-scope the `PlatformBackend` proto shape.
- Add `IaCStateBackend` + `PlatformBackend` services to `plugin/external/proto/iac.proto`.
- Add the secret-redaction task + the gRPC-interceptor guard test (blocking).
- workflow-plugin-azure implements `azure_blob` `IaCStateBackend` + `aks` `PlatformBackend`.
- Core PR: delete `iac_state_azure.go`; strip the `azure_blob` case + `newAzureSharedKeyCredential` from `iac_module.go`; delete `platform_kubernetes_aks.go` (the Phase-0 split file) and wire its `PlatformBackend` dispatch. This drops `Azure/azure-sdk-for-go` from `go.mod`.

**Phase B — AWS.** Inventory + destination (the authoritative list is the audit-script output):

| core file | disposition | atomicity note |
|-----------|-------------|----------------|
| `iac_state_spaces.go` | DELETE → aws plugin `s3` `IaCStateBackend` | shared with `spaces` — see Phase D |
| `cloud_account_aws.go` | DELETE outright — dead code, **zero non-test consumers verified** | no consumer rewrite; trivial deletion |
| `cloud_account_aws_creds.go` | **EDIT** — rewrite `awsProfileResolver`/`awsRoleARNResolver` bodies SDK-free; file stays in core | the resolver `init()` registrations stay — `provider: aws` credential resolution still works in-core, now declare-only |
| `nosql_dynamodb.go` | DELETE → aws plugin `nosql.dynamodb`; drop the entry from `plugins/datastores/plugin.go` | same commit as the built-in-plugin factory-map edit |
| `pipeline_step_s3_upload.go` | DELETE → aws plugin `step.s3_upload`; drop from `plugins/pipelinesteps/plugin.go` | same commit |
| `s3_storage.go` | DELETE → aws plugin `storage.s3`; drop from `plugins/storage/plugin.go` | same commit |

- Core PR also: **strip the `spaces` case from `iac_module.go`** (it calls `NewSpacesIaCStateStore` from the deleted `iac_state_spaces.go`). Drop `aws-sdk-go-v2` from `go.mod`.
- **No AWS `platform.*` work** — #653 already stubbed `eks` (`eksErrorBackend` stays in core) and removed `platform/providers/aws/`.
- `storage_artifact_s3.go` stays in core (comment-only SDK reference).

**Phase C — GCP:**
- workflow-plugin-gcp implements `IaCStateBackend` (`gcs`), `PlatformBackend` (`gke`), plugin-native `storage.gcs`.
- Core PR: delete `iac_state_gcs.go`, `storage_gcs.go` (drop the entry from `plugins/storage/plugin.go`), `platform_kubernetes_gke.go` (the Phase-0 split file); strip the `gcs` case from `iac_module.go`; drop `cloud.google.com/go` + `google.golang.org/api`. After Phase C, `go list -deps ./...` shows zero packages from the three in-scope SDK trees — the permanent CI gate is added here. (`godo` remains — out of scope.)

**Phase D — DigitalOcean (`spaces` clean-break):**
- workflow-plugin-digitalocean implements `IaCStateBackend` for `spaces` (S3-compatible — pulls `aws-sdk-go-v2/service/s3`, the one service package, not the whole tree).
- **Clean break, not soft-compat.** `iac_state_spaces.go` + the `spaces` case in `iac_module.go` are deleted by **Phase B's core PR** (`iac_state_spaces.go` is the one S3-compatible store backing *both* `s3` and `spaces`). After Phase B's core PR merges, `iac.state` with `backend: spaces` fails to build unless the DO plugin version implementing `IaCStateBackend` is loaded.
- **Minor version bump** on workflow-plugin-digitalocean (compatibility-break marker) + `minEngineVersion` set to the core version that drops the in-core `spaces` case + migration doc.
- **Sequencing:** the DO plugin PR (implementing `spaces` `IaCStateBackend`) MUST merge + release **before** Phase B's core PR merges — otherwise `backend: spaces` has no implementation anywhere. Writing-plans orders the DO plugin PR as a Phase-B blocker.

## Migration (user-facing)

Published in each plugin's CHANGELOG + a consolidated `docs/migrations/2026-05-14-cloud-sdk-extraction.md`:

- `iac.state` with `backend: s3|azure_blob|gcs|spaces` → load the matching plugin (`wfctl plugin install workflow-plugin-{aws,azure,gcp,digitalocean}`). yaml `backend:` value unchanged. **Hard requirement after the relevant phase merges.**
- `platform.kubernetes` with `provider: gke|aks` → load the matching plugin. yaml `provider:` value unchanged. (`kind`/`k3s`/`eks` unchanged — still core.)
- `nosql.dynamodb`, `step.s3_upload`, `storage.s3`, `storage.gcs` → load the matching plugin. Module/step type names unchanged; `credentials:` block moves inline (or `credentials_ref:` an in-plugin `aws.credentials`/`gcp.credentials` module). **This inline-credentials move is the only yaml-shape change.**
- `memory` / `filesystem` / `postgres` state backends, `kind`/`k3s`/`eks` k8s backends, `storage.artifact` (`storage_artifact_s3.go`) → no change, still core.

## Assumptions

1. **gRPC's 4 MB default message cap covers real-world IaC state files.** If a deployment's state exceeds 4 MB the unary `IaCStateBackend` contract needs streaming — the benchmark task validates the typical case; a hostile-large state is out of initial scope (`SaveState` returns a clear "state exceeds transport limit" error). The benchmark runs before the proto is locked.
2. **`kubernetesBackend` is cleanly provider-separable.** The design assumes the `kubernetesBackend` interface is segregated such that `kindBackend`/`eksErrorBackend` can stay while `gkeBackend`/`aksBackend` extract. Post-#653 this is **much less fragile than earlier drafts** — there is one interface, not five, and `eksErrorBackend` already proves a core-staying SDK-free impl coexists with cloud impls behind the same interface. The Phase 0/A interface-audit spike still validates it formally before the proto lock.
3. **Plugins may ship ahead of core.** A plugin implementing `IaCStateBackend`/`PlatformBackend` against the published proto is harmless to load on a core version that doesn't yet dispatch to it — the contract is additive.
4. **`aws-sdk-go-v2/service/s3` in workflow-plugin-digitalocean is acceptable.** DO Spaces is S3-API; there is no godo-native Spaces client. Adding one AWS service package is the minimal cost of self-contained `spaces` state support.
5. **The credential resolvers can all be made SDK-free in-core.** `cloud_account_azure.go` / `cloud_account_gcp.go` are *already* SDK-free (verified — 0 SDK imports); `cloud_account_aws_creds.go`'s `awsStaticResolver`/`awsEnvResolver` are already SDK-free, and `awsProfileResolver`/`awsRoleARNResolver` become SDK-free once their SDK blocks are rewritten out (Phase B). The load-bearing assumption: a resolver does not *need* to resolve in-core — for deferred credential types it records declared inputs + an `Extra["credential_source"]` marker, and the plugin honors the marker. The plugin **must** implement marker handling for every deferred type.
6. **No core code outside `module/` imports these SDKs.** Verified: the only real `aws-sdk-go-v2` / `azure-sdk-for-go` / `cloud.google.com` / `google.golang.org/api` imports are under `module/`. A `go list -deps` CI gate in Phase C enforces this permanently.
7. **#653 is final and merged.** This design builds on `origin/main` post-#653. If #653 work were reverted, this design's file inventory would need re-baselining — but #653's issue is *closed* and all three PRs are merged, so this is a stable foundation.

## Rollback

This design changes **plugin loading paths** and **go.mod dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list.

- **Per-phase revert:** each phase is an isolated core PR + plugin PR(s). Reverting the **core PR** restores the in-process backend `switch` / cloud backends and re-adds the SDK to `go.mod` — deleted files recoverable from git. Plugin PRs are additive and can stay merged harmlessly even if core reverts. **Phase D has no separate core PR** — its core deletion *is* Phase B's core PR — so a Phase D rollback means reverting Phase B's core PR + the DO plugin PR together.
- **Forward-fix preferred over revert:** because core keeps the old in-process path until the contract dispatch is wired *in the same core PR*, a broken phase fails at PR CI (image-launch / strict-contracts gates), not in production. The revert path exists but the gate is the primary safety.
- **`spaces` clean-break (Phase B core PR + Phase D plugin PR):** the only change with an external-user-visible compat break. Rollback = revert Phase B's core PR (restores `iac_state_spaces.go` + the `spaces` case) **and** revert the DO plugin minor bump, together — a matched pair. The migration doc + the DO plugin's `minEngineVersion` bump is the forward guard.

## Alternatives Considered

1. **Fold the cloud Kubernetes provisioners into the existing `IaCProviderRequired` / `ResourceDriver` contract instead of inventing `PlatformBackend`.** A GKE/AKS cluster is structurally a managed resource with create/plan/apply/destroy/status — exactly what `ResourceDriver` already models. **Rejected as the default** because `platform.kubernetes` has a distinct `provider:` UX the user explicitly asked to preserve, and a continuous-reconciliation lifecycle surface — but **retained as the gated fallback**: the Phase 0/A `kubernetesBackend` interface-audit spike decides. Post-#653 the case for a dedicated `PlatformBackend` is *weaker* (only 2 cloud backends, both Kubernetes) — the spike may well conclude `ResourceDriver` suffices. The design defers to the spike rather than pre-committing.
2. **Leave `iac_state_spaces.go` in core, accept one `aws-sdk-go-v2/service/s3` dependency.** Downgrades the Goal from "core drops `aws-sdk-go-v2/*` entirely" to "keeps one S3 client." **Rejected** because it leaves dependabot churning one AWS package indefinitely and weakens the `go list -deps` gate. The cost (both aws + DO plugins carry an S3 client) is real but bounded — one service package, independently versioned.
3. **A shared `s3compat` Go module consumed by both the aws and DO plugins** (instead of each re-implementing the S3-compatible state store + `buildAWSConfig`). **Deferred, not rejected:** a *plugin-side* optimisation that doesn't affect the core contract or any phase boundary — lands as a follow-up after the extraction is proven. Writing-plans logs it as a post-extraction cleanup candidate.
4. **In-process Go-module plugin loading (build-tag imports) instead of gRPC sidecars.** Rejected in brainstorm by explicit user decision — strict gRPC sidecar model only.
5. **Wait for / extend #653 to also extract state backends + `platform.kubernetes`.** #653's issue is closed with an explicit scope boundary ("RBAC/secrets/artifact stay"). Extending a closed issue rather than opening a clearly-scoped successor would muddy the audit trail. **Rejected** — this design is the named successor and cites #653 as predecessor.

## Self-challenge — top doubts surfaced (carried forward, with mitigations wired into phases)

1. **`PlatformBackend` may be over-general** AND **2. clean provider-separability (Assumption 2) is fragile.** Both are settled by the *one* `kubernetesBackend` interface-audit spike — Phase 0/A task 1, ordered before the proto lock. Post-#653 both doubts are materially smaller: one interface, two cloud impls, and `eksErrorBackend` already demonstrates an SDK-free core impl behind that interface. If `kubernetesBackend`'s 4 methods don't map cleanly onto `Plan/Apply/Destroy/Status`, the fallback is folding into `ResourceDriver` (Alternatives #1).
3. **The state-backend benchmark could come back "streaming required"** and reshape the `IaCStateBackend` proto. Mitigation: benchmark is a Phase A task ordered *before* the proto lock.
4. **The inventory could be stale again** — cycle 8 caught exactly this (the design predated #653). Mitigation: every file/symbol claim in this revision is grep-verified against `origin/main` HEAD, the worktree is confirmed 0 commits behind `origin/main`, and `scripts/audit-cloud-symbols.sh` (Phase 0 task 1) makes the inventory a CI-enforced build artifact from Phase 0 onward — not a prose claim that can rot.

## Open items deferred to writing-plans

- Exact proto field layouts for both new contracts (sketches above are directional; field-level layout follows the benchmark + interface-audit results).
- Whether `PlatformBackend` ships as designed or folds into `ResourceDriver` (gated on the `kubernetesBackend` interface-audit spike — Alternatives Considered #1).
- Benchmark harness location + the concrete acceptance threshold (p99 added latency bar).
- Exact wording of the secret-redaction extension + whether existing redaction already covers `credentials:` keys.
- The `s3compat` shared-module cleanup (Alternatives Considered #3) — logged as a post-extraction follow-up candidate.
- Per-plugin CHANGELOG entries + the consolidated migration doc wording.
