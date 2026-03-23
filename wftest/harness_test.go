package wftest_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestHarness_ExecutePipeline_SetStep(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  greet:
    steps:
      - name: set_greeting
        type: step.set
        config:
          values:
            message: "hello world"
`))

	result := h.ExecutePipeline("greet", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output["message"] != "hello world" {
		t.Errorf("expected 'hello world', got %v", result.Output["message"])
	}
}

func TestHarness_ExecutePipeline_WithInput(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  echo:
    steps:
      - name: copy
        type: step.set
        config:
          values:
            echoed: "{{ .input_val }}"
`))

	result := h.ExecutePipeline("echo", map[string]any{"input_val": "test123"})
	if result.Output["echoed"] != "test123" {
		t.Errorf("expected 'test123', got %v", result.Output["echoed"])
	}
}

func TestHarness_WithConfig_LoadsFile(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  simple:
    steps:
      - name: done
        type: step.set
        config:
          values:
            status: "ok"
`))
	result := h.ExecutePipeline("simple", nil)
	if result.Output["status"] != "ok" {
		t.Errorf("expected 'ok', got %v", result.Output["status"])
	}
}

func TestHarness_ExecutePipeline_NotFound(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  exists:
    steps:
      - name: s
        type: step.set
        config:
          values: { x: 1 }
`))
	result := h.ExecutePipeline("does-not-exist", nil)
	if result.Error == nil {
		t.Error("expected error for missing pipeline")
	}
}
