# Infra Solution Holistic — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.18.6 + workflow-plugin-digitalocean v0.7.2 (in flight) and v0.7.3, manually teardown `bmw-staging`, bump BMW to new pins, redeploy BMW through staging to prod.

**Architecture:** Five-fix workflow core release + two-release DO plugin cadence + one manual cloud teardown + BMW adoption PR. All changes flow through branch → code-reviewer local → push → Copilot → team-lead admin-merge. No direct-to-main pushes. No bypass.

**Tech Stack:** Go 1.26, workflow's IaCProvider/ResourceDriver/StateStore interfaces, godo, doctl, GitHub Actions.

**Reference:** Design doc at `docs/plans/2026-04-23-infra-solution-holistic-design.md`.

---

## Phase A — v0.7.2 plugin (PR #16 in flight)

### Task 1: Merge PR #16 and tag v0.7.2

**Files:** none (merge + tag)

**Step 1:** Wait for Copilot re-review on PR #16 head commit to fire. If new comments appear, impl-digitalocean-2 addresses them with LOCAL code-reviewer approval before pushing fix commits.

**Step 2:** When CI green on the head commit AND zero unreplied Copilot threads AND code-reviewer approved — team-lead admin-merge via `gh pr merge 16 -R GoCodeAlone/workflow-plugin-digitalocean --admin --squash`.

**Step 3:** Tag: `git checkout main && git pull --ff-only && git tag -a v0.7.2 -m "v0.7.2 — panic-safe upsert gate via UpsertSupporter interface" <merge-sha> && git push origin v0.7.2`

**Step 4:** Verify Release workflow publishes binaries. `gh run list --repo GoCodeAlone/workflow-plugin-digitalocean --workflow=Release --limit 1`.

---

## Phase B — v0.7.3 plugin (name-based discovery on VPC/Firewall/Database)

### Task 2: Branch off main for v0.7.3

**Files:** none

**Step 1:** `cd /Users/jon/workspace/workflow-plugin-digitalocean && git fetch origin && git checkout main && git pull --ff-only && git checkout -b feat/v0.7.3-name-based-read`

### Task 3: VPCDriver name-based Read + SupportsNameBasedRead

**Files:**
- Modify: `internal/drivers/vpc.go`
- Modify: `internal/drivers/vpc_test.go`

**Step 1: Write failing test** for `VPCDriver.Read` with empty ProviderID:

```go
func TestVPCDriver_Read_NameBased(t *testing.T) {
    fakeClient := &fakeGodoClient{
        vpcs: []*godo.VPC{
            {ID: "vpc-1", Name: "other-vpc", RegionSlug: "nyc3"},
            {ID: "vpc-target", Name: "bmw-staging-vpc", RegionSlug: "nyc3"},
        },
    }
    d := NewVPCDriver(fakeClient, "nyc3")
    ref := interfaces.ResourceRef{Name: "bmw-staging-vpc", Type: "infra.vpc"}  // empty ProviderID
    out, err := d.Read(t.Context(), ref)
    if err != nil {
        t.Fatalf("Read: %v", err)
    }
    if out.ProviderID != "vpc-target" {
        t.Errorf("ProviderID = %q, want vpc-target", out.ProviderID)
    }
}

func TestVPCDriver_SupportsNameBasedRead(t *testing.T) {
    d := NewVPCDriver(nil, "nyc3")
    us, ok := d.(interfaces.UpsertSupporter)
    if !ok {
        t.Fatal("VPCDriver should implement UpsertSupporter")
    }
    if !us.SupportsNameBasedRead() {
        t.Error("SupportsNameBasedRead = false, want true")
    }
}
```

**Step 2: Run tests — verify they fail** with "undefined" / "does not implement UpsertSupporter".

**Step 3: Implement** in `internal/drivers/vpc.go`:
- In `Read(ctx, ref)`: if `ref.ProviderID == ""`, call `client.List(ctx, opts)` (paginate), find VPC matching `ref.Name` in the configured region, return `ResourceOutput` with its ID. If not found, return `interfaces.ErrResourceNotFound`.
- Add `func (d *VPCDriver) SupportsNameBasedRead() bool { return true }`.

**Step 4: Run tests — verify pass** via `GOWORK=off go test -race ./internal/drivers/ -run TestVPCDriver_Read_NameBased -v`.

**Step 5: Commit**: `git add internal/drivers/vpc.go internal/drivers/vpc_test.go && git commit -m "feat(vpc): name-based Read + UpsertSupporter"`

### Task 4: FirewallDriver name-based Read + SupportsNameBasedRead

Same structure as Task 3 for `FirewallDriver`. Critical: also fixes the `firewall.go:148` nil-deref panic that blocked BMW. Test must cover the scenario where empty ProviderID is passed — current code panics; new code lists and matches by name.

**Files:**
- Modify: `internal/drivers/firewall.go`
- Modify: `internal/drivers/firewall_test.go`

Implementation: when `ref.ProviderID == ""`, `client.Firewalls.List(ctx, opts)`, match by name, return; else return `ErrResourceNotFound`. Guard `fwOutput(nil)` case with a nil-check that returns a clear error instead of panicking (belt + suspenders).

Include tests: `TestFirewallDriver_Read_NameBased`, `TestFirewallDriver_Read_EmptyProviderID_NoMatch_ReturnsNotFound`, `TestFirewallDriver_SupportsNameBasedRead`, `TestFirewallDriver_Read_EmptyProviderID_NilClientReturn_NoPanic` (regression test for the BMW panic).

Commit: `git commit -m "fix(firewall): name-based Read + UpsertSupporter + nil-safe fwOutput"`

### Task 5: DatabaseDriver name-based Read + SupportsNameBasedRead

Same structure. `client.Databases.List(ctx, opts)`, match on `Name`.

**Files:**
- Modify: `internal/drivers/database.go`
- Modify: `internal/drivers/database_test.go`

Tests: `TestDatabaseDriver_Read_NameBased`, `TestDatabaseDriver_SupportsNameBasedRead`.

Commit: `git commit -m "feat(database): name-based Read + UpsertSupporter"`

### Task 6: Integration test — all drivers upsert

**Files:**
- Modify: `internal/provider_test.go`

**Step 1:** Add test `TestDOProvider_Apply_UpsertAllDrivers` using the existing upsert fake machinery from PR #16. Verify Apply succeeds for a plan containing VPC + Firewall + Database + AppService creates where all four already exist in the cloud (return `ErrResourceAlreadyExists`); assert Update was called for each, not Create.

Commit: `git commit -m "test: integration — upsert across VPC+Firewall+DB+App drivers"`

### Task 7: CHANGELOG + push PR

**Step 1:** Update `CHANGELOG.md` with v0.7.3 section: "name-based Read fallback on VPCDriver/FirewallDriver/DatabaseDriver; fixes FirewallDriver nil-deref panic when called from upsert path with empty ProviderID; enables end-to-end upsert for BMW-style multi-resource apply configs."

**Step 2:** `GOWORK=off go build ./... && GOWORK=off go test -race -short ./... && go vet ./... && gofmt -l ./...` — all clean.

**Step 3:** DM code-reviewer LOCAL with the 4 commits. Wait for approval.

**Step 4:** `git push -u origin feat/v0.7.3-name-based-read`. Open PR titled `feat: v0.7.3 — name-based Read on VPC/Firewall/Database drivers`.

**Step 5:** `gh pr edit <N> --add-reviewer copilot-pull-request-reviewer`. Wait full 15-30min Copilot window.

**Step 6:** Address all Copilot comments via fix commits. LOCAL code-reviewer approval BEFORE push for each round.

**Step 7:** When zero unreplied threads AND CI green AND Copilot fired, DM team-lead with "v0.7.3 PR ready for merge approval".

**Step 8:** Team-lead admin-merges. Tags v0.7.3. Waits for Release workflow to publish binaries.

---

## Phase C — v0.18.6 workflow core (5 fixes)

### Task 8: Branch for v0.18.6

**Step 1:** `cd /Users/jon/workspace/workflow && git fetch origin && git checkout main && git pull --ff-only && git checkout -b feat/v0.18.6-infra-holistic`

### Task 9: State persistence in applyWithProvider

**Files:**
- Modify: `cmd/wfctl/infra_apply.go`
- Modify: `cmd/wfctl/infra_apply_test.go`

**Step 1: Write failing test:**

```go
func TestApplyWithProvider_SavesStateForSuccessfulResources(t *testing.T) {
    fake := &fakeIaCProvider{
        applyResult: &interfaces.ApplyResult{
            Resources: []interfaces.ResourceOutput{
                {Name: "r1", Type: "infra.vpc", ProviderID: "vpc-1", Outputs: map[string]any{"id": "vpc-1"}},
                {Name: "r2", Type: "infra.database", ProviderID: "db-1", Outputs: map[string]any{"uri": "postgres://..."}},
            },
        },
    }
    store := &fakeStateStore{}
    err := applyWithProviderAndStore(t.Context(), fake, someSpecs, nil, store)
    if err != nil { t.Fatalf("apply: %v", err) }
    if len(store.saved) != 2 {
        t.Errorf("saved = %d, want 2", len(store.saved))
    }
    if store.saved[0].ProviderID != "vpc-1" { t.Errorf("wrong ProviderID: %q", store.saved[0].ProviderID) }
}

func TestApplyWithProvider_SavesStateOnPartialFailure(t *testing.T) {
    // Apply returns 2 Resources + 1 Error. Assert only 2 states saved.
    fake := &fakeIaCProvider{
        applyResult: &interfaces.ApplyResult{
            Resources: []interfaces.ResourceOutput{{Name: "r1", ProviderID: "id-1"}, {Name: "r2", ProviderID: "id-2"}},
            Errors:    []interfaces.ActionError{{Resource: "r3", Action: "create", Error: "boom"}},
        },
    }
    store := &fakeStateStore{}
    err := applyWithProviderAndStore(t.Context(), fake, threeSpecs, nil, store)
    if err == nil { t.Fatal("expected error on partial failure") }
    if len(store.saved) != 2 {
        t.Errorf("saved = %d, want 2 (partial success should still persist)", len(store.saved))
    }
}
```

**Step 2: Run — fail** (test refs `applyWithProviderAndStore` which doesn't exist).

**Step 3: Refactor + implement:**

- Rename existing `applyWithProvider` to `applyWithProviderAndStore(ctx, provider, specs, current, store)`. Old callers pass `nil` for store (no-op save) during migration.
- After `provider.Apply` returns success, iterate `result.Resources` and for each construct a `ResourceState{ID, Name, Type, Provider, ProviderID, Outputs, AppliedConfig: specs[i].Config, Status:"ready", CreatedAt/UpdatedAt: now}` and `store.SaveState(&st)`. If SaveState errors, log a loud warning but do NOT fail the apply (the cloud resource exists).
- For `delete` actions in the plan: after provider.Apply, call `store.DeleteState(id)` for each resource in the delete actions.
- Update `applyInfraModules` to instantiate the state store once and pass it through.

**Step 4: Run — pass** via `GOWORK=off go test ./cmd/wfctl/ -run TestApplyWithProvider_SavesState -v`.

**Step 5: Commit**: `git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_test.go && git commit -m "feat(wfctl): persist ResourceState after provider.Apply — fixes BMW state loss"`

### Task 10: Remote-backend state loader

**Files:**
- Modify: `cmd/wfctl/infra.go` (`loadCurrentState`)
- Modify: `cmd/wfctl/infra_test.go`
- Possibly: `cmd/wfctl/infra_state_loader.go` (new helper file)

**Step 1: Write failing test** mocking a Spaces state store:

```go
func TestLoadCurrentState_SpacesBackend(t *testing.T) {
    // Config declares iac.state with backend: spaces, bucket: test-bkt
    // Mock SpacesStateStore.List() → 2 ResourceState records
    // Assert loadCurrentState returns both
}

func TestLoadCurrentState_BackendLoadError_PropagatesError(t *testing.T) {
    // Mock backend returns a transient error on Load
    // Assert loadCurrentState returns the error (does NOT silently return empty state)
}
```

**Step 2: Run — fail.**

**Step 3: Implement:**

Add a new function `resolveStateStore(cfgFile string) (StateStore, error)` that:
- Reads the iac.state module config.
- Instantiates the correct backend: filesystem / spaces / s3 / gcs / azure / postgres.
- Returns the store.

Rewrite `loadCurrentState(cfgFile)` to call `resolveStateStore` then `store.List()`. On error from Load (not "not found"), return it.

Credential resolution: env vars per backend (`DO_SPACES_ACCESS_KEY` + `DO_SPACES_SECRET_KEY` for spaces; `AWS_ACCESS_KEY_ID` etc for s3; `GOOGLE_APPLICATION_CREDENTIALS` for gcs; `AZURE_STORAGE_CONNECTION_STRING` for azure; `DATABASE_URL` for postgres).

**Step 4: Run — pass.**

**Step 5: Commit**: `git commit -m "feat(wfctl): loadCurrentState supports remote state backends (spaces/s3/gcs/azure/postgres)"`

### Task 11: Direct-path runInfraDestroy

**Files:**
- Modify: `cmd/wfctl/infra.go` (`runInfraDestroy`)
- Create: `cmd/wfctl/infra_destroy.go` (new)
- Create: `cmd/wfctl/infra_destroy_test.go`

**Step 1: Write failing test:**

```go
func TestRunInfraDestroy_InfraModules_DirectPath(t *testing.T) {
    // Config with only infra.* modules + 2 ResourceStates in the store.
    // Mock provider.Destroy returning success.
    // Run destroyInfraModules → assert provider.Destroy called with 2 refs, store.DeleteState called 2x.
}

func TestRunInfraDestroy_LegacyPlatformModules_PipelinePath(t *testing.T) {
    // Config with platform.* modules and pipelines.destroy defined.
    // Assert runPipelineRun(-p destroy) is invoked.
}
```

**Step 2: Run — fail.**

**Step 3: Implement:** in `cmd/wfctl/infra_destroy.go`, add `destroyInfraModules(ctx, cfg, envName, store)`:

- Load current state via the new state store.
- Group state records by provider (infer provider from resource type's prefix).
- For each provider group, resolve the IaCProvider plugin, build ResourceRefs from state records, call `provider.Destroy(ctx, refs)`.
- On success, call `store.DeleteState(r.ID)` for each record.
- Fail fast on mixed module configs (same pattern as apply in v0.18.5).

Modify `runInfraDestroy` to dispatch via `hasInfraModules(cfg)` — new path for infra.*, legacy path for platform.*.

**Step 4: Run — pass.**

**Step 5: Commit**: `git commit -m "feat(wfctl): direct-path infra destroy for infra.* modules"`

### Task 12: Direct-path runInfraStatus + runInfraDrift

**Files:**
- Modify: `cmd/wfctl/infra.go` (`runInfraStatus`, `runInfraDrift`)
- Create: `cmd/wfctl/infra_status_drift.go` (new) OR add to existing infra.go
- Create: `cmd/wfctl/infra_status_drift_test.go`

**Step 1: Write failing tests** mirroring Task 11 pattern.

**Step 2: Implement** `statusInfraModules` and `driftInfraModules` — iterate state records, call `provider.Status(ctx, refs)` and `provider.DetectDrift(ctx, refs)` respectively, format output. Same dispatch logic in runInfraStatus/Drift.

**Step 3: Pass tests.**

**Step 4: Commit**: `git commit -m "feat(wfctl): direct-path infra status + drift for infra.* modules"`

### Task 13: Bootstrap URL export

**Files:**
- Modify: `cmd/wfctl/infra_bootstrap.go` (or `infra_bootstrap_secrets.go`)
- Modify: `cmd/wfctl/infra_bootstrap_test.go` (or create)

**Step 1: Write failing test:**

```go
func TestBootstrapStateBackend_WritesBucketURLBackToConfig(t *testing.T) {
    // Mock: bootstrap creates a Spaces bucket, returns URL.
    // Call runInfraBootstrap with a config that has iac.state { backend: spaces, bucket: "" }
    // Assert: the on-disk config after bootstrap has bucket: "bmw-iac-state-staging" (or whatever was generated)
}
```

**Step 2: Run — fail.**

**Step 3: Implement:** after `bootstrapStateBackend` creates the bucket, write its name (and region) back to the on-disk config file via YAML round-trip. Also print `export DO_SPACES_BUCKET=bmw-iac-state-staging` to stdout for CI capture.

**Step 4: Pass.**

**Step 5: Commit**: `git commit -m "feat(wfctl): bootstrap exports Spaces bucket URL back to config"`

### Task 14: CHANGELOG + validate + push PR

**Files:**
- Modify: `CHANGELOG.md`

**Step 1:** Add v0.18.6 section to `CHANGELOG.md`:

```
## v0.18.6 - 2026-04-23

### Fixed

- `wfctl infra apply` now persists ResourceState after each successful resource apply (previously state was never saved, causing every run to attempt recreate)
- `loadCurrentState` supports remote state backends (spaces/s3/gcs/azure/postgres), not just filesystem
- `wfctl infra destroy` dispatches to a direct provider path for `infra.*` module configs (previously required non-existent `pipelines.destroy`)
- `wfctl infra status` and `wfctl infra drift` same dispatch fix
- `wfctl infra bootstrap` now writes the provisioned Spaces bucket URL back to the config and exports it to stdout for CI capture
```

**Step 2:** `GOWORK=off go build ./... && GOWORK=off go test -race -short ./cmd/wfctl/... ./module/... ./interfaces/... && go vet ./... && gofmt -l ./...` — all clean.

**Step 3:** DM code-reviewer LOCAL with all 5 feature commits. Wait for approval.

**Step 4:** `git push -u origin feat/v0.18.6-infra-holistic`. Open PR titled `feat: v0.18.6 — infra state persistence + remote backend loader + destroy/status/drift direct paths + bootstrap URL export`.

**Step 5:** `gh pr edit <N> --add-reviewer copilot-pull-request-reviewer`. Wait full 15-30min Copilot window.

**Step 6:** Address all Copilot comments via fix commits. LOCAL code-reviewer approval BEFORE push for each round.

**Step 7:** When zero unreplied threads AND CI green AND Copilot fired, DM team-lead with "v0.18.6 PR ready for merge approval".

**Step 8:** Team-lead admin-merges. Tags v0.18.6. Verifies Release workflow publishes binaries.

---

## Phase D — bmw-staging teardown

### Task 15: One-off teardown of existing BMW staging cluster

**Step 1:** Team-lead runs (user-approved destructive op):

```
doctl apps list --format ID,Spec.Name --no-header | awk '$2 == "bmw-staging" { print $1 }' | xargs -r doctl apps delete --force
doctl databases list --format ID,Name --no-header | awk '$2 == "bmw-staging-db" { print $1 }' | xargs -r doctl databases delete --force
doctl compute firewall list --format ID,Name --no-header | awk '$2 == "bmw-staging-firewall" { print $1 }' | xargs -r doctl compute firewall delete --force
doctl vpcs list --format ID,Name --no-header | awk '$2 == "bmw-staging-vpc" { print $1 }' | xargs -r doctl vpcs delete --force
```

**Step 2:** Verify teardown: `doctl apps list` etc. — none of the bmw-staging resources present.

**Step 3:** Also clear any stale state in the Spaces state bucket: `doctl spaces -s bmw-iac-state rm staging/ --recursive` (if bucket exists; ignore if not).

**Step 4:** DM implementers that Phase D is complete.

---

## Phase E — BMW bumps

### Task 16: Bump setup-wfctl and workflow-plugin-digitalocean

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist/.github/workflows/deploy.yml`
- Modify: `/Users/jon/workspace/buymywishlist/app.yaml`

**Step 1:** `cd /Users/jon/workspace/buymywishlist && git fetch origin && git checkout main && git pull --ff-only && git checkout -b chore/bump-wfctl-v0.18.6-do-v0.7.3`

**Step 2:** Edit `.github/workflows/deploy.yml`: replace all `setup-wfctl@v0.18.5` refs with `@v0.18.6` (and any earlier pins). Typically 3-6 occurrences.

**Step 3:** Edit `app.yaml` (or the BMW file that pins `requires.plugins[].version`): replace `workflow-plugin-digitalocean: v0.7.1` with `v0.7.3`.

**Step 4:** DM code-reviewer LOCAL, wait for approval.

**Step 5:** Push + open PR titled `chore: bump setup-wfctl v0.18.6 + workflow-plugin-digitalocean v0.7.3`.

**Step 6:** `gh pr edit <N> --add-reviewer copilot-pull-request-reviewer`. Wait Copilot window.

**Step 7:** Address any Copilot comments.

**Step 8:** DM team-lead when ready. Team-lead admin-merges.

---

## Phase F — BMW staging deploy + verify

### Task 17: Monitor Deploy workflow

**Step 1:** After Phase E merge, Deploy workflow auto-triggers on main. impl-bmw-2 monitors `gh run list --repo GoCodeAlone/buymywishlist --workflow=Deploy --limit 1`.

**Step 2:** Verify each job completes:
- build-image (supply-chain preinstall + distroless build)
- deploy-staging (bootstrap-staging → wfctl infra apply → state saved → deploy app → PRE_DEPLOY migrations → rollout)

**Step 3:** Once deploy-staging completes, curl `https://bmw-staging.ondigitalocean.app/healthz`. Expect 200.

**Step 4:** DM team-lead with "staging /healthz green".

### Task 18: Playwright verification (delegated agent)

**Step 1:** impl-bmw-2 spawns a delegated playwright Agent via the Agent tool. Prompt:

```
URL: https://bmw-staging.ondigitalocean.app
Golden path: (1) signup with new email, (2) log in, (3) browse wishlist index,
(4) create a wishlist, (5) share wishlist link, (6) view shared link as guest.
Edge cases: (a) unauthed /dashboard → expect redirect to /login, (b) /wishlist/bad-slug → expect graceful 404 UI not 500, (c) login with bad password → expect form error not 500.
Report: pass/fail for each step, any JS console errors, any 5xx responses observed.
Return under 300 words.
```

**Step 2:** Agent runs headless playwright, returns structured summary.

**Step 3:** impl-bmw-2 DMs team-lead with the agent's verdict.

---

## Phase G — BMW prod promotion

### Task 19: Monitor auto-promote + prod /healthz

**Step 1:** Per BMW's DEPLOYMENT.md, deploy-prod job runs automatically after deploy-staging's /healthz gate passes. impl-bmw-2 monitors the deploy workflow's deploy-prod job.

**Step 2:** On deploy-prod success, curl prod `/healthz`. Expect 200.

**Step 3:** Final DM to team-lead: "Platform maturity rollout complete — BMW in prod on distroless + signed + SBOM'd + tenant-aware + pre-deploy migrations + state-persisted infra".

**Step 4:** team-lead DMs user with the final state.

---

## Execution

- **Task 1 (Phase A merge)** runs now if PR #16 is merge-ready.
- **Task 2 (Phase B start)** depends on Task 1 merge (uses main post-v0.7.2 as base).
- **Tasks 8-14 (Phase C)** run in parallel with Phase B.
- **Task 15 (Phase D)** requires BOTH v0.7.3 and v0.18.6 published.
- **Phase E** requires Phase D complete.
- **Phase F** requires Phase E merged.
- **Phase G** follows Phase F.

Autonomous pipeline continues in subagent-driven-development skill using the existing `platform-maturity-stage2` team.
