# Workflow Config Field Quirks & Inconsistencies

**Date:** 2026-04-13
**Context:** Discovered during self-improving agentic workflow scenario execution. These inconsistencies cause LLM-generated configs to fail at startup, requiring iterative validation to correct.

## Critical: Field Names That Don't Match Intuition

### 1. `http.server` uses `address` not `port`

**Wrong:**
```yaml
- name: server
  type: http.server
  config:
    port: 8080       # FAILS: "required config field 'address' is missing"
```

**Correct:**
```yaml
- name: server
  type: http.server
  config:
    address: ":8080"  # Note: string with colon prefix, not integer
```

**Impact:** Every LLM and most humans guess `port: 8080`. The field name `address` is non-obvious, and the value format (`:8080` string vs `8080` integer) is also unexpected.

### 2. `step.db_exec` / `step.db_query` use `database` not `module`

**Wrong:**
```yaml
- name: insert
  type: step.db_exec
  config:
    module: db        # FAILS: "'database' is required"
```

**Correct:**
```yaml
- name: insert
  type: step.db_exec
  config:
    database: db      # References the module by name
```

**Impact:** Since the value references a module name, `module:` is the intuitive field name. `database:` is the storage-specific name.

### 3. `step.db_exec` / `step.db_query` use `params` not `args`

**Wrong:**
```yaml
config:
  query: "INSERT INTO tasks (title) VALUES (?)"
  args:               # FAILS: "missing argument with index 1"
    - "{{ .body.title }}"
```

**Correct:**
```yaml
config:
  query: "INSERT INTO tasks (title) VALUES (?)"
  params:             # Named "params" not "args"
    - "{{ .body.title }}"
```

**Impact:** `args` is the more common name in most frameworks and languages. `params` works but isn't obvious.

### 4. `step.db_query` mode values: `list`/`single` not `many`/`one`

**Wrong:**
```yaml
config:
  mode: many          # FAILS: "mode must be 'list' or 'single'"
  mode: one           # Also fails
```

**Correct:**
```yaml
config:
  mode: list          # For multiple rows
  mode: single        # For one row
```

**Impact:** `many`/`one` or `multiple`/`single` are intuitive pairs. `list`/`single` is an asymmetric naming pattern.

### 5. `step.request_parse` uses `parse_body: true` not `format: json`

**Wrong:**
```yaml
- name: parse
  type: step.request_parse
  config:
    format: json      # Silently ignored — body not parsed
```

**Correct:**
```yaml
- name: parse
  type: step.request_parse
  config:
    parse_body: true  # Boolean flag, format auto-detected from Content-Type
```

**Impact:** Most HTTP frameworks use `format` or `content_type` to specify body parsing. The boolean `parse_body` flag is unusual and doesn't indicate what format is expected.

### 6. Response step is `step.json_response` not `step.response`

**Wrong:**
```yaml
- name: respond
  type: step.response  # FAILS: "unknown step type: step.response"
```

**Correct:**
```yaml
- name: respond
  type: step.json_response  # Must specify json_ prefix
```

**Impact:** `step.response` is the obvious name. Having to know it's `step.json_response` requires checking the step registry. There's no generic `step.response` — you must choose `step.json_response`, `step.html_response`, etc.

## Important: Structural Patterns That Surprise

### 7. Pipeline triggers are inline, not workflow-level routes

**Wrong (routes pointing to pipelines):**
```yaml
workflows:
  http:
    routes:
      - path: /tasks
        method: GET
        pipeline: list_tasks  # NOT how it works
pipelines:
  list_tasks:
    steps: [...]
```

**Correct (inline trigger in pipeline):**
```yaml
workflows:
  http:
    routes: []                # Empty — routes come from triggers
pipelines:
  list_tasks:
    trigger:
      type: http
      config:
        path: /tasks
        method: GET
    steps: [...]
```

**Impact:** This is the most confusing structural pattern. Most frameworks define routes in one place and point to handlers. Workflow inverts this — each pipeline declares its own trigger. The `routes: []` in the workflow section is required but counterintuitive since it's always empty when using inline triggers.

### 8. `step.json_response` body can be object or template string

```yaml
# Object form — produces clean JSON:
body:
  status: healthy
  version: "1.0"

# Template form — produces JSON string (may double-serialize):
body: "{{ .steps.query.rows | json }}"
```

**Impact:** The template form produces a JSON string value, not raw JSON. List endpoints return `"[{...}]"` (a JSON string containing JSON) instead of `[{...}]` (a JSON array). Consumers must parse twice.

### 9. `step.conditional` uses `field` + `routes` map + `default`, not `condition`/`then`/`else`

**Wrong:**
```yaml
- name: check
  type: step.conditional
  config:
    condition: "{{ .status == 'active' }}"
    then: proceed
    else: reject
```

**Correct:**
```yaml
- name: check
  type: step.conditional
  config:
    field: "{{ .status }}"
    routes:
      active: proceed
      inactive: reject
    default: reject     # REQUIRED — omitting causes error
```

**Impact:** `condition`/`then`/`else` is the universal conditional pattern. The `field` + `routes` map pattern is more like a switch statement, which is unusual for a step named "conditional".

### 10. Template data access varies by source

```yaml
# Path params are flat:
"{{ .id }}"                    # NOT {{ .path.id }}

# Query params are flat:
"{{ .q }}"                     # NOT {{ .query.q }}

# Request body requires parse step:
"{{ .body.title }}"            # After step.request_parse

# Step outputs use index for hyphens:
"{{ index .steps \"my-step\" \"key\" }}"  # NOT {{ .steps.my-step.key }}

# But dot-access works for underscore names:
"{{ .steps.my_step.key }}"    # Works fine
```

**Impact:** Inconsistent data access patterns make template authoring error-prone. Flat path/query params are convenient but surprising. The hyphenated step name issue is documented but easy to forget.

## Recommendations

1. **Add aliases** — Accept `port` as alias for `address`, `module` as alias for `database`, `args` as alias for `params`, `many`/`one` as aliases for `list`/`single`
2. **Register `step.response`** as alias for `step.json_response` (most common case)
3. **Fix double-serialization** — Template `body` expressions should produce raw values, not JSON strings
4. **Add `step.request_parse` format param** — Accept `format: json` as equivalent to `parse_body: true`
5. **Document route pattern explicitly** — The inline trigger pattern is the biggest structural surprise; needs prominent documentation
6. **Add `wfctl modernize` rules** — Detect and auto-fix common misnamings (port→address, module→database, args→params)
7. **Enhance LSP completions** — When typing a wrong field name, suggest the correct one
8. **Consider `step.conditional` `if`/`then`/`else` mode** — Add alongside existing `field`/`routes` for simple boolean conditions
