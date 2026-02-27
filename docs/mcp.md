# MCP Server for Workflow Engine

The workflow engine includes a built-in [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server that exposes engine functionality to AI assistants and tools.

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

## Building

```bash
# Build the MCP server binary
make build-mcp

# Or directly with Go
go build -o workflow-mcp-server ./cmd/workflow-mcp-server

# Or install globally with Go
go install github.com/GoCodeAlone/workflow/cmd/workflow-mcp-server@latest
```

## Installation

### Claude Desktop

Add the following to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

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
      "command": "/path/to/workflow-mcp-server",
      "args": ["-plugin-dir", "/path/to/data/plugins"]
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
      "command": "/path/to/workflow-mcp-server",
      "args": ["-plugin-dir", "/path/to/data/plugins"]
    }
  }
}
```

### Generic MCP Client

The server communicates over **stdio** using JSON-RPC 2.0. Any MCP-compatible client can connect:

```bash
./workflow-mcp-server -plugin-dir ./data/plugins
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
Usage: workflow-mcp-server [options]

Options:
  -plugin-dir string   Plugin data directory (default "data/plugins")
  -version             Show version and exit
```

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
```

## Architecture

```
cmd/workflow-mcp-server/main.go  → Entry point (stdio transport)
mcp/server.go        → MCP server setup, tool handlers, resource handlers
mcp/docs.go          → Embedded documentation content
mcp/server_test.go   → Unit tests
```

The server uses the [mcp-go](https://github.com/mark3labs/mcp-go) library for MCP protocol implementation over stdio transport.
