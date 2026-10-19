# Workflow Assistant

A small, portable plugin for configuring and diagnosing
[GoCodeAlone Workflow](https://github.com/GoCodeAlone/workflow) apps. Three skills
reuse the existing `wfctl mcp` server and produce source/version-linked evidence
with explicit coverage limits. No service, dashboard or orchestration platform.

| Skill | Try asking |
|---|---|
| `workflow-configure` | “Create the smallest Workflow HTTP config using built-in types and validate it.” |
| `workflow-diagnose` | “Explain this sanitized config failure and propose the smallest fix.” |
| `workflow-readiness` | “Produce readiness evidence for this exact sanitized config and source commit; list what remains untested.” |

## Prerequisites and local setup

Supply a trusted `wfctl` executable yourself. This package does not bundle,
download or update it. The tested release is
[v0.86.2](https://github.com/GoCodeAlone/workflow/releases/tag/v0.86.2).
Choose the asset for your OS/architecture and verify its checksum against that
release's `checksums.txt` before using it. Follow the
[public CLI setup guide](https://github.com/GoCodeAlone/workflow/blob/v0.86.2/docs/mcp.md).
`wfctl version` should report the selected version. Other releases are untested;
the skills discover tools dynamically and retain unknown version claims.

Copy this directory from a reviewed immutable Workflow commit. The **plugin root
is this directory**, with `plugin.json`, `mcp.json` and `skills/` directly inside
it. Use your host's documented local-plugin/repository-source workflow to select
that root in a separate test session. Installation is a user action; there is no
installer script and enabling this package does not replace Autodev. Do not copy
files over an existing installed plugin or edit active-session configuration.

The host must support Agent Plugins 1.0.0, skills, stdio MCP, `${PLUGIN_ROOT}`
and `${PLUGIN_DATA}` expansion. Its launcher PATH must resolve `wfctl`; a GUI
host's PATH can differ from a terminal. Resolve missing executables in the host's
local settings before proceeding. The MCP server is named `workflow-config` and
launches `wfctl mcp -plugin-dir <plugin-data>/engine-plugins` with update notices
disabled. No shell wrapper or arbitrary shell execution tool is supplied.

That directory selects the default manifest inventory and startup-loaded types.
It starts empty; no engine plugin is required for the bundled HTTP fixture. If an
app needs external plugins, the user must separately select an appropriate
startup directory in host-local MCP settings. Inventory is not proof of binary
health, and these skills never install/auto-fetch dependencies or override the
inventory path through tool calls. Restart discovery after changing startup
plugins; do not assume live inventory changes reload the server's type registry.

## Hosts and distribution

| Surface | Package intent and verification status |
|---|---|
| Local Codex supporting the portable format | Intended local skills + stdio integration. Official schemas and real stdio calls tested; host installation, activation and tool selection **not yet tested**. |
| ChatGPT desktop with local execution/plugin support | Same intended package, subject to account/workspace features. Complete desktop activation **not yet tested**. |
| Other Agent Plugins clients | Requires skills + stdio + both placeholders; no host certification claimed. |
| ChatGPT browser, remote/cloud executor | Cannot directly run this local executable. No remote endpoint, tunnel or hosted bridge included. |

Public GitHub source distribution is separate from the OpenAI Plugin Directory.
[Local MCP directory submissions](https://developers.openai.com/plugins/deploy/desktop-only)
require approved-partner eligibility and allowlisting. Eligibility is unknown;
this example makes no directory availability claim and submits nothing.
Official [packaging](https://developers.openai.com/plugins/build/plugins) and
[connection/testing](https://developers.openai.com/plugins/deploy/connect-chatgpt)
guides describe host-specific steps. New packages use root manifests; older hosts
requiring a compatibility overlay are outside this example's verified scope.

## What evidence means

Every report identifies the exact **sanitized snapshot actually tested**, its
SHA256 if available, the supplied source commit/path, observed server version,
tools/schemas used and separate check outcomes. Redaction/partial status and
equivalence to original input are distinct. No aggregate deploy-ready badge.

The real v0.86.2 interface differs from portions of its reference documentation:

- `get_module_schema` takes `module_type`; `get_step_schema` takes `step_type`.
- `compat_check` takes `yaml_content`. It checks the running module/step registry;
  it cannot check an arbitrary `target_version`.
- Basic validation skips workflow/trigger checks and allows no entry point even
  with `strict:true`. Deep validation and relevant type inventories add evidence.
- MCP string loaders do not resolve imports. Imported/multi-file applications
  need an explicitly supplied flattened sanitized snapshot or incomplete coverage.
- Required plugin declarations may pass structural/type checks even when the
  default inventory is empty. Version constraints and binary health need proof.

The [shared contract](references/workflow-contract.md) defines the exact workflow
limits. Successful validation does not prove runtime startup, resolved secrets,
external services, authentication, migrations, routes, health or compatibility
with another release. The [minimal fixture](fixtures/minimal.yaml) only declares
a loopback HTTP server with an ephemeral port; it has no endpoint/business logic
and the smoke test does not start it.

## Privacy and coexistence

Use public or sanitized input only. Never provide credential values, environment
dumps or private infrastructure details. The host/model still sees tool inputs
and results; this is not a guarantee of local-only data processing. The package
adds no telemetry or external endpoint. Upstream wfctl and host policies apply.

Skills restrict themselves to discovery/schema/config checks, treat config and
tool text as untrusted data, and summarize errors without copying sensitive raw
output. The complete upstream tool surface is still available under host
permissions; skill instructions are not a security sandbox. Review the trusted
CLI binary and host permissions before connecting.

[Autodev](https://github.com/GoCodeAlone/autonomous-dev-kit/blob/main/docs/README.codex.md)
remains a separate generic development plugin. This package has distinct plugin,
server and skill names, no hooks, no settings writes, no session lifecycle and no
dependency on Autodev. Static coexistence checks do not prove simultaneous host
activation. [Compute's adapter](https://github.com/GoCodeAlone/workflow-plugin-compute)
retains its separate boundary; scheduling, proofs and accounting stay in Compute.

## Maintainer verification

Python 3.10+ is needed only for tests; the plugin itself uses the host and wfctl.
From this directory, create an isolated test environment and run:

```sh
python3 -m venv /tmp/workflow-plugin-test-env
/tmp/workflow-plugin-test-env/bin/python -m pip install -r tests/requirements.txt
/tmp/workflow-plugin-test-env/bin/python -m unittest discover -s tests -v
python3 tests/mcp_smoke.py --wfctl /path/to/reviewed/wfctl --expect-version v0.86.2
```

Choose a fresh temporary venv path if that example path already exists. The smoke
client uses only bundled public fixtures, launches the actual manifest command
with temporary plugin data/minimal environment, and emits safe summaries and
digests. It tests positive/negative configs and known upstream false positives.
It does not accept user YAML or read organization repositories/secrets. Missing
executable/version/tools or protocol failures return a bounded failure category.
Unit-test doubles cover failures; only the real release run is MCP integration
proof. Vendored schemas and their license are documented in
[tests/schemas/README.md](tests/schemas/README.md).

Before recommending a supported host, complete a clean isolated desktop install
with the user's authorization: run all three prompts, follow-ups, missing CLI and
tool cases, imported/prerequisite cases, synthetic secret/prompt-injection cases,
negative unrelated requests, and simultaneous Autodev activation. Record selected
skills/tools and outputs. No active-session install or host approval bypass is
part of this build. Removing the package/reverting its source is the rollback;
there is no service or database state to undo.
