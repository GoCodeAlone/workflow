---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow-plugin-twilio
    commit: 1345186
  - repo: workflow-plugin-monday
    commit: 94ccd28
  - repo: workflow-plugin-turnio
    commit: 670fdbf
external_refs:
  - "workflow-scenarios: scenarios/52-monday-integration"
  - "workflow-scenarios: scenarios/53-turnio-integration"
  - "workflow-scenarios: scenarios/63-twilio-integration"
  - "workflow-plugin-auth: tags include v0.1.0+"
  - "workflow-plugin-security-scanner: tags include v0.1.0+"
verification:
  last_checked: 2026-04-25
  commands:
    - "git -C /Users/jon/workspace/workflow-plugin-twilio tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-monday tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-turnio tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-auth tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-security-scanner tag --list 'v*'"
    - "rg -n \"workflow-plugin-(twilio|monday|turnio)\" /Users/jon/workspace/workflow-scenarios"
  result: pass
supersedes: []
superseded_by: []
---

# Plugin Releases & Validation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Tag v0.1.0 releases on all untagged plugins, then validate every integration plugin with workflow-scenarios test scenarios.

**Architecture:** Tag releases trigger GoReleaser via GitHub Actions, producing cross-platform binaries. Validation scenarios use the workflow engine's external plugin loader with mock HTTP servers to test step execution end-to-end.

**Tech Stack:** GoReleaser v2, GitHub Actions, workflow-scenarios test harness, Go test framework.

---

## Task 1: Tag and Release Untagged Plugins

The following plugins need `v0.1.0` tags to trigger their first GoReleaser release:

| Plugin | Repo | Status |
|--------|------|--------|
| workflow-plugin-twilio | GoCodeAlone/workflow-plugin-twilio | no tags |
| workflow-plugin-monday | GoCodeAlone/workflow-plugin-monday | no tags |
| workflow-plugin-turnio | GoCodeAlone/workflow-plugin-turnio | no tags |
| workflow-plugin-auth | GoCodeAlone/workflow-plugin-auth | no tags |
| workflow-plugin-security-scanner | GoCodeAlone/workflow-plugin-security-scanner | no tags |

**Already tagged** (no action needed): admin v1.0.0, agent v0.3.1, authz v0.3.1, authz-ui v0.1.0, bento v1.0.0, data-protection v0.1.0, github v1.0.0, payments v0.1.0, sandbox v0.1.0, security v0.1.0, supply-chain v0.1.0, waf v0.1.0.

### Step 1: Verify each untagged plugin builds and tests pass

For each plugin in the untagged list:
```bash
cd /Users/jon/workspace/workflow-plugin-<name>
go vet ./...
go test ./... -count=1
go build -o /dev/null ./cmd/workflow-plugin-<name>
```

### Step 2: Ensure release workflow exists

Verify each repo has `.github/workflows/release.yml` and `.goreleaser.yml`. If missing, copy from `workflow-plugin-payments` and adjust the binary name.

### Step 3: Tag and push

For each plugin:
```bash
cd /Users/jon/workspace/workflow-plugin-<name>
git tag v0.1.0
git push origin v0.1.0
```

### Step 4: Verify releases

Wait for GitHub Actions to complete, then verify:
```bash
gh release view v0.1.0 --repo GoCodeAlone/workflow-plugin-<name>
```

Each release should have 4 archives (linux/darwin x amd64/arm64) plus checksums.txt.

---

## Task 2: Create Validation Scenarios for Wave 1 Plugins

Create 3 new workflow-scenarios (51-53) that test the Twilio, monday.com, and turn.io plugins with mock HTTP backends. Each scenario:

1. Uses the workflow engine with the external plugin loaded
2. Configures the plugin module with mock server URLs
3. Executes pipelines that call plugin steps
4. Validates step outputs

### Scenario Pattern

Each scenario directory:
```
scenarios/<id>-<name>/
├── scenario.yaml       # Metadata
├── config/app.yaml     # Workflow engine config with plugin module + pipelines
├── mock/server.go      # Go mock HTTP server for the service API
├── k8s/                # Kubernetes deployment (optional)
└── test/
    └── run.sh          # Test script with PASS:/FAIL: assertions
```

### Step 1: Create scenario 51-twilio-integration

**`scenarios/51-twilio-integration/scenario.yaml`:**
```yaml
name: Twilio Integration
id: "51-twilio-integration"
category: C
description: |
  Tests workflow-plugin-twilio step types against a mock Twilio API server.
  Validates SMS sending, message listing, verification, and call creation.
components:
  - workflow (engine)
  - workflow-plugin-twilio (external plugin)
  - mock Twilio API server
status: testable
version: "1.0"
image: workflow-server:local
port: 8080
tests:
  type: bash
  script: test/run.sh
```

**`scenarios/51-twilio-integration/config/app.yaml`:**
- Plugin: `workflow-plugin-twilio` binary from PATH or data dir
- Module: `twilio.provider` with `accountSid`, `authToken`, mock server base URL override
- Pipelines testing: `send_sms` → verify output has `sid`, `status`; `list_messages` → verify output has messages array; `send_verification` → verify `status: pending`; `create_call` → verify `sid` returned

**`scenarios/51-twilio-integration/test/run.sh`:**
- Start mock server (Go binary that returns canned Twilio JSON responses)
- POST to pipeline endpoints
- Assert response contains expected fields
- Tests: send_sms, send_mms, list_messages, fetch_message, send_verification, check_verification, create_call, list_calls, lookup_phone (9 tests minimum)

### Step 2: Create scenario 52-monday-integration

Same pattern but for monday.com:
- Mock GraphQL server returning canned monday.com responses
- Pipelines testing: `create_board`, `list_boards`, `create_item`, `list_items`, `create_group`, `query` (generic)
- 8 tests minimum

### Step 3: Create scenario 53-turnio-integration

Same pattern but for turn.io:
- Mock REST server returning WhatsApp message responses + rate limit headers
- Pipelines testing: `send_text`, `send_template`, `check_contact`, `list_templates`, `create_flow`
- Verify rate limit header tracking
- 6 tests minimum

### Step 4: Run tests locally

```bash
cd /Users/jon/workspace/workflow-scenarios
make test SCENARIO=51-twilio-integration
make test SCENARIO=52-monday-integration
make test SCENARIO=53-turnio-integration
```

All tests must pass.

### Step 5: Commit and push

```bash
cd /Users/jon/workspace/workflow-scenarios
git add scenarios/51-twilio-integration scenarios/52-monday-integration scenarios/53-turnio-integration
git commit -m "feat: add integration plugin validation scenarios (51-53)"
git push
```

---

## Task 3: Update scenarios.json Registry

Add entries for the 3 new scenarios to `scenarios.json`:

```json
{
  "id": "51-twilio-integration",
  "name": "Twilio Integration",
  "category": "C",
  "status": "testable"
},
{
  "id": "52-monday-integration",
  "name": "monday.com Integration",
  "category": "C",
  "status": "testable"
},
{
  "id": "53-turnio-integration",
  "name": "turn.io Integration",
  "category": "C",
  "status": "testable"
}
```

---

## Task 4: Validate Wave 2 Plugins (after wave 2 implementation)

After wave 2 plugins are built, create scenarios 54-59:

| Scenario | Plugin | Key Tests |
|----------|--------|-----------|
| 54-okta-integration | okta | user CRUD, group membership, app assignment, MFA enrollment, auth server config |
| 55-datadog-integration | datadog | metric submit/query, monitor CRUD, event creation, log search, SLO lifecycle |
| 56-launchdarkly-integration | launchdarkly | flag CRUD, project/environment management, segment operations, context evaluation |
| 57-permit-integration | authz (permit provider) | RBAC check, user/role CRUD, resource management, relationship tuples, condition sets |
| 58-salesforce-integration | salesforce | record CRUD, SOQL query, bulk operations, composite requests, approval process |
| 59-openlms-integration | openlms | user/course CRUD, enrollment, grades, quiz lifecycle, assignment submission |

Same mock server pattern as wave 1.

---

## Key Reference Files

| File | Purpose |
|------|---------|
| `/Users/jon/workspace/workflow-scenarios/CLAUDE.md` | Scenario harness conventions |
| `/Users/jon/workspace/workflow-scenarios/scenarios/48-payment-processing/` | Reference scenario structure |
| `/Users/jon/workspace/workflow-plugin-payments/.goreleaser.yml` | GoReleaser config template |
| `/Users/jon/workspace/workflow-plugin-payments/.github/workflows/release.yml` | Release workflow template |
