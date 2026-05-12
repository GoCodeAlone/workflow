# ADR-0031: Strict-contracts ergonomics (manifest embedding + internal-key strip)

**Date:** 2026-05-12
**Status:** Accepted
**Supersedes:** (none)
**Related:** ADR-0024 (IaC typed force-cutover), ADR-0028 (pure typed cutover)

## Context

BMW PR Task 8 smoke gate (against `workflow-server v0.51.2` + 6 wave plugins) caught two upstream bugs blocking strict-cutover plugin registration:

1. **IaC plugin manifest version gap.** `sdk.ServeIaCPlugin` (the specialized IaC entrypoint introduced for strict-cutover) does not implement the gRPC `GetManifest` RPC. Engine's `ExternalPluginAdapter` falls back to a synthesized `pb.Manifest{Name: "<x>"}` with empty `Version`. `adapter.EngineManifest().Validate()` rejects empty `Version` → plugin registration fails. Affects every IaC plugin (DO today; AWS/GCP/Azure when shipped).

2. **`_config_dir` injection contaminates STRICT_PROTO module configs.** Engine injects `_config_dir` into every module config (`engine.go:499`) to support legacy modules that resolve filesystem-relative paths. For STRICT_PROTO modules, `mapToTypedAny` (with `DiscardUnknown: false`) rejects `_config_dir` as an unknown field. The auth-credential module (from auth v0.2.1) failed initialization with `proto: unknown field "_config_dir"`.

Per workspace mandate `feedback_force_strict_contracts_no_compat`: strict-cutover ships with no compat layer. These are ergonomics gaps in the strict-cutover surface that block multiple plugin classes.

## Decision

Three surgical changes shipped in workflow v0.51.3 (minimal-scope point release; revised after R1 adversarial review):

1. **Engine-side disk-manifest fallback (PRIMARY Bug 1 fix).** `manager.go:108` already loads `*plugin.PluginManifest` via `pluginpkg.LoadManifest` + `Validate` before subprocess launch. Thread that manifest into `NewExternalPluginAdapter` as a fallback. When gRPC `GetManifest` returns empty `Version`, `EngineManifest()` returns disk-manifest fields (field-mapped via `manifestFromDisk(*plugin.PluginManifest) *pb.Manifest`). **Zero plugin-side change required to unblock plugin registration.**

2. **SDK manifest-embedding helper (FORWARD-LOOKING Bug 1 fix).** `sdk.EmbedManifest([]byte) (*plugin.PluginManifest, error)` + `sdk.MustEmbedManifest`. **Parses via canonical `*plugin.PluginManifest`** (camelCase JSON tags matching `plugin.json` authoring convention), NOT directly into `*pb.Manifest` (which has snake_case proto JSON tags — would silently drop `configMutable` etc.). Wired into:
   - `sdk.Serve` + `sdk.ServePluginFull`: via `grpcServer.GetManifest` handler.
   - `sdk.ServeIaCPlugin`: via `IaCServeOptions.ManifestProvider` field; `iacPluginServiceBridge.GetManifest` override returns mapped manifest (the bridge embeds `pb.UnimplementedPluginServiceServer`, so this MUST be on the bridge, NOT on `grpcServer`).
   No new `ManifestProvider` interface — concrete `*plugin.PluginManifest` is the type. Existing tolerance (workflow PR #627) remains as safety net.

3. **Engine strips internal keys before STRICT_PROTO encoding.** `createTypedConfigRequest` (in `plugin/external/adapter.go`) filters `_`-prefix keys from `cfg` before `mapToTypedAny`. Copy-on-clean (fresh map; engine's original `modCfg.Config` retains `_config_dir` for the legacy `*structpb.Struct` path). Establishes `_`-prefix as the reserved namespace for engine internals; STRICT_PROTO module proto schemas must not declare `_`-prefix fields. STRICT_PROTO modules that need filesystem context must declare it explicitly in their proto schema.

## Alternatives considered

**Bug 1 alternatives (revised after R1 review):**
- ~~**Engine reads disk plugin.json fallback:** couples engine to filesystem layout; fights strict-cutover discipline.~~ **R1 reviewer Option 3 corrected this:** the engine ALREADY loads + validates `plugin.json` from disk at `manager.go:108` before subprocess launch. The "coupling" already exists; reusing the loaded manifest as a fallback adds zero new coupling. **Accepted as the PRIMARY fix** because it requires zero plugin-side change and unblocks BMW deploy the moment v0.51.3 ships.
- **SDK helper alone (without engine fallback):** would require every plugin to ship a new version adopting the helper before BMW could deploy. Multi-PR cascade. Rejected as primary fix; kept as forward-looking secondary fix.
- **Per-plugin custom GetManifest:** each plugin author writes boilerplate; missed by `ServeIaCPlugin` users today. Rejected.

Final decision: **both** engine fallback + SDK helper. Engine fallback is the immediate unblock (zero plugin churn). SDK helper is forward-looking (defense-in-depth + portability).

**Bug 2 alternatives:**
- **Add `_config_dir` to every proto schema:** pollutes API surface; doesn't scale to future internals like `_module_name`, `_workflow_id`, etc.
- **Switch to `mapToTypedAnyKnownFields` (filterUnknown=true):** silently drops legitimate typos / mistakes — bad debuggability.

Rejected: surgical strip at the engine boundary keeps STRICT_PROTO module APIs clean while preserving DiscardUnknown=false for catching real authoring errors.

## Consequences

**Positive:**

- Unblocks BMW deploy (workflow-server v0.51.3 + plugins compatible).
- Establishes `sdk.EmbedManifest` as the recommended manifest pattern for all future plugins.
- Establishes `_`-prefix as the engine-internals namespace; documents the convention.
- IaC plugin authors don't have to think about gRPC manifest plumbing.
- STRICT_PROTO module authors don't have to know about `_config_dir`.

**Negative:**

- Plugin-side adoption is a separate workstream — each plugin's next release must adopt `sdk.EmbedManifest`. Old releases still register (via PR #627 tolerance fallback), but with stale-info manifest.
- Plugin authors must keep `plugin.json:version` in sync with their goreleaser tag (or registration succeeds but reports stale version). Mitigated by the auth-style `PLUGIN_MANIFEST_EXPECT_VERSION` integration test pattern.
- `_`-prefix convention is forward-looking — pre-existing proto schemas with `_`-prefix fields would have those silently dropped (no known instances today; audit before merge).

**Neutral:**

- Engine-side `_config_dir` injection (engine.go:499) is unchanged. Legacy modules continue to receive it; STRICT_PROTO modules see it filtered before proto encoding.
- PR #627 tolerance remains as a safety net; combining helper + tolerance gradual-migrates the plugin ecosystem to clean manifest semantics.

## Citations

- Design doc: `docs/plans/2026-05-12-strict-contracts-ergonomics-design.md`
- BMW smoke transcript (failure evidence): `buymywishlist/docs/audit/2026-05-12-smoke-transcript.txt`
- Engine `_config_dir` injection: `engine.go:495-500`
- Engine adapter manifest validation: `plugin/external/adapter.go:274-300` (`EngineManifest`)
- mapToTypedAny + DiscardUnknown: `plugin/external/convert.go:38-65`
- Related cutover: ADR-0024 (IaC typed force-cutover), workflow PR #627 (GetManifest tolerance, in v0.51.2)
