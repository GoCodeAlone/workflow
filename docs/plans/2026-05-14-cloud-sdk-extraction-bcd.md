# Cloud-SDK Extraction — Phases B/C/D Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the AWS, GCP, and DigitalOcean cloud-SDK surface out of `workflow` core's `module/` package into the existing `workflow-plugin-{aws,gcp,digitalocean}` gRPC sidecar plugins, dropping `Azure/azure-sdk-for-go` (already gone), `cloud.google.com/go/*`, and `google.golang.org/api/*` from core's build graph entirely and `aws-sdk-go-v2` from core's `module/` package.

**Architecture:** Builds directly on Phase A's proven, merged patterns (workflow `origin/main` `d179b1aa`): the strict `IaCStateBackend` gRPC contract (`plugin/external/proto/iac.proto`) + `ListBackendNames` RPC, the SDK serve hook (`registerIaCServicesOnly` auto-registers `pb.IaCStateBackendServer`), the engine host-wiring (`loadPluginInternal` → `plugin.IaCStateBackendProvider` → `module.RegisterIaCStateBackend`), `plugin.PluginManifest.IaCStateBackends`, the ctx-ful `module.IaCStateStore`, and the `azureIaCServer`-style cross-repo plugin pattern (one provider type also implements `pb.IaCStateBackendServer`; `plugin.json capabilities.iacStateBackends` advertises names). Phase B (AWS) + Phase D (DigitalOcean) share one S3-compatible store; Phase C (GCP) adds the one SDK-bearing `platform.*` backend (`gke`) gated on an interface-audit spike.

**Tech Stack:** Go 1.x, gRPC (`go-plugin`), `aws-sdk-go-v2`, `cloud.google.com/go`, `google.golang.org/api`, GoReleaser v2, the superpowers autonomous pipeline.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 8
**Tasks:** 26
**Estimated Lines of Change:** ~2600 (informational; not enforced)

**Out of scope:**
- The out-of-`module/` AWS SDK surface — `provider/aws/{clients,deploy,plugin}.go`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go` — the #653-retained "RBAC/secrets/artifact stay" surface. `aws-sdk-go-v2` therefore remains a `go.mod` entry after Phase B (the CI gate is asymmetric — see Task 25).
- `github.com/digitalocean/godo` extraction — `module/cloud_account_do.go` + `module/platform_do_*.go` — structurally-identical follow-up, per design Non-Goals.
- `aws-sdk-go-v2/service/kinesis` — transitive via `modular`, per design Non-Goals.
- The IaC state at-rest format change (JSON → binary/pb) — design Open Item; needs its own brainstorming pass.
- `workflow-plugin-azure` `minEngineVersion` re-pin — blocked on a tagged `workflow` release; tracked separately, not in this plan.
- The comment-only stubs `module/nosql_dynamodb.go` / `module/storage_artifact_s3.go` — they carry no SDK, stay untouched.
- Changing `wfctl plugin install` discovery flow.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | workflow-plugin-aws: `s3` IaCStateBackend (cross-repo) | Task 1, Task 2 | `workflow-plugin-aws`: `feat/s3-iac-state-backend` |
| 2 | workflow-plugin-aws: `storage.s3` + `step.s3_upload` + in-plugin credentials + release (cross-repo) | Task 3, Task 4, Task 5, Task 6, Task 7 | `workflow-plugin-aws`: `feat/s3-storage-step-credentials` |
| 3 | workflow-plugin-digitalocean: `spaces` IaCStateBackend + release (cross-repo) | Task 8, Task 9, Task 10 | `workflow-plugin-digitalocean`: `feat/spaces-iac-state-backend` |
| 4 | workflow core: Phase B deletion — AWS files + `spaces` case + resolver rewrite + go.mod | Task 11, Task 12, Task 13, Task 14, Task 15 | `workflow`: `feat/cloud-sdk-extraction-bcd-p4-core-aws` |
| 5 | workflow core: `kubernetesBackend` interface-audit spike → ADR 0036 | Task 16 | `workflow`: `feat/cloud-sdk-extraction-bcd-p5-gke-spike` |
| 6 | workflow-plugin-gcp: `gcs` IaCStateBackend + `gke` contract + `storage.gcs` + release (cross-repo) | Task 17, Task 18, Task 19, Task 20, Task 21 | `workflow-plugin-gcp`: `feat/gcs-gke-storage` |
| 7 | workflow core: `gke` cross-process wiring (engine seam + adapter + registry) | Task 22, Task 23 | `workflow`: `feat/cloud-sdk-extraction-bcd-p7-gke-wiring` |
| 8 | workflow core: Phase C deletion — GCP files + `gcs` case + permanent CI gate | Task 24, Task 25, Task 26 | `workflow`: `feat/cloud-sdk-extraction-bcd-p8-core-gcp` |

**Execution order / dependencies:**
- **PR 1 → PR 2** — same repo (`workflow-plugin-aws`), sequential; PR 2's final task cuts the aws plugin release tag.
- **PR 3** — independent (`workflow-plugin-digitalocean`); cuts the DO plugin release tag.
- **PR 4** — depends on **PR 2** (aws tag) **and PR 3** (DO tag): it deletes `iac_state_spaces.go`, the one S3-compatible store backing *both* `s3` (aws) and `spaces` (DO).
- **PR 5** — independent (docs/ADR only); must merge before PR 6 and PR 7.
- **PR 6** — depends on **PR 5** (ADR 0036 fixes the `gke` contract shape); cuts the gcp plugin release tag.
- **PR 7** — depends on **PR 5** (ADR 0036); additive core wiring, no cross-repo release dependency — can merge before PR 6.
- **PR 8** — depends on **PR 6** (gcp tag) **and PR 7** (gke wiring merged).
- Parallel streams: `{PR1→PR2}`, `{PR3}`, `{PR5→PR6}`, `{PR5→PR7}` run largely in parallel; PR 4 joins after PR2+PR3; PR 8 joins after PR6+PR7.
- No PR stacking — every `workflow` PR branches off `origin/main` directly.

**Status:** Draft

---

## Cross-repo note

PRs 1, 2, 3, 6 land in **different git repositories** than the planning worktree. Per `decisions/0034-cross-repo-agent-operation-for-plugin-prs.md` this is **fully autonomous** — implement, push, open PR, AND cut/push the release tag, all following normal review discipline (feature branch → PR → admin-merge → tag; never direct-to-default-branch).

**Every cross-repo task dispatch MUST state, explicitly in the implementer prompt, the absolute path of the repo it works in and that it is a *different* repo than the worktree:**
- `workflow-plugin-aws` → `/Users/jon/workspace/workflow-plugin-aws`
- `workflow-plugin-digitalocean` → `/Users/jon/workspace/workflow-plugin-digitalocean`
- `workflow-plugin-gcp` → `/Users/jon/workspace/workflow-plugin-gcp`
- planning worktree (PRs 4/5/7/8) → `/Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction`

## Environment note (ALL tasks)

The planning worktree sits under a parent `go.work` that does not list it. **Every Go command in `/Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction` must be prefixed `GOWORK=off`** (`GOWORK=off go build ./...`, `GOWORK=off go test ./...`, `GOWORK=off go mod tidy`). IDE "not in workspace" / "undefined" diagnostics in that worktree are that artifact, not real errors — always verify via `GOWORK=off go build`. The plugin repos (`workflow-plugin-*`) are normal checkouts — no `GOWORK=off` needed there.

## Workflow-core pin (ALL plugin tasks — PRs 1/2/3/6)

Phase A's `IaCStateBackend` proto + `ListBackendNames` RPC + SDK serve hook landed in workflow `origin/main` at `d179b1aa`. No tagged `workflow` release carries it yet, so each plugin pins a `main` pseudo-version. In each plugin repo run `go get github.com/GoCodeAlone/workflow@d179b1aa && go mod tidy` to pin the pseudo-version of `d179b1aa` (the same mechanism `workflow-plugin-azure` used for `9d7ca68e`). Re-pinning to a clean release tag is a tracked follow-up, out of scope here.

---

### Task 1: workflow-plugin-aws — port the S3-compatible state store

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 1, branch `feat/s3-iac-state-backend`)

**Files:**
- Create: `internal/statebackend/s3.go`
- Create: `internal/statebackend/s3_test.go`
- Reference (source — workflow core, do NOT modify): `module/iac_state_spaces.go`, `module/iac_state.go` (the `IaCState` struct), `module/iac_state_spaces_test.go`

**Context:** `module/iac_state_spaces.go` holds `SpacesIaCStateStore` — an S3-compatible IaC state store (`aws-sdk-go-v2/service/s3` with `UsePathStyle` + `BaseEndpoint`). It backs *both* the `s3` and `spaces` core backends today. This task ports it into the aws plugin as the `s3` backend. The DO plugin ports the same store independently in Task 8 (`spaces`). This mirrors `workflow-plugin-azure/internal/statebackend/azure_blob.go`.

**Step 1: Create the failing test**

Copy `module/iac_state_spaces_test.go` → `internal/statebackend/s3_test.go`. Change `package module` → `package statebackend`. The store's public API is unchanged from core; the test exercises `NewS3IaCStateStoreWithClient` (renamed from `NewSpacesIaCStateStoreWithClient`) + the 6 ctx-ful methods round-tripping `IaCState`.

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/statebackend/ -v`
Expected: FAIL — `undefined: S3IaCStateStore`

**Step 3: Port the store**

Copy `module/iac_state_spaces.go` → `internal/statebackend/s3.go`. Edits:
- `package module` → `package statebackend`
- Rename the type `SpacesIaCStateStore` → `S3IaCStateStore`; rename `NewSpacesIaCStateStore` → `NewS3IaCStateStore`, `NewSpacesIaCStateStoreWithClient` → `NewS3IaCStateStoreWithClient`, `SpacesS3Client` → `S3Client`. (Generic S3-compatible store; the aws plugin serves it as `s3`.)
- Define a local `IaCState` struct + `IaCStateStore` interface in this package (copy the struct from `module/iac_state.go`; the 6 ctx-ful method signatures from the ctx-widened interface). The plugin owns its own copy — it does NOT import `workflow/module`.
- Keep the constructor's env-var fallbacks (`DO_SPACES_ACCESS_KEY`/`DO_SPACES_SECRET_KEY` stay — they are harmless for the aws case and the DO plugin needs them; do not special-case).
- `go mod tidy` — `aws-sdk-go-v2/service/s3` should already be a direct dep of `workflow-plugin-aws`.

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/statebackend/ -v`
Expected: PASS — all ported tests green.

**Step 5: Commit**

```bash
git add internal/statebackend/s3.go internal/statebackend/s3_test.go go.mod go.sum
git commit -m "feat: port S3-compatible IaC state store into aws plugin"
```

---

### Task 2: workflow-plugin-aws — serve `s3` via `pb.IaCStateBackendServer`

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 1, branch `feat/s3-iac-state-backend`)

**Files:**
- Create: `internal/statebackend_server.go`
- Create: `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `awsIaCServer`; wire `NewIaCServer`)
- Modify: `plugin.json` (add `capabilities.iacStateBackends`)
- Modify: `go.mod` / `go.sum` (pin workflow `d179b1aa` — see "Workflow-core pin" above)
- Reference (do NOT modify): `workflow-plugin-azure/internal/statebackend_server.go`, `workflow-plugin-azure/internal/iacserver.go` — the exact precedent.

**Context:** Mirror `workflow-plugin-azure` PR #8 exactly. `awsIaCServer` (`internal/iacserver.go:36`) already embeds the 7 `IaCProvider*` + `ResourceDriver` Unimplemented servers. Add `pb.UnimplementedIaCStateBackendServer`. The SDK serve hook (`registerIaCServicesOnly`, merged in workflow #673) auto-registers `pb.IaCStateBackendServer` by type-assertion — no `main.go` change needed.

**Step 1: Write the failing test**

`internal/statebackend_server_test.go`: instantiate `NewIaCServer()`, wire a fake `S3Client` via `stateBackend.setStateStore(...)`, call `GetState`/`SaveState`/`ListStates`/`DeleteState`/`Lock`/`Unlock` through the `pb` request/response types, assert round-trip. Add a test asserting `ListBackendNames` returns `{BackendNames: []string{"s3"}}`. Add a compile-time guard test referencing `var _ pb.IaCStateBackendServer = (*awsIaCServer)(nil)`.

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/ -run StateBackend -v`
Expected: FAIL — `awsIaCServer does not implement pb.IaCStateBackendServer`

**Step 3: Implement the state-backend server**

Port `workflow-plugin-azure/internal/statebackend_server.go` structure:
- `const awsStateBackendName = "s3"`
- `type stateBackend struct { mu sync.Mutex; store *statebackend.S3IaCStateStore }` + `resolveStore()` (returns `codes.FailedPrecondition` if unwired) + `setStateStore(...)`.
- On `awsIaCServer`: the 6 RPC methods (`GetState`/`SaveState`/`ListStates`/`DeleteState`/`Lock`/`Unlock`) delegating to `s.stateBackend.resolveStore()`, plus `ListBackendNames` returning `&pb.ListBackendNamesResponse{BackendNames: []string{awsStateBackendName}}`.
- Local `iacStateToPB` / `iacStateFromPB` / `marshalIaCMap` / `unmarshalIaCMap` converters (copy from azure plugin — the plugin owns its serialization; the `bytes outputs_json`/`config_json` JSON-bytes shape is the `iac.proto` hard invariant).
- `internal/iacserver.go`: add `pb.UnimplementedIaCStateBackendServer` to the `awsIaCServer` struct embeds + a `stateBackend stateBackend` field; add `var _ pb.IaCStateBackendServer = (*awsIaCServer)(nil)` to the compile-time guard block. In `NewIaCServer`, wire the store (the store's bucket/region/credentials come from the `iac.state` module config at host call time — follow the azure precedent for how the store is constructed/injected; if azure constructs lazily on first `SaveState`, do the same).

`plugin.json`: add to `capabilities`:
```json
"iacStateBackends": ["s3"]
```

**Step 4: Run tests + host-conformance**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...`
Expected: PASS — incl. `internal/host_conformance_test.go` (the plugin loads + the contract registry sees `IaCStateBackend`).

**Step 5: Commit**

```bash
git add internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go plugin.json go.mod go.sum
git commit -m "feat: serve s3 IaC state backend via pb.IaCStateBackendServer"
```

---

### Task 3: workflow-plugin-aws — in-plugin AWS credential resolution (`buildAWSConfig` + marker handling)

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/awscreds/awscreds.go`
- Create: `internal/awscreds/awscreds_test.go`
- Modify: the aws plugin's existing IaC-provider credential path (locate via `grep -rn 'CloudCredentials\|AccessKey\|LoadDefaultConfig' internal/ provider/`) to route through `buildAWSConfig`.
- Reference (do NOT modify): `workflow` `module/cloud_account_aws_creds.go` (the SDK-bearing `awsProfileResolver`/`awsRoleARNResolver` bodies being re-homed here), `workflow` `module/cloud_account.go` (`CloudCredentials` struct).

**Context:** Phase B (Task 12) rewrites core's `awsProfileResolver`/`awsRoleARNResolver` to *declare, don't resolve* — they record `Extra["credential_source"] = "profile"|"role_arn"` markers instead of calling the SDK. The SDK-bearing resolution (`config.LoadDefaultConfig(WithSharedConfigProfile)`, `sts.AssumeRole`) must be re-homed **in the plugin**. `buildAWSConfig` is the single in-plugin entry point: given a `CloudCredentials` (static keys, or a `credential_source` marker) it returns a resolved `aws.Config`. It ALSO serves the standalone `storage.s3`/`step.s3_upload` modules' inline `credentials:` blocks (Tasks 4/5/6). This is the design's Option-1 credential model.

**Step 1: Write the failing test**

`internal/awscreds/awscreds_test.go`:
- `buildAWSConfig` with static `accessKey`/`secretKey` → config carries those creds.
- `buildAWSConfig` with `credential_source: "role_arn"` + a fake STS client injection point → `AssumeRole` path exercised.
- `buildAWSConfig` with `credential_source: "profile"` → `WithSharedConfigProfile` path (test with a temp `AWS_CONFIG_FILE`).
- `buildAWSConfig` with empty input → returns the env/default chain (no error).

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/awscreds/ -v`
Expected: FAIL — `undefined: buildAWSConfig`

**Step 3: Implement**

`internal/awscreds/awscreds.go` — `func BuildAWSConfig(ctx context.Context, creds CredInput) (aws.Config, error)` where `CredInput` carries `AccessKey/SecretKey/SessionToken/Region/RoleARN/ExternalID/Profile` + a `Source string` field (the marker). Logic:
- `Source == "profile"` → `config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))`.
- `Source == "role_arn"` (or `RoleARN != ""`) → build a base config (region + optional static creds), `sts.NewFromConfig`, `AssumeRole`, return a config carrying the assumed creds. (This is the body deleted from core's `awsRoleARNResolver` — port it verbatim, adapted to return `aws.Config`.)
- static keys present → `config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(...))`.
- else → `config.LoadDefaultConfig(ctx)` (env/default chain).
Then wire the aws plugin's existing IaC-provider credential path to call `BuildAWSConfig` so a host-supplied `CloudCredentials` with a `credential_source` marker resolves correctly inside the plugin.

**Step 4: Run tests**

Run: `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/awscreds/ ./internal/... -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/awscreds/ internal/<modified-provider-path>
git commit -m "feat: in-plugin AWS credential resolution with credential_source marker handling"
```

---

### Task 4: workflow-plugin-aws — plugin-native `storage.s3` module

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/modules/storage_s3.go`
- Create: `internal/modules/storage_s3_test.go`
- Modify: the aws plugin's module-factory registration (locate via `grep -rn 'ModuleFactories\|moduleTypes' internal/ cmd/`)
- Modify: `plugin.json` (`capabilities.moduleTypes` += `"storage.s3"`)
- Reference (do NOT modify): `workflow` `module/s3_storage.go` (`S3Storage` + `NewS3Storage`), `workflow` `plugins/storage/plugin.go:89` (the current `storage.s3` factory).

**Context:** `storage.s3` is user-facing pipeline functionality, not engine infra — it becomes a plugin-native module via the existing `ModuleFactories` SDK path (no new contract). Credentials move inline: a `credentials:` config block resolved via `awscreds.BuildAWSConfig` (Task 3), or `credentials_ref:` an in-plugin `aws.credentials` module (Task 5).

**Step 1: Write the failing test** — `storage_s3_test.go`: factory builds the module from a config map with an inline `credentials:` block; assert the module's `Name()` and that it resolves creds via `awscreds.BuildAWSConfig`.

**Step 2: Verify it fails** — `go test ./internal/modules/ -run StorageS3 -v` → FAIL `undefined`.

**Step 3: Implement** — port `module/s3_storage.go` → `internal/modules/storage_s3.go` (`package modules`, drop the `workflow/module` dependency, resolve creds via `awscreds.BuildAWSConfig` from the inline `credentials:` block or `credentials_ref:`). Register `"storage.s3"` in the plugin's `ModuleFactories` map. Add `"storage.s3"` to `plugin.json` `capabilities.moduleTypes`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/modules/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/modules/storage_s3.go internal/modules/storage_s3_test.go internal/<factory-file> plugin.json
git commit -m "feat: plugin-native storage.s3 module"
```

---

### Task 5: workflow-plugin-aws — plugin-native `step.s3_upload` + optional `aws.credentials` module

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/s3-storage-step-credentials`)

**Files:**
- Create: `internal/steps/s3_upload.go`, `internal/steps/s3_upload_test.go`
- Create: `internal/modules/aws_credentials.go`, `internal/modules/aws_credentials_test.go`
- Modify: the aws plugin's step-factory + module-factory registration
- Modify: `plugin.json` (`capabilities.stepTypes` += `"step.s3_upload"`; `moduleTypes` += `"aws.credentials"`)
- Reference (do NOT modify): `workflow` `module/pipeline_step_s3_upload.go` (`S3UploadStep` + `NewS3UploadStepFactory`), `workflow` `plugins/pipelinesteps/plugin.go:183`.

**Context:** `step.s3_upload` becomes plugin-native via the `StepFactories` SDK path. `aws.credentials` is the optional in-plugin DRY module: a config can declare one `aws.credentials` module and have many `storage.s3`/`step.s3_upload`/`iac.provider` entries `credentials_ref:` it, avoiding per-module `credentials:` repetition (design §3 Option-1 redundancy mitigation).

**Step 1: Write the failing tests** — `s3_upload_test.go`: factory builds the step from config (`bucket`/`region`/`key`/`body_from` required, per `module/pipeline_step_s3_upload.go`), creds via inline block or `credentials_ref:`. `aws_credentials_test.go`: the module exposes a resolved `CredInput` retrievable by `credentials_ref:` consumers.

**Step 2: Verify they fail** — `go test ./internal/steps/ ./internal/modules/ -run 'S3Upload|AWSCredentials' -v` → FAIL `undefined`.

**Step 3: Implement** — port `module/pipeline_step_s3_upload.go` → `internal/steps/s3_upload.go` (`package steps`, creds via `awscreds.BuildAWSConfig`). Create `internal/modules/aws_credentials.go` — a thin module wrapping a `CredInput`, registered as `aws.credentials`, resolvable by name from the service registry so `credentials_ref:` works. Register `"step.s3_upload"` in `StepFactories`, `"aws.credentials"` in `ModuleFactories`. Update `plugin.json`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 5: Commit**
```bash
git add internal/steps/ internal/modules/aws_credentials.go internal/modules/aws_credentials_test.go internal/<factory-files> plugin.json
git commit -m "feat: plugin-native step.s3_upload + aws.credentials DRY module"
```

---

### Task 6: workflow-plugin-aws — release

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/s3-storage-step-credentials`)

**Files:**
- Modify: `plugin.json` (`version` bump), `CHANGELOG.md` (if present)

**Context:** PR 4 (workflow core Phase B deletion) is **blocked on an installable aws plugin release** carrying the `s3` IaCStateBackend + `storage.s3` + `step.s3_upload`. This task cuts that release after PRs 1 + 2 are merged. Per `decisions/0034` this is autonomous.

**Change class:** Version pin update → after the tag, the version-skew audit is exercised by PR 4's Task 15 (image-launch). **Rollback: the plugin release is additive; if a defect surfaces, cut a patch release — do not delete the tag.**

**Step 1:** Bump `plugin.json` `version` (minor bump — new capabilities: `iacStateBackends`, `storage.s3`, `step.s3_upload`, `aws.credentials`). Add a CHANGELOG entry naming the new backend + module/step types + the inline-`credentials:` shape.

**Step 2:** Commit on the PR 2 branch:
```bash
git add plugin.json CHANGELOG.md
git commit -m "chore: release workflow-plugin-aws <version> — s3 state backend + storage.s3 + step.s3_upload"
```

**Step 3:** After PR 1 and PR 2 are both merged to the aws plugin default branch, tag + push from the merged default branch:
```bash
git checkout main && git pull
git tag v<version> && git push origin v<version>
```
Expected: GoReleaser CI run completes; release assets (linux/darwin amd64/arm64) attached to `v<version>`.

**Step 4: Verify** — `gh release view v<version> --repo GoCodeAlone/workflow-plugin-aws` shows the assets; the GoReleaser workflow run is `success`.

---

### Task 7: workflow-plugin-aws — register modules/steps + capability declaration audit

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/s3-storage-step-credentials`)

**Files:**
- Modify: `plugin.json` (final capability cross-check)
- Test: `internal/host_conformance_test.go` (extend if it asserts capability ↔ registration parity)

**Context:** Guard against the Phase A failure mode (`plugin.json` declaring a capability the plugin doesn't actually serve, or vice versa). This task is the explicit parity check before the release tag is consumed.

**Step 1: Write/extend the failing test** — extend `host_conformance_test.go` (or add `internal/capabilities_test.go`): for every name in `plugin.json` `capabilities.iacStateBackends`, assert `NewIaCServer().ListBackendNames` returns it; for every `moduleTypes`/`stepTypes` entry that this plan adds, assert a factory is registered.

**Step 2: Verify it fails** (if any drift exists) — `go test ./internal/ -run Conformance -v`.

**Step 3: Fix any drift** — reconcile `plugin.json` ↔ registrations.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add plugin.json internal/host_conformance_test.go
git commit -m "test: assert plugin.json capability ↔ registration parity"
```

---

### Task 8: workflow-plugin-digitalocean — port the S3-compatible store + serve `spaces`

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 3, branch `feat/spaces-iac-state-backend`)

**Files:**
- Create: `internal/statebackend/spaces.go`, `internal/statebackend/spaces_test.go`
- Create: `internal/statebackend_server.go`, `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `doIaCServer`; wire `NewIaCServer`)
- Modify: `plugin.json` (`capabilities.iacStateBackends` += `"spaces"`)
- Modify: `go.mod`/`go.sum` (pin workflow `d179b1aa`)
- Reference (do NOT modify): `workflow` `module/iac_state_spaces.go`, `workflow-plugin-azure/internal/statebackend_server.go`, Tasks 1 + 2 (the aws plugin did the structurally-identical port — same store, backend name `spaces` instead of `s3`).

**Context:** `iac_state_spaces.go` backs *both* `s3` and `spaces`. The DO plugin ports the **same store** independently (no shared module — each plugin owns its copy) and serves it as `spaces`. `doIaCServer` (`internal/iacserver.go:49`) already embeds the `IaCProvider*` + `ResourceDriver` + `PluginService` Unimplemented servers; add `pb.UnimplementedIaCStateBackendServer`.

**Step 1: Write the failing tests** — port `module/iac_state_spaces_test.go` → `internal/statebackend/spaces_test.go` (`package statebackend`, type `SpacesIaCStateStore` kept by its original name here). `internal/statebackend_server_test.go`: round-trip the 6 RPCs + assert `ListBackendNames` → `{["spaces"]}` + compile-guard `var _ pb.IaCStateBackendServer = (*doIaCServer)(nil)`.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-digitalocean && go test ./internal/statebackend/ ./internal/ -run 'State' -v` → FAIL `undefined`.

**Step 3: Implement** — port `module/iac_state_spaces.go` → `internal/statebackend/spaces.go` (`package statebackend`, local `IaCState` struct + `IaCStateStore` interface, keep env-var fallbacks). Create `internal/statebackend_server.go` mirroring Task 2 (`const doStateBackendName = "spaces"`, the 6 RPC methods on `doIaCServer`, `ListBackendNames` → `["spaces"]`, local converters). Add the embed + `stateBackend` field to `doIaCServer`; add the compile-guard. `plugin.json`: `capabilities.iacStateBackends: ["spaces"]`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-digitalocean && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 5: Commit**
```bash
git add internal/statebackend/ internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go plugin.json go.mod go.sum
git commit -m "feat: serve spaces IaC state backend via pb.IaCStateBackendServer"
```

---

### Task 9: workflow-plugin-digitalocean — `minEngineVersion` bump + migration note

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 3, branch `feat/spaces-iac-state-backend`)

**Files:**
- Modify: `plugin.json` (`minEngineVersion`, `description`/`keywords` if needed)
- Create: `docs/migrations/spaces-state-backend.md` (or append to an existing CHANGELOG/migrations doc)

**Context:** Phase B's core PR (PR 4) is a **clean break** for `spaces` — it deletes the in-core `spaces` case. After PR 4 merges, `iac.state` with `backend: spaces` requires a DO plugin version that serves the `spaces` `IaCStateBackend`. `minEngineVersion` must move to the `workflow` version that drops the in-core case. Since that version isn't tagged yet, set `minEngineVersion` to the `d179b1aa` pseudo-version floor (it carries the proto + serve hook the plugin now depends on); re-pinning to the post-PR-4 release is the tracked follow-up.

**Step 1:** In `plugin.json`, bump `minEngineVersion` from `"0.51.7"` to the `d179b1aa` pseudo-version (`go list -m github.com/GoCodeAlone/workflow` after Task 8's pin gives the exact string).

**Step 2:** Write the migration note: `iac.state` with `backend: spaces` now requires `workflow-plugin-digitalocean >= <this version>` loaded; the yaml `backend: spaces` value is unchanged; the in-core `spaces` backend is removed as of `workflow` <post-PR-4>.

**Step 3: Commit**
```bash
git add plugin.json docs/migrations/spaces-state-backend.md
git commit -m "docs: spaces state-backend migration note + minEngineVersion bump"
```

---

### Task 10: workflow-plugin-digitalocean — release

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (PR 3, branch `feat/spaces-iac-state-backend`)

**Files:**
- Modify: `plugin.json` (`version` — **minor** bump as the compatibility-break marker)

**Context:** PR 4 is blocked on an installable DO plugin release serving `spaces`. The DO plugin is currently `v1.0.13` — bump to `v1.1.0` (minor: the `spaces` clean-break + new `iacStateBackends` capability is a compatibility-relevant change). Per `decisions/0034` this is autonomous.

**Change class:** Version pin update. **Rollback: additive plugin release; on defect cut a patch — do not delete the tag. The `spaces` clean-break itself rolls back only as a matched pair with PR 4 (see plan Rollback section).**

**Step 1:** Bump `plugin.json` `version` → `1.1.0`. Commit on the PR 3 branch:
```bash
git add plugin.json && git commit -m "chore: release workflow-plugin-digitalocean v1.1.0 — spaces IaC state backend"
```

**Step 2:** After PR 3 is merged to the DO plugin default branch, tag + push from the merged default branch:
```bash
git checkout main && git pull && git tag v1.1.0 && git push origin v1.1.0
```

**Step 3: Verify** — `gh release view v1.1.0 --repo GoCodeAlone/workflow-plugin-digitalocean` shows assets; GoReleaser run `success`.

---

### Task 11: workflow core — delete dead `cloud_account_aws.go`

**Repo:** planning worktree (PR 4, branch `feat/cloud-sdk-extraction-bcd-p4-core-aws`) — `GOWORK=off` on all Go commands.

**Files:**
- Delete: `module/cloud_account_aws.go`

**Context:** `cloud_account_aws.go` holds `AWSConfigProvider` (interface) + `CloudAccount.AWSConfig()` + `CloudAccount.ValidateCredentials()` — all pure `aws-sdk-go-v2`. The design verified these are **dead code**: `awsProviderFrom` and every consumer were removed by #653.

**Step 1: Verify zero non-test consumers** (the failing-test equivalent for a deletion)

Run: `cd /Users/jon/workspace/workflow/_worktrees/cloud-sdk-extraction && grep -rn 'AWSConfigProvider\|\.AWSConfig(\|\.ValidateCredentials(' --include='*.go' . | grep -v '_test.go' | grep -v 'cloud_account_aws.go'`
Expected: **no output** (zero non-test consumers). If any line prints, STOP — the design's dead-code premise is wrong; surface to the user.

**Step 2: Delete + build**

```bash
git rm module/cloud_account_aws.go
GOWORK=off go build ./...
```
Expected: build succeeds (nothing referenced it). If a `_test.go` file referenced it, delete those test bodies too (they tested dead code).

**Step 3: Run module tests**

Run: `GOWORK=off go test ./module/...`
Expected: PASS.

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor: delete dead cloud_account_aws.go (zero consumers, removed by #653)"
```

---

### Task 12: workflow core — rewrite the SDK-bearing AWS credential resolvers

**Repo:** planning worktree (PR 4, branch `feat/cloud-sdk-extraction-bcd-p4-core-aws`) — `GOWORK=off`.

**Files:**
- Modify: `module/cloud_account_aws_creds.go`
- Modify: `module/cloud_account_aws_creds_test.go` (update behavior assertions for the two rewritten resolvers)

**Context:** `awsStaticResolver`/`awsEnvResolver` are already SDK-free. `awsProfileResolver`/`awsRoleARNResolver` carry the only `aws-sdk-go-v2` imports in this file (`aws`, `config`, `credentials`, `sts`). The design's model: every in-core resolver *declares, doesn't resolve* — `profile`/`role_arn` record the declared inputs + an `Extra["credential_source"]` marker; the aws plugin's `awscreds.BuildAWSConfig` (Task 3) performs the SDK-bearing resolution. After this rewrite the file imports only `fmt` + `os`.

**Step 1: Update the tests first**

In `cloud_account_aws_creds_test.go`, change the `awsProfileResolver`/`awsRoleARNResolver` assertions: they no longer populate `m.creds.AccessKey`/`SecretKey` from the SDK; they record `m.creds.Extra["profile"]` / `m.creds.RoleARN` + `m.creds.Extra["external_id"]` + `m.creds.Extra["credential_source"]`. The `awsRoleARNResolver` `roleARN == ""` → `fmt.Errorf` required-check is **kept** — assert it still errors.

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v`
Expected: FAIL — old SDK-resolution assertions don't match the (not-yet-rewritten) code... actually they still match the *old* code; this step verifies the *updated test* fails against the *old* implementation. Expected: FAIL — updated test expects markers, old code returns SDK-resolved keys.

**Step 3: Rewrite the two resolver bodies**

Replace `awsProfileResolver.Resolve` body — keep everything through the `m.creds.Extra["profile"] = profile` record, then:
```go
	m.creds.Extra["credential_source"] = "profile"
	return nil
}
```
(delete the `ctx`/`config.LoadDefaultConfig`/`cfg.Credentials.Retrieve`/key-assignment tail.)

Replace `awsRoleARNResolver.Resolve` body — keep the `credsMap` nil-check, the `roleARN`/`externalID` extraction, the `m.creds.RoleARN` + `Extra["external_id"]` records, and the `roleARN == ""` required-check; then:
```go
	m.creds.Extra["credential_source"] = "role_arn"
	return nil
}
```
(delete the `sessionName` extraction and the entire SDK block: `baseCfgOpts`, `config.LoadDefaultConfig`, `sts.NewFromConfig`, `AssumeRole`, the result recording.)

Update the import block to just `"fmt"` and `"os"` (drop `context`, `aws`, `config`, `credentials`, `sts`).

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add module/cloud_account_aws_creds.go module/cloud_account_aws_creds_test.go
git commit -m "refactor: AWS profile/role_arn resolvers declare credential_source marker, no SDK"
```

---

### Task 13: workflow core — delete `iac_state_spaces.go` + strip the `spaces` case

**Repo:** planning worktree (PR 4, branch `feat/cloud-sdk-extraction-bcd-p4-core-aws`) — `GOWORK=off`.

**Files:**
- Delete: `module/iac_state_spaces.go`, `module/iac_state_spaces_test.go`
- Modify: `module/iac_module.go` (remove the `case "spaces":` block from the `Init()` backend switch)

**Context:** `iac_state_spaces.go` backs `s3` *and* `spaces`. Both are now plugin-served (aws plugin `s3` — Task 2; DO plugin `spaces` — Task 8). The `spaces` case in `iac_module.go`'s `Init()` switch calls `NewSpacesIaCStateStore` from this file. Removing the `case "spaces":` block lets the switch's `default:` arm (merged in Phase A) route `backend: spaces|s3` to the plugin registry. **Clean break** — after this merges, `backend: spaces` and `backend: s3` require the respective plugin loaded.

**Step 1: Delete the store + remove the case**

```bash
git rm module/iac_state_spaces.go module/iac_state_spaces_test.go
```
In `module/iac_module.go`, delete the entire `case "spaces":` block (region/bucket/prefix/accessKey/secretKey/endpoint extraction + `NewSpacesIaCStateStore` call). Update the `default:` arm's error message — its in-core-backends list currently reads `'memory', 'filesystem', 'spaces', 'gcs', 'postgres'`; drop `'spaces'` (and also `'s3'` if listed). Leave `gcs` for now — Phase C (PR 8) removes it.

**Step 2: Build**

Run: `GOWORK=off go build ./...`
Expected: FAIL — `module/iac_module.go` no longer compiles only if something else referenced `NewSpacesIaCStateStore`; verify with `grep -rn 'NewSpacesIaCStateStore\|SpacesIaCStateStore' --include='*.go' .` → expected no output. If clean, build succeeds.

**Step 3: Test**

Run: `GOWORK=off go test ./module/ -run 'IaCModule|IaCState' -v`
Expected: PASS — the `default:`-arm plugin-registry dispatch test (from Phase A) still covers the unknown-backend path; `spaces` now flows through it.

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor: delete in-core spaces/s3 IaC state store — now plugin-served"
```

---

### Task 14: workflow core — delete `s3_storage.go` + `pipeline_step_s3_upload.go` + drop built-in registrations

**Repo:** planning worktree (PR 4, branch `feat/cloud-sdk-extraction-bcd-p4-core-aws`) — `GOWORK=off`.

**Files:**
- Delete: `module/s3_storage.go`, `module/s3_storage_test.go` (if present), `module/pipeline_step_s3_upload.go`, `module/pipeline_step_s3_upload_test.go` (if present)
- Modify: `plugins/storage/plugin.go` (drop the `"storage.s3"` factory at `:89`, the `"storage.s3"` entry in the capability list at `:37`, and the `storage.s3` schema entry at `:326`)
- Modify: `plugins/pipelinesteps/plugin.go` (drop the `"step.s3_upload"` factory at `:183` and the `"step.s3_upload"` capability entry at `:93`)
- Modify: `DOCUMENTATION.md` (remove `storage.s3` / `step.s3_upload` from the module/step tables per the CLAUDE.md documentation-maintenance rule)

**Context:** `storage.s3` + `step.s3_upload` are now plugin-native in `workflow-plugin-aws` (Tasks 4/5). The built-in engine plugins under `plugins/` import `module.*` directly — extracting each one drops its factory-map entry and the impl file. `storage.local`/`storage.gcs`/etc. in `plugins/storage/plugin.go` are untouched (`gcs` goes in PR 8).

**Step 1: Delete the impl files + remove registrations**

```bash
git rm module/s3_storage.go module/pipeline_step_s3_upload.go
git rm module/s3_storage_test.go module/pipeline_step_s3_upload_test.go 2>/dev/null || true
```
Edit `plugins/storage/plugin.go`: remove the `"storage.s3": func(...)` factory block, the `"storage.s3"` string from the capability slice, the `storage.s3` `Type:` schema block. Edit `plugins/pipelinesteps/plugin.go`: remove the `"step.s3_upload": wrapStepFactory(...)` line + the `"step.s3_upload"` capability string. Update `DOCUMENTATION.md`.

**Step 2: Build**

Run: `GOWORK=off go build ./...`
Expected: build succeeds. If a test or other file still references `NewS3Storage`/`NewS3UploadStepFactory`/`S3Storage`/`S3UploadStep`, `grep -rn` them — expected no output outside the deleted files.

**Step 3: Test**

Run: `GOWORK=off go test ./plugins/storage/... ./plugins/pipelinesteps/... ./module/...`
Expected: PASS.

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor: delete in-core storage.s3 + step.s3_upload — now plugin-native in workflow-plugin-aws"
```

---

### Task 15: workflow core — `go mod tidy` + `.phase-b-complete` marker + Phase B migration doc + image-launch validation

**Repo:** planning worktree (PR 4, branch `feat/cloud-sdk-extraction-bcd-p4-core-aws`) — `GOWORK=off`.

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `.phase-b-complete` (tracked marker file consumed by `scripts/audit-cloud-symbols.sh --check`)
- Create: `docs/migrations/2026-05-14-cloud-sdk-extraction.md` (or append the Phase B section if Phase A created it)

**Context:** After Tasks 11–14, `module/` no longer imports `aws-sdk-go-v2` for the IaC-state / standalone-S3 surface. `aws-sdk-go-v2` **stays** in `go.mod` — `provider/aws/`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go` still import it (out of scope). `go mod tidy` drops only the now-unused service modules. The `.phase-b-complete` marker arms the audit script's `cloud_account_aws_creds.go` zero-`aws-sdk-go-v2` invariant.

**Change class:** Build pipeline + go.mod dependency change → runtime-launch-validation required. **Rollback: revert PR 4; the deleted files are recoverable from git, the in-core `spaces`/`s3`/`storage.s3`/`step.s3_upload` paths restore, `go.mod` re-tidies. The `spaces` clean-break rolls back only as a matched pair with the DO plugin `v1.1.0` release (see Rollback section).**

**Step 1: Tidy + create marker**

```bash
GOWORK=off go mod tidy
touch .phase-b-complete
```

**Step 2: Run the audit script in enforcing mode**

Run: `bash scripts/audit-cloud-symbols.sh --check`
Expected: `audit-cloud-symbols: OK` — with `.phase-b-complete` present, the script asserts `cloud_account_aws_creds.go` has **0** `aws-sdk-go-v2` import lines. If FAIL, Task 12's rewrite is incomplete.

**Step 3: Build + full test + image-launch validation**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./...
GOWORK=off go build -o /tmp/wf-server ./cmd/server
```
Then runtime-launch-validation: build the server image / launch it against a representative `iac.state` config and confirm clean startup. Per `superpowers:runtime-launch-validation` — capture the transcript. The engine must start without the `aws-sdk-go-v2` IaC-state imports and dispatch `backend: s3`/`spaces` to the plugin registry (or fail cleanly with the "install the plugin" error if no plugin loaded — that is the *correct* clean-break behavior).
Expected: build + tests green; server starts; transcript captured; exit 0.

**Step 4: Write the migration doc**

`docs/migrations/2026-05-14-cloud-sdk-extraction.md` — Phase B section: `iac.state backend: s3` → load `workflow-plugin-aws`; `backend: spaces` → load `workflow-plugin-digitalocean`; `storage.s3` / `step.s3_upload` → load `workflow-plugin-aws`, `credentials:` moves inline (or `credentials_ref:` an `aws.credentials` module); `provider: aws` credential resolution for `profile`/`role_arn` is now performed in-plugin. yaml `backend:`/`provider:`/step-type names unchanged.

**Step 5: Commit**

```bash
git add go.mod go.sum .phase-b-complete docs/migrations/2026-05-14-cloud-sdk-extraction.md
git commit -m "build: drop unused aws-sdk-go-v2 IaC modules + arm Phase B audit invariant"
```

---

### Task 16: workflow core — `kubernetesBackend` interface-audit spike → ADR 0036

**Repo:** planning worktree (PR 5, branch `feat/cloud-sdk-extraction-bcd-p5-gke-spike`) — docs/decisions only, no Go code.

**Files:**
- Create: `decisions/0036-gke-cross-process-contract.md`

**Context:** Phase C extracts the one SDK-bearing `platform.*` backend — `gkeBackend` (`module/platform_kubernetes_gke.go`, `google.golang.org/api/container/v1`). The cross-process contract for `gke` is **gated on this spike** (design Architecture §2). The in-core `kubernetesBackend` interface (`module/platform_kubernetes.go:44-49`) is 4 methods: `plan(k) (*PlatformPlan, error)`, `apply(k) (*PlatformResult, error)`, `status(k) (*KubernetesClusterState, error)`, `destroy(k) error`. The audit picks, in the design's preference order:
1. **Fold `gke` into the existing `ResourceDriver` contract** (`iac.proto:78-88`, 9 RPCs: Create/Read/Update/Delete/Diff/Scale/HealthCheck/SensitiveKeys/Troubleshoot). A GKE cluster is a managed resource — `plan`→`Diff`, `apply`→`Create`/`Update`, `status`→`Read`, `destroy`→`Delete`. *Preferred* — zero new proto surface. **Strong prior signal:** `workflow-plugin-gcp/provider/drivers/real_clients.go` already imports `cloud.google.com/go/container` — the gcp plugin's `ResourceDriver` very likely already catalogs a GKE/`infra.k8s_cluster` resource type (the DO plugin declares `infra.k8s_cluster`).
2. **Plugin-native `kubernetesBackend`** via the `ModuleFactories`/`RemoteModule` SDK — only if `ResourceDriver`'s lifecycle shape doesn't fit.
3. **A minimal new `PlatformBackend` service** — fallback only.

**Step 1: Audit the in-core interface**

Read `module/platform_kubernetes.go` (the `kubernetesBackend` interface, `PlatformKubernetes`, `RegisterKubernetesBackend`), `module/platform_kubernetes_gke.go` (the `gkeBackend` 4 methods + `containerService`), `module/platform_provider.go` (`PlatformPlan`/`PlatformResult`), and `plugin/external/proto/iac.proto` (`ResourceDriver` + its request/response messages). Map each `kubernetesBackend` method onto a `ResourceDriver` RPC; note any shape mismatch (e.g. `status` returns the rich typed `KubernetesClusterState` — does `ResourceReadResponse.outputs_json` carry it cleanly? — and whether `gke` has any continuous-reconciliation behavior, which it does not: the 4 methods are one-shot lifecycle).

**Step 2: Investigate the gcp plugin's existing GKE coverage**

In `/Users/jon/workspace/workflow-plugin-gcp`: `grep -rn 'container\|gke\|k8s\|kubernetes' provider/ --include='*.go'`; read `provider/drivers/real_clients.go` + the `ResourceDriver` registration. Determine whether a GKE-cluster resource driver **already exists** in the gcp plugin (which would make Option 1's plugin-side work near-trivial — Task 18 just exposes/confirms it).

**Step 3: Write ADR 0036**

`decisions/0036-gke-cross-process-contract.md` in the Nygard format (`recording-decisions` skill). **Context:** the spike's premise + the 4-method interface + the 3 options. **Decision:** the chosen contract + one sentence per rejected option. **Consequences:** what Task 18 (gcp plugin) and Tasks 22/23 (core wiring) must implement; whether the gcp plugin already covers GKE; the proto-surface cost (zero if Option 1). Cite the design + this plan. Update this plan's Task 18/22/23 reference lines are unnecessary — those tasks already say "per ADR 0036".

**Step 4: Verify (documentation class)**

Render-preview the ADR; confirm no broken cross-references; confirm it picks exactly one option with reasoning. Run `ls decisions/ | sort | tail -3` to confirm `0036-` is the next free number.

**Step 5: Commit**

```bash
git add decisions/0036-gke-cross-process-contract.md
git commit -m "docs: ADR 0036 — gke cross-process contract (kubernetesBackend interface audit)"
```

---

### Task 17: workflow-plugin-gcp — port the GCS state store + serve `gcs`

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 6, branch `feat/gcs-gke-storage`)

**Files:**
- Create: `internal/statebackend/gcs.go`, `internal/statebackend/gcs_test.go`
- Create: `internal/statebackend_server.go`, `internal/statebackend_server_test.go`
- Modify: `internal/iacserver.go` (add `pb.UnimplementedIaCStateBackendServer` embed + `stateBackend` field to `gcpIaCServer`; wire `NewIaCServer`)
- Modify: `plugin.json` (`capabilities.iacStateBackends` += `"gcs"`)
- Modify: `go.mod`/`go.sum` (pin workflow `d179b1aa`)
- Reference (do NOT modify): `workflow` `module/iac_state_gcs.go` (`GCSIaCStateStore` + `NewGCSIaCStateStore`/`NewGCSIaCStateStoreWithClient`, `GCSObjectClient`), `workflow-plugin-azure/internal/statebackend_server.go`, Tasks 1+2/8 (the structurally-identical aws+DO ports).

**Context:** `module/iac_state_gcs.go`'s `GCSIaCStateStore` uses `cloud.google.com/go/storage` + `google.golang.org/api/{iterator,option}`. Port it into the gcp plugin, serve as `gcs`. `gcpIaCServer` (`internal/iacserver.go:36`) already embeds the `IaCProvider*` + `ResourceDriver` Unimplemented servers; add `pb.UnimplementedIaCStateBackendServer`.

**Step 1: Write the failing tests** — port `module/iac_state_gcs_test.go` → `internal/statebackend/gcs_test.go` (`package statebackend`, local `IaCState` + `IaCStateStore`, exercise `NewGCSIaCStateStoreWithClient` + the 6 ctx-ful methods). `internal/statebackend_server_test.go`: round-trip the 6 RPCs + `ListBackendNames` → `{["gcs"]}` + compile-guard `var _ pb.IaCStateBackendServer = (*gcpIaCServer)(nil)`.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/statebackend/ ./internal/ -run 'State' -v` → FAIL `undefined`.

**Step 3: Implement** — port `module/iac_state_gcs.go` → `internal/statebackend/gcs.go` (`package statebackend`, local `IaCState` struct + `IaCStateStore` interface, keep the `GCSObjectClient` indirection + `gcsRealClient`). Create `internal/statebackend_server.go` mirroring Task 2 (`const gcpStateBackendName = "gcs"`, the 6 RPCs on `gcpIaCServer`, `ListBackendNames` → `["gcs"]`, local converters). Add the embed + `stateBackend` field + compile-guard to `internal/iacserver.go`. `plugin.json`: `capabilities.iacStateBackends: ["gcs"]`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 5: Commit**
```bash
git add internal/statebackend/ internal/statebackend_server.go internal/statebackend_server_test.go internal/iacserver.go plugin.json go.mod go.sum
git commit -m "feat: serve gcs IaC state backend via pb.IaCStateBackendServer"
```

---

### Task 18: workflow-plugin-gcp — `gke` cross-process contract implementation (per ADR 0036)

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 6, branch `feat/gcs-gke-storage`)

**Files:**
- Modify/Create: per ADR 0036's decision (see below)
- Reference (do NOT modify): `decisions/0036-gke-cross-process-contract.md` (Task 16's output — **read it first, it fixes this task's shape**), `workflow` `module/platform_kubernetes_gke.go` (the `gkeBackend` logic being re-homed).

**Context:** This task's exact shape is **determined by ADR 0036** (Task 16). The implementer MUST read ADR 0036 before starting. The three possible shapes:
- **ADR picked Option 1 (ResourceDriver fold):** if the gcp plugin's `ResourceDriver` already catalogs a GKE/`infra.k8s_cluster` resource type (Task 16 determines this) → this task is a **verification + gap-fill**: confirm the existing driver covers create/read/update/diff/delete for a GKE cluster matching the in-core `gkeBackend` behavior (cluster create, status, destroy); add any missing field coverage; ensure `plugin.json` `capabilities.iacProvider.resourceTypes` lists the GKE type. If no such driver exists → port `module/platform_kubernetes_gke.go`'s `gkeBackend` logic into a new GKE `ResourceDriver` driver under `provider/drivers/`.
- **ADR picked Option 2 (plugin-native `kubernetesBackend`):** create a plugin-native module via the `ModuleFactories`/`RemoteModule` SDK exposing the GKE backend lifecycle.
- **ADR picked Option 3 (new minimal `PlatformBackend` service):** implement the new service (the proto addition is part of Task 22, core side; here implement the server).

**Step 1: Read ADR 0036.** Identify the chosen option + the consequences section's "what Task 18 must implement".

**Step 2: Write the failing test** — a test exercising the GKE lifecycle through whichever contract ADR 0036 picked (create → status/read → destroy/delete), against a fake GKE container client.

**Step 3: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./... -run GKE -v` → FAIL.

**Step 4: Implement** per ADR 0036. Port the `gkeBackend` SDK logic (`containerService`, the `Projects.Locations.Clusters.{Create,Get,Delete}` calls) from `module/platform_kubernetes_gke.go`. Credentials arrive as a serialized `CloudCredentials` (already proto-serialisable, no struct change) — resolve `ServiceAccountJSON` in-plugin exactly as the in-core `containerService` did.

**Step 5: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS (incl. `host_conformance_test.go`).

**Step 6: Commit**
```bash
git add internal/ provider/ plugin.json
git commit -m "feat: gke cross-process contract per ADR 0036"
```

---

### Task 19: workflow-plugin-gcp — plugin-native `storage.gcs` module + gcp credentials helper

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 6, branch `feat/gcs-gke-storage`)

**Files:**
- Create: `internal/gcpcreds/gcpcreds.go`, `internal/gcpcreds/gcpcreds_test.go`
- Create: `internal/modules/storage_gcs.go`, `internal/modules/storage_gcs_test.go`
- Create: `internal/modules/gcp_credentials.go`, `internal/modules/gcp_credentials_test.go`
- Modify: the gcp plugin's module-factory registration; `plugin.json` (`capabilities.moduleTypes` += `"storage.gcs"`, `"gcp.credentials"`)
- Reference (do NOT modify): `workflow` `module/storage_gcs.go` (`GCSStorage` + `NewGCSStorage`), `workflow` `plugins/storage/plugin.go:109`, Tasks 3+4 (the aws plugin's structurally-identical `awscreds`/`storage.s3`/`aws.credentials`).

**Context:** `storage.gcs` becomes plugin-native, mirroring Phase B's `storage.s3`. The gcp credential resolvers (`module/cloud_account_gcp.go`) are already SDK-free, so `gcpcreds.BuildGCPOptions` is simpler than the aws equivalent — it builds `[]option.ClientOption` from an inline `credentials:` block (`ServiceAccountJSON` → `option.WithCredentialsJSON`) with an Application-Default-Credentials fallback. `gcp.credentials` is the optional DRY module + `credentials_ref:` key.

**Step 1: Write the failing tests** — `gcpcreds_test.go`: `BuildGCPOptions` with inline service-account JSON; with empty input (ADC fallback). `storage_gcs_test.go`: factory builds the module from a config with `credentials:`/`credentials_ref:`. `gcp_credentials_test.go`: the module exposes a resolved option set by name.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/gcpcreds/ ./internal/modules/ -v` → FAIL `undefined`.

**Step 3: Implement** — `internal/gcpcreds/gcpcreds.go` (`BuildGCPOptions`). Port `module/storage_gcs.go` → `internal/modules/storage_gcs.go` (`package modules`, creds via `gcpcreds.BuildGCPOptions`). Create `internal/modules/gcp_credentials.go` (the `gcp.credentials` DRY module). Register `"storage.gcs"` + `"gcp.credentials"` in `ModuleFactories`; update `plugin.json`.

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add internal/gcpcreds/ internal/modules/ internal/<factory-file> plugin.json
git commit -m "feat: plugin-native storage.gcs module + gcp.credentials DRY module"
```

---

### Task 20: workflow-plugin-gcp — capability parity audit

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 6, branch `feat/gcs-gke-storage`)

**Files:**
- Modify: `plugin.json` (final cross-check); `internal/host_conformance_test.go` (extend)

**Context:** Same parity guard as Task 7 — `plugin.json` capabilities (`iacStateBackends`, `moduleTypes`, the GKE `resourceTypes` entry if ADR 0036 picked Option 1) must match actual registrations.

**Step 1: Write/extend the failing test** — assert every `plugin.json` capability this plan adds (`gcs` backend, `storage.gcs`, `gcp.credentials`, and the GKE contract surface per ADR 0036) has a corresponding registration.

**Step 2: Verify it fails** (if drift) — `go test ./internal/ -run Conformance -v`.

**Step 3: Reconcile** `plugin.json` ↔ registrations.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add plugin.json internal/host_conformance_test.go
git commit -m "test: assert plugin.json capability ↔ registration parity"
```

---

### Task 21: workflow-plugin-gcp — release

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 6, branch `feat/gcs-gke-storage`)

**Files:**
- Modify: `plugin.json` (`version` bump), `CHANGELOG.md` (if present)

**Context:** PR 8 (workflow core Phase C deletion) is blocked on an installable gcp plugin release carrying `gcs` + the `gke` contract + `storage.gcs`. The gcp plugin is currently `v1.0.0` — minor bump (new capabilities). Per `decisions/0034` autonomous.

**Change class:** Version pin update. **Rollback: additive plugin release; on defect cut a patch — do not delete the tag.**

**Step 1:** Bump `plugin.json` `version` (minor). CHANGELOG entry naming `gcs` backend + `gke` contract + `storage.gcs`/`gcp.credentials`.

**Step 2:** Commit on the PR 6 branch:
```bash
git add plugin.json CHANGELOG.md && git commit -m "chore: release workflow-plugin-gcp <version> — gcs state backend + gke + storage.gcs"
```

**Step 3:** After PR 6 is merged to the gcp plugin default branch, tag + push from the merged default branch:
```bash
git checkout main && git pull && git tag v<version> && git push origin v<version>
```

**Step 4: Verify** — `gh release view v<version> --repo GoCodeAlone/workflow-plugin-gcp` shows assets; GoReleaser run `success`.

---

### Task 22: workflow core — `gke` cross-process contract proto/adapter (per ADR 0036)

**Repo:** planning worktree (PR 7, branch `feat/cloud-sdk-extraction-bcd-p7-gke-wiring`) — `GOWORK=off`.

**Files:**
- Create: `module/platform_kubernetes_grpc.go`, `module/platform_kubernetes_grpc_test.go`
- Modify (only if ADR 0036 picked Option 3): `plugin/external/proto/iac.proto` + regenerate
- Reference (do NOT modify): `decisions/0036-gke-cross-process-contract.md` (**read first**), `module/platform_kubernetes.go` (`kubernetesBackend` interface), `module/iac_state_grpc_client.go` (the Phase A `grpcIaCStateStore` adapter — the precedent for this file).

**Context:** The host-side adapter that lets `platform.kubernetes`'s in-core `kubernetesBackend` interface dispatch the `gke` provider to a plugin gRPC client. Shape per ADR 0036:
- **Option 1 (ResourceDriver fold):** `grpcKubernetesBackend` implements the in-core `kubernetesBackend` interface (`plan`/`apply`/`status`/`destroy`), delegating to a `pb.ResourceDriverClient` — `plan`→`Diff`, `apply`→`Create`/`Update`, `status`→`Read`, `destroy`→`Delete`. JSON-bytes converters (`PlatformPlan`/`PlatformResult`/`KubernetesClusterState` ↔ the `ResourceDriver` request/response messages), mirroring `iac_state_grpc_client.go`'s `iacStateToProto`/`FromProto` pattern. **No proto change.**
- **Option 2:** the `RemoteModule` adapter for a plugin-native `kubernetesBackend`.
- **Option 3:** add the minimal `PlatformBackend` service to `iac.proto` (regenerate — additive, preserves the no-`structpb` invariant) + the `grpcKubernetesBackend` adapter over it.

**Step 1: Read ADR 0036.** Pin the contract.

**Step 2: Write the failing test** — `platform_kubernetes_grpc_test.go`: a fake client of the chosen contract; assert `grpcKubernetesBackend.{plan,apply,status,destroy}` round-trip correctly (incl. `KubernetesClusterState` survives the JSON-bytes round-trip).

**Step 3: Verify it fails** — `GOWORK=off go test ./module/ -run GRPCKubernetesBackend -v` → FAIL `undefined`.

**Step 4: Implement** `module/platform_kubernetes_grpc.go` — `grpcKubernetesBackend` + the converters (+ the proto regen for Option 3 only).

**Step 5: Verify it passes** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ -run GRPCKubernetesBackend -v` → PASS.

**Step 6: Commit**
```bash
git add module/platform_kubernetes_grpc.go module/platform_kubernetes_grpc_test.go plugin/external/proto/
git commit -m "feat: grpcKubernetesBackend adapter for plugin-served gke (per ADR 0036)"
```

---

### Task 23: workflow core — engine seam + registry for plugin-served kubernetes backends

**Repo:** planning worktree (PR 7, branch `feat/cloud-sdk-extraction-bcd-p7-gke-wiring`) — `GOWORK=off`.

**Files:**
- Create: `module/platform_kubernetes_plugin_registry.go`, `module/platform_kubernetes_plugin_registry_test.go`
- Modify: `module/platform_kubernetes.go` (the backend-resolution path consults the registry for non-core providers)
- Modify: `engine.go` (`loadPluginInternal` populates the registry — mirrors the Phase A `IaCStateBackendProvider` seam)
- Modify: `plugin/` — a `KubernetesBackendProvider` optional interface + `ExternalPluginAdapter` accessor (mirrors `plugin/iac_state_backend_provider.go` + the Phase A adapter accessor)
- Reference (do NOT modify): `module/iac_state_plugin_registry.go`, `plugin/iac_state_backend_provider.go`, the `engine.go` `loadPluginInternal` `IaCStateBackendProvider` block — **the exact Phase A precedent for every piece of this task.**

**Context:** Phase A wired `iac.state` plugin backends via: `module.iacStateBackendRegistry` + exported `RegisterIaCStateBackend`, the `plugin.IaCStateBackendProvider` optional interface, the `ExternalPluginAdapter` accessor, the `engine.go` `loadPluginInternal` type-assert seam. This task does the structurally-identical wiring for **kubernetes backends**: a `kubernetesBackendClientRegistry` (`gke` → contract client), an exported `RegisterKubernetesBackendClient`, a `plugin.KubernetesBackendProvider` optional interface, the adapter accessor, and the `loadPluginInternal` seam. `module/platform_kubernetes.go`'s backend resolution: for `provider: kind|k3s|eks|aks` use the in-core `kubernetesBackendRegistry` (factory map) unchanged; for any other provider (`gke`) consult the new client registry and wrap the client in Task 22's `grpcKubernetesBackend`.

**Step 1: Write the failing tests** — registry register/resolve/reserved-name-rejection (mirror `iac_state_plugin_registry_test.go`); a `platform_kubernetes_test.go` case asserting `provider: gke` with a registered client resolves to a `grpcKubernetesBackend`, and with no client gives a clean "install workflow-plugin-gcp" error.

**Step 2: Verify they fail** — `GOWORK=off go test ./module/ ./plugin/... -run 'KubernetesBackend|PlatformKubernetes' -v` → FAIL.

**Step 3: Implement** — the registry + exported register fn; the `plugin.KubernetesBackendProvider` interface + adapter accessor; the `engine.go` seam (copy the `IaCStateBackendProvider` block's structure); the `platform_kubernetes.go` resolution branch.

**Step 4: Verify they pass** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ ./plugin/... ./... -run 'KubernetesBackend|PlatformKubernetes|Engine' -v` → PASS.

**Step 5: Commit**
```bash
git add module/platform_kubernetes_plugin_registry.go module/platform_kubernetes_plugin_registry_test.go module/platform_kubernetes.go engine.go plugin/
git commit -m "feat: engine seam + registry for plugin-served kubernetes backends"
```

---

### Task 24: workflow core — delete GCS files + strip the `gcs` case

**Repo:** planning worktree (PR 8, branch `feat/cloud-sdk-extraction-bcd-p8-core-gcp`) — `GOWORK=off`.

**Files:**
- Delete: `module/iac_state_gcs.go`, `module/iac_state_gcs_test.go`, `module/storage_gcs.go`, `module/storage_gcs_test.go` (if present), `module/platform_kubernetes_gke.go`, `module/platform_kubernetes_gke_test.go` (if present)
- Modify: `module/iac_module.go` (remove the `case "gcs":` block)
- Modify: `plugins/storage/plugin.go` (drop the `"storage.gcs"` factory at `:109`, the capability entry at `:39`, the schema at `:352`)
- Modify: `DOCUMENTATION.md` (remove `storage.gcs`)

**Context:** Depends on **PR 6** (gcp plugin release: `gcs` backend + `gke` contract + `storage.gcs`) and **PR 7** (gke wiring merged). `iac_state_gcs.go` (`gcs` backend) → gcp plugin; `storage_gcs.go` → plugin-native; `platform_kubernetes_gke.go` (`gkeBackend`) → its `gke` dispatch now flows through PR 7's `kubernetesBackendClientRegistry` + `grpcKubernetesBackend`. The `gke` `init()` registration in `platform_kubernetes_gke.go` is deleted with the file — `provider: gke` resolution falls through to PR 7's plugin-client path.

**Step 1: Delete + strip**
```bash
git rm module/iac_state_gcs.go module/storage_gcs.go module/platform_kubernetes_gke.go
git rm module/iac_state_gcs_test.go module/storage_gcs_test.go module/platform_kubernetes_gke_test.go 2>/dev/null || true
```
In `module/iac_module.go`: delete the `case "gcs":` block; drop `'gcs'` from the `default:`-arm error message's in-core-backends list (it should now read `'memory', 'filesystem', 'postgres'`). In `plugins/storage/plugin.go`: remove the `storage.gcs` factory + capability + schema. Update `DOCUMENTATION.md`.

**Step 2: Build**
Run: `GOWORK=off go build ./...`
Expected: succeeds. `grep -rn 'NewGCSIaCStateStore\|NewGCSStorage\|gkeBackend\|GCSIaCStateStore\|GCSStorage' --include='*.go' .` → expected no output (the audit script's `init()` partition check also guards `platform_kubernetes_gke.go`'s removal).

**Step 3: Test**
Run: `GOWORK=off go test ./module/... ./plugins/storage/...`
Expected: PASS — `provider: gke` resolution is covered by PR 7's registry test; `backend: gcs` flows through the `default:` arm.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core gcs state store + storage.gcs + gkeBackend — now plugin-served"
```

---

### Task 25: workflow core — drop GCP SDKs from go.mod + permanent CI gate

**Repo:** planning worktree (PR 8, branch `feat/cloud-sdk-extraction-bcd-p8-core-gcp`) — `GOWORK=off`.

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `scripts/audit-cloud-symbols.sh` (add the permanent Phase C invariants)
- Modify: `.github/workflows/ci.yml` (the `cloud-sdk-audit` job — add the `go list -deps` graph check)
- Create: `.phase-c-complete` (tracked marker)

**Context:** After Task 24, `cloud.google.com/go/storage` + `google.golang.org/api/*` have **zero** importers in core's build graph — `go mod tidy` drops them entirely. The permanent CI gate is **asymmetric** (design Goals): (a) `go list -deps ./...` asserts **zero** `Azure/azure-sdk-for-go` AND **zero** `cloud.google.com/go` / `google.golang.org/api` packages anywhere in core's build graph — Azure + GCP fully gone; (b) `scripts/audit-cloud-symbols.sh --check` asserts **zero** `aws-sdk-go-v2` imports under `module/` — AWS gone from `module/`, but `aws-sdk-go-v2` *remains* a `go.mod` entry for the out-of-scope `provider/aws/` etc. surface. `godo` remains — out of scope, not asserted.

**Change class:** Build pipeline + go.mod dependency change → runtime-launch-validation required. **Rollback: revert PR 8; deleted files recoverable from git, the in-core `gcs`/`storage.gcs`/`gke` paths restore, `go.mod` re-adds the GCP SDKs on `go mod tidy`. The `gke` dispatch falls back to PR 7's registry returning the clean "install plugin" error until reverted — no crash.**

**Step 1: Tidy + marker**
```bash
GOWORK=off go mod tidy
touch .phase-c-complete
```
Confirm `go.mod` no longer lists `cloud.google.com/go/storage` or `google.golang.org/api`.

**Step 2: Add the permanent invariants**

In `scripts/audit-cloud-symbols.sh`: add a `--check` block — `go list -deps ./... 2>/dev/null` piped to `grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'` must be **empty** (FAIL if any line). Add a `module/`-scoped `aws-sdk-go-v2` zero-import assertion (the existing whole-repo map already distinguishes `module/` from elsewhere — assert the `module/` count is 0). In `.github/workflows/ci.yml` `cloud-sdk-audit` job, ensure it runs `audit-cloud-symbols.sh --check` (already wired in Phase 0) — confirm the new graph check executes there.

**Step 3: Build + full test + audit + image-launch validation**
```bash
GOWORK=off go build ./... && GOWORK=off go test ./...
bash scripts/audit-cloud-symbols.sh --check    # expect: audit-cloud-symbols: OK
GOWORK=off go list -deps ./... | grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'   # expect: no output
```
Then runtime-launch-validation: build + launch the server against a representative `iac.state` / `platform.kubernetes` config; confirm clean startup; capture the transcript. Expected: all green; `go list -deps` grep empty; server starts; exit 0.

**Step 4: Commit**
```bash
git add go.mod go.sum scripts/audit-cloud-symbols.sh .github/workflows/ci.yml .phase-c-complete
git commit -m "build: drop GCP SDKs from go.mod + permanent asymmetric cloud-SDK CI gate"
```

---

### Task 26: workflow core — Phase C migration doc + final cross-phase verification

**Repo:** planning worktree (PR 8, branch `feat/cloud-sdk-extraction-bcd-p8-core-gcp`) — `GOWORK=off`.

**Files:**
- Modify: `docs/migrations/2026-05-14-cloud-sdk-extraction.md` (append the Phase C section)
- Modify: `DOCUMENTATION.md` (final pass — `platform.kubernetes` `provider: gke` now requires `workflow-plugin-gcp`)

**Context:** Final documentation + the cross-phase sanity check that the whole B/C/D extraction is coherent.

**Step 1: Write the Phase C migration section** — `iac.state backend: gcs` → load `workflow-plugin-gcp`; `platform.kubernetes provider: gke` → load `workflow-plugin-gcp` (`provider: kind|k3s|eks|aks` unchanged, still core); `storage.gcs` → load `workflow-plugin-gcp`, `credentials:` inline (or `credentials_ref:` a `gcp.credentials` module). yaml `backend:`/`provider:`/module-type names unchanged.

**Step 2: Final verification (documentation class + cross-phase)**
- Render-preview the migration doc — no broken anchors.
- `bash scripts/audit-cloud-symbols.sh --check` → `OK` (both `.phase-b-complete` and `.phase-c-complete` present, all invariants enforced).
- `GOWORK=off go build ./... && GOWORK=off go test ./...` → green.

**Step 3: Commit**
```bash
git add docs/migrations/2026-05-14-cloud-sdk-extraction.md DOCUMENTATION.md
git commit -m "docs: Phase C migration guide + final cloud-SDK-extraction doc pass"
```

---

## Rollback (whole-plan)

This plan changes **plugin loading paths** and **go.mod dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list. Per-PR rollback:

- **PRs 1/2/3/6 (plugin PRs)** are additive — reverting them is harmless to a core that still has the in-core paths; on a defect, prefer a forward patch release over deleting a tag.
- **PR 4 (Phase B core deletion)** — reverting restores the in-core `s3`/`spaces`/`storage.s3`/`step.s3_upload` paths + the SDK-bearing resolvers; `go.mod` re-tidies. The **`spaces` clean-break** is the one external-user-visible compat break — PR 4 + the DO plugin `v1.1.0` release roll back **as a matched pair**.
- **PR 5 (ADR)** — docs only; revert is a doc revert.
- **PR 7 (gke wiring)** — additive; reverting removes the plugin-served `gke` path. Safe only *before* PR 8 deletes the in-core `gkeBackend`; after PR 8, PR 7 + PR 8 revert as a pair.
- **PR 8 (Phase C core deletion)** — reverting restores in-core `gcs`/`storage.gcs`/`gkeBackend` + re-adds the GCP SDKs on `go mod tidy`.
- **Forward-fix preferred:** each core PR keeps the old in-process path removed only *after* the contract dispatch is wired in the same PR (or a merged predecessor) — a broken phase fails at PR CI (image-launch / audit-script gates), not in production. The revert path exists; the gate is the primary safety.

## Notes for the executor

- **Team sizing:** 26 tasks → 3 implementers (per `subagent-driven-development` sizing).
- **Cross-repo discipline:** every PR-1/2/3/6 dispatch prompt MUST name the absolute plugin-repo path and state it is a *different* repo than the worktree (see Cross-repo note). The aws plugin must brief implementers that PR 1 and PR 2 are *sequential, same repo* — PR 2 branches off PR 1's merged result.
- **`GOWORK=off`** on every Go command in the planning worktree; never in the plugin repos.
- **Dependency gates are real:** PR 4 cannot start until PR 2 + PR 3 tags exist and are installable; PR 6 + PR 7 cannot start until PR 5 (ADR 0036) merges; PR 8 cannot start until PR 6 tag + PR 7 merged. The scope-lock per-task checkpoint + watchdog cadence apply.
- **ADR 0036 is load-bearing for Tasks 18, 22, 23** — those tasks are written contract-parameterized; the implementer reads ADR 0036 first. The design pre-ranked Option 1 (ResourceDriver fold) as preferred and the gcp plugin already imports the GKE container SDK — Option 1 is the strongly-expected outcome, but the spike is authoritative.
- **The Phase A precedent is the template** — `workflow-plugin-azure` (PR #8) for the plugin side, `module/iac_state_grpc_client.go` + `module/iac_state_plugin_registry.go` + the `engine.go` `IaCStateBackendProvider` seam for the core side. Cite them; don't reinvent.
