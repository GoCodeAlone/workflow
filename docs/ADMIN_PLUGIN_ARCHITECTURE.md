# Plugin-Based Administration Architecture

## Design Philosophy

The Administration is itself a workflow running on the workflow engine. The engine provides core primitives (modules, pipeline steps, triggers, HTTP serving, auth). Everything else — including the admin UI, workflow editor, observability tools — is delivered as plugins with declared dependencies.

This creates a recursive architecture: the admin workflow can be edited by the workflow editor, which is itself a plugin loaded by the admin workflow.

## Core Engine (Not a Plugin)

These are built into the binary and always available:

- **Module system**: All 50+ module types (http.server, http.router, messaging, state machine, events, database, etc.)
- **Pipeline steps**: All step types (delegate, db_query, db_exec, http_call, validate, transform, conditional, jq, shell_exec, etc.)
- **Triggers**: HTTP, EventBus, Schedule, Event triggers
- **Workflow handlers**: HTTP, Messaging, StateMachine, Scheduler, Integration, Pipeline
- **Configuration**: YAML parsing, config feeders, secrets resolution
- **HTTP serving**: Server, router, CORS, rate limiting, security headers
- **Auth primitives**: JWT creation/validation, auth middleware
- **Storage**: SQLite, local file, S3, GCS storage modules
- **Observability**: Health checks, metrics, OpenAPI generation
- **Dynamic loading**: Yaegi interpreter, component registry, file watcher
- **Plugin system**: NativePlugin interface, dependency resolution, lifecycle management

## Plugin Dependency System

Plugins declare dependencies on other plugins. Installing a plugin auto-installs its dependencies. Uninstalling checks for dependents.

```go
type NativePlugin interface {
    Name() string
    Version() string
    Description() string
    Dependencies() []PluginDependency // NEW: required plugins
    RegisterRoutes(mux *http.ServeMux)
    UIPages() []UIPageDef
    OnEnable(ctx PluginContext) error  // NEW: called when plugin is enabled
    OnDisable(ctx PluginContext) error // NEW: called when plugin is disabled
}

type PluginDependency struct {
    Name       string // plugin name
    MinVersion string // semver constraint (optional)
}

type PluginContext struct {
    App       modular.Application
    Engine    *StdEngine
    DB        *sql.DB       // shared database
    Logger    *slog.Logger
    DataDir   string        // plugin-specific data directory
}
```

## Plugin Decomposition

### Layer 1: Foundation Plugins (no dependencies)

#### `auth-manager`
User CRUD, role management, IAM providers, SSO configuration.
- Routes: `/api/v1/auth/*`, `/api/v1/admin/iam/*`
- UI Pages: Settings (IAM configuration)
- Services: user-store, iam-provider-store

#### `workflow-registry`
Workflow CRUD, version management, validation.
- Routes: `/api/v1/admin/workflows/*`
- UI Pages: None (consumed by editor and dashboard)
- Services: workflow-registry, workflow-store

#### `data-store`
Company/organization/project hierarchy, system schema.
- Routes: `/api/v1/admin/companies/*`, `/api/v1/admin/organizations/*`, `/api/v1/admin/projects/*`
- UI Pages: None (consumed by dashboard)
- Services: v1-store, schema-mgmt

### Layer 2: Feature Plugins (depend on Layer 1)

#### `workflow-editor`
Visual workflow designer with node palette, canvas, property panel.
- Dependencies: `workflow-registry`, `data-store`
- Routes: `/api/v1/admin/engine/*` (status, services — read-only for non-admin roles)
- UI Pages: Editor (main view), ProjectSwitcher, WorkflowList, WorkflowTabs
- Services: engine-mgmt (read-only)

#### `execution-tracker`
Execution recording, step tracking, basic execution views.
- Dependencies: `workflow-registry`
- Routes: `/api/v1/admin/executions/*` (basic: list, get, steps, cancel)
- UI Pages: Executions, Logs, Events (basic views)
- Services: execution-store, event-recorder

#### `schema-browser`
Module schema definitions and browsing.
- Dependencies: None (reads from engine's built-in schema registry)
- Routes: `/api/v1/admin/schemas/*`
- UI Pages: None (consumed by editor's NodePalette)
- Services: schema-mgmt

### Layer 3: Advanced Plugins (depend on Layer 2)

#### `observability`
Timeline visualization, replay, event drill-down.
- Dependencies: `execution-tracker`
- Routes: `/api/v1/admin/executions/{id}/timeline`, `/api/v1/admin/executions/{id}/replay`
- UI Pages: Timeline view, Replay controls
- Services: timeline-mgmt, replay-mgmt

#### `error-management`
Dead letter queue inspection, retry, discard.
- Dependencies: `execution-tracker`
- Routes: `/api/v1/admin/dlq/*`
- UI Pages: DLQ browser
- Services: dlq-mgmt

#### `testing-tools`
Backfill, step mocking, execution diff comparison.
- Dependencies: `execution-tracker`, `observability`
- Routes: `/api/v1/admin/backfill/*`, `/api/v1/admin/mocks/*`, `/api/v1/admin/executions/diff`
- UI Pages: Backfill manager, Mock editor, Diff viewer
- Services: backfill-mgmt

#### `store-browser`
Direct database table inspection (existing NativePlugin, refactored).
- Dependencies: `data-store`
- Routes: `/api/v1/admin/plugins/store-browser/*`
- UI Pages: Store Browser
- Services: None (self-contained handler)

#### `doc-manager`
Workflow documentation management (existing NativePlugin, refactored).
- Dependencies: `workflow-registry`
- Routes: `/api/v1/admin/plugins/doc-manager/*`
- UI Pages: Documentation
- Services: None (self-contained handler)

### Layer 4: Extension Plugins

#### `cicd-environments`
Deployment target management, environment CRUD, connection testing.
- Dependencies: `data-store`
- Routes: `/api/v1/admin/environments/*`
- UI Pages: Environments
- Services: env-mgmt

#### `cloud-providers`
Cloud provider status, region listing, credential management.
- Dependencies: `cicd-environments`
- Routes: `/api/v1/providers/*`
- UI Pages: Provider config (within Environments)
- Services: cloud-providers

#### `ai-assistant`
AI-powered workflow generation, component creation, suggestions.
- Dependencies: `workflow-editor`, `workflow-registry`
- Routes: `/api/v1/admin/ai/*`
- UI Pages: AI Copilot panel
- Services: ai-mgmt

#### `billing`
Usage metering, plan management, subscription tracking.
- Dependencies: `data-store`, `execution-tracker`
- Routes: `/api/v1/admin/billing/*`
- UI Pages: Billing dashboard
- Services: billing-mgmt

#### `marketplace`
Remote plugin registry, search, install from registry.
- Dependencies: None (operates on plugin system itself)
- Routes: `/api/v1/admin/plugins/registry/*`
- UI Pages: Marketplace
- Services: plugin-registry

### Meta Plugin: `administration`

The full admin experience. Installing this installs everything.

```go
func (p *AdministrationPlugin) Dependencies() []PluginDependency {
    return []PluginDependency{
        {Name: "auth-manager"},
        {Name: "workflow-registry"},
        {Name: "data-store"},
        {Name: "workflow-editor"},
        {Name: "execution-tracker"},
        {Name: "schema-browser"},
        {Name: "observability"},
        {Name: "error-management"},
        {Name: "testing-tools"},
        {Name: "store-browser"},
        {Name: "doc-manager"},
        {Name: "cicd-environments"},
        {Name: "cloud-providers"},
        {Name: "ai-assistant"},
        {Name: "billing"},
        {Name: "marketplace"},
    }
}
```

## Plugin Lifecycle

```
[Registered] → [Enabled] ⇄ [Disabled] → [Unregistered]
```

- **Registered**: Plugin code is available in the binary
- **Enabled**: `OnEnable()` called — routes registered, services created, UI pages activated
- **Disabled**: `OnDisable()` called — routes removed, services deregistered, UI pages hidden
- **Unregistered**: Plugin removed from registry

State is persisted in `plugin_state` table:
```sql
CREATE TABLE plugin_state (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT 0,
    version TEXT NOT NULL,
    enabled_at TEXT,
    disabled_at TEXT
);
```

## UI Architecture

The UI reads enabled plugins from `/api/v1/plugins` and dynamically builds navigation:

```typescript
// No more hard-coded nav items. Everything comes from plugin metadata.
const { plugins } = usePluginStore();
const navItems = plugins
    .filter(p => p.enabled)
    .flatMap(p => p.uiPages)
    .sort((a, b) => a.order - b.order);
```

Plugin UI pages are lazy-loaded React components registered via a plugin manifest.

## RBAC Integration

Plugins declare required permissions:
```go
func (p *WorkflowEditorPlugin) RequiredPermissions() []Permission {
    return []Permission{
        {Resource: "workflows", Action: "read"},
        {Resource: "workflows", Action: "write"},
        {Resource: "engine", Action: "read"},
    }
}
```

The admin workflow editor requires `engine:write` permission, which only the `admin` role has. Regular `editor` roles can edit workflow definitions within their project scope but cannot modify the admin config itself.

## Engine Reload Fix

The current reload destroys services because `wireV1HandlerPostStart()` is only called once at startup. The fix:

1. Extract all service registration into plugin `OnEnable()` methods
2. Engine reload re-initializes the core engine (modules, handlers, triggers)
3. After reload, re-enable all enabled plugins (calling `OnEnable()` on each)
4. Plugin state survives reload because it's persisted in the database

## Config Decomposition

Instead of one monolithic `admin/config.yaml`, each plugin contributes its own config fragment:

```
plugins/
  auth-manager/
    config.yaml       # Auth routes, user store module, JWT module
    plugin.go         # Plugin implementation
  workflow-editor/
    config.yaml       # Engine mgmt routes, editor-specific endpoints
    plugin.go
  execution-tracker/
    config.yaml       # Execution routes
    plugin.go
  ...
```

The engine composes the final config by merging core config + all enabled plugin configs.

## Deployment Scenarios

### Scenario 1: Full Admin (default for development)
```bash
./server --plugins=administration
# Installs all 16 plugins, full admin UI
```

### Scenario 2: Lightweight Worker
```bash
./server --config=my-workflow.yaml
# No plugins enabled, just runs the workflow
# Health, metrics, and basic auth available from core
```

### Scenario 3: Monitoring Dashboard
```bash
./server --plugins=execution-tracker,observability,error-management
# Read-only views into execution state, no editing capability
```

### Scenario 4: Editor-Only
```bash
./server --plugins=auth-manager,workflow-registry,data-store,workflow-editor,schema-browser
# Workflow design and validation, no execution tracking
```

## Migration Strategy

This is pre-alpha. No backwards compatibility needed.

1. Refactor NativePlugin interface to include Dependencies, OnEnable, OnDisable
2. Add plugin_state table and PluginManager
3. Extract each service block from wireV1HandlerPostStart into a plugin
4. Move route definitions from admin/config.yaml into plugin config fragments
5. Make UI navigation fully dynamic from plugin metadata
6. Remove all hard-coded service registrations from main.go
7. Remove old wireV1HandlerPostStart and wireManagementHandler functions
8. Add --plugins flag for deployment configuration

## External Plugins (gRPC-based)

In addition to the **native plugins** described above (compiled into the binary), the engine supports **external plugins** that run as separate OS processes communicating via gRPC. External plugins extend the engine with new module types, step types, and trigger types without recompiling.

### How External Plugins Relate to Native Plugins

Native plugins (this document) handle admin functionality: UI pages, routes, lifecycle hooks, and dependency management via the `NativePlugin` / `PluginManager` system. External plugins handle engine extensibility: new module types and pipeline step types via the `EnginePlugin` / `EnginePluginManager` system.

Both systems coexist:

| Aspect | Native Plugins | External Plugins |
|--------|---------------|-----------------|
| Interface | `NativePlugin` | `EnginePlugin` (via `ExternalPluginAdapter`) |
| Manager | `PluginManager` (SQLite-backed state) | `EnginePluginManager` (in-memory) |
| Communication | Direct Go calls | gRPC over Unix socket |
| Purpose | Admin UI, routes, services | Module types, step types, schemas |
| Deployment | Compiled into binary | Separate binary in `data/plugins/` |

### External Plugin Discovery

External plugins are placed in `data/plugins/{name}/` with a `plugin.json` manifest and a matching binary. The engine discovers them on startup and exposes management endpoints:

- `GET /api/v1/plugins/external` -- list available
- `GET /api/v1/plugins/external/loaded` -- list loaded
- `POST /api/v1/plugins/external/{name}/load` -- load
- `POST /api/v1/plugins/external/{name}/unload` -- unload
- `POST /api/v1/plugins/external/{name}/reload` -- reload

### Integration with Admin Plugins

An admin native plugin (e.g., `marketplace` or a future `plugin-manager` plugin) can provide UI for managing external plugins -- listing available plugins, loading/unloading them, and monitoring their health. The admin plugin would call the external plugin API endpoints internally.

### Further Reading

- [PLUGIN_ARCHITECTURE.md](PLUGIN_ARCHITECTURE.md) -- technical architecture of the external plugin system (gRPC protocol, lifecycle, process isolation, security)
- [PLUGIN_DEVELOPMENT_GUIDE.md](PLUGIN_DEVELOPMENT_GUIDE.md) -- developer guide for building external plugins (SDK, examples, testing, deployment)
