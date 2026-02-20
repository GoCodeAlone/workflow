# External Plugin Development Guide

## Overview

External plugins run as separate OS processes that communicate with the workflow engine over gRPC, powered by [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin). They can provide **module types**, **step types**, and **trigger types** that are loaded and unloaded at runtime without recompiling the engine.

The engine treats external plugins identically to built-in plugins. Both implement the `EnginePlugin` interface; the engine does not know whether a plugin is compiled-in or running in a separate process. An `ExternalPluginAdapter` wraps the gRPC client so the engine sees a standard `EnginePlugin`.

Key properties of external plugins:

- **Process isolation**: Each plugin runs in its own OS process. A crash in the plugin does not crash the engine.
- **Language agnostic**: Any language that can implement the gRPC protocol can serve as a plugin (Go SDK provided, protobuf definitions available for other languages).
- **Hot load/unload**: Plugins can be loaded, unloaded, and reloaded at runtime via the HTTP API.
- **Bidirectional communication**: Plugins can call back to the engine to trigger workflows, look up services, and send log entries.

## Quick Start

### 1. Install the SDK

```bash
go get github.com/GoCodeAlone/workflow/plugin/external/sdk
```

### 2. Implement the Plugin Provider

Every external plugin must implement the `sdk.PluginProvider` interface, which returns metadata about the plugin:

```go
type PluginProvider interface {
    Manifest() sdk.Manifest
}
```

To provide step types, also implement `sdk.StepProvider`:

```go
type StepProvider interface {
    StepTypes() []string
    CreateStep(typeName, name string, config map[string]any) (sdk.Step, error)
}
```

To provide module types, also implement `sdk.ModuleProvider`:

```go
type ModuleProvider interface {
    ModuleTypes() []string
    CreateModule(typeName, name string, config map[string]any) (sdk.Module, error)
}
```

### 3. Call `sdk.Serve()` in `main()`

```go
func main() {
    sdk.Serve(&MyPlugin{})
}
```

The `Serve` function handles:
- Setting the magic cookie environment variable for the go-plugin handshake
- Starting the gRPC server
- Registering all provided services
- Blocking until the host process terminates the connection

## Plugin Manifest

Every external plugin must include a `plugin.json` file in its directory. This file provides metadata used during discovery, dependency resolution, and display in the admin UI.

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "author": "Your Name",
  "description": "A brief description of what this plugin does"
}
```

### Manifest Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Lowercase alphanumeric with hyphens. Must be unique across all plugins. |
| `version` | Yes | Semantic version string (e.g., `1.0.0`, `0.2.1`). |
| `author` | Yes | Author name or organization. |
| `description` | Yes | Human-readable description of the plugin's purpose. |
| `license` | No | SPDX license identifier (e.g., `MIT`, `Apache-2.0`). |
| `tags` | No | Array of search/categorization tags. |
| `repository` | No | URL to the source code repository. |
| `dependencies` | No | Array of `{"name": "other-plugin", "constraint": ">=1.0.0"}` objects. |

The `name` field must match the directory name under `data/plugins/`. Names are validated against the pattern `^[a-z][a-z0-9-]*[a-z0-9]$` (minimum 2 characters) or a single lowercase letter.

## Directory Layout

External plugins live under the `data/plugins/` directory relative to the engine's working directory. Each plugin gets its own subdirectory. The binary inside must have the same name as the directory.

```
data/plugins/
  my-plugin/
    plugin.json       # manifest (required)
    my-plugin         # compiled binary (same name as directory)
  another-plugin/
    plugin.json
    another-plugin
```

The engine scans this directory on startup to discover available plugins. You can also trigger discovery at runtime through the API.

## Step Plugin Example

Pipeline steps are the most common type of external plugin. A step receives a `PipelineContext` (trigger data, previous step outputs, current merged state, metadata), performs some operation, and returns output data.

Here is a complete example of a step plugin that converts string values to uppercase:

```go
package main

import (
	"context"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// UppercasePlugin provides a single step type: "step.uppercase".
type UppercasePlugin struct{}

// Manifest returns the plugin metadata.
func (p *UppercasePlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:        "uppercase",
		Version:     "1.0.0",
		Author:      "Example Author",
		Description: "Converts string values to uppercase",
	}
}

// StepTypes returns the step type names this plugin provides.
func (p *UppercasePlugin) StepTypes() []string {
	return []string{"step.uppercase"}
}

// CreateStep creates a new step instance of the given type.
func (p *UppercasePlugin) CreateStep(typeName, name string, config map[string]any) (sdk.Step, error) {
	field, _ := config["field"].(string)
	if field == "" {
		field = "value" // default field to transform
	}
	return &UppercaseStep{name: name, field: field}, nil
}

// UppercaseStep implements sdk.Step.
type UppercaseStep struct {
	name  string
	field string
}

// Name returns the step's instance name.
func (s *UppercaseStep) Name() string {
	return s.name
}

// Execute reads a string field from the pipeline context's current data,
// converts it to uppercase, and returns the result.
func (s *UppercaseStep) Execute(ctx context.Context, pc *sdk.PipelineContext) (*sdk.StepResult, error) {
	input, _ := pc.Current[s.field].(string)
	output := strings.ToUpper(input)

	return &sdk.StepResult{
		Output: map[string]any{
			s.field: output,
		},
	}, nil
}

func main() {
	sdk.Serve(&UppercasePlugin{})
}
```

### Using the Step in a Workflow YAML

Once the plugin is loaded, the step type is available in any workflow configuration:

```yaml
workflows:
  - name: process-text
    type: pipeline
    trigger:
      type: http
      config:
        method: POST
        path: /process
    pipeline:
      steps:
        - name: uppercase-name
          type: step.uppercase
          config:
            field: name
        - name: uppercase-email
          type: step.uppercase
          config:
            field: email
```

## Module Plugin Example

Module plugins provide full `modular.Module` implementations that participate in the engine's lifecycle (Init, Start, Stop). They can provide services, depend on other modules, and run background tasks.

```go
package main

import (
	"context"
	"log"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// MetricsPlugin provides a custom metrics collector module.
type MetricsPlugin struct{}

func (p *MetricsPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:        "custom-metrics",
		Version:     "1.0.0",
		Author:      "Example Author",
		Description: "Custom metrics collection module",
	}
}

func (p *MetricsPlugin) ModuleTypes() []string {
	return []string{"custom.metrics_collector"}
}

func (p *MetricsPlugin) CreateModule(typeName, name string, config map[string]any) (sdk.Module, error) {
	endpoint, _ := config["endpoint"].(string)
	interval, _ := config["interval"].(string)
	return &MetricsModule{
		name:     name,
		endpoint: endpoint,
		interval: interval,
	}, nil
}

// MetricsModule implements sdk.Module.
type MetricsModule struct {
	name     string
	endpoint string
	interval string
}

func (m *MetricsModule) Name() string         { return m.name }
func (m *MetricsModule) Dependencies() []string { return nil }

func (m *MetricsModule) Init() error {
	log.Printf("MetricsModule %s: initialized with endpoint=%s interval=%s",
		m.name, m.endpoint, m.interval)
	return nil
}

func (m *MetricsModule) Start(ctx context.Context) error {
	log.Printf("MetricsModule %s: started", m.name)
	// Start collecting metrics in a goroutine, respect ctx.Done()
	return nil
}

func (m *MetricsModule) Stop(ctx context.Context) error {
	log.Printf("MetricsModule %s: stopped", m.name)
	return nil
}

func (m *MetricsModule) InvokeService(method string, args map[string]any) (map[string]any, error) {
	switch method {
	case "get_metrics":
		return map[string]any{
			"requests_total": 42,
			"errors_total":   3,
		}, nil
	default:
		return nil, nil
	}
}

func main() {
	sdk.Serve(&MetricsPlugin{})
}
```

### Using the Module in a Workflow YAML

```yaml
modules:
  - name: app-metrics
    type: custom.metrics_collector
    config:
      endpoint: http://metrics-server:9090
      interval: 30s
```

## Module Schemas for the UI

External plugins can provide UI schema definitions so the workflow editor knows how to render configuration forms for the plugin's module types. Implement the `sdk.SchemaProvider` interface:

```go
func (p *MetricsPlugin) ModuleSchemas() []sdk.ModuleSchema {
	return []sdk.ModuleSchema{
		{
			Type:        "custom.metrics_collector",
			Label:       "Metrics Collector",
			Category:    "observability",
			Description: "Collects and reports custom metrics",
			Inputs: []sdk.ServiceIODef{
				{Name: "config", Type: "object", Description: "Collector configuration"},
			},
			Outputs: []sdk.ServiceIODef{
				{Name: "metrics", Type: "object", Description: "Collected metrics data"},
			},
			ConfigFields: []sdk.ConfigFieldDef{
				{Name: "endpoint", Type: "string", Description: "Metrics server endpoint", Required: true},
				{Name: "interval", Type: "string", Description: "Collection interval (e.g., 30s, 1m)", DefaultValue: "30s"},
			},
		},
	}
}
```

These schemas are automatically merged into the engine's schema registry and served to the UI at `/api/v1/module-schemas`.

## Engine Callbacks

External plugins can call back to the engine through the `EngineCallbackService` gRPC interface. The SDK wraps this as a `Callback` object passed during plugin initialization:

### Trigger a Workflow

```go
err := callback.TriggerWorkflow("http", "POST /webhook", map[string]any{
    "source": "my-plugin",
    "event":  "data_ready",
})
```

### Look Up a Host Service

```go
found := callback.GetService("user-store")
if found {
    // The host has a "user-store" service available
}
```

### Send Log Entries

```go
callback.Log("info", "Processing complete", map[string]any{
    "items_processed": 150,
    "duration_ms":     342,
})
```

Log entries from plugins are routed to the engine's logger with a `[plugin]` prefix and the log level tag.

## Testing

### Unit Testing Steps

Test your step logic independently without gRPC by constructing the step directly:

```go
package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func TestUppercaseStep(t *testing.T) {
	step := &UppercaseStep{name: "test-step", field: "value"}

	result, err := step.Execute(context.Background(), &sdk.PipelineContext{
		TriggerData: map[string]any{"source": "test"},
		StepOutputs: map[string]map[string]any{},
		Current:     map[string]any{"value": "hello world"},
		Metadata:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["value"] != "HELLO WORLD" {
		t.Errorf("expected HELLO WORLD, got %v", result.Output["value"])
	}
}
```

### Integration Testing with go-plugin Test Mode

HashiCorp go-plugin supports a test mode where the plugin runs in-process, avoiding subprocess management in tests:

```go
package main

import (
	"testing"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/plugin/external"
)

func TestPluginIntegration(t *testing.T) {
	// Create a test plugin client that runs in-process
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: external.Handshake,
		Plugins: map[string]goplugin.Plugin{
			"plugin": &external.GRPCPlugin{},
		},
		Cmd:              nil, // nil Cmd = test mode
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
	})
	defer client.Kill()

	// Use the client to test your plugin's gRPC interface...
}
```

### Testing the Full Lifecycle

For end-to-end testing, build the plugin binary and use the engine's discovery mechanism:

```bash
# Build the plugin
cd my-plugin/
go build -o my-plugin .

# Create the expected directory structure
mkdir -p data/plugins/my-plugin
cp my-plugin data/plugins/my-plugin/
cp plugin.json data/plugins/my-plugin/

# Start the engine — it discovers the plugin on startup
cd ../../
go run ./cmd/server -config my-config.yaml
```

Then use the API to load the plugin and verify it works:

```bash
# Load the plugin
curl -X POST http://localhost:8081/api/v1/plugins/external/my-plugin/load

# List loaded plugins to verify
curl http://localhost:8081/api/v1/plugins/external/loaded

# Test a workflow that uses the plugin's step type
curl -X POST http://localhost:8081/process -d '{"name": "test"}'
```

## Deployment

### Building the Plugin

```bash
cd my-plugin/
go build -o my-plugin .
```

Cross-compile for the target platform if needed:

```bash
GOOS=linux GOARCH=amd64 go build -o my-plugin .
```

### Installing the Plugin

Place the built binary and manifest in the correct directory structure:

```bash
mkdir -p data/plugins/my-plugin
cp my-plugin data/plugins/my-plugin/
cp plugin.json data/plugins/my-plugin/
```

### Discovery and Loading

The engine discovers external plugins through two mechanisms:

1. **Startup discovery**: On boot, the engine scans `data/plugins/` for directories containing both a `plugin.json` manifest and a matching binary.

2. **Runtime API**: Use the HTTP API to load, unload, and reload plugins without restarting the engine.

### Runtime Load/Unload via API

Load a discovered plugin:
```bash
curl -X POST http://localhost:8081/api/v1/plugins/external/my-plugin/load
```

Unload a running plugin (graceful shutdown of the subprocess):
```bash
curl -X POST http://localhost:8081/api/v1/plugins/external/my-plugin/unload
```

Reload a plugin (unload + load, useful after updating the binary):
```bash
curl -X POST http://localhost:8081/api/v1/plugins/external/my-plugin/reload
```

## API Endpoints

All external plugin management endpoints are under the `/api/v1/plugins/external` prefix.

### List Available Plugins

```
GET /api/v1/plugins/external
```

Returns all plugins discovered in the `data/plugins/` directory, whether loaded or not.

**Response:**
```json
[
  {
    "name": "my-plugin",
    "version": "1.0.0",
    "author": "Example Author",
    "description": "Converts string values to uppercase",
    "loaded": false
  }
]
```

### List Loaded Plugins

```
GET /api/v1/plugins/external/loaded
```

Returns only the plugins that are currently loaded and active.

**Response:**
```json
[
  {
    "name": "my-plugin",
    "version": "1.0.0",
    "author": "Example Author",
    "description": "Converts string values to uppercase",
    "loaded": true,
    "moduleTypes": [],
    "stepTypes": ["step.uppercase"],
    "triggerTypes": []
  }
]
```

### Load a Plugin

```
POST /api/v1/plugins/external/{name}/load
```

Starts the plugin subprocess, performs the handshake, and registers its types with the engine. Returns an error if the plugin is already loaded or if the binary is not found.

**Response (success):**
```json
{
  "status": "loaded",
  "name": "my-plugin",
  "moduleTypes": [],
  "stepTypes": ["step.uppercase"],
  "triggerTypes": []
}
```

### Unload a Plugin

```
POST /api/v1/plugins/external/{name}/unload
```

Gracefully stops the plugin subprocess and removes its types from the engine. Any modules or steps created by the plugin become unavailable. Returns an error if the plugin is not currently loaded.

**Response (success):**
```json
{
  "status": "unloaded",
  "name": "my-plugin"
}
```

### Reload a Plugin

```
POST /api/v1/plugins/external/{name}/reload
```

Equivalent to unload followed by load. Useful after updating the plugin binary on disk.

**Response (success):**
```json
{
  "status": "reloaded",
  "name": "my-plugin",
  "moduleTypes": [],
  "stepTypes": ["step.uppercase"],
  "triggerTypes": []
}
```

## Data Flow

When a workflow step is backed by an external plugin, the execution flow is:

```
1. Pipeline engine calls step.Execute(ctx, pipelineContext)
2. RemoteStep converts PipelineContext to protobuf:
   - TriggerData   -> google.protobuf.Struct
   - StepOutputs   -> map<string, google.protobuf.Struct>
   - Current        -> google.protobuf.Struct
   - Metadata       -> google.protobuf.Struct
3. gRPC call: ExecuteStep(ExecuteStepRequest) -> ExecuteStepResponse
4. Plugin deserializes, runs logic, returns output
5. RemoteStep converts response back to StepResult{Output, Stop}
6. Pipeline engine merges output into Current state
```

All complex data structures are serialized through `google.protobuf.Struct`, which represents JSON-compatible maps. This means plugin data must be expressible as JSON (strings, numbers, booleans, arrays, nested objects). Binary data should be base64-encoded.

## Troubleshooting

### Plugin fails to load: "handshake failed"

The magic cookie values must match exactly. Ensure your plugin uses the SDK's `Serve()` function, which sets the correct handshake configuration:

- Cookie key: `WORKFLOW_PLUGIN`
- Cookie value: `workflow-external-plugin-v1`
- Protocol version: `1`

### Plugin loads but types are not available

Check the plugin's `GetStepTypes()` / `GetModuleTypes()` / `GetTriggerTypes()` responses. The type names returned here must match exactly what you use in your YAML configurations.

### Plugin crashes during execution

Check the engine logs for `[plugin]` entries. The engine captures plugin stdout/stderr. If the plugin process exits unexpectedly, the `RemoteStep` or `RemoteModule` proxy will return a gRPC connection error.

### Data conversion errors

All data passed between host and plugin goes through `protobuf.Struct` (JSON-compatible). Ensure your step output contains only JSON-serializable types. Go-specific types (channels, functions, complex numbers) cannot be transmitted.

### Plugin binary not found

Verify the directory layout matches the expected convention:
- Directory name matches the `name` field in `plugin.json`
- Binary inside has the same name as the directory
- Binary has execute permissions (`chmod +x data/plugins/my-plugin/my-plugin`)
