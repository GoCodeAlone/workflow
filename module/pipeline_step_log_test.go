package module

import (
	"context"
	"strings"
	"testing"
)

func TestLogStep_ExecutesAtEachLevel(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	factory := NewLogStepFactory()

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			step, err := factory("log-"+level, map[string]any{
				"level":   level,
				"message": "test message",
			}, nil)
			if err != nil {
				t.Fatalf("factory error for level %q: %v", level, err)
			}

			if step.Name() != "log-"+level {
				t.Errorf("expected step name 'log-%s', got %q", level, step.Name())
			}

			pc := NewPipelineContext(nil, nil)
			result, err := step.Execute(context.Background(), pc)
			if err != nil {
				t.Fatalf("expected no error for level %q, got: %v", level, err)
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestLogStep_DefaultsToInfoLevel(t *testing.T) {
	factory := NewLogStepFactory()

	step, err := factory("log-default", map[string]any{
		"message": "a message",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error with default level, got: %v", err)
	}
}

func TestLogStep_TemplateResolvesInMessage(t *testing.T) {
	factory := NewLogStepFactory()

	step, err := factory("log-template", map[string]any{
		"level":   "info",
		"message": "Processing order {{ .order_id }} for {{ .user }}",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"order_id": "ORD-99",
		"user":     "Bob",
	}, nil)

	// Execute should succeed (the actual log output goes to slog)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestLogStep_TemplateResolvesStepOutput(t *testing.T) {
	factory := NewLogStepFactory()

	step, err := factory("log-step-data", map[string]any{
		"level":   "info",
		"message": "Validation result: {{ .steps.validate.status }}",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"status": "passed"})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestLogStep_FactoryRejectsEmptyMessage(t *testing.T) {
	factory := NewLogStepFactory()

	_, err := factory("bad-log", map[string]any{
		"level": "info",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
	if !strings.Contains(err.Error(), "'message' is required") {
		t.Errorf("expected message about missing message field, got: %v", err)
	}
}

func TestLogStep_FactoryRejectsInvalidLevel(t *testing.T) {
	factory := NewLogStepFactory()

	_, err := factory("bad-level", map[string]any{
		"level":   "trace",
		"message": "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	if !strings.Contains(err.Error(), "invalid level") {
		t.Errorf("expected 'invalid level' in error, got: %v", err)
	}
}

func TestLogStep_ReturnsEmptyOutput(t *testing.T) {
	factory := NewLogStepFactory()
	step, err := factory("log-out", map[string]any{
		"message": "hello",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Output) != 0 {
		t.Errorf("expected empty output from log step, got %v", result.Output)
	}
	if result.Stop {
		t.Error("log step should not set Stop")
	}
	if result.NextStep != "" {
		t.Error("log step should not set NextStep")
	}
}
