# Module Best Practices

This document defines conventions and guidelines for building modules in the workflow engine. Following these practices ensures modules are composable, testable, and maintainable.

## Core Principles

### 1. Single Responsibility

Each module should do one thing well. A module that validates configs should not also reload the engine. A module that handles REST CRUD should not also manage authentication.

**Good**: `health.checker` -- only checks health and exposes endpoints.
**Good**: `metrics.collector` -- only collects and exposes Prometheus metrics.
**Bad**: A single module that handles config read, config write, config validate, engine reload, AND engine status.

### 2. Clear I/O Signatures

Every module should declare what services it provides and requires through `ProvidesServices()` and `RequiresServices()`. These declarations serve as the module's contract with the rest of the system.

```go
// Good: explicit about what it offers and needs
func (m *WebhookSender) ProvidesServices() []modular.ServiceProvider {
    return []modular.ServiceProvider{
        {Name: "webhook-sender", Description: "Sends HTTP webhooks with retry", Instance: m},
    }
}

func (m *WebhookSender) RequiresServices() []modular.ServiceDependency {
    return []modular.ServiceDependency{
        {Name: "http-client", Required: true},
    }
}
```

```go
// Bad: provides no services, requires nothing -- invisible to the dependency graph
func (m *MyModule) ProvidesServices() []modular.ServiceProvider { return nil }
func (m *MyModule) RequiresServices() []modular.ServiceDependency { return nil }
```

### 3. No Internal HTTP Routing

Handler modules should not implement their own URL routing logic. Instead, they should implement the `HTTPHandler` interface and let the router module dispatch requests to them.

**Good**: A module implements `Handle(w, r)` and the router calls it for the correct path.
**Bad**: A module parses `r.URL.Path` internally to route between `/config`, `/status`, `/reload`, etc.

When a module must handle multiple sub-endpoints (e.g., admin APIs), decompose it into sub-handlers with clear boundaries (see Decomposition Plan below).

### 4. Composable via Chaining

Modules should be designed to work in pipelines. Middleware modules transform the request and pass it along. Processing steps execute a component and trigger the next transition. This composability is what makes the YAML config powerful.

### 5. Config-Driven Behavior

All behavior should be controllable through YAML configuration. Modules should not hardcode values that users might need to change. Use `ConfigFields` in the schema to document what can be configured.

```yaml
# Good: everything is configurable
- name: rate-limiter
  type: http.middleware.ratelimit
  config:
    requestsPerMinute: 120
    burstSize: 20

# Bad: hardcoded limits that require code changes
```

## Module Schema Requirements

Every module type registered in `engine.go` must have a corresponding schema in `schema/module_schema.go` with:

1. **Type** -- matches the string used in `BuildFromConfig`
2. **Label** -- human-readable name for the UI
3. **Category** -- one of: http, middleware, messaging, statemachine, events, integration, scheduling, infrastructure, database, observability
4. **ConfigFields** -- every config key the engine extracts must have a field definition
5. **Inputs/Outputs** -- service I/O metadata describing what the module consumes and produces

## I/O Signature Metadata

Each `ModuleSchema` includes `Inputs` and `Outputs` fields that describe the services the module consumes and produces. These are used by:

- The UI to render I/O badges on canvas nodes
- The validator to check module wiring
- Documentation generators

```go
r.Register(&ModuleSchema{
    Type:     "webhook.sender",
    Label:    "Webhook Sender",
    Category: "integration",
    Inputs: []ServiceIODef{
        {Name: "payload", Type: "JSON", Description: "Webhook payload to send"},
    },
    Outputs: []ServiceIODef{
        {Name: "response", Type: "http.Response", Description: "HTTP response from target"},
    },
    ConfigFields: []ConfigFieldDef{...},
})
```

## When to Decompose vs Keep Monolithic

Decompose when:
- The module handles more than 3 distinct concerns (e.g., CRUD + validation + reload)
- Different users/roles need access to different parts of the functionality
- Sub-parts have different dependency requirements
- Testing requires mocking large portions of the module

Keep monolithic when:
- The module has a single concern (health checks, metrics collection)
- All operations share the same state and dependencies
- Splitting would add complexity without clear benefit
- The module has fewer than 200 lines of implementation

## Decomposition Plan: admin-management (WorkflowUIHandler)

The `WorkflowUIHandler` in `module/api_workflow_ui.go` currently handles:
- `GET /workflow/config` -- read current config
- `PUT /workflow/config` -- write/update config
- `GET /workflow/modules` -- list available module types
- `POST /workflow/validate` -- validate a config
- `POST /workflow/reload` -- reload engine with current config
- `GET /workflow/status` -- get engine status

### Recommended decomposition into sub-handlers:

| Sub-handler | Responsibility | Endpoints |
|-------------|---------------|-----------|
| `configReader` | Read current workflow config | `GET /workflow/config` |
| `configWriter` | Update workflow config | `PUT /workflow/config` |
| `configValidator` | Validate config structure | `POST /workflow/validate` |
| `engineReloader` | Trigger engine reload | `POST /workflow/reload` |
| `engineStatus` | Report engine status | `GET /workflow/status` |
| `moduleRegistry` | List available module types | `GET /workflow/modules` |

The current implementation already has these as separate methods. The decomposition maintains the single `HandleManagement` dispatcher but organizes the internal methods into clearly named groups with documented boundaries.

## Decomposition Plan: admin-v1-api (V1APIHandler)

The `V1APIHandler` in `module/api_v1_handler.go` handles:
- Companies CRUD (list, create, get)
- Organizations CRUD (list, create)
- Projects CRUD (list, create)
- Workflows CRUD (list, create, get, update, delete, deploy, stop, versions)
- Dashboard aggregation

### Recommended decomposition into sub-handler groups:

| Sub-handler Group | Responsibility | Endpoint Prefix |
|-------------------|---------------|-----------------|
| `companiesHandler` | Company management | `/companies/*` |
| `organizationsHandler` | Organization management | `/organizations/*` |
| `projectsHandler` | Project management | `/projects/*` |
| `workflowsHandler` | Workflow lifecycle management | `/workflows/*` |
| `dashboardHandler` | Dashboard data aggregation | `/dashboard` |

These are implemented as method groups within `V1APIHandler` with clear naming boundaries (e.g., all company handlers are prefixed `handleListCompanies`, `handleCreateCompany`, `handleGetCompany`). The dispatcher `HandleV1` routes to the appropriate group.

## Audit: Remaining Admin Handlers

### admin-ai-api
**Verdict: Acceptable as monolithic.** The AI API handler wraps a single concern (AI service integration) and delegates to the `ai/service.go` layer. No decomposition needed.

### admin-dynamic-api
**Verdict: Acceptable as monolithic.** The dynamic component API is a thin CRUD layer over the dynamic component registry. All operations share the same registry dependency.

### admin-schema-api
**Verdict: Acceptable as monolithic.** The schema API handler (`schema/handler.go`) serves module schemas and the JSON schema. It has only 2 endpoints and under 80 lines. No decomposition needed.

## Module Validation

Use `ValidateModule()` from `module/validator.go` to check module implementations at development time:

```go
issues := module.ValidateModule(myModule)
for _, issue := range issues {
    fmt.Printf("[%s] %s: %s\n", issue.Severity, issue.Field, issue.Message)
}
```

The validator checks:
- Module has a non-empty name
- `ProvidesServices()` declares at least one service
- Service names follow naming conventions (lowercase, dot-separated)
- No duplicate service names
- Config completeness against the schema registry
