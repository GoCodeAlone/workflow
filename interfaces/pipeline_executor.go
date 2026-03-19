package interfaces

import "context"

// PipelineExecutor provides direct pipeline invocation for Go callers
// (gRPC servers, tests, etc.) without HTTP serialization overhead.
// *workflow.StdEngine satisfies this interface.
type PipelineExecutor interface {
	// ExecutePipeline runs a named pipeline synchronously and returns its
	// structured output. Returns _pipeline_output if set by
	// step.pipeline_output, otherwise the pipeline's merged Current state.
	ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error)
}
