# Platform Maturity Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.18.0 core extensions (interfaces, hooks, dynamic CLI, tenancy, canonical IaC keys), plus matching plugin releases (migrations v0.1.0, digitalocean v0.7.0, supply-chain v0.3.0), and BMW adoption so BMW deploys end-to-end on a hardened base image with signed + SBOM'd artifacts, pre-deploy migrations, and tenant-aware request handling.

**Architecture:** Interfaces and orchestrators in workflow core; implementations in external plugins. Core emits build-pipeline hook events; plugins register handlers via `plugin.json` capability. Core exposes a dynamic CLI registry; plugins declare top-level subcommands. Migration drivers, supply-chain SBOM/sign, and DO IaC field coverage all live in their respective plugin repos with full feature ownership (code + docs + scaffolds). BMW consumes everything via `requires.plugins[]`.

**Tech Stack:** Go 1.26, `github.com/GoCodeAlone/modular`, `hashicorp/go-plugin`, `digitalocean/godo`, `anchore/syft`, `sigstore/cosign`, `golang-migrate/migrate/v4`, `pressly/goose/v3`, `ariga.io/atlas`, `fergusstrange/embedded-postgres`, `testcontainers-go`. Canonical base image: `gcr.io/distroless/base-debian12:nonroot`.

**Reference:** Design doc at `docs/plans/2026-04-22-platform-maturity-design.md`. Read it before starting.

---

## Pre-work: Setup

### Task 0: Confirm baseline

**Files:** none

**Step 1: Verify workflow main is green**

Run: `cd /Users/jon/workspace/workflow && git switch main && git pull --ff-only && GOWORK=off go test ./... -race -short 2>&1 | tail -5`
Expected: `ok` per package, no FAIL.

**Step 2: Verify BMW local build still works**

Run: `cd /Users/jon/workspace/buymywishlist && ls data/plugins/` — confirm previous session's local plugins still in place. If not, local verification happens later.

**Step 3: Create worktree for workflow work**

Run: `cd /Users/jon/workspace/workflow && git worktree add ../workflow-platform-maturity -b feat/platform-maturity-v0.18.0`

---

## Phase 1 — workflow v0.18.0: core extensions

All tasks happen inside `/Users/jon/workspace/workflow-platform-maturity`.

### Task 1: MigrationDriver interface

**Files:**
- Create: `interfaces/migration_driver.go`
- Create: `interfaces/migration_driver_test.go`

**Step 1: Write the failing test**

```go
// interfaces/migration_driver_test.go
package interfaces_test

import (
    "errors"
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

func TestMigrationRequest_Validate(t *testing.T) {
    tests := []struct{
        name string
        req  interfaces.MigrationRequest
        wantErr error
    }{
        {"missing DSN", interfaces.MigrationRequest{Source: interfaces.MigrationSource{Dir: "/m"}}, interfaces.ErrValidation},
        {"missing source", interfaces.MigrationRequest{DSN: "postgres://"}, interfaces.ErrValidation},
        {"ok", interfaces.MigrationRequest{DSN: "postgres://", Source: interfaces.MigrationSource{Dir: "/m"}}, nil},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            err := tc.req.Validate()
            if !errors.Is(err, tc.wantErr) {
                t.Errorf("got %v, want %v", err, tc.wantErr)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./interfaces/ -run TestMigrationRequest_Validate -v`
Expected: FAIL — undefined `MigrationRequest`.

**Step 3: Write minimal implementation**

```go
// interfaces/migration_driver.go
package interfaces

import (
    "context"
    "fmt"
    "time"
)

type MigrationDriver interface {
    Name() string
    Up(ctx context.Context, req MigrationRequest) (MigrationResult, error)
    Down(ctx context.Context, req MigrationRequest) (MigrationResult, error)
    Status(ctx context.Context, req MigrationRequest) (MigrationStatus, error)
    Goto(ctx context.Context, req MigrationRequest, target string) (MigrationResult, error)
}

type MigrationRequest struct {
    DSN     string
    Source  MigrationSource
    Options MigrationOptions
}

func (r MigrationRequest) Validate() error {
    if r.DSN == "" {
        return fmt.Errorf("%w: DSN is required", ErrValidation)
    }
    if r.Source.Dir == "" && len(r.Source.Files) == 0 {
        return fmt.Errorf("%w: source (dir or files) required", ErrValidation)
    }
    return nil
}

type MigrationSource struct {
    Dir        string
    Files      []string
    SchemaName string
}

type MigrationOptions struct {
    Steps   int
    DryRun  bool
    Timeout time.Duration
    Version string
}

type MigrationResult struct {
    Applied    []string
    Skipped    []string
    DurationMs int64
}

type MigrationStatus struct {
    Current string
    Pending []string
    Dirty   bool
}
```

**Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./interfaces/ -run TestMigrationRequest_Validate -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add interfaces/migration_driver.go interfaces/migration_driver_test.go
git commit -m "feat(interfaces): add MigrationDriver interface and request validation"
```

### Task 2: TenantResolver + Tenant types

**Files:**
- Create: `interfaces/tenant_resolver.go`
- Create: `interfaces/tenant_resolver_test.go`

**Step 1: Failing test**

```go
func TestTenant_IsZero(t *testing.T) {
    var z interfaces.Tenant
    if !z.IsZero() { t.Error("zero value not detected") }
    nz := interfaces.Tenant{ID: "abc"}
    if nz.IsZero() { t.Error("non-zero flagged as zero") }
}
```

**Step 2-4:** Implement `Tenant{ID,Name,Slug,Domains,Metadata,IsActive}`, `TenantResolver` interface, `Selector` interface, `IsZero()` method. Confirm test passes.

**Step 5: Commit** — `feat(interfaces): add TenantResolver, Selector, Tenant types`

### Task 3: TenantRegistry interface

**Files:**
- Create: `interfaces/tenant_registry.go`
- Create: `interfaces/tenant_registry_test.go`

**Step 1-4:** Add `TenantRegistry` interface (Ensure/GetByID/GetByDomain/GetBySlug/List/Update/Disable), `TenantSpec`, `TenantPatch`, `TenantFilter` types. One sanity test: `TenantSpec.Validate` catches empty slug + empty name.

**Step 5: Commit** — `feat(interfaces): add TenantRegistry interface`

### Task 4: Canonical IaC key constants

**Files:**
- Create: `interfaces/iac_canonical_keys.go`
- Create: `interfaces/iac_canonical_keys_test.go`

**Step 1: Failing test**

```go
func TestCanonicalKeys_AllPresent(t *testing.T) {
    required := []string{"name", "region", "image", "http_port", "instance_count",
        "size", "env_vars", "autoscaling", "routes", "health_check", "domains",
        "jobs", "workers", "static_sites", "sidecars", "provider_specific"}
    for _, k := range required {
        if !interfaces.IsCanonicalKey(k) {
            t.Errorf("canonical key %q missing", k)
        }
    }
}
```

**Step 2-4:** Define `const (KeyName = "name"; ...)` for every canonical key (see design doc §canonical IaC keys), plus `IsCanonicalKey(string) bool`, `CanonicalKeys() []string`. All keys from design.

**Step 5: Commit** — `feat(interfaces): add canonical IaC config key set`

### Task 5: Canonical spec types

**Files:**
- Create: `interfaces/iac_canonical_types.go`
- Create: `interfaces/iac_canonical_types_test.go`

**Step 1-4:** Add `JobSpec`, `WorkerSpec`, `StaticSiteSpec`, `SidecarSpec`, `PortSpec`, `AutoscalingSpec`, `HealthCheckSpec`, `RouteSpec`, `CORSSpec`, `DomainSpec`, `AlertSpec`, `LogDestinationSpec`, `TerminationSpec`, `IngressSpec`, `EgressSpec`, `MaintenanceSpec`, `ResourceSpec`. One test per type: empty-value check + required-field validation.

**Step 5: Commit** — `feat(interfaces): add canonical IaC spec types (Job, Worker, StaticSite, Sidecar, Port, ...)`

### Task 6: JSON schema for canonical keys

**Files:**
- Create: `interfaces/iac_canonical_schema.json`
- Create: `interfaces/iac_canonical_schema_test.go`

**Step 1-4:** Write JSON schema validating every canonical key. Test: several valid + invalid sample configs from `workflow-scenarios/` round-trip through schema validation correctly.

**Step 5: Commit** — `feat(interfaces): add JSON schema for canonical IaC keys`

### Task 7: SupportedCanonicalKeys() on IaCProvider

**Files:**
- Modify: `interfaces/iac_provider.go` — add method to interface
- Modify: `module/iac_provider_builtin_test.go` or similar — stub mocks

**Step 1-4:** Add `SupportedCanonicalKeys() []string` to `IaCProvider`. Implementations in built-in modules return the full canonical set initially; external plugins (DO, AWS, etc.) update in their own phases.

**Step 5: Commit** — `feat(interfaces): add SupportedCanonicalKeys to IaCProvider`

### Task 8: Hook event type enumeration

**Files:**
- Create: `interfaces/build_hooks.go`
- Create: `interfaces/build_hooks_test.go`

**Step 1-4:** Define `HookEvent` string type + constants for all 11 events (pre_build, pre_target_build, post_target_build, pre_container_build, post_container_build, pre_container_push, post_container_push, pre_artifacts_publish, post_artifacts_publish, pre_build_fail, post_build). Add `HookPayload` marshaling types per event. `IsValidHookEvent(string) bool` test.

**Step 5: Commit** — `feat(interfaces): add build-pipeline hook event types`

### Task 9: Hook dispatcher in wfctl

**Files:**
- Create: `cmd/wfctl/build_hooks.go`
- Create: `cmd/wfctl/build_hooks_test.go`

**Step 1: Failing test**

```go
func TestHookDispatcher_PriorityOrder(t *testing.T) {
    var order []string
    fake := &fakePluginRegistry{
        handlers: []HookHandler{
            {Plugin: "a", Event: "post_build", Priority: 500, Run: func() { order = append(order, "a") }},
            {Plugin: "b", Event: "post_build", Priority: 100, Run: func() { order = append(order, "b") }},
        },
    }
    disp := NewHookDispatcher(fake)
    _ = disp.Dispatch(context.Background(), "post_build", nil)
    if got := strings.Join(order, ","); got != "b,a" {
        t.Errorf("priority order: got %q want b,a", got)
    }
}
```

**Step 2-4:** Implement `HookDispatcher` that scans installed plugins via `plugin.json` capabilities, orders handlers by priority ascending, dispatches via subprocess invocation (`plugin-binary --wfctl-hook <event>` with JSON payload on stdin). Failure policy per plugin manifest (`on_hook_failure: fail|warn|skip`). Timeout per handler.

**Step 5: Commit** — `feat(wfctl): add build-hook dispatcher with priority ordering and subprocess dispatch`

### Task 10: SDK ServePluginFull helper

**Files:**
- Create: `plugin/external/sdk/cli.go`
- Create: `plugin/external/sdk/hooks.go`
- Create: `plugin/external/sdk/serve_full.go`
- Create: `plugin/external/sdk/serve_full_test.go`

**Step 1-4:** Implement `CLIProvider` + `HookHandler` interfaces and `ServePluginFull(p, cli, hooks)` that inspects `os.Args` for `--wfctl-cli` or `--wfctl-hook` and dispatches accordingly, falling back to gRPC plugin mode. Test covers all three paths with a fake plugin.

**Step 5: Commit** — `feat(sdk): add ServePluginFull for CLI + hook + gRPC plugin dispatch`

### Task 11: Dynamic CLI command registry

**Files:**
- Create: `cmd/wfctl/plugin_cli_commands.go`
- Create: `cmd/wfctl/plugin_cli_commands_test.go`
- Modify: `cmd/wfctl/main.go`

**Step 1-4:** At wfctl startup, scan `data/plugins/*/plugin.json` for `capabilities.cliCommands`. Build a map `command-name -> plugin-binary-path`. On invocation, if first arg matches a dynamic command and no static command shadows it, invoke `<plugin-binary> --wfctl-cli <args...>`. Static commands win. Conflict detection at startup: same command from two plugins → error with remediation.

**Reserved names:** Block registration of `plugin|build|infra|ci|deploy|tenant|config|api|contract|diff|dev|generate|git|help|init|inspect|list|mcp|modernize|pipeline|registry|template|update|validate|version`.

**Test:** fake plugin directory with two plugins declaring different commands; one declaring a reserved name; one declaring a colliding command. Router behaves per rules.

**Step 5: Commit** — `feat(wfctl): add dynamic CLI command registration from plugin manifests`

### Task 12: plugin.json schema additions

**Files:**
- Modify: `cmd/wfctl/plugin_registry.go` — extend `RegistryCapabilities` with `BuildHooks`, `CLICommands`, `MigrationDrivers`, `PortIntrospect`.
- Modify: test

**Step 1-4:** Add new optional capability fields. JSON parse test confirms unmarshaling from a sample registry manifest.

**Step 5: Commit** — `feat(registry): extend capability schema with buildHooks, cliCommands, portIntrospect`

### Task 13: ProvidesMigrations() module contract

**Files:**
- Create: `interfaces/provides_migrations.go`
- Create: `interfaces/provides_migrations_test.go`

**Step 1-4:** Interface:
```go
type MigrationProvider interface {
    ProvidesMigrations() (fs.FS, error)
    MigrationsDependencies() []string // other providers that must apply first
}
```
Test with an in-memory fs.FS.

**Step 5: Commit** — `feat(interfaces): add MigrationProvider module contract`

### Task 14: Tenant selector implementations

**Files:**
- Create: `module/tenant_selector_host.go`
- Create: `module/tenant_selector_host_test.go`
- Create: `module/tenant_selector_subdomain.go` + test
- Create: `module/tenant_selector_header.go` + test
- Create: `module/tenant_selector_cookie.go` + test
- Create: `module/tenant_selector_jwt_claim.go` + test
- Create: `module/tenant_selector_session.go` + test
- Create: `module/tenant_selector_static.go` + test

**Step 1-4 per selector:** TDD. `Match(*http.Request) (key string, matched bool, err error)`. Unit tests with `httptest.NewRequest`. JWT selector uses the existing auth module's claims extraction.

**Step 5: Commit per selector or batch** — `feat(tenants): add <type> selector`

### Task 15: TenantRegistry SQL backend

**Files:**
- Create: `module/tenants.go` — `TenantRegistry` implementation
- Create: `module/tenants_test.go`
- Create: `module/tenants_migrations.go` — `ProvidesMigrations()` returning embedded FS
- Create: `module/tenants_migrations/20260422000001_tenants.up.sql`
- Create: `module/tenants_migrations/20260422000001_tenants.down.sql`

**Step 1-4:** SQL backend implements `TenantRegistry`. LRU cache in front of DB. Template-based DDL using `TenantSchemaConfig` (table prefix/suffix, column names). Tests run against an ephemeral Postgres via testcontainers.

**Step 5: Commit** — `feat(tenants): SQL-backed registry with configurable schema + embedded migrations`

### Task 16: TenantResolver module (HTTP middleware)

**Files:**
- Create: `module/tenant_resolver.go`
- Create: `module/tenant_resolver_test.go`

**Step 1: Failing test**

Table-driven across modes:
- `first_match` returns first matching selector's tenant.
- `all_must_match` returns tenant when all required selectors match and all match same tenant; errors on mismatch or missing-required.
- `consensus` returns majority.

**Step 2-4:** Implement middleware. Injects `Tenant` into `context.Context` via well-known key. Emits `tenant.mismatch` event on disagreements. On 403 errors emit structured body `{error, detail}`.

**Step 5: Commit** — `feat(tenants): HTTP resolver middleware with first_match/all_must_match/consensus modes`

### Task 17: Tenant pipeline steps

**Files:**
- Create: `module/pipeline_step_tenant_ensure.go` + test
- Create: `module/pipeline_step_tenant_list.go` + test
- Create: `module/pipeline_step_tenant_get_by_domain.go` + test
- Create: `module/pipeline_step_tenant_update.go` + test
- Create: `module/pipeline_step_tenant_disable.go` + test

**Step 1-4:** Each step resolves the `TenantRegistry` service and calls the matching method. RBAC-checked via existing authz plugin.

**Step 5: Commit** — `feat(tenants): pipeline steps for ensure/list/get/update/disable`

### Task 18: wfctl tenant CLI

**Files:**
- Create: `cmd/wfctl/tenant.go`
- Create: `cmd/wfctl/tenant_test.go`
- Modify: `cmd/wfctl/main.go` to register subcommand

**Step 1-4:** `wfctl tenant ensure|list|get|update|disable` that loads the workflow config, instantiates the tenant registry, and calls through. Output formats: table | json | yaml.

**Step 5: Commit** — `feat(wfctl): add tenant subcommand`

### Task 19: Scaffold dockerfile with --mode + --base-image

**Files:**
- Create: `cmd/wfctl/scaffold_dockerfile.go`
- Create: `cmd/wfctl/scaffold_dockerfile_test.go`
- Create: `cmd/wfctl/templates/Dockerfile.prebuilt.generic.tmpl`
- Create: `cmd/wfctl/templates/Dockerfile.prebuilt.library.tmpl`

**Step 1-4:** Command:
```
wfctl scaffold dockerfile --mode=generic            # default
wfctl scaffold dockerfile --mode=library --binary=bmw-server
wfctl scaffold dockerfile --base-image=...          # override, validated
```

Templates embed digest-pinned distroless. Validation: warn on alpine or glibc bases; block shell-containing bases unless `--allow-shell`. Output: `Dockerfile.prebuilt` in current dir.

**Step 5: Commit** — `feat(wfctl): scaffold canonical Dockerfile with mode + base-image flags`

### Task 20: Port introspection aggregator

**Files:**
- Create: `cmd/wfctl/port_introspect.go`
- Create: `cmd/wfctl/port_introspect_test.go`

**Step 1-4:** Aggregate port-field declarations from installed plugins + core built-ins (http.server.port, observability.metrics.port, etc.). Scan app.yaml, return list of discovered ports. Respect explicit `ci.build.containers[].expose_ports` overrides. Output consumable by `scaffold dockerfile` to emit `EXPOSE` directives.

**Step 5: Commit** — `feat(wfctl): plugin-extensible port introspection`

### Task 21: Ship workflow v0.18.0

**Files:**
- Update: CHANGELOG.md (new section for v0.18.0).
- Update: version in cmd/server/main.go ldflags default to "0.18.0".

**Step 1:** Open PR from `feat/platform-maturity-v0.18.0` to main with the whole Phase 1 stack.
**Step 2:** Wait for CI (lint, test, build, performance benchmarks, osv-scan).
**Step 3:** Admin-merge once green.
**Step 4:** Tag `v0.18.0`; push.
**Step 5:** Confirm release workflow publishes wfctl + workflow-server binaries.

---

## Phase 2 — workflow-plugin-migrations v0.1.0

Create new repo `github.com/GoCodeAlone/workflow-plugin-migrations`. All work inside that repo's worktree.

### Task 22: Scaffold new plugin repo

**Files:**
- Create repo via `gh repo create --template GoCodeAlone/workflow-plugin-template --public`
- go.mod, .goreleaser.yaml, .github/workflows/{ci,release}.yml, LICENSE, README.md, plugin.json stubs

**Step 1:** `gh repo create GoCodeAlone/workflow-plugin-migrations --template GoCodeAlone/workflow-plugin-template --public`
**Step 2:** Clone + edit go.mod module path.
**Step 3:** Update .goreleaser.yaml with **three builds** (`workflow-plugin-migrations`, `workflow-plugin-atlas-migrate`, `workflow-migrate`) + archives + SBOMs + dockers for workflow-migrate.
**Step 4:** Commit initial scaffold.
**Step 5:** Push to main (scaffold is fine on main; features come as PRs).

### Task 23: Shared pkg/driver types + module types

**Files:**
- pkg/driver/driver.go — adapter around `interfaces.MigrationDriver`
- pkg/driver/registry.go — in-process driver registry
- internal/module_migrations.go — `database.migrations` module type
- internal/module_driver.go — `database.migration_driver` module type
- internal/module_migrations_test.go, internal/module_driver_test.go

**Step 1-4:** TDD the module types. Config parsing + `driver_ref` resolution via modular service registry.

**Step 5:** `feat: database.migrations + database.migration_driver module types`

### Task 24: golang-migrate driver

**Files:**
- internal/golangmigrate/driver.go
- internal/golangmigrate/driver_test.go

**Step 1-4:** Wrap `github.com/golang-migrate/migrate/v4`. Implement Up/Down/Status/Goto against the canonical `interfaces.MigrationDriver`. Tests with embedded-postgres harness.

**Step 5:** `feat(golang-migrate): driver implementation`

### Task 25: goose driver

Same as Task 24 with `github.com/pressly/goose/v3`.

**Step 5:** `feat(goose): driver implementation`

### Task 26: Test harness + conformance suite

**Files:**
- pkg/testharness/harness.go + embedded.go + testcontainers.go + provided.go
- pkg/conformance/suite.go + corpus/001_create_users.up.sql + 002_add_indexes.up.sql + matching .down.sql + bad_002.up.sql (for partial-failure case)
- pkg/conformance/suite_test.go — runs suite against golang-migrate + goose drivers using EmbeddedPostgres harness

**Step 1-4:** TDD suite. Each of the 10 cases from design as a method. Corpus bundled as `embed.FS`.

**Step 5:** `feat(conformance): public Suite runnable by any driver`

### Task 27: Atlas driver (separate cmd)

**Files:**
- internal/atlas/driver.go + atlas_test.go
- cmd/workflow-plugin-atlas-migrate/main.go
- cmd/workflow-plugin-atlas-migrate/plugin.json

**Step 1-4:** Import `ariga.io/atlas`. Implement driver. Wire as separate binary that registers only the atlas driver.

**Step 5:** `feat(atlas): driver implementation as separate plugin binary`

### Task 28: Lint tool

**Files:**
- pkg/lint/lint.go + lint_test.go
- cmd/workflow-migrate/lint.go

**Step 1-4:** `workflow-migrate lint <dir>` performs ordering, naming, dangerous-op, reversibility checks. Output as structured JSON.

**Step 5:** `feat(lint): static analysis for migration files`

### Task 29: workflow-migrate standalone binary

**Files:**
- cmd/workflow-migrate/main.go
- cmd/workflow-migrate/up.go, down.go, status.go, goto.go, test.go, lint.go, tenant_ensure.go
- cmd/workflow-migrate/Dockerfile — distroless/static base

**Step 1-4:** Cobra root + subcommands. Each subcommand loads workflow config, instantiates drivers via registry, executes. `test` subcommand drives the test harness (full-cycle + checkpoint + --keep-alive).

**Step 5:** `feat(workflow-migrate): standalone binary + OCI image for pre-deploy jobs`

### Task 30: wfctl migrate CLI (dynamic command)

**Files:**
- pkg/cli/root.go — shared Cobra root for `wfctl migrate *`
- cmd/workflow-plugin-migrations/main.go — hooks into wfctl via `--wfctl-cli`
- cmd/workflow-plugin-migrations/plugin.json — declares cliCommands

**Step 1-4:** `CLIProvider` implementation. Shared root so `workflow-migrate` CLI and `wfctl migrate` call the same code. Plugin manifest declares `cliCommands: [{name: migrate, subcommands: [up, down, status, goto, test, lint, tenant-ensure]}]`.

**Step 5:** `feat: wfctl migrate dynamic command via plugin CLI dispatch`

### Task 31: Release v0.1.0

Tag `v0.1.0`. Verify three artifacts release cleanly: `workflow-plugin-migrations` tarball, `workflow-plugin-atlas-migrate` tarball, `workflow-migrate` binary + image.

---

## Phase 3 — workflow-plugin-digitalocean v0.7.0

### Task 32: SupportedCanonicalKeys on DOProvider

**Files:**
- Modify: internal/provider.go — add method returning full canonical key list minus unsupported keys.
- Modify: tests

**Step 5:** `feat(do): implement SupportedCanonicalKeys`

### Task 33: buildAppSpec rewrite (top-level fields)

**Files:**
- internal/drivers/app_platform.go — split `buildAppSpec` out of Create/Update
- internal/drivers/app_platform_buildspec.go (new)
- internal/drivers/app_platform_buildspec_test.go

**Step 1-4:** TDD per canonical key group. Domains, Alerts, Ingress, Egress, Maintenance, Features, Vpc, LogDestinations mapped from canonical → `godo.AppSpec`. Test table: for each canonical input, assert expected godo struct.

**Step 5:** `feat(do): map canonical top-level fields to AppSpec`

### Task 34: AppServiceSpec field fill

Same pattern for: `InstanceSizeSlug` (via existing `resolveSizing`), `Autoscaling`, `Protocol`, `Routes`, `HealthCheck`, `CORS`, `InternalPorts`, `BuildCommand`, `RunCommand`, `DockerfilePath`, `SourceDir`, `Termination`, `LivenessHealthCheck`.

**Step 5:** `feat(do): map canonical service fields to AppServiceSpec`

### Task 35: AppJobSpec mapping (PRE_DEPLOY etc.)

**Files:**
- internal/drivers/app_platform_jobs.go + test

**Step 1-4:** Canonical `jobs[]` → `godo.AppJobSpec{Kind: PRE_DEPLOY|POST_DEPLOY|FAILED_DEPLOY|SCHEDULED}`. Env vars, env_vars_secret resolution, cron for scheduled.

**Step 5:** `feat(do): map canonical jobs to AppJobSpec`

### Task 36: Workers + StaticSites

**Step 5:** `feat(do): map canonical workers + static_sites`

### Task 37: Sidecars → sibling AppServiceSpec

**Files:**
- internal/drivers/app_platform_sidecars.go + test

**Step 1-4:** Each canonical `sidecars[]` entry becomes a separate `AppServiceSpec` on the same App with DO internal-routing config. `SharesNetworkWith` hint sets up the routing. Test: Tailscale sidecar spec generates correct sibling service.

**Step 5:** `feat(do): map canonical sidecars to sibling services with internal routing`

### Task 38: appHealthResult fix

**Files:**
- Modify: internal/drivers/app_platform.go `appHealthResult`
- Add: test case for `ActiveDeployment.Phase == ERROR/FAILED/CANCELED/SUPERSEDED`

**Step 1: Failing test** asserting `Phase == ERROR` returns `Healthy=false, Message contains "deployment failed"`, not "no deployment found".

**Step 5:** `fix(do): appHealthResult recognizes non-ACTIVE ActiveDeployment phases`

### Task 39: DatabaseDriver.trusted_sources

**Files:**
- Modify: internal/drivers/database.go
- Add: test mapping {type, name} to `godo.DatabaseFirewallRule`

**Step 5:** `feat(do): DatabaseDriver trusted_sources with app/ip/k8s/droplet/tag types`

### Task 40: Release v0.7.0

Bump version, update registry manifest, tag `v0.7.0`.

---

## Phase 4 — workflow-plugin-supply-chain v0.3.0

### Task 41: Build hook handler (post_container_build → SBOM)

**Files:**
- internal/hooks/sbom.go + sbom_test.go
- cmd/workflow-plugin-supply-chain/main.go — registers as HookHandler

**Step 1-4:** Invoke `syft` on the built image, produce CycloneDX JSON, attach as OCI artifact via `cosign attach sbom`. Test with a tiny test image.

**Step 5:** `feat(supply-chain): post_container_build hook generates and attaches SBOM`

### Task 42: Signing hook

**Files:**
- internal/hooks/sign.go + sign_test.go

**Step 1-4:** Keyless cosign sign on the image digest. In CI, uses GitHub OIDC. Locally, skips with a warning.

**Step 5:** `feat(supply-chain): post_container_build hook signs image via cosign keyless`

### Task 43: SLSA provenance

**Step 1-4:** Integrate `slsa-framework/slsa-github-generator` via reusable workflow include. Document + test.

**Step 5:** `feat(supply-chain): SLSA Level 3 provenance attestation`

### Task 44: wfctl supply-chain dynamic CLI

**Files:**
- pkg/cli/root.go
- cmd/workflow-plugin-supply-chain/plugin.json — declares cliCommands
- internal/cli/scan.go, verify.go, sbom.go, report.go

**Step 1-4:** `wfctl supply-chain scan|verify|sbom|report`. Uses the registered `syft`/`cosign`/OSV machinery.

**Step 5:** `feat(supply-chain): wfctl supply-chain * subcommands`

### Task 45: wfctl scaffold sbom / sign / ci doctor

**Files:**
- internal/cli/scaffold_sbom.go, scaffold_sign.go, ci_doctor.go

**Step 1-4:** Scaffolders add appropriate config blocks to app.yaml + `.github/workflows/*.yml`. Doctor runs preflight checks (OIDC perms, workflow allowlists, cosign install) via `gh api`.

**Step 5:** `feat(supply-chain): scaffold sbom|sign + ci doctor`

### Task 46: supply_chain config schema

**Files:**
- pkg/schema/supply_chain_schema.json
- cmd/workflow-plugin-supply-chain/main.go — exposes via `--wfctl-schema supply_chain`

**Step 5:** `feat(supply-chain): expose config schema for wfctl validate`

### Task 47: Docs migration

**Files:**
- docs/overview.md, enabling-sbom.md, github-setup.md, verifying-artifacts.md, incident-response.md, compliance-mapping.md, tutorials/

**Step 1-4:** Author. Reference in README.md.

**Step 5:** `docs: consolidate supply-chain docs into plugin repo`

### Task 48: Release v0.3.0

---

## Phase 5 — BMW adoption (Dockerfile + config)

### Task 49: Scaffold canonical Dockerfile in BMW

**Files:**
- buymywishlist/Dockerfile.prebuilt (replace)
- remove migrate.sh copying from BMW's build

**Step 1:** Run `wfctl scaffold dockerfile --mode=generic` from BMW root to generate new Dockerfile.
**Step 2:** Diff against current — confirm removal of migrate.sh and switch to distroless.
**Step 3:** Locally `wfctl build -c app.yaml --tag local-test --no-push` succeeds.
**Step 4:** `docker run` the image; confirm `/healthz` returns green against a migrated test DB.

**Step 5:** Commit to a BMW branch.

### Task 50: Add expose_ports, supply_chain config to BMW

**Files:**
- buymywishlist/app.yaml

**Step 1-4:** Add `ci.build.containers[].expose_ports` + `supply_chain:` blocks.

**Step 5:** Commit.

---

## Phase 6 — BMW tenancy + migrations adoption

### Task 51: BMW pre-deploy jobs

**Files:**
- buymywishlist/infra.yaml

**Step 1-4:** Add `jobs:` to `bmw-app`'s config — `migrate` + `tenant-ensure`. Reference `registry.digitalocean.com/bmw-registry/workflow-migrate:${MIGRATE_SHA}` image. Secrets wired.

**Step 5:** Commit.

### Task 52: BMW tenant-resolver + bootstrap extensions

**Files:**
- buymywishlist/app.yaml — add `auth.tenant_resolver` module with `StaticSelector` reading `BMW_TENANT_ID`
- buymywishlist/infra.yaml — add `secrets.generate` entries for `BMW_TENANT_ID`, `BMW_TENANT_SLUG`, `BMW_TENANT_DOMAIN`

**Step 1-4:** Modify. Validate with `wfctl validate`.

**Step 5:** Commit.

### Task 53: BMW-PR to main

Open PR with tasks 49–52 combined. Admin-merge once CI green.

---

## Phase 7 — Final BMW deploy + verification

### Task 54: Bump setup-wfctl

**Files:**
- buymywishlist/.github/workflows/*.yml

**Step 1-4:** Bump `setup-wfctl` version input to `v0.18.0` across all four workflow files.

**Step 5:** Commit.

### Task 55: Deploy to staging

**Step 1:** Merge BMW adoption PR.
**Step 2:** Watch CI → Deploy workflow_run → build-image → deploy-staging.
**Step 3:** Monitor pre-deploy job logs (migrate + tenant-ensure) directly from DO console or `doctl apps logs`.
**Step 4:** Confirm main service reaches `ACTIVE` phase.

### Task 56: Verify against deployed staging

**Step 1:** Run playwright against deployed `https://bmw-staging.ondigitalocean.app` URL.
**Step 2:** Confirm 90%+ of API + UI tests pass. Target: same baseline as local run (once tenancy seeded).
**Step 3:** `wfctl supply-chain verify` against deployed image digest — confirm signature + SBOM attached.

### Task 57: Promote to prod

Only once staging E2E is green + supply-chain verification clean. Use existing BMW deploy gate.

---

## Commit discipline

- TDD: test → fail → impl → pass → commit for every discrete feature.
- Squash commits within a task if they're TDD increments of the same feature; keep separate task commits for distinct features so `git log` tells the story.
- Each Phase 1 task is its own PR-ready commit sequence on `feat/platform-maturity-v0.18.0`.
- Plugin-repo tasks are commits on the plugin repo's default branch (new plugins) or PRs for existing plugins.
- BMW tasks go through BMW's PR flow with CI + Copilot review gate.

## Tests inventory (must-pass before shipping each phase)

- **Phase 1:** `GOWORK=off go test ./... -race` green. Lint clean. Schema contract tests for canonical keys against `workflow-scenarios/` examples.
- **Phase 2:** Conformance suite passes for all three drivers against embedded-postgres AND testcontainers-postgres. `workflow-migrate test` + `test --keep-alive` integration tests.
- **Phase 3:** Per-canonical-key translation tests. `appHealthResult` regression tests. Sidecar sibling-service mapping tests.
- **Phase 4:** End-to-end hook integration: `wfctl build` → SBOM attached → signature verified. `wfctl plugin install --verify` rejects tampered tarball. OSV catch-known-CVE test.
- **Phase 5:** Distroless build + run under `--read-only` succeeds. SIGTERM graceful shutdown test.
- **Phase 6:** Pre-deploy migrate + tenant-ensure works against a scratch Postgres. BMW container starts + `/healthz` green.
- **Phase 7:** Playwright suite run against deployed staging. Supply-chain verification clean.

---

**End of plan.**
