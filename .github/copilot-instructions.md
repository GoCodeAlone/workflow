# Workflow Engine — Copilot Instructions

Configuration-driven workflow orchestration engine (Go 1.26+) built on [`GoCodeAlone/modular`](https://github.com/GoCodeAlone/modular). YAML configs become running applications — API servers, event pipelines, state machines, scheduled jobs, and more — with 90+ built-in module types.

## Build, Test, Lint

```sh
# Build (prefer make targets over raw go build)
make build-go          # builds server binary → ./server
make build-wfctl       # builds CLI → ./wfctl
make build             # builds UI + server (full build)
make build-examples    # builds example/ directory

# Test
make test              # go test -race ./...
go test -run TestName ./module/       # single test
go test -run TestName -v ./...        # single test, verbose, all packages
go test -cover ./...                  # with coverage
make test-configs                     # validate example YAML configs load correctly

# Lint
make lint              # golangci-lint run --timeout=5m
make fmt               # go fmt ./...
make vet               # go vet ./...
make ci                # fmt + vet + test + lint (pre-push suite)

# Benchmarks
make bench             # full benchmark suite
make bench-baseline    # save baseline (count=6)
make bench-compare     # compare current vs baseline with benchstat

# UI (React + Vite)
cd ui && npm ci && npm run dev        # dev server
cd ui && npm test -- --run            # run tests
cd ui && npm run lint                 # ESLint
```

## Architecture

### Engine Core

`StdEngine` (root package `workflow`) is the central orchestrator:

- **`engine.go`** — `StdEngine` struct: holds the `modular.Application`, workflow handlers, loaded plugins, module/step/trigger factories, pipeline registry, secrets resolver, and infra provisioner.
- **`engine_builder.go`** — Fluent builder: `NewEngineBuilder().WithApplication(app).WithAllDefaults().WithPlugin(p).Build()`.
- **`interfaces/`** — Shared interfaces (`Trigger`, `StepRegistrar`, etc.) to avoid import cycles.

### Plugin System (the primary extension mechanism)

Everything is a plugin. The `EnginePlugin` interface (`plugin/engine_plugin.go`) provides:
- `ModuleFactories()` — register new module types
- `StepFactories()` — register new pipeline step types
- `TriggerFactories()` — register new trigger types
- `WorkflowHandlers()` — register new workflow handler types
- `ModuleSchemas()` / `StepSchemas()` — UI metadata
- `WiringHooks()` — post-init cross-module wiring
- `ConfigTransformHooks()` — pre-registration config transforms
- `DeployTargets()` / `SidecarProviders()` — deployment extensions

**Built-in plugins** live in `plugins/` (~32 packages: http, auth, messaging, statemachine, pipelinesteps, ai, storage, observability, etc.). They're loaded in `cmd/server/main.go` via `allplugins`.

**External plugins** (`workflow-plugin-*` sibling repos) are separate Go modules with a `plugin.json` manifest declaring capabilities (module types, step types, tier, minEngineVersion).

### Pipeline & Expression Engines

Pipelines are the primary way work flows through the system. Two template/expression syntaxes:

- **Go templates** `{{ .steps.prev.output }}` — processed by `pipeline/template.go` (`TemplateEngine`)
- **Expr-lang** `${ steps.prev.output }` — processed by `pipeline/expr.go` (`ExprEngine`, powered by `expr-lang/expr`)

Pipeline steps live in `module/pipeline_step_<name>.go` and are registered via `plugins/pipelinesteps/plugin.go`.

### Module System

All module implementations live in `module/` (~277 source files). Major categories:
- **HTTP**: server, router, handlers, middleware (auth, CORS, rate-limit, security headers, OTEL), reverse proxy
- **Messaging**: Kafka, NATS, memory broker, EventBus bridge
- **Database**: PostgreSQL, SQLite, DynamoDB, Redis, MongoDB
- **Cloud**: AWS (ECS, EKS, S3, Route53, CodeBuild), GCP, Azure, DigitalOcean
- **Platform**: Kubernetes, DNS, networking, IaC state
- **State**: state machine, state tracker, state connector
- **Auth**: JWT, OAuth2, M2M, token blacklist, user store
- **Observability**: OTEL tracing, Prometheus, health checks, SSE tracer
- **Pipeline**: 100+ step types (set, validate, transform, conditional, http_call, db_query, deploy, git, Docker, S3, circuit breaker, feature flags, etc.)

### Binaries (`cmd/`)

- **`cmd/server/`** — Main server. Flags: `-config`, `-addr`, `-jwt-secret`, `-database-dsn`, `-license-key`, etc.
- **`cmd/wfctl/`** — CLI tool with 34 commands (validate, inspect, run, deploy, plugin, mcp, test, secrets, security, wizard, etc.). Commands are defined in `cmd/wfctl/<command>.go`. The CLI itself runs through the workflow engine — commands are pipelines triggered via `"cli"` trigger.
- **`cmd/workflow-lsp-server/`** — LSP server for IDE integration.

### MCP Server (`mcp/`)

Exposes the engine to AI assistants via [Model Context Protocol](https://modelcontextprotocol.io). Tools include `get_module_schema`, `get_step_schema`, `validate_template_expressions`, `infer_pipeline_context`, scaffold tools, and wfctl wrappers.

### Dynamic Hot-Reload (`dynamic/`)

Yaegi-based system (`GoCodeAlone/yaegi` fork) for loading Go components at runtime without recompilation.

## Key Conventions

### File Naming & Registration

- Module implementations: `module/<type>.go` (e.g., `module/http_server.go`)
- Pipeline steps: `module/pipeline_step_<name>.go`
- Template functions: registered in `module/pipeline_template.go` via `templateFuncMap()`
- Module factories: registered in plugin `ModuleFactories()` or `engine.go` `BuildFromConfig()`
- Plugin step/module factories: registered in `plugins/<name>/plugin.go`
- wfctl commands: `cmd/wfctl/<command>.go`

### Testing Patterns

- Tests colocated with source (`*_test.go` alongside `*.go`)
- Shared mocks in `mock/`
- Test helpers: `testhelpers_test.go` (root), `module/module_test_helpers.go`
- Isolated app per test: `modular.NewStdApplication(modular.NewStdConfigProvider(nil), mockLogger)`
- Use `testify` for assertions
- Always test with `-race` flag
- E2E tests at root: `e2e_execution_test.go`, `e2e_middleware_test.go`, etc.
- Use `app.SetConfigFeeders()` in tests, not global `modular.ConfigFeeders` mutation
- Coverage targets: 80%+ for most packages, 85%+ for `ai/`

### YAML Config Structure

```yaml
modules:
  - name: my-server
    type: http.server
    config:
      address: ":8080"

workflows:
  http:
    routes:
      - method: GET
        path: /api/health
        handler: health-handler

pipelines:
  process-request:
    steps:
      - name: validate
        type: step.validate
        config:
          rules: [...]
```

### Documentation Updates

When adding functionality, update the corresponding docs:

| Change | Update |
|---|---|
| New module type | `DOCUMENTATION.md` module table, factory registration |
| New pipeline step | `DOCUMENTATION.md` step table, register in plugin |
| New template function | `DOCUMENTATION.md`, add to `templateFuncMap()` |
| New wfctl command | `docs/WFCTL.md`, update `main.go` usage() |
| New trigger type | `DOCUMENTATION.md` trigger types section |

### Commit Messages

Imperative mood, 50-char summary line. Reference issues with `Fixes #N` or `Relates to #N`.

### Error Handling

Return errors with context (`fmt.Errorf("action: %w", err)`), don't panic. Modules implementing `StartStopModule` must clean up resources in `Stop()`.

## Ecosystem

This repo is the hub of a multi-repo ecosystem under [GoCodeAlone](https://github.com/GoCodeAlone):

- **`modular`** — Core Go application framework (dependency injection, lifecycle, config, observers)
- **`workflow-cloud`** / **`workflow-cloud-ui`** — SaaS control plane (plugin registry, licensing, multi-tenant)
- **`workflow-ui`** — Shared React component library (React 19, Zustand, GitHub Packages)
- **`workflow-editor`** — Visual YAML editor (Vite, Playwright E2E)
- **`workflow-vscode`** / **`workflow-jetbrains`** — IDE plugins (LSP, schema validation, snippets)
- **`workflow-registry`** — Static plugin + template catalog (GitHub Pages)
- **`workflow-plugin-*`** — External plugins (auth, broker, discord, payments, dnd, worldsim, etc.)
- **`workflow-scenarios`** — Docker-based integration test scenarios
