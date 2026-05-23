package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacyAWSTypesAbsent_FromTypeRegistry locks the post-cutover state of
// cmd/wfctl/type_registry.go for issue #653. If any legacy AWS type leaks back
// in, this test fires and the CI gate fires.
func TestLegacyAWSTypesAbsent_FromTypeRegistry(t *testing.T) {
	modules := KnownModuleTypes()
	steps := KnownStepTypes()
	legacyModules := []string{
		"platform.ecs", "platform.networking",
		"platform.apigateway", "platform.autoscaling",
	}
	legacySteps := []string{
		"step.ecs_plan", "step.ecs_apply", "step.ecs_status", "step.ecs_destroy",
		"step.network_plan", "step.network_apply", "step.network_status",
		"step.apigw_plan", "step.apigw_apply", "step.apigw_status", "step.apigw_destroy",
		"step.scaling_plan", "step.scaling_apply", "step.scaling_status", "step.scaling_destroy",
	}
	for _, tname := range legacyModules {
		if _, ok := modules[tname]; ok {
			t.Errorf("module type registry still contains legacy AWS type %q (issue #653)", tname)
		}
	}
	for _, tname := range legacySteps {
		if _, ok := steps[tname]; ok {
			t.Errorf("step type registry still contains legacy AWS type %q (issue #653)", tname)
		}
	}
}

// TestValidateFile_LegacyAWSModule_ReturnsActionableError verifies that
// wfctl validate emits the actionable migration error when the config
// references a removed legacy AWS module type (issue #653). Covers the
// validate path (the engine path is covered by
// TestLegacyAWSModuleError_PluginNotLoaded in the workflow package).
func TestValidateFile_LegacyAWSModule_ReturnsActionableError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "legacy.yaml")
	yamlContent := []byte("modules:\n  - name: svc\n    type: platform.ecs\n    config: {}\n")
	if err := os.WriteFile(cfgPath, yamlContent, 0o600); err != nil {
		t.Fatal(err)
	}
	err := validateFile(cfgPath, false, false, false, false)
	if err == nil {
		t.Fatal("expected error for legacy AWS module type")
	}
	msg := err.Error()
	for _, want := range []string{
		"removed from workflow core",
		"workflow-plugin-aws",
		"infra.container_service",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q; got: %s", want, msg)
		}
	}
}

// TestCIValidateFile_LegacyAWSStep_ReturnsActionableError covers ciValidateFile's
// accumulating return for legacy AWS step types.
func TestCIValidateFile_LegacyAWSStep_ReturnsActionableError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "legacy.yaml")
	yamlContent := []byte("pipelines:\n  deploy:\n    steps:\n      - type: step.ecs_apply\n")
	if err := os.WriteFile(cfgPath, yamlContent, 0o600); err != nil {
		t.Fatal(err)
	}
	errs := ciValidateFile(cfgPath, false, false, "")
	if len(errs) == 0 {
		t.Fatal("expected error for legacy AWS step type")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "step.iac_apply") && strings.Contains(e.Error(), "removed from workflow core") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected actionable migration error in errs; got: %v", errs)
	}
}
