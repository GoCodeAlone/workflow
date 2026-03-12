# Documentation Generation Example

This example demonstrates how to use `wfctl docs generate` to produce Markdown
documentation with Mermaid diagrams from a workflow configuration file.

## Configuration

The `workflow.yaml` file describes a **Secure Order Processing API** that uses:

- HTTP server with routing and middleware
- JWT authentication and authorization (via `workflow-plugin-authz`)
- Messaging / event publishing
- A state machine for order lifecycle management
- Pipelines with validation, HTTP calls, and compensation steps
- Storage (SQLite)
- Observability (metrics, health checks)
- Sidecars (Redis cache, Jaeger tracing)

## Generating Documentation

```bash
# From this directory:
wfctl docs generate \
  -output ./docs/ \
  -plugin-dir ./plugins/ \
  workflow.yaml
```

This creates the following files in `./docs/`:

| File | Description |
|------|-------------|
| `README.md` | Application overview with stats and index |
| `modules.md` | Module inventory, types, and dependency graph |
| `pipelines.md` | Pipeline definitions with workflow diagrams |
| `workflows.md` | HTTP routes, messaging, and state machine diagrams |
| `plugins.md` | External plugin details and capabilities |
| `architecture.md` | System architecture diagram |

## External Plugins

The `plugins/` directory contains a `plugin.json` manifest for
[GoCodeAlone/workflow-plugin-authz](https://github.com/GoCodeAlone/workflow-plugin-authz),
which provides authorization enforcement capabilities:

- **Module types:** `authz.enforcer`, `authz.policy`
- **Step types:** `step.authz_check`, `step.authz_grant`
- **Capabilities:** `authorization` (provider)

The documentation generator reads these manifests to produce a dedicated
plugins page describing each plugin's version, dependencies, module types,
step types, and capabilities.

## Viewing on GitHub

All generated `.md` files use standard Markdown. Mermaid diagrams are embedded
in fenced code blocks with the `mermaid` language tag:

````markdown
```mermaid
graph LR
    A --> B
```
````

GitHub automatically renders these as SVG diagrams when viewed in the browser.
