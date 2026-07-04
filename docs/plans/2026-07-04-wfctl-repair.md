# wfctl Repair Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Add dry-run-by-default `wfctl repair` for project plugin lifecycle drift.

**Architecture:** Reuse `doctor` diagnostics and existing `plugin lock` / `plugin install` mutation paths. `repair` plans ordered actions, requires `--apply` to mutate, and keeps unsupported/provider-specific fixes as suggestions.

**Tech Stack:** Go stdlib CLI, existing `cmd/wfctl` helpers, Workflow embedded `wfctl.yaml`.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~420

**Out of scope:**
- Provider-specific repair APIs or plugin-owned fixes.
- Binary self-update execution.
- Global plugin mutation.
- New plugin resolution/install behavior.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add guarded wfctl repair | Task 1, Task 2, Task 3, Task 4 | `feat/wfctl-repair-lifecycle` |

**Status:** Draft

### Task 1: Repair Planner Tests

**Files:**
- Create: `cmd/wfctl/repair_test.go`

**Steps:**
1. Add test fixture reusing `writeDoctorFixture`.
2. Test stale lock + missing install dry-run returns ordered `plugin lock` then `plugin install`.
3. Test missing manifest yields suggestion-only and no executable actions.
4. Test `--apply` uses injected runner and records lock/install args.
5. Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRepair' -count=1`
6. Expected: FAIL with undefined `runRepairWithOutput` / repair types.
7. Commit after Task 2 green.

### Task 2: Repair Command Implementation

**Files:**
- Create: `cmd/wfctl/repair.go`
- Modify: `cmd/wfctl/main.go`
- Modify: `cmd/wfctl/wfctl.yaml`

**Steps:**
1. Implement flags: `--workflow`, `--manifest`, `--lock-file`, `--plugin-dir`, `--include-global`, `--online`, `--format text|json`, `--apply`.
2. Implement `repairReport`, `repairAction`, `repairRunner`.
3. Planner:
   - call `buildDoctorReport`.
   - inspect manifest/lock/install state.
   - emit `plugin lock` action when lock missing/stale/incomplete.
   - emit `plugin install` action when relock is planned or installed plugins mismatch.
   - emit suggest-only entries for missing workflow/manifest, parse errors, update hints, global info.
4. Executor:
   - dry-run prints `DRY-RUN` and commands.
   - `--apply` runs lock before install; stop on first error.
5. Wire `commands["repair"]` and `cmd-repair` in `wfctl.yaml`.
6. Run: `GOWORK=off go test ./cmd/wfctl -run 'TestRepair|TestDoctorCommandWiring' -count=1`
7. Expected: PASS.
8. Regression proof: temporarily remove command wiring; `TestRepairCommandWiring` fails; restore; test passes.
9. Commit: `feat(wfctl): add guarded repair command`.

Rollback: revert this commit; existing `doctor`, `plugin lock`, and `plugin install` remain.

### Task 3: Docs

**Files:**
- Modify: `README.md`
- Modify: `docs/WFCTL.md`
- Modify: `docs/WFCTL_INSTALLATION.md`

**Steps:**
1. Add README command list entry for `wfctl repair`.
2. Add `docs/WFCTL.md` section after `doctor`: flags, dry-run/apply examples, mutation boundaries.
3. Add install lifecycle doc note: `doctor` diagnoses, `repair --apply` relocks/reinstalls project plugins.
4. Run: `rg -n "wfctl repair|repair --apply|cmd-repair" README.md docs/WFCTL.md docs/WFCTL_INSTALLATION.md cmd/wfctl/wfctl.yaml`
5. Expected: command documented and wired.
6. Commit: `docs(wfctl): document repair lifecycle`.

### Task 4: Verification And PR

**Files:**
- No new source unless verification finds defects.

**Steps:**
1. Run focused tests: `GOWORK=off go test ./cmd/wfctl -run 'TestRepair|TestDoctor|TestMain' -count=1`
2. Expected: PASS.
3. Run package tests: `GOWORK=off go test ./cmd/wfctl`
4. Expected: PASS.
5. Run CLI help: `GOWORK=off go run ./cmd/wfctl repair --help`
6. Expected: usage text includes `--apply`.
7. Run dry-run smoke against temp fixture.
8. Expected: output contains `DRY-RUN`, `wfctl plugin lock`, `wfctl plugin install`; no mutation without `--apply`.
9. Run `git diff --check origin/main...HEAD`.
10. Expected: no whitespace errors.
11. Open PR, monitor CI/review, admin-merge once green and review addressed.

Rollback: revert PR; no runtime migration or deployment rollback.
