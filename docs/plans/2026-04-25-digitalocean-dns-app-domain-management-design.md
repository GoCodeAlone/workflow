---
status: in_progress
area: wfctl
owner: workflow
implementation_refs: []
external_refs:
  - buymywishlist.com
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl ./module -count=1
  result: passing
supersedes: []
superseded_by: []
---

# DigitalOcean DNS And App Domain Management Design

Date: 2026-04-25

## Goal

Make `buymywishlist.com` manageable through Workflow and wfctl without manual
DigitalOcean console steps. If the DNS zone or records already exist in
DigitalOcean, wfctl should discover and adopt that live state before applying
the desired app-domain changes.

## Intent

This is dogfooding infrastructure lifecycle. BMW should use Workflow and wfctl
to manage its app, DNS zone, DNS records, and App Platform domain association.
When the tooling is missing a lifecycle capability, fix Workflow/wfctl or the
provider plugin instead of hiding the gap in custom CI YAML or console actions.

## Current State

DigitalOcean App Platform already supports custom domains in app specs through
`domains`. Its `zone` field tells App Platform to manage the domain through a
DigitalOcean DNS zone when the zone exists in the account.

DigitalOcean DNS exposes zones as domain resources and records as nested domain
record resources. The existing `workflow-plugin-digitalocean` `infra.dns`
driver can create a zone and upsert simple records, but `Read` does not include
records, `Diff` does not compare records, and record identity is too shallow
for production reconciliation.

wfctl has an `IaCProvider.Import` interface hook, but `wfctl infra import` is
currently a stub and direct `infra apply` does not adopt live provider resources
before computing the local plan.

## Design

Model DNS and app-domain association as separate resources:

- `infra.dns`: owns the DigitalOcean-managed DNS zone and declared DNS records.
- `infra.container_service`: owns the DigitalOcean App Platform app and its
  `domains` app-spec entries.

wfctl direct `infra apply` gets an adoption pass before `ComputePlan`. For each
desired resource missing from state, wfctl derives a provider lookup ID for
adoptable resource types, reads the live provider resource through the resource
driver, writes a state record with the live provider ID and outputs, then lets
the normal plan become an update instead of a create. A not-found response keeps
the current create path. Other read errors fail fast.

`wfctl infra import` becomes config-aware so users and CI can explicitly import
one desired resource by name:

```sh
wfctl infra import --config infra.yaml --env prod --name buymywishlist-dns
```

The explicit command and the apply-time adoption pass share the same state
record construction code.

The DigitalOcean DNS driver reads and returns records, reconciles records
idempotently, and avoids duplicate record creation. The first implementation is
non-destructive: it creates missing declared records and updates matching
declared records, but does not delete undeclared records. That preserves
unrelated records when adopting an existing production zone.

BMW then declares:

- prod App Platform domain `buymywishlist.com` with `type: PRIMARY` and
  `zone: buymywishlist.com`.
- a prod `infra.dns` resource for `buymywishlist.com`.
- dependency ordering so DNS adoption/management precedes app domain mapping.

## Implementation Plan

### Phase 1 - Workflow/wfctl

1. Add tests for apply-time adoption: existing `infra.dns` missing from state is
   read from the provider, saved to state, and converted from create to update.
2. Add tests for not-found and non-not-found adoption errors.
3. Implement adoptable resource lookup helpers for `infra.dns`.
4. Wire adoption before `platform.ComputePlan` in the direct `infra.*` apply
   path.
5. Replace the `wfctl infra import` stub with a config-aware provider import
   path that supports `--config`, `--env`, and `--name`.

### Phase 2 - workflow-plugin-digitalocean

1. Add DNS driver tests for `Read` returning live records.
2. Add DNS driver tests proving existing records are updated rather than
   duplicated.
3. Add DNS driver tests for multiple record fields used by DigitalOcean:
   `type`, `name`, `data`, `ttl`, `priority`, `port`, `weight`, `flags`, `tag`.
4. Implement canonical record normalization and record matching.
5. Keep reconciliation non-destructive unless a future `prune_records` option
   is explicitly added.

### Phase 3 - BMW

1. Bump BMW to the released Workflow/wfctl and DigitalOcean plugin versions.
2. Add prod domain config to the App Platform resource.
3. Add prod DNS-zone management for `buymywishlist.com`.
4. Run local `wfctl infra plan --env prod --config infra.yaml`.
5. Open a BMW PR, wait for green checks, admin merge, and monitor deploy.

## Alignment Check

- The plan keeps provider-specific DNS behavior in the DigitalOcean plugin.
- wfctl owns lifecycle orchestration and state adoption, matching its product
  role as the portable infrastructure lifecycle CLI.
- No custom GitHub workflow logic is introduced.
- Existing production DNS is treated conservatively: adopt and update declared
  records, do not prune unknown records.
- BMW deployment remains blocked on releases rather than unreleased local code.

## References

- DigitalOcean App Platform app spec domains:
  https://docs.digitalocean.com/products/app-platform/reference/app-spec/
- DigitalOcean Domains API:
  https://docs.digitalocean.com/reference/api/reference/domains/
- DigitalOcean Domain Records API:
  https://docs.digitalocean.com/reference/api/reference/domain-records/
