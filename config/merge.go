package config

// DeepMergeConfigs merges override config on top of base config with override-wins semantics.
// Unlike MergeConfigs (which uses primary-wins for fragment injection), this uses override-wins
// for tenant config customization.
func DeepMergeConfigs(base, override *WorkflowConfig) *WorkflowConfig {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := &WorkflowConfig{
		Modules:   deepMergeModules(base.Modules, override.Modules),
		Workflows: deepMergeMap(base.Workflows, override.Workflows),
		Triggers:  deepMergeMap(base.Triggers, override.Triggers),
		Pipelines: deepMergeMap(base.Pipelines, override.Pipelines),
		Platform:  deepMergeMap(base.Platform, override.Platform),
		ConfigDir: base.ConfigDir,
	}
	if override.ConfigDir != "" {
		result.ConfigDir = override.ConfigDir
	}
	if override.Requires != nil {
		result.Requires = override.Requires
	} else {
		result.Requires = base.Requires
	}
	return result
}

func deepMergeModules(base, override []ModuleConfig) []ModuleConfig {
	if len(override) == 0 {
		return base
	}
	result := make([]ModuleConfig, len(base))
	copy(result, base)

	baseIdx := make(map[string]int)
	for i, m := range result {
		baseIdx[m.Name] = i
	}

	for _, om := range override {
		if idx, ok := baseIdx[om.Name]; ok {
			merged := result[idx]
			merged.Config = deepMergeMap(merged.Config, om.Config)
			if om.Type != "" {
				merged.Type = om.Type
			}
			result[idx] = merged
		} else {
			result = append(result, om)
		}
	}
	return result
}

func deepMergeMap(base, override map[string]any) map[string]any {
	if base == nil && override == nil {
		return nil
	}
	result := make(map[string]any)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		if baseVal, exists := result[k]; exists {
			baseMap, baseIsMap := baseVal.(map[string]any)
			overMap, overIsMap := v.(map[string]any)
			if baseIsMap && overIsMap {
				result[k] = deepMergeMap(baseMap, overMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

// MergeConfigs merges a config fragment into the primary config.
// Modules are appended. Workflows and triggers are merged without
// overwriting existing keys.
func MergeConfigs(primary, fragment *WorkflowConfig) {
	primary.Modules = append(primary.Modules, fragment.Modules...)

	if len(fragment.Workflows) > 0 {
		if primary.Workflows == nil {
			primary.Workflows = make(map[string]any)
		}
		for k, v := range fragment.Workflows {
			if _, exists := primary.Workflows[k]; !exists {
				primary.Workflows[k] = v
			}
		}
	}

	if len(fragment.Triggers) > 0 {
		if primary.Triggers == nil {
			primary.Triggers = make(map[string]any)
		}
		for k, v := range fragment.Triggers {
			if _, exists := primary.Triggers[k]; !exists {
				primary.Triggers[k] = v
			}
		}
	}
}
