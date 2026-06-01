# IaC Provider Region Lister Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add the optional `IaCProviderRegionLister` gRPC contract and make infra-admin use provider-sourced regions when advertised, falling back to the local catalog otherwise.

**Architecture:** Follow the existing optional IaC service pattern: additive proto service, SDK auto-registration by Go type assertion, typed wfctl adapter gated by ContractRegistry, and infra-admin handler probing an optional host-side interface. Absence or failure of the optional service keeps the current `local-catalog` behavior.

**Tech Stack:** Go 1.26, protobuf/gRPC via `buf generate`, Workflow plugin SDK, `wfctl` typed IaC adapter, infra-admin handler.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~600 including generated protobuf files

**Out of scope:**
- Cloud provider plugin implementations; they need a follow-up cascade after this core contract PR lands.
- Changes to `workflow-plugin-infra`; it does not own cloud credentials or the engine/plugin contract.
- New UI fields beyond existing `supported_regions` and `regions_source`.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add IaCProviderRegionLister contract and infra-admin consumer | Task 1, Task 2, Task 3, Task 4 | feat/813-iac-provider-region-lister |

**Status:** Draft

## Global Design Guidance

Source: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `decisions/0025-iac-optional-method-typed-services-not-bool.md`

| Guidance | Plan response |
|---|---|
| Core owns shared contracts; plugins own provider-specific runtime integrations. | Add the shared proto/SDK/adapter contract in workflow only; defer cloud SDK implementations to provider repos. |
| Optional IaC capabilities are advertised by ContractRegistry; absence is the negative signal. | Region lister is an optional service registered only when providers implement it. |
| Use `GOWORK=off` and focused tests first. | Verification starts with proto/SDK/adapter/handler tests, then broadens to package tests and lint. |
| Do not add scratch example/test directories. | No new root directories; only source, tests, generated proto, and this plan. |

## Task 1: Proto Contract

**Files:**
- Modify: `plugin/external/proto/iac.proto`
- Regenerate: `plugin/external/proto/iac.pb.go`
- Regenerate: `plugin/external/proto/iac_grpc.pb.go`
- Test: `plugin/external/proto/iac_proto_test.go`

**Steps:**
1. Add optional service `IaCProviderRegionLister` with `ListRegions(ListRegionsRequest) returns (ListRegionsResponse)`.
2. Add strict typed messages with scalar fields only: `ListRegionsRequest { string env_name = 1; }`, `ProviderRegion { string name = 1; string display_name = 2; }`, and `ListRegionsResponse { repeated ProviderRegion regions = 1; }`.
3. Run `buf generate` from repo root.
4. Add/extend compile-time tests proving the service exists and remains optional.

**Verification:**
- `GOWORK=off go test ./plugin/external/proto -run 'TestIaCProto|TestRegion' -count=1`
- Expected: package passes and generated Go exposes `pb.IaCProviderRegionListerServer`.

**Rollback:** Revert the proto + generated files; provider plugins keep using local catalog fallback.

## Task 2: SDK Registration and Contract Advertisement

**Files:**
- Modify: `plugin/external/sdk/iacserver.go`
- Modify: `plugin/external/sdk/contracts_iac_test.go`
- Modify as needed: `plugin/external/sdk/iacserver_test.go`

**Steps:**
1. Add `pb.IaCProviderRegionListerServer` to the optional service list in SDK docs/comments.
2. Register it in `registerIaCServicesOnly` via the existing type-assertion pattern.
3. Add a contract registry test proving the service is advertised when implemented and absent when not implemented.

**Verification:**
- `GOWORK=off go test ./plugin/external/sdk -run 'TestBuildContractRegistry|TestRegisterAllIaCProviderServices' -count=1`
- Expected: service descriptor includes `workflow.plugin.external.iac.IaCProviderRegionLister` only for implementing providers.

**Rollback:** Revert SDK registration; plugins can still compile with the proto but the host will not discover the service.

## Task 3: wfctl Typed Adapter

**Files:**
- Modify: `cmd/wfctl/iac_typed_adapter.go`
- Modify: `cmd/wfctl/iac_typed_adapter_test.go`

**Steps:**
1. Add `iacServiceRegionLister` constant and gated client construction in `newTypedIaCAdapter`.
2. Add `RegionLister()` accessor and a small `ListProviderRegions(ctx, envName)` helper returning sorted region names or `interfaces.ErrProviderMethodUnimplemented` when not advertised.
3. Add tests for advertised and absent region lister behavior.

**Verification:**
- `GOWORK=off go test ./cmd/wfctl -run 'TestTypedIaCAdapter.*Region|TestNewTypedIaCAdapter' -count=1`
- Expected: advertised service returns provider regions; absent service returns the existing unimplemented sentinel.

**Rollback:** Revert adapter changes; infra-admin falls back to local catalog.

## Task 4: Infra-Admin Consumer

**Files:**
- Modify: `iac/admin/handler/list_providers.go`
- Modify: `iac/admin/handler/list_providers_test.go`
- Modify: `iac/admin/proto/infra_admin.proto`
- Regenerate if proto comments or fields change: `iac/admin/proto/infra_admin.pb.go`
- Modify docs if needed: `docs/WFCTL.md`

**Steps:**
1. Add a narrow handler-local interface for providers that can list regions.
2. In `ListProviders`, call the optional lister when available. On nil error, use provider regions and set `regions_source = "provider-lister"`; on absence or error, keep existing `local-catalog` fallback.
3. Update tests for provider-sourced regions, error fallback, and existing local-catalog behavior.
4. Update comments/docs to remove the “future v1.1” wording.

**Verification:**
- `GOWORK=off go test ./iac/admin/handler -run 'TestListProviders' -count=1`
- `GOWORK=off go test ./cmd/wfctl -run 'TestInfraAdminCLI_ListProvidersOutput_RoundTrip' -count=1`
- `GOWORK=off go test ./plugin/external/proto ./plugin/external/sdk ./iac/admin/handler ./cmd/wfctl -count=1`
- `GOWORK=off golangci-lint run ./plugin/external/... ./iac/admin/... ./cmd/wfctl`
- Expected: all tests/lint pass; local catalog behavior remains unchanged when the optional service is absent.

**Rollback:** Revert handler + docs; infra-admin returns to host local catalog without schema migration.
