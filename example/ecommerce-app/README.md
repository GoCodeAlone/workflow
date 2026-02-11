# Workflow Store -- E-Commerce Example Application

A complete e-commerce storefront built entirely from a single YAML configuration
file. No application code was written -- every component is a reusable Workflow
engine module wired together declaratively.

This demonstrates the Workflow engine as a **full application platform**: HTTP
serving, JWT authentication, REST APIs, a state machine for order lifecycle,
event-driven messaging, and a vanilla JS single-page application -- all composed
from 15 modules in one config file.

## Screenshots

| Catalog | Product Detail |
|---------|---------------|
| ![Catalog](docs/screenshot-catalog.png) | ![Product](docs/screenshot-product.png) |

| Cart | Checkout |
|------|----------|
| ![Cart](docs/screenshot-cart.png) | ![Checkout](docs/screenshot-checkout.png) |

| Login | Order Placed |
|-------|-------------|
| ![Login](docs/screenshot-login.png) | ![Order](docs/screenshot-order.png) |

## Quick Start

### Docker Compose (recommended)

```bash
cd workflow
docker compose -f example/ecommerce-app/docker-compose.yml up --build
```

Open [http://localhost:8080](http://localhost:8080) in your browser.

### Bare Metal

```bash
cd workflow

# Build the server
go build -o server ./cmd/server

# Set a JWT signing secret
export JWT_SECRET="my-dev-secret"

# Run with the e-commerce config
./server -config example/ecommerce-app/workflow.yaml
```

Open [http://localhost:8080](http://localhost:8080).

### Try It Out

1. Browse the product catalog (no account required)
2. Click **Login** > **Create one** to register
3. Add products to your cart
4. Proceed to checkout, fill in shipping, and place an order
5. View your order status under **Orders**

---

## Architecture

### System Diagram

```
                           +-----------------+
                           |   Browser SPA   |
                           |  (vanilla JS)   |
                           +--------+--------+
                                    |
                              HTTP :8080
                                    |
                 +------------------+------------------+
                 |            web-server               |
                 |          (http.server)               |
                 +--+-------------------+--+-----------+
                    |                   |  |
          +---------+------+     +------+--+--------+
          | static.fileserver|   |     router       |
          |    (SPA files)   |   |  (http.router)   |
          +------------------+   +------+-----------+
                                        |
                          +-------------+-------------+
                          |             |             |
                     cors + request-id + rate-limiter
                          |             |             |
                +---------+--+  +-------+---+  +-----+-------+
                | /api/auth/* |  |/api/products|  |/api/orders  |
                |  (auth.jwt) |  | (api.handler)|  |(api.handler)|
                +-------------+  +-------------+  +------+------+
                                                         |
                                                  auth-middleware
                                                         |
                                                  +------+------+
                                                  | orders-api  |
                                                  +------+------+
                                                         |
                                                +--------+--------+
                                                | statemachine     |
                                                | engine           |
                                                | (order lifecycle)|
                                                +--------+--------+
                                                         |
                                                +--------+--------+
                                                | messaging.broker |
                                                | (event pub/sub)  |
                                                +--------+--------+
                                                         |
                                                +--------+--------+
                                                | notification-   |
                                                | handler          |
                                                +-----------------+
```

### Module Inventory

All 15 modules are declared in `workflow.yaml`:

| Module | Type | Purpose |
|--------|------|---------|
| `web-server` | `http.server` | Listens on `:8080`, serves all HTTP traffic |
| `router` | `http.router` | Routes `/api/*` requests to handlers |
| `cors` | `http.middleware.cors` | Allows cross-origin requests |
| `request-id` | `http.middleware.requestid` | Adds `X-Request-ID` to every response |
| `rate-limiter` | `http.middleware.ratelimit` | 300 req/min per client (burst 50) |
| `auth` | `auth.jwt` | JWT register/login/profile with bcrypt passwords |
| `auth-middleware` | `http.middleware.auth` | Validates JWT Bearer tokens on protected routes |
| `products-api` | `api.handler` | Product catalog CRUD, seeded from JSON |
| `orders-api` | `api.handler` | Order CRUD, integrated with state machine |
| `order-state` | `statemachine.engine` | Manages order state transitions |
| `event-broker` | `messaging.broker` | In-memory event pub/sub for order events |
| `notification-handler` | `messaging.handler` | Reacts to order lifecycle events |
| `metrics` | `metrics.collector` | Prometheus-compatible metrics |
| `health` | `health.checker` | `/healthz`, `/readyz`, `/livez` probes |
| `spa` | `static.fileserver` | Serves the storefront SPA with fallback routing |

### Request Flow

```
Browser ──> GET /                ──> static.fileserver (SPA HTML/JS/CSS)
Browser ──> GET /api/products    ──> cors ──> request-id ──> rate-limiter ──> products-api
Browser ──> POST /api/auth/login ──> cors ──> request-id ──> rate-limiter ──> auth (JWT)
Browser ──> POST /api/orders     ──> cors ──> request-id ──> rate-limiter ──> auth-middleware ──> orders-api
                                                                                                      │
                                                                                              statemachine.engine
                                                                                                      │
                                                                                              messaging.broker
                                                                                                      │
                                                                                           notification-handler
```

---

## Workflow Configuration Walkthrough

The entire application is defined in [`workflow.yaml`](workflow.yaml). Here's how the key sections work:

### Modules Section

Declares the 15 modules and their configuration. Each module has a `name`, `type`,
optional `config`, and `dependsOn` for ordering:

```yaml
modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"

  - name: auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"    # Environment variable expansion
      tokenExpiry: "24h"
      issuer: "workflow-store"
```

### Workflows Section

Wires modules together with routing rules, middleware chains, and state machine definitions:

```yaml
workflows:
  http:
    router: router
    routes:
      - method: "GET"
        path: "/api/products"
        handler: products-api
        middlewares: [cors, request-id, rate-limiter]    # Public

      - method: "POST"
        path: "/api/orders"
        handler: orders-api
        middlewares: [cors, request-id, rate-limiter, auth-middleware]  # Protected

  statemachine:
    definitions:
      - name: order-processing
        initialState: "new"
        states: { new, validated, shipped, delivered, cancelled, failed }
        transitions: { validate_order, ship_order, deliver_order, cancel_order, fail_order }
```

### Triggers Section

Defines what starts workflows automatically:

```yaml
triggers:
  http:
    routes:
      - path: "/api/orders"
        method: "POST"
        workflow: "order-processing"
  event:
    subscriptions:
      - topic: "order.created"
        workflow: "order-processing"
        action: "validate_order"
```

---

## API Reference

### Public Endpoints (no auth required)

| Method | Path | Description | Example Response |
|--------|------|-------------|-----------------|
| `GET` | `/` | Storefront SPA | HTML page |
| `GET` | `/api/products` | List all products | `[{"id":"prod-001","data":{...},"state":"active"}]` |
| `GET` | `/api/products/{id}` | Get single product | `{"id":"prod-001","data":{...},"state":"active"}` |
| `POST` | `/api/auth/register` | Create account | `{"token":"eyJ...","user":{...}}` |
| `POST` | `/api/auth/login` | Sign in | `{"token":"eyJ...","user":{...}}` |
| `GET` | `/healthz` | Health check | `{"status":"healthy","checks":{}}` |
| `GET` | `/readyz` | Readiness probe | `{"status":"ready"}` |
| `GET` | `/livez` | Liveness probe | `{"status":"alive"}` |

### Protected Endpoints (JWT required)

| Method | Path | Description | Example Response |
|--------|------|-------------|-----------------|
| `GET` | `/api/auth/profile` | Get user profile | `{"id":"1","email":"...","name":"..."}` |
| `PUT` | `/api/auth/profile` | Update user name | `{"id":"1","name":"Updated"}` |
| `POST` | `/api/orders` | Place an order | `{"id":"1","data":{...},"state":"new"}` |
| `GET` | `/api/orders` | List orders | `[{"id":"1","data":{...},"state":"new"}]` |
| `GET` | `/api/orders/{id}` | Get order detail | `{"id":"1","data":{...},"state":"validated"}` |

### Example: Register + Place Order

```bash
# Register
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"secret123","name":"Alice"}' \
  | jq -r '.token')

# Browse products
curl -s http://localhost:8080/api/products | jq '.[0].data.name'

# Place an order
curl -s -X POST http://localhost:8080/api/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "items": [{"productId":"prod-001","quantity":2}],
    "shipping": {"address":"123 Main St","city":"SF","state":"CA","zip":"94102"}
  }' | jq .
```

---

## Order State Machine

Orders progress through a defined lifecycle managed by the `statemachine.engine` module.
Transitions are triggered automatically via event subscriptions.

```
                    ┌─────────────┐
                    │     new     │
                    └──────┬──────┘
                           │ validate_order
                    ┌──────▼──────┐
              ┌─────│  validated  │
              │     └──────┬──────┘
              │            │ ship_order
              │     ┌──────▼──────┐
              │     │   shipped   │
              │     └──────┬──────┘
              │            │ deliver_order
              │     ┌──────▼──────┐
              │     │  delivered  │  (final)
              │     └─────────────┘
              │
     cancel_order / cancel_validated
              │
       ┌──────▼──────┐
       │  cancelled   │  (final)
       └─────────────┘

       ┌─────────────┐
       │   failed     │  (final, error)
       └─────────────┘
         ▲ fail_order (from new)
```

**Transitions:**

| Transition | From | To | Trigger |
|-----------|------|-----|---------|
| `validate_order` | new | validated | Automatic on order creation |
| `ship_order` | validated | shipped | Event: `order.validated` |
| `deliver_order` | shipped | delivered | Manual or event-driven |
| `cancel_order` | new | cancelled | Manual |
| `cancel_validated` | validated | cancelled | Manual |
| `fail_order` | new | failed | On processing error |

---

## SPA (Single Page Application)

The storefront is a vanilla JavaScript SPA with no build step. It's served by the
`static.fileserver` module with SPA fallback routing (unmatched paths serve `index.html`).

### Pages

| Route | Page | Auth Required |
|-------|------|:------------:|
| `#/` | Product catalog grid | No |
| `#/product/{id}` | Product detail | No |
| `#/login` | Sign in form | No |
| `#/register` | Create account form | No |
| `#/cart` | Shopping cart | No |
| `#/checkout` | Shipping form + order summary | Yes |
| `#/orders` | Order history | Yes |
| `#/orders/{id}` | Order detail with status | Yes |
| `#/profile` | View/edit profile, sign out | Yes |

### Tech Stack

- **Routing**: Hash-based (`window.location.hash`)
- **State**: `localStorage` for auth token, user data, and cart
- **Styling**: Catppuccin Mocha dark theme, CSS Grid for responsive layout
- **Modules**: ES module `import`/`export`, no bundler needed
- **Icons**: Inline SVG category icons

### File Structure

```
spa/
├── index.html          # Shell: <div id="app"> + script loader
├── styles.css          # Full Catppuccin Mocha theme (~670 lines)
├── js/
│   ├── app.js          # Hash router, view dispatcher
│   ├── api.js          # Fetch wrapper, auto Bearer token injection
│   ├── auth.js         # Login, register, profile views
│   ├── catalog.js      # Product grid + product detail
│   ├── cart.js         # Cart state (localStorage), quantity controls
│   ├── orders.js       # Checkout, order list, order detail
│   └── components.js   # Header, toast notifications, spinner, helpers
└── img/
    ├── electronics.svg
    ├── accessories.svg
    ├── home-office.svg
    └── software.svg
```

---

## Seed Data

The `seed/products.json` file pre-populates the product catalog with 8 items.
Products are loaded automatically when the `products-api` module starts.

| ID | Product | Category | Price |
|----|---------|----------|-------|
| `prod-001` | Mechanical Keyboard | Electronics | $149.99 |
| `prod-002` | Wireless Mouse | Electronics | $49.99 |
| `prod-003` | USB-C Hub | Electronics | $79.99 |
| `prod-004` | Laptop Stand | Home Office | $89.99 |
| `prod-005` | Desk Mat | Accessories | $34.99 |
| `prod-006` | Cable Management Kit | Accessories | $24.99 |
| `prod-007` | Monitor Light Bar | Home Office | $59.99 |
| `prod-008` | IDE License Pro | Software | $9.99 |

---

## E2E Tests

The `e2e/` directory contains 23 Playwright tests covering all user flows:

```bash
# Run tests (requires server running on :8080)
cd example/ecommerce-app/e2e
npx playwright test --config playwright.config.ts
```

| Suite | Tests | Coverage |
|-------|:-----:|----------|
| `catalog.spec.ts` | 5 | Product grid, cards, detail, category badge, back nav |
| `auth.spec.ts` | 5 | Register, login, invalid password, profile CRUD, logout |
| `cart.spec.ts` | 5 | Add to cart, item display, quantity +/-, remove, empty state |
| `checkout.spec.ts` | 5 | Place order, order history, status, cart cleared, auth guard |
| `e2e-flow.spec.ts` | 3 | Full register-to-order flow, auth redirect, orders guard |

---

## Project Structure

```
example/ecommerce-app/
├── workflow.yaml           # Complete application configuration (15 modules)
├── Dockerfile              # Multi-stage build (golang:1.25 -> alpine:3.19)
├── docker-compose.yml      # One-command deployment
├── README.md               # This file
├── seed/
│   └── products.json       # 8 seed products
├── spa/                    # Vanilla JS storefront (no build step)
│   ├── index.html
│   ├── styles.css
│   ├── js/ (7 modules)
│   └── img/ (4 SVG icons)
├── e2e/                    # Playwright E2E tests (23 tests)
│   ├── playwright.config.ts
│   ├── helpers.ts
│   ├── catalog.spec.ts
│   ├── auth.spec.ts
│   ├── cart.spec.ts
│   ├── checkout.spec.ts
│   └── e2e-flow.spec.ts
└── docs/                   # Screenshots for documentation
    ├── screenshot-catalog.png
    ├── screenshot-product.png
    ├── screenshot-login.png
    ├── screenshot-cart.png
    ├── screenshot-checkout.png
    └── screenshot-order.png
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|:--------:|---------|-------------|
| `JWT_SECRET` | Yes | -- | Secret key for signing JWT tokens |

## What This Demonstrates

This example proves the Workflow engine can build **real applications**, not just pipelines:

- **15 modules** wired together with zero Go code
- **Full auth flow** with bcrypt + JWT
- **REST API** with middleware chains (CORS, rate limiting, request IDs)
- **State machine** managing order lifecycle with automatic transitions
- **Event-driven architecture** with pub/sub messaging
- **Static file serving** with SPA fallback routing
- **Health/readiness probes** for container orchestration
- **23 E2E tests** validating every user flow
