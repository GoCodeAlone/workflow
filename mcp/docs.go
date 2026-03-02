package mcp

// docsOverview contains the overview documentation for the workflow engine.
const docsOverview = `# GoCodeAlone/workflow Engine Overview

## What is the Workflow Engine?

The workflow engine is a configuration-driven application framework that allows you to build
complete applications entirely from YAML configuration files. The same codebase can operate as:

- **API servers** with authentication middleware
- **Event processing systems** with pub/sub messaging
- **Message-based communication systems** (Kafka, NATS, in-memory)
- **Scheduled job processors** with cron scheduling
- **State machines** for complex business logic
- **CI/CD pipelines** with build, test, and deploy steps

## Key Concepts

### Modules
Modules are the building blocks of a workflow application. Each module has a **type** (e.g.,
` + "`http.server`" + `, ` + "`messaging.broker`" + `) and a **name** (unique identifier). Modules are
declared in the ` + "`modules`" + ` section of the YAML config.

### Workflows
Workflows define how modules interact. Workflow handlers (e.g., ` + "`http`" + `, ` + "`messaging`" + `,
` + "`statemachine`" + `) configure routing, subscriptions, and state transitions.

### Triggers
Triggers start workflow execution. Types include:
- ` + "`http`" + ` - HTTP request triggers
- ` + "`schedule`" + ` - Cron-based scheduled triggers
- ` + "`event`" + ` - Event-driven triggers
- ` + "`eventbus`" + ` - Event bus triggers

### Pipelines
Pipelines are ordered sequences of steps that process data. Steps can be conditional,
parallel, or sequential. Each step type (e.g., ` + "`step.http_call`" + `, ` + "`step.transform`" + `,
` + "`step.shell_exec`" + `) performs a specific action.

### Plugins
The engine is extensible via plugins that provide additional module types, step types,
trigger types, and workflow handlers. Plugins can be:
- **Built-in** (compiled into the engine)
- **External** (separate binaries communicating via gRPC)

## Architecture

1. **Engine** creates and wires modules from config
2. **Plugins** register factories for modules, steps, triggers
3. **BuildFromConfig** instantiates everything from YAML
4. **Triggers** start the workflow (HTTP requests, schedules, events)
5. **Handlers** process work through configured pipelines
6. **Steps** execute individual operations within pipelines

## Getting Started

1. Create a YAML configuration file
2. Define your modules (server, router, handlers)
3. Configure workflows (routes, subscriptions)
4. Add triggers or pipelines as needed
5. Run with: ` + "`wfctl run -config your-config.yaml`" + `

## wfctl CLI Reference

` + "`wfctl`" + ` is the workflow engine CLI for building, validating, deploying, and managing workflow applications.

### Project Scaffolding

- ` + "`wfctl init`" + ` — Scaffold a new workflow project from a template
- ` + "`wfctl build-ui`" + ` — Build the application UI (npm install + npm run build + validate)

### Configuration

- ` + "`wfctl validate <config.yaml>`" + ` — Validate a workflow configuration file
- ` + "`wfctl inspect <config.yaml>`" + ` — Inspect modules, workflows, and triggers in a config
- ` + "`wfctl schema`" + ` — Generate JSON Schema for workflow configs
- ` + "`wfctl manifest <config.yaml>`" + ` — Analyze config and report infrastructure requirements (JSON/YAML output)
- ` + "`wfctl diff <old.yaml> <new.yaml>`" + ` — Compare configs, show changes, detect breaking changes with ` + "`-check-breaking`" + `
- ` + "`wfctl template validate`" + ` — Validate templates against known module/step type registry
- ` + "`wfctl contract test`" + ` — Generate API contracts from config, compare to baseline for breaking changes
- ` + "`wfctl compat check`" + ` — Check config compatibility with the current engine version

### Running

- ` + "`wfctl run -config <config.yaml>`" + ` — Run a workflow engine from a config file
- ` + "`wfctl pipeline list`" + ` / ` + "`wfctl pipeline run`" + ` — List and run pipelines
- ` + "`wfctl mcp`" + ` — Start the MCP server over stdio for AI assistant integration

### API & UI Tooling

- ` + "`wfctl api extract <config.yaml>`" + ` — Extract OpenAPI 3.0 spec from a workflow config (offline, no running server needed)
- ` + "`wfctl ui scaffold -spec <openapi.yaml>`" + ` — Generate a Vite+React+TypeScript SPA from an OpenAPI spec

### Deployment

#### Docker
- ` + "`wfctl deploy docker -config <config.yaml>`" + ` — Build Docker image and run locally via docker compose
  - ` + "`-image <name:tag>`" + ` — Image name (default: workflow-app:local)
  - ` + "`-no-compose`" + ` — Build image only, skip docker compose up

#### Kubernetes (Native Manifests via Server-Side Apply)
- ` + "`wfctl deploy k8s generate`" + ` — Generate Kubernetes manifests (Deployment, Service, ConfigMap)
  - ` + "`-config <file>`" + ` — Workflow config file (default: app.yaml)
  - ` + "`-image <name:tag>`" + ` — Container image (required)
  - ` + "`-namespace <ns>`" + ` — Kubernetes namespace (default: default)
  - ` + "`-output <dir>`" + ` — Output directory for manifests (default: ./k8s-generated/)
  - ` + "`-replicas <n>`" + ` — Number of replicas (default: 1)
  - ` + "`-secret <name>`" + ` — Secret name for environment variables
  - ` + "`-command <cmd>`" + ` — Container command (comma-separated)
  - ` + "`-args <args>`" + ` — Container args (comma-separated)
  - ` + "`-strategy <type>`" + ` — Deployment strategy: Recreate or RollingUpdate
  - ` + "`-health-path <path>`" + ` — Health check endpoint (default: /healthz)
  - ` + "`-service-account <name>`" + ` — Pod service account
  - ` + "`-image-pull-policy <policy>`" + ` — Never, Always, or IfNotPresent

- ` + "`wfctl deploy k8s apply`" + ` — Apply manifests to cluster via server-side apply
  - All flags from ` + "`generate`" + ` plus:
  - ` + "`--build`" + ` — Build Docker image and load into cluster before deploying
  - ` + "`--dockerfile <path>`" + ` — Dockerfile path (default: Dockerfile)
  - ` + "`--build-context <dir>`" + ` — Docker build context (default: .)
  - ` + "`--build-arg <ARGS>`" + ` — Docker build args (comma-separated KEY=VALUE pairs)
  - ` + "`--runtime <type>`" + ` — Override cluster runtime detection (minikube|kind|docker-desktop|k3d|remote)
  - ` + "`--registry <url>`" + ` — Registry for remote clusters (e.g. ghcr.io/org)
  - ` + "`--dry-run`" + ` — Server-side dry run without applying
  - ` + "`--wait`" + ` — Wait for rollout to complete
  - ` + "`--force`" + ` — Force take ownership of fields from other managers
  - Auto-detects cluster runtime from kubeconfig context (minikube, kind-*, docker-desktop, k3d-*)
  - Automatically loads images into local clusters (minikube image load, kind load docker-image, etc.)

- ` + "`wfctl deploy k8s status -app <name>`" + ` — Show deployment status
- ` + "`wfctl deploy k8s logs -app <name>`" + ` — View pod logs (` + "`-follow`" + `, ` + "`-tail`" + `)
- ` + "`wfctl deploy k8s destroy -app <name>`" + ` — Delete deployment resources
- ` + "`wfctl deploy k8s diff`" + ` — Show what would change vs. live cluster state

#### Helm
- ` + "`wfctl deploy kubernetes`" + ` — Deploy via Helm chart
  - ` + "`-namespace`" + `, ` + "`-release`" + `, ` + "`-chart`" + `, ` + "`-values`" + `, ` + "`-set`" + `, ` + "`-dry-run`" + `

#### Cloud
- ` + "`wfctl deploy cloud -target <staging|production>`" + ` — Deploy infrastructure to cloud environment
  - Reads cloud config from ` + "`.wfctl.yaml`" + ` or ` + "`deploy.yaml`" + `
  - Provisions cloud resources defined by ` + "`platform.*`" + ` modules in config
  - ` + "`-dry-run`" + ` — Show plan without applying
  - ` + "`-yes`" + ` — Skip confirmation prompt

### Plugin Management

- ` + "`wfctl plugin init`" + ` — Scaffold a new plugin project
- ` + "`wfctl plugin docs`" + ` — Generate documentation for a plugin
- ` + "`wfctl plugin test`" + ` — Run plugin through lifecycle test harness
- ` + "`wfctl plugin search <query>`" + ` — Search the plugin registry
- ` + "`wfctl plugin install <name>`" + ` — Install a plugin from registry
- ` + "`wfctl plugin list`" + ` — List installed plugins
- ` + "`wfctl plugin update <name>`" + ` — Update an installed plugin
- ` + "`wfctl plugin remove <name>`" + ` — Uninstall a plugin
- ` + "`wfctl publish`" + ` — Publish a plugin manifest to workflow-registry

### Database Migrations

- ` + "`wfctl migrate status -db <file>`" + ` — Show applied and pending migrations
- ` + "`wfctl migrate diff -db <file>`" + ` — Show pending migrations without applying
- ` + "`wfctl migrate apply -db <file>`" + ` — Apply pending migrations

### CI/CD & Git Integration

- ` + "`wfctl generate github-actions <config.yaml>`" + ` — Generate GitHub Actions CI/CD workflow files from config
- ` + "`wfctl git connect -repo <owner/repo>`" + ` — Link project to a GitHub repository (` + "`-init`" + ` to create repo)
- ` + "`wfctl git push -message <msg>`" + ` — Commit and push to configured repo (` + "`-tag <version>`" + ` to tag release)
`

// docsYAMLSyntax contains the YAML configuration syntax guide.
const docsYAMLSyntax = `# Workflow YAML Configuration Syntax

## Top-Level Structure

A workflow configuration YAML file has these top-level fields:

` + "```yaml" + `
# Optional: declare required plugins
requires:
  plugins:
    - name: workflow-plugin-http
      version: ">=1.0.0"

# Required: module definitions
modules:
  - name: <unique-name>
    type: <module-type>
    config:
      <key>: <value>
    dependsOn:
      - <other-module-name>

# Optional: workflow handler configurations
workflows:
  http:
    routes: [...]
  messaging:
    subscriptions: [...]
  statemachine:
    states: [...]

# Optional: trigger configurations
triggers:
  http:
    routes: [...]
  schedule:
    jobs: [...]

# Optional: pipeline definitions
pipelines:
  my-pipeline:
    timeout: 30s
    errorStrategy: stop
    steps:
      - name: step-1
        type: step.http_call
        config:
          url: "https://api.example.com"
          method: GET
` + "```" + `

## Module Configuration

Each module requires:
- ` + "`name`" + `: Unique identifier (alphanumeric, dots, dashes, underscores)
- ` + "`type`" + `: Module type identifier (use ` + "`list_module_types`" + ` tool to see all types)
- ` + "`config`" + `: Module-specific key-value configuration
- ` + "`dependsOn`" + `: (optional) List of module names this module depends on

### Example Modules

` + "```yaml" + `
modules:
  # HTTP server
  - name: webServer
    type: http.server
    config:
      address: ":8080"

  # Router
  - name: router
    type: http.router
    dependsOn: [webServer]

  # Request handler
  - name: userHandler
    type: http.handler
    config:
      contentType: "application/json"
      response: '{"status": "ok"}'
    dependsOn: [router]

  # Authentication middleware
  - name: authMiddleware
    type: http.middleware.auth
    config:
      type: jwt
      secret: "${JWT_SECRET}"
    dependsOn: [router]
` + "```" + `

## Workflow Configuration

### HTTP Workflows

` + "```yaml" + `
workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: userHandler
      - method: POST
        path: /api/users
        handler: createUserHandler
        middlewares:
          - authMiddleware
` + "```" + `

### Messaging Workflows

` + "```yaml" + `
workflows:
  messaging:
    subscriptions:
      - topic: user-events
        handler: userEventHandler
      - topic: order-events
        handler: orderEventHandler
` + "```" + `

### State Machine Workflows

` + "```yaml" + `
workflows:
  statemachine:
    initial: pending
    states:
      - name: pending
        transitions:
          - event: approve
            target: approved
          - event: reject
            target: rejected
      - name: approved
        transitions:
          - event: complete
            target: completed
      - name: rejected
      - name: completed
` + "```" + `

## Pipeline Configuration

` + "```yaml" + `
pipelines:
  process-order:
    timeout: 60s
    errorStrategy: stop  # stop, skip, or compensate
    steps:
      - name: validate-input
        type: step.validate
        config:
          schema: '{"type": "object", "required": ["orderId"]}'

      - name: fetch-order
        type: step.http_call
        config:
          url: "https://api.example.com/orders/{{.orderId}}"
          method: GET

      - name: transform-data
        type: step.transform
        config:
          template: '{"id": "{{.orderId}}", "status": "processed"}'

      - name: log-result
        type: step.log
        config:
          message: "Order {{.orderId}} processed"
          level: info
` + "```" + `

## Environment Variables and Secrets

Configuration values can reference environment variables using ` + "`${VAR_NAME}`" + ` syntax:

` + "```yaml" + `
modules:
  - name: dbConnection
    type: database.workflow
    config:
      connectionString: "${DATABASE_URL}"
      maxConnections: 10
` + "```" + `

## Multi-Workflow Applications

For larger applications, use an application config that references multiple workflow files:

` + "```yaml" + `
application:
  name: my-ecommerce-app
  workflows:
    - file: services/order-service.yaml
    - file: services/payment-service.yaml
    - file: services/notification-service.yaml
` + "```" + `
`
