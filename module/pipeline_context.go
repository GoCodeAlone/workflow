package module

import "maps"

// PipelineContext carries data through a pipeline execution.
type PipelineContext struct {
	// TriggerData is the original data from the trigger (immutable after creation).
	TriggerData map[string]any

	// StepOutputs maps step-name -> output from each completed step.
	StepOutputs map[string]map[string]any

	// Current is the merged state: trigger data + all step outputs.
	// Steps read from Current and their output is merged back into it.
	Current map[string]any

	// Metadata holds execution metadata (pipeline name, trace ID, etc.)
	Metadata map[string]any
}

// NewPipelineContext creates a PipelineContext initialized with trigger data.
func NewPipelineContext(triggerData map[string]any, metadata map[string]any) *PipelineContext {
	current := make(map[string]any)
	if triggerData != nil {
		maps.Copy(current, triggerData)
	}

	td := make(map[string]any)
	if triggerData != nil {
		maps.Copy(td, triggerData)
	}

	md := make(map[string]any)
	if metadata != nil {
		maps.Copy(md, metadata)
	}

	return &PipelineContext{
		TriggerData: td,
		StepOutputs: make(map[string]map[string]any),
		Current:     current,
		Metadata:    md,
	}
}

// MergeStepOutput records a step's output and merges it into Current.
func (pc *PipelineContext) MergeStepOutput(stepName string, output map[string]any) {
	if output == nil {
		return
	}

	// Store the step output under its name
	stepOut := make(map[string]any)
	maps.Copy(stepOut, output)
	pc.StepOutputs[stepName] = stepOut

	// Merge into Current
	maps.Copy(pc.Current, output)
}

// StepResult is the output of a single pipeline step execution.
type StepResult struct {
	// Output is the data produced by this step.
	Output map[string]any

	// NextStep overrides the default next step (for conditional routing).
	// Empty string means continue to the next step in sequence.
	NextStep string

	// Stop indicates the pipeline should stop after this step (success).
	Stop bool
}
