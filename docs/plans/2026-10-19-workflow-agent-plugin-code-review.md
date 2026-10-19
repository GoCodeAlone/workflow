# Independent package review

Reviewer: `package_skeptic`, read-only, 2026-10-19.

## Round 1 — REQUEST-CHANGES

Full diff `origin/main..1383f64`; structural scope gate PASS.

| ID | severity/class | location | finding | resolution |
|---|---|---|---|---|
| C1 | Important / missing edge cases | `tests/mcp_smoke.py`, Client.close | Broken stdin close could throw before child reaping. Real child reproduced the leak. | `a3331d3`: catch pipe failure, continue teardown; real-child regression. |
| C2 | Minor / concurrency | `tests/mcp_smoke.py`, Client._read | Blocking queue writes could strand daemon reader after shutdown. | `a3331d3`: stop event + interruptible bounded puts; full-queue regression. |
| C3 | Minor / missing edge cases | `tests/mcp_smoke.py`, Client.request | Notifications renewed a 10s timeout up to 100 times. | `a3331d3`: one monotonic request deadline; notification/elapsed-time regression. |

All paths in finding locations are relative to `example/workflow-agent-plugin/`.
Each root-cause fix was independently reverted/restored: expected FAIL → PASS.

## Round 2 — SHIP-IT

Full diff `origin/main..a3331d3` re-read, all mandatory classes scanned. No open
Critical/Important/Minor findings. Reviewer reran 17 tests and real v0.86.2 smoke
(33 tools, release checksum unchanged); diff check PASS. Final historical test
counts were reconciled in the evidence document before publication.

| mandatory class | result |
|---|---|
| Symmetry | Clean: three skills share contract. |
| Error swallowing | Clean: safe categorization intentional, broken pipe no longer interrupts cleanup. |
| Comment/code drift | Clean after final evidence count reconciliation. |
| Test-name/body mismatch | Clean: regression names match failure paths. |
| Missing edge cases | Clean: lifecycle/notification cases covered. |
| Concurrency | Clean: producer observes shutdown; reader termination checked. |
| Type coercion | Clean: bool IDs rejected, malformed data fails safely. |
| Dead/unreachable code | No tangible finding. |
| Scope drift | None. |
| Shortcut/band-aid fixes | Clean: fixes target actual lifecycle/deadline causes. |
| One-sided boundary wiring | Clean: real manifest/released-wfctl crossing. |
| Declared integration unused | Clean: host/coexistence explicitly config-only; activation deferred. |
| Hermeticity/non-determinism | Clean: isolated children/doubles, explicit release executable. |
| Portability | Clean: placeholders/direct launch, no certification claim. |
| Vacuous assertions | Clean: process/reader/deadline and actual release behavior observed. |

Scope gate covers all three user workflows, direct public wfctl reuse, standalone
fixtures, negative/privacy tests, bounded evidence, preserved Autodev/Compute,
and no forbidden hooks/installer/platform/hosted service/telemetry. Source
publication is the remaining Task 3 operation. Full desktop activation is an
explicit authorized deferral, not inferred integration proof.

## Go conversion — independent REQUEST-CHANGES

The user requested Go verification; ADR 0065 replaces the initial Python test
implementation. Historical rounds above do not certify this replacement.

| ID | severity/class | finding | resolution |
|---|---|---|---|
| G1 | Important / silent coercion | Invalid UTF-8 is replaced by Go JSON encoding while original bytes were hashed; real released smoke reproduced a false tested-snapshot identity. | Strict fixture UTF-8 rejection before child launch; strict protocol decode too. Independent real-binary repro now fails safely without evidence; launch-marker regression and revert/restore proof pass. |
| G2 | Minor / vacuous assertion | A short notification timeout could expire before a slow child sends notifications. | Child readiness barrier plus at least two consumed notifications; deadline-renewal mutation fails and restored test passes. |

Independent reviewer mapped all four prior package contracts and thirteen smoke
contracts to Go equivalents, verified unchanged schemas/six fixture digests,
focused CI wiring and bounded host claims. Race tests, vet and released MCP
passed before these findings; passing happy paths did not resolve G1/G2.
Additional focused cancellation coverage and removal of unnecessary Unix PATH
assumptions are included in the correction. All mandatory bug classes were
scanned; no feature/platform scope drift found.

Corrected Go verification independently passed: 22 substantive tests plus helper
under race detection (7.561 seconds), vet, actual v0.86.2 smoke (33 tools, same
binary/fixture digests), and the actual root integration test file (8.953 seconds
including nested race suite). Root wiring preserves the existing workflow trust
policy and introduces no workflow/authority change. Installed-host discovery
claims remain bounded to the separately reviewed disposable test.

## Corrected Go round — SHIP-IT

Independent final scan: no Critical, Important or Minor findings remain. All
mandatory classes were rescanned: symmetry, error handling, comment/body drift,
edge cases, concurrency, coercion, dead code, scope, root causes, boundary wiring,
integration use, hermeticity, portability and vacuous assertions. This verdict
approves bounded draft source, not complete customer host/runtime acceptance.
Exact-head hosted CI is an external acceptance gate tracked on PR 1083.

Hosted lint on `2289194` found SA1019 in the root integration test's use of
deprecated runtime.GOROOT. The correction uses standard PATH-based `exec` lookup
for Go, without suppressions. Independent narrow review: SHIP-IT; exact-file vet,
race test including nested suite (8.975 seconds), diff check all pass. Official
CI linter v2.14.0 was checksum-verified and run task-locally against that file:
zero issues. Package source/tree/archive are unchanged by this wrapper fix.
