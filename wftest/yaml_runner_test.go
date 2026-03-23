package wftest_test

import (
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestRunYAMLTests_SimpleFixture(t *testing.T) {
	wftest.RunYAMLTests(t, "testdata/simple_test.yaml")
}

func TestRunAllYAMLTests_Testdata(t *testing.T) {
	wftest.RunAllYAMLTests(t, "testdata")
}

func TestHarness_StopAfter_HaltsExecution(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  multi-step:
    steps:
      - name: step1
        type: step.set
        config:
          values: { a: "1" }
      - name: step2
        type: step.set
        config:
          values: { b: "2" }
      - name: step3
        type: step.set
        config:
          values: { c: "3" }
`))

	result := h.ExecutePipelineOpts("multi-step", nil, wftest.StopAfter("step2"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("step1") {
		t.Error("step1 should have executed")
	}
	if !result.StepExecuted("step2") {
		t.Error("step2 should have executed")
	}
	if result.StepExecuted("step3") {
		t.Error("step3 should NOT have executed (stop_after step2)")
	}
}

func TestHarness_StopAfter_FirstStep(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  three-steps:
    steps:
      - name: a
        type: step.set
        config:
          values: { x: "1" }
      - name: b
        type: step.set
        config:
          values: { y: "2" }
      - name: c
        type: step.set
        config:
          values: { z: "3" }
`))

	result := h.ExecutePipelineOpts("three-steps", nil, wftest.StopAfter("a"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("a") {
		t.Error("step a should have executed")
	}
	if result.StepExecuted("b") {
		t.Error("step b should NOT have executed")
	}
	if result.StepExecuted("c") {
		t.Error("step c should NOT have executed")
	}
}

func TestHarness_StopAfter_LastStep_IsNoop(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  two-steps:
    steps:
      - name: first
        type: step.set
        config:
          values: { a: "1" }
      - name: last
        type: step.set
        config:
          values: { b: "2" }
`))

	// Stopping after the last step should run all steps normally.
	result := h.ExecutePipelineOpts("two-steps", nil, wftest.StopAfter("last"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("first") {
		t.Error("step first should have executed")
	}
	if !result.StepExecuted("last") {
		t.Error("step last should have executed")
	}
}

func TestHarness_StopAfter_UnknownStep_ReturnsError(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  simple:
    steps:
      - name: s
        type: step.set
        config:
          values: { x: 1 }
`))

	result := h.ExecutePipelineOpts("simple", nil, wftest.StopAfter("nonexistent"))
	if result.Error == nil {
		t.Error("expected error for unknown stop_after step")
	}
}

func TestRunYAMLTests_StopAfterInYAML(t *testing.T) {
	// Inline test file via a fixture written in the test.
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/stop_test.yaml", `
yaml: |
  pipelines:
    pipeline1:
      steps:
        - name: alpha
          type: step.set
          config:
            values: { x: "1" }
        - name: beta
          type: step.set
          config:
            values: { y: "2" }
        - name: gamma
          type: step.set
          config:
            values: { z: "3" }
tests:
  partial-run:
    trigger:
      type: pipeline
      name: pipeline1
    stop_after: beta
    assertions:
      - step: alpha
        executed: true
      - step: beta
        executed: true
      - step: gamma
        executed: false
`)
	wftest.RunYAMLTests(t, tmpDir+"/stop_test.yaml")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
