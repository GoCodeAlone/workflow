# 0011 — refresh-outputs tolerates ErrResourceNotFound (ghosts) — skip with warn, not error

**Date:** 2026-05-06
**Status:** Accepted

## Context

PR #525 shipped `wfctl infra refresh-outputs` as an opt-in apply pre-step
(`WFCTL_REFRESH_OUTPUTS`) and a standalone subcommand. The original
implementation in `iac/refreshoutputs/refresh.go::refreshOne` propagated every
`Read` error uniformly — `ErrResourceNotFound` was treated the same as a
transient network failure or an auth error.

This surface was first tested end-to-end in the TC2 cutover (run 25476341708).
Ghost Droplet `coredump-staging-pg` (DO ID 568721969, deleted out-of-band but
still present in persisted IaC state) caused `refresh-outputs` to hard-fail
with `iac: resource not found` before the Phase-1 ghost-prune phase could act
on it. The cascade aborted; the pipeline never reached the step that would have
removed the ghost entry.

## Decision

`refreshOne` treats `errors.Is(err, interfaces.ErrResourceNotFound)` as a
skip-and-continue condition: it leaves the ghost resource's `Outputs`
unchanged and returns `nil`. All other errors from `Read` continue to
propagate as hard failures (existing semantics).

Ghost-prune (via `wfctl infra apply --refresh`) remains the canonical mechanism
for removing stale state entries. This fix only makes refresh-outputs safe to
run *before* ghost-prune in an operational pipeline without aborting.

## Alternatives Considered

**(a) Caller-side filter:** Require the caller to detect drift first, filter
out ghosts, then call `Refresh` on the surviving set. Rejected: pushes
complexity onto every caller; callers already have enough to coordinate.

**(b) `--skip-ghosts` flag:** Expose the behaviour as opt-in. Rejected as
overkill — there is no valid use case for "fail on ghost" semantics in
refresh-outputs. The implicit "skip ghost, leave state alone" behaviour is
correct in all operational contexts.

## Consequences

1. `refresh-outputs` is now safe to run before ghost-prune in pipelines.
2. Operators running `wfctl infra refresh-outputs` against state with ghosts
   receive fresh outputs for live resources and no error; ghost entries remain
   in state until `--refresh` prunes them.
3. Non-ghost `Read` errors (transient, auth, quota) still propagate as hard
   failures, preserving the "no half-persist" contract documented in the
   `Refresh` function godoc.

## References

- TC2 cutover run 25476341708 failure analysis
- `iac/refreshoutputs/refresh.go::refreshOne` behaviour change (this PR)
- `interfaces/iac_resource_driver.go`: `ErrResourceNotFound` sentinel definition
