# 0037. gke cross-process contract — fold into the existing ResourceDriver

**Status:** Superseded by ADR 0055
**Date:** 2026-05-14
**Decision-makers:** Jon (operator), autonomous pipeline
**Related:** docs/plans/2026-05-14-cloud-sdk-extraction-design.md (Architecture §2), docs/plans/2026-05-14-cloud-sdk-extraction-bcd.md (Task 19, PR 7), decisions/0035, decisions/0026

> **Superseded:** ADR 0055 replaces this GKE-specific host contract with
> manifest-declared, provider-neutral Kubernetes backend bindings. The body
> below is preserved as the historical rationale for reusing `ResourceDriver`.

## Context

Phase C of the cloud-SDK extraction moves the **one SDK-bearing `platform.*` backend** —
`gkeBackend` (`module/platform_kubernetes_gke.go`, imports `google.golang.org/api/container/v1`
+ `google.golang.org/api/option`) — out of `workflow` core into `workflow-plugin-gcp`. The
design (Architecture §2) deliberately did **not** pre-commit to a cross-process contract for
`gke`: a brand-new single-backend proto service is a YAGNI risk. It gated the contract shape on
this interface-audit spike, ordered before any Phase C plugin work.

### The in-core interface

`platform.kubernetes` dispatches on its `type:` config key (`module/platform_kubernetes.go:87`
reads `config["type"]`; schema `schema/module_schema.go:2665` exposes it as `type`, one of
`eks | gke | aks | kind | k3s`) to a `kubernetesBackend` (`module/platform_kubernetes.go:46-51`)
— a **4-method** interface. (The design §2's `provider:` wording is imprecise and is superseded
here: the dispatch config key is `type:`. The string `gke` is *also* recorded into the
`KubernetesClusterState.Provider` field, which is the likely source of the design's slip.)

| method | signature | gke implementation (`platform_kubernetes_gke.go`) |
|---|---|---|
| `plan`    | `plan(k) (*PlatformPlan, error)`             | `Clusters.Get` existence probe → `PlatformPlan{create}` or `{noop}` |
| `apply`   | `apply(k) (*PlatformResult, error)`          | `Clusters.Create`; swallows `ALREADY_EXISTS` as success |
| `status`  | `status(k) (*KubernetesClusterState, error)` | `Clusters.Get` → fills the typed `KubernetesClusterState` |
| `destroy` | `destroy(k) error`                           | `Clusters.Delete`; swallows `NOT_FOUND` as success |

Credentials reach the backend through `k.provider` (`CloudCredentialProvider`); the
`containerService` helper builds the SDK client from `CloudCredentials.ServiceAccountJSON` via
`option.WithCredentialsJSON`. **Confirmed one-shot lifecycle:** every method is a single discrete
SDK call — no goroutine, no watch loop, no ticker, no continuous reconciliation. The next `plan`
reconciles against live cloud state, exactly as `ResourceDriver` consumers already do.

### The three options (design preference order)

1. **Fold `gke` into the existing `ResourceDriver` contract** (`iac.proto:78-88`, 9 RPCs). A GKE
   cluster is a managed resource: plan→`Read`, apply→`Create`, status→`Read`, destroy→`Delete`.
   *Preferred* — reuses a battle-tested contract, **zero new proto surface**.
2. **Plugin-native `kubernetesBackend`** via the `ModuleFactories`/`RemoteModule` SDK — only if
   `ResourceDriver`'s lifecycle shape doesn't fit.
3. **A minimal new `PlatformBackend` proto service** — fallback only.

### Strong prior signal — confirmed

`workflow-plugin-gcp` **already implements GKE as a `ResourceDriver`**:

- `provider/drivers/gke.go` — `GKEDriver` implements the full `interfaces.ResourceDriver`
  (`Create`/`Read`/`Update`/`Delete`/`Diff`/`HealthCheck`/`Scale` + `SensitiveKeys`).
- `provider/provider.go:121` registers it under resource type **`infra.k8s_cluster`**
  (line 82 wires the real client); `provider.go:146` declares it in `Capabilities()` as
  `{ResourceType: "infra.k8s_cluster", Tier: 1, Operations: scalableOps}`.
- `provider/drivers/real_clients.go` — `realGKEClient` / `NewRealGKEClient` already carry the
  GCP SDK (`cloud.google.com/go/container/apiv1`) and accept `option.ClientOption` for credentials.

The GKE cross-process path is therefore not a new build — it largely *already exists*.

## Decision

**Option 1 — fold `gke` into the existing `ResourceDriver` contract.** No new proto surface.
The `gke` provider of `platform.kubernetes` dispatches, in core, to a `ResourceDriver` gRPC
client for resource type **`infra.k8s_cluster`**, served by `workflow-plugin-gcp`.

Method mapping (`kubernetesBackend` → `ResourceDriver` RPC):

| `kubernetesBackend` | `ResourceDriver` RPC | notes |
|---|---|---|
| `plan`    | `Read`   | host adapter probes existence, then synthesizes `PlatformPlan{create}` or `{noop}` — the in-core `plan` is itself only a Get-or-create check |
| `apply`   | `Create` | `ALREADY_EXISTS` must resolve to success (see Consequences) |
| `status`  | `Read`   | `ResourceReadResponse.output.outputs_json` carries the cluster fields; host adapter projects them onto the typed `KubernetesClusterState` |
| `destroy` | `Delete` | `NOT_FOUND` must resolve to success (see Consequences) |

**Shape note (`status`).** The in-core `status` returns the rich typed `KubernetesClusterState`
(`Name`/`Provider`/`Version`/`Status`/`Endpoint`/`NodeGroups`/`CreatedAt`). `ResourceOutput`
carries a free-form `outputs_json` map plus a `status` string — it carries the data *cleanly as
JSON*, but not as the typed struct. The host adapter (Tasks 25/26) owns the map→struct
projection: it sets `Provider = "gke"` itself, reads `status`/`endpoint`/`version`/node-pool
entries from `outputs_json`, and tolerates a missing `CreatedAt`. This is the normal
`outputs_json` contract, not a contract gap.

**Rejected — Option 2 (plugin-native `kubernetesBackend`).** `gke`'s 4 methods are confirmed
one-shot lifecycle calls; there is no continuous-reconciliation behavior that would need the
module-native path, and `ResourceDriver`'s CRUD shape already maps 1:1 — so a second contract
would only duplicate one that already exists *and is already implemented for GKE*.

**Rejected — Option 3 (new `PlatformBackend` proto service).** Pure YAGNI: a whole new proto
service for a single backend when `ResourceDriver` already models managed-resource CRUD and the
gcp plugin already serves GKE through it.

**Does the gcp plugin already cover GKE?** Yes — `GKEDriver` under `infra.k8s_cluster`, fully
implemented and capability-declared (see Context). Task 22 is therefore a *verify + harden* task,
not a from-scratch build.

## Consequences

**Task 22 (workflow-plugin-gcp — `gke` cross-process contract).** Not a new implementation; a
hardening pass over the existing `infra.k8s_cluster` `GKEDriver`:
- Confirm the `infra.k8s_cluster` driver + capability are served over the gRPC `ResourceDriver`
  service (they are, post the v1.0.0 typed-IaC migration).
- Make `GKEDriver.Create` idempotent on `ALREADY_EXISTS` and `GKEDriver.Delete` idempotent on
  `NOT_FOUND` — the in-core `gkeBackend` swallowed both; the cross-process path must preserve
  that, either in-driver or via the host adapter (in-driver preferred — keeps parity local).
- Pin the `GKEDriver.Read` output-key set (`status`, `endpoint`, `version`, node-pool list) the
  host adapter reads to reconstruct `KubernetesClusterState`.
- Resolve credentials in-plugin from the serialized `CloudCredentials.ServiceAccountJSON` via
  `option.WithCredentialsJSON`, exactly as the in-core `containerService` did — `NewRealGKEClient`
  already accepts `option.ClientOption`.

**Tasks 25/26 (workflow core — `gke` dispatch + adapter).** Add a host-side `kubernetesBackend`
implementation that dispatches to a `ResourceDriver` gRPC client for `infra.k8s_cluster`,
resolved from `workflow-plugin-gcp` when `type == gke`. It builds `ResourceSpec`/`ResourceRef`
from `PlatformKubernetes` config, serializes `CloudCredentials` into the request, and performs
the method mapping + the `outputs_json`→`KubernetesClusterState` projection above. Precedent: the
Phase A `grpcIaCStateStore` adapter (`module/iac_state_grpc_client.go`). `platform.kubernetes`
keeps its module type and its `type:` config key in core; `kind`/`k3s`/`eks`/`aks` stay
fully in-core and unchanged.

**Proto-surface cost: ZERO.** `iac.proto`'s `ResourceDriver` service and its messages are
untouched. Therefore the plan's parallel-stream model holds in full: **PR 9's proto regen is NOT
a serial prerequisite to PR 8's Task 22** — that caveat (plan PR 9 note) applied only if this ADR
had picked Option 3.

**Open contract detail to pin in Tasks 22 + 25/26.** The exact `outputs_json` key names
`GKEDriver.Read` emits must equal the keys the host adapter reads. Recommend the host adapter
(Task 25/26) define the key contract and Task 22 conform `GKEDriver.Read` to it.

**ADR numbering.** On `workflow` `origin/main` the highest committed ADR is `0035`; `0036` is
reserved by PR 1 (the `IaCStateBackend.Configure` RPC), which is in flight on a parallel branch
and not yet merged. PR 7 is independent of PR 1, so the spike's `tail -1` free-number check shows
`0035` rather than the plan's anticipated `0036` — expected for parallel work. `0037` is the
scope-locked number for this ADR; it follows `0036` once PR 1 merges. (No claim is made about
the wider `decisions/` sequence, which already has pre-existing gaps and duplicates — e.g. a
missing `0019` and two `0014` entries.)
