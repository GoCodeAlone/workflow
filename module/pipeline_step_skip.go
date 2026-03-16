package module

import (
	"context"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// SkippableStep wraps a PipelineStep with optional skip_if / if guard expressions.
//
// skip_if: when the resolved template is truthy (non-empty, not "false", not "0"),
// the step is skipped. Falsy → execute.
//
// if: the logical inverse of skip_if. When the resolved template is truthy,
// the step executes. Falsy → skip.
//
// When both fields are set, skip_if takes precedence.
// When neither is set, the step always executes (backward compatible).
type SkippableStep struct {
	inner  interfaces.PipelineStep
	skipIf string // Go template; truthy result → skip
	ifExpr string // Go template; falsy result → skip
	tmpl   *TemplateEngine
}

// NewSkippableStep creates a SkippableStep wrapping inner.
// skipIf and ifExpr may be empty strings to disable the respective guard.
func NewSkippableStep(inner interfaces.PipelineStep, skipIf, ifExpr string) *SkippableStep {
	return &SkippableStep{
		inner:  inner,
		skipIf: skipIf,
		ifExpr: ifExpr,
		tmpl:   NewTemplateEngine(),
	}
}

// Name delegates to the wrapped step.
func (s *SkippableStep) Name() string {
	return s.inner.Name()
}

// Execute evaluates skip_if / if guards and either skips or delegates to the
// wrapped step.
func (s *SkippableStep) Execute(ctx context.Context, pc *PipelineContext) (*interfaces.StepResult, error) {
	// Evaluate skip_if (takes precedence when both are set)
	if s.skipIf != "" {
		val, err := s.tmpl.Resolve(s.skipIf, pc)
		if err != nil {
			// Template resolution errors are non-fatal: treat as falsy (execute).
			val = ""
		}
		if isTruthy(val) {
			return skippedResult("skip_if evaluated to true"), nil
		}
	}

	// Evaluate if (inverse logic: falsy → skip)
	if s.ifExpr != "" {
		val, err := s.tmpl.Resolve(s.ifExpr, pc)
		if err != nil {
			// Template resolution errors are non-fatal: treat as falsy (skip).
			val = ""
		}
		if !isTruthy(val) {
			return skippedResult("if evaluated to false"), nil
		}
	}

	return s.inner.Execute(ctx, pc)
}

// isTruthy returns true when the resolved template value should cause a
// skip_if guard to trigger (or an if guard to execute).
// Falsy values: empty string, "false", "0".
// Everything else is truthy.
func isTruthy(val string) bool {
	trimmed := strings.TrimSpace(val)
	switch trimmed {
	case "", "false", "0":
		return false
	default:
		return true
	}
}

// skippedResult builds the standard output for a step that was skipped by a guard.
func skippedResult(reason string) *interfaces.StepResult {
	return &interfaces.StepResult{
		Output: map[string]any{
			"skipped": true,
			"reason":  reason,
		},
	}
}
