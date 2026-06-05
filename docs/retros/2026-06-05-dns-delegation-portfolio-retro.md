# Retro — DNS delegation in portfolio (both layers)

**Date:** 2026-06-05
**Scope:** workflow v0.71.0 + workflow-plugin-hover v0.5.2→v0.5.4 + gocodealone-dns import-dns delegation
**Artifacts:** design `docs/plans/2026-06-02-dns-delegation-portfolio-design.md` (3 revs) · plan `...portfolio.md` (3 revs, scope-locked→complete) · ADR 0047

## Outcome

The DNS catalog now captures BOTH layers per domain: registrar NS delegation (`Snapshot.Authority{registrar_nameservers, live_nameservers}`) + hosted records. Live-proven: import-dns.yml imported 30 `infra.dns_delegation`; 30/30 portfolio snapshots carry both layers; 15 domains flagged delegated-away (NS ≠ hover → Hover records are placeholder/staging). Catalog data PR gocodealone-dns#18 open.

## What worked

- **Adversarial gates caught 3 real bugs PRE-merge** that would have silently shipped broken: (1) the state-store overwrite — both import types keyed the same `.json` file by domain, so the merge would have seen only delegation → empty `records` (the entire point, defeated). (2) source-of-truth — the delegation `Read` returns live-DNS-first, so a naive import would capture the *stale* live NS during a cutover, not the registrar intent. (3) Outputs key-shape — emitting only `registrar_nameservers` would have broken `Diff`/`parseDelegationSpec` (spurious perpetual drift). None were visible from the design text alone; the reviewer had to read the code.
- **Type-namespacing the import state ID** (`resourceType + "/" + zone`) — a small, general engine fix that makes any two resource types for one domain coexist on disk.

## What live validation caught (that nothing else did)

The unit tests + 2 design + 2 plan adversarial cycles were all green, yet the **first bulk live import** surfaced three runtime-only failures in sequence — a textbook case for `runtime-launch-validation`:

1. **Imperva 429** on the per-domain NS read burst → fixed with retry-with-backoff (v0.5.3).
2. **HTTP 404 on every domain** → the delegation read used `GET /api/control_panel/domains/domain-<name>`, which is **PUT-only** (Hover field-update endpoint). The read path had **never been live-tested** — the test account has 0 domains, and `SetNameservers`/delegation were never exercised against real Hover. Root-caused with the operator's live API captures: `GET /api/domains` (the list `ListDomains` already calls) returns `nameservers` for every domain in ONE call. Fix (v0.5.4): parse + cache NS from the list → the delegation import is ~1 Hover call (also dissolving the 429 fan-out), with `GET /api/domains/<name>` as the per-domain fallback.
3. **Release `verify-capabilities` gate** failed twice on `plugin.json` version ≠ git tag.

## Lessons

- **A read/write path with zero live coverage is a latent wrong-endpoint bug.** The delegation `Read` shipped (months earlier) against a guessed endpoint and passed all stub tests; only the first bulk live call exposed it. Treat "never live-tested" as a release risk, not a footnote — the design's own Assumption #1 flagged this, and it was right.
- **Bulk per-resource fan-out is rate-limit-fragile against bot-protected APIs.** Prefer the list endpoint when it already carries the field (it did). The fix turned 30 calls into 1.
- **Release-carrying PRs must bump `plugin.json` (root + cmd/) to match the intended tag** — the `verify-capabilities` gate enforces tag==manifest version. Fold the bump into the feature PR, not a follow-up.
- **Operator API captures are the fastest root-cause for a closed third-party API.** Two `curl`s against the real endpoints settled days of guessing.

## Follow-ups

- Merge catalog PR gocodealone-dns#18 (real portfolio data).
- Live-test the in-browser WRITE path (`SetNameservers`) against a disposable domain before any migration relies on it (still httptest-only).
- DO delegation not captured (registrar Hover holds it) — future-optional.
- Carryover (hover#31): UA/platform/version derivation; setup-go Node-20 bump.
