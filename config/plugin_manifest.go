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
	Name                    string                  `json:"name" yaml:"name"`
	Version                 string                  `json:"version" yaml:"version"`
	Description             string                  `json:"description" yaml:"description"`
	Capabilities            PluginCapabilities      `json:"capabilities" yaml:"capabilities"`
	ModuleInfraRequirements PluginInfraRequirements `json:"moduleInfraRequirements,omitempty" yaml:"moduleInfraRequirements,omitempty"`
}

// PluginCapabilities describes what module, step, and trigger types a plugin provides.
type PluginCapabilities struct {
	ModuleTypes  []string `json:"moduleTypes" yaml:"moduleTypes"`
	StepTypes    []string `json:"stepTypes" yaml:"stepTypes"`
	TriggerTypes []string `json:"triggerTypes" yaml:"triggerTypes"`
}
