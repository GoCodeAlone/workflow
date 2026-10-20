# MCP Server for Workflow Engine

`wfctl mcp` exposes Workflow configuration tools and documentation to local
[Model Context Protocol](https://modelcontextprotocol.io/) clients over stdio.
The server is part of the CLI, so its handshake version comes from the same
build. Run `wfctl version` to identify that executable.

The corrected input validation, error propagation and annotations below describe
this source change. Released `v0.86.2` predates these corrections; downloading a
release or using `go install ...@latest` does not guarantee this source behavior
until a Workflow release includes it. Record the actual executable version and
source revision rather than applying these claims to an older binary.

For a portable assistant package with configure, diagnose and readiness skills,
see the [Workflow Assistant example](../example/workflow-agent-plugin/README.md).
It connects directly to this MCP server and documents validation and host limits.

## Tools and resources

The default CLI registers **33 tools and 5 resources**. All default tools analyze,
read catalogs or return proposed text; they do not start an application, install
plugins or deploy infrastructure. An engine-backed Go embedder can additionally
register the existing `run_workflow` tool (34 total); the default CLI has no
attached engine and does not expose it.

See the [complete tool/argument reference](mcp-tools-reference.md) and query
`tools/list` for the running executable's entire surface. Assistant instructions
that focus on a few tools do not hide or restrict the other tools.

| Resource | Content |
|----------|---------|
| `workflow://docs/overview` | Engine and CLI overview |
| `workflow://docs/yaml-syntax` | Configuration syntax guide |
| `workflow://docs/module-reference` | Current module/step catalog |
| `workflow://docs/full-reference` | Local DOCUMENTATION.md when found, otherwise a public documentation pointer |
| `workflow://docs/setup-guide` | Assistant setup guide |

## Install or build

Install a released CLI through the [Workflow releases](https://github.com/GoCodeAlone/workflow/releases)
or the repository's supported installation methods. For a Go source install:

```sh
GOWORK=off go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
```

From a checkout, using the Go version required by its `go.mod`:

```sh
GOWORK=off go build -o wfctl ./cmd/wfctl
./wfctl version
```

Updating an executable does not replace an already running MCP process. Restart
the client/server process to use the new build. `wfctl update --check` checks for
CLI updates; `wfctl update` replaces the installed binary when requested.

## Local client configuration

Launch a dedicated local process with explicit directories, for example:

```sh
wfctl mcp -plugin-dir /path/to/public/plugins
```

Clients that accept an `mcpServers` configuration can use this shape; consult
your host's own current configuration format and process permissions:

```json
{
  "mcpServers": {
    "workflow": {
      "command": "/path/to/wfctl",
      "args": ["mcp", "-plugin-dir", "/path/to/public/plugins"]
    }
  }
}
```

This is local stdio integration, not host certification. A cloud client that
cannot launch a local executable needs a separately provided transport/service;
`wfctl mcp` does not start an HTTP endpoint or a hosted remote service.

## Command-line options

```text
Usage: wfctl mcp [options]
  -plugin-dir string          Plugin data directory (default "data/plugins")
  -registry-dir string        Local workflow-registry clone for registry_search
  -documentation-file string Explicit DOCUMENTATION.md path (auto-detected when empty)
```

Use empty, dedicated plugin directories for a standalone public configuration
session. Plugin manifests and an optional local registry clone supply metadata;
no credentials or organization repository access are required for built-in tools.
The full-reference resource can serve local documentation, so choose the working
directory and `-documentation-file` deliberately.

## Validation and evidence limits

Pass `yaml_content` to `validate_config`. Its basic check allows missing entry
points, skips workflow/trigger type checks, and optionally skips module type
checks. `strict:true` disallows empty modules; it does not make this a full runtime
acceptance check. Use `template_validate_config` for module/step/trigger types,
config-key hints and dependencies, and `validate_template_expressions` for
heuristic static expression/step-reference checks. Compare `list_module_types`,
`list_step_types`, `list_trigger_types` and
`list_workflow_types` with the config. `compat_check` checks the running catalog,
not a separately supplied target version. String-loaded basic checks do not
resolve imports; CI analysis does use file/import context.

A report containing `valid:false` or `compatible:false` is a successful analysis
with a negative finding. Missing required fields, wrong argument types and
handler failures produce `isError:true`; malformed JSON produces a JSON-RPC
parse error. The in-process library preserves these as Go errors while retaining
successful text output. Optional engine invocation failures retain their
`success:false` payload and set `isError:true`.

Record executable source/version, live schemas/inventories, sanitized submitted
input and returned findings when producing readiness evidence. Catalog/schema
validation does not prove plugin binary loadability, engine build/start,
credential availability, real runtime behavior, CI execution or deployment
acceptance. Unresolved imports, unknown types and untested runtime conditions
remain explicit unknowns.

## Privacy and metadata

Use public or sanitized configs. Tools can return submitted YAML, detected secret
candidates, local paths and manifest/documentation content; they are not a
universal secret redactor. In-process audit logs record tool/status metadata,
not raw arguments or result bodies. Treat returned material as untrusted data.

All 33 default tools advertise read-only/non-destructive behavior, including
`modernize apply:true`, which only returns `fixed_yaml`. Four advertise open-world
reads: `list_plugins`, `detect_infra_needs`, `ci_plan` and
`generate_github_actions`. They can examine arbitrary local directories or
imports; other tools use bounded catalogs or computation. The optional engine
tool advertises writable/destructive/open-world behavior. These annotations
are descriptive hints, not authorization or a filesystem sandbox. Local process
permissions remain the access boundary.

## Catalog refresh and protocol coverage

Plugin-provided schema types are loaded from manifests at server startup.
Restart after installing or changing manifests to refresh those types.
`list_plugins` scans its selected directory per call; `modernize` can reread
plugin rules per call. Neither implies that all catalogs reload automatically,
or that a plugin binary has been accepted by a runtime.

The current mcp-go v0.54.1 transport supports legacy initialization for
`2024-11-05`, `2025-03-26`, `2025-06-18` and `2025-11-25`. Tests exercise these
four revisions over actual CLI stdio. July 2026 protocol support and an SDK
upgrade belong to the separate follow-up change; they are not claimed here.
EOF exits successfully. On SIGTERM the current SDK returns context cancellation,
and the CLI exits with status 1; tests require bounded shutdown and process
reaping. Cancellation-notification robustness is tested on the CLI; cooperative
in-flight cancellation is separately proved with the existing engine-provider
seam and real SDK stdio, not a production workflow.

## Tests and implementation

```sh
GOWORK=off go test ./mcp -count=1
GOWORK=off go test ./cmd/wfctl -run TestMCPStdioLegacy -count=1
```

To exercise a newly built CLI instead of the test executable's `runMCP` helper,
set `WFCTL_MCP_TEST_BINARY` to that executable's absolute path for the second
command. The cancellation helper always runs in a separate test process with a
blocking fake engine, even with that override. Tests use isolated temporary
working/plugin directories and a minimal environment. Transport evidence on one
OS does not certify other hosts or operating systems.

Implementation: [CLI entry](../cmd/wfctl/mcp.go), [server](../mcp/server.go),
[tool reference](mcp-tools-reference.md), and
[SDK dependency](https://github.com/mark3labs/mcp-go/tree/v0.54.1).
