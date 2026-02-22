# Workflow Engine Documentation

## Overview

The Workflow Engine is a configuration-driven orchestration platform built in Go. It turns YAML configuration files into running applications with no code changes required. The engine provides 48+ built-in module types, a visual workflow builder UI, a multi-tenant admin platform, AI-assisted configuration generation, and dynamic hot-reload of Go components at runtime.

## Core Engine

The engine is built on the [CrisisTextLine/modular](https://github.com/CrisisTextLine/modular) framework for module lifecycle, dependency injection, and service registry management.

**Key capabilities:**
- YAML-driven configuration with environment variable expansion (`${JWT_SECRET}`)
- Config validation via JSON Schema
- Module factory registry with 48 built-in types
- Trigger-based workflow dispatch (HTTP, EventBus, cron schedule)
- Graceful lifecycle management (start/stop)

**CLI tools:**
- `cmd/server` -- runs workflow configs as a server process
- `cmd/wfctl` -- validates and inspects workflow configs offline

## Module Types (48+)

All modules are registered in `engine.go` and instantiated from YAML config. Organized by category:

### HTTP & Routing
| Type | Description |
|------|-------------|
| `http.server` | Configurable web server |
| `http.router` | Request routing with path and method matching |
| `http.handler` | HTTP request processing with configurable responses |
| `http.proxy` | HTTP reverse proxy |
| `http.simple_proxy` | Simplified proxy configuration |
| `httpserver.modular` | Modular framework HTTP server integration |
| `httpclient.modular` | Modular framework HTTP client |
| `chimux.router` | Chi mux-based router |
| `reverseproxy` | Modular framework reverse proxy (v2) |
| `static.fileserver` | Static file serving |

### Middleware
| Type | Description |
|------|-------------|
| `http.middleware.auth` | Authentication middleware |
| `http.middleware.cors` | CORS header management |
| `http.middleware.logging` | Request/response logging |
| `http.middleware.ratelimit` | Rate limiting |
| `http.middleware.requestid` | Request ID injection |
| `http.middleware.securityheaders` | Security header injection |

### Authentication
| Type | Description |
|------|-------------|
| `auth.jwt` | JWT authentication with seed users, persistence, token refresh |
| `auth.modular` | Modular framework auth integration |
| `auth.user-store` | User storage backend |

### API & CQRS
| Type | Description |
|------|-------------|
| `api.handler` | Generic REST resource handler |
| `api.command` | CQRS command handler with route pipelines |
| `api.query` | CQRS query handler with route pipelines |

### State Machine
| Type | Description |
|------|-------------|
| `statemachine.engine` | State definitions, transitions, hooks, auto-transitions |
| `state.tracker` | State observation and tracking |
| `state.connector` | State machine interconnection |

### Messaging
| Type | Description |
|------|-------------|
| `messaging.broker` | In-memory message broker |
| `messaging.broker.eventbus` | EventBus-backed message broker |
| `messaging.handler` | Message processing handler |
| `messaging.kafka` | Apache Kafka broker integration |
| `messaging.nats` | NATS broker integration |

### Database & Persistence
| Type | Description |
|------|-------------|
| `database.modular` | Modular framework database integration |
| `database.workflow` | Workflow-specific database (SQLite + PostgreSQL) |
| `persistence.store` | Write-through persistence (SQLite/PostgreSQL) |

### Pipeline Steps
| Type | Description |
|------|-------------|
| `processing.step` | Configurable processing step |
| `step.validate` | Validates pipeline data against required fields or JSON schema |
| `step.transform` | Transforms data shape and field mapping |
| `step.conditional` | Conditional branching based on field values |
| `step.set` | Sets values in pipeline context with template support |
| `step.log` | Logs pipeline data for debugging |
| `step.publish` | Publishes events to EventBus |
| `step.http_call` | Makes outbound HTTP requests |
| `step.delegate` | Delegates to a named service |
| `step.request_parse` | Extracts path params, query params, and request body from HTTP requests |
| `step.db_query` | Executes parameterized SQL SELECT queries against a named database |
| `step.db_exec` | Executes parameterized SQL INSERT/UPDATE/DELETE against a named database |
| `step.json_response` | Writes HTTP JSON response with custom status code and headers |

### Template Functions

Pipeline steps support Go template syntax with these built-in functions:

| Function | Description | Example |
|----------|-------------|---------|
| `uuidv4` | Generates a UUID v4 | `{{ uuidv4 }}` |
| `now` | Current time in RFC3339 format | `{{ now }}` |
| `lower` | Lowercase string | `{{ lower .name }}` |
| `default` | Default value when empty | `{{ default "pending" .status }}` |
| `json` | Marshal value to JSON string | `{{ json .data }}` |

Template expressions can reference previous step outputs via `{{ .steps.step-name.field }}` or for hyphenated names `{{index .steps "step-name" "field"}}`.

### Observability
| Type | Description |
|------|-------------|
| `metrics.collector` | Prometheus metrics collection and `/metrics` endpoint |
| `health.checker` | Health endpoints (`/healthz`, `/readyz`, `/livez`) |
| `log.collector` | Centralized log collection |
| `observability.otel` | OpenTelemetry tracing integration |
| `eventlogger.modular` | Modular framework event logger |

### Storage
| Type | Description |
|------|-------------|
| `storage.s3` | Amazon S3 storage |
| `storage.gcs` | Google Cloud Storage |
| `storage.local` | Local filesystem storage |
| `storage.sqlite` | SQLite storage |

### Scheduling
| Type | Description |
|------|-------------|
| `scheduler.modular` | Cron-based job scheduling |

### Integration
| Type | Description |
|------|-------------|
| `webhook.sender` | Outbound webhook delivery with retry and dead letter |
| `notification.slack` | Slack notifications |
| `openapi.consumer` | OpenAPI spec consumer for external service integration |
| `openapi.generator` | OpenAPI spec generation from workflow config |

### Secrets
| Type | Description |
|------|-------------|
| `secrets.vault` | HashiCorp Vault integration |
| `secrets.aws` | AWS Secrets Manager integration |

### Other
| Type | Description |
|------|-------------|
| `cache.modular` | Modular framework cache |
| `jsonschema.modular` | JSON Schema validation |
| `eventbus.modular` | Modular framework EventBus |
| `dynamic.component` | Yaegi hot-reload Go component |
| `data.transformer` | Data transformation |
| `workflow.registry` | Workflow registration and discovery |

## Workflow Types

Workflows are configured in YAML and dispatched by the engine through registered handlers (`handlers/` package):

| Type | Description |
|------|-------------|
| **HTTP** | Route definitions, middleware chains, route pipelines with ordered steps |
| **Messaging** | Pub/sub topic subscriptions with message handlers |
| **State Machine** | State definitions, transitions, hooks, auto-transitions |
| **Scheduler** | Cron-based recurring task execution |
| **Integration** | External service composition and orchestration |

## Trigger Types

Triggers start workflow execution in response to external events:

| Type | Description |
|------|-------------|
| **HTTP** | Routes mapped to workflow actions |
| **Event** | EventBus subscription triggers workflow action |
| **EventBus** | EventBus topic subscription |
| **Schedule** | Cron expression-based scheduling |

## Configuration Format

```yaml
name: "Example Workflow"
description: "A workflow with HTTP server, JWT auth, and health monitoring"

modules:
  - name: "http-server"
    type: "http.server"
    config:
      address: ":${PORT:-8080}"

  - name: "jwt-auth"
    type: "auth.jwt"
    config:
      secret: "${JWT_SECRET}"
      token_expiry: "24h"

  - name: "health"
    type: "health.checker"
    config:
      path: "/healthz"

  - name: "metrics"
    type: "metrics.collector"
    config:
      path: "/metrics"
      namespace: "myapp"

  - name: "api-router"
    type: "http.router"
    config:
      routes:
        - path: "/api/v1/users"
          method: "GET"
          handler: "user-handler"

workflows:
  - name: "main-workflow"
    type: "http"
    config:
      endpoints:
        - path: "/health"
          method: "GET"
          response:
            statusCode: 200
            body: '{"status": "ok"}'

triggers:
  - name: "http-trigger"
    type: "http"
    config:
      route: "/api/v1/orders"
      method: "POST"
      workflow: "order-workflow"
      action: "create-order"
```

## Visual Workflow Builder (UI)

A React-based visual editor for composing workflow configurations (`ui/` directory).

**Technology stack:** React, ReactFlow, Zustand, TypeScript, Vite

**Features:**
- Drag-and-drop canvas for module composition
- Node palette with search and click-to-add
- Property panel with per-module config forms driven by module schemas
- Array and map field editors for complex config values
- Middleware chain visualization with ordered badges
- Pipeline step visualization with pipeline-flow edges on canvas
- Handler route editor with inline pipeline step editing
- YAML import and export
- Auto-layout using dagre algorithm
- Collapsible side panels
- Connection compatibility rules preventing invalid edges
- Module schemas fetched from `/api/v1/module-schemas` endpoint

## Admin Platform (V1 API)

A multi-tenant administration platform for managing workflows at scale.

**Data model:** Companies -> Organizations -> Projects -> Workflows

**Capabilities:**
- Role-based access control (Owner, Admin, Editor, Viewer)
- JWT authentication with login, register, token refresh, logout
- REST API endpoints for all resource CRUD operations
- Workflow versioning with deploy/stop lifecycle
- Execution tracking with step-level detail
- Audit trail
- Dashboard with system metrics
- IAM provider integration (SAML/OIDC)
- Workspace file management

**Pipeline-native API routes** use declarative step sequences (request_parse -> db_query -> json_response) instead of delegating to monolithic Go handler services. This proves the engine's completeness -- it can express its own admin API using its own primitives.

## AI Integration

Hybrid approach with two providers (`ai/` package):

- **Anthropic Claude** (`ai/llm/`) -- direct API with tool use for component and config generation
- **GitHub Copilot SDK** (`ai/copilot/`) -- session-based integration (Technical Preview)
- **Service layer** (`ai/service.go`, `ai/deploy.go`) -- provider selection, validation loop with retry, deployment to dynamic components
- **Specialized analyzers** -- sentiment analysis, alert classification, content suggestions

## Dynamic Hot-Reload

Yaegi-based runtime loading of Go components (`dynamic/` package):

- Load Go source files as modules at runtime without restart
- Sandbox validates stdlib-only imports for security
- `ModuleAdapter` wraps dynamic components as `modular.Module` instances
- File watcher monitors directories for automatic reload
- Resource limits and contract enforcement
- HTTP API: `POST/GET/DELETE /api/dynamic/components`

## Testing

The project has comprehensive test coverage across multiple layers:

- **Go unit tests** -- 43+ passing test packages including module, handler, engine, config, schema, AI, dynamic, and webhook packages
- **Integration tests** (`tests/integration/`) -- cross-package integration scenarios
- **Regression tests** (`tests/regression/`) -- preventing known bug recurrence
- **Load tests** (`tests/load/`) -- performance and scalability testing
- **Chaos tests** (`tests/chaos/`) -- failure injection and resilience testing
- **UI unit tests** (Vitest) -- 180+ test files covering React components, stores, and utilities
- **E2E tests** (Playwright) -- browser-based end-to-end testing of the UI

## Example Applications (36 configs)

The `example/` directory contains workflow configurations demonstrating different patterns:

**Full applications:**
- **Chat Platform** (`example/chat-platform/`) -- multi-config application with API gateway, conversation management, and state machine (1200+ lines, 13-state machine, 60+ routes)
- **E-commerce App** (`example/ecommerce-app/`) -- order processing, user/product management, API gateway
- **Multi-workflow E-commerce** (`example/multi-workflow-ecommerce/`) -- cross-workflow orchestration with branching, fulfillment, and notifications

**Individual configs:**
- API Gateway, API Server, API Gateway (modular)
- Data Pipeline, Data Sync Pipeline
- Event Processing, Event-Driven Workflow
- Integration Workflow
- Multi-Workflow Orchestration
- Notification Pipeline, Webhook Pipeline
- Order Processing Pipeline
- Realtime Messaging (modular)
- Scheduled Jobs, Advanced Scheduler Workflow
- Simple Workflow, SMS Chat
- State Machine Workflow
- Trigger Workflow, Dependency Injection

## Current Limitations

1. **Single-process execution** -- sharding and worker pool primitives exist, but no distributed mode in production yet
2. **In-memory broker is default** -- Kafka and NATS module types exist but need production hardening
3. **No Kubernetes operator** -- Helm chart exists, but no CRD-based operator for auto-scaling
4. **No infrastructure provisioning** -- platform deploys apps but doesn't provision underlying infrastructure (databases, brokers)
5. **No billing/metering** -- execution tracking exists but no payment integration
6. **No event replay** -- execution history is recorded but cannot be replayed or backfilled
7. **No idempotency store** -- at-least-once delivery without deduplication
8. **In-process state machine locking** -- needs distributed locks for horizontal scaling
9. **Limited observability UI** -- step-level tracking exists in API but no execution timeline visualization

## Platform Roadmap

The roadmap is organized around transforming Workflow from a config-driven app builder into a full platform with event-native execution, infrastructure management, and "Datadog-level" observability. See [PLATFORM_ROADMAP.md](docs/PLATFORM_ROADMAP.md) for the complete plan.

### Phase 1: Durable Execution (Weeks 1-8)
- Event store (append-only execution history as source of truth)
- Idempotency key store for exactly-once effects
- Execution timeline UI (step-by-step view with inputs/outputs/timing)
- Request replay API (replay any past execution)
- Billing integration (Stripe)

### Phase 2: Event-Native Infrastructure (Weeks 9-16)
- Source/sink connector framework with plugin interface
- Database CDC connector (PostgreSQL logical replication)
- Enhanced transforms (JQ expressions, nested operations)
- Dead letter queue UI with inspection and replay
- Event backfill (replay from timestamp through pipeline)
- Step mocking for testing

### Phase 3: Infrastructure & Scale (Weeks 17-24)
- Infrastructure-as-Config (declare databases, brokers, caches in YAML)
- Kubernetes operator with CRDs for auto-scaling
- Distributed state machine (Redis-based distributed locks)
- Blue/green deployment support
- Circuit breaker middleware
- Multi-region data routing

### Phase 4: Enterprise & Ecosystem (Weeks 25-32)
- Saga orchestrator (cross-service transactions with compensation)
- Live request tracing and pipeline breakpoints
- AI-safe orchestration (LLM steps with guardrails)
- Plugin marketplace UI
- Client SDKs (TypeScript, Python, Go)
- SOC2 audit readiness
