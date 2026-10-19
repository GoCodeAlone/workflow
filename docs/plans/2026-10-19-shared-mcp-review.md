# Shared MCP compatibility adversarial review

Date: 2026-10-19.
Phase: design + plan; independent review before implementation.
Artifacts: `docs/plans/2026-10-19-shared-mcp-design.md`, `docs/plans/2026-10-19-shared-mcp.md`.
Current verdict: **PASS** after narrow Cycle 2 rereview; initial verdict was REQUEST CHANGES.
Scope: two PRs / six tasks; PR 1083 and all concurrent package changes remain separate. Review changes only this file; no implementation, SDK download/install, build, runtime process, commit or publication performed.

## Original findings — Cycle 1

### P1 — Important: cancellation proof lacks a deterministic in-flight operation

Location: plan Task 2.4, Task 3.3–3.5 and Task 5.6; design Multi-Component Validation / R6.

Task 2's fake EngineProvider waiting on `ctx.Done()` can prove cancellation across the in-process dispatcher. Task 3 instead launches actual `runMCP`, which attaches no engine and exposes 33 predominantly short computations. The plan does not identify an entered, blocking operation or synchronization barrier before sending a cancellation notification. A cancel followed by a successful ordinary request can pass when the target request already completed or never entered its handler. Repeating it in July does not strengthen that proof.

Fix within current scope: make the fake-engine dispatcher test wait for an explicit handler-entry barrier before canceling; require observed `ctx.Done()` and no post-cancel success effect. For actual stdio handler-cancellation proof, specify a separate test-helper process with a controlled blocking handler, entry barrier, real SDK transport, bounded completion and subsequent successful request. Keep this helper clearly distinguished from the unchanged 33-tool production CLI. For the newly built wfctl executable, narrow the claim to cancellation-notification robustness unless a real deterministic handler can be exercised safely. Do not add a production delay/probe tool or launch a real workflow. Preserve five-second operation deadlines and reaping.

Status: **resolved in Cycle 2** — revised Task 2.4 requires entered-handler synchronization before context/notification cancellation; Task 3.3–3.4 and Task 5.6 distinguish the controlled 34-tool SDK-stdio helper from the 33-tool built production CLI. The helper must observe cooperative cancellation and no post-cancel success; production CLI claims only notification robustness. Deadlines/reaping remain required.

### P2 — Important: local-resource privacy claim can be tested against public fallback content

Location: plan Task 5.1–5.2; design PR B cache policy / A3.

The wire plan reads “one public docs URI” but asserts that local full-reference content remains private. In the proposed empty temporary cwd/plugin layout, `workflow://docs/full-reference` can return its built-in public fallback without reading any local documentation. That passes metadata assertions without exercising the local-file boundary at issue. `mcp/server.go` supports an explicit documentation override and otherwise probes configured/local paths; this boundary needs a controlled fixture.

Fix within current scope: write a synthetic `DOCUMENTATION.md` under the test's temporary directory, start the helper and newly built wfctl with explicit `-documentation-file <fixture>`, read `workflow://docs/full-reference`, and assert the exact synthetic marker/content plus `resultType:"complete"`, explicit `ttlMs:0`, and `cacheScope:"private"` on the actual response. Use public synthetic bytes, never operator/repository documentation or secrets. Assert the marker is absent from stderr/audit logs while allowing it in the explicitly requested resource result. This fits `cmd/wfctl/mcp_stdio_test.go`; no new resource or feature is required.

Status: **resolved in Cycle 2** — revised Task 5.1–5.2 supplies exact synthetic DOCUMENTATION.md bytes, explicit `-documentation-file` for helper and newly built CLI, exact full-reference result equality/marker, complete/private/zero-TTL assertions and marker exclusion from stderr/audit logs. Public fallback content cannot pass.

No Critical findings. No additional findings manufactured to meet a count: every mandatory class is scanned below.

## Verified foundations and clean outcomes

- The official [specification entrypoint](https://modelcontextprotocol.io/specification/) currently redirects to 2026-07-28. The [July changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog) supports the plan's stateless request model, discovery, result metadata and deterministic-listing direction. Existing initialize-era operation is not thereby proven broken.
- [mcp-go v1.3.0](https://github.com/mark3labs/mcp-go/releases/tag/v1.3.0) and its [module file](https://github.com/mark3labs/mcp-go/blob/v1.3.0/go.mod) are public. The module path remains unchanged and its Go minimum is 1.25.5, below this repository's Go 1.27.2. Public API inspection supports evaluating this candidate; compilation/runtime success remains unverified.
- Local v0.54.1 `mcp.NewTool` already emits explicit default annotation booleans. Workflow has 11 ordinary registrations without a read-only override and explicitly marks `modernize` false. The proposal correctly fixes meanings rather than claiming absent wire fields. `modernize apply:true` returns transformed YAML without persisting it; local arbitrary-path/import tools warrant the explicit open-world exceptions. Metadata is not authorization.
- v0.54.1 already sorts wire `tools/list`. The unsorted maps are the Workflow in-process `ListTools`/`ListToolSchemas` methods. The plan targets those and adds a wire regression assertion rather than inventing another sort layer.
- Both inspected SDK versions use `santhosh-tekuri/jsonschema/v6` for input validation and skip validation if a schema fails compilation. The plan's inventory compile gate addresses that fail-open risk for Workflow-owned schemas. The compiler's default loader is unset; external references do not trigger network/file loading by default. See [candidate input validation](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/input_validation.go); locally inspected `jsonschema/v6@v6.0.3/compiler.go` and `loader.go` confirm no default URL loader. Do not add one.
- Candidate [response decoration](https://github.com/mark3labs/mcp-go/blob/v1.3.0/server/response.go) already defaults cache hints to private/zero TTL and decorates modern responses. Explicit Workflow policy is reasonable; it is not a fix for an observed public-cache default. [Caching semantics](https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching) permit same-context private reuse; zero TTL marks results immediately stale. Neither field is an access-control mechanism.
- The [tools error contract](https://modelcontextprotocol.io/specification/2026-07-28/server/tools#error-handling) distinguishes malformed/unknown-tool protocol failures from actionable tool failures. A successful validation report with `valid:false` or compatibility report with `compatible:false` is analytical data. The plan preserves this distinction while repairing stripped `IsError` and engine failure flags.
- Current `docs/mcp.md` omits much of the registered surface and promises restart-free manifest discovery. The planned 33-tool/5-resource inventory and startup/restart correction is justified. Runtime binary/type loading and desktop activation remain separate claims.

## Mandatory bug-class scan — initial cycle

This table preserves the initial scan and finding history. Cycle 2 resolves the two findings below; it introduces no additional finding or scope change.

| Class | Result | Specific check |
|---|---|---|
| Project-guidance conflicts | Clean | Read Workflow AGENTS/CLAUDE/agent/layout guidance; future isolation, GOWORK=off, additive tests/docs and package exclusions conform. |
| Assumptions under attack | Finding | P1/P2 expose proof assumptions; SDK source compatibility is appropriately separated from future compilation and launched-byte evidence. |
| Repo-precedent conflicts | Clean | Reuses existing `mcp/`, `runMCP`, SDK, provider interface and JSON Schema dependency; no replacement framework. |
| Artifact-class precedent | Clean | Go tests are beside source; inline public fixtures avoid new runnable root examples; ADR allocation is checked rather than presumed. |
| YAGNI violations | Clean | Remote/auth/tasks/MRTR/UI/output-schema migration and universal hardening are explicitly deferred; two PRs separate correctness from dependency migration. |
| Missing failure modes | Finding | P1: cancellation can race completion and falsely pass; other malformed/schema/tool/engine/EOF/version/cache failures are represented. |
| Security/privacy at architecture level | Finding | P2: private local-file content is not yet exercised; synthetic canaries, minimal env, no raw audit values and no ambient config are otherwise explicit. |
| Infrastructure impact | Clean | No hosting, databases, accounts, listeners, installation, Docker or persistent access; planning authorizes no source operations. |
| Multi-component validation | Finding | P1/P2 need concrete adjacent-component stimuli; real built CLI/SDK/provider boundaries are otherwise required. |
| Declared integration proof | Clean | Matrix labels future CLI/SDK/provider proof, fake-engine boundary and deferred hosts/remote integrations without equating a fake with engine runtime. |
| Contributed UI rendering proof | Clean | No contributed UI or route claims. |
| Rollback story | Clean | PR A reverts contracts; PR B independently restores v0.54.1 while retaining PR A and removing July claims. |
| Simpler alternative not considered | Clean | Combined PR, frozen SDK and server rewrite are considered; none improves the bounded two-stage proof/rollback path. |
| User-intent drift | Clean | Shared MCP maintenance remains separate from packaging PR 1083, active sessions/CMS and Compute work. |
| Existence/runtime-validity | Finding | Consumer APIs and target source files exist; P1/P2 concern stimuli missing from promised real-consumer proof, not imaginary SDK surfaces. |
| Over-/under-decomposition | Clean | Six tasks across two sequential PRs are reasonable; implementers should retain individual RED/GREEN/checkpoint actions within the listed steps. |
| Verification-class mismatch | Finding | P1/P2: current tests can prove robustness/public fallback metadata while claiming handler cancellation/local-resource privacy. |
| Auth/authz chain composition | Clean | No new auth chain; existing local OS/process boundary and engine-embedder responsibility are explicit. |
| Hidden serial dependencies | Clean | PR B follows PR A; shared server/docs/test/ADR files are sequenced, not falsely parallelized. |
| Missing rollback wiring | Clean | Each task includes rollback and Task 6 requires an isolated old-pin/candidate rebuild sequence. |
| Missing integration proof | Finding | P1/P2 require amendments; remaining actual compiled CLI and provider-dispatch tests cross the intended boundaries. |
| Missing declared integration matrix | Clean | Every declared component is runtime-integrated via future evidence, controlled test double or explicitly deferred. |
| Missing contributed UI route proof | Clean | No UI integration. |
| Infrastructure verification mismatch | Clean | No infrastructure mutation; future full checks remain resource-safe and explicit blockers if unavailable. |
| Plugin-loader runtime layout | Clean | No external plugin binary build/load is added; default inventory uses isolated temp directories. |
| Config-validation schema rules | Clean | Tool argument schemas, array items and offline compilation are selected; no new runnable app config or replacement YAML validator. |
| Identifier/naming-convention match | Clean | Checked actual SDK options, flags, tool names and July metadata/error identifiers; ordinary successful payloads/interfaces remain stable. |
| Planned-code compile-validity | Clean | No implementation code snippets require compilation now; exact SDK pin/API compilation and build-metadata gates precede future compatibility claims. |

## Options and remaining unknowns

1. Prefer the smallest cancellation claim: prove cooperative cancellation deterministically through the real dispatcher/fake engine; prove only notification robustness through the unchanged production CLI. Add a test-only stdio blocking consumer only if transport cancellation itself is an acceptance requirement.
2. Keep all July cache tests explicit and local: one synthetic full-reference fixture plus list/read metadata assertions is sufficient. No cache service, public resource, authentication mechanism or operator file is needed.

Current planning blockers: **none from this review**; P1 and P2 are resolved by the revised plan/design. SDK compilation, newly built CLI behavior, rollback execution, full CI and desktop activation remain **unknown future gates**, not failures observed by this review; the plan correctly forbids claiming them early. Broader filesystem/rate-limit/output-security work remains deferred and prevents blanket security/directory-certification claims.

## Cycle 2 — narrow rereview

Verdict: **PASS** for design/plan acceptance. No implementation or publication permission is created by this verdict.

| Finding | Resolution checked | Outcome |
|---|---|---|
| P1 | Design R6, Components cancellation levels and integration matrix; plan Task 2.4, Task 3.3–3.4, Task 5.6 | Resolved: entry barrier precedes cancellation; fake dispatcher and real SDK-stdio helper prove cooperative behavior; unchanged built CLI proves only notification robustness. |
| P2 | Design local full-reference proof; plan Task 5.1–5.2 | Resolved: test-created exact file bytes and explicit CLI flag force local resource retrieval; actual response must match and carry complete/private/zero-TTL metadata. |
| Scope | Scope Manifest and exclusions | Unchanged: two PRs, six tasks; helper uses existing conditional engine tool, no production probe/tool, package changes, remote endpoint or new integration. |

No new Critical/Important finding from these corrections. The existing test helper/file/deadline boundaries make both proposed proofs concrete and falsifiable. Original findings and full initial scan are retained above; no unrelated broad rereview was performed.

Reviewed corrected artifact SHA-256:

- `docs/plans/2026-10-19-shared-mcp-design.md`: `6980878e8c567fce013e7722ffba4be174988c903d24e514b53c05b74de09d7b`
- `docs/plans/2026-10-19-shared-mcp.md`: `e68949e632007d71a5a9ed3a2ccf3510b083ee80df32e9e15aed38b374319d1b`

Read-only source-status check for `mcp/`, `cmd/wfctl/`, `go.mod` and `go.sum` returned clean during this rereview. This is a planning gate only; no future test result is claimed.

## Review evidence / writes

Inspected local Workflow `mcp/{server,library,tools,wfctl_tools,scaffold_tools}.go`, provider interface, relevant tests/docs, CLI startup, cached mcp-go v0.54.1 and jsonschema v6.0.3 source; compared public v1.3.0 module/version/server/dispatcher/stdio/validation/response source and official July specification pages. Existing unrelated working-tree changes were observed and preserved.

Writes: `docs/plans/2026-10-19-shared-mcp-review.md` only. No source edits, commits, installations, SDK compilation, tests, publication or active app/session interactions.
