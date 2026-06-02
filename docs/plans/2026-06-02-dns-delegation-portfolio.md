# DNS delegation in portfolio (both layers) Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task. ALWAYS prefix Go commands with `GOWORK=off GOTOOLCHAIN=auto`. Ignore editor "undefined symbol" diagnostics from a sibling repo's go.work — the CLI build is the truth.

**Goal:** Capture registrar NS delegation alongside hosted records in the canonical DNS portfolio, so the catalog shows where each domain's DNS is live (`authority`) AND its hosted records per provider (`records`, incl. staging records before an NS switch).

**Architecture:** 3-repo cascade. (1) workflow engine: `FromResourceStates` merges `infra.dns` (records) + `infra.dns_delegation` (authority) by `(provider,domain)` into one Snapshot; `Sanitize` allow-lists Authority keys. (2) workflow-plugin-hover: `EnumerateAll` lists domains for `infra.dns_delegation`; `HoverProvider.Import` dual-fetches registrar (GetDomainDelegation) + live (public NS) — bypassing the live-first `Read` (drift unchanged). (3) gocodealone-dns: import-dns.yml adds a delegation import (runs second, owns the merged portfolio) + fail-gate + pin bumps.

**Tech Stack:** Go 1.26, workflow IaC provider/portfolio (`workflow.dns-portfolio.export.v1`), wfctl `infra import-all`, GitHub Actions self-hosted runner.

**Base branch:** main (each repo)

**Design:** docs/plans/2026-06-02-dns-delegation-portfolio-design.md · **ADR:** decisions/0047-dns-delegation-portfolio-authority.md

---

## Scope Manifest

**PR Count:** 3
**Tasks:** 6
**Estimated Lines of Change:** ~450 (informational; not enforced)

**Out of scope:**
- DO (or other-provider) delegation enumeration — DO NS are self-referential; the registrar (Hover) holds the delegation truth. Future-optional.
- Changing `DelegationDriver.Read` / drift / apply semantics — explicitly left as-is (zero drift blast radius).
- Any DNS mutation — import is read-only; no `apply`/`SetNameservers`.
- A new wfctl portfolio-export subcommand (Option F) — reuse `import-all --format portfolio`.

**PR Grouping:**

| PR # | Title | Tasks | Branch | Repo |
|------|-------|-------|--------|------|
| 1 | Portfolio: merge infra.dns_delegation into Snapshot.Authority | Task 1, Task 2 | feat/dns-delegation-portfolio | workflow |
| 2 | Hover: enumerate + import infra.dns_delegation (registrar+live NS) | Task 3, Task 4 | feat/hover-delegation-enumerate | workflow-plugin-hover |
| 3 | DNS catalog: import Hover delegation, both layers | Task 5, Task 6 | chore/dns-catalog-delegation | gocodealone-dns |

**Deploy ordering (load-bearing):** PR1 merge → workflow/wfctl release; PR2 merge → hover v0.5.1 release; THEN PR3 (bumps both pins, re-runs import). PR3's validation depends on both releases.

**Status:** Draft

---

### Task 1: `FromResourceStates` merges delegation into `Snapshot.Authority`

**Repo:** workflow **Files:**
- Modify: `dns/record/canonicalize.go`
- Modify: `dns/record/canonicalize_test.go`

**Step 1: Write/adjust failing tests.** In `canonicalize_test.go`:
- Update `TestFromResourceStatesSkipsNonDNS` (finding T-1): a genuinely-unknown type (e.g. `infra.compute`) is still skipped (0 snapshots), but `infra.dns_delegation` is now CONSUMED.
- `TestFromResourceStates_DelegationPopulatesAuthority`: a state `{Type:"infra.dns_delegation", Provider:"hover", ProviderID:"x.com", Outputs:{"registrar_nameservers":["ns1.dnsimple.com"],"live_nameservers":["ns1.digitalocean.com"]}}` → one snapshot with `Authority["registrar_nameservers"]==["ns1.dnsimple.com"]` and `Authority["live_nameservers"]==["ns1.digitalocean.com"]`, `Records` empty.
- `TestFromResourceStates_MergesBothLayersByDomain`: an `infra.dns` state + an `infra.dns_delegation` state, SAME `(provider="hover", domain="x.com")` → exactly ONE snapshot carrying both `Records` (from infra.dns) and `Authority` (from delegation).
- `TestFromResourceStates_DelegationOnlyDomain` (finding m-1): delegation state with no matching infra.dns → one authority-only snapshot, `Records: []` (non-nil-or-empty per existing Validate).
- Keep existing records-only behavior: N `infra.dns` states → N snapshots.

Run: `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -run TestFromResourceStates -count=1 -v` → expect FAIL.

**Step 2: Implement.** Restructure `FromResourceStates` to group by key `provider+"\x00"+domain`:
- Iterate states; resolve `domain` (ProviderID, fall back `AppliedConfig["domain"]`); skip if domain empty.
- For `infra.dns`: get-or-create the snapshot, append `Records` (existing `pickRecords`/`recordFromMap`).
- For `infra.dns_delegation`: get-or-create the snapshot, set `Authority` from `st.Outputs` reading `registrar_nameservers` + `live_nameservers` (each `[]any`→`[]string` via a small helper; copy only those keys — do NOT copy the whole Outputs map). Tolerate missing key (omit it).
- For any other type: `continue` (still skipped).
- Emit snapshots sorted by `(provider, domain)` for determinism.
- Preserve `ID` (first state that creates the group sets it).

Run the tests → expect PASS.

**Step 3: Full package + repo verification.**
`GOWORK=off GOTOOLCHAIN=auto go test ./dns/record ./cmd/wfctl -count=1` (cmd/wfctl exercises dumpPortfolioToFile) → all green.
`GOWORK=off GOTOOLCHAIN=auto golangci-lint run --new-from-rev=origin/main ./dns/... ./cmd/wfctl/...` → exit 0.

**Step 4: Commit.** `git commit -m "feat(dns): merge infra.dns_delegation into Snapshot.Authority"`

Rollback: revert commit → `FromResourceStates` returns to records-only; no schema break. (Runtime-affecting via wfctl release — see deploy ordering.)

### Task 2: `Sanitize` allow-lists `Authority` keys

**Repo:** workflow **Files:**
- Modify: `dns/record/sanitize.go`
- Modify: `dns/record/sanitize_test.go`

**Step 1: Failing test.** `TestSanitizeStripsUnknownAuthorityKeys`: a snapshot with `Authority{"registrar_nameservers":[...],"live_nameservers":[...],"secret_token":"x"}` → after `Sanitize`, `registrar_nameservers`+`live_nameservers` remain, `secret_token`+any key ∉ allow-list `{registrar_nameservers, live_nameservers, domain_id}` removed. Records sanitization unchanged.
Run: `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -run TestSanitize -count=1 -v` → FAIL.

**Step 2: Implement.** In `Sanitize`, after the Records pass, for each snapshot with non-nil `Authority`, delete keys not in the allow-list. (NS are public; this guards future callers from leaking non-NS data via the free-form `map[string]any`.)

**Step 3:** `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -count=1` → green. `golangci-lint run --new-from-rev=origin/main ./dns/...` → exit 0.

**Step 4: Commit.** `git commit -m "feat(dns): sanitize allow-lists Snapshot.Authority keys"`

Rollback: revert commit.

### Task 3: Hover `EnumerateAll` lists domains for `infra.dns_delegation`

**Repo:** workflow-plugin-hover **Files:**
- Modify: `internal/provider.go` (`EnumerateAll`)
- Modify: `internal/provider_test.go`

**Step 1: Failing test.** `TestEnumerateAll_DelegationListsDomains`: a stub domains-lister returning 2 domains → `EnumerateAll(ctx, "infra.dns_delegation")` returns 2 `ResourceOutput`s, each `Type=="infra.dns_delegation"`, `ProviderID==domain.Name`. Also assert an unknown type still errors `"resource type %q not supported"`.
Run: `GOWORK=off GOTOOLCHAIN=auto go test ./internal -run TestEnumerateAll -count=1 -v` → FAIL.

**Step 2: Implement.** Change the `EnumerateAll` guard: accept `infra.dns` AND `infra.dns_delegation`; for either, `ListDomains` → emit `ResourceOutput{ProviderID:d.Name, Type:resourceType, Outputs: {"domain_id": d.ID}}` (delegation NS are fetched in Import, Task 4 — keep enumerate cheap: one `ListDomains` call). Reject other types as before.

**Step 3:** `GOWORK=off GOTOOLCHAIN=auto go test ./internal -count=1` → green.

**Step 4: Commit.** `git commit -m "feat(provider): EnumerateAll lists domains for infra.dns_delegation"`

### Task 4: Hover `Import` dual-fetches registrar + live NS (bypass Read)

**Repo:** workflow-plugin-hover **Files:**
- Modify: `internal/provider.go` (`Import`)
- Modify: `internal/drivers/delegation.go` (export a dual-fetch helper)
- Modify: `internal/drivers/delegation_test.go`
- Modify: `internal/provider_test.go`

**Step 1: Failing tests.**
- `delegation_test.go` `TestDelegationImportRead_DualNS`: with a stub client whose `GetDomainDelegation` returns `["ns1.dnsimple.com"]` and a stub public-NS resolver returning `["ns1.digitalocean.com"]`, the new dual-fetch helper returns `Outputs{"nameservers":["ns1.dnsimple.com"], "registrar_nameservers":["ns1.dnsimple.com"], "live_nameservers":["ns1.digitalocean.com"], "domain_id":...}`. (`nameservers` == registrar = PRIMARY key so `Diff`/`parseDelegationSpec` stay consistent — finding I-NEW-3.)
- `provider_test.go` `TestImport_DelegationUsesRegistrarNotLiveRead`: `HoverProvider.Import(ctx, "x.com", "infra.dns_delegation")` returns a `ResourceState` whose `Outputs["registrar_nameservers"]` comes from `GetDomainDelegation` (NOT the live-first `Read` path) — assert via a stub where registrar≠live and confirm `registrar_nameservers`==registrar.
Run: `GOWORK=off GOTOOLCHAIN=auto go test ./internal ./internal/drivers -run 'Delegation|Import' -count=1 -v` → FAIL.

**Step 2: Implement.**
- In `internal/drivers/delegation.go`, add an exported `func (d *DelegationDriver) ReadForImport(ctx, ref) (*interfaces.ResourceOutput, error)`: call `d.client.GetDomainDelegation(ctx, domain)` (registrar) for the authoritative NS; call the existing `lookupPublicNameservers(ctx, domain)` (live) best-effort (ignore error → omit `live_nameservers`); build `Outputs{"nameservers": registrar, "registrar_nameservers": registrar, "live_nameservers": live, "domain_id": ...}` (`[]any` values). Do NOT modify `Read` (drift path).
- In `internal/provider.go` `Import`, BEFORE the generic `d.Read` call: `if resourceType == "infra.dns_delegation" { if dd, ok := d.(*drivers.DelegationDriver); ok { out, err := dd.ReadForImport(ctx, ref); ... build/return ResourceState } }`. Fall through to `d.Read` for `infra.dns`.

**Step 3:** `GOWORK=off GOTOOLCHAIN=auto go build ./... && GOWORK=off GOTOOLCHAIN=auto go vet ./... && GOWORK=off GOTOOLCHAIN=auto go test ./... -count=1` → all green (existing delegation Diff/Read tests stay green — `nameservers` key preserved). `golangci-lint run --new-from-rev=origin/main` → exit 0.

**Step 4: Commit.** `git commit -m "feat(provider): Import dual-fetches registrar+live NS for delegation"`

**Plugin runtime validation (Step 1b trigger — plugin loading path):** the live proof is the gocodealone-dns import re-run in Task 6; locally confirm the plugin still builds + loads (existing `TestPluginBinaryEmbedsManifest` green). After merge → release **v0.5.1**.

Rollback: revert commits → delegation enumerate returns "not supported", Import unchanged; pin consumers back to v0.5.0.

### Task 5: import-dns.yml — Hover delegation import (second) + fail-gate

**Repo:** gocodealone-dns **Files:**
- Modify: `.github/workflows/import-dns.yml`

**Step 1: Add the delegation import step** AFTER the existing `Import Hover DNS zones` step, `id: import-hover-delegation`, `continue-on-error: true`, same `HOVER_*` env, same `--config infra/hover.wfctl.yaml --provider hover --plugin-dir data/plugins`, but `--type infra.dns_delegation` and `-o zones/hover.portfolio.json` (it reads the shared `.state/hover/` populated by BOTH imports → emits the MERGED records+authority portfolio, overwriting the records-only one — finding I-NEW-4). The first (`infra.dns`) Hover step keeps `-o zones/hover.portfolio.json` too (records-only, then overwritten). Ensure both Hover steps use the SAME default `browser_profile_dir` (no override → shared `$XDG_STATE` profile → cookie reuse, finding m-2).

**Step 2: Update the fail-gate** (`Fail run if any provider import failed`, finding I-3): add `[ "${{ steps.import-hover-delegation.outcome }}" = "failure" ] && failed="$failed hover-delegation"`.

**Step 3: Lint the workflow.** `actionlint .github/workflows/import-dns.yml` (or confirm YAML parses); the real exercise is Task 6's dispatch.

**Step 4: Commit.** `git commit -m "ci(dns): import Hover delegation (both layers) + fail-gate"`

Rollback: revert commit → import returns to records-only.

### Task 6: Pin bumps + live catalog validation

**Repo:** gocodealone-dns **Files:**
- Modify: `.github/wfctl-version` (bump to the workflow release carrying Task 1/2)
- Modify: `wfctl.yaml` + `.wfctl-lock.yaml` (hover v0.5.0 → v0.5.1)

**Precondition:** PR1 merged + workflow released; PR2 merged + hover **v0.5.1** released.

**Step 1:** Bump `.github/wfctl-version` to the new workflow release tag; bump hover pin to v0.5.1 in both `wfctl.yaml` and `.wfctl-lock.yaml`.

**Step 2: Commit + open PR3.** `git commit -m "chore(dns): bump wfctl + hover v0.5.1 for delegation catalog"`

**Step 3: Multi-component live validation (the real-consumer proof).** After PR3 merges, dispatch `import-dns.yml` on the self-hosted runner. Assert from the run log + the catalog PR's `zones/hover.portfolio.json`:
- `imported N infra.dns zones via provider "hover"` AND `imported N infra.dns_delegation` (or equivalent) — both steps succeed, fail-gate green.
- Snapshots carry `authority.registrar_nameservers` (non-empty) AND `records`.
- At least one domain shows `registrar_nameservers` ≠ a hover nameserver (delegated-away → its Hover records are staging/placeholder).
- Exactly one snapshot per `(provider,domain)` (no duplicates from the merge).
Expected: the resulting catalog-refresh PR shows both layers; no 401/ErrBotChallenge.

Rollback: revert pins (wfctl + hover→v0.5.0) + the import step (Task 5) → catalog returns to records-only.
