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
