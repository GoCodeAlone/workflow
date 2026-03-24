package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestEngine_TemplateValidation_WarnMode verifies that with the default "warn" mode
// (no Engine config set), validation warnings are logged but BuildFromConfig succeeds.
func TestEngine_TemplateValidation_WarnMode(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.logger)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		// Pipeline with a dangling step reference — should warn but not fail.
		Pipelines: map[string]any{
			"test-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "step-a",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.nonexistent.value }}",
							},
						},
					},
				},
			},
		},
		// No Engine config → default mode is "warn".
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig should not fail in default warn mode, got: %v", err)
	}

	// Confirm a warning was logged about the missing step reference.
	found := false
	for _, entry := range app.logger.logs {
		if strings.Contains(entry, "nonexistent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning log about 'nonexistent' step, log entries: %v", app.logger.logs)
	}
}

// TestEngine_TemplateValidation_ExplicitWarnMode verifies that explicitly setting
// engine.validation.templateRefs to "warn" behaves the same as the default.
func TestEngine_TemplateValidation_ExplicitWarnMode(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.logger)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Pipelines: map[string]any{
			"test-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "step-a",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.missing.value }}",
							},
						},
					},
				},
			},
		},
		Engine: &config.EngineConfig{
			Validation: &config.EngineValidationConfig{
				TemplateRefs: "warn",
			},
		},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig should not fail in warn mode, got: %v", err)
	}
}

// TestEngine_TemplateValidation_ErrorMode verifies that with mode "error", a config
// with a dangling step reference causes BuildFromConfig to return an error.
func TestEngine_TemplateValidation_ErrorMode(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.logger)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Pipelines: map[string]any{
			"test-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "step-a",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.nonexistent.value }}",
							},
						},
					},
				},
			},
		},
		Engine: &config.EngineConfig{
			Validation: &config.EngineValidationConfig{
				TemplateRefs: "error",
			},
		},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("BuildFromConfig should return an error in error mode for broken pipeline")
	}
	if !strings.Contains(err.Error(), "pipeline template validation failed") {
		t.Errorf("error should mention template validation, got: %v", err)
	}
}

// TestEngine_TemplateValidation_OffMode verifies that with mode "off", no validation
// is performed and no warnings are logged even for broken pipelines.
func TestEngine_TemplateValidation_OffMode(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.logger)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Pipelines: map[string]any{
			"test-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "step-a",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.nonexistent.value }}",
							},
						},
					},
				},
			},
		},
		Engine: &config.EngineConfig{
			Validation: &config.EngineValidationConfig{
				TemplateRefs: "off",
			},
		},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig should not fail in off mode, got: %v", err)
	}
	for _, entry := range app.logger.logs {
		if strings.Contains(entry, "nonexistent") {
			t.Errorf("expected no log about 'nonexistent' in off mode, got: %s", entry)
		}
	}
}

// TestEngine_TemplateValidation_ValidPipeline verifies that a valid pipeline with
// correct step references does not produce warnings or errors.
func TestEngine_TemplateValidation_ValidPipeline(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.logger)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Pipelines: map[string]any{
			"test-pipeline": map[string]any{
				"steps": []any{
					map[string]any{
						"name":   "first",
						"type":   "step.set",
						"config": map[string]any{"values": map[string]any{"val": "hello"}},
					},
					map[string]any{
						"name": "second",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"copy": "{{ .steps.first.val }}",
							},
						},
					},
				},
			},
		},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed for valid pipeline: %v", err)
	}
}
