# Workflow DSL Reference

Authoritative reference for the Workflow engine YAML configuration format. Each top-level key maps to a section below.

---

<!-- section: application -->
## Application

The optional top-level metadata fields identify the application and declare its external dependencies.

### Optional Fields
- `name` (string) — human-readable application name, used in logs and the management API
- `version` (string) — application version (semver recommended, e.g., `"1.0.0"`)
- `description` (string) — free-text description used by `wfctl docs generate`
- `requires` (object) — declares plugins and capabilities the engine must load before starting

### `requires` Sub-fields
- `requires.plugins` (array) — list of plugin objects, each with:
  - `name` (string, required) — plugin registry name
  - `version` (string, optional) — semver constraint (e.g., `">=1.0.0"`)
- `requires.capabilities` (array of strings) — abstract capability names (e.g., `"authorization"`, `"http-serving"`)

### Example
```yaml
name: order-api
version: "2.1.0"
description: "REST API for order management with JWT auth and state machine processing"

requires:
  plugins:
    - name: workflow-plugin-http
    - name: workflow-plugin-authz
      version: ">=1.0.0"
  capabilities:
    - authorization
    - http-serving
```

### Relationship to Other Sections
- `requires.plugins` must be satisfied before any module or workflow config is validated
- Plugin names resolve to module type registrations consumed by `modules[].type`

---

<!-- section: modules -->
## Modules

Modules are the building blocks of a workflow application. Each module represents a runtime service component (HTTP server, database connection, message broker, state machine, etc.). The engine initialises modules in dependency order and keeps them running for the application lifetime.

### Required Fields
- `name` (string) — unique identifier for this module instance (used as reference in workflows, triggers, and pipelines)
- `type` (string) — module type from the registry (e.g., `http.server`, `database.workflow`, `messaging.broker`)

### Optional Fields
- `config` (map) — type-specific configuration key/value pairs; available keys depend on the module type
- `dependsOn` (string[]) — module names this module waits for before initialising (controls start-up order)
- `branches` (map) — conditional routing to other modules (used by `dynamic.component` and `processing.step`)

### Example
```yaml
modules:
  - name: api-server
    type: http.server
    config:
      address: ":8080"

  - name: api-router
    type: http.router
    dependsOn:
      - api-server

  - name: db
    type: database.workflow
    config:
      driver: postgres
      dsn: "${DATABASE_URL}"

  - name: auth-jwt
    type: auth.jwt
    dependsOn:
      - api-router
    config:
      secret: "${JWT_SECRET}"
      issuer: "https://auth.example.com"
```

### Common Module Types

| Category | Types |
|----------|-------|
| HTTP | `http.server`, `http.router`, `http.handler`, `http.middleware.cors`, `http.middleware.auth`, `http.middleware.logging`, `http.middleware.ratelimit`, `http.middleware.requestid` |
| API | `api.handler`, `api.query`, `api.command` |
| Auth | `auth.jwt`, `auth.apikey` |
| Database | `database.workflow`, `storage.sqlite`, `storage.s3`, `storage.gcs`, `storage.local` |
| Messaging | `messaging.broker`, `messaging.handler`, `messaging.nats`, `messaging.kafka` |
| State Machine | `statemachine.engine`, `state.tracker`, `state.connector` |
| Observability | `health.checker`, `metrics.collector` |
| Scheduling | `scheduler.modular` |
| Config | `config.provider` |

### Relationship to Other Sections
- Module `name` values are referenced in `workflows.http.routes[].handler`, `workflows.messaging.broker`, `workflows.statemachine.engine`, and `triggers.http.server`
- `dependsOn` forms a DAG — circular dependencies are rejected at startup
- Module types are registered by plugins declared in `requires.plugins`

---

<!-- section: workflows -->
## Workflows

The `workflows` section wires together modules into runtime behaviours. It has four sub-sections, each mapping to a workflow kind: `http`, `messaging`, `statemachine`, and `event`.

### Optional Fields
- `workflows.http` — HTTP routing configuration
- `workflows.messaging` — message broker subscriptions and producers
- `workflows.statemachine` — state machine engine, definitions, and transitions
- `workflows.event` — event processor, handlers, and adapters

### Example
```yaml
workflows:
  http:
    server: api-server
    router: api-router
    routes:
      - method: GET
        path: /api/orders
        handler: orders-handler

  messaging:
    broker: event-broker
    subscriptions:
      - topic: order.created
        handler: notification-handler

  statemachine:
    engine: order-state
    definitions:
      - name: order-lifecycle
        initialState: pending
        states:
          pending: {}
          shipped: { isFinal: false }
          delivered: { isFinal: true }
        transitions:
          ship: { fromState: pending, toState: shipped }
          deliver: { fromState: shipped, toState: delivered }
```

### Relationship to Other Sections
- `workflows` references module names declared in `modules`
- `pipelines` have their own trigger mechanism and are independent of `workflows`

---

<!-- section: workflows-http -->
## Workflows > HTTP

The `workflows.http` sub-section configures inbound HTTP routing. It links an `http.server` module to an `http.router` module and declares the route table.

### Optional Fields
- `server` (string) — name of the `http.server` module (defaults to the first `http.server` found)
- `router` (string) — name of the `http.router` module (defaults to the first `http.router` found)
- `routes` (array) — list of route definitions

### Route Fields
- `method` (string, required) — HTTP method: `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`
- `path` (string, required) — URL path pattern; supports `:param` named parameters and `*wildcard`
- `handler` (string, required) — name of the handler module that processes the request
- `middlewares` (string[]) — ordered list of middleware module names applied before the handler

### Example
```yaml
workflows:
  http:
    server: api-server
    router: api-router
    routes:
      - method: GET
        path: /health
        handler: health-handler

      - method: GET
        path: /api/users
        handler: users-handler
        middlewares:
          - auth-jwt
          - authz-enforcer

      - method: POST
        path: /api/users
        handler: users-handler
        middlewares:
          - auth-jwt

      - method: GET
        path: /api/users/:id
        handler: users-handler
```

### Relationship to Other Sections
- `handler` and `middlewares` values reference module `name` fields in `modules`
- `triggers.http` provides an alternative inline trigger mechanism for `pipelines` without needing a separate route entry

---

<!-- section: workflows-messaging -->
## Workflows > Messaging

The `workflows.messaging` sub-section wires a message broker module to subscribers and declares which modules publish to which topics.

### Optional Fields
- `broker` (string) — name of the `messaging.broker` module (defaults to first broker found)
- `subscriptions` (array) — topic subscriptions
- `producers` (array) — modules that publish events to topics

### Subscription Fields
- `topic` (string, required) — topic name to subscribe to
- `handler` (string, required) — name of the handler module that processes messages on this topic

### Producer Fields
- `name` (string, required) — module name that emits events
- `forwardTo` (string[]) — list of topic names this module publishes to

### Example
```yaml
workflows:
  messaging:
    broker: event-broker
    subscriptions:
      - topic: order.created
        handler: notification-handler
      - topic: order.shipped
        handler: fulfillment-handler
      - topic: order.completed
        handler: analytics-handler
    producers:
      - name: orders-handler
        forwardTo:
          - order.created
          - order.updated
```

### Relationship to Other Sections
- `broker`, `handler`, and `name` values reference module `name` fields in `modules`
- Pipelines can publish to messaging topics using the `step.publish` step type

---

<!-- section: workflows-statemachine -->
## Workflows > State Machine

The `workflows.statemachine` sub-section configures state machine engines with lifecycle definitions and transition tables.

### Optional Fields
- `engine` (string) — name of the `statemachine.engine` module
- `definitions` (array) — list of state machine definitions

### Definition Fields
- `name` (string, required) — unique name for this state machine definition
- `description` (string) — human-readable description
- `initialState` (string, required) — name of the starting state
- `states` (map) — map of state name → state config
- `transitions` (map) — map of transition name → transition config

### State Config Fields
- `description` (string) — state description
- `isFinal` (bool) — whether this is a terminal state (default: false)
- `isError` (bool) — whether this is an error terminal state (default: false)

### Transition Config Fields
- `fromState` (string, required) — source state name
- `toState` (string, required) — target state name

### Example
```yaml
workflows:
  statemachine:
    engine: order-state
    definitions:
      - name: order-lifecycle
        description: "Manages order from creation to delivery"
        initialState: pending
        states:
          pending:
            description: "Awaiting confirmation"
          confirmed:
            description: "Payment verified"
          shipped:
            description: "In transit"
          delivered:
            description: "Delivered successfully"
            isFinal: true
          cancelled:
            description: "Cancelled"
            isFinal: true
            isError: true
        transitions:
          confirm_order:
            fromState: pending
            toState: confirmed
          ship_order:
            fromState: confirmed
            toState: shipped
          deliver_order:
            fromState: shipped
            toState: delivered
          cancel_order:
            fromState: pending
            toState: cancelled
```

### Relationship to Other Sections
- `engine` references a module `name` in `modules`
- `api.handler` modules can reference a `workflowEngine` config key to bind REST CRUD to a state machine

---

<!-- section: workflows-events -->
## Workflows > Events

The `workflows.event` sub-section configures event-driven processing with handlers and adapters.

### Optional Fields
- `processor` (string) — name of the event processor module
- `handlers` (array) — event type to handler mappings
- `adapters` (array) — event source adapters (e.g., webhook adapters, queue pollers)

### Handler Fields
- `event` (string, required) — event type name
- `handler` (string, required) — module name that processes this event type

### Example
```yaml
workflows:
  event:
    processor: event-processor
    handlers:
      - event: user.signup
        handler: welcome-email-handler
      - event: payment.failed
        handler: retry-handler
    adapters:
      - name: stripe-webhook-adapter
        type: http.webhook
        config:
          path: /webhooks/stripe
          secret: "${STRIPE_WEBHOOK_SECRET}"
```

### Relationship to Other Sections
- Handler names reference module `name` fields in `modules`
- Event triggers in `triggers` can route to event processors

---

<!-- section: pipelines -->
## Pipelines

Pipelines are composable step sequences that execute in response to a trigger. Each pipeline is independent of the `workflows` section and has its own trigger, step list, error policy, and timeout.

### Required Fields
- *(pipeline name)* (string key) — unique pipeline identifier

### Pipeline Fields
- `trigger` (object, required) — trigger configuration (see Triggers section)
- `steps` (array, required) — ordered list of step definitions
- `on_error` (string) — error handling policy: `stop` (default), `continue`, or `compensate`
- `timeout` (duration string) — maximum pipeline execution time (e.g., `30s`, `5m`)
- `compensation` (array) — steps to run in reverse if `on_error: compensate` (saga pattern)

### Step Fields
- `name` (string, required) — unique step identifier within the pipeline; used as a key in `steps.*` output references
- `type` (string, required) — step type (e.g., `step.set`, `step.http_call`, `step.validate`)
- `config` (map) — step type-specific configuration

### Template Expressions
Step `config` values support two expression syntaxes that may be mixed in the same string.

#### Expr syntax (recommended)
A simpler, more readable syntax using `${ }`:
- `${ field_name }` — top-level context field (e.g., request body fields)
- `${ steps["step-name"]["output_key"] }` — output from a named step (bracket notation supports hyphenated names)
- `${ trigger["headers"]["X-Request-Id"] }` — request headers
- `${ trigger["query"]["param"] }` — URL query parameters
- `${ upper(name) }` — call any template function
- `${ status == "active" && count > 0 }` — boolean/comparison expressions for skip_if/if guards
- `${ "Hello " + body["name"] }` — string concatenation

**Available namespaces:** `steps`, `trigger`, `body` (alias for trigger), `meta`, `current`

**Migrate existing templates:** `wfctl expr-migrate --config app.yaml --dry-run`

#### Go template syntax (legacy)
The original `{{ }}` syntax is still fully supported:
- `{{ .field_name }}` — top-level context field (e.g., request body fields)
- `{{ .steps.step_name.output_key }}` — output from a named step
- `{{ .headers.X-Request-Id }}` — request headers
- `{{ .query.param_name }}` — URL query parameters

### Built-in Step Types

| Type | Purpose |
|------|---------|
| `step.set` | Set key/value pairs in pipeline context |
| `step.log` | Log a message at a specified level |
| `step.validate` | Validate context fields against rules or JSON Schema |
| `step.transform` | Transform values using field mapping or templates |
| `step.conditional` | Branch execution based on a field value |
| `step.http_call` | Make an outbound HTTP request |
| `step.json_response` | Send a JSON HTTP response and stop pipeline |
| `step.db_query` | Execute a SQL query against a database module |
| `step.publish` | Publish a message to a messaging topic |

### Example
```yaml
pipelines:
  stripe-payment:
    trigger:
      type: http
      config:
        path: /webhooks/stripe
        method: POST

    steps:
      - name: validate-payload
        type: step.validate
        config:
          strategy: required_fields
          required_fields:
            - type
            - data

      - name: extract-event
        type: step.set
        config:
          values:
            event_type: "{{ .type }}"
            payload_id: "{{ .data.id }}"

      - name: route-by-type
        type: step.conditional
        config:
          field: "{{ .event_type }}"
          routes:
            "payment_intent.succeeded": process-success
            "payment_intent.failed": process-failure
          default: log-unknown

      - name: process-success
        type: step.json_response
        config:
          status: 200
          body_from: "steps.extract-event"

    on_error: stop
    timeout: 30s
```

### Relationship to Other Sections
- Pipelines reference module names in step configs (e.g., `step.db_query` references a database module)
- `workflows.messaging` can publish events consumed by pipelines with a `messaging` trigger type
- `triggers` provides named trigger definitions that pipelines can reference

---

<!-- section: triggers -->
## Triggers

Triggers connect external events to pipelines or handlers. They can be defined inline within a pipeline's `trigger` field, or as named top-level entries under `triggers`.

### Top-level Triggers Fields
- `http` (object) — HTTP trigger configuration
  - `server` (string) — name of the `http.server` module

### Inline Pipeline Trigger Fields
- `type` (string, required) — trigger type: `http`, `cron`, `event`, `messaging`
- `config` (map) — type-specific configuration

### HTTP Trigger Config
- `path` (string, required) — URL path (e.g., `/webhooks/stripe`)
- `method` (string, required) — HTTP method
- `middlewares` (string[]) — middleware module names applied to this route

### Cron Trigger Config
- `schedule` (string, required) — cron expression (e.g., `"0 * * * *"` for hourly)
- `timezone` (string) — IANA timezone name (default: `UTC`)

### Event Trigger Config
- `event` (string, required) — event type name to listen for

### Example
```yaml
triggers:
  http:
    server: api-server

pipelines:
  hourly-sync:
    trigger:
      type: cron
      config:
        schedule: "0 * * * *"
        timezone: "America/New_York"
    steps:
      - name: sync
        type: step.http_call
        config:
          url: "http://data-service:8081/sync"
          method: POST
    timeout: 5m

  on-user-created:
    trigger:
      type: event
      config:
        event: user.created
    steps:
      - name: send-welcome
        type: step.http_call
        config:
          url: "http://email-service:8082/send"
          method: POST
    timeout: 10s
```

### Relationship to Other Sections
- HTTP triggers share the same `http.server` module declared in `modules`
- `workflows.http.routes` and pipeline HTTP triggers are additive — both register routes on the same server
- Cron triggers require a `scheduler.modular` module in `modules`

---

<!-- section: imports -->
## Imports

The `imports` section splits large configurations across multiple YAML files. Imported files are merged into the main config before validation.

### Fields
- `imports` (string[]) — list of file paths relative to the main config file

### Merge Behavior
- `modules` arrays are concatenated (main file first, then imports in order)
- `pipelines` maps are merged (main file keys take precedence on collision)
- `workflows` sub-sections are deep-merged
- `triggers` maps are merged
- `requires.plugins` lists are deduplicated by name

### Example
```yaml
# main.yaml
imports:
  - infrastructure/modules.yaml
  - api/routes.yaml
  - pipelines/webhooks.yaml

name: my-app
version: "1.0.0"

modules:
  - name: app-server
    type: http.server
    config:
      address: ":8080"
```

```yaml
# infrastructure/modules.yaml
modules:
  - name: db
    type: database.workflow
    config:
      driver: sqlite
      dsn: "./data/app.db"

  - name: event-broker
    type: messaging.broker
```

### Relationship to Other Sections
- Module names must be unique across all imported files — collisions are rejected
- `sourceMap` metadata in the management API tracks which file each module/pipeline originated from
- Circular imports are detected and rejected

---

<!-- section: config-providers -->
## Config Providers

Config providers supply runtime configuration values to module `config` blocks using `{{config "key"}}` template expressions. They run before any modules are initialised.

### Usage Pattern
Declare a `config.provider` module with a `sources` list. The engine loads all sources in order, later sources overriding earlier ones.

### Config Provider `config` Fields
- `sources` (array) — ordered list of value sources
  - `type` (string, required) — source type: `defaults`, `env`, `file`
  - `values` (map) — key/value pairs (for `type: defaults`)
  - `prefix` (string) — env var prefix to strip (for `type: env`)
  - `path` (string) — path to a JSON or YAML file (for `type: file`)
- `schema` (map) — key → schema definition for validation
  - `required` (bool) — whether the key must be present
  - `description` (string) — key description

### Referencing Config Values
Use `{{config "key"}}` anywhere in a module or pipeline `config` block:
```yaml
config:
  dsn: "{{config \"database_url\"}}"
  secret: "{{config \"jwt_secret\"}}"
```

### Example
```yaml
modules:
  - name: app-config
    type: config.provider
    config:
      sources:
        - type: defaults
          values:
            database_url: "sqlite://./data/dev.db"
            jwt_secret: "dev-secret-change-in-prod"
            log_level: "info"
        - type: env
          prefix: "APP_"
        - type: file
          path: "./config/production.yaml"
      schema:
        database_url:
          required: true
          description: "Database connection string"
        jwt_secret:
          required: true
          description: "JWT signing secret"

  - name: db
    type: database.workflow
    config:
      dsn: "{{config \"database_url\"}}"

  - name: auth-jwt
    type: auth.jwt
    config:
      secret: "{{config \"jwt_secret\"}}"
```

### Relationship to Other Sections
- Config providers must be declared in `modules` — the `config.provider` type is registered by the `configprovider` plugin
- `{{config "key"}}` expressions are expanded in all `config` blocks across `modules`, `pipelines`, `workflows`, and `triggers`
- Environment variable source reads `APP_DATABASE_URL` for key `database_url` when `prefix: "APP_"`

---

<!-- section: platform -->
## Platform / Infrastructure / Sidecars

These top-level sections describe infrastructure-as-code resources that the engine can provision or reference. They are used primarily with IaC plugins and deployment tooling.

### `platform` Fields
- `org` (string) — organisation identifier
- `environment` (string) — deployment environment (e.g., `production`, `staging`)
- `templates` (array) — reusable infrastructure templates
  - `name` (string) — template name
  - `version` (string) — template version
  - `parameters` (array) — template parameter definitions
  - `capabilities` (array) — resource capability definitions

### `infrastructure` Fields
A map of infrastructure resource definitions. Structure is plugin-defined.

### `sidecars` Fields
- `sidecars` (array) — list of sidecar container definitions
  - `name` (string, required) — sidecar instance name
  - `type` (string, required) — sidecar type (e.g., `redis`, `jaeger`, `envoy`)
  - `config` (map) — type-specific configuration (e.g., `port`, `endpoint`)

### Example
```yaml
platform:
  org: "acme-corp"
  environment: "production"
  templates:
    - name: "microservice"
      version: "1.0.0"
      parameters:
        - name: "app_name"
          type: "string"
          required: true
        - name: "replicas"
          type: "int"
          default: 3
      capabilities:
        - name: "${app_name}"
          type: "container_runtime"
          properties:
            replicas: "${replicas}"
            ports:
              - container_port: 8080

sidecars:
  - name: redis-cache
    type: redis
    config:
      port: 6379
  - name: jaeger-agent
    type: jaeger
    config:
      port: 6831
      endpoint: "http://jaeger-collector:14268"
```

### Relationship to Other Sections
- `platform.context` module type references the `platform` section org/environment values
- Sidecars are deployed alongside the application container but are not addressable as workflow modules
- `infrastructure` resources are provisioned by IaC plugins before application start
