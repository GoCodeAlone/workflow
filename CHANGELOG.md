# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
