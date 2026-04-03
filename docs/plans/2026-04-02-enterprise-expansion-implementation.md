# Enterprise Expansion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build 6 external gRPC plugins — CRM, ERP, Vector Store, SSO, Audit, Approval — following the enterprise expansion design.

**Architecture:** Each plugin is a standalone gRPC binary using the external SDK pattern (`sdk.Serve()`). Repos created from `workflow-plugin-template` under the GoCodeAlone org.

**Tech Stack:** Go 1.26, workflow SDK v0.3.56, `PramithaMJ/salesforce/v2` (CRM), `pinecone-io/go-pinecone/v5` (vectors), `milvus-io/milvus/client` (vectors), `coreos/go-oidc/v3` + `golang.org/x/oauth2` (SSO), `aws/aws-sdk-go-v2` (audit S3), custom OData HTTP client (ERP/SAP).

**Repos:** All created under `GoCodeAlone/` from the `workflow-plugin-template` repository template.

**Parallelism:** Phase 0's two plugin upgrades can run concurrently. Phases 1-3 each contain two independent plugins that can be built concurrently by separate agents. Within each plugin, module implementation and step implementation can be parallelized after the scaffolding task completes.

---

## Phase 0: Existing Plugin SDK Upgrades (Prerequisite)

Phase 0 migrates the two existing vendor plugins from custom HTTP clients to production-grade SDKs. This is a prerequisite — the CRM and SSO plugins will import these as Go library dependencies.

### Task 0.1: Migrate workflow-plugin-salesforce to PramithaMJ SDK

**Current state:** Custom `salesforceClient` struct in `internal/client.go` — manual OAuth2 (`client_credentials` flow only), manual `http.Do()` per request, no retry/backoff, no typed errors, no SOQL builder.

**Target state:** Replace `salesforceClient` with `github.com/PramithaMJ/salesforce/v2`. All 72 existing step types continue to work.

**Step 1: Add SDK dependency**

```bash
cd workflow-plugin-salesforce
go get github.com/PramithaMJ/salesforce/v2
```

**Step 2: Create exported client package**

Move core types to a new top-level `salesforce/` package (exported, not `internal/`):

```go
// salesforce/provider.go
package salesforce

import (
    sf "github.com/PramithaMJ/salesforce/v2"
)

// Provider wraps the PramithaMJ Salesforce client for use by other plugins.
type Provider struct {
    Client *sf.Client
}

// Config holds connection parameters.
type Config struct {
    AuthType      string // "oauth_refresh", "password", "client_credentials", "access_token"
    ClientID      string
    ClientSecret  string
    RefreshToken  string
    Username      string
    Password      string
    SecurityToken string
    AccessToken   string
    InstanceURL   string
    LoginURL      string
    APIVersion    string
    Sandbox       bool
}

// NewProvider creates a connected Salesforce provider.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
    // ... construct sf.Client with appropriate WithXxx options
}
```

**Step 3: Refactor internal steps to use SDK**

Replace all `salesforceClient.do(method, path, body)` calls with PramithaMJ SDK equivalents:
- `client.SObjects().Get/Create/Update/Delete/Upsert()`
- `client.Query().Execute()` / `client.Query().NewBuilder()`
- `client.Search().Execute()`
- `client.Bulk().CreateJob()` / `UploadCSV()` / `WaitForCompletion()`
- `client.Composite().Execute()`
- `client.Tooling().ExecuteAnonymous()` / `Query()`
- `client.Analytics().RunReport()` / `ListDashboards()`
- `client.Chatter().GetNewsFeed()` / `PostFeedElement()`
- `client.Limits().GetLimits()`
- `client.Apex().GetJSON()` / `PostJSON()`

**Step 4: Add auth flow support**

The PramithaMJ SDK supports `WithOAuthRefresh`, `WithPasswordAuth`, `WithCredentials`, `WithAccessToken`, and `WithSandbox`. Map all existing auth config options to SDK options. This expands auth support beyond the current `client_credentials`-only flow.

**Step 5: Leverage SDK error handling**

Replace generic error returns with SDK typed errors for better step output:
```go
if types.IsNotFoundError(err) { /* 404 */ }
if types.IsRateLimitError(err) { /* 429, retry */ }
if types.IsAuthError(err) { /* re-auth needed */ }
```

**Step 6: Run all existing tests, fix any breakage**

All 72 step types must continue to work. The SDK changes are internal — step configs and outputs remain the same.

### Task 0.2: Migrate workflow-plugin-okta to Official SDK

**Current state:** Custom `OktaClient` struct in `internal/helpers.go` — manual `oktaGet/Post/Put/Delete` HTTP helpers, no pagination, no rate limiting.

**Target state:** Replace `OktaClient` with `github.com/okta/okta-sdk-golang/v6`. All 131 existing step types continue to work.

**Step 1: Add SDK dependency**

```bash
cd workflow-plugin-okta
go get github.com/okta/okta-sdk-golang/v6
```

**Step 2: Create exported client package**

```go
// okta/provider.go
package okta

import (
    oktasdk "github.com/okta/okta-sdk-golang/v6/okta"
)

// Provider wraps the official Okta SDK client for use by other plugins.
type Provider struct {
    Client *oktasdk.Client
}

type Config struct {
    Domain    string
    APIToken  string
    // OR OAuth2:
    ClientID     string
    PrivateKey   string
    AuthServerID string
}

func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
    // ... construct okta.Client
}
```

**Step 3: Refactor internal steps to use SDK**

Replace all `oktaGet/oktaPost/oktaPut/oktaDelete` calls with typed SDK methods. The SDK provides domain-specific methods (e.g., `client.User.ListUsers()`, `client.Group.CreateGroup()`) instead of raw HTTP.

**Step 4: Run all existing tests, fix any breakage**

All 131 step types must continue to work.

---

## Phase 1: CRM + Approval

### Task 1.1: Scaffold workflow-plugin-crm

**Files to create:**
- `workflow-plugin-crm/go.mod`
- `workflow-plugin-crm/cmd/workflow-plugin-crm/main.go`
- `workflow-plugin-crm/internal/plugin.go`
- `workflow-plugin-crm/internal/crm.go`
- `workflow-plugin-crm/plugin.json`
- `workflow-plugin-crm/CLAUDE.md`
- `workflow-plugin-crm/.goreleaser.yaml`
- `workflow-plugin-crm/.github/workflows/ci.yml`
- `workflow-plugin-crm/.github/workflows/release.yml`
- `workflow-plugin-crm/Makefile`
- `workflow-plugin-crm/LICENSE`

**Step 1: Create repo from template**

```bash
gh repo create GoCodeAlone/workflow-plugin-crm \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Vendor-neutral CRM plugin (Salesforce via PramithaMJ SDK)" \
  --clone
cd workflow-plugin-crm
```

**Step 2: Update go.mod**

```go
module github.com/GoCodeAlone/workflow-plugin-crm

go 1.26

require (
    github.com/GoCodeAlone/workflow v0.3.56
    github.com/GoCodeAlone/workflow-plugin-salesforce v0.1.0
)
```

The CRM plugin does **not** depend on PramithaMJ SDK directly — it depends on `workflow-plugin-salesforce`, which itself uses the SDK. This keeps the dependency chain clean.

**Step 3: Write main.go**

```go
// cmd/workflow-plugin-crm/main.go
package main

import (
    "github.com/GoCodeAlone/workflow-plugin-crm/internal"
    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

var version = "dev"

func main() {
    sdk.Serve(internal.NewCRMPlugin(version))
}
```

**Step 4: Write CRM provider interface (internal/crm.go)**

Define `CRMProvider` interface, `ProviderConfig`, `RecordResult`, `QueryResult`, `SearchResult`, `BulkOp`, `BulkResult`, `ObjectDescription`, `APILimits` types. See design doc for the full interface.

**Step 5: Write plugin.go**

Implement `PluginProvider`, `ModuleProvider`, `StepProvider`. Register `crm.provider` module type and all 10 step types. Follow the `workflow-plugin-auth` pattern: top-level `plugin.go` calls `internal.NewCRMPlugin()`.

**Step 6: Write plugin.json**

Use the manifest from the design doc's registry manifests section. Set `minEngineVersion: "0.3.56"`.

### Task 1.2: Implement Salesforce CRM Adapter

**Files:**
- `workflow-plugin-crm/internal/salesforce_adapter.go`
- `workflow-plugin-crm/internal/salesforce_adapter_test.go`
- `workflow-plugin-crm/internal/module_provider.go`
- `workflow-plugin-crm/internal/module_provider_test.go`

**Prerequisite:** Task 0.1 (salesforce plugin migrated to PramithaMJ SDK and exporting `salesforce.Provider`).

**Step 1: Add salesforce plugin as Go dependency**

```bash
cd workflow-plugin-crm
go get github.com/GoCodeAlone/workflow-plugin-salesforce
```

**Step 2: Implement `salesforceAdapter` struct**

The adapter implements `CRMProvider` by wrapping the Salesforce plugin's exported `salesforce.Provider`:

```go
package internal

import (
    "context"
    sfprovider "github.com/GoCodeAlone/workflow-plugin-salesforce/salesforce"
)

type salesforceAdapter struct {
    provider *sfprovider.Provider
}

func newSalesforceAdapter() *salesforceAdapter {
    return &salesforceAdapter{}
}

func (a *salesforceAdapter) Connect(ctx context.Context, config ProviderConfig) error {
    p, err := sfprovider.NewProvider(ctx, sfprovider.Config{
        AuthType:      config.Auth.Type,
        ClientID:      config.Auth.ClientID,
        ClientSecret:  config.Auth.ClientSecret,
        RefreshToken:  config.Auth.RefreshToken,
        Username:      config.Auth.Username,
        Password:      config.Auth.Password,
        AccessToken:   config.Auth.AccessToken,
        InstanceURL:   config.Auth.InstanceURL,
        APIVersion:    config.APIVersion,
        Sandbox:       config.Sandbox,
    })
    if err != nil {
        return fmt.Errorf("crm salesforce connect: %w", err)
    }
    a.provider = p
    return nil
}
```

Each `CRMProvider` method delegates to the Salesforce provider's client (which uses PramithaMJ SDK internally):
- `CreateRecord` → `a.provider.Client.SObjects().Create(ctx, objectType, fields)`
- `Query` → `a.provider.Client.Query().Execute(ctx, query)`
- `BulkOperation` → `a.provider.Client.Bulk().CreateJob()` + `UploadCSV()` + `WaitForCompletion()`

The adapter is thin — mapping CRM types to Salesforce types. Bug fixes and new features in the Salesforce plugin flow through automatically.

### Task 1.3: Implement CRM Step Types

**Files:**
- `workflow-plugin-crm/internal/step_record.go` (create, update, upsert, delete, get)
- `workflow-plugin-crm/internal/step_query.go` (query, search)
- `workflow-plugin-crm/internal/step_bulk.go` (bulk_import)
- `workflow-plugin-crm/internal/step_describe.go` (describe, limits)
- `workflow-plugin-crm/internal/step_*_test.go` (one test file per step file)

Each step follows the SDK pattern:
1. Parse config in constructor → return `sdk.StepInstance`
2. `Execute(ctx, input)` resolves the `CRMProvider` from the module registry by `moduleName`
3. Call the appropriate provider method
4. Return `sdk.StepResult{Output: map[string]any{...}}`

Handle errors with the PramithaMJ SDK's typed errors:
- `types.IsNotFoundError(err)` → output `{error: "not_found", ...}`
- `types.IsRateLimitError(err)` → output `{error: "rate_limited", retryAfter: ...}`
- `types.IsAuthError(err)` → output `{error: "auth_failed", ...}`

### Task 1.4: Scaffold workflow-plugin-approval

**Same scaffolding pattern as Task 1.1** but for `workflow-plugin-approval`.

```bash
gh repo create GoCodeAlone/workflow-plugin-approval \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Human-in-the-loop approval workflows" \
  --clone
```

**go.mod dependencies:** Only `github.com/GoCodeAlone/workflow v0.3.56` (no external SDKs — uses engine's own state machine and database).

### Task 1.5: Implement Approval Engine

**Files:**
- `workflow-plugin-approval/internal/approval.go` — types: `ApprovalRequest`, `ApprovalDecision`, `ApprovalStatus` enum
- `workflow-plugin-approval/internal/store.go` — database persistence using `sdk.ServiceInvoker` to call the host engine's `database.workflow` module
- `workflow-plugin-approval/internal/state_machine.go` — state transitions: `pending → approved|rejected|escalated|delegated|expired`
- `workflow-plugin-approval/internal/module_engine.go` — `approval.engine` module
- `workflow-plugin-approval/internal/trigger_webhook.go` — `trigger.approval_webhook`

The approval store persists to the host engine's database via `ServiceInvoker.InvokeMethod("database.workflow", "exec", ...)`. Schema:

```sql
CREATE TABLE IF NOT EXISTS approval_requests (
    id TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    approvers TEXT NOT NULL,           -- JSON array
    required_approvals INTEGER DEFAULT 1,
    status TEXT DEFAULT 'pending',
    decisions TEXT DEFAULT '[]',       -- JSON array of {actor, decision, comment, timestamp}
    metadata TEXT DEFAULT '{}',        -- JSON
    continuation_token TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Task 1.6: Implement Approval Steps + Trigger

**Files:**
- `workflow-plugin-approval/internal/step_request.go` — creates DB record, publishes EventBus notification, returns `{requestId, status: "suspended", continuationToken}`
- `workflow-plugin-approval/internal/step_check.go` — queries DB for current status
- `workflow-plugin-approval/internal/step_decide.go` — records decision, transitions state machine
- `workflow-plugin-approval/internal/step_list.go` — lists pending approvals by approver
- `workflow-plugin-approval/internal/step_escalate.go` — reassigns approvers
- `workflow-plugin-approval/internal/trigger_webhook.go` — HTTP POST handler at `/api/approvals/:id/decide`
- Tests for all steps

### Task 1.7: Validation Scenario 1

Create `workflow-scenarios/enterprise-lead-enrichment/` with:
- `config.yaml` — full pipeline config from the design doc
- `docker-compose.yml` — if external services needed (mock Salesforce API)
- `scenario_test.go` — Go test that loads the config, sends a webhook, verifies CRM record created and approval flow works

---

## Phase 2: Vector Store + SSO

### Task 2.1: Scaffold workflow-plugin-vectorstore

```bash
gh repo create GoCodeAlone/workflow-plugin-vectorstore \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Vector database integration (Pinecone, Milvus)" \
  --clone
```

**go.mod dependencies:**

```go
require (
    github.com/GoCodeAlone/workflow v0.3.56
    github.com/pinecone-io/go-pinecone/v5 v5.0.0
    github.com/milvus-io/milvus/client v0.0.0  // use latest commit hash
)
```

### Task 2.2: Implement Pinecone Adapter

**Files:**
- `internal/vectorstore.go` — `VectorStoreProvider` interface + shared types
- `internal/pinecone_adapter.go` — wraps `go-pinecone/v5/pinecone`
- `internal/pinecone_adapter_test.go`

Map provider interface methods:
- `Connect` → `pinecone.NewClient(pinecone.NewClientParams{ApiKey: ...})`
- `Upsert` → `idx.UpsertVectors(ctx, vectors)`
- `Query` → `idx.QueryByVectorValues(ctx, ...)`
- `Fetch` → `idx.FetchVectors(ctx, ids)`
- `Delete` → `idx.DeleteVectorsById(ctx, ids)` or `idx.DeleteVectorsByFilter(ctx, filter)`
- `CreateIndex` → `pc.CreateServerlessIndex(ctx, ...)` or `pc.CreatePodIndex(ctx, ...)`
- `ListIndexes` → `pc.ListIndexes(ctx)`
- `DescribeIndex` → `pc.DescribeIndex(ctx, name)`

### Task 2.3: Implement Milvus Adapter

**Files:**
- `internal/milvus_adapter.go` — wraps `milvus-io/milvus/client`
- `internal/milvus_adapter_test.go`

Note: The SDK moved to the main `milvus-io/milvus` repository. Import path is `github.com/milvus-io/milvus/client/v2` (verify latest at implementation time).

### Task 2.4: Implement Vector Store Steps

**Files:** `internal/step_upsert.go`, `step_query.go`, `step_fetch.go`, `step_delete.go`, `step_index.go` + tests.

Each step resolves the `VectorStoreProvider` from the module registry and delegates. The `step.vector_query` step should normalize output format across Pinecone/Milvus so downstream steps see consistent `{matches: [{id, score, metadata}]}`.

### Task 2.5: Scaffold workflow-plugin-sso

```bash
gh repo create GoCodeAlone/workflow-plugin-sso \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Enterprise SSO via OpenID Connect (Entra ID, Okta)" \
  --clone
```

**go.mod dependencies:**

```go
require (
    github.com/GoCodeAlone/workflow v0.3.56
    github.com/GoCodeAlone/workflow-plugin-okta v0.1.0
    github.com/coreos/go-oidc/v3 v3.12.0
    golang.org/x/oauth2 v0.28.0
)
```

The SSO plugin depends on `workflow-plugin-okta` for Okta management operations (user lookup, group membership) and `go-oidc/v3` for OIDC token validation.

### Task 2.6: Implement OIDC Module

**Prerequisite:** Task 0.2 (okta plugin migrated to official SDK and exporting `okta.Provider`).

**Files:**
- `internal/oidc.go` — multi-provider registry, JWKS caching, claim mapping
- `internal/module_oidc.go` — `sso.oidc` module: on init, discovers all configured providers via `go-oidc`'s `provider.NewProvider(ctx, issuer)`, caches JWKS
- `internal/entra_provider.go` — Entra ID specifics: tenant-based issuer URL, v2.0 endpoint, `groups` claim from ID token
- `internal/okta_provider.go` — Okta specifics: org auth server vs custom auth server
- `internal/generic_provider.go` — any OIDC-compliant IdP
- `internal/claims.go` — claim mapping logic: extract roles/groups from configurable claim paths

The `sso.oidc` module config:

```go
type OIDCModuleConfig struct {
    Providers    []ProviderConfig  `yaml:"providers"`
    ClaimMapping ClaimMapping      `yaml:"claimMapping"`
    SessionTTL   string           `yaml:"sessionTTL"`
}

type ProviderConfig struct {
    Name         string   `yaml:"name"`
    Issuer       string   `yaml:"issuer"`
    ClientID     string   `yaml:"clientId"`
    ClientSecret string   `yaml:"clientSecret"`
    Scopes       []string `yaml:"scopes"`
}
```

### Task 2.7: Implement SSO Steps

**Files:** `internal/step_validate.go`, `step_userinfo.go`, `step_groups.go`, `step_refresh.go` + tests.

`step.sso_validate_token`:
1. Extract token from config or from `Authorization: Bearer` header in trigger context
2. Determine provider (explicit or auto-detect from token's `iss` claim)
3. Call `verifier.Verify(ctx, rawToken)` using `go-oidc`
4. Apply claim mapping
5. Output: `{valid: true, userId, email, roles, groups, claims, provider}`

### Task 2.8: Validation Scenario 2

Create `workflow-scenarios/enterprise-rag-pipeline/` with the RAG pipeline config from the design doc. Mock the Entra ID OIDC discovery endpoint and Pinecone API for testing.

---

## Phase 3: ERP + Audit

### Task 3.1: Scaffold workflow-plugin-erp

```bash
gh repo create GoCodeAlone/workflow-plugin-erp \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Enterprise ERP integration (SAP S/4HANA)" \
  --clone
```

**go.mod dependencies:** Only `github.com/GoCodeAlone/workflow v0.3.56` (custom HTTP client, no external ERP SDK).

### Task 3.2: Implement OData Client + SAP Adapter

**Files:**
- `internal/odata_client.go` — Generic OData v4 HTTP client: entity CRUD, `$filter/$select/$expand/$orderby/$top/$skip`, `$batch`, function imports, metadata parsing
- `internal/sap_auth.go` — SAP-specific auth: OAuth2 token flow + X-CSRF token fetching (SAP requires GET `/` with `X-CSRF-Token: Fetch` header, then include returned token in mutating requests)
- `internal/sap_adapter.go` — Implements `ERPProvider` using `odata_client.go`
- Tests with `httptest.NewServer` mocking SAP OData responses

### Task 3.3: Implement ERP Steps

**Files:** `internal/step_entity.go` (read, create, update, delete), `step_query.go`, `step_batch.go`, `step_function.go`, `step_metadata.go` + tests.

### Task 3.4: Scaffold workflow-plugin-audit

```bash
gh repo create GoCodeAlone/workflow-plugin-audit \
  --template GoCodeAlone/workflow-plugin-template \
  --public \
  --description "Compliance audit logging (EventBus → S3/DB)" \
  --clone
```

**go.mod dependencies:**

```go
require (
    github.com/GoCodeAlone/workflow v0.3.56
    github.com/aws/aws-sdk-go-v2 v1.36.0
    github.com/aws/aws-sdk-go-v2/service/s3 v1.78.0
    github.com/aws/aws-sdk-go-v2/config v1.29.0
)
```

### Task 3.5: Implement Audit Collector + Sinks

**Files:**
- `internal/collector.go` — subscribes to EventBus topics via `sdk.MessageAwareModule`, buffers events in a ring buffer, flushes on interval or buffer full
- `internal/event.go` — `AuditEvent` struct: `{id, timestamp, eventType, workflowId, pipelineId, stepName, actor, correlationId, data, annotations}`
- `internal/sink.go` — `AuditSink` interface: `Write(ctx, []AuditEvent) error`, `Query(ctx, AuditQuery) ([]AuditEvent, error)`
- `internal/module_collector.go` — `audit.collector` module
- `internal/module_sink_s3.go` — `audit.sink.s3` module: batch writes JSONL/Parquet to S3 partitioned by date (`s3://bucket/prefix/2026/04/02/events-<uuid>.jsonl.gz`)
- `internal/module_sink_db.go` — `audit.sink.db` module: batch INSERTs to configurable table

The collector uses a dedicated goroutine for flushing — critical path pipeline execution is never blocked. Events are serialized to a channel with configurable buffer size.

### Task 3.6: Implement Audit Steps

**Files:** `internal/step_query.go`, `step_export.go`, `step_annotate.go` + tests.

`step.audit_annotate` adds key-value metadata to a goroutine-local (or context-carried) annotation map. The collector includes these annotations in all subsequent events for the current execution.

---

## Phase 4: Integration Testing + Scenarios

### Task 4.1: Cross-Plugin Scenario Tests

Add to `workflow-scenarios/`:
- `enterprise-lead-enrichment/` — Scenario 1 from design (CRM + Approval + AI)
- `enterprise-rag-pipeline/` — Scenario 2 from design (SSO + Vector Store + Audit)
- `enterprise-erp-sync/` — ERP data sync with audit trail

Each scenario directory contains:
- `config.yaml` — workflow engine config
- `docker-compose.yml` — mock services
- `scenario_test.go` — Go integration test
- `README.md` — scenario description

### Task 4.2: Registry Updates

Submit all 6 plugin manifests to `workflow-registry/` following the existing PR pattern. Update `v1/plugins.json` catalog.

### Task 4.3: Documentation

- Update `workflow/DOCUMENTATION.md` with new plugin references
- Add tutorial: `docs/tutorials/enterprise-crm-approval.md`
- Add tutorial: `docs/tutorials/rag-pipeline-sso.md`

---

## Spec Alignment Audit

This section documents corrections applied to the original enterprise design input after auditing against the actual codebase state as of 2026-04-02, and refined on 2026-04-03 based on the provider dependency model.

### Finding 1: Salesforce SDK — CORRECTED

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **SDK** | "Custom-build a lightweight API client" | Existing `workflow-plugin-salesforce` also uses a custom HTTP client (`internal/client.go`) with no SDK. User directive: use `PramithaMJ/salesforce/v2`. | **Migrate `workflow-plugin-salesforce` to PramithaMJ SDK (Phase 0).** The CRM plugin then imports the Salesforce plugin as a Go library dependency — inheriting SDK-quality code without duplicating it. |

### Finding 2: Okta SDK — CORRECTED (new finding)

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **SDK** | Design doesn't mention Okta SDK | Existing `workflow-plugin-okta` uses a custom HTTP client (`internal/registry.go` + `internal/helpers.go`: `oktaGet/Post/Put/Delete`). No pagination, no rate limiting. Official Go SDK exists: `okta/okta-sdk-golang/v6`. | **Migrate `workflow-plugin-okta` to official SDK (Phase 0).** The SSO plugin then imports the Okta plugin as a Go library dependency. SDK provides pagination, rate limiting, retry, and type-safe API coverage for all 131 step types. |

### Finding 3: Existing Salesforce Plugin — LEVERAGED (upgraded from "acknowledged")

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **Plugin** | Design proposes CRM plugin without leveraging existing work | `workflow-plugin-salesforce` has 72 step types covering records, SOQL, bulk, composite, tooling, Apex, reports, etc. | **CRM plugin depends on salesforce plugin as Go library.** Salesforce plugin exports its provider; CRM wraps it behind vendor-neutral interface. Bug fixes and features flow through automatically. No duplicate code. |

### Finding 4: Existing Okta Plugin — LEVERAGED (upgraded from "acknowledged")

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **Plugin** | Design says SSO should support "Okta" as a provider | `workflow-plugin-okta` has 131 step types covering users, groups, apps, auth servers, policies, etc. | **SSO plugin depends on okta plugin as Go library.** Okta plugin provides management API access; SSO adds OIDC validation middleware on top. Complementary, not overlapping. |

### Finding 5: Milvus SDK — CORRECTED

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **SDK** | "Use `milvus-io/milvus-sdk-go/v2`" | The `milvus-sdk-go` repository is **deprecated**. README states: "Go to github.com/milvus-io/milvus/tree/master/client for the newest Go SDK". | **Use `github.com/milvus-io/milvus/client/v2`** (or latest from main milvus repo). Verify exact import path at implementation time. |

### Finding 6: SAP ERP SDK — EVALUATED, CUSTOM JUSTIFIED

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **SDK** | "Custom-build the integration over SAP's OData REST APIs" | SAP Cloud SDK is Java/JavaScript only. No official or community-maintained Go SDK for SAP OData. | **Custom OData client confirmed as only viable path.** But we build a reusable `odata` package (generic OData v4 CRUD, `$filter/$select/$expand`, `$batch`, metadata parsing) that could serve future ERP providers. |

### Finding 7: Provider Dependency Model — NEW (not in original design)

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **Architecture** | Each plugin is fully independent | Existing plugins are Go modules with `internal/` packages — can't be imported from outside | **Vendor plugins must export public client API** (move core types to top-level package). New plugins import them as Go library dependencies (compile-time). This is a prerequisite refactoring step (Phase 0). |

### Finding 8: S3 Storage — REUSE PATTERNS

| | Original Design | Codebase Reality | Correction |
|---|---|---|---|
| **S3** | "Use `aws-sdk-go-v2`" | Already a direct dependency. `artifact/s3.go`, `module/s3_storage.go`, `module/pipeline_step_s3_upload.go` exist. | **Correct SDK. Audit S3 sink follows same init patterns** (credential chain, region config). External plugin imports `aws-sdk-go-v2` directly. |

### Finding 9: State Machine + EventBus — CONFIRMED

Both `statemachine.engine` and EventBus bridge exist and work as described. Approval plugin uses state machine patterns via `sdk.ServiceInvoker`. Audit collector subscribes to EventBus via `sdk.MessageAwareModule`. No corrections needed.

### Finding 10: Pinecone SDK Version — CONFIRMED

Pinecone Go SDK v5 confirmed as current: `github.com/pinecone-io/go-pinecone/v5/pinecone`. No correction needed.

---

## Summary

| Plugin | SDK / Approach | New Repo | Key Dependency |
|---|---|---|---|
| Salesforce (upgrade) | `PramithaMJ/salesforce/v2` | existing `workflow-plugin-salesforce` | PramithaMJ SDK (replaces custom HTTP) |
| Okta (upgrade) | `okta/okta-sdk-golang/v6` | existing `workflow-plugin-okta` | Official Okta SDK (replaces custom HTTP) |
| CRM | Imports `workflow-plugin-salesforce` | `workflow-plugin-crm` | Salesforce plugin (Go library dep) |
| ERP | Custom OData v4 HTTP client | `workflow-plugin-erp` | stdlib `net/http` (no Go SDK for SAP) |
| Vector Store | `go-pinecone/v5` + `milvus/client` | `workflow-plugin-vectorstore` | Pinecone + Milvus SDKs |
| SSO | Imports `workflow-plugin-okta` + `go-oidc/v3` | `workflow-plugin-sso` | Okta plugin + go-oidc |
| Audit | `aws-sdk-go-v2` + EventBus | `workflow-plugin-audit` | AWS SDK |
| Approval | Engine internals (state machine + DB) | `workflow-plugin-approval` | workflow SDK only |
