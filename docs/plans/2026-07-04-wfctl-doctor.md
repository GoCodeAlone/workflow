# wfctl Doctor Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add a read-only `wfctl doctor` lifecycle diagnostic command for binary, project, lockfile, and plugin install health.

**Architecture:** Implement one top-level CLI command in `cmd/wfctl` that reuses existing config, lockfile, plugin verification, and update helpers. The command emits a stable report model rendered as text or JSON and never mutates state.

**Tech Stack:** Go standard library, existing `config` package, existing wfctl plugin/update helpers.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~500

**Out of scope:**
- Auto-repair or file mutation.
- Provider-specific diagnostics.
- New plugin registry behavior.
- Website docs release work unless a later Workflow release triggers generated docs.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add wfctl doctor lifecycle diagnostics | Task 1, Task 2, Task 3, Task 4 | feat/wfctl-lifecycle-evaluation |

**Status:** Draft

## Design Trace

| requirement | task |
|---|---|
| Top-level read-only diagnostic command | Task 1, Task 2 |
| Binary/project/manifest/lock/plugin checks | Task 1 |
| Text and JSON output | Task 1 |
| CLI wiring and help | Task 2 |
| Docs and examples | Task 3 |
| Focused + package verification | Task 4 |

### Task 1: Doctor Report Model and Checks

**Files:**
- Create: `cmd/wfctl/doctor.go`
- Create: `cmd/wfctl/doctor_test.go`

**Step 1: Write failing tests**

Add tests for:
- stale lock provenance reports `WARN` and fix `wfctl plugin lock`;
- locked plugin missing from `data/plugins` reports `WARN` and fix `wfctl plugin install`;
- `--format json` emits valid JSON with section/check statuses;
- `--online` uses `githubReleasesURLOverride` and reports update availability
  without touching the filesystem;
- healthy project manifest + lock + installed plugin reports overall `OK`.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestDoctor' -count=1`

Expected: FAIL with undefined `runDoctorWithOutput` / missing doctor implementation.

**Step 2: Implement minimal model**

Define:
- `doctorStatus` enum: `OK`, `WARN`, `ERROR`.
- `doctorCheck`, `doctorSection`, `doctorReport`.
- `runDoctor(args []string) error`.
- `runDoctorWithOutput(args []string, out io.Writer) error`.

Implement checks:
- binary section: version + executable path;
- optional online latest-release check when `--online` is set;
- project section: workflow file existence and parse result;
- plugin section: manifest/lock/provenance/install version checks;
- optional global plugin summary when `--include-global` is set.

**Step 3: Render output**

Add text renderer with `Fix:` lines and JSON renderer using `encoding/json`.
`--strict` returns an error when any `WARN` or `ERROR` exists; default returns
nil after printing diagnostics.

**Step 4: Verify**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestDoctor' -count=1`

Expected: PASS.

Rollback: revert `cmd/wfctl/doctor.go` and `cmd/wfctl/doctor_test.go`.

### Task 2: Wire Top-Level Command

**Files:**
- Modify: `cmd/wfctl/main.go`
- Modify: `cmd/wfctl/wfctl.yaml`
- Modify: `cmd/wfctl/main_test.go` or add focused command-list coverage in `cmd/wfctl/doctor_test.go`

**Step 1: Write failing wiring test**

Add a test that the embedded command map/config exposes `doctor`, and a help
invocation includes `doctor`.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestDoctor.*Help|TestMain.*Doctor' -count=1`

Expected: FAIL because `doctor` is not wired.

**Step 2: Wire command**

Add `doctor: runDoctor` to `commands` and a `doctor` entry plus `cmd-doctor`
pipeline to `cmd/wfctl/wfctl.yaml`.

**Step 3: Verify CLI help**

Run: `GOWORK=off go run ./cmd/wfctl doctor --help`

Expected: usage prints `Usage: wfctl doctor [options]`.

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestDoctor.*Help|TestMain.*Doctor' -count=1`

Expected: PASS.

Rollback: revert command map/config wiring.

### Task 3: Documentation

**Files:**
- Modify: `docs/WFCTL.md`
- Modify: `README.md`
- Modify: `docs/plans/INDEX.md` only if existing plan-index convention requires it

**Step 1: Add docs**

Document `wfctl doctor` near platform inspection/lifecycle commands:
- offline default;
- `--online` opt-in;
- strict CI usage;
- common repair commands.

**Step 2: Verify docs references**

Run: `rg -n "wfctl doctor|doctor" docs/WFCTL.md README.md cmd/wfctl/wfctl.yaml`

Expected: docs and command metadata all contain `doctor`.

Rollback: revert doc additions.

### Task 4: Final Verification and PR

**Files:**
- No new source files beyond previous tasks.

**Step 1: Format**

Run: `gofmt -w cmd/wfctl/doctor.go cmd/wfctl/doctor_test.go`

Expected: command exits 0.

**Step 2: Focused tests**

Run: `GOWORK=off go test ./cmd/wfctl -run 'TestDoctor|TestMain' -count=1`

Expected: PASS.

**Step 3: Package tests**

Run: `GOWORK=off go test ./cmd/wfctl`

Expected: PASS.

**Step 4: CLI smoke**

Run: `GOWORK=off go run ./cmd/wfctl doctor --format json`

Expected: valid JSON; command exits 0 by default even when warnings exist.

Run: `GOWORK=off go run ./cmd/wfctl doctor --workflow /tmp/wfctl-doctor-missing-workflow.yaml --manifest /tmp/wfctl-doctor-missing-manifest.yaml --lock-file /tmp/wfctl-doctor-missing-lock.yaml --strict`

Expected: exits non-zero and prints missing workflow/manifest diagnostics.

**Step 5: Commit**

```bash
git add cmd/wfctl docs/WFCTL.md README.md
git commit -m "feat(wfctl): add lifecycle doctor"
```

Rollback: revert the feature commit and re-run `GOWORK=off go test ./cmd/wfctl`.
