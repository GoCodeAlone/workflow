---
status: in_progress
area: ecosystem
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 57ae52f
  - repo: workflow
    commit: abbb53f
  - repo: workflow
    commit: be62358
  - repo: workflow
    commit: 8399629
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"expr-lang|ExprEngine|\\$\\{|migrate expressions|dual-mode|templateFuncMap|Resolve\" go.mod module cmd docs -g '!docs/plans/**'"
    - "git log --oneline --all -- module/pipeline_expr.go module/pipeline_template.go go.mod cmd/wfctl"
    - "GOWORK=off go test ./module ./plugins/pipelinesteps -run 'TestRawResponse|TestPipelineOutput|TestHTTPTrigger_PipelineOutput|TestResolve_PureExpr|TestExprEngine|TestSkip'"
  result: partial
supersedes: []
superseded_by: []
---

# Ecosystem Restructuring Design

**Date:** 2026-03-28
**Status:** Approved
**Scope:** modular, workflow, ratchet, ratchet-cli, 6 security plugins, workflow-plugin-agent

## Overview

Restructure the workflow ecosystem based on strategic review findings. Five workstreams: modular consolidation, workflow core extraction, agent vertical merge, security vertical merge, and dual-mode template system.

## Workstream 1: Modular Sub-Module Consolidation

Merge 10 lightweight sub-modules into the modular core. Keep only 3 heavy-dependency modules separate.

### Before (14 go.mod files)

```
modular/                → core
modules/auth/           → separate
modules/cache/          → separate
modules/chimux/         → separate
modules/configwatcher/  → separate
modules/database/v2     → separate
modules/eventbus/v2     → separate
modules/eventlogger/    → separate
modules/httpclient/     → separate
modules/httpserver/     → separate
modules/jsonschema/     → separate
modules/letsencrypt/    → separate
modules/logmasker/      → separate
modules/reverseproxy/v2 → separate
modules/scheduler/      → separate
```

### After (4 go.mod files)

```
modular/                → core v2.0.0 (absorbs auth, cache, chimux, configwatcher,
                          httpserver, httpclient, jsonschema, logmasker, scheduler,
                          eventlogger, letsencrypt)
modules/database/v2     → separate (pgx, mysql, sqlite driver deps)
modules/eventbus/v2     → separate (Kafka, NATS, Kinesis, Redis deps)
modules/reverseproxy/v2 → separate (heavy HTTP proxy deps)
```

Cache absorbed into core (in-memory impl has no heavy deps; Redis cache moves to eventbus module).

This is a **major version bump** for modular core (v1.x → v2.0.0) since import paths change for absorbed modules.

### Migration for consumers

```go
// Before
import "github.com/GoCodeAlone/modular/modules/httpserver"
import "github.com/GoCodeAlone/modular/modules/scheduler"

// After
import "github.com/GoCodeAlone/modular/v2/httpserver"
import "github.com/GoCodeAlone/modular/v2/scheduler"
```

## Workstream 2: Workflow Engine — Extract Verticals

Keep HTTP + pipeline steps + core modules. Extract IaC, deployment, CI/CD, AI, actors, and platform types into external plugin repos.

### Stays in core

| Plugin dir | Types | Reason |
|-----------|-------|--------|
| `plugins/http/` | server, router, handler, middleware, triggers | Fundamental |
| `plugins/pipelinesteps/` | all `step.*` types | Bread and butter of pipeline execution |
| `plugins/auth/` | JWT, OAuth2, M2M | Core auth primitives |
| `plugins/storage/` | sqlite, local, S3, GCS | Basic persistence |
| `plugins/messaging/` | broker, eventbus, NATS, Kafka | Core eventing |
| `plugins/observability/` | OTEL, health, metrics, logging | Core ops |
| `plugins/scheduling/` | scheduler, cron triggers | Core scheduling |
| `plugins/cache/` | modular cache | Core caching |
| `plugins/statemachine/` | engine, tracker, connector | Core state management |
| `plugins/openapi/` | OpenAPI generation | Used by wfctl api extract |

### Extract to external plugin repos

| Source dir | New repo | Types |
|-----------|----------|-------|
| `plugins/platform/` | `workflow-plugin-platform` | platform.* (networking, DNS, autoscaling, region, ECS, K8s, DO) |
| `plugins/infra/` | `workflow-plugin-infra` | IaC state, drift detection |
| `plugins/deployment/` | `workflow-plugin-deployment` | step.deploy_*, blue-green, canary, rolling, container_build |
| `plugins/cicd/` | `workflow-plugin-cicd` | step.git_*, step.docker_*, step.argo_*, step.codebuild_* |
| `plugins/ai/` | merged into workflow-plugin-agent | step.ai_classify, step.ai_complete, step.ai_extract |
| `plugins/actors/` | `workflow-plugin-actors` | actor.pool, actor.system, step.actor_* |
| `plugins/marketplace/` | `workflow-plugin-marketplace` | step.marketplace_* |
| `plugins/gitlab/` | `workflow-plugin-gitlab` | gitlab client, webhook, step.gitlab_* |
| `plugins/policy/` | merged into workflow-plugin-security | step.policy_* |
| `plugins/scanner/` | merged into workflow-plugin-security | security.scanner |

**Result:** Core drops from ~279 to ~120 types. 7 new external plugin repos. Schema export (`wfctl editor-schemas`) still covers all types — external plugins register via manifest system.

## Workstream 3: Agent Vertical — Merge ratchet + agent plugin

### Before (3 repos)

```
ratchet/                    → Go library (25K lines)
workflow-plugin-agent/      → gRPC plugin (11K lines)
ratchet-cli/                → CLI binary (22K lines)
```

### After (2 repos)

```
workflow-plugin-agent/      → merged: agent SDK + ratchet orchestration + AI steps
├── sdk/                    (from agent/sdk — daemon, streaming, tool execution)
├── provider/               (merged providers from both repos)
├── orchestrator/           (from ratchet/ratchetplugin — agent loop, tools, memory)
├── plugin.go               (gRPC entry — step.agent_execute, step.ai_classify, etc.)
└── go.mod

ratchet-cli/                → stays separate, imports workflow-plugin-agent
└── go.mod                  (depends on workflow-plugin-agent instead of ratchet + agent)
```

`ratchet/` repo archived after migration.

## Workstream 4: Security Vertical — Merge 6 repos

### Before (6 repos, ~21K lines total)

```
workflow-plugin-waf              (2.5K — WAF providers)
workflow-plugin-security         (3.4K — MFA, encryption, KMS)
workflow-plugin-authz            (10K — Casbin RBAC)
workflow-plugin-data-protection  (1.4K — PII detection)
workflow-plugin-sandbox          (1.3K — WASM sandbox)
workflow-plugin-supply-chain     (2K — signatures, vuln scan, SBOM)
```

### After (1 repo)

```
workflow-plugin-security/
├── waf/              (WAF providers)
├── mfa/              (TOTP, encryption, KMS)
├── authz/            (Casbin RBAC)
├── dataprotection/   (PII detection + masking)
├── sandbox/          (WASM sandboxing)
├── supplychain/      (signatures, vuln scan, SBOM)
├── plugin.go         (single gRPC plugin entry)
├── go.mod
└── .goreleaser.yaml
```

`workflow-plugin-authz-ui` stays separate (React SPA, not Go).

## Workstream 5: Dual-Mode Template System

Add `expr-lang/expr` as a second expression engine. New `${ }` syntax for expressions, `{{ }}` continues to work.

### Syntax

```yaml
# Go templates (existing — stays forever for string interpolation)
values:
  message: "Hello {{ .body.name }}"

# New expr syntax (for logic and data transformation)
condition: ${ steps.validate.status == "ok" && body.age > 18 }
values:
  total: ${ steps.fetch.rows | map(.amount) | sum() }
  name: ${ upper(steps.lookup.row.first_name) }
```

### Implementation

1. New `template/expr.go` — wraps expr-lang/expr with pipeline context
2. Detection in `pipeline_template.go` — `${ }` → expr, `{{ }}` → Go templates
3. `skip_if` / `if` fields — support both syntaxes
4. Built-in expr functions — map existing 30+ template functions
5. `wfctl migrate expressions` — automated migration tool
6. LSP support — expr gets hover docs and completion

### Deprecation path

- v0.5.0: Add expr support, dual mode
- v0.6.0: LSP warns on Go template usage in condition/logic fields
- v1.0.0: Go templates only valid for string interpolation, not conditionals

## Implementation Order

1. **Workstream 5 (template system)** — no repo restructuring, additive change, can ship immediately
2. **Workstream 4 (security merge)** — smallest merge, good practice run for the pattern
3. **Workstream 3 (agent merge)** — medium complexity, 2 repos → 1
4. **Workstream 2 (workflow extraction)** — large but mechanical, extract plugins/ to repos
5. **Workstream 1 (modular consolidation)** — largest, breaking change (v2.0.0), do last

Each workstream is independently shippable. Later workstreams don't depend on earlier ones.
