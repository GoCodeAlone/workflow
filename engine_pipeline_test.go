package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// setupPipelineEngine creates an engine with the PipelineWorkflowHandler
// registered and step types pre-loaded.
func setupPipelineEngine(t *testing.T) (*StdEngine, *handlers.PipelineWorkflowHandler) {
	t.Helper()

	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register built-in step types
	engine.AddStepType("step.validate", module.NewValidateStepFactory())
	engine.AddStepType("step.set", module.NewSetStepFactory())
	engine.AddStepType("step.log", module.NewLogStepFactory())
	engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
	engine.AddStepType("step.delegate", module.NewDelegateStepFactory())

	// Register the pipeline handler
	pipelineHandler := handlers.NewPipelineWorkflowHandler()
	engine.RegisterWorkflowHandler(pipelineHandler)

	return engine, pipelineHandler
}

func TestPipeline_ConfigurePipelines_SimplePipeline(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)
	_ = engine

	pipelineCfg := map[string]any{
		"hello-world": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "greet",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"message": "hello",
						},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	// The pipeline should be registered with the handler
	if !pipelineHandler.CanHandle("hello-world") {
		t.Error("expected handler to recognize 'hello-world' pipeline")
	}
	if !pipelineHandler.CanHandle("pipeline:hello-world") {
		t.Error("expected handler to recognize 'pipeline:hello-world'")
	}
}

func TestPipeline_ConfigurePipelines_CreatesStepsFromRegistry(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"multi-step": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "validate-input",
					"type": "step.validate",
					"config": map[string]any{
						"strategy":        "required_fields",
						"required_fields": []any{"name"},
					},
				},
				map[string]any{
					"name": "set-greeting",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"greeting": "Hello!",
						},
					},
				},
				map[string]any{
					"name": "log-output",
					"type": "step.log",
					"config": map[string]any{
						"level":   "info",
						"message": "Done",
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	if !pipelineHandler.CanHandle("multi-step") {
		t.Error("expected handler to recognize 'multi-step' pipeline")
	}

	// Verify the pipeline actually works by executing it through the handler
	ctx := context.Background()
	result, err := pipelineHandler.ExecuteWorkflow(
		ctx,
		"multi-step",
		"",
		map[string]any{"name": "Test"},
	)
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	if result["greeting"] != "Hello!" {
		t.Errorf("expected greeting 'Hello!', got %v", result["greeting"])
	}
}

func TestPipeline_ConfigurePipelines_InlineHTTPTrigger(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)

	// Register a mock trigger that responds to "http" type
	mt := &mockTrigger{
		name:       module.HTTPTriggerName,
		configType: "http",
	}
	engine.RegisterTrigger(mt)

	pipelineCfg := map[string]any{
		"api-pipeline": map[string]any{
			"trigger": map[string]any{
				"type": "http",
				"config": map[string]any{
					"method": "POST",
					"path":   "/api/orders",
				},
			},
			"steps": []any{
				map[string]any{
					"name": "set-ok",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"status": "ok"},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	// Pipeline should be registered
	if !pipelineHandler.CanHandle("api-pipeline") {
		t.Error("expected pipeline to be registered")
	}

	// Trigger should have been configured
	if !mt.configuredCalled {
		t.Error("expected HTTP trigger to be configured for the inline pipeline trigger")
	}
}

func TestPipeline_ConfigurePipelines_InlineEventTrigger(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)

	// Register a mock trigger that responds to "event" type
	mt := &mockTrigger{
		name:       module.EventTriggerName,
		configType: "event",
	}
	engine.RegisterTrigger(mt)

	pipelineCfg := map[string]any{
		"event-pipeline": map[string]any{
			"trigger": map[string]any{
				"type": "event",
				"config": map[string]any{
					"topic": "orders.created",
				},
			},
			"steps": []any{
				map[string]any{
					"name": "log-event",
					"type": "step.log",
					"config": map[string]any{
						"level":   "info",
						"message": "Event received",
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	if !pipelineHandler.CanHandle("event-pipeline") {
		t.Error("expected pipeline to be registered")
	}

	if !mt.configuredCalled {
		t.Error("expected event trigger to be configured")
	}
}

func TestPipeline_ConfigurePipelines_RejectsUnknownStepType(t *testing.T) {
	engine, _ := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"bad-pipeline": map[string]any{
			"steps": []any{
				map[string]any{
					"name":   "mystery",
					"type":   "step.nonexistent",
					"config": map[string]any{},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err == nil {
		t.Fatal("expected error for unknown step type")
	}

	if !strings.Contains(err.Error(), "unknown step type") {
		t.Errorf("expected 'unknown step type' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "step.nonexistent") {
		t.Errorf("expected step type name in error, got: %v", err)
	}
}

func TestPipeline_ConfigurePipelines_ErrorStrategy(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"skip-pipeline": map[string]any{
			"on_error": "skip",
			"steps": []any{
				map[string]any{
					"name": "validate-missing",
					"type": "step.validate",
					"config": map[string]any{
						"strategy":        "required_fields",
						"required_fields": []any{"nonexistent"},
					},
				},
				map[string]any{
					"name": "after-skip",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"reached": "true"},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	// Execute the pipeline: the validate step will fail but should be skipped
	ctx := context.Background()
	result, err := pipelineHandler.ExecuteWorkflow(ctx, "skip-pipeline", "", map[string]any{})
	if err != nil {
		t.Fatalf("expected skip strategy to allow pipeline to complete, got: %v", err)
	}
	if result["reached"] != "true" {
		t.Error("expected step after skipped failure to execute")
	}
}

func TestPipeline_ConfigurePipelines_WithCompensation(t *testing.T) {
	engine, pipelineHandler := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"comp-pipeline": map[string]any{
			"on_error": "compensate",
			"steps": []any{
				map[string]any{
					"name": "will-fail",
					"type": "step.validate",
					"config": map[string]any{
						"strategy":        "required_fields",
						"required_fields": []any{"missing_field"},
					},
				},
			},
			"compensation": []any{
				map[string]any{
					"name": "comp-log",
					"type": "step.log",
					"config": map[string]any{
						"level":   "warn",
						"message": "Compensating",
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}

	if !pipelineHandler.CanHandle("comp-pipeline") {
		t.Error("expected pipeline to be registered")
	}

	// Execute and expect failure with compensation
	ctx := context.Background()
	_, err = pipelineHandler.ExecuteWorkflow(ctx, "comp-pipeline", "", map[string]any{})
	if err == nil {
		t.Fatal("expected error from compensating pipeline")
	}
	if !strings.Contains(err.Error(), "compensation executed") {
		t.Errorf("expected 'compensation executed' in error, got: %v", err)
	}
}

func TestPipeline_ConfigurePipelines_WithTimeout(t *testing.T) {
	engine, _ := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"timed-pipeline": map[string]any{
			"timeout": "5s",
			"steps": []any{
				map[string]any{
					"name": "quick",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"ok": "true"},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err != nil {
		t.Fatalf("configurePipelines failed: %v", err)
	}
}

func TestPipeline_ConfigurePipelines_InvalidTimeout(t *testing.T) {
	engine, _ := setupPipelineEngine(t)

	pipelineCfg := map[string]any{
		"bad-timeout": map[string]any{
			"timeout": "not-a-duration",
			"steps": []any{
				map[string]any{
					"name": "step1",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"ok": "true"},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected 'invalid timeout' in error, got: %v", err)
	}
}

func TestPipeline_ConfigurePipelines_NoPipelineHandler(t *testing.T) {
	// Create an engine without registering a PipelineWorkflowHandler
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	pipelineCfg := map[string]any{
		"orphan-pipeline": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "step1",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"ok": "true"},
					},
				},
			},
		},
	}

	err := engine.configurePipelines(pipelineCfg)
	if err == nil {
		t.Fatal("expected error when no PipelineWorkflowHandler is registered")
	}
	if !strings.Contains(err.Error(), "no PipelineWorkflowHandler") {
		t.Errorf("expected 'no PipelineWorkflowHandler' in error, got: %v", err)
	}
}
