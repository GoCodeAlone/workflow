# 0047. Represent DNS delegation in the portfolio via Snapshot.Authority

**Status:** Accepted
**Date:** 2026-06-02
**Decision-makers:** codingsloth@pm.me (directed), Claude (Opus 4.8)
**Related:** docs/plans/2026-06-02-dns-delegation-portfolio-design.md

## Context

The DNS catalog (gocodealone-dns import-dns.yml) imports `--type infra.dns` only — hosted records. For domains whose NS delegate elsewhere, those are parking/placeholder records, and the registrar-level NS delegation (which provider actually serves each domain) is never captured. The canonical portfolio schema `workflow.dns-portfolio.export.v1` already has an unused `Snapshot.Authority map[string]any` field. User requires BOTH layers in the catalog — delegation AND hosted records — so that records staged at a provider ahead of an NS cutover stay visible (live-only would hide them).

## Decision

Populate `Snapshot.Authority` with the registrar NS delegation, merged by `(provider, domain)` with hosted records, so one snapshot carries both layers. `record.FromResourceStates` gains an `infra.dns_delegation` branch (`Outputs["nameservers"]` → `Authority["nameservers"]`); `workflow-plugin-hover` `EnumerateAll` gains an `infra.dns_delegation` case (the per-domain `GetDomainDelegation` already exists).

Alternatives rejected:
- **Side-file (`--format state`)** — no engine change, but splits the catalog into two inconsistent formats; the user wants both layers in the canonical portfolio.
- **NS-as-records** — conflates registrar delegation with in-zone NS records (different semantics); `Authority` is the correct home.

## Consequences

- 3-repo cascade (workflow engine + hover plugin + gocodealone-dns) + 2 releases (wfctl + hover v0.5.1). Engine change is ~15 lines + tests.
- Additive schema use — `Authority` was always present (`omitempty`); no schema break, existing portfolio consumers unaffected.
- A snapshot now distinguishes live (NS point here) from staging/placeholder (NS elsewhere) records.
- Revertible per-repo by version pins; reverting canonicalize returns portfolios to records-only.
- DO delegation not captured (DO NS are self-referential; the registrar Hover holds the real delegation) — future-optional, not built.
