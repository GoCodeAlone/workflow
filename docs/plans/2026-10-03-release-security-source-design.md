# Workflow Source Security Remediation

**Status:** Approved; design review PASS with two proof refinements incorporated.
**Baseline:** main `49e1803424f303f6f98cef51cc9ea2dee96c7d38`.
**Authority:** independent source-only maintenance. Existing Signal scope unchanged.
**Decision:** `decisions/0056-isolate-source-security-remediation.md`.

### Backport 2026-10-04: Existing Test Boundary Corrections

Evidence: cold Linux checkpoint33b4 failed the two init default-module
expectations and linked-version combined-output assertion. Defaults correctly
omit optional external observability plugins per ADR0059; the final version
line is v9.9.9 and stderr also includes a dependency compatibility warning.
The focused channel probe disproves a stdout assumption: the existing handler
uses stderr for version output, with empty stdout.
See decisions/0062-align-scaffold-and-version-test-boundaries.md.

Correction: Task2 additionally names init_test.go; assert absent optional
types without dropping existing file/module/UI checks. Already-owned
main_test.go checks empty stdout and the exact final stderr version line,
retaining preceding stderr diagnostics/logs.
No warning suppression, dependency change or production-version change.
The existing default-registry/health/runtime tests cover the product invariant;
this is a stale expectation/channel correction, not a new product feature.
Manifest scope stays three tasks/one existing PR, protected exclusions intact.

## Goal And Requirements

Ship reviewed, patched Workflow source and reliable real-host tests without
modifying protected CI authority, unrelated staged work, or release tags.

### Backport 2026-10-04: Native Linux Proof Custody And DHI Builder

Cold33b4 used an unregistered UID501 and host VirtioFS source/temp/cache paths.
The resulting home-resolution, inode, lock, permission and LRU failures are
environmental: unchanged33b4 with registered UID1000, container-native source/
temp/cache and Docker --init passes every matched case (wfctl173s, LRU2s), with
unchanged tracked source. Final cold proof uses that corrected custody; retain
the exact CI test command/default600s limit and move coverage output outside
source before clean-status readback. No filesystem/test-runtime product changes.

The immutable official DHI catalog615555cd declares Go1.27.1 dev only on
Alpine3.23, not3.22. Use exact dhi.io/golang:1.27.1-alpine3.23-dev while retaining
DHI/dev/musl, CGO_ENABLED=0 and the existing nonroot static3.22 runtime. This is
a builder-OS-minor change, not a vendor/runtime/hardening change. Official
metadata declares amd64/arm64 and compiler1.27.1; both old/new registry probes
return401, so published digests/native compiler/image build remain unverified
until host authentication. No guessed3.22 tag or substitute image proof.
Both corrections stay Task2/S4/S6; three tasks/one PR and CI exclusions unchanged.

### Backport 2026-10-04: Rate-Recovery Fixture Scheduling

Corrected native cold ee6eb078 exits1 solely on root rate-recovery test: the
6000RPM fixture refills a token every10ms, but its two HTTP burst requests take
50ms; the subsequent200 is valid refill, not a middleware/compiler defect.
wfctl passes460.576s under unchanged600s; native filesystem/home failures do not
recur. The isolated fast-host probe passes, confirming a scheduling assumption.

Task2/S4 additionally names e2e_middleware_negative_test.go. First demonstrate
the invalid10ms assumption with a25ms client pause through the actual engine/
HTTP test. Then use60RPM and derive a one-token refill wait plus100ms from that
configuration, matching the existing module-test precedent. Preserve all three
subtests, exact success/429/handler-body assertions, HTTP5s/default-package
deadlines and production middleware bytes. No clock mock, skip or timeout
increase; the changed wait represents the configured rate. Rerun focused
RED/GREEN/reversal and the whole cold command on frozen corrected source.
This test-only proof backport adds no product invariant/feature; manifest
three tasks/one existing PR and protected exclusions remain unchanged.

The approved2026-10-04 backport below supersedes earlier1.26.8/generator/Docker
exclusions; earlier evidence remains historical, not finalGo1.27.1 proof.

- S1: Root, example, and external-plugin fixture Go floors become 1.26.8;
  affected OTel modules become 1.45.0; vulnerable gRPC/x-text fixture pins use
  established root 1.83.2/0.42.0. Tidy each owning module. Source fixture
  compiler overrides and runnable minimum-version docs match 1.26.8.
- S2: Patch the ten affected npm package families within existing compatible
  ranges, preserving unrelated lock resolutions and direct dependencies.
  Add a real js-yaml merge-budget regression and exercise the actual imported
  `@gocodealone/workflow-editor/utils` `parseYaml`; run the UI suite/build.
- S3: Lazily reuse only the immutable source-built record host (`.`, race),
  matching cwd, compiler and complete build environment. Changed inputs use
  the original uncached path. Explicit downloaded-binary selection/denial runs
  first. Copy to distinct mode-0700 paths; remove the owned cache before exit.
  SDK, Docker, registry, mutant, and capability builders remain independent.
  Preserve GORACE, all deadlines/cases and the default 600s package limit.
- S4: Prove original-source failures, new cache semantics, full cold
  race/coverage completion, Go package/build metadata, actual Workflow HTTP
  startup, SDK/composite/custody matrices, strict Docker and registry matrices,
  UI build/test and version-skew audit. New source must be lint-clean.
- S5: Preserve CI/workflow/policytool bytes and staged contexts. Exact Go
  1.26.5 policytool reachability proof is required. Independently run supported
  OSV v2.6.0 against source plus an isolated called-vulnerability control,
  using verified image index digest
  `sha256:71ad04ab2f8798be47870f9b18817ad317c2f8f2f97aa6726ba10d5578bc174a`;
  retain tool identity and analyzed JSON. No scan suppression, ignores or clean-CI claim for the
  old pinned main scanner. Source PR merge needs all required PR checks green.

## Alternatives

| Option | Decision |
|---|---|
| Hold all source fixes until unrelated trust rollout settles | Reject: independent production remediation can proceed |
| Source-only patch with real runtime/native analysis proof | Choose |
| Rewrite staged CI/harness authority or weaken environment policy | Reject: owner reconciliation required |

## Global Design Guidance

Repository `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`
and `docs/public-workflow-policy.md` apply. Reuse Go/npm resolvers, existing
runtime fixtures and upstream scanner. No new plugin, CLI, app, framework,
scenario, provider, Go minor migration or CI-policy exception.

## Security Review

- Main/base full scans have identical 159 findings, 157 fixable. Code-based
  triage found plausible shipped TLS/relative-URL DoS and UI YAML CPU DoS;
  unchanged baseline is not risk acceptance. Patch owning dependencies.
- Go drivers in existing CI stay pinned 1.26.5; their authenticated default
  selection must compile the new module floor with 1.26.8. Check actual binary
  build metadata, not the job label. No production tag is created here.
- Policytool Go 1.26.5 and scanner CI rollout stay outside this source boundary;
  prove no affected policytool calls using that exact compiler. Called findings
  or analysis errors block progress. Old main OSV remains a disclosed issue.
- Source cache contains only a test executable in an owned private directory;
  no credentials/input/result data, persistent custody store, or runtime cache.

## Infrastructure Impact

No workflow, runner, IAM/token, deployment, resource or data change. Temporary
local compiler/scanner containers and test services only. Preserve automatic
repository-token policy and concurrent v0.86.1 tag/run.

## Multi-Component Validation

| Integration | Class | Proof |
|---|---|---|
| compiler + Workflow server | runtime-integrated | actual built server/YAML/HTTP request plus compiler metadata |
| wfctl + real SDK/plugin/Docker | runtime-integrated | complete existing host, late-output, composite and crash matrices; live strict sandbox |
| wfctl + registry + Git | runtime-integrated | default-API Linux CLI accepted/rejected snapshot matrix |
| UI + js-yaml | runtime-integrated | actual installed parser regression, UI tests/build |
| upstream OSV + Go loader + reporter | runtime-integrated | successful native analysis, preserved classification, outside-tree called-vulnerability control rejected |
| existing CI policy | config-only | no byte changes; existing positive/mutation suite |
| scanner CI rollout/fleet upgrade/Signal promotion | separate | user approved CI reconciliation; this source-only PR does not perform it; existing Signal tasks remain open |

## Assumptions And Self-Challenge

| ID | Claim | Verification/Failure |
|---|---|---|
| A1 | Four avoided duplicate host builds provide sufficient margin | Whole cold Linux race/coverage suite must finish under 600s; otherwise backport, do not silently extend limits |
| A2 | Cache cannot hide a compiler/environment override | Changed-input and error regressions; explicit prebuilt selection still first |
| A3 | Dependency patches preserve typed SDK/runtime contracts | Real host/HTTP/registry/Docker matrices and all required CI |
| A4 | Module floor drives actual compiler 1.26.8 | Older-driver probe and `go version -m` on built artifacts |
| A5 | Remaining module-only findings are uncalled | Successful analyzed JSON, per-release-platform OpenPGP absence, exact policytool compiler proof; no suppression |

## Rollback

Reviewed revert of the single source-remediation PR, rerun build/runtime tests,
and hold promotion because known vulnerabilities return. No workflow trust,
tag, registry, deployment or data rollback occurs.

### Backport 2026-10-04: Approved Go And Generated Toolchains

Authority: explicit operator Go1.27.1/CI/admin instruction; ADR0057 records
original locked timestamp/hash and owning-file expansion. S1 now targets
Go1.27.1 in root/example/eleven fixtures and source-host expectations. S6 adds
existing wfctl app/plugin scaffold modules, workflow/release/Docker templates,
legacy generate-CI emitters, deploy Docker generator, SDK minimum/default Go
constants, binary-build default (explicit overrides preserved), maintained
admin/legacy Docker builders, and cigen CircleCI cimg/go:1.27.1. Official
CircleCI sources/registry confirm Linuxamd64/arm64 image availability.

S4 requires literal independent version regressions (RED/GREEN), actual built
wfctl-generated app/plugin output and build/launch, exact-head four-command
metadata/server smoke, new nativeGo1.27.1 host/SDK/registry/Docker/coldLinux
proof, and compatible golangci-lint2.14.0. Existing dependency/UI fixes remain.
Generated instructions and active prerequisites align; historical evidence
and deliberate explicit test overrides are not mechanically rewritten.

S5 still excludes all protected CI/scripts/policy/trust writes. Its policytool
minimum remains1.26.5; Go1.27.1 execution/CI drivers are separately governed.
Native prior1.26.5 reachability evidence remains disclosed; new source must
pass all actual hosted checks after the CI rollover. Approved admin exception
affects ordinary review only; no settings, failed-check or policy bypass.
Manifest amendment: unchanged three tasks/one PR; Task2 gains S6/Go-minor and
maintained generator/Docker ownership. No fleet, release or Signal completion.

### Backport 2026-10-04: Real Generated Engine Startup

Actual default BuildBinaryStep factory/Execute emitted three files withGo1.27.1;
native generated-project compilation succeeded after disclosed local resolver
replacement, but the real binary exited1 with unknown http.server module.
Cause: emitted NewStdEngine constructs an empty factory registry, unlike the
existing app scaffold's NewEngineBuilder.WithAllDefaults/setup registration.
The first correction still failed real HTTP startup: WithAllDefaults omits
module plugins. Reuse plugins/all.DefaultPlugins in the existing builder and
all three application main templates. Correct the owning bootstrap and add a
regression; rerun actual emitted binary HTTP/lifecycle proof. No mock/fake app,
accepted compilation error or empty config proves this boundary. Task2 already
owns generated-runtime proof; ADR0058 explicitly adds the three main templates
to owning files, preserving three tasks/one PR and protected-file exclusions.

### Backport 2026-10-04: Strict Generated StringValue Runtime

Actual generated plugin compiles, starts, loads through the real host, then
fails configuration. Initial wrong main.Version probe flag was corrected to
the generated internal.Version before causal analysis; it is not a product bug.
Host mapToTypedAny sends object JSON to scalar StringValue, and typedAnyToMap
would decode scalar JSON into an object. Keep the generated strict wire schema;
adapt only StringValue's value field with canonical protojson validation,
empty-map default and strict unknown-field rejection. No legacy fallback or
other well-known type changes. ADR0058 adds external convert.go/tests to Task2;
native RED/GREEN, strict negative cases and real uppercase pipeline execution
must verify both directions. Three tasks/one PR and CI exclusions unchanged.

Independent code review S1: Go Windows Process.Signal rejects SIGTERM. Retain
both generated native compilation modes on Windows, then explicitly skip only
the POSIX lifecycle portion before process launch. Darwin/Linux continue to
prove real HTTP and graceful SIGTERM; forced kill is never graceful evidence.
Windows cross-compilation is compile-only, not a native lifecycle receipt.

### Backport 2026-10-04: Default App Config Has External Modules

Actual wfctl init API output, compiled withGo1.27.1 and its corrected default
plugin bootstrap, exits1 because observability.telemetry/collector are external
plugin types, not built-ins. API/full-stack templates omit their installation
and discovery. Per ADR0059, remove only those optional external modules from
default configurations and disclose explicit opt-in composition in existing
READMEs. Assert every generated app module against the actual default registry;
rerun the real API config, changing only loopback address/response identity in
the proof. No alternate-type substitution or proof-only config deletion.
Task2 adds the two YAML templates; three tasks/one PR and CI exclusions remain.

### Backport 2026-10-04: Default Health Route Is Not A Handler

Fresh generated API now initializes all modules, then rejects its manual GET
/health route because the named health.checker service is not an HTTP handler.
All three app configs share that invalid route. Per ADR0060, set healthPath to
/health and let the existing observability hook mount its real health handler;
remove only the invalid manual route. Test generated paths/routes RED/GREEN
and rerun real API/health/shutdown. Task2 adds event-processor YAML alongside
the existing two YAML owners; no new core API or endpoint/feature removal.

### Backport 2026-10-04: Remove Redundant Invalid Trigger Blocks

The real API now reaches trigger configuration, then exits1 because the emitted
http-trigger key is not a registered trigger type. All three templates use this
obsolete named/type/config shape; event-processor also has event-trigger. Actual
HTTP/event trigger configuration expects type-keyed routes/subscriptions, not
the nested server/topic blocks. Existing workflows.http already configures the
real API routes; workflows.messaging already configures the event subscriptions
and producers. Remove only these redundant invalid top-level trigger blocks,
retaining those real workflow sections unchanged. Add actual default trigger
registry assertions RED/GREEN and regenerate/launch the API app again. This is
Task2C in its existing three YAML owners: no manifest, task/PR count, engine API,
or application-route/subscription removal. Do not repair only the proof config.

### Backport 2026-10-04: Complete The Maintained-Default Inventory

The structured33b4 version audit proves thirteen source module floors and five
scaffold workflow pins match1.27.1, but finds two old MCP legacy driver literals,
an old maintained e-commerce DHI builder, and five active prerequisite/default
claims. Per ADR0061, extend Task2's owning files for those defaults only; parsed
MCP emitter/actual CD handler regression RED/GREEN, verified DHI tag/compiler,
and updated source audit/frozen gates are required. Do not implement new MCP
plugin detection or count the legacy release helper as a proved host feature;
its existing HasPlugin condition is unchanged. Historical receipts and explicit
test overrides remain. Three tasks/one PR, no protected CI or runtime-image
hardening change. The current33b4 cold check predates this owning-file repair.

### Backport 2026-10-03: Bootstrap Pins And Actual Toolchains

Cause: full skew audit found existing Go 1.26.5 scaffold/generator, Docker
bootstrap and deployment-documentation pins, not just protected CI/policytool.
Changing those product defaults is the separately deferred fleet/generator
boundary; the source-only patch does not silently perform it.

Evidence: root/example/eleven owning fixture floors and relevant fixture
overrides are 1.26.8; old `go1.26.5+auto` driver builds patched source with
actual 1.26.8 metadata. Exact policytool 1.26.5 analysis has eight module-only
stdlib rows and zero called findings; its wrapper passes unchanged. Forcing
`GOTOOLCHAIN=go1.26.5` across the root mutation harness prevents root-level
helper compilation. CI-equivalent authenticated auto selection runs all
mutation assertions, but concurrent worker edits invalidate its production
immutability snapshot; repeat once in a frozen, noncompeting plain checkout.

Correction: audit classifies every residual pin as protected driver, standalone
legacy scaffold/bootstrap, historical example or pending fleet follow-up.
Never call this a completed fleet upgrade. Nested Git worktree artifact VCS
metadata reports the outer checkout: frozen plain-checkout verification is
required for exact-head evidence. Local path/actual CLI and compiler checks
establish source behavior, not a release. Scope: no manifest/task/PR change.

### Backport 2026-10-03: Bundled Editor Parser Prerequisite

Cause: installed editor 0.2.0 embeds its own js-yaml implementation. Updating
Workflow's external lock cannot patch it. Real consumer regression remains
RED (140 tests PASS, one editor hostile-merge test FAIL); direct updated
js-yaml tests and UI build pass.

Correction: independently design/review/patch/release compatible editor 0.2.x,
then resolve it through existing Workflow `^0.2.0` range. Preserve current
Workflow manifest/PR grouping; no parser mock, UI shim, externalization shortcut
or weakened assertion. The adjacent owning-library fix has its own design and
PR, not an unrecorded edit in this source-only manifest.

### Backport 2026-10-03: Match CI Filesystem Privilege

Cause: the first local container used a read-only checkout/tmpfs and was not a
valid CI environment. Its writable replacement ran as root: wfctl finished in
542.027s, but root bypassed the intentional read-only lockfile denial and could
create `/nonexistent/path`, contaminating three subsequent missing-path tests.
Neither run is full-suite GREEN or a reason to modify tests or their deadlines.

Correction: cold-cache proof uses a fresh complete plain checkout, writable
owned source copy and a non-root CI user, with disk-backed compiler cache.
Verify exact HEAD/clean tracked bytes before execution. Preserve the unchanged
`go test -v -race -coverprofile=coverage.out ./...` command and 600s package
timeout. Classify environmental failures separately from code regressions; no
retry without a falsifiable corrected cause. Scope: no manifest/task/PR change.

### Backport 2026-10-03: Reproduce CI Init/Reaping

Cause: non-root `runuser` as container PID 1 does not reap adopted helper
orphans; inherited-FD/cleanup tests saw surviving PID identities with no active
group. Matched control reproduces FAIL in 4.318s and observes PID-1-owned
zombies. A shell PID 1 can itself reap, so a superficially similar container
is not the negative control. No production defect was inferred from either.

Correction/proof: same source/user/control with Docker `--init` passes the
inherited-FD and seven-case crash matrix (83.503s). Fresh disk compiler cache,
non-root user, normal init and clean plain source `40d5d6e6` pass the exact
full CI command: 152 packages, wfctl 519.387s below the unchanged 600s limit,
run 19:46:25-19:59:01 UTC. Go 1.26.8 real strict live Docker tests also pass
(36.661s), with journal/container cleanup. Explicitly source evidence, not
downloaded-release proof. Scope: no manifest/task/PR or protected-file change.

### Backport 2026-10-03: Native Scanner Memory Custody

Cause: digest-pinned OSV2.6 unchanged entrypoint discovered all14 Go modules,
then Docker killed the scanner after63.224s: exit137/OOMKilled=true, 7GiB
container cap, no results JSON and no reporter execution. Discovery alone is
not successful native analysis. Docker Desktop has8GiB total; another owner's
container must remain untouched. No production-code defect or clean scan is
inferred from this failed local verification environment.

Hypothesis: a container-local `GOMEMLIMIT=4GiB` soft GC budget can reduce the
scanner's peak memory without changing analyses. This is ordinary runtime
resource tuning, not a Go/compiler/authentication or advisory exception. It
does not authorize CI adoption: the protected policytool's GO* env restriction
remains unchanged. Official [runtime docs](https://pkg.go.dev/runtime#hdr-Environment_Variables)
define a soft Go-managed-memory bound, not a hard shared container bound.

Correction: one paired resource trial on the same frozen550c0f7a source/image,
platform, entrypoint, recursive command, container limits and analysis defaults;
only GOMEMLIMIT changes. Fresh outside-source evidence, exact OOM receipt and
raw logs retained. If it completes, verify final preserved-resolution source
separately under the corrected environment. Require all14 module discovery and
native-analysis coverage, current valid JSON, no loading/analysis/OOM/timeout
errors, raw classification and unchanged reporter acceptance. Missing analysis
stays unknown. Preserve the genuine called-control reporter rejection. Another
incomplete trial remains blocking; no automatic retry loop, ignores, disabled
analysis or deadline extension. This external resource failure needs no new
product/spec invariant. Scope: no manifest/task/PR or protected-file change.

Trial disposition: paired550 source scanner0/reporter0; final preserved-source
cf28e5a4 scanner0/reporter0, no loading/analysis errors or OOM. Raw root/example
OpenPGP findings remain called:false; same GC-budget real-call control yields
called:true and reporter1. Upstream v2.6 default calls native analysis only for
sources with affected dependency groups. Classification covers all14 discovered
modules, distinguishing the12 without reported affected groups from the2 with
native results; no invented analysis receipt. Exact native compiler proofs
remain separate, including protected policytool stdlib module-only findings.

### Backport 2026-10-10: HTTP/2 Patch Floor And Compute Ownership

Authority: parent verified paused managed Signal successor and explicitly
assigned this repair to Compute; ADR0063. No Signal feature/acceptance re-entry.
Evidence: source3e62f5fa is Go1.27.1/x/net0.58.0/OTel1.45; actual consumed
wfctl0.86.1 is Go1.26.5/x/net0.58.0. GO-2026-6611 requires Go1.27.2 and
x/net0.60.0. Official stable Go, proxy/sumdb and Docker/CircleCI/DHI tag metadata
verified; their existence does not substitute for native/runtime artifact proof.

S1/S6→Task2: upgrade the existing source/example/eleven fixtures, compiler-host
expectations and generated/default/container pins1.27.1→1.27.2; x/net0.58→0.60
in owning graphs; retain OTel1.45 and all deliberate version overrides.
S4→Task2 plus existing Tasks1/3 acceptance: native authenticated targeted tidy,
focused literal RED/GREEN, real generated app/plugin and runtime/SDK/registry/
Docker proof, exact-head four-command metadata, full cold Linux race/coverage,
native scanner/called control and UI gates. Prior Go1.27.1 receipts are history.
Task2 integration preserves accepted main's source-map-js1.2.2 repair, verified
at2fcb9baf; source-only protected diff compares to that recorded accepted main,
not the historical49e18034 base. Use the existing branch and a non-force merge.
S5: protected CI/scripts/policytool remain separate; current accepted CI/release
Go1.26.5 pins must be reconciled through the existing governed rollout before
source merge. No failed-check bypass, settings change, release or deployment
inside this source manifest. Source3tasks/1PR grouping retained; re-lock after
independent amendment review and structural alignment PASS.

See `decisions/0063-source-go1272-http2-security.md`.

### Backport 2026-10-10: Preserve Accepted Fixture Input Guards

Accepted main2fcb9baf already owns reusable SDK/Docker fixture callers, a full
effective Go/config/compiler/tool/source fingerprint, cancellation-aware
exclusive copy, process teardown and the relocated linked-version regression.
Retaining the older source cache alone would bypass those accepted guards.

Task1 keeps its source `.`+race cache, sticky failure/lazy/private-copy cases
and independent changed-input build semantics. Its actual request path first
uses accepted `fixtureBuildKey`; the full fingerprint joins cache identity.
Unclosed or failed input-key resolution uses the unchanged raw command, never
another cache. Cached delivery uses accepted context-aware exclusive copy.
Integrated source-dispatch controls cover fingerprint changes and native/
GOENV bypass; helper sentinels remain distinct from real runtime acceptance.
Attempt both process-cache cleanups after `m.Run()` and fail on either error.
The accepted integration version test retains empty stdout, preceding stderr
diagnostics and final v9.9.9; remove only its duplicate package-main copy.
No new cache kind, SDK/Docker opt-in, task/PR count change, deadline increase or
protected-file source change. Final frozen runtime/CI gates remain mandatory.
Task2's inherited compiler-host inventory names
`cmd/wfctl/fixture_build_artifacts_contract_test.go`: its normal GOTOOLCHAIN pin
moves1.27.1→1.27.2, preserving deliberate older-version controls. The inherited
public-policy document describes separately protected bridge pins and remains
outside the source/generated-default patch. No1.27.2 proof is claimed here.

### Backport 2026-10-10: Match Physical Overlay Source Paths

The real native Go1.27.2 fixture/cache/host run passed wfctl/SDK/composite/
cleanup and every other control, but its inherited overlay case returned the
original embedded asset. A matched raw-Go control changed only the overlay
source key: logical /var kept the original value; physical /private/var
returned both requested mutations. This proves a Darwin path-alias fixture
defect, not cached-byte reuse or an accepted Go overlay bypass.

Task2/S4 corrects only the source key in already-owned
`cmd/wfctl/fixture_build_artifacts_contract_test.go` with `filepath.EvalSymlinks`.
Keep command/environment, two actual output assertions and2-build/0-hit/
2-bypass counts unchanged. Retain native RED/causal evidence and require
focused race GREEN plus all fixture controls; final frozen Linux/full runtime/
scanner/UI/CI gates remain mandatory. No cache/production behavior, task/PR
count, protected ownership or deadline change.
