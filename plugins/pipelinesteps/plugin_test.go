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
		"step.event_publish",
		"step.http_call",
		"step.request_parse",
		"step.db_query",
		"step.db_exec",
		"step.json_response",
		"step.raw_response",
		"step.validate_path_param",
		"step.validate_pagination",
		"step.validate_request_body",
		"step.foreach",
		"step.webhook_verify",
		"step.cache_get",
		"step.cache_set",
		"step.cache_delete",
		"step.ui_scaffold",
		"step.ui_scaffold_analyze",
		"step.dlq_send",
		"step.dlq_replay",
		"step.retry_with_backoff",
		"step.resilient_circuit_breaker",
		"step.auth_validate",
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
	if len(steps) != 29 {
		t.Fatalf("expected 29 step factories after load, got %d", len(steps))
	}
}
