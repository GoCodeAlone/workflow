# Plan: Decompose Admin Config — Replace Delegates with Pipeline Primitives

## Context

The admin config (`admin/config.yaml`) has **18 intermediate `api.query`/`api.command` module definitions** (9 domain pairs) that each delegate to a Go service, and **~60 routes with a lazy `step.delegate` step named `forward`**. This needs to be decomposed into pipeline primitives.

**Current problems:**
1. 16 unnecessary handler modules (only need 2 generic ones)
2. ~60 routes with useless `name: forward` step.delegate steps
3. Many V1 routes (executions, audit, IAM, permissions, logs) delegate to `admin-v1-mgmt` which **doesn't handle those paths** — they silently return 404
4. V1Store only creates 4 tables (companies, projects, workflows, workflow_versions) — missing tables for executions, logs, audit, IAM, permissions

**Goal:** Consolidate modules, decompose all feasible routes into pipeline primitives, add missing DB tables, and give meaningful names to routes that must remain as delegates.

---

## Scope

### Phase A: Module Consolidation + Handler Rewiring
- Remove 16 domain-specific modules, keep 2 generic handlers
- Update all ~77 route `handler:` references

### Phase B: Decompose V1 CRUD Routes (existing tables)
- 6 routes that use existing admin-db tables (projects create, workflows create/update/deploy/stop)

### Phase C: Add Missing Tables + Decompose New Routes
- Add workflow_executions, execution_steps, execution_logs, audit_log, iam_provider_configs, iam_role_mappings tables to V1Store
- Decompose ~15 currently-broken routes into working pipeline primitives

### Phase D: Improve Remaining Delegate Routes
- ~40 routes for domain services (Timeline, DLQ, AI, Components, Engine, Schema, Backfill, Billing, Replay)
- Replace `forward` with meaningful step names + optional pre-processing steps

---

## Phase A: Module Consolidation

### Remove these 16 modules from `modules:` section:
```
admin-engine-queries, admin-engine-commands
admin-schema-queries
admin-ai-queries, admin-ai-commands
admin-component-queries, admin-component-commands
admin-timeline-queries
admin-replay-queries, admin-replay-commands
admin-dlq-queries, admin-dlq-commands
admin-backfill-queries, admin-backfill-commands
admin-billing-queries, admin-billing-commands
```

### Rename + simplify the 2 remaining handlers:
```yaml
# Before:
- name: admin-v1-queries
  type: api.query
  config:
    delegate: admin-v1-mgmt
  dependsOn: [admin-workflow-registry, admin-auth]

- name: admin-v1-commands
  type: api.command
  config:
    delegate: admin-v1-mgmt
  dependsOn: [admin-workflow-registry, admin-auth]

# After:
- name: admin-queries
  type: api.query
  dependsOn: [admin-router]

- name: admin-commands
  type: api.command
  dependsOn: [admin-router]
```

**Key code fact:** `QueryHandler` and `CommandHandler` work fine without `delegate` config — it's optional. When a route has a pipeline, the pipeline runs; the delegate is only a fallback. See `query_handler.go:64-68` and `command_handler.go:63-67`.

### Update all route `handler:` references:
- `handler: admin-engine-queries` → `handler: admin-queries`
- `handler: admin-engine-commands` → `handler: admin-commands`
- `handler: admin-v1-queries` → `handler: admin-queries`
- `handler: admin-v1-commands` → `handler: admin-commands`
- (all 18 old handler names → one of the 2 new ones)

---

## Phase B: Decompose V1 CRUD Routes (Existing Tables)

These routes currently delegate to `admin-v1-mgmt` (V1APIHandler). Replace with pipeline primitives using `admin-db`.

**Tables available:** companies, projects, workflows, workflow_versions (in `module/api_v1_store.go:52-108`)

### B1: POST /api/v1/admin/organizations/{id}/projects
```yaml
pipeline:
  steps:
    - name: parse-request
      type: step.request_parse
      config:
        path_params: [id]
        parse_body: true
    - name: validate
      type: step.validate
      config:
        strategy: required_fields
        required_fields: [name]
        source: "steps.parse-request.body"
    - name: prepare
      type: step.set
      config:
        values:
          id: "{{ uuidv4 }}"
          now: "{{ now }}"
    - name: insert-project
      type: step.db_exec
      config:
        database: admin-db
        query: "INSERT INTO projects (id, company_id, name, slug, description, is_system, metadata, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, '{}', ?, ?)"
        params:
          - "{{ .steps.prepare.id }}"
          - "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
          - "{{index .steps \"parse-request\" \"body\" \"name\"}}"
          - "{{lower (index .steps \"parse-request\" \"body\" \"name\")}}"
          - "{{default \"\" (index .steps \"parse-request\" \"body\" \"description\")}}"
          - "{{ .steps.prepare.now }}"
          - "{{ .steps.prepare.now }}"
    - name: respond
      type: step.json_response
      config:
        status: 201
        body:
          id: "{{ .steps.prepare.id }}"
          name: "{{index .steps \"parse-request\" \"body\" \"name\"}}"
          slug: "{{lower (index .steps \"parse-request\" \"body\" \"name\")}}"
          company_id: "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
          created_at: "{{ .steps.prepare.now }}"
```

### B2: POST /api/v1/admin/projects/{id}/workflows
Same pattern as B1 but INSERT INTO workflows.

### B3: POST /api/v1/admin/workflows
parse_body → validate(name,project_id,config_yaml) → set(uuid,now) → db_exec(INSERT workflows) → json_response(201)

### B4: PUT /api/v1/admin/workflows/{id}
```yaml
pipeline:
  steps:
    - name: parse-request
      type: step.request_parse
      config:
        path_params: [id]
        parse_body: true
    - name: check-exists
      type: step.db_query
      config:
        database: admin-db
        query: "SELECT id, version, is_system FROM workflows WHERE id = ?"
        params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
        mode: single
    - name: check-found
      type: step.conditional
      config:
        field: "steps.check-exists.found"
        routes:
          "false": not-found
        default: prepare-update
    - name: prepare-update
      type: step.set
      config:
        values:
          now: "{{ now }}"
          version_id: "{{ uuidv4 }}"
          new_version: "{{ .steps.check-exists.row.version }}"  # note: need +1
    - name: update-workflow
      type: step.db_exec
      config:
        database: admin-db
        query: "UPDATE workflows SET name=?, description=?, config_yaml=?, version=version+1, updated_by=?, updated_at=? WHERE id=?"
        params:
          - "{{index .steps \"parse-request\" \"body\" \"name\"}}"
          - "{{default \"\" (index .steps \"parse-request\" \"body\" \"description\")}}"
          - "{{default \"\" (index .steps \"parse-request\" \"body\" \"config_yaml\")}}"
          - ""
          - "{{ .steps.prepare-update.now }}"
          - "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
    - name: save-version
      type: step.db_exec
      config:
        database: admin-db
        query: "INSERT INTO workflow_versions (id, workflow_id, version, config_yaml, created_by, created_at) SELECT ?, id, version, config_yaml, '', ? FROM workflows WHERE id = ?"
        params:
          - "{{ .steps.prepare-update.version_id }}"
          - "{{ .steps.prepare-update.now }}"
          - "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
    - name: get-updated
      type: step.db_query
      config:
        database: admin-db
        query: "SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at FROM workflows WHERE id = ?"
        params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
        mode: single
    - name: respond
      type: step.json_response
      config:
        status: 200
        body_from: "steps.get-updated.row"
    - name: not-found
      type: step.json_response
      config:
        status: 404
        body:
          error: "workflow not found"
```

### B5: POST /api/v1/admin/workflows/{id}/deploy
```yaml
pipeline:
  steps:
    - name: parse-request
      type: step.request_parse
      config:
        path_params: [id]
    - name: set-now
      type: step.set
      config:
        values:
          now: "{{ now }}"
    - name: activate
      type: step.db_exec
      config:
        database: admin-db
        query: "UPDATE workflows SET status='active', updated_at=? WHERE id=?"
        params:
          - "{{ .steps.set-now.now }}"
          - "{{index .steps \"parse-request\" \"path_params\" \"id\"}}"
    - name: get-workflow
      type: step.db_query
      config:
        database: admin-db
        query: "SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at FROM workflows WHERE id = ?"
        params: ["{{index .steps \"parse-request\" \"path_params\" \"id\"}}"]
        mode: single
    - name: respond
      type: step.json_response
      config:
        status: 200
        body_from: "steps.get-workflow.row"
```

### B6: POST /api/v1/admin/workflows/{id}/stop
Same pattern as B5 but `status='stopped'`.

---

## Phase C: Add Missing Tables + Decompose New Routes

### C1: Add tables to V1Store.initSchema() (`module/api_v1_store.go`)

Add these `CREATE TABLE IF NOT EXISTS` statements:

```sql
-- Execution tracking
CREATE TABLE IF NOT EXISTS workflow_executions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    trigger_data TEXT DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    output_data TEXT DEFAULT '{}',
    error_message TEXT DEFAULT '',
    started_at TEXT NOT NULL,
    completed_at TEXT,
    duration_ms INTEGER DEFAULT 0,
    metadata TEXT DEFAULT '{}',
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS execution_steps (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,
    input_data TEXT DEFAULT '{}',
    output_data TEXT DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT DEFAULT '',
    started_at TEXT,
    completed_at TEXT,
    duration_ms INTEGER DEFAULT 0,
    sequence_num INTEGER NOT NULL,
    metadata TEXT DEFAULT '{}',
    FOREIGN KEY (execution_id) REFERENCES workflow_executions(id) ON DELETE CASCADE
);

-- Logs & Audit
CREATE TABLE IF NOT EXISTS execution_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    execution_id TEXT,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    module_name TEXT DEFAULT '',
    fields TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    details TEXT DEFAULT '{}',
    ip_address TEXT DEFAULT '',
    user_agent TEXT DEFAULT '',
    created_at TEXT NOT NULL
);

-- IAM
CREATE TABLE IF NOT EXISTS iam_provider_configs (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    name TEXT NOT NULL,
    config TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS iam_role_mappings (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    external_identifier TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (provider_id) REFERENCES iam_provider_configs(id) ON DELETE CASCADE
);
```

### C2: Decompose routes using new tables

Each of these currently returns 404 via the V1APIHandler fallback. Implement as pipeline primitives:

| Route | Pipeline |
|-------|----------|
| GET /admin/workflows/{id}/executions | parse(id) → db_query(workflow_executions WHERE workflow_id=?) → json_response |
| GET /admin/workflows/{id}/logs | parse(id) → db_query(execution_logs WHERE workflow_id=? ORDER BY created_at DESC LIMIT 100) → json_response |
| GET /admin/workflows/{id}/events | parse(id) → db_query(execution_events via admin-db) → json_response |
| GET /admin/workflows/{id}/permissions | parse(id) → json_response(200, []) — stub until memberships wired |
| POST /admin/workflows/{id}/permissions | parse + validate → json_response(501, "not implemented") |
| GET /admin/executions/{id} | parse(id) → db_query(workflow_executions WHERE id=?) → conditional → json_response |
| GET /admin/executions/{id}/steps | parse(id) → db_query(execution_steps WHERE execution_id=?) → json_response |
| POST /admin/executions/{id}/cancel | parse(id) → set(now) → db_exec(UPDATE status='cancelled') → json_response |
| GET /admin/audit | db_query(audit_log ORDER BY created_at DESC LIMIT 100) → json_response |
| GET /admin/iam/providers/{id} | parse(id) → db_query(iam_provider_configs) → conditional → json_response |
| POST /admin/iam/providers | parse + validate → set(uuid,now) → db_exec(INSERT) → json_response(201) |
| PUT /admin/iam/providers/{id} | parse + body → set(now) → db_exec(UPDATE) → json_response |
| DELETE /admin/iam/providers/{id} | parse(id) → db_exec(DELETE) → json_response |
| POST /admin/iam/providers/{id}/test | parse(id) → json_response(200, {status: "ok", message: "connection test not yet implemented"}) |
| GET /admin/iam/providers/{id}/mappings | parse(id) → db_query(iam_role_mappings) → json_response |
| POST /admin/iam/providers/{id}/mappings | parse + validate → set(uuid,now) → db_exec(INSERT) → json_response(201) |
| DELETE /admin/iam/mappings/{id} | parse(id) → db_exec(DELETE) → json_response |
| GET /admin/workflows/{id}/dashboard | parse(id) → db_query(workflow + counts) → json_response |
| POST /admin/workflows/{id}/trigger | parse(id) → set(uuid,now) → db_exec(INSERT workflow_executions status='pending') → json_response(202) |
| GET /admin/workflows/{id}/status | parse(id) → db_query(SELECT status FROM workflows) → json_response |

**SSE streaming routes** (logs/stream, events/stream) → keep as delegate to `admin-v1-mgmt` with meaningful names (`stream-workflow-logs`, `stream-workflow-events`).

---

## Phase D: Improve Remaining Delegate Routes

For domain services that need Go logic, replace `forward` with descriptive names:

### Engine Management (→ admin-engine-mgmt)
| Route | Step name |
|-------|-----------|
| GET /engine/config | `get-engine-config` |
| GET /engine/status | `get-engine-status` |
| GET /engine/modules | `list-engine-modules` |
| GET /engine/services | `list-engine-services` |
| PUT /engine/config | `update-engine-config` |
| POST /engine/validate | `validate-engine-config` |
| POST /engine/reload | `reload-engine` |

### Schema (→ admin-schema-mgmt)
| Route | Step name |
|-------|-----------|
| GET /schemas | `list-schemas` |
| GET /schemas/modules | `list-module-schemas` |

### AI (→ admin-ai-mgmt)
| Route | Step name |
|-------|-----------|
| GET /ai/providers | `list-ai-providers` |
| POST /ai/generate | `generate-workflow-from-intent` |
| POST /ai/component | `generate-component` |
| POST /ai/suggest | `suggest-improvements` |
| POST /ai/deploy | `deploy-generated-workflow` |
| POST /ai/deploy/component | `deploy-generated-component` |

### Dynamic Components (→ admin-component-mgmt)
| Route | Step name |
|-------|-----------|
| GET /components | `list-dynamic-components` |
| GET /components/{id} | `get-dynamic-component` |
| POST /components | `create-dynamic-component` |
| PUT /components/{id} | `update-dynamic-component` |
| DELETE /components/{id} | `delete-dynamic-component` |

### Timeline (→ admin-timeline-mgmt)
| Route | Step name |
|-------|-----------|
| GET /executions | `list-all-executions` |
| GET /executions/{id}/timeline | `get-execution-timeline` |
| GET /executions/{id}/events | `get-execution-events` |

### Replay (→ admin-replay-mgmt)
| Route | Step name |
|-------|-----------|
| POST /executions/{id}/replay | `replay-execution` |
| GET /executions/{id}/replay | `get-replay-history` |

### DLQ (→ admin-dlq-mgmt)
| Route | Step name |
|-------|-----------|
| GET /dlq | `list-dlq-entries` |
| GET /dlq/stats | `get-dlq-stats` |
| GET /dlq/{id} | `get-dlq-entry` |
| POST /dlq/{id}/retry | `retry-dlq-entry` |
| POST /dlq/{id}/discard | `discard-dlq-entry` |
| POST /dlq/{id}/resolve | `resolve-dlq-entry` |
| DELETE /dlq/purge | `purge-resolved-dlq` |

### Backfill/Mock/Diff (→ admin-backfill-mgmt)
| Route | Step name |
|-------|-----------|
| GET /backfill | `list-backfills` |
| POST /backfill | `create-backfill` |
| GET /backfill/{id} | `get-backfill` |
| POST /backfill/{id}/cancel | `cancel-backfill` |
| GET /mocks | `list-mocks` |
| POST /mocks | `set-step-mock` |
| DELETE /mocks | `clear-all-mocks` |
| GET /mocks/{pipeline} | `list-pipeline-mocks` |
| DELETE /mocks/{pipeline}/{step} | `remove-step-mock` |
| GET /executions/diff | `compute-execution-diff` |

### Billing (→ admin-billing-mgmt)
| Route | Step name |
|-------|-----------|
| GET /billing/plans | `list-billing-plans` |
| GET /billing/usage | `get-usage-report` |
| POST /billing/subscribe | `create-subscription` |
| DELETE /billing/subscribe | `cancel-subscription` |
| POST /billing/webhook | `handle-billing-webhook` |

---

## Files to Modify

| File | Change |
|------|--------|
| `admin/config.yaml` | Remove 16 modules, rename 2, rewrite all ~77 route pipelines |
| `module/api_v1_store.go` | Add 6 new CREATE TABLE statements to initSchema() |
| `module/query_handler.go` | No changes needed (delegate already optional) |
| `module/command_handler.go` | No changes needed (delegate already optional) |

---

## Implementation Order

1. **Phase A first** — structural cleanup, all routes still work via delegates
2. **Phase B** — decompose 6 V1 CRUD routes using existing tables
3. **Phase C** — add tables + decompose 20 currently-broken routes
4. **Phase D** — rename all remaining delegate steps

Test after each phase: `go build && ./server -config admin/config.yaml`

---

## Verification

1. `go build -o server ./cmd/server` — compiles
2. `rm -f data/workflow.db && ./server -config admin/config.yaml` — starts clean
3. Auth: `POST /api/v1/auth/setup` + `POST /api/v1/auth/login` → get token
4. Existing CRUD: `POST /api/v1/admin/companies` → 201
5. New CRUD: `POST /api/v1/admin/organizations/{id}/projects` → 201 (was delegate, now pipeline)
6. New tables: `GET /api/v1/admin/audit` → 200 with [] (was 404, now works)
7. Delegate routes: `GET /api/v1/admin/engine/status` → 200 (still works)
8. `go test ./...` — all existing tests pass

---

## Post-Implementation: Documentation

After all phases complete, spawn concurrent agents to:
1. **Screenshot agent**: Start server, navigate Playwright through every UI page, take screenshots
2. **Documentation agent**: Write a markdown document (`docs/ADMIN_UI_FEATURES.md`) describing each screenshot and the functionality it demonstrates
3. Save plan copy to `docs/ADMIN_CONFIG_DECOMPOSITION.md` in the repo for permanent reference

---

## Key Reference Files

| File | Purpose |
|------|---------|
| `admin/config.yaml` | The config being rewritten (~1400 lines) |
| `module/api_v1_store.go` | V1Store schema — needs new tables added |
| `module/api_v1_handler.go` | V1APIHandler — handles companies/orgs/projects/workflows/dashboard only |
| `module/query_handler.go` | QueryHandler — dispatch chain: Func → Pipeline → Delegate → 404 |
| `module/command_handler.go` | CommandHandler — same dispatch chain |
| `module/pipeline_step_delegate.go` | step.delegate — passes full request to service http.Handler |
| `module/pipeline_step_db_query.go` | step.db_query — parameterized SELECT |
| `module/pipeline_step_db_exec.go` | step.db_exec — parameterized INSERT/UPDATE/DELETE |
| `module/pipeline_step_request_parse.go` | step.request_parse — extracts path/query params + body |
| `module/pipeline_step_json_response.go` | step.json_response — writes HTTP JSON response |
| `module/pipeline_step_conditional.go` | step.conditional — branch based on field value |
| `module/pipeline_step_set.go` | step.set — generate UUIDs, timestamps, computed values |
| `module/pipeline_step_validate.go` | step.validate — required_fields or json_schema |
| `store/timeline_handler.go` | TimelineHandler + ReplayHandler |
| `store/dlq_handler.go` | DLQHandler |
| `store/backfill_handler.go` | BackfillMockDiffHandler |
| `billing/handler.go` | billing.Handler |
| `module/api_workflow_ui.go` | WorkflowUIHandler (engine mgmt) |
| `schema/handler.go` | SchemaService |
| `ai/combined_handler.go` | AI CombinedHandler |
| `dynamic/api.go` | Dynamic components APIHandler |
