# MCP tool metadata accuracy Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Declare the two params `generate_github_actions`'s handler already reads (`phase_config_yaml`, `wfctl_version`) on its tool schema, and correct the drifted `scaffold_environment`/`scaffold_infra` entries in `docs/mcp-tools-reference.md`.

**Architecture:** Add two `mcp.WithString` (optional) declarations to one tool def; rewrite two markdown param tables to match the real defs. No handler/behavior change. A `Contains`/schema-properties test locks the new declarations.

**Tech Stack:** Go (`mcp` package), markdown.

**Base branch:** main (worktree branch `feat/mcp-tool-metadata-accuracy`)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 4
**Estimated Lines of Change:** ~50 (informational; not enforced)

**Out of scope:**
- No handler / behavior / CIPlan change (the two params are already read by the handler).
- No edit to other tool defs or other `docs/mcp-tools-reference.md` entries (the same `config`-vs-`yaml_content` drift exists for `api_extract`/`detect_project_features`/etc. — left as a separate concern; only the two #854-flagged scaffold entries are fixed here).
- No new tool, no release.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | fix(mcp): declare generate_github_actions phase params + correct scaffold ref-doc | Task 1, Task 2, Task 3, Task 4 | feat/mcp-tool-metadata-accuracy |

**Status:** Locked 2026-06-05T02:44:05Z

---

### Task 1: Failing schema-declaration test

**Change class:** Internal logic → unit test.

**Files:**
- Modify: `mcp/cigen_info_test.go` (append a test — same `package mcp`)

**Step 1: Write the failing test.** Append to `mcp/cigen_info_test.go`:

```go
// TestGenerateGithubActionsDeclaresAllHandlerParams locks the tool's input
// schema to the params its handler reads (handleGenerateGithubActions reads
// yaml_content, registry, platforms, phase_config_yaml, wfctl_version).
func TestGenerateGithubActionsDeclaresAllHandlerParams(t *testing.T) {
	srv := NewServer("")
	tools := srv.MCPServer().ListTools()
	gha, ok := tools["generate_github_actions"]
	if !ok {
		t.Fatal("generate_github_actions tool not registered")
	}
	props := gha.Tool.InputSchema.Properties
	for _, p := range []string{"yaml_content", "registry", "platforms", "phase_config_yaml", "wfctl_version"} {
		if _, ok := props[p]; !ok {
			t.Errorf("generate_github_actions input schema missing declared param %q", p)
		}
	}
}
```

**Step 2: Run to verify it fails.**

Run: `GOWORK=off go test ./mcp/ -run TestGenerateGithubActionsDeclaresAllHandlerParams -v`
Expected: FAIL — `missing declared param "phase_config_yaml"` and `"wfctl_version"` (not yet declared).

> If `gha.Tool.InputSchema.Properties` is not `map[string]any` in this mcp-go version (the design verified it is), fall back to marshaling `gha.Tool.InputSchema` to JSON and asserting the param-name substrings. Decide by compiling; the typed accessor is expected to work.

**Step 3: Commit**

```bash
git add mcp/cigen_info_test.go
git commit -m "test(mcp): assert generate_github_actions declares all handler params (failing)"
```

---

### Task 2: Declare phase_config_yaml + wfctl_version (mcp/wfctl_tools.go)

**Files:**
- Modify: `mcp/wfctl_tools.go` (`generate_github_actions` tool def — the `platforms` → `WithReadOnlyHintAnnotation` region, ~line 144-147)

**Change class:** internal logic (tool schema) → covered by Task 1 test.

**Step 1: Implement.** Find:

```go
			mcp.WithString("platforms",
				mcp.Description("Platforms to build for (default: \"linux/amd64,linux/arm64\")"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGenerateGithubActions,
```

Replace with (insert the two optional params, mirroring the `ci_plan` wording):

```go
			mcp.WithString("platforms",
				mcp.Description("Platforms to build for (default: \"linux/amd64,linux/arm64\")"),
			),
			mcp.WithString("phase_config_yaml",
				mcp.Description("Optional YAML content of a prerequisite phase config (creates a 2-phase plan)"),
			),
			mcp.WithString("wfctl_version",
				mcp.Description("wfctl version to pin in the plan (default: latest)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGenerateGithubActions,
```

**Step 2: Verify.**

Run: `GOWORK=off go test ./mcp/ -run TestGenerateGithubActionsDeclaresAllHandlerParams -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add mcp/wfctl_tools.go
git commit -m "fix(mcp): declare phase_config_yaml + wfctl_version on generate_github_actions"
```

---

### Task 3: Correct scaffold_environment + scaffold_infra ref-doc (docs/mcp-tools-reference.md)

**Files:**
- Modify: `docs/mcp-tools-reference.md` (`scaffold_environment` ~line 238-246, `scaffold_infra` ~line 249-257)

**Change class:** Documentation → table renders; params match the def.

**Step 1: Replace the `scaffold_environment` entry.** Find:

```markdown
#### `scaffold_environment`

Generate environment configuration (Docker Compose, kubernetes manifests).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `target` | string | yes | Target environment (`docker-compose`, `kubernetes`, `minikube`) |
| `config` | string | no | Existing workflow config to analyze |
```

Replace with (real def — `mcp/scaffold_tools.go:42-61`):

```markdown
#### `scaffold_environment`

Generate an `environments:` YAML section (per-environment provider, region, env vars, secrets provider, exposure method) for a workflow config.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `provider` | string | yes | Deployment provider: `docker`, `kubernetes`, `aws-ecs`, `gcp-cloudrun`, `digitalocean` |
| `environments` | array | no | Environment names to generate (default: `['local', 'staging', 'production']`) |
| `secrets_provider` | string | no | Secrets provider: `env`, `aws-secrets-manager`, `gcp-secret-manager`, `vault` (default: `env`) |
| `exposure` | string | no | Exposure method for the `local` environment: `tailscale`, `cloudflare`, `port-forward` (default: `port-forward`) |
```

**Step 2: Replace the `scaffold_infra` entry.** Find:

```markdown
#### `scaffold_infra`

Generate infrastructure-as-code for a workflow application.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `provider` | string | yes | IaC provider (`opentofu`, `terraform`, `pulumi`) |
| `config` | string | no | Existing workflow config |
```

Replace with (real def — `mcp/scaffold_tools.go:63-81`):

```markdown
#### `scaffold_infra`

Generate an `infra:` YAML section by analyzing the config's modules (e.g. `database.postgres` → RDS, `cache.redis` → ElastiCache).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `yaml_content` | string | yes | Workflow YAML config content to analyze for infrastructure needs |
| `provider` | string | yes | Cloud provider for resources: `aws`, `gcp`, `azure`, `digitalocean` |
| `environment` | string | no | Environment name for resource sizing (default: `production`) |
```

**Step 3: Verify** — the two tables render; no stray pipe rows.

Run: `grep -c "^| \`provider\` | string | yes | Deployment provider" docs/mcp-tools-reference.md`
Expected: `1` (the corrected scaffold_environment provider row exists). Eyeball both tables.

**Step 4: Commit**

```bash
git add docs/mcp-tools-reference.md
git commit -m "docs: correct scaffold_environment + scaffold_infra params in MCP tools reference"
```

---

### Task 4: Full gate + runtime tools/list check

**Files:** none (verification).

**Change class:** Go-repo code change + multi-component (MCP schema ↔ client).

**Step 1: Full mcp package + build + lint.**

Run: `GOWORK=off go test ./mcp/...`
Expected: `ok  github.com/GoCodeAlone/workflow/mcp`

Run: `GOWORK=off go build ./cmd/wfctl`
Expected: exit 0.

Run: `GOWORK=off golangci-lint run --new-from-rev=origin/main ./mcp/...`
Expected: exit 0.

**Step 2: Runtime — the live `tools/list` schema advertises the new params.**

```bash
GOWORK=off go build -o /tmp/wfctl-mcp ./cmd/wfctl
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"c","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | /tmp/wfctl-mcp mcp 2>/dev/null \
  | python3 -c "import sys,json
for line in sys.stdin:
    line=line.strip()
    if not line.startswith('{'): continue
    try: m=json.loads(line)
    except Exception: continue
    if m.get('id')==2:
        tools={t['name']:t for t in m['result']['tools']}
        props=tools['generate_github_actions']['inputSchema']['properties']
        print('declares phase_config_yaml:', 'phase_config_yaml' in props)
        print('declares wfctl_version:', 'wfctl_version' in props)"
```
Expected: both print `True` (the client-visible `tools/list` schema now advertises the params). Capture the transcript. If the framing differs, the goal is a real launched-server `tools/list` showing the two params in `generate_github_actions.inputSchema.properties`.

**Rollback:** revert the PR; the tool schema reverts to 3 declared params and the ref doc to its prior (wrong) entries. No release, no state.

---

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md` + `CLAUDE.md`.

| guidance | response |
|---|---|
| Update docs with behavior/CLI/config changes (CLAUDE.md) | F2 is the doc-accuracy fix; F1 updates the MCP tool schema |
| Use `GOWORK=off` | all verification commands use it |

## Verification per change class

- Task 1-2 (tool schema): Task 1 `Contains`/properties test + `go test ./mcp/`.
- Task 3 (markdown): tables render; provider row grep == 1.
- Task 4 (Go-repo + multi-component): full `go test ./mcp/...`, `go build ./cmd/wfctl`, `golangci-lint --new-from-rev`, and a **real launched `wfctl mcp` `tools/list`** showing the two params in the client-visible schema (MCP↔client boundary).

## Rollback

Not a runtime-affecting change class. Rollback = revert the PR. No release, no state.
