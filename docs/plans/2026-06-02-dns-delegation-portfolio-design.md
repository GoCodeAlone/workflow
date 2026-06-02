# DNS catalog: capture delegation (NS) alongside hosted records — Design

**Date:** 2026-06-02
**Status:** Design (autonomous pipeline; user-directed "design and build it, both layers"). Rev 2 — incorporates adversarial-design-review findings.
**Repos:** workflow (engine/wfctl) · workflow-plugin-hover · gocodealone-dns
**Guidance:** no `docs/design-guidance.md`; Q&A captured inline (user direction below)

## Problem

The gocodealone-dns DNS catalog import (`import-dns.yml`) imports `--type infra.dns` only → each provider's **hosted records**. For a domain whose **NS delegate elsewhere**, the Hover-hosted records are **parking/placeholders** (`blackorchid-tributeband.com` → `A * → 216.40.34.41` = Hover park IP, default Hover MX; portfolio `authority: null`, 0 NS). The catalog never captures the **registrar-level NS delegation** — the map of which provider actually serves each domain.

**User requirement (verbatim intent):** capture BOTH layers — (1) delegation (registrar NS = where DNS is intended to be served) and (2) hosted records per provider — *"regardless of hover or another provider, because if only track the live dns, we won't have knowledge of records being added to prep for an NS switch. So both layers seem a minimum."* Records staged at a provider before an NS cutover must stay visible.

## Decision

Populate the **already-existing** `Snapshot.Authority` field (canonical schema `workflow.dns-portfolio.export.v1`, `dns/record/record.go`) with delegation NS, merged by domain with hosted records, so one snapshot carries BOTH layers:

```
Snapshot{ provider, domain,
          authority: { registrar_nameservers:[...],   // GetDomainDelegation — registrar INTENT (authoritative)
                       live_nameservers:[...] },        // public DNS — current PROPAGATION
          records:   [...] }                            // infra.dns — hosted records (live OR staging) }
```

**The registrar-vs-live gap is the NS-switch-staging signal** (adversarial finding I-1, Option E): during a cutover the registrar holds the new NS while live DNS (TTL-cached) still shows the old. Capturing only live NS would hide exactly the prep state the user needs; capturing only registrar NS would hide propagation status. So capture both.

### Critical source-of-truth detail (finding I-1)

`DelegationDriver.Read` (drift/apply path) returns `lookupPublicNameservers` **first**, `GetDomainDelegation` only on fallback. `import-all` re-imports via `provider.Import` → that Read → so a naive delegation import captures **live** NS, not registrar intent. **Fix:** the catalog delegation path MUST source `registrar_nameservers` from `GetDomainDelegation` (registrar) explicitly, and `live_nameservers` from the public lookup — independent of the live-first `Read`. The existing `Read` (drift) stays **unchanged** (no DNS-provider-ecosystem drift blast radius). Implementation: `EnumerateAll("infra.dns_delegation")` fetches both per domain and emits them in `Outputs`; the hover `Import("infra.dns_delegation", domain)` path returns the same registrar+live `Outputs` (so the re-import in `runInfraImportAllWithDeps` persists registrar truth, not the live-first Read).

### 3-repo cascade

1. **workflow-plugin-hover** — `HoverProvider.EnumerateAll` currently `!= "infra.dns"` → hard error. Add an `infra.dns_delegation` case: `ListDomains` → per domain: `GetDomainDelegation` (registrar) + `lookupPublicNameservers` (live) → emit `ResourceOutput{ProviderID: domain, Type: "infra.dns_delegation", Outputs: {registrar_nameservers, live_nameservers, domain_id}}`. **Per-domain failures skip-with-warning + continue** (do not abort the 30-domain enumerate; matches import-all's per-zone isolation). The delegation `Import` path returns the same dual `Outputs`. `Read`/drift unchanged. Release v0.5.1.
2. **workflow** — `record.FromResourceStates` (`dns/record/canonicalize.go`) currently `continue`s on every non-`infra.dns` state, one Snapshot per state. **Restructure to group by `(provider, domain)`** (control-flow change, not a one-liner): build `map[provider+domain]*Snapshot`; `infra.dns` → `Records`; `infra.dns_delegation` → `Authority = {registrar_nameservers, live_nameservers}` (read from `st.Outputs`). A domain with delegation but no `infra.dns` state → authority-only snapshot (`records: []`). Deterministic emit order (sort by provider,domain). Release (next wfctl minor).
3. **gocodealone-dns** — `import-dns.yml`: add a Hover `--type infra.dns_delegation` import into the **same** `.state/hover/` (so the portfolio merge sees both); **add the new step's outcome to the final fail-gate** (currently hardcoded to `import-do`/`import-hover` only — finding I-3). Bump wfctl pin (engine change) + hover pin (v0.5.1). The two Hover imports **share `browser_profile_dir`** → the second reuses Imperva clearance (cookie reuse, no second full login → no extra lockout risk; finding m-2).

### Consumer read semantics (finding I-2)

Delegation is a **registrar fact**, so `authority` attaches to the **registrar's** snapshot (provider=hover). To find where a domain is **live**: read `authority.registrar_nameservers` and match it to a provider (e.g. `ns1.digitalocean.com` → provider=digitalocean) — the records in *that* provider's snapshot are authoritative. A Hover snapshot whose `registrar_nameservers` point elsewhere carries **staging/placeholder** records. This read model is documented in the ADR consequences.

## Approaches considered

- **A — unified portfolio via `Authority` (chosen).** Engine populates the existing `Authority` field; one canonical catalog, both layers per snapshot. Cost: 3-repo cascade + 2 releases.
- **B — side-file (`--format state`).** No engine change but splits the catalog into two inconsistent formats. Rejected (user wants both layers in the canonical portfolio).
- **C — NS-as-records.** Conflates registrar delegation with in-zone NS records. Rejected (`Authority` is the correct home).
- **D — single EnumerateAll pass emitting both `infra.dns` + `infra.dns_delegation`** (one browser session). Considered (finding m-2 / reviewer Option D); rejected for now because it overloads `EnumerateAll("infra.dns")` to emit a different type, breaking the `--type` filter contract. Shared `browser_profile_dir` already mitigates the double-login cost.

## Global Design Guidance

| guidance | response |
|---|---|
| Primary Go, stdlib-first | engine change is pure Go map-merge; delegation reuses existing `net.LookupNS` + `GetDomainDelegation` |
| Canonical portfolio schema | reuses existing `Authority`/`Snapshot` — additive (`authority,omitempty`), no schema break |
| Plugin contract stability | hover gains an enumerate case + dual-NS Import outputs; gRPC/driver contract unchanged; `Read`/drift unchanged |
| e2e via real consumer | validated by re-running gocodealone-dns import-dns.yml on the self-hosted runner |

## Security Review

- No new secrets/flows. Delegation reads use the existing authenticated Hover session; NS are public/non-sensitive.
- `Authority map[string]any` is free-form → `Sanitize` (`dns/record/sanitize.go`) only touches `Records`. **Restrict written Authority keys to `{registrar_nameservers, live_nameservers, domain_id}`** and have `Sanitize` drop any key outside that allow-list when `--sanitize` is set (finding m-3), so future callers can't leak internal data via Authority.

## Infrastructure Impact

- Two releases (workflow wfctl + hover v0.5.1) + gocodealone-dns pin bumps. import-dns.yml gains one read-only provider-import step (one extra Hover login per run, cookie-reused via shared profile). No new cloud resources.
- Rollback class: version pins + engine behavior + plugin behavior → see Rollback.

## Multi-Component Validation

Re-run gocodealone-dns `import-dns.yml` on the self-hosted runner after both releases + pin bumps. Assert: (a) `hover.portfolio.json` snapshots carry `authority.registrar_nameservers` AND `records`; (b) at least one domain shows `registrar_nameservers` ≠ hover (delegated-away → its Hover records are staging/placeholder); (c) the merge produces ONE snapshot per (provider,domain) — no duplicates; (d) a delegation-only domain (if any) yields an authority-only snapshot. Engine merge-by-domain unit-tested in `canonicalize_test.go` (incl. delegation-only + both-layers + records-only cases); hover enumerate + dual-NS Import unit-tested; live probe path exercised.

## Assumptions

1. **`GetDomainDelegation` works live for the 30 domains** (browser-auth session reads `/api/control_panel/domains/domain-<name>` NS). Most fragile — validated by the live import; per-domain failures skip-with-warning, not abort.
2. **Registrar NS at Hover is the authoritative delegation source** (all 30 are Hover-registered). True for the current portfolio.
3. **Merge by `(provider, domain)` is correct** — both Hover imports run against the same `.state/hover/`, so a domain's records + delegation share provider=hover.
4. **`Snapshot.Authority` consumers tolerate population** — verified: `record.Validate()` ignores Authority; scenario-88 fixtures don't use it; `omitempty`.

## Rollback

- Engine: revert canonicalize → `Authority` never populated; portfolios drop to records-only (prior behavior). No schema break.
- hover: revert to v0.5.0 (delegation enumerate → "not supported"). `Read`/drift never changed, so nothing to undo there.
- gocodealone-dns: revert pins + drop the delegation import step + fail-gate entry. Catalog returns to records-only.
- Additive + version-pinned + per-repo revertible.

## Self-challenge — top doubts

1. **Scope grew** (dual-NS + Import-vs-Read distinction) from the naive "import delegation." Justified: the naive version captured the WRONG (live) NS, defeating the user's staging requirement. The dual capture is the correct minimum.
2. **Assumption #1** — partial live-read failure handled by skip-with-warning + the import-dns.yml fail-gate now catches a failed delegation step (I-3).
3. **DO delegation not captured** — DO NS self-referential; registrar (Hover) holds the real delegation. YAGNI for this portfolio; future-optional, not built.

## ADR

ADR 0047 (workflow) records the unified-`Authority` decision + the registrar-vs-live dual-capture (I-1) + the consumer read model (I-2).
