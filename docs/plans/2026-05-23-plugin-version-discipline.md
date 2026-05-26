# Plugin Version Discipline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Per workflow#758: stop the sync-plugin-version.yml PR pileup by deleting the workflow; deliver an operator-visible runtime plugin version via SDK + ldflag contract; gate non-semver tags at both `wfctl plugin validate-contract --for-publish` (operator-facing) and `workflow-registry/scripts/sync-versions.sh` (registry ingest). Pilot the per-plugin migration on 5 representative repos; remaining 56 deferred to a follow-up sweep with separate authorization.

**Architecture:** SDK adds `sdk.ResolveBuildVersion(declared) + IaCServeOptions.BuildVersion + sdk.WithBuildVersion`. New `wfctl plugin validate-contract` subcommand performs static checks (manifest valid, capabilities/minEngineVersion populated, main.go invokes ResolveBuildVersion, goreleaser ldflag pattern present) and tag-format gate. Registry sync rejects non-semver tags as defense in depth. Per-plugin migration: delete sync workflow + sentinel committed version `"0.0.0"` + wire ResolveBuildVersion in main.go + add release.yml pre+post build gates.

**Tech Stack:** Go 1.26; bash for workflow scripts; GitHub Actions YAML. No new dependencies.

**Base branch:** main (per repo)

---

## Scope Manifest

**PR Count:** 9
**Tasks:** 9

(Note: PR 8 is a no-PR row — it produces a follow-up issue only. Effective code/doc PRs = 8.)
**Estimated Lines of Change:** ~1200 across all PRs

**Out of scope:**
- Migrating the remaining 56 plugin repos beyond the pilot batch (filed as follow-up issue; user authorizes separately based on pilot outcome).
- Full SemVer 2.0.0 pre-release tag support (deferred; requires concerted ParseSemver + sync-versions + wfctl install update).
- Binary-vs-file capability freshness gate (deferred per cycle 4-A1 I3).
- Establishing release pipelines in gap-repos (repos without release.yml + .goreleaser).
- Engine-side hard-blocking minEngineVersion mismatches (existing soft-warn behavior is fine).
- Cleaning up the 13 historical stale sync PRs (already done manually 2026-05-23).

**PR Grouping:**

| PR # | Title | Tasks | Branch | Repo |
|------|-------|-------|--------|------|
| 1 | feat(sdk+wfctl): ResolveBuildVersion + IaCServeOptions.BuildVersion + WithBuildVersion + wfctl plugin validate-contract (#758) | Task 1 | feat/758-plugin-version-ldflag | workflow |
| 2 | feat(registry): tag-string semver gate in sync-versions.sh (#758) | Task 2 | feat/758-registry-tag-gate | workflow-registry |
| 3 | chore(release): delete sync-plugin-version + wire ResolveBuildVersion + sentinel + release.yml gates (#758) | Task 3 | chore/758-release-discipline | workflow-plugin-digitalocean |
| 4 | chore(release): delete sync-plugin-version + wire ResolveBuildVersion + sentinel + release.yml gates (#758) | Task 4 | chore/758-release-discipline | workflow-plugin-aws |
| 5 | chore(release): delete sync-plugin-version + wire ResolveBuildVersion + sentinel + release.yml gates (#758) | Task 5 | chore/758-release-discipline | workflow-plugin-gcp |
| 6 | chore(release): delete sync-plugin-version + wire ResolveBuildVersion + sentinel + release.yml gates (#758) | Task 6 | chore/758-release-discipline | workflow-plugin-azure |
| 7 | chore(release): delete sync-plugin-version + wire ResolveBuildVersion + sentinel + release.yml gates (#758) | Task 7 | chore/758-release-discipline | workflow-plugin-github |
| 8 | (no PR — file follow-up issue for remaining 56 plugin repo sweep) | Task 8 | n/a | workflow |
| 9 | docs(retro): workflow#758 pilot retro + close issue | Task 9 | docs/758-retro | workflow |

**Status:** Locked 2026-05-23T20:08:47Z

---

### Task 1: Workflow Layer 1 — SDK + wfctl validate-contract (single PR, single branch)

**Files in workflow repo:**
- Create: `plugin/external/sdk/buildversion.go`
- Create: `plugin/external/sdk/buildversion_test.go`
- Modify: `plugin/external/sdk/iacserver.go` (IaCServeOptions struct + iacPluginServiceBridge + GetManifest)
- Modify: `plugin/external/sdk/grpc_server.go` (grpcServer struct + GetManifest)
- Modify: `plugin/external/sdk/serve.go` (WithBuildVersion ServeOption)
- Create: `plugin/external/sdk/serve_test.go` additions or `plugin/external/sdk/grpc_server_test.go` additions
- Modify: `plugin/external/adapter.go` (one-shot version-divergence warn log; small)
- Create: `cmd/wfctl/plugin_validate_contract.go`
- Create: `cmd/wfctl/plugin_validate_contract_test.go`
- Create: `cmd/wfctl/testdata/plugin_validate_contract/good/`, `.../bad-missing-caps/`, `.../bad-missing-ldflag/`, `.../bad-tag/`, `.../release-dir-good/`, `.../release-dir-stale/`
- Modify: `cmd/wfctl/plugin.go` (register subcommand)
- Create: `docs/PLUGIN_RELEASE_GATES.md`

**Step 1: Write failing tests for `ResolveBuildVersion`**

`plugin/external/sdk/buildversion_test.go`:

```go
package sdk

import (
    "strings"
    "testing"
)

func TestResolveBuildVersion_ReleaseDeclaredPassThrough(t *testing.T) {
    got := ResolveBuildVersion("v1.2.3")
    if got != "v1.2.3" {
        t.Errorf("got %q, want v1.2.3", got)
    }
}

func TestResolveBuildVersion_EmptyFallsToBuildInfo(t *testing.T) {
    got := ResolveBuildVersion("")
    if !strings.HasPrefix(got, "(devel)") {
        t.Errorf("got %q, want prefix (devel)", got)
    }
}

func TestResolveBuildVersion_DevSentinelFallsToBuildInfo(t *testing.T) {
    got := ResolveBuildVersion("dev")
    if !strings.HasPrefix(got, "(devel)") {
        t.Errorf("got %q, want prefix (devel) for dev sentinel", got)
    }
}

func TestResolveBuildVersion_DevelSentinelFallsToBuildInfo(t *testing.T) {
    got := ResolveBuildVersion("(devel)")
    if !strings.HasPrefix(got, "(devel)") {
        t.Errorf("got %q, want prefix (devel) for (devel) sentinel", got)
    }
}
```

**Step 2: Run failing**

```
GOWORK=off go test ./plugin/external/sdk/ -run TestResolveBuildVersion -count=1
```
Expected: FAIL undefined.

**Step 3: Implement `ResolveBuildVersion`**

`plugin/external/sdk/buildversion.go`:

```go
package sdk

import (
    "fmt"
    "runtime/debug"
)

// ResolveBuildVersion returns the operator-visible build-version string.
// declared non-empty + not a known dev sentinel → returned as-is (typical
// for goreleaser-built binaries where the ldflag injects the release tag).
// Otherwise consults runtime/debug.ReadBuildInfo() as fallback:
//   "(devel) [@ shortsha[.dirty]]" when vcs.revision is set
//   "(devel)" when no VCS info
//
// Intended call sites (plugin author chooses ANY package-level Version var name):
//
//   var Version = "dev"   // ldflag-injected at release
//
//   sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
//       BuildVersion: sdk.ResolveBuildVersion(internal.Version),
//   })
//   sdk.Serve(p, sdk.WithManifestProvider(m),
//       sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))
//
// Goreleaser config provides the tag:
//   ldflags:
//     - -X github.com/<...>/internal.Version={{.Version}}
//
// Mirrors the wfctl pattern at cmd/wfctl/main.go:37-50.
func ResolveBuildVersion(declared string) string {
    switch declared {
    case "", "dev", "(devel)":
        return buildInfoVersion()
    }
    return declared
}

func buildInfoVersion() string {
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
    return fmt.Sprintf("(devel) [@ %s%s]", sha, modified)
}
```

**Step 4: Pass**

```
GOWORK=off go test ./plugin/external/sdk/ -run TestResolveBuildVersion -count=1
```

**Step 5: Add `BuildVersion` field to `IaCServeOptions` + bridge wiring**

In `plugin/external/sdk/iacserver.go`:

- Add `BuildVersion string` to `IaCServeOptions` struct (current ~line 320).
- Add `buildVersion string` to `iacPluginServiceBridge` struct.
- Wire in `registerAllIaCProviderServicesWithOpts` (current ~line 87-90 area): set bridge's `buildVersion` from `opts.BuildVersion`.
- Modify `iacPluginServiceBridge.GetManifest` (current line 300-312):

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
        out.Version = b.buildVersion
    }
    return out, nil
}
```

**Step 6: Test bridge augmentation**

In `plugin/external/sdk/iacserver_internal_test.go` (or new file):

```go
func TestIaCBridge_GetManifest_BuildVersionOverridesDiskVersion(t *testing.T) {
    disk := &pluginpkg.PluginManifest{Name: "x", Version: "v1.0.0", Author: "a", Description: "d"}
    b := &iacPluginServiceBridge{diskManifest: disk, buildVersion: "v1.0.1"}
    got, err := b.GetManifest(context.Background(), &emptypb.Empty{})
    if err != nil { t.Fatal(err) }
    if got.Version != "v1.0.1" {
        t.Errorf("Version = %q, want BuildVersion-augmented v1.0.1", got.Version)
    }
}

func TestIaCBridge_GetManifest_NoBuildVersionFallsToDiskVersion(t *testing.T) {
    disk := &pluginpkg.PluginManifest{Name: "x", Version: "v1.0.0", Author: "a", Description: "d"}
    b := &iacPluginServiceBridge{diskManifest: disk}
    got, _ := b.GetManifest(context.Background(), &emptypb.Empty{})
    if got.Version != "v1.0.0" {
        t.Errorf("Version = %q, want disk v1.0.0", got.Version)
    }
}
```

Run + iterate until passes.

**Step 7: Add `WithBuildVersion` ServeOption + grpcServer wiring**

In `plugin/external/sdk/grpc_server.go`:

- Add `buildVersion string` field to `grpcServer` struct (current line 21-39, alongside `diskManifest`).
- Modify `GetManifest` (current line 142-162): after the existing `if s.diskManifest != nil { ... } else { provider.Manifest() }` block computes the manifest, append `if s.buildVersion != "" { m.Version = s.buildVersion }` before return.

In `plugin/external/sdk/serve.go`:

```go
// WithBuildVersion sets the runtime build-version surfaced via GetManifest.
// Single-channel: takes precedence over any ManifestProvider.Version or
// provider.Manifest().Version. Typically populated via
// sdk.ResolveBuildVersion(<plugin's ldflag-injected Version var>).
func WithBuildVersion(v string) ServeOption {
    return func(s *grpcServer) {
        s.buildVersion = v
    }
}
```

**Step 8: Test Serve-path augmentation**

```go
func TestGRPCServer_GetManifest_BuildVersionOverridesDiskVersion(t *testing.T) {
    s := &grpcServer{
        diskManifest: &pluginpkg.PluginManifest{Name: "x", Version: "v1.0.0", Author: "a", Description: "d"},
        buildVersion: "v1.0.1",
    }
    got, _ := s.GetManifest(context.Background(), &emptypb.Empty{})
    if got.Version != "v1.0.1" {
        t.Errorf("Version = %q, want v1.0.1", got.Version)
    }
}
```

**Step 9: Add `wfctl plugin validate-contract` subcommand**

Tag source precedence for `--for-publish` (check 6): explicit `--tag <vX.Y.Z>` flag > `$GITHUB_REF_NAME` env (set automatically in GitHub Actions on tag push) > `git describe --tags --exact-match HEAD` (local dev fallback). Test fixtures must exercise both `--tag` and `GITHUB_REF_NAME` paths.

`cmd/wfctl/plugin_validate_contract.go`: new subcommand implementing the §3 design checks. Flags: `--for-publish`, `--tag`, `--release-dir`. Checks:
1. plugin.json exists + parses + Validate() OK (sentinel `0.0.0` OK)
2. capabilities populated
3. minEngineVersion populated
4. main.go (any `cmd/**/main.go` or `.go` file in repo root) contains `sdk.ResolveBuildVersion(` AND (`IaCServeOptions{` with `BuildVersion:` OR `sdk.WithBuildVersion(`)
5. `.goreleaser.{yaml,yml}` contains regex `-X .*\.Version=`
6. (--for-publish) Tag matches `^v[0-9]+\.[0-9]+\.[0-9]+$`
7. (--release-dir) `<dir>/plugin.json` `.version` field equals `--tag` value with leading `v` stripped.

Register in `cmd/wfctl/plugin.go`.

**Step 10: testdata fixtures + table-driven tests**

`cmd/wfctl/testdata/plugin_validate_contract/`:
- `good/plugin.json` (sentinel `0.0.0`, capabilities populated, minEngineVersion populated)
- `good/cmd/plugin/main.go` (calls sdk.ResolveBuildVersion + IaCServeOptions BuildVersion)
- `good/.goreleaser.yaml` (has `-X test/internal.Version=`)
- `bad-missing-caps/plugin.json` (no capabilities)
- `bad-missing-ldflag/.goreleaser.yaml` (no -X line)
- `bad-tag/` (good + invokes --tag v1.2 → fails)
- `release-dir-good/plugin.json` (.version = "1.2.3" → --tag v1.2.3 passes)
- `release-dir-stale/plugin.json` (.version = "1.2.0" → --tag v1.2.3 fails)

`cmd/wfctl/plugin_validate_contract_test.go`: table-driven test invoking `runPluginValidateContract` against each fixture.

**Step 11: Create `docs/PLUGIN_RELEASE_GATES.md`**

Document the contract, the sentinel pattern, the goreleaser ldflag requirement, the release.yml two-step gate pattern (pre-build static checks + post-build tarball verify). Reference workflow#758.

**Step 12: Run full test sweep**

```
GOWORK=off go test ./plugin/external/sdk/... ./cmd/wfctl/... -count=1 -race
```
Must be green.

**Step 13: Commit (single squash; multiple commit chunks fine on the branch)**

```bash
git add plugin/external/sdk/buildversion.go plugin/external/sdk/buildversion_test.go
git commit -m "feat(sdk): ResolveBuildVersion helper for ldflag + buildinfo (#758)"

git add plugin/external/sdk/iacserver.go plugin/external/sdk/grpc_server.go plugin/external/sdk/serve.go plugin/external/sdk/*_test.go
git commit -m "feat(sdk): IaCServeOptions.BuildVersion + WithBuildVersion ServeOption (#758)"

git add plugin/external/adapter.go plugin/external/adapter_test.go 2>/dev/null
git commit -m "feat(adapter): warn on disk-vs-runtime plugin version divergence (#758)" 2>/dev/null || true

git add cmd/wfctl/plugin_validate_contract.go cmd/wfctl/plugin_validate_contract_test.go cmd/wfctl/plugin.go cmd/wfctl/testdata/plugin_validate_contract/
git commit -m "feat(wfctl): plugin validate-contract subcommand + --for-publish + --release-dir (#758)"

git add docs/PLUGIN_RELEASE_GATES.md
git commit -m "docs: PLUGIN_RELEASE_GATES.md (#758)"
```

**Step 14: Push + PR + monitor + admin-merge**

```bash
git push -u origin feat/758-plugin-version-ldflag
gh pr create --title "feat(sdk+wfctl): ResolveBuildVersion + WithBuildVersion + plugin validate-contract (#758)" --body "...long form summary..."
gh pr checks <N> --watch  # wait for CI green
gh pr merge <N> --squash --admin --delete-branch
```

**Step 15: Tag workflow v0.61.0**

```bash
git checkout main && git pull --ff-only
git tag v0.61.0 -m "v0.61.0 — plugin release-gate SDK + wfctl validate-contract (#758)"
git push origin v0.61.0
```

**Rollback:** revert the PR; SDK reverts to pre-758 state; existing plugins keep working (changes are additive).

---

### Task 2: workflow-registry Layer 2 — tag-string semver gate

**Files in workflow-registry:**
- Modify: `scripts/sync-versions.sh` (gate after `latest_tag` set at line ~125)
- Create: `scripts/testdata/tag-gate/` (optional fixtures)

**Step 1: Add gate**

After `latest_tag="$(gh release view ...)"` skip-empty check:

```bash
# workflow#758 — strict-semver gate.
if [[ ! "$latest_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "  REJECT  $plugin_name — upstream release tag $latest_tag is not release-grade semver (engine ParseSemver requires flat M.m.p)"
  continue
fi
```

**Step 2: Test (manual / inline)**

Invoke against a dry-run plugin entry with malformed tag in test fixture; assert REJECT line.

**Step 3: Commit + PR + admin-merge**

```bash
git checkout -b feat/758-registry-tag-gate
git add scripts/sync-versions.sh
git commit -m "feat(registry): strict-semver gate on upstream release tag (#758)

Reject ingest of plugins whose upstream GitHub release tag does not match
the release-grade semver whitelist (^v\d+\.\d+\.\d+\$). Catches plugins
that bypass release.yml (manual upload, self-hosted runner, force-push)."
git push -u origin feat/758-registry-tag-gate
gh pr create ...
gh pr merge --squash --admin --delete-branch
```

**Rollback:** single-revert.

---

### Task 3: workflow-plugin-digitalocean — pilot Layer 3 (canonical)

**Files in target repo:**
- Delete: `.github/workflows/sync-plugin-version.yml`
- Modify: `.github/workflows/release.yml` (add setup-wfctl + pre-build validate-contract + post-build verify)
- Modify: `cmd/plugin/main.go` (pass `sdk.ResolveBuildVersion(internal.Version)` to `IaCServeOptions.BuildVersion`)
- Modify: `plugin.json` (`.version` → `"0.0.0"`)
- Verify (no edit): `.goreleaser.yaml` has `-X github.com/.../internal.Version=` (already present at line 25)

**Step 1: Pre-flight audit**

```bash
ls .github/workflows/sync-plugin-version.yml && \
ls .github/workflows/release.yml && \
(ls .goreleaser.yaml || ls .goreleaser.yml) && \
(MAIN=$(find cmd -name main.go | head -1); [ -n "$MAIN" ] && echo "main: $MAIN") && \
grep -qE '\-X.*\.Version=' .goreleaser.yaml .goreleaser.yml 2>/dev/null && \
grep -qE 'sdk\.(Serve|ServeIaCPlugin)' $(find cmd -name main.go) && \
grep -rqE 'var (Version|ProviderVersion)\b' . --include='*.go' && \
gh api repos/GoCodeAlone/<this-repo>/branches/main/protection -q '.enforce_admins.enabled' | grep -q '^false$' && \
echo OK || echo FAIL
```
Expected: OK. (Variations per repo: non-IaC uses `sdk.Serve` not `ServeIaCPlugin`; main.go path varies (`cmd/plugin/main.go` vs `cmd/workflow-plugin-<name>/main.go`); Version var may live in `internal` or `provider` package. The audit accepts variance and just verifies presence.)

**Step 2: Apply migration**

```bash
git checkout -b chore/758-release-discipline
git rm .github/workflows/sync-plugin-version.yml
```

Edit `cmd/plugin/main.go`:

```go
package main

import (
    "github.com/GoCodeAlone/workflow-plugin-digitalocean/internal"
    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
    sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
        BuildVersion: sdk.ResolveBuildVersion(internal.Version),
    })
}
```

Edit `plugin.json` `.version` field to `"0.0.0"`.

Edit `.github/workflows/release.yml` — add at top of `release:` job (before checkout):

```yaml
      - uses: GoCodeAlone/setup-wfctl@v1
        with:
          version: v0.61.0
      - name: Validate plugin contract for publish (pre-build)
        run: |
          wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .
```

Add BETWEEN the goreleaser step and the `Publish release (was draft during asset upload)` step (so a verify-fail halts before the draft→public promotion):

```yaml
      - name: Verify shipped plugin.json carries tag (post-build)
        run: |
          if [ -f .release/plugin.json ]; then
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir .release .
          else
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir . .
          fi
```

**Step 3: Local verify (with locally-built wfctl@v0.61.0 or main HEAD)**

```bash
GOWORK=off go build ./...
GOWORK=off go test ./... -count=1
# wfctl plugin validate-contract . (requires wfctl built from workflow main; OK to skip if local wfctl not bumped yet)
```

**Step 4: Commit + push + PR + admin-merge**

```bash
git add -A
git commit -m "chore(release): delete sync-plugin-version + ResolveBuildVersion + sentinel + release.yml gates (#758)

- Delete sync-plugin-version.yml (committed plugin.json version no longer
  syncs with tag; goreleaser before-hook + binary ldflag carry the truth)
- plugin.json.version → \"0.0.0\" (sentinel; parses through ParseSemver)
- cmd/plugin/main.go: pass sdk.ResolveBuildVersion(internal.Version)
- release.yml: pre-build wfctl plugin validate-contract --for-publish +
  post-build --release-dir verification

Rollback: revert PR; restores prior sync mechanism."
git push -u origin chore/758-release-discipline
gh pr create --title "chore(release): plugin-version discipline (#758)" --body "..."
gh pr checks <N> --watch
gh pr merge <N> --squash --admin --delete-branch
```

---

### Task 4: workflow-plugin-aws — pilot Layer 3 per Task 3 template

Same as Task 3 against `workflow-plugin-aws`, with these differences:

- main.go lives at `cmd/workflow-plugin-aws/main.go` (not `cmd/plugin/main.go`).
- AWS `.goreleaser.yaml` injects BOTH `provider.ProviderVersion` AND `internal.Version`. Check which package-level vars actually exist (`grep -rn 'var \(Version\|ProviderVersion\)' provider/ internal/`). If neither exists, current goreleaser-built binaries silently ship `(devel)` — declare a `var Version = "dev"` in the package the migration will reference (recommend `internal/` for parity with DO).
- Pass the existing var to `sdk.ResolveBuildVersion(...)`. Migration must add an import for that package in main.go.
- Pre-flight audit (Step 1) accepts variance.

### Task 5: workflow-plugin-gcp — pilot Layer 3 per Task 3 template

Same as Task 4 against `workflow-plugin-gcp`.

### Task 6: workflow-plugin-azure — pilot Layer 3 per Task 3 template

Same as Task 4 against `workflow-plugin-azure`.

### Task 7: workflow-plugin-github — pilot Layer 3 per Task 3 template (non-IaC adaptation)

Same as Task 4 against `workflow-plugin-github`, with these differences:

- github plugin is non-IaC; current main.go calls `sdk.Serve(internal.NewGitHubPlugin())` with NO ServeOption args.
- Migration changes main.go to:

  ```go
  sdk.Serve(internal.NewGitHubPlugin(),
      sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
  )
  ```

- If `var Version = "dev"` doesn't yet exist in the package the goreleaser ldflag targets (likely `internal`), add it as part of the migration. Verify by reading `.goreleaser.yaml` ldflag line; e.g. `-X github.com/GoCodeAlone/workflow-plugin-github/internal.Version={{.Version}}` means add `var Version = "dev"` to `internal/`.

- Pre-flight audit (Step 1) accepts `sdk.Serve` per the updated grep `sdk\.(Serve|ServeIaCPlugin)`.

---

### Task 8: File follow-up issue for remaining 56 plugin repos

**Files:** none (uses `gh issue create` only).

Audit identified 61 plugin repos with full release pipelines. Pilot covers DO + AWS + GCP + Azure + github (5 repos). Remaining 56 need the same migration via parallel agent fan-out, but user authorization required to commit to a 56-PR sweep.

**Step 1: File issue**

```bash
gh issue create --title "Apply plugin-version discipline migration to remaining 56 plugin repos (#758 follow-up)" --body "$(cat <<'EOF'
## Context

workflow#758 pilot landed in DO, AWS, GCP, Azure, github plugin repos.
Pattern is mechanical (delete sync-plugin-version.yml + plugin.json sentinel
+ main.go ResolveBuildVersion call + release.yml pre+post gates) and
should fan-out cleanly via parallel agents.

## Remaining repos (56)

(enumerate: workflow-plugin-admin, agent, analytics, approval, audit,
audit-chain, audit-chain-docs, auth, authz, authz-ui, bento, botdetect,
broker, ci-generator, cicd, cms, compute, crm, data-engineering,
data-protection, datadog, discord, dnd, economy, erp, eventbus, gitlab,
infra, ... — full list at exec time via `ls workflow-plugin-*` minus pilot)

## Plan

After pilot lands cleanly, request user authorization to dispatch parallel
agents (worktree-isolated per repo) to apply the canonical Task 3
template. Each agent: pre-flight audit + 4-file edit + push + PR + monitor
+ admin-merge.

Estimated effort: 56 PRs, ~1 hour wall time with 5-10 agents in parallel.
EOF
)"
```

**Step 2: Save issue number for retro reference.**

---

### Task 9: Close-out retro

**Files:**
- Create: `docs/retros/2026-05-NN-workflow-758-plugin-version-discipline.md`

Document pilot outcome; any per-repo variance discovered; recommended adjustments before fan-out.

```bash
git checkout -b docs/758-retro main
git add docs/retros/2026-05-NN-workflow-758-plugin-version-discipline.md
git commit -m "docs(retro): workflow#758 plugin-version discipline pilot complete"
git push -u origin docs/758-retro
gh pr create ... && gh pr merge --squash --admin --delete-branch
gh issue close 758 --comment "Pilot shipped; follow-up #<N> tracks remaining sweep."
```

---

## Pipeline gate at end of plan

This plan executes Layer 1 + Layer 2 + Layer 3 pilot (5 plugins) + follow-up issue + retro autonomously. Layer 3b (remaining 56 plugins) is filed as a follow-up issue requiring separate user authorization based on pilot outcome.

**Hard ordering gate (cycle 4-P1 I2):** Tasks 3-7 (Layer 3 pilot PRs) depend on workflow v0.61.0 being TAGGED + reachable (`gh release view v0.61.0 --repo GoCodeAlone/workflow` returns non-error). Task 1 Step 15 creates that tag immediately after Task 1's PR merges. Layer 3 dispatch MUST wait for that step. Task 2 (workflow-registry) has no such dependency and may run in parallel with Task 1.

**Implementation note for `wfctl plugin validate-contract` rule 4 (cycle 4-P1 I6):** the grep for `sdk.ResolveBuildVersion(` AND (`IaCServeOptions{...BuildVersion:` OR `sdk.WithBuildVersion(`) MUST be whole-file scoped, not line-scoped (gofmt formats multi-line). When a repo has multiple `cmd/**/main.go` binaries, rule 4 PASSES if ANY of them satisfies both patterns (typical: only the plugin's serve binary does; other cmds are operator tools).
