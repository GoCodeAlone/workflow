# 0022: Spaces-key plan Tasks 5+6 land as no-op confirmation

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

The spaces-key-iac-resource plan
(`docs/plans/2026-05-08-spaces-key-iac-resource.md`, commit `316559f7`)
defined PR2 (Tasks 5+6) as a migration from a two-entry
`provider_credential` schema to a single canonical entry in
`core-dump/infra.yaml`. The plan's `### Task 6:` section showed the
expected BEFORE state:

```yaml
secrets:
  generate:
    - key: SPACES_access_key
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
    - key: SPACES_secret_key
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
```

Task 5 ("Failing test") was supposed to capture that
`wfctl infra align --strict` on this shape would fail with
`ERROR R-A9: provider_credential key "SPACES_access_key"...` after
PR1's R-A9 severity flip (commit `288f68d7`, merged in PR #583).
Task 6 was supposed to migrate the file to the canonical
single-entry shape and re-run `--strict` to confirm exit 0.

When Task 5 implementation was claimed, the implementer built `wfctl`
from PR1's branch and ran `--strict` against the real
`core-dump/infra.yaml` at `origin/main` HEAD `3bb46833`:

```
$ /tmp/wfctl infra align --strict -c infra.yaml --env staging
## wfctl infra align

No alignment issues found.
$ echo $?
0
```

Direct read of `core-dump/infra.yaml` at the same SHA confirmed the
file already uses the canonical single-entry pattern at lines 32-36:

```yaml
secrets:
  provider: github
  config:
    repo: GoCodeAlone/core-dump
    token_env: GH_MANAGEMENT_TOKEN
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
```

The `${SPACES_access_key}` and `${SPACES_secret_key}` references at
`infra.yaml:108-109` and `:120-121` are **module-config interpolations
of bootstrapSecrets-derived sub-keys** — i.e. the canonical
auto-derive pattern (per `providerCredentialSubKeys["digitalocean.spaces"]`),
not entries in `secrets.generate`. So R-A9 has nothing to fire on.

Git log shows the original migration likely landed in PR #190
(TC1 cutover, commit `3cb544a1`) or PR #194 (TC2 cutover,
commit `9d1cadf5`) — well before this plan was authored on
2026-05-08. The plan's premise was stale by the time it was locked.

The implementer paused before committing any "migration" and
escalated to the team lead, who in turn surfaced the mismatch to the
user (the plan-locked manifest required explicit unlock for any
rescope).

## Decision

**Tasks 5+6 land as a no-op confirmation.** No code change in
`core-dump/infra.yaml`. PR2 is closed without a code commit; this
ADR is the durable record of the rationale. The associated
TaskList entries (#5, #6 implement; #33, #34 spec-review;
#59, #60 quality-review) are marked completed.

PR1's R-A9 hardening (now merged in workflow main) provides the
actual regression protection going forward: anyone reintroducing
the two-entry pattern in any `infra.yaml` will hit
`ERROR R-A9: provider_credential key %q ends in %q; use canonical %q
(auto-derives sub-keys via providerCredentialSubKeys[%q])` from
`wfctl infra align --strict`, exit code 1, before the bad shape
ever touches `wfctl infra plan` or apply.

## Consequences

**Positive:**

- The plan-reality check at impl-time caught the mismatch before
  any fabricated "migration" diff was committed. The strict-
  interpretation invariant ("Locked plans are inviolate. If the
  user phrase appears to conflict... the locked manifest wins
  until the user goes through the unlock path") worked as
  designed: ambiguity was resolved upward, not sideways.
- PR1's R-A9 severity flip (Tasks 3+4) is the load-bearing
  protection. PR1 + PR0 (audit-secrets CLI) together cover the
  attack surface that Tasks 5+6 were originally going to fix
  manually — at lint time, going forward, for any caller.
- One less PR to merge, review, and CI-gate.

**Negative:**

- This ADR is the only durable evidence that Tasks 5+6 were ever
  considered. Future contributors auditing the spaces-key plan
  will need to consult this file to understand why the manifest
  has Tasks 5+6 but no PR2 in the merge history.
- The plan rev3-rev6 history doesn't reflect this resolution.
  Rather than re-rev the plan post-merge (per
  `superpowers:scope-lock`'s rules), this ADR is the closeout.

## Alternatives considered

**1(b) — Replace Tasks 5+6 with a CI regression-sentinel.** A
machine-readable test in `core-dump/.github/workflows/` that runs
`wfctl infra align --strict` on every PR and fails CI if R-A9 ever
fires. **Rejected:** PR1's R-A9-as-error change already provides
this protection at lint time during normal `wfctl infra align`
invocations (which `core-dump/.github/workflows/deploy.yml` already
runs). Adding a separate sentinel duplicates coverage and adds
maintenance burden without buying additional safety. Out-of-band:
if the user later wants `--strict` enforcement at PR-CI time
specifically (rather than just at deploy time), that's a one-line
addition to the existing workflow, no plan task needed.

**1(c) — Look for a different config that still has the broken
shape.** An exhaustive grep across all `*.yaml` in the org's
repos. **Rejected:** verified `core-dump/infra.yaml` is the only
matching config in scope of this plan (the plan's Task 6 explicitly
names `core-dump/infra.yaml:9-46` as the migration target);
expanding scope mid-plan violates the scope-lock contract. If
other repos turn up with the broken pattern, they'll be caught
by R-A9 ERROR on their next `wfctl infra align` run — same
mechanism, no special-casing needed.

**2 — Unlock the plan and rev to drop Tasks 5+6.** Per
`superpowers:scope-lock`, an authorized scope reduction. **Rejected
in favor of (1a):** the plan's PR Grouping table treats PR2 as a
named deliverable; rather than mutating that, this ADR records the
one-line resolution ("PR2 is the no-op, see ADR 0022") and the plan
remains the canonical record of the original intent.

## Lessons

**Planner blind spot:** the design phase assumed
`core-dump/infra.yaml`'s shape based on operator memory rather than
re-verifying against `origin/main` HEAD at plan-write time. The
plan's Task 6 BEFORE-state YAML block was authored verbatim from
that memory.

**Mitigation for future plans (added to retro):** before locking a
plan that mutates files in another repo, run a freshness check —
`git fetch origin main && grep -A 10 '<target-block>' <file>` — and
paste the actual BEFORE state into the plan. If the BEFORE state
differs from the plan's assumed shape, surface the mismatch in the
adversarial-design-review or alignment-check phase, not at
implementation time.

A subtler point: the team-lead initially misread this same file
on a stale local branch (`fix/post-cleanup-copilot-comments`) and
asserted the broken shape was still there. Two implementers (lead
+ me) independently failed to fetch origin/main before reading.
Add to the retro: **operator memory about file shapes is unreliable
at the day-scale — always re-fetch before claiming what's at HEAD.**

## Related

- Plan: `docs/plans/2026-05-08-spaces-key-iac-resource.md` (commit
  `316559f7`), Tasks 5+6 in PR Grouping table.
- ADR 0015: spaces-key as IaC resource (overall trade study; ADR 0022
  is the closeout for one phase of that plan that turned out to be
  pre-resolved).
- PR1 (workflow #583, merged): R-A9 severity flip — the ongoing
  regression protection that supersedes Tasks 5+6's one-time fix.
- PR0 (workflow #581, merged): `wfctl infra audit-secrets` CLI — the
  proactive companion check.
- `core-dump` PR #190 (TC1 cutover, commit `3cb544a1`) and PR #194
  (TC2 cutover, commit `9d1cadf5`) — the original migrations of
  `core-dump/infra.yaml` to canonical single-entry shape, predating
  this plan.
- `wfctl infra align --strict` rule R-A9: `cmd/wfctl/infra_align_rules.go`
  function `checkRA9`.
