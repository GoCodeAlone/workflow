# Internal EnginePlugin Development Guide

This guide covers how to create **built-in engine plugins** — compiled-in Go packages that register module types, step types, triggers, workflow handlers, and wiring hooks with the workflow engine.

> For **external** (gRPC-based, process-isolated) plugins, see [PLUGIN_DEVELOPMENT_GUIDE.md](PLUGIN_DEVELOPMENT_GUIDE.md).

## Overview

The workflow engine is decomposed into a minimal core and a set of plugins. The core handles YAML parsing, module lifecycle, service registry, workflow dispatch, pipeline execution, and plugin loading. Everything else — HTTP, messaging, state machines, storage, auth, observability — lives in plugins under `plugins/`.

Each plugin implements the `plugin.EnginePlugin` interface and contributes:

| Contribution | Method | Description |
|---|---|---|
| Module types | `ModuleFactories()` | Factory functions that create `modular.Module` instances |
| Step types | `StepFactories()` | Factory functions that create pipeline steps |
| Trigger types | `TriggerFactories()` | Constructors for trigger instances |
| Workflow handlers | `WorkflowHandlers()` | Handlers that process workflow sections in YAML |
| Capabilities | `Capabilities()` | Capability contracts this plugin satisfies |
| UI schemas | `ModuleSchemas()` | Schema definitions for the workflow builder UI |
| Wiring hooks | `WiringHooks()` | Post-init cross-module integration logic |
| Modernize rules | `ModernizeRules()` _(optional)_ | Migration rules for `wfctl modernize` |

## The EnginePlugin Interface

```go
// plugin/engine_plugin.go
type EnginePlugin interface {
    NativePlugin // Name(), Version(), Description(), Dependencies(), ...

    EngineManifest() *PluginManifest
    Capabilities() []capability.Contract
    ModuleFactories() map[string]ModuleFactory
    StepFactories() map[string]StepFactory
    TriggerFactories() map[string]TriggerFactory
    WorkflowHandlers() map[string]WorkflowHandlerFactory
    ModuleSchemas() []*schema.ModuleSchema
    WiringHooks() []WiringHook
}
```

**`BaseEnginePlugin`** provides no-op defaults for every method. Embed it and override only what your plugin needs.

## Creating a Plugin: Step by Step

### 1. Create the package

```
plugins/myplugin/
├── plugin.go       # Plugin struct, manifest, capabilities
├── modules.go      # ModuleFactories() implementation
├── steps.go        # StepFactories() implementation (if any)
├── trigger.go      # TriggerFactories() implementation (if any)
├── wiring.go       # WiringHooks() implementation (if any)
├── schemas.go      # ModuleSchemas() implementation
└── plugin_test.go  # Tests
```

### 2. Define the plugin struct

```go
package myplugin

import (
    "github.com/GoCodeAlone/workflow/plugin"
)

type Plugin struct {
    plugin.BaseEnginePlugin
}

func New() *Plugin {
    return &Plugin{
        BaseEnginePlugin: plugin.BaseEnginePlugin{
            BaseNativePlugin: plugin.BaseNativePlugin{
                PluginName:        "workflow-plugin-myplugin",
                PluginVersion:     "1.0.0",
                PluginDescription: "Short description of what this plugin provides",
            },
            Manifest: plugin.PluginManifest{
                Name:        "workflow-plugin-myplugin",
                Version:     "1.0.0",
                Author:      "YourName",
                Description: "Short description of what this plugin provides",
                Tier:        plugin.TierCommunity, // or TierCore
                ModuleTypes: []string{"myplugin.worker"},
                Capabilities: []plugin.CapabilityDecl{
                    {Name: "my-capability", Role: "provider", Priority: 10},
                },
            },
        },
    }
}
```

### 3. Register module factories

Module factories create `modular.Module` instances from a name and config map:

```go
// modules.go
package myplugin

import (
    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow/plugin"
)

func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
    return map[string]plugin.ModuleFactory{
        "myplugin.worker": func(name string, cfg map[string]any) modular.Module {
            address, _ := cfg["address"].(string)
            return NewWorkerModule(name, address)
        },
    }
}
```

The key in the map (e.g., `"myplugin.worker"`) is the module type used in YAML configs:

```yaml
modules:
  - name: my-worker
    type: myplugin.worker
    config:
      address: ":9090"
```

### 4. Register step factories (optional)

Step factories create pipeline step instances:

```go
// steps.go
package myplugin

import (
    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow/plugin"
)

func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
    return map[string]plugin.StepFactory{
        "step.my_transform": func(name string, cfg map[string]any, app modular.Application) (any, error) {
            return NewMyTransformStep(name, cfg)
        },
    }
}
```

The returned value must implement `module.PipelineStep`:

```go
type PipelineStep interface {
    Name() string
    Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error)
}
```

### 5. Register trigger factories (optional)

```go
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
    return map[string]plugin.TriggerFactory{
        "my-trigger": func() any {
            return NewMyTrigger()
        },
    }
}
```

The returned trigger must implement `module.Trigger`:

```go
type Trigger interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Configure(app modular.Application, config any) error
}
```

### 6. Register workflow handlers (optional)

```go
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
    return map[string]plugin.WorkflowHandlerFactory{
        "my-workflow": func() any {
            return NewMyWorkflowHandler()
        },
    }
}
```

Workflow handlers process named sections under `workflows:` in YAML configs.

### 7. Declare capabilities

Capabilities let workflow configs declare *what they need* rather than *which plugins*:

```go
func (p *Plugin) Capabilities() []capability.Contract {
    return []capability.Contract{
        {
            Name:          "my-capability",
            Description:   "Provides my-capability for workflow configs",
            InterfaceType: reflect.TypeOf((*MyInterface)(nil)).Elem(),
        },
    }
}
```

Workflow configs reference capabilities in the `requires` section:

```yaml
requires:
  capabilities:
    - my-capability
```

### 8. Add wiring hooks (optional)

Wiring hooks run after all modules are initialized, enabling cross-module integration:

```go
func (p *Plugin) WiringHooks() []plugin.WiringHook {
    return []plugin.WiringHook{
        {
            Name:     "myplugin-wiring",
            Priority: 50, // higher priority runs first
            Hook: func(app modular.Application, cfg *config.WorkflowConfig) error {
                // Wire module A to module B
                var svcA *ServiceA
                if err := app.GetService("service-a", &svcA); err != nil {
                    return nil // service not present, skip
                }
                // ... perform wiring
                return nil
            },
        },
    }
}
```

Wiring hooks are the replacement for hardcoded post-init logic in the engine. They enable plugins to wire their modules together without the engine knowing the details.

### 9. Add UI schemas

```go
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
    return []*schema.ModuleSchema{
        {
            Type:     "myplugin.worker",
            Category: "Custom",
            Inputs:   []string{"config"},
            Outputs:  []string{"result"},
            ConfigFields: []schema.ConfigField{
                {Name: "address", Type: "string", Required: true, Description: "Listen address"},
            },
        },
    }
}
```

### 10. Load the plugin

Register the plugin in `cmd/server/main.go`:

```go
import pluginmyplugin "github.com/GoCodeAlone/workflow/plugins/myplugin"

// In main():
if err := engine.LoadPlugin(pluginmyplugin.New()); err != nil {
    log.Fatalf("Failed to load myplugin: %v", err)
}
```

Also add it to `testhelpers_test.go` → `allPlugins()` for test coverage.

### 11. Declare modernize rules (optional)

If your plugin introduces or renames module/step types over time, you can declare **modernize rules** so that users can automatically detect and fix stale configs using `wfctl modernize`.

#### In-process Go plugins

Implement the optional `plugin.ModernizeRulesProvider` interface:

```go
import "github.com/GoCodeAlone/workflow/modernize"

// ModernizeRules returns migration rules for the wfctl modernize command.
func (p *Plugin) ModernizeRules() []modernize.Rule {
    return []modernize.Rule{
        modernize.ManifestRule{
            ID:            "myplugin-rename-v2",
            Description:   "Rename myplugin.old_worker to myplugin.worker (v2.0 migration)",
            Severity:      "error",
            OldModuleType: "myplugin.old_worker",
            NewModuleType: "myplugin.worker",
        }.MustToRule(),
        modernize.ManifestRule{
            ID:          "myplugin-rename-endpoint",
            Description: "Rename apiEndpoint to endpoint in myplugin.worker config",
            ModuleType:  "myplugin.worker",
            OldKey:      "apiEndpoint",
            NewKey:      "endpoint",
        }.MustToRule(),
    }
}
```

The engine will call `ModernizeRules()` on plugins that implement `ModernizeRulesProvider`. You can also write arbitrary Check/Fix functions for complex migrations that go beyond type and key renaming.

#### External plugins via `plugin.json`

External (process-isolated) plugins declare rules in their `plugin.json` manifest under the `modernizeRules` key:

```json
{
  "name": "my-vendor-plugin",
  "version": "2.0.0",
  "author": "Vendor Inc.",
  "description": "Vendor plugin for workflow",
  "moduleTypes": ["vendor.connector"],
  "modernizeRules": [
    {
      "id": "vendor-rename-type",
      "description": "Rename vendor.old_connector to vendor.connector (v2 migration)",
      "severity": "error",
      "oldModuleType": "vendor.old_connector",
      "newModuleType": "vendor.connector"
    },
    {
      "id": "vendor-rename-key",
      "description": "Rename apiEndpoint to endpoint in vendor.connector config",
      "moduleType": "vendor.connector",
      "oldKey": "apiEndpoint",
      "newKey": "endpoint"
    },
    {
      "id": "vendor-rename-step",
      "description": "Rename step.vendor_fetch to step.vendor_get",
      "oldStepType": "step.vendor_fetch",
      "newStepType": "step.vendor_get"
    }
  ]
}
```

Users with the plugin installed run:

```bash
wfctl modernize --plugin-dir data/plugins config.yaml
wfctl modernize --apply --plugin-dir data/plugins config.yaml
```

#### ManifestRule fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Required. Unique kebab-case rule identifier |
| `description` | string | Required. Human-readable description |
| `severity` | string | `"error"` or `"warning"` (default: `"warning"`) |
| `message` | string | Override the auto-generated finding message |
| `oldModuleType` | string | Trigger a module type rename: old type to detect |
| `newModuleType` | string | Target module type after rename |
| `oldStepType` | string | Trigger a step type rename: old type to detect |
| `newStepType` | string | Target step type after rename |
| `moduleType` | string | Scope a config key rename to modules of this type |
| `stepType` | string | Scope a config key rename to steps of this type |
| `oldKey` | string | Config key to detect (used with `moduleType` or `stepType`) |
| `newKey` | string | Replacement config key |

**Exactly one rule kind must be configured per rule:**
- Module type rename: `oldModuleType` + `newModuleType`
- Step type rename: `oldStepType` + `newStepType`
- Module config key rename: `moduleType` + `oldKey` + `newKey`
- Step config key rename: `stepType` + `oldKey` + `newKey`

## Plugin Manifest

The `PluginManifest` struct declares metadata used for discovery, dependency resolution, and the admin UI:

```go
type PluginManifest struct {
    Name           string                    // unique plugin name (kebab-case)
    Version        string                    // semver, e.g. "1.0.0"
    Author         string                    // required
    Description    string                    // required
    Tier           PluginTier                // TierCore, TierOfficial, TierCommunity
    ModuleTypes    []string                  // module types this plugin provides
    StepTypes      []string                  // step types this plugin provides
    TriggerTypes   []string                  // trigger types this plugin provides
    WorkflowTypes  []string                  // workflow handler types
    WiringHooks    []string                  // names of wiring hooks
    Capabilities   []CapabilityDecl          // capability declarations
    Dependencies   []Dependency              // plugin dependencies with version constraints
    ModernizeRules []modernize.ManifestRule  // migration rules for wfctl modernize
}
```

**All of `Name`, `Version`, `Author`, and `Description` are required** — the plugin loader validates these during `LoadPlugin()`.

The `ModernizeRules` field allows plugins to embed migration rules directly in the Go manifest struct. For external plugins (process-isolated), declare the equivalent rules in `plugin.json` — see [Declare modernize rules](#11-declare-modernize-rules-optional) above.

## Workflow Dependency Validation

Configs can declare required capabilities and plugins:

```yaml
requires:
  capabilities:
    - http-server
    - message-broker
  plugins:
    - name: workflow-plugin-http
      version: ">=1.0.0"
```

During `BuildFromConfig`, the engine:
1. Checks that every listed capability has at least one registered provider
2. Checks that every listed plugin is loaded (with optional semver constraint matching)
3. Returns clear error messages listing all missing requirements

This enables workflows to fail fast with actionable errors rather than cryptic runtime failures.

## Existing Plugins

| Plugin | Package | Module Types | Key Capabilities |
|---|---|---|---|
| HTTP | `plugins/http` | http.server, http.router, http.handler, http.proxy, ... | http-server, http-router, http-middleware |
| Messaging | `plugins/messaging` | messaging.broker, messaging.handler | message-broker |
| State Machine | `plugins/statemachine` | statemachine.engine | state-machine |
| Auth | `plugins/auth` | auth.jwt, auth.basic, auth.apikey | authentication |
| Storage | `plugins/storage` | storage.s3, storage.local, storage.gcs | object-storage |
| API | `plugins/api` | api.query, api.command, api.gateway | api-gateway |
| Observability | `plugins/observability` | metrics.collector, health.checker, log.collector | metrics, health-check |
| Pipeline Steps | `plugins/pipelinesteps` | (step types only) | pipeline-steps |
| Scheduler | `plugins/scheduler` | scheduler.cron, scheduler.job | scheduling |
| Secrets | `plugins/secrets` | secrets.vault, secrets.aws, secrets.env | secrets-management |
| Feature Flags | `plugins/featureflags` | featureflag.service | feature-flags |
| Integration | `plugins/integration` | integration.webhook, integration.adapter | integration |
| AI | `plugins/ai` | ai.classifier, ai.generator | ai-processing |
| Platform | `plugins/platform` | (platform module types) | platform |
| License | `plugins/license` | (license module types) | licensing |
| CI/CD | `plugins/cicd` | (step types for CI/CD) | cicd |
| Modular Compat | `plugins/modularcompat` | scheduler.modular, cache.modular, database.modular | legacy-compat |

## Testing Your Plugin

```go
// plugin_test.go
package myplugin

import (
    "log/slog"
    "testing"

    "github.com/CrisisTextLine/modular"
    "github.com/GoCodeAlone/workflow"
    "github.com/GoCodeAlone/workflow/config"
)

func TestPluginLoads(t *testing.T) {
    app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)
    engine := workflow.NewStdEngine(app, slog.Default())

    if err := engine.LoadPlugin(New()); err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }
}

func TestModuleCreation(t *testing.T) {
    app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)
    engine := workflow.NewStdEngine(app, slog.Default())
    if err := engine.LoadPlugin(New()); err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    cfg := &config.WorkflowConfig{
        Modules: []config.ModuleConfig{
            {Name: "test", Type: "myplugin.worker", Config: map[string]any{"address": ":0"}},
        },
        Workflows: map[string]any{},
        Triggers:  map[string]any{},
    }

    if err := engine.BuildFromConfig(cfg); err != nil {
        t.Fatalf("BuildFromConfig failed: %v", err)
    }
}
```

## Best Practices

1. **Single responsibility**: Each plugin should cover one domain (HTTP, messaging, auth, etc.).
2. **Use `BaseEnginePlugin`**: Embed it to get no-op defaults; override only what you need.
3. **Declare capabilities**: Always declare what your plugin provides so configs can validate dependencies.
4. **Graceful wiring hooks**: Wiring hooks should be resilient — if an optional service isn't present, skip rather than fail.
5. **Complete manifests**: Fill in all manifest fields including `ModuleTypes`, `StepTypes`, `TriggerTypes` for discoverability.
6. **Test in isolation**: Test your plugin with only its own dependencies loaded, not all 17 plugins.
