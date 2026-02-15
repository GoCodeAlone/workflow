# Trigger-Action Composable Workflow Architecture

## Status

**Proposed** -- Design document for transitioning the workflow engine from monolithic handler-based workflows to composable trigger-action pipelines.

---

## 1. Current Architecture Summary

### 1.1 How Workflows Work Today

The engine has three distinct concepts that process events:

**Modules** -- Instantiated from YAML `modules:` section. Each module is a `modular.Module` that registers itself as a service. There are 40+ built-in types (`http.server`, `statemachine.engine`, `processing.step`, `data.transformer`, etc.). Modules are infrastructure -- they provide capabilities but do not define how events flow.

**Workflow Handlers** -- Registered in `cmd/server/main.go` and matched against keys in the YAML `workflows:` section. Five handlers exist:

| Handler | `CanHandle()` | What it does |
|---------|---------------|--------------|
| `HTTPWorkflowHandler` | `"http"` or `"http-*"` | Wires routes to an HTTP router and server |
| `MessagingWorkflowHandler` | `"messaging"` | Subscribes message handlers to broker topics |
| `StateMachineWorkflowHandler` | `"statemachine"` | Registers state/transition definitions on an engine |
| `SchedulerWorkflowHandler` | `"scheduler"` | Schedules jobs on a scheduler service |
| `IntegrationWorkflowHandler` | `"integration"` | Configures external connectors and multi-step flows |

Each handler's `ConfigureWorkflow()` method reads its section of the YAML, finds the relevant services in the registry, and wires them together. Each handler's `ExecuteWorkflow()` method handles runtime dispatch.

**Triggers** -- Registered in `cmd/server/main.go` and configured from the YAML `triggers:` section. Four triggers exist:

| Trigger | Type key | What it does |
|---------|----------|--------------|
| `HTTPTrigger` | `"http"` | Registers HTTP routes that call `engine.TriggerWorkflow()` |
| `EventTrigger` | `"event"` | Subscribes to message broker topics and calls `engine.TriggerWorkflow()` |
| `ScheduleTrigger` | `"schedule"` | Registers cron jobs that call `engine.TriggerWorkflow()` |
| `EventBusTrigger` | `"eventbus"` | Subscribes to modular EventBus and calls `engine.TriggerWorkflow()` |

### 1.2 The Dispatch Flow

```
Trigger -> engine.TriggerWorkflow(ctx, workflowType, action, data)
             -> finds WorkflowHandler where CanHandle(workflowType) == true
                -> handler.ExecuteWorkflow(ctx, workflowType, action, data)
                   -> handler-specific logic (state transition, message publish, etc.)
```

### 1.3 What Works Well

- Modules are already composable building blocks with dependency injection
- Triggers already decouple "what starts a workflow" from "what the workflow does"
- The state machine + processing step + hooks pattern already provides a step execution model
- The `IntegrationWorkflowHandler` already has sequential step execution with retry, variable substitution, and error handling
- The `DataTransformer` already supports named transformation pipelines with extract/map/filter/convert operations
- The `FieldMapping` system already handles configurable field name resolution between components
- The `WorkflowEventEmitter` already publishes step-level lifecycle events to the EventBus

### 1.4 What Does Not Work Well

**Monolithic workflow handlers.** Each handler is a self-contained world. The HTTP handler wires routes. The messaging handler wires subscriptions. The state machine handler wires definitions. They do not compose -- you cannot say "when this HTTP endpoint is hit, validate the payload, then run it through the state machine, then publish to a topic." Instead, you must manually wire these together using hooks, processing steps, and implicit service lookups.

**Action routing is handler-internal.** `TriggerWorkflow()` dispatches to a single handler based on `workflowType`. There is no concept of "run step A, then step B, then step C" at the engine level. The integration handler has this internally, but it is not available to other workflow types.

**No explicit data flow between steps.** Data passes through `map[string]any` bags. The integration handler has `${variable}` substitution, but it is local to that handler. There is no unified way to say "the output of step A feeds into step B."

**Trigger-to-handler coupling.** A trigger fires `TriggerWorkflow(workflowType, action, data)` which maps 1:1 to a handler. You cannot chain: "trigger fires -> run three steps from different handler types."

---

## 2. Target Architecture

### 2.1 Core Concept: The Pipeline

A **pipeline** is an ordered sequence of **steps** that processes data flowing from a **trigger**. Each step receives the accumulated context from previous steps and produces output that merges into the context for subsequent steps.

```
Trigger -> Pipeline -> Step 1 -> Step 2 -> Step 3 -> ... -> Done
              |           |          |          |
              +--- ctx ---+--- ctx --+--- ctx --+--- ctx (final)
```

### 2.2 New Abstraction: `PipelineStep`

```go
// PipelineStep is a single composable unit of work in a pipeline.
type PipelineStep interface {
    // Name returns the step's unique name within the pipeline.
    Name() string

    // Execute runs the step with the pipeline context.
    // It receives accumulated data from previous steps and returns
    // its own output to be merged into the context.
    Execute(ctx context.Context, input *PipelineContext) (*StepResult, error)
}
```

```go
// PipelineContext carries data through a pipeline execution.
type PipelineContext struct {
    // TriggerData is the original data from the trigger (immutable).
    TriggerData map[string]any

    // StepOutputs maps step-name -> output from each completed step.
    StepOutputs map[string]map[string]any

    // Current is the merged state: trigger data + all step outputs.
    // Steps read from Current and their output is merged back into it.
    Current map[string]any

    // Metadata holds execution metadata (workflow ID, trace ID, etc.)
    Metadata map[string]any
}
```

```go
// StepResult is the output of a single step execution.
type StepResult struct {
    // Output is the data produced by this step.
    Output map[string]any

    // NextStep overrides the default next step (for conditional routing).
    // Empty string means continue to the next step in sequence.
    NextStep string

    // Stop indicates the pipeline should stop after this step.
    Stop bool
}
```

### 2.3 New Abstraction: `Pipeline`

```go
// Pipeline executes an ordered sequence of steps.
type Pipeline struct {
    Name     string
    Steps    []PipelineStep
    OnError  ErrorStrategy // "stop", "skip", "retry", "compensate"
    Timeout  time.Duration
}

// Execute runs the pipeline from trigger data.
func (p *Pipeline) Execute(ctx context.Context, triggerData map[string]any) (*PipelineContext, error)
```

The pipeline executor:
1. Creates a `PipelineContext` from the trigger data
2. Runs each step in order, passing the accumulated context
3. Merges each step's output into `Current`
4. Handles errors according to the configured strategy
5. Supports conditional routing via `StepResult.NextStep`
6. Emits lifecycle events via `WorkflowEventEmitter`

### 2.4 How It Connects to the Existing Engine

The pipeline does NOT replace `WorkflowHandler` or `TriggerWorkflow()`. Instead, a new `PipelineWorkflowHandler` wraps the pipeline executor:

```go
type PipelineWorkflowHandler struct {
    pipelines map[string]*Pipeline // pipeline name -> Pipeline
}

func (h *PipelineWorkflowHandler) CanHandle(workflowType string) bool {
    _, ok := h.pipelines[workflowType]
    return ok
}

func (h *PipelineWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType, action string, data map[string]any) (map[string]any, error) {
    pipeline := h.pipelines[workflowType]
    result, err := pipeline.Execute(ctx, data)
    if err != nil {
        return nil, err
    }
    return result.Current, nil
}
```

This means:
- Existing workflow handlers continue to work unchanged
- New composable pipelines are just another handler type
- Triggers dispatch to pipelines the same way they dispatch to existing handlers
- Both models coexist in the same config

### 2.5 Built-in Step Types

Each step type is a `PipelineStep` implementation that wraps an existing module or provides new functionality:

| Step Type | Description | Wraps |
|-----------|-------------|-------|
| `step.validate` | JSON Schema or custom validation | new |
| `step.transform` | Data transformation pipeline | `DataTransformer` |
| `step.http_call` | Make an HTTP request | `IntegrationConnector` |
| `step.db_query` | Execute a database query | `WorkflowDatabase` |
| `step.db_insert` | Insert/update database record | `WorkflowDatabase` |
| `step.publish` | Publish to message broker/EventBus | `MessageBroker` / `EventBus` |
| `step.state_transition` | Trigger a state machine transition | `StateMachineEngine` |
| `step.conditional` | Route to different steps based on field values | new |
| `step.parallel` | Execute multiple steps concurrently | new |
| `step.delay` | Wait for a duration or until a condition | new |
| `step.script` | Execute a dynamic Yaegi script | `dynamic.Loader` |
| `step.notify` | Send notification (Slack, email, webhook) | `SlackNotification` / `WebhookSender` |
| `step.log` | Log data at a specified level | new |
| `step.set` | Set/override values in the pipeline context | new |

---

## 3. YAML Config Format

### 3.1 New Top-Level Section: `pipelines`

Pipelines are defined alongside the existing `modules`, `workflows`, and `triggers` sections:

```yaml
modules:
  # ... module definitions (unchanged)

workflows:
  # ... existing workflow definitions (unchanged, still supported)

triggers:
  # ... existing trigger definitions (unchanged, still supported)

pipelines:
  # NEW: composable trigger-action workflows
  <pipeline-name>:
    trigger: <trigger-definition>
    steps:
      - <step-definitions>
    on_error: stop | skip | compensate
    timeout: 30s
```

### 3.2 Example: Stripe Webhook Processing

```yaml
modules:
  - name: order-db
    type: storage.sqlite
    config:
      dbPath: "data/orders.db"

  - name: order-broker
    type: messaging.broker

  - name: order-state-engine
    type: statemachine.engine

  - name: slack-alerts
    type: notification.slack
    config:
      webhookURL: "${SLACK_WEBHOOK_URL}"
      channel: "#payments"

pipelines:
  stripe-payments:
    trigger:
      type: http.webhook
      config:
        path: /webhooks/stripe
        method: POST

    steps:
      - name: validate-signature
        type: step.validate
        config:
          strategy: hmac_sha256
          secret: "${STRIPE_WEBHOOK_SECRET}"
          header: Stripe-Signature

      - name: parse-event
        type: step.transform
        config:
          operations:
            - type: extract
              config:
                path: "body"
            - type: map
              config:
                mappings:
                  type: event_type
                  data.object: payload

      - name: route-by-event
        type: step.conditional
        config:
          field: event_type
          routes:
            "payment_intent.succeeded": process-payment
            "payment_intent.failed": handle-failure
            "customer.created": create-customer
          default: log-unknown

      - name: process-payment
        type: step.db_insert
        config:
          database: order-db
          table: payments
          fields:
            stripe_id: "{{ .payload.id }}"
            amount: "{{ .payload.amount }}"
            currency: "{{ .payload.currency }}"
            status: "succeeded"

      - name: notify-success
        type: step.publish
        config:
          broker: order-broker
          topic: "payment.succeeded"

      - name: handle-failure
        type: step.notify
        config:
          provider: slack-alerts
          template: "Payment failed: {{ .payload.id }} - {{ .payload.last_payment_error.message }}"

      - name: log-unknown
        type: step.log
        config:
          level: warn
          message: "Unhandled Stripe event: {{ .event_type }}"

    on_error: stop
    timeout: 30s
```

### 3.3 Example: Order Processing Pipeline (Composable Version)

This shows the same order pipeline from `order-processing-pipeline.yaml` rewritten as a composable pipeline:

```yaml
modules:
  - name: order-db
    type: storage.sqlite
    config:
      dbPath: "data/orders.db"

  - name: order-state-engine
    type: statemachine.engine

  - name: order-broker
    type: messaging.broker

  - name: order-metrics
    type: metrics.collector

pipelines:
  order-processing:
    trigger:
      type: http.endpoint
      config:
        path: /api/orders
        method: POST

    steps:
      - name: validate-order
        type: step.validate
        config:
          schema:
            type: object
            required: [customer_id, items, total]
            properties:
              customer_id: { type: string }
              items: { type: array, minItems: 1 }
              total: { type: number, minimum: 0 }

      - name: enrich-order
        type: step.transform
        config:
          operations:
            - type: map
              config:
                mappings:
                  customer_id: customerId
            - type: extract
              config:
                path: "items"

      - name: create-workflow-instance
        type: step.state_transition
        config:
          engine: order-state-engine
          workflow: order-lifecycle
          action: create
          initial_data:
            customerId: "{{ .customerId }}"
            items: "{{ .items }}"
            total: "{{ .total }}"

      - name: validate-transition
        type: step.state_transition
        config:
          engine: order-state-engine
          instance: "{{ .steps.create-workflow-instance.instanceId }}"
          transition: validate_order

      - name: persist-order
        type: step.db_insert
        config:
          database: order-db
          table: orders
          fields:
            id: "{{ .steps.create-workflow-instance.instanceId }}"
            customer_id: "{{ .customerId }}"
            total: "{{ .total }}"
            status: "validated"

      - name: publish-event
        type: step.publish
        config:
          broker: order-broker
          topic: "order.validated"
          payload:
            orderId: "{{ .steps.create-workflow-instance.instanceId }}"
            customerId: "{{ .customerId }}"

    on_error: compensate
    timeout: 60s

    compensation:
      - name: rollback-state
        type: step.state_transition
        config:
          engine: order-state-engine
          instance: "{{ .steps.create-workflow-instance.instanceId }}"
          transition: fail_validation
```

### 3.4 Example: Cron-Triggered Data Sync

```yaml
pipelines:
  nightly-sync:
    trigger:
      type: schedule
      config:
        cron: "0 2 * * *"  # 2 AM daily

    steps:
      - name: fetch-external-data
        type: step.http_call
        config:
          url: "https://api.partner.com/v1/orders"
          method: GET
          headers:
            Authorization: "Bearer ${PARTNER_API_KEY}"
          timeout: 30s

      - name: transform-records
        type: step.transform
        config:
          operations:
            - type: extract
              config:
                path: "data.orders"
            - type: filter
              config:
                fields: [id, status, amount, customer_email]

      - name: upsert-records
        type: step.db_query
        config:
          database: sync-db
          query: |
            INSERT INTO partner_orders (id, status, amount, email)
            VALUES ({{ .id }}, {{ .status }}, {{ .amount }}, {{ .customer_email }})
            ON CONFLICT (id) DO UPDATE SET status = {{ .status }}, amount = {{ .amount }}
          iterate: true  # run query for each record in the input array

      - name: notify-complete
        type: step.notify
        config:
          provider: slack-ops
          template: "Nightly sync complete: {{ len .steps.transform-records.output }} records processed"

    on_error: stop
    timeout: 300s
```

### 3.5 Example: EventBus-Triggered Cross-Workflow Pipeline

```yaml
pipelines:
  fulfillment-on-order:
    trigger:
      type: eventbus
      config:
        topic: "order.validated"

    steps:
      - name: check-inventory
        type: step.http_call
        config:
          url: "http://inventory-service/api/check"
          method: POST
          body:
            items: "{{ .items }}"

      - name: route-by-stock
        type: step.conditional
        config:
          field: "steps.check-inventory.in_stock"
          routes:
            "true": reserve-inventory
            "false": backorder-notify

      - name: reserve-inventory
        type: step.http_call
        config:
          url: "http://inventory-service/api/reserve"
          method: POST
          body:
            orderId: "{{ .orderId }}"
            items: "{{ .items }}"

      - name: backorder-notify
        type: step.notify
        config:
          provider: slack-alerts
          template: "Order {{ .orderId }} has backordered items"

      - name: update-order-state
        type: step.state_transition
        config:
          engine: order-state-engine
          instance: "{{ .orderId }}"
          transition: start_processing
```

---

## 4. New Module Types Needed

### 4.1 Trigger Modules (inline in pipeline config)

These are NOT new module types in the `modules:` section. They are inline trigger definitions within a `pipelines:` block. Internally, they reuse the existing trigger infrastructure.

| Inline Trigger | Maps to | Notes |
|---------------|---------|-------|
| `http.webhook` | `HTTPTrigger` | Single-endpoint POST receiver with optional secret validation |
| `http.endpoint` | `HTTPTrigger` | Single REST endpoint (any method) |
| `schedule` | `ScheduleTrigger` | Cron-based schedule |
| `eventbus` | `EventBusTrigger` | EventBus topic subscription |
| `event` | `EventTrigger` | Message broker topic subscription |

Implementation: the `PipelineWorkflowHandler.ConfigureWorkflow()` method reads the inline trigger, creates the appropriate trigger type, and registers it with the engine to call `TriggerWorkflow()` targeting this pipeline.

### 4.2 Step Modules

These are new module implementations in `workflow/module/pipeline_step_*.go`:

| Module | File | Priority | Notes |
|--------|------|----------|-------|
| `PipelineExecutor` | `pipeline_executor.go` | P0 | Core pipeline execution engine |
| `ValidateStep` | `pipeline_step_validate.go` | P0 | JSON Schema + HMAC + custom validation |
| `TransformStep` | `pipeline_step_transform.go` | P0 | Wraps existing `DataTransformer` |
| `ConditionalStep` | `pipeline_step_conditional.go` | P0 | Field-based routing |
| `PublishStep` | `pipeline_step_publish.go` | P0 | Publish to broker/EventBus |
| `StateTransitionStep` | `pipeline_step_state.go` | P1 | Trigger state machine transition |
| `HTTPCallStep` | `pipeline_step_http.go` | P1 | Outbound HTTP request |
| `DBQueryStep` | `pipeline_step_db.go` | P1 | Database read/write |
| `NotifyStep` | `pipeline_step_notify.go` | P1 | Slack/email/webhook notification |
| `SetStep` | `pipeline_step_set.go` | P1 | Set values in context |
| `LogStep` | `pipeline_step_log.go` | P2 | Structured logging |
| `DelayStep` | `pipeline_step_delay.go` | P2 | Wait/sleep |
| `ParallelStep` | `pipeline_step_parallel.go` | P2 | Concurrent step execution |
| `ScriptStep` | `pipeline_step_script.go` | P2 | Yaegi dynamic script execution |

### 4.3 No Changes to Existing Module Types

The existing 40+ module types remain unchanged. Pipeline steps reference them by service name (e.g., `database: order-db` in a `step.db_insert` resolves the `order-db` module from the service registry).

---

## 5. Engine Changes Required

### 5.1 Config Changes

Add `Pipelines` field to `WorkflowConfig`:

```go
// In config/config.go
type WorkflowConfig struct {
    Modules   []ModuleConfig   `json:"modules" yaml:"modules"`
    Workflows map[string]any   `json:"workflows" yaml:"workflows"`
    Triggers  map[string]any   `json:"triggers" yaml:"triggers"`
    Pipelines map[string]any   `json:"pipelines" yaml:"pipelines"` // NEW
    ConfigDir string           `json:"-" yaml:"-"`
}
```

### 5.2 Pipeline Configuration Structs

```go
// In config/pipeline.go (new file)
type PipelineConfig struct {
    Trigger      PipelineTriggerConfig `json:"trigger" yaml:"trigger"`
    Steps        []PipelineStepConfig  `json:"steps" yaml:"steps"`
    OnError      string                `json:"on_error" yaml:"on_error"`
    Timeout      string                `json:"timeout" yaml:"timeout"`
    Compensation []PipelineStepConfig  `json:"compensation,omitempty" yaml:"compensation,omitempty"`
}

type PipelineTriggerConfig struct {
    Type   string         `json:"type" yaml:"type"`
    Config map[string]any `json:"config" yaml:"config"`
}

type PipelineStepConfig struct {
    Name   string         `json:"name" yaml:"name"`
    Type   string         `json:"type" yaml:"type"`
    Config map[string]any `json:"config" yaml:"config"`
}
```

### 5.3 Engine Build Changes

In `engine.go`, `BuildFromConfig()` gains a new section after workflow handler configuration:

```go
// Configure pipelines (new section)
if err := e.configurePipelines(cfg.Pipelines); err != nil {
    return fmt.Errorf("failed to configure pipelines: %w", err)
}
```

The `configurePipelines()` method:
1. Parses each pipeline config into `PipelineConfig` structs
2. For each pipeline, creates `PipelineStep` instances by looking up step type factories
3. Creates a `Pipeline` with the ordered steps
4. Registers the pipeline with a `PipelineWorkflowHandler`
5. Creates the appropriate trigger from the inline trigger config
6. The trigger calls `engine.TriggerWorkflow()` with the pipeline name as `workflowType`

### 5.4 Template Expression Engine

Steps reference data from previous steps using template expressions: `{{ .fieldName }}` or `{{ .steps.step-name.field }}`.

This requires a lightweight template evaluation layer:

```go
// In module/pipeline_template.go (new file)
type TemplateEngine struct{}

// Resolve evaluates a template string against a PipelineContext.
// Supports:
//   {{ .fieldName }}                    -- from Current
//   {{ .steps.step-name.field }}        -- from a specific step's output
//   {{ .trigger.field }}                -- from original trigger data
//   {{ len .steps.step-name.output }}   -- Go template functions
func (te *TemplateEngine) Resolve(template string, ctx *PipelineContext) (any, error)

// ResolveMap evaluates all string values in a map that contain {{ }} expressions.
func (te *TemplateEngine) ResolveMap(data map[string]any, ctx *PipelineContext) (map[string]any, error)
```

Implementation uses Go's `text/template` package with the `PipelineContext` as the data source.

### 5.5 Step Type Registry

A new registry maps step type strings to factory functions:

```go
// In module/pipeline_step_registry.go (new file)
type StepFactory func(name string, config map[string]any, app modular.Application) (PipelineStep, error)

type StepRegistry struct {
    factories map[string]StepFactory
}

func NewStepRegistry() *StepRegistry
func (r *StepRegistry) Register(stepType string, factory StepFactory)
func (r *StepRegistry) Create(stepType, name string, config map[string]any, app modular.Application) (PipelineStep, error)
```

The engine registers built-in step types during initialization, and users can register custom step types via `engine.AddStepType()`.

### 5.6 Registration in main.go

```go
// In cmd/server/main.go, inside buildEngine():

// Register the pipeline workflow handler
pipelineHandler := handlers.NewPipelineWorkflowHandler()
engine.RegisterWorkflowHandler(pipelineHandler)

// Register built-in step types
engine.AddStepType("step.validate", module.NewValidateStepFactory())
engine.AddStepType("step.transform", module.NewTransformStepFactory())
engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
engine.AddStepType("step.publish", module.NewPublishStepFactory())
engine.AddStepType("step.state_transition", module.NewStateTransitionStepFactory())
engine.AddStepType("step.http_call", module.NewHTTPCallStepFactory())
engine.AddStepType("step.db_query", module.NewDBQueryStepFactory())
engine.AddStepType("step.db_insert", module.NewDBInsertStepFactory())
engine.AddStepType("step.notify", module.NewNotifyStepFactory())
engine.AddStepType("step.set", module.NewSetStepFactory())
engine.AddStepType("step.log", module.NewLogStepFactory())
engine.AddStepType("step.delay", module.NewDelayStepFactory())
engine.AddStepType("step.parallel", module.NewParallelStepFactory())
engine.AddStepType("step.script", module.NewScriptStepFactory())
```

---

## 6. Backwards Compatibility

### 6.1 Zero Breaking Changes

The `pipelines:` section is entirely optional. Existing configs with only `modules:`, `workflows:`, and `triggers:` continue to work exactly as before. The engine checks for `pipelines:` only if it is present.

### 6.2 Coexistence

A single config can use both models:

```yaml
modules:
  - name: my-server
    type: http.server
    config: { address: ":8080" }
  - name: my-router
    type: http.router
    dependsOn: [my-server]
  - name: my-handler
    type: http.handler

# Old model: handler-based workflow
workflows:
  http:
    server: my-server
    router: my-router
    routes:
      - method: GET
        path: /api/status
        handler: my-handler

# New model: composable pipeline
pipelines:
  webhook-processor:
    trigger:
      type: http.endpoint
      config:
        path: /api/webhooks
        method: POST
    steps:
      - name: validate
        type: step.validate
        config: { ... }
      - name: process
        type: step.transform
        config: { ... }
```

Both the HTTP workflow handler and the pipeline handler register their routes on the same router. They do not conflict because they listen on different paths.

### 6.3 Shared Services

Pipeline steps look up modules from the same service registry. A `step.state_transition` referencing `engine: order-state-engine` resolves the same module that the `statemachine` workflow handler configured. This means pipelines can interoperate with handler-based workflows -- a pipeline can publish an event that an existing event trigger picks up and routes to an existing handler.

---

## 7. Migration Path

### Phase 1: Core Infrastructure (P0)

1. Add `Pipelines map[string]any` to `WorkflowConfig`
2. Implement `PipelineContext`, `PipelineStep` interface, `StepResult`
3. Implement `PipelineExecutor` with sequential step execution
4. Implement template expression engine
5. Implement step type registry with factory pattern
6. Implement `PipelineWorkflowHandler` that wraps `PipelineExecutor`
7. Implement inline trigger creation from pipeline trigger config
8. Add `configurePipelines()` to `engine.go`
9. Implement P0 step types: `step.validate`, `step.transform`, `step.conditional`, `step.publish`
10. Register everything in `cmd/server/main.go`
11. Add example pipeline YAML configs

**Files created:**
- `config/pipeline.go` -- config structs
- `module/pipeline_context.go` -- PipelineContext, StepResult
- `module/pipeline_step.go` -- PipelineStep interface
- `module/pipeline_executor.go` -- Pipeline, PipelineExecutor
- `module/pipeline_template.go` -- TemplateEngine
- `module/pipeline_step_registry.go` -- StepRegistry
- `module/pipeline_step_validate.go` -- ValidateStep
- `module/pipeline_step_transform.go` -- TransformStep
- `module/pipeline_step_conditional.go` -- ConditionalStep
- `module/pipeline_step_publish.go` -- PublishStep
- `handlers/pipeline.go` -- PipelineWorkflowHandler

**Files modified:**
- `config/config.go` -- add Pipelines field
- `engine.go` -- add configurePipelines(), step type registry
- `cmd/server/main.go` -- register handler and step types

### Phase 2: Extended Steps (P1)

12. Implement `step.state_transition` -- wraps `StateMachineEngine`
13. Implement `step.http_call` -- wraps `IntegrationConnector` or standalone HTTP client
14. Implement `step.db_query` / `step.db_insert` -- wraps `WorkflowDatabase`
15. Implement `step.notify` -- wraps `SlackNotification` / `WebhookSender`
16. Implement `step.set` -- context value manipulation
17. Add compensation flow support to `PipelineExecutor`
18. Add more example configs

### Phase 3: Advanced Steps (P2)

19. Implement `step.parallel` -- concurrent step execution with fan-out/fan-in
20. Implement `step.delay` -- wait/timer support
21. Implement `step.script` -- Yaegi dynamic script execution
22. Implement `step.log` -- structured logging step
23. Add pipeline visualization support to the UI (ReactFlow nodes for steps)

### Phase 4: UI Integration (P3)

24. Add pipeline builder to the workflow UI
25. Render pipeline steps as connected ReactFlow nodes
26. Drag-and-drop step creation from a palette of step types
27. Live pipeline execution monitoring (step status, data flow)
28. Pipeline YAML import/export

---

## 8. Relationship to Existing Patterns

### 8.1 Processing Steps vs Pipeline Steps

The existing `ProcessingStep` module bridges dynamic components to state machine transitions. It implements `TransitionHandler` and fires transitions on success/failure. Pipeline steps are a different abstraction -- they are not tied to state machines and do not fire transitions unless explicitly configured via `step.state_transition`.

However, a `step.script` (Yaegi) is functionally equivalent to a `ProcessingStep` backed by a `dynamic.component`. The key difference is that pipeline steps participate in a sequential data flow, while processing steps are fire-and-forget hooks on state transitions.

### 8.2 Integration Steps vs Pipeline Steps

The `IntegrationWorkflowHandler` already has sequential step execution with retry, variable substitution, and error handling via `ExecuteIntegrationWorkflow()`. Pipeline steps generalize this pattern:

- Integration steps are limited to external connectors (`IntegrationConnector`)
- Pipeline steps can be any operation (validate, transform, state transition, publish, etc.)
- Integration steps use `${variable}` substitution
- Pipeline steps use `{{ .variable }}` template expressions (more powerful, supports functions)
- Integration steps are internal to the integration handler
- Pipeline steps are engine-level and available to all pipeline types

### 8.3 DataTransformer Pipelines vs Pipeline Steps

The `DataTransformer` has its own concept of "pipeline" -- a named sequence of transform operations (extract, map, filter, convert). This is a different granularity. A `step.transform` pipeline step wraps a `DataTransformer` pipeline, running it as one step in the larger pipeline.

---

## 9. Open Questions

1. **Template syntax**: `{{ .field }}` (Go templates) vs `${field}` (shell-style, already used in integration handler) vs `${{ field }}` (GitHub Actions style). Recommendation: use Go template syntax `{{ .field }}` for pipeline steps since it supports functions and conditionals, and keep `${ENV_VAR}` for environment variable expansion in module config (as it currently works).

2. **Parallel step semantics**: When `step.parallel` runs multiple sub-steps, how are their outputs merged? Options: (a) merge all outputs into a single map with step-name prefixes, (b) use an array of outputs, (c) configurable merge strategy. Recommendation: option (a) with step-name prefixes matching the `steps.<name>` convention.

3. **Error handling granularity**: Should each step have its own `on_error` override, or is pipeline-level `on_error` sufficient? Recommendation: both -- pipeline-level default, per-step override.

4. **Pipeline-to-pipeline chaining**: Should one pipeline be able to invoke another? This would enable sub-pipeline reuse. Recommendation: yes, via a `step.pipeline` step type (future P3 work).

5. **Timeout semantics**: Pipeline-level timeout vs per-step timeout. Recommendation: both. Pipeline timeout is a hard cap; per-step timeout defaults to pipeline timeout / number of steps but can be overridden.

---

## 10. Summary

The composable pipeline architecture layers on top of the existing engine without breaking anything. It introduces three new concepts:

- **PipelineStep**: A small, reusable unit of work
- **Pipeline**: An ordered sequence of steps with error handling
- **PipelineContext**: A data bag that flows through the pipeline

These map directly to what users see in YAML:

```yaml
pipelines:
  my-workflow:
    trigger: { ... }      # What starts it
    steps:                 # What it does (ordered)
      - name: step-1
        type: step.X
        config: { ... }
    on_error: stop         # What happens on failure
```

The implementation reuses existing modules (state machine, message broker, database, HTTP client, data transformer) as the backing services for pipeline steps. No existing functionality is removed or changed.
