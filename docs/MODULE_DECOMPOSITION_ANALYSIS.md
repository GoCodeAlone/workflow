# Module Decomposition Analysis

Analysis of the workflow engine's module architecture, identifying monolithic components and recommending a path toward composable, trigger-driven workflows.

**Date:** 2026-02-14
**Scope:** `/home/jon/workspace/workflow/`

---

## 1. Module Catalog

### 1.1 Built-in Module Types (from `engine.go` BuildFromConfig switch)

| # | Module Type | File(s) | Purpose | Composable? | Notes |
|---|---|---|---|---|---|
| 1 | `http.server` | `module/http_server.go` | Starts an HTTP listener | Yes | Single-purpose infrastructure |
| 2 | `http.router` | `module/http_router.go` | Routes HTTP requests to handlers | Yes | Clean abstraction |
| 3 | `http.handler` | `module/http_handlers.go` | Simple HTTP handler (content type) | Yes | Thin wrapper |
| 4 | `api.handler` | `module/api_handlers.go` | REST CRUD + state machine + persistence + event publishing + risk assessment + routing | **No** | **Major monolith** -- 2256 lines, 10+ concerns |
| 5 | `http.middleware.auth` | `module/auth_middleware.go` | Auth middleware for HTTP | Yes | Clean |
| 6 | `http.middleware.logging` | `module/http_middleware.go` | Request logging middleware | Yes | Clean |
| 7 | `http.middleware.ratelimit` | `module/http_middleware.go` | Rate limiting middleware | Yes | Clean |
| 8 | `http.middleware.cors` | `module/http_middleware.go` | CORS middleware | Yes | Clean |
| 9 | `http.middleware.requestid` | `module/request_id.go` | Request ID middleware | Yes | Clean |
| 10 | `http.middleware.securityheaders` | `module/security_headers.go` | Security headers middleware | Yes | Clean |
| 11 | `messaging.broker` | `module/memory_broker.go` | In-memory message broker | Yes | Single-purpose |
| 12 | `messaging.broker.eventbus` | `module/eventbus_bridge.go` | EventBus bridge to message broker | Yes | Adapter pattern |
| 13 | `messaging.handler` | `module/message_handlers.go` | Simple message handler | Yes | Single-purpose |
| 14 | `messaging.nats` | `module/nats_broker.go` | NATS message broker | Yes | Single-purpose |
| 15 | `messaging.kafka` | `module/kafka_broker.go` | Kafka message broker | Yes | Single-purpose |
| 16 | `statemachine.engine` | `module/state_machine.go` | State machine engine | Partially | Core primitive, well-factored |
| 17 | `state.tracker` | `module/state_tracker.go` | Tracks entity states | Yes | Single-purpose |
| 18 | `state.connector` | `module/state_connector.go` | Maps resources to state machines | Yes | Wiring utility |
| 19 | `http.proxy` / `reverseproxy` | modular-fork reverseproxy | Reverse proxy (modular fork v2) | Yes | External module |
| 20 | `http.simple_proxy` | `module/simple_proxy.go` | Simple reverse proxy with target map | Yes | Single-purpose |
| 21 | `httpserver.modular` | modular-fork httpserver | HTTP server (modular framework) | Yes | External module |
| 22 | `scheduler.modular` | modular-fork scheduler | Scheduler (modular framework) | Yes | External module |
| 23 | `auth.modular` | modular-fork auth | Auth (modular framework) | Yes | External module |
| 24 | `eventbus.modular` | modular-fork eventbus | EventBus (modular framework) | Yes | External module |
| 25 | `cache.modular` | modular-fork cache | Cache (modular framework) | Yes | External module |
| 26 | `chimux.router` | modular-fork chimux | Chi mux router (modular framework) | Yes | External module |
| 27 | `eventlogger.modular` | modular-fork eventlogger | Event logger (modular framework) | Yes | External module |
| 28 | `httpclient.modular` | modular-fork httpclient | HTTP client (modular framework) | Yes | External module |
| 29 | `database.modular` | modular-fork database | Database (modular framework) | Yes | External module |
| 30 | `jsonschema.modular` | modular-fork jsonschema | JSON schema validator (modular framework) | Yes | External module |
| 31 | `metrics.collector` | `module/metrics.go` | Prometheus-style metrics | Yes | Single-purpose |
| 32 | `health.checker` | `module/health.go` | Health check endpoints | Yes | Single-purpose |
| 33 | `log.collector` | `module/log_collector.go` | Log collection | Yes | Single-purpose |
| 34 | `dynamic.component` | `dynamic/` | Yaegi-loaded Go components | Yes | The composability primitive |
| 35 | `database.workflow` | `module/database.go` | Workflow database (SQL) | Yes | Single-purpose |
| 36 | `data.transformer` | `module/data_transformer.go` | Data transformation | Yes | Single-purpose |
| 37 | `webhook.sender` | `module/webhook_sender.go` | Outbound webhook sender | Yes | Single-purpose |
| 38 | `notification.slack` | `module/slack_notification.go` | Slack notifications | Yes | Single-purpose |
| 39 | `storage.s3` | `module/s3_storage.go` | S3 storage | Yes | Single-purpose |
| 40 | `storage.local` | `module/storage_local.go` | Local filesystem storage | Yes | Single-purpose |
| 41 | `storage.gcs` | `module/storage_gcs.go` | Google Cloud Storage | Yes | Single-purpose |
| 42 | `storage.sqlite` | `module/sqlite_storage.go` | SQLite storage | Yes | Single-purpose |
| 43 | `observability.otel` | `module/otel_tracing.go` | OpenTelemetry tracing | Yes | Single-purpose |
| 44 | `static.fileserver` | `module/static_fileserver.go` | Static file serving (SPA) | Yes | Single-purpose |
| 45 | `persistence.store` | `module/persistence.go` | Key-value persistence | Yes | Single-purpose |
| 46 | `auth.jwt` | `module/jwt_auth.go` | JWT auth + user CRUD + setup wizard + admin management | **No** | **Monolith** -- 14 HTTP routes, 6+ concerns |
| 47 | `processing.step` | `module/processing_step.go` | Bridges dynamic components to state machine transitions | Yes | The composability bridge |
| 48 | `secrets.vault` | `module/secrets_vault.go` | HashiCorp Vault secrets | Yes | Single-purpose |
| 49 | `secrets.aws` | `module/secrets_aws.go` | AWS Secrets Manager | Yes | Single-purpose |
| 50 | `auth.user-store` | `module/auth_user_store.go` | User store (backing for JWT auth) | Yes | Single-purpose |
| 51 | `workflow.registry` | `module/workflow_registry.go` | Workflow definition registry | Yes | Single-purpose |
| 52 | `openapi.generator` | `module/openapi_generator.go` | OpenAPI spec generation | Yes | Single-purpose |
| 53 | `openapi.consumer` | `module/openapi_consumer.go` | OpenAPI spec consumption | Yes | Single-purpose |

### 1.2 Non-Module Handlers (wired outside BuildFromConfig)

| Handler | File | Purpose | Monolithic? |
|---|---|---|---|
| `V1APIHandler` | `module/api_v1_handler.go` + `api_v1_store.go` | Admin API: companies, orgs, projects, workflows, dashboard, versions, deploy/stop | **Yes** -- 20+ routes, 6 entity types |
| `WorkflowUIHandler` | `module/api_workflow_ui.go` | Management UI API: config CRUD, modules list, validate, reload, status | **Moderate** -- 6 routes, reasonable scope |
| `WorkspaceHandler` | `module/workspace_handler.go` | File management for project workspaces | Yes (but focused) |
| `dynamic.APIHandler` | `dynamic/api.go` | Dynamic component CRUD API | Yes (but focused) |

### 1.3 Workflow Handlers (from `handlers/`)

| Handler | File | Handles Type | Has Step Execution? | Notes |
|---|---|---|---|---|
| `HTTPWorkflowHandler` | `handlers/http.go` | `"http"` | No -- just wires routes | Route-wiring only, no pipeline |
| `MessagingWorkflowHandler` | `handlers/messaging.go` | `"messaging"` | No -- subscribes handlers to topics | Subscription-wiring only |
| `StateMachineWorkflowHandler` | `handlers/state_machine.go` | `"statemachine"` | Yes -- via transition hooks | **Best composability model** |
| `SchedulerWorkflowHandler` | `handlers/scheduler.go` | `"scheduler"` | No -- schedules jobs | Job-wiring only |
| `IntegrationWorkflowHandler` | `handlers/integration.go` | `"integration"` | Yes -- sequential steps with retry | Has pipeline execution model |
| `EventWorkflowHandler` | `handlers/events.go` | `"event"` | No -- pattern matching + handler dispatch | CEP pattern matching |

### 1.4 Trigger Types (from `module/`)

| Trigger | File | Config Key | Dispatches To |
|---|---|---|---|
| `HTTPTrigger` | `module/http_trigger.go` | `"http"` | `engine.TriggerWorkflow()` |
| `EventTrigger` | `module/event_trigger.go` | `"event"` | `engine.TriggerWorkflow()` |
| `ScheduleTrigger` | `module/schedule_trigger.go` | `"schedule"` | `engine.TriggerWorkflow()` |
| `EventBusTrigger` | `module/eventbus_trigger.go` | `"eventbus"` | `engine.TriggerWorkflow()` |

---

## 2. Decomposition Candidates

### 2.1 CRITICAL: `api.handler` (RESTAPIHandler) -- 2256 lines, 10+ concerns

**File:** `/home/jon/workspace/workflow/module/api_handlers.go`

This is the single largest monolith in the codebase. It bundles:

1. **CRUD operations** (handleGet, handleGetAll, handlePost, handlePut, handleDelete)
2. **State machine bridge** (handleTransition, startWorkflowForResource, syncResourceStateFromEngine)
3. **Sub-action dispatch** (handleSubAction -- assign, transfer, escalate, close, tag, messages)
4. **Risk assessment** (assessRiskLevel -- hardcoded keyword matching for crisis detection)
5. **Conversation routing** (resolveConversationRouting -- hardcoded keyword/shortcode/provider mapping)
6. **Cross-resource bridging** (bridgeToConversation -- creates conversation resources from webhook data)
7. **Persistence sync** (syncFromPersistence, write-through persistence logic)
8. **Auth/filtering** (extractUserID, extractAuthClaims, affiliate/program filtering)
9. **Queue health** (handleQueueHealth -- aggregated program-level statistics)
10. **Seed data loading** (loadSeedData)
11. **Field mapping** (configurable field name resolution)
12. **Event publishing** (publish to message broker on CRUD operations)
13. **Summary sub-resource** (handleSubActionGet)

**Decomposition plan:**

| New Module Type | Extracted From | Purpose |
|---|---|---|
| `api.crud` | handleGet/Post/Put/Delete | Pure CRUD with persistence |
| `api.statemachine-bridge` | handleTransition, startWorkflow* | Bridges REST resources to state machine |
| `risk.assessor` | assessRiskLevel | Content analysis (should be a `dynamic.component`) |
| `conversation.router` | resolveConversationRouting | Message routing (already partially exists as dynamic component) |
| `resource.bridge` | bridgeToConversation | Cross-resource synchronization |
| `queue.health` | handleQueueHealth | Queue health aggregation endpoint |

### 2.2 HIGH: `auth.jwt` (JWTAuthModule) -- 14 routes, 6+ concerns

**File:** `/home/jon/workspace/workflow/module/jwt_auth.go`

Bundles:
1. **Token validation** (Authenticate -- the AuthProvider interface)
2. **User registration** (handleRegister)
3. **Login/logout** (handleLogin, handleLogout)
4. **Token refresh** (handleRefresh)
5. **Profile management** (handleGetProfile, handleUpdateProfile)
6. **Setup wizard** (handleSetupStatus, handleSetup)
7. **User CRUD** (handleListUsers, handleCreateUser, handleDeleteUser)
8. **Role management** (handleUpdateUserRole)
9. **User store** (in-memory map or delegated to external UserStore)
10. **Persistence** (optional write-through)

**Decomposition plan:**

| New Module Type | Extracted From | Purpose |
|---|---|---|
| `auth.token-validator` | Authenticate() | Pure JWT validation (AuthProvider) |
| `auth.login` | handleLogin, handleLogout, handleRefresh | Auth flow endpoints |
| `auth.user-management` | handleListUsers, handleCreateUser, handleDeleteUser, handleUpdateUserRole | Admin user CRUD |
| `auth.profile` | handleGetProfile, handleUpdateProfile | Self-service profile |
| `auth.setup` | handleSetupStatus, handleSetup | First-run bootstrap |

### 2.3 HIGH: `V1APIHandler` -- 20+ routes, 6 entity types

**File:** `/home/jon/workspace/workflow/module/api_v1_handler.go`

Bundles Companies, Organizations, Projects, Workflows (CRUD + versioning + deploy/stop), and Dashboard into a single handler with method+path routing.

**Decomposition plan:**

Each entity type should be its own `api.handler` instance:

| New Module Type | Purpose |
|---|---|
| `api.handler` (resourceName: companies) | Company CRUD |
| `api.handler` (resourceName: organizations) | Organization CRUD |
| `api.handler` (resourceName: projects) | Project CRUD |
| `api.handler` (resourceName: workflows) | Workflow CRUD + versioning |
| `api.handler` (resourceName: dashboard) | Dashboard aggregation |

### 2.4 MODERATE: `WorkflowUIHandler` -- 6 routes

**File:** `/home/jon/workspace/workflow/module/api_workflow_ui.go`

Reasonably scoped but bundles UI serving with management API. The 6 routes (config CRUD, modules list, validate, reload, status) are all related to workflow management, so this is acceptable as-is. However, the hardcoded `availableModules` list (lines 152-248) duplicates information from the `schema.ModuleSchemaRegistry` and should be removed in favor of the schema system.

### 2.5 LOW: `dynamic.APIHandler`

**File:** `/home/jon/workspace/workflow/dynamic/api.go`

3 routes (list, create, get/update/delete by ID). Focused and clean. No action needed.

---

## 3. Trigger Architecture Gap Analysis

### 3.1 What the Engine Currently Supports

The current trigger system works as follows:

```
Trigger (HTTP/Event/Schedule/EventBus)
  --> engine.TriggerWorkflow(ctx, workflowType, action, data)
    --> WorkflowHandler.ExecuteWorkflow(ctx, type, action, data)
      --> returns (map[string]any, error)
```

**Supported trigger types:**
- `http` -- HTTPTrigger: HTTP request --> TriggerWorkflow
- `event` -- EventTrigger: Message broker topic --> TriggerWorkflow
- `schedule` -- ScheduleTrigger: Cron job --> TriggerWorkflow
- `eventbus` -- EventBusTrigger: EventBus subscription --> TriggerWorkflow

### 3.2 Can the Engine Support Webhook-to-Action Workflows?

**The desired model:**
```yaml
trigger:
  type: http.webhook
  path: /webhooks/stripe
  method: POST
steps:
  - validate_payload
  - process_payment
  - send_confirmation
```

**Current state of support:**

The engine has **two competing models** for step execution:

1. **StateMachine + ProcessingStep model** (the more mature path):
   - Define states and transitions in `workflows.statemachine`
   - Attach `processing.step` modules as transition hooks
   - Each processing step wraps a `dynamic.component` or `Executor`
   - Supports retry, compensation, async execution
   - **Gap:** Requires a full state machine definition, which is heavyweight for simple linear pipelines

2. **Integration handler model** (partially implemented):
   - Define steps with connectors in `workflows.integration`
   - `IntegrationWorkflowHandler.ExecuteIntegrationWorkflow()` runs steps sequentially
   - Supports variable substitution, retry, error handlers
   - **Gap:** Only supports HTTP/webhook/database connectors, not arbitrary service invocations

3. **Direct trigger-to-handler model** (current production path):
   - HTTPTrigger routes map directly to `engine.TriggerWorkflow()`
   - The WorkflowHandler is responsible for all execution logic
   - **Gap:** No built-in step pipeline; the handler IS the monolith

### 3.3 What is Missing

| Gap | Description | Severity |
|---|---|---|
| **No step pipeline primitive** | There is no lightweight "execute steps A, B, C in sequence" module that does not require a full state machine | High |
| **No webhook receiver module** | Inbound webhooks go through `api.handler` (RESTAPIHandler), which bundles 10+ concerns. No dedicated webhook-receive-and-forward module | High |
| **No step-to-step data flow** | `processing.step` passes data via the state machine instance's Data map. There is no explicit input/output contract between steps | Medium |
| **No conditional branching in pipelines** | The integration handler runs steps sequentially. Conditional branching requires a full state machine | Medium |
| **No parallel step execution** | Steps always run sequentially. No fan-out/fan-in | Low |
| **Trigger config is separate from workflow config** | Triggers are in `cfg.Triggers` while workflows are in `cfg.Workflows`. A composable workflow should define its trigger alongside its steps | Medium |
| **No workflow-level error handler** | Individual processing steps have compensate transitions, but there is no workflow-level catch/finally | Low |

### 3.4 What Already Works Well

- **Trigger --> TriggerWorkflow dispatch** is clean and extensible
- **processing.step** module correctly bridges dynamic components to state machine transitions with retry and compensation
- **dynamic.component** with Yaegi hot-reload is the right primitive for custom step logic
- **EventBus trigger** enables event-driven workflows
- **The integration handler** has the right shape for sequential step execution (just needs to be promoted to a first-class pattern)

---

## 4. Recommended Architecture

### 4.1 Target: The "Trigger --> Pipeline --> Action" Model

The goal is to enable this YAML pattern:

```yaml
name: stripe-webhook-handler
version: "1.0"

modules:
  - name: webhook-server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router

  - name: validate-signature
    type: dynamic.component
    config:
      source: components/validate_stripe_signature.go

  - name: process-payment
    type: dynamic.component
    config:
      source: components/process_payment.go

  - name: send-confirmation
    type: dynamic.component
    config:
      source: components/send_confirmation.go

  - name: payment-pipeline
    type: pipeline
    config:
      steps:
        - validate-signature
        - process-payment
        - send-confirmation
      onError: log-and-retry

triggers:
  http:
    routes:
      - path: /webhooks/stripe
        method: POST
        workflow: pipeline
        action: payment-pipeline
```

### 4.2 New Module Types Needed

#### `pipeline` -- Lightweight Step Pipeline

A new module type that executes a sequence of `Executor` steps without requiring a full state machine.

```go
type Pipeline struct {
    name     string
    steps    []string          // service names of Executor implementations
    onError  string            // error handler service name
    timeout  time.Duration     // pipeline-level timeout
}

// Implements WorkflowHandler so TriggerWorkflow can dispatch to it
func (p *Pipeline) CanHandle(workflowType string) bool
func (p *Pipeline) ExecuteWorkflow(ctx, type, action, data) (map[string]any, error)
```

This is essentially a simplified version of `IntegrationWorkflowHandler.ExecuteIntegrationWorkflow()` but:
- Steps are `Executor` services (not connector+action pairs)
- Data flows through: each step receives the previous step's output merged with original input
- Error handling is configurable (stop, continue, compensate)
- No connector abstraction needed

#### `webhook.receiver` -- Dedicated Webhook Endpoint

A focused module that receives webhooks and forwards them to a pipeline or message broker.

```go
type WebhookReceiver struct {
    name       string
    path       string
    method     string
    secret     string    // for signature validation
    targetType string    // "pipeline", "eventbus", "broker"
    target     string    // service name
}
```

This replaces the need for `api.handler` when the goal is "receive HTTP POST, forward data."

#### `action.http-call` -- Outbound HTTP Action

A step-compatible module for making outbound HTTP calls as a pipeline step (similar to `webhook.sender` but implementing `Executor`).

#### `action.publish-event` -- Event Publishing Action

A step-compatible module for publishing to a message broker or EventBus as a pipeline step.

### 4.3 Composability Model

```
                          Trigger Layer
                    +-------------------+
                    | HTTP    | EventBus |
                    | Event   | Schedule |
                    +---+-----+----+----+
                        |          |
                        v          v
                   TriggerWorkflow()
                        |
                        v
                   Pipeline Layer
                +------------------+
                | pipeline module  |
                | (step sequence)  |
                +--+----+----+----+
                   |    |    |
                   v    v    v
                Action Layer (Executor interface)
          +--------+--------+---------+
          |        |        |         |
     dynamic   http-call  publish  transform
     component             -event
```

Each layer is independently composable:
- **Triggers** are decoupled from pipelines (same trigger can feed different pipelines)
- **Pipelines** orchestrate steps without knowing what the steps do
- **Actions** implement the `Executor` interface and are interchangeable

### 4.4 Unified Workflow Config Format

Instead of separating triggers from workflows, unify them:

```yaml
# Current (split)
workflows:
  statemachine:
    engine: sm-engine
    definitions: [...]
triggers:
  http:
    routes:
      - path: /foo
        workflow: statemachine
        action: start

# Proposed (unified, backward-compatible)
workflows:
  statemachine:
    engine: sm-engine
    definitions: [...]

  # New: pipeline workflows
  pipelines:
    - name: stripe-handler
      trigger:
        type: http
        path: /webhooks/stripe
        method: POST
      steps:
        - validate-signature
        - process-payment
        - send-confirmation
      onError: log-failure
```

The existing `triggers` config section continues to work. The new `pipelines` section is syntactic sugar that registers both the trigger and the pipeline in one place.

---

## 5. Priority Actions

### Phase 1: Foundation (immediate)

| # | Action | Effort | Impact |
|---|---|---|---|
| 1.1 | **Create `pipeline` module type** | Medium | High -- enables the webhook-to-action pattern without state machines |
| 1.2 | **Create `PipelineWorkflowHandler`** | Medium | High -- allows `TriggerWorkflow()` to dispatch to pipelines |
| 1.3 | **Add `pipeline` to schema registry** | Low | Medium -- UI support for pipeline configuration |
| 1.4 | **Remove hardcoded `availableModules`** in `api_workflow_ui.go` | Low | Low -- use `schema.ModuleSchemaRegistry` instead |

### Phase 2: Decompose Monoliths (next sprint)

| # | Action | Effort | Impact |
|---|---|---|---|
| 2.1 | **Extract risk assessment from `api.handler`** into a `dynamic.component` | Low | Medium -- removes domain-specific logic from infrastructure |
| 2.2 | **Extract conversation routing from `api.handler`** into a `dynamic.component` | Low | Medium -- already partially done (conversation_router.go exists) |
| 2.3 | **Extract queue health endpoint** from `api.handler` into a dedicated module | Low | Low -- reduces api.handler complexity |
| 2.4 | **Split `auth.jwt`** into token-validator + auth-endpoints + user-management | High | Medium -- cleaner separation of concerns |

### Phase 3: Webhook Receiver (next sprint)

| # | Action | Effort | Impact |
|---|---|---|---|
| 3.1 | **Create `webhook.receiver` module type** | Medium | High -- dedicated inbound webhook handling |
| 3.2 | **Create `action.http-call` module** (Executor-compatible HTTP client) | Low | Medium -- reusable HTTP action for pipelines |
| 3.3 | **Create `action.publish-event` module** (Executor-compatible event publisher) | Low | Medium -- reusable event action for pipelines |

### Phase 4: Unified Config (later)

| # | Action | Effort | Impact |
|---|---|---|---|
| 4.1 | **Add `pipelines` section to WorkflowConfig** | Medium | High -- unified trigger+pipeline definition |
| 4.2 | **Migrate existing integration workflows** to the new pipeline model | Medium | Medium -- consistency |
| 4.3 | **Split V1APIHandler** into per-entity handlers | High | Low -- mostly affects admin UI |

---

## 6. Key Code Locations

| Concept | File | Line(s) |
|---|---|---|
| Module type switch | `/home/jon/workspace/workflow/engine.go` | 145-689 |
| WorkflowHandler interface | `/home/jon/workspace/workflow/engine.go` | 29-38 |
| TriggerWorkflow dispatch | `/home/jon/workspace/workflow/engine.go` | 960-1004 |
| Trigger interface | `/home/jon/workspace/workflow/module/trigger.go` | 8-15 |
| ProcessingStep (composability bridge) | `/home/jon/workspace/workflow/module/processing_step.go` | 31-228 |
| Executor interface | `/home/jon/workspace/workflow/module/processing_step.go` | 14-16 |
| RESTAPIHandler (main monolith) | `/home/jon/workspace/workflow/module/api_handlers.go` | 79-2256 |
| JWTAuthModule (auth monolith) | `/home/jon/workspace/workflow/module/jwt_auth.go` | 32-169 (route dispatch) |
| V1APIHandler (admin monolith) | `/home/jon/workspace/workflow/module/api_v1_handler.go` | 36-90 (route dispatch) |
| IntegrationWorkflowHandler (step model) | `/home/jon/workspace/workflow/handlers/integration.go` | 286-418 |
| HTTPTrigger | `/home/jon/workspace/workflow/module/http_trigger.go` | 32-257 |
| Schema registry | `/home/jon/workspace/workflow/schema/module_schema.go` | 54-93 |
| Config validation | `/home/jon/workspace/workflow/schema/validate.go` | - |
| Handler registration | `/home/jon/workspace/workflow/cmd/server/main.go` | 55-65 |

---

## 7. Summary

The workflow engine has strong foundations -- the modular framework, service registry, trigger system, and dynamic component loading are all well-designed. The primary issue is that **the HTTP-facing modules evolved into monoliths** as features were added incrementally:

- **`api.handler`** (RESTAPIHandler) is the worst offender at 2256 lines with 10+ bundled concerns
- **`auth.jwt`** bundles authentication, user management, and setup into one module
- **`V1APIHandler`** bundles 6 entity types into one routing handler

The missing primitive is a **lightweight pipeline module** that can execute a sequence of steps (each implementing the existing `Executor` interface) without requiring a full state machine. The `IntegrationWorkflowHandler` already has this execution model but it is tied to the integration connector abstraction. Extracting and generalizing this into a first-class `pipeline` module type would immediately enable the "webhook --> validate --> process --> notify" pattern.

The existing `processing.step` + `dynamic.component` + `statemachine` combination already supports composable workflows for complex cases. The pipeline module would complement this for simple linear flows.
