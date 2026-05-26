### Alignment Report

**Status:** PASS

**Coverage:**

| Design Requirement | Plan Task(s) | Status |
|---|---|---|
| Provider-neutral requirements for observability, web/API, brokers, databases, caches, and storage | Task 1, Task 2, Task 3 | Covered |
| `wfctl infra derive` calculates missing requirements and writes expanded YAML before plan/apply | Task 4, Task 5, Task 6 | Covered |
| Deterministic explicit overrides through `modules[].satisfies` | Task 2, Task 4, Task 5, Task 6, Task 7 | Covered |
| Strict-proto provider mapping surface for provider plugins | Task 1, Task 5, Task 9, Task 10, Task 11, Task 12 | Covered |
| Support OTel first, plus Prometheus, Loki, Grafana, and Datadog without Workflow core provider logic | Task 8, Task 9, Task 10, Task 11, Task 12 | Covered |
| Generate concrete `infra.*` modules and leave provisioning to existing IaC pipeline | Task 4, Task 5, Task 6 | Covered |
| No apply-time derivation | Task 6 and out-of-scope manifest | Covered |
| No heuristic existing-resource matching; only `satisfies` keys count | Task 2, Task 4, Task 5, Task 6 | Covered |
| Do not make Workflow core own provider mapping rules | Task 9, Task 10, Task 11, Task 12 | Covered |
| Do not commit app-specific names such as `cms_*` or `multisite_*` to generic plugins | Task 8 and release/migration sequence | Covered |
| Typed requirement fields, enums, vendor extension strings, and JSON bytes only for justified payload exceptions | Task 1, Task 2 | Covered |
| Config-aware requirement discovery through Go interface plus optional strict-proto external-plugin service | Task 1, Task 3, Task 8 | Covered |
| Provider/runtime precedence and non-interactive ambiguity diagnostics | Task 5, Task 6 | Covered |
| Multi-file v1 behavior mutates only the root `--config` file | Task 6 | Covered |
| Observability mapping preferences for OTel, Prometheus, Loki, Grafana, and Datadog | Task 8, Task 9, Task 10, Task 11, Task 12 | Covered |
| Secret placeholders only; reject plaintext secret-looking generated config | Task 5, Task 8, Task 9, Task 10, Task 11, Task 12 | Covered |
| YAML mutation via `gopkg.in/yaml.v3`, preserving order/comments/unknown keys where possible | Task 4, Task 6 | Covered |
| workflow-editor preserves `modules[].satisfies` | Task 7 | Covered |
| Do not implement derivation as a CLI plugin; use IaC provider plugins for mapping | Task 5, Task 6, Task 9, Task 10, Task 11, Task 12 | Covered |
| Backwards compatibility: existing configs/plugins still work and v1 manifest remains valid | Task 2, Task 3, Task 6 | Covered |
| Rollback paths for CLI, proto/service, YAML field, and provider mapper changes | Per-task rollback notes and release/migration sequence | Covered |

**Scope Check:**

| Plan Task | Design Requirement | Status |
|---|---|---|
| Task 1 | Strict-proto requirement/discovery/mapping services and contract registry | Justified |
| Task 2 | Local model, `satisfies`, manifest v2, backwards-compatible manifest parsing | Justified |
| Task 3 | Built-in, manifest, Go-interface, and external-plugin requirement discovery | Justified |
| Task 4 | Safe YAML node mutation and idempotent generated module insertion | Justified |
| Task 5 | Derivation engine, provider/runtime resolution, mapper diagnostics, secret rejection | Justified |
| Task 6 | `wfctl infra derive` CLI behavior, dry-run/write semantics, multi-file root mutation | Justified |
| Task 7 | workflow-editor preservation for `satisfies` | Justified |
| Task 8 | Observability plugin generic requirement emission and backend support | Justified |
| Task 9 | DigitalOcean provider-owned mapping | Justified |
| Task 10 | AWS provider-owned mapping | Justified |
| Task 11 | GCP provider-owned mapping | Justified |
| Task 12 | Azure provider-owned mapping | Justified |

**Manifest Trace:**

| Check | Status |
|---|---|
| `## Scope Manifest` exists | PASS |
| PR count matches PR grouping rows | PASS: 8 rows for `PR Count: 8` |
| Task count matches `### Task N` headings | PASS: 12 headings for `Tasks: 12` |
| Every PR row references existing task IDs | PASS |
| Every task appears in exactly one PR row | PASS |
| `tests/plan-scope-check.sh --plan ...` | Not present in this repository; manifest checked manually using the same invariants |

**Drift Items:** None.
