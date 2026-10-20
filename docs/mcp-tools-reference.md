# MCP Tools Reference

`wfctl mcp` exposes **33 tools and 5 resources** over local stdio. The in-process
library uses the same tool registrations and SDK input validation. An embedder
that attaches an engine with `WithEngine` / `WithInProcessEngine` additionally
exposes `run_workflow` (34 tools). It is absent from the default CLI.

The corrected tool contracts and July protocol support describe this source
build. Released `v0.86.2` predates them; a release download or `go install
...@latest` may differ until a Workflow release includes the changes. Match
claims to the executable's version/source, not only this reference.

Use live `tools/list` for this executable's complete schemas and descriptions;
this table lists every registered argument. `string[]` means an array containing
only strings. Optional fields may be omitted; a supplied value must have its
advertised type. Extra top-level argument fields currently remain permitted,
but undocumented names do not select alternate behavior.

## Built-in tool inventory

| Tool | Required arguments | Optional arguments | Purpose |
|------|--------------------|--------------------|---------|
| `list_module_types` | — | — | Available module metadata. |
| `list_step_types` | — | — | Available pipeline step metadata. |
| `list_trigger_types` | — | — | Available trigger metadata. |
| `list_workflow_types` | — | — | Available workflow-handler metadata. |
| `generate_schema` | — | — | Workflow configuration JSON Schema. |
| `validate_config` | `yaml_content: string` | `strict: boolean`, `skip_unknown_types: boolean` | Basic configuration validation report. |
| `inspect_config` | `yaml_content: string` | — | Modules, dependencies, workflows, triggers and pipeline summary. |
| `list_plugins` | — | `data_dir: string` | Installed plugin manifests; data_dir overrides the startup catalog path. |
| `get_config_skeleton` | `module_types: string[]` | — | Skeleton YAML for requested module types. |
| `run_workflow` | `workflow_type: string`, `action: string` | `data: object` | Conditional engine execution; absent from wfctl mcp. |
| `get_module_schema` | `module_type: string` | — | Module config fields and example. |
| `get_step_schema` | `step_type: string` | — | Step config keys and example. |
| `get_template_functions` | — | — | Template function catalog. |
| `validate_template_expressions` | `yaml_content: string` | — | Static template-reference diagnostics. |
| `infer_pipeline_context` | `yaml_content: string`, `pipeline_name: string` | `after_step: string`, `openapi_spec: string` | Static context inferred before/after a pipeline step. |
| `get_config_examples` | — | `name: string` | List examples, or return one by name. |
| `api_extract` | `yaml_content: string` | `title: string`, `version: string`, `include_schemas: boolean` | Extracted OpenAPI 3.0 specification. |
| `diff_configs` | `old_yaml: string`, `new_yaml: string` | `check_breaking: boolean` | Semantic change and breaking-change report. |
| `manifest_analyze` | `yaml_content: string` | `name: string` | Application deployment manifest inferred from workflow YAML. |
| `contract_generate` | `yaml_content: string` | `baseline_json: string` | API contract snapshot; optional baseline comparison. |
| `compat_check` | `yaml_content: string` | — | Module/step compatibility with this server’s catalog. |
| `template_validate_config` | `yaml_content: string` | `strict: boolean` | Module/step/trigger types, config-key hints and dependency diagnostics. |
| `generate_github_actions` | `yaml_content: string` | `registry: string`, `platforms: string`, `phase_config_yaml: string`, `wfctl_version: string` | Config-derived CI plan and GitHub Actions files, plus legacy CD/release output. |
| `ci_plan` | `yaml_content: string` | `phase_config_yaml: string`, `wfctl_version: string` | Config-derived platform-neutral CIPlan. |
| `detect_project_features` | `yaml_content: string` | — | Application feature summary. |
| `modernize` | `yaml_content: string` | `apply: boolean`, `rules: string`, `exclude_rules: string` | Findings and optional fixed_yaml returned in memory. |
| `registry_search` | — | `query: string`, `type: string`, `tier: string`, `include_private: boolean` | Search a configured local workflow-registry clone. |
| `scaffold_ci` | `description: string` | `yaml_content: string`, `binary_path: string`, `environments: string[]` | Proposed ci: section. |
| `scaffold_environment` | `provider: string` | `environments: string[]`, `secrets_provider: string`, `exposure: string` | Proposed environments: section. |
| `scaffold_infra` | `yaml_content: string`, `provider: string` | `environment: string` | Proposed infra: section. |
| `detect_secrets` | `yaml_content: string` | — | Secret candidates and environment references; not secret removal. |
| `detect_ports` | `yaml_content: string` | — | Application port inventory. |
| `generate_bootstrap` | `yaml_content: string`, `platform: string` | — | CI bootstrap YAML invoking wfctl ci run. |
| `detect_infra_needs` | `yaml_content: string` | `provider: string`, `plugins_dir: string` | Suggested infrastructure requirements; optionally reads plugin manifests. |

## Behavior and defaults

- `validate_config` defaults `strict:false` and `skip_unknown_types:false`.
  Strict mode disallows an empty module list. The basic check permits absent
  entry points and skips workflow/trigger type checks even with strict mode;
  skipping unknown types also skips the module type check. It does not build or
  start an engine. Use `template_validate_config` for deeper checks and compare
  the module, step, trigger and workflow inventories explicitly.
- `template_validate_config` defaults `strict:false`; strict mode treats its
  warnings as failure. It checks module/step/trigger types, config-key hints and
  dependencies; despite its name, it does not analyze template expressions.
  Use `validate_template_expressions` for heuristic static template-expression
  and step-reference checks. Neither tool evaluates templates at runtime.
- `compat_check` checks the running server's catalog. It has no `target_version`
  argument and does not prove that plugin binaries can load or an app can run.
- `modernize` defaults `apply:false`. `apply:true` returns `fixed_yaml` when fixes
  exist; it does not save a file. `rules` and `exclude_rules` are comma-separated
  rule IDs. Plugin-declared rules can be read from the startup plugin directory.
- `infer_pipeline_context` requires `pipeline_name`; omitting `after_step`
  includes outputs from all pipeline steps. Its advertised `openapi_spec` field
  is currently unused by the handler, so supplying it does not enrich the result.
- `get_config_examples` lists examples when `name` is omitted. Named examples
  come from the discovered `example/` catalog; this is not an arbitrary path
  reader. Catalog contents depend on the server's local installation.
- `api_extract` defaults its API `version` to `1.0.0` and `include_schemas:true`;
  `title` comes from the config or `Workflow API`. There is no `format` argument.
- `diff_configs` advertises `check_breaking`, but its current handler does not
  read that field; breaking-change analysis is always included.
  `contract_generate` consumes `baseline_json`, not an OpenAPI document, for
  optional contract comparison.
- `ci_plan` and `generate_github_actions` re-analyze the supplied config through
  cigen. `phase_config_yaml` being nonempty selects a prerequisite phase's
  logical path; its contents are not currently analyzed. `wfctl_version` defaults
  to `latest`. The GitHub renderer returns `plan`, `files`, `ci_yaml` and legacy
  CD/release fields. `registry` (`ghcr.io`) and `platforms`
  (`linux/amd64,linux/arm64`) affect that legacy output. These tools do not consume
  an edited CIPlan; use `wfctl ci generate --from-plan` for that CLI operation.
- `scaffold_ci` requires a description; `binary_path` defaults to `./cmd/server`.
  Its advertised optional `yaml_content` is currently unused by the handler;
  application features are inferred from the description.
  `scaffold_environment` defaults to local/staging/production environments,
  `secrets_provider:env`, and `exposure:port-forward`. `scaffold_infra` defaults
  its environment to production. These return proposed YAML without deploying.
- `generate_bootstrap` accepts `platform:github-actions` or `gitlab-ci`; it
  generates CI bootstrap text rather than an application or installed runner.
- `detect_infra_needs` defaults its provider to `aws`; `plugins_dir` selects a
  directory whose plugin manifests can contribute requirements.
- `registry_search` reads the startup `-registry-dir` clone and defaults
  `include_private:false`. `query`, `type` and `tier` are optional local filters;
  no configured clone produces a tool error. It does not query a remote service.

## Result and error contract

Tools return MCP text content: either YAML/text or a JSON report encoded in the
text field. Required-field/type violations and handler failures have
`isError:true`. A parsed analysis reporting `valid:false` or `compatible:false`
remains a successful tool invocation: inspect its report, not only `isError`.
Malformed JSON produces a JSON-RPC parse error. The in-process adapter returns a
nonnil Go error for protocol/tool failures and preserves successful text results,
including analytical negatives. Audit logging records tool/status metadata only.

The engine-only tool reports `success:false` with `isError:true` when its provider
fails; successful execution reports `success:true`. It can have arbitrary
external or destructive effects, so an embedder must supply authorization.

## Protocol envelopes and cache hints

With mcp-go v1.3.0, the CLI supports legacy initialization for `2024-11-05`,
`2025-03-26`, `2025-06-18` and `2025-11-25`, plus the stateless `2026-07-28`
model. July uses `server/discover` without initialization; every request supplies
`io.modelcontextprotocol/protocolVersion` and
`io.modelcontextprotocol/clientCapabilities` in `params._meta`. Capabilities are
per request, not inherited from an earlier call. See the
[tested request example](mcp.md#catalog-refresh-and-protocol-coverage).

Ordinary July results add `resultType:"complete"` and Workflow identity metadata.
This completion label does not change the tool's `isError` or analytical report
semantics. `server/discover`, `tools/list`, `resources/list`,
`resources/templates/list` and `resources/read` results advertise `ttlMs:0` and
`cacheScope:"private"`; resource templates are currently an empty inventory.
The cache fields are required protocol metadata, while their zero/private values
are this server's local-data policy. They do not sanitize returned content.
Legacy responses retain their earlier shape. Text content remains supported;
this change does not add `structuredContent` or output schemas.

## Metadata and input boundaries

All 33 default tools explicitly advertise `readOnlyHint:true` and
`destructiveHint:false`: analysis and generation do not persist customer changes.
`modernize apply:true` is included. CI analysis uses temporary implementation
files that are removed after use. `openWorldHint:true` applies to `list_plugins`,
`detect_infra_needs`, `ci_plan` and `generate_github_actions`, which can inspect
caller-selected local directories or file imports. Other tools use bounded local
catalogs or in-memory computation and advertise `openWorldHint:false`.
`run_workflow` advertises false/true/true for readOnly/destructive/openWorld.
These hints are descriptive metadata, not an OS filesystem sandbox.

Supply sanitized YAML and public/synthetic values. Results may repeat submitted
configuration, paths, manifests and detected secret candidates; no general
redaction guarantee exists. Basic string-loaded config analysis does not resolve
imports. CI analysis does use file/import context and can read local references.
Manifest discovery proves metadata availability, not executable/plugin or runtime
acceptance. See [server setup and limits](mcp.md).

Further input/output resource budgets, filesystem confinement, rate limiting and
general redaction remain deferred. The tests cover specific schema, canary,
cancellation and cache boundaries; they do not certify generic security
compliance. OpenAI's directory annotation policy is separate from protocol
compatibility. Neither implies directory eligibility, host activation or support
for optional hosted transport, authentication, tasks or UI features; this change
also leaves Compute integration unchanged.

## Separate application integrations

The integrations below do not add tools to the default `wfctl mcp` inventory.
LSP diagnostics/completion/hover are not registered by this stdio server.

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

## In-process and stdio use

`NewInProcessServer` provides the Go `MCPProvider` interface with sorted inventory
methods and the same validated calls. `wfctl mcp` launches a separate local
process over stdio; it does not start an HTTP server. Neither mode introduces
an authentication or filesystem-isolation boundary by itself.

```go
provider := mcp.NewInProcessServer(
    mcp.WithInProcessPluginDir("/path/to/public/plugins"),
    mcp.WithInProcessAuditLog(logger),
)
result, err := provider.CallTool(ctx, "validate_config", map[string]any{
    "yaml_content": proposedYAML,
})
```

For CLI launch options and generic client configuration, see [MCP server](mcp.md).
