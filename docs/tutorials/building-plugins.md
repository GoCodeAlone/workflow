# Building Plugins

## Overview

Plugins are dynamic components packaged with a manifest for the plugin registry. They run in a sandboxed Yaegi interpreter with stdlib-only imports.

## Scaffold a Plugin

```bash
./wfctl plugin init my-plugin --author "Your Name" --contract
```

This creates:

```
my-plugin/
  plugin.json      # Manifest
  component.go     # Component source
```

## Plugin Manifest

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "author": "Your Name",
  "description": "What this plugin does",
  "license": "MIT",
  "tags": ["utility"],
  "contract": {
    "requiredInputs": {
      "input1": {"type": "string", "description": "Primary input"}
    },
    "optionalInputs": {
      "verbose": {"type": "bool", "description": "Enable verbose output", "default": false}
    },
    "outputs": {
      "result": {"type": "string", "description": "Processing result"}
    }
  }
}
```

## Component Source

```go
//go:build ignore

package component

import (
    "context"
    "fmt"
)

type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "my-plugin" }
func (p *MyPlugin) Init(config map[string]interface{}) error { return nil }
func (p *MyPlugin) Start(ctx context.Context) error { return nil }
func (p *MyPlugin) Stop(ctx context.Context) error { return nil }

func (p *MyPlugin) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
    input1, _ := params["input1"].(string)
    return map[string]interface{}{
        "result": fmt.Sprintf("Processed: %s", input1),
    }, nil
}

func (p *MyPlugin) Contract() map[string]interface{} {
    return map[string]interface{}{
        "requiredInputs": map[string]interface{}{
            "input1": map[string]interface{}{"type": "string", "description": "Primary input"},
        },
        "outputs": map[string]interface{}{
            "result": map[string]interface{}{"type": "string", "description": "Processing result"},
        },
    }
}
```

## Generate Documentation

```bash
./wfctl plugin docs my-plugin/
```

## Register with the Engine

```bash
curl -X POST http://localhost:8081/api/plugins \
  -H "Content-Type: application/json" \
  -d @my-plugin/plugin.json
```

## Hot-Reload During Development

Place your plugin directory in a watched location. The engine auto-reloads on file changes when running with dev mode enabled.

## Submission Checklist

Before contributing a plugin, ensure it passes the community validator:
1. Valid manifest with name, version, author, description
2. Semver version format
3. Component compiles in Yaegi sandbox
4. Contract declared with typed fields
5. License specified

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for the full plugin PR process.
