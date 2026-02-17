# Engine Decomposition Plan

Decomposing the workflow engine core and moving functionality into plugins.

## 1. Executive Summary

The workflow engine currently has 40+ module types hardcoded in a single `BuildFromConfig` switch statement in `engine.go`, with another 30+ step types registered in `cmd/server/main.go`. All module factories, step factories, workflow handlers, and trigger handlers are compiled into the binary. This creates tight coupling: changing an HTTP server implementation requires modifying the core engine, adding a new storage backend means touching `engine.go`, and every module type bloats the core regardless of whether it is used.

This plan decomposes the engine into a minimal core and a set of plugins. The core retains only: YAML parsing, module lifecycle management, service registry, workflow dispatch, pipeline execution, trigger dispatch, and plugin loading. Everything else -- HTTP, messaging, state machines, storage, auth, observability, AI, feature flags, and all pipeline step types -- moves into plugins that register themselves through a capability contract system.

Plugins do not just provide module implementations. They introduce *capability categories* (e.g., "http-server", "message-broker", "database") and satisfy *interface contracts*. Workflows declare which capabilities they need, not which plugins. This enables plugin substitution: swapping one HTTP server plugin for another requires zero changes to workflow configs, as long as both satisfy the "http-server" capability contract.

The migration happens in four phases across an estimated 8-12 weeks, with each phase producing a working system. Negative tests prove functionality was removed from core; positive tests prove plugins provide it correctly; workflow dependency tests prove configs correctly declare their capability requirements.

---

## 2. Current State Analysis

### 2.1 What Is In Core Today

**File: `engine.go` (1647 lines)**

The `StdEngine.BuildFromConfig` method contains a switch statement with cases for every built-in module type. Each case contains module-specific construction logic: parsing config maps, extracting typed values, calling constructors. The engine also contains post-init wiring logic for:

- AuthMiddleware-to-AuthProvider wiring (lines 898-912)
- Static file server to router wiring (lines 946-1035)
- Health checker endpoint registration (lines 1037-1073)
- Metrics collector endpoint registration (lines 1075-1089)
- Log collector endpoint registration (lines 1091-1104)
- OpenAPI generator spec building and endpoint registration (lines 1106-1124)
- Trigger configuration dispatch (lines 1126-1129)
- Pipeline configuration and step building (lines 1132-1136)
- Route pipeline wiring for CQRS handlers (lines 941-943)

**File: `cmd/server/main.go`**

Registers 30+ step types, 5 workflow handlers, and 4 trigger types directly:

| Registration Type | Count | Examples |
|---|---|---|
| Workflow handlers | 6 | HTTP, Messaging, StateMachine, Scheduler, Integration, Pipeline |
| Step types | 30+ | step.validate, step.transform, step.http_call, step.shell_exec, step.deploy, etc. |
| Module factories | 0 (all in engine.go) | -- |

**File: `schema/schema.go`**

`KnownModuleTypes()` returns 69 hardcoded type strings. `KnownTriggerTypes()` returns 4. `KnownWorkflowTypes()` returns 6.

**File: `schema/module_schema.go`**

`ModuleSchemaRegistry.registerBuiltins()` registers UI schema definitions for all 69 module types with their config fields, inputs, outputs, and categories.

### 2.2 Module Types in `BuildFromConfig` Switch

| Module Type | Lines of Config Parsing | Constructor Complexity |
|---|---|---|
| `http.server` | 6 | Simple (name, address) |
| `http.router` | 2 | Trivial |
| `http.handler` | 5 | Simple (name, contentType) |
| `api.query` | 5 | Simple + optional delegate |
| `api.command` | 5 | Simple + optional delegate |
| `api.handler` | 50 | Complex (fieldMapping, transitionMap, summaryFields) |
| `http.middleware.auth` | 5 | Simple |
| `http.middleware.logging` | 5 | Simple |
| `http.middleware.ratelimit` | 12 | Medium (int parsing) |
| `http.middleware.cors` | 15 | Medium (array parsing) |
| `http.middleware.requestid` | 2 | Trivial |
| `http.middleware.securityheaders` | 15 | Medium (multiple string fields) |
| `messaging.broker` | 8 | Medium (queue size, timeout) |
| `messaging.broker.eventbus` | 2 | Trivial |
| `messaging.handler` | 2 | Trivial |
| `statemachine.engine` | 8 | Medium (maxInstances, TTL) |
| `state.tracker` | 5 | Simple |
| `state.connector` | 2 | Trivial |
| `http.proxy` / `reverseproxy` | 2 each | Trivial (delegates to modular module) |
| `http.simple_proxy` | 10 | Medium (targets map) |
| `scheduler.modular` | 2 | Trivial (delegates to modular module) |
| `cache.modular` | 2 | Trivial (delegates to modular module) |
| `database.modular` | 2 | Trivial (delegates to modular module) |
| `featureflag.service` | 15 | Medium + step type registration side-effect |
| `metrics.collector` | 15 | Medium (namespace, enabledMetrics array) |
| `health.checker` | 15 | Medium (paths, timeout, autoDiscover) |
| `log.collector` | 8 | Simple |
| `dynamic.component` | 25 | Complex (registry lookup, source loading, provides/requires) |
| `database.workflow` | 8 | Simple |
| `data.transformer` | 2 | Trivial |
| `webhook.sender` | 5 | Simple |
| `notification.slack` | 2 | Trivial |
| `storage.s3` | 8 | Simple |
| `storage.local` | 5 | Simple |
| `storage.gcs` | 8 | Simple |
| `storage.sqlite` | 8 | Simple |
| `messaging.nats` | 2 | Trivial |
| `messaging.kafka` | 12 | Medium (brokers array, groupId) |
| `observability.otel` | 2 | Trivial |
| `static.fileserver` | 20 | Medium (multiple config options) |
| `persistence.store` | 5 | Simple |
| `auth.jwt` | 15 | Medium (secret, expiry, issuer, seedFile) |
| `auth.user-store` | 2 | Trivial |
| `processing.step` | 10 | Medium (multiple config fields) |
| `secrets.vault` | 10 | Medium |
| `secrets.aws` | 8 | Simple |
| `workflow.registry` | 5 | Simple |
| `openapi.generator` | 15 | Medium (servers array) |
| `openapi.consumer` | 10 | Medium (fieldMapping) |
| `api.gateway` | 50 | Complex (routes, rateLimit, cors, auth) |

**Total: ~50 case branches, ~550 lines of config parsing code.**

### 2.3 What Is Already Modular

The existing plugin system (`plugin/` package) supports:

- **NativePlugin interface**: Name, Version, Description, Dependencies, UIPages, RegisterRoutes, OnEnable, OnDisable
- **WorkflowPlugin interface**: Extends NativePlugin with EmbeddedWorkflows
- **PluginManager**: Enable/disable with dependency resolution, state persistence, HTTP dispatch
- **PluginManifest**: Semver-versioned metadata with dependency constraints
- **PluginRegistry / LocalRegistry**: Registration, scanning, dependency checking
- **Existing plugins**: `docmanager`, `storebrowser`, AI plugins (anthropic, openai, generic)

However, the existing plugin system is oriented toward *admin UI extensions* (UIPages, RegisterRoutes for admin API endpoints). It does not support plugins contributing:

- Module type factories (what `BuildFromConfig` needs)
- Pipeline step type factories (what `StepRegistry` needs)
- Workflow handler implementations (what the engine dispatches to)
- Trigger implementations
- Schema definitions (what the UI and validation need)
- Post-init wiring hooks (what health check, metrics, OpenAPI registration need)

### 2.4 Handlers Package

The `handlers/` package contains 6 workflow handlers:

| Handler | File | Purpose |
|---|---|---|
| `HTTPWorkflowHandler` | `http.go` | Wires HTTP routes to handler modules |
| `MessagingWorkflowHandler` | `messaging.go` | Wires messaging subscriptions |
| `StateMachineWorkflowHandler` | `state_machine.go` | Configures state machine definitions |
| `SchedulerWorkflowHandler` | `scheduler.go` | Configures scheduled jobs |
| `IntegrationWorkflowHandler` | `integration.go` | Configures third-party service connectors |
| `PipelineWorkflowHandler` | `pipeline.go` | Manages pipeline-based workflows |

All 6 are registered in `cmd/server/main.go`. The pipeline handler is core (it executes the pipeline engine itself), but the other 5 are domain-specific and should move to plugins.

### 2.5 Triggers

Four trigger types exist, mapped by `canHandleTrigger()`:

| Trigger Type | Constant | Implementation File |
|---|---|---|
| `http` | `module.HTTPTriggerName` | `module/trigger.go` + `module/http_trigger_test.go` |
| `schedule` | `module.ScheduleTriggerName` | `module/scheduler.go` |
| `event` | `module.EventTriggerName` | `module/event_processor.go` |
| `eventbus` | `module.EventBusTriggerName` | `module/eventbus_trigger.go` |

---

## 3. Plugin Interface Contract System Design

### 3.1 Design Principles

1. **Plugins introduce categories, not the core.** The core does not know about "http-server" or "message-broker". Plugins declare what capability categories they provide.
2. **Workflows depend on capabilities, not plugins.** A workflow declares `requires: [http-server, message-broker]`, not `requires: [http-plugin, nats-plugin]`.
3. **Interface contracts are defined by capability providers.** The first plugin to register an "http-server" capability defines the Go interface. Other plugins providing the same capability must satisfy the same interface.
4. **Core provides registration machinery only.** The core provides `CapabilityRegistry`, `ModuleTypeRegistry`, `StepTypeRegistry`, and `SchemaRegistry` -- all populated by plugins at load time.

### 3.2 Capability Contract

A capability contract defines what a category of plugins provides. It is identified by a string key (e.g., `"http-server"`) and associated with a Go interface type.

```go
// File: capability/contract.go
package capability

import "reflect"

// Contract defines a capability category that plugins can provide.
type Contract struct {
    // Name is the capability identifier (e.g., "http-server", "message-broker").
    Name        string
    // Description explains what this capability provides.
    Description string
    // InterfaceType is the Go reflect.Type of the interface that providers must implement.
    // This is set by the first plugin that registers this capability.
    InterfaceType reflect.Type
    // RequiredMethods lists the method signatures that any provider must implement.
    // Used for documentation and validation.
    RequiredMethods []MethodSignature
}

// MethodSignature describes a single method on a capability interface.
type MethodSignature struct {
    Name    string
    Params  []string // type names
    Returns []string // type names
}
```

### 3.3 Plugin Manifest Schema (Extended)

The existing `PluginManifest` is extended with capability declarations:

```go
// File: plugin/manifest.go (extended)

type PluginManifest struct {
    // Existing fields
    Name         string       `json:"name" yaml:"name"`
    Version      string       `json:"version" yaml:"version"`
    Author       string       `json:"author" yaml:"author"`
    Description  string       `json:"description" yaml:"description"`
    License      string       `json:"license,omitempty" yaml:"license,omitempty"`
    Dependencies []Dependency `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
    Tags         []string     `json:"tags,omitempty" yaml:"tags,omitempty"`

    // New fields for engine decomposition
    Capabilities    []CapabilityDecl  `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
    ModuleTypes     []string          `json:"moduleTypes,omitempty" yaml:"moduleTypes,omitempty"`
    StepTypes       []string          `json:"stepTypes,omitempty" yaml:"stepTypes,omitempty"`
    TriggerTypes    []string          `json:"triggerTypes,omitempty" yaml:"triggerTypes,omitempty"`
    WorkflowTypes   []string          `json:"workflowTypes,omitempty" yaml:"workflowTypes,omitempty"`
    WiringHooks     []string          `json:"wiringHooks,omitempty" yaml:"wiringHooks,omitempty"`
}

// CapabilityDecl declares that this plugin provides or requires a capability.
type CapabilityDecl struct {
    Name     string `json:"name" yaml:"name"`         // e.g., "http-server"
    Role     string `json:"role" yaml:"role"`          // "provider" or "consumer"
    Priority int    `json:"priority,omitempty" yaml:"priority,omitempty"` // for provider selection when multiple exist
}
```

### 3.4 Engine Plugin Interface (New)

The existing `NativePlugin` interface focuses on admin UI. We introduce `EnginePlugin` for engine-level contributions:

```go
// File: plugin/engine_plugin.go
package plugin

import (
    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow/capability"
    "github.com/GoCodeAlone/workflow/config"
    "github.com/GoCodeAlone/workflow/module"
    "github.com/GoCodeAlone/workflow/schema"
)

// EnginePlugin is a plugin that contributes module types, step types,
// trigger types, workflow handlers, and/or capability contracts to the engine.
type EnginePlugin interface {
    NativePlugin

    // Manifest returns the extended plugin manifest.
    Manifest() *PluginManifest

    // Capabilities returns the capability contracts this plugin defines or satisfies.
    Capabilities() []capability.Contract

    // ModuleFactories returns module type factories contributed by this plugin.
    // Key is the module type string (e.g., "http.server").
    ModuleFactories() map[string]ModuleFactory

    // StepFactories returns pipeline step type factories.
    // Key is the step type string (e.g., "step.validate").
    StepFactories() map[string]module.StepFactory

    // TriggerFactories returns trigger constructors.
    // Key is the trigger type string (e.g., "http").
    TriggerFactories() map[string]TriggerFactory

    // WorkflowHandlers returns workflow handler implementations.
    // Key is the workflow type string (e.g., "http", "messaging").
    WorkflowHandlers() map[string]WorkflowHandlerFactory

    // ModuleSchemas returns UI schema definitions for module types.
    ModuleSchemas() []*schema.ModuleSchema

    // WiringHooks returns post-init wiring functions.
    // These run after all modules are initialized and can wire cross-module integrations.
    WiringHooks() []WiringHook
}

// ModuleFactory creates a modular.Module from a name and config map.
// This is the same signature as workflow.ModuleFactory.
type ModuleFactory func(name string, config map[string]any) modular.Module

// TriggerFactory creates a Trigger instance.
type TriggerFactory func() module.Trigger

// WorkflowHandlerFactory creates a WorkflowHandler instance.
type WorkflowHandlerFactory func() interface{} // returns workflow.WorkflowHandler

// WiringHook is called after module initialization to wire cross-module integrations.
// It receives the application (for service registry access) and the workflow config.
type WiringHook func(app modular.Application, cfg *config.WorkflowConfig) error
```

### 3.5 Capability Registry

```go
// File: capability/registry.go
package capability

import (
    "fmt"
    "reflect"
    "sync"
)

// Registry tracks registered capabilities and their providers.
type Registry struct {
    mu          sync.RWMutex
    contracts   map[string]*Contract
    providers   map[string][]ProviderEntry
}

// ProviderEntry records which plugin provides a capability.
type ProviderEntry struct {
    PluginName string
    Priority   int
    // InterfaceImpl is the reflect.Type of the concrete type that satisfies the contract.
    InterfaceImpl reflect.Type
}

func NewRegistry() *Registry {
    return &Registry{
        contracts: make(map[string]*Contract),
        providers: make(map[string][]ProviderEntry),
    }
}

// RegisterContract registers a capability contract.
// Returns error if a contract with this name already exists with a different interface type.
func (r *Registry) RegisterContract(c Contract) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if existing, ok := r.contracts[c.Name]; ok {
        if existing.InterfaceType != c.InterfaceType {
            return fmt.Errorf("capability %q already registered with different interface type", c.Name)
        }
    }
    r.contracts[c.Name] = &c
    return nil
}

// RegisterProvider records that a plugin provides a capability.
func (r *Registry) RegisterProvider(capabilityName, pluginName string, priority int, implType reflect.Type) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, ok := r.contracts[capabilityName]; !ok {
        return fmt.Errorf("capability %q not registered", capabilityName)
    }

    r.providers[capabilityName] = append(r.providers[capabilityName], ProviderEntry{
        PluginName:    pluginName,
        Priority:      priority,
        InterfaceImpl: implType,
    })
    return nil
}

// Resolve returns the highest-priority provider for a capability.
func (r *Registry) Resolve(capabilityName string) (*ProviderEntry, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    providers, ok := r.providers[capabilityName]
    if !ok || len(providers) == 0 {
        return nil, fmt.Errorf("no provider for capability %q", capabilityName)
    }

    best := &providers[0]
    for i := 1; i < len(providers); i++ {
        if providers[i].Priority > best.Priority {
            best = &providers[i]
        }
    }
    return best, nil
}

// ListCapabilities returns all registered capability names.
func (r *Registry) ListCapabilities() []string { ... }

// HasProvider returns true if at least one provider exists for the capability.
func (r *Registry) HasProvider(capabilityName string) bool { ... }
```

### 3.6 Plugin Loading and Registration Flow

```
1. Core starts
2. Core scans plugin directories / compiled-in plugin list
3. For each plugin:
   a. Call plugin.Manifest() to get declarations
   b. Validate manifest (dependencies, version constraints)
   c. Call plugin.Capabilities() -- register contracts and providers
   d. Call plugin.ModuleFactories() -- register with engine.AddModuleType()
   e. Call plugin.StepFactories() -- register with engine.AddStepType()
   f. Call plugin.TriggerFactories() -- register triggers
   g. Call plugin.WorkflowHandlers() -- register handlers
   h. Call plugin.ModuleSchemas() -- register with schema registry
   i. Record wiring hooks for later execution
4. Core loads YAML config
5. BuildFromConfig uses registered factories (no built-in switch)
6. After module init, execute wiring hooks in plugin priority order
7. Start triggers and application
```

---

## 4. Plugin Categories and Groupings

### 4.1 HTTP Plugin (`workflow-plugin-http`)

**Capabilities provided:** `http-server`, `http-router`, `http-handler`, `http-middleware`, `http-proxy`, `static-files`

**Module types:**
| Type | Source File |
|---|---|
| `http.server` | `module/http.go` |
| `http.router` | `module/http.go` |
| `http.handler` | `module/http_handlers.go` |
| `http.middleware.auth` | `module/auth_middleware.go` |
| `http.middleware.logging` | `module/http_middleware.go` |
| `http.middleware.ratelimit` | `module/http_middleware.go` |
| `http.middleware.cors` | `module/http_middleware.go` |
| `http.middleware.requestid` | `module/request_id.go` |
| `http.middleware.securityheaders` | `module/security_headers.go` |
| `http.proxy` / `reverseproxy` | modular `reverseproxy` package |
| `http.simple_proxy` | `module/simple_proxy.go` |
| `static.fileserver` | `module/static_fileserver.go` (inferred from test) |

**Step types:**
| Type | Source File |
|---|---|
| `step.http_call` | `module/pipeline_step_http_call.go` |
| `step.request_parse` | `module/pipeline_step_*.go` |
| `step.json_response` | `module/pipeline_step_*.go` |
| `step.rate_limit` | `module/pipeline_step_*.go` |
| `step.circuit_breaker` | `module/pipeline_step_*.go` |

**Trigger types:** `http`

**Workflow handlers:** `http` (+ `http-*` prefix variants)

**Wiring hooks:**
- Static file server registration on router
- Health checker endpoint registration on router
- Metrics endpoint registration on router
- Log collector endpoint registration on router
- OpenAPI endpoint registration on router

### 4.2 Messaging Plugin (`workflow-plugin-messaging`)

**Capabilities provided:** `message-broker`, `message-handler`

**Module types:**
| Type | Source File |
|---|---|
| `messaging.broker` | `module/messaging.go` |
| `messaging.broker.eventbus` | `module/eventbus_bridge.go` |
| `messaging.handler` | `module/message_handlers.go` |
| `messaging.nats` | `module/nats_broker.go` |
| `messaging.kafka` | `module/kafka_broker.go` |
| `notification.slack` | `module/slack_notification.go` |
| `webhook.sender` | `module/webhook_sender.go` |

**Step types:**
| Type | Source File |
|---|---|
| `step.publish` | `module/pipeline_step_publish.go` |

**Trigger types:** `event`, `eventbus`

**Workflow handlers:** `messaging`

### 4.3 State Machine Plugin (`workflow-plugin-statemachine`)

**Capabilities provided:** `state-machine`, `state-tracking`

**Module types:**
| Type | Source File |
|---|---|
| `statemachine.engine` | `module/state_machine_test.go` (impl in module/) |
| `state.tracker` | `module/state_tracker_test.go` |
| `state.connector` | `module/state_connector.go` |

**Workflow handlers:** `statemachine`

### 4.4 Pipeline Steps Plugin (`workflow-plugin-pipeline-steps`)

**Capabilities provided:** `pipeline-steps`

This plugin provides the generic pipeline step types that are not domain-specific. The pipeline *engine* (executor, context, step interface) stays in core.

**Step types:**
| Type | Source File |
|---|---|
| `step.validate` | `module/pipeline_step_validate_test.go` |
| `step.transform` | `module/pipeline_step_*.go` |
| `step.conditional` | `module/pipeline_step_conditional_test.go` |
| `step.set` | `module/pipeline_step_set.go` |
| `step.log` | `module/pipeline_step_log.go` |
| `step.delegate` | `module/pipeline_step_*.go` |
| `step.jq` | `module/pipeline_step_*.go` |

### 4.5 Storage Plugin (`workflow-plugin-storage`)

**Capabilities provided:** `storage`, `database`, `persistence`

**Module types:**
| Type | Source File |
|---|---|
| `storage.s3` | `module/s3_storage.go` |
| `storage.local` | `module/storage_local.go` |
| `storage.gcs` | `module/storage_gcs.go` |
| `storage.sqlite` | `module/pipeline_step_*.go` (inferred) |
| `database.workflow` | `module/database.go` |
| `persistence.store` | `module/persistence.go` |

**Step types:**
| Type | Source File |
|---|---|
| `step.db_query` | `module/pipeline_step_*.go` |
| `step.db_exec` | `module/pipeline_step_*.go` |

### 4.6 Auth Plugin (`workflow-plugin-auth`)

**Capabilities provided:** `authentication`, `user-management`

**Module types:**
| Type | Source File |
|---|---|
| `auth.jwt` | `module/jwt_auth.go` |
| `auth.user-store` | `module/` |

**Wiring hooks:**
- AuthProvider-to-AuthMiddleware wiring (currently in engine.go lines 898-912)

### 4.7 Observability Plugin (`workflow-plugin-observability`)

**Capabilities provided:** `metrics`, `health-check`, `logging`, `tracing`, `openapi`

**Module types:**
| Type | Source File |
|---|---|
| `metrics.collector` | `module/metrics_test.go` |
| `health.checker` | `module/health_test.go` |
| `log.collector` | `module/log_collector.go` |
| `observability.otel` | `module/otel_tracing.go` |
| `openapi.generator` | `module/` |
| `openapi.consumer` | `module/` |

**Wiring hooks:**
- Health check endpoint registration
- Metrics endpoint registration
- Log collector endpoint registration
- OpenAPI spec building and endpoint registration
- Health check auto-discovery of HealthCheckable services

### 4.8 Feature Flags Plugin (`workflow-plugin-featureflags`)

**Capabilities provided:** `feature-flags`

**Module types:**
| Type | Source File |
|---|---|
| `featureflag.service` | `module/` |

**Step types:**
| Type | Source File |
|---|---|
| `step.feature_flag` | `module/` |
| `step.ff_gate` | `module/` |

### 4.9 AI Plugin (`workflow-plugin-ai`)

**Capabilities provided:** `ai-completion`, `ai-classification`, `ai-extraction`

**Module types:**
| Type | Source File |
|---|---|
| `dynamic.component` | `dynamic/` package |

**Step types:**
| Type | Source File |
|---|---|
| `step.ai_classify` | `module/` |
| `step.ai_complete` | `module/` |
| `step.ai_extract` | `module/` |
| `step.sub_workflow` | `module/` |

### 4.10 CI/CD Plugin (`workflow-plugin-cicd`)

**Capabilities provided:** `cicd-pipeline`

**Step types:**
| Type | Source File |
|---|---|
| `step.shell_exec` | `module/` |
| `step.artifact_pull` | `module/` |
| `step.artifact_push` | `module/` |
| `step.docker_build` | `module/` |
| `step.docker_push` | `module/` |
| `step.docker_run` | `module/` |
| `step.scan_sast` | `module/` |
| `step.scan_container` | `module/` |
| `step.scan_deps` | `module/` |
| `step.deploy` | `module/` |
| `step.gate` | `module/` |
| `step.build_ui` | `module/` |

### 4.11 Legacy Modular Plugin (`workflow-plugin-modular-compat`)

**Capabilities provided:** `scheduler`, `cache`, `database` (via modular framework modules)

**Module types:**
| Type | Source File |
|---|---|
| `scheduler.modular` | modular `scheduler` package |
| `cache.modular` | modular `cache` package |
| `database.modular` | modular `database` package |

### 4.12 CQRS / API Plugin (`workflow-plugin-api`)

**Capabilities provided:** `rest-api`, `cqrs`, `api-gateway`

**Module types:**
| Type | Source File |
|---|---|
| `api.query` | `module/api_handlers.go` |
| `api.command` | `module/api_handlers.go` |
| `api.handler` | `module/api_handlers.go` |
| `api.gateway` | `module/` |
| `workflow.registry` | `module/workflow_registry.go` |
| `data.transformer` | `module/data_transformer.go` |
| `processing.step` | `module/processing_step.go` |

### 4.13 Secrets Plugin (`workflow-plugin-secrets`)

**Capabilities provided:** `secrets-management`

**Module types:**
| Type | Source File |
|---|---|
| `secrets.vault` | `module/secrets_vault.go` |
| `secrets.aws` | `module/` |

### 4.14 Scheduler Plugin (`workflow-plugin-scheduler`)

**Capabilities provided:** `job-scheduling`

**Workflow handlers:** `scheduler`

**Trigger types:** `schedule`

### 4.15 Integration Plugin (`workflow-plugin-integration`)

**Capabilities provided:** `integration-connectors`

**Workflow handlers:** `integration`

---

## 5. Migration Strategy

### Phase 1: Foundation (Weeks 1-2)

**Goal:** Create the plugin contract system and engine plugin loader without moving any functionality yet.

**Tasks:**

1. Create `capability/` package with `Contract`, `Registry`, `ProviderEntry`
2. Create `plugin/engine_plugin.go` with `EnginePlugin` interface
3. Extend `PluginManifest` with capability, module type, step type, trigger type, and workflow type declarations
4. Add `PluginLoader` to engine that:
   - Accepts `[]EnginePlugin`
   - Calls each plugin's registration methods
   - Populates module factory map, step registry, trigger registry, workflow handlers
5. Modify `StdEngine.BuildFromConfig` to check plugin-registered factories *before* the built-in switch statement (already partially done via `moduleFactories` map)
6. Add `WiringHookRunner` that executes hooks after module init
7. Create `ModuleTypeRegistry` that replaces the hardcoded `KnownModuleTypes()` list
8. Create `EnginePluginManager` that extends `PluginManager` with engine-plugin-specific lifecycle

**Files to create:**
- `capability/contract.go`
- `capability/registry.go`
- `capability/registry_test.go`
- `plugin/engine_plugin.go`
- `plugin/engine_plugin_manager.go`
- `plugin/engine_plugin_manager_test.go`
- `plugin/loader.go`
- `plugin/loader_test.go`

**Files to modify:**
- `plugin/manifest.go` -- extend struct
- `plugin/manifest_test.go` -- new fields
- `engine.go` -- add plugin loader integration
- `schema/schema.go` -- make `KnownModuleTypes()` dynamic
- `schema/module_schema.go` -- allow runtime registration

### Phase 2: First Plugin Extraction (Weeks 3-5)

**Goal:** Extract the HTTP plugin and Observability plugin as proof-of-concept. These two are chosen because:
- HTTP is the most heavily used module category (32 configs use `http.server`)
- Observability has the most wiring hooks (health, metrics, logs, openapi)
- Together they exercise all plugin registration paths

**Tasks:**

1. **Create `workflow-plugin-http`:**
   a. Create `plugins/http/plugin.go` implementing `EnginePlugin`
   b. Move module factory logic from `engine.go` switch cases to plugin's `ModuleFactories()`
   c. Move `step.http_call`, `step.request_parse`, `step.json_response`, `step.rate_limit`, `step.circuit_breaker` to plugin's `StepFactories()`
   d. Move HTTP trigger factory to plugin's `TriggerFactories()`
   e. Move `HTTPWorkflowHandler` to plugin's `WorkflowHandlers()`
   f. Move static file server wiring to plugin's `WiringHooks()`
   g. Move relevant `ModuleSchema` registrations to plugin's `ModuleSchemas()`

2. **Create `workflow-plugin-observability`:**
   a. Create `plugins/observability/plugin.go` implementing `EnginePlugin`
   b. Move metrics, health, log collector, otel, openapi module factories
   c. Move health/metrics/log/openapi endpoint wiring hooks
   d. Move relevant schemas

3. **Remove extracted module types from `engine.go` switch statement.**

4. **Write negative tests (see Section 6.1).**

5. **Write positive tests (see Section 6.2).**

6. **Update `cmd/server/main.go` to load the extracted plugins.**

### Phase 3: Remaining Plugin Extractions (Weeks 5-8)

**Goal:** Extract all remaining module types into plugins.

**Order of extraction (by dependency and risk):**

| Order | Plugin | Rationale |
|---|---|---|
| 3a | `workflow-plugin-messaging` | Second most used, has triggers |
| 3b | `workflow-plugin-statemachine` | Has workflow handler |
| 3c | `workflow-plugin-auth` | Has wiring hook (AuthProvider wiring) |
| 3d | `workflow-plugin-storage` | Multiple backends, straightforward |
| 3e | `workflow-plugin-api` | Complex (api.handler has 50 lines of config parsing) |
| 3f | `workflow-plugin-pipeline-steps` | Generic steps, no module types |
| 3g | `workflow-plugin-cicd` | Step types only |
| 3h | `workflow-plugin-featureflags` | Module + step types |
| 3i | `workflow-plugin-secrets` | Module types only |
| 3j | `workflow-plugin-modular-compat` | Delegates to modular packages |
| 3k | `workflow-plugin-scheduler` | Workflow handler + trigger |
| 3l | `workflow-plugin-integration` | Workflow handler only |
| 3m | `workflow-plugin-ai` | Dynamic component + AI steps |

**For each plugin:**
1. Create plugin package under `plugins/<name>/`
2. Implement `EnginePlugin` interface
3. Move factory logic from `engine.go`
4. Move step registrations from `main.go`
5. Remove switch case from `engine.go`
6. Write negative test
7. Write positive test
8. Update `main.go` to load plugin

### Phase 4: Clean Core and Workflow Dependencies (Weeks 8-10)

**Goal:** The `engine.go` `BuildFromConfig` switch statement is eliminated. All module types come from plugins. Workflow configs declare their capability requirements.

**Tasks:**

1. **Remove the entire `switch modCfg.Type {` block** from `BuildFromConfig`. Replace with:
   ```go
   factory, exists := e.moduleFactories[modCfg.Type]
   if !exists {
       return fmt.Errorf("unknown module type %q: no plugin provides this type", modCfg.Type)
   }
   mod = factory(modCfg.Name, modCfg.Config)
   ```

2. **Remove all post-init wiring logic** from `engine.go`. Replace with wiring hook execution:
   ```go
   for _, hook := range e.wiringHooks {
       if err := hook(e.app, cfg); err != nil {
           return fmt.Errorf("wiring hook failed: %w", err)
       }
   }
   ```

3. **Remove `canHandleTrigger()` function.** Trigger dispatch uses the trigger's own `CanHandle` method or a type-to-trigger registry.

4. **Remove direct imports** of `module/`, `handlers/`, modular `cache`, `database`, `reverseproxy`, `scheduler` from `engine.go`.

5. **Add `requires` section to WorkflowConfig:**
   ```yaml
   requires:
     capabilities:
       - http-server
       - message-broker
       - metrics
     plugins:     # optional, for explicit plugin pinning
       - name: workflow-plugin-http
         version: ">=1.0.0"
   ```

6. **Add workflow dependency validation** that runs before `BuildFromConfig`:
   - Parse `requires` section
   - Check each capability against the capability registry
   - Return clear error messages for missing capabilities

7. **Create auto-detection tool** that scans a workflow YAML and infers required capabilities from module types used (see Section 7.2).

8. **Update all 37+ example configs** with `requires` sections.

---

## 6. Testing Strategy

### 6.1 Negative Test Pattern

Negative tests verify that the core engine does NOT contain moved functionality. They prove that the decomposition was successful.

**Pattern:** Create an engine with NO plugins loaded. Attempt to use a module type that should only exist in a plugin. Assert that it fails with a clear "unknown module type" error.

```go
// File: engine_negative_test.go
package workflow_test

import (
    "testing"

    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow"
    "github.com/GoCodeAlone/workflow/config"
)

// TestCoreDoesNotContainHTTPServer verifies that the core engine
// does not have a built-in http.server module type.
func TestCoreDoesNotContainHTTPServer(t *testing.T) {
    app := modular.NewStdApplication(nil, nil)
    engine := workflow.NewStdEngine(app, testLogger(t))

    cfg := &config.WorkflowConfig{
        Modules: []config.ModuleConfig{
            {Name: "test-server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
        },
    }

    err := engine.BuildFromConfig(cfg)
    if err == nil {
        t.Fatal("Expected error when using http.server without HTTP plugin, got nil")
    }
    if !strings.Contains(err.Error(), "unknown module type") {
        t.Fatalf("Expected 'unknown module type' error, got: %v", err)
    }
}

// TestCoreDoesNotContainMessagingBroker verifies that the core engine
// does not have a built-in messaging.broker module type.
func TestCoreDoesNotContainMessagingBroker(t *testing.T) {
    // ... same pattern ...
}
```

**Negative tests to create (one per extracted module type):**

| Test Function | Verifies Absence Of |
|---|---|
| `TestCoreDoesNotContainHTTPServer` | `http.server` |
| `TestCoreDoesNotContainHTTPRouter` | `http.router` |
| `TestCoreDoesNotContainHTTPHandler` | `http.handler` |
| `TestCoreDoesNotContainHTTPMiddleware` | `http.middleware.*` (all variants) |
| `TestCoreDoesNotContainMessagingBroker` | `messaging.broker` |
| `TestCoreDoesNotContainStateMachine` | `statemachine.engine` |
| `TestCoreDoesNotContainMetrics` | `metrics.collector` |
| `TestCoreDoesNotContainHealthChecker` | `health.checker` |
| `TestCoreDoesNotContainAuthJWT` | `auth.jwt` |
| `TestCoreDoesNotContainStorage` | `storage.s3`, `storage.local`, `storage.gcs` |
| `TestCoreDoesNotContainDatabase` | `database.workflow` |
| `TestCoreDoesNotContainFeatureFlags` | `featureflag.service` |
| `TestCoreDoesNotContainSecrets` | `secrets.vault`, `secrets.aws` |
| `TestCoreDoesNotContainModularLegacy` | `scheduler.modular`, `cache.modular`, `database.modular` |
| `TestCoreDoesNotContainAPIGateway` | `api.gateway` |
| `TestCoreDoesNotContainOpenAPI` | `openapi.generator`, `openapi.consumer` |
| `TestCoreDoesNotContainOTel` | `observability.otel` |
| `TestCoreDoesNotContainLogCollector` | `log.collector` |
| `TestCoreDoesNotContainCICDSteps` | `step.shell_exec`, `step.docker_build`, etc. |
| `TestCoreDoesNotContainAISteps` | `step.ai_classify`, `step.ai_complete`, etc. |

**Additional structural negative tests:**

```go
// TestCoreHasNoModuleImports verifies that engine.go does not import module/ or handlers/
func TestCoreHasNoModuleImports(t *testing.T) {
    // Read engine.go source and check imports
    // After Phase 4, engine.go should not import "github.com/GoCodeAlone/workflow/module"
    // or "github.com/GoCodeAlone/workflow/handlers"
}

// TestKnownModuleTypesIsEmpty verifies that the core has no hardcoded module types
func TestKnownModuleTypesIsEmpty(t *testing.T) {
    types := schema.CoreModuleTypes() // new function that returns only core types
    if len(types) != 0 {
        t.Fatalf("Expected 0 core module types, got %d: %v", len(types), types)
    }
}
```

### 6.2 Positive Test Pattern

Positive tests verify that functionality works correctly when the appropriate plugin is loaded.

**Pattern:** Create an engine, load the plugin, then verify the module type works.

```go
// File: plugins/http/plugin_test.go
package http_test

import (
    "testing"

    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow"
    "github.com/GoCodeAlone/workflow/config"
    httpplugin "github.com/GoCodeAlone/workflow/plugins/http"
)

// TestHTTPPluginProvidesHTTPServer verifies that loading the HTTP plugin
// makes http.server module type available.
func TestHTTPPluginProvidesHTTPServer(t *testing.T) {
    app := modular.NewStdApplication(nil, nil)
    engine := workflow.NewStdEngine(app, testLogger(t))

    // Load the HTTP plugin
    plugin := httpplugin.New()
    engine.LoadPlugin(plugin)

    cfg := &config.WorkflowConfig{
        Modules: []config.ModuleConfig{
            {Name: "test-server", Type: "http.server", Config: map[string]any{"address": ":0"}},
        },
    }

    err := engine.BuildFromConfig(cfg)
    if err != nil {
        t.Fatalf("Expected http.server to work with HTTP plugin loaded, got: %v", err)
    }
}

// TestHTTPPluginProvidesHTTPTrigger verifies that the HTTP trigger works.
func TestHTTPPluginProvidesHTTPTrigger(t *testing.T) {
    // ... create engine, load plugin, configure trigger, verify it starts ...
}

// TestHTTPPluginProvidesHTTPWorkflowHandler verifies HTTP workflow routing.
func TestHTTPPluginProvidesHTTPWorkflowHandler(t *testing.T) {
    // ... create engine, load plugin, configure HTTP workflow, execute ...
}
```

**Positive tests per plugin:**

| Plugin | Key Tests |
|---|---|
| HTTP | Server starts, router routes, middleware chains, trigger fires, workflow handler configures routes |
| Messaging | Broker publishes/subscribes, Kafka broker connects, event trigger fires |
| State Machine | Engine creates instances, transitions work, tracker persists state |
| Pipeline Steps | Each step type executes correctly with valid config |
| Storage | Each backend stores/retrieves data |
| Auth | JWT signs/verifies tokens, user store CRUD works |
| Observability | Metrics endpoint serves, health checks run, log collector captures |
| Feature Flags | Flag evaluation works, gate step routes correctly |
| CI/CD | Shell exec runs command, docker steps validate config |
| Secrets | Vault/AWS resolvers expand references |

### 6.3 Workflow Dependency Validation Tests

```go
// File: workflow_dependency_test.go
package workflow_test

// TestWorkflowDependencyValidation_MissingCapability verifies that loading
// a workflow requiring a capability that no plugin provides returns a clear error.
func TestWorkflowDependencyValidation_MissingCapability(t *testing.T) {
    engine := createEngineWithPlugin(httpPlugin)

    cfg := loadConfig("requires-messaging-and-http.yaml")
    // This config requires both http-server and message-broker capabilities
    // but only the HTTP plugin is loaded.

    err := engine.BuildFromConfig(cfg)
    assertErrorContains(t, err, "missing capability: message-broker")
}

// TestWorkflowDependencyValidation_AllSatisfied verifies that a workflow
// loads successfully when all required capabilities are provided.
func TestWorkflowDependencyValidation_AllSatisfied(t *testing.T) {
    engine := createEngineWithPlugins(httpPlugin, messagingPlugin)

    cfg := loadConfig("requires-messaging-and-http.yaml")
    err := engine.BuildFromConfig(cfg)
    if err != nil {
        t.Fatalf("All capabilities should be satisfied: %v", err)
    }
}

// TestWorkflowDependencyAutoDetection verifies that the auto-detector
// correctly infers required capabilities from module types in the YAML.
func TestWorkflowDependencyAutoDetection(t *testing.T) {
    cfg := loadConfig("order-processing-pipeline.yaml")
    detected := capability.DetectRequired(cfg)

    assert.Contains(t, detected, "http-server")     // uses http.server
    assert.Contains(t, detected, "http-router")      // uses http.router
    assert.Contains(t, detected, "state-machine")    // uses statemachine.engine
    assert.Contains(t, detected, "message-broker")   // uses messaging.broker
    assert.Contains(t, detected, "metrics")          // uses metrics.collector
    assert.Contains(t, detected, "health-check")     // uses health.checker
}
```

### 6.4 Plugin Substitution Tests

```go
// TestPluginSubstitution_DifferentHTTPServer verifies that replacing the
// standard HTTP plugin with an alternative that satisfies the same
// capability contract works transparently with existing workflows.
func TestPluginSubstitution_DifferentHTTPServer(t *testing.T) {
    // Create a mock alternative HTTP plugin
    altPlugin := &mockHTTPPlugin{
        factories: map[string]ModuleFactory{
            "http.server": func(name string, cfg map[string]any) modular.Module {
                return &alternativeHTTPServer{name: name}
            },
        },
        capabilities: []capability.Contract{
            {Name: "http-server", InterfaceType: reflect.TypeOf((*HTTPServer)(nil)).Elem()},
        },
    }

    engine := createEngineWithPlugin(altPlugin)
    cfg := loadConfig("simple-workflow-config.yaml")
    err := engine.BuildFromConfig(cfg)
    if err != nil {
        t.Fatalf("Alternative HTTP plugin should satisfy same capability: %v", err)
    }
}
```

---

## 7. Workflow Dependency Tracking System

### 7.1 Manifest Format for Workflow Configs

Extend `WorkflowConfig` with a `requires` section:

```go
// File: config/config.go (extended)

type WorkflowConfig struct {
    Requires  *RequiresConfig  `json:"requires,omitempty" yaml:"requires,omitempty"`
    Modules   []ModuleConfig   `json:"modules" yaml:"modules"`
    Workflows map[string]any   `json:"workflows" yaml:"workflows"`
    Triggers  map[string]any   `json:"triggers" yaml:"triggers"`
    Pipelines map[string]any   `json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
    ConfigDir string           `json:"-" yaml:"-"`
}

type RequiresConfig struct {
    // Capabilities lists the capability categories this workflow needs.
    // Any plugin providing the capability will satisfy the requirement.
    Capabilities []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`

    // Plugins lists specific plugin requirements with version constraints.
    // Use this only when a specific plugin (not just any provider of a capability) is needed.
    Plugins []PluginRequirement `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

type PluginRequirement struct {
    Name    string `json:"name" yaml:"name"`
    Version string `json:"version,omitempty" yaml:"version,omitempty"` // semver constraint
}
```

**Example workflow config with requirements:**

```yaml
requires:
  capabilities:
    - http-server
    - http-router
    - message-broker
    - state-machine
    - metrics
    - health-check

modules:
  - name: order-server
    type: http.server
    config:
      address: ":8080"
  # ... rest of config ...
```

### 7.2 Auto-Detection of Required Capabilities

A utility that scans a workflow YAML and infers which capabilities are needed based on the module types, step types, trigger types, and workflow types used.

```go
// File: capability/detect.go
package capability

import "github.com/GoCodeAlone/workflow/config"

// ModuleTypeToCapability maps module type strings to their capability category.
// This is populated by plugins during registration.
var moduleTypeToCapability = map[string][]string{}

// RegisterModuleTypeMapping records that a module type belongs to a capability.
func RegisterModuleTypeMapping(moduleType string, capabilities ...string) {
    moduleTypeToCapability[moduleType] = capabilities
}

// DetectRequired scans a WorkflowConfig and returns the set of capabilities needed.
func DetectRequired(cfg *config.WorkflowConfig) []string {
    seen := make(map[string]bool)

    // Scan module types
    for _, mod := range cfg.Modules {
        if caps, ok := moduleTypeToCapability[mod.Type]; ok {
            for _, cap := range caps {
                seen[cap] = true
            }
        }
    }

    // Scan workflow types
    for wfType := range cfg.Workflows {
        if caps, ok := workflowTypeToCapability[wfType]; ok {
            for _, cap := range caps {
                seen[cap] = true
            }
        }
    }

    // Scan trigger types
    for trigType := range cfg.Triggers {
        if caps, ok := triggerTypeToCapability[trigType]; ok {
            for _, cap := range caps {
                seen[cap] = true
            }
        }
    }

    // Scan pipeline step types
    // (requires parsing the pipeline config to extract step types)

    result := make([]string, 0, len(seen))
    for cap := range seen {
        result = append(result, cap)
    }
    sort.Strings(result)
    return result
}
```

### 7.3 Plugin Resolution and Loading

When a workflow config is loaded:

1. Parse `requires` section (if present)
2. If no `requires` section, run auto-detection
3. For each required capability, check if a provider is registered
4. If any capability is missing, return error listing:
   - Which capabilities are missing
   - Which plugins could provide them (from a known plugin catalog)
   - How to install/enable the missing plugins
5. For explicit plugin requirements, check version constraints

```go
// File: plugin/resolver.go
package plugin

// ResolveWorkflowDependencies checks that all capabilities required by a
// workflow config are satisfied by loaded plugins.
func (m *EnginePluginManager) ResolveWorkflowDependencies(cfg *config.WorkflowConfig) error {
    required := cfg.Requires
    if required == nil {
        // Auto-detect
        detected := capability.DetectRequired(cfg)
        required = &config.RequiresConfig{Capabilities: detected}
    }

    var missing []string
    for _, cap := range required.Capabilities {
        if !m.capabilityRegistry.HasProvider(cap) {
            missing = append(missing, cap)
        }
    }

    if len(missing) > 0 {
        return &MissingCapabilitiesError{
            Capabilities: missing,
            Suggestions:  m.suggestPlugins(missing),
        }
    }

    // Check explicit plugin version constraints
    for _, req := range required.Plugins {
        entry, ok := m.pluginRegistry.Get(req.Name)
        if !ok {
            return fmt.Errorf("required plugin %q is not loaded", req.Name)
        }
        if req.Version != "" {
            ok, err := CheckVersion(entry.Manifest.Version, req.Version)
            if err != nil {
                return fmt.Errorf("version check for %q: %w", req.Name, err)
            }
            if !ok {
                return fmt.Errorf("plugin %q version %s does not satisfy %s",
                    req.Name, entry.Manifest.Version, req.Version)
            }
        }
    }

    return nil
}
```

### 7.4 Example Config Audit

Each example config will be annotated with its plugin dependencies. Here is the audit of existing examples:

| Example Config | Module Types Used | Required Plugins |
|---|---|---|
| `order-processing-pipeline.yaml` | http.server, http.router, http.handler, data.transformer, statemachine.engine, state.tracker, messaging.broker, messaging.handler, metrics.collector, health.checker | http, api, statemachine, messaging, observability |
| `simple-workflow-config.yaml` | http.server, http.router, http.handler | http |
| `api-server-config.yaml` | http.server, http.router, http.handler, http.middleware.* | http |
| `event-processor-config.yaml` | http.server, http.router, messaging.broker, messaging.handler | http, messaging |
| `state-machine-workflow.yaml` | http.server, http.router, statemachine.engine, state.tracker | http, statemachine |
| `data-pipeline-config.yaml` | http.server, http.router, data.transformer | http, api |
| `multi-workflow-config.yaml` | http.server, http.router, http.handler, messaging.broker | http, messaging |
| `sms-chat-config.yaml` | http.server, http.router, messaging.broker, messaging.handler | http, messaging |
| `feature-flag-workflow.yaml` | http.server, http.router, featureflag.service | http, featureflags |
| `integration-workflow.yaml` | workflow.registry, http.server, http.router, api.handler, messaging.broker, messaging.handler | http, api, messaging, integration |
| `webhook-pipeline.yaml` | http.server, http.router, webhook.sender | http, messaging |
| `api-gateway-config.yaml` | http.server, http.router, reverseproxy | http, modular-compat |
| `api-gateway-modular-config.yaml` | http.server, http.router, reverseproxy | http, modular-compat |
| `scheduled-jobs-config.yaml` | http.server, scheduler.modular | http, modular-compat, scheduler |
| `scheduled-jobs-modular-config.yaml` | scheduler.modular | modular-compat, scheduler |
| `realtime-messaging-modular-config.yaml` | messaging.broker, messaging.handler | messaging |
| `trigger-workflow-example.yaml` | http.server, http.router, http.handler | http |
| `event-driven-workflow.yaml` | http.server, http.router, messaging.broker | http, messaging |
| `advanced-scheduler-workflow.yaml` | http.server, http.router, scheduler.modular | http, modular-compat, scheduler |
| `dependency-injection-example.yaml` | http.server, http.router | http |
| `notification-pipeline.yaml` | http.server, http.router, notification.slack, webhook.sender | http, messaging |
| `ui-build-and-serve.yaml` | http.server, http.router, static.fileserver | http |
| `data-sync-pipeline.yaml` | http.server, http.router | http |
| `test-route-pipeline.yaml` | http.server, http.router, api.query, api.command | http, api |

---

## 8. Detailed Task Breakdown

### 8.1 Phase 1 Tasks (Foundation)

| Task ID | Task | Files | Est. Hours | Dependencies |
|---|---|---|---|---|
| P1-01 | Create `capability/` package with Contract and Registry | `capability/contract.go`, `capability/registry.go`, `capability/registry_test.go` | 4 | None |
| P1-02 | Create `EnginePlugin` interface | `plugin/engine_plugin.go` | 3 | None |
| P1-03 | Extend `PluginManifest` with new fields | `plugin/manifest.go`, `plugin/manifest_test.go` | 2 | None |
| P1-04 | Create plugin loader for engine | `plugin/loader.go`, `plugin/loader_test.go` | 6 | P1-01, P1-02 |
| P1-05 | Create `EnginePluginManager` | `plugin/engine_plugin_manager.go`, `plugin/engine_plugin_manager_test.go` | 6 | P1-01, P1-02, P1-04 |
| P1-06 | Add wiring hook runner to engine | `engine.go` (modify) | 3 | P1-02 |
| P1-07 | Make `KnownModuleTypes()` dynamic | `schema/schema.go` (modify) | 2 | P1-01 |
| P1-08 | Allow runtime schema registration | `schema/module_schema.go` (modify) | 2 | None |
| P1-09 | Add `LoadPlugin()` method to `StdEngine` | `engine.go` (modify) | 4 | P1-04 |
| P1-10 | Add `requires` section to `WorkflowConfig` | `config/config.go` (modify) | 2 | None |
| P1-11 | Create capability auto-detection | `capability/detect.go`, `capability/detect_test.go` | 4 | P1-01, P1-10 |
| P1-12 | Create dependency resolver | `plugin/resolver.go`, `plugin/resolver_test.go` | 4 | P1-01, P1-05, P1-11 |

**Phase 1 total: ~42 hours**

### 8.2 Phase 2 Tasks (First Extractions)

| Task ID | Task | Files | Est. Hours | Dependencies |
|---|---|---|---|---|
| P2-01 | Create HTTP plugin skeleton | `plugins/http/plugin.go` | 4 | P1-* |
| P2-02 | Move http.server factory to HTTP plugin | `plugins/http/module_server.go` | 2 | P2-01 |
| P2-03 | Move http.router factory to HTTP plugin | `plugins/http/module_router.go` | 2 | P2-01 |
| P2-04 | Move http.handler factory to HTTP plugin | `plugins/http/module_handler.go` | 2 | P2-01 |
| P2-05 | Move all http.middleware.* factories | `plugins/http/module_middleware.go` | 4 | P2-01 |
| P2-06 | Move proxy factories | `plugins/http/module_proxy.go` | 2 | P2-01 |
| P2-07 | Move static.fileserver factory | `plugins/http/module_fileserver.go` | 2 | P2-01 |
| P2-08 | Move HTTP step types | `plugins/http/steps.go` | 3 | P2-01 |
| P2-09 | Move HTTP trigger | `plugins/http/trigger.go` | 3 | P2-01 |
| P2-10 | Move HTTP workflow handler | `plugins/http/handler.go` | 3 | P2-01 |
| P2-11 | Move HTTP-related wiring hooks | `plugins/http/wiring.go` | 4 | P2-01 |
| P2-12 | Move HTTP module schemas | `plugins/http/schemas.go` | 4 | P2-01 |
| P2-13 | Create Observability plugin skeleton | `plugins/observability/plugin.go` | 4 | P1-* |
| P2-14 | Move metrics, health, log, otel, openapi factories | `plugins/observability/modules.go` | 4 | P2-13 |
| P2-15 | Move observability wiring hooks | `plugins/observability/wiring.go` | 4 | P2-13 |
| P2-16 | Move observability schemas | `plugins/observability/schemas.go` | 3 | P2-13 |
| P2-17 | Remove extracted types from engine.go | `engine.go` (modify) | 4 | P2-02..P2-16 |
| P2-18 | Write negative tests for HTTP types | `engine_negative_test.go` | 4 | P2-17 |
| P2-19 | Write positive tests for HTTP plugin | `plugins/http/plugin_test.go` | 6 | P2-01..P2-12 |
| P2-20 | Write negative tests for observability types | `engine_negative_test.go` | 3 | P2-17 |
| P2-21 | Write positive tests for observability plugin | `plugins/observability/plugin_test.go` | 4 | P2-13..P2-16 |
| P2-22 | Update main.go to load HTTP + observability | `cmd/server/main.go` (modify) | 2 | P2-17 |
| P2-23 | Verify all existing tests still pass | (run test suite) | 3 | P2-22 |

**Phase 2 total: ~70 hours**

### 8.3 Phase 3 Tasks (Remaining Extractions)

Each plugin extraction follows the same pattern. Estimated hours per plugin:

| Plugin | Module Types | Steps | Handler | Trigger | Wiring | Schema | Tests | Total Hours |
|---|---|---|---|---|---|---|---|---|
| messaging | 7 | 1 | 1 | 2 | 0 | 7 | 8 | 20 |
| statemachine | 3 | 0 | 1 | 0 | 0 | 3 | 6 | 12 |
| auth | 2 | 0 | 0 | 0 | 1 | 2 | 5 | 10 |
| storage | 6 | 2 | 0 | 0 | 0 | 6 | 8 | 16 |
| api | 7 | 0 | 0 | 0 | 0 | 7 | 8 | 16 |
| pipeline-steps | 0 | 7 | 0 | 0 | 0 | 7 | 6 | 12 |
| cicd | 0 | 12 | 0 | 0 | 0 | 12 | 6 | 14 |
| featureflags | 1 | 2 | 0 | 0 | 0 | 3 | 5 | 8 |
| secrets | 2 | 0 | 0 | 0 | 0 | 2 | 4 | 6 |
| modular-compat | 3 | 0 | 0 | 0 | 0 | 3 | 4 | 8 |
| scheduler | 0 | 0 | 1 | 1 | 0 | 0 | 4 | 6 |
| integration | 0 | 0 | 1 | 0 | 0 | 0 | 4 | 6 |
| ai | 1 | 4 | 0 | 0 | 0 | 5 | 6 | 12 |

**Phase 3 total: ~146 hours**

### 8.4 Phase 4 Tasks (Clean Core)

| Task ID | Task | Files | Est. Hours | Dependencies |
|---|---|---|---|---|
| P4-01 | Remove BuildFromConfig switch statement | `engine.go` | 4 | P3-* |
| P4-02 | Remove all post-init wiring from engine.go | `engine.go` | 4 | P3-* |
| P4-03 | Remove canHandleTrigger function | `engine.go` | 2 | P3-* |
| P4-04 | Remove direct module/handlers imports from engine.go | `engine.go` | 3 | P4-01, P4-02 |
| P4-05 | Add dependency validation to BuildFromConfig | `engine.go` | 4 | P1-12 |
| P4-06 | Update all 37+ example configs with requires | `example/*.yaml` | 6 | P4-05 |
| P4-07 | Write structural negative tests | `engine_structural_test.go` | 4 | P4-04 |
| P4-08 | Write workflow dependency tests | `workflow_dependency_test.go` | 4 | P4-05 |
| P4-09 | Run full test suite, fix regressions | (all files) | 8 | P4-* |
| P4-10 | Update CLAUDE.md with new architecture | `CLAUDE.md` | 2 | P4-* |
| P4-11 | Create plugin development guide | `docs/PLUGIN_DEVELOPMENT.md` | 4 | P4-* |

**Phase 4 total: ~45 hours**

---

## 9. Files to Create and Modify

### 9.1 New Files

| File Path | Purpose |
|---|---|
| `capability/contract.go` | Capability contract type definitions |
| `capability/registry.go` | Capability registry implementation |
| `capability/registry_test.go` | Registry unit tests |
| `capability/detect.go` | Auto-detection of required capabilities from YAML |
| `capability/detect_test.go` | Detection tests |
| `plugin/engine_plugin.go` | EnginePlugin interface definition |
| `plugin/engine_plugin_manager.go` | Engine-level plugin lifecycle manager |
| `plugin/engine_plugin_manager_test.go` | Manager tests |
| `plugin/loader.go` | Plugin loading and registration orchestration |
| `plugin/loader_test.go` | Loader tests |
| `plugin/resolver.go` | Workflow dependency resolution |
| `plugin/resolver_test.go` | Resolver tests |
| `plugins/http/plugin.go` | HTTP plugin implementation |
| `plugins/http/modules.go` | Module factory implementations |
| `plugins/http/steps.go` | Step factory implementations |
| `plugins/http/trigger.go` | HTTP trigger factory |
| `plugins/http/handler.go` | HTTP workflow handler factory |
| `plugins/http/wiring.go` | Post-init wiring hooks |
| `plugins/http/schemas.go` | Module schema definitions |
| `plugins/http/plugin_test.go` | HTTP plugin integration tests |
| `plugins/observability/plugin.go` | Observability plugin implementation |
| `plugins/observability/modules.go` | Module factory implementations |
| `plugins/observability/wiring.go` | Post-init wiring hooks |
| `plugins/observability/schemas.go` | Module schema definitions |
| `plugins/observability/plugin_test.go` | Observability plugin tests |
| `plugins/messaging/plugin.go` | Messaging plugin |
| `plugins/messaging/modules.go` | Module factories |
| `plugins/messaging/steps.go` | Step factories |
| `plugins/messaging/trigger.go` | Event/eventbus trigger factories |
| `plugins/messaging/handler.go` | Messaging workflow handler |
| `plugins/messaging/schemas.go` | Module schemas |
| `plugins/messaging/plugin_test.go` | Tests |
| `plugins/statemachine/plugin.go` | State machine plugin |
| `plugins/statemachine/modules.go` | Module factories |
| `plugins/statemachine/handler.go` | State machine workflow handler |
| `plugins/statemachine/schemas.go` | Module schemas |
| `plugins/statemachine/plugin_test.go` | Tests |
| `plugins/auth/plugin.go` | Auth plugin |
| `plugins/auth/modules.go` | Module factories |
| `plugins/auth/wiring.go` | AuthProvider wiring hook |
| `plugins/auth/schemas.go` | Module schemas |
| `plugins/auth/plugin_test.go` | Tests |
| `plugins/storage/plugin.go` | Storage plugin |
| `plugins/storage/modules.go` | Module factories |
| `plugins/storage/steps.go` | DB step factories |
| `plugins/storage/schemas.go` | Module schemas |
| `plugins/storage/plugin_test.go` | Tests |
| `plugins/api/plugin.go` | API/CQRS plugin |
| `plugins/api/modules.go` | Module factories |
| `plugins/api/schemas.go` | Module schemas |
| `plugins/api/plugin_test.go` | Tests |
| `plugins/pipeline_steps/plugin.go` | Generic pipeline steps plugin |
| `plugins/pipeline_steps/steps.go` | Step factories |
| `plugins/pipeline_steps/schemas.go` | Step schemas |
| `plugins/pipeline_steps/plugin_test.go` | Tests |
| `plugins/cicd/plugin.go` | CI/CD plugin |
| `plugins/cicd/steps.go` | CI/CD step factories |
| `plugins/cicd/schemas.go` | Step schemas |
| `plugins/cicd/plugin_test.go` | Tests |
| `plugins/featureflags/plugin.go` | Feature flags plugin |
| `plugins/featureflags/modules.go` | Module factory |
| `plugins/featureflags/steps.go` | Step factories |
| `plugins/featureflags/schemas.go` | Module schemas |
| `plugins/featureflags/plugin_test.go` | Tests |
| `plugins/secrets/plugin.go` | Secrets plugin |
| `plugins/secrets/modules.go` | Module factories |
| `plugins/secrets/schemas.go` | Module schemas |
| `plugins/secrets/plugin_test.go` | Tests |
| `plugins/modular_compat/plugin.go` | Modular compatibility plugin |
| `plugins/modular_compat/modules.go` | Module factories (scheduler, cache, database) |
| `plugins/modular_compat/schemas.go` | Module schemas |
| `plugins/modular_compat/plugin_test.go` | Tests |
| `plugins/scheduler/plugin.go` | Scheduler plugin |
| `plugins/scheduler/handler.go` | Scheduler workflow handler |
| `plugins/scheduler/trigger.go` | Schedule trigger factory |
| `plugins/scheduler/plugin_test.go` | Tests |
| `plugins/integration/plugin.go` | Integration plugin |
| `plugins/integration/handler.go` | Integration workflow handler |
| `plugins/integration/plugin_test.go` | Tests |
| `plugins/ai/plugin.go` | AI plugin |
| `plugins/ai/modules.go` | Dynamic component factory |
| `plugins/ai/steps.go` | AI step factories |
| `plugins/ai/schemas.go` | Module schemas |
| `plugins/ai/plugin_test.go` | Tests |
| `engine_negative_test.go` | All negative tests for core |
| `engine_structural_test.go` | Structural tests (no forbidden imports) |
| `workflow_dependency_test.go` | Workflow dependency validation tests |

### 9.2 Modified Files

| File Path | Changes |
|---|---|
| `engine.go` | Remove switch statement, add plugin loader, add wiring hook runner, remove post-init wiring, remove direct module/handler imports |
| `config/config.go` | Add `RequiresConfig` struct and field |
| `plugin/manifest.go` | Add capability, module type, step type, trigger type, workflow type, wiring hook fields |
| `plugin/manifest_test.go` | Tests for new fields |
| `schema/schema.go` | Make `KnownModuleTypes()` delegate to registry, add `CoreModuleTypes()` |
| `schema/module_schema.go` | Move `registerBuiltins()` content to plugins, make registry accept runtime registrations |
| `cmd/server/main.go` | Replace direct handler/step registration with plugin loading |
| `example/*.yaml` (37+ files) | Add `requires:` section to each |

### 9.3 Files to Eventually Remove (or Empty)

After all module types are extracted to plugins, the following files become plugin-internal and should not be imported by the core:

| Current Location | Moves To |
|---|---|
| `module/http.go` | `plugins/http/` (still exists, imported by plugin not core) |
| `module/http_handlers.go` | `plugins/http/` |
| `module/http_middleware.go` | `plugins/http/` |
| `module/messaging.go` | `plugins/messaging/` |
| `module/message_handlers.go` | `plugins/messaging/` |
| `handlers/http.go` | `plugins/http/` |
| `handlers/messaging.go` | `plugins/messaging/` |
| `handlers/state_machine.go` | `plugins/statemachine/` |
| `handlers/scheduler.go` | `plugins/scheduler/` |
| `handlers/integration.go` | `plugins/integration/` |

Note: The `module/` package files do not physically move. The plugins import them. The key change is that `engine.go` no longer imports `module/` for factory construction -- that responsibility moves to each plugin. The `module/` package becomes a library of implementations that plugins wrap.

---

## 10. Post-Decomposition Core Engine

After all phases are complete, `engine.go` will contain approximately:

```go
package workflow

// StdEngine -- the core workflow engine
type StdEngine struct {
    app              modular.Application
    workflowHandlers []WorkflowHandler
    moduleFactories  map[string]ModuleFactory
    stepRegistry     *StepRegistry       // moved from module/ to core
    triggerRegistry  *TriggerRegistry    // moved from module/ to core
    wiringHooks      []WiringHook
    capabilityReg    *capability.Registry
    logger           modular.Logger
    secretsResolver  *secrets.MultiResolver
    configDir        string
}

func (e *StdEngine) BuildFromConfig(cfg *config.WorkflowConfig) error {
    // 1. Validate capabilities
    if err := e.validateRequirements(cfg); err != nil {
        return err
    }

    // 2. Validate config schema
    if err := schema.ValidateConfig(cfg, e.validationOptions()...); err != nil {
        return err
    }

    // 3. Instantiate modules from config using plugin-registered factories
    for _, modCfg := range cfg.Modules {
        expandConfigStrings(e.secretsResolver, modCfg.Config)

        factory, exists := e.moduleFactories[modCfg.Type]
        if !exists {
            return fmt.Errorf("unknown module type %q: no plugin provides this type", modCfg.Type)
        }

        mod := factory(modCfg.Name, modCfg.Config)
        e.app.RegisterModule(mod)
    }

    // 4. Initialize all modules
    if err := e.app.Init(); err != nil {
        return err
    }

    // 5. Execute wiring hooks (plugin-provided)
    for _, hook := range e.wiringHooks {
        if err := hook(e.app, cfg); err != nil {
            return err
        }
    }

    // 6. Configure workflows
    for workflowType, workflowConfig := range cfg.Workflows {
        // ... dispatch to registered handlers (unchanged) ...
    }

    // 7. Configure triggers
    if err := e.configureTriggers(cfg.Triggers); err != nil {
        return err
    }

    // 8. Configure pipelines
    if len(cfg.Pipelines) > 0 {
        if err := e.configurePipelines(cfg.Pipelines); err != nil {
            return err
        }
    }

    return nil
}
```

**Core line count estimate: ~400 lines** (down from ~1647 currently).

**Core imports:** Only `modular`, `config`, `schema`, `capability`, `secrets`, `plugin` -- no `module/`, no `handlers/`, no modular `cache/database/reverseproxy/scheduler`.

---

## 11. Risk Mitigation

| Risk | Mitigation |
|---|---|
| Breaking existing tests during migration | Phase 2 maintains backward compatibility: engine checks plugin factories first, falls back to switch statement. Tests keep passing throughout. |
| Plugin load order matters | EnginePluginManager performs topological sort based on declared dependencies before loading. Circular dependencies are detected and reported. |
| Performance impact of indirection | Module factory lookup is a single map access (O(1)). Wiring hooks add a linear scan that runs once at startup. No runtime performance impact. |
| Module package becomes orphaned | Module package remains as a library of implementations. Plugins import it. No code duplication. |
| Backward compatibility of YAML configs | The `requires` section is optional. Auto-detection works for configs that omit it. Existing configs work unchanged with a "default plugin set" that loads all standard plugins. |
| Plugin versioning conflicts | Semver constraints enforced by existing manifest validation. Registry prevents downgrades. |

---

## 12. Success Criteria

The decomposition is complete when:

1. `engine.go` contains no module type switch statement
2. `engine.go` does not import `module/` or `handlers/` packages
3. All negative tests pass (core rejects unknown module types)
4. All positive tests pass (plugins provide module types)
5. All 37+ example configs load and validate successfully
6. All existing integration tests pass
7. A new plugin can be created and loaded without modifying any core file
8. Two different plugins providing the same capability can be swapped without changing workflow configs
9. The `requires` section (or auto-detection) correctly identifies all plugin dependencies for every example config
10. `go build ./cmd/server` succeeds with and without optional plugins (the server binary size decreases when plugins are excluded)
