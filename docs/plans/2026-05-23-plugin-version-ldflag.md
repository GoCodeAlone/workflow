# Plugin Version Sync Hardening + Ldflag Contract Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stop the `sync-plugin-version.yml` PR pileup by replacing PR-opening with direct-push-to-main (or auto-merge variant for stricter repos); add a strict-semver tag-format gate in both each plugin's `release.yml` and `workflow-registry`'s ingest; restore the user's stated ldflag-contract requirement via a new `sdk.ResolveBuildVersion` helper + `IaCServeOptions.BuildVersion` field + per-plugin lint script enforced at release time.

**Architecture:** Three composable pieces — sync-mechanism fix (per plugin), tag-format gate (per plugin + central registry), and ldflag contract (SDK + per-plugin main.go + lint). The disk `plugin.json.version` field stays so the engine's pre-spawn `LoadManifest+Validate` is untouched and `workflow-registry/scripts/sync-versions.sh`'s `downloads_match_version` check continues to pass.

**Tech Stack:** Go 1.26 (SDK + wfctl); bash (workflow scripts); GitHub Actions YAML. No new dependencies.

**Base branch:** main (per repo)

**Design-only mode:** plan is written + adversarially reviewed + alignment-checked + scope-locked; execution does NOT auto-dispatch. User must explicitly authorize the cross-repo sweep on return.

---

## Scope Manifest

**PR Count:** 26
**Tasks:** 29
**Estimated Lines of Change:** ~1500 across all PRs (informational; not enforced)

**Out of scope:**
- Dropping the `plugin.json.version` field entirely (deferred; needs solving cycle-1 C1/C2/C3 in a separate design).
- Replacing goreleaser.
- Cleaning up the existing 13 stale sync PRs on `workflow-plugin-digitalocean` (already done manually 2026-05-23).
- Changing the engine's pre-spawn `LoadManifest+Validate` semantics.
- Modifying `workflow-plugin-edge-risk` (scenarios-repo contract-only; no plugin binary).

**PR Grouping:**

| PR # | Title | Tasks | Branch | Repo |
|------|-------|-------|--------|------|
| 1 | feat(sdk): ResolveBuildVersion + IaCServeOptions.BuildVersion + check-plugin-contract.sh (workflow#758) | Task 1, Task 2, Task 3, Task 4 | feat/758-sdk-buildversion-contract | workflow |
| 2 | feat(registry): tag-string semver gate in sync-versions.sh (workflow#758) | Task 5 | feat/758-registry-tag-gate | workflow-registry |
| 3 | chore(release): direct-push + tag-gate + ldflag wiring (workflow#758) | Task 6 | chore/758-release-discipline | workflow-plugin-digitalocean |
| 4 | chore(release): direct-push + tag-gate + ldflag wiring (workflow#758) | Task 7 | chore/758-release-discipline | workflow-plugin-aws |
| 5 | (same) | Task 8 | (same) | workflow-plugin-azure |
| 6 | (same) | Task 9 | (same) | workflow-plugin-gcp |
| 7 | (same) | Task 10 | (same) | workflow-plugin-tofu |
| 8 | (same) | Task 11 | (same) | workflow-plugin-ci-generator |
| 9 | (same) | Task 12 | (same) | workflow-plugin-agent |
| 10 | (same) | Task 13 | (same) | workflow-plugin-auth |
| 11 | (same) | Task 14 | (same) | workflow-plugin-authz |
| 12 | (same) | Task 15 | (same) | workflow-plugin-cms |
| 13 | (same) | Task 16 | (same) | workflow-plugin-compute |
| 14 | (same) | Task 17 | (same) | workflow-plugin-edge-compute |
| 15 | (same) | Task 18 | (same) | workflow-plugin-github |
| 16 | (same) | Task 19 | (same) | workflow-plugin-payments |
| 17 | chore(release): auto-merge + tag-gate + ldflag wiring (workflow#758) | Task 20 | chore/758-release-discipline | workflow-plugin-admin (private) |
| 18 | (same) | Task 21 | (same) | workflow-plugin-authz-ui (private) |
| 19 | (same) | Task 22 | (same) | workflow-plugin-bento (private) |
| 20 | (same) | Task 23 | (same) | workflow-plugin-cloud-ui (private) |
| 21 | (same) | Task 24 | (same) | workflow-plugin-data-protection (private) |
| 22 | (same) | Task 25 | (same) | workflow-plugin-sandbox (private) |
| 23 | (same) | Task 26 | (same) | workflow-plugin-security (private) |
| 24 | (same) | Task 27 | (same) | workflow-plugin-supply-chain (private) |
| 25 | (same) | Task 28 | (same) | workflow-plugin-waf (private) |
| 26 | docs(workflow): post-rollout retro + close #758 | Task 29 | docs/758-retro | workflow |

(PR 26 + Task 29 are scope-aligned bookends; the closing retro confirms the inventory matches what shipped.)

**Status:** Draft

---

### Task 1: Add `sdk.ResolveBuildVersion` helper

**Files:**
- Create: `plugin/external/sdk/buildversion.go`
- Test: `plugin/external/sdk/buildversion_test.go`

**Step 1: Write failing tests**

```go
package sdk

import (
	"strings"
	"testing"
)

func TestResolveBuildVersion_DeclaredSemverPassThrough(t *testing.T) {
	got := ResolveBuildVersion("v1.2.3")
	if got != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", got)
	}
}

func TestResolveBuildVersion_EmptyDeclaredFallsToBuildInfo(t *testing.T) {
	got := ResolveBuildVersion("")
	if !strings.HasPrefix(got, "(devel)") {
		t.Errorf("got %q, want prefix (devel)", got)
	}
}

func TestResolveBuildVersion_DevSentinelFallsToBuildInfo(t *testing.T) {
	got := ResolveBuildVersion("dev")
	if !strings.HasPrefix(got, "(devel)") {
		t.Errorf("got %q, want prefix (devel)", got)
	}
}
```

**Step 2: Run failing**

```
GOWORK=off go test ./plugin/external/sdk/ -run TestResolveBuildVersion -count=1
```
Expected: FAIL — undefined.

**Step 3: Implement**

```go
package sdk

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// ResolveBuildVersion returns the operator-visible build-version string.
// When declared is non-empty and not a known dev sentinel ("", "dev", "(devel)"),
// returns declared as-is. Otherwise consults runtime/debug.ReadBuildInfo() and
// returns a string like "(devel) [VCS-branch @ shortsha]" when VCS info is
// available, else "(devel)".
//
// Intended call site:
//
//	sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
//	    BuildVersion: sdk.ResolveBuildVersion(internal.Version),
//	})
//
// where internal.Version is set via "-X internal.Version=..." ldflag in the
// plugin's goreleaser configuration. For tagged release builds, returns the
// ldflag-injected semver. For local/test builds (where internal.Version
// defaults to "dev"), surfaces branch + short SHA so operators see the
// test nature in the version string.
func ResolveBuildVersion(declared string) string {
	if declared != "" && declared != "dev" && declared != "(devel)" {
		return declared
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	var sha, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				sha = s.Value[:7]
			} else {
				sha = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				modified = ".dirty"
			}
		}
	}
	if sha == "" {
		return "(devel)"
	}
	// VCS branch is not exposed by debug.ReadBuildInfo; surface SHA only.
	// Operators who want branch can correlate the SHA themselves.
	return fmt.Sprintf("(devel) [@ %s%s]", strings.TrimSpace(sha), modified)
}
```

**Step 4: Run tests pass**

```
GOWORK=off go test ./plugin/external/sdk/ -run TestResolveBuildVersion -count=1
```

**Step 5: Commit**

```bash
git add plugin/external/sdk/buildversion.go plugin/external/sdk/buildversion_test.go
git commit -m "feat(sdk): ResolveBuildVersion helper for ldflag + buildinfo surface (workflow#758)

Plugin authors call sdk.ResolveBuildVersion(internal.Version) to feed
IaCServeOptions.BuildVersion. For release builds (ldflag-set), returns the
declared semver. For local/test builds, surfaces runtime/debug.ReadBuildInfo
VCS short-SHA so operator + engine logs reflect the build's test nature."
```

---

### Task 2: Add `IaCServeOptions.BuildVersion` field + `GetManifest` augmentation

**Files:**
- Modify: `plugin/external/sdk/iacserver.go` (struct + bridge.GetManifest)
- Test: `plugin/external/sdk/iacserver_internal_test.go` or new `iacserver_buildversion_test.go`

**Step 1: Write failing test**

```go
func TestGetManifest_BuildVersionOverridesDiskVersion(t *testing.T) {
	disk := &pluginpkg.PluginManifest{Name: "x", Version: "v1.0.0", Author: "a", Description: "d"}
	b := &iacPluginServiceBridge{diskManifest: disk, buildVersion: "v1.0.1"}
	got, err := b.GetManifest(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Version != "v1.0.1" {
		t.Errorf("Version = %q, want BuildVersion-augmented v1.0.1", got.Version)
	}
}

func TestGetManifest_NoBuildVersionFallsToDiskVersion(t *testing.T) {
	disk := &pluginpkg.PluginManifest{Name: "x", Version: "v1.0.0", Author: "a", Description: "d"}
	b := &iacPluginServiceBridge{diskManifest: disk}
	got, _ := b.GetManifest(context.Background(), &emptypb.Empty{})
	if got.Version != "v1.0.0" {
		t.Errorf("Version = %q, want disk v1.0.0", got.Version)
	}
}
```

**Step 2: Failing**

```
GOWORK=off go test ./plugin/external/sdk/ -run 'TestGetManifest_BuildVersion|TestGetManifest_NoBuildVersion' -count=1
```

**Step 3: Implement**

Add `BuildVersion string` to `IaCServeOptions` (existing struct ~line 320). Add `buildVersion string` to `iacPluginServiceBridge` struct. Wire it through `ServeIaCPlugin` initialization.

Modify `GetManifest` (current line 300-312):

```go
func (b *iacPluginServiceBridge) GetManifest(_ context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
	if b.diskManifest == nil && b.buildVersion == "" {
		return nil, status.Error(codes.Unimplemented, "manifest not embedded; engine falls back to disk plugin.json")
	}
	out := &pb.Manifest{}
	if b.diskManifest != nil {
		out.Name = b.diskManifest.Name
		out.Version = b.diskManifest.Version
		out.Author = b.diskManifest.Author
		out.Description = b.diskManifest.Description
		out.ConfigMutable = b.diskManifest.ConfigMutable
		out.SampleCategory = b.diskManifest.SampleCategory
	}
	if b.buildVersion != "" {
		out.Version = b.buildVersion // augment / override
	}
	return out, nil
}
```

**Step 4: Pass + full SDK suite**

```
GOWORK=off go test ./plugin/external/sdk/... -count=1
```

**Step 5: Commit**

```bash
git add plugin/external/sdk/iacserver.go plugin/external/sdk/*_test.go
git commit -m "feat(sdk): IaCServeOptions.BuildVersion augments GetManifest (workflow#758)

When set (typically via sdk.ResolveBuildVersion(internal.Version)),
BuildVersion overrides the disk plugin.json Version field in the
GetManifest gRPC response. Engine sees the runtime-injected version; disk
field stays in place so pre-spawn LoadManifest+Validate is untouched."
```

---

### Task 3: Engine-side log on disk vs runtime version divergence (post-spawn observability)

**Files:**
- Modify: `plugin/external/adapter.go` (post-handshake GetManifest call site; cycle-1 reviewer identified at line 113-138)

Add a one-shot warning log when post-spawn `GetManifest`'s `Version` differs from `diskManifest.Version`. Pure observability; no behavior change.

**Step 1: Write failing test**

Verify the existing adapter test suite has a "GetManifest returns version X" pattern; add a case where disk says Y and runtime says X, assert the log contains "version divergence: disk=Y runtime=X" + assert plugin still loads.

**Step 2-5:** standard TDD cycle. (Plan detail intentionally lighter for purely-observability tasks.)

```bash
git add plugin/external/adapter.go plugin/external/adapter_test.go
git commit -m "feat(adapter): warn on disk-vs-runtime plugin version divergence (workflow#758)"
```

---

### Task 4: Add `scripts/check-plugin-contract.sh` lint script + docs

**Files:**
- Create: `scripts/check-plugin-contract.sh`
- Create: `docs/PLUGIN_RELEASE_GATES.md`

`check-plugin-contract.sh` reads a plugin repo path and asserts:
- `.goreleaser.yaml` contains `-X .*\.Version=` (any package path matching `*.Version=`)
- `cmd/plugin/main.go` (or `**/main.go` if cmd/plugin not present) contains `sdk.ResolveBuildVersion(`
- Exit non-zero with operator-friendly error pointing at `docs/PLUGIN_RELEASE_GATES.md`

**Step 1: Write failing test fixture**

`scripts/check-plugin-contract_test.sh` runs against `testdata/plugin-good/` (passes) and `testdata/plugin-missing-ldflag/` (fails).

**Step 2-5:** TDD cycle. Add `docs/PLUGIN_RELEASE_GATES.md` describing the convention + the tag-format whitelist regex + concurrency-group guidance + direct-push-vs-auto-merge decision rubric.

```bash
git add scripts/check-plugin-contract.sh scripts/testdata/plugin-good/ scripts/testdata/plugin-missing-ldflag/ docs/PLUGIN_RELEASE_GATES.md
git commit -m "feat: check-plugin-contract.sh lint + PLUGIN_RELEASE_GATES.md (workflow#758)

Plugin repos invoke this script as the first step of release.yml so the
ldflag contract is enforced at release time, not engine load time. Docs
enumerate the tag-format whitelist, direct-push-vs-auto-merge rubric,
and concurrency-group guidance."
```

---

### Task 5: workflow-registry — tag-string semver gate in sync-versions.sh

**Files:**
- Modify: `workflow-registry/scripts/sync-versions.sh` (after `latest_tag` is set at line ~125, before `latest_version="${latest_tag#v}"` at line 132)

**Step 1: Write failing test**

Add `workflow-registry/scripts/sync-versions_test.sh` (if no test pattern exists, add bats/shellspec or a simple test runner). Test cases:
- `latest_tag = v1.2.3` → accepted, proceed.
- `latest_tag = v1.2.3-rc1` → accepted.
- `latest_tag = v1.2.3-rc.1` → accepted.
- `latest_tag = v1.2.3-alpha2` → accepted.
- `latest_tag = v1.2.3-feat-foo.deadbeef` → REJECTED.
- `latest_tag = v1.2.3-dirty` → REJECTED.
- `latest_tag = release-2026-05` → REJECTED.

**Step 2: Failing.**

**Step 3: Implement gate**

After `latest_tag="$(gh release view ...)"` and skip-if-empty check:

```bash
# workflow#758 — strict-semver tag gate. Same regex as plugin release.yml.
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver"
  continue
fi
```

**Step 4: Tests pass.**

**Step 5: Commit.**

```bash
git add scripts/sync-versions.sh scripts/sync-versions_test.sh
git commit -m "feat(registry): strict-semver gate on upstream release tag (workflow#758)

Reject ingest of plugins whose upstream GitHub release tag does not match
the release-grade semver whitelist (vN.N.N or vN.N.N-(alpha|beta|rc)[.]N).
Same regex as plugin release.yml gate; both sources of truth converge on
the tag string. Catches plugins that bypass release.yml (manual upload,
self-hosted runner, force-push)."
```

---

### Task 6: workflow-plugin-digitalocean — direct-push + tag-gate + ldflag wiring (canonical reference)

**Files in target repo:**
- Modify: `.github/workflows/sync-plugin-version.yml` (replace `gh pr create` with direct-push; add `concurrency:`)
- Modify: `.github/workflows/release.yml` (add tag-format gate as first step; add `concurrency:`; add `check-plugin-contract.sh` call)
- Modify: `cmd/plugin/main.go` (add `sdk.ResolveBuildVersion(internal.Version)` to options)
- Modify: `plugin.json` (bump `minEngineVersion` to `0.61.0`)
- Verify: `.goreleaser.yaml` already has `-X internal.Version=` (it does per DO plugin audit)

**Step 1: Pre-flight verification**

```
grep -n '\\-X .*Version=' .goreleaser.yaml
grep -n 'sdk.ServeIaCPlugin' cmd/plugin/main.go
gh api repos/GoCodeAlone/workflow-plugin-digitalocean/branches/main/protection -q '.enforce_admins.enabled'
```
Expected: ldflag present; ServeIaCPlugin present; enforce_admins=false → direct-push variant.

**Step 2: Modify `sync-plugin-version.yml`**

Add at top:

```yaml
concurrency:
  group: plugin-version-sync-${{ github.repository }}
  cancel-in-progress: false
```

Replace the `gh pr create` block (lines 56-74) with:

```yaml
      - name: Direct-push plugin.json bump to main
        env:
          GH_TOKEN: ${{ secrets.RELEASES_TOKEN }}
        run: |
          if git diff --quiet plugin.json; then
            echo "no changes"
            exit 0
          fi
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git config user.name "github-actions[bot]"
          git fetch origin main
          git pull --ff-only origin main
          git add plugin.json
          git commit -m "chore: sync plugin.json version to ${{ steps.ver.outputs.tag }}"
          git push origin HEAD:main
```

**Step 3: Modify `release.yml`**

Add at top:

```yaml
concurrency:
  group: plugin-version-sync-${{ github.repository }}
  cancel-in-progress: false
```

Add first job step (before checkout):

```yaml
      - name: Validate tag is release-grade semver
        run: |
          TAG="${{ github.ref_name }}"
          if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.?[0-9]+)?$ ]]; then
            echo "::error::Tag $TAG is not release-grade semver (allowed: vN.N.N or vN.N.N-(alpha|beta|rc)[.]N)"
            exit 1
          fi
```

Add after checkout, before goreleaser:

```yaml
      - name: Check plugin contract (ldflag + ResolveBuildVersion)
        run: |
          curl -sSfL https://raw.githubusercontent.com/GoCodeAlone/workflow/main/scripts/check-plugin-contract.sh | bash -s -- "$(pwd)"
```

**Step 4: Modify `cmd/plugin/main.go`**

```go
sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
    BuildVersion: sdk.ResolveBuildVersion(internal.Version),
})
```

**Step 5: Bump `plugin.json.minEngineVersion` to `0.61.0`.**

**Step 6: Local verify**

```
GOWORK=off go build ./cmd/plugin
./cmd/plugin/workflow-plugin-digitalocean --version 2>/dev/null || true  # smoke; binary doesn't crash
GOWORK=off go test ./... -count=1
```

**Step 7: Commit + push + open PR**

```bash
git add .github/workflows/sync-plugin-version.yml .github/workflows/release.yml cmd/plugin/main.go plugin.json
git commit -m "chore(release): direct-push sync + tag-gate + ldflag contract (workflow#758)

- sync-plugin-version.yml: direct-push to main (no PR), concurrency-safe
- release.yml: tag-format gate (release-grade semver) + check-plugin-contract.sh + concurrency
- cmd/plugin/main.go: sdk.ServeIaCPlugin now passes BuildVersion via ResolveBuildVersion
- plugin.json: minEngineVersion 0.57.1 → 0.61.0"

git push -u origin chore/758-release-discipline

gh pr create --title "chore(release): direct-push + tag-gate + ldflag wiring (workflow#758)" --body "..."
```

**Rollback:** revert commit; sync-plugin-version reverts to PR-opening; release.yml gate removed; main.go reverts to zero-value IaCServeOptions.

---

### Tasks 7-19: Public plugin repos (direct-push variant)

Each task is a structural clone of Task 6 against the listed repo. Pre-flight verification per repo: ldflag present in `.goreleaser.yaml`; `sdk.ServeIaCPlugin` (or `sdk.Serve`) in `cmd/plugin/main.go` (or equivalent); branch protection `enforce_admins: false`. If any check fails, switch that repo to the auto-merge variant (Task 20's template).

### Task 7: workflow-plugin-aws — direct-push variant per Task 6

Same template as Task 6 applied to workflow-plugin-aws. Verify ldflag, main.go pattern, enforce_admins=false pre-flight. Same files, same commit shape.

### Task 8: workflow-plugin-azure — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-azure.

### Task 9: workflow-plugin-gcp — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-gcp.

### Task 10: workflow-plugin-tofu — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-tofu.

### Task 11: workflow-plugin-ci-generator — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-ci-generator.

### Task 12: workflow-plugin-agent — direct-push variant per Task 6 (non-IaC adaptation)

Same as Task 7 against workflow-plugin-agent. Agent plugin uses `sdk.Serve` (non-IaC); adapt the main.go update to `sdk.Serve(srv, sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))` if Task 2's `WithBuildVersion` option lands on sdk.Serve. If sdk.Serve doesn't yet have the option, file a follow-up before this task ships.

### Task 13: workflow-plugin-auth — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-auth.

### Task 14: workflow-plugin-authz — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-authz.

### Task 15: workflow-plugin-cms — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-cms.

### Task 16: workflow-plugin-compute — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-compute.

### Task 17: workflow-plugin-edge-compute — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-edge-compute.

### Task 18: workflow-plugin-github — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-github.

### Task 19: workflow-plugin-payments — direct-push variant per Task 6

Same as Task 7 against workflow-plugin-payments.

---

### Tasks 20-28: Private plugin repos (auto-merge variant)

Per cycle-3 design: private plugins use the auto-merge variant for sync-plugin-version.yml — workflow opens PR + immediately runs `gh pr merge --admin --auto --squash --delete-branch`. The PR exists for audit history; no human in the merge queue.

Auto-merge variant: after `gh pr create`:

```yaml
      - name: Auto-merge sync PR with admin
        env:
          GH_TOKEN: ${{ secrets.RELEASES_TOKEN }}
        run: |
          gh pr merge "$BRANCH" --admin --squash --delete-branch --repo ${{ github.repository }}
```

All other steps (tag-gate, check-plugin-contract.sh, main.go update, plugin.json minEngineVersion bump) identical to Task 6.

### Task 20: workflow-plugin-admin — auto-merge variant per template above

Same as Task 6 but with auto-merge sync. Verify ldflag + main.go pre-flight.

### Task 21: workflow-plugin-authz-ui — auto-merge variant per template above

Same as Task 20 against workflow-plugin-authz-ui.

### Task 22: workflow-plugin-bento — auto-merge variant per template above

Same as Task 20 against workflow-plugin-bento.

### Task 23: workflow-plugin-cloud-ui — auto-merge variant per template above

Same as Task 20 against workflow-plugin-cloud-ui.

### Task 24: workflow-plugin-data-protection — auto-merge variant per template above

Same as Task 20 against workflow-plugin-data-protection.

### Task 25: workflow-plugin-sandbox — auto-merge variant per template above

Same as Task 20 against workflow-plugin-sandbox.

### Task 26: workflow-plugin-security — auto-merge variant per template above

Same as Task 20 against workflow-plugin-security.

### Task 27: workflow-plugin-supply-chain — auto-merge variant per template above

Same as Task 20 against workflow-plugin-supply-chain.

### Task 28: workflow-plugin-waf — auto-merge variant per template above

Same as Task 20 against workflow-plugin-waf.

---

### Task 29: Close-out retro

**Files:**
- Create: `docs/retros/2026-05-NN-workflow-758-plugin-version-discipline.md`

Document what shipped, what surprised (per repo), and what's deferred (the field-removal path that cycle-1 surfaced as too costly).

**Verification:** retro file exists; close issue #758 with cross-link.

```bash
git add docs/retros/2026-05-NN-workflow-758-plugin-version-discipline.md
git commit -m "docs(retro): workflow#758 plugin-version discipline complete"
gh issue close 758 --comment "Shipped per docs/retros/2026-05-NN-workflow-758-plugin-version-discipline.md"
```

---

## Risk register

- **R1 (concurrency races):** Multiple tags fired within seconds. Mitigated by `concurrency: group: plugin-version-sync-${{ github.repository }}` + `cancel-in-progress: false` + fast-forward push semantics.
- **R2 (auto-merge race):** auto-merge variant fires `gh pr merge --auto` before required-check completes; second sync workflow opens a stacked PR. Mitigated by per-repo `concurrency:` group serializing both workflows on the same `(repo, group)` key.
- **R3 (direct-push permission revoked mid-rollout):** A repo's branch protection changes between pre-flight verify (Task N step 1) and the actual push. Mitigated by direct-push failing loudly (non-zero exit + Actions log); operator switches that repo to auto-merge variant.
- **R4 (minEngineVersion bump strands operators):** Plugin v_next requires engine ≥0.61.0. Operators on pinned wfctl < v0.61 cannot install. Mitigated by SDK change (Task 2) being additive — older engines fall back to disk plugin.json Version unchanged. The minEngineVersion bump is conservative because the engine change is opt-in (only fires when BuildVersion is non-empty).
- **R5 (workflow-registry sync schedule):** registry sync is cron-driven; PR 2 lands but sync hasn't run yet. The next cron tick catches up.

## Pipeline gate at end of plan

**This plan is design-only mode.** After adversarial-design-review (plan phase) PASS + alignment-check PASS + scope-lock applied, the pipeline STOPS. The cross-repo execution (Tasks 5-28) requires explicit user authorization on return. Task 1-4 (workflow SDK PR) and Task 29 (retro) can be authorized independently of the multi-plugin sweep.
