---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 31f2154
  - repo: workflow
    commit: c114e1f
  - repo: workflow
    commit: 6942e83
  - repo: workflow
    commit: d369b77
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"isHelpRequested|checkTrailingFlags|plugin-dir|data-dir|PluginManifest|include:\" cmd/wfctl mcp plugins schema .github"
    - "git log --oneline --all -- cmd/wfctl/main.go cmd/wfctl/plugin_install.go cmd/wfctl/flag_helpers.go cmd/wfctl/validate.go"
  result: pass
supersedes: []
superseded_by: []
---

# Design: wfctl CLI Audit & Plugin Ecosystem Improvements

**Date:** 2026-03-12
**Status:** Approved
**Scope:** wfctl CLI fixes, workflow-registry data fixes, plugin ecosystem standardization

## Summary

Comprehensive audit and fix of the wfctl CLI tool addressing 13 bugs/UX issues found during testing, 5 registry data problems, and establishing a holistic plugin ecosystem plan across workflow-registry and all workflow-plugin-* repos. Addresses workflow PRs #321, #322, and issue #316.

## Motivation

- `--help` exits 1 with internal engine errors for all pipeline-dispatched commands
- `plugin install -data-dir` flag is silently ignored — plugins install to wrong directory
- Flags after positional args are silently dropped with no helpful error
- Full plugin names (`workflow-plugin-authz`) don't resolve in registry lookups
- Plugin manifest versions don't match release tags
- No way to install plugins from GitHub URLs directly
- No plugin version pinning or lockfile support
- Inconsistent goreleaser configs across plugin repos produce different tarball layouts

## A. wfctl CLI Fixes

### Fix 1: `--help` exits 0 and suppresses engine error

Root cause: `flag.ErrHelp` propagates through the pipeline engine as a step failure. In `main.go`'s command dispatch, catch `flag.ErrHelp` and `os.Exit(0)` instead of letting it reach the engine error wrapper. Same for the no-args case — show usage and exit 0.

### Fix 2: `plugin install -plugin-dir` honored

In `plugin_install.go`, the install logic hardcodes `data/plugins`. Thread the `-plugin-dir` flag value through to the download/extract path. Rename the flag from `-data-dir` to `-plugin-dir` (see Fix 13).

### Fix 3: Flag ordering — helpful error message

Go's `flag.FlagSet.Parse` stops at first non-flag arg. Detect unused flags after the positional arg and print: `"error: flags must come before arguments (got -foo after 'bar'). Try: wfctl plugin init -author X bar"`. Add a helper `checkTrailingFlags(args)` used by all subcommands that accept both flags and positional args.

### Fix 4: Full plugin name resolution

In the plugin install/search path, strip the `workflow-plugin-` prefix when looking up. So `workflow-plugin-authz` → `authz`. Try the raw name first, then the stripped name.

### Fix 5: Positional config arg consistency

Commands like `validate`, `inspect`, `api extract` accept positional config args. `deploy kubernetes generate` and other deploy subcommands don't. Add positional arg support to deploy subcommands using the same pattern as validate.

### Fix 6: `plugin update` version check

Before downloading, compare installed `plugin.json` version against registry manifest version. If equal, print "already at latest version (vX.Y.Z)" and skip the download.

### Fix 7: `init` generates valid Dockerfile

Generate a Dockerfile that handles missing `go.sum` gracefully: use `COPY go.sum* ./` (glob, no error if missing) followed by `RUN go mod download` which handles both cases.

### Fix 8: Infra commands — better error

When no config found, print: `"No infrastructure config found. Create infra.yaml with cloud.account and platform.* modules. Run 'wfctl init --template infra' for a starter config."`.

### Fix 9: `validate --dir` skips non-workflow YAML

Before validating a file found by directory scan, check for at least one of `modules:`, `workflows:`, or `pipelines:` as top-level keys. Skip files that don't match with a debug-level message: `"Skipping non-workflow file: .github/workflows/ci.yml"`.

### Fix 10: `plugin info` absolute paths

Resolve the binary path to absolute before displaying.

### Fix 11: PR #322 — `PluginManifest` legacy capabilities

Add `UnmarshalJSON` on `PluginManifest` that handles `capabilities` as either `[]CapabilityDecl` (new array format) or `{configProvider, moduleTypes, stepTypes, triggerTypes}` (legacy object format), merging the object's type lists into the top-level manifest fields.

### Fix 12: Validation follows YAML includes

When a config uses `include:` directives to split config across multiple files, `validate` should recursively resolve and validate the referenced files. Parse the root config, find `include` references, resolve relative paths, and validate each included file in context.

### Fix 13: Rename `-data-dir` to `-plugin-dir`

Several commands already use `-plugin-dir` (validate, template validate, mcp, docs generate, modernize). The plugin subcommands (`install`, `list`, `info`, `update`, `remove`) use `-data-dir`. Rename all plugin subcommand flags to `-plugin-dir` for consistency. Keep `-data-dir` as a hidden alias that still works but prints a deprecation notice.

## B. Registry Data Fixes

### B1. `agent` manifest type

Change `type: "internal"` → `type: "builtin"` in `plugins/agent/manifest.json`. The agent plugin ships with the engine as a Go library.

### B2. `ratchet` manifest downloads

Add `downloads` entries for linux/darwin x amd64/arm64 pointing to `GoCodeAlone/ratchet` GitHub releases. Currently `downloads: []` which causes validation failure.

### B3. `authz` manifest name resolution

Verify the manifest exists at `plugins/authz/manifest.json` and that the `name` field matches what wfctl expects. The PR #321 failure (`"workflow-plugin-authz" not found in registry`) suggests a name mismatch.

### B4. Version alignment script

Create `scripts/sync-versions.sh` that queries `gh release view --json tagName` for each external plugin with a `repository` field and compares against the manifest `version`. Report mismatches. Run in CI as a weekly check.

### B5. Schema validation gap

The `agent` manifest with `type: "internal"` should have been caught by the JSON Schema validation in CI. Either the schema enum needs updating to match, or CI isn't running properly. Investigate and fix.

## C. Plugin Ecosystem Plan

### C1. goreleaser standardization

Create a reference `.goreleaser.yml` and audit all plugin repos:

```yaml
# Standard plugin goreleaser config
builds:
  - binary: "{{ .ProjectName }}"
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    env: [CGO_ENABLED=0]
    ldflags: ["-s", "-w", "-X main.version={{ .Version }}"]

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
    files:
      - plugin.json

# Template version in plugin.json before build
before:
  hooks:
    - cmd: sed -i'' -e 's/"version":.*/"version": "{{ .Version }}",/' plugin.json
```

Repos to audit and fix:
- workflow-plugin-authz, payments, admin, bento, github, agent
- workflow-plugin-waf, security, sandbox, supply-chain, data-protection
- workflow-plugin-authz-ui, cloud-ui

### C2. `wfctl plugin install` from GitHub URL

Support `wfctl plugin install GoCodeAlone/workflow-plugin-authz@v0.3.1`:

1. First try registry lookup (strip `workflow-plugin-` prefix)
2. If not found and input contains `/`, treat as `owner/repo@version`
3. Query GitHub Releases API: `GET /repos/{owner}/{repo}/releases/tags/{version}`
4. Find asset matching `{repo}_{os}_{arch}.tar.gz`
5. Download, extract, install to `-plugin-dir`

Falls back gracefully: registry hit → GitHub direct → error with helpful message.

### C3. Plugin lockfile (`.wfctl.yaml` plugins section)

Extend `.wfctl.yaml` (already created by `git connect`) with a `plugins:` section:

```yaml
plugins:
  authz:
    version: v0.3.1
    repository: GoCodeAlone/workflow-plugin-authz
    sha256: abc123...
  payments:
    version: v0.1.0
    repository: GoCodeAlone/workflow-plugin-payments
```

- `wfctl plugin install` (no args): reads lockfile, installs/verifies all entries
- `wfctl plugin install <name>@<version>`: installs and updates lockfile entry
- `wfctl plugin install --save <name>`: shorthand to install latest and pin

### C4. Engine `minEngineVersion` check

In the engine's `PluginLoader`, after reading `plugin.json`, compare `minEngineVersion` against the running engine version using semver comparison. If incompatible, log a warning: `"plugin X requires engine >= vY.Z.0, running vA.B.C — may cause runtime failures"`. Don't hard-fail to allow testing.

### C5. Registry manifest auto-sync CI

Add a GitHub Action to each plugin repo's release workflow:

```yaml
# After goreleaser publishes:
- name: Update registry manifest
  run: |
    # Clone workflow-registry, update manifest version + downloads + checksums
    # Open PR to workflow-registry
```

This eliminates manual version drift between releases and registry manifests.

## Testing Strategy

- Build wfctl, run all commands in `/tmp/wfctl-test` directory
- Test each fix against the specific failure scenario from the audit
- `go test ./cmd/wfctl/...` for unit tests
- `go test ./...` for full suite
- Manual verification of plugin install/update/list lifecycle
- Registry manifest validation via `scripts/validate-manifests.sh`

## Decisions

- **`-plugin-dir` over `-data-dir`**: Consistency with existing commands. Hidden alias prevents breakage.
- **Registry name stripping over aliasing**: Simpler than maintaining alias maps. `workflow-plugin-authz` → `authz` covers all cases.
- **Warning over hard-fail for minEngineVersion**: Allows testing newer plugins against older engines without blocking.
- **Lockfile in `.wfctl.yaml` over separate file**: Reuses existing config file, keeps project root clean.
- **goreleaser `sed` hook over Go ldflags for version**: `plugin.json` is a static file that needs the version at build time, not just the binary.
