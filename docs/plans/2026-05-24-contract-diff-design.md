# `wfctl plugin verify-capabilities` Contract-Diff Extension Design

**Issue:** [workflow#767](https://github.com/GoCodeAlone/workflow/issues/767)
**Status:** Revised cycle 2 2026-05-24 — awaiting re-review
**Author:** Jon Langevin
**Parent:** workflow#765 (verify-capabilities scoped to Name+Version) + cycle-3 Reviewer Option 2 deferring contract-diff with two prerequisites

## Revision history

- **Cycle 1**: initial design hardcoded namespace `workflow.iac.v1.*`. FAILED — 2 Critical (wrong namespace; never traced to proto file; actual is `workflow.plugin.external.iac.*`. Duplicates existing `registeredIaCServices`/`iacServiceRequired` precedent without citing or reusing). + 5 Important.
- **Cycle 2** (this version): namespace derived programmatically from `pb.IaCProviderRequired_ServiceDesc.ServiceName`. Citations + reuse of existing helpers. Directional diff (FAIL on missing-from-binary, WARN on extra). Use cached `adapter.ContractRegistry()`. Add non-goal for embedded plugin.json. Sweep-target SDK pin assumption made explicit. IaCStateBackend orthogonality documented.

## Problem

`wfctl plugin verify-capabilities` (workflow#765) verifies binary `Manifest.Name` + `Manifest.Version` match `plugin.json`. Two intentional deferrals from #765 cycle-3 review remain open:

1. **No LHS for IaC service diff** — `plugin.json` does not have an `iacServices` field. Static `validate-contract` cannot enforce "binary advertises the typed IaC services it claims to" because there's nothing to claim.
2. **`GetContractRegistry` returns ALL gRPC services** including go-plugin infra (`workflow.plugin.v1.PluginService`, `plugin.GRPCBroker`, `plugin.GRPCStdio`, `grpc.health.v1.Health`). Set-equal diff against any plugin.json-declared list would surface 3-5 spurious "extra-in-binary" entries per plugin.

This design closes both gaps so verify-capabilities can diff `ContractRegistry` against declared `iacServices` cleanly.

## Solution

Three pieces, single PR:

### 1. New `PluginManifest.IaCServices []string` field

```go
// IaCServices lists the typed IaC service names this plugin serves
// (fully-qualified gRPC service names, e.g.
// "workflow.plugin.external.iac.IaCProviderRequired"). Authored in
// plugin.json either as a top-level "iacServices" key OR nested under
// "capabilities.iacServices" (UnmarshalJSON's object branch promotes
// the nested form). The engine cross-checks these against the plugin's
// runtime ContractRegistry via wfctl plugin verify-capabilities.
//
// Orthogonal to IaCStateBackends (which lists backend NAMES the plugin
// serves, not gRPC service names). A plugin that registers the
// IaCStateBackend service AND lists its backend name(s) in
// iacStateBackends will appear in BOTH manifest fields: IaCServices for
// the wire surface, IaCStateBackends for the backend-name surface.
IaCServices []string `json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
```

Pattern mirrors existing `IaCStateBackends` field (`plugin/manifest.go:54-62`). UnmarshalJSON's legacy object-form promotion (`plugin/manifest.go:142-159`) gets the new key wired same way.

### 2. SDK helper: `BuildContractRegistryForPlugin(grpcSrv, namespacePrefix)`

```go
// BuildContractRegistryForPlugin enumerates gRPC services registered on
// grpcSrv whose name starts with the given namespacePrefix and returns
// a *pb.ContractRegistry. Filters out go-plugin infra services
// (PluginService, GRPCBroker, GRPCStdio, health) and any other
// namespaces that aren't plugin-owned.
//
// Plugin authors opt in by switching their ContractProvider impl:
//   import pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
//   func (p *plugin) ContractRegistry() *pb.ContractRegistry {
//       // Derive prefix from the canonical service descriptor so the
//       // string can't drift from the .proto package declaration.
//       prefix := strings.TrimSuffix(pb.IaCProviderRequired_ServiceDesc.ServiceName, ".IaCProviderRequired") + "."
//       return sdk.BuildContractRegistryForPlugin(p.grpcServer, prefix)
//   }
//
// BuildContractRegistry (full-surface, no filter) is retained for plugin
// types where exposing the full surface is intentional.
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespacePrefix string) *pb.ContractRegistry { ... }
```

### 3. Extend `wfctl plugin verify-capabilities`

After existing Name+Version diff:

- Reuse cached `adapter.ContractRegistry()` (set at `NewExternalPluginAdapter` construction per `plugin/external/adapter.go:165-172`) — no second RPC. The adapter maps `Unimplemented` to an empty registry, so "Unimplemented" and "empty registry" both reduce to "filter returns 0 services" handled by §4 skip-if-LHS-empty.
- Reuse the existing client-side filter precedent: `registeredIaCServices` in `cmd/wfctl/deploy_providers.go:344-361` already walks ContractDescriptors and returns SERVICE-kind names. Either call it directly OR refactor into a shared `cmd/wfctl/iac_contract_filter.go` helper consumed by both deploy_providers and verify-capabilities (recommend the refactor; one line in plan-time task list).
- Derive the namespace prefix programmatically from the existing canonical `iacServiceRequired` const (`cmd/wfctl/iac_typed_adapter.go:52` = `"workflow.plugin.external.iac.IaCProviderRequired"`) via `strings.TrimSuffix(iacServiceRequired, ".IaCProviderRequired") + "."`. No new string literal; single source of truth.
- Set-difference diff (directional, NOT set-equal):
  - `missing-from-binary` = declared in plugin.json AND not in filtered binary set → FAIL.
  - `extra-in-binary` = in filtered binary set AND not declared in plugin.json → WARN (display in output, exit 0 on that field).

Rationale: the load-bearing truth-loop is "did the plugin author forget to register what they declared" (mirrors #765's "did ldflag fire"). "Registered but not declared" is additive — plugin author shipped a new capability ahead of plugin.json bump. That's a documentation lag, not a runtime defect. Set-equal would force lockstep updates between Go-side `iacserver.go` and JSON-side plugin.json for every optional-interface addition.

- IF `plugin.json.iacServices` is empty: skip the contract-diff entirely (plugin doesn't claim any IaC services; no diff to perform). Non-IaC plugins remain unaffected.

### 4. Sweep 4 IaC plugins to populate `iacServices`

- `workflow-plugin-aws` — populate from its existing typed-IaC registrations.
- `workflow-plugin-azure` — same.
- `workflow-plugin-gcp` — same.
- `workflow-plugin-digitalocean` — same.

Each plugin's `plugin.json` gets `"iacServices": ["workflow.plugin.external.iac.IaCProviderRequired", ...]`. Per `iacserver.go:148-192`, the auto-registered set includes 8 IaCProvider* services, plus optionally ResourceDriver, plus optionally IaCStateBackend. Per-plugin list depends on which optional interfaces each implements.

Per cycle-2 IMPORTANT-5 Option B: IaCServices INCLUDES `IaCStateBackend` (and ResourceDriver) when registered. Orthogonal to existing `iacStateBackends` field (backend NAMES). Documented in the field docstring (§1).

## Decisions

**§1 — Namespace filter: derive vs hardcode.**
Derive from `pb.IaCProviderRequired_ServiceDesc.ServiceName` via TrimSuffix. Single source of truth keyed to the .proto package decl. Eliminates cycle-1 C1 (wrong namespace) by construction. ADR: `decisions/NNNN-verify-capabilities-iac-namespace.md`.

**§2 — Server-side filter (plugin authors switch ContractRegistry impl) vs client-side filter (verify-capabilities filters).**
BOTH. Server-side helper exists for plugin authors who want clean output everywhere (logs, debug RPC). Client-side filter in verify-capabilities runs regardless — plugins that haven't migrated to `BuildContractRegistryForPlugin` still get correct diff results because client-side filter handles the noise. Defense in depth.

**§3 — Directional diff vs set-equal.** (Revised cycle 2 per IMPORTANT-1.)
Directional. `missing-from-binary` is FAIL (truth-loop bug). `extra-in-binary` is WARN. Plugin authors who add optional-interface methods don't need to lockstep-update plugin.json.

**§4 — Empty `iacServices` semantics.**
Empty list = "no contract-diff for this plugin". Both non-IaC plugins (legitimately no IaC services) and IaC plugins that haven't been swept yet skip the diff cleanly. Future tightening: `validate-contract` could enforce non-empty `iacServices` for `type:"iac"` plugins.

**§5 — Reuse existing helpers vs new code.** (Cycle 2 per CRITICAL-2.)
Reuse `registeredIaCServices` (deploy_providers.go:344) and `iacServiceRequired` (iac_typed_adapter.go:52). Refactor `registeredIaCServices` into a shared file if its current location isn't import-friendly for verify-capabilities. Cite both precedents in §Files entries.

## Files

- `plugin/manifest.go` — add `IaCServices` field + UnmarshalJSON nested-promotion.
- `plugin/external/sdk/contracts.go` — add `BuildContractRegistryForPlugin`.
- `cmd/wfctl/iac_contract_filter.go` (NEW) OR `cmd/wfctl/deploy_providers.go` move — house `registeredIaCServices` in a location both deploy_providers and verify-capabilities can import.
- `cmd/wfctl/plugin_verify_capabilities.go` — extend `runPluginVerifyCapabilities` with cached `adapter.ContractRegistry()` walk + filter (reuse helper) + directional diff.
- `cmd/wfctl/plugin_verify_capabilities_test.go` — new test scenarios: `iac-good` (matching services), `iac-missing-service` (declared but not advertised → FAIL), `iac-extra-service` (advertised but not declared → WARN exit 0).
- `cmd/wfctl/testdata/verify_capabilities/iac-{good,missing-service,extra-service}/` — 3 new fixture scenarios using `sdk.ServeIaCPlugin` so they actually register IaC services on the wire.
- `workflow-plugin-aws/plugin.json` (+ azure/gcp/digitalocean) — populate `iacServices` field.

## Assumptions

1. **`pb.IaCProviderRequired_ServiceDesc.ServiceName` resolves to a string ending in `.IaCProviderRequired`** — verified per `/tmp/wfprobe/plugin/external/proto/iac_grpc.pb.go:443` (canonical generated descriptor) AND existing usage in `cmd/wfctl/iac_typed_adapter.go:52` const.
2. **`adapter.ContractRegistry()` returns the cached registry constructed during `NewExternalPluginAdapter` and is safe to call repeatedly** — verified per `plugin/external/adapter.go:165-176`.
3. **All 4 sweep-target plugins (aws, azure, gcp, digitalocean) pin workflow v0.62.0+** — required for the strict-contracts cutover path that registers the typed IaC services and the PluginService bridge that serves GetContractRegistry. (Cycle 2 explicit per IMPORTANT-4.) Pre-flight check: each plugin's `go.mod` must show `github.com/GoCodeAlone/workflow v0.62.0+` before opening its sweep PR. If any plugin pins an older version, the sweep blocker is a workflow-bump cascade, not this design.
4. **`grpc.Server.GetServiceInfo()` returns fully-qualified service names** — verified per gRPC-go API.
5. **Fixture scenarios can build IaC plugins in-place via `go build -mod=readonly`** — verified pattern from #765 fixtures.

## Failure modes

- **Plugin doesn't implement ContractProvider**: `adapter.ContractRegistry()` returns an empty (but non-nil) registry per `plugin/external/adapter.go:165-176` (Unimplemented is mapped to empty there). Filter returns 0 services. Skip-if-LHS-empty handles non-IaC plugins; for IaC plugins with non-empty plugin.json.iacServices the directional diff fires FAIL on every declared service (consistent with the "plugin advertises nothing" truth-loop signal).
- **Network/RPC failure mid-call**: N/A — no new RPC in verify-capabilities (cached adapter accessor). Adapter-construction RPC failure already surfaces in spawn-and-dial path.
- **Plugin advertises a service in a different namespace** (e.g. `workflow.iac.v2.*` post-cutover): client-side filter excludes it. Forward-compat handled by re-deriving prefix from a future `IaCProviderRequired_v2_ServiceDesc.ServiceName` or by introducing a `--namespace` flag at v2 cutover time.
- **Plugin author lists a service in plugin.json that exists in proto but the plugin's Go code doesn't register** (e.g. declared IaCProviderValidator without implementing the interface): registry filter doesn't surface it → declared-but-not-advertised → FAIL. Correct outcome.
- **Plugin author registers a service the proto package doesn't define** (impossible-by-construction for typed IaC since registration is per-pb-helper): N/A.

## Rollback

Runtime-affecting (changes CLI subcommand behavior + adds plugin SDK API). Rollback path:

- **Workflow PR revert**: removes `IaCServices` field, removes SDK helper, reverts verify-capabilities extension. All scaffold-template `release.yml` files that ran the post-goreleaser step continue working (verify-capabilities still does Name+Version diff; the contract-diff just stops firing).
- **Sweep PRs (4 IaC plugins)**: each is a separate plugin-repo PR; revert independently if needed. Once workflow PR is reverted, the populated `iacServices` field becomes inert (unrecognized JSON key during UnmarshalJSON; no effect on behavior).
- **Backwards-compat**: subcommand behavior is a strict SUPERSET — adds new diff dimension; doesn't change Name/Version diff semantics. Older wfctl callers (without verify-capabilities the subcommand) continue to work. plugin.json with no `iacServices` field continues to work (treated as empty list per §4).

## Top 3 doubts (self-challenge — cycle 2)

1. **Directional diff means a plugin can quietly add services + ship them without ever updating plugin.json.** Mitigation: WARN line in verify-capabilities output surfaces it; plugin authors who care can bump plugin.json. Reverse direction (lockstep) costs more than it catches.
2. **Sweep-target plugins MUST be on workflow v0.62.0+ before their iacServices field can be verified.** Mitigation: explicit assumption (§Assumptions #3) + per-plugin pre-flight check before opening each sweep PR. If a plugin needs a bump first, file as separate cascade.
3. **`IaCStateBackend` field appears in BOTH `iacServices` (wire) and `iacStateBackends` (backend names) — risk of drift between the two.** Mitigation: orthogonality documented in field docstring (§1). Sweep PR populates both consistently. Future tightening: validate-contract could enforce "if iacStateBackends non-empty, iacServices must include workflow.plugin.external.iac.IaCStateBackend".

## Non-goals (explicit)

- Does NOT support multi-namespace plugins (single derived prefix hardcoded).
- Does NOT auto-populate `plugin.json.iacServices` from runtime introspection (operator authors it; or a future `wfctl plugin scaffold-iac-services` helper).
- Does NOT change non-IaC plugin behavior (empty iacServices = no diff).
- Does NOT extend `validate-contract` to enforce `iacServices` non-empty for `type:"iac"` plugins (future tightening; out of scope here).
- Does NOT sweep the scaffold release.yml wiring (already done in Layer 3b extension; no change needed).
- Does NOT verify that the binary's embedded plugin.json (via `sdk.WithManifestProvider`) contains the same `iacServices` as the on-disk plugin.json. The embedded-manifest path doesn't surface this field on the wire (`pb.Manifest` is 6-scalar). Disk plugin.json is the authoritative source for this diff. (Cycle 2 IMPORTANT-3.)
