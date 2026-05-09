# 0020: Storage filter for sidecar metadata in `bootstrapSecrets`

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

The DO Spaces credential creation API
(`POST /v2/spaces/keys`) returns a JSON object with at least three
useful fields:

```json
{
  "access_key": "DO00...",
  "secret_key": "...",
  "created_at": "2026-05-08T11:00:00Z"
}
```

Pre-this-plan, `generateDOSpacesKey` returned only the `access_key` +
`secret_key` (concatenated as a JSON string), and `bootstrapSecrets`
JSON-decoded that string + stored each key as a separate GH Secret
named `<KEY>_<subkey>` (e.g., `SPACES_access_key`, `SPACES_secret_key`).

The `wfctl infra prune` design (ADR 0017) needs the rotation result
to include `created_at` so the time-based discriminator
(`--created-before`) can be derived without a second API call. The
natural fix: have `generateDOSpacesKey` return ALL three fields
(`access_key` + `secret_key` + `created_at`) and let
`bootstrapSecrets` extract `created_at` from the result.

The naive fix is dangerous, though. `bootstrapSecrets`'s storage
loop iterates EVERY field in the JSON map and writes each as a GH
Secret named `<KEY>_<field>`. Adding `created_at` would create a
phantom `SPACES_created_at` GH Secret on every bootstrap run — a
shape that makes no sense semantically (`created_at` is metadata,
not a credential), pollutes the audit-keys / prune output (the
phantom secret has no corresponding cloud key), and confuses
downstream consumers expecting only `_access_key` / `_secret_key`.

## Decision

Make two changes together (must land in the same merge commit per
this ADR's "same-commit constraint"):

1. `generateDOSpacesKey` returns a JSON map with `access_key` +
   `secret_key` + `created_at` (full DO API response shape, not just
   the credential pair).

2. `bootstrapSecrets` storage loop **filters** the JSON map by the
   `providerCredentialSubKeys[source]` allow-list before writing GH
   Secrets. For `digitalocean.spaces`, that's
   `["access_key", "secret_key"]` — `created_at` is silently ignored
   at the storage layer.

3. `created_at` is surfaced to operators via stderr sidecar emission
   (`WFCTL_NEW_KEY_CREATED_AT=<ts>` line printed alongside the
   existing rotation log) so the multi-step prune workflow can
   capture it from the bootstrap output.

4. `RotationResult` (the in-process return shape) includes
   `CreatedAt` so the all-in-one `rotate-and-prune` doesn't need to
   parse stderr — it gets the metadata directly.

The "same-commit constraint" is critical: shipping (1) without (2)
creates a phantom GH Secret on every bootstrap; shipping (2) without
(1) is a no-op (nothing to filter). Splitting them across commits
would leave main in a broken state for any window between merges.

## Consequences

- All future provider_credential generators that return additional
  metadata (DO Spaces precedent) MUST be filtered by the per-source
  sub-key allow-list. New providers register their canonical sub-keys
  in `providerCredentialSubKeys` (`cmd/wfctl/infra_bootstrap.go`).

- `audit-keys` / `prune` for spaces keys can rely on `created_at`
  being available in `ResourceOutput.Outputs` (via `EnumerateAll`)
  AND in `RotationResult.CreatedAt` (in-process) — no second API
  call needed.

- The stderr sidecar (`WFCTL_NEW_KEY_CREATED_AT=<ts>`) is part of the
  documented contract for the multi-step variant of the prune
  workflow (see `docs/runbooks/spaces-key-prune.md`). Format
  prefixed with `WFCTL_` so operators can `grep`/parse reliably.

- Backward compatibility: existing canonical-bootstrap consumers
  (configs that already use single-entry `secrets.generate` shape)
  continue to work without change. The storage filter is invisible
  to them — they only ever stored `_access_key` + `_secret_key`
  before, and that's still all they get.

- Rejected approach (sidecar return as a separate `[]Sidecar`
  parameter on `secrets.GenerateSecret`) would have changed the
  public package signature. This filter approach keeps the public
  signature stable.

## Alternatives considered

- **Sidecar return parameter** on `secrets.GenerateSecret` (a new
  `[]Sidecar` slice for non-credential metadata). Rejected because
  it changes the public package signature, breaking every downstream
  consumer of `secrets.GenerateSecret`. The storage-filter approach
  achieves the same result without ABI churn.

- **Filter at the `generateDOSpacesKey` callsite** instead of in
  `bootstrapSecrets`. Rejected because it would force every future
  provider_credential generator to remember to filter — putting the
  filter in `bootstrapSecrets` makes it the system's invariant, not
  a per-generator obligation.

- **Don't return `created_at` at all** from `generateDOSpacesKey` and
  re-fetch via a separate `Read` call when prune needs it. Rejected
  for the second-API-call cost and for opening a TOCTOU window
  between rotation and prune.

## Related

- ADR 0012 — Provider credential rotation (the rotation primitive
  this metadata flows from).
- ADR 0017 — `wfctl infra prune` UX (the consumer of `created_at`).
- `cmd/wfctl/infra_bootstrap.go` — `bootstrapSecrets` storage loop +
  `providerCredentialSubKeys` map.
- `secrets/generators.go` — `generateDOSpacesKey` return shape.
- ADR 0021 — `rewriteTransport` (test stubbing fix landed in PR4a;
  same area but unrelated decision).
