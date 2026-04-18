# Deploy Pipeline Multi-Env Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Finish multi-env support in `wfctl infra`, document the pipeline pattern, retrofit DND, and migrate BMW to a single `infra.yaml` with staging + prod auto-promotion.

**Architecture:** Four deliverables. D1 (engine) unblocks all downstream work. D1 adds `Environments` to `ModuleConfig`, switches `wfctl infra` to `config.LoadFromFile` (unlocks `imports:`), and adds `--env` to infra commands with resolution logic. D2 is a tutorial that dogfoods the new behavior. D3 is a retrofit of `workflow-dnd` against the tutorial. D4 migrates `buymywishlist` with full DOCR + auto-promotion.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, `config.LoadFromFile` (existing loader with `imports:` support), GitHub Actions, DigitalOcean (DOCR + App Platform), `GoCodeAlone/setup-wfctl@v1`.

**Design doc:** `docs/plans/2026-04-17-deploy-pipeline-multi-env-design.md`

**Gap called out during planning:** `wfctl infra` parses `modules:` using `ModuleConfig` (no Environments field), while `InfraResourceConfig` (which has `Environments`) lives under `infrastructure.resources:` and is never read. Canonical path: add `Environments` to `ModuleConfig`; leave `InfraResourceConfig` alone for now (or wire it as an alternative schema in a follow-up).

---

## Phase 1 — Engine multi-env in `workflow`

Work directory: `/Users/jon/workspace/workflow`

### Task 1: Verify current behavior before touching anything

**Files:**
- Read: `cmd/wfctl/infra.go` (full)
- Read: `cmd/wfctl/infra_bootstrap.go` (full)
- Read: `config/config.go` (types only)
- Read: `config/infra_resolution.go` (full)

**Step 1: Run the full infra test suite to establish baseline**

```
cd /Users/jon/workspace/workflow
go test ./cmd/wfctl/... -run Infra -count=1
go test ./config/... -count=1
```

Expected: all pass. If any fail, stop and investigate before making changes.

**Step 2: Record the baseline** in the plan PR description under "Baseline" — test count + duration.

**Step 3: Commit nothing.** This task is observation only.

### Task 2: Add `Environments` field to `ModuleConfig`

**Files:**
- Modify: `config/config.go:73-79` (add `Environments` field to `ModuleConfig`)

**Step 1: Write the failing test**

Create `config/module_config_env_test.go`:

```go
package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestModuleConfig_UnmarshalEnvironments(t *testing.T) {
	const src = `
name: bmw-database
type: infra.database
config:
  size: db-s-1vcpu-1gb
environments:
  staging:
    config:
      size: db-s-1vcpu-1gb
  prod:
    config:
      size: db-s-2vcpu-4gb
`
	var m ModuleConfig
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(m.Environments); got != 2 {
		t.Fatalf("want 2 environments, got %d", got)
	}
	if m.Environments["prod"].Config["size"] != "db-s-2vcpu-4gb" {
		t.Fatalf("prod size override not applied: %+v", m.Environments["prod"].Config)
	}
}
```

**Step 2: Run test, confirm failure**

```
go test ./config/ -run TestModuleConfig_UnmarshalEnvironments -v
```

Expected: FAIL (`ModuleConfig` has no `Environments` field).

**Step 3: Add the field**

Edit `config/config.go:73-79`:

```go
type ModuleConfig struct {
	Name      string            `json:"name" yaml:"name"`
	Type      string            `json:"type" yaml:"type"`
	Config    map[string]any    `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Branches  map[string]string `json:"branches,omitempty" yaml:"branches,omitempty"`
	Environments map[string]*InfraEnvironmentResolution `json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

**Step 4: Run test, confirm pass**

```
go test ./config/ -run TestModuleConfig_UnmarshalEnvironments -v
```

Expected: PASS.

**Step 5: Run full config tests to catch regressions**

```
go test ./config/... -count=1
```

Expected: all pass.

**Step 6: Commit**

```
git add config/config.go config/module_config_env_test.go
git commit -m "config: add Environments field to ModuleConfig"
```

### Task 3: Add `ResolveForEnv` helper on `ModuleConfig`

**Files:**
- Create: `config/module_resolve_env.go`
- Create: `config/module_resolve_env_test.go`

**Step 1: Write the failing tests**

Create `config/module_resolve_env_test.go`:

```go
package config

import "testing"

func TestResolveForEnv_NoEnvironments_ReturnsTopLevel(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small"},
	}
	resolved, ok := m.ResolveForEnv("staging")
	if !ok {
		t.Fatal("want ok=true when no environments defined")
	}
	if resolved.Config["size"] != "small" {
		t.Fatalf("want size=small, got %v", resolved.Config["size"])
	}
}

func TestResolveForEnv_OverridesMerge(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small", "region": "nyc1"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"size": "large"}},
		},
	}
	resolved, ok := m.ResolveForEnv("prod")
	if !ok {
		t.Fatal("want ok=true")
	}
	if resolved.Config["size"] != "large" {
		t.Fatalf("want size=large, got %v", resolved.Config["size"])
	}
	if resolved.Config["region"] != "nyc1" {
		t.Fatalf("want region=nyc1 preserved, got %v", resolved.Config["region"])
	}
}

func TestResolveForEnv_NilEnvSkipsResource(t *testing.T) {
	m := &ModuleConfig{
		Name: "dns",
		Type: "infra.dns",
		Environments: map[string]*InfraEnvironmentResolution{
			"prod":    {Config: map[string]any{"domain": "example.com"}},
			"staging": nil, // explicit skip
		},
	}
	if _, ok := m.ResolveForEnv("staging"); ok {
		t.Fatal("want ok=false when env explicitly nil")
	}
	if _, ok := m.ResolveForEnv("prod"); !ok {
		t.Fatal("want ok=true for prod")
	}
}

func TestResolveForEnv_EnvNotListed_UsesTopLevel(t *testing.T) {
	m := &ModuleConfig{
		Name:   "db",
		Type:   "infra.database",
		Config: map[string]any{"size": "small"},
		Environments: map[string]*InfraEnvironmentResolution{
			"prod": {Config: map[string]any{"size": "large"}},
		},
	}
	resolved, ok := m.ResolveForEnv("dev")
	if !ok {
		t.Fatal("want ok=true when env not listed (falls back to top-level)")
	}
	if resolved.Config["size"] != "small" {
		t.Fatalf("want size=small, got %v", resolved.Config["size"])
	}
}
```

**Step 2: Run tests, confirm failure**

```
go test ./config/ -run TestResolveForEnv -v
```

Expected: FAIL (`ResolveForEnv` does not exist).

**Step 3: Implement `ResolveForEnv`**

Create `config/module_resolve_env.go`:

```go
package config

// ResolvedModule is the effective module config for a specific environment.
type ResolvedModule struct {
	Name     string
	Type     string
	Provider string
	Config   map[string]any
}

// ResolveForEnv returns the effective module config for envName.
// If m.Environments is empty or envName is not listed, the top-level fields are returned.
// If m.Environments[envName] is explicitly nil, ok=false (resource skipped in this env).
// Otherwise the per-env resolution is merged over the top-level fields.
func (m *ModuleConfig) ResolveForEnv(envName string) (*ResolvedModule, bool) {
	resolved := &ResolvedModule{
		Name:   m.Name,
		Type:   m.Type,
		Config: cloneMap(m.Config),
	}

	if len(m.Environments) == 0 {
		return resolved, true
	}

	envRes, listed := m.Environments[envName]
	if !listed {
		// Env not mentioned — use top-level.
		return resolved, true
	}
	if envRes == nil {
		// Explicit skip.
		return nil, false
	}

	if envRes.Provider != "" {
		resolved.Provider = envRes.Provider
	}
	for k, v := range envRes.Config {
		if resolved.Config == nil {
			resolved.Config = map[string]any{}
		}
		resolved.Config[k] = v
	}
	return resolved, true
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
```

**Step 4: Run tests, confirm pass**

```
go test ./config/ -run TestResolveForEnv -v
```

Expected: PASS (all 4 subtests).

**Step 5: Commit**

```
git add config/module_resolve_env.go config/module_resolve_env_test.go
git commit -m "config: add ResolveForEnv helper on ModuleConfig"
```

### Task 4: Switch `wfctl infra` from raw YAML parse to `config.LoadFromFile`

**Files:**
- Modify: `cmd/wfctl/infra.go:98-165` (replace `infraModuleEntry` + `discoverInfraModules` with `config.LoadFromFile`-based discovery)
- Modify: `cmd/wfctl/infra_bootstrap.go` (same pattern wherever it parses YAML)

**Step 1: Write the failing test**

Create `cmd/wfctl/infra_imports_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Verifies that wfctl infra honors imports: in config files.
func TestDiscoverInfraModules_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `modules:
  - name: cloud-credentials
    type: cloud.account
    config:
      provider: mock
`
	main := `imports:
  - shared.yaml
modules:
  - name: bmw-database
    type: infra.database
    config:
      size: small
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	state, platforms, accounts, err := discoverInfraModules(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_ = state
	_ = platforms
	if len(accounts) != 1 {
		t.Fatalf("want 1 cloud.account from imported shared.yaml, got %d", len(accounts))
	}
	if accounts[0].Name != "cloud-credentials" {
		t.Fatalf("want cloud-credentials, got %s", accounts[0].Name)
	}
}
```

**Step 2: Run test, confirm failure**

```
go test ./cmd/wfctl/ -run TestDiscoverInfraModules_HonorsImports -v
```

Expected: FAIL (import not resolved — accounts len is 0).

**Step 3: Replace raw-YAML discovery with `config.LoadFromFile`**

In `cmd/wfctl/infra.go`:

- Delete the `infraModuleEntry` struct (lines ~98-103).
- Rewrite `discoverInfraModules` to accept the path, call `config.LoadFromFile`, and map `cfg.Modules` to the existing return signature. Preserve the switch that categorises modules as `iac.state`, `cloud.account`, or `platform.*`/`infra.*`.

Replacement:

```go
// discoverInfraModules parses the config (resolving imports) and finds IaC-related modules.
func discoverInfraModules(cfgFile string) (iacState []infraModuleEntry, platforms []infraModuleEntry, cloudAccounts []infraModuleEntry, err error) {
	cfg, loadErr := config.LoadFromFile(cfgFile)
	if loadErr != nil {
		return nil, nil, nil, fmt.Errorf("load %s: %w", cfgFile, loadErr)
	}
	for _, m := range cfg.Modules {
		entry := infraModuleEntry{Name: m.Name, Type: m.Type, Config: m.Config}
		switch {
		case m.Type == "iac.state":
			iacState = append(iacState, entry)
		case m.Type == "cloud.account":
			cloudAccounts = append(cloudAccounts, entry)
		case strings.HasPrefix(m.Type, "platform.") || strings.HasPrefix(m.Type, "infra."):
			platforms = append(platforms, entry)
		}
	}
	return
}
```

Keep `infraModuleEntry` as a struct (still used elsewhere in the file), but now populated from `ModuleConfig`.

**Step 4: Add config import if missing**

Top of `cmd/wfctl/infra.go` — add `"github.com/GoCodeAlone/workflow/config"` to imports.

**Step 5: Run tests, confirm pass**

```
go test ./cmd/wfctl/ -run TestDiscoverInfraModules_HonorsImports -v
go test ./cmd/wfctl/... -count=1
```

Expected: both runs pass.

**Step 6: Commit**

```
git add cmd/wfctl/infra.go cmd/wfctl/infra_imports_test.go
git commit -m "wfctl infra: honor imports by switching to config.LoadFromFile"
```

### Task 5: Apply the same `LoadFromFile` switch to `infra_bootstrap.go`

**Files:**
- Modify: `cmd/wfctl/infra_bootstrap.go` (wherever raw YAML is parsed)

**Step 1: Locate raw YAML parsing in `infra_bootstrap.go`**

```
grep -n "yaml.Unmarshal\|os.ReadFile" cmd/wfctl/infra_bootstrap.go
```

**Step 2: Replace with `config.LoadFromFile`** mirroring Task 4's pattern. If the file already uses `config.LoadFromFile`, no change needed.

**Step 3: Add a test** (mirror `infra_imports_test.go`) asserting `runInfraBootstrap` resolves imports.

**Step 4: Run tests**

```
go test ./cmd/wfctl/... -count=1
```

Expected: pass.

**Step 5: Commit**

```
git add cmd/wfctl/infra_bootstrap.go cmd/wfctl/infra_bootstrap_imports_test.go
git commit -m "wfctl infra bootstrap: honor imports"
```

### Task 6: Add `--env` flag to `wfctl infra plan`

**Files:**
- Modify: `cmd/wfctl/infra.go` — `runInfraPlan`

**Step 1: Write a failing test**

Create `cmd/wfctl/infra_env_flag_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfraPlan_EnvFlagFiltersResources(t *testing.T) {
	dir := t.TempDir()
	cfg := `modules:
  - name: cloud-credentials
    type: cloud.account
    config:
      provider: mock
  - name: bmw-database
    type: infra.database
    config:
      size: small
    environments:
      staging:
        config:
          size: small
      prod:
        config:
          size: large
  - name: bmw-dns
    type: infra.dns
    environments:
      prod:
        config:
          domain: example.com
      staging: null
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("staging plan excludes dns", func(t *testing.T) {
		resources, err := planResourcesForEnv(path, "staging")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range resources {
			if r.Name == "bmw-dns" {
				t.Fatalf("dns should be skipped under staging (null env)")
			}
			if r.Name == "bmw-database" && r.Config["size"] != "small" {
				t.Fatalf("want staging size=small, got %v", r.Config["size"])
			}
		}
	})

	t.Run("prod plan includes dns with prod sizing", func(t *testing.T) {
		resources, err := planResourcesForEnv(path, "prod")
		if err != nil {
			t.Fatal(err)
		}
		var sawDNS, sawLargeDB bool
		for _, r := range resources {
			if r.Name == "bmw-dns" {
				sawDNS = true
			}
			if r.Name == "bmw-database" && r.Config["size"] == "large" {
				sawLargeDB = true
			}
		}
		if !sawDNS {
			t.Fatal("prod plan should include dns")
		}
		if !sawLargeDB {
			t.Fatal("prod plan should have size=large")
		}
	})
}
```

**Step 2: Run test, confirm failure**

```
go test ./cmd/wfctl/ -run TestInfraPlan_EnvFlagFiltersResources -v
```

Expected: FAIL (`planResourcesForEnv` does not exist).

**Step 3: Implement the resolver**

Add to `cmd/wfctl/infra.go`:

```go
// planResourcesForEnv loads the config at path and returns the list of
// resolved resources for envName. Resources whose environments[envName] is
// explicitly null are skipped. If envName is empty, all resources are returned
// with their top-level config.
func planResourcesForEnv(path, envName string) ([]*config.ResolvedModule, error) {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	var out []*config.ResolvedModule
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if envName == "" {
			out = append(out, &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: m.Config})
			continue
		}
		if resolved, ok := m.ResolveForEnv(envName); ok {
			out = append(out, resolved)
		}
	}
	return out, nil
}
```

**Step 4: Wire `--env` into `runInfraPlan`**

In `runInfraPlan`:

```go
envName := fs.String("env", "", "Environment name (resolves per-module environments: overrides)")
```

Route through `planResourcesForEnv(cfgFile, *envName)` instead of the raw discovery; feed the resolved resources into the existing plan rendering logic.

**Step 5: Run tests, confirm pass**

```
go test ./cmd/wfctl/ -run TestInfraPlan_EnvFlagFiltersResources -v
go test ./cmd/wfctl/... -count=1
```

Expected: pass.

**Step 6: Commit**

```
git add cmd/wfctl/infra.go cmd/wfctl/infra_env_flag_test.go
git commit -m "wfctl infra plan: add --env flag with per-module resolution"
```

### Task 7: Wire `--env` into `apply`, `destroy`, `status`, `drift`, `bootstrap`

**Files:**
- Modify: `cmd/wfctl/infra.go` — each of `runInfraApply`, `runInfraDestroy`, `runInfraStatus`, `runInfraDrift`
- Modify: `cmd/wfctl/infra_bootstrap.go` — `runInfraBootstrap`

**Step 1: For each command, add the `envName` flag** — follow Task 6's pattern exactly.

**Step 2: Write an integration test** per command confirming that the resolved resource list honors `--env`.

Single shared helper test: `cmd/wfctl/infra_env_integration_test.go`:

```go
package main

import "testing"

func TestInfraCommands_AllHonorEnvFlag(t *testing.T) {
	cmds := []string{"plan", "apply", "status", "drift", "bootstrap", "destroy"}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			fs := newInfraFlagSet(cmd)
			if fs.Lookup("env") == nil {
				t.Fatalf("%s is missing --env flag", cmd)
			}
		})
	}
}
```

Needs a small helper `newInfraFlagSet(cmd)` that returns the flagset each subcommand configures (factor this out during the task). Alternatively, assert by executing each subcommand with `--env foo --config <fixture>` and checking no error.

**Step 3: Run tests**

```
go test ./cmd/wfctl/... -count=1
```

Expected: pass.

**Step 4: Commit**

```
git add cmd/wfctl/infra.go cmd/wfctl/infra_bootstrap.go cmd/wfctl/infra_env_integration_test.go
git commit -m "wfctl infra: add --env to apply/destroy/status/drift/bootstrap"
```

### Task 8: Inject `environments[env].envVars` into provisioned containers

**Files:**
- Modify: `cmd/wfctl/infra.go` — wherever container resources are rendered (search `http_port|container_service|dockerImage`)

**Step 1: Write a test**

Create `cmd/wfctl/infra_env_vars_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfraPlan_MergesTopLevelEnvVars(t *testing.T) {
	dir := t.TempDir()
	cfg := `environments:
  staging:
    provider: digitalocean
    envVars:
      LOG_LEVEL: debug
  prod:
    provider: digitalocean
    envVars:
      LOG_LEVEL: info
modules:
  - name: app
    type: infra.container_service
    config:
      image: example/app:latest
      env_vars:
        PORT: "8080"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := planResourcesForEnv(path, "prod")
	if err != nil {
		t.Fatal(err)
	}
	var app *struct{ envVars map[string]string }
	_ = app
	// Assert LOG_LEVEL=info was merged into app's env_vars and PORT preserved.
	// (fill in with actual shape once render path is wired.)
	_ = resolved
}
```

**Step 2: Implement the merge** — when resolving each module, if the top-level `environments[envName].envVars` is set and the module is a container/service type, merge keys into `resolved.Config["env_vars"]` (module wins on conflict).

**Step 3: Run test, pass.**

**Step 4: Commit**

```
git add cmd/wfctl/infra.go cmd/wfctl/infra_env_vars_test.go
git commit -m "wfctl infra: merge top-level environments[].envVars into container resources"
```

### Task 9: Wire `environments[env].secretsStoreOverride` into infra secret injection

**Files:**
- Modify: `cmd/wfctl/infra.go` — secret injection path (search `injectSecrets` calls)

**Step 1:** Ensure `injectSecrets(ctx, cfg, envName)` is called with the flag value wherever the infra commands resolve `${SECRET_NAME}` templates. This code path exists (`cmd/wfctl/deploy_providers.go:452`) but needs to be called from infra commands using the env.

**Step 2: Test** — write a test with two envs that set different `secretsStoreOverride` values, and confirm the resolved module uses the right store.

**Step 3: Commit.**

### Task 10: Add end-to-end fixture test

**Files:**
- Create: `cmd/wfctl/testdata/infra-multi-env.yaml`
- Create: `cmd/wfctl/infra_e2e_test.go`

**Step 1: Create the fixture** — a realistic two-env config matching BMW's target shape (database, vpc, firewall, registry, container_service, dns — with dns nil under staging).

**Step 2: Write the test** — invoke `wfctl infra plan --env staging` and `--env prod` as subprocess (`exec.Command`), parse output, assert the set of planned resources matches expectations.

**Step 3: Run, pass, commit.**

### Task 11: Update CHANGELOG + version bump to v0.11.0

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` if it references the infra command surface

**Step 1: Add CHANGELOG entry** under `## [0.11.0] - 2026-04-17`:

```markdown
### Added
- `wfctl infra plan|apply|bootstrap|destroy|status|drift` now accept `--env <name>`.
- Module configs support an `environments:` block for per-environment resolution (provider/config/image). Set an env value to `null` to skip the module in that env.
- Top-level `environments:` `envVars` are merged into container resources during infra apply.
- `wfctl infra` now honors `imports:` (consistent with every other wfctl subcommand).

### Fixed
- `ModuleConfig` previously lacked an `Environments` field; it was defined on the unused `InfraResourceConfig` type. Multi-env is now wired to the schema `wfctl infra` actually parses.
```

**Step 2: Commit**

```
git add CHANGELOG.md README.md
git commit -m "chore: v0.11.0 — multi-env support in wfctl infra"
```

### Task 12: Tag and release v0.11.0

**Step 1: Verify all tests pass**

```
go test ./... -count=1
```

**Step 2: Tag**

```
git tag v0.11.0
git push origin main v0.11.0
```

Release workflow (`.github/workflows/release.yml`) takes over.

**Step 3: Wait for release to publish** and confirm `setup-wfctl@v1` picks up the new version.

---

## Phase 2 — Tutorial in `workflow`

### Task 13: Draft `docs/tutorials/deploy-pipeline.md`

**Files:**
- Create: `docs/tutorials/deploy-pipeline.md`

**Step 1: Write the tutorial** covering:

1. Minimal single-env `infra.yaml`
2. Add a second env via `environments:` (top-level + per-module)
3. Share config with `imports:`
4. Declare `ci.deploy.environments` with `healthCheck`
5. Generate GH Actions: `wfctl ci init --platform github-actions`
6. Customize: add build/push between `build-test` and `deploy-staging`
7. Auto-promote: `deploy-prod.needs: [deploy-staging]`
8. Optional `requireApproval: true` for manual gates
9. Zero-secret-prep with `wfctl infra bootstrap` + `secrets: generate:`
10. Troubleshooting

Use the v0.11.0 behavior. Include working copy-paste snippets.

**Step 2: Add cross-links**
- From `docs/DEPLOYMENT_GUIDE.md` — add a "See also" pointing to this tutorial.
- From this tutorial — link to `docs/WFCTL.md` and `docs/DEPLOYMENT_GUIDE.md`.

**Step 3: Commit**

```
git add docs/tutorials/deploy-pipeline.md docs/DEPLOYMENT_GUIDE.md
git commit -m "docs: add deploy-pipeline multi-env tutorial"
```

---

## Phase 3 — Retrofit `workflow-dnd`

Work directory: `/Users/jon/workspace/workflow-dnd`

### Task 14: Bump workflow dependency to v0.11.0

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1:**

```
cd /Users/jon/workspace/workflow-dnd
go get github.com/GoCodeAlone/workflow@v0.11.0
go mod tidy
go build ./...
```

**Step 2: Commit**

```
git add go.mod go.sum
git commit -m "chore: bump workflow to v0.11.0"
```

### Task 15: Collapse `infra/staging.yaml` into single `infra.yaml`

**Files:**
- Delete: `infra/staging.yaml`
- Create: `infra.yaml`

**Step 1: Write the new config** — content of current `infra/staging.yaml` with each module gaining an `environments.staging:` block mirroring its current config. This is a structural change; the rendered resource list stays identical when `--env staging` is passed.

Baseline shape:

```yaml
infra:
  auto_bootstrap: true

secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow-dnd
    token_env: GH_MANAGEMENT_TOKEN
  generate:
    - key: JWT_SECRET
      type: random_hex
      length: 32
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: dnd-deploy-key

environments:
  staging:
    provider: digitalocean
    region: nyc3

modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env
      region: nyc3

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      region: nyc3
      bucket: dnd-iac-state
      prefix: staging/
      accessKey: ${SPACES_ACCESS_KEY}
      secretKey: ${SPACES_SECRET_KEY}

  - name: dnd-vpc
    type: infra.vpc
    config:
      cidr: "10.10.0.0/16"
      region: nyc3
      provider: do-provider
    environments:
      staging:
        config:
          name: dnd-staging-vpc

  # ... (firewall, registry, database, app with same pattern)
```

**Step 2: Delete `infra/staging.yaml`**

```
git rm infra/staging.yaml
```

**Step 3: Validate**

```
wfctl validate infra.yaml
wfctl infra plan --env staging --config infra.yaml
```

Expected: plan succeeds, resource list matches pre-change plan.

**Step 4: Commit**

```
git add infra.yaml
git commit -m "infra: collapse staging.yaml into single infra.yaml with environments block"
```

### Task 16: Add `ci.deploy.environments.staging`

**Files:**
- Modify: `infra.yaml`

**Step 1: Add**

```yaml
ci:
  deploy:
    environments:
      staging:
        provider: digitalocean
        region: nyc3
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
```

**Step 2: Validate**

```
wfctl validate infra.yaml
```

**Step 3: Commit**

```
git add infra.yaml
git commit -m "ci: add deploy.environments.staging with health check"
```

### Task 17: Regenerate `deploy.yml`

**Files:**
- Modify: `.github/workflows/deploy.yml` (regenerate then customize)

**Step 1: Generate**

```
wfctl ci init --platform github-actions --config infra.yaml --output .github/workflows/deploy.generated.yml
```

**Step 2: Port the existing Docker build/push steps** from the current `deploy.yml` into the generated workflow — keep them between the build-test job and the `deploy-staging` job.

**Step 3: Delete the `.generated.yml` scratch file** once merged.

**Step 4: Dry-run via `act` or push a PR** to confirm the pipeline runs.

**Step 5: Commit**

```
git add .github/workflows/deploy.yml
git commit -m "ci: regenerate deploy.yml via wfctl ci init with env-aware jobs"
```

---

## Phase 4 — BMW migration

Work directory: `/Users/jon/workspace/buymywishlist`

### Task 18: Bump workflow dependency to v0.11.0

Same pattern as Task 14.

### Task 19: Rewrite `infra.yaml` with `environments: { staging, prod }`

**Files:**
- Replace: `infra.yaml`

**Step 1: Design the new config**

```yaml
infra:
  auto_bootstrap: true

secrets:
  provider: github
  config:
    repo: GoCodeAlone/buymywishlist
    token_env: GH_MANAGEMENT_TOKEN
  generate:
    - key: JWT_SECRET
      type: random_hex
      length: 32
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: bmw-deploy-key

environments:
  staging:
    provider: digitalocean
    region: nyc3
  prod:
    provider: digitalocean
    region: nyc1

modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      bucket: bmw-iac-state
      accessKey: ${SPACES_ACCESS_KEY}
      secretKey: ${SPACES_SECRET_KEY}
    environments:
      staging:
        config:
          prefix: staging/
          region: nyc3
      prod:
        config:
          prefix: prod/
          region: nyc1

  - name: bmw-vpc
    type: infra.vpc
    config:
      cidr: "10.10.10.0/24"
      provider: do-provider
    environments:
      staging:
        config:
          name: bmw-staging-vpc
          region: nyc3
      prod:
        config:
          name: bmw-prod-vpc
          region: nyc1

  - name: bmw-firewall
    type: infra.firewall
    config:
      provider: do-provider
      inbound_rules:
        - protocol: tcp
          ports: "443"
          sources: ["0.0.0.0/0"]
        - protocol: tcp
          ports: "80"
          sources: ["0.0.0.0/0"]
    environments:
      staging:
        config:
          name: bmw-staging-firewall
      prod:
        config:
          name: bmw-prod-firewall

  - name: bmw-registry
    type: infra.registry
    config:
      name: bmw-registry
      tier: basic
      region: nyc3
      provider: do-provider

  - name: bmw-database
    type: infra.database
    config:
      engine: pg
      version: "16"
      num_nodes: 1
      provider: do-provider
    environments:
      staging:
        config:
          name: bmw-staging-db
          size: db-s-1vcpu-1gb
          region: nyc3
      prod:
        config:
          name: bmw-prod-db
          size: db-s-2vcpu-4gb
          region: nyc1

  - name: bmw-app
    type: infra.container_service
    config:
      image: registry.digitalocean.com/bmw-registry/buymywishlist:latest
      http_port: 8080
      provider: do-provider
      env_vars:
        DATABASE_URL: "${DATABASE_URL}"
        JWT_SECRET: "${JWT_SECRET}"
        STRIPE_SECRET_KEY: "${STRIPE_SECRET_KEY}"
    environments:
      staging:
        config:
          name: bmw-staging
          instance_count: 1
          region: nyc3
          env_vars:
            APP_ORIGIN: "https://bmw-staging.ondigitalocean.app"
      prod:
        config:
          name: buymywishlist
          instance_count: 2
          region: nyc1
          env_vars:
            APP_ORIGIN: "https://buymywishlist.com"
            WEBAUTHN_RP_ID: "buymywishlist.com"
            WEBAUTHN_ORIGIN: "https://buymywishlist.com"

  - name: bmw-dns
    type: infra.dns
    config:
      provider: do-provider
    environments:
      staging: null  # no custom domain for staging
      prod:
        config:
          domain: buymywishlist.com
          records:
            - name: "@"
              type: A
              data: "${APP_IP:-127.0.0.1}"
              ttl: 300
            - name: www
              type: CNAME
              data: buymywishlist.com.
              ttl: 300

ci:
  deploy:
    environments:
      staging:
        provider: digitalocean
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
      prod:
        provider: digitalocean
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
```

**Step 2: Validate both envs**

```
wfctl validate infra.yaml
wfctl infra plan --env staging --config infra.yaml
wfctl infra plan --env prod --config infra.yaml
```

**Step 3: Commit**

```
git add infra.yaml
git commit -m "infra: rewrite with environments block (staging+prod, infra.* types, DOCR)"
```

### Task 20: Generate new `.github/workflows/deploy.yml`

**Files:**
- Delete: `.github/workflows/infra.yml`
- Create: `.github/workflows/deploy.yml`

**Step 1: Generate**

```
wfctl ci init --platform github-actions --config infra.yaml --output .github/workflows/deploy.yml
```

**Step 2: Customize generated workflow**

Insert a `build-image` job between `build-test` and `deploy-staging`:

```yaml
  build-image:
    runs-on: ubuntu-latest
    needs: [build-test]
    outputs:
      sha: ${{ steps.meta.outputs.sha }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - uses: digitalocean/action-doctl@v2
        with:
          token: ${{ secrets.DIGITALOCEAN_TOKEN }}
      - name: Cross-compile workflow-server and bmw-plugin
        env:
          GOPRIVATE: github.com/GoCodeAlone/*
          RELEASES_TOKEN: ${{ secrets.RELEASES_TOKEN }}
        run: |
          git config --global url."https://${RELEASES_TOKEN}@github.com/".insteadOf "https://github.com/"
          GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o workflow-server-linux github.com/GoCodeAlone/workflow/cmd/server
          GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bmw-plugin-linux ./cmd/bmw-plugin
      - name: Build UI
        working-directory: ui
        run: |
          npm ci
          npm run build
      - name: Copy assets to build root
        run: |
          cp -r ui/dist dist
          cp bmwplugin/plugin.json plugin.json
      - name: Build and push image
        id: meta
        run: |
          doctl registry login
          docker build -f Dockerfile.prebuilt -t registry.digitalocean.com/bmw-registry/buymywishlist:${{ github.sha }} .
          docker tag registry.digitalocean.com/bmw-registry/buymywishlist:${{ github.sha }} registry.digitalocean.com/bmw-registry/buymywishlist:latest
          docker push registry.digitalocean.com/bmw-registry/buymywishlist:${{ github.sha }}
          docker push registry.digitalocean.com/bmw-registry/buymywishlist:latest
          echo "sha=${{ github.sha }}" >> $GITHUB_OUTPUT
```

Make `deploy-staging` and `deploy-prod` depend on `build-image`:

```yaml
  deploy-staging:
    needs: [build-image]
  deploy-prod:
    needs: [deploy-staging]
```

Both deploy jobs run:

```yaml
  - uses: GoCodeAlone/setup-wfctl@v1
  - run: wfctl ci run --phase deploy --env <name>
    env:
      DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
      GH_MANAGEMENT_TOKEN: ${{ secrets.GH_MANAGEMENT_TOKEN }}
      STRIPE_SECRET_KEY: ${{ secrets.STRIPE_SECRET_KEY }}
      IMAGE_SHA: ${{ needs.build-image.outputs.sha }}
```

**Step 3: Delete old manual workflow**

```
git rm .github/workflows/infra.yml
```

**Step 4: Commit**

```
git add .github/workflows/deploy.yml
git commit -m "ci: replace manual infra.yml with auto-promotion deploy.yml"
```

### Task 21: Open PR, verify staging deploy, verify prod auto-promotion

**Step 1: Push branch, open PR**

```
gh pr create --title "ci: migrate to wfctl multi-env deploy pipeline" --body "..."
```

**Step 2: Watch the first PR run** — `wfctl infra plan --env staging` should post a plan comment.

**Step 3: Merge, watch `build-image → deploy-staging → deploy-prod`**

Capture logs and confirm:
- DOCR push succeeds
- Staging applies cleanly
- `/health` check passes
- Prod applies with same SHA

**Step 4: If prod fails, revert the merge.** The image tag under `bmw-app.environments.prod.config.image` can be pinned to the last-known-good SHA in a hotfix commit.

**Step 5: Delete `.github/workflows/infra.yml` references** anywhere in README/docs.

---

## Phase 5 — Final wrap-up

### Task 22: Update BMW CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` in `buymywishlist`

**Step 1:** Replace the manual `infra.yml` deployment instructions with a pointer to `deploy.yml` and the new auto-promotion flow.

**Step 2: Commit.**

### Task 23: Record the GHCR sunset follow-up

**Files:**
- Create: `docs/plans/2026-04-XX-ghcr-sunset.md` (placeholder, date left blank)

After two clean prod deploys to DOCR, write up the GHCR publish removal as a separate small plan.

### Task 24: Update workspace MEMORY

**Files:**
- Update entries for `workflow` (v0.11.0), `workflow-dnd`, `buymywishlist` in the workspace memory index
- Add a feedback memory if any surprising corrections surfaced during execution

---

## Testing conventions used throughout

- All Go tests use `-count=1` to defeat caching.
- Integration tests that shell out to `wfctl` use `exec.LookPath` to find the binary and skip if absent (`t.Skip`), so unit test runs don't require a compiled binary.
- YAML fixtures live under `cmd/wfctl/testdata/` with self-documenting filenames.
- Each phase's work is isolated by `git commit` per task so individual tasks can be reverted cleanly.

## Skills referenced

- @superpowers:test-driven-development — every task with a code change follows the failing-test → minimal-implementation → passing-test rhythm.
- @superpowers:verification-before-completion — before marking any task complete, the specified verification command must have been run and its output observed.
- @superpowers:using-git-worktrees — D3 and D4 should be executed in dedicated worktrees given they touch different repos.
