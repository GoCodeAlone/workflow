# wfctl Provider-Executed Migration Repair Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `wfctl migrate repair-dirty` so guarded dirty migration repairs run inside provider-managed trusted runtime boundaries instead of from CI runners.

**Architecture:** wfctl resolves env-aware app/database infra modules, applies destructive-operation gating, and calls an optional provider interface. The first provider implementation target is DigitalOcean App Platform via `workflow-plugin-digitalocean`; Workflow core owns the CLI, request/result types, remote dispatch plumbing, and tests.

**Tech Stack:** Go 1.26, wfctl, Workflow `interfaces`, external plugin gRPC service dispatch, DigitalOcean App Platform jobs/deployments, GitHub Actions step summaries.

---

### Task 1: Add Core Migration Repair Interface Types

**Files:**
- Create: `interfaces/migration_repair.go`
- Test: `interfaces/migration_repair_test.go`

**Step 1: Write the failing interface validation tests**

Create tests for `MigrationRepairRequest.Validate()`:

```go
func TestMigrationRepairRequestValidateRequiresGuardFields(t *testing.T) {
    req := interfaces.MigrationRepairRequest{
        AppResourceName:      "bmw-app",
        DatabaseResourceName: "bmw-database",
        JobImage:             "registry.example/workflow-migrate:sha",
        SourceDir:            "/migrations",
    }
    err := req.Validate()
    if err == nil {
        t.Fatal("expected validation error")
    }
    for _, want := range []string{"expected_dirty_version", "force_version", "confirm_force"} {
        if !strings.Contains(err.Error(), want) {
            t.Fatalf("error %q missing %q", err, want)
        }
    }
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./interfaces -run TestMigrationRepairRequest -count=1`

Expected: FAIL because `MigrationRepairRequest` is undefined.

**Step 3: Add types and validation**

Define:

```go
type MigrationRepairRequest struct {
    AppResourceName      string            `json:"app_resource_name"`
    DatabaseResourceName string            `json:"database_resource_name"`
    JobImage             string            `json:"job_image"`
    SourceDir            string            `json:"source_dir"`
    ExpectedDirtyVersion string            `json:"expected_dirty_version"`
    ForceVersion         string            `json:"force_version"`
    ThenUp               bool              `json:"then_up"`
    UpIfClean            bool              `json:"up_if_clean"`
    ConfirmForce         string            `json:"confirm_force"`
    Env                  map[string]string `json:"env,omitempty"`
    TimeoutSeconds       int               `json:"timeout_seconds,omitempty"`
}

type MigrationRepairResult struct {
    ProviderJobID string       `json:"provider_job_id,omitempty"`
    Status        string       `json:"status"`
    Applied       []string     `json:"applied,omitempty"`
    Logs          string       `json:"logs,omitempty"`
    Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

type ProviderMigrationRepairer interface {
    RepairDirtyMigration(ctx context.Context, req MigrationRepairRequest) (*MigrationRepairResult, error)
}
```

`Validate()` must require app, database, job image, source dir, expected dirty version, force version, and `ConfirmForce == "FORCE_MIGRATION_METADATA"`.

**Step 4: Run tests**

Run: `GOWORK=off go test ./interfaces -run TestMigrationRepairRequest -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add interfaces/migration_repair.go interfaces/migration_repair_test.go
git commit -m "feat(interfaces): add migration repair request contract"
```

### Task 2: Add wfctl Destructive Operation Gate

**Files:**
- Create: `cmd/wfctl/destructive_gate.go`
- Test: `cmd/wfctl/destructive_gate_test.go`

**Step 1: Write failing gate tests**

Add tests covering:

- `staging` without `--approve-destructive` returns approval-required and writes JSON.
- `prod` without approval returns approval-required.
- `dev` executes without explicit approval.
- `staging` with approval executes.

Use a temp file path for the artifact and assert it contains `"operation":"migration_repair_dirty"`.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestDestructiveGate -count=1`

Expected: FAIL because helper is undefined.

**Step 3: Implement gate helper**

Add:

```go
type destructiveDecision struct {
    Operation string `json:"operation"`
    Env string `json:"env"`
    Resource string `json:"resource,omitempty"`
    RequiresApproval bool `json:"requires_approval"`
}

func requireDestructiveApproval(envName, operation, resource string, approved bool, artifactPath string) error
```

Rules:

- `envName` in `dev`, `local`, `test` does not require approval.
- Any other env requires approval unless `approved == true`.
- If approval is missing and `artifactPath != ""`, write the JSON decision before returning an error that includes `approval required`.

**Step 4: Run tests**

Run: `GOWORK=off go test ./cmd/wfctl -run TestDestructiveGate -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/destructive_gate.go cmd/wfctl/destructive_gate_test.go
git commit -m "feat(wfctl): add destructive operation approval gate"
```

### Task 3: Extend Provider Remote Dispatch

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go`
- Test: `cmd/wfctl/deploy_providers_remote_iac_test.go`
- Modify: `plugin/grpc` files if strict proto service dispatch requires a method entry

**Step 1: Write failing remote dispatch test**

In the remote IaC provider tests, create a fake invoker that captures:

```go
method == "IaCProvider.RepairDirtyMigration"
args["request"].(map[string]any)["expected_dirty_version"] == "20260426000005"
```

Call `remoteIaCProvider.RepairDirtyMigration(ctx, interfaces.MigrationRepairRequest{...})`.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRemoteIaCProvider_RepairDirtyMigration -count=1`

Expected: FAIL because method is undefined.

**Step 3: Implement remote method**

Add method to `remoteIaCProvider`:

```go
func (r *remoteIaCProvider) RepairDirtyMigration(ctx context.Context, req interfaces.MigrationRepairRequest) (*interfaces.MigrationRepairResult, error)
```

Use `jsonToAny(req)`, invoke `"IaCProvider.RepairDirtyMigration"`, decode into `MigrationRepairResult`.

If strict proto dispatch requires allowlisting, add it in the same task with a test that unregistered methods still fail.

**Step 4: Run tests**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRemoteIaCProvider_RepairDirtyMigration|TestRemoteIaCProvider' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_remote_iac_test.go plugin/grpc
git commit -m "feat(wfctl): dispatch provider migration repair calls"
```

### Task 4: Implement `wfctl migrate repair-dirty`

**Files:**
- Modify: `cmd/wfctl/migrate.go`
- Create: `cmd/wfctl/migrate_repair.go`
- Test: `cmd/wfctl/migrate_repair_test.go`

**Step 1: Write failing CLI tests**

Use temp `infra.yaml` with:

- `iac.provider` named `do-provider`
- `infra.database` named `bmw-database`, env override `bmw-staging-db`
- `infra.container_service` named `bmw-app`, env override `bmw-staging`

Inject a fake provider through `resolveIaCProvider` that implements `ProviderMigrationRepairer`.

Test:

```go
err := runMigrate([]string{
  "repair-dirty",
  "--config", cfgPath,
  "--env", "staging",
  "--database", "bmw-database",
  "--app", "bmw-app",
  "--job-image", "registry.example/workflow-migrate:sha",
  "--expected-dirty-version", "20260426000005",
  "--force-version", "20260426000004",
  "--then-up",
  "--confirm-force", "FORCE_MIGRATION_METADATA",
  "--approve-destructive",
})
```

Assert the fake provider received `AppResourceName == "bmw-staging"` and `DatabaseResourceName == "bmw-staging-db"`.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirty -count=1`

Expected: FAIL because subcommand is unknown.

**Step 3: Implement CLI**

Add `repair-dirty` branch before SQLite DB opening, like `plugins`.

Flags:

- `--config`
- `--env`
- `--database`
- `--app`
- `--job-image`
- `--source-dir` default `/migrations`
- `--expected-dirty-version`
- `--force-version`
- `--then-up`
- `--up-if-clean`
- `--confirm-force`
- `--approve-destructive`
- `--approval-artifact`
- `--timeout` default `10m`

Implementation must:

1. Parse env-resolved infra modules using existing helpers.
2. Find app/database modules by base or env-resolved name.
3. Validate both reference the same provider.
4. Load that provider via `resolveIaCProvider`.
5. Type-assert `interfaces.ProviderMigrationRepairer`.
6. Run the destructive gate.
7. Call provider and print `provider job <id>: <status>`.
8. Print logs if returned.

**Step 4: Run focused tests**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirty -count=1`

Expected: PASS.

**Step 5: Run representative CLI help**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirtyHelp -count=1`

Expected: PASS and help output contains `--expected-dirty-version`, `--force-version`, and `--approve-destructive`.

**Step 6: Commit**

```bash
git add cmd/wfctl/migrate.go cmd/wfctl/migrate_repair.go cmd/wfctl/migrate_repair_test.go
git commit -m "feat(wfctl): add provider-executed migration repair command"
```

### Task 5: Document DigitalOcean Provider Contract

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `docs/manual/build-deploy/03-ci-deploy-environments.md`
- Create: `docs/plans/2026-04-27-wfctl-provider-migration-repair-bmw.md`

**Step 1: Update docs**

Document:

- why repair runs through provider-managed jobs
- required guard flags
- GitHub Actions environment gating example
- BMW staging example command

**Step 2: Verify docs render paths and command names**

Run: `rg -n "migrate repair-dirty|approve-destructive|FORCE_MIGRATION_METADATA" docs cmd/wfctl`

Expected: At least one match in `docs/WFCTL.md`, one in manual docs, one in CLI code/tests.

**Step 3: Commit**

```bash
git add docs/WFCTL.md docs/manual/build-deploy/03-ci-deploy-environments.md docs/plans/2026-04-27-wfctl-provider-migration-repair-bmw.md
git commit -m "docs(wfctl): document provider-executed migration repair"
```

### Task 6: Platform Integration Verification

**Files:**
- No source changes unless prior tasks reveal gaps.

**Step 1: Run package tests**

Run: `GOWORK=off go test ./interfaces ./cmd/wfctl -count=1`

Expected: PASS.

**Step 2: Run representative full wfctl test slice**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrateRepairDirty|TestRemoteIaCProvider_RepairDirtyMigration|TestDestructiveGate|TestInfraApply_EnvFlagResolvesOverrides' -count=1`

Expected: PASS.

**Step 3: Run CLI help manually**

Run: `GOWORK=off go run ./cmd/wfctl migrate repair-dirty --help`

Expected: exit 0 and help shows `--expected-dirty-version`, `--force-version`, `--confirm-force`, and `--approve-destructive`.

**Step 4: Commit only if verification fixes were needed**

If no fixes were needed, do not create an empty commit.

### Task 7: Hand Off DigitalOcean Plugin Implementation

**Files:**
- No edits in this Workflow repo.

**Step 1: Create follow-up implementation issue or plan in `workflow-plugin-digitalocean`**

The DO plugin must implement `ProviderMigrationRepairer` after the Workflow interface lands. The implementation plan should cover App Platform job/deployment mechanics, log collection, and restoration if temporary app spec mutation is required.

**Step 2: Verify issue/plan reference**

Run: `rg -n "ProviderMigrationRepairer|RepairDirtyMigration|migration repair" docs/plans`

Expected: the follow-up plan exists and references the Workflow PR/commit.

