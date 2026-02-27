# MCP Server for Workflow Engine

The workflow engine includes a built-in [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server that exposes engine functionality to AI assistants and tools.

> **Note**: The MCP server is now integrated into the `wfctl` CLI as the `mcp` subcommand. The standalone `workflow-mcp-server` binary is still available for backward compatibility but the recommended approach is to use `wfctl mcp`. This ensures the MCP server version always matches the CLI and engine version, and benefits from automatic updates via `wfctl update`.

## Features

The MCP server provides:

### Tools

| Tool | Description |
|------|-------------|
| `list_module_types` | List all available module types for workflow YAML configs |
| `list_step_types` | List all pipeline step types |
| `list_trigger_types` | List all trigger types (http, schedule, event, eventbus) |
| `list_workflow_types` | List all workflow handler types |
| `generate_schema` | Generate JSON Schema for workflow config files |
| `validate_config` | Validate a workflow YAML configuration string |
| `inspect_config` | Inspect a config and get structured summary of modules, workflows, triggers |
| `list_plugins` | List installed external plugins |
| `get_config_skeleton` | Generate a skeleton YAML config for given module types |

### Resources

| Resource | Description |
|----------|-------------|
| `workflow://docs/overview` | Engine overview documentation |
| `workflow://docs/yaml-syntax` | YAML configuration syntax guide |
| `workflow://docs/module-reference` | Dynamic module type reference |

## Installation

Install `wfctl` (the CLI includes the MCP server):

```bash
# Install via Go
go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest

# Or download a pre-built binary from GitHub releases
# https://github.com/GoCodeAlone/workflow/releases/latest

# Keep wfctl up to date (and thus the MCP server too)
wfctl update
```

### Building from Source

```bash
# Build wfctl (includes the mcp command)
go build -o wfctl ./cmd/wfctl

# Build the standalone MCP server binary (legacy)
make build-mcp
# Or: go build -o workflow-mcp-server ./cmd/workflow-mcp-server
```

## Configuring AI Clients

### Claude Desktop

Add the following to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "workflow": {
      "command": "wfctl",
      "args": ["mcp", "-plugin-dir", "/path/to/data/plugins"]
    }
  }
}
```

**Legacy (standalone binary)**:
```json
{
  "mcpServers": {
    "workflow": {
      "command": "/path/to/workflow-mcp-server",
      "args": ["-plugin-dir", "/path/to/data/plugins"]
    }
  }
}
```

### VS Code with GitHub Copilot

Add to your VS Code `settings.json`:

```json
{
  "github.copilot.chat.mcpServers": {
    "workflow": {
      "command": "wfctl",
      "args": ["mcp", "-plugin-dir", "/path/to/data/plugins"]
    }
  }
}
```

### Cursor

Add to your Cursor MCP configuration (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "workflow": {
      "command": "wfctl",
      "args": ["mcp", "-plugin-dir", "/path/to/data/plugins"]
    }
  }
}
```

### Generic MCP Client

The server communicates over **stdio** using JSON-RPC 2.0. Any MCP-compatible client can connect:

```bash
wfctl mcp -plugin-dir ./data/plugins
```

## Usage Examples

Once connected, the AI assistant can use the tools to:

### List available module types
```
Use the list_module_types tool to see what modules are available.
```

### Validate a configuration
```
Validate this workflow config:
modules:
  - name: webServer
    type: http.server
    config:
      address: ":8080"
```

### Generate a config skeleton
```
Generate a skeleton config with http.server, http.router, and http.handler modules.
```

### Inspect a configuration
```
Inspect this config and show me the dependency graph...
```

## Command Line Options

```
Usage: wfctl mcp [options]

Options:
  -plugin-dir string   Plugin data directory (default "data/plugins")
```

## Keeping the MCP Server Up to Date

Because the MCP server is now part of `wfctl`, you can use the built-in update command to keep everything in sync:

```bash
# Check for updates
wfctl update --check

# Install the latest version (replaces the wfctl binary in-place)
wfctl update
```

Set `WFCTL_NO_UPDATE_CHECK=1` to suppress automatic update notices.

## Dynamic Updates

The MCP server dynamically reflects the current state of the engine:

- **Module types** are read from the schema registry, which includes both built-in types and any dynamically registered plugin types
- **Plugin list** scans the configured plugin directory at query time
- **Schema generation** uses the current module schema registry
- **Validation** uses the same validation logic as `wfctl validate`

This means the MCP server automatically picks up new module types and plugins without requiring a restart.

## Running Tests

```bash
go test -v ./mcp/
go test -v -run TestRunMCP ./cmd/wfctl/
```

## Architecture

```
cmd/wfctl/main.go    → CLI entry point; registers "mcp" command
cmd/wfctl/mcp.go     → "wfctl mcp" command handler (delegates to mcp package)
mcp/server.go        → MCP server setup, tool handlers, resource handlers
mcp/docs.go          → Embedded documentation content
mcp/server_test.go   → Unit tests

cmd/workflow-mcp-server/main.go  → Standalone binary entry point (legacy)
```

The server uses the [mcp-go](https://github.com/mark3labs/mcp-go) library for MCP protocol implementation over stdio transport.

