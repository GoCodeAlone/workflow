# Infra Solution Holistic Fix — Design

**Status:** Approved (autonomous pipeline, 2026-04-23)

**Goal:** Close the end-to-end correctness gaps in the wfctl infra subsystem that blocked BMW's staging deploy. Ship workflow v0.18.6 (state persistence + remote-backend loader + destroy/status/drift direct paths + bootstrap URL export), workflow-plugin-digitalocean v0.7.2 (panic-safe upsert gate, in flight) and v0.7.3 (name-based discovery for VPC/Firewall/Database). One-off teardown of `bmw-staging` cluster in DigitalOcean. Retry BMW staging deploy and verify prod.

**Why now:** A holistic audit of wfctl infra surfaced three compounding bugs that cascaded through the BMW platform-maturity rollout: (a) `applyWithProvider` never persists state after Apply, so every run sees "empty" state and tries to create every resource; (b) `loadCurrentState` only handles the filesystem backend, so Spaces/S3/GCS state buckets are silently ignored; (c) `DOProvider.Apply`'s upsert on `ErrResourceAlreadyExists` only works for `AppPlatformDriver`, panics for `FirewallDriver` et al. These combined mean every BMW deploy attempts to recreate bmw-vpc/bmw-firewall/bmw-database/bmw-app, gets 409 conflicts, and crashes. The fix must land at both workflow core and DO plugin levels. While we're in here, also repair `runInfraDestroy`/`status`/`drift` (same v0.18.5 regression as `apply` had) and wire `infra bootstrap`'s bucket URL back into the apply path so the bootstrap actually does something useful.

## Architecture

Three-tier fix spanning two repos and one manual cloud op:

1. **workflow v0.18.6** — core wfctl fixes, tagged after PR merge.
2. **workflow-plugin-digitalocean v0.7.2** — panic-safe upsert gate (already in PR #16), then v0.7.3 — name-based discovery on VPC/Firewall/Database drivers.
3. **bmw-staging teardown + retry** — one-off `doctl` deletes of app/firewall/vpc/database, then BMW deploys on v0.18.6 + v0.7.3 with empty-state-saves-correctly semantics.

The direct-provision path introduced in v0.18.5 (`applyWithProvider`) is kept — this design doesn't revert that. It adds the missing pieces: state save on success, state load from remote backends, destroy/status/drift via direct path, bootstrap URL export.

## Components

### 1. State persistence in applyWithProvider (workflow v0.18.6)

File: `cmd/wfctl/infra_apply.go` — `applyWithProvider` function at line 158.

After `provider.Apply()` returns success, iterate `result.Resources` and for each, call `stateStore.SaveState(&interfaces.ResourceState{...})` populated from the resource outputs. State is saved per-resource so partial success persists correctly — if 3 of 4 resources succeed and the 4th errors, the 3 successes are still in state for the next run.

Resource state fields populated: `ID`, `Name`, `Type`, `Provider` (from the provider module type config), `ProviderID` (from `result.Resources[i].ProviderID`), `Outputs` (from `result.Resources[i].Outputs`), `AppliedConfig` (from the spec's Config map), `Status: "ready"`, `CreatedAt` / `UpdatedAt`.

For update actions, the existing state record is updated in place (same ID) rather than appended. For delete actions, the record is removed via `stateStore.DeleteState(id)`.

The state store is resolved via a new helper `resolveStateStore(cfgFile) (StateStore, error)` that reads the `iac.state` module config and instantiates the appropriate backend. This replaces the current `loadCurrentState` function which only handles filesystem.

### 2. Remote-backend state loader (workflow v0.18.6)

File: `cmd/wfctl/infra.go` — `loadCurrentState` function at line 342.

Extend `loadCurrentState` to resolve the full `iac.state` module config and construct the matching backend:

- `backend: filesystem` → existing path
- `backend: spaces` → `module.NewSpacesStateStore(bucket, region, accessKey, secretKey, prefix)` reading from config + env
- `backend: s3` → similar via AWS SDK
- `backend: gcs` → GCS client
- `backend: azure` → Azure Blob client
- `backend: postgres` → PostgreSQL state store (already has an implementation at `module/iac_state_postgres.go`)

All backends expose the same `StateStore` interface (Load / Save / Delete / List). The function returns a loaded `[]interfaces.ResourceState` slice.

Credentials come from env vars (`AWS_ACCESS_KEY_ID`, `DO_SPACES_ACCESS_KEY`, `GCP_APPLICATION_CREDENTIALS`, `AZURE_STORAGE_CONNECTION_STRING`, `DATABASE_URL` respectively). If a backend fails to load (transient network error, missing credentials), `loadCurrentState` returns the error rather than silently returning empty state — so downstream `ComputePlan` doesn't mis-plan against empty state.

### 3. Direct-path runInfraDestroy (workflow v0.18.6)

File: `cmd/wfctl/infra.go` — `runInfraDestroy` function at line 943.

Mirror the v0.18.5 pattern: check whether the config has `infra.*` modules via `hasInfraModules(cfg)`. If yes, run a new `destroyInfraModules(ctx, cfg, envName)` that:

1. Resolves provider groups (same as apply).
2. Loads current state via the new remote-backend loader.
3. For each provider, computes a "destroy plan" = all state records whose `Provider` matches → turn into `Destroy` actions.
4. Calls `provider.Destroy(ctx, resourceRefs)`.
5. On success, deletes state records via `stateStore.DeleteState(id)`.

If the config has `platform.*` modules (legacy), fall through to the existing `runPipelineRun(-p destroy)` path. Mixed-module configs fail fast, same as apply.

### 4. Direct-path runInfraStatus / runInfraDrift (workflow v0.18.6)

Files: `cmd/wfctl/infra.go` — both functions.

Same dispatch pattern. For `infra.*` modules:

- Status: iterate state records, call `provider.Status(ctx, refs)` per provider, print results.
- Drift: iterate state records, call `provider.DetectDrift(ctx, refs)` per provider, report any drift detected.

For legacy `platform.*` modules: fall through to pipeline path.

### 5. Bootstrap URL export (workflow v0.18.6)

File: `cmd/wfctl/infra_bootstrap.go` (and related `infra_bootstrap_*.go`).

After `bootstrapStateBackend()` successfully creates the Spaces bucket (or equivalent), capture the bucket URL and:

1. Write it to the `iac.state` module's config in-place (via a YAML round-trip) so subsequent commands don't need an env var — the config itself is self-contained.
2. ALSO export it as `DO_SPACES_BUCKET_URL` env var for the current process (useful for immediate downstream steps in the same shell).
3. Print the URL to stdout for CI workflows to capture and persist as a GitHub secret if desired.

This closes the "bootstrap creates bucket but nothing uses it" gap.

### 6. v0.7.2 panic-safe upsert gate (workflow-plugin-digitalocean, PR #16 in flight)

Already implemented in the open PR. Adds a new optional `UpsertSupporter` interface. `DOProvider.Apply()` only calls `driver.Read()` with empty ProviderID if the driver implements `UpsertSupporter` and returns true from `SupportsNameBasedRead()`. Only `AppPlatformDriver` currently does. Non-supporting drivers propagate `ErrResourceAlreadyExists` without panicking.

### 7. v0.7.3 name-based discovery on VPC/Firewall/Database (workflow-plugin-digitalocean, new)

For each of `VPCDriver`, `FirewallDriver`, `DatabaseDriver`:

- Implement `Read(ctx, ref)` to support empty `ref.ProviderID`: list all resources of this type in the configured DO account, find the one whose name matches `ref.Name`, return its `ResourceOutput`. If not found, return `ErrResourceNotFound`.
- Implement `SupportsNameBasedRead() bool` returning `true`.
- Add tests using the existing `godo` mock pattern.

After v0.7.3, DO plugin upsert works for all four core resource types. BMW's apply plan can then upsert bmw-vpc/bmw-firewall/bmw-database/bmw-app idempotently.

### 8. bmw-staging manual teardown

User-approved destructive cloud op. Via `doctl`:

```
doctl apps delete <bmw-staging app id> --force
doctl databases delete <bmw-staging-db id> --force
doctl compute firewall delete <bmw-staging-fw id> --force
doctl vpcs delete <bmw-staging-vpc id> --force
```

After teardown, the next `wfctl infra apply --env staging` starts clean. With v0.18.6 state-save and remote loader in place, subsequent applies are idempotent.

## Data flow (the critical path)

```
wfctl infra apply --env staging
  → loadCurrentState (remote Spaces backend) → ResourceState[] (empty on first run)
  → group specs by provider
  → for each provider group:
     → ComputePlan(specs, stateForThisProvider) → Actions
     → provider.Apply(ctx, plan) → Result{Resources, Errors}
     → for each Resource in Result: stateStore.SaveState(&ResourceState{...})  # ← NEW
     → if plan action was delete: stateStore.DeleteState(id)                   # ← NEW
  → syncInfraOutputSecrets (existing)

Next run:
wfctl infra apply --env staging
  → loadCurrentState → ResourceState[] (populated from last run)               # ← NOW POPULATED
  → ComputePlan finds matching state → no-op for unchanged, Update for changed
  → idempotent
```

## Error handling

- **State save failure after successful Apply**: log a loud warning ("cloud resource created but state save failed — next apply will see conflict; run `wfctl infra import` to reconcile"). Do NOT fail the overall apply since the resource IS created; failing would leave the user in a worse state.
- **State load failure (transient)**: fail the apply with a clear error rather than assume empty state. Operator retries or fixes credentials.
- **Upsert 409 on non-supporting driver**: return `ErrResourceAlreadyExists` wrapped with driver name. Operator can import manually or wait for v0.7.3.
- **Destroy partial failure**: same pattern as apply — per-resource state delete on success, keep failed records in state.

## Testing

- `cmd/wfctl/infra_apply_test.go`:
  - `TestApplyWithProvider_SavesState` — run Apply, assert state store was called with N ResourceStates.
  - `TestApplyWithProvider_SavesStateOnPartialFailure` — Apply returns 3 successes + 1 error; assert 3 states saved, 1 not.
- `cmd/wfctl/infra_test.go`:
  - `TestLoadCurrentState_Spaces` — mock Spaces backend, assert state loaded correctly.
  - `TestLoadCurrentState_BackendError_Fails` — transient load error propagates, apply fails fast.
- `cmd/wfctl/infra_destroy_test.go` (new):
  - `TestRunInfraDestroy_InfraModules_DirectPath` — destroy routes through `destroyInfraModules` when only infra.* modules present.
  - `TestRunInfraDestroy_LegacyPlatformModules_PipelinePath` — legacy config falls through to pipeline.
- DO plugin:
  - `TestVPCDriver_Read_NameBased` — empty ProviderID returns VPC by name.
  - `TestFirewallDriver_Read_NameBased` — same for firewall.
  - `TestDatabaseDriver_Read_NameBased` — same for database.
  - `TestDOProvider_Apply_UpsertAllDrivers` — apply with pre-existing VPC+firewall+database+app all upsert cleanly.

## Deferred (explicit non-goals)

Documenting these so subsequent work knows what's intentionally not here:

- Multi-provider state isolation (`ResourceState.Provider` field + filtering in ComputePlan). Deferred. BMW is single-provider. Tracked as follow-up.
- Partial-apply retry / exponential backoff. Deferred. Operators retry the whole apply; v0.18.6 state-save means retry is now idempotent.
- `wfctl infra apply --plan <file>` consuming saved plans. Deferred.
- `wfctl infra import` real implementation (currently a stub). Deferred.
- Transient List error handling in `syncInfraOutputSecrets`. Deferred.
- Old `step.iac_apply` signature audit. Deferred — if legacy pipeline configs break, users migrate to direct path.
- STRIPE_SECRET_KEY capture pattern (Task #16). Deferred to a separate BMW-side PR after staging is up.

## Rollout phases

1. **Phase A — v0.7.2 plugin** (PR #16, in flight): panic-safe upsert gate. Currently in Copilot re-review. Merge + tag.
2. **Phase B — v0.7.3 plugin**: name-based discovery on VPC/Firewall/Database. New PR. Parallel to Phase C.
3. **Phase C — v0.18.6 workflow**: all 5 core fixes (state save, remote backend loader, destroy/status/drift direct paths, bootstrap URL export). Single PR, one release.
4. **Phase D — bmw-staging teardown**: `doctl` deletes. Manual one-off.
5. **Phase E — BMW bumps**: new PR to BMW bumping setup-wfctl → v0.18.6, workflow-plugin-digitalocean → v0.7.3. After merge, Deploy retriggers.
6. **Phase F — BMW staging verify**: delegated playwright agent against the fresh deploy. Confirm /healthz green.
7. **Phase G — BMW prod promote**: auto-promote on staging green (per BMW DEPLOYMENT.md). Confirm prod /healthz.

Phase A currently in-flight; Phase B can begin as soon as A merges. Phase C is independent of A/B and can begin immediately in parallel. Phase D runs after both A+B+C are released. Phases E→F→G serialize on BMW.

## Success criteria

- `wfctl infra apply --env staging` on BMW leaves all 4 resources in state; second invocation is a no-op.
- `wfctl infra destroy --env staging` on BMW tears everything down cleanly when invoked against infra.* config.
- BMW staging deploys end-to-end, /healthz returns 200, playwright golden-path passes, prod auto-promotes and returns 200.
- Zero direct-push or admin-bypass operations in the rollout (all changes through PR + review + Copilot + team-lead merge).
