# wfctl Provider-Executed Migration Repair Implementation Plan

> **Implementation note:** Execute this plan task-by-task and validate each step before proceeding.

**Goal:** Add `wfctl migrate repair-dirty` so guarded dirty migration repairs run inside provider-managed trusted runtime boundaries instead of from CI runners.

**Architecture:** wfctl resolves env-aware app/database infra modules, applies destructive-operation gating, and calls an optional provider interface. The first provider implementation target is DigitalOcean App Platform via `workflow-plugin-digitalocean`; Workflow core owns the CLI, request/result types, remote dispatch plumbing, CI summaries, and tests. The DigitalOcean plugin owns App Platform job/spec mechanics and diagnostics. BMW owns adoption/removal of temporary app-config repair once the platform path exists.

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

- `staging` without `--approve-destructive` returns approval-required and writes JSON to an explicit artifact path.
- `staging` without `--approve-destructive` and without an explicit artifact writes the default local artifact `wfctl-destructive-approval.json`.
- `prod` without approval returns approval-required and writes the default artifact.
- `GITHUB_ACTIONS=true` + `RUNNER_TEMP=<tmp>` defaults the artifact to `<tmp>/wfctl-destructive-approval.json`.
- approval artifact JSON includes `operation`, `env`, `app`, `database`, `expected_dirty_version`, `force_version`, and `requires_approval`.
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
    App string `json:"app,omitempty"`
    Database string `json:"database,omitempty"`
    ExpectedDirtyVersion string `json:"expected_dirty_version,omitempty"`
    ForceVersion string `json:"force_version,omitempty"`
    RequiresApproval bool `json:"requires_approval"`
}

func requireDestructiveApproval(decision destructiveDecision, approved bool, artifactPath string) (*interfaces.MigrationRepairResult, error)
```

Rules:

- `envName` in `dev`, `local`, `test` does not require approval.
- Any other env requires approval unless `approved == true`.
- If approval is missing, write the JSON decision before returning `MigrationRepairResult{Status: "approval_required"}` and an error that includes `approval required`.
- If `artifactPath == ""` and `GITHUB_ACTIONS=true` with `RUNNER_TEMP` set, use `$RUNNER_TEMP/wfctl-destructive-approval.json`.
- Otherwise use `wfctl-destructive-approval.json` in the current working directory.

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
8. Print diagnostics and logs if returned.
9. Write a GitHub step summary when `GITHUB_STEP_SUMMARY` is set. The summary must include operation, environment, approval status, provider job/deployment IDs, terminal status, and log tail.

**Step 4: Run focused tests**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirty -count=1`

Expected: PASS.

**Step 5: Run representative CLI help**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirtyHelp -count=1`

Expected: PASS and help output contains `--expected-dirty-version`, `--force-version`, and `--approve-destructive`.

**Step 6: Add step-summary tests**

Add `TestRunMigrateRepairDirtyWritesGitHubStepSummary` with temp
`GITHUB_STEP_SUMMARY`. Assert the summary includes:

- `migration_repair_dirty`
- selected environment
- provider job/deployment ID
- terminal status
- log tail

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrateRepairDirtyWritesGitHubStepSummary -count=1`

Expected: PASS.

**Step 7: Add env propagation and structured status tests**

Add tests covering:

- `--job-env FOO=bar` places `Env["FOO"] == "bar"` in `MigrationRepairRequest`.
- `--job-env-from-env DATABASE_URL` reads the process env and places the value in `Env`.
- missing env for `--job-env-from-env` fails before provider invocation.
- provider missing `ProviderMigrationRepairer` returns/prints status `unsupported`.
- missing approval returns/prints status `approval_required` and does not call provider.

Implementation adds flags:

- `--job-env KEY=VALUE`, repeatable.
- `--job-env-from-env KEY`, repeatable.

wfctl must redact request env values from stdout, stderr, errors, logs, and
GitHub step summary.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrateRepairDirty.*(Env|Unsupported|Approval)' -count=1`

Expected: PASS.

**Step 8: Commit**

```bash
git add cmd/wfctl/migrate.go cmd/wfctl/migrate_repair.go cmd/wfctl/migrate_repair_test.go
git commit -m "feat(wfctl): add provider-executed migration repair command"
```

### Task 5: Document Workflow Provider Contract

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `docs/manual/build-deploy/03-ci-deploy-environments.md`

**Step 1: Update docs**

Document:

- why repair runs through provider-managed jobs
- required guard flags
- GitHub Actions environment gating example
- generic env-aware app/database example command
- default approval artifact and GitHub step summary behavior

**Step 2: Verify docs render paths and command names**

Run: `rg -n "migrate repair-dirty|approve-destructive|FORCE_MIGRATION_METADATA" docs cmd/wfctl`

Expected: At least one match in `docs/WFCTL.md`, one in manual docs, one in CLI code/tests.

**Step 3: Commit**

```bash
git add docs/WFCTL.md docs/manual/build-deploy/03-ci-deploy-environments.md
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

### Task 7: Implement DigitalOcean Provider Capability

**Repository:** `/Users/jon/workspace/workflow-plugin-digitalocean`

**Files:**
- Modify: `internal/module_instance.go`
- Create: `internal/provider_migration_repair.go`
- Test: `internal/provider_migration_repair_test.go`
- Modify: `internal/grpc_dispatch_test.go`
- Modify: `internal/drivers/app_platform.go` or create an app-job helper if the existing driver boundary is a better fit.
- Test: `internal/drivers/app_platform_migration_repair_test.go`

**Step 1: Create an isolated plugin worktree after the Workflow interface lands**

Use a branch like `feat/provider-migration-repair`. Do not edit the dirty root
checkout. Update `go.mod` to the Workflow commit/release containing
`ProviderMigrationRepairer` or use a temporary local `replace` only during
development.

**Step 2: Write failing dispatch tests**

Add a test proving `InvokeMethod("IaCProvider.RepairDirtyMigration", ...)`
decodes `request`, calls a fake provider implementing the optional interface,
and encodes `MigrationRepairResult`.

Run: `GOWORK=off go test ./internal -run TestGRPCDispatch_IaCProvider_RepairDirtyMigration -count=1`

Expected: FAIL before dispatch support exists.

**Step 3: Implement `InvokeMethod` dispatch**

Add `IaCProvider.RepairDirtyMigration` to the switch and return a clear
unsupported error when the provider does not implement
`interfaces.ProviderMigrationRepairer`.

Run the focused dispatch test until PASS.

**Step 4: Write failing App Platform command/spec tests**

Tests must prove the provider:

- resolves an App Platform app by resource/app name
- builds `/workflow-migrate repair-dirty` with source dir, expected dirty version, force version, confirmation, and `--then-up`/`--up-if-clean`
- injects caller env without logging secret values
- adds a generated one-shot job name that cannot collide with declared jobs
- uses official App Platform job/spec behavior: app spec update plus deployment/job invocation polling if a true ad hoc job endpoint is unavailable
- restores the previous app spec after terminal success or failure when it mutated spec
- returns app ID, deployment ID, job invocation ID when available, terminal status, diagnostics, and log tail

Run: `GOWORK=off go test ./internal ./internal/drivers -run 'RepairDirtyMigration|MigrationRepair' -count=1`

Expected: FAIL before implementation.

**Step 5: Implement App Platform repair**

Use existing App Platform client abstractions where possible. If godo exposes
job invocations/log APIs, add them to the narrow client interface. If not, add a
small HTTP client adapter for the documented endpoints while keeping it behind
the driver interface for tests.

Implementation rules:

- Never open database trusted sources.
- Never print env secret values.
- Treat app-spec mutation as destructive and return diagnostics if restore fails.
- Poll with context timeout and return the final known deployment/job state on timeout.
- Prefer job invocation logs; fall back to deployment diagnostics/log tail when invocation logs are unavailable.

**Step 6: Run plugin tests**

Run: `GOWORK=off go test ./internal ./internal/drivers -count=1`

Expected: PASS.

**Step 7: Commit plugin changes**

```bash
git add internal go.mod go.sum
git commit -m "feat(do): run migration repair via App Platform jobs"
```

### Task 8: Release Workflow Core

**Repository:** `/Users/jon/workspace/workflow`

**Files:**
- Release metadata according to the existing Workflow release process.

**Step 1: Merge Workflow implementation PR**

After code review, Copilot comments, and CI are clean, admin squash merge the
Workflow PR.

**Step 2: Generate and verify release**

Use the repository's existing release process to tag a new Workflow/wfctl
version containing `ProviderMigrationRepairer` and `wfctl migrate repair-dirty`.

Verify:

- GitHub release workflow succeeds.
- `setup-wfctl` can install the new version.
- `wfctl migrate repair-dirty --help` works from the released binary.

### Task 9: Release DigitalOcean Plugin

**Repository:** `/Users/jon/workspace/workflow-plugin-digitalocean`

**Step 1: Merge plugin implementation PR**

After Workflow release is available, update the plugin dependency to that
version, ensure CI is green, resolve comments, and admin squash merge.

**Step 2: Generate plugin release**

Use the plugin's existing release process to publish a version containing
`IaCProvider.RepairDirtyMigration`.

Verify the plugin registry metadata and platform lockfile checksums reference
the released artifacts, not local or platform-specific top-level hashes.

### Task 10: BMW Adoption Plan

**Repository:** `/Users/jon/workspace/buymywishlist`

**Files:**
- Modify: `.github/workflows/migration-repair.yml`
- Modify: `infra.yaml`
- Modify: docs under `docs/ops/` if present
- Test: existing config/migration tests

**Step 1: Bump released dependencies**

Update:

- setup-wfctl/workflow version pins to the Workflow release from Task 8.
- workflow-plugin-digitalocean registry/lockfile pins to the plugin release from Task 9.
- Any app/deploy image base references that still point at older alpha labels.

Run `wfctl plugin lock` or the repo's lockfile refresh command so BMW consumes
release artifacts through lockfiles rather than explicit GitHub workflow plugin
installs.

**Step 2: Replace runner-side DB repair**

After Workflow and DigitalOcean plugin releases are available, update the BMW
migration repair workflow to call:

```bash
wfctl migrate repair-dirty --env staging --config infra.yaml \
  --database bmw-database \
  --app bmw-app \
  --job-image "registry.digitalocean.com/bmw-registry/workflow-migrate:${IMAGE_SHA}" \
  --expected-dirty-version 20260426000005 \
  --force-version 20260422000001 \
  --then-up \
  --confirm-force FORCE_MIGRATION_METADATA \
  --approve-destructive
```

Use GitHub environment gating for staging/prod approval. Keep force-version
selection explicit; it must be before the earliest missing prerequisite that
needs replay.

**Step 3: Remove temporary pre-deploy repair after staging is clean**

Once one provider-executed repair succeeds and normal deploy migration has run,
remove the temporary staging `run_command` from `infra.yaml` so the image CMD
returns to normal `workflow-migrate up --source-dir /migrations`.

**Step 4: Verify BMW**

Run:

- `GOWORK=off go test ./bmwplugin/...`
- `wfctl validate --skip-unknown-types app.yaml`
- GitHub staging deploy, then prod deploy.
