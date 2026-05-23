# Plugin version sync hardening + publish-tag semver gate — design

Issue: GoCodeAlone/workflow#758
Date: 2026-05-23 (cycle 2 — pivoted per adversarial cycle 1)
Mode: design-only handoff (autonomous brainstorm; user is away)

## Problem

Today every plugin repo carries a `plugin.json.version` field on `main`. After each release tag fires, `sync-plugin-version.yml` opens a PR to bump that field AND rewrite every `downloads[].url` to point at the new tag. Those PRs:

- Pile up if not actively merged (13 stale PRs found on `workflow-plugin-digitalocean` 2026-05-23 — closed in a sweep).
- Are necessary — `workflow-registry/scripts/sync-versions.sh:122` reads `.version` AND asserts `downloads[].url` contains `/releases/download/v${version}/`. The committed file IS the source of truth for the registry sync. Cannot be eliminated.
- Add review surface for purely mechanical bot churn.

Additionally, the user wants protection against non-semver versions getting into the registry. Branch / test / `-dirty` builds should install locally but be rejected from publish.

## What the adversarial cycle taught us

Cycle 1 proposed dropping `version` from committed plugin.json and surfacing it via ldflag-injected runtime value. That design had three critical defects: (a) the pre-spawn `LoadManifest+Validate` site at `plugin/external/manager.go:134-140` makes "defer to GetManifest" structurally impossible at the named code path; (b) `sync-plugin-version.yml` also rewrites `downloads[].url` (`sync-plugin-version.yml:47-52`) — not just version — and `workflow-registry/scripts/sync-versions.sh:122-138` depends on that URL being version-correct, so dropping the workflow without compensating produces unbuildable registry manifests; (c) `wfctl plugin validate --for-publish` has no source for the version string it would validate against once the disk plugin.json no longer carries it.

The cycle-1 reviewer surfaced four simpler alternatives. The combination of Options 1 + 4 (with a smaller Option 3 piece) reaches the user's actual goal — stop the PR churn AND gate non-semver publishes — without dropping the committed version field at all.

## Cycle-2 direction

**Keep `version` field in committed plugin.json.** The cycle-1 root cause is sync-mechanism, not field-presence. Fix the sync mechanism; leave the field alone.

Three composable pieces:

### 1. Replace PR-opening sync with direct-push-to-main

`sync-plugin-version.yml` today: tag fires → workflow checks out main → rewrites plugin.json → opens PR. The PR sits unmerged.

New: tag fires → workflow checks out main → rewrites plugin.json + downloads URLs (exactly today's logic, both substitutions preserved) → commits directly to `main` as `github-actions[bot]`, signed-off with the triggering tag in the commit message. No PR. No review queue.

Requires either (a) branch protection on `main` to allow `enforce_admins: false` + the bot to push (DO plugin's branch protection is `enforce_admins: false` already; admins can bypass — bot uses an admin PAT, OR the GITHUB_TOKEN gets a one-time bypass via a "linear history" / "allow bot pushes" rule), or (b) a dedicated "release-bot" branch protection exemption.

For repos where direct push is undesirable (some private plugins may have stricter rules), the workflow stays PR-opening but ALSO calls `gh pr merge --auto --squash --delete-branch` immediately after creation, with `--admin` if the token has permission. The PR still gets opened but is automerged on creation — no human review queue.

### 2. Tag-level semver gate in release workflow

`release.yml` today: triggered by tag push, runs goreleaser. No tag-format validation. A malformed tag (`v1.2`, `v1.2.3-dirty`, `release-2026-05`, etc.) just builds and publishes a malformed release.

Add a first step in `release.yml`:

```yaml
- name: Validate tag is strict semver
  run: |
    TAG="${{ github.ref_name }}"
    if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)(\.[0-9]+)?)?$ ]]; then
      echo "::error::Tag $TAG is not strict semver (allowed: vN.N.N or vN.N.N-{alpha|beta|rc}[.N])"
      exit 1
    fi
```

The whitelist `alpha|beta|rc` is narrow on purpose (cycle 1 I4 finding): bare semver pre-release syntax (`-feat-foo.deadbeef`) satisfies semver but is exactly what we want to reject from publish. If teams need a different pre-release vocabulary, the regex is the place to change it (single line).

Local / branch / test builds via `goreleaser --snapshot` or plain `go build` (which produce `(devel)` / SNAPSHOT tags) are NOT triggered by tag push, so they skip the gate entirely. They install via `wfctl plugin install --local <dir>` which doesn't go through release.yml. That's the answer to "people should be able to generate a plugin from a custom/test branch."

### 3. Independent semver gate in `workflow-registry` ingest

Defense in depth. `workflow-registry/scripts/sync-versions.sh` (or whichever ingest path is canonical — verify before plan) gets the same regex check before accepting a plugin update:

```bash
TAG="v${manifest_version}"
if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)(\.[0-9]+)?)?$ ]]; then
  echo "  REJECT  $plugin_name — manifest version $manifest_version is not strict semver"
  continue
fi
```

A plugin author who bypasses the release.yml gate (e.g., self-hosted runner, force-push, manual tarball upload) still gets caught at registry sync.

### 4. ldflag version surface (out of cycle-1 scope, kept as future option)

The runtime-version contract (ldflag → `internal.Version` → GetManifest RPC) already exists in workflow SDK + DO plugin. We don't *change* it here. A future design cycle could collapse the disk `version` field onto the runtime surface — but that requires solving cycle-1's C1/C2/C3 problems first (engine load order, registry URL rewrite, wfctl publish-validation source). Not scoped here.

## Migration plan

Each plugin repo is independent.

1. **workflow (PR 1, this repo):** no engine changes. Add a `release.yml` snippet template + document in `docs/PLUGIN_RELEASE_GATES.md`. Optionally: small wfctl helper `wfctl plugin validate-tag <vX.Y.Z>` that runs the regex (operator convenience).
2. **workflow-registry (PR 2):** add the semver gate in `scripts/sync-versions.sh`. Reject malformed versions with clear errors.
3. **Per-plugin migration PR (N repos):** update local `sync-plugin-version.yml` to direct-push-to-main (or auto-merge variant), AND add the tag-format gate step to local `release.yml`. Each PR is ~10 lines, isolated, individually mergeable. Same PR can also remove any stale `chore: sync plugin.json version to v…` history-trash if desired (out of scope).

The migration order is workflow → registry → plugins. Plugin migrations can run in parallel.

## Verified API surface

- `workflow-registry/scripts/sync-versions.sh:122` — reads `.version` from manifest. Stays as-is.
- `workflow-registry/scripts/sync-versions.sh:138` — `downloads_match_version` requires `downloads[].url` contains `/releases/download/v${version}/`. Stays as-is.
- `workflow-plugin-digitalocean/.github/workflows/sync-plugin-version.yml:47-52` — rewrites `downloads[].url` via Python sed; this LOGIC is preserved in the direct-push variant, only the `gh pr create` is replaced by `git push origin main`.
- `workflow-plugin-digitalocean/.github/workflows/release.yml` — tag-fired today; receives new validate-tag step at the top.
- `workflow-plugin-digitalocean/.goreleaser.yaml:7-9` — before-hook stays as-is.

## Assumptions

A1. Every plugin repo has branch protection set such that an admin PAT can push to `main` directly (enforce_admins: false). **Verified for DO plugin via `gh api repos/.../branches/main/protection` in chat 2026-05-23. Assumed similar for others; per-repo verification step in each plugin migration PR.**
A2. Auto-merge `--admin --squash --delete-branch` is available on GitHub for repos with branch protection. **Verified by prior session work (multiple admin-merges in 2026-05).**
A3. The user's "branch / test build" use case is locally-installed via `wfctl plugin install --local <dir>` and never goes through release.yml. **Plausible from existing wfctl support; per-plugin migration verifies no other publish path exists.**
A4. The tag-format gate regex `^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)(\.[0-9]+)?)?$` matches the user's intent (no `-feat-foo`, no `-dirty`, no `+meta`). **User said "valid semver" and "reject branch versions"; explicit pre-release whitelist enforces the latter.**
A5. workflow-registry's sync-versions.sh is the canonical publish ingest. If another path exists (webhook, manual upload) it needs the same gate. **Per registry PR audit; flag in plan.**
A6. The direct-push-to-main mechanism doesn't break CI in any plugin repo (e.g., a "PRs only" branch protection rule). **Per-repo verification step.**

## Self-challenge — top 3 doubts

D1. **Is direct-push-to-main actually safer than auto-merging a sync PR?** Direct-push: bot has commit-to-main perm forever. Auto-merge: bot opens PR → merges immediately, audit trail in PR history, perm scoped to "create + merge own PRs." The latter is the better default for security-conscious repos. Cycle-2 design supports BOTH (per-repo choice) — direct-push for fast-moving plugin repos, auto-merge for privates.

D2. **What about the 13 stale PRs that piled up on DO before the sweep?** Those PRs were closed manually 2026-05-23. Going forward, the new mechanism prevents new accumulation. There is no general-purpose "close stale sync PRs" automation in this design — that's a one-time cleanup that already happened.

D3. **Does the tag-format regex's whitelist (`alpha|beta|rc`) hard-code a release-vocabulary choice?** Yes. Some plugin authors might want `dev`, `preview`, `experimental`, etc. The regex is the place to change it. Documenting the supported list in `docs/PLUGIN_RELEASE_GATES.md` makes the choice visible.

## Rollback

- §1 (direct-push or auto-merge in sync-plugin-version.yml): per-repo, revert the workflow file change. Old PR-opening behavior returns; sync PRs accumulate again. Cheap revert; no state.
- §2 (tag-format gate in release.yml): per-repo, remove the step. Malformed tags can publish again. Cheap revert; no state.
- §3 (registry-side gate): single revert in `workflow-registry/scripts/sync-versions.sh`. Cheap; no state.

Cross-repo rollback risk: very low. None of the changes break the engine/SDK contract; they only affect the release pipeline. Worst case is a single bad release that the existing tag-delete + re-tag flow already handles.

## Out of scope

- Dropping `plugin.json.version` field entirely (deferred; needs solving cycle-1 C1/C2/C3 in a separate design).
- Replacing goreleaser.
- Cleaning up the existing 13 stale sync PRs (already done manually 2026-05-23).
- Auditing private plugins (a separate sweep PR per repo; mechanical; can be batched).
- Changing how plugins surface their runtime version via gRPC (existing ldflag-based path stays unchanged).

## Decision points reserved for user return

- **Approve overall design + execute pipeline** — design → plan → adversarial-design-review (plan) → alignment-check → scope-lock → dispatch workflow PR 1 only, pause for review before registry PR 2 and per-plugin PRs.
- **Approve §1+§2 only** (defer §3 registry gate); ship sync hardening + tag-format gate first, registry-side defense in depth as a follow-up.
- **Re-scope** (e.g., go back and revive the cycle-1 ldflag direction; the cycle-1 review explicitly notes Option 3 — `runtime/debug.ReadBuildInfo()` — as a cleaner alternative that bypasses both the disk-version AND ldflag-coordination problems).
