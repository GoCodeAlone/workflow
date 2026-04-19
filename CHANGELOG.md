# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.14.1] - 2026-04-19

### Added

#### `wfctl build audit` — supply-chain security checks (T34)

- **`wfctl build audit`** — two-layer security audit combining CI config checks and per-target Dockerfile linting.

  **Config-level checks (six):**
  1. `ci.build.security.hardened=false` → WARN
  2. Dockerfile containers without `sbom` or `provenance` configured → WARN
  3. Registries without a `retention:` policy → WARN
  4. `requires.plugins` or `plugins.external` declared without a `.wfctl.yaml` lockfile → WARN
  5. Registry `auth.env` pointing to an env var not set at audit time → WARN
  6. `environments.local.build.security.hardened=false` → NOTE (expected for local dev)

  **Target-level checks:**
  - Calls `builder.SecurityLint(cfg)` for each typed build target (go, nodejs, custom) and aggregates findings.
  - For each `method: dockerfile` container, lints the Dockerfile for:
    - `USER root` → CRITICAL
    - Missing `USER` directive → CRITICAL
    - `FROM <image>:latest` without version pinning → WARN
    - `ADD https?://` URL (untrusted remote fetch) → WARN
    - Embedded secret patterns (`password=`, `token=`, `api_key=`, etc.) → CRITICAL
    - Base image not in `ci.build.security.base_image_policy.allow_prefixes` → WARN (when policy is set)

  **Exit codes:** CRITICAL findings always exit 1. `--strict` also exits 1 on any WARN. Plain runs exit 0 unless CRITICAL.

#### BuildKit provenance attestation (T33)

- When `ci.build.security.hardened=true`, `wfctl build image` appends `--provenance=mode=max` and `--sbom=true` to every `docker build` invocation.
- Emits a warning when `DOCKER_BUILDKIT` is not set to `1`, since BuildKit is required for provenance attestation to work.

#### GitLab Container Registry provider (T31)

- **`plugins/registry-gitlab`** — full implementation replacing the stub:
  - `Login`: uses `gitlab-ci-token` + `$CI_JOB_TOKEN` in CI context; falls back to `oauth2` + `auth.env` token.
  - `Push`: `docker push <ref>` (GitLab accepts anything under the logged-in registry path).
  - `Prune`: calls GitLab API (`GET /api/v4/projects/:id/registry/repositories` + `DELETE .../tags/:name`) to delete tags beyond `retention.keep_latest`.

## [0.14.0] - 2026-04-19

### Added

#### `wfctl build` command family

- **`wfctl build`** — top-level orchestrator; chains `go → ui → image → push` per `ci.build` config. Flags: `--config`, `--dry-run`, `--only`, `--skip`, `--tag`, `--format json|yaml|table`, `--no-push`, `--env`.
- **`wfctl build go`** — builds all `type: go` targets via the built-in Go builder plugin. `--target` flag to select a single target.
- **`wfctl build ui`** — builds all `type: nodejs` targets via the built-in Node.js builder plugin.
- **`wfctl build image`** — builds all `ci.build.containers[]` entries. Supports `method: dockerfile` (BuildKit with secrets/cache/platforms) and `method: ko`. External images (`external: true`) are resolved via the `tag_from` chain instead of being built.
- **`wfctl build push`** — pushes each container's image refs to registries declared in `push_to[]`.
- **`wfctl build custom`** — runs all `type: custom` targets via the custom builder plugin.

#### Builder plugin contract (`plugin/builder`, `plugins/builder-*`)

- **`plugin/builder.Builder` interface** — `Name()`, `Validate(cfg)`, `Build(ctx, cfg, out)`, `SecurityLint(cfg)` contract for all builder plugins.
- **Built-in `go` builder** — invokes `go build` with ldflags, tags, CGO, cross-compilation. SecurityLint warns on secret-embedding ldflags, CGO without link_mode, and unknown builder images.
- **Built-in `nodejs` builder** — runs `npm ci && npm run <script>` (or yarn/pnpm). SecurityLint warns on missing `package-lock.json`.
- **Built-in `custom` builder** — runs arbitrary shell commands via `sh -c`. Always emits a SecurityLint warn.
- **Builder registry** (`plugin/builder/registry.go`) — `Register`, `Get`, `List` with init-time registration.

#### `wfctl registry` container commands

- **`wfctl registry login`** — logs into declared registries. DO provider uses `doctl registry login`; GHCR provider uses `docker login ghcr.io --password-stdin`.
- **`wfctl registry push`** — pushes image refs to declared registries.
- **`wfctl registry prune`** — runs `doctl registry garbage-collection` + tag pruning (DO provider); GH API version pruning (GHCR provider). Preserves `latest`. Dry-run supported.
- **RegistryProvider interface** (`plugin/registry/provider.go`) — `Name()`, `Login()`, `Push()`, `Prune()` with `Context` carrying `io.Writer` + `dryRun`.
- **DO provider** (`plugins/registry-do`) — full Login/Push/Prune implementation.
- **GHCR provider** (`plugins/registry-github`) — full Login/Push/Prune implementation via GH API.
- **Stub providers** for GitLab, AWS, GCP, Azure — register and return `ErrNotImplemented`; full implementations in future releases.

#### Config schema additions

- **`ci.build.targets[]`** — typed build targets with `name`, `type`, `path`, `config`, `environments` overrides. Supersedes `binaries:` (legacy `binaries:` auto-coerced with deprecation warning).
- **`ci.registries[]`** — `CIRegistry` + `CIRegistryAuth` + `CIRegistryRetention` types. Retention supports `keep_latest`, `untagged_ttl`, `schedule`.
- **`CIContainerTarget` extensions** — `method`, `ko_*`, `platforms`, `build_args`, `secrets`, `cache`, `target`, `labels`, `extra_flags`, `external`, `source` (with `tag_from` chain), `push_to`.
- **`CIBuildSecurity`** — `hardened`, `sbom`, `provenance`, `sign`, `non_root`, `base_image_policy`. Defaults applied automatically at `LoadFromFile` time: `hardened: true`, `sbom: true`, `provenance: slsa-3`, `non_root: true`.
- **`TagFromEntry`** — `env` + `command` fields for tag resolution chains on external images.
- **`EnvironmentConfig.Build`** — `*CIBuildConfig` field for per-environment build overrides consumed by `wfctl dev up`.
- **`PluginRequirement` auth** — `source` + `auth.env` fields for private plugin repos.

#### Plugin install enhancements

- **`wfctl plugin install --from-config <file>`** — batch-installs `requires.plugins[]` from a workflow config; skips already-installed plugins.
- **`wfctl plugin lock`** — regenerates the `.wfctl.yaml` lockfile from `requires.plugins[]`.
- **Private repo auth** — `requires.plugins[].auth.env` triggers `git config --global url."...".insteadOf` + `GOPRIVATE` injection before fetching; undone after install.

#### Supply-chain hardening

- **Hardened defaults** applied at load time (`LoadFromFile` → `applyBuildDefaults()`).
- **`CIConfig.ValidateWithWarnings()`** — emits `"hardened defaults disabled — images may not meet supply-chain baseline"` when `security.hardened: false`.
- **SBOM generation** (`build_sbom.go`) — shells out to `syft` binary; attaches via `oras attach` or `cosign attach sbom`. No-op when `security.sbom: false`.

#### Local dev symmetry

- **`environments.local.build`** overrides — target config keys merged by name; env keys win over base.
- **Local hardening skip** — when `env == "local"`, auto-applied hardened defaults are replaced with `{hardened: false, sbom: false}` for fast iteration.
- **Local Docker cache** — container targets under `env == "local"` get `cache.from: [{type: local}]` injected.
- **`wfctl dev up`** now calls `runDevBuild(cfgPath, "local")` before starting services.

#### External image support

- **`external: true`** on container targets — skips `docker build`; resolves image ref via `source.tag_from` chain (env var → shell command → fallback).
- **`ResolveTag`** (`build_resolve_tag.go`) — generic tag resolver used by external and built containers.

#### CI init emitter

- **`wfctl ci init`** now emits three files for GitHub Actions:
  - `.github/workflows/ci.yml` — build + test (unchanged)
  - `.github/workflows/deploy.yml` (**new**) — minimal ~45-line pipeline with `workflow_run` trigger, `build-image` job using `wfctl build --push --format json`, chained `deploy-*` jobs with correct SHA pinning (`${{ github.event.workflow_run.head_sha || github.sha }}`), and `concurrency` block.
  - `.github/workflows/registry-retention.yml` (**new, conditional**) — emitted when any registry has `retention.schedule`; wraps `wfctl registry prune`.

#### Documentation

- **Tutorial**: `docs/tutorials/build-deploy-pipeline.md` — 16-section step-by-step guide from hello-world Go deployment to polyglot multi-registry pipeline with signing and attestation.
- **Manual** (`docs/manual/build-deploy/`): 9-page reference covering `ci.build` schema, `ci.registries` schema, deploy environments, builder plugins, CLI reference, auth providers, security hardening, local dev, and troubleshooting.

### Changed

- **`wfctl registry`** now refers to **container registry** commands (`login`, `push`, `prune`). The plugin catalog command is renamed to **`wfctl plugin-registry`**. A deprecation alias `wfctl registry` (routing to `wfctl plugin-registry`) is kept until v1.0.
- **`ci.build.security` defaults are on** — configs that omit `ci.build.security` get `hardened: true`, `sbom: true`, `provenance: slsa-3`, `non_root: true` applied automatically.

### Notes

- **Hardened defaults are on by default.** Opt out with `ci.build.security.hardened: false` — a supply-chain warning is emitted. See [Security Hardening](docs/manual/build-deploy/07-security-hardening.md).
- Rust, Python, JVM, cmake builder plugins ship as separate `workflow-plugin-builder-*` packages.
- Kubernetes, ECS, Nomad, Cloud Run deploy targets ship as separate provider plugins.
- GitLab/AWS/GCP/Azure registry providers are stub-registered in v0.14.0; full implementations tracked in the issue tracker.

### Links

- [Build + Deploy Pipeline Tutorial](docs/tutorials/build-deploy-pipeline.md)
- [Manual: ci.build schema](docs/manual/build-deploy/01-ci-build-schema.md)
- [Manual: CLI reference](docs/manual/build-deploy/05-cli-reference.md)
- [Manual: Security hardening](docs/manual/build-deploy/07-security-hardening.md)

### Deferred to v0.14.1

- **GitLab registry auth** (T31) — full GitLab Container Registry Login/Push/Prune implementation.
- **BuildKit provenance attestation** (T33) — `--provenance=mode=max` attestation via BuildKit.
- **`wfctl build --security-audit`** (T34) — Dockerfile and builder config linting; exits 1 on critical findings (e.g. `USER root`, embedded secrets, policy violations).

## [Unreleased]

### Added

- **`wfctl dev`** (`cmd/wfctl/dev.go`, `dev_compose.go`, `dev_process.go`, `dev_k8s.go`, `dev_expose.go`): local development cluster management. Subcommands: `up`, `down`, `logs`, `status`, `restart`. Three modes: docker-compose (default), process (`--local`, with hot-reload via fsnotify), and minikube (`--k8s`). Exposure integrations: Tailscale Funnel, Cloudflare Tunnel, ngrok (`--expose`). Auto-detects `environments.local.exposure.method` when `--expose` is omitted.
- **`wfctl wizard`** (`cmd/wfctl/wizard.go`, `wizard_models.go`): interactive Bubbletea TUI wizard for project setup. Eleven screens: project info → services → infrastructure → infra resolution (per-env strategy) → environments → deployment → secret stores → secret routing → secret values → CI/CD → review → write. New screens vs prior version: per-environment infra resolution (container/provision/existing with connection details for "existing"), named secret store configuration with add/remove flow, per-secret store routing (← → to assign), and bulk hidden secret input with Ctrl+G auto-generation for keys/tokens. Generates a complete `app.yaml` including `secretStores:` and per-secret `store:` routing.
- **`wfctl secrets setup`** (`cmd/wfctl/secrets_setup.go`): standalone interactive command to set all secrets for a given environment. Reads `secrets.entries` from config, resolves store per secret (env override → per-secret store → defaultStore → legacy provider), prompts with hidden terminal input, and supports `--auto-gen-keys` flag to auto-generate random hex values for names ending in `_KEY`, `_SECRET`, `_TOKEN`, or `_SIGNING`.
- **Plugin manifest `moduleInfraRequirements`** (`config/plugin_manifest.go`): `PluginManifestFile` struct with `moduleInfraRequirements` map, `ModuleInfraSpec`, and `InfraRequirement`. Allows plugin authors to declare infrastructure dependencies (type, name, Docker image, ports, secrets, providers) per module type.
- **Multi-store secrets** (`config/secrets_config.go`, `config/config.go`): `SecretStoreConfig`, `SecretStores` map on `WorkflowConfig`, `DefaultStore` on `SecretsConfig`, and `Store` field on `SecretEntry` for per-secret store routing.
- **Per-environment infra resolution** (`config/infra_resolution.go`): `InfraEnvironmentResolution` with `strategy` (container/provision/existing), `dockerImage`, `port`, `provider`, `config`, and `connection` (host/port/auth). Added `Environments` map to `InfraResourceConfig`.
- **`SecretsProvider.Check()`** (`cmd/wfctl/secrets_providers.go`): `SecretState` enum (Set/NotSet/NoAccess/FetchError/Unconfigured) and `Check()` method on the interface with `envProvider` implementation. `SecretStatus` now includes `Store` and `State` fields.
- **Multi-store secret resolution** (`cmd/wfctl/secrets_resolve.go`): `ResolveSecretStore` (priority: env override → per-secret store → defaultStore → legacy provider → "env"), `getProviderForStore` (maps SecretStores config to providers), `buildSecretStatuses` (access-aware status for `wfctl secrets list`).
- **`detect_infra_needs` plugin manifest integration** (`cmd/wfctl/plugin_infra.go`, `mcp/scaffold_tools.go`): `LoadPluginManifests` and `DetectPluginInfraNeeds` scan local plugin directories for `plugin.json` manifests and surface module-type infra requirements. MCP tool gains optional `plugins_dir` parameter.

### Documentation

- `docs/WFCTL.md`: updated `wizard` reference (11 screens, new navigation keys); added `secrets setup` command reference with flag table and examples; updated `secrets list` description to mention multi-store routing.
- `docs/dsl-reference.md` + `cmd/wfctl/dsl-reference-embedded.md`: expanded `infrastructure` fields with per-env resolution strategies, connection config, and extended example; added `secretStores:` section with example and relationships; updated `secrets:` section with `defaultStore`, per-secret `store` field, multi-store example, and `secrets setup` CLI command.
- `docs/plugin-manifest-guide.md`: new guide for plugin authors on declaring infrastructure requirements in `plugin.json` via `moduleInfraRequirements`.



- **`services:` config section** (`config/services_config.go`): new top-level YAML key for multi-service applications. Each service declares a binary path, scaling policy (replicas/min/max/metric/target), exposed ports, per-service modules/pipelines, and plugins.
- **`mesh:` config section** (`config/services_config.go`): inter-service communication config. Declares transport (nats/http/grpc), service discovery, NATS connection details, and explicit service-to-service routes with via/subject/endpoint.
- **`networking:` config section** (`config/networking_config.go`): network exposure and policy config. Declares ingress entries (service, port, TLS termination), inter-service network policies, and DNS records.
- **`security:` config section** (`config/security_config.go`): application security policies. Fields: `tls` (internal/external/provider/minVersion), `network` (defaultPolicy), `identity` (provider/perService), `runtime` (readOnlyFilesystem/noNewPrivileges/runAsNonRoot/drop+addCapabilities), `scanning` (containerScan/dependencyScan/sast).
- **`wfctl ports list`** (`cmd/wfctl/ports.go`): scans config modules, `services[*].expose`, and `networking.ingress` for port bindings; prints a table with service, module, port, protocol, and exposure classification (public/internal).
- **`wfctl security audit`** (`cmd/wfctl/security_cmd.go`): reports TLS, network policy, ingress TLS, auth module, runtime hardening, and scanning issues with severity HIGH/WARN/INFO. Exits non-zero on any HIGH finding.
- **`wfctl security generate-network-policies`** (`cmd/wfctl/security_cmd.go`): generates Kubernetes `NetworkPolicy` YAML from `networking.policies` + `mesh.routes`; outputs one file per service to `--output` directory (default: `k8s/`).
- **Validation** (`config/services_config_validate.go`, `config/networking_config_validate.go`): `ValidateServices` (scaling constraints, port ranges), `ValidateMeshRoutes` (from/to reference known services, via transport valid), `ValidateNetworking` (ingress service/port exists and is exposed, TLS provider valid, policy from required), `ValidateSecurity` (TLS provider valid), `CrossValidate` (warns on exposed ports with no ingress route). All wired into `wfctl validate`.

- **`ci:` config section** (`config/ci_config.go`): new top-level YAML key declaring build (binaries/containers/assets), test (unit/integration/e2e with ephemeral deps), deploy (per-environment with strategy/healthCheck/approval), and infra phases. Includes `Validate()` method enforcing required fields.
- **`environments:` config section** (`config/environments_config.go`): named deployment environments with provider, region, env vars, secrets provider, and exposure config (Tailscale Funnel, Cloudflare Tunnel, port-forward).
- **`secrets:` config section** (`config/secrets_config.go`): provider, rotation policy (with `Strategy` field for `dual-credential`/`graceful`/`immediate`), and declared secret entries with per-secret rotation overrides.
- **`wfctl ci run`** (`cmd/wfctl/ci_run.go`): executes build, test, and deploy phases from the `ci:` config section. Build phase cross-compiles Go binaries and builds containers. Test phase supports ephemeral Docker deps (postgres/redis/mysql) via `needs:`. Deploy phase is stubbed for Tier 2.
- **`wfctl ci init`** (`cmd/wfctl/ci_init.go`): generates bootstrap CI YAML for GitHub Actions (`.github/workflows/ci.yml`) or GitLab CI (`.gitlab-ci.yml`), with per-environment deploy jobs derived from `ci.deploy.environments`.
- **`wfctl secrets`** (`cmd/wfctl/secrets.go`, `secrets_detect.go`, `secrets_providers.go`): secret lifecycle management. Subcommands: `detect` (scan config for secret-like values), `set` (with `--value` or `--from-file`), `list`, `validate`, `init`, `rotate`, `sync`. `SecretsProvider` interface with `env` backend.
- **`wfctl validate`**: now validates `ci:` sections using `CIConfig.Validate()` when present.

### Documentation

- `docs/dsl-reference.md` + `cmd/wfctl/dsl-reference-embedded.md`: added `services:`, `mesh:`, `networking:`, and `security:` sections with full field reference and examples; also added `ci:`, `environments:`, and `secrets:` sections.
- `docs/WFCTL.md`: added `ports list`, `security audit`, and `security generate-network-policies` command documentation; updated command tree and category table. Also added `ci run`, `ci init`, and `secrets` documentation.

---

## [0.13.0] - 2026-04-17

### Added

- `wfctl infra plan|apply|bootstrap|destroy|status|drift` now accept `--env <name>`.
- `wfctl infra import` does not currently accept `--env`; env-scoped imports will land alongside config-aware import in a follow-up.
- Module configs support an `environments:` block for per-environment resolution (provider/config/image). Set an env value to `null` to skip the module in that env.
- Top-level `environments:` `envVars` are merged into container resources during infra apply; `region` and `provider` default from `environments[env]` when a module omits them.
- `wfctl infra` now honors `imports:` (consistent with every other wfctl subcommand).

### Notes

- `ci.deploy.environments[].requireApproval` continues to work via `wfctl ci init` emitting `environment: <name>` in generated GitHub Actions. No engine change needed — GitHub's native environment approval UI handles the gate.
- `InfraResourceConfig` (under `infrastructure.resources:`) already had an `Environments` field but was never wired to `wfctl infra` (which parses `modules:`). Multi-env is now wired to `ModuleConfig`; `InfraResourceConfig` consumption is deferred to a follow-up and `infrastructure.resources:` remains unused by wfctl infra commands in this release.

### Fixed

- `ModuleConfig` previously lacked an `Environments` field; it was defined on the unused `InfraResourceConfig` type. Multi-env is now wired to the schema `wfctl infra` actually parses.

## [0.4.1] - 2026-03-27

### Fixed
- Editor schemas golden file updated to reflect v0.4.0 schema changes (release CI fix)

## [0.4.0] - 2026-03-27

This release eliminates all `type: "json"` schema fields, replacing them with proper typed fields (`map`, `array`, or individual sub-fields). This improves the visual editor experience — config fields that previously rendered as raw JSON textareas now render as structured form widgets. See the [migration guide](docs/migrations/v0.4.0-schema-types.md) for details.

### Breaking Changes (Editor Schema)
- **88 config fields changed from `type: "json"` to typed schemas.** If your tooling parses `wfctl editor-schemas` output and relies on specific field types, those fields are now `map`, `array`, or individual typed fields instead of `json`. The engine runtime behavior is unchanged — this only affects editor/UI consumers.
- **`wfctl editor-schemas` now includes `stepSchemas`** alongside `moduleSchemas` and `coercionRules`. Consumers parsing this JSON output will see a new top-level key.
- **workflow-editor npm package:** The static `MODULE_TYPES` array has been removed. `MODULE_TYPE_MAP` is still exported but now sourced from `engine-schemas.json` instead of a hand-maintained array. If you imported `MODULE_TYPES` directly, switch to `MODULE_TYPE_MAP` or `getEngineModuleTypes()`.

### Added
- **DSL Reference** (`docs/dsl-reference.md`): Canonical specification for the workflow YAML DSL covering all 12 top-level sections (application, modules, workflows, pipelines, triggers, imports, config providers, platform/infrastructure/sidecars)
- **`wfctl dsl-reference` command**: Extracts the DSL reference as structured JSON for consumption by editors and IDE plugins
- **Step schemas in `wfctl editor-schemas`**: 182 step type schemas now exported alongside 279 module type schemas
- **Struct-tag reflection generator** (`pkg/schema/reflect.go`): `GenerateConfigFields()` produces `[]ConfigFieldDef` from Go struct `editor:"..."` tags, enabling config structs to be the source of truth for editor schemas
- **Editor struct tags** on 5 key config structs (`DatabaseConfig`, `HTTPServer`, `RedisNoSQLConfig`, `HealthCheckerConfig`, `MetricsCollectorConfig`) as the initial adoption of the reflection-based schema pattern
- **LSP hover documentation**: `textDocument/hover` returns DSL reference descriptions and schema details for YAML keys, module types, and step types — surfaces in both VS Code and JetBrains
- **LSP completion with DSL descriptions**: `textDocument/completion` suggests top-level keys, module types, step types, and config keys with descriptions from the DSL reference and schema registry
- **CI contract test** (`schema/schema_contract_test.go`): Zero-tolerance enforcement for `type: "json"` fields in both built-in registries and plugin schemas — any new json-typed field fails CI

### Changed
- 88 `FieldTypeJSON` config fields converted to proper typed schemas across built-in modules (39), built-in steps (12), and plugins (37)
- Schema contract test ratcheted from `maxAllowed: 51` → unconditional zero tolerance

### Changed
- Switch yaegi dependency from `github.com/traefik/yaegi` v0.16.1 to `github.com/GoCodeAlone/yaegi` v0.17.0 (our maintained fork)
  - Eval/EvalPath now recover from panics instead of crashing the host process
  - Fixed binary channel type alias nil pointer, binary-to-source interface conversion
  - 11-45x faster `yaegi extract` with x/tools/go/packages
  - Generic function import support via `//yaegi:add` directive

### Added
- Dynamic field mapping with FieldMapping type supporting fallback chains and primary/resolve/set operations
- Schema-agnostic field resolution for REST API handler modules (42+ references refactored)
- Runtime field resolution from workflow context via FieldMapping.Resolve()
- Configurable field aliases in YAML (fieldMapping, transitionMap, summaryFields)
- Engine integration: fieldMapping/transitionMap/summaryFields wired from YAML config
- 18 unit tests for FieldMapping type
- Multi-chat UI view for responders with real-time conversation switching
- Updated screenshots and user guide documentation
- Go 1.26 upgrade and security fixes

---

## [0.5.3] - 2026-03-28

### Changed
- Bump modular to v1.13.0 (consolidated sub-modules)

---

## [0.5.2] - 2026-03-28

### Changed
- Extract `TemplateEngine` to `pipeline/` package; decouple plugins from `module` package (SDK boundary)

---

## [0.5.1] - 2026-03-28

### Fixed
- Implement `http.Hijacker` and `http.Flusher` interfaces on `trackedResponseWriter`
- Bump modular dependencies to v1.12.5 / v1.15.0 / v2.8.0

---

## [0.5.0] - 2026-03-28

### Added
- **Expr template engine** — `${ }` syntax for pipeline step `config` values alongside the existing `{{ }}` Go template syntax; both may be mixed in the same string
  - Bracket notation for hyphenated step names: `${ steps["step-name"]["key"] }`
  - Boolean/comparison guards: `${ status == "active" && count > 0 }`
  - String concatenation: `${ "Hello " + body["name"] }`
  - Template functions in expr context: `${ upper(name) }`
- **`skip_if` expr support** — pipeline steps accept `skip_if: ${ expr }` for inline conditional skipping
- **`wfctl expr-migrate` command** — auto-converts Go template (`{{ }}`) expressions to expr syntax (`${ }`) with `--dry-run`, `--output`, and `--inplace` modes; complex patterns receive `# TODO: migrate` comments
- **LSP hover/completion for `${ }`** — IDE plugins surface available namespaces and function signatures inside expr expressions
- **docs/dsl-reference.md expr syntax section** — documents all expr namespaces, operators, and migration path
- **docs/migrations/v0.5.0-expr-templates.md** — step-by-step migration guide for upgrading configs from Go templates to expr syntax

### Fixed
- Eviction added to unbounded rate-limit, cache, and lock maps (memory leak)
- Panic recovery added to 9 goroutine sites
- EventProcessor goroutine leak and CronScheduler data race
- HTTP stability: ListenError channel, trackedResponseWriter race, SSE lock contention
- Release RLock before handler dispatch; panic recovery in handler goroutines

---

## [0.4.0] - 2026-02-11

### Added
- **AI Server Bootstrap**
  - cmd/server/main.go with HTTP mux and AI handler registration
  - CLI flags for config, address, AI provider configuration
  - Graceful shutdown with signal handling
  - initAIService with conditional Anthropic/Copilot provider registration
  - cmd/server/main_test.go with route verification tests
- **Go Test Coverage Push to 80%+**
  - Root package (engine_test.go): 68.6% -> 80%+
  - Module package: 77.1% -> 80%+
  - Dynamic package: 75.4% -> 80%+
  - AI packages: maintained at 85%+
- **11 New Playwright E2E Test Suites**
  - Shared helpers (helpers.ts) with complete module type map
  - deep-module-coverage.spec.ts: all 30 module types verified
  - deep-complex-workflows.spec.ts: multi-node workflow tests
  - deep-property-editing.spec.ts: all field types tested
  - deep-keyboard-shortcuts.spec.ts: shortcut verification
  - deep-ai-panel.spec.ts: AI Copilot panel tests
  - deep-component-browser.spec.ts: Component Browser tests
  - deep-import-export.spec.ts: complex round-trip tests
  - deep-edge-cases.spec.ts: edge case coverage
  - deep-accessibility.spec.ts: a11y testing
  - deep-toast-notifications.spec.ts: toast behavior tests
  - deep-visual-regression.spec.ts: visual regression baselines

---

## [0.3.0] - 2026-02-10

### Added
- **EventBus Bridge**
  - EventBusBridge adapter bridging MessageBroker interface to EventBus
  - WorkflowEventEmitter with lifecycle events (workflow.started, workflow.completed, workflow.failed, step.started, step.completed, step.failed)
- **EventBus Trigger**
  - EventBusTrigger for native EventBus subscriptions
  - Configurable topics, event filtering, and async mode
  - Start/Stop with subscription lifecycle management
- **Engine Integration**
  - Engine integration with workflow/step event emission
  - canHandleTrigger support for "eventbus" trigger type
  - TriggerWorkflow emits start/complete/fail events
- **UI Updates**
  - messaging.broker.eventbus module type in NodePalette (30 total)

---

## [0.2.0] - 2026-02-09

### Added
- **Observability Foundation**
  - MetricsCollector wrapping Prometheus with 6 pre-registered metric vectors and `/metrics` endpoint
  - HealthChecker with `/health`, `/ready`, `/live` endpoints and auto-discovery
  - RequestIDMiddleware with `X-Request-ID` propagation and UUID generation
- **Database Module**
  - WorkflowDatabase wrapping `database/sql` with Query, Execute, InsertRow, UpdateRows, DeleteRows
  - SQL builder helpers (BuildInsertSQL, BuildUpdateSQL, BuildDeleteSQL)
  - DatabaseIntegrationConnector adapter for integration workflows
- **Data Transformation**
  - DataTransformer with named pipelines of operations (extract, map, filter, convert)
  - Dot-notation JSON path extraction with array index support
- **Webhook Delivery**
  - WebhookSender with exponential backoff retry (configurable maxRetries, backoff, timeout)
  - Dead letter queue for exhausted retries with manual RetryDeadLetter support
- **AI Validation Loop**
  - Validator with compile-test-retry cycle (import validation, Yaegi compile, required function check)
  - ValidateAndFix integrating AI regeneration on validation failure
  - ContextEnrichedPrompt for module/service-aware generation
- **Dynamic-to-Modular Bridge**
  - ModuleAdapter wrapping DynamicComponent as modular.Module
  - Configurable provides/requires for dependency injection
  - Engine integration via `dynamic.component` module type
- **UI Updates**
  - 2 new categories: Database, Observability (10 total)
  - 6 new MODULE_TYPES: database.workflow, metrics.collector, health.checker, http.middleware.requestid, data.transformer, webhook.sender (29 total)
  - Updated component tests for new types and categories

---

## [0.1.0] - 2026-02-08

### Added
- **Core Engine**
  - Workflow engine with BuildFromConfig, TriggerWorkflow lifecycle
  - HTTP, Messaging, State Machine, Event workflow handlers
  - HTTP, Schedule, Event trigger system
  - Module factory pattern for extensible module types
- **Modular Framework**
  - Migration from GoCodeAlone/modular to CrisisTextLine/modular fork (v1.11.11)
  - Integration of all modular modules: httpserver, httpclient, chimux, scheduler, eventbus, eventlogger, cache, database, auth, jsonschema, reverseproxy
- **Dynamic Component System (Yaegi)**
  - Interpreter pool with sandboxed execution
  - Component registry with lifecycle management
  - File watcher for hot-reload
  - Source validation (stdlib-only imports)
- **AI-Powered Generation**
  - WorkflowGenerator interface with LLM + Copilot SDK backends
  - Anthropic Claude direct API client with tool use
  - GitHub Copilot SDK integration with session management
  - Deploy service bridging AI generation to dynamic components
- **ReactFlow UI**
  - Drag-and-drop node palette with 8 categorized module groups (23 types)
  - Property panel for node configuration
  - YAML import/export with round-trip fidelity
  - Undo/redo with history management
  - Validation (local + server) and Zustand state management
- **Test Infrastructure**
  - Go unit tests: module 73%, ai/llm 85%, dynamic 74%
  - Playwright E2E: app-load, node-operations, connections, import-export, toolbar
  - Vitest component tests: 100 tests across 6 files
- **CI/CD**
  - GitHub Actions: automated testing on Go 1.23/1.24, linting, multi-platform releases
  - Code coverage reporting via Codecov
  - Weekly dependency updates

### Changed
- Upgraded to Modular v1.3.9 with IsVerboseConfig, SetVerboseConfig, SetLogger support
- Improved error handling for service registration and I/O operations

### Fixed
- Critical error checking issues identified by linters
- HTTP response writing error handling
- Service registration error handling in engine

### Security
- Enhanced dependency management with automated updates
- Improved error handling to prevent potential runtime issues

## [Previous Versions]

Previous version history was not maintained in changelog format.
