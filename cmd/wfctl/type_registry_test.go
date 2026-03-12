package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugins/all"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestKnownModuleTypesPopulated(t *testing.T) {
	types := KnownModuleTypes()
	if len(types) == 0 {
		t.Fatal("expected known module types to be non-empty")
	}
	// Check some well-known types
	expected := []string{
		"storage.sqlite",
		"http.server",
		"http.router",
		"auth.jwt",
		"messaging.broker",
		"statemachine.engine",
		"metrics.collector",
		"health.checker",
		"cache.redis",
	}
	for _, e := range expected {
		if _, ok := types[e]; !ok {
			t.Errorf("expected module type %q to be in registry", e)
		}
	}
}

func TestKnownModuleTypesPluginField(t *testing.T) {
	types := KnownModuleTypes()
	for typeName, info := range types {
		if info.Plugin == "" {
			t.Errorf("module type %q has empty Plugin field", typeName)
		}
		if info.Type != typeName {
			t.Errorf("module type %q has mismatched Type field: %q", typeName, info.Type)
		}
	}
}

func TestKnownModuleTypesStateful(t *testing.T) {
	types := KnownModuleTypes()

	// These should be stateful
	statefulTypes := []string{"storage.sqlite", "database.workflow", "statemachine.engine", "auth.user-store"}
	for _, typeName := range statefulTypes {
		info, ok := types[typeName]
		if !ok {
			t.Errorf("module type %q not found", typeName)
			continue
		}
		if !info.Stateful {
			t.Errorf("expected module type %q to be stateful", typeName)
		}
	}

	// These should NOT be stateful
	nonStatefulTypes := []string{"http.server", "health.checker", "messaging.broker"}
	for _, typeName := range nonStatefulTypes {
		info, ok := types[typeName]
		if !ok {
			t.Errorf("module type %q not found", typeName)
			continue
		}
		if info.Stateful {
			t.Errorf("expected module type %q to NOT be stateful", typeName)
		}
	}
}

func TestKnownStepTypesPopulated(t *testing.T) {
	types := KnownStepTypes()
	if len(types) == 0 {
		t.Fatal("expected known step types to be non-empty")
	}
	expected := []string{
		// pipelinesteps plugin
		"step.validate",
		"step.transform",
		"step.json_response",
		"step.raw_response",
		"step.json_parse",
		"step.db_query",
		"step.publish",
		"step.http_call",
		"step.cache_get",
		"step.auth_validate",
		"step.authz_check",
		"step.hash",
		"step.regex_match",
		"step.parallel",
		"step.field_reencrypt",
		"step.token_revoke",
		"step.sandbox_exec",
		"step.ui_scaffold",
		"step.ui_scaffold_analyze",
		// http plugin
		"step.rate_limit",
		// actors plugin
		"step.actor_send",
		"step.actor_ask",
		// secrets plugin
		"step.secret_rotate",
		// platform plugin
		"step.platform_template",
		"step.scaling_plan",
		"step.scaling_apply",
		"step.scaling_status",
		"step.scaling_destroy",
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
		"step.ecs_plan",
		"step.ecs_apply",
		"step.ecs_status",
		"step.ecs_destroy",
		"step.app_deploy",
		"step.app_status",
		"step.app_rollback",
		// cicd plugin
		"step.git_clone",
		"step.git_commit",
		"step.git_push",
		"step.git_tag",
		"step.git_checkout",
	}
	for _, e := range expected {
		if _, ok := types[e]; !ok {
			t.Errorf("expected step type %q to be in registry", e)
		}
	}
}

func TestKnownStepTypesAllHaveStepPrefix(t *testing.T) {
	types := KnownStepTypes()
	for typeName := range types {
		if !strings.HasPrefix(typeName, "step.") {
			t.Errorf("step type %q does not start with 'step.'", typeName)
		}
	}
}

func TestKnownStepTypesPluginField(t *testing.T) {
	types := KnownStepTypes()
	for typeName, info := range types {
		if info.Plugin == "" {
			t.Errorf("step type %q has empty Plugin field", typeName)
		}
		if info.Type != typeName {
			t.Errorf("step type %q has mismatched Type field: %q", typeName, info.Type)
		}
	}
}

func TestKnownTriggerTypes(t *testing.T) {
	triggers := KnownTriggerTypes()
	expected := []string{"http", "event", "schedule"}
	for _, e := range expected {
		if !triggers[e] {
			t.Errorf("expected trigger type %q to be known", e)
		}
	}
}

func TestModuleTypeCount(t *testing.T) {
	types := KnownModuleTypes()
	// We should have a substantial number of module types
	if len(types) < 30 {
		t.Errorf("expected at least 30 module types, got %d", len(types))
	}
}

func TestStepTypeCount(t *testing.T) {
	types := KnownStepTypes()
	// We should have a substantial number of step types — all built-in plugin steps.
	// This threshold guards against accidental removal; update it when new steps are added.
	if len(types) < 120 {
		t.Errorf("expected at least 120 step types, got %d — some step types may have been dropped", len(types))
	}
}

// TestKnownStepTypesCoverAllPlugins ensures KnownStepTypes() is in sync with all step
// types registered by the built-in plugins. Any step type registered by a DefaultPlugin
// that is not listed in KnownStepTypes() will cause this test to fail, preventing silent
// omissions from being introduced in the future.
func TestKnownStepTypesCoverAllPlugins(t *testing.T) {
	// Collect all step types registered by DefaultPlugins via the PluginLoader.
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)
	for _, p := range all.DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) error: %v", p.Name(), err)
		}
	}

	pluginSteps := loader.StepFactories()
	known := KnownStepTypes()

	for stepType := range pluginSteps {
		if _, ok := known[stepType]; !ok {
			t.Errorf("step type %q is registered by a built-in plugin but missing from KnownStepTypes()", stepType)
		}
	}
}
