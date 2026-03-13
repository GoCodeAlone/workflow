# wfctl Audit & Plugin Ecosystem Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 13 wfctl CLI bugs/UX issues, correct registry data, and establish a standardized plugin ecosystem across all repos.

**Architecture:** Three phases — (A) CLI fixes in `workflow/cmd/wfctl/`, (B) registry data fixes in `workflow-registry/`, (C) plugin ecosystem improvements across repos. All Go changes include tests. Registry changes validated by existing CI.

**Tech Stack:** Go 1.26, flag package, YAML/JSON parsing, GitHub Releases API, goreleaser

---

## Phase A: wfctl CLI Fixes

### Task 1: Fix `--help` exit code and engine error leakage

**Files:**
- Modify: `cmd/wfctl/main.go:108-127`
- Test: `cmd/wfctl/main_test.go` (create if needed)

**Step 1: Write the failing test**

```go
// cmd/wfctl/main_test.go
package main

import (
	"strings"
	"testing"
)

func TestHelpFlagDoesNotLeakEngineError(t *testing.T) {
	// The dispatch error for --help should mention "help" but NOT
	// "workflow execution failed" or "pipeline.*failed".
	// We test the error wrapping logic, not the full engine dispatch.
	err := fmt.Errorf("flag: help requested")
	if !isHelpRequested(err) {
		t.Error("expected isHelpRequested to detect flag.ErrHelp message")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestHelpFlag -v`
Expected: FAIL — `isHelpRequested` undefined

**Step 3: Implement the fix**

In `main.go`, add a helper and modify the dispatch error handling:

```go
// Add after the commands map
func isHelpRequested(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "flag: help requested")
}
```

Then modify the `main()` function's error handling (around line 118):

```go
	// Replace current no-args handling:
	if len(os.Args) < 2 {
		_ = cliHandler.Dispatch([]string{"-h"})
		os.Exit(0) // was os.Exit(1) — help is not an error
	}

	// ...existing code...

	dispatchErr := cliHandler.DispatchContext(ctx, os.Args[1:])
	stop()

	if dispatchErr != nil {
		if isHelpRequested(dispatchErr) {
			os.Exit(0)
		}
		if _, isKnown := commands[cmd]; isKnown {
			fmt.Fprintf(os.Stderr, "error: %v\n", dispatchErr)
		}
		os.Exit(1)
	}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestHelpFlag -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -v -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add cmd/wfctl/main.go cmd/wfctl/main_test.go
git commit -m "fix(wfctl): --help exits 0 and suppresses engine error leakage"
```

---

### Task 2: Rename `-data-dir` to `-plugin-dir` across plugin subcommands

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (lines 69, 171, 223, 248, 275)
- Modify: `cmd/wfctl/plugin.go` (usage text)
- Test: `cmd/wfctl/plugin_install_test.go` (create)

**Step 1: Write the failing test**

```go
// cmd/wfctl/plugin_install_test.go
package main

import (
	"testing"
)

func TestPluginListAcceptsPluginDirFlag(t *testing.T) {
	// Ensure -plugin-dir flag is accepted (not just -data-dir)
	err := runPluginList([]string{"-plugin-dir", t.TempDir()})
	if err != nil {
		// If it contains "No plugins installed" that's fine — we just want no flag parse error
		if !strings.Contains(err.Error(), "No plugins") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/wfctl/ -run TestPluginListAcceptsPluginDir -v`
Expected: FAIL — flag provided but not defined: -plugin-dir

**Step 3: Implement the rename**

In `plugin_install.go`, for each function (`runPluginInstall`, `runPluginList`, `runPluginUpdate`, `runPluginRemove`, `runPluginInfo`):

1. Change: `fs.String("data-dir", defaultDataDir, "Plugin data directory")`
   To: `fs.String("plugin-dir", defaultDataDir, "Plugin directory")`

2. After each `fs.String("plugin-dir", ...)`, add the hidden alias:
```go
	pluginDir := fs.String("plugin-dir", defaultDataDir, "Plugin directory")
	// Hidden alias for backwards compatibility
	fs.String("data-dir", "", "")
	// After parse, if plugin-dir is default but data-dir was set, use data-dir
```

Actually, simpler approach — register both flags pointing to the same variable:

```go
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
```

Apply this pattern to all 5 functions: `runPluginInstall`, `runPluginList`, `runPluginUpdate`, `runPluginRemove`, `runPluginInfo`.

Also update `pluginUsage()` in `plugin.go` to show `-plugin-dir` in the help text.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/wfctl/ -run TestPluginListAcceptsPluginDir -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./cmd/wfctl/ -v -count=1`

**Step 6: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin.go cmd/wfctl/plugin_install_test.go
git commit -m "fix(wfctl): rename -data-dir to -plugin-dir for consistency

Keep -data-dir as hidden alias for backwards compatibility."
```

---

### Task 3: Add trailing flag detection helper

**Files:**
- Modify: `cmd/wfctl/validate.go` (it already has `reorderFlags` — check its implementation)
- Create: `cmd/wfctl/flag_helpers.go`
- Create: `cmd/wfctl/flag_helpers_test.go`

**Step 1: Check existing `reorderFlags` in validate.go**

Read `validate.go` and understand what `reorderFlags` does. If it already reorders flags before positional args, we may be able to reuse it. The approach: detect flags that appear after the first positional arg and print a helpful error.

**Step 2: Write the failing test**

```go
// cmd/wfctl/flag_helpers_test.go
package main

import "testing"

func TestCheckTrailingFlags(t *testing.T) {
	tests := []struct {
		args     []string
		wantErr  bool
	}{
		{[]string{"-author", "X", "myname"}, false},         // flags before positional — OK
		{[]string{"myname", "-author", "X"}, true},           // flags after positional — error
		{[]string{"-author", "X", "-output", ".", "myname"}, false}, // all flags before — OK
		{[]string{"myname"}, false},                          // no flags at all — OK
	}
	for _, tt := range tests {
		err := checkTrailingFlags(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkTrailingFlags(%v) error=%v, wantErr=%v", tt.args, err, tt.wantErr)
		}
	}
}
```

**Step 3: Implement**

```go
// cmd/wfctl/flag_helpers.go
package main

import (
	"fmt"
	"strings"
)

// checkTrailingFlags detects flags that appear after the first positional argument
// and returns a helpful error message suggesting the correct ordering.
func checkTrailingFlags(args []string) error {
	seenPositional := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && seenPositional {
			return fmt.Errorf("flags must come before arguments (got %s after positional arg). "+
				"Reorder so all flags precede the name argument", arg)
		}
		if !strings.HasPrefix(arg, "-") {
			seenPositional = true
		}
	}
	return nil
}
```

**Step 4: Wire into subcommands**

Add `checkTrailingFlags(args)` call at the top of: `runPluginInit`, `runRegistryAdd`, `runRegistryRemove`. Example for `runPluginInit`:

```go
func runPluginInit(args []string) error {
	if err := checkTrailingFlags(args); err != nil {
		return err
	}
	// ... existing code
```

**Step 5: Run tests**

Run: `go test ./cmd/wfctl/ -run TestCheckTrailingFlags -v`
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/wfctl/flag_helpers.go cmd/wfctl/flag_helpers_test.go cmd/wfctl/plugin.go cmd/wfctl/registry_cmd.go
git commit -m "fix(wfctl): detect trailing flags and show helpful error message"
```

---

### Task 4: Full plugin name resolution (strip `workflow-plugin-` prefix)

**Files:**
- Modify: `cmd/wfctl/multi_registry.go:45-58`
- Test: `cmd/wfctl/multi_registry_test.go` (create or extend)

**Step 1: Write the failing test**

```go
// cmd/wfctl/multi_registry_test.go (add to existing if present)
package main

import "testing"

func TestNormalizePluginName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"authz", "authz"},
		{"workflow-plugin-authz", "authz"},
		{"workflow-plugin-payments", "payments"},
		{"custom-plugin", "custom-plugin"}, // no prefix, keep as-is
	}
	for _, tt := range tests {
		got := normalizePluginName(tt.input)
		if got != tt.want {
			t.Errorf("normalizePluginName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

**Step 2: Implement**

In `multi_registry.go`, add:

```go
import "strings"

// normalizePluginName strips the "workflow-plugin-" prefix if present,
// since the registry uses short names (e.g. "authz" not "workflow-plugin-authz").
func normalizePluginName(name string) string {
	return strings.TrimPrefix(name, "workflow-plugin-")
}
```

Update `FetchManifest`:

```go
func (m *MultiRegistry) FetchManifest(name string) (*RegistryManifest, string, error) {
	normalized := normalizePluginName(name)
	var lastErr error
	// Try normalized name first
	for _, src := range m.sources {
		manifest, err := src.FetchManifest(normalized)
		if err == nil {
			return manifest, src.Name(), nil
		}
		lastErr = err
	}
	// If normalized != original, also try the original name
	if normalized != name {
		for _, src := range m.sources {
			manifest, err := src.FetchManifest(name)
			if err == nil {
				return manifest, src.Name(), nil
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("plugin %q not found in any configured registry", name)
}
```

Also update `SearchPlugins` to normalize the query.

**Step 3: Run tests**

Run: `go test ./cmd/wfctl/ -run TestNormalizePluginName -v`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/wfctl/multi_registry.go cmd/wfctl/multi_registry_test.go
git commit -m "fix(wfctl): resolve full plugin names by stripping workflow-plugin- prefix"
```

---

### Task 5: Fix `plugin install -plugin-dir` being ignored

**Files:**
- Modify: `cmd/wfctl/plugin_install.go:122`

**Context:** Line 122 uses `*dataDir` (now `pluginDirVal`) correctly after Task 2's rename, but the bug is that `plugin update` calls `runPluginInstall` with `--data-dir` hardcoded at line 243. Fix this call to use `--plugin-dir`.

**Step 1: Write the failing test**

```go
func TestPluginInstallRespectsPluginDir(t *testing.T) {
	customDir := t.TempDir()
	// runPluginInstall with a non-existent plugin will fail at manifest fetch,
	// but we can verify the destDir is constructed correctly by checking the
	// MkdirAll path in the error when network is unavailable.
	err := runPluginInstall([]string{"-plugin-dir", customDir, "nonexistent-test-plugin"})
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
	// The error should NOT mention "data/plugins" (the default dir)
	if strings.Contains(err.Error(), "data/plugins") {
		t.Errorf("plugin install ignored -plugin-dir flag, error references default path: %v", err)
	}
}
```

**Step 2: Verify line 243 in runPluginUpdate**

Change line 243 from:
```go
return runPluginInstall(append([]string{"--data-dir", *dataDir}, pluginName))
```
to:
```go
return runPluginInstall(append([]string{"-plugin-dir", pluginDirVal}, pluginName))
```

(After Task 2, `*dataDir` becomes `pluginDirVal`.)

**Step 3: Run tests**

Run: `go test ./cmd/wfctl/ -run TestPluginInstallRespectsPluginDir -v`

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "fix(wfctl): plugin install respects -plugin-dir flag"
```

---

### Task 6: `plugin update` version check

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (runPluginUpdate function, around line 223)

**Step 1: Write the failing test**

```go
func TestReadInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"test","version":"1.2.3"}`
	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0644)
	ver := readInstalledVersion(dir)
	if ver != "1.2.3" {
		t.Errorf("readInstalledVersion = %q, want %q", ver, "1.2.3")
	}
}
```

**Step 2: Implement version check in runPluginUpdate**

After fetching the manifest but before downloading, compare versions:

```go
	// In runPluginUpdate, after mr.FetchManifest:
	installedVer := readInstalledVersion(pluginDir)
	if installedVer != "" && installedVer == manifest.Version {
		fmt.Printf("%s is already at latest version (v%s)\n", pluginName, installedVer)
		return nil
	}
	if installedVer != "" {
		fmt.Fprintf(os.Stderr, "Updating %s from v%s to v%s...\n", pluginName, installedVer, manifest.Version)
	}
```

**Step 3: Run tests and commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "fix(wfctl): plugin update checks version before re-downloading"
```

---

### Task 7: Deploy subcommands accept positional config arg

**Files:**
- Modify: `cmd/wfctl/deploy.go` (kubernetes generate and other subcommands)

**Step 1: Identify the pattern**

In `validate.go`, positional args work because `fs.Args()` is checked after `fs.Parse()`. In deploy subcommands, only `-config` flag is checked. Add: after parsing, if `configFile` is empty and `fs.NArg() > 0`, set `configFile` to `fs.Arg(0)`.

**Step 2: Add positional config fallback**

In each deploy subcommand that uses `-config`, after `fs.Parse(args)`:

```go
	if *configFile == "" && fs.NArg() > 0 {
		*configFile = fs.Arg(0)
	}
```

Apply to: `runDeployDocker`, `runDeployK8sGenerate`, `runDeployK8sApply`, `runDeployHelm`, `runDeployCloud`.

**Step 3: Run existing deploy tests**

Run: `go test ./cmd/wfctl/ -run TestDeploy -v`

**Step 4: Commit**

```bash
git add cmd/wfctl/deploy.go
git commit -m "fix(wfctl): deploy subcommands accept positional config arg"
```

---

### Task 8: `init` generates valid Dockerfile (handles missing go.sum)

**Files:**
- Modify: `cmd/wfctl/init.go` (the Dockerfile template)

**Step 1: Find and fix the Dockerfile template**

Search for the embedded Dockerfile template in `init.go` or `cmd/wfctl/templates/`. Change:

```dockerfile
COPY go.mod go.sum ./
```

to:

```dockerfile
COPY go.mod go.sum* ./
```

The `*` glob makes `go.sum` optional — Docker COPY with no match on `go.sum*` still copies `go.mod`.

Actually, Docker COPY requires at least one match. Better approach:

```dockerfile
COPY go.mod ./
RUN go mod download
```

This works whether `go.sum` exists or not.

**Step 2: Run tests and commit**

```bash
git add cmd/wfctl/init.go
git commit -m "fix(wfctl): init Dockerfile handles missing go.sum"
```

---

### Task 9: `validate --dir` skips non-workflow YAML files

**Files:**
- Modify: `cmd/wfctl/validate.go`
- Test: `cmd/wfctl/validate_test.go` (create or extend)

**Step 1: Write the failing test**

```go
func TestValidateSkipsNonWorkflowYAML(t *testing.T) {
	dir := t.TempDir()
	// Write a GitHub Actions YAML — should be skipped
	ghDir := filepath.Join(dir, ".github", "workflows")
	os.MkdirAll(ghDir, 0755)
	os.WriteFile(filepath.Join(ghDir, "ci.yml"), []byte("name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n"), 0644)
	// Write a real workflow YAML — should be validated
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("modules:\n  - name: test\n    type: http.server\n    config:\n      port: 8080\nworkflows:\n  http:\n    handler: http\n"), 0644)

	files := findYAMLFiles(dir)
	// ci.yml should be found but isWorkflowYAML should skip it
	workflowFiles := 0
	for _, f := range files {
		if isWorkflowYAML(f) {
			workflowFiles++
		}
	}
	if workflowFiles != 1 {
		t.Errorf("expected 1 workflow YAML, got %d", workflowFiles)
	}
}
```

**Step 2: Implement**

Add to `validate.go`:

```go
// isWorkflowYAML does a quick check for workflow-engine top-level keys.
func isWorkflowYAML(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// Check first 100 lines for modules:, workflows:, or pipelines: at top level
	lines := strings.SplitN(string(data), "\n", 100)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "modules:") || strings.HasPrefix(trimmed, "workflows:") || strings.HasPrefix(trimmed, "pipelines:") {
			return true
		}
	}
	return false
}
```

Then in the `--dir` file loop, add: `if !isWorkflowYAML(f) { continue }`.

**Step 3: Run tests and commit**

```bash
git add cmd/wfctl/validate.go cmd/wfctl/validate_test.go
git commit -m "fix(wfctl): validate --dir skips non-workflow YAML files"
```

---

### Task 10: Validate follows YAML imports

**Files:**
- Modify: `cmd/wfctl/validate.go`

**Context:** The config package already has `processImports` that resolves `imports:` references. When `validate` loads a file, it calls `config.LoadFromFile()` which follows imports automatically. However, the individual imported files may not be validated independently. Ensure that:

1. `validate` reports which imports were resolved
2. If an imported file has errors, the error references the import chain

**Step 1: Check current behavior**

Create a test config with imports and run validate to see if it already works:

```yaml
# /tmp/test-imports/main.yaml
imports:
  - modules.yaml
workflows:
  http:
    handler: http
```

```yaml
# /tmp/test-imports/modules.yaml
modules:
  - name: server
    type: http.server
    config:
      port: 8080
```

Run: `/tmp/wfctl validate /tmp/test-imports/main.yaml`

If it already resolves imports (which it should since `config.LoadFromFile` handles them), just add a verbose message like "Resolved import: modules.yaml". If it doesn't, wire import resolution into the validate path.

**Step 2: Add import resolution feedback**

In `validateFile()`, after loading the config, check if the original YAML had `imports:` and log what was resolved:

```go
// In validateFile, after successful load:
if len(rawImports) > 0 {
	fmt.Fprintf(os.Stderr, "  Resolved %d import(s): %s\n", len(rawImports), strings.Join(rawImports, ", "))
}
```

**Step 3: Run tests and commit**

```bash
git add cmd/wfctl/validate.go
git commit -m "fix(wfctl): validate reports resolved YAML imports"
```

---

### Task 11: Infra commands — better error messages

**Files:**
- Modify: `cmd/wfctl/infra.go:73`

**Step 1: Improve the error message**

Change line 73 from:
```go
return "", fmt.Errorf("no config file found (tried infra.yaml, config/infra.yaml)")
```
to:
```go
return "", fmt.Errorf("no infrastructure config found (tried infra.yaml, config/infra.yaml).\n" +
	"Create an infra config with cloud.account and platform.* modules.\n" +
	"Run 'wfctl init --template full-stack' for a starter config with infrastructure.")
```

**Step 2: Commit**

```bash
git add cmd/wfctl/infra.go
git commit -m "fix(wfctl): infra commands show helpful error when no config found"
```

---

### Task 12: `plugin info` shows absolute paths

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (runPluginInfo function, around line 275)

**Step 1: Fix**

In `runPluginInfo`, after constructing `pluginDir`, convert to absolute:

```go
	absDir, _ := filepath.Abs(pluginDir)
	// Use absDir when printing the binary path
```

**Step 2: Commit**

```bash
git add cmd/wfctl/plugin_install.go
git commit -m "fix(wfctl): plugin info shows absolute binary path"
```

---

### Task 13: PR #322 — PluginManifest legacy capabilities UnmarshalJSON

**Files:**
- Modify: `plugin/manifest.go` (or wherever `PluginManifest` is defined)
- Test: `plugin/manifest_test.go`

**Step 1: Find the PluginManifest type**

Run: `grep -rn "type PluginManifest struct" /Users/jon/workspace/workflow/plugin/`

**Step 2: Write the failing test**

```go
func TestPluginManifest_LegacyCapabilities(t *testing.T) {
	input := `{
		"name": "test",
		"version": "1.0.0",
		"capabilities": {
			"configProvider": true,
			"moduleTypes": ["test.module"],
			"stepTypes": ["step.test"],
			"triggerTypes": ["http"]
		}
	}`
	var m PluginManifest
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Legacy capabilities should be merged into top-level fields
	if len(m.ModuleTypes) == 0 || m.ModuleTypes[0] != "test.module" {
		t.Errorf("moduleTypes not parsed from legacy capabilities: %v", m.ModuleTypes)
	}
	if len(m.StepTypes) == 0 || m.StepTypes[0] != "step.test" {
		t.Errorf("stepTypes not parsed from legacy capabilities: %v", m.StepTypes)
	}
}
```

**Step 3: Implement UnmarshalJSON**

Add a custom `UnmarshalJSON` method on `PluginManifest` that:
1. First tries to unmarshal normally (new format with `capabilities` as `[]CapabilityDecl`)
2. If `capabilities` is an object, unmarshal it as `{configProvider, moduleTypes, stepTypes, triggerTypes}` and merge into the top-level manifest fields

**Step 4: Run tests and commit**

```bash
git add plugin/manifest.go plugin/manifest_test.go
git commit -m "fix(wfctl): PluginManifest handles legacy capabilities object format

Addresses PR #322."
```

---

## Phase B: Registry Data Fixes

### Task 14: Fix registry manifest issues

**Working directory:** `/Users/jon/workspace/workflow-registry/`

**Files:**
- Modify: `plugins/agent/manifest.json`
- Modify: `plugins/ratchet/manifest.json`
- Verify: `plugins/authz/manifest.json` exists and name matches

**Step 1: Fix agent manifest type**

In `plugins/agent/manifest.json`, change `"type": "internal"` to `"type": "builtin"`.

**Step 2: Fix ratchet manifest downloads**

In `plugins/ratchet/manifest.json`, add downloads entries:

```json
"downloads": [
  {"os": "linux", "arch": "amd64", "url": "https://github.com/GoCodeAlone/ratchet/releases/latest/download/ratchet_linux_amd64.tar.gz"},
  {"os": "linux", "arch": "arm64", "url": "https://github.com/GoCodeAlone/ratchet/releases/latest/download/ratchet_linux_arm64.tar.gz"},
  {"os": "darwin", "arch": "amd64", "url": "https://github.com/GoCodeAlone/ratchet/releases/latest/download/ratchet_darwin_amd64.tar.gz"},
  {"os": "darwin", "arch": "arm64", "url": "https://github.com/GoCodeAlone/ratchet/releases/latest/download/ratchet_darwin_arm64.tar.gz"}
]
```

**Step 3: Verify authz manifest**

Check `plugins/authz/manifest.json` exists. Verify the `name` field is `"authz"` (not `"workflow-plugin-authz"`). If it doesn't exist, create it from the existing `plugin.json` in the authz repo.

**Step 4: Fix schema validation gap (B5)**

Check `schema/registry-schema.json` — verify that the `type` enum includes `"builtin"` (not just `"external"` and `"internal"`). If the enum is missing `"builtin"`, add it. Then check CI (`.github/workflows/`) to confirm schema validation runs on PRs. If CI isn't validating manifests against the schema, add a step that runs `scripts/validate-manifests.sh`.

**Step 5: Run validation**

```bash
cd /Users/jon/workspace/workflow-registry
./scripts/validate-manifests.sh
```

**Step 6: Commit and push**

```bash
git add plugins/
git commit -m "fix: correct agent type, add ratchet downloads, verify authz manifest"
git push origin main
```

---

### Task 15: Create version sync script

**Files:**
- Create: `scripts/sync-versions.sh`

**Step 1: Write the script**

```bash
#!/usr/bin/env bash
# Compares manifest versions against latest GitHub release tags.
# Usage: ./scripts/sync-versions.sh [--fix]

set -euo pipefail

fix_mode=false
[[ "${1:-}" == "--fix" ]] && fix_mode=true

mismatches=0

for manifest in plugins/*/manifest.json; do
    name=$(jq -r .name "$manifest")
    repo=$(jq -r '.repository // empty' "$manifest")
    manifest_ver=$(jq -r .version "$manifest")

    [[ -z "$repo" ]] && continue

    # Extract owner/repo from URL
    owner_repo=$(echo "$repo" | sed 's|https://github.com/||')

    # Query latest release
    latest=$(gh release view --repo "$owner_repo" --json tagName -q .tagName 2>/dev/null || echo "")
    [[ -z "$latest" ]] && continue

    # Strip 'v' prefix for comparison
    latest_ver="${latest#v}"

    if [[ "$manifest_ver" != "$latest_ver" ]]; then
        echo "MISMATCH: $name — manifest=$manifest_ver, latest=$latest_ver ($owner_repo)"
        ((mismatches++))

        if $fix_mode; then
            jq --arg v "$latest_ver" '.version = $v' "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
            echo "  Fixed → $latest_ver"
        fi
    fi
done

echo ""
echo "$mismatches mismatch(es) found."
[[ $mismatches -gt 0 ]] && exit 1 || exit 0
```

**Step 2: Run it**

```bash
chmod +x scripts/sync-versions.sh
./scripts/sync-versions.sh
```

**Step 3: Fix any mismatches found**

```bash
./scripts/sync-versions.sh --fix
```

**Step 4: Commit**

```bash
git add scripts/sync-versions.sh plugins/
git commit -m "feat: add version sync script and fix manifest version mismatches"
```

---

## Phase C: Plugin Ecosystem

### Task 16: GitHub URL install support in wfctl

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (runPluginInstall)
- Modify: `cmd/wfctl/multi_registry.go` or create `cmd/wfctl/github_install.go`
- Test: `cmd/wfctl/plugin_install_test.go`

**Step 1: Write the test**

```go
func TestParseGitHubPluginRef(t *testing.T) {
	tests := []struct {
		input   string
		owner   string
		repo    string
		version string
		isGH    bool
	}{
		{"GoCodeAlone/workflow-plugin-authz@v0.3.1", "GoCodeAlone", "workflow-plugin-authz", "v0.3.1", true},
		{"GoCodeAlone/workflow-plugin-authz", "GoCodeAlone", "workflow-plugin-authz", "", true},
		{"authz", "", "", "", false},
		{"authz@v1.0", "", "", "", false},
	}
	for _, tt := range tests {
		owner, repo, version, isGH := parseGitHubRef(tt.input)
		if isGH != tt.isGH || owner != tt.owner || repo != tt.repo || version != tt.version {
			t.Errorf("parseGitHubRef(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tt.input, owner, repo, version, isGH, tt.owner, tt.repo, tt.version, tt.isGH)
		}
	}
}
```

**Step 2: Implement**

Create `cmd/wfctl/github_install.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// parseGitHubRef parses "owner/repo@version" format.
// Returns empty strings and false if not a GitHub ref.
func parseGitHubRef(input string) (owner, repo, version string, isGitHub bool) {
	// Must contain exactly one "/" to be a GitHub ref
	nameVer := input
	if atIdx := strings.LastIndex(input, "@"); atIdx > 0 {
		nameVer = input[:atIdx]
		version = input[atIdx+1:]
	}
	parts := strings.SplitN(nameVer, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], version, true
}

// installFromGitHub downloads a plugin directly from GitHub Releases.
func installFromGitHub(owner, repo, version, destDir string) error {
	if version == "" {
		version = "latest"
	}

	var releaseURL string
	if version == "latest" {
		releaseURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	} else {
		releaseURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, version)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(releaseURL)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("release %s not found for %s/%s (HTTP %d)", version, owner, repo, resp.StatusCode)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}

	// Find matching asset: repo_os_arch.tar.gz
	wantName := fmt.Sprintf("%s_%s_%s.tar.gz", repo, runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == wantName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no asset matching %s found in release (available: %d assets)", wantName, len(release.Assets))
	}

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", downloadURL)
	data, err := downloadURL(downloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	if err := extractTarGz(data, destDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	return ensurePluginBinary(destDir, repo)
}
```

Then in `runPluginInstall`, after the registry lookup fails:

```go
	manifest, sourceName, err := mr.FetchManifest(pluginName)
	if err != nil {
		// Try GitHub direct install if input looks like owner/repo
		owner, repo, ver, isGH := parseGitHubRef(nameArg)
		if isGH {
			shortName := normalizePluginName(repo)
			destDir := filepath.Join(pluginDirVal, shortName)
			os.MkdirAll(destDir, 0750)
			return installFromGitHub(owner, repo, ver, destDir)
		}
		return err
	}
```

**Step 3: Run tests and commit**

```bash
git add cmd/wfctl/github_install.go cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat(wfctl): plugin install supports owner/repo@version GitHub URLs

Falls back to GitHub Releases API when registry lookup fails.
Addresses issue #316 item 2."
```

---

### Task 17: Plugin lockfile support (`.wfctl.yaml` plugins section)

**Files:**
- Create: `cmd/wfctl/plugin_lockfile.go`
- Create: `cmd/wfctl/plugin_lockfile_test.go`
- Modify: `cmd/wfctl/plugin_install.go`

**Step 1: Write the test**

```go
func TestLoadPluginLockfile(t *testing.T) {
	dir := t.TempDir()
	content := `plugins:
  authz:
    version: v0.3.1
    repository: GoCodeAlone/workflow-plugin-authz
  payments:
    version: v0.1.0
    repository: GoCodeAlone/workflow-plugin-payments
`
	os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(content), 0644)

	lf, err := loadPluginLockfile(filepath.Join(dir, ".wfctl.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(lf.Plugins))
	}
	if lf.Plugins["authz"].Version != "v0.3.1" {
		t.Errorf("authz version = %q", lf.Plugins["authz"].Version)
	}
}
```

**Step 2: Implement**

```go
// cmd/wfctl/plugin_lockfile.go
package main

import (
	"fmt"
	"os"
	"gopkg.in/yaml.v3"
)

type PluginLockEntry struct {
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
	SHA256     string `yaml:"sha256,omitempty"`
}

type PluginLockfile struct {
	Plugins map[string]PluginLockEntry `yaml:"plugins"`
	// Preserve other .wfctl.yaml fields
	raw map[string]any
}

func loadPluginLockfile(path string) (*PluginLockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lf PluginLockfile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Preserve raw for round-trip
	yaml.Unmarshal(data, &lf.raw)
	return &lf, nil
}

func (lf *PluginLockfile) Save(path string) error {
	if lf.raw == nil {
		lf.raw = make(map[string]any)
	}
	lf.raw["plugins"] = lf.Plugins
	data, err := yaml.Marshal(lf.raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
```

**Step 3: Wire into `plugin install`**

In `runPluginInstall`:
- After successful install, if `.wfctl.yaml` exists in cwd, update the plugins section
- Add `wfctl plugin install` (no args) path: read lockfile, install all entries

**Step 4: Run tests and commit**

```bash
git add cmd/wfctl/plugin_lockfile.go cmd/wfctl/plugin_lockfile_test.go cmd/wfctl/plugin_install.go
git commit -m "feat(wfctl): plugin lockfile support in .wfctl.yaml

'wfctl plugin install' with no args reads .wfctl.yaml plugins section.
Installing a plugin with @version updates the lockfile entry.
Addresses issue #316 item 3."
```

---

### Task 18: Engine minEngineVersion check

**Files:**
- Modify: `plugin/loader.go` (or wherever plugins are loaded)
- Test: `plugin/loader_test.go`

**Step 1: Find the plugin loading code**

Run: `grep -rn "minEngineVersion\|MinEngineVersion\|LoadManifest" /Users/jon/workspace/workflow/plugin/ | head -20`

**Step 2: Implement version check**

After loading `plugin.json`, compare `minEngineVersion` with the engine's version:

```go
import "github.com/Masterminds/semver/v3"

func checkEngineCompatibility(manifest *PluginManifest, engineVersion string) {
	if manifest.MinEngineVersion == "" || engineVersion == "" || engineVersion == "dev" {
		return
	}
	minVer, err := semver.NewVersion(manifest.MinEngineVersion)
	if err != nil {
		return
	}
	engVer, err := semver.NewVersion(strings.TrimPrefix(engineVersion, "v"))
	if err != nil {
		return
	}
	if engVer.LessThan(minVer) {
		fmt.Fprintf(os.Stderr, "WARNING: plugin %q requires engine >= v%s, running v%s — may cause runtime failures\n",
			manifest.Name, manifest.MinEngineVersion, engineVersion)
	}
}
```

**Step 3: Run tests and commit**

```bash
git add plugin/
git commit -m "feat: engine warns when plugin minEngineVersion exceeds current version

Addresses issue #316 item 5."
```

---

### Task 19: goreleaser audit across plugin repos

**Working directory:** Each plugin repo

**Step 1: Create reference goreleaser config**

Save to `/Users/jon/workspace/workflow/docs/plugin-goreleaser-reference.yml`:

```yaml
# Reference goreleaser config for workflow plugins
# Ensures consistent tarball layout: bare binary + plugin.json
version: 2
builds:
  - binary: "{{ .ProjectName }}"
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    env: [CGO_ENABLED=0]
    ldflags: ["-s", "-w"]

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
    files:
      - plugin.json

before:
  hooks:
    - cmd: "sed -i'' -e 's/\"version\": *\"[^\"]*\"/\"version\": \"{{ .Version }}\"/' plugin.json"

checksum:
  name_template: checksums.txt
```

**Step 2: Audit each plugin repo**

For each repo in: `workflow-plugin-authz`, `workflow-plugin-payments`, `workflow-plugin-admin`, `workflow-plugin-bento`, `workflow-plugin-github`, `workflow-plugin-waf`, `workflow-plugin-security`, `workflow-plugin-sandbox`, `workflow-plugin-supply-chain`, `workflow-plugin-data-protection`, `workflow-plugin-authz-ui`, `workflow-plugin-cloud-ui`:

1. Read `.goreleaser.yml` or `.goreleaser.yaml`
2. Check binary naming (should be `{{ .ProjectName }}`, not platform-suffixed)
3. Check archive includes `plugin.json`
4. Check `plugin.json` version is templated from tag
5. Fix any deviations

**Step 3: Commit fixes per repo**

Each repo gets its own commit: `"chore: standardize goreleaser config for consistent tarball layout"`

---

### Task 20: Registry auto-sync CI for plugin repos

**Files per plugin repo:**
- Modify: `.github/workflows/release.yml` (add registry update step)

**Step 1: Create reusable workflow snippet**

After the goreleaser step in each plugin's `release.yml`, add:

```yaml
    - name: Update registry manifest
      if: success()
      env:
        GH_TOKEN: ${{ secrets.REGISTRY_PAT }}
      run: |
        PLUGIN_NAME=$(jq -r .name plugin.json)
        VERSION="${GITHUB_REF_NAME#v}"

        # Clone registry
        git clone https://x-access-token:${GH_TOKEN}@github.com/GoCodeAlone/workflow-registry.git /tmp/registry
        cd /tmp/registry

        # Update version
        MANIFEST="plugins/${PLUGIN_NAME}/manifest.json"
        jq --arg v "$VERSION" '.version = $v' "$MANIFEST" > "$MANIFEST.tmp" && mv "$MANIFEST.tmp" "$MANIFEST"

        # Update download URLs
        REPO="${GITHUB_REPOSITORY}"
        TAG="${GITHUB_REF_NAME}"
        jq --arg repo "$REPO" --arg tag "$TAG" '
          .downloads = [
            {"os":"linux","arch":"amd64","url":"https://github.com/\($repo)/releases/download/\($tag)/\(.name)_linux_amd64.tar.gz"},
            {"os":"linux","arch":"arm64","url":"https://github.com/\($repo)/releases/download/\($tag)/\(.name)_linux_arm64.tar.gz"},
            {"os":"darwin","arch":"amd64","url":"https://github.com/\($repo)/releases/download/\($tag)/\(.name)_darwin_amd64.tar.gz"},
            {"os":"darwin","arch":"arm64","url":"https://github.com/\($repo)/releases/download/\($tag)/\(.name)_darwin_arm64.tar.gz"}
          ]' "$MANIFEST" > "$MANIFEST.tmp" && mv "$MANIFEST.tmp" "$MANIFEST"

        # Create PR
        BRANCH="auto/update-${PLUGIN_NAME}-${TAG}"
        git checkout -b "$BRANCH"
        git add "$MANIFEST"
        git commit -m "chore: update ${PLUGIN_NAME} manifest to ${TAG}"
        git push origin "$BRANCH"
        gh pr create --repo GoCodeAlone/workflow-registry --title "Update ${PLUGIN_NAME} to ${TAG}" --body "Automated manifest update from release ${TAG}."
```

**Step 2: Apply to each external plugin repo that has a registry manifest**

Repos: authz, payments, waf, security, sandbox, supply-chain, data-protection, authz-ui, cloud-ui, bento, github, admin

**Step 3: Commit per repo**

```bash
git commit -m "ci: auto-update registry manifest on release"
```

---

## Summary

| Phase | Tasks | Scope |
|-------|-------|-------|
| A: CLI Fixes | 1-13 | workflow repo, cmd/wfctl/ |
| B: Registry Data | 14-15 | workflow-registry repo |
| C: Ecosystem | 16-20 | workflow repo + all plugin repos |

**Total: 20 tasks**

Phase A (Tasks 1-13) can be done in parallel by 2 implementers splitting the work.
Phase B (Tasks 14-15) is independent and can run in parallel with Phase A.
Phase C (Tasks 16-20) depends on Phase A being complete (especially Tasks 2, 4, 5 for the plugin-dir and name resolution changes).
