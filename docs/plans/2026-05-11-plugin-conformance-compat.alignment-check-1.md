### Alignment Report

**Status:** FAIL

**Coverage:**
| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| `wfctl plugin conformance --output <path>` writes JSON evidence | — | MISSING |
| `--format text|json` is supported | Task 3 | Partial: JSON covered; text not explicit |
| `SupportedCanonicalKeys` may be called, but resource/credential methods must not be called | — | MISSING |
| local-dir conformance evidence lacks `archiveSHA256` and remains advisory | — | MISSING |
| registry update marks indexes stale when latest engine is newer than evidence | — | MISSING |
| manifest/index scope, resolver behavior, lock metadata, trust, rollback | Tasks 1-7 | Covered |

**Scope Check:**
| Plan Task | Design Requirement | Status |
|---|---|---|
| Task 1 | Evidence model, digest, version grammar, trust modes | Covered |
| Task 2 | Registry source index API and same-source resolution | Covered |
| Task 3 | Conformance command, staging, timeout, typed-IaC checks | Covered with missing sub-requirements |
| Task 4 | Registry compatibility update command | Covered with missing stale marker |
| Task 5 | Install/update compatibility resolver | Covered |
| Task 6 | Lockfile compatibility metadata | Covered |
| Task 7 | Docs, broad verification, runtime validation | Covered |

**Drift Items:**
- Add Task 3 checks for `--output`, text format, local advisory evidence, and `SupportedCanonicalKeys`/no-resource-call behavior.
- Add Task 4 check for stale index marking.
- `tests/plan-scope-check.sh` does not exist in this repository; manifest was checked manually for PR count, task count, and PR row/task references.
