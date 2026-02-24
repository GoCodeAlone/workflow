package config

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
