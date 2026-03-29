# Plugin Manifest Guide

This guide explains how plugin authors declare infrastructure requirements in `plugin.json` so that `wfctl` can detect and provision them automatically.

---

## Overview

When a workflow application uses a plugin module type (e.g., `payments.provider`, `agent.executor`), the plugin may require external infrastructure — a database, a message queue, a cache. The `moduleInfraRequirements` field in `plugin.json` lets you declare these dependencies so that:

- `wfctl detect_infra_needs` surfaces them alongside built-in infra requirements
- `wfctl wizard` includes them in the infra resolution screen
- `wfctl dev up` can provision the correct containers locally

---

## plugin.json Structure

```json
{
  "name": "workflow-plugin-payments",
  "version": "0.2.1",
  "description": "Payment processing plugin with Stripe and PayPal support",
  "capabilities": {
    "moduleTypes": ["payments.provider"],
    "stepTypes": [
      "step.payment_charge",
      "step.payment_capture",
      "step.payment_refund"
    ],
    "triggerTypes": []
  },
  "moduleInfraRequirements": {
    "payments.provider": {
      "requires": [
        {
          "type": "postgresql",
          "name": "payments-db",
          "description": "PostgreSQL database for payment records and audit log",
          "dockerImage": "postgres:16",
          "ports": [5432],
          "secrets": ["DATABASE_URL"],
          "providers": ["aws", "gcp", "azure"],
          "optional": false
        },
        {
          "type": "redis",
          "name": "payments-cache",
          "description": "Redis for idempotency key cache",
          "dockerImage": "redis:7",
          "ports": [6379],
          "secrets": ["REDIS_URL"],
          "providers": ["aws", "gcp", "azure"],
          "optional": true
        }
      ]
    }
  }
}
```

---

## moduleInfraRequirements Fields

The `moduleInfraRequirements` key is a map from **module type** to a spec object.

### ModuleInfraSpec

| Field | Type | Description |
|-------|------|-------------|
| `requires` | array | List of infrastructure requirements for this module type |

### InfraRequirement

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | Infrastructure type: `postgresql`, `redis`, `nats`, `elasticsearch`, `mongodb`, `s3-bucket`, `queue`, etc. |
| `name` | string | yes | Logical name for this dependency (must be unique within the plugin) |
| `description` | string | no | Human-readable description shown in `wfctl` output |
| `dockerImage` | string | no | Docker image to use when running locally (e.g., `postgres:16`) |
| `ports` | array[int] | no | Container ports the service listens on |
| `secrets` | array[string] | no | Secret names from `secrets.entries` this dependency requires |
| `providers` | array[string] | no | IaC providers that can provision this resource (e.g., `aws`, `gcp`, `azure`, `digitalocean`) |
| `optional` | bool | no | If `true`, the dependency is not required for the plugin to function (defaults to `false`) |

---

## How `wfctl` Uses This Information

### `wfctl secrets detect --config app.yaml`

Scans `plugin.json` files in the plugins directory (default: `plugins/`). For each module type declared in `config.modules[*].type`, it looks up the manifest and surfaces any `InfraRequirement.secrets` entries alongside the config-detected secrets.

### `wfctl detect_infra_needs` (MCP tool)

The `detect_infra_needs` MCP tool accepts an optional `plugins_dir` parameter. It merges plugin-declared infra requirements with the built-in infrastructure analysis. Returns a combined list of infra needs for the AI assistant to suggest provisioning steps.

### `wfctl wizard` (infra resolution screen)

The wizard's **Infrastructure** screen detects selected module types and cross-references them with plugin manifests. Required infra resources from plugins appear in the **Infra Resolution** screen alongside PostgreSQL/Redis/NATS, letting you choose container/provision/existing per environment.

---

## Example: Agent Plugin

An agent plugin that requires a vector database:

```json
{
  "name": "workflow-plugin-agent",
  "version": "0.5.2",
  "capabilities": {
    "moduleTypes": ["agent.executor", "agent.memory"],
    "stepTypes": ["step.agent_execute", "step.memory_extract"],
    "triggerTypes": []
  },
  "moduleInfraRequirements": {
    "agent.memory": {
      "requires": [
        {
          "type": "postgresql",
          "name": "agent-memory-db",
          "description": "PostgreSQL with pgvector extension for long-term memory",
          "dockerImage": "pgvector/pgvector:pg16",
          "ports": [5432],
          "secrets": ["AGENT_DATABASE_URL"],
          "providers": ["aws", "gcp", "azure"],
          "optional": false
        }
      ]
    }
  }
}
```

When an app config declares `type: agent.memory` in `modules:`, `wfctl detect_infra_needs` will include `agent-memory-db` (type: `postgresql`) in its output.

---

## Conventions

- Use lowercase, hyphenated `type` values: `postgresql`, `redis`, `nats`, `s3-bucket`
- `name` should be unique per manifest; prefix with the plugin name for clarity (e.g., `payments-db`, `agent-memory-db`)
- Always populate `dockerImage` for types that have a standard Docker Hub image — this enables `wfctl dev up` to start the dependency locally without manual configuration
- List only the providers your plugin has been tested with in `providers`
- Mark dependencies as `optional: true` when the module degrades gracefully without them (e.g., a cache that falls back to in-memory)

---

## Registering Your Plugin in the Registry

After adding `moduleInfraRequirements` to your `plugin.json`, push a new release. The workflow-registry manifest for your plugin should reference the same infra requirements so that `wfctl registry search` can surface them during project setup.

See the [Plugin Development Guide](PLUGIN_DEVELOPMENT_GUIDE.md) for publishing steps.
