package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPipelineClosureWalksEveryStaticComposite(t *testing.T) {
	leaf := map[string]any{"name": "called", "type": "step.workflow_call", "config": map[string]any{"workflow": "child"}}
	for kind, childConfig := range map[string]map[string]any{
		"step.branch":                    {"branches": map[string]any{"case": []any{leaf}}, "default": []any{leaf}},
		"step.foreach":                   {"step": leaf},
		"step.while":                     {"steps": []any{leaf}},
		"step.parallel":                  {"steps": []any{leaf}},
		"step.retry_with_backoff":        {"step": leaf},
		"step.resilient_circuit_breaker": {"step": leaf, "fallback": leaf},
	} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.WorkflowConfig{Pipelines: map[string]any{
				"selected": map[string]any{
					"steps":        []any{map[string]any{"name": "composite", "type": kind, "config": childConfig}},
					"compensation": []any{map[string]any{"name": "undo", "type": "step.fixture"}},
				},
				"child":     map[string]any{"steps": []any{map[string]any{"name": "result", "type": "step.set"}}},
				"unrelated": map[string]any{"steps": []any{map[string]any{"type": "step.not_installed"}}},
			}}
			before, _ := json.Marshal(cfg)
			closure, err := selectPipelineClosure(cfg, "selected", true)
			if err != nil {
				t.Fatal(err)
			}
			if len(closure.config.Pipelines) != 2 || !closure.types["step.fixture"] || !closure.types["step.set"] {
				t.Fatalf("incomplete static closure: %#v, %#v", closure.config.Pipelines, closure.types)
			}
			after, _ := json.Marshal(cfg)
			if string(before) != string(after) {
				t.Fatal("preflight mutated application config")
			}
		})
	}
}

func TestPipelineClosureNamesMatchNativeFactories(t *testing.T) {
	for _, kind := range []string{"step.foreach", "step.while"} {
		cfg := &config.WorkflowConfig{Pipelines: map[string]any{"selected": map[string]any{
			"steps": []any{
				map[string]any{"type": "step.set"},
				map[string]any{"name": "loop", "type": kind, "config": map[string]any{"steps": []any{
					map[string]any{"type": "step.set"}, map[string]any{"type": "step.set"},
				}}},
			},
		}}}
		closure, err := selectPipelineClosure(cfg, "selected", true)
		if err != nil {
			t.Fatal(err)
		}
		if closure.names["loop-sub-0"] != 1 || closure.names["loop-sub-1"] != 1 || closure.names["selected[0]"] != 0 {
			t.Fatalf("closure invented names unavailable from native %s factories: %#v", kind, closure.names)
		}
	}
}

func TestPipelineClosureDeniesRuntimeAndUnknownForms(t *testing.T) {
	for name, step := range map[string]map[string]any{
		"dynamic":           {"type": "step.workflow_call", "config": map[string]any{"workflow": "{{ .target }}"}},
		"plugin child":      {"type": "step.sub_workflow"},
		"unknown composite": {"type": "step.fixture", "config": map[string]any{"steps": []any{}}},
		"cycle":             {"type": "step.workflow_call", "config": map[string]any{"workflow": "selected"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.WorkflowConfig{Pipelines: map[string]any{
				"selected": map[string]any{"steps": []any{step}},
			}}
			if _, err := selectPipelineClosure(cfg, "selected", true); err == nil {
				t.Fatal("accepted runtime/unknown executable closure")
			}
		})
	}
}

func TestPipelineClosureRejectsNestedSkippedErrors(t *testing.T) {
	cfg := &config.WorkflowConfig{Pipelines: map[string]any{
		"selected": map[string]any{"steps": []any{map[string]any{
			"name": "call", "type": "step.workflow_call", "config": map[string]any{"workflow": "child"},
		}}},
		"child": map[string]any{"on_error": "skip", "steps": []any{map[string]any{
			"name": "result", "type": "step.set", "config": map[string]any{"values": map[string]any{"ready": true}},
		}}},
	}}
	if _, err := selectPipelineClosure(cfg, "selected", true); err == nil || !strings.Contains(err.Error(), "skipped pipeline errors") {
		t.Fatalf("literal child bypassed the record error contract: %v", err)
	}
	if _, err := selectPipelineClosure(cfg, "selected", false); err != nil {
		t.Fatalf("record-only restriction changed human closure: %v", err)
	}
}

func TestPipelineInstallationDependencyOrderAndDenial(t *testing.T) {
	installed := &pipelineInstallations{
		owners: map[string]string{"step.fixture": "fixture"},
		manifests: map[string]*plugin.PluginManifest{
			"fixture":    {Name: "fixture", Version: "1.0.0", Dependencies: []plugin.Dependency{{Name: "dependency", Constraint: ">=1.0.0"}}},
			"dependency": {Name: "dependency", Version: "1.0.0"},
			"unused":     {Name: "unused", Version: "1.0.0"},
		},
	}
	types := map[string]bool{"step.fixture": true}
	got, err := installed.resolve(types)
	if err != nil || strings.Join(got, ",") != "dependency,fixture" {
		t.Fatalf("dependency start order = %v, %v", got, err)
	}
	delete(installed.manifests, "dependency")
	if _, err := installed.resolve(types); err == nil {
		t.Fatal("missing dependency was accepted")
	}
	if _, err := installed.resolve(map[string]bool{"step.unknown": true}); err == nil {
		t.Fatal("unknown step ownership was accepted")
	}
}

func TestPipelineRunPrunesUnselectedPipeline(t *testing.T) {
	path := writePipelineConfig(t, t.TempDir(), "application.yaml", `
modules: []
pipelines:
  selected:
    steps:
      - name: result
        type: step.set
        config:
          values: {ready: "true"}
  unrelated:
    steps:
      - name: unavailable
        type: step.not_installed
`)
	if err := runPipelineRun([]string{"-c", path, "-p", "selected"}); err != nil {
		t.Fatalf("unselected pipeline must not be compiled: %v", err)
	}
}

func TestPipelineRunKeepsLiteralCallClosure(t *testing.T) {
	path := writePipelineConfig(t, t.TempDir(), "application.yaml", `
modules: []
pipelines:
  selected:
    steps:
      - name: call
        type: step.workflow_call
        config: {workflow: child}
  child:
    steps:
      - name: result
        type: step.set
        config:
          values: {ready: "true"}
  unrelated:
    steps:
      - name: unavailable
        type: step.not_installed
`)
	if err := runPipelineRun([]string{"-c", path, "-p", "selected"}); err != nil {
		t.Fatalf("literal child closure must be compiled without unrelated pipeline: %v", err)
	}
}

func TestPipelineRunRejectsDynamicCallBeforeStartup(t *testing.T) {
	path := writePipelineConfig(t, t.TempDir(), "application.yaml", `
modules: []
pipelines:
  selected:
    steps:
      - name: call
        type: step.workflow_call
        config: {workflow: "{{ .target }}"}
`)
	err := runPipelineRun([]string{"-c", path, "-p", "selected", "--input", `{"target":"absent"}`})
	if err == nil || !strings.Contains(err.Error(), "dynamic workflow target") {
		t.Fatalf("dynamic closure must fail during preflight: %v", err)
	}
}

func TestPipelineRunRejectsOwnershipCollisionBeforeStartup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "plugin-started")
	for _, name := range []string{"first", "second"} {
		installed := filepath.Join(dir, name)
		if err := os.Mkdir(installed, 0700); err != nil {
			t.Fatal(err)
		}
		manifest := `{"name":"` + name + `","version":"1.0.0","author":"fixture","description":"startup canary","stepTypes":["step.fixture"]}`
		if err := os.WriteFile(filepath.Join(installed, "plugin.json"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		// This is a startup canary, not a simulated external-plugin implementation.
		script := "#!/bin/sh\nprintf started > '" + marker + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(installed, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := writePipelineConfig(t, t.TempDir(), "application.yaml", `
modules: []
pipelines:
  selected:
    steps:
      - name: result
        type: step.fixture
`)
	err := runPipelineRun([]string{"-c", path, "-p", "selected", "--plugin-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "ownership collision") {
		t.Errorf("want manifest ownership collision before startup, got %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("plugin code ran before manifest validation: %v", err)
	}
}

func TestPipelineRunRejectsUnreferencedMovedIdentityBeforeStartup(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "moved")
	if err := os.Mkdir(installed, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"original","version":"1.0.0","author":"fixture","description":"moved identity","stepTypes":["step.fixture"]}`
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "moved"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path := writePipelineConfig(t, t.TempDir(), "application.yaml", `
modules: []
pipelines:
  selected:
    steps:
      - name: result
        type: step.set
        config:
          values: {ready: "true"}
`)
	err := runPipelineRun([]string{"-c", path, "-p", "selected", "--plugin-dir", dir})
	if err == nil || !strings.Contains(err.Error(), "installation identity") {
		t.Fatalf("want canonical installation identity denial, got %v", err)
	}
}
