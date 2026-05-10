# 0024: IaC typed force-cutover — supersedes 2026-04-26 additive design (IaC interfaces only)

- **Date:** 2026-05-10
- **Status:** Accepted

## Context

The 2026-04-26 strict-grpc-plugin-contracts design proposed an **additive**
migration: introduce typed gRPC contracts alongside the legacy
`InvokeService` + `*structpb.Struct` dispatch path; let plugins migrate
when convenient; eventually drop the legacy surface after every plugin
opted in. Workflow shipped that design's PR #497 and the resulting
14-plugin migration tracker is in flight for Module/Step/Trigger types.

For the **IaC interfaces specifically** (`interfaces.IaCProvider` +
`interfaces.ResourceDriver`), the additive path produced a recurring
bug class through 2026-04 and 2026-05:

- **T3.9 finding** — `*structpb.Struct.NewStruct` silently dropped
  `map[string]bool` entries (sensitive-key flags on `ResourceOutput`),
  reaching the plugin as `args=map[]` rather than producing a typing
  error. The bug shipped to production before runtime-launch validation
  caught it.
- **v0.27.0 EnumeratorAll bridge gap** — the wfctl-side
  `*remoteIaCProvider` proxy did not implement the optional
  `EnumeratorAll` method, so type-asserts in `infra audit-keys` /
  `infra prune` always failed even when the plugin process implemented
  it. Required a v0.27.1 hot-fix and a workspace memory entry
  (`feedback_workflow_plugin_structpb_boundary`) to prevent
  recurrence.
- **InvokeService case-string typo class** — the 600+ line
  `remoteIaCProvider` proxy and the per-method `switch` dispatcher in
  `internal/module_instance.go` paired hand-written method-name
  literals on both sides. Mismatches were caught only at runtime
  dispatch time, on the first call site to exercise the affected RPC.

The bug-cycle data is unambiguous: **for IaC interfaces, the additive
approach has not paid back its own complexity**. The four bug-class
surfaces (string-keyed args, structpb encoding, hand-written client
proxy, hand-written server dispatcher) compound on each other; each
one taken individually is "small," but the failure modes cross the
boundary in ways that single-side typing cannot prevent.

The user mandate (`feedback_force_strict_contracts_no_compat`) closes
the question: hard cutover, no compat shim, no build-tag dual-path,
no codes.Unimplemented loophole. The strict-contracts force-cutover
plan (`docs/plans/2026-05-10-strict-contracts-force-cutover.md`)
implements that mandate.

## Decision

**Hard cutover for IaC-flavored interfaces (`IaCProvider`,
`ResourceDriver`) supersedes the additive approach of 2026-04-26 for
those interfaces only.** The Module/Step/Trigger additive work
(workflow PR #497 + the 14-plugin migration tracker) remains the live
design for those interfaces.

Concretely:

- Workflow ships `v1.0.0-rc1` introducing `iac.proto` with
  `service IaCProviderRequired` + 6 optional services + `service
  ResourceDriver`, plus a typed SDK helper that auto-registers every
  service the plugin satisfies.
- Workflow ships `v1.0.0` final after the DO plugin migrates to
  `pb.IaCProviderRequiredServer`. The cutover commit DELETES the
  legacy `remoteIaCProvider`, `remoteResourceDriver`, the
  `InvokeService` RPC (for IaC paths), and the DO plugin's
  `internal/module_instance.go` switch dispatcher.
- The DO plugin (`workflow-plugin-digitalocean`) ships `v1.0.0`
  implementing `pb.IaCProviderRequiredServer` + every optional
  service it needs.
- Other GoCodeAlone IaC plugins (`workflow-plugin-aws`,
  `workflow-plugin-gcp`, `workflow-plugin-azure`,
  `workflow-plugin-tofu`) are NOT impacted: cycle 1 I-5 verified they
  do not currently expose IaC via remote dispatch. They migrate
  net-new at their own cadence.
- Application consumers (`core-dump`, `BMW`, `workflow-cloud`,
  `ratchet`, `ratchet-cli`, `workflow-cloud-ui`) bump their
  workflow + DO plugin pins. None imports `interfaces.IaCProvider`
  programmatically (cycle 1 I-5 grep verified); the change is
  invisible to their code.

The cutover scope is locked at commit `e82b7e0c` per
`superpowers:scope-lock`.

## Consequences

Positive:

- Eliminates four compounding bug-class surfaces (string-keyed args,
  structpb encoding, hand-written client proxy, hand-written server
  dispatcher) in a single coordinated cutover, rather than one PR
  at a time over months.
- Compile-time safety: a missing required method on the plugin side
  fails the plugin's `go build`. A missing client-side stub fails
  `wfctl`'s build. The bug class becomes unreachable, not better-
  caught.
- Shrinks `cmd/wfctl/deploy_providers.go` by ~600 lines (the entire
  `remoteIaCProvider` + `remoteResourceDriver` proxy is deleted, not
  refactored).

Negative:

- One-shot coordination across workflow + DO plugin + 6 application
  pin bumps. Mitigated by the `feat/iac-typed-rc1` rc-tag protocol
  (rc1 unblocks DO plugin work; final v1.0.0 unblocks pin bumps) and
  by the per-PR adversarial review cycle.
- Application repos that pin workflow as a transitive Go module need
  one go.mod bump each. Acceptable per cycle 1 I-5 (none of them
  import the affected interfaces directly).
- The dual-path option that 2026-04-26 contemplated is gone; future
  bug-class surfaces in the IaC contract require a typed-side fix,
  not a structpb-side workaround.

Wire-format: PR 2 workflow ships rc1 first (additive); operators can
test plugins against rc1 before final v1.0.0 lands. State-file format
(JSON-serialized `interfaces.ResourceState`) is invariant across the
cutover — only the wfctl <-> plugin gRPC envelope changes.

## Alternatives Rejected

**Continue the additive 2026-04-26 plan for IaC**: rejected because
the bug-cycle data shows the four bug-class surfaces compound and
single-side typing has not prevented the failures (T3.9 + v0.27.1).

**Compat shim / build-tag dual-path**: rejected by the user mandate
(`feedback_force_strict_contracts_no_compat`). A compat shim
preserves the bug-class surface; build-tag dual-path doubles the
maintenance burden without removing any of the four surfaces.

**Cycle 3 deferral until the 14-plugin Module/Step/Trigger migration
finishes**: rejected because IaC bugs are recurring on the live
production v0.27.x line; the deferral cost (more T3.9-class
incidents) exceeds the cost of the coordinated cutover.
