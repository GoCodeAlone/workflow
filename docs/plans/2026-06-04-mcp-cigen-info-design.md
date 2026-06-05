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
- **`docs/mcp-tools-reference.md`** (human reference for the MCP tools): omits `ci_plan` and `generate_github_actions` entirely. Its existing `scaffold_ci` entry is **stale** (lists a `provider` param incl `circleci`; the real tool takes `description` req + `yaml_content` opt — `mcp/scaffold_tools.go:20-28`).
- **`workflow://docs/setup-guide` resource** (`mcp/setup_guide.go`, "CI/CD Setup Flow" ~line 61): routes the AI through `scaffold_ci` → `generate_bootstrap` → `wfctl ci run` and **never mentions cigen / `ci plan` / `ci generate`**, so the server's own AI-onboarding decision tree steers *away* from the config-derived path. (These are genuinely distinct: `scaffold_ci` emits a `ci:` *section* in the app config and `generate_bootstrap` emits a thin file that calls `wfctl ci run`; cigen *renders* a full platform-native workflow from the config. Both are valid — the gap is that the relationship is never stated.)

Ground truth (verified @ `main` 905405954): `handleGenerateGithubActions` routes through `cigen.Analyze` + `cigen.RenderGitHubActions` (`mcp/wfctl_tools.go:416-421`); `cd_yaml`/`release_yaml` come from `mcpGenerateCDWorkflow(features, registry, platforms)` (legacy template — this is the ONLY hybrid). `ci_plan` already exists and is cigen-backed. **All four platforms (`github_actions`, `gitlab_ci`, `jenkins`, `circleci`) are config-derived from the same CIPlan** (`docs/WFCTL.md:2142`; RenderJenkins/RenderCircleCI mirror GHA per workflow#810 / ADR 0044) — there is NO "GHA-only is smart, Jenkins/CircleCI are templates" split. `docs/WFCTL.md:2140+` documents `ci generate`/cigen accurately (CLI docs are the wording reference — do not diverge).

## 2. Goal

MCP output (runtime Instructions + the `workflow://docs/overview` resource + the two tool descriptions) and `docs/mcp-tools-reference.md` accurately describe the cigen config-derived CI surface and the `generate_github_actions` hybrid. **Informational only:** no new tools, no handler/behavior change, no release.

## 3. Non-goals

- No new MCP tool (e.g. a `ci_generate`/from-plan render tool). The render path already exists (`generate_github_actions`); only the *information* is missing. (Logged as a possible future follow-up, not built here.)
- No change to any handler, the CIPlan shape, or the `registry`/`platforms` inputs.
- No edit to `docs/WFCTL.md` (already accurate) — reuse its wording for consistency.
- No GitLab/Jenkins/CircleCI behavior change.

## 4. Architecture / changes (5 surfaces, 1 PR)

1. **`mcp/server.go` `WithInstructions`** — insert a cigen sentence: the `ci_plan` tool analyzes a config into a config-derived CIPlan (deploy phases, secrets scoped per phase, a `wfctl migrations up` step, a health-check smoke job, a plan-guard); `generate_github_actions` renders it to GitHub Actions YAML; both derive CI from the app config, not fixed templates; mirrors the `wfctl ci plan` / `ci generate` commands. Change the trailing "CI/CD generation" list item to "config-derived CI/CD generation (ci plan / ci generate)".

2. **`mcp/docs.go` `docsOverview`** — replace the single legacy CLI line in "CI/CD & Git Integration" with: a short "config-derived (cigen)" framing paragraph (per-phase secret scoping, `wfctl migrations up` step with `--format json`/`--env` when derivable, smoke job, plugin-install, plan-guard; **all four target platforms — `github_actions`, `gitlab_ci`, `jenkins`, `circleci` — are config-derived from the same CIPlan**, mirroring `docs/WFCTL.md:2142`), the `wfctl ci plan` and `wfctl ci generate` commands (with the real flags from `docs/WFCTL.md`), a note that legacy `wfctl generate github-actions` now renders through cigen, and the MCP equivalents (`ci_plan` returns the CIPlan; `generate_github_actions` renders it). Keep the two `wfctl git …` lines under a "Git Integration" sub-heading.

3. **`mcp/wfctl_tools.go`** —
   - `generate_github_actions` description: render CI via the cigen engine — config-derived (`files`/`ci_yaml`): per-phase scoped secrets, a `wfctl migrations up` step when `ci.migrations` is set, a smoke job, plugin-install steps, a plan-guard that fails on replace/destroy; also returns the `plan` (CIPlan) and **legacy template-based** `cd_yaml`/`release_yaml` (these use the `registry`/`platforms` inputs); use `ci_plan` to inspect/edit the plan first, or pass `phase_config_yaml` for a two-phase pipeline.
   - `ci_plan` description: minor — note secrets are scoped per phase (not just a union) and that `generate_github_actions` renders the plan in-process.

4. **`docs/mcp-tools-reference.md`** — add a `ci_plan` entry and a `generate_github_actions` entry mirroring the file's existing per-tool format (heading + one-line purpose + an inputs table). Use the **exact** param names from the tool defs (NOT the doc's drifted `config`/`provider` naming): `ci_plan` — `yaml_content` (string, required), `phase_config_yaml` (string, optional), `wfctl_version` (string, optional); `generate_github_actions` — `yaml_content` (string, required), `registry` (string, optional, default `ghcr.io`), `platforms` (string, optional). **Also correct the adjacent stale `scaffold_ci` entry** (CI-adjacent, near-zero cost): the real tool (`mcp/scaffold_tools.go:20-28`) takes `description` (string, required) + `yaml_content` (string, optional) and emits a `ci:` section — drop the fictional `provider`/`circleci`/`config` params. **Out of scope (logged follow-up):** the `scaffold_environment` / `scaffold_infra` ref entries also drift from their tool defs but are not CI-related; leave them.

5. **`mcp/setup_guide.go` "CI/CD Setup Flow"** (the `workflow://docs/setup-guide` resource) — resolve the competing-CI-story gap (I1). After the existing `scaffold_ci` → `generate_bootstrap` → `wfctl ci run` steps, add a short note distinguishing the two valid paths: (a) **thin bootstrap** — `scaffold_ci` emits a `ci:` config section and `generate_bootstrap` emits a minimal file that calls `wfctl ci run` (the engine runs the steps); (b) **config-derived platform-native workflow** — `ci_plan` + `generate_github_actions` (or `wfctl ci plan` / `ci generate`) render a full GitHub Actions / GitLab CI workflow from the config (scoped secrets, migrations, smoke, plan-guard). Pick (b) when you want a native CI YAML committed to the repo. Do not delete the existing flow — add the cigen alternative + the one-line "when to use which".

## 5. Testing

- `GOWORK=off go build ./...` + `GOWORK=off go test ./mcp/...` — the `docsOverview` const is compile-time; tool descriptions are plain strings. Confirms nothing that enumerates tools/asserts metadata breaks (no test currently pins these strings — verified).
- **Runtime / multi-component:** build the MCP binary and start the server (`wfctl mcp`), then assert the rendered Instructions, the `workflow://docs/overview` resource, and the `workflow://docs/setup-guide` resource contain "cigen"/"ci plan"/"ci generate" and that the `generate_github_actions` tool description names cigen. (Add a focused `mcp` package test — `NewServer("")` is dep-free and the proven pattern in `server_test.go` — asserting via `Contains`: the Instructions string, the `docsOverview` const, the `setupGuide` const, and the two tool descriptions carry the cigen keywords; and that `docsOverview` does NOT claim Jenkins/CircleCI are template-based.)
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

1. `generate_github_actions` CI output is cigen-derived; `cd_yaml`/`release_yaml` are legacy template using `registry`/`platforms` — this is the ONLY hybrid. *(Verified: `mcp/wfctl_tools.go:416-453`.)*
2. All four CI platforms (`github_actions`, `gitlab_ci`, `jenkins`, `circleci`) are config-derived from the same CIPlan — no per-platform "smart vs template" split. *(Verified: `docs/WFCTL.md:2142`; RenderJenkins/RenderCircleCI per workflow#810/ADR 0044.)*
3. No test pins the Instructions / tool-description / `docsOverview` / `setupGuide` strings verbatim. *(Verified by grep; the new assertion test uses `Contains`, not equality. `NewServer("")` is dep-free — proven in `server_test.go`.)*
4. `docs/WFCTL.md` already documents `ci plan`/`ci generate`/cigen accurately, so MCP wording can mirror it. *(Verified: `docs/WFCTL.md:178,2140+`.)*
5. `docs/mcp-tools-reference.md` per-tool format is NOT uniform (some entries lack tables; param naming drifts `config` vs `yaml_content`; the `scaffold_ci` entry is stale). The plan mirrors the *table* entries and uses exact tool-def param names, and corrects the stale `scaffold_ci` entry. *(Verified: `scaffold_ci` ref entry `:201-208` vs tool `scaffold_tools.go:20-28`.)*

## Rollback

Not a runtime-affecting change class (no build/deploy/version-pin/startup/migration/plugin-loading change). Rollback = revert the PR; the MCP reverts to the prior (generic) strings. No release, no state, no migration.

## Self-challenge — top doubts

1. **Hybrid detail in a tool description may be too much.** Mitigation: keep it to one clause ("CI via cigen; `cd_yaml`/`release_yaml` are legacy template") — accurate without a paragraph.
2. **Wording could diverge from `docs/WFCTL.md`.** Mitigation: reuse WFCTL.md's cigen phrasing; don't re-invent.
3. **Ref-doc format drift.** Mitigation: copy the existing entry shape in `docs/mcp-tools-reference.md` exactly.
