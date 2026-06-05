# MCP cigen information surfacing Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Make wfctl's MCP output (server Instructions, the `workflow://docs/overview` + `workflow://docs/setup-guide` resources, the `ci_plan`/`generate_github_actions` tool descriptions) and `docs/mcp-tools-reference.md` accurately describe the config-derived cigen CI generation.

**Architecture:** Edit static descriptive strings/consts in the `mcp` package + one markdown reference doc. The render path is already cigen-backed; this is informational only — no new tools, no handler/behavior change, no release. A focused `mcp`-package test (`Contains` assertions) locks the cigen keywords in.

**Tech Stack:** Go (`mcp` package strings/consts), markdown (`docs/mcp-tools-reference.md`).

**Base branch:** main (worktree branch `feat/mcp-cigen-info`)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 7
**Estimated Lines of Change:** ~120 (informational; not enforced)

**Out of scope:**
- No new MCP tool (e.g. a `ci_generate`/from-plan render tool). Render already exists via `generate_github_actions`.
- No handler / CIPlan / behavior change; `registry`/`platforms` inputs unchanged.
- No edit to `docs/WFCTL.md` (already accurate; reused as the wording reference).
- The `generate_github_actions` handler reads `phase_config_yaml`/`wfctl_version` but its tool def does NOT declare them (latent schema gap) — NOT fixed here (would be a schema change); logged as a follow-up. The description therefore does NOT advertise `phase_config_yaml`.
- `scaffold_environment` / `scaffold_infra` ref-doc entries also drift from their defs but are not CI-related — left as a logged follow-up.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | docs(mcp): surface cigen config-derived CI generation in MCP output | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7 | feat/mcp-cigen-info |

**Status:** Complete 2026-06-05T01:47:12Z

---

### Task 1: Failing assertion test (mcp package)

**Change class:** Internal logic / docs → unit test (`Contains`).

**Files:**
- Create: `mcp/cigen_info_test.go` (NEW — `package mcp` internal so it can read the unexported `docsOverview`/`setupGuideContent` consts and call `NewServer`)

**Step 1: Write the failing test**

Create `mcp/cigen_info_test.go`:

```go
package mcp

import (
	"strings"
	"testing"
)

// TestMCPOutputSurfacesCigen locks the cigen config-derived CI information into
// the MCP output an AI client receives: the server Instructions, the
// workflow://docs/overview + workflow://docs/setup-guide resource bodies, and
// the ci_plan / generate_github_actions tool descriptions.
func TestMCPOutputSurfacesCigen(t *testing.T) {
	// docsOverview resource: names the cigen CLI surface.
	for _, want := range []string{"cigen", "wfctl ci plan", "wfctl ci generate", "config-derived"} {
		if !strings.Contains(docsOverview, want) {
			t.Errorf("docsOverview missing %q", want)
		}
	}
	// Negative: must NOT claim Jenkins/CircleCI are template-based (all four are config-derived).
	if strings.Contains(docsOverview, "Jenkins/CircleCI template") ||
		strings.Contains(docsOverview, "Jenkins and CircleCI are template") {
		t.Errorf("docsOverview wrongly claims Jenkins/CircleCI are template-based")
	}

	// setup-guide resource: surfaces the cigen path alongside the scaffold_ci flow.
	for _, want := range []string{"ci_plan", "generate_github_actions", "wfctl ci generate"} {
		if !strings.Contains(setupGuideContent, want) {
			t.Errorf("setupGuideContent missing %q", want)
		}
	}

	// Server Instructions: mention cigen + ci_plan.
	srv := NewServer("")
	tools := srv.MCPServer().ListTools()

	gha, ok := tools["generate_github_actions"]
	if !ok {
		t.Fatal("generate_github_actions tool not registered")
	}
	for _, want := range []string{"cigen", "ci_plan"} {
		if !strings.Contains(gha.Tool.Description, want) {
			t.Errorf("generate_github_actions description missing %q", want)
		}
	}
	ciPlan, ok := tools["ci_plan"]
	if !ok {
		t.Fatal("ci_plan tool not registered")
	}
	if !strings.Contains(ciPlan.Tool.Description, "cigen") {
		t.Errorf("ci_plan description missing %q", "cigen")
	}
}
```

> **Note on the Instructions string:** `NewServer` passes the Instructions to `server.NewMCPServer(...)`; the mcp-go API may not re-expose it on the constructed server. If `srv.MCPServer()` exposes no Instructions getter, assert the Instructions content by reading the source-level constant instead — extract the Instructions text into an unexported package const (e.g. `serverInstructions`) in Task 2 and assert `strings.Contains(serverInstructions, "cigen")` here. Decide in Task 2; keep the test asserting whatever Task 2 makes assertable. The tool-description and resource-const assertions above do not depend on this.

**Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./mcp/ -run TestMCPOutputSurfacesCigen -v`
Expected: FAIL — `docsOverview missing "cigen"`, etc. (strings not yet added).

**Step 3-4:** (no implementation in this task — it's the failing test; Tasks 2-5 make it pass.)

**Step 5: Commit**

```bash
git add mcp/cigen_info_test.go
git commit -m "test(mcp): assert MCP output surfaces cigen CI info (failing)"
```

---

### Task 2: Server Instructions (mcp/server.go)

**Files:**
- Modify: `mcp/server.go` (the `server.WithInstructions(...)` call, ~line 104-115)

**Change class:** internal logic (static string) → covered by Task 1 test.

**Step 1: Implement** — replace the two trailing string fragments. Find:

```go
			"Use the registry_search tool to discover available plugins from the workflow-registry. "+
			"Resources provide documentation and example configurations. "+
			"The workflow engine includes a CLI tool called wfctl with commands for project scaffolding, "+
			"config validation, deployment (Docker, Kubernetes, cloud), API spec extraction, "+
			"diff/contract testing, CI/CD generation, plugin management, and Git integration. "+
			"Read the workflow://docs/overview resource for full CLI reference."),
```

Replace with:

```go
			"Use the registry_search tool to discover available plugins from the workflow-registry. "+
			"For CI/CD generation use the config-derived cigen engine (analyze -> CIPlan -> render): the "+
			"ci_plan tool analyzes a workflow config into a platform-neutral CIPlan (deploy phases, "+
			"secrets scoped per phase, a wfctl migrations step, a health-check smoke job, and a plan-guard), "+
			"and generate_github_actions renders it into GitHub Actions YAML - both produce CI derived "+
			"from the app config, NOT fixed templates. Mirrors the wfctl 'ci plan' / 'ci generate' commands. "+
			"Resources provide documentation and example configurations. "+
			"The workflow engine includes a CLI tool called wfctl with commands for project scaffolding, "+
			"config validation, deployment (Docker, Kubernetes, cloud), API spec extraction, "+
			"diff/contract testing, config-derived CI/CD generation (ci plan / ci generate), plugin management, and Git integration. "+
			"Read the workflow://docs/overview resource for full CLI reference."),
```

> If Task 1 needs a source-level constant to assert the Instructions (because mcp-go exposes no getter), first extract the whole instruction string into `const serverInstructions = "..."` above `NewServer` and pass `server.WithInstructions(serverInstructions)`. Otherwise leave inline. Either way the words "cigen" and "ci_plan" must appear.

**Step 2: Verify** — `GOWORK=off go build ./mcp/` (compiles). The Task 1 assertion for the gha/ci_plan descriptions still fails until Task 4; that is expected.

**Step 3: Commit**

```bash
git add mcp/server.go
git commit -m "docs(mcp): name the cigen CI surface in server Instructions"
```

---

### Task 3: docsOverview CI/CD section (mcp/docs.go)

**Files:**
- Modify: `mcp/docs.go` (`docsOverview` const, the "CI/CD & Git Integration" section ~line 165-169)

**Change class:** internal logic (static const) → covered by Task 1 test (incl the negative Jenkins/CircleCI assertion).

**Step 1: Implement** — replace the section. Find:

```go
### CI/CD & Git Integration

- ` + "`wfctl generate github-actions <config.yaml>`" + ` — Generate GitHub Actions CI/CD workflow files from config
- ` + "`wfctl git connect -repo <owner/repo>`" + ` — Link project to a GitHub repository (` + "`-init`" + ` to create repo)
- ` + "`wfctl git push -message <msg>`" + ` — Commit and push to configured repo (` + "`-tag <version>`" + ` to tag release)
`
```

Replace with (mirrors `docs/WFCTL.md:2142` — all four platforms config-derived):

```go
### CI/CD generation (config-derived via the cigen engine)

CI generation is **config-derived**, not template-based: the cigen engine (analyze -> CIPlan ->
render) builds the workflow from your app config. The output reflects the config's required
secrets (scoped per deploy phase - an apply job only gets the secrets that phase references), a
` + "`wfctl migrations up`" + ` step when ` + "`ci.migrations`" + ` is declared (with ` + "`--format json`" + ` and ` + "`--env`" + ` when an
environment is unambiguous), a health-check smoke job, plugin-install steps when the app uses
plugins, and a plan-guard that fails the job on a replace/destroy. All four target platforms
(` + "`github_actions`" + `, ` + "`gitlab_ci`" + `, ` + "`jenkins`" + `, ` + "`circleci`" + `) are config-derived from the same CIPlan.

- ` + "`wfctl ci plan <config.yaml>`" + ` — Analyze the config and emit a platform-neutral CIPlan JSON (` + "`--out -`" + ` for stdout, ` + "`--phase-config <prereq.yaml>`" + ` for a two-phase prereq->deploy plan). Edit it, then render with ` + "`--from-plan`" + `.
- ` + "`wfctl ci generate <config.yaml> --platform github_actions`" + ` — Render CI files from the config (or ` + "`--from-plan <plan.json>`" + `). ` + "`--diff`" + ` previews vs the on-disk file; ` + "`--write`" + ` overwrites; ` + "`--out <dir>`" + ` sets the output directory.
- ` + "`wfctl generate github-actions <config.yaml>`" + ` — Legacy command name; the CI workflow is now rendered through the same cigen engine.

MCP equivalents: the ` + "`ci_plan`" + ` tool returns the CIPlan, and ` + "`generate_github_actions`" + ` renders it to GitHub Actions YAML.

### Git Integration

- ` + "`wfctl git connect -repo <owner/repo>`" + ` — Link project to a GitHub repository (` + "`-init`" + ` to create repo)
- ` + "`wfctl git push -message <msg>`" + ` — Commit and push to configured repo (` + "`-tag <version>`" + ` to tag release)
`
```

**Step 2: Verify** — `GOWORK=off go build ./mcp/`; `GOWORK=off go test ./mcp/ -run TestMCPOutputSurfacesCigen -v` now passes the `docsOverview` assertions (gha/ci_plan/setup-guide still pending Tasks 4-5).

**Step 3: Commit**

```bash
git add mcp/docs.go
git commit -m "docs(mcp): document config-derived cigen CI in docs/overview resource"
```

---

### Task 4: Tool descriptions (mcp/wfctl_tools.go)

**Files:**
- Modify: `mcp/wfctl_tools.go` — `generate_github_actions` description (~line 136-137), `ci_plan` description (~line 155-158)

**Change class:** internal logic (static strings) → covered by Task 1 test.

**Step 1: Implement.**

(a) `generate_github_actions` — find:

```go
			mcp.WithDescription("Generate GitHub Actions CI/CD workflow YAML files based on analysis of a workflow config. "+
				"Detects features (UI, auth, database, plugins, HTTP) and generates appropriate CI and CD workflows."),
```

Replace with:

```go
			mcp.WithDescription("Render CI/CD workflow YAML from a workflow config. The CI workflow (the 'files'/'ci_yaml' result) is config-derived via the cigen engine (analyze -> CIPlan -> render): required secrets scoped per deploy phase, a 'wfctl migrations up' step when ci.migrations is set, a health-check smoke job, plugin-install steps when plugins are used, and a plan-guard that fails on a replace/destroy - not a fixed template. Also returns the 'plan' (CIPlan) and legacy template-based 'cd_yaml'/'release_yaml' (these use the registry/platforms inputs). Use the ci_plan tool to inspect or edit the plan before rendering."),
```

(b) `ci_plan` — find:

```go
			mcp.WithDescription("Analyze a workflow YAML config and emit a platform-neutral CIPlan JSON. "+
				"The plan describes project name, wfctl version, deploy phases, secrets union, "+
				"migrations spec, smoke test URL, plan guard, and warnings. "+
				"Pass the returned JSON to 'wfctl ci generate --from-plan' to render CI files."),
```

Replace with:

```go
			mcp.WithDescription("Analyze a workflow YAML config and emit a platform-neutral CIPlan JSON using the config-derived cigen engine. "+
				"The plan describes project name, wfctl version, deploy phases (with secrets scoped per phase), the secrets union, "+
				"migrations spec, smoke test URL, plan guard, and warnings. "+
				"Render it with the generate_github_actions tool, or pass the JSON to 'wfctl ci generate --from-plan' to write CI files."),
```

**Step 2: Verify** — `GOWORK=off go test ./mcp/ -run TestMCPOutputSurfacesCigen -v` now passes the gha + ci_plan description assertions.

**Step 3: Commit**

```bash
git add mcp/wfctl_tools.go
git commit -m "docs(mcp): name cigen + hybrid CD in ci tool descriptions"
```

---

### Task 5: setup-guide CI/CD Setup Flow (mcp/setup_guide.go)

**Files:**
- Modify: `mcp/setup_guide.go` (`setupGuideContent` const, "CI/CD Setup Flow" section ~line 61-72)

**Change class:** internal logic (static const) → covered by Task 1 test.

**Step 1: Implement** — find the "Validate" step that closes the flow:

```go
4. **Validate** — use ` + "`validate_config`" + ` to check the final config

---
```

Replace with (add the two-paths note; keep the existing flow):

```go
4. **Validate** — use ` + "`validate_config`" + ` to check the final config

**Two CI paths:**
- *Thin bootstrap* (the steps above): ` + "`scaffold_ci`" + ` writes a ` + "`ci:`" + ` section and ` + "`generate_bootstrap`" + ` emits a minimal file that calls ` + "`wfctl ci run`" + ` (the engine runs the steps).
- *Config-derived platform-native workflow*: use ` + "`ci_plan`" + ` + ` + "`generate_github_actions`" + ` (or ` + "`wfctl ci plan`" + ` / ` + "`wfctl ci generate`" + `) to render a full GitHub Actions / GitLab CI workflow from the config - scoped secrets, migrations, smoke job, plan-guard. Pick this when you want a native CI YAML committed to the repo.

---
```

**Step 2: Verify** — `GOWORK=off go test ./mcp/ -run TestMCPOutputSurfacesCigen -v` now PASSES fully (all surfaces present).

**Step 3: Commit**

```bash
git add mcp/setup_guide.go
git commit -m "docs(mcp): add cigen path to setup-guide CI/CD flow"
```

---

### Task 6: MCP tools reference doc (docs/mcp-tools-reference.md)

**Files:**
- Modify: `docs/mcp-tools-reference.md` — add `ci_plan` + `generate_github_actions` entries; correct the stale `scaffold_ci` entry (~line 201-208)

**Change class:** Documentation → render/no-broken-table check.

**Step 1: Correct the stale `scaffold_ci` entry.** Find:

```markdown
#### `scaffold_ci`

Generate CI/CD pipeline config for a workflow application.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `provider` | string | yes | CI provider (`github-actions`, `gitlab-ci`, `circleci`) |
| `config` | string | no | Existing workflow config to analyze |
```

Replace with (real params from `mcp/scaffold_tools.go:20-37`):

```markdown
#### `scaffold_ci`

Generate a `ci:` YAML section (build / test / deploy sub-sections) for a workflow config, tailored to the app type.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `description` | string | yes | Short description of the application (e.g. 'Go API server with Postgres') |
| `yaml_content` | string | no | Existing workflow config to analyze (used to detect app type) |
| `binary_path` | string | no | Go binary entrypoint path (default: `./cmd/server`) |
| `environments` | array | no | Deployment environment names to include (e.g. `['staging', 'production']`) |
```

**Step 2: Add the two CI tool entries.** Immediately after the corrected `scaffold_ci` entry (and its `---` separator), insert:

```markdown
#### `ci_plan`

Analyze a workflow config and emit a platform-neutral `CIPlan` JSON via the config-derived cigen engine (deploy phases with per-phase scoped secrets, migrations spec, smoke test, plan-guard, warnings). Render it with `generate_github_actions` or `wfctl ci generate --from-plan`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `yaml_content` | string | yes | The YAML content of the workflow configuration |
| `phase_config_yaml` | string | no | YAML of a prerequisite phase config (creates a two-phase plan) |
| `wfctl_version` | string | no | wfctl version to pin in the plan (default: latest) |

---

#### `generate_github_actions`

Render CI/CD workflow YAML from a workflow config. The CI workflow (`files`/`ci_yaml`) is config-derived via the cigen engine (scoped secrets, migrations step, smoke job, plan-guard); also returns the `plan` (CIPlan) and legacy template-based `cd_yaml`/`release_yaml` (which use the `registry`/`platforms` inputs).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `yaml_content` | string | yes | The YAML content of the workflow configuration |
| `registry` | string | no | Container registry for the legacy CD/release output (default: `ghcr.io`) |
| `platforms` | string | no | Build platforms for the legacy CD/release output (default: `linux/amd64,linux/arm64`) |

---
```

(Exact insertion point is flexible — keep the three entries adjacent in the scaffold/CI area of the doc. Do not duplicate an existing heading.)

**Step 2 (verify): no broken table / anchors.**

Run: `GOWORK=off go test ./tests/... -run NoMachinePaths 2>/dev/null; grep -c "^#### \`ci_plan\`" docs/mcp-tools-reference.md`
Expected: the `ci_plan` heading count is `1` (added once); the file still parses (no stray pipe rows). Eyeball the three tables render.

**Step 3: Commit**

```bash
git add docs/mcp-tools-reference.md
git commit -m "docs: add ci_plan + generate_github_actions to MCP tools reference; fix stale scaffold_ci"
```

---

### Task 7: Full gate + runtime MCP launch check

**Files:** none (verification task).

**Change class:** Go-repo code change + multi-component (MCP server ↔ client boundary).

**Step 1: Full mcp package + build.**

Run: `GOWORK=off go test ./mcp/...`
Expected: `ok  github.com/GoCodeAlone/workflow/mcp` (incl `TestMCPOutputSurfacesCigen`).

Run: `GOWORK=off go build ./cmd/wfctl`
Expected: exit 0.

**Step 2: Lint changed lines.**

Run: `GOWORK=off golangci-lint run --new-from-rev=origin/main ./mcp/...`
Expected: exit 0.

**Step 3: Runtime multi-component check — launch the real MCP server and confirm the output carries cigen.** The MCP server speaks JSON-RPC over stdio; the `initialize` response returns the Instructions, and `resources/read` returns the overview body. Drive it with a here-doc:

```bash
GOWORK=off go build -o /tmp/wfctl-mcp ./cmd/wfctl
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"c","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"workflow://docs/overview"}}' \
  | /tmp/wfctl-mcp mcp 2>/dev/null | grep -o "cigen" | head -1
```
Expected: prints `cigen` (the live server's `initialize` Instructions and/or the overview resource contain it). If `wfctl mcp` needs a flag/subcommand variation, confirm with `/tmp/wfctl-mcp mcp --help`; the goal is a real launched-server transcript showing the cigen string in the output an MCP client receives. Capture the transcript.

**Step 4: Commit** (only if a doc tweak was needed; otherwise none).

**Rollback (informational change):** revert the PR; the MCP reverts to the prior generic strings. No release, no state, no migration.

---

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md` + `CLAUDE.md`.

| guidance | response |
|---|---|
| Update docs with behavior/CLI/config changes (CLAUDE.md) | This is the doc/metadata accuracy fix itself; updates MCP runtime docs + `docs/mcp-tools-reference.md` |
| CI-gen changes touch `cigen/`, `cmd/wfctl/ci*.go`, `docs/WFCTL.md`, ci-generator (AGENT_GUIDE:48) | No CI-gen behavior change; MCP-side info only. `docs/WFCTL.md` already accurate (reused, not edited). No plugin/contract impact. |
| Use `GOWORK=off` for Go commands | All verification commands use `GOWORK=off`. |

## Verification per change class

- Tasks 2-5 (static strings/consts): covered by the Task 1 `Contains` test + `go build ./mcp/`.
- Task 6 (markdown): table renders, `ci_plan` heading added once.
- Task 7 (Go-repo + multi-component): full `go test ./mcp/...`, `go build ./cmd/wfctl`, `golangci-lint --new-from-rev`, and a **real launched MCP server** transcript showing the cigen string in the client-facing output (the MCP↔client boundary, not a mock).

## Rollback

Not a runtime-affecting change class (no build/deploy/version-pin/startup/migration/plugin-loading change). Rollback = revert the PR. No release, no state.
