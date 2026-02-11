# Multi-Workflow E-Commerce Example

This example demonstrates three interconnected workflows that cooperate to handle
e-commerce order processing via cross-workflow event routing.

## Architecture

```
  Customer
     |
     v
+-----------------------+     order.validated     +---------------------------+
| Workflow A            | ----------------------> | Workflow B                |
| Order Ingestion       |                         | Fulfillment Processing    |
|                       |                         |                           |
| POST /api/orders      |                         | Triggered by event from A |
| Validates & persists  |                         | Processes fulfillment     |
| Emits order.validated |     order.failed        | Emits fulfillment.*       |
| Emits order.failed    | ----+                   |                           |
+-----------------------+     |                   +---------------------------+
                              |                              |
                              v                              | fulfillment.*
                    +---------------------------+            |
                    | Workflow C                | <-----------+
                    | Notification Hub          |
                    |                           |
                    | Triggered by events from  |
                    | both A and B              |
                    | Sends notifications via   |
                    | webhook                   |
                    +---------------------------+
```

## Workflows

### Workflow A: Order Ingestion (`workflow-a-orders.yaml`)

Receives HTTP orders, validates them, tracks state via a state machine, and
emits events for downstream processing.

- **Entry point**: `POST /api/orders`
- **Modules**: HTTP server/router/handler, data transformer, state machine,
  state tracker, messaging broker, metrics, health check
- **State machine**: `new -> validated -> processing -> completed | failed`
- **Events emitted**: `order.validated`, `order.failed`

### Workflow B: Fulfillment Processing (`workflow-b-fulfillment.yaml`)

Triggered by `order.validated` events from Workflow A. Manages the fulfillment
lifecycle and emits completion events.

- **Entry point**: Messaging subscription on `order.validated`
- **Modules**: Messaging broker/handler, data transformer, state machine,
  state tracker, webhook sender, metrics
- **State machine**: `pending -> picking -> packing -> shipped -> delivered | failed`
- **Events emitted**: `fulfillment.started`, `fulfillment.shipped`, `fulfillment.completed`

### Workflow C: Notification Hub (`workflow-c-notifications.yaml`)

Subscribes to events from both Workflow A and Workflow B and sends
notifications via webhook.

- **Entry point**: Messaging subscriptions on `order.*` and `fulfillment.*`
- **Modules**: Messaging broker/handler, webhook sender, metrics
- **Notifications**: Order confirmation, fulfillment updates, failure alerts

## Cross-Workflow Links

The `cross-workflow-links.yaml` file documents the event routing between
workflows. These links are managed via the platform API:

```
POST /api/v1/workflows/{id}/links
```

| Source       | Target         | Event Pattern       | Purpose                        |
|-------------|----------------|---------------------|--------------------------------|
| Workflow A  | Workflow B     | `order.validated`   | Trigger fulfillment processing |
| Workflow B  | Workflow C     | `fulfillment.*`     | Send fulfillment notifications |
| Workflow A  | Workflow C     | `order.failed`      | Send failure alerts            |

## Running

Start each workflow in a separate terminal:

```bash
# Terminal 1: Order Ingestion (port 8080)
go run ./cmd/server -config example/multi-workflow-ecommerce/workflow-a-orders.yaml

# Terminal 2: Fulfillment Processing (no HTTP, event-driven)
go run ./cmd/server -config example/multi-workflow-ecommerce/workflow-b-fulfillment.yaml

# Terminal 3: Notification Hub (no HTTP, event-driven)
go run ./cmd/server -config example/multi-workflow-ecommerce/workflow-c-notifications.yaml
```

Submit a test order:

```bash
curl -X POST http://localhost:8080/api/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "cust-123", "items": [{"sku": "WIDGET-1", "qty": 2}]}'
```
