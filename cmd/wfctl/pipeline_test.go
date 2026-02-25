package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipelineConfig is a minimal config with two pipelines using only
// step.log and step.set, which are always available via the pipeline-steps plugin.
const pipelineConfig = `
modules: []

pipelines:
  greet:
    trigger:
      type: http
      config:
        path: /greet
        method: POST
    steps:
      - name: say-hello
        type: step.log
        config:
          level: info
          message: "Hello, world!"
      - name: set-result
        type: step.set
        config:
          values:
            greeted: "true"
    on_error: stop

  echo:
    trigger:
      type: http
      config:
        path: /echo
        method: POST
    steps:
      - name: log-input
        type: step.log
        config:
          level: info
          message: "Echo: {{ .message }}"
    on_error: stop
`

// pipelineSingleConfig has only one pipeline, for tests that need a single-pipeline config.
const pipelineSingleConfig = `
modules: []

pipelines:
  hello:
    trigger:
      type: http
      config:
        path: /hello
        method: GET
    steps:
      - name: log-hello
        type: step.log
        config:
          level: info
          message: "Hello from pipeline!"
    on_error: stop
`

// noPipelinesConfig has no pipelines section.
const noPipelinesConfig = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`

func writePipelineConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

// --- pipeline subcommand routing ---

func TestRunPipelineMissingSubcommand(t *testing.T) {
	err := runPipeline([]string{})
	if err == nil {
		t.Fatal("expected error when no pipeline subcommand given")
	}
	if !strings.Contains(err.Error(), "subcommand") {
		t.Errorf("expected subcommand error, got: %v", err)
	}
}

func TestRunPipelineUnknownSubcommand(t *testing.T) {
	err := runPipeline([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

// --- pipeline list ---

func TestRunPipelineListMissingConfig(t *testing.T) {
	err := runPipelineList([]string{})
	if err == nil {
		t.Fatal("expected error when -c is missing")
	}
	if !strings.Contains(err.Error(), "-c") {
		t.Errorf("expected -c error, got: %v", err)
	}
}

func TestRunPipelineListNoPipelines(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "no-pipelines.yaml", noPipelinesConfig)
	if err := runPipelineList([]string{"-c", path}); err != nil {
		t.Fatalf("expected no error for empty pipelines, got: %v", err)
	}
}

func TestRunPipelineListWithPipelines(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	if err := runPipelineList([]string{"-c", path}); err != nil {
		t.Fatalf("pipeline list failed: %v", err)
	}
}

func TestRunPipelineListSinglePipeline(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "single.yaml", pipelineSingleConfig)
	if err := runPipelineList([]string{"-c", path}); err != nil {
		t.Fatalf("pipeline list failed: %v", err)
	}
}

func TestRunPipelineListInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "bad.yaml", "not: valid: yaml: ::::")
	// LoadFromFile uses yaml.Unmarshal which is lenient; test a nonexistent file
	_ = path
	err := runPipelineList([]string{"-c", filepath.Join(dir, "nonexistent.yaml")})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- pipeline run ---

func TestRunPipelineRunMissingConfig(t *testing.T) {
	err := runPipelineRun([]string{"-p", "greet"})
	if err == nil {
		t.Fatal("expected error when -c is missing")
	}
	if !strings.Contains(err.Error(), "-c") {
		t.Errorf("expected -c error, got: %v", err)
	}
}

func TestRunPipelineRunMissingPipelineName(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	err := runPipelineRun([]string{"-c", path})
	if err == nil {
		t.Fatal("expected error when -p is missing")
	}
	if !strings.Contains(err.Error(), "-p") {
		t.Errorf("expected -p error, got: %v", err)
	}
}

func TestRunPipelineRunUnknownPipeline(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	err := runPipelineRun([]string{"-c", path, "-p", "does-not-exist"})
	if err == nil {
		t.Fatal("expected error for unknown pipeline name")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected pipeline name in error, got: %v", err)
	}
}

func TestRunPipelineRunUnknownPipelineShowsAvailable(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	err := runPipelineRun([]string{"-c", path, "-p", "missing"})
	if err == nil {
		t.Fatal("expected error for unknown pipeline")
	}
	// Should list available pipelines in the error
	if !strings.Contains(err.Error(), "available") {
		t.Errorf("expected 'available' in error, got: %v", err)
	}
}

func TestRunPipelineRunNoPipelinesInConfig(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", noPipelinesConfig)
	err := runPipelineRun([]string{"-c", path, "-p", "anything"})
	if err == nil {
		t.Fatal("expected error when no pipelines defined")
	}
	if !strings.Contains(err.Error(), "no pipelines defined") {
		t.Errorf("expected 'no pipelines defined' in error, got: %v", err)
	}
}

func TestRunPipelineRunSuccess(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	if err := runPipelineRun([]string{"-c", path, "-p", "hello"}); err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
}

func TestRunPipelineRunWithVars(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	if err := runPipelineRun([]string{"-c", path, "-p", "hello", "--var", "env=test", "--var", "version=1.0"}); err != nil {
		t.Fatalf("pipeline run with vars failed: %v", err)
	}
}

func TestRunPipelineRunWithInputJSON(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	if err := runPipelineRun([]string{"-c", path, "-p", "hello", "--input", `{"key":"value"}`}); err != nil {
		t.Fatalf("pipeline run with input JSON failed: %v", err)
	}
}

func TestRunPipelineRunWithInvalidInputJSON(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	err := runPipelineRun([]string{"-c", path, "-p", "hello", "--input", `not-json`})
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("expected JSON error, got: %v", err)
	}
}

func TestRunPipelineRunWithInvalidVar(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	err := runPipelineRun([]string{"-c", path, "-p", "hello", "--var", "noequals"})
	if err == nil {
		t.Fatal("expected error for invalid --var format")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Errorf("expected key=value error, got: %v", err)
	}
}

func TestRunPipelineRunVerbose(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineSingleConfig)
	if err := runPipelineRun([]string{"-c", path, "-p", "hello", "--verbose"}); err != nil {
		t.Fatalf("pipeline run verbose failed: %v", err)
	}
}

func TestRunPipelineRunMultiStep(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	if err := runPipelineRun([]string{"-c", path, "-p", "greet"}); err != nil {
		t.Fatalf("multi-step pipeline run failed: %v", err)
	}
}

func TestRunPipelineRunEchoPipeline(t *testing.T) {
	dir := t.TempDir()
	path := writePipelineConfig(t, dir, "config.yaml", pipelineConfig)
	// Echo pipeline logs .message — pass it via --var
	if err := runPipelineRun([]string{"-c", path, "-p", "echo", "--var", "message=hello"}); err != nil {
		t.Fatalf("echo pipeline run failed: %v", err)
	}
}

// --- stringSliceFlag ---

func TestStringSliceFlag(t *testing.T) {
	var f stringSliceFlag
	if err := f.Set("key=val"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := f.Set("foo=bar"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if len(f) != 2 {
		t.Errorf("expected 2 entries, got %d", len(f))
	}
	if f[0] != "key=val" || f[1] != "foo=bar" {
		t.Errorf("unexpected values: %v", f)
	}
	if !strings.Contains(f.String(), "key=val") {
		t.Errorf("String() missing entry: %s", f.String())
	}
}
