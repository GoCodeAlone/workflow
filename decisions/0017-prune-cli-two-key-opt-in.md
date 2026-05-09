# 0017: `wfctl infra prune` UX — discriminator + two-key opt-in

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

`wfctl infra prune` is destructive — it deletes cloud resources by
calling the provider's `ResourceDriver.Delete`. The user it serves
most often is post-leak cleanup: an operator has rotated a credential
and needs to delete every older key so the leaked one stops being
accepted. This is a high-stakes operation; a typo can nuke the live
key and cause downtime.

The design considered three discriminator shapes:

- **Name regex** (`--allowlist '^manual-'` style as the primary
  filter): rejected by adversarial review #2. A regex protects
  *everything matching the regex*, including any leaked key whose
  name happens to match. The leak surface is exactly what we're
  trying to remove.

- **Per-key tag**: rejected because spaces keys don't support tags
  (motivated ADR 0016).

- **Time + access_key** (this ADR): only resources whose
  `created_at` is OLDER than `--created-before` are eligible, AND the
  resource whose `access_key` matches `--exclude-access-key` is always
  preserved.

The time + access_key shape pairs naturally with the rotation
primitive: rotating a credential mints a new key with a fresh
`created_at` and a known `access_key`. Pass those values and the
filter is unambiguous: "everything older than the new key, except
the new key itself".

The opt-in shape considered three levels:

- **Just `--confirm` flag** (single per-invocation): too easy to
  recover from `~/.bash_history`.
- **Just env var**: too easy to ship in a Makefile / CI step.
- **Both required (this ADR)**: the operator must export the env var
  AND pass the flag, so neither a stale shell history line nor a
  misconfigured Makefile alone is enough to fire.

## Decision

`wfctl infra prune` requires:

1. `--type <T>` (required) — single resource type per invocation. Rejects
   if missing.
2. `--created-before <RFC3339>` (required) — only resources older than
   this are eligible.
3. `--exclude-access-key <AK>` (required) — this access_key is preserved
   no matter what (paranoia rail).
4. `--confirm` flag — explicit per-invocation consent.
5. `WFCTL_CONFIRM_PRUNE=1` environment variable — two-key authorization.
6. Interactive `y/N` prompt — skipped only with `--non-interactive`.

If ANY of (1)-(5) is missing, prune exits with a structured error and
NO cloud calls are made.

`--allowlist <regex>` is offered as a secondary, additive filter — it
preserves resources whose `name` matches the regex on top of the
`--exclude-access-key` exclusion.

`--recovery-from-last-rotation` is the recovery-mode shortcut: reads
the discriminator from the recovery file written by `infra
rotate-and-prune` (see ADR for that flow) so an operator who just had
a partial-failure rotation can finish cleanup without manually
copy-pasting timestamps + access_keys.

## Consequences

- Operators must capture the rotation result before pruning. The
  `infra rotate-and-prune` all-in-one command does this automatically;
  the multi-step variant requires reading stderr after `infra
  bootstrap --force-rotate`.

- The two-key opt-in adds one extra step (`export
  WFCTL_CONFIRM_PRUNE=1`) for routine operator use. Acceptable
  friction for a destructive operation. CI workflows that need to
  prune (e.g., scheduled hygiene jobs) export the env var explicitly.

- The feature flag this ADR codifies (`WFCTL_CONFIRM_PRUNE`) has a
  retirement milestone: v0.28.0 makes it always-on (effectively
  removing the env var step) IF operational telemetry shows zero
  unintended fires for 60 days post-release. Tracked in plan rev3 as
  the "feature-flag retirement" sub-task; not in this ADR's scope.

- A typo in `--exclude-access-key` is caught by the value-equality
  filter — if the typo'd value doesn't match any real key, every
  eligible key (including the live one) is targeted. This is why the
  interactive `y/N` prompt exists as the third opt-in: the operator
  reviews the list of keys-to-be-deleted before confirming.

## Alternatives considered

- **Soft-delete via tag-and-cron**: rejected because DO Spaces keys
  don't support tags + cron-based delayed-delete adds operational
  state we don't want.

- **Allowlist-only filter** (no `--exclude-access-key`): rejected per
  adversarial review #2. The rotation use case needs an *exclusion*
  semantic (preserve N, delete N-1), not an *inclusion* semantic
  (delete N matching the regex). Inclusion-by-regex is dangerous
  because typos default to "match nothing" which is safe; exclusion-
  by-regex defaults to "match everything" which deletes the live key.

- **Single opt-in (env var XOR flag)**: rejected. Either alone is
  insufficient defense in depth — env vars persist in shell history
  + CI logs; flags persist in shell history.

## Related

- `cmd/wfctl/infra_prune.go` — implementation.
- `cmd/wfctl/infra_prune_test.go` — `TestInfraPrune_RequiresTwoKeyOptIn`,
  `TestInfraPrune_RequiresExcludeAccessKey`,
  `TestInfraPrune_FiltersByTimeAndExcludesAccessKey`.
- `docs/runbooks/spaces-key-prune.md` — operator-facing runbook.
- ADR 0015 (two-phase plan), ADR 0016 (EnumeratorAll — what prune
  enumerates against).
