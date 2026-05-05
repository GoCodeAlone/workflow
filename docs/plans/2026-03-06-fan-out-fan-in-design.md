---
status: implemented
area: runtime
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 3d2eb47
  - repo: workflow
    commit: c4f775a
  - repo: workflow
    commit: 566fb5a
  - repo: workflow
    commit: 50f9c06
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"step.parallel|concurrency|groupBy\" module plugins cmd"
    - "GOWORK=off go test ./module -run 'Test(ExplicitTraceHeader|TrackPipelineExecution|ParallelStep|ForEachStep|TemplateEngine_Func|Scan)'"
  result: pass
supersedes: []
superseded_by: []
---

# Fan-Out / Fan-In / Map-Reduce Design

## Problem

The workflow engine executes pipeline steps strictly sequentially. There is no way to:

- Run independent API calls or DB queries concurrently (e.g., fetch user + orders + inventory in parallel)
- Process large collections with bounded concurrency (e.g., send 1000 notifications with 10 workers)
- Aggregate collection data inline (sum, group, deduplicate) without writing a full `step.jq` expression

## Decision

**Approach A — step.parallel + enhanced step.foreach + collection template functions.**

Concurrency is opt-in at the step level. The pipeline executor stays sequential. Individual steps internally spawn goroutines using deep-copied PipelineContext instances, eliminating shared mutable state.

This is a non-breaking, additive change. Existing pipelines behave identically.

## Components

### 1. `step.parallel` — Fixed-Branch Fan-Out

Execute N named sub-steps concurrently, collect all results.

**Config:**

```yaml
- type: step.parallel
  name: fetch-all
  config:
    error_strategy: fail_fast  # fail_fast | collect_errors (default: fail_fast)
    steps:
      - name: users
        type: step.http_call
        config: { url: "https://api/users/{{ .id }}", method: GET }
      - name: orders
        type: step.db_query
        config: { query: "SELECT * FROM orders WHERE user_id = $1", args: ["{{ .id }}"] }
      - name: inventory
        type: step.http_call
        config: { url: "https://api/inventory", method: GET }
```

**Output:**

```json
{
  "results": {
    "users": { "body": {...}, "status": 200 },
    "orders": { "rows": [...], "count": 5 },
    "inventory": { "body": {...}, "status": 200 }
  },
  "errors": {},
  "completed": 3,
  "failed": 0
}
```

**Access pattern:**

```yaml
{{ index .steps "fetch-all" "results" "users" "body" }}
{{ index .steps "fetch-all" "results" "orders" "rows" }}
```

**Error strategies:**

- `fail_fast` (default): Cancel derived context on first error. `errors` map contains the first failure. Step returns error.
- `collect_errors`: Let all branches finish. Failed branches go into `errors` map, successful into `results`. Step returns error only if ALL branches fail.

**Implementation:**

- Each branch gets a deep copy of PipelineContext (same pattern as `ForEachStep.buildChildContext`)
- Goroutines write to pre-allocated result slots indexed by branch name — no shared mutable state
- Uses `sync.WaitGroup` for coordination, `context.WithCancel` for fail-fast
- Parent step merges all branch results into its output after WaitGroup completes
- Reuses `buildSubStep()` from `pipeline_step_resilience.go` for sub-step construction
- Uses lazy registry function pattern (same as foreach, retry, circuit breaker)

**Complexity:**

| Metric | Complexity |
|--------|-----------|
| Time | O(max(branch_duration)) — wall clock bounded by slowest branch |
| Space | O(branches × context_size) — deep copy of PipelineContext per branch |

### 2. Enhanced `step.foreach` — Concurrent Collection Processing

Add optional `concurrency` and `error_strategy` fields. When `concurrency` is set, items are processed by a bounded worker pool.

**Config:**

```yaml
- type: step.foreach
  name: send-notifications
  config:
    collection: users
    item_var: user
    concurrency: 10            # 0 or absent = sequential (backward compatible)
    error_strategy: fail_fast  # fail_fast | collect_errors (default: fail_fast)
    step:
      type: step.http_call
      config:
        url: "https://notify.example.com/send"
        method: POST
        body: '{"email": "{{ .user.email }}"}'
```

**Behavior:**

- `concurrency: 0` or absent → existing sequential behavior (100% backward compatible)
- `concurrency: N` → semaphore-based worker pool with N goroutines
- Each worker gets a child PipelineContext copy (existing `buildChildContext` pattern)
- Results collected in original order (slot-indexed, not arrival order) to maintain determinism
- `error_strategy: fail_fast` → cancel context on first error
- `error_strategy: collect_errors` → continue, mark failed items with `_error` key

**Output (unchanged format, new optional field):**

```json
{
  "results": [
    { "status": 200 },
    { "status": 200 },
    { "_error": "timeout", "_index": 2 }
  ],
  "count": 3,
  "error_count": 1
}
```

**Implementation:**

- Semaphore channel (`make(chan struct{}, concurrency)`) controls worker count
- Pre-allocated `results []any` slice indexed by item position preserves order
- `sync.WaitGroup` for completion, `context.WithCancel` for fail-fast
- `error_count` field added to output when `error_strategy: collect_errors`

**Complexity:**

| Metric | Complexity |
|--------|-----------|
| Time (sequential) | O(n × per_item) |
| Time (concurrent) | O(⌈n/c⌉ × per_item) where c = concurrency |
| Space (sequential) | O(context_size) — reuses single child context |
| Space (concurrent) | O(c × context_size) — one deep copy per active worker |

### 3. Collection Template Functions

New template functions for inline aggregation and transformation of slices:

| Function | Signature | Complexity | Description |
|----------|-----------|-----------|-------------|
| `sum` | `sum SLICE [KEY]` | O(n) | Sum numeric values. Optional KEY for maps. |
| `pluck` | `pluck SLICE KEY` | O(n) | Extract one field from each map in slice. |
| `flatten` | `flatten SLICE` | O(n×m) | Flatten one level of nested slices. n=outer, m=avg inner. |
| `unique` | `unique SLICE [KEY]` | O(n) | Deduplicate. Hash-map based, preserves insertion order. |
| `groupBy` | `groupBy SLICE KEY` | O(n) | Group maps by key value → `map[string][]any`. |
| `sortBy` | `sortBy SLICE KEY` | O(n log n) | Stable sort ascending by key. Uses `sort.SliceStable`. |
| `first` | `first SLICE` | O(1) | First element, nil if empty. |
| `last` | `last SLICE` | O(1) | Last element, nil if empty. |
| `min` | `min SLICE [KEY]` | O(n) | Minimum numeric value. |
| `max` | `max SLICE [KEY]` | O(n) | Maximum numeric value. |

All functions accept `[]any` and `[]map[string]any`. The optional `KEY` parameter extracts a map field for numeric operations. For simple scalar slices (e.g., `[1,2,3]`), `sum`/`min`/`max` work without a key.

**Examples:**

```yaml
# Sum all amounts
total: "{{ sum .steps.fetch-sales.rows \"amount\" }}"

# Group by region
by_region: "{{ json (groupBy .steps.fetch-sales.rows \"region\") }}"

# Get unique tags
tags: "{{ json (unique .steps.fetch-items.results \"category\") }}"

# Extract names
names: "{{ json (pluck .steps.fetch-users.results \"name\") }}"

# Top sale amount
top: "{{ max .steps.fetch-sales.rows \"amount\" }}"
```

## Scenarios

### Scenario 1 — API Gateway Aggregation

Fetch user profile from 3 microservices in parallel, merge into single response.

```yaml
steps:
  - type: step.request_parse
    name: parse
    config: { path_params: [id] }
  - type: step.parallel
    name: aggregate
    config:
      error_strategy: collect_errors
      steps:
        - name: profile
          type: step.http_call
          config: { url: "https://users/{{ .path_params.id }}" }
        - name: orders
          type: step.http_call
          config: { url: "https://orders?user={{ .path_params.id }}" }
        - name: recommendations
          type: step.http_call
          config: { url: "https://recs/{{ .path_params.id }}" }
  - type: step.json_response
    name: respond
    config:
      status_code: 200
      body: '{{ json .steps.aggregate.results }}'
```

### Scenario 2 — Batch Webhook Processing

Process incoming webhook with array of events using 20 concurrent workers.

```yaml
steps:
  - type: step.request_parse
    name: parse
    config: { parse_body: true }
  - type: step.foreach
    name: process-events
    config:
      collection: body.events
      item_var: event
      concurrency: 20
      error_strategy: collect_errors
      step:
        type: step.http_call
        config:
          url: "https://internal/process"
          method: POST
          body: '{{ json .event }}'
  - type: step.set
    name: summary
    config:
      values:
        total: "{{ .steps.process-events.count }}"
        errors: "{{ .steps.process-events.error_count }}"
```

### Scenario 3 — Map/Reduce Sales Report

Query sales data, aggregate with template functions.

```yaml
steps:
  - type: step.db_query
    name: fetch-sales
    config:
      query: "SELECT region, amount FROM sales WHERE date >= $1"
      args: ["{{ .start_date }}"]
      mode: list
  - type: step.set
    name: report
    config:
      values:
        total: '{{ sum .steps.fetch-sales.rows "amount" }}'
        by_region: '{{ json (groupBy .steps.fetch-sales.rows "region") }}'
        top_sale: '{{ max .steps.fetch-sales.rows "amount" }}'
        regions: '{{ json (unique .steps.fetch-sales.rows "region") }}'
```

### Scenario 4 — Scatter/Gather Validation

Run fraud, inventory, and credit checks in parallel; route based on results.

```yaml
steps:
  - type: step.request_parse
    name: parse
    config: { parse_body: true, path_params: [id] }
  - type: step.parallel
    name: checks
    config:
      error_strategy: fail_fast
      steps:
        - name: inventory
          type: step.http_call
          config: { url: "https://inventory/check/{{ .body.product_id }}" }
        - name: fraud
          type: step.http_call
          config: { url: "https://fraud/score/{{ .body.user_id }}" }
        - name: credit
          type: step.http_call
          config: { url: "https://credit/verify/{{ .body.user_id }}" }
  - type: step.conditional
    name: route
    config:
      field: steps.checks.results.fraud.risk_level
      routes:
        high: reject-order
        low: fulfill-order
      default: manual-review
```

## Non-Goals

- DAG executor / `depends_on` — may be added in a future version if use cases demand it
- Nested parallelism limits (step.parallel inside step.parallel) — allowed but users should use judgment
- Distributed fan-out across nodes — out of scope, single-process concurrency only

## Files Changed

| Action | File |
|--------|------|
| Create | `module/pipeline_step_parallel.go` |
| Create | `module/pipeline_step_parallel_test.go` |
| Modify | `module/pipeline_step_foreach.go` |
| Modify | `module/pipeline_step_foreach_test.go` |
| Modify | `module/pipeline_template.go` |
| Modify | `module/pipeline_template_test.go` |
| Modify | `plugins/pipelinesteps/plugin.go` |
| Modify | `schema/step_schema.go` |
| Modify | `schema/step_inference.go` |
| Modify | `DOCUMENTATION.md` |
