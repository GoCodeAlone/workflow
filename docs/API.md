# Workflow Engine API Reference

## Overview

The workflow engine exposes two HTTP servers:

| Server | Default Address | Purpose |
|--------|----------------|---------|
| **Workflow Engine** | `:8080` | Application endpoints defined by YAML config (health, metrics, REST resources, webhooks, auth) |
| **Management API** | `:8081` | AI services, dynamic components, workflow UI, schema |

Both servers are configured via command-line flags:

```
-addr       Workflow engine listen address (default ":8080")
-mgmt-addr  Management API listen address (default ":8081")
```

### Content Type

All API endpoints accept and return `application/json` unless otherwise noted.

### Authentication

Authenticated endpoints require a JWT bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

Tokens are obtained via `POST /api/auth/login` or `POST /api/auth/register`. JWT claims include `sub` (user ID), `email`, `name`, `role`, `affiliateId`, and `programIds`.

### Error Format

All error responses use the following JSON structure:

```json
{
  "error": "Description of what went wrong"
}
```

---

## Management API Endpoints (port 8081)

### Schema

#### GET /api/schema

Returns the JSON Schema describing a valid workflow configuration YAML file.

| Field | Value |
|-------|-------|
| Auth required | No |
| Content-Type | `application/schema+json` |

**Response** (200 OK):

A JSON Schema document with `$schema`, `title`, `type`, `required`, and `properties` fields describing the `modules`, `workflows`, and `triggers` top-level keys.

```bash
curl http://localhost:8081/api/schema
```

---

### Dynamic Components

Dynamic components are Go source files loaded at runtime via the Yaegi interpreter. Components must use `package component` and only standard-library imports.

#### GET /api/dynamic/components

List all registered dynamic components.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
[
  {
    "id": "my-component",
    "name": "My Component",
    "status": "loaded",
    "loaded_at": "2026-02-13T10:00:00Z"
  }
]
```

**Status codes**: 200 OK

```bash
curl http://localhost:8081/api/dynamic/components
```

---

#### POST /api/dynamic/components

Load a new dynamic component from Go source code.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique identifier for the component |
| `source` | string | Yes | Go source code (must use `package component`) |

```json
{
  "id": "my-transform",
  "source": "package component\n\nfunc Name() string { return \"my-transform\" }\n\nfunc Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {\n  return params, nil\n}"
}
```

**Response** (201 Created):

```json
{
  "id": "my-transform",
  "name": "my-transform",
  "status": "loaded",
  "loaded_at": "2026-02-13T10:00:00Z"
}
```

**Status codes**: 201 Created, 400 Bad Request (missing fields), 422 Unprocessable Entity (compilation error)

```bash
curl -X POST http://localhost:8081/api/dynamic/components \
  -H "Content-Type: application/json" \
  -d '{"id": "my-transform", "source": "package component\n\nfunc Name() string { return \"my-transform\" }"}'
```

---

#### GET /api/dynamic/components/{id}

Get a specific dynamic component including its source code.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "id": "my-transform",
  "name": "my-transform",
  "status": "loaded",
  "loaded_at": "2026-02-13T10:00:00Z",
  "source": "package component\n\nfunc Name() string { return \"my-transform\" }"
}
```

**Status codes**: 200 OK, 404 Not Found

```bash
curl http://localhost:8081/api/dynamic/components/my-transform
```

---

#### PUT /api/dynamic/components/{id}

Replace a component's source code and reload it.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source` | string | Yes | New Go source code |

**Response** (200 OK): Updated `ComponentInfo` object.

**Status codes**: 200 OK, 400 Bad Request, 422 Unprocessable Entity

```bash
curl -X PUT http://localhost:8081/api/dynamic/components/my-transform \
  -H "Content-Type: application/json" \
  -d '{"source": "package component\n\nfunc Name() string { return \"my-transform-v2\" }"}'
```

---

#### DELETE /api/dynamic/components/{id}

Unregister and stop a dynamic component.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response**: 204 No Content

**Status codes**: 204 No Content, 404 Not Found

```bash
curl -X DELETE http://localhost:8081/api/dynamic/components/my-transform
```

---

### AI Service

AI endpoints require at least one AI provider to be configured (Anthropic API key or Copilot CLI path).

#### POST /api/ai/generate

Generate a complete workflow configuration from a natural language description.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `intent` | string | Yes | Natural language description of the desired workflow |
| `context` | object | No | Key-value pairs providing additional context |
| `constraints` | string[] | No | List of requirements or constraints |

```json
{
  "intent": "Create an order processing pipeline with validation and notification",
  "context": {
    "environment": "production"
  },
  "constraints": [
    "Must include rate limiting",
    "Use NATS for messaging"
  ]
}
```

**Response** (200 OK):

```json
{
  "workflow": { "modules": [...], "workflows": {...}, "triggers": {...} },
  "components": [
    {
      "name": "order-validator",
      "type": "processing.step",
      "description": "Validates order data",
      "interface": "modular.Module",
      "goCode": "package component\n..."
    }
  ],
  "explanation": "This workflow sets up..."
}
```

**Status codes**: 200 OK, 400 Bad Request (missing intent), 500 Internal Server Error

```bash
curl -X POST http://localhost:8081/api/ai/generate \
  -H "Content-Type: application/json" \
  -d '{"intent": "Create a REST API with CRUD operations for a product catalog"}'
```

---

#### POST /api/ai/component

Generate Go source code for a single component.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Component name |
| `interface` | string | Yes | Target interface (e.g., `modular.Module`, `MessageHandler`) |
| `type` | string | No | Component type identifier |
| `description` | string | No | What the component should do |

```json
{
  "name": "price-calculator",
  "interface": "modular.Module",
  "type": "processing.step",
  "description": "Calculates total price with tax and discounts"
}
```

**Response** (200 OK):

```json
{
  "code": "package component\n\nimport (\n\t\"context\"\n)\n\nfunc Name() string { ... }\nfunc Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) { ... }"
}
```

**Status codes**: 200 OK, 400 Bad Request (missing name or interface), 500 Internal Server Error

```bash
curl -X POST http://localhost:8081/api/ai/component \
  -H "Content-Type: application/json" \
  -d '{"name": "price-calculator", "interface": "modular.Module", "description": "Calculates total price"}'
```

---

#### POST /api/ai/suggest

Get workflow suggestions for a use case. Results are cached in memory.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `useCase` | string | Yes | Description of the use case |

```json
{
  "useCase": "real-time chat application"
}
```

**Response** (200 OK):

```json
[
  {
    "name": "chat-platform",
    "description": "A real-time chat platform with...",
    "config": { "modules": [...] },
    "confidence": 0.92
  }
]
```

**Status codes**: 200 OK, 400 Bad Request (missing useCase), 500 Internal Server Error

```bash
curl -X POST http://localhost:8081/api/ai/suggest \
  -H "Content-Type: application/json" \
  -d '{"useCase": "real-time chat application"}'
```

---

#### GET /api/ai/providers

List all registered AI providers.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "providers": ["anthropic", "copilot"]
}
```

```bash
curl http://localhost:8081/api/ai/providers
```

---

### AI Deploy

#### POST /api/ai/deploy

Generate a workflow from an intent, deploy any required components to the dynamic registry, and return the configuration.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `intent` | string | Yes | Natural language description |

```json
{
  "intent": "Build a stock price alerting system"
}
```

**Response** (200 OK):

```json
{
  "status": "deployed",
  "components": ["stock-checker", "alert-handler"],
  "workflow": { "modules": [...] }
}
```

**Status codes**: 200 OK, 400 Bad Request (missing intent), 500 Internal Server Error

```bash
curl -X POST http://localhost:8081/api/ai/deploy \
  -H "Content-Type: application/json" \
  -d '{"intent": "Build a stock price alerting system"}'
```

---

#### POST /api/ai/deploy/component

Deploy a single component to the dynamic registry. If `source` is empty, the AI service generates the code from the name and description.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Component name |
| `type` | string | No | Component type |
| `description` | string | No | What the component does (used for code generation if source is empty) |
| `source` | string | No | Go source code; if empty, generated by AI |

```json
{
  "name": "email-sender",
  "type": "notification",
  "description": "Sends email notifications via SMTP",
  "source": "package component\n..."
}
```

**Response** (201 Created):

```json
{
  "id": "email-sender",
  "name": "email-sender",
  "status": "loaded",
  "loaded_at": "2026-02-13T10:00:00Z"
}
```

**Status codes**: 201 Created, 400 Bad Request (missing name), 422 Unprocessable Entity (deploy failed), 500 Internal Server Error

```bash
curl -X POST http://localhost:8081/api/ai/deploy/component \
  -H "Content-Type: application/json" \
  -d '{"name": "email-sender", "description": "Sends email notifications"}'
```

---

### Workflow UI

#### GET /api/workflow/config

Get the current workflow configuration as JSON.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK): The full `WorkflowConfig` object with `modules`, `workflows`, and `triggers`.

```bash
curl http://localhost:8081/api/workflow/config
```

---

#### PUT /api/workflow/config

Replace the current workflow configuration. Accepts JSON or YAML (set `Content-Type: application/x-yaml` for YAML).

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**: A complete `WorkflowConfig` object.

**Response** (200 OK):

```json
{
  "status": "ok"
}
```

**Status codes**: 200 OK, 400 Bad Request

```bash
curl -X PUT http://localhost:8081/api/workflow/config \
  -H "Content-Type: application/json" \
  -d '{"modules": [{"name": "server", "type": "http.server", "config": {"address": ":9090"}}]}'
```

---

#### GET /api/workflow/modules

List all available module types with their configuration field schemas.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
[
  {
    "type": "http.server",
    "label": "HTTP Server",
    "category": "http",
    "configFields": [
      { "key": "address", "label": "Address", "type": "string", "defaultValue": ":8080" }
    ]
  }
]
```

```bash
curl http://localhost:8081/api/workflow/modules
```

---

#### POST /api/workflow/validate

Validate a workflow configuration without applying it.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**: A `WorkflowConfig` object.

**Response** (200 OK):

```json
{
  "valid": true,
  "errors": []
}
```

Or with validation errors:

```json
{
  "valid": false,
  "errors": [
    "module of type http.server has no name",
    "duplicate module name: server"
  ]
}
```

```bash
curl -X POST http://localhost:8081/api/workflow/validate \
  -H "Content-Type: application/json" \
  -d '{"modules": [{"name": "", "type": "http.server"}]}'
```

---

#### POST /api/workflow/reload

Reload the workflow engine with the current configuration. Stops the running engine and starts a new one.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "status": "reloaded"
}
```

**Status codes**: 200 OK, 500 Internal Server Error, 503 Service Unavailable (reload not configured)

```bash
curl -X POST http://localhost:8081/api/workflow/reload
```

---

#### GET /api/workflow/status

Get the current engine status including module and workflow counts.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "status": "running",
  "moduleCount": 15,
  "workflowCount": 3
}
```

```bash
curl http://localhost:8081/api/workflow/status
```

---

### Feature Flags

Feature flag management endpoints require authentication and a configured `featureflag.service` module.

#### GET /api/v1/admin/feature-flags

List all feature flag definitions.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK):

```json
[
  {
    "key": "new-pricing-engine",
    "type": "boolean",
    "description": "Enable the new pricing engine",
    "enabled": true,
    "default_val": "false",
    "tags": ["backend"],
    "percentage": 0,
    "created_at": "2026-02-16T10:00:00Z",
    "updated_at": "2026-02-16T10:00:00Z"
  }
]
```

```bash
curl http://localhost:8081/api/v1/admin/feature-flags \
  -H "Authorization: Bearer $TOKEN"
```

---

#### POST /api/v1/admin/feature-flags

Create a new feature flag.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Request body**:

```json
{
  "key": "new-pricing-engine",
  "type": "boolean",
  "description": "Enable the new pricing engine",
  "enabled": true,
  "default_val": "false",
  "tags": ["backend"],
  "percentage": 0
}
```

**Response** (201 Created): The created flag object.

**Status codes**: 201 Created, 400 Bad Request

```bash
curl -X POST http://localhost:8081/api/v1/admin/feature-flags \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key": "new-pricing-engine", "type": "boolean", "enabled": true, "default_val": "false"}'
```

---

#### GET /api/v1/admin/feature-flags/{key}

Get a specific feature flag by key.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK): Flag object.

**Status codes**: 200 OK, 404 Not Found

```bash
curl http://localhost:8081/api/v1/admin/feature-flags/new-pricing-engine \
  -H "Authorization: Bearer $TOKEN"
```

---

#### PUT /api/v1/admin/feature-flags/{key}

Update an existing feature flag.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Request body**: Flag object with updated fields.

**Response** (200 OK): Updated flag object.

**Status codes**: 200 OK, 400 Bad Request

```bash
curl -X PUT http://localhost:8081/api/v1/admin/feature-flags/new-pricing-engine \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key": "new-pricing-engine", "enabled": false}'
```

---

#### DELETE /api/v1/admin/feature-flags/{key}

Delete a feature flag and its associated rules and overrides.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK):

```json
{
  "status": "deleted"
}
```

**Status codes**: 200 OK, 500 Internal Server Error

```bash
curl -X DELETE http://localhost:8081/api/v1/admin/feature-flags/new-pricing-engine \
  -H "Authorization: Bearer $TOKEN"
```

---

#### PUT /api/v1/admin/feature-flags/{key}/overrides

Set user or group overrides for a feature flag.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Request body**:

```json
{
  "overrides": [
    {
      "scope": "user",
      "scope_key": "user-123",
      "value": "true"
    },
    {
      "scope": "group",
      "scope_key": "beta-testers",
      "value": "true"
    }
  ]
}
```

**Response** (200 OK): Updated flag with overrides.

**Status codes**: 200 OK, 400 Bad Request

```bash
curl -X PUT http://localhost:8081/api/v1/admin/feature-flags/new-pricing-engine/overrides \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"overrides": [{"scope": "user", "scope_key": "user-123", "value": "true"}]}'
```

---

#### GET /api/v1/admin/feature-flags/{key}/evaluate

Evaluate a feature flag for a specific user and/or group context.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Query parameters**:

| Parameter | Description |
|-----------|-------------|
| `user` | User ID for evaluation context |
| `group` | Group name for evaluation context |

**Response** (200 OK):

```json
{
  "value": {
    "key": "new-pricing-engine",
    "value": true,
    "type": "boolean",
    "source": "override",
    "reason": "user override matched"
  }
}
```

**Status codes**: 200 OK, 400 Bad Request

```bash
curl "http://localhost:8081/api/v1/admin/feature-flags/new-pricing-engine/evaluate?user=user-123" \
  -H "Authorization: Bearer $TOKEN"
```

---

#### GET /api/v1/admin/feature-flags/stream

SSE (Server-Sent Events) stream for real-time feature flag change notifications.

| Field | Value |
|-------|-------|
| Auth required | Yes |
| Content-Type | `text/event-stream` |

**Event format**:

```
event: flag.updated
data: {"key":"new-pricing-engine","value":true}
```

```bash
curl -N http://localhost:8081/api/v1/admin/feature-flags/stream \
  -H "Authorization: Bearer $TOKEN"
```

---

## Workflow Engine Endpoints (port 8080)

The workflow engine port serves endpoints defined by the YAML configuration. The following endpoints are provided by built-in module types. All paths below assume the default gateway at `http://localhost:8080`.

### Health Checks

These endpoints are provided by the `health.checker` module type.

#### GET /health

Run all registered health checks and return aggregate status.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK / 503 Service Unavailable):

```json
{
  "status": "healthy",
  "checks": {
    "database": { "status": "healthy", "message": "" },
    "messaging": { "status": "healthy", "message": "" }
  }
}
```

Status values: `healthy`, `degraded`, `unhealthy`. Returns 503 if any check is `unhealthy`.

```bash
curl http://localhost:8080/health
```

---

#### GET /ready

Readiness probe. Returns 200 only if the engine has started and all health checks pass.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "status": "ready"
}
```

**Response** (503 Service Unavailable):

```json
{
  "status": "not_ready"
}
```

```bash
curl http://localhost:8080/ready
```

---

#### GET /live

Liveness probe. Always returns 200.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK):

```json
{
  "status": "alive"
}
```

```bash
curl http://localhost:8080/live
```

---

### Metrics

Provided by the `metrics.collector` module type.

#### GET /metrics

Prometheus-format metrics endpoint.

| Field | Value |
|-------|-------|
| Auth required | No |
| Content-Type | `text/plain; charset=utf-8` (Prometheus exposition format) |

Available metrics:
- `workflow_executions_total` (counter, labels: `workflow_type`, `action`, `status`)
- `workflow_duration_seconds` (histogram, labels: `workflow_type`, `action`)
- `http_requests_total` (counter, labels: `method`, `path`, `status_code`)
- `http_request_duration_seconds` (histogram, labels: `method`, `path`)
- `module_operations_total` (counter, labels: `module`, `operation`, `status`)
- `active_workflows` (gauge, labels: `workflow_type`)

```bash
curl http://localhost:8080/metrics
```

---

### Authentication

Provided by the `auth.jwt` module type.

#### POST /api/auth/login

Authenticate with email and password, receive a JWT token.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `email` | string | Yes | User email address |
| `password` | string | Yes | User password |

```json
{
  "email": "responder1@example.com",
  "password": "demo123"
}
```

**Response** (200 OK):

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "user-001",
    "email": "responder1@example.com",
    "name": "Alex Rivera",
    "role": "responder",
    "affiliateId": "aff-001",
    "programIds": ["prog-001", "prog-002"]
  }
}
```

**Status codes**: 200 OK, 400 Bad Request, 401 Unauthorized

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "responder1@example.com", "password": "demo123"}'
```

---

#### POST /api/auth/register

Create a new user account and receive a JWT token.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `email` | string | Yes | Email address |
| `name` | string | No | Display name |
| `password` | string | Yes | Password |

```json
{
  "email": "newuser@example.com",
  "name": "New User",
  "password": "securepassword"
}
```

**Response** (201 Created):

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "9",
    "email": "newuser@example.com",
    "name": "New User",
    "createdAt": "2026-02-13T10:00:00Z"
  }
}
```

**Status codes**: 201 Created, 400 Bad Request (missing fields), 409 Conflict (email exists)

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "newuser@example.com", "name": "New User", "password": "securepassword"}'
```

---

#### GET /api/auth/profile

Get the authenticated user's profile.

| Field | Value |
|-------|-------|
| Auth required | Yes (any role) |

**Response** (200 OK):

```json
{
  "id": "user-001",
  "email": "responder1@example.com",
  "name": "Alex Rivera",
  "role": "responder",
  "affiliateId": "aff-001",
  "programIds": ["prog-001", "prog-002"],
  "createdAt": "2026-01-01T00:00:00Z"
}
```

**Status codes**: 200 OK, 401 Unauthorized

```bash
curl http://localhost:8080/api/auth/profile \
  -H "Authorization: Bearer $TOKEN"
```

---

#### PUT /api/auth/profile

Update the authenticated user's profile.

| Field | Value |
|-------|-------|
| Auth required | Yes (any role) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | New display name |

**Response** (200 OK): Updated user profile object.

**Status codes**: 200 OK, 400 Bad Request, 401 Unauthorized

```bash
curl -X PUT http://localhost:8080/api/auth/profile \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Updated Name"}'
```

---

### Platform Resources (CRUD)

The following endpoints are provided by the `api.handler` module type. Each resource follows the same CRUD pattern. Resources are filtered by the authenticated user's affiliate and program membership unless the user has the `admin` role.

All resource endpoints require authentication via the auth middleware. Admin role is required for create, update, and delete operations on affiliates, programs, users, keywords, and surveys.

**Common query parameters** (for list endpoints):

| Parameter | Description |
|-----------|-------------|
| `affiliateId` | Filter by affiliate ID |
| `programId` | Filter by program ID (comma-separated for multiple) |
| `role` | Filter users by role |

---

#### Affiliates

##### GET /api/affiliates

List all affiliates. Non-admin users see only their own affiliate.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK):

```json
[
  {
    "id": "aff-001",
    "data": {
      "name": "Crisis Support International",
      "region": "US-East",
      "dataRetentionDays": 365,
      "contactEmail": "admin@csi.org"
    },
    "state": "active",
    "lastUpdate": "2026-02-13T10:00:00Z"
  }
]
```

```bash
curl http://localhost:8080/api/affiliates \
  -H "Authorization: Bearer $TOKEN"
```

##### GET /api/affiliates/{id}

Get a specific affiliate by ID.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Status codes**: 200 OK, 404 Not Found

```bash
curl http://localhost:8080/api/affiliates/aff-001 \
  -H "Authorization: Bearer $TOKEN"
```

##### POST /api/affiliates

Create a new affiliate.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Request body**:

```json
{
  "id": "aff-003",
  "name": "New Affiliate",
  "region": "US-Central",
  "dataRetentionDays": 180,
  "contactEmail": "admin@newaffiliate.org"
}
```

**Status codes**: 201 Created, 400 Bad Request

```bash
curl -X POST http://localhost:8080/api/affiliates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id": "aff-003", "name": "New Affiliate", "region": "US-Central"}'
```

##### PUT /api/affiliates/{id}

Update an existing affiliate.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Status codes**: 200 OK, 400 Bad Request, 404 Not Found

##### DELETE /api/affiliates/{id}

Delete an affiliate.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Status codes**: 204 No Content, 404 Not Found

---

#### Programs

##### GET /api/programs

List all programs. Filtered by affiliate and program membership for non-admin users.

| Field | Value |
|-------|-------|
| Auth required | Yes |

```bash
curl http://localhost:8080/api/programs \
  -H "Authorization: Bearer $TOKEN"
```

##### GET /api/programs/{id}

Get a specific program.

| Field | Value |
|-------|-------|
| Auth required | Yes |

##### POST /api/programs

Create a new program.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Request body**:

```json
{
  "id": "prog-005",
  "name": "New Program",
  "affiliateId": "aff-001",
  "providers": ["twilio", "webchat"],
  "shortCode": "741743",
  "description": "A new support program",
  "settings": {
    "maxConcurrentPerResponder": 3,
    "queueAlertThreshold": 10
  }
}
```

**Status codes**: 201 Created, 400 Bad Request

##### PUT /api/programs/{id}

Update a program.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

##### DELETE /api/programs/{id}

Delete a program.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

---

#### Users

##### GET /api/users

List all users. Supports `?role=responder` filter.

| Field | Value |
|-------|-------|
| Auth required | Yes |

```bash
curl http://localhost:8080/api/users \
  -H "Authorization: Bearer $TOKEN"

# Filter by role
curl "http://localhost:8080/api/users?role=responder" \
  -H "Authorization: Bearer $TOKEN"
```

##### GET /api/users/{id}

Get a specific user.

| Field | Value |
|-------|-------|
| Auth required | Yes |

##### POST /api/users

Create a new user.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Request body**:

```json
{
  "id": "user-010",
  "email": "newresponder@example.com",
  "name": "New Responder",
  "role": "responder",
  "affiliateId": "aff-001",
  "programIds": ["prog-001"],
  "maxConcurrent": 3,
  "password": "securepassword"
}
```

##### PUT /api/users/{id}

Update a user.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

##### DELETE /api/users/{id}

Delete a user.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

---

#### Keywords

##### GET /api/keywords

List all keyword routing rules.

| Field | Value |
|-------|-------|
| Auth required | Yes |

```bash
curl http://localhost:8080/api/keywords \
  -H "Authorization: Bearer $TOKEN"
```

##### POST /api/keywords

Create a keyword routing rule.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Request body**:

```json
{
  "programId": "prog-001",
  "keyword": "SOS",
  "action": "route_priority",
  "subProgram": "crisis-immediate",
  "response": "Help is on the way. Stay with us."
}
```

**Status codes**: 201 Created, 400 Bad Request

##### PUT /api/keywords/{id}

Update a keyword.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

##### DELETE /api/keywords/{id}

Delete a keyword.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

---

#### Surveys

##### GET /api/surveys

List all survey templates.

| Field | Value |
|-------|-------|
| Auth required | Yes |

```bash
curl http://localhost:8080/api/surveys \
  -H "Authorization: Bearer $TOKEN"
```

##### POST /api/surveys

Create a survey template.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

**Request body**:

```json
{
  "programId": "prog-001",
  "type": "exit",
  "title": "Session Review",
  "questions": [
    { "id": "q1", "text": "How do you feel now?", "type": "scale", "min": 1, "max": 5 },
    { "id": "q2", "text": "Was this helpful?", "type": "choice", "options": ["Yes", "No"] }
  ]
}
```

**Status codes**: 201 Created, 400 Bad Request

##### PUT /api/surveys/{id}

Update a survey.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

##### DELETE /api/surveys/{id}

Delete a survey.

| Field | Value |
|-------|-------|
| Auth required | Yes (admin) |

---

### Conversations

Conversation endpoints are backed by the `api.handler` module with a state-machine workflow engine. State transitions are triggered automatically when sub-action endpoints are called.

#### GET /api/conversations

List conversations. Filtered by the authenticated user's affiliate and program membership.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK):

```json
[
  {
    "id": "conv-001",
    "data": {
      "from": "+15551234567",
      "programId": "prog-001",
      "programName": "Crisis Text Line",
      "affiliateId": "aff-001",
      "riskLevel": "low",
      "tags": [],
      "messages": [...]
    },
    "state": "queued",
    "lastUpdate": "2026-02-13T10:00:00Z"
  }
]
```

```bash
curl http://localhost:8080/api/conversations \
  -H "Authorization: Bearer $TOKEN"
```

---

#### GET /api/conversations/{id}

Get conversation detail with messages, tags, risk level, and live state from the workflow engine.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK): Full conversation resource with enriched state data.

**Status codes**: 200 OK, 404 Not Found

```bash
curl http://localhost:8080/api/conversations/conv-001 \
  -H "Authorization: Bearer $TOKEN"
```

---

#### POST /api/conversations/{id}/messages

Send a message in a conversation. Appends to the message history and re-assesses risk level.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | Yes | Message text |
| `direction` | string | No | `inbound` or `outbound` (defaults to outbound for responders) |

```json
{
  "content": "I hear you. Can you tell me more about what you are feeling?",
  "direction": "outbound"
}
```

**Response** (201 Created):

```json
{
  "messageId": "msg-conv-001-5",
  "conversationId": "conv-001",
  "direction": "outbound",
  "status": "sent",
  "timestamp": "2026-02-13T10:05:00Z"
}
```

**Status codes**: 201 Created, 404 Not Found

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content": "I hear you. Can you tell me more?", "direction": "outbound"}'
```

---

#### POST /api/conversations/{id}/assign

Assign a queued conversation to the authenticated responder. Triggers the `assign` state transition.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**: Empty or `{}`.

**Response** (200 OK):

```json
{
  "success": true,
  "action": "assign",
  "transition": "assign_responder",
  "id": "conv-001",
  "state": "active",
  "lastUpdate": "2026-02-13T10:01:00Z"
}
```

**Status codes**: 200 OK, 400 Bad Request (invalid transition), 404 Not Found

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/assign \
  -H "Authorization: Bearer $TOKEN"
```

---

#### POST /api/conversations/{id}/transfer

Transfer a conversation to another responder.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `reason` | string | No | Transfer reason / context for handoff |

```json
{
  "reason": "Shift ending, warm handoff needed"
}
```

**Response** (200 OK):

```json
{
  "success": true,
  "action": "transfer",
  "transition": "transfer_conversation",
  "id": "conv-001",
  "state": "transferred",
  "lastUpdate": "2026-02-13T10:10:00Z"
}
```

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/transfer \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reason": "Shift ending, warm handoff needed"}'
```

---

#### POST /api/conversations/{id}/escalate

Escalate a conversation to medical or police services.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | `medical` or `police` |
| `urgency` | string | No | `low`, `medium`, `high` |
| `location` | string | No | Location information for emergency services |

```json
{
  "type": "medical",
  "urgency": "high",
  "location": "Seattle, WA"
}
```

**Response** (200 OK):

```json
{
  "success": true,
  "action": "escalate",
  "transition": "escalate_to_medical",
  "id": "conv-001",
  "state": "escalated_medical",
  "lastUpdate": "2026-02-13T10:15:00Z"
}
```

When `type` is `police`, the transition name becomes `escalate_to_police`.

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/escalate \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type": "medical", "urgency": "high", "location": "Seattle, WA"}'
```

---

#### POST /api/conversations/{id}/wrap-up

Begin the conversation wrap-up process. Triggers exit survey if configured.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**: Empty or `{}`.

**Response** (200 OK):

```json
{
  "success": true,
  "action": "wrap-up",
  "transition": "begin_wrap_up",
  "id": "conv-001",
  "state": "wrap_up",
  "lastUpdate": "2026-02-13T10:20:00Z"
}
```

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/wrap-up \
  -H "Authorization: Bearer $TOKEN"
```

---

#### POST /api/conversations/{id}/close

Close a conversation. The transition name varies based on current state:

| Current State | Transition |
|---------------|-----------|
| `wrap_up` | `close_from_wrap_up` |
| `follow_up_active` | `close_from_followup` |
| Other | `close_conversation` |

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**: Empty or `{}`.

**Response** (200 OK):

```json
{
  "success": true,
  "action": "close",
  "transition": "close_from_wrap_up",
  "id": "conv-001",
  "state": "closed",
  "lastUpdate": "2026-02-13T10:25:00Z"
}
```

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/close \
  -H "Authorization: Bearer $TOKEN"
```

---

#### POST /api/conversations/{id}/follow-up

Schedule an automated follow-up check-in.

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scheduledTime` | string | No | ISO 8601 datetime for the follow-up |
| `message` | string | No | Follow-up message text |

```json
{
  "scheduledTime": "2026-02-14T10:00:00Z",
  "message": "Hi, just checking in. How are you doing today?"
}
```

**Response** (200 OK):

```json
{
  "success": true,
  "action": "follow-up",
  "transition": "schedule_follow_up",
  "id": "conv-001",
  "state": "follow_up_active",
  "lastUpdate": "2026-02-13T10:30:00Z"
}
```

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/follow-up \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"scheduledTime": "2026-02-14T10:00:00Z", "message": "Hi, just checking in."}'
```

---

#### POST /api/conversations/{id}/tag

Add tags to a conversation. This is a data-only update (no state transition).

| Field | Value |
|-------|-------|
| Auth required | Yes (responder) |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tags` | string[] | No | Array of tag strings to add |
| `tag` | string | No | Single tag to add |

```json
{
  "tags": ["anxiety", "school-stress"]
}
```

**Response** (200 OK):

```json
{
  "success": true,
  "id": "conv-001",
  "tags": ["anxiety", "school-stress"]
}
```

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/tag \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tags": ["anxiety", "school-stress"]}'
```

---

#### POST /api/conversations/{id}/survey

Submit a survey response for a conversation.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `surveyId` | string | Yes | Survey template ID |
| `responses` | object[] | Yes | Array of `{id, value}` answer objects |

```json
{
  "surveyId": "survey-002",
  "responses": [
    { "id": "q1", "value": 4 },
    { "id": "q2", "value": "Yes" }
  ]
}
```

**Response** (200 OK): Success confirmation with updated state.

```bash
curl -X POST http://localhost:8080/api/conversations/conv-001/survey \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"surveyId": "survey-002", "responses": [{"id": "q1", "value": 4}]}'
```

---

#### GET /api/conversations/{id}/summary

Get a summary of the conversation including key fields and live state.

| Field | Value |
|-------|-------|
| Auth required | Yes |

**Response** (200 OK):

```json
{
  "id": "conv-001",
  "state": "active",
  "lastUpdate": "2026-02-13T10:05:00Z",
  "riskLevel": "medium",
  "tags": ["anxiety"],
  "programId": "prog-001"
}
```

The summary includes fields from the configurable `summaryFields` list (default: `riskLevel`, `tags`, `programId`, `programName`, `affiliateId`).

```bash
curl http://localhost:8080/api/conversations/conv-001/summary \
  -H "Authorization: Bearer $TOKEN"
```

---

### Queue

Queue endpoints are backed by a view handler (`api.handler` with `sourceResourceName` set to `conversations` and `stateFilter` set to `queued`).

#### GET /api/queue

Get queued conversations with count.

| Field | Value |
|-------|-------|
| Auth required | Yes (supervisor, admin) |

**Response** (200 OK):

```json
{
  "totalQueued": 5,
  "count": 5,
  "conversations": [...]
}
```

```bash
curl http://localhost:8080/api/queue \
  -H "Authorization: Bearer $TOKEN"
```

---

#### GET /api/queue/health

Get per-program queue health metrics.

| Field | Value |
|-------|-------|
| Auth required | Yes (supervisor, admin) |

**Response** (200 OK):

```json
{
  "programs": [
    {
      "programId": "prog-001",
      "programName": "Crisis Text Line",
      "depth": 3,
      "queued": 3,
      "avgWaitSeconds": 120.5,
      "oldestMessageAt": "2026-02-13T09:58:00Z",
      "alertThreshold": 10
    }
  ],
  "alerts": 0
}
```

```bash
curl http://localhost:8080/api/queue/health \
  -H "Authorization: Bearer $TOKEN"
```

---

### Providers

#### GET /api/providers

List configured messaging providers.

| Field | Value |
|-------|-------|
| Auth required | Yes |

```bash
curl http://localhost:8080/api/providers \
  -H "Authorization: Bearer $TOKEN"
```

---

### Webhooks

Webhook endpoints receive inbound messages from external providers. They create conversation resources and trigger the workflow pipeline.

#### POST /api/webhooks/twilio

Receive an inbound SMS from Twilio.

| Field | Value |
|-------|-------|
| Auth required | No |
| Content-Type | `application/x-www-form-urlencoded` |

**Request body** (form-encoded):

| Field | Type | Description |
|-------|------|-------------|
| `From` | string | Sender phone number (E.164 format) |
| `To` | string | Destination short code |
| `Body` | string | Message text |
| `MessageSid` | string | Twilio message identifier |

**Response** (201 Created): Resource object with state and workflow instance.

```bash
curl -X POST http://localhost:8080/api/webhooks/twilio \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "From=%2B15551234567&To=%2B1741741&Body=HELLO&MessageSid=SM1234567890"
```

---

#### POST /api/webhooks/aws

Receive an inbound message from AWS SNS/Pinpoint.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

```json
{
  "Type": "Notification",
  "MessageId": "msg-aws-001",
  "Message": "{\"originationNumber\":\"+15559876543\",\"destinationNumber\":\"+1741741\",\"messageBody\":\"HELP\",\"messageKeyword\":\"HELP\"}"
}
```

**Response** (201 Created): Resource object.

```bash
curl -X POST http://localhost:8080/api/webhooks/aws \
  -H "Content-Type: application/json" \
  -d '{"Type": "Notification", "MessageId": "msg-aws-001", "Message": "{\"originationNumber\":\"+15559876543\",\"messageBody\":\"HELP\"}"}'
```

---

#### POST /api/webhooks/partner

Receive an inbound message from a partner integration.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Description |
|-------|------|-------------|
| `partnerId` | string | Partner identifier |
| `from` | string | Sender phone number |
| `message` | string | Message text |
| `timestamp` | string | ISO 8601 timestamp |
| `metadata` | object | Additional partner metadata |

```json
{
  "partnerId": "partner-001",
  "from": "+15555551234",
  "message": "WELLNESS",
  "timestamp": "2026-02-13T10:00:00Z",
  "metadata": { "region": "EU-West" }
}
```

**Response** (201 Created): Resource object.

```bash
curl -X POST http://localhost:8080/api/webhooks/partner \
  -H "Content-Type: application/json" \
  -d '{"partnerId": "partner-001", "from": "+15555551234", "message": "WELLNESS"}'
```

---

### Webchat

Webchat endpoints allow web-based messaging without SMS. No authentication is required; sessions are identified by `sessionId`.

#### POST /api/webchat/message

Send a message from the webchat widget.

| Field | Value |
|-------|-------|
| Auth required | No |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sessionId` | string | Yes | Unique session identifier |
| `message` | string | Yes | Message text |

```json
{
  "sessionId": "web-session-001",
  "message": "I need someone to talk to"
}
```

**Response** (201 Created): Resource object.

```bash
curl -X POST http://localhost:8080/api/webchat/message \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "web-session-001", "message": "I need someone to talk to"}'
```

---

#### GET /api/webchat/poll/{sessionId}

Poll for new messages in a webchat session.

| Field | Value |
|-------|-------|
| Auth required | No |

**Response** (200 OK): Conversation resource with messages array.

**Status codes**: 200 OK, 404 Not Found

```bash
curl http://localhost:8080/api/webchat/poll/web-session-001
```

---

### State Machine Transitions

Any resource with a configured workflow engine supports explicit state transitions.

#### POST /api/{resource}/{id}/transition

Trigger a named state machine transition on a resource.

| Field | Value |
|-------|-------|
| Auth required | Depends on resource |

**Request body**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `transition` | string | Yes | Transition name (e.g., `start_validation`, `approve`) |
| `data` | object | No | Additional data to merge into the workflow |
| `workflowType` | string | No | Override the configured workflow type |

```json
{
  "transition": "approve",
  "data": {
    "approvedBy": "manager-001"
  }
}
```

**Response** (200 OK):

```json
{
  "success": true,
  "id": "order-001",
  "instanceId": "conv-order-001",
  "transition": "approve",
  "state": "approved",
  "lastUpdate": "2026-02-13T10:00:00Z",
  "resource": { ... }
}
```

**Status codes**: 200 OK, 400 Bad Request (invalid transition, failed transition), 404 Not Found, 500 Internal Server Error (no engine)

```bash
curl -X POST http://localhost:8080/api/orders/order-001/transition \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"transition": "approve", "data": {"approvedBy": "manager-001"}}'
```
