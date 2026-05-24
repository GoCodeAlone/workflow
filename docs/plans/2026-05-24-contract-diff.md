# verify-capabilities contract-diff Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `wfctl plugin verify-capabilities` (#765/#769 shipped at v0.63.2) with a typed-IaC service diff: add `PluginManifest.IaCServices []string` field, add `sdk.BuildContractRegistryForPlugin` namespace-filter helper, rewire `iacPluginServiceBridge.GetContractRegistry` to use it, and append a directional diff in `runPluginVerifyCapabilities` against `plugin.json.iacServices`.

**Architecture:** Single PR. Mirror existing `IaCStateBackends` precedent for the manifest field + nested-promotion. SDK helper derives namespace prefix from `pb.IaCProviderRequired_ServiceDesc.ServiceName` via TrimSuffix (single source of truth keyed to .proto). Bridge rewiring at `plugin/external/sdk/iacserver.go:302`. Verify-capabilities extension: ONE new RPC `pbClient.GetContractRegistry(ctx, Empty)` after existing `pbClient.GetManifest` call at line 131; explicit `codes.Unimplemented` branch maps to empty registry; directional diff (FAIL missing-from-binary, WARN extra-in-binary). Sweep of 4 IaC plugins (aws/azure/gcp/digitalocean) is out-of-scope (separate per-repo PRs after this workflow PR lands).

**Tech Stack:** Go (wfctl + SDK), `pb.PluginService` gRPC (raw client), `pb.IaCProviderRequired_ServiceDesc.ServiceName` canonical prefix derivation.

**Base branch:** `main`

**Design doc:** `docs/plans/2026-05-24-contract-diff-design.md` (cycle 4 PASS adversarial).

**Issue:** workflow#767

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 5
**Estimated Lines of Change:** ~400 (manifest field + SDK helper + bridge rewire + verify-cap extension + 3 fixture scenarios + tests)

**Out of scope:**
- Sweep of 4 IaC plugin repos (aws/azure/gcp/digitalocean) — separate per-repo PRs after this workflow PR lands and v0.64+ ships.
- `validate-contract` static enforcement of non-empty `iacServices` for `type:"iac"` plugins.
- Multi-namespace support beyond `workflow.plugin.external.iac.*`.
- Auto-population of `iacServices` from runtime introspection.
- Embedded plugin.json verification (`sdk.WithManifestProvider` doesn't surface this field on `pb.Manifest`).
- ResourceDriver/IaCStateBackend semantic split — `iacServices` includes ALL `workflow.plugin.external.iac.*` services when registered.
- Refactor `registeredIaCServices` into a new file — direct call works (both `package main` per cycle-4 I-2).

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(sdk+wfctl): contract-diff extension for verify-capabilities (workflow#767) | Task 1, Task 2, Task 3, Task 4, Task 5 | feat/767-contract-diff |

**Status:** Draft

---

### Task 1: Add `IaCServices` field to `PluginManifest` + nested-promotion

**Change class:** Internal logic refactor.

**Files:**
- Modify: `plugin/manifest.go` — add `IaCServices` field after `IaCStateBackends` (line 56); extend UnmarshalJSON legacy-object branch (line 159) for nested-promotion.
- Modify: `plugin/manifest_test.go` — add tests for top-level + nested-promotion + omitempty paths.

**Step 1: Write failing tests** (edit existing SINGLE import block; add `"encoding/json"` if missing)

```go
func TestPluginManifest_IaCServices_TopLevel(t *testing.T) {
	const j = `{"name":"x","version":"1.0.0","author":"a","description":"d","iacServices":["workflow.plugin.external.iac.IaCProviderRequired"]}`
	var m PluginManifest
	if err := json.Unmarshal([]byte(j), &m); err != nil { t.Fatal(err) }
	if len(m.IaCServices) != 1 || m.IaCServices[0] != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("IaCServices = %v", m.IaCServices)
	}
}

func TestPluginManifest_IaCServices_NestedPromotion(t *testing.T) {
	const j = `{"name":"x","version":"1.0.0","author":"a","description":"d","capabilities":{"iacServices":["workflow.plugin.external.iac.IaCProviderRequired","workflow.plugin.external.iac.IaCProviderFinalizer"]}}`
	var m PluginManifest
	if err := json.Unmarshal([]byte(j), &m); err != nil { t.Fatal(err) }
	if len(m.IaCServices) != 2 {
		t.Errorf("IaCServices = %v, want 2 entries promoted from nested capabilities", m.IaCServices)
	}
}

func TestPluginManifest_IaCServices_OmitWhenEmpty(t *testing.T) {
	m := PluginManifest{Name: "x", Version: "1.0.0", Author: "a", Description: "d"}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "iacServices") {
		t.Errorf("empty IaCServices should be omitted; got %s", b)
	}
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestPluginManifest_IaCServices -count=1 ./plugin/...`
Expected: FAIL `m.IaCServices undefined`.

**Step 3: Implement**

In `plugin/manifest.go` after line 56 (`IaCStateBackends`), add:

```go
// IaCServices lists the typed IaC service names this plugin serves
// (fully-qualified gRPC service names, e.g.
// "workflow.plugin.external.iac.IaCProviderRequired"). Authored in
// plugin.json either as a top-level "iacServices" key OR nested under
// "capabilities.iacServices" (UnmarshalJSON's object branch promotes
// the nested form, same as IaCStateBackends). The engine cross-checks
// these against the plugin's runtime ContractRegistry via wfctl plugin
// verify-capabilities (workflow#767).
//
// Orthogonal to IaCStateBackends (which lists backend NAMES, not gRPC
// service names). A plugin that registers the IaCStateBackend service
// AND lists its backend names will appear in BOTH manifest fields.
IaCServices []string `json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
```

In the UnmarshalJSON `case '{':` legacy-object branch (line 150 `legacyCaps` struct), add:
```go
IaCServices []string `json:"iacServices"`
```
After line 159 (`m.IaCStateBackends = appendUnique(...)`), add:
```go
m.IaCServices = appendUnique(m.IaCServices, legacyCaps.IaCServices...)
```

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run TestPluginManifest_IaCServices -count=1 ./plugin/...`
Expected: 3 tests PASS.

**Step 5: Commit + push**

```bash
git add plugin/manifest.go plugin/manifest_test.go
git commit -m "feat(plugin): add IaCServices manifest field with nested-promotion (workflow#767 Task 1)"
git push
```

---

### Task 2: Add `BuildContractRegistryForPlugin` SDK helper

**Change class:** Internal logic refactor (new exported helper).

**Files:**
- Modify: `plugin/external/sdk/contracts.go` — append helper after `BuildContractRegistry`.
- Modify: `plugin/external/sdk/contracts_test.go` (create if absent) — tests.

**Step 1: Write failing tests**

Create `plugin/external/sdk/contracts_test.go`:

```go
package sdk

import (
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

func TestBuildContractRegistryForPlugin_NilServer(t *testing.T) {
	reg := BuildContractRegistryForPlugin(nil, "workflow.plugin.external.iac.")
	if reg == nil { t.Fatal("want non-nil") }
	if len(reg.Contracts) != 0 { t.Errorf("want 0 contracts; got %d", len(reg.Contracts)) }
}

func TestBuildContractRegistryForPlugin_FiltersByPrefix(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &stubPluginService{})
	go func() {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		_ = s.Serve(l)
	}()
	defer s.Stop()
	reg := BuildContractRegistryForPlugin(s, "workflow.plugin.external.iac.")
	if len(reg.Contracts) != 1 {
		t.Fatalf("want 1 contract (iac.IaCProviderRequired); got %d: %v", len(reg.Contracts), reg.Contracts)
	}
	if reg.Contracts[0].ServiceName != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("unexpected service: %s", reg.Contracts[0].ServiceName)
	}
}

type stubIaCRequired struct{ pb.UnimplementedIaCProviderRequiredServer }
type stubPluginService struct{ pb.UnimplementedPluginServiceServer }
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestBuildContractRegistryForPlugin -count=1 ./plugin/external/sdk/...`
Expected: FAIL `undefined: BuildContractRegistryForPlugin`.

**Step 3: Implement**

In `plugin/external/sdk/contracts.go` append after existing `BuildContractRegistry`:

```go
// BuildContractRegistryForPlugin enumerates gRPC services registered on
// grpcSrv whose name STARTS WITH namespacePrefix and returns a
// *pb.ContractRegistry with one SERVICE-kind STRICT_PROTO ContractDescriptor
// per matching service. Filters out go-plugin infra services (PluginService,
// GRPCBroker, GRPCStdio, grpc.health.v1.Health) so downstream contract-diff
// (workflow#767) sees only plugin-owned services.
//
// Safe to call with nil server; returns empty (but non-nil) registry.
// Names alphabetically sorted for stable diff output.
//
// Typical caller: iacPluginServiceBridge.GetContractRegistry derives prefix
// from pb.IaCProviderRequired_ServiceDesc.ServiceName minus the ".IaCProviderRequired"
// suffix so the filter cannot drift from the .proto package declaration.
//
// BuildContractRegistry (full-surface, no filter) is retained for callers
// that want every registered service.
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespacePrefix string) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if grpcSrv == nil { return registry }
	info := grpcSrv.GetServiceInfo()
	names := make([]string, 0, len(info))
	for name := range info {
		if strings.HasPrefix(name, namespacePrefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		registry.Contracts = append(registry.Contracts, &pb.ContractDescriptor{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: name,
			Mode:        pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		})
	}
	return registry
}
```

Add `"strings"` to the existing single import block.

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run TestBuildContractRegistryForPlugin -count=1 ./plugin/external/sdk/...`
Expected: both tests PASS.

**Step 5: Commit + push**

```bash
git add plugin/external/sdk/contracts.go plugin/external/sdk/contracts_test.go
git commit -m "feat(sdk): BuildContractRegistryForPlugin namespace-filtering helper (workflow#767 Task 2)"
git push
```

---

### Task 3: Rewire `iacPluginServiceBridge.GetContractRegistry` to use filtered helper

**Change class:** Internal logic refactor (SDK bridge swap; cleans wire output for the 4 sweep targets).

**Files:**
- Modify: `plugin/external/sdk/iacserver.go:302` — swap `BuildContractRegistry` for `BuildContractRegistryForPlugin`.
- Modify: `plugin/external/sdk/iacserver_internal_test.go` (create if absent; `package sdk` for internal access) — regression test.

**Step 1: Write failing test**

Append (or create) `plugin/external/sdk/iacserver_internal_test.go`:

```go
package sdk

import (
	"context"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestIaCBridge_ContractRegistry_FiltersInfra(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &iacPluginServiceBridge{grpcSrv: s})
	bridge := &iacPluginServiceBridge{grpcSrv: s}
	reg, err := bridge.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil { t.Fatal(err) }
	for _, c := range reg.Contracts {
		if !strings.HasPrefix(c.ServiceName, "workflow.plugin.external.iac.") {
			t.Errorf("bridge surfaced non-iac service %q after filter", c.ServiceName)
		}
	}
	found := false
	for _, c := range reg.Contracts {
		if c.ServiceName == "workflow.plugin.external.iac.IaCProviderRequired" { found = true }
	}
	if !found { t.Error("expected IaCProviderRequired in filtered registry") }
}
```

**Step 2: Run test — verify FAIL**

Run: `GOWORK=off go test -run TestIaCBridge_ContractRegistry_FiltersInfra -count=1 ./plugin/external/sdk/...`
Expected: FAIL — pre-rewire bridge surfaces PluginService + GRPCBroker + GRPCStdio + health.

**Step 3: Rewire bridge**

In `plugin/external/sdk/iacserver.go` around line 302, replace:

```go
func (b *iacPluginServiceBridge) GetContractRegistry(_ context.Context, _ *emptypb.Empty) (*pb.ContractRegistry, error) {
	return BuildContractRegistry(b.grpcSrv), nil
}
```

with:

```go
func (b *iacPluginServiceBridge) GetContractRegistry(_ context.Context, _ *emptypb.Empty) (*pb.ContractRegistry, error) {
	// Derive prefix from canonical proto descriptor (workflow#767 §1) so the
	// filter cannot drift from the .proto package declaration.
	prefix := strings.TrimSuffix(pb.IaCProviderRequired_ServiceDesc.ServiceName, ".IaCProviderRequired") + "."
	return BuildContractRegistryForPlugin(b.grpcSrv, prefix), nil
}
```

Add `"strings"` to the existing import block. **DO NOT add a second `import (...)` declaration.**

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -count=1 ./plugin/external/sdk/...`
Expected: new test PASS; existing SDK tests PASS (no regression).

Run conformance regression check (per design audit — existing consumers of `GetContractRegistry` already SERVICE-kind-filter or namespace-filter client-side):

Run: `GOWORK=off go test -run TestPluginConformance -count=1 -timeout 300s ./cmd/wfctl/...`
Expected: PASS.

**Step 5: Commit + push**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_internal_test.go
git commit -m "feat(sdk): IaC bridge GetContractRegistry filters infra services (workflow#767 Task 3)"
git push
```

**Rollback:** revert this commit — bridge returns to surfacing all registered services (current main behavior). No data migration.

---

### Task 4: Extend `runPluginVerifyCapabilities` with directional contract-diff

**Change class:** Plugin / extension (CLI subcommand behavior change; adds new diff dimension).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go` — add `diffIaCServices` helper + extend `runPluginVerifyCapabilities` after the existing Name/Version diff at line 137.
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go` — unit tests for diff helper.

**Step 1: Write failing tests** (pure logic; integration tests in Task 5)

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`. Edit existing SINGLE import block to add `pb "github.com/GoCodeAlone/workflow/plugin/external/proto"` if absent. DO NOT add a second `import (...)`.

```go
func TestDiffIaCServices_Match(t *testing.T) {
	missing, extra := diffIaCServices(
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"},
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"})
	if len(missing) != 0 || len(extra) != 0 { t.Errorf("missing=%v extra=%v", missing, extra) }
}

func TestDiffIaCServices_MissingFromBinary(t *testing.T) {
	declared := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderFinalizer",
	}
	advertised := []string{"workflow.plugin.external.iac.IaCProviderRequired"}
	missing, extra := diffIaCServices(declared, advertised)
	if len(missing) != 1 || missing[0] != "workflow.plugin.external.iac.IaCProviderFinalizer" {
		t.Errorf("want Finalizer missing; got %v", missing)
	}
	if len(extra) != 0 { t.Errorf("want no extras; got %v", extra) }
}

func TestDiffIaCServices_ExtraInBinary(t *testing.T) {
	missing, extra := diffIaCServices(
		[]string{"workflow.plugin.external.iac.IaCProviderRequired"},
		[]string{
			"workflow.plugin.external.iac.IaCProviderRequired",
			"workflow.plugin.external.iac.IaCProviderFinalizer",
		})
	if len(missing) != 0 { t.Errorf("missing=%v", missing) }
	if len(extra) != 1 || extra[0] != "workflow.plugin.external.iac.IaCProviderFinalizer" {
		t.Errorf("want Finalizer extra; got %v", extra)
	}
}

func TestDiffIaCServices_EmptyDeclared_SkipsDiff(t *testing.T) {
	missing, extra := diffIaCServices(nil, []string{"workflow.plugin.external.iac.IaCProviderRequired"})
	if missing != nil || extra != nil { t.Errorf("empty LHS should skip; got missing=%v extra=%v", missing, extra) }
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestDiffIaCServices -count=1 ./cmd/wfctl/...`
Expected: FAIL `undefined: diffIaCServices`.

**Step 3: Implement diff helper + wire into runPluginVerifyCapabilities**

In `cmd/wfctl/plugin_verify_capabilities.go`:

A. Edit existing single import block. Add: `"sort"`, `"google.golang.org/grpc/codes"`, `"google.golang.org/grpc/status"`. **DO NOT add a second `import (...)` declaration.**

B. Append at end of file (after `preflightBinary`):

```go
// diffIaCServices computes directional set-difference of declared
// (plugin.json.iacServices) vs advertised (binary's filtered ContractRegistry).
// Returns (missing, extra) where:
//   - missing: declared but not advertised → caller emits FAIL (truth-loop bug).
//   - extra: advertised but not declared → caller emits WARN (additive doc-lag).
// Empty declared returns (nil, nil) → caller must skip the diff entirely.
func diffIaCServices(declared, advertised []string) (missing, extra []string) {
	if len(declared) == 0 { return nil, nil }
	declSet := make(map[string]bool, len(declared))
	for _, s := range declared { declSet[s] = true }
	advSet := make(map[string]bool, len(advertised))
	for _, s := range advertised { advSet[s] = true }
	for _, s := range declared {
		if !advSet[s] { missing = append(missing, s) }
	}
	for _, s := range advertised {
		if !declSet[s] { extra = append(extra, s) }
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
```

C. In `runPluginVerifyCapabilities`, AFTER the existing Name/Version diff (after the `failures` slice is populated but BEFORE the `if len(failures) > 0` summary block, which is currently at line 143), insert:

```go
	// Contract-diff (workflow#767). One new RPC after GetManifest.
	contractReg, regErr := pbClient.GetContractRegistry(ctx, &emptypb.Empty{})
	switch {
	case regErr != nil && status.Code(regErr) == codes.Unimplemented:
		// Empty registry semantics — skip-if-LHS-empty handles non-IaC plugins;
		// non-empty plugin.json.iacServices → directional diff FAILs every
		// declared service (correct: plugin advertises nothing).
		contractReg = nil
	case regErr != nil:
		return fmt.Errorf("GetContractRegistry RPC: %w (stderr: %s)", regErr, stderr.String())
	}
	advertisedServices := registeredIaCServices(contractReg)
	missingSvc, extraSvc := diffIaCServices(declared.IaCServices, advertisedServices)
	for _, s := range missingSvc {
		failures = append(failures, fmt.Sprintf("iacServices: plugin.json declares %q but binary does not advertise it", s))
	}
	for _, s := range extraSvc {
		// WARN, not FAIL — directional diff per design §3.
		fmt.Fprintf(os.Stderr, "WARN  %s: binary advertises %q not in plugin.json.iacServices (additive — consider updating plugin.json)\n", declared.Name, s)
	}
```

D. Add a small helper `registeredIaCServices` adapter at end of file (returns SERVICE-kind names from a ContractRegistry, sorted):

```go
// registeredIaCServices returns SERVICE-kind contract names from reg
// (already-filtered by namespace prefix at the SDK bridge per Task 3).
// Returns nil for nil reg.
func registeredIaCServices(reg *pb.ContractRegistry) []string {
	if reg == nil { return nil }
	names := make([]string, 0, len(reg.Contracts))
	for _, c := range reg.Contracts {
		if c.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			names = append(names, c.GetServiceName())
		}
	}
	sort.Strings(names)
	return names
}
```

(Note: there is an existing `registeredIaCServices` in `deploy_providers.go:324` that returns `map[string]bool`. The two have different return types; this file's helper has different shape and is local to verify-capabilities. Per design's "no refactor needed" decision, do NOT consolidate — accept the local helper.)

**Step 4: Run unit tests — verify PASS**

Run: `GOWORK=off go test -run "TestDiffIaCServices|TestVerifyCapabilities" -count=1 ./cmd/wfctl/...`
Expected: 4 diff unit tests PASS + existing verify-capabilities tests PASS (regression check).

Build verify: `GOWORK=off go build ./...` exit 0.

**Step 5: Commit + push**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities contract-diff (directional FAIL/WARN) (workflow#767 Task 4)"
git push
```

**Rollback:** revert this commit + Task 3 — verify-capabilities returns to Name/Version diff only; bridge returns to unfiltered.

---

### Task 5: Integration tests — 3 IaC fixture scenarios end-to-end

**Change class:** Plugin / extension.

**Files:**
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-missing-service/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-extra-service/{plugin.json,main.go,go.mod,go.sum}`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go` — 3 integration test functions reusing existing `buildFixtureBinaryForVerify` helper from #765.

**Step 1: Generate fixtures** (one-off script)

Save as `/tmp/gen-iac-fixtures.sh`:

```bash
#!/bin/bash
set -euo pipefail
BASE=cmd/wfctl/testdata/verify_capabilities
mkdir -p "$BASE/iac-good" "$BASE/iac-missing-service" "$BASE/iac-extra-service"

# iac-good: implements Required + Finalizer; plugin.json declares both.
cat > "$BASE/iac-good/plugin.json" <<'JSON'
{
  "name": "verify-iac-good",
  "version": "0.0.0",
  "minEngineVersion": "v0.62.0",
  "author": "test fixture",
  "description": "IaC fixture: registered services match plugin.json declared services",
  "iacServices": [
    "workflow.plugin.external.iac.IaCProviderRequired",
    "workflow.plugin.external.iac.IaCProviderFinalizer"
  ]
}
JSON
cat > "$BASE/iac-good/main.go" <<'GO'
package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

var Version = "dev"

type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderFinalizerServer
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-iac-good"}, nil
}

func (fixture) Finalize(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "test fixture")
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		BuildVersion: sdk.ResolveBuildVersion(Version),
	})
}
GO
cat > "$BASE/iac-good/go.mod" <<'MOD'
module github.com/test/iac-good

go 1.26.0

require github.com/GoCodeAlone/workflow v0.63.2

replace github.com/GoCodeAlone/workflow => ../../../../..
MOD

# iac-missing-service: implements Required ONLY; declares both → FAIL on Finalizer
cat > "$BASE/iac-missing-service/plugin.json" <<'JSON'
{
  "name": "verify-iac-missing",
  "version": "0.0.0",
  "minEngineVersion": "v0.62.0",
  "author": "test fixture",
  "description": "IaC fixture: plugin.json declares more services than binary registers",
  "iacServices": [
    "workflow.plugin.external.iac.IaCProviderRequired",
    "workflow.plugin.external.iac.IaCProviderFinalizer"
  ]
}
JSON
cat > "$BASE/iac-missing-service/main.go" <<'GO'
package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

var Version = "dev"

type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
	// Does NOT embed IaCProviderFinalizerServer — type-assertion in
	// sdk.ServeIaCPlugin skips the optional Finalizer registration.
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-iac-missing"}, nil
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		BuildVersion: sdk.ResolveBuildVersion(Version),
	})
}
GO
cat > "$BASE/iac-missing-service/go.mod" <<'MOD'
module github.com/test/iac-missing-service

go 1.26.0

require github.com/GoCodeAlone/workflow v0.63.2

replace github.com/GoCodeAlone/workflow => ../../../../..
MOD

# iac-extra-service: implements both; declares only Required → WARN on Finalizer
cat > "$BASE/iac-extra-service/plugin.json" <<'JSON'
{
  "name": "verify-iac-extra",
  "version": "0.0.0",
  "minEngineVersion": "v0.62.0",
  "author": "test fixture",
  "description": "IaC fixture: binary registers more services than plugin.json declares",
  "iacServices": [
    "workflow.plugin.external.iac.IaCProviderRequired"
  ]
}
JSON
cat > "$BASE/iac-extra-service/main.go" <<'GO'
package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

var Version = "dev"

type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderFinalizerServer
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-iac-extra"}, nil
}

func (fixture) Finalize(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "test fixture")
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		BuildVersion: sdk.ResolveBuildVersion(Version),
	})
}
GO
cat > "$BASE/iac-extra-service/go.mod" <<'MOD'
module github.com/test/iac-extra-service

go 1.26.0

require github.com/GoCodeAlone/workflow v0.63.2

replace github.com/GoCodeAlone/workflow => ../../../../..
MOD
```

**Step 2: Generate + tidy + standalone-build verify**

```bash
bash /tmp/gen-iac-fixtures.sh
for d in cmd/wfctl/testdata/verify_capabilities/iac-*/; do
  (cd "$d" && GOWORK=off go mod tidy)
  (cd "$d" && GOWORK=off go build -mod=readonly -o /tmp/p .) && echo "$d: ok" || { echo "$d: FAIL"; exit 1; }
done
```
Expected: all 3 print `ok`.

**Step 3: Write integration tests**

Append to `cmd/wfctl/plugin_verify_capabilities_test.go` (DO NOT add a second `import (...)` declaration — existing block already has needed imports):

```go
func TestVerifyCapabilities_IaCGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_IaCMissingService(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-missing-service", "v0.1.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-missing-service"})
	if err == nil { t.Fatal("want FAIL on missing Finalizer, got nil") }
	if !strings.Contains(err.Error(), "iacServices:") {
		t.Errorf("want iacServices: error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "IaCProviderFinalizer") {
		t.Errorf("want Finalizer-specific error, got: %v", err)
	}
}

func TestVerifyCapabilities_IaCExtraService(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "iac-extra-service", "v0.1.0")
	// Extra services produce WARN (stderr) but exit 0 per design §3.
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/iac-extra-service"}); err != nil {
		t.Fatalf("want PASS (extra=WARN, not FAIL); got: %v", err)
	}
}
```

**Step 4: Run integration tests — verify PASS**

Run: `GOWORK=off go test -run TestVerifyCapabilities_IaC -count=1 -timeout 180s ./cmd/wfctl/...`
Expected: 3 scenario tests PASS.

Full regression: `GOWORK=off go test -count=1 -timeout 300s ./cmd/wfctl/...` — expect all PASS.

**Step 5: Commit + push**

```bash
git add cmd/wfctl/testdata/verify_capabilities/iac-good cmd/wfctl/testdata/verify_capabilities/iac-missing-service cmd/wfctl/testdata/verify_capabilities/iac-extra-service cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "test(wfctl): 3 IaC integration fixture scenarios (workflow#767 Task 5)"
git push
```

**Rollback:** revert this commit + Task 4 + Task 3 — full revert of contract-diff path; verify-capabilities returns to Name/Version diff only.

---

## Final verification (post-Task-5)

```bash
# 1. All tests pass
GOWORK=off go test -count=1 -timeout 300s ./...

# 2. Lint clean
GOWORK=off go vet ./...
GOWORK=off golangci-lint run ./cmd/wfctl/... ./plugin/...

# 3. wfctl help correct
GOWORK=off go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl plugin verify-capabilities --help

# 4. Conformance regression — Task 3 touched sdk/iacserver.go
GOWORK=off go test -run TestPluginConformance -count=1 -timeout 300s ./cmd/wfctl/...

# 5. SDK unit tests pass
GOWORK=off go test -count=1 ./plugin/external/sdk/...
```

## Rollback

PR adds manifest field, SDK helper, SDK bridge filter change, wfctl subcommand extension. Path:
- `git revert <merge-sha>` reverts all 5 commits cleanly. Plugin.json files with `iacServices` field (from any sweep PRs filed independently) continue to parse — older wfctl ignores unknown field per `encoding/json` default.
- Bridge filter rollback: pre-change surfaced all gRPC services including infra. Reverting restores noisy registry — downstream consumers (deploy_providers, conformance) handle noise via own filters today, no regression.
- Manifest field rollback: `IaCServices` gone; existing manifests with the field load fine (unknown field ignored).

Backwards-compat: subcommand behavior is additive at the diff level. Older wfctl callers without verify-capabilities continue to work; new wfctl on old plugin.json (no `iacServices`) skips contract-diff per `diffIaCServices` empty-LHS short-circuit.

## Implementer notes

- **PUSH AFTER EACH COMMIT** per #765 squash-merge debacle lesson. Verify `git log origin/feat/767-contract-diff..HEAD` is empty before opening PR.
- Edit existing SINGLE `import (...)` blocks; never add a second `import (...)` declaration.
- Worktree is rebased onto current main (HEAD f43420535 from #771 merge). All shipped #765 verify-capabilities code is present.
- Task 4's local `registeredIaCServices` helper has different return type than the existing `deploy_providers.go:324` helper of the same name — both coexist in `package main` (function-scoped lookups distinguish; Go allows same name in different files of same package only with different signatures — VERIFY no clash; if Go complains, rename the new helper to `serviceNamesFromRegistry` or similar).
