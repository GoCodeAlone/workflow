# Module Best Practices

This document defines conventions and guidelines for building modules in the workflow engine. Following these practices ensures modules are composable, testable, and maintainable.

---

## 1. Single Responsibility Principle

Each module should do ONE thing well. Handler modules should not embed their own routing or storage logic. A module that validates configs should not also reload the engine. A module that handles REST CRUD should not also manage authentication.

**Good examples:**
- `health.checker` -- only checks health and exposes a health endpoint.
- `metrics.collector` -- only collects and exposes Prometheus metrics.
- `auth.jwt` -- only handles JWT signing, verification, and user lookup.
- `storage.sqlite` -- only provides a SQLite database connection as a service.

**Bad examples:**
- A single module that handles config read, config write, config validate, engine reload, AND engine status.
- An HTTP handler that embeds its own URL routing logic instead of delegating to a router module.
- A storage module that also implements caching and validation internally.

**When to decompose:**
- The module handles more than 3 distinct concerns
- Different users/roles need access to different parts of the functionality
- Sub-parts have different dependency requirements
- Testing requires mocking large portions of the module

**When a monolithic module is acceptable:**
- The module has a single concern (health checks, metrics collection)
- All operations share the same state and dependencies
- Splitting would add complexity without clear benefit
- The module has fewer than 200 lines of implementation

---

## 2. Clear I/O Signatures

Every module must declare what services it provides and requires through `ProvidesServices()` and `RequiresServices()`. These declarations serve as the module's contract with the rest of the system.

### Go Interface Requirements

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

### Schema I/O Metadata

Each `ModuleSchema` in `schema/module_schema.go` must include `Inputs` and `Outputs` fields describing what the module consumes and produces. These are used by:

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

### Standard Service Types

Use these standard type names in `ServiceIODef.Type` for consistency:

| Type | Description | Example Modules |
|------|-------------|-----------------|
| `http.Request` | Incoming or outgoing HTTP request | `http.server`, `http.router`, all middleware |
| `http.Response` | HTTP response | `http.handler`, `http.proxy` |
| `http.Client` | HTTP client for outbound calls | `httpclient.modular` |
| `net.Listener` | Network listener / server | `httpserver.modular` |
| `sql.DB` | SQL database connection pool | `database.modular`, `database.workflow`, `storage.sqlite` |
| `EventBus` | In-process event bus (pub/sub) | `eventbus.modular` |
| `CloudEvent` | CloudEvents-formatted event | `eventbus.modular`, `eventlogger.modular` |
| `Event` | Generic event | `statemachine.engine`, `messaging.broker.eventbus` |
| `[]byte` | Raw byte message payload | `messaging.broker`, `messaging.nats`, `messaging.kafka` |
| `AuthService` | Authentication/authorization service | `auth.jwt`, `auth.modular` |
| `Credentials` | Login credentials or token | `auth.jwt`, `auth.user-store` |
| `UserStore` | User storage service | `auth.user-store` |
| `DataStore` | Generic data persistence service | `persistence.store` |
| `SecretProvider` | Secret retrieval service | `secrets.vault`, `secrets.aws` |
| `prometheus.Metrics` | Prometheus metrics endpoint | `metrics.collector` |
| `HealthStatus` | Health check status | `health.checker` |
| `Scheduler` | Cron job scheduler service | `scheduler.modular` |
| `Cache` | Key-value cache service | `cache.modular` |
| `SchemaValidator` | JSON Schema validation service | `jsonschema.modular` |
| `WorkflowRegistry` | Workflow data registry | `workflow.registry` |
| `ObjectStore` | S3/GCS-compatible object storage | `storage.s3`, `storage.gcs` |
| `FileStore` | Local filesystem storage | `storage.local` |
| `PipelineContext` | Pipeline execution context | All `step.*` modules |
| `StepResult` | Pipeline step execution result | All `step.*` modules |
| `Transition` | State machine transition result | `statemachine.engine` |
| `trace.Tracer` | OpenTelemetry tracer | `observability.otel` |
| `OpenAPISpec` | OpenAPI 3.0 specification | `openapi.generator`, `openapi.consumer` |
| `ExternalAPIClient` | Typed HTTP client from OpenAPI spec | `openapi.consumer` |
| `JSON` | Arbitrary JSON data | `api.handler`, `jsonschema.modular` |
| `any` | Untyped data (use sparingly) | `data.transformer`, `dynamic.component` |

---

## 3. Config-Driven Behavior

All behavior should be controllable through YAML configuration. No hardcoded URLs, paths, or service names. Modules should not require code changes for values that users might need to adjust.

### Good: Everything is Configurable

```yaml
- name: rate-limiter
  type: http.middleware.ratelimit
  config:
    requestsPerMinute: 120
    burstSize: 20

- name: my-db
  type: database.workflow
  config:
    driver: postgres
    dsn: $DATABASE_URL
    maxOpenConns: 25
    maxIdleConns: 5
```

### Bad: Hardcoded Values

```go
// Bad: hardcoded connection limit
func (m *MyDB) Configure() error {
    m.maxConns = 10  // Should come from config
    m.host = "localhost:5432"  // Should come from config
    return nil
}
```

### Environment Variable Expansion

Support `$ENV_VAR` expansion for sensitive values like secrets and connection strings:

```yaml
- name: jwt-auth
  type: auth.jwt
  config:
    secret: $JWT_SECRET       # Expanded at runtime
    issuer: my-app
```

### Config Defaults

Always provide sensible defaults in the schema's `DefaultConfig` map:

```go
DefaultConfig: map[string]any{
    "requestsPerMinute": 60,
    "burstSize":         10,
},
```

---

## 4. Composability

Modules should be chainable. HTTP middleware should work with any router. Storage modules should work with any consumer. Processing steps should work in any pipeline order.

### Middleware Chaining

HTTP middleware modules accept an `http.Request`, apply their transformation, and pass it through. Any middleware should be insertable between any server and handler:

```yaml
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: cors
    type: http.middleware.cors
    config:
      allowedOrigins: ["*"]
  - name: rate-limiter
    type: http.middleware.ratelimit
    config:
      requestsPerMinute: 100
  - name: api
    type: api.handler
    config:
      resourceName: orders
```

### Pipeline Step Chaining

Pipeline steps (`step.*`) receive a `PipelineContext` and produce a `StepResult`. Each step's output is merged back into the context for subsequent steps:

```yaml
pipeline:
  - name: validate-input
    type: step.validate
    config:
      strategy: required_fields
      required_fields: [name, email]
  - name: enrich-data
    type: step.set
    config:
      values:
        created_at: "{{ now }}"
        status: pending
  - name: notify
    type: step.publish
    config:
      topic: user.created
```

### Storage Interchangeability

Storage modules (`storage.sqlite`, `database.workflow`, `database.modular`) all output `sql.DB` connections. Consumer modules should depend on the service name, not the specific storage implementation:

```yaml
- name: admin-db
  type: storage.sqlite
  config:
    dbPath: data/admin.db

- name: registry
  type: workflow.registry
  config:
    storageBackend: admin-db    # Works with any module providing sql.DB
```

---

## 5. Naming Conventions

Module types follow the `{category}.{specific}` pattern with dots as separators:

| Pattern | Examples |
|---------|----------|
| `http.server` | HTTP server |
| `http.router` | HTTP request router |
| `http.handler` | Generic HTTP handler |
| `http.middleware.{name}` | `http.middleware.cors`, `http.middleware.ratelimit`, `http.middleware.auth` |
| `http.proxy`, `http.simple_proxy` | Reverse proxy variants |
| `storage.{backend}` | `storage.sqlite`, `storage.s3`, `storage.gcs`, `storage.local` |
| `database.{scope}` | `database.modular`, `database.workflow` |
| `messaging.{type}` | `messaging.broker`, `messaging.nats`, `messaging.kafka` |
| `auth.{mechanism}` | `auth.jwt`, `auth.modular`, `auth.user-store` |
| `secrets.{provider}` | `secrets.vault`, `secrets.aws` |
| `step.{action}` | `step.validate`, `step.transform`, `step.publish`, `step.set`, `step.log`, `step.http_call`, `step.conditional` |
| `{framework}.modular` | `httpserver.modular`, `scheduler.modular`, `eventbus.modular` (CrisisTextLine/modular wrappers) |

### Module Instance Names

Instance names in YAML config should be descriptive and kebab-case:

```yaml
# Good
- name: order-api
- name: admin-db
- name: cors-middleware
- name: user-auth

# Bad
- name: module1
- name: MyModule
- name: db
```

---

## 6. Error Handling

### Use Structured Errors

Return errors with context. Never panic in module code.

```go
// Good: structured error with context
func (m *MyModule) Configure() error {
    if m.config.Endpoint == "" {
        return fmt.Errorf("module %s: endpoint is required", m.Name())
    }
    return nil
}

// Bad: panic
func (m *MyModule) Configure() error {
    if m.config.Endpoint == "" {
        panic("no endpoint")  // Never do this
    }
    return nil
}
```

### Propagate Context Cancellation

Always respect `context.Context` in `Start()` and long-running operations:

```go
func (m *MyModule) Start(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()  // Clean shutdown
        case msg := <-m.incoming:
            if err := m.process(ctx, msg); err != nil {
                m.logger.Error("processing failed", "error", err)
                // Don't return -- keep processing unless ctx is cancelled
            }
        }
    }
}
```

### Graceful Degradation

Modules should handle missing optional dependencies gracefully:

```go
func (m *MyModule) Init(app modular.Application) error {
    // Required dependency -- fail if missing
    db, err := app.GetService("database")
    if err != nil {
        return fmt.Errorf("required service 'database' not found: %w", err)
    }
    m.db = db

    // Optional dependency -- degrade gracefully
    cache, err := app.GetService("cache")
    if err != nil {
        m.logger.Warn("cache service not available, operating without cache")
    } else {
        m.cache = cache
    }
    return nil
}
```

---

## 7. Testing

### Unit Testing with MockApp

Each module needs unit tests with mock dependencies. Use `mock.NewTestApplication()` from the `mock/` package:

```go
package module_test

import (
    "testing"
    "github.com/GoCodeAlone/workflow/mock"
)

func TestMyModule_Configure(t *testing.T) {
    app := mock.NewTestApplication()

    m := NewMyModule("test-module", map[string]any{
        "endpoint": "http://localhost:8080",
        "timeout":  "30s",
    })

    if err := m.Init(app); err != nil {
        t.Fatalf("Init failed: %v", err)
    }

    // Verify configuration was applied
    if m.endpoint != "http://localhost:8080" {
        t.Errorf("endpoint = %q, want %q", m.endpoint, "http://localhost:8080")
    }
}
```

### Test Module Contracts

Verify `ProvidesServices()` and `RequiresServices()` return the expected values:

```go
func TestMyModule_Services(t *testing.T) {
    m := NewMyModule("test", nil)

    provides := m.ProvidesServices()
    if len(provides) == 0 {
        t.Error("module should provide at least one service")
    }
    if provides[0].Name != "my-service" {
        t.Errorf("service name = %q, want %q", provides[0].Name, "my-service")
    }
}
```

### Test Config Edge Cases

Test with missing config, nil config, empty strings, and invalid values:

```go
func TestMyModule_MissingConfig(t *testing.T) {
    m := NewMyModule("test", nil)
    err := m.Init(mock.NewTestApplication())
    if err == nil {
        t.Error("expected error for nil config")
    }
}

func TestMyModule_EmptyEndpoint(t *testing.T) {
    m := NewMyModule("test", map[string]any{"endpoint": ""})
    err := m.Init(mock.NewTestApplication())
    if err == nil {
        t.Error("expected error for empty endpoint")
    }
}
```

### Use Module Test Helpers

The `module/testutil.go` and `module/module_test_helpers.go` files provide common test utilities. Use them instead of reinventing test infrastructure.

---

## 8. Configuration Fields in Schema

All config must be declared in `schema/module_schema.go` with proper types, descriptions, defaults, and validation patterns.

### Required Fields

Every config key that `engine.go` extracts in `BuildFromConfig` must have a corresponding `ConfigFieldDef`:

```go
ConfigFields: []ConfigFieldDef{
    {
        Key:          "address",
        Label:        "Listen Address",
        Type:         FieldTypeString,
        Required:     true,
        Description:  "Host:port to listen on (e.g. :8080, 0.0.0.0:80)",
        DefaultValue: ":8080",
        Placeholder:  ":8080",
    },
},
```

### Field Type Reference

| Type | Go Constant | Use For |
|------|-------------|---------|
| `string` | `FieldTypeString` | Text values, URLs, paths, names |
| `number` | `FieldTypeNumber` | Integers, counts, limits, ports |
| `boolean` | `FieldTypeBool` | On/off toggles, feature flags |
| `select` | `FieldTypeSelect` | Enumerated choices (requires `Options`) |
| `json` | `FieldTypeJSON` | Embedded JSON objects or schemas |
| `duration` | `FieldTypeDuration` | Time durations (e.g., "30s", "5m", "24h") |
| `array` | `FieldTypeArray` | Lists of values (requires `ArrayItemType`) |
| `map` | `FieldTypeMap` | Key-value pairs (requires `MapValueType`) |
| `filepath` | `FieldTypeFilePath` | File system paths |

### Special Field Properties

- `Sensitive: true` -- renders as a password field in the UI (e.g., secrets, tokens, DSNs)
- `InheritFrom: "dependency.name"` -- auto-populates from connected node's name in the UI
- `Group: "advanced"` -- groups the field under an "Advanced" collapsible section in the UI
- `DefaultValue` -- must match the type; also add to `DefaultConfig` map on the schema

### Validation

The schema validation system (`schema/validate.go`) automatically checks:
- Required fields are present
- Module types are known
- Dependencies reference existing modules
- Custom validation rules per module type (e.g., `http.server` requires `address`)

---

## Module Schema Requirements Checklist

Every module type registered in `engine.go` must have a corresponding schema in `schema/module_schema.go` with:

1. **Type** -- matches the string used in `BuildFromConfig`
2. **Label** -- human-readable name for the UI
3. **Category** -- one of: `http`, `middleware`, `messaging`, `statemachine`, `events`, `integration`, `scheduling`, `infrastructure`, `database`, `observability`, `pipeline`
4. **Description** -- one-sentence summary of what the module does
5. **Inputs** -- `[]ServiceIODef` describing what the module consumes (omit only for pure source modules like `http.server`)
6. **Outputs** -- `[]ServiceIODef` describing what the module produces
7. **ConfigFields** -- every config key the engine extracts must have a field definition
8. **DefaultConfig** -- default values for optional config fields

---

## Decomposition Plans

### admin-management (WorkflowUIHandler)

The `WorkflowUIHandler` in `module/api_workflow_ui.go` currently handles:
- `GET /workflow/config` -- read current config
- `PUT /workflow/config` -- write/update config
- `GET /workflow/modules` -- list available module types
- `POST /workflow/validate` -- validate a config
- `POST /workflow/reload` -- reload engine with current config
- `GET /workflow/status` -- get engine status

Recommended decomposition into sub-handlers:

| Sub-handler | Responsibility | Endpoints |
|-------------|---------------|-----------|
| `configReader` | Read current workflow config | `GET /workflow/config` |
| `configWriter` | Update workflow config | `PUT /workflow/config` |
| `configValidator` | Validate config structure | `POST /workflow/validate` |
| `engineReloader` | Trigger engine reload | `POST /workflow/reload` |
| `engineStatus` | Report engine status | `GET /workflow/status` |
| `moduleRegistry` | List available module types | `GET /workflow/modules` |

The current implementation already has these as separate methods. The decomposition maintains the single `HandleManagement` dispatcher but organizes the internal methods into clearly named groups with documented boundaries.

### admin-v1-api (V1APIHandler)

The `V1APIHandler` in `module/api_v1_handler.go` handles:
- Companies CRUD (list, create, get)
- Organizations CRUD (list, create)
- Projects CRUD (list, create)
- Workflows CRUD (list, create, get, update, delete, deploy, stop, versions)
- Dashboard aggregation

Recommended decomposition into sub-handler groups:

| Sub-handler Group | Responsibility | Endpoint Prefix |
|-------------------|---------------|-----------------|
| `companiesHandler` | Company management | `/companies/*` |
| `organizationsHandler` | Organization management | `/organizations/*` |
| `projectsHandler` | Project management | `/projects/*` |
| `workflowsHandler` | Workflow lifecycle management | `/workflows/*` |
| `dashboardHandler` | Dashboard data aggregation | `/dashboard` |

### Acceptable Monolithic Modules

- **admin-ai-api**: Wraps a single concern (AI service integration) and delegates to the `ai/service.go` layer.
- **admin-dynamic-api**: Thin CRUD layer over the dynamic component registry. All operations share the same registry dependency.
- **admin-schema-api**: Schema API handler (`schema/handler.go`) with only 2 endpoints and under 80 lines.

---

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
