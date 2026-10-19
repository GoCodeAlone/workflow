# Workflow Assistant build evidence

Captured 2026-10-19 UTC. Plan: `2026-10-19-workflow-agent-plugin.md`.
Source: [draft PR #1083](https://github.com/GoCodeAlone/workflow/pull/1083).

## Milestones

| milestone | evidence |
|---|---|
| Public foundation/design | Public baseline `b12aff37cfdf40d71e72c875f4c271ec23c28178`; ADR/design committed before implementation. |
| Independent design/plan review | Five substantive coverage/evidence findings and one argument-boundary clarification resolved before scope lock; adjacent reports retain details. |
| Portable package | Three skills, root manifests, shared contract, public fixtures/license; direct existing wfctl MCP. |
| Released MCP proof | Real v0.86.2 discovery and six fixture cases; known validator false positives retained explicitly. |
| Go conversion | User-requested replacement of Python consumer/tests, ADR 0065; existing pinned public schema dependencies only. |
| Discovery correction | Failure fixtures renamed `.yaml.txt`, bytes unchanged, outside recursive runnable `.yaml` discovery. |
| Disposable host proof | Codex CLI 0.163.0 loaded Workflow Assistant 0.1.0 and public Autodev 6.6.3 together; 3 Workflow skills, 33 MCP tools, 5 resources. |

## Reproducible verification

From `example/workflow-agent-plugin/tests` using Go 1.27.2:

```sh
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go run . -wfctl /path/to/reviewed/wfctl -expect-version v0.86.2
```

The corrected race-enabled suite passed: 22 substantive top-level tests plus
the subprocess helper entry. Vet and released MCP smoke passed. Independent
review identified an invalid-UTF8 snapshot identity regression; strict rejection
now occurs before child launch, with additional strict protocol decoding. Both
fixes failed when reverted and passed after restoration. A synchronized
notification test proves notifications entered the path before its deadline;
context-cancellation mutation also fails as expected. The root integration test
file executed the actual nested race suite successfully (9.105 seconds), and
its vet/format checks passed without compiling the full engine. No Python/pip dependency remains in the
package. The runnable smoke uses Go stdlib only; schema libraries are test-only.
No Workflow engine/private module is imported. The inherited build cache is
preserved. Existing repository versions are reused: jsonschema/v6 v6.0.3,
regexp2 v1.11.0 and indirect x/text v0.42.0. ECMA negative lookahead in the
unchanged official schema requires the regex adapter; no weaker schema is used.

EOF-first cleanup, full-queue cancellation and one overall notification deadline
each failed when deliberately reverted and passed after restoration. Tests cover
broken stdin and actual child-process canary output. Doubles are not integration
proof; the released executable run is. A standalone copy outside the checkout
passed the final 22-test race suite with dependency/toolchain downloads disabled and the actual
released MCP smoke. No organization access or secret input was needed.

A related root Go integration test runs the nested-module race suite through
the existing CI command. A direct workflow step was discarded after local
policy correctly rejected its unstaged context; the three-PR trust lifecycle
remains intact. No workflow/authority or unrelated test/benchmark policy changes. No local root build, Docker build,
live application, database or browser was used.

## Provenance

- [wfctl v0.86.2](https://github.com/GoCodeAlone/workflow/releases/tag/v0.86.2), published 2026-10-04. Darwin arm64 SHA256 verified against release checksums: `020e8a56927bcbcccbcab2e4ef88fa09a98b22046873c117bc17b06ee042933a`. Installed CLI remains v0.85.4.
- Real initialization: `wfctl-mcp v0.86.2`, legacy MCP `2024-11-05`, 33 tools. Go inventory JSON SHA256 `ada560c5e9f9306b33c0d8374de7d89f1ad3409eaba3527625c1e5a3a019e7f0`. This hashes Go encoding/json bytes; historical Python serialization hashes are not interchangeable.
- Official Agent Plugins 1.0.0 schemas fetched 2026-10-19, unchanged. Plugin SHA256 `0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883`; MCP SHA256 `6539175bfcdf43085855183e86da40ea94b166547a72b47ae9a0a390516d3acb`. Copies/license/provenance under tests/schemas. Host extension semantics require separate evidence.
- Minimal public fixture SHA256 `5c622dee665432094842b38494b25fc37fd184ae0184167649e71bfb5c49f3f8`. Filename correction preserves exact UTF-8 bytes; no customer application is identified.
- [Codex rust-v0.163.0](https://github.com/openai/codex/releases/tag/rust-v0.163.0), published 2026-10-15. Archive SHA256 verified against release asset digest `61c4d9a92521bdb42b0c1d9f87c673f6da628a06de5945cbdd9a0010632f6d10`; executable SHA256 `8813367285524ac798dd96d1b071aea30d58708c7226fd236cd275cf1cfdee31`.
- Public Autodev v6.6.3 tag: `06e28925f6df133d342c4cd6924103223045c46f`; installed user copy not used as fixture.

## Actual behavior and limits

| check | result | coverage |
|---|---|---|
| Basic/deep minimal + inspection | accepted; one module | static topology only |
| Compatibility | compatible at v0.86.2 | current registry; no target-version argument |
| Malformed YAML | tool error | expected negative |
| Missing module | validation rejected/incompatible | expected negative |
| Unknown workflow/trigger/step | basic accepts; deep rejects; inventories lack types | basic skips checks |
| Missing import | basic accepts | imports unresolved, coverage incomplete |
| Known type + missing prerequisite | basic/compat accept; inventory empty | prerequisite unmet; binary/version unknown |
| Missing/older executable | bounded missing_executable/version_mismatch | no install/update attempted |
| Unknown version/server/tool/schema/protocol/timeout | unit negatives reject | doubles, not integration proof |
| Synthetic canary | raw errors/config/child stdout/stderr suppressed | consumer error paths, not model redaction |
| Teardown | EOF; bounded signal fallback | child/reader regressions exercised |
| wfctl stderr | known prerequisite-version deprecation only | other stderr fails |
| Example discovery | explicit minimal `.yaml.txt` validates; failure vectors excluded | no unrelated example/test modified |

## Disposable host and coexistence

Supported local marketplace commands registered public package copies in fresh
home/state/temp/work directories. File-only auth stores contained no auth files.
Analytics, history, feedback, hooks, apps, daemon auto-start and remote plugins
were disabled. An outer OS sandbox denied network and writes outside the
disposable test directory; containment probes confirmed both denials. User-home
contents outside the authorized task workspace and keychain lookup were denied
by policy. No shared app, daemon, database, browser or installed-session state
was used.

Only initialize/initialized, plugin/list, plugin/read, skills/list and
mcpServerStatus/list were called over private stdio, with explicit gateway OAuth
mode. No thread, model turn or authentication API. Reader threads were joined
before evidence persistence.

| result | observed |
|---|---|
| Disposable installed/enabled plugins | Workflow Assistant 0.1.0; public Autodev 6.6.3 |
| Skills | 3 Workflow, 27 Autodev, 5 host built-ins; no collisions/load errors |
| Paths | all discovered skills inside disposable state |
| Host-launched MCP | workflow-config linked to Workflow plugin; wfctl-mcp v0.86.2 |
| Inventory | 33 tools, 5 resources; no error/remaining page |
| Process | EOF exit 0; drained stderr empty |

Runtime manifests, contract and all three skills byte-match reviewed source.
Independent review: SHIP-IT for bounded loading/discovery claims, with two minor
evidence caveats corrected and rerun. Raw host logs contain generated device
identifiers and remain private scratch; they are not publication artifacts.

Installed Autodev snapshot: 201 selected manifest/Markdown/shell files unchanged.
Public package/docs/fixtures are audited for private paths, keys, credentials,
internal domains/accounts and dependencies before publication. The full upstream
MCP surface remains visible: skill instructions are not an OS sandbox. Compute
retains scheduling, proofs and accounting; no operational expansion.

## Acceptance and distribution

| integration | result | remaining |
|---|---|---|
| Released MCP and portable schemas | passed | other CLI releases/host semantics untested |
| Codex loader/discovery with Autodev | passed isolated test | model selection, prompts/follow-ups, injection/negative behavior |
| ChatGPT desktop | not tested | full authorized isolated desktop acceptance |
| Application runtime | not tested | startup/health/secrets/dependencies/routes |
| Compute | outside scope | no expansion |

Ready for draft source review, **not an install-certified/customer release**.
PR stays draft. Final exact-head hosted CI must be reported from terminal check
results. Earlier head `4c4814b` exposed the fixture discovery defect corrected
here; its benchmark parser rejected an unrelated upstream Sonic Go 1.27 warning
before comparing samples. No benchmark policy was weakened.

Head `2289194` passed hosted example validation, builds, example lint, policy and
security checks. Root lint found a deprecated runtime.GOROOT use in the new
integration test. PATH-based Go lookup fixes that single diagnostic; the exact
CI linter v2.14.0 reports zero issues locally, and independent wrapper review,
vet and race execution pass. A subsequent exact-head CI run remains required;
the standalone package tree/archive did not change.

Public GitHub distribution is separate from directory eligibility. Local/stdio
Plugin Directory submission requires approved-partner eligibility, still unknown.
No submission, remote endpoint/tunnel, credential setup, active-session install,
visibility change, merge or deployment. Final archive digest belongs with the
exact-source result; the earlier Python archive is superseded.
