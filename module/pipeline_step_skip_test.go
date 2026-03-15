package module

import (
	"context"
	"testing"
)

// TestStepSkipIf_SkipsWhenTrue verifies that a step with skip_if evaluating to
// a truthy string is not executed.
func TestStepSkipIf_SkipsWhenTrue(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	wrapped := NewSkippableStep(inner, "{{ if true }}true{{ end }}", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inner step should NOT have executed
	if len(inner.execLog) != 0 {
		t.Errorf("inner step should not execute when skip_if is truthy, but executed %d time(s)", len(inner.execLog))
	}

	// Result should indicate skipped
	if result == nil {
		t.Fatal("expected non-nil result for skipped step")
	}
	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true in output, got %v", result.Output["_skipped"])
	}
}

// TestStepSkipIf_ExecutesWhenFalse verifies that a step with skip_if evaluating
// to an empty/falsy string is executed normally.
func TestStepSkipIf_ExecutesWhenFalse(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// Template produces empty string → falsy → do NOT skip → execute
	wrapped := NewSkippableStep(inner, "{{ if false }}true{{ end }}", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inner step SHOULD have executed
	if len(inner.execLog) != 1 {
		t.Errorf("expected inner step to execute once, got %d", len(inner.execLog))
	}

	if result == nil || result.Output["ran"] != true {
		t.Errorf("expected inner step output, got %v", result)
	}
}

// TestStepSkipIf_OutputContainsSkippedFlag verifies the output of a skipped step.
func TestStepSkipIf_OutputContainsSkippedFlag(t *testing.T) {
	inner := newMockStep("inner", map[string]any{})
	wrapped := NewSkippableStep(inner, "true", "") // literal "true"

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true, got %v", result.Output["_skipped"])
	}
	if result.Output["_error"] == nil || result.Output["_error"] == "" {
		t.Errorf("expected non-empty _error in output, got %v", result.Output["_error"])
	}
}

// TestStepIf_ExecutesWhenTrue verifies that a step with `if` evaluating to a
// non-empty string is executed.
func TestStepIf_ExecutesWhenTrue(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// if="some-value" → truthy → execute
	wrapped := NewSkippableStep(inner, "", "some-value")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 1 {
		t.Errorf("expected inner step to execute once, got %d", len(inner.execLog))
	}
	if result == nil || result.Output["ran"] != true {
		t.Errorf("expected inner step output")
	}
}

// TestStepIf_SkipsWhenFalse verifies that a step with `if` evaluating to empty
// is skipped.
func TestStepIf_SkipsWhenFalse(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// if="" → falsy → skip
	wrapped := NewSkippableStep(inner, "", "{{ if false }}yes{{ end }}")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 0 {
		t.Errorf("inner step should not execute when `if` is falsy")
	}
	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true, got %v", result.Output["_skipped"])
	}
}

// TestStepSkipIf_TemplateResolution verifies that skip_if templates have access
// to step outputs and current context.
func TestStepSkipIf_TemplateResolution(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// skip_if reads from current context — if feature_flag == "off", skip
	wrapped := NewSkippableStep(inner, `{{ if eq .feature_flag "off" }}true{{ end }}`, "")

	// feature_flag=off → skip_if = "true" → skip
	pc := NewPipelineContext(map[string]any{"feature_flag": "off"}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 0 {
		t.Errorf("expected inner step to be skipped when feature_flag=off")
	}
	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true in output")
	}

	// Now with feature_flag=on → skip_if = "" → execute
	inner2 := newMockStep("inner2", map[string]any{"ran": true})
	wrapped2 := NewSkippableStep(inner2, `{{ if eq .feature_flag "off" }}true{{ end }}`, "")

	pc2 := NewPipelineContext(map[string]any{"feature_flag": "on"}, nil)
	result2, err := wrapped2.Execute(context.Background(), pc2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner2.execLog) != 1 {
		t.Errorf("expected inner2 step to execute when feature_flag=on")
	}
	if result2.Output["ran"] != true {
		t.Errorf("expected inner step output when not skipped")
	}
}

// TestStepSkipIf_TemplateAccessesStepOutputs verifies that skip_if can read
// outputs from previously-executed steps.
func TestStepSkipIf_TemplateAccessesStepOutputs(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// skip_if reads from a previous step's output
	wrapped := NewSkippableStep(inner, `{{ index .steps "check" "should_skip" }}`, "")

	pc := NewPipelineContext(map[string]any{}, nil)
	pc.MergeStepOutput("check", map[string]any{"should_skip": "true"})

	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 0 {
		t.Errorf("expected step to be skipped based on previous step output")
	}
	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true")
	}
}

// TestStepSkipIf_AbsentMeansExecute verifies that steps without skip_if or if
// always execute (backward compatibility).
func TestStepSkipIf_AbsentMeansExecute(t *testing.T) {
	inner := newMockStep("plain", map[string]any{"value": 42})
	// No skip_if, no if → always execute
	wrapped := NewSkippableStep(inner, "", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 1 {
		t.Errorf("expected inner step to execute once, got %d", len(inner.execLog))
	}
	if result == nil || result.Output["value"] != 42 {
		t.Errorf("expected inner step output, got %v", result)
	}
}

// TestStepSkipIf_FalseStringIsFalsy verifies that the string "false" is treated
// as falsy (do not skip).
func TestStepSkipIf_FalseStringIsFalsy(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	wrapped := NewSkippableStep(inner, "false", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 1 {
		t.Errorf("expected inner step to execute when skip_if='false', got %d executions", len(inner.execLog))
	}
}

// TestStepSkipIf_ZeroStringIsFalsy verifies that the string "0" is treated as
// falsy (do not skip).
func TestStepSkipIf_ZeroStringIsFalsy(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	wrapped := NewSkippableStep(inner, "0", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 1 {
		t.Errorf("expected inner step to execute when skip_if='0', got %d executions", len(inner.execLog))
	}
}

// TestSkippableStep_Name verifies that the wrapper delegates Name() to the inner step.
func TestSkippableStep_Name(t *testing.T) {
	inner := newMockStep("my-step", map[string]any{})
	wrapped := NewSkippableStep(inner, "true", "")

	if wrapped.Name() != "my-step" {
		t.Errorf("expected name 'my-step', got %q", wrapped.Name())
	}
}

// TestStepSkipIf_BothFieldsSet_SkipIfTakesPrecedence verifies that when both
// skip_if and if are set, skip_if takes precedence.
func TestStepSkipIf_BothFieldsSet_SkipIfTakesPrecedence(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// skip_if=true AND if=true → skip (skip_if wins)
	wrapped := NewSkippableStep(inner, "true", "also-true")

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := wrapped.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.execLog) != 0 {
		t.Errorf("expected step to be skipped when skip_if=true")
	}
	if result.Output["_skipped"] != true {
		t.Errorf("expected _skipped=true")
	}
}

// TestStepSkipIf_TemplateError_ReturnsError verifies that a broken skip_if
// template returns an error (fail closed) rather than silently changing control flow.
func TestStepSkipIf_TemplateError_ReturnsError(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// Invalid template syntax → should return an error
	wrapped := NewSkippableStep(inner, "{{ .nonexistent | badFunc }}", "")

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err := wrapped.Execute(context.Background(), pc)
	if err == nil {
		t.Error("expected error from broken skip_if template, got nil")
	}

	// Inner step must NOT have executed
	if len(inner.execLog) != 0 {
		t.Errorf("inner step should not execute when skip_if template errors")
	}
}

// TestStepIf_TemplateError_ReturnsError verifies that a broken `if` template
// returns an error (fail closed) rather than silently skipping the step.
func TestStepIf_TemplateError_ReturnsError(t *testing.T) {
	inner := newMockStep("inner", map[string]any{"ran": true})
	// Invalid template syntax → should return an error
	wrapped := NewSkippableStep(inner, "", "{{ .nonexistent | badFunc }}")

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err := wrapped.Execute(context.Background(), pc)
	if err == nil {
		t.Error("expected error from broken `if` template, got nil")
	}

	// Inner step must NOT have executed
	if len(inner.execLog) != 0 {
		t.Errorf("inner step should not execute when `if` template errors")
	}
}
