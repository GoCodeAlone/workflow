# Workflow Source Security Remediation Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Patch shipped Go/UI vulnerabilities and remove redundant record-host builds without weakening runtime evidence.
**Architecture:** Preserve the public Workflow engine/SDK contracts and existing test cases. Patch owning module/lock resolutions; lazily reuse only identical source race-host builds in process-owned temporary custody. CI authority, scanner rollout and release promotion remain separate.
**Tech Stack:** Go 1.27.1, golangci-lint 2.14.0, OTel 1.45.0, Node 24/npm, Vitest, real Workflow binaries, Docker, digest-pinned OSV 2.6.0.
**Base branch:** main (`49e1803424f303f6f98cef51cc9ea2dee96c7d38`).
**Design:** `docs/plans/2026-10-03-release-security-source-design.md`.
**Decisions:** `decisions/0056-isolate-source-security-remediation.md`; approved amendment `decisions/0057-upgrade-source-and-generated-go.md`.
Generated-runtime owning-file amendment: `decisions/0058-repair-generated-runtime-boundaries.md`.
Default-config owning-file amendment: `decisions/0059-use-self-contained-app-scaffolds.md`.
Health-config owning-file amendment: `decisions/0060-use-existing-scaffold-health-wiring.md`.
Remaining-default owning-file amendment: `decisions/0061-align-remaining-go-defaults.md`.

## Guidance And Evidence

Guidance: `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/public-workflow-policy.md`; workspace portfolio consulted. No parallel scanner/framework/plugin invented.

Baseline: exact base/main OSV rows identical (159 findings, 157 fixable); old scanner Go 1.26.2 cannot analyze Go 1.26.5 modules. Shipped TLS/relative-URL DoS and UI YAML parsing warrant dependency remediation. Full wfctl race/coverage exceeded its existing 600s budget twice; no retry loop or deadline increase.

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~1,000

**Out of scope:**
- All `.github/`, `scripts/`, policytool, authority manifests and staged workflow contexts; embedded scaffold CI templates are Task2 source, not executable repository workflows.
- Release tags/publication, deployment, Signal manifest changes and other-repository fleet migration. Workflow Go1.27.1/generated defaults and maintained Docker builders are explicitly included by ADR0057.
- Cache reuse for SDK, Docker, registry, mutant or capability executables; test removal, timeout extension, GORACE changes, scanner ignores/suppression.
- Generated strict StringValue/app bootstrap defects are Task2 per ADR0058; broader codec types, new scaffold ABI and new generator APIs remain excluded.
- Default optional external observability config repair is Task2 per ADR0059; new external-plugin loader and replacement telemetry semantics remain excluded.
- Existing health endpoint wiring is Task2 per ADR0060; no new health API or handler substitute.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Patch Workflow source security and reuse immutable test hosts | Task 1, Task 2, Task 3 | fix/workflow-release-security-20261003 |

**Status:** Locked 2026-10-04T06:44:30Z

### Task 1: Reuse Identical Source Record Hosts

Requirements: S3, S4. Files: modify `cmd/wfctl/pipeline_record_host_test.go`, `cmd/wfctl/main_test.go`; create portable `cmd/wfctl/pipeline_record_host_cache_test.go`.

1. Add local-cache tests for one build across repeated identical requests, distinct copied mode-0700 destinations, changed cwd/compiler/environment fallback, failure propagation, lazy initialization and owned-directory removal. Inject build operation only in helper unit tests; sentinel bytes are not runtime proof. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPipelineRecordHostCache' -count=1`; expected new tests fail before helper exists.
2. Implement a synchronized lazy process-owned cache only for source target `.` with `race=true`. Key exact cwd, resolved compiler identity and complete effective build environment. Error/cancellation never supplies a binary. For any differing key, retain original independent build. Never cache explicit downloaded-host selection, including invalid selections; existing denial branch remains first. Do not build eagerly in TestMain.
3. Copy cached bytes to each caller's existing isolated destination with 0700 mode. Remove only the owned cache after `m.Run()` and before `os.Exit`; preserve private-child dispatch. Cache helper must compile on Windows. No test parallelism/global environment mutations added.
4. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPipelineRecordHost(Cache|PrebuiltBinary)' -race -count=1`; expected exit 0, all copy/denial/cache cases PASS. Run `GOOS=windows GOARCH=amd64 GOWORK=off GOTOOLCHAIN=go1.26.8 go test -c -o /tmp/wfctl-security-windows.test.exe ./cmd/wfctl`; expected exit 0.
5. After Task 2 stabilizes modules, run the exact CI `go test -v -race -coverprofile=coverage.out ./...` on a cold Linux compiler cache, unchanged default package timeout. Retain output and timings; wfctl must finish below 600s with all late-output, composites, SDK and cleanup cases present. Otherwise backport A1 and hold, not extend limits.
6. Commit only helper/test files after focused proof. Rollback: revert this test-helper change and rerun focused/full tests; no production behavior changes.

### Task 2: Patch Go Runtime Dependencies And Prove Real Hosts

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
