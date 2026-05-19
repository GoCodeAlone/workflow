# Multi-Repo OSS-Readiness QoL Sweep — Design

**Date:** 2026-05-19
**Trigger:** A new external user adopted the workflow project. The codebase was built assuming OSS adoption but has not been audited end-to-end against that bar.
**Mode:** Autonomous (operator unavailable for approval; bypass approved per user instruction this session).

## Problem

`workflow` ships a real engine with 90+ module types, a registry of ~58 plugins, and 50 public plugin repos. A spot audit reveals four user-visible discoverability holes that a new external adopter hits immediately:

1. **No README at all** in 5 plugin repos (`aws`, `gcp`, `azure`, `tofu`, `ci-generator`).
2. **No `examples/` directory** in any plugin repo — no copy-pasteable starting point.
3. **No `CONTRIBUTING.md`** in any plugin repo — contribution path is invisible.
4. **License inconsistency** — 12 public GoCodeAlone-owned repos have `none` or non-MIT licenses despite the workflow project being MIT.
5. **Active-usage gap not surfaced.** Only 7 of 50 public plugins are validated in any active GoCodeAlone project. The other 43 ship without any "experimental / unverified" signal — a new user has no way to know which plugins are real and which are scaffolded.

## Goal

Bring the workflow ecosystem to a uniform OSS-readiness baseline by applying a single checklist to every relevant repo. Documentation, examples, license consistency, experimental-status markers, and trivial example-validation fixes only. **No new features.**

## Non-Goals

- Adding new features, new module types, new step types, new manifests.
- Rewriting documentation that is already accurate.
- Live-deployment validation (we will validate syntax + schema only — no real cloud calls).
- Touching upstream forks (`genkit`, `v8go`, `voxtral-tts`, `wgpu`, `yaegi`, `go-plugin`) — they retain upstream licenses.
- Touching private plugins (security cluster: waf, security, sandbox, supply-chain, data-protection) and other private repos — filed as P2 follow-up issues only.

## Active-Usage Verification Matrix

Aggregated from `wfctl.yaml` and `.wfctl-lock.yaml` across active GoCodeAlone projects (`buymywishlist`, `core-dump`, `workflow-compute`, `ratchet`):

### Verified (used in production GoCodeAlone projects) — 7 plugins

| Plugin | Used in |
|--------|---------|
| `workflow-plugin-audit-chain` | buymywishlist |
| `workflow-plugin-auth` | buymywishlist |
| `workflow-plugin-digitalocean` | buymywishlist, core-dump, workflow-compute |
| `workflow-plugin-eventbus` | buymywishlist |
| `workflow-plugin-payments` | buymywishlist |
| `workflow-plugin-twilio` | buymywishlist |
| `workflow-plugin-agent` | ratchet (alias) |

### Unverified (public, but no GoCodeAlone-project usage) — 43 plugins

All other public `workflow-plugin-*` repos. Examples called out by the user: `aws`, `gcp`, `azure`, `tofu`, `ci-generator`. Full list derived from `gh repo list GoCodeAlone --visibility public` minus the verified set above.

These will be **marked experimental** — see "Experimental Marker" section below.

## License Audit

Public GoCodeAlone-owned repos that need license action:

### Public repos with `none` license — add MIT

- `workflow-plugin-cicd`, `workflow-plugin-infra`, `workflow-plugin-marketplace`, `workflow-plugin-platform`, `workflow-plugin-deployment`
- `homebrew-tap`, `superpowers-marketplace`
- `ratchet`, `ratchet-cli`
- `claude-skills`
- `EvoSim`, `rover`

### Public repos with apache-2.0 — convert to MIT if GoCodeAlone-original

- `workflow-plugin-migrations` — Convert (verify no upstream Apache-2.0 code first).

### Public repos with non-MIT/non-Apache — leave alone

- `go-plugin` (MPL-2.0 — HashiCorp fork), `v8go` (BSD-3 — fork), `genkit`/`yaegi`/`voxtral-tts`/`wgpu` (Apache-2.0 forks), `benchmark-it` / `terraform-aws-transit-gateway` / `github-action-matrix-outputs-{read,write}` (Apache-2.0 — likely community).

## Experimental Marker

For each of the 43 unverified public plugins:

1. **Registry manifest** (`workflow-registry/plugins/<name>/plugin.json`) gains a `"status"` field with values `"verified"`, `"experimental"`, or `"deprecated"`. Default new value: `"experimental"`. Set verified plugins explicitly. Schema in `workflow-registry/schema/plugin.json` updated.
2. **README banner** at the top:
   ```markdown
   > ⚠️ **Experimental** — This plugin compiles and passes unit tests, but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution and please report issues.
   ```
3. **Registry badge** — wfctl marketplace listing and the static API JSON surface the `status` field so users see it before installing.

When a plugin moves to "verified" status (gets adopted in a real GoCodeAlone project), both the manifest and the README banner are updated in a follow-up PR.

## Per-Repo OSS-Readiness Checklist

Applied uniformly. Pass = file exists AND content meets the criteria.

| # | File / Property | Criteria |
|---|-----------------|----------|
| 1 | `LICENSE` | MIT for all public GoCodeAlone-owned repos (excl. forks); private retains commercial |
| 2 | `README.md` | Tagline + status badge (verified/experimental) + build badge + license badge + 60-second quickstart + install + link to docs |
| 3 | `CHANGELOG.md` | Keep-a-Changelog header + latest released tag's entry |
| 4 | `CONTRIBUTING.md` | Link to `workflow/CONTRIBUTING.md` + repo-specific notes (build/test commands) |
| 5 | `examples/minimal/` | At least one runnable example config validated with `wfctl validate` |
| 6 | `.github/ISSUE_TEMPLATE/` | `bug_report.md` + `feature_request.md` (or link to shared workflow templates) |
| 7 | `.github/PULL_REQUEST_TEMPLATE.md` | Short, links to CONTRIBUTING |
| 8 | Manifest in registry | `plugin.json` lists correct latest tag + `status` field |
| 9 | Build green | `go build ./...` and `go vet ./...` clean on default branch |
| 10 | Example validation | `wfctl validate examples/**/*.yaml` passes |

## Scope Tiers

### P0 (must complete this session) — engine + registry + verified plugins (9 repos)

1. `workflow` — README polish, examples index, getting-started cross-references
2. `workflow-registry` — add CHANGELOG + CONTRIBUTING, add `status` field to schema + populate all manifests, update README
3. `workflow-plugin-digitalocean` — full checklist, mark `verified`
4. `workflow-plugin-payments` — full checklist, mark `verified`
5. `workflow-plugin-agent` — full checklist, mark `verified`
6. `workflow-plugin-audit-chain` — full checklist, mark `verified`
7. `workflow-plugin-auth` — full checklist, mark `verified`
8. `workflow-plugin-eventbus` — full checklist, mark `verified`
9. `workflow-plugin-twilio` — full checklist, mark `verified`

### P1 (best-effort this session) — user-called-out unverified plugins (5 repos)

10. `workflow-plugin-aws` — **add README** + examples + experimental banner
11. `workflow-plugin-gcp` — **add README** + examples + experimental banner
12. `workflow-plugin-azure` — **add README** + examples + experimental banner
13. `workflow-plugin-tofu` — **add README** + examples + experimental banner
14. `workflow-plugin-ci-generator` — **add README** + examples + experimental banner

### P2 (mass-marker sweep — minimal change per repo) — remaining ~38 unverified public plugins

For each: open one PR that does only the experimental banner + manifest status field + LICENSE check (if missing, add MIT). Skip the full README/examples/CONTRIBUTING build-out — file as a separate tracking issue in `workflow-registry`.

This is the "low-cost, high-coverage" pass: every plugin gets correctly flagged so users aren't misled. The deep doc work is deferred but visible.

### P3 (license-only sweep) — non-plugin public GoCodeAlone repos without MIT (7 repos)

`homebrew-tap`, `superpowers-marketplace`, `ratchet`, `ratchet-cli`, `claude-skills`, `EvoSim`, `rover` → add MIT LICENSE file.

## Execution Model

Lead-orchestrated team, subagent-driven-development pattern, per-repo worktree-isolated agents.

```
team-lead (main thread, this conversation)
├── doc-impl-1 (Sonnet 4.6) — P0 repos (workflow, registry, 2 verified plugins)
├── doc-impl-2 (Sonnet 4.6) — P0 repos (3 verified plugins)
├── doc-impl-3 (Sonnet 4.6) — P0 repos (2 verified plugins)
├── doc-impl-4 (Sonnet 4.6) — P1 repos (5 user-called-out plugins)
├── doc-impl-5 (Haiku 4.5)  — P2 mass-marker sweep across 38 repos
├── doc-impl-6 (Haiku 4.5)  — P3 license-only sweep across 7 repos
└── reviewer    (Sonnet 4.6) — checklist audit + PR review across all PRs
```

- **One worktree per repo.** Each implementer creates a fresh worktree per assigned repo so concurrent work cannot collide.
- **One PR per repo.** Per `feedback_per_agent_worktree_per_task_pr`.
- **Branch naming:** `chore/qol-sweep-2026-05-19`.
- **Commit format:** Conventional commits, e.g. `docs: add README and examples (QoL sweep)`.
- **Pre-push validation:** local `go build ./...`, `go vet ./...`, `wfctl validate examples/**/*.yaml` per `feedback_local_image_launch_validation` and `feedback_no_speculative_remote_ci`.

## Example Generation Strategy

Each plugin's `examples/minimal/config.yaml` is derived from its `plugin.json` manifest (modules + steps + triggers). Reviewers validate by:

```sh
cd <repo>
wfctl validate examples/minimal/config.yaml
```

Plugins that need credentials use `${ENV_VAR}` substitution and document the env vars in the README. We **do not** run live cloud calls.

## Per-PR Validation

Each implementer runs locally before opening:

```sh
go build ./...
go vet ./...
wfctl validate examples/**/*.yaml
# markdown link sanity if tool available
```

Reviewer agent re-runs the same on the worktree before marking the PR ready, then PR is admin-merged per `feedback_admin_override_pr_merge`.

## Registry Schema Change (single coordinated PR)

`workflow-registry/schema/plugin.json` gains an optional `status` field:

```json
{
  "status": {
    "type": "string",
    "enum": ["verified", "experimental", "deprecated"],
    "description": "Active-usage verification status. 'verified' = used in production; 'experimental' = compiles + tested but no GoCodeAlone-internal usage; 'deprecated' = scheduled removal."
  }
}
```

This is additive and backward-compatible — older `plugin.json` files without `status` continue to validate.

## Assumptions

- `wfctl validate` exists and validates plugin YAML against schema. Verified — `docs/WFCTL.md`.
- Repos are independently versioned and PRs are independently mergeable. Verified — recent 10-PR Apply-removal cascade pattern.
- The user is OK with PRs being opened and admin-merged autonomously per the stated grant. **Load-bearing.**
- Plugin manifests in `workflow-registry/plugins/*/plugin.json` are the source of truth for what each plugin exports. Verified.
- Public-MIT plugins permit doc-only PRs without owner approval. True for GoCodeAlone-owned repos.
- The active-usage matrix (7 verified) is complete. **Risk:** if a project I didn't sample uses one of the "unverified" plugins, that plugin will be incorrectly marked experimental. Mitigation: revert/relabel is one-line manifest change.
- Adding a `status` field to the registry schema does not break wfctl marketplace clients. Verified by reading `wfctl marketplace` parser — unknown fields are ignored.

## Top Self-Challenge Doubts

1. **Scope risk.** ~60 PRs across ~50 repos in one session is at the edge of feasibility. Mitigation: P0/P1 PRs get deep treatment (9+5 = 14 repos); P2/P3 are mass-marker passes (one tiny PR each, template-driven); P2 deep work is deferred to per-repo tracking issues.
2. **Experimental-marker political risk.** Marking 43 plugins "experimental" is a visible downgrade. If a user is already using one of them, they'll see the banner and worry. Mitigation: phrasing emphasizes "compiles + tested but not GoCodeAlone-internal validated" rather than "unstable"; the banner is informational, not a warning. Verified-set list goes in workflow README so users know which are blessed.
3. **License conversion for apache-2.0 repos.** `workflow-plugin-migrations` is currently Apache-2.0. Switching requires verifying no upstream Apache-2.0 code was incorporated (which would make MIT relicense legally suspect). Mitigation: audit commit history before changing; if there's upstream code, leave as Apache-2.0 and file an issue noting the deviation.

## Rollback

Each PR is independent and small. If a sweep PR is wrong:

1. `git revert <merge-sha>` on the affected repo's default branch + revert PR.
2. Pre-merge validation prevents most cascades.
3. Documentation/banner regressions are immediately user-visible — fast detect, fast revert.
4. No runtime config or production config touched — examples are isolated under `examples/minimal/`. The blast radius of a bad example is "tutorial reader confused", not "production broken".
5. Registry schema change is additive; if it causes wfctl-marketplace issues, set `status` removed from manifests + revert schema PR.

This change class is documentation + manifest-additive-field, so it does not require `runtime-launch-validation` (no code changes affect engine startup, plugin loading, or deployment).

## Success Criteria

- **9 P0 PRs** opened and admin-merged.
- **5 P1 PRs** opened and admin-merged.
- **38 P2 mass-marker PRs** opened and admin-merged (template-driven).
- **7 P3 license PRs** opened and admin-merged.
- All 43 unverified public plugins surface an "experimental" banner in their README and `"status": "experimental"` in their registry manifest.
- All public GoCodeAlone-owned non-fork repos carry an MIT LICENSE.
- One post-sweep retro: `docs/retros/2026-05-19-multi-repo-qol-sweep.md`.

## Out of Scope (Explicit Deferrals)

- **Private plugins** (security cluster, authz-ui, cardgame/dnd content, cloud-ui, etc.) — filed as P2 tracking issues if applicable.
- **Workflow-cloud, workflow-cloud-ui, ratchet-cli, modular, workflow-editor, workflow-vscode, workflow-jetbrains** — first-class projects but each warrants its own sweep, filed as P2 tracking issues.
- **Live-deployment example validation** — future work; needs CI with credentials.
- **Translation / i18n** — future work.
- **Plugin-specific deep documentation** for the 38 P2 plugins — tracking issues filed.

## References

- `feedback_per_agent_worktree_per_task_pr` — per-task PR pattern
- `feedback_local_image_launch_validation` — pre-push validation
- `feedback_no_speculative_remote_ci` — no remote dry-runs
- `feedback_continuous_autonomous_phases` — proceed without re-asking
- `feedback_admin_override_pr_merge` — pre-authorized admin merge
- `feedback_version_bump_immediate_merge` — analogous low-risk PR pattern
- `feedback_docs_pr_verify_against_codebase` — docs must match files
- `feedback_check_versions_actively` — confirm latest tags before manifest updates
- `docs/PLUGIN_AUTHORING.md` — canonical plugin guide
- `docs/BUILDING_APPS_GUIDE.md` — canonical user guide
