# ADR 0008 — wfctl --refresh-outputs: paired flag, not semantic change to --refresh

**Status:** Accepted 2026-05-07

## Context

wfctl infra apply has a `--refresh` flag that detects drift and prunes ghost-in-state entries (resources that have disappeared from the cloud) before planning. Separately, `WFCTL_REFRESH_OUTPUTS` is an environment variable that enables a pre-step which reads live cloud Outputs and persists field-level changes to state before computing a plan.

The TC2 cutover workflow (`core-dump/.github/workflows/tc2-cutover.yml`) needs to trigger both behaviors in a single apply invocation without requiring an environment variable to be set. Operators running ad-hoc cutover operations also need an explicit flag that is self-documenting in shell history and CI logs.

### Candidate designs

1. **Extend --refresh semantics** to include output refresh. Simple for callers; one flag to remember.
2. **New --refresh-outputs flag** paired with --refresh as a separate opt-in.

## Decision

Add a new `--refresh-outputs` flag to `wfctl infra apply`. The flag is apply-only (not added to `wfctl infra plan`) and is independent of `--refresh`:

- `--refresh-outputs` triggers `applyPreStepRefreshOutputs` (reads live cloud Outputs, persists field-level changes).
- `--refresh` continues to detect drift and prune ghost-in-state entries.
- Both may be combined in a single invocation; when combined, `--refresh-outputs` runs first.
- `--skip-refresh` cancels only the `WFCTL_REFRESH_OUTPUTS` env-var pre-step; it does NOT cancel an explicit `--refresh-outputs` flag.
- The `refreshOutputsRan` guard in `runInfraApply` prevents double-invocation when both the flag and the env-var are set.

Design 1 was rejected. The adversarial design review (cycle 1) flagged this as a "behavior break for existing operators": changing `--refresh` semantics would silently add a cloud-read pass for every operator already using `--refresh` for ghost-prune-only workflows. That is a backwards-incompatible behavior change disguised as a flag renaming. Operators would see longer apply runtimes and new network calls with no visible warning.

## Consequences

- Operators who want Outputs refresh without ghost-prune use `--refresh-outputs` alone.
- Operators who want ghost-prune without Outputs refresh use `--refresh` alone (unchanged).
- Operators who want both use `--refresh --refresh-outputs`.
- TC2 cutover scripts invoke both flags explicitly, making the intended behavior self-documenting.
- `WFCTL_REFRESH_OUTPUTS` env var remains available for CI environments that want the pre-step without explicit flags; it does not activate ghost-prune and is not deprecated by this ADR.
- A future deprecation notice for `WFCTL_REFRESH_OUTPUTS` (in favor of `--refresh-outputs`) is deferred; the env var is still used by environments that force it on globally.

## References

- Design doc: `docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout-design.md`
- Plan: `docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout.md` Task 7 (T2.1)
- Adversarial design review finding: "behavior break for existing operators" (Important, cycle 1)
- ADR 0006: `WFCTL_REFRESH_OUTPUTS` env var semantics (remains in force)
