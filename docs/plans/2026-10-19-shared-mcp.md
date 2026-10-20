# Shared Workflow MCP Compatibility Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task when the parent starts execution under existing user authorization after review.

**Goal:** Correct existing MCP tool contracts and prove July 2026 plus legacy stdio interoperability without changing the assistant packaging work.
**Architecture:** Keep Workflow's current CLI/server and SDK. First correct contracts on v0.54.1; then upgrade the existing SDK to publicly verified v1.3.0 with independently reversible wire proof.
**Tech Stack:** Go 1.27.2 per current repository; mcp-go v0.54.1 then v1.3.0; existing JSON Schema dependency; standard Go testing and stdlib subprocess client.
**Base branch:** main, refreshed and inspected when execution is authorized; do not branch from or amend packaging PR 1083.
**Design:** `docs/plans/2026-10-19-shared-mcp-design.md` (local planning artifact outside repository).

## Scope Manifest

**PR Count:** 2
**Tasks:** 6
**Estimated Lines of Change:** ~600-900 (informational; no authority to grow scope to meet estimate).

**Out of scope:**
- Changes to PR 1083, its lock, `example/workflow-agent-plugin/`, installed plugins/sessions, or active CMS/Compute work.
- New server/framework, orchestration, tools/features, HTTP hosting, credentials, auth flows, tasks, MRTR features, UI, prompts or sampling.
- Structured-output migration, filesystem sandbox redesign, arbitrary user-provided schema support, generic rate-limit platform, universal redactor, directory submission or certification.
- Updating Go/toolchain or unrelated dependencies; changing unrelated source or protected workflows.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|---|---|---|---|
| 1 | fix: enforce shared MCP tool contracts and metadata | Task 1, Task 2, Task 3 | fix/mcp-tool-contracts |
| 2 | fix: support July 2026 MCP while preserving legacy stdio | Task 4, Task 5, Task 6 | fix/mcp-july-compatibility |

**Status:** Complete 2026-10-20T00:57:24Z

PR 2 follows merged PR 1 (or is explicitly stacked for review); never silently collapse, drop or enlarge these PRs. Both begin draft. Source changes, branch creation, commits and publication remain future work pending the parent's execution decision under existing user authorization. Rebase/precedent inspection must preserve concurrent changes. Before implementation, independent adversarial review and design-to-plan alignment must pass and the owner must lock this manifest; this local draft itself is not a scope lock. No additional human approval gate is introduced.

## Requirements and acceptance map

| Design | Task | Acceptance |
|---|---|---|
| R1/R2 validation and arrays | 1, 3, 5 | Invalid invocation rejected before handler; valid typed input unchanged |
| R3 errors / adapter parity | 2, 3, 5 | Tool/engine errors remain errors; `valid:false` analytical report remains success |
| R4 truthful explicit annotations | 3 | Exhaustive per-name booleans match audited behavior |
| R5 SDK July and legacy | 4, 5, 6 | Real CLI passes four legacy versions and stateless July |
| R6 wire/security/determinism | 1, 2, 3, 5 | Offline schemas; no raw secret logging; clean framing/EOF/cancel/private cache |
| R7 documentation accuracy | 3, 6 | 33 tools/5 resources by live inventory, restart caveat, supported eras and limits |

## Future execution prerequisites

Read `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, relevant `CLAUDE.md`, and changed-source history. Confirm target files exist; refresh source/API review if versions changed. Use a clean isolated checkout, `GOWORK=off`, temporary build/plugin/cwd directories, minimal process environment and public fixtures. Never use ambient application configs, secret values, installed plugin directories or shared app/database/browser sessions. Record source revision, SDK pin and executable digest with the final transcript. All commands below are future verification commands, not operations performed by this plan author.

### Task 1: Enforce existing schemas without changing successful calls

**Files:** Modify `mcp/server.go`, `mcp/wfctl_tools.go`, `mcp/scaffold_tools.go`; create `mcp/input_contract_test.go`; adjust existing `mcp/server_test.go` only for changed error expectations.

1. Add failing dispatcher tests for missing required arguments, string-as-boolean, scalar/null array values and mixed `module_types` elements. Add a valid typed counterpart for each. For a malformed call to an engine-backed test handler, assert the handler's invocation counter stays zero.
2. Run `GOWORK=off go test ./mcp -run 'TestMCPInputContract|TestMCPSchemaInventory' -count=1`; expected pre-fix failure includes accepted wrong-type input / missing `items.type:string`.
3. Enable `server.WithInputSchemaValidation()` in `NewServer`. Add `mcp.WithStringItems()` to every string-array registration (including module_types, environment lists); retain currently permitted extra top-level fields rather than adding a global breaking strictness policy. Make skeleton handler reject any nonstring element instead of silently dropping it; no partial output.
4. Add offline schema inventory validation: every built-in schema compiles with the existing JSON Schema library; `$ref` is absent or fragment-local. No remote/file loader, no downloaded schemas, and no custom YAML validator. This catches the SDK's invalid-schema skip behavior for Workflow-owned registrations.
5. Rerun focused tests plus `GOWORK=off go test ./mcp -count=1`. Expected `PASS`; wrong-type calls have `IsError:true`; valid baseline text payloads remain equivalent. A YAML validation report with `valid:false` is a separate successful-report test.
6. Commit future task changes after review. Rollback: revert task commit and rerun existing legacy valid-call checks; explicitly record reintroduced malformed-input acceptance.

### Task 2: Preserve adapter errors and make inventories deterministic

**Files:** Modify `mcp/library.go`, `mcp/server.go`; test `mcp/library_test.go`, `mcp/server_test.go`; create `mcp/error_contract_test.go`.

1. Add failing tests: provider calls with missing/wrong-type input return nonnil Go error; failed EngineProvider trigger produces `IsError:true` and existing `success:false`; successful `valid:false`/`compatible:false` reports return nil Go error. Preserve successful string outputs and the `MCPProvider` interface.
2. Run `GOWORK=off go test ./mcp -run 'TestMCPErrorContract|TestInProcessErrorContract|TestInProcessInventoryOrder' -count=1`; expect old error-stripping, engine IsError and ordering assertions to fail.
3. Replace raw-handler bypass in `InProcessServer.CallTool` with SDK `HandleMessage` dispatch of serialized tools/call JSON-RPC. Use monotonically unique per-instance request IDs for concurrent calls; do not introduce external sessions/network. Decode actual protocol/tool errors and retain bounded caller feedback. Keep audit logs to tool/status metadata; do not log raw args, config, payload or engine error strings. Sort `ListTools` and `ListToolSchemas` by tool name.
4. On `TriggerWorkflow` failure preserve payload but mark tool result error. Do not mark analytical negative results as invocation errors. For deterministic cooperative cancellation, a fake EngineProvider signals an explicit entered-handler barrier and then blocks on `ctx.Done()`. The test waits for that barrier before canceling the provider call context; a dispatcher test with known request ID waits for entry before sending a matching `notifications/cancelled` through the same SDK dispatcher. Require observed ctx.Done, bounded completion, no post-cancel success counter, and a subsequent successful request. Never start a real engine workflow; a cancel sent to an already completed request is not cancellation proof.
5. Run focused tests, `GOWORK=off go test ./mcp -count=1`, and `GOWORK=off go test -race ./mcp -run 'TestInProcessErrorContract|TestInProcessInventoryOrder' -count=1`. Expected `PASS`, no data race, identical sorted names across repeated calls, no canary in audit log.
6. Commit after review. Rollback: revert adapter/engine-error commit as a unit; public interface remains unchanged in both directions.

### Task 3: Audit annotations and prove PR 1 through real CLI stdio

**Files:** Modify registrations in `mcp/server.go`, `mcp/tools.go`, `mcp/wfctl_tools.go`, `mcp/scaffold_tools.go`; create `mcp/annotations_test.go`, `cmd/wfctl/mcp_stdio_test.go`; update `docs/mcp.md`, `docs/mcp-tools-reference.md`; create one available numbered `decisions/<next>-shared-mcp-contracts-and-protocol.md` after checking allocation.

1. Write failing exhaustive annotation tests keyed by each registered name. Read-only computations/previews: true/false for readOnly/destructive; modernize is read-only even when returning fixed_yaml. OpenWorld exceptions: list_plugins, detect_infra_needs, ci_plan, generate_github_actions true for arbitrary local paths/imports; bounded remaining catalogs/computations false. Conditional run_workflow: false/true/true. Assert exact booleans exist; they already exist today, so test semantic mismatches rather than presence alone.
2. Run `GOWORK=off go test ./mcp -run TestMCPAnnotations -count=1`; expect failure against SDK-default meanings. Apply the design's explicit annotation table; audit actual effects before assigning each row. Keep optional idempotence tuning outside scope.
3. Implement a Go stdio contract client in `cmd/wfctl/mcp_stdio_test.go`. Production-CLI mode exercises actual `runMCP` in a fresh subprocess of the compiled Go test binary, with isolated cwd/plugin dirs and a minimal environment. An explicit `WFCTL_MCP_TEST_BINARY` selects a newly built wfctl executable for this mode. A distinct test-only transport-helper mode always launches the Go test binary, constructs the existing server with a blocking fake EngineProvider, and serves actual SDK stdio; it never adds a production tool or substitutes for built-CLI proof. Its existing conditional run_workflow signals `WFCTL_MCP_TEST_HANDLER_ENTERED` on captured stderr after entry, then blocks on ctx.Done; parent waits for this exact barrier before sending the matching cancellation notification. Require observed cancellation, no post-cancel success, bounded completion and subsequent valid request. Keep stdout exclusively MCP JSON. Public fixture YAML lives as test strings; every response is parsed and asserted.
4. Test legacy initialize/initialized for 2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25; tools/list, resources/list, valid call, wrong-type call, mixed array and malformed JSON. Default production count 33 tools/5 resources; run_workflow absent. Test no extra stdout, notification response absence, EOF exit and SIGTERM exit. For the actual/newly built production CLI assert cancellation-notification robustness and subsequent valid-request handling only, not cancellation of a short handler. Cooperative handler cancellation is proved by the separate entered-barrier transport helper (34 tools including the existing engine tool), not by a notification race. Require 5-second per-operation deadlines and always terminate/reap children. Reject raw canaries in stderr/audit logs; allow explicitly requested public fixture data and the transport helper's fixed nonsecret stderr barrier.
5. Run `GOWORK=off go test ./cmd/wfctl -run TestMCPStdioLegacy -count=1` and focused package tests. Then build an isolated executable with `GOWORK=off go build -o "$TASK_MCP_BIN/wfctl" ./cmd/wfctl`, where TASK_MCP_BIN is a fresh temporary directory; run `GOWORK=off WFCTL_MCP_TEST_BINARY="$TASK_MCP_BIN/wfctl" go test ./cmd/wfctl -run TestMCPStdioLegacy -count=1`. Expected exit 0/`PASS`, four negotiated versions and asserted tool errors.
6. Correct docs to 33 tools/5 resources, exact live argument names, manifest startup/restart behavior, negative-report versus invocation-error semantics, sanitized-input/runtime limits and full-surface visibility. Keep existing packaging links untouched. Record ADR rationale for two PRs, error preservation and dependency reuse; check filename availability before adding it.
7. Run `GOWORK=off golangci-lint run --new-from-rev=origin/main` and relevant checks; inspect doc links/rendering and diff for package/lock/secret changes. The parent may open PR 1 as draft under existing publication authorization after its execution/review checkpoints. Rollback: revert PR 1, rebuild and rerun legacy smoke; no installed binary/session mutation.

### Task 4: Upgrade existing SDK after actual API compilation

**Files:** Modify `go.mod`, `go.sum`, `mcp/server.go`; adjust only proven necessary consumers among `mcp/library.go`, `mcp/server_test.go`, `mcp/input_contract_test.go`, `mcp/error_contract_test.go`, `mcp/annotations_test.go`, `cmd/wfctl/mcp_stdio_test.go`. Do not change package manifests, Go directive, unrelated dependencies or generated engine code.

1. Refresh public v1.3.0 release/tag and consumed API review described in design. Confirm module path and no need for another framework; inspect release diff before downloading dependencies. Source/API compatibility is verified at planning time; executable compatibility is not.
2. In isolated authorized checkout pin only mcp-go to v1.3.0; use `GOWORK=off go get github.com/mark3labs/mcp-go@v1.3.0`. Inspect go.mod/go.sum diff; reject unrelated version churn instead of blindly accepting tidy output.
3. Run `GOWORK=off go test ./mcp ./cmd/wfctl -run 'TestMCP|TestInProcess' -count=1` and `GOWORK=off go test ./... -run '^$'`. Expected exit 0 and no unresolved API symbols across real consumers. If changes exceed the listed API adaptation boundary, stop for design review; do not rewrite framework or silently drop PR 2.
4. Set `server.WithCacheHints(0,mcp.CacheScopePrivate)` explicitly. Keep existing capabilities; do not enable tasks/extensions/auth/UI. Preserve legacy payload semantics. Re-run all PR 1 contract tests on the new SDK.
5. Audit versions with `GOWORK=off go list -m github.com/mark3labs/mcp-go` (expect `github.com/mark3labs/mcp-go v1.3.0`); build isolated CLI and inspect `go version -m` for same pin. Launch legacy suite against that executable before continuing.
6. Commit after review. Rollback: restore v0.54.1 go.mod/go.sum and accompanying API/cache adaptations, retain PR 1 fixes, rebuild and pass legacy suite.

### Task 5: Prove July stateless wire behavior and security boundaries

**Files:** Extend `cmd/wfctl/mcp_stdio_test.go`; create `mcp/protocol_contract_test.go`; extend `mcp/input_contract_test.go`. Only minimal `mcp/server.go` adaptation if actual tests show SDK wiring omission; no handwritten protocol engine.

1. Add July stdio tests using per-request `_meta` keys `io.modelcontextprotocol/protocolVersion:"2026-07-28"` and `io.modelcontextprotocol/clientCapabilities:{}`; no initialize/initialized in this client path. Create `DOCUMENTATION.md` inside the test temporary directory with exact public synthetic bytes `Workflow MCP synthetic public documentation fixture\nmarker: PUBLIC_MCP_DOCS_20261019\n`. Launch both the CLI helper and newly built production wfctl with explicit `-documentation-file <fixture-path>` plus isolated plugin/cwd directories; never use operator/repository documentation. Call server/discover, tools/list, resources/list, resources/templates/list, resources/read for `workflow://docs/full-reference`, valid tools/call and invalid typed call.
2. Assert discovery supportedVersions contains July and all four legacy revisions; advertised capabilities match actual tools/resources, without unimplemented extensions. Ordinary results carry resultType complete. On each actual `workflow://docs/full-reference` wire result assert the text equals the exact synthetic file bytes and contains PUBLIC_MCP_DOCS_20261019, plus explicit resultType complete, ttlMs 0 and cacheScope private. Public fallback content must fail this test. The marker may appear in this explicitly requested result and must be absent from captured stderr/audit logs. Assert the same cache fields on other cacheable results. Server identity metadata identifies wfctl-mcp and observed source version; repeated tool ordering is stable.
3. Negative wire cases: missing/wrong-shaped modern clientCapabilities -> -32602; unsupported modern date -> -32022 with supported versions; modern initialize and ping -> -32601. Repeat wrong-argument/engine-failure semantics in dispatcher tests. A malformed request is a protocol error; tool argument/business failure is IsError; analytical negatives remain normal results.
4. Verify no version/capability leakage across successive modern/legacy requests, no unsolicited logging when logLevel absent, no unknown extension advertised, and no attempt to network-resolve schemas. Run schema-inventory compilation offline for every built-in tool, including engine-attached variant. Test external/absolute refs are rejected by the Workflow-owned schema audit; do not make real outbound requests.
5. Run `GOWORK=off go test ./mcp -run 'TestMCPProtocolContract|TestMCPSchemaInventory' -count=1` and `GOWORK=off go test ./cmd/wfctl -run 'TestMCPStdioLegacy|TestMCPStdioJuly' -count=1`. Build isolated CLI and repeat with `WFCTL_MCP_TEST_BINARY` exactly as Task 3. Expected exit 0/`PASS`; assertions observe real response bytes, not SDK constants alone.
6. Reuse Task 3 EOF/frame/canary cases in both eras. In the test-only SDK-stdio helper, wait for the entered blocking fake-handler barrier before sending notifications/cancelled; assert observed ctx.Done, no post-cancel success, bounded completion and subsequent valid request. In the newly built production wfctl, assert only cancellation-notification robustness and continued service. Do not equate that CLI robustness check with deterministic handler cancellation. If SDK behavior fails, report exact contract and stop compatibility claims; a bounded upstream fix/pin needs review, not transport rewriting. Rollback: Task 4 SDK rollback plus legacy suite; no persisted data.

### Task 6: Publish accurate compatibility evidence and finish separate PR 2

**Files:** Update `docs/mcp.md`, `docs/mcp-tools-reference.md` and the Task 3 decision record; no package example or PR1083 files. Final transcripts are sanitized task artifacts, not committed machine-path logs.

1. Document supported revisions from successful launched-CLI matrix, both connection models, explicit private cache policy, current tool/resource inventory, exact argument schemas, restart requirements, input/error changes and adapter parity. Explain July upgrade need without claiming existing initialize clients are broken. Note historical SDK upgrade rather than saying SDK was never maintained.
2. Separate protocol requirements from OpenAI directory policy and optional features. State text-only results remain supported; no structured-output migration or directory/host certification. List remaining filesystem/rate-limit/output-sanitization security work without claiming complete generic security compliance.
3. Run `GOWORK=off go test ./mcp ./cmd/wfctl -count=1`, `GOWORK=off golangci-lint run --new-from-rev=origin/main`, and actual built-binary legacy/July suites. Run full repository checks required by current repo guidance/CI when authorized and resource-safe; do not substitute mocks or prior release probes for new executable evidence.
4. Review integration matrix: CLI/SDK/provider runtime evidence present, engine represented by controlled fake only, desktop activation deferred, HTTP/auth/tasks/UI/Compute unchanged. Inspect docs links/rendered Markdown, dependency diff, tracked/untracked changes, and package/lock exclusions. Any unmet required check remains an explicit blocker.
5. Independently review final changes; the parent may publish PR 2 as draft under existing source/publication authorization after its execution checkpoints. Attach created PR to its task. This plan adds no new merge/deploy/install action. Record tested source SHA, executable digest, SDK version, protocol results and limitations.
6. Prove rollback in the isolated worktree: restore PR 1 base plus v0.54.1, build another temporary CLI, pass legacy suite; restore PR 2 candidate and repeat modern smoke. Expected legacy support returns, July support is unavailable under old pin, then candidate proof passes again. Do not disturb published history or other worktrees.

## Review and remaining gates

Independent adversarial design/plan review passed after P1/P2 corrections; formal alignment/scope lock for future execution remains pending. This plan is deliberately uncommitted outside the checkout per explicit instructions; those instructions override skill defaults to write/commit under docs/plans. No implementation or new PR has been executed. Planning is pending the parent's execution decision under existing human authorization; do not ask for redundant user approval. Before execution, record accepted review/alignment and lock exactly this manifest. SDK compile/runtime success, full CI and host activation remain unknown until their respective checks run.

## Execution continuation

The user explicitly authorized execution after independent review. Planning-only
restrictions above describe the prior phase; Tasks 1–6 now run in isolated
worktrees with the same two-PR grouping and exclusions. No additional human
approval gate. PR 1083 stays separate.
