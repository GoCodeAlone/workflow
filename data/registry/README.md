# workflow-registry

[![Validate Registry](https://github.com/GoCodeAlone/workflow-registry/actions/workflows/validate.yml/badge.svg)](https://github.com/GoCodeAlone/workflow-registry/actions/workflows/validate.yml)

The official plugin and template registry for the [GoCodeAlone/workflow](https://github.com/GoCodeAlone/workflow) engine.

This registry catalogs all built-in plugins, community extensions, and reusable templates that can be used with the workflow engine. It serves as the source of truth for the `wfctl` CLI's marketplace and `wfctl publish` command.

## Table of Contents

- [What is this?](#what-is-this)
- [Browsing Plugins](#browsing-plugins)
- [Plugin Tiers](#plugin-tiers)
- [Built-in Plugins](#built-in-plugins)
- [Templates](#templates)
- [Schema](#schema)
- [Submitting a Community Plugin](#submitting-a-community-plugin)
- [Plugin Manifest Format](#plugin-manifest-format)

---

## What is this?

The workflow engine is built around a plugin system. Every capability — HTTP servers, messaging brokers, state machines, AI steps, CI/CD pipelines — is provided by a plugin. This registry tracks:

- **Plugins**: Packages that extend the engine with new module types, pipeline steps, triggers, and workflow handlers
- **Templates**: Starter configurations for common workflow patterns

The registry is consumed by:
- `wfctl marketplace` — browse and search available plugins
- `wfctl publish` — submit your plugin to the registry
- The workflow UI Marketplace page

---

## Browsing Plugins

Plugins are organized under `plugins/<name>/manifest.json`. Each manifest describes the plugin's capabilities, version, tier, and source location.

To search via CLI:

```bash
wfctl marketplace search http
wfctl marketplace info http
```

To browse manually, see the [`plugins/`](./plugins/) directory.

---

## Plugin Tiers

| Tier | Description |
|------|-------------|
| **core** | Maintained by GoCodeAlone, shipped with the engine, guaranteed compatibility |
| **community** | Third-party plugins submitted via PR, reviewed by maintainers |
| **premium** | Commercial plugins with additional licensing requirements |

All plugins in this registry must pass manifest schema validation before merging.

---

## Built-in Plugins

These plugins ship with the workflow engine and are always available:

| Plugin | Description | Tier |
|--------|-------------|------|
| [http](./plugins/http/manifest.json) | HTTP server, router, middleware, proxy, static files | core |
| [messaging](./plugins/messaging/manifest.json) | In-memory broker, EventBus, NATS, Kafka, Slack, webhooks | core |
| [statemachine](./plugins/statemachine/manifest.json) | State machine engine, tracker, connector | core |
| [scheduler](./plugins/scheduler/manifest.json) | Cron-based job scheduling | core |
| [observability](./plugins/observability/manifest.json) | Metrics, health checks, tracing, OpenAPI | core |
| [storage](./plugins/storage/manifest.json) | S3, GCS, local, SQLite, SQL databases | core |
| [pipelinesteps](./plugins/pipelinesteps/manifest.json) | Generic pipeline steps (validate, transform, jq, db, etc.) | core |
| [auth](./plugins/auth/manifest.json) | JWT auth, user store, auth middleware wiring | core |
| [api](./plugins/api/manifest.json) | REST handlers, CQRS, API gateway, data transformer | core |
| [featureflags](./plugins/featureflags/manifest.json) | Feature flag service and pipeline steps | core |
| [platform](./plugins/platform/manifest.json) | Infrastructure-as-code provider, resource, context modules | core |
| [modularcompat](./plugins/modularcompat/manifest.json) | CrisisTextLine/modular framework compatibility (scheduler, cache) | core |
| [secrets](./plugins/secrets/manifest.json) | HashiCorp Vault and AWS Secrets Manager | core |
| [cicd](./plugins/cicd/manifest.json) | CI/CD steps: shell, Docker, scan, deploy, gate | core |
| [integration](./plugins/integration/manifest.json) | Integration workflow handler for multi-system connectors | core |
| [ai](./plugins/ai/manifest.json) | AI steps (complete, classify, extract), dynamic components, sub-workflows | core |

## External Plugins

These plugins run as separate subprocesses via the [go-plugin](https://github.com/GoCodeAlone/go-plugin) IPC framework:

| Plugin | Description | Tier |
|--------|-------------|------|
| [bento](./plugins/bento/manifest.json) | Stream processing via Bento — 100+ connectors, Bloblang transforms, at-least-once delivery | core |
| [ratchet](./plugins/ratchet/manifest.json) | Autonomous AI agent orchestration — agent coordination, task management, MCP integration, secrets/vault, SSE, and intelligent pipeline execution (13 step types, 15 wiring hooks) | community |

---

## Templates

Starter configurations for common workflow patterns:

| Template | Description |
|----------|-------------|
| [api-service](./templates/api-service.yaml) | HTTP API with auth, SQLite, and pipelines |
| [event-processor](./templates/event-processor.yaml) | Event-driven processor with messaging and pipelines |
| [full-stack](./templates/full-stack.yaml) | Full application with HTTP, auth, state machine, messaging, scheduler, and observability |
| [plugin](./templates/plugin.yaml) | Scaffold for building a new engine plugin |
| [ui-plugin](./templates/ui-plugin.yaml) | Scaffold for a React UI extension |
| [stream-processor](./templates/stream-processor.yaml) | Stream processing pipeline using the Bento plugin |

Initialize a project from a template:

```bash
wfctl init my-project --template api-service
```

---

## Schema

All plugin manifests must conform to the [registry schema](./schema/registry-schema.json). The schema is a JSON Schema (draft 2020-12) defining required fields, enums for `type` and `tier`, and the structure of `capabilities`.

Validate a manifest locally:

```bash
# Validate a single manifest
npx ajv-cli validate --spec=draft2020 -s schema/registry-schema.json -d plugins/my-plugin/manifest.json

# Validate all manifests at once
bash scripts/validate-manifests.sh

# Validate template plugin references
bash scripts/validate-templates.sh
```

---

## Local Pre-commit Hook

Install the provided pre-commit hook to catch validation errors before they reach CI:

```bash
cp scripts/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

The hook runs `scripts/validate-manifests.sh` when `plugins/*/manifest.json` or `schema/registry-schema.json` are staged, and `scripts/validate-templates.sh` when `templates/*.yaml` files are staged.

---

## CI/CD

Every pull request and push to `main` triggers the [Validate Registry](.github/workflows/validate.yml) workflow, which:

1. Validates all `plugins/*/manifest.json` files against `schema/registry-schema.json` (JSON Schema draft 2020-12 via `ajv-cli`)
2. Checks that every plugin referenced in `templates/*.yaml` has a corresponding manifest

PRs that fail validation cannot be merged.

---

## Submitting a Community Plugin

1. **Fork** this repository
2. **Create** a directory under `plugins/<your-plugin-name>/`
3. **Add** a `manifest.json` that conforms to the [registry schema](./schema/registry-schema.json)
4. **Validate** your manifest against the schema
5. **Open a PR** with a description of your plugin

### Manifest Requirements

- `name`, `version`, `author`, `description`, `type`, `tier`, `license` are required
- `type` must be `"external"` for community plugins (only GoCodeAlone sets `"builtin"`)
- `tier` must be `"community"` for third-party submissions
- `source` should point to the public repository where the plugin lives
- `capabilities.moduleTypes`, `stepTypes`, `triggerTypes`, `workflowHandlers` must accurately reflect what the plugin registers

### Review Process

PRs are reviewed by maintainers for:
- Schema validity
- Accurate capability declarations
- Source repo accessibility
- License compatibility

---

## Plugin Manifest Format

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "author": "your-github-username",
  "description": "What this plugin provides",
  "source": "github.com/yourorg/my-plugin",
  "path": ".",
  "type": "external",
  "tier": "community",
  "license": "MIT",
  "minEngineVersion": "0.1.0",
  "keywords": ["tag1", "tag2"],
  "capabilities": {
    "moduleTypes": ["mymodule.type"],
    "stepTypes": ["step.my_step"],
    "triggerTypes": [],
    "workflowHandlers": []
  }
}
```

Full schema documentation: [`schema/registry-schema.json`](./schema/registry-schema.json)

---

## License

MIT — see [LICENSE](./LICENSE)
