# MCP Tools Reference

Complete reference for all MCP tools available through `wfctl mcp` and the
in-process MCP library. Tools are organized by namespace.

## wfctl Tools

### Config Validation

#### `validate_config`

Validate a workflow YAML configuration string.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `yaml_content` | string | yes | YAML config content to validate |
| `skip_unknown_types` | bool | no | Skip unknown module/step type errors |
| `strict` | bool | no | Fail on any warning |

**Example:**
```json
{
  "tool": "validate_config",
  "arguments": {
    "yaml_content": "modules:\n  - name: server\n    type: http.server\n    config:\n      address: ':8080'\n"
  }
}
```

**Returns:** Validation result with errors and warnings.

---

#### `template_validate_config`

Validate a workflow config that uses template expressions.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config with template expressions |
| `context` | object | no | Template context variables for expression evaluation |

---

#### `inspect_config`

Inspect a config and get a structured summary.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `yaml_content` | string | yes | YAML config content |

**Returns:** Structured summary with module names/types, workflow definitions, pipeline triggers, step counts.

---

#### `diff_configs`

Compute a semantic diff between two workflow configs.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `old_yaml` | string | yes | Base config YAML |
| `new_yaml` | string | yes | Proposed config YAML |

**Returns:** Structured diff showing added/modified/removed modules, pipelines, and steps.

---

#### `modernize`

Detect and fix known YAML config anti-patterns.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config to modernize |
| `dry_run` | bool | no | Report fixes without applying them |

**Returns:** Modernized config with fix descriptions.

---

#### `compat_check`

Check config compatibility with a specific engine version.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config to check |
| `target_version` | string | yes | Engine version to check against (e.g., `v0.5.0`) |

---

### Type Discovery

#### `list_module_types`

List all available module types.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `category` | string | no | Filter by category (e.g., `database`, `http`, `agent`) |
| `plugin` | string | no | Filter by plugin name |

---

#### `list_step_types`

List all pipeline step types.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `category` | string | no | Filter by category |

---

#### `list_trigger_types`

List all trigger types.

---

#### `list_workflow_types`

List all workflow handler types.

---

#### `list_plugins`

List installed external plugins.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `include_uninstalled` | bool | no | Include plugins from the registry that aren't installed |

---

### Schema & Documentation

#### `get_module_schema`

Get the JSON Schema for a specific module type.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | yes | Module type (e.g., `http.server`, `storage.sqlite`) |

**Example:**
```json
{"tool": "get_module_schema", "arguments": {"type": "agent.guardrails"}}
```

---

#### `get_step_schema`

Get the JSON Schema for a specific step type.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | yes | Step type (e.g., `step.agent_execute`, `step.db_query`) |

---

#### `get_config_skeleton`

Generate a skeleton YAML config for given module types.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `module_types` | array | yes | List of module types to include |
| `include_optional` | bool | no | Include optional config fields |

---

#### `get_config_examples`

Get example configs for a module or step type.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | yes | Module or step type |

---

#### `get_module_schema` / `get_step_schema` / `get_template_functions`

Get the template functions available in workflow expressions.

---

### Code Generation & Scaffolding

#### `generate_schema`

Generate a JSON Schema for workflow config files.

---

#### `scaffold_ci`

Generate CI/CD pipeline config for a workflow application.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `provider` | string | yes | CI provider (`github-actions`, `gitlab-ci`, `circleci`) |
| `config` | string | no | Existing workflow config to analyze |

---

#### `scaffold_environment`

Generate environment configuration (Docker Compose, kubernetes manifests).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `target` | string | yes | Target environment (`docker-compose`, `kubernetes`, `minikube`) |
| `config` | string | no | Existing workflow config to analyze |

---

#### `scaffold_infra`

Generate infrastructure-as-code for a workflow application.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `provider` | string | yes | IaC provider (`opentofu`, `terraform`, `pulumi`) |
| `config` | string | no | Existing workflow config |

---

#### `generate_bootstrap`

Generate a complete bootstrap config for a new workflow application.

---

#### `contract_generate`

Generate API contracts (OpenAPI spec) from a workflow config.

---

### Introspection & Analysis

#### `api_extract`

Extract API endpoints from a workflow config.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config |
| `format` | string | no | Output format (`openapi`, `markdown`, `table`) |

---

#### `detect_project_features`

Detect features and capabilities used in a project.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config |

---

#### `detect_infra_needs`

Analyze a config and detect infrastructure requirements.

---

#### `detect_ports`

Detect all ports used by a workflow config.

---

#### `detect_secrets`

Detect secret references in a workflow config.

---

#### `infer_pipeline_context`

Infer the runtime context available at each step in a pipeline.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `config` | string | yes | YAML config |
| `pipeline` | string | yes | Pipeline name to analyze |

---

#### `manifest_analyze`

Analyze a plugin manifest for compatibility.

---

#### `registry_search`

Search the plugin registry for available plugins.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | yes | Search query |
| `category` | string | no | Filter by category |

---

#### `validate_template_expressions`

Validate template expressions in a workflow config.

---

## LSP Tools

The Language Server Protocol (LSP) tools provide IDE-like diagnostics for
workflow YAML files.

#### `mcp:lsp:diagnose`

Run diagnostics on a YAML workflow config.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `content` | string | yes | YAML content to diagnose |
| `uri` | string | no | File URI for context |

**Returns:** List of diagnostics with severity (error, warning, hint), line/column, and message.

---

#### `mcp:lsp:complete`

Get completion suggestions at a position in a YAML file.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `content` | string | yes | YAML content |
| `line` | int | yes | 0-indexed line number |
| `character` | int | yes | 0-indexed character position |

---

#### `mcp:lsp:hover`

Get hover documentation at a position in a YAML file.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `content` | string | yes | YAML content |
| `line` | int | yes | 0-indexed line number |
| `character` | int | yes | 0-indexed character position |

---

## Workflow-Defined MCP Tools

> **Requires workflow-plugin-agent v0.8.0+.** The `mcp_tool` trigger, `mcp.registry`
> module, and all `step.agent_execute` / `step.self_improve_*` / `step.blackboard_*`
> types are provided by this plugin, not the workflow core engine.
> Install with: `wfctl plugin install workflow-plugin-agent`

Workflow applications can expose their own pipelines as MCP tools using the
`mcp_tool` trigger type and `mcp` workflow handler.

### Creating a Workflow-Defined Tool

```yaml
# Define the tool as a pipeline with an mcp_tool trigger
pipelines:
  task_analytics:
    trigger:
      type: mcp_tool
      config:
        tool_name: task_analytics
        description: "Analyze task completion rates and identify bottlenecks"
        parameters:
          - name: date_range
            type: string
            description: "Date range in ISO format: YYYY-MM-DD/YYYY-MM-DD"
            required: true
          - name: group_by
            type: string
            description: "Group results by: status, assignee, or priority"
            required: false
    steps:
      - name: query
        type: step.db_query
        config:
          database: db
          mode: list
          query: |
            SELECT {{ .group_by }}, COUNT(*) as count,
                   AVG(JULIANDAY(completed_at) - JULIANDAY(created_at)) as avg_days
            FROM tasks
            WHERE created_at BETWEEN ? AND ?
            GROUP BY {{ .group_by }}
          params:
            - "{{ .date_start }}"
            - "{{ .date_end }}"
      - name: respond
        type: step.json_response
        config:
          status: 200
          body_from: "steps.query.rows"
```

### MCP Server Registry

Use `mcp.registry` to track and audit registered MCP tool registrations
(provided by workflow-plugin-agent v0.8.0+):

```yaml
modules:
  - name: mcp-registry
    type: mcp.registry
    config:
      audit_tool_calls: true
```

## In-Process vs External MCP Server

| Feature | In-Process | External (stdio/HTTP) |
|---------|------------|----------------------|
| Latency | <1ms | 5-50ms |
| Setup | Zero (built-in) | Requires subprocess or HTTP server |
| Tool access | All wfctl tools | All wfctl tools |
| Security | Agent process boundary | Process isolation |
| Multi-agent | Shared instance | Per-agent instances |
| Use case | Self-improvement loops | IDE integration, Claude Desktop |

### In-Process Usage (workflow-plugin-agent)

```go
// In-process MCP server — no HTTP overhead
mcp := mcp.NewInProcessServer(
    mcp.WithInProcessPluginDir("/data/plugins"),
    mcp.WithInProcessAuditLog(logger),
)

result, err := mcp.CallTool(ctx, "validate_config", map[string]any{
    "config": proposedYAML,
})
```

### External Usage (wfctl mcp)

```bash
# Start MCP server for Claude Desktop or other IDE
wfctl mcp
```

Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "workflow": {
      "command": "wfctl",
      "args": ["mcp"]
    }
  }
}
```
