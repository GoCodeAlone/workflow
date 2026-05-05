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

// PluginCapabilities describes what module, step, trigger types, build hooks,
// and CLI commands a plugin provides.
type PluginCapabilities struct {
	ModuleTypes   []string               `json:"moduleTypes" yaml:"moduleTypes"`
	StepTypes     []string               `json:"stepTypes" yaml:"stepTypes"`
	TriggerTypes  []string               `json:"triggerTypes" yaml:"triggerTypes"`
	BuildHooks    []BuildHookDeclaration `json:"buildHooks,omitempty" yaml:"buildHooks,omitempty"`
	OnHookFailure string                 `json:"onHookFailure,omitempty" yaml:"onHookFailure,omitempty"` // fail | warn | skip
	// PortPaths is a list of dot-notation JSON paths into module config that
	// contain port values (e.g. ["config.api_port", "config.grpc_port"]).
	// The port introspector walks these paths for modules of any type declared by this plugin.
	PortPaths   []string                `json:"portPaths,omitempty" yaml:"portPaths,omitempty"`
	CLICommands []CLICommandDeclaration `json:"cliCommands,omitempty" yaml:"cliCommands,omitempty"`
}

// BuildHookDeclaration registers a plugin as a handler for a specific hook event.
type BuildHookDeclaration struct {
	Event          string `json:"event" yaml:"event"`
	Priority       int    `json:"priority" yaml:"priority"` // lower = runs first
	Description    string `json:"description,omitempty" yaml:"description,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"` // 0 = use global default
}

// CLICommandDeclaration registers a plugin as the handler for a top-level wfctl subcommand.
type CLICommandDeclaration struct {
	Name             string `json:"name" yaml:"name"`
	Description      string `json:"description,omitempty" yaml:"description,omitempty"`
	FlagsPassthrough bool   `json:"flagsPassthrough,omitempty" yaml:"flagsPassthrough,omitempty"`
}
