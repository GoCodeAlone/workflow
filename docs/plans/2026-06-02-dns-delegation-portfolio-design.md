# DNS catalog: capture delegation (NS) alongside hosted records — Design

**Date:** 2026-06-02
**Status:** Design (autonomous pipeline; user-directed "design and build it, both layers")
**Repos:** workflow (engine/wfctl) · workflow-plugin-hover · gocodealone-dns
**Guidance:** no `docs/design-guidance.md`; Q&A captured inline (user direction below)

## Problem

The gocodealone-dns DNS catalog import (`import-dns.yml`) imports `--type infra.dns` only → each provider's **hosted records**. For a domain whose **NS delegate elsewhere**, the Hover-hosted records are **parking/placeholders** (e.g. `blackorchid-tributeband.com` → `A * → 216.40.34.41` = Hover park IP, default Hover MX; portfolio `authority: null`, 0 NS). The catalog never captures the **registrar-level NS delegation** — the map of *which provider actually serves each domain*. Result: the catalog shows placeholder records with no indication they're not live.

**User requirement (verbatim intent):** capture BOTH layers — (1) delegation (registrar NS = where DNS is live) and (2) hosted records per provider — *"regardless of hover or another provider, because if only track the live dns, we won't have knowledge of records being added to prep for an NS switch. So both layers seem a minimum."* I.e. staging records (provisioned at a provider before an NS cutover) must remain visible.

## Decision

Populate the **already-existing** `Snapshot.Authority` field (canonical schema `workflow.dns-portfolio.export.v1`, `dns/record/record.go`) with the registrar NS delegation, merged by domain with the hosted records, so one portfolio snapshot carries BOTH layers.

```
Snapshot{ provider, domain,
          authority: {nameservers:[...]},   // infra.dns_delegation — where DNS is LIVE
          records:   [...] }                // infra.dns — hosted records (live OR staging)
```

A domain's snapshot now answers: *where is DNS authoritatively served* (authority NS) AND *what records exist at this provider* (records — authoritative if NS point here, staging/placeholder otherwise).

### 3-repo cascade

1. **workflow-plugin-hover** — `HoverProvider.EnumerateAll` currently rejects all but `infra.dns`. Add `infra.dns_delegation`: `ListDomains` → per-domain `GetDomainDelegation` → emit `ResourceOutput{ProviderID: domain, Type: "infra.dns_delegation", Outputs: {nameservers: [...], domain_id}}`. (The delegation driver + `GetDomainDelegation` already exist; only the enumerate loop is missing.) Release v0.5.1.
2. **workflow** — `record.FromResourceStates` (`dns/record/canonicalize.go`) currently `continue`s on every non-`infra.dns` state. Change to merge by `(provider, domain)`: `infra.dns` → `Records`; `infra.dns_delegation` → `Authority = {"nameservers": [...]}` (read from `st.Outputs["nameservers"]`). Domains with only one type get that layer; the other stays empty/omitted. Release (next wfctl minor).
3. **gocodealone-dns** — `import-dns.yml`: add a Hover `--type infra.dns_delegation` import into the **same** Hover state dir (so the portfolio merge sees both), bump the wfctl pin (engine change) + hover pin (v0.5.1). The Hover delegation = the registrar master map (all 30 domains are Hover-registered).

### Data flow

`import-all --provider hover --type infra.dns` + `--type infra.dns_delegation` → both resource types land in `.state/hover/` → `--format portfolio` → `FromResourceStates` merges by domain → `hover.portfolio.json` with `authority` + `records` per snapshot. DO unchanged (records only; DO NS are self-referential — registrar truth comes from Hover).

## Approaches considered

- **A — unified portfolio via `Authority` (chosen).** Engine change populates the schema's existing `Authority` field. One canonical catalog, both layers per snapshot. Cost: 3-repo cascade + 2 releases (engine change is ~15 lines + tests). Matches the schema author's intent (the field exists, unused).
- **B — side-file (`--format state`).** Import delegation as a raw state file (no engine change). Lighter (2 repos, 1 release) but produces a NON-canonical side artifact split from the portfolio; the catalog becomes two inconsistent formats. Rejected: the user wants both layers in the catalog, and the portfolio schema already models authority.
- **C — NS-as-records.** Emit delegation as `NS` records in the existing `records[]`. Rejected: conflates registrar delegation (where the zone is served) with in-zone NS records (subdomain delegation) — different semantics; `Authority` is the correct home.

## Global Design Guidance

| guidance | response |
|---|---|
| Primary Go, stdlib-first | engine change is pure Go map-merge; no new deps |
| Canonical portfolio schema | reuses existing `Authority`/`Snapshot` — no schema break (additive; `authority,omitempty`) |
| Plugin contract stability | hover gains an enumerate case; no gRPC/driver contract change |
| e2e via real consumer | validated by re-running gocodealone-dns import-dns.yml on the self-hosted runner |

## Security Review

- No new secrets/flows. Delegation read uses the same authenticated Hover session as records. NS are non-sensitive (public DNS). Portfolio `Sanitize` path unaffected (NS are not secret).
- `Authority map[string]any` is free-form; restrict written keys to `nameservers` (+ optional `domain_id`) to avoid leaking internal state into the catalog.

## Infrastructure Impact

- Two releases (workflow wfctl + hover v0.5.1) + gocodealone-dns pin bumps. import-dns.yml gains one provider-import step (read-only; another browser-auth Hover login per run). No new cloud resources.
- Rollback class: version pins + engine behavior + plugin behavior → see Rollback.

## Multi-Component Validation

Re-run gocodealone-dns `import-dns.yml` on the self-hosted runner after both releases + pin bumps: assert `hover.portfolio.json` snapshots carry non-null `authority.nameservers` AND `records`, and that at least one domain shows NS ≠ hover (delegated-away → records are staging/placeholder). Engine change unit-tested in `dns/record`; hover enumerate unit-tested + the live probe path.

## Assumptions

1. **`GetDomainDelegation` works for all 30 domains live** (the browser-auth session reads `/api/control_panel/domains/domain-<name>` NS). Most fragile — validated by the live import.
2. **Registrar NS at Hover is the authoritative delegation source** for these domains (all Hover-registered). True for the current portfolio.
3. **Merging by `(provider, domain)` is correct** — a domain appears once per provider state; the delegation + records for the same domain share provider=hover. Holds because both imports run against the same `.state/hover/`.
4. **`Snapshot.Authority` consumers tolerate population** (it was always in the schema, `omitempty`; scenario-88 fixtures + `Validate()` don't reject it).

## Rollback

- Engine: revert the canonicalize change → `Authority` returns to never-populated; portfolios drop to records-only (prior behavior). No schema break.
- hover: revert to v0.5.0 (delegation enumerate returns "not supported" — prior behavior).
- gocodealone-dns: revert pins + drop the delegation import step. Catalog returns to records-only.
- Additive + behind version pins; each repo independently revertible.

## Self-challenge — top doubts

1. **3-repo cascade for a ~15-line engine change feels heavy.** But the canonical-catalog requirement forces it: a side-file (B) splits the catalog. The engine change is small + the schema was built for it.
2. **Assumption #1 (live delegation read for all 30 domains)** — if `GetDomainDelegation` fails for some domains (rate-limit, shape drift), enumerate must continue + emit partial (skip-with-warning), not abort the whole import.
3. **DO delegation not captured** — DO NS are self-referential (ns1.digitalocean.com); the registrar (Hover) holds the real delegation. Capturing DO delegation is YAGNI for this portfolio; the design notes it as future-optional, not built.

## ADR

Will record an ADR for the unified-Authority decision (chosen over side-file / NS-as-records) in the workflow repo.
