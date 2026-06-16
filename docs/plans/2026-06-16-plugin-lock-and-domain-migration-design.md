# Plugin Lock And Domain Migration Design

## Goal

Fix `wfctl` plugin lock drift semantics, close DNS ownership/TXT staging gaps,
and continue the Cloudflare migration path including Blackorchid's move from
Wix-hosted DNS/content to `gocodealone-multisite`.

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/PLAN_PLATFORM_ABSTRACTION.md`, `gocodealone-dns/README.md`, `gocodealone-multisite/SPEC.md`.

| guidance | design response |
|---|---|
| `wfctl` changes update docs/tests with CLI behavior | Add tests + `docs/WFCTL.md` for install/ci/lock validation. |
| Plan before apply for infrastructure | DNS cutovers stay behind generated plans and guarded GitHub Actions applies. |
| Plugin-first but core keeps bootstrap-critical CLI behavior | `wfctl plugin ci/install --locked` stays core; provider DNS behavior stays in plugins. |
| DNS ownership source is `_workflow-dns-policy.<zone>` | Generated Cloudflare DNS configs must include that TXT when intent knows an owner. |
| Multisite content lives in independent repos | Blackorchid content becomes a private content repo that publishes a bundle to the host. |

## Design

### A. Plugin Lock Semantics

Choice: package-manager split.

- Local/dev: `wfctl plugin install` reads `wfctl.yaml`; if lock provenance is stale/missing, regenerate `.wfctl-lock.yaml`; install from refreshed lock.
- CI/frozen: `wfctl plugin ci` and `wfctl plugin install --locked` validate lock provenance + integrity, install exactly from lock, and never write.
- Targeted update: keep `wfctl plugin update --version <v> <name>` and document it as direct pin + relock.
- Alerting:
  - `source_manifest_sha256`: SHA-256 of canonical JSON built from sorted typed
    `wfctl.yaml` plugin entries only: `name`, `version`, `source`.
  - `lockfile_sha256`: SHA-256 of canonical JSON built from sorted typed
    `.wfctl-lock.yaml` plugin entries excluding digest/provenance fields.
  - stale manifest digest → local install warns + relocks; CI fails.
  - stale lockfile digest → CI fails; local install rewrites only through the lock writer.
  - legacy lockfiles without digest fields are migratable: local install writes
    digests; CI fails with "run `wfctl plugin install` or `wfctl plugin lock`".

See `decisions/0050-plugin-lock-install-modes.md`.

### B. DNS Cloudflare Staging

- Cloudflare plugin quote-normalizes TXT content for create/update.
- TXT diff logic compares quoted/unquoted forms canonically to avoid churn.
- Infra DNS intent injects `_workflow-dns-policy.<domain>` TXT into Cloudflare-generated zone records when owner intent is known.
- Continue using existing `_workflow-dns-policy`, not `_dns-mgmt` or per-record TXT markers.
- `gocodealone-dns` workflows switch CI plugin install to `wfctl plugin ci` once released.

### C. Domain Migration Order

Current public NS evidence on 2026-06-16:

| domain | live NS | action |
|---|---|---|
| `buymywishlist.net` | Cloudflare | active; verify redirect remains. |
| `codingsloth.io` | Cloudflare | active; monitor. |
| `coredump.computer` | Cloudflare | active; monitor. |
| `epiccardbattles.com` | Cloudflare | active; monitor. |
| `epiccardbattles.online` | Hover | low-risk Hover parked cutover candidate. |
| `gigbagg.rocks` | Hover | low-risk Hover parked cutover candidate. |
| `gigbagg.com` | Hover, but DigitalOcean has an A record | inspect intent before cutover. |
| `buymywishlist.com` | DigitalOcean | cut only after TXT quote + policy TXT release; Google MX/TXT verification must match. |
| `gocodealone.com` | Namecheap | defer until Namecheap delegation write path verified. |
| `blackorchid-tributeband.com` | Wix | content/hosting migration before DNS. |

### D. Blackorchid Multisite Migration

Target shape:

- New private repo: `GoCodeAlone/blackorchid-tributeband-site`.
- Static content repo contract from `gocodealone-multisite/docs/content-repo-contract.md`.
- `multisite.yaml` binds tenant `blackorchid-tributeband`, domains:
  - `blackorchid-tributeband.com`
  - `www.blackorchid-tributeband.com`
- Source capture:
  - Pull public site content/assets from current Wix site where the operator owns
    or has rights to the material; do not bypass access controls or scrape
    non-public assets.
  - Preserve pages, images, CSS, links, metadata, and any embedded booking/contact affordances.
  - Flag any Wix-only dynamic form/widget as follow-up content work; do not fake it silently.
- Publish path:
  - Add content repo release workflow.
  - Set ingest HMAC secret + ensure multisite content token can read the private repo.
  - Register tenant/domain in multisite.
  - Verify preview route first.
  - Only then change DNS from Wix to multisite target records.

## Security Review

| area | treatment |
|---|---|
| lockfile drift | checksums detect accidental/manual drift; not a malicious tamper-proof boundary. |
| CI writes | `plugin ci`/`--locked` must never write manifest or lockfile. |
| secrets | Blackorchid repo receives only content release HMAC; multisite fetch token remains in host repo/app secrets. |
| private content | Repo private; host fetches release tarballs with `MULTISITE_CONTENT_REPO_TOKEN`. |
| content rights | Capture only operator-owned public site material; no credentialed scraping or third-party asset laundering. |
| DNS cutover | No cutover before Cloudflare records + ownership TXT + route proof are verified. |

## Infrastructure Impact

- Workflow core: CLI behavior + lockfile schema metadata.
- Provider plugins: Cloudflare TXT normalization; infra DNS intent record injection.
- `gocodealone-dns`: workflow command changes and domain intents.
- GitHub: new private Blackorchid content repo.
- `gocodealone-multisite`: tenant/domain registration and possibly content repo mapping.
- DNS: nameserver/record changes only after generated plan + workflow apply.

## Multi-Component Validation

| boundary | proof |
|---|---|
| `wfctl.yaml` → lock → install | unit tests + `wfctl plugin ci` fixture run. |
| Cloudflare desired TXT → SDK payload → diff | Cloudflare plugin tests; staged config dry-run. |
| infra DNS intent → generated zone records | infra plugin tests; generated YAML inspection. |
| DNS repo workflow → plugin CI install | GitHub Actions run on branch. |
| Blackorchid content repo → multisite ingest → route | release artifact, ingest webhook, preview HTTP checks. |
| route proof → DNS cutover | pre/post public DNS and HTTP checks. |

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | Canonical plugin manifest digest can ignore YAML formatting | Loader may preserve fields not in typed model | Digest only typed plugin entries; docs state scope. |
| A2 | CI can migrate from `plugin install` to `plugin ci` without plugin changes | Some workflows rely on implicit relock | Fail message points to local `plugin install`/`plugin lock`. |
| A3 | Cloudflare accepts quoted TXT content via SDK | API may return unquoted content | Compare TXT quote-insensitively. |
| A4 | Owner intent is available for policy TXT injection | Current `ownership/*.yaml` says unknown | Use explicit intent owner or leave unknown and block strict gate. |
| A5 | Blackorchid can be captured as static content | Wix widgets may be dynamic | Preserve static shell; create follow-up for dynamic replacement. |
| A6 | Existing lockfiles may lack digest fields | CI would fail every repo immediately | Local install performs one-time digest bootstrap; CI error gives exact repair command. |

## Rollback

- `wfctl` CLI: revert PR; workflows return to `wfctl plugin install`; relock command still exists.
- Cloudflare plugin: pin previous plugin in `wfctl.yaml`, run relock, reinstall.
- Infra plugin: pin previous plugin; generated configs stop injecting policy TXT.
- DNS repo: revert domain intent changes before apply; after apply, run generated reverse delegation/record plan or manually restore previous NS.
- Blackorchid: keep Wix NS until multisite verified. If DNS cutover fails, restore Wix NS from committed portfolio.

## Planned Phases

| phase | scope | auto-continuation trigger |
|---|---|---|
| 1 | `wfctl` lock semantics + docs/tests/release | PR merged and release available. |
| 2 | Cloudflare TXT normalization + release | Phase 1 released or independent if no dependency. |
| 3 | Infra policy TXT injection + release | Phase 2 released or canonical TXT compare in place. |
| 4 | `gocodealone-dns` workflow/intents + safe Hover cutovers | Phases 1-3 released and pinned. |
| 5 | Blackorchid content repo + multisite preview | Content capture complete and repo secrets available. |
| 6 | Blackorchid DNS cutover | Preview route + content validation pass. |
| 7 | Follow-ups discovered during phases | File issue + add to next phase unless scope-lock requires amendment. |
