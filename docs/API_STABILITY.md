# API Stability Contract

This document defines the public API surface of the `github.com/GoCodeAlone/workflow` Go module, the semver policy governing changes, and the contract for embedding the engine in downstream applications.

## Table of Contents

- [Public API Packages](#public-api-packages)
- [Internal Packages](#internal-packages)
- [Semver Policy](#semver-policy)
- [Deprecation Policy](#deprecation-policy)
- [Embedding Contract](#embedding-contract)

---

## Public API Packages

The following packages form the stable, versioned public API. They are safe to import in application code and plugins. Breaking changes to these packages require a major version bump (post-1.0) or are accompanied by migration notes (pre-1.0).

### `github.com/GoCodeAlone/workflow` (root)

Core engine types. The root package is the primary integration point for embedding the engine.

| Symbol | Kind | Description |
|--------|------|-------------|
| `Engine` | interface | Minimal interface for embedding: `RegisterWorkflowHandler`, `RegisterTrigger`, `AddModuleType`, `BuildFromConfig`, `Start`, `Stop`, `TriggerWorkflow` |
| `StdEngine` | struct | Full engine implementation. Created with `NewStdEngine`. |
| `NewStdEngine(app, logger)` | func | Creates a new workflow engine backed by a `modular.Application`. |
| `WorkflowHandler` | interface | Handler for a workflow type: `CanHandle`, `ConfigureWorkflow`, `ExecuteWorkflow`. |
| `PipelineAdder` | interface | Optional extension on `WorkflowHandler`: `AddPipeline(name, *module.Pipeline)`. |
| `RoutePipelineSetter` | interface | Optional: `SetRoutePipeline(routePath, *module.Pipeline)`. |
| `ModuleFactory` | type | `func(name string, config map[string]any) modular.Module` |
| `StartStopModule` | interface | `modular.Module` extended with `Start(ctx)` and `Stop(ctx)`. |
| `(*StdEngine).App()` | method | Returns the underlying `modular.Application`. |
| `(*StdEngine).GetApp()` | method | Alias for `App()`. |
| `(*StdEngine).LoadPlugin(EnginePlugin)` | method | Loads an `EnginePlugin`, registering its factories, schemas, and hooks. |
| `(*StdEngine).LoadedPlugins()` | method | Returns all loaded `EnginePlugin` instances. |
| `(*StdEngine).BuildFromConfig(*config.WorkflowConfig)` | method | Instantiates modules, configures workflows, triggers, and pipelines from config. |
| `(*StdEngine).Start(ctx)` | method | Starts all modules and triggers. |
| `(*StdEngine).Stop(ctx)` | method | Stops all triggers and modules. |
| `(*StdEngine).TriggerWorkflow(ctx, type, action, data)` | method | Dispatches a workflow execution to the matching handler. |
| `(*StdEngine).RegisterWorkflowHandler(WorkflowHandler)` | method | Adds a handler to the engine. |
| `(*StdEngine).RegisterTrigger(module.Trigger)` | method | Adds a trigger to the engine. |
| `(*StdEngine).RegisterTriggerType(type, name)` | method | Maps a trigger config key to a trigger `Name()`. |
| `(*StdEngine).RegisterTriggerConfigWrapper(type, fn)` | method | Registers a flat-to-native config wrapper for pipeline triggers. |
| `(*StdEngine).AddModuleType(type, ModuleFactory)` | method | Registers a custom module factory. |
| `(*StdEngine).AddStepType(type, module.StepFactory)` | method | Registers a custom pipeline step factory. |
| `(*StdEngine).GetStepRegistry()` | method | Returns the engine's `*module.StepRegistry`. |
| `(*StdEngine).PluginLoader()` | method | Returns the engine's `*plugin.PluginLoader` (created lazily). |
| `(*StdEngine).SetPluginLoader(*plugin.PluginLoader)` | method | Overrides the plugin loader. |
| `(*StdEngine).SetPluginInstaller(*plugin.PluginInstaller)` | method | Enables auto-installation of missing plugins. |
| `(*StdEngine).SecretsResolver()` | method | Returns the engine's `*secrets.MultiResolver`. |

---

### `github.com/GoCodeAlone/workflow/config`

YAML configuration structs. These types are the schema for workflow config files.

| Symbol | Kind | Description |
|--------|------|-------------|
| `WorkflowConfig` | struct | Top-level config: `Modules`, `Workflows`, `Triggers`, `Pipelines`, `Platform`, `Requires`, `ConfigDir`. |
| `ModuleConfig` | struct | Single module definition: `Name`, `Type`, `Config`, `DependsOn`, `Branches`. |
| `RequiresConfig` | struct | Declared requirements: `Capabilities`, `Plugins`. |
| `PluginRequirement` | struct | Plugin requirement: `Name`, `Version` (semver constraint). |
| `PipelineConfig` | struct | Pipeline definition: `Trigger`, `Steps`, `OnError`, `Timeout`, `Compensation`. |
| `PipelineTriggerConfig` | struct | Inline pipeline trigger: `Type`, `Config`. |
| `PipelineStepConfig` | struct | Step definition: `Name`, `Type`, `Config`, `OnError`, `Timeout`. |
| `LoadFromFile(path)` | func | Parses a YAML config file into `*WorkflowConfig`. |
| `LoadFromString(yaml)` | func | Parses a YAML string into `*WorkflowConfig`. |
| `NewEmptyWorkflowConfig()` | func | Returns an initialized empty `*WorkflowConfig`. |
| `ResolvePathInConfig(cfg, path)` | func | Resolves a path relative to `_config_dir` stored in a module config map. |
| `(*WorkflowConfig).ResolveRelativePath(path)` | method | Resolves a path relative to the config file's directory. |

---

### `github.com/GoCodeAlone/workflow/plugin`

Plugin system types. Use these to build and load engine plugins.

| Symbol | Kind | Description |
|--------|------|-------------|
| `EnginePlugin` | interface | Full engine plugin: extends `NativePlugin` with `EngineManifest`, `Capabilities`, `ModuleFactories`, `StepFactories`, `TriggerFactories`, `WorkflowHandlers`, `ModuleSchemas`, `WiringHooks`. |
| `NativePlugin` | interface | HTTP/lifecycle plugin: `Name`, `Version`, `Description`, `Dependencies`, `UIPages`, `RegisterRoutes`, `OnEnable`, `OnDisable`. |
| `BaseEnginePlugin` | struct | No-op embed for `EnginePlugin`; override only what you need. |
| `BaseNativePlugin` | struct | No-op embed for `NativePlugin`. |
| `PipelineTriggerConfigProvider` | interface | Optional on `EnginePlugin`: `PipelineTriggerConfigWrappers() map[string]TriggerConfigWrapperFunc`. |
| `NativePluginProvider` | interface | Optional on `EnginePlugin`: `NativePlugins(PluginContext) []NativePlugin`. |
| `PluginManifest` | struct | Plugin metadata: `Name`, `Version`, `Author`, `Description`, `License`, `Dependencies`, `Capabilities`, `Tags`, `Repository`, and type lists. |
| `CapabilityDecl` | struct | Manifest capability declaration: `Name`, `Role` (`"provider"` or `"consumer"`), `Priority`. |
| `Dependency` | struct | Plugin dependency: `Name`, `Constraint` (semver, e.g. `">=1.0.0"`). |
| `PluginDependency` | struct | Native plugin dependency: `Name`, `MinVersion`. |
| `PluginContext` | struct | Shared resources passed to lifecycle hooks: `App`, `DB`, `Logger`, `DataDir`. |
| `UIPageDef` | struct | UI page metadata contributed by a plugin. |
| `ModuleFactory` | type | `func(name string, config map[string]any) modular.Module` |
| `StepFactory` | type | `func(name string, config map[string]any, app modular.Application) (any, error)` |
| `TriggerFactory` | type | `func() any` — must return a `module.Trigger`. |
| `WorkflowHandlerFactory` | type | `func() any` — must return a `WorkflowHandler`. |
| `TriggerConfigWrapperFunc` | type | `func(pipelineName string, flatConfig map[string]any) map[string]any` |
| `WiringHook` | struct | Post-init cross-module wiring: `Name`, `Priority`, `Hook func(modular.Application, *config.WorkflowConfig) error`. |
| `PluginLoader` | struct | Loads and validates `EnginePlugin` instances. |
| `NewPluginLoader(capReg, schemaReg)` | func | Creates a `PluginLoader`. |
| `(*PluginLoader).LoadPlugin(EnginePlugin)` | method | Validates manifest, registers all factories and schemas. |
| `(*PluginLoader).LoadPlugins([]EnginePlugin)` | method | Topologically sorts and loads a set of plugins. |
| `(*PluginLoader).LoadedPlugins()` | method | Returns all loaded plugins in load order. |
| `(*PluginLoader).WiringHooks()` | method | Returns all wiring hooks sorted by priority. |
| `(*PluginLoader).CapabilityRegistry()` | method | Returns the backing `*capability.Registry`. |
| `(*PluginLoader).ModuleFactories()` | method | Returns a copy of all registered module factories. |
| `(*PluginLoader).StepFactories()` | method | Returns a copy of all registered step factories. |
| `(*PluginLoader).TriggerFactories()` | method | Returns a copy of all registered trigger factories. |
| `(*PluginLoader).WorkflowHandlerFactories()` | method | Returns a copy of all registered workflow handler factories. |
| `PluginInstaller` | struct | Installs plugin binaries to a directory. |
| `(*PluginManifest).Validate()` | method | Checks required fields and semver validity. |
| `LoadManifest(path)` | func | Reads a manifest from a JSON file. |
| `Semver` | struct | Parsed semver: `Major`, `Minor`, `Patch`. |
| `Constraint` | struct | Semver constraint: `Op`, `Version`. |
| `ParseSemver(v)` | func | Parses a version string into `Semver`. |
| `ParseConstraint(s)` | func | Parses a constraint string (`>=`, `^`, `~`, etc.) into `*Constraint`. |
| `CheckVersion(version, constraint)` | func | Checks if a version string satisfies a constraint string. |
| `(*Constraint).Check(Semver)` | method | Returns true if the given version satisfies the constraint. |

---

### `github.com/GoCodeAlone/workflow/module`

Module implementations and core pipeline types. Import these to implement custom triggers, steps, or handlers.

**Core interfaces and types:**

| Symbol | Kind | Description |
|--------|------|-------------|
| `Trigger` | interface | Extends `modular.Module`, `modular.Startable`, `modular.Stoppable` with `Configure(app, triggerConfig) error`. |
| `TriggerRegistry` | struct | Registry of triggers by name. |
| `NewTriggerRegistry()` | func | Creates an empty `TriggerRegistry`. |
| `(*TriggerRegistry).RegisterTrigger(Trigger)` | method | Adds a trigger. |
| `(*TriggerRegistry).GetTrigger(name)` | method | Returns a trigger by name. |
| `(*TriggerRegistry).GetAllTriggers()` | method | Returns all registered triggers. |
| `PipelineStep` | interface | `Name() string`, `Execute(ctx, *PipelineContext) (*StepResult, error)`. |
| `PipelineContext` | struct | Pipeline execution context: `TriggerData`, `StepOutputs`, `Current`, `Metadata`. |
| `StepResult` | struct | Step output: `Output`, `NextStep`, `Stop`. |
| `NewPipelineContext(triggerData, metadata)` | func | Creates an initialized pipeline context. |
| `(*PipelineContext).MergeStepOutput(name, output)` | method | Records a step output and merges it into `Current`. |
| `Pipeline` | struct | Executable pipeline with named steps, error strategy, timeout, and compensation steps. |
| `StepFactory` | type | `func(name string, config map[string]any, app modular.Application) (PipelineStep, error)` |
| `StepRegistry` | struct | Maps step type strings to `StepFactory` functions. |
| `NewStepRegistry()` | func | Creates an empty `StepRegistry`. |
| `(*StepRegistry).Register(type, StepFactory)` | method | Adds a step factory. |
| `(*StepRegistry).Create(type, name, config, app)` | method | Instantiates a step by type. |
| `(*StepRegistry).Types()` | method | Returns all registered step type names. |
| `ErrorStrategyStop` | const | Pipeline stops on first step error (default). |
| `ErrorStrategySkip` | const | Pipeline skips failed steps and continues. |
| `ErrorStrategyCompensate` | const | Pipeline runs compensation steps on failure. |

**Event types:**

| Symbol | Kind | Description |
|--------|------|-------------|
| `WorkflowEventEmitter` | struct | Publishes lifecycle events to the EventBus. Safe to use when EventBus is unavailable. |
| `NewWorkflowEventEmitter(app)` | func | Creates a `WorkflowEventEmitter`. |
| `WorkflowLifecycleEvent` | struct | Payload for workflow-level events: `WorkflowType`, `Action`, `Status`, `Timestamp`, `Duration`, `Data`, `Error`, `Results`. |
| `StepLifecycleEvent` | struct | Payload for step-level events. |
| `WorkflowTopic(workflowType, lifecycle)` | func | Returns the event bus topic for a workflow lifecycle event. |
| `StepTopic(workflowType, stepName, lifecycle)` | func | Returns the event bus topic for a step lifecycle event. |
| `LifecycleStarted` | const | `"started"` |
| `LifecycleCompleted` | const | `"completed"` |
| `LifecycleFailed` | const | `"failed"` |

**Notable exported module types** (each implements `modular.Module`):

- `HTTPServer`, `HTTPRouter` — HTTP server and routing
- `MessagingModule` — message broker integration
- `SchedulerModule` — cron-based scheduling
- `DatabaseModule` — database connection management
- `EventProcessorModule` — event processing
- `MetricsCollector` — workflow execution metrics
- `EncryptionModule` — field encryption
- `AuthMiddleware` — authentication middleware
- `LogCollector` — execution log aggregation
- `WorkflowRegistry` — in-process workflow registry
- `EventBusBridge` — EventBus ↔ workflow event bridge

> **Note:** The set of concrete module types is additive; new modules may be added in minor versions without breaking changes.

---

### `github.com/GoCodeAlone/workflow/schema`

Module schema definitions used by the UI and config validation.

| Symbol | Kind | Description |
|--------|------|-------------|
| `ModuleSchema` | struct | Full schema for a module type: `Type`, `Label`, `Category`, `Description`, `Inputs`, `Outputs`, `ConfigFields`, `DefaultConfig`, `MaxIncoming`, `MaxOutgoing`. |
| `ConfigFieldDef` | struct | Single config field: `Key`, `Label`, `Type`, `Description`, `Required`, `DefaultValue`, `Options`, `Placeholder`, `Group`, `ArrayItemType`, `MapValueType`, `InheritFrom`, `Sensitive`. |
| `ServiceIODef` | struct | Service port descriptor: `Name`, `Type`, `Description`. |
| `ConfigFieldType` | type | String enum for field types. |
| `FieldTypeString`, `FieldTypeNumber`, `FieldTypeBool`, `FieldTypeSelect`, `FieldTypeJSON`, `FieldTypeDuration`, `FieldTypeArray`, `FieldTypeMap`, `FieldTypeFilePath`, `FieldTypeSQL` | const | `ConfigFieldType` constants. |
| `ModuleSchemaRegistry` | struct | Registry of `ModuleSchema` by type string. |
| `NewModuleSchemaRegistry()` | func | Creates a registry pre-populated with all built-in schemas. |
| `(*ModuleSchemaRegistry).Register(*ModuleSchema)` | method | Adds or replaces a schema. |
| `(*ModuleSchemaRegistry).Unregister(type)` | method | Removes a schema (test cleanup). |
| `(*ModuleSchemaRegistry).Get(type)` | method | Returns the schema for a type. |
| `(*ModuleSchemaRegistry).All()` | method | Returns all registered schemas as a slice. |
| `(*ModuleSchemaRegistry).AllMap()` | method | Returns all registered schemas as a map. |
| `RegisterModuleType(type)` | func | Registers a type string as known to the global schema registry. |
| `ValidateConfig(cfg, ...ValidationOption)` | func | Validates a `*config.WorkflowConfig` against registered schemas. |
| `ValidationOption` | type | Functional option for `ValidateConfig`. |
| `WithAllowEmptyModules()` | func | Allows configs with no modules. |
| `WithSkipWorkflowTypeCheck()` | func | Skips workflow type validation. |
| `WithSkipTriggerTypeCheck()` | func | Skips trigger type validation. |
| `WithExtraModuleTypes(types...)` | func | Adds additional valid module type strings for validation. |

---

### `github.com/GoCodeAlone/workflow/capability`

Capability contract system for declaring and resolving plugin capabilities.

| Symbol | Kind | Description |
|--------|------|-------------|
| `Contract` | struct | Capability definition: `Name`, `Description`, `InterfaceType`, `RequiredMethods`. |
| `MethodSignature` | struct | Method descriptor: `Name`, `Params`, `Returns`. |
| `ProviderEntry` | struct | Registered provider: `PluginName`, `Priority`, `InterfaceImpl`. |
| `Registry` | struct | Thread-safe registry of contracts and providers. |
| `NewRegistry()` | func | Creates an empty `Registry`. |
| `(*Registry).RegisterContract(Contract)` | method | Adds a capability contract. |
| `(*Registry).RegisterProvider(capName, pluginName, priority, implType)` | method | Registers a plugin as a provider. |
| `(*Registry).Resolve(capName)` | method | Returns the highest-priority provider. |
| `(*Registry).HasProvider(capName)` | method | Returns true if any provider is registered. |
| `(*Registry).ListCapabilities()` | method | Returns sorted capability names. |
| `(*Registry).ListProviders(capName)` | method | Returns all providers for a capability. |
| `(*Registry).ContractFor(capName)` | method | Returns the contract for a capability. |

---

### `github.com/GoCodeAlone/workflow/store`

Persistence interfaces for events, executions, users, and workflows. Implement these to plug in custom storage backends.

**Primary store interfaces:**

| Symbol | Kind | Description |
|--------|------|-------------|
| `ExecutionStore` | interface | CRUD for `WorkflowExecution` and `ExecutionStep` records. |
| `LogStore` | interface | `Append` and `Query` for execution logs. |
| `AuditStore` | interface | `Record` and `Query` for audit entries. |
| `UserStore` | interface | CRUD for user records. |
| `CompanyStore` | interface | CRUD for company/organization records. |
| `OrganizationStore` | type alias | Alias for `CompanyStore`. |
| `ProjectStore` | interface | CRUD for project records. |
| `WorkflowStore` | interface | CRUD and versioning for workflow records. |
| `MembershipStore` | interface | CRUD and role resolution for memberships. |
| `SessionStore` | interface | CRUD for user sessions. |
| `CrossWorkflowLinkStore` | interface | CRUD for cross-workflow links. |
| `IAMStore` | interface | CRUD for IAM providers and role mappings. |
| `Pagination` | struct | Common pagination: `Offset`, `Limit`. |
| `DefaultPagination()` | func | Returns `{Offset: 0, Limit: 50}`. |

---

### `github.com/GoCodeAlone/workflow/handlers`

Workflow handler implementations. Register these with the engine via `RegisterWorkflowHandler`.

| Symbol | Kind | Description |
|--------|------|-------------|
| `HTTPWorkflowHandler` | struct | Routes HTTP requests to registered handler modules. |
| `NewHTTPWorkflowHandler()` | func | Creates an `HTTPWorkflowHandler`. |
| `HTTPRouteConfig` | struct | Route configuration: `Method`, `Path`, `Handler`, `Middlewares`, `Config`. |
| `MessagingWorkflowHandler` | struct | Routes messaging events to handlers. |
| `NewMessagingWorkflowHandler()` | func | Creates a `MessagingWorkflowHandler`. |
| `PipelineWorkflowHandler` | struct | Executes named pipeline workflows. |
| `NewPipelineWorkflowHandler()` | func | Creates a `PipelineWorkflowHandler`. |
| `(*PipelineWorkflowHandler).AddPipeline(name, *module.Pipeline)` | method | Registers a named pipeline. |
| `(*PipelineWorkflowHandler).SetStepRegistry(*module.StepRegistry)` | method | Sets the step registry. |
| `(*PipelineWorkflowHandler).SetLogger(logger)` | method | Sets the execution logger. |
| `(*PipelineWorkflowHandler).SetEventRecorder(recorder)` | method | Sets the event recorder. |
| `StateMachineWorkflowHandler` | struct | Manages state machine workflows. |
| `NewStateMachineWorkflowHandler()` | func | Creates a `StateMachineWorkflowHandler`. |
| `SchedulerWorkflowHandler` | struct | Manages scheduled/cron workflows. |
| `NewSchedulerWorkflowHandler()` | func | Creates a `SchedulerWorkflowHandler`. |
| `IntegrationWorkflowHandler` | struct | Manages integration workflows (external APIs, webhooks). |
| `NewIntegrationWorkflowHandler()` | func | Creates an `IntegrationWorkflowHandler`. |
| `PlatformWorkflowHandler` | struct | Manages platform-level workflows. |
| `NewPlatformWorkflowHandler()` | func | Creates a `PlatformWorkflowHandler`. |

---

### `github.com/GoCodeAlone/workflow/versioning`

Workflow configuration versioning with rollback and diff.

| Symbol | Kind | Description |
|--------|------|-------------|
| `WorkflowVersion` | struct | Versioned snapshot: `WorkflowName`, `Version`, `ConfigYAML`, `Description`, `CreatedBy`, `CreatedAt`. |
| `Diff` | struct | Version comparison result: `WorkflowName`, `FromVersion`, `ToVersion`, `FromConfig`, `ToConfig`, `Changed`. |
| `RollbackFunc` | type | `func(workflowName, configYAML string) error` — applies a restored config. |
| `VersionStore` | struct | In-memory version store. Thread-safe. |
| `NewVersionStore()` | func | Creates a new `VersionStore`. |
| `(*VersionStore).Save(name, yaml, desc, createdBy)` | method | Saves a new version (auto-increments version number). |
| `(*VersionStore).Get(name, version)` | method | Retrieves a specific version. |
| `(*VersionStore).Latest(name)` | method | Returns the latest version. |
| `(*VersionStore).List(name)` | method | Returns all versions, newest first. |
| `(*VersionStore).ListWorkflows()` | method | Returns all workflow names with versions. |
| `(*VersionStore).Count(name)` | method | Returns the number of stored versions. |
| `Rollback(store, name, targetVersion, createdBy, applyFn)` | func | Restores a version and saves it as a new version. |
| `Compare(store, name, fromVersion, toVersion)` | func | Returns a `*Diff` between two versions. |

---

## Internal Packages

The following packages are **not part of the stable public API**. They may change between any version without notice. Do not import them in plugins or embedding applications.

| Package | Reason |
|---------|--------|
| `github.com/GoCodeAlone/workflow/cmd/...` | CLI binaries; not importable as libraries. |
| `github.com/GoCodeAlone/workflow/dynamic` | Yaegi hot-reload internals. API is volatile as the interpreter integration evolves. |
| `github.com/GoCodeAlone/workflow/ai` | AI integration layer. Experimental; provider APIs change frequently. |
| `github.com/GoCodeAlone/workflow/ai/llm` | Anthropic Claude direct API integration. Experimental. |
| `github.com/GoCodeAlone/workflow/ai/copilot` | GitHub Copilot SDK integration. Technical Preview. |
| `github.com/GoCodeAlone/workflow/admin` | Admin server internals. Not designed for external embedding. |
| `github.com/GoCodeAlone/workflow/mock` | Test helpers. Not stable; only for use in the repo's own test suite. |
| `github.com/GoCodeAlone/workflow/secrets` | Secrets resolver internals. Accessed via `StdEngine.SecretsResolver()` only. Note: `StdEngine.SecretsResolver()` returns `*secrets.MultiResolver`. Callers should treat this type as opaque and not depend on its internal methods. A future version may replace this with a public interface. |
| `github.com/GoCodeAlone/workflow/plugin/external` | gRPC-based external plugin protocol. Wire format may change. |
| `github.com/GoCodeAlone/workflow/plugin/rbac` | RBAC plugin internals. Use the `plugin.EnginePlugin` interface instead. |
| `github.com/GoCodeAlone/workflow/plugin/sdk` | Plugin scaffolding/generation tools. Internal CLI tooling only. |
| `github.com/GoCodeAlone/workflow/plugin/community` | Community plugin validator internals. |

---

## Semver Policy

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

### Version format: `MAJOR.MINOR.PATCH`

| Component | When it increments | API impact |
|-----------|-------------------|------------|
| **PATCH** (0.x.Y) | Bug fixes, documentation, internal refactors | No API changes. Safe to update. |
| **MINOR** (0.X.0) | New features, new exported symbols, new module types | Backward-compatible additions to public packages. Safe to update. |
| **MAJOR** (X.0.0) | Breaking changes to public API packages | Required after 1.0 for any breaking change. |

### Pre-1.0 Disclaimer

**The project is currently pre-1.0.** During this phase:

- **Minor versions MAY contain breaking changes** to public API packages.
- Breaking changes in minor releases will be accompanied by a `MIGRATION.md` note or changelog entry describing what changed and how to update.
- Patch versions (0.x.Y) are strictly backward-compatible.
- Internal packages (listed above) may change in any release without notice.

Once v1.0.0 is tagged, strict semver compatibility guarantees apply to all public API packages.

### What constitutes a breaking change

For public API packages, the following are breaking changes requiring a major version bump (post-1.0):

- Removing or renaming an exported type, function, method, or constant.
- Adding a required method to a published interface (e.g., `WorkflowHandler`, `Trigger`).
- Changing a method signature (parameter types, return types, parameter order).
- Changing the semantics of an existing method in a way that breaks existing callers.
- Removing a field from a published struct that was explicitly documented as stable.

The following are **not** breaking changes:

- Adding new exported symbols (functions, types, methods, constants).
- Adding new optional fields to a struct (with a zero-value that preserves existing behavior).
- Adding new implementations of an interface the engine already consumes internally.
- Changes to internal packages.
- Changes to example YAML configs.

---

## Deprecation Policy

Before removing or changing a public API symbol:

1. The symbol is marked with a `// Deprecated: Use X instead.` comment.
2. The deprecated symbol remains in at least **2 minor version releases** after the deprecation comment appears.
3. The replacement API (if any) is available in the same release as the deprecation.
4. The changelog notes the deprecation and the planned removal version.

---

## Embedding Contract

The workflow engine is designed to be embedded in downstream Go applications (e.g., [GoCodeAlone/ratchet](https://github.com/GoCodeAlone/ratchet)).

### `go.mod` requirements

```go
require (
    github.com/GoCodeAlone/workflow v0.x.y
)
```

- Use an explicit `require` directive with a pinned version.
- **Do not** use `replace` directives pointing at a local checkout except during active development; they prevent reproducible builds.
- To track a minor version range, use a tool like `go get github.com/GoCodeAlone/workflow@v0.x` and pin the latest resolved version in `go.sum`.

### Only depend on public API packages

Import only from the [Public API Packages](#public-api-packages) listed above. Do not import from `dynamic`, `ai`, `admin`, `mock`, `secrets`, or other internal packages — they are not part of the embedding contract and may break without notice.

### Recommended embedding pattern

```go
package main

import (
    "context"
    "log/slog"

    "github.com/CrisisTextLine/modular"
    workflow "github.com/GoCodeAlone/workflow"
    "github.com/GoCodeAlone/workflow/config"
    "github.com/GoCodeAlone/workflow/handlers"
    "github.com/GoCodeAlone/workflow/plugin"
)

func main() {
    app := modular.NewApplication()
    logger := slog.Default()

    engine := workflow.NewStdEngine(app, logger)

    // Load built-in or custom plugins
    engine.LoadPlugin(&myapp.MyPlugin{})

    // Register workflow handlers
    engine.RegisterWorkflowHandler(handlers.NewPipelineWorkflowHandler())
    engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

    // Load configuration
    cfg, err := config.LoadFromFile("config.yaml")
    if err != nil {
        panic(err)
    }

    if err := engine.BuildFromConfig(cfg); err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := engine.Start(ctx); err != nil {
        panic(err)
    }

    // ... serve traffic ...

    engine.Stop(ctx)
}
```

### Plugin development

Implement `plugin.EnginePlugin` (embed `plugin.BaseEnginePlugin`) to contribute module types, step types, triggers, workflow handlers, schemas, and wiring hooks:

```go
type MyPlugin struct {
    plugin.BaseEnginePlugin
}

func (p *MyPlugin) ModuleFactories() map[string]plugin.ModuleFactory {
    return map[string]plugin.ModuleFactory{
        "my.module": func(name string, cfg map[string]any) modular.Module {
            return NewMyModule(name, cfg)
        },
    }
}
```

See [`docs/PLUGIN_DEVELOPMENT_GUIDE.md`](PLUGIN_DEVELOPMENT_GUIDE.md) for a full walkthrough.

### Interface stability for plugin authors

Plugin authors implement interfaces the engine consumes. To avoid breaking plugin binaries when the engine adds optional methods to consumed interfaces, embed a base struct rather than implementing interfaces directly:

- Always embed `plugin.BaseEnginePlugin` in `EnginePlugin` implementations.
- When implementing `module.Trigger`, use the `module.Trigger` interface directly (it is intentionally minimal).
- When implementing `WorkflowHandler`, implement all three methods (`CanHandle`, `ConfigureWorkflow`, `ExecuteWorkflow`).

The engine uses runtime type assertions for optional interfaces (`PipelineAdder`, `RoutePipelineSetter`, `PipelineTriggerConfigProvider`, `NativePluginProvider`) — you do not need to implement these unless you need the capability.
