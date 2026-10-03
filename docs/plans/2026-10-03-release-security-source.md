# Workflow Source Security Remediation Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Patch shipped Go/UI vulnerabilities and remove redundant record-host builds without weakening runtime evidence.
**Architecture:** Preserve the public Workflow engine/SDK contracts and existing test cases. Patch owning module/lock resolutions; lazily reuse only identical source race-host builds in process-owned temporary custody. CI authority, scanner rollout and release promotion remain separate.
**Tech Stack:** Go 1.26.8, OTel 1.45.0, Node 24/npm, Vitest, real Workflow binaries, Docker, digest-pinned OSV 2.6.0.
**Base branch:** main (`49e1803424f303f6f98cef51cc9ea2dee96c7d38`).
**Design:** `docs/plans/2026-10-03-release-security-source-design.md`.
**Decision:** `decisions/0056-isolate-source-security-remediation.md`.

## Guidance And Evidence

Guidance: `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `docs/public-workflow-policy.md`; workspace portfolio consulted. No parallel scanner/framework/plugin invented.

Baseline: exact base/main OSV rows identical (159 findings, 157 fixable); old scanner Go 1.26.2 cannot analyze Go 1.26.5 modules. Shipped TLS/relative-URL DoS and UI YAML parsing warrant dependency remediation. Full wfctl race/coverage exceeded its existing 600s budget twice; no retry loop or deadline increase.

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~700

**Out of scope:**
- All `.github/`, `scripts/`, policytool, authority manifests, staged workflow contexts and Dockerfile changes.
- Release tags/publication, deployment, Signal manifest changes and Go minor/fleet migration.
- Cache reuse for SDK, Docker, registry, mutant or capability executables; test removal, timeout extension, GORACE changes, scanner ignores/suppression.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Patch Workflow source security and reuse immutable test hosts | Task 1, Task 2, Task 3 | fix/workflow-release-security-20261003 |

**Status:** Locked 2026-10-03T18:25:28Z

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

1. Preserve existing full-scan failure/actual code reachability evidence as RED; run exact Go 1.26.5 native `govulncheck` on shipped server/wfctl packages if needed to establish specific reachable advisory before update. Separately test policytool with its unchanged exact Go 1.26.5 compiler; errors/called vulnerable findings block, module-only uncalled findings must remain disclosed.
2. Use structured Go resolver commands: `go mod edit -go=1.26.8`, targeted `go get` OTel API/SDK/trace/metric/exporter families at 1.45.0, `go mod tidy` under `GOWORK=off GOTOOLCHAIN=go1.26.8`. Root existing gRPC 1.83.2/x-text 0.42.0 retained; fix stale fixture pins to these established versions. Repeat edit/tidy in each owning example/fixture module; exclude policytool. No broad `go get -u` or unrelated direct-dependency changes.
3. Set source-host registry compiler override/expected environment to 1.26.8; retain selector failures, default API and isolated TLS/proxy semantics. Update executable minimum-version commands/docs only, distinguishing historical Darwin trust behavior. Run `GOWORK=off GOTOOLCHAIN=go1.26.8 go test ./cmd/wfctl -run '^TestPluginRegistrySync(HostBinary|ReleaseProxy)_' -count=1`; expected exit 0.
4. Build actual `cmd/server`, `cmd/wfctl`, `cmd/workflow-sandbox-runner`, `cmd/workflow-lsp-server`; inspect `go version -m`: compiler 1.26.8, OTel 1.45.0 where linked. Probe original Go 1.26.5 driver with authenticated default `auto` selection, no workflow/env-policy change; built source must select 1.26.8. Enumerate shipped Linux/Darwin/Windows dependency lists: no OpenPGP packages. Full module version-skew audit must show only intentional unchanged policytool/CI driver pins.
5. Use a structured YAML parser to derive temporary config from a committed runnable HTTP example: explicit `127.0.0.1` listeners, a run-specific response identity, private data/management paths. Launch actual built server without production credentials, reject startup/bind/listener errors, assert owned process alive and HTTP 200 with that exact identity. Verify graceful owned-process shutdown and endpoint absence afterwards. No response from an unrelated existing listener or substitute server can satisfy the smoke gate.
6. Run full host matrix `go test ./cmd/wfctl -run '^TestPipelineRecordHost' -race -count=1`, including live Docker flags/image according to fixture README. Require actual sandbox properties and empty journals/containers. Run Linux default-API `go test -p=2 ./cmd/wfctl -run '^TestPluginRegistrySync(Target_ActualCLI|HostNegatives)$' -count=1 -v`; expected forward/no-op/rollback control and all 30 denials PASS, no source fallback for explicit downloaded selectors.
7. Run exact Go 1.26.8 vet and `golangci-lint run --new-from-rev=origin/main`; expected exit 0. Cross-platform builds, module tidy diffs stable. Commit only Task 2 owning files. Rollback: reviewed dependency revert/rebuild/runtime smoke, then hold release because vulnerabilities return.

### Task 3: Patch UI Parsing And Integrated Verification

Requirements: S2, S4, S5. Files: `ui/package-lock.json`; create `ui/src/utils/yamlSecurity.test.ts`; design/review/progress evidence docs for this plan. `ui/package.json` changes only if the existing compatible range genuinely cannot resolve a target; otherwise preserve it byte-for-byte.

1. Add bounded real-parser tests: old js-yaml 4.2.0 must fail a merge-budget rejection assertion; actual imported `@gocodealone/workflow-editor/utils` `parseYaml` must parse a valid parametric Workflow config, reject malformed syntax and reject the hostile merge input through its real default-budget path. Do not mock parser or editor export. Run focused Vitest against old installed dependencies; retain RED attributable to actual parser.
2. Target only vulnerable resolutions within existing compatible ranges: js-yaml >=4.3.2, nanoid 3.x >=3.3.18, undici 7.x >=7.29.1, Babel core >=7.29.6, browserslist >=4.28.7, baseline-browser-mapping >=2.11.0, postcss 8.x >=8.5.23, humanfs/node 0.16.x >=0.16.8, vitest and its mocker 4.1.x >=4.1.11. Preserve existing overrides, direct manifest, unrelated npm graph and ecommerce e2e lock. Use npm resolver, not JSON string surgery. If peer alignment requires an additional resolution, justify exact causality in evidence.
3. `npm ci`, `npm test -- --run`, `npm run build` in ui: expected focused regression and complete suite PASS, tsc/Vite exit 0. Existing UI lint baseline may be measured separately; no unrelated UI refactor. Audit exact installed lock versions/paths; no vulnerable duplicate resolution of targeted families remains.
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
