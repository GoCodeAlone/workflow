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
| `step.jq` | Applies a JQ expression to pipeline data for complex transformations |
| `step.ai_complete` | AI text completion using a configured provider |
| `step.ai_classify` | AI text classification into named categories |
| `step.ai_extract` | AI structured data extraction using tool use or prompt-based parsing |

### CI/CD Pipeline Steps
| Type | Description |
|------|-------------|
| `step.docker_build` | Builds a Docker image from a context directory and Dockerfile |
| `step.docker_push` | Pushes a Docker image to a remote registry |
| `step.docker_run` | Runs a command inside a Docker container via sandbox |
| `step.scan_sast` | Static Application Security Testing (SAST) via configurable scanner |
| `step.scan_container` | Container image vulnerability scanning via Trivy |
| `step.scan_deps` | Dependency vulnerability scanning via Grype |
| `step.artifact_push` | Stores a file in the artifact store for cross-step sharing |
| `step.artifact_pull` | Retrieves an artifact from a prior execution, URL, or S3 |

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

### Infrastructure
| Type | Description |
|------|-------------|
| `license.validator` | License key validation against a remote server with caching and grace period |
| `platform.provider` | Cloud infrastructure provider declaration (e.g., Terraform, Pulumi) |
| `platform.resource` | Infrastructure resource managed by a platform provider |
| `platform.context` | Execution context for platform operations (org, environment, tier) |

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

## Module Type Reference

Detailed configuration reference for module types not covered in the main table above.

### Audit Logging (`audit/`)

The `audit/` package provides a structured JSON audit logger for recording security-relevant events. It is used internally by the engine and admin platform -- not a YAML module type, but rather a Go library used by other modules.

**Event types:** `auth`, `auth_failure`, `admin_op`, `escalation`, `data_access`, `config_change`, `component_op`

Each audit event is written as a single JSON line containing `timestamp`, `type`, `action`, `actor`, `resource`, `detail`, `source_ip`, `success`, and `metadata` fields.

---

### `license.validator`

Validates license keys against a remote server with local caching and an offline grace period. When no `server_url` is configured the module operates in offline/starter mode and synthesizes a valid starter-tier license locally.

**Configuration:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server_url` | string | `""` | License validation server URL. Leave empty for offline/starter mode. |
| `license_key` | string | `""` | License key. When empty, falls back to the `WORKFLOW_LICENSE_KEY` environment variable. |
| `cache_ttl` | duration | `1h` | How long to cache a valid license result before re-validating. |
| `grace_period` | duration | `72h` | How long to allow operation when the license server is unreachable. |
| `refresh_interval` | duration | `1h` | How often the background goroutine re-validates the license. |

**Outputs:** Provides the `license-validator` service (`LicenseValidator`).

**Example:**

```yaml
modules:
  - name: license
    type: license.validator
    config:
      server_url: "https://license.gocodalone.com/api/v1"
      license_key: ""  # leave empty to use WORKFLOW_LICENSE_KEY env var
      cache_ttl: "1h"
      grace_period: "72h"
      refresh_interval: "1h"
```

---

### `platform.provider`

Declares a cloud infrastructure provider (e.g., AWS, Docker Compose, GCP) for use with the platform workflow handler and reconciliation trigger.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `name` | string | yes | Provider identifier (e.g., `aws`, `docker-compose`, `gcp`). Used to construct the service name `platform.provider.<name>`. |
| `config` | map[string]string | no | Provider-specific configuration (credentials, region, etc.). |
| `tiers` | JSON | no | Three-tier infrastructure layout (`infrastructure`, `shared_primitives`, `application`). |

**Example:**

```yaml
modules:
  - name: cloud-provider
    type: platform.provider
    config:
      name: "aws"
      config:
        region: "us-east-1"
```

---

### `platform.resource`

A capability-based resource declaration managed by the platform abstraction layer.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `name` | string | yes | Unique identifier for this resource within its tier. |
| `type` | string | yes | Abstract capability type (e.g., `container_runtime`, `database`, `message_queue`). |
| `tier` | string | no | Infrastructure tier: `infrastructure`, `shared_primitive`, or `application` (default: `application`). |
| `capabilities` | JSON | no | Provider-agnostic capability properties (replicas, memory, ports, etc.). |
| `constraints` | JSON | no | Hard limits imposed by parent tiers. |

**Example:**

```yaml
modules:
  - name: orders-db
    type: platform.resource
    config:
      name: orders-db
      type: database
      tier: application
      capabilities:
        engine: postgresql
        storage: "10Gi"
```

---

### `platform.context`

Provides the execution context for platform operations. Used to identify the organization, environment, and tier for a deployment.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `org` | string | yes | Organization identifier. |
| `environment` | string | yes | Deployment environment (e.g., `production`, `staging`, `dev`). |
| `tier` | string | no | Infrastructure tier: `infrastructure`, `shared_primitive`, or `application` (default: `application`). |

**Example:**

```yaml
modules:
  - name: platform-ctx
    type: platform.context
    config:
      org: "acme-corp"
      environment: "production"
      tier: "application"
```

---

### `observability.otel`

Initializes an OpenTelemetry distributed tracing provider that exports spans via OTLP/HTTP to a collector. Sets the global OTel tracer provider so all instrumented code in the process is covered.

**Configuration:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `endpoint` | string | `localhost:4318` | OTLP collector endpoint (host:port). |
| `serviceName` | string | `workflow` | Service name used for trace attribution. |

**Outputs:** Provides the `tracer` service (`trace.Tracer`).

**Example:**

```yaml
modules:
  - name: tracing
    type: observability.otel
    config:
      endpoint: "otel-collector:4318"
      serviceName: "order-api"
```

---

### `step.jq`

Applies a JQ expression to pipeline data for complex transformations. Uses the `gojq` pure-Go JQ implementation, supporting the full JQ language: field access, pipes, `map`/`select`, object construction, arithmetic, conditionals, and more.

The expression is compiled at startup so syntax errors are caught early. When the result is a single object, its keys are merged into the step output so downstream steps can access fields directly.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `expression` | string | yes | JQ expression to evaluate. |
| `input_from` | string | no | Dotted path to the input value (e.g., `steps.fetch.items`). Defaults to the full current pipeline context. |

**Output fields:** `result` — the JQ result. When the result is a single object, its keys are also promoted to the top level.

**Example:**

```yaml
steps:
  - name: extract-active
    type: step.jq
    config:
      input_from: "steps.fetch-users.users"
      expression: "[.[] | select(.active == true) | {id, email}]"
```

---

### `step.ai_complete`

Invokes an AI provider to produce a text completion. Provider resolution order: explicit `provider` name, then model-based lookup, then first registered provider.

**Configuration:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `provider` | string | `""` | Named AI provider to use. Omit to auto-select. |
| `model` | string | `""` | Model name (e.g., `claude-3-5-sonnet-20241022`). Used for provider lookup if `provider` is unset. |
| `system_prompt` | string | `""` | System prompt. Supports Go template syntax with pipeline context. |
| `input_from` | string | `""` | Template expression to resolve the user message (e.g., `.body`). Falls back to `text` or `body` fields in current context. |
| `max_tokens` | number | `1024` | Maximum tokens in the completion. |
| `temperature` | number | `0` | Sampling temperature (0.0–1.0). |

**Output fields:** `content`, `model`, `finish_reason`, `usage.input_tokens`, `usage.output_tokens`.

**Example:**

```yaml
steps:
  - name: summarize
    type: step.ai_complete
    config:
      model: "claude-3-5-haiku-20241022"
      system_prompt: "You are a helpful assistant. Summarize the following text concisely."
      input_from: ".body"
      max_tokens: 512
```

---

### `step.ai_classify`

Classifies input text into one of a configured set of categories using an AI provider. Returns the winning category, a confidence score (0.0–1.0), and brief reasoning.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `categories` | array of strings | yes | List of valid classification categories. |
| `provider` | string | no | Named AI provider. Auto-selected if omitted. |
| `model` | string | no | Model name for provider lookup. |
| `input_from` | string | no | Template expression for the input text. Falls back to `text` or `body` fields. |
| `max_tokens` | number | `256` | Maximum tokens for the classification response. |
| `temperature` | number | `0` | Sampling temperature. |

**Output fields:** `category`, `confidence`, `reasoning`, `raw`, `model`, `usage.input_tokens`, `usage.output_tokens`.

**Example:**

```yaml
steps:
  - name: classify-ticket
    type: step.ai_classify
    config:
      input_from: ".body"
      categories:
        - "billing"
        - "technical-support"
        - "account"
        - "general-inquiry"
```

---

### `step.ai_extract`

Extracts structured data from text using an AI provider. When the provider supports tool use, it uses the tool-calling API for reliable structured output. Otherwise it falls back to prompt-based JSON extraction.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `schema` | object | yes | JSON Schema object describing the fields to extract. |
| `provider` | string | no | Named AI provider. Auto-selected if omitted. |
| `model` | string | no | Model name for provider lookup. |
| `input_from` | string | no | Template expression for the input text. Falls back to `text` or `body` fields. |
| `max_tokens` | number | `1024` | Maximum tokens. |
| `temperature` | number | `0` | Sampling temperature. |

**Output fields:** `extracted` (map of extracted fields), `method` (`tool_use`, `text_parse`, or `prompt`), `model`, `usage.input_tokens`, `usage.output_tokens`.

**Example:**

```yaml
steps:
  - name: extract-order
    type: step.ai_extract
    config:
      input_from: ".body"
      schema:
        type: object
        properties:
          customer_name: {type: string}
          order_items: {type: array, items: {type: string}}
          total_amount: {type: number}
```

---

### `step.docker_build`

Builds a Docker image from a context directory and Dockerfile using the Docker SDK. The context directory is tar-archived and sent to the Docker daemon.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `context` | string | yes | Path to the build context directory. |
| `dockerfile` | string | `Dockerfile` | Dockerfile path relative to the context directory. |
| `tags` | array of strings | no | Image tags to apply (e.g., `["myapp:latest", "myapp:1.2.3"]`). |
| `build_args` | map | no | Build argument key/value pairs. |
| `cache_from` | array of strings | no | Image references to use as layer cache sources. |

**Output fields:** `image_id`, `tags`, `context`.

**Example:**

```yaml
steps:
  - name: build-image
    type: step.docker_build
    config:
      context: "./src"
      dockerfile: "Dockerfile"
      tags:
        - "myapp:latest"
      build_args:
        APP_VERSION: "1.2.3"
```

---

### `step.docker_push`

Pushes a Docker image to a remote registry.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `image` | string | yes | Image name/tag to push. |
| `registry` | string | no | Registry hostname prefix (prepended to `image` when constructing the reference). |
| `auth_provider` | string | no | Named auth provider for registry credentials (informational; credentials are read from Docker daemon config). |

**Output fields:** `image`, `registry`, `digest`, `auth_provider`.

**Example:**

```yaml
steps:
  - name: push-image
    type: step.docker_push
    config:
      image: "myapp:latest"
      registry: "ghcr.io/myorg"
```

---

### `step.docker_run`

Runs a command inside a Docker container using the sandbox. Returns exit code, stdout, and stderr.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `image` | string | yes | Docker image to run. |
| `command` | array of strings | no | Command to execute inside the container. Uses image default entrypoint if omitted. |
| `env` | map | no | Environment variables to set in the container. |
| `wait_for_exit` | boolean | `true` | Whether to wait for the container to exit. |
| `timeout` | duration | `""` | Maximum time to wait for the container. |

**Output fields:** `exit_code`, `stdout`, `stderr`, `image`.

**Example:**

```yaml
steps:
  - name: run-tests
    type: step.docker_run
    config:
      image: "golang:1.25"
      command: ["go", "test", "./..."]
      env:
        CI: "true"
      timeout: "10m"
```

---

### `step.scan_sast`

Runs a Static Application Security Testing (SAST) scanner inside a Docker container and evaluates findings against a severity gate. Supports Semgrep and generic scanner commands.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `scanner` | string | yes | Scanner to use. Supported: `semgrep`. Generic commands also accepted. |
| `image` | string | `semgrep/semgrep:latest` | Docker image for the scanner. |
| `source_path` | string | `/workspace` | Path to the source code to scan. |
| `rules` | array of strings | no | Semgrep rule configs to apply (e.g., `auto`, `p/owasp-top-ten`). |
| `fail_on_severity` | string | `error` | Minimum severity that causes the step to fail (`error`, `warning`, `info`). |
| `output_format` | string | `sarif` | Output format: `sarif` or `json`. |

**Output fields:** `scan_result`, `command`, `image`.

**Example:**

```yaml
steps:
  - name: sast-scan
    type: step.scan_sast
    config:
      scanner: "semgrep"
      source_path: "/workspace/src"
      rules:
        - "p/owasp-top-ten"
        - "p/golang"
      fail_on_severity: "error"
```

---

### `step.scan_container`

Scans a container image for vulnerabilities using Trivy. Evaluates findings against a configurable severity threshold.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `target_image` | string | yes | Container image to scan (e.g., `myapp:latest`). |
| `scanner` | string | `trivy` | Scanner to use. |
| `severity_threshold` | string | `HIGH` | Minimum severity to report: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `INFO`. |
| `ignore_unfixed` | boolean | `false` | Skip vulnerabilities without a known fix. |
| `output_format` | string | `sarif` | Output format: `sarif` or `json`. |

**Output fields:** `scan_result`, `command`, `image`, `target_image`.

**Example:**

```yaml
steps:
  - name: scan-image
    type: step.scan_container
    config:
      target_image: "myapp:latest"
      severity_threshold: "HIGH"
      ignore_unfixed: true
```

---

### `step.scan_deps`

Scans project dependencies for known vulnerabilities using Grype. Evaluates findings against a severity gate.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `scanner` | string | `grype` | Scanner to use. |
| `image` | string | `anchore/grype:latest` | Docker image for the scanner. |
| `source_path` | string | `/workspace` | Path to the project source to scan. |
| `fail_on_severity` | string | `high` | Minimum severity that causes the step to fail: `critical`, `high`, `medium`, `low`, or `info`. |
| `output_format` | string | `sarif` | Output format: `sarif` or `json`. |

**Output fields:** `scan_result`, `command`, `image`.

**Example:**

```yaml
steps:
  - name: dep-scan
    type: step.scan_deps
    config:
      source_path: "/workspace"
      fail_on_severity: "high"
```

---

### `step.artifact_push`

Reads a file from `source_path` and stores it in the pipeline's artifact store. Computes a SHA-256 checksum of the artifact. Requires `artifact_store` and `execution_id` in pipeline metadata.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `source_path` | string | yes | Path to the file to store. |
| `key` | string | yes | Artifact key under which to store the file. |
| `dest` | string | `artifact_store` | Destination identifier (informational). |

**Output fields:** `key`, `size`, `checksum`, `dest`.

**Example:**

```yaml
steps:
  - name: upload-binary
    type: step.artifact_push
    config:
      source_path: "./bin/server"
      key: "server-binary"
```

---

### `step.artifact_pull`

Retrieves an artifact from a prior execution, a URL, or S3 and writes it to a local destination path.

**Configuration:**

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `source` | string | yes | Source type: `previous_execution`, `url`, or `s3`. |
| `dest` | string | yes | Local file path to write the artifact to. |
| `key` | string | yes (for `previous_execution`, `s3`) | Artifact key to retrieve. |
| `execution_id` | string | no | Specific execution ID to pull from. Defaults to current execution. |
| `url` | string | yes (for `url`) | URL to fetch the artifact from. |

**Output fields:** `source`, `key`, `dest`, `size`, `bytes_written`.

**Example:**

```yaml
steps:
  - name: download-binary
    type: step.artifact_pull
    config:
      source: "previous_execution"
      key: "server-binary"
      dest: "./bin/server"
```

---

### Admin Core Plugin (`plugin/admincore/`)

The `admincore` plugin is a NativePlugin that registers the built-in admin UI page definitions. It declares no HTTP routes -- all views are rendered entirely in the React frontend. Registering this plugin ensures navigation is driven by the plugin system with no static fallbacks.

**UI pages declared:**

| ID | Label | Category |
|----|-------|----------|
| `dashboard` | Dashboard | global |
| `editor` | Editor | global |
| `marketplace` | Marketplace | global |
| `templates` | Templates | global |
| `environments` | Environments | global |
| `settings` | Settings | global |
| `executions` | Executions | workflow |
| `logs` | Logs | workflow |
| `events` | Events | workflow |

Global pages appear in the main navigation. Workflow-scoped pages (`executions`, `logs`, `events`) are only shown when a workflow is open.

The plugin is auto-registered via `init()` in `plugin/admincore/plugin.go`. No YAML configuration is required.

---

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
