# 0018: Bootstrap key exemption from `infra.spaces_key`

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

ADR 0015 introduced `infra.spaces_key` as a first-class IaC resource
type so purpose-scoped DO Spaces credentials can participate in
plan/apply/drift like any other resource. The natural follow-on
question is: should the *bootstrap key* — the canonical credential
that `bootstrapSecrets` mints before any plan runs — also be modeled
as an `infra.spaces_key` resource?

Migrating the bootstrap key to `infra.spaces_key` would unify the two
code paths (`bootstrapSecrets` and the IaC driver) and let drift
detection catch out-of-band rotations of the bootstrap credential.

But the bootstrap key has two characteristics that make it
fundamentally different from purpose-scoped keys:

1. **Chicken-and-egg with state**: `bootstrapSecrets` must run
   *before* the IaC state backend is loaded (in many configs, the
   state backend itself is hosted on DO Spaces — accessing it
   requires the bootstrap key). If the bootstrap key were managed by
   `infra.spaces_key`, the IaC subsystem would need that key to load
   state, but the key wouldn't be in state until apply ran. Circular.

2. **Lifecycle separation**: the bootstrap key is rotated by
   `wfctl infra bootstrap --force-rotate <NAME>` — a flow that
   reaches into the upstream provider (DO API) directly, mints a new
   key, then revokes the old one via
   `interfaces.ProviderCredentialRevoker` (ADR 0012). It does NOT go
   through the resource driver Plan/Apply cycle. Forcing it through
   the IaC pipeline would either require reimplementing the rotation
   primitive there, or having two competing rotation paths that
   could disagree.

## Decision

The bootstrap key is **explicitly NOT** an `infra.spaces_key`
resource. It remains managed by the canonical bootstrap path
(`bootstrapSecrets` + `--force-rotate`) and lives outside the IaC
graph. The `wfctl infra audit-keys` CLI surfaces it (because it
appears in the cloud account's key list), so operators can SEE it
during an audit, but `wfctl infra prune` will skip it as long as
operators specify the canonical key's `access_key` in
`--exclude-access-key`.

This carve-out is explicitly documented in:

- The R-A9 align rule (which fires `ERROR` only on `provider_credential`
  entries that violate the canonical-shape contract — bootstrap keys
  always satisfy it).
- The runbook (`docs/runbooks/spaces-key-prune.md`) — operators are
  reminded to exclude the bootstrap key's access_key when running
  `prune` against `infra.spaces_key` if they have one in their
  account.
- The plan's "Out of scope" section — bootstrap-key rotation reaper
  deferred to ADR 0019 (future plan).

## Consequences

- Two code paths exist for spaces-key lifecycle: the canonical
  bootstrap path (for the bootstrap key) and the new
  `SpacesKeyDriver` (for purpose-scoped keys). They share the GH
  Secrets naming convention (`<NAME>_access_key` / `<NAME>_secret_key`)
  so downstream consumer code is identical.

- Drift detection does NOT cover the bootstrap key. If an operator
  rotates the bootstrap credential through the DO console, IaC
  doesn't notice. This is acceptable because the credential is
  required to load state at all — any out-of-band rotation that breaks
  this is operationally visible immediately.

- A future plan (ADR 0019, deferred) can add a bootstrap-key rotation
  reaper that watches for stale keys + reports them via a separate
  CLI surface. Out of scope for this plan.

- Operators running `wfctl infra audit-keys --type infra.spaces_key`
  will see the bootstrap key in the output. This is intentional —
  visibility is good even when management is out-of-band. The runbook
  notes this so they don't try to delete it.

## Alternatives considered

- **Migrate bootstrap key to `infra.spaces_key`**: rejected for the
  chicken-and-egg + lifecycle reasons above. Would require either
  bootstrapping a state backend without managed credentials (defeats
  the bootstrap workflow's purpose) or two competing rotation paths.

- **Block the bootstrap key from `audit-keys` output entirely** (so
  operators don't see it): rejected. Visibility is good even when
  the key is out-of-IaC-scope. Hiding it would create a different
  failure mode where an operator forgets it exists.

- **Track the bootstrap key in `infra.yaml` as a `data` source** (a
  read-only IaC reference): rejected as overengineering for the
  current need. Revisit when ADR 0019 lands.

## Related

- ADR 0012 — Provider credential rotation (the primitive
  `bootstrapSecrets --force-rotate` uses).
- ADR 0015 — Spaces key as IaC resource (introduces the resource
  type this ADR carves out from).
- ADR 0019 (deferred) — Bootstrap-key rotation reaper.
- `docs/runbooks/spaces-key-prune.md` — runbook with the
  bootstrap-key carve-out called out.
