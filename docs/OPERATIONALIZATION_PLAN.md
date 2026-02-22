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

### Current State

- 30+ module types, config-driven app building, visual editor, external plugin system (go-plugin/gRPC), deployment strategies (rolling, blue-green, canary), Helm chart, Prometheus/Grafana dashboards
- No tagged semver releases — Ratchet depends on `v0.0.0` with `replace ../workflow`
- Admin UI embedded in binary via `go:embed`
- CI exists (`ci.yml`, `release.yml`, `helm-lint.yml`, `osv-scanner.yml`, `dependency-update.yml`)

### Actions

| # | Action | Detail |
|---|--------|--------|
| 1.1 | **Semver releases with GitHub Actions** | Tag `v0.1.0` as baseline. Enhance `release.yml`: run tests + lint, build binaries (linux/darwin amd64/arm64), build admin UI, create GitHub Release with checksums. Every merge to `main` creates a pre-release; tag push creates stable release. |
| 1.2 | **Remove `replace` directive from Ratchet** | Once workflow has tagged releases, Ratchet's `go.mod` changes from `replace github.com/GoCodeAlone/workflow => ../workflow` to `require github.com/GoCodeAlone/workflow v0.x.y`. |
| 1.3 | **Admin UI decoupling** | Tracked: issue #34. Admin UI becomes standalone build artifact, not embedded via `go:embed`. |
| 1.4 | **Plugin-driven admin navigation** | Tracked: issue #35. Remove hardcoded `FALLBACK_PAGES`, `PLUGIN_VIEW_COMPONENTS`. |
| 1.5 | **UI plugin hot-reload** | Tracked: issue #38. go-plugin based UI plugin system. |
| 1.6 | **API stability contract** | Document which Go packages are public API (`config`, `plugin`, `module`, `schema`, `capability`, `store`) vs internal. Breaking changes require major version bump. |
| 1.7 | **License** | Engine stays open-source (MIT or Apache 2.0). Enterprise features gated at Layer 4. |

### Deliverables

- `github.com/GoCodeAlone/workflow` as a versioned Go module (e.g., `v0.1.0`)
- `workflow-server` binary downloadable from GitHub Releases
- `workflow-admin-ui` tarball as a separate release artifact
- Docker image: `ghcr.io/gocodalone/workflow:<version>`

---

## Layer 2: Developer Tooling

Everything a software engineer needs to build, test, and deploy a workflow application.

### 2a. `wfctl` CLI (already at `cmd/wfctl/`)

Current capabilities: `validate`, `run`, `serve`. Needs expansion:

| Command | Purpose | Status |
|---------|---------|--------|
| `wfctl init` | Scaffold a new workflow application project | New |
| `wfctl validate` | Validate workflow YAML config | Exists |
| `wfctl run` | Run a workflow locally | Exists |
| `wfctl build-ui` | Build UI assets, validate output, package | New (tracked: #37) |
| `wfctl plugin init` | Scaffold a new plugin project | New |
| `wfctl plugin test` | Run plugin in-process test mode | New |
| `wfctl publish` | Package and push a plugin to the registry | New (Layer 3) |
| `wfctl deploy` | Deploy to target environment | New (Layer 4 integration) |

### 2b. Shared UI Component Library (`@GoCodeAlone/workflow-ui`)

Tracked: issue #36. npm packages extracted from `workflow/ui/`:

| Package | Contents |
|---------|----------|
| `@GoCodeAlone/workflow-ui/auth` | Login, JWT management, protected routes |
| `@GoCodeAlone/workflow-ui/editor` | ReactFlow workflow editor, module palette, property panel |
| `@GoCodeAlone/workflow-ui/observability` | Execution timeline, log viewer, event inspector |
| `@GoCodeAlone/workflow-ui/layout` | App shell, plugin-driven nav, theme tokens |
| `@GoCodeAlone/workflow-ui/api` | Typed API client for engine endpoints |

### 2c. Application UI Build/Serve Contract

Tracked: issue #37. Standardized pattern:
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

A git-based registry:

```
workflow-registry/
├── plugins/
│   ├── feature-flags/
│   │   └── manifest.json
│   ├── ratchet/
│   │   └── manifest.json
│   └── kubernetes-operator/   # premium
│       └── manifest.json
├── templates/
│   ├── api-service.yaml
│   └── event-processor.yaml
└── schema/
    └── registry-schema.json
```

Each `manifest.json`:
```json
{
  "name": "feature-flags",
  "version": "1.0.0",
  "author": "GoCodeAlone",
  "description": "Feature flag service with LaunchDarkly integration",
  "source": "github.com/GoCodeAlone/workflow",
  "path": "plugins/featureflags",
  "type": "builtin",
  "license": "MIT",
  "tier": "community",
  "checksums": { "darwin-arm64": "sha256:...", "linux-amd64": "sha256:..." }
}
```

### 3b. Registry API Service

A lightweight API (itself built on the workflow engine) that:
- Indexes the registry repo
- Serves plugin search/filter by tier, category, tag
- Validates plugin submissions
- Serves download URLs and checksums
- Integrates with `wfctl publish` and admin Marketplace UI

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
4. Uses the license validation pattern already designed in `docs/APPLICATION_LIFECYCLE.md`

---

## Layer 4: Platform Services (`GoCodeAlone/workflow-cloud` — private)

The commercial layer: multi-tenancy, billing, license management, SaaS control plane.

### 4a. SaaS Control Plane

A workflow application (built on the engine itself) providing:

| Feature | Implementation |
|---------|---------------|
| Tenant management | PostgreSQL store (`PGWorkflowStore`, company/project hierarchy, RBAC) |
| Workflow deployment | `WorkflowEngineManager` with `ModuleNamespace` for tenant isolation |
| Visual designer | `@GoCodeAlone/workflow-ui/editor` shared component |
| Execution monitoring | `@GoCodeAlone/workflow-ui/observability` shared component |
| Billing | `billing/plans.go` (Starter/Professional/Enterprise tiers), Stripe integration |
| License validation API | `POST /api/v1/license/validate` — validates keys, returns tier limits |
| Plugin marketplace | Proxies Layer 3 registry, adds install/enable/disable per tenant |
| SSO/OAuth | OAuth2 (GitHub, Google, OIDC) — enterprise feature |

### 4b. License Server

```
Customer Engine ──→ POST /api/v1/license/validate ──→ License Server
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

### 4d. Deployment Infrastructure (OpenTofu + AWS)

```
workflow-cloud/
├── infra/
│   ├── modules/
│   │   ├── vpc/
│   │   ├── ecs/
│   │   ├── rds/
│   │   ├── elasticache/
│   │   ├── alb/
│   │   ├── cloudfront/
│   │   ├── ecr/
│   │   └── monitoring/
│   ├── environments/
│   │   ├── dev/
│   │   ├── staging/
│   │   └── production/
│   └── main.tf
├── deploy/
│   ├── helm/
│   └── docker-compose/
└── ci/
    ├── build.yml
    ├── deploy.yml
    └── release.yml
```

---

## Layer 5: Applications

Each mode of operation is a different configuration of the layers below.

### Mode: SaaS

```
User ──→ SaaS UI (Layer 4) ──→ Control Plane API ──→ WorkflowEngineManager
              │                                              │
    @GoCodeAlone/workflow-ui                    Deploy isolated engine per tenant
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
    ├── go.mod: require github.com/GoCodeAlone/workflow v0.x.y
    ├── ratchetplugin/       (custom Go plugin)
    ├── ratchet.yaml         (workflow config)
    ├── ui/                  (custom React UI using @GoCodeAlone/workflow-ui)
    └── cmd/ratchetd/main.go (thin bootstrap, ~100 lines)
```

---

## Repo Strategy

| Repo | Visibility | Purpose |
|------|-----------|---------|
| `GoCodeAlone/workflow` | Public | Core engine, admin UI, CLI, plugins, shared UI lib |
| `GoCodeAlone/ratchet` | Public | AI agent platform — embedded workflow application |
| `GoCodeAlone/workflow-registry` | Public | Plugin/template registry (git-based) |
| `GoCodeAlone/workflow-cloud` | Private | SaaS control plane, billing, license server, infra |

---

## Implementation Phases

### Phase 1: Stabilize Core (Weeks 1-3)

- Semver releases with CI/CD for workflow repo
- API stability contract documentation
- Remove `replace` directive from Ratchet, switch to versioned import

### Phase 2: UI Overhaul (Weeks 1-6, parallel with Phase 1)

Already tracked as issues #34-#39:
- #34: Decouple admin UI from Go binary
- #35: Plugin-driven admin navigation
- #36: Extract shared UI component library
- #37: Application UI build/serve contract
- #38: go-plugin UI hot-reload
- #39: Cleanup legacy UI scaffolding

### Phase 3: Developer Tooling (Weeks 3-6)

- `wfctl init` with project templates
- `wfctl plugin init` for plugin scaffolding
- `wfctl build-ui` (tracked: #37)
- Shared UI library npm publish (tracked: #36)

### Phase 4: Plugin Registry (Weeks 5-8)

- Create `workflow-registry` repo with schema
- Build registry API service (on the workflow engine itself)
- `wfctl publish` command
- Plugin tier system (core/community/premium)

### Phase 5: Platform Services (Weeks 6-12)

- License server implementation
- Billing integration (Stripe, existing `billing/plans.go`)
- SaaS control plane application
- Tenant management and workflow deployment via `WorkflowEngineManager`
- Premium plugin gating

### Phase 6: Infrastructure (Weeks 8-14)

- OpenTofu modules for AWS (VPC, ECS, RDS, ElastiCache, ALB, CloudFront)
- CI/CD for workflow-cloud
- Staging and production environments
- Monitoring (Prometheus, Grafana, CloudWatch)

### Phase 7: Ratchet Migration (Weeks 4-6, parallel)

- Remove `replace` directive, use versioned workflow import
- Migrate UI to use `@GoCodeAlone/workflow-ui` shared components
- Publish `ratchetplugin` to registry

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

- #34: Decouple Admin UI from Go Binary
- #35: Complete Plugin-Driven Admin UI Navigation
- #36: Extract Shared UI Component Library
- #37: Application UI Build/Serve Contract
- #38: go-plugin UI Hot-Reload
- #39: Cleanup Legacy UI Scaffolding

Additional issues will be created from this plan for phases not yet tracked (semver releases, API stability, plugin registry, license server, wfctl expansion, Ratchet migration, infrastructure).
