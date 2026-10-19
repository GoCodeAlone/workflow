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

1. Configure: establish intended app; discover server/version/tools/types/schemas; propose smallest self-contained config; call basic and deep validation, compare module/step/trigger/workflow types against discovered inventories, inspect; deliver editable config and explicit coverage/runtime gaps. No replacement validator is built.
2. Diagnose: classify missing executable, handshake/version, unavailable tool/type/plugin, syntax/validation/compatibility failures; use actual schema arguments; propose minimal repair and revalidate. Never update CLI, install plugins or run app automatically.
3. Readiness: per-check human-readable evidence tied to source revision and exact tested sanitized UTF-8 snapshot digest, observed server version, tool inventory and relevant schemas, basic/deep validation/inspection/compatibility results. Record redaction/partial status; original input equivalence and runtime acceptance stay unknown. Separate observed/inferred/not checked; never aggregate incomplete checks into app PASS. Missing source/version/runtime evidence remains unknown.

Direct MCP entry namespaced `workflow-config`; bare `wfctl` resolves from user's PATH; `-plugin-dir ${PLUGIN_DATA}/engine-plugins` selects isolated startup type discovery/default manifest inventory, `cwd ${PLUGIN_ROOT}`. Skills use `list_plugins({})` only, no `data_dir` override or registry searches; this is not a filesystem sandbox. Users can deliberately select another startup directory in host-local settings; no absolute machine path ships. No CLI binary bundled and no automatic download.

MVP tool allowlist: `list_module_types`, `list_step_types`, `list_trigger_types`, `list_workflow_types`, `get_module_schema`, `get_step_schema`, `generate_schema`, `get_config_skeleton`, `validate_config`, `template_validate_config`, `inspect_config`, `compat_check`, `modernize` with `apply:false`, `list_plugins` with no arguments. Tool availability and supported arguments are still discovered live.

Input boundary: self-contained sanitized single-document YAML only. MCP string loaders do not resolve imports. Detect imports before interpreting results; nonempty imports or uncertain structure means incomplete coverage. User may supply an explicitly flattened sanitized snapshot as a separate artifact; never fetch fragments or silently omit imports. Basic validation skips workflow/trigger types and allows absent entry points; deep validation and inventories improve coverage but do not prove runtime semantics.

Declared prerequisites (`requires.plugins`, `plugins.external`, engine version requirements) are separate from referenced types. Compare declarations to the default plugin inventory, recording missing/unknown versions and binaries as unmet or unknown. Known module types may validate while required plugins are absent; never install, auto-fetch or equate inventory with executable health.

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

Maintainer tests cover missing executable, unknown/mismatched version, missing required tools, missing external module/step/trigger/workflow types, missing declared plugin prerequisites despite known types, unresolved imports, malformed/invalid YAML, synthetic canary suppression, MCP protocol failures and honest unknowns. Capture real v0.86.2 false positives as well as true negatives; tests are not a replacement config validator. No fake demo output.

## Rollback

Delete/uninstall this standalone package in an authorized future test session or revert its additive commit. Existing CLI, plugins, sessions and applications are untouched. No migration or service rollback.

Decision: `decisions/0064-package-workflow-agent-example.md`.
