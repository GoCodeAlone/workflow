package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeTestYAML writes content to path/name under a temp dir and returns the path.
func writeTestYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestYAML %s: %v", p, err)
	}
	return p
}

// simpleTestYAML is a minimal *_test.yaml fixture used across several tests.
const simpleTestYAML = `
yaml: |
  pipelines:
    greet:
      steps:
        - name: set_msg
          type: step.set
          config:
            values:
              message: "hello {{ .name }}"
tests:
  greet-alice:
    trigger:
      type: pipeline
      name: greet
      data:
        name: alice
    assertions:
      - output:
          message: "hello alice"
`

// --- mergeTestMocks ---

func TestMergeTestMocks_NilOverride(t *testing.T) {
	base := &testMockConfig{Steps: map[string]map[string]any{
		"step.a": {"count": 1},
	}}
	got := mergeTestMocks(base, nil)
	if got != base {
		t.Error("expected base to be returned unchanged when override is nil")
	}
}

func TestMergeTestMocks_OverrideWins(t *testing.T) {
	base := &testMockConfig{Steps: map[string]map[string]any{
		"step.a": {"count": 1},
		"step.b": {"count": 2},
	}}
	override := &testMockConfig{Steps: map[string]map[string]any{
		"step.a": {"count": 99},
	}}
	merged := mergeTestMocks(base, override)
	if merged.Steps["step.a"]["count"] != 99 {
		t.Errorf("expected step.a count=99, got %v", merged.Steps["step.a"]["count"])
	}
	if merged.Steps["step.b"]["count"] != 2 {
		t.Errorf("expected step.b count=2 (inherited), got %v", merged.Steps["step.b"]["count"])
	}
}

// --- checkTestAssertion ---

func TestCheckTestAssertion_OutputPass(t *testing.T) {
	output := map[string]any{"message": "hello alice"}
	var failures []string
	checkTestAssertion("[0]", testAssertion{Output: map[string]any{"message": "hello alice"}},
		output, nil, nil, &failures)
	if len(failures) != 0 {
		t.Errorf("expected no failures, got: %v", failures)
	}
}

func TestCheckTestAssertion_OutputFail(t *testing.T) {
	output := map[string]any{"message": "hello bob"}
	var failures []string
	checkTestAssertion("[0]", testAssertion{Output: map[string]any{"message": "hello alice"}},
		output, nil, nil, &failures)
	if len(failures) != 1 {
		t.Errorf("expected 1 failure, got %d: %v", len(failures), failures)
	}
}

func TestCheckTestAssertion_ExecutedTrue(t *testing.T) {
	executed := true
	stepOutputs := map[string]map[string]any{"my-step": {"x": 1}}
	var failures []string
	checkTestAssertion("[0]", testAssertion{Step: "my-step", Executed: &executed},
		nil, stepOutputs, nil, &failures)
	if len(failures) != 0 {
		t.Errorf("expected no failures, got: %v", failures)
	}
}

func TestCheckTestAssertion_ExecutedFalse_StepRan(t *testing.T) {
	notExecuted := false
	stepOutputs := map[string]map[string]any{"my-step": {"x": 1}}
	var failures []string
	checkTestAssertion("[0]", testAssertion{Step: "my-step", Executed: &notExecuted},
		nil, stepOutputs, nil, &failures)
	if len(failures) == 0 {
		t.Error("expected failure: step ran but executed=false asserted")
	}
}

func TestCheckTestAssertion_StepOutput(t *testing.T) {
	stepOutputs := map[string]map[string]any{
		"my-step": {"result": "ok"},
	}
	var failures []string
	checkTestAssertion("[0]", testAssertion{Step: "my-step", Output: map[string]any{"result": "ok"}},
		nil, stepOutputs, nil, &failures)
	if len(failures) != 0 {
		t.Errorf("expected no failures, got: %v", failures)
	}
}

func TestCheckTestAssertion_PipelineError(t *testing.T) {
	var failures []string
	checkTestAssertion("[0]", testAssertion{Output: map[string]any{"x": 1}},
		nil, nil, os.ErrNotExist, &failures)
	if len(failures) == 0 {
		t.Error("expected failure when pipeline returned error")
	}
}

// --- runTestFile ---

func TestRunTestFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "greet_test.yaml", simpleTestYAML)

	pass, fail, err := runTestFile(filepath.Join(dir, "greet_test.yaml"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pass != 1 {
		t.Errorf("expected 1 pass, got %d", pass)
	}
	if fail != 0 {
		t.Errorf("expected 0 failures, got %d", fail)
	}
}

func TestRunTestFile_AssertionFails(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "bad_test.yaml", `
yaml: |
  pipelines:
    greet:
      steps:
        - name: set_msg
          type: step.set
          config:
            values:
              message: "hello world"
tests:
  wrong-output:
    trigger:
      type: pipeline
      name: greet
    assertions:
      - output:
          message: "this is wrong"
`)

	pass, fail, err := runTestFile(filepath.Join(dir, "bad_test.yaml"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pass != 0 {
		t.Errorf("expected 0 passes, got %d", pass)
	}
	if fail != 1 {
		t.Errorf("expected 1 failure, got %d", fail)
	}
}

func TestRunTestFile_NoTests(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "empty_test.yaml", `
yaml: |
  pipelines:
    noop:
      steps:
        - name: s
          type: step.set
          config:
            values: {}
tests: {}
`)

	pass, fail, err := runTestFile(filepath.Join(dir, "empty_test.yaml"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pass != 0 || fail != 0 {
		t.Errorf("expected 0 pass/fail for empty tests, got pass=%d fail=%d", pass, fail)
	}
}

func TestRunTestFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "broken_test.yaml", `{{{not valid yaml`)

	_, _, err := runTestFile(filepath.Join(dir, "broken_test.yaml"), false)
	if err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

// --- runTestCase ---

func TestRunTestCase_Pass(t *testing.T) {
	tf := parseTestFileString(t, simpleTestYAML)
	tc := tf.Tests["greet-alice"]
	r := runTestCase("greet-alice", tf, &tc)
	if !r.pass {
		t.Errorf("expected pass, got failures: %v", r.failures)
	}
}

func TestRunTestCase_UnknownPipeline(t *testing.T) {
	tf := parseTestFileString(t, simpleTestYAML)
	// Include an output assertion so checkTestAssertion sees the execution error.
	tc := testCase{
		Trigger:    testTriggerDef{Type: "pipeline", Name: "nonexistent"},
		Assertions: []testAssertion{{Output: map[string]any{"x": 1}}},
	}
	r := runTestCase("bad", tf, &tc)
	if r.pass {
		t.Error("expected failure for unknown pipeline")
	}
}

func TestRunTestCase_MockOverridesStep(t *testing.T) {
	const yaml = `
yaml: |
  pipelines:
    fetch:
      steps:
        - name: query
          type: step.db_query
          config:
            database: db
            query: "SELECT 1"
            mode: list
        - name: count
          type: step.set
          config:
            values:
              total: "{{ index .steps \"query\" \"count\" }}"
mocks:
  steps:
    step.db_query:
      rows: []
      count: 0
tests:
  fetch-mocked:
    trigger:
      type: pipeline
      name: fetch
    assertions:
      - output:
          total: "0"
`
	tf := parseTestFileString(t, yaml)
	tc := tf.Tests["fetch-mocked"]
	r := runTestCase("fetch-mocked", tf, &tc)
	if !r.pass {
		t.Errorf("expected pass with mocked step, got failures: %v", r.failures)
	}
}

// --- runTest (integration) ---

func TestRunTest_DirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "greet_test.yaml", simpleTestYAML)

	err := runTest([]string{dir})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestRunTest_NoFilesFound(t *testing.T) {
	dir := t.TempDir()
	// No *_test.yaml files — should not error, just print a notice.
	err := runTest([]string{dir})
	if err != nil {
		t.Errorf("expected no error when no test files found, got: %v", err)
	}
}

func TestRunTest_FailingTestExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "fail_test.yaml", `
yaml: |
  pipelines:
    greet:
      steps:
        - name: s
          type: step.set
          config:
            values:
              message: "actual"
tests:
  wrong:
    trigger:
      type: pipeline
      name: greet
    assertions:
      - output:
          message: "expected-but-wrong"
`)

	err := runTest([]string{dir})
	if err == nil {
		t.Error("expected non-nil error when test fails")
	}
}

// parseTestFileString is a test helper that parses a YAML test file string.
func parseTestFileString(t *testing.T, content string) *testFile {
	t.Helper()
	var tf testFile
	if err := yaml.Unmarshal([]byte(content), &tf); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &tf
}
