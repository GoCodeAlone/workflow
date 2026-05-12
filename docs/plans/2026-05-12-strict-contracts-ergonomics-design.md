# Workflow Strict-Contracts Ergonomics — Design

**Date:** 2026-05-12
**Author:** Claude (autonomous, per user mandate)
**Status:** Draft — pending adversarial review
**Driver:** BMW v0.51.2 smoke gate caught 3 upstream bugs blocking deploy. This design addresses bugs 1 + 2 (engine + SDK). Bug 3 (payments TypedModuleProvider without ContractProvider) ships as a separate plugin-side fix.

## Goal

Eliminate two strict-cutover ergonomics gaps in workflow v0.51.2 that block plugin registration when the plugin follows recommended patterns:

1. **Bug 1:** IaC plugins (served via specialized helpers without `GetManifest` impl) get a synthesized manifest with empty `Version`, which fails `Validate()` and prevents registration.
2. **Bug 2:** Engine-injected `_config_dir` key contaminates STRICT_PROTO module configs; `protojson.UnmarshalOptions{DiscardUnknown: false}` rejects it as an unknown field.

Both bugs surfaced in BMW PR Task 8 smoke (transcript at `buymywishlist/docs/audit/2026-05-12-smoke-transcript.txt`).

## Motivation

Per workspace mandate `feedback_force_strict_contracts_no_compat` (2026-05-09): strict-cutover is force-cutover with no compat layer. These two bugs are ergonomics gaps in the strict-cutover surface that block every IaC plugin (Bug 1) and every STRICT_PROTO module plugin (Bug 2). Fixing them in the engine + SDK ships ONE upstream change that unblocks the entire plugin ecosystem.

## Scope

**In scope (workflow repo):**

- New SDK helper `sdk.EmbedManifest([]byte) ManifestProvider` that parses a `plugin.json` byte slice (typically from `//go:embed`).
- New SDK interface `ManifestProvider` exposing manifest via Go API.
- Update `sdk.Serve`, `sdk.ServePluginFull`, and `sdk.ServeIaCPlugin` (if/where exists) to accept a `ManifestProvider` option and use it to implement the gRPC `GetManifest` RPC automatically.
- Engine fix: `createTypedConfigRequest` in `plugin/external/adapter.go` strips internal-only keys (any key starting with `_`) from `cfg` before `mapToTypedAny`. Documents the engine ↔ plugin convention.
- Backward-compatibility: existing plugins that already populate `GetManifest` via their own implementation continue to work unchanged.
- Workflow release v0.51.3 with both fixes + ADR.
- Test coverage for both fixes (unit + integration where applicable).

**Out of scope:**

- Adding `_config_dir` to any proto schema (we strip, not declare).
- Removing engine-side `_config_dir` injection (legacy modules still need it).
- Bug 3 (payments TypedModuleProvider+ContractProvider gap) — fixed in payments + audit-chain plugin repos separately.
- Plugin-side adoption of `sdk.EmbedManifest` (separate per-plugin PRs after v0.51.3 ships).
- Removing the engine's tolerance for Unimplemented GetManifest (workflow PR #627) — kept as safety net; combined with the SDK helper, it becomes a rarely-taken path.

## Architecture

Two surgical changes:

### Change 1 — SDK manifest embedding helper

Add to `plugin/external/sdk/manifest.go` (new file):

```go
package sdk

import (
    "encoding/json"
    "fmt"

    pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ManifestProvider exposes a plugin's identity manifest for the engine's gRPC GetManifest RPC.
type ManifestProvider interface {
    Manifest() *pb.Manifest
}

// EmbedManifest parses plugin.json content (typically from //go:embed) into a ManifestProvider.
//
// Plugin authors write:
//
//   //go:embed plugin.json
//   var manifestJSON []byte
//   var manifest = sdk.MustEmbedManifest(manifestJSON)
//
// Then pass `manifest` to sdk.Serve / sdk.ServePluginFull / sdk.ServeIaCPlugin via
// the appropriate option helper (e.g., sdk.WithManifestProvider(manifest)).
func EmbedManifest(content []byte) (ManifestProvider, error) {
    var m pb.Manifest
    if err := json.Unmarshal(content, &m); err != nil {
        return nil, fmt.Errorf("parse embedded plugin.json: %w", err)
    }
    if m.Name == "" {
        return nil, fmt.Errorf("embedded plugin.json missing 'name'")
    }
    if m.Version == "" {
        return nil, fmt.Errorf("embedded plugin.json missing 'version'")
    }
    return &embeddedManifest{m: &m}, nil
}

// MustEmbedManifest panics on parse error; intended for package-level var initialization.
func MustEmbedManifest(content []byte) ManifestProvider {
    p, err := EmbedManifest(content)
    if err != nil {
        panic(err)
    }
    return p
}

type embeddedManifest struct{ m *pb.Manifest }
func (e *embeddedManifest) Manifest() *pb.Manifest { return e.m }
```

Update each `Serve*` helper to accept an optional `ManifestProvider`. Implementation route depends on existing helper shape:

- If helpers take a `*Options` struct: add `ManifestProvider ManifestProvider` field; in the gRPC server's `GetManifest` handler, return `options.ManifestProvider.Manifest()` when non-nil.
- If helpers take variadic options: add `WithManifestProvider(p ManifestProvider) Option` constructor.

The gRPC `GetManifest` handler logic becomes:

```go
func (s *grpcServer) GetManifest(ctx context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
    if s.manifestProvider != nil {
        return s.manifestProvider.Manifest(), nil
    }
    // Existing fallback: return Name-only manifest OR codes.Unimplemented
    return s.legacyManifestFallback()
}
```

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
| `plugin/external/sdk/manifest.go` (new) | `ManifestProvider` interface + `EmbedManifest()` + `MustEmbedManifest()` helpers |
| `plugin/external/sdk/serve.go` (modified) | Accept manifest provider; wire into gRPC `GetManifest` |
| `plugin/external/sdk/serve_full.go` (modified) | Same |
| `plugin/external/sdk/serve_iac.go` (modified or new — verify existence) | Same |
| `plugin/external/adapter.go:createTypedConfigRequest` (modified) | Strip `_`-prefix keys from cfg before `mapToTypedAny` |
| `plugin/external/convert.go` (or new file) | `stripInternalKeys` helper (or inline) |
| Unit tests | `manifest_test.go` (parse + reject empty fields); `convert_test.go` (verify strip + STRICT_PROTO success) |
| Integration test | Load a plugin that uses `sdk.EmbedManifest` + has STRICT_PROTO module config with engine `_config_dir` injection; assert plugin registers + module instantiates |
| ADR | `decisions/0022-strict-contracts-ergonomics.md` (number TBD per repo's actual ADR sequence) |

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

## Top 3 doubts (self-challenge)

1. **Plugin-side adoption is a separate workstream.** This PR ships the SDK helper + engine fix in workflow v0.51.3. Each plugin then needs a follow-up PR adopting `sdk.EmbedManifest`. For DO specifically (the plugin that surfaced Bug 1), DO v1.0.12 will adopt the helper after v0.51.3 ships. That's 2 PRs to fully unblock DO registration in BMW.
2. **`_`-prefix convention is implicit today.** Documenting it AND enforcing it (via the strip) is a forward-looking convention change. If any pre-existing plugin schema has `_`-prefix fields (unlikely but unverified across all workflow's own plugins), the strip would drop them silently. Mitigation: audit before merge.
3. **Manifest version drift:** `plugin.json` `version` field is now load-bearing for engine registration. If a plugin author bumps the goreleaser tag but forgets to bump `plugin.json:version` (as happened with auth v0.2.0 → v0.2.1 in the wave), the registration succeeds but reports a stale version. Mitigation: same integration test that auth uses (`PLUGIN_MANIFEST_EXPECT_VERSION`) becomes a recommended pattern for all plugins; document in pattern doc.

## Decisions recorded

ADR captures:
- SDK embed-vs-runtime-synthesis trade-off (chose embed for strict-cutover discipline)
- Engine strip-vs-schema-declaration trade-off (chose strip for surgical-fix scope)
- v0.51.3 as minimal-scope point release boundary
