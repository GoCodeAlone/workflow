# Alignment Report

**Status:** PASS

## Coverage

| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| Add stable inventory model with evidence, providers, usage, findings, and metadata. | Task 1 | Covered |
| Own an explicit versioned taxonomy for product-level capability IDs and aliases. | Task 1 | Covered |
| Generate ecosystem matrix from registry and local plugin repos. | Task 2, Task 5 | Covered |
| Include released and local-only capabilities with release/source metadata. | Task 2, Task 5 | Covered |
| Emit uncategorized/needs-review rows for unmapped raw plugin types. | Task 1, Task 2 | Covered |
| Generate application capability profile from wfctl manifest, lockfile, config, and plugin manifests. | Task 3, Task 4 | Covered |
| Reuse existing config discovery/import/merge behavior. | Task 3 | Covered |
| Mark declared vs inferred usage with confidence and evidence. | Task 3 | Covered |
| Report missing-provider and policy-risk findings warning-only. | Task 3, Task 4 | Covered |
| Avoid reading/printing secret values. | Task 3, Task 4, Task 6 | Covered |
| Add `wfctl capability ecosystem|app|check` command surface. | Task 4 | Covered |
| Emit JSON and Markdown artifacts suitable for website consumption. | Task 4, Task 5 | Covered |
| Commit generated Workflow-owned snapshot with schema and provenance metadata. | Task 5 | Covered |
| Update docs and verify CLI/help/invocations. | Task 4, Task 6 | Covered |
| Run focused tests and final smoke verification. | Task 6 | Covered |

## Scope Check

| Plan Task | Design Requirement | Status |
|---|---|---|
| Task 1 | Capability model and taxonomy ownership | Justified |
| Task 2 | Ecosystem matrix generation | Justified |
| Task 3 | Application profile and policy findings | Justified |
| Task 4 | CLI command and docs surface | Justified |
| Task 5 | Website-consumable generated artifacts | Justified |
| Task 6 | Verification, smoke checks, and PR prep | Justified |

## Manifest Check

- `PR Count: 1` matches one PR grouping row.
- `Tasks: 6` matches six `## Task N:` headings.
- Each task appears in exactly one PR row.
- `tests/plan-scope-check.sh` is absent in this repo revision; inline manifest check performed instead.

**Drift Items:** none.
