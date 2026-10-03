# Record Host Fixtures

`plugin/` is a real external plugin using the Workflow SDK and an embedded,
matching manifest. It exports a primary step plus an auxiliary catalog type,
and no config fragment. Its execution
audit proves that each failure test reached the SDK step rather than failing
during loader or Docker preflight. Successful output contains no numbers.
Runtime identity/catalog mismatch tests edit only the installed disk manifest
and require the SDK startup audit with no step execution marker.
`foreach.yaml`, `parallel.yaml`, `while.yaml`, `retry-with-backoff.yaml`, and
`resilient-circuit-breaker.yaml` drive the real composite implementations with
SDK sub-steps. Their top-level `result` selects nested SDK fields and stringified
engine counters, not a constant success marker. Each runs with two different
caller-supplied participant pools; exact SDK audits check resolved config,
parent input, prior step output, invocation counts, and ordered retry/fallback.
`compensation.yaml` requires SDK compensation calls in reverse order followed
by the fixed public error record, never the unreachable success result.
All cases require SDK process reaping and empty cleanup journals. The fixture
only supplies operation responses; it does not implement the composites.
SDK outputs are promoted into the runtime's flat `Current` map. Actor lookups
after an SDK call use the existing `.trigger.pool` namespace so the SDK's
string-valued `pool` output cannot shadow the caller's original collection.
The `wait` mode records entry to Execute and waits for RPC cancellation, providing
a readiness boundary for crash tests without introducing a second CLI executor.

`pipeline_record_host_test.go` builds the actual `wfctl` and plugin binaries
with race instrumentation unless a downloaded host binary is explicitly
selected below. The ordinary host tests substitute only `docker/`,
a Docker CLI dependency answering context show/inspect, info, and empty ps
queries. Without an explicit transport fixture it refuses container operations. Engine,
child isolation, real SDK RPC, framing, canonical encoding, and public CLI
stdout are not substituted. These tests do not prove live Docker health or
container execution. All temporary paths are canonicalized before journal use.

Run from the repository root with `GOWORK=off GOTOOLCHAIN=go1.26.5`:

```sh
go test ./cmd/wfctl -run '^TestPipelineRecordHost' -count=1
```

For Task 35 step 10, first verify the selected release archive/checksum and
check out its exact source tag for the SDK fixture and tests. Then opt in:

```sh
GOWORK=off GOTOOLCHAIN=go1.26.5 \
  WFCTL_RECORD_HOST_BINARY=/absolute/path/to/verified/wfctl \
  go test ./cmd/wfctl -run '^TestPipelineRecordHost' -count=1
```

Only the host build target `.` is replaced. The helper copies the selected
regular file byte-for-byte to the test's host path with mode `0700`; missing,
relative, directory, or symlink selections fail without a source-build
fallback. SDK and Docker dependency fixtures still build normally. The
downloaded host is not rebuilt or race-instrumented by this harness. Caller
checksum verification remains required; this selector does not verify a
release. `TestPipelineRecordHostPrebuiltBinary` uses sentinel bytes only to
test copy/fail-closed behavior, not to claim runtime or release conformance.

The opt-in `TestPipelineRecordHostLiveDockerBuiltin` uses the real Docker CLI
and daemon, requires `WFCTL_RECORD_HOST_LIVE_DOCKER=1`, and exercises only a
builtin pipeline plus lifecycle preflight/cleanup. It creates no containers.
Skipped or fake-CLI tests are not live Docker evidence.

`TestPipelineRecordHostLiveDockerSandbox` additionally launches a real strict
sandbox container through the built CLI, Workflow engine, and parent broker.
It requires `WFCTL_RECORD_HOST_LIVE_IMAGE` to name a pre-pulled digest-pinned
BusyBox-compatible image. Its command checks UID/GID, dropped capabilities,
no-new-privileges, read-only root, non-executable `/tmp`, owner-only `/work`,
absence of host credentials, and a request-supplied value read back from tmpfs.
On GitHub Actions Linux runners, the sandbox test runs automatically through
the existing CI test command: it pulls the public image, resolves its immutable
digest, and requires final journal/container readback to be empty. No additional
CI authority or workflow trust-policy change is needed.

`cleanup.yaml` drives the real SDK, Workflow sandbox step, private WFD2 broker,
and parent Docker adapter. Cleanup-matrix tests enable a dependency-only Docker
transport state file: create writes a fake full ID/CID file and label state;
start/wait return fixture output; inspect/ps/rm reconcile that state. No image,
container, command, or isolation boundary is executed by this transport.
An optional create gate and TERM-resistant late-create helper use explicit
ready/release files. Tests kill only the actual host parent after readiness,
then invoke the built CLI again against the same journal/daemon identity.
PID start tokens and process groups are checked; non-running zombies are not
treated as live actors capable of a late create.

`late-stdout.yaml` arms an emitter in one successful SDK Execute RPC. A second
SDK Execute receives the first step's output via the real host, records that
response barrier, and releases the emitter. The host test requires a separate
audit written after a stdout write completes. The oversized case audits a
completed 64-byte late prefix before attempting the tail of its
1 MiB-plus-one-byte payload. This is an attempted oversized late write
interrupted by zero-stdout quarantine, which cancels at the first byte; it
does not prove full payload transmission or a stdout-overflow flag trip.
Actual prefix-write counts and requested size are checked; cap enforcement
remains covered by stderr-overflow and capture tests. No sleep is used to manufacture
late output that shutdown could kill before emission. Both ordinary and oversized
late stdout are tested, along with bounded stderr and stderr overflow. The
descriptor mode probes FD3 through FD6, including private result FD4/control FD5.
