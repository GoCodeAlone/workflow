# Exact existing-candidate schema witness

This finite manual trial validates only SHA256 `9819a5b582e1f0b6ab135b75c5113baf36f7507a6904e64e2984c318447e8bdf` (115,900,576 bytes), built from source `81b9b1df2386f7d44c8464c999a4ea0919813526`. Approved draft release `409874491` and asset `631609441` remain unpublished. Original bytes are preserved and a separate immutable receiver is mounted read-only. No Go build, compiler cache or vulnerability exception is part of this witness. GO-2026-5932 remains HOLD.

Use Copilot Setup Steps on `refs/heads/prep/osv-prerequisites-20261011-r1` in `GoCodeAlone/workflow`, with `exact_candidate_runtime_proof=true` and both other proof inputs false. Both current independent source reviews and one root-issued dispatch ledger precede the trial. Run attempt must be 1. The two jobs have separate hosted runners:

- `exact-candidate-acquisition` has a six-minute timeout and job-only `contents:write`. The ephemeral token is passed only to fixed GitHub CLI GETs outside execution. Token expiry follows job completion; this timeout is not a cryptographic six-minute TTL.
- `exact-candidate-runtime` has `permissions: {}` and consumes one exact artifact ID from the same actually successful acquisition run and attempt. No checkout or GitHub token is needed in this job. Actions service credentials stay outside the closed controller and container environment.

Supply `exact_candidate_source_review_json` and the SHA256 of its exact UTF-8 bytes in `exact_candidate_source_review_sha256`. Admission has exactly these fields:

- `schema="workflow.GO5932.source-review-admission.v1"`, `complete=true`, `accepted=false`.
- `source_manifest_sha256`, `cold_wrapper_sha256`, `workflow_sha256`: inherited manifest, inherited `hosted.py` and complete current workflow hashes.
- `split_wrapper_sha256`, `source_binding_sha256`, `source_rows`: new cold controller hash, exact source-review binding and complete twelve-file byte/hash bundle inventory.
- `security_decision_sha256`, `antagonistic_decision_sha256`: both current independent source decisions.
- `security_critical`, `security_important`, `security_advisory`, `antagonistic_critical`, `antagonistic_important`, `antagonistic_advisory`: integer 0 after actual review.
- `allows_native_controls=true`, `candidate_runtime_authorized=true`, `artifact_sha256`: the exact digest above.
- `operation=["/candidate/wfctl","schema"]`, `workflow_ref="refs/heads/prep/osv-prerequisites-20261011-r1"`.

Also supply `exact_candidate_trial_json` and its exact UTF-8 SHA256 in `exact_candidate_trial_sha256`. The root issues schema `workflow.GO5932.single-trial.v1` with the recorded owner approval SHA, fresh 64-hex `trial_id`, literal UTC `not_before_utc`/`expires_utc` within two hours, actual `head`, exact `review_sha256`, consumed `issuer_ledger_sha256`, integer `dispatches=1` and `accepted=false`. Root-issued times are operational fields, not owner-supplied dates. The operator ledger and attempt guard do not create a global GitHub replay lock. Same-action, destination and scope retries remain covered by the owner approval; each manually scheduled attempt needs its own current source admission, fresh trial/nonce and consumed per-attempt ledger. Failed historical results are preserved, with no automatic retry loop.

The workflow authenticates `split-hosted.py` before isolated Python startup. The controller verifies every bundle byte, current event/job/run/attempt/head, trial, review and inherited eight-helper manifest before any subject helper import. Public source fetches use fixed current-commit URLs without credentials, redirects or retries; no checkout is used. Each source file is bounded at 64 KiB and the complete bundle at 512 KiB.

Acquisition uses only the fixed release and asset GET endpoints, rechecks draft state and verifies all downloaded bytes. A credential-free bounded `gh api --help` preflight determines whether that actual CLI supports `--allow-escape-sequences`; the flag is used only for the private binary sink. Binary bytes and authentication stderr are never displayed. Failures retain native rc/byte counts/hashes plus exact fixed GET endpoint and metadata/binary role. There is no PAT, persistent token, draft publication or alternate denied-client retry.

The one-day carrier contains the exact executable, guarded source bundle including the hidden workflow, authentic acquisition identity and receipt, review and trial. Its broader signed-in public-repository artifact readership is explicitly owner-approved. The pinned downloader uses the producer's exact same-run artifact ID and `digest-mismatch:error`; complete file-set, byte, run/attempt/head, nonce and actual producer outcome checks precede imports or execution. Runtime records a separately labelled derived receiver transport while retaining the acquisition identity. It does not relabel acquisition as runtime.

Candidate mode runs all 35 actual syscall controls before exactly `/candidate/wfctl schema`. USER_NOTIF/CONTINUE, notification sizes, Landlock ABI and negative paths require real Linux proof; mocks cannot establish them. The manager permits only its trusted child's first fixed exec and denies later exec, process creation and unapproved code mappings. Unchanged CPU 2, aggregate memory 2 GiB, swap 0, PID 256, read-only/no-egress mounts, descriptor guards and exact container-ID retirement remain mandatory. The caller's 90-second accepted deadline includes controls, target, output and cleanup with the existing cleanup reserve; outer 90-second timeout is a failure bound.

New carrier, proof and failure artifacts use one-day retention. Proof artifacts list bounded receipts, controls, ownership/retirement and schema streams; they exclude executable copies and authentication configuration. Existing scanner artifacts retain their separate lifecycle. No deletion permission is inferred. Positive receipts stay `accepted:false` and provisional until actual current job success. A failed source, acquisition, upload, receiver, image or runtime phase cannot become green through a stale receipt.

Offline controls do not execute Docker, Linux syscall probes, Go or the candidate:

```sh
python3 -I -S -B scripts/osv-offline/exact-candidate-runtime/test-candidate-runtime.py
python3 -I -S -B scripts/osv-offline/exact-candidate-runtime/test-hosted.py
python3 -I -S -B -X pycache_prefix=/absolute/fresh-absent-prefix scripts/osv-offline/exact-candidate-runtime/test-split-hosted.py -v
```

The runtime suite retains all31 prior controls (including seven cancellation/ioctl controls) and adds11 exact diagnostic/schema/conservation controls; the hosted suite retains its seven controls. The split suite requires a fresh absent bytecode prefix and tests admission, carrier provenance, token separation and fixed denial attribution with explicit mocks. None supplies hosted acceptance or finding clearance.

A canceled target may make notification RECV return ENOENT. Only that typed RECV error is retried with a new zeroed notification buffer under one absolute receive timer and deadline; the timer is never restarted. All other ioctl errors fail closed. Received flags/architecture/syscall checks, ordered PID/ID/CONTINUE validation and the original canceled-notification deadline oracle remain unchanged. This source correction needs a fresh all-35-control Linux witness before candidate execution.

For this exact existing artifact/source/buildinfo and fixed schema operation only, the reviewed proof may classify empty stderr or the exact 165-byte Sonic v1.15.2 Go1.27 standard-library fallback initializer notice. The full raw stderr stream, size and SHA256 remain evidence; no bytes are filtered or rewritten. The manifest's exact source/artifact/Go1.27.2/Linux-amd64/CGO0/Sonic h1 subject must match before launch. Native0 and the existing duplicate/nonfinite-rejecting schema predicate remain mandatory. Any differing/additional message, invalid schema, nonzero native exit or subject drift refuses. All35 native controls, executable-memory protections and post-execution artifact/empty-operation conservation remain unchanged. `candidate_process_exit_observed` records a returned brokered native process separately from `candidate_execution` readiness; neither is finding/release/install acceptance. Old strict-failed receipts remain unchanged.
