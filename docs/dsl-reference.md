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

The `infrastructure:` section declares infrastructure resources the application depends on. Each resource can specify per-environment resolution strategies.

- `infrastructure.resources` (array) — list of infrastructure resource declarations. Each entry:
  - `name` (string, required) — unique resource name
  - `type` (string, required) — resource type (e.g., `postgresql`, `redis`, `nats`, `s3-bucket`)
  - `provider` (string) — IaC provider to use for provisioning (e.g., `aws`, `gcp`, `azure`, `digitalocean`)
  - `config` (map) — resource-specific configuration
  - `environments` (map) — per-environment resolution strategies. Each key is an environment name and the value is an `InfraEnvironmentResolution` object:
    - `strategy` (string, required) — how to resolve this resource in this environment:
      - `container` — run a container locally (for local/CI environments)
      - `provision` — provision via IaC plugin (for staging/production)
      - `existing` — connect to an already-running instance
    - `dockerImage` (string) — container image to use when `strategy: container`
    - `port` (int) — container port when `strategy: container`
    - `provider` (string) — override IaC provider for this environment
    - `config` (map) — environment-specific resource configuration
    - `connection` (object) — connection details when `strategy: existing`:
      - `host` (string, required) — hostname or IP
      - `port` (int) — port number
      - `auth` (string) — authentication reference (e.g., a secret name)

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

infrastructure:
  resources:
    - name: db
      type: postgresql
      provider: aws
      config:
        instanceClass: db.t3.micro
      environments:
        local:
          strategy: container
          dockerImage: postgres:16
          port: 5432
        staging:
          strategy: provision
          provider: aws
        production:
          strategy: provision
          provider: aws
          config:
            instanceClass: db.r6g.large
    - name: cache
      type: redis
      environments:
        local:
          strategy: container
          dockerImage: redis:7
          port: 6379
        staging:
          strategy: existing
          connection:
            host: redis.internal.staging.example.com
            port: 6379
            auth: REDIS_PASSWORD
        production:
          strategy: provision
          provider: aws

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
- `infrastructure.resources[*].environments` per-env strategies control `wfctl dev up` container lifecycle
- Plugins declare their infra requirements in `plugin.json` via `moduleInfraRequirements` (see [Plugin Manifest Guide](plugin-manifest-guide.md))

---

<!-- section: ci -->
## CI

The optional `ci:` section declares the CI/CD lifecycle: what to build, how to test, and where to deploy.

### Fields

- `ci.build` (object) — build phase configuration
  - `ci.build.binaries` (array) — Go binaries to compile. Each entry:
    - `name` (string, required) — output binary name
    - `path` (string, required) — Go package path (e.g., `./cmd/server`)
    - `os` (array of strings) — target OS list (default: current OS)
    - `arch` (array of strings) — target arch list (default: current arch)
    - `ldflags` (string) — `go build -ldflags` value; env vars expanded
    - `env` (map) — additional environment variables for the build
  - `ci.build.containers` (array) — container images to build. Each entry:
    - `name` (string, required) — image name
    - `dockerfile` (string) — path to Dockerfile (default: `Dockerfile`)
    - `context` (string) — build context (default: `.`)
    - `registry` (string) — container registry prefix
    - `tag` (string) — image tag; env vars expanded
  - `ci.build.assets` (array) — non-binary artifacts (e.g., frontend bundles). Each entry:
    - `name` (string, required) — asset name
    - `build` (string, required) — shell command to build the asset
    - `path` (string, required) — output path
- `ci.test` (object) — test phase configuration
  - `ci.test.unit` / `ci.test.integration` / `ci.test.e2e` (object) — test phase:
    - `command` (string, required) — shell command to run tests
    - `coverage` (bool) — enable coverage reporting
    - `needs` (array of strings) — ephemeral Docker services to start before testing (e.g., `postgres`, `redis`, `mysql`)
- `ci.deploy` (object) — deployment configuration
  - `ci.deploy.environments` (map) — keyed by environment name. Each entry:
    - `provider` (string, required) — deployment provider (e.g., `aws-ecs`, `k8s`, `docker`)
    - `cluster` (string) — target cluster name
    - `namespace` (string) — Kubernetes namespace
    - `region` (string) — cloud region
    - `strategy` (string) — deployment strategy (`rolling`, `blue-green`, `canary`)
    - `requireApproval` (bool) — gate deployment on manual approval
    - `preDeploy` (array) — commands to run before deploying
    - `healthCheck` (object) — post-deploy health check:
      - `path` (string) — HTTP path to check
      - `timeout` (string) — timeout duration (e.g., `30s`)
- `ci.infra` (object) — infrastructure provisioning
  - `ci.infra.provision` (bool) — provision infrastructure as part of CI
  - `ci.infra.stateBackend` (string) — state backend name
  - `ci.infra.resources` (array) — inline resource declarations (see `infrastructure:` section)

### Example

```yaml
ci:
  build:
    binaries:
      - name: server
        path: ./cmd/server
        os: [linux]
        arch: [amd64, arm64]
        ldflags: "-X main.version=${VERSION}"
    containers:
      - name: my-app
        registry: ghcr.io/myorg
        tag: "${VERSION}"
  test:
    unit:
      command: go test ./... -race -count=1
      coverage: true
    integration:
      command: go test ./... -tags=integration
      needs: [postgres, redis]
  deploy:
    environments:
      staging:
        provider: k8s
        namespace: staging
        strategy: rolling
      production:
        provider: k8s
        namespace: production
        strategy: blue-green
        requireApproval: true
        healthCheck:
          path: /healthz
          timeout: 60s
```

### CLI Commands

- `wfctl ci run --phase build,test` — execute build and test phases
- `wfctl ci run --phase deploy --env staging` — deploy to a named environment
- `wfctl ci init --platform github-actions` — generate bootstrap GitHub Actions YAML
- `wfctl ci init --platform gitlab-ci` — generate bootstrap GitLab CI YAML
- `wfctl ci generate --platform github_actions` — generate infra-focused CI config

---

<!-- section: environments -->
## Environments

The optional `environments:` section declares named deployment environments with provider, region, env vars, and exposure config.

### Fields

`environments` is a map keyed by environment name. Each entry:

- `provider` (string, required) — deployment provider (e.g., `docker`, `k8s`, `aws-ecs`)
- `region` (string) — cloud region
- `envVars` (map) — environment variables injected at deploy time
- `secretsProvider` (string) — secrets provider for this environment
- `secretsPrefix` (string) — prefix applied to secret names in this environment
- `approvalRequired` (bool) — gate deployments on manual approval
- `exposure` (object) — how the service is exposed:
  - `method` (string) — exposure method (`tailscale`, `cloudflare-tunnel`, `port-forward`)
  - `exposure.tailscale` (object):
    - `funnel` (bool) — enable Tailscale Funnel
    - `hostname` (string) — Tailscale hostname
  - `exposure.cloudflareTunnel` (object):
    - `tunnelName` (string) — Cloudflare Tunnel name
    - `domain` (string) — domain to route traffic to
  - `exposure.portForward` (map) — local port-forward mappings

### Example

```yaml
environments:
  local:
    provider: docker
    envVars:
      LOG_LEVEL: debug
      DATABASE_URL: postgres://localhost/dev
    exposure:
      method: port-forward
      portForward:
        "8080": "8080"

  staging:
    provider: k8s
    region: us-east-1
    secretsProvider: env
    secretsPrefix: STAGING_
    exposure:
      method: tailscale
      tailscale:
        funnel: true
        hostname: my-app-staging

  production:
    provider: k8s
    region: us-east-1
    approvalRequired: true
    secretsProvider: env
    exposure:
      method: cloudflare-tunnel
      cloudflareTunnel:
        tunnelName: my-app-prod
        domain: api.myapp.com
```

---

<!-- section: secretStores -->
## Secret Stores

The optional `secretStores:` section declares named secret storage backends. This enables routing different secrets to different backends (e.g., application secrets in environment variables, payment keys in AWS Secrets Manager).

### Fields

- `secretStores.<name>` (object) — a named store. Fields:
  - `provider` (string, required) — backend provider: `env`, `vault`, `aws-secrets-manager`, `gcp-secret-manager`
  - `config` (map) — provider-specific configuration (e.g., Vault address, AWS region)

### Example

```yaml
secretStores:
  primary:
    provider: env
  payment-vault:
    provider: aws-secrets-manager
    config:
      region: us-east-1
```

### Relationship to Other Sections

- `secrets.defaultStore` references a named store from `secretStores`
- `secrets.entries[*].store` routes an individual secret to a specific store
- `environments[*].secretsProvider` overrides the store name for all secrets in that environment

---

<!-- section: secrets -->
## Secrets

The optional `secrets:` section declares the application's secret management configuration: which stores to use, rotation policy, and what secrets the application needs.

### Fields

- `secrets.defaultStore` (string) — name of the default store from `secretStores`. When set, all secrets without an explicit `store` field use this store.
- `secrets.provider` (string) — legacy single-provider name (use `defaultStore` + `secretStores` for new configs). Supported: `env`, `vault`, `aws-secrets-manager`, `gcp-secret-manager`
- `secrets.config` (map) — provider-specific configuration (used with legacy `provider` field)
- `secrets.rotation` (object) — default rotation policy:
  - `enabled` (bool) — enable automatic rotation
  - `interval` (string) — rotation interval (e.g., `30d`, `7d`)
  - `strategy` (string) — rotation strategy (`dual-credential`, `graceful`, `immediate`)
- `secrets.entries` (array) — declared secrets the application needs. Each entry:
  - `name` (string, required) — secret name (typically an env var name)
  - `description` (string) — human-readable description
  - `store` (string) — name of a specific store from `secretStores`; overrides `defaultStore` and environment override
  - `rotation` (object) — per-secret rotation override (same fields as `secrets.rotation`)

### Example (multi-store)

```yaml
secretStores:
  primary:
    provider: env
  payment-vault:
    provider: aws-secrets-manager
    config:
      region: us-east-1

secrets:
  defaultStore: primary
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
    - name: JWT_SECRET
      description: JWT signing key
      rotation:
        enabled: true
        interval: 7d
        strategy: graceful
    - name: STRIPE_SECRET_KEY
      description: Stripe payment API key
      store: payment-vault
```

### Example (single provider, legacy)

```yaml
secrets:
  provider: env
  rotation:
    enabled: true
    interval: 30d
    strategy: dual-credential
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
    - name: JWT_SECRET
      description: JWT signing key
```

### CLI Commands

- `wfctl secrets detect --config app.yaml` — scan config for secret-like values
- `wfctl secrets set DATABASE_URL --value "..."` — set a secret in the provider
- `wfctl secrets set TLS_CERT --from-file ./certs/server.crt` — set from file
- `wfctl secrets list --config app.yaml` — list declared secrets and their store routing
- `wfctl secrets validate --config app.yaml` — verify all secrets are set
- `wfctl secrets init --provider env --env staging` — initialize provider config
- `wfctl secrets rotate DATABASE_URL --env production` — trigger rotation
- `wfctl secrets sync --from staging --to production` — sync secret structure
- `wfctl secrets setup --env local` — interactively set all secrets for an environment
- `wfctl secrets setup --env production --auto-gen-keys` — set secrets, auto-generating key/token values

### Relationship to Other Sections

- `secretStores` must be declared before referencing store names in `secrets.defaultStore` or `secrets.entries[*].store`
- `environments[*].secretsProvider` overrides the store for all secrets in that environment (takes priority over per-secret `store` fields)
- `environments[*].secretsPrefix` is prepended to secret names when resolving in that environment
- `ci.deploy.environments` can reference secrets from the `secrets:` section

---

<!-- section: services -->
## Services

The optional `services:` section defines a multi-service application where each key is a service name. Each service can have its own binary, scaling policy, exposed ports, modules, pipelines, and plugins.

### Fields

- `services.<name>.description` (string) — human-readable description of this service
- `services.<name>.binary` (string) — Go package path to compile for this service (e.g., `./cmd/api`)
- `services.<name>.scaling` (object) — scaling policy:
  - `replicas` (int) — desired replica count
  - `min` (int) — minimum replicas for autoscaling
  - `max` (int) — maximum replicas for autoscaling
  - `metric` (string) — autoscaling metric (`cpu`, `memory`, `rps`)
  - `target` (int) — target metric value (e.g., `70` for 70% CPU)
- `services.<name>.expose` (array) — ports this service exposes. Each entry:
  - `port` (int, required) — port number
  - `protocol` (string) — protocol (`http`, `grpc`, `tcp`; default `tcp`)
- `services.<name>.modules` (array) — per-service module config (same format as top-level `modules`)
- `services.<name>.pipelines` (map) — per-service pipeline config
- `services.<name>.workflows` (map) — per-service workflow config
- `services.<name>.plugins` (string[]) — plugin names to load for this service

### Example

```yaml
services:
  api:
    description: "Public REST API"
    binary: ./cmd/api
    scaling:
      replicas: 2
      min: 1
      max: 10
      metric: cpu
      target: 70
    expose:
      - port: 8080
        protocol: http
    plugins:
      - workflow-plugin-auth

  worker:
    description: "Background job processor"
    binary: ./cmd/worker
    scaling:
      replicas: 1
      min: 1
      max: 5
    expose:
      - port: 9090
        protocol: grpc
```

### Relationship to Other Sections

- `networking.ingress` entries reference service names declared here
- `mesh.routes` `from`/`to` fields reference service names
- `wfctl ports list` scans `services[*].expose` for port bindings
- `wfctl validate` checks scaling constraints (min ≤ max) and port ranges

---

<!-- section: mesh -->
## Mesh

The optional `mesh:` section configures inter-service communication for multi-service applications.

### Fields

- `mesh.transport` (string) — default transport for service-to-service calls (`nats`, `http`, `grpc`)
- `mesh.discovery` (string) — service discovery mechanism (`dns`, `k8s`, `consul`)
- `mesh.nats` (object) — NATS-specific configuration:
  - `url` (string, required) — NATS server URL (e.g., `nats://nats:4222`)
  - `clusterId` (string) — NATS cluster ID for streaming
- `mesh.routes` (array) — inter-service communication declarations. Each route:
  - `from` (string, required) — source service name
  - `to` (string, required) — destination service name
  - `via` (string, required) — transport (`nats`, `http`, `grpc`)
  - `subject` (string) — NATS subject (when `via: nats`)
  - `endpoint` (string) — HTTP endpoint path (when `via: http`)

### Example

```yaml
mesh:
  transport: nats
  discovery: dns
  nats:
    url: nats://nats:4222
    clusterId: my-cluster
  routes:
    - from: api
      to: worker
      via: nats
      subject: tasks.process
    - from: api
      to: worker
      via: http
      endpoint: /internal/jobs
```

### Relationship to Other Sections

- `mesh.routes` `from`/`to` must reference services declared in `services:`
- `wfctl security generate-network-policies` uses `mesh.routes` to generate Kubernetes `NetworkPolicy` YAML
- `wfctl validate` warns when `from`/`to` reference unknown services

---

<!-- section: networking -->
## Networking

The optional `networking:` section configures how services are exposed externally, what inter-service traffic is permitted, and DNS records.

### Fields

- `networking.ingress` (array) — externally-accessible endpoints. Each entry:
  - `service` (string) — service name from `services:` (optional for single-service apps)
  - `port` (int, required) — internal service port
  - `externalPort` (int) — external-facing port (default: same as `port`)
  - `protocol` (string) — protocol (`http`, `https`, `grpc`, `tcp`)
  - `path` (string) — URL path prefix for HTTP routing
  - `tls` (object) — TLS termination config:
    - `provider` (string) — TLS certificate provider (`letsencrypt`, `manual`, `acm`, `cloudflare`)
    - `domain` (string) — domain name for certificate provisioning
    - `minVersion` (string) — minimum TLS version (`1.2`, `1.3`)
- `networking.policies` (array) — allowed inter-service communication (used for `NetworkPolicy` generation). Each entry:
  - `from` (string, required) — source service name
  - `to` (string[], required) — list of destination service names
- `networking.dns` (object) — DNS management:
  - `provider` (string) — DNS provider (`cloudflare`, `route53`, `gcp`)
  - `zone` (string) — DNS zone name
  - `records` (array) — DNS records. Each record:
    - `name` (string) — record name
    - `type` (string) — record type (`A`, `CNAME`, `TXT`)
    - `target` (string) — record value

### Example

```yaml
networking:
  ingress:
    - service: api
      port: 8080
      externalPort: 443
      protocol: https
      path: /
      tls:
        provider: letsencrypt
        domain: api.example.com
        minVersion: "1.2"
  policies:
    - from: api
      to: [worker, db]
    - from: worker
      to: [db]
  dns:
    provider: cloudflare
    zone: example.com
    records:
      - name: api
        type: CNAME
        target: lb.example.com
```

### Relationship to Other Sections

- `networking.ingress[*].service` must reference a service from `services:` that exposes the given port
- `networking.policies` are used by `wfctl security generate-network-policies` to generate Kubernetes `NetworkPolicy` YAML
- `wfctl ports list` includes ingress entries as `public` exposure
- `wfctl validate` checks that ingress services exist, ports are actually exposed, and TLS providers are valid

---

<!-- section: security -->
## Security

The optional `security:` section declares security policies for the application, including TLS requirements, network isolation, service identity, container runtime hardening, and automated scanning configuration.

### Fields

- `security.tls` (object) — TLS requirements:
  - `internal` (bool) — require TLS for service-to-service traffic
  - `external` (bool) — require TLS for external traffic
  - `provider` (string) — certificate provider (`letsencrypt`, `manual`, `acm`, `cloudflare`)
  - `minVersion` (string) — minimum TLS version (`1.2`, `1.3`)
- `security.network` (object) — network isolation:
  - `defaultPolicy` (string) — default network policy (`deny`, `allow`)
- `security.identity` (object) — service identity management:
  - `provider` (string) — identity provider (`spiffe`, `istio`, `linkerd`)
  - `perService` (bool) — issue a unique identity per service
- `security.runtime` (object) — container runtime security:
  - `readOnlyFilesystem` (bool) — mount container filesystem read-only
  - `noNewPrivileges` (bool) — prevent privilege escalation
  - `runAsNonRoot` (bool) — require non-root user
  - `dropCapabilities` (string[]) — Linux capabilities to drop (e.g., `[ALL]`)
  - `addCapabilities` (string[]) — Linux capabilities to add (e.g., `[NET_BIND_SERVICE]`)
- `security.scanning` (object) — automated security scanning:
  - `containerScan` (bool) — scan container images for vulnerabilities
  - `dependencyScan` (bool) — scan Go/npm dependencies for CVEs
  - `sast` (bool) — enable static application security testing

### Example

```yaml
security:
  tls:
    internal: true
    external: true
    provider: letsencrypt
    minVersion: "1.3"
  network:
    defaultPolicy: deny
  identity:
    provider: spiffe
    perService: true
  runtime:
    readOnlyFilesystem: true
    noNewPrivileges: true
    runAsNonRoot: true
    dropCapabilities: [ALL]
    addCapabilities: [NET_BIND_SERVICE]
  scanning:
    containerScan: true
    dependencyScan: true
    sast: false
```

### CLI Commands

- `wfctl security audit [--config app.yaml]` — scan config for TLS, network, auth, and runtime issues
- `wfctl security generate-network-policies [--output k8s/]` — generate Kubernetes `NetworkPolicy` YAML

### Relationship to Other Sections

- `security.tls.provider` must be one of `letsencrypt`, `manual`, `acm`, `cloudflare`
- `security.network.defaultPolicy: deny` causes `wfctl security generate-network-policies` to include `Egress` in policy types
- `networking.ingress` entries without a `tls` block are flagged as `HIGH` by `wfctl security audit`
- `wfctl validate` checks `security.tls.provider` for valid values
