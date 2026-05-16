# Post-cloud-SDK Plugin Ecosystem Sweep Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bump 15 lagging plugin repos from workflow `v0.51.x`/pseudo-version pins → `v0.53.1`, cutting a fresh patch/minor release for each plugin so the entire plugin ecosystem aligns with the post-cloud-SDK-extraction workflow tag.

**Architecture:** Mechanical per-plugin parallel sweep. Each plugin = one PR + one new tag + one GoReleaser-driven release. Wave-1 parallel (14 PRs); wave-2 sequencing for `workflow-plugin-agent` (PR15) which depends on `workflow-plugin-authz` v0.5.4 (PR11) being tagged first because agent's go.mod directly imports authz.

**Tech Stack:** Go modules + GoReleaser + GitHub Actions (per-plugin `release.yml`). 11 plugins use `ubuntu-latest` runners; 4 use `[self-hosted, Linux, X64]` (tofu, authz-ui, security, supply-chain — intentional, NOT migrating).

**Base branch:** `main` (per-plugin repo)

---

## Scope Manifest

**PR Count:** 15
**Tasks:** 15
**Estimated Lines of Change:** ~30 lines per plugin (go.mod + plugin.json) × 15 = ~450 lines total; agent adds ~5 lines for dual-bump; tofu adds ~3 lines for `.goreleaser.yaml` draft fix.

**Out of scope:**
- gcp #6 + azure #4 host conformance (separate design — needs ExternalPluginManager subprocess test infrastructure)
- workflow#640 v2 action lifecycle migration (substantive 5-invariant scope; user-flagged as "worth tracking" per autonomous mandate; tracked in MEMORY.md only)
- Catalog manifest-derivation refactor (172+ hardcoded type strings in workflow's `schema/`; high blast radius)
- TypedProvider migration for the 5 plan-2 types (SDK scaffolding ready via workflow PR #686; awaits first consumer)
- MessagePublisher/MessageSubscriber for IaC-bridge modules (decisions/0038 Non-Goal)
- aws-sdk-go-v2 extraction from `provider/aws/`/`plugin/rbac/aws.go`/`iam/aws.go`/`artifact/s3.go` (out-of-scope of recent extraction)
- workflow-plugin-waf v0.2.1 / sandbox v0.2.1 / data-protection v0.2.1 — all pin v0.3.56 (50+ minor versions behind; separate cadence cluster)
- workflow-plugin-cloud-ui — no Go go.mod; React-only structural shape
- Phase B RLV doc (non-blocking nicety from cloud-SDK closure)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | chore: bump workflow pin v0.51.6 → v0.53.1; release v0.4.6 | Task 1 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-payments) |
| 2 | chore: bump workflow pin v0.51.6 → v0.53.1; release v0.2.4 | Task 2 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-audit-chain) |
| 3 | chore: bump workflow pin v0.51.7 → v0.53.1; first release v0.1.3 + draft fix | Task 3 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-tofu) |
| 4 | chore: bump workflow pin v0.51.7 → v0.53.1; release v0.1.4 | Task 4 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-ci-generator) |
| 5 | chore: bump workflow pin v0.51.7 → v0.53.1; release v1.0.4 | Task 5 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-github) |
| 6 | chore: bump workflow pin v0.51.7 → v0.53.1; release v1.0.3 | Task 6 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-gitlab) |
| 7 | chore: bump workflow pseudo → v0.53.1; release v1.1.2 | Task 7 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-azure) |
| 8 | chore: bump workflow pin v0.51.7 → v0.53.1; release v1.0.1 | Task 8 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-admin) |
| 9 | chore: bump workflow pin v0.51.7 → v0.53.1; release v1.1.3 | Task 9 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-bento) |
| 10 | chore: bump workflow pin v0.51.7 → v0.53.1; release v1.0.1 | Task 10 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-authz-ui) |
| 11 | chore: bump workflow pin v0.51.7 → v0.53.1; release v0.5.4 | Task 11 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-authz) |
| 12 | chore: bump workflow pin v0.51.6 → v0.53.1; release v0.3.5 | Task 12 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-eventbus) |
| 13 | chore: bump workflow pin v0.51.7 → v0.53.1; release v2.0.1 | Task 13 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-security) |
| 14 | chore: bump workflow pin v0.51.7 → v0.53.1; release v0.4.1 | Task 14 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-supply-chain) |
| 15 | chore: dual-bump workflow v0.51.7 → v0.53.1 + authz v0.2.2 → v0.5.4; release v0.9.3 | Task 15 | `chore/workflow-v0.53.1-pin-bump` (in workflow-plugin-agent) |

**Status:** Draft

---

## Pre-dispatch setup (team-lead, ONCE before any task starts)

Two setup steps — done once by the team-lead, NOT inside any per-task PR:

**1. Post #656 supersession comment:**

```bash
gh issue comment 656 --repo GoCodeAlone/workflow --body "Superseded by post-cloud-SDK plugin sweep landing at workflow v0.53.1; tracking 15 plugin PRs per docs/plans/2026-05-16-post-cloud-sdk-plugin-sweep.md. Original v0.52.x inventory in this issue is stale; the sweep picks up the actual current state. Closing-via-supersession when wave 1 + wave 2 complete."
```

**2. Self-hosted runner pre-flight (ONCE for all 4 self-hosted plugins: tofu, authz-ui, security, supply-chain):**

```bash
gh api /orgs/GoCodeAlone/actions/runners --jq '.runners | map(select(.status=="online")) | length'
```

Expected: ≥1 online runner. If 0, PAUSE all 4 self-hosted plugin tasks until runners return; the other 11 PRs continue.

(Per-task pre-checks for self-hosted plugins below repeat this verification defensively in case runners go offline mid-sweep.)

---

## Universal per-task pattern

For tasks 1, 2, 4-14 (the 13 standard PRs — wave 1 minus tofu PR3, plus all of wave 1 except agent PR15), each task follows the **5-step pattern**. Tasks 3 (tofu) and 15 (agent) extend it.

### Standard 5-step pattern (applies to PRs 1, 2, 4-14)

**Files:**
- Modify: `go.mod` — bump `github.com/GoCodeAlone/workflow vOLD → v0.53.1`
- Modify: `go.sum` — auto-updated by `go mod tidy`
- Modify: `plugin.json` — set/confirm `"minEngineVersion": "0.53.0"`
- Tag: `vNEW` (per-plugin from PR Grouping table)
- Release: triggered by tag push via `.github/workflows/release.yml`

**Step 1: Branch + ff-pull**

```bash
cd /Users/jon/workspace/<plugin-repo-name>
git fetch origin
git checkout -b chore/workflow-v0.53.1-pin-bump origin/main
git pull --ff-only origin main
```

**Step 2: Bump pin**

Edit `go.mod`:

```
require (
    github.com/GoCodeAlone/workflow v0.53.1   # was vOLD per table
    ...
)
```

If `replace` directive present (verify via `grep '^replace' go.mod`), update there too.

**Step 3: Tidy + build + test**

```bash
go mod tidy
go build ./...
go test ./... -race
```

Expected:
- `go mod tidy` produces a clean diff (only the workflow pin + transitive bumps; no surprise indirect introductions or removals)
- `go build ./...` exits 0
- `go test ./... -race` exits 0

**If build/test fails:** STOP. Capture failure signature. DM team-lead with diff + first 20 lines of failure. File upstream issue against `GoCodeAlone/workflow` if API drift. Pause this PR; the other 14 continue.

**Step 4: Update plugin.json minEngineVersion**

Edit `plugin.json`:

```json
{
  ...
  "minEngineVersion": "0.53.0",
  ...
}
```

If field missing, add it. If field already set higher (e.g., azure's `"0.52.0"`), confirm bump to `"0.53.0"`.

Re-run `go test ./... -race` if your plugin reads this field at startup (defensive).

**Step 5: Commit + push + tag + monitor release**

(Substitute `vOLD` and `vNEW` per the PR Grouping table — they are placeholder tokens, not real tag patterns.)

```bash
git add go.mod go.sum plugin.json
git commit -m "chore: bump workflow pin vOLD → v0.53.1; release vNEW"
git push -u origin chore/workflow-v0.53.1-pin-bump
gh pr create --base main --head chore/workflow-v0.53.1-pin-bump \
  --title "chore: bump workflow pin vOLD → v0.53.1; release vNEW" \
  --body "Pin sweep per https://github.com/GoCodeAlone/workflow/blob/main/docs/plans/2026-05-16-post-cloud-sdk-plugin-sweep-design.md.

Closes part of GoCodeAlone/workflow#656.

## Test plan
- [x] go build ./... clean
- [x] go test ./... -race PASS
- [x] minEngineVersion bumped to 0.53.0 (tested-floor)
- [x] go mod tidy diff is the expected pin + transitives only"
```

After CI green + Copilot review settle (~10 min), admin-merge:

```bash
gh pr merge <N> --squash --admin --delete-branch
```

Then tag + release:

```bash
git checkout main && git pull
git tag vNEW
git push origin vNEW
```

GoReleaser triggers via `.github/workflows/release.yml`. Monitor:

```bash
gh release view vNEW --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'
```

Expected: `draft=false assets=N` where N ≥ 4 (typical: 4-7 platform binaries).

If `draft=true` (azure-pattern): `gh release edit vNEW --draft=false --latest`.

**Rollback (per-plugin):** if downstream consumer breaks, cut `vNEW+1` re-pinning workflow → vOLD; old vNEW tag stays in Go proxy (immutable) but `latest` resolves to rollback. Matches plan-2 cloud-SDK matched-pair pattern.

---

## Tasks

### Task 1: workflow-plugin-payments — pin bump v0.51.6 → v0.53.1; release v0.4.6

**Repo:** `/Users/jon/workspace/workflow-plugin-payments`
**Files:** `go.mod` (workflow pin), `go.sum`, `plugin.json` (minEng `0.51.2` → `0.53.0`)
**Tag:** `v0.4.6`

Apply the **standard 5-step pattern** above.

**Verification (build-class verification + asset-existence check; operator-run wfctl install is advisory post-deploy gate, NOT a CI gate):**
- `go build ./... && go test ./... -race` PASS
- Post-release: `gh release view v0.4.6 --repo GoCodeAlone/workflow-plugin-payments --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`
- Operator advisory (NOT CI): `wfctl plugin install github.com/GoCodeAlone/workflow-plugin-payments@v0.4.6` resolves successfully

**Rollback:** cut v0.4.7 re-pinning workflow → v0.51.6 if consumers break: `go get github.com/GoCodeAlone/workflow@v0.51.6 && go mod tidy && git tag v0.4.7 && git push origin v0.4.7`.

---

### Task 2: workflow-plugin-audit-chain — pin bump v0.51.6 → v0.53.1; release v0.2.4

**Repo:** `/Users/jon/workspace/workflow-plugin-audit-chain`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.5` → `0.53.0`)
**Tag:** `v0.2.4`

Apply the **standard 5-step pattern**.

**Verification:** same shape as Task 1 — `gh release view v0.2.4 --repo GoCodeAlone/workflow-plugin-audit-chain` reports `draft=false assets≥4`.

**Rollback:** v0.2.5 re-pin workflow → v0.51.6.

---

### Task 3: workflow-plugin-tofu — pin bump v0.51.7 → v0.53.1; first GoReleaser release v0.1.3 + draft=true pre-fix

**Repo:** `/Users/jon/workspace/workflow-plugin-tofu`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`), `.goreleaser.yaml` (release.draft `true` → `false`)
**Tag:** `v0.1.3` (NOT `v0.1.0` — git tags v0.1.0/v0.1.1/v0.1.2 already exist as bare git tags without GoReleaser releases)

**EXTENDED 6-step pattern** (5 standard steps + draft pre-check). Step 0 includes BOTH the branch-create AND the draft inspection — when the standard pattern is referenced for steps 1-5, **SKIP the standard Step 1 (branch creation already done in Step 0; do not re-run `git checkout -b`).**

**Step 0 (PRE-CHECK — MANDATORY; INCLUDES branch creation, replaces standard Step 1):** Inspect `.goreleaser.yaml` for `release: draft: true`:

```bash
cd /Users/jon/workspace/workflow-plugin-tofu
git fetch origin && git checkout -b chore/workflow-v0.53.1-pin-bump origin/main
grep -A2 '^release:' .goreleaser.yaml
```

Expected output includes `draft: true`.

If found, patch:

```bash
# Edit .goreleaser.yaml — change `draft: true` to `draft: false` (or remove the line)
```

If `release.yml` references `[self-hosted, Linux, X64]` (verified — it does), confirm runners are online before tag push:

```bash
gh api /orgs/GoCodeAlone/actions/runners --jq '.runners | map(select(.status=="online")) | length'
```

Expected: ≥1 (currently AM5GamingRig + AM5GamingRig-2 + Jonathans-MBP).

**Steps 2-5: standard pattern, but SKIP standard Step 1 (branch was created in Step 0).** Apply standard Steps 2-5 (bump pin → tidy/build/test → minEng → commit/push/admin-merge/tag/monitor). The Step 5 commit includes the `.goreleaser.yaml` patch from Step 0.

Commit message:

```
chore: bump workflow pin v0.51.7 → v0.53.1; first release v0.1.3

- go.mod: workflow v0.51.7 → v0.53.1
- plugin.json: minEngineVersion 0.51.7 → 0.53.0
- .goreleaser.yaml: release.draft true → false (prior config never published; this is the first release-with-binaries)
```

**Verification:**
- `go build ./... && go test ./... -race` PASS
- `goreleaser release --snapshot --skip=publish --clean` exits 0 (catches goreleaser config errors but NOT the draft flag — the Step 0 pre-fix is the gate for that)
- Post-release: `gh release view v0.1.3 --repo GoCodeAlone/workflow-plugin-tofu --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`. If `draft=true` slips through (Step 0 missed), `gh release edit v0.1.3 --draft=false --latest` recovers.

**Rollback:** v0.1.4 re-pin workflow → v0.51.7. The v0.1.3 tag stays (Go proxy immutable).

---

### Task 4: workflow-plugin-ci-generator — pin bump v0.51.7 → v0.53.1; release v0.1.4

**Repo:** `/Users/jon/workspace/workflow-plugin-ci-generator`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v0.1.4`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v0.1.4` reports `draft=false assets≥4`.

**Rollback:** v0.1.5 re-pin workflow → v0.51.7.

---

### Task 5: workflow-plugin-github — pin bump v0.51.7 → v0.53.1; release v1.0.4

**Repo:** `/Users/jon/workspace/workflow-plugin-github`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v1.0.4`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v1.0.4` reports `draft=false assets≥4`.

**Rollback:** v1.0.5 re-pin workflow → v0.51.7.

---

### Task 6: workflow-plugin-gitlab — pin bump v0.51.7 → v0.53.1; release v1.0.3

**Repo:** `/Users/jon/workspace/workflow-plugin-gitlab`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v1.0.3`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v1.0.3` reports `draft=false assets≥4`.

**Rollback:** v1.0.4 re-pin workflow → v0.51.7.

---

### Task 7: workflow-plugin-azure — pseudo-version pin → v0.53.1; release v1.1.2

**Repo:** `/Users/jon/workspace/workflow-plugin-azure`
**Files:** `go.mod` (workflow pseudo-version `v0.51.11-0.20260514225636-522748f35474` → `v0.53.1` in raw `require` line; NO `replace` directive present), `go.sum`, `plugin.json` (minEng `0.52.0` → `0.53.0`)
**Tag:** `v1.1.2`

Apply the **standard 5-step pattern**, with one specific:

**Step 2 specific:** the workflow pin in `go.mod` is a pseudo-version directly in the `require` block, NOT in a `replace` directive (verified 2026-05-16). Update the require line:

```
require (
    github.com/GoCodeAlone/workflow v0.53.1   # was v0.51.11-0.20260514225636-522748f35474
    ...
)
```

`go mod tidy` after the change resolves to the clean v0.53.1 tag.

**Verification:**
- `go build ./... && go test ./... -race` PASS
- `gh release view v1.1.2 --repo GoCodeAlone/workflow-plugin-azure --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`
- (Defensive — azure had a draft-release issue in the prior session): if `draft=true`, `gh release edit v1.1.2 --draft=false --latest`

**Rollback:** v1.1.3 re-pin workflow → previous pseudo-version (or to v0.52.0 if the pseudo's base is still ambiguous).

---

### Task 8: workflow-plugin-admin — pin bump v0.51.7 → v0.53.1; release v1.0.1

**Repo:** `/Users/jon/workspace/workflow-plugin-admin`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v1.0.1`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v1.0.1` reports `draft=false assets≥4`.

**Rollback:** v1.0.2 re-pin workflow → v0.51.7.

---

### Task 9: workflow-plugin-bento — pin bump v0.51.7 → v0.53.1; release v1.1.3

**Repo:** `/Users/jon/workspace/workflow-plugin-bento`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v1.1.3`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v1.1.3` reports `draft=false assets≥4`.

**Rollback:** v1.1.4 re-pin workflow → v0.51.7.

---

### Task 10: workflow-plugin-authz-ui — pin bump v0.51.7 → v0.53.1; release v1.0.1 (self-hosted runner)

**Repo:** `/Users/jon/workspace/workflow-plugin-authz-ui`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v1.0.1`

**Self-hosted runner pre-check:** authz-ui's `.github/workflows/release.yml` uses `[self-hosted, Linux, X64]` + `GOPRIVATE: github.com/GoCodeAlone/*` (verified — intentional infra; NOT migrating). Before tag push, confirm runners are online:

```bash
gh api /orgs/GoCodeAlone/actions/runners --jq '.runners | map(select(.status=="online")) | length'
```

Expected: ≥1.

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v1.0.1 --repo GoCodeAlone/workflow-plugin-authz-ui --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'` → `draft=false assets≥4`.

**Rollback:** v1.0.2 re-pin workflow → v0.51.7.

---

### Task 11: workflow-plugin-authz — pin bump v0.51.7 → v0.53.1; release v0.5.4 (FIRST WAVE — agent (PR15) blocks on this tag)

**Repo:** `/Users/jon/workspace/workflow-plugin-authz`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v0.5.4`

Apply the **standard 5-step pattern**.

**CRITICAL:** PR15 (agent) cannot start until `v0.5.4` tag is published + visible on GitHub. After `git push origin v0.5.4`, confirm tag visibility before unblocking PR15:

```bash
gh release view v0.5.4 --repo GoCodeAlone/workflow-plugin-authz --json tagName,isDraft,assets --jq '"tag=\(.tagName) draft=\(.isDraft) assets=\(.assets|length)"'
```

Expected: `tag=v0.5.4 draft=false assets≥4`. Once confirmed, DM team-lead with `Authz v0.5.4 published — agent PR15 unblocked`.

**Verification:** as above + the explicit unblock signal for PR15.

**Rollback:** v0.5.5 re-pin workflow → v0.51.7. **PR15 ROLLBACK NOTE:** if v0.5.4 needs revert, agent's v0.9.3 ALSO needs to revert FIRST (cut agent v0.9.4 re-pinning both workflow → v0.51.7 AND authz → v0.5.3) BEFORE shipping authz v0.5.5. Wave-2 cascading order.

---

### Task 12: workflow-plugin-eventbus — pin bump v0.51.6 → v0.53.1; release v0.3.5

**Repo:** `/Users/jon/workspace/workflow-plugin-eventbus`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng confirm current → `0.53.0`)
**Tag:** `v0.3.5`

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v0.3.5` reports `draft=false assets≥4`.

**Rollback:** v0.3.6 re-pin workflow → v0.51.6.

---

### Task 13: workflow-plugin-security — pin bump v0.51.7 → v0.53.1; release v2.0.1 (self-hosted runner)

**Repo:** `/Users/jon/workspace/workflow-plugin-security`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng confirm current → `0.53.0`)
**Tag:** `v2.0.1`

**Self-hosted runner pre-check:** same shape as Task 10 — confirm runners online before tag push.

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v2.0.1` reports `draft=false assets≥4`.

**Rollback:** v2.0.2 re-pin workflow → v0.51.7.

---

### Task 14: workflow-plugin-supply-chain — pin bump v0.51.7 → v0.53.1; release v0.4.1 (self-hosted runner)

**Repo:** `/Users/jon/workspace/workflow-plugin-supply-chain`
**Files:** `go.mod`, `go.sum`, `plugin.json` (minEng confirm current → `0.53.0`)
**Tag:** `v0.4.1`

**Self-hosted runner pre-check:** same shape as Task 10 — confirm runners online before tag push.

Apply the **standard 5-step pattern**.

**Verification:** `gh release view v0.4.1` reports `draft=false assets≥4`.

**Rollback:** v0.4.2 re-pin workflow → v0.51.7.

---

### Task 15: workflow-plugin-agent — DUAL-BUMP workflow + authz; release v0.9.3 (WAVE 2 — depends on Task 11)

**Repo:** `/Users/jon/workspace/workflow-plugin-agent`
**Files:** `go.mod` (TWO require lines change: workflow pin AND workflow-plugin-authz pin), `go.sum`, `plugin.json` (minEng `0.51.7` → `0.53.0`)
**Tag:** `v0.9.3`

**EXTENDED 6-step pattern (DUAL-BUMP):**

**Step 0 (PRE-CHECK — MANDATORY):** Confirm authz v0.5.4 tag exists on remote. Do NOT start before this:

```bash
gh release view v0.5.4 --repo GoCodeAlone/workflow-plugin-authz --json tagName,isDraft,assets --jq '"tag=\(.tagName) draft=\(.isDraft) assets=\(.assets|length)"'
```

Expected: `tag=v0.5.4 draft=false assets≥4`. If output is anything else (404, draft=true, no tag), PAUSE — wait for team-lead unblock signal.

**Step 1: Branch + ff-pull**

```bash
cd /Users/jon/workspace/workflow-plugin-agent
git fetch origin
git checkout -b chore/workflow-v0.53.1-pin-bump origin/main
git pull --ff-only origin main
```

**Step 2: DUAL-BUMP go.mod**

Edit `go.mod` — TWO require lines change (NOT just one):

```
require (
    github.com/GoCodeAlone/workflow v0.53.1                        # was v0.51.7
    github.com/GoCodeAlone/workflow-plugin-authz v0.5.4            # was v0.2.2
    ...
)
```

**Why both lines:** `go mod tidy` (Step 3) does NOT auto-upgrade authz because workflow's go.mod doesn't import authz, so MVS has no forcing function. Both lines MUST change in this commit.

**Step 3: Tidy + build + test**

```bash
go mod tidy
go build ./...
go test ./... -race
```

Expected: clean build + tests PASS. If authz v0.5.4 has API drift from v0.2.2 (separate from workflow drift), build fails — STOP, capture, DM team-lead, file authz issue if needed.

**Step 4: minEngineVersion**

Edit `plugin.json`:

```json
"minEngineVersion": "0.53.0"
```

**Step 5: Commit + push + PR + admin-merge + tag**

```bash
git add go.mod go.sum plugin.json
git commit -m "chore: dual-bump workflow v0.51.7 → v0.53.1 + authz v0.2.2 → v0.5.4; release v0.9.3"
git push -u origin chore/workflow-v0.53.1-pin-bump
gh pr create --base main --head chore/workflow-v0.53.1-pin-bump \
  --title "chore: dual-bump workflow + authz; release v0.9.3" \
  --body "Wave 2 of post-cloud-SDK plugin sweep — depends on workflow-plugin-authz v0.5.4 tag (PR11) shipped 2026-05-16.

DUAL-BUMP rationale: agent imports workflow-plugin-authz directly. MVS does not auto-resolve authz when workflow bumps because workflow's own go.mod does not import authz.

Closes part of GoCodeAlone/workflow#656.

## Test plan
- [x] authz v0.5.4 tag confirmed published BEFORE start
- [x] go build ./... clean
- [x] go test ./... -race PASS
- [x] minEngineVersion bumped to 0.53.0"
```

After CI green + Copilot settle (~10 min) + admin-merge:

```bash
gh pr merge <N> --squash --admin --delete-branch
git checkout main && git pull
git tag v0.9.3
git push origin v0.9.3
```

**Step 6: Monitor release**

```bash
gh release view v0.9.3 --repo GoCodeAlone/workflow-plugin-agent --json assets,isDraft --jq '"draft=\(.isDraft) assets=\(.assets|length)"'
```

Expected: `draft=false assets≥4`.

**Verification:** all of above + the dual-bump line check in `git show v0.9.3:go.mod | grep -E '(workflow|authz)'`.

**Rollback (CASCADING):** if v0.9.3 needs revert: cut v0.9.4 re-pinning BOTH `workflow → v0.51.7` AND `authz → v0.5.3` (revert dual-bump). If the broader cascade requires authz v0.5.4 itself to revert, agent v0.9.4 MUST ship FIRST before authz v0.5.5 (per design's wave-2 cascading rollback rule).

---

## Out of scope (per design)

- gcp #6 + azure #4 host conformance — separate design pass with conformance test infrastructure
- workflow#640 v2 action lifecycle migration — track-only via MEMORY.md per user direction
- Catalog manifest-derivation refactor — schema/manifest/wfctl/UI/MCP refactor; high blast radius
- TypedProvider migration for the 5 plan-2 types — SDK scaffolding ready (workflow PR #686); awaits first consumer
- MessagePublisher/MessageSubscriber for IaC-bridge modules — decisions/0038 Non-Goal
- aws-sdk-go-v2 extraction from `provider/aws/`/`plugin/rbac/aws.go`/`iam/aws.go`/`artifact/s3.go`
- Security-cadence cluster (waf/sandbox/data-protection on v0.3.56) — 50+ minor versions behind; needs dedicated cadence-governance assessment
- workflow-plugin-cloud-ui — no Go go.mod; React-only structural shape
- Phase B RLV doc

## Memory updates (post-execution)

After all 15 tasks complete:

- Append to `project_cloud_sdk_extraction_complete.md`'s "Deferred / out-of-scope" section: mark "Plugin ecosystem v0.53.1 sweep" COMPLETE; flag remaining followups (#640, gcp#6, azure#4, catalog manifest-derivation, security-cadence cluster).
- Update MEMORY.md: change "Cloud-SDK Extraction COMPLETE 2026-05-16" entry to also reference the sweep completion.
- Track #640 explicitly in MEMORY.md as standalone next-pass candidate.
- Close umbrella tracking issue: `gh issue close 656 --repo GoCodeAlone/workflow --comment "Sweep complete. All 15 plugins on workflow v0.53.1 as of <date>. Tracking issue closed via supersession; remaining followups (gcp#6 + azure#4 host conformance, #640 v2 lifecycle, catalog manifest-derivation, security-cadence cluster waf/sandbox/data-protection on v0.3.56) tracked separately."`
