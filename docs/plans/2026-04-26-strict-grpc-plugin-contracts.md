---
status: in_progress
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 4150f78
  - repo: workflow
    commit: 8daa224
  - repo: workflow
    commit: 72d2477
  - repo: workflow
    commit: 5c135a0
  - repo: workflow
    commit: dd1b222
  - repo: workflow
    commit: 64c15fa
  - repo: workflow
    commit: 95e80ad
  - repo: workflow
    commit: e91187f
  - repo: workflow
    commit: eb53150
  - repo: workflow-plugin-ci-generator
    commit: 5c158ff154ce43d391473a4ed6cd3d3bf7788931
  - repo: workflow-plugin-approval
    commit: 48898f12c10d800d8b60cc9ee06e81c1580d3f01
  - repo: workflow-plugin-gitlab
    commit: 93eb57223b9401e8a3bc0812e71211cb3a3770fa
  - repo: workflow-plugin-marketplace
    commit: 7ce430e709160e4c77527bb3ce1ee8ff2dd22309
  - repo: workflow-plugin-infra
    commit: 6c1802de752f71b1eac895805e01e2e32f92bb5c
  - repo: workflow-plugin-rooms
    commit: 575a6171de8ae8ea436cd957b0c279f6dc0c3e34
  - repo: workflow-plugin-botdetect
    commit: 40b9a4c11937ce26c01eea292d67bc8c9d098211
  - repo: workflow-plugin-audit
    commit: a720577335c76422e5f732484de1864464e8183d
  - repo: workflow-plugin-sso
    commit: b94f3a661df9f5862b7b736b5824fda2b76d47e8
  - repo: workflow-plugin-ws-auth
    commit: 4a27b88580fc64109a5bd723d3dc0b837342dec3
  - repo: workflow-plugin-authz
    commit: 812905e426b50d8501e91772ea3227da554c654e
  - repo: workflow-plugin-security
    commit: ab653a4c8fe836e55c434706b64730fe428c6c01
  - repo: workflow-plugin-authz-ui
    commit: a84d965d4ceee35049f6b7ed1c11e103bde68f93
  - repo: workflow-plugin-auth
    commit: b7e892e9a05db871d469aedee86cd1b7e45660d2
external_refs:
  - "#76"
verification:
  last_checked: 2026-04-27
  commands:
    - GOWORK=off go test ./plugin/external/... ./cmd/wfctl -count=1
    - GOWORK=off go run ./cmd/wfctl audit plugins --repo-root "$WORKSPACE" --strict-contracts
    - GOWORK=off go run ./cmd/wfctl audit plans --dir docs/plans --fix-index
  result: partial
supersedes: []
superseded_by: []
---

# Strict gRPC Plugin Contracts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add additive proto-backed strict plugin contracts to Workflow, then migrate first-party plugin and application repos away from generic map-only plugin boundaries.

**Architecture:** Keep the existing external plugin lifecycle RPCs, add contract descriptors plus typed `Any` payloads beside legacy `Struct` fields, and expose SDK adapters that let plugin authors implement typed steps/modules while the host still manages plugins generically. Enforce strictness through `wfctl` validation first, then through runtime startup checks once repos have migrated.

**Tech Stack:** Go, gRPC, protobuf `Any`, existing Workflow external plugin SDK, `wfctl audit/validate`, GitHub PR review/CI loop.

---

## Implementation Checkpoint

Core Workflow support is implemented through `workflow eb53150`: additive proto descriptors, plugin-owned descriptor-set based dynamic codecs, typed SDK adapters, host-side strict dispatch, strict input projection, typed integer output normalization, strict module error surfacing, `wfctl` strict contract audit/validation, and source-checkout strict plugin scaffolding.

Downstream strict-contract migrations are merged for `workflow-plugin-ci-generator`, `workflow-plugin-approval`, `workflow-plugin-gitlab`, `workflow-plugin-marketplace`, `workflow-plugin-infra`, `workflow-plugin-rooms`, `workflow-plugin-botdetect`, `workflow-plugin-audit`, `workflow-plugin-sso`, `workflow-plugin-ws-auth`, `workflow-plugin-authz`, `workflow-plugin-security`, `workflow-plugin-authz-ui`, and `workflow-plugin-auth`.

The overall plan remains `in_progress` because downstream first-party plugin and application repos still need typed descriptors and adapters. Set `WORKSPACE` to the local checkout root that contains `workflow` and sibling plugin/application repositories; the strict audit against that workspace is expected to fail until those repos migrate.

## Migration Tracker

Last updated: 2026-04-27 15:00 America/New_York.

### Merged

| Repo | PR | Merge Commit | Notes |
|---|---:|---|---|
| workflow | #497 | 9d98ed5 | Core strict contract support merged. |
| workflow-plugin-ci-generator | #1 | 5c158ff154ce43d391473a4ed6cd3d3bf7788931 | Strict contracts merged. |
| workflow-plugin-approval | #1 | 48898f12c10d800d8b60cc9ee06e81c1580d3f01 | Strict contracts merged. |
| workflow-plugin-gitlab | #1 | 93eb57223b9401e8a3bc0812e71211cb3a3770fa | Strict contracts merged. |
| workflow-plugin-marketplace | #1 | 7ce430e709160e4c77527bb3ce1ee8ff2dd22309 | Strict contracts merged. |
| workflow-plugin-infra | #1 | 6c1802de752f71b1eac895805e01e2e32f92bb5c | Strict contracts merged. |
| workflow-plugin-rooms | #1 | 575a6171de8ae8ea436cd957b0c279f6dc0c3e34 | Strict contracts merged. |
| workflow-plugin-botdetect | #2 | 40b9a4c11937ce26c01eea292d67bc8c9d098211 | Strict contracts merged. |
| workflow-plugin-audit | #1 | a720577335c76422e5f732484de1864464e8183d | Strict contracts merged. |
| workflow-plugin-sso | #1 | b94f3a661df9f5862b7b736b5824fda2b76d47e8 | Strict contracts merged. |
| workflow-plugin-ws-auth | #1 | 4a27b88580fc64109a5bd723d3dc0b837342dec3 | Strict contracts merged. |
| workflow-plugin-authz | #19 | 812905e426b50d8501e91772ea3227da554c654e | Strict contracts merged. |
| workflow-plugin-security | #5 | ab653a4c8fe836e55c434706b64730fe428c6c01 | Copilot reviewed; checks green; admin merged. |
| workflow-plugin-authz-ui | #3 | a84d965d4ceee35049f6b7ed1c11e103bde68f93 | Copilot reviewed; checks green; admin merged. |
| workflow-plugin-auth | #7 | b7e892e9a05db871d469aedee86cd1b7e45660d2 | Copilot comments fixed; checks green; admin merged. |

### Open PRs / Monitoring

| Repo | PR | Head | Status | Next Action |
|---|---:|---|---|---|
| workflow-plugin-security-scanner | #1 | 18a67c3 | Copilot re-review clean; CodeQL green; `test` queued on self-hosted runner. | Merge when queued check passes. |

### Verified Locally, PR Not Opened Yet

| Repo | Branch / Worktree | Commit | Verification |
|---|---|---|---|
| workflow-plugin-admin | `branch: strict-contracts` | 1ffd6dd71892ce21c2ca11b8bd60a077085fb606 | Race tests, vet, tidy diff, diff check, JSON validation, `wfctl plugin validate -strict-contracts`, build passed. |
| workflow-plugin-agent | `branch: strict-contracts` | 17925772b257a6530a18af0051306fb797fbb29c | Focused tests, full tests, race tests, vet, tidy diff, diff check, JSON validation, `wfctl plugin validate -strict-contracts` passed. |
| workflow-plugin-azure | `branch: strict-contract` | 5ed96e2155a5d975bdcd135bbcc575f2234de000 | Focused tests, race tests, vet, tidy diff, diff check, JSON validation, `wfctl plugin validate -strict-contracts` passed. |

### Active Agent Work

| Repo | Agent | Status |
|---|---|---|
| workflow-plugin-aws | Helmholtz | Recommended provider-operation-only design approved; implementation resumed. |

### Backlog

| Batch | Repos |
|---|---|
| Provider / platform | workflow-plugin-broker, workflow-plugin-cicd, workflow-plugin-digitalocean, workflow-plugin-gcp, workflow-plugin-platform, workflow-plugin-supply-chain, workflow-plugin-tofu |
| SaaS / integration | workflow-plugin-crm, workflow-plugin-data-engineering, workflow-plugin-datadog, workflow-plugin-erp, workflow-plugin-github, workflow-plugin-launchdarkly, workflow-plugin-monday, workflow-plugin-okta, workflow-plugin-openlms, workflow-plugin-payments, workflow-plugin-salesforce, workflow-plugin-slack, workflow-plugin-teams, workflow-plugin-turnio, workflow-plugin-twilio, workflow-plugin-vectorstore |
| Game / world | workflow-plugin-dnd, workflow-plugin-economy, workflow-plugin-gameserver, workflow-plugin-moderation, workflow-plugin-steam, workflow-plugin-tournament, workflow-plugin-websocket, workflow-plugin-worldengine, workflow-plugin-worldsim |
| Templates / samples | workflow-plugin-bento, workflow-plugin-messaging-core, workflow-plugin-migrations, workflow-plugin-template, workflow-plugin-template-private |
| Application-owned plugins | workflow-dnd, workflow-cardgame, core-dump, buymywishlist |

### Task 1: Core Proto Contract Descriptors

**Files:**
- Modify: `plugin/external/proto/plugin.proto`
- Modify: `plugin/external/proto/plugin.pb.go`
- Modify: `plugin/external/proto/plugin_grpc.pb.go`
- Test: `plugin/external/sdk/grpc_server_test.go`
- Test: `plugin/external/adapter_test.go`

**Step 1: Write the failing tests**

Add tests that call a future `GetContractRegistry` RPC on a test plugin and assert descriptors include step type, config message, input message, output message, and strictness mode.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./plugin/external/... -run 'Test.*ContractRegistry' -count=1`

Expected: FAIL because `GetContractRegistry` and descriptor messages do not exist.

**Step 3: Implement additive proto fields**

Add `ContractRegistry`, `ContractDescriptor`, `ContractKind`, and `ContractMode`. Add `typed_config`, `typed_input`, and `typed_output` `google.protobuf.Any` fields beside existing `Struct` fields on create/execute/service messages.

**Step 4: Regenerate protobuf bindings**

Run the repo's established protobuf generation command. If no generator config exists, add the smallest documented `buf.yaml` and `buf.gen.yaml` needed to reproduce the existing Go output.

Expected: generated files compile and no unrelated proto output changes.

**Step 5: Run tests**

Run: `GOWORK=off go test ./plugin/external/... -count=1`

Expected: PASS.

**Step 6: Commit**

```bash
git add plugin/external/proto/plugin.proto plugin/external/proto/plugin.pb.go plugin/external/proto/plugin_grpc.pb.go plugin/external/sdk/grpc_server_test.go plugin/external/adapter_test.go
git commit -m "feat(plugin): add strict contract descriptors"
```

### Task 2: SDK Typed Adapter Helpers

**Files:**
- Modify: `plugin/external/sdk/interfaces.go`
- Create: `plugin/external/sdk/typed.go`
- Create: `plugin/external/sdk/typed_test.go`
- Modify: `plugin/external/sdk/grpc_server.go`

**Step 1: Write the failing tests**

Add a typed test step with concrete request/config/output proto messages. Assert the SDK adapter rejects a mismatched message type and executes successfully with the correct type.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./plugin/external/sdk -run 'TestTypedStep' -count=1`

Expected: FAIL because typed adapter helpers do not exist.

**Step 3: Implement typed helpers**

Add generic typed step/module adapter types that pack/unpack `proto.Message` values through `Any`, return clear errors on type mismatch, and keep legacy `StepInstance` compatibility.

**Step 4: Run tests**

Run: `GOWORK=off go test ./plugin/external/sdk -run 'TestTypedStep|TestGRPCServer' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add plugin/external/sdk/interfaces.go plugin/external/sdk/typed.go plugin/external/sdk/typed_test.go plugin/external/sdk/grpc_server.go
git commit -m "feat(sdk): add typed plugin contract adapters"
```

### Task 3: Host-Side Strict Execution Path

**Files:**
- Modify: `plugin/external/adapter.go`
- Modify: `plugin/external/remote_step.go`
- Modify: `plugin/external/remote_module.go`
- Modify: `plugin/external/convert.go`
- Test: `plugin/external/remote_step_test.go`
- Test: `plugin/external/adapter_test.go`

**Step 1: Write the failing tests**

Add a fake strict plugin client that advertises descriptors and requires typed payloads. Assert the host sends typed fields and fails closed when descriptors require typed payloads but only legacy `Struct` data is available.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./plugin/external -run 'TestRemote.*Strict|TestExternalPluginAdapter.*Contract' -count=1`

Expected: FAIL because the host ignores descriptors and only sends `Struct`.

**Step 3: Implement host descriptor cache and strict dispatch**

Fetch contract registry during adapter construction, cache descriptors by module/step/service type, and prefer typed fields when descriptors and generated codecs are present. Keep `Struct` only for `LEGACY_STRUCT` descriptors.

**Step 4: Run tests**

Run: `GOWORK=off go test ./plugin/external/... -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add plugin/external/adapter.go plugin/external/remote_step.go plugin/external/remote_module.go plugin/external/convert.go plugin/external/remote_step_test.go plugin/external/adapter_test.go
git commit -m "feat(plugin): enforce strict remote contract descriptors"
```

### Task 4: wfctl Audit And Validate Strict Contracts

**Files:**
- Modify: `cmd/wfctl/plugin_audit.go`
- Modify: `cmd/wfctl/plugin_audit_test.go`
- Modify: `cmd/wfctl/plugin_validate.go`
- Modify: `cmd/wfctl/plugin_verify.go`
- Modify: `docs/WFCTL.md`

**Step 1: Write the failing tests**

Add tests for `wfctl audit plugins --strict-contracts` and plugin validation errors when a repo advertises module or step types without contract descriptors.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl -run 'Test.*StrictContract|TestRunAuditPlugins' -count=1`

Expected: FAIL because strict contract audit flags do not exist.

**Step 3: Implement wfctl strict-contract checks**

Inspect plugin manifests and optional generated descriptor files. Report coverage by module, step, trigger, and service method. In non-strict mode, emit warnings; in strict mode, fail on missing or legacy descriptors.

**Step 4: Run tests**

Run: `GOWORK=off go test ./cmd/wfctl -run 'Test.*StrictContract|TestRunAuditPlugins|TestRunValidatePlugin' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_audit.go cmd/wfctl/plugin_audit_test.go cmd/wfctl/plugin_validate.go cmd/wfctl/plugin_verify.go docs/WFCTL.md
git commit -m "feat(wfctl): audit strict plugin contracts"
```

### Task 5: Template Strict Contract Defaults

**Files:**
- Modify: `cmd/wfctl/plugin_init.go`
- Modify: `cmd/wfctl/plugin_init_test.go`
- Modify: `plugin/sdk/generator.go`
- Modify: `plugin/sdk/generator_test.go`
- Modify: `examples/external-plugin/main.go`

**Step 1: Write the failing tests**

Assert newly generated external plugins include a proto contract file, generated typed adapter scaffold, descriptor registration, and no map parsing in the generated step entrypoint.

**Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./cmd/wfctl ./plugin/sdk -run 'Test.*PluginInit|Test.*Generator' -count=1`

Expected: FAIL because generated plugins are legacy map-only.

**Step 3: Update generators**

Make strict contracts the default for new plugins and add an explicit legacy flag only for compatibility scaffolds.

**Step 4: Run tests**

Run: `GOWORK=off go test ./cmd/wfctl ./plugin/sdk -run 'Test.*PluginInit|Test.*Generator' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_init.go cmd/wfctl/plugin_init_test.go plugin/sdk/generator.go plugin/sdk/generator_test.go examples/external-plugin/main.go
git commit -m "feat(wfctl): scaffold strict-contract plugins"
```

### Task 6: Runtime Verification And Plan Index

**Files:**
- Modify: `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md`
- Modify: `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md`
- Modify: `docs/plans/INDEX.md`

**Step 1: Run focused tests**

Run: `GOWORK=off go test ./plugin/external/... ./cmd/wfctl -count=1`

Expected: PASS.

**Step 2: Run runtime validation**

Run: `GOWORK=off go run ./cmd/wfctl audit plugins --repo-root "$WORKSPACE" --strict-contracts`

Expected: non-zero before downstream repos migrate, with findings naming each legacy repo/type.

**Step 3: Regenerate the plan index**

Run: `GOWORK=off go run ./cmd/wfctl audit plans --dir docs/plans --fix-index`

Expected: `docs/plans/INDEX.md` contains both strict gRPC contract documents.

**Step 4: Commit**

```bash
git add docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md docs/plans/2026-04-26-strict-grpc-plugin-contracts.md docs/plans/INDEX.md
git commit -m "docs: track strict grpc plugin contract plan"
```

### Task 7: Downstream Repo Batch Plans

**Files:**
- Create or modify downstream plan docs in each migrated repo.

**Step 1: Inventory repos**

Run: `find "$WORKSPACE" -maxdepth 1 -type d \( -name 'workflow-plugin-*' -o -name 'workflow-dnd' -o -name 'workflow-cardgame' -o -name 'core-dump' -o -name 'buymywishlist' \) -print | sort`

Expected: list includes all first-party plugin and application repos.

**Step 2: Batch work**

Create branches per batch:

- infra/provider plugins
- SaaS/integration plugins
- auth/security plugins
- game/world plugins
- templates
- application-owned plugins

**Step 3: Dispatch implementation agents**

For each batch, dispatch one implementer plus spec and code-review agents. Require typed contract descriptors, at least one real host/plugin boundary test, local tests, PR creation, Copilot review handling, green checks, and admin merge.

**Step 4: Commit and PR per repo**

Each repo PR must reference this Workflow plan and report strict-contract audit output.
