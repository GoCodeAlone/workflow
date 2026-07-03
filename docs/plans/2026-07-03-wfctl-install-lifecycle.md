# wfctl Install Lifecycle Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Publish verified wfctl install, update, and plugin lifecycle documentation across Workflow, the Homebrew tap, and website surfaces.

**Architecture:** Workflow is the canonical source for wfctl docs. Homebrew tap documents tap-specific setup and formula inventory. Website changes avoid stale pinned install commands and rely on generated Workflow docs after release.

**Tech Stack:** Markdown docs, Go `wfctl` CLI help validation, Homebrew formula metadata, Vite/React website.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 3
**Tasks:** 4
**Estimated Lines of Change:** ~500

**Out of scope:**
- New installer scripts or release asset names.
- Plugin installer behavior changes.
- Generated website docs refresh before Workflow docs release.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | docs: clarify wfctl install and plugin lifecycle | Task 1 | docs/wfctl-install-lifecycle |
| 2 | docs: document GoCodeAlone Homebrew tap usage | Task 2 | docs/tap-install-readme |
| 3 | docs: remove stale wfctl install copy and release docs | Task 3, Task 4 | docs/wfctl-install-copy |

**Status:** Locked 2026-07-03T23:38:53Z

### Task 1: Workflow Canonical wfctl Docs

**Files:**
- Create: `docs/WFCTL_INSTALLATION.md`
- Modify: `README.md`
- Modify: `docs/WFCTL.md`
- Modify: `docs/PLUGIN_DEVELOPMENT_GUIDE.md`

**Step 1: Write docs**

Add a dedicated installation/lifecycle guide covering:

- Homebrew: `brew tap gocodealone/tap`, `brew install wfctl`, `brew upgrade wfctl`, `brew update`.
- Terminal release install without Homebrew: detect OS/arch, download latest raw binary and `checksums.txt`, verify SHA-256, `chmod +x`, move into `PATH`.
- Browser/manual install: GitHub Releases page, choose matching asset, download `checksums.txt`, verify locally, make executable, move into `PATH`.
- Windows manual install: download `wfctl-windows-amd64.exe`, verify via `certutil -hashfile`, add directory to `PATH`.
- Source install: `go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest` for Go users/devs.
- Self-update: `wfctl update --check`, `wfctl update`; Homebrew users should prefer `brew upgrade wfctl`.
- Plugin lifecycle: `plugin search`, `add`, `lock`, `install`, `ci`, `install -g`, `update`, `update --all`, `update -g --all`, `remove`, `list`, lockfiles, default project/global plugin directories, checksum/compatibility behavior.

Update `README.md` and `docs/WFCTL.md` to link the guide instead of duplicating stale one-liners. Add a short pointer from `docs/PLUGIN_DEVELOPMENT_GUIDE.md`.

**Step 2: Verify command surfaces**

Run:

```bash
GOWORK=off go run ./cmd/wfctl --help >/tmp/wfctl-help.txt
GOWORK=off go run ./cmd/wfctl update --help >/tmp/wfctl-update-help.txt
GOWORK=off go run ./cmd/wfctl plugin --help >/tmp/wfctl-plugin-help.txt
```

Expected: each exits `0`; update help includes `--check`; plugin help includes `install`, `ci`, `update`, `remove`, and `-g or -global`.

Run:

```bash
gh release view --repo GoCodeAlone/workflow --json tagName,assets --jq '.tagName,([.assets[].name]|join("\n"))'
```

Expected: latest stable tag and assets include `checksums.txt`, `wfctl-darwin-amd64`, `wfctl-darwin-arm64`, `wfctl-linux-amd64`, `wfctl-linux-arm64`, and `wfctl-windows-amd64.exe`.

**Step 3: Commit**

```bash
git add README.md docs/WFCTL.md docs/WFCTL_INSTALLATION.md docs/PLUGIN_DEVELOPMENT_GUIDE.md docs/plans/2026-07-03-wfctl-install-lifecycle*.md
git commit -m "docs: clarify wfctl install lifecycle"
```

Rollback: revert commit; no runtime state changes.

### Task 2: Homebrew Tap README And Auto-Merge Guard

**Files:**
- Modify: `README.md`
- Inspect: `.github/workflows/update-wfctl.yml`
- External setting: `GoCodeAlone/homebrew-tap` `main` branch protection

**Step 1: Write tap docs**

Expand the tap README with:

- `brew tap gocodealone/tap`.
- Available formula table for `wfctl`, `ratchet-cli`, and `claude-skills`.
- Install, upgrade, pin/unpin, uninstall, and inspect examples.
- Formula-specific examples: `brew install gocodealone/tap/wfctl`, `brew upgrade wfctl`.
- Troubleshooting for stale formula cache via `brew update` and `brew info gocodealone/tap/wfctl`.
- Automation note: Workflow releases dispatch `update-wfctl`; generated wfctl formula PRs are expected to auto-merge or admin-merge after checks when no unresolved review threads remain.
- Branch-protection note: required reviews must not be mandatory for generated formula PRs unless a generated-PR bypass exists.

**Step 2: Verify tap state**

Run:

```bash
ruby -c Formula/wfctl.rb
ruby -c Formula/ratchet-cli.rb
brew info gocodealone/tap/wfctl
gh pr list --repo GoCodeAlone/homebrew-tap --state all --limit 5 --json number,title,state --jq '.[] | [.number,.state,.title] | @tsv'
gh api repos/GoCodeAlone/homebrew-tap/branches/main/protection --jq '.required_pull_request_reviews' || true
gh pr view 59 --repo GoCodeAlone/homebrew-tap --json state,mergedAt,reviewDecision,statusCheckRollup --jq '{state,mergedAt,reviewDecision,checks:[.statusCheckRollup[]? | {name,conclusion,status}]}'
```

Expected: Ruby syntax OK; `brew info` reports the tap formula; recent PR list shows automated formula updates; branch protection is absent or has no required-review gate for generated formula PRs; recent generated wfctl formula PR merged with green checks.

If the branch-protection check reports required reviews and no generated-PR bypass exists, update the repository rule before relying on the automation. If workflow inspection shows generated formula PRs no longer merge after checks, patch `.github/workflows/update-wfctl.yml` to restore the current behavior: open/update the generated PR, request Copilot review as advisory, wait for checks, leave open on failed checks or unresolved review threads, and admin-merge after green checks.

**Step 3: Commit**

```bash
git add README.md .github/workflows/update-wfctl.yml
git commit -m "docs: document Homebrew tap usage"
```

Rollback: revert commit; formulas unchanged.

### Task 3: Website Install Copy

**Files:**
- Modify: `src/pages/PlatformPage.tsx`
- Optionally modify generated docs only after Workflow docs release/sync.

**Step 1: Replace stale install snippets**

Replace hardcoded `go install ...@v0.60.17` snippets with current lifecycle copy that avoids pin drift:

- Primary snippet: `brew install gocodealone/tap/wfctl`.
- Secondary source command: `go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest`.
- Link users to Workflow releases/docs for binary downloads rather than duplicating versioned asset names in React copy.

**Step 2: Verify website build**

Run:

```bash
npm install
npm run build
```

Expected: build exits `0`.

**Step 3: Commit**

```bash
git add src/pages/PlatformPage.tsx
git commit -m "docs: refresh wfctl install copy"
```

Rollback: revert commit; no runtime state changes until website release.

### Task 4: Post-Merge Release And Downstream Verification

**Files:**
- Inspect: `.github/workflows/release.yml`
- Inspect: `.github/workflows/sync-docs.yml`
- External repos: `GoCodeAlone/workflow`, `GoCodeAlone/homebrew-tap`, `GoCodeAlone/workflow-registry`, `GoCodeAlone/workflow-scenarios`, `GoCodeAlone/workflow-editor`, `GoCodeAlone/gocodealone-website`, `GoCodeAlone/gocodealone-multisite`

**Step 1: Merge PRs after checks and review threads**

For each PR, wait for checks to complete and inspect unresolved review threads. Merge with admin privileges only after checks are green and actionable review threads are resolved or explicitly non-blocking.

```bash
gh pr checks <PR> --repo <owner/repo>
gh pr merge <PR> --repo <owner/repo> --admin --squash --delete-branch
```

Expected: PR state becomes `MERGED`.

**Step 2: Release Workflow if Workflow docs changed**

After the Workflow PR merges, create a new patch tag from `main` using the repository's current release convention, push the tag, then monitor `release.yml`.

```bash
gh release view --repo GoCodeAlone/workflow --json tagName
git fetch origin main --tags
git checkout main
git pull --ff-only origin main
git tag <new-workflow-tag>
git push origin <new-workflow-tag>
gh run list -R GoCodeAlone/workflow --workflow release.yml --limit 3
```

Expected: release run exits `success`, assets include `wfctl-*` and `checksums.txt`, and stable-release dispatch jobs trigger Homebrew tap, workflow-registry, workflow-scenarios, and workflow-editor.

**Step 3: Verify Homebrew generated PR freshness**

```bash
gh run list --repo GoCodeAlone/homebrew-tap --workflow update-wfctl.yml --limit 3
gh pr list --repo GoCodeAlone/homebrew-tap --state all --limit 5 --json number,state,title,headRefName
brew update
brew info gocodealone/tap/wfctl
```

Expected: generated wfctl formula update run is `success`, generated PR is merged, and `brew info` shows the latest Workflow version after `brew update`.

**Step 4: Refresh and release website docs**

After the Workflow release is available, run the website docs sync, merge its generated PR after checks, then release the website per its README by bumping `package.json` and pushing a `v*` tag.

```bash
gh workflow run sync-docs.yml -R GoCodeAlone/gocodealone-website
gh pr list -R GoCodeAlone/gocodealone-website --head chore/sync-docs-snapshot
git fetch origin main --tags
git checkout main
git pull --ff-only origin main
# edit package.json version to the next patch version
git commit -am "chore: bump website release to <new-website-version>"
git tag v<new-website-version>
git push origin main --tags
gh run list -R GoCodeAlone/gocodealone-website --workflow release.yml --limit 3
gh run list -R GoCodeAlone/gocodealone-multisite --workflow content-ingest.yml --limit 3
```

Expected: docs sync PR includes regenerated Workflow docs, website release succeeds, and multisite ingest succeeds.

Rollback: if a bad docs release ships, revert the relevant PR, cut a follow-up patch release, and rerun failed downstream workflows from the corrected tag. Do not hand-edit generated tap checksums.
