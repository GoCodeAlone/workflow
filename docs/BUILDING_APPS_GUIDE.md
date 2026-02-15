# Building Applications with the Workflow Engine

A comprehensive guide for developers who want to build production web applications and backend services using the Workflow engine instead of writing a custom Go server from scratch.

Throughout this guide, we reference the [Chat Platform example](../example/chat-platform/) as a real-world case study. For a walkthrough of the chat platform itself, see [tutorials/chat-platform-walkthrough.md](tutorials/chat-platform-walkthrough.md). This guide teaches you the **process** of building your own application.

---

## Table of Contents

1. [Planning Your Application](#1-planning-your-application)
2. [Starting from Scratch](#2-starting-from-scratch)
3. [Building the Module Stack](#3-building-the-module-stack)
4. [Wiring Workflows](#4-wiring-workflows)
5. [Writing Dynamic Components](#5-writing-dynamic-components)
6. [Setting Up Triggers](#6-setting-up-triggers)
7. [Deployment](#7-deployment)
8. [Using the Visual Builder](#8-using-the-visual-builder)
9. [Common Patterns](#9-common-patterns)
10. [Troubleshooting](#10-troubleshooting)

---

## 1. Planning Your Application

Before writing YAML, spend time understanding what your application needs to do. The Workflow engine replaces the boilerplate of building a Go HTTP server, wiring middleware, setting up databases, and implementing state machines. Your job is to decompose your application into the right set of modules.

### Identify Your Application's Concerns

Every web application has a set of cross-cutting concerns. List yours:

| Concern | Questions to Answer |
|---------|-------------------|
| **HTTP API** | What endpoints do you need? REST CRUD? Webhooks? Static files? |
| **Authentication** | JWT tokens? OAuth2? API keys? Seed users for development? |
| **Persistence** | What data do you store? SQLite for dev, PostgreSQL for prod? |
| **Business Logic** | What custom processing do you need beyond CRUD? |
| **State Management** | Do entities have lifecycles? (e.g., orders: created -> paid -> shipped) |
| **Messaging** | Do components need async communication? Event-driven patterns? |
| **Observability** | Health checks? Metrics? Structured logging? |
| **Integrations** | External APIs? Webhooks to third parties? S3 storage? |

For example, the Chat Platform identified these concerns:

- HTTP API for conversations, users, affiliates, programs, keywords, surveys
- JWT authentication with role-based users (responder, supervisor, admin)
- SQLite persistence with seed data files
- State machine for conversation lifecycle (13 states, 17 transitions)
- Dynamic components for SMS providers, routing, encryption, surveys
- In-memory messaging for event notifications
- Health checks and Prometheus metrics

### Map Concerns to Module Types

The engine provides 30+ built-in module types. Here is how common concerns map to modules:

| Concern | Module Type(s) |
|---------|---------------|
| HTTP server | `http.server` |
| URL routing | `http.router` |
| CORS | `http.middleware.cors` |
| Request tracing | `http.middleware.requestid` |
| Rate limiting | `http.middleware.ratelimit` |
| JWT auth | `auth.jwt` + `http.middleware.auth` |
| REST CRUD | `api.handler` |
| CQRS reads | `api.query` |
| CQRS writes | `api.command` |
| Database | `database.workflow` (PostgreSQL/MySQL/SQLite) |
| Key-value persistence | `persistence.store` |
| State machine | `statemachine.engine` |
| Custom logic | `dynamic.component` |
| Processing step | `processing.step` |
| Event broker | `messaging.broker` (in-memory), `messaging.nats`, `messaging.kafka` |
| Event handler | `messaging.handler` |
| Health probes | `health.checker` |
| Prometheus metrics | `metrics.collector` |
| Static files / SPA | `static.fileserver` |
| Reverse proxy | `http.simple_proxy`, `http.proxy`, `reverseproxy` |
| Cron jobs | `scheduler.modular` |
| Secrets | `secrets.vault`, `secrets.aws` |
| Object storage | `storage.s3`, `storage.gcs`, `storage.local` |

### Decide What Needs Dynamic Components

Built-in modules handle infrastructure. For **business logic specific to your application**, use dynamic components:

- **Use built-in modules for**: HTTP routing, auth, CRUD, persistence, health, metrics, messaging infrastructure
- **Use dynamic components for**: Payment processing, custom routing logic, data transformation, third-party API integrations, domain-specific validation, encryption, AI/ML calls

Rule of thumb: if the logic is generic (CRUD, auth, health), use a built-in module. If it is specific to your domain (keyword matching, risk assessment, SMS provider integration), write a dynamic component.

### Design Your Data Flow

Sketch how data flows through your application:

```
Request arrives
    |
    v
HTTP Server --> Router --> Middleware Chain (CORS -> RequestID -> RateLimit -> Auth)
    |
    v
API Handler (CRUD operations, seed data)
    |
    v
State Machine (lifecycle transitions, hooks)
    |
    v
Processing Steps (dynamic components with retry/timeout)
    |
    v
Event Broker --> Messaging Handlers (notifications, side effects)
```

The Chat Platform's flow:

```
Inbound webhook/webchat
    |
    v
POST /api/webhooks/* --> cors -> request-id -> rate-limiter --> webhooks-api
    |
    v
conversation-engine (state machine: new -> queued -> assigned -> active -> ...)
    |
    v
step-route-message --> keyword-matcher (dynamic component)
    |
    v
event-broker --> notification-handler
```

---

## 2. Starting from Scratch

### Prerequisites

- **Go 1.25+** installed
- The workflow repository cloned and built:

```bash
git clone https://github.com/GoCodeAlone/workflow.git
cd workflow
go build -o server ./cmd/server
```

The `server` binary is the only executable you need. Everything else is YAML configuration and optional dynamic component source files.

### Creating the Directory Structure

Create a directory for your application:

```bash
mkdir -p my-app/seed
mkdir -p my-app/components
mkdir -p my-app/data
```

```
my-app/
  workflow.yaml         # Main configuration
  seed/                 # JSON files with initial data
    users.json
  components/           # Dynamic component Go source files
    my_processor.go
  data/                 # Runtime data (SQLite databases, etc.)
```

### Writing Your First Config

Start with the absolute minimum: an HTTP server, a router, and one handler.

Create `my-app/workflow.yaml`:

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router
    config:
      prefix: /api
    dependsOn:
      - web-server

  - name: items-api
    type: api.handler
    config:
      resourceName: items
    dependsOn:
      - router

workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: "GET"
        path: "/api/items"
        handler: items-api

      - method: "GET"
        path: "/api/items/{id}"
        handler: items-api

      - method: "POST"
        path: "/api/items"
        handler: items-api

      - method: "PUT"
        path: "/api/items/{id}"
        handler: items-api

      - method: "DELETE"
        path: "/api/items/{id}"
        handler: items-api
```

### Running and Testing

```bash
./server -config my-app/workflow.yaml
```

Test your API:

```bash
# Create an item
curl -X POST http://localhost:8080/api/items \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 9.99}'

# List items
curl http://localhost:8080/api/items

# Get by ID
curl http://localhost:8080/api/items/{id}

# Update
curl -X PUT http://localhost:8080/api/items/{id} \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget Pro", "price": 19.99}'

# Delete
curl -X DELETE http://localhost:8080/api/items/{id}
```

You now have a fully functional REST API with zero Go code. Every change from here is additive -- you layer on modules one at a time.

---

## 3. Building the Module Stack

Build your application layer by layer. Each section below adds a concern, with the YAML you need.

### HTTP Infrastructure

Every application starts with the HTTP layer.

#### http.server

The server listens on a network address. Configure timeouts for production:

```yaml
- name: web-server
  type: http.server
  config:
    address: ":8080"
    readTimeout: "30s"
    writeTimeout: "30s"
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `address` | string | `:8080` | Host:port to listen on |
| `readTimeout` | duration | - | Max time to read request (optional) |
| `writeTimeout` | duration | - | Max time to write response (optional) |

#### http.router

The router dispatches requests to handlers based on path and method. The `prefix` option scopes all routes:

```yaml
- name: router
  type: http.router
  config:
    prefix: /api
  dependsOn:
    - web-server
```

The `dependsOn` declaration ensures the router starts after the server.

#### Middleware Chain

Middleware modules wrap handlers. The order in `dependsOn` determines the initialization order, but the actual request processing order is determined by the `middlewares` list on each route (see [Wiring Workflows](#4-wiring-workflows)).

Define middleware modules:

```yaml
- name: cors
  type: http.middleware.cors
  config:
    allowedOrigins: ["*"]
    allowedMethods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    allowedHeaders: ["Content-Type", "Authorization"]
  dependsOn:
    - router

- name: request-id
  type: http.middleware.requestid
  dependsOn:
    - cors

- name: rate-limiter
  type: http.middleware.ratelimit
  config:
    requestsPerMinute: 600
    burstSize: 100
  dependsOn:
    - request-id
```

Available middleware types:

| Type | Purpose | Key Config |
|------|---------|-----------|
| `http.middleware.cors` | CORS headers | `allowedOrigins`, `allowedMethods` |
| `http.middleware.requestid` | Adds X-Request-ID | (none) |
| `http.middleware.ratelimit` | Token bucket rate limiting | `requestsPerMinute`, `burstSize` |
| `http.middleware.auth` | Token validation | `authType` (Bearer, Basic, ApiKey) |
| `http.middleware.logging` | Request/response logging | `logLevel` |
| `http.middleware.securityheaders` | Security headers (HSTS, CSP, etc.) | `frameOptions`, `hstsMaxAge` |

### Authentication

#### auth.jwt

JWT authentication handles login, registration, token signing, and verification:

```yaml
- name: auth
  type: auth.jwt
  config:
    secret: "${JWT_SECRET}"
    tokenExpiry: "24h"
    issuer: "my-app"
    seedFile: "./seed/users.json"
  dependsOn:
    - rate-limiter
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `secret` | string | (required) | JWT signing secret. Use `${ENV_VAR}` for production. |
| `tokenExpiry` | duration | `24h` | Token expiration |
| `issuer` | string | `workflow` | Token issuer claim |
| `seedFile` | string | - | Path to JSON file with initial user accounts |

#### http.middleware.auth

The auth middleware validates tokens on incoming requests. It depends on the `auth.jwt` module:

```yaml
- name: auth-middleware
  type: http.middleware.auth
  config:
    authType: Bearer
  dependsOn:
    - auth
```

#### Seed User File Format

Create `seed/users.json` with initial users:

```json
[
  {
    "id": "user-001",
    "data": {
      "email": "admin@example.com",
      "name": "Admin User",
      "role": "admin",
      "password": "changeme123"
    },
    "state": "active"
  },
  {
    "id": "user-002",
    "data": {
      "email": "user@example.com",
      "name": "Regular User",
      "role": "user",
      "password": "changeme123"
    },
    "state": "active"
  }
]
```

Each user object has `id`, `data` (arbitrary fields including `email`, `password`, `role`), and `state`.

#### Wiring Auth Routes

Auth routes (login, register) should **not** use auth-middleware (they are public). Protected routes should include it in their middleware list:

```yaml
workflows:
  http:
    routes:
      # Public: login and register
      - method: "POST"
        path: "/api/auth/login"
        handler: auth
        middlewares:
          - cors
          - request-id
          - rate-limiter

      - method: "POST"
        path: "/api/auth/register"
        handler: auth
        middlewares:
          - cors
          - request-id
          - rate-limiter

      # Protected: get/update profile
      - method: "GET"
        path: "/api/auth/profile"
        handler: auth
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware
```

### Data Layer

#### database.workflow

The workflow database module supports PostgreSQL, MySQL, and SQLite:

```yaml
- name: app-db
  type: database.workflow
  config:
    driver: "sqlite"
    dsn: "./data/my-app.db"
```

For production, switch to PostgreSQL:

```yaml
- name: app-db
  type: database.workflow
  config:
    driver: "postgres"
    dsn: "${DATABASE_URL}"
    maxOpenConns: 25
    maxIdleConns: 5
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `driver` | select | (required) | `postgres`, `mysql`, `sqlite3` |
| `dsn` | string | (required) | Connection string |
| `maxOpenConns` | number | `25` | Max open connections |
| `maxIdleConns` | number | `5` | Max idle connections |

#### persistence.store

The persistence store provides generic key-value persistence on top of a database:

```yaml
- name: persistence
  type: persistence.store
  config:
    database: "app-db"
  dependsOn:
    - app-db
```

The `database` config points to the name of your database module.

#### api.handler

The REST API handler provides full CRUD for a resource. It uses the persistence store for data and can optionally integrate with a state machine:

```yaml
- name: users-api
  type: api.handler
  config:
    resourceName: users
    seedFile: "./seed/users.json"
  dependsOn:
    - auth-middleware
    - persistence
```

| Config Key | Type | Description |
|-----------|------|-------------|
| `resourceName` | string | Resource name (used in URL paths and storage) |
| `seedFile` | string | JSON file with initial data to load on startup |
| `workflowType` | string | State machine workflow name (for lifecycle-managed resources) |
| `workflowEngine` | string | Name of the state machine engine module |
| `initialTransition` | string | Transition to trigger on resource creation |
| `instanceIDField` | string | Field name to use as the state machine instance ID |

For simple CRUD without a state machine, omit `workflowType`, `workflowEngine`, and `initialTransition`. For lifecycle-managed resources (like conversations or orders), include them.

### Business Logic

#### statemachine.engine

The state machine engine manages entity lifecycles. Declare it as a module; its states, transitions, and hooks are defined in the `workflows.statemachine` section:

```yaml
- name: order-engine
  type: statemachine.engine
  config:
    description: "Manages order lifecycle"
  dependsOn:
    - orders-api
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `maxInstances` | number | `1000` | Max concurrent workflow instances |
| `instanceTTL` | duration | `24h` | TTL for idle instances |

State definitions, transitions, and hooks are configured in the `workflows.statemachine` section (see [Wiring Workflows](#4-wiring-workflows)).

#### processing.step

Processing steps link dynamic components to state machine transitions with retry and timeout:

```yaml
- name: step-validate-order
  type: processing.step
  config:
    componentId: "order-validator"
    successTransition: "validate"
    compensateTransition: "fail"
    maxRetries: 2
    retryBackoffMs: 1000
    timeoutSeconds: 30
  dependsOn:
    - order-validator
    - order-engine
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `componentId` | string | (required) | Name of the dynamic component to execute |
| `successTransition` | string | - | Transition to trigger on success |
| `compensateTransition` | string | - | Transition to trigger on failure |
| `maxRetries` | number | `2` | Max retry attempts |
| `retryBackoffMs` | number | `1000` | Base backoff between retries (ms) |
| `timeoutSeconds` | number | `30` | Max execution time per attempt (s) |

#### dynamic.component

Dynamic components load custom Go logic at runtime. Each one points to a `.go` source file:

```yaml
- name: order-validator
  type: dynamic.component
  config:
    source: "./components/order_validator.go"
    description: "Validates order data and inventory availability"
  dependsOn:
    - order-engine
```

See [Writing Dynamic Components](#5-writing-dynamic-components) for the source file format.

### Messaging and Events

#### messaging.broker

The in-memory broker provides pub/sub within a single process:

```yaml
- name: event-broker
  type: messaging.broker
  config:
    topics:
      - "order.created"
      - "order.paid"
      - "order.shipped"
      - "order.cancelled"
  dependsOn:
    - order-engine
```

For distributed deployments, swap to `messaging.nats` or `messaging.kafka`:

```yaml
# NATS
- name: event-broker
  type: messaging.nats
  config:
    url: "nats://localhost:4222"

# Kafka
- name: event-broker
  type: messaging.kafka
  config:
    brokers: ["localhost:9092"]
    groupId: "my-app"
```

The rest of your config (subscriptions, handlers) stays the same regardless of broker implementation.

#### messaging.handler

Message handlers subscribe to topics and process events:

```yaml
- name: notification-handler
  type: messaging.handler
  config:
    topic: "order.*"
    description: "Sends notifications for order lifecycle events"
  dependsOn:
    - event-broker
```

### Observability

#### health.checker

Provides `/healthz`, `/readyz`, and `/livez` endpoints:

```yaml
- name: health
  type: health.checker
  config:
    description: "Application health probes"
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `healthPath` | string | `/healthz` | Health check path |
| `readyPath` | string | `/readyz` | Readiness probe path |
| `livePath` | string | `/livez` | Liveness probe path |
| `checkTimeout` | duration | `5s` | Per-check timeout |
| `autoDiscover` | boolean | `true` | Auto-discover HealthCheckable services |

#### metrics.collector

Exposes Prometheus metrics at `/metrics`:

```yaml
- name: metrics
  type: metrics.collector
  config:
    namespace: my_app
    subsystem: api
```

| Config Key | Type | Default | Description |
|-----------|------|---------|-------------|
| `namespace` | string | `workflow` | Prometheus namespace prefix |
| `subsystem` | string | - | Prometheus subsystem |
| `metricsPath` | string | `/metrics` | Scrape endpoint path |
| `enabledMetrics` | array | `[workflow, http, module, active_workflows]` | Metric groups to register |

---

## 4. Wiring Workflows

A Workflow config has three top-level sections: `modules`, `workflows`, and `triggers`. The `modules` section declares **what** exists. The `workflows` section declares **how they connect**. The `triggers` section declares **what starts execution**.

### HTTP Workflows

HTTP workflows define routes and their middleware chains.

```yaml
workflows:
  http:
    router: router          # Name of the http.router module
    server: web-server      # Name of the http.server module
    routes:
      - method: "GET"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware
```

#### Route Definitions

Each route specifies:

| Field | Required | Description |
|-------|:--------:|-------------|
| `method` | Yes | HTTP method (GET, POST, PUT, DELETE, PATCH) |
| `path` | Yes | URL path, supports `{param}` placeholders |
| `handler` | Yes | Name of the handler module |
| `middlewares` | No | Ordered list of middleware module names |

Path parameters use curly braces: `/api/items/{id}`, `/api/items/{id}/comments/{commentId}`.

#### Middleware Chain Ordering

Middlewares execute in the order listed, outermost first. The request passes through each middleware before reaching the handler, and the response passes back through in reverse:

```
Request -->  cors  -->  request-id  -->  rate-limiter  -->  auth-middleware  -->  Handler
Response <-- cors  <--  request-id  <--  rate-limiter  <--  auth-middleware  <--  Handler
```

Typical ordering:

1. `cors` -- must be first to handle preflight OPTIONS requests
2. `request-id` -- adds tracing ID early so downstream middleware can use it
3. `rate-limiter` -- reject excess traffic before doing expensive auth work
4. `auth-middleware` -- validate tokens last (only authenticated requests reach the handler)

**Public vs. protected routes**: Simply omit `auth-middleware` from public routes:

```yaml
routes:
  # Public: no auth-middleware
  - method: "POST"
    path: "/api/auth/login"
    handler: auth
    middlewares: [cors, request-id, rate-limiter]

  # Public: webhook endpoint
  - method: "POST"
    path: "/api/webhooks/stripe"
    handler: webhooks-api
    middlewares: [cors, request-id, rate-limiter]

  # Protected: requires valid JWT
  - method: "GET"
    path: "/api/orders"
    handler: orders-api
    middlewares: [cors, request-id, rate-limiter, auth-middleware]
```

#### Route Pipelines (CQRS)

For advanced request processing, use `api.query` and `api.command` handlers with inline pipeline steps:

```yaml
- name: order-queries
  type: api.query
  config:
    delegate: orders-api
    routes:
      - path: "/api/orders/search"
        method: "GET"
        pipeline:
          - step: validate
            config:
              strategy: required_fields
              required_fields: [status]
          - step: delegate
            config:
              service: orders-api
```

### State Machine Workflows

State machines model entity lifecycles. Define them in `workflows.statemachine`:

```yaml
workflows:
  statemachine:
    engine: order-engine          # Name of the statemachine.engine module
    resourceMappings:
      - resourceType: "orders"    # Maps api.handler resourceName
        stateMachine: "order-engine"
        instanceIDKey: "id"       # Field in the resource used as instance ID
    definitions:
      - name: order-processing
        description: "Order processing lifecycle"
        initialState: "received"
        states:
          received:
            description: "Order has been received"
            isFinal: false
            isError: false
          validated:
            description: "Order data validated"
            isFinal: false
            isError: false
          paid:
            description: "Payment confirmed"
            isFinal: false
            isError: false
          shipped:
            description: "Order shipped"
            isFinal: true
            isError: false
          cancelled:
            description: "Order cancelled"
            isFinal: true
            isError: false
          failed:
            description: "Processing failed"
            isFinal: true
            isError: true
        transitions:
          validate:
            fromState: "received"
            toState: "validated"
            autoTransform: true     # Happens automatically on creation
          pay:
            fromState: "validated"
            toState: "paid"
          ship:
            fromState: "paid"
            toState: "shipped"
          cancel_validated:
            fromState: "validated"
            toState: "cancelled"
          cancel_paid:
            fromState: "paid"
            toState: "cancelled"
    hooks:
      - workflowType: "order-processing"
        transitions: ["validate"]
        handler: "step-validate-order"
      - workflowType: "order-processing"
        transitions: ["pay"]
        handler: "step-process-payment"
      - workflowType: "order-processing"
        transitions: ["ship"]
        handler: "step-ship-order"
      - workflowType: "order-processing"
        toStates: ["paid", "shipped", "cancelled", "failed"]
        handler: "notification-handler"
```

#### State Definitions

Each state has:

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Human-readable state description |
| `isFinal` | boolean | If `true`, no further transitions are allowed |
| `isError` | boolean | If `true`, this is an error/failure terminal state |

#### Transitions

Each transition defines a named edge between states:

| Field | Type | Description |
|-------|------|-------------|
| `fromState` | string | Source state |
| `toState` | string | Target state |
| `autoTransform` | boolean | If `true`, transition executes automatically (no external trigger needed) |

#### Hooks

Hooks bind processing steps or handlers to transitions:

| Field | Type | Description |
|-------|------|-------------|
| `workflowType` | string | Name of the state machine definition |
| `transitions` | array | List of transition names to hook into |
| `toStates` | array | Alternative: fire when entering any of these states |
| `handler` | string | Name of the processing step or handler module |

You can hook by transition name (`transitions`) or by destination state (`toStates`). Use `toStates` for cross-cutting concerns like notifications that should fire on every state change.

#### Resource Mappings

Resource mappings connect an `api.handler` to a state machine, so CRUD operations automatically create and manage workflow instances:

```yaml
resourceMappings:
  - resourceType: "orders"          # Matches api.handler.config.resourceName
    stateMachine: "order-engine"    # Matches statemachine.engine module name
    instanceIDKey: "id"             # Field in the resource data used as instance ID
```

When the `api.handler` has `workflowType` and `workflowEngine` configured, creating a resource through the API automatically creates a state machine instance and triggers the initial transition.

### Messaging Workflows

Messaging workflows define topic subscriptions:

```yaml
workflows:
  messaging:
    broker: event-broker        # Name of the messaging.broker module
    subscriptions:
      - topic: "order.created"
        handler: notification-handler
      - topic: "order.paid"
        handler: notification-handler
      - topic: "order.shipped"
        handler: notification-handler
      - topic: "order.cancelled"
        handler: notification-handler
```

The broker name must match a declared `messaging.broker` (or `messaging.nats` / `messaging.kafka`) module. Each subscription binds a topic pattern to a handler module.

Wildcard patterns (e.g., `order.*`) match any topic starting with the prefix:

```yaml
subscriptions:
  - topic: "order.*"
    handler: notification-handler
```

---

## 5. Writing Dynamic Components

Dynamic components are single `.go` files that implement custom business logic. They are loaded at runtime by the Yaegi interpreter and can be hot-reloaded without restarting the engine.

### File Structure

Each component is a single `.go` file in your `components/` directory:

```
my-app/
  components/
    order_validator.go
    payment_processor.go
    inventory_checker.go
```

### Required Interface

Every dynamic component must be in `package component` and implement the `Execute` function:

```go
//go:build ignore

package component

import (
    "context"
    "fmt"
    "time"
)

// Name returns the unique identifier for this component.
func Name() string {
    return "order-validator"
}

// Contract describes the inputs and outputs for documentation and validation.
func Contract() map[string]interface{} {
    return map[string]interface{}{
        "required_inputs": map[string]interface{}{
            "items": map[string]interface{}{
                "type":        "array",
                "description": "List of order items",
            },
        },
        "optional_inputs": map[string]interface{}{},
        "outputs": map[string]interface{}{
            "valid": map[string]interface{}{
                "type":        "bool",
                "description": "Whether the order is valid",
            },
            "errors": map[string]interface{}{
                "type":        "array",
                "description": "Validation error messages",
            },
        },
    }
}

// Init is called once when the component is loaded. Use it to set up
// connections or state using available services.
func Init(services map[string]interface{}) error {
    return nil
}

// Start is called when the engine starts. Use it for background goroutines.
func Start(ctx context.Context) error {
    return nil
}

// Stop is called when the engine stops. Clean up resources here.
func Stop(ctx context.Context) error {
    return nil
}

// Execute is the main entry point. It receives input data and returns
// output data. This is called for each processing step invocation.
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
    items, ok := params["items"].([]interface{})
    if !ok || len(items) == 0 {
        return nil, fmt.Errorf("missing required parameter: items")
    }

    var errors []string

    for i, item := range items {
        itemMap, ok := item.(map[string]interface{})
        if !ok {
            errors = append(errors, fmt.Sprintf("item %d: invalid format", i))
            continue
        }
        if _, ok := itemMap["name"]; !ok {
            errors = append(errors, fmt.Sprintf("item %d: missing name", i))
        }
        if _, ok := itemMap["quantity"]; !ok {
            errors = append(errors, fmt.Sprintf("item %d: missing quantity", i))
        }
    }

    return map[string]interface{}{
        "valid":     len(errors) == 0,
        "errors":    errors,
        "timestamp": time.Now().UTC().Format(time.RFC3339),
    }, nil
}
```

Key points about the interface:

- **`//go:build ignore`** -- Required at the top. Prevents `go build` from compiling the file directly.
- **`package component`** -- Must use this package name.
- **`Name()`** -- Returns the component identifier. Must match the `componentId` in your processing step config.
- **`Contract()`** -- Documents inputs and outputs. Used for validation and UI.
- **`Init(services)`** -- Receives a map of available services. Called once on load.
- **`Start(ctx)`** / **`Stop(ctx)`** -- Lifecycle hooks. Start background work or clean up.
- **`Execute(ctx, params)`** -- The main function. Receives a `map[string]interface{}` of input data and returns a `map[string]interface{}` of output data.

### Sandbox Restrictions

Dynamic components run in a sandbox with these restrictions:

- **Standard library only** -- You can import any Go stdlib package (`fmt`, `strings`, `time`, `encoding/json`, `net/http`, `crypto/*`, etc.)
- **No external packages** -- You cannot import third-party modules
- **No file system writes** -- Read-only filesystem access
- **No os.Exit** -- The component cannot terminate the process

These restrictions exist for security. If you need external packages, implement the logic as a built-in module instead.

### Hot-Reload

Dynamic components support true hot-reload:

1. Edit the `.go` file and save
2. The file watcher detects the change (500ms debounce)
3. The new source is validated and compiled
4. The old component is swapped for the new one
5. No engine restart -- other modules keep running

You can also reload via the API:

```bash
# Update a component programmatically
curl -X PUT http://localhost:8080/api/dynamic/components/order-validator \
  -H "Content-Type: text/plain" \
  --data-binary @components/order_validator.go
```

### Testing Dynamic Components Outside the Engine

Since dynamic components are standard Go files (with the `//go:build ignore` tag), you can test them by creating a test file that imports the functions:

```go
package component_test

import (
    "context"
    "testing"
)

func TestOrderValidator(t *testing.T) {
    // Call Init with empty services
    if err := Init(nil); err != nil {
        t.Fatalf("Init failed: %v", err)
    }

    // Test with valid input
    result, err := Execute(context.Background(), map[string]interface{}{
        "items": []interface{}{
            map[string]interface{}{"name": "Widget", "quantity": 2},
        },
    })
    if err != nil {
        t.Fatalf("Execute failed: %v", err)
    }
    if valid, ok := result["valid"].(bool); !ok || !valid {
        t.Errorf("expected valid=true, got %v", result["valid"])
    }

    // Test with missing items
    _, err = Execute(context.Background(), map[string]interface{}{})
    if err == nil {
        t.Error("expected error for missing items")
    }
}
```

---

## 6. Setting Up Triggers

Triggers start workflow execution. They live in the `triggers` section of your config.

### HTTP Triggers

HTTP triggers map incoming requests to workflow actions. When a request arrives at a trigger path, the engine dispatches it to the named workflow and action:

```yaml
triggers:
  http:
    routes:
      - path: "/api/webhooks/stripe"
        method: "POST"
        workflow: "order-processing"
        action: "pay"

      - path: "/api/webhooks/shipping"
        method: "POST"
        workflow: "order-processing"
        action: "ship"
```

| Field | Description |
|-------|-------------|
| `path` | URL path that triggers the workflow |
| `method` | HTTP method (POST, PUT, etc.) |
| `workflow` | Name of the state machine definition |
| `action` | Transition name to trigger |

This is particularly useful for webhooks from external services (payment processors, shipping providers, SMS gateways).

### Event Triggers

Event triggers subscribe to EventBus events and map them to workflow actions:

```yaml
triggers:
  event:
    subscriptions:
      - topic: "order.created"
        event: "order.created"
        workflow: "order-processing"
        action: "validate"

      - topic: "payment.confirmed"
        event: "payment.confirmed"
        workflow: "order-processing"
        action: "pay"
```

| Field | Description |
|-------|-------------|
| `topic` | EventBus topic to subscribe to |
| `event` | Event type filter |
| `workflow` | Target workflow definition |
| `action` | Transition to trigger |

### Schedule Triggers

Schedule triggers use cron expressions to trigger workflows on a timer:

```yaml
triggers:
  schedule:
    jobs:
      - cron: "0 0 * * *"            # Daily at midnight
        workflow: "cleanup"
        action: "run"

      - cron: "*/5 * * * *"          # Every 5 minutes
        workflow: "health-monitor"
        action: "check"
```

| Field | Description |
|-------|-------------|
| `cron` | Standard cron expression (5 fields: min hour day month weekday) |
| `workflow` | Target workflow |
| `action` | Action to trigger |

---

## 7. Deployment

For full deployment lifecycle details, see [APPLICATION_LIFECYCLE.md](APPLICATION_LIFECYCLE.md). This section provides a quick-start overview.

### Development

```bash
export JWT_SECRET="dev-secret-change-in-production"
./server -config my-app/workflow.yaml
```

### Docker

Create a `Dockerfile` in your project:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /build/server .
COPY my-app/ ./config/
USER nonroot
EXPOSE 8080
CMD ["./server", "-config", "config/workflow.yaml"]
```

Build and run:

```bash
docker build -t my-app .
docker run -p 8080:8080 -e JWT_SECRET=your-secret my-app
```

### Docker Compose with Observability

```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - JWT_SECRET=${JWT_SECRET}
    volumes:
      - app-data:/app/data
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin

volumes:
  app-data:
```

### Environment Variables for Secrets

Never hardcode secrets in YAML. Use `${ENV_VAR}` syntax:

```yaml
- name: auth
  type: auth.jwt
  config:
    secret: "${JWT_SECRET}"

- name: app-db
  type: database.workflow
  config:
    driver: "postgres"
    dsn: "${DATABASE_URL}"
```

The engine expands environment variables at config load time.

### Database Choice

| Environment | Driver | DSN Example |
|-------------|--------|-------------|
| Development | `sqlite` | `./data/my-app.db` |
| Staging | `postgres` | `postgres://user:pass@staging-db:5432/myapp?sslmode=require` |
| Production | `postgres` | `${DATABASE_URL}` |

Switching from SQLite to PostgreSQL requires changing two config fields (`driver` and `dsn`). No code changes. No schema migrations -- the persistence store handles this automatically.

---

## 8. Using the Visual Builder

The Workflow engine includes an embedded React-based visual builder for designing module graphs, drawing connections, and configuring modules through a graphical interface.

### Accessing the UI

When running the server with the admin configuration, the visual builder is available at the root URL (typically `http://localhost:8080/`). The UI is embedded in the server binary from the `module/ui_dist/` directory.

### Adding Modules from the Palette

The left panel displays all available module types grouped by category (HTTP, Middleware, Messaging, State Machine, Pipeline, etc.). Click or drag a module type onto the canvas to add an instance.

### Drawing Connections

Click on a module's output port and drag to another module's input port to create a dependency edge. This is equivalent to adding an entry in the `dependsOn` list in YAML.

### Configuring Modules in the Property Panel

Select a module on the canvas to open its property panel on the right. The panel shows all available configuration fields for that module type, with:

- Required fields marked with an indicator
- Default values pre-populated
- Sensitive fields (secrets, DSNs) rendered as password inputs
- Select fields rendered as dropdowns
- Array fields with add/remove controls

### Pipeline Step Composition

For pipeline-based workflows, drag step modules (`step.validate`, `step.transform`, `step.set`, `step.publish`, etc.) onto the canvas and connect them in sequence. The visual layout represents the processing order.

### Importing and Exporting YAML

- **Import**: Use the import function to load an existing YAML configuration onto the canvas
- **Export**: The builder generates valid YAML from the current canvas state, which you can save to a file and run with the server

---

## 9. Common Patterns

### CRUD API with Auth

The most common pattern: a REST API with JWT authentication and a middleware chain.

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router
    config:
      prefix: /api
    dependsOn: [web-server]

  - name: cors
    type: http.middleware.cors
    config:
      allowedOrigins: ["http://localhost:3000"]
      allowedMethods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
      allowedHeaders: ["Content-Type", "Authorization"]
    dependsOn: [router]

  - name: request-id
    type: http.middleware.requestid
    dependsOn: [cors]

  - name: rate-limiter
    type: http.middleware.ratelimit
    config:
      requestsPerMinute: 300
      burstSize: 50
    dependsOn: [request-id]

  - name: auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"
      tokenExpiry: "24h"
      issuer: "my-app"
      seedFile: "./seed/users.json"
    dependsOn: [rate-limiter]

  - name: auth-middleware
    type: http.middleware.auth
    config:
      authType: Bearer
    dependsOn: [auth]

  - name: app-db
    type: database.workflow
    config:
      driver: "sqlite"
      dsn: "./data/app.db"

  - name: persistence
    type: persistence.store
    config:
      database: "app-db"
    dependsOn: [app-db]

  - name: products-api
    type: api.handler
    config:
      resourceName: products
      seedFile: "./seed/products.json"
    dependsOn: [auth-middleware, persistence]

  - name: health
    type: health.checker

  - name: metrics
    type: metrics.collector
    config:
      namespace: my_app

workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: "POST"
        path: "/api/auth/login"
        handler: auth
        middlewares: [cors, request-id, rate-limiter]

      - method: "POST"
        path: "/api/auth/register"
        handler: auth
        middlewares: [cors, request-id, rate-limiter]

      - method: "GET"
        path: "/api/products"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "GET"
        path: "/api/products/{id}"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "POST"
        path: "/api/products"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "PUT"
        path: "/api/products/{id}"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "DELETE"
        path: "/api/products/{id}"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
```

### Event-Sourced Microservice

State machine with event broker and processing steps for each transition:

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router
    dependsOn: [web-server]

  - name: app-db
    type: database.workflow
    config:
      driver: "sqlite"
      dsn: "./data/orders.db"

  - name: persistence
    type: persistence.store
    config:
      database: "app-db"
    dependsOn: [app-db]

  - name: orders-api
    type: api.handler
    config:
      resourceName: orders
      workflowType: "order-lifecycle"
      workflowEngine: "order-engine"
      initialTransition: "submit"
      instanceIDField: "id"
    dependsOn: [router, persistence]

  - name: order-engine
    type: statemachine.engine
    dependsOn: [orders-api]

  - name: order-validator
    type: dynamic.component
    config:
      source: "./components/order_validator.go"
    dependsOn: [order-engine]

  - name: payment-processor
    type: dynamic.component
    config:
      source: "./components/payment_processor.go"
    dependsOn: [order-engine]

  - name: step-validate
    type: processing.step
    config:
      componentId: "order-validator"
      successTransition: "submit"
      maxRetries: 1
      timeoutSeconds: 10
    dependsOn: [order-validator, order-engine]

  - name: step-pay
    type: processing.step
    config:
      componentId: "payment-processor"
      successTransition: "confirm_payment"
      compensateTransition: "fail"
      maxRetries: 3
      retryBackoffMs: 2000
      timeoutSeconds: 30
    dependsOn: [payment-processor, order-engine]

  - name: event-broker
    type: messaging.broker
    config:
      topics:
        - "order.submitted"
        - "order.paid"
        - "order.shipped"
        - "order.failed"
    dependsOn: [order-engine]

  - name: notification-handler
    type: messaging.handler
    config:
      topic: "order.*"
    dependsOn: [event-broker]

  - name: health
    type: health.checker

workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: "POST"
        path: "/api/orders"
        handler: orders-api

      - method: "GET"
        path: "/api/orders"
        handler: orders-api

      - method: "GET"
        path: "/api/orders/{id}"
        handler: orders-api

  statemachine:
    engine: order-engine
    resourceMappings:
      - resourceType: "orders"
        stateMachine: "order-engine"
        instanceIDKey: "id"
    definitions:
      - name: order-lifecycle
        initialState: "new"
        states:
          new:
            description: "Order created"
            isFinal: false
            isError: false
          submitted:
            description: "Order validated and submitted"
            isFinal: false
            isError: false
          paid:
            description: "Payment confirmed"
            isFinal: false
            isError: false
          shipped:
            description: "Order shipped"
            isFinal: true
            isError: false
          failed:
            description: "Processing failed"
            isFinal: true
            isError: true
        transitions:
          submit:
            fromState: "new"
            toState: "submitted"
            autoTransform: true
          confirm_payment:
            fromState: "submitted"
            toState: "paid"
          ship:
            fromState: "paid"
            toState: "shipped"
          fail:
            fromState: "submitted"
            toState: "failed"
    hooks:
      - workflowType: "order-lifecycle"
        transitions: ["submit"]
        handler: "step-validate"
      - workflowType: "order-lifecycle"
        transitions: ["confirm_payment"]
        handler: "step-pay"
      - workflowType: "order-lifecycle"
        toStates: ["submitted", "paid", "shipped", "failed"]
        handler: "notification-handler"

  messaging:
    broker: event-broker
    subscriptions:
      - topic: "order.*"
        handler: notification-handler

triggers:
  http:
    routes:
      - path: "/api/webhooks/payment"
        method: "POST"
        workflow: "order-lifecycle"
        action: "confirm_payment"
      - path: "/api/webhooks/shipping"
        method: "POST"
        workflow: "order-lifecycle"
        action: "ship"
```

### API Gateway

Reverse proxy with middleware chain and rate limiting:

```yaml
modules:
  - name: gateway-server
    type: http.server
    config:
      address: ":8080"

  - name: gateway-router
    type: http.router
    dependsOn: [gateway-server]

  - name: cors
    type: http.middleware.cors
    config:
      allowedOrigins: ["*"]
    dependsOn: [gateway-router]

  - name: request-id
    type: http.middleware.requestid
    dependsOn: [cors]

  - name: rate-limiter
    type: http.middleware.ratelimit
    config:
      requestsPerMinute: 1000
      burstSize: 200
    dependsOn: [request-id]

  - name: proxy
    type: http.simple_proxy
    config:
      targets:
        /api/users: "http://users-service:8080"
        /api/orders: "http://orders-service:8080"
        /api/products: "http://products-service:8080"
    dependsOn: [rate-limiter]

  - name: health
    type: health.checker

  - name: metrics
    type: metrics.collector
    config:
      namespace: api_gateway

workflows:
  http:
    router: gateway-router
    server: gateway-server
    routes:
      - method: "GET"
        path: "/api/users/*"
        handler: proxy
        middlewares: [cors, request-id, rate-limiter]

      - method: "POST"
        path: "/api/users/*"
        handler: proxy
        middlewares: [cors, request-id, rate-limiter]

      - method: "GET"
        path: "/api/orders/*"
        handler: proxy
        middlewares: [cors, request-id, rate-limiter]

      - method: "POST"
        path: "/api/orders/*"
        handler: proxy
        middlewares: [cors, request-id, rate-limiter]

      - method: "GET"
        path: "/api/products/*"
        handler: proxy
        middlewares: [cors, request-id, rate-limiter]
```

### Scheduled Batch Processing

Processing step executed on a cron schedule:

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"

  - name: scheduler
    type: scheduler.modular

  - name: cleanup-job
    type: dynamic.component
    config:
      source: "./components/data_cleanup.go"
      description: "Removes expired records and anonymizes old data"

  - name: step-cleanup
    type: processing.step
    config:
      componentId: "cleanup-job"
      maxRetries: 3
      retryBackoffMs: 5000
      timeoutSeconds: 300
    dependsOn: [cleanup-job]

  - name: report-job
    type: dynamic.component
    config:
      source: "./components/report_generator.go"
      description: "Generates daily usage reports"

  - name: step-report
    type: processing.step
    config:
      componentId: "report-job"
      maxRetries: 2
      retryBackoffMs: 10000
      timeoutSeconds: 600
    dependsOn: [report-job]

  - name: health
    type: health.checker

workflows:
  scheduler:
    jobs:
      - scheduler: scheduler
        job: step-cleanup

      - scheduler: scheduler
        job: step-report

triggers:
  schedule:
    jobs:
      - cron: "0 2 * * *"
        workflow: "cleanup"
        action: "run"

      - cron: "0 6 * * *"
        workflow: "reports"
        action: "generate"
```

### Multi-Provider Messaging

The Chat Platform pattern: multiple dynamic components per provider with a router that dispatches based on provider type:

```yaml
modules:
  # Provider components
  - name: twilio-provider
    type: dynamic.component
    config:
      source: "./components/twilio_provider.go"
      description: "Twilio SMS provider"
    dependsOn: [conversation-engine]

  - name: aws-provider
    type: dynamic.component
    config:
      source: "./components/aws_provider.go"
      description: "AWS SNS/Pinpoint SMS provider"
    dependsOn: [conversation-engine]

  - name: webchat-handler
    type: dynamic.component
    config:
      source: "./components/webchat_handler.go"
      description: "Browser-based webchat handler"
    dependsOn: [conversation-engine]

  # Router component
  - name: conversation-router
    type: dynamic.component
    config:
      source: "./components/conversation_router.go"
      description: "Routes messages to the correct provider based on channel"
    dependsOn: [conversation-engine]

  # Processing steps for routing
  - name: step-route-message
    type: processing.step
    config:
      componentId: "conversation-router"
      successTransition: "route_message"
      maxRetries: 1
      timeoutSeconds: 10
    dependsOn:
      - conversation-router
      - twilio-provider
      - aws-provider
      - webchat-handler
      - conversation-engine
```

The `conversation-router` dynamic component inspects the incoming message, determines the channel (SMS/Twilio, SMS/AWS, webchat), and delegates to the appropriate provider component. Each provider is a standalone dynamic component that can be hot-reloaded independently.

---

## 10. Troubleshooting

### Config Validation Errors

Use the `wfctl` CLI to validate your config before running:

```bash
./wfctl validate my-app/workflow.yaml
```

Common validation errors:

| Error | Cause | Fix |
|-------|-------|-----|
| `unknown module type "X"` | Typo in the `type` field | Check `schema/module_schema.go` for valid types |
| `module "X" depends on unknown module "Y"` | `dependsOn` references a module name that does not exist | Fix the module name in `dependsOn` |
| `required field "address" missing` | A required config field is not set | Add the missing field to `config:` |
| `circular dependency detected` | Modules depend on each other in a cycle | Restructure dependencies to break the cycle |
| `duplicate module name "X"` | Two modules have the same `name` | Rename one of them |

### Module Dependency Resolution Order

Modules start in topological order based on `dependsOn` declarations. If module A depends on B, B starts first. When you see startup failures, check:

1. **Is every dependency declared?** If module A uses a service from module B, A must list B in `dependsOn`.
2. **Are database modules listed before persistence stores?** The persistence store needs the database to be ready.
3. **Are auth modules listed before auth middleware?** The middleware needs the auth service to validate tokens.

You can inspect the dependency graph:

```bash
./wfctl inspect my-app/workflow.yaml
```

### Checking Health Endpoints

The `health.checker` module exposes three endpoints:

```bash
# Overall health (200 = healthy, 503 = unhealthy)
curl http://localhost:8080/healthz

# Readiness (200 = ready to serve traffic, 503 = not ready)
curl http://localhost:8080/readyz

# Liveness (200 = process is alive, 503 = should be restarted)
curl http://localhost:8080/livez
```

If health checks fail, the response body contains details about which checks failed and why.

### Metrics

The `metrics.collector` module exposes Prometheus metrics:

```bash
curl http://localhost:8080/metrics
```

Key metrics to watch:

- `workflow_http_requests_total` -- Request count by method, path, and status
- `workflow_http_request_duration_seconds` -- Request latency histogram
- `workflow_active_workflows` -- Currently active workflow instances
- `workflow_module_status` -- Module health status

### Server Log Output

The server logs to stderr with structured logging. Key log entries to look for:

```
# Successful startup
INFO starting workflow engine config=my-app/workflow.yaml
INFO module started name=web-server type=http.server
INFO module started name=router type=http.router
INFO listening on :8080

# Config errors
ERROR failed to build engine error="unknown module type: http.servers"

# Runtime errors
ERROR handler failed module=orders-api error="persistence store not available"
WARN rate limit exceeded client=192.168.1.100 limit=300

# Dynamic component reload
INFO component reloaded name=order-validator source=components/order_validator.go
```

### Common Mistakes

**Missing `dependsOn`**: The most common mistake. If a module needs a service from another module, it must declare the dependency:

```yaml
# Wrong: persistence-store cannot find database
- name: persistence
  type: persistence.store
  config:
    database: "app-db"

# Right: explicit dependency
- name: persistence
  type: persistence.store
  config:
    database: "app-db"
  dependsOn:
    - app-db
```

**Wrong module type name**: Module types use dots as separators and are case-sensitive:

```yaml
# Wrong
type: HttpServer
type: http-server
type: http_server

# Right
type: http.server
```

**Invalid middleware order**: Auth middleware must come after the auth module in the dependency chain, and CORS must be the outermost middleware:

```yaml
# Wrong: auth-middleware before cors
middlewares:
  - auth-middleware
  - cors
  - rate-limiter

# Right: cors first, auth last
middlewares:
  - cors
  - request-id
  - rate-limiter
  - auth-middleware
```

**Missing environment variables**: If your config uses `${JWT_SECRET}` but the environment variable is not set, the value will be empty at runtime:

```bash
# Set required environment variables before running
export JWT_SECRET="your-secret-here"
export DATABASE_URL="postgres://..."
./server -config my-app/workflow.yaml
```

**Workflow definition name mismatch**: The `workflowType` in an `api.handler` must exactly match the `name` in `workflows.statemachine.definitions`:

```yaml
# In modules section
- name: orders-api
  type: api.handler
  config:
    workflowType: "order-lifecycle"    # Must match definition name below

# In workflows section
workflows:
  statemachine:
    definitions:
      - name: order-lifecycle          # Must match workflowType above
        initialState: "new"
```

**Hook handler name mismatch**: The `handler` in a state machine hook must match the `name` of a processing step or messaging handler module:

```yaml
# In modules section
- name: step-validate                  # This name...
  type: processing.step

# In workflows section
hooks:
  - workflowType: "order-lifecycle"
    transitions: ["submit"]
    handler: "step-validate"           # ...must match here
```

---

## Next Steps

- Browse the [example/](../example/) directory for 30+ working configurations
- Read [MODULE_BEST_PRACTICES.md](MODULE_BEST_PRACTICES.md) for module development conventions
- See [APPLICATION_LIFECYCLE.md](APPLICATION_LIFECYCLE.md) for scaling and platform deployment
- Explore the [Chat Platform walkthrough](tutorials/chat-platform-walkthrough.md) for a full case study
- Use `./wfctl inspect` to visualize the dependency graph of any config
