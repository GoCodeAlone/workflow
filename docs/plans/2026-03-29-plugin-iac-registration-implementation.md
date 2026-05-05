---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow
    commit: a0929e9
  - repo: workflow
    commit: 1962903
  - repo: workflow
    commit: bacd7f5
  - repo: workflow
    commit: 9a886f8
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "moduleInfraRequirements|SecretStore|secretsStoreOverride|ResolveSecretStore" config cmd mcp docs -S'
    - 'git log --oneline --all -- config/plugin_manifest.go config/secrets_config.go cmd/wfctl/plugin_infra.go cmd/wfctl/secrets_resolve.go'
  result: pass
supersedes: []
superseded_by: []
---

# Plugin IaC Registration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Plugins declare infrastructure dependencies in manifests. Multi-store secrets with per-secret routing. Per-environment infrastructure resolution. Access-aware secret status. Wizard bulk secret setup.

**Architecture:** Extend plugin.json manifest with `moduleInfraRequirements`. Refactor `SecretsConfig` to support named stores with `secretStores` map and per-secret `store` field. Extend `InfraResourceConfig` with per-environment resolution strategies. Update `detect_infra_needs` MCP tool and `wfctl secrets` to consult plugin manifests and multi-store routing. Add `Check()` to SecretsProvider interface.

**Tech Stack:** Go 1.26, existing plugin/manifest system, existing secrets providers

**Design Doc:** `docs/plans/2026-03-29-plugin-iac-registration-design.md`

---

### Task 1: Manifest schema — moduleInfraRequirements

**Files:**
- Create: `config/plugin_manifest.go` — Go structs for the manifest infra section
- Create: `config/plugin_manifest_test.go`

Define the manifest types that represent plugin infrastructure requirements:

```go
// config/plugin_manifest.go
package config

// PluginInfraRequirements maps module types to their infrastructure needs.
type PluginInfraRequirements map[string]*ModuleInfraSpec

// ModuleInfraSpec declares what a module type requires.
type ModuleInfraSpec struct {
	Requires []InfraRequirement `json:"requires" yaml:"requires"`
}

// InfraRequirement is a single infrastructure dependency.
type InfraRequirement struct {
	Type        string   `json:"type" yaml:"type"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	DockerImage string   `json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	Ports       []int    `json:"ports,omitempty" yaml:"ports,omitempty"`
	Secrets     []string `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Providers   []string `json:"providers,omitempty" yaml:"providers,omitempty"`
	Optional    bool     `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// PluginManifestFile represents the full plugin.json manifest.
type PluginManifestFile struct {
	Name                   string                  `json:"name"`
	Version                string                  `json:"version"`
	Description            string                  `json:"description"`
	Capabilities           PluginCapabilities      `json:"capabilities"`
	ModuleInfraRequirements PluginInfraRequirements `json:"moduleInfraRequirements,omitempty"`
}

type PluginCapabilities struct {
	ModuleTypes []string `json:"moduleTypes"`
	StepTypes   []string `json:"stepTypes"`
	TriggerTypes []string `json:"triggerTypes"`
}
```

Test: parse a plugin.json with `moduleInfraRequirements`, verify all fields round-trip.

Run: `go test ./config/ -run TestPluginManifest -v`
Commit: `feat: plugin manifest structs with moduleInfraRequirements`

---

### Task 2: Config updates — multi-store secrets + per-env infra resolution

**Files:**
- Modify: `config/secrets_config.go` — add SecretStoreConfig, update SecretsConfig, add Store field to SecretEntry
- Modify: `config/config.go` — add SecretStores field to WorkflowConfig
- Modify: `config/environments_config.go` — add SecretsStoreOverride
- Create: `config/infra_resolution.go` — per-environment resolution strategy for infra resources
- Create: `config/secrets_multistore_test.go`

Update SecretsConfig for multi-store:

```go
// config/secrets_config.go additions

// SecretStoreConfig defines a named secret storage backend.
type SecretStoreConfig struct {
	Provider string         `json:"provider" yaml:"provider"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}
```

Update SecretEntry:
```go
type SecretEntry struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Store       string                 `json:"store,omitempty" yaml:"store,omitempty"`
	Rotation    *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
```

Update SecretsConfig:
```go
type SecretsConfig struct {
	DefaultStore string                 `json:"defaultStore,omitempty" yaml:"defaultStore,omitempty"`
	Entries      []SecretEntry          `json:"entries,omitempty" yaml:"entries,omitempty"`
	// Keep legacy Provider field for backward compat
	Provider     string                 `json:"provider,omitempty" yaml:"provider,omitempty"`
	Config       map[string]any         `json:"config,omitempty" yaml:"config,omitempty"`
	Rotation     *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
```

Add to WorkflowConfig:
```go
SecretStores map[string]*SecretStoreConfig `json:"secretStores,omitempty" yaml:"secretStores,omitempty"`
```

Add to EnvironmentConfig:
```go
SecretsStoreOverride string `json:"secretsStoreOverride,omitempty" yaml:"secretsStoreOverride,omitempty"`
```

Per-environment infra resolution:
```go
// config/infra_resolution.go
package config

// InfraEnvironmentResolution defines how a resource is resolved in a specific environment.
type InfraEnvironmentResolution struct {
	Strategy    string         `json:"strategy" yaml:"strategy"`
	DockerImage string         `json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	Port        int            `json:"port,omitempty" yaml:"port,omitempty"`
	Provider    string         `json:"provider,omitempty" yaml:"provider,omitempty"`
	Config      map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Connection  *InfraConnectionConfig `json:"connection,omitempty" yaml:"connection,omitempty"`
}

// InfraConnectionConfig holds connection details for strategy: existing.
type InfraConnectionConfig struct {
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port,omitempty" yaml:"port,omitempty"`
	Auth string `json:"auth,omitempty" yaml:"auth,omitempty"`
}
```

Update InfraResourceConfig:
```go
type InfraResourceConfig struct {
	Name         string                                    `json:"name" yaml:"name"`
	Type         string                                    `json:"type" yaml:"type"`
	Provider     string                                    `json:"provider,omitempty" yaml:"provider,omitempty"`
	Config       map[string]any                            `json:"config,omitempty" yaml:"config,omitempty"`
	Environments map[string]*InfraEnvironmentResolution    `json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

Test: parse YAML with secretStores + per-secret store + per-env infra strategies.

Run: `go test ./config/ -run TestMultiStore -v`
Commit: `feat: multi-store secrets + per-environment infrastructure resolution`

---

### Task 3: Extend detect_infra_needs to consult plugin manifests

**Files:**
- Create: `cmd/wfctl/plugin_infra.go` — loads plugin manifests and resolves infra requirements
- Create: `cmd/wfctl/plugin_infra_test.go`
- Modify: `mcp/scaffold_tools.go` — update detect_infra_needs handler

```go
// cmd/wfctl/plugin_infra.go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"github.com/GoCodeAlone/workflow/config"
)

// LoadPluginManifests loads all plugin.json files from the plugins directory.
func LoadPluginManifests(pluginsDir string) (map[string]*config.PluginManifestFile, error) {
	manifests := make(map[string]*config.PluginManifestFile)
	entries, err := os.ReadDir(pluginsDir)
	// For each dir, read plugin.json
	// Parse into PluginManifestFile
	// Key by plugin name
	return manifests, nil
}

// DetectPluginInfraNeeds checks which modules in the config come from plugins
// and returns their infrastructure requirements.
func DetectPluginInfraNeeds(cfg *config.WorkflowConfig, manifests map[string]*config.PluginManifestFile) []config.InfraRequirement {
	var needs []config.InfraRequirement
	for _, mod := range cfg.Modules {
		for _, manifest := range manifests {
			if reqs, ok := manifest.ModuleInfraRequirements[mod.Type]; ok {
				needs = append(needs, reqs.Requires...)
			}
		}
	}
	// Also check services[*].modules if multi-service
	return needs
}
```

Update the MCP `detect_infra_needs` handler to also call `DetectPluginInfraNeeds`.

Run: `go test ./cmd/wfctl/ -run TestPluginInfra -v && go test ./mcp/ -v`
Commit: `feat: detect_infra_needs consults plugin manifests for infra requirements`

---

### Task 4: Secrets provider Check() method + multi-store resolution

**Files:**
- Modify: `cmd/wfctl/secrets_providers.go` — add Check() to interface, implement for env provider
- Create: `cmd/wfctl/secrets_resolve.go` — multi-store resolution logic
- Create: `cmd/wfctl/secrets_resolve_test.go`

Add Check() to the provider interface:
```go
type SecretState int
const (
	SecretSet SecretState = iota
	SecretNotSet
	SecretNoAccess
	SecretFetchError
	SecretUnconfigured
)

type SecretsProvider interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	Check(ctx context.Context, name string) (SecretState, error)
	List(ctx context.Context) ([]SecretStatus, error)
	Delete(ctx context.Context, name string) error
}

type SecretStatus struct {
	Name        string
	Store       string
	State       SecretState
	Error       string
	LastRotated time.Time
}
```

Multi-store resolution:
```go
// ResolveSecretStore determines which store a secret uses for a given environment.
func ResolveSecretStore(secretName string, envName string, cfg *config.WorkflowConfig) string {
	// 1. Check environments[env].secretsStoreOverride
	if env, ok := cfg.Environments[envName]; ok && env.SecretsStoreOverride != "" {
		return env.SecretsStoreOverride
	}
	// 2. Check secret entry's store field
	if cfg.Secrets != nil {
		for _, entry := range cfg.Secrets.Entries {
			if entry.Name == secretName && entry.Store != "" {
				return entry.Store
			}
		}
	}
	// 3. Fall back to defaultStore
	if cfg.Secrets != nil && cfg.Secrets.DefaultStore != "" {
		return cfg.Secrets.DefaultStore
	}
	// 4. Legacy: use Provider field
	if cfg.Secrets != nil && cfg.Secrets.Provider != "" {
		return cfg.Secrets.Provider
	}
	return "env"
}
```

Update `wfctl secrets list` to show per-secret store routing and access-aware status.

Run: `go test ./cmd/wfctl/ -run TestSecretResolve -v`
Commit: `feat: multi-store secret resolution with Check() and access-aware status`

---

### Task 5: Deploy-time secret fetching from multi-store

**Files:**
- Modify: `cmd/wfctl/deploy_providers.go` — update `injectSecrets` to use multi-store resolution
- Create: `cmd/wfctl/deploy_secrets_test.go`

Update `injectSecrets` to resolve each secret from the correct store:

```go
func injectSecrets(cfg *config.WorkflowConfig, envName string) (map[string]string, error) {
	secrets := make(map[string]string)
	if cfg.Secrets == nil {
		return secrets, nil
	}
	for _, entry := range cfg.Secrets.Entries {
		storeName := ResolveSecretStore(entry.Name, envName, cfg)
		provider, err := getProviderForStore(storeName, cfg)
		if err != nil {
			return nil, fmt.Errorf("secret %s: store %q: %w", entry.Name, storeName, err)
		}
		val, err := provider.Get(context.Background(), entry.Name)
		if err != nil {
			return nil, fmt.Errorf("secret %s: failed to fetch from %s: %w", entry.Name, storeName, err)
		}
		secrets[entry.Name] = val
	}
	return secrets, nil
}
```

Test: verify correct store routing with mixed stores.

Run: `go test ./cmd/wfctl/ -run TestDeploySecrets -v`
Commit: `feat: deploy-time multi-store secret fetching`

---

### Task 6: Wizard updates — per-env infra + bulk secret setup

**Files:**
- Modify: `cmd/wfctl/wizard.go` — add infra resolution and secret setup screens
- Modify: `cmd/wfctl/wizard_models.go` — add new screen types
- Create: `cmd/wfctl/secrets_setup.go` — standalone `wfctl secrets setup --env` command

Add wizard screens:
- **Infra resolution screen**: for each detected infra resource, ask per-environment strategy (container/provision/existing). If existing, prompt for connection details.
- **Secret stores screen**: define named stores (github, aws, etc.), set default
- **Secret routing screen**: for secrets that need a non-default store, allow override
- **Bulk secret input screen**: hidden input for each secret, auto-generate for crypto keys, skip for no-access stores

Add `wfctl secrets setup --env <name>`:
```go
func runSecretsSetup(args []string) error {
	// Parse --env flag
	// Load config, resolve all secrets for this env
	// For each secret: Check() status → prompt if writable, skip if no-access
	// Use term.ReadPassword for hidden input
	// Offer auto-generate for keys
}
```

Run: `go build ./cmd/wfctl/`
Commit: `feat: wizard infra resolution + bulk secret setup + wfctl secrets setup`

---

### Task 7: Documentation

**Files:**
- Modify: `docs/dsl-reference.md` + `cmd/wfctl/dsl-reference-embedded.md` — secretStores, per-env infra resolution, plugin manifest guide
- Modify: `docs/WFCTL.md` — secrets setup command, updated secrets list output
- Modify: `CHANGELOG.md`
- Create: `docs/plugin-manifest-guide.md` — how plugin authors declare infra requirements

Commit: `docs: plugin IaC registration, multi-store secrets, per-env infra resolution`

---

## Summary

| Task | Scope | Key Deliverable |
|------|-------|-----------------|
| 1 | Config | Plugin manifest structs with moduleInfraRequirements |
| 2 | Config | SecretStores map, per-secret store routing, per-env infra resolution |
| 3 | Detection | detect_infra_needs loads plugin manifests for plugin module types |
| 4 | Secrets | Check() method, multi-store resolution, access-aware status |
| 5 | Deploy | Deploy-time secret fetching from correct store per secret |
| 6 | Wizard | Per-env infra screens, bulk secret setup, wfctl secrets setup |
| 7 | Docs | Plugin manifest guide, multi-store secrets docs |
