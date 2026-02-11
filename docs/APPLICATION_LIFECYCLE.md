# Workflow Application Lifecycle

How applications built with the Workflow engine are developed, deployed, updated,
scaled, and operated -- from a single YAML file on a laptop to a multi-tenant
platform serving thousands of customers.

---

## Table of Contents

1. [Development Lifecycle](#development-lifecycle)
2. [Deployment Models](#deployment-models)
3. [Configuration Updates & Hot-Reload](#configuration-updates--hot-reload)
4. [Scaling for High Load](#scaling-for-high-load)
5. [Workflow-as-a-Service Platform](#workflow-as-a-service-platform)
6. [Monetization & Licensing](#monetization--licensing)
7. [Roadmap: What Exists vs. What's Needed](#roadmap)

---

## Development Lifecycle

### Build Phase

A Workflow application starts as a YAML configuration file. The developer:

1. **Declares modules** -- picks from 30+ built-in types (HTTP server, auth, state
   machine, messaging, etc.) and configures each one
2. **Wires workflows** -- defines HTTP routes, middleware chains, state machine
   definitions, and messaging subscriptions
3. **Sets up triggers** -- specifies what starts workflows (HTTP requests, events,
   cron schedules)
4. **Writes dynamic components** (optional) -- Go source files that run through the
   Yaegi interpreter for custom logic (inventory checks, payment processing, etc.)

```bash
# Start developing
go build -o server ./cmd/server
export JWT_SECRET="dev-secret"
./server -config my-app/workflow.yaml
```

### Test Phase

- **Unit tests**: Test individual modules in isolation with mock services
- **Integration tests**: Run the full engine with test configs
- **E2E tests**: Playwright tests against the running application
- Dynamic components can be tested outside the engine since they're standard Go

### No Code Changes for Config Changes

The key value proposition: changing application behavior (adding routes, modifying
state machine transitions, adjusting middleware) requires **editing YAML only**.
No recompilation. No code review for business logic changes. The Go codebase
changes only when adding new module types.

---

## Deployment Models

### Model 1: Single Binary (Current)

```
┌──────────────┐
│  ./server    │
│  -config     │
│  app.yaml    │
│              │
│  All 24+     │
│  modules     │
│  in-process  │
└──────┬───────┘
       │
  ┌────┴────┐
  │ SQLite  │
  │ (local) │
  └─────────┘
```

- One binary, one config file, one process
- SQLite for persistence (no external dependencies)
- Suitable for: development, single-tenant deployments, edge computing
- **Limitations**: single-process, no redundancy

### Model 2: Containerized (Docker Compose)

```
┌────────────┐  ┌────────────┐  ┌────────────┐
│   Store    │  │ Prometheus │  │  Grafana   │
│  :8080     │  │  :9090     │  │  :3000     │
│            │  │            │  │            │
│ workflow   │──│ scrapes    │──│ dashboards │
│ engine     │  │ /metrics   │  │            │
└─────┬──────┘  └────────────┘  └────────────┘
      │
 ┌────┴────┐
 │ SQLite  │
 │ volume  │
 └─────────┘
```

- Store + observability stack
- Volume-mounted SQLite for data persistence
- Suitable for: small teams, staging environments, demos
- **Limitations**: still single-instance

### Model 3: Multi-Instance with External State (Target)

```
                    ┌──────────────┐
                    │ Load Balancer│
                    └──────┬───────┘
              ┌────────────┼────────────┐
        ┌─────┴─────┐ ┌───┴─────┐ ┌────┴────┐
        │ Instance 1│ │Instance 2│ │Instance 3│
        │ (engine)  │ │(engine)  │ │(engine)  │
        └─────┬─────┘ └───┬─────┘ └────┬────┘
              │            │            │
     ┌────────┴────────────┴────────────┴───────┐
     │              PostgreSQL                  │
     │    (state machines, orders, users)        │
     ├──────────────────────────────────────────┤
     │           NATS / Kafka                   │
     │    (event messaging, pub/sub)             │
     ├──────────────────────────────────────────┤
     │              Redis                       │
     │    (rate limiting, caching, sessions)     │
     └──────────────────────────────────────────┘
```

- Stateless engine instances behind a load balancer
- External state store (PostgreSQL), message broker (NATS/Kafka), cache (Redis)
- Horizontal autoscaling based on CPU/request rate
- Suitable for: production SaaS, high-traffic applications

---

## Configuration Updates & Hot-Reload

### What Happens When You Change the YAML?

The current system supports two reload mechanisms:

#### 1. Full Engine Restart (Config Changes)

When the YAML config is updated (via the UI handler or file change):

```
Time ──────────────────────────────────────────────────>

  Old Engine Running          New Engine Running
  ├── handling requests ──┤   ├── handling requests ──>
                          │   │
                     stop │   │ start
                          │   │
                     ┌────┴───┴────┐
                     │   Reload    │
                     │  (200-500ms)│
                     └─────────────┘
```

1. `engine.Stop()` -- stops all triggers, waits for module shutdown
2. `buildEngine(newConfig)` -- creates fresh engine from new YAML
3. `engine.Start()` -- starts all modules and triggers

**Impact on in-flight requests:**
- Requests currently being processed will fail (connection reset)
- No graceful draining -- this is a hard stop/start
- Typical reload time: 200-500ms for the e-commerce example (24 modules)

**What this means for production:**
- Brief downtime during config updates
- Behind a load balancer, you can do rolling updates (stop instance A, update,
  start A, stop instance B, update, start B)
- For zero-downtime, use the multi-instance model with rolling deploys

#### 2. Dynamic Component Hot-Reload (No Downtime)

Dynamic components (`.go` files loaded via Yaegi) support true hot-reload:

```
Time ──────────────────────────────────────────────────>

  Old Component V1          New Component V2
  ├── executing ───────┤    ├── executing ────────────>
                       │    │
                  ┌────┴────┴────┐
                  │   Reload     │
                  │  (50-100ms)  │
                  │  - validate  │
                  │  - compile   │
                  │  - swap      │
                  └──────────────┘
```

- File watcher detects `.go` file changes (500ms debounce)
- New source is validated (syntax + sandbox check) and compiled
- Old component is stopped, new component registered and started
- **No engine restart required** -- other modules continue running
- API endpoint: `PUT /api/dynamic/components/{id}` for programmatic updates

**This is powerful**: you can update payment processing logic, inventory check
rules, or notification templates without any downtime. The state machine,
HTTP routes, and middleware chain all keep running.

### Update Workflow: YAML Changes

```
Developer edits workflow.yaml
         │
         ▼
┌─────────────────┐     ┌────────────────┐
│ Option A: CLI   │     │ Option B: API  │
│ restart server  │     │ PUT /workflows │
│ with new config │     │ + deploy       │
└────────┬────────┘     └────────┬───────┘
         │                       │
         ▼                       ▼
┌─────────────────────────────────────────┐
│         Engine Reload Cycle             │
│  1. Stop triggers                       │
│  2. Stop modules (reverse order)        │
│  3. Parse new config                    │
│  4. Build new engine                    │
│  5. Start modules (dependency order)    │
│  6. Start triggers                      │
└─────────────────────────────────────────┘
```

### Update Workflow: Dynamic Component Changes

```
Developer edits components/payment_processor.go
         │
         ▼
┌──────────────────────────────────┐
│  File watcher detects change     │
│  (fsnotify, 500ms debounce)      │
└────────────────┬─────────────────┘
                 │
                 ▼
┌──────────────────────────────────┐
│  1. Read new source              │
│  2. Validate (syntax + sandbox)  │
│  3. Stop old component           │
│  4. Compile new source (Yaegi)   │
│  5. Register new component       │
│  6. Start new component          │
│                                  │
│  Engine keeps running            │
│  Other modules unaffected        │
└──────────────────────────────────┘
```

---

## Scaling for High Load

### Current State: What's In-Memory

| Component | Storage | Horizontal Scaling? |
|-----------|---------|:---:|
| HTTP server | Stateless | Yes |
| JWT auth validation | Stateless (token-based) | Yes |
| Middleware chain | Stateless | Yes |
| REST API resources | In-memory map | No |
| State machine instances | In-memory map | No |
| Message broker | In-memory subscribers | No |
| Dynamic components | In-process interpreter | Per-instance |
| Metrics collector | Per-process counters | Aggregate externally |
| Persistence store | SQLite (single-writer) | No |

### Scale-Up Path (Vertical)

The simplest path for moderate load:

1. **Bigger machine** -- Go's concurrency model scales well vertically
2. **Connection pooling** -- PostgreSQL instead of SQLite
3. **Response caching** -- Add Redis cache module for product catalog
4. **Rate limiting** -- Already built-in (300 req/min default)

Expected capacity on a single 4-core instance: **500-2000 req/sec** depending
on workflow complexity and component latency.

### Scale-Out Path (Horizontal)

For high load (10,000+ req/sec), the engine needs external state:

#### Step 1: Replace SQLite with PostgreSQL

```yaml
modules:
  - name: order-db
    type: database.workflow
    config:
      driver: "postgres"
      dsn: "postgres://user:pass@db:5432/ecommerce?sslmode=require"
      maxOpenConns: 25
      maxIdleConns: 5
```

The `database.workflow` module already supports PostgreSQL -- just change the
driver and DSN. The persistence store and all queries work unchanged.

#### Step 2: Replace In-Memory Broker with Distributed Messaging

The engine has an `eventbus.bridge` module type that connects to the modular
framework's EventBus system, which can be backed by NATS, Kafka, or Redis Streams.

```yaml
modules:
  - name: event-broker
    type: eventbus.bridge        # Instead of messaging.broker
    config:
      provider: "nats"
      url: "nats://nats:4222"
      topics:
        - "order.*"
```

This replaces the in-memory broker with a distributed one. Events published by
one instance are received by all instances.

#### Step 3: Distributed State Machine Locking

The state machine currently uses an in-process `sync.RWMutex`. For multi-instance
deployments, state transitions need distributed locking:

```
Instance A                    PostgreSQL                   Instance B
    │                             │                            │
    ├─ SELECT ... FOR UPDATE ────>│                            │
    │  (acquires row lock)        │                            │
    │                             │<── SELECT ... FOR UPDATE ──┤
    │                             │    (blocks, waits)         │
    ├─ UPDATE state = 'paying' ──>│                            │
    ├─ COMMIT ───────────────────>│                            │
    │  (releases lock)            │                            │
    │                             │──> lock acquired ──────────┤
    │                             │<── UPDATE state = ... ─────┤
```

**Implementation approach**: Add a `PersistentStateMachine` module type that wraps
the existing `StateMachineEngine` but uses PostgreSQL advisory locks or
`SELECT ... FOR UPDATE` for transition serialization.

#### Step 4: Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: workflow-store
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: store
        image: workflow-store:latest
        ports:
        - containerPort: 8080
        env:
        - name: JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: store-secrets
              key: jwt-secret
        resources:
          requests: { cpu: "500m", memory: "256Mi" }
          limits:   { cpu: "2000m", memory: "1Gi" }
        livenessProbe:
          httpGet: { path: /livez, port: 8080 }
        readinessProbe:
          httpGet: { path: /readyz, port: 8080 }
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target: { type: Utilization, averageUtilization: 70 }
```

### Scaling Summary

| Load Level | Architecture | Expected Capacity |
|-----------|-------------|------------------|
| Development | Single binary + SQLite | 100 req/sec |
| Small SaaS | Docker Compose + SQLite | 500 req/sec |
| Medium SaaS | 2-4 instances + PostgreSQL + NATS | 5,000 req/sec |
| Large SaaS | K8s autoscaling + PostgreSQL cluster + Kafka | 50,000+ req/sec |

---

## Workflow-as-a-Service Platform

### Vision

A centralized platform where customers configure, deploy, and manage Workflow
applications through a web interface -- while deploying to their own infrastructure.

```
┌─────────────────────────────────────────────────────────────┐
│                  Workflow Platform (you host)                │
│                                                             │
│  ┌──────────┐  ┌───────────┐  ┌────────────┐  ┌─────────┐ │
│  │ Web      │  │ Workflow  │  │ Deployment │  │ Billing │ │
│  │ Console  │  │ Designer  │  │ Manager    │  │ Engine  │ │
│  │ (React)  │  │ (UI)      │  │ (API)      │  │         │ │
│  └────┬─────┘  └─────┬─────┘  └─────┬──────┘  └────┬────┘ │
│       │              │              │              │       │
│  ┌────┴──────────────┴──────────────┴──────────────┴────┐  │
│  │              Platform API (Go)                        │  │
│  │  - User management & SSO                              │  │
│  │  - Workflow CRUD & versioning                         │  │
│  │  - Deployment orchestration                           │  │
│  │  - Usage metering & billing                           │  │
│  │  - Audit logging                                      │  │
│  └──────────────────────┬────────────────────────────────┘  │
│                         │                                   │
└─────────────────────────┼───────────────────────────────────┘
                          │
           Deploy via agent/API
                          │
          ┌───────────────┼───────────────┐
          │               │               │
    ┌─────┴─────┐  ┌─────┴─────┐  ┌─────┴─────┐
    │ Customer  │  │ Customer  │  │ Customer  │
    │ AWS VPC   │  │ GCP VPC   │  │ On-Prem   │
    │           │  │           │  │           │
    │ ┌───────┐ │  │ ┌───────┐ │  │ ┌───────┐ │
    │ │Workflow│ │  │ │Workflow│ │  │ │Workflow│ │
    │ │Engine  │ │  │ │Engine  │ │  │ │Engine  │ │
    │ └───┬───┘ │  │ └───┬───┘ │  │ └───┬───┘ │
    │     │     │  │     │     │  │     │     │
    │ ┌───┴───┐ │  │ ┌───┴───┐ │  │ ┌───┴───┐ │
    │ │ RDS   │ │  │ │CloudSQL│ │  │ │Postgres│ │
    │ └───────┘ │  │ └───────┘ │  │ └───────┘ │
    └───────────┘  └───────────┘  └───────────┘
```

### What Already Exists in the Codebase

The Workflow engine already has significant SaaS infrastructure:

| Capability | Status | Location |
|-----------|:------:|----------|
| Multi-tenant data model (companies, orgs, projects) | Built | `api/router.go`, `store/` |
| Role-based access control (Owner/Admin/Editor/Viewer) | Built | `api/middleware.go` |
| Workflow CRUD API (40+ endpoints) | Built | `api/workflow_handler.go` |
| Workflow versioning | Built | `store/pg_workflow.go` |
| Deploy/stop lifecycle management | Built | `api/router.go` |
| Execution tracking & history | Built | `store/pg_execution.go` |
| Audit trail | Built | `store/pg_audit.go` |
| OAuth2/SSO integration | Built | `api/oauth_handler.go` |
| Dashboard & analytics | Built | `api/dashboard_handler.go` |
| Cross-workflow communication | Built | `api/link_handler.go` |
| Visual workflow designer (React) | Built | `ui/` |
| Usage metering | Partial | Execution counts exist, billing integration needed |
| Customer infrastructure deployment | Not built | Agent/provisioner needed |
| Billing integration (Stripe) | Not built | API hooks exist |

### Platform Architecture: Detailed

#### Control Plane (You Host)

The control plane runs on your infrastructure and handles:

```
Platform Control Plane
├── Web Console (React/Next.js)
│   ├── Workflow visual designer (ReactFlow -- already built)
│   ├── YAML editor with validation
│   ├── Deployment dashboard
│   ├── Execution monitoring
│   └── Billing & usage dashboard
│
├── Platform API (Go)
│   ├── Authentication (JWT + OAuth2/SSO)
│   ├── Workflow management (CRUD, versions, validation)
│   ├── Deployment orchestration
│   ├── Usage metering (execution counts, compute time)
│   ├── Billing webhooks (Stripe integration)
│   └── Agent communication (gRPC/WebSocket)
│
├── Platform Database (PostgreSQL)
│   ├── Users, companies, organizations
│   ├── Workflow configs & versions
│   ├── Deployment records
│   ├── Usage metrics & billing
│   └── Audit logs
│
└── Agent Registry
    ├── Track which agents are connected
    ├── Health monitoring
    └── Deployment queue
```

#### Data Plane (Customer Hosts)

Each customer runs a lightweight agent on their infrastructure:

```
Customer Infrastructure
├── Workflow Agent (single binary, ~20MB)
│   ├── Connects to control plane via WebSocket/gRPC
│   ├── Receives deployment commands
│   ├── Manages local Workflow engine lifecycle
│   ├── Reports metrics & health back to control plane
│   ├── Handles config updates & hot-reload
│   └── Manages dynamic component updates
│
├── Workflow Engine (the actual application)
│   ├── Runs the customer's YAML config
│   ├── All data stays in customer's VPC
│   ├── Connects to customer's database, message broker, etc.
│   └── Exposes metrics to agent for reporting
│
└── Customer's Infrastructure
    ├── Database (RDS, CloudSQL, self-hosted)
    ├── Message broker (MSK, Cloud Pub/Sub)
    ├── Load balancer (ALB, Cloud LB)
    └── Secrets manager (Secrets Manager, Vault)
```

#### Deployment Flow

```
1. Customer creates workflow in web console
   │
2. Console validates YAML & saves version
   │
3. Customer clicks "Deploy" → selects target environment
   │
4. Platform API creates deployment record
   │
5. Agent on customer infra receives deployment command
   │
6. Agent pulls workflow config from platform API
   │
7. Agent builds & starts Workflow engine locally
   │
8. Agent reports status back to platform
   │
9. Platform updates deployment dashboard
   │
10. Agent periodically reports metrics:
    - Execution counts
    - Error rates
    - Compute time
    - Module health
```

#### Update Flow (Config Changes)

```
1. Customer edits workflow.yaml in web console
   │
2. Platform saves new version (v2)
   │
3. Customer clicks "Deploy v2"
   │
4. Platform sends update command to agent
   │
5. Agent performs rolling update:
   a. If running multiple instances: drain instance A
   b. Update instance A config to v2
   c. Start instance A with new config
   d. Verify health checks pass
   e. Repeat for remaining instances
   │
6. Agent reports successful rollout
   │
7. Platform marks v2 as active
```

#### Update Flow (Dynamic Component Changes)

```
1. Customer edits payment_processor.go in web console
   │
2. Platform validates source (sandbox check)
   │
3. Platform sends component update to agent
   │
4. Agent pushes source to engine via hot-reload API:
   PUT /api/dynamic/components/payment-processor
   │
5. Engine hot-reloads component (NO downtime):
   - Validates new source
   - Compiles via Yaegi
   - Swaps old component for new
   │
6. Agent confirms successful reload
   │
7. Zero downtime -- no engine restart needed
```

---

## Monetization & Licensing

### Pricing Model: Usage-Based with Tiers

The platform retains billing control even though the engine runs on customer
infrastructure, because the agent reports usage back to the control plane.

```
┌─────────────────────────────────────────────────────────────┐
│                    Pricing Tiers                            │
├─────────────┬──────────────┬───────────────┬───────────────┤
│    Free     │   Pro        │  Business     │  Enterprise   │
├─────────────┼──────────────┼───────────────┼───────────────┤
│ 1 workflow  │ 10 workflows │ Unlimited     │ Unlimited     │
│ 1,000 exec  │ 50,000 exec  │ 500,000 exec  │ Custom        │
│ /month      │ /month       │ /month        │               │
│ Community   │ Email        │ Priority      │ Dedicated     │
│ support     │ support      │ support       │ support       │
│ Shared      │ Own infra    │ Own infra     │ Own infra     │
│ hosting     │              │ + SSO/SAML    │ + SLA + audit │
│             │              │               │ + custom mods │
│ $0          │ $49/mo       │ $299/mo       │ Contact us    │
│             │ + $0.001/exec│ + $0.0005/exec│               │
└─────────────┴──────────────┴───────────────┴───────────────┘
```

### How Billing Works with Customer-Hosted Infrastructure

The key challenge: the engine runs on the customer's machines, but you need to
charge for usage. Three enforcement mechanisms:

#### Mechanism 1: License Key Validation (Primary)

```go
// Agent checks license on startup and periodically
type LicenseCheck struct {
    CustomerID    string
    LicenseKey    string
    WorkflowCount int
    ExecCount     int64
    Period        time.Time
}

// Agent → Platform API (every hour)
POST /api/v1/license/validate
{
    "customer_id": "cust-123",
    "license_key": "wf-pro-abc...",
    "usage": {
        "workflows_active": 5,
        "executions_this_period": 12450,
        "compute_seconds": 3600
    }
}

// Platform validates and returns
{
    "valid": true,
    "tier": "pro",
    "limits": {
        "max_workflows": 10,
        "max_executions_per_month": 50000
    },
    "usage_remaining": {
        "executions": 37550
    }
}
```

The agent **enforces limits locally**:
- Won't start more workflows than the tier allows
- Won't execute workflows if execution quota is exhausted
- Gracefully degrades (rejects new executions, keeps existing ones running)
- Reports actual usage for billing

#### Mechanism 2: Config Encryption (Secondary)

Workflow configs delivered to the agent are encrypted with a key that rotates
based on license validity:

```
Platform                          Agent
   │                                │
   ├── encrypt(config, rotatingKey) │
   │                                │
   │──────── encrypted config ─────>│
   │                                ├── decrypt(config, cachedKey)
   │                                │
   │   (key rotates every 24h)      │
   │                                │
   │<──── license check + refresh ──┤
   │                                │
   ├── new key ────────────────────>│
   │                                │
```

If the agent can't reach the platform for >48h (grace period), the cached key
expires and new deployments are blocked. Existing workflows continue running
(fail-open for reliability).

#### Mechanism 3: Usage Metering (Billing)

The execution tracking infrastructure already exists in the codebase:

```sql
-- Already implemented in store/pg_execution.go
SELECT
    company_id,
    COUNT(*) as total_executions,
    SUM(duration_ms) as total_compute_ms,
    DATE_TRUNC('month', started_at) as billing_period
FROM workflow_executions
WHERE started_at >= $1
GROUP BY company_id, DATE_TRUNC('month', started_at);
```

Monthly billing cycle:
1. Agent reports usage daily to platform
2. Platform aggregates by customer and billing period
3. At month end, generate invoice via Stripe API
4. Overages charged at per-execution rate
5. Dashboard shows real-time usage vs. quota

### Open Source + Commercial Model

```
┌─────────────────────────────────────────────────────────┐
│                     Open Source                          │
│  (Apache 2.0 or similar)                                │
│                                                         │
│  - Workflow engine core                                 │
│  - All 30+ built-in module types                        │
│  - Dynamic component system (Yaegi)                     │
│  - YAML config parser                                   │
│  - CLI server binary                                    │
│  - Example applications                                 │
│                                                         │
│  Anyone can self-host, modify, embed                    │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                    Commercial                           │
│  (Proprietary or BSL)                                   │
│                                                         │
│  - Web console & visual designer                        │
│  - Multi-tenant platform API                            │
│  - Deployment agent                                     │
│  - Usage metering & billing                             │
│  - SSO/SAML integration                                 │
│  - Priority support & SLAs                              │
│  - Enterprise security features                         │
│  - Managed hosting option                               │
│                                                         │
│  Pay to use the platform, agent, or managed service     │
└─────────────────────────────────────────────────────────┘
```

This model (used by Docker, GitLab, Elastic, Grafana, etc.) allows:
- Community growth and contributions to the engine
- Commercial value capture on the platform and tooling
- Customers can always self-host the engine directly (no lock-in)
- Platform provides enough value (designer, deployment, monitoring, billing)
  that customers choose to pay rather than build it themselves

---

## Roadmap

### What Exists Today

| Component | Status | Notes |
|-----------|:------:|-------|
| Workflow engine (30+ module types) | Production | Core value, well-tested |
| YAML config system | Production | Environment variable expansion, relative paths |
| Dynamic components (Yaegi) | Production | Hot-reload, sandbox, API |
| Visual workflow designer (React) | Built | ReactFlow-based, needs polish |
| Multi-tenant API (40+ endpoints) | Built | Companies, projects, RBAC, audit |
| PostgreSQL persistence | Built | Workflows, executions, users, audit |
| Execution tracking & history | Built | Step-level logs, duration, errors |
| OAuth2/SSO | Built | GitHub, Google, OIDC |
| Prometheus metrics | Built | Exposed at /metrics, Grafana dashboard |
| Health probes (K8s ready) | Built | /healthz, /readyz, /livez |

### What Needs to Be Built

| Component | Priority | Effort | Description |
|-----------|:--------:|:------:|-------------|
| Deployment agent | High | 2-3 weeks | Binary that runs on customer infra, communicates with platform |
| Distributed state machine | High | 1-2 weeks | PostgreSQL-backed state with advisory locks |
| Distributed messaging | High | 1 week | NATS/Kafka broker module (EventBus bridge exists) |
| License key system | Medium | 1 week | Key generation, validation, quota enforcement |
| Stripe billing integration | Medium | 1 week | Subscription management, usage-based invoicing |
| Web console polish | Medium | 2-3 weeks | Production-ready UI for the existing React app |
| CLI tooling | Medium | 1 week | `workflow init`, `workflow deploy`, `workflow logs` |
| Terraform provider | Low | 2 weeks | Infrastructure-as-code for customer deployments |
| Customer onboarding flow | Low | 1 week | Guided setup, template gallery |
| Marketplace (module store) | Low | 3-4 weeks | Community-contributed module types |

### Scaling Roadmap

```
Phase 1 (Now): Single-instance, SQLite
  └─ Suitable for development, demos, small deployments

Phase 2 (Next): PostgreSQL + distributed locking
  └─ 2-4 instances, suitable for medium SaaS
  └─ Effort: 2-3 weeks

Phase 3: NATS/Kafka messaging + Redis caching
  └─ 10+ instances, suitable for large SaaS
  └─ Effort: 2-3 weeks (on top of Phase 2)

Phase 4: Kubernetes operator + auto-scaling
  └─ Elastic scaling, suitable for enterprise
  └─ Effort: 3-4 weeks (on top of Phase 3)
```

---

## Key Takeaways

1. **Config changes today require a brief restart** (~200-500ms). Dynamic component
   changes are hot-reloaded with zero downtime. For zero-downtime config changes,
   use rolling deploys behind a load balancer.

2. **Vertical scaling is the easiest win**. A single Go process on a 4-core machine
   handles 500-2000 req/sec. For most applications, this is sufficient.

3. **Horizontal scaling requires three changes**: PostgreSQL for state persistence,
   a distributed message broker (NATS/Kafka), and distributed locking for the state
   machine. The engine architecture supports all three -- the module interfaces are
   already abstracted correctly.

4. **The SaaS infrastructure is largely built**. Multi-tenant API, RBAC, versioning,
   execution tracking, audit trail, and OAuth2 are all implemented. The main missing
   pieces are the deployment agent, billing integration, and distributed state.

5. **The open-source + commercial model is natural**. The engine is the open-source
   core (anyone can self-host). The platform (web console, deployment, billing,
   support) is the commercial offering. This avoids lock-in while capturing value.
