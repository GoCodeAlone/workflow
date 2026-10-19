# Workflow Agent Plugin Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a standalone public Workflow assistant package for configure, diagnose and readiness workflows.

**Architecture:** Three skills call existing wfctl MCP directly using discovered schemas. A secret-free fixture and maintainer stdio consumer test the released CLI; package validation and explicit deferred host checks bound the claims.

**Tech Stack:** Markdown, Agent Plugins 1.0.0 JSON, Python 3 stdlib smoke client; jsonschema for maintainer contract tests.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 3
**Estimated Lines of Change:** ~1000 (informational)

**Out of scope:**
- New server, hooks, shell MCP tools, dashboard, orchestration, telemetry, Compute expansion.
- Active-session installation, remote hosting/tunnels, credentials, directory submission, visibility changes, deployment.
- Engine changes, fixes to upstream MCP/reference drift, runtime app acceptance or full desktop activation claims.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Add portable Workflow assistant example | Task 1, Task 2, Task 3 | feat/workflow-agent-plugin |

**Status:** Draft

## Guidance and acceptance mapping

Design: `2026-10-19-workflow-agent-plugin-design.md`; ADR: `decisions/0064-package-workflow-agent-example.md`.
Repo guidance: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`.
All files are additive except a link in `docs/mcp.md`. All package content lives under `example/workflow-agent-plugin/`.

Integration matrix: released wfctl stdio = runtime-integrated (Task 2); portable manifests = config-only (Task 1); Autodev coexistence = config-only (Task 3); host skill activation, app runtime and Compute = deferred (Task 3 explains why). No live app or database needed.

### Task 1: Package the three workflows

**Files:** create `example/workflow-agent-plugin/{plugin.json,mcp.json,README.md,LICENSE,references/workflow-contract.md,skills/workflow-configure/SKILL.md,skills/workflow-diagnose/SKILL.md,skills/workflow-readiness/SKILL.md,tests/test_package.py,tests/requirements.txt,tests/schemas/plugin.schema.json,tests/schemas/mcp.schema.json}`; modify `docs/mcp.md` with package link.

1. Write package contract tests first: official schemas, expected namespace/three skills/metadata, portable local paths, direct command/args/env, safe shared instructions, contained relative links, no hooks/remote endpoints/credentials/private paths. Run and observe missing package failures.
2. Add manifests: name `workflow-assistant`, version `0.1.0`, OpenAI `requires_local_executor: true`; server `workflow-config`, type `stdio`, command `wfctl`, args `mcp -plugin-dir ${PLUGIN_DATA}/engine-plugins`, cwd `${PLUGIN_ROOT}`, env `WFCTL_NO_UPDATE_CHECK=1`.
3. Add focused skills/shared contract: discover available tool schemas + initialize server version; do not invent args; use only discovery/schema/config tools. Source/version evidence; sanitized input/output; no secret reads/installs/updates; untrusted data treatment; explicit unknown/runtime gaps.
4. Add README quickstart/manual wfctl prerequisite, host capability matrix, first prompts, privacy/trust limits, verification steps, source links and distribution limits. Copy MIT license; add docs link.
5. Verify: `python3 -m unittest discover -s example/workflow-agent-plugin/tests -p 'test_package.py' -v` → all PASS using test-local jsonschema. Relative links resolve; official schemas accept manifests. Commit Task 1.

Rollback: revert additive package/docs commit; no installed state changes.

### Task 2: Exercise the real released MCP

**Files:** create `example/workflow-agent-plugin/{fixtures/{minimal,invalid,missing-plugin}.yaml,tests/mcp_smoke.py,tests/test_smoke.py}`.

1. Write negative smoke-client unit tests first: missing executable, unknown/mismatched version, missing tools, protocol/tool errors, no raw exception/config/stderr leakage; do not present mocks as integration proof. Run and observe missing implementation failure.
2. Implement dependency-free maintainer client: launch the manifest's command/args in temp plugin data with a minimal environment, exact executable selected via PATH; initialize, tools/list with bounded timeout; validate inputs against discovered required fields, request IDs/errors; capture only safe summaries and digests. Always stop child processes.
3. Add secret-free loopback HTTP fixture and intentionally invalid/unknown-module fixtures. Run actual tool discovery, module/schema/skeleton, validate, inspect, compat and plugin inventory; schema-gated calls; invalid config must fail validation, malformed YAML must return tool error, missing external type must be unavailable/incompatible. A declaration is not proof of plugin runtime.
4. Verify: `python3 example/workflow-agent-plugin/tests/mcp_smoke.py --wfctl <released-wfctl> --expect-version v0.86.2` → exit 0, 33 tools discovered at researched release, valid fixture accepted, negative fixtures rejected; runtime/host acceptance `not_checked`. Missing executable invocation → exit 1 + safe category, no traceback. `python3 -m unittest discover -s example/workflow-agent-plugin/tests -v` → all PASS. Commit Task 2.

Rollback: revert fixtures/tests; fixture never starts a service and no binary is installed.

### Task 3: Review, audit and publish the draft source

**Files:** create `docs/plans/2026-10-19-workflow-agent-plugin-evidence.md`, review/alignment reports for this plan; update README verification status as needed.

1. Run a fresh independent adversarial code/package review; resolve tangible findings and repeat focused verification. Preserve the same three-task/one-PR scope.
2. Audit all added files for secrets, real user/home paths, internal domains/accounts, private dependencies and hidden executables. Check package extraction into a temp directory with no org repo or secrets, rerun official schemas and released MCP. Compare plugin/server/skill namespaces against public Autodev manifests/skills; no installed files changed. Distinguish static coexistence proof from runtime host activation.
3. Record actual commands/results, release checksum, source revision links, schema versions/digests, validation limitations and remaining desktop acceptance gate. Do not publish raw logs/config contents containing secrets. Package tests audit the public fixture only; real-secret processing is a manual host gate.
4. Verify scope manifest and `git diff --check`; confirm `gh api repos/GoCodeAlone/workflow --jq .private` → `false` immediately before push. Push only feature branch and create exactly one draft PR with body file; attach it to task. No merge or active plugin install. Inspect CI state once; follow pending meaningful checks without expanding to engine repairs.

Expected: isolated clean package test + MCP PASS, independent findings resolved, public branch/draft PR URL; host activation/runtime application acceptance remain explicitly deferred. If publishing blocked, retain local commits and report exact failed action and reason.

Rollback: close draft PR/delete feature branch only when authorized later; no runtime effects to undo.
