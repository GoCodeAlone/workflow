# 0064. Package a direct Workflow assistant example

**Status:** Accepted
**Date:** 2026-10-19
**Related:** `docs/plans/2026-10-19-workflow-agent-plugin-design.md`

## Context

Users need repeatable config authoring, diagnosis and readiness evidence using public Workflow capabilities. Existing wfctl already exposes stdio MCP. Go engine plugins occupy `plugins/`; full standalone samples belong under `example/`.

## Decision

Place a portable Agent Plugins package under `example/workflow-agent-plugin`, containing three skills and a direct `wfctl mcp` connection. Discover live tools and schemas before use. Keep evidence explicit about validation versus runtime acceptance. Do not bundle binaries, create an orchestration wrapper or depend on private services.

Alternatives: prose-only documentation lacks a loadable workflow package; a new wrapper server duplicates wfctl and adds a maintenance surface. A new repository is unnecessary for this bounded example.

## Consequences

The package can be copied from public GitHub independently of engine source. Users supply wfctl and a local-capable host. Full desktop skill activation remains a separately recorded acceptance gate. The upstream MCP tool surface remains visible and host permissions still apply. Reverting the additive package removes it without service changes.
