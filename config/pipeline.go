package config

// PipelineConfig represents a single composable pipeline definition.
type PipelineConfig struct {
	Trigger      PipelineTriggerConfig `json:"trigger" yaml:"trigger"`
	Steps        []PipelineStepConfig  `json:"steps" yaml:"steps"`
	OnError      string                `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Timeout      string                `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Compensation []PipelineStepConfig  `json:"compensation,omitempty" yaml:"compensation,omitempty"`
}

// PipelineTriggerConfig defines what starts a pipeline.
type PipelineTriggerConfig struct {
	Type   string         `json:"type" yaml:"type"`
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// PipelineStepConfig defines a single step in a pipeline.
type PipelineStepConfig struct {
	Name    string         `json:"name" yaml:"name"`
	Type    string         `json:"type" yaml:"type"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	OnError string         `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Timeout string         `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// SkipIf is an optional Go template expression. When it evaluates to a
	// truthy value (non-empty, not "false", not "0"), the step is skipped and
	// the pipeline continues with the next step. Falsy or absent → execute.
	SkipIf string `json:"skip_if,omitempty" yaml:"skip_if,omitempty"`
	// If is the logical inverse of SkipIf: the step executes only when the
	// template evaluates to truthy. A present `if` that resolves to a falsy
	// value (empty, "false", or "0") skips the step.
	// When both SkipIf and If are set, SkipIf takes precedence.
	If string `json:"if,omitempty" yaml:"if,omitempty"`
}
