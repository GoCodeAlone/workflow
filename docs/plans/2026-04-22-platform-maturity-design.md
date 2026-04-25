# Platform maturity design — 2026-04-22

Brings six structural gaps to resolution in one coordinated rollout: schema migrations, tenancy, canonical IaC config, hardened base image, supply-chain verification, and plugin-extensible wfctl (dynamic CLI commands + build-pipeline hooks).

The design came out of BMW's deploy chain surfacing each gap in sequence. Rather than file each as a standalone workstream, we treat them together because they share core plumbing — a migration driver needs a pre-deploy job that needs canonical IaC key support that needs DO provider gap-fill, etc.

## Executive summary

Six deliverables, each with a clear home:

| # | Concern | Home | Ships as |
|---|---|---|---|
| 1 | Schema migrations | new `workflow-plugin-migrations` repo | plugin v0.1.0 + standalone `workflow-migrate` binary |
| 2 | Tenancy | `workflow` core + plugin-extensible selectors/registry | part of workflow v0.18.0 |
| 3 | Canonical IaC keys + DO field-fill | `workflow` core interfaces + `workflow-plugin-digitalocean` | core v0.18.0 + DO v0.7.0 |
| 4 | Hardened base image | `workflow` core (scaffolder) + BMW adoption | v0.18.0 scaffolder; BMW PR |
| 5 | Supply-chain verification | `workflow-plugin-supply-chain` (full ownership) | plugin v0.3.0 |
| 6 | wfctl extensibility (dynamic CLI + build hooks) | `workflow` core | part of v0.18.0 |

Scope ends when BMW deploys successfully against the new stack, with signed+SBOM'd images, migrations applied by a pre-deploy job, and tenancy resolved at request time.

## Background

BMW's deploy chain exposed six independent-looking failures across ~20 hours of iteration:

1. Plugin versions statically "0.1.0" despite being released as v0.1.2/v0.2.1 — fixed by ldflags injection (done prior; not in this design).
2. Dockerfile missed copying `openapi.json` — fixed ad hoc (committed).
3. No mechanism in the container to run DB migrations; schema was empty on deploy.
4. Even after manual migration, the default tenant row was missing, breaking auth-register.
5. DO plugin maps `Services[0]` only — 30+ AppSpec fields silently dropped, including `Jobs[]` (pre-deploy), `Domains`, `Alerts`, `Autoscaling`, `Vpc`, sidecars.
6. Base image is `alpine:3.20` with shell + package manager — no SBOM, no signatures, too much attack surface.

Each fix in isolation would paper over the symptom without addressing the class. The design below puts each concern in a first-class home and wires them together through a small set of core extension points (interfaces, hooks, dynamic CLI).

## Non-goals

- Multi-tenant admin UI for BMW (lives in BMW; referenced here but not specified).
- Re-release every existing plugin with these new hook/CLI capabilities. Plugins opt in as they need them.
- New IaC providers. Canonical key set grows as needed but AWS/GCP/Azure field-fill beyond what DO needs is a follow-up.
- Compliance certifications (SOC 2, ISO 27001, FedRAMP). We produce the artifacts they want (SBOMs, signatures, SLSA provenance); auditors consume them.

## Core extension points (workflow v0.18.0)

The release contains four additions that everything else in the design depends on:

### 1. Canonical interfaces

Interfaces live in `workflow/interfaces/` so plugins can depend on them without pulling the migrations or supply-chain plugin as a transitive dep.

- `interfaces/migration_driver.go` — `MigrationDriver` interface with Up/Down/Status/Goto and Request/Result/Status types; errors go through the existing IaC sentinel system (`ErrTransient`, `ErrUnauthorized`, `ErrValidation`, etc.).
- `interfaces/tenant_resolver.go` — `TenantResolver` interface; `Tenant` struct (ID, Name, Slug, Domains, Metadata, IsActive); `Selector` interface.
- `interfaces/tenant_registry.go` — `TenantRegistry` interface (Ensure/GetByID/GetByDomain/GetBySlug/List/Update/Disable); default SQL-backed implementation in `module/`.
- `interfaces/iac_canonical_keys.go` — the portable config key set (documented fully in the "Canonical IaC keys" section below), plus JSON schema and `SupportedCanonicalKeys()` method added to `IaCProvider`.

### 2. Build pipeline hooks

`wfctl build` becomes a hook orchestrator. Events emitted during a build:

| Event | Context |
|---|---|
| `pre_build` | Full `ci.build` config; planned targets |
| `pre_target_build` / `post_target_build` | Target name, type, outputs, duration |
| `pre_container_build` / `post_container_build` | Dockerfile, context, image ref, digest |
| `pre_container_push` / `post_container_push` | Image refs, registries, digests |
| `pre_artifacts_publish` / `post_artifacts_publish` | Asset paths, URLs |
| `pre_build_fail` | Error context |
| `post_build` | Build summary |

Plugins register hook handlers via `plugin.json`:

```json
"capabilities": {
  "buildHooks": [
    { "event": "post_container_build", "priority": 500, "description": "..." }
  ]
}
```

Dispatch is subprocess-based: wfctl invokes `<plugin-binary> --wfctl-hook <event>` with JSON event payload on stdin. Exit 0 = continue; non-zero + policy determines fail/warn/skip. Handler order is priority-sorted; ties are resolved by plugin-name lexical order (deterministic).

Core itself emits hooks but registers none. The supply-chain plugin registers hooks for SBOM + sign + provenance.

### 3. Dynamic CLI command registration

`plugin.json` can declare top-level wfctl subcommands:

```json
"capabilities": {
  "cliCommands": [
    {
      "name": "supply-chain",
      "description": "...",
      "subcommands": [ { "name": "scan", "description": "..." }, ... ],
      "flags_passthrough": true
    }
  ]
}
```

At wfctl startup, the command router scans installed plugin manifests, registers their top-level commands, and dispatches matching invocations via subprocess (`<plugin-binary> --wfctl-cli <command> <args...>`).

Reserved names (today's static commands) are blocked in registry manifest validation: `plugin`, `build`, `infra`, `ci`, `deploy`, `migrate` (wait — `migrate` comes from a plugin! — see below), `tenant`, `config`, `api`, `contract`, `diff`, `dev`, `generate`, `git`, `help`, `init`, `inspect`, `list`, `mcp`, `modernize`, `pipeline`, `registry`, `template`, `update`, `validate`, `version`.

Exception: `migrate` is owned by the migrations plugin, not core. It's reserved in registry manifest validation but routed through dynamic dispatch, not a core static command. Same for `supply-chain`.

Conflict rules: if two installed plugins declare the same top-level command, wfctl errors at startup with remediation. Static wfctl commands always win.

### 4. Plugin SDK additions

```go
// plugin/external/sdk/
type CLIProvider interface {
    RunCLI(args []string) int   // returns exit code
}
type HookHandler interface {
    HandleBuildHook(event string, payload []byte) (result []byte, err error)
}

func ServePluginFull(p PluginProvider, cli CLIProvider, hooks HookHandler) {
    // dispatches --wfctl-cli / --wfctl-hook / gRPC plugin based on argv
}
```

Plugins that don't use CLI/hooks keep calling `ServePlugin(p)` — zero impact.

### 5. `ProvidesMigrations() fs.FS` on modules

Core modules that have schema (tenants, persistence, authz) embed their SQL via `go:embed` and return an `fs.FS` from `ProvidesMigrations()`. The migration runner (in `workflow-plugin-migrations`) walks all registered modules, collects these filesystems, orders by declared dependency (`after: [workflow.tenants]`), and applies.

Same mechanism apps use: their own `migrations/` directory registers through a `database.migrations` module with `source_dir: /app/migrations`.

### 6. Canonical IaC keys

Core publishes the portable config shape for `infra.container_service` and friends. Full list:

- **Top-level**: `name`, `region`, `image`, `http_port`, `internal_ports`, `protocol`, `instance_count`, `size` (abstract `xs|s|m|l|xl`), `env_vars`, `env_vars_secret`, `vpc_ref`.
- **Autoscaling**: `autoscaling: {min, max, cpu_percent, memory_percent}`.
- **HTTP**: `routes`, `cors`, `domains`.
- **Health**: `health_check`, `liveness_check`.
- **Traffic**: `ingress`, `egress` (fail-closed on providers that support it).
- **Observability**: `alerts`, `log_destinations`.
- **Lifecycle**: `termination: {drain_seconds, grace_period_seconds}`, `maintenance`.
- **Sibling workloads**: `jobs`, `workers`, `static_sites`, `sidecars`.
- **Source**: `build_command`, `run_command`, `dockerfile_path`, `source_dir`.
- **Provider overflow**: `provider_specific: { digitalocean: {...}, aws: {...}, ... }`.

Jobs (first-class because DO/ECS/k8s/Cloud Run all support them):

```go
type JobSpec struct {
    Name, Kind    string    // kind: pre_deploy | post_deploy | failed_deploy | scheduled
    Image         string
    RunCommand    string
    EnvVars       map[string]string
    EnvVarsSecret map[string]string
    Cron          string    // for scheduled
    Termination   *Termination
    Alerts        []AlertSpec
    LogDestinations []LogDestinationSpec
}
```

Ports are structured and named:

```go
type PortSpec struct {
    Name     string     // "http", "metrics", "grpc"
    Port     int32
    Protocol string     // tcp | udp | sctp, default tcp
    Public   bool       // exposed externally
}
```

JSON schema at `interfaces/iac_canonical_schema.json`. `wfctl validate` checks configs before any driver runs.

Port auto-detection: each plugin declares `introspect: port-fields` in plugin.json pointing at config JSON paths its module types use (`$.config.port`, `$.config.grpc.port`). wfctl aggregates these declarations + core's built-in paths when scaffolding Dockerfiles. Plugins without declarations don't participate in auto-detect — explicit `expose_ports` required.

## workflow-plugin-migrations

New public plugin repo. Single repo publishes **three binaries** so Atlas's deps only land in the Atlas-specific binary while the common driver set ships minimal.

### Repo layout

```
workflow-plugin-migrations/
├── go.mod                          # all drivers declared
├── .goreleaser.yaml                # multiple builds / archives / sboms / dockers
├── docs/                           # shared docs for every driver
├── pkg/
│   ├── driver/                     # MigrationDriver adapter & registry
│   ├── conformance/                # exported for third-party driver authors
│   ├── testharness/                # embedded-postgres + testcontainers
│   ├── lint/                       # static analysis
│   └── cli/                        # shared Cobra root for `wfctl migrate *`
├── internal/
│   ├── golangmigrate/              # driver
│   ├── goose/                      # driver
│   ├── atlas/                      # driver (only imported by atlas binary)
│   └── runner/                     # shared pre-deploy runner logic
└── cmd/
    ├── workflow-plugin-migrations/      # golang-migrate + goose + CLI
    ├── workflow-plugin-atlas-migrate/   # atlas only
    └── workflow-migrate/                # standalone binary for pre-deploy jobs
```

Go's build tree only pulls what each `main.go` imports — Atlas deps only land in the Atlas binary even though declared in the shared `go.mod`.

### Drivers (v1)

1. **`golang-migrate`** — pair of up/down `.sql` files, numbered. Most common, simplest.
2. **`goose`** — `.sql` with `-- +goose Up/Down` annotations; supports Go-function migrations too.
3. **`atlas`** — declarative HCL + versioned SQL. Best for complex schema drift / declarative ops.

### Module types

```yaml
- name: bmw-db-migrations
  type: database.migrations
  config:
    driver_ref: golang-migrate-driver    # resolves to a `database.migration_driver` module
    source_dir: /app/migrations
    dsn_env: DATABASE_URL
    history_table: schema_migrations     # driver default
    timeout: 5m
    hooks:
      after_up: [ { sql: seeds/tenants.sql } ]
      after_checkpoint: [ { sql: fixtures/test_wishlist.sql } ]  # test mode only

- name: golang-migrate-driver
  type: database.migration_driver
  config:
    driver: golang-migrate
```

### Step types

`step.migrate_up`, `step.migrate_down`, `step.migrate_status`, `step.migrate_to` — dispatch through `database.migrations` module; plugin registers step factories on ModuleTypes.

### Test harness

`workflow-migrate test` (CLI subcommand, shared `pkg/cli`) + Go library for use in app test suites:

- `TestHarness` interface with three implementations: `ProvidedDSN`, `TestContainers` (postgres:16-alpine), `EmbeddedPostgres` (Fergus Strange's pure-Go embedded).
- Auto-fallback order: `--dsn` > testcontainers if Docker socket > embedded-postgres.
- **Full cycle** (`workflow-migrate test`) applies all up → runs hooks → applies all down → asserts clean final state.
- **Checkpoint** (`workflow-migrate test <version>`) applies 1..N-1 → applies N-up → runs hooks → applies N-down → asserts pre-N state.
- **Integration mode** (`--keep-alive`) sets up DB, prints DSN on stdout, returns exit 0 so integration tests can run against it; `workflow-migrate test-teardown` cleans up.

### Conformance suite

Behavioral matrix every driver must pass against a real Postgres:

| Case | Assertion |
|---|---|
| Fresh DB, up all | All migrations applied; history consistent; expected tables exist. |
| Already-applied idempotent | Re-run up: no-op, no error, history unchanged. |
| Down one | Last migration reverted; history shrunk. |
| Down all | Clean schema; history empty. |
| Partial failure mid-way | Earlier applied; failing and later not applied; history consistent. |
| Goto specific version | Driver moves to exactly that version, up or down. |
| Concurrent run | Exactly one wins; other waits or errors cleanly (advisory lock). |
| Large corpus (50 migs) | Completes; history accurate. |
| Unicode, long identifiers | Applies without corruption. |
| Rollback on error | Transactional migration reverts; history not updated. |

`pkg/conformance/Suite` is exported. Third parties writing custom drivers import and run the same suite. Each driver has its own `<driver>_conformance_test.go` wiring.

CI runs the suite against both embedded-postgres (fast) and testcontainers-postgres (catches locking/networking quirks). Must pass both to merge.

### Lint

`workflow-migrate lint <source_dir>`:

- Ordering: timestamps monotonically increasing; up/down pairs complete.
- Naming: `YYYYMMDDHHMMSS_snake_case_name.{up,down}.sql`.
- Dangerous ops: `DROP TABLE` without `IF EXISTS`; `ALTER TABLE ALTER COLUMN` without proper defaulting; missing `CREATE INDEX CONCURRENTLY`.
- Reversibility: paired DDL counts match.
- Forbidden patterns (configurable): `TRUNCATE` on prod-marked tables; `DELETE FROM users WHERE ...` without `LIMIT` in down migrations.
- Driver-specific: Atlas HCL lint; golang-migrate/goose via `pg_query_go` AST walks.

Output is structured JSON (consumable by CI). `--fix` auto-renames files for naming/ordering issues.

### `workflow-migrate` standalone

Small static Go binary (CGO_ENABLED=0) published from the same repo. Own Docker image at `gcr.io/distroless/static-debian12:nonroot` base. DO pre-deploy jobs reference `registry.digitalocean.com/bmw-registry/workflow-migrate:${VERSION}`. Takes advisory `pg_try_advisory_lock` as a second defense against concurrent application.

Two flavors:
- All-in-one: all drivers compiled in (~25 MB).
- Slim: golang-migrate + goose only, no Atlas (~10 MB).

Users pick by referencing the matching image in `jobs[].image`.

## Tenancy (workflow core)

### Resolver middleware

`auth.tenant_resolver` module injected as HTTP middleware. Selector chain config:

```yaml
- name: tenant-resolver
  type: auth.tenant_resolver
  config:
    mode: all_must_match          # first_match | all_must_match | consensus
    selectors:
      - type: host
        required: true
      - type: cookie
        name: bmw_tenant
        required: false
      - type: jwt_claim
        claim: tid
        required: true
    registry_ref: tenants-registry
    on_mismatch: error            # error (403) | log_and_error | log_and_prefer_<selector>
    on_missing_required: error
    audit_event: tenant.mismatch
    cache_ttl: 60s
```

Default core selectors: `HostSelector`, `SubdomainSelector`, `HeaderSelector`, `CookieSelector`, `JWTClaimSelector`, `SessionSelector`, `StaticSelector`. Plugins extend via `tenant.selector` capability.

Modes:
- `first_match` — default, legacy. First match wins.
- `all_must_match` — every `required: true` must match; all matching selectors must agree. Mismatch → `on_mismatch` action.
- `consensus` — majority wins; min N-of-M configurable.

Mismatches emit `tenant.mismatch` event to the engine event bus. Auth token blacklist (already in core) subscribes so session-hijack attempts blacklist the offending token.

Tenant resolution injects the full `Tenant` into `context.Context`. Pipeline executor propagates; `${tenant_id}` template resolves in every downstream step.

### Registry

`TenantRegistry` interface; default SQL backend in core, plugin-provided backends register via `tenants.registry` module type with alternative `backend:` config value.

Schema (shipped as an embedded migration via `ProvidesMigrations()`):

```sql
CREATE TABLE IF NOT EXISTS {{ .Schema.FullTableName }} (
  {{ .Schema.PK }}             UUID PRIMARY KEY,
  {{ .Schema.SlugColumn }}     TEXT UNIQUE NOT NULL,
  name                         TEXT NOT NULL,
  {{ .Schema.DomainsColumn }}  TEXT[] NOT NULL DEFAULT '{}',
  {{ .Schema.MetadataColumn }} JSONB NOT NULL DEFAULT '{}',
  {{ .Schema.IsActiveColumn }} BOOLEAN NOT NULL DEFAULT true,
  created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS {{ .Schema.Idx "slug" }}    ON {{ .Schema.FullTableName }} ({{ .Schema.SlugColumn }});
CREATE INDEX IF NOT EXISTS {{ .Schema.Idx "domains" }} ON {{ .Schema.FullTableName }} USING GIN ({{ .Schema.DomainsColumn }});
```

All table/column names are configurable via `tenants.registry` module config and exposed as a service (`TenantSchemaConfig`) for `database.partitioned` to consume. Single source of truth for prefix/suffix/column-name customization.

### Pipeline steps

`step.tenant_ensure` (idempotent upsert), `step.tenant_list`, `step.tenant_get_by_domain`, `step.tenant_update`, `step.tenant_disable`. All respect RBAC via the existing authz plugin.

### `wfctl tenant *` commands

`wfctl tenant ensure|list|get|update|disable`. Core commands (not dynamic) because tenancy is fundamentally core.

### BMW short-term use

`StaticSelector` reading `BMW_TENANT_ID` env. Bootstrap generates `BMW_TENANT_ID`, `BMW_TENANT_SLUG`, `BMW_TENANT_DOMAIN` secrets. Pre-deploy `step.tenant_ensure` job populates the row using the same UUID.

## DO plugin field fill

`workflow-plugin-digitalocean` v0.7.0 rewrites `AppPlatformDriver.buildAppSpec(spec)` to cover the full canonical key set:

- **AppSpec top-level**: `Domains`, `Alerts`, `Ingress`, `Egress`, `Maintenance`, `Features`, `Vpc`, `LogDestinations`.
- **AppServiceSpec**: `InstanceSizeSlug` (via existing `resolveSizing`), `Autoscaling`, `Protocol`, `Routes`, native `HealthCheck`, `CORS`, `InternalPorts`, `BuildCommand`, `RunCommand`, `DockerfilePath`, `SourceDir`, `Termination`, `LivenessHealthCheck`.
- **AppJobSpec**: new — `jobs[]` → `godo.AppJobSpec{Kind: PRE_DEPLOY|POST_DEPLOY|FAILED_DEPLOY|SCHEDULED}`.
- **AppWorkerSpec**, **AppStaticSiteSpec**: new.
- **Sidecars**: translated to sibling `AppServiceSpec` entries with DO internal routing.
- **DO-only**: `DisableEdgeCache`, `DisableEmailObfuscation`, `EnhancedThreatControlEnabled`, `InactivitySleep` from `provider_specific.digitalocean`.

### `appHealthResult` fix

Current code falls through to "no deployment found" when `ActiveDeployment != nil && Phase != ACTIVE`. Fix recognizes `Phase == ERROR/FAILED/CANCELED/SUPERSEDED` and returns `deployment_failed` with the phase string. Same in `APIGatewayDriver`.

### `DatabaseDriver.trusted_sources`

Canonical key `trusted_sources: [{type: app, name: bmw-app}]` maps to `godo.DatabaseFirewallRule`. Types: `app`, `ip_addr`, `k8s`, `droplet`, `tag`. Sibling resource names resolve to DO IDs via state-store lookup. Configuring `trusted_sources` implies default-deny.

### Parity across providers

AWS/GCP/Azure drivers do the same canonical fill (separate workstreams). Each declares `SupportedCanonicalKeys()`; `wfctl validate` errors on unsupported-by-provider keys instead of silently dropping them.

## Hardened base image + containers

### Canonical `Dockerfile.prebuilt`

`gcr.io/distroless/base-debian12:nonroot@sha256:<digest>` (pinned, Dependabot-renewed monthly). No shell, no package manager, no writable root FS.

```dockerfile
FROM gcr.io/distroless/base-debian12:nonroot@sha256:<digest>
WORKDIR /app
COPY --chown=65532:65532 workflow-server /app/workflow-server
COPY --chown=65532:65532 data/plugins/ /data/plugins/
COPY --chown=65532:65532 app.yaml /config/app.yaml
COPY --chown=65532:65532 openapi.json /config/openapi.json
COPY --chown=65532:65532 dist/ /app/ui/dist/
USER 65532:65532
ENTRYPOINT ["/app/workflow-server"]
CMD ["-config", "/config/app.yaml", "-data-dir", "/data"]
```

Two modes:
- **Generic** (`--mode=generic`) — BMW-style; uses the canonical `workflow-server` binary.
- **Library** (`--mode=library --binary=bmw-server`) — custom `main.go` importing workflow as a library; app's own binary name.

`wfctl scaffold dockerfile --mode=...` emits the right template. `ci.build.containers[].base_image` overrides the base; validated (warn on alpine/glibc, block on shell-containing bases unless `allow_shell: true`).

### Exec-form ENTRYPOINT/CMD contract

No shell wrappers. Canonical flags exposed by both modes: `-config`, `-data-dir`, `-env`, `-listen-addr`, `-log-level`, `-tenant-id`, `-version`, `-help`. All accept env-var overrides.

### CGO

Default `CGO_ENABLED=0` (static binaries, work on `distroless/static`). Opt-in `ci.build.targets[].cgo: true` for apps needing native bindings; those land in `distroless/base-debian12`.

### Multi-platform compatibility contract

Canonical Dockerfile template enforces:
1. Exec-form ENTRYPOINT/CMD (PID 1 signals).
2. Non-root USER.
3. Bind to `$PORT` when set (Cloud Run).
4. Graceful SIGTERM drain (`workflow.Engine.Shutdown` respects deadline).
5. Health endpoints at `/healthz`, `/livez`, `/metrics`.
6. No writable FS dependency (writes to mounted volumes or `/tmp`).
7. stdout/stderr logging only.
8. No `root` group membership.

### Sidecars

Canonical `sidecars: [...]` list with `SharesNetworkWith` hint. Per-provider mapping:

- DO App Platform: sibling service with internal routing.
- AWS ECS: extra containerDefinition in same task.
- K8s: extra container in same Pod.
- Cloud Run: validation error (no multi-container support).

BMW's Tailscale sidecar continues working via DO's internal-routing translation.

### Pre-deploy jobs

DO App Platform runs `PRE_DEPLOY` jobs to completion before promoting new revisions. Canonical `jobs[]` maps directly. BMW's migration job:

```yaml
jobs:
  - name: migrate
    kind: pre_deploy
    image: registry.digitalocean.com/bmw-registry/workflow-migrate:${MIGRATE_SHA}
    run_command: /workflow-migrate up
    env_vars_secret: { DATABASE_URL: staging-database-url }
  - name: tenant-ensure
    kind: pre_deploy
    image: registry.digitalocean.com/bmw-registry/workflow-migrate:${MIGRATE_SHA}
    run_command: /workflow-migrate tenant-ensure --slug ${BMW_TENANT_SLUG} --domain ${BMW_TENANT_DOMAIN}
    env_vars_secret: { DATABASE_URL: staging-database-url }
```

Runs **once per deploy** regardless of instance count. Advisory Postgres lock inside `workflow-migrate` is a second defense.

Migrations must be backwards-compatible for one release (additive only); column drops happen in a later deploy. Standard zero-downtime discipline, documented in `workflow-plugin-migrations/docs/`.

### Local dev parity

`wfctl dev up` generates `docker-compose.yaml` with:
1. `postgres:16-alpine` (or embedded — user picks).
2. `workflow-migrate up` as one-shot with `depends_on: [postgres]`.
3. `workflow-server` with `depends_on: [workflow-migrate]`.
4. `workflow-migrate tenant-ensure` one-shot after migrate.

Same execution order as DO pre-deploy. Same images.

## Supply-chain verification

Full feature ownership in `workflow-plugin-supply-chain` v0.3.0. Core provides only the hook protocol and dynamic CLI registration.

### What the plugin ships

- SBOM generation via `anchore/syft` (CycloneDX JSON).
- Signing via cosign keyless (GitHub OIDC → Sigstore/Fulcio/Rekor).
- SLSA Level 3 provenance via `slsa-framework/slsa-github-generator`.
- Verification (cosign verify + OSV vuln scan).
- Build hooks: `post_container_build` (SBOM + sign), `post_artifacts_publish` (sign assets).
- Dynamic CLI: `wfctl supply-chain scan|verify|sbom|report` + `wfctl scaffold sbom|sign` + `wfctl ci doctor`.
- JSON schema for `supply_chain:` config block, exposed via `--wfctl-schema`.
- All docs (overview, enabling-sbom, github-setup, verifying-artifacts, incident-response, compliance-mapping) live in `docs/` of this plugin repo.

### Config

```yaml
ci:
  build:
    containers:
      - name: bmw
        dockerfile: Dockerfile.prebuilt
    supply_chain:
      sbom:
        format: cyclonedx-json
        attach_to_image: true
        publish_as_release_asset: true
      sign:
        mode: keyless
        oidc_issuer: https://token.actions.githubusercontent.com
      provenance:
        slsa_level: 3
      verify_before_push:
        vuln_policy: block-critical
```

### Install-time verification

`wfctl plugin install` gains verification via the supply-chain plugin:

```yaml
requires:
  plugins:
    - name: workflow-plugin-digitalocean
      version: v0.7.0
      verify:
        signature: required
        sbom: required
        vuln_policy: block-critical
```

Default for new configs: `signature: required, sbom: required, vuln_policy: warn`.

### Compliance alignment

SBOMs + signatures + provenance + vuln scanning meet EU CRA + CISA SBOM requirements + SLSA L3 + NIST SSDF. Compliance-mapping doc in the plugin repo explains which controls each feature covers.

## Rollout phases

| Phase | Scope | Ship as |
|---|---|---|
| 1 | Core interfaces + hook protocol + dynamic CLI + scaffolder + canonical keys + tenancy core | workflow v0.18.0 |
| 2 | Migrations plugin + atlas plugin + `workflow-migrate` binary/image | workflow-plugin-migrations v0.1.0 |
| 3 | DO plugin canonical field fill + `appHealthResult` fix + trusted_sources | workflow-plugin-digitalocean v0.7.0 |
| 4 | Supply-chain hooks, CLI, docs migration, `supply_chain:` config | workflow-plugin-supply-chain v0.3.0 |
| 5 | Canonical Dockerfile + BMW adoption | workflow v0.18.0 + BMW PR |
| 6 | BMW `jobs[]` + tenancy config + bootstrap extension | BMW PR |
| 7 | Production deploy + E2E verification | BMW deploy run |

Phase 1 is the largest and blocks everything else. Phases 2, 3, and 4 can run in parallel after Phase 1. Phases 5 and 6 are BMW-scoped and sequenced after their upstream plugin releases are available.

### Rollback strategy

- Phase 1: new interfaces with no existing consumers in core → revert the commit; no downstream impact.
- Phases 2–4: new plugin releases; previous versions remain valid → don't bump BMW's `requires.plugins[]`.
- Phases 5–6: BMW-scoped → git revert PRs.
- Phase 7: bad deploy rolls back via `wfctl infra rollback` + DO deployment history.

## Risks

- **Atlas dependency weight.** If Atlas pulls more than ~5 MB transitive, split further (but single repo already isolates via separate binary).
- **Distroless digest pin renewal cadence.** Weekly may be noisy; start monthly and adjust.
- **BMW static selector in prod.** Fine for staging; prod needs host-based + JWT-claim before customer data lands. Tracked as follow-up.
- **Sidecars as DO sibling services.** Shared-network semantics differ from true k8s sidecars; Tailscale as ingress should still work but needs a smoke test.
- **Hook handler misbehavior.** A slow hook handler blocks the build. Default timeouts per event, documented ways to mark handlers non-blocking.
- **Dynamic CLI conflicts.** Two plugins declaring the same command at startup: error with remediation, but ensures the user must uninstall one. Document this clearly and consider a namespacing fallback (`wfctl <plugin-name> <command>`) later.

## Testing summary

- Schema contract test: every example in `workflow-scenarios` validates against the canonical IaC schema.
- Hook dispatch: priority ordering, payload marshaling, abort vs warn policies.
- Dynamic CLI: registration conflict detection, help-text aggregation, flag passthrough.
- Migration conformance: matrix per driver against embedded-postgres + testcontainers.
- Test harness: full-cycle and checkpoint modes; `--keep-alive` produces a reachable DSN.
- Tenant resolver: per-selector unit tests; combined-mode integrity rejection; session-hijack emulation.
- DO plugin: per-canonical-key translation; `appHealthResult` regression suite; sidecar sibling-service mapping.
- Supply-chain: end-to-end `wfctl build` with plugin installed → SBOM attached → signature verified → release assets signed; `wfctl plugin install` rejects tampered tarball; OSV injection catches a known-CVE package.
- Hardened image: canonical Dockerfiles validate; build+run succeeds in distroless; read-only FS; SIGTERM graceful shutdown within drain deadline.
- BMW e2e: pre-deploy migrate job runs against scratch Postgres → `workflow-server` starts → `/healthz` green → `POST /api/v1/auth/register` succeeds → existing Playwright suite green.

## Follow-ups (post-rollout)

- AWS/GCP/Azure plugin canonical key fill parity.
- BMW tenant admin UI (BMW repo).
- Host-based + JWT-claim tenant resolution in BMW prod.
- Keyed cosign signing for orgs that can't use GitHub OIDC.
- Private Sigstore (Fulcio + Rekor) for sovereign/air-gapped deployments.
- `workflow-plugin-slack-notify`, `workflow-plugin-image-scanner`, `workflow-plugin-registry-mirror` — candidate hook-consumers to prove the extension model.
- Multi-tenant migration strategy (per-tenant schema separation via prefixes once multi-tenant traffic exists).

## Deliverables summary

- **workflow repo** (v0.18.0): new interfaces, hook protocol, dynamic CLI, scaffolder, tenancy core, canonical keys, `ProvidesMigrations()` wiring.
- **workflow-plugin-migrations** (v0.1.0, new repo): three drivers, conformance suite, test harness, lint, `workflow-migrate` binary + OCI image, `wfctl migrate *` CLI.
- **workflow-plugin-digitalocean** (v0.7.0): full canonical key fill, health-check fix, trusted_sources.
- **workflow-plugin-supply-chain** (v0.3.0): hooks, CLI, docs migration, config schema.
- **workflow-registry**: three new/updated manifests (migrations, atlas-migrate, digitalocean v0.7.0, supply-chain v0.3.0).
- **buymywishlist**: canonical Dockerfile, `jobs[]` wiring, tenancy config, bootstrap extension, `supply_chain:` block.
