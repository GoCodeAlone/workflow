# End-to-End Workflow Execution Scenarios

This document describes each E2E test scenario in the workflow engine, including architecture diagrams showing the module topology, data flow, and evidence of successful execution.

All scenarios use **real HTTP servers on random ports**, **real modular.Application instances** (not mocks), and exercise the full engine lifecycle: `BuildFromConfig` -> `Start` -> real HTTP requests -> `Stop`.

---

## Table of Contents

1. [Core HTTP Pipeline](#1-core-http-pipeline)
2. [Order Processing Pipeline](#2-order-processing-pipeline)
3. [Order Pipeline Error Path](#3-order-pipeline-error-path)
4. [Broker Messaging](#4-broker-messaging)
5. [Config Push & Reload](#5-config-push--reload)
6. [Dynamic Component Load & Execute](#6-dynamic-component-load--execute)
7. [Dynamic Component Hot Reload](#7-dynamic-component-hot-reload)
8. [Dynamic Component Sandbox](#8-dynamic-component-sandbox)
9. [Dynamic Component Engine Integration](#9-dynamic-component-engine-integration)
10. [Dynamic Component Message Processor](#10-dynamic-component-message-processor)
11. [Integration Workflow](#11-integration-workflow)
12. [Event Workflow](#12-event-workflow)
13. [Data Transformer Pipeline](#13-data-transformer-pipeline)
14. [Scheduler Workflow](#14-scheduler-workflow)
15. [Metrics & Health Check](#15-metrics--health-check)
16. [EventBus Bridge](#16-eventbus-bridge)
17. [Webhook Sender](#17-webhook-sender)
18. [Slack Notification](#18-slack-notification)
19. [Middleware: Auth](#19-middleware-auth)
20. [Middleware: Rate Limiting](#20-middleware-rate-limiting)
21. [Middleware: CORS](#21-middleware-cors)
22. [Middleware: Request ID](#22-middleware-request-id)
23. [Middleware: Full Chain](#23-middleware-full-chain)

---

## 1. Core HTTP Pipeline

**Test:** `TestE2E_SimpleHTTPWorkflow`
**Modules:** `http.server`, `http.router`, `http.handler`

The simplest possible workflow: an HTTP server receives a POST request, routes it through a router to a handler, which returns a JSON response.

```mermaid
flowchart LR
    Client["HTTP Client"]
    Server["http.server<br/>:random-port"]
    Router["http.router"]
    Handler["http.handler<br/>test-handler"]

    Client -->|"POST /api/test"| Server
    Server --> Router
    Router -->|"route match"| Handler
    Handler -->|"200 OK<br/>{status: success}"| Client

    style Server fill:#4A90D9,color:white
    style Router fill:#7B68EE,color:white
    style Handler fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_SimpleHTTPWorkflow
    E2E HTTP workflow: POST /api/test -> 200 OK, handler=test-handler
--- PASS: TestE2E_SimpleHTTPWorkflow (0.08s)
```
</details>

---

## 2. Order Processing Pipeline

**Test:** `TestE2E_OrderPipeline_FullExecution`
**Modules:** `http.server`, `http.router`, `api.handler`, `statemachine.engine`, `state.tracker`, `messaging.broker`, `messaging.handler`

The flagship E2E test: a 6-step order processing pipeline that exercises HTTP ingress, CRUD operations, state machine transitions, and message broker delivery.

```mermaid
flowchart TD
    Client["HTTP Client"]
    Server["http.server<br/>:random-port"]
    Router["http.router"]
    API["api.handler<br/>order-api"]
    SM["statemachine.engine<br/>order-state-engine"]
    Tracker["state.tracker"]
    Broker["messaging.broker"]
    Notifier["messaging.handler<br/>notification-handler"]

    Client -->|"POST /api/orders"| Server
    Server --> Router
    Router --> API
    API -->|"create resource"| API
    API -->|"PUT /transition"| SM
    SM -->|"track state"| Tracker
    SM -->|"state changed"| Broker
    Broker -->|"order.completed"| Notifier

    style Server fill:#4A90D9,color:white
    style Router fill:#7B68EE,color:white
    style API fill:#50C878,color:white
    style SM fill:#FF8C00,color:white
    style Tracker fill:#FFD700,color:black
    style Broker fill:#DC143C,color:white
    style Notifier fill:#8B008B,color:white
```

```mermaid
stateDiagram-v2
    [*] --> new: POST /api/orders
    new --> received: auto
    received --> validated: validate_order
    validated --> stored: store_order
    stored --> notified: send_notification
    notified --> [*]: final state

    received --> failed: fail_validation
    failed --> [*]: error state
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_OrderPipeline_FullExecution
    Step 1: Creating order via POST /api/orders
      Created order: id=ORD-001, state=new
    Step 2: Transitioning order to 'validated'
      State after validate_order: validated
    Step 3: Transitioning order to 'stored'
      State after store_order: stored
    Step 4: Transitioning order to 'notified' (final state)
      State after send_notification: notified
    Step 5: Verifying final state via GET /api/orders/ORD-001
      Final order state: notified
    Step 6: Verifying message broker delivery
      Broker delivered message: {"test":"broker-delivery","orderId":"ORD-001"}
    E2E Order Pipeline: All 6 steps passed - full pipeline execution verified
--- PASS: TestE2E_OrderPipeline_FullExecution (0.11s)
```
</details>

---

## 3. Order Pipeline Error Path

**Test:** `TestE2E_OrderPipeline_ErrorPath`
**Modules:** `http.server`, `http.router`, `api.handler`, `statemachine.engine`, `state.tracker`

Validates that the state machine correctly handles error transitions: an order transitions to the `failed` state, and subsequent transitions from a final state are rejected with HTTP 400.

```mermaid
flowchart LR
    Client["HTTP Client"]
    API["api.handler"]
    SM["statemachine.engine"]

    Client -->|"POST /api/orders<br/>{invalid data}"| API
    API -->|"PUT /transition<br/>fail_validation"| SM
    SM -->|"received -> failed"| SM
    Client -->|"PUT /transition<br/>fail_validation"| API
    API -->|"400 Bad Request<br/>final state"| Client

    style SM fill:#FF4444,color:white
    style API fill:#50C878,color:white
```

```mermaid
stateDiagram-v2
    [*] --> received: POST /api/orders
    received --> failed: fail_validation
    failed --> failed: any transition (REJECTED 400)
    failed --> [*]: error final state
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_OrderPipeline_ErrorPath
    E2E Error Path: Order correctly transitioned to 'failed' and rejected further transitions
--- PASS: TestE2E_OrderPipeline_ErrorPath (0.06s)
```
</details>

---

## 4. Broker Messaging

**Test:** `TestE2E_BrokerMessaging`
**Modules:** `http.server`, `http.router`, `http.handler`, `messaging.broker`, `messaging.handler`

Validates that the in-memory message broker correctly delivers messages from producer to subscriber, running alongside an HTTP server in the same engine.

```mermaid
flowchart LR
    subgraph Engine
        Server["http.server"]
        Router["http.router"]
        Handler["http.handler"]
        Broker["messaging.broker"]
        Sub["messaging.handler<br/>msg-subscriber"]
    end

    Client["HTTP Client"]
    Client -->|"POST /api/publish"| Server
    Server --> Router --> Handler

    Test["Test Code"]
    Test -->|"broker.Producer()<br/>.SendMessage()"| Broker
    Broker -->|"topic: test.events"| Sub

    style Broker fill:#DC143C,color:white
    style Sub fill:#8B008B,color:white
    style Server fill:#4A90D9,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_BrokerMessaging
    E2E Broker Messaging: Message broker and HTTP server both operational within same engine
--- PASS: TestE2E_BrokerMessaging (0.05s)
```
</details>

---

## 5. Config Push & Reload

**Test:** `TestE2E_ConfigPushAndReload`
**Modules:** `WorkflowUIHandler` (management API)

Tests the full config management lifecycle: retrieve config, push new config, reload engine, check status, and validate config. This is the foundation for the UI's ability to design workflows and deploy them.

```mermaid
sequenceDiagram
    participant Client
    participant UI as WorkflowUIHandler
    participant Engine as Workflow Engine

    Client->>UI: GET /api/workflow/config
    UI-->>Client: {modules:[], workflows:{}}

    Client->>UI: PUT /api/workflow/config
    Note right of Client: Push 3-module config

    Client->>UI: POST /api/workflow/reload
    UI->>Engine: Stop old engine
    UI->>Engine: BuildFromConfig(newCfg)
    UI->>Engine: Start new engine
    UI-->>Client: {status: reloaded}

    Client->>UI: GET /api/workflow/status
    UI-->>Client: {status: running}

    Client->>UI: POST /api/workflow/validate
    UI-->>Client: {valid: true}
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_ConfigPushAndReload
      Initial config retrieved: {"modules":[],"workflows":{},"triggers":{}}
    E2E Config Push & Reload: Config CRUD, reload, status, and validation all working
--- PASS: TestE2E_ConfigPushAndReload (0.06s)
```
</details>

---

## 6. Dynamic Component Load & Execute

**Test:** `TestE2E_DynamicComponent_LoadAndExecute`
**System:** Dynamic Component API (Yaegi interpreter)

Loads Go source code at runtime via the HTTP API, verifies it appears in the registry, executes it, and retrieves details.

```mermaid
flowchart TD
    Client["Test Client"]
    API["Dynamic API<br/>/api/dynamic/components"]
    Loader["Loader"]
    Sandbox["Sandbox<br/>Validator"]
    Yaegi["Yaegi<br/>Interpreter"]
    Registry["Component<br/>Registry"]

    Client -->|"POST source code"| API
    API --> Loader
    Loader --> Sandbox
    Sandbox -->|"validate imports"| Sandbox
    Sandbox -->|"allowed"| Yaegi
    Yaegi -->|"compile & load"| Registry

    Client -->|"GET /components"| API
    API -->|"list from"| Registry

    Client -->|"Execute(ctx, input)"| Registry
    Registry -->|"greeting: Hello, E2E!"| Client

    style Sandbox fill:#FF8C00,color:white
    style Yaegi fill:#4A90D9,color:white
    style Registry fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DynamicComponent_LoadAndExecute
    Step 1: Creating dynamic component via POST /api/dynamic/components
      Created component: id=greeter, name=greeter, status=loaded
    Step 2: Verifying component is listed via GET /api/dynamic/components
      Registry has 1 component(s)
    Step 3: Executing dynamic component directly
      Execute result: greeting=Hello, E2E!, source=dynamic
    Step 4: Verifying component details via GET /api/dynamic/components/greeter
      Component detail: name=greeter, status=running
    E2E Dynamic Component Load & Execute: All steps passed
--- PASS: TestE2E_DynamicComponent_LoadAndExecute (0.12s)
```
</details>

---

## 7. Dynamic Component Hot Reload

**Test:** `TestE2E_DynamicComponent_HotReload`
**System:** Dynamic Component API (Yaegi interpreter)

Loads v1 of a component, updates it to v2 with different behavior (adds `strings.ToUpper`), confirms the new behavior is active, then deletes the component.

```mermaid
sequenceDiagram
    participant Client
    participant API as Dynamic API
    participant Registry

    Client->>API: POST (v1 source: returns "v1:data")
    API->>Registry: Load v1
    Client->>Registry: Execute({input: "data"})
    Registry-->>Client: {output: "v1:data"}

    Client->>API: PUT (v2 source: returns "v2:DATA")
    API->>Registry: Replace with v2
    Client->>Registry: Execute({input: "data"})
    Registry-->>Client: {output: "v2:DATA"}

    Client->>API: DELETE /components/transformer
    API->>Registry: Remove
    Note right of Registry: Count = 0
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DynamicComponent_HotReload
    Step 1: Creating v1 of component (returns 'v1')
      v1 result: version=v1, output=v1:data
    Step 2: Updating to v2 via PUT /api/dynamic/components/transformer
      Updated component: name=transformer-v2, status=loaded
    Step 3: Executing updated component - verifying new behavior
      v2 result: version=v2, output=v2:DATA
    Step 4: Confirming v1 behavior is no longer present
    Step 5: Deleting component via DELETE /api/dynamic/components/transformer
      Component deleted from registry, count=0
    E2E Dynamic Component Hot Reload: All steps passed
--- PASS: TestE2E_DynamicComponent_HotReload (0.06s)
```
</details>

---

## 8. Dynamic Component Sandbox

**Test:** `TestE2E_DynamicComponent_SandboxRejectsUnsafe`
**System:** Sandbox Validator

Tests that the Yaegi sandbox blocks dangerous imports (`os/exec`, `syscall`, `unsafe`, `os`, `net`, `reflect`) while allowing safe imports (`fmt`, `strings`, `encoding/json`).

```mermaid
flowchart TD
    subgraph Blocked["Blocked Imports (422 Unprocessable)"]
        OsExec["os/exec"]
        Syscall["syscall"]
        Unsafe["unsafe"]
        Os["os"]
        Net["net"]
        Reflect["reflect"]
    end

    subgraph Allowed["Allowed Imports (200 OK)"]
        Fmt["fmt"]
        Strings["strings"]
        Json["encoding/json"]
    end

    Source["Go Source Code"] --> Sandbox["Sandbox<br/>Validator"]
    Sandbox -->|"import in blocklist?"| Decision{Blocked?}
    Decision -->|"Yes"| Reject["422: blocked import"]
    Decision -->|"No"| Accept["Load into Yaegi"]

    style Blocked fill:#FF4444,color:white
    style Allowed fill:#50C878,color:white
    style Sandbox fill:#FF8C00,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DynamicComponent_SandboxRejectsUnsafe
      Blocked os/exec: status=422, error contains "os/exec"
      Blocked syscall: status=422, error contains "syscall"
      Blocked unsafe: status=422, error contains "unsafe"
      Blocked os (filesystem access): status=422, error contains "os"
      Blocked net (raw sockets): status=422, error contains "net"
      Blocked reflect: status=422, error contains "reflect"
    Verifying valid component still loads after rejections
      Valid component loaded and executed: message=safe and sound
    E2E Dynamic Component Sandbox: All blocked imports rejected, valid component still works
--- PASS: TestE2E_DynamicComponent_SandboxRejectsUnsafe (0.06s)
    --- PASS: os/exec (0.00s)
    --- PASS: syscall (0.00s)
    --- PASS: unsafe (0.00s)
    --- PASS: os_(filesystem_access) (0.00s)
    --- PASS: net_(raw_sockets) (0.00s)
    --- PASS: reflect (0.00s)
```
</details>

---

## 9. Dynamic Component Engine Integration

**Test:** `TestE2E_DynamicComponent_EngineIntegration`
**Modules:** `dynamic.component`, `http.server`, `http.router`, `http.handler`

Pre-loads a dynamic component into the registry, then builds a full engine that references it via `"dynamic.component"` module type. Exercises the full engine lifecycle, hot-reloads the component while the engine is running, and verifies the HTTP server survives the reload.

```mermaid
flowchart TD
    subgraph Engine["Workflow Engine"]
        Server["http.server"]
        Router["http.router"]
        Handler["http.handler"]
        Dynamic["dynamic.component<br/>text-processor"]
    end

    Registry["Component<br/>Registry"]
    Client["HTTP Client"]

    Registry -->|"pre-loaded"| Dynamic
    Client -->|"POST /api/test"| Server
    Server --> Router --> Handler

    subgraph HotReload["Hot Reload (while running)"]
        V1["v1: upper + reverse"]
        V2["v2: upper + underscore"]
    end

    Registry -.->|"replace"| Dynamic

    style Dynamic fill:#FF8C00,color:white
    style Server fill:#4A90D9,color:white
    style Registry fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DynamicComponent_EngineIntegration
    Step 1: Building engine with dynamic.component module type
      BuildFromConfig succeeded with dynamic.component
    Step 2: Starting engine (full lifecycle)
      Engine started, HTTP server accepting connections
    Step 3: Executing dynamic component through registry
      upper: Hello World -> HELLO WORLD
      reverse: Hello World -> dlroW olleH
    Step 4: Hot-reloading component while engine is running
      v2 result: HELLO_WORLD (version=v2)
    Step 5: Verifying HTTP server still works after hot-reload
      HTTP server still healthy after hot-reload
    Step 6: Verifying BuildFromConfig fails without dynamic registry
      Got expected error: dynamic registry not set, cannot load dynamic component "orphan"
    E2E Dynamic Component Engine Integration: All steps passed
--- PASS: TestE2E_DynamicComponent_EngineIntegration (0.06s)
```
</details>

---

## 10. Dynamic Component Message Processor

**Test:** `TestE2E_DynamicComponent_MessageProcessor`
**Modules:** `dynamic.component`, `messaging.broker`, `http.server`, `http.router`, `http.handler`

A dynamic component processes messages received from a broker (transform/validate/echo operations), publishes results back through the broker, and a subscriber verifies delivery.

```mermaid
flowchart LR
    subgraph Engine
        Server["http.server"]
        Broker["messaging.broker"]
        Dynamic["dynamic.component<br/>msg-processor"]
        Sub["subscriber"]
    end

    Test["Test Code"]

    Test -->|'1. SendMessage<br/>action: transform'| Broker
    Broker -->|"2. deliver"| Dynamic
    Dynamic -->|"3. process<br/>HELLO WORLD"| Dynamic
    Dynamic -->|"4. publish result"| Broker
    Broker -->|"5. deliver result"| Sub

    Test -->|"validate"| Dynamic
    Test -->|"echo"| Dynamic
    Test -->|"invalid JSON"| Dynamic

    style Dynamic fill:#FF8C00,color:white
    style Broker fill:#DC143C,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DynamicComponent_MessageProcessor
    Step 1: Verifying dynamic component and broker are in service registry
      Both dynamic component and broker found
    Step 2: Publishing message via broker and processing with dynamic component
      Transform result: input=hello world, output=HELLO WORLD
      Results subscriber received: {"action":"transform","input":"hello world","output":"HELLO WORLD","processed":true}
    Step 3: Processing validate message
      Validate result: action=validate, output=valid
    Step 4: Processing echo message
      Echo result: action=unknown, output=echo:test-data
    Step 5: Testing error handling with invalid input
      Got expected error: invalid JSON message
    Step 6: Verifying HTTP server still operational
      HTTP server healthy
    E2E Dynamic Component Message Processor: All steps passed
--- PASS: TestE2E_DynamicComponent_MessageProcessor (0.11s)
```
</details>

---

## 11. Integration Workflow

**Test:** `TestE2E_IntegrationWorkflow`
**Handlers:** `IntegrationWorkflowHandler`
**Modules:** `http.server`, `http.router`, `http.handler` + mock target server

Configures the IntegrationWorkflowHandler with an HTTP connector pointing at a mock target server. Verifies the connector is registered in the IntegrationRegistry and can execute real HTTP requests to the external service.

```mermaid
flowchart LR
    subgraph WorkflowEngine["Workflow Engine"]
        Server["http.server"]
        Router["http.router"]
        Handler["http.handler"]
        IntHandler["IntegrationWorkflow<br/>Handler"]
        Connector["HTTP Connector<br/>api-connector"]
    end

    Target["Mock Target Server<br/>/api/customer/123"]

    IntHandler -->|"configure"| Connector
    Connector -->|"GET /api/customer/123"| Target
    Target -->|"{name: Alice, status: active}"| Connector

    style IntHandler fill:#FF8C00,color:white
    style Connector fill:#4A90D9,color:white
    style Target fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_IntegrationWorkflow
      Registered connectors: [api-connector]
      Integration connector returned: name=Alice, status=active
    E2E Integration Workflow: Registry configured, HTTP connector executed against real server
--- PASS: TestE2E_IntegrationWorkflow (0.11s)
```
</details>

---

## 12. Event Workflow

**Test:** `TestE2E_EventWorkflow`
**Handlers:** `EventWorkflowHandler`
**Modules:** `http.server`, `http.router`, `http.handler`, `messaging.broker`, `messaging.handler`

Configures the EventWorkflowHandler with an EventProcessor, a login-failure pattern (minOccurs=2, 5-minute window), message handler-based event handlers, and a broker-to-event adapter.

```mermaid
flowchart TD
    Broker["messaging.broker"]
    Adapter["Broker-Event<br/>Adapter"]
    Processor["Event<br/>Processor"]
    Pattern["Pattern: login-failures<br/>minOccurs=2, window=5m"]
    MsgHandler["messaging.handler<br/>alert-handler"]

    Broker -->|"events"| Adapter
    Adapter -->|"feed events"| Processor
    Processor -->|"match pattern"| Pattern
    Pattern -->|"pattern matched"| MsgHandler

    subgraph EventStream["Event Stream"]
        E1["login_failed #1"]
        E2["login_failed #2"]
        E3["login_failed #3"]
    end

    EventStream -->|"publish"| Broker

    style Processor fill:#FF8C00,color:white
    style Pattern fill:#DC143C,color:white
    style Adapter fill:#7B68EE,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_EventWorkflow
    E2E Event Workflow: Processor configured with patterns, adapters connected to broker, HTTP running
--- PASS: TestE2E_EventWorkflow (0.20s)
```
</details>

---

## 13. Data Transformer Pipeline

**Test:** `TestE2E_DataTransformer`
**Modules:** `data.transformer`, `http.server`, `http.router`, `http.handler`

Registers a "normalize-user" transformation pipeline with extract, map, and filter operations. Verifies field renaming (firstName -> first_name) and sensitive field stripping (password, internal fields removed).

```mermaid
flowchart LR
    Input["Input Data<br/>{firstName, lastName,<br/>email, password,<br/>_internal}"]

    subgraph Pipeline["normalize-user Pipeline"]
        Extract["Extract<br/>select fields"]
        Map["Map<br/>rename keys<br/>firstName -> first_name"]
        Filter["Filter<br/>strip sensitive<br/>password, _internal"]
    end

    Output["Output<br/>{first_name: Alice,<br/>last_name: Smith,<br/>email: alice@...}"]

    Input --> Extract --> Map --> Filter --> Output

    style Extract fill:#4A90D9,color:white
    style Map fill:#7B68EE,color:white
    style Filter fill:#FF8C00,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_DataTransformer
      Transformed result: first_name=Alice, last_name=Smith, email=alice@example.com
    E2E Data Transformer: Pipeline executed extract->map->filter, sensitive fields stripped
--- PASS: TestE2E_DataTransformer (0.06s)
```
</details>

---

## 14. Scheduler Workflow

**Test:** `TestE2E_SchedulerWorkflow`
**Handlers:** `SchedulerWorkflowHandler`
**Modules:** Custom `cron.scheduler` + `test.job` via `AddModuleType`

Configures the SchedulerWorkflowHandler with a CronScheduler and a test job. Verifies both are registered in the service registry and that the job can be executed on demand.

```mermaid
flowchart LR
    Handler["SchedulerWorkflow<br/>Handler"]
    Scheduler["CronScheduler"]
    Job["test.job<br/>cleanup-job"]

    Handler -->|"configure"| Scheduler
    Handler -->|"register"| Job
    Scheduler -->|"trigger<br/>(on schedule or manual)"| Job
    Job -->|"execute task"| Job

    style Handler fill:#FF8C00,color:white
    style Scheduler fill:#4A90D9,color:white
    style Job fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_SchedulerWorkflow
    E2E Scheduler Workflow: Scheduler configured with job, job executed, HTTP server running
--- PASS: TestE2E_SchedulerWorkflow (0.06s)
```
</details>

---

## 15. Metrics & Health Check

**Test:** `TestE2E_MetricsAndHealthCheck`
**Modules:** `metrics.collector`, `health.checker`, `http.server`, `http.router`, `http.handler`

Validates the observability stack: Prometheus-style metrics recording and health check endpoints (`/health`, `/ready`, `/live`) including both healthy and unhealthy states.

```mermaid
flowchart TD
    Client["HTTP Client"]
    Server["http.server"]
    Router["http.router"]
    Handler["http.handler"]
    Metrics["metrics.collector"]
    Health["health.checker"]

    Client -->|"POST /api/test"| Server
    Server --> Router --> Handler
    Handler -->|"record"| Metrics

    Client -->|"GET /health"| Health
    Health -->|"200: healthy"| Client

    Client -->|"GET /ready"| Health
    Health -->|"200: ready"| Client

    Client -->|"GET /live"| Health
    Health -->|"200: alive"| Client

    Note["After registering<br/>unhealthy component"]
    Client -->|"GET /health"| Health
    Health -->|"503: unhealthy"| Client

    style Metrics fill:#FF8C00,color:white
    style Health fill:#50C878,color:white
    style Server fill:#4A90D9,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_MetricsAndHealthCheck
    Step 1: Verifying metrics collector is registered as a service
      metrics.collector service found in registry
    Step 2: Verifying health checker is registered as a service
      health.checker service found in registry
    Step 3: Registering health check and setting started
    Step 4: Making HTTP requests to generate metrics
      Made 3 successful HTTP requests
    Step 5: Recording and verifying metrics
      Prometheus metrics contain expected counters
    Step 6: Testing health checker HTTP handlers
      Health endpoint: status=healthy
      Ready endpoint: status=ready
      Live endpoint: status=alive
    Step 7: Testing health check with unhealthy component
      Unhealthy endpoint: status=unhealthy, httpCode=503
    E2E Metrics & Health Check: All steps passed
--- PASS: TestE2E_MetricsAndHealthCheck (0.06s)
```
</details>

---

## 16. EventBus Bridge

**Test:** `TestE2E_EventBusBridge`
**Modules:** `messaging.broker.eventbus`, `messaging.handler`, EventBus module

Validates the EventBusBridge adapter that connects the workflow `MessageBroker` interface to the modular `EventBus`. Tests bidirectional communication: publish via bridge (round-trip through EventBus) and publish directly via EventBus (received by bridge subscriber).

```mermaid
flowchart LR
    subgraph WorkflowSide["Workflow Layer"]
        Bridge["EventBusBridge<br/>(MessageBroker)"]
        Sub["Subscriber"]
    end

    subgraph ModularSide["Modular Framework"]
        EventBus["EventBus"]
    end

    Test["Test Code"]

    Test -->|"1. bridge.Publish()"| Bridge
    Bridge -->|"2. forward"| EventBus
    EventBus -->|"3. deliver"| Bridge
    Bridge -->|"4. deliver to"| Sub

    Test -->|"5. eventbus.Publish()"| EventBus
    EventBus -->|"6. bridge receives"| Bridge
    Bridge -->|"7. deliver to"| Sub

    style Bridge fill:#7B68EE,color:white
    style EventBus fill:#4A90D9,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_EventBusBridge
    Step 1: Subscribing to topic via EventBusBridge
      Subscribed to 'test.events' via bridge
    Step 2: Publishing message through the bridge
      Received message via bridge: {"action":"test","value":42}
    Step 3: Publishing directly through EventBus module
      Bridge subscriber received direct EventBus message: {"id":123,"source":"direct"}
    Step 4: Unsubscribing and verifying no more messages
      No messages received after unsubscribe (correct)
    E2E EventBusBridge: All steps passed
--- PASS: TestE2E_EventBusBridge (0.38s)
```
</details>

---

## 17. Webhook Sender

**Test:** `TestE2E_WebhookSender`
**Modules:** `webhook.sender`, `http.server`, `http.router`, `http.handler`

Spins up a real webhook target server, sends a webhook with custom headers, verifies receipt, then tests the dead letter queue by sending to an unreachable URL with retry exhaustion.

```mermaid
flowchart TD
    subgraph Engine["Workflow Engine"]
        Server["http.server"]
        Webhook["webhook.sender"]
    end

    Target["Mock Target Server<br/>:random-port"]
    DeadURL["http://127.0.0.1:1<br/>(unreachable)"]
    DLQ["Dead Letter Queue"]

    Webhook -->|"POST /webhook<br/>X-Webhook-Secret<br/>X-Event-Type"| Target
    Target -->|"200 OK<br/>status: delivered"| Webhook

    Webhook -->|"POST (3 attempts)"| DeadURL
    DeadURL -->|"timeout x3"| Webhook
    Webhook -->|"status: dead_letter"| DLQ

    style Webhook fill:#FF8C00,color:white
    style Target fill:#50C878,color:white
    style DeadURL fill:#FF4444,color:white
    style DLQ fill:#DC143C,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_WebhookSender
    Step 1: Retrieving webhook sender from service registry
      webhook.sender service found
    Step 2: Sending webhook to target server
      Webhook delivered: id=wh-...-1, status=delivered, attempts=1
    Step 3: Verifying target server received the webhook
      Target server received correct payload and headers
    Step 4: Sending webhook to invalid URL (expects dead letter)
      Failed delivery: id=wh-...-2, status=dead_letter, attempts=3
    Step 5: Verifying dead letter queue
      Dead letter queue: id=wh-...-2, lastError=request failed: Post "http://127.0.0.1:1/nonexistent": context deadline exceeded
    E2E Webhook Sender: All steps passed - delivery, headers, dead letter verified
--- PASS: TestE2E_WebhookSender (4.65s)
```
</details>

---

## 18. Slack Notification

**Test:** `TestE2E_SlackNotification_MockEndpoint`
**Modules:** `notification.slack`, `http.server`, `http.router`

Points the Slack notification module at a mock HTTP server that captures webhook POSTs. Verifies the correct JSON payload format (text, channel, username) and tests error handling when the webhook URL is empty.

```mermaid
flowchart LR
    Slack["notification.slack"]
    MockSlack["Mock Slack Server<br/>:random-port/webhook"]
    Engine["Workflow Engine<br/>(http.server)"]

    Trigger["HandleMessage()<br/>order notification"] --> Slack
    Slack -->|"POST /webhook<br/>{text, channel, username}"| MockSlack
    MockSlack -->|"200 OK"| Slack

    Slack -->|"empty webhook URL"| Error["Error: not configured"]

    style Slack fill:#7B68EE,color:white
    style MockSlack fill:#50C878,color:white
    style Error fill:#FF4444,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_SlackNotification_MockEndpoint
    Step 1: Configuring Slack notification module
      Configured: webhook, channel, username
    Step 2: Sending notification via HandleMessage
      Notification sent
    Step 3: Verifying mock Slack server received the payload
      Slack payload correct: text="Order ORD-300 has been completed successfully", channel=#test-alerts, username=workflow-bot
    Step 4: Sending second notification
    Step 5: Testing error handling with empty webhook URL
      Got expected error: slack webhook URL not configured
    E2E Slack Notification: All steps passed
--- PASS: TestE2E_SlackNotification_MockEndpoint (0.11s)
```
</details>

---

## 19. Middleware: Auth

**Test:** `TestE2E_Middleware_Auth`
**Modules:** `http.server`, `http.router`, `http.middleware.auth`, `http.handler`

Tests Bearer token authentication middleware with 4 scenarios: no header, wrong auth type, invalid token, and valid token.

```mermaid
flowchart LR
    subgraph Requests["HTTP Requests"]
        R1["No Authorization<br/>header"]
        R2["Basic auth<br/>(wrong type)"]
        R3["Bearer invalid-token"]
        R4["Bearer valid-token"]
    end

    Auth["http.middleware.auth<br/>authType: Bearer"]
    Handler["http.handler"]

    R1 -->|"401"| Auth
    R2 -->|"401"| Auth
    R3 -->|"401"| Auth
    R4 -->|"pass"| Auth
    Auth -->|"200"| Handler

    style Auth fill:#FF4444,color:white
    style Handler fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_Middleware_Auth
    E2E Middleware Auth: All auth scenarios verified
--- PASS: TestE2E_Middleware_Auth (0.06s)
    --- PASS: no_auth_header (0.00s)
    --- PASS: wrong_auth_type (0.00s)
    --- PASS: invalid_token (0.00s)
    --- PASS: valid_token (0.00s)
```
</details>

---

## 20. Middleware: Rate Limiting

**Test:** `TestE2E_Middleware_RateLimit`
**Modules:** `http.server`, `http.router`, `http.middleware.ratelimit`, `http.handler`

Configures a rate limiter with burstSize=3. The first 3 requests succeed (200), the 4th is rejected (429 Too Many Requests).

```mermaid
flowchart LR
    subgraph Requests["Sequential Requests"]
        R1["Request 1"] -->|"200 OK<br/>tokens: 3->2"| RL
        R2["Request 2"] -->|"200 OK<br/>tokens: 2->1"| RL
        R3["Request 3"] -->|"200 OK<br/>tokens: 1->0"| RL
        R4["Request 4"] -->|"429 Too Many<br/>tokens: 0"| RL
    end

    RL["http.middleware.ratelimit<br/>burstSize=3<br/>requestsPerMinute=60"]
    Handler["http.handler"]

    RL -->|"tokens > 0"| Handler

    style RL fill:#FF8C00,color:white
    style R4 fill:#FF4444,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_Middleware_RateLimit
      Got 429 on request 3 (burst=3)
    E2E Middleware RateLimit: Rate limiting verified with burstSize=3
--- PASS: TestE2E_Middleware_RateLimit (0.06s)
```
</details>

---

## 21. Middleware: CORS

**Test:** `TestE2E_Middleware_CORS`
**Modules:** `http.server`, `http.router`, `http.middleware.cors`, `http.handler`

Tests Cross-Origin Resource Sharing headers: allowed origin gets CORS headers, disallowed origin gets none, custom allowed headers are returned, and OPTIONS preflight returns proper headers.

```mermaid
flowchart TD
    subgraph Scenarios
        S1["Origin: http://allowed.example.com"]
        S2["Origin: http://evil.example.com"]
        S3["OPTIONS preflight"]
    end

    CORS["http.middleware.cors<br/>allowedOrigins:<br/>http://allowed.example.com"]

    S1 -->|"pass"| CORS
    CORS -->|"Access-Control-Allow-Origin:<br/>http://allowed.example.com"| Response1["200 + CORS headers"]

    S2 -->|"pass"| CORS
    CORS -->|"no CORS headers"| Response2["200 (no CORS)"]

    S3 -->|"OPTIONS"| CORS
    CORS -->|"Allow-Origin + Allow-Methods"| Response3["200 + full CORS"]

    style CORS fill:#7B68EE,color:white
    style Response1 fill:#50C878,color:white
    style Response2 fill:#FFD700,color:black
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_Middleware_CORS
    E2E Middleware CORS: Allowed, disallowed, headers, and preflight scenarios verified
--- PASS: TestE2E_Middleware_CORS (0.11s)
    --- PASS: allowed_origin (0.00s)
    --- PASS: disallowed_origin (0.00s)
    --- PASS: allowed_headers (0.00s)
    --- PASS: preflight_with_options_route (0.05s)
```
</details>

---

## 22. Middleware: Request ID

**Test:** `TestE2E_Middleware_RequestID`
**Modules:** `http.server`, `http.router`, `http.handler` + RequestID middleware

Verifies that the server generates a UUID `X-Request-ID` when the client doesn't provide one, and preserves the client-supplied ID when one is sent.

```mermaid
flowchart LR
    subgraph NoID["No X-Request-ID"]
        R1["GET /api/test"] --> MW1["RequestID<br/>Middleware"]
        MW1 -->|"generate UUID"| Handler1["Handler"]
        Handler1 -->|"X-Request-ID:<br/>aad8e019-6469-..."| Response1["Response"]
    end

    subgraph WithID["Client-supplied ID"]
        R2["GET /api/test<br/>X-Request-ID:<br/>my-custom-id"] --> MW2["RequestID<br/>Middleware"]
        MW2 -->|"preserve"| Handler2["Handler"]
        Handler2 -->|"X-Request-ID:<br/>my-custom-id"| Response2["Response"]
    end

    style MW1 fill:#7B68EE,color:white
    style MW2 fill:#7B68EE,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_Middleware_RequestID
      Generated X-Request-ID: d91af827-329f-48c7-bcfe-09893992be72
    E2E Middleware RequestID: Generated and preserved ID scenarios verified
--- PASS: TestE2E_Middleware_RequestID (0.05s)
    --- PASS: generated_id (0.00s)
    --- PASS: preserved_id (0.00s)
```
</details>

---

## 23. Middleware: Full Chain

**Test:** `TestE2E_Middleware_FullChain`
**Modules:** `http.server`, `http.router`, `http.middleware.cors`, `http.middleware.ratelimit`, `http.middleware.auth`, `http.middleware.logging`, `http.handler`

Wires 4 middlewares into a single route and tests them cooperating: CORS + rate limiting + authentication + logging. Verifies that unauthenticated requests are blocked, authenticated requests with CORS pass through, and rate limiting kicks in after burst exhaustion.

```mermaid
flowchart LR
    Client["HTTP Client"]
    CORS["CORS<br/>Middleware"]
    RL["Rate Limit<br/>Middleware"]
    Auth["Auth<br/>Middleware"]
    Log["Logging<br/>Middleware"]
    Handler["http.handler"]

    Client --> CORS
    CORS -->|"add CORS headers"| RL
    RL -->|"check tokens"| Auth
    Auth -->|"verify Bearer"| Log
    Log -->|"log request"| Handler
    Handler -->|"200 OK"| Client

    RL -->|"429 (no tokens)"| Client
    Auth -->|"401 (no token)"| Client

    style CORS fill:#7B68EE,color:white
    style RL fill:#FF8C00,color:white
    style Auth fill:#FF4444,color:white
    style Log fill:#4A90D9,color:white
    style Handler fill:#50C878,color:white
```

<details>
<summary>Execution Evidence</summary>

```
=== RUN   TestE2E_Middleware_FullChain
      Got 429 on request 3 (burst=5)
    E2E Middleware FullChain: CORS + RateLimit + Auth + Logging all cooperating
--- PASS: TestE2E_Middleware_FullChain (0.06s)
    --- PASS: no_auth_no_origin (0.00s)
    --- PASS: valid_auth_with_cors (0.00s)
    --- PASS: rate_limit_after_burst (0.00s)
```
</details>

---

## Summary

| # | Scenario | Modules Tested | Status |
|---|----------|---------------|--------|
| 1 | Core HTTP Pipeline | http.server, http.router, http.handler | PASS |
| 2 | Order Processing Pipeline | api.handler, statemachine.engine, state.tracker, messaging.broker, messaging.handler | PASS |
| 3 | Order Pipeline Error Path | statemachine error states, transition rejection | PASS |
| 4 | Broker Messaging | messaging.broker, messaging.handler (pub/sub) | PASS |
| 5 | Config Push & Reload | WorkflowUIHandler (config CRUD, reload, validate) | PASS |
| 6 | Dynamic Load & Execute | dynamic.component (Yaegi), HTTP API | PASS |
| 7 | Dynamic Hot Reload | v1->v2 update, behavior change, deletion | PASS |
| 8 | Dynamic Sandbox | 6 blocked imports, valid after rejections | PASS |
| 9 | Dynamic Engine Integration | dynamic.component in full engine lifecycle | PASS |
| 10 | Dynamic Message Processor | dynamic component + broker pub/sub | PASS |
| 11 | Integration Workflow | IntegrationWorkflowHandler, HTTP connectors | PASS |
| 12 | Event Workflow | EventWorkflowHandler, pattern matching | PASS |
| 13 | Data Transformer | extract->map->filter pipeline | PASS |
| 14 | Scheduler Workflow | SchedulerWorkflowHandler, job execution | PASS |
| 15 | Metrics & Health Check | metrics.collector, health.checker, /health /ready /live | PASS |
| 16 | EventBus Bridge | messaging.broker.eventbus, bidirectional pub/sub | PASS |
| 17 | Webhook Sender | webhook.sender, delivery + dead letter queue | PASS |
| 18 | Slack Notification | notification.slack, mock endpoint verification | PASS |
| 19 | Middleware: Auth | http.middleware.auth (Bearer token) | PASS |
| 20 | Middleware: Rate Limit | http.middleware.ratelimit (token bucket) | PASS |
| 21 | Middleware: CORS | http.middleware.cors (origins, preflight) | PASS |
| 22 | Middleware: Request ID | RequestID middleware (generate/preserve) | PASS |
| 23 | Middleware: Full Chain | CORS + RateLimit + Auth + Logging combined | PASS |

**Total: 23 scenarios, 23 PASS, 0 FAIL**
