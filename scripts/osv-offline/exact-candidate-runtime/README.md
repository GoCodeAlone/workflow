# Exact existing-candidate schema witness

This manual hosted job validates only SHA256 `9819a5b582e1f0b6ab135b75c5113baf36f7507a6904e64e2984c318447e8bdf` (115900576 bytes), built from source `81b9b1df2386f7d44c8464c999a4ea0919813526`. It downloads only the approved unpublished draft409874491 asset631609441 through authenticated GitHub reads. Original bytes are preserved; a separate0500 receiver is mounted read-only. There is no Go setup/build/cache or security waiver. GO-2026-5932 remains HOLD.

Use the existing Copilot Setup Steps workflow on `refs/heads/prep/osv-prerequisites-20261011-r1`, repository `GoCodeAlone/workflow`, with `exact_candidate_runtime_proof=true` and both other proof inputs false. Execution requires current independent exact-source reviews first. The agent preparing these files neither dispatches nor executes the job.

Supply `exact_candidate_source_review_json` and SHA256 of those exact UTF-8 JSON bytes (no added newline) in `exact_candidate_source_review_sha256`. This is the authenticated root-supplied review admission, not a caller-created claim of runtime or vulnerability acceptance. The JSON must have exactly these fields:

- `schema="workflow.GO5932.source-review-admission.v1"`, `complete=true`, `accepted=false`.
- `source_manifest_sha256`, `cold_wrapper_sha256`, `workflow_sha256`: hashes of the committed source-manifest.json, hosted.py and whole copilot-setup-steps.yml.
- `security_decision_sha256`, `antagonistic_decision_sha256`: exact current reviewed decisions.
- `security_critical`, `security_important`, `security_advisory`, `antagonistic_critical`, `antagonistic_important`, `antagonistic_advisory`: integer0 after actual review.
- `allows_native_controls=true`, `candidate_runtime_authorized=true`, `artifact_sha256`: exact9819 digest above.
- `operation=["/candidate/wfctl","schema"]`, `workflow_ref="refs/heads/prep/osv-prerequisites-20261011-r1"`.

The receipt is capped64KiB; duplicate/nonfinite JSON, extra fields, missing pins and any mismatch fail before helper imports or network actions. The workflow pins hosted.py first. Its cold guard verifies manifest and all eight helper bytes before any helper import. The exact checkout commit, current hosted event, ref/job/repository, independent seals and whole workflow hash are also bound. Container helpers are the same guarded bytes on validated read-only binds.

GH_TOKEN is available only to fixed outside-container GitHub CLI reads. Closed subprocess environment, fixed GET endpoints, bounded time/streams, exact draft/asset metadata and full downloaded hash are required. No credential/raw authentication stderr is retained. The source tree, compiler, caches, daemon socket, token and incoming download are never mounted as candidate inputs.

Each following phase requires the actual preceding step outcome. Receiver and runtime use one fresh256-bit invocation token. Candidate mode runs all35 actual static-ELF native controls first, then exactly `/candidate/wfctl schema`. Controls need USER_NOTIF/CONTINUE/native notification sizes, LandlockABI>=3 and actual negative-path proof; source tests cannot establish those. Missing support or compatibility is a genuine blocker. The pre-fork manager permits only the trusted child’s first fixed exec, denies all re-exec, and layers process/code-mapping/Landlock restrictions without changing the base filter.

The runtime’s entire caller phase has a90-second deadline including cold guard/startup, controls, target<=30s, private output and exact-ID cleanup, with30s reserved for cleanup. An outer90s timeout/5s forced caller retirement is a failure bound, not extra accepted runtime time. CPU2/memory2GiB/swap0/pids256 and16MiB worker file limit remain unchanged. Schema stdout<=4MiB, stderr<=1MiB; native0/empty stderr/strict schema required. Genuine findings remain HOLD regardless of this operation.

Always-retained artifacts explicitly list receipts, native controls, controller ownership/retirement and schema streams. Both115900576-byte copies, generated executable probes and authentication configuration are excluded. Final green requires actual success of source checks, image pull, authenticated delivery, receiver and runtime. Positive receipts stay accepted:false; stale positive files cannot override a failed phase.

Offline source checks (no Docker, Linux syscall probes, Go or candidate execution):

```sh
python3 -I -S -B scripts/osv-offline/exact-candidate-runtime/test-candidate-runtime.py
python3 -I -S -B scripts/osv-offline/exact-candidate-runtime/test-hosted.py
```

The first suite preserves the24 original file/policy/phase controls. The second adds7 focused cold-start/delivery controls. Their mocks never transfer to actual Linux admission.
