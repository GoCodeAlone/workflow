package module

import (
	"context"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ErrorStatusStep wraps a PipelineStep and converts any step error into a
// ValidationError with a configured HTTP status code. This allows YAML pipeline
// authors to declare that a step's failure is a client error (4xx), not a
// server error (500), by setting error_status on the step config.
type ErrorStatusStep struct {
	inner  interfaces.PipelineStep
	status int
}

// NewErrorStatusStep wraps inner so that any error it returns is wrapped in a
// ValidationError with the given HTTP status code.
func NewErrorStatusStep(inner interfaces.PipelineStep, status int) *ErrorStatusStep {
	return &ErrorStatusStep{inner: inner, status: status}
}

// Name delegates to the wrapped step.
func (s *ErrorStatusStep) Name() string {
	return s.inner.Name()
}

// Execute runs the wrapped step. If it returns an error that is not already a
// ValidationError, the error is wrapped in one with the configured status code.
func (s *ErrorStatusStep) Execute(ctx context.Context, pc *PipelineContext) (*interfaces.StepResult, error) {
	result, err := s.inner.Execute(ctx, pc)
	if err != nil {
		if !interfaces.IsValidationError(err) {
			return nil, interfaces.NewValidationError(err.Error(), s.status)
		}
		return nil, err
	}
	return result, nil
}
