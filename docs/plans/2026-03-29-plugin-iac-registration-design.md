---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow
    commit: a0929e9
  - repo: workflow
    commit: bacd7f5
  - repo: workflow
    commit: 9a886f8
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "moduleInfraRequirements|SecretStore|secretsStoreOverride" config cmd mcp docs -S'
    - 'git log --oneline --all -- config/plugin_manifest.go cmd/wfctl/plugin_infra.go cmd/wfctl/secrets_resolve.go'
  result: pass
supersedes: []
superseded_by: []
---

# Plugin IaC Registration Design

**Date:** 2026-03-29
**Status:** Approved
**Scope:** plugin.json manifest schema, wfctl detect/scaffold tools, secrets architecture, wizard

## Overview

Plugins that introduce modules with infrastructure dependencies should declare those dependencies in their manifests. When `wfctl` detects a plugin module in use, it knows what infrastructure that module needs and can scaffold/provision it through the existing IaC pipeline.

This is NOT a new infrastructure system — it feeds into the existing `infrastructure:`, `secrets:`, and `environments:` sections. The plugin manifest is a **capability-to-dependency mapping**: "IF you use module X, it needs resource Y."

## 1. Plugin Manifest Schema Extension

Add `moduleInfraRequirements` to `plugin.json` — maps each module type to its infrastructure dependencies:

```json
{
  "name": "workflow-plugin-payments",
  "capabilities": {
    "moduleTypes": ["payments.provider", "payments.cache"]
  },
  "moduleInfraRequirements": {
    "payments.provider": {
      "requires": [
        {
          "type": "external-api",
          "name": "payment-gateway",
          "description": "Payment processor API (Stripe, PayPal)",
          "providers": ["stripe", "paypal"],
          "secrets": ["STRIPE_SECRET_KEY"],
          "ports": [],
          "optional": false
        }
      ]
    },
    "payments.cache": {
      "requires": [
        {
          "type": "cache.redis",
          "name": "payments-cache",
          "description": "Redis cache for payment session data",
          "dockerImage": "redis:7-alpine",
          "ports": [6379],
          "optional": false
        }
      ]
    }
  }
}
```

### Resource Types

| Type | Meaning | Provisionable? |
|------|---------|----------------|
| `database.postgres` | PostgreSQL database | Yes (RDS, Cloud SQL, DO Managed DB) |
| `database.mysql` | MySQL database | Yes |
| `nosql.redis` / `cache.redis` | Redis | Yes (ElastiCache, Memorystore) |
| `messaging.nats` | NATS message broker | Yes (container or managed) |
| `messaging.kafka` | Kafka | Yes (MSK, Confluent) |
| `storage.s3` / `storage.gcs` | Object storage | Yes (S3, GCS, Spaces) |
| `external-api` | External service (Stripe, Twilio) | No — needs API keys/credentials |
| `compute.gpu` | GPU compute | Special provisioning |

### Requirement Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | Infrastructure resource type |
| `name` | string | yes | Suggested resource name (user can override) |
| `description` | string | yes | Human-readable explanation |
| `dockerImage` | string | no | Docker image for local dev containers |
| `ports` | int[] | no | Ports the resource needs |
| `secrets` | string[] | no | Secret names this resource introduces |
| `providers` | string[] | no | For external-api: which providers are supported |
| `optional` | bool | no | If true, module works without this (degraded) |

## 2. Integration with Existing IaC Pipeline

### Detection Flow

```
User config has:
  modules:
    - name: stripe
      type: payments.provider

wfctl detect_infra_needs:
  1. Scan config modules
  2. For built-in types → check moduleToInfra map (existing)
  3. For plugin types → load plugin manifest → check moduleInfraRequirements
  4. Return merged suggestions:
     - secrets: [STRIPE_SECRET_KEY]
     - infrastructure: [] (external-api = not provisionable)
```

### Suggestion → Config → Provisioning

Suggestions are written into the existing workflow config sections:
- Provisionable resources → `infrastructure:` section → `wfctl infra apply`
- Secrets → `secrets:` section → `wfctl secrets set`
- External APIs → secrets only (user configures externally)

State tracked in the configured IaC state backend (S3, GCS, PostgreSQL, etc.).

## 3. Per-Environment Infrastructure Resolution

Each infrastructure resource has a resolution strategy per environment:

```yaml
infrastructure:
  game-cache:
    type: cache.redis
    environments:
      local:
        strategy: container
        dockerImage: redis:7-alpine
        port: 6379
      staging:
        strategy: provision
        provider: aws
        config:
          instanceType: cache.t3.micro
          engine: redis
          engineVersion: "7.0"
      production:
        strategy: existing
        connection:
          host: shared-redis.prod.internal
          port: 6379
          auth: ${REDIS_AUTH_TOKEN}
```

### Three Strategies

| Strategy | Meaning | Used by |
|----------|---------|---------|
| `container` | Run as Docker container | `wfctl dev up` |
| `provision` | Create managed cloud resource via IaC | `wfctl infra apply` |
| `existing` | Connect to pre-existing infrastructure | Deploy only (no provisioning) |

### Wizard Flow

```
"The gameserver.cache module needs Redis."

  Local development:     [●] Container  [ ] Existing  [ ] Skip
  Staging:               [ ] Provision   [●] Existing  [ ] Share with prod
  Production:            [ ] Provision   [●] Existing cluster

  → "Production Redis host?" > shared-redis.prod.internal:6379
  → "Authentication needed?" > Yes → adds REDIS_AUTH_TOKEN to secrets
```

## 4. Multi-Store Secrets with Per-Secret Routing

Replace single `secrets.provider` with named stores. Each secret routes to a specific store, with a default for convenience:

```yaml
secretStores:
  github:
    provider: github-secrets
    config:
      repository: GoCodeAlone/myapp
  aws:
    provider: aws-secrets-manager
    config:
      region: us-east-1
      prefix: prod/myapp/
  local:
    provider: env

secrets:
  defaultStore: github
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
      # uses defaultStore (github)

    - name: ENCRYPTION_MASTER_KEY
      description: AES-256 master key managed by security team
      store: aws                 # per-secret override

    - name: TLS_PRIVATE_KEY
      description: TLS certificate private key
      store: aws

environments:
  local:
    secretsStoreOverride: local  # all secrets from env vars in local dev
```

### Store Resolution Order

For a given secret in a given environment:
1. `environments.<env>.secretsStoreOverride` → overrides ALL secrets in that env
2. `secrets.entries[].store` → per-secret override
3. `secrets.defaultStore` → fallback

### Provider Interface

```go
type SecretsProvider interface {
    Get(ctx context.Context, name string) (string, error)
    Set(ctx context.Context, name, value string) error
    Check(ctx context.Context, name string) (SecretState, error)
    List(ctx context.Context) ([]SecretStatus, error)
    Delete(ctx context.Context, name string) error
}
```

### Supported Providers

| Provider | Backend | Check() Method |
|----------|---------|---------------|
| `github-secrets` | GitHub Actions secrets | Always `no_access` (GitHub has no read API for secrets) |
| `aws-secrets-manager` | AWS Secrets Manager | `DescribeSecret` (metadata, lower permission than GetSecretValue) |
| `vault` | HashiCorp Vault | `sys/capabilities-self` |
| `gcp-secret-manager` | GCP Secret Manager | `GetSecret` (metadata only) |
| `do-secrets` | DigitalOcean App Platform | API check |
| `env` | Environment variables | `os.LookupEnv` |
| `file` | Encrypted file (age/sops) | File exists check |

## 5. Secure Secret Input

### AI-Safe Pattern

The AI assistant (via MCP) **never handles secret values**. It generates commands for the user to run:

```
AI: "Run this in your terminal (not here):
     wfctl secrets set STRIPE_SECRET_KEY --env production"
```

### Wizard Bulk Setup

The Bubbletea wizard prompts for all secrets in one form with hidden input:

```
┌─────────────────────────────────────────────────────────┐
│  Secret Setup — Production                               │
│                                                          │
│  DATABASE_URL .............. ✗ not set (github)          │
│    > ●●●●●●●●●●●●●●●●●●●●                              │
│  ENCRYPTION_MASTER_KEY ..... ? no access (aws)           │
│    ℹ Managed externally — verify with your security team │
│  JWT_SIGNING_KEY ........... ✗ not set (github)          │
│    > [Enter to auto-generate]                            │
│                                                          │
│  [Enter] Set  [Tab] Next  [g] Auto-generate  [s] Skip   │
└─────────────────────────────────────────────────────────┘
```

### Standalone Bulk Command

```bash
wfctl secrets setup --env production
# Same form as wizard, reads entries from config, prompts for each
```

### Input Methods

| Method | Usage |
|--------|-------|
| Hidden terminal input | Interactive: `term.ReadPassword()` |
| File | `wfctl secrets set TLS_CERT --from-file ./cert.pem` |
| Env var passthrough | `wfctl secrets set DB_URL --from-env DATABASE_URL` |
| Auto-generate | Cryptographic keys: 32-byte random base64 |

## 6. Access-Aware Secret Status

Five states based on user's access to each secret store:

| Status | Display | Meaning |
|--------|---------|---------|
| `set` | `✓ set` | Exists and readable |
| `not_set` | `✗ not set` | Store accessible, key missing |
| `no_access` | `? no access` | Can't verify — insufficient credentials |
| `fetch_error` | `⚠ fetch error` | Store reachable but returned error |
| `unconfigured` | `○ unconfigured` | Store not configured for this environment |

### Deploy-Time Behavior

At deploy, all secrets must be fetchable. If any secret fails:

```
Fetching secrets for production...
  ✓ DATABASE_URL            (github)
  ✓ STRIPE_SECRET_KEY       (github)
  ✗ ENCRYPTION_MASTER_KEY   (aws) — AccessDeniedException:
    Deploy role needs secretsmanager:GetSecretValue for
    arn:aws:secretsmanager:us-east-1:...:prod/myapp/ENCRYPTION_MASTER_KEY

Deploy aborted. Fix the above secret access issues before deploying.
```

### Wizard Behavior

Only prompts for input on secrets the user CAN write to. For `no_access` stores, shows informational message and skips. User can still see what's needed and coordinate with the team that manages those stores.

## 7. Composition — Multiple Plugins Needing Same Resource

When two plugins both declare a requirement for `cache.redis`, the wizard/MCP tool asks:

```
"Both gameserver.cache and payments.cache need Redis.
 Share one instance or use separate ones?"

 [●] Share (recommended — uses one Redis, both modules connect to it)
 [ ] Separate (provisions two Redis instances)
```

If shared: one `infrastructure:` entry, both modules reference the same connection details.
If separate: two entries with distinct names.

## Implementation Tasks

1. **Manifest schema** — add `moduleInfraRequirements` to plugin.json + registry manifest validation
2. **Config updates** — per-environment infrastructure resolution (`strategy: container|provision|existing`), `secretStores` map, per-secret `store` field, `defaultStore`
3. **Detect updates** — extend `detect_infra_needs` and `detect_secrets` to consult plugin manifests
4. **Provider implementations** — `Check()` method on all secrets providers, github-secrets + aws-sm providers
5. **Wizard updates** — per-environment infra resolution screens, multi-store secret routing, bulk secret setup with access-aware status
6. **Deploy integration** — secret resolution from multi-store at deploy time, access-aware error messages
7. **Documentation** — plugin manifest guide, secrets architecture reference
