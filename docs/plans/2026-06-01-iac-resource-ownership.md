# IaC Resource Ownership Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add a provider-neutral IaC resource ownership contract so wfctl can refuse cross-owner cloud mutations and list resources owned by an operator identity.

**Architecture:** Follow the existing optional typed IaC service pattern: additive proto service, SDK auto-registration by Go type assertion, typed wfctl adapter gated by ContractRegistry, and wfctl command paths that skip providers when the optional service is absent. DNS ownership remains handled by `wfctl dns-policy`; this plan targets generic non-DNS cloud resources.

**Tech Stack:** Go 1.26, protobuf/gRPC via `buf generate`, Workflow plugin SDK, `wfctl` typed IaC adapter.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 5
**Estimated Lines of Change:** ~900 including generated protobuf files

**Out of scope:**
- DNS TXT ownership; current `wfctl dns-policy` remains the DNS mechanism.
- Cloud provider plugin implementations; they follow as cascade PRs after this core contract lands.
- Automatic ownership for applies where no owner identity is supplied.
- `workflow-plugin-infra`; it does not own cloud-provider resource mutation.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add IaC ownership contract and wfctl owners gate | Task 1, Task 2, Task 3, Task 4, Task 5 | issue-779-ownership-tags |

**Status:** Draft

## Global Design Guidance

Source: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/PLAN_PLATFORM_ABSTRACTION.md`, `decisions/0046-iac-resource-ownership-contract.md`

| Guidance | Plan response |
|---|---|
| Plan before apply; mutation safety belongs at the IaC boundary. | Enforce ownership in `ApplyPlanHooks.OnBeforeAction` before resource-driver mutation. |
| Core keeps shared contracts; provider plugins own cloud-specific integrations. | Add only contract, adapter, CLI gate, tests, and docs in workflow; provider tag/label implementations cascade later. |
| Optional IaC capabilities are discovered through ContractRegistry. | Ownership is an optional typed gRPC service; absence is a visible skip for listing and a no-op unless ownership enforcement is requested. |
| Use clean worktrees, `GOWORK=off`, and focused tests first. | Work executes in a clean worktree; verification runs focused packages before broader checks. |

## Security Review

Ownership is an operator-supplied safety identity, not authentication. The gate prevents accidental cross-owner mutation when providers can read ownership metadata, but it must not be treated as an authz boundary. `wfctl` does not log secrets or resource config values for this check; diagnostics include only resource name/type/provider and owner strings. `--force-owner` is explicit and applies only when `--owner` is set.

## Infrastructure Impact

The core PR creates no cloud resources. Provider cascades may write cloud tags/labels or provider-specific metadata when `SetOwner` is called; each cascade must document the provider mechanism and rollback. Rollback for core is revert plus regenerate proto; older plugins remain compatible because the service is optional.

## Multi-Component Validation

Core validation uses a bufconn-backed typed plugin fixture so the SDK registration, ContractRegistry discovery, adapter client, apply hook, and CLI command cross the real gRPC boundary. Provider cascades must add provider-level unit tests for tag/label mapping.

## Task 1: Proto Ownership Contract

**Files:**
- Modify: `plugin/external/proto/iac.proto`
- Regenerate: `plugin/external/proto/iac.pb.go`
- Regenerate: `plugin/external/proto/iac_grpc.pb.go`
- Test: `plugin/external/proto/iac_proto_test.go`

**Steps:**
1. Add optional service `IaCProviderOwnership`.
2. Add messages `GetOwnerRequest`, `GetOwnerResponse`, `SetOwnerRequest`, `SetOwnerResponse`, `ListOwnersRequest`, `ListOwnersResponse`, and `OwnedResource`.
3. Keep fields scalar/repeated/map-free except existing `ResourceRef`, matching strict-contract guidance.
4. Run `buf generate`.

**Verification:**
- Run: `GOWORK=off go test ./plugin/external/proto -run 'TestIaCProto|Ownership' -count=1`
- Expected: package passes and generated Go exposes `pb.IaCProviderOwnershipServer`.

**Rollback:** Revert proto and generated files; no state migration.

## Task 2: SDK and Interface Surface

**Files:**
- Modify: `interfaces/iac_provider.go`
- Modify: `interfaces/iac_provider_test.go`
- Modify: `plugin/external/sdk/iacserver.go`
- Modify: `plugin/external/sdk/iacserver_test.go`
- Modify: `plugin/external/sdk/contracts_iac_test.go`

**Steps:**
1. Add optional `OwnershipProvider` Go interface with `GetOwner`, `SetOwner`, and `ListOwners`.
2. Auto-register `pb.IaCProviderOwnershipServer` in `registerIaCServicesOnly`.
3. Update SDK tests to prove registration and ContractRegistry advertisement when implemented.

**Verification:**
- Run: `GOWORK=off go test ./interfaces ./plugin/external/sdk -run 'Ownership|RegisterAll|ContractRegistry' -count=1`
- Expected: optional service is advertised only for implementing providers.

**Rollback:** Revert SDK/interface changes; provider plugins cannot advertise ownership.

## Task 3: Typed Adapter

**Files:**
- Modify: `cmd/wfctl/iac_typed_adapter.go`
- Modify: `cmd/wfctl/iac_typed_adapter_test.go`
- Modify: `cmd/wfctl/iac_typed_fixture_test.go`

**Steps:**
1. Add ownership service constant, gated client construction, and `Ownership()` accessor.
2. Implement `GetOwner`, `SetOwner`, and `ListOwners` on `typedIaCAdapter` with `ErrProviderMethodUnimplemented` when absent.
3. Add tests for absent service and bufconn round-trip.

**Verification:**
- Run: `GOWORK=off go test ./cmd/wfctl -run 'TypedAdapter.*Owner|Ownership' -count=1`
- Expected: absent service returns the sentinel; registered service round-trips owners.

**Rollback:** Revert adapter changes; apply/list commands cannot reach ownership RPCs.

## Task 4: wfctl Apply Ownership Gate

**Files:**
- Create: `cmd/wfctl/infra_apply_ownership.go`
- Create: `cmd/wfctl/infra_apply_ownership_test.go`
- Modify: `cmd/wfctl/infra_apply.go`
- Modify: `cmd/wfctl/infra.go`
- Modify: `docs/WFCTL.md`

**Steps:**
1. Add `--owner` and `--force-owner` flags to `wfctl infra apply`; default owner from `WORKFLOW_RESOURCE_OWNER`.
2. Compose ownership enforcement with the existing DNS gate through `OnBeforeAction`.
3. For create/update/replace/delete actions, call `GetOwner`. Missing owner calls `SetOwner` before mutation; mismatched owner aborts unless `--force-owner` is set.
4. Skip DNS resources in this generic gate because DNS policy is separate.

**Verification:**
- Run: `GOWORK=off go test ./cmd/wfctl -run 'ApplyOwnership|InfraApply.*Owner|DNSGate' -count=1`
- Expected: matching/missing owners pass, missing owners call `SetOwner`, mismatched owners block, force overrides, DNS gate remains wired.

**Rollback:** Revert hook/flag/doc changes; generic ownership enforcement disabled.

## Task 5: wfctl Owners Listing

**Files:**
- Create: `cmd/wfctl/infra_owners.go`
- Create: `cmd/wfctl/infra_owners_test.go`
- Modify: `cmd/wfctl/infra.go`
- Modify: `docs/WFCTL.md`

**Steps:**
1. Add `wfctl infra owners --owner NAME [--type RESOURCE_TYPE]`.
2. Load every declared `iac.provider` and call typed `ListOwners` when advertised.
3. Print a stable table in text mode; skipped providers emit visible skip lines.

**Verification:**
- Run: `GOWORK=off go test ./cmd/wfctl -run 'InfraOwners|TypedAdapter.*Owner' -count=1`
- Expected: listed resources are sorted; providers without ownership support are visibly skipped.

**Rollback:** Revert command and docs; provider ownership metadata remains accessible only through plugin-specific tooling.
