package platform

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "platform" {
		t.Fatalf("expected name platform, got %s", p.Name())
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
		"step.platform_template",
		"step.k8s_plan",
		"step.k8s_apply",
		"step.k8s_status",
		"step.k8s_destroy",
		"step.ecs_plan",
		"step.ecs_apply",
		"step.ecs_status",
		"step.ecs_destroy",
		"step.iac_plan",
		"step.iac_apply",
		"step.iac_status",
		"step.iac_destroy",
		"step.iac_drift_detect",
		"step.dns_plan",
		"step.dns_apply",
		"step.dns_status",
		"step.network_plan",
		"step.network_apply",
		"step.network_status",
		"step.apigw_plan",
		"step.apigw_apply",
		"step.apigw_status",
		"step.apigw_destroy",
		"step.scaling_plan",
		"step.scaling_apply",
		"step.scaling_status",
		"step.scaling_destroy",
		"step.app_deploy",
		"step.app_status",
		"step.app_rollback",
		"step.region_deploy",
		"step.region_promote",
		"step.region_failover",
		"step.region_status",
		"step.region_weight",
		"step.region_sync",
		"step.argo_submit",
		"step.argo_status",
		"step.argo_logs",
		"step.argo_delete",
		"step.argo_list",
		"step.do_deploy",
		"step.do_status",
		"step.do_logs",
		"step.do_scale",
		"step.do_destroy",
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

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedModules := []string{
		"platform.kubernetes",
		"platform.ecs",
		"platform.dns",
		"platform.networking",
		"platform.apigateway",
		"platform.autoscaling",
		"iac.state",
		"platform.provider",
		"platform.resource",
		"platform.context",
		"app.container",
		"platform.region",
		"argo.workflows",
		"platform.doks",
		"platform.do_networking",
		"platform.do_dns",
		"platform.do_app",
		"platform.do_database",
		"platform.region_router",
	}

	for _, modType := range expectedModules {
		if _, ok := factories[modType]; !ok {
			t.Errorf("missing module factory: %s", modType)
		}
	}

	if len(factories) != len(expectedModules) {
		t.Errorf("expected %d module factories, got %d", len(expectedModules), len(factories))
	}
}

func TestTriggerFactories(t *testing.T) {
	p := New()
	triggers := p.TriggerFactories()

	if _, ok := triggers["reconciliation"]; !ok {
		t.Error("missing trigger factory: reconciliation")
	}

	if len(triggers) != 1 {
		t.Errorf("expected 1 trigger factory, got %d", len(triggers))
	}
}

func TestWorkflowHandlers(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()

	if _, ok := handlers["platform"]; !ok {
		t.Error("missing workflow handler: platform")
	}

	if len(handlers) != 1 {
		t.Errorf("expected 1 workflow handler, got %d", len(handlers))
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	steps := loader.StepFactories()
	if len(steps) != len(p.StepFactories()) {
		t.Fatalf("expected %d step factories after load, got %d", len(p.StepFactories()), len(steps))
	}
}
