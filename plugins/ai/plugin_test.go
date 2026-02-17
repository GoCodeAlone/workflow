package ai

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	pluginPkg "github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "ai-plugin" {
		t.Fatalf("expected name ai-plugin, got %s", p.Name())
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

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	if _, ok := factories["dynamic.component"]; !ok {
		t.Error("missing module factory: dynamic.component")
	}
	if len(factories) != 1 {
		t.Errorf("expected 1 module factory, got %d", len(factories))
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	factories := p.StepFactories()

	expectedSteps := []string{
		"step.ai_complete",
		"step.ai_classify",
		"step.ai_extract",
		"step.sub_workflow",
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
	loader := pluginPkg.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	modules := loader.ModuleFactories()
	if len(modules) != 1 {
		t.Fatalf("expected 1 module factory after load, got %d", len(modules))
	}

	steps := loader.StepFactories()
	if len(steps) != 4 {
		t.Fatalf("expected 4 step factories after load, got %d", len(steps))
	}
}

func TestSetters(t *testing.T) {
	p := New()

	// These should not panic
	p.SetAIRegistry(nil)
	p.SetDynamicRegistry(nil)
	p.SetDynamicLoader(nil)
	p.SetWorkflowRegistry(nil)
}
