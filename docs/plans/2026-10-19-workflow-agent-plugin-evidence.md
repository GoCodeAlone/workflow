# Workflow Assistant build evidence

Captured 2026-10-19 UTC. Plan: `2026-10-19-workflow-agent-plugin.md`.

## Milestones

| milestone | evidence |
|---|---|
| Public foundation and bounded design | Workflow public baseline `b12aff37cfdf40d71e72c875f4c271ec23c28178`; design + ADR committed before implementation. |
| Independent design and plan review | `design_skeptic` and `plan_skeptic`; five substantive coverage/evidence findings resolved before scope lock, minor argument-boundary clarification resolved too. See adjacent review reports. |
| Three-skill portable package | Task 1 `d70ba7f`; root manifests, focused skills/shared contract, public fixture, docs and license. |
| Real released MCP + failures | Task 2 `1383f64`; initial 14 tests and released stdio smoke pass; final Task 3 lifecycle corrections `a3331d3` bring the suite to 17 tests. |
| Standalone extraction and privacy/coexistence audit | Final export of `a3331d3` tested outside checkout; 17 tests + real stdio smoke pass with no organization access or secret input. |

## Reproducible verification

From repository root, using a task-local virtual environment with
`example/workflow-agent-plugin/tests/requirements.txt` installed:

```sh
python -m unittest discover -s example/workflow-agent-plugin/tests -v
python example/workflow-agent-plugin/tests/mcp_smoke.py --wfctl /path/to/reviewed/wfctl --expect-version v0.86.2
git diff --check
```

Observed final: 17 tests, zero failures; smoke exit 0; diff check exit 0.
Tests were written before package/client implementation and initially failed for
missing artifacts. Shutdown regression was reproduced as `-15 != 0`, fixed by
EOF-first teardown, then explicitly reverted/restored: FAIL → PASS. No full Go
suite/lint or Docker build was run: no Go, engine, workflow/CI or build files changed.

Standalone check: `git archive HEAD example/workflow-agent-plugin` extracted into
a temporary directory; tests and smoke launched from outside the checkout. The
first audit invocation accidentally resolved the venv interpreter symlink to its
base interpreter and lacked jsonschema; using the venv executable path corrected
the harness. The final exported package at `a3331d3` has 23 tracked files and needs only the
reviewed wfctl plus Python/test-only jsonschema. No package changes were needed.

## Release and schema provenance

- [Released wfctl v0.86.2](https://github.com/GoCodeAlone/workflow/releases/tag/v0.86.2), published 2026-10-04. Workspace-local Darwin arm64 asset, SHA256 verified against release `checksums.txt`: `020e8a56927bcbcccbcab2e4ef88fa09a98b22046873c117bc17b06ee042933a`. Installed CLI remained v0.85.4.
- Real initialization: `wfctl-mcp`, `v0.86.2`, MCP `2024-11-05`; tools/list returned 33 tools. Complete discovered tool metadata digest: `cd0a7901b69cde79d23d05b3f3fb188d18ea785166cbb01b9b90ffdd603d37a5`.
- Agent Plugins 1.0.0 official schemas fetched 2026-10-19, validated with jsonschema 4.26.0. Plugin schema SHA256 `0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883`; MCP schema SHA256 `6539175bfcdf43085855183e86da40ea94b166547a72b47ae9a0a390516d3acb`. Copies/license/provenance retained in package tests. OpenAI extension semantics remain outside portable schema validation.
- [Fixture source](https://github.com/GoCodeAlone/workflow/tree/1383f64/example/workflow-agent-plugin/fixtures) uses exact committed UTF-8 bytes. Minimal fixture SHA256 `5c622dee665432094842b38494b25fc37fd184ae0184167649e71bfb5c49f3f8`. Secret-free, unredacted public fixture; this digest does not identify any customer app.

## Observed behavior and limits

| check | actual result | coverage |
|---|---|---|
| Basic + deep minimal | accepted | static fixture only |
| Inspection | one module | parsed topology |
| Compatibility | compatible at running v0.86.2 | module/step registry, no target-version argument |
| Malformed YAML | tool error | expected negative |
| Missing external module | validation rejected, incompatible | expected negative |
| Unknown workflow/trigger/step | basic accepted; deep rejected; unknown inventories absent | shows skipped basic checks; no complete semantic validator claim |
| Missing import | basic accepted | import coverage incomplete; no resolver added |
| Known type + absent declared plugin | basic/compat accepted, default inventory empty | prerequisite unmet; version/binary health unknown |
| Missing executable | safe failure category, exit 1 | no install attempted |
| Installed older CLI | real `version_mismatch`, exit 1 | existing binary unchanged |
| Unknown version/wrong server/missing tool/schema/protocol/timeout | unit negative cases rejected | failure doubles are not integration proof |
| Synthetic canary | exceptions plus real child stdout/stderr suppressed | consumer error-path proof; actual desktop/model redaction not certified |
| Teardown | normal EOF exit | no cancellation error; bounded terminate/kill fallback |
| Server stderr | known inline-version deprecation only | fixture intentionally exercises deprecated requirement version syntax; no unexpected stderr |

## Privacy, coexistence and operational boundary

Audited all 31 changed source files at Task 2 for machine home paths, private
keys, realistic token literals and internal-domain URLs: zero matches. Reviewed
package/docs/fixtures manually for private repositories, credentials, accounts
and infrastructure: public sources and deliberate example names only. Executable
content is maintainer Python tests, not a hook or MCP shell tool. No CLI bundled.

Installed Autodev read-only snapshot: 201 selected manifest/Markdown/shell files
unchanged; plugin/server/three skill namespaces do not overlap. No installed
plugin/session configuration was written or reloaded. This is config-only
coexistence proof. No shared app, database or browser actions, heavy builds,
deployments, credentials, persistent access or telemetry were introduced.

## Deferred acceptance and distribution

| integration | level | remaining proof |
|---|---|---|
| Released stdio MCP | runtime-integrated | passed above |
| Portable manifests | config-only | official schemas pass; host extension behavior not proven |
| ChatGPT/Codex activation | deferred | authorized clean isolated desktop install, discovery/tool selection, all three workflows and negative/injection cases |
| Simultaneous Autodev activation | config-only | live host coexistence deferred; existing installation preserved |
| Application runtime | deferred | no app server launched; startup/health/dependencies/secrets/routes not checked |
| Compute | deferred | no dependency or scheduling/proof/accounting expansion |

Source is suitable for draft GitHub review, not an install-certified or publicly
listed release. Local/stdio directory submission requires partner eligibility;
eligibility remains unknown. No directory submission, remote service/tunnel,
credential setup, active-session installation, visibility change or deployment
is performed by this build. Full host/runtime acceptance must stay pending.

## Independent package review corrections

`package_skeptic` round 1 found one Important broken-stdin cleanup failure and
two Minor queue/deadline failures. `a3331d3` handles broken stdin before process
reaping, interrupts full-queue reader writes on shutdown and uses one monotonic
response deadline across notifications. Real child-process and deterministic
clock regressions each failed with their fix reverted and passed when restored.
Final standalone export: 17 tests PASS plus real released MCP PASS. These are
maintainer consumer fixes; plugin transport and engine behavior are unchanged.
