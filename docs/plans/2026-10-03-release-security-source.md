# Workflow Source Security Remediation Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Patch shipped Go/UI vulnerabilities and remove redundant record-host builds without weakening runtime evidence.
**Architecture:** Preserve the public Workflow engine/SDK contracts and existing test cases. Patch owning module/lock resolutions; lazily reuse only identical source race-host builds in process-owned temporary custody. CI authority, scanner rollout and release promotion remain separate.
**Tech Stack:** Go 1.27.2, golangci-lint 2.14.0, OTel 1.45.0, Node 24/npm, Vitest, real Workflow binaries, Docker, digest-pinned OSV 2.6.0.
**Base branch:** main (`49e1803424f303f6f98cef51cc9ea2dee96c7d38`).
**Design:** `docs/plans/2026-10-03-release-security-source-design.md`.
**Decisions:** `decisions/0056-isolate-source-security-remediation.md`; approved amendment `decisions/0057-upgrade-source-and-generated-go.md`.
Generated-runtime owning-file amendment: `decisions/0058-repair-generated-runtime-boundaries.md`.
Default-config owning-file amendment: `decisions/0059-use-self-contained-app-scaffolds.md`.
Health-config owning-file amendment: `decisions/0060-use-existing-scaffold-health-wiring.md`.
Remaining-default owning-file amendment: `decisions/0061-align-remaining-go-defaults.md`.
Existing-test owning-file backport: `decisions/0062-align-scaffold-and-version-test-boundaries.md`.
Approved patch-security amendment: `decisions/0063-source-go1272-http2-security.md`.

## Guidance And Evidence

Guidance: `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/public-workflow-policy.md`; workspace portfolio consulted. No parallel scanner/framework/plugin invented.

Baseline: exact base/main OSV rows identical (159 findings, 157 fixable); old scanner Go 1.26.2 cannot analyze Go 1.26.5 modules. Shipped TLS/relative-URL DoS and UI YAML parsing warrant dependency remediation. Full wfctl race/coverage exceeded its existing 600s budget twice; no retry loop or deadline increase.

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~1,000

**Out of scope:**
- All `.github/`, `scripts/`, policytool, authority manifests and staged workflow contexts; embedded scaffold CI templates are Task2 source, not executable repository workflows.
- Release tags/publication, deployment, Signal manifest changes and other-repository fleet migration. Workflow Go1.27.2/generated defaults and maintained Docker builders are explicitly included by ADR0057/0063.
- Cache reuse for SDK, Docker, registry, mutant or capability executables; test removal, timeout extension, GORACE changes, scanner ignores/suppression.
- Generated strict StringValue/app bootstrap defects are Task2 per ADR0058; broader codec types, new scaffold ABI and new generator APIs remain excluded.
- Default optional external observability config repair is Task2 per ADR0059; new external-plugin loader and replacement telemetry semantics remain excluded.
- Existing health endpoint wiring is Task2 per ADR0060; no new health API or handler substitute.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Patch Workflow source security and reuse immutable test hosts | Task 1, Task 2, Task 3 | fix/workflow-release-security-20261003 |

**Status:** Locked 2026-10-10T15:40:45Z

### Task 1: Reuse Identical Source Record Hosts

Requirements: S3, S4. Files: modify `cmd/wfctl/pipeline_record_host_test.go`, `cmd/wfctl/main_test.go`; create portable `cmd/wfctl/pipeline_record_host_cache_test.go`.

**Accepted-main integration backport (ADR0063):** retain the source race-host
cache and every existing case. Actual requests use accepted `fixtureBuildKey`
to include effective Go config, selected compiler/tools and source contents in
identity; external/unclosed inputs execute the original command unchanged.
Use accepted context-aware exclusive copy for cache delivery. Add integrated
source-dispatch controls in the existing host-test owner. Both process caches
close after `m.Run()`, even if one fails. Accepted SDK/Docker cache callers and
the relocated linked-version test remain inherited baseline, not new scope.
All runtime, timeout and final exact-head proof gates below remain mandatory.

1. Add local-cache tests for one build across repeated identical requests, distinct copied mode-0700 destinations, changed cwd/compiler/environment fallback, failure propagation, lazy initialization and owned-directory removal. Inject build operation only in helper unit tests; sentinel bytes are not runtime proof. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPipelineRecordHostCache' -count=1`; expected new tests fail before helper exists.
2. Implement a synchronized lazy process-owned cache only for source target `.` with `race=true`. Key exact cwd, resolved compiler identity and complete effective build environment. Error/cancellation never supplies a binary. For any differing key, retain original independent build. Never cache explicit downloaded-host selection, including invalid selections; existing denial branch remains first. Do not build eagerly in TestMain.
3. Copy cached bytes to each caller's existing isolated destination with 0700 mode. Remove only the owned cache after `m.Run()` and before `os.Exit`; preserve private-child dispatch. Cache helper must compile on Windows. No test parallelism/global environment mutations added.
4. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPipelineRecordHost(Cache|PrebuiltBinary)' -race -count=1`; expected exit 0, all copy/denial/cache cases PASS. Run `GOOS=windows GOARCH=amd64 GOWORK=off GOTOOLCHAIN=go1.26.8 go test -c -o /tmp/wfctl-security-windows.test.exe ./cmd/wfctl`; expected exit 0.
5. After Task 2 stabilizes modules, run the exact CI `go test -v -race -coverprofile=coverage.out ./...` on a cold Linux compiler cache, unchanged default package timeout. Retain output and timings; wfctl must finish below 600s with all late-output, composites, SDK and cleanup cases present. Otherwise backport A1 and hold, not extend limits.
6. Commit only helper/test files after focused proof. Rollback: revert this test-helper change and rerun focused/full tests; no production behavior changes.

### Task 2: Patch Go Runtime Dependencies And Prove Real Hosts

**Operative patch-security amendment (ADR0063):** existing Task2/sole PR1022,
assigned to Compute by the parent; Signal stays paused. Every final1.27.1
compiler/floor/default/assertion/command in the earlier Task2A-E amendment below
now targets1.27.2. Earlier dated receipts remain historical; explicit older
version overrides and unrelated dependency/checksum versions stay unchanged.

1. Integrate the accepted main into the existing source branch with a normal
   merge before collecting source acceptance, preserving its source-map-js1.2.2
   repair and all accepted authority bytes. Record that exact integration base;
   source-only protected-file diff against it must be empty. This supersedes
   the historical49e18034 protected comparison, not its evidence. No new clone,
   worktree or private Go cache. Every following gate binds the integrated head.
2. Update the existing independent default-version assertions first; retain
   their literal1.27.2 expectations and all existing override/runtime cases.
   Run the unchanged focused Task2A command with GOTOOLCHAIN=go1.27.2: RED must
   come from old defaults, not compiler setup. Parent serial allocation governs
   heavy local/native work; existing authorized CI supplies public evidence.
3. Update root/example/eleven fixture Go floors and every already-owned
   generated/source-host/default/container pin1.27.1→1.27.2. Update x/net0.58→
   0.60 in root/example and ten linked fixture graphs; iac-hang has no x/net.
   The inherited `cmd/wfctl/fixture_build_artifacts_contract_test.go` compiler
   control also moves its normal GOTOOLCHAIN1.27.1 pin to1.27.2; retain its
   deliberate older source-floor/compiler-variant controls. The inherited
   public-policy documentation describes its separate protected bridge and
   is not a generated/source default owner.
   Retain OTel1.45 and existing crypto/sys/text/term versions unless native
   targeted tidy requires a documented compatible companion correction.
4. With GOWORK=off/GOTOOLCHAIN=go1.27.2, authenticated native targeted module
   resolution/tidy must preserve unrelated resolutions. Never replace text in
   go.sum checksum rows. Verify all owning module files are tidy and focused
   literal assertions GREEN; existing explicit overrides still pass.
5. Rerun Task2C/D actual generated/host/SDK/registry/Docker/four-command proofs
   with1.27.2, full cold Linux race/coverage under the same600s wfctl deadline,
   and Tasks1/3 native scanner/called-control/UI gates. Inspect exact frozen
   revision, vcs.modified=false, compiler1.27.2/x/net0.60/OTel1.45 where linked.
   Official image catalog metadata alone cannot pass maintained-image gates.
6. Independently review exact source, commit/push the existing PR1022 only,
   and require all real current-head checks. Compatible CI/release drivers
   remain the separately locked rollout; no protected source-PR writes,
   skipped acceptance, timeout extension, settings exception or failing merge.
   Subsequent official release/artifact verification uses its separately
   authorized governing path; this source manifest does not publish a tag.

Requirements: S1, S4, S5. Files: `go.mod`, `go.sum`, `example/go.mod`, `example/go.sum`; all eleven module pairs under `cmd/wfctl/testdata/conformance/{iac-hang,iac-pass,no-iac}/` and `cmd/wfctl/testdata/verify_capabilities/{iac-extra-service,good,name-drift,release-good,version-drift,iac-good,missing-ldflag,iac-missing-service}/`; `cmd/wfctl/plugin_registry_sync_host_test.go`, `README.md`, `cmd/wfctl/testdata/pipeline-record/README.md`, `cmd/wfctl/testdata/registry-sync/README.md`.

Approved2026-10-04 amendment additionally owns S6: `cmd/wfctl/templates/`
existing module/CI/release/Docker/README files, `cmd/wfctl/generate.go`,
`cmd/wfctl/generate_test.go`, `cmd/wfctl/deploy.go`, `cmd/wfctl/deploy_test.go`,
`plugin/sdk/generator.go`, `plugin/sdk/generator_test.go`,
`module/pipeline_step_build_binary.go`, `module/pipeline_step_build_binary_test.go`,
`cigen/render_circleci.go`, `cigen/render_circleci_test.go`, `Dockerfile.admin`,
`Dockerfile.legacy` and active minimum-version docs. No new generator API.
ADR0058 additionally owns `cmd/wfctl/templates/{api-service,event-processor,full-stack}/main.go.tmpl`
and `plugin/external/convert.go`, `plugin/external/convert_test.go`. Reuse the
existing default-plugin registry for all generated app bootstraps and preserve
the generated strict StringValue schema with a bounded bidirectional host
value-map adapter. Add failing tests first; invalid fields/types must still
reject and ordinary messages retain existing behavior. No wider WKT support.
ADR0059 adds only `cmd/wfctl/templates/{api-service,full-stack}/workflow.yaml.tmpl`:
remove optional external observability modules from the self-contained defaults,
document opt-in composition in existing owning READMEs, and test generated module
types against the actual default-plugin registry. Add failing assertions first;
rerun real generated API config with only address/identity parameterization.
ADR0060 adds `cmd/wfctl/templates/event-processor/workflow.yaml.tmpl` to the two
existing YAML owners: preserve /health via healthPath and existing observability
wiring, not a manual route to a non-handler service. Add generated-config path/
route regression RED/GREEN and rerun actual API/health/SIGTERM/endpoint absence.
Observed trigger backport additionally removes redundant invalid named trigger
blocks from these same owners, preserving existing HTTP routes and messaging
subscriptions/producers. Assert any emitted triggers against the actual default
trigger registry before final generated-app proof; no manifest change.
ADR0061 additionally owns `mcp/wfctl_tools.go`, `mcp/wfctl_tools_test.go`,
`example/ecommerce-app/Dockerfile`, `DOCUMENTATION.md` and `CONTRIBUTING.md`.
Assert structured Go pins in the real MCP CD result and existing release helper
RED/GREEN; retain current MCP API/HasPlugin behavior. Verify the DHI builder's
actual tag/compiler; update only active prerequisites/defaults in these and
existing README/build/deploy owners. No historical-plan rewrite, runtime image
vendor/hardening change or protected-file ownership.
The steps below supersede earlier1.26.8 commands for final verification;
previous tests/results remain historical receipts, not new-compiler acceptance.
ADR0062 additionally names `cmd/wfctl/init_test.go`: align optional-module
absence assertions with the approved self-contained defaults, retaining all
existing generated-file/module/UI checks. In already-owned `main_test.go`,
assert the exact final linked-version stderr line and empty stdout, retaining
preceding dependency stderr diagnostics. Preserve the existing handler writer.
Require authentic failure then focused GREEN; no production output change.
The observed scheduling backport additionally owns
`e2e_middleware_negative_test.go` for Task2/S4 only: controlled25ms client
pause must reproduce the old10ms-refill failure;60RPM/derived refill wait must
preserve all three actual engine/HTTP assertions and existing deadlines.
No production limiter/clock change; rerun the exact final cold command.

A. Add independent literal1.27.1 assertions for scaffold modules/workflows,
legacy emitters, SDK module+local-replace modules, deploy Docker builder,
binary-build default and CircleCI image; preserve explicit overrides. Run
`GOWORK=off GOTOOLCHAIN=go1.27.1 go test ./cmd/wfctl ./plugin/sdk ./module ./cigen -run 'Test(InitTemplatesIncludeGithubWorkflows|CIWorkflowContent|CDWorkflowContent|ReleaseWorkflowContent|WriteDockerfile|GenerateProjectStructure|GenerateGoMod|BuildBinaryStep_Defaults|RenderCircleCI)' -count=1`;
new version assertions must fail on old pins, not compiler setup.
B. Use `go mod edit -go=1.27.1` and native targeted tidy in root/example/eleven
fixtures, no unrelated dependency upgrade. Update owning default pins above
and source-host expectations; preserve explicit overrides and prior dependency
fixes. Rerun A: all assertions PASS. Compatible2.14.0 lint, vet, Windows compile
and focused real record/registry suites use exact native1.27.1.
C. Build actual wfctl and use its existing `init`, `plugin init`, `generate`
and deploy-generation tests/commands. Parse emitted Go modules/YAML/Docker
versions independently; run a real generated API app with an explicit local
Workflow replacement, build with1.27.1/local, observe owned HTTP200/process
shutdown. Generate a fresh SDK plugin from built wfctl in source cwd using
`plugin init --author GoCodeAlone --output "$PLUGIN_SOURCE" workflow-plugin-go127proof`;
inside generated source run
`go mod edit -replace=github.com/GoCodeAlone/workflow="$SOURCE_ROOT"` to normalize
the discovered relative replacement for this integration-only proof, then
assert the absolute replacement, native tidy and compile
`go build -o "$WFCTL_PLUGIN_DIR/workflow-plugin-go127proof/workflow-plugin-go127proof" ./cmd/workflow-plugin-go127proof`,
and copy actual generated plugin.json/plugin.contracts.json alongside it.
Through the real Workflow plugin loader, execute generated
`step.go127proof_example` with a run-specific protobuf StringValue input;
require exact uppercase output, successful discovery/lifecycle/cleanup.
Record generated plugin output, not record-fixture binary, as this proof.
Exercise actual default BuildBinaryStep factory/Execute dry-run with no
go_version override, independently assert output go.mod1.27.1, materialize
those exact emitted files outside the repo, add explicit local Workflow
replacement via `go mod edit -replace` solely for unreleased source integration,
native tidy/build and launch the generated embedded-config binary. Require
owned HTTP200/identity and graceful shutdown. Existing compile test that accepts
an error cannot satisfy this gate; this proves generated output, not published
v0.0.0 dependency resolution. Dockerfile
builder smoke builds and launches a real maintained image; never insert a fake
prebuilt admin-plugin. Existing container/SDK matrices prove adjacent boundary.
D. Rebuild four shipped commands from clean frozen plain checkout and inspect
`go version -m`: exact frozen revision, vcs.modified=false, go1.27.1,
OTel1.45.0 where linked. Repeat actual server HTTP/SIGTERM/endpoint absence,
strict Docker, Linux default-API control plus30denials, and whole cold non-root
`--init` Linux152-package race/coverage under unchanged600s wfctl deadline.
Rerun full UI141/build and native source OSV/called-control evidence on final
source; no CI clean claim until all actual PR checks succeed. Run unchanged
policy harness under1.27.1 and byte-empty protected-file diff.
E. Commit only Task2 source files; push existing PR1022, independent bounded
review and all actual PR checks/trusted policy required before ordinary/admin
merge. No new source PR or settings changes. Rollback: reviewed source/default
revert, rebuild and launch; hold release because older limitations return.

1. Preserve existing full-scan failure/actual code reachability evidence as RED; run exact Go 1.26.5 native `govulncheck` on shipped server/wfctl packages if needed to establish specific reachable advisory before update. Separately test policytool with its unchanged exact Go 1.26.5 compiler; errors/called vulnerable findings block, module-only uncalled findings must remain disclosed.
2. Use structured Go resolver commands: `go mod edit -go=1.26.8`, targeted `go get` OTel API/SDK/trace/metric/exporter families at 1.45.0, `go mod tidy` under `GOWORK=off GOTOOLCHAIN=go1.26.8`. Root existing gRPC 1.83.2/x-text 0.42.0 retained; fix stale fixture pins to these established versions. Repeat edit/tidy in each owning example/fixture module; exclude policytool. No broad `go get -u` or unrelated direct-dependency changes.
3. Set source-host registry compiler override/expected environment to 1.26.8; retain selector failures, default API and isolated TLS/proxy semantics. Update executable minimum-version commands/docs only, distinguishing historical Darwin trust behavior. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPluginRegistrySync(HostBinary|ReleaseProxy)_' -count=1`; expected exit 0.
4. Build actual `cmd/server`, `cmd/wfctl`, `cmd/workflow-sandbox-runner`, `cmd/workflow-lsp-server`; inspect `go version -m`: compiler 1.26.8, OTel 1.45.0 where linked. Probe original Go 1.26.5 driver with authenticated default `auto` selection, no workflow/env-policy change; built source must select 1.26.8. Enumerate shipped Linux/Darwin/Windows dependency lists: no OpenPGP packages. Full version-skew audit classifies residual protected CI/policytool drivers, legacy standalone scaffold/bootstrap pins and historical examples, with fleet/generator follow-up disclosed; no claim that those separate defaults were upgraded. Verify final exact-head artifact from a frozen plain checkout to avoid the nested-worktree VCS-stamp defect documented in the design backport.
5. Use a structured YAML parser to derive temporary config from a committed runnable HTTP example: explicit `127.0.0.1` listeners, a run-specific response identity, private data/management paths. Launch actual built server without production credentials, reject startup/bind/listener errors, assert owned process alive and HTTP 200 with that exact identity. Verify graceful owned-process shutdown and endpoint absence afterwards. No response from an unrelated existing listener or substitute server can satisfy the smoke gate.
6. Run full host matrix `go test ./cmd/wfctl -run '^TestPipelineRecordHost' -race -count=1`, including live Docker flags/image according to fixture README. Require actual sandbox properties and empty journals/containers. Run Linux default-API `go test -p=2 ./cmd/wfctl -run '^TestPluginRegistrySync(Target_ActualCLI|HostNegatives)$' -count=1 -v`; expected forward/no-op/rollback control and all 30 denials PASS, no source fallback for explicit downloaded selectors.
7. Run exact Go 1.26.8 vet and `golangci-lint run --new-from-rev=origin/main`; expected exit 0. Cross-platform builds, module tidy diffs stable. Commit only Task 2 owning files. Rollback: reviewed dependency revert/rebuild/runtime smoke, then hold release because vulnerabilities return.

### Task 3: Patch UI Parsing And Integrated Verification

Requirements: S2, S4, S5. Files: `ui/package-lock.json`; create `ui/src/utils/yamlSecurity.test.ts`; design/review/progress evidence docs for this plan. `ui/package.json` changes only if the existing compatible range genuinely cannot resolve a target; otherwise preserve it byte-for-byte.

1. Add bounded real-parser tests: old js-yaml 4.2.0 must fail a merge-budget rejection assertion; actual imported `@gocodealone/workflow-editor/utils` `parseYaml` must parse a valid parametric Workflow config, reject malformed syntax and reject the hostile merge input through its real default-budget path. Do not mock parser or editor export. Run focused Vitest against old installed dependencies; retain RED attributable to actual parser.
2. Target only vulnerable resolutions within existing compatible ranges: js-yaml >=4.3.2, nanoid 3.x >=3.3.18, undici 7.x >=7.29.1, Babel core >=7.29.6, browserslist >=4.28.7, baseline-browser-mapping >=2.11.0, postcss 8.x >=8.5.23, humanfs/node 0.16.x >=0.16.8, vitest and its mocker 4.1.x >=4.1.11. Preserve existing overrides, direct manifest, unrelated npm graph and ecommerce e2e lock. Use npm resolver, not JSON string surgery. If peer alignment requires an additional resolution, justify exact causality in evidence.
3. `npm ci`, `npm test -- --run`, `npm run build` in ui: expected focused regression and complete suite PASS, tsc/Vite exit 0. Existing UI lint baseline may be measured separately; no unrelated UI refactor. Audit exact installed lock versions/paths; no vulnerable duplicate resolution of targeted families remains. Bundled editor 0.2.0 failure is a separate owning-library prerequisite: consume its independently reviewed compatible patch through the existing range before GREEN, never weaken the consumer assertion.
4. Run upstream OSV action container pinned to verified index `sha256:71ad04ab2f8798be47870f9b18817ad317c2f8f2f97aa6726ba10d5578bc174a` with its unchanged entrypoint, `scan source --format=json --output-file=/evidence/results.json -r --allow-no-lockfiles ./`. Bind `/evidence` to a private outside-checkout directory, never inside scanned root; retain scanner version, image identity, successful native-analysis log/classification JSON for all owning modules. No ignores, environment exception or trust changes. Module-only findings are not clean reachability claims.
5. Independently outside the production/scanned checkout, create one minimal real module calling a known vulnerable API at its vulnerable version. Same scanner/tool must identify called vulnerability; `/root/osv-reporter --new=<control.json> --fail-on-vuln=true` must reject it. This is a labeled scanner positive-control, never an application demo. Errors/incomplete analysis cannot count as vulnerability rejection. Remove only owned control resources.
6. Run existing positive/mutation policy suite with unchanged policy files, root/full cold Linux race/coverage (Task 1), actual HTTP/SDK/Docker/registry proof (Task 2), UI tests/build. `git diff 49e18034 -- .github scripts` must be empty. Retain existing scanner-CI limitation explicitly; source-only proof cannot close Signal Task 35 or publish a tag.
7. Commit lock/regression files and bounded evidence. Request one independent spec/code review, fix real findings, push exact head and open the sole planned PR. Require all required PR checks green; monitor without repeated timeout retries. Merge under standing user approval, do post-merge retro and verify merged main. Remaining scanner-rollout/release work continues separately. Rollback: dependency/regression revert and rebuilt UI, known-vulnerability hold reinstated.

## Integration Matrix

| Integration | Class | Executable Proof |
|---|---|---|
| compiler + server | runtime-integrated | Task 2 actual artifact metadata, YAML startup and HTTP 200 |
| wfctl + SDK/composites/custody | runtime-integrated | Tasks 1/2 complete real host matrices |
| wfctl + Docker | runtime-integrated | Task 2 actual digest-pinned strict sandbox and cleanup |
| wfctl + registry/Git | runtime-integrated | Task 2 Linux default-API control and 30 exact denials |
| UI + editor/js-yaml | runtime-integrated | Task 3 real imports, parser RED/GREEN and UI build |
| upstream OSV + Go loader/reporter | runtime-integrated | Task 3 successful analysis plus called control rejection |
| current public CI policy | config-only | Task 3 unchanged bytes plus existing positive/mutation suite |
| scanner CI/release/Signal promotion | separate | approved CI reconciliation; existing Signal manifest not completed here |

## Execution Order And Ownership

Task 1 helper and Task 3 UI writes are independent; Task 2 module writes must settle before broad Go builds. Lead owns module/docs/integration verification; workers own disjoint cache or UI files, no git authority. Full scenario/shared-state tests and Docker matrix execute sequentially. Cold Linux race gate is measured once after dependency/cache stabilization, not during competing builds. On A1 failure, record actual elapsed evidence/backport; no silent timeout increase.

## Alignment

PASS after bounded plan-review corrections. S1 -> Task 2, S2 -> Task 3, S3 -> Task 1, S4 -> Tasks 1/2/3, S5 -> Tasks 2/3. Reverse trace: Task 1 -> S3/S4; Task 2 -> S1/S4/S5; Task 3 -> S2/S4/S5. All three task headings exist and ship only PR 1. Protected-file exclusions, policytool native proof and rollback are explicit; no workflow/trust/release task is orphaned or silently added. Programmatic strict manifest check PASS.

Amendment alignment PASS after bounded P1/P2 proof corrections: S1/S6 -> Task2; S4 exact new compiler -> Task2A-D
and historical Task1/3 cases rerun. Task1 -> S3/S4, Task2 -> S1/S4/S5/S6,
Task3 -> S2/S4/S5. Three tasks/one existing PR preserved; no protected CI writes.

ADR0058 bounded alignment PASS: observed generated bootstrap/strict StringValue
failures map to Task2C and its added owning files; strict negative/ordinary
message compatibility plus actual app/plugin runtime proof are explicit.
Three tasks/one existing PR retained. Independent reviewer Ramanujan approved
causal correction and checked ownership/coverage; strict manifest check passed.

ADR0059 bounded alignment PASS: real default-config startup failure maps to
Task2C and the two added YAML owners, existing README opt-in notes, real default
registry assertions and generated API runtime gates. Forward/reverse trace
and strict three-task/one-PR manifest check passed; independent Ramanujan
confirmed the narrow coverage. Protected exclusions unchanged.

ADR0060 inline alignment PASS: health path/service correction -> Task2C;
Task2 owning YAML/default runtime proof -> S6/S4 and the observed design backport.
S1/S3/S2 and Tasks1/3 retain their existing coverage. All tasks still ship the
sole existing PR; strict manifest check passed with no missing task, count
change, new health API or protected-file ownership.

ADR0061 inline alignment PASS: maintained MCP/e-commerce/default-doc omissions
map to S6 and Task2A-D plus their named owning files. Task2 remains the sole
source PR row; Tasks1/3 and all other requirements retain coverage. Three
tasks/one existing PR and protected exclusions unchanged. No added MCP feature
or unverified image tag; strict manifest check required before execution.

ADR0062 inline alignment PASS: init expectation correction -> Task2/S6/S4;
version final-line test boundary -> existing Task1/2/S4 ownership. Reverse trace
adds no product behavior, task, PR, dependency or protected-file change.
Three tasks/one existing PR retained; verify the unchanged manifest hash.

Rate-fixture inline alignment PASS: observed real cold failure -> Task2/S4
full-suite acceptance; test-only owning file and RED/GREEN/reversal -> same
runtime proof, no new API, task, PR, timeout or protected-file ownership.

### Observed Overlay-Control Backport

Task2/S4 already owns `cmd/wfctl/fixture_build_artifacts_contract_test.go`.
The native Go1.27.2 race run failed only its real-overlay mutation case:
Darwin logical /var source paths did not match Go's physical /private/var
working directory. Matched raw-Go controls exclude the fixture cache and
prove both requested asset values with a canonical source key. Resolve only
that overlay source key with `filepath.EvalSymlinks`; preserve the raw command,
both output assertions and exact2-build/0-hit/2-bypass counts. Retain the actual
RED and causal receipt, then rerun the real-overlay and all fixture controls
with the same compiler/race/deadlines. Final Linux/runtime/CI gates remain due.
Three tasks/one PR, owning files, protected exclusions and scope lock unchanged.


### Backport 2026-10-16: Recover snapshot acceptance through existing source PR1022 (ADR0198)

Actual Task34/PR1073 is normally merged as main `3c5aa969` (reviewed tree `a0ebe8a9`, parent `aee545f5`). All13 actual-main CI jobs and the four strict checks pass, including153 uncached packages/57 critical parents, realCLI1272/liveDocker4.99s and wfctl578.623s. Its separate automatic snapshot38889204283 attempt1 genuinely FAILS: `go test -race ./...` reaches the unchanged600-second aggregate wfctl package alarm at600.221s while MissingLdflag has run1s;152 other packages pass, image/release jobs SKIP. The active one-second build is not proved hung. Preserve the complete failed log and all historical receipts. Existing source-host reuse in the already approved three-task/one-PR1022 lane is a recovery candidate, not a proved fix or a new task.

| ID | Effective requirement | Existing boundary |
|---|---|---|
| SR1 | Preserve all36 compiler task IDs/branches/PRs, Task34's shipped four-map-only scope, source1022's existing3tasks/1PR/owners and the actual shipping order34→1022. No task, owner, PR grouping, shipped feature or Scope Manifest changes; all17 workspace locks and the source-plan lock remain byte-identical. Task35 remains conditional UNPERFORMED/open because the unchanged harness passes. | Governance/preservation |
| SR2 | Task34 remains unaccepted after its failed snapshot; mainCI PASS never substitutes for snapshot PASS and no original run is relabeled. Record Main1 failure reviews/counters before recovery. Allow only the existing source1022 lane to integrate the engineering-integrated actualmain3c5aa969 through its normal merge under ADR0063 while forward Task34 acceptance remains pending. This supersedes only the accepted-predecessor timing in SF1/RR1/IQ8 and sourceTask2 step1 for this recovery; it does not change which task ships in which PR or accept Task34/runtime. | Existing source1022 preparation |
| SR3 | Compose the already-owned Task1 source race-host reuse with accepted fixtureBuildKey/compiler/config/source identity and context-aware exclusive copy; retain all cache bypass/error/cancellation/cleanup controls and inherited SDK/Docker/registry/capability behavior. Retain existing Task2 Go1.27.2/x/net0.60/generated/runtime/module/default repairs and Task3 UI parsing scope. Reconcile only existing source owners against the recorded actual integration base; all protected .github/scripts/policytool/H/maps bytes must equal that base. No speculative cache efficacy or transferred historical1.27.1 proof. | Existing3tasks/1PR1022 |
| SR4 | Preserve source1022's exact-head focused/full native/default600 budget, generated app/plugin/realhost/UI/maintained-image and ordinary current security/review/protection requirements. Its publication/integration uses the existing PR and ordinary protected checks; actual corrected-main native and unchanged600 snapshot tests/release/image provenance plus final reviews must genuinely succeed before source acceptance. No timeout/race/assertion weakening, manual tag/image/release, blind rerun, private Go cache, extra clone or deployment. | Source1022 verification/integration |
| SR5 | Only after source1022 is genuinely accepted, perform a fresh Task34 Main2 forward reevaluation on that corrected accepted descendant. Prove Task34 H/fourmaps/realized context unchanged and classify every intervening source delta under the approved3/1 source owners; all other bytes/modes remain exact. Join genuine corrected-main/native/snapshot/security/protection evidence and two fresh final reviews before Task34 forward acceptance. Preserve originalMain1 snapshotFAIL and every counter/seal; no retroactivePASS or whole-planComplete. | Task34 forward acceptance |
| SR6 | Existing named34 same-known nonrequired OSV timing still expires at reviewed source1022 integration; source1022 retains its own ordinary current security obligation and no extension/Task35 inheritance is added. Preserve actual full finding/error/alias/risk assessment, CodeQL/Quality/required protections and UNKNOWN/AnalysisComplete=false/SecurityClean=false while incomplete. No scanner framework/upstream env-policy change, setting/credential/runner/deploy authority is added. | Security/runtime boundary |

Forward: SR1→all36/source3/1/locks;SR2→Task34 failure and sourceTask2 integration timing;SR3–SR4→existing sourceTasks1–3;SR5→fresh Task34Main2;SR6→both current security/unchanged expiry. Reverse: Task34 four-map source→unchanged SF5; Task34 acceptance→SR2/SR5 plus unchanged real gates; sourceTasks1–3→ADR0063 andSR2–SR4/SR6; Task35→unchangedSF6; runtime/issuer/releasedLinuxCLI/Compute identities-worker-quietwindow→unchanged obligations. This is a bug/assumption design backport under existing approved source work: PR34 has already shipped before PR1022, and their task grouping/shipped scope is unchanged. Two independent scope-first/all15 governance reviews and full36+source3 alignment precede canonical apply; verify existing locks without relocking.


### Backport 2026-10-16: Preserve current accepted integration dependencies and compiler dispatch

The SR2 normal merge records engineering-integrated main `3c5aa969` as the comparison base. Its owning module graphs already select OTel1.47.0/otelhttp0.70.0 and newer accepted schema/protobuf/genproto companions; its UI already selects js-yaml4.3.2/Vitest5.0.3/source-map-js1.2.2. Preserve those exact accepted versions and graph classifications instead of restoring the source branch's older1.45/4.x graph. Start the thirteen owning modules and their existing checksum files from that accepted base, then use native structured `go mod edit` for only the existing Go1.27.2 floor and x/net0.60.0 requirements, followed by native targeted tidy and complete graph-delta review. No unrelated upgrade, checksum text edit or protected policytool change. Any genuine companion correction must be evidence-backed and reviewed. Older literal1.45/source UI counts are historical proof details; final metadata/runtime expectations bind the inherited accepted1.47 graph and actual current UI suite.

Preserve the accepted invoking-compiler dispatch and `GOTOOLCHAIN=local` registry environment, actual built-CLI metadata rejection and startup/PATH controls. The outer exact source verifier runs native1.27.2; it must not restore downloaded registry compiler selection or remove current main controls. In the already-owned fixture contract, update only normal fixed1.27.1 selection/observation expectations to1.27.2, preserve every deliberate old-compiler/floor variant and current startup/discovery/registry controls, and retain the source-owned physical overlay key fix. SourceTask1 remains source `.` plus race only, with complete accepted fixtureBuildKey, exclusive context-aware copy and both cache cleanups; capability/SDK/Docker/registry cache expansion remains excluded.

This is an accepted-baseline integration clarification within the same three tasks, one PR1022 and existing module/UI/fixture/document owners. It changes no manifest, lock, task/PR grouping, API, timeout or security gate. All historical failure/review counters remain, actual current-head/generated/runtime/Linux default600/UI/image/security/main/snapshot/final proof remains due, and Task34 remains unaccepted under SR1–SR6.


### Backport 2026-10-16: Retain the already-approved editor security resolution

Actual source3 owning CI38895629738/UI job116955296201 fails1/141 cases: the real imported editor parseYaml accepts the above-budget hostile merge, while direct js-yaml.load rejects it. Whole main-lock preservation had restored editor0.2.0 and dropped this source branch's existing0.2.1 correction. Root js-yaml4.3.2 alone does not establish the separately bundled editor parser's budget. Preserve original failure/log/source3 seals; no weakened or removed parser assertion.

Existing Task3 already owns the compatible lock resolution. Native npm metadata resolution restores exactly the original source81b9 editor0.2.1 package object within unchanged manifest^0.2.0; every other package/root object remains exact, including accepted Vitest5/rootjsYaml4.3.2/source-map-js1.2.2. This narrowly corrects the earlier accepted-baseline UI-graph preservation note. No new editor release/repository source, package manifest edit, task/PR/owner/Scope Manifest or lock change. Resolver uses ignore-scripts/package-lock-only and installs no node_modules; its Node25 version is metadata tooling, not Node24 runtime proof.

Installed original0.2.1 artifact statically includes default10000 merge-work accounting and the original source resolution is supporting evidence only. Fresh exact-source review and real Node24 focused/full UI141/build plus all ordinary current CI/security/runtime/main/snapshot/final gates remain due. Source4 review has not started and tests/builds are HELD for Tina after the source3 four-command batch released the local heavy slot. Do not blind-rerun b436, transfer historical1.27.1/older-Vitest proof, accept source1022/Task34 or infer the snapshot600 cause. All existing review counters and source3 M1 cancellation-delay limits remain.


### Backport 2026-10-16: Offline Generated-App Production Graph

The original natural source3 run38895629738/native116955501479 failed both embedded/external `TestBuildBinaryStep_DryRun_GeneratedMainHTTPRuntime` variants before compilation: forced-offline `go mod tidy` loads dependency test graphs not provisioned by the owning source module's download/test. Preserve the complete failed producer log,82 proxy-off denials and both post-seal Important review supplements. This is a test dependency-resolution assumption defect; no generated-app runtime failure was reached or proved.

Existing Task2 already owns this runtime test. Keep the exact three emitted files, default factory/Execute without a Go override, explicit source-only Workflow replacement, invoking native compiler and all HTTP identity/SIGTERM/endpoint-absence assertions in both modes. Seed only committed owning go.sum metadata into the disposable generated module; build its production graph with explicit `-mod=mod` under unchanged GOWORKoff/GOTOOLCHAINlocal/GOPROXYoff/GOSUMDBoff/CGO0. Do not preload dependency test modules, add a clone/private Go cache, enable network, suppress an error or reuse a prebuilt artifact. Add an independent literal1.27.2 assertion to the actual emitted module. This bounded runtime harness no longer invokes dependency-test-graph tidy; ordinary13-owning-module tidy and the separate generated-app/plugin native tidy integration proofs remain mandatory. No production emitter, dependency graph, task/PR/owner/manifest, lock, protected file, assertion or deadline changes.

Fresh exact Node24.21.0 install, real-parser10/fullUI141 tests, build and lint now PASS on the already-reviewed editor0.2.1 resolution with accepted Vitest5.0.3/js-yaml4.3.2 preserved. Keep original UI140PASS1FAIL as failed historical evidence. Source5 continues round5/max5 with both Source4 static seals and new Important native supplements preserved; final full-source independent review follows meaningful actual offline HTTP/lifecycle validation, scoped vet/lint and source preservation checks.

Source3 wfctl604.441s genuinely passed the actual900s CI alarm and exceeded the separate600s functional acceptance target by4.441s. Proportionate log assessment separates that timing gap from the concrete module-resolution failure and advisory benchmark. Do not treat a benchmark as a blocker or infer an individual hang/cache cause; no additional cache scope, assertion weakening, alarm increase or blind prior-head rerun. The corrected source's natural native run and genuine unchanged600 snapshot/acceptance must establish their own results. Source1022/Task34/runtime remain unaccepted until their ordinary current gates succeed; named34 security timing expiry and retired scanner-framework disposition remain unchanged.


### Backport 2026-10-16: Match snapshot functional budget to native CI (ADR0200)

The explicit parent directs the bounded snapshot600→established-native900 consistency repair after actual main e13a0816 passes153/57/all13 but its automatic snapshot fails600.387s. This supersedes the prior600s functional-acceptance requirement, retains600s as an unmet performance target, and authorizes the normal separate corrective trust lifecycle. No cache project/cause, test removal, original-failure relabel or new approval hold.

| ID | Operative correction | Boundary |
|---|---|---|
| SB1 | Native CI already runs the unfiltered Go1.27.2 race suite with explicit900s; snapshot retained Go's implicit600s. Add only `-timeout=15m` to snapshot's `go test -race ./...`. Preserve setup/reporting differences, all tests/race/assertions/child deadlines and original failures. Keep600s as an unmet performance target; no speed/cache-cause claim. | Snapshot functional budget |
| SB2 | Follow the existing normal trust stage→adopt→promote lifecycle in separate corrective PRs: stage only48 command/5 action/1 presence rows for the exact future snapshot context while retaining active rows; adopt the one-line workflow change plus these two source design/plan backports; after actual adoption proof, promote only that context. Keep all38 authority files/H, executable/secret maps, other contexts, settings, events, permissions and runners unchanged. Counts298/74/20 return after cleanup; transient346/79/21 is explicit staging data. Tagged release workflow remains outside this automatic-snapshot repair. | Normal policy lifecycle |
| SB3 | Each corrective PR needs its own reviewed exact diff, genuine natural required/native/policy/security checks and fresh ordinary protection/review/thread custody before normal squash. Stage-only engineering admission does not require an impossible success from the not-yet-repaired600s snapshot; preserve that real result and do not accept source/Task34 from staging. No blind rerun, admin/force/bypass or new security waiver. | Corrective staging/integration |
| SB4 | Actual budget-repaired main native/full153, required checks, automatic900s snapshot tests, real release checksums/binary and image-digest provenance and two final reviews must genuinely succeed before source acceptance. MainCI alone remains insufficient. Supersede only the prior600s functional-acceptance clauses, including SR4/A1; retain600s observations as performance failures. Promotion also follows ordinary current-main acceptance. No deployment/manual tag/image/release. | Actual acceptance |
| SB5 | Preserve the original36 compiler rows and source3tasks/1PR1022 manifests, all17 workspace/source locks and every failed seal/counter. These are explicitly authorized corrective follow-ups, not extra source1022 tasks or Source6/reset. Fresh Task34 Main2 must classify both the accepted source delta and this separate budget workflow/three-map/two-doc correction; H/all other authority remains exact. Source and Task34 stay unaccepted until their genuine remaining gates. Named34 timing expired at reviewed source integration; no extension, Task35 inheritance, bespoke scanner framework or security-clean claim. | Scope/security/forward acceptance |

See [workspace ADR0200](https://github.com/GoCodeAlone/workspace/blob/docs/bmw-upstream-contracts-20261005/decisions/0200-match-snapshot-native-test-budgets.md). Original manifests and lock bytes remain unchanged; source1022/Task34/runtime are not accepted by this backport.
