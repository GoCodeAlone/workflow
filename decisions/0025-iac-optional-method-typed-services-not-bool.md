# 0025: IaC optional methods are typed gRPC services, not NotSupported flags or codes.Unimplemented

- **Date:** 2026-05-10
- **Status:** Accepted

## Context

`interfaces.IaCProvider` exposes 11 required methods (`Initialize`,
`Plan`, `Apply`, `Destroy`, …) and a set of OPTIONAL sub-interfaces
that a provider MAY implement: `Enumerator`, `EnumeratorAll`,
`DriftConfigDetector`, `ProviderCredentialRevoker`,
`ProviderMigrationRepairer`, `ProviderValidator`. The wfctl call
sites historically `type-assert` against the optional interface and
treat the negative case as "skip this provider."

The strict-contracts force-cutover (ADR 0024) replaces the legacy
`InvokeService` + `*structpb.Struct` dispatch with typed gRPC
services. The question this ADR settles: **how does the typed
contract surface "optional" capability?**

Three candidate designs were considered:

### Candidate A — single service + `NotSupported bool` per method

```proto
service IaCProvider {
  rpc EnumerateAll(EnumerateAllRequest) returns (EnumerateAllResponse);
  // …
}
message EnumerateAllResponse {
  repeated ResourceOutput outputs = 1;
  bool not_supported = 2;  // plugin sets when it doesn't implement
}
```

The plugin author embeds an `Unimplemented` server stub for every
RPC; per-method, they choose whether to override. Untouched methods
return `NotSupported=true` from the embed.

**Cycle 1 I-1 finding**: this is the legacy bug class repackaged. The
"is this method supported?" decision shifts from a typed Go
interface (caught at compile time on the plugin side) to a runtime
boolean field (caught at the first call site that observes the
flag). The "stub-everything-and-forget-to-flip-not_supported"
failure mode is identical in shape to the legacy
"forget-to-add-a-case-string-to-the-switch" failure mode.

### Candidate B — single service + `codes.Unimplemented` for unsupported methods

```proto
service IaCProvider { /* same as A */ }
```

The plugin author embeds `Unimplemented*Server` stubs that return
`status.Error(codes.Unimplemented, ...)` from every method by
default. wfctl call sites check `status.FromError` for
`codes.Unimplemented` and treat it as "skip."

**Cycle 1 I-1 finding** (escalated): worse than A. The bug-class
shape is now identical to v0.27.1's
`isPluginMethodUnimplemented` boundary translator: a string match
on the gRPC status, with each call site repeating the boilerplate.
A new optional method ships, the plugin author forgets to override
the embed, every call site that uses it observes
`codes.Unimplemented`, and the system silently skips real work.

### Candidate C — split into REQUIRED + OPTIONAL gRPC services (chosen)

```proto
service IaCProviderRequired {
  rpc Initialize(InitializeRequest) returns (InitializeResponse);
  // … 11 methods — every plugin MUST implement every one.
}
service IaCProviderEnumerator {
  rpc EnumerateAll(EnumerateAllRequest) returns (EnumerateAllResponse);
  rpc EnumerateByTag(EnumerateByTagRequest) returns (EnumerateByTagResponse);
}
service IaCProviderDriftDetector { /* … */ }
// … 6 optional services total.
```

The plugin author implements `pb.IaCProviderRequiredServer` (compile-
fail-equivalent error if any method is missing) plus whichever
optional service interfaces match capability. The SDK helper
`RegisterAllIaCProviderServices(grpcSrv, provider)` uses Go type-
assertion to register every typed service the provider satisfies, in
one call.

Plugin authors **cannot half-implement** an optional capability and
forget to register it: if their Go type satisfies the interface, the
SDK auto-registers. wfctl detects "absent capability" via the
service NOT appearing in the `GetContractRegistry` response — the
absence of the service IS the negative signal, no flag in any
response body.

Per cycle 3 I-1 of the design.

## Decision

Adopt **Candidate C**: split the typed IaC contract into one
REQUIRED service (`IaCProviderRequired`, 11 RPCs) plus 6 OPTIONAL
services, one per capability. The plugin SDK auto-registers every
service whose Go interface the provider satisfies via a single helper
call (`sdk.RegisterAllIaCProviderServices`). wfctl detects
"capability absent" by the service not appearing in the
`ContractRegistry` payload — there is NO `NotSupported bool` field
on any response and the typed contract NEVER returns
`codes.Unimplemented` as a "supported by intent" signal.

`ResourceDriver` ships as a separate gRPC service (10 RPCs), also
auto-registered, also discovered via the same `ContractRegistry`
mechanism.

## Consequences

Positive:

- Capability decision moves to compile time on the plugin side: a
  Go type that doesn't satisfy `pb.IaCProviderEnumeratorServer`
  cannot be auto-registered as an enumerator. The plugin author
  cannot stub-and-forget.
- Capability discovery on the wfctl side is a single grep:
  `descriptor.ServiceName == "workflow.plugin.external.iac.IaCProviderEnumerator"`.
  No status-code string matching, no per-method response-flag
  checking.
- Adding a new optional capability later is purely additive: append
  a service to `iac.proto`, add the `if v, ok := provider.(...)`
  branch to `RegisterAllIaCProviderServices`, and the existing
  plugins continue to work unchanged (their type doesn't satisfy
  the new interface, so registration skips).

Negative:

- Six small gRPC services rather than one large one — slightly more
  proto surface, slightly more generated code, slightly more
  service descriptors emitted in `ContractRegistry`. Cycle 2 I-1's
  belt-and-braces wftest BDD test
  (`AssertProviderCapabilitiesMatchRegistration`) catches the rare
  case where a plugin author ignores the auto-register helper and
  manually picks a subset of services to register (advanced
  use case).
- The split makes the existing Go interface naming (`Enumerator`
  vs `EnumeratorAll`) non-1:1 with the proto naming
  (`IaCProviderEnumerator` covers both methods). Documented in
  `iac.proto` comments; no functional impact.

## Alternatives Rejected

**Candidate A (NotSupported flag)**: rejected per cycle 1 I-1 — same
bug-class shape as the legacy `InvokeService` switch dispatcher.

**Candidate B (codes.Unimplemented)**: rejected per cycle 1 I-1 +
v0.27.1 evidence — string-matching `Unimplemented` at the boundary
is the same boilerplate the cutover is trying to delete.

**One service with method-level type-assert in the SDK**: rejected
because the plugin author can register a partial server (some
methods bound, others stubbed via embed); the bug class returns at
the first call site that hits a stubbed method. The service-level
split forces the decision at registration time, not at call time.
