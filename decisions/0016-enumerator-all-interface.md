# 0016: EnumeratorAll interface for non-tag-supporting resources

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

The existing `interfaces.Enumerator` interface (added in earlier
plans for `wfctl infra cleanup --tag`) returns `[]ResourceRef` for
resources matching a tag query. It works for resource types where the
cloud API exposes a per-resource tag field that can be queried server-
side: DO Droplets, DO Volumes, DO Domains, AWS EC2 instances, etc.

Several resource types do NOT support tagging at all — DO Spaces
access keys are the canonical example. The DO API exposes `name`,
`access_key`, `created_at` on each key but no tag field. Building
`audit-keys` / `prune` against `Enumerator.EnumerateByTag` would have
required either:

- A spurious tag-querying step that returns "tags unsupported for
  resource type" — operators would have to know which types support
  tags before invoking.
- A per-type fallback path inside the CLI that branches between
  Enumerator + a hand-rolled list call.

Both options leak provider-API quirks into the CLI layer.

## Decision

Add a new optional interface `interfaces.EnumeratorAll`:

```go
type EnumeratorAll interface {
    EnumerateAll(ctx context.Context, resourceType string) ([]*ResourceOutput, error)
}
```

Providers that can list resources of `resourceType` regardless of tag
implement this interface. The DO provider implements it for
`infra.spaces_key`. Future providers can implement it for any type
whose API supports listing without filter.

Returns `[]*ResourceOutput` (full metadata) rather than `[]ResourceRef`
(name + type + provider_id) because audit + prune both need the
metadata to render / filter — and the DO List API returns the full
shape in one call, so re-Reading would be wasteful.

CLI dispatchers type-assert at the boundary; providers that don't
implement it surface a structured error (`audit-keys: no loaded
provider implements EnumeratorAll`) rather than crashing.

## Consequences

- Existing `Enumerator` (tag-based) is unchanged. New interface is
  purely additive — older provider plugins continue to work without
  modification.

- DO provider (`workflow-plugin-digitalocean`) implements
  `EnumerateAll` for `infra.spaces_key` with transparent pagination
  (DO's `/v2/spaces/keys` API page-size limits). Per-page calls happen
  inside the implementation; callers see one slice.

- The CLI dispatchers (`runInfraAuditKeysCmd`,
  `runInfraPruneCmd`, `runInfraRotateAndPruneCmd`) iterate loaded
  iac.provider modules and pick the first one implementing
  `EnumeratorAll`. Multi-provider configs need to ensure at most one
  provider per `--type`.

- Returning `[]*ResourceOutput` ties the interface to the
  `ResourceOutput` shape (`Outputs map[string]any`). Providers must
  populate the `Outputs` map with the fields callers expect — for
  spaces keys: `name`, `access_key`, `created_at`. This is the
  documented contract.

## Alternatives considered

- **Add `EnumerateAll` to the existing `Enumerator` interface** as a
  second method. Rejected because it would force every existing
  Enumerator implementer to grow a second method or stop satisfying
  the interface — backwards-incompatible.

- **Return `[]ResourceRef` from `EnumerateAll`**. Rejected because
  audit + prune need the metadata; forcing callers to round-trip back
  to `Read` for every result would double the API calls.

- **Centralize listing in `wfctl` core** (manage list-by-type as a
  generic operation against `iac.state` only). Rejected because
  state-only listing misses resources that exist in the cloud but not
  in state — exactly the orphan keys this plan is built to find.

## Related

- `interfaces/iac_provider.go` (commit c8e4c4e8) — `EnumeratorAll`
  declaration.
- `workflow-plugin-digitalocean/internal/providers/digitalocean.go`
  — DO implementation with pagination.
- `cmd/wfctl/infra_audit_keys.go`, `cmd/wfctl/infra_prune.go` — CLI
  consumers.
- ADR 0015 (spaces-key as IaC resource — this interface enables the
  audit + prune CLIs needed to operationalize it).
