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
