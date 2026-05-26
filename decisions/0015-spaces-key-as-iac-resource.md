# 0015: Spaces key as IaC resource — two-phase fix (canonical-schema + Hybrid resource)

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

DigitalOcean Spaces access keys (`access_key` + `secret_key` pairs) are
provisioned and stored by the canonical bootstrap path
(`bootstrapSecrets` in `cmd/wfctl/infra_bootstrap.go`) when a config
declares a `secrets.generate` entry of `type: provider_credential` with
`source: digitalocean.spaces`. The bootstrap layer mints the key via
`generateDOSpacesKey` (in `secrets/generators.go`), then stores the two
sub-keys (`<KEY>_access_key`, `<KEY>_secret_key`) as GitHub Actions
Secrets per `providerCredentialSubKeys`.

Two failure classes had accumulated:

1. **The doubled-create anti-pattern** — operators sometimes wrote two
   separate `secrets.generate` entries (`SPACES_access_key` AND
   `SPACES_secret_key`), each `type: provider_credential`, each with
   the same `name`. Each entry triggered a separate cloud credential
   creation; the second silently orphaned the first's access_key. After
   N CI runs, the DO account accumulated N orphaned keys (the leak
   surface that motivated this plan).

2. **No IaC lifecycle for purpose-scoped keys** — when downstream code
   needed a credential scoped to a single bucket (e.g., a customer-
   data export job), there was no way to declare it in `infra.yaml` as
   a regular IaC resource alongside the bucket. Operators had to mint
   one out-of-band and paste the access_key into a secret, which
   defeated drift detection + state correlation.

The design considered two approaches:

- **Approach A**: Pure schema correction. Block the doubled-create
  anti-pattern at lint (R-A9), rename existing-broken configs to the
  canonical single-entry shape, leave purpose-scoped keys to the
  out-of-band path. Smallest change; doesn't address (2).

- **Approach B**: Two-phase fix. Phase 1 = same as A (block + migrate);
  Phase 2 adds a new `infra.spaces_key` IaC resource type with its own
  `ResourceDriver.Create/Read/Update/Delete` so purpose-scoped keys
  enter state and participate in plan/apply/drift like any other
  resource. Larger change; addresses both failure classes.

## Decision

Take **Approach B** — two-phase fix. Phase 1 ships the schema
correction (R-A9 severity flip + canonical-schema migration of broken
configs + storage-filter fix for sidecar metadata). Phase 2 ships
`infra.spaces_key` as a first-class IaC resource type via a new
`SpacesKeyDriver` in `workflow-plugin-digitalocean`.

The two phases are sequenced: Phase 1 lands first (PR0+PR1+PR2+PR4a in
this plan), then Phase 2 (PR4b+PR5+PR6) once a workflow tag containing
Phase 1 is cut.

## Consequences

- The doubled-create anti-pattern is now blocked at `wfctl infra
  align --strict` with severity `ERROR` (always exits non-zero, not just
  under `--strict`). See ADR for R-A9 (in plan rev3).

- A new resource type `infra.spaces_key` exists. Its driver writes the
  two GH Secrets at Create time using the same naming convention
  (`<NAME>_access_key`, `<NAME>_secret_key`) so downstream code that
  references `${NAME_access_key}` works identically whether the key
  came from canonical bootstrap or the new IaC driver.

- The bootstrap key (the canonical credential `bootstrapSecrets` mints
  before any plan runs) is **explicitly NOT** an `infra.spaces_key` —
  it lives outside IaC for chicken-and-egg reasons (state-backend
  creds need to exist before state can be loaded). See ADR 0018.

- Drift detection for purpose-scoped keys is now meaningful: if an
  operator deletes a key out-of-band, `infra drift` flags it.

- The new resource type implies an `EnumeratorAll` interface on the DO
  provider (so `wfctl infra audit-keys` / `prune` can find every key
  in the account, not just those in state). See ADR 0016.

## Alternatives considered

- **Approach A (pure schema correction)**: rejected because it leaves
  purpose-scoped keys in the out-of-band path. Operators would still
  hand-paste access_keys into secrets; drift detection would silently
  miss orphans.

- **Single-phase fix (combine 1 + 2 in one PR)**: rejected because the
  blast radius is too large for one review pass. The schema correction
  has security urgency (block the leak vector); the IaC driver is
  additive and can take a second review cycle.

- **Tag-based key correlation** (use DO's per-key tag field to match
  state): rejected by adversarial review #2 — DO tags overload with
  Spaces *bucket* tags and cause noise in cleanup queries; the DO API
  doesn't enforce uniqueness on the tag namespace.

## Related

- `docs/plans/2026-05-08-spaces-key-iac-resource-design.md` — full
  design doc with the Approach A vs B trade study.
- `docs/plans/2026-05-08-spaces-key-iac-resource-design.adversarial-review-1.md`
  through `-7.md` — review history that converged on Approach B.
- ADR 0016 (EnumeratorAll), ADR 0017 (prune CLI two-key opt-in),
  ADR 0018 (bootstrap-key exemption), ADR 0020 (storage-filter sidecar
  metadata).
