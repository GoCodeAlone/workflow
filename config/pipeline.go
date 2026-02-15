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
}
