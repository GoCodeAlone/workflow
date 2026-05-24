# wfctl plugin verify-capabilities Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `wfctl plugin verify-capabilities --binary <path> <plugin-dir>` subcommand that spawns a plugin binary, calls `PluginService.GetManifest` directly via gRPC, and diffs `Name` + `Version` against plugin.json with sentinel-pattern matching to catch the ldflag-missing truth-loop bug from workflow#762/#764.

**Architecture:** New subcommand registered in `cmd/wfctl/plugin.go`. Spawn-and-dial wired INLINE in the subcommand (~40 LOC) — `plugin_conformance.go`'s spawn pattern is the reference; no shared-helper extraction in this PR (defer until a 3rd caller appears). GetManifest called DIRECTLY via `pb.NewPluginServiceClient(pluginClient.Conn())` to bypass `ExternalPluginAdapter.EngineManifest()`'s precedence rules (which would defeat the truth-loop check). Diff logic: exact Name + sentinel-aware Version matrix.

**Tech Stack:** Go (workflow CLI), `goplugin` (go-plugin v1.7), `pb.PluginService` (gRPC, raw client), `external.Handshake`, `external.PluginClient.Conn()`.

**Base branch:** `main`

**Design doc:** `docs/plans/2026-05-24-verify-capabilities-design.md` (cycle-6 PASS adversarial).

**Issue:** workflow#765

**Revision history:**
- Cycle 1: 9-task plan with shared `spawnAndDial` helper extraction. FAILED — 4 Critical (fictional `EngineManifest()` signature; fixture template wrong PluginManifest type; missing-ldflag mechanics misstated; fixture plugin.json shape diverges from PluginManifest).
- Cycle 2: drop helper extraction; direct GetManifest RPC; fix fixture types; fix sentinel mechanics. FAILED — 3 Critical (anchor `case "validate-contract":` didn't exist on stale worktree base; duplicate `import (...)` blocks in test+production files would fail compile; name-drift test assertion too lenient).
- Cycle 3 (this version): rebased worktree onto current main (validate-contract + registry-sync now in dispatcher). Restructured every "append imports" instruction to "Edit the SINGLE existing import block" with explicit warnings. Task 4 Step 1 documents the final import-block shape end-to-end. Fixture go-directive bumped 1.24 → 1.26.0 (matches workflow root). Name-drift fixture ldflag changed to `v0.0.0` so Version matrix PASSes (isolated Name diff); test assertion tightened to `"name:"` substring; verify-capabilities error now embeds joined failure list so tests can assert on field-name without capturing stderr.

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 8
**Estimated Lines of Change:** ~500 (subcommand + tests + 5 fixtures + docs)

**Out of scope:**
- Contract-diff (`GetContractRegistry` walk) — deferred to follow-up issue #766; needs `capabilities.iacServices` schema on `PluginManifest` first
- Per-type RPC walk (`GetModuleTypes`/`GetStepTypes`/`GetTriggerTypes`) — IaC bridge returns Unimplemented for these
- Build-from-source mode — `--binary` REQUIRED; dev convenience builds documented in §Synopsis
- `--json` output mode — defer YAGNI
- Multi-binary repos — runs against the binary passed; multi-binary plugins invoke multiple times
- Author/Description/ConfigMutable/SampleCategory diff — display fields, not contract surface
- `minEngineVersion` runtime check — not on `pb.Manifest`; static-check responsibility
- Scaffold-template release.yml wiring — separate follow-up PR on scaffold-workflow-plugin after this lands
- Shared `spawnAndDial` helper extraction — defer until a 3rd caller exists (cycle-2 reviewer Option 3)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): plugin verify-capabilities subcommand (workflow#765) | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7, Task 8 | feat/765-verify-capabilities |

**Status:** Draft

---

## Task 1: Subcommand registration + flag parsing skeleton

**Change class:** CLI command.

**Files:**
- Create: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin.go` (add `case "verify-capabilities"` + help line)
- Test: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the failing tests**

Create `cmd/wfctl/plugin_verify_capabilities_test.go`:

**Note: every "append" instruction in this plan EDITS the existing file's import block (Go disallows redundant imports across multiple `import` declarations in the same file). Adding new imports = Edit the existing block; never append a second `import (...)` block.**

```go
package main

import (
	"os"           // added in Task 2
	"path/filepath" // added in Task 2
	"strings"
	"testing"
)

func TestVerifyCapabilitiesUsage(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{})
	if err == nil {
		t.Fatal("want error for missing args")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestVerifyCapabilitiesRequiresBinary(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{"."})
	if err == nil {
		t.Fatal("want error when --binary missing")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}
```

**Step 2: Run test — verify FAIL**

Run: `cd cmd/wfctl && go test -run TestVerifyCapabilities -count=1 ./...`
Expected: FAIL with `undefined: runPluginVerifyCapabilities`.

**Step 3: Create the subcommand skeleton**

Create `cmd/wfctl/plugin_verify_capabilities.go`:

```go
// Package main — `wfctl plugin verify-capabilities` subcommand.
// Spawns a plugin binary, calls PluginService.GetManifest directly via gRPC,
// diffs returned Manifest against plugin.json. Catches ldflag-missing
// truth-loop bug from workflow#762/#764.
//
// Design: docs/plans/2026-05-24-verify-capabilities-design.md
// Issue:  https://github.com/GoCodeAlone/workflow/issues/765
package main

import (
	"flag"
	"fmt"
)

func runPluginVerifyCapabilities(args []string) error {
	fs := flag.NewFlagSet("plugin verify-capabilities", flag.ContinueOnError)
	binary := fs.String("binary", "", "Path to plugin binary (REQUIRED)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin verify-capabilities --binary <path> <plugin-dir>

Spawn the plugin binary and verify its runtime PluginService.GetManifest
matches the declared plugin.json. Catches ldflag-missing / version-drift
bugs at release time (workflow#762 truth-loop closure).

REQUIRED: --binary <path>  (no build-from-source; operator builds the binary)

WARNING: this command EXECUTES <binary> as a subprocess. Only run against
build artifacts you trust.

Examples:
  # Local dev:
  go build -ldflags="-X github.com/.../internal.Version=v1.2.3" -o /tmp/p ./cmd/<name>
  wfctl plugin verify-capabilities --binary /tmp/p .

  # CI (post-goreleaser, in release.yml):
  RUNNER_ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
  BIN=$(jq -r --arg arch "$RUNNER_ARCH" \
    '[.[] | select(.type=="Binary" and .goos=="linux" and .goarch==$arch)] | .[0].path // ""' \
    dist/artifacts.json)
  wfctl plugin verify-capabilities --binary "$BIN" .

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *binary == "" {
		fs.Usage()
		return fmt.Errorf("--binary is required")
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("exactly one <plugin-dir> argument required")
	}
	pluginDir := fs.Arg(0)
	_ = pluginDir
	return fmt.Errorf("not yet implemented")
}
```

Edit `cmd/wfctl/plugin.go`:
- Find the `case "validate-contract":` dispatcher block (~line 38) and add right after:
  ```go
  case "verify-capabilities":
      return runPluginVerifyCapabilities(args[1:])
  ```
- In the usage() help-text block, add (alphabetical):
  ```go
  fmt.Fprintln(out, "  verify-capabilities  Spawn plugin binary, verify runtime GetManifest matches plugin.json")
  ```

**Step 4: Run test — verify PASS**

Run: `cd cmd/wfctl && go build ./... && go test -run TestVerifyCapabilities -count=1 ./...`
Expected: build exit 0; both tests PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go cmd/wfctl/plugin.go
git commit -m "feat(wfctl): plugin verify-capabilities subcommand skeleton (workflow#765)"
```

---

## Task 2: Preflight binary path validation

**Change class:** Internal logic refactor (input validation).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the failing tests**

Append the test functions below to `cmd/wfctl/plugin_verify_capabilities_test.go`. **DO NOT add a new `import (...)` block** — the file's existing import block (created in Task 1 with `os`, `path/filepath`, `strings`, `testing`) already covers everything these tests need.

```go
func TestPreflightBinaryEmpty(t *testing.T) {
	if err := preflightBinary(""); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want empty-path error, got %v", err)
	}
}

func TestPreflightBinaryNull(t *testing.T) {
	if err := preflightBinary("null"); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want null-path error (jq fallback), got %v", err)
	}
}

func TestPreflightBinaryMissing(t *testing.T) {
	if err := preflightBinary("/nonexistent/missing-xyz"); err == nil || !strings.Contains(err.Error(), "stat") {
		t.Errorf("want stat error, got %v", err)
	}
}

func TestPreflightBinaryDirectory(t *testing.T) {
	if err := preflightBinary(t.TempDir()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("want directory error, got %v", err)
	}
}

func TestPreflightBinaryNonExecutable(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("not-exec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Errorf("want non-executable error, got %v", err)
	}
}

func TestPreflightBinaryOK(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err != nil {
		t.Errorf("want PASS, got %v", err)
	}
}
```

**Step 2: Run test — verify FAIL**

Run: `cd cmd/wfctl && go test -run TestPreflightBinary -count=1 ./...`
Expected: FAIL `undefined: preflightBinary`.

**Step 3: Implement**

In `cmd/wfctl/plugin_verify_capabilities.go`: **Edit the existing single import block** to add `"os"` (alongside existing `"flag"`, `"fmt"`). DO NOT add a second `import (...)` block. Then append the `preflightBinary` function below.

```go
// preflightBinary validates the --binary path before exec:
//   - non-empty + not literal "null" (guards against jq fallback returning empty)
//   - file exists and is a regular file (not directory)
//   - has at least one executable bit set
func preflightBinary(path string) error {
	if path == "" || path == "null" {
		return fmt.Errorf("--binary path empty (jq filter may have returned no match)")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("--binary %q is a directory", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("--binary %q is not a regular file (mode=%s)", path, fi.Mode())
	}
	if fi.Mode()&0o111 == 0 {
		return fmt.Errorf("--binary %q is not executable (mode=%s)", path, fi.Mode())
	}
	return nil
}
```

In `runPluginVerifyCapabilities`, before the `return fmt.Errorf("not yet implemented")` line:

```go
if err := preflightBinary(*binary); err != nil {
    return err
}
```

**Step 4: Run test — verify PASS**

Run: `cd cmd/wfctl && go test -run TestPreflightBinary -count=1 ./...`
Expected: all 6 preflight tests PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities preflight binary-path validation (workflow#765)"
```

---

## Task 3: Sentinel-pattern Version diff matrix

**Change class:** Internal logic refactor (pure-logic diff function).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the failing tests** (table-driven)

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
func TestIsSentinel(t *testing.T) {
	cases := map[string]bool{
		"":                          true,
		"dev":                       true,
		"0.0.0":                     true,
		"(devel)":                   true,
		"(devel) [@ a1b2c3d]":       true,
		"(devel) [@ a1b2c3d.dirty]": true,
		"v1.2.3":                    false,
		"1.2.3":                     false,
		"v0.0.1":                    false,
	}
	for v, want := range cases {
		if got := isSentinel(v); got != want {
			t.Errorf("isSentinel(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDiffVersion(t *testing.T) {
	cases := []struct {
		declared, runtime string
		wantPass          bool
		wantReason        string
	}{
		// 0.0.0 + non-sentinel -> PASS (CI artifact)
		{"0.0.0", "v1.2.3", true, ""},
		{"0.0.0", "0.1.0", true, ""},
		// 0.0.0 + sentinel -> FAIL (ldflag missing)
		{"0.0.0", "", false, "ldflag"},
		{"0.0.0", "(devel)", false, "ldflag"},
		{"0.0.0", "(devel) [@ abc1234]", false, "ldflag"},
		{"0.0.0", "dev", false, "ldflag"},
		{"0.0.0", "0.0.0", false, "ldflag"},
		// X.Y.Z + vX.Y.Z or X.Y.Z -> PASS (normalize leading v)
		{"1.2.3", "v1.2.3", true, ""},
		{"1.2.3", "1.2.3", true, ""},
		// X.Y.Z + sentinel -> FAIL
		{"1.2.3", "", false, "ldflag"},
		{"1.2.3", "(devel)", false, "ldflag"},
		{"1.2.3", "(devel) [@ deadbee]", false, "ldflag"},
		// X.Y.Z + drift -> FAIL
		{"1.2.3", "v0.9.0", false, "drift"},
		{"1.2.3", "v2.0.0", false, "drift"},
	}
	for _, c := range cases {
		pass, reason := diffVersion(c.declared, c.runtime)
		if pass != c.wantPass {
			t.Errorf("diffVersion(%q, %q) pass=%v want=%v reason=%q",
				c.declared, c.runtime, pass, c.wantPass, reason)
			continue
		}
		if !pass && !strings.Contains(reason, c.wantReason) {
			t.Errorf("diffVersion(%q, %q) reason=%q want substring %q",
				c.declared, c.runtime, reason, c.wantReason)
		}
	}
}
```

**Step 2: Run tests — verify FAIL**

Run: `cd cmd/wfctl && go test -run "TestIsSentinel|TestDiffVersion" -count=1 ./...`
Expected: FAIL `undefined: isSentinel`, `undefined: diffVersion`.

**Step 3: Implement**

In `cmd/wfctl/plugin_verify_capabilities.go`: **Edit the existing single import block** to add `"strings"`. Then append `isSentinel` + `diffVersion` below.

```go
// isSentinel returns true when v is one of the SDK's dev-sentinel forms
// OR the on-disk plugin.json sentinel "0.0.0".
//
// SDK sentinel set (per plugin/external/sdk/buildversion.go:36-42):
//   "", "dev", "(devel)" — ResolveBuildVersion replaces these with build-info
// Plus build-info fallback produces "(devel) [@ <sha>[.dirty]]" — HasPrefix catches all forms.
// Plus on-disk plugin.json "0.0.0" sentinel (workflow#762 convention).
//
// The predicate MUST be a SUPERSET of the SDK's set; "dev" is defensive
// (canonical wiring through sdk.ResolveBuildVersion prevents literal "dev"
// from reaching the wire — included to catch non-canonical wiring accidents).
func isSentinel(v string) bool {
	switch v {
	case "", "dev", "0.0.0", "(devel)":
		return true
	}
	return strings.HasPrefix(v, "(devel)")
}

// diffVersion implements the Version-rule matrix from the design doc:
//
//   plugin.json   binary Manifest.Version   outcome
//   ------------  ------------------------  -------
//   "0.0.0"       non-sentinel              PASS (CI artifact under verification)
//   "0.0.0"       sentinel                  FAIL (ldflag injection missing)
//   "X.Y.Z"       "vX.Y.Z" or "X.Y.Z"       PASS (normalize leading v)
//   "X.Y.Z"       sentinel                  FAIL (ldflag missing)
//   "X.Y.Z"       anything else             FAIL (version drift)
//
// Returns (pass bool, reason string). reason is non-empty only when pass=false.
func diffVersion(declared, runtime string) (bool, string) {
	runtimeSentinel := isSentinel(runtime)
	if declared == "0.0.0" {
		if runtimeSentinel {
			return false, fmt.Sprintf("ldflag injection missing: plugin.json=%q; binary Manifest.Version=%q (sentinel)", declared, runtime)
		}
		return true, ""
	}
	if runtimeSentinel {
		return false, fmt.Sprintf("ldflag injection missing: plugin.json=%q (release); binary Manifest.Version=%q (sentinel)", declared, runtime)
	}
	rNorm := strings.TrimPrefix(runtime, "v")
	if rNorm == declared {
		return true, ""
	}
	return false, fmt.Sprintf("version drift: plugin.json=%q; binary Manifest.Version=%q", declared, runtime)
}
```

**Step 4: Run tests — verify PASS**

Run: `cd cmd/wfctl && go test -run "TestIsSentinel|TestDiffVersion" -count=1 ./...`
Expected: all matrix cases PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities sentinel-pattern Version diff matrix (workflow#765)"
```

---

## Task 4: Inline spawn-and-dial + direct GetManifest RPC

**Change class:** Plugin / extension (CLI subcommand that calls a plugin via raw gRPC).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`

This task wires the actual spawn-and-dial INLINE (no shared helper extraction — cycle-2 reviewer Option 3 + I2 elimination). GetManifest is called DIRECTLY via `pb.NewPluginServiceClient(pluginClient.Conn())` to bypass `ExternalPluginAdapter`'s precedence rules.

**Step 1: Edit the existing import block**

Add these to the SINGLE existing import block at the top of `cmd/wfctl/plugin_verify_capabilities.go` (do NOT add a second `import (...)` declaration):

- `"context"`
- `"encoding/json"`
- `"os/exec"`
- `"path/filepath"`
- `"time"`
- `external "github.com/GoCodeAlone/workflow/plugin/external"`
- `pb "github.com/GoCodeAlone/workflow/plugin/external/proto"`
- `"github.com/GoCodeAlone/workflow/plugin"`
- `goplugin "github.com/GoCodeAlone/go-plugin"`
- `hclog "github.com/hashicorp/go-hclog"`
- `"google.golang.org/protobuf/types/known/emptypb"`

Final import block should contain (alphabetical, stdlib then 3rd-party):

```go
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/plugin"
	external "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	hclog "github.com/hashicorp/go-hclog"
	"google.golang.org/protobuf/types/known/emptypb"
)
```

**Step 2: Load + validate plugin.json**

In `runPluginVerifyCapabilities`, replace the `return fmt.Errorf("not yet implemented")` line with:

```go
abs, err := filepath.Abs(pluginDir)
if err != nil {
    return fmt.Errorf("resolve %q: %w", pluginDir, err)
}
manifestPath := filepath.Join(abs, "plugin.json")
manifestBytes, err := os.ReadFile(manifestPath) //nolint:gosec // operator-supplied path.
if err != nil {
    return fmt.Errorf("plugin.json: %w", err)
}
var declared plugin.PluginManifest
if err := json.Unmarshal(manifestBytes, &declared); err != nil {
    return fmt.Errorf("plugin.json parse: %w", err)
}
if err := declared.Validate(); err != nil {
    return fmt.Errorf("plugin.json validate: %w", err)
}
```

**Step 3: Spawn + dial INLINE (no shared helper)**

Continue in `runPluginVerifyCapabilities`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

binAbs, err := filepath.Abs(*binary)
if err != nil {
    return fmt.Errorf("resolve --binary %q: %w", *binary, err)
}

var stdout, stderr tailBuffer
cmd := exec.CommandContext(ctx, binAbs) //nolint:gosec // operator-supplied binary path.
client := goplugin.NewClient(&goplugin.ClientConfig{
    HandshakeConfig:  external.Handshake,
    Plugins:          goplugin.PluginSet{"plugin": &external.GRPCPlugin{}},
    Cmd:              cmd,
    AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
    Stderr:           &stderr,
    SyncStdout:       &stdout,
    SyncStderr:       &stderr,
    Logger:           hclog.NewNullLogger(),
})
defer client.Kill()

rpcClient, err := client.Client()
if err != nil {
    if ctx.Err() != nil {
        return fmt.Errorf("timeout waiting for plugin handshake (stderr: %s)", stderr.String())
    }
    return fmt.Errorf("plugin dial: %w (stderr: %s)", err, stderr.String())
}
raw, err := rpcClient.Dispense("plugin")
if err != nil {
    return fmt.Errorf("dispense plugin: %w (stderr: %s)", err, stderr.String())
}
pluginClient, ok := raw.(*external.PluginClient)
if !ok {
    return fmt.Errorf("dispensed object is %T, want *external.PluginClient", raw)
}
```

Note: `tailBuffer` is defined in `cmd/wfctl/plugin_conformance.go` (same package). All required imports already added in Step 1.

**Step 4: Call GetManifest DIRECTLY via raw gRPC client** (bypasses adapter precedence)

```go
pbClient := pb.NewPluginServiceClient(pluginClient.Conn())
runtime, err := pbClient.GetManifest(ctx, &emptypb.Empty{})
if err != nil {
    return fmt.Errorf("GetManifest RPC: %w (stderr: %s)", err, stderr.String())
}
```

**Step 5: Diff Name + Version and report**

```go
var failures []string
if runtime.GetName() != declared.Name {
    failures = append(failures, fmt.Sprintf("name: plugin.json=%q; binary Manifest.Name=%q", declared.Name, runtime.GetName()))
}
if pass, reason := diffVersion(declared.Version, runtime.GetVersion()); !pass {
    failures = append(failures, "version: "+reason)
}
if len(failures) > 0 {
    fmt.Fprintf(os.Stderr, "FAIL  %s (plugin.json)\nerror: %d mismatch(es)\n", declared.Name, len(failures))
    for _, f := range failures {
        fmt.Fprintf(os.Stderr, "  - %s\n", f)
    }
    // Embed the joined failure list in the returned error so tests can assert
    // on specific field names (e.g. "name:" prefix) without capturing stderr.
    return fmt.Errorf("verify-capabilities: %d mismatch(es): %s", len(failures), strings.Join(failures, "; "))
}
fmt.Printf("OK    %s %s (plugin.json: %s)\n", declared.Name, runtime.GetVersion(), declared.Version)
return nil
```

**Step 6: Build + help-output sanity check**

Run:
```bash
cd cmd/wfctl && go build -o /tmp/wfctl ./... && /tmp/wfctl plugin verify-capabilities --help
```
Expected: help text printed; exit 0. Help contains "REQUIRED: --binary", "WARNING: this command EXECUTES", `jq` CI example.

**Step 7: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go
git commit -m "feat(wfctl): wire inline spawn + direct GetManifest + Name/Version diff (workflow#765)"
```

---

## Task 5: Create 4 build-PASS fixture scenarios

**Change class:** Test fixture (no runtime impact).

**Files:**
- Create: `cmd/wfctl/testdata/verify_capabilities/good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/release-good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/missing-ldflag/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/version-drift/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/README.md`

Cycle-2 fix C2: fixture template uses `sdk.PluginManifest` (sdk-package value, NOT `plugin.PluginManifest`), no error return on `Manifest()`. Cycle-2 fix C3: initial `Version = "dev"` so `ResolveBuildVersion("dev")` falls back to `(devel)` when no ldflag is applied (true exercise of the missing-ldflag scenario). Cycle-2 fix C4: minimal plugin.json with only fields PluginManifest actually models.

**Step 1: Write the generator script** (one-off; not committed)

Save as `/tmp/gen-verify-fixtures.sh`:

```bash
#!/bin/bash
set -euo pipefail
BASE=cmd/wfctl/testdata/verify_capabilities
REPO_ROOT=$(git rev-parse --show-toplevel)
declare -A NAMES=( [good]=verify-good [release-good]=verify-release-good [missing-ldflag]=verify-missing-ldflag [version-drift]=verify-version-drift )
declare -A VERS=( [good]=0.0.0 [release-good]=1.2.3 [missing-ldflag]=0.0.0 [version-drift]=1.2.3 )
for s in good release-good missing-ldflag version-drift; do
  d="$BASE/$s"
  mkdir -p "$d"
  name="${NAMES[$s]}"
  version="${VERS[$s]}"
  cat > "$d/plugin.json" <<JSON
{
  "name": "$name",
  "version": "$version",
  "minEngineVersion": "v0.62.0",
  "author": "test fixture",
  "description": "verify-capabilities $s scenario"
}
JSON
  cat > "$d/main.go" <<GO
package main

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

// Version is ldflag-injected at build time. Initial "dev" so
// sdk.ResolveBuildVersion falls back to "(devel) [@ <sha>]" when no
// ldflag fires (exercises the missing-ldflag scenario faithfully).
var Version = "dev"

type stubProvider struct{}

func (stubProvider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "$name",
		Version:     "$version",
		Author:      "test fixture",
		Description: "verify-capabilities $s scenario",
	}
}

func main() {
	sdk.Serve(stubProvider{},
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)),
	)
}
GO
  cat > "$d/go.mod" <<MOD
module github.com/test/$s

go 1.26.0

require github.com/GoCodeAlone/workflow v0.62.0

replace github.com/GoCodeAlone/workflow => $REPO_ROOT
MOD
done
```

**Step 2: Generate + tidy + rewrite to relative replace**

```bash
bash /tmp/gen-verify-fixtures.sh
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go mod tidy)
  sed -i.bak "s|replace github.com/GoCodeAlone/workflow => .*|replace github.com/GoCodeAlone/workflow => ../../../../..|" "$d/go.mod"
  rm -f "$d/go.mod.bak"
done
```

**Step 3: Verify all 4 fixtures build standalone**

```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go build -mod=readonly -o /tmp/p .) && echo "$d: ok" || { echo "$d: FAIL"; exit 1; }
done
```
Expected: all 4 print `ok`.

**Step 4: Create maintenance README**

```bash
cat > cmd/wfctl/testdata/verify_capabilities/README.md <<'MD'
# verify_capabilities test fixtures

Fixtures for `plugin_verify_capabilities_test.go` (workflow#765).

Each scenario directory is a self-contained Go module. Tests build in-place
with `go build -mod=readonly`; binary emitted to `t.TempDir()`.

## Maintenance

When workflow SDK adds a new transitive dep that fixtures pick up, regenerate
each fixture's `go.sum`:

```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go mod tidy)
done
git add cmd/wfctl/testdata/verify_capabilities/*/go.sum
```

The `replace github.com/GoCodeAlone/workflow => ../../../../..` directive
resolves 5-ups from each scenario directory to the workflow repo root.
DO NOT use an absolute path — it diverges across developer machines.

## Scenarios

- `good/` — plugin.json version=0.0.0, ldflag injects v0.1.0 → PASS (CI artifact case)
- `release-good/` — plugin.json version=1.2.3, ldflag injects v1.2.3 → PASS (release case)
- `missing-ldflag/` — plugin.json version=0.0.0, no ldflag (Version="dev" → ResolveBuildVersion returns "(devel) [@ sha]") → FAIL
- `version-drift/` — plugin.json version=1.2.3, ldflag injects v0.9.0 → FAIL
- `name-drift/` — plugin.json name="foo", binary advertises Name="bar" (see Task 6 — created separately) → FAIL

## SDK semantics

`sdk.ResolveBuildVersion` returns its argument unchanged UNLESS the arg is one
of `{"", "dev", "(devel)"}`, in which case it consults `debug.ReadBuildInfo()`
and returns `"(devel) [@ <sha>[.dirty]]"`. So:

- Initial `var Version = "dev"` + no ldflag → wire Version is `"(devel) [@ sha]"`
- Initial `var Version = "dev"` + ldflag `-X .Version=v1.2.3` → wire Version is `"v1.2.3"`
- Initial `var Version = "0.0.0"` + no ldflag → wire Version is `"0.0.0"` (NOT a build-info fallback; `"0.0.0"` is NOT in the SDK's reset set)

The `missing-ldflag` fixture uses `Version = "dev"` deliberately so it exercises
the `(devel)` fallback path, not the `"0.0.0"` pass-through.
MD
```

**Step 5: Commit**

```bash
git add cmd/wfctl/testdata/verify_capabilities/
git commit -m "test(wfctl): verify-capabilities fixtures (4 build-pass scenarios) (workflow#765)"
```

---

## Task 6: Create name-drift fixture (binary advertises different Name)

**Change class:** Test fixture.

**Files:**
- Create: `cmd/wfctl/testdata/verify_capabilities/name-drift/{plugin.json,main.go,go.mod,go.sum}`

**Step 1: Generate the fixture**

```bash
REPO_ROOT=$(git rev-parse --show-toplevel)
d=cmd/wfctl/testdata/verify_capabilities/name-drift
mkdir -p "$d"
cat > "$d/plugin.json" <<'JSON'
{
  "name": "verify-name-drift",
  "version": "0.0.0",
  "minEngineVersion": "v0.62.0",
  "author": "test fixture",
  "description": "verify-capabilities name-drift scenario"
}
JSON
cat > "$d/main.go" <<'GO'
package main

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

var Version = "dev"

type stubProvider struct{}

// Manifest intentionally returns a DIFFERENT name than plugin.json declares.
// plugin.json says "verify-name-drift"; runtime says "verify-name-drift-binary".
func (stubProvider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "verify-name-drift-binary",
		Version:     "0.0.0",
		Author:      "test fixture",
		Description: "verify-capabilities name-drift scenario",
	}
}

func main() {
	sdk.Serve(stubProvider{},
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)),
	)
}
GO
cat > "$d/go.mod" <<MOD
module github.com/test/name-drift

go 1.26.0

require github.com/GoCodeAlone/workflow v0.62.0

replace github.com/GoCodeAlone/workflow => $REPO_ROOT
MOD
(cd "$d" && GOWORK=off go mod tidy)
sed -i.bak "s|replace github.com/GoCodeAlone/workflow => .*|replace github.com/GoCodeAlone/workflow => ../../../../..|" "$d/go.mod"
rm -f "$d/go.mod.bak"
```

**Step 2: Verify fixture builds**

Run: `(cd cmd/wfctl/testdata/verify_capabilities/name-drift && GOWORK=off go build -mod=readonly -o /tmp/p .)`
Expected: exit 0.

**Step 3: Commit**

```bash
git add cmd/wfctl/testdata/verify_capabilities/name-drift/
git commit -m "test(wfctl): name-drift fixture (binary advertises mismatched Name) (workflow#765)"
```

---

## Task 7: Integration tests — 5 scenarios end-to-end

**Change class:** Plugin / extension (exercises spawn + RPC + diff against real fixture binaries).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Add the fixture-build helper + 5 test cases**

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
import (
	"os/exec"
	// keep existing imports
)

// buildFixtureBinaryForVerify builds the fixture scenario in-place and emits
// the binary to t.TempDir(). ldflag is the -X ...Version= value ("" = no flag,
// which makes ResolveBuildVersion fall back to "(devel) [@ sha]" for fixtures
// whose initial Version var is "dev").
func buildFixtureBinaryForVerify(t *testing.T, scenario, ldflagTag string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "p")
	args := []string{"build", "-mod=readonly"}
	if ldflagTag != "" {
		args = append(args, "-ldflags",
			fmt.Sprintf("-X github.com/test/%s.Version=%s", scenario, ldflagTag))
	}
	args = append(args, "-o", binPath, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("testdata", "verify_capabilities", scenario)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", scenario, err, out)
	}
	return binPath
}

func TestVerifyCapabilities_Good(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_ReleaseGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "release-good", "v1.2.3")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/release-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_MissingLdflag(t *testing.T) {
	// No ldflag → Version stays "dev" → ResolveBuildVersion("dev") → "(devel) [@ sha]"
	bin := buildFixtureBinaryForVerify(t, "missing-ldflag", "")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/missing-ldflag"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_VersionDrift(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "version-drift", "v0.9.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/version-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_NameDrift(t *testing.T) {
	// Use ldflag tag matching plugin.json sentinel so Version PASSes (matrix row "0.0.0 + v0.0.0" -> PASS via TrimPrefix);
	// Name is the ISOLATED failure under test. Without this, both name AND version mismatches fire and a regression
	// that breaks Name-diff while leaving Version-diff would silently pass through the lenient "mismatch" substring check.
	bin := buildFixtureBinaryForVerify(t, "name-drift", "v0.0.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/name-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	// Tighter assertion: error must specifically mention "name:" prefix from the diff report.
	if !strings.Contains(err.Error(), "name:") && !strings.Contains(fmt.Sprintf("%v", err), "name:") {
		t.Errorf("want name-mismatch error, got: %v", err)
	}
}
```

**Step 2: Run all integration tests**

Run: `cd cmd/wfctl && go test -run TestVerifyCapabilities -count=1 -timeout 120s ./...`
Expected: 5 scenario tests + 8 unit tests from Tasks 1-3 = 13 PASS.

**Step 3: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "test(wfctl): verify-capabilities integration tests (5 scenarios) (workflow#765)"
```

---

## Task 8: Documentation update — PLUGIN_RELEASE_GATES.md

**Change class:** Documentation.

**Files:**
- Modify: `docs/PLUGIN_RELEASE_GATES.md` (append Verify-Capabilities section)

**Step 1: Append section**

```bash
cat >> docs/PLUGIN_RELEASE_GATES.md <<'MD'

## Verify-Capabilities (workflow#765 — runtime truth-check)

`wfctl plugin verify-capabilities` is the runtime sibling of `validate-contract`:
it spawns the plugin binary, calls `PluginService.GetManifest`, and verifies
the returned `Name` + `Version` match `plugin.json`. Catches the
**ldflag-missing truth-loop bug**: a plugin can pass `validate-contract`
(static check) and still ship a binary whose `Manifest.Version` is the
SDK's `(devel) [@ sha]` sentinel because the goreleaser ldflag never fired.

### Synopsis

```
wfctl plugin verify-capabilities --binary <path> <plugin-dir>
```

`--binary` REQUIRED (no build-from-source — operator builds via goreleaser
or `go build`).

⚠ **Executes the binary** as a subprocess. Only run against artifacts you trust.

### Local development

```bash
go build -ldflags="-X github.com/GoCodeAlone/workflow-plugin-<name>/internal.Version=v1.2.3" \
  -o /tmp/p ./cmd/<name>
wfctl plugin verify-capabilities --binary /tmp/p .
```

### CI integration (release.yml post-goreleaser, pre-publish)

```yaml
- name: Verify capabilities (post-build runtime check)
  run: |
    RUNNER_ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    BIN=$(jq -r --arg arch "$RUNNER_ARCH" \
      '[.[] | select(.type=="Binary" and .goos=="linux" and .goarch==$arch)] | .[0].path // ""' \
      dist/artifacts.json)
    "${RUNNER_TEMP}/wfctl-bin/wfctl" plugin verify-capabilities --binary "$BIN" .
```

### Version diff matrix

| plugin.json `version` | binary `Manifest.Version` | Outcome |
|---|---|---|
| `"0.0.0"` (sentinel) | non-sentinel (`"v1.2.3"`) | PASS — CI artifact under verification |
| `"0.0.0"` | sentinel (`""`, `"dev"`, `"0.0.0"`, `"(devel)..."`) | FAIL — ldflag missing |
| `"X.Y.Z"` (release) | `"vX.Y.Z"` or `"X.Y.Z"` | PASS — normalize leading v |
| `"X.Y.Z"` | sentinel | FAIL — ldflag missing |
| `"X.Y.Z"` | anything else | FAIL — version drift |

### Non-goals

- Does NOT walk per-type RPCs (`GetModuleTypes`/`GetStepTypes`/`GetTriggerTypes`) — IaC bridge returns Unimplemented.
- Does NOT diff `GetContractRegistry` — deferred to workflow#766 (requires `capabilities.iacServices` schema first).
- Does NOT build the binary — operator's responsibility.
- Does NOT verify `minEngineVersion` at runtime (not on `pb.Manifest`).

See `docs/plans/2026-05-24-verify-capabilities-design.md` for full design.
MD
```

**Step 2: Verify no broken anchors**

Run (best-effort; skip if tool absent): `markdown-link-check docs/PLUGIN_RELEASE_GATES.md 2>&1 | head`
Expected: no broken links (or tool-missing — acceptable; visual review of the append).

**Step 3: Commit**

```bash
git add docs/PLUGIN_RELEASE_GATES.md
git commit -m "docs: add Verify-Capabilities section to PLUGIN_RELEASE_GATES (workflow#765)"
```

---

## Final verification (post-Task-8)

Before opening the PR:

```bash
# 1. All tests pass (unit + integration)
cd cmd/wfctl && go test -count=1 -timeout 120s ./...

# 2. Lint clean
go vet ./...
golangci-lint run ./cmd/wfctl/...

# 3. Help text correct
go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl plugin verify-capabilities --help
# Expected: help text contains "REQUIRED: --binary", "WARNING: this command EXECUTES", jq example

# 4. Conformance still works (no regression from inlined spawn — we did NOT touch conformance)
go test -run TestConformance -count=1 -timeout 300s ./cmd/wfctl/...

# 5. End-to-end smoke against a real plugin (out-of-tree)
#    cd /tmp && git clone --depth=1 git@github.com:GoCodeAlone/workflow-plugin-discord.git
#    cd workflow-plugin-discord
#    go build -ldflags="-X .../internal.Version=v0.1.1" -o /tmp/p ./cmd/workflow-plugin-discord
#    /tmp/wfctl plugin verify-capabilities --binary /tmp/p .
#    Expected: "OK    workflow-plugin-discord v0.1.1 (plugin.json: 0.1.1)"
```

## Rollback

This PR adds a CLI subcommand only — no shared-helper refactor, no schema migrations, no upstream consumer changes. Rollback path:
- `git revert <merge-sha>` removes the new subcommand and its tests + fixtures + doc append.
- No data migration, no schema change, no upstream consumer change.
- Backwards-compat: subcommand is purely additive; pre-PR wfctl callers continue to work.

Scaffold-template release.yml wiring is a separate follow-up PR on scaffold-workflow-plugin (not in this PR's scope).
