package config

// ResolvedModule is the effective module config for a specific environment.
type ResolvedModule struct {
	Name     string
	Type     string
	Provider string
	Region   string
	Config   map[string]any
}

// ResolveForEnv returns the effective module config for envName.
// If m.Environments is empty or envName is not listed, the top-level fields are returned.
// If m.Environments[envName] is explicitly nil, ok=false (resource skipped in this env).
// Otherwise the per-env resolution is deep-merged over the top-level fields.
// region and provider are written into the Config map so downstream ResourceSpec
// construction (which reads only Config) picks them up.
func (m *ModuleConfig) ResolveForEnv(envName string) (*ResolvedModule, bool) {
	resolved := &ResolvedModule{
		Name:   m.Name,
		Type:   m.Type,
		Config: cloneMap(m.Config),
	}
	setRegionFromConfig(resolved)

	if len(m.Environments) == 0 {
		return resolved, true
	}

	envRes, listed := m.Environments[envName]
	if !listed {
		return resolved, true
	}
	if envRes == nil {
		return nil, false
	}

	if envRes.Provider != "" {
		resolved.Provider = envRes.Provider
		if resolved.Config == nil {
			resolved.Config = map[string]any{}
		}
		// Write into Config so ResourceSpec construction sees it.
		if _, present := resolved.Config["provider"]; !present {
			resolved.Config["provider"] = envRes.Provider
		}
	}

	// Deep-merge env overrides so nested maps (e.g. env_vars) are merged
	// rather than replaced wholesale.
	if len(envRes.Config) > 0 {
		if resolved.Config == nil {
			resolved.Config = map[string]any{}
		}
		resolved.Config = deepMergeMap(resolved.Config, envRes.Config)
	}

	setRegionFromConfig(resolved) // re-apply after env overrides
	// Write region into Config so downstream ResourceSpec construction sees it.
	if resolved.Region != "" {
		if _, present := resolved.Config["region"]; !present {
			resolved.Config["region"] = resolved.Region
		}
	}
	return resolved, true
}

func setRegionFromConfig(r *ResolvedModule) {
	if r == nil {
		return
	}
	if v, ok := r.Config["region"].(string); ok {
		r.Region = v
	}
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
