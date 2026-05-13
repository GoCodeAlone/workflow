package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

// newIsolatedEngine builds a plugin-free StdEngine — required so that the
// iac.provider factory-map lookup is deterministically absent in the
// "plugin not loaded" tests, and so that the manual AddModuleType stub in
// the "plugin loaded" test is the only factory registered. This differs from
// setupEngineTest (engine_test.go) which calls loadAllPlugins, and from
// newTestEngine (engine_multi_config_test.go) which loads pipelinesteps.
// Reuses the `mockLogger` type already defined in engine_test.go — both files
// are in package workflow so the type is visible at compile time. DO NOT
// redeclare it here.
func newIsolatedEngine(t *testing.T) *StdEngine {
	t.Helper()
	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	if err := app.Init(); err != nil {
		t.Fatalf("app.Init: %v", err)
	}
	return NewStdEngine(app, logger)
}

func TestLegacyDOModuleError_PluginNotLoaded(t *testing.T) {
	cases := []struct{ legacyType, hint string }{
		{"platform.do_app", "infra.container_service"},
		{"platform.do_database", "infra.database"},
		{"platform.do_dns", "infra.dns"},
		{"platform.do_networking", "infra.vpc"},
		{"platform.doks", "infra.k8s_cluster"},
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
				"workflow-plugin-digitalocean",
				"Install workflow-plugin-digitalocean",
				tc.hint,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error for %q missing %q; got: %s", tc.legacyType, want, msg)
				}
			}
		})
	}
}

func TestLegacyDOModuleError_PluginLoaded(t *testing.T) {
	e := newIsolatedEngine(t)
	// Register a stub iac.provider factory to simulate workflow-plugin-digitalocean
	// being loaded. ModuleFactory signature: func(name string, config map[string]any) modular.Module.
	e.AddModuleType("iac.provider", func(name string, cfg map[string]any) modular.Module { return nil })

	cfg := &config.WorkflowConfig{Modules: []config.ModuleConfig{{Name: "x", Type: "platform.do_app", Config: map[string]any{}}}}
	err := e.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already loaded") {
		t.Errorf("plugin-loaded branch must say 'already loaded'; got: %s", msg)
	}
	if strings.Contains(msg, "Install workflow-plugin-digitalocean") {
		t.Errorf("plugin-loaded branch must NOT instruct install; got: %s", msg)
	}
}
