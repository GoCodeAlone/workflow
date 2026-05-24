# wfctl Plugin Registry-Sync + Template Modernization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Per workflow#762: replace `workflow-registry/scripts/sync-versions.sh` + `sync-core-manifests.sh` + `generate-readme.sh` with a `wfctl plugin registry-sync` subcommand (collapses 3 bash scripts → 1 Go entrypoint; single regex source-of-truth shared with `validate-contract`); rename scaffold repos (`workflow-plugin-template` → `scaffold-workflow-plugin`; same for private) so they aren't treated as plugins; bake workflow#758 compliance into the scaffold content; delete the now-stale registry entry. Layer 3b/c per-repo sweep (54 repos) is filed as follow-up for separate execution wave.

**Architecture:** New wfctl subcommand `wfctl plugin registry-sync` with `core` + `readme` sub-modes. Initial workflow-registry CI runs Go in dry-run alongside authoritative bash for one parity cycle; followup PR deletes bash + swaps `--fix` ownership to Go. Scaffold rename + content modernization happens in parallel with the parity cycle. Layer 3b/c (54-repo sweep) deferred to follow-up issue.

**Tech Stack:** Go 1.26; existing `wfctl plugin` subcommand framework; reuses `wfctl plugin install --local` pipeline for capability verification; bash for the rename script shipped in the scaffold repo.

**Base branch:** main (per repo)

---

## Scope Manifest

**PR Count:** 6
**Tasks:** 6
**Estimated Lines of Change:** ~2000 across all PRs

**Out of scope:**
- Layer (a'') — bash deletion + Go `--fix` swap in workflow-registry. Lands as a separate PR AFTER the Layer (a') parity-cycle gates the swap. Tracked in workflow#762.
- Layer 3b sweep (54-repo `Layer (c)` ldflag bootstrap + `Layer (b)` canonical migration). Fans out via parallel sub-agents in a separate execution wave; tracked at workflow#760 (which is updated by Task 6 of this plan to drop the 2 template repos).
- Stale-repo audit (repos with no release in 90+ days) — filed as part of Layer 3b prep when that wave launches.
- SemVer 2.0.0 prerelease support — separate design (touches ParseSemver + wfctl install + registry).
- OCI catalog (`wfctl registry push/pull/login`) — unrelated subcommand family.
- Gap-repos (~8 plugins without release pipelines) — separate per-repo issues.
- Retro doc — filed after Layer (a'') + Layer 3b complete.

**PR Grouping:**

| PR # | Title | Tasks | Branch | Repo |
|------|-------|-------|--------|------|
| 1 | feat(wfctl): plugin registry-sync subcommand + core + readme sub-modes + shared semver regex (#762 Layer a) | Task 1 | feat/762-registry-sync | workflow |
| 2 | ci(registry): add wfctl plugin registry-sync dry-run alongside bash for parity cycle (#762 Layer a') | Task 2 | feat/762-registry-sync-parity | workflow-registry |
| 3 | feat(scaffold): rename workflow-plugin-template → scaffold-workflow-plugin + modernize content (#762 Layer d.1) | Task 3 | feat/762-scaffold-modernize | scaffold-workflow-plugin (post-rename) |
| 4 | feat(scaffold): rename workflow-plugin-template-private → scaffold-workflow-plugin-private + modernize content (#762 Layer d.2) | Task 4 | feat/762-scaffold-modernize | scaffold-workflow-plugin-private (post-rename) |
| 5 | chore(registry): delete plugins/template/ — superseded by scaffold-workflow-plugin (#762 Layer d.3) | Task 5 | chore/762-delete-template | workflow-registry |
| 6 | chore(issue): update workflow#760 sweep list — drop scaffold repos (#762 Layer d.4) | Task 6 | n/a (issue edit) | workflow (issue) |

**Status:** Draft

---

### Task 1: `wfctl plugin registry-sync` subcommand (workflow, single PR)

**Files in workflow repo:**
- Create: `cmd/wfctl/plugin_registry_sync.go` (root subcommand + default mode = port of sync-versions.sh)
- Create: `cmd/wfctl/plugin_registry_sync_core.go` (core mode = port of sync-core-manifests.sh)
- Create: `cmd/wfctl/plugin_registry_sync_readme.go` (readme mode = port of generate-readme.sh)
- Create: `cmd/wfctl/plugin_registry_sync_test.go` (table-driven tests for all 3 modes)
- Create: `cmd/wfctl/plugin_release_grade_semver.go` (shared regex constant)
- Create: `cmd/wfctl/testdata/plugin_registry_sync/{good,stale-version,stale-caps,non-semver-tag,empty-assets,fetch-plugin-json-missing,prerelease-vs-stable,...}/`
- Modify: `cmd/wfctl/plugin.go` (register subcommand)
- Modify: `cmd/wfctl/plugin_validate_contract.go` (replace local strict-semver regex with shared constant)
- Modify: `docs/PLUGIN_RELEASE_GATES.md` (document the new subcommand under "Registry sync")

**Step 1: Extract shared semver regex**

`cmd/wfctl/plugin_release_grade_semver.go`:

```go
package main

import "regexp"

// PublishGradeSemverRe matches strict release-grade semver tags (flat M.m.p,
// no prerelease, no build metadata). Engine ParseSemver requires this shape.
// Shared by:
//   - wfctl plugin validate-contract --for-publish (operator-side gate)
//   - wfctl plugin registry-sync (registry-side gate)
// workflow#762: single source-of-truth.
var PublishGradeSemverRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
```

In `cmd/wfctl/plugin_validate_contract.go`: delete local `publishGradeSemverRe` declaration; references use `PublishGradeSemverRe` from the new file.

**Step 2: Add subcommand dispatch**

Edit `cmd/wfctl/plugin.go`:

```go
case "validate-contract":
    return runPluginValidateContract(args[1:])
case "registry-sync":
    return runPluginRegistrySync(args[1:])
```

Update `pluginUsage()`: add `registry-sync` row.

**Step 3: Default mode (sync-versions.sh port)**

`cmd/wfctl/plugin_registry_sync.go`: implements `runPluginRegistrySync(args []string) error`. Flags: `--fix`, `--plugin <name>`, `--verify-capabilities`, `--registry-dir <path>`. Default `--registry-dir` is `.`.

Pseudo:

```go
func runPluginRegistrySync(args []string) error {
    fs := flag.NewFlagSet("plugin registry-sync", flag.ContinueOnError)
    fix := fs.Bool("fix", false, "Apply changes (default: dry-run)")
    pluginFilter := fs.String("plugin", "", "Restrict to single plugin directory name")
    verifyCaps := fs.Bool("verify-capabilities", false, "Spawn binary + diff capabilities (registry-side; slow)")
    registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
    if len(args) > 0 && (args[0] == "core" || args[0] == "readme") {
        switch args[0] {
        case "core":
            return runPluginRegistrySyncCore(args[1:])
        case "readme":
            return runPluginRegistrySyncReadme(args[1:])
        }
    }
    if err := fs.Parse(args); err != nil { return err }
    return syncDefault(*registryDir, *fix, *pluginFilter, *verifyCaps)
}
```

`syncDefault` walks `<registryDir>/plugins/*/manifest.json`. For each:
1. Parse `repository`/`source`; derive `gh_repo` via `normalizeRepo()` (port `sync-versions.sh:36-44`).
2. `gh release view --repo <gh_repo> --json tagName -q '.tagName'` → `latestTag`. Empty → SKIP.
3. **Strict-semver gate** (`PublishGradeSemverRe`). Non-match → `REJECT plugin_name — tag X is not release-grade semver`, continue.
4. `latestVersion = strings.TrimPrefix(latestTag, "v")`.
5. `downloadsMatchVersion(manifest, manifestVersion)` per `sync-versions.sh:46-58` — JQ filter ported to typed Go.
6. Compute `targetVersion` (manifest if downloads match + release exists; else latest).
7. If `--fix` AND (versions differ OR downloads stale): rewrite registry's `manifest.json` with new `.version` + `.downloads` (ports `sync-versions.sh:175-211`).
8. **Capability + minEngineVersion + iacProvider sync** via `fetch_plugin_json` port: `gh api repos/<gh_repo>/contents/plugin.json?ref=<tag> --jq '.content' | base64 -d`. Empty output → silent fallback (preserve current behavior per cycle 1 C2).
9. If `--verify-capabilities` (registry-side only): see Step 7 below.
10. Print summary (matching bash output format byte-for-byte for parity).

**Implementation detail (cycle 1 C2 fixture pins):** include fixtures for empty-assets short-circuit, fetch-plugin-json-missing silent-fallback, prerelease-vs-stable comparator (using `sort -V` semantics for parity; semver-correct deferred).

**Step 4: Core mode (sync-core-manifests.sh port)**

`cmd/wfctl/plugin_registry_sync_core.go`: implements `runPluginRegistrySyncCore(args []string) error`. Flags: `--fix`, `--workflow-repo <path>`, `--registry-dir <path>`.

Embeds the inspect program (currently in `sync-core-manifests.sh:39-89` as bash heredoc) via Go `embed.FS`. At runtime:

1. Resolve `<workflow-repo>` path; verify `go.mod` present.
2. Write embedded inspect.go to a tmpdir inside `<workflow-repo>`.
3. `cd <workflow-repo> && GOWORK=off go run ./<tmpdir>/inspect.go` → JSON of core plugins.
4. Cleanup tmpdir.
5. Parse JSON; for each core plugin, compare against registry's `plugins/<name>/manifest.json`; with `--fix` rewrite.
6. Output matches bash format for parity.

**Step 5: Readme mode (generate-readme.sh port)**

`cmd/wfctl/plugin_registry_sync_readme.go`: implements `runPluginRegistrySyncReadme(args []string) error`. Flags: `--check`, `--registry-dir <path>`.

Reads `plugins/*/manifest.json` + `templates/*/template.json` (whatever the bash reads); regenerates the plugin/template indexes in README.md between marker comments. `--check` is dry-run + exit non-zero on diff.

**Step 6: Tests (table-driven, fixture-backed)**

`cmd/wfctl/plugin_registry_sync_test.go`: per mode, table of fixtures + expected output. Critical fixtures:
- `good`: tag matches manifest; no-op.
- `stale-version`: manifest is older than latest tag; `--fix` rewrites.
- `stale-caps`: committed plugin.json at tag has newer caps than registry manifest; `--fix` syncs.
- `non-semver-tag`: REJECT line + skip.
- `empty-assets`: latest tag has no platform release assets; SKIP without rewriting.
- `fetch-plugin-json-missing`: `gh api contents/plugin.json` returns empty; silent fallback preserves existing caps.
- `prerelease-vs-stable`: `sort -V` semantics preserved (matches bash).

For `gh` API calls: use a test-injected interface or `httptest`-backed fake.

**Step 7: --verify-capabilities (reuses install pipeline per C3 fix)**

When `--verify-capabilities` set, for each plugin:

1. `gh release download <tag> --repo <gh_repo> --pattern '<plugin-name>-<os>-<arch>.tar.gz' -O /tmp/<plugin>-<tag>.tar.gz`
2. Extract to `/tmp/<plugin>-<tag>-extracted/`
3. Invoke existing `runPluginInstall` programmatically with `--local /tmp/<plugin>-<tag>-extracted/ --plugin-dir /tmp/<plugin>-<tag>-installed/` (avoids re-implementing rename + lockfile + integrity).
4. Spawn the installed plugin via existing `wfctl plugin info`-style code path; call `GetContractRegistry` RPC.
5. Diff RPC response vs `<registryDir>/plugins/<name>/manifest.json.capabilities`. If `--fix`, rewrite the registry manifest.
6. Cleanup temp dirs.

If existing wfctl APIs aren't exported in a way that supports invocation from `registry-sync`, **bail with a `TODO: refactor needed`** rather than reimplementing — file as a sub-task on the PR.

**Step 8: Update docs/PLUGIN_RELEASE_GATES.md**

Add a "Registry sync" section: documents `wfctl plugin registry-sync` (default + `core` + `readme` modes), `--verify-capabilities`, the parity-cycle migration plan, and links to workflow#762.

**Step 9: Verify**

```bash
cd /Users/jon/workspace/_worktrees/wf-762-design
GOWORK=off go build -o /tmp/wfctl-762 ./cmd/wfctl
GOWORK=off go test ./cmd/wfctl/ -run 'TestPluginRegistrySync|TestPluginValidateContract' -count=1 -race
# Smoke against an actual workflow-registry checkout (dry-run, no --fix):
/tmp/wfctl-762 plugin registry-sync --registry-dir /Users/jon/workspace/workflow-registry --plugin digitalocean
```
Expected: tests green; smoke OK matches bash output for the same plugin.

**Step 10: Commit + push + PR + monitor + admin-merge**

Standard pattern. Tag workflow `v0.62.0` after merge (Layer (a') depends on this tag).

---

### Task 2: workflow-registry parity cycle (workflow-registry, single PR)

**Files in workflow-registry:**
- Modify: `.github/workflows/sync-registry-manifests.yml`
- Create: `.github/workflows/scripts/parity-diff.sh` (compare bash vs Go outputs)

**Step 1: Add Go dry-run step alongside bash**

Edit `sync-registry-manifests.yml` to add (BEFORE the `--fix` bash step):

```yaml
- uses: GoCodeAlone/setup-wfctl@v1
  with:
    version: v0.62.0
- name: Registry-sync dry-run (Go, observation-only)
  run: |
    wfctl plugin registry-sync --registry-dir . > /tmp/go-sync-versions.txt
    WORKFLOW_REPO="$GITHUB_WORKSPACE/_workflow" wfctl plugin registry-sync core --registry-dir . --workflow-repo "$GITHUB_WORKSPACE/_workflow" > /tmp/go-sync-core.txt
    wfctl plugin registry-sync readme --registry-dir . --check > /tmp/go-sync-readme.txt || true
- name: Registry-sync dry-run (bash, observation-only — current authoritative)
  run: |
    scripts/sync-versions.sh > /tmp/bash-sync-versions.txt
    WORKFLOW_REPO="$GITHUB_WORKSPACE/_workflow" scripts/sync-core-manifests.sh > /tmp/bash-sync-core.txt
    scripts/generate-readme.sh --check > /tmp/bash-sync-readme.txt || true
- name: Compare bash vs Go parity
  run: |
    bash .github/workflows/scripts/parity-diff.sh /tmp/bash-sync-versions.txt /tmp/go-sync-versions.txt versions
    bash .github/workflows/scripts/parity-diff.sh /tmp/bash-sync-core.txt /tmp/go-sync-core.txt core
    bash .github/workflows/scripts/parity-diff.sh /tmp/bash-sync-readme.txt /tmp/go-sync-readme.txt readme
- name: Upload parity artifacts
  if: always()
  uses: actions/upload-artifact@v4
  with:
    name: parity-cycle-${{ github.run_id }}
    path: /tmp/*-sync-*.txt
```

The EXISTING `--fix` bash steps stay UNCHANGED. Bash remains authoritative; Go is observation-only. Parity-diff script fails the workflow on any non-zero diff.

**Step 2: Create parity-diff.sh**

`.github/workflows/scripts/parity-diff.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
bash_out="$1"
go_out="$2"
label="$3"

# Normalize: strip ANSI colors, trim trailing whitespace per line.
sed -E 's/\x1b\[[0-9;]*[mK]//g; s/[[:space:]]+$//' "$bash_out" | sort > "$bash_out.norm"
sed -E 's/\x1b\[[0-9;]*[mK]//g; s/[[:space:]]+$//' "$go_out" | sort > "$go_out.norm"

if ! diff -u "$bash_out.norm" "$go_out.norm"; then
  echo "::error::Parity diff for $label between bash + Go outputs. Bash remains authoritative; investigate Go port."
  exit 1
fi
echo "Parity OK for $label"
```

**Step 3: Local verify (limited; full sync needs gh auth)**

Local dry-run against a workflow-registry checkout (use existing bash for reference):

```bash
cd /Users/jon/workspace/workflow-registry
bash scripts/sync-versions.sh > /tmp/bash.txt
/tmp/wfctl-762 plugin registry-sync --registry-dir . > /tmp/go.txt
bash .github/workflows/scripts/parity-diff.sh /tmp/bash.txt /tmp/go.txt versions
```
Expected: parity OK or a small fixable diff.

**Step 4: Commit + push + PR + monitor + admin-merge**

```
gh pr create --title "ci(registry): add wfctl plugin registry-sync dry-run alongside bash (#762 Layer a')"
gh pr checks --watch
gh pr merge --squash --admin --delete-branch
```

**Rollback:** revert PR. Bash continues to be authoritative; Go dry-run + parity-diff removed.

---

### Task 3: scaffold-workflow-plugin rename + modernize (public scaffold)

**Pre-flight:** Layer (a) merged + workflow v0.62.0 tagged.

**Pre-step (org admin):**

```bash
gh repo rename workflow-plugin-template --repo GoCodeAlone/workflow-plugin-template scaffold-workflow-plugin
gh api -X PATCH /repos/GoCodeAlone/scaffold-workflow-plugin -f is_template=true
```

(Or use the GitHub UI: Settings → "Template repository" toggle.)

**Files in scaffold-workflow-plugin (post-rename):**
- Rename: `cmd/workflow-plugin-TEMPLATE/` → `cmd/scaffold-workflow-plugin/`
- Create: `cmd/scaffold-workflow-plugin-iac/main.go` (IaC variant)
- Create: `internal/version.go`
- Create: `scripts/rename-from-scaffold.sh`
- Create: `.github/workflows/scaffold-rename-test.yml`
- Modify: `cmd/scaffold-workflow-plugin/main.go` (non-IaC default)
- Modify: `plugin.json`
- Modify: `.goreleaser.yaml`
- Modify: `.github/workflows/release.yml`
- Delete: `.github/workflows/sync-plugin-version.yml`
- Modify: `README.md`
- Modify: `go.mod` (module path)

**Step 1: Worktree + branch**

```bash
cd /Users/jon/workspace
gh repo clone GoCodeAlone/scaffold-workflow-plugin
cd scaffold-workflow-plugin
git checkout -b feat/762-scaffold-modernize
```

**Step 2: Rename cmd dir + go.mod module path**

```bash
git mv cmd/workflow-plugin-TEMPLATE cmd/scaffold-workflow-plugin
go mod edit -module github.com/GoCodeAlone/scaffold-workflow-plugin
# Update imports across all .go files
find . -name '*.go' -not -path './vendor/*' -not -path './_worktrees/*' \
  -exec sed -i.bak 's|workflow-plugin-template|scaffold-workflow-plugin|g' {} \;
find . -name '*.go.bak' -delete
```

**Step 3: Create non-IaC main.go**

`cmd/scaffold-workflow-plugin/main.go`:

```go
// Command scaffold-workflow-plugin is the non-IaC variant of the
// workflow-plugin scaffold. Instantiators copy this main.go to
// cmd/workflow-plugin-<their-name>/main.go via scripts/rename-from-scaffold.sh.
package main

import (
    "github.com/GoCodeAlone/scaffold-workflow-plugin/internal"
    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

//go:embed plugin.json
var manifestJSON []byte
var manifest = sdk.MustEmbedManifest(manifestJSON)

func main() {
    sdk.Serve(internal.NewPlugin(),
        sdk.WithManifestProvider(manifest),
        sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
    )
}
```

**Step 4: Create IaC main.go**

`cmd/scaffold-workflow-plugin-iac/main.go`:

```go
// Command scaffold-workflow-plugin-iac is the IaC variant of the
// workflow-plugin scaffold. Instantiators using rename-from-scaffold.sh
// --mode iac copy this main.go to cmd/workflow-plugin-<their-name>/main.go.
// The non-IaC main.go in cmd/scaffold-workflow-plugin/ is removed by the
// rename script when --mode iac is selected.
package main

import (
    "github.com/GoCodeAlone/scaffold-workflow-plugin/internal"
    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
    sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
        BuildVersion: sdk.ResolveBuildVersion(internal.Version),
    })
}
```

`internal/NewIaCServer()` returns a stub `IaCProviderRequiredServer` implementation with all methods returning `codes.Unimplemented`. Instantiator replaces with their real implementation.

**Step 5: internal/version.go**

```go
package internal

// Version is set at build time via -ldflags
// "-X github.com/<...>/internal.Version=X.Y.Z".
// Mirrors the workflow#758 plugin contract.
var Version = "dev"
```

**Step 6: plugin.json**

```json
{
  "name": "scaffold-workflow-plugin",
  "version": "0.0.0",
  "description": "Template scaffold for new workflow plugins. NOT an installable plugin. See README.",
  "author": "GoCodeAlone",
  "license": "MIT",
  "type": "scaffold",
  "minEngineVersion": "0.61.0",
  "capabilities": {
    "moduleTypes": ["TEMPLATE.module"],
    "stepTypes": ["TEMPLATE.step"],
    "triggerTypes": [],
    "iacProvider": { "resourceTypes": ["TEMPLATE.resource"] }
  }
}
```

Note: `type: scaffold` (new value; registry-side allowlist defense in Task 1's `wfctl plugin registry-sync` rejects this type so accidental re-registration fails fast).

**Step 7: .goreleaser.yaml**

Add `ldflags` block to existing `builds`:

```yaml
builds:
  - id: scaffold-workflow-plugin
    main: ./cmd/scaffold-workflow-plugin
    binary: scaffold-workflow-plugin
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/GoCodeAlone/scaffold-workflow-plugin/internal.Version={{.Version}}
```

(Keep existing `before:` hook for plugin.json version-rewrite; goreleaser's standard pattern.)

**Step 8: .github/workflows/release.yml**

Replace with workflow#758 canonical pattern:

```yaml
name: Release
on: { push: { tags: ['v*'] } }
permissions: { contents: write, id-token: write }
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: GoCodeAlone/setup-wfctl@v1
        with: { version: v0.62.0 }
      - name: Validate plugin contract for publish (pre-build)
        run: wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" .
      - uses: goreleaser/goreleaser-action@v7
        with: { distribution: goreleaser, version: '~> v2', args: release --clean }
        env: { GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}" }
      - name: Verify shipped plugin.json carries tag (post-build)
        run: |
          if [ -f .release/plugin.json ]; then
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir .release .
          else
            wfctl plugin validate-contract --for-publish --tag "${{ github.ref_name }}" --release-dir . .
          fi
      - name: Publish release
        env: { GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}" }
        run: gh release edit ${{ github.ref_name }} --draft=false --repo ${{ github.repository }}
```

**Step 9: Delete sync-plugin-version.yml**

```bash
git rm .github/workflows/sync-plugin-version.yml
```

**Step 10: scripts/rename-from-scaffold.sh (TESTED)**

```bash
#!/usr/bin/env bash
# Usage: bash scripts/rename-from-scaffold.sh <your-plugin-name> [--mode iac|non-iac]
# Renames scaffold-workflow-plugin internals to workflow-plugin-<your-plugin-name>.
# Deletes the unused main.go variant. Updates go.mod, plugin.json, .goreleaser.yaml.
set -euo pipefail

NEW_NAME="${1:?Usage: rename-from-scaffold.sh <name> [--mode iac|non-iac]}"
MODE="non-iac"
if [[ "${2:-}" == "--mode" ]]; then
  MODE="${3:?Mode required}"
fi
case "$MODE" in iac|non-iac) ;; *) echo "Mode must be iac or non-iac" >&2; exit 1 ;; esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# 1. Pick main.go variant; delete the other.
if [[ "$MODE" == "iac" ]]; then
  rm -rf cmd/scaffold-workflow-plugin
  mv cmd/scaffold-workflow-plugin-iac "cmd/workflow-plugin-$NEW_NAME"
else
  rm -rf cmd/scaffold-workflow-plugin-iac
  mv cmd/scaffold-workflow-plugin "cmd/workflow-plugin-$NEW_NAME"
fi

# 2. go.mod
go mod edit -module "github.com/GoCodeAlone/workflow-plugin-$NEW_NAME"

# 3. Bounded sed across known file globs.
for f in plugin.json .goreleaser.yaml README.md cmd/**/*.go internal/**/*.go; do
  [[ -f "$f" ]] || continue
  sed -i.bak "s|scaffold-workflow-plugin|workflow-plugin-$NEW_NAME|g" "$f"
  rm -f "$f.bak"
done

# 4. plugin.json: reset type from "scaffold" to "external".
python3 -c "
import json
p = json.load(open('plugin.json'))
p['type'] = 'external'
p['name'] = 'workflow-plugin-$NEW_NAME'
json.dump(p, open('plugin.json', 'w'), indent=2)
open('plugin.json', 'a').write('\n')
"

# 5. Remove the rename script itself (instantiators don't need it).
rm scripts/rename-from-scaffold.sh

# 6. Remove the scaffold-rename-test workflow.
rm .github/workflows/scaffold-rename-test.yml

echo "Renamed to workflow-plugin-$NEW_NAME ($MODE mode). Review changes, edit capabilities in plugin.json, then commit + tag."
```

**Step 11: .github/workflows/scaffold-rename-test.yml**

```yaml
name: Scaffold rename test
on: [push, pull_request]
jobs:
  rename-non-iac:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: Rename to test-plugin (non-iac) + build
        run: |
          cp -r . /tmp/scaffold-copy
          cd /tmp/scaffold-copy
          bash scripts/rename-from-scaffold.sh test-plugin --mode non-iac
          go build ./...
  rename-iac:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: Rename to test-plugin (iac) + build
        run: |
          cp -r . /tmp/scaffold-copy
          cd /tmp/scaffold-copy
          bash scripts/rename-from-scaffold.sh test-plugin --mode iac
          go build ./...
```

**Step 12: README.md**

Rewrite as scaffold documentation:

```markdown
# scaffold-workflow-plugin

This is a SCAFFOLD repo. It is NOT an installable plugin. Use it to
create a new workflow plugin via GitHub's "Use this template" button OR
via `wfctl plugin init --from-scaffold scaffold-workflow-plugin`.

## After creating your new repo

1. **Enable GitHub Actions**: Settings → Actions → "I understand my
   workflows, enable them" (required for releases — templates ship
   with workflows disabled by default).
2. **Run the rename script**:
   ```
   bash scripts/rename-from-scaffold.sh <your-plugin-name> --mode [iac|non-iac]
   ```
   This renames cmd/, go.mod, plugin.json, README.md; deletes the
   unused main.go variant; sets `plugin.json.type` from `scaffold` to
   `external`.
3. **Edit `plugin.json`**: replace placeholder capabilities/minEngineVersion
   with your plugin's actual values.
4. **Commit + tag**:
   ```
   git add . && git commit -m "feat: initial plugin scaffold from scaffold-workflow-plugin"
   git tag v0.1.0 && git push origin main v0.1.0
   ```
   release.yml's `wfctl plugin validate-contract --for-publish` gate
   will verify your tag + contract.

## Modes

- `--mode non-iac` (default): for module/step/trigger plugins that use
  `sdk.Serve`. Suitable for most plugins.
- `--mode iac`: for IaC provider plugins that use `sdk.ServeIaCPlugin` +
  satisfy `pb.IaCProviderRequiredServer`. Use only if your plugin
  provisions infrastructure (cloud resources, etc.).
```

**Step 13: Verify locally**

```bash
GOWORK=off go build ./...
GOWORK=off go test ./... -count=1
bash scripts/rename-from-scaffold.sh test-plugin --mode non-iac
# Verify renamed repo builds:
(cd /tmp && rm -rf scaffold-copy && cp -r /Users/jon/workspace/scaffold-workflow-plugin scaffold-copy && cd scaffold-copy && bash scripts/rename-from-scaffold.sh test-plugin --mode non-iac && go build ./...)
```

**Step 14: Commit + push + PR + monitor + admin-merge**

Multi-commit allowed; squash on merge. CI runs scaffold-rename-test workflow which guards C5 regressions.

**Rollback:** revert PR + `gh repo rename` back to `workflow-plugin-template`.

---

### Task 4: scaffold-workflow-plugin-private rename + modernize

**Identical structure to Task 3**, against `workflow-plugin-template-private` → `scaffold-workflow-plugin-private`. Differences:

- `plugin.json.name` = `scaffold-workflow-plugin-private`
- All module paths reference `scaffold-workflow-plugin-private`
- README.md opens with I7 clarification: "This repo's `-private` suffix refers to its GitHub repo visibility (only org members can clone). It is NOT related to `plugin.json.private: true` semantics."
- `.goreleaser.yaml` references `RELEASES_TOKEN` for private go-module access (verify the existing template already has this; if not, add).

Same scaffold-rename-test workflow + same release.yml pattern.

Pre-flight admin:

```bash
gh repo rename workflow-plugin-template-private --repo GoCodeAlone/workflow-plugin-template-private scaffold-workflow-plugin-private
gh api -X PATCH /repos/GoCodeAlone/scaffold-workflow-plugin-private -f is_template=true
```

**Verify + Commit + push + PR + monitor + admin-merge** same as Task 3.

**Rollback:** same as Task 3.

---

### Task 5: workflow-registry delete plugins/template/

**Files in workflow-registry:**
- Delete: `plugins/template/manifest.json`
- Delete: `plugins/template/` directory

**Step 1: Branch + delete**

```bash
cd /Users/jon/workspace/workflow-registry
git fetch origin main
git worktree add /Users/jon/workspace/_worktrees/wfreg-762-template-delete -b chore/762-delete-template origin/main
cd /Users/jon/workspace/_worktrees/wfreg-762-template-delete
git rm -r plugins/template/
```

**Step 2: Regenerate README index**

```bash
# After Task 1 lands + wfctl v0.62.0 available:
wfctl plugin registry-sync readme --registry-dir . --fix
# Or fall back to bash if wfctl not yet released:
bash scripts/generate-readme.sh
```

**Step 3: Verify**

```bash
bash scripts/validate-manifests.sh
```
Expected: all remaining manifests valid; no broken refs to deleted `template` plugin.

**Step 4: Commit + push + PR + monitor + admin-merge**

PR body:

```
Deletes plugins/template/ — the entry was a stub manifest pointing at
workflow-plugin-template (since renamed to scaffold-workflow-plugin per
workflow#762 Layer d). Scaffold repos are NOT installable plugins; this
entry should never have been registered.

**Breaking change for operators with `template` in `.wfctl-lock.yaml`**:
those operators must remove the entry. The plugin was non-functional
(empty stub); no real installs affected.

Refs workflow#762
```

**Rollback:** `git revert`; restores `plugins/template/manifest.json` and registry once again exposes the scaffold as a plugin (regression).

---

### Task 6: Update workflow#760 sweep list

**Files:** none (issue edit only).

**Step 1: Edit issue body via gh CLI**

```bash
gh issue edit 760 --repo GoCodeAlone/workflow --body "$(gh issue view 760 --repo GoCodeAlone/workflow --json body -q .body | sed '/workflow-plugin-template$/d; /workflow-plugin-template-private$/d')"
```

(Or via UI: remove `workflow-plugin-template` + `workflow-plugin-template-private` from the enumerated 56-repo list; update the "Remaining repos to migrate" count from 56 to 54.)

**Step 2: Append comment**

```bash
gh issue comment 760 --repo GoCodeAlone/workflow --body "Updated per workflow#762 Layer d: dropped scaffold-workflow-plugin (renamed from workflow-plugin-template) + scaffold-workflow-plugin-private from the sweep. These are now scaffold repos, not plugins. 56 → 54."
```

**Verify:** `gh issue view 760 --repo GoCodeAlone/workflow` shows updated body + comment.

---

## Pipeline gate at end of plan

This plan ships Layer (a) + (a') + (d). Layer (a'') (bash deletion + Go --fix swap) and Layer 3b sweep are explicit follow-up work — Layer (a'') waits on one parity-cycle observation; Layer 3b is filed at workflow#760 and gets its own execution wave with separate authorization.
