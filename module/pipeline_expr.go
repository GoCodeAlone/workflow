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

// containsExpr reports whether s contains a ${ } expression block.
func containsExpr(s string) bool {
	return pipeline.ContainsExpr(s)
}

// resolveExprBlocks replaces all ${ ... } blocks in s by evaluating each
// expression against pc. Returns the substituted string or the first error.
func resolveExprBlocks(s string, pc *PipelineContext, ee *ExprEngine) (string, error) {
	return pipeline.ResolveExprBlocks(s, pc, ee)
}
