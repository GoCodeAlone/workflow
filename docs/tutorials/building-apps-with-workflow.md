# Building Applications with the Workflow Engine

A hands-on tutorial for developers who want to build real applications using the Workflow engine. This guide walks through every major capability of the engine, starting from a minimal REST API and progressing through authentication, persistence, state machines, event-driven patterns, pipeline-native routes, dynamic components, and production deployment.

Throughout this guide, the [Chat Platform example](../../example/chat-platform/) serves as the reference architecture. It is a production-grade mental health support platform built entirely from YAML configuration and dynamic Go components -- no custom server code required.

**Prerequisites**: Go 1.25+, the workflow server binary, and a text editor.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Core Concepts](#2-core-concepts)
3. [Your First Application (Step-by-Step)](#3-your-first-application-step-by-step)
4. [Adding Business Logic with Pipelines](#4-adding-business-logic-with-pipelines)
5. [State Machine Workflows](#5-state-machine-workflows)
6. [Event-Driven Patterns](#6-event-driven-patterns)
7. [Dynamic Components](#7-dynamic-components)
8. [Real-World Case Study: Chat Platform](#8-real-world-case-study-chat-platform)
9. [Deploying and Managing Your Application](#9-deploying-and-managing-your-application)
10. [Next Steps](#10-next-steps)

---

## 1. Introduction

### What the Workflow Engine Does

The Workflow engine turns YAML configuration files into running applications. You declare what modules your application needs (HTTP server, router, database, authentication, state machine, message broker, etc.), how they connect (routes, middleware chains, event subscriptions), and what triggers execution (HTTP requests, events, cron schedules). The engine handles the rest: instantiation, dependency injection, lifecycle management, and graceful shutdown.

No Go code is required for infrastructure. You write Go only for domain-specific business logic, and even that gets loaded at runtime as dynamic components.

```
YAML Configuration
       |
       v
 +------------------+
 |  Workflow Engine  |  Loads config, creates modules, wires dependencies,
 |  (server binary)  |  registers routes, starts triggers
 +------------------+
       |
       v
 Running Application
 (HTTP server, routes, auth, DB, state machine, messaging, metrics)
```

### When to Use It

The Workflow engine is a good fit when you are building:

- **REST API servers** -- CRUD endpoints with authentication, rate limiting, and persistence
- **Event-driven pipelines** -- Webhook ingestion, data transformation, event routing
- **Multi-service platforms** -- Multiple APIs behind a gateway, each with their own concerns
- **Stateful applications** -- Entities with lifecycle management (orders, tickets, conversations, approvals)
- **Chat and messaging systems** -- Real-time message routing, multi-provider integration, conversation state tracking
- **Admin and management platforms** -- CRUD for organizational hierarchies, workflow management, user administration

It is not a good fit for:

- CPU-intensive computation (image processing, ML inference) -- use those as external services and call them from a dynamic component
- Applications that require fine-grained control over the HTTP response lifecycle (streaming, SSE, WebSockets) -- these need custom module implementations

### Prerequisites

1. **Go 1.25+** installed on your machine
2. **The workflow repository** cloned and built:

```bash
git clone https://github.com/GoCodeAlone/workflow.git
cd workflow
go build -o server ./cmd/server
```

The `server` binary is the only executable you need. Everything else is YAML config files, seed data, and optional dynamic component source files.

3. **Optional**: Node.js 18+ if you want to use the visual workflow builder UI
4. **Optional**: `golangci-lint` if you plan to contribute custom modules

---

## 2. Core Concepts

Four concepts define every Workflow application: **modules**, **workflows**, **triggers**, and **pipeline steps**.

### Modules

Modules are the building blocks of your application. Each module has a name, a type, optional configuration, and optional dependencies.

```yaml
modules:
  - name: web-server          # Unique instance name (kebab-case)
    type: http.server          # Module type from the registry (50+ built-in)
    config:                    # Type-specific configuration
      address: ":8080"
      readTimeout: "30s"
      writeTimeout: "30s"

  - name: router
    type: http.router
    config:
      prefix: /api
    dependsOn:                 # Modules that must initialize first
      - web-server
```

**Module types** are organized by category:

| Category | Types |
|----------|-------|
| HTTP | `http.server`, `http.router`, `http.handler`, `http.simple_proxy`, `static.fileserver` |
| Middleware | `http.middleware.cors`, `http.middleware.requestid`, `http.middleware.ratelimit`, `http.middleware.auth`, `http.middleware.logging`, `http.middleware.securityheaders` |
| Auth | `auth.jwt`, `http.middleware.auth` |
| API | `api.handler`, `api.query`, `api.command` |
| Database | `database.workflow` (SQLite/PostgreSQL/MySQL) |
| Persistence | `persistence.store` |
| State Machine | `statemachine.engine`, `state.tracker`, `state.connector` |
| Messaging | `messaging.broker` (in-memory), `messaging.nats`, `messaging.kafka` |
| Processing | `processing.step`, `dynamic.component` |
| Observability | `health.checker`, `metrics.collector`, `log.collector` |
| Scheduling | `scheduler.modular` |
| Secrets | `secrets.vault`, `secrets.aws` |
| Storage | `storage.s3`, `storage.gcs`, `storage.local`, `storage.sqlite` |

The `dependsOn` field establishes initialization order. A module's dependencies are guaranteed to be fully configured and started before the module itself.

### Workflows

Workflows define how modules connect. They live in the `workflows` section of your YAML config and come in four types:

**HTTP workflows** map HTTP routes to handler modules with middleware chains:

```yaml
workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: "GET"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - auth-middleware
```

**State machine workflows** define entity lifecycles with states, transitions, and hooks:

```yaml
workflows:
  statemachine:
    engine: order-engine
    definitions:
      - name: order-lifecycle
        initialState: "new"
        states:
          new: { description: "Created", isFinal: false, isError: false }
          paid: { description: "Payment confirmed", isFinal: false, isError: false }
          shipped: { description: "Shipped", isFinal: true, isError: false }
        transitions:
          pay: { fromState: "new", toState: "paid" }
          ship: { fromState: "paid", toState: "shipped" }
```

**Messaging workflows** define pub/sub topic subscriptions:

```yaml
workflows:
  messaging:
    broker: event-broker
    subscriptions:
      - topic: "order.*"
        handler: notification-handler
```

**Pipelines** define ordered step sequences triggered by HTTP requests or events:

```yaml
pipelines:
  create-order:
    trigger:
      type: http
      config:
        path: /api/orders
        method: POST
    steps:
      - name: validate
        type: step.validate
        config:
          strategy: required_fields
          required_fields: [customer_id, items]
      - name: prepare
        type: step.set
        config:
          values:
            id: "{{ uuidv4 }}"
            status: pending
```

### Triggers

Triggers are what starts execution. They map external events to workflow actions.

**HTTP triggers** fire when specific endpoints receive requests:

```yaml
triggers:
  http:
    routes:
      - path: "/api/webhooks/stripe"
        method: "POST"
        workflow: "order-lifecycle"
        action: "pay"
```

**Event triggers** fire when messages arrive on EventBus topics:

```yaml
triggers:
  event:
    subscriptions:
      - topic: "order.created"
        event: "order.created"
        workflow: "order-lifecycle"
        action: "validate"
```

**Schedule triggers** fire on cron schedules:

```yaml
triggers:
  schedule:
    jobs:
      - cron: "0 0 * * *"
        workflow: "cleanup"
        action: "run"
```

### Pipeline Steps

Pipeline steps are the atomic units of business logic within route pipelines. Each step receives the accumulated context from all prior steps and adds its own output. Steps execute in sequence, forming a processing chain.

Available step types:

| Step Type | Purpose |
|-----------|---------|
| `step.request_parse` | Extract path params, query params, and body from the HTTP request |
| `step.validate` | Validate data (required fields, JSON Schema) |
| `step.set` | Set values in the pipeline context (supports template functions) |
| `step.transform` | Data transformation pipeline |
| `step.conditional` | Route execution based on field values |
| `step.db_query` | Execute a SELECT query against a database module |
| `step.db_exec` | Execute INSERT/UPDATE/DELETE against a database module |
| `step.json_response` | Return a JSON HTTP response |
| `step.http_call` | Make an outbound HTTP request |
| `step.publish` | Publish a message to a broker or EventBus topic |
| `step.state_transition` | Trigger a state machine transition |
| `step.delegate` | Forward the request to another handler module |
| `step.log` | Log data at a specified level |
| `step.delay` | Wait for a duration |
| `step.parallel` | Execute multiple steps concurrently |
| `step.script` | Execute a Yaegi dynamic script |
| `step.notify` | Send notifications (Slack, email, webhook) |

---

## 3. Your First Application (Step-by-Step)

This section walks through building a complete REST API application, adding one layer at a time.

### Step 1: Create the Project Structure

```bash
mkdir -p my-app/seed
mkdir -p my-app/components
mkdir -p my-app/data
```

Your directory will look like:

```
my-app/
  workflow.yaml         # Main configuration
  seed/                 # JSON files with initial data
  components/           # Dynamic component Go source files
  data/                 # Runtime data (SQLite databases, etc.)
```

### Step 2: Minimal HTTP Server

Start with the absolute minimum: an HTTP server, a router, and one API handler.

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

Build and run:

```bash
cd workflow
go build -o server ./cmd/server
./server -config my-app/workflow.yaml
```

Test it:

```bash
# Create an item
curl -s -X POST http://localhost:8080/api/items \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 9.99}'

# List all items
curl -s http://localhost:8080/api/items

# Get by ID (replace {id} with the actual ID from the POST response)
curl -s http://localhost:8080/api/items/{id}

# Update
curl -s -X PUT http://localhost:8080/api/items/{id} \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget Pro", "price": 19.99}'

# Delete
curl -s -X DELETE http://localhost:8080/api/items/{id}
```

You have a fully functional REST API with zero Go code. Every change from here is additive.

### Step 3: Add Middleware

Add CORS headers, request tracing, and rate limiting. Insert these modules between the router and API handler:

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"
      readTimeout: "30s"
      writeTimeout: "30s"

  - name: router
    type: http.router
    config:
      prefix: /api
    dependsOn:
      - web-server

  # --- Middleware chain ---
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
      requestsPerMinute: 300
      burstSize: 50
    dependsOn:
      - request-id

  - name: items-api
    type: api.handler
    config:
      resourceName: items
    dependsOn:
      - rate-limiter
```

Update your routes to include the middleware chain:

```yaml
workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: "GET"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter

      - method: "POST"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
```

Middlewares execute in the order listed. The typical ordering is:

```
Request -->  cors  -->  request-id  -->  rate-limiter  -->  auth-middleware  -->  Handler
Response <-- cors  <--  request-id  <--  rate-limiter  <--  auth-middleware  <--  Handler
```

1. **cors** -- must be first to handle preflight OPTIONS requests
2. **request-id** -- adds X-Request-ID early so downstream middleware can use it
3. **rate-limiter** -- rejects excess traffic before doing expensive work
4. **auth-middleware** -- validates tokens last (only authenticated requests reach the handler)

### Step 4: Add Authentication

Add JWT authentication with seed users:

```yaml
  # --- Authentication ---
  - name: auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"
      tokenExpiry: "24h"
      issuer: "my-app"
      seedFile: "./seed/users.json"
    dependsOn:
      - rate-limiter

  - name: auth-middleware
    type: http.middleware.auth
    config:
      authType: Bearer
    dependsOn:
      - auth
```

Create the seed users file at `my-app/seed/users.json`:

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

Now update your `items-api` to depend on `auth-middleware` instead of `rate-limiter`:

```yaml
  - name: items-api
    type: api.handler
    config:
      resourceName: items
    dependsOn:
      - auth-middleware
```

Wire public and protected routes:

```yaml
workflows:
  http:
    router: router
    server: web-server
    routes:
      # Public: authentication endpoints (no auth-middleware)
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

      # Protected: requires valid JWT
      - method: "GET"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware

      - method: "POST"
        path: "/api/items"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware

      - method: "GET"
        path: "/api/items/{id}"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware

      - method: "PUT"
        path: "/api/items/{id}"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware

      - method: "DELETE"
        path: "/api/items/{id}"
        handler: items-api
        middlewares:
          - cors
          - request-id
          - rate-limiter
          - auth-middleware
```

Test authentication:

```bash
# Set the JWT secret
export JWT_SECRET="dev-secret-change-in-production"

# Start the server
./server -config my-app/workflow.yaml

# Login to get a token
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@example.com", "password": "changeme123"}' \
  | jq -r '.token')

# Use the token for protected endpoints
curl -s http://localhost:8080/api/items \
  -H "Authorization: Bearer $TOKEN"

# Without a token, you get 401 Unauthorized
curl -s http://localhost:8080/api/items
```

### Step 5: Add Persistence

Add a database and persistence store so data survives restarts:

```yaml
  # --- Database ---
  - name: app-db
    type: database.workflow
    config:
      driver: "sqlite"
      dsn: "./data/my-app.db"

  - name: persistence
    type: persistence.store
    config:
      database: "app-db"
    dependsOn:
      - app-db
```

Update `items-api` to depend on persistence:

```yaml
  - name: items-api
    type: api.handler
    config:
      resourceName: items
      seedFile: "./seed/items.json"
    dependsOn:
      - auth-middleware
      - persistence
```

Create `my-app/seed/items.json` with initial data:

```json
[
  {
    "id": "item-001",
    "data": {
      "name": "Widget",
      "price": 9.99,
      "category": "gadgets",
      "inStock": true
    }
  },
  {
    "id": "item-002",
    "data": {
      "name": "Sprocket",
      "price": 14.50,
      "category": "parts",
      "inStock": true
    }
  }
]
```

The persistence store automatically creates SQLite tables and loads seed data on first run. Data persists to the `./data/my-app.db` file.

For production, switch to PostgreSQL by changing two config fields:

```yaml
  - name: app-db
    type: database.workflow
    config:
      driver: "postgres"
      dsn: "${DATABASE_URL}"
      maxOpenConns: 25
      maxIdleConns: 5
```

No code changes. No schema migrations. The persistence store handles this automatically.

### Step 6: Add Health Checks and Metrics

```yaml
  # --- Observability ---
  - name: health
    type: health.checker
    config:
      description: "Application health probes"

  - name: metrics
    type: metrics.collector
    config:
      namespace: my_app
```

These modules automatically register endpoints:

- `GET /healthz` -- health check (returns 200 if the application is healthy)
- `GET /readyz` -- readiness probe (returns 200 when the application is ready to serve traffic)
- `GET /livez` -- liveness probe (returns 200 while the application is running)
- `GET /metrics` -- Prometheus metrics in exposition format

No additional route configuration is needed for these -- they register themselves on the HTTP server.

### Step 7: Complete Application

Here is the full `my-app/workflow.yaml` with all layers combined:

```yaml
modules:
  # --- HTTP infrastructure ---
  - name: web-server
    type: http.server
    config:
      address: ":8080"
      readTimeout: "30s"
      writeTimeout: "30s"

  - name: router
    type: http.router
    config:
      prefix: /api
    dependsOn:
      - web-server

  # --- Middleware chain ---
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
      requestsPerMinute: 300
      burstSize: 50
    dependsOn:
      - request-id

  # --- Authentication ---
  - name: auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"
      tokenExpiry: "24h"
      issuer: "my-app"
      seedFile: "./seed/users.json"
    dependsOn:
      - rate-limiter

  - name: auth-middleware
    type: http.middleware.auth
    config:
      authType: Bearer
    dependsOn:
      - auth

  # --- Database ---
  - name: app-db
    type: database.workflow
    config:
      driver: "sqlite"
      dsn: "./data/my-app.db"

  - name: persistence
    type: persistence.store
    config:
      database: "app-db"
    dependsOn:
      - app-db

  # --- API handlers ---
  - name: items-api
    type: api.handler
    config:
      resourceName: items
      seedFile: "./seed/items.json"
    dependsOn:
      - auth-middleware
      - persistence

  # --- Observability ---
  - name: health
    type: health.checker
    config:
      description: "Application health probes"

  - name: metrics
    type: metrics.collector
    config:
      namespace: my_app

workflows:
  http:
    router: router
    server: web-server
    routes:
      # Public: authentication
      - method: "POST"
        path: "/api/auth/login"
        handler: auth
        middlewares: [cors, request-id, rate-limiter]

      - method: "POST"
        path: "/api/auth/register"
        handler: auth
        middlewares: [cors, request-id, rate-limiter]

      - method: "GET"
        path: "/api/auth/profile"
        handler: auth
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      # Protected: items CRUD
      - method: "GET"
        path: "/api/items"
        handler: items-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "GET"
        path: "/api/items/{id}"
        handler: items-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "POST"
        path: "/api/items"
        handler: items-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "PUT"
        path: "/api/items/{id}"
        handler: items-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]

      - method: "DELETE"
        path: "/api/items/{id}"
        handler: items-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
```

Run it:

```bash
export JWT_SECRET="dev-secret-change-in-production"
./server -config my-app/workflow.yaml
```

You now have a production-ready REST API with:
- CORS support
- Request tracing (X-Request-ID)
- Rate limiting (300 req/min)
- JWT authentication with seed users
- SQLite persistence with seed data
- Health checks and Prometheus metrics

All in ~100 lines of YAML.

---

## 4. Adding Business Logic with Pipelines

Pipeline steps let you build complex request-processing logic directly in YAML. Instead of writing a custom handler, you compose a pipeline from atomic steps that validate input, prepare data, query databases, and return responses.

### What Pipeline Steps Are

A pipeline is an ordered sequence of steps attached to an HTTP route. Each step:

1. Receives the accumulated context from all prior steps
2. Performs its operation (validate, set, query, etc.)
3. Adds its output to the context under `steps.<step-name>`
4. Passes control to the next step

```
Request
   |
   v
step.request_parse  --> steps.parse-request.body, steps.parse-request.path_params
   |
   v
step.validate       --> (passes or fails with 400)
   |
   v
step.set            --> steps.prepare.id, steps.prepare.now
   |
   v
step.db_exec        --> (inserts row into database)
   |
   v
step.json_response  --> HTTP 201 response with created resource
```

### Template Syntax

Pipeline steps support Go template expressions for dynamic values. Templates access the pipeline context using two syntaxes:

**Dot notation** for simple paths:

```yaml
body_from: "steps.get-item.row"
```

**Template expressions** with `{{ }}` delimiters:

```yaml
values:
  id: "{{ uuidv4 }}"
  timestamp: "{{ now }}"
  name: "{{ .steps.parse-request.body.name }}"
```

**Index syntax** for keys containing hyphens:

```yaml
params:
  - "{{index .steps \"parse-request\" \"body\" \"name\"}}"
  - "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
```

### Available Template Functions

| Function | Description | Example |
|----------|-------------|---------|
| `uuidv4` | Generate a random UUID v4 | `{{ uuidv4 }}` |
| `now` | Current timestamp (RFC3339) | `{{ now }}` |
| `lower` | Lowercase a string | `{{lower (index .steps "parse-request" "body" "name")}}` |
| `default` | Default value if empty | `{{default .steps.input.status "pending"}}` |
| `json` | Marshal a value to JSON | `{{json .steps.query.rows}}` |

### Example: Building a CRUD Endpoint

Here is a complete pipeline-native CRUD implementation for a "products" resource. This approach uses `api.query` and `api.command` handlers with inline pipeline steps instead of the generic `api.handler`.

First, declare the handler modules and database:

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

  - name: app-db
    type: database.workflow
    config:
      driver: "sqlite"
      dsn: "./data/app.db"

  - name: product-queries
    type: api.query
    dependsOn:
      - router
      - app-db

  - name: product-commands
    type: api.command
    dependsOn:
      - router
      - app-db
```

#### List Products (GET)

```yaml
workflows:
  http:
    router: router
    server: web-server
    routes:
      - method: GET
        path: "/api/products"
        handler: product-queries
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
        pipeline:
          steps:
            - name: list-products
              type: step.db_query
              config:
                database: app-db
                query: "SELECT id, name, price, category, created_at FROM products ORDER BY created_at DESC"
                mode: list
            - name: respond
              type: step.json_response
              config:
                status: 200
                body_from: "steps.list-products.rows"
```

The `step.db_query` runs a SELECT and stores the results in `steps.list-products.rows`. The `step.json_response` returns those rows as a JSON array with HTTP 200.

#### Get Single Product (GET by ID)

```yaml
      - method: GET
        path: "/api/products/{id}"
        handler: product-queries
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
        pipeline:
          steps:
            - name: parse-request
              type: step.request_parse
              config:
                path_params: [id]
            - name: get-product
              type: step.db_query
              config:
                database: app-db
                query: "SELECT id, name, price, category, created_at FROM products WHERE id = ?"
                params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
                mode: single
            - name: check-found
              type: step.conditional
              config:
                field: "steps.get-product.found"
                routes:
                  "false": not-found
                default: respond
            - name: respond
              type: step.json_response
              config:
                status: 200
                body_from: "steps.get-product.row"
            - name: not-found
              type: step.json_response
              config:
                status: 404
                body:
                  error: "product not found"
```

Key points:
- `step.request_parse` extracts path parameters (`{id}` from the URL)
- `step.db_query` with `mode: single` returns one row (or `found: false`)
- `step.conditional` branches based on whether the row was found
- Two `step.json_response` steps handle the success and 404 cases

#### Create Product (POST)

```yaml
      - method: POST
        path: "/api/products"
        handler: product-commands
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
        pipeline:
          steps:
            - name: parse-request
              type: step.request_parse
              config:
                parse_body: true
            - name: validate
              type: step.validate
              config:
                strategy: required_fields
                required_fields: [name, price]
                source: "steps.parse-request.body"
            - name: prepare
              type: step.set
              config:
                values:
                  id: "{{ uuidv4 }}"
                  now: "{{ now }}"
            - name: insert
              type: step.db_exec
              config:
                database: app-db
                query: "INSERT INTO products (id, name, price, category, created_at) VALUES (?, ?, ?, ?, ?)"
                params:
                  - "{{ .steps.prepare.id }}"
                  - "{{index .steps \"parse-request\" \"body\" \"name\"}}"
                  - "{{index .steps \"parse-request\" \"body\" \"price\"}}"
                  - "{{default (index .steps \"parse-request\" \"body\" \"category\") \"uncategorized\"}}"
                  - "{{ .steps.prepare.now }}"
            - name: respond
              type: step.json_response
              config:
                status: 201
                body:
                  id: "{{ .steps.prepare.id }}"
                  name: "{{index .steps \"parse-request\" \"body\" \"name\"}}"
                  price: "{{index .steps \"parse-request\" \"body\" \"price\"}}"
                  created_at: "{{ .steps.prepare.now }}"
```

The flow is: **parse request** -> **validate required fields** -> **generate ID and timestamp** -> **insert into database** -> **return 201 with created resource**.

#### Delete Product (DELETE)

```yaml
      - method: DELETE
        path: "/api/products/{id}"
        handler: product-commands
        middlewares: [cors, request-id, rate-limiter, auth-middleware]
        pipeline:
          steps:
            - name: parse-request
              type: step.request_parse
              config:
                path_params: [id]
            - name: check-exists
              type: step.db_query
              config:
                database: app-db
                query: "SELECT id FROM products WHERE id = ?"
                params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
                mode: single
            - name: check-found
              type: step.conditional
              config:
                field: "steps.check-exists.found"
                routes:
                  "false": not-found
                default: delete
            - name: delete
              type: step.db_exec
              config:
                database: app-db
                query: "DELETE FROM products WHERE id = ?"
                params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
            - name: respond
              type: step.json_response
              config:
                status: 200
                body:
                  deleted: true
                  id: "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
            - name: not-found
              type: step.json_response
              config:
                status: 404
                body:
                  error: "product not found"
```

### Pipeline Error Handling

Pipelines support two error modes:

```yaml
pipelines:
  my-pipeline:
    on_error: stop     # Stop on first error (default)
    timeout: 30s       # Overall pipeline timeout
    steps: [...]
```

| `on_error` | Behavior |
|------------|----------|
| `stop` | Stop the pipeline immediately on error. Return the error to the caller. |
| `skip` | Skip the failed step and continue with the remaining steps. |

### When to Use Pipelines vs. api.handler

| Use Case | Approach |
|----------|----------|
| Standard CRUD with seed data, no custom logic | `api.handler` |
| Custom SQL queries, complex validation, multi-step processing | Pipeline steps |
| Resources with state machine lifecycle | `api.handler` with `workflowType`/`workflowEngine` |
| Admin/management APIs with direct DB access | Pipeline steps |

The `api.handler` approach is simpler for standard CRUD. Pipeline steps give you full control over the request-processing flow when you need it.

---

## 5. State Machine Workflows

State machines model entity lifecycles. An order goes from `new` to `paid` to `shipped`. A support ticket goes from `open` to `in_progress` to `resolved`. A conversation goes from `new` to `queued` to `active` to `closed`. The state machine engine manages these transitions, enforces valid state changes, and triggers processing at each step.

### Defining a State Machine

A state machine has three parts: **states**, **transitions**, and **hooks**.

#### States

Each state describes a point in an entity's lifecycle:

```yaml
states:
  new:
    description: "Order created, not yet validated"
    isFinal: false
    isError: false
  validated:
    description: "Order data has been validated"
    isFinal: false
    isError: false
  paid:
    description: "Payment has been confirmed"
    isFinal: false
    isError: false
  shipped:
    description: "Order has shipped"
    isFinal: true        # No further transitions allowed
    isError: false
  cancelled:
    description: "Order was cancelled"
    isFinal: true
    isError: false
  failed:
    description: "Processing error occurred"
    isFinal: true
    isError: true         # Marks this as an error terminal state
```

- `isFinal: true` means no transitions can originate from this state
- `isError: true` marks the state as an error/failure terminal

#### Transitions

Transitions are named edges between states:

```yaml
transitions:
  validate:
    fromState: "new"
    toState: "validated"
    autoTransform: true      # Happens automatically on creation
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
```

Key fields:
- `fromState` -- the state the entity must be in for this transition to be valid
- `toState` -- the state the entity moves to after the transition
- `autoTransform` -- if `true`, the transition fires automatically when the entity enters `fromState` (no external trigger needed)

#### Hooks

Hooks bind processing steps or handlers to transitions. When a transition fires, all matching hooks execute:

```yaml
hooks:
  - workflowType: "order-lifecycle"
    transitions: ["validate"]
    handler: "step-validate-order"

  - workflowType: "order-lifecycle"
    transitions: ["pay"]
    handler: "step-process-payment"

  - workflowType: "order-lifecycle"
    transitions: ["ship"]
    handler: "step-ship-order"

  # Cross-cutting: notify on any state change
  - workflowType: "order-lifecycle"
    toStates: ["validated", "paid", "shipped", "cancelled", "failed"]
    handler: "notification-handler"
```

You can hook by:
- **`transitions`** -- fires when specific named transitions execute
- **`toStates`** -- fires when entering any of the listed states (useful for cross-cutting concerns like notifications)

### Connecting to API Handlers

To make CRUD operations automatically create and manage state machine instances, configure the `api.handler` and add resource mappings:

```yaml
modules:
  - name: orders-api
    type: api.handler
    config:
      resourceName: orders
      workflowType: "order-lifecycle"       # State machine definition name
      workflowEngine: "order-engine"        # State machine engine module name
      initialTransition: "validate"          # Transition to trigger on creation
      instanceIDField: "id"                  # Field used as instance ID
    dependsOn:
      - auth-middleware
      - persistence

  - name: order-engine
    type: statemachine.engine
    config:
      description: "Manages order lifecycle"
    dependsOn:
      - orders-api
```

Resource mappings in the workflow section connect them:

```yaml
workflows:
  statemachine:
    engine: order-engine
    resourceMappings:
      - resourceType: "orders"              # Matches api.handler.config.resourceName
        stateMachine: "order-engine"        # Matches statemachine.engine module name
        instanceIDKey: "id"                 # Field in the resource used as instance ID
    definitions:
      - name: order-lifecycle
        # ... states, transitions as above
    hooks:
      # ... hooks as above
```

When you `POST /api/orders` with order data:
1. The `orders-api` handler creates the resource in persistence
2. A state machine instance is created in state `new`
3. The `validate` transition fires automatically (because `autoTransform: true`)
4. The `step-validate-order` hook executes
5. On success, the order moves to `validated`

### Processing Steps for Transitions

Processing steps link dynamic components to state machine transitions:

```yaml
  - name: step-validate-order
    type: processing.step
    config:
      componentId: "order-validator"       # Name of the dynamic component
      successTransition: "validate"         # Transition to trigger on success
      compensateTransition: "fail"          # Transition to trigger on failure
      maxRetries: 2
      retryBackoffMs: 1000
      timeoutSeconds: 30
    dependsOn:
      - order-validator
      - order-engine
```

| Config Key | Description |
|-----------|-------------|
| `componentId` | Name of the dynamic component to execute |
| `successTransition` | Transition to trigger when the component returns successfully |
| `compensateTransition` | Transition to trigger when the component fails (after retries exhausted) |
| `maxRetries` | Maximum retry attempts (default: 2) |
| `retryBackoffMs` | Base backoff between retries in milliseconds (default: 1000) |
| `timeoutSeconds` | Maximum execution time per attempt in seconds (default: 30) |

### Complete State Machine Example

Here is a complete order processing system with a state machine, dynamic components, and webhook triggers:

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
      topics: ["order.submitted", "order.paid", "order.shipped", "order.failed"]
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

### Chat Platform Example: 13-State Conversation Lifecycle

The Chat Platform uses a 13-state machine for conversation management. This demonstrates how complex real-world lifecycles are modeled:

```
new --> queued --> assigned --> active --> wrap_up --> closed
                                |                     ^
                                +-- transferred ------+
                                |                     |
                                +-- escalated_medical +
                                |                     |
                                +-- escalated_police -+
                                |
                                +-- follow_up_scheduled --> follow_up_active --> closed
                                |
                                +-- closed (direct close)

queued --> expired (timeout)
any --> failed (processing error)
```

Key patterns from the Chat Platform state machine:

1. **Auto-transitions** (`autoTransform: true`): `route_message` and `start_conversation` fire automatically, so the inbound message flow (new -> queued -> assigned -> active) happens without manual intervention.

2. **Multiple transitions from one state**: The `active` state has 6 outgoing transitions (transfer, escalate medical, escalate police, wrap up, close, follow up), each with its own hook.

3. **Cross-cutting hooks**: A single notification handler fires on every state change using `toStates` with all non-initial states.

4. **Compensating transitions**: Processing steps can trigger failure transitions when errors occur, moving the entity to the `failed` state.

---

## 6. Event-Driven Patterns

Event-driven architecture decouples producers from consumers. Instead of calling a notification service directly after a state transition, you publish an event. Any number of handlers can subscribe to that event independently.

### Setting Up a Message Broker

#### In-Memory Broker (Single Process)

For development and single-process deployments, use the in-memory broker:

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

The `topics` list pre-registers topics. Handlers can also subscribe to wildcard patterns.

#### NATS Broker (Distributed)

For multi-process deployments, swap to NATS:

```yaml
  - name: event-broker
    type: messaging.nats
    config:
      url: "nats://localhost:4222"
```

#### Kafka Broker (High-Throughput)

For high-throughput event streaming:

```yaml
  - name: event-broker
    type: messaging.kafka
    config:
      brokers: ["localhost:9092"]
      groupId: "my-app"
```

The critical point: the rest of your configuration (subscriptions, handlers, topics) stays identical regardless of which broker implementation you use. Swap the module type and you swap the transport.

### Message Handlers

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

Wildcard patterns (`order.*`) match any topic starting with the prefix.

### Messaging Workflow Subscriptions

The `workflows.messaging` section wires handlers to specific topics:

```yaml
workflows:
  messaging:
    broker: event-broker
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

You can use wildcards to simplify:

```yaml
workflows:
  messaging:
    broker: event-broker
    subscriptions:
      - topic: "order.*"
        handler: notification-handler
```

### Event Triggers

Event triggers connect EventBus events to workflow actions. When an event arrives on a topic, the engine triggers the specified transition on the matching workflow:

```yaml
triggers:
  event:
    subscriptions:
      - topic: "order.created"
        event: "order.created"
        workflow: "order-lifecycle"
        action: "validate"
      - topic: "payment.confirmed"
        event: "payment.confirmed"
        workflow: "order-lifecycle"
        action: "confirm_payment"
```

This pattern enables event-driven orchestration: external systems publish events, and the engine reacts by advancing state machines.

### Publishing Events from Pipeline Steps

Pipeline steps can publish events using `step.publish`:

```yaml
pipelines:
  process-webhook:
    trigger:
      type: http
      config:
        path: /webhooks/incoming
        method: POST
    steps:
      - name: validate
        type: step.validate
        config:
          strategy: required_fields
          required_fields: [type, data]
      - name: extract
        type: step.set
        config:
          values:
            event_type: "{{ .type }}"
      - name: publish
        type: step.publish
        config:
          topic: "webhook.{{ .steps.extract.event_type }}"
          broker: event-broker
```

### Chat Platform Example: Event-Driven Notifications

The Chat Platform uses events extensively. Here is its messaging setup:

```yaml
  - name: event-broker
    type: messaging.broker
    config:
      topics:
        - "conversation.created"
        - "conversation.assigned"
        - "conversation.message.inbound"
        - "conversation.message.outbound"
        - "conversation.transferred"
        - "conversation.escalated"
        - "conversation.closed"
        - "conversation.followup.scheduled"
        - "conversation.followup.triggered"
        - "conversation.survey.completed"
        - "conversation.tag.updated"
        - "dm.message.sent"
        - "dm.message.received"
    dependsOn:
      - conversation-engine

  - name: notification-handler
    type: messaging.handler
    config:
      topic: "conversation.*"
      description: "Handles notifications for all conversation lifecycle events"
    dependsOn:
      - event-broker
```

Combined with state machine hooks that fire on every state change, this creates a complete event stream for the entire conversation lifecycle:

```yaml
    hooks:
      # ... transition-specific hooks ...

      # Cross-cutting: notify on ALL state changes
      - workflowType: "conversation-lifecycle"
        toStates: ["queued", "assigned", "active", "transferred",
                   "escalated_medical", "escalated_police", "wrap_up",
                   "closed", "expired", "failed"]
        handler: "notification-handler"
```

And event triggers that chain events into further state transitions:

```yaml
triggers:
  event:
    subscriptions:
      - topic: "conversation.created"
        event: "conversation.created"
        workflow: "conversation-lifecycle"
        action: "route_message"
      - topic: "conversation.assigned"
        event: "conversation.assigned"
        workflow: "conversation-lifecycle"
        action: "start_conversation"
      - topic: "conversation.followup.triggered"
        event: "conversation.followup.triggered"
        workflow: "conversation-lifecycle"
        action: "trigger_follow_up"
```

This creates an event-driven chain: a new conversation fires `conversation.created`, which triggers `route_message`, which moves to `queued`, which fires `conversation.assigned` (when picked up), which triggers `start_conversation`, and so on.

---

## 7. Dynamic Components

When built-in modules do not cover your domain-specific business logic, you write dynamic components. These are standard Go source files loaded at runtime by the Yaegi interpreter. They can be hot-reloaded without restarting the engine.

### When to Use Dynamic Components

- **Use built-in modules for**: HTTP routing, auth, CRUD, persistence, health, metrics, messaging infrastructure
- **Use dynamic components for**: Payment processing, custom routing logic, data transformation, third-party API integrations, domain-specific validation, encryption, AI/ML calls

Rule of thumb: if the logic is generic (CRUD, auth, health), use a built-in module. If it is specific to your domain (keyword matching, risk assessment, SMS provider integration), write a dynamic component.

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

Every dynamic component must satisfy these requirements:

1. **`//go:build ignore`** at the top -- prevents `go build` from compiling the file directly
2. **`package component`** -- must use this package name
3. **`Name()`** -- returns the component identifier (must match `componentId` in processing step config)
4. **`Contract()`** -- documents inputs and outputs for validation and UI
5. **`Init(services)`** -- called once on load with available services
6. **`Start(ctx)`** / **`Stop(ctx)`** -- lifecycle hooks
7. **`Execute(ctx, params)`** -- main entry point, receives input data and returns output data

Here is a complete example for an order validator:

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

// Contract describes the inputs and outputs.
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

// Init is called once when the component is loaded.
func Init(services map[string]interface{}) error {
	return nil
}

// Start is called when the engine starts.
func Start(ctx context.Context) error {
	return nil
}

// Stop is called when the engine stops.
func Stop(ctx context.Context) error {
	return nil
}

// Execute validates order data.
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

### Sandbox Restrictions

Dynamic components run in a Yaegi sandbox with these restrictions:

| Restriction | Details |
|-------------|---------|
| **Standard library only** | You can import any Go stdlib package (`fmt`, `strings`, `time`, `encoding/json`, `net/http`, `crypto/*`, etc.) |
| **No external packages** | You cannot import third-party modules (no `github.com/...` imports) |
| **No file system writes** | Read-only filesystem access |
| **No os.Exit** | The component cannot terminate the process |

If you need external packages, implement the logic as a built-in module instead of a dynamic component.

### Declaring Components in YAML

```yaml
  - name: order-validator
    type: dynamic.component
    config:
      source: "./components/order_validator.go"
      description: "Validates order data and inventory availability"
    dependsOn:
      - order-engine
```

The `source` field points to the Go source file relative to where you run the server.

### Connecting to Processing Steps

Processing steps execute dynamic components as part of state machine transitions:

```yaml
  - name: step-validate-order
    type: processing.step
    config:
      componentId: "order-validator"        # Must match Name() return value
      successTransition: "validate"
      compensateTransition: "fail"
      maxRetries: 2
      retryBackoffMs: 1000
      timeoutSeconds: 30
    dependsOn:
      - order-validator
      - order-engine
```

### Hot-Reload

Dynamic components support true hot-reload while the engine is running:

1. Edit the `.go` source file and save
2. The file watcher detects the change (500ms debounce)
3. The new source is validated and compiled by Yaegi
4. The old component is swapped for the new one
5. No engine restart needed -- other modules keep running

You can also reload via the HTTP API:

```bash
# List loaded components
curl -s http://localhost:8080/api/dynamic/components

# Update a component programmatically
curl -X PUT http://localhost:8080/api/dynamic/components/order-validator \
  -H "Content-Type: text/plain" \
  --data-binary @components/order_validator.go

# Delete a component
curl -X DELETE http://localhost:8080/api/dynamic/components/order-validator
```

### Testing Dynamic Components

Since dynamic components are standard Go files (with the `//go:build ignore` tag), you can test them directly:

```go
package component_test

import (
	"context"
	"testing"
)

func TestOrderValidator(t *testing.T) {
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

### Example: Custom Message Processor

Here is a dynamic component from the Chat Platform that processes inbound messages:

```go
//go:build ignore

package component

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func Name() string {
	return "message-processor"
}

func Contract() map[string]interface{} {
	return map[string]interface{}{
		"required_inputs": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "object",
				"description": "Inbound message with body, sender, channel",
			},
		},
		"outputs": map[string]interface{}{
			"processed": map[string]interface{}{
				"type":        "bool",
				"description": "Whether message was processed successfully",
			},
			"normalized_body": map[string]interface{}{
				"type":        "string",
				"description": "Cleaned and normalized message text",
			},
		},
	}
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	msg, ok := params["message"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing required parameter: message")
	}

	body, _ := msg["body"].(string)

	// Normalize the message
	normalized := strings.TrimSpace(body)
	normalized = strings.ToLower(normalized)

	return map[string]interface{}{
		"processed":       true,
		"normalized_body": normalized,
		"word_count":      len(strings.Fields(body)),
		"processed_at":    time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

### Example: Risk Tagger

A more complex component that classifies messages by risk level:

```go
//go:build ignore

package component

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func Name() string {
	return "risk-tagger"
}

func Contract() map[string]interface{} {
	return map[string]interface{}{
		"required_inputs": map[string]interface{}{
			"message_body": map[string]interface{}{
				"type":        "string",
				"description": "Message text to assess for risk",
			},
		},
		"outputs": map[string]interface{}{
			"risk_level": map[string]interface{}{
				"type":        "string",
				"description": "Risk level: low, medium, high, critical",
			},
			"risk_categories": map[string]interface{}{
				"type":        "array",
				"description": "Matching risk categories",
			},
		},
	}
}

func Init(services map[string]interface{}) error { return nil }
func Start(ctx context.Context) error            { return nil }
func Stop(ctx context.Context) error             { return nil }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	body, ok := params["message_body"].(string)
	if !ok {
		return nil, fmt.Errorf("missing required parameter: message_body")
	}

	lower := strings.ToLower(body)
	var categories []string
	riskLevel := "low"

	// Check for risk indicators (simplified example)
	criticalKeywords := []string{"emergency", "immediate danger", "life threatening"}
	highKeywords := []string{"harm", "unsafe", "crisis"}
	mediumKeywords := []string{"worried", "anxious", "struggling"}

	for _, kw := range criticalKeywords {
		if strings.Contains(lower, kw) {
			categories = append(categories, "critical_risk")
			riskLevel = "critical"
		}
	}
	for _, kw := range highKeywords {
		if strings.Contains(lower, kw) {
			categories = append(categories, "high_risk")
			if riskLevel != "critical" {
				riskLevel = "high"
			}
		}
	}
	for _, kw := range mediumKeywords {
		if strings.Contains(lower, kw) {
			categories = append(categories, "medium_risk")
			if riskLevel == "low" {
				riskLevel = "medium"
			}
		}
	}

	return map[string]interface{}{
		"risk_level":      riskLevel,
		"risk_categories": categories,
		"assessed_at":     time.Now().UTC().Format(time.RFC3339),
	}, nil
}
```

---

## 8. Real-World Case Study: Chat Platform

The Chat Platform is a production-grade mental health support application built entirely on the Workflow engine. It demonstrates how all the concepts in this guide come together in a complex, real-world system.

### Architecture Overview

The platform consists of 60+ modules organized across three logical services:

```
Browser -> [gateway:8080] -> reverse proxy -> [api:8081]          (auth, admin)
                                           -> [conversation:8082] (chat, state machine)

[conversation] <-> [Kafka] <-> [conversation]  (event-driven routing)
```

In monolith mode, all services run in a single process on port 8080 with an in-memory message broker.

### Module Count by Category

| Category | Count | Examples |
|----------|-------|---------|
| HTTP infrastructure | 6 | web-server, router, cors, request-id, rate-limiter, spa |
| Authentication | 2 | auth (JWT), auth-middleware |
| API handlers | 11 | affiliates, programs, users, keywords, surveys, conversations, messages, queue, webhooks, webchat, dm, resources, providers |
| State machine | 1 | conversation-engine |
| Persistence | 2 | platform-db (SQLite), persistence |
| Dynamic components | 15 | twilio-provider, aws-provider, partner-provider, webchat-handler, conversation-router, keyword-matcher, pii-encryptor, survey-engine, followup-scheduler, escalation-handler, ai-summarizer, risk-tagger, data-retention, message-processor, notification-sender |
| Processing steps | 11 | route-message, assign, start-convo, survey, transfer, accept-transfer, escalate, wrap-up, schedule-followup, trigger-followup, close |
| Messaging | 2 | event-broker, notification-handler |
| Observability | 2 | metrics, health |

### Key Patterns Used

#### 1. Multi-Provider Webhook Ingestion

Three SMS providers (Twilio, AWS, Partner) plus webchat all funnel into the same state machine:

```yaml
triggers:
  http:
    routes:
      - path: "/api/webhooks/twilio"
        method: "POST"
        workflow: "conversation-lifecycle"
        action: "route_message"
      - path: "/api/webhooks/aws"
        method: "POST"
        workflow: "conversation-lifecycle"
        action: "route_message"
      - path: "/api/webhooks/partner"
        method: "POST"
        workflow: "conversation-lifecycle"
        action: "route_message"
      - path: "/api/webchat/message"
        method: "POST"
        workflow: "conversation-lifecycle"
        action: "route_message"
```

Each provider has its own dynamic component that normalizes the provider-specific webhook format into a common message structure. The `route_message` transition processes the normalized message regardless of source.

#### 2. State Machine for Conversation Lifecycle

13 states, 17 transitions, with auto-transitions for the happy path:

```
new --(auto)--> queued --(manual accept)--> assigned --(auto)--> active
```

The `autoTransform` flag on `route_message` and `start_conversation` means inbound messages flow automatically from `new` through `queued` (after routing) to `active` (after assignment). Manual intervention is only needed for the `assign_responder` step (a human picks up the conversation).

#### 3. Dynamic Components for Business Logic

15 dynamic components handle all domain-specific logic:

- **keyword_matcher** -- routes messages to programs by keyword
- **conversation_router** -- assigns conversations to queues
- **message_processor** -- normalizes and processes messages
- **risk_tagger** -- real-time risk assessment
- **pii_encryptor** -- AES-256-GCM field-level encryption
- **escalation_handler** -- medical/police escalation workflow
- **survey_engine** -- entry/exit survey management
- **ai_summarizer** -- AI-generated conversation summaries

Each one is a single `.go` file that can be hot-reloaded independently.

#### 4. Role-Based Access

Three roles with different views:

| Role | Capabilities |
|------|-------------|
| **Responder** | Dashboard, chat, multi-chat, DM, actions (transfer, escalate, tag, wrap-up) |
| **Supervisor** | Overview with KPIs, responder monitoring, read-only chat, queue health |
| **Admin** | Affiliate/program/user/keyword/survey CRUD |

Role enforcement happens at the API handler level based on the JWT claims from `auth.jwt`.

#### 5. Multi-Tenancy with Affiliate Isolation

Three demo affiliates operate independently:

- Crisis Support International (US-East)
- Youth Mental Health Alliance (US-West)
- Global Wellness Network (EU-West)

Each has its own responders, supervisors, programs, keywords, and data isolation. A responder logged into one affiliate cannot see conversations from another.

### How to Run It

#### Monolith Mode (Development)

```bash
go run ./cmd/server -config example/chat-platform/workflow.yaml
```

Open http://localhost:8080 and log in:
- Responder: `responder1@example.com` / `demo123`
- Supervisor: `supervisor1@example.com` / `demo123`
- Admin: `admin@example.com` / `demo123`

#### Docker Compose (Distributed)

```bash
cd example/chat-platform
docker compose --profile distributed up --build
```

This starts three separate services (gateway, api, conversation) with Kafka for inter-service messaging.

### Testing the Platform

Simulate an inbound SMS:

```bash
curl -X POST http://localhost:8080/api/webhooks/twilio \
  -d "From=%2B15551234567&To=%2B1741741&Body=HELLO&MessageSid=SM001"
```

Simulate a webchat message:

```bash
curl -X POST http://localhost:8080/api/webchat/message \
  -d '{"sessionId": "web-001", "message": "I need help"}'
```

For the full walkthrough including accepting conversations, transfers, escalations, and multi-affiliate isolation, see [Chat Platform Walkthrough](chat-platform-walkthrough.md).

---

## 9. Deploying and Managing Your Application

### Build Process

#### Backend Only

If your application is backend-only (API server, no UI):

```bash
cd workflow
go build -o server ./cmd/server
```

The `server` binary is self-contained. Copy it and your config directory to the target machine.

#### With UI

If you want the embedded visual workflow builder:

```bash
# Build the UI
cd workflow/ui
npm install
npm run build

# Copy dist to the embed directory
cp -r dist/ ../module/ui_dist/

# Build the Go binary with embedded UI
cd ..
go build -o server ./cmd/server
```

The `go:embed` directive in `module/ui_dist/` bundles the built UI into the server binary.

### Running with Different Configs

The same binary can run different applications by pointing to different config files:

```bash
# Run your custom app
./server -config my-app/workflow.yaml

# Run the chat platform
./server -config example/chat-platform/workflow.yaml

# Run the admin platform
./server -config admin/config.yaml

# Run the ecommerce example
./server -config example/ecommerce-app/workflow.yaml
```

### Using the Admin Platform

The admin platform provides a management layer for multiple workflow deployments:

```bash
./server -config admin/config.yaml
```

This starts an admin API with pipeline-native routes for managing:

- Companies and organizations (hierarchical structure)
- Projects within organizations
- Workflows within projects (create, version, deploy, stop)
- User administration and IAM

The admin API itself is built using the same pipeline steps covered in Section 4.

### Health Checks and Monitoring

Every application automatically gets observability endpoints when you include the `health.checker` and `metrics.collector` modules:

```bash
# Health check
curl http://localhost:8080/healthz
# {"status":"healthy","checks":[...]}

# Readiness probe (for Kubernetes)
curl http://localhost:8080/readyz
# {"status":"ready"}

# Liveness probe (for Kubernetes)
curl http://localhost:8080/livez
# {"status":"alive"}

# Prometheus metrics
curl http://localhost:8080/metrics
# workflow_http_requests_total{method="GET",path="/api/items",status="200"} 42
# workflow_http_request_duration_seconds_bucket{...} ...
```

#### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-workflow-app
spec:
  template:
    spec:
      containers:
        - name: app
          image: my-workflow-app:latest
          ports:
            - containerPort: 8080
          env:
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: app-secrets
                  key: jwt-secret
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: app-secrets
                  key: database-url
          livenessProbe:
            httpGet:
              path: /livez
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
```

### Docker Deployment

Create a `Dockerfile`:

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
docker run -p 8080:8080 \
  -e JWT_SECRET=your-production-secret \
  -e DATABASE_URL=postgres://user:pass@db:5432/myapp \
  my-app
```

### Docker Compose with Observability Stack

```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - JWT_SECRET=${JWT_SECRET}
      - DATABASE_URL=postgres://app:secret@postgres:5432/myapp
    volumes:
      - app-data:/app/data
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: myapp
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app -d myapp"]
      interval: 5s
      timeout: 5s
      retries: 5

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
  postgres-data:
```

### Secrets Management

Never hardcode secrets in YAML. The engine supports environment variable expansion:

```yaml
config:
  secret: "${JWT_SECRET}"                    # Expanded from environment
  dsn: "${DATABASE_URL}"                     # Expanded from environment
  apiKey: "${THIRD_PARTY_API_KEY}"           # Expanded from environment
```

For production, use a secrets manager module:

```yaml
  # HashiCorp Vault
  - name: vault-secrets
    type: secrets.vault
    config:
      address: "${VAULT_ADDR}"
      token: "${VAULT_TOKEN}"
      path: "secret/data/my-app"

  # AWS Secrets Manager
  - name: aws-secrets
    type: secrets.aws
    config:
      region: "us-east-1"
      secretName: "my-app/production"
```

### Configuration Updates

Dynamic components support hot-reload via the API (see Section 7). For configuration changes that affect module topology (adding/removing modules, changing routes), you need to restart the server with the updated config file.

The admin platform provides a workflow management API that can version, deploy, and stop workflow configurations without manual restarts:

```bash
# Deploy a new version via admin API
curl -X POST http://localhost:8080/api/v1/admin/workflows/{id}/deploy \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Stop a running workflow
curl -X POST http://localhost:8080/api/v1/admin/workflows/{id}/stop \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Database Selection

| Environment | Driver | DSN Example |
|-------------|--------|-------------|
| Development | `sqlite` | `./data/my-app.db` |
| Staging | `postgres` | `postgres://user:pass@staging-db:5432/myapp?sslmode=require` |
| Production | `postgres` | `${DATABASE_URL}` |

Switching from SQLite to PostgreSQL requires changing two config fields (`driver` and `dsn`). No code changes, no schema migrations.

---

## 10. Next Steps

You now have a solid understanding of how to build applications with the Workflow engine. Here are the resources for going deeper:

### Reference Documentation

- **[BUILDING_APPS_GUIDE.md](../BUILDING_APPS_GUIDE.md)** -- Comprehensive module reference with config tables for every module type, common patterns, and troubleshooting
- **[MODULE_BEST_PRACTICES.md](../MODULE_BEST_PRACTICES.md)** -- Guidelines for developing custom modules, naming conventions, testing patterns
- **[TRIGGER_ACTION_ARCHITECTURE.md](../TRIGGER_ACTION_ARCHITECTURE.md)** -- Deep dive into the pipeline step architecture, step type registry, template expressions
- **[DOCUMENTATION.md](../../DOCUMENTATION.md)** -- Full module type catalog with descriptions

### Tutorials

- **[Chat Platform Walkthrough](chat-platform-walkthrough.md)** -- Step-by-step walkthrough of the chat platform including testing the responder workflow, supervisor monitoring, and multi-tenant isolation
- **[Getting Started](getting-started.md)** -- Quick-start guide for your first workflow
- **[Building Plugins](building-plugins.md)** -- Writing custom built-in modules for the engine
- **[Scaling Workflows](scaling-workflows.md)** -- Performance tuning and horizontal scaling patterns

### Example Configurations

The `example/` directory contains 37+ YAML configurations demonstrating different patterns:

| Example | Key Patterns |
|---------|-------------|
| `example/chat-platform/` | Full platform: 60+ modules, 13-state machine, multi-provider webhooks, 15 dynamic components |
| `example/ecommerce-app/` | E-commerce: order lifecycle, inventory, payment processing, shipping |
| `example/order-processing-pipeline.yaml` | Simple pipeline: HTTP -> state machine -> messaging |
| `example/webhook-pipeline.yaml` | Pipeline steps: validate -> set -> conditional -> publish |
| `example/data-sync-pipeline.yaml` | Outbound HTTP calls, data transformation |
| `example/notification-pipeline.yaml` | Event-driven notification routing |

### What to Build Next

1. **Start simple**: Build a REST API with auth and persistence (Section 3)
2. **Add pipelines**: Replace simple CRUD with pipeline-native routes for custom logic (Section 4)
3. **Add state machines**: Model entity lifecycles with transitions and hooks (Section 5)
4. **Add events**: Decouple processing with pub/sub messaging (Section 6)
5. **Add dynamic components**: Write domain-specific Go logic loaded at runtime (Section 7)
6. **Deploy**: Containerize and add observability (Section 9)

Each layer is additive. You can start with a 20-line YAML file and grow to a 1000+ line configuration as your application's complexity grows -- all without writing custom server code.

---

*For the full Chat Platform walkthrough, see [tutorials/chat-platform-walkthrough.md](chat-platform-walkthrough.md). For the comprehensive module reference, see [BUILDING_APPS_GUIDE.md](../BUILDING_APPS_GUIDE.md).*
