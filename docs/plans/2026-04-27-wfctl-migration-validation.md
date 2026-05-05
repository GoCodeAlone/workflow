---
title: wfctl Migration Validation Implementation Plan
status: in_progress
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    pr: 504
external_refs: []
verification:
  last_checked: 2026-04-27
  commands:
    - 'GOWORK=off go test ./cmd/wfctl ./config -run "TestRunMigrations|TestResolveMigration|TestExtractTar|TestGenerateGHA|TestGenerateGitLab|TestRunCIRunDeploy|TestRunDeployCloud|TestCIConfig" -count=1'
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Migration Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a portable `wfctl migrations` command family that validates application database migrations, checks deploy safety, and delegates repair to migration plugins without embedding migration drivers in wfctl.

**Architecture:** `wfctl` owns config discovery, environment/secret resolution, CI-facing decisions, and JSON/text output. `workflow-plugin-migrations` keeps owning migration drivers and executes via its existing CLI surface (`migrate lint`, `migrate test`, `migrate status`, `migrate repair-dirty`) through an internal plugin CLI runner. Initial implementation supports one or more configured migration sources and keeps CI wrappers thin.

**Tech Stack:** Go `flag` CLI in `cmd/wfctl`, existing Workflow config loading, existing plugin manifest/install conventions, JSON output structs, `workflow-plugin-migrations` CLI contract.

---

### Task 1: Add Migration CI Config Model

**Files:**
- Modify: `config/ci_config.go`
- Test: `config/ci_config_test.go`

**Step 1: Write the failing test**

Add `TestCIMigrationsConfigParsesValidationOptions`:

```go
func TestCIMigrationsConfigParsesValidationOptions(t *testing.T) {
	data := []byte(`
version: 1
ci:
  migrations:
    - name: app
      plugin: workflow-plugin-migrations
      driver: golang-migrate
      source_dir: migrations
      database:
        env: DATABASE_URL
      baseline:
        ref: origin/main
        mode: apply-before-candidate
      validation:
        lint: true
        fresh_cycle: true
        baseline_candidate: true
        forbid_dirty: true
`)
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	got := cfg.CI.Migrations[0]
	if got.Name != "app" || got.Plugin != "workflow-plugin-migrations" || got.Driver != "golang-migrate" {
		t.Fatalf("unexpected migration config: %+v", got)
	}
	if got.Database.Env != "DATABASE_URL" {
		t.Fatalf("database env = %q", got.Database.Env)
	}
	if !got.Validation.FreshCycle || !got.Validation.BaselineCandidate || !got.Validation.ForbidDirty {
		t.Fatalf("validation flags not parsed: %+v", got.Validation)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./config -run TestCIMigrationsConfigParsesValidationOptions -count=1`

Expected: FAIL because `CIConfig` has no `Migrations` field.

**Step 3: Implement config structs**

Add structs:

```go
type CIMigrationConfig struct {
	Name       string                       `yaml:"name" json:"name"`
	Plugin     string                       `yaml:"plugin" json:"plugin"`
	Driver     string                       `yaml:"driver" json:"driver"`
	SourceDir  string                       `yaml:"source_dir" json:"source_dir"`
	Database   CIMigrationDatabaseConfig    `yaml:"database" json:"database"`
	Baseline   CIMigrationBaselineConfig    `yaml:"baseline" json:"baseline"`
	Validation CIMigrationValidationConfig  `yaml:"validation" json:"validation"`
}
```

Add `Migrations []CIMigrationConfig` to `CIConfig`.

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./config -run TestCIMigrationsConfigParsesValidationOptions -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add config/ci_config.go config/ci_config_test.go
git commit -m "feat(config): add migration validation ci config"
```

### Task 2: Add Migration Config Resolver

**Files:**
- Create: `cmd/wfctl/migrations_config.go`
- Test: `cmd/wfctl/migrations_config_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestResolveMigrationConfigsDefaultsPluginAndDriver`
- `TestResolveMigrationConfigsReadsDSNFromEnvWithoutLoggingValue`
- `TestResolveMigrationConfigsRejectsMissingSourceDir`

Representative test:

```go
func TestResolveMigrationConfigsDefaultsPluginAndDriver(t *testing.T) {
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name: "app", SourceDir: "migrations", Database: config.CIMigrationDatabaseConfig{Env: "DATABASE_URL"},
	}}}}
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	got, err := resolveMigrationConfigs(cfg, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Plugin != "workflow-plugin-migrations" || got[0].Driver != "golang-migrate" {
		t.Fatalf("defaults not applied: %+v", got[0])
	}
	if got[0].DSN != "postgres://secret@example/db" {
		t.Fatal("dsn not resolved from env")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run TestResolveMigrationConfigs -count=1`

Expected: FAIL because resolver does not exist.

**Step 3: Implement resolver**

Create internal resolved type:

```go
type resolvedMigrationConfig struct {
	Name string
	Plugin string
	Driver string
	SourceDir string
	DSN string
	BaselineRef string
	BaselineMode string
	Validation config.CIMigrationValidationConfig
}
```

Rules:

- Default `Plugin` to `workflow-plugin-migrations`.
- Default `Driver` to `golang-migrate`.
- Require `Name`, `SourceDir`, and either `Database.Env` or `Database.DSN`.
- Resolve `Database.Env` through `os.Getenv`.
- Return missing secret errors that mention env var names, never values.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run TestResolveMigrationConfigs -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations_config.go cmd/wfctl/migrations_config_test.go
git commit -m "feat(wfctl): resolve migration validation config"
```

### Task 3: Add Plugin Migration CLI Runner

**Files:**
- Create: `cmd/wfctl/migrations_plugin_runner.go`
- Test: `cmd/wfctl/migrations_plugin_runner_test.go`

**Step 1: Write failing tests**

Add tests for:

- `TestMigrationPluginRunnerBuildsWorkflowMigrateArgs`
- `TestMigrationPluginRunnerRedactsDSNInErrors`

The runner should build invocations equivalent to:

```sh
<plugin-binary> --wfctl-cli migrate test --driver golang-migrate --source-dir migrations --dsn <secret>
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run TestMigrationPluginRunner -count=1`

Expected: FAIL because runner does not exist.

**Step 3: Implement runner with injectable executor**

Define:

```go
type migrationPluginRunner struct {
	exec func(ctx context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error)
}
```

The default executor may initially resolve plugin binary paths using existing installed-plugin conventions. Keep it small and test the argument builder independently.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run TestMigrationPluginRunner -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations_plugin_runner.go cmd/wfctl/migrations_plugin_runner_test.go
git commit -m "feat(wfctl): add migration plugin cli runner"
```

### Task 4: Add `wfctl migrations validate`

**Files:**
- Create: `cmd/wfctl/migrations.go`
- Modify: `cmd/wfctl/main.go`
- Modify: `cmd/wfctl/wfctl.yaml`
- Test: `cmd/wfctl/migrations_validate_test.go`

**Step 1: Write failing tests**

Add:

- `TestRunMigrationsValidateRunsLintAndFreshCycle`
- `TestRunMigrationsValidateJSONOutput`
- `TestRunMigrationsMissingSubcommand`

Expected command behavior:

```sh
wfctl migrations validate --config infra.yaml --env staging --format json
```

Returns JSON:

```json
{"decision":"pass","migrations":[{"name":"app","lint":"pass","fresh_cycle":"pass","dirty":false}]}
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrations(Validate|Missing)' -count=1`

Expected: FAIL because command is not registered.

**Step 3: Implement command**

Add `runMigrations(args []string) error` with subcommands. Register `"migrations": runMigrations` in `main.go` and `cmd-migrations` in `wfctl.yaml`.

For `validate`:

- Load config via the same path used by other wfctl commands.
- Resolve migration configs.
- For each config, run `migrate lint` if enabled.
- Run `migrate test` if `fresh_cycle` is enabled.
- Write a validation result artifact when `--result-file <path>` is provided. Include `commit`, `decision`, `migrations[]`, and per-check status.
- Output text by default and JSON when `--format json`.
- Redact DSN from all errors and output.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrations(Validate|Missing)' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations.go cmd/wfctl/migrations_validate_test.go cmd/wfctl/main.go cmd/wfctl/wfctl.yaml
git commit -m "feat(wfctl): add migrations validate command"
```

### Task 5: Add Baseline/Candidate Migration Validation

**Files:**
- Modify: `cmd/wfctl/migrations.go`
- Create: `cmd/wfctl/migrations_baseline.go`
- Test: `cmd/wfctl/migrations_baseline_test.go`

**Step 1: Write failing tests**

Add:

- `TestRunMigrationsValidateAppliesBaselineBeforeCandidate`
- `TestRunMigrationsValidateDetectsChangedMigrationSources`
- `TestRunMigrationsValidateSkipsBaselineWhenDisabled`
- `TestRunMigrationsValidateRecordsResultFileForCommit`

Use an injectable migration runner and git materializer so the test can assert this ordered call sequence without shelling out:

```text
discover changed source migrations between origin/main and HEAD
lint candidate
materialize baseline ref origin/main
test keep-alive/apply baseline
materialize candidate ref HEAD
up candidate
status candidate clean
write result file with commit abc123 and decision pass
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrationsValidate.*Baseline -count=1`

Expected: FAIL because baseline/candidate orchestration does not exist.

**Step 3: Implement baseline/candidate orchestration**

When `validation.baseline_candidate` is true:

- Require or default `baseline.ref` to `origin/main`.
- Compare the candidate ref to `baseline.ref` and identify configured migration sources whose files changed. If no configured sources changed, still run `lint` when enabled but skip baseline/candidate database replay unless `--force-baseline-candidate` is set.
- Materialize the baseline migration source in a temporary directory.
- Apply baseline migrations to an ephemeral DB by invoking the migration plugin against the baseline source.
- Materialize the candidate source.
- Apply candidate migrations to the same DB.
- Run status and require `dirty=false` and no pending migrations when `forbid_dirty` is true.

Do not print DSNs. Clean up temp directories unless `--debug-keep-temp` is provided.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrationsValidate.*Baseline -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations.go cmd/wfctl/migrations_baseline.go cmd/wfctl/migrations_baseline_test.go
git commit -m "feat(wfctl): validate candidate migrations from baseline"
```

### Task 6: Add `wfctl migrations status` and `ci-check`

**Files:**
- Modify: `cmd/wfctl/migrations.go`
- Test: `cmd/wfctl/migrations_status_test.go`
- Test: `cmd/wfctl/migrations_ci_check_test.go`

**Step 1: Write failing tests**

Add:

- `TestRunMigrationsStatusReportsDirty`
- `TestRunMigrationsStatusReportsCurrentPendingDirtyAndDriver`
- `TestRunMigrationsCICheckFailsClosedOnDirty`
- `TestRunMigrationsCICheckRequiresPassingValidationResultForSHA`
- `TestRunMigrationsCICheckFailsClosedWhenPluginLoadFails`

Expected dirty JSON:

```json
{"decision":"fail","reasons":["migration app is dirty at version 20260426000005"],"destructive":false,"human_approval_required":false,"migrations":[{"name":"app","driver":"golang-migrate","current":"20260426000005","pending":[],"dirty":true}]}
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrations(Status|CICheck)' -count=1`

Expected: FAIL because subcommands do not exist.

**Step 3: Implement subcommands**

`status` delegates to `migrate status` and parses/normalizes result into wfctl output.

`ci-check`:

- Runs status.
- Fails if dirty when `forbid_dirty` is true.
- Accepts `--commit` and `--validation-result <path>`.
- When `--require-validation-result` is true, reads the JSON result artifact written by `validate` and fails unless `commit` matches and `decision == "pass"`.
- Accepts `--require-same-sha` as an alias for `--require-validation-result` plus commit matching, making the deploy-guard intent explicit.
- Fails closed when the migration plugin cannot be loaded or invoked.
- Emits JSON fields `decision`, `reasons`, `destructive`, `human_approval_required`.
- Status and ci-check output include each migration source's `name`, `driver`, `current`, `pending`, and `dirty`.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRunMigrations(Status|CICheck)' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations.go cmd/wfctl/migrations_status_test.go cmd/wfctl/migrations_ci_check_test.go
git commit -m "feat(wfctl): add migration deploy guard checks"
```

### Task 7: Add Guarded `repair-dirty`

**Files:**
- Modify: `cmd/wfctl/migrations.go`
- Test: `cmd/wfctl/migrations_repair_dirty_test.go`

**Step 1: Write failing tests**

Add:

- `TestRunMigrationsRepairDirtyRequiresConfirmation`
- `TestRunMigrationsRepairDirtyPassesExactVersionAndThenUp`
- `TestRunMigrationsRepairDirtyApprovalRequiredForProdWithoutApprovedToken`
- `TestRunMigrationsRepairDirtyPrintsPostRepairStatus`
- `TestRunMigrationsRepairDirtyTypedConfirmationRepairsDirtyState`

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrationsRepairDirty -count=1`

Expected: FAIL because subcommand does not exist.

**Step 3: Implement repair wrapper**

Delegate to plugin CLI:

```sh
migrate repair-dirty --driver <driver> --source-dir <dir> --dsn <dsn> \
  --expected-dirty-version <v> --force-version <v> \
  --confirm-force FORCE_MIGRATION_METADATA [--then-up|--up-if-clean]
```

Rules:

- Require confirmation always.
- Require exact dirty version and force version.
- For `prod`/`production`, return approval-required JSON unless `--approved-token` is present.
- Approval-required JSON includes the exact command to approve, with DSNs and secrets redacted.
- After approved repair execution, run `migrate status` and print current version plus dirty flag. Successful repair output must include `dirty: false`.
- Redact DSN from output and errors.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run TestRunMigrationsRepairDirty -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/migrations.go cmd/wfctl/migrations_repair_dirty_test.go
git commit -m "feat(wfctl): add guarded migration dirty repair"
```

### Task 8: Add `wfctl ci init` Wrappers

**Files:**
- Modify: `cmd/wfctl/ci_init.go`
- Test: `cmd/wfctl/ci_init_migrations_test.go`

**Step 1: Write failing tests**

Add:

- `TestCIInitEmitsMigrationsValidateWhenConfigured`
- `TestCIInitDeployUsesMigrationsCICheckBeforeDeploy`
- `TestCIInitEmitsManualRepairWorkflowWithProtectedEnvironment`

Expected generated GitHub Actions snippets:

```yaml
- run: wfctl migrations validate --config app.yaml --commit ${{ github.sha }} --result-file .wfctl/migrations-result.json --format json
```

and before deploy:

```yaml
- run: wfctl migrations ci-check --config app.yaml --env prod --commit ${{ github.event.workflow_run.head_sha || github.sha }} --validation-result .wfctl/migrations-result.json --require-validation-result --format json
```

When a non-dev environment is configured, emit an optional manual repair workflow with a protected GitHub environment:

```yaml
environment: prod
run: wfctl migrations repair-dirty --config app.yaml --env prod --expected-dirty-version "${{ inputs.expected_dirty_version }}" --force-version "${{ inputs.force_version }}" --confirm-force FORCE_MIGRATION_METADATA --approved-token github-environment
```

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./cmd/wfctl -run TestCIInit.*Migrations -count=1`

Expected: FAIL because generator does not emit migration wrappers.

**Step 3: Implement generator updates**

When `cfg.CI.Migrations` is non-empty:

- Add a migration validation job to CI output.
- Add `wfctl migrations ci-check` before deploy jobs.
- Keep generated deploy YAML platform-light and do not embed repair commands in automatic deploy.
- Emit a separate manually-triggered repair workflow for GitHub Actions when non-dev environments exist. The repair workflow must use the target GitHub environment so GitHub's UI handles human approval, while the actual command remains portable.

**Step 4: Run tests to verify they pass**

Run: `GOWORK=off go test ./cmd/wfctl -run TestCIInit.*Migrations -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/ci_init.go cmd/wfctl/ci_init_migrations_test.go
git commit -m "feat(wfctl): generate migration ci wrappers"
```

### Task 9: BMW Rollout Plan and Compatibility Fixtures

**Files:**
- Create: `docs/plans/2026-04-27-bmw-wfctl-migrations-rollout.md`
- Create: `cmd/wfctl/testdata/migrations/bmw-like-infra.yaml`
- Test: `cmd/wfctl/migrations_bmw_compat_test.go`

**Step 1: Write failing compatibility test**

Add `TestBMWMigrationGuardCanReplaceHandWrittenShell`:

```go
func TestBMWMigrationGuardCanReplaceHandWrittenShell(t *testing.T) {
	cfg := loadTestConfig(t, "testdata/migrations/bmw-like-infra.yaml")
	got, err := renderMigrationCICommands(cfg, "prod", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, got.Validate, "wfctl migrations validate")
	assertContains(t, got.Check, "wfctl migrations ci-check")
	assertNotContains(t, got.Validate, "workflow-migrate up")
	assertNotContains(t, got.Check, "gh run list")
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run TestBMWMigrationGuardCanReplaceHandWrittenShell -count=1`

Expected: FAIL because compatibility fixture/helper does not exist.

**Step 3: Add fixture and rollout doc**

Create a BMW-like fixture with one `ci.migrations[]` entry for `migrations`, `DATABASE_URL`, `origin/main`, `fresh_cycle`, `baseline_candidate`, and `forbid_dirty`.

Write the rollout doc with exact downstream steps:

1. Release workflow with `wfctl migrations`.
2. Bump BMW to that release.
3. Replace BMW hand-written migration validation shell with `wfctl migrations validate`.
4. Replace deploy guard shell with `wfctl migrations ci-check --require-same-sha`.
5. Keep the production repair workflow but change it to call `wfctl migrations repair-dirty`.
6. Verify BMW staging and prod deploys pass.

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./cmd/wfctl -run TestBMWMigrationGuardCanReplaceHandWrittenShell -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add docs/plans/2026-04-27-bmw-wfctl-migrations-rollout.md cmd/wfctl/testdata/migrations/bmw-like-infra.yaml cmd/wfctl/migrations_bmw_compat_test.go
git commit -m "docs: plan bmw migration guard rollout"
```

### Task 10: Other Workflow App Rollout Tracker

**Files:**
- Create: `docs/plans/2026-04-27-workflow-app-migration-guard-rollout.md`

**Step 1: Write tracker doc**

Create a short rollout tracker covering known Workflow apps:

- `buymywishlist`
- `workflow-dnd`
- `workflow-cardgame`
- `core-dump`

For each app, record whether migrations exist, whether `ci.migrations[]` should be added, and whether the app can use `wfctl migrations validate` immediately or needs plugin/config work first.

**Step 2: Verify tracker is not claiming implementation**

Run: `rg -n "status: implemented|implemented" docs/plans/2026-04-27-workflow-app-migration-guard-rollout.md`

Expected: no output.

**Step 3: Commit**

```bash
git add docs/plans/2026-04-27-workflow-app-migration-guard-rollout.md
git commit -m "docs: track workflow app migration guard rollout"
```

### Task 11: Documentation and End-to-End Verification

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `DOCUMENTATION.md`
- Create: `cmd/wfctl/testdata/migrations/infra.yaml`
- Create: `cmd/wfctl/testdata/migrations/migrations/20260427000001_create_example.up.sql`
- Create: `cmd/wfctl/testdata/migrations/migrations/20260427000001_create_example.down.sql`

**Step 1: Write docs**

Document:

- `wfctl migrations validate`
- `wfctl migrations status`
- `wfctl migrations ci-check`
- `wfctl migrations repair-dirty`
- Example `ci.migrations[]` config.

**Step 2: Run focused tests**

Run:

```bash
GOWORK=off go test ./config ./cmd/wfctl -run 'Test(CIMigrations|ResolveMigration|MigrationPlugin|RunMigrations|CIInit.*Migrations|.*Baseline.*)' -count=1
```

Expected: PASS.

**Step 3: Run broader verification**

Run:

```bash
GOWORK=off go test ./interfaces ./config ./cmd/wfctl -run 'Test(Migration|Migrations|CIInit|PluginCLI|PluginInstall|Deploy|Destructive)' -count=1
GOWORK=off go test ./cmd/wfctl -count=1
git diff --check
```

Expected: PASS.

**Step 4: Runtime launch validation**

Build and launch-help check:

```bash
GOWORK=off go build -o /tmp/wfctl-migrations ./cmd/wfctl
/tmp/wfctl-migrations migrations --help
/tmp/wfctl-migrations migrations validate --config cmd/wfctl/testdata/migrations/infra.yaml --env ci --format json
```

Expected: help output includes `validate`, `status`, `ci-check`, and `repair-dirty`; validate output includes `"decision":"pass"`, `"dirty":false`, and migration source name `"app"`.

**Step 5: Commit**

```bash
git add docs/WFCTL.md DOCUMENTATION.md cmd/wfctl/testdata/migrations
git commit -m "docs: document wfctl migration validation"
```

### Task 12: PR, Review, and Release Prep

**Files:**
- No new source files unless review requires changes.

**Step 1: Run final verification**

Run:

```bash
GOWORK=off go test ./interfaces ./config ./cmd/wfctl -run 'Test(Migration|Migrations|CIInit|PluginCLI|PluginInstall|Deploy|Destructive)' -count=1
GOWORK=off go test ./cmd/wfctl -count=1
GOWORK=off go build -o /tmp/wfctl-migrations ./cmd/wfctl
/tmp/wfctl-migrations migrations --help
git diff --check
```

Expected: all commands exit 0, help includes migrations subcommands.

**Step 2: Push branch and open PR**

```bash
git push -u origin design/wfctl-migration-validation
gh pr create --repo GoCodeAlone/workflow --base main --head design/wfctl-migration-validation --title "feat(wfctl): add migration validation orchestration" --body-file /tmp/wfctl-migration-validation-pr.md
```

**Step 3: Request review**

Use `superpowers:requesting-code-review` with an antagonistic reviewer focused on:

- Whether migration driver behavior leaked into wfctl.
- Whether secrets can appear in logs.
- Whether production repair can mutate without human approval.
- Whether generated CI remains portable.

**Step 4: Monitor PR**

Use `superpowers:pr-monitoring`: address Copilot and reviewer comments, wait for green checks, then admin/override merge when allowed.
