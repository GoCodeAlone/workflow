package config

// WorkflowConfig represents a workflow definition
type WorkflowConfig struct {
	Modules   []ModuleConfig         `json:"modules" yaml:"modules"`
	Workflows map[string]interface{} `json:"workflows" yaml:"workflows"`
}

// ModuleConfig represents configuration for a single module
type ModuleConfig struct {
	Name      string                 `json:"name" yaml:"name"`
	Type      string                 `json:"type" yaml:"type"`
	DependsOn []string               `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}
