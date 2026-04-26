---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: cc22551
  - repo: workflow
    commit: f0faf96
  - repo: workflow
    commit: b50e3f3
  - repo: workflow
    commit: 856a7b9
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - rg -n "ResolveForEnv|resourceName = resolved.Name|resolveInfraOutput" cmd/wfctl config
    - GOWORK=off go test ./interfaces ./config ./platform ./cmd/wfctl -run 'Test(Migration|Tenant|Canonical|BuildHook|PluginCLI|ScaffoldDockerfile|ResolveForEnv|ConfigHash|ApplyInfraModules|Diagnostic|Troubleshoot|ProviderID|ValidateProviderID|PluginInstall|ParseChecksums|Audit|WfctlManifest|WfctlLockfile|PluginLock|PluginAdd|PluginRemove|MigratePlugins|InfraOutputs)' -count=1
  result: pass
supersedes: []
superseded_by: []
---

# wfctl v0.18.9 Phase-Continuation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow v0.18.9 fixing the env-resolution consistency gap in `wfctl ci run --phase deploy` and `infra_output` source resolution, unblocking BMW staging deploy which currently creates duplicate resources due to the base-name vs env-resolved-name mismatch.

**Architecture:** Refactor `resolveModCfg` closure in `cmd/wfctl/deploy_providers.go` to return the full `*config.ResolvedModule` so callers see both `resolved.Name` (env-resolved identity) and `resolved.Config` (env-merged map). Replace `m.Name` with `resolved.Name` at every downstream consumer in the deploy-phase path. Apply the same fix to `infra_output` source-module parsing in `cmd/wfctl/infra_secrets.go`. Add regression tests gating both code paths.

**Tech Stack:** Go 1.26, existing wfctl codebase. No new dependencies.

**Reference design:** `docs/plans/2026-04-24-wfctl-phase-continuation-design.md`.

**Timing:** v0.18.9 HOTFIX — sole workstream until merged. Pauses v0.19.0 Phase 2+ work.

---

## Task 1: Refactor `resolveModCfg` closure signature

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go:709-718` (the closure definition)

**Step 1: Write the failing test**

```go
// cmd/wfctl/deploy_providers_env_test.go (new file)
package main

import (
	"testing"
	"github.com/GoCodeAlone/workflow/config"
)

func TestPluginDeployProvider_UsesEnvResolvedName(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "do-provider",
				Type: "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name: "bmw-app",
				Type: "infra.container_service",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]config.EnvOverride{
					"staging": {
						Config: map[string]any{"name": "bmw-staging"},
					},
				},
			},
		},
	}

	dp, err := newPluginDeployProvider("digitalocean", wfCfg, "staging")
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}

	pdp, ok := dp.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", dp)
	}
	if pdp.resourceName != "bmw-staging" {
		t.Errorf("resourceName = %q, want %q (env-resolved name)", pdp.resourceName, "bmw-staging")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/jon/workspace/workflow
GOWORK=off go test ./cmd/wfctl/ -run TestPluginDeployProvider_UsesEnvResolvedName -v
```
Expected: FAIL with `resourceName = "bmw-app", want "bmw-staging"` (the bug).

**Step 3: Refactor the closure**

Change `cmd/wfctl/deploy_providers.go:709-718` from:
```go
resolveModCfg := func(m *config.ModuleConfig) (map[string]any, bool) {
    if envName == "" {
        return m.Config, true
    }
    resolved, ok := m.ResolveForEnv(envName)
    if !ok {
        return nil, false
    }
    return resolved.Config, true
}
```
To:
```go
resolveModule := func(m *config.ModuleConfig) (*config.ResolvedModule, bool) {
    if envName == "" {
        return &config.ResolvedModule{
            Name:   m.Name,
            Type:   m.Type,
            Config: m.Config,
        }, true
    }
    return m.ResolveForEnv(envName)
}
```

**Step 4: Commit (do NOT run tests yet — will fix call sites in Task 2)**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_env_test.go
git commit -m "refactor(wfctl): resolveModule returns *ResolvedModule for env-resolved Name access"
```

Note: after this commit the code won't compile until Task 2 updates the call sites. Commit the refactor + call-site fixes together in Task 2's final commit.

---

## Task 2: Update three call sites to use resolved.Name + resolved.Config

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go` lines 723-802 (iac.provider lookup + findByType closure + fallback loop)

**Step 1: Update the iac.provider lookup (was lines 723-737)**

```go
// Find the iac.provider module matching the requested provider name.
var providerModName string
var providerModCfg map[string]any
for i := range wfCfg.Modules {
    m := &wfCfg.Modules[i]
    if m.Type != "iac.provider" {
        continue
    }
    resolved, ok := resolveModule(m)
    if !ok {
        continue
    }
    cfgProvider, _ := resolved.Config["provider"].(string)
    if cfgProvider == providerName || resolved.Name == providerName {
        providerModName = resolved.Name
        providerModCfg = resolved.Config
        break
    }
}
```

**Step 2: Update findByType closure (was lines 758-776)**

Change:
```go
findByType := func(target string) bool {
    for i := range wfCfg.Modules {
        m := &wfCfg.Modules[i]
        if m.Type != target {
            continue
        }
        cfg, ok := resolveModCfg(m)    // OLD
        if !ok {
            continue
        }
        if p, _ := cfg["provider"].(string); p == providerModName {
            resourceName = m.Name      // ← BUG
            resourceType = m.Type
            resourceCfg = cfg
            return true
        }
    }
    return false
}
```
To:
```go
findByType := func(target string) bool {
    for i := range wfCfg.Modules {
        m := &wfCfg.Modules[i]
        if m.Type != target {
            continue
        }
        resolved, ok := resolveModule(m)
        if !ok {
            continue
        }
        if p, _ := resolved.Config["provider"].(string); p == providerModName {
            resourceName = resolved.Name   // ← fix: env-resolved
            resourceType = resolved.Type
            resourceCfg = resolved.Config
            return true
        }
    }
    return false
}
```

**Step 3: Update fallback loop (was lines 782-802)**

Change:
```go
for i := range wfCfg.Modules {
    m := &wfCfg.Modules[i]
    if m.Type == "iac.provider" || m.Type == "" {
        continue
    }
    cfg, ok := resolveModCfg(m)    // OLD
    if !ok {
        continue
    }
    if p, _ := cfg["provider"].(string); p == providerModName {
        fmt.Fprintf(os.Stderr, "warning: no deploy-target module ...; falling back to first infra module %q (type %q)\n",
            deployTargetTypes, providerModName, m.Name, m.Type)
        resourceName = m.Name    // ← BUG
        resourceType = m.Type
        resourceCfg = cfg
        break
    }
}
```
To:
```go
for i := range wfCfg.Modules {
    m := &wfCfg.Modules[i]
    if m.Type == "iac.provider" || m.Type == "" {
        continue
    }
    resolved, ok := resolveModule(m)
    if !ok {
        continue
    }
    if p, _ := resolved.Config["provider"].(string); p == providerModName {
        fmt.Fprintf(os.Stderr, "warning: no deploy-target module (%v) found for provider %q; falling back to first infra module %q (type %q)\n",
            deployTargetTypes, providerModName, resolved.Name, resolved.Type)
        resourceName = resolved.Name   // ← fix
        resourceType = resolved.Type
        resourceCfg = resolved.Config
        break
    }
}
```

**Step 4: Run the regression test — should PASS now**

```bash
cd /Users/jon/workspace/workflow
GOWORK=off go test ./cmd/wfctl/ -run TestPluginDeployProvider_UsesEnvResolvedName -v
```
Expected: PASS.

**Step 5: Run full wfctl test suite**

```bash
GOWORK=off go test ./cmd/wfctl/... -v
```
Expected: all tests pass. Any failures indicate an existing test was implicitly depending on `m.Name` (base) behavior — fix by updating those tests to reflect the correct env-resolved semantic.

**Step 6: Commit**

```bash
git add cmd/wfctl/deploy_providers.go cmd/wfctl/deploy_providers_env_test.go
git commit -m "fix(wfctl): ci run deploy uses env-resolved module name (not base)

Regression tested via TestPluginDeployProvider_UsesEnvResolvedName. Same
class of bug as v0.18.7's Task #32 fix for ResourceSpec.Name — env override
of Config[\"name\"] was lifted into ResolvedModule.Name but deploy_providers.go
read m.Name directly, ignoring the override. Caused BMW deploy run
24888583717 to create duplicate DO apps (bmw-app vs bmw-staging)."
```

---

## Task 3: Add fallback test for no-env case

**Files:**
- Modify: `cmd/wfctl/deploy_providers_env_test.go`

**Step 1: Add the test**

```go
func TestPluginDeployProvider_FallsBackToModuleNameWhenNoEnv(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "do-provider",
				Type: "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name:   "bmw-app",
				Type:   "infra.container_service",
				Config: map[string]any{"provider": "do-provider"},
				// NOTE: no Environments block — base name should be used
			},
		},
	}

	dp, err := newPluginDeployProvider("digitalocean", wfCfg, "")
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}

	pdp, ok := dp.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", dp)
	}
	if pdp.resourceName != "bmw-app" {
		t.Errorf("resourceName = %q, want %q (base module name when no env)", pdp.resourceName, "bmw-app")
	}
}
```

**Step 2: Run**

```bash
GOWORK=off go test ./cmd/wfctl/ -run TestPluginDeployProvider_FallsBackToModuleNameWhenNoEnv -v
```
Expected: PASS.

**Step 3: Commit**

```bash
git add cmd/wfctl/deploy_providers_env_test.go
git commit -m "test(wfctl): verify pluginDeployProvider falls back to module name with no env"
```

---

## Task 4: Audit infra_output source module-name resolution (task #56)

**Files:**
- Modify: `cmd/wfctl/infra_secrets.go` (find and fix the source path parser)
- Test: `cmd/wfctl/infra_secrets_env_test.go` (new)

**Step 1: Discover the current parser**

```bash
grep -n 'infra_output\|infra output\|\.uri\b\|source.*\.' cmd/wfctl/infra_secrets.go | head -20
```

Identify the function that parses `bmw-database.uri` (the format is `<module-name>.<output-field>`). Likely named `parseInfraOutputSource` or similar, or inlined in the secret-generation loop.

**Step 2: Write the failing test**

```go
// cmd/wfctl/infra_secrets_env_test.go (new)
package main

import (
	"testing"
	"github.com/GoCodeAlone/workflow/config"
)

func TestInfraOutput_EnvResolvesModuleSource(t *testing.T) {
	// Staging env renames bmw-database → bmw-staging-db. State is keyed by
	// env-resolved name. Secret generation reads source "bmw-database.uri"
	// which must resolve to "bmw-staging-db" for the state lookup to succeed.

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "bmw-database",
				Type: "infra.database",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]config.EnvOverride{
					"staging": {Config: map[string]any{"name": "bmw-staging-db"}},
				},
			},
		},
		Secrets: &config.SecretsConfig{
			Generate: []config.SecretGenerateEntry{
				{
					Key:    "STAGING_DATABASE_URL",
					Type:   "infra_output",
					Source: "bmw-database.uri",
				},
			},
		},
	}

	// Simulate pre-populated state with env-resolved name.
	fakeState := map[string]interfaces.ResourceState{
		"bmw-staging-db": {
			ID:   "bmw-staging-db",
			Name: "bmw-staging-db",
			Outputs: map[string]any{"uri": "postgresql://test"},
		},
	}

	// Function under test: resolve infra_output source with envName.
	// The function must transform "bmw-database.uri" → lookup "bmw-staging-db" → "uri" field.
	val, err := resolveInfraOutput(wfCfg, "bmw-database.uri", "staging", fakeState)
	if err != nil {
		t.Fatalf("resolveInfraOutput: %v", err)
	}
	if val != "postgresql://test" {
		t.Errorf("got %q, want %q (state lookup via env-resolved name)", val, "postgresql://test")
	}
}
```

Adjust function name to match the actual existing function. If the function is tightly coupled to a larger codepath, extract a small helper and test that.

**Step 3: Run to verify failure**

```bash
GOWORK=off go test ./cmd/wfctl/ -run TestInfraOutput_EnvResolvesModuleSource -v
```
Expected: FAIL (function returns "module bmw-database not found in state").

**Step 4: Fix the source parser**

In `infra_secrets.go`, where the `<module>.<output>` string is parsed, after extracting the module name apply env resolution:

```go
// Before (buggy):
moduleName := parts[0]  // e.g., "bmw-database"
outputField := parts[1]
state, ok := stateMap[moduleName]  // FAILS — state has "bmw-staging-db"

// After (fixed):
moduleName := parts[0]
outputField := parts[1]

// Apply env resolution to the module name so state lookup matches
// how infra apply persisted the resource.
if envName != "" {
    for i := range wfCfg.Modules {
        m := &wfCfg.Modules[i]
        if m.Name != moduleName {
            continue
        }
        if resolved, ok := m.ResolveForEnv(envName); ok {
            moduleName = resolved.Name
            break
        }
    }
}
state, ok := stateMap[moduleName]
```

Or, if a helper exists (`config.ResolveModuleName(wfCfg, name, envName) string`), use it. If not, consider introducing it in a later refactor — for v0.18.9 keep the inline version.

**Step 5: Run test — should PASS**

```bash
GOWORK=off go test ./cmd/wfctl/ -run TestInfraOutput_EnvResolvesModuleSource -v
```

**Step 6: Run full wfctl test suite**

```bash
GOWORK=off go test ./cmd/wfctl/... -v
```
Expected: all tests pass.

**Step 7: Commit**

```bash
git add cmd/wfctl/infra_secrets.go cmd/wfctl/infra_secrets_env_test.go
git commit -m "fix(wfctl): infra_output source module name flows through env resolution

Closes task #56. Matches the v0.18.7 fix pattern: state lookup uses
env-resolved module name (bmw-staging-db) not base name (bmw-database)
when --env is set."
```

---

## Task 5: Update CHANGELOG

**Files:**
- Modify: `CHANGELOG.md` — add v0.18.9 entry at the top

**Step 1: Add CHANGELOG entry**

At the top of `CHANGELOG.md`, between the existing title/intro and the v0.18.8 section, insert:

```markdown
## [0.18.9] - 2026-04-24

### Fixed

- **`wfctl ci run --phase deploy` now uses env-resolved module name** — `pluginDeployProvider.resourceName` was populated from `m.Name` (base config name) instead of `ResolvedModule.Name` (env-override lifted from `Config["name"]`). When infra apply had env-renamed a module (e.g. BMW's `bmw-app` → `bmw-staging` for staging), the deploy phase used the base name for `driver.Read` lookup, didn't find the resource, and went down the Create path — producing duplicate DO resources with conflicting names. Same class as v0.18.7 Task #32 fix, but in the ci-run code path. BMW deploy run 24888583717 is the regression case.
- **`infra_output` source resolution now applies env override to module name** — `secrets.generate[].source: "bmw-database.uri"` now resolves to the env-resolved state key (e.g. `bmw-staging-db`) when `--env staging` is set, matching how infra apply persists state. Closes task #56.

### Tests

- `cmd/wfctl/deploy_providers_env_test.go` — `TestPluginDeployProvider_UsesEnvResolvedName`, `TestPluginDeployProvider_FallsBackToModuleNameWhenNoEnv`
- `cmd/wfctl/infra_secrets_env_test.go` — `TestInfraOutput_EnvResolvesModuleSource`
```

**Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG v0.18.9 entry"
```

---

## Task 6: Open PR

**Step 1: Push branch**

```bash
git push -u origin fix/v0.18.9-phase-continuation
```

**Step 2: Open PR on workflow repo**

```bash
gh pr create --repo GoCodeAlone/workflow --title "fix(wfctl): v0.18.9 phase-continuation — env-resolution consistency" --body "$(cat <<'EOF'
## Summary

Closes the class of bugs where wfctl's env-resolution is applied in some
code paths but not others, producing different identities for the same
logical resource.

**Surfaced by:** BMW deploy run 24888583717 — `wfctl infra apply` created
DO app `bmw-staging` (env-resolved), then `wfctl ci run --phase deploy`
immediately created a SECOND DO app `bmw-app` (base name), because the
deploy path at `cmd/wfctl/deploy_providers.go:769` read `m.Name` directly
instead of `ResolvedModule.Name`.

**Fixes in this PR:**

- Refactored `resolveModCfg` closure in deploy_providers.go to return
  `*config.ResolvedModule`; call sites now read `resolved.Name` everywhere.
- Same pattern applied to `infra_secrets.go` `infra_output` source parser
  (task #56).
- 3 regression tests gating both fixes.

## Test plan

- [x] `GOWORK=off go test ./cmd/wfctl/...` passes
- [x] New regression tests fail on main, pass on this branch
- [ ] BMW setup-wfctl bumps to v0.18.9; teardown + redeploy
- [ ] BMW staging /healthz 200 confirmed post-deploy

## Reference

Design: docs/plans/2026-04-24-wfctl-phase-continuation-design.md
Plan: docs/plans/2026-04-24-wfctl-phase-continuation.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

**Step 3: Request Copilot review**

```bash
gh pr edit <pr-number> --repo GoCodeAlone/workflow --add-reviewer copilot-pull-request-reviewer
```

**Step 4: DM team-lead when CI green + Copilot window elapsed for merge decision.**

Standard flow: LOCAL review with code-reviewer → push → Copilot → merge. If reviewer finds issues, iterate.

---

## Task 7 (team-lead): Admin-merge + tag v0.18.9

After PR is green + Copilot cleared:

**Step 1: Admin-merge**

```bash
gh pr merge <pr-number> --repo GoCodeAlone/workflow --admin --squash \
  --subject "fix(wfctl): v0.18.9 phase-continuation — env-resolution consistency (#<pr-number>)"
```

**Step 2: Get the merge commit SHA**

```bash
gh pr view <pr-number> --repo GoCodeAlone/workflow --json mergeCommit --jq '.mergeCommit.oid'
```

**Step 3: Tag v0.18.9**

```bash
gh api repos/GoCodeAlone/workflow/git/refs --method POST \
  -f ref='refs/tags/v0.18.9' \
  -f sha='<merge-commit-sha>'
```

Release workflow fires on tag push, produces setup-wfctl v0.18.9 artifacts.

---

## Task 8 (impl-bmw-2-2): BMW bump to v0.18.9

After v0.18.9 release artifacts publish:

**Files:**
- Modify: `.github/workflows/deploy.yml` — all 3 job sites with `setup-wfctl@v1 with: version: v0.18.8` → `v0.18.9`

**Step 1: Create branch + edit**

```bash
cd /Users/jon/workspace/buymywishlist
git checkout -b chore/bump-setup-wfctl-v0.18.9
# edit deploy.yml — 3 occurrences of v0.18.8 → v0.18.9
```

**Step 2: LOCAL review with code-reviewer, push, open PR**

```bash
git push -u origin chore/bump-setup-wfctl-v0.18.9
gh pr create --repo GoCodeAlone/buymywishlist --title "chore: bump setup-wfctl v0.18.8 → v0.18.9 (phase-continuation fix)"
```

**Step 3: DM team-lead for fast-track merge on CI green.**

---

## Task 9 (team-lead): Teardown + redeploy

After BMW bump PR merges:

**Step 1: Trigger teardown-staging (corrected patterns + state wipe from PRs #178/#179)**

```bash
gh workflow run teardown-staging.yml --repo GoCodeAlone/buymywishlist -f confirm=yes
```

**Step 2: Wait for teardown to complete.**

**Step 3: Deploy auto-fires from CI on main.**

Expected end-to-end:
- infra apply: 4 CREATE actions, all succeed
- secrets.generate: STAGING_DATABASE_URL captured via env-resolved `bmw-staging-db` ✓
- ci run deploy: `driver.Read(bmw-staging)` finds existing app from infra apply → UPDATE path (not CREATE)
- staging /healthz → 200 (Phase F closes; task #25)
- auto-promote to prod → prod /healthz → 200 (Phase G closes; task #26)

**DM team-lead when staging /healthz is 200 confirmed.**

---

## Success criteria

- All regression tests pass
- PR merges, v0.18.9 tag published
- BMW bump PR merges
- Teardown clears duplicate apps + state
- Post-deploy: staging /healthz 200, prod /healthz 200
- No new duplicate resources created by env-resolution mismatch
- Task #60 closes; task #56 closes
- Task #25 (Phase F) closes; task #26 (Phase G) closes
