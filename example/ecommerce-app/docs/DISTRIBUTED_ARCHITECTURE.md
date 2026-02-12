# Distributed E-Commerce Architecture Guide

This document describes the multi-container distributed architecture of the Workflow E-Commerce example application, including how the system works, user personas, and exploratory testing results.

## Architecture Overview

The application is split into 3 containers communicating via Kafka events, plus observability infrastructure:

```mermaid
flowchart LR
    Browser([Browser])

    subgraph Gateway["gateway :8080"]
        SPA[SPA Static Files]
        Proxy[Reverse Proxy]
    end

    subgraph UP["users-products :8080"]
        Auth[JWT Auth]
        Products[Product Catalog]
        UsersDB[(SQLite)]
    end

    subgraph Orders["orders :8080"]
        OrdersAPI[Orders API]
        SM[State Machine]
        DC[Dynamic Components]
        OrdersDB[(SQLite)]
    end

    Kafka{{Apache Kafka}}
    Prom[Prometheus :9090]
    Graf[Grafana :3000]

    Browser --> Gateway
    Proxy -->|"/api/auth, /api/products"| UP
    Proxy -->|"/api/orders"| Orders
    SM <-->|order events| Kafka
    Prom -.->|scrape| Gateway
    Prom -.->|scrape| UP
    Prom -.->|scrape| Orders
    Graf -.->|query| Prom
```

### Container Responsibilities

| Container | Port | Config | Purpose |
|-----------|------|--------|---------|
| **gateway** | 8080 (exposed) | `configs/gateway.yaml` | Reverse proxy + SPA serving. No business logic. |
| **users-products** | 8080 (internal) | `configs/users-products.yaml` | JWT auth, user CRUD, product catalog (SQLite) |
| **orders** | 8080 (internal) | `configs/orders.yaml` | Order CRUD, state machine, processing pipeline (SQLite + Kafka) |
| **kafka** | 9092 | Apache Kafka KRaft | Event bus for order lifecycle events |
| **prometheus** | 9090 (exposed) | `observability/prometheus.yml` | Metrics collection from all services |
| **grafana** | 3000 (exposed) | `observability/grafana/` | Metrics visualization dashboards |

### Communication Patterns

- **Synchronous**: HTTP requests flow through the gateway's reverse proxy (`http.simple_proxy` module) to backend services
- **Asynchronous**: Order lifecycle events flow through Kafka topics (`order.created`, `order.validated`, `order.paid`, `order.shipped`, `order.delivered`, `order.cancelled`)
- **Choreography**: No central orchestrator. Each service reacts to events independently. The orders container processes its own state machine internally.

### Shared Configuration

- **JWT Secret**: Both `users-products` and `orders` share the same `JWT_SECRET` environment variable for token validation
- **Same Binary**: All containers use the same Go binary, loaded with different YAML configs via the `-config` flag

---

## Running the Distributed Setup

```bash
cd example/ecommerce-app

# Start all distributed services
docker compose up --build -d

# Check health
docker compose ps

# View logs
docker compose logs -f orders

# List Kafka topics
docker compose exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list

# Stop everything
docker compose down -v
```

### Monolith Mode (Backwards Compatible)

The original single-container setup still works:

```bash
docker compose --profile monolith up --build -d
```

---

## User Personas & Interaction Flows

### 1. Customer (End User)

**Goal**: Browse products, create an account, place orders, track delivery.

**Flow**: Catalog -> Product Detail -> Cart -> Checkout -> Order Tracking

#### Step-by-Step Walkthrough

**Browse the Product Catalog** (unauthenticated)

The landing page displays all products from the catalog. Products are loaded through the gateway proxy from the `users-products` container. Each product card shows the name, category, price, and stock count.

![Product Catalog](screenshots/01-product-catalog.png)

**View Product Details**

Clicking a product opens the detail page with full description, stock availability, and an "Add to Cart" button. The product data is served by the `products-api` module in the `users-products` container.

![Product Detail](screenshots/06-product-detail.png)

**Register an Account**

New users create an account with name, email, and password. Registration is handled by the `auth.jwt` module in the `users-products` container, which stores user data in SQLite and returns a JWT token.

![Registration Form](screenshots/04-register-filled.png)

After registration, the user is redirected to the catalog with authenticated navigation (Orders, Profile links appear).

![Authenticated Catalog](screenshots/05-registered-catalog.png)

**Add Items to Cart & Checkout**

The cart is managed client-side in the SPA. Users can adjust quantities and see the total.

![Shopping Cart](screenshots/07-cart.png)

At checkout, users enter shipping information. The order summary shows all items and total.

![Checkout](screenshots/08-checkout.png)

**Order Placed - Pipeline Begins**

Clicking "Place Order" sends a POST to `/api/orders` through the gateway to the orders container. The order is created and the state machine triggers `start_validation`.

![Order Placed](screenshots/09-order-placed-new.png)

The Processing Pipeline visualization shows 8 stages:
1. **Created** - Order received
2. **Checking Inventory** - `inventory-checker` dynamic component runs
3. **Inventory OK** - Stock confirmed and reserved
4. **Processing Payment** - `payment-processor` dynamic component runs
5. **Payment OK** - Payment approved
6. **Shipping** - `shipping-service` dynamic component runs
7. **Shipped** - Shipping label generated, tracking number assigned
8. **Delivered** - Order delivered (auto-transition)

**Order Delivered - Pipeline Complete**

The entire pipeline completes in under 1 second via Kafka events and dynamic processing components. The order detail page shows all checkmarks completed and full processing details.

![Order Delivered](screenshots/11-order-delivered.png)

Processing details include:
- Transaction ID (from payment processor)
- Card ending (simulated)
- Tracking number (from shipping service)
- Carrier (USPS)
- Estimated delivery date

**View Order History**

The orders list page shows all orders with their current state.

![Orders List](screenshots/10-orders-list-delivered.png)

**Manage Profile**

Users can view and update their profile information, or sign out.

![User Profile](screenshots/12-profile.png)

#### Request Path (Technical)

```mermaid
sequenceDiagram
    participant B as Browser
    participant G as Gateway
    participant O as Orders
    participant K as Kafka

    B->>G: POST /api/orders
    G->>O: proxy request
    O->>O: auth-middleware (validate JWT)
    O->>O: create order + workflow instance
    O->>O: start_validation → inventory-checker
    O->>O: validation_passed → validated
    O->>K: publish order.validated
    K->>O: event trigger → start_payment
    O->>O: payment-processor runs
    O->>O: payment_approved → paid
    O->>K: publish order.paid
    K->>O: event trigger → start_shipping
    O->>O: shipping-service runs
    O->>O: shipping_confirmed → shipped
    O->>O: deliver_order → delivered (auto)
    O->>K: publish order.delivered
    O-->>G: 201 Created
    G-->>B: order response
```

---

### 2. Platform Operator / SRE

**Goal**: Monitor system health, track performance, investigate issues.

**Tools**: Prometheus (http://localhost:9090), Grafana (http://localhost:3000)

#### Prometheus Targets

All 3 distributed services report as UP targets with sub-millisecond scrape durations.

![Prometheus Targets](screenshots/13-prometheus-targets.png)

| Target | Status | Scrape Duration |
|--------|--------|----------------|
| gateway:8080 | UP | ~0.5ms |
| orders:8080 | UP | ~0.6ms |
| users-products:8080 | UP | ~0.5ms |
| store:8080 (monolith) | DOWN | expected when running distributed |

#### Prometheus Queries

The `up` metric confirms service health across the distributed cluster.

![Prometheus Up Query](screenshots/15-prometheus-up-query.png)

Useful PromQL queries:
- `up` - Service health
- `ecommerce_orders_module_operation_duration_seconds_bucket` - Order processing latency
- `ecommerce_gateway_http_request_duration_seconds_count` - Gateway request rate
- `ecommerce_users_products_http_request_duration_seconds_count` - Auth/product request rate

#### Grafana Dashboard

The pre-provisioned "Workflow E-Commerce Store" dashboard shows:
- **Workflow Duration (p95)**: Processing step latency for validate, payment, and shipping
- **Module Operations**: Success/failure rates per processing step
- **Workflow Execution Rate**: Order throughput over time
- **Active Workflows**: Currently processing orders

![Grafana Dashboard](screenshots/14-grafana-dashboard.png)

#### Kafka Topic Monitoring

```bash
# List all topics
docker compose exec kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list

# Output:
# order.cancelled
# order.created
# order.delivered
# order.paid
# order.shipped
# order.validated
```

---

### 3. Workflow Designer / Platform Engineer

**Goal**: Design and modify workflow configurations, add new processing steps, create new service containers.

**Tools**: Workflow Editor UI (http://localhost:5173), YAML config files

#### Workflow Editor UI

The visual workflow editor provides a ReactFlow-based canvas for designing workflow configurations. Key features:

- **Import/Export YAML**: Load existing configs and export modified ones
- **AI Copilot**: AI-assisted component generation
- **Components Browser**: View and manage dynamic processing components
- **Container View**: Visualize module groupings by container
- **Auto-group**: Automatically organize modules by dependency
- **Validate**: Check config correctness before deployment
- **Undo/Redo**: Full edit history

![Workflow Editor](screenshots/16-workflow-editor-empty.png)

When a YAML config is imported, the editor loads all module definitions (22 modules for the orders config) and enables editing capabilities.

![Editor with Imported Config](screenshots/17-workflow-editor-orders-imported.png)

#### YAML Configuration Structure

Each container has its own YAML config defining:

1. **Modules**: Named components with type, config, and dependency declarations
2. **Workflows**: HTTP routes, state machine definitions, hooks
3. **Triggers**: Event-driven workflow activation (HTTP + Kafka events)

Example: Adding a new processing step to the orders pipeline:

```yaml
# 1. Add the dynamic component
- name: fraud-checker
  type: dynamic.component
  config:
    source: "../components/fraud_checker.go"
    description: "Checks order for fraud signals"
  dependsOn:
    - order-state

# 2. Add the processing step
- name: step-fraud
  type: processing.step
  config:
    componentId: "fraud-checker"
    successTransition: "fraud_cleared"
    compensateTransition: "fraud_detected"
    maxRetries: 1
    timeoutSeconds: 15
  dependsOn:
    - fraud-checker
    - order-state

# 3. Add states and transitions to the state machine
# 4. Add a hook to trigger the step
```

#### Extending the Architecture

To add a new container (e.g., a notifications service):

1. Create `configs/notifications.yaml` with the required modules
2. Add the service to `docker-compose.yml`
3. Add Kafka subscriptions for events to react to
4. Update the gateway proxy targets if HTTP endpoints are needed
5. Add Prometheus scrape config for metrics

---

### 4. E-Commerce Admin (Future Persona)

**Goal**: Manage products, view all orders, handle refunds, update inventory.

**Current Status**: Not yet implemented. The current system supports customer-facing flows. Admin capabilities would require:

- Admin role in JWT claims
- Admin-only API endpoints (PUT/DELETE products, GET all orders across users)
- Admin dashboard in the SPA
- Potentially a separate admin container with elevated permissions

---

## Order State Machine

The order processing pipeline is defined as a state machine with 12 states and 15 transitions:

```mermaid
stateDiagram-v2
    [*] --> new
    new --> validating : start_validation
    new --> cancelled : cancel_order

    validating --> validated : validation_passed
    validating --> failed : validation_failed

    validated --> paying : start_payment (auto)
    validated --> cancelled : cancel_validated

    paying --> paid : payment_approved
    paying --> payment_failed : payment_declined

    payment_failed --> paying : retry_payment

    paid --> shipping : start_shipping (auto)

    shipping --> shipped : shipping_confirmed
    shipping --> ship_failed : shipping_failed

    ship_failed --> shipping : retry_shipping

    shipped --> delivered : deliver_order (auto)

    delivered --> [*]
    cancelled --> [*]
    failed --> [*]
```

### State Descriptions

| State | Description | Final? |
|-------|-------------|--------|
| new | Order created, awaiting validation | No |
| validating | Checking inventory availability | No |
| validated | Inventory confirmed, ready for payment | No |
| paying | Processing payment | No |
| paid | Payment confirmed, ready for shipping | No |
| payment_failed | Payment declined, may retry | No |
| shipping | Generating shipping label | No |
| shipped | Order shipped, in transit | No |
| ship_failed | Shipping failed, may retry | No |
| delivered | Order delivered successfully | Yes |
| cancelled | Order cancelled | Yes |
| failed | Order processing failed permanently | Yes (error) |

### Auto-Transitions

Three transitions are marked `autoTransform: true`, meaning they fire automatically when the preceding state is reached:
- `start_payment` (validated -> paying)
- `start_shipping` (paid -> shipping)
- `deliver_order` (shipped -> delivered)

---

## Kafka Event Topics

| Topic | Published When | Consumed By |
|-------|---------------|-------------|
| `order.created` | Order created via API | notification-handler |
| `order.validated` | Inventory check passed | event trigger -> start_payment |
| `order.paid` | Payment approved | event trigger -> start_shipping |
| `order.shipped` | Shipping label generated | notification-handler |
| `order.delivered` | Order delivered | notification-handler |
| `order.cancelled` | Order cancelled | notification-handler |

---

## Dynamic Processing Components

Four Go components are loaded at runtime via the Yaegi interpreter:

| Component | File | Purpose |
|-----------|------|---------|
| inventory-checker | `components/inventory_checker.go` | Simulates warehouse inventory check |
| payment-processor | `components/payment_processor.go` | Simulates payment gateway (generates txn ID) |
| shipping-service | `components/shipping_service.go` | Simulates shipping label generation (tracking #) |
| notification-sender | `components/notification_sender.go` | Simulates email/SMS notifications |

These components run in a sandboxed stdlib-only environment and can be hot-reloaded without restarting the container.

---

## API Endpoints Reference

All endpoints are accessed through the gateway at `http://localhost:8080`.

### Authentication (proxied to users-products)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/register` | No | Register a new user. Body: `{"name":"...","email":"...","password":"..."}` |
| `POST` | `/api/auth/login` | No | Login. Body: `{"email":"...","password":"..."}`. Returns `{"token":"...","user":{...}}` |
| `GET` | `/api/auth/profile` | Bearer | Get current user profile |
| `PUT` | `/api/auth/profile` | Bearer | Update profile. Body: `{"name":"...","email":"..."}` |

### Products (proxied to users-products)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/products` | No | List all products |
| `GET` | `/api/products/{id}` | No | Get product by ID (e.g., `prod-001`) |

### Orders (proxied to orders)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/orders` | Bearer | Place a new order. Body: `{"items":[...],"shipping":{...}}` |
| `GET` | `/api/orders` | Bearer | List orders for current user |
| `GET` | `/api/orders/{id}` | Bearer | Get order detail with pipeline status |

### Health & Metrics (per-container)

| Method | Path | Container | Description |
|--------|------|-----------|-------------|
| `GET` | `/healthz` | All | Health check (used by Docker healthcheck) |
| `GET` | `/metrics` | All | Prometheus metrics endpoint |

### Quick Test with curl

```bash
# Register
curl -s http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test User","email":"test@example.com","password":"password123"}'

# Login (save token)
TOKEN=$(curl -s http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@example.com","password":"password123"}' | jq -r .token)

# Browse products
curl -s http://localhost:8080/api/products | jq

# Place an order
curl -s http://localhost:8080/api/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "items":[{"productId":"prod-001","name":"Mechanical Keyboard","price":149.99,"quantity":1}],
    "shipping":{"address":"123 Main St","city":"Portland","state":"OR","zip":"97201"}
  }'

# Check order status (wait 1s for pipeline to complete)
sleep 1
curl -s http://localhost:8080/api/orders \
  -H "Authorization: Bearer $TOKEN" | jq '.[0].state'
# Expected: "delivered"
```

---

## Testing Results Summary

All distributed system tests passed successfully:

- **Container Health**: All 6 containers start and report healthy
- **Gateway Proxy**: SPA served correctly, API requests routed to correct backends
- **User Registration**: Account created via users-products container, JWT issued
- **Product Catalog**: 8 products loaded from seed data, served via gateway
- **Order Placement**: Order created through full pipeline in <1 second
- **State Machine**: All 8 pipeline stages completed (new -> delivered)
- **Kafka Events**: All 6 topics created and messages flowing
- **Prometheus Metrics**: All 3 services scraped successfully
- **Grafana Dashboard**: Pre-provisioned dashboard showing processing metrics
- **Persistence**: SQLite databases created in Docker volumes (users-data, orders-data)
