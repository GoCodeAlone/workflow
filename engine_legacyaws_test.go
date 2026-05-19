package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

func TestLegacyAWSModuleError_PluginNotLoaded(t *testing.T) {
	cases := []struct{ legacyType, hint string }{
		{"platform.ecs", "infra.container_service"},
		{"platform.networking", "infra.vpc"},
		{"platform.apigateway", "infra.api_gateway"},
		{"platform.autoscaling", "infra.autoscaling_group"},
	}
	for _, tc := range cases {
		t.Run(tc.legacyType, func(t *testing.T) {
			e := newIsolatedEngine(t)
			cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{{Name: "x", Type: tc.legacyType, Config: map[string]any{}}}}
			err := e.BuildFromConfig(cfg)
			if err == nil {
				t.Fatalf("expected error for legacy type %q", tc.legacyType)
			}
			msg := err.Error()
			for _, want := range []string{
				"removed from workflow core",
				"workflow-plugin-aws",
				"Install workflow-plugin-aws",
				tc.hint,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error for %q missing %q; got: %s", tc.legacyType, want, msg)
				}
			}
		})
	}
}

func TestLegacyAWSModuleError_PluginLoaded(t *testing.T) {
	e := newIsolatedEngine(t)
	// Register a stub iac.provider factory to simulate workflow-plugin-aws being loaded.
	e.AddModuleType("iac.provider", func(name string, cfg map[string]any) modular.Module { return nil })

	cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{{Name: "x", Type: "platform.ecs", Config: map[string]any{}}}}
	err := e.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already loaded") {
		t.Errorf("plugin-loaded branch must say 'already loaded'; got: %s", msg)
	}
	if strings.Contains(msg, "Install workflow-plugin-aws") {
		t.Errorf("plugin-loaded branch must NOT instruct install; got: %s", msg)
	}
}
