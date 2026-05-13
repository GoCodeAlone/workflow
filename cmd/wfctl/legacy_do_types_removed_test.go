package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacyDOTypesAbsent_FromTypeRegistry locks the post-cutover state of
// cmd/wfctl/type_registry.go for issue #617. If any legacy type leaks back in,
// this test fires and the CI gate fires.
func TestLegacyDOTypesAbsent_FromTypeRegistry(t *testing.T) {
	modules := KnownModuleTypes()
	steps := KnownStepTypes()
	legacyModules := []string{
		"platform.do_app", "platform.do_database", "platform.do_dns",
		"platform.do_networking", "platform.doks",
	}
	legacySteps := []string{
		"step.do_deploy", "step.do_status", "step.do_logs",
		"step.do_scale", "step.do_destroy",
	}
	for _, tname := range legacyModules {
		if _, ok := modules[tname]; ok {
			t.Errorf("module type registry still contains legacy DO type %q (issue #617)", tname)
		}
	}
	for _, tname := range legacySteps {
		if _, ok := steps[tname]; ok {
			t.Errorf("step type registry still contains legacy DO type %q (issue #617)", tname)
		}
	}
}

// TestValidateFile_LegacyDOModule_ReturnsActionableError verifies that
// wfctl validate emits the actionable migration error when the config
// references a removed legacy DO module type (issue #617). Covers AC3
// on the validate path (the engine path is covered by
// TestLegacyDOModuleError_PluginNotLoaded in the workflow package).
func TestValidateFile_LegacyDOModule_ReturnsActionableError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "legacy.yaml")
	yamlContent := []byte("modules:\n  - name: api\n    type: platform.do_app\n    config: {}\n")
	if err := os.WriteFile(cfgPath, yamlContent, 0o600); err != nil {
		t.Fatal(err)
	}
	err := validateFile(cfgPath, false, false, false)
	if err == nil {
		t.Fatal("expected error for legacy DO module type")
	}
	msg := err.Error()
	for _, want := range []string{
		"removed from workflow core",
		"workflow-plugin-digitalocean",
		"infra.container_service",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q; got: %s", want, msg)
		}
	}
}

// TestCIValidateFile_LegacyDOStep_ReturnsActionableError covers ciValidateFile's
// accumulating return for legacy DO step types.
func TestCIValidateFile_LegacyDOStep_ReturnsActionableError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "legacy.yaml")
	yamlContent := []byte("pipelines:\n  deploy:\n    steps:\n      - type: step.do_deploy\n")
	if err := os.WriteFile(cfgPath, yamlContent, 0o600); err != nil {
		t.Fatal(err)
	}
	errs := ciValidateFile(cfgPath, false, false, "")
	if len(errs) == 0 {
		t.Fatal("expected error for legacy DO step type")
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
