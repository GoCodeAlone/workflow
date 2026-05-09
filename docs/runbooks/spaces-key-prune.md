# Spaces Key Prune Runbook

When DigitalOcean Spaces accumulates leaked or stale access keys (post-leak
cleanup, periodic hygiene, scheduled rotation), use `wfctl infra
rotate-and-prune` to rotate the canonical credential AND prune older keys
in one atomic flow. Inspect-then-prune and recovery flows are documented
below for the cases where the all-in-one is not the right tool.

This runbook assumes:

- The workflow CLI is built from a release containing the `infra
  audit-keys` / `infra prune` / `infra rotate-and-prune` subcommands
  (`wfctl infra --help` lists all three).
- The DO plugin is loaded for the target env (`infra audit-keys` exits
  with `no loaded provider implements EnumeratorAll` if not).
- The operator has shell access where `WFCTL_CONFIRM_PRUNE=1` can be
  exported and `${WFCTL_STATE_DIR:-$HOME/.wfctl}` is writable.

## Overview

| Tool | Use when |
|------|----------|
| `infra audit-keys` | Read-only — list every cloud-side key for a `--type` to compare against IaC state. Always safe. |
| `infra prune` | Destructive — delete cloud-side keys older than `--created-before` except `--exclude-access-key`. Three-step opt-in (see below). |
| `infra rotate-and-prune` | Destructive — mint new credential, then prune older keys with the new one as exclusion target. Three-step opt-in + recovery file. |

Both destructive subcommands require **three** opt-ins to fire. The
first two are the **two-key** authorization (env var + flag — both
required, named after the two-person rule from physical security
contexts); the third is the runtime confirmation:

1. The `--confirm` flag on the CLI invocation (per-invocation consent).
2. `WFCTL_CONFIRM_PRUNE=1` exported in the environment (per-session
   authorization). Together with (1) this is the "two-key opt-in"
   referred to throughout the rest of this runbook + ADR 0017.
3. An interactive `y/N` prompt — skipped only with `--non-interactive`
   (CI workflows MUST opt in explicitly).

The `--exclude-access-key` flag is mandatory for `prune` (paranoia rail —
prevents a typo from nuking the live key); `rotate-and-prune` derives it
from the rotation result so it's auto-populated and not operator-provided.

## Happy path: rotate-and-prune

Recommended for routine hygiene + post-leak cleanup. One command, atomic
intent: the new credential is minted and persisted before any deletion
touches the cloud, and the recovery file is removed only after the prune
step succeeds.

```bash
export WFCTL_CONFIRM_PRUNE=1
wfctl infra rotate-and-prune \
  --type infra.spaces_key \
  --name coredump-deploy-key \
  --confirm
```

The flow is:

1. **Rotate** the canonical `coredump-deploy-key` credential — mints a
   new key on DO, stores `coredump-deploy-key_access_key` +
   `coredump-deploy-key_secret_key` as GH Secrets, then revokes the old
   credential at the upstream provider per ADR 0012.
2. **Persist** a recovery record at
   `${WFCTL_STATE_DIR:-$HOME/.wfctl}/last-rotation.json` (perms `0600`)
   BEFORE any deletion. The record contains
   `{type, name, access_key, created_at, source, rotated_at}`.
3. **List** every other `infra.spaces_key` resource in the account whose
   `created_at` is older than the new key's `created_at`.
4. **Prompt** for confirmation — skipped under `--non-interactive`.
5. **Delete** each older key via the DO API.
6. **Remove** the recovery file on full success. On any partial
   failure the recovery file is RETAINED (see "Recovery from partial
   failure" below).

Add `--preserve-names` to skip keys whose names match a regex even if
they're older than the cutoff (see "Preserving hand-created keys").

## Multi-step variant: audit then prune

Use this when:

- You want to manually inspect keys before any destructive call.
- The rotation step needs to run separately (e.g., the secrets backend
  requires elevated credentials only available in CI).
- You're recovering from a leak where the rotation already happened
  out-of-band (e.g., the credential was rotated through the DO console).

```bash
# Step 1: list all cloud-side keys (read-only, always safe).
wfctl infra audit-keys --type infra.spaces_key
```

The output is a fixed-width table with `NAME`, `ACCESS_KEY`,
`CREATED_AT` columns. Compare against IaC state (`wfctl infra outputs`)
to identify orphans.

```bash
# Step 2: rotate the canonical credential (separate command — no prune).
wfctl infra bootstrap --force-rotate SPACES --env staging
# Capture the stderr sidecar line — only sidecar metadata fields emit
# WFCTL_NEW_KEY_<UPPER>= markers (per the storage-filter contract,
# ADR 0020). The new credential's access_key is NOT in the stderr
# stream — it's stored as the SPACES_access_key GH Secret instead:
#   wfctl: rotated provider_credential SPACES (replaced existing value at <ts>)
#   WFCTL_NEW_KEY_CREATED_AT=<ts>
```

`access_key` is canonical credential data, not sidecar metadata, so
it goes through the same code path as `secret_key` (stored as a GH
Secret named `SPACES_access_key`, never logged). To recover it for
the prune step, pick whichever lookup matches your environment:

```bash
# Option A: read directly from the GH Secrets store where bootstrap put it.
gh secret view SPACES_access_key --repo <owner>/<repo>

# Option B: list cloud-side keys and identify the new one by created_at
# (the WFCTL_NEW_KEY_CREATED_AT= line you captured above).
wfctl infra audit-keys --type infra.spaces_key
# → grep the row whose CREATED_AT matches; its ACCESS_KEY column
#   is the value to pass as --exclude-access-key.
```

```bash
# Step 3: prune with the captured discriminator.
export WFCTL_CONFIRM_PRUNE=1
wfctl infra prune \
  --type infra.spaces_key \
  --created-before <ts> \
  --exclude-access-key <ak> \
  --confirm
```

If the rotation happened out-of-band (DO console, not bootstrap), get
both `access_key` and `created_at` from the `audit-keys` output for
the new key and use those values directly in step 3.

## Recovery from partial failure

If `rotate-and-prune` fails mid-flow — most commonly because the prune
step hit an API rate limit or transient network failure — the rotate
step has ALREADY succeeded (a new credential is live in the secrets
store) but some older keys remain. The recovery file at
`${WFCTL_STATE_DIR:-$HOME/.wfctl}/last-rotation.json` is RETAINED in
this case so you can finish the prune without re-rotating.

```bash
export WFCTL_CONFIRM_PRUNE=1
wfctl infra prune \
  --type infra.spaces_key \
  --recovery-from-last-rotation \
  --confirm
```

This reads the recovery file and applies the SAME discriminator
(`--created-before` + `--exclude-access-key`) the failed
`rotate-and-prune` would have applied. On success the recovery file is
deleted; on another failure it stays for the next attempt.

**Do NOT re-run `rotate-and-prune` directly to recover.** It would mint
yet another new credential — leaking another key to the audit log and
making the cleanup harder. The `--recovery-from-last-rotation` path is
the only correct recovery.

## Preserving hand-created keys

If your DO account has hand-created keys outside the IaC graph that
must be preserved regardless of age (e.g., a vendor integration key, a
contractor's access key with no managed lifecycle), pass
`--preserve-names` with a regex that matches their `name`:

```bash
wfctl infra rotate-and-prune \
  --type infra.spaces_key \
  --name coredump-deploy-key \
  --preserve-names '^manual-' \
  --confirm
```

Names matching the regex are skipped during the prune phase even if
their `created_at` is older than the cutoff. The regex is matched
against the resource `name` (cloud-side `name` field, not the
`access_key`).

`--preserve-names` is orthogonal to `--exclude-access-key`: the active
credential's access_key is always preserved, AND any name matching the
regex is preserved on top.

> **Why "preserve-names" and not "allowlist"?** On a destructive
> command the verb has to be unambiguous. `--allowlist '^manual-'`
> reads to some operators as "delete only manual-* keys" and to
> others as "preserve manual-* keys". The opposite mental models
> would yield opposite outcomes — one of them deleting every
> production key. `--preserve-names` only reads one way. Per ADR 0017.

## GH Secrets convention for managed `infra.spaces_key` resources

When you declare an `infra.spaces_key` module in `infra.yaml`:

```yaml
modules:
  - name: api-scoped-key
    type: infra.spaces_key
    config:
      name: api-scoped-key
      grants:
        - bucket: api-uploads
          permission: readwrite
```

The DO provider's `SpacesKeyDriver.Create` writes two GH Secrets at
provision time:

- `api-scoped-key_access_key`
- `api-scoped-key_secret_key`

Same naming convention as the canonical bootstrap path
(`bootstrapSecrets` for `provider_credential` types, ADR 0015). Module
names that already end in `_access_key` / `_secret_key` are caught by
the `R-A9` align rule (severity `ERROR`) so the doubled-create
anti-pattern can't reach `apply`.

## Frequently asked

**Q: Why two-key opt-in (env var + flag)?** Defense in depth against
a stuck-in-history shell line in someone's `~/.bash_history` running
under their UID. The env var is operator-provided per session; the
flag is operator-provided per invocation. Either alone is insufficient.
This is opt-ins (1) + (2) from the "Overview" three-step list; the
interactive y/N prompt (3) is the third gate, skippable only with
`--non-interactive`. ADR 0017.

**Q: What happens if I omit `--exclude-access-key`?** `prune` exits
with `prune: --created-before AND --exclude-access-key are both
required (paranoia rail)` and code 1. No cloud calls are made.

**Q: Can I run this against multiple types in one invocation?** No.
`--type` is single-valued. Run `prune` once per type if you have keys
from multiple resource types to clean up. (`audit-keys` is also single-
type for the same reason.)

**Q: Does the recovery file leak the secret value?** No. It stores
only `{type, name, access_key, created_at, source, rotated_at}`. The
`secret_key` is in the GH Secrets store, never on disk. The file is
written `0600` to be doubly safe.

**Q: What if the secrets store rejects the new credential write
mid-rotation?** `bootstrapSecrets` returns the error before the old
credential is revoked, so the system stays on the old key. Re-run the
command after fixing the secrets store; nothing was leaked or lost.
