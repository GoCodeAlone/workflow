# ADR 011: refresh-outputs merge semantics

**Status:** Accepted
**Date:** 2026-05-06

## Context

`iac/refreshoutputs/refresh.go::refreshOne` previously **replaced** `dst.Outputs`
entirely with the cloud Read response (`live.Outputs`). This was correct for fields
that cloud Read returns, but caused silent data loss for fields that a provider's
Read endpoint does not return.

The canonical example is DigitalOcean Droplets: `user_data` (cloud-init script) is
accepted at creation time but is **never returned by the Read endpoint**. After a
refresh-outputs pass, state would contain `user_data: ""`. The planner's next
plan/apply cycle would compare `state.user_data=""` against `config.user_data="<cloud-init>"`
and emit a REPLACE action. Apply would attempt delete+create; with a Volume already
attached the DO API returned a 422, blocking the TC2 cutover (run 25508442022).

The previous TC2 run (25507244491) succeeded only because that Droplet was a GHOST
at refresh time — the ghost-tolerance fix (PR #572) skipped it, preserving `user_data`
in state. Once the Droplet was created the next refresh clobbered the field.

## Decision

`refreshOne` now **merges** `live.Outputs` into `src.Outputs` rather than replacing
it:

1. Start from a clone of `src.Outputs` (preserves all create-time fields).
2. For each field `k` in `live.Outputs`: if it is absent or differs from `merged[k]`,
   set `merged[k] = v` and mark `needsUpdate = true`.
3. If any field changed, assign `dst.Outputs = merged`.

Semantics:
- **Cloud truth wins** for any field present in the live Read response.
- **Create-time fields are preserved** for fields absent from the live Read response.
- The existing "skip write when nothing changed" optimisation is retained.

## Trade-offs

If a cloud provider truly removes a field from a live resource's outputs (rare for
IaC-managed resources), refresh-outputs will keep the stale value in state. The
planner may not detect the removal unless it also re-reads outputs.

**This is acceptable** because:
- Provider-managed removal of a previously-set output field is uncommon for
  IaC-controlled resources.
- The remediation path is well-defined: `wfctl infra apply --refresh` performs a
  full reconcile and will surface the discrepancy as a plan diff.
- The alternative (replace semantics) causes false REPLACE storms for write-only
  fields, which is a far more disruptive failure mode.

## References

- TC2 run 25508442022 — failure: 422 `storage already associated with another droplet`
  caused by `user_data` clobber.
- TC2 run 25507244491 — success: ghost-skip preserved `user_data`.
- PR #572 — ghost-tolerance fix (`ErrResourceNotFound` skip in `refreshOne`).
- DO Droplet API docs: `user_data` is a create-time-only attribute.
