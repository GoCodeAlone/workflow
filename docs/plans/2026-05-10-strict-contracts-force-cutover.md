---
status: approved
area: ecosystem
owner: jon
supersedes: [docs/plans/2026-04-26-strict-grpc-plugin-contracts.md]
---

# Strict-Contracts Force-Cutover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace string-method dispatch (`InvokeService("IaCProvider.X", map[string]any)`) with typed proto-generated gRPC services for the **external-plugin gRPC bridge** (workflow ↔ DO plugin), eliminating two specific bug classes (missing client bridge, missing server dispatcher) by making them compile-time errors.

**Scope clarification (per plan-phase cycle 1 C-1/C-3 finding):** The `interfaces.IaCProvider` Go interface IS the consumption surface for IN-PROCESS engine consumers (`module/infra_module.go`, `iac/wfctlhelpers/apply.go`, `platform/differ.go`, `iac/conformance/*`, `iac/refreshoutputs/refresh.go`, `plugin/sdk/iaclint/iaclint.go`, etc.). These ALREADY have compile-time interface enforcement via Go's structural typing — the bug class doesn't apply. Only the gRPC bridge between workflow and external plugins surfaces the runtime-vs-compile-time gap.

**This plan therefore migrates ONLY the gRPC bridge.** `interfaces.IaCProvider` survives unchanged as the in-process interface. The wfctl-side typed client (`pb.IaCProviderRequiredClient`) is wrapped in a thin adapter that satisfies `interfaces.IaCProvider` for engine consumers. Adapter is a typed-call dispatcher (not a string-marshalling proxy), so ADR-0026's "no hand-written re-marshalling wrapper" mandate is honored.

**Architecture:** Per design rev5 (commit `6073c3ce`). Single coordinated cutover via operator-upgrade-order model: workflow ships v1.0.0-rc1 (additive typed surface) → DO consumes rc1 + ships v1.0.0 → workflow PR-A merges to main + tags v1.0.0 final (cutover deletes IaC-specific gRPC-bridge legacy; engine-side `interfaces.IaCProvider` consumers unchanged). Optional capabilities split into 6 dedicated gRPC services with reflection-based auto-registration. Generic `InvokeService` RPC kept for non-IaC consumers (security-scanner-adapter et al.). Two-variable model for consumer YAML pins (`WFCTL_VERSION` + `WFCTL_LEGACY_STATE_VERSION`) prevents breaking state-file-compat workflows.

**Tech Stack:** Go 1.26, protobuf v1.36, grpc-go v1.65, gRPC server-side handler interfaces (compile-time enforcement), reflection-based registration helper, GoReleaser, GitHub Actions.

**Base branch:** main (workflow), main (workflow-plugin-digitalocean), main (each consumer repo)

---

## Scope Manifest

**PR Count:** 9
**Tasks:** 31
**Estimated Lines of Change:** ~2500 (workflow) + ~600 (DO plugin) + ~50 per consumer × 6 = ~3450 total

**Out of scope:**
- AWS / GCP / Azure / Tofu IaC plugins (they don't currently use the legacy surface; future Phase 2 plan)
- Module / Step / Trigger interfaces (already strict-typed via existing additive work; untouched)
- workflow-cloud / ratchet / ratchet-cli / workflow-cloud-ui programmatic IaC consumers (verified: don't import `interfaces.IaCProvider`)
- CLI flag schema testing (separate bug class; tracked as workflow-side follow-up)
- Internal-result-shape tests (separate bug class; addressed by general test coverage discipline)
- ContractRegistry → typed gRPC convergence for Module/Step/Trigger (potential follow-up plan)
- Generic `InvokeService` RPC removal (kept for non-IaC consumers per cycle 3 C-1)
- `WFCTL_LEGACY_STATE_VERSION` files (`teardown.yml`, `deploy.yml` rollback, `registry-retention.yml`) bumping to v1.0.0 — they intentionally pin to v0.14.2 for state-file-compat per `project_p0_core_dump_wfctl_bump_shipped`
- **In-process engine-side `interfaces.IaCProvider` consumers** (`module/infra_module.go`, `iac/wfctlhelpers/apply.go`, `platform/differ.go`, `iac/conformance/*`, `iac/refreshoutputs/refresh.go`, `plugin/sdk/iaclint/iaclint.go`) — these consume the Go interface directly (compile-time-checked); not part of the gRPC bridge bug class. UNCHANGED. The wfctl-side typed-client adapter satisfies `interfaces.IaCProvider` so these consumers see no API change.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | docs: supersede 2026-04-26 strict-contracts design for IaC interfaces | Task 1 | docs/supersede-2026-04-26-design |
| 2 | feat(plugin/external/proto): IaC typed gRPC services (rc1) | Task 2, Task 3, Task 4, Task 29, Task 5, Task 6 | feat/iac-typed-rc1 (workflow) |
| 3 | feat(do): implement pb.IaCProviderRequiredServer + 6 optional services (v1.0.0) | Task 7, Task 8, Task 9, Task 10, Task 11, Task 12, Task 13 | feat/iac-typed-server (DO plugin) |
| 4 | feat(workflow): cutover wfctl to typed pb.IaCProviderClient + delete legacy IaC paths (v1.0.0 final) | Task 14, Task 15, Task 30, Task 16, Task 17, Task 18, Task 19, Task 20, Task 31 | feat/iac-typed-cutover (workflow) |
| 5 | chore(deps): core-dump bump WFCTL_VERSION to v1.0.0 + DO plugin pin to v1.0.0 + per-file YAML audit | Task 21, Task 22 | chore/iac-cutover-pin-bump (core-dump) |
| 6 | chore(deps): buymywishlist wfctl + DO plugin pin bump | Task 23 | chore/iac-cutover-pin-bump (BMW) |
| 7 | chore(deps): workflow-cloud wfctl pin bump (no IaC use; safe) | Task 24 | chore/iac-cutover-pin-bump (workflow-cloud) |
| 8 | chore(deps): ratchet + ratchet-cli wfctl pin bump (no IaC use) | Task 25, Task 26 | chore/iac-cutover-pin-bump (each repo) |
| 9 | chore(deps): workflow-cloud-ui wfctl pin bump (no IaC use) | Task 27, Task 28 | chore/iac-cutover-pin-bump (workflow-cloud-ui) |

**Status:** Locked 2026-05-10T05:51:18Z

---

## Sequencing constraints

- **PR 1** independent; can land any time before PR 2.
- **PR 2** ships workflow v1.0.0-rc1 (typed proto + SDK + auto-registration helper, ADDITIVE — legacy IaC paths still in place).
- **PR 3** consumes workflow v1.0.0-rc1, implements DO pb.IaCProviderRequiredServer, ships DO plugin v1.0.0.
- **PR 4** workflow cutover: consumes DO v1.0.0 in cross-plugin-build CI, deletes IaC-specific legacy paths, tags workflow v1.0.0 final. Held in DRAFT until PR 3 merges.
- **PRs 5-9** parallel after PR 4 merges; consumer pin bumps.

Operator upgrade order (documented in PR 4's CHANGELOG + runbook): bump DO plugin pin to v1.0.0+ FIRST; bump wfctl to v1.0.0+ SECOND. Workflow v1.0.0 pre-flight gate fails loud if DO plugin is older.

---

## PR 1: docs: supersede 2026-04-26 strict-contracts design for IaC interfaces

### Task 1: Add supersession-notice doc + update design frontmatter

**Per plan-phase cycle 1 I-3:** the 2026-04-26 implementation PLAN is scope-lock-protected (per memory `feedback_plan_files_lead_owned`); editing its frontmatter mid-flight may be blocked. Compromise: edit only the DESIGN doc's frontmatter (designs are not scope-locked the same way; status is informational); for the implementation PLAN, add a NEW supersession-notice file in the same dir.

**Files:**
- Modify: `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md` (frontmatter only — design docs are not scope-lock-protected)
- Create: `docs/plans/2026-04-26-strict-grpc-plugin-contracts.SUPERSEDED-NOTICE.md` (NEW supersession pointer; doesn't modify the locked plan file)

**Change class:** Documentation. Verification: spell-check + render preview; no runtime impact, no rollback note required.

**Step 1: Update design frontmatter**

Edit the YAML frontmatter at top of `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md`:

```diff
 ---
 status: in_progress
+superseded_by: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md
+supersession_scope: IaCProvider, ResourceDriver (Module/Step/Trigger work remains live)
+status: superseded_partial
 area: plugins
 ...
```

**Step 2: Create supersession-notice file (don't edit the scope-locked plan)**

Create `docs/plans/2026-04-26-strict-grpc-plugin-contracts.SUPERSEDED-NOTICE.md`:

```markdown
# Supersession Notice — 2026-04-26 Strict-Contracts Plan (IaC Scope)

**Date:** 2026-05-10
**Scope of supersession:** IaCProvider + ResourceDriver migration entries

The IaCProvider + ResourceDriver migration tracker entries in `2026-04-26-strict-grpc-plugin-contracts.md` are SUPERSEDED by the 2026-05-10 force-cutover plan:
- Design: `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md`
- Plan: `docs/plans/2026-05-10-strict-contracts-force-cutover.md`

Per `feedback_force_strict_contracts_no_compat`: the 2026-04-26 additive approach was insufficient; the IaC migration needs hard-cutover.

The Module/Step/Trigger migration tracker entries (workflow-plugin-{audit, sso, ws-auth, authz, security, etc.}) in the 2026-04-26 plan REMAIN LIVE — they're not superseded.

This notice exists as a separate file because the 2026-04-26 plan itself is scope-lock-protected per `feedback_plan_files_lead_owned` and cannot be edited in-place.
```

**Step 3: Verify markdown renders cleanly**

```bash
# Render-check (workspace convention: github markdown preview tool)
cat docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md | head -20
# Expected: frontmatter at top with new fields visible
```

**Step 4: Commit**

```bash
git add docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md docs/plans/2026-04-26-strict-grpc-plugin-contracts.SUPERSEDED-NOTICE.md
git commit -m "docs(plans): supersede 2026-04-26 strict-contracts design for IaCProvider+ResourceDriver scope"
```

---

## PR 2: feat(plugin/external/proto): IaC typed gRPC services (workflow v1.0.0-rc1)

This PR is ADDITIVE. Adds typed proto + SDK + auto-registration helper. Does NOT delete legacy. Ships as workflow v1.0.0-rc1 pre-release. DO plugin PR 3 consumes this rc1.

### Task 2: Add tools.go + grpc-versions.txt artifact (cross-repo dep sync foundation)

**Files:**
- Create: `tools.go` (workflow root)
- Create: `.goreleaser.d/grpc-versions.tpl` (or analogous mechanism per workflow's GoReleaser config)
- Modify: `.github/workflows/release.yml` (publish grpc-versions.txt artifact)

**Change class:** Build pipeline. Verification: `go build ./...` clean; release-pipeline dry-run produces the artifact.

**Step 1: Create tools.go pinning protoc-gen-go + protoc-gen-go-grpc**

```go
//go:build tools
// +build tools

package tools

import (
    _ "google.golang.org/protobuf/cmd/protoc-gen-go"
    _ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
)
```

**Step 2: `go mod tidy` + verify versions**

Run:
```bash
GOWORK=off go mod tidy
GOWORK=off go list -m google.golang.org/grpc google.golang.org/protobuf google.golang.org/grpc/cmd/protoc-gen-go-grpc google.golang.org/protobuf/cmd/protoc-gen-go
```
Expected: 4 lines, each with explicit version. Capture these values for the artifact template.

**Step 3: Add grpc-versions.txt generation to release pipeline (per plan-phase cycle 1 I-2 — the workflow repo uses softprops/action-gh-release@v2, NOT goreleaser-extra-files)**

Read `.github/workflows/release.yml` first to identify the actual release-action being used. Workflow's release pipeline is via `softprops/action-gh-release@v2`. The artifact upload mechanism is its `files:` input.

Edit `.github/workflows/release.yml` to:

```yaml
      - name: Generate grpc-versions.txt
        run: |
          set -euo pipefail
          {
            echo "grpc=$(go list -m -json google.golang.org/grpc | jq -r .Version)"
            echo "protobuf=$(go list -m -json google.golang.org/protobuf | jq -r .Version)"
            echo "protoc-gen-go=$(go list -m -json google.golang.org/protobuf/cmd/protoc-gen-go | jq -r .Version)"
            echo "protoc-gen-go-grpc=$(go list -m -json google.golang.org/grpc/cmd/protoc-gen-go-grpc | jq -r .Version)"
          } > grpc-versions.txt
          cat grpc-versions.txt

      - name: Add grpc-versions.txt to release artifacts
        # Add to the existing softprops/action-gh-release@v2 step's `files:` input
        # If files is currently `dist/*`, change to `dist/*\ngrpc-versions.txt`
```

(If the release file structure differs at implementation time, adapt — the goal is publishing `grpc-versions.txt` as a release asset.)

**Step 4: Smoke test**

```bash
GOWORK=off go build -tags tools ./...
# Expected: clean, no errors
```

**Step 5: Commit**

```bash
git add tools.go go.mod go.sum .github/workflows/release.yml
git commit -m "feat(release): publish grpc-versions.txt artifact for cross-repo dep sync"
```

**Rollback:** revert commit; release pipeline drops back to prior state. No state migration.

### Task 3: Add iac.proto with IaCProviderRequired + 6 optional services

**Files:**
- Create: `plugin/external/proto/iac.proto`
- Test: `plugin/external/proto/iac_proto_test.go`

**Change class:** Plugin / extension (proto definition). Verification: `protoc` produces valid Go code; generated server interfaces compile.

**Step 1: Write the failing test first (proto-conformance)**

```go
// plugin/external/proto/iac_proto_test.go
package proto_test

import (
    "testing"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestIaCProviderRequiredServerHasAllRequiredMethods asserts that the
// generated server interface has every method named in the design.
// Catches accidental method drops in iac.proto.
func TestIaCProviderRequiredServerHasAllRequiredMethods(t *testing.T) {
    var srv pb.IaCProviderRequiredServer = (*requiredStub)(nil)
    _ = srv // type-assert satisfaction at compile time

    // Methods are checked at compile time via the type assertion.
    // The stub MUST implement: Initialize, Name, Version, Capabilities,
    // Plan, Apply, Destroy, Status, Import, ResolveSizing, BootstrapStateBackend.
}

type requiredStub struct {
    pb.UnimplementedIaCProviderRequiredServer
}

// TestOptionalServicesHaveDistinctInterfaces asserts each optional
// service has its own server interface (not method-on-required).
func TestOptionalServicesHaveDistinctInterfaces(t *testing.T) {
    type optional interface {
        pb.IaCProviderEnumeratorServer
        pb.IaCProviderDriftDetectorServer
        pb.IaCProviderCredentialRevokerServer
        pb.IaCProviderMigrationRepairerServer
        pb.IaCProviderValidatorServer
        pb.IaCProviderDriftConfigDetectorServer
    }
    var _ optional = (*allOptionalStub)(nil)
}

type allOptionalStub struct {
    pb.UnimplementedIaCProviderEnumeratorServer
    pb.UnimplementedIaCProviderDriftDetectorServer
    pb.UnimplementedIaCProviderCredentialRevokerServer
    pb.UnimplementedIaCProviderMigrationRepairerServer
    pb.UnimplementedIaCProviderValidatorServer
    pb.UnimplementedIaCProviderDriftConfigDetectorServer
}
```

**Step 2: Run the test — expect compile failure**

```bash
GOWORK=off go test ./plugin/external/proto/...
# Expected: FAIL with "undefined: pb.IaCProviderRequiredServer" — proto file doesn't exist yet
```

**Step 3: Write iac.proto**

Create `plugin/external/proto/iac.proto`:

```proto
syntax = "proto3";

package workflow.plugin.external.iac;
option go_package = "github.com/GoCodeAlone/workflow/plugin/external/proto;proto";

// REQUIRED service — every IaC provider MUST implement every RPC.
// Compile fails if plugin doesn't satisfy this interface.
service IaCProviderRequired {
  rpc Initialize(InitializeRequest) returns (InitializeResponse);
  rpc Name(NameRequest) returns (NameResponse);
  rpc Version(VersionRequest) returns (VersionResponse);
  rpc Capabilities(CapabilitiesRequest) returns (CapabilitiesResponse);
  rpc Plan(PlanRequest) returns (PlanResponse);
  rpc Apply(ApplyRequest) returns (ApplyResponse);
  rpc Destroy(DestroyRequest) returns (DestroyResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Import(ImportRequest) returns (ImportResponse);
  rpc ResolveSizing(ResolveSizingRequest) returns (ResolveSizingResponse);
  rpc BootstrapStateBackend(BootstrapStateBackendRequest) returns (BootstrapStateBackendResponse);
}

// OPTIONAL services — registered only when the provider actually implements them.
// Absence of registration IS the negative signal (per cycle 3 I-1).
// Each plugin that wants the capability registers the corresponding service.

service IaCProviderEnumerator {
  rpc EnumerateAll(EnumerateAllRequest) returns (EnumerateAllResponse);
  rpc EnumerateByTag(EnumerateByTagRequest) returns (EnumerateByTagResponse);
}

service IaCProviderDriftDetector {
  rpc DetectDrift(DetectDriftRequest) returns (DetectDriftResponse);
  rpc DetectDriftWithSpecs(DetectDriftWithSpecsRequest) returns (DetectDriftWithSpecsResponse);
}

service IaCProviderCredentialRevoker {
  rpc RevokeProviderCredential(RevokeProviderCredentialRequest) returns (RevokeProviderCredentialResponse);
}

service IaCProviderMigrationRepairer {
  rpc RepairDirtyMigration(RepairDirtyMigrationRequest) returns (RepairDirtyMigrationResponse);
}

service IaCProviderValidator {
  rpc ValidatePlan(ValidatePlanRequest) returns (ValidatePlanResponse);
}

service IaCProviderDriftConfigDetector {
  rpc DetectDriftConfig(DetectDriftConfigRequest) returns (DetectDriftConfigResponse);
}

// ResourceDriver service: separate gRPC service.
service ResourceDriver {
  rpc Create(ResourceCreateRequest) returns (ResourceCreateResponse);
  rpc Read(ResourceReadRequest) returns (ResourceReadResponse);
  rpc Update(ResourceUpdateRequest) returns (ResourceUpdateResponse);
  rpc Delete(ResourceDeleteRequest) returns (ResourceDeleteResponse);
  rpc Diff(ResourceDiffRequest) returns (ResourceDiffResponse);
  rpc Scale(ResourceScaleRequest) returns (ResourceScaleResponse);
  rpc HealthCheck(ResourceHealthCheckRequest) returns (ResourceHealthCheckResponse);
  rpc SensitiveKeys(SensitiveKeysRequest) returns (SensitiveKeysResponse);
  rpc Troubleshoot(TroubleshootRequest) returns (TroubleshootResponse);
}

// --- Typed messages ---
// (~30 message types follow; one per request/response pair.)
// Each message uses typed fields — NO google.protobuf.Struct, NO google.protobuf.Any.
// Sensitive output handling: ResourceOutput.sensitive map<string,bool>.
// See full message definitions in subsequent commits within this PR.
```

**Sub-task 3.1**: Author the ~30 message definitions. Each request/response pair derives from the existing Go interface signatures in `interfaces/iac_provider.go` and `interfaces/iac_resource_driver.go`. Sensitive map: `map<string, bool> sensitive = N;` per `ResourceOutput`.

**Step 4: Generate Go code**

```bash
cd /Users/jon/workspace/workflow/_worktrees/iac-typed-rc1   # use a fresh worktree per per-agent-worktree pattern
GOWORK=off protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  plugin/external/proto/iac.proto
ls plugin/external/proto/iac*.go
# Expected: iac.pb.go + iac_grpc.pb.go generated
```

**Step 5: Run the test — expect PASS**

```bash
GOWORK=off go test ./plugin/external/proto/... -count=1
# Expected: PASS — generated server interfaces match the test's type-assert expectations
```

**Step 6: Commit**

```bash
git add plugin/external/proto/iac.proto plugin/external/proto/iac.pb.go plugin/external/proto/iac_grpc.pb.go plugin/external/proto/iac_proto_test.go
git commit -m "feat(proto): add iac.proto with IaCProviderRequired + 6 optional services + ResourceDriver"
```

**Rollback:** revert commit; legacy `InvokeService` RPC still functional; no consumer affected (PR is additive).

### Task 4: SDK reflection-based auto-registration helper

**Files:**
- Create: `plugin/external/sdk/iacserver.go`
- Test: `plugin/external/sdk/iacserver_test.go`

**Change class:** Plugin / extension (SDK helper). Verification: unit tests cover required-satisfied + optional-satisfied + required-missing failure cases.

**Step 1: Write failing tests first**

```go
// plugin/external/sdk/iacserver_test.go
package sdk_test

import (
    "testing"

    "google.golang.org/grpc"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
    "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// TestRegisterAllIaCProviderServices_RequiredSatisfied_RegistersRequired
func TestRegisterAllIaCProviderServices_RequiredSatisfied_RegistersRequired(t *testing.T) {
    grpcSrv := grpc.NewServer()
    provider := &fullProviderStub{}
    err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    info := grpcSrv.GetServiceInfo()
    if _, ok := info["workflow.plugin.external.iac.IaCProviderRequired"]; !ok {
        t.Fatalf("required service not registered")
    }
}

// TestRegisterAllIaCProviderServices_OptionalSatisfied_RegistersOptional
func TestRegisterAllIaCProviderServices_OptionalSatisfied_RegistersOptional(t *testing.T) {
    grpcSrv := grpc.NewServer()
    provider := &enumeratorOnlyStub{}
    err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    info := grpcSrv.GetServiceInfo()
    if _, ok := info["workflow.plugin.external.iac.IaCProviderEnumerator"]; !ok {
        t.Fatalf("Enumerator optional service NOT registered despite provider satisfying interface")
    }
    if _, ok := info["workflow.plugin.external.iac.IaCProviderDriftDetector"]; ok {
        t.Fatalf("DriftDetector incorrectly registered (provider doesn't satisfy)")
    }
}

// TestRegisterAllIaCProviderServices_RequiredMissing_ReturnsError
func TestRegisterAllIaCProviderServices_RequiredMissing_ReturnsError(t *testing.T) {
    grpcSrv := grpc.NewServer()
    provider := &emptyStub{} // doesn't satisfy IaCProviderRequiredServer
    err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider)
    if err == nil {
        t.Fatalf("expected error for unsatisfied required interface; got nil")
    }
    if !strings.Contains(err.Error(), "IaCProviderRequiredServer") {
        t.Fatalf("error message must name the unsatisfied interface; got %q", err.Error())
    }
}

// fullProviderStub satisfies IaCProviderRequired + Enumerator + DriftDetector
// (representative of DO plugin's expected shape).
type fullProviderStub struct {
    pb.UnimplementedIaCProviderRequiredServer
    pb.UnimplementedIaCProviderEnumeratorServer
    pb.UnimplementedIaCProviderDriftDetectorServer
}

// enumeratorOnlyStub satisfies Required + Enumerator only.
type enumeratorOnlyStub struct {
    pb.UnimplementedIaCProviderRequiredServer
    pb.UnimplementedIaCProviderEnumeratorServer
}

type emptyStub struct{}
```

**Step 2: Run tests — expect compile failure (function not defined)**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run TestRegisterAllIaCProvider
# Expected: FAIL with "undefined: sdk.RegisterAllIaCProviderServices"
```

**Step 3: Implement the helper**

```go
// plugin/external/sdk/iacserver.go
package sdk

import (
    "fmt"

    "google.golang.org/grpc"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// RegisterAllIaCProviderServices uses Go type-assertion to register every
// IaC service interface the provider satisfies.
//
// REQUIRED: pb.IaCProviderRequiredServer (compile error if missing — caught
// at the type-assert below).
//
// OPTIONAL (auto-detected): IaCProviderEnumerator, IaCProviderDriftDetector,
// IaCProviderCredentialRevoker, IaCProviderMigrationRepairer,
// IaCProviderValidator, IaCProviderDriftConfigDetector. Plus ResourceDriver.
//
// Per cycle 3 I-1: plugin author writes ONE line; cannot omit a registration
// for a capability they implemented.
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error {
    required, ok := provider.(pb.IaCProviderRequiredServer)
    if !ok {
        return fmt.Errorf("RegisterAllIaCProviderServices: provider %T does not satisfy pb.IaCProviderRequiredServer (missing methods); see https://github.com/GoCodeAlone/workflow/blob/main/docs/plans/2026-05-10-strict-contracts-force-cutover-design.md", provider)
    }
    pb.RegisterIaCProviderRequiredServer(s, required)

    if v, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
        pb.RegisterIaCProviderEnumeratorServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderDriftDetectorServer); ok {
        pb.RegisterIaCProviderDriftDetectorServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderCredentialRevokerServer); ok {
        pb.RegisterIaCProviderCredentialRevokerServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderMigrationRepairerServer); ok {
        pb.RegisterIaCProviderMigrationRepairerServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderValidatorServer); ok {
        pb.RegisterIaCProviderValidatorServer(s, v)
    }
    if v, ok := provider.(pb.IaCProviderDriftConfigDetectorServer); ok {
        pb.RegisterIaCProviderDriftConfigDetectorServer(s, v)
    }
    if v, ok := provider.(pb.ResourceDriverServer); ok {
        pb.RegisterResourceDriverServer(s, v)
    }
    return nil
}
```

**Step 4: Run tests — expect PASS**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run TestRegisterAllIaCProvider -v -count=1
# Expected: PASS for all 3 tests
```

**Step 5: Commit**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_test.go
git commit -m "feat(sdk): RegisterAllIaCProviderServices auto-registration helper (cycle 3 I-1 fix)"
```

**Rollback:** revert commit; SDK consumers can still register services manually via the per-service Register* helpers protoc generated.

### Task 29: SDK ServeIaCPlugin — high-level API hiding *grpc.Server (per plan-phase cycle 2 C-2-NEW)

**Files:**
- Modify: `plugin/external/sdk/iacserver.go` — add `ServeIaCPlugin(provider, opts)` + `IaCServeOptions` types; reuse existing handshake / serve loop from `sdk.Serve`.
- Test: `plugin/external/sdk/iacserver_serve_test.go`

**Concrete API specification (per cycle 3 I-1: must use go-plugin's GRPCServer callback pattern; cannot pre-create *grpc.Server):**

```go
// IaCServeOptions configures the IaC plugin gRPC server.
type IaCServeOptions struct {
    // PluginInfo describes the plugin for the go-plugin HandshakeConfig.
    PluginInfo *PluginInfo
}

// iacGRPCPlugin implements go-plugin's plugin.GRPCPlugin interface.
// Service registration happens INSIDE GRPCServer per go-plugin v1.7.0
// architecture (server.go:87 — the framework owns *grpc.Server lifecycle).
type iacGRPCPlugin struct {
    plugin.NetRPCUnsupportedPlugin
    provider any
}

func (p *iacGRPCPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
    return RegisterAllIaCProviderServices(s, p.provider)
}

func (p *iacGRPCPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
    // Plugin-side; client is built by wfctl from the typed pb.IaCProviderRequiredClient.
    return nil, fmt.Errorf("iac plugin GRPCClient not used; wfctl uses typed pb client directly")
}

// ServeIaCPlugin starts an IaC plugin gRPC server with auto-registration.
// Wraps hashicorp/go-plugin's plugin.Serve to register the typed IaC services
// inside the framework-managed *grpc.Server callback.
//
// Plugin authors call this once in main.go; cannot accidentally omit a
// service registration for an interface they implemented.
func ServeIaCPlugin(provider any, opts IaCServeOptions) {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: opts.PluginInfo.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "iac": &iacGRPCPlugin{provider: provider},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

This pattern is consistent with the existing `plugin/external/sdk/serve.go:42-56` `servePlugin` flow that other plugin types use. We're not extracting from `sdk.Serve` (cycle 3 I-1's error in rev3); we're using the same hashicorp/go-plugin entrypoint with a different `Plugins` map entry for IaC.

**Step 1: Failing test** — stub plugin process; spawn via os/exec; assert `ServeIaCPlugin` registers all services (verify via gRPC reflection from test client) + responds to handshake protocol.

**Step 2: Implement** — Refactor `sdk.Serve` to extract `serveWithHandshake`; build `ServeIaCPlugin` on top. The existing `sdk.Serve` is preserved for non-IaC plugins.

**Step 3: Tests + commit.**

PR 3 (Task 9) imports `sdk.ServeIaCPlugin` + `sdk.IaCServeOptions` from this PR.

### Task 5: SDK ContractRegistry advertises registered IaC services (capability discovery wfctl-side)

**Files:**
- Modify: `plugin/external/sdk/contracts.go` (or wherever ContractRegistry is built)
- Test: `plugin/external/sdk/contracts_iac_test.go`

**Change class:** Plugin / extension (SDK extension). Verification: unit test asserts ContractRegistry response includes the registered IaC service names.

**Step 1: Write failing test**

```go
// plugin/external/sdk/contracts_iac_test.go
package sdk_test

import (
    "context"
    "testing"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
    "github.com/GoCodeAlone/workflow/plugin/external/sdk"
    "google.golang.org/grpc"
)

// TestContractRegistry_AdvertisesRegisteredIaCServices verifies that after
// calling RegisterAllIaCProviderServices, the GetContractRegistry RPC
// response advertises the registered IaC services in its ContractDescriptor
// list. wfctl reads this to discover provider capabilities.
func TestContractRegistry_AdvertisesRegisteredIaCServices(t *testing.T) {
    grpcSrv := grpc.NewServer()
    provider := &fullProviderStub{}
    if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
        t.Fatalf("register: %v", err)
    }

    // Simulate the GetContractRegistry RPC the SDK exposes.
    registry := sdk.BuildContractRegistry(grpcSrv)

    services := map[string]bool{}
    for _, c := range registry.Contracts {
        if c.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE {
            services[c.ServiceName] = true
        }
    }

    expected := []string{
        "workflow.plugin.external.iac.IaCProviderRequired",
        "workflow.plugin.external.iac.IaCProviderEnumerator",
        "workflow.plugin.external.iac.IaCProviderDriftDetector",
    }
    for _, name := range expected {
        if !services[name] {
            t.Errorf("ContractRegistry missing service %q", name)
        }
    }
}
```

**Step 2: Run test — expect FAIL**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run TestContractRegistry_AdvertisesRegisteredIaC -count=1
# Expected: FAIL — sdk.BuildContractRegistry doesn't yet enumerate gRPC services
```

**Step 3: Implement**

Read existing `plugin/external/sdk/contracts.go` to find where ContractRegistry response is built. Add a hook that iterates `grpcServer.GetServiceInfo()` and emits a `ContractDescriptor{Kind: SERVICE, ServiceName: name}` for each registered service.

```go
// In sdk/contracts.go (concrete location: read existing file structure first)
func BuildContractRegistry(grpcSrv *grpc.Server) *pb.ContractRegistry {
    // ... existing module/step/trigger contract emission ...

    // NEW: emit a SERVICE contract for every registered gRPC service.
    // wfctl uses this for capability discovery on the IaC interfaces.
    for serviceName := range grpcSrv.GetServiceInfo() {
        registry.Contracts = append(registry.Contracts, &pb.ContractDescriptor{
            Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
            ServiceName: serviceName,
            Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
        })
    }
    return registry
}
```

**Step 4: Run test — expect PASS**

```bash
GOWORK=off go test ./plugin/external/sdk/ -run TestContractRegistry_AdvertisesRegisteredIaC -count=1 -v
# Expected: PASS
```

**Step 5: Commit**

```bash
git add plugin/external/sdk/contracts.go plugin/external/sdk/contracts_iac_test.go
git commit -m "feat(sdk): ContractRegistry advertises registered IaC services for wfctl capability discovery"
```

**Rollback:** revert commit; ContractRegistry returns prior shape (Module/Step/Trigger only).

### Task 6: Add typed-IaC E2E integration test fixture in workflow CI

**Files:**
- Create: `plugin/external/sdk/iac_e2e_test.go` (build tag `integration`)
- Modify: `.github/workflows/cross-plugin-build-test.yml` (add new matrix row for typed-IaC test)

**Per plan-phase cycle 1 I-2 + cycle 2 I-1-NEW:** the existing matrix may only do `go build`. For typed-IaC verification, the matrix step must additionally LOAD the cross-built plugin via real gRPC subprocess + invoke at least one typed RPC end-to-end. Otherwise wire incompat between workflow + plugin grpc-go versions slips through. Add an "IaC wire test" matrix step:

```yaml
      - name: IaC typed-RPC wire test (workflow ↔ plugin)
        run: |
          set -euo pipefail
          # Build the plugin against this workflow PR's commit
          GOWORK=off go build -o /tmp/do-plugin ./cmd/...
          # Start the plugin as subprocess, exercise IaCProvider.EnumerateAll via typed client
          GOWORK=off go test -tags=integration -run TestIaC_CrossPluginWireTest ./plugin/external/sdk/...
        working-directory: ${{ github.workspace }}/cross-plugin-deps/workflow-plugin-digitalocean
```

The test in `plugin/external/sdk/iac_e2e_test.go` (this task) handles both the in-process path AND the subprocess path. Cross-plugin-build uses the subprocess path against the real DO plugin v1.0.0 binary.

**Change class:** Plugin / extension (integration test) + CI YAML. Verification: E2E test loads a fake-IaC plugin via real gRPC subprocess and exercises EnumerateAll typed RPC.

**Step 1: Write E2E test using a minimal in-process gRPC server stub**

```go
//go:build integration
// +build integration

// plugin/external/sdk/iac_e2e_test.go
package sdk_test

// TestIaC_EndToEnd_RequiredAndOptional_TypedDispatch loads an in-process
// gRPC server with the typed IaCProviderRequired + Enumerator services,
// invokes EnumerateAll via the typed pb.IaCProviderEnumeratorClient,
// asserts the typed response is correct.
//
// This catches the bug class the design closes: missing client bridge,
// missing server dispatcher. If either side drops a method, the test
// fails to compile (server interface unsatisfied) or the client call
// returns a typed error.
func TestIaC_EndToEnd_RequiredAndOptional_TypedDispatch(t *testing.T) {
    // ... start in-process gRPC server with fullProviderStub
    // ... create a typed client
    // ... call EnumerateAll(ctx, &pb.EnumerateAllRequest{ResourceType: "fake.type"})
    // ... assert typed response shape
}
```

**Step 2: Add CI matrix row**

In `.github/workflows/cross-plugin-build-test.yml`, add:
```yaml
      - name: Typed-IaC E2E test
        run: GOWORK=off go test -tags=integration ./plugin/external/sdk/... -run TestIaC_EndToEnd
```

**Step 3: Run locally to validate**

```bash
GOWORK=off go test -tags=integration ./plugin/external/sdk/... -run TestIaC_EndToEnd -count=1 -v
# Expected: PASS — typed roundtrip works in-process
```

**Step 4: Commit + push for rc1 release**

```bash
git add plugin/external/sdk/iac_e2e_test.go .github/workflows/cross-plugin-build-test.yml
git commit -m "test(sdk): typed-IaC E2E integration test + CI gate"
git push -u origin feat/iac-typed-rc1
```

After PR-review approval, merge + tag workflow `v1.0.0-rc1`. This rc1 is the consumable for DO plugin PR 3.

**Rollback:** revert commit; rc1 release stays available; no main impact.

---

## PR 3: feat(do): implement pb.IaCProviderRequiredServer + 6 optional services (DO plugin v1.0.0)

This PR consumes workflow `v1.0.0-rc1` from PR 2. Adds typed gRPC server-side implementation. Deletes the legacy `internal/module_instance.go` switch dispatcher entirely.

### Task 7: Bump go.mod to workflow v1.0.0-rc1 + cross-repo grpc-go version sync

**Files (DO plugin repo):**
- Modify: `go.mod`
- Create: `.github/workflows/grpc-version-sync.yml` (CI gate per F-4)
- Modify: `internal/main.go` (or plugin entrypoint — minor import update)

**Change class:** Version pin update (per workflow runtime-launch-validation trigger list). REQUIRES rollback note.

**Step 1: Update go.mod**

```diff
-	github.com/GoCodeAlone/workflow v0.27.2
+	github.com/GoCodeAlone/workflow v1.0.0-rc1
```

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin main
git worktree add _worktrees/iac-typed-server -b feat/iac-typed-server origin/main
cd _worktrees/iac-typed-server
GOWORK=off go mod tidy
```

**Step 2: Add cross-repo grpc-go version sync CI gate**

Create `.github/workflows/grpc-version-sync.yml`:
```yaml
name: gRPC Version Sync
on: [pull_request]
permissions:
  contents: read
jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
      - name: Fetch workflow grpc-versions.txt
        run: |
          set -euo pipefail
          WORKFLOW_VERSION=$(grep "github.com/GoCodeAlone/workflow " go.mod | awk '{print $2}')
          curl -L "https://github.com/GoCodeAlone/workflow/releases/download/${WORKFLOW_VERSION}/grpc-versions.txt" -o /tmp/grpc-versions.txt
          cat /tmp/grpc-versions.txt
      - name: Verify resolved grpc-go matches workflow's pinned version
        run: |
          set -euo pipefail
          EXPECTED=$(grep "^grpc=" /tmp/grpc-versions.txt | cut -d= -f2)
          ACTUAL=$(GOWORK=off go list -m -json google.golang.org/grpc | jq -r .Version)
          if [ "$EXPECTED" != "$ACTUAL" ]; then
            echo "::error::grpc-go drift: workflow requires $EXPECTED; this plugin resolves to $ACTUAL"
            echo "Hint: run \`go mod why google.golang.org/grpc\` to see the dep chain"
            echo "Fix: add a replace directive in go.mod or upgrade the offending dep"
            exit 1
          fi
          echo "grpc-go version matches workflow ($ACTUAL)"
```

**Step 3: Verify build**

```bash
GOWORK=off go build ./...
# Expected: clean
```

**Step 4: Commit**

```bash
git add go.mod go.sum .github/workflows/grpc-version-sync.yml
git commit -m "chore(deps): bump workflow to v1.0.0-rc1 + add grpc-version-sync CI gate"
```

**Rollback:** revert commit; restore prior workflow v0.27.2 dep; v0.14.2 plugin release remains usable.

### Task 8: Failing test for typed IaCProviderRequiredServer.EnumerateAll happy path

**Files:**
- Test: `internal/iacserver_test.go`

**Step 1: Write failing test**

```go
// internal/iacserver_test.go
package internal_test

import (
    "context"
    "testing"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
    diplugin "github.com/GoCodeAlone/workflow-plugin-digitalocean/internal"
)

// TestDOIaCEnumeratorServer_EnumerateAll asserts the typed server
// implementation returns spaces-key list correctly. Catches:
//   - Missing typed implementation (compile fail)
//   - Wrong message field assignment
//   - Hidden side effects in Initialize-not-called path
func TestDOIaCEnumeratorServer_EnumerateAll(t *testing.T) {
    server := diplugin.NewIaCServer(...)  // exact constructor TBD
    // ... set up fake godo client returning 1 SpacesKey
    resp, err := server.EnumerateAll(context.Background(), &pb.EnumerateAllRequest{
        ResourceType: "infra.spaces_key",
    })
    if err != nil {
        t.Fatalf("EnumerateAll: %v", err)
    }
    if len(resp.Outputs) != 1 {
        t.Fatalf("expected 1 output, got %d", len(resp.Outputs))
    }
    if resp.Outputs[0].ProviderId == "" {
        t.Errorf("ProviderID empty")
    }
    if !resp.Outputs[0].Sensitive["access_key"] {
        t.Errorf("Sensitive[access_key] not true")
    }
}
```

**Step 2: Run test — expect FAIL** (`undefined: diplugin.NewIaCServer`).

**Step 3: Commit failing test**

```bash
git add internal/iacserver_test.go
git commit -m "test(internal): failing test for typed IaCEnumerator EnumerateAll"
```

### Task 9: Implement IaCProviderRequiredServer + IaCProviderEnumeratorServer + delete legacy module_instance.go

**Files:**
- Create: `internal/iacserver.go` (~400 lines: typed server method implementations delegating to existing DOProvider/driver structs)
- Delete: `internal/module_instance.go` (entire file)
- Delete: `internal/dispatcher_coverage_test.go` (the v0.14.2 reflection test — now redundant since Go compile enforces it)
- Modify: `internal/main.go` (replace `sdk.ServiceInvoker` registration with `sdk.RegisterAllIaCProviderServices`)

**Step 1: Implement the typed server**

```go
// internal/iacserver.go
package internal

import (
    "context"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// DOIaCServer is the typed gRPC server-side implementation that satisfies
// pb.IaCProviderRequiredServer + multiple optional servers. Delegates to
// the existing DOProvider Go interface implementations.
type DOIaCServer struct {
    pb.UnimplementedIaCProviderRequiredServer  // forward-compat
    pb.UnimplementedIaCProviderEnumeratorServer
    pb.UnimplementedIaCProviderDriftDetectorServer
    pb.UnimplementedIaCProviderCredentialRevokerServer
    pb.UnimplementedIaCProviderMigrationRepairerServer
    pb.UnimplementedIaCProviderValidatorServer
    pb.UnimplementedIaCProviderDriftConfigDetectorServer

    provider *DOProvider
}

func NewIaCServer(provider *DOProvider) *DOIaCServer {
    return &DOIaCServer{provider: provider}
}

func (s *DOIaCServer) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
    cfg := initializeRequestToConfig(req)
    if err := s.provider.Initialize(ctx, cfg); err != nil {
        return nil, err
    }
    return &pb.InitializeResponse{}, nil
}

// ... ~25 more methods, each delegating to s.provider.X(...)

// EnumerateAll dispatches to provider.EnumerateAll. Replaces the legacy
// switch case "IaCProvider.EnumerateAll" in module_instance.go.
func (s *DOIaCServer) EnumerateAll(ctx context.Context, req *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {
    outs, err := s.provider.EnumerateAll(ctx, req.ResourceType)
    if err != nil {
        return nil, err
    }
    return &pb.EnumerateAllResponse{Outputs: resourceOutputsToProto(outs)}, nil
}

// ... etc
```

**Step 2: Update main.go to use auto-registration via sdk.ServeIaCPlugin (per cycle 3 I-1: symbol name was wrong)**

```diff
-    sdk.Serve(&plugin.ServePlugins{
-        Modules: ...
-    })
+    iacServer := internal.NewIaCServer(provider)
+    sdk.ServeIaCPlugin(iacServer, sdk.IaCServeOptions{
+        PluginInfo: &sdk.PluginInfo{HandshakeConfig: sharedHandshakeConfig},
+    })
+    // sdk.ServeIaCPlugin uses hashicorp/go-plugin's GRPCServer callback;
+    // RegisterAllIaCProviderServices is invoked inside that callback per
+    // go-plugin v1.7.0 architecture.
+    // Single API call replaces sdk.Serve + manual registration.
```

The `sdk.ServeIaCPlugin` API + `sdk.IaCServeOptions` struct are added in PR 2 Task 29. Symbol name MUST be `IaCServeOptions` (not `ServeOptions`) — IaC-specific naming avoids collision with future generic helpers.

**Step 3: DELETE legacy files**

```bash
git rm internal/module_instance.go
git rm internal/dispatcher_coverage_test.go
```

**Step 4: Run tests + build**

```bash
GOWORK=off go build ./...
# Expected: clean
GOWORK=off go test ./internal/ -count=1 -v -run TestDOIaCEnumeratorServer_EnumerateAll
# Expected: PASS — typed server matches the Task 8 test
GOWORK=off go test ./...
# Expected: all green; legacy-targeting tests deleted alongside their code
```

**Step 5: Commit**

```bash
git add internal/iacserver.go internal/main.go
git commit -m "feat(internal): typed IaCProvider + Enumerator + 5 more optional gRPC servers; delete legacy module_instance.go"
```

**Rollback:** Cannot easily restore module_instance.go without re-deriving switch cases. Mitigation: previous commit (Task 7) is the safe rollback point — DO plugin v0.14.2 release still installable.

### Task 10: Implement remaining 4 optional services (DriftDetector, CredentialRevoker, MigrationRepairer, Validator, DriftConfigDetector)

**Files:**
- Modify: `internal/iacserver.go` (add ~5 method groups)
- Test: `internal/iacserver_optional_test.go`

**Step 1: Write failing tests for each optional service** (one Test* per service).

**Step 2: Implement each method group on `*DOIaCServer`** delegating to existing `*DOProvider` methods.

**Step 3: Run tests**

```bash
GOWORK=off go test ./internal/ -count=1 -v -run TestDOIaC
# Expected: all PASS
```

**Step 4: Commit**

```bash
git add internal/iacserver.go internal/iacserver_optional_test.go
git commit -m "feat(internal): implement 5 remaining optional IaC services on DOIaCServer"
```

### Task 11: Implement ResourceDriverServer (Spaces, Spaces_Key, App, Database, Droplet, etc. — 14 drivers)

**Files:**
- Create: `internal/resourcedriverserver.go` — typed wrapper that routes by resource_type to the existing per-driver Go interface implementations
- Test: `internal/resourcedriverserver_test.go`

**Step 1: Failing test**

```go
func TestResourceDriverServer_Create_DispatchesByType(t *testing.T) {
    // Test that Create(ctx, &pb.ResourceCreateRequest{Type: "infra.spaces_key", ...})
    // routes to SpacesKeyDriver.Create
}
```

**Step 2: Implement** — typed RPC wraps the existing per-resource-type driver dispatch:

```go
func (s *DOResourceDriverServer) Create(ctx context.Context, req *pb.ResourceCreateRequest) (*pb.ResourceCreateResponse, error) {
    driver, ok := s.driversByType[req.Type]
    if !ok {
        return nil, status.Errorf(codes.NotFound, "no driver for resource type %q", req.Type)
    }
    spec := resourceCreateRequestToSpec(req)
    out, err := driver.Create(ctx, spec)
    if err != nil {
        return nil, err
    }
    return &pb.ResourceCreateResponse{Output: resourceOutputToProto(out)}, nil
}
```

**Step 3: Tests + commit per Task 8/9 pattern.**

### Task 12: Add wftest BDD contract test asserting capabilities-match-registration

**Files:**
- Modify: `wftest/bdd/strict.go` (workflow side, but committed in this DO plugin PR via the workflow rc1 dep)

Actually — this is a workflow-side change. Move to PR 4 (workflow cutover). Skip in PR 3.

### Task 13: Tag DO plugin v1.0.0

**Step 1: Verify CI green** (all tests + cross-plugin-build matrix).

**Step 2: Push branch + open PR + merge.**

**Step 3: After PR 3 merges to DO main:**

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin main
git tag -a v1.0.0 <merge-commit-sha> -m "v1.0.0: typed gRPC IaC services (force-cutover Phase 1)
- pb.IaCProviderRequiredServer + 6 optional services implemented
- internal/module_instance.go DELETED (legacy switch dispatcher removed)
- Consumes workflow v1.0.0-rc1; ready for workflow v1.0.0 final cutover"
git push origin v1.0.0
```

**Step 4: Wait for GoReleaser to publish DO v1.0.0.**

**Verification (per Task 7 runtime-launch-validation):** built artifact installable; `wfctl plugin list` shows DO v1.0.0 with typed IaC service registrations.

**Rollback:** v1.0.0 tag is published; cannot un-publish. v0.14.2 remains installable for old workflow versions.

---

## PR 4: feat(workflow): wfctl cutover to typed pb.IaCProviderClient + delete legacy IaC paths (workflow v1.0.0 final)

Held in DRAFT until PR 3 (DO v1.0.0) is published. Then merges + tags workflow v1.0.0 final.

### Task 14: ADRs 0024, 0025, 0026

**Files:**
- Create: `decisions/0024-iac-typed-force-cutover.md`
- Create: `decisions/0025-iac-optional-method-typed-services-not-bool.md`
- Create: `decisions/0026-iac-direct-grpc-client-no-wrapper.md`

**Change class:** Documentation. Verification: spell-check + render preview. No rollback required.

**Step 1: Write each ADR using `superpowers:recording-decisions` template** (Status, Context, Decision, Consequences, Alternatives Rejected).

**Step 2: Commit**

```bash
git add decisions/0024-*.md decisions/0025-*.md decisions/0026-*.md
git commit -m "docs(adr): record IaC typed force-cutover decisions (0024 + 0025 + 0026)"
```

### Task 15: Add wftest BDD contract test for capabilities-match-registration (cycle 4 belt-and-braces)

**Files:**
- Modify: `wftest/bdd/strict.go` — add `AssertProviderCapabilitiesMatchRegistration(t *testing.T, provider any, plugin loadedPlugin)`
- Test: `wftest/bdd/strict_iac_test.go`

**Step 1: Write failing test using a stub plugin that has manually-registered services missing one** — assert helper fails.

**Step 2: Implement helper.** Iterates Go provider's interface satisfaction set; iterates plugin's gRPC server registered services; asserts equivalence; reports specific missing registrations.

**Step 3: Tests + commit.**

### Task 30: Typed-client → `interfaces.IaCProvider` adapter (per plan-phase cycle 1 C-1/C-3)

**Files:**
- Create: `cmd/wfctl/iac_typed_adapter.go` — adapter type that wraps `pb.IaCProviderRequiredClient` + optional clients map AND satisfies the existing `interfaces.IaCProvider` Go interface
- Test: `cmd/wfctl/iac_typed_adapter_test.go`

**Why:** Engine consumers (`module/infra_module.go`, `iac/wfctlhelpers/apply.go`, etc.) call provider methods via the `interfaces.IaCProvider` Go interface. The typed pb client doesn't satisfy that interface natively. The adapter:
- Type: `*typedIaCAdapter` satisfies `interfaces.IaCProvider`
- Each Go-interface method translates to a typed RPC call on the underlying `pb.IaCProviderRequiredClient`
- Optional methods on `interfaces.X` sub-interfaces translate to optional-client lookup + typed RPC; `errors.Is(err, ErrProviderMethodUnimplemented)` if optional service isn't registered

This is NOT a hand-written string-marshalling proxy (the thing ADR-0026 forbids). It's a thin typed-call dispatcher — interface method directly maps to typed RPC, no `map[string]any`. Engine consumers get the same `interfaces.IaCProvider` they had; bug class is closed at the gRPC bridge.

**Step 1: Failing test** — assert `*typedIaCAdapter` satisfies `interfaces.IaCProvider`; assert `EnumerateAll(ctx, "type")` typed RPC dispatch works.

**Step 2: Implement** — ~300 lines, one method per interface method, each wrapping one typed RPC.

**Step 3: Tests + commit.**

### Task 16: wfctl-side typed client — replace `remoteIaCProvider` proxy with direct `pb.IaCProviderRequiredClient` + adapter for engine consumers

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go` — REWRITE `discoverAndLoadIaCProvider` to return typed clients
- Create: `cmd/wfctl/iac_errors.go` — typed Go error translation from gRPC status codes
- Delete: `remoteIaCProvider` struct (~600 lines), `remoteResourceDriver` struct (~300 lines), all `InvokeService` call sites in cmd/wfctl that target IaC

**Step 1: Failing tests for new typed-client path**

```go
// cmd/wfctl/deploy_providers_typed_test.go
func TestDiscoverAndLoadIaCProvider_ReturnsTypedClient(t *testing.T) {
    // Load a fake plugin via in-process gRPC.
    // Assert returned interface is pb.IaCProviderRequiredClient (typed).
}
```

**Step 2: Run test** — FAIL (function doesn't yet return typed client).

**Step 3: Implement** — rewrite `discoverAndLoadIaCProvider` to:
1. Call `GetContractRegistry` on the loaded plugin handle
2. Verify `IaCProviderRequired` service is registered (else: typed error pointing to `wfctl plugin update`)
3. Construct `pb.NewIaCProviderRequiredClient(handle.conn)` + map of optional clients keyed by service name
4. **Wrap in `typedIaCAdapter` from Task 30** so the return type still satisfies `interfaces.IaCProvider` for engine + wfctl consumers
5. Return `interfaces.IaCProvider` (the adapter), not the raw typed client

Engine-side consumers (`module/infra_module.go`, `iac/wfctlhelpers/apply.go`, etc.) see no API change — they continue to receive `interfaces.IaCProvider` and call methods on it. The typed transport is invisible to them.

For wfctl callers (audit-keys, prune, rotate-and-prune, cleanup, drift, status, plan, apply, destroy) that don't need the `interfaces.IaCProvider` shape — they can call typed RPC methods on the underlying client directly via a typed-client accessor on the adapter (e.g., `adapter.TypedClient()`). This avoids the marshalling-roundtrip overhead for hot-path callers per ADR-0026 spirit.

**Test files for cycle 1 I-1 enumeration**:
The wfctl-side test files that need DELETE-vs-CONVERT decisions (per cycle 1 plan-phase I-1):
- `cmd/wfctl/deploy_providers_remote_iac_test.go` — DELETE alongside the remoteIaCProvider it tests
- `cmd/wfctl/deploy_providers_dispatch_matrix_test.go` — REWRITE to test the typed adapter dispatch matrix
- `cmd/wfctl/deploy_providers_strict_bridge_coverage_test.go` — DELETE (now redundant; compile-time interface satisfaction replaces the reflection-based coverage check)
- `cmd/wfctl/deploy_providers_test.go` — KEEP, update typed-adapter sites
- `cmd/wfctl/infra_strict_mode_test.go` — KEEP, update to use typed-adapter test fixtures
- `cmd/wfctl/infra_typed_e2e_test.go` (new from Task 6) — KEEP, exercises typed-RPC E2E

Per-test-file decision is committed as part of Task 16; aggregate diff (~1500-2000 lines: ~600 deleted + ~400 added + ~400-1000 modified test fixtures).

**Step 4: Delete legacy paths**

```bash
# Specific deletions per design rev5 §Removed surface
git rm cmd/wfctl/deploy_providers_remote.go  # if remoteIaCProvider lives in its own file; else extract from deploy_providers.go
```

**Step 5: Run all tests**

```bash
GOWORK=off go test ./...
# Expected: green; legacy-targeting tests removed alongside legacy code
```

**Step 6: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/iac_errors.go cmd/wfctl/deploy_providers_typed_test.go
git commit -m "feat(wfctl): replace remoteIaCProvider proxy with direct pb.IaCProviderRequiredClient (cycle 1 Alt C adoption)"
```

**Rollback:** revert commit + emergency v0.99.x maintenance tag from pre-cutover commit. Operators stay on v0.27.x + DO v0.14.x indefinitely.

### Task 17: Convert 5 type-assert sites to typed RPC calls + capability-discovery

**Files:**
- Modify: `cmd/wfctl/infra_cleanup.go` (line ~97 — `interfaces.Enumerator` type-assert)
- Modify: `cmd/wfctl/infra_status_drift.go` (line ~107 — `interfaces.DriftConfigDetector`)
- Modify: `cmd/wfctl/infra_apply_refresh.go` (line ~54 — `interfaces.DriftConfigDetector`)
- Modify: `cmd/wfctl/infra_bootstrap.go` (line ~331 — `interfaces.ProviderCredentialRevoker`)
- Modify: `cmd/wfctl/infra_align_rules.go` (R-A10 — `interfaces.ProviderValidator`)

**Step 1: Per file, write failing tests** using the typed-client mock + capability-discovery (registered service map).

**Step 2: Implement** — replace `if x, ok := p.(interfaces.X); ok` with `if optClient, ok := optionals["IaCProviderXService"]; ok` (using the optional-client map from Task 16).

**Step 3: Tests + commit per file.**

### Task 18: Workflow plugin loader pre-flight gate — refuse legacy plugins

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (or wherever plugin loading happens)
- Test: `cmd/wfctl/plugin_install_strict_test.go`

**Step 1: Failing test** — load a stub plugin handle that lacks `IaCProviderRequired` service registration; assert wfctl returns typed error pointing to `wfctl plugin update`.

**Step 2: Implement pre-flight gate** in `wfctl deploy` (and any IaC-using command). Read `GetContractRegistry`; if any pinned IaC plugin doesn't advertise `IaCProviderRequired` → fail loud:

```
plugin "workflow-plugin-digitalocean" v0.14.2 uses legacy InvokeService dispatch removed in workflow v1.0.0.
Migration: edit .wfctl-lock.yaml to pin v1.0.0+, then re-run `wfctl plugin install`.
See: https://github.com/GoCodeAlone/workflow/blob/main/docs/runbooks/iac-typed-cutover.md
```

**Step 3: Add runbook**

```
docs/runbooks/iac-typed-cutover.md
```

Operator-facing: upgrade order ("plugins first, then wfctl"), `.wfctl-lock.yaml` migration, troubleshooting.

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_strict_test.go docs/runbooks/iac-typed-cutover.md
git commit -m "feat(wfctl): pre-flight gate refusing legacy IaC plugins + iac-typed-cutover runbook"
```

**Rollback:** revert commit; wfctl falls back to loading legacy plugins (which would then fail at the typed-client call site with a worse error message).

### Task 19: Update wfctl-strict-contracts CI gate — narrow IaC scope

**Files:**
- Modify: `cmd/wfctl/plugin_audit.go` — skip IaC-interface-coverage checks (now compile-time enforced); keep Module/Step/Trigger checks
- Modify: `wfctl-strict-contracts` CI YAML if separate

**Step 1: Failing test** — assert audit doesn't report IaC interfaces as "needing strict-contract advertisement" (since they're now compile-time-enforced).

**Step 2: Implement** — edit the audit logic to filter IaC services out of the strict-contracts coverage requirement.

**Step 3: Tests + commit.**

### Task 20: Workflow rebuild + integration test against DO v1.0.0 + tag v1.0.0 final

**Files:**
- Modify: `.github/workflows/cross-plugin-build-test.yml` — add DO plugin v1.0.0 to the matrix

**Step 1: Local build + test against DO v1.0.0**

```bash
# Replace go.mod's DO test dep
GOWORK=off go test -tags=integration ./plugin/external/sdk/... -run TestIaC_EndToEnd
# Expected: PASS using real DO v1.0.0 binary
```

**Step 2: Commit + push + merge PR 4**

After CI passes against DO v1.0.0 + Copilot review clean:

```bash
gh pr merge <PR4> --repo GoCodeAlone/workflow --squash --admin --delete-branch
```

**Step 3: Tag workflow v1.0.0 final**

```bash
git tag -a v1.0.0 <merge-commit-sha> -m "v1.0.0: IaC typed gRPC force-cutover

Replaces string-method dispatch (InvokeService) with typed gRPC services
for IaCProvider + ResourceDriver. Plugins MUST be ≥v1.0.0.

Operator upgrade order: bump DO plugin pin to v1.0.0+ FIRST, then bump
wfctl to v1.0.0+ SECOND. Pre-flight gate refuses legacy plugins.

Per design rev5 (commit 6073c3ce); 4 adversarial review cycles.
Closes 2 of 4 bug classes (missing client bridge, missing server dispatcher)
at compile time."
git push origin v1.0.0
```

**Step 4: Wait for GoReleaser v1.0.0 publish.**

**Verification (runtime-launch-validation per Step 1b trigger):** install wfctl v1.0.0 + DO v1.0.0; run `wfctl infra audit-keys --type infra.spaces_key` end-to-end against staging DO; assert success.

**Rollback:** revert merge commit + cut emergency v0.99.x maintenance tag from pre-cutover commit. Operators pin to v0.99.x + DO v0.14.2 indefinitely. Documented in CHANGELOG.

---

## PR 5: chore(deps): core-dump bump WFCTL_VERSION to v1.0.0 + DO plugin pin to v1.0.0 + per-file YAML audit

### Task 31: State-file-compat verification PRE-flight (per plan-phase cycle 1 I-4)

**Files (workflow repo, part of PR 4):**
- Create: `test/fixtures/state-v0.14.2.json` — state file produced by v0.14.2 wfctl (captured during PR 4 prep)
- Create: `cmd/wfctl/state_compat_test.go` — read v0.14.2 state via v1.0.0 read path

**Step 1: Operator copies REAL state file from existing staging deploy (per cycle 3 C-1 — `--state-file` and `--dry-run` flags don't exist in v0.14.2 wfctl)**

The operator already has a state file in production: `coredump-staging/iac-state/state.json` in the Spaces backend. They copy that file to `test/fixtures/state-v0.14.2.json` as a one-time manual capture:

```bash
# Operator-side: download from Spaces backend
aws s3 cp \
  s3://coredump-staging/iac-state/state.json \
  test/fixtures/state-v0.14.2.json \
  --endpoint-url=https://nyc3.digitaloceanspaces.com

# Add fixture metadata header (jq edit; see Task 22 for schema)
```

This avoids the need for a runnable v0.14.2 wfctl binary entirely. The fixture IS the real production state.

**Step 2: Add test asserting v1.0.0 reads v0.14.2 state cleanly**

```go
func TestStateFileCompat_v0_14_2_to_v1_0_0(t *testing.T) {
    // Read fixture; assert no schema errors; assert key fields present
}
```

**Step 3: Run**

```bash
GOWORK=off go test -run TestStateFileCompat_v0_14_2 -v
# Expected: PASS — confirms v1.0.0 reads v0.14.2 state cleanly, unblocking PR 5
```

**If FAIL**: PR 5 cascade-block surfaces. Plan response:
- File a separate workflow PR (`feat: state-file v0.14.2 compat shim`) that adds the compatibility layer
- Hold PR 5 (and consequently PRs 6-9) until shim PR merges
- Document gap in PR 4's CHANGELOG

This pre-flight catches the cycle 1 I-4 cascade-block risk BEFORE PR 5 merges.

**Step 4: Commit + include in PR 4**

```bash
git add test/fixtures/state-v0.14.2.json cmd/wfctl/state_compat_test.go
git commit -m "test: state-file-compat v0.14.2 → v1.0.0 read path (PR 4 pre-flight, prevents PR 5 cascade-block per plan-phase cycle 1 I-4)"
```

### Task 21: Conditional two-variable model — `WFCTL_VERSION` + `WFCTL_LEGACY_STATE_VERSION` (only if Task 31 indicates state-file-compat issues)

**Per plan-phase cycle 2 I-3-NEW:** the two-variable model was hardcoded in rev2 regardless of Task 31 verification. If 20-bis passes (v1.0.0 reads v0.14.2 state cleanly), the legacy variable is dead weight + adds operator confusion. Make this CONDITIONAL.

**Decision tree (executed at PR 5 prep time, after PR 4 merges):**

- **If Task 31 state-file-compat test PASSED:** v1.0.0 reads v0.14.2 state cleanly. Use SINGLE variable (`WFCTL_VERSION`). Bump every workflow YAML pin to `${{ vars.WFCTL_VERSION }} = v1.0.0`. No legacy split needed. Remove the `WFCTL_LEGACY_STATE_VERSION` plan from this task.
- **If Task 31 state-file-compat test FAILED:** keep two-variable model below. Files with state-file-compat dependency stay on legacy version until shim PR lands.

**Apply: as captured by Task 31 result. The plan author records the chosen branch in the PR 5 commit message.**

**Files (core-dump repo):**
- Modify: `.wfctl-lock.yaml` (DO plugin pin → v1.0.0)
- Modify: `wfctl.yaml` (DO plugin pin → v1.0.0)
- Modify: 5 `.github/workflows/*.yml` files using current wfctl: `bootstrap.yml`, `image-launch-ci.yml`, `tc2-cutover.yml`, `drift-recovery.yml`, plus the `prune-spaces-keys.yml` + `rotate-spaces-key.yml` already migrated
- DO NOT modify: `teardown.yml`, `deploy.yml` (rollback path), `registry-retention.yml` (these stay on `WFCTL_LEGACY_STATE_VERSION` for state-file-compat per cycle 3 I-2)

**Step 1: Add new GH variable**

```bash
gh variable set WFCTL_LEGACY_STATE_VERSION --repo GoCodeAlone/core-dump --body 'v0.14.2'
gh variable set WFCTL_VERSION --repo GoCodeAlone/core-dump --body 'v1.0.0'
```

**Step 2: Per-file audit — replace hardcoded `version: vX.Y.Z` with the appropriate variable**

In each `.github/workflows/*.yml` file in scope, find:
```yaml
      - uses: GoCodeAlone/setup-wfctl@<sha> # v1
        with:
          version: v0.21.2  # hardcoded
```

Replace with:
```yaml
      - uses: GoCodeAlone/setup-wfctl@<sha> # v1
        with:
          version: ${{ vars.WFCTL_VERSION }}
```

For files using legacy semantic (`teardown.yml`, `deploy.yml`, `registry-retention.yml`): replace with `${{ vars.WFCTL_LEGACY_STATE_VERSION }}`.

**Step 3: Add regression-prevention CI gate**

```yaml
# .github/workflows/wfctl-pin-discipline.yml
name: wfctl Pin Discipline
on: [pull_request]
jobs:
  no-hardcoded-pins:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Verify no hardcoded wfctl version pins in workflows
        run: |
          set -euo pipefail
          # Allow setup-wfctl@<sha> action pin (intentional); allow vars.WFCTL_*_VERSION refs
          if grep -nE 'version: v[0-9]' .github/workflows/*.yml | grep -v 'WFCTL_'; then
            echo "::error::Hardcoded wfctl version pin found. Use vars.WFCTL_VERSION (current) or vars.WFCTL_LEGACY_STATE_VERSION (legacy state-file-compat)."
            exit 1
          fi
          echo "No hardcoded pins; CI gate green"
```

**Step 4: Update `.wfctl-lock.yaml` + `wfctl.yaml`** to DO plugin v1.0.0.

**Step 5: Verify all workflows still parse**

```bash
for f in .github/workflows/*.yml; do
  python3 -c "import yaml; yaml.safe_load(open('$f'))" || echo "INVALID: $f"
done
```

**Step 6: Commit**

```bash
git add .wfctl-lock.yaml wfctl.yaml .github/workflows/
git commit -m "chore(deps): bump WFCTL_VERSION to v1.0.0; per-file YAML pin audit (cycle 3 I-2)"
```

**Rollback:** downgrade `WFCTL_VERSION` GH variable; revert `.wfctl-lock.yaml` pin; existing v0.14.2 plugin remains installed.

### Task 22: Operator-captured state-file-compat verification

**Per plan-phase cycle 3 C-1**: rev2's automated fixture-capture relied on `docker run ghcr.io/gocodealone/wfctl:v0.14.2` (image doesn't exist) + `--state-file` / `--output` flags that don't exist in v0.14.2 wfctl OR current main. Mechanism is unrunnable. Replaced with operator-captured fixture model.

**Files (core-dump repo):**
- Create: `test/fixtures/state-v0.14.2.json` (fixture: REAL state file from operator's existing v0.14.2 staging deploy)
- Create: `cmd/state_compat_check/main.go` (Go program that reads the fixture via v1.0.0 wfctl's state-decoder and asserts no schema errors)
- Create: `.github/workflows/state-file-compat.yml` (CI gate that runs the Go program)

**Step 1: Operator captures fixture (one-time manual action)**

The operator copies the current `state.json` from their staging Spaces backend (the file v0.14.2 wfctl already wrote during a real deploy) into `test/fixtures/state-v0.14.2.json`. Document the path in the test fixture file's header:
```json
{
  "_fixture_meta": {
    "captured_from": "staging Spaces backend at coredump-staging/iac-state/v0.14.2/state.json",
    "captured_at": "2026-05-10",
    "captured_by_wfctl_version": "v0.14.2",
    "purpose": "cross-version state-file-compat verification per plan task 22"
  },
  "resources": [...]
}
```

**Step 2: Write Go program that reads the fixture via v1.0.0 wfctl's state-decoder**

```go
// cmd/state_compat_check/main.go (NEW)
package main

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/GoCodeAlone/workflow/iac/state"  // v1.0.0 state decoder
)

// state_compat_check exits 0 if the fixture state file decodes cleanly via v1.0.0;
// exits 1 if any schema error / field-loss / decode failure.
func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: state_compat_check <state-file.json>")
        os.Exit(2)
    }
    data, err := os.ReadFile(os.Args[1])
    if err != nil {
        fmt.Fprintf(os.Stderr, "read: %v\n", err)
        os.Exit(1)
    }
    var s state.State  // v1.0.0 typed state struct
    if err := json.Unmarshal(data, &s); err != nil {
        fmt.Fprintf(os.Stderr, "v1.0.0 cannot decode v0.14.2 state file: %v\n", err)
        os.Exit(1)
    }
    if len(s.Resources) == 0 {
        fmt.Fprintln(os.Stderr, "decoded but resources slice empty — possible field name drift")
        os.Exit(1)
    }
    fmt.Printf("OK: decoded %d resources from v0.14.2 state via v1.0.0 decoder\n", len(s.Resources))
}
```

(If the actual v1.0.0 state package import path differs, adapt — the test program's job is "use the v1.0.0 wfctl module's state decoder to read the fixture; fail loud on any error".)

**Step 3: CI gate**

```yaml
# .github/workflows/state-file-compat.yml
name: State File Cross-Version Compat
on: [pull_request]
jobs:
  compat:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
      - name: Build state_compat_check
        run: go build -o /tmp/state_compat_check ./cmd/state_compat_check
      - name: Read v0.14.2 state via v1.0.0 decoder
        run: /tmp/state_compat_check test/fixtures/state-v0.14.2.json
```

**Step 4: Run locally; commit + push.**

If gate FAILS: `WFCTL_LEGACY_STATE_VERSION` files MUST stay pinned to v0.14.2 until state-file-compat shim lands as a separate workflow PR. Document the gap in the commit message.

```bash
git add .github/workflows/state-file-compat.yml test/fixtures/state-v0.14.2.json cmd/state_compat_check/main.go
git commit -m "ci: cross-version state-file-compat gate via operator-captured fixture (plan-phase cycle 3 C-1 fix)"
```

**Rollback:** revert commit; CI loses the gate.

---

## PR 6: chore(deps): buymywishlist wfctl + DO plugin pin bump

### Task 23: Survey + replace hardcoded YAML pins; bump WFCTL_VERSION + DO plugin pin

**Files (BMW repo):**
- Modify: `.wfctl-lock.yaml`, `wfctl.yaml`, `.github/workflows/*.yml`
- Add: regression-prevention CI gate (mirrors core-dump Task 21 Step 3)

**Step 1: Survey hardcoded pins**

```bash
cd /Users/jon/workspace/buymywishlist
grep -nE 'version: v[0-9]' .github/workflows/*.yml | grep -v 'setup-wfctl@'
```

**Step 2-4: Same pattern as core-dump Task 21 — variable rewrite + regression gate.**

**Step 5: Commit + open PR + admin-merge per `feedback_version_bump_immediate_merge`.**

---

## PR 7: chore(deps): workflow-cloud wfctl pin bump

### Task 24: Pin bump (no IaC consumers; safe)

**Files (workflow-cloud repo):**
- Modify: `go.mod` workflow dep → v1.0.0
- Modify: any wfctl version pins in CI YAML (survey + apply same pattern as Task 21)

**Step 1: Verify no IaC interface usage**

```bash
grep -rn "interfaces.IaCProvider\|interfaces.ResourceDriver" /Users/jon/workspace/workflow-cloud
# Expected: 0 matches (verified per cycle 1 I-5 + cycle 2 verification)
```

**Step 2-4: Standard pin-bump PR pattern.**

---

## PR 8: chore(deps): ratchet + ratchet-cli wfctl pin bump

### Task 25: ratchet pin bump

Same as Task 24 for ratchet repo.

### Task 26: ratchet-cli pin bump

Same as Task 24 for ratchet-cli repo.

---

## PR 9: chore(deps): workflow-cloud-ui wfctl pin bump

### Task 27: workflow-cloud-ui Go-side pin bump

Same as Task 24 for workflow-cloud-ui repo.

### Task 28: Final cross-PR verification

After all 9 PRs merged + workflow v1.0.0 published + DO v1.0.0 published + all pin-bump PRs merged:

**Verification checklist:**

1. **Compile-time enforcement smoke test:** add a stub method to `pb.IaCProviderRequiredServer` in workflow proto in a temporary branch; attempt to build DO plugin without implementing it → MUST FAIL with `*DOIaCServer does not implement pb.IaCProviderRequiredServer (missing method NewMethod)`. Revert temp branch.
2. **Runtime smoke test:** install wfctl v1.0.0 + DO v1.0.0; run `wfctl infra audit-keys --type infra.spaces_key` against staging DO; assert non-zero output (catches the ~199 orphan keys still in DO post-spaces-key plan).
3. **Pre-flight gate test:** install wfctl v1.0.0 with DO plugin v0.14.2 pinned; run `wfctl deploy` → MUST FAIL with the typed error pointing to `wfctl plugin update`.
4. **Cross-plugin-build CI:** workflow CI matrix includes DO v1.0.0 + passes.
5. **Pin-discipline CI:** core-dump + BMW PR CIs detect any hardcoded wfctl pin → fail loud.
6. **State-file-compat:** core-dump's state-file-compat gate is green; or the gap is documented.
7. **Mandate verification:** `git grep -E 'remoteIaCProvider|remoteResourceDriver' workflow/cmd/wfctl/` returns ZERO matches; `git grep -E 'case "IaCProvider\.' workflow-plugin-digitalocean/internal/` returns ZERO matches.

If any of 1-7 fails, escalate to operator with which check + last-known-good commit.

---

## Citations

All design facts traced to:
- `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (rev5, commit `6073c3ce`)
- `docs/plans/2026-05-10-strict-contracts-force-cutover-design.adversarial-review-{1,2,3,4}.md` (4 adversarial cycles)
- `cmd/wfctl/deploy_providers.go:267-649` — `remoteIaCProvider` proxy (19 methods to replace)
- `cmd/wfctl/deploy_providers.go:229` — type-assert that AWS/GCP/Azure/Tofu plugins fail (verifying out-of-scope claim)
- `plugin/external/sdk/interfaces.go:145-163` — legacy `ServiceInvoker / ServiceContextInvoker / TypedServiceInvoker` (KEPT for non-IaC consumers per cycle 3 C-1)
- `plugin/external/proto/plugin.proto:30, 100-137` — `GetContractRegistry` RPC (KEPT) + ContractRegistry messages
- `workflow-plugin-digitalocean/internal/module_instance.go:35-125` — DO plugin's switch dispatcher (DELETED entirely)
- `workflow-plugin-digitalocean/internal/dispatcher_coverage_test.go` — v0.14.2 reflection test (DELETED, redundant)
- Workspace memory `feedback_force_strict_contracts_no_compat` — user mandate
- Workspace memory `feedback_per_agent_worktree_per_task_pr` — multi-PR coordination pattern
- `project_p0_core_dump_wfctl_bump_shipped` — legacy `v0.14.2` pin rationale (state-file-compat)
