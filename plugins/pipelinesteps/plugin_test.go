package pipelinesteps

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "pipeline-steps" {
		t.Fatalf("expected name pipeline-steps, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	factories := p.StepFactories()

	expectedSteps := []string{
		"step.validate",
		"step.transform",
		"step.conditional",
		"step.set",
		"step.log",
		"step.delegate",
		"step.jq",
		"step.publish",
		"step.http_call",
		"step.request_parse",
		"step.db_query",
		"step.db_exec",
		"step.json_response",
		"step.validate_path_param",
		"step.validate_pagination",
		"step.validate_request_body",
	}

	for _, stepType := range expectedSteps {
		if _, ok := factories[stepType]; !ok {
			t.Errorf("missing step factory: %s", stepType)
		}
	}

	if len(factories) != len(expectedSteps) {
		t.Errorf("expected %d step factories, got %d", len(expectedSteps), len(factories))
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	steps := loader.StepFactories()
	if len(steps) != 16 {
		t.Fatalf("expected 16 step factories after load, got %d", len(steps))
	}
}
