# Design: `step.graphql` — Generic GraphQL Step

**Date:** 2026-03-12
**Status:** Approved
**Scope:** Workflow engine (`step.graphql`) + BMW BwP migration

## Summary

Add a full-featured `step.graphql` pipeline step to the workflow engine. Reuses `step.http_call`'s existing OAuth2 token cache internally. Replaces BMW's 5 custom BwP plugin steps with generic GraphQL queries in app.yaml.

## Motivation

- BMW's Buy with Prime integration has 5 custom plugin steps that are just thin wrappers around GraphQL + OAuth2 client credentials
- The OAuth2 token management in the BwP plugin duplicates `step.http_call`'s existing `oauth2_client_credentials` support (process-wide cache, singleflight dedup, 401 retry)
- Zero GraphQL support exists in the engine — users must write custom plugin code for any GraphQL API
- A generic `step.graphql` makes GraphQL APIs (Shopify, GitHub, Stripe Billing, BwP, Contentful, Hasura, etc.) accessible via YAML config

## Architecture

### File Layout

```
module/pipeline_step_graphql.go       — Step implementation
module/pipeline_step_graphql_test.go  — Unit tests
plugins/pipelinesteps/plugin.go       — Registration (add to StepFactories)
```

### Internal Dependencies

- Reuses `oauthTokenCache` + `oauthSingleflight` from `pipeline_step_http_call.go` for OAuth2 token management
- Reuses `getOAuth2Token()` / `getOAuth2TokenWithConfig()` for token acquisition
- No new auth infrastructure needed

## Config Schema

```yaml
- name: my_query
  type: step.graphql
  config:
    # === Required ===
    url: "https://api.example.com/graphql"
    query: |
      query GetUser($id: ID!) {
        user(id: $id) { name email }
      }

    # === Variables (template-resolved) ===
    variables:
      id: '{{ .user_id }}'

    # === Response Extraction ===
    data_path: "user"    # dot-path into response.data; omit for full data

    # === Authentication ===
    auth:
      type: oauth2_client_credentials  # or "bearer", "api_key", "basic"
      token_url: "https://auth.example.com/token"
      client_id: '{{ config "client_id" }}'
      client_secret: '{{ config "client_secret" }}'
      scopes: ["api.read"]

    # === Headers (optional) ===
    headers:
      X-Custom: "value"

    # === Fragments (optional, prepended to query) ===
    fragments:
      - |
        fragment UserFields on User {
          id name email
        }

    # === Pagination (optional) ===
    pagination:
      strategy: cursor             # "cursor" or "offset"
      page_info_path: "users.pageInfo"
      cursor_variable: "after"
      has_next_field: "hasNextPage"
      cursor_field: "endCursor"
      max_pages: 10

    # === Batch Queries (optional) ===
    batch:
      queries:
        - query: "query A { ... }"
          variables: { ... }
        - query: "query B { ... }"
          variables: { ... }

    # === Automatic Persisted Queries (optional) ===
    persisted_query:
      enabled: true
      sha256: ""   # auto-computed if empty

    # === Introspection (optional) ===
    introspection:
      enabled: true   # sends standard introspection query, overrides query field

    # === Error Handling ===
    fail_on_graphql_errors: true   # default true
    timeout: "30s"
    retry_on_network_error: false
```

## Step Output

### Standard Query/Mutation

```
data        — Extracted via data_path (or full response.data if omitted)
errors      — GraphQL errors array (empty if none)
raw         — Full GraphQL response {data, errors, extensions}
status_code — HTTP status code
has_errors  — true if GraphQL errors present
extensions  — GraphQL extensions object (rate limits, tracing, etc.)
```

### Pagination Mode

```
data        — Merged array from all pages
page_count  — Number of pages fetched
total_items — Total items across all pages
errors      — Aggregated errors from all pages
```

### Batch Mode

```
results — Array of {data, errors} per query
```

### Introspection Mode

```
schema — Parsed __schema object
types  — Extracted type list
```

## GraphQL Error Handling

GraphQL APIs return HTTP 200 even on errors. The step:

1. Parses response body as JSON `{data, errors, extensions}`
2. If `errors` array present and `fail_on_graphql_errors: true` → returns Go error with first GraphQL error message + all errors in detail
3. If `errors` array present and `fail_on_graphql_errors: false` → populates output with both `data` and `errors`, sets `has_errors: true`
4. If HTTP status non-200 → returns Go error (network/auth failure)
5. OAuth2 401 → invalidates cached token, retries once (same pattern as `step.http_call`)

## Pagination Implementation

### Cursor-Based (Relay Connections)

```
1. Execute query with initial variables
2. Extract pageInfo from response via page_info_path
3. If hasNextPage → set cursor_variable = endCursor, re-execute
4. Merge data arrays across pages
5. Stop at max_pages or !hasNextPage
```

### Offset-Based

```
1. Execute query with offset=0
2. If result count == limit → offset += limit, re-execute
3. Merge arrays, stop at max_pages
```

## APQ (Automatic Persisted Queries)

```
1. Compute SHA-256 hash of query string
2. Send request with only hash (no query body)
3. If server returns PersistedQueryNotFound error → resend with hash + full query
4. Cache successful hash associations for future requests
```

## BMW Migration Plan

Replace 5 BwP plugin steps with `step.graphql` in app.yaml:

| Plugin Step | Replacement | Type |
|---|---|---|
| `step.bmw.bwp_delivery_preview` | `step.graphql` + DeliveryPreview query | Query |
| `step.bmw.bwp_create_order` | `step.graphql` + CreateOrder mutation | Mutation |
| `step.bmw.bwp_order_status` | `step.graphql` + OrderStatus query | Query |
| `step.bmw.bwp_cancel_order` | `step.graphql` + CancelOrder mutation | Mutation |
| `step.bmw.bwp_process_return` | `step.graphql` + ProcessReturn mutation | Mutation (NEW pipeline) |

Shared BwP OAuth2 credentials via `config.provider`:
```yaml
# Already in BMW app.yaml
config:
  bwp_api_url: ${BWP_API_URL}
  bwp_token_url: ${BWP_TOKEN_URL}
  bwp_client_id: ${BWP_CLIENT_ID}
  bwp_client_secret: ${BWP_CLIENT_SECRET}
```

Each migrated step uses:
```yaml
auth:
  type: oauth2_client_credentials
  token_url: '{{ config "bwp_token_url" }}'
  client_id: '{{ config "bwp_client_id" }}'
  client_secret: '{{ config "bwp_client_secret" }}'
```

After migration, the 5 `step.bmw.bwp_*` step types and `bwpClient` singleton can be removed from the BMW plugin.

## Testing Strategy

- Unit tests with `httptest` server mocking GraphQL responses
- Test cases:
  - Basic query + variables
  - data_path extraction (nested paths)
  - GraphQL error handling (partial data, full failure)
  - fail_on_graphql_errors: false (partial data passthrough)
  - Cursor pagination (multi-page merge)
  - Offset pagination
  - Batch queries
  - APQ negotiation (miss → resend)
  - OAuth2 token flow (cache hit, 401 retry)
  - Fragment prepending
  - Introspection query
  - Timeout handling
  - Template resolution in variables

## Decisions

- **Standalone step over extending step.http_call**: Cleaner API surface, GraphQL-specific config schema, easier to document
- **Single step over step family**: GraphQL features are tightly coupled (pagination re-executes same query, APQ needs hash negotiation) — doesn't decompose cleanly
- **Full migration over hybrid**: Proves step.graphql works end-to-end, removes custom code from BMW plugin
- **Reuse existing OAuth2 cache**: No new auth infrastructure — `step.http_call`'s token cache is process-wide and already battle-tested
