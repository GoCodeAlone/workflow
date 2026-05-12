# Workflow Strict-Contracts Ergonomics — Design

**Date:** 2026-05-12
**Author:** Claude (autonomous, per user mandate)
**Status:** Revised after R1 adversarial review (2 Critical + 3 Important + 2 Minor addressed) — primary Bug 1 fix moved to engine-side, SDK helper made forward-looking
**Driver:** BMW v0.51.2 smoke gate caught 3 upstream bugs blocking deploy. This design addresses bugs 1 + 2 (engine + SDK). Bug 3 (payments TypedModuleProvider without ContractProvider) ships as a separate plugin-side fix.

## Goal

Eliminate two strict-cutover ergonomics gaps in workflow v0.51.2 that block plugin registration when the plugin follows recommended patterns:

1. **Bug 1:** IaC plugins (served via specialized helpers without `GetManifest` impl) get a synthesized manifest with empty `Version`, which fails `Validate()` and prevents registration.
2. **Bug 2:** Engine-injected `_config_dir` key contaminates STRICT_PROTO module configs; `protojson.UnmarshalOptions{DiscardUnknown: false}` rejects it as an unknown field.

Both bugs surfaced in BMW PR Task 8 smoke (transcript at `buymywishlist/docs/audit/2026-05-12-smoke-transcript.txt`).

## Motivation

Per workspace mandate `feedback_force_strict_contracts_no_compat` (2026-05-09): strict-cutover is force-cutover with no compat layer. These two bugs are ergonomics gaps in the strict-cutover surface that block every IaC plugin (Bug 1) and every STRICT_PROTO module plugin (Bug 2). Fixing them in the engine + SDK ships ONE upstream change that unblocks the entire plugin ecosystem.

## Scope

**Bug 1 fix strategy revised after R1:** primary fix is **engine-side disk-manifest fallback** (no plugin change required to unblock BMW). SDK helper is a forward-looking opt-in that plugins can adopt over time for compile-time-embedded manifests. This split is per R1 reviewer's Option 3 — `manager.go:108` already loads + validates `plugin.json` via `pluginpkg.LoadManifest`; threading that into the adapter is a one-PR, zero-plugin-change fix.

**In scope (workflow repo):**

- **Bug 1 primary fix (engine-side):** `manager.go::LoadPlugin` already loads `*plugin.PluginManifest` via `pluginpkg.LoadManifest` (line ~108) before subprocess launch. Thread that manifest into `NewExternalPluginAdapter` as a fallback. When the gRPC `GetManifest` RPC returns an empty `Version` (Unimplemented tolerance fallback or otherwise), field-map the disk-loaded `plugin.PluginManifest` into the synthesized `pb.Manifest`. **This alone unblocks BMW** — no plugin-side change required.
- **Bug 1 secondary fix (SDK forward-looking):** new SDK helper `sdk.EmbedManifest([]byte) (*plugin.PluginManifest, error)` (parses via existing canonical type, NOT `*pb.Manifest` directly — per R1 Critical) + `sdk.WithManifestProvider(*plugin.PluginManifest)` option on each `Serve*` helper:
  - `sdk.Serve` + `sdk.ServePluginFull`: wires into `grpcServer.GetManifest` handler.
  - `sdk.ServeIaCPlugin`: wires into `IaCServeOptions.ManifestProvider` field which `iacPluginServiceBridge.GetManifest` consults (per R1 Critical — bridge owns GetManifest for IaC path, not grpcServer).
  - Existing plugins continue working; helper is opt-in.
- **Bug 2 fix (engine-side):** `createTypedConfigRequest` in `plugin/external/adapter.go` strips internal-only keys (any key starting with `_`) from `cfg` before `mapToTypedAny`. Documents the engine ↔ plugin convention.
- Workflow release v0.51.3 with all three changes (engine fallback, SDK helper, engine strip) + ADR.
- Test coverage:
  - Unit: disk-manifest fallback in adapter (Bug 1 primary).
  - Unit: `EmbedManifest` parses canonical `plugin.PluginManifest` correctly (validates name + version present).
  - Unit: `stripInternalKeys` (Bug 2).
  - Unit: `createTypedConfigRequest` with `_config_dir` in cfg + STRICT_PROTO ConfigMessage.
  - Integration: existing test plugin loads cleanly with engine fallback (no SDK helper adoption); separate integration test exercising the SDK helper path.

**Out of scope:**

- Adding `_config_dir` to any proto schema (we strip, not declare).
- Removing engine-side `_config_dir` injection (legacy modules still need it).
- Bug 3 (payments TypedModuleProvider+ContractProvider gap) — fixed in payments + audit-chain plugin repos separately.
- Per-plugin adoption of `sdk.EmbedManifest` — pluging cutover wave plugins continue working without it; can adopt over time.
- Removing the engine's tolerance for Unimplemented GetManifest (workflow PR #627) — kept as safety net for plugins that don't return a manifest at all; combined with disk-fallback, it becomes a defense-in-depth path.

## Architecture

Three surgical changes (revised after R1):

### Change 1A — Engine-side disk-manifest fallback (PRIMARY Bug 1 fix)

`manager.go::LoadPlugin` (line ~108) already calls:

```go
manifest, err := pluginpkg.LoadManifest(manifestPath)  // returns *plugin.PluginManifest
if err := manifest.Validate(); err != nil { ... }
```

After subprocess launch + adapter construction (currently at `manager.go` later in the function), thread `manifest` (the disk-loaded `*plugin.PluginManifest`) into the adapter:

```go
// Update NewExternalPluginAdapter signature to accept the disk manifest as fallback
func NewExternalPluginAdapter(name string, client *PluginClient, diskManifest *plugin.PluginManifest) (*ExternalPluginAdapter, error)
```

In `EngineManifest()` (currently `plugin/external/adapter.go:274`), when `a.manifest.Version` is empty (the synthesized-empty case from Unimplemented tolerance, or the empty-Manifest{Name:name} fallback), use the disk manifest fields. Map function:

```go
func manifestFromDisk(m *plugin.PluginManifest) *pb.Manifest {
    if m == nil { return nil }
    return &pb.Manifest{
        Name:        m.Name,
        Version:     m.Version,
        Author:      m.Author,
        Description: m.Description,
    }
}
```

Use this in both `NewExternalPluginAdapter` (when `client.GetManifest` returns Unimplemented or empty-version) AND in `EngineManifest()` (as a post-hoc fallback for older plugins that bypassed the constructor path).

**Effect:** every plugin that lands its `plugin.json` on disk via standard `wfctl plugin install` (all current and future plugins) gets a fully-populated manifest at registration time, with ZERO plugin-side code change. BMW deploy unblocks the moment workflow v0.51.3 ships.

### Change 1B — SDK manifest helper (FORWARD-LOOKING Bug 1 fix)

For plugins that want compile-time-embedded manifest (defense-in-depth + portability — no reliance on `wfctl plugin install` landing plugin.json adjacent to binary), add SDK helper. **Key correction per R1 Critical:** parse via the existing canonical `plugin.PluginManifest` type (which already has correct camelCase JSON tags matching `plugin.json` authoring convention), NOT `*pb.Manifest` directly (which has snake_case proto JSON tags).

Add to `plugin/external/sdk/manifest.go` (new file):

```go
package sdk

import (
    "encoding/json"
    "fmt"

    pluginpkg "github.com/GoCodeAlone/workflow/plugin"
)

// EmbedManifest parses plugin.json content (typically from //go:embed) into the
// canonical *plugin.PluginManifest type. Plugin authors write:
//
//   //go:embed plugin.json
//   var manifestJSON []byte
//   var manifest = sdk.MustEmbedManifest(manifestJSON)
//
// Then pass `manifest` via the appropriate option (sdk.WithManifestProvider for
// Serve / ServePluginFull, or IaCServeOptions.ManifestProvider for ServeIaCPlugin).
func EmbedManifest(content []byte) (*pluginpkg.PluginManifest, error) {
    var m pluginpkg.PluginManifest
    if err := json.Unmarshal(content, &m); err != nil {
        return nil, fmt.Errorf("parse embedded plugin.json: %w", err)
    }
    if err := m.Validate(); err != nil {
        return nil, fmt.Errorf("validate embedded plugin.json: %w", err)
    }
    return &m, nil
}

// MustEmbedManifest panics on parse error; intended for package-level var initialization.
func MustEmbedManifest(content []byte) *pluginpkg.PluginManifest {
    p, err := EmbedManifest(content)
    if err != nil {
        panic(err)
    }
    return p
}
```

**No new `ManifestProvider` interface** (per R1 Minor finding — YAGNI). The concrete `*plugin.PluginManifest` is the return type; SDK option helpers consume it directly.

#### Change 1B wiring per Serve helper (per R1 Critical):

- **`sdk.Serve` + `sdk.ServePluginFull`:** `grpcServer.GetManifest` is the handler. Add `*plugin.PluginManifest` field to grpcServer; if set + the auto-populated manifest from existing logic is empty, return mapped pb.Manifest from disk-manifest field.
- **`sdk.ServeIaCPlugin`:** the `iacPluginServiceBridge` struct (in `plugin/external/sdk/iacserver.go`) embeds `pb.UnimplementedPluginServiceServer` which returns `codes.Unimplemented` for `GetManifest`. **This is the critical wire-point per R1 — NOT grpcServer.** Add `ManifestProvider *plugin.PluginManifest` field to `IaCServeOptions` (the documented forward-extension point at `iacserver.go:145`). Update `iacPluginServiceBridge` to override `GetManifest`:

```go
func (b *iacPluginServiceBridge) GetManifest(ctx context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
    if b.diskManifest != nil {
        return manifestFromDisk(b.diskManifest), nil
    }
    return nil, status.Error(codes.Unimplemented, "manifest not embedded; engine falls back to disk plugin.json")
}
```

The `Unimplemented` return path triggers the engine's existing PR #627 tolerance + disk-fallback (Change 1A) — so even IaC plugins that don't adopt the embed helper get clean registration via the engine fallback.

### Change 2 — Engine strips internal keys before STRICT_PROTO encoding

In `plugin/external/adapter.go`, modify `createTypedConfigRequest` (line ~221) to strip `_`-prefix keys from `cfg` before calling `mapToTypedAny`:

```go
func createTypedConfigRequest(descriptor *pb.ContractDescriptor, cfg map[string]any, resolver protoregistry.MessageTypeResolver) (*structpb.Struct, *anypb.Any, error) {
    // ... existing code building legacy *structpb.Struct from cfg (unchanged) ...

    // Strip engine-internal keys (prefix "_") from STRICT_PROTO module configs.
    // Internal keys like "_config_dir" are injected by the engine for legacy
    // modules; STRICT_PROTO modules declare their schema explicitly and don't
    // accept engine internals. If a STRICT_PROTO module needs filesystem
    // context (e.g., relative path resolution), it must declare a field for
    // it in its proto schema.
    cleaned := stripInternalKeys(cfg)
    typed, err := mapToTypedAny(descriptor.ConfigMessage, cleaned, resolver)
    if err != nil {
        return nil, nil, err
    }
    return legacyStruct, typed, nil
}

func stripInternalKeys(cfg map[string]any) map[string]any {
    if cfg == nil {
        return nil
    }
    cleaned := make(map[string]any, len(cfg))
    for k, v := range cfg {
        if strings.HasPrefix(k, "_") {
            continue
        }
        cleaned[k] = v
    }
    return cleaned
}
```

**Clarification per R1 Important:** the strip produces a **fresh `cleaned` copy** — the engine's original `modCfg.Config` map is NOT mutated and retains `_config_dir` for the legacy `*structpb.Struct` path (which is what legacy modules consume). The strip is copy-on-clean, not in-place sanitization.

### Change 2 — Engine strips internal keys before STRICT_PROTO encoding

In `plugin/external/adapter.go`, modify `createTypedConfigRequest` (line ~221) to strip `_`-prefix keys from `cfg` before calling `mapToTypedAny`:

```go
func createTypedConfigRequest(descriptor *pb.ContractDescriptor, cfg map[string]any, resolver protoregistry.MessageTypeResolver) (*structpb.Struct, *anypb.Any, error) {
    // ... existing code building legacy *structpb.Struct from cfg (unchanged) ...

    // Strip engine-internal keys (prefix "_") from STRICT_PROTO module configs.
    // Internal keys like "_config_dir" are injected by the engine for legacy
    // modules; STRICT_PROTO modules declare their schema explicitly and don't
    // accept engine internals. If a STRICT_PROTO module needs filesystem
    // context (e.g., relative path resolution), it must declare a field for
    // it in its proto schema.
    cleaned := stripInternalKeys(cfg)
    typed, err := mapToTypedAny(descriptor.ConfigMessage, cleaned, resolver)
    if err != nil {
        return nil, nil, err
    }
    return legacyStruct, typed, nil
}

func stripInternalKeys(cfg map[string]any) map[string]any {
    if cfg == nil {
        return nil
    }
    cleaned := make(map[string]any, len(cfg))
    for k, v := range cfg {
        if strings.HasPrefix(k, "_") {
            continue
        }
        cleaned[k] = v
    }
    return cleaned
}
```

The legacy `*structpb.Struct` path remains unchanged (legacy modules still read `_config_dir`).

## Components

| Component | Responsibility |
|---|---|
| `plugin/external/manager.go::LoadPlugin` (modified) | Pass disk-loaded `*plugin.PluginManifest` to `NewExternalPluginAdapter` |
| `plugin/external/adapter.go::NewExternalPluginAdapter` (signature change) | Accept `*plugin.PluginManifest` disk-manifest fallback; use it when gRPC `GetManifest` returns empty version |
| `plugin/external/adapter.go::EngineManifest` (modified) | Field-map disk manifest fields when adapter.manifest fields are empty |
| `plugin/external/adapter.go::manifestFromDisk` (new helper) | Map `*plugin.PluginManifest` → `*pb.Manifest` |
| `plugin/external/sdk/manifest.go` (new) | `EmbedManifest(bytes) (*plugin.PluginManifest, error)` + `MustEmbedManifest(bytes) *plugin.PluginManifest` helpers (parse via canonical type, NOT *pb.Manifest) |
| `plugin/external/sdk/serve.go` (modified) | Accept disk manifest field on grpcServer; wire into `GetManifest` handler |
| `plugin/external/sdk/serve_full.go` (modified) | Same |
| `plugin/external/sdk/iacserver.go` (modified) | Add `ManifestProvider *plugin.PluginManifest` field to `IaCServeOptions`; `iacPluginServiceBridge.GetManifest` returns mapped manifest when set |
| `plugin/external/adapter.go:createTypedConfigRequest` (modified) | Strip `_`-prefix keys from cfg before `mapToTypedAny` |
| `plugin/external/convert.go` (or inline) | `stripInternalKeys` helper |
| Unit tests | `manifest_test.go` (parse + validate); `convert_test.go` (verify strip + STRICT_PROTO success); `adapter_test.go` (disk-manifest fallback when gRPC returns empty/Unimplemented) |
| Integration test | Existing plugin (no SDK helper adoption) registers cleanly with disk-manifest fallback; separate integration plugin exercises SDK helper path |
| ADR | `decisions/0031-strict-contracts-ergonomics.md` |

## Data Flow

**Manifest path (Bug 1 fix):**
```
Plugin binary startup
  → sdk.Serve(..., sdk.WithManifestProvider(manifest))
  → gRPC server registers GetManifest handler that calls provider.Manifest()
Engine LoadPlugin
  → NewExternalPluginAdapter → client.GetManifest(ctx) → adapter.manifest = populated pb.Manifest (Name + Version + Author + Description)
  → engine.LoadPlugin calls adapter.EngineManifest().Validate() → PASS (Version present)
  → plugin registered ✓
```

**STRICT_PROTO config path (Bug 2 fix):**
```
engine.go:499  modCfg.Config["_config_dir"] = e.configDir   (still happens, unchanged)
factory called → createTypedConfigRequest(descriptor, cfg, resolver)
  → stripInternalKeys(cfg) → cleaned (no _config_dir)
  → mapToTypedAny(descriptor.ConfigMessage, cleaned, resolver)
  → protojson.Unmarshal succeeds (cleaned has no unknown fields)
  → typed *anypb.Any returned
plugin gRPC server.CreateModule receives TypedConfig → unpacks ProviderConfig{...} with real fields → success
```

## Error Handling

- **Bug 1 fix:** if `EmbedManifest` is called with bad bytes (malformed JSON, empty name/version), returns error at startup. `MustEmbedManifest` panics — surfaces immediately, before any deploy reaches runtime. SDK option `WithManifestProvider` is optional — plugins that don't use it fall back to current behavior (PR #627 tolerance).
- **Bug 2 fix:** stripping is purely additive — removes `_`-prefix keys from a fresh map copy. Original `cfg` map is not mutated. If a STRICT_PROTO module DECLARED a field with `_`-prefix in its proto schema (unusual but technically valid protobuf), the strip would drop it. **Mitigation:** document that `_`-prefix is reserved for engine internals; proto schema authors should not declare `_`-prefix fields.

## Testing

| Layer | Method | Pass criteria |
|---|---|---|
| Unit: EmbedManifest happy path | parse valid plugin.json bytes | returns ManifestProvider; Manifest() returns expected name/version/author/description |
| Unit: EmbedManifest error paths | empty bytes, malformed JSON, missing name, missing version | returns error with descriptive message |
| Unit: stripInternalKeys | map containing `_config_dir` + real fields | returned map has only real fields; original map unchanged |
| Unit: createTypedConfigRequest with _-injected key | call with cfg containing `_config_dir` + STRICT_PROTO ConfigMessage that doesn't declare it | succeeds; typed *anypb.Any unpacks to correct message with `_config_dir` absent |
| Integration: plugin loads with embedded manifest | sample test plugin using `sdk.EmbedManifest` + STRICT_PROTO module | engine `LoadPlugin` succeeds; module instantiates; `_config_dir` not visible to plugin |
| Existing test suite | full workflow tests | no regressions |

## Assumptions (load-bearing)

1. **A1 — `plugin.json` is reliably present at plugin module root.** Plugin authors use `//go:embed plugin.json` in main package; goreleaser archives include `plugin.json`. All 6 current wave plugins meet this (verified per BMW Task 1 pre-flight + Task 7 archive verification).
2. **A2 — `_`-prefix is reserved for engine internals.** No plugin's proto schema declares an `_`-prefix field today (verified spot-check across DO, eventbus, payments, audit-chain). Documented as a forward-looking convention.
3. **A3 — Existing plugins that implement `GetManifest` via their own gRPC code continue to work.** The new SDK helper is opt-in; pre-existing behavior is preserved.
4. **A4 — The PR #627 tolerance fix remains as a safety net.** Plugins that adopt `sdk.EmbedManifest` don't hit it; plugins that don't can still register (with limited info). Combining the helper + tolerance prevents regressions during the gradual plugin migration.
5. **A5 — Workflow v0.51.3 ships these fixes.** No bundling with other workflow changes; minimal-scope point release. Branch off `origin/main`.
6. **A6 — Engine's `_config_dir` injection (engine.go:499) continues unchanged.** Legacy modules still receive it; STRICT_PROTO modules see it stripped.
7. **A7 — `pb.Manifest` proto schema unchanged.** Helper parses existing fields; no proto change needed.

## Rollback

This is a runtime-affecting change (engine semantics, plugin loading paths). Rollback paths:

1. **Pre-merge:** discard PR branch. Zero impact.
2. **Post-merge / pre-release:** `git revert` on workflow main; cancel v0.51.3 release.
3. **Post-release:** ship v0.51.4 with revert. Plugins pinned to v0.51.3 SDK would need to bump down OR continue using the helper (helper is additive; even if engine-side strip reverts, plugin still ships proper manifest). The engine `_config_dir` strip revert would re-break STRICT_PROTO modules — same blocker as current state.
4. **BMW pin behavior:** BMW pins workflow-server to v0.51.3 in image-launch-ci.yml + deploy.yml. A workflow v0.51.3 revert means BMW bumps back to v0.51.2 → smoke fails as today. Forward fix is the only path.

## Top 3 doubts (self-challenge + R1 absorption)

1. **Engine fallback fully unblocks BMW; SDK helper is forward-looking.** v0.51.3 ships both, but BMW only requires the engine-side fix. DO doesn't need v1.0.12 immediately — DO v1.0.11 will register cleanly via disk-manifest fallback. SDK helper adoption is a per-plugin choice over time. Resolves R1's "user-intent drift" finding.
2. **`_`-prefix convention is implicit today** (R1 finding A2). Audit step before merge: grep every plugin's proto schema for `_`-prefix fields. If any are found, surface as scope expansion before shipping the strip.
3. **Manifest version drift:** `plugin.json` `version` field is now load-bearing for engine registration. If a plugin author bumps the goreleaser tag but forgets to bump `plugin.json:version` (as happened with auth v0.2.0 → v0.2.1 in the wave), the registration succeeds but reports a stale version. Mitigation: same integration test pattern that auth uses (`PLUGIN_MANIFEST_EXPECT_VERSION`) is the recommended pattern for all plugins; document in pattern doc.

## Decisions recorded

ADR captures:
- SDK embed-vs-runtime-synthesis trade-off (chose embed for strict-cutover discipline)
- Engine strip-vs-schema-declaration trade-off (chose strip for surgical-fix scope)
- v0.51.3 as minimal-scope point release boundary
