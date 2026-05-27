package wfctlhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// writeEnvResolvedConfig loads cfgFile (honoring imports:), resolves every
// module for envName (ResolveForEnv is called on ALL module types so that
// environments[envName]: null is honored for iac.*, cloud.account, etc.),
// applies top-level environments[env] defaults, and writes the entire
// WorkflowConfig back to a temp file. The caller must defer os.Remove(tmpPath).
//
// Mirrors cmd/wfctl/infra_env_resolve.go:writeEnvResolvedConfig so the
// helper path produces byte-identical resolved configs to the wfctl CLI.
// Per docs/plans/2026-05-27-infra-admin-dynamic.md Task 1 this is part of
// the lift; the cmd/wfctl version delegates here to avoid double
// maintenance.
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", cfgFile, err)
	}

	var topEnv *config.EnvironmentConfig
	if cfg.Environments != nil {
		topEnv = cfg.Environments[envName]
	}

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
		// ${VAR} / $VAR expansion is intentionally deferred to read time
		// (config.ExpandEnvInMap) so secrets generated AFTER this temp
		// file is written (e.g. bootstrap-generated SPACES_access_key)
		// are not substituted to empty strings here. Mirrors cmd/wfctl
		// behavior.
		resolved = append(resolved, config.ModuleConfig{
			Name:      rm.Name,
			Type:      rm.Type,
			Config:    rm.Config,
			DependsOn: m.DependsOn,
			Branches:  m.Branches,
		})
	}

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

// isInfraType returns true for module types in the infra.*/platform.*
// namespaces. Kept consistent with cmd/wfctl/infra.go:isInfraType.
func isInfraType(t string) bool {
	return strings.HasPrefix(t, "infra.") || strings.HasPrefix(t, "platform.")
}

// isContainerType returns true for module types that accept env_vars
// defaults from top-level environments[env]. Kept consistent with
// cmd/wfctl/infra.go:isContainerType.
func isContainerType(t string) bool {
	return t == "infra.container_service"
}
