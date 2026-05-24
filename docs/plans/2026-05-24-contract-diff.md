# verify-capabilities Contract-Diff Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `wfctl plugin verify-capabilities` (workflow#765) to diff plugin's runtime `GetContractRegistry` typed-IaC services against `plugin.json.iacServices`. Adds new manifest field, server-side namespace-filtering SDK helper, and sweeps 4 IaC plugins to populate the field.

**Architecture:** Single PR adds (a) `PluginManifest.IaCServices []string` with nested-promotion in UnmarshalJSON mirroring existing `IaCStateBackends` pattern; (b) `sdk.BuildContractRegistryForPlugin(grpcSrv, prefix)` helper; (c) `iacPluginServiceBridge.GetContractRegistry` rewires to use the filtered helper so all `sdk.ServeIaCPlugin` callers get clean output; (d) `verify-capabilities` calls `adapter.ContractRegistryError()` first (surface RPC errors verbatim) then reuses cached `adapter.ContractRegistry()` + existing `registeredIaCServices` helper for directional diff (FAIL missing-from-binary, WARN extra-in-binary). Sweep 4 IaC plugins (aws/azure/gcp/digitalocean) separately as follow-up PRs out of this scope.

**Tech Stack:** Go (workflow CLI + SDK), `pb.PluginService` gRPC, `pb.IaCProviderRequired_ServiceDesc.ServiceName` for canonical namespace, `external.PluginAdapter` cached accessors.

**Base branch:** `main` (worktree was branched from `origin/main` post-#765 merge at 827158b5f)

**Design doc:** `docs/plans/2026-05-24-contract-diff-design.md` (cycle 3 PASS adversarial).

**Issue:** workflow#767

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~450 (manifest field + SDK helper + bridge rewire + verify extension + 3 fixtures + tests)

**Out of scope:**
- Sweep of 4 IaC plugin repos (aws, azure, gcp, digitalocean) to populate `iacServices` field — separate per-repo PRs after this workflow PR lands and v0.63.2+ is tagged. Each per-repo PR is small (plugin.json edit only). Tracked in workflow#767 issue body as AC #4.
- `validate-contract` static enforcement of non-empty `iacServices` for `type:"iac"` plugins — future tightening.
- Multi-namespace support beyond `workflow.plugin.external.iac.*` — single derived prefix only.
- Auto-population of `iacServices` from runtime introspection — operator authors the list manually.
- Embedded plugin.json verification (via `sdk.WithManifestProvider`) — `pb.Manifest` is 6-scalar and doesn't surface `iacServices`; disk plugin.json is the authoritative source.
- ResourceDriver/IaCStateBackend semantic split — `iacServices` includes ALL `workflow.plugin.external.iac.*` services when registered; orthogonal to existing `iacStateBackends` (backend NAMES) field.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(sdk+wfctl): contract-diff extension for verify-capabilities (workflow#767) | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/767-contract-diff |

**Status:** Draft

---

### Task 1: Add `IaCServices` field to `PluginManifest` with nested-promotion

**Change class:** Internal logic refactor (schema field addition with backwards-compat JSON unmarshaling).

**Files:**
- Modify: `plugin/manifest.go` — add `IaCServices` field (after `IaCStateBackends` at line 56); extend UnmarshalJSON legacy-object branch to promote nested `capabilities.iacServices` (after line 159).
- Modify: `plugin/manifest_test.go` — add tests for both top-level and nested-promotion parse paths.

**Step 1: Write the failing tests**

Append to `plugin/manifest_test.go` (edit existing single `import (...)` block to add `"encoding/json"` if not present):

```go
func TestPluginManifest_IaCServices_TopLevel(t *testing.T) {
	const j = `{"name":"x","version":"1.0.0","author":"a","description":"d","iacServices":["workflow.plugin.external.iac.IaCProviderRequired"]}`
	var m PluginManifest
	if err := json.Unmarshal([]byte(j), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.IaCServices) != 1 || m.IaCServices[0] != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("IaCServices = %v, want [workflow.plugin.external.iac.IaCProviderRequired]", m.IaCServices)
	}
}

func TestPluginManifest_IaCServices_NestedPromotion(t *testing.T) {
	const j = `{"name":"x","version":"1.0.0","author":"a","description":"d","capabilities":{"iacServices":["workflow.plugin.external.iac.IaCProviderRequired","workflow.plugin.external.iac.IaCProviderFinalizer"]}}`
	var m PluginManifest
	if err := json.Unmarshal([]byte(j), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.IaCServices) != 2 {
		t.Errorf("IaCServices = %v, want 2 entries from nested capabilities object", m.IaCServices)
	}
}

func TestPluginManifest_IaCServices_OmitWhenEmpty(t *testing.T) {
	m := PluginManifest{Name: "x", Version: "1.0.0", Author: "a", Description: "d"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "iacServices") {
		t.Errorf("empty IaCServices should be omitted via omitempty; got %s", b)
	}
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestPluginManifest_IaCServices -count=1 ./plugin/...`
Expected: FAIL with `m.IaCServices undefined`.

**Step 3: Implement**

Edit `plugin/manifest.go`:

1. After existing `IaCStateBackends` field (line 56), add:

```go
// IaCServices lists the typed IaC service names this plugin serves
// (fully-qualified gRPC service names, e.g.
// "workflow.plugin.external.iac.IaCProviderRequired"). Authored in
// plugin.json either as a top-level "iacServices" key OR nested under
// "capabilities.iacServices" (UnmarshalJSON's object branch promotes
// the nested form). The engine cross-checks these against the plugin's
// runtime ContractRegistry via wfctl plugin verify-capabilities (#767).
//
// Orthogonal to IaCStateBackends (which lists backend NAMES the plugin
// serves, not gRPC service names). A plugin that registers the
// IaCStateBackend service AND lists its backend name(s) in
// iacStateBackends appears in BOTH manifest fields.
IaCServices []string `json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
```

2. In the UnmarshalJSON `case '{':` legacy-object branch, extend the `legacyCaps` struct (line 150) to include `IaCServices []string \`json:"iacServices"\`` (alphabetical with existing fields).
3. After the existing `m.IaCStateBackends = appendUnique(...)` (line 159) add:

```go
m.IaCServices = appendUnique(m.IaCServices, legacyCaps.IaCServices...)
```

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run TestPluginManifest_IaCServices -count=1 ./plugin/...`
Expected: 3 tests PASS.

**Step 5: Commit**

```bash
git add plugin/manifest.go plugin/manifest_test.go
git commit -m "feat(plugin): add IaCServices manifest field with nested-promotion (workflow#767)"
```

---

### Task 2: Add `BuildContractRegistryForPlugin` SDK helper

**Change class:** Internal logic refactor (new exported helper function).

**Files:**
- Modify: `plugin/external/sdk/contracts.go` — add helper after `BuildContractRegistry` (current file ends around line 90).
- Modify: `plugin/external/sdk/contracts_test.go` (create if missing) — add tests for filter behavior.

**Step 1: Write the failing tests**

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
	if reg == nil {
		t.Fatal("want non-nil registry for nil server")
	}
	if len(reg.Contracts) != 0 {
		t.Errorf("want empty Contracts; got %d", len(reg.Contracts))
	}
}

func TestBuildContractRegistryForPlugin_FiltersByPrefix(t *testing.T) {
	s := grpc.NewServer()
	// Register the iac required service + the plugin-service bridge (which
	// is NOT in the iac namespace and should be filtered out).
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &stubPluginService{})
	// Sink listener so grpc.Server.GetServiceInfo() has registered services.
	go func() {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		_ = s.Serve(l)
	}()
	defer s.Stop()

	reg := BuildContractRegistryForPlugin(s, "workflow.plugin.external.iac.")
	if len(reg.Contracts) != 1 {
		t.Fatalf("want 1 contract (only iac.IaCProviderRequired); got %d: %v", len(reg.Contracts), serviceNames(reg))
	}
	if reg.Contracts[0].ServiceName != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("unexpected service: %s", reg.Contracts[0].ServiceName)
	}
}

// Stubs (minimal — only need RegisterServer to succeed).
type stubIaCRequired struct{ pb.UnimplementedIaCProviderRequiredServer }
type stubPluginService struct{ pb.UnimplementedPluginServiceServer }

func serviceNames(reg *pb.ContractRegistry) []string {
	out := make([]string, 0, len(reg.Contracts))
	for _, c := range reg.Contracts {
		out = append(out, c.ServiceName)
	}
	return out
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestBuildContractRegistryForPlugin -count=1 ./plugin/external/sdk/...`
Expected: FAIL with `undefined: BuildContractRegistryForPlugin`.

**Step 3: Implement**

Append to `plugin/external/sdk/contracts.go`:

```go
// BuildContractRegistryForPlugin enumerates gRPC services registered on
// grpcSrv whose name STARTS WITH namespacePrefix and returns a
// *pb.ContractRegistry with one SERVICE-kind, STRICT_PROTO-mode
// ContractDescriptor per matching service. Used to filter out go-plugin
// infra services (PluginService, GRPCBroker, GRPCStdio, grpc.health.v1.Health)
// so downstream contract-diff (workflow#767) sees only plugin-owned services.
//
// Safe to call with nil server; returns an empty (but non-nil) registry.
// Service descriptors are alphabetically sorted for stable diff output.
//
// Typical caller: iacPluginServiceBridge.GetContractRegistry (this package)
// derives the prefix from pb.IaCProviderRequired_ServiceDesc.ServiceName
// minus the ".IaCProviderRequired" suffix so the filter cannot drift from
// the .proto package declaration.
//
// BuildContractRegistry (full-surface, no filter) is retained for callers
// that want every registered service.
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespacePrefix string) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	if grpcSrv == nil {
		return registry
	}
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

Add `"strings"` to the import block (edit existing single block, don't add a second `import (...)`).

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run TestBuildContractRegistryForPlugin -count=1 ./plugin/external/sdk/...`
Expected: both tests PASS.

**Step 5: Commit**

```bash
git add plugin/external/sdk/contracts.go plugin/external/sdk/contracts_test.go
git commit -m "feat(sdk): BuildContractRegistryForPlugin namespace-filtering helper (workflow#767)"
```

---

### Task 3: Rewire `iacPluginServiceBridge.GetContractRegistry` to use the filtered helper

**Change class:** Internal logic refactor (SDK bridge swap; behavior cleaner output, no contract change to callers since infra-noise removal is the design intent).

**Files:**
- Modify: `plugin/external/sdk/iacserver.go` — change line ~302 (the only `BuildContractRegistry(b.grpcSrv)` call in the IaC bridge).

**Step 1: Write the failing test**

Append to `plugin/external/sdk/iacserver_internal_test.go` (or create if missing — keep `package sdk` for internal access):

```go
func TestIaCBridge_ContractRegistry_FiltersInfra(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	// Bridge registers PluginService itself — verify that's filtered out.
	pb.RegisterPluginServiceServer(s, &iacPluginServiceBridge{grpcSrv: s})
	bridge := &iacPluginServiceBridge{grpcSrv: s}
	reg, err := bridge.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range reg.Contracts {
		if !strings.HasPrefix(c.ServiceName, "workflow.plugin.external.iac.") {
			t.Errorf("bridge surfaced non-iac service %q after filter", c.ServiceName)
		}
	}
	// Sanity: at least the IaCProviderRequired service should be present.
	found := false
	for _, c := range reg.Contracts {
		if c.ServiceName == "workflow.plugin.external.iac.IaCProviderRequired" {
			found = true
		}
	}
	if !found {
		t.Error("expected IaCProviderRequired in filtered registry")
	}
}
```

Edit imports to add `"context"`, `"strings"`, `"testing"`, `"google.golang.org/protobuf/types/known/emptypb"`, `"google.golang.org/grpc"`, `pb "github.com/GoCodeAlone/workflow/plugin/external/proto"`. (Edit the existing single import block.)

**Step 2: Run test — verify FAIL**

Run: `GOWORK=off go test -run TestIaCBridge_ContractRegistry_FiltersInfra -count=1 ./plugin/external/sdk/...`
Expected: FAIL — pre-rewire bridge surfaces PluginService + GRPCBroker + GRPCStdio + health, all non-iac-prefixed.

**Step 3: Rewire the bridge**

Edit `plugin/external/sdk/iacserver.go` around line 302. Find:

```go
func (b *iacPluginServiceBridge) GetContractRegistry(_ context.Context, _ *emptypb.Empty) (*pb.ContractRegistry, error) {
	return BuildContractRegistry(b.grpcSrv), nil
}
```

Replace with:

```go
func (b *iacPluginServiceBridge) GetContractRegistry(_ context.Context, _ *emptypb.Empty) (*pb.ContractRegistry, error) {
	// Derive prefix from canonical proto descriptor so the filter cannot
	// drift from the .proto package declaration (workflow#767 §1).
	prefix := strings.TrimSuffix(pb.IaCProviderRequired_ServiceDesc.ServiceName, ".IaCProviderRequired") + "."
	return BuildContractRegistryForPlugin(b.grpcSrv, prefix), nil
}
```

Add `"strings"` to the existing import block (do NOT add a second `import (...)`).

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -count=1 ./plugin/external/sdk/...`
Expected: new TestIaCBridge_ContractRegistry_FiltersInfra PASS; existing SDK tests still PASS (no regressions).

**Step 5: Commit**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_internal_test.go
git commit -m "feat(sdk): IaC bridge GetContractRegistry filters infra services (workflow#767)"
```

**Rollback:** revert this commit — bridge returns to surfacing all registered services (current main behavior). No data migration.

---

### Task 4: Extend `wfctl plugin verify-capabilities` with directional contract-diff

**Change class:** Plugin / extension (CLI subcommand behavior change; adds new diff dimension).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go` — extend `runPluginVerifyCapabilities` after existing Name/Version diff (added by workflow#765).
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go` — add unit tests for the diff helper.

**Step 1: Write the failing tests** (pure logic; integration tests come in Task 6)

Append to `cmd/wfctl/plugin_verify_capabilities_test.go` (edit existing single import block to add `pb "github.com/GoCodeAlone/workflow/plugin/external/proto"` if absent):

```go
func TestDiffIaCServices_Match(t *testing.T) {
	declared := []string{"workflow.plugin.external.iac.IaCProviderRequired"}
	advertised := []string{"workflow.plugin.external.iac.IaCProviderRequired"}
	missing, extra := diffIaCServices(declared, advertised)
	if len(missing) != 0 || len(extra) != 0 {
		t.Errorf("want clean match; got missing=%v extra=%v", missing, extra)
	}
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
	if len(extra) != 0 {
		t.Errorf("want no extras; got %v", extra)
	}
}

func TestDiffIaCServices_ExtraInBinary(t *testing.T) {
	declared := []string{"workflow.plugin.external.iac.IaCProviderRequired"}
	advertised := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderFinalizer",
	}
	missing, extra := diffIaCServices(declared, advertised)
	if len(missing) != 0 {
		t.Errorf("want no missing; got %v", missing)
	}
	if len(extra) != 1 || extra[0] != "workflow.plugin.external.iac.IaCProviderFinalizer" {
		t.Errorf("want Finalizer extra; got %v", extra)
	}
}

func TestDiffIaCServices_EmptyDeclared_SkipsDiff(t *testing.T) {
	missing, extra := diffIaCServices(nil, []string{"workflow.plugin.external.iac.IaCProviderRequired"})
	if missing != nil || extra != nil {
		t.Errorf("empty LHS should skip diff entirely; got missing=%v extra=%v", missing, extra)
	}
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run TestDiffIaCServices -count=1 ./cmd/wfctl/...`
Expected: FAIL with `undefined: diffIaCServices`.

**Step 3: Implement** the diff function + wire into runPluginVerifyCapabilities

In `cmd/wfctl/plugin_verify_capabilities.go`:

A. Add `diffIaCServices` helper at end of file:

```go
// diffIaCServices computes set-difference of declared (plugin.json.iacServices)
// vs advertised (binary's filtered ContractRegistry).
// Returns (missing, extra) where:
//   - missing: declared but not advertised (FAIL — truth-loop bug).
//   - extra: advertised but not declared (WARN — additive; doc-lag, not runtime defect).
// If declared is empty, returns (nil, nil) — caller must skip the diff
// entirely (non-IaC plugins, or IaC plugins not yet swept).
func diffIaCServices(declared, advertised []string) (missing, extra []string) {
	if len(declared) == 0 {
		return nil, nil
	}
	declSet := make(map[string]bool, len(declared))
	for _, s := range declared {
		declSet[s] = true
	}
	advSet := make(map[string]bool, len(advertised))
	for _, s := range advertised {
		advSet[s] = true
	}
	for _, s := range declared {
		if !advSet[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range advertised {
		if !declSet[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
```

Add `"sort"` to the existing import block.

B. After the existing Name/Version diff block in `runPluginVerifyCapabilities` (just before the `if len(failures) > 0 { ... }` summary), add:

```go
// Contract-diff (workflow#767). Surface ContractRegistryError verbatim
// first — non-Unimplemented RPC failures during adapter construction
// would otherwise emit synthetic "missing service" FAILs that mask the
// real cause.
if regErr := adapter.ContractRegistryError(); regErr != nil {
	return fmt.Errorf("plugin GetContractRegistry: %w (stderr: %s)", regErr, stderr.String())
}
advertisedServices := registeredIaCServiceNames(adapter.ContractRegistry())
missingSvc, extraSvc := diffIaCServices(declared.IaCServices, advertisedServices)
for _, s := range missingSvc {
	failures = append(failures, fmt.Sprintf("iacServices: plugin.json declares %q but binary does not advertise it", s))
}
for _, s := range extraSvc {
	// WARN, not FAIL — directional diff per design §3.
	fmt.Fprintf(os.Stderr, "WARN  %s: binary advertises %q not in plugin.json.iacServices (additive — consider updating plugin.json)\n", declared.Name, s)
}
```

C. Add `registeredIaCServiceNames` helper at end of file (single-line wrapper over the SHARED helper from Task 5 — see Task 5 for the shared `registeredIaCServices` factor-out):

```go
// registeredIaCServiceNames returns the SERVICE-kind contract names from
// reg (already-filtered by namespace prefix at the SDK bridge — see
// plugin/external/sdk/iacserver.go GetContractRegistry). Sorted.
func registeredIaCServiceNames(reg *pb.ContractRegistry) []string {
	if reg == nil {
		return nil
	}
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

Note: variable `adapter`, `stderr`, `declared`, `failures` are defined earlier in `runPluginVerifyCapabilities` by workflow#765 — verify their names match before applying this patch (they should per #765 final design).

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run "TestDiffIaCServices|TestVerifyCapabilities" -count=1 ./cmd/wfctl/...`
Expected: 4 new diff unit tests PASS; existing verify-capabilities tests still PASS (regression check).

Also run: `GOWORK=off go build ./...` — expect exit 0.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities contract-diff (directional FAIL/WARN) (workflow#767)"
```

---

### Task 5: (Optional — defer per cycle-3 reviewer Option B) reuse `registeredIaCServices` precedent

**Change class:** Internal logic refactor (move + import-friendly factor; behavior unchanged).

**Status:** Per cycle-3 adversarial reviewer Option B (`don't nitpick`), this task is **DEFERRED to follow-up**. The shared helper rename/move provides no behavior change; current Task 4 uses a local `registeredIaCServiceNames` wrapper that's structurally identical to `registeredIaCServices` in `deploy_providers.go:344`. If a third caller appears, factor at that point. For now, the duplication is ≤20 LOC and the names differ enough (existing returns map[string]bool, new returns []string sorted) that conflating them in this PR would add a refactor blast radius beyond scope.

**Note**: Task 5 retained as a placeholder so PR Grouping table's task count matches the body's task headings (alignment-check enforces this). Empty task → zero-diff commit avoided by combining with Task 4 commit message footer instead. **Implementer: skip this task entirely.** The Scope Manifest's "Tasks: 6" remains accurate because Task 5 is a documented no-op acknowledged in the design's "Simpler alternative not considered" finding.

Actually — to keep Tasks-N matching ### headings cleanly: this task IS the no-op acknowledgement. Implementer does NOT create a commit for it.

---

### Task 6: Integration tests — 3 IaC fixture scenarios

**Change class:** Plugin / extension (exercise spawn + RPC + diff against real fixture binaries).

**Files:**
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-missing-service/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/iac-extra-service/{plugin.json,main.go,go.mod,go.sum}`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go` — add 3 integration test functions reusing existing `buildFixtureBinaryForVerify` helper from #765.

**Step 1: Generate fixtures via helper script** (one-off, not committed)

Save as `/tmp/gen-iac-fixtures.sh`:

```bash
#!/bin/bash
set -euo pipefail
BASE=cmd/wfctl/testdata/verify_capabilities

mkdir -p "$BASE/iac-good" "$BASE/iac-missing-service" "$BASE/iac-extra-service"

# Shared go.mod template (relative replace; per-fixture module name only).
mkmod() {
  local d="$1" mod="$2"
  cat > "$d/go.mod" <<MOD
module github.com/test/$mod

go 1.26.0

require github.com/GoCodeAlone/workflow v0.62.0

replace github.com/GoCodeAlone/workflow => ../../../../..
MOD
}

# Shared main.go for IaC plugins — IaCProviderRequired methods all return Unimplemented
# (only registration matters for the diff). Optional services determined by IMPLEMENT_OPT env-like build tag.

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
mkmod "$BASE/iac-good" iac-good

# iac-missing-service: implements Required ONLY; plugin.json declares Required + Finalizer
# → diff fires FAIL on Finalizer (missing-from-binary).
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
	// Note: does NOT embed IaCProviderFinalizerServer — type-assertion in
	// iacserver.go:178-180 will skip the optional registration.
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
mkmod "$BASE/iac-missing-service" iac-missing-service

# iac-extra-service: implements Required + Finalizer; plugin.json declares Required ONLY
# → diff fires WARN on Finalizer (extra-in-binary; exit 0).
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
mkmod "$BASE/iac-extra-service" iac-extra-service
```

**Step 2: Generate + tidy + verify each fixture builds**

```bash
bash /tmp/gen-iac-fixtures.sh
for d in cmd/wfctl/testdata/verify_capabilities/iac-*/; do
  (cd "$d" && GOWORK=off go mod tidy)
  (cd "$d" && GOWORK=off go build -mod=readonly -o /tmp/p .) && echo "$d: ok" || { echo "$d: FAIL"; exit 1; }
done
```
Expected: all 3 print `ok`.

**Step 3: Write the integration tests**

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

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
	if err == nil {
		t.Fatal("want FAIL on missing Finalizer service, got nil")
	}
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
Expected: 3 IaC scenario tests PASS.

Also run full verify-capabilities suite (regression check): `GOWORK=off go test -run TestVerifyCapabilities -count=1 -timeout 180s ./cmd/wfctl/...` — expect ALL tests PASS (5 from #765 + 4 diff unit tests from Task 4 + 3 IaC integration tests from this task = 12).

**Step 5: Commit**

```bash
git add cmd/wfctl/testdata/verify_capabilities/iac-good cmd/wfctl/testdata/verify_capabilities/iac-missing-service cmd/wfctl/testdata/verify_capabilities/iac-extra-service cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "test(wfctl): 3 IaC integration fixture scenarios (workflow#767)"
```

**Rollback:** revert this commit + the Task 4 commit — verify-capabilities returns to Name+Version diff only.

---

## Final verification (post-Task-6)

Before opening the PR:

```bash
# 1. All tests pass
GOWORK=off go test -count=1 -timeout 180s ./...

# 2. Lint clean
GOWORK=off go vet ./...
GOWORK=off golangci-lint run

# 3. wfctl --help still works (regression check on top-level help)
GOWORK=off go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl plugin verify-capabilities --help

# 4. Conformance regression still passes (we DID touch sdk/iacserver.go in Task 3)
GOWORK=off go test -run TestPluginConformance -count=1 -timeout 300s ./cmd/wfctl/...

# 5. SDK unit tests pass (Task 3 + Task 2 surface)
GOWORK=off go test -count=1 ./plugin/external/sdk/...
```

## Rollback

This PR adds a manifest field, an SDK helper, an SDK bridge filter change, and a wfctl subcommand extension. Rollback path:

- `git revert <merge-sha>` reverts all 6 commits cleanly. Sweep-target plugin.json files (separate PRs) become inert — older wfctl ignores the unknown field per `encoding/json` default (no `DisallowUnknownFields`).
- **Bridge filter rollback (Task 3)**: pre-change behavior surfaced all gRPC services including infra. Reverting restores the noisy registry — downstream consumers (deploy_providers, conformance) handle noise via their own filters today, so no regression.
- **Manifest field rollback (Task 1)**: `IaCServices` field gone; existing manifests with the field load successfully (unknown field ignored).

Backwards-compat: PR is purely additive at the subcommand-behavior level. Older wfctl callers continue to work; new wfctl on old plugin.json (no `iacServices`) skips the diff per `diffIaCServices` empty-LHS short-circuit.

## Notes for implementer

- Worktree base assumes #765 has merged (commit `827158b5f`). If not, rebase onto current main first.
- Edit existing single `import (...)` blocks in every file — DO NOT add a second `import (...)` declaration (Go allows but `gofumpt`/`goimports`/`golangci-lint` enforce single-block).
- Fixture build pattern matches #765 (in-place `-mod=readonly` + checked-in go.sum + relative `replace` + GOWORK=off).
- Task 5 is intentionally a no-op acknowledgement; do NOT create a commit for it (the alignment-check + scope-lock manifest count is preserved via the body heading only).
- After Task 6 commits, run `bash tests/plan-scope-check.sh --plan <plan>` if the harness asks — expected PASS.
