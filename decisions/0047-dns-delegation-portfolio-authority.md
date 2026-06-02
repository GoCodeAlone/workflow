# 0047. Represent DNS delegation in the portfolio via Snapshot.Authority

**Status:** Accepted
**Date:** 2026-06-02
**Decision-makers:** codingsloth@pm.me (directed), Claude (Opus 4.8)
**Related:** docs/plans/2026-06-02-dns-delegation-portfolio-design.md

## Context

The DNS catalog (gocodealone-dns import-dns.yml) imports `--type infra.dns` only — hosted records. For domains whose NS delegate elsewhere, those are parking/placeholder records, and the registrar-level NS delegation (which provider actually serves each domain) is never captured. The canonical portfolio schema `workflow.dns-portfolio.export.v1` already has an unused `Snapshot.Authority map[string]any` field. User requires BOTH layers in the catalog — delegation AND hosted records — so that records staged at a provider ahead of an NS cutover stay visible (live-only would hide them).

## Decision

Populate `Snapshot.Authority` with delegation NS, merged by `(provider, domain)` with hosted records, so one snapshot carries both layers. `record.FromResourceStates` groups states by `(provider, domain)`: `infra.dns` → `Records`; `infra.dns_delegation` → `Authority`. `workflow-plugin-hover` `EnumerateAll` gains an `infra.dns_delegation` case.

**Capture BOTH registrar and live NS** (`authority.registrar_nameservers` from `GetDomainDelegation` = registrar intent/authoritative; `authority.live_nameservers` from public DNS = propagation). The registrar-vs-live gap IS the NS-switch-staging signal the user requires. Critically: the catalog must source `registrar_nameservers` from `GetDomainDelegation` explicitly — NOT from `DelegationDriver.Read`, which returns the live public lookup first (so a naive import would capture stale live NS during a cutover). `Read`/drift behavior is left unchanged (no DNS-provider drift blast radius); the delegation `EnumerateAll`/`Import` path sources registrar+live directly.

**Consumer read model:** `authority` attaches to the registrar's snapshot (provider=hover). To find where a domain is live, match `registrar_nameservers` to a provider; a Hover snapshot whose `registrar_nameservers` point elsewhere carries staging/placeholder records.

Alternatives rejected:
- **Side-file (`--format state`)** — splits the catalog into two inconsistent formats.
- **NS-as-records** — conflates registrar delegation with in-zone NS records.
- **Live-NS-only** — captures the wrong NS during a cutover (defeats the staging-visibility requirement).
- **Change `DelegationDriver.Read` to registrar-primary** — would fix the source but changes drift semantics across the DNS-provider ecosystem; isolated to the catalog path instead.
- **Single EnumerateAll pass emitting both types** — overloads the `--type` filter contract; shared browser profile mitigates the double-login cost instead.

## Consequences

- 3-repo cascade (workflow engine + hover plugin + gocodealone-dns) + 2 releases (wfctl + hover v0.5.1). Engine change is ~15 lines + tests.
- Additive schema use — `Authority` was always present (`omitempty`); no schema break, existing portfolio consumers unaffected.
- A snapshot now distinguishes live (NS point here) from staging/placeholder (NS elsewhere) records.
- Revertible per-repo by version pins; reverting canonicalize returns portfolios to records-only.
- DO delegation not captured (DO NS are self-referential; the registrar Hover holds the real delegation) — future-optional, not built.
