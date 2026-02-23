# Operationalization Plan: Layered Approach for Workflow Engine

## Overview

This document is the master plan for operationalizing the GoCodeAlone/workflow engine across three modes of operation: **SaaS/PaaS**, **Self-Hosted**, and **Embedded** (like GoCodeAlone/ratchet). The approach is organized into five layers, each building on the one below.

## Architecture Layers

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Layer 5: Applications                                                    │
│  Ratchet, SaaS control plane, customer apps — consume everything below   │
├──────────────────────────────────────────────────────────────────────────┤
│  Layer 4: Platform Services  (workflow-cloud, private)                    │
│  Multi-tenancy, billing, license validation, SaaS control plane          │
├──────────────────────────────────────────────────────────────────────────┤
│  Layer 3: Plugin Ecosystem  (registry repo + marketplace)                │
│  Community plugins, premium plugins, UI plugins, external go-plugin      │
├──────────────────────────────────────────────────────────────────────────┤
│  Layer 2: Developer Tooling  (wfctl, SDKs, templates)                    │
│  CLI, project scaffolding, CI helpers, shared UI library                 │
├──────────────────────────────────────────────────────────────────────────┤
│  Layer 1: Core Engine  (GoCodeAlone/workflow, public)                    │
│  Modules, plugins, pipelines, config, static.fileserver, go-plugin       │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## Layer 1: Core Engine (`GoCodeAlone/workflow` — public)

The open-source workflow engine library and server binary. Everything in the `workflow` repo today, stabilized and released with semver.

### Current State (as of v0.1.5)

- 48+ module types, config-driven app building, visual editor, external plugin system (go-plugin/gRPC), deployment strategies (rolling, blue-green, canary), Helm chart, Prometheus/Grafana dashboards
- Semver releases: v0.1.0 through v0.1.5 tagged with CI/CD automation
- Admin UI decoupled from binary, served via `static.fileserver`
- CI: `ci.yml`, `release.yml`, `pre-release.yml`, `helm-lint.yml`, `osv-scanner.yml`, `dependency-update.yml`
- MIT License

### Actions

| # | Action | Detail | Status |
|---|--------|--------|--------|
| 1.1 | **Semver releases with GitHub Actions** | `release.yml` runs tests + lint, builds admin UI artifact, creates GitHub Release. `pre-release.yml` creates snapshot builds on main merges. | Done (v0.1.5) |
| 1.2 | **Remove `replace` directive from Ratchet** | Ratchet uses `require github.com/GoCodeAlone/workflow v0.1.5`. | Done |
| 1.3 | **Admin UI decoupling** | Issue #34. Admin UI served via `static.fileserver`, no longer embedded via `go:embed`. | Done |
| 1.4 | **Plugin-driven admin navigation** | Issue #35. Navigation fully plugin-driven, no hardcoded pages. | Done |
| 1.5 | **UI plugin hot-reload** | Issue #38. go-plugin based UI plugin system implemented (PR #45). | Done |
| 1.6 | **API stability contract** | `docs/API_STABILITY.md` documents public vs internal packages, semver policy, deprecation policy, embedding contract. | Done |
| 1.7 | **License** | MIT License. Enterprise features gated at Layer 4. | Done |

### Deliverables

- `github.com/GoCodeAlone/workflow` as a versioned Go module (`v0.1.5`)
- `workflow-server` binary downloadable from GitHub Releases
- `workflow-admin-ui` tarball as a separate release artifact
- Docker image: `ghcr.io/gocodalone/workflow:<version>`

---

## Layer 2: Developer Tooling

Everything a software engineer needs to build, test, and deploy a workflow application.

### 2a. `wfctl` CLI (at `cmd/wfctl/`)

| Command | Purpose | Status |
|---------|---------|--------|
| `wfctl init` | Scaffold a new workflow application project | Done (5 templates) |
| `wfctl validate` | Validate workflow YAML config | Done |
| `wfctl run` | Run a workflow locally | Done |
| `wfctl build-ui` | Build UI assets, validate output, package | Done (#37) |
| `wfctl plugin init` | Scaffold a new plugin project | Done |
| `wfctl plugin docs` | Generate documentation for a plugin | Done |
| `wfctl plugin test` | Run plugin in-process test mode | **TODO** |
| `wfctl publish` | Package and push a plugin to the registry | Done |
| `wfctl deploy` | Deploy to target environment | **TODO** |
| `wfctl inspect` | Inspect workflow config details | Done |
| `wfctl schema` | Generate/view module schemas | Done |
| `wfctl manifest` | Manage plugin manifests | Done |
| `wfctl migrate` | Run config migrations | Done |

### 2b. Shared UI Component Library (`@gocodealone/workflow-ui`)

Issue #36. Published to GitHub Packages (`npm.pkg.github.com`):

| Export Path | Contents |
|-------------|----------|
| `@gocodealone/workflow-ui` | Main exports |
| `@gocodealone/workflow-ui/auth` | Login, JWT management, protected routes |
| `@gocodealone/workflow-ui/api` | Typed API client for engine endpoints |
| `@gocodealone/workflow-ui/sse` | Server-Sent Events utilities |
| `@gocodealone/workflow-ui/theme` | Theme/styling exports |

### 2c. Application UI Build/Serve Contract

Issue #37, `docs/APPLICATION_UI_CONTRACT.md`:
- SPA framework builds to `dist/`
- `static.fileserver` serves with SPA fallback
- Vite dev proxy for development
- Optional `go:embed` for single-binary distribution (app-level choice, not engine-forced)

### 2d. Project Templates

Shipped with `wfctl init`:

| Template | Description |
|----------|-------------|
| `api-service` | HTTP API with auth, database, pipelines |
| `event-processor` | Messaging broker, event handling, state machine |
| `full-stack` | API + React UI + database + auth |
| `plugin` | External go-plugin project scaffold |
| `ui-plugin` | UI plugin with React, hot-reload, go-plugin bridge |

---

## Layer 3: Plugin Ecosystem

A registry and marketplace for discovering, distributing, and managing workflow plugins.

### 3a. Plugin Registry Repo (`GoCodeAlone/workflow-registry` — public)

Live at [GoCodeAlone/workflow-registry](https://github.com/GoCodeAlone/workflow-registry) with 18 plugin manifests, 5 templates, JSON schema.

```
workflow-registry/
├── plugins/
│   ├── ai/manifest.json
│   ├── api/manifest.json
│   ├── auth/manifest.json
│   ├── bento/manifest.json
│   ├── cicd/manifest.json
│   ├── featureflags/manifest.json
│   ├── http/manifest.json
│   ├── integration/manifest.json
│   ├── messaging/manifest.json
│   ├── modularcompat/manifest.json
│   ├── observability/manifest.json
│   ├── pipelinesteps/manifest.json
│   ├── platform/manifest.json
│   ├── scheduler/manifest.json
│   ├── secrets/manifest.json
│   ├── statemachine/manifest.json
│   └── storage/manifest.json
├── templates/
│   ├── api-service.yaml
│   ├── event-processor.yaml
│   ├── full-stack.yaml
│   ├── microservice-template.yaml
│   └── plugin-template.yaml
└── schema/
    └── registry-schema.json
```

### 3b. Registry API Service

Built in `workflow-cloud` (dogfooding the workflow engine). Serves 4 endpoints matching the `RemoteRegistry` client contract:

| Endpoint | Purpose | Status |
|----------|---------|--------|
| `GET /api/v1/plugins?q={query}` | Search plugins by name/keywords/description | Done |
| `GET /api/v1/plugins/{name}/versions/{version}` | Get specific plugin manifest | Done |
| `GET /api/v1/plugins/{name}/versions` | List available versions | Done |
| `GET /api/v1/plugins/{name}/versions/{version}/download` | Download plugin archive | Done |

### 3c. Plugin Tiers

| Tier | Examples | License | Distribution |
|------|----------|---------|-------------|
| **Core** | http, auth, messaging, storage, api, pipeline, scheduler | MIT (ships with engine) | Built into `workflow-server` binary |
| **Community** | Custom module types, steps, integrations | MIT/Apache (author's choice) | Registry, go-plugin binaries |
| **Premium** | K8s operator, multi-region routing, advanced analytics, SSO | Commercial (requires license key) | Registry with gated download |

### 3d. Premium Plugin Gating

Premium plugins (go-plugin binaries) on load:
1. Call `EngineCallbackService.GetService("license-validator")`
2. License validator checks customer tier against platform API
3. If tier doesn't include the plugin, `Init()` returns error and plugin refuses to start
4. Uses the license validation pattern designed in `docs/APPLICATION_LIFECYCLE.md`

---

## Layer 4: Platform Services (`GoCodeAlone/workflow-cloud` — private)

The commercial layer: multi-tenancy, billing, license management, SaaS control plane. Built as a workflow application (dogfooding the engine) following the ratchet pattern.

### 4a. SaaS Control Plane

Repository: `GoCodeAlone/workflow-cloud` (private). Architecture: minimal Go bootstrap (~120 lines) + YAML config + custom `CloudPlugin` EnginePlugin.

| Feature | Implementation | Status |
|---------|---------------|--------|
| Registry API | `cloudplugin/step_registry_*.go` — search, get, versions, download | Done |
| License validation API | `cloudplugin/step_license_validate.go` — `POST /validate` | Done |
| Tenant provisioning | `cloudplugin/step_tenant_provision.go` — creates tenant + license key | Done |
| Stripe billing webhook | `cloudplugin/step_stripe_webhook.go` — subscription lifecycle | Done |
| Usage reporting | `cloudplugin/step_usage_report.go` — quota tracking | Done |
| DB migrations | `cloudplugin/hook_db_init.go` — PostgreSQL schema management | Done |
| Registry sync | `cloudplugin/hook_registry_sync.go` — GitHub archive download + indexing | Done |
| Per-tenant engine deployment | `WorkflowEngineManager` with `ModuleNamespace` for tenant isolation | **TODO** |
| Visual designer | `@gocodealone/workflow-ui/editor` shared component | **TODO** (SaaS UI) |
| Execution monitoring | `@gocodealone/workflow-ui/observability` shared component | **TODO** (SaaS UI) |
| Plugin marketplace | Proxies registry, adds install/enable/disable per tenant | **TODO** (SaaS UI) |
| SSO/OAuth | OAuth2 (GitHub, Google, OIDC) — enterprise feature | **TODO** (SaaS UI) |

### 4b. License Server

```
Customer Engine ──→ POST /validate ──→ workflow-cloud License Server
                                              │
                             ┌────────────────┤
                             │                │
                       Check tier        Check usage
                       limits            quotas
                             │                │
                             └───────┬────────┘
                                     │
                               Return: valid/invalid,
                               tier, remaining quota
```

Enforcement:
- Won't start more workflows than tier allows
- Won't execute if quota exhausted
- Graceful degradation (reject new, keep existing running)
- Periodic phone-home (hourly), cached locally with TTL for offline tolerance

### 4c. Self-Hosted Distribution

- Same `workflow-server` binary from Layer 1
- License key at startup (`--license-key` or `WORKFLOW_LICENSE_KEY` env)
- Engine includes `license.validator` module that phones home
- Premium plugins require matching tier
- Offline operation with cached license (configurable grace period)

### 4d. Deployment Infrastructure

| Component | Location | Status |
|-----------|----------|--------|
| OpenTofu modules (VPC, ECS, RDS, ALB, ECR, monitoring) | `workflow/deploy/tofu/` | Done |
| Helm chart for workflow-cloud | `workflow-cloud/deploy/helm/` | Done |
| Docker Compose for local dev | `workflow-cloud/deploy/docker-compose.yml` | Done |
| CI/CD pipelines | `workflow-cloud/.github/workflows/ci.yml`, `deploy.yml` | Done |
| OpenTofu for workflow-cloud specifically | `workflow-cloud/infra/` | **TODO** |

---

## Layer 5: Applications

Each mode of operation is a different configuration of the layers below.

### Mode: SaaS

```
User ──→ SaaS UI (Layer 4) ──→ Control Plane API ──→ WorkflowEngineManager
              │                                              │
    @gocodealone/workflow-ui                   Deploy isolated engine per tenant
              │                                              │
         Registry API (Layer 3)                    Each engine loads tenant YAML
              │                                    + tenant's enabled plugins
         Plugin marketplace
```

### Mode: Self-Hosted

```
Customer ──→ workflow-server binary (Layer 1)
                    │
              workflow.yaml (their config)
                    │
              Admin UI (Layer 2 artifact)
                    │
              Plugins from registry (Layer 3)
                    │
              License key → License Server (Layer 4)
```

### Mode: Embedded (Ratchet-style)

```
Ratchet binary
    ├── go.mod: require github.com/GoCodeAlone/workflow v0.1.5
    ├── ratchetplugin/       (custom Go plugin)
    ├── ratchet.yaml         (workflow config)
    ├── ui/                  (custom React UI using @gocodealone/workflow-ui)
    └── cmd/ratchetd/main.go (thin bootstrap, ~100 lines)
```

---

## Repo Strategy

| Repo | Visibility | Purpose | Status |
|------|-----------|---------|--------|
| `GoCodeAlone/workflow` | Public | Core engine, admin UI, CLI, plugins | v0.1.5 released |
| `GoCodeAlone/workflow-ui` | Public | Shared UI component library (npm) | Published |
| `GoCodeAlone/workflow-registry` | Public | Plugin/template registry (git-based) | Live, 18 plugins |
| `GoCodeAlone/workflow-plugin-bento` | Public | Bento messaging plugin | v0.1.5 dep |
| `GoCodeAlone/ratchet` | Public | AI agent platform — embedded workflow application | v0.1.5 dep |
| `GoCodeAlone/workflow-cloud` | Private | SaaS control plane, billing, license server | Built, API functional |

---

## Implementation Phases

### Phase 1: Stabilize Core (Weeks 1-3) — COMPLETE

- [x] Semver releases with CI/CD for workflow repo — `release.yml` enhanced with admin UI artifact, `pre-release.yml` added for snapshot builds on main merges. v0.1.5 tagged.
- [x] API stability contract documentation — `docs/API_STABILITY.md` documents public vs internal packages, semver policy, deprecation policy, embedding contract.
- [x] Remove `replace` directive from Ratchet, switch to versioned import — Ratchet updated to `workflow v0.1.5`.

### Phase 2: UI Overhaul (Weeks 1-6) — COMPLETE

- [x] #34: Decouple admin UI from Go binary — completed, admin UI served via `static.fileserver`
- [x] #35: Plugin-driven admin navigation — completed, navigation fully plugin-driven
- [x] #36: Extract shared UI component library — completed, `@gocodealone/workflow-ui` published to GitHub Packages
- [x] #37: Application UI build/serve contract — `wfctl build-ui` command implemented, `docs/APPLICATION_UI_CONTRACT.md` created
- [x] #38: go-plugin UI hot-reload — implemented in PR #45
- [x] #39: Cleanup legacy UI scaffolding — removed stale `module/ui_dist/`, updated deployment docs, legacy embed references removed

### Phase 3: Developer Tooling (Weeks 3-6) — IN PROGRESS

- [x] `wfctl init` with project templates — 5 templates: api-service, event-processor, full-stack, plugin, ui-plugin
- [x] `wfctl plugin init` for plugin scaffolding — improved with template system
- [x] `wfctl build-ui` (tracked: #37) — framework detection, build, validate, copy, config snippet generation
- [x] Shared UI library npm publish (tracked: #36) — `@gocodealone/workflow-ui` on GitHub Packages
- [ ] `wfctl plugin test` — run plugin in isolated test mode with mock engine
- [ ] `wfctl deploy` — deploy workflow application to target environment (local Docker, Kubernetes, cloud)

### Phase 4: Plugin Registry (Weeks 5-8) — COMPLETE

- [x] Create `workflow-registry` repo with schema — [GoCodeAlone/workflow-registry](https://github.com/GoCodeAlone/workflow-registry) live with 18 plugin manifests, 5 templates, JSON schema
- [x] Build registry API service — implemented in `workflow-cloud` cloudplugin (search, get, versions, download endpoints)
- [x] `wfctl publish` command — auto-detection, validation, binary build, registry submission workflow
- [x] Plugin tier system (core/community/premium) — `PluginTier` type, tier validation in loader, all built-in plugins marked core

### Phase 5: Platform Services (Weeks 6-12) — IN PROGRESS

- [x] License server implementation — `license.validator` module with HTTP validation, caching, offline grace period, background refresh. Also `step.license_validate` in workflow-cloud.
- [x] Billing integration (Stripe, existing `billing/plans.go`) — Stripe provider, subscription management, enforcement middleware, webhook handling. Also `step.stripe_webhook` in workflow-cloud.
- [x] SaaS control plane application — `workflow-cloud` private repo built with registry API, license validation, tenant provisioning, Stripe webhook, usage reporting.
- [ ] Per-tenant workflow deployment via `WorkflowEngineManager` — engine has the capability, SaaS integration in workflow-cloud pending.
- [x] Premium plugin gating — integrated with tier system and license validator

### Phase 6: Infrastructure (Weeks 8-14) — IN PROGRESS

- [x] OpenTofu modules for AWS (VPC, ECS, RDS, ElastiCache, ALB, ECR, monitoring) — `workflow/deploy/tofu/` with dev/staging/production environments
- [x] CI/CD for workflow-cloud — `.github/workflows/ci.yml` (test + lint with PG service container) and `deploy.yml` (build, push to GHCR, deploy staging/production)
- [x] Staging and production environments — OpenTofu environments with appropriate instance sizing
- [x] Monitoring (Prometheus, Grafana, CloudWatch) — CloudWatch dashboard + alarms, Docker Compose with Prometheus/Grafana
- [ ] OpenTofu infrastructure for workflow-cloud — `workflow-cloud/infra/` with VPC, ECS, RDS, ElastiCache, ALB, CloudFront, ECR, monitoring modules

### Phase 7: Ratchet Migration (Weeks 4-6) — IN PROGRESS

- [x] Remove `replace` directive, use versioned workflow import — ratchet updated to `workflow v0.1.5`
- [x] Migrate UI to use `@gocodealone/workflow-ui` shared components — completed in Phase 2
- [ ] Publish `ratchetplugin` to registry — add `ratchet/manifest.json` to `workflow-registry`

---

## Remaining Work Summary

| # | Item | Layer | Effort |
|---|------|-------|--------|
| R1 | `wfctl plugin test` subcommand | Layer 2 | Medium — mock engine harness, plugin lifecycle test |
| R2 | `wfctl deploy` subcommand | Layer 2 | Medium — Docker/K8s/cloud deployment targets |
| R3 | Per-tenant engine deployment in workflow-cloud | Layer 4 | Large — WorkflowEngineManager integration, tenant isolation |
| R4 | OpenTofu infrastructure for workflow-cloud | Layer 4 | Medium — mirror `workflow/deploy/tofu/` patterns for cloud-specific infra |
| R5 | Publish ratchetplugin to registry | Layer 3 | Small — create manifest.json, add to workflow-registry |

---

## Stakeholder Value

| Stakeholder | What They Get |
|-------------|---------------|
| SaaS customers | Sign up, design workflows in browser, deploy instantly, pay per usage |
| Self-hosted customers | Download binary, write YAML, optionally buy license for premium plugins |
| Embedded developers | `go get` the engine, write Go plugins, build custom UI, single binary |
| Plugin authors | Scaffold with `wfctl plugin init`, test locally, publish to registry |
| UI contributors | Convention-based plugin directory, lazy loading, hot-reload via go-plugin |
| Platform operators | OpenTofu modules, Helm charts, Grafana dashboards, one-click environments |
| CTL (org) | Revenue from SaaS subscriptions, premium plugin licenses, enterprise support |

---

## Related Issues

- #34: Decouple Admin UI from Go Binary (Done)
- #35: Complete Plugin-Driven Admin UI Navigation (Done)
- #36: Extract Shared UI Component Library (Done)
- #37: Application UI Build/Serve Contract (Done)
- #38: go-plugin UI Hot-Reload (Done)
- #39: Cleanup Legacy UI Scaffolding (Done)
