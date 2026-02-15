# Workflow Platform Vision and Roadmap

**Status**: Living Document
**Last Updated**: February 2026
**Audience**: Engineering, Product, Investors, Strategic Partners

---

## Table of Contents

1. [Vision Statement](#1-vision-statement)
2. [Current State Assessment](#2-current-state-assessment)
3. [Architecture Evolution](#3-architecture-evolution)
   - [3a. Durable Execution Engine](#3a-durable-execution-engine)
   - [3b. Event-Native Infrastructure](#3b-event-native-infrastructure)
   - [3c. Infrastructure Management](#3c-infrastructure-management)
   - [3d. Observability Platform](#3d-observability-platform-datadog-for-workflows)
   - [3e. Scale and Reliability](#3e-scale-and-reliability)
4. [Phased Roadmap](#4-phased-roadmap)
5. [Competitive Positioning](#5-competitive-positioning)
6. [Success Metrics](#6-success-metrics)

---

## 1. Vision Statement

### The Problem

The workflow and automation market is vast, fragmented, and full of tools that
solve half the problem well and the other half poorly.

**Consumer/prosumer tools** (Zapier, Make, IFTTT, n8n) let non-engineers glue
SaaS apps together, but they crumble under production load. They poll instead
of stream. They retry blindly instead of recovering intelligently. They have
no concept of state machines, compensation, or multi-tenant isolation. When
something fails at 3 AM, you get an email that says "step 4 failed" with no
way to inspect what happened, replay the execution, or understand why.

**Enterprise iPaaS platforms** (MuleSoft, Workato, Boomi, SnapLogic) solve
governance and connectivity but cost six figures, require specialized teams,
and lock you into proprietary ecosystems. Extending them with custom logic
means escaping into SDKs that fight the platform.

**Developer-first tools** (Temporal, Airflow, Dagster, Prefect) give engineers
full control but require writing Go/Python/Java code for every workflow. They
have no visual builder, no built-in multi-tenancy, no admin UI, no
config-driven deployment. Onboarding a new workflow means a PR, a code review,
a deploy cycle.

**App platforms** (Power Platform, ServiceNow, OutSystems, Mendix) let
"citizen developers" build apps visually but run only in their cloud, offer
limited extensibility, and charge per-user licensing that scales to absurdity.

**Data pipeline tools** (Benthos/Redpanda Connect, Apache NiFi, Kafka Connect)
are event-native and composable but focused on data movement, not application
orchestration. They do not manage auth, serve UIs, handle state machines, or
provide multi-tenant deployment.

No single tool in the market provides:

- **Developer-grade durability** (event sourcing, idempotency, replay, sagas)
  with **business-usable UX** (visual builder, YAML config, no-code admin)
- **Event-native architecture** (streams, CDC, exactly-once, backfill) instead
  of polling-based triggers
- **Full application platform** (auth, databases, APIs, state machines, UIs)
  instead of just automation glue or data pipelines
- **Operational transparency** at the level of "show me exactly what happened
  at step 7 of execution #4,231, let me replay it with modified inputs, and
  diff it against yesterday's execution"
- **Open-core extensibility** with Go plugins, dynamic hot-reload, and a
  module ecosystem -- not a walled garden

### The Workflow Mental Model

Workflow operates on a single, powerful mental model:

> **When this system state changes, coordinate these actors, with guarantees,
> visibility, and recovery.**

This is not "if new row in Google Sheets, send Slack message." This is:

- When a payment webhook arrives, validate it, update the order state machine,
  reserve inventory, send a confirmation, and if any step fails, compensate
  everything that succeeded -- with a complete audit trail, per-step timing,
  and the ability to replay the entire sequence from any point.

- When a database row changes, stream the change event through a transform
  pipeline, enrich it with data from three APIs, publish it to a topic, and
  guarantee exactly-once delivery with dead-letter handling and backfill
  capability.

- When a scheduled job runs, execute a multi-step pipeline across distributed
  workers with per-tenant rate limiting, circuit breakers, and automatic
  retry with exponential backoff -- and surface the entire execution in a
  timeline UI that any team member can inspect.

### What Workflow Is

Workflow is a **configuration-driven orchestration platform** that turns
declarative YAML into running, observable, scalable applications.

It is the platform where:

1. You **build applications** entirely in YAML -- HTTP APIs, state machines,
   event pipelines, scheduled jobs, data transforms -- no code changes needed.
2. You **deploy and manage** those applications with the platform handling
   infrastructure, scaling, secrets, and lifecycle.
3. You **handle eventing** with source/sink connectors, transformations,
   delivery guarantees, replay, and dead-letter debugging.
4. You **view activity** within any workflow -- see exactly what happened
   during any request or event, step by step, with inputs and outputs.
5. You **mock and replay** requests for debugging -- pause at a step, swap in
   mock data, replay a failed execution with modified inputs.
6. You **observe everything** with per-step metrics, distributed tracing,
   structured logging, and real-time dashboards.
7. You **scale** to handle production traffic with worker pools, tenant
   isolation, rate limiting, circuit breakers, and auto-scaling.
8. The platform can be offered as a **SaaS/PaaS** where customers pay to
   build and run their applications on managed infrastructure.

### What Workflow Is Not

Workflow is not:

- **"More connectors"** -- We will never compete with Zapier's 6,000 app
  integrations. We provide the engine; connectors are plugins.
- **"Cheaper Zapier"** -- Price is not our differentiator. Capability,
  durability, and transparency are.
- **"Another drag-and-drop ETL"** -- We do not move data between warehouses.
  We orchestrate application logic with data movement as one capability.
- **"A low-code app builder"** -- We do not generate forms and tables from
  database schemas. We orchestrate workflows that power applications.

### Target Users

**Primary ICP: Platform Engineering Teams**
Teams building internal developer platforms who need workflow orchestration
that integrates with their infrastructure (Kubernetes, Kafka, PostgreSQL,
Vault) and serves their organization's custom needs.

**Secondary ICP: Data-Heavy SaaS Companies**
Companies processing high-volume event streams (payments, IoT, logistics,
healthcare) who need durable, observable, event-native orchestration with
multi-tenant isolation.

**Tertiary ICP: AI-Enabled Products**
Teams building products with LLM-powered features who need to orchestrate
AI agent workflows with guardrails, retry logic, human-in-the-loop steps,
and cost tracking.

**Future ICP: Technical Operators and Consultancies**
Agencies and operators who deploy Workflow as a managed platform for their
clients, using the multi-tenant hierarchy to isolate customer workloads.

### Strategic Positioning

**Compete on**: Event-native architecture, execution replay and observability,
config-driven simplicity, open-core extensibility, AI-assistable orchestration.

**Avoid competing on**: Connector breadth, price, no-code simplicity for
non-technical users.

---

## 2. Current State Assessment

### What Exists Today

The Workflow engine is a working, production-capable system built on the
[CrisisTextLine/modular](https://github.com/CrisisTextLine/modular) framework.
It currently provides:

- **65+ built-in module types** registered in `engine.go`, covering HTTP
  servers, routers, middleware, messaging (Kafka, NATS, EventBus), state
  machines, databases, storage (S3, GCS, local, SQLite), auth (JWT, OIDC,
  SAML, LDAP, AWS IAM, Kubernetes), metrics, health checks, logging,
  OpenAPI generation, and more.

- **Pipeline execution engine** (`module/pipeline_executor.go`) with ordered
  step sequences, error strategies (stop, skip, compensate), per-step
  timeout, compensation chains, conditional routing, and metadata propagation.

- **Pipeline-native API routes** that replace delegation with declarative
  step sequences. The admin API (`admin/config.yaml`) dogfoods this pattern,
  expressing its own CRUD operations as `step.request_parse` ->
  `step.db_query` -> `step.json_response` pipelines.

- **Visual workflow builder** (React + ReactFlow + Zustand + TypeScript,
  served via Vite) with a drag-and-drop canvas, module schemas, and
  config editing.

- **Multi-tenant data model** with Company > Organization > Project >
  Workflow hierarchy, role-based access control (Owner, Admin, Editor,
  Viewer), and PostgreSQL-backed persistence.

- **60+ REST API endpoints** covering auth, workflow CRUD, versioning,
  execution tracking, audit logging, IAM provider management, dashboard
  metrics, and user management.

- **Execution tracking** (`store/pg_execution.go`) with per-execution and
  per-step recording including input/output data, status, error messages,
  timing, and metadata -- stored in PostgreSQL via `pgxpool`.

- **Distributed tracing** (`observability/tracing/provider.go`) with
  OpenTelemetry OTLP export, configurable sample rates, and service
  metadata propagation.

- **Worker pool** (`scale/worker_pool.go`) with min/max goroutine scaling,
  per-tenant task routing, idle timeout, and task result tracking.

- **AI integration** (`ai/`) with Anthropic Claude direct API for component
  generation and GitHub Copilot SDK for session-based dev tooling.

- **Dynamic hot-reload** (`dynamic/`) using Yaegi interpreter for loading
  Go components at runtime with stdlib-only sandbox.

- **Secrets management** with HashiCorp Vault, AWS Secrets Manager, and
  environment variable backends.

- **Infrastructure artifacts** including multi-stage Dockerfile and Helm
  chart for Kubernetes deployment.

- **37 example YAML configs** in `example/` demonstrating order processing,
  webhook pipelines, data sync, event-driven workflows, state machines,
  multi-workflow e-commerce apps, chat platforms, and more.

### Capability Matrix

| Area                       | Status    | Completeness | Notes                                                    |
|----------------------------|-----------|--------------|----------------------------------------------------------|
| Module Ecosystem           | EXCELLENT | 90%          | 65+ types, factory registry, schema metadata             |
| Pipeline Execution         | EXCELLENT | 85%          | Steps, error strategies, compensation, conditional       |
| Pipeline-Native Routes     | EXCELLENT | 80%          | Admin API fully dogfoods this pattern                    |
| Worker Pool                | EXCELLENT | 90%          | Min/max scaling, tenant routing, idle timeout            |
| Multi-tenancy              | STRONG    | 85%          | Full hierarchy, RBAC, quotas                             |
| Visual Builder UI          | STRONG    | 85%          | ReactFlow canvas, module schemas, config editing         |
| Workflow Versioning        | STRONG    | 85%          | Snapshots, version promotion                             |
| Auth (JWT/OIDC/SAML)      | STRONG    | 80%          | JWT + IAM framework + user store                         |
| Secrets Management         | STRONG    | 80%          | Vault, AWS SM, env vars                                  |
| Helm/Docker                | STRONG    | 80%          | Multi-stage Docker, Helm chart                           |
| Distributed Tracing        | GOOD      | 75%          | OpenTelemetry OTLP, configurable sampling                |
| Rate Limiting              | GOOD      | 75%          | Tenant quotas + worker pool                              |
| Connectors (Kafka/NATS/S3) | GOOD      | 70%          | Core connectors present, no plugin interface             |
| Plugin Framework           | GOOD      | 70%          | Registry, manifest, validator, dynamic hot-reload        |
| Data Transforms            | GOOD      | 65%          | extract/map/filter/set, no JQ/JS runtime                 |
| Delivery Guarantees        | GOOD      | 70%          | At-least-once, no exactly-once semantics                 |
| AI Integration             | PARTIAL   | 60%          | Anthropic + Copilot SDK, no guardrails                   |
| Step Visibility            | PARTIAL   | 60%          | Step recording, no real-time timeline UI                 |
| Compensation/Saga          | PARTIAL   | 50%          | Basic compensation in pipeline, no distributed saga      |
| Compliance Framework       | PARTIAL   | 50%          | SOC2/HIPAA stubs, no audit completeness                  |
| Event Sourcing             | PARTIAL   | 40%          | Execution snapshots, no append-only event stream         |
| Replay/Mock                | MINIMAL   | 30%          | Webhook replay only, no step-level mock/replay           |
| IaC/Deployment             | MINIMAL   | 25%          | Helm chart only, no deployment API                       |
| Idempotency                | MINIMAL   | 20%          | Request ID only, no dedup store                          |

### Where Workflow Already Excels

**1. Config-driven application building.** No other platform lets you express
a full application -- HTTP API with auth, database queries, state machines,
event publishing, and observability -- entirely in YAML. The
`admin/config.yaml` proves this works: the engine's own admin API is built
using its own pipeline-native routes.

Example from the admin API (a real production route):

```yaml
# From admin/config.yaml -- the engine serves its own API using pipelines
- method: GET
  path: "/api/v1/admin/companies/{id}"
  handler: admin-v1-queries
  middlewares: [admin-cors, admin-auth-middleware]
  pipeline:
    steps:
      - name: parse-request
        type: step.request_parse
        config:
          path_params: [id]
      - name: get-company
        type: step.db_query
        config:
          database: admin-db
          query: >
            SELECT id, name, slug, owner_id, parent_id, is_system,
            metadata, created_at, updated_at
            FROM companies WHERE id = ?
          params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
          mode: single
      - name: check-found
        type: step.conditional
        config:
          field: "steps.get-company.found"
          routes:
            "false": not-found
      - name: respond
        type: step.json_response
        config:
          status: 200
          body_from: "steps.get-company.row"
      - name: not-found
        type: step.json_response
        config:
          status: 404
          body:
            error: "Company not found"
```

This is not a demo. This is the running admin API. Every CRUD endpoint
for companies, organizations, projects, and workflows is expressed this way.

**2. Module ecosystem breadth.** 65+ module types covering HTTP, messaging,
state machines, databases, auth, storage, observability, and more --
instantiated from YAML config with zero code.

**3. Multi-tenant architecture.** A full Company > Organization > Project >
Workflow hierarchy with RBAC, quotas, and tenant-scoped execution. This is
table stakes for SaaS but rare in open-source workflow engines.

**4. Visual builder with schema awareness.** The React + ReactFlow UI reads
module schemas from `schema/module_schema.go` and presents config fields,
input/output ports, and validation -- not a generic JSON editor.

**5. Pipeline execution model.** Ordered steps with conditional routing,
error strategies, compensation chains, and template expressions. This is
more expressive than most no-code tools and simpler than writing Go/Python.

### Key Gaps Mapped to Market Opportunity

| Market Gap                              | Current State      | Opportunity                                         |
|-----------------------------------------|--------------------|-----------------------------------------------------|
| Durable, replayable execution           | Snapshots only     | Event store + replay = #1 differentiator            |
| Event-native (not polling)              | Kafka/NATS exist   | CDC, schema registry, DLQ, backfill needed          |
| Operational transparency ("Datadog")    | Step recording     | Timeline UI, live tracing, diff, breakpoints        |
| Composable connectors (Benthos-style)   | Hard-coded modules | Plugin interface for source/sink/transform          |
| Infrastructure-as-Config                | Helm chart only    | Declare infra in YAML, platform provisions it       |
| Exactly-once delivery                   | At-least-once      | Idempotency store + outbox pattern                  |
| SaaS billing and onboarding             | None               | Stripe integration, self-service signup             |

---

## 3. Architecture Evolution

Workflow needs to evolve from a **config-driven application builder** (what it
is today) into a **full orchestration platform** (where it is going). This
section describes the five pillars of that evolution.

### Reference Architecture

The target architecture follows this reference model:

```
+------------------------------------------------------------------+
|                         CONTROL PLANE                             |
|  +------------------+  +------------------+  +-----------------+ |
|  | Workflow API     |  | Definition       |  | Scheduler       | |
|  | start/signal/    |  | Registry         |  | cron + event    | |
|  | cancel/query     |  | (versioned)      |  | triggers        | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
                               |
+------------------------------------------------------------------+
|                       EXECUTION ENGINE                            |
|  +------------------+  +------------------+  +-----------------+ |
|  | Orchestrator     |  | Task Queues      |  | Workers         | |
|  | state machine +  |  | per-tenant       |  | horizontally    | |
|  | saga coordinator |  | priority + DLQ   |  | scalable        | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
                               |
+------------------------------------------------------------------+
|                       STATE & HISTORY                             |
|  +------------------+  +------------------+  +-----------------+ |
|  | Event Store      |  | Materialized     |  | Idempotency     | |
|  | append-only      |  | State Views      |  | Store           | |
|  | source of truth  |  | fast queries     |  | dedup keys      | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
                               |
+------------------------------------------------------------------+
|                         CONNECTORS                                |
|  +------------------+  +------------------+  +-----------------+ |
|  | Sources          |  | Transform        |  | Sinks           | |
|  | HTTP, Kafka,     |  | Runtime          |  | HTTP, Kafka,    | |
|  | NATS, CDC, S3    |  | JQ/JS/WASM       |  | NATS, S3, DB    | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
                               |
+------------------------------------------------------------------+
|                        OBSERVABILITY                              |
|  +------------------+  +------------------+  +-----------------+ |
|  | Execution        |  | Replay &         |  | Metrics &       | |
|  | Timeline UI      |  | Backfill         |  | Tracing         | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
                               |
+------------------------------------------------------------------+
|                         GOVERNANCE                                |
|  +------------------+  +------------------+  +-----------------+ |
|  | RBAC + Policy    |  | Environments     |  | Data Residency  | |
|  | tenant isolation |  | dev/stage/prod   |  | + Retention     | |
|  +------------------+  +------------------+  +-----------------+ |
+------------------------------------------------------------------+
```

The execution flow for every event through the platform:

```
Ingress -> Normalize -> Decide -> Dispatch -> Execute -> Advance -> Observe -> Recover
   |          |           |          |           |          |          |          |
   |          |           |          |           |          |          |          |
 receive   validate    route to   place on   worker     update     emit      handle
 event     + schema    correct    task       picks up   state      traces    failures
           check       pipeline   queue      + runs     + record   + metrics + retry
                                             steps      event      + timeline + DLQ
```

### 3a. Durable Execution Engine

The most critical gap in the current system. Today, executions are recorded
as snapshots in PostgreSQL (`store/pg_execution.go`). This captures _what
happened_ but not _why_ or _how to replay it_. To become a durable execution
platform, we need an event-sourced execution model.

#### Event Store: Append-Only Execution History

Every action during a pipeline execution becomes an immutable event in an
append-only log. This log is the source of truth -- the materialized state
(current execution status, step outputs) is derived from it.

**Schema:**

```sql
CREATE TABLE execution_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id    UUID NOT NULL REFERENCES workflow_executions(id),
    sequence_num    BIGINT NOT NULL,
    event_type      TEXT NOT NULL,
    event_data      JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Ordering guarantee within an execution
    UNIQUE (execution_id, sequence_num)
);

CREATE INDEX idx_execution_events_exec_id ON execution_events(execution_id);
CREATE INDEX idx_execution_events_type ON execution_events(event_type);
CREATE INDEX idx_execution_events_created ON execution_events(created_at);
```

**Event types:**

```go
// Event types for the execution event store
const (
    EventExecutionStarted    = "execution.started"
    EventStepStarted         = "step.started"
    EventStepInputRecorded   = "step.input_recorded"
    EventStepOutputRecorded  = "step.output_recorded"
    EventStepCompleted       = "step.completed"
    EventStepFailed          = "step.failed"
    EventStepSkipped         = "step.skipped"
    EventStepCompensated     = "step.compensated"
    EventConditionalRouted   = "conditional.routed"
    EventRetryAttempted      = "retry.attempted"
    EventExecutionCompleted  = "execution.completed"
    EventExecutionFailed     = "execution.failed"
    EventExecutionCancelled  = "execution.cancelled"
    EventSagaCompensating    = "saga.compensating"
    EventSagaCompensated     = "saga.compensated"
)
```

**Example event stream for an order pipeline:**

```json
[
  {
    "sequence_num": 1,
    "event_type": "execution.started",
    "event_data": {
      "pipeline": "process-order",
      "trigger": "http",
      "trigger_data": {"method": "POST", "path": "/api/orders"},
      "tenant_id": "acme-corp",
      "idempotency_key": "ord_20260215_abc123"
    }
  },
  {
    "sequence_num": 2,
    "event_type": "step.started",
    "event_data": {
      "step_name": "parse-request",
      "step_type": "step.request_parse",
      "step_index": 0
    }
  },
  {
    "sequence_num": 3,
    "event_type": "step.input_recorded",
    "event_data": {
      "step_name": "parse-request",
      "input": {"headers": {"content-type": "application/json"}, "body_size": 245}
    }
  },
  {
    "sequence_num": 4,
    "event_type": "step.output_recorded",
    "event_data": {
      "step_name": "parse-request",
      "output": {"body": {"product_id": "SKU-001", "quantity": 3, "customer_id": "cust_789"}}
    }
  },
  {
    "sequence_num": 5,
    "event_type": "step.completed",
    "event_data": {
      "step_name": "parse-request",
      "duration_ms": 2,
      "status": "completed"
    }
  },
  {
    "sequence_num": 6,
    "event_type": "step.started",
    "event_data": {
      "step_name": "check-inventory",
      "step_type": "step.http_call",
      "step_index": 1
    }
  }
]
```

Every pipeline execution becomes a replayable history. This is the
foundation for replay, debugging, audit, and exactly-once semantics.

#### Materialized State: Fast Current-State Views

The event store is append-only and optimized for writes. For reads (dashboard,
API queries, the timeline UI), we maintain materialized views that are
projections of the event stream.

```go
// MaterializedExecution is the read-optimized view of an execution.
// Rebuilt from the event stream on demand or via background projection.
type MaterializedExecution struct {
    ID            uuid.UUID
    WorkflowID    uuid.UUID
    Pipeline      string
    TenantID      string
    Status        ExecutionStatus
    CurrentStep   string
    StepResults   []MaterializedStep
    StartedAt     time.Time
    CompletedAt   *time.Time
    DurationMs    int64
    ErrorMessage  string
    RetryCount    int
    Metadata      map[string]any
}

type MaterializedStep struct {
    Name       string
    Type       string
    Status     StepStatus
    Input      json.RawMessage
    Output     json.RawMessage
    Error      string
    StartedAt  time.Time
    DurationMs int64
    Attempts   int
}
```

The materialized view is rebuilt by replaying events. This means:

- **Corrupted state?** Rebuild from events.
- **Need a new view?** Write a new projection.
- **Debugging?** Walk the event stream step by step.
- **Audit?** The event stream _is_ the audit log.

#### Idempotency Store: Exactly-Once Effects

Every external-facing step (HTTP call, database write, message publish) gets
an idempotency key. Before executing, the step checks the idempotency store.
If the key exists, the stored result is returned without re-executing.

```sql
CREATE TABLE idempotency_keys (
    key             TEXT PRIMARY KEY,
    execution_id    UUID NOT NULL,
    step_name       TEXT NOT NULL,
    result          JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,

    -- Prevent duplicate keys for different executions
    UNIQUE (execution_id, step_name)
);

CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);
```

**How it works in a pipeline:**

```yaml
pipelines:
  process-payment:
    idempotency:
      # Key is derived from trigger data -- same input = same key
      key_template: "pay_{{ .customer_id }}_{{ .order_id }}"
      ttl: 24h

    steps:
      - name: charge-card
        type: step.http_call
        config:
          url: "https://api.stripe.com/v1/charges"
          method: POST
          # This step is idempotent: if the key exists, return cached result
          idempotent: true
          idempotency_key: "stripe_charge_{{ .order_id }}"
```

The combination of event store + idempotency store gives us **exactly-once
semantics** for the effects of each step, even if the step itself is retried.

#### Saga Coordinator: Multi-Step Transactions with Compensation

The current pipeline supports basic compensation (`OnError: compensate` in
`module/pipeline_executor.go`). The saga coordinator extends this to
distributed, cross-service transactions.

```yaml
pipelines:
  create-order:
    saga:
      enabled: true
      timeout: 30s
      # When a step fails, compensate all previously completed steps
      # in reverse order
      compensation_order: reverse

    steps:
      - name: reserve-inventory
        type: step.http_call
        config:
          url: "http://inventory-service/api/reserve"
          method: POST
          body_from: "trigger"
        compensate:
          type: step.http_call
          config:
            url: "http://inventory-service/api/release"
            method: POST
            body_from: "steps.reserve-inventory.output"

      - name: charge-payment
        type: step.http_call
        config:
          url: "http://payment-service/api/charge"
          method: POST
          body_from: "steps.reserve-inventory.output"
          idempotent: true
          idempotency_key: "charge_{{ .order_id }}"
        compensate:
          type: step.http_call
          config:
            url: "http://payment-service/api/refund"
            method: POST
            body_from: "steps.charge-payment.output"

      - name: create-shipment
        type: step.http_call
        config:
          url: "http://shipping-service/api/shipments"
          method: POST
          body_from: "steps.charge-payment.output"
        compensate:
          type: step.http_call
          config:
            url: "http://shipping-service/api/shipments/cancel"
            method: DELETE
            body_from: "steps.create-shipment.output"
```

If `create-shipment` fails, the saga coordinator automatically runs the
compensation for `charge-payment` (refund) and then `reserve-inventory`
(release), recording every compensation step in the event store.

**What this means in practice:** Every pipeline execution becomes a durable,
replayable, auditable sequence of events. If a step fails, the system knows
exactly what happened, can compensate automatically, and an operator can
replay the execution to understand and fix the issue.

### 3b. Event-Native Infrastructure

The current system supports Kafka, NATS, and EventBus as messaging modules.
But event-native means more than "can publish to Kafka." It means events are
first-class citizens: they have schemas, delivery guarantees, dead-letter
handling, replay capability, and backfill support.

#### Source Connectors: Ingress from Anywhere

Sources bring events into Workflow pipelines. Each source is a module that
implements a common `EventSource` interface and can be configured in YAML.

**Available today (from `engine.go` module registry):**

| Source             | Module Type           | Status       |
|--------------------|-----------------------|--------------|
| HTTP/Webhook       | `http.handler`        | Production   |
| Kafka Consumer     | `messaging.kafka`     | Production   |
| NATS Subscriber    | `messaging.nats`      | Production   |
| EventBus (in-proc) | `messaging.broker.eventbus` | Production |
| Cron Schedule      | `scheduler.modular`   | Production   |
| S3 Event           | `storage.s3`          | Partial      |

**Planned sources:**

| Source             | Module Type              | Priority  |
|--------------------|--------------------------|-----------|
| PostgreSQL CDC     | `source.postgres_cdc`    | Phase 2   |
| MySQL CDC          | `source.mysql_cdc`       | Phase 3   |
| Redis Streams      | `source.redis_stream`    | Phase 2   |
| SQS                | `source.aws_sqs`         | Phase 2   |
| SNS                | `source.aws_sns`         | Phase 2   |
| GCP Pub/Sub        | `source.gcp_pubsub`     | Phase 3   |
| File Watch         | `source.file_watch`      | Phase 3   |

**Connector configuration example:**

```yaml
modules:
  - name: order-events
    type: source.postgres_cdc
    config:
      # PostgreSQL logical replication slot
      connection: "${DATABASE_URL}"
      publication: "order_changes"
      slot_name: "workflow_order_cdc"
      tables:
        - schema: public
          table: orders
          columns: [id, status, customer_id, total, updated_at]
      # Filter: only capture UPDATE events where status changed
      filter:
        operations: [INSERT, UPDATE]
        condition: "OLD.status IS DISTINCT FROM NEW.status"
      # Output format
      output:
        format: cloudevents
        include_before: true
        include_after: true

pipelines:
  order-status-changed:
    trigger:
      type: event
      source: order-events
    steps:
      - name: log-change
        type: step.log
        config:
          message: "Order {{ .after.id }} status: {{ .before.status }} -> {{ .after.status }}"
      - name: notify
        type: step.http_call
        config:
          url: "https://hooks.slack.com/services/xxx"
          method: POST
          body:
            text: "Order {{ .after.id }} moved to {{ .after.status }}"
```

#### Sink Connectors: Egress to Anywhere

Sinks are output destinations for pipeline results. They mirror the source
list with additional targets.

**Available today:**

| Sink               | Module Type           | Status       |
|--------------------|-----------------------|--------------|
| HTTP/Webhook       | `webhook.sender`      | Production   |
| Kafka Producer     | `messaging.kafka`     | Production   |
| NATS Publisher     | `messaging.nats`      | Production   |
| S3 Upload          | `storage.s3`          | Production   |
| Slack              | `notification.slack`  | Production   |
| Database Write     | `step.db_exec`        | Production   |

**Planned sinks:**

| Sink               | Module Type              | Priority  |
|--------------------|--------------------------|-----------|
| Email (SES/SMTP)   | `sink.email`             | Phase 1   |
| Redis Publish      | `sink.redis`             | Phase 2   |
| SQS                | `sink.aws_sqs`           | Phase 2   |
| GCP Pub/Sub        | `sink.gcp_pubsub`       | Phase 3   |
| Elasticsearch      | `sink.elasticsearch`     | Phase 3   |

#### Transform Runtime: Enhanced Data Manipulation

The current pipeline supports `step.set`, `step.transform` (extract, map,
filter), `step.validate`, and template expressions. This covers many cases
but falls short for complex data transformations.

**Phase 2 enhancements:**

```yaml
steps:
  # JQ expressions for powerful JSON manipulation
  - name: transform-order
    type: step.jq
    config:
      # JQ expression applied to the pipeline context
      expression: |
        .trigger.items | map({
          sku: .product_id,
          qty: .quantity,
          subtotal: (.price * .quantity),
          tax: (.price * .quantity * 0.08)
        }) | {
          items: .,
          total: (map(.subtotal + .tax) | add),
          item_count: length
        }

  # JavaScript for complex logic (V8 isolate or QuickJS)
  - name: enrich-customer
    type: step.js
    config:
      # JS function receives step context, returns transformed data
      code: |
        function transform(ctx) {
          const customer = ctx.steps["fetch-customer"].output;
          const orders = ctx.steps["fetch-orders"].output;
          return {
            ...customer,
            lifetime_value: orders.reduce((sum, o) => sum + o.total, 0),
            order_count: orders.length,
            tier: orders.length > 50 ? "gold" : orders.length > 10 ? "silver" : "bronze"
          };
        }
      timeout: 5s
      memory_limit: 64MB
```

#### Event Schema Registry

Events flowing through the platform need validation. The schema registry
ensures events conform to expected shapes at ingress and egress.

```yaml
schemas:
  order.created:
    version: "1.0"
    type: cloudevents
    data_schema:
      type: object
      required: [order_id, customer_id, items, total]
      properties:
        order_id:
          type: string
          format: uuid
        customer_id:
          type: string
        items:
          type: array
          items:
            type: object
            required: [product_id, quantity, price]
            properties:
              product_id: { type: string }
              quantity: { type: integer, minimum: 1 }
              price: { type: number, minimum: 0 }
        total:
          type: number
          minimum: 0

  order.shipped:
    version: "1.0"
    type: cloudevents
    data_schema:
      type: object
      required: [order_id, tracking_number, carrier]
      properties:
        order_id: { type: string, format: uuid }
        tracking_number: { type: string }
        carrier: { type: string, enum: [ups, fedex, usps, dhl] }

pipelines:
  process-order:
    trigger:
      type: event
      source: order-events
      # Validate incoming events against schema
      schema: order.created

    steps:
      - name: process
        type: step.transform
        config:
          operations:
            - type: extract
              field: "data"
      - name: publish-shipped
        type: step.publish
        config:
          topic: order.shipped
          # Validate outgoing events against schema
          schema: order.shipped
```

#### Dead Letter Queues with Inspection and Replay

When a pipeline execution fails after exhausting retries, the triggering
event goes to a dead letter queue (DLQ). The DLQ is not a black hole -- it
is an inspectable, replayable queue with a UI.

```yaml
pipelines:
  process-payment:
    trigger:
      type: event
      source: payment-events

    retry:
      max_attempts: 3
      backoff: exponential
      initial_delay: 1s
      max_delay: 30s

    dead_letter:
      enabled: true
      # How long to retain DLQ messages
      retention: 7d
      # Alert when DLQ depth exceeds threshold
      alert_threshold: 100
      # Allow manual replay from DLQ UI
      replayable: true

    steps:
      - name: charge
        type: step.http_call
        config:
          url: "https://api.stripe.com/v1/charges"
          method: POST
```

**DLQ API:**

```
GET  /api/v1/dlq/pipelines/{pipeline}          # List DLQ messages
GET  /api/v1/dlq/pipelines/{pipeline}/{id}      # Get DLQ message detail
POST /api/v1/dlq/pipelines/{pipeline}/{id}/replay  # Replay single message
POST /api/v1/dlq/pipelines/{pipeline}/replay-all   # Replay all messages
DELETE /api/v1/dlq/pipelines/{pipeline}/{id}       # Discard message
```

#### Backfill: Replay Events from a Point in Time

One of the most powerful capabilities of an event-native platform: the ability
to replay historical events through a pipeline. This is how you:

- Fix a bug in your transform logic and reprocess last week's events
- Add a new pipeline and populate it with historical data
- Validate a pipeline change against production traffic before deploying

```
POST /api/v1/backfill
{
  "pipeline": "process-order",
  "source": "order-events",
  "from": "2026-02-01T00:00:00Z",
  "to": "2026-02-15T00:00:00Z",
  "filter": {
    "event_type": "order.created",
    "data.total": { "$gte": 100 }
  },
  "mode": "dry_run",          // dry_run | shadow | live
  "concurrency": 10,
  "rate_limit": "100/s"
}
```

**Backfill modes:**

| Mode      | Description                                                   |
|-----------|---------------------------------------------------------------|
| `dry_run` | Execute pipeline but discard all sink outputs. Record metrics.|
| `shadow`  | Execute pipeline and compare outputs to original executions.  |
| `live`    | Execute pipeline and deliver outputs to real sinks.           |

The `shadow` mode is particularly valuable: it lets you deploy a modified
pipeline, backfill production traffic through it, and compare results to
the original -- before promoting the change to production.

### 3c. Infrastructure Management

Today, deploying a Workflow application means: write YAML config, copy it to
a server, run `go run ./cmd/server -config my-app.yaml`. This works for
development but not for a platform where hundreds of customers deploy
thousands of workflows.

#### Deployment API

The platform receives workflow configurations, provisions required resources,
deploys the workflow, and health-checks it.

```
POST /api/v1/deployments
{
  "workflow_id": "uuid",
  "version": 3,
  "environment": "production",
  "config_overrides": {
    "modules.web-server.config.address": ":0",
    "modules.rate-limiter.config.requestsPerMinute": 500
  }
}
```

**Response:**

```json
{
  "deployment_id": "dep_abc123",
  "status": "deploying",
  "environment": "production",
  "resources": {
    "pod": "workflow-acme-orders-v3-7f8d9",
    "service": "workflow-acme-orders.prod.svc",
    "ingress": "orders.acme.workflow.cloud"
  },
  "health_check": {
    "url": "http://workflow-acme-orders.prod.svc/healthz",
    "interval": "10s"
  }
}
```

#### Environment Management

Workflows progress through environments with promotion gates.

```yaml
environments:
  development:
    auto_deploy: true
    resources:
      cpu: "250m"
      memory: "256Mi"
    scaling:
      min_replicas: 1
      max_replicas: 1

  staging:
    promotion_gate:
      require_approval: false
      require_tests: true
      # Pipeline must pass all integration tests
      test_suite: "integration"
    resources:
      cpu: "500m"
      memory: "512Mi"
    scaling:
      min_replicas: 1
      max_replicas: 3

  production:
    promotion_gate:
      require_approval: true
      approvers: ["platform-team"]
      require_tests: true
      test_suite: "smoke"
      # Canary deployment: route 10% traffic to new version
      canary:
        enabled: true
        initial_weight: 10
        step_weight: 20
        step_interval: 5m
        success_criteria:
          error_rate_threshold: 0.01
          latency_p99_threshold: 500ms
    resources:
      cpu: "1000m"
      memory: "1Gi"
    scaling:
      min_replicas: 2
      max_replicas: 20
      target_cpu: 70
```

**Promotion flow:**

```
POST /api/v1/deployments/{id}/promote
{
  "from": "staging",
  "to": "production",
  "strategy": "canary",
  "approved_by": "user_id"
}
```

#### Infrastructure-as-Config

The most ambitious feature: declare the infrastructure your workflow needs
alongside the workflow itself, and the platform provisions it.

```yaml
name: order-processing-platform
version: "2.0"

# Infrastructure declarations -- the platform provisions these
infrastructure:
  databases:
    - name: orders-db
      type: postgresql
      version: "16"
      size: small          # small: 1 vCPU, 2GB RAM, 20GB storage
      extensions: [uuid-ossp, pg_trgm]
      backups:
        enabled: true
        retention: 30d
        schedule: "0 2 * * *"

  brokers:
    - name: order-events
      type: kafka
      version: "3.7"
      partitions: 12
      replication_factor: 3
      retention: 7d
      topics:
        - order.created
        - order.updated
        - order.shipped
        - order.dlq

  caches:
    - name: rate-limiter-cache
      type: redis
      version: "7"
      size: small
      maxmemory_policy: allkeys-lru

  storage:
    - name: order-attachments
      type: s3
      region: us-east-1
      lifecycle:
        - transition_to: GLACIER
          after_days: 90

# Module configuration references provisioned infrastructure
modules:
  - name: db
    type: database.workflow
    config:
      # Reference to provisioned database -- platform injects connection string
      database: "{{ .infrastructure.databases.orders-db.connection_url }}"

  - name: event-stream
    type: messaging.kafka
    config:
      brokers: "{{ .infrastructure.brokers.order-events.bootstrap_servers }}"
      topic: order.created

  - name: cache
    type: cache.modular
    config:
      backend: redis
      address: "{{ .infrastructure.caches.rate-limiter-cache.address }}"

  - name: attachments
    type: storage.s3
    config:
      bucket: "{{ .infrastructure.storage.order-attachments.bucket }}"
      region: "{{ .infrastructure.storage.order-attachments.region }}"
```

The platform resolves `{{ .infrastructure.* }}` references at deployment time
by provisioning or discovering the declared resources. In a managed SaaS
context, this means:

1. Customer defines infrastructure needs in YAML
2. Platform provisions resources (RDS, MSK, ElastiCache, S3)
3. Platform injects connection details into module config
4. Workflow starts with all dependencies satisfied
5. Platform monitors and scales infrastructure based on usage

For self-hosted deployments, the infrastructure block maps to existing
resources via a connection registry.

#### Blue/Green and Canary Deployments

```yaml
deployment:
  strategy: canary
  canary:
    initial_weight: 5        # 5% of traffic to new version
    step_weight: 15           # increase by 15% every interval
    step_interval: 10m        # every 10 minutes
    max_weight: 100
    success_criteria:
      # Automatic rollback if these thresholds are breached
      error_rate: 0.5%
      latency_p99: 800ms
      latency_p50: 200ms
    rollback:
      automatic: true
      # Keep the old version running for quick rollback
      retain_previous: true
```

#### Kubernetes Operator

A custom Kubernetes operator with CRDs for managing Workflow deployments.

```yaml
apiVersion: workflow.io/v1alpha1
kind: WorkflowDeployment
metadata:
  name: order-processing
  namespace: acme-corp
spec:
  workflowRef:
    name: order-processing
    version: 3
  environment: production
  replicas:
    min: 2
    max: 20
    targetCPU: 70
    # Custom metric scaling
    metrics:
      - type: queue_depth
        target: 100
        source: order-events
      - type: execution_rate
        target: 1000
        window: 1m
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
    limits:
      cpu: 2000m
      memory: 2Gi
  infrastructure:
    databaseRef: orders-db
    brokerRef: order-events
    cacheRef: rate-limiter
```

### 3d. Observability Platform ("Datadog for Workflows")

This is the feature set that makes Workflow irresistible to platform teams.
Not just metrics and logs -- a complete understanding of what happened during
every execution, with the tools to debug, replay, and compare.

#### Execution Timeline UI

The centerpiece of observability: a step-by-step visual timeline of every
pipeline execution with inputs, outputs, timing, errors, and metadata.

**Timeline API response:**

```
GET /api/v1/executions/{id}/timeline
```

```json
{
  "execution_id": "exec_abc123",
  "pipeline": "process-order",
  "tenant": "acme-corp",
  "status": "completed",
  "started_at": "2026-02-15T14:30:00.000Z",
  "completed_at": "2026-02-15T14:30:00.847Z",
  "duration_ms": 847,
  "trigger": {
    "type": "http",
    "method": "POST",
    "path": "/api/orders",
    "request_id": "req_xyz789"
  },
  "steps": [
    {
      "name": "parse-request",
      "type": "step.request_parse",
      "status": "completed",
      "started_at": "2026-02-15T14:30:00.001Z",
      "duration_ms": 2,
      "input": {
        "headers": {"content-type": "application/json", "x-request-id": "req_xyz789"},
        "body_size": 245
      },
      "output": {
        "body": {
          "product_id": "SKU-001",
          "quantity": 3,
          "customer_id": "cust_789"
        },
        "path_params": {}
      }
    },
    {
      "name": "validate-order",
      "type": "step.validate",
      "status": "completed",
      "started_at": "2026-02-15T14:30:00.003Z",
      "duration_ms": 1,
      "input": {
        "strategy": "required_fields",
        "required_fields": ["product_id", "quantity", "customer_id"]
      },
      "output": {
        "valid": true
      }
    },
    {
      "name": "check-inventory",
      "type": "step.http_call",
      "status": "completed",
      "started_at": "2026-02-15T14:30:00.004Z",
      "duration_ms": 234,
      "input": {
        "url": "http://inventory-service/api/check",
        "method": "POST",
        "body": {"sku": "SKU-001", "quantity": 3}
      },
      "output": {
        "available": true,
        "warehouse": "us-east-1",
        "reserved_until": "2026-02-15T14:45:00Z"
      },
      "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
      "span_id": "00f067aa0ba902b7"
    },
    {
      "name": "charge-payment",
      "type": "step.http_call",
      "status": "completed",
      "started_at": "2026-02-15T14:30:00.238Z",
      "duration_ms": 567,
      "input": {
        "url": "https://api.stripe.com/v1/charges",
        "method": "POST",
        "idempotency_key": "charge_ord_20260215_abc123"
      },
      "output": {
        "charge_id": "ch_3abc123",
        "amount": 8997,
        "currency": "usd",
        "status": "succeeded"
      },
      "idempotent": true,
      "attempts": 1,
      "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
      "span_id": "b7ad6b7169203331"
    },
    {
      "name": "respond",
      "type": "step.json_response",
      "status": "completed",
      "started_at": "2026-02-15T14:30:00.805Z",
      "duration_ms": 1,
      "input": {
        "status": 201,
        "body": {"order_id": "ord_abc123", "status": "confirmed"}
      },
      "output": {
        "http_status": 201,
        "body_size": 62
      }
    }
  ],
  "metadata": {
    "customer_id": "cust_789",
    "order_total": 89.97,
    "idempotency_key": "ord_20260215_abc123"
  }
}
```

The UI renders this as a waterfall-style timeline (similar to Chrome DevTools
network tab or Jaeger trace view) where each step is a horizontal bar showing:

- Step name and type (with icon)
- Duration as a proportional bar
- Status color (green = completed, red = failed, yellow = retried, gray = skipped)
- Expandable input/output JSON
- Link to OpenTelemetry trace span

#### Live Request Tracing

See requests flow through pipelines in real-time via Server-Sent Events.

```
GET /api/v1/executions/live?pipeline=process-order&tenant=acme-corp
Accept: text/event-stream
```

```
event: execution.started
data: {"execution_id": "exec_def456", "pipeline": "process-order", "trigger": "http"}

event: step.started
data: {"execution_id": "exec_def456", "step": "parse-request", "type": "step.request_parse"}

event: step.completed
data: {"execution_id": "exec_def456", "step": "parse-request", "duration_ms": 2}

event: step.started
data: {"execution_id": "exec_def456", "step": "check-inventory", "type": "step.http_call"}

event: step.completed
data: {"execution_id": "exec_def456", "step": "check-inventory", "duration_ms": 234}

event: execution.completed
data: {"execution_id": "exec_def456", "duration_ms": 847, "status": "completed"}
```

The live tracing view in the UI shows:

- Active executions as animated pipeline diagrams
- Step progress with real-time duration counters
- Error highlighting as failures occur
- Filterable by pipeline, tenant, status

#### Request Replay

Click any past execution and replay it with the same or modified inputs.

```
POST /api/v1/executions/{id}/replay
{
  "mode": "exact",                    // exact | modified | step_from
  "modifications": {},                // only for mode=modified
  "from_step": null,                  // only for mode=step_from
  "target_environment": "staging",    // replay in different environment
  "record_as": "replay"               // tag the new execution as a replay
}
```

**Replay modes:**

| Mode        | Description                                                |
|-------------|------------------------------------------------------------|
| `exact`     | Replay with identical trigger data and context             |
| `modified`  | Replay with modified trigger data (e.g., change a field)   |
| `step_from` | Replay from a specific step, using recorded outputs for    |
|             | all prior steps                                            |

The `step_from` mode is exceptionally powerful for debugging: if step 5 of 8
failed, you can replay from step 5 without re-executing steps 1-4. The
event store provides the recorded outputs of prior steps as input.

#### Step Mocking

Replace any step's behavior with mock data for testing pipelines without
hitting real services.

```
POST /api/v1/pipelines/{pipeline}/mock
{
  "mocks": {
    "check-inventory": {
      "output": {
        "available": false,
        "reason": "out_of_stock"
      },
      "delay_ms": 50
    },
    "charge-payment": {
      "output": {
        "charge_id": "ch_mock_123",
        "status": "succeeded"
      },
      "delay_ms": 200
    }
  },
  "trigger_data": {
    "product_id": "SKU-999",
    "quantity": 1,
    "customer_id": "cust_test"
  }
}
```

This lets developers:

- Test failure paths without breaking production services
- Validate conditional routing with controlled inputs
- Benchmark pipeline performance with simulated latencies
- Create reproducible test cases from production failures

#### Execution Diff View

Compare two executions side-by-side to understand what changed. This is
invaluable when debugging regressions: "this worked yesterday, why does it
fail today?"

```
GET /api/v1/executions/diff?left={exec_id_1}&right={exec_id_2}
```

```json
{
  "left": "exec_abc123",
  "right": "exec_def456",
  "summary": {
    "left_status": "completed",
    "right_status": "failed",
    "left_duration_ms": 847,
    "right_duration_ms": 1234,
    "steps_different": 2,
    "steps_identical": 3
  },
  "step_diffs": [
    {
      "step": "parse-request",
      "status": "identical",
      "left_duration_ms": 2,
      "right_duration_ms": 3
    },
    {
      "step": "validate-order",
      "status": "identical"
    },
    {
      "step": "check-inventory",
      "status": "output_different",
      "left_output": {
        "available": true,
        "warehouse": "us-east-1"
      },
      "right_output": {
        "available": false,
        "reason": "out_of_stock"
      },
      "diff": {
        "available": {"left": true, "right": false},
        "warehouse": {"left": "us-east-1", "right": null},
        "reason": {"left": null, "right": "out_of_stock"}
      }
    },
    {
      "step": "charge-payment",
      "status": "right_skipped",
      "left_output": {"charge_id": "ch_3abc123", "status": "succeeded"},
      "right_output": null,
      "reason": "Step skipped due to conditional routing from check-inventory"
    }
  ]
}
```

#### Pipeline Breakpoints

Pause pipeline execution at a specific step for live inspection. This turns
the platform into a debugger.

```yaml
# Set via API or UI -- not in the YAML config (breakpoints are transient)
POST /api/v1/pipelines/{pipeline}/breakpoints
{
  "step": "charge-payment",
  "condition": "steps.check-inventory.output.total > 10000",
  "action": "pause",
  "ttl": "1h",
  "notify": ["user@example.com"]
}
```

When a breakpoint is hit:

1. Execution pauses before the step runs
2. The UI shows the paused execution with all prior step outputs
3. The operator can:
   - **Inspect**: View all pipeline context, step outputs, metadata
   - **Modify**: Change the input that will be passed to the next step
   - **Resume**: Continue execution normally
   - **Skip**: Skip this step and continue with the next
   - **Abort**: Cancel the execution
   - **Replay**: Restart from an earlier step

Breakpoints are tenant-scoped and time-limited. They are never stored in the
workflow YAML -- they are operational tools, not configuration.

#### Metrics Dashboard

Per-pipeline, per-step, per-tenant metrics surfaced via Prometheus and
visualized in the admin UI.

**Pipeline-level metrics:**

| Metric                                        | Type      | Labels                       |
|-----------------------------------------------|-----------|------------------------------|
| `workflow_pipeline_executions_total`           | Counter   | pipeline, tenant, status     |
| `workflow_pipeline_duration_seconds`           | Histogram | pipeline, tenant             |
| `workflow_pipeline_active_executions`          | Gauge     | pipeline, tenant             |
| `workflow_pipeline_error_rate`                 | Gauge     | pipeline, tenant             |

**Step-level metrics:**

| Metric                                        | Type      | Labels                       |
|-----------------------------------------------|-----------|------------------------------|
| `workflow_step_duration_seconds`               | Histogram | pipeline, step, type, tenant |
| `workflow_step_executions_total`               | Counter   | pipeline, step, status       |
| `workflow_step_retries_total`                  | Counter   | pipeline, step, tenant       |
| `workflow_step_error_rate`                     | Gauge     | pipeline, step               |

**Infrastructure metrics:**

| Metric                                        | Type      | Labels                       |
|-----------------------------------------------|-----------|------------------------------|
| `workflow_worker_pool_active`                  | Gauge     | pool                         |
| `workflow_worker_pool_queue_depth`             | Gauge     | pool, tenant                 |
| `workflow_dlq_depth`                           | Gauge     | pipeline                     |
| `workflow_event_store_events_total`            | Counter   | execution_id                 |
| `workflow_connector_health`                    | Gauge     | connector, type              |

**Dashboard API (built into admin):**

```
GET /api/v1/dashboard/metrics?pipeline=process-order&period=24h
```

```json
{
  "pipeline": "process-order",
  "period": "24h",
  "summary": {
    "total_executions": 12847,
    "success_rate": 99.2,
    "avg_duration_ms": 312,
    "p50_duration_ms": 245,
    "p95_duration_ms": 678,
    "p99_duration_ms": 1234,
    "error_count": 103,
    "dlq_depth": 7
  },
  "by_step": [
    {"step": "parse-request", "avg_ms": 2, "p99_ms": 5, "error_rate": 0.0},
    {"step": "validate-order", "avg_ms": 1, "p99_ms": 3, "error_rate": 0.1},
    {"step": "check-inventory", "avg_ms": 89, "p99_ms": 456, "error_rate": 0.3},
    {"step": "charge-payment", "avg_ms": 198, "p99_ms": 890, "error_rate": 0.5},
    {"step": "respond", "avg_ms": 1, "p99_ms": 2, "error_rate": 0.0}
  ],
  "by_hour": [
    {"hour": "2026-02-15T00:00Z", "executions": 234, "errors": 2},
    {"hour": "2026-02-15T01:00Z", "executions": 189, "errors": 1}
  ]
}
```

#### Distributed Tracing

OpenTelemetry tracing is already implemented (`observability/tracing/provider.go`)
with OTLP HTTP export. The evolution adds:

- **Trace context propagation** across pipeline steps, HTTP calls, and
  message publishing
- **Automatic span creation** for every step execution with input/output
  attributes
- **Cross-service correlation** linking Workflow execution IDs to downstream
  service spans
- **Trace-to-timeline linkage** in the UI: click a trace to see the
  execution timeline, and vice versa

#### Log Aggregation

Structured logs correlated to execution IDs for searchable debugging.

```json
{
  "timestamp": "2026-02-15T14:30:00.238Z",
  "level": "info",
  "message": "Step completed",
  "execution_id": "exec_abc123",
  "pipeline": "process-order",
  "step": "check-inventory",
  "tenant": "acme-corp",
  "duration_ms": 234,
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7"
}
```

All logs include `execution_id`, `pipeline`, `step`, and `tenant` fields,
enabling queries like "show me all logs for execution exec_abc123" or "show
me all errors in the check-inventory step across all tenants in the last hour."

#### Alerting

Rule-based alerts on execution metrics, integrated with common notification
channels.

```yaml
alerts:
  - name: high-error-rate
    pipeline: process-order
    condition:
      metric: error_rate
      operator: ">"
      threshold: 5
      window: 5m
    severity: critical
    channels:
      - type: slack
        webhook: "${SLACK_ALERTS_WEBHOOK}"
      - type: email
        recipients: ["oncall@acme.com"]
      - type: pagerduty
        service_key: "${PAGERDUTY_KEY}"

  - name: dlq-growing
    pipeline: process-order
    condition:
      metric: dlq_depth
      operator: ">"
      threshold: 50
      window: 15m
    severity: warning
    channels:
      - type: slack
        webhook: "${SLACK_ALERTS_WEBHOOK}"

  - name: slow-payments
    pipeline: process-order
    step: charge-payment
    condition:
      metric: duration_p99
      operator: ">"
      threshold: 2000
      window: 10m
    severity: warning
    channels:
      - type: slack
        webhook: "${SLACK_ALERTS_WEBHOOK}"
```

### 3e. Scale and Reliability

The worker pool (`scale/worker_pool.go`) provides the foundation for
scalable execution. The evolution adds distributed coordination, fault
isolation, and auto-scaling.

#### Distributed State Machine

The current state machine uses in-process locks. For horizontal scaling,
state transitions need distributed coordination.

**Phase 1: PostgreSQL Advisory Locks**

```go
// AcquireStateLock obtains a PostgreSQL advisory lock for a state machine
// instance, preventing concurrent transitions.
func (s *PGStateStore) AcquireStateLock(ctx context.Context, instanceID string) (func(), error) {
    lockID := hashToInt64(instanceID)
    _, err := s.pool.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID)
    if err != nil {
        return nil, fmt.Errorf("acquire lock for %s: %w", instanceID, err)
    }
    return func() {
        s.pool.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockID)
    }, nil
}
```

**Phase 3: Redis-Based Distributed Locks**

```go
// RedisStateLock provides distributed locking via Redis for state machine
// transitions across multiple Workflow instances.
type RedisStateLock struct {
    client     *redis.Client
    ttl        time.Duration
    retryDelay time.Duration
}

func (l *RedisStateLock) Acquire(ctx context.Context, key string) (func(), error) {
    lockKey := "workflow:lock:" + key
    lockValue := uuid.New().String()

    for {
        ok, err := l.client.SetNX(ctx, lockKey, lockValue, l.ttl).Result()
        if err != nil {
            return nil, err
        }
        if ok {
            return func() {
                // Lua script for atomic check-and-delete
                l.client.Eval(ctx, `
                    if redis.call("get", KEYS[1]) == ARGV[1] then
                        return redis.call("del", KEYS[1])
                    end
                    return 0
                `, []string{lockKey}, lockValue)
            }, nil
        }

        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(l.retryDelay):
            continue
        }
    }
}
```

#### Worker Pools: Per-Tenant Task Queues

The existing `WorkerPool` (`scale/worker_pool.go`) supports tenant-scoped
tasks. The evolution adds:

- **Priority queues**: High-priority tenants get faster processing
- **Rate limiting per tenant**: Prevent noisy neighbors from starving others
- **Backpressure signaling**: When queue depth exceeds threshold, slow down
  ingress
- **Task routing**: Consistent hashing routes related tasks to the same
  worker for cache locality

```yaml
scale:
  worker_pool:
    min_workers: 8
    max_workers: 128
    queue_size: 4096
    idle_timeout: 30s

  tenant_quotas:
    default:
      max_concurrent: 50
      rate_limit: "100/s"
      priority: normal
    enterprise:
      max_concurrent: 500
      rate_limit: "1000/s"
      priority: high
    free:
      max_concurrent: 5
      rate_limit: "10/s"
      priority: low

  backpressure:
    # When queue depth exceeds this, start rejecting new tasks with 429
    queue_threshold: 3072
    # When queue depth exceeds this, stop accepting from non-priority sources
    soft_threshold: 2048
```

#### Horizontal Scaling

The architecture separates stateless API nodes from stateful execution:

```
                    Load Balancer
                         |
              +----------+----------+
              |          |          |
          API Node 1  API Node 2  API Node 3
              |          |          |
              +----------+----------+
                         |
                  Message Broker
                  (Kafka / NATS)
                         |
              +----------+----------+
              |          |          |
          Worker 1   Worker 2   Worker 3
              |          |          |
              +----------+----------+
                         |
                  Shared State
              (PostgreSQL + Redis)
```

- **API Nodes**: Stateless. Accept requests, validate, enqueue tasks. Scale
  with standard load balancer.
- **Message Broker**: Kafka or NATS for task distribution. Provides ordering
  guarantees and replay.
- **Workers**: Pull tasks from queues, execute pipeline steps, write results.
  Scale based on queue depth.
- **Shared State**: PostgreSQL for durable state (event store, execution
  records). Redis for ephemeral state (locks, rate counters, cache).

#### Circuit Breakers

Automatic failover for unhealthy connectors and downstream services.

```yaml
modules:
  - name: payment-client
    type: step.http_call
    config:
      url: "https://api.stripe.com/v1/charges"
      circuit_breaker:
        enabled: true
        failure_threshold: 5     # Open after 5 consecutive failures
        success_threshold: 3     # Close after 3 consecutive successes
        timeout: 30s             # Time in open state before half-open
        fallback:
          # Return cached result or error when circuit is open
          type: static
          response:
            status: 503
            body:
              error: "Payment service temporarily unavailable"
              retry_after: 30
```

Circuit breaker states are shared across workers via Redis, so all instances
see the same circuit state.

#### Bulkhead Isolation

Per-tenant resource limits prevent noisy neighbors.

```yaml
isolation:
  tenants:
    acme-corp:
      # Dedicated worker pool
      worker_pool:
        min_workers: 4
        max_workers: 32
      # Dedicated database connection pool
      db_pool:
        max_connections: 20
      # Memory limit for transforms
      transform_memory: 256MB
      # Network bandwidth limit
      egress_rate: 100Mbps

    # Default for all other tenants
    default:
      worker_pool:
        min_workers: 2
        max_workers: 8
      db_pool:
        max_connections: 5
      transform_memory: 64MB
      egress_rate: 10Mbps
```

#### Auto-Scaling

Kubernetes HPA with custom Workflow metrics.

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: workflow-workers
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: workflow-workers
  minReplicas: 2
  maxReplicas: 50
  metrics:
    - type: Pods
      pods:
        metric:
          name: workflow_worker_pool_queue_depth
        target:
          type: AverageValue
          averageValue: "100"
    - type: Pods
      pods:
        metric:
          name: workflow_pipeline_active_executions
        target:
          type: AverageValue
          averageValue: "50"
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Percent
          value: 100
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Percent
          value: 10
          periodSeconds: 120
```

#### Multi-Region

Active-active deployment with data residency controls.

```yaml
regions:
  us-east-1:
    role: primary
    services: [api, worker, scheduler]
    databases:
      event_store: rds-us-east-1
      state_store: rds-us-east-1
    brokers:
      events: msk-us-east-1

  eu-west-1:
    role: secondary
    services: [api, worker]
    databases:
      event_store: rds-eu-west-1   # Read replica + local writes
      state_store: rds-eu-west-1
    brokers:
      events: msk-eu-west-1        # Cross-region replication

  data_residency:
    eu_tenants:
      match: { tenant.region: "EU" }
      primary_region: eu-west-1
      # Data never leaves EU region
      replication: none
    us_tenants:
      match: { tenant.region: "US" }
      primary_region: us-east-1
      replication: none
    global_tenants:
      match: { tenant.region: "global" }
      primary_region: us-east-1
      replication: cross-region
```

---

## 4. Phased Roadmap

### Phase 1: Foundation (Weeks 1-8)

**Goal**: Achieve durable execution with replay capability and SaaS-ready
billing. The first external user deploys a workflow that handles production
traffic.

#### Deliverables

| Feature                        | Priority  | Effort | Package/File                              |
|--------------------------------|-----------|--------|-------------------------------------------|
| Event store (PostgreSQL)       | P0        | 2w     | `store/event_store.go`                    |
| Idempotency key store          | P0        | 1w     | `store/idempotency.go`                    |
| Execution timeline API         | P0        | 2w     | `admin/timeline_routes.go`                |
| Timeline UI component          | P0        | 2w     | `ui/src/components/ExecutionTimeline.tsx`  |
| Enhanced step recording        | P0        | 1w     | `module/pipeline_executor.go`             |
| Request replay API             | P1        | 1w     | `admin/replay_routes.go`                  |
| Stripe billing integration     | P1        | 2w     | `billing/stripe.go`                       |
| Email verification (SES)       | P1        | 1w     | `auth/email_verify.go`                    |
| API key management             | P1        | 1w     | `store/api_keys.go`                       |
| Usage metering                 | P2        | 1w     | `billing/metering.go`                     |

**Dependency graph:**

```
Event Store ──────────────────────────────────────────┐
    |                                                  |
    v                                                  v
Enhanced Step Recording ──> Timeline API ──> Timeline UI
    |
    v
Idempotency Store ──> Request Replay API

Stripe Integration ──> Usage Metering
Email Verification
API Key Management
```

**Event store integration with pipeline executor:**

The pipeline executor (`module/pipeline_executor.go`) currently logs steps
via `slog`. The event store integration wraps each step execution:

```go
// Before (current):
logger.Info("Pipeline started", "pipeline", p.Name, "steps", len(p.Steps))

// After (with event store):
eventStore.Append(ctx, executionID, EventExecutionStarted, map[string]any{
    "pipeline":   p.Name,
    "steps":      len(p.Steps),
    "trigger":    triggerData,
    "tenant_id":  tenantID,
})

// For each step:
eventStore.Append(ctx, executionID, EventStepStarted, map[string]any{
    "step_name": step.Name(),
    "step_type": step.Type(),
    "step_index": i,
})
eventStore.Append(ctx, executionID, EventStepInputRecorded, map[string]any{
    "step_name": step.Name(),
    "input":     sanitizedInput,
})
// ... execute step ...
eventStore.Append(ctx, executionID, EventStepOutputRecorded, map[string]any{
    "step_name": step.Name(),
    "output":    sanitizedOutput,
})
eventStore.Append(ctx, executionID, EventStepCompleted, map[string]any{
    "step_name":   step.Name(),
    "duration_ms": elapsed.Milliseconds(),
    "status":      "completed",
})
```

**Billing model:**

```yaml
billing:
  plans:
    free:
      price: 0
      limits:
        executions_per_month: 1000
        pipelines: 5
        steps_per_pipeline: 20
        retention_days: 7
        workers: 2
    starter:
      price: 49
      limits:
        executions_per_month: 50000
        pipelines: 25
        steps_per_pipeline: 50
        retention_days: 30
        workers: 8
    professional:
      price: 199
      limits:
        executions_per_month: 500000
        pipelines: unlimited
        steps_per_pipeline: unlimited
        retention_days: 90
        workers: 32
    enterprise:
      price: custom
      limits:
        executions_per_month: unlimited
        pipelines: unlimited
        steps_per_pipeline: unlimited
        retention_days: 365
        workers: unlimited
        features:
          - sso
          - multi_region
          - dedicated_infrastructure
          - sla
          - priority_support
```

**Phase 1 exit criteria:**

- Event store records all pipeline executions with step-level detail
- Idempotency keys prevent duplicate effects on retry
- Timeline UI shows step-by-step execution history with inputs/outputs
- Replay API can re-execute any past execution
- At least one external user has deployed and is running a workflow
- Stripe billing is collecting payments for usage

### Phase 2: Event-Native (Weeks 9-16)

**Goal**: Event-native infrastructure with connectors, schema validation,
dead-letter handling, and developer-facing debugging tools. Demonstrate
end-to-end event pipeline with replay.

#### Deliverables

| Feature                        | Priority  | Effort | Package/File                              |
|--------------------------------|-----------|--------|-------------------------------------------|
| Connector plugin interface     | P0        | 2w     | `connector/interface.go`                  |
| PostgreSQL CDC source          | P0        | 2w     | `connector/source/postgres_cdc.go`        |
| Redis Streams source           | P1        | 1w     | `connector/source/redis_stream.go`        |
| SQS source/sink                | P1        | 1w     | `connector/source/aws_sqs.go`             |
| JQ transform step              | P0        | 2w     | `module/step_jq.go`                       |
| Event schema registry          | P1        | 2w     | `schema/event_registry.go`                |
| Dead letter queue API + UI     | P0        | 2w     | `admin/dlq_routes.go`                     |
| Backfill API                   | P1        | 2w     | `admin/backfill_routes.go`                |
| Step mocking API               | P1        | 1w     | `admin/mock_routes.go`                    |
| Execution diff API + UI        | P2        | 2w     | `admin/diff_routes.go`                    |

**Connector plugin interface:**

```go
// EventSource defines the interface for event ingress connectors.
type EventSource interface {
    // Name returns the connector name.
    Name() string
    // Type returns the connector type (e.g., "postgres_cdc", "kafka").
    Type() string
    // Start begins consuming events and sends them to the output channel.
    Start(ctx context.Context, output chan<- Event) error
    // Stop gracefully shuts down the connector.
    Stop(ctx context.Context) error
    // Healthy returns the connector's health status.
    Healthy() bool
    // Checkpoint saves the current consumption position for resume.
    Checkpoint(ctx context.Context) error
}

// EventSink defines the interface for event egress connectors.
type EventSink interface {
    Name() string
    Type() string
    // Deliver sends an event to the sink. Returns error for retry.
    Deliver(ctx context.Context, event Event) error
    // DeliverBatch sends multiple events. Returns per-event errors.
    DeliverBatch(ctx context.Context, events []Event) []error
    Stop(ctx context.Context) error
    Healthy() bool
}

// Event is the universal event envelope (CloudEvents compatible).
type Event struct {
    ID          string          `json:"id"`
    Source      string          `json:"source"`
    Type        string          `json:"type"`
    Subject     string          `json:"subject,omitempty"`
    Time        time.Time       `json:"time"`
    Data        json.RawMessage `json:"data"`
    DataSchema  string          `json:"dataschema,omitempty"`
    // Internal metadata (not serialized to CloudEvents)
    TenantID    string          `json:"-"`
    PipelineID  string          `json:"-"`
    IdempotencyKey string       `json:"-"`
}
```

**PostgreSQL CDC connector example:**

```yaml
modules:
  - name: order-changes
    type: source.postgres_cdc
    config:
      connection: "${DATABASE_URL}"
      publication: "order_events"
      slot_name: "workflow_orders"
      tables:
        - schema: public
          table: orders
          columns: [id, status, customer_id, total, updated_at]
      filter:
        operations: [INSERT, UPDATE]
      output:
        format: cloudevents
        type_template: "order.{{ .operation | lower }}"

pipelines:
  order-change-processor:
    trigger:
      type: event
      source: order-changes
      schema: order.changed    # Validated against schema registry

    dead_letter:
      enabled: true
      retention: 7d

    steps:
      - name: enrich
        type: step.http_call
        config:
          url: "http://customer-service/api/customers/{{ .data.customer_id }}"
          method: GET
      - name: transform
        type: step.jq
        config:
          expression: |
            {
              order_id: .trigger.data.id,
              status: .trigger.data.status,
              customer: .steps.enrich.output,
              total: .trigger.data.total
            }
      - name: publish
        type: step.publish
        config:
          topic: enriched-orders
          schema: enriched.order
```

**Phase 2 exit criteria:**

- PostgreSQL CDC connector captures real database changes and triggers pipelines
- JQ expressions work in `step.jq` for complex data transforms
- Dead letter queue captures failed events with UI for inspection and replay
- Backfill API can replay events from a time range through a pipeline
- Step mocking allows testing pipelines without hitting external services
- Execution diff shows side-by-side comparison of two executions

### Phase 3: Infrastructure & Scale (Weeks 17-24)

**Goal**: Infrastructure-as-Config, Kubernetes operator, distributed state,
and auto-scaling. Handle 10,000 requests/minute under auto-scaling.

#### Deliverables

| Feature                        | Priority  | Effort | Package/File                              |
|--------------------------------|-----------|--------|-------------------------------------------|
| Infrastructure-as-Config       | P0        | 3w     | `infra/provisioner.go`                    |
| Kubernetes operator + CRDs     | P0        | 3w     | `operator/`                               |
| Distributed state (Redis)      | P0        | 2w     | `scale/distributed_lock.go`               |
| Blue/green deployment          | P1        | 2w     | `deploy/bluegreen.go`                     |
| Canary deployment              | P1        | 2w     | `deploy/canary.go`                        |
| Circuit breaker middleware     | P0        | 1w     | `middleware/circuit_breaker.go`            |
| Bulkhead per-tenant isolation  | P1        | 1w     | `scale/bulkhead.go`                       |
| Per-tenant metrics dashboard   | P1        | 2w     | `ui/src/components/TenantDashboard.tsx`    |
| Multi-region data routing      | P2        | 3w     | `infra/region_router.go`                  |

**Infrastructure provisioner interface:**

```go
// Provisioner manages infrastructure resources declared in workflow config.
type Provisioner interface {
    // Plan returns the resources that would be created/modified/deleted.
    Plan(ctx context.Context, config InfraConfig) (*InfraPlan, error)
    // Apply creates or updates infrastructure to match the config.
    Apply(ctx context.Context, plan *InfraPlan) (*InfraState, error)
    // Destroy removes all infrastructure for a workflow.
    Destroy(ctx context.Context, workflowID string) error
    // Status returns the current state of provisioned infrastructure.
    Status(ctx context.Context, workflowID string) (*InfraState, error)
}

// InfraConfig represents the `infrastructure:` block in workflow YAML.
type InfraConfig struct {
    Databases []DatabaseConfig `yaml:"databases"`
    Brokers   []BrokerConfig   `yaml:"brokers"`
    Caches    []CacheConfig    `yaml:"caches"`
    Storage   []StorageConfig  `yaml:"storage"`
}

// InfraPlan is the diff between desired and current state.
type InfraPlan struct {
    Create []Resource
    Update []ResourceChange
    Delete []Resource
}
```

**Kubernetes operator reconciliation loop:**

```go
func (r *WorkflowDeploymentReconciler) Reconcile(ctx context.Context,
    req ctrl.Request) (ctrl.Result, error) {

    var wd workflowv1.WorkflowDeployment
    if err := r.Get(ctx, req.NamespacedName, &wd); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 1. Resolve workflow config from registry
    config, err := r.Registry.GetVersion(ctx, wd.Spec.WorkflowRef.Name, wd.Spec.WorkflowRef.Version)
    if err != nil {
        return ctrl.Result{}, err
    }

    // 2. Provision infrastructure if declared
    if config.Infrastructure != nil {
        plan, err := r.Provisioner.Plan(ctx, *config.Infrastructure)
        if err != nil {
            return ctrl.Result{}, err
        }
        if !plan.Empty() {
            state, err := r.Provisioner.Apply(ctx, plan)
            if err != nil {
                return ctrl.Result{RequeueAfter: 30 * time.Second}, err
            }
            config = config.WithInfraState(state)
        }
    }

    // 3. Deploy workflow (create/update Deployment + Service)
    deployment := r.buildDeployment(wd, config)
    if err := r.createOrUpdate(ctx, deployment); err != nil {
        return ctrl.Result{}, err
    }

    // 4. Configure auto-scaling
    hpa := r.buildHPA(wd)
    if err := r.createOrUpdate(ctx, hpa); err != nil {
        return ctrl.Result{}, err
    }

    // 5. Update status
    wd.Status.Phase = "Running"
    wd.Status.Replicas = deployment.Status.ReadyReplicas
    return ctrl.Result{RequeueAfter: 60 * time.Second}, r.Status().Update(ctx, &wd)
}
```

**Phase 3 exit criteria:**

- Infrastructure-as-Config provisions databases and brokers from YAML
- Kubernetes operator manages WorkflowDeployment CRDs
- Distributed locks enable horizontal scaling of state machines
- Circuit breakers protect against downstream failures
- Auto-scaling handles 10,000 requests/minute with stable latency
- Per-tenant dashboard shows isolated metrics

### Phase 4: Enterprise & Ecosystem (Weeks 25-32)

**Goal**: Enterprise readiness, developer ecosystem, and the first paying
SaaS customer. The platform is ready for production workloads with SLA
commitments.

#### Deliverables

| Feature                        | Priority  | Effort | Package/File                              |
|--------------------------------|-----------|--------|-------------------------------------------|
| Saga orchestrator              | P0        | 3w     | `orchestration/saga.go`                   |
| Live request tracing (SSE)     | P0        | 2w     | `admin/live_trace_routes.go`              |
| Pipeline breakpoints           | P1        | 2w     | `debug/breakpoints.go`                    |
| AI-safe orchestration          | P1        | 2w     | `ai/guardrails.go`                        |
| Plugin marketplace UI          | P1        | 3w     | `ui/src/pages/Marketplace.tsx`            |
| TypeScript client SDK          | P0        | 2w     | `sdk/typescript/`                         |
| Python client SDK              | P1        | 2w     | `sdk/python/`                             |
| Go client SDK                  | P1        | 1w     | `sdk/go/`                                 |
| SOC2 audit readiness           | P0        | 3w     | `compliance/`                             |
| Template gallery + one-click   | P2        | 2w     | `ui/src/pages/Templates.tsx`              |
| Environment promotion UI       | P1        | 2w     | `ui/src/pages/Environments.tsx`           |

**Saga orchestrator example:**

```yaml
pipelines:
  book-travel:
    saga:
      enabled: true
      timeout: 60s
      compensation_order: reverse
      # Record saga state in event store for visibility
      track_compensation: true

    steps:
      - name: book-flight
        type: step.http_call
        config:
          url: "http://flights-api/api/bookings"
          method: POST
          body:
            origin: "{{ .trigger.origin }}"
            destination: "{{ .trigger.destination }}"
            date: "{{ .trigger.date }}"
          idempotent: true
          idempotency_key: "flight_{{ .trigger.booking_id }}"
        compensate:
          type: step.http_call
          config:
            url: "http://flights-api/api/bookings/{{ .steps.book-flight.output.booking_id }}/cancel"
            method: POST

      - name: book-hotel
        type: step.http_call
        config:
          url: "http://hotels-api/api/reservations"
          method: POST
          body:
            city: "{{ .trigger.destination }}"
            checkin: "{{ .trigger.date }}"
            nights: "{{ .trigger.nights }}"
          idempotent: true
          idempotency_key: "hotel_{{ .trigger.booking_id }}"
        compensate:
          type: step.http_call
          config:
            url: "http://hotels-api/api/reservations/{{ .steps.book-hotel.output.reservation_id }}/cancel"
            method: POST

      - name: book-car
        type: step.http_call
        config:
          url: "http://cars-api/api/rentals"
          method: POST
          body:
            city: "{{ .trigger.destination }}"
            pickup_date: "{{ .trigger.date }}"
            days: "{{ .trigger.nights }}"
        # If this fails, the saga coordinator will:
        # 1. Record saga.compensating event
        # 2. Call book-hotel compensate (cancel reservation)
        # 3. Call book-flight compensate (cancel booking)
        # 4. Record saga.compensated event
        # All visible in the execution timeline UI
        compensate:
          type: step.http_call
          config:
            url: "http://cars-api/api/rentals/{{ .steps.book-car.output.rental_id }}/cancel"
            method: POST
```

**AI-safe orchestration:**

```yaml
pipelines:
  ai-customer-support:
    steps:
      - name: classify-intent
        type: step.llm
        config:
          provider: anthropic
          model: claude-sonnet-4-20250514
          prompt_template: |
            Classify the following customer message into one of:
            billing, technical, general, escalation

            Message: {{ .trigger.message }}
          guardrails:
            # Maximum tokens to prevent runaway costs
            max_tokens: 100
            # Content filter
            block_patterns: ["ignore previous instructions", "system prompt"]
            # Cost tracking
            track_cost: true
            cost_budget_per_execution: 0.10
            # Retry with exponential backoff on rate limit
            retry:
              max_attempts: 3
              backoff: exponential

      - name: route-by-intent
        type: step.conditional
        config:
          field: "steps.classify-intent.output.classification"
          routes:
            billing: handle-billing
            technical: handle-technical
            escalation: escalate-to-human
            default: handle-general

      - name: handle-billing
        type: step.llm
        config:
          provider: anthropic
          model: claude-sonnet-4-20250514
          prompt_template: |
            You are a billing support agent. Help the customer with their
            billing question. You have access to their account data.

            Customer: {{ .trigger.customer_name }}
            Account: {{ .steps.fetch-account.output | json }}
            Message: {{ .trigger.message }}
          guardrails:
            max_tokens: 500
            # PII detection and masking in logs
            mask_pii: true
            # Human review for refund amounts over threshold
            human_review:
              condition: "output contains 'refund' AND amount > 100"
              notify: ["billing-team@company.com"]
              timeout: 24h
```

**Client SDK (TypeScript):**

```typescript
import { WorkflowClient } from '@workflow/sdk';

const client = new WorkflowClient({
  apiUrl: 'https://api.workflow.cloud',
  apiKey: process.env.WORKFLOW_API_KEY,
});

// Deploy a workflow
const deployment = await client.deployments.create({
  workflowId: 'wf_abc123',
  version: 3,
  environment: 'production',
});

// Trigger a pipeline execution
const execution = await client.executions.create({
  pipeline: 'process-order',
  data: {
    product_id: 'SKU-001',
    quantity: 3,
    customer_id: 'cust_789',
  },
  idempotencyKey: 'order_20260215_abc123',
});

// Get execution timeline
const timeline = await client.executions.timeline(execution.id);
for (const step of timeline.steps) {
  console.log(`${step.name}: ${step.status} (${step.duration_ms}ms)`);
}

// Replay a failed execution
const replay = await client.executions.replay(execution.id, {
  mode: 'modified',
  modifications: { quantity: 1 },
});

// Watch live executions
const stream = client.executions.live({
  pipeline: 'process-order',
});
stream.on('step.completed', (event) => {
  console.log(`Step ${event.step} completed in ${event.duration_ms}ms`);
});
```

**Phase 4 exit criteria:**

- Saga orchestrator handles cross-service transactions with compensation
- Live tracing shows real-time pipeline execution in the UI
- Breakpoints allow pausing and inspecting pipeline execution
- AI steps include guardrails (cost limits, PII masking, human review)
- TypeScript and Python SDKs are published to npm/PyPI
- SOC2 Type II audit is in progress
- First paying customer is on the SaaS platform with SLA
- Template gallery has 20+ one-click-deploy templates

---

## 5. Competitive Positioning

### Five-Dimension Comparison

Every workflow platform can be evaluated on five dimensions. The following
assessment compares Workflow's current state and target state against key
competitors.

| Dimension       | Description                                    | Current | Target (Phase 4) |
|-----------------|------------------------------------------------|---------|-------------------|
| Expressiveness  | Branching, state machines, long-running         | HIGH    | VERY HIGH         |
| Durability      | Retries, idempotency, replay, sagas             | PARTIAL | HIGH              |
| Governance      | RBAC, environments, audit, compliance           | HIGH    | VERY HIGH         |
| Extensibility   | Custom code, plugins, dynamic hot-reload        | VERY HIGH | VERY HIGH       |
| Observability   | Per-step visibility, replay, debugging          | PARTIAL | VERY HIGH         |

### Competitive Matrix

```
                    Expressiveness  Durability  Governance  Extensibility  Observability
                    ──────────────  ──────────  ──────────  ─────────────  ─────────────
Zapier              LOW             LOW         LOW         LOW            LOW
Make                MEDIUM          LOW         LOW         MEDIUM         LOW
n8n                 MEDIUM          LOW         MEDIUM      HIGH           LOW
Temporal            VERY HIGH       VERY HIGH   MEDIUM      HIGH           MEDIUM
Airflow/Dagster     HIGH            MEDIUM      MEDIUM      HIGH           MEDIUM
MuleSoft            HIGH            HIGH        VERY HIGH   MEDIUM         MEDIUM
Workato             MEDIUM          MEDIUM      HIGH        MEDIUM         MEDIUM
Benthos             MEDIUM          MEDIUM      LOW         HIGH           LOW
Power Platform      MEDIUM          MEDIUM      HIGH        LOW            MEDIUM
───────────────────────────────────────────────────────────────────────────────────────
Workflow (current)  HIGH            PARTIAL     HIGH        VERY HIGH      PARTIAL
Workflow (target)   VERY HIGH       HIGH        VERY HIGH   VERY HIGH      VERY HIGH
```

### Head-to-Head Positioning

#### vs. Zapier / Make

**Their strength**: 6,000+ app integrations, dead-simple UX, instant setup.

**Where Workflow wins**: Workflow is a full application platform, not
automation glue. You build entire applications in YAML with HTTP APIs,
state machines, database queries, and event pipelines. Zapier triggers
on "new row in spreadsheet"; Workflow handles "when this order state
changes, coordinate payment, inventory, shipping, and notifications with
saga compensation and exactly-once delivery."

**When to use them**: Simple SaaS-to-SaaS automations for non-technical
users who need breadth of integrations.

**When to use Workflow**: Production workloads requiring state management,
durability, multi-tenancy, observability, and custom logic.

#### vs. Temporal

**Their strength**: Durable execution engine with the strongest replay and
state management in the market. Used by major companies for mission-critical
workflows.

**Where Workflow wins**: Config-driven (no SDK required), visual builder,
built-in multi-tenancy, complete admin platform, pipeline-native CRUD routes.
Temporal requires writing Go/Java/Python workflow definitions and deploying
worker processes. Workflow requires writing YAML.

To add a new workflow in Temporal:
1. Write Go/Java/Python workflow definition
2. Write activity implementations
3. Write worker registration code
4. Code review, merge, deploy
5. Register workflow with Temporal server

To add a new workflow in Workflow:
1. Write YAML config
2. Deploy via API or UI

**When to use Temporal**: Ultra-high-durability requirements with complex
workflow logic that benefits from a real programming language (e.g., complex
branching, custom retry strategies, long-running workflows spanning days).

**When to use Workflow**: Teams that want config-driven orchestration with
a visual builder, built-in admin, and platform-level features (multi-tenancy,
billing, environments) without writing code for every workflow.

#### vs. MuleSoft / Workato

**Their strength**: Enterprise iPaaS with deep connector libraries,
governance, and compliance certifications. Trusted by Fortune 500 companies.

**Where Workflow wins**: Open-source core with no vendor lock-in. Lighter
weight. Extensible with Go plugins and dynamic hot-reload. Self-hostable.
No six-figure annual license. MuleSoft requires Anypoint Studio and
proprietary DataWeave; Workato requires their proprietary recipe builder.
Workflow uses standard YAML, Go, and open protocols (CloudEvents,
OpenTelemetry, OTLP).

**When to use MuleSoft**: Enterprise environments with existing MuleSoft
investment, complex B2B integrations, and compliance requirements that
demand certified connectors.

**When to use Workflow**: Teams that want control over their orchestration
platform, need event-native architecture, and value open-source flexibility
over enterprise hand-holding.

#### vs. n8n

**Their strength**: Self-hostable, open-source, visual workflow builder
with a growing community. The closest open-source competitor.

**Where Workflow wins**: State machines and saga coordination (n8n has no
concept of state machine workflows). Database integration as a first-class
citizen (pipeline-native routes with `step.db_query` and `step.db_exec`).
Deployment management (n8n is a single-server tool). Event-native
architecture (n8n is polling-based). Multi-tenant hierarchy with RBAC
(n8n has basic user management). Worker pool scaling (n8n uses Bull queues
in Redis). Execution replay and debugging tools beyond n8n's execution
history view.

**When to use n8n**: Teams that want a visual-first workflow builder for
moderate-complexity automations with broad community support.

**When to use Workflow**: Production workloads requiring state management,
event streaming, pipeline-native database operations, multi-tenant
isolation, and platform-level deployment management.

#### vs. Benthos (Redpanda Connect)

**Their strength**: Best-in-class data pipeline tool with composable
processors. Streams-native, fast, and elegant.

**Where Workflow wins**: Full application platform with auth, admin UI,
multi-tenancy, visual builder, state machines, and deployment management.
Benthos moves and transforms data; Workflow orchestrates entire
applications. Benthos has no concept of pipelines as API routes, no built-in
auth, no visual builder, no admin platform.

**When to use Benthos**: Pure data pipeline workloads (ETL, stream
processing, data enrichment) where you need maximum throughput and
minimal overhead.

**When to use Workflow**: Application orchestration that includes data
pipelines as one capability alongside APIs, state machines, events, and
deployment management.

#### vs. Power Platform

**Their strength**: Deepest integration with the Microsoft ecosystem. Easy
for Office 365 users to automate common tasks. Enterprise-ready governance.

**Where Workflow wins**: Not cloud-locked (runs anywhere). Developer-friendly
(YAML and Go, not proprietary expression language). Extensible with real
code (Go plugins, dynamic hot-reload). Event-native (not limited to
Microsoft connectors). Open-source core.

**When to use Power Platform**: Microsoft-centric organizations where
integration with Teams, SharePoint, Dynamics, and Azure is the primary
requirement.

**When to use Workflow**: Teams that need cloud-agnostic, developer-friendly
orchestration with event-native architecture and open-source flexibility.

### The Unique Workflow Position

No other platform sits at the intersection of these five attributes:

```
         CONFIG-DRIVEN           EVENT-NATIVE
          (YAML, no SDK)          (CDC, streams, replay)
               \                   /
                \                 /
                 +───────────────+
                 |   WORKFLOW    |
                 +───────────────+
                /                 \
               /                   \
        FULL PLATFORM           OBSERVABLE
        (auth, DB, admin,       (timeline, replay,
         state machines,         diff, breakpoints,
         multi-tenancy)          live tracing)
                    \           /
                     \         /
                   EXTENSIBLE
                   (Go plugins,
                    dynamic reload,
                    module ecosystem)
```

This combination does not exist today. Temporal has durability but requires
an SDK. n8n has a visual builder but lacks durability. Benthos is
event-native but is not a platform. MuleSoft is a platform but is not
extensible or open.

---

## 6. Success Metrics

### Phase 1 Milestones (Weeks 1-8)

| Metric                                        | Target                      |
|-----------------------------------------------|------------------------------|
| Event store records all executions             | 100% of pipeline executions  |
| Timeline UI renders step-level detail          | <500ms render for 50 steps   |
| Replay API executes past executions            | Any execution from last 30d  |
| Idempotency prevents duplicate effects         | 0 duplicates in load test    |
| First external user deploys a workflow         | 1 user, 1 workflow           |
| External workflow handles production traffic   | 1,000 req/min sustained      |
| Stripe billing collects first payment          | 1 paid subscription          |

**Technical validation:**
- Load test: 1,000 req/min sustained for 1 hour with <100ms p95 latency
- Event store: <5ms append latency, <50ms timeline query for 1000 events
- Replay: Successfully replays 100% of recorded executions from the last 7 days

### Phase 2 Milestones (Weeks 9-16)

| Metric                                        | Target                      |
|-----------------------------------------------|------------------------------|
| PostgreSQL CDC captures changes in real-time   | <100ms end-to-end latency    |
| JQ transforms process complex JSON             | 10,000 transforms/sec        |
| DLQ captures and replays failed events         | 100% capture, 95% replay     |
| Backfill replays historical events             | 1M events in <1 hour         |
| Event pipeline demonstrated end-to-end         | CDC -> transform -> sink     |
| Step mocking enables offline testing           | Zero external dependencies   |
| Schema registry validates events               | <1ms validation overhead     |

**Technical validation:**
- CDC connector: Captures INSERT/UPDATE/DELETE within 100ms of commit
- Backfill: Processes 1M events through a 5-step pipeline in <60 minutes
- DLQ: Failed events are captured, inspectable, and replayable from UI
- Schema validation: Adds <1ms latency per event at ingress

### Phase 3 Milestones (Weeks 17-24)

| Metric                                        | Target                      |
|-----------------------------------------------|------------------------------|
| Infrastructure-as-Config provisions resources  | DB + broker + cache from YAML|
| Kubernetes operator manages deployments        | CRD -> running pod in <60s   |
| Distributed locks enable horizontal scaling    | 5 replicas, no state conflicts|
| Auto-scaling handles traffic spikes            | 0 -> 10,000 req/min in <5min |
| Circuit breakers prevent cascade failures      | <1s detection, <30s recovery |
| Per-tenant isolation under load                | No cross-tenant impact       |
| Auto-scaling Kubernetes deployment             | 10,000 req/min sustained     |

**Technical validation:**
- Load test: Scale from 2 to 20 replicas based on queue depth, stabilize at
  10,000 req/min with <200ms p95 latency
- Distributed locks: No deadlocks or state corruption under concurrent
  state machine transitions from 5 replicas
- Circuit breaker: When downstream service fails, circuit opens within 1s,
  fallback response in <10ms, recovery within 30s of downstream recovery
- Bulkhead: High-load tenant consuming 90% of capacity does not impact
  other tenants' p99 latency by more than 10%

### Phase 4 Milestones (Weeks 25-32)

| Metric                                        | Target                      |
|-----------------------------------------------|------------------------------|
| Saga coordinator handles distributed txns      | 100% compensation on failure |
| Live tracing shows real-time execution          | <100ms event delivery        |
| Breakpoints pause and resume execution         | <1s pause response           |
| AI steps include cost tracking                  | Per-execution cost visible   |
| TypeScript SDK published to npm                | v1.0.0 release               |
| Python SDK published to PyPI                   | v1.0.0 release               |
| SOC2 Type II audit in progress                 | Auditor engaged              |
| First paying SaaS customer                     | 1 customer, production SLA   |
| Template gallery                               | 20+ templates available      |

**Technical validation:**
- Saga: 3-step saga with compensation executes correctly under concurrent
  load (100 concurrent sagas)
- Live tracing: SSE events delivered to UI within 100ms of step completion
- SDK: Full coverage of platform API (deployments, executions, replay, DLQ)
- SOC2: All controls documented, evidence collection automated

### Long-Term Success Criteria (6-12 months post-Phase 4)

| Metric                                        | Target                      |
|-----------------------------------------------|------------------------------|
| Monthly recurring revenue                      | $10k MRR                     |
| Active tenants on platform                     | 50 tenants                   |
| Pipeline executions per day                    | 1M executions/day            |
| Platform uptime                                | 99.9% SLA                    |
| Community contributors                         | 25 external contributors     |
| Plugin marketplace listings                    | 50 community plugins         |
| Customer support response time                 | <4 hours (business hours)    |

---

## Appendix A: Decision Log

Key architectural decisions and their rationale.

| Decision                                  | Rationale                                              | Date       |
|-------------------------------------------|--------------------------------------------------------|------------|
| PostgreSQL for event store (not Kafka)    | Simpler ops, strong consistency, good enough perf      | 2026-02    |
| CloudEvents as event envelope             | Industry standard, interoperable, well-specified       | 2026-02    |
| JQ before JS/WASM for transforms          | Lower attack surface, deterministic, well-known syntax | 2026-02    |
| Redis for distributed locks (not etcd)    | Already in stack, simpler ops, sufficient for needs    | 2026-02    |
| Kubernetes operator (not Terraform)       | Continuous reconciliation, native scaling integration  | 2026-02    |
| Canary over blue/green as default         | Lower resource overhead, gradual rollout, auto-rollback| 2026-02    |
| Pipeline-native routes over delegation     | Proven pattern, dogfooded in admin, more transparent   | 2026-01    |
| CrisisTextLine/modular over original       | Richer module ecosystem, multi-tenancy support         | 2025       |
| Yaegi for dynamic hot-reload               | Go native, stdlib sandbox, no compilation step         | 2025       |
| Anthropic + Copilot SDK hybrid AI          | Best-in-class API + dev tool integration               | 2025       |

---

## Appendix B: Key File References

| Area                    | File                                              | Description                              |
|-------------------------|---------------------------------------------------|------------------------------------------|
| Engine core             | `engine.go`                                       | Module factory registry, 65+ types       |
| Pipeline execution      | `module/pipeline_executor.go`                     | Step sequencing, error strategies         |
| Execution tracking      | `store/pg_execution.go`                           | PostgreSQL execution persistence         |
| Execution models        | `store/models.go`                                 | WorkflowExecution, ExecutionStep structs |
| Worker pool             | `scale/worker_pool.go`                            | Min/max scaling, tenant task routing     |
| Distributed tracing     | `observability/tracing/provider.go`               | OpenTelemetry OTLP configuration         |
| Admin config            | `admin/config.yaml`                               | Dogfooded admin API (pipeline-native)    |
| Module schemas          | `schema/module_schema.go`                         | Schema definitions for all module types  |
| AI integration          | `ai/service.go`, `ai/llm/`, `ai/copilot/`        | Anthropic + Copilot SDK                  |
| Dynamic hot-reload      | `dynamic/`                                        | Yaegi interpreter, file watcher          |
| SaaS analysis           | `docs/SAAS_PLATFORM_ANALYSIS.md`                  | Gap analysis for platform transformation |
| Module best practices   | `docs/MODULE_BEST_PRACTICES.md`                   | Guidelines for module development        |
| Example configs         | `example/`                                        | 37 YAML examples across use cases        |
| UI source               | `ui/`                                             | React + ReactFlow visual builder         |

---

_This document is the definitive guide for Workflow platform evolution. It
will be updated as phases are completed and new market insights emerge._

_Last reviewed: February 2026_
