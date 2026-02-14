# Workflow Engine

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Built on Modular](https://img.shields.io/badge/Built%20on-CrisisTextLine%2Fmodular-green)](https://github.com/CrisisTextLine/modular)

A production-grade, configuration-driven workflow orchestration engine built on [CrisisTextLine/modular](https://github.com/CrisisTextLine/modular) v1.11.11. Define entire applications in YAML -- from API servers to multi-service chat platforms -- with 48 module types, dynamic hot-reload, AI-powered generation, and a visual builder UI.

## What It Does

The workflow engine turns YAML configuration files into running applications. No code changes needed. The same codebase can operate as:

- A RESTful API server with JWT authentication and middleware chains
- An event-driven pipeline with Kafka messaging and state machines
- A multi-service platform with Docker Compose, reverse proxies, and observability
- An AI-assisted workflow builder with drag-and-drop visual editing

```yaml
modules:
  - name: http-server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: chimux.router
  - name: auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"
  - name: orders-api
    type: api.handler
    config:
      resourceName: orders
      operations: [list, get, create, update]

workflows:
  http:
    routes:
      - method: GET
        path: /api/orders
        handler: orders-api
        middleware: [auth]
```

## Features

### 48 Module Types Across 10 Categories

| Category | Count | Types |
|----------|-------|-------|
| **HTTP** | 10 | http.server, http.router, http.handler, http.middleware.{auth, cors, logging, ratelimit, requestid}, http.proxy, http.simple_proxy |
| **Messaging** | 6 | messaging.broker, messaging.broker.eventbus, messaging.handler, messaging.nats, messaging.kafka, notification.slack |
| **State Machine** | 4 | statemachine.engine, state.tracker, state.connector, processing.step |
| **Modular Framework** | 10 | httpserver, httpclient, chimux, scheduler, auth, eventbus, cache, database, eventlogger, jsonschema |
| **Storage/Persistence** | 4 | database.workflow, persistence.store, storage.s3, static.fileserver |
| **Observability** | 3 | metrics.collector, health.checker, observability.otel |
| **Data/Integration** | 4 | data.transformer, api.handler, webhook.sender, dynamic.component |
| **Auth** | 2 | auth.jwt, auth.modular |
| **Reverse Proxy** | 2 | reverseproxy, http.proxy |
| **Triggers** | 5 | http, schedule, event, eventbus, mock |

### Security

- **JWT Authentication** with user registration, login, token generation/validation, role-based claims, and bcrypt password hashing
- **PII Encryption at Rest** using AES-256-GCM with SHA-256 key derivation -- configurable field-level encryption integrated with PersistenceStore and Kafka payloads
- **Middleware Chain**: CORS, rate limiting, request ID propagation, auth enforcement

### Dynamic Component Hot-Reload

Load Go components at runtime without restarting the server. The [Yaegi](https://github.com/traefik/yaegi) interpreter provides:

- Sandboxed execution with stdlib-only import validation
- File watcher for automatic hot-reload on save
- Component registry with full lifecycle management (init, start, stop)
- HTTP API: `POST/GET/DELETE /api/dynamic/components`

### AI-Powered Workflow Generation

Hybrid AI integration with two providers:

- **Anthropic Claude** -- direct API with tool use for component generation and validation
- **GitHub Copilot SDK** -- session-based integration for development workflows
- Automatic validation loop with compile-test-retry cycle
- Natural language to YAML workflow generation

### EventBus Integration

- Native EventBus bridge adapting MessageBroker to EventBus
- Workflow lifecycle events: `workflow.started`, `workflow.completed`, `workflow.failed`, `step.started`, `step.completed`, `step.failed`
- EventBus trigger for native subscription-based workflow activation
- Topic filtering and async mode support

### Visual Workflow Builder (ReactFlow UI)

- Drag-and-drop node palette with all 48 module types across categorized sections
- Property panel for node configuration with type-specific fields
- YAML import/export with round-trip fidelity
- Undo/redo, validation (local + server), Zustand state management

### Observability

- **Prometheus** metrics collection with 6 pre-registered metric vectors
- **Grafana** dashboards for platform monitoring
- **OpenTelemetry** tracing via OTLP/HTTP export
- Health check endpoints: `/health`, `/ready`, `/live`
- Request ID propagation via `X-Request-ID` header

### Dynamic Field Mapping

Schema-agnostic field resolution for REST API handlers:

- `FieldMapping` type with fallback chains and primary/resolve/set operations
- Configurable field aliases in YAML (`fieldMapping`, `transitionMap`, `summaryFields`)
- Runtime field resolution from workflow context

## Quick Start

### Requirements

- Go 1.26+
- Node.js 18+ (for UI development)

### Run the Server

```bash
# Clone the repository
git clone https://github.com/GoCodeAlone/workflow.git
cd workflow

# Build and run with an example config
go build -o server ./cmd/server
./server -config example/order-processing-pipeline.yaml

# Or run directly
go run ./cmd/server -config example/order-processing-pipeline.yaml
```

The server starts on `:8080` by default. Override with `-addr :9090`.

### Run the Visual Builder

```bash
cd ui
npm install
npm run dev
```

Opens at [http://localhost:5173](http://localhost:5173) with hot module replacement.

### Run with Docker

```bash
# Chat platform (multi-service with Kafka, Prometheus, Grafana)
cd example/chat-platform
docker compose up

# E-commerce app
cd example/ecommerce-app
docker compose up
```

## Example Applications

### Chat Platform -- Production-Grade Mental Health Support

A 73-file, multi-service platform demonstrating the full capabilities of the engine. Located in [`example/chat-platform/`](example/chat-platform/).

**Architecture:**
```
Browser -> [gateway:8080] -> reverse proxy -> [api:8081]          (auth, CRUD, admin)
                                           -> [conversation:8082] (chat, state machine)

[conversation] <-> [Kafka] <-> [conversation]  (event-driven messaging)
[prometheus] -> [grafana]                       (observability)
```

**Highlights:**
- 6 Docker Compose services: gateway, API, conversation, Kafka, Prometheus, Grafana
- 18 dynamic components: AI summarizer, PII encryptor, risk tagger, conversation router, survey engine, escalation handler, keyword matcher, and more
- Full SPA with role-based views: admin, responder, and supervisor dashboards
- Conversation state machine with 13 states and 18 transitions (queued -> assigned -> active -> wrap_up -> closed)
- Real-time risk assessment with keyword pattern matching across 5 categories (self-harm, suicidal ideation, crisis, substance abuse, domestic violence)
- PII masking in UI, field-level encryption at rest
- Webchat widget, SMS providers (Twilio, AWS, partner webhooks)
- Seed data system for users, affiliates, programs, keywords, and surveys

### Order Processing Pipeline

A 10+ module workflow demonstrating module composition with HTTP servers, routers, handlers, data transformers, state machines, message brokers, and observability. See [`example/order-processing-pipeline.yaml`](example/order-processing-pipeline.yaml).

### 100+ Example Configurations

The [`example/`](example/) directory contains configurations covering:

- API gateways and reverse proxies
- Event-driven and scheduled workflows
- State machine lifecycle management
- Data transformation and webhook delivery
- Multi-workflow composition
- Real-time messaging
- Dependency injection patterns
- Multi-tenant scenarios

Each example includes a companion `.md` file documenting its architecture and usage.

## Architecture

```
cmd/server/          Server binary, HTTP mux, graceful shutdown
  main.go            Entry point with CLI flags and AI provider init

config/              YAML config structs (WorkflowConfig, ModuleConfig)
module/              48 built-in module implementations
handlers/            5 workflow handler types:
                       HTTP, Messaging, StateMachine, Scheduler, Integration
dynamic/             Yaegi-based hot-reload system
ai/                  AI integration layer
  llm/                 Anthropic Claude direct API with tool use
  copilot/             GitHub Copilot SDK with session management
  service.go           Provider selection and orchestration
  deploy.go            Validation loop and deployment to dynamic components
ui/                  React + ReactFlow + Zustand visual builder (Vite, TypeScript)
example/             100+ YAML configs and 2 full application examples
mock/                Test helpers and mock implementations
```

**Core flow:**
1. `StdEngine` loads YAML config via `BuildFromConfig()`
2. Each module definition is matched to a factory (48 built-in types) and instantiated
3. Modules register with the modular `Application` (dependency injection, service registry)
4. Workflow handlers (HTTP, Messaging, StateMachine, Scheduler, Integration) configure workflows
5. Triggers (HTTP endpoints, EventBus subscriptions, cron schedules) start the system
6. `TriggerWorkflow()` dispatches incoming events to the correct handler, emitting lifecycle events

**Key interfaces:**
- `modular.Module` -- all components implement `Name()`, `Dependencies()`, `Configure()`
- `WorkflowHandler` -- `CanHandle()`, `ConfigureWorkflow()`, `ExecuteWorkflow()`
- `Trigger` -- `Name()`, `Start(ctx)`, `Stop(ctx)`

### Adding a New Module Type

1. Implement the module in `module/`
2. Register it in `engine.go`'s `BuildFromConfig` switch statement
3. Add an example YAML config in `example/`

### Adding a New Workflow Handler

1. Implement the `WorkflowHandler` interface in `handlers/`
2. Register with `engine.RegisterWorkflowHandler()` in `cmd/server/main.go`

## Testing

```bash
# All Go tests
go test ./...

# With race detection
go test -race ./...

# With coverage
go test -cover ./...

# Single test
go test -v -run TestName .

# UI component tests (Vitest)
cd ui && npm test

# UI E2E tests (Playwright)
cd ui && npx playwright test

# Lint
go fmt ./...
golangci-lint run
cd ui && npm run lint
```

**Test coverage targets:** root package 80%+, module 80%+, dynamic 80%+, AI packages 85%+.

## Technology

| Component | Technology |
|-----------|-----------|
| Language | Go 1.26 |
| Framework | [CrisisTextLine/modular](https://github.com/CrisisTextLine/modular) v1.11.11 |
| UI | React, ReactFlow, Zustand, Vite, TypeScript |
| Hot-Reload | [Yaegi](https://github.com/traefik/yaegi) Go interpreter |
| Messaging | Apache Kafka ([Sarama](https://github.com/IBM/sarama)), NATS, EventBus |
| Database | SQLite ([modernc](https://pkg.go.dev/modernc.org/sqlite)), PostgreSQL ([pgx](https://github.com/jackc/pgx)) |
| Storage | AWS S3 |
| Auth | JWT ([golang-jwt](https://github.com/golang-jwt/jwt)), OAuth2 |
| Encryption | AES-256-GCM, bcrypt |
| Metrics | Prometheus, Grafana |
| Tracing | OpenTelemetry (OTLP/HTTP) |
| AI | Anthropic Claude API, GitHub Copilot SDK |
| Containers | Docker multi-stage builds, Docker Compose |
| Testing | Go testing, Vitest, Playwright |

## Roadmap

See [ROADMAP.md](ROADMAP.md) for the full development history (Phases 1-6 complete) and planned work including JSON Schema config validation, performance benchmarks, Helm charts, and security hardening.

## License

MIT
