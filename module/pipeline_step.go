package module

import "context"

// PipelineStep is a single composable unit of work in a pipeline.
type PipelineStep interface {
	// Name returns the step's unique name within the pipeline.
	Name() string

	// Execute runs the step with the pipeline context.
	// It receives accumulated data from previous steps and returns
	// its own output to be merged into the context.
	Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error)
}
