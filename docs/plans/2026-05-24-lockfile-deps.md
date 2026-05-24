# lockfile dep tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `wfctl plugin install` to write transitively-installed deps to `.wfctl-lock.yaml` (both legacy + new-format) AND always-track plain `install <name>` (drop `@version` gate), while preserving `installFromLockfile` / `installFromWfctlLockfile` no-clobber contracts via a chokepoint guard inside `updateLockfileWithChecksum`.

**Architecture:** Single PR. Add `mergeIntoNewFormatLockfile` helper + fan-out write inside refactored `updateLockfileWithChecksum`. Add package-level `installSkipLockfileUpdate` flag checked at TOP of `updateLockfileWithChecksum` (single chokepoint). Set+defer-clear the flag in both outer-frame installers (`installFromLockfile`, `installFromWfctlLockfile`) around their inner `runPluginInstall`/`installFromURL` calls. Drop the `name@version` gate. Extend `resolveDependencies` and `installPluginReqDirect` to call the helper.

**Tech Stack:** Go (wfctl), `config.WfctlLockfile` / `config.SaveWfctlLockfile` for new-format, existing `loadPluginLockfile` for legacy.

**Base branch:** `main`

**Design doc:** `docs/plans/2026-05-24-lockfile-deps-design.md` (cycle 5 PASS adversarial).

**Issue:** workflow#771

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~250

**Out of scope:**
- `wfctl plugin remove` lockfile cleanup
- `Repository` fallback URL construction for empty manifest entries
- Platforms-data backfill for newly-tracked deps
- `--no-lock` user-facing opt-out flag
- Concurrent-wfctl lockfile race handling (pre-existing)
- Constraint-metadata persistence on dep entries
- Lockfile schema migration (additive only)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(wfctl): lockfile dep tracking + always-track plain installs (workflow#771) | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | feat/771-lockfile-deps |

**Status:** Draft

## Reviewer override (plan cycle 1 → cycle 2)

Cycle-1 plan-phase adversarial reviewer flagged 2 Critical:

- **C1 (Tasks 4+5 redundant due to line-846 chokepoint)**: OVERRIDDEN as factually incorrect. Line 846 is inside `installFromURL` (lines 760+), NOT `installPluginFromManifest` (lines 325-431). `installPluginFromManifest` body verified: ends at line 431 with `commitPluginStagingDir` + `Printf("Installed ...")` — NO `updateLockfileWithChecksum` call. `resolveDependencies:268` and `installPluginReqDirect:111` both call `installPluginFromManifest` directly (not via `installFromURL`), so neither triggers any lockfile write today. Tasks 4 + 5 ARE necessary. Override documented per reviewer's "Options" rubric.
- **C2 (fake-TDD: tests call helper directly, not changed entrypoint)**: ACCEPTED. Cycle 2 rewrites each task's Step-1 test to invoke the changed production entrypoint via httptest server pattern (precedent at `plugin_install_lockfile_test.go:568+`).

Plus cycle-2 fixes Important: anon-func explicit in Task 3, parallel-test warning on global state, variable-name pre-resolution in Task 5, wrong Task-2 Step-4 commentary deleted.

---

### Task 1: Add `installSkipLockfileUpdate` chokepoint guard + `mergeIntoNewFormatLockfile` helper

**Change class:** Internal logic refactor.

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` — add package-level `var installSkipLockfileUpdate bool`.
- Modify: `cmd/wfctl/plugin_lockfile.go` — add `mergeIntoNewFormatLockfile` + insert chokepoint guard at top of `updateLockfileWithChecksum`; remove existing `LoadWfctlLockfile().Version > 0` early-return; fan-out to both formats.
- Test: `cmd/wfctl/plugin_install_lockfile_test.go` — add tests for chokepoint guard + new-format write.

**Step 1: Write the failing tests**

In `cmd/wfctl/plugin_install_lockfile_test.go` (edit existing SINGLE import block; do NOT add a second `import (...)`):

```go
func TestUpdateLockfileWithChecksum_GuardSkips(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkipLockfileUpdate = true
	defer func() { installSkipLockfileUpdate = false }()
	updateLockfileWithChecksum("foo", "1.0.0", "", "", "")
	b, _ := os.ReadFile(wfctlLockPath)
	if strings.Contains(string(b), "foo") {
		t.Errorf("guard should have suppressed write; got: %s", b)
	}
}

func TestUpdateLockfileWithChecksum_NewFormatFanOut(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins:\n  bar:\n    version: 0.1.0\n    source: github.com/x/bar\n    platforms:\n      linux_amd64:\n        url: https://example.com/bar\n        sha256: deadbeef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updateLockfileWithChecksum("bar", "1.2.3", "github.com/x/bar-new", "", "feedface")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := lf.Plugins["bar"]
	if got.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", got.Version)
	}
	if got.Source != "github.com/x/bar-new" {
		t.Errorf("Source = %q, want github.com/x/bar-new", got.Source)
	}
	if len(got.Platforms) == 0 {
		t.Errorf("Platforms should be preserved; got empty")
	}
	if got.Platforms["linux_amd64"].URL != "https://example.com/bar" {
		t.Errorf("Platforms URL clobbered: %v", got.Platforms)
	}
}
```

**Step 2: Run tests — verify FAIL**

Run: `GOWORK=off go test -run "TestUpdateLockfileWithChecksum_GuardSkips|TestUpdateLockfileWithChecksum_NewFormatFanOut" -count=1 ./cmd/wfctl/...`
Expected: FAIL — `installSkipLockfileUpdate undefined` AND new-format write doesn't happen.

**Step 3: Implement**

In `cmd/wfctl/plugin_install.go` (add after existing package-level vars near top of file):

```go
// installSkipLockfileUpdate suppresses ALL lockfile writes when set. Outer
// installers (installFromLockfile / installFromWfctlLockfile) hold the
// lockfile in memory and re-save it themselves; without this guard, inner
// install paths' lockfile writes would be silently overwritten by the
// outer re-save (workflow#771 cycle-5 chokepoint pattern).
//
// NOTE: package-level state. Tests touching this MUST NOT call t.Parallel() —
// cross-test flag leakage would silently break lockfile invariants. See
// design doc §"Top 3 doubts #2" for rationale on rejecting context.Context
// threading.
var installSkipLockfileUpdate bool
```

In `cmd/wfctl/plugin_lockfile.go`, add `mergeIntoNewFormatLockfile` helper:

In `cmd/wfctl/plugin_lockfile.go`, add `mergeIntoNewFormatLockfile` helper. Cycle-4 fix per reviewer CYC3-C2: helper handles BOTH key forms (real-world lockfiles key entries by long-form `workflow-plugin-auth` while runPluginInstall normalizes to `auth`). Returns bool so caller knows whether v1 path fired:

```go
// mergeIntoNewFormatLockfile updates the new-format .wfctl-lock.yaml's
// Plugins[name] entry, preserving Platforms / Compatibility data while
// refreshing Version + Source. Returns true iff lockfile is v1 format
// (so caller can skip the legacy write path).
//
// name passed in normalized form. Helper handles existing entries keyed
// by long-form (e.g. "workflow-plugin-auth") by scanning for any key
// matching normalizePluginName.
func mergeIntoNewFormatLockfile(name, version, source string) bool {
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil || lf == nil || lf.Version == 0 {
		return false
	}
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]config.WfctlLockPluginEntry)
	}
	// Lookup: exact match first, else scan for normalized-equivalent key.
	key := name
	if _, ok := lf.Plugins[name]; !ok {
		for existingKey := range lf.Plugins {
			if normalizePluginName(existingKey) == name {
				key = existingKey
				break
			}
		}
	}
	existing := lf.Plugins[key]
	existing.Version = version
	if source != "" {
		existing.Source = source
	}
	lf.Plugins[key] = existing
	_ = config.SaveWfctlLockfile(wfctlLockPath, lf)
	return true
}
```

Then refactor `updateLockfileWithChecksum` with chokepoint guard + MUTUALLY-EXCLUSIVE format paths (cycle-4 fix per CYC3-C1: `PluginLockEntry` has no `Platforms` field, so the legacy `Save()` re-marshals over the v1 file's plugins block destroying Platforms; protect by returning if v1 helper handled it):

```go
func updateLockfileWithChecksum(pluginName, version, repository, registry, sha256Hash string) {
	if installSkipLockfileUpdate {
		return
	}
	// V1 format takes precedence: if .wfctl-lock.yaml exists with version: 1,
	// write ONLY to that path (preserves Platforms). Skip legacy save entirely.
	if mergeIntoNewFormatLockfile(pluginName, version, repository) {
		return
	}
	// Legacy fallback: no v1 lockfile present; write to legacy .wfctl.yaml plugins block.
	lf, err := loadPluginLockfile(wfctlLockPath)
	if err != nil {
		return
	}
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]PluginLockEntry)
	}
	lf.Plugins[pluginName] = PluginLockEntry{
		Version:    version,
		Repository: repository,
		Registry:   registry,
		SHA256:     sha256Hash,
	}
	_ = lf.Save(wfctlLockPath)
}
```

Add `"github.com/GoCodeAlone/workflow/config"` to the existing single import block in `plugin_lockfile.go` if not already present.

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run "TestUpdateLockfileWithChecksum_GuardSkips|TestUpdateLockfileWithChecksum_NewFormatFanOut" -count=1 ./cmd/wfctl/...`
Expected: both PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_lockfile.go cmd/wfctl/plugin_install_lockfile_test.go
git commit -m "feat(wfctl): chokepoint guard + new-format fan-out in updateLockfileWithChecksum (workflow#771 Task 1)"
```

**Rollback:** revert this commit — `updateLockfileWithChecksum` returns to legacy-only behavior with prior early-return guard. No data migration.

---

### Task 2: Drop `name@version` gate in `runPluginInstall`

**Change class:** Internal logic refactor (gate removal).

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` lines 255-266 — remove the `if _, ver := parseNameVersion(nameArg); ver != ""` wrapper around lockfile update; always call the helper.
- Test: `cmd/wfctl/plugin_install_lockfile_test.go` — add test for plain-name install tracking.

**Step 1: Write the failing test**

Append:

```go
func TestRunPluginInstall_NoVersionTracksLockfile(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Directly invoke updateLockfileWithChecksum with the resolved manifest
	// version to simulate the post-gate-removal behavior the production code
	// will produce: plain `install <name>` resolves a version then writes
	// it unconditionally.
	updateLockfileWithChecksum("baz", "2.0.0", "github.com/x/baz", "", "abc123")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["baz"].Version != "2.0.0" {
		t.Errorf("plain install should track; got %v", lf.Plugins["baz"])
	}
}
```

**Step 1.5 (cycle-2 fix per reviewer C2): rewrite to invoke `runPluginInstall` via httptest pattern**

The Step-1 test above directly invokes the helper which won't catch the gate's presence. Replace with an end-to-end test using the existing `httptest.NewServer` pattern (precedent at `plugin_install_lockfile_test.go:568+`):

```go
func TestRunPluginInstall_NoVersionTracksLockfile(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	// New-format empty lockfile so the chokepoint fan-out fires.
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil { t.Fatal(err) }

	// Reuse the existing httptest pattern — see TestRunPluginInstall_DoesNotRewriteNewFormatLockfile (the only end-to-end httptest precedent in this file at line 548)
	// at line 38 of this file. Serve a minimal plugin tarball + manifest; call
	// runPluginInstall with PLAIN name (no @version). Assert lockfile entry
	// appears post-install.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// ... mirror existing test's manifest+tarball serving logic
	}))
	defer srv.Close()
	// ... call runPluginInstall([]string{"--plugin-dir", dir, "--source", srv.URL, "baz"})
	// Then assert lf.Plugins["baz"].Version == manifest.Version
}
```

**Implementer note**: use existing `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile (the only end-to-end httptest precedent in this file at line 548)` (line 38) as the literal template — copy its httptest setup, change `baz@1.0.0` → bare `baz` in the install args, and remove the `@version` parse assertion. The gate's presence will cause the lockfile NOT to be written → test FAILs → drop gate → test PASSes.

Run: `GOWORK=off go test -run TestRunPluginInstall_NoVersionTracksLockfile -count=1 ./cmd/wfctl/...`
Expected: FAIL with "plugins.baz not in lockfile" UNTIL Step 3's gate removal.

**Step 3: Remove the gate**

In `cmd/wfctl/plugin_install.go` lines 255-266, replace:

```go
// Update .wfctl-lock.yaml lockfile if name@version was provided.
if _, ver := parseNameVersion(nameArg); ver != "" {
	pluginName = normalizePluginName(pluginName)
	binaryChecksum := ""
	binaryPath := filepath.Join(pluginDirVal, pluginName, pluginName)
	if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
		binaryChecksum = cs
	} else {
		fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
	}
	updateLockfileWithChecksum(pluginName, manifest.Version, manifest.Repository, sourceName, binaryChecksum)
}
```

with:

```go
// Update .wfctl-lock.yaml lockfile (workflow#771: always-track, gate removed).
// The chokepoint guard inside updateLockfileWithChecksum (Task 1) is responsible
// for suppressing writes during outer-frame installers.
pluginName = normalizePluginName(pluginName)
binaryChecksum := ""
binaryPath := filepath.Join(pluginDirVal, pluginName, pluginName)
if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
	binaryChecksum = cs
} else {
	fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
}
updateLockfileWithChecksum(pluginName, manifest.Version, manifest.Repository, sourceName, binaryChecksum)
```

**Step 4: Run all lockfile tests — verify PASS**

Run: `GOWORK=off go test -run "TestUpdateLockfileWithChecksum|TestRunPluginInstall" -count=1 ./cmd/wfctl/...`
Expected: all PASS including the existing `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile` (must still pass because the test's setup may rely on the gate; if it fails, the test invariant is now covered by the Task 3 guard instead).

**Cycle-2 correction** (per reviewer C3 misread): the existing `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile` will continue to PASS even without Task 3's guards, NOT because of guards but because `mergeIntoNewFormatLockfile` (Task 1) preserves `Platforms` and never touches top-level `SHA256` field. The test's assertions about `entry.SHA256 == ""` and `platform.URL == "https://example.test/original.tar.gz"` and `platform.SHA256 == "archive-sha-from-lock"` ALL survive the fan-out because the helper is intentionally non-clobbering. No test update needed in this task.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_lockfile_test.go
git commit -m "feat(wfctl): always-track plain install in lockfile (drop @version gate) (workflow#771 Task 2)"
```

**Rollback:** revert this commit — gate restored; plain installs no longer track.

---

### Task 3: Set guard in outer-frame installers (`installFromLockfile` + `installFromWfctlLockfile`)

**Change class:** Internal logic refactor (preserves no-clobber contracts).

**Files:**
- Modify: `cmd/wfctl/plugin_lockfile.go` line ~115 (`installFromLockfile`) — set+defer-clear `installSkipLockfileUpdate` around the inner `runPluginInstall` call.
- Modify: `cmd/wfctl/plugin_install_wfctllock.go` — set+defer-clear `installSkipLockfileUpdate` around BOTH the per-arch `installFromURL` call (line ~86) AND the fallback `runPluginInstall` call (line ~99).
- Test: `cmd/wfctl/plugin_install_lockfile_test.go` — add regression tests for both no-clobber contracts.

**Step 1: Write the failing tests**

Append:

```go
func TestInstallFromLockfile_NoClobberInvariant(t *testing.T) {
	// Test that when installFromLockfile is active, inner runPluginInstall
	// does NOT mutate the on-disk lockfile (would clobber pinned entries
	// before checksum verification).
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins:\n  pinned:\n    version: 1.0.0\n    source: github.com/x/pinned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkipLockfileUpdate = true
	defer func() { installSkipLockfileUpdate = false }()
	// Simulate inner install attempting to write a different version
	updateLockfileWithChecksum("pinned", "9.9.9", "github.com/x/pinned-bad", "", "badchecksum")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["pinned"].Version != "1.0.0" {
		t.Errorf("inner install clobbered pinned entry; got %s, want 1.0.0", lf.Plugins["pinned"].Version)
	}
}
```

**Step 1.5 (cycle-2 fix per reviewer C2): rewrite to invoke `installFromLockfile` via httptest**

The Step-1 test sets the guard manually without exercising production wiring. Replace with full-flow test that invokes `installFromLockfile` against a legacy-format `.wfctl.yaml` with a `plugins:` block; ensure inner install attempts run but the on-disk pinned entry is preserved (regression catches missing/misplaced guard at outer-frame).

Reuse precedent at `plugin_install_lockfile_test.go:38+` for httptest setup; substitute `runPluginInstall` invocation with `installFromLockfile(...)` and assert the on-disk `lf.Plugins["pinned"].Version` matches the ORIGINAL pin, not the post-install version.

Run: `GOWORK=off go test -run TestInstallFromLockfile_NoClobberInvariant -count=1 ./cmd/wfctl/...`
Expected: FAIL (without Step-3 guard, inner install would clobber pinned entry).

**Step 3: Wire the guard in outer-frame installers**

In `cmd/wfctl/plugin_lockfile.go`, find `installFromLockfile` (around line 89). Cycle-4 fix per CYC3-I2: place guard at FUNCTION SCOPE (top of function, before the for-loop opens at line 106), NOT inside the loop body — mirrors the same fix applied to `installFromWfctlLockfile`. Single set+defer pair:

```go
// At the TOP of installFromLockfile (after the v1 branch's early-return at line 92,
// before the legacy loop opens at line 106):
installSkipLockfileUpdate = true
defer func() { installSkipLockfileUpdate = false }()
```

In `cmd/wfctl/plugin_install_wfctllock.go`, find `installFromWfctlLockfile` (around line 27). At the TOP of the per-plugin loop body (before line 86's `installFromURL` call and before the line-99 fallback `runPluginInstall` call), add a function-scope guard:

```go
// Note (workflow#771): function-scope guard suppresses lockfile writes by
// inner install paths. Deps installed via the fallback runPluginInstall
// inherit this guard and are NOT auto-pinned to the lockfile. Users must
// explicitly install deps to add them — matches the "lockfile is what the
// user explicitly pinned" contract.
```

```go
installSkipLockfileUpdate = true
// Cleared at loop iteration end via defer; for safety, we use an inline
// reset on each iteration since defer-in-loop would only fire at function exit.
```

**Cycle-3 fix per reviewer CYC2-C2**: anon-func-per-iter wrapper is illegal because `installFromWfctlLockfile`'s loop body has 6× `continue` statements (lines 62, 68, 79, 89, 94, 108 of `plugin_install_wfctllock.go`) — `continue` inside an anonymous function literal is a compile error ("continue is not in a loop"). Use function-scope guard instead:

```go
// At the TOP of installFromWfctlLockfile (line ~28), BEFORE the for-loop:
installSkipLockfileUpdate = true
defer func() { installSkipLockfileUpdate = false }()
// ... rest of function (loop with continues intact)
```

This protects the entire function call (including ALL per-plugin per-arch and per-plugin fallback runPluginInstall calls). Single set+defer pair at function scope; no per-iteration churn. Acceptable because `installFromWfctlLockfile` is itself an outer-frame installer — nothing nested calls it recursively, so the flag stays correctly scoped for the full lifetime of the function.

**Step 4: Run all install-related tests — verify PASS**

Run: `GOWORK=off go test -run "TestInstallFromLockfile|TestInstallFromWfctlLockfile|TestRunPluginInstall|TestUpdateLockfileWithChecksum" -count=1 -timeout 120s ./cmd/wfctl/...`
Expected: all PASS including the existing `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile` (now covered by Task 3 guards instead of the dropped @version gate).

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_lockfile.go cmd/wfctl/plugin_install_wfctllock.go cmd/wfctl/plugin_install_lockfile_test.go
git commit -m "feat(wfctl): set installSkipLockfileUpdate in outer-frame installers (workflow#771 Task 3)"
```

**Rollback:** revert this commit — outer-frame guards removed; inner installs from lockfile resume risking on-disk clobber (regression to pre-Task-2 state but with Task 2's gate removed → real lockfile mutation possible). Task 3 MUST land with Task 2 for no-clobber contract preservation.

---

### Task 4: Track transitive deps in `resolveDependencies`

**Change class:** Internal logic refactor.

**Files:**
- Modify: `cmd/wfctl/plugin_deps.go:268-273` — append hash + updateLockfileWithChecksum after successful dep install.
- Test: `cmd/wfctl/plugin_deps_test.go` — assert dep entries appear in lockfile post-install.

**Step 1: Write the failing test**

Append to `cmd/wfctl/plugin_deps_test.go` (edit existing single import block; do NOT add a second):

```go
func TestResolveDependencies_TracksDepsInLockfile(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Directly invoke updateLockfileWithChecksum to simulate dep install path.
	// (Full resolveDependencies test infrastructure already exists; this verifies
	// the chokepoint helper is reachable from the dep recursion site post-Task-4.)
	updateLockfileWithChecksum("depA", "0.5.0", "github.com/x/depA", "", "depAsha")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["depA"].Version != "0.5.0" {
		t.Errorf("dep not tracked; got %v", lf.Plugins["depA"])
	}
}
```

**Step 1.5 (cycle-2 fix per reviewer C2): rewrite to invoke `resolveDependencies` end-to-end**

Replace direct-helper-call with a real `resolveDependencies` invocation using the existing `plugin_deps_test.go:170` pattern (which already wires `manifest`, `pluginDir`, `cfgFile`, `resolved` map). Add post-call assertion that `lf.Plugins[<dep.Name>].Version` is set.

Run: `GOWORK=off go test -run TestResolveDependencies_TracksDepsInLockfile -count=1 ./cmd/wfctl/...`
Expected: FAIL — dep tracking line is not yet added at Step 3. After Step 3's append, expected PASS.

**Step 3: Wire dep tracking in `resolveDependencies`**

In `cmd/wfctl/plugin_deps.go` after the existing `resolved[dep.Name] = depManifest.Version` (around line 271), append:

```go
		// Track dep in lockfile (workflow#771 Task 4). The chokepoint guard
		// inside updateLockfileWithChecksum (Task 1) suppresses writes when
		// running under an outer-frame installer (installFromLockfile etc.).
		// Cycle-4 reviewer I1 Option-(b): use raw dep.Name for ALL three sites
		// (install dir, hash path, lockfile key) — matches the un-normalized
		// install at line 268 (`installPluginFromManifest(pluginDir, dep.Name, ...)`).
		// Normalizing only the lockfile-side without changing install-side
		// produces hash-MISS warnings + empty checksums for long-form dep names.
		// Parent (Task 5) keeps normalize because runPluginInstall:257 normalizes
		// install-side too — symmetric for that path. Asymmetric across Task 4 vs 5
		// is a pre-existing convention difference, not regressed by this PR.
		depBinaryPath := filepath.Join(pluginDir, dep.Name, dep.Name)
		depChecksum := ""
		if cs, hashErr := hashFileSHA256(depBinaryPath); hashErr == nil {
			depChecksum = cs
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not hash dep binary %s: %v (lockfile will have no checksum)\n", depBinaryPath, hashErr)
		}
		updateLockfileWithChecksum(dep.Name, depManifest.Version, depManifest.Repository, "", depChecksum)
```

Add `"path/filepath"` and `"os"` to the existing single import block in `plugin_deps.go` if not already present.

**Step 4: Run dep tests — verify PASS**

Run: `GOWORK=off go test -run "TestResolveDependencies" -count=1 -timeout 60s ./cmd/wfctl/...`
Expected: existing dep tests PASS + new lockfile assertion PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_deps.go cmd/wfctl/plugin_deps_test.go
git commit -m "feat(wfctl): track transitive deps in lockfile via resolveDependencies (workflow#771 Task 4)"
```

**Rollback:** revert this commit — deps stop being tracked. Parent + plain-install tracking from Tasks 1-3 unaffected.

---

### Task 5: Track parent in `installPluginReqDirect` (`--from-config` path)

**Change class:** Internal logic refactor.

**Files:**
- Modify: `cmd/wfctl/plugin_deps.go` `installPluginReqDirect` function (around line 82-112) — append `updateLockfileWithChecksum` after successful install.
- Test: `cmd/wfctl/plugin_deps_test.go` — assert parent entry appears for `--from-config` install path.

**Step 1: Write the failing test**

Append:

```go
func TestInstallPluginReqDirect_TracksParentInLockfile(t *testing.T) {
	dir := t.TempDir()
	// wfctlLockPath is a const (cmd/wfctl/plugin_lockfile.go:20); existing tests
	// redirect via os.Chdir + relative-path semantics (precedent at
	// plugin_install_lockfile_test.go:30, :95, :160, etc).
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.WriteFile(".wfctl-lock.yaml", []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Simulate the post-Task-5 write the production code will produce.
	updateLockfileWithChecksum("from-config-parent", "1.5.0", "github.com/x/parent", "", "sha")
	lf, err := config.LoadWfctlLockfile(".wfctl-lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["from-config-parent"].Version != "1.5.0" {
		t.Errorf("from-config parent not tracked; got %v", lf.Plugins["from-config-parent"])
	}
}
```

**Step 1.5 (cycle-2 fix per reviewer C2): rewrite to invoke `installPluginReqDirect` end-to-end**

Use the existing `installPluginReqDirect` test pattern (precedent: similar tests at plugin_deps_test.go) to invoke the function directly with a `config.PluginRequirement`. Assert lockfile entry appears post-call.

Run: `GOWORK=off go test -run TestInstallPluginReqDirect_TracksParentInLockfile -count=1 ./cmd/wfctl/...`
Expected: FAIL — `installPluginReqDirect` doesn't write lockfile yet; PASS after Step 3.

**Step 3: Wire the call in `installPluginReqDirect`**

In `cmd/wfctl/plugin_deps.go` `installPluginReqDirect` function, AFTER the successful `installPluginFromManifest` call (around line 111, before the function `return nil`), append:

```go
	// Track parent in lockfile (workflow#771 Task 5). Closes the asymmetry
	// where --from-config dep installs were tracked via Task 4 but parent
	// installs via this path were not.
	// Cycle-3 fix per reviewer CYC2-I1: use the function's NORMALIZED pluginName
	// (already computed at line 87 via normalizePluginName(rawName)), NOT raw
	// req.Name. installPluginFromManifest installs at pluginDir/<normalized>/<normalized>;
	// raw req.Name "workflow-plugin-auth" would hash-miss at pluginDir/workflow-plugin-auth/...
	// and produce an asymmetric lockfile key vs runPluginInstall's writes.
	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	checksum := ""
	if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
		checksum = cs
	} else {
		fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
	}
	updateLockfileWithChecksum(pluginName, manifest.Version, manifest.Repository, "", checksum)
```

**Cycle-3 pre-resolved**: verified against `cmd/wfctl/plugin_deps.go:82-95`:
- Signature: `func installPluginReqDirect(pluginDir, registryCfgPath string, req config.PluginRequirement) error`.
- Line 87: `pluginName := normalizePluginName(rawName)` — use THIS for both the hash path AND the lockfile key (not raw `req.Name`).
- `manifest` is the local var at line 95.

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test -run "TestInstallPluginReqDirect" -count=1 -timeout 60s ./cmd/wfctl/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_deps.go cmd/wfctl/plugin_deps_test.go
git commit -m "feat(wfctl): track --from-config parent in lockfile via installPluginReqDirect (workflow#771 Task 5)"
```

**Rollback:** revert this commit — `--from-config` parent tracking gap returns; deps via Task 4 still tracked.

---

### Task 6: Final verification + regression sweep

**Change class:** Internal logic refactor (test-only).

**Files:**
- Test: `cmd/wfctl/plugin_install_lockfile_test.go` — extend coverage if any gap surfaces.

**Step 1: Run full lockfile + install test suite**

Run: `GOWORK=off go test -run "TestUpdateLockfileWithChecksum|TestRunPluginInstall|TestInstallFromLockfile|TestInstallFromWfctlLockfile|TestResolveDependencies|TestInstallPluginReqDirect" -count=1 -timeout 180s ./cmd/wfctl/...`
Expected: all PASS, including existing `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile` (now covered by Task 3 guard).

**Step 2: Run `go vet` + lint**

Run: `GOWORK=off go vet ./...` and `GOWORK=off golangci-lint run ./cmd/wfctl/...`
Expected: no warnings.

**Step 3: Run full wfctl test suite — regression check**

Run: `GOWORK=off go test -count=1 -timeout 300s ./cmd/wfctl/...`
Expected: all PASS.

**Step 4: Commit any gap-fill tests**

If Step 1-3 surfaces a gap (e.g., a test that relied on the now-dropped @version gate semantics), add coverage + commit:

```bash
git add cmd/wfctl/plugin_install_lockfile_test.go
git commit -m "test(wfctl): regression coverage for lockfile dep tracking (workflow#771 Task 6)"
```

If no gap surfaces, this task produces no commit (acknowledge in the Task-6 commit message footer of Task 5 or skip silently).

**Rollback:** N/A (test-only).

---

## Final verification (post-Task-6)

Before opening the PR:

```bash
# 1. All tests pass
GOWORK=off go test -count=1 -timeout 300s ./cmd/wfctl/...

# 2. Lint clean
GOWORK=off go vet ./...
GOWORK=off golangci-lint run

# 3. wfctl --help still works
GOWORK=off go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl plugin install --help

# 4. End-to-end smoke (out-of-tree)
#    Install a plugin with deps, verify lockfile has both entries:
#    /tmp/wfctl plugin install workflow-plugin-something
#    grep -E "^  (something|dep-of-something):" .wfctl-lock.yaml
```

## Rollback

This PR touches lockfile-write behavior across multiple call sites. Rollback path:

- `git revert <merge-sha>` reverts all 6 commits cleanly. Existing `.wfctl-lock.yaml` files with dep entries continue to parse (additive entries; no schema change).
- **Lockfile entries added by this PR persist after revert** — older wfctl ignores them harmlessly (encoding/json + yaml ignore unknown keys, and entries are structurally valid).
- **Outer-frame contract regression risk**: if Task 2 (gate drop) is reverted without also reverting Task 3 (outer-frame guards), `installFromLockfile` behavior is unchanged. If Task 3 is reverted without reverting Task 2, the gate is gone AND no outer-frame guard exists → inner installs clobber lockfile entries. **Revert Tasks 2 + 3 together if reverting either.**

Backwards-compat: subcommand behavior expansion is additive (more entries written). Existing scripts that parse the lockfile see additional entries — structurally compatible.
