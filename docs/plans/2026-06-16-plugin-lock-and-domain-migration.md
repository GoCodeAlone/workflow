# Plugin Lock And Domain Migration Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Ship safe plugin-lock CI semantics, DNS ownership/TXT fixes, and continue Cloudflare/Blackorchid migration through verified hosting and DNS cutover.

**Architecture:** Core `wfctl` owns manifest/lock install modes. Provider plugins own DNS record behavior. `gocodealone-dns` remains the declarative intent/catalog repo. Blackorchid becomes a private multisite content repo and DNS changes wait for route proof.

**Tech Stack:** Go, wfctl, Workflow external plugins, GitHub Actions, Cloudflare SDK, Hover/Namecheap/DigitalOcean DNS catalogs, gocodealone-multisite content repo contract.

**Base branch:** `workflow: main`; plugin/content repos use their default branch.

---

## Scope Manifest

**PR Count:** 7
**Tasks:** 7
**Estimated Lines of Change:** ~1800

**Out of scope:**
- Cloudflare Registrar transfer-in.
- Namecheap nameserver write automation unless already implemented and verified.
- Replacing Wix-only dynamic widgets beyond static capture; file follow-up instead.
- Blackorchid DNS cutover before multisite preview route and content proof pass.
- Rewriting the DNS ownership policy format away from `_workflow-dns-policy`.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | wfctl plugin lock CI semantics | Task 1 | `codex/plugin-lock-ci` |
| 2 | Cloudflare TXT quote normalization | Task 2 | `codex/cloudflare-txt-quote` |
| 3 | Infra DNS ownership TXT injection | Task 3 | `codex/infra-dns-policy-txt` |
| 4 | gocodealone-dns lock/intents and safe cutovers | Task 4 | `codex/dns-lock-intents` |
| 5 | Blackorchid content repo | Task 5 | `codex/blackorchid-content` |
| 6 | Multisite Blackorchid tenant route | Task 6 | `codex/multisite-blackorchid` |
| 7 | Blackorchid DNS cutover | Task 7 | `codex/blackorchid-dns-cutover` |

**Status:** Locked 2026-06-16T15:06:45Z

### Task 1: wfctl plugin lock CI semantics

**Files:**
- Modify: `cmd/wfctl/plugin.go`
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_lock.go`
- Modify: `cmd/wfctl/plugin_lockfile.go`
- Modify: `cmd/wfctl/config_validate.go`
- Modify: `config/wfctl_manifest.go` or nearest lockfile model file
- Test: `cmd/wfctl/plugin_install_lockfile_test.go`
- Test: `cmd/wfctl/plugin_lock_test.go`
- Test: `cmd/wfctl/config_migrate_test.go`
- Docs: `docs/WFCTL.md`

**Steps:**
1. Add failing tests:
   - `wfctl plugin install` with stale lock updates lock provenance and installs new manifest pin.
   - `wfctl plugin install --locked` fails stale/missing provenance and does not write.
   - `wfctl plugin ci` is equivalent to locked install.
   - `wfctl config validate --locked` fails stale manifest/lock digest.
2. Add canonical digest helpers:
   - manifest digest over sorted typed plugin entries: `name`, `version`, `source`.
   - lock digest over sorted typed lock plugin entries excluding digest/provenance fields.
3. Extend lockfile model with provenance fields.
4. Update `runPluginLockFromManifestWithOptions` to write provenance.
5. Update no-arg `runPluginInstall`:
   - default: if `wfctl.yaml` exists and lock is stale/missing provenance, run lock first, then install.
   - `--locked`: validate, install, no writes.
6. Add `plugin ci` dispatcher and usage text.
7. Add `config validate --locked` check and docs.
8. Run focused tests, then package tests.
9. Open PR, monitor CI/review, merge after green.
10. Tag/release workflow; verify `wfctl update` can install the release.

**Verify:**
- `GOWORK=off go test ./cmd/wfctl -run 'TestPlugin(Install|Lock)|TestConfigValidate' -count=1` → PASS.
- `GOWORK=off go test ./cmd/wfctl ./config -count=1` → PASS.
- Built `wfctl plugin ci --help` prints usage and exits 0.
- Runtime fixture: stale lock + bumped `wfctl.yaml`:
  - `wfctl plugin install --locked` exits non-zero with stale-lock message.
  - `wfctl plugin install` rewrites `.wfctl-lock.yaml`.
- Rollback: revert PR and pin prior workflow release in consuming repos.

### Task 2: Cloudflare TXT quote normalization

**Files:**
- Repo: `workflow-plugin-cloudflare`
- Modify: `internal/drivers/dns.go`
- Test: `internal/drivers/dns_test.go`
- Docs: `README.md` if DNS behavior docs mention TXT values

**Steps:**
1. Add failing tests:
   - TXT desired `google-site-verification=abc` sends `"google-site-verification=abc"` in create/update payload.
   - current quoted vs desired unquoted TXT records match.
   - non-TXT records are unchanged.
2. Implement TXT quote normalization helper.
3. Use helper in `newRecordBody` and `editRecordBody`.
4. Canonicalize TXT values quote-insensitively in `canonicalData`.
5. Run tests and open PR.
6. Merge and release plugin.

**Verify:**
- `GOWORK=off go test ./internal/drivers -run 'TestDNSDriver_.*TXT|Test.*TXT' -count=1` → PASS.
- `GOWORK=off go test ./...` → PASS.
- Release artifact has plugin binary + `plugin.json`.
- Rollback: pin previous Cloudflare plugin version.

### Task 3: Infra DNS ownership TXT injection

**Files:**
- Repo: `workflow-plugin-infra`
- Modify: DNS intent compiler files containing `records_policy`, `forward_to`, and Cloudflare stage generation
- Test: corresponding DNS intent compiler tests
- Docs: `README.md` and DNS intent docs if command surface changes

**Steps:**
1. Add failing tests:
   - Cloudflare-generated `infra.dns` includes `_workflow-dns-policy.<domain>` TXT when intent has owner.
   - Unknown owner does not silently emit a misleading owner TXT.
   - Injected TXT survives `records_policy: discard_parked`.
   - Generated policy TXT value is quoted-safe for Cloudflare.
2. Add explicit optional `owner` field to domain intent if absent.
3. Resolve owner from domain intent first; optionally from committed ownership mirror only when not `unknown`.
4. Inject TXT record with short TTL.
5. Update command/report output to mention ownership marker inserted or skipped.
6. Run tests, open PR, merge, release plugin.

**Verify:**
- `GOWORK=off go test ./... -run 'Test.*DNS.*Intent|Test.*Cloudflare.*Stage|Test.*Policy' -count=1` → PASS.
- `wfctl dns intent reconcile --mode plan` on fixture emits `_workflow-dns-policy`.
- Rollback: pin previous infra plugin; generated configs no longer inject marker.

### Task 4: gocodealone-dns lock/intents and safe cutovers

**Files:**
- Repo: `gocodealone-dns`
- Modify: `.github/wfctl-version`
- Modify: `wfctl.yaml`
- Modify: `.wfctl-lock.yaml`
- Modify: `.github/workflows/reconcile-domain-intent.yml`
- Modify: `.github/workflows/import-dns.yml`
- Modify: `domains.json`
- Docs: `README.md`, `docs/runbooks/cloudflare-domain-migration.md`

**Steps:**
1. Bump workflow, Cloudflare plugin, and infra plugin pins to released versions from Tasks 1-3.
2. Run local `wfctl plugin install` to refresh lock provenance.
3. Replace CI plugin install steps with `wfctl plugin ci`.
4. Add explicit owners to existing `domains.json` entries where known.
5. Add low-risk intent entries:
   - `epiccardbattles.online`: Hover → Cloudflare, `discard_parked`.
   - `gigbagg.rocks`: Hover → Cloudflare, `discard_parked`.
6. Inspect and document `gigbagg.com`; do not cut if DigitalOcean A record is intentional.
7. Add `buymywishlist.com` intent only after generated plan includes quoted TXT and policy TXT; preserve Google MX/TXT.
8. Plan applies via GitHub Actions; apply low-risk domains first.
9. Run import refresh and commit PR with updated portfolios/ownership.

**Verify:**
- `wfctl config validate --manifest wfctl.yaml --locked` → PASS.
- `wfctl plugin ci --plugin-dir data/plugins` → PASS.
- `gh workflow run reconcile-domain-intent.yml ... -f mode=plan` for each candidate → plan artifact shows expected records/delegation.
- Apply only for domains whose plan matches expected old NS and record policy.
- `dig +short NS <domain> @1.1.1.1` after apply → Cloudflare NS for cutover domains.
- Rollback: rerun intent with prior nameservers or restore registrar NS manually from committed portfolio.

### Task 5: Blackorchid content repo

**Files:**
- New private GitHub repo: `GoCodeAlone/blackorchid-tributeband-site`
- Create: `multisite.yaml`
- Create: static content files under repo root or `dist/`
- Create: `.github/workflows/release.yml`
- Create: `README.md`
- Optional: `content-audit.md`

**Steps:**
1. Create private repo.
   - Verify name availability first.
   - Verify resulting repo visibility is `PRIVATE`.
2. Copy content-repo template from `gocodealone-multisite/content-repo-template`.
3. Capture current public Blackorchid site content/assets without credentialed scraping.
4. Normalize into static output.
5. Add `multisite.yaml`:
   - `tenant_id: blackorchid-tributeband`
   - `static_dir: dist` or chosen output dir
   - domains for apex and `www`
6. Add release workflow using multisite ingest contract.
7. Add repo secrets needed for ingest.
   - Verify with `gh secret list --repo GoCodeAlone/blackorchid-tributeband-site`.
8. Tag first content release.
9. Record gaps in `content-audit.md` and file issues for dynamic widgets.

**Verify:**
- Static site opens locally from generated directory.
- `wfctl validate --skip-unknown-types multisite.yaml` or schema validation equivalent → PASS.
- `gh repo view GoCodeAlone/blackorchid-tributeband-site --json visibility` → `PRIVATE`.
- `gh secret list --repo GoCodeAlone/blackorchid-tributeband-site` includes required ingest secret names without printing values.
- GitHub release contains `site.tar.gz`.
- No private credentials or machine paths committed.
- Rollback: delete/disable release tag; multisite keeps old/no Blackorchid tenant content.

### Task 6: Multisite Blackorchid tenant route

**Files:**
- Repo: `gocodealone-multisite`
- Modify: `deploy.yaml` or tenant/content mapping config if static bootstrap map is still required
- Modify: docs/runbooks if tenant onboarding needs command improvements
- Test: existing multisite tenant/domain tests as needed

**Steps:**
1. Ensure host can map `GoCodeAlone/blackorchid-tributeband-site` to tenant `blackorchid-tributeband`.
2. Add tenant/domain registration via existing admin API/runbook or code/config if required.
   - If admin auth/bootstrap is not available, stop before DNS work and file/execute the onboarding automation follow-up under F4.
3. Ensure content token can read private repo release tarball.
4. Trigger content ingest.
5. Verify preview route, apex host header route, and `www` host header route without DNS cutover.
6. Add follow-up issues for missing onboarding automation if manual steps remain.

**Verify:**
- `GOWORK=off go test ./cmd/multisite-host ./... -run 'Test.*Tenant|Test.*Domain|Test.*Content|Test.*Ingest' -count=1` → PASS or narrowed equivalent.
- Preview URL returns 200 and expected Blackorchid content.
- Host-header probe for `blackorchid-tributeband.com` against multisite app returns expected content.
- Admin/tenant operation evidence is captured without printing credentials.
- Rollback: remove tenant/domain mapping or restore prior content mapping; no DNS changed yet.

### Task 7: Blackorchid DNS cutover

**Files:**
- Repo: `gocodealone-dns`
- Modify: `domains.json`
- Modify: Cloudflare staging/generated config if committed
- Modify: `docs/runbooks/cloudflare-domain-migration.md`
- Generated: updated portfolios/ownership after import

**Steps:**
1. Add Blackorchid intent only after Task 6 proof:
   - DNS host Cloudflare.
   - records point to verified multisite target.
   - preserve or replace mail records intentionally.
   - `nameserver_cutover` only if registrar support and preflight pass.
2. Generate plan and inspect Cloudflare records plus policy TXT.
3. Apply DNS staging.
4. Change nameservers only after Cloudflare records and multisite route proof pass.
5. Run import refresh and commit catalog state.
6. Monitor HTTP and DNS propagation.

**Verify:**
- `dig +short NS blackorchid-tributeband.com @1.1.1.1` → expected Cloudflare NS after cutover.
- `curl -I https://blackorchid-tributeband.com` → 200/301 to expected route; no Wix response headers.
- `curl -I https://www.blackorchid-tributeband.com` → 200/301 expected.
- Cloudflare zone contains quoted TXT policy marker.
- Rollback: restore Wix nameservers `ns12.wixdns.net,ns13.wixdns.net` from portfolio and verify Wix route.

## Planned Follow-Up Phase Queue

| id | trigger | follow-up |
|---|---|---|
| F1 | `gigbagg.com` DigitalOcean A record proves intentional | create separate content/hosting intent before DNS cutover. |
| F2 | Namecheap delegation driver missing | plan provider intent support for Namecheap NS cutover. |
| F3 | Blackorchid Wix widget not statically portable | create multisite/CMS replacement issue and defer widget behind content audit. |
| F4 | Multisite tenant onboarding still manual | add plugin/CLI command to onboard tenant + content repo without bespoke scripts. |
| F5 | DNS policy owners remain unknown | add owner bootstrap command/report in infra DNS intent. |
