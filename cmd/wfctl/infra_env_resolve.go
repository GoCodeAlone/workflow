package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// writeEnvResolvedConfig loads cfgFile (honoring imports:), resolves every
// module for envName (ResolveForEnv is called on ALL module types so that
// environments[envName]: null is honored for iac.*, cloud.account, etc.),
// applies top-level environments[env] defaults, and writes the result to a
// temp file in the same directory as cfgFile, preserving all top-level
// sections (secrets, secretStores, infra, environments, ...) so that
// bootstrap and pipeline commands have full context. The caller must
// defer os.Remove(tmpPath).
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", cfgFile, err)
	}

	var topEnv *config.EnvironmentConfig
	if cfg.Environments != nil {
		topEnv = cfg.Environments[envName]
	}

	var resolvedModules []map[string]any
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		// Call ResolveForEnv on ALL module types (not just infra.*) so that
		// environments[envName]: null is honored for iac.state, cloud.account, etc.
		rm, ok := m.ResolveForEnv(envName)
		if !ok {
			continue // skip this module for envName (explicit null)
		}
		if topEnv != nil && isInfraType(rm.Type) {
			// Apply top-level defaults only to infra/platform modules.
			if rm.Region == "" {
				rm.Region = topEnv.Region
				if rm.Region != "" {
					if rm.Config == nil {
						rm.Config = map[string]any{}
					}
					if _, present := rm.Config["region"]; !present {
						rm.Config["region"] = rm.Region
					}
				}
			}
			if rm.Provider == "" {
				rm.Provider = topEnv.Provider
				if rm.Provider != "" {
					if rm.Config == nil {
						rm.Config = map[string]any{}
					}
					if _, present := rm.Config["provider"]; !present {
						rm.Config["provider"] = rm.Provider
					}
				}
			}
			if isContainerType(rm.Type) && len(topEnv.EnvVars) > 0 {
				ev, _ := rm.Config["env_vars"].(map[string]any)
				if ev == nil {
					ev = map[string]any{}
				}
				for k, v := range topEnv.EnvVars {
					if _, present := ev[k]; !present {
						ev[k] = v
					}
				}
				rm.Config["env_vars"] = ev
			}
		}
		resolvedModules = append(resolvedModules, resolvedModuleToMap(rm))
	}

	// Build output preserving all top-level sections so pipeline and bootstrap
	// commands have full context (secrets, secretStores, infra, environments, ...).
	out := map[string]any{
		"modules": resolvedModules,
	}
	if cfg.Secrets != nil {
		out["secrets"] = cfg.Secrets
	}
	if len(cfg.SecretStores) > 0 {
		out["secretStores"] = cfg.SecretStores
	}
	if cfg.Infrastructure != nil {
		out["infrastructure"] = cfg.Infrastructure
	}
	if len(cfg.Environments) > 0 {
		out["environments"] = cfg.Environments
	}
	if cfg.CI != nil {
		out["ci"] = cfg.CI
	}
	if len(cfg.Workflows) > 0 {
		out["workflows"] = cfg.Workflows
	}
	if len(cfg.Pipelines) > 0 {
		out["pipelines"] = cfg.Pipelines
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshal resolved config: %w", err)
	}

	dir := filepath.Dir(cfgFile)
	f, err := os.CreateTemp(dir, ".wfctl-env-resolved-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write temp config: %w", err)
	}
	f.Close()
	return f.Name(), nil
}

func resolvedModuleToMap(r *config.ResolvedModule) map[string]any {
	out := map[string]any{
		"name": r.Name,
		"type": r.Type,
	}
	if len(r.Config) > 0 {
		out["config"] = r.Config
	}
	return out
}
