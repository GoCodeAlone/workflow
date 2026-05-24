# `wfctl plugin verify-capabilities` Contract-Diff Extension Design

**Issue:** [workflow#767](https://github.com/GoCodeAlone/workflow/issues/767)
**Status:** Draft 2026-05-24 — awaiting adversarial design review
**Author:** Jon Langevin
**Parent:** workflow#765 (verify-capabilities scoped to Name+Version) + cycle-3 Reviewer Option 2 deferring contract-diff with two prerequisites

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
// (fully-qualified gRPC service names, e.g. "workflow.iac.v1.IaCProviderRequired").
// Authored in plugin.json either as a top-level "iacServices" key OR nested
// under "capabilities.iacServices" (UnmarshalJSON's object branch promotes
// the nested form). The engine cross-checks these against the plugin's
// runtime ContractRegistry via wfctl plugin verify-capabilities.
IaCServices []string `json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
```

Pattern mirrors existing `IaCStateBackends` field (`plugin/manifest.go:54-62`). UnmarshalJSON's legacy object-form promotion (`plugin/manifest.go:142-159`) gets the new key wired same way.

### 2. SDK helper: `BuildContractRegistryForPlugin(grpcSrv, namespace)`

```go
// BuildContractRegistryForPlugin enumerates gRPC services registered on
// grpcSrv whose name starts with the given namespace (e.g. "workflow.iac.v1.")
// and returns a *pb.ContractRegistry. Filters out go-plugin infra services
// (PluginService, GRPCBroker, GRPCStdio, health) and any other namespaces
// that aren't plugin-owned.
//
// Plugin authors opt in by switching their ContractProvider impl:
//   func (p *plugin) ContractRegistry() *pb.ContractRegistry {
//       return sdk.BuildContractRegistryForPlugin(p.grpcServer, "workflow.iac.v1.")
//   }
//
// BuildContractRegistry (full-surface, no filter) is retained for plugin
// types where exposing the full surface is intentional.
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespace string) *pb.ContractRegistry { ... }
```

### 3. Extend `wfctl plugin verify-capabilities`

After existing Name+Version diff:

- Call `pb.NewPluginServiceClient(conn).GetContractRegistry(ctx, Empty)`.
- Client-side filter to services starting with `workflow.iac.v1.` (hardcoded namespace; design Decision §1 below).
- Set-equal diff against `plugin.json.iacServices` (sorted both sides).
- IF `plugin.json.iacServices` is empty: skip the contract-diff (plugin doesn't claim any IaC services; no diff to perform). Non-IaC plugins remain unaffected.
- ELSE: missing or extra service names → FAIL with field-by-field report.

### 4. Sweep 4 IaC plugins to populate `iacServices`

- `workflow-plugin-aws` — populate from its existing typed-IaC registrations.
- `workflow-plugin-azure` — same.
- `workflow-plugin-gcp` — same.
- `workflow-plugin-digitalocean` — same.

Each plugin's `plugin.json` gets `"iacServices": ["workflow.iac.v1.IaCProviderRequired", ...]` listing the typed services it registers via `iacserver.go`'s `pb.RegisterIaCProviderRequiredServer` etc. The exact list is per-plugin (depends on which optional IaC interfaces each implements).

## Decisions

**§1 — Namespace filter: hardcoded `workflow.iac.v1.` vs configurable.**
Hardcoded. IaC plugins are the only consumers today. A future second-namespace use (e.g. `workflow.cms.v1.*`) can either reuse the helper with the new namespace, or extend verify-capabilities to take a `--namespace` flag. YAGNI for now. ADR: `decisions/NNNN-verify-capabilities-iac-namespace.md`.

**§2 — Server-side filter (plugin authors switch ContractRegistry impl) vs client-side filter (verify-capabilities filters).**
BOTH. Server-side helper exists for plugin authors who want clean output everywhere (logs, debug RPC). Client-side filter in verify-capabilities runs regardless — plugins that haven't migrated to `BuildContractRegistryForPlugin` still get correct diff results because client-side filter handles the noise. Defense in depth.

**§3 — Set-equal vs subset comparison.**
Set-equal. Plugin.json's `iacServices` is the source of truth; binary must advertise EXACTLY those (no more, no less). Missing → bug. Extra → bug (something registered that isn't declared).

**§4 — Empty `iacServices` semantics.**
Empty list = "no contract-diff for this plugin". Both non-IaC plugins (legitimately no IaC services) and IaC plugins that haven't been swept yet (Task 4 backlog) skip the diff cleanly. Future tightening: `validate-contract` could enforce non-empty `iacServices` for `type:"iac"` plugins.

## Files

- `plugin/manifest.go` — add `IaCServices` field + UnmarshalJSON nested-promotion.
- `plugin/external/sdk/contracts.go` — add `BuildContractRegistryForPlugin`.
- `cmd/wfctl/plugin_verify_capabilities.go` — extend `runPluginVerifyCapabilities` with GetContractRegistry call + filter + diff.
- `cmd/wfctl/plugin_verify_capabilities_test.go` — new test scenarios: `iac-good` (matching services), `iac-missing-service` (declared but not advertised), `iac-extra-service` (advertised but not declared).
- `cmd/wfctl/testdata/verify_capabilities/iac-{good,missing-service,extra-service}/` — 3 new fixture scenarios using `sdk.ServeIaCPlugin` + `sdk.IaCServeOptions{Modules: ..., BuildVersion: ...}` so they actually register IaC services on the wire.
- `workflow-plugin-aws/plugin.json` (+ azure/gcp/digitalocean) — populate `iacServices` field.

## Assumptions

1. **All 4 IaC plugins register the same canonical namespace `workflow.iac.v1.*`** — verified per `plugin/external/proto/iac.proto` package declaration.
2. **`pb.PluginServiceClient.GetContractRegistry` is wired uniformly across all plugin types** — non-IaC plugins return an empty/infra-only registry that the namespace filter reduces to empty (no diff fires per §4).
3. **`grpc.Server.GetServiceInfo()` returns fully-qualified service names** — verified per gRPC-go API.
4. **Fixture scenarios can build IaC plugins in-place via `go build -mod=readonly`** — verified pattern from #765 fixtures.

## Failure modes

- **Plugin doesn't implement ContractProvider**: `GetContractRegistry` returns `Unimplemented`. Skip contract-diff with a WARN line (matches verify-capabilities' existing GetManifest-Unimplemented handling).
- **Network/RPC failure mid-call**: hard exit 1 with the RPC error message + stderr tail.
- **Plugin advertises a service in a different namespace** (e.g. `workflow.iac.v2.*` post-cutover): client-side filter excludes it. Forward-compat handled by bumping the hardcoded namespace in a future PR.

## Rollback

Runtime-affecting (changes CLI subcommand behavior + plugin SDK API). Rollback path:

- **PR revert**: removes `IaCServices` field, removes SDK helper, reverts verify-capabilities extension. All scaffold-template `release.yml` files that ran the post-goreleaser step continue working (verify-capabilities still does Name+Version diff; the contract-diff just stops firing).
- **Sweep PRs (4 IaC plugins)**: each is a separate plugin-repo PR; revert independently if needed.
- **Backwards-compat**: subcommand behavior is a strict SUPERSET — adds new diff dimension; doesn't change Name/Version diff semantics. Older wfctl callers (without verify-capabilities the subcommand) continue to work. plugin.json with no `iacServices` field continues to work (treated as empty list per §4).

## Top 3 doubts (self-challenge)

1. **Hardcoded `workflow.iac.v1.` namespace is forward-incompatible with a v2 cutover.** Mitigation: documented in §1 ADR; v2 introduces `--namespace` flag at that point. Single-line change.
2. **Empty `iacServices` skip-with-no-diff means non-swept IaC plugins get NO truth-check on their contract surface.** Mitigation: explicit in §4; sweep follow-up tracked. Acceptable trade-off: opt-in is better than forced-empty failure for non-swept plugins.
3. **Plugin authors who DON'T switch to `BuildContractRegistryForPlugin` still emit noise in their `GetContractRegistry` RPC response.** Mitigation: client-side filter in verify-capabilities masks the noise from the diff. Authors can migrate independently for cleaner debug/log output. No correctness regression.

## Non-goals (explicit)

- Does NOT support multi-namespace plugins (single `workflow.iac.v1.*` hardcoded).
- Does NOT auto-populate `plugin.json.iacServices` from runtime introspection (operator authors it; or a future `wfctl plugin scaffold-iac-services` helper).
- Does NOT change non-IaC plugin behavior (empty iacServices = no diff).
- Does NOT extend `validate-contract` to enforce `iacServices` non-empty for `type:"iac"` plugins (future tightening; out of scope here).
- Does NOT sweep the scaffold release.yml wiring (already done in Layer 3b extension; no change needed).
