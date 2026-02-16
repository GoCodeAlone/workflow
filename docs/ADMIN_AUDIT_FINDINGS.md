# Administration Audit Findings

**Date**: 2026-02-16
**Auditors**: 3 automated QA agents (plugin coherence, architecture separation, functional API)

## Executive Summary

The administration server bundles ~116 API routes, 14+ services, and a full React UI into a single monolithic binary. Three audits revealed: the plugin system is cosmetic (install/uninstall doesn't gate access), the engine reload endpoint is destructive (wipes services and user DB), and there's no separation between admin and worker functionality.

## Functional API Results

**76 endpoints tested: 31 working, 37 broken (post-reload), 8 not registered**

### Working (31)
- Auth & User Management: 12/12
- Workflow CRUD: 14/15 (validate not implemented)
- Companies: 4/4
- Execution basics: 4/4 (list, detail, steps, cancel)
- Audit: 1/1

### Critical: Engine Reload is Destructive
`POST /api/v1/admin/engine/reload` wipes the user database, destroys all `wireV1HandlerPostStart()` service registrations, and renders the entire server non-functional. Requires full restart + re-setup.

### Services Lost After Reload
All delegate-based services: timeline, replay, DLQ, backfill, billing, environments, cloud providers, plugins, schemas, components, AI, v1-mgmt.

## Plugin System Coherence

### Two Independent Registries
1. **NativeRegistry** — compiled-in plugins (Store Browser, Doc Manager, cloud providers). Always active, no enable/disable.
2. **CompositeRegistry** (Marketplace) — external plugins. Install/uninstall operates on a separate data structure.

### Key Gaps
- Store Browser is always available regardless of marketplace state
- UI hard-codes plugin nav items in AppNav.tsx
- pluginStore fetches metadata but nav never uses it
- No mechanism to conditionally register services based on plugin state
- No plugin-level RBAC

## Architecture Separation

### Current: Everything in One Binary
- Single `modular.Application` instance
- Single service registry
- All services unconditionally registered in `wireV1HandlerPostStart()`
- Monolithic `admin/config.yaml` with all ~116 routes

### Route Categorization (116 routes)
| Category | Count | Examples |
|----------|-------|---------|
| Admin-only | 56 | User CRUD, workflow editor, engine config, IAM, AI |
| Plugin-worthy | 39 | Timeline, replay, DLQ, backfill, billing, environments, cloud providers |
| Shared | 21 | Auth validation, schemas, execution reads, health, metrics |

### Security Concerns
1. Editor can modify admin config (disable auth, add shell_exec steps)
2. Store Browser exposes all tables (audit logs, IAM configs) to any authenticated user
3. AI service deploys code without review
4. No RBAC on plugin endpoints
5. JWT secret potentially exposed via engine config GET

## Recommendations

See `ADMIN_PLUGIN_ARCHITECTURE.md` for the plugin-based decomposition plan.
