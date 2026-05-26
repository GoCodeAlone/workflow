# 0013: JIT plan-time resolver against state

- **Date:** 2026-05-07
- **Status:** Accepted

## Context

`wfctl infra plan` left `${MODULE.field}` and `infra_output`-typed `${VAR}` references unresolved at
plan time. Substitution happened at apply time inside `wfctlhelpers.ApplyPlan` via
`jitsubst.ResolveSpec` against `result.ReplaceIDMap` + `syncedOutputs` (the W-5 design). Plan-time
`parseInfraResourceSpecsForEnv` preserved `infra_output`-typed secret keys so that
`desiredStateHash(desired)` was hash-stable across plan/apply boundaries.

This was correct for `random_*` secrets (value doesn't exist until bootstrap). But it was **wrong**
for `infra_output`-typed secrets whose source IS already in state — `STAGING_VPC_UUID` resolves to
`core-dump-vpc.id`, which has been in state since the VPC was first applied.

Downstream effect: `DropletDriver.Diff` compared the literal string `${STAGING_VPC_UUID}` against
state's real UUID and emitted a `ForceNew` change → spurious `replace`. TC2 cutover dispatches
25476341708, 25478458395, 25479374975 all hit this.

## Decision

Add `jitsubst.TryResolveSpec` (lenient variant of `ResolveSpec`) and call it from `runInfraPlan`
AFTER `parseInfraResourceSpecsForEnv` and AFTER `loadCurrentState`, BEFORE
`computePlanForInfraSpecs`. Mirror the same call in `applyInfraModules` for the apply path.

Substitution sources at plan time:

- `${VAR}` env-var refs: process env (`os.LookupEnv`).
- `${MODULE.field}` refs: `syncedOutputs` built from current `[]ResourceState`.
- `${SECRET_KEY}` where `SECRET_KEY` is in `cfg.Secrets.Generate` with `Type == "infra_output"` and
  `Source` resolves from current state: registered in a synthetic env-lookup closure.

`TryResolveSpec` differs from strict `ResolveSpec`:

1. Unresolved refs pass through verbatim (returns `unresolved []string` for diagnostics).
2. Malformed refs (`${.x}`, `${x.}`, `${}`) ARE hard errors — same as strict `ResolveSpec`.

**No `StateOutputSnapshot` field added to `interfaces.IaCPlan`.** The existing
`desiredStateHash(desired)` already captures every state-output value that resolved into the spec.
State-output drift between plan and apply ⇒ different hash ⇒ existing `plan stale: config hash
mismatch` error fires.

## Why this approach over alternatives

- **(A2) Auto-export `infra_output` into env at plan-prep**: rejected. Leaks state values into
  process env. Conflates state-output substitution with env-var typing. Doesn't help non-secret
  `${MODULE.field}` references.
- **(A3) Drop `secretGenKeys` preservation for `infra_output` types only**: rejected. Only fixes
  the secret-shaped case; non-secret `${MODULE.field}` references still produce literals at plan time.
- **(A4) Extend `ExpandEnvInMapPreservingVars`**: rejected. Bleeds IaC-state-aware concerns into a
  config-package primitive that is purely lexical today.

## Consequences

1. Spurious replace class disappears for cross-module references whose source is in state.
2. No new field on `IaCPlan`. Existing `DesiredHash` covers state-output drift detection.
3. Plan-time IO surface unchanged — resolver reuses the `loadCurrentState` result.
4. First-plan-no-state path unchanged: every `${MODULE.field}` ref stays unresolved → SchemaVersion=2
   → apply-time JIT (existing W-5 path).

## References

- `iac/jitsubst/jitsubst.go` — strict `ResolveSpec`, package docs explain the existing JIT contract.
- `iac/wfctlhelpers/apply.go::ApplyPlan` — apply-time JIT dispatch loop.
- `cmd/wfctl/infra_resolve_state.go` — implementation.
- core-dump decisions/0009 — canonical source for this decision.
