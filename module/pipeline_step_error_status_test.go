package module

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fixedErrorStep is a test step that always returns a configured error.
type fixedErrorStep struct {
	name string
	err  error
}

func (s *fixedErrorStep) Name() string { return s.name }
func (s *fixedErrorStep) Execute(_ context.Context, _ *PipelineContext) (*interfaces.StepResult, error) {
	return nil, s.err
}

// fixedSuccessStep is a test step that always succeeds.
type fixedSuccessStep struct {
	name string
}

func (s *fixedSuccessStep) Name() string { return s.name }
func (s *fixedSuccessStep) Execute(_ context.Context, _ *PipelineContext) (*interfaces.StepResult, error) {
	return &interfaces.StepResult{Output: map[string]any{"ok": true}}, nil
}

// TestErrorStatusStep_WrapsErrorWithValidationError verifies that a plain error
// returned by the inner step is wrapped in a ValidationError with the configured
// HTTP status code.
func TestErrorStatusStep_WrapsErrorWithValidationError(t *testing.T) {
	inner := &fixedErrorStep{name: "validate", err: errors.New("card doesn't match")}
	step := NewErrorStatusStep(inner, 400)

	_, err := step.Execute(context.Background(), &PipelineContext{
		Current:  map[string]any{},
		Metadata: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !interfaces.IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if got := interfaces.ValidationErrorStatus(err); got != 400 {
		t.Errorf("expected status 400, got %d", got)
	}
	if err.Error() != "card doesn't match" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

// TestErrorStatusStep_PassesThroughExistingValidationError verifies that a
// ValidationError already set by the inner step is not double-wrapped.
func TestErrorStatusStep_PassesThroughExistingValidationError(t *testing.T) {
	inner := &fixedErrorStep{
		name: "validate",
		err:  interfaces.NewValidationError("already a validation error", 422),
	}
	step := NewErrorStatusStep(inner, 400)

	_, err := step.Execute(context.Background(), &PipelineContext{
		Current:  map[string]any{},
		Metadata: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should keep the original 422, not override with 400
	if got := interfaces.ValidationErrorStatus(err); got != 422 {
		t.Errorf("expected original status 422 preserved, got %d", got)
	}
}

// TestErrorStatusStep_SuccessPassesThrough verifies that a successful step
// result is returned unchanged.
func TestErrorStatusStep_SuccessPassesThrough(t *testing.T) {
	inner := &fixedSuccessStep{name: "ok-step"}
	step := NewErrorStatusStep(inner, 400)

	result, err := step.Execute(context.Background(), &PipelineContext{
		Current:  map[string]any{},
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Output["ok"] != true {
		t.Errorf("expected successful result with ok=true, got %v", result)
	}
}

// TestStepConfig_ErrorStatus verifies that a pipeline with error_status on a
// step surfaces the configured 4xx status when the step fails.
func TestStepConfig_ErrorStatus(t *testing.T) {
	inner := &fixedErrorStep{name: "validate", err: errors.New("invalid input")}
	step := NewErrorStatusStep(inner, 400)

	p := &Pipeline{
		Name:    "test-pipeline",
		Steps:   []PipelineStep{step},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error from pipeline")
	}
	if !interfaces.IsValidationError(err) {
		t.Fatalf("expected ValidationError in pipeline error chain, got %T: %v", err, err)
	}
	if got := interfaces.ValidationErrorStatus(err); got != 400 {
		t.Errorf("expected status 400 from pipeline error, got %d", got)
	}
}
