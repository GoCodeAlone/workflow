# Source Security Code Review

## Go27 Delta Review 2026-10-04

Independent Ramanujan reviewed33b4 Go27/source-generated repairs, then only
the new ADR0061/0062 MCP/default-doc/DHI/test delta at bca10b34 plus owned
working changes: SHIP-IT, no Important/Critical findings. Parsed actual MCP
CD output and config-only release helper require literal1.27.1; default init
checks preserve existing coverage; version test retains real stderr/final-line
behavior and diagnostics. DHI builder changes only the official declared
tag/builder Alpine minor, not vendor/dev/CGO0/nonroot runtime hardening.

A new native-cold failure warranted one separate root-test-only review:
6000RPM refills during real50ms HTTP burst, so200 was legitimate. Corrected
60RPM/derived1100ms wait and25ms client pause retain all three stages and
exact denial/recovery/JSON/configured-handler assertions. Review SHIP-IT,
no Important/Critical findings; no production limiter/clock/deadline change.
Old-rate control fails the429 assertion; corrected focused race/lint/vet pass.
Scheduling stalls >=1s remain visible failures, not skipped or retried.

CI B2/bootstrap and DHI401 publication/compiler/build are still HOLD;
final corrected frozen whole-cold/runtime gates are lead verification, not
cleared by this review. Three tasks/one source PR and protected bytes retained.

2026-10-03; independent bounded review of550c0f7a against49e18034.
Tasks1/2 independent spec/quality PASS retained; final reviewer did not author
Task3. Read-only review inspected code, lock graph and retained real evidence;
lead reran UI verification after the only finding's resolution.

## Scope And Finding

| Check | Result |
|---|---|
| 73 protected paths/direct UI manifest | Byte-identical to base |
| Go code/modules vs cold40d5d6e6 | Unchanged |
| Ten targets/editor consumer | Patched duplicates; real RED/reversal RED/released0.2.1 GREEN |
| Critical/Important defects | None |
| Minor M1 unnecessary chai/tinyrainbow refresh | Restored6.2.2/3.1.0 through isolated npm resolver; only two package entries differ from reviewed head |
| M1 validation | Clean npmci0; full141 tests; tsc/Vite0; focused lint0; ten-family installed audit0; manifest/root-lock unchanged |

Disposition: SHIP-IT for source review, not release authorization. Final
cf28e5a4 artifacts/native production scan subsequently pass; exact-head
PR checks/merge remain mandatory.
No new design or broad review cycle warranted for M1.

## Bug Classes

| Class | Evidence/result |
|---|---|
| Symmetry | Both real parser paths receive same valid/syntax/budget cases |
| Error swallowing | Build/copy errors propagate; independent fallback retained |
| Comment/code drift | M1 closure description corrected |
| Test-name/body mismatch | Actual syntax and budget paths exercised |
| Missing edges | Exact budget/+1, malformed syntax, two resource/participant pairs |
| Concurrency | Mutex covers initialization/copy/cleanup; concurrent coverage |
| Silent coercion | Complete parsed structure asserted |
| Dead code | Initialization/reuse/fallback/cancellation/close exercised |
| Scope drift | M1 fixed; protected bytes/direct manifest preserved |
| Band-aid | Real RED/GREEN; no weakened assertion/deadline |
| One-sided wiring | Actual public editor export/SDK/Docker/registry consumers |
| Unconsumed integration | Runtime evidence retained; OSV pending separately |
| Hermeticity | Local bounded inputs; process-owned temporary custody |
| Portability | Portable cache; Windows compile evidence |
| Vacuity | Old-dependency reversal reproduces rejection failure |

Runtime evidence inspected: cold152 packages, wfctl519.387s under600s; live
strict Docker36.661s; Linux registry control and30 denials30.169s; UI141
tests/build. Unfinished scanner/CI gates are not counted as fulfilled.
