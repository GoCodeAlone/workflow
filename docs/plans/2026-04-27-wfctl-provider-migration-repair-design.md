---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
external_refs:
  - https://docs.digitalocean.com/products/app-platform/how-to/manage-jobs/
  - https://docs.digitalocean.com/products/app-platform/reference/api/
verification:
  last_checked:
  commands: []
  result:
supersedes: []
superseded_by: []
---

# wfctl Provider-Executed Migration Repair — Design

**Status:** Approved for autonomous design/plan pipeline, 2026-04-27

**Goal:** Add a dogfooded `wfctl migrate repair-dirty` path that can repair a known dirty migration state from inside the provider trust boundary, without exposing managed databases to GitHub-hosted runners or ad hoc operator SQL.

**Why:** BMW staging hit a dirty migration version (`20260426000005`). The existing GitHub Actions repair workflow had the correct guard logic, but it timed out connecting to the DigitalOcean managed database because the database correctly trusts only the App Platform app. Running repair from the CI runner would require opening database ingress to public runner IPs; running it as an App Platform job keeps database access in the same boundary as normal deploy-time migrations.

---

## Requirements

1. **Provider-trusted execution:** migration repair commands run from a provider workload that already has database access, such as a DigitalOcean App Platform job trusted by managed-DB firewall rules.
2. **Guarded destructive metadata changes:** dirty repair requires the expected dirty version, the force target version, and a typed confirmation. Non-dev environments must have a human-gate mode available before execution.
3. **Portable wfctl surface:** users invoke a wfctl command, not provider-specific scripts. Provider-specific details live behind plugin interfaces.
4. **Immediate BMW path:** BMW can repair staging dirty version `20260426000005`, continue migrations, and then return to normal `migrate up`.
5. **No broad DB ingress workaround:** do not add GitHub runner IPs or `0.0.0.0/0` trusted database sources as the default answer.
6. **Auditable output:** command output includes the provider job/deployment ID, status, log tail, and a GitHub step summary when running in CI.

## Recommended Approach

Introduce a provider-executed operation behind wfctl:

```sh
wfctl migrate repair-dirty --env staging --config infra.yaml \
  --database bmw-database \
  --app bmw-app \
  --job-image registry.digitalocean.com/bmw-registry/workflow-migrate:${IMAGE_SHA} \
  --expected-dirty-version 20260426000005 \
  --force-version <known-safe-version-before-replay> \
  --then-up \
  --confirm-force FORCE_MIGRATION_METADATA
```

wfctl resolves the env-aware infra config and loads the relevant IaC provider. It asks the provider to run a one-shot migration job using the target app/container service as the network and secret boundary. On DigitalOcean, the plugin updates or creates a temporary App Platform job/deployment using the existing app spec shape and polls it to terminal status, returning logs and phase diagnostics.

This keeps wfctl as the lifecycle orchestrator while letting the DigitalOcean plugin own App Platform mechanics. Other providers can later implement the same interface with ECS one-off tasks, Cloud Run jobs, Kubernetes jobs, or Azure Container Apps jobs.

## Alternatives Considered

### A. Open database access to GitHub Actions

Fast for BMW, but weak security. GitHub-hosted runner IP ranges are large and change, and allowing public ingress undermines the current `trusted_sources: type: app` model. Reject as default.

### B. Keep temporary pre-deploy repair in app config only

This works immediately when a deploy succeeds far enough for the pre-deploy job to run. It is not ergonomic or auditable: operators must edit app config, merge, deploy, then remember to revert. Keep it as an emergency fallback, not the platform answer.

### C. Provider-executed wfctl migration operation

Recommended. It is secure by default, portable by interface, and dogfoods Workflow/wfctl. It also creates a reusable foundation for other guarded one-shot operational actions.

## Architecture

### Core Interfaces

Add an optional provider capability in `interfaces`:

```go
type MigrationRepairRequest struct {
    AppResourceName       string            `json:"app_resource_name"`
    DatabaseResourceName  string            `json:"database_resource_name"`
    JobImage              string            `json:"job_image"`
    SourceDir             string            `json:"source_dir"`
    ExpectedDirtyVersion  string            `json:"expected_dirty_version"`
    ForceVersion          string            `json:"force_version"`
    ThenUp                bool              `json:"then_up"`
    UpIfClean             bool              `json:"up_if_clean"`
    ConfirmForce          string            `json:"confirm_force"`
    Env                   map[string]string `json:"env,omitempty"`
    TimeoutSeconds        int               `json:"timeout_seconds,omitempty"`
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

The interface is optional. wfctl probes for it over the same plugin service dispatch pattern used by other optional provider capabilities.

### wfctl Command

Extend `wfctl migrate` with a provider-aware branch:

```text
wfctl migrate repair-dirty --config infra.yaml --env staging --database bmw-database --app bmw-app ...
```

The command:

1. Resolves `infra.yaml` for the selected env.
2. Loads the `iac.provider` referenced by the app/database resources.
3. Validates that the database and app use the same provider.
4. Requires `--confirm-force FORCE_MIGRATION_METADATA` for metadata repair.
5. For non-dev envs, emits a destructive-operation decision artifact and can require an explicit `--approve-destructive` flag or CI environment gate before execution.
6. Calls `ProviderMigrationRepairer.RepairDirtyMigration`.
7. Emits logs, diagnostics, and step summary.

### DigitalOcean Plugin

DigitalOcean implements `ProviderMigrationRepairer` by using App Platform jobs.
The official App Platform docs describe jobs as app-spec components that can run
before or after deployments, and describe adding a job through `doctl apps
update` or the `PUT /v2/apps/{id}` API. They also expose job invocations and
logs through App Platform job-invocation APIs. The plugin should use those
provider-native surfaces rather than opening the database to the caller.

1. Resolve the App Platform app by env-resolved resource name (`bmw-staging`).
2. Resolve the database URI from provider output or caller-provided env secret.
3. Build a job command:

```sh
/workflow-migrate repair-dirty \
  --source-dir /migrations \
  --expected-dirty-version <version> \
  --force-version <version> \
  --confirm-force FORCE_MIGRATION_METADATA \
  --then-up
```

4. Create a temporary App Platform deployment/job using the app's existing network boundary and env vars.
5. Poll until the job succeeds or fails.
6. Return deployment/job ID, log tail, and diagnostics.

If DigitalOcean cannot run a true ad hoc job without changing app spec, the first implementation may apply a temporary `POST_DEPLOY` or `PRE_DEPLOY` job mutation with a generated job name, deploy it, collect the result, and restore the previous app spec. That path must be treated as a destructive provider operation because it mutates app spec, even if temporary.

The provider result must distinguish:

- `succeeded`: repair command exited zero.
- `failed`: provider job reached a terminal failed state or exited non-zero.
- `approval_required`: wfctl stopped before provider invocation.
- `unsupported`: provider plugin does not implement the capability.

The result diagnostics must include the provider app ID, deployment ID when
available, job invocation ID when available, terminal phase, component name, and
a log tail suitable for CI summaries.

## Human Gate

For `staging`, the command may run with `--approve-destructive` from a GitHub environment-gated job. For `prod`, wfctl should default to requiring a CI approval artifact:

```json
{
  "operation": "migration_repair_dirty",
  "env": "prod",
  "database": "bmw-database",
  "app": "bmw-app",
  "expected_dirty_version": "20260426000005",
  "force_version": "20260426000004",
  "requires_approval": true
}
```

In GitHub Actions, BMW can bind that job to an environment with required reviewers. In other CI systems, wfctl still emits the same artifact and exits with a clear "approval required" status unless `--approve-destructive` is present. If the caller omits `--approval-artifact`, wfctl writes a default JSON artifact to `$RUNNER_TEMP/wfctl-destructive-approval.json` when running under GitHub Actions, otherwise `./wfctl-destructive-approval.json`.

The CLI must also be able to pass provider runtime environment values into the
job request without hardcoding a CI provider. It accepts repeatable
`--job-env KEY=VALUE` flags and `--job-env-from-env KEY` flags. `--job-env` is
for non-secret values that are safe in shell history; `--job-env-from-env`
reads a local environment variable and stores only the key/value in the
in-memory request. wfctl must redact these values in all logs and summaries.

wfctl also writes a GitHub step summary when `GITHUB_STEP_SUMMARY` is set. The
summary must be terse and operator-oriented: operation, environment, approval
state, provider app/job/deployment IDs, terminal status, and the final log tail.

If wfctl stops before provider execution, it should still produce a structured
result/status for automation:

- Missing human approval: status `approval_required`, exit non-zero, artifact written.
- Provider does not implement repair capability: status `unsupported`, exit non-zero.
- Provider runs and fails: status `failed`, exit non-zero with diagnostics.
- Provider runs and succeeds: status `succeeded`, exit zero.

## BMW Adoption

BMW keeps the current temporary pre-deploy repair until staging is clean. The
safe force target is the newest migration version before any missing prerequisite
that must be replayed; for example, if the dirty migration alters a table that
should have been created by an earlier idempotent migration, the force target
must be before that table-creation migration. After the platform feature ships:

1. Replace `.github/workflows/migration-repair.yml` runner-side `psql` with `wfctl migrate repair-dirty`.
2. Gate the repair job with the `staging` environment.
3. Remove the temporary staging `infra.yaml` repair `run_command` after one successful repair and deploy.
4. Keep normal pre-deploy migrations as `workflow-migrate up --source-dir /migrations`.

Shipping this through BMW requires a cross-repo release sequence:

1. Merge Workflow core support and release a new Workflow/wfctl version.
2. Update and release `workflow-plugin-digitalocean` against that Workflow interface.
3. Update BMW setup-wfctl, workflow server image, plugin registry/lockfile, and any workflow-plugin-digitalocean pin to consume the releases.
4. Merge BMW adoption and verify staging then prod deploy.

## Acceptance Criteria

- Running `wfctl migrate repair-dirty --help` documents the required guard flags and provider execution behavior.
- A fake provider test proves wfctl resolves app/database modules for an env and calls `RepairDirtyMigration` with the expected request.
- A non-dev destructive run without approval emits an approval artifact at the default path and does not call the provider.
- wfctl writes a GitHub step summary when `GITHUB_STEP_SUMMARY` is set.
- DigitalOcean unit tests prove the provider builds the expected repair command, mutates/restores app spec only when necessary, and returns job logs/diagnostics.
- BMW staging can repair dirty version `20260426000005` without opening database ingress to GitHub-hosted runners.
