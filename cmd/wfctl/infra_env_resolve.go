package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// writeEnvResolvedConfig loads cfgFile (honoring imports:), resolves every
// module for envName (skipping null-env entries), applies top-level
// environments[env] defaults (region, provider, envVars), and writes the
// result to a temp file in the same directory as cfgFile. The caller is
// responsible for removing the temp file (defer os.Remove(tmpPath)).
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", cfgFile, err)
	}

	var topEnv *config.EnvironmentConfig
	if cfg.Environments != nil {
		topEnv = cfg.Environments[envName]
	}

	var resolved []map[string]any
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !isInfraType(m.Type) {
			// Non-infra modules pass through unchanged.
			resolved = append(resolved, moduleToMap(m))
			continue
		}
		rm, ok := m.ResolveForEnv(envName)
		if !ok {
			continue // skip this module for envName
		}
		if topEnv != nil {
			if rm.Region == "" {
				rm.Region = topEnv.Region
				if rm.Region != "" && rm.Config != nil {
					if _, present := rm.Config["region"]; !present {
						rm.Config["region"] = rm.Region
					}
				}
			}
			if rm.Provider == "" {
				rm.Provider = topEnv.Provider
				if rm.Provider != "" && rm.Config != nil {
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
		resolved = append(resolved, resolvedModuleToMap(rm))
	}

	// Build a minimal config map for the resolved YAML.
	out := map[string]any{
		"modules": resolved,
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

func moduleToMap(m *config.ModuleConfig) map[string]any {
	out := map[string]any{
		"name": m.Name,
		"type": m.Type,
	}
	if len(m.Config) > 0 {
		out["config"] = m.Config
	}
	if len(m.DependsOn) > 0 {
		out["dependsOn"] = m.DependsOn
	}
	return out
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
