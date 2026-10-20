# Shared Workflow MCP compatibility design

Status: local design draft for independent review; no implementation, repository writes, commits or publication performed. Execution disposition belongs to the parent under existing user authorization.
Date: 2026-10-19.
Boundary: preserve packaging PR [1083](https://github.com/GoCodeAlone/workflow/pull/1083), its scope lock, installed plugins, active sessions, CMS work, and Compute work. This is a separate shared-MCP proposal.

## Global Design Guidance

Sources: Workflow `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`; explicit delegated task. No canonical `docs/design-guidance.md` was found. These existing guides and the task supply the design constraints.

| Guidance | Response |
|---|---|
| Preserve unrelated work; isolate release-bound changes | Future clean worktree/branches from public main; never amend PR 1083 or change its package |
| Reuse core bootstrap CLI/contracts | Keep `wfctl mcp`, existing `mcp/`, and mcp-go; no replacement server or orchestration layer |
| Update docs/tests with behavior | Shared contract, CLI-wire and documentation checks in each proposed PR |
| GOWORK=off for Go | Every future Go command uses `GOWORK=off` |
| Bounded customer value | Correct existing tools and transport interoperability; no feature expansion or Compute dependency |

## Evidence and requirements classification

Current inspected core matches baseline `b12aff37cfdf40d71e72c875f4c271ec23c28178`; packaging-only changes do not alter `go.mod`, `mcp/`, or `cmd/wfctl/mcp.go`. Release v0.86.2 build metadata identifies revision `92aecb70013735aac2660c60df76ac9f1a0195d2`, Go 1.26.5, mcp-go v0.54.1. Current tree uses Go 1.27.2 and the same SDK. This SDK was upgraded previously; this proposal does not claim it was never maintained.

| ID | Kind | Verified state / required outcome |
|---|---|---|
| R1 | Protocol/correctness | Enable declared-input validation. Current `NewServer` omits the SDK opt-in; release accepts string `skip_unknown_types:"true"` and skips unknown-type checking. Reject invalid argument types as tool errors; preserve valid calls. |
| R2 | Contract accuracy | `get_config_skeleton.module_types` lacks string item schema; handler silently drops nonstrings. Release `['http.server',73]` succeeds and drops 73. Type every described string array and reject the entire malformed invocation. |
| R3 | Protocol/correctness | `InProcessServer.CallTool` strips `IsError`; engine-backed `run_workflow` returns `success:false` without `IsError`. Preserve failure semantics across wire and in-process paths. A successful validation report containing `valid:false` remains a normal successful invocation. |
| R4 | OpenAI directory policy + honest metadata | Explicit annotation booleans already exist via SDK defaults. Fix their meanings per handler; do not describe this as adding absent fields. [OpenAI policy](https://developers.openai.com/plugins/plugin-guidelines#correct-annotation) governs directory review; it is distinct from MCP's optional hints. |
| R5 | Claimed protocol-version support | Latest released spec is [2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/changelog). Existing SDK stops at 2025-11-25. Actual release modern discovery returns -32601, and modern tools/list lacks July metadata. Existing initialize clients still work; no evidence the packaging plugin is broken. |
| R6 | Security/transport evidence | Test genuine CLI framing, EOF, cancellation-notification robustness, argument rejection, no ambient secret/log leakage, offline schemas, private synthetic local-resource caching and deterministic inventories. Prove cooperative handler cancellation separately with an entered blocking fake handler and explicit barrier through the dispatcher and real SDK stdio helper. These are scoped proofs, not blanket security certification. |
| R7 | Documentation accuracy | `docs/mcp.md` lists 11 tools/3 resources and promises restart-free discovery; actual stdio inventory is 33 tools/5 resources. Startup manifest discovery needs restart; metadata inventory is not binary/runtime acceptance. |

Primary sources: [released Workflow dependency](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/go.mod), [server](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/mcp/server.go), [adapter](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/mcp/library.go), [tools/error contract](https://modelcontextprotocol.io/specification/2026-07-28/server/tools), [discovery contract](https://modelcontextprotocol.io/specification/2026-07-28/server/discover).

July support requires stateless `server/discover`, protocol/capabilities per request in `_meta`, ordinary `resultType:"complete"`, version-mismatch errors, and cache fields on supported list/read results. Legacy initialization remains for older revisions. HTTP session/header changes do not justify adding HTTP to this stdio server. Optional tasks, MRTR features, authentication, UI, prompts, sampling and structured outputs are not selected.

## Selected approach and alternatives

Recommended: two sequential reviewable PRs. PR A fixes existing contracts on v0.54.1. PR B upgrades the existing SDK to verified released v1.3.0 and proves both protocol eras. Both preserve tool names, successful text payloads, `MCPProvider` signatures, and local stdio distribution.

Alternatives: one combined PR mixes correctness changes with a major dependency migration and weakens rollback attribution; a new server/SDK rewrite increases maintenance without a customer requirement. Staying on v0.54.1 indefinitely preserves legacy interoperability but cannot substantiate July support.

### SDK release/API review

Candidate v1.3.0 is publicly released (2026-10-14), tagged and signed in GitHub's UI; its module path remains `github.com/mark3labs/mcp-go`, minimum declared Go is 1.25.5. [Release](https://github.com/mark3labs/mcp-go/releases/tag/v1.3.0), [module](https://github.com/mark3labs/mcp-go/blob/v1.3.0/go.mod).

| Existing consumed API | Public v1.3.0 source inspection |
|---|---|
| `server.NewMCPServer(name,version,opts...)`, `AddTool`, `ListTools` | Retained; input validator remains opt-in |
| `server.ServeStdio(server,opts...)` | Retained; no transport rewrite required |
| `MCPServer.HandleMessage(ctx,json.RawMessage)` | Retained; dispatcher routes both eras and decorates modern responses |
| `mcp.Tool.InputSchema.Properties/Required`, `CallToolRequest`, `CallToolResult.IsError/Content` | Retained consumed fields; new protocol fields are additive for this use |
| Tool/property/annotation helpers and ParseString/ParseBoolean | Consumed helpers remain; `WithStringItems` exists in both SDK versions |
| Version negotiation | `version.go` lists July plus all four existing revisions; legacy negotiation is capped at 2025-11-25 |
| Cache hints | `server.WithCacheHints(0,mcp.CacheScopePrivate)` exists; modern response decorator emits cache/result metadata |

Sources: [version.go](https://github.com/mark3labs/mcp-go/blob/v1.3.0/mcp/version.go), [server API](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/server.go), [dispatcher](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/request_handler.go), [stdio](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/stdio.go), [tool types/helpers](https://github.com/mark3labs/mcp-go/blob/v1.3.0/mcp/tools.go), [response metadata](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/response.go).

Selection is supported by released source/API inspection, not compilation. No candidate SDK was installed or built. Future PR B must compile all actual consumers and launch both eras before claiming compatibility; unexpected API churn beyond the listed files stops that PR for review. No opportunistic dependency/toolchain upgrade.

## Components and behavior

- PR A: enable SDK input-schema validation in `NewServer`; audit registered schemas; retain permissive additional properties where currently declared. Add string item constraints to all arrays documented as strings, and defensive all-or-nothing skeleton parsing. Compile all built-in tool input schemas in offline contract tests; the SDK silently skips malformed schemas, so tests must fail on non-compiling definitions or nonlocal `$ref`.
- Route the in-process adapter through the same SDK `HandleMessage` dispatcher using unique per-instance request IDs and existing arguments serialized as JSON; do not call raw handlers around SDK validation. Convert protocol/tool error results to nonnil Go errors while preserving useful bounded tool feedback. Keep successful text behavior and public interfaces. Audit logs record tool/status only, never raw arguments, result bodies, or raw engine errors. Sort both in-process inventory methods by name.
- Set `IsError:true` on engine trigger failure while preserving the existing `success:false` payload. A fake EngineProvider exercises this test; do not trigger a real workflow. Validation reports with `valid:false` or compatibility reports with `compatible:false` are successful analytical results, not invocation errors.
- Cancellation proof has three distinct levels: (1) dispatcher/fake-engine test waits for explicit handler entry, then cancels and observes `ctx.Done()` without post-cancel success; (2) a test-only subprocess serves the existing conditional `run_workflow` through real SDK stdio with the same blocking fake, emits a fixed nonsecret entry barrier on its captured stderr, and the client waits for that barrier before sending a matching cancellation notification; (3) the newly built production wfctl, whose tools are short computations and have no engine, proves only cancellation-notification robustness and continued valid-request handling. No new production delay/probe tool or API. Every operation has a five-second deadline, and child processes are terminated/reaped on failure.
- PR B changes only the existing SDK pin and necessary API adaptation, retaining PR A's behavior. Explicit private/zero-TTL cache hints apply to local documentation and inventories. For local full-reference privacy proof, tests create a synthetic `DOCUMENTATION.md` containing an exact public marker, pass its path explicitly via `-documentation-file` to helper and newly built wfctl processes, and read `workflow://docs/full-reference`. Assert exact synthetic content plus `resultType:"complete"`, `ttlMs:0` and `cacheScope:"private"` on actual wire results; a public fallback cannot satisfy this check. Marker is allowed only in the requested resource result, absent from stderr/audit logs. Never read operator/repository documentation. SDK supplies July dispatch/metadata; Workflow adds assertions rather than reimplementing protocol negotiation.
- Continue treating configs, plugin manifests, documentation and tool outputs as untrusted data. No hooks, shell-execution tools, credentials, network schema dereferencing or telemetry are added.

### Annotation decision table

Apply explicit `readOnlyHint`, `destructiveHint`, `openWorldHint` in each registration, with an exhaustive test for the default 33 tools and conditional engine tool. Do not rely on SDK defaults.

| Tools | readOnly | destructive | openWorld | Reason |
|---|---|---|---|---|
| All existing non-engine tools except openWorld exceptions below | true | false | false | Retrieval/computation/preview; `modernize apply:true` returns transformed YAML but does not persist it |
| `list_plugins`, `detect_infra_needs` | true | false | true | Caller can supply arbitrary filesystem directories |
| `ci_plan`, `generate_github_actions` | true | false | true | cigen loads config files/imports from temporary-path context; arbitrary local references may be examined |
| Conditional `run_workflow` | false | true | true | Engine-specific workflows can cause unknown external/destructive effects |

`registry_search` searches a configured local catalog; it does not contact an arbitrary remote registry. Source audit must confirm every registration fits its row; discovering another external read expands the exception row explicitly, not by setting every tool openWorld. Temporary implementation files used for computation do not imply a persisted customer artifact. Annotation tests cannot serve as authorization tests.

## Security Review

Local process launch/OS permissions remain the access boundary; no remote endpoint or identity flow. CLI has no attached engine, so `run_workflow` remains absent. Engine-backed embedders retain responsibility for authorization. Input validation runs before handlers across both adapters. Public fixtures and temporary isolated dirs only; inherited secret values must never be read for tests or added to logs.

Test malformed/wrong-type input with synthetic canaries, and inspect stdout/stderr/audit logs. Intentionally requested public result data remains allowed; this is not a promise to redact every arbitrary string. Registered input schemas must be self-contained or fragment-local; no HTTP/file `$ref` resolution. Do not claim local directory flags form a sandbox. Path/symlink sandbox redesign, generic rate limiting and universal output sanitization are separate security work; the tools spec lists broader security obligations, so this bounded plan must not claim full generic server security conformance or directory eligibility.

## Infrastructure Impact

No cloud resources, database migrations, listener, host installation, account changes or runtime deployments. Planning is read-only except the two local research files. Future tests may launch only isolated transient CLI/test processes. No Docker builds, app/browser/DB interaction, installed-plugin changes or persistent access.

## Multi-Component Validation

| Integration | Level | Required evidence |
|---|---|---|
| wfctl -> SDK stdio -> JSON-RPC client | runtime-integrated, future | Launch newly built CLI; actual discovery/list/call/error/EOF transcripts, cancellation-notification robustness only, and exact synthetic full-reference content/cache assertions |
| In-process provider -> SDK dispatcher -> handlers | runtime-integrated, future | Same malformed/valid cases, preserved IsError semantics and deterministic listing |
| mcp-go v1.3.0 | runtime-integrated, future | Go consumer compilation and legacy/July wire matrix |
| Optional engine provider | runtime-integrated via test double | Failure and deterministic cooperative cancellation across actual dispatcher and separate SDK-stdio helper; wait for entered-handler barrier before cancel, observe ctx.Done and no post-cancel success; no real engine actions |
| ChatGPT/Codex plugin activation / PR1083 package | deferred and unchanged | Existing host evaluation gate; shared CLI tests do not prove host activation |
| HTTP/auth/tasks/UI/remote services/Compute | deferred | Explicitly outside this plan |

## Assumptions and self-challenge

| ID | Assumption | Failure/fallback |
|---|---|---|
| A1 | Source-compatible v1.3.0 compiles with actual consumers | Compile/API gate precedes merge; unexpected spread stops PR B for review, no rewrite |
| A2 | SDK dispatcher preserves adapter success payload shape | Differential tests on representative tools before/after; retain public interface |
| A3 | SDK emits accurate July cache/metadata over stdio | Observe actual bytes; no support claim based on constants alone |
| A4 | Correct annotations can reflect existing behavior | Audit arbitrary-path/import tools; retain conservative engine labels |
| A5 | Legacy clients stay interoperable | Four-version matrix and independent PR B rollback |

Top doubts: metadata accuracy is not access control; SDK validation can fail open on invalid schemas; SDK release notes are not Workflow runtime proof. The proposed tests and limited claims address these without feature expansion.

## Rollback

PR A: revert its additive contract/annotation changes together; rebuild and rerun legacy wire smoke. Document restored malformed-input permissiveness rather than concealing it. No migration or persistent state.
PR B: revert SDK/API/cache changes to v0.54.1 while retaining PR A; rebuild and run legacy smoke. July support is then explicitly unavailable; public docs must revert that claim with the pin. Never edit installed CLI binaries or active plugin sessions.

## Review and execution gate

Primary owner obtains independent adversarial review of this design and the companion plan. They remain local drafts, uncommitted and unlocked, pending the parent's execution decision. Existing human authorization remains applicable; this document adds no new human approval requirement. The parent applies review/alignment/scope-lock checkpoints before execution under that authorization. Future ADR: record the two-stage SDK decision and retained success/error contracts in an available numbered `decisions/` file; do not overwrite another workstream's allocation.
