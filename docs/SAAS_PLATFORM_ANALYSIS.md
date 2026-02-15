# SaaS/PaaS Platform Gap Analysis

**Goal**: Transform the Workflow engine into a central SaaS/PaaS service where
customers pay to build, deploy, and manage their applications.

**Date**: February 2026
**Status**: Analysis

---

## Executive Summary

### Vision: Workflow-as-a-Service

A hosted platform where customers sign up, design workflows visually or via YAML,
deploy them to managed infrastructure, and pay based on usage. The platform handles
scaling, monitoring, billing, and lifecycle management -- customers focus on their
business logic.

### Current Maturity Assessment

The Workflow engine is surprisingly far along for SaaS readiness. It already has:

- A multi-tenant data model (Company > Organization > Project > Workflow hierarchy)
- 60+ REST API endpoints covering auth, CRUD, versioning, execution tracking, audit, IAM
- JWT authentication with refresh tokens, user registration, role-based access control
- A visual workflow builder (React + ReactFlow) with 70+ module types
- Pipeline-native API routes that replace delegation with declarative step sequences (request_parse -> db_query -> json_response), proving the engine can express its own admin API using its own primitives
- Template engine with built-in functions (uuidv4, now, lower, default, json)
- Workflow versioning with config snapshots per version
- Deploy/stop lifecycle management via API
- Execution tracking with step-level detail and duration
- Audit logging with user, action, resource, and timestamp
- IAM provider framework (OIDC, SAML, LDAP, AWS IAM, Kubernetes)
- Prometheus metrics, health probes, dashboard API

What it lacks are the operational concerns that separate a product from a platform:
automated deployment, billing, tenant isolation beyond logical scoping, horizontal
scaling, and self-service onboarding.

### Estimated Effort to MVP

| Phase | Duration | Focus |
|-------|----------|-------|
| Phase 1: Foundation | Weeks 1-8 | Billing, deployment automation, PostgreSQL state, auth hardening |
| Phase 2: Scale & Security | Weeks 9-18 | Kubernetes, compliance, team management, API keys |
| Phase 3: Enterprise | Weeks 19-28 | SSO hardening, multi-region, DR, CI/CD integration |
| Phase 4: Ecosystem | Weeks 29-38+ | Marketplace, templates, SDKs, community |

**MVP (paying customers possible): ~8 weeks** (reduced from 10 -- pipeline-native routes
eliminate custom Go handler dependency for basic CRUD workloads, simplifying deployment).
Full enterprise platform: ~28 weeks.

---

## 1. What Already Exists

### Multi-Tenancy

The V1 API implements a four-level tenant hierarchy:

```
Company (top-level billing entity)
  -> Organization (department/team)
    -> Project (logical grouping)
      -> Workflow (deployable unit)
```

**Implemented in**: `module/api_v1_store.go`, `module/api_v1_handler.go`

| Capability | Status | Implementation Detail |
|-----------|:------:|----------------------|
| Company CRUD | Built | `POST/GET /api/v1/companies`, owner_id tracking |
| Organization nesting | Built | Companies with `parent_id` serve as organizations |
| Project management | Built | Scoped under organizations via `company_id` FK |
| Workflow CRUD | Built | Full create/read/update/delete with project scoping |
| System entity protection | Built | `is_system` flag prevents deletion/modification by non-admins |
| Slug generation | Built | Auto-generated URL-safe slugs from names |
| Metadata storage | Built | JSON metadata field on companies, projects |

### Workflow Lifecycle

**Implemented in**: `module/api_v1_handler.go`, `module/api_v1_store.go`

| Capability | Status | Implementation Detail |
|-----------|:------:|----------------------|
| Workflow creation | Built | `POST /projects/{id}/workflows` with config_yaml |
| Workflow versioning | Built | Auto-incrementing version on config change, version snapshots |
| Version history | Built | `GET /workflows/{id}/versions` returns all snapshots |
| Deploy/stop lifecycle | Built | `POST /workflows/{id}/deploy`, `POST /workflows/{id}/stop` |
| Status tracking | Built | States: `draft` -> `active` -> `stopped` -> `error` |
| System workflow reload | Built | Deploy triggers engine reload via `reloadFn` callback |
| Config diff detection | Built | Version only increments when `config_yaml` actually changes |
| Created/updated tracking | Built | `created_by`, `updated_by` with email or user ID |

### Management API

The API client (`ui/src/utils/api.ts`) reveals 60+ REST endpoints across these domains:

| Domain | Endpoint Count | Key Endpoints |
|--------|:--------------:|--------------|
| Engine Management | 4 | Config get/put, module types, validate |
| AI Integration | 3 | Generate workflow, generate component, suggest |
| Dynamic Components | 3 | List, create, delete hot-reload components |
| Authentication | 6 | Login, register, refresh, logout, profile get/update |
| User Management | 4 | List, create, delete users, update roles |
| Companies | 3 | List, create, get companies |
| Organizations | 2 | List, create organizations |
| Projects | 2 | List, create projects |
| Workflows | 8 | CRUD, deploy, stop, status, versions |
| Permissions | 2 | List permissions, share workflow |
| Dashboard | 2 | System dashboard, per-workflow dashboard |
| Executions | 4 | List, detail, steps, trigger, cancel |
| Logs | 2 | Fetch logs, SSE log stream |
| Events | 2 | Fetch events, SSE event stream |
| Audit | 1 | Fetch audit log with filters |
| IAM Providers | 5 | CRUD + test for identity providers |
| IAM Role Mappings | 2 | Create, delete role mappings |

### Visual Builder

**Implemented in**: `ui/` (React + Vite + TypeScript)

| Capability | Status | Implementation Detail |
|-----------|:------:|----------------------|
| ReactFlow canvas | Built | Drag-and-drop workflow composition |
| 70+ module types | Built | Schema-driven from `schema/module_schema.go` |
| Property panel | Built | Per-module config forms with field validation |
| Pipeline step composition | Built | Sequential step ordering on canvas |
| YAML import/export | Built | Bidirectional YAML <-> visual conversion |
| Connection validation | Built | Input/output type compatibility checking |
| Module schema registry | Built | `GET /api/v1/module-schemas` with fallback to static types |
| Real-time preview | Built | Live config preview as modules are configured |

### Observability

**Implemented in**: Various modules, `ui/src/types/observability.ts`

| Capability | Status | Implementation Detail |
|-----------|:------:|----------------------|
| Prometheus metrics | Built | Exposed at `/metrics`, scrape-ready |
| Health probes | Built | `/healthz`, `/readyz`, `/livez` (K8s compatible) |
| Execution history | Built | Per-workflow execution list with filters (status, date range) |
| Step-level tracking | Built | Each execution step tracked with input/output/duration |
| Duration metrics | Built | `duration_ms` on executions and steps |
| Log collection | Built | Structured logs with level, module, execution correlation |
| Log streaming | Built | Server-Sent Events for real-time log tailing |
| Event streaming | Built | Server-Sent Events for real-time event monitoring |
| Dashboard aggregation | Built | Execution counts and log counts per workflow |

### Identity & Access

**Implemented in**: `module/jwt_auth.go`, `module/auth_middleware.go`, `module/auth_user_store.go`

| Capability | Status | Implementation Detail |
|-----------|:------:|----------------------|
| JWT authentication | Built | HMAC-signed tokens with `sub`, `email`, `role` claims |
| Refresh tokens | Built | `POST /auth/refresh` with refresh token rotation |
| User registration | Built | `POST /auth/register` with bcrypt password hashing |
| Login/logout | Built | Token issuance and invalidation |
| User profile | Built | `GET/PUT /auth/me` for self-service profile updates |
| Role-based access | Built | `admin` role check on system resources |
| Auth middleware | Built | Pluggable `AuthProvider` interface, context propagation |
| User store | Built | In-memory with optional persistence write-through |
| Seed file loading | Built | Bootstrap users from JSON file |
| IAM provider framework | Built | OIDC, SAML, LDAP, AWS IAM, Kubernetes provider types |
| Role mapping | Built | Map external identifiers to internal roles |
| Membership model | Built | User-to-company/project membership with role (Owner/Admin/Editor/Viewer) |
| Audit trail | Built | Action logging with user, resource type, resource ID, IP, user agent |

---

## Pipeline-Native Architecture

### The Shift from Delegation to Declarative Pipelines

The workflow engine originally implemented admin API routes using `step.delegate`, which
forwarded HTTP requests to monolithic Go handler functions. This was functional but
contradicted the platform's core value proposition: everything should be config-driven.

The pipeline-native architecture replaces `step.delegate` with real pipeline steps that
express API logic entirely in YAML. Instead of "forward this request to a Go function,"
routes now declare exactly what happens at each stage: parse the request, query the
database, transform the result, send the response.

### New Step Types

| Step Type | Purpose | Key Features |
|-----------|---------|-------------|
| `step.request_parse` | Extract data from HTTP requests | Path params, query params, JSON body, headers; automatic type coercion |
| `step.db_query` | Execute SELECT queries | Parameterized SQL, template-driven queries, result mapping to context |
| `step.db_exec` | Execute INSERT/UPDATE/DELETE | Parameterized SQL, affected row count, last insert ID capture |
| `step.json_response` | Send JSON HTTP responses | Status code, headers, body templates with context variable interpolation |

### What This Means for SaaS

Pipeline-native routes are a breakthrough for the SaaS model because customers can now
build complete CRUD APIs entirely in YAML without writing a single line of Go code.
Previously, any non-trivial API endpoint required a custom Go handler. Now, the full
request lifecycle -- parsing, validation, database operations, response formatting --
is expressible as a declarative step sequence.

This directly addresses the "Go-only dynamic components" business risk identified in
the gap analysis. Customers do not need to know Go to build production APIs on the
platform.

### Example: Pipeline-Native GET Endpoint

A GET endpoint that retrieves a resource by ID:

```yaml
- path: "/api/v1/items/{id}"
  method: GET
  steps:
    - type: step.request_parse
      config:
        path_params: ["id"]
    - type: step.db_query
      config:
        query: "SELECT id, name, status, created_at FROM items WHERE id = ?"
        params: ["{{.Request.PathParams.id}}"]
        result_key: "item"
    - type: step.conditional
      config:
        condition: "{{if .Pipeline.item}}true{{else}}false{{end}}"
        on_false:
          type: step.json_response
          config:
            status: 404
            body: '{"error": "Item not found"}'
    - type: step.json_response
      config:
        status: 200
        body: '{{json .Pipeline.item}}'
```

### Example: Pipeline-Native POST Endpoint

A POST endpoint that creates a new resource with validation:

```yaml
- path: "/api/v1/items"
  method: POST
  steps:
    - type: step.request_parse
      config:
        body_format: json
        required_fields: ["name"]
    - type: step.validate
      config:
        rules:
          - field: "name"
            min_length: 1
            max_length: 255
    - type: step.set
      config:
        values:
          id: "{{uuidv4}}"
          created_at: "{{now}}"
          status: "{{default .Request.Body.status \"active\"}}"
    - type: step.db_exec
      config:
        query: "INSERT INTO items (id, name, status, created_at) VALUES (?, ?, ?, ?)"
        params:
          - "{{.Pipeline.id}}"
          - "{{.Request.Body.name}}"
          - "{{.Pipeline.status}}"
          - "{{.Pipeline.created_at}}"
    - type: step.json_response
      config:
        status: 201
        body: '{"id": "{{.Pipeline.id}}", "name": "{{.Request.Body.name}}", "status": "{{.Pipeline.status}}"}'
```

### Template Functions Available

The template engine provides built-in functions for pipeline logic:

| Function | Description | Example |
|----------|-------------|---------|
| `uuidv4` | Generate a UUID v4 | `{{uuidv4}}` -> `"a1b2c3d4-..."` |
| `now` | Current timestamp (RFC3339) | `{{now}}` -> `"2026-02-15T..."` |
| `lower` | Lowercase a string | `{{lower .name}}` -> `"example"` |
| `upper` | Uppercase a string | `{{upper .name}}` -> `"EXAMPLE"` |
| `default` | Fallback if value is empty | `{{default .status "active"}}` -> `"active"` |
| `json` | Marshal value to JSON | `{{json .Pipeline.item}}` -> `{...}` |
| `contains` | Check string containment | `{{contains .name "test"}}` |
| `replace` | String replacement | `{{replace .name " " "-"}}` |
| `trimSpace` | Trim leading/trailing whitespace | `{{trimSpace .name}}` |
| `urlEncode` | URL-encode a string | `{{urlEncode .query}}` |

---

## 2. Gap Analysis by Category

### 2A. Deployment & Infrastructure Management

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Deployment method | Manual CLI: `./server -config app.yaml` | API-driven automated deployment | No automation |
| Container support | No Dockerfile in repo | Container image build pipeline | Need Dockerfile + CI |
| Orchestration | None | Kubernetes operator for scaling | Need K8s operator |
| Multi-region | Single location | At least 2 regions for reliability | No region awareness |
| Deployment strategy | Hard restart (200-500ms downtime) | Blue/green, canary, rolling | Only rolling via LB (manual) |
| Infrastructure provisioning | Manual | Terraform/Pulumi for tenant infra | Nothing exists |

**Priority**: P0 -- Cannot accept paying customers without automated deployment.
Kubernetes operator and multi-region remain needed. However, pipeline-native routes
reduce the complexity of the deployed artifact: customer workloads are pure YAML
configurations, not custom Go binaries, simplifying the deployment pipeline.

**Effort**: 8-12 weeks

**Breakdown**:
- Dockerfile + CI pipeline: 1 week
- Deployment API (receive config, build, deploy): 2-3 weeks
- Kubernetes operator (basic): 3-4 weeks
- Blue/green deployment support: 1-2 weeks
- Multi-region foundation: 2-3 weeks

### 2B. Tenant Isolation & Security

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Data isolation | Logical (`company_id` scoping, shared SQLite) | Database-per-tenant option | Shared DB only |
| Network isolation | Single process, no network boundary | VPC/namespace per tenant | None |
| Resource quotas | None | CPU, memory, connection limits per tenant | None |
| Secrets management | `JWT_SECRET` env var, seed files | Vault/AWS Secrets Manager integration | `secrets.vault` and `secrets.aws` module types exist but not integrated with tenant model |
| Encryption at rest | None | Per-tenant encryption keys | None |
| Compliance | None | SOC 2 Type II readiness | Audit logging exists (good start) |
| PII handling | No data classification | GDPR: data deletion, export, consent | No data lifecycle management |
| Input validation | Basic (empty string checks) | Schema validation, SQL injection prevention, XSS | Partial |

**Priority**: P0 for basic isolation (quotas, separate schemas); P1 for compliance.

**Effort**: 6-8 weeks

**Breakdown**:
- PostgreSQL migration from SQLite: 1 week (database.workflow module exists)
- Schema-per-tenant isolation: 1-2 weeks
- Resource quota enforcement at engine level: 1-2 weeks
- Secrets management tenant integration: 1 week
- SOC 2 gap analysis + remediation: 2-3 weeks

### 2C. Billing & Metering

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Execution tracking | Built (count, duration, status) | Billable usage metering | Need billing dimension tagging |
| Subscription management | None | Stripe integration (plans, trials, upgrades) | Not built |
| Tier-based limits | None | Free/Pro/Business/Enterprise enforcement | Not built |
| Quota enforcement | None | Block execution when quota exceeded | Not built |
| Invoice generation | None | Automated monthly invoicing | Not built |
| Usage dashboard | Execution counts exist in dashboard | Customer-facing usage vs. quota view | Partially exists |
| Overage handling | None | Automatic overage billing or graceful degradation | Not built |

**Priority**: P0 -- Core to monetization. Cannot generate revenue without billing.

**Effort**: 4-6 weeks

**Breakdown**:
- Stripe subscription integration: 1-2 weeks
- Usage metering pipeline (aggregate executions per billing period): 1 week
- Tier limit enforcement in engine: 1 week
- Customer-facing usage dashboard: 1 week
- Overage and invoice automation: 1 week

**Existing advantage**: The execution tracking infrastructure (`WorkflowExecution` with
`duration_ms`, `started_at`, per-step tracking) provides the raw data. The gap is
aggregating it into billing dimensions and connecting to a payment processor.

**Note (Feb 2026)**: Billing remains the single most critical gap for SaaS launch. The
pipeline-native architecture reduces the Go code dependency significantly (customers can
build CRUD APIs in YAML), but without Stripe integration and usage metering, there is
no path to revenue. This is still P0 and should be the first priority after engine
feature completeness is demonstrated.

### 2D. User Management & Onboarding

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Self-service signup | `POST /auth/register` exists | Email verification, CAPTCHA | No email verification |
| Onboarding wizard | None | Guided: create company -> first project -> first workflow | Not built |
| Team invitations | None | Invite by email, accept/decline, role assignment | Not built |
| SSO/SAML | IAM provider framework built (OIDC, SAML, LDAP types) | Production-hardened SSO with major IdPs | Framework exists, needs hardening |
| Password reset | None | Forgot password flow with email token | Not built |
| API key management | None | Per-user or per-project API keys for CI/CD | Not built |
| Account management | Basic profile update | Plan changes, billing info, team settings | Minimal |
| User activity tracking | Audit log exists | Last login, active sessions, usage patterns | Partial |

**Priority**: P1

**Effort**: 3-4 weeks

**Breakdown**:
- Email verification + password reset: 1 week
- Onboarding wizard (UI + API): 1 week
- Team invitation system: 1 week
- API key management: 0.5-1 week

### 2E. Horizontal Scaling

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| State machine | In-memory `sync.RWMutex` | Distributed locks (PostgreSQL advisory locks) | Architecture doc describes approach, not implemented |
| Messaging | In-memory broker (`messaging.broker`) | Distributed broker (NATS/Kafka) | `messaging.nats` and `messaging.kafka` module types registered but untested at scale |
| Session/cache | None | Redis for rate limiting, sessions | `cache.modular` exists but not Redis-backed |
| Database | SQLite (single-writer) | PostgreSQL with connection pooling | `database.workflow` module supports PostgreSQL driver |
| Engine instances | Single process | Stateless instances behind load balancer | Architecture supports it, not implemented |
| EventBus | In-process | Distributed (`eventbus.bridge` module exists) | Bridge module exists, needs production validation |

**Priority**: P0 for medium-scale (100s of tenants); P1 for large-scale (1000s).

**Effort**: 4-6 weeks

**Breakdown**:
- PostgreSQL migration + distributed state machine: 2 weeks
- NATS/Kafka production messaging: 1-2 weeks
- Redis integration for rate limiting and caching: 1 week
- Load testing and validation: 1 week

**Existing advantage**: The module architecture abstracts storage and messaging behind
interfaces. The `database.workflow` module already accepts a PostgreSQL DSN. The
`messaging.nats` and `messaging.kafka` module types are registered in `engine.go`.
The work is integration and hardening, not greenfield development.

### 2F. API Gateway & Rate Limiting

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Rate limiting | Per-instance (`http.middleware.ratelimit`, 300 req/min default) | Global rate limiting (shared Redis counter) | Per-instance only |
| API keys | None | Per-customer API key issuance and validation | Not built |
| Request throttling | Uniform rate for all users | Per-tier throttling (Free: 10 req/s, Pro: 100 req/s) | Not built |
| API versioning | All routes under `/api/v1` | Versioning strategy for breaking changes | V1 prefix exists, no migration strategy |
| OpenAPI spec | `openapi.generator` module type registered | Auto-generated, always-current spec | Module exists, integration unclear |
| Client SDKs | TypeScript API client in `ui/src/utils/api.ts` | SDKs for Python, Go, Node.js, etc. | Only internal TS client |

**Priority**: P1

**Effort**: 3-4 weeks

**Breakdown**:
- Global rate limiting with Redis: 1 week
- API key management (issuance, rotation, revocation): 1 week
- Per-tier throttling: 0.5 week
- OpenAPI spec generation and publishing: 0.5-1 week
- One additional SDK (Go or Python): 1 week

### 2G. Module Marketplace

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Module catalog | 70+ built-in types in `engine.go` switch statement | Searchable registry with metadata | Hardcoded in engine |
| Custom modules | Dynamic components via Yaegi (hot-reload) | Publish/discover/install from marketplace | No marketplace infrastructure |
| Version management | None for modules | Semantic versioning per marketplace module | Not built |
| Security review | Sandbox validation (stdlib-only imports) | Automated + manual review for community modules | Sandbox exists, review process does not |
| Revenue sharing | None | Payment splits for module authors | Not built |
| Templates | 37 example YAML configs in `example/` | Template gallery with one-click deploy | Examples exist but no gallery UI |

**Priority**: P2

**Effort**: 6-8 weeks

### 2H. Backup, Recovery & Disaster Recovery

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Database backup | SQLite file (manual copy) | Automated per-tenant backups | None |
| Point-in-time recovery | None | Restore to any point within retention window | Not built |
| Cross-region replication | Single location | At least 2 regions | Not built |
| Failover automation | None | Automatic failover with health detection | Not built |
| Data export | `GET /workflows/{id}` returns config_yaml | Full tenant data export (GDPR compliance) | Partial (workflow configs only) |

**Priority**: P1

**Effort**: 3-4 weeks

### 2I. CI/CD Integration

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| Git integration | None | Push-to-deploy from Git repository | Not built |
| Webhook support | `webhook.sender` module type exists | Inbound webhooks for CI/CD triggers | Outbound only |
| Preview environments | None | Deploy PR branches to ephemeral environments | Not built |
| Config validation | `POST /admin/engine/validate` exists | Pre-deploy validation in CI pipeline | API exists, no CI integration |
| Rollback | Version history exists | One-click rollback to previous version | Version snapshots exist, rollback API does not |

**Priority**: P1

**Effort**: 3-4 weeks

**Existing advantage**: Workflow versioning with full config_yaml snapshots per version
is already implemented. Adding a `POST /workflows/{id}/rollback/{version}` endpoint
is straightforward.

### 2J. Documentation & Developer Experience

| Aspect | Current State | Required for SaaS | Gap |
|--------|--------------|-------------------|-----|
| API documentation | `docs/API.md`, `docs/openapi.yaml` | Interactive API explorer (Swagger UI) | Static docs only |
| Module reference | `schema/module_schema.go` with field definitions | Per-module docs with examples, use cases | Schema exists, prose docs sparse |
| Tutorials | `docs/tutorials/` directory exists | Video tutorials, interactive guides | Text-only tutorials |
| Example apps | 37 YAML configs in `example/` | Template gallery with one-click deploy | Files exist, no gallery UI |
| Community | None | Forum, Discord, GitHub Discussions | Not built |
| Onboarding docs | `docs/MODULE_BEST_PRACTICES.md` | Getting-started guide for new customers | Developer-focused, not user-focused |

**Priority**: P1

**Effort**: 4-6 weeks

---

## 3. Implementation Roadmap

### Phase 1: Foundation (Weeks 1-8)

**Goal**: Core SaaS infrastructure that enables paid customers.

**Update (Feb 2026)**: Pipeline-native routes are now complete, proving the engine can
express full CRUD APIs in YAML. This eliminates the need for custom Go handlers for
basic customer workloads and accelerates Phase 1 -- the engine's feature completeness
is demonstrated, so SaaS infrastructure work can begin immediately. Phase 1 is
reduced from 10 weeks to 8 weeks because deployment is simpler when customer
workloads are pure YAML configurations rather than custom Go binaries.

| Work Item | Priority | Effort | Dependencies |
|-----------|:--------:|:------:|:------------:|
| PostgreSQL migration (replace SQLite) | P0 | 1 week | None |
| Distributed state machine with advisory locks | P0 | 2 weeks | PostgreSQL |
| Dockerfile + container build pipeline | P0 | 1 week | None |
| Deployment API (config -> container -> run) | P0 | 1-2 weeks | Dockerfile (simpler now: deploy YAML configs, not custom binaries) |
| Stripe subscription integration | P0 | 1-2 weeks | None |
| Usage metering pipeline | P0 | 1 week | Stripe |
| Tier limit enforcement in engine | P0 | 0.5 weeks | Metering |
| Email verification + password reset | P0 | 1 week | None |
| Basic tenant resource quotas | P0 | 0.5 weeks | PostgreSQL |

**Milestone**: First paying customer can sign up, create a workflow using pipeline-native
YAML steps, deploy it, and be billed based on usage -- all without writing Go code.

### Phase 2: Scale & Security (Weeks 11-20)

**Goal**: Handle production workloads with baseline compliance.

| Work Item | Priority | Effort | Dependencies |
|-----------|:--------:|:------:|:------------:|
| NATS/Kafka production messaging validation | P0 | 1-2 weeks | Phase 1 |
| Kubernetes operator (basic) | P0 | 3-4 weeks | Dockerfile |
| SOC 2 readiness (audit controls, encryption) | P1 | 2-3 weeks | PostgreSQL |
| Team invitation system | P1 | 1 week | None |
| Guided onboarding wizard | P1 | 1 week | Teams |
| API key management | P1 | 1 week | None |
| Global rate limiting (Redis) | P1 | 1 week | None |
| Per-tier throttling | P1 | 0.5 weeks | Rate limiting, tiers |

**Milestone**: Platform handles multiple concurrent tenants with proper isolation,
teams can collaborate, and rate limits prevent abuse.

### Phase 3: Enterprise Features (Weeks 21-30)

**Goal**: Enterprise-ready platform with compliance and reliability.

| Work Item | Priority | Effort | Dependencies |
|-----------|:--------:|:------:|:------------:|
| SSO/SAML production hardening | P1 | 2 weeks | IAM framework |
| Multi-region deployment | P1 | 2-3 weeks | K8s operator |
| Automated backup and DR | P1 | 2 weeks | PostgreSQL |
| CI/CD integration (GitHub Actions, GitLab) | P1 | 2 weeks | API keys |
| Rollback API | P1 | 0.5 weeks | Versioning |
| Advanced monitoring and alerting | P1 | 1-2 weeks | Prometheus |
| SLA management dashboard | P1 | 1 week | Monitoring |

**Milestone**: Enterprise customers can sign contracts with SLAs, use SSO, deploy
across regions, and integrate with their CI/CD pipelines.

### Phase 4: Growth & Ecosystem (Weeks 31-40+)

**Goal**: Platform ecosystem that drives organic growth.

| Work Item | Priority | Effort | Dependencies |
|-----------|:--------:|:------:|:------------:|
| Module marketplace infrastructure | P2 | 3-4 weeks | Phase 1 |
| Template gallery with one-click deploy | P2 | 2 weeks | Marketplace |
| Community portal (forum/Discord) | P2 | 1 week | None |
| SDK generation (Go, Python, Node.js) | P2 | 2-3 weeks | OpenAPI spec |
| Interactive documentation portal | P2 | 2 weeks | SDKs |
| Partner integrations | P2 | 2-3 weeks | Marketplace |

**Milestone**: Developers discover the platform through templates and community,
extend it through the marketplace, and integrate it through SDKs.

---

## 4. Architecture Decision: Hosted vs. Customer-Hosted

### Model A: Fully Hosted (Heroku/Vercel Model)

You run everything: control plane (web UI, API, billing) and data plane (workflow
engine instances, databases, message brokers).

| Aspect | Assessment |
|--------|-----------|
| Build complexity | Lower -- no deployment agent needed |
| Time to MVP | Faster by 3-4 weeks |
| Billing enforcement | Full control (engine runs on your infra) |
| Infrastructure cost | Higher (you pay for all compute) |
| Data residency | Problematic for enterprise (data in your cloud) |
| Margin | Lower at scale (compute costs eat into margin) |
| Scaling | You manage capacity for all tenants |

### Model B: Customer-Hosted Data Plane (GitLab/Temporal Cloud Model)

You host the control plane (web UI, API, billing). Customers host the data plane
(workflow engine on their infrastructure via a deployment agent).

| Aspect | Assessment |
|--------|-----------|
| Build complexity | Higher -- requires deployment agent (~20MB binary) |
| Time to MVP | Slower by 3-4 weeks |
| Billing enforcement | Harder (agent reports usage, customer could tamper) |
| Infrastructure cost | Lower (customers pay for their own compute) |
| Data residency | Solved (data stays in customer's VPC) |
| Margin | Higher at scale (minimal infra costs) |
| Scaling | Customers manage their own capacity |

### Hybrid Model C: Hosted Default + Customer-Hosted Option

Start with fully hosted (Model A) for Free and Pro tiers. Offer customer-hosted
(Model B) for Business and Enterprise tiers.

| Tier | Data Plane | Rationale |
|------|-----------|-----------|
| Free | Fully hosted (shared) | Lowest friction to try |
| Pro | Fully hosted (dedicated) | Simple, pay and use |
| Business | Customer choice | Some want hosted, some want VPC |
| Enterprise | Customer-hosted (required) | Data residency, compliance |

### Recommendation

**Start with Model A (fully hosted) for MVP.** Rationale:

1. Eliminates the deployment agent requirement (saves 3-4 weeks)
2. Full billing enforcement -- no trust issues with usage reporting
3. Simpler debugging -- you can access all logs and metrics directly
4. The existing architecture already describes this in `APPLICATION_LIFECYCLE.md`

Add Model B (customer-hosted) in Phase 3 when enterprise customers demand data
residency. The architecture documented in `APPLICATION_LIFECYCLE.md` already
outlines the agent-based deployment model with WebSocket/gRPC communication,
license key validation, and config encryption -- so the design work is done.

---

## 5. Competitive Landscape

| Platform | Model | Pricing | Differentiator | Workflow's Advantage |
|----------|-------|---------|----------------|---------------------|
| **Zapier/Make** | No-code automation | $20-$800/mo | 5000+ app connectors, trigger-action simplicity | Full application platform vs. integration glue; state machines, databases, auth built-in; not limited to "connect A to B" |
| **AWS Step Functions** | Cloud workflow orchestration | $0.025/1000 transitions | Deep AWS integration, JSON state language | Not cloud-locked; YAML over JSON; visual builder included; runs anywhere (K8s, bare metal, any cloud) |
| **Temporal** | Workflow orchestration SDK | $200+/mo (Cloud) | Code-first (Go/Java/Python SDKs), durable execution | No code required -- pipeline-native YAML replaces SDK boilerplate; YAML-driven vs. code-first |
| **n8n** | Visual workflow automation | $20-$299/mo + self-hosted | 400+ integrations, visual node editor | State machine + module composition vs. simple trigger-action; pipeline-native CRUD APIs go beyond workflow automation |
| **Railway/Render** | Generic PaaS | $5-$20/service/mo | Deploy any container | Workflow primitives (state machines, event buses, middleware chains, pipeline-native API routes) built in; not generic container hosting |
| **Windmill** | Script-based workflows | $10-$40/user/mo | Multi-language scripts, approval flows | Declarative config vs. imperative scripts; 70+ built-in module types; no scripting needed for CRUD APIs |

### Workflow's Core Differentiators

1. **Declarative YAML configuration**: No code required for HTTP APIs, state machines,
   middleware chains, event-driven architectures, and scheduled workflows. Change
   behavior by editing YAML, not redeploying code.

2. **Pipeline-native API routes**: Build complete CRUD APIs entirely in YAML using
   declarative step sequences (request_parse -> db_query -> json_response). No other
   platform combines config-driven development with a full module ecosystem AND a
   visual builder AND pipeline-native data operations.

3. **70+ built-in module types**: HTTP server, auth (JWT, OAuth2, SAML), state
   machines, messaging (NATS, Kafka), database, caching, rate limiting, metrics,
   reverse proxy, dynamic components, AI integration, and more -- all configurable
   via YAML.

4. **Visual builder + YAML bidirectionality**: Design visually in the ReactFlow
   canvas or write YAML directly. Both representations stay in sync.

5. **State machine as a first-class primitive**: Most workflow platforms handle
   simple trigger-action patterns. Workflow supports full state machine definitions
   with transitions, guards, and compensation -- critical for business processes.

6. **Dynamic hot-reload**: Update business logic (Go components) at runtime without
   restarting the engine. No other workflow platform offers sub-100ms hot-reload
   of custom code.

7. **Template engine with built-in functions**: Pipeline steps use a template engine
   with functions like `uuidv4`, `now`, `lower`, `default`, `json` -- enabling
   dynamic behavior without code.

8. **Modular framework**: Built on the modular dependency injection framework,
   providing proper service registry, lifecycle management, multi-tenancy support,
   and configuration management -- not a monolith with plugins bolted on.

### Key Differentiator Summary

Workflow is the **only platform that combines config-driven development with a full
module ecosystem AND a visual builder**. Zapier and Make are integration glue, not
application platforms. AWS Step Functions are cloud-locked and JSON-only. Temporal
requires SDK code for everything. n8n is limited to workflow automation. Railway and
Render are generic PaaS with no workflow primitives. Workflow provides the complete
stack -- from HTTP routing to database operations to state machines -- all driven by
YAML configuration.

### Positioning

Workflow occupies a unique position: **more powerful than no-code workflow tools
(n8n, Zapier, Make), less complex than code-first orchestration (Temporal, AWS Step
Functions), more opinionated than generic PaaS (Railway, Render)**. It targets
developers who want architectural control without writing boilerplate, and teams
that want to change business logic without code deployments. With pipeline-native
routes, even database-backed CRUD APIs require zero code.

---

## SaaS Revenue Model Options

### Usage-Based Pricing

Charge customers based on actual consumption:

| Metric | Unit | Estimated Price | Notes |
|--------|------|:---------------:|-------|
| Workflow executions | Per execution | $0.001 - $0.01 | Based on step count and duration |
| API calls | Per 1,000 calls | $0.50 - $2.00 | Pipeline-native endpoints counted individually |
| Storage | Per GB/month | $0.10 - $0.50 | Database storage, config snapshots, audit logs |
| Compute time | Per vCPU-second | $0.00005 | For long-running workflows and state machines |
| Bandwidth | Per GB transferred | $0.05 - $0.15 | Outbound data transfer only |

**Pros**: Aligns cost with value; low barrier to entry; scales naturally with customer growth.
**Cons**: Revenue unpredictable; hard for customers to budget; requires robust metering infrastructure.

### Tier-Based Pricing

Fixed monthly plans with usage limits:

| Tier | Monthly Price | Executions | API Calls | Workflows | Support |
|------|:------------:|:----------:|:---------:|:---------:|---------|
| **Free** | $0 | 1,000 | 10,000 | 3 | Community only |
| **Pro** | $49 | 50,000 | 500,000 | 25 | Email (48h SLA) |
| **Business** | $299 | 500,000 | 5,000,000 | Unlimited | Priority (4h SLA) |
| **Enterprise** | Custom | Custom | Custom | Unlimited | Dedicated + phone |

**Pros**: Predictable revenue; easy for customers to understand; clear upgrade path.
**Cons**: May leave money on the table for heavy users; requires overage policy.

### Hybrid Model (Recommended)

Tier-based plans with usage-based overage:
- Each tier includes a base allocation of executions and API calls
- Overage billed per-unit at the tier's rate (lower tiers pay more per-unit overage)
- Storage and bandwidth billed usage-based across all tiers
- Enterprise plans negotiated individually

### Infrastructure Costs Per Tenant

| Resource | Free Tier | Pro Tier | Business Tier | Notes |
|----------|:---------:|:--------:|:-------------:|-------|
| Compute (vCPU) | 0.25 shared | 0.5 dedicated | 2.0 dedicated | Kubernetes pod resource limits |
| Memory | 256 MB shared | 512 MB | 2 GB | Engine + database connections |
| Database | Shared PostgreSQL | Shared with schema isolation | Dedicated PostgreSQL | Schema-per-tenant scales to ~500 tenants |
| Storage | 100 MB | 1 GB | 10 GB | Config, execution history, audit logs |
| Bandwidth | 1 GB/mo | 10 GB/mo | 100 GB/mo | Outbound only |
| **Estimated cost** | **~$2/mo** | **~$8/mo** | **~$25/mo** | Cloud provider dependent |

### Value Proposition

**"Build production APIs in YAML, deploy in minutes, scale automatically."**

With pipeline-native routes, the value proposition is concrete and demonstrable:

1. **No Go code required**: Customers define complete CRUD APIs using declarative YAML
   step sequences (request_parse -> db_query -> json_response)
2. **Minutes to production**: Upload a YAML config, deploy via API or visual builder,
   get a running application with auth, database, and monitoring included
3. **Automatic scaling**: The platform handles horizontal scaling, load balancing, and
   failover -- customers focus on business logic
4. **Built-in everything**: Authentication, authorization, rate limiting, metrics,
   health checks, audit logging -- all configured via YAML, not coded

---

## 6. Revenue Projections (Illustrative)

### Pricing Tiers

Based on the tiers defined in `APPLICATION_LIFECYCLE.md`:

| Tier | Monthly Price | Included Executions | Overage Rate | Target Customer |
|------|:------------:|:-------------------:|:------------:|----------------|
| Free | $0 | 1,000 | N/A (hard limit) | Evaluation, hobby projects |
| Pro | $49 | 50,000 | $0.001/exec | Small teams, startups |
| Business | $299 | 500,000 | $0.0005/exec | Growing companies |
| Enterprise | Custom | Custom | Negotiated | Large organizations |

### Year 1 Revenue Model

Assumptions: Launch MVP at month 3, organic growth via content marketing and
community, no enterprise sales team.

| Month | Free Users | Pro | Business | Enterprise | MRR |
|:-----:|:----------:|:---:|:--------:|:----------:|----:|
| 3 | 20 | 2 | 0 | 0 | $98 |
| 6 | 60 | 8 | 1 | 0 | $691 |
| 9 | 120 | 15 | 3 | 0 | $1,632 |
| 12 | 200 | 25 | 5 | 1 | $3,720+ |

**Year 1 target**: ~$3,700 MRR (~$44K ARR) by month 12, excluding enterprise
contracts and overage revenue.

### Unit Economics

| Metric | Estimate | Notes |
|--------|:--------:|-------|
| Infrastructure cost per Free user | ~$2/mo | Shared hosting, limited executions |
| Infrastructure cost per Pro user | ~$8/mo | Dedicated resources, higher execution volume |
| Infrastructure cost per Business user | ~$25/mo | More compute, more storage |
| Gross margin (Pro) | ~84% | $49 - $8 = $41 margin |
| Gross margin (Business) | ~92% | $299 - $25 = $274 margin |
| CAC target | <$200 | Content-driven, community-driven |
| LTV:CAC ratio target | >3:1 | Standard SaaS benchmark |

### Break-Even Analysis

Assuming $5,000/mo in fixed costs (hosting, domains, monitoring, email service):

- **Break-even**: ~20 Pro customers + 2 Business customers = $1,578 + $598 = ~$2,176
  (not yet break-even at this mix)
- **Actual break-even**: ~40 Pro + 5 Business = $1,960 + $1,495 = ~$3,455
  (covers fixed costs with margin)

The path to profitability depends heavily on landing Business-tier and Enterprise
customers. Free-tier users are a funnel, not a revenue source.

---

## 7. Key Risks

### Technical Risks

| Risk | Impact | Likelihood | Mitigation |
|------|:------:|:----------:|-----------|
| SQLite to PostgreSQL migration breaks existing deployments | High | Medium | Dual-driver support already in `database.workflow`; migration tooling required |
| Distributed state machine introduces race conditions | High | Medium | Extensive testing with `SELECT ... FOR UPDATE`; the approach is well-documented in `APPLICATION_LIFECYCLE.md` |
| In-memory user store doesn't scale | Medium | High | Persistence write-through exists but needs PostgreSQL backend for multi-instance |
| Dynamic component sandbox escape | Critical | Low | Yaegi sandbox already validates stdlib-only imports; add additional security layers |
| Hot-reload causes data loss during config update | High | Medium | 200-500ms restart window; mitigate with rolling deploys behind load balancer |

### Business Risks

| Risk | Impact | Likelihood | Mitigation |
|------|:------:|:----------:|-----------|
| **Single maintainer risk** | Critical | High | If the project is solo-developed, bus factor = 1. Mitigate by documenting everything, keeping architecture simple, and hiring/partnering early |
| **Go-only dynamic components** | Medium | Medium | Reduced risk: pipeline-native routes (step.request_parse, step.db_query, step.db_exec, step.json_response) now allow CRUD APIs without Go code. Go is only needed for truly custom logic. Further mitigate with JavaScript/Python runtimes via Wasm or subprocess |
| **Market education** | Medium | High | Explaining YAML-driven workflows vs. code-first alternatives requires clear positioning and demo content. Mitigate: video demos, template gallery, comparison pages |
| **Enterprise sales cycle** | High | High | Enterprise deals take 3-6 months. High MRR but slow to close. Mitigate: focus on self-service Pro/Business tiers for predictable revenue, add enterprise later |
| **Competitor response** | Medium | Medium | n8n, Temporal, or new entrants could build similar declarative config features. Mitigate: move fast, build community, focus on differentiators (state machine, hot-reload, module ecosystem) |
| **Pricing too low** | Medium | Medium | $49/mo for Pro may undervalue the platform. Validate with early customers and adjust. Usage-based overage provides upside |
| **Free tier abuse** | Low | Medium | Free users consuming disproportionate resources. Mitigate: hard execution limits, no custom domains, shared hosting |

### Operational Risks

| Risk | Impact | Likelihood | Mitigation |
|------|:------:|:----------:|-----------|
| Platform outage affects all tenants | Critical | Medium | Multi-region deployment, automated failover, incident response runbook |
| Data breach / security incident | Critical | Low | Tenant isolation, encryption at rest, SOC 2 controls, security audit |
| Billing errors (over/under-charging) | High | Medium | Reconciliation checks, usage audit trail, customer-facing usage dashboard |
| Support burden exceeds capacity | Medium | High | Self-service docs, community forum, tiered support (Free: community only) |

---

## Appendix A: Existing Code Inventory for SaaS

Files and packages that form the SaaS foundation:

| File/Package | SaaS Relevance | Reuse Assessment |
|-------------|---------------|-----------------|
| `module/api_v1_handler.go` | Multi-tenant API routing | Direct reuse; extend with billing endpoints |
| `module/api_v1_store.go` | Tenant data model (companies, orgs, projects, workflows) | Direct reuse; migrate from SQLite to PostgreSQL |
| `module/auth_user_store.go` | User management with bcrypt, persistence | Needs PostgreSQL backend; in-memory won't scale |
| `module/auth_middleware.go` | Pluggable auth provider chain | Direct reuse; add API key provider |
| `module/jwt_auth.go` | JWT token issuance and validation | Direct reuse; add email verification claims |
| `ui/src/utils/api.ts` | 60+ endpoint client | Direct reuse; add billing and team endpoints |
| `ui/src/types/observability.ts` | Execution, step, log, audit types | Direct reuse |
| `schema/module_schema.go` | Module configuration schemas | Direct reuse; basis for marketplace metadata |
| `engine.go` (70+ case statements) | Module factory registry | Direct reuse; add marketplace module loader |
| `dynamic/` | Hot-reload via Yaegi | Direct reuse; key differentiator |
| `example/` (37 YAML files) | Template gallery content | Direct reuse as starter templates |

## Appendix B: API Endpoints Needed for SaaS (Not Yet Built)

| Endpoint | Purpose | Phase |
|----------|---------|:-----:|
| `POST /api/v1/auth/verify-email` | Email verification | 1 |
| `POST /api/v1/auth/forgot-password` | Password reset request | 1 |
| `POST /api/v1/auth/reset-password` | Password reset completion | 1 |
| `POST /api/v1/billing/subscribe` | Create Stripe subscription | 1 |
| `GET /api/v1/billing/subscription` | Get current plan | 1 |
| `PUT /api/v1/billing/subscription` | Change plan | 1 |
| `GET /api/v1/billing/usage` | Current period usage vs. quota | 1 |
| `GET /api/v1/billing/invoices` | Invoice history | 1 |
| `POST /api/v1/billing/webhook` | Stripe webhook receiver | 1 |
| `POST /api/v1/teams/invite` | Send team invitation | 2 |
| `POST /api/v1/teams/accept` | Accept invitation | 2 |
| `GET /api/v1/teams/members` | List team members | 2 |
| `DELETE /api/v1/teams/members/{id}` | Remove team member | 2 |
| `POST /api/v1/api-keys` | Create API key | 2 |
| `GET /api/v1/api-keys` | List API keys | 2 |
| `DELETE /api/v1/api-keys/{id}` | Revoke API key | 2 |
| `POST /api/v1/workflows/{id}/rollback/{version}` | Rollback to version | 3 |
| `POST /api/v1/deployments` | Trigger deployment | 1 |
| `GET /api/v1/deployments/{id}` | Deployment status | 1 |
| `GET /api/v1/deployments/{id}/logs` | Deployment logs | 1 |
| `GET /api/v1/marketplace/modules` | Browse marketplace | 4 |
| `POST /api/v1/marketplace/modules` | Publish module | 4 |
| `GET /api/v1/templates` | Browse templates | 4 |
| `POST /api/v1/templates/{id}/deploy` | One-click template deploy | 4 |
