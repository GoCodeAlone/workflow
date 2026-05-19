# Multi-Repo OSS-Readiness QoL Sweep — Design

**Date:** 2026-05-19
**Trigger:** A new external user adopted the workflow project. The codebase was built assuming OSS adoption but has not been audited end-to-end against that bar.
**Mode:** Autonomous (operator unavailable for approval; bypass approved per user instruction this session and ADR 0041).
**Revision:** v3 — round 1 fixed C-1/C-2/I-1..I-4; round 2 fixed C-2-regression/N-1..N-5; round 3 fixed F-1..F-4 count drift.

## Problem

`workflow` ships a real engine with 90+ module types, a registry of ~58 plugins, and 50 public plugin repos. A spot audit reveals four user-visible discoverability holes that a new external adopter hits immediately:

1. **No README at all** in 5 plugin repos (`aws`, `gcp`, `azure`, `tofu`, `ci-generator`).
2. **No `examples/` directory** in any plugin repo — no copy-pasteable starting point.
3. **No `CONTRIBUTING.md`** in any plugin repo — contribution path is invisible.
4. **License inconsistency** — 12 public GoCodeAlone-owned repos have `none` or non-MIT licenses despite the workflow project being MIT.
5. **Active-usage gap not surfaced.** Only 7 of 50 public plugins are validated by merged main-branch usage in an active GoCodeAlone project. The other 43 ship without any "experimental / unverified" signal.

## Goal

Bring the workflow ecosystem to a uniform OSS-readiness baseline by applying a single checklist to every relevant repo. Documentation, examples, license consistency, experimental-status markers, and trivial example-validation fixes only. **No new features.**

## Non-Goals

- New features, new module types, new step types.
- Rewriting accurate existing documentation.
- Live-deployment validation of examples (syntax + schema validation only).
- Touching upstream forks (`genkit`, `v8go`, `voxtral-tts`, `wgpu`, `yaegi`, `go-plugin`) — retain upstream licenses.
- Touching private plugins (security cluster: waf, security, sandbox, supply-chain, data-protection) — filed as P2 issues.

## Active-Usage Verification Matrix (revised per I-2)

Aggregated from **every `wfctl.yaml` and `.wfctl-lock.yaml` across all GoCodeAlone projects in the workspace, including worktree variants**:

```sh
find /Users/jon/workspace -maxdepth 5 -name 'wfctl.yaml' -exec grep -hE "name: workflow-plugin-" {} \; | sort -u
```

Three tiers of status:

- **`verified`** — pinned in a *merged* main-branch wfctl.yaml of a GoCodeAlone production project.
- **`experimental`** — public plugin, no verified production usage.
- **`deprecated`** — scheduled removal (not used in this sweep).

A fourth distinction — "pending pin in an open PR" — is **not** modeled as a separate status. Such plugins are classified `experimental` until the pin actually merges, then promoted by a one-line manifest PR.

### Verified (pinned in merged main-branch wfctl.yaml) — 7 plugins

| Plugin | Used in |
|--------|---------|
| `workflow-plugin-agent` | ratchet (alias `ratchet`, source = workflow-plugin-agent) |
| `workflow-plugin-audit-chain` | buymywishlist |
| `workflow-plugin-auth` | buymywishlist |
| `workflow-plugin-digitalocean` | buymywishlist, core-dump, workflow-compute |
| `workflow-plugin-eventbus` | buymywishlist |
| `workflow-plugin-payments` | buymywishlist |
| `workflow-plugin-twilio` | buymywishlist |

> Note on `workflow-plugin-analytics`: appears in 4 *unmerged* BMW worktrees (www-dns, wfctl-v0.60, admin-bootstrap-resolve, payment-intent-route) but is not yet in any merged main-branch wfctl.yaml. Per the three-tier rule above, classified `experimental` for this sweep; will be promoted by a follow-up manifest PR when BMW merges the pin.

### Unverified (public, no GoCodeAlone-project usage) — 43 plugins

All other public `workflow-plugin-*` repos. User-called-out: `aws`, `gcp`, `azure`, `tofu`, `ci-generator`. Full list derived from `gh repo list GoCodeAlone --visibility public` minus the verified set.

These will be **marked experimental** — see "Experimental Marker" section.

## License Audit

### Public repos with `none` license — add MIT

`workflow-plugin-cicd`, `workflow-plugin-infra`, `workflow-plugin-marketplace`, `workflow-plugin-platform`, `workflow-plugin-deployment`, `homebrew-tap`, `superpowers-marketplace`, `ratchet`, `ratchet-cli`, `claude-skills`, `rover`. (`EvoSim` deferred — appears abandoned.)

### Public repos with apache-2.0 — convert to MIT only if GoCodeAlone-original

- `workflow-plugin-migrations` — Apache-2.0. **Required pre-check before conversion:**
  ```sh
  cd workflow-plugin-migrations
  git log --diff-filter=A --name-only --pretty= -- '*.go' | sort -u | \
    xargs grep -l 'Copyright.*Apache\|github.com/golang-migrate/migrate' 2>/dev/null
  ```
  If matches indicate vendored or copied Apache-2.0 code, **abort** the conversion for that repo and file a tracking issue. Dependency-only usage (in `go.mod`) is fine under MIT — only inline Apache-2.0 source code blocks the relicense.

### Public repos with non-MIT/non-Apache — leave alone

- `go-plugin` (MPL-2.0 HashiCorp fork), `v8go` (BSD-3 fork), `yaegi`/`genkit`/`voxtral-tts`/`wgpu` (Apache-2.0 forks), `benchmark-it` / `terraform-aws-transit-gateway` / `github-action-matrix-outputs-{read,write}` (Apache-2.0 community).

## Experimental Marker — Implementation (revised per C-1, C-2)

The marker requires **a coordinated change across two repos** before any per-plugin manifest update can succeed:

### Step A. workflow-registry — schema update

File: `workflow-registry/schema/registry-schema.json` (verified — this is the actual filename, not `schema/plugin.json`).

The schema root and per-plugin entries have `"additionalProperties": false`. Adding `status` to a `manifest.json` **without** first updating the schema fails `ajv-cli` CI immediately. So:

1. Add an optional `status` property to the per-plugin entry schema:
   ```json
   "status": {
     "type": "string",
     "enum": ["verified", "experimental", "deprecated"],
     "description": "Active-usage verification status."
   }
   ```
2. Do NOT add `status` to `"required"` — keep it optional so existing manifests keep validating.
3. Run `ajv-cli validate --spec=draft2020 -s schema/registry-schema.json -d 'plugins/*/manifest.json'` locally to confirm no regressions before push.

### Step B. workflow — RegistryManifest Go struct update

Files in `workflow/cmd/wfctl/`:

1. `plugin_registry.go` — add field to the `RegistryManifest` struct (line 26 region):
   ```go
   Status string `json:"status,omitempty"` // verified | experimental | deprecated
   ```
2. `registry_validate.go` — add a `validPluginStatuses` map alongside `validPluginTiers` (line 34) **AND** add an enum-validation block inside `ValidateManifest` (mirroring the tier check at lines 63-66) so unknown status values are rejected:
   ```go
   if m.Status != "" && !validPluginStatuses[m.Status] {
       errs = append(errs, ValidationError{Field: "status", Message: fmt.Sprintf("must be one of: verified, experimental, deprecated (got %q)", m.Status)})
   }
   ```
3. Adjacent gap to close in the same PR (per round-2 review N-4): the existing `RegistryManifest` struct is missing a `Private bool \`json:"private,omitempty\"\`` field even though `manifest.json` files include `"private": false`. Add this field as part of Step B since the struct is already being touched.
4. Update tests in `multi_registry_test.go` to cover the new optional `status` field and the `private` field.

Without Step B, wfctl marketplace silently ignores the `status` field even after the manifests are populated. **Step A and Step B ship as one coordinated 2-PR pair** before any plugin-manifest PR.

### Step C. workflow-registry — manifest population

For each plugin, edit the correct file:

- **Correct file path:** `workflow-registry/plugins/<name>/manifest.json` (NOT `plugin.json` — the design v1 used the wrong filename).
- Add `"status": "verified"` for the 7 verified plugins.
- Add `"status": "experimental"` for the 43 unverified public plugins (analytics included pending BMW pin merge).

### Step D. Per-plugin-repo README banner

For each of the 43 unverified plugins, add at the top of `README.md`:

```markdown
> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but
> has not been validated in any active GoCodeAlone-internal production
> deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-<name>/issues/new)
> if you adopt it so we can promote it to **verified** status.
```

The verified plugins get a complementary banner: `✅ **Verified** — used in <project list>`.

## Per-Repo OSS-Readiness Checklist

| # | File / Property | Criteria |
|---|-----------------|----------|
| 1 | `LICENSE` | MIT for public GoCodeAlone-owned non-fork repos |
| 2 | `README.md` | Tagline + status banner + build badge + license badge + 60-second quickstart + install + link to docs |
| 3 | `CHANGELOG.md` | Keep-a-Changelog header + latest released tag's entry |
| 4 | `CONTRIBUTING.md` | Link to workflow/CONTRIBUTING.md + repo-specific build/test commands |
| 5 | `examples/minimal/` | At least one example config validated with `wfctl validate --skip-unknown-types` |
| 6 | `.github/ISSUE_TEMPLATE/` | Minimum `bug_report.md` + `feature_request.md` |
| 7 | `.github/PULL_REQUEST_TEMPLATE.md` | Short, links to CONTRIBUTING |
| 8 | Manifest at `workflow-registry/plugins/<name>/manifest.json` | Correct latest tag + `status` field |
| 9 | Build green | `go build ./...` and `go vet ./...` clean |
| 10 | Example validation | `wfctl validate --skip-unknown-types examples/**/*.yaml` passes |

### Per-plugin-category validation flag (per I-1)

- **IaC plugins** (aws, gcp, azure, do, tofu) and **module-type-heavy plugins**: examples MUST use `wfctl validate --skip-unknown-types` since they introduce non-builtin module types. The bare command fails on `unknown module type` errors.
- **Step-only plugins** (payments, twilio, audit-chain, …): the bare `wfctl validate` works because step types are not type-checked the same way.

The reviewer agent uses `--skip-unknown-types` uniformly to avoid per-category branching.

### Pre-existing lint failures (per I-3)

`CONTRIBUTING.md` requires `golangci-lint run` for code PRs. For docs-only QoL PRs:

- Do NOT run `golangci-lint`; doc-only changes cannot introduce lint failures.
- If pre-existing `go build` or `go vet` failures exist on a target repo's default branch, note in the PR description with a follow-up issue link, but do not fix in this sweep.

## Scope Tiers

### P0 (must complete this session) — engine + registry + verified plugins (9 repos, 10 PRs)

1. `workflow` (1 PR) — README polish, examples index, getting-started cross-references, RegistryManifest Go struct update (Step B: add `Status` + `Private` fields + ValidateManifest enum block)
2. `workflow-registry` (2 PRs) — PR-R1 schema update (Step A); PR-R2 manifest population (Step C: 50 manifests), gated on PR-R1
3. `workflow-plugin-digitalocean` — full checklist, banner `verified`
4. `workflow-plugin-payments` — full checklist, banner `verified`
5. `workflow-plugin-agent` — full checklist, banner `verified`
6. `workflow-plugin-audit-chain` — full checklist, banner `verified`
7. `workflow-plugin-auth` — full checklist, banner `verified`
8. `workflow-plugin-eventbus` — full checklist, banner `verified`
9. `workflow-plugin-twilio` — full checklist, banner `verified`

(`workflow-plugin-analytics` is in tier P2 — experimental until BMW pin merges.)

### P1 (best-effort this session) — user-called-out unverified plugins (5 repos)

11. `workflow-plugin-aws` — **add README** + examples + experimental banner
12. `workflow-plugin-gcp` — **add README** + examples + experimental banner
13. `workflow-plugin-azure` — **add README** + examples + experimental banner
14. `workflow-plugin-tofu` — **add README** + examples + experimental banner
15. `workflow-plugin-ci-generator` — **add README** + examples + experimental banner

### P2 (mass-marker sweep — minimal change per repo) — remaining 38 unverified public plugins (37 + `workflow-plugin-analytics`)

For each: open one minimal PR doing only:
- Experimental banner in README (or create README from a template if missing)
- LICENSE check (add MIT if missing)
- Reference back to the workflow-registry manifest update (which carries the `status` field)

Per-repo follow-up issue filed in workflow-registry for deeper docs/examples work.

### P3 (license-only sweep) — non-plugin public GoCodeAlone repos without MIT (6 repos)

`homebrew-tap`, `superpowers-marketplace`, `ratchet`, `ratchet-cli`, `claude-skills`, `rover`.

## Execution Model

Lead-orchestrated team, subagent-driven-development pattern, per-repo worktree-isolated agents.

```
team-lead (main thread)
├── doc-impl-1 (Sonnet 4.6) — workflow + workflow-registry (Steps A/B/C)
├── doc-impl-2 (Sonnet 4.6) — P0 plugins (4)
├── doc-impl-3 (Sonnet 4.6) — P0 plugins (4)
├── doc-impl-4 (Sonnet 4.6) — P1 plugins (5)
├── doc-impl-5 (Haiku 4.5)  — P2 mass-marker (38 repos incl. analytics)
├── doc-impl-6 (Haiku 4.5)  — P3 license-only (6 repos)
└── reviewer    (Sonnet 4.6) — checklist audit + PR review
```

- **One worktree per repo.** Per `feedback_per_agent_worktree_per_task_pr`.
- **One PR per repo, with one exception (per round-2 review N-2):** `workflow-registry` ships TWO PRs to honor the sequencing constraint:
  - **PR-R1:** schema change only (`schema/registry-schema.json` adds optional `status` enum). Lands FIRST.
  - **PR-R2:** manifest population (50 `plugins/*/manifest.json` updates writing the `status` field). Gated on PR-R1 merge.
  - All other repos remain one-PR-per-repo.
- **Sequencing constraint (per C-1):** PR-R1 (workflow-registry schema) AND the workflow Go-struct PR (Step B) MUST merge **before** PR-R2 (manifest population). Per-plugin-repo banner PRs are README-only and have no schema dependency; they may be opened in parallel from the start and merged independently. PR-R1 and the workflow Go-struct PR can land in either order.
- **Commits:** Conventional — `docs: add README and examples (QoL sweep)`.

### Review Tiers (per I-4)

`CONTRIBUTING.md` requires "All PRs require at least one review before merging." The admin-merge pattern in this sweep is a deliberate deviation under autonomous-mandate (ADR 0041). To soften the deviation, per-priority review tiers:

| Priority | Pre-merge gate |
|----------|----------------|
| P0 (9 repos, 10 PRs) | Reviewer-agent audit + Copilot review pass + CI green; then admin-merge |
| P1 (5 repos)  | Reviewer-agent audit + Copilot review pass + CI green; then admin-merge |
| P2 (38 repos) | Reviewer-agent audit + CI green; admin-merge (Copilot pass desirable but not required because PRs are template-driven one-liners) |
| P3 (6 repos)  | Reviewer-agent audit + CI green; admin-merge (license-only) |

Per `feedback_copilot_review_settle_window`, allow ~10 minutes after `requested_reviewers POST` for Copilot to surface findings before admin-merge.

## Per-PR Validation

Each implementer runs locally before push:

```sh
go build ./...
go vet ./...
wfctl validate --skip-unknown-types examples/**/*.yaml
```

Reviewer re-runs on the worktree before marking ready.

## Assumptions (load-bearing)

- `wfctl validate` exists and validates plugin YAML against schema. Verified — `docs/WFCTL.md`.
- `wfctl validate --skip-unknown-types` accepts plugin-introduced module types. Verified by reading `cmd/wfctl/validate.go`.
- The verified-plugin matrix (7) is complete after broad-scan re-sample. **Risk reduced but not zero.** If a project I didn't sample uses one of the "experimental" plugins, mitigation = revert via one-line manifest change.
- Adding `status` field to `registry-schema.json` is additive-safe because `additionalProperties: false` is enforced at the per-plugin entry level but the field is optional. Verified by reading the schema.
- The user is OK with PRs being admin-merged autonomously per autonomy grant. ADR 0041 records.
- Updating `RegistryManifest` Go struct is non-breaking because Go's `encoding/json` ignores unknown fields by default. Verified — no `DisallowUnknownFields` call in wfctl manifest parsing.

## Top Self-Challenge Doubts

1. **Scope risk.** 58 PRs across 50+ repos in one session is at the upper edge of feasibility. Mitigation: P0/P1 PRs get deep treatment (15 repos); P2/P3 are template-driven mass-marker (43 repos × ~5 min/PR if dispatched in parallel). If we run short, P3 deferred to a follow-up session — lowest-impact tier.
2. **Reverse-correction discoverability.** If we mark a plugin "experimental" wrongly and the user already adopted it, they see the banner and worry. The banner explicitly invites the user to file an issue to promote to `verified` — the correction path is one-line + immediately visible.
3. **Apache-2.0 license-conversion risk for workflow-plugin-migrations.** Specified verification command included; if the audit finds vendored Apache code, the conversion is aborted and the repo stays Apache-2.0.

## Rollback

Per-PR rollback: `git revert <merge-sha>` + revert PR.

Schema/Go-struct rollback: there are now three PRs in the schema-feature group — PR-R1 (workflow-registry schema), workflow Go-struct PR (Step B), PR-R2 (workflow-registry manifest population). Revert order: PR-R2 first (so manifests no longer reference `status`), then PR-R1 (schema), then workflow Go-struct PR (struct). Because `status` is optional throughout, any intermediate state is benign — struct + schema unaware of manifests is fine; struct aware + schema unaware would fail ajv CI but only on a newly-merged manifest population, which is why PR-R1 lands before PR-R2.

No runtime config touched. Documentation + manifest-additive only. Blast radius = "tutorial reader confused", not "production broken". Does not trigger runtime-launch-validation.

## Success Criteria

- 10 P0 PRs merged (9 repos; workflow-registry splits into 2).
- 5 P1 PRs merged.
- 38 P2 mass-marker PRs merged (37 + analytics until BMW pin lands).
- 6 P3 license PRs merged.
- All 43 unverified public plugins show `status: experimental` + README banner.
- All 7 verified public plugins show `status: verified` + README banner.
- All public GoCodeAlone-owned non-fork repos carry MIT (or, for workflow-plugin-migrations, retain Apache-2.0 with documented reason).
- Post-sweep retro at `docs/retros/2026-05-19-multi-repo-qol-sweep.md`.

## Out of Scope (Explicit Deferrals)

- Private plugins (security cluster, authz-ui, cardgame/dnd content, cloud-ui).
- workflow-cloud, workflow-cloud-ui, modular, workflow-editor, workflow-vscode, workflow-jetbrains — each warrants own sweep.
- Live-deployment example validation — needs CI with credentials.
- Translation / i18n.
- Per-plugin deep documentation for the 38 P2 plugins (tracking issues filed).
- `wfctl plugin verify` subcommand (future ergonomic improvement).
- GitHub topic tagging — supplementary, easy follow-up.

## References

- ADR 0041 — experimental-status marker rationale
- `feedback_per_agent_worktree_per_task_pr`, `feedback_local_image_launch_validation`, `feedback_no_speculative_remote_ci`, `feedback_continuous_autonomous_phases`, `feedback_admin_override_pr_merge`, `feedback_check_review_comments_before_merge`, `feedback_copilot_review_settle_window`, `feedback_docs_pr_verify_against_codebase`, `feedback_check_versions_actively`
- `docs/PLUGIN_AUTHORING.md`, `docs/BUILDING_APPS_GUIDE.md`
- `workflow-registry/schema/registry-schema.json`
- `workflow/cmd/wfctl/plugin_registry.go`, `workflow/cmd/wfctl/registry_validate.go`

## Adversarial Review Findings — disposition

### Round 1 (v1 → v2)

| Finding | Severity | Disposition |
|---------|----------|-------------|
| C-1 schema strict + Go struct missing | Critical | **Fixed.** Steps A + B specified explicitly; sequencing constraint added. |
| C-2 wrong filename plugin.json | Critical | **Fixed.** All references corrected to `manifest.json` / `registry-schema.json`. |
| I-1 wfctl validate flag | Important | **Fixed.** `--skip-unknown-types` mandated; per-category guidance added. |
| I-2 verified-set incomplete | Important | **Fixed.** Re-sampled all workspace wfctl.yaml. |
| I-3 lint mandate vs doc-only PR | Important | **Fixed.** Explicit doc-only carve-out. |
| I-4 review-required precedent | Important | **Fixed.** Per-priority review tiers added. |
| m-1..m-4 | Minor | **Fixed/clarified.** |

### Round 2 (v2 → v3)

| Finding | Severity | Disposition |
|---------|----------|-------------|
| C-2 (regression) plugin.json survives in ADR | Critical | **Fixed in v3.** ADR 0041 corrected to `manifest.json` + `registry-schema.json`. |
| N-1 analytics worktree-only classification | Important | **Fixed in v3.** Analytics demoted to `experimental` until BMW pin merges; verified set is now 7 not 8. |
| N-2 one-PR-per-repo vs two-step sequencing | Important | **Fixed in v3.** workflow-registry splits into PR-R1 (schema) + PR-R2 (manifests); explicit exception to one-PR-per-repo rule. |
| N-3 ADR stale count 7 vs 8 | Minor | **Fixed in v3.** ADR count updated to 7 verified + 43 experimental. |
| N-4 RegistryManifest missing Private field | Minor | **Fixed in v3.** Step B scope expanded to add `Private bool` field alongside `Status`. |
| N-5 validate enum block not wired | Minor | **Fixed in v3.** Explicit code snippet for ValidateManifest block added. |
