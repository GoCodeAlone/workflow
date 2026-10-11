# Offline scanner

The pinned container prerequisite passed at commit
`6150c3712d984942889436b5d4e53ba9ce5bc41c`,
[hosted run38156036062](https://github.com/GoCodeAlone/workflow/actions/runs/38156036062).
Production execution and scanner admission require their own actual evidence.
The permanent replacement is authorized; all five admission decisions remain
unbound pending independent review.

Production analysis and reporting run in one nonroot, offline container with
memory2GiB, swap0, pids256 and CPU2. Every invocation witnesses the actual
namespace, descriptor, socket/syscall/ABI, readonly and quota controls. Readonly
inputs cannot have writable aliases. Only owned worker/runs/home/tmp state and
the separate existing build cache are writable.

The host prepares authenticated public modules and fixed Go/npm database
archives. [SUPPLIER.md](SUPPLIER.md) describes the source-conserving supplier
build. Actual paths bind to fresh same-job delivery evidence; admission does
not reuse paths from a previous VM. Credentialed uploads remain host-side.

`manual-proof-plan.json` permits finite dispatch on the owned ref and fixed
candidate/base commits. Its collector preserves the actual event, raw findings,
finding exit1 and incomplete exit2. The immutable code/runtime/profile contract
is separate from the admission index. Manual proof cannot supply the four
normal-event delivery proofs or authorize automatic installation.

Host-owned creation and retirement records sit outside worker writable mounts.
Shared deadlines reserve cleanup. The collector attempts authenticated closure
even after a phase failure; raw publication requires full-ID retirement first.

GO-2026-5932 and the original baseline loading failures remain genuine holds.
Forward fixture repair requires direct committed-fixture readonly load/tidy/test
checks plus focused conformance behavior tests. An artifact-operation exception
does not waive full-source findings. Historical systemd/bubblewrap sources are
retained; production uses the approved container backend.

Offline controls execute no Docker, Go build, network or namespace probes:

```sh
python3 -I -S -B scripts/osv-offline/test-osv-container.py
python3 -I -S -B scripts/osv-offline/test-container-seccomp.py
python3 -I -S -B scripts/osv-offline/test-container-worker.py
python3 -I -S -B scripts/osv-offline/test-container-proof.py
python3 -I -S -B scripts/osv-offline/test-production-manager.py
python3 -I -S -B scripts/osv-offline/test-prepare-supplier.py
```
