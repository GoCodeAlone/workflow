package module

import (
	"github.com/GoCodeAlone/workflow/pipeline"
)

// ExprEngine evaluates ${ ... } expressions against a PipelineContext.
// Aliased from pipeline.ExprEngine for backwards compatibility.
type ExprEngine = pipeline.ExprEngine

// NewExprEngine creates a new ExprEngine.
// Delegates to pipeline.NewExprEngine.
var NewExprEngine = pipeline.NewExprEngine
