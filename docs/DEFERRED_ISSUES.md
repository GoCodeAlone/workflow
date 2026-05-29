# Deferred Issues — Workflow Codebase Review

Issues found during the fleet review that were **not addressed** in this session, grouped by category and severity.

---

## 🔴 Architecture / Plugin Coupling

These are the most significant issues but carry the highest refactoring risk and scope.

### `cmd/server/main.go` — God Object (`serverApp`)
- `serverApp` struct has 20+ fields that directly reference concrete module types (`*module.V1APIHandler`, `*module.ExecutionTracker`, `*module.RuntimeManager`, etc.)
- This file acts as a central coordinator that is aware of every module in the system, violating plugin isolation.
- **Fix direction:** Break `serverApp` into smaller, domain-scoped structs. Wire modules through interfaces registered at startup rather than being held directly.

### `cmd/server/main.go` — 15+ Concrete Type Assertions for Service Discovery
- Pattern: `if gen, ok := svc.(*module.OpenAPIGenerator); ok { ... }` repeated 15+ times.
- Forces the server to know about every specific module type at compile time.
- **Fix direction:** Define capability interfaces (e.g., `SchemaRegistrar`, `WorkflowStoreProvider`) that modules opt into. Iterate the service registry checking for interface satisfaction instead of type identity.

### `cmd/server/main.go` — Switch on Concrete Module Types
- Pattern around lines 280–295: `case *module.QueryHandler: ...; case *module.CommandHandler: ...`
- **Fix direction:** Define an `ExecutionTrackerSetter` interface and have eligible modules implement it. The engine can then range over services and call the interface method without knowing concrete types.

### `plugins/pipelinesteps/plugin.go` — Plugin Exposes Concrete Handler
- Plugin retains a `*handlers.PipelineWorkflowHandler` field and exposes it via a getter.
- `cmd/server/main.go` calls `pipelinePlugin.PipelineHandler()` then directly sets properties on it, bypassing the plugin interface contract.
- **Fix direction:** Use wiring hooks in the plugin lifecycle to perform setup internally. The caller should never need to reach inside a plugin.

### `handlers/pipeline.go` — Bidirectional Handler↔Module Dependency
- The `handlers` package directly imports and holds concrete types from the `module` package (`*module.Pipeline`, `*module.StepRegistry`).
- This means handlers cannot be tested without instantiating real module types.
- **Fix direction:** Define `PipelineRunner` and `StepRegistry` interfaces in a shared package. `handlers` depends on the interfaces; `module` implements them.

### `plugins/api/plugin.go` — Factory Creates Concrete Types Directly
- Module factories in this plugin call `module.NewQueryHandler()`, `module.NewCommandHandler()` etc. directly.
- Swapping implementations requires modifying the plugin.
- **Fix direction:** Factories should accept constructor functions (or use a registry) so the concrete type can be substituted without touching the plugin.

### `engine.go` — `RoutePipelineSetter` Interface Uses Concrete `*module.Pipeline`
- `SetRoutePipeline(routePath string, pipeline *module.Pipeline)` takes a concrete type, limiting extensibility.
- Noted in `fix-executiontracker-interface` session as out-of-scope due to cascading changes.
- **Fix direction:** Define a `PipelineExecutor` interface with the methods actually called through this interface. Switch the parameter type. Cascading changes required in engine.go, handlers, and tests.

### `module/api_gateway.go` — Global Rate Limiter State
- `globalLimiter *gatewayRateLimiter` is an instance field set via `SetGlobalRateLimit()`, but behaves like a singleton that would be shared unexpectedly in multi-tenant scenarios.
- **Fix direction:** Make the rate limiter a proper dependency injected at construction time, or clarify the isolation contract in documentation.

---

## 🟠 Security

### No Rate Limiting on Auth Endpoints
- `/api/v1/auth/register` and `/api/v1/auth/login` are public endpoints with no rate limiting.
- Exposed to brute-force and credential stuffing attacks.
- **Fix direction:** Apply the existing `ratelimit` middleware to auth routes, or add IP-based throttling at the router level for these specific paths.

### JWT Algorithm Not Explicitly Pinned
- `api/middleware.go` checks `(*jwt.SigningMethodHMAC)` type but does not verify the specific algorithm (HS256 vs HS384 vs HS512).
- **Fix direction:** After the type check, add `if token.Method.Alg() != "HS256" { return nil, fmt.Errorf(...) }` (or make the expected algorithm configurable).

---

## 🟡 Incomplete Implementations / TODOs

### `module/pipeline_step_scan_sast.go`, `_scan_container.go`, `_scan_deps.go`
- All three contain: `// TODO: Execute via sandbox.DockerSandbox once the sandbox package is available.`
- The scan steps are non-functional stubs.
- **Fix direction:** Either integrate with the sandbox package once available, or return a clear `ErrNotImplemented` error rather than silently succeeding.

### `cmd/server/main.go` — API Package Not Implemented
- Contains: `// TODO: Once the api package is implemented, this section will:`
- Unclear scope; the surrounding code may be wiring that is currently dead.
- **Fix direction:** Audit what this section is supposed to do and either implement it or remove the dead code.

### `ai/examples/stock_trading.go`
- Contains: `// In production, call real stock API`
- This is an example file but ships with a stub that silently does nothing.
- **Fix direction:** Either connect to a real/mock data source or add a clear `// This is a demonstration stub` comment and return synthetic data explicitly.

---

## 🟡 Code Smells — Large Files / Functions

### `module/api_handlers.go` — ~1000 Lines, Multiple Responsibilities
- Manages CRUD operations, workflow orchestration, field mapping, state filtering, event publishing, and hardcoded crisis-detection keyword matching all in one file.
- **Fix direction:** Split into: `api_crud_handler.go`, `api_workflow_handler.go`, `api_event_handler.go`. Extract `riskPatterns` to a configurable external source (YAML/DB) rather than hardcoded strings.

### `handlers/integration.go` — Functions Still Too Long After Refactor
- `ConfigureWorkflow()` is ~179 lines and `ExecuteIntegrationWorkflow()` is ~133 lines even after the connector factory refactor.
- **Fix direction:** Extract `parseConnectorConfigs()` and `parseStepConfigs()` helpers from `ConfigureWorkflow()`. Extract the retry loop in `ExecuteIntegrationWorkflow()` into `executeStepWithRetry()`.

### `handlers/integration.go` — Nested Conditionals (4 Levels Deep)
- The retry + error-handler logic around lines 348–399 has 4 levels of nesting.
- **Fix direction:** Apply early-return and extract-method patterns. The retry loop should be its own function.

---

## 🟡 Code Smells — Minor

### Boolean Parameters
- `NewStaticFileServer(root string, spaFallback bool)` — boolean parameter is ambiguous at call site.
- `SetAllowPrivateIPs(allow bool)` in integration connector — should be two methods or an options struct.
- **Fix direction:** Use functional options pattern (`WithSPAFallback()`) or separate `AllowPrivateIPs()` / `DisallowPrivateIPs()` methods.

### `module/api_handlers.go` — Scattered Workflow Config Fields
- Six related fields (`workflowType`, `workflowEngine`, `initialTransition`, `instanceIDPrefix`, `instanceIDField`, `seedFile`) are always set together but live as flat struct fields.
- **Fix direction:** Extract into a `WorkflowConfig` embedded struct.

### Missing Documentation for Several Module Types
- The following features exist in code but are entirely absent from `DOCUMENTATION.md` and `README.md`:
  - `audit/` — audit logging system
  - `plugins/license/` — `license.validator` module
  - `platform.provider`, `platform.resource`, `platform.context` modules
  - `step.ai_complete`, `step.ai_classify`, `step.ai_extract` pipeline steps
  - CI/CD steps: `step.docker_build`, `step.docker_push`, `step.docker_run`, `step.scan_sast`, `step.scan_container`, `step.scan_deps`, `step.artifact_push`, `step.artifact_pull`
  - `step.jq` — JSON query transformations
  - `plugins/admincore/` — admin platform plugin
  - `observability.otel` — no example YAML or configuration guide

### Example Count Exaggeration
- `example/README.md` claims "100+ example configs"; the actual count is ~37.
- **Fix direction:** Count examples accurately and update the claim, or remove the count entirely.
