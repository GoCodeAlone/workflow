# 0014: JIT plan-time resolver substitutes infra_output only

- **Date:** 2026-05-06
- **Status:** Accepted

## Context

PR #576 (ADR 0013) shipped `jitsubst.TryResolveSpec` as the plan-time lenient resolver. It substitutes
`infra_output`-typed secret refs from state, and `${MODULE.field}` refs from `syncedOutputs`. For all
other `${VAR}` env-var references not in `resolvedSecrets`, `planTimeEnvLookup` fell through to
`os.LookupEnv`.

TC2 cutover run 25533331078 exposed a bug in this fallthrough: `NATS_AUTH_TOKEN` is declared in
`secrets.generate` with `type: random_hex, length: 32` (a runtime-generated secret bootstrapped and
stored as a GitHub Secret). The `tc2-cutover.yml` workflow injects it as an env var for each step. At
plan time, `os.LookupEnv("NATS_AUTH_TOKEN")` returns the actual 64-char hex token and substitutes it
as a literal value in the plan. Security-check R4 (`ruleR4SecretShapeLiteralInEnvVars`) then flags it:

```
R4 | FAIL | coredump-staging (infra.container_service) |
env_vars["NATS_AUTH_TOKEN"]: potential secret literal in env var "NATS_AUTH_TOKEN" — use ${VAR} references instead
```

R4 already skips values containing `${` — the guard works correctly for unresolved templates. The bug
is that the template was resolved to its literal value before R4 saw it.

## Decision

Scope `planTimeEnvLookup` to block `os.LookupEnv` for keys declared in `secrets.generate` with any
type other than `infra_output`. These "runtime-only" keys must remain as `${VAR}` template references
in the plan.

Implementation:

1. `buildRuntimeOnlySecretKeys(cfg)` — returns the set of `SecretGen.Key` values whose `Type !=
   "infra_output"`. Called once in `resolveSpecsAgainstState`.
2. `planTimeEnvLookup(resolvedSecrets, runtimeOnlyKeys)` — updated signature. If a key is in
   `runtimeOnlyKeys`, returns `("", false)` instead of calling `os.LookupEnv`. This leaves the
   `${VAR}` template intact in the plan output.

The `runtimeOnlyKeys` blocklist is logically equivalent to "secrets managed by a provider store that
are injected at runtime". Any type that is not `infra_output` belongs here:

- `random_hex`, `random_base64`, `random_alphanumeric` — generated at bootstrap, stored as GitHub
  Secrets, injected into DO App Platform as `SECRET`-typed env vars at apply time.
- `provider_credential` — provider-managed credentials. Their literal value must not appear in plans.

## Why this approach

- **Minimal blast radius**: only `infra_resolve_state.go` changes. `jitsubst.TryResolveSpec` is
  unchanged — it correctly treats any env-var reference where `envLookup` returns `false` as
  "unresolved, pass through verbatim".
- **Config-driven**: the blocklist derives directly from `secrets.generate`. No hardcoded key names,
  no regex matching, no second pass over the plan.
- **Deterministic**: whether the secret is currently set in the environment is irrelevant — the
  blocklist is structural (based on the declared type in config), not environmental.
- **Backward compatible**: existing `infra_output` resolution is unchanged. `${STAGING_VPC_UUID}` and
  similar keys still collapse to literals at plan time (which is the ADR-0013 correctness property).

## Consequences

1. Security-check R4 no longer flags `random_*` / `provider_credential` secrets in env_vars that are
   declared in `secrets.generate` — they remain `${VAR}` templates in the plan.
2. Apply-time JIT (`jitsubst.ResolveSpec`, strict) substitutes these references from the live env at
   apply time — unchanged behavior.
3. DO App Platform's `AppPlatformDriver` receives the template string `${NATS_AUTH_TOKEN}` at plan
   time (for Diff) and the resolved value at apply time. Because Diff is plan-time-only, the driver
   sees consistent config between plan and apply once apply-time JIT runs.
4. `desiredStateHash` (plan-time) hashes the template, not the literal. The apply-time hash is
   computed from the post-JIT spec. If plan-time and apply-time environments differ for a runtime-only
   secret, the hash mismatch fires — this is correct: plan staleness should be detected.

## References

- TC2 cutover run 25533331078 — surfaced the R4 failure.
- `cmd/wfctl/infra_resolve_state.go` — `buildRuntimeOnlySecretKeys`, `planTimeEnvLookup`.
- `iac/jitsubst/jitsubst.go` — `TryResolveSpec` (lenient resolver; unchanged by this ADR).
- ADR 0013 — original JIT plan-time resolver design (infra_output substitution rationale).
- security-check R4 (`ruleR4SecretShapeLiteralInEnvVars`) — the rule this fix addresses.
- core-dump `tc2-cutover.yml` — injects `NATS_AUTH_TOKEN` as env var; caused os.LookupEnv hit.
