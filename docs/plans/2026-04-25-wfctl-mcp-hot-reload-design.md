---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
external_refs:
  - "wfctl-mcp-hot-reload"
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./mcp ./cmd/wfctl
  result: unknown
supersedes: []
superseded_by: []
---

# wfctl MCP Hot Reload Design

Date: 2026-04-25

## Goal

Let agents improve `wfctl mcp` without restarting the whole MCP client session. A rebuilt `wfctl` binary should be reloadable through an MCP tool, using the supervisor pattern already proven in `workflow-dnd` and approximated in `workflow-cardgame`.

## Reference Pattern

`workflow-dnd` documents the working pattern:

- `.mcp.json` points to `bin/dnd-mcp-supervisor.sh`, not the direct MCP binary.
- the MCP server exposes `reload_mcp`.
- the reload tool exits the MCP child process.
- the supervisor restarts the child with the newly built binary.
- connection handles are invalidated and agents reconnect.
- Claude Code reconnected successfully in testing.

`workflow-cardgame` registers a `reload_mcp` tool and exits with a reload-specific code. That is a cleaner signal than a generic crash.

Known limitation: some MCP clients cache tool lists at session start. Reloading can update behavior of existing tools, but brand-new tool names may still require a new client session.

## Design

Add a reloadable process boundary around `wfctl mcp`.

Recommended command shape:

```sh
wfctl mcp-supervisor --wfctl /path/to/wfctl -- mcp -plugin-dir data/plugins
```

The supervisor should:

- own stdio for the MCP client
- start the child `wfctl mcp` process
- proxy stdin/stdout without rewriting JSON-RPC payloads
- restart the child when it exits with a documented reload code
- exit when the child exits normally or with a non-reload error
- log supervisor diagnostics to stderr only

The MCP server should:

- register `reload_mcp` by default
- flush a text result explaining that reconnect is required
- exit with the reload code after a short delay so the response can be sent
- document that connection handles and in-memory state are invalidated

## Alternatives

In-process dynamic tool registration is not the first step. MCP client caching makes this unreliable for brand-new tools, and it adds complexity inside `mcp.Server`. The supervisor model is simpler and already works for the game projects.

## Testing

Unit tests:

```sh
GOWORK=off go test ./mcp ./cmd/wfctl -run 'TestReloadMCP|TestMCPSupervisor'
```

Runtime smoke:

```sh
go build -o /tmp/wfctl ./cmd/wfctl
/tmp/wfctl mcp-supervisor --wfctl /tmp/wfctl -- mcp
```

Then call `reload_mcp` through a JSON-RPC MCP request and assert the supervisor restarts the child.

## Acceptance Criteria

- `wfctl mcp` exposes `reload_mcp`.
- `wfctl mcp-supervisor` restarts the child on the reload exit code.
- supervisor logs never corrupt MCP stdout.
- docs explain the rebuild plus reload workflow.
- limitations around cached tool lists are explicit.
