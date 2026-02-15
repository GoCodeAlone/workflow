package module

import (
	"context"
	"strings"
	"testing"
)

// newTestRegistry returns a StepRegistry pre-loaded with the real built-in
// step factories (validate, set, log, conditional). No mocks.
func newTestRegistry() *StepRegistry {
	r := NewStepRegistry()
	r.Register("step.validate", NewValidateStepFactory())
	r.Register("step.set", NewSetStepFactory())
	r.Register("step.log", NewLogStepFactory())
	r.Register("step.conditional", NewConditionalStepFactory())
	return r
}

// buildStep is a helper that creates a real step via the registry.
func buildStep(t *testing.T, reg *StepRegistry, stepType, name string, cfg map[string]any) PipelineStep {
	t.Helper()
	step, err := reg.Create(stepType, name, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create %s step %q: %v", stepType, name, err)
	}
	return step
}

// --------------------------------------------------------------------------
// Integration tests
// --------------------------------------------------------------------------

func TestIntegration_ValidateSetLogPipeline(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: validate that "name" and "email" exist in trigger data
	validateStep := buildStep(t, reg, "step.validate", "check-input", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"name", "email"},
	})

	// Step 2: set a greeting derived from trigger data using templates
	setStep := buildStep(t, reg, "step.set", "build-greeting", map[string]any{
		"values": map[string]any{
			"greeting": "Hello, {{.name}}!",
			"status":   "processed",
		},
	})

	// Step 3: log a message that references the set step's output
	logStep := buildStep(t, reg, "step.log", "log-result", map[string]any{
		"level":   "info",
		"message": "Processed {{.name}} with status {{.status}}",
	})

	p := &Pipeline{
		Name:    "validate-set-log",
		Steps:   []PipelineStep{validateStep, setStep, logStep},
		OnError: ErrorStrategyStop,
	}

	triggerData := map[string]any{
		"name":  "Alice",
		"email": "alice@example.com",
	}

	pc, err := p.Execute(context.Background(), triggerData)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Validate outputs accumulated correctly
	if pc.Current["greeting"] != "Hello, Alice!" {
		t.Errorf("expected greeting 'Hello, Alice!', got %v", pc.Current["greeting"])
	}
	if pc.Current["status"] != "processed" {
		t.Errorf("expected status 'processed', got %v", pc.Current["status"])
	}

	// Original trigger data should still be accessible
	if pc.Current["name"] != "Alice" {
		t.Errorf("expected trigger data 'name' preserved, got %v", pc.Current["name"])
	}
	if pc.Current["email"] != "alice@example.com" {
		t.Errorf("expected trigger data 'email' preserved, got %v", pc.Current["email"])
	}

	// Step outputs should be recorded individually
	if _, ok := pc.StepOutputs["check-input"]; !ok {
		t.Error("expected StepOutputs to contain 'check-input'")
	}
	setOut := pc.StepOutputs["build-greeting"]
	if setOut["greeting"] != "Hello, Alice!" {
		t.Errorf("expected set step output 'greeting', got %v", setOut["greeting"])
	}
}

func TestIntegration_ValidateConditionalSetPipeline(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: validate that "order_type" exists
	validateStep := buildStep(t, reg, "step.validate", "check-type", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"order_type"},
	})

	// Step 2: conditional routes based on order_type.
	// Because the pipeline continues linearly after a conditional jump,
	// we place the branch targets at the end so only the matched branch
	// runs (there are no subsequent steps after each branch set).
	//
	// Layout: [validate] -> [conditional] -> [set-express or set-standard]
	// The conditional routes either to "set-express" (last step) or
	// "set-standard" (second-to-last step which then falls through to set-express).
	//
	// For a clean test we use two separate pipelines: one for express, one for standard.

	// --- Test express path ---
	t.Run("express route", func(t *testing.T) {
		condStep := buildStep(t, reg, "step.conditional", "route-order", map[string]any{
			"field": "order_type",
			"routes": map[string]any{
				"express":  "set-express",
				"standard": "set-standard",
			},
			"default": "set-standard",
		})

		setExpressStep := buildStep(t, reg, "step.set", "set-express", map[string]any{
			"values": map[string]any{
				"priority": "high",
				"sla_days": "1",
			},
		})

		// Express pipeline: validate -> conditional -> set-express
		// The conditional jumps directly to set-express, the last step.
		p := &Pipeline{
			Name:    "express-pipeline",
			Steps:   []PipelineStep{validateStep, condStep, setExpressStep},
			OnError: ErrorStrategyStop,
		}

		pc, err := p.Execute(context.Background(), map[string]any{"order_type": "express"})
		if err != nil {
			t.Fatalf("pipeline failed: %v", err)
		}

		if pc.Current["priority"] != "high" {
			t.Errorf("expected priority 'high', got %v", pc.Current["priority"])
		}
		if pc.Current["sla_days"] != "1" {
			t.Errorf("expected sla_days '1', got %v", pc.Current["sla_days"])
		}

		condOut := pc.StepOutputs["route-order"]
		if condOut["matched_value"] != "express" {
			t.Errorf("expected matched_value 'express', got %v", condOut["matched_value"])
		}
	})

	// --- Test standard path ---
	t.Run("standard route", func(t *testing.T) {
		condStep := buildStep(t, reg, "step.conditional", "route-order", map[string]any{
			"field": "order_type",
			"routes": map[string]any{
				"express":  "set-express",
				"standard": "set-standard",
			},
			"default": "set-standard",
		})

		setStandardStep := buildStep(t, reg, "step.set", "set-standard", map[string]any{
			"values": map[string]any{
				"priority": "normal",
				"sla_days": "5",
			},
		})

		// Standard pipeline: validate -> conditional -> set-standard
		p := &Pipeline{
			Name:    "standard-pipeline",
			Steps:   []PipelineStep{validateStep, condStep, setStandardStep},
			OnError: ErrorStrategyStop,
		}

		pc, err := p.Execute(context.Background(), map[string]any{"order_type": "standard"})
		if err != nil {
			t.Fatalf("pipeline failed: %v", err)
		}

		if pc.Current["priority"] != "normal" {
			t.Errorf("expected priority 'normal', got %v", pc.Current["priority"])
		}
		if pc.Current["sla_days"] != "5" {
			t.Errorf("expected sla_days '5', got %v", pc.Current["sla_days"])
		}
	})

	// --- Test default route (unknown order type falls through to standard) ---
	t.Run("default route", func(t *testing.T) {
		condStep := buildStep(t, reg, "step.conditional", "route-order", map[string]any{
			"field": "order_type",
			"routes": map[string]any{
				"express":  "set-express",
				"standard": "set-standard",
			},
			"default": "set-standard",
		})

		setStandardStep := buildStep(t, reg, "step.set", "set-standard", map[string]any{
			"values": map[string]any{
				"priority": "normal",
				"sla_days": "5",
			},
		})

		p := &Pipeline{
			Name:    "default-pipeline",
			Steps:   []PipelineStep{validateStep, condStep, setStandardStep},
			OnError: ErrorStrategyStop,
		}

		pc, err := p.Execute(context.Background(), map[string]any{"order_type": "bulk"})
		if err != nil {
			t.Fatalf("pipeline failed: %v", err)
		}

		if pc.Current["priority"] != "normal" {
			t.Errorf("expected default priority 'normal', got %v", pc.Current["priority"])
		}
		condOut := pc.StepOutputs["route-order"]
		if condOut["used_default"] != true {
			t.Errorf("expected used_default=true for unmatched order type")
		}
	})
}

func TestIntegration_TemplateResolvesStepOutputsBetweenSteps(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: set some values
	step1 := buildStep(t, reg, "step.set", "compute", map[string]any{
		"values": map[string]any{
			"first_name": "Jane",
			"last_name":  "Doe",
		},
	})

	// Step 2: use templates to reference step 1's output by name via "steps" map
	step2 := buildStep(t, reg, "step.set", "format", map[string]any{
		"values": map[string]any{
			"full_name": "{{.first_name}} {{.last_name}}",
			// Access via the steps map (step-specific outputs)
			"step_ref": "{{index .steps \"compute\" \"first_name\"}}",
		},
	})

	p := &Pipeline{
		Name:    "template-resolution",
		Steps:   []PipelineStep{step1, step2},
		OnError: ErrorStrategyStop,
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	if pc.Current["full_name"] != "Jane Doe" {
		t.Errorf("expected full_name 'Jane Doe', got %v", pc.Current["full_name"])
	}
	if pc.Current["step_ref"] != "Jane" {
		t.Errorf("expected step_ref 'Jane' (from steps.compute.first_name), got %v", pc.Current["step_ref"])
	}
}

func TestIntegration_ValidateFailsStopsPipeline(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: validate requires "user_id" but it won't be provided
	validateStep := buildStep(t, reg, "step.validate", "check-user", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"user_id", "action"},
	})

	// Step 2: should never run
	setStep := buildStep(t, reg, "step.set", "should-not-run", map[string]any{
		"values": map[string]any{"reached": "yes"},
	})

	p := &Pipeline{
		Name:    "validate-fails",
		Steps:   []PipelineStep{validateStep, setStep},
		OnError: ErrorStrategyStop,
	}

	// Trigger data is missing "user_id" and "action"
	_, err := p.Execute(context.Background(), map[string]any{"other": "data"})
	if err == nil {
		t.Fatal("expected pipeline to fail due to missing required fields")
	}

	if !strings.Contains(err.Error(), "missing required fields") {
		t.Errorf("expected 'missing required fields' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "user_id") {
		t.Errorf("expected 'user_id' in error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("expected 'action' in error message, got: %v", err)
	}
}

func TestIntegration_CompensationRunsOnStepFailure(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: set some data (succeeds)
	step1 := buildStep(t, reg, "step.set", "init-data", map[string]any{
		"values": map[string]any{"initialized": "true"},
	})

	// Step 2: validate a field that does not exist (fails)
	step2 := buildStep(t, reg, "step.validate", "check-missing", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"nonexistent_field"},
	})

	// Compensation step: log a rollback message
	compStep := buildStep(t, reg, "step.log", "rollback-log", map[string]any{
		"level":   "warn",
		"message": "Rolling back due to failure",
	})

	// Another compensation step: set a rollback flag
	compSetStep := buildStep(t, reg, "step.set", "rollback-flag", map[string]any{
		"values": map[string]any{"rolled_back": "true"},
	})

	p := &Pipeline{
		Name:         "compensate-test",
		Steps:        []PipelineStep{step1, step2},
		OnError:      ErrorStrategyCompensate,
		Compensation: []PipelineStep{compStep, compSetStep},
	}

	_, err := p.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error from pipeline with compensation")
	}

	// The error should reference the failing step and compensation
	if !strings.Contains(err.Error(), "check-missing") {
		t.Errorf("expected failing step name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "compensation executed") {
		t.Errorf("expected 'compensation executed' in error, got: %v", err)
	}
}

func TestIntegration_JSONSchemaValidation(t *testing.T) {
	reg := newTestRegistry()

	// Validate with JSON schema strategy
	validateStep := buildStep(t, reg, "step.validate", "schema-check", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"required": []any{"name", "age"},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "number"},
			},
		},
	})

	setStep := buildStep(t, reg, "step.set", "mark-valid", map[string]any{
		"values": map[string]any{"validated": "true"},
	})

	p := &Pipeline{
		Name:    "schema-validate",
		Steps:   []PipelineStep{validateStep, setStep},
		OnError: ErrorStrategyStop,
	}

	t.Run("valid data passes", func(t *testing.T) {
		pc, err := p.Execute(context.Background(), map[string]any{
			"name": "Bob",
			"age":  float64(30),
		})
		if err != nil {
			t.Fatalf("expected valid data to pass: %v", err)
		}
		if pc.Current["validated"] != "true" {
			t.Error("expected set step to run after successful validation")
		}
	})

	t.Run("missing required field fails", func(t *testing.T) {
		_, err := p.Execute(context.Background(), map[string]any{
			"name": "Bob",
			// age is missing
		})
		if err == nil {
			t.Fatal("expected validation to fail for missing 'age'")
		}
		if !strings.Contains(err.Error(), "age") {
			t.Errorf("expected 'age' in error, got: %v", err)
		}
	})

	t.Run("wrong type fails", func(t *testing.T) {
		_, err := p.Execute(context.Background(), map[string]any{
			"name": 123, // should be string
			"age":  float64(30),
		})
		if err == nil {
			t.Fatal("expected validation to fail for wrong type")
		}
		if !strings.Contains(err.Error(), "expected string") {
			t.Errorf("expected type error in message, got: %v", err)
		}
	})
}

func TestIntegration_ConditionalWithNoMatchAndNoDefault(t *testing.T) {
	reg := newTestRegistry()

	condStep := buildStep(t, reg, "step.conditional", "route", map[string]any{
		"field": "status",
		"routes": map[string]any{
			"active":   "handle-active",
			"inactive": "handle-inactive",
		},
		// No default route
	})

	p := &Pipeline{
		Name:    "no-default-route",
		Steps:   []PipelineStep{condStep},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), map[string]any{"status": "unknown"})
	if err == nil {
		t.Fatal("expected error when conditional has no matching route and no default")
	}
	if !strings.Contains(err.Error(), "not found in routes") {
		t.Errorf("expected 'not found in routes' in error, got: %v", err)
	}
}

func TestIntegration_SkipStrategyWithRealSteps(t *testing.T) {
	reg := newTestRegistry()

	// Step 1: set some initial data
	step1 := buildStep(t, reg, "step.set", "init", map[string]any{
		"values": map[string]any{"started": "true"},
	})

	// Step 2: validate will fail (requires missing field)
	step2 := buildStep(t, reg, "step.validate", "bad-check", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"does_not_exist"},
	})

	// Step 3: set should still run because on_error=skip
	step3 := buildStep(t, reg, "step.set", "after-skip", map[string]any{
		"values": map[string]any{"completed": "true"},
	})

	p := &Pipeline{
		Name:    "skip-integration",
		Steps:   []PipelineStep{step1, step2, step3},
		OnError: ErrorStrategySkip,
	}

	pc, err := p.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("expected no error with skip strategy, got: %v", err)
	}

	if pc.Current["started"] != "true" {
		t.Error("expected 'started' from step1")
	}
	if pc.Current["completed"] != "true" {
		t.Error("expected 'completed' from step3 after skipping step2")
	}

	// The skipped step should have error metadata
	step2Out := pc.StepOutputs["bad-check"]
	if step2Out["_skipped"] != true {
		t.Error("expected _skipped=true for failed validate step")
	}
}
