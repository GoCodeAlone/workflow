# Workflow agent plugin design

Status: bounded build authorized by the task; independent review before implementation.

## Assessment and public foundations

- Home: `example/workflow-agent-plugin/` in the public Workflow repository. `plugins/` contains Go engine plugins; this is a distributable assistant integration and example, not a new engine plugin. `docs/REPO_LAYOUT.md` reserves `example/` for full examples and external plugin samples. No existing assistant package was found by `rg --files -g '*plugin.json' -g '*mcp.json' -g SKILL.md`.
- Source baseline: `b12aff37cfdf40d71e72c875f4c271ec23c28178`; released CLI: [v0.86.2](https://github.com/GoCodeAlone/workflow/releases/tag/v0.86.2), published 2026-10-04. Darwin arm64 SHA256: `020e8a56927bcbcccbcab2e4ef88fa09a98b22046873c117bc17b06ee042933a`, verified against release checksums.
- [MCP guide](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/docs/mcp.md), [implementation](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/mcp/server.go), [reference](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/docs/mcp-tools-reference.md). A real stdio initialize + tools/list observed server version v0.86.2 and 33 tools. `compat_check` has only `yaml_content`, contrary to reference documentation's `config`/`target_version`. `list_plugins` accepts `data_dir`. Live schemas win.
- [Official packaging](https://developers.openai.com/plugins/build/plugins), [architecture](https://developers.openai.com/plugins/concepts/plugins), [desktop limits](https://developers.openai.com/plugins/deploy/desktop-only), [testing](https://developers.openai.com/plugins/deploy/connect-chatgpt). Fetched Agent Plugins 1.0.0 JSON schemas; validate emitted manifests against these schemas. OpenAI extension semantics are host-specific and require separate host verification.
- [Autodev](https://github.com/GoCodeAlone/autonomous-dev-kit/blob/main/docs/README.codex.md) remains a separate generic development plugin. [Compute adapter](https://github.com/GoCodeAlone/workflow-plugin-compute/blob/main/README.md) delegates scheduling/proofs/accounting to Compute. Neither is a runtime dependency.

## Global Design Guidance

Source: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`; task's detailed product constraints provide the remaining guidance. No repo `docs/design-guidance.md` or portfolio tracker found.

| constraint | response |
|---|---|
| Portable public customer workflows | three narrowly triggered skills; no organization context required |
| Reuse public wfctl | direct `wfctl mcp`; no wrapper, new server, hook or shell MCP tool |
| Preserve concurrent work | fresh clone + isolated branch; no installed plugin changes |
| Small scope / Compute boundary | no scheduler, dashboard, runtime deployment, CI generation or Compute expansion |
| Examples under example/ | self-contained package and secret-free fixtures inside existing root |

## Components and workflows

Root `plugin.json` + `mcp.json`, `skills/workflow-configure`, `skills/workflow-diagnose`, `skills/workflow-readiness`, shared reference, README, MIT license, minimal valid/invalid/unknown-plugin YAML fixtures, dependency-free Python stdio smoke test and package contract tests. Tests are maintainer tools, not exposed MCP capabilities.

1. Configure: establish intended app; discover server/version/tools/types/schemas; propose smallest config; validate with unknown types enforced; inspect; deliver editable config and explicit runtime gaps.
2. Diagnose: classify missing executable, handshake/version, unavailable tool/type/plugin, syntax/validation/compatibility failures; use actual schema arguments; propose minimal repair and revalidate. Never update CLI, install plugins or run app automatically.
3. Readiness: human-readable evidence tied to source revision/config digest, observed server version, tool inventory and relevant schemas, validation/inspection/compatibility results. Separate observed/inferred/not checked; config validation is not runtime acceptance. Missing source/version/runtime evidence remains unknown, never PASS.

Direct MCP entry namespaced `workflow-config`; bare `wfctl` resolves from user's PATH; `-plugin-dir ${PLUGIN_DATA}/engine-plugins` isolates manifest inventory from ambient app directories, `cwd ${PLUGIN_ROOT}`. Users can deliberately select another directory in host-local settings; no absolute machine path ships. No CLI binary bundled and no automatic download.

## Alternatives and self-challenge

- Recommended: skills + direct MCP package. A prose-only guide is cheaper but does not package repeatable workflows. A wrapper server could constrain tools but would duplicate a mature interface and create maintenance/transport surface.
- Doubt 1: schema/docs drift → discover tools every connection; never infer target version compatibility.
- Doubt 2: unfiltered upstream MCP surface/weak annotations → explicitly constrain skills to discovery/schema/config tools; host approvals still apply, and package is not a security sandbox. Audit actual registered handlers; no execution tool added by package.
- Doubt 3: host loading not proven by JSON schema tests → distinguish schema/MCP evidence from desktop skill activation; host session tests remain pending because active installs are excluded.

## Assumptions

| id | assumption | challenge/fallback |
|---|---|---|
| A1 | user supplies a compatible wfctl executable | missing/unknown versions stop dependent claims; show manual setup |
| A2 | portable host supports stdio + PLUGIN_ROOT/PLUGIN_DATA | document required host features; unsupported remote/cloud excluded |
| A3 | tools/list accurately describes live server | validate required arguments; protocol failures stop claims |
| A4 | plugin inventory proves only manifests present | does not prove binary health, type loading or runtime acceptance |
| A5 | user-authorized config can be shared with model | request sanitized minimal YAML, never secrets, full env, credentials or raw logs |

## Security Review

Configs/tool results are untrusted data, never instructions. Redact before sending YAML, never retrieve secret values. Do not echo raw tool errors or source files into evidence; safe summaries only, secret references retained. No secrets in fixtures/test output. Package adds no telemetry, network endpoint, privileged installer, authentication or persistent access. CLI and host are existing trust boundaries; upstream tooling is not sandboxed by skill text. Controlled smoke tests use temporary working/plugin directories, minimal environment and public fixtures, without services or network.

## Infrastructure Impact

None: no app/DB/browser interaction, Docker builds, deployments, hosted services, credential creation, directory submission, visibility changes or active-session install. Distribution is reviewed GitHub source/draft PR; public-directory eligibility is unknown and partner-gated for local MCP.

## Multi-Component Validation

| integration | level | evidence |
|---|---|---|
| released wfctl stdio | runtime-integrated | initialize, tools/list, schema, valid/invalid config, inspect, compatibility, missing plugin |
| Agent Plugins manifests | config-only | official 1.0.0 schemas + path/secret/namespace audit |
| ChatGPT/Codex skill activation | deferred | clean desktop install/selection eval requires separate isolated host session; not promised as tested |
| Autodev coexistence | config-only | distinct plugin/server/skill names, no hooks/settings writes, unchanged installed files |
| Workflow application runtime | deferred | no app launched; runtime acceptance stays not checked |
| Compute | deferred | no adapter/service dependency |

Maintainer tests cover missing executable, unknown/mismatched version, missing required tools, missing external type/plugin, malformed/invalid YAML, redaction policy, MCP protocol failures and honest unknowns. Capture real v0.86.2 results; no fake demo output.

## Rollback

Delete/uninstall this standalone package in an authorized future test session or revert its additive commit. Existing CLI, plugins, sessions and applications are untouched. No migration or service rollback.

Decision: `decisions/0064-package-workflow-agent-example.md`.
