# Workflow Strict-Contracts Ergonomics v0.51.3 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.51.3 with three surgical fixes (engine-side disk-manifest fallback, SDK manifest-embed helper, engine `_`-prefix strip for STRICT_PROTO) that unblock BMW deploy + every strict-cutover IaC + STRICT_PROTO plugin.

**Architecture:** Engine threads disk-loaded `*plugin.PluginManifest` (already loaded at `manager.go:108`) into `NewExternalPluginAdapter` as a fallback when gRPC `GetManifest` is Unimplemented or returns empty `Version`. New SDK helper `sdk.EmbedManifest([]byte) (*plugin.PluginManifest, error)` lets plugins compile-time-embed `plugin.json`. Engine strips `_`-prefix keys from cfg before `mapToTypedAny` (copy-on-clean; legacy `*structpb.Struct` path keeps `_config_dir`).

**Tech Stack:** Go 1.23, gRPC, hashicorp/go-plugin (GoCodeAlone fork v1.7.0), protobuf/protojson.

**Base branch:** `fix/strict-contracts-ergonomics-v0.51.3` (off origin/main); design + ADR already committed.

---

## R1 absorption notes (2026-05-12)

R1 adversarial review FAIL → revised:

- **F1 Critical:** missed caller `cmd/wfctl/plugin_conformance.go:327`. **Fix:** Task 1 Files list updated; pass `nil` (conformance probe doesn't load plugin.json from disk — tests plugin's own gRPC manifest, which is the intent of the verifier).
- **F2 Important:** Task 2's defensive `EngineManifest` overlay was redundant with Task 1's constructor overlay AND used inconsistent per-field merge precedence. **Fix:** Task 2 retained as **regression test only** (no code; asserts Task 1's overlay covers the empty-Version-but-no-error case). Single precedence rule: Task 1's constructor overlay replaces `a.manifest` wholesale when gRPC returns empty Version. Documented in adapter.go comment.
- **F3 Important:** Task 7 grep pattern had false positives + scope gap. **Fix:** Task 7 grep tightened to proto3 field-declaration shape only; audit scope expanded to include workflow's own `plugin/external/proto/`.
- **F4 Important:** `plugin.PluginManifest.Validate()` requires Name+Version+Author+Description (verified at `plugin/manifest.go:183-201`). **Fix:** `EmbedManifest` docstring documents all 4 required fields; Task 4 happy-path test includes all 4; Task 4 added explicit test for missing Author + missing Description.
- **F5 Important:** Task 3 step-level STRICT_PROTO coverage. **Fix:** Task 3 Step 6 expanded with explicit step-level descriptor test path.
- **F6 Important:** `MustEmbedManifest` panic semantics undocumented for rollback story. **Fix:** docstring expanded; ADR-0031 Consequences section notes plugin authors should use `EmbedManifest` with explicit error handling for production paths.
- **F7 Minor (over-decomposition):** acknowledged; tasks 1, 5, 6 each touch 3-4 files. Splitting further fragments the change set and increases reviewer context-switching. Kept as single tasks; each implementer can subdivide commits at their discretion.
- **F8 Minor (test package):** Task 6 tests use `package sdk` (internal access) — explicit in Files list.
- **F9 Minor (name mismatch):** added pre-flight check in Task 1; for all 6 wave plugins, `plugin.json:name` matches install directory name (verified per workspace audit; wave plugins use directory names equal to plugin.json:name).
- **F10 Minor (gRPC-wins precedence test):** added to Task 1 as third unit test.
- **F11 Minor (manager Validate ordering invariant):** noted in adapter.go comment + ADR.
- **F12 Minor (wfctl plugin install load-bearing):** noted in ADR-0031 Consequences.

## Scope Manifest

**PR Count:** 1
**Tasks:** 8
**Estimated Lines of Change:** ~500 (engine ~140, SDK ~90, tests ~270)

**Out of scope:**

- Plugin-side adoption of `sdk.EmbedManifest` (each wave plugin can adopt over time; no plugin change required to unblock BMW).
- Removing engine `_config_dir` injection at `engine.go:499` / `engine.go:1105` — legacy modules depend on it.
- Adding `_config_dir` to any proto schema — strip, not declare.
- Bug 3 (payments `TypedModuleProvider` without `ContractProvider`) — separate plugin repos, not workflow.
- Removing PR #627 `Unimplemented` tolerance — kept as defense-in-depth.
- Step config injection refactor (`engine.go:1105`) — strip applies symmetrically via `createTypedConfigRequest`, but step path uses separate code; verify in Task 3.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | fix(plugin/external): engine disk-manifest fallback + SDK embed helper + STRICT_PROTO `_`-prefix strip | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7, Task 8 | fix/strict-contracts-ergonomics-v0.51.3 |

**Status:** Draft

---

## Task 1: Engine disk-manifest fallback — `manifestFromDisk` helper + adapter signature change

**Change class:** Internal logic refactor (engine adapter), but **runtime-affecting** per `finishing-a-development-branch` Step 1b (plugin loading path). Include rollback note.

**Files:**
- Modify: `plugin/external/adapter.go:84-98` (signature of `NewExternalPluginAdapter`; replace `manifest = &pb.Manifest{Name: name}` fallback with `manifestFromDisk(diskManifest)`)
- Modify: `plugin/external/adapter.go:31-42` (add `diskManifest *plugin.PluginManifest` field to `ExternalPluginAdapter` struct)
- Add: `plugin/external/adapter.go` — `manifestFromDisk(*plugin.PluginManifest) *pb.Manifest` helper (place before `NewExternalPluginAdapter`)
- Modify: `plugin/external/manager.go:169` (pass `manifest` as 3rd arg to `NewExternalPluginAdapter`)
- **Modify: `cmd/wfctl/plugin_conformance.go:327`** (pass `nil` as 3rd arg — F1; the conformance probe deliberately tests the plugin's own gRPC manifest in isolation; disk-fallback is not its concern)
- Test: `plugin/external/adapter_test.go` (add 3 tests: `TestNewExternalPluginAdapterDiskManifestFallback`, `TestNewExternalPluginAdapterDiskManifestNilStillWorks`, `TestNewExternalPluginAdapterPrefersGRPCWhenVersionPresent`)
- Update: every other existing call to `NewExternalPluginAdapter` in `plugin/external/adapter_test.go` (append `, nil` to the 2-arg form)

**Step 1: Write failing tests** in `plugin/external/adapter_test.go`:

```go
func TestNewExternalPluginAdapterDiskManifestFallback(t *testing.T) {
    disk := &plugin.PluginManifest{
        Name:           "iac-plugin",
        Version:        "1.0.11",
        Author:         "GoCodeAlone",
        Description:    "DigitalOcean IaC provider",
        ConfigMutable:  true,
        SampleCategory: "iac",
    }
    a, err := NewExternalPluginAdapter("iac-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
        manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
    }}, disk)
    if err != nil {
        t.Fatalf("NewExternalPluginAdapter: %v", err)
    }
    if got := a.Version(); got != "1.0.11" {
        t.Fatalf("Version() = %q, want 1.0.11 (disk fallback)", got)
    }
    if got := a.Description(); got != "DigitalOcean IaC provider" {
        t.Fatalf("Description() = %q, want disk value", got)
    }
}

func TestNewExternalPluginAdapterDiskManifestNilStillWorks(t *testing.T) {
    a, err := NewExternalPluginAdapter("legacy-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
        manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
    }}, nil)
    if err != nil {
        t.Fatalf("NewExternalPluginAdapter with nil disk: %v", err)
    }
    if got := a.Name(); got != "legacy-plugin" {
        t.Fatalf("Name() = %q, want legacy-plugin (constructor name fallback)", got)
    }
    if got := a.Version(); got != "" {
        t.Fatalf("Version() = %q, want empty (no disk, no gRPC)", got)
    }
}

// F10 regression — gRPC value WINS when both gRPC and disk are non-empty.
// Locks in the precedence rule: disk is fallback for missing-or-empty gRPC fields only.
func TestNewExternalPluginAdapterPrefersGRPCWhenVersionPresent(t *testing.T) {
    disk := &plugin.PluginManifest{
        Name: "x", Version: "9.9.9", Author: "disk", Description: "disk desc",
    }
    a, err := NewExternalPluginAdapter("x", &PluginClient{client: &adapterTestPluginServiceClient{
        manifestResp: &pb.Manifest{Name: "x", Version: "1.0.0", Author: "grpc", Description: "grpc desc"},
    }}, disk)
    if err != nil {
        t.Fatalf("NewExternalPluginAdapter: %v", err)
    }
    if got := a.Version(); got != "1.0.0" {
        t.Fatalf("Version() = %q, want 1.0.0 (gRPC wins over disk)", got)
    }
    if got := a.Description(); got != "grpc desc" {
        t.Fatalf("Description() = %q, want grpc desc (gRPC wins)", got)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./plugin/external/ -run TestNewExternalPluginAdapterDiskManifestFallback -v`
Expected: FAIL with compile error — `NewExternalPluginAdapter` takes 2 args, test passes 3.

**Step 3: Add helper + change signature**

In `plugin/external/adapter.go` (before line 84):

```go
// manifestFromDisk field-maps a canonical *plugin.PluginManifest into the
// *pb.Manifest the adapter caches. Used as the disk-manifest fallback when
// the plugin's gRPC GetManifest RPC returns codes.Unimplemented or an empty
// Version. Maps all 6 scalar fields of pb.Manifest.
func manifestFromDisk(m *plugin.PluginManifest) *pb.Manifest {
    if m == nil {
        return nil
    }
    return &pb.Manifest{
        Name:           m.Name,
        Version:        m.Version,
        Author:         m.Author,
        Description:    m.Description,
        ConfigMutable:  m.ConfigMutable,
        SampleCategory: m.SampleCategory,
    }
}
```

Update struct (line 31-42):

```go
type ExternalPluginAdapter struct {
    name                string
    client              *PluginClient
    manifest            *pb.Manifest
    diskManifest        *plugin.PluginManifest // fallback when gRPC GetManifest is Unimplemented or returns empty Version
    contractRegistry    *pb.ContractRegistry
    contractRegistryErr error
    contracts           contractDescriptorCache
    contractTypes       *protoregistry.Types
    configFragment      []byte
    pluginDir           string
    triggerSetupErr     error
}
```

Change signature (line 84-98). Replace synthesized-name fallback with disk-fallback:

```go
// NewExternalPluginAdapter creates an adapter from a connected plugin client.
// diskManifest is the *plugin.PluginManifest loaded by the manager at
// manager.go:108 via pluginpkg.LoadManifest + Validate. It is used as the
// canonical fallback when the plugin's gRPC GetManifest RPC returns
// codes.Unimplemented (strict-cutover IaC plugins) or an empty Version
// (defensive). Pass nil only in tests that exercise the no-disk fallback
// path; production callers must pass the manager-loaded manifest.
func NewExternalPluginAdapter(name string, client *PluginClient, diskManifest *plugin.PluginManifest) (*ExternalPluginAdapter, error) {
    ctx := context.Background()
    manifest, err := client.client.GetManifest(ctx, &emptypb.Empty{})
    if err != nil {
        if status.Code(err) != codes.Unimplemented {
            return nil, fmt.Errorf("get manifest from plugin %s: %w", name, err)
        }
        // gRPC GetManifest is Unimplemented (strict-cutover IaC plugins served
        // via sdk.ServeIaCPlugin). Prefer disk-loaded plugin.json fields.
        if dm := manifestFromDisk(diskManifest); dm != nil {
            manifest = dm
        } else {
            manifest = &pb.Manifest{Name: name}
        }
    } else if manifest != nil && manifest.Version == "" {
        // gRPC returned a manifest but Version is empty (auto-synthesized or
        // misconfigured plugin). Overlay missing fields from disk if available.
        if dm := manifestFromDisk(diskManifest); dm != nil {
            manifest = dm
        }
    }
    // ... rest of body unchanged ...
}
```

Store `diskManifest` on the adapter:

```go
    a := &ExternalPluginAdapter{
        name:            name,
        client:          client,
        manifest:        manifest,
        diskManifest:    diskManifest,
        triggerSetupErr: triggerSetupErr,
    }
```

**Step 4: Update production callers**

`plugin/external/manager.go:169`:

```go
adapter, err := NewExternalPluginAdapter(name, pluginClient, manifest)
```

(The `manifest` var here is the `*plugin.PluginManifest` already loaded at line 108.)

`cmd/wfctl/plugin_conformance.go:327` (F1 fix):

```go
adapter, err := external.NewExternalPluginAdapter(name, pluginClient, nil)
```

(Conformance probe deliberately tests the plugin's own gRPC manifest in isolation. Disk fallback is the manager's concern, not the conformance verifier's. Passing `nil` preserves the conformance contract: a plugin that doesn't implement gRPC `GetManifest` fails conformance, which is the intent.)

**Step 5: Update existing test call sites** in `plugin/external/adapter_test.go` (every existing `NewExternalPluginAdapter("foo", &PluginClient{...})` call). Pass `nil` as 3rd arg — they exercise the no-disk path:

```bash
# Verify count
grep -n "NewExternalPluginAdapter(" plugin/external/adapter_test.go
```

Update each (add `, nil` before closing paren).

**Step 6: Run tests + build to verify pass**

Run: `go test ./plugin/external/ -run TestNewExternalPluginAdapter -v`
Expected: PASS. Specifically:
- `TestNewExternalPluginAdapterDiskManifestFallback` returns `Version() = "1.0.11"`.
- `TestNewExternalPluginAdapterDiskManifestNilStillWorks` returns `Name() = "legacy-plugin"` + `Version() = ""`.
- `TestNewExternalPluginAdapterPrefersGRPCWhenVersionPresent` returns `Version() = "1.0.0"`.

Run: `go build ./...`
Expected: exits 0. **Verify `cmd/wfctl` builds** specifically: `go build ./cmd/wfctl` — must exit 0. If this fails, the conformance caller was missed (F1 regression).

**Step 7: Rollback note**

Rollback: `git revert <commit>` — restores 2-arg signature; manager.go reverts to old call; tests revert to old expectations. Plugins that depended on disk fallback regress to PR #627 tolerance (empty manifest synthesized from name only).

**Step 8: Commit**

```bash
git add plugin/external/adapter.go plugin/external/adapter_test.go plugin/external/manager.go cmd/wfctl/plugin_conformance.go
git commit -m "fix(plugin/external): thread disk manifest into adapter as gRPC GetManifest fallback"
```

---

## Task 2: Regression test — `EngineManifest()` validates after Task 1 overlay

**Change class:** Internal logic refactor (test-only).

**Files:**
- Test: `plugin/external/adapter_test.go` (add `TestEngineManifestValidatesAfterDiskOverlay`)

**Background (F2 absorption):** R1 review correctly flagged that a second per-field merge in `EngineManifest()` introduces precedence ambiguity vs Task 1's wholesale-replace overlay. **Decision: single source of truth = Task 1's constructor overlay.** `EngineManifest()` reads `a.manifest` (already overlaid). This task is regression coverage only — locks in the contract that the overlay flows through to `Validate()`.

**Step 1: Write the test**

```go
func TestEngineManifestValidatesAfterDiskOverlay(t *testing.T) {
    disk := &plugin.PluginManifest{
        Name: "iac-plugin", Version: "1.0.11", Author: "GoCodeAlone", Description: "DO IaC",
    }
    a, err := NewExternalPluginAdapter("iac-plugin", &PluginClient{client: &adapterTestPluginServiceClient{
        // gRPC returns Unimplemented (strict-cutover IaC plugin path).
        manifestErr: status.Error(codes.Unimplemented, "GetManifest not implemented"),
    }}, disk)
    if err != nil {
        t.Fatalf("NewExternalPluginAdapter: %v", err)
    }
    em := a.EngineManifest()
    if em.Version != "1.0.11" {
        t.Fatalf("EngineManifest().Version = %q, want 1.0.11", em.Version)
    }
    if err := em.Validate(); err != nil {
        t.Fatalf("EngineManifest().Validate(): %v", err)
    }
}
```

**Step 2: Run test to verify pass**

Run: `go test ./plugin/external/ -run TestEngineManifestValidatesAfterDiskOverlay -v`
Expected: PASS — Task 1's constructor overlay populated `a.manifest` from disk; `EngineManifest()` reads it; `Validate()` returns nil.

**Step 3: Add precedence-rule comment to adapter.go**

In `plugin/external/adapter.go` near the constructor's overlay branch, add a comment:

```go
// Precedence rule (load-bearing): gRPC GetManifest is authoritative when it
// returns a non-empty Version. Disk-manifest fallback fires only when gRPC
// returns Unimplemented (strict-cutover IaC plugins) OR returns an empty
// Version. EngineManifest() reads a.manifest directly — no second-layer
// overlay, to avoid precedence ambiguity (see workflow ADR-0031 + plan F2).
```

**Step 4: Commit**

```bash
git add plugin/external/adapter.go plugin/external/adapter_test.go
git commit -m "test(plugin/external): regression — EngineManifest validates after disk overlay"
```

Rollback: revert removes test + comment; no functional impact (Task 1's overlay carries the change).

---

## Task 3: Engine `_`-prefix strip in `createTypedConfigRequest` (Bug 2)

**Change class:** Internal logic refactor; runtime-affecting (STRICT_PROTO config decode path).

**Files:**
- Modify: `plugin/external/adapter.go:251-285` (`createTypedConfigRequest`)
- Add: `plugin/external/convert.go` — `stripInternalKeys(map[string]any) map[string]any`
- Test: `plugin/external/convert_test.go` (`TestStripInternalKeys`)
- Test: `plugin/external/adapter_test.go` (`TestCreateTypedConfigRequestStripsInternalKeys`)

**Step 1: Write failing test for `stripInternalKeys`**

In `plugin/external/convert_test.go`:

```go
func TestStripInternalKeys(t *testing.T) {
    tests := []struct {
        name string
        in   map[string]any
        want map[string]any
    }{
        {name: "nil input", in: nil, want: nil},
        {name: "no underscore keys", in: map[string]any{"a": 1, "b": "x"}, want: map[string]any{"a": 1, "b": "x"}},
        {name: "strips _config_dir", in: map[string]any{"_config_dir": "/etc", "name": "x"}, want: map[string]any{"name": "x"}},
        {name: "strips multiple", in: map[string]any{"_a": 1, "_b": 2, "c": 3}, want: map[string]any{"c": 3}},
        {name: "all stripped", in: map[string]any{"_a": 1, "_b": 2}, want: map[string]any{}},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := stripInternalKeys(tc.in)
            if !reflect.DeepEqual(got, tc.want) {
                t.Fatalf("stripInternalKeys(%v) = %v, want %v", tc.in, got, tc.want)
            }
        })
    }
}

func TestStripInternalKeysDoesNotMutateInput(t *testing.T) {
    in := map[string]any{"_config_dir": "/etc", "name": "x"}
    _ = stripInternalKeys(in)
    if _, ok := in["_config_dir"]; !ok {
        t.Fatalf("stripInternalKeys mutated input — _config_dir removed from original")
    }
}
```

**Step 2: Run test to verify fail**

Run: `go test ./plugin/external/ -run TestStripInternalKeys -v`
Expected: FAIL — compile error, `stripInternalKeys` undefined.

**Step 3: Implement `stripInternalKeys`** in `plugin/external/convert.go` (after `mapToStruct`):

```go
// stripInternalKeys returns a fresh copy of m with all keys having the "_"
// prefix removed. The engine injects internal keys (e.g., "_config_dir") into
// every module config to support legacy modules that resolve filesystem-
// relative paths. STRICT_PROTO modules declare their schema explicitly via
// protobuf and reject unknown fields at protojson decode time — so engine
// internals must be stripped before mapToTypedAny is called.
//
// Returns nil if m is nil. Copy-on-clean: the caller's original map is not
// mutated; legacy *structpb.Struct paths continue to receive "_config_dir".
//
// The "_" prefix is the reserved namespace for engine internals; STRICT_PROTO
// module proto schemas must not declare fields with this prefix.
func stripInternalKeys(m map[string]any) map[string]any {
    if m == nil {
        return nil
    }
    cleaned := make(map[string]any, len(m))
    for k, v := range m {
        if strings.HasPrefix(k, "_") {
            continue
        }
        cleaned[k] = v
    }
    return cleaned
}
```

Add `"strings"` to imports of `plugin/external/convert.go`.

**Step 4: Run test to verify pass**

Run: `go test ./plugin/external/ -run TestStripInternalKeys -v`
Expected: PASS for all subtests including `TestStripInternalKeysDoesNotMutateInput`.

**Step 5: Wire into `createTypedConfigRequest`**

Modify `plugin/external/adapter.go:251-285`:

```go
func createTypedConfigRequest(descriptor *pb.ContractDescriptor, cfg map[string]any, resolver protoregistry.MessageTypeResolver) (*structpb.Struct, *anypb.Any, error) {
    if descriptor == nil || descriptor.Mode == pb.ContractMode_CONTRACT_MODE_UNSPECIFIED {
        s, err := mapToStruct(cfg)
        if err != nil {
            return nil, nil, fmt.Errorf("encode config as Struct: %w", err)
        }
        return s, nil, nil
    }
    if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT {
        s, err := mapToStruct(cfg)
        if err != nil {
            return nil, nil, fmt.Errorf("encode LEGACY_STRUCT config as Struct: %w", err)
        }
        return s, nil, nil
    }
    // Strip engine-internal "_"-prefix keys before proto decode. STRICT_PROTO
    // and PROTO_WITH_LEGACY_STRUCT modules use protojson with DiscardUnknown
    // = false (convert.go:62), which rejects engine internals like
    // "_config_dir" as unknown fields. Strip is copy-on-clean — the caller's
    // original cfg map retains all keys for the legacy *structpb.Struct
    // path below.
    cleaned := stripInternalKeys(cfg)
    typed, err := mapToTypedAny(descriptor.ConfigMessage, cleaned, resolver)
    if err != nil {
        if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
            return nil, nil, fmt.Errorf("STRICT_PROTO contract for config message %q cannot use legacy Struct fallback: %w", descriptor.ConfigMessage, err)
        }
        s, sErr := mapToStruct(cfg)
        if sErr != nil {
            return nil, nil, fmt.Errorf("encode config as Struct after typed fallback: %w", sErr)
        }
        return s, nil, nil
    }
    if descriptor.Mode == pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
        return nil, typed, nil
    }
    s, err := mapToStruct(cfg)
    if err != nil {
        return nil, nil, fmt.Errorf("encode PROTO_WITH_LEGACY_STRUCT config as Struct: %w", err)
    }
    return s, typed, nil
}
```

Note: the legacy `*structpb.Struct` path (PROTO_WITH_LEGACY_STRUCT branch) keeps `cfg` (with `_config_dir`) — only `cleaned` feeds `mapToTypedAny`.

**Step 6: Integration tests — STRICT_PROTO `_config_dir` strip (module AND step paths)**

`createTypedConfigRequest` is called from BOTH `CreateModule` (line ~411) AND `CreateStep` (line ~452) factory paths in `adapter.go`. F5 absorption: both paths must be covered, because `engine.go:495-499` injects `_config_dir` for modules and `engine.go:1104-1105` injects it for steps. The strip applies symmetrically because both paths funnel through `createTypedConfigRequest`, but the test must prove it.

In `plugin/external/adapter_test.go`:

```go
// Module path coverage.
func TestCreateTypedConfigRequestStripsInternalKeysForStrictProtoModule(t *testing.T) {
    descriptor := &pb.ContractDescriptor{
        Kind:          pb.ContractKind_CONTRACT_KIND_MODULE,
        Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
        ConfigMessage: "<existing-registered-test-message>", // see adapter_test.go for the typed test fixture
    }
    cfg := map[string]any{
        "_config_dir": "/etc/wf",
        "name":        "test",
    }
    _, typed, err := createTypedConfigRequest(descriptor, cfg, testTypeResolver())
    if err != nil {
        t.Fatalf("createTypedConfigRequest (module) with _config_dir: %v", err)
    }
    if typed == nil {
        t.Fatalf("expected typed *anypb.Any; got nil")
    }
    if _, ok := cfg["_config_dir"]; !ok {
        t.Fatalf("cfg mutated — _config_dir removed from input map")
    }
}

// Step path coverage (F5 — symmetric with module path).
func TestCreateTypedConfigRequestStripsInternalKeysForStrictProtoStep(t *testing.T) {
    descriptor := &pb.ContractDescriptor{
        Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
        Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
        ConfigMessage: "<existing-registered-test-message>",
    }
    cfg := map[string]any{
        "_config_dir": "/etc/wf",
        "operation":   "do-thing",
    }
    _, typed, err := createTypedConfigRequest(descriptor, cfg, testTypeResolver())
    if err != nil {
        t.Fatalf("createTypedConfigRequest (step) with _config_dir: %v", err)
    }
    if typed == nil {
        t.Fatalf("expected typed *anypb.Any; got nil")
    }
}

// Legacy-struct payload retains _config_dir (engine consumes it for relative paths).
func TestCreateTypedConfigRequestRetainsInternalKeysInLegacyStruct(t *testing.T) {
    descriptor := &pb.ContractDescriptor{
        Kind: pb.ContractKind_CONTRACT_KIND_MODULE,
        Mode: pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT,
    }
    cfg := map[string]any{"_config_dir": "/etc/wf", "name": "test"}
    legacy, typed, err := createTypedConfigRequest(descriptor, cfg, nil)
    if err != nil {
        t.Fatalf("createTypedConfigRequest (legacy): %v", err)
    }
    if typed != nil {
        t.Fatalf("expected nil typed for LEGACY_STRUCT mode")
    }
    if legacy == nil || legacy.Fields["_config_dir"] == nil {
        t.Fatalf("legacy *structpb.Struct must retain _config_dir for legacy modules")
    }
}
```

If no suitable STRICT_PROTO test message exists in `plugin/external/proto/` for direct test use, define one in `plugin/external/testdata/` or reuse the fixture from `TestCreateTypedConfigRequest_StrictProto` (locate via `grep -n "STRICT_PROTO" plugin/external/adapter_test.go`).

**Step 7: Run tests to verify pass**

Run: `go test ./plugin/external/ -run "TestStripInternalKeys|TestCreateTypedConfigRequest" -v`
Expected: all PASS.

**Step 8: Commit**

```bash
git add plugin/external/convert.go plugin/external/convert_test.go plugin/external/adapter.go plugin/external/adapter_test.go
git commit -m "fix(plugin/external): strip _-prefix keys before STRICT_PROTO encode"
```

Rollback: `git revert <commit>` — `createTypedConfigRequest` reverts to passing raw `cfg` to `mapToTypedAny`; STRICT_PROTO modules re-fail with `unknown field "_config_dir"`.

---

## Task 4: SDK `EmbedManifest` helper (forward-looking Bug 1 fix)

**Change class:** Plugin / extension (SDK). Verification: unit tests + integration test in Task 8.

**Files:**
- Add: `plugin/external/sdk/manifest.go`
- Add: `plugin/external/sdk/manifest_test.go`

**Step 1: Write failing test** in `plugin/external/sdk/manifest_test.go`:

```go
package sdk

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestEmbedManifestHappyPath(t *testing.T) {
    src := []byte(`{
        "name": "test-plugin",
        "version": "0.2.0",
        "author": "GoCodeAlone",
        "description": "test plugin",
        "configMutable": true,
        "sampleCategory": "iac"
    }`)
    m, err := EmbedManifest(src)
    if err != nil {
        t.Fatalf("EmbedManifest: %v", err)
    }
    if m.Name != "test-plugin" {
        t.Fatalf("Name = %q, want test-plugin", m.Name)
    }
    if m.Version != "0.2.0" {
        t.Fatalf("Version = %q, want 0.2.0", m.Version)
    }
    if !m.ConfigMutable {
        t.Fatalf("ConfigMutable = false, want true")
    }
    if m.SampleCategory != "iac" {
        t.Fatalf("SampleCategory = %q, want iac", m.SampleCategory)
    }
}

func TestEmbedManifestRejectsEmpty(t *testing.T) {
    _, err := EmbedManifest(nil)
    if err == nil {
        t.Fatalf("EmbedManifest(nil): want error, got nil")
    }
    _, err = EmbedManifest([]byte{})
    if err == nil {
        t.Fatalf("EmbedManifest([]byte{}): want error, got nil")
    }
}

func TestEmbedManifestRejectsMalformedJSON(t *testing.T) {
    _, err := EmbedManifest([]byte(`{not json`))
    if err == nil {
        t.Fatalf("EmbedManifest(malformed): want error, got nil")
    }
    if !strings.Contains(err.Error(), "parse embedded plugin.json") {
        t.Fatalf("error message = %q, want containing 'parse embedded plugin.json'", err.Error())
    }
}

func TestEmbedManifestRejectsMissingName(t *testing.T) {
    _, err := EmbedManifest([]byte(`{"version": "1.0.0", "author": "x", "description": "x"}`))
    if err == nil {
        t.Fatalf("EmbedManifest without name: want error, got nil")
    }
    if !strings.Contains(err.Error(), "validate") {
        t.Fatalf("error message = %q, want containing 'validate'", err.Error())
    }
}

func TestEmbedManifestRejectsMissingVersion(t *testing.T) {
    _, err := EmbedManifest([]byte(`{"name": "x", "author": "x", "description": "x"}`))
    if err == nil {
        t.Fatalf("EmbedManifest without version: want error, got nil")
    }
}

// F4 — Validate() also requires Author + Description. EmbedManifest must
// surface these as actionable errors, not silent acceptance.
func TestEmbedManifestRejectsMissingAuthor(t *testing.T) {
    _, err := EmbedManifest([]byte(`{"name": "x", "version": "1.0.0", "description": "x"}`))
    if err == nil {
        t.Fatalf("EmbedManifest without author: want error, got nil")
    }
}

func TestEmbedManifestRejectsMissingDescription(t *testing.T) {
    _, err := EmbedManifest([]byte(`{"name": "x", "version": "1.0.0", "author": "x"}`))
    if err == nil {
        t.Fatalf("EmbedManifest without description: want error, got nil")
    }
}

func TestMustEmbedManifestPanicsOnError(t *testing.T) {
    defer func() {
        if r := recover(); r == nil {
            t.Fatalf("MustEmbedManifest(malformed): want panic, got none")
        }
    }()
    _ = MustEmbedManifest([]byte(`{bad`))
}
```

**Step 2: Run test to verify fail**

Run: `go test ./plugin/external/sdk/ -run TestEmbedManifest -v`
Expected: FAIL — `EmbedManifest` undefined.

**Step 3: Implement** in `plugin/external/sdk/manifest.go`:

```go
package sdk

import (
    "encoding/json"
    "fmt"

    pluginpkg "github.com/GoCodeAlone/workflow/plugin"
)

// EmbedManifest parses plugin.json content (typically loaded via go:embed) into
// the canonical *plugin.PluginManifest type and runs the canonical Validate().
//
// Validate() requires ALL of: Name, Version, Author, Description (verified at
// plugin/manifest.go:183-201). A plugin.json missing any of these is rejected.
// This matches the same contract enforced by the engine's manager.go on disk
// load — there is no "minimal" path. If you cannot supply Author or
// Description at build time, the plugin should not ship a release.
//
// Plugin authors write:
//
//   //go:embed plugin.json
//   var manifestJSON []byte
//   var manifest = sdk.MustEmbedManifest(manifestJSON)
//
// The returned manifest is passed into sdk.Serve via WithManifestProvider, or
// into sdk.IaCServeOptions.ManifestProvider for ServeIaCPlugin. The SDK wires
// it into the appropriate GetManifest gRPC handler so the workflow engine sees
// a fully-populated manifest at plugin registration time.
//
// Parses via the canonical *plugin.PluginManifest (camelCase JSON tags matching
// the plugin.json authoring convention), NOT directly into *pb.Manifest (which
// has snake_case proto JSON tags and would silently drop configMutable etc.).
//
// For production code paths that need to recover from a missing/malformed
// plugin.json (e.g., plugins that ship with multiple manifest candidates),
// prefer EmbedManifest with explicit error handling over MustEmbedManifest.
// MustEmbedManifest panics at process startup, which surfaces misconfiguration
// loudly but is unrecoverable.
func EmbedManifest(content []byte) (*pluginpkg.PluginManifest, error) {
    if len(content) == 0 {
        return nil, fmt.Errorf("parse embedded plugin.json: empty content")
    }
    var m pluginpkg.PluginManifest
    if err := json.Unmarshal(content, &m); err != nil {
        return nil, fmt.Errorf("parse embedded plugin.json: %w", err)
    }
    if err := m.Validate(); err != nil {
        return nil, fmt.Errorf("validate embedded plugin.json: %w", err)
    }
    return &m, nil
}

// MustEmbedManifest panics on parse or validation error. Intended for
// package-level var initialization in plugin main packages — failure indicates
// a build-time misconfiguration that must be fixed before the binary ships.
//
// WARNING: panic semantics make this a process-startup canary. Plugin
// authors who need graceful degradation (e.g., to recover from a
// missing/malformed plugin.json in tooling-only code paths) should use
// EmbedManifest with explicit error handling instead.
func MustEmbedManifest(content []byte) *pluginpkg.PluginManifest {
    p, err := EmbedManifest(content)
    if err != nil {
        panic(err)
    }
    return p
}
```

**Step 4: Run tests to verify pass**

Run: `go test ./plugin/external/sdk/ -run "TestEmbedManifest|TestMustEmbedManifest" -v`
Expected: all PASS.

**Step 5: Commit**

```bash
git add plugin/external/sdk/manifest.go plugin/external/sdk/manifest_test.go
git commit -m "feat(sdk): add EmbedManifest helper for compile-time plugin.json embedding"
```

Rollback: revert removes helper; no callers depend on it (Tasks 5+6 add the wiring).

---

## Task 5: Wire `EmbedManifest` into `sdk.Serve` + `sdk.ServePluginFull` via `WithManifestProvider`

**Change class:** Plugin / extension (SDK). Verification: unit tests.

**Files:**
- Modify: `plugin/external/sdk/grpc_server.go` (add `diskManifest *pluginpkg.PluginManifest` field; modify `GetManifest`)
- Modify: `plugin/external/sdk/serve.go` (add `WithManifestProvider` functional option; modify `Serve` to accept variadic `ServeOption`)
- Modify: `plugin/external/sdk/serve_full.go` (thread option through to `Serve`)
- Test: `plugin/external/sdk/grpc_server_test.go` (add `TestGetManifestPrefersDiskManifest`)

**Step 1: Write failing test** in `plugin/external/sdk/grpc_server_test.go`:

```go
func TestGetManifestPrefersDiskManifest(t *testing.T) {
    disk := &pluginpkg.PluginManifest{
        Name:           "embedded-plugin",
        Version:        "1.2.3",
        Author:         "GoCodeAlone",
        Description:    "embedded test",
        ConfigMutable:  true,
        SampleCategory: "iac",
    }
    s := newGRPCServer(&stubProvider{manifest: PluginManifest{Name: "fallback", Version: ""}})
    s.diskManifest = disk
    got, err := s.GetManifest(context.Background(), &emptypb.Empty{})
    if err != nil {
        t.Fatalf("GetManifest: %v", err)
    }
    if got.Version != "1.2.3" {
        t.Fatalf("Version = %q, want 1.2.3 (disk override)", got.Version)
    }
    if got.SampleCategory != "iac" {
        t.Fatalf("SampleCategory = %q, want iac", got.SampleCategory)
    }
}

func TestGetManifestFallsBackToProviderWhenNoDisk(t *testing.T) {
    s := newGRPCServer(&stubProvider{manifest: PluginManifest{Name: "p", Version: "0.1.0"}})
    got, err := s.GetManifest(context.Background(), &emptypb.Empty{})
    if err != nil {
        t.Fatalf("GetManifest: %v", err)
    }
    if got.Version != "0.1.0" {
        t.Fatalf("Version = %q, want 0.1.0 (provider fallback)", got.Version)
    }
}
```

(If `stubProvider` doesn't exist in the test file, add it as a local minimal `PluginProvider`.)

**Step 2: Run test to verify fail**

Run: `go test ./plugin/external/sdk/ -run TestGetManifest -v`
Expected: FAIL — `diskManifest` field undefined on `grpcServer`.

**Step 3: Add field + modify `GetManifest`**

In `plugin/external/sdk/grpc_server.go` (after `broker *goplugin.GRPCBroker` field at line 33):

```go
    // diskManifest, when non-nil, takes precedence over provider.Manifest()
    // in GetManifest. Set via sdk.WithManifestProvider — lets plugins
    // compile-time embed plugin.json without re-declaring fields in the
    // PluginProvider implementation. Per workflow ADR-0031.
    diskManifest *pluginpkg.PluginManifest
```

Add import:

```go
    pluginpkg "github.com/GoCodeAlone/workflow/plugin"
```

Modify `GetManifest` at line 135:

```go
func (s *grpcServer) GetManifest(_ context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
    if s.diskManifest != nil {
        return &pb.Manifest{
            Name:           s.diskManifest.Name,
            Version:        s.diskManifest.Version,
            Author:         s.diskManifest.Author,
            Description:    s.diskManifest.Description,
            ConfigMutable:  s.diskManifest.ConfigMutable,
            SampleCategory: s.diskManifest.SampleCategory,
        }, nil
    }
    m := s.provider.Manifest()
    return &pb.Manifest{
        Name:           m.Name,
        Version:        m.Version,
        Author:         m.Author,
        Description:    m.Description,
        ConfigMutable:  m.ConfigMutable,
        SampleCategory: m.SampleCategory,
    }, nil
}
```

**Step 4: Add functional option in `plugin/external/sdk/serve.go`**

Add at top of file (after imports):

```go
// ServeOption configures Serve and ServePluginFull.
type ServeOption func(*grpcServer)

// WithManifestProvider wires a canonical *plugin.PluginManifest (typically
// loaded via sdk.EmbedManifest) into the gRPC GetManifest handler. When set,
// the disk-embedded manifest takes precedence over the provider's Manifest()
// method.
//
// Recommended pattern:
//
//   //go:embed plugin.json
//   var manifestJSON []byte
//   var manifest = sdk.MustEmbedManifest(manifestJSON)
//
//   func main() {
//       sdk.Serve(&myProvider{}, sdk.WithManifestProvider(manifest))
//   }
func WithManifestProvider(m *pluginpkg.PluginManifest) ServeOption {
    return func(s *grpcServer) {
        s.diskManifest = m
    }
}
```

Modify `Serve` signature (line 26):

```go
func Serve(provider PluginProvider, opts ...ServeOption) {
    if up, ok := provider.(UIProvider); ok {
        writeUIManifestIfAbsent(up.UIManifest())
    }
    server := newGRPCServer(provider)
    for _, opt := range opts {
        opt(server)
    }
    goplugin.Serve(&goplugin.ServeConfig{
        HandshakeConfig: ext.Handshake,
        GRPCServer:      goplugin.DefaultGRPCServer,
        Plugins: goplugin.PluginSet{
            "plugin": &servePlugin{server: server},
        },
    })
}
```

Add import for `pluginpkg "github.com/GoCodeAlone/workflow/plugin"` in `serve.go`.

**Step 5: Modify `ServePluginFull` signature in `plugin/external/sdk/serve_full.go`** to accept options:

```go
func ServePluginFull(p PluginProvider, cli CLIProvider, hooks HookHandler, opts ...ServeOption) {
    code := DispatchArgs(os.Args, p, cli, hooks, os.Stdin, os.Stdout)
    if code < 0 {
        Serve(p, opts...)
        return
    }
    os.Exit(code)
}
```

(Variadic — existing callers `ServePluginFull(p, cli, hooks)` remain valid; no existing-call-site changes.)

**Step 6: Run tests to verify pass**

Run: `go test ./plugin/external/sdk/ -run "TestGetManifest|TestEmbedManifest" -v`
Expected: all PASS.

Run: `go build ./...`
Expected: exits 0 (variadic preserves existing callers).

**Step 7: Commit**

```bash
git add plugin/external/sdk/grpc_server.go plugin/external/sdk/grpc_server_test.go plugin/external/sdk/serve.go plugin/external/sdk/serve_full.go
git commit -m "feat(sdk): WithManifestProvider option wires disk manifest into GetManifest"
```

Rollback: `git revert <commit>` — `Serve`/`ServePluginFull` revert to non-variadic; field removed.

---

## Task 6: `IaCServeOptions.ManifestProvider` + bridge `GetManifest` override

**Change class:** Plugin / extension (SDK). Verification: unit tests + e2e in Task 8.

**Files:**
- Modify: `plugin/external/sdk/iacserver.go` (add `ManifestProvider` field to `IaCServeOptions`; modify `iacPluginServiceBridge` to consume it; add `GetManifest` override)
- Modify: `plugin/external/sdk/iacserver.go` (`RegisterAllIaCProviderServices` signature unchanged but bridge construction must thread provider; refactor: bridge construction needs the provider)
- Test: `plugin/external/sdk/iacserver_test.go` or new (`TestIaCBridgeGetManifestUsesProvider`)

**Step 1: Write failing test**

```go
func TestIaCBridgeGetManifestUsesProvider(t *testing.T) {
    disk := &pluginpkg.PluginManifest{Name: "do", Version: "1.0.12", Description: "DO IaC"}
    bridge := &iacPluginServiceBridge{
        grpcSrv:      grpc.NewServer(),
        diskManifest: disk,
    }
    got, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
    if err != nil {
        t.Fatalf("GetManifest: %v", err)
    }
    if got.Version != "1.0.12" {
        t.Fatalf("Version = %q, want 1.0.12", got.Version)
    }
}

func TestIaCBridgeGetManifestUnimplementedWhenNoProvider(t *testing.T) {
    bridge := &iacPluginServiceBridge{grpcSrv: grpc.NewServer()}
    _, err := bridge.GetManifest(context.Background(), &emptypb.Empty{})
    if err == nil {
        t.Fatalf("GetManifest: want Unimplemented error, got nil")
    }
    if status.Code(err) != codes.Unimplemented {
        t.Fatalf("status.Code = %v, want Unimplemented", status.Code(err))
    }
}
```

**Step 2: Run test to verify fail**

Run: `go test ./plugin/external/sdk/ -run TestIaCBridge -v`
Expected: FAIL — `diskManifest` field undefined; `GetManifest` override not on bridge.

**Step 3: Modify `iacPluginServiceBridge`** in `iacserver.go`:

```go
type iacPluginServiceBridge struct {
    pb.UnimplementedPluginServiceServer
    grpcSrv      *grpc.Server
    diskManifest *pluginpkg.PluginManifest
}
```

Add import: `pluginpkg "github.com/GoCodeAlone/workflow/plugin"` plus `"google.golang.org/grpc/codes"` + `"google.golang.org/grpc/status"`.

Add method:

```go
// GetManifest returns the disk-embedded *plugin.PluginManifest as a
// *pb.Manifest when set via IaCServeOptions.ManifestProvider. Returns
// codes.Unimplemented when no manifest is wired, which triggers the engine's
// disk-fallback path (Task 1) — so even IaC plugins that haven't adopted
// sdk.EmbedManifest get clean registration via the engine's manager.go-loaded
// plugin.json.
func (b *iacPluginServiceBridge) GetManifest(_ context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
    if b.diskManifest == nil {
        return nil, status.Error(codes.Unimplemented, "manifest not embedded; engine falls back to disk plugin.json")
    }
    return &pb.Manifest{
        Name:           b.diskManifest.Name,
        Version:        b.diskManifest.Version,
        Author:         b.diskManifest.Author,
        Description:    b.diskManifest.Description,
        ConfigMutable:  b.diskManifest.ConfigMutable,
        SampleCategory: b.diskManifest.SampleCategory,
    }, nil
}
```

**Step 4: Add field to `IaCServeOptions`** (line 145):

```go
type IaCServeOptions struct {
    // PluginInfo overrides the default handshake/metadata.
    PluginInfo *PluginInfo

    // ManifestProvider, when set, is returned by the bridge's GetManifest
    // RPC. Typically populated via sdk.MustEmbedManifest from a go:embed-ed
    // plugin.json. When nil, GetManifest returns codes.Unimplemented and the
    // engine falls back to its manager.go-loaded plugin.json.
    ManifestProvider *pluginpkg.PluginManifest
}
```

**Step 5: Thread provider through plugin construction**

The bridge is constructed inside `RegisterAllIaCProviderServices` at line 113. The current signature is `RegisterAllIaCProviderServices(s *grpc.Server, provider any) error` and doesn't have access to `IaCServeOptions`. Two paths:

**Path A (preferred — minimal API change):** Add internal helper `registerAllIaCProviderServicesWithOpts` that takes the options. `RegisterAllIaCProviderServices` keeps its current 2-arg signature for backward compat with anyone calling it directly; internal IaC plugin path uses the new helper.

In `iacserver.go`:

```go
// registerAllIaCProviderServicesWithOpts is the internal variant of
// RegisterAllIaCProviderServices that threads IaCServeOptions through to the
// PluginService bridge. Public callers use RegisterAllIaCProviderServices.
func registerAllIaCProviderServicesWithOpts(s *grpc.Server, provider any, opts IaCServeOptions) error {
    if err := registerIaCServicesOnly(s, provider); err != nil {
        return err
    }
    if _, alreadyRegistered := s.GetServiceInfo()[pb.PluginService_ServiceDesc.ServiceName]; !alreadyRegistered {
        pb.RegisterPluginServiceServer(s, &iacPluginServiceBridge{
            grpcSrv:      s,
            diskManifest: opts.ManifestProvider,
        })
    }
    return nil
}

func registerIaCServicesOnly(s *grpc.Server, provider any) error {
    // ... extracted body of current RegisterAllIaCProviderServices minus the
    // bridge-registration step. (Move lines 67-99 here verbatim.)
}

// RegisterAllIaCProviderServices: public 2-arg form unchanged.
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error {
    return registerAllIaCProviderServicesWithOpts(s, provider, IaCServeOptions{})
}
```

**Step 6: Thread `IaCServeOptions` from `ServeIaCPlugin` → `iacGRPCPlugin`**

```go
type iacGRPCPlugin struct {
    provider any
    opts     IaCServeOptions
}

func (p *iacGRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
    return registerAllIaCProviderServicesWithOpts(s, p.provider, p.opts)
}

func ServeIaCPlugin(provider any, opts IaCServeOptions) {
    hs, err := resolveServeHandshake(opts)
    if err != nil {
        panic(fmt.Errorf("ServeIaCPlugin: %w", err))
    }
    goplugin.Serve(&goplugin.ServeConfig{
        HandshakeConfig: hs,
        Plugins: goplugin.PluginSet{
            "iac": &iacGRPCPlugin{provider: provider, opts: opts},
        },
        GRPCServer: goplugin.DefaultGRPCServer,
    })
}
```

**Step 7: Run tests to verify pass**

Run: `go test ./plugin/external/sdk/ -run "TestIaCBridge|TestServeIaC" -v`
Expected: PASS (the bridge test + any existing ServeIaCPlugin tests).

Run: `go build ./...`
Expected: exits 0.

**Step 8: Commit**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_test.go
git commit -m "feat(sdk): IaCServeOptions.ManifestProvider + bridge GetManifest override"
```

Rollback: `git revert <commit>` — bridge reverts to `UnimplementedPluginServiceServer`-only; `ManifestProvider` field removed.

---

## Task 7: Audit `_`-prefix collision in plugin proto schemas (A2 verification)

**Change class:** Documentation / audit. Verification: grep transcript captured.

**Files:**
- Add: `docs/audit/2026-05-12-underscore-prefix-audit.md`

**Step 1: Run audit grep across all wave plugin repos AND workflow's own proto/**

F3 absorption: the previous pattern produced false positives on snake_case fields. Tight pattern matches only proto3 field declarations whose **field name** starts with `_`. Audit scope expanded to include `workflow/plugin/external/proto/` (engine-defined messages that STRICT_PROTO plugins reference).

```bash
cd /Users/jon/workspace
PATTERN='^[[:space:]]+(repeated[[:space:]]+|optional[[:space:]]+)?[a-zA-Z_][a-zA-Z0-9_.]*[[:space:]]+_[a-z][a-zA-Z0-9_]*[[:space:]]*=[[:space:]]*[0-9]'
for repo in workflow workflow-plugin-digitalocean workflow-plugin-eventbus workflow-plugin-audit-chain workflow-plugin-payments workflow-plugin-auth workflow-plugin-twilio; do
    echo "=== $repo ==="
    if [ -d "$repo" ]; then
        # Find all .proto files (most plugins put them at proto/ or plugin/proto/; workflow has plugin/external/proto/)
        find "$repo" -name "*.proto" -not -path "*/node_modules/*" -not -path "*/_worktrees/*" -print0 \
            | xargs -0 grep -HnE "$PATTERN" 2>/dev/null || echo "(no underscore-prefix field declarations found)"
    else
        echo "(repo not present locally)"
    fi
done
```

Sanity-check pattern by running against a known-good corpus to confirm it does NOT match snake_case field declarations like `string my_field = 1;`. Expected: pattern only matches lines like `string _internal_field = 1;`.

**Step 2: Record findings**

In `/Users/jon/workspace/workflow/docs/audit/2026-05-12-underscore-prefix-audit.md`:

```markdown
# Underscore-prefix proto field audit

**Date:** 2026-05-12
**Context:** ADR-0031 establishes `_`-prefix as the engine-internals namespace. Verify no current plugin's proto schema declares a field with `_`-prefix that would be silently stripped by the new `stripInternalKeys` in `createTypedConfigRequest`.

## Audit transcript

<paste grep output from Step 1>

## Findings

<one line per repo: "clean" or "field <name> at <path:line> — needs migration before v0.51.3 ships">

## Verdict

<PASS = no collisions; FAIL = list collisions + propose mitigation (rename field in plugin's next release; or scope expansion to fix here)>
```

**Step 3: Verify PASS**

If audit returns clean across all 6 plugins → record verdict PASS → commit. If any plugin has an `_`-prefix field → STOP this task, escalate to user with proposed mitigation (most likely: ask plugin author to rename in their next release; v0.51.3 can ship in parallel since `stripInternalKeys` only affects new STRICT_PROTO modules, and plugin must currently work with non-strict mode).

**Step 4: Commit**

```bash
git add docs/audit/2026-05-12-underscore-prefix-audit.md
git commit -m "docs(audit): verify _-prefix proto fields absent across wave plugins"
```

Rollback: revert deletes audit doc; no code impact.

---

## Task 8: E2E integration test — disk-fallback registration without SDK helper

**Change class:** Plugin / extension. Verification: `go test ./...` exercises a test plugin binary that does NOT use `sdk.EmbedManifest` and validates the engine disk-fallback path end-to-end.

**Files:**
- Modify or add: `plugin/external/integration_test.go` (or whichever existing integration-test file builds an in-tree test plugin binary)
- The test plugin source already exists in `plugin/external/testdata/` or similar. Verify its current behavior and add an assertion for the disk-fallback path.

**Step 1: Locate existing integration scaffold**

```bash
find plugin/external -name "*integration*" -o -name "*e2e*" | head
ls plugin/external/testdata/ 2>/dev/null || true
```

If no in-tree integration scaffold exists, mark this task as DEFERRED and rely on Task 1+2 unit tests + BMW Task 8 smoke (the workspace-level integration gate) as e2e coverage.

**Step 2: If scaffold exists**, add a test that:

- Builds an in-tree test plugin binary that does NOT implement gRPC `GetManifest` (e.g., uses `sdk.ServeIaCPlugin` without `ManifestProvider` set).
- Loads it via `ExternalPluginManager` (which loads `plugin.json` from disk at `manager.go:108`).
- Asserts `adapter.EngineManifest().Version != ""` (disk-fallback populated it).
- Asserts `adapter.EngineManifest().Validate()` returns nil.

**Step 3: If scaffold does not exist**, write a smaller in-process simulation:

```go
func TestManagerLoadPluginThreadsDiskManifestToAdapter(t *testing.T) {
    // This is the in-process equivalent of an e2e test. Builds a temp
    // plugin dir with a valid plugin.json + a fake binary that gRPC-replies
    // Unimplemented for GetManifest. The manager loads it, the adapter
    // falls back to disk manifest.
    tmpDir := t.TempDir()
    pluginDir := filepath.Join(tmpDir, "test-plugin")
    require.NoError(t, os.MkdirAll(pluginDir, 0755))
    manifest := `{"name":"test-plugin","version":"9.9.9","author":"test","description":"disk fallback test"}`
    require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0644))
    // ... build fake binary or stub PluginClient ...
    // ... call NewExternalPluginAdapter("test-plugin", stubClientUnimplemented(), loadedManifest) ...
    // ... assert adapter.EngineManifest().Version == "9.9.9"
}
```

(Adjust to existing helpers in the file; the goal is to cover the manager → adapter → EngineManifest path without spinning up a subprocess.)

**Step 4: Run test to verify pass**

Run: `go test ./plugin/external/ -run TestManagerLoadPlugin -v`
Expected: PASS — adapter reports disk-fallback version.

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: all PASS (no regressions across the engine, plugins, or sdk packages).

Run: `go test -race ./...`
Expected: no data races.

Run: `golangci-lint run`
Expected: clean.

**Step 6: Commit**

```bash
git add plugin/external/integration_test.go
git commit -m "test(plugin/external): verify disk-manifest fallback end-to-end"
```

Rollback: revert removes test only; no functional impact.

---

## Release steps (post-merge, NOT part of this PR)

Once PR #N merges to `main`:

1. Tag: `git tag -a v0.51.3 -m "fix: strict-contracts ergonomics (engine disk-manifest fallback + SDK embed helper + _-prefix strip)"`
2. Push tag: `git push origin v0.51.3`
3. GoReleaser CI fires; verify release published.
4. BMW pin bump: change `workflow-server` pin in `buymywishlist/image-launch-ci.yml` + `deploy.yml` from `v0.51.2` → `v0.51.3`; commit + push BMW PR continuation.
5. Resume BMW Task 8 smoke.

Rollback (post-release): ship v0.51.4 with `git revert` of v0.51.3 commits. BMW pin reverts to v0.51.2.

---

## Verification summary per change class

| Task | Change class | Verification command | Expected output |
|---|---|---|---|
| 1 | Internal logic refactor (runtime-affecting plugin loading) | `go test ./plugin/external/ -run TestNewExternalPluginAdapter -v` | all PASS; `Version() = "1.0.11"` for disk-fallback test |
| 2 | Internal logic refactor (runtime-affecting plugin loading) | `go test ./plugin/external/ -run TestEngineManifest -v` | PASS; `EngineManifest().Validate()` returns nil |
| 3 | Internal logic refactor (runtime-affecting STRICT_PROTO decode) | `go test ./plugin/external/ -run "TestStripInternalKeys\|TestCreateTypedConfigRequest" -v` | all PASS; cfg map not mutated |
| 4 | Plugin / extension (SDK helper) | `go test ./plugin/external/sdk/ -run TestEmbedManifest -v` | all PASS including 5 error paths |
| 5 | Plugin / extension (SDK wiring) | `go test ./plugin/external/sdk/ -run TestGetManifest -v` | PASS for both disk-precedence + provider-fallback cases |
| 6 | Plugin / extension (SDK IaC wiring) | `go test ./plugin/external/sdk/ -run TestIaCBridge -v` | PASS — bridge returns embedded manifest when set, Unimplemented otherwise |
| 7 | Documentation / audit | `cat docs/audit/2026-05-12-underscore-prefix-audit.md` | verdict line says PASS |
| 8 | Plugin / extension (e2e) | `go test ./... && go test -race ./... && golangci-lint run` | all clean |

Full PR-level verification before merge: `go test ./... -race && golangci-lint run && go build ./...`. Plus: runtime-launch-validation by building `cmd/server` + smoke-loading the DO plugin (already shipped at v1.0.11) — must register cleanly via disk fallback without re-releasing DO.
