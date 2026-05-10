# 0029 — Capability extension: canonical_keys + compute_plan_version on `CapabilitiesResponse`

**Status:** Accepted
**Date:** 2026-05-10
**Plan:** docs/plans/2026-05-10-strict-contracts-force-cutover.md (rev5, scope-locked at e82b7e0c)
**Supersedes nothing.** Closes regressions surfaced during ADR-0024/0025/0026 implementation.

## Context

The strict-contracts force-cutover (ADRs 0024-0028) replaced legacy
`InvokeService(method, args)` IaC dispatch with typed `pb.IaCProvider*`
gRPC services. Two engine-side IaCProvider methods had no typed-RPC
counterpart in the rev5 proto:

1. **`SupportedCanonicalKeys() []string`** — wfctl-side canonical-key set,
   per ADR-0026 documented as "wfctl-side resolved" via the static
   `interfaces.CanonicalKeys()` default. The legacy `remoteIaCProvider`
   actually routed this through `InvokeService("SupportedCanonicalKeys", …)`,
   letting plugins **override** with a strict subset (DO plugin removes
   `loadbalancer`, `vpc`, `kubernetes_cluster` via its
   `doUnsupportedCanonicalKeys` filter). The typed cutover preserved the
   wfctl-side default but lost the override path — confirmed regression
   on DO plugin per Task 30 implementer survey.

2. **`ComputePlanVersion() string`** (optional via
   `wfctlhelpers.ComputePlanVersionDeclarer`) — apply-time dispatch
   version selector. Legacy `remoteIaCProvider` exposed this from the
   plugin manifest (`iacProvider.computePlanVersion` field). DO plugin
   declares `"v2"`, routing apply through `wfctlhelpers.ApplyPlan` instead
   of legacy `provider.Apply`. The typed `*typedIaCAdapter` did not
   implement `ComputePlanVersionDeclarer`, so `DispatchVersionFor` fell
   silently back to `"v1"` — silent dispatch downgrade.

Both regressions are bounded (only DO plugin overrides today; AWS/GCP/
Azure surveys came back empty) but real. Spec-reviewer + team-lead
ruled the fix as a follow-up additive PR (option-d capability extension)
sequenced between Task 17 and Task 20.

## Decision

Extend the existing `CapabilitiesResponse` proto message with two
optional plugin-level fields:

```protobuf
message CapabilitiesResponse {
  repeated IaCCapabilityDeclaration capabilities = 1;
  // Provider-level override of interfaces.CanonicalKeys() default.
  // Empty = use wfctl-side default; non-empty = filter to these keys.
  repeated string canonical_keys = 2;
  // Provider-level apply-time dispatch version. "" or unrecognized = "v1";
  // "v2" routes through wfctlhelpers.ApplyPlan.
  string compute_plan_version = 3;
}
```

Update `*typedIaCAdapter`:

- **Cache `CapabilitiesResponse`** at first access via
  `fetchCapabilities()`. Capabilities are advertised once at plugin
  startup and don't change during a wfctl invocation; caching avoids
  RPC thrash on the apply-time dispatch hot path.
- **`SupportedCanonicalKeys()`** reads `resp.GetCanonicalKeys()`; falls
  back to `interfaces.CanonicalKeys()` when empty or RPC fails.
- **`ComputePlanVersion()`** reads `resp.GetComputePlanVersion()`;
  returns `""` on miss (empty string defaults to `"v1"` via
  `DispatchVersionFor`'s existing default-on-empty rule).
- Compile-time guard
  `var _ wfctlhelpers.ComputePlanVersionDeclarer = (*typedIaCAdapter)(nil)`
  so the type-assert dispatch resolves to the typed adapter without
  callers needing concrete-type knowledge.

## Consequences

- **DO plugin regression closed** once DO plugin v1.0.0 (Phase 2) populates
  these fields in its `Capabilities` RPC response. Until then, DO plugin
  on rev5+ workflow yields the wfctl-side default canonical keys + v1
  dispatch — matches the typed-cutover transient behavior (acceptable).
- **AWS/GCP/Azure plugins**: when they cut over to typed gRPC services,
  they SHOULD populate `canonical_keys` if they want to filter the
  default set; otherwise they get the default automatically. Backward-
  compatible.
- **Wire-level**: additive proto field numbers (2, 3); old plugin
  binaries return empty values; new wfctl interprets empty as
  "use defaults". No flag-day required.
- **Engine consumers** (`module/infra_module.go`, `iac/wfctlhelpers/apply.go`)
  unchanged — they call `provider.SupportedCanonicalKeys()` and
  `wfctlhelpers.DispatchVersionFor(provider)` exactly as before; the
  adapter satisfies both contracts now.
- **Tests**: 5 new test cases on `*typedIaCAdapter` cover the override
  path, the fallback path, the apply-version declaration, the empty-
  declaration default, and the capability-cache reuse invariant.

## Alternatives Rejected

- **Per-`IaCCapabilityDeclaration` `canonical_keys` (per resource type)**:
  rejected. DO's `doUnsupportedCanonicalKeys` is a top-level filter, not
  per-resource-type. Per-type would add structural complexity for a
  flexibility no current plugin exercises. YAGNI.

- **Dedicated `SupportedCanonicalKeys` RPC**: rejected per spec-reviewer
  during Task 30 review. Capability discovery is the natural home; an
  extra RPC would mean two round-trips at plugin load time, extra
  schema drift surface, and another optional-service registration to
  manage. The Capabilities RPC is already required and lightweight.

- **`ComputePlanVersionDeclarer` as a separate optional gRPC service**:
  rejected. The version is a single string declaration, not a method
  surface. Extending `CapabilitiesResponse` is a 1-field change; adding
  a service is a registration + stub + auto-detect cycle. Keep the
  registration-based capability advertisement (ADR-0025) for actual
  *behavior* surfaces (Enumerator, DriftDetector, etc.), not for
  static configuration values.

- **Embed in `IaCCapabilityDeclaration` operations field**: rejected.
  `operations` is the per-resource-type op set (create/read/update/…).
  Plugin-level filters and apply-version don't belong there.

## Migration

For plugin authors:

```go
// Capabilities RPC returns:
return &pb.CapabilitiesResponse{
    Capabilities: []*pb.IaCCapabilityDeclaration{ /* per-type decls */ },
    CanonicalKeys: []string{
        "infra.spaces", "infra.spaces_key", "infra.droplet",
        // (omit infra.loadbalancer, infra.vpc, infra.kubernetes_cluster
        // if your provider doesn't support them)
    },
    ComputePlanVersion: "v2", // or "" for legacy v1 path
}, nil
```

For engine consumers: no change. `provider.SupportedCanonicalKeys()`
and `wfctlhelpers.DispatchVersionFor(provider)` work as before.

## Related

- ADR 0024 (IaC typed force-cutover)
- ADR 0026 (direct gRPC client, no wrapper) — established
  SupportedCanonicalKeys as wfctl-side; this ADR adds the
  plugin-override path
- Task 30 implementer survey (DO plugin override evidence)
- `feedback_force_strict_contracts_no_compat`: this PR is fix-forward
  to a typed gRPC field, NOT a compat shim. Empty fields default to
  legacy/wfctl-side behavior.
