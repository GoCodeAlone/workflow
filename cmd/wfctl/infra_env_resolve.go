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
// applies top-level environments[env] defaults, and writes the entire
// WorkflowConfig back to a temp file — preserving secrets, secretStores,
// infra, environments, ci, workflows, pipelines, etc. so that bootstrap and
// pipeline commands have full context. The caller must defer os.Remove(tmpPath).
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", cfgFile, err)
	}

	var topEnv *config.EnvironmentConfig
	if cfg.Environments != nil {
		topEnv = cfg.Environments[envName]
	}

	// Resolve modules for envName. ResolveForEnv is called on ALL module types
	// (not just infra.*) so environments[envName]: null is honored for iac.state,
	// cloud.account, etc. Infra/platform defaults from topEnv are applied here.
	var resolved []config.ModuleConfig
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		rm, ok := m.ResolveForEnv(envName)
		if !ok {
			continue
		}
		if topEnv != nil && isInfraType(rm.Type) {
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
		// Rebuild as ModuleConfig preserving DependsOn and Branches from the
		// original (ResolvedModule doesn't carry them).
		resolved = append(resolved, config.ModuleConfig{
			Name:      rm.Name,
			Type:      rm.Type,
			Config:    rm.Config,
			DependsOn: m.DependsOn,
			Branches:  m.Branches,
		})
	}

	// Replace modules with the env-resolved list; clear Imports so the temp
	// file doesn't try to re-import files that may resolve relative to a
	// different directory.
	cfg.Modules = resolved
	cfg.Imports = nil
	cfg.ConfigDir = "" // internal field, not serialised

	data, err := yaml.Marshal(cfg)
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
