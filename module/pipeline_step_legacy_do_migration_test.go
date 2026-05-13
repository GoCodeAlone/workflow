package module_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func TestLegacyDOStepError_PluginNotLoaded(t *testing.T) {
	// step.do_logs / step.do_scale have GAP messages; the others map 1:1 to step.iac_*.
	cases := []struct{ step, mustContain string }{
		{"step.do_deploy", "step.iac_apply"},
		{"step.do_status", "step.iac_status"},
		{"step.do_destroy", "step.iac_destroy"},
		{"step.do_logs", "wfctl infra logs"},
		{"step.do_scale", "instance_count"},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			r := module.NewStepRegistry() // fresh registry — iacProviderLoaded defaults to false
			_, err := r.Create(tc.step, "x", map[string]any{}, nil)
			if err == nil {
				t.Fatalf("expected error for %q", tc.step)
			}
			msg := err.Error()
			for _, want := range []string{
				"removed from workflow core",
				"workflow-plugin-digitalocean",
				"Install workflow-plugin-digitalocean",
				tc.mustContain,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error for %q missing %q; got: %s", tc.step, want, msg)
				}
			}
		})
	}
}

func TestLegacyDOStepError_PluginLoaded(t *testing.T) {
	// Symmetric to TestLegacyDOModuleError_PluginLoaded — flips the per-registry
	// flag and confirms the step guard's "already loaded" branch fires.
	r := module.NewStepRegistry()
	r.SetIaCProviderLoaded(true)
	_, err := r.Create("step.do_deploy", "x", map[string]any{}, nil)
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
