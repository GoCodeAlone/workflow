---
status: implemented
area: runtime
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 8adfcac
  - repo: workflow
    commit: d13fca0
  - repo: workflow
    commit: ebec203
  - repo: workflow
    commit: baf2626
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"actor.system|step.actor_send|step.actor_ask\" plugins/actors cmd examples"
    - "GOWORK=off go test ./plugins/actors"
  result: pass
supersedes: []
superseded_by: []
---

# Actor Model Integration Design

## Problem

The workflow engine handles request-response workflows well via pipelines (now with fan-out/fan-in and map/reduce). But several classes of problems don't fit the pipeline model:

- **Stateful long-lived entities** — an order, a session, a tenant context that persists across requests and activates on demand
- **Distributed execution** — scaling beyond a single node with automatic actor placement, failover, and state migration
- **Structured fault recovery** — supervisor trees where a crashing component is automatically restarted without affecting siblings
- **Message-driven architectures** — pub-sub, event sourcing, and reactive systems where entities communicate asynchronously

These are the problems the Actor model was designed to solve. Netflix Maestro achieved 100x latency improvement (5s to 50ms) by replacing stateless DB-polling workers with per-workflow actors. Temporal and Dapr both use actor-like patterns as their core workflow runtime.

## Decision

**Approach A — Actors alongside pipelines, with goakt v4 as the runtime.**

Actors and pipelines are complementary paradigms. Pipelines handle sequential/parallel request-response workflows. Actors handle stateful entities, background processing, and distributed coordination. Bridge steps (`step.actor_send`, `step.actor_ask`) connect the two worlds.

This is a non-breaking, additive change. Existing pipelines and modules behave identically.

### Approaches Considered

**Approach B — Actors power pipelines (under the hood).** Replace the pipeline executor with actor-based execution. Each pipeline run becomes a supervisor tree, each step a child actor. Rejected because: (1) sequential pipeline steps aren't naturally "entities that receive messages" — the abstraction is forced; (2) distributing individual steps across nodes means serializing PipelineContext over the wire for every step, with potentially large payloads; (3) hides goakt's power — grains, behavior stacking, pub-sub, consistent-hash routing are never exposed to users.

**Approach C — Full actor-native engine.** Everything is an actor: modules, HTTP server, router, pipeline executor, state machines. Rejected *for now* because: (1) rewrite cost — ~50 module types, ~42 step types, 6 workflow handlers need redesign; (2) pipelines are the right abstraction for most workflows, forcing actor semantics onto simple HTTP-to-DB flows adds complexity without benefit; (3) goakt is a new dependency that should be validated in production before deepening the commitment.

**How Approach A enables a future Approach C** — see "Road to C" section below.

## Why goakt v4

[Tochemey/goakt](https://github.com/Tochemey/goakt) v4 is a production-ready distributed actor framework for Go, inspired by Erlang/OTP and Akka.

Key features that align with our needs:

| Feature | How We Use It |
|---------|---------------|
| `any` message types | Workflow engine already passes `map[string]any` everywhere — no protobuf constraint |
| Grains (virtual actors) | Auto-managed actors identified by key (order ID, tenant ID). Activate on demand, deactivate when idle |
| Supervisor trees | OneForOne / AllForOne strategies with restart/stop/escalate directives and retry limits |
| Clustering | Gossip-based membership (hashicorp/memberlist) + distributed hash table (olric) |
| Discovery providers | Kubernetes, Consul, etcd, NATS, mDNS, static — pluggable via `discovery.Provider` interface |
| Routers | Round-robin, random, fan-out, consistent-hash routing with configurable hash functions |
| Cluster singletons | Exactly-one actor across the cluster (leader election, coordination) |
| Multi-datacenter | DC-aware placement for geo-distributed deployments |
| Passivation strategies | Time-based, message-count-based, or long-lived (never passivate) |
| OTEL metrics | Built-in actor system metrics |
| Scheduling | Cron, interval, and one-shot message scheduling |
| CBOR serialization | Arbitrary Go types over the wire without protobuf |
| Testkit | `testkit.New(t)` for actor unit testing |

## Components

### 1. `actor.system` Module — The Cluster Runtime

Wraps goakt's `ActorSystem`. One per engine instance. Manages clustering, discovery, and remoting.

```yaml
modules:
  - name: my-actors
    type: actor.system
    config:
      # -- Clustering (optional, omit for single-node) --
      cluster:
        discovery: kubernetes        # kubernetes | consul | etcd | nats | mdns | static
        discoveryConfig:             # provider-specific settings
          namespace: default
          labelSelector: "app=my-service"
        remotingHost: "0.0.0.0"
        remotingPort: 9000
        gossipPort: 3322
        peersPort: 3320
        replicaCount: 3
        minimumPeers: 2              # quorum before activating actors
        partitionCount: 271
        roles:                       # this node's roles (for targeted placement)
          - api
          - worker

      # -- Defaults --
      shutdownTimeout: 30s
      defaultRecovery:               # applies to all pools unless overridden
        failureScope: isolated       # isolated (one-for-one) | all-for-one
        action: restart              # restart | stop | escalate
        maxRetries: 5
        retryWindow: 30s

      # -- Observability --
      metrics: true                  # expose actor metrics via OTEL
      tracing: true                  # propagate trace context through messages
```

**Lifecycle:** `Init()` creates the `ActorSystem`. `Start()` joins the cluster (if configured) and waits for quorum. `Stop()` gracefully leaves the cluster and drains in-flight messages.

### 2. `actor.pool` Module — Actor Groups

Defines a group of actors that handle the same type of work. Each actor has its own state and processes messages independently.

```yaml
  - name: order-processors
    type: actor.pool
    config:
      system: my-actors              # references the actor.system module by name

      # -- Actor Lifecycle --
      mode: auto-managed             # auto-managed (grain) | permanent
      idleTimeout: 10m               # auto-managed only: deactivate after idle

      # -- Scaling & Routing --
      poolSize: 10                   # permanent only: fixed number of actors
      routing: sticky                # round-robin | random | broadcast | sticky
      routingKey: "order_id"         # sticky only: which field determines affinity

      # -- Recovery --
      recovery:                      # overrides system default
        failureScope: isolated
        action: restart
        maxRetries: 3
        retryWindow: 10s

      # -- Placement (cluster mode) --
      placement: least-load          # round-robin | random | local | least-load
      targetRoles:                   # only place on nodes with these roles
        - worker
      failover: true                 # relocate to healthy node on failure
```

**Modes:**
- **auto-managed** — maps to goakt Grains. Actors activate when first messaged and deactivate after `idleTimeout`. Identified by a unique key (e.g., order ID). State survives deactivation/reactivation cycles.
- **permanent** — maps to goakt long-lived actors or routers. Fixed `poolSize` actors start with the engine and run until shutdown.

### 3. `step.actor_send` — Fire-and-Forget

Send a message to an actor without waiting for a response. The actor processes it asynchronously.

```yaml
- type: step.actor_send
  name: notify-order
  config:
    pool: order-processors           # actor.pool module name
    identity: "{{ .body.order_id }}" # unique key (auto-managed actors only)
    message:
      type: OrderPlaced              # message type name (matched in actor receive handler)
      payload:                       # data to send
        order_id: "{{ .body.order_id }}"
        items: "{{ json .body.items }}"
        total: "{{ .body.total }}"
```

**Outputs:** `{ "delivered": true }`

Maps to goakt `actor.Tell()`.

### 4. `step.actor_ask` — Request-Response

Send a message and wait for a response. The actor's reply becomes this step's output.

```yaml
- type: step.actor_ask
  name: get-order-status
  config:
    pool: order-processors
    identity: "{{ .path.order_id }}"
    timeout: 5s
    message:
      type: GetStatus
      payload:
        order_id: "{{ .path.order_id }}"
```

**Outputs:** Whatever the actor's message handler returns — the final step output of the `receive` pipeline for that message type.

Maps to goakt `actor.Ask()`.

### 5. Actor Workflow Handler — Message Receive Pipelines

Defines what actors DO when they receive messages. Each message type maps to a mini-pipeline of steps.

```yaml
workflows:
  actors:
    pools:
      order-processors:              # matches actor.pool module name
        state:                       # initial state schema (for documentation)
          status: "new"
          items: []
          total: 0

        receive:                     # message handlers
          OrderPlaced:
            description: "Process a new order — validate, reserve inventory, confirm"
            steps:
              - type: step.set
                name: init
                config:
                  values:
                    status: "processing"
              - type: step.http_call
                name: reserve-inventory
                config:
                  url: "https://inventory.internal/reserve"
                  method: POST
                  body: "{{ json .message.payload }}"
              - type: step.conditional
                name: check-reserve
                config:
                  field: "{{ .steps.reserve-inventory.status_code }}"
                  routes:
                    "200": confirm
                    "409": out-of-stock
                  default: confirm
              - type: step.set
                name: confirm
                config:
                  values:
                    status: "confirmed"
                    reserved: true
              - type: step.set
                name: out-of-stock
                config:
                  values:
                    status: "backordered"
                    reserved: false

          GetStatus:
            description: "Return the current order status"
            steps:
              - type: step.set
                name: respond
                config:
                  values:
                    order_id: "{{ .message.payload.order_id }}"
                    status: "{{ .state.status }}"
                    items: "{{ json .state.items }}"

          CancelOrder:
            description: "Cancel the order and release inventory"
            steps:
              - type: step.http_call
                name: release
                config:
                  url: "https://inventory.internal/release"
                  method: POST
                  body: "{{ json .state.items }}"
              - type: step.set
                name: update
                config:
                  values:
                    status: "cancelled"
```

**Key design decisions:**

1. **Receive handlers are mini-pipelines** — they reuse the same step types users already know. No new execution model to learn.
2. **`.message`** — the incoming message available as `{{ .message.type }}` and `{{ .message.payload.* }}`.
3. **`.state`** — the actor's persistent state. The final `step.set` values in a handler merge back into the actor's state.
4. **Reply to `step.actor_ask`** — the last step's output becomes the reply sent back to the caller.

**Template context inside actor handlers:**

| Variable | What it contains |
|----------|-----------------|
| `{{ .message.type }}` | Message type name (e.g., "OrderPlaced") |
| `{{ .message.payload.* }}` | Message data from `step.actor_send` / `step.actor_ask` |
| `{{ .state.* }}` | Actor's current persisted state |
| `{{ .steps.*.* }}` | Step outputs within this handler (same as pipelines) |
| `{{ .actor.identity }}` | Actor's unique key (e.g., "order-123") |
| `{{ .actor.pool }}` | Pool name |

## End-to-End Example

An HTTP API that routes requests to actor-backed order processing:

```yaml
app:
  name: order-service

modules:
  - name: http
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router

  - name: actors
    type: actor.system
    config:
      shutdownTimeout: 15s

  - name: order-processors
    type: actor.pool
    config:
      system: actors
      mode: auto-managed
      idleTimeout: 10m
      routing: sticky
      routingKey: order_id
      recovery:
        action: restart
        maxRetries: 3

workflows:
  actors:
    pools:
      order-processors:
        receive:
          ProcessOrder:
            description: "Validate and confirm an order"
            steps:
              - type: step.set
                name: result
                config:
                  values:
                    status: "confirmed"
                    order_id: "{{ .message.payload.order_id }}"

          GetStatus:
            description: "Return current order state"
            steps:
              - type: step.set
                name: result
                config:
                  values:
                    status: "{{ .state.status }}"

  http:
    routes:
      - path: /orders
        method: POST
        pipeline:
          steps:
            - type: step.request_parse
              name: parse
              config:
                parse_body: true
            - type: step.actor_ask
              name: process
              config:
                pool: order-processors
                identity: "{{ .body.order_id }}"
                timeout: 10s
                message:
                  type: ProcessOrder
                  payload:
                    order_id: "{{ .body.order_id }}"
                    items: "{{ json .body.items }}"
            - type: step.json_response
              name: respond
              config:
                status_code: 201
                body: '{{ json .steps.process }}'

      - path: /orders/{id}
        method: GET
        pipeline:
          steps:
            - type: step.actor_ask
              name: status
              config:
                pool: order-processors
                identity: "{{ .id }}"
                timeout: 5s
                message:
                  type: GetStatus
            - type: step.json_response
              name: respond
              config:
                body: '{{ json .steps.status }}'
```

## Deployment

### Single-Node (Development)

Omit the `cluster` block from `actor.system`. goakt runs in local mode — all actors in-process.

### Multi-Node (Kubernetes)

```yaml
modules:
  - name: actors
    type: actor.system
    config:
      cluster:
        discovery: kubernetes
        discoveryConfig:
          namespace: default
          labelSelector: "app=order-service"
        remotingHost: "0.0.0.0"
        remotingPort: 9000
        gossipPort: 3322
        peersPort: 3320
        replicaCount: 3
        minimumPeers: 2
```

**Required K8s env vars** (goakt expectations):

```yaml
env:
  - name: NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
  - name: NODE_IP
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
  - name: GOSSIP_PORT
    value: "3322"
  - name: CLUSTER_PORT
    value: "3320"
  - name: REMOTING_PORT
    value: "9000"
```

**Required ports:**

| Port | Protocol | Purpose |
|------|----------|---------|
| 8080 | TCP | HTTP (workflow engine) |
| 9000 | TCP | Actor remoting (goakt TCP frames) |
| 3320 | TCP | Cluster peering |
| 3322 | UDP | Gossip (memberlist) |

**Headless Service** required for peer discovery:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: order-service-actors
spec:
  clusterIP: None
  selector:
    app: order-service
  ports:
    - name: remoting
      port: 9000
    - name: peers
      port: 3320
    - name: gossip
      port: 3322
      protocol: UDP
```

### Role-Based Topology

For larger deployments, different pods handle different workloads:

```yaml
# API pods
cluster:
  roles: [api]

# Worker pods
cluster:
  roles: [worker]
```

```yaml
# Actor pool targets worker nodes:
- name: order-processors
  type: actor.pool
  config:
    placement: least-load
    targetRoles: [worker]
```

### wfctl Integration

`wfctl deploy` should detect actor modules and:
- Add headless Service to generated K8s manifests
- Include required env vars in Deployment specs
- Open remoting/gossip/peers ports
- Generate readinessProbe that waits for cluster quorum

## Naming & Documentation Strategy

### Terminology

Actor model jargon is used in type names (correct domain terminology, searchable) but all descriptions are self-explanatory in plain English.

| Actor Jargon | Schema Label | User-Facing Description |
|---|---|---|
| Actor System | Actor Cluster | Distributed runtime that coordinates stateful services across nodes |
| Grain | Auto-Managed Actor | Activates on first message, deactivates after idle timeout. Identified by unique key |
| Long-lived actor | Permanent Actor | Starts with the engine, runs until shutdown |
| Passivation | Idle Timeout | How long an actor stays in memory without messages before deactivating |
| Supervision | Recovery Policy | What happens when an actor crashes |
| Supervision strategy | Failure Scope | Whether a crash affects only the failing actor or all actors in the pool |
| Directive | Recovery Action | restart / stop / escalate |
| Tell | Send (fire-and-forget) | Send a message without waiting for a response |
| Ask | Request (wait for reply) | Send a message and block until the actor responds or timeout |
| Router | Load Balancing | How messages are distributed across actors in a pool |
| Consistent hash | Sticky Routing | Messages with the same key always go to the same actor |
| Placement | Node Selection | Which cluster node an actor runs on |
| Relocation | Failover | Actor moves to healthy node when its node fails |

### Three Documentation Surfaces

All documentation flows from `ModuleSchema` and `StepSchema` registries — one source of truth.

**MCP (AI assistants):**
- `get_module_schema("actor.system")` → label, description, all config fields with descriptions, example YAML
- `get_step_schema("step.actor_ask")` → description, config fields, output schema, example
- `get_config_examples("actor-system-config")` → complete working example
- New MCP resource `workflow://docs/actors` → concept guide

**LSP/IDE (developers writing YAML):**
- Hover `type: actor.pool` → label + description
- Config key completions with field descriptions
- Enum fields show all options with descriptions
- Diagnostics for missing required fields

**Workflow UI (visual builder):**
- "Actor" category in module palette
- Config panels with descriptions, dropdowns for enums, help icons
- "Start from template" using example YAML

### Schema Example

```go
&schema.ModuleSchema{
    Type:        "actor.pool",
    Label:       "Actor Pool",
    Category:    "actor",
    Description: "Defines a group of actors that handle the same type of work. " +
        "Each actor has its own state and processes messages one at a time, " +
        "eliminating concurrency bugs. Use 'auto-managed' mode for actors " +
        "identified by a unique key (e.g. one per order) that activate on " +
        "demand. Use 'permanent' for a fixed pool of always-running workers.",
    ConfigFields: []schema.ConfigField{
        {
            Key:         "system",
            Label:       "Actor Cluster",
            Type:        "string",
            Description: "Name of the actor.system module this pool belongs to",
            Required:    true,
        },
        {
            Key:         "mode",
            Label:       "Lifecycle Mode",
            Type:        "enum",
            Description: "How actors are created and destroyed. " +
                "'auto-managed': activates on first message, deactivates after " +
                "idle timeout. Identified by a unique key. " +
                "'permanent': fixed pool that runs from engine start to shutdown.",
            Options:     []string{"auto-managed", "permanent"},
            Default:     "auto-managed",
        },
        // ...
    },
}
```

## Road to C — The Actor-Native Engine

### Why Not Now

1. **Rewrite cost vs. incremental value.** The engine has ~50 module types, ~42 step types, 6 workflow handlers, and a mature plugin ecosystem. Rewriting every component as an actor is months of work with high regression risk. Approach A delivers all four values (distribution, stateful entities, supervision, new paradigm) as an additive feature.

2. **Pipelines are the right abstraction for most workflows.** The sequential pipeline model (enhanced with `step.parallel` and concurrent `step.foreach`) is intuitive and sufficient for request-response patterns. Forcing actor semantics onto simple HTTP-to-DB pipelines adds complexity without benefit.

3. **goakt integration risk.** This is a new dependency. Building the entire engine on it before validating in production would be irresponsible. Approach A lets us validate goakt's clustering, grain lifecycle, and supervisor behavior in real deployments.

4. **The actor plugin IS the foundation for C.** The `actor.system` module wraps goakt's `ActorSystem`. The `actor` workflow handler defines message-driven behaviors. If we later decide to run HTTP routing, state machines, or pipeline execution through actors, we already have the runtime.

### What Approach C Looks Like

A future v2.0 where actors are the universal execution primitive:

```
Engine v2.0 (Approach C)
+-- ActorSystem (goakt) --- the runtime for everything
    +-- HTTPListenerActor --- replaces http.server module
    |   +-- RouterActor --- replaces http.router module
    |       +-- RequestActor (per request) --- replaces per-request goroutine
    |           +-- PipelineActor --- replaces pipeline executor
    |               +-- StepActor("step-1")
    |               +-- ParallelSupervisorActor
    |               |   +-- StepActor("branch-a")
    |               |   +-- StepActor("branch-b")
    |               +-- StepActor("step-3")
    +-- StateMachineActor --- replaces state_machine module
    +-- SchedulerActor --- replaces scheduler trigger
    +-- MessagingActor --- replaces messaging.broker
    +-- UserDefinedActors --- same as Approach A
```

**What C unlocks that A doesn't:**
- **Distributed pipeline execution** — individual steps can run on different nodes via actor placement. Useful for compute-heavy steps (ML inference, video processing).
- **Unified supervision** — HTTP server, router, and pipeline executor all get supervisor trees. A crashing step doesn't kill the HTTP listener.
- **Unified observability** — every operation is an actor message; a single tracing system captures everything.
- **Hot code reload** — actor behavior stacking could enable swapping step implementations without restart.

**Prerequisites before C makes sense:**
1. goakt running in production for 6+ months via Approach A
2. Concrete cases identified where internal components benefit from actor semantics
3. YAML config syntax is stable and users are comfortable with actor concepts
4. Performance benchmarks show actor overhead is acceptable for the hot path

### How A Sets Up C

- `actor.system` module lifecycle (Init/Start/Stop) establishes how goakt integrates with the modular framework
- `actor` workflow handler proves that step pipelines work inside actor message handlers
- Schema and documentation infrastructure handles actor-specific help text
- goakt's `ActorSystem` is already a singleton managed by the engine — C just routes more traffic through it

## Implementation Packaging

Built-in engine plugin (like HTTP or platform plugins), NOT an external gRPC plugin. Reasons:

- Actor systems need in-process access to the step registry, service registry, and pipeline executor
- The gRPC boundary would prevent actors from executing step pipelines directly
- Performance — no serialization overhead for actor-to-step communication
- Supervisor trees need to manage step execution lifecycle directly

The plugin structure:

```
plugins/
  actors/
    plugin.go          # EnginePlugin implementation
    modules.go         # actor.system and actor.pool module factories
    steps.go           # step.actor_send and step.actor_ask factories
    handler.go         # actor workflow handler
    actor_bridge.go    # goakt Actor implementation that runs step pipelines
    schemas.go         # ModuleSchemas and StepSchemas with descriptions
```

## goakt v4 Integration Details

### Key APIs Used

| goakt API | Our Usage |
|-----------|-----------|
| `actor.NewActorSystem()` | `actor.system` module Init |
| `system.Start(ctx)` | `actor.system` module Start (joins cluster) |
| `system.Stop(ctx)` | `actor.system` module Stop (leaves cluster) |
| `system.Spawn()` | permanent pool actors |
| `system.SpawnRouter()` | permanent pool with routing |
| `ClusterConfig.WithGrains()` | register auto-managed actor kinds |
| `system.GrainIdentity()` | activate/get auto-managed actor by key |
| `actor.Tell()` | `step.actor_send` |
| `actor.Ask()` | `step.actor_ask` |
| `supervisor.NewSupervisor()` | recovery policy per pool |
| `actor.WithPassivationStrategy()` | idleTimeout mapping |
| `actor.WithDependencies()` | inject StepRegistry, PipelineExecutor into actors |
| `remote.NewConfig()` | cluster remoting setup |
| `discovery/kubernetes` | K8s cluster discovery |

### The Bridge Actor

The core integration piece — a goakt `Actor` implementation that receives messages and executes step pipelines:

```go
type BridgeActor struct {
    pool       string
    state      map[string]any
    handlers   map[string][]config.StepConfig  // message type -> step pipeline
    registry   func() *module.StepRegistry
    executor   *module.PipelineExecutor
}

func (a *BridgeActor) PreStart(ctx *actor.Context) error {
    // Initialize state from config defaults
    return nil
}

func (a *BridgeActor) Receive(ctx *actor.ReceiveContext) {
    switch msg := ctx.Message().(type) {
    case *actor.PostStart:
        // Actor activated
    case *ActorMessage:
        steps := a.handlers[msg.Type]
        // Build PipelineContext with .message, .state, .actor
        // Execute step pipeline
        // Merge final step.set values back into a.state
        // If Ask: ctx.Response(result)
    case *actor.PoisonPill:
        ctx.Stop(ctx.Self())
    }
}

func (a *BridgeActor) PostStop(ctx *actor.Context) error {
    // Persist state if needed
    return nil
}
```

### Serialization

goakt v4 uses `any` messages with pluggable serialization. For cluster mode, we use CBOR (supports arbitrary Go types without protobuf):

```go
remote.WithSerializers(
    &ActorMessage{},
    remote.NewCBORSerializer(),
)
```

`ActorMessage` is a simple struct:

```go
type ActorMessage struct {
    Type    string         `cbor:"type"`
    Payload map[string]any `cbor:"payload"`
}
```

This aligns with the engine's `map[string]any` convention.

## Non-Goals (For Now)

- **Actor persistence / event sourcing** — actors start with default state. Persistence can be added later via goakt extensions.
- **Actor-to-actor messaging in YAML** — actors only receive messages from pipelines (`step.actor_send/ask`) or external triggers. Direct actor-to-actor communication is a future enhancement.
- **Nested actor hierarchies in config** — pools are flat. Supervisor trees handle fault recovery but aren't user-configurable beyond pool-level recovery policy.
- **Custom serializers** — CBOR for cluster, in-process `any` for local. No user-pluggable serialization.
