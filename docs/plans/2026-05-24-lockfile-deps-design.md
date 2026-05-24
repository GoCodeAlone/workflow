# wfctl plugin install — lockfile dep tracking Design

**Issue:** [workflow#771](https://github.com/GoCodeAlone/workflow/issues/771)
**Status:** Revised cycle 2 2026-05-24 — awaiting re-review
**Author:** Jon Langevin

## Revision history

- **Cycle 1**: drafted minimal "call updateLockfileWithChecksum from resolveDependencies + drop @version gate". FAILED — 2 Critical:
  - C1: new-format lockfile early-return at `plugin_lockfile.go:147` makes dep-write a no-op on the dominant case (any project with `version: 1` lockfile).
  - C2: dropping the `@version` gate breaks `installFromLockfile`'s no-clobber contract (relied on by lockfile-driven install per `plugin_lockfile.go:115-118` comment).
- **Cycle 2**: both formats covered + installSkipLockfileUpdate flag for installFromLockfile contract preservation. FAILED — 1 Critical (C3: installFromWfctlLockfile line-105 calls runPluginInstall ALSO unguarded; its in-memory `lf.Save` at line 116 silently clobbers the fan-out's writes) + 1 Important (I4: installPluginReqDirect skips parent lockfile track via direct installPluginFromManifest call, bypassing runPluginInstall).
- **Cycle 3** (this version): adds installSkipLockfileUpdate guard around installFromWfctlLockfile's runPluginInstall call site (mirror of cycle-2 fix for the legacy installFromLockfile path); adds updateLockfileWithChecksum call to installPluginReqDirect parent path; updates test plan with §e for installFromWfctlLockfile regression.

## Problem

`wfctl plugin install <name>@<version>` recursively resolves + installs transitive `manifest.Dependencies` (`cmd/wfctl/plugin_deps.go:201 resolveDependencies`) but only the **parent** plugin gets tracked, and only in some cases:

1. `updateLockfileWithChecksum` only touches the LEGACY `.wfctl-lock.yaml` format (`plugin_lockfile.go:142`). On projects with NEW-format lockfiles (`Version: 1`), the early-return at line 147 silently skips writes entirely → no parent tracking either.
2. Even on legacy format, the `if _, ver := parseNameVersion(nameArg); ver != ""` gate at `plugin_install.go:256` skips lockfile writes for plain `install <name>` invocations.
3. Recursive dep installs (`plugin_deps.go:268`) never call any lockfile update — neither format gets dep entries.

## Solution

Three pieces, single PR:

### 1. New-format lockfile dep+parent merge (`config/wfctl_lockfile.go` consumer)

Add `mergeIntoNewFormatLockfile(name, version, source string)` helper in `cmd/wfctl/plugin_lockfile.go` that:

```go
func mergeIntoNewFormatLockfile(name, version, source string) {
    lf, err := config.LoadWfctlLockfile(wfctlLockPath)
    if err != nil || lf == nil || lf.Version == 0 {
        return // no new-format lockfile present; legacy path handles it
    }
    if lf.Plugins == nil {
        lf.Plugins = make(map[string]config.WfctlLockPluginEntry)
    }
    existing := lf.Plugins[name]
    // Preserve Platforms / Compatibility from existing entry — only update Version + Source.
    existing.Version = version
    if source != "" {
        existing.Source = source
    }
    lf.Plugins[name] = existing
    _ = config.SaveWfctlLockfile(wfctlLockPath, lf)
}
```

**Important**: do NOT clobber `existing.Platforms` (per-arch URLs + checksums) — those came from prior `wfctl plugin lock` generation. Merge-update preserves the heavy data, only refreshes Version + Source.

### 2. Refactor `updateLockfileWithChecksum` to update BOTH formats

Replace the early-return at `plugin_lockfile.go:147` with a fan-out:

```go
func updateLockfileWithChecksum(pluginName, version, repository, registry, sha256Hash string) {
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
        Version: version, Repository: repository, Registry: registry, SHA256: sha256Hash,
    }
    _ = lf.Save(wfctlLockPath)
}
```

The legacy path still runs (idempotent on new-format files which don't have `plugins:` in the legacy shape). Both writes are best-effort silent (preserves existing failure-tolerance posture).

### 3. Track deps in `resolveDependencies` (`plugin_deps.go`)

After successful dep install (after `plugin_deps.go:270` `resolved[dep.Name] = depManifest.Version`):

```go
depBinaryPath := filepath.Join(pluginDir, dep.Name, dep.Name)
depChecksum := ""
if cs, hashErr := hashFileSHA256(depBinaryPath); hashErr == nil {
    depChecksum = cs
} else {
    fmt.Fprintf(os.Stderr, "warning: could not hash dep binary %s: %v (lockfile will have no checksum)\n", depBinaryPath, hashErr)
}
updateLockfileWithChecksum(dep.Name, depManifest.Version, depManifest.Repository, "", depChecksum)
```

### 4. Replace `@version` gate with explicit skip flag (preserves `installFromLockfile` contract)

Per cycle-1 C2: `installFromLockfile` (`plugin_lockfile.go:115-118`) deliberately passes `name` without `@version` to avoid clobbering pinned entries before checksum verification. Removing the gate naively breaks that contract.

Add package-level guard set/cleared by `installFromLockfile`:

```go
// In plugin_install.go (package-level):
// installSkipLockfileUpdate suppresses lockfile updates during installFromLockfile's
// pre-verification install. Set by installFromLockfile; cleared in deferred reset.
var installSkipLockfileUpdate bool

// In runPluginInstall, replace the gate at line 256:
if !installSkipLockfileUpdate {
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

In `installFromLockfile` AND `installFromWfctlLockfile` (per cycle-3 C3 — both outer-frame installers hold in-memory `lf` and assume nothing underneath touches the on-disk file):

```go
installSkipLockfileUpdate = true
defer func() { installSkipLockfileUpdate = false }()
// ... existing call to runPluginInstall(...)
```

Specifically:
- `cmd/wfctl/plugin_lockfile.go` line ~115 (legacy `installFromLockfile`).
- `cmd/wfctl/plugin_install_wfctllock.go` line ~99 (new-format `installFromWfctlLockfile` fallback path that calls `runPluginInstall(spec)` when per-platform archive is unavailable).

(Package-level boolean is acceptable — installs are sequential within a single wfctl invocation; no concurrent goroutines call this path.)

Plain `install <name>` (no `@version`) now writes to lockfile by default per AC #2.

### 5. Cover `installPluginReqDirect` parent (cycle-3 I4)

`cmd/wfctl/plugin_deps.go:82-112` (`installPluginReqDirect`) is the `--from-config` / `installFromWorkflowConfig` parent path. It calls `installPluginFromManifest` directly (NOT `runPluginInstall`) so the §4 guard logic doesn't apply. Add lockfile tracking explicitly after the parent install succeeds:

```go
// In installPluginReqDirect, after successful installPluginFromManifest:
binaryPath := filepath.Join(pluginDir, req.Name, req.Name)
checksum := ""
if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
    checksum = cs
}
updateLockfileWithChecksum(req.Name, manifest.Version, manifest.Repository, "", checksum)
```

Pre-existing gap (not regressed by this PR) but trivially closeable in the same PR — same chokepoint helper. Closes the user-intent asymmetry where `--from-config` deps land in lockfile but parents don't.

## Files

- `cmd/wfctl/plugin_lockfile.go` — refactor `updateLockfileWithChecksum` to fan out to both formats; add `mergeIntoNewFormatLockfile` helper.
- `cmd/wfctl/plugin_install.go:255-266` — replace `@version` gate with `!installSkipLockfileUpdate` guard; declare package-level bool.
- `cmd/wfctl/plugin_lockfile.go` (legacy `installFromLockfile`) — set+defer-clear `installSkipLockfileUpdate` around the inner `runPluginInstall` call.
- `cmd/wfctl/plugin_install_wfctllock.go` (new-format `installFromWfctlLockfile`) — set+defer-clear `installSkipLockfileUpdate` around the inner `runPluginInstall` fallback call (cycle-3 C3).
- `cmd/wfctl/plugin_deps.go:82-112` (`installPluginReqDirect` parent path) — append `updateLockfileWithChecksum` after success (cycle-3 I4).
- `cmd/wfctl/plugin_deps.go:270` — append dep checksum + updateLockfileWithChecksum.
- `cmd/wfctl/plugin_install_lockfile_test.go` — add tests for: (a) parent+dep tracking on LEGACY format, (b) parent+dep tracking on NEW format (verifying Platforms preserved), (c) no-version-install tracked on both formats, (d) `installFromLockfile` no-clobber invariant still holds (regression test), (e) `installFromWfctlLockfile` no-clobber invariant when fallback fires (regression test for cycle-3 C3), (f) `installPluginReqDirect` parent gets tracked (cycle-3 I4 coverage).
- `cmd/wfctl/plugin_deps_test.go` — extend existing dep tests to assert lockfile entries on both formats.

## Architecture choices

| Choice | Picked | Rejected (reason) |
|---|---|---|
| New-format coverage | refactor `updateLockfileWithChecksum` to fan out | document as out-of-scope (rejected: AC says "track in THE lockfile"; format split is invisible to operator) |
| `installFromLockfile` no-clobber preservation | package-level bool flag set+deferred | new `runPluginInstallNoLock` entrypoint (rejected: larger diff, more surface) |
| New-format dep merge semantics | preserve Platforms; update Version+Source only | overwrite entire entry (rejected: would clobber `wfctl plugin lock` generated per-arch data) |
| `Repository` field on dep entry | use `depManifest.Repository` as-is; empty if unset | fallback to constructed GitHub URL (rejected: per-org assumption + verify against existing registry data — defer to follow-up) |

## Assumptions

1. **`installPluginFromManifest` writes binary at `<pluginDir>/<dep.Name>/<dep.Name>`** for both parent and dep installs — verified per `plugin_install.go:259` (parent) and `plugin_deps.go:268` (dep, same function). Binary location consistent across both call sites.
2. **`config.LoadWfctlLockfile` returns `Version == 0` when file is missing OR legacy-format** — verified per `config/wfctl_lockfile.go:49-60`; missing file returns error, legacy parse returns zero-value struct. Helper's `Version > 0` check guards correctly.
3. **`config.SaveWfctlLockfile` accepts a lockfile struct with existing `Plugins` map and merges via replace** — verified per `config/wfctl_lockfile.go:62`; full re-serialization. Our merge logic pre-builds the desired final map shape.
4. **`installSkipLockfileUpdate` flag is safe as package-level state** — installs run sequentially within a single wfctl invocation; no goroutine concurrency across `runPluginInstall` calls. Cleared via defer; can't leak.
5. **`depManifest.Repository` is `omitempty`** but treated as best-effort (empty Source in new-format lockfile = entry still valid; legacy replay needs the field for source URL but lockfile is informational on the legacy path).
6. **Lockfile writes are idempotent** — `updateLockfileWithChecksum` called N times for N deps produces N distinct entries; existing parent code calls it once and works. New code inherits same semantics.

## Failure modes

- **Dep binary missing after install (race/disk)**: `hashFileSHA256` returns error → warning printed → lockfile entry has empty checksum. Matches parent behavior. Acceptable.
- **Lockfile write fails (permission/disk)**: silent no-op per existing semantics; install still succeeds. Not regressed.
- **New-format `Platforms` data dropped accidentally**: explicit `existing := lf.Plugins[name]` + selective field update preserves Platforms. Regression test required (Test plan §b).
- **`installFromLockfile` regression** (cycle-1 C2): mitigated by `installSkipLockfileUpdate` guard; regression test required (Test plan §d).
- **Concurrent wfctl invocations**: lockfile has no file-level locking; pre-existing race window. Not regressed; not addressed in this PR.
- **Plain `install <name>` against registry whose latest moves**: lockfile captures moment-in-time resolved version. Next run with lockfile present uses locked version (already-installed-skip path).

## Rollback

Runtime-affecting (changes lockfile-write behavior).

- **PR revert**: dep entries stop being written. Existing entries continue to parse. Lockfile-driven installs continue to work (gate replacement is internal-only refactor).
- **`installSkipLockfileUpdate` is package-private**: revert removes the flag + restores the gate. No external API change.
- **Lockfile schema unchanged**: both legacy `PluginLockEntry` and new `WfctlLockPluginEntry` structures untouched.
- **Backwards-compat**: older wfctl reading newer-written lockfile: more entries than would have been written; structurally valid; no parse failure.

## Top 3 doubts (self-challenge)

1. **Dual-format write doubles I/O on every install.** Acceptable per design (best-effort + cheap YAML write); the operator-perceived value of unified dep tracking outweighs the cost.
2. **Package-level `installSkipLockfileUpdate` is global state.** Cleaner pattern is a context-passed flag, but requires threading through `runPluginInstall` signature. Defer to a future cleanup if pattern proliferates. Acceptable for this PR (single call site).
3. **`Platforms` preservation in new-format merge** could surprise — operator who manually crafted a new-format lockfile expects `wfctl plugin install` to be inert. But: design only merges when an entry is being newly written for an install we just performed, NOT for unrelated plugins. The merge is additive per-plugin.

## Non-goals

- `wfctl plugin remove` lockfile cleanup (separate concern).
- Lockfile schema migration (additive only — no field changes).
- Constraint-metadata persistence on dep entries (deferred to future tightening).
- `--no-lock` user-facing opt-out flag (YAGNI for this PR; AC says default-on).
- Concurrent-wfctl lockfile race handling (cross-cutting; pre-existing).
- `Repository` fallback URL construction for empty manifest entries (defer to follow-up; gap documented).
- Platforms data backfill for newly-tracked deps (deps only get Version+Source; per-arch archive metadata stays absent until next `wfctl plugin lock` run).
