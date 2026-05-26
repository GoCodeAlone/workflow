# 0026: wfctl uses pb.IaCProviderRequiredClient directly — no hand-written wrapper layer

- **Date:** 2026-05-10
- **Status:** Accepted

## Context

ADR 0024 force-cuts the IaC contract over to typed gRPC services.
The natural follow-up question: how does the wfctl side consume the
typed client?

Two candidate designs:

### Candidate A — typed wrapper layer (`iacProviderClient`)

A hand-written struct in `cmd/wfctl/deploy_providers.go` (or similar)
satisfies `interfaces.IaCProvider` by holding a
`pb.IaCProviderRequiredClient` and a per-optional-service client,
translating each call:

```go
type iacProviderClient struct {
    required   pb.IaCProviderRequiredClient
    enumerator pb.IaCProviderEnumeratorClient   // nil if not registered
    drift      pb.IaCProviderDriftDetectorClient // nil if not registered
    // … one field per optional service
}

func (c *iacProviderClient) Plan(ctx context.Context, …) (*interfaces.IaCPlan, error) {
    resp, err := c.required.Plan(ctx, &pb.PlanRequest{…})
    if err != nil { return nil, c.wrapErr(err) }
    return convertPlanFromProto(resp.Plan), nil
}
```

This is structurally similar to the legacy
`*remoteIaCProvider` (`cmd/wfctl/deploy_providers.go`) — a hand-
written translation layer between Go interface and gRPC client.

### Candidate B — wfctl uses `pb.IaCProviderRequiredClient` directly (chosen)

wfctl call sites construct `pb.IaCProviderRequiredClient` (and per-
optional clients) from the gRPC connection and call typed RPCs
directly. A small `cmd/wfctl/iac_errors.go` helper translates
`status.FromError` to typed Go errors; **no struct wrapper**.

Capability discovery: wfctl reads `GetContractRegistry` once at
plugin handle, sees which services are registered, and only
constructs the optional clients that exist. Type-assert sites in
audit-keys / prune / cleanup are converted to "do I have a non-nil
`pb.IaCProviderEnumeratorClient`?" checks (Task 17 of the plan).

**Cycle 1 Alternative C finding**: the wrapper layer in Candidate A
IS one of the four bug-class surfaces this cutover is trying to
delete. Renaming `remoteIaCProvider` to `iacProviderClient` and
swapping `InvokeService` calls for typed `pb.*Client` calls keeps
the bug-class surface; it just changes the encoding from string-
keyed args to typed structs. The conversion functions
(`convertPlanFromProto`, `convertResourceSpecToProto`, …) re-
introduce a per-method translation surface where field-rename or
field-omission bugs can hide.

By using `pb.IaCProviderRequiredClient` directly, the type system
makes wfctl call sites look at the proto types literally — there is
no Go-interface adapter that can drop a field on translation.

### Engine-side adapter (Task 30) — narrow exception

Engine-side consumers (`platform.ComputePlan`,
`wfctlhelpers.ApplyPlan`, drift-detection helpers) work against
`interfaces.IaCProvider` and `interfaces.ResourceDriver` for
testability (in-process fake providers, table-driven plan tests).
Task 30 of the plan adds a NARROW typed-client→interfaces.IaCProvider
adapter scoped to those engine entry points only — NOT a general-
purpose wrapper. The adapter is a thin lifter, not a re-marshalling
layer.

## Decision

**wfctl uses `pb.IaCProviderRequiredClient` (and per-optional
service clients) directly. No hand-written `iacProviderClient`
wrapper struct that satisfies `interfaces.IaCProvider` exists in
wfctl call sites.** The 5 type-assert sites
(audit-keys, prune, cleanup, drift, rotate-and-prune) are converted
to direct typed-RPC calls with `iac_errors.go` translating
`status.FromError` to typed Go errors.

A narrow engine-side adapter (Task 30) lifts a typed gRPC client to
`interfaces.IaCProvider` for `platform.ComputePlan` /
`wfctlhelpers.ApplyPlan` consumers that already operate against the
Go interface. The adapter is scoped to those engine entry points and
does NOT replace the wfctl-side direct-client pattern.

## Consequences

Positive:

- Removes one of the four bug-class surfaces by removing the layer
  entirely, not by typing it. Cycle 1 Alternative C dynamic.
- wfctl call sites read like the proto: `client.EnumerateAll(ctx,
  &pb.EnumerateAllRequest{ResourceType: "infra.spaces_key"})`. The
  proto IS the API; there is no second API surface to keep in sync.
- Adding a new optional service means a new typed client field at
  call sites that need it, not a new field on a wrapper struct +
  conversion + Go interface method.
- The diff for the cutover commit shows DELETIONS, not RENAMES. The
  cycle 1 M-1 acceptance test (`git log -p` confirms 600+ lines
  removed in `cmd/wfctl/deploy_providers.go`) verifies the wrapper
  was actually deleted, not refactored.

Negative:

- wfctl call sites are slightly more verbose at the call point — a
  typed `Plan(ctx, &pb.PlanRequest{Desired: ..., Current: ...})`
  rather than a Go-interface `provider.Plan(ctx, desired, current)`
  with the proto types hidden behind the abstraction. Mitigated by
  small per-call-site helpers in `cmd/wfctl/iac_errors.go` for
  conversions that genuinely repeat (e.g., resource-ref slice
  conversions).
- Test setup is slightly more verbose: tests construct a typed
  client (or use `bufconn` with a typed server stub) rather than
  passing a Go-interface fake. Acceptable per the typed-IaC E2E
  test pattern in Task 6 (`plugin/external/sdk/iac_e2e_test.go`).
- The engine-side adapter (Task 30) IS a small wrapper. It exists
  because `platform.ComputePlan` already accepts
  `interfaces.IaCProvider` and rewriting it to accept a typed
  client would require changes to every in-process plan-test
  fixture (out of scope for the cutover). Bound to engine call
  sites only.

## Alternatives Rejected

**Candidate A (`iacProviderClient` wrapper struct)**: rejected per
cycle 1 Alternative C — preserves one of the four bug-class
surfaces. The wrapper is structurally indistinguishable from the
legacy `*remoteIaCProvider` it would replace.

**Engine-side direct client (no Task 30 adapter at all)**: rejected
because rewriting `platform.ComputePlan` + `wfctlhelpers.ApplyPlan`
to consume a typed client would multiply the cutover scope and
force every in-process plan-test fixture to be rewritten. The
engine-side adapter is bounded; the wfctl-side direct-client is
the structural improvement.

**General-purpose wrapper that satisfies both `interfaces.IaCProvider`
AND can be substituted for the typed client at call sites**:
rejected because the moment the wrapper has both surfaces, every
caller has a choice; the cutover failure mode shifts to "different
call sites picking different surfaces" — a regression of the
multi-mode bug class this ADR closes.
