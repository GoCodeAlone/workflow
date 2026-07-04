### Alignment Report

**Status:** PASS

**Coverage:**
| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| Add top-level read-only `wfctl doctor` | Task 1, Task 2 | Covered |
| Report binary, project config, manifest, lockfile, installed plugin state | Task 1 | Covered |
| Support `--include-global`, `--online`, `--format text|json`, `--strict` | Task 1 | Covered |
| Reuse existing lock/update/plugin helpers without mutation | Task 1 | Covered |
| Wire command through `main.go` and `wfctl.yaml` | Task 2 | Covered |
| Update docs for CLI behavior | Task 3 | Covered |
| Verify with focused tests, package tests, and CLI smoke | Task 4 | Covered |
| Exclude auto-repair, provider diagnostics, registry mutation | Scope Manifest, Task 1 | Covered |

**Scope Check:**
| Plan Task | Design Requirement | Status |
|---|---|---|
| Task 1 | Doctor report model/checks/output | Justified |
| Task 2 | Top-level command wiring and help | Justified |
| Task 3 | Docs for new CLI behavior | Justified |
| Task 4 | Verification and PR preparation | Justified |

**Manifest Check:** `plan-scope-check.sh --plan docs/plans/2026-07-04-wfctl-doctor.md` → PASS.

**Drift Items:** None.
