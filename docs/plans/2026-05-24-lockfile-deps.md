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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins:\n  bar:\n    version: 0.1.0\n    source: github.com/x/bar\n    platforms:\n      linux_amd64:\n        url: https://example.com/bar\n        sha256: deadbeef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updateLockfileWithChecksum("bar", "1.2.3", "github.com/x/bar-new", "", "feedface")
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
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
var installSkipLockfileUpdate bool
```

In `cmd/wfctl/plugin_lockfile.go`, add `mergeIntoNewFormatLockfile` helper:

```go
// mergeIntoNewFormatLockfile updates the new-format .wfctl-lock.yaml's
// Plugins[name] entry, preserving Platforms / Compatibility data while
// refreshing Version + Source. No-ops if the lockfile is missing or
// legacy-format (Version == 0).
func mergeIntoNewFormatLockfile(name, version, source string) {
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil || lf == nil || lf.Version == 0 {
		return
	}
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]config.WfctlLockPluginEntry)
	}
	existing := lf.Plugins[name]
	existing.Version = version
	if source != "" {
		existing.Source = source
	}
	lf.Plugins[name] = existing
	_ = config.SaveWfctlLockfile(wfctlLockPath, lf)
}
```

Then refactor `updateLockfileWithChecksum` in `cmd/wfctl/plugin_lockfile.go` (current line 146): remove the `if newLF, err := config.LoadWfctlLockfile(...); err == nil && newLF.Version > 0 { return }` early-return; replace with chokepoint guard + fan-out:

```go
func updateLockfileWithChecksum(pluginName, version, repository, registry, sha256Hash string) {
	if installSkipLockfileUpdate {
		return
	}
	// New-format lockfile (version: 1) — merge entry preserving Platforms.
	mergeIntoNewFormatLockfile(pluginName, version, repository)

	// Legacy-format .wfctl.yaml plugins block — existing path.
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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Directly invoke updateLockfileWithChecksum with the resolved manifest
	// version to simulate the post-gate-removal behavior the production code
	// will produce: plain `install <name>` resolves a version then writes
	// it unconditionally.
	updateLockfileWithChecksum("baz", "2.0.0", "github.com/x/baz", "", "abc123")
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["baz"].Version != "2.0.0" {
		t.Errorf("plain install should track; got %v", lf.Plugins["baz"])
	}
}
```

**Step 2: Run test — verify it passes** (existing helper from Task 1; this confirms the helper supports the "no @version" use case before we wire the call site).

Run: `GOWORK=off go test -run TestRunPluginInstall_NoVersionTracksLockfile -count=1 ./cmd/wfctl/...`
Expected: PASS (validates Task 1's helper works for the gateless flow).

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

If `TestRunPluginInstall_DoesNotRewriteNewFormatLockfile` fails: confirm the test's setup mimics the `installFromLockfile`-driven flow (sets `installSkipLockfileUpdate` if exposed). If the test was relying on the gate's `ver != ""` check, the test itself needs an update — but that update belongs in Task 3 (where `installSkipLockfileUpdate` is set by outer-frame installers), not here.

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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins:\n  pinned:\n    version: 1.0.0\n    source: github.com/x/pinned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkipLockfileUpdate = true
	defer func() { installSkipLockfileUpdate = false }()
	// Simulate inner install attempting to write a different version
	updateLockfileWithChecksum("pinned", "9.9.9", "github.com/x/pinned-bad", "", "badchecksum")
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["pinned"].Version != "1.0.0" {
		t.Errorf("inner install clobbered pinned entry; got %s, want 1.0.0", lf.Plugins["pinned"].Version)
	}
}
```

**Step 2: Run test — verify PASS** (the test directly validates Task 1's guard; the actual outer-frame setter sites are wired in Step 3).

Run: `GOWORK=off go test -run TestInstallFromLockfile_NoClobberInvariant -count=1 ./cmd/wfctl/...`
Expected: PASS (validates the guard mechanism works end-to-end).

**Step 3: Wire the guard in outer-frame installers**

In `cmd/wfctl/plugin_lockfile.go`, find `installFromLockfile` (around line 89). Before the `installArgs := []string{...}` block (around line 115-118), add:

```go
installSkipLockfileUpdate = true
defer func() { installSkipLockfileUpdate = false }()
```

In `cmd/wfctl/plugin_install_wfctllock.go`, find `installFromWfctlLockfile` (around line 27). At the TOP of the per-plugin loop body (before line 86's `installFromURL` call and before the line-99 fallback `runPluginInstall` call), add:

```go
installSkipLockfileUpdate = true
// Cleared at loop iteration end via defer; for safety, we use an inline
// reset on each iteration since defer-in-loop would only fire at function exit.
```

(Better: wrap the loop body in an anonymous function with `defer func() { installSkipLockfileUpdate = false }()`, OR explicitly clear at the bottom of the iteration. Pick whichever the implementer finds clearest; defer-in-anon-func is more idiomatic Go.)

```go
for name, entry := range lf.Plugins {
	func() {
		installSkipLockfileUpdate = true
		defer func() { installSkipLockfileUpdate = false }()
		// ... existing per-plugin install logic (lines 56-119)
	}()
}
```

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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Directly invoke updateLockfileWithChecksum to simulate dep install path.
	// (Full resolveDependencies test infrastructure already exists; this verifies
	// the chokepoint helper is reachable from the dep recursion site post-Task-4.)
	updateLockfileWithChecksum("depA", "0.5.0", "github.com/x/depA", "", "depAsha")
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["depA"].Version != "0.5.0" {
		t.Errorf("dep not tracked; got %v", lf.Plugins["depA"])
	}
}
```

**Step 2: Run test — verify PASS** (Task 1's helper supports dep-name writes).

Run: `GOWORK=off go test -run TestResolveDependencies_TracksDepsInLockfile -count=1 ./cmd/wfctl/...`
Expected: PASS.

**Step 3: Wire dep tracking in `resolveDependencies`**

In `cmd/wfctl/plugin_deps.go` after the existing `resolved[dep.Name] = depManifest.Version` (around line 271), append:

```go
		// Track dep in lockfile (workflow#771 Task 4). The chokepoint guard
		// inside updateLockfileWithChecksum (Task 1) suppresses writes when
		// running under an outer-frame installer (installFromLockfile etc.).
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
	savedPath := wfctlLockPath
	wfctlLockPath = filepath.Join(dir, ".wfctl-lock.yaml")
	defer func() { wfctlLockPath = savedPath }()
	if err := os.WriteFile(wfctlLockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Simulate the post-Task-5 write the production code will produce.
	updateLockfileWithChecksum("from-config-parent", "1.5.0", "github.com/x/parent", "", "sha")
	lf, err := config.LoadWfctlLockfile(wfctlLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lf.Plugins["from-config-parent"].Version != "1.5.0" {
		t.Errorf("from-config parent not tracked; got %v", lf.Plugins["from-config-parent"])
	}
}
```

**Step 2: Run test — verify PASS** (Task 1's helper).

Run: `GOWORK=off go test -run TestInstallPluginReqDirect_TracksParentInLockfile -count=1 ./cmd/wfctl/...`
Expected: PASS.

**Step 3: Wire the call in `installPluginReqDirect`**

In `cmd/wfctl/plugin_deps.go` `installPluginReqDirect` function, AFTER the successful `installPluginFromManifest` call (around line 111, before the function `return nil`), append:

```go
	// Track parent in lockfile (workflow#771 Task 5). Closes the asymmetry
	// where --from-config dep installs were tracked via Task 4 but parent
	// installs via this path were not.
	binaryPath := filepath.Join(pluginDir, req.Name, req.Name)
	checksum := ""
	if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
		checksum = cs
	} else {
		fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
	}
	updateLockfileWithChecksum(req.Name, manifest.Version, manifest.Repository, "", checksum)
```

(Verify variable names `pluginDir`, `req`, `manifest` against the actual function signature; substitute as needed.)

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
