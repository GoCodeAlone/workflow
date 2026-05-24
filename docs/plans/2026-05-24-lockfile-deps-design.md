# wfctl plugin install — lockfile dep tracking Design

**Issue:** [workflow#771](https://github.com/GoCodeAlone/workflow/issues/771)
**Status:** Draft 2026-05-24 — awaiting adversarial design review
**Author:** Jon Langevin

## Problem

`wfctl plugin install <name>@<version>` recursively resolves + installs transitive `manifest.Dependencies` (`cmd/wfctl/plugin_deps.go:201 resolveDependencies`) but only the **parent** plugin gets a `.wfctl-lock.yaml` entry. Transitively-installed deps are written to disk via `installPluginFromManifest` (`plugin_deps.go:268`) but never get a lockfile entry.

Second gap: plain `wfctl plugin install <name>` (no `@version`) skips lockfile update entirely (`plugin_install.go:256` `if _, ver := parseNameVersion(nameArg); ver != ""` gate). The resolved registry version isn't captured.

## Solution

Two small edits, single PR:

### 1. Track deps in lockfile (`plugin_deps.go`)

After successful dep install in `resolveDependencies` (after `plugin_deps.go:270` `resolved[dep.Name] = depManifest.Version`), compute the dep's binary SHA256 and call `updateLockfileWithChecksum` with the same args shape used for parent installs:

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

Matches the parent's pattern at `plugin_install.go:258-265`. Registry field is empty (matches parent behavior when installed via registry resolution).

### 2. Drop the `name@version` gate (`plugin_install.go`)

Remove the `if _, ver := parseNameVersion(nameArg); ver != ""` gate at line 256. Always update the lockfile with the resolved `manifest.Version` after a successful install. Per AC #2: plain `install <name>` should capture the resolved registry version, not skip the lockfile.

## Files

- `cmd/wfctl/plugin_deps.go:270` — append the dep-checksum + updateLockfileWithChecksum block.
- `cmd/wfctl/plugin_install.go:255-266` — drop the `ver != ""` gate; always update lockfile.
- `cmd/wfctl/plugin_install_lockfile_test.go` — add tests for transitive dep tracking + no-version install tracking.
- `cmd/wfctl/plugin_deps_test.go` — extend existing dep tests to assert lockfile entries.

## Architecture choices

| Choice | Picked | Rejected (reason) |
|---|---|---|
| Where to call updateLockfileWithChecksum for deps | inside resolveDependencies after each install | wrap installPluginFromManifest with always-lockfile (broader blast radius; some callers may not want auto-lock) |
| no-version install lockfile behavior | always update with resolved version | gate behind `--lock` flag (extra UX surface; AC says default) |
| Registry field on dep lockfile entry | empty string (matches parent's `sourceName` when via registry) | populate "registry" sentinel (no upstream consumer reads this field meaningfully today) |

## Assumptions

1. **All deps install via `installPluginFromManifest` writing binary at `<pluginDir>/<dep.Name>/<dep.Name>`** — verified per `plugin_install.go:259` parent pattern + `plugin_deps.go:268` dep install call (same function). Binary location consistent.
2. **`updateLockfileWithChecksum` is safe to call N times in a single install run** — verified per `plugin_lockfile.go:146`: idempotent merge into `lf.Plugins` map; later writes overwrite earlier entries for the same key. Multiple distinct dep names produce distinct entries.
3. **Failed dep install short-circuits before lockfile call** — verified per `plugin_deps.go:268-269`: `if err := installPluginFromManifest(...); err != nil { return ... }`. Lockfile entry only written on success.
4. **`hashFileSHA256` is the canonical hash function and returns empty + error on missing binary** — verified per existing `plugin_install.go:261` usage; "no checksum" fallback path documented.
5. **Lockfile schema field `Registry` accepts empty string** — verified per `plugin_lockfile.go:142-165` struct + Save serialization; YAML omits empty strings naturally per omitempty tags (or stays empty without breaking parse).

## Failure modes

- **Dep binary missing after install (race/disk error)**: `hashFileSHA256` returns error; warning printed; lockfile entry has empty checksum. Parent has same behavior. Acceptable.
- **Lockfile write fails (permission/disk)**: `updateLockfileWithChecksum` silently no-ops per existing semantics; install still succeeds. Existing behavior; not regressed.
- **Lockfile write race with concurrent wfctl invocations**: existing lockfile has no locking; new code inherits same race window. Out of scope for this PR (cross-cutting concern).
- **Plain `install <name>` against a registry whose latest moves between resolution and re-run**: lockfile captures the moment-in-time resolved version. Next run with `.wfctl-lock.yaml` present uses the locked version (already-installed-skip path at `plugin_deps.go:230-234` short-circuits).

## Rollback

Runtime-affecting (changes which entries `.wfctl-lock.yaml` accumulates).

- **PR revert**: existing lockfiles with dep entries continue to parse (additive entries; no schema change). Future installs revert to parent-only tracking. Users who relied on dep entries lose them on next install.
- **Lockfile entries with empty checksum**: rare edge (binary hash failed); existing parent code already handles this case; no new failure path.
- **Backwards-compat**: `.wfctl-lock.yaml` schema unchanged (using existing PluginLockEntry struct). Older wfctl reading newer lockfile with dep entries: each entry is structurally valid; older wfctl just sees more entries than it would have written itself. No parse failure.

## Top 3 doubts (self-challenge)

1. **Plain `install <name>` always updating lockfile** changes user-visible behavior. Some operators may have deliberately avoided lockfile entries by omitting `@version`. Mitigation: the change matches the explicit AC; if users want opt-out, they can `--no-lock` (future flag, out of scope).
2. **Lockfile write happens after dep install but BEFORE parent install** — if parent install fails, lockfile has dep entries without parent. On retry the dep entries help (skip-already-installed). On manual diagnosis, the half-lockfile is informative. Existing behavior for plain installs: parent succeeds → lockfile written. New behavior: dep written before parent. If parent fails → dep stays in lockfile. Acceptable — install is best-effort transactional already (no atomic rollback).
3. **No dep version constraint persistence**: lockfile entries are version-pinned, but `dep.MinVersion`/`MaxVersion` constraints from the parent manifest are NOT captured in the dep's lockfile entry. Future re-install reads the lockfile-pinned version and skips constraint re-check (already-installed-skip path). Acceptable for AC scope; future tightening could record constraint metadata.

## Non-goals

- `wfctl plugin remove` lockfile cleanup (separate concern; existing behavior).
- Lockfile validation against installed state (covered by other tooling).
- Lockfile schema migration (additive only — no field changes).
- Constraint-metadata persistence on dep entries (deferred to future tightening).
- `--no-lock` opt-out flag (YAGNI for this PR).
- Concurrent-wfctl lockfile race handling (cross-cutting; pre-existing).
