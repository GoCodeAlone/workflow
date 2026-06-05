# Surface cigen information in wfctl's MCP output — design

**Date:** 2026-06-04
**Status:** Design (adversarial review pending)
**Repo:** `workflow` (the `mcp/` package + `docs/mcp-tools-reference.md`)
**Guidance:** `docs/AGENT_GUIDE.md` + `CLAUDE.md` (no `docs/design-guidance.md`).

## 1. Problem

The render path is already cigen-backed, but the MCP server's *information* about it is stale/missing, so an AI driving the MCP can't tell the config-derived CI surface exists or how it behaves:

- **Instructions string** (`mcp/server.go`, `WithInstructions`): says generic "CI/CD generation". No `ci_plan`, no cigen, no config-derived framing.
- **`workflow://docs/overview` resource** (`mcp/docs.go`, `docsOverview` const): the "CI/CD & Git Integration" section lists only the legacy `wfctl generate github-actions`. Nothing on `wfctl ci plan` / `wfctl ci generate` or the config-derived nature.
- **`generate_github_actions` tool description** (`mcp/wfctl_tools.go`): "based on analysis" — doesn't name cigen, per-phase secret scoping, the migrations step, smoke, or plan-guard. The tool is a **hybrid**: CI (`files`/`ci_yaml`) is cigen-derived; `cd_yaml`/`release_yaml` are still legacy template (driven by the `registry`/`platforms` inputs). The description hides both facts.
- **`docs/mcp-tools-reference.md`** (human reference for the MCP tools): omits `ci_plan` and `generate_github_actions` entirely.

Ground truth (verified @ `main` 905405954): `handleGenerateGithubActions` routes through `cigen.Analyze` + `cigen.RenderGitHubActions` (`mcp/wfctl_tools.go:416-421`); `cd_yaml`/`release_yaml` come from `mcpGenerateCDWorkflow(features, registry, platforms)` (legacy template). `ci_plan` already exists and is cigen-backed. `docs/WFCTL.md:2140+` already documents `ci generate`/cigen accurately (so the CLI docs are the wording reference — do not diverge).

## 2. Goal

MCP output (runtime Instructions + the `workflow://docs/overview` resource + the two tool descriptions) and `docs/mcp-tools-reference.md` accurately describe the cigen config-derived CI surface and the `generate_github_actions` hybrid. **Informational only:** no new tools, no handler/behavior change, no release.

## 3. Non-goals

- No new MCP tool (e.g. a `ci_generate`/from-plan render tool). The render path already exists (`generate_github_actions`); only the *information* is missing. (Logged as a possible future follow-up, not built here.)
- No change to any handler, the CIPlan shape, or the `registry`/`platforms` inputs.
- No edit to `docs/WFCTL.md` (already accurate) — reuse its wording for consistency.
- No GitLab/Jenkins/CircleCI behavior change.

## 4. Architecture / changes (4 surfaces, 1 PR)

1. **`mcp/server.go` `WithInstructions`** — insert a cigen sentence: the `ci_plan` tool analyzes a config into a config-derived CIPlan (deploy phases, secrets scoped per phase, a `wfctl migrations up` step, a health-check smoke job, a plan-guard); `generate_github_actions` renders it to GitHub Actions YAML; both derive CI from the app config, not fixed templates; mirrors the `wfctl ci plan` / `ci generate` commands. Change the trailing "CI/CD generation" list item to "config-derived CI/CD generation (ci plan / ci generate)".

2. **`mcp/docs.go` `docsOverview`** — replace the single legacy CLI line in "CI/CD & Git Integration" with: a short "config-derived (cigen)" framing paragraph (per-phase secret scoping, `wfctl migrations up` step with `--format json`/`--env` when derivable, smoke job, plugin-install, plan-guard; GHA/GitLab config-derived, Jenkins/CircleCI template-based), the `wfctl ci plan` and `wfctl ci generate` commands (with the real flags from `docs/WFCTL.md`), a note that legacy `wfctl generate github-actions` now renders through cigen, and the MCP equivalents (`ci_plan` returns the CIPlan; `generate_github_actions` renders it). Keep the two `wfctl git …` lines under a "Git Integration" sub-heading.

3. **`mcp/wfctl_tools.go`** —
   - `generate_github_actions` description: render CI via the cigen engine — config-derived (`files`/`ci_yaml`): per-phase scoped secrets, a `wfctl migrations up` step when `ci.migrations` is set, a smoke job, plugin-install steps, a plan-guard that fails on replace/destroy; also returns the `plan` (CIPlan) and **legacy template-based** `cd_yaml`/`release_yaml` (these use the `registry`/`platforms` inputs); use `ci_plan` to inspect/edit the plan first, or pass `phase_config_yaml` for a two-phase pipeline.
   - `ci_plan` description: minor — note secrets are scoped per phase (not just a union) and that `generate_github_actions` renders the plan in-process.

4. **`docs/mcp-tools-reference.md`** — add a `ci_plan` entry and a `generate_github_actions` entry mirroring the file's existing per-tool format (heading + one-line purpose + an inputs table). Inputs from the tool defs: `ci_plan` (`yaml_content` req, `phase_config_yaml`, `wfctl_version`); `generate_github_actions` (`yaml_content` req, `registry`, `platforms`).

## 5. Testing

- `GOWORK=off go build ./...` + `GOWORK=off go test ./mcp/...` — the `docsOverview` const is compile-time; tool descriptions are plain strings. Confirms nothing that enumerates tools/asserts metadata breaks (no test currently pins these strings — verified).
- **Runtime / multi-component:** build the MCP binary and start the server (`wfctl mcp`), then assert the rendered Instructions and the `workflow://docs/overview` resource contain "cigen"/"ci plan" and that the `generate_github_actions` tool description names cigen. (Add a focused `mcp` package test that calls `NewServer(...)` and asserts the Instructions string + the `docsOverview` const + the two tool descriptions contain the cigen keywords — exercises the real server, not a mock.)
- `GOWORK=off golangci-lint run --new-from-rev=origin/main ./mcp/...`.
- Markdown: `docs/mcp-tools-reference.md` renders (no broken table).

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md` + `CLAUDE.md`.

| guidance | response |
|---|---|
| Update docs with behavior/CLI/config changes (CLAUDE.md) | This *is* a doc/metadata accuracy fix; updates the MCP runtime docs + `docs/mcp-tools-reference.md` |
| CI-gen changes touch `cigen/`, `cmd/wfctl/ci*.go`, `docs/WFCTL.md`, ci-generator (AGENT_GUIDE:48) | No CI-gen *behavior* change; only MCP-side information. `docs/WFCTL.md` already accurate (reused, not edited). No plugin impact (no contract change). |
| Keep committed examples under `example/` | N/A (no examples added) |

## Security Review

None. Static descriptive strings + a markdown doc. No auth/authz, no secrets (secret *names* already surfaced by cigen output; this only describes that behavior), no PII, no new trust boundary, no new input handling.

## Infrastructure Impact

None. No cloud resources, no migrations, no network exposure, no release. Compile-time string/const changes + one markdown file.

## Multi-Component Validation

Real boundary = MCP server ↔ AI client. Proof: construct the real `mcp.NewServer(...)` and assert the Instructions + `workflow://docs/overview` resource + tool descriptions carry the cigen keywords (the server's actual output an MCP client would receive). No mock.

## Assumptions

1. `generate_github_actions` CI output is cigen-derived; `cd_yaml`/`release_yaml` are legacy template using `registry`/`platforms`. *(Verified: `mcp/wfctl_tools.go:416-453`.)*
2. No test pins the Instructions / tool-description / `docsOverview` strings verbatim. *(Verified by grep; the new assertion test will use `Contains`, not equality.)*
3. `docs/WFCTL.md` already documents `ci plan`/`ci generate`/cigen accurately, so MCP wording can mirror it. *(Verified: `docs/WFCTL.md:178,2140+`.)*
4. `docs/mcp-tools-reference.md` has a stable per-tool format (heading + purpose + inputs table) to mirror. *(Verified: e.g. `template_validate_config` entry.)*

## Rollback

Not a runtime-affecting change class (no build/deploy/version-pin/startup/migration/plugin-loading change). Rollback = revert the PR; the MCP reverts to the prior (generic) strings. No release, no state, no migration.

## Self-challenge — top doubts

1. **Hybrid detail in a tool description may be too much.** Mitigation: keep it to one clause ("CI via cigen; `cd_yaml`/`release_yaml` are legacy template") — accurate without a paragraph.
2. **Wording could diverge from `docs/WFCTL.md`.** Mitigation: reuse WFCTL.md's cigen phrasing; don't re-invent.
3. **Ref-doc format drift.** Mitigation: copy the existing entry shape in `docs/mcp-tools-reference.md` exactly.
