# wfctl plugin verify-capabilities Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `wfctl plugin verify-capabilities --binary <path> <plugin-dir>` subcommand that spawns a plugin binary, calls `PluginService.GetManifest`, and diffs `Name` + `Version` against plugin.json with sentinel-pattern matching to catch the ldflag-missing truth-loop bug from workflow#762/#764.

**Architecture:** New subcommand registered in `cmd/wfctl/plugin.go`. Extracts existing spawn-and-dial pattern from `plugin_conformance.go:462-504` into a shared helper (`spawnAndDial`) so verify-capabilities and conformance both use it. Diff logic is set-equal-on-Name + sentinel-aware-on-Version (see design doc §Version rule matrix). Fixtures use in-place build with `-mod=readonly` + checked-in `go.sum` + `GOWORK=off`.

**Tech Stack:** Go (workflow CLI), `goplugin` (go-plugin v1.7), `pb.PluginService` (gRPC), `external.Handshake`, `external.NewExternalPluginAdapter`.

**Base branch:** `main`

**Design doc:** `docs/plans/2026-05-24-verify-capabilities-design.md` (cycle-6 PASS adversarial).

**Issue:** workflow#765

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 9
**Estimated Lines of Change:** ~600 (helper + subcommand + tests + 5 fixtures + docs)

**Out of scope:**
- Contract-diff (`GetContractRegistry` walk) — deferred to follow-up issue #766; needs `capabilities.iacServices` schema on `PluginManifest` first
- Per-type RPC walk (`GetModuleTypes`/`GetStepTypes`/`GetTriggerTypes`) — IaC bridge returns Unimplemented for these
- Build-from-source mode — `--binary` REQUIRED; dev convenience builds documented in §Synopsis
- `--json` output mode — defer YAGNI
- Multi-binary repos — runs against the binary passed; multi-binary plugins invoke multiple times
- Author/Description/ConfigMutable/SampleCategory diff — display fields, not contract surface
- `minEngineVersion` runtime check — not on `pb.Manifest`; static-check responsibility
- Scaffold-template release.yml wiring — separate follow-up PR on scaffold-workflow-plugin after this lands

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): plugin verify-capabilities subcommand (workflow#765) | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7, Task 8, Task 9 | feat/765-verify-capabilities |

**Status:** Draft

---

## Task 1: Extract shared `spawnAndDial` helper from conformance

**Change class:** Internal logic refactor (existing functionality moves; behavior unchanged).

**Files:**
- Create: `cmd/wfctl/plugin_spawn.go`
- Modify: `cmd/wfctl/plugin_conformance.go:462-504` (replace inline spawn with `spawnAndDial` call; preserve lines 505-513 IaC-validation inline)
- Test: existing `cmd/wfctl/plugin_conformance_test.go` covers the refactored path; no new test needed for the move itself

**Step 1: Confirm conformance test baseline passes**

Run: `cd cmd/wfctl && go test -run TestConformance -count=1 ./...`
Expected: PASS (establishes baseline before refactor).

**Step 2: Create the shared helper**

Create `cmd/wfctl/plugin_spawn.go`:

```go
// Package main — shared spawn-and-dial helper for wfctl plugin subcommands
// that need to exec a plugin binary and obtain a *external.PluginAdapter.
//
// Extracted from plugin_conformance.go:462-504 per workflow#765 design doc
// §Files. Used by `plugin conformance` and `plugin verify-capabilities`.
// Plugin-type-agnostic; IaC-specific post-dial assertions stay in callers.
package main

import (
	"context"
	"fmt"
	"os/exec"

	external "github.com/GoCodeAlone/workflow/plugin/external"
	goplugin "github.com/GoCodeAlone/go-plugin"
	hclog "github.com/hashicorp/go-hclog"
)

// spawnedPlugin is the result of spawning a plugin binary and dialing its
// gRPC server. Cleanup MUST be called by the caller (typically via defer)
// to kill the spawned subprocess. stdoutTail and stderrTail are the captured
// tail buffers (useful for error reporting when spawn or dial fails).
type spawnedPlugin struct {
	Adapter    *external.PluginAdapter
	Cleanup    func()
	StdoutTail *tailBuffer
	StderrTail *tailBuffer
}

// spawnAndDial executes binaryPath as a go-plugin subprocess, dials its
// gRPC server with the canonical workflow ext handshake, dispenses the
// "plugin" interface, and wraps it in a *external.PluginAdapter.
//
// The returned Cleanup func wraps client.Kill — caller must `defer cleanup()`.
// If spawn or dial fails, Cleanup is non-nil but a no-op (still safe to defer).
//
// pluginName is informational (used for error messages + adapter naming).
// pluginDir is the working directory for the subprocess (typically the
// plugin's install directory; can be empty to inherit wfctl's cwd).
// env is the subprocess environment (typically conformancePluginEnv() —
// reuse from plugin_conformance.go for consistency).
func spawnAndDial(ctx context.Context, binaryPath, pluginName, pluginDir string, env []string) (*spawnedPlugin, error) {
	var stdout, stderr tailBuffer
	cmd := exec.CommandContext(ctx, binaryPath) //nolint:gosec // operator-supplied binary path.
	if pluginDir != "" {
		cmd.Dir = pluginDir
	}
	if env != nil {
		cmd.Env = env
	}
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

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout waiting for plugin handshake (stderr: %s)", stderr.String())
		}
		return nil, fmt.Errorf("plugin dial: %w (stderr: %s)", err, stderr.String())
	}
	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("dispense plugin: %w (stderr: %s)", err, stderr.String())
	}
	pluginClient, ok := raw.(*external.PluginClient)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("dispensed object is %T, want *external.PluginClient", raw)
	}
	adapter, err := external.NewExternalPluginAdapter(pluginName, pluginClient, nil)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("new adapter: %w", err)
	}
	return &spawnedPlugin{
		Adapter:    adapter,
		Cleanup:    client.Kill,
		StdoutTail: &stdout,
		StderrTail: &stderr,
	}, nil
}
```

**Step 3: Refactor `checkTypedIaCPlugin` to call the helper**

Edit `cmd/wfctl/plugin_conformance.go` — replace lines 462-504 (spawn+dial+dispense+adapter) with a single `spawnAndDial` call. KEEP lines 505-513 (IaC-validation: `ContractRegistryError`, `AssertIaCPluginAdvertisesRequiredService`, `registeredIaCServices`, `newTypedIaCAdapter`) inline after the call. Pattern:

```go
func checkTypedIaCPlugin(timeout time.Duration, pluginsDir, name string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pluginDir := filepath.Join(pluginsDir, name)
	binaryPath, err := filepath.Abs(filepath.Join(pluginDir, name))
	if err != nil {
		return "", "", err
	}
	sp, err := spawnAndDial(ctx, binaryPath, name, pluginDir, conformancePluginEnv())
	if err != nil {
		stdout := ""
		stderr := ""
		if sp != nil {
			if sp.StdoutTail != nil { stdout = sp.StdoutTail.String() }
			if sp.StderrTail != nil { stderr = sp.StderrTail.String() }
		}
		return stdout, stderr, err
	}
	defer sp.Cleanup()

	// IaC-specific post-dial validation (NOT in shared helper).
	if regErr := sp.Adapter.ContractRegistryError(); regErr != nil {
		return sp.StdoutTail.String(), sp.StderrTail.String(), regErr
	}
	if err := AssertIaCPluginAdvertisesRequiredService(name, "", sp.Adapter.ContractRegistry()); err != nil {
		return sp.StdoutTail.String(), sp.StderrTail.String(), err
	}
	registered := registeredIaCServices(sp.Adapter.ContractRegistry())
	typed := newTypedIaCAdapter(sp.Adapter.Conn(), registered)
	_ = typed.SupportedCanonicalKeys()
	return sp.StdoutTail.String(), sp.StderrTail.String(), nil
}
```

**Step 4: Build + run conformance tests**

Run: `cd cmd/wfctl && go build ./... && go test -run TestConformance -count=1 ./...`
Expected: build exit 0; tests PASS (behavior unchanged).

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_spawn.go cmd/wfctl/plugin_conformance.go
git commit -m "refactor(wfctl): extract spawnAndDial helper from conformance (workflow#765 prep)"
```

---

## Task 2: Subcommand registration + flag parsing skeleton

**Change class:** CLI command.

**Files:**
- Create: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin.go` (add `case "verify-capabilities"` + help line)

**Step 1: Write the failing test**

Create `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// TestVerifyCapabilitiesUsage asserts the subcommand prints usage when invoked
// with no args.
func TestVerifyCapabilitiesUsage(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{})
	if err == nil {
		t.Fatal("want error for missing args")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

// TestVerifyCapabilitiesRequiresBinary asserts --binary is required.
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

**Step 2: Run test to verify it fails**

Run: `cd cmd/wfctl && go test -run TestVerifyCapabilitiesUsage -count=1 ./...`
Expected: FAIL with `undefined: runPluginVerifyCapabilities`.

**Step 3: Write minimal implementation**

Create `cmd/wfctl/plugin_verify_capabilities.go`:

```go
// Package main — `wfctl plugin verify-capabilities` subcommand.
// Spawns a plugin binary, calls PluginService.GetManifest, diffs returned
// Manifest against plugin.json. Catches ldflag-missing truth-loop bug from
// workflow#762/#764.
//
// Design: docs/plans/2026-05-24-verify-capabilities-design.md
// Issue:  https://github.com/GoCodeAlone/workflow/issues/765
package main

import (
	"flag"
	"fmt"
	"os"
)

func runPluginVerifyCapabilities(args []string) error {
	fs := flag.NewFlagSet("plugin verify-capabilities", flag.ContinueOnError)
	binary := fs.String("binary", "", "Path to plugin binary (REQUIRED; see help for goreleaser dist/artifacts.json pattern)")
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

	// TODO: preflight + spawn + diff (Tasks 3-5)
	_ = pluginDir
	_ = os.Stat
	return fmt.Errorf("not yet implemented")
}
```

Edit `cmd/wfctl/plugin.go` — find the `case "validate-contract":` dispatcher block (line ~38) and add right after:

```go
	case "verify-capabilities":
		return runPluginVerifyCapabilities(args[1:])
```

Also find the help-text block in `plugin.go`'s usage() function and add (alphabetical order with other verbs):

```go
		fmt.Fprintln(out, "  verify-capabilities  Spawn plugin binary, verify runtime GetManifest matches plugin.json")
```

**Step 4: Run test to verify it passes**

Run: `cd cmd/wfctl && go build ./... && go test -run TestVerifyCapabilities -count=1 ./...`
Expected: build exit 0; both tests PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go cmd/wfctl/plugin.go
git commit -m "feat(wfctl): plugin verify-capabilities subcommand skeleton (workflow#765)"
```

---

## Task 3: Preflight binary path validation

**Change class:** Internal logic refactor (input validation).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the failing tests**

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
func TestPreflightBinaryEmpty(t *testing.T) {
	err := preflightBinary("")
	if err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want empty-path error, got %v", err)
	}
}

func TestPreflightBinaryNull(t *testing.T) {
	err := preflightBinary("null")
	if err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want null-path error (jq fallback), got %v", err)
	}
}

func TestPreflightBinaryMissing(t *testing.T) {
	err := preflightBinary("/nonexistent/missing-binary-xyz")
	if err == nil || !strings.Contains(err.Error(), "stat") {
		t.Errorf("want stat error, got %v", err)
	}
}

func TestPreflightBinaryDirectory(t *testing.T) {
	d := t.TempDir()
	err := preflightBinary(d)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("want directory error, got %v", err)
	}
}

func TestPreflightBinaryNonExecutable(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("not-exec"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := preflightBinary(f)
	if err == nil || !strings.Contains(err.Error(), "executable") {
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

Add to imports: `"os"`, `"path/filepath"`.

**Step 2: Run test to verify it fails**

Run: `cd cmd/wfctl && go test -run TestPreflightBinary -count=1 ./...`
Expected: FAIL with `undefined: preflightBinary`.

**Step 3: Implement**

Append to `cmd/wfctl/plugin_verify_capabilities.go`:

```go
// preflightBinary validates the --binary path before exec:
//   - non-empty + not literal "null" (guards against jq fallback returning empty)
//   - file exists and is a regular file (not directory or symlink-to-dir)
//   - has at least one executable bit set
//
// Design: §Synopsis preflight checks.
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

Update `runPluginVerifyCapabilities` to call it before the `return "not yet implemented"` line:

```go
	if err := preflightBinary(*binary); err != nil {
		return err
	}
```

**Step 4: Run test to verify it passes**

Run: `cd cmd/wfctl && go test -run TestPreflightBinary -count=1 ./...`
Expected: all 6 preflight tests PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities preflight binary-path validation (workflow#765)"
```

---

## Task 4: Sentinel-pattern Version diff matrix

**Change class:** Internal logic refactor (pure-logic diff function).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the failing tests** (table-driven)

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
func TestIsSentinel(t *testing.T) {
	cases := map[string]bool{
		"":                       true,
		"dev":                    true,
		"0.0.0":                  true,
		"(devel)":                true,
		"(devel) [@ a1b2c3d]":    true,
		"(devel) [@ a1b2c3d.dirty]": true,
		"v1.2.3":                 false,
		"1.2.3":                  false,
		"v0.0.1":                 false,
	}
	for v, want := range cases {
		if got := isSentinel(v); got != want {
			t.Errorf("isSentinel(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDiffVersion(t *testing.T) {
	// (declared, runtime, wantPass, wantReason-substring)
	cases := []struct {
		declared, runtime string
		wantPass          bool
		wantReason        string
	}{
		// Dev-sentinel row 1: 0.0.0 + non-sentinel -> PASS (CI artifact)
		{"0.0.0", "v1.2.3", true, ""},
		{"0.0.0", "0.1.0", true, ""},
		// Dev-sentinel row 2: 0.0.0 + sentinel -> FAIL (ldflag missing)
		{"0.0.0", "", false, "ldflag"},
		{"0.0.0", "(devel)", false, "ldflag"},
		{"0.0.0", "(devel) [@ abc1234]", false, "ldflag"},
		{"0.0.0", "dev", false, "ldflag"},
		{"0.0.0", "0.0.0", false, "ldflag"},
		// Release row 3: X.Y.Z + vX.Y.Z or X.Y.Z -> PASS (normalize leading v)
		{"1.2.3", "v1.2.3", true, ""},
		{"1.2.3", "1.2.3", true, ""},
		// Release row 4: X.Y.Z + sentinel -> FAIL
		{"1.2.3", "", false, "ldflag"},
		{"1.2.3", "(devel)", false, "ldflag"},
		{"1.2.3", "(devel) [@ deadbee]", false, "ldflag"},
		// Release row 5: X.Y.Z + drift -> FAIL
		{"1.2.3", "v0.9.0", false, "drift"},
		{"1.2.3", "v2.0.0", false, "drift"},
	}
	for _, c := range cases {
		pass, reason := diffVersion(c.declared, c.runtime)
		if pass != c.wantPass {
			t.Errorf("diffVersion(%q, %q) pass = %v, want %v (reason: %q)",
				c.declared, c.runtime, pass, c.wantPass, reason)
			continue
		}
		if !pass && !strings.Contains(reason, c.wantReason) {
			t.Errorf("diffVersion(%q, %q) reason = %q, want substring %q",
				c.declared, c.runtime, reason, c.wantReason)
		}
	}
}
```

**Step 2: Run tests to verify failure**

Run: `cd cmd/wfctl && go test -run "TestIsSentinel|TestDiffVersion" -count=1 ./...`
Expected: FAIL with `undefined: isSentinel`, `undefined: diffVersion`.

**Step 3: Implement**

Append to `cmd/wfctl/plugin_verify_capabilities.go`:

```go
import "strings"  // add to existing import block if absent

// isSentinel returns true when v is one of the SDK's dev-sentinel forms
// OR the on-disk plugin.json sentinel "0.0.0".
//
// SDK sentinel set (per plugin/external/sdk/buildversion.go:36-42):
//   "", "dev", "(devel)" — ResolveBuildVersion replaces these with build-info
// Plus build-info fallback produces "(devel) [@ <sha>[.dirty]]" — HasPrefix catches all forms.
// Plus on-disk plugin.json "0.0.0" sentinel (workflow#762 convention).
//
// The predicate MUST be a SUPERSET of the SDK's set; "dev" is defensive even
// though canonical wiring (sdk.ResolveBuildVersion) prevents literal "dev"
// from reaching the wire — included to catch non-canonical wiring accidents.
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
	// Normalize leading v on runtime side
	rNorm := strings.TrimPrefix(runtime, "v")
	if rNorm == declared {
		return true, ""
	}
	return false, fmt.Sprintf("version drift: plugin.json=%q; binary Manifest.Version=%q", declared, runtime)
}
```

**Step 4: Run tests to verify pass**

Run: `cd cmd/wfctl && go test -run "TestIsSentinel|TestDiffVersion" -count=1 ./...`
Expected: all matrix cases PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "feat(wfctl): verify-capabilities sentinel-pattern Version diff matrix (workflow#765)"
```

---

## Task 5: Wire spawn + GetManifest + diff into runPluginVerifyCapabilities

**Change class:** Plugin / extension (CLI subcommand that calls a plugin via gRPC).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities.go`

**Step 1: Write the failing integration test** (deferred to Task 7 fixture work — this task is the implementation wiring; test in Task 8)

For this task, the verification is `go build ./... && wfctl plugin verify-capabilities --help` showing the help text.

**Step 2: Read plugin.json + load PluginManifest**

Modify `runPluginVerifyCapabilities` — replace the `return fmt.Errorf("not yet implemented")` line with:

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

Add imports: `"encoding/json"`, `"path/filepath"`, `"github.com/GoCodeAlone/workflow/plugin"`.

**Step 3: Spawn + dial via shared helper + call GetManifest**

Continue in `runPluginVerifyCapabilities`:

```go
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	binAbs, err := filepath.Abs(*binary)
	if err != nil {
		return fmt.Errorf("resolve --binary %q: %w", *binary, err)
	}
	sp, err := spawnAndDial(ctx, binAbs, declared.Name, "", nil)
	if err != nil {
		return fmt.Errorf("spawn %s: %w", declared.Name, err)
	}
	defer sp.Cleanup()

	runtime, err := sp.Adapter.EngineManifest()
	if err != nil {
		return fmt.Errorf("GetManifest RPC: %w (stderr: %s)", err, sp.StderrTail.String())
	}
```

Note: `external.PluginAdapter.EngineManifest()` returns `(*pb.Manifest, error)` per `/tmp/wfprobe/plugin/external/adapter.go:367`. Verify with `grep -n "func.*EngineManifest" /tmp/wfprobe/plugin/external/adapter.go` if the method name has changed; substitute the exact accessor.

Add imports: `"context"`, `"time"`.

**Step 4: Diff Name + Version and report**

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
		return fmt.Errorf("verify-capabilities: %d mismatch(es)", len(failures))
	}
	fmt.Printf("OK    %s %s (plugin.json: %s)\n", declared.Name, runtime.GetVersion(), declared.Version)
	return nil
```

**Step 5: Build + help-output sanity check**

Run:
```bash
cd cmd/wfctl && go build -o /tmp/wfctl ./... && /tmp/wfctl plugin verify-capabilities --help
```
Expected: help text printed; exit 0. The help text contains "REQUIRED: --binary", "WARNING: this command EXECUTES", and the `jq` CI example.

**Step 6: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities.go
git commit -m "feat(wfctl): wire spawn + GetManifest + Name/Version diff (workflow#765)"
```

---

## Task 6: Create 5 fixture scenarios under testdata/verify_capabilities/

**Change class:** Documentation / fixture data (no runtime impact).

**Files:**
- Create: `cmd/wfctl/testdata/verify_capabilities/good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/release-good/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/missing-ldflag/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/version-drift/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/name-drift/{plugin.json,main.go,go.mod,go.sum}`
- Create: `cmd/wfctl/testdata/verify_capabilities/README.md` (maintenance note)

**Step 1: Write a generator script** (one-off; not committed)

Create `/tmp/gen-fixtures.sh` (not committed; just for generating):

```bash
#!/bin/bash
set -euo pipefail
BASE=cmd/wfctl/testdata/verify_capabilities
REPO_ROOT=$(git rev-parse --show-toplevel)
declare -A NAMES=(
  [good]=verify-good
  [release-good]=verify-release-good
  [missing-ldflag]=verify-missing-ldflag
  [version-drift]=verify-version-drift
  [name-drift]=verify-name-drift
)
declare -A VERS=(
  [good]=0.0.0
  [release-good]=1.2.3
  [missing-ldflag]=0.0.0
  [version-drift]=1.2.3
  [name-drift]=0.0.0
)
for s in good release-good missing-ldflag version-drift name-drift; do
  d="$BASE/$s"
  mkdir -p "$d"
  name="${NAMES[$s]}"
  version="${VERS[$s]}"
  cat > "$d/plugin.json" <<JSON
{
  "name": "$name",
  "version": "$version",
  "minEngineVersion": "v0.62.0",
  "type": "external",
  "author": "test fixture",
  "description": "verify-capabilities $s scenario",
  "capabilities": {
    "moduleTypes": ["fake.module"],
    "stepTypes": [],
    "triggerTypes": [],
    "workflowTypes": [],
    "wiringHooks": []
  }
}
JSON
  cat > "$d/main.go" <<GO
package main

import (
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/GoCodeAlone/workflow/plugin"
)

var Version = "0.0.0"

type stubProvider struct{}

func (stubProvider) Manifest() (*plugin.PluginManifest, error) {
	return &plugin.PluginManifest{
		Name:    "$name",
		Version: sdk.ResolveBuildVersion(Version),
	}, nil
}

func main() {
	sdk.Serve(stubProvider{}, sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)))
}
GO
  cat > "$d/go.mod" <<MOD
module github.com/test/$s

go 1.24

require github.com/GoCodeAlone/workflow v0.62.0

replace github.com/GoCodeAlone/workflow => $REPO_ROOT
MOD
done
```

Note: the `replace` directive uses an absolute path during generation, but we'll convert to relative `../../../../..` before committing for portability across machines.

**Step 2: Generate fixtures + verify go.sum + adjust replace to relative**

```bash
bash /tmp/gen-fixtures.sh
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go mod tidy)
  # rewrite replace from absolute to relative
  sed -i.bak "s|replace github.com/GoCodeAlone/workflow => .*|replace github.com/GoCodeAlone/workflow => ../../../../..|" "$d/go.mod"
  rm -f "$d/go.mod.bak"
done
```

Verify each fixture builds standalone:
```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go build -mod=readonly -o /tmp/p .) && echo "$d: ok" || echo "$d: FAIL"
done
```
Expected: all 5 print `ok`. (This catches go.sum drift before the integration tests run.)

**Step 3: Create the maintenance README**

```bash
cat > cmd/wfctl/testdata/verify_capabilities/README.md <<'MD'
# verify_capabilities test fixtures

Fixtures for `plugin_verify_capabilities_test.go` (workflow#765).

Each scenario directory contains a self-contained Go module that builds into
a minimal plugin binary. Tests call `go build -mod=readonly` in-place; the
binary is emitted to `t.TempDir()`.

## Maintenance

When the workflow SDK adds a new transitive dep that the fixtures' build
graph picks up, regenerate each fixture's `go.sum`:

```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go mod tidy)
done
git add cmd/wfctl/testdata/verify_capabilities/*/go.sum
```

The `replace github.com/GoCodeAlone/workflow => ../../../../..` directive
resolves 5-ups from each scenario directory to the workflow repo root.
DO NOT use an absolute path — it will diverge across developer machines.

## Scenarios

- `good/` — plugin.json version=0.0.0, ldflag injects v0.1.0 → PASS
- `release-good/` — plugin.json version=1.2.3, ldflag injects v1.2.3 → PASS
- `missing-ldflag/` — plugin.json version=0.0.0, no ldflag → FAIL
- `version-drift/` — plugin.json version=1.2.3, ldflag injects v0.9.0 → FAIL
- `name-drift/` — plugin.json name="foo", binary advertises Name="bar" → FAIL
MD
```

**Step 4: Verify all fixtures build (rerun)**

Run:
```bash
for d in cmd/wfctl/testdata/verify_capabilities/*/; do
  (cd "$d" && GOWORK=off go build -mod=readonly -o /tmp/p .) || exit 1
done
echo "all fixtures build"
```
Expected: `all fixtures build`.

**Step 5: Commit**

```bash
git add cmd/wfctl/testdata/verify_capabilities/
git commit -m "test(wfctl): verify-capabilities testdata fixtures (workflow#765)"
```

---

## Task 7: Special-case name-drift fixture (binary advertises different Name)

**Change class:** Internal logic refactor (fixture variant).

**Files:**
- Modify: `cmd/wfctl/testdata/verify_capabilities/name-drift/main.go`

**Step 1: Adjust the name-drift fixture's main.go**

Replace `cmd/wfctl/testdata/verify_capabilities/name-drift/main.go` so the binary advertises `Name="verify-name-drift-binary"` while plugin.json declares `name="verify-name-drift"`:

```go
package main

import (
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/GoCodeAlone/workflow/plugin"
)

var Version = "0.0.0"

type stubProvider struct{}

func (stubProvider) Manifest() (*plugin.PluginManifest, error) {
	return &plugin.PluginManifest{
		Name:    "verify-name-drift-binary", // intentional drift vs plugin.json "verify-name-drift"
		Version: sdk.ResolveBuildVersion(Version),
	}, nil
}

func main() {
	sdk.Serve(stubProvider{}, sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)))
}
```

**Step 2: Verify fixture still builds**

Run: `(cd cmd/wfctl/testdata/verify_capabilities/name-drift && GOWORK=off go build -mod=readonly -o /tmp/p .)`
Expected: exit 0.

**Step 3: Commit**

```bash
git add cmd/wfctl/testdata/verify_capabilities/name-drift/main.go
git commit -m "test(wfctl): name-drift fixture (binary advertises mismatched Name) (workflow#765)"
```

---

## Task 8: Integration tests — 5 scenarios end-to-end

**Change class:** Plugin / extension (exercise spawn + RPC + diff against real fixture binaries).

**Files:**
- Modify: `cmd/wfctl/plugin_verify_capabilities_test.go`

**Step 1: Write the integration test scaffolding**

Append to `cmd/wfctl/plugin_verify_capabilities_test.go`:

```go
import (
	"os/exec"
	// ...existing imports
)

// buildFixtureBinaryForVerify builds the fixture scenario in-place and emits
// the binary to t.TempDir(). ldflag is the -X ...Version= value (may be "").
// Returns the absolute path to the built binary.
//
// Pattern mirrors plugin_conformance_test.go:buildFixtureBinary but adapted
// for in-place build per verify-capabilities design (no copy-to-TempDir).
func buildFixtureBinaryForVerify(t *testing.T, scenario, ldflagTag string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "p")
	args := []string{"build", "-mod=readonly", "-o", binPath, "."}
	if ldflagTag != "" {
		args = append([]string{"build", "-mod=readonly",
			"-ldflags", fmt.Sprintf("-X github.com/test/%s.Version=%s", scenario, ldflagTag),
			"-o", binPath, "."}, args[4:]...)
		// Re-stitch to keep only the ldflags variant.
		args = []string{"build", "-mod=readonly",
			"-ldflags", fmt.Sprintf("-X github.com/test/%s.Version=%s", scenario, ldflagTag),
			"-o", binPath, "."}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("testdata", "verify_capabilities", scenario)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", scenario, err, out)
	}
	return binPath
}

func TestVerifyCapabilities_Good(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "good", "v0.1.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/good"})
	if err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_ReleaseGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "release-good", "v1.2.3")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/release-good"})
	if err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_MissingLdflag(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "missing-ldflag", "") // no ldflag → sentinel reaches wire
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/missing-ldflag"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_VersionDrift(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "version-drift", "v0.9.0") // declared 1.2.3, binary 0.9.0
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/version-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_NameDrift(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "name-drift", "v0.1.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/name-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}
```

**Step 2: Simplify buildFixtureBinaryForVerify** (the stitched logic above is muddled — rewrite cleanly):

Replace the helper with:

```go
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", scenario, err, out)
	}
	return binPath
}
```

**Step 3: Run all integration tests**

Run: `cd cmd/wfctl && go test -run TestVerifyCapabilities -count=1 -timeout 120s ./...`
Expected: all 5 scenario tests PASS (3 expect verify-success, 2 expect verify-failure).

**Step 4: Commit**

```bash
git add cmd/wfctl/plugin_verify_capabilities_test.go
git commit -m "test(wfctl): verify-capabilities integration tests (5 scenarios end-to-end) (workflow#765)"
```

---

## Task 9: Documentation update — PLUGIN_RELEASE_GATES.md

**Change class:** Documentation.

**Files:**
- Modify: `docs/PLUGIN_RELEASE_GATES.md` (append Verify-Capabilities section)

**Step 1: Read current doc**

```bash
wc -l docs/PLUGIN_RELEASE_GATES.md  # know the size before append
```

**Step 2: Append Verify-Capabilities section**

Append to `docs/PLUGIN_RELEASE_GATES.md`:

```markdown

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
- Does NOT diff `GetContractRegistry` — deferred to workflow#766 (requires `capabilities.iacServices` schema on PluginManifest first).
- Does NOT build the binary — operator's responsibility.
- Does NOT verify `minEngineVersion` at runtime (not on `pb.Manifest`).

See `docs/plans/2026-05-24-verify-capabilities-design.md` for full design.
```

**Step 3: Verify markdown renders without broken anchors**

Run: `markdown-link-check docs/PLUGIN_RELEASE_GATES.md 2>&1 | head` (skip if tool absent — eyeball check works for an append).

Expected: no broken links.

**Step 4: Commit**

```bash
git add docs/PLUGIN_RELEASE_GATES.md
git commit -m "docs: add Verify-Capabilities section to PLUGIN_RELEASE_GATES (workflow#765)"
```

---

## Final verification (post-Task-9)

Before opening the PR:

```bash
# 1. All tests pass
cd cmd/wfctl && go test -count=1 -timeout 120s ./...

# 2. Lint clean
cd cmd/wfctl && go vet ./...
golangci-lint run ./cmd/wfctl/...

# 3. Help text correct
go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl plugin verify-capabilities --help

# 4. Conformance still works (smoke — full conformance suite runs in CI)
go test -run TestConformance -count=1 -timeout 300s ./cmd/wfctl/...

# 5. End-to-end smoke against a real plugin (out-of-tree)
#    cd /tmp && git clone --depth=1 git@github.com:GoCodeAlone/workflow-plugin-discord.git
#    cd workflow-plugin-discord
#    go build -ldflags="-X .../internal.Version=v0.1.1" -o /tmp/p ./cmd/workflow-plugin-discord
#    /tmp/wfctl plugin verify-capabilities --binary /tmp/p .
#    Expected: "OK    workflow-plugin-discord v0.1.1 (plugin.json: 0.1.1)"
```

## Rollback

This PR adds a CLI subcommand + a shared helper refactor. Rollback path:
- `git revert <merge-sha>` removes the new subcommand and re-inlines the spawn logic in `plugin_conformance.go`.
- No data migration, no schema change, no upstream consumer change (scaffold-template wiring is a separate PR not included here).
- Backwards-compat: subcommand is purely additive; pre-PR wfctl callers continue to work.
