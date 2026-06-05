# MCP tool metadata accuracy (generate_github_actions params + scaffold ref-doc) — design

**Date:** 2026-06-04
**Status:** Design — adversarial review PASS (1 cycle, 2026-06-04)
**Repo:** `workflow` (`mcp/wfctl_tools.go` + `docs/mcp-tools-reference.md`)
**Guidance:** `docs/AGENT_GUIDE.md` + `CLAUDE.md` (no `docs/design-guidance.md`). Follow-on to #854.

## 1. Problem

Two MCP-tool-metadata inaccuracies logged as #854 follow-ups:

- **F1 — undeclared params (schema gap).** `handleGenerateGithubActions` (`mcp/wfctl_tools.go:416`) reads `phase_config_yaml` and `wfctl_version` via `mcp.ParseString(req, ...)`, but the `generate_github_actions` tool **def** declares only `yaml_content`/`registry`/`platforms`. MCP clients see the tool's input schema and won't pass undeclared params, so the two-phase (`phase_config_yaml`) and version-pin (`wfctl_version`) capabilities are unreachable via MCP even though the handler honors them. The sibling `ci_plan` tool already declares both.
- **F2 — ref-doc drift.** `docs/mcp-tools-reference.md` documents fictional params for two scaffold tools:
  - `scaffold_environment` — doc says `target` (docker-compose/kubernetes/minikube) + `config`; real def (`mcp/scaffold_tools.go:42-58`): `provider` (req: docker/kubernetes/aws-ecs/gcp-cloudrun/digitalocean), `environments` (array), `secrets_provider`, `exposure`.
  - `scaffold_infra` — doc says `provider` (opentofu/terraform/pulumi) + `config`; real def (`:64-80`): `yaml_content` (req), `provider` (req: aws/gcp/azure/digitalocean), `environment`.

## 2. Goal

The `generate_github_actions` tool schema declares every param its handler reads; `docs/mcp-tools-reference.md` matches the real `scaffold_environment`/`scaffold_infra` defs. **No handler change** (the params are already read); F1 only makes them discoverable. Single PR, no release.

## 3. Non-goals

- No handler / behavior / CIPlan change. F1 adds **optional** param declarations only (backward-compatible — existing clients that omit them get the unchanged `""` default).
- No change to other tool defs or other ref-doc entries (the broader ref-doc param-naming drift, e.g. `config` vs `yaml_content` elsewhere, is out of scope — only the two scaffold entries flagged in #854).
- No new tool.

## 4. Architecture / changes (2 surfaces, 1 PR)

1. **`mcp/wfctl_tools.go`** — in the `generate_github_actions` tool def, add two `mcp.WithString` declarations (after `platforms`, before `WithReadOnlyHintAnnotation`), mirroring the `ci_plan` wording:
   - `mcp.WithString("phase_config_yaml", mcp.Description("Optional YAML content of a prerequisite phase config (creates a 2-phase plan)"))`
   - `mcp.WithString("wfctl_version", mcp.Description("wfctl version to pin in the plan (default: latest)"))`
   Both optional (no `mcp.Required()`) — the handler already defaults them to `""`.

2. **`docs/mcp-tools-reference.md`** — replace the `scaffold_environment` and `scaffold_infra` parameter tables with the real defs:
   - `scaffold_environment`: `provider` (string, yes — docker/kubernetes/aws-ecs/gcp-cloudrun/digitalocean), `environments` (array, no — default `['local','staging','production']`), `secrets_provider` (string, no — env/aws-secrets-manager/gcp-secret-manager/vault, default env), `exposure` (string, no — exposure method **for local**: tailscale/cloudflare/port-forward, default port-forward; only applied to the `local` environment).
   - `scaffold_infra`: `yaml_content` (string, yes), `provider` (string, yes — aws/gcp/azure/digitalocean), `environment` (string, no — default production).
   Also refresh the one-line purpose of each to match the def description (environments section / infra section).

## 5. Testing

- **F1 (schema):** extend the existing `mcp/cigen_info_test.go` (or add a focused test) asserting the registered `generate_github_actions` tool's input schema declares `phase_config_yaml` and `wfctl_version`. Accessor: `srv.MCPServer().ListTools()["generate_github_actions"].Tool.InputSchema.Properties` is a `map[string]any` keyed by param name — assert both keys present. Run failing-first.
- **Runtime / multi-component:** launch the real `wfctl mcp` server; `tools/list` JSON-RPC response for `generate_github_actions` includes `phase_config_yaml`/`wfctl_version` in its `inputSchema.properties` (the client-visible schema). Capture transcript.
- **F2 (markdown):** the two tables render; param names match the def. No Go test (doc-only).
- `GOWORK=off go test ./mcp/...`, `GOWORK=off go build ./cmd/wfctl`, `GOWORK=off golangci-lint run --new-from-rev=origin/main ./mcp/...`.

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md` + `CLAUDE.md`.

| guidance | response |
|---|---|
| Update docs with behavior/CLI/config changes (CLAUDE.md) | F2 is the doc-accuracy fix; F1 updates the tool schema (the MCP "API") |
| Use `GOWORK=off` for Go commands | All verification uses `GOWORK=off` |
| Keep examples under `example/` | N/A |

## Security Review

None. F1 declares two optional string params already read by the handler (no new input surface — they were already parseable; this only advertises them). No auth/secrets/PII/trust-boundary change. F2 is markdown.

## Infrastructure Impact

None. Static tool-schema metadata + markdown. No release, no resources, no migration.

## Multi-Component Validation

Real boundary = MCP server ↔ client. Proof: launched `wfctl mcp`; the `tools/list` response for `generate_github_actions` carries `phase_config_yaml`/`wfctl_version` in `inputSchema.properties` (the schema a client actually receives). No mock.

## Assumptions

1. `handleGenerateGithubActions` reads `phase_config_yaml` + `wfctl_version` and passes them to `mcpAnalyzeFromYAML` → `cigen.Analyze` (two-phase + version pin). *(Verified: `mcp/wfctl_tools.go:416`.)*
2. Declaring an optional param in mcp-go is backward-compatible (clients omitting it are unaffected; the handler's `ParseString(..., "")` default is unchanged). *(mcp-go optional-param semantics; `ci_plan` already does this.)*
3. The real `scaffold_environment`/`scaffold_infra` param lists are as read from `mcp/scaffold_tools.go:42-80`. *(Verified.)*
4. `ListTools()["x"].Tool.InputSchema.Properties` exposes declared param names for the test. *(mcp.Tool.InputSchema.Properties; confirm exact type in impl.)*

## Rollback

Not a runtime-affecting change class (no build/deploy/version-pin/startup/migration/plugin-loading change). Rollback = revert the PR. No release, no state.

## Self-challenge — top doubts

1. **Does declaring `phase_config_yaml` change `generate_github_actions` behavior?** No — the handler already reads + honors it; declaring only makes it client-discoverable (and it genuinely produces a two-phase plan via `cigen.Analyze`).
2. **Should F2 match the handler or the def?** The **def** is the client contract (what `tools/list` advertises); the ref doc documents the client-facing params, so it matches the def. (Handlers reading those params is assumed; not re-audited here.)
3. **InputSchema accessor shape.** The test depends on `Tool.InputSchema.Properties` being a map of param names — confirm the exact field/type at implementation; fall back to asserting the raw marshaled schema contains the param names if the typed accessor differs.
