# Cloud-SDK Extraction — Phases B/C/D Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the AWS, GCP, and DigitalOcean cloud-SDK surface out of `workflow` core's `module/` package into the existing `workflow-plugin-{aws,gcp,digitalocean}` gRPC sidecar plugins — dropping `cloud.google.com/go/*` and `google.golang.org/api/*` from core's build graph entirely and `aws-sdk-go-v2` from core's `module/` package — and close the Phase-A gap that left plugin-served IaC state backends unable to receive their configuration.

**Architecture:** Builds on Phase A's merged patterns (workflow `origin/main` `d179b1aa`): the strict `IaCStateBackend` gRPC contract + `ListBackendNames` RPC, the SDK serve hook (`registerIaCServicesOnly` auto-registers `pb.IaCStateBackendServer`), the engine host-wiring (`loadPluginInternal` → `plugin.IaCStateBackendProvider` → `module.RegisterIaCStateBackend`), `plugin.PluginManifest.IaCStateBackends`, the ctx-ful `module.IaCStateStore`, and the `azureIaCServer`-style cross-repo plugin pattern. **Prerequisite (PRs 1–2):** Phase A's `IaCStateBackend` contract has no config-passing RPC — plugin-served backends round-trip state but cannot receive their bucket/credential config (`workflow-plugin-azure` source: *"backend configuration plumbing … is a follow-up PR"*). This plan adds the `Configure` RPC (`decisions/0036`), wires the host, and retrofits the azure plugin, so plugin-served backends are functional end-to-end. Phase B (AWS) + Phase D (DigitalOcean) share one S3-compatible store; Phase C (GCP) adds the one SDK-bearing `platform.*` backend (`gke`), gated on an interface-audit spike (`decisions/0037`, produced by Task 19).

**Tech Stack:** Go 1.x, gRPC (`go-plugin`), protobuf, `aws-sdk-go-v2`, `cloud.google.com/go`, `google.golang.org/api`, GoReleaser v2, the superpowers autonomous pipeline.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 10
**Tasks:** 29
**Estimated Lines of Change:** ~3100 (informational; not enforced)

**Out of scope:**
- The out-of-`module/` AWS SDK surface — `provider/aws/{clients,deploy,plugin}.go`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go` — the #653-retained "RBAC/secrets/artifact stay" surface. `aws-sdk-go-v2` therefore remains a `go.mod` entry after Phase B (the CI gate is asymmetric — see Task 28).
- `github.com/digitalocean/godo` extraction — structurally-identical follow-up, per design Non-Goals.
- `aws-sdk-go-v2/service/kinesis` — transitive via `modular`, per design Non-Goals.
- The IaC state at-rest format change (JSON → binary/pb) — design Open Item; needs its own brainstorming pass.
- `workflow-plugin-azure` `minEngineVersion` re-pin to a tagged release — blocked on a tagged `workflow` release; tracked separately. (PR 2 *does* retrofit the azure plugin's `Configure` handler — that is in scope per `decisions/0036`; only the version re-pin is out.)
- Multi-`iac.state`-module-per-backend-name support — `decisions/0036` records "one config per backend-name per plugin process" as an accepted limitation inherent to the Phase-A registry shape.
- The comment-only stubs `module/nosql_dynamodb.go` / `module/storage_artifact_s3.go` — they carry no SDK, stay untouched.
- Changing `wfctl plugin install` discovery flow.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | workflow core: `IaCStateBackend.Configure` RPC + host wiring | Task 1, Task 2 | `workflow`: `feat/cloud-sdk-bcd-p1-configure-rpc` |
| 2 | workflow-plugin-azure: `Configure` retrofit + release (cross-repo) | Task 3, Task 4 | `workflow-plugin-azure`: `feat/iac-state-configure` |
| 3 | workflow-plugin-aws: `s3` IaCStateBackend (cross-repo) | Task 5, Task 6 | `workflow-plugin-aws`: `feat/s3-iac-state-backend` |
| 4 | workflow-plugin-aws: `storage.s3` + `step.s3_upload` + in-plugin credentials + release (cross-repo) | Task 7, Task 8, Task 9, Task 10 | `workflow-plugin-aws`: `feat/s3-storage-step-credentials` |
| 5 | workflow-plugin-digitalocean: `spaces` IaCStateBackend + release (cross-repo) | Task 11, Task 12, Task 13 | `workflow-plugin-digitalocean`: `feat/spaces-iac-state-backend` |
| 6 | workflow core: Phase B deletion — AWS files + `spaces` case + resolver rewrite + go.mod | Task 14, Task 15, Task 16, Task 17, Task 18 | `workflow`: `feat/cloud-sdk-bcd-p6-core-aws` |
| 7 | workflow core: `kubernetesBackend` interface-audit spike → ADR 0037 | Task 19 | `workflow`: `feat/cloud-sdk-bcd-p7-gke-spike` |
| 8 | workflow-plugin-gcp: `gcs` IaCStateBackend + `gke` contract + `storage.gcs` + release (cross-repo) | Task 20, Task 21, Task 22, Task 23, Task 24 | `workflow-plugin-gcp`: `feat/gcs-gke-storage` |
| 9 | workflow core: `gke` cross-process wiring (engine seam + adapter + registry) | Task 25, Task 26 | `workflow`: `feat/cloud-sdk-bcd-p9-gke-wiring` |
| 10 | workflow core: Phase C deletion — GCP files + `gcs` case + permanent CI gate | Task 27, Task 28, Task 29 | `workflow`: `feat/cloud-sdk-bcd-p10-core-gcp` |

**Execution order / dependencies:**
- **PR 1** — first; the `Configure` RPC. Every plugin PR (2/3/4/5/8) pins a `workflow` pseudo-version that includes PR 1.
- **PR 2** — depends on PR 1. Retrofits the azure plugin's `Configure` handler (`decisions/0036`).
- **PR 3 → PR 4** — same repo (`workflow-plugin-aws`), sequential; both depend on PR 1; PR 4's final task cuts the aws plugin release tag.
- **PR 5** — depends on PR 1; cuts the DO plugin release tag.
- **PR 6** — depends on **PR 4** (aws tag) **and PR 5** (DO tag): it deletes `iac_state_spaces.go`, the one S3-compatible store backing both the `s3` (aws) and `spaces` (DO) plugin backends.
- **PR 7** — independent (docs/ADR only); must merge before PR 8 and PR 9.
- **PR 8** — depends on **PR 1** and **PR 7** (ADR 0037 fixes the `gke` contract shape); cuts the gcp plugin release tag.
- **PR 9** — depends on **PR 7** (ADR 0037). Additive core wiring. **If ADR 0037 picks Option 3 (a new `PlatformBackend` proto service), PR 9's proto regen becomes a serial prerequisite to PR 8's Task 22** — the parallel-stream model below only holds for Options 1 and 2.
- **PR 10** — depends on **PR 8** (gcp tag) **and PR 9** (gke wiring merged).
- Parallel streams (Options 1/2): `{PR1→PR2}`, `{PR1→PR3→PR4}`, `{PR1→PR5}`, `{PR7→PR8}`, `{PR7→PR9}` run largely in parallel after PR 1; PR 6 joins after PR4+PR5; PR 10 joins after PR8+PR9.
- No PR stacking — every `workflow` PR branches off `origin/main` directly.

**Status:** Draft

---

## Cross-repo note

PRs 2, 3, 4, 5, 8 land in **different git repositories** than the planning worktree. Per `decisions/0034-cross-repo-agent-operation-for-plugin-prs.md` this is **fully autonomous** — implement, push, open PR, AND cut/push the release tag, all following normal review discipline (feature branch → PR → admin-merge → tag; never direct-to-default-branch).

**Every cross-repo task dispatch MUST state, explicitly in the implementer prompt, the absolute path of the repo it works in and that it is a *different* repo than the worktree:**
- `workflow-plugin-azure` → `/Users/jon/workspace/workflow-plugin-azure`
- `workflow-plugin-aws` → `/Users/jon/workspace/workflow-plugin-aws`
- `workflow-plugin-digitalocean` → `/Users/jon/workspace/workflow-plugin-digitalocean`
- `workflow-plugin-gcp` → `/Users/jon/workspace/workflow-plugin-gcp`
- planning worktree (PRs 1/6/7/9/10) → `/Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction`

## Environment note (ALL workflow-core tasks)

The planning worktree sits under a parent `go.work` that does not list it. **Every Go command in `/Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction` must be prefixed `GOWORK=off`** (`GOWORK=off go build ./...`, `GOWORK=off go test ./...`, `GOWORK=off go mod tidy`). IDE "not in workspace" / "undefined" diagnostics there are that artifact, not real — always verify via `GOWORK=off go build`. The plugin repos are normal checkouts — no `GOWORK=off`.

## Workflow-core pin (ALL plugin tasks — PRs 2/3/4/5/8)

These plugins must pin a `workflow` version that includes **PR 1** (the `Configure` RPC). No tagged `workflow` release carries it. **After PR 1 is merged to `workflow` `origin/main`**, in each plugin repo run `go get github.com/GoCodeAlone/workflow@<PR-1-merge-commit-sha>` then `go mod tidy` — this pins the `main` pseudo-version of PR 1's merge commit. Re-pinning to a clean release tag is a tracked follow-up, out of scope here.

---

### Task 1: workflow core — `IaCStateBackend.Configure` RPC

**Repo:** planning worktree (PR 1, branch `feat/cloud-sdk-bcd-p1-configure-rpc`) — `GOWORK=off` on all Go commands.

**Files:**
- Modify: `plugin/external/proto/iac.proto` (add the RPC + 2 messages) — then regenerate the `.pb.go`
- Modify: `module/iac_state_grpc_client.go` (`grpcIaCStateStore` gains a `Configure` client-call method; `iacStateBackendServer` — which embeds `pb.UnimplementedIaCStateBackendServer` — gets a no-op `Configure` only if a non-default is needed)
- Modify: any in-repo `pb.IaCStateBackendClient` / `pb.IaCStateBackendServer` test fakes/stubs (e.g. the `fakeStateBackendClient` added in workflow #673) — add the new `Configure` method so they still satisfy the regenerated interface
- Test: `module/iac_state_grpc_client_test.go`
- Reference (do NOT modify): `iac.proto:350-354` (`InitializeRequest { bytes config_json = 1; }` — the exact precedent this RPC mirrors).

**Context:** `decisions/0036` — the `IaCStateBackend` contract has no config-passing RPC, so plugin-served backends can't receive their YAML config. Add `Configure`, mirroring the existing `InitializeRequest.config_json` pattern (JSON bytes, no `structpb` — the `iac.proto` hard invariant).

**Step 1: Write the failing test**

In `module/iac_state_grpc_client_test.go`: a test that `grpcIaCStateStore.Configure(ctx, backendName, cfgMap)` JSON-encodes `cfgMap` and calls the client's `Configure` with `&pb.ConfigureRequest{BackendName: backendName, ConfigJson: <json>}`. Use a fake client capturing the request.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./module/ -run Configure -v`
Expected: FAIL — `pb.ConfigureRequest` undefined / `grpcIaCStateStore` has no `Configure`.

**Step 3: Add the RPC + regenerate + implement**

`iac.proto` — in `service IaCStateBackend`, add as the first RPC:
```protobuf
  rpc Configure(ConfigureRequest) returns (ConfigureResponse);
```
And the messages (next to the other `IaCStateBackend` messages):
```protobuf
// Configure delivers the iac.state module's YAML config to the plugin so it
// can construct the SDK-backed store. backend_name selects which backend the
// config is for (a plugin may serve more than one). config_json is the
// JSON-encoded module config map[string]any — same JSON-bytes invariant as
// InitializeRequest.config_json. See decisions/0036.
message ConfigureRequest  { string backend_name = 1; bytes config_json = 2; }
message ConfigureResponse {}
```
Regenerate the protobuf Go (the repo's existing codegen command — check `Makefile` / `buf.gen.yaml` / a `//go:generate` directive in `plugin/external/proto/`). Add `grpcIaCStateStore.Configure(ctx, backendName string, cfg map[string]any) error` — `json.Marshal(cfg)` → `client.Configure(ctx, &pb.ConfigureRequest{...})`. If `iacStateBackendServer` (the host-side server delegate) needs a non-Unimplemented `Configure`, make it a no-op returning `&pb.ConfigureResponse{}` (the core-served path has its store already constructed). Fix every in-repo fake/stub so it satisfies the regenerated `pb.IaCStateBackendClient`/`Server` interface — the manually-implemented `fakeStateBackendClient` (in `module/iac_state_plugin_registry_test.go`) needs a `Configure` method; fakes embedding `pb.Unimplemented*` get it for free. Add one line to the proto compile-guard test (`plugin/external/proto/iac_statebackend_test.go` if present): `_ = &ConfigureRequest{BackendName: "x", ConfigJson: []byte("{}")}; _ = ConfigureResponse{}` to lock the new message shape.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./module/ ./plugin/... -run 'Configure|StateBackend|IaCState' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add plugin/external/proto/ module/iac_state_grpc_client.go module/iac_state_grpc_client_test.go
git commit -m "feat: add IaCStateBackend.Configure RPC for backend config plumbing"
```

---

### Task 2: workflow core — host wiring: `iac_module.go` calls `Configure`

**Repo:** planning worktree (PR 1, branch `feat/cloud-sdk-bcd-p1-configure-rpc`) — `GOWORK=off`.

**Files:**
- Modify: `module/iac_module.go` (the `default:` arm of the `Init()` backend switch)
- Test: `module/iac_module_test.go`

**Context:** Today the `default:` arm does `m.store = newGRPCIaCStateStore(client)` and never passes `m.config`. With Task 1's RPC, the host must call `Configure` so the plugin can build its store. Per `decisions/0036`, last-`Configure`-wins per backend-name per plugin process is an accepted limitation.

**Change class:** Plugin-loading path → runtime-launch-validation required. **Rollback: revert PR 1; the proto RPC is additive (plugins embedding `Unimplemented*` are unaffected), the host simply stops calling `Configure`. No data migration.**

**Step 1: Write the failing test**

In `module/iac_module_test.go`: an `IaCModule` with a plugin-backed `backend:` and a `m.config` map; a fake registered client; assert `Init()` calls `client.Configure` with the backend name + the JSON-encoded config *before* the module is usable. Assert a `Configure` error aborts `Init()` with a wrapped error naming the module + backend.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./module/ -run IaCModuleConfigure -v`
Expected: FAIL — `Configure` never called.

**Step 3: Implement**

In `iac_module.go` `Init()` `default:` arm, after `client, ok := iacStateBackendRegistryInstance.resolve(m.backend)`:
```go
		if client, ok := iacStateBackendRegistryInstance.resolve(m.backend); ok {
			store := newGRPCIaCStateStore(client)
			if err := store.Configure(context.Background(), m.backend, m.config); err != nil {
				return fmt.Errorf("iac.state %q: backend %q: configure plugin backend: %w", m.name, m.backend, err)
			}
			m.store = store
			break
		}
```

**Step 4: Run tests + image-launch validation**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./module/ ./... -run 'IaCModule|Engine' -v`
Then runtime-launch-validation (`superpowers:runtime-launch-validation`): build the server, launch against a config with a plugin-backed `iac.state` block (with a stub/test plugin or assert the clean "install the plugin" error path), capture the transcript.
Expected: tests green; server starts; transcript captured; exit 0.

**Step 5: Commit**

```bash
git add module/iac_module.go module/iac_module_test.go
git commit -m "feat: iac_module passes module config to plugin backends via Configure RPC"
```

---

### Task 3: workflow-plugin-azure — `Configure` retrofit

**Repo:** `/Users/jon/workspace/workflow-plugin-azure` (PR 2, branch `feat/iac-state-configure`)

**Files:**
- Modify: `internal/statebackend_server.go` (add `Configure` to `azureIaCServer`)
- Modify: `go.mod`/`go.sum` (pin workflow to PR 1's merge commit — see "Workflow-core pin")
- Test: `internal/statebackend_server_test.go`
- Reference (do NOT modify): `internal/statebackend_server.go` already has the `stateBackend` holder + `setStateStore` + `resolveStore` (the lazy-construction seam) — `Configure` is the missing caller of `setStateStore`.

**Context:** `decisions/0036` mandates retrofitting the azure plugin so `azure_blob` is functional end-to-end (Phase A left the store `nil` → `FailedPrecondition`). The azure plugin already has the `setStateStore`/`resolveStore` lazy seam — this task adds the `Configure` handler that decodes the config and calls `setStateStore`.

**Step 1: Write the failing test** — `internal/statebackend_server_test.go`: call `azureIaCServer.Configure(ctx, &pb.ConfigureRequest{BackendName: "azure_blob", ConfigJson: <json of {accountURL,container,...}>})`; assert `resolveStore()` subsequently returns a non-nil store (not `FailedPrecondition`).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-azure && go test ./internal/ -run Configure -v` → FAIL (`Configure` is the `Unimplemented` default → returns `Unimplemented` status).

**Step 3: Implement** — `azureIaCServer.Configure`: `json.Unmarshal(req.ConfigJson, &cfg)`, validate `req.BackendName == "azure_blob"`, construct `statebackend.NewAzureBlobIaCStateStore(...)` from the decoded config fields (account URL / container / credential — the same fields the deleted in-core `iac_state_azure.go` switch case read), call `s.stateBackend.setStateStore(store)`, return `&pb.ConfigureResponse{}`. Pin workflow to PR 1's merge commit.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-azure && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 5: Commit**
```bash
git add internal/statebackend_server.go internal/statebackend_server_test.go go.mod go.sum
git commit -m "feat: Configure RPC handler — construct azure_blob store from host config"
```

---

### Task 4: workflow-plugin-azure — release

**Repo:** `/Users/jon/workspace/workflow-plugin-azure` (PR 2, branch `feat/iac-state-configure`)

**Files:** Modify `plugin.json` (`version` — **patch** bump: `v1.1.0` → `v1.1.1`), `CHANGELOG.md`.

**Context:** The retrofit must be released so a `workflow` engine with PR 1 actually has a functional `azure_blob` backend. Per `decisions/0034` autonomous.

**Change class:** Version pin update. **Rollback: additive patch release; on a defect cut another patch — do not delete the tag.**

**Step 1:** Bump `plugin.json` `version` → `1.1.1`; CHANGELOG entry: "implement `IaCStateBackend.Configure` — `azure_blob` backend now constructs its store from host-supplied config (closes the Phase-A config-plumbing gap). **Must be co-deployed with `workflow` core that includes PR 1** — a post-PR-1 engine calls `IaCStateBackend.Configure` during `IaCModule.Init()`; `v1.1.0` returns `Unimplemented` and causes a loud startup failure (better than the prior silent `FailedPrecondition`, but a co-deploy requirement)."

**Step 2:** Commit on the PR 2 branch:
```bash
git add plugin.json CHANGELOG.md && git commit -m "chore: release workflow-plugin-azure v1.1.1 — Configure RPC handler"
```

**Step 3:** After PR 2 is merged to the azure plugin default branch, tag from the merged default branch:
```bash
git checkout main && git pull && git tag v1.1.1 && git push origin v1.1.1
```

**Step 4: Verify** — `gh release view v1.1.1 --repo GoCodeAlone/workflow-plugin-azure` shows assets; GoReleaser run `success`.

---

### Task 5: workflow-plugin-aws — port the S3-compatible state store

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 3, branch `feat/s3-iac-state-backend`)

**Files:**
- Create: `internal/statebackend/s3.go`, `internal/statebackend/s3_test.go`
- Reference (source — workflow core, do NOT modify): `module/iac_state_spaces.go`, the proto `IaCState` message at `iac.proto:636` (the canonical state shape), `module/iac_state_spaces_test.go`.

**Context:** `module/iac_state_spaces.go`'s `SpacesIaCStateStore` is an S3-compatible IaC state store (`aws-sdk-go-v2/service/s3` with `UsePathStyle` + `BaseEndpoint`). It backs the in-core `spaces` backend today (there is **no in-core `s3` switch case** — `backend: s3` has only ever been reachable via the Phase-A plugin registry). This task ports the store into the aws plugin as the `s3` backend. The DO plugin ports the same store independently in Task 11 as `spaces`. Mirrors `workflow-plugin-azure/internal/statebackend/azure_blob.go`.

**Step 1: Create the failing test** — copy `module/iac_state_spaces_test.go` → `internal/statebackend/s3_test.go`; `package module` → `package statebackend`; exercise `NewS3IaCStateStoreWithClient` + the 6 ctx-ful methods round-tripping the local `IaCState`.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/statebackend/ -v` → FAIL `undefined: S3IaCStateStore`.

**Step 3: Port the store** — copy `module/iac_state_spaces.go` → `internal/statebackend/s3.go`. Edits:
- `package module` → `package statebackend`.
- Rename `SpacesIaCStateStore` → `S3IaCStateStore`, `NewSpacesIaCStateStore` → `NewS3IaCStateStore`, `NewSpacesIaCStateStoreWithClient` → `NewS3IaCStateStoreWithClient`, `SpacesS3Client` → `S3Client`.
- **Strip the `DO_SPACES_ACCESS_KEY` / `DO_SPACES_SECRET_KEY` env-var fallbacks** from the constructor — they are DigitalOcean-specific and would silently authenticate an aws `s3` backend against DO credentials in a mixed deployment. Replace with the AWS-conventional behavior: if `accessKey`/`secretKey` are empty, do **not** inject static creds — let `aws-sdk-go-v2`'s default credential chain (env `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, instance role, etc.) apply via `config.LoadDefaultConfig`. (The DO plugin's copy in Task 11 keeps the `DO_SPACES_*` fallbacks — that is correct *there*.)
- Define a local `IaCState` struct + `IaCStateStore` interface in this package. **The struct fields must match the proto `IaCState` message (`iac.proto:636`) exactly** — the proto is the canonical wire shape; if the proto and core's Go struct ever diverge, the proto wins. The 6 method signatures are the ctx-ful `module.IaCStateStore` shape. The plugin does NOT import `workflow/module`.
- `go mod tidy`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/statebackend/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/statebackend/ go.mod go.sum
git commit -m "feat: port S3-compatible IaC state store into aws plugin (no DO env fallbacks)"
```

---

### Task 6: workflow-plugin-aws — serve `s3` via `pb.IaCStateBackendServer` (incl. `Configure`)

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 3, branch `feat/s3-iac-state-backend`)

**Files:**
- Create: `internal/statebackend_server.go`, `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `awsIaCServer`; compile-guard)
- Modify: `plugin.json` (`capabilities.iacStateBackends: ["s3"]`)
- Modify: `go.mod`/`go.sum` (pin workflow to PR 1's merge commit)
- Modify/extend: `internal/host_conformance_test.go` (capability ↔ registration parity — see Step 4)
- Reference (do NOT modify): `workflow-plugin-azure/internal/statebackend_server.go` + `internal/iacserver.go` — the exact precedent, including the lazy `stateBackend`/`setStateStore`/`resolveStore` holder and the `Configure` handler shape from Task 3.

**Context:** Mirror `workflow-plugin-azure` exactly. `awsIaCServer` (`internal/iacserver.go:36`) already embeds the 7 `IaCProvider*` + `ResourceDriver` Unimplemented servers. The SDK serve hook (`registerIaCServicesOnly`, workflow #673) auto-registers `pb.IaCStateBackendServer` by type-assertion — no `main.go` change. **Construction is lazy via `Configure`** (the azure precedent): the `stateBackend` holder starts with a `nil` store; `Configure` decodes the host config and calls `setStateStore`; the 6 State RPCs go through `resolveStore()` which returns `FailedPrecondition` until `Configure` has run.

**Step 1: Write the failing test** — `internal/statebackend_server_test.go`: `Configure` with a JSON config builds the store; the 6 RPCs round-trip through `pb` types; `ListBackendNames` → `{["s3"]}`; a State RPC before `Configure` → `FailedPrecondition`. Add compile-guard `var _ pb.IaCStateBackendServer = (*awsIaCServer)(nil)`.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/ -run StateBackend -v` → FAIL `awsIaCServer does not implement pb.IaCStateBackendServer`.

**Step 3: Implement** — port `workflow-plugin-azure/internal/statebackend_server.go`:
- `const awsStateBackendName = "s3"`; `type stateBackend struct { mu sync.Mutex; store *statebackend.S3IaCStateStore }` + `resolveStore()` (`codes.FailedPrecondition` if `nil`) + `setStateStore(...)`.
- On `awsIaCServer`: `Configure` (decode `config_json`, validate `backend_name == "s3"`, `statebackend.NewS3IaCStateStore(region, bucket, prefix, accessKey, secretKey, endpoint)` from the decoded fields, `setStateStore`); the 6 State RPC methods via `resolveStore()`; `ListBackendNames` → `["s3"]`.
- Local `iacStateToPB` / `iacStateFromPB` / `marshalIaCMap` / `unmarshalIaCMap` converters (copy from the azure plugin).
- `internal/iacserver.go`: add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend stateBackend` field + the compile-guard line.
- `plugin.json`: `capabilities.iacStateBackends: ["s3"]`.

**Step 4: Capability parity + tests + host-conformance** — extend `host_conformance_test.go` (or add `internal/capabilities_test.go`): for every `plugin.json` `capabilities.iacStateBackends` entry, assert `NewIaCServer().ListBackendNames` returns it. Then: `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go internal/host_conformance_test.go plugin.json go.mod go.sum
git commit -m "feat: serve s3 IaC state backend via pb.IaCStateBackendServer + Configure"
```

---

### Task 7: workflow-plugin-aws — in-plugin AWS credential resolution (`BuildAWSConfig` + marker handling)

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 4, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/awscreds/awscreds.go`, `internal/awscreds/awscreds_test.go`
- Modify: the aws plugin's existing IaC-provider credential path (locate via `grep -rn 'CloudCredentials\|AccessKey\|LoadDefaultConfig' internal/ provider/`) to route through `BuildAWSConfig`
- Reference (do NOT modify): `workflow` `module/cloud_account_aws_creds.go` (the SDK-bearing `awsProfileResolver`/`awsRoleARNResolver` bodies being re-homed here), `workflow` `module/cloud_account.go` (`CloudCredentials` struct shape).

**Context:** Phase B Task 15 rewrites core's `awsProfileResolver`/`awsRoleARNResolver` to *declare, don't resolve* — they record `Extra["credential_source"] = "profile"|"role_arn"` markers. The SDK-bearing resolution (`config.LoadDefaultConfig(WithSharedConfigProfile)`, `sts.AssumeRole`) is re-homed **here**. `BuildAWSConfig` is the single in-plugin entry point: given a `CloudCredentials` it returns a resolved `aws.Config`, handling static keys, env/default chain, and the `profile`/`role_arn` markers. It also serves the standalone `storage.s3`/`step.s3_upload` inline `credentials:` blocks (Tasks 8/9).

**Step 1: Write the failing test** — `awscreds_test.go`: `BuildAWSConfig` with static keys; with `credential_source: "role_arn"` + a fake STS injection point; with `credential_source: "profile"` (temp `AWS_CONFIG_FILE`); with empty input (default chain, no error).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/awscreds/ -v` → FAIL `undefined`.

**Step 3: Implement** — `internal/awscreds/awscreds.go`: `func BuildAWSConfig(ctx context.Context, creds CredInput) (aws.Config, error)` where `CredInput` carries `AccessKey/SecretKey/SessionToken/Region/RoleARN/ExternalID/Profile/Source`. Logic: `Source == "profile"` → `config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))`; `Source == "role_arn"` (or `RoleARN != ""`) → port the deleted-from-core `awsRoleARNResolver` SDK block (base config + `sts.NewFromConfig` + `AssumeRole`), return a config carrying the assumed creds; static keys → `config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(...))`; else → `config.LoadDefaultConfig(ctx)`. Wire the aws plugin's existing IaC-provider credential path to call `BuildAWSConfig` so a host-supplied `CloudCredentials` with a marker resolves in-plugin.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/awscreds/ ./internal/... -v` → PASS.

**Step 5: Commit**
```bash
git add internal/awscreds/ internal/<modified-provider-path>
git commit -m "feat: in-plugin AWS credential resolution with credential_source marker handling"
```

---

### Task 8: workflow-plugin-aws — plugin-native `storage.s3` + `aws.credentials` DRY module

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 4, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/modules/storage_s3.go`, `internal/modules/storage_s3_test.go`
- Create: `internal/modules/aws_credentials.go`, `internal/modules/aws_credentials_test.go`
- Create: `internal/modules/credref/registry.go` (the process-local `credentials_ref:` registry)
- Modify: the aws plugin's module-factory registration; `plugin.json` (`capabilities.moduleTypes` += `"storage.s3"`, `"aws.credentials"`)
- Reference (do NOT modify): `workflow` `module/s3_storage.go` (`S3Storage` + `NewS3Storage`), `workflow` `plugins/storage/plugin.go:89`.

**Context:** `storage.s3` becomes a plugin-native module via the existing `ModuleFactories` SDK path (no new contract). Credentials move inline per design §3 Option-1: a `credentials:` block resolved via `awscreds.BuildAWSConfig`, OR `credentials_ref:` an in-plugin `aws.credentials` module. **`credentials_ref:` resolution mechanism (explicit per adversarial review):** the aws plugin maintains a *process-local* `credref` registry (a `map[string]CredInput` guarded by a mutex, package `internal/modules/credref`); each `aws.credentials` module registers its resolved `CredInput` under its module name at factory-construction time; `storage.s3`/`step.s3_upload` factories look up `credentials_ref:` in that registry. `credentials_ref:` names **must be unique within a config** — duplicate registration is a factory error, not a silent clobber.

**Step 1: Write the failing tests** — `storage_s3_test.go`: factory builds the module from a config with an inline `credentials:` block AND from one with `credentials_ref:`. `aws_credentials_test.go`: the module registers its `CredInput` in the `credref` registry under its name; a second module with the same name → error.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/modules/... -v` → FAIL `undefined`.

**Step 3: Implement** — `internal/modules/credref/registry.go` (the mutex-guarded `map[string]CredInput` + `Register(name, CredInput) error` rejecting duplicates + `Resolve(name) (CredInput, bool)`). Port `module/s3_storage.go` → `internal/modules/storage_s3.go` (`package modules`, resolve creds via `awscreds.BuildAWSConfig` from the inline block or the `credref` registry). `internal/modules/aws_credentials.go` — the `aws.credentials` module: parses a `credentials:` block into a `CredInput`, registers it in `credref`. Register `"storage.s3"` + `"aws.credentials"` in `ModuleFactories`; update `plugin.json`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/modules/... -v` → PASS.

**Step 5: Commit**
```bash
git add internal/modules/ internal/<factory-file> plugin.json
git commit -m "feat: plugin-native storage.s3 + aws.credentials DRY module with credentials_ref"
```

---

### Task 9: workflow-plugin-aws — plugin-native `step.s3_upload` + capability parity

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 4, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/steps/s3_upload.go`, `internal/steps/s3_upload_test.go`
- Modify: the aws plugin's step-factory registration; `plugin.json` (`capabilities.stepTypes` += `"step.s3_upload"`)
- Modify/extend: `internal/host_conformance_test.go` (final capability ↔ registration parity for everything PR 3 + PR 4 added)
- Reference (do NOT modify): `workflow` `module/pipeline_step_s3_upload.go` (`S3UploadStep` + `NewS3UploadStepFactory`, required config: `bucket`/`region`/`key`/`body_from`), `workflow` `plugins/pipelinesteps/plugin.go:183`.

**Context:** `step.s3_upload` becomes plugin-native via the `StepFactories` SDK path; credentials via an inline `credentials:` block or `credentials_ref:` (Task 8's `credref` registry).

**Step 1: Write the failing test** — `s3_upload_test.go`: factory builds the step from config (`bucket`/`region`/`key`/`body_from` required); creds via inline block or `credentials_ref:`.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/steps/ -run S3Upload -v` → FAIL `undefined`.

**Step 3: Implement** — port `module/pipeline_step_s3_upload.go` → `internal/steps/s3_upload.go` (`package steps`, creds via `awscreds.BuildAWSConfig`). Register `"step.s3_upload"` in `StepFactories`; update `plugin.json`. Extend `host_conformance_test.go`: assert every `moduleTypes`/`stepTypes`/`iacStateBackends` entry this plan added has a registered factory/server.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 5: Commit**
```bash
git add internal/steps/ internal/<factory-file> internal/host_conformance_test.go plugin.json
git commit -m "feat: plugin-native step.s3_upload + capability parity assertion"
```

---

### Task 10: workflow-plugin-aws — release

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 4, branch `feat/s3-storage-step-credentials`)

**Files:** Modify `plugin.json` (`version` — **minor** bump: new `iacStateBackends`/`storage.s3`/`step.s3_upload`/`aws.credentials` capabilities), `CHANGELOG.md`.

**Context:** PR 6 (workflow core Phase B deletion) is blocked on an installable aws plugin release carrying `s3` + `storage.s3` + `step.s3_upload`. Cut after PRs 3 + 4 merge. Per `decisions/0034` autonomous.

**Change class:** Version pin update. **Rollback: additive plugin release; on a defect cut a patch — do not delete the tag.**

**Step 1:** Bump `plugin.json` `version` `v1.0.0` → `v1.1.0` (minor). CHANGELOG entry naming the new backend + module/step types + the inline-`credentials:` shape.

**Step 2:** Commit on the PR 4 branch:
```bash
git add plugin.json CHANGELOG.md && git commit -m "chore: release workflow-plugin-aws <version> — s3 state backend + storage.s3 + step.s3_upload"
```

**Step 3:** After PR 3 and PR 4 are both merged to the aws plugin default branch, tag from the merged default branch:
```bash
git checkout main && git pull && git tag v<version> && git push origin v<version>
```

**Step 4: Verify** — `gh release view v<version> --repo GoCodeAlone/workflow-plugin-aws` shows assets; GoReleaser run `success`.

---

### Task 11: workflow-plugin-digitalocean — port the S3-compatible store

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 5, branch `feat/spaces-iac-state-backend`)

**Files:**
- Create: `internal/statebackend/spaces.go`, `internal/statebackend/spaces_test.go`
- Reference (do NOT modify): `workflow` `module/iac_state_spaces.go`, the proto `IaCState` message (`iac.proto:636`), Task 5 (the structurally-identical aws port).

**Context:** `iac_state_spaces.go` backs both `s3` (aws) and `spaces` (DO). The DO plugin ports the **same store** independently (no shared module — each plugin owns its copy) and serves it as `spaces`. **Unlike the aws copy, the DO copy KEEPS the `DO_SPACES_ACCESS_KEY` / `DO_SPACES_SECRET_KEY` env-var fallbacks** — they are correct here.

**Step 1: Write the failing test** — port `module/iac_state_spaces_test.go` → `internal/statebackend/spaces_test.go` (`package statebackend`, type kept as `SpacesIaCStateStore`).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-digitalocean && go test ./internal/statebackend/ -v` → FAIL `undefined`.

**Step 3: Implement** — copy `module/iac_state_spaces.go` → `internal/statebackend/spaces.go` (`package statebackend`; keep the `Spaces*` names and the `DO_SPACES_*` env fallbacks; define a local `IaCState` struct matching the proto `IaCState` message + the `IaCStateStore` interface; do NOT import `workflow/module`). `go mod tidy`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-digitalocean && go test ./internal/statebackend/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/statebackend/ go.mod go.sum
git commit -m "feat: port S3-compatible IaC state store into DO plugin (spaces)"
```

---

### Task 12: workflow-plugin-digitalocean — serve `spaces` via `pb.IaCStateBackendServer` (incl. `Configure`)

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 5, branch `feat/spaces-iac-state-backend`)

**Files:**
- Create: `internal/statebackend_server.go`, `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `doIaCServer`; compile-guard)
- Modify: `plugin.json` (`capabilities.iacStateBackends: ["spaces"]`; `minEngineVersion` — see Step 4)
- Modify: `go.mod`/`go.sum` (pin workflow to PR 1's merge commit)
- Modify/extend: `internal/host_conformance_test.go` (capability parity)
- Reference (do NOT modify): `workflow-plugin-azure/internal/statebackend_server.go`, Task 6 (the aws plugin did the structurally-identical work — same store, backend name `spaces`).

**Context:** Mirror Task 6. `doIaCServer` (`internal/iacserver.go:49`) already embeds the `IaCProvider*` + `ResourceDriver` + `PluginService` Unimplemented servers; add `pb.UnimplementedIaCStateBackendServer`. Lazy construction via `Configure` (the azure precedent).

**Step 1: Write the failing test** — `internal/statebackend_server_test.go`: `Configure` builds the store; the 6 RPCs round-trip; `ListBackendNames` → `{["spaces"]}`; State-RPC-before-`Configure` → `FailedPrecondition`; compile-guard `var _ pb.IaCStateBackendServer = (*doIaCServer)(nil)`.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-digitalocean && go test ./internal/ -run StateBackend -v` → FAIL.

**Step 3: Implement** — `internal/statebackend_server.go` mirroring Task 6 (`const doStateBackendName = "spaces"`; the lazy `stateBackend` holder; `Configure` → `statebackend.NewSpacesIaCStateStore(...)` → `setStateStore`; the 6 State RPCs; `ListBackendNames` → `["spaces"]`; local converters). Add the embed + `stateBackend` field + compile-guard to `internal/iacserver.go`. `plugin.json`: `capabilities.iacStateBackends: ["spaces"]`.

**Step 4: `minEngineVersion` — verify comparison semantics first, then set** — before setting `minEngineVersion`, determine how `wfctl` / the engine compares it: `grep -rn 'minEngineVersion\|MinEngineVersion' /Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction --include='*.go'`. If it is a semver `>=` comparison, a Go pseudo-version parses as a `v0.51.x` pre-release and may compare **lower** than the current `"0.51.7"` — in that case **leave `minEngineVersion` at `"0.51.7"`** (the go.mod pin + the migration doc are the real guards) and note this in the PR description. If it is an exact-string or pseudo-version-aware comparison, set it to the PR-1 pseudo-version. Do not guess — the grep result decides.

**Step 5: Capability parity + tests** — extend `host_conformance_test.go` (assert `iacStateBackends` ↔ `ListBackendNames`). Run: `cd /Users/jon/workspace/workflow-plugin-digitalocean && go build ./... && go test ./...` → PASS.

**Step 6: Commit**
```bash
git add internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go internal/host_conformance_test.go plugin.json go.mod go.sum
git commit -m "feat: serve spaces IaC state backend via pb.IaCStateBackendServer + Configure"
```

---

### Task 13: workflow-plugin-digitalocean — release + migration note

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 5, branch `feat/spaces-iac-state-backend`)

**Files:** Modify `plugin.json` (`version` — **minor** bump `v1.0.13` → `v1.1.0`, the compatibility-break marker); Create `docs/migrations/spaces-state-backend.md`.

**Context:** Phase B's core PR (PR 6) is a **clean break** for `spaces` — it deletes the in-core `spaces` case. After PR 6, `iac.state backend: spaces` requires a DO plugin version serving the `spaces` `IaCStateBackend`. PR 6 is blocked on this release. Per `decisions/0034` autonomous.

**Change class:** Version pin update. **Rollback: the `spaces` clean-break rolls back only as a matched pair with PR 6 (see plan Rollback). The plugin release itself is additive — on a defect cut a patch.**

**Step 1:** Bump `plugin.json` `version` → `1.1.0`. Migration note: `iac.state backend: spaces` now requires `workflow-plugin-digitalocean >= v1.1.0` loaded; the yaml `backend: spaces` value is unchanged; the in-core `spaces` backend is removed as of `workflow` <post-PR-6>.

**Step 2:** Commit on the PR 5 branch:
```bash
git add plugin.json docs/migrations/spaces-state-backend.md
git commit -m "chore: release workflow-plugin-digitalocean v1.1.0 — spaces IaC state backend"
```

**Step 3:** After PR 5 is merged to the DO plugin default branch, tag from the merged default branch:
```bash
git checkout main && git pull && git tag v1.1.0 && git push origin v1.1.0
```

**Step 4: Verify** — `gh release view v1.1.0 --repo GoCodeAlone/workflow-plugin-digitalocean` shows assets; GoReleaser run `success`.

---

### Task 14: workflow core — delete dead `cloud_account_aws.go`

**Repo:** planning worktree (PR 6, branch `feat/cloud-sdk-bcd-p6-core-aws`) — `GOWORK=off`.

**Files:** Delete `module/cloud_account_aws.go`.

**Context:** `cloud_account_aws.go` holds `AWSConfigProvider` + `CloudAccount.AWSConfig()` + `CloudAccount.ValidateCredentials()` — all pure `aws-sdk-go-v2`, verified dead code (`awsProviderFrom` and every consumer removed by #653).

**Step 1: Verify zero non-test consumers**

Run: `cd /Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction && grep -rn 'AWSConfigProvider\|\.AWSConfig(\|\.ValidateCredentials(' --include='*.go' . | grep -v '_test.go' | grep -v 'cloud_account_aws.go'`
Expected: **no output**. If any line prints, STOP — the dead-code premise is wrong; surface to the user.

**Step 2: Delete + build**
```bash
git rm module/cloud_account_aws.go
GOWORK=off go build ./...
```
Expected: succeeds. Delete any `_test.go` bodies that referenced the deleted symbols.

**Step 3: Test** — `GOWORK=off go test ./module/...` → PASS.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete dead cloud_account_aws.go (zero consumers, removed by #653)"
```

---

### Task 15: workflow core — rewrite the SDK-bearing AWS credential resolvers

**Repo:** planning worktree (PR 6, branch `feat/cloud-sdk-bcd-p6-core-aws`) — `GOWORK=off`.

**Files:**
- Modify: `module/cloud_account_aws_creds.go`, `module/cloud_account_aws_creds_test.go`

**Context:** `awsStaticResolver`/`awsEnvResolver` are already SDK-free. `awsProfileResolver`/`awsRoleARNResolver` carry the only `aws-sdk-go-v2` imports in this file. The model: every in-core resolver *declares, doesn't resolve* — `profile`/`role_arn` record the declared inputs + an `Extra["credential_source"]` marker; the aws plugin's `awscreds.BuildAWSConfig` (Task 7) performs the SDK-bearing resolution. **Adversarial-review finding (gap window):** a deployment running this rewritten core against a pre-Task-7 aws plugin would emit markers the old plugin ignores, silently producing empty credentials. Mitigation: the rewritten resolvers **log a warning** when emitting a marker, so a mixed-version deployment gets a diagnostic instead of silent failure; Task 18's migration doc documents the coordinated-upgrade requirement.

**Step 1: Update the tests first** — in `cloud_account_aws_creds_test.go`, change the `awsProfileResolver`/`awsRoleARNResolver` assertions: they no longer populate `AccessKey`/`SecretKey`; they record `Extra["profile"]` / `RoleARN` + `Extra["external_id"]` + `Extra["credential_source"]`. Keep the `awsRoleARNResolver` `roleARN == ""` → `fmt.Errorf` required-check assertion. Assert a warning is logged when a marker is emitted.

**Step 2: Run tests to verify they fail** — `GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v` → FAIL (updated tests expect markers; old code SDK-resolves).

**Step 3: Rewrite the two resolver bodies**

`awsProfileResolver.Resolve` — keep everything through `m.creds.Extra["profile"] = profile`, then:
```go
	m.creds.Extra["credential_source"] = "profile"
	// Resolution is deferred to the aws plugin (decisions/0036 / cloud-sdk-extraction).
	// A pre-extraction aws plugin will not honor this marker — log so mixed-version
	// deployments get a diagnostic instead of silent empty credentials.
	logCredentialSourceMarker("aws", "profile")
	return nil
}
```
(delete the `ctx`/`config.LoadDefaultConfig`/`Retrieve`/key-assignment tail.)

`awsRoleARNResolver.Resolve` — keep the `credsMap` nil-check, the `roleARN`/`externalID` extraction, the `RoleARN` + `Extra["external_id"]` records, the `roleARN == ""` required-check; then:
```go
	m.creds.Extra["credential_source"] = "role_arn"
	logCredentialSourceMarker("aws", "role_arn")
	return nil
}
```
(delete the `sessionName` extraction and the entire SDK block.) Add a small `logCredentialSourceMarker(provider, source string)` helper. The resolver call-site is a pure resolver with no module-scoped logger, so a stdlib `log.Printf` is the intentional pragmatic choice here — add a `// TODO: plumb a structured logger when the resolver gains module context` comment so it isn't mistaken for an oversight. Update the import block to `"fmt"` + `"log"` + `"os"` (drop `context`, `aws`, `config`, `credentials`, `sts`).

**Step 4: Run tests to verify they pass** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v` → PASS.

**Step 5: Commit**
```bash
git add module/cloud_account_aws_creds.go module/cloud_account_aws_creds_test.go
git commit -m "refactor: AWS profile/role_arn resolvers declare credential_source marker + warn, no SDK"
```

---

### Task 16: workflow core — delete `iac_state_spaces.go` + strip the `spaces` case

**Repo:** planning worktree (PR 6, branch `feat/cloud-sdk-bcd-p6-core-aws`) — `GOWORK=off`.

**Files:**
- Delete: `module/iac_state_spaces.go`, `module/iac_state_spaces_test.go`
- Modify: `module/iac_module.go` (remove `case "spaces":` from the `Init()` switch)

**Context:** `iac_state_spaces.go` is now plugin-served — aws plugin `s3` (Task 6), DO plugin `spaces` (Task 12). **There is NO `case "s3":` in `iac_module.go`** — the current switch is `memory`/`filesystem`/`spaces`/`gcs`/`postgres`; `backend: s3` has only ever routed through the Phase-A `default:` plugin-registry arm. So this task removes **only** `case "spaces":`. After it merges, `backend: spaces` (and `backend: s3`) require the respective plugin loaded — a **clean break** for `spaces`.

**Step 1: Delete the store + remove the case** — `git rm module/iac_state_spaces.go module/iac_state_spaces_test.go`. In `module/iac_module.go`: delete the entire `case "spaces":` block. Update the `default:`-arm error message — its in-core-backends list currently reads `'memory', 'filesystem', 'spaces', 'gcs', 'postgres'` → drop `'spaces'` (leave `'gcs'`; PR 10 removes it). Do **not** add or reference an `s3` case — none exists.

**Step 2: Build** — `GOWORK=off go build ./...`. Verify nothing else referenced the store: `grep -rn 'NewSpacesIaCStateStore\|SpacesIaCStateStore' --include='*.go' .` → expected no output.

**Step 3: Test** — `GOWORK=off go test ./module/ -run 'IaCModule|IaCState' -v` → PASS (the `default:`-arm plugin-registry dispatch test from Phase A + Task 2's `Configure` test cover the `spaces` path now).

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core spaces IaC state store — now plugin-served (clean break)"
```

---

### Task 17: workflow core — delete `s3_storage.go` + `pipeline_step_s3_upload.go` + drop built-in registrations

**Repo:** planning worktree (PR 6, branch `feat/cloud-sdk-bcd-p6-core-aws`) — `GOWORK=off`.

**Files:**
- Delete: `module/s3_storage.go`, `module/pipeline_step_s3_upload.go` (+ their `_test.go` if present)
- Modify: `plugins/storage/plugin.go` (drop the `"storage.s3"` factory `:89`, the capability entry `:37`, the schema `:326`)
- Modify: `plugins/pipelinesteps/plugin.go` (drop the `"step.s3_upload"` factory `:183`, the capability entry `:93`)
- Modify: `DOCUMENTATION.md` (remove `storage.s3` / `step.s3_upload` from the module/step tables)

**Context:** `storage.s3` + `step.s3_upload` are now plugin-native in `workflow-plugin-aws` (Tasks 8/9). The built-in engine plugins under `plugins/` import `module.*` directly — extracting each drops its factory-map entry + the impl file. `storage.local`/`storage.gcs`/etc. untouched (`gcs` goes in PR 10).

**Step 1: Delete + remove registrations** — `git rm module/s3_storage.go module/pipeline_step_s3_upload.go` (+ test files if present). Edit `plugins/storage/plugin.go` and `plugins/pipelinesteps/plugin.go` per the line refs. Update `DOCUMENTATION.md`.

**Step 2: Build** — `GOWORK=off go build ./...`. Verify: `grep -rn 'NewS3Storage\|NewS3UploadStepFactory\|S3Storage\|S3UploadStep' --include='*.go' .` → expected no output.

**Step 3: Test** — `GOWORK=off go test ./plugins/storage/... ./plugins/pipelinesteps/... ./module/...` → PASS.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core storage.s3 + step.s3_upload — now plugin-native"
```

---

### Task 18: workflow core — `go mod tidy` + `.phase-b-complete` marker + Phase B migration doc + image-launch validation

**Repo:** planning worktree (PR 6, branch `feat/cloud-sdk-bcd-p6-core-aws`) — `GOWORK=off`.

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `.phase-b-complete` (tracked marker — consumed by `scripts/audit-cloud-symbols.sh --check`)
- Create: `docs/migrations/2026-05-14-cloud-sdk-extraction.md`

**Context:** After Tasks 14–17, `module/` no longer imports `aws-sdk-go-v2` for the IaC-state / standalone-S3 surface. `aws-sdk-go-v2` **stays** in `go.mod` (`provider/aws/`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go` still import it — out of scope). `go mod tidy` drops only the now-unused service modules. The `.phase-b-complete` marker arms the audit script's `cloud_account_aws_creds.go` zero-`aws-sdk-go-v2` invariant.

**Change class:** Build pipeline + go.mod dependency change → runtime-launch-validation required. **Rollback: revert PR 6; deleted files recoverable from git, the in-core `spaces`/`storage.s3`/`step.s3_upload` paths + SDK-bearing resolvers restore, `go.mod` re-tidies. The `spaces` clean-break rolls back only as a matched pair with the DO plugin `v1.1.0` release (see Rollback section).**

**Step 1: Tidy + marker** — `GOWORK=off go mod tidy && touch .phase-b-complete`.

**Step 2: Audit script enforcing mode** — `bash scripts/audit-cloud-symbols.sh --check` → expected `audit-cloud-symbols: OK` (with `.phase-b-complete` present, asserts `cloud_account_aws_creds.go` has 0 `aws-sdk-go-v2` imports). FAIL → Task 15 incomplete.

**Step 3: Build + full test + image-launch validation** — `GOWORK=off go build ./... && GOWORK=off go test ./...`; then runtime-launch-validation: build + launch the server against a representative `iac.state` config; confirm clean startup (plugin-backed `backend: s3|spaces` either dispatch to the registry or fail with the clean "install the plugin" error — that is correct clean-break behavior); capture the transcript. Expected: all green; exit 0.

**Step 4: Write the migration doc** — `docs/migrations/2026-05-14-cloud-sdk-extraction.md`, Phase B section:
- `iac.state backend: spaces` → load `workflow-plugin-digitalocean >= v1.1.0`. **Clean break** — the in-core backend is removed.
- `iac.state backend: s3` → load `workflow-plugin-aws >= <release>`. (`s3` was never a first-class in-core backend; this is *new* first-class plugin support.)
- `storage.s3` / `step.s3_upload` → load `workflow-plugin-aws`; `credentials:` moves inline (or `credentials_ref:` an `aws.credentials` module).
- **`provider: aws` with `credentialType: profile` or `role_arn`** — credential resolution is now performed in-plugin. **Core and `workflow-plugin-aws` must be upgraded together**: a new core against a pre-extraction aws plugin will emit a `credential_source` marker the old plugin ignores, producing empty credentials (core logs a warning). State this prominently.
- **`azure_blob` backend** — upgrade `workflow-plugin-azure` to `v1.1.1` simultaneously with any `workflow` core upgrade that includes PR 1 (the `Configure` RPC). A post-PR-1 engine calls `IaCStateBackend.Configure` during `IaCModule.Init()`; `workflow-plugin-azure v1.1.0` returns `Unimplemented` → loud startup failure. (`v1.1.1` closes a real Phase-A gap — `azure_blob` was non-functional end-to-end before it.)
- yaml `backend:`/`provider:`/step-type names unchanged.

**Step 5: Commit**
```bash
git add go.mod go.sum .phase-b-complete docs/migrations/2026-05-14-cloud-sdk-extraction.md
git commit -m "build: drop unused aws-sdk-go-v2 IaC modules + arm Phase B audit invariant"
```

---

### Task 19: workflow core — `kubernetesBackend` interface-audit spike → ADR 0037

**Repo:** planning worktree (PR 7, branch `feat/cloud-sdk-bcd-p7-gke-spike`) — docs/decisions only, no Go code.

**Files:** Create `decisions/0037-gke-cross-process-contract.md`.

**Context:** Phase C extracts the one SDK-bearing `platform.*` backend — `gkeBackend` (`module/platform_kubernetes_gke.go`, `google.golang.org/api/container/v1`). The cross-process contract for `gke` is **gated on this spike** (design Architecture §2). The in-core `kubernetesBackend` interface (`module/platform_kubernetes.go:44-49`) is 4 methods: `plan(k) (*PlatformPlan, error)`, `apply(k) (*PlatformResult, error)`, `status(k) (*KubernetesClusterState, error)`, `destroy(k) error`. Options, in the design's preference order:
1. **Fold `gke` into the existing `ResourceDriver` contract** (`iac.proto:78-88`, 9 RPCs). A GKE cluster is a managed resource — `plan`→`Diff`, `apply`→`Create`/`Update`, `status`→`Read`, `destroy`→`Delete`. *Preferred* — zero new proto surface. **Strong prior signal:** `workflow-plugin-gcp/provider/drivers/real_clients.go` already imports `cloud.google.com/go/container` — the gcp plugin's `ResourceDriver` very likely already catalogs a GKE / `infra.k8s_cluster` resource type (the DO plugin declares `infra.k8s_cluster`).
2. **Plugin-native `kubernetesBackend`** via the `ModuleFactories`/`RemoteModule` SDK — only if `ResourceDriver`'s lifecycle shape doesn't fit.
3. **A minimal new `PlatformBackend` proto service** — fallback only.

**Step 1: Audit the in-core interface** — read `module/platform_kubernetes.go`, `module/platform_kubernetes_gke.go` (the `gkeBackend` 4 methods + `containerService`), `module/platform_provider.go` (`PlatformPlan`/`PlatformResult`), `module/platform_kubernetes.go:11` (`KubernetesClusterState`), and `plugin/external/proto/iac.proto` (`ResourceDriver` + its messages). Map each `kubernetesBackend` method onto a `ResourceDriver` RPC; note any shape mismatch (`status` returns the rich typed `KubernetesClusterState` — does `ResourceReadResponse.outputs_json` carry it cleanly? — and confirm `gke` has no continuous-reconciliation behavior: the 4 methods are one-shot lifecycle).

**Step 2: Investigate the gcp plugin's existing GKE coverage** — in `/Users/jon/workspace/workflow-plugin-gcp`: `grep -rn 'container\|gke\|k8s\|kubernetes' provider/ --include='*.go'`; read `provider/drivers/real_clients.go` + the `ResourceDriver` registration. Determine whether a GKE-cluster resource driver **already exists**.

**Step 3: Write ADR 0037** — `decisions/0037-gke-cross-process-contract.md` in the Nygard format. **Context:** the spike premise + the 4-method interface + the 3 options. **Decision:** the chosen contract + one sentence per rejected option + whether the gcp plugin already covers GKE. **Consequences:** what Task 22 (gcp plugin) and Tasks 25/26 (core wiring) must implement; the proto-surface cost (zero if Option 1; if Option 3, note that PR 9's proto regen becomes a serial prerequisite to PR 8 Task 22). Cite the design + this plan. Confirm `0037` is the next free number: `ls decisions/ | grep -E '^[0-9]{4}-' | sort | tail -1`.

**Step 4: Verify (documentation class)** — render-preview the ADR; confirm no broken cross-references; confirm it picks exactly one option with reasoning.

**Step 5: Commit**
```bash
git add decisions/0037-gke-cross-process-contract.md
git commit -m "docs: ADR 0037 — gke cross-process contract (kubernetesBackend interface audit)"
```

---

### Task 20: workflow-plugin-gcp — port the GCS state store

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 8, branch `feat/gcs-gke-storage`)

**Files:**
- Create: `internal/statebackend/gcs.go`, `internal/statebackend/gcs_test.go`
- Reference (do NOT modify): `workflow` `module/iac_state_gcs.go` (`GCSIaCStateStore` + `NewGCSIaCStateStore`/`NewGCSIaCStateStoreWithClient`, `GCSObjectClient`), the proto `IaCState` message (`iac.proto:636`), Tasks 5/11 (the structurally-identical aws+DO ports).

**Context:** `module/iac_state_gcs.go`'s `GCSIaCStateStore` uses `cloud.google.com/go/storage` + `google.golang.org/api/{iterator,option}`. Port it into the gcp plugin, serve as `gcs`.

**Step 1: Write the failing test** — port `module/iac_state_gcs_test.go` → `internal/statebackend/gcs_test.go` (`package statebackend`, local `IaCState` matching the proto message + `IaCStateStore`, exercise `NewGCSIaCStateStoreWithClient` + the 6 ctx-ful methods).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/statebackend/ -v` → FAIL `undefined`.

**Step 3: Implement** — copy `module/iac_state_gcs.go` → `internal/statebackend/gcs.go` (`package statebackend`, local `IaCState` + `IaCStateStore`, keep the `GCSObjectClient` indirection + `gcsRealClient`, do NOT import `workflow/module`). `go mod tidy`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/statebackend/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/statebackend/ go.mod go.sum
git commit -m "feat: port GCS IaC state store into gcp plugin"
```

---

### Task 21: workflow-plugin-gcp — serve `gcs` via `pb.IaCStateBackendServer` (incl. `Configure`)

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 8, branch `feat/gcs-gke-storage`)

**Files:**
- Create: `internal/statebackend_server.go`, `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `gcpIaCServer`; compile-guard)
- Modify: `plugin.json` (`capabilities.iacStateBackends: ["gcs"]`)
- Modify: `go.mod`/`go.sum` (pin workflow to PR 1's merge commit)
- Modify/extend: `internal/host_conformance_test.go` (capability parity)
- Reference (do NOT modify): `workflow-plugin-azure/internal/statebackend_server.go`, Task 6.

**Context:** Mirror Task 6. `gcpIaCServer` (`internal/iacserver.go:36`) already embeds the `IaCProvider*` + `ResourceDriver` Unimplemented servers; add `pb.UnimplementedIaCStateBackendServer`. Lazy construction via `Configure`.

**Step 1: Write the failing test** — `internal/statebackend_server_test.go`: `Configure` builds the store; 6 RPCs round-trip; `ListBackendNames` → `{["gcs"]}`; State-before-`Configure` → `FailedPrecondition`; compile-guard `var _ pb.IaCStateBackendServer = (*gcpIaCServer)(nil)`.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/ -run StateBackend -v` → FAIL.

**Step 3: Implement** — `internal/statebackend_server.go` mirroring Task 6 (`const gcpStateBackendName = "gcs"`; lazy `stateBackend` holder; `Configure` → `statebackend.NewGCSIaCStateStore(...)` → `setStateStore`; the 6 State RPCs; `ListBackendNames` → `["gcs"]`; local converters). Add the embed + field + compile-guard to `internal/iacserver.go`. `plugin.json`: `iacStateBackends: ["gcs"]`.

**Step 4: Capability parity + tests** — extend `host_conformance_test.go`. Run: `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go internal/host_conformance_test.go plugin.json go.mod go.sum
git commit -m "feat: serve gcs IaC state backend via pb.IaCStateBackendServer + Configure"
```

---

### Task 22: workflow-plugin-gcp — `gke` cross-process contract implementation (per ADR 0037)

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 8, branch `feat/gcs-gke-storage`)

**Files:** Modify/Create per ADR 0037's decision (see below). Reference (do NOT modify): `decisions/0037-gke-cross-process-contract.md` (**read first — it fixes this task's shape**), `workflow` `module/platform_kubernetes_gke.go`.

**Context:** This task's exact shape is **determined by ADR 0037** (Task 19). The implementer MUST read it first. **If ADR 0037 picked Option 3** (a new `PlatformBackend` proto service), this task depends on PR 9's proto regen being merged first — see the manifest dependency note. The three shapes:
- **Option 1 (ResourceDriver fold):** if the gcp plugin's `ResourceDriver` already catalogs a GKE / `infra.k8s_cluster` resource type → **verification + gap-fill**: confirm it covers create/read/update/diff/delete matching the in-core `gkeBackend` behavior; add missing field coverage; ensure `plugin.json` `capabilities.iacProvider.resourceTypes` lists the GKE type. If no such driver exists → port `module/platform_kubernetes_gke.go`'s `gkeBackend` logic into a new GKE `ResourceDriver` driver under `provider/drivers/`.
- **Option 2:** a plugin-native `kubernetesBackend` module via `ModuleFactories`/`RemoteModule`.
- **Option 3:** implement the new `PlatformBackend` server (the proto addition is PR 9's Task 25).

**Step 1: Read ADR 0037.** Identify the chosen option + its "what Task 22 must implement".

**Step 2: Write the failing test** — a GKE lifecycle test (create → status/read → destroy/delete) through whichever contract ADR 0037 picked, against a fake GKE container client.

**Step 3: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./... -run GKE -v` → FAIL.

**Step 4: Implement** per ADR 0037 — port the `gkeBackend` SDK logic (`containerService`, `Projects.Locations.Clusters.{Create,Get,Delete}`) from `module/platform_kubernetes_gke.go`. Credentials arrive as a serialized `CloudCredentials` — resolve `ServiceAccountJSON` in-plugin exactly as the in-core `containerService` did.

**Step 5: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 6: Commit**
```bash
git add internal/ provider/ plugin.json
git commit -m "feat: gke cross-process contract per ADR 0037"
```

---

### Task 23: workflow-plugin-gcp — plugin-native `storage.gcs` + `gcp.credentials` DRY module + release

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 8, branch `feat/gcs-gke-storage`)

**Files:**
- Create: `internal/gcpcreds/gcpcreds.go`, `internal/gcpcreds/gcpcreds_test.go`
- Create: `internal/modules/storage_gcs.go`, `internal/modules/storage_gcs_test.go`, `internal/modules/gcp_credentials.go`, `internal/modules/gcp_credentials_test.go`, `internal/modules/credref/registry.go`
- Modify: the gcp plugin's module-factory registration; `plugin.json` (`moduleTypes` += `"storage.gcs"`, `"gcp.credentials"`; `version` minor bump); `CHANGELOG.md`
- Modify/extend: `internal/host_conformance_test.go`
- Reference (do NOT modify): `workflow` `module/storage_gcs.go`, `workflow` `plugins/storage/plugin.go:109`, Task 8 (the aws plugin's structurally-identical `credref`/`storage`/`*.credentials`).

**Context:** `storage.gcs` becomes plugin-native, mirroring Task 8. The gcp credential resolvers (`module/cloud_account_gcp.go`) are already SDK-free, so `gcpcreds.BuildGCPOptions` builds `[]option.ClientOption` from an inline `credentials:` block (`ServiceAccountJSON` → `option.WithCredentialsJSON`) with an ADC fallback. `gcp.credentials` + the `credref` registry mirror Task 8 exactly. **This task also cuts the gcp plugin release** (PR 10 is blocked on it).

**Step 1: Write the failing tests** — `gcpcreds_test.go`: `BuildGCPOptions` with inline service-account JSON; with empty input (ADC fallback). `storage_gcs_test.go`: factory from a config with `credentials:`/`credentials_ref:`. `gcp_credentials_test.go`: the module registers in `credref` by name; duplicate → error.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/gcpcreds/ ./internal/modules/... -v` → FAIL.

**Step 3: Implement** — `internal/gcpcreds/gcpcreds.go` (`BuildGCPOptions`); `internal/modules/credref/registry.go` (mirror Task 8); port `module/storage_gcs.go` → `internal/modules/storage_gcs.go`; `internal/modules/gcp_credentials.go` (the `gcp.credentials` DRY module). Register `"storage.gcs"` + `"gcp.credentials"` in `ModuleFactories`; extend `host_conformance_test.go` (capability parity for everything PR 8 added); update `plugin.json` (`moduleTypes` + a minor `version` bump); CHANGELOG entry naming `gcs` + the `gke` contract + `storage.gcs`/`gcp.credentials`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS.

**Step 5: Commit + release**
```bash
git add internal/ plugin.json CHANGELOG.md
git commit -m "feat: plugin-native storage.gcs + gcp.credentials DRY module; release prep"
```
After PR 8 is merged to the gcp plugin default branch, tag from the merged default branch:
```bash
git checkout main && git pull && git tag v<version> && git push origin v<version>
```

**Step 6: Verify release** — `gh release view v<version> --repo GoCodeAlone/workflow-plugin-gcp` shows assets; GoReleaser run `success`.

---

### Task 24: workflow-plugin-gcp — capability parity audit (final)

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 8, branch `feat/gcs-gke-storage`)

**Files:** Modify `plugin.json` (final cross-check); `internal/host_conformance_test.go`.

**Context:** Final guard against `plugin.json` declaring a capability the plugin doesn't serve (or vice versa), across everything PR 8 added — `gcs` backend, `storage.gcs`, `gcp.credentials`, and the GKE contract surface per ADR 0037.

**Step 1: Run the parity test** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/ -run Conformance -v`. If drift, reconcile `plugin.json` ↔ registrations.

**Step 2: Full test** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./...` → PASS.

**Step 3: Commit** (only if changes were needed)
```bash
git add plugin.json internal/host_conformance_test.go
git commit -m "test: final plugin.json capability ↔ registration parity"
```

---

### Task 25: workflow core — `gke` cross-process contract proto/adapter (per ADR 0037)

**Repo:** planning worktree (PR 9, branch `feat/cloud-sdk-bcd-p9-gke-wiring`) — `GOWORK=off`.

**Files:**
- Create: `module/platform_kubernetes_grpc.go`, `module/platform_kubernetes_grpc_test.go`
- Modify (only if ADR 0037 picked Option 3): `plugin/external/proto/iac.proto` + regenerate
- Reference (do NOT modify): `decisions/0037-gke-cross-process-contract.md` (**read first**), `module/platform_kubernetes.go` (`kubernetesBackend` interface), `module/iac_state_grpc_client.go` (the Phase A `grpcIaCStateStore` adapter — the precedent).

**Context:** The host-side adapter that lets `platform.kubernetes`'s in-core `kubernetesBackend` interface dispatch the `gke` provider to a plugin gRPC client. Shape per ADR 0037:
- **Option 1 (ResourceDriver fold):** `grpcKubernetesBackend` implements `kubernetesBackend`, delegating `plan`→`Diff`, `apply`→`Create`/`Update`, `status`→`Read`, `destroy`→`Delete` on a `pb.ResourceDriverClient`. JSON-bytes converters (`PlatformPlan`/`PlatformResult`/`KubernetesClusterState` ↔ the `ResourceDriver` messages), mirroring `iac_state_grpc_client.go`. **No proto change.**
- **Option 2:** the `RemoteModule` adapter for a plugin-native `kubernetesBackend`.
- **Option 3:** add the minimal `PlatformBackend` service to `iac.proto` (regenerate — additive, preserves the no-`structpb` invariant) + the `grpcKubernetesBackend` adapter over it. **If Option 3, this task's proto regen must merge before PR 8's Task 22.**

**Step 1: Read ADR 0037.** Pin the contract.

**Step 2: Write the failing test** — `platform_kubernetes_grpc_test.go`: a fake client of the chosen contract; assert `grpcKubernetesBackend.{plan,apply,status,destroy}` round-trip (incl. `KubernetesClusterState` surviving the JSON-bytes round-trip).

**Step 3: Verify it fails** — `GOWORK=off go test ./module/ -run GRPCKubernetesBackend -v` → FAIL `undefined`.

**Step 4: Implement** `module/platform_kubernetes_grpc.go` — `grpcKubernetesBackend` + converters (+ proto regen for Option 3 only).

**Step 5: Verify it passes** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ -run GRPCKubernetesBackend -v` → PASS.

**Step 6: Commit**
```bash
git add module/platform_kubernetes_grpc.go module/platform_kubernetes_grpc_test.go plugin/external/proto/
git commit -m "feat: grpcKubernetesBackend adapter for plugin-served gke (per ADR 0037)"
```

---

### Task 26: workflow core — engine seam + registry for plugin-served kubernetes backends

**Repo:** planning worktree (PR 9, branch `feat/cloud-sdk-bcd-p9-gke-wiring`) — `GOWORK=off`.

**Files:**
- Create: `module/platform_kubernetes_plugin_registry.go`, `module/platform_kubernetes_plugin_registry_test.go`
- Modify: `module/platform_kubernetes.go` (backend resolution consults the registry for non-core providers)
- Modify: `engine.go` (`loadPluginInternal` populates the registry — mirrors the Phase A `IaCStateBackendProvider` seam)
- Modify: `plugin/` — a `KubernetesBackendProvider` optional interface + `ExternalPluginAdapter` accessor
- Reference (do NOT modify): `module/iac_state_plugin_registry.go`, `plugin/iac_state_backend_provider.go`, the `engine.go` `IaCStateBackendProvider` block (`engine.go:331-339`) — **the exact Phase A precedent for every piece.**

**Context:** Structurally-identical to Phase A's `iac.state` plugin-backend wiring, for **kubernetes backends**: a `kubernetesBackendClientRegistry` (`gke` → contract client), an exported `RegisterKubernetesBackendClient`, a `plugin.KubernetesBackendProvider` optional interface, an `ExternalPluginAdapter` accessor, the `engine.go` `loadPluginInternal` seam. `module/platform_kubernetes.go` resolution: `provider: kind|k3s|eks|aks` use the in-core `kubernetesBackendRegistry` factory map unchanged; any other provider (`gke`) consult the new client registry and wrap the client in Task 25's `grpcKubernetesBackend`.

**Step 1: Write the failing tests** — registry register/resolve (mirror `iac_state_plugin_registry_test.go`); a `platform_kubernetes_test.go` case: `provider: gke` with a registered client → `grpcKubernetesBackend`; with no client → a clean "install workflow-plugin-gcp" error.

**Step 2: Verify they fail** — `GOWORK=off go test ./module/ ./plugin/... -run 'KubernetesBackend|PlatformKubernetes' -v` → FAIL.

**Step 3: Implement** — the registry + exported register fn; the `plugin.KubernetesBackendProvider` interface + adapter accessor; the `engine.go` seam (copy the `IaCStateBackendProvider` block's structure); the `platform_kubernetes.go` resolution branch.

**Step 4: Verify they pass** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ ./plugin/... ./... -run 'KubernetesBackend|PlatformKubernetes|Engine' -v` → PASS.

**Step 5: Commit**
```bash
git add module/platform_kubernetes_plugin_registry.go module/platform_kubernetes_plugin_registry_test.go module/platform_kubernetes.go engine.go plugin/
git commit -m "feat: engine seam + registry for plugin-served kubernetes backends"
```

---

### Task 27: workflow core — delete GCS files + strip the `gcs` case

**Repo:** planning worktree (PR 10, branch `feat/cloud-sdk-bcd-p10-core-gcp`) — `GOWORK=off`.

**Files:**
- Delete: `module/iac_state_gcs.go`, `module/storage_gcs.go`, `module/platform_kubernetes_gke.go` (+ their `_test.go` if present)
- Modify: `module/iac_module.go` (remove `case "gcs":`)
- Modify: `plugins/storage/plugin.go` (drop the `"storage.gcs"` factory `:109`, capability `:39`, schema `:352`)
- Modify: `DOCUMENTATION.md` (remove `storage.gcs`)

**Context:** Depends on **PR 8** (gcp plugin release) + **PR 9** (gke wiring merged). `iac_state_gcs.go` (`gcs` backend) → gcp plugin; `storage_gcs.go` → plugin-native; `platform_kubernetes_gke.go` (`gkeBackend` + its `gke` `init()` registration) → its `gke` dispatch now flows through PR 9's `kubernetesBackendClientRegistry` + `grpcKubernetesBackend`.

**Step 1: Delete + strip** — `git rm module/iac_state_gcs.go module/storage_gcs.go module/platform_kubernetes_gke.go` (+ test files if present). In `module/iac_module.go`: delete `case "gcs":`; the `default:`-arm error message in-core list becomes `'memory', 'filesystem', 'postgres'`. In `plugins/storage/plugin.go`: remove the `storage.gcs` factory + capability + schema. Update `DOCUMENTATION.md`.

**Step 2: Build** — `GOWORK=off go build ./...`. Verify: `grep -rn 'NewGCSIaCStateStore\|NewGCSStorage\|gkeBackend\|GCSIaCStateStore\|GCSStorage' --include='*.go' .` → expected no output (the audit script's `init()` partition check also guards the `platform_kubernetes_gke.go` removal).

**Step 3: Test** — `GOWORK=off go test ./module/... ./plugins/storage/...` → PASS (`provider: gke` covered by PR 9's registry test; `backend: gcs` flows through the `default:` arm + Task 2's `Configure`).

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core gcs store + storage.gcs + gkeBackend — now plugin-served"
```

---

### Task 28: workflow core — drop GCP SDKs from go.mod + permanent CI gate

**Repo:** planning worktree (PR 10, branch `feat/cloud-sdk-bcd-p10-core-gcp`) — `GOWORK=off`.

**Files:**
- Modify: `go.mod`, `go.sum`, `scripts/audit-cloud-symbols.sh`, `.github/workflows/ci.yml`
- Create: `.phase-c-complete` (tracked marker)

**Context:** After Task 27, `cloud.google.com/go/storage` + `google.golang.org/api/*` have **zero** importers in core's build graph — `go mod tidy` drops them entirely. The permanent CI gate is **asymmetric** (design Goals): (a) `go list -deps ./...` asserts **zero** `Azure/azure-sdk-for-go` AND **zero** `cloud.google.com/go` / `google.golang.org/api` packages in core's build graph; (b) `audit-cloud-symbols.sh --check` asserts **zero** `aws-sdk-go-v2` imports under `module/` — AWS gone from `module/`, but `aws-sdk-go-v2` *remains* a `go.mod` entry for the out-of-scope `provider/aws/` etc. surface. `godo` remains — not asserted.

**Change class:** Build pipeline + go.mod dependency change → runtime-launch-validation required. **Rollback: revert PR 10; deleted files recoverable from git, the in-core `gcs`/`storage.gcs`/`gke` paths restore, `go.mod` re-adds the GCP SDKs on `go mod tidy`. Note: a running deployment that already cut over to plugin-served `gcs` must, on rollback, either also roll back the gcp plugin to a pre-`gcs` version OR keep the gcs-serving plugin installed (the reverted engine routes `backend: gcs` to the in-core case, so the in-core path must be the one in use) — coordinate engine + plugin versions.**

**Step 1: Tidy + marker** — `GOWORK=off go mod tidy && touch .phase-c-complete`. Confirm `go.mod` no longer lists `cloud.google.com/go/storage` or `google.golang.org/api`.

**Step 2: Add the permanent invariants** — in `scripts/audit-cloud-symbols.sh`, add a `--check` block: `GOWORK=off go list -deps ./... 2>/dev/null | grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'` must be **empty** (FAIL if any line). Add a `module/`-scoped `aws-sdk-go-v2` zero-import assertion (the existing whole-repo map already separates `module/` from elsewhere — assert the `module/` count is 0). In `.github/workflows/ci.yml` `cloud-sdk-audit` job, confirm `audit-cloud-symbols.sh --check` runs (wired in Phase 0) and the new graph check executes there.

**Step 3: Build + full test + audit + image-launch validation**
```bash
GOWORK=off go build ./... && GOWORK=off go test ./...
bash scripts/audit-cloud-symbols.sh --check          # expect: audit-cloud-symbols: OK
GOWORK=off go list -deps ./... | grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'  # expect: no output
```
Then runtime-launch-validation: build + launch the server against a representative `iac.state` / `platform.kubernetes` config; confirm clean startup; capture the transcript. Expected: all green; `go list -deps` grep empty; exit 0.

**Step 4: Commit**
```bash
git add go.mod go.sum scripts/audit-cloud-symbols.sh .github/workflows/ci.yml .phase-c-complete
git commit -m "build: drop GCP SDKs from go.mod + permanent asymmetric cloud-SDK CI gate"
```

---

### Task 29: workflow core — Phase C migration doc + final cross-phase verification

**Repo:** planning worktree (PR 10, branch `feat/cloud-sdk-bcd-p10-core-gcp`) — `GOWORK=off`.

**Files:** Modify `docs/migrations/2026-05-14-cloud-sdk-extraction.md` (append Phase C), `DOCUMENTATION.md` (final pass).

**Context:** Final documentation + the cross-phase coherence check.

**Step 1: Write the Phase C migration section** — `iac.state backend: gcs` → load `workflow-plugin-gcp`; `platform.kubernetes provider: gke` → load `workflow-plugin-gcp` (`provider: kind|k3s|eks|aks` unchanged, still core); `storage.gcs` → load `workflow-plugin-gcp`, `credentials:` inline (or `credentials_ref:` a `gcp.credentials` module). yaml `backend:`/`provider:`/module-type names unchanged.

**Step 2: Final verification** — render-preview the migration doc (no broken anchors); `bash scripts/audit-cloud-symbols.sh --check` → `OK` (both `.phase-b-complete` + `.phase-c-complete` present); `GOWORK=off go build ./... && GOWORK=off go test ./...` → green.

**Step 3: Commit**
```bash
git add docs/migrations/2026-05-14-cloud-sdk-extraction.md DOCUMENTATION.md
git commit -m "docs: Phase C migration guide + final cloud-SDK-extraction doc pass"
```

---

## Rollback (whole-plan)

This plan changes **plugin loading paths** and **go.mod dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list. Per-PR rollback:

- **PR 1 (`Configure` RPC)** — the proto RPC is additive; plugins embedding `Unimplemented*` are unaffected. Reverting just stops the host calling `Configure`. Safe before any plugin depends on it being called.
- **PRs 2/3/4/5/8 (plugin PRs)** are additive — reverting is harmless to a core that still has the in-core paths; on a defect prefer a forward patch release over deleting a tag.
- **PR 6 (Phase B core deletion)** — reverting restores the in-core `spaces`/`storage.s3`/`step.s3_upload` paths + SDK-bearing resolvers; `go.mod` re-tidies. The **`spaces` clean-break** is the one external-user-visible compat break — PR 6 + the DO plugin `v1.1.0` release roll back **as a matched pair**.
- **PR 7 (ADR)** — docs only; revert is a doc revert.
- **PR 9 (gke wiring)** — additive; reverting removes the plugin-served `gke` path. Safe only *before* PR 10 deletes the in-core `gkeBackend`; after PR 10, PR 9 + PR 10 revert as a pair.
- **PR 10 (Phase C core deletion)** — reverting restores in-core `gcs`/`storage.gcs`/`gkeBackend` + re-adds the GCP SDKs on `go mod tidy`. A deployment already cut over to plugin-served `gcs` must coordinate engine + plugin versions on rollback (see Task 28's rollback note).
- **Forward-fix preferred:** each core deletion PR removes the old in-process path only *after* the contract dispatch is wired (in the same PR or a merged predecessor) — a broken phase fails at PR CI (image-launch / audit-script gates), not in production.

## Notes for the executor

- **Team sizing:** 29 tasks → 3 implementers (per `subagent-driven-development` sizing).
- **Cross-repo discipline:** every PR-2/3/4/5/8 dispatch prompt MUST name the absolute plugin-repo path and state it is a *different* repo than the worktree (see Cross-repo note). PR 3 → PR 4 are *sequential, same repo* (`workflow-plugin-aws`) — PR 4 branches off PR 3's merged result.
- **`GOWORK=off`** on every Go command in the planning worktree; never in the plugin repos.
- **PR 1 is the gate for all plugin work** — PRs 2/3/4/5/8 cannot pin their `workflow` dependency until PR 1 is merged to `origin/main` (they pin its merge-commit pseudo-version).
- **Dependency gates are real:** PR 6 needs PR 4 + PR 5 tags installable; PR 8 + PR 9 need PR 7 (ADR 0037) merged; PR 10 needs PR 8 tag + PR 9 merged. The scope-lock per-task checkpoint + watchdog cadence apply.
- **ADR 0037 is load-bearing for Tasks 22, 25, 26** — those tasks are contract-parameterized; the implementer reads ADR 0037 first. Option 1 (ResourceDriver fold) is the strongly-expected outcome (the gcp plugin already imports the GKE container SDK), but the spike is authoritative. **If ADR 0037 picks Option 3, PR 9's proto regen is a serial prerequisite to PR 8's Task 22** — adjust the parallel-stream schedule accordingly.
- **The Phase A precedent is the template** — `workflow-plugin-azure` (PR #8 + the PR-2 `Configure` retrofit here) for the plugin side; `module/iac_state_grpc_client.go` + `module/iac_state_plugin_registry.go` + the `engine.go` `IaCStateBackendProvider` seam (`engine.go:331-339`) for the core side. Cite them; don't reinvent.
- **`S3IaCStateStore` (aws) and `SpacesIaCStateStore` (DO) are deliberately diverging copies** of the same upstream `module/iac_state_spaces.go` — they differ only in env-var fallback behavior (aws strips the `DO_SPACES_*` fallbacks; DO keeps them). Any future fix to the shared S3-locking protocol must be applied to both copies. The design's "Alternative 3 — shared `s3compat` module" is the recommended eventual cleanup; out of scope here.
