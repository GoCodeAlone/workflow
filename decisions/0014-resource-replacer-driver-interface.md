# 0014: ResourceReplacer optional driver interface

- **Date:** 2026-05-07
- **Status:** Accepted

## Context

`wfctlhelpers.doReplace` (the engine's default Replace dispatcher) decomposes every Replace action
into `Delete(old) → Create(new)`. This is correct for the majority of cloud resources. But some
resources own **single-attach dependents** — child resources that the cloud associates with exactly
one parent at a time and refuses to re-associate while the old parent still exists.

DigitalOcean Block Storage Volumes are the primary example: a Volume is attached to at most one
Droplet. When wfctl replaces a Droplet, the naive Delete-then-Create sequence fails with
`422 Unprocessable Entity: storage already associated with another droplet` if Volumes were attached
to the old Droplet at delete time (DO does not auto-detach on deletion in all API paths).

The DO Droplet driver needs a way to orchestrate: detach volumes → delete old droplet → create new
droplet → attach volumes. The engine cannot do this generically because the detach/reattach sequence
is resource-type-specific.

## Decision

Add `interfaces.ResourceReplacer` as an **optional** interface:

```go
type ResourceReplacer interface {
    Replace(ctx context.Context, oldRef ResourceRef, spec ResourceSpec) (*ResourceOutput, error)
}
```

`wfctlhelpers.doReplace` probes the driver for this interface via a type assertion. On opt-in, the
driver receives the OLD ref (for detach) and the NEW spec (for create-with-resolved-volumes) and is
responsible for the full transition. On non-opt-in, `doReplace` delegates to `DefaultReplace` (the
prior `doReplace` body, now exported).

An engine-side error-prefix backstop (`wrapDriverReplaceError`) wraps non-conforming driver errors
with `"replace: driver: "` so operator attribution is preserved regardless of per-plugin
discipline. Conforming prefixes (`"replace: ..."` engine family, `"<type> replace ..."` driver
family) pass through unchanged.

## Why this approach over alternatives

- **(A1) Engine-generic detach-before-delete**: rejected. Requires the engine to know about
  volume-attachment semantics, which are provider + resource-type specific. Violates the driver
  abstraction boundary.
- **(A2) Sentinel error from doReplace → driver re-entry**: rejected. Round-trips through two driver
  calls, complicates error attribution, adds cognitive overhead to driver authors.
- **(A3) New `ReplaceSequence` action type in IaCPlan**: rejected. Wider blast radius (plan
  serialisation, schema versioning, test fixtures). The hook-style optional interface achieves the
  same without touching plan wire format.

## Consequences

1. Drivers owning single-attach dependents can implement `ResourceReplacer` without any engine-side
   changes beyond this PR.
2. The `"storage already associated"` (TC2 class) failure disappears for DO Droplet replace actions
   when `DropletDriver` adopts the interface (workflow-plugin-digitalocean PR-3).
3. Default behavior is unchanged for all existing drivers — non-opt-in drivers continue to use
   `DefaultReplace` (Delete-then-Create). No breaking change.
4. `wfctlhelpers.DefaultReplace` is now exported so driver-owned `Replace` implementations can
   delegate specific specs back to engine-default behavior (e.g., "replace this network resource
   normally, but detach volumes for this compute resource").

## References

- `interfaces/iac_resource_driver.go` — `ResourceReplacer` interface definition.
- `iac/wfctlhelpers/apply.go` — `doReplace` dispatch + `DefaultReplace` + `wrapDriverReplaceError`.
- `iac/wfctlhelpers/apply_replacer_dispatch_test.go` — dispatch tests.
- `iac/wfctlhelpers/apply_replacer_prefix_test.go` — prefix backstop tests.
- `decisions/0010-applied-config-source-on-resourcestate.md` — related ADR on applied-config provenance (workflow repo).
