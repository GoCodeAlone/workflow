package module

import "github.com/GoCodeAlone/workflow/interfaces"

// PipelineContext carries data through a pipeline execution.
// Aliased from interfaces.PipelineContext for backwards compatibility.
type PipelineContext = interfaces.PipelineContext

// StepResult is the output of a single pipeline step execution.
// Aliased from interfaces.StepResult for backwards compatibility.
type StepResult = interfaces.StepResult

// NewPipelineContext creates a PipelineContext initialized with trigger data.
// Delegates to interfaces.NewPipelineContext.
var NewPipelineContext = interfaces.NewPipelineContext
