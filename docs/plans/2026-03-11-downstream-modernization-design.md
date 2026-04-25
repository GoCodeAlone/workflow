---
status: in_progress
area: ecosystem
owner: workflow
implementation_refs: []
external_refs:
  - "buymywishlist-phase3: app.yaml has X-Workflow-Trace and some step.parallel use, but parse_body remains"
  - "ratchet: config/modules.yaml has X-Workflow-Trace and routes-core.yaml has step.parallel, but parse_body remains"
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"step\\.parallel|X-Workflow-Trace|parse_body|user-dashboard|analytics-overview\" /Users/jon/workspace/buymywishlist* /Users/jon/workspace/ratchet*"
  result: partial
supersedes: []
superseded_by: []
---

# Downstream Modernization: BMW, Ratchet, Ratchet-CLI

**Date:** 2026-03-11
**Status:** Approved

## Goal

Ensure BMW, ratchet, and ratchet-cli take advantage of workflow engine improvements from v0.3.18 → v0.3.32 and modular v1.12.0 → v1.12.3.

## Changes

### 1. wfctl modernize --apply (BMW + Ratchet)

Run automated YAML fixes on both projects:

**BMW** (`buymywishlist-phase3/app.yaml`): ~364 issues
- 305 hyphen-step names → underscore
- ~40 `parse_body: true` → remove (body auto-parsed since v0.3.28)
- 1 conditional-field using template syntax → dot-path
- 2 camelCase config violations

**Ratchet** (`ratchet/config/*.yaml`): ~230+ issues
- 228 hyphen-step names → underscore
- 28 `parse_body: true` → remove
- 5 camelCase config violations

### 2. step.parallel Adoption

**BMW pipelines** (6-8 candidates):
- `user-dashboard`: parallelize count_wishlists + count_contributions
- `analytics-overview`: parallelize check_permission + fetch_stats
- `analytics-wishlists`: parallelize check_permission + fetch_stats
- `analytics-contributions`: parallelize check_permission + fetch_stats
- `analytics-revenue`: parallelize check_permission + fetch_revenue
- `payment-create-intent`: parallelize check_mock_mode + get_tenant_settings
- `admin-users-list`: parallelize fetch_role + fetch_users
- `admin-audit-logs`: parallelize fetch_role + fetch_logs

**Ratchet** (1 candidate):
- `/api/info` route: parallelize get-started-at + count-agents + count-teams (3 independent queries)

### 3. Execution Tracing Configuration

**BMW:**
- Add `X-Workflow-Trace` to CORS allowedHeaders
- Already has observability.otel + http.middleware.otel configured

**Ratchet:**
- Add `X-Workflow-Trace` to CORS allowedHeaders (if CORS configured)
- Already has observability.otel configured

### Out of Scope

- `ratchet modernize` CLI command (ratchet-cli is an AI agent client, not a workflow toolchain)
- step.hash for bcrypt (only supports MD5/SHA256/SHA512)
- Collection template functions replacing SQL aggregation (SQL is more efficient)
- step.cli_print/cli_invoke for ratchet-cli (uses Bubbletea TUI)
- Actor model (no current use case)

## Implementation Plan

### Phase 1: Automated YAML Modernization
1. Run `wfctl modernize --apply` on BMW app.yaml
2. Run `wfctl modernize --apply` on ratchet config/*.yaml
3. Verify configs still parse correctly
4. Commit changes

### Phase 2: step.parallel Adoption
1. BMW: Refactor 6-8 pipelines to use step.parallel for independent queries
2. Ratchet: Refactor /api/info route to use step.parallel
3. Update template references to access parallel step outputs
4. Commit changes

### Phase 3: Execution Tracing
1. Add X-Workflow-Trace to CORS headers in both projects
2. Commit changes

### Phase 4: Verify and Push
1. Run wfctl validate on all configs
2. Push all changes
