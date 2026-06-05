# DNS delegation in portfolio (both layers) Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task. ALWAYS prefix Go commands with `GOWORK=off GOTOOLCHAIN=auto`. Ignore editor "undefined symbol" diagnostics from a sibling repo's go.work — the CLI build is the truth.

**Goal:** Capture registrar NS delegation alongside hosted records in the canonical DNS portfolio, so the catalog shows where each domain's DNS is live (`authority`) AND its hosted records per provider (`records`, incl. staging records before an NS switch).

**Architecture:** 3-repo cascade. (1) workflow engine: `FromResourceStates` merges `infra.dns` (records) + `infra.dns_delegation` (authority) by `(provider,domain)` into one Snapshot; import-all state IDs become type-namespaced so two resource types for one domain don't overwrite each other on disk; `Sanitize` allow-lists Authority keys. (2) workflow-plugin-hover: `EnumerateAll` lists domains for `infra.dns_delegation`; `HoverProvider.Import` dual-fetches registrar (GetDomainDelegation) + live (public NS) — bypassing the live-first `Read` (drift unchanged). (3) gocodealone-dns: import-dns.yml adds a delegation import (runs second, owns the merged portfolio) + fail-gate + pin bumps.

**Tech Stack:** Go 1.26, workflow IaC provider/portfolio (`workflow.dns-portfolio.export.v1`), wfctl `infra import-all`, GitHub Actions self-hosted runner.

**Base branch:** main (each repo)

**Design:** docs/plans/2026-06-02-dns-delegation-portfolio-design.md · **ADR:** decisions/0047-dns-delegation-portfolio-authority.md

---

## Scope Manifest

**PR Count:** 3
**Tasks:** 7
**Estimated Lines of Change:** ~550 (informational; not enforced)

**Out of scope:**
- DO (or other-provider) delegation enumeration — DO NS are self-referential; the registrar (Hover) holds the delegation truth. Future-optional.
- Changing `DelegationDriver.Read` / drift / apply semantics — explicitly left as-is (zero drift blast radius).
- Any DNS mutation — import is read-only; no `apply`/`SetNameservers`.
- A new wfctl portfolio-export subcommand — reuse `import-all --format portfolio`.

**PR Grouping:**

| PR # | Title | Tasks | Branch | Repo |
|------|-------|-------|--------|------|
| 1 | Portfolio: type-namespaced import IDs + merge delegation into Authority | Task 1, Task 2, Task 3 | feat/dns-delegation-portfolio | workflow |
| 2 | Hover: enumerate + import infra.dns_delegation (registrar+live NS) | Task 4, Task 5 | feat/hover-delegation-enumerate | workflow-plugin-hover |
| 3 | DNS catalog: import Hover delegation, both layers | Task 6, Task 7 | chore/dns-catalog-delegation | gocodealone-dns |

**Deploy ordering (load-bearing):** PR1 merge → workflow/wfctl release (minor — behavioral change to `FromResourceStates` + import-all state IDs); PR2 merge → hover v0.5.1 release; THEN PR3 (bumps both pins, re-runs import). PR3 is independently revertible (revert pins) but NOT independently deployable.

**Status:** Complete 2026-06-05T06:25:38Z

---

### Task 1: `FromResourceStates` merges delegation into `Snapshot.Authority`

**Repo:** workflow **Files:** Modify `dns/record/canonicalize.go`, `dns/record/canonicalize_test.go`

**Step 1: Tests.** In `canonicalize_test.go`:
- Update `TestFromResourceStatesSkipsNonDNS` (finding T-1): a genuinely-unknown type (`infra.compute`) is still skipped (0 snapshots); `infra.dns_delegation` is now CONSUMED.
- `TestFromResourceStates_DelegationPopulatesAuthority`: state `{Type:"infra.dns_delegation", Provider:"hover", ProviderID:"x.com", Outputs:{"registrar_nameservers":[]any{"ns1.dnsimple.com"},"live_nameservers":[]any{"ns1.digitalocean.com"}}}` → one snapshot, `Authority["registrar_nameservers"]==[]any{"ns1.dnsimple.com"}`, `Authority["live_nameservers"]==[]any{"ns1.digitalocean.com"}`, `Records != nil && len(Records)==0`.
- `TestFromResourceStates_MergesBothLayersByDomain`: an `infra.dns` state + an `infra.dns_delegation` state, SAME `(provider="hover", ProviderID="x.com")` → exactly ONE snapshot carrying both `Records` and `Authority`; assert `snap.ID` contains NO `/` and equals `"hover-x-com"` (derived from provider+domain, NOT the type-namespaced `st.ID` — finding I-NEW-1).
- `TestFromResourceStates_DelegationOnlyDomain` (finding m-1): delegation-only → authority-only snapshot, `Records: []Record{}` (non-nil → JSON `"records":[]`, finding I-NEW-2).
- Records-only unchanged: N `infra.dns` states → N snapshots.

Run: `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -run TestFromResourceStates -count=1 -v` → FAIL.

**Step 2: Implement.** Restructure to group by `provider+"\x00"+domain`:
- get-or-create the snapshot per key; initialize `Records: []Record{}` (non-nil).
- `infra.dns` → append `Records` (existing `pickRecords`/`recordFromMap`).
- `infra.dns_delegation` → set `Authority` reading ONLY `registrar_nameservers` + `live_nameservers` from `st.Outputs` (each `[]any`; copy only those keys — never the whole Outputs map; omit a missing key).
- other types → `continue`.
- set `snap.ID = provider + "-" + sanitizeDomainForID(domain)` (add a tiny unexported helper in `dns/record` that lowercases + replaces runs of non-alphanumeric — incl. `.` and `/` — with `-`). Do NOT inherit `st.ID` (Task 2 makes it type-namespaced, e.g. `infra.dns/x.com`, which would leak a `/` into the portfolio JSON — finding I-NEW-1). The domain comes from `st.ProviderID` (the bare domain), so it's stable across both layers.
- emit sorted by `(provider, domain)`.

Run tests → PASS.

**Step 3: Verify.** `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -count=1` green; `golangci-lint run --new-from-rev=origin/main ./dns/...` exit 0.

**Step 4: Commit.** `git commit -m "feat(dns): merge infra.dns_delegation into Snapshot.Authority"`

Rollback: revert → records-only; no schema break. (Runtime-affecting via wfctl release.)

### Task 2: Type-namespace import-all state IDs (fixes overwrite collision)

**Repo:** workflow **Files:** Modify `cmd/wfctl/infra_import_all.go` (`buildResourceStateFromImport`), `cmd/wfctl/infra_import_all_format_test.go`

**Why:** `buildResourceStateFromImport` sets `spec.Name = sanitizeImportedZoneName(zoneName)` → `resourceStateFromImportedState` sets `ID = spec.Name` (infra.go:38) → `SaveResource` writes `sanitizeStateID(ID)+".json"`. For one domain, `infra.dns` and `infra.dns_delegation` imports produce the SAME ID/file → the second OVERWRITES the first → the portfolio merge never sees both layers (adversarial CRITICAL-1, verified).

**Step 1: Tests.** In `infra_import_all_format_test.go`:
- `TestDumpPortfolio_MergesDnsAndDelegationForSameDomain`: pre-populate a state store with an `infra.dns` state AND an `infra.dns_delegation` state for the SAME domain (distinct IDs now), run `dumpPortfolioToFile`, assert the output has ONE snapshot for that domain carrying both `records` (non-empty) and `authority.registrar_nameservers`. This catches CRITICAL-1 at unit level, pre-merge (finding I-NEW-3).
- `TestBuildResourceStateFromImport_TypeNamespacedID`: `buildResourceStateFromImport("example.com","example.com","infra.dns","hover",...)` and the same with `"infra.dns_delegation"` produce DISTINCT `ID`s (so distinct on-disk files), while both retain `ProviderID == "example.com"` (domain unchanged).

Run → FAIL.

**Step 2: Implement.** In `buildResourceStateFromImport`, set `Name: resourceType + "/" + sanitizeImportedZoneName(zoneName)` (so `sanitizeStateID` maps `/`→`_` → `infra.dns_example-com.json` vs `infra.dns_delegation_example-com.json`). Do NOT change `ProviderID` (stays the bare domain via `cloudID`) — `FromResourceStates` keys the snapshot domain on `ProviderID`, so the portfolio domain is unaffected. Backward-compatible: single-type import-all runs have no collision; `.state/` dirs are ephemeral/gitignored so no orphan migration.

Run tests → PASS.

**Step 3: Verify.** `GOWORK=off GOTOOLCHAIN=auto go test ./cmd/wfctl -count=1` green; `golangci-lint run --new-from-rev=origin/main ./cmd/wfctl/...` exit 0.

**Step 4: Commit.** `git commit -m "fix(wfctl): type-namespace import-all state IDs to avoid cross-type overwrite"`

Rollback: revert → import IDs return to domain-only (single-type imports unaffected). (Runtime-affecting via wfctl release.)

### Task 3: `Sanitize` allow-lists `Authority` keys

**Repo:** workflow **Files:** Modify `dns/record/sanitize.go`, `dns/record/sanitize_test.go`

**Step 1: Test.** `TestSanitizeStripsUnknownAuthorityKeys`: `Authority{"registrar_nameservers":...,"live_nameservers":...,"secret_token":"x"}` → after `Sanitize`, allow-list `{registrar_nameservers, live_nameservers}` kept, others removed; Records sanitization unchanged. Run → FAIL.

**Step 2: Implement.** After the Records pass, for each snapshot with non-nil `Authority`, delete keys ∉ allow-list `{registrar_nameservers, live_nameservers}`.

**Step 3:** `GOWORK=off GOTOOLCHAIN=auto go test ./dns/record -count=1` green; `golangci-lint run --new-from-rev=origin/main ./dns/...` exit 0.

**Step 4: Commit.** `git commit -m "feat(dns): sanitize allow-lists Snapshot.Authority keys"`

Rollback: revert.

### Task 4: Hover `EnumerateAll` lists domains for `infra.dns_delegation`

**Repo:** workflow-plugin-hover **Files:** Modify `internal/provider.go` (`EnumerateAll`), `internal/provider_test.go`

**Step 1: Test.** `TestEnumerateAll_DelegationListsDomains`: stub lister with 2 domains → `EnumerateAll(ctx,"infra.dns_delegation")` returns 2 `ResourceOutput`s, `ProviderID==domain.Name` for each; unknown type still errors `"resource type %q not supported"`. (Note: `o.Type` on the output is advisory — the authoritative resource type for state persistence is the `--type` flag threaded as `resourceType` into `buildResourceStateFromImport`, exercised by Task 2's `TestBuildResourceStateFromImport_TypeNamespacedID`; this test just verifies the domain listing — finding I-NEW-2.) Run → FAIL.

**Step 2: Implement.** Accept `infra.dns` AND `infra.dns_delegation` in the guard; for either, `ListDomains` → emit `ResourceOutput{ProviderID:d.Name, Type:resourceType}` (delegation NS fetched in Import — keep enumerate to one `ListDomains` call; no per-domain `domain_id` Output needed). Reject other types.

**Step 3:** `GOWORK=off GOTOOLCHAIN=auto go test ./internal -count=1` green.

**Step 4: Commit.** `git commit -m "feat(provider): EnumerateAll lists domains for infra.dns_delegation"`

### Task 5: Hover `Import` dual-fetches registrar + live NS (bypass Read)

**Repo:** workflow-plugin-hover **Files:** Modify `internal/provider.go` (`Import`), `internal/drivers/delegation.go`, `internal/drivers/delegation_test.go`, `internal/provider_test.go`

**Step 1: Tests.**
- `delegation_test.go` `TestDelegationReadForImport_DualNS`: stub client `GetDomainDelegation`→`["ns1.dnsimple.com"]`, stub public resolver→`["ns1.digitalocean.com"]`; the new `ReadForImport` returns `Outputs{"nameservers":[]any{"ns1.dnsimple.com"}, "registrar_nameservers":[]any{"ns1.dnsimple.com"}, "live_nameservers":[]any{"ns1.digitalocean.com"}}`. (`nameservers`==registrar=PRIMARY so existing `Diff`/`parseDelegationSpec`/`nameserversFromOutputs` stay consistent — no spurious drift, finding I-NEW-3.)
- `TestDelegationReadForImport_LiveLookupFailsGracefully`: public resolver errors → `live_nameservers` omitted; `nameservers`/`registrar_nameservers` still from registrar.
- `provider_test.go` `TestImport_DelegationUsesRegistrarNotLiveRead`: with a stub where registrar≠live, `HoverProvider.Import(ctx,"x.com","infra.dns_delegation")` returns a `ResourceState` whose `Outputs["registrar_nameservers"]`==registrar (proves it bypassed the live-first `Read`).

Run → FAIL.

**Step 2: Implement.**
- `internal/drivers/delegation.go`: add `func (d *DelegationDriver) ReadForImport(ctx, ref) (*interfaces.ResourceOutput, error)` — `GetDomainDelegation` (registrar, authoritative); `lookupPublicNameservers` best-effort (error → omit `live_nameservers`); build `Outputs{nameservers:registrar(primary), registrar_nameservers:registrar, live_nameservers:live}` (`[]any` via existing `nameserversToAny`; omit the unused `domain_id`). Do NOT touch `Read`.
- `internal/provider.go` `Import`: BEFORE the generic `d.Read`, `if resourceType == "infra.dns_delegation" { dd, ok := d.(*drivers.DelegationDriver); if ok { out, err := dd.ReadForImport(...); build+return ResourceState } }`. (`drivers` is already imported at provider.go:14; `p.ResourceDriver("infra.dns_delegation")` returns the `*DelegationDriver`.) Fall through to `d.Read` for `infra.dns`.

**Step 3: Verify.** `GOWORK=off GOTOOLCHAIN=auto go build ./... && go vet ./... && go test ./... -count=1` all green (existing delegation `Diff`/`Read` tests stay green — `nameservers` primary key preserved). `golangci-lint run --new-from-rev=origin/main` exit 0.

**Step 4: Commit.** `git commit -m "feat(provider): Import dual-fetches registrar+live NS for delegation"`

**Plugin runtime validation (Step 1b — plugin loading path):** live proof is Task 7's import re-run; locally confirm the plugin builds + `TestPluginBinaryEmbedsManifest` green. After merge → release **v0.5.1**.

Rollback: revert → delegation enumerate "not supported", Import unchanged; pin consumers back to v0.5.0.

### Task 6: import-dns.yml — Hover delegation import (second) + fail-gate

**Repo:** gocodealone-dns **Files:** Modify `.github/workflows/import-dns.yml`

**Step 1: Add the delegation import step** with `id: import-hover-delegation`, inserted **AFTER** the `Import Hover DNS zones` step (id: import-hover) and **BEFORE** the `Derive ownership from _workflow-dns-policy TXT (Hover)` step (finding I-1, so ownership derivation reads the merged portfolio). `continue-on-error: true`, same `HOVER_*` env, same `mkdir -p zones` + `wfctl infra import-all --config infra/hover.wfctl.yaml --provider hover --type infra.dns_delegation --format portfolio --plugin-dir data/plugins -o zones/hover.portfolio.json`. **`--format portfolio` is REQUIRED** (finding C-2; default is `state`). It reads the shared `.state/hover/` (now containing BOTH type-namespaced states after Task 2) → emits the MERGED records+authority portfolio, overwriting the records-only file from `import-hover`. Both Hover steps use the default `browser_profile_dir` (no override → shared `$XDG_STATE` profile → cookie reuse, finding m-2). No edit to `infra/hover.wfctl.yaml` (the provider registers both drivers at Initialize).

**Step 2: Update the fail-gate** (`Fail run if any provider import failed`, finding I-3): add `[ "${{ steps.import-hover-delegation.outcome }}" = "failure" ] && failed="$failed hover-delegation"`.

**Step 3: Verify.** `actionlint .github/workflows/import-dns.yml` (or confirm YAML parses) → exit 0. Real exercise is Task 7's dispatch.

**Step 4: Commit.** `git commit -m "ci(dns): import Hover delegation (both layers) + fail-gate"`

Rollback: revert → import returns to records-only.

### Task 7: Pin bumps + live catalog validation

**Repo:** gocodealone-dns **Files:** Modify `.github/wfctl-version`, `wfctl.yaml`, `.wfctl-lock.yaml`

**Precondition:** PR1 merged + workflow released (minor bump — behavioral); PR2 merged + hover **v0.5.1** released.

**Step 1:** Bump `.github/wfctl-version` to the new workflow release (minor — `FromResourceStates`/import-all behavioral change); bump hover v0.5.0→v0.5.1 in `wfctl.yaml` + `.wfctl-lock.yaml`.

**Step 2: Commit + open PR3.** `git commit -m "chore(dns): bump wfctl + hover v0.5.1 for delegation catalog"`

**Step 3: Multi-component live validation (real-consumer proof).** After PR3 merges, dispatch `import-dns.yml` on the self-hosted runner. Assert from the run log + the catalog PR's `zones/hover.portfolio.json`:
- both `import-hover` and `import-hover-delegation` succeed; fail-gate green.
- portfolio `schema == "workflow.dns-portfolio.export.v1"` (confirms `--format portfolio` took).
- snapshots carry `authority.registrar_nameservers` (non-empty) AND `records` (non-empty for at least the Hover-hosted domains) — proves the merge produced BOTH layers in ONE snapshot (the CRITICAL-1 regression check).
- exactly one snapshot per `(provider,domain)` (no duplicates).
- at least one domain shows `registrar_nameservers` ≠ a hover nameserver (delegated-away → its Hover records are staging/placeholder).
Expected: catalog-refresh PR shows both layers; no 401/ErrBotChallenge.

Rollback: revert pins (wfctl + hover→v0.5.0) + the Task 6 import step → catalog returns to records-only.
