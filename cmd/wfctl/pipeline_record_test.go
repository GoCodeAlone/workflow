package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPipelineRecordEnvelopeRejectsUnknownFields(t *testing.T) {
	for _, envelope := range []map[string]any{
		{"status": "ok", "output": map[string]any{"ready": true}, "foreign": true},
		{"status": "ok", "output": "not-an-object"},
		{"status": "error", "code": "private-canary"},
		{"status": "error", "code": "pipeline_failed", "output": map[string]any{}},
		{"status": "other"},
	} {
		if _, _, err := selectPipelineRecordEnvelope(envelope); err == nil {
			t.Fatal("accepted an invalid child envelope")
		}
	}
	output, code, err := selectPipelineRecordEnvelope(map[string]any{"status": "ok", "output": map[string]any{"ready": true}})
	if err != nil || code != "" || output["ready"] != true {
		t.Fatalf("valid envelope denied: %#v, %q, %v", output, code, err)
	}
}

func TestPipelineRecordSelectorsRejectWhitespaceAndControl(t *testing.T) {
	for _, result := range []string{"result\x00", "result\v", "result\f", "result\x7f", "result\u0085", "result\u00a0", "result\u2003"} {
		t.Run(result, func(t *testing.T) {
			if err := validatePipelineRecordSelectors(result, "FIXTURE_V1", "FIXTURE_ERROR_V1"); err == nil {
				t.Fatal("accepted whitespace/control in a result-step selector")
			}
		})
	}
	if err := validatePipelineRecordSelectors("r\u00e9sultat", "FIXTURE_V1", "FIXTURE_ERROR_V1"); err != nil {
		t.Fatalf("literal Unicode name denied: %v", err)
	}
}

func TestPipelineRecordRuntimeTypeOwnership(t *testing.T) {
	manifest := &plugin.PluginManifest{Name: "fixture", StepTypes: []string{"step.fixture"}}
	for _, factories := range []map[string]plugin.StepFactory{
		{"step.fixture": nil, "step.set": nil}, {}, {"step.other": nil},
	} {
		if err := validatePipelineRuntimeTypes(manifest, factories); err == nil {
			t.Fatal("runtime factories bypassed manifest-only ownership")
		}
	}
	if err := validatePipelineRuntimeTypes(manifest, map[string]plugin.StepFactory{"step.fixture": nil}); err != nil {
		t.Fatal(err)
	}
}

func TestPipelineRecordExecutesActualEngine(t *testing.T) {
	cfg, err := config.LoadFromBytes([]byte(`
modules: []
pipelines:
  selected:
    steps:
      - name: result
        type: step.set
        config:
          values: {message: "{{ .message }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	request := pipelineRecordRequest{Config: cfg, Pipeline: "selected", ResultStep: "result",
		Input: map[string]any{"message": "dynamic-message"}}
	envelope := executePipelineRecord(context.Background(), request)
	output, ok := envelope["output"].(map[string]any)
	if envelope["status"] != "ok" || !ok || output["message"] != "dynamic-message" {
		t.Fatalf("record backend did not execute the configured Workflow engine: %#v", envelope)
	}
	request.ResultStep = "missing"
	envelope = executePipelineRecord(context.Background(), request)
	if envelope["status"] != "error" || envelope["code"] != "result_invalid" {
		t.Fatalf("unreachable selector was accepted: %#v", envelope)
	}
}

func TestPipelineRecordRejectsSkippedPipelineErrors(t *testing.T) {
	cfg, err := config.LoadFromBytes([]byte(`
modules: []
pipelines:
  selected:
    on_error: skip
    steps:
      - name: invalid
        type: step.validate
        config:
          required_fields: [missing]
      - name: result
        type: step.set
        config:
          values: {ready: true}
`))
	if err != nil {
		t.Fatal(err)
	}
	envelope := executePipelineRecord(context.Background(), pipelineRecordRequest{Config: cfg, Pipeline: "selected", ResultStep: "result"})
	if envelope["status"] != "error" {
		t.Fatalf("a failed step followed by a result produced a successful record: %#v", envelope)
	}
	if _, err := selectPipelineClosure(cfg, "selected", false); err != nil {
		t.Fatalf("record-mode error-strategy restriction changed human preflight: %v", err)
	}
}

func TestWorkflowCanonicalJSONV1(t *testing.T) {
	value := map[string]any{
		"z": []any{nil, true, false, "9007199254740993"},
		"a": map[string]any{"text": "<>&\n\t\"\\", "unicode": "\u00e9"},
	}
	want := `{"a":{"text":"<>&\n\t\"\\","unicode":"` + "\u00e9" + `"},"z":[null,true,false,"9007199254740993"]}`
	got, err := workflowCanonicalJSONV1(value)
	if err != nil || string(got) != want {
		t.Fatalf("canonical bytes = %q, %v; want %q", got, err, want)
	}
}

func TestWorkflowCanonicalJSONV1RejectsInvalidValues(t *testing.T) {
	for _, value := range []any{
		1, int64(9007199254740993), float64(1), json.Number("9007199254740993"),
		"\xff", map[string]any{"\xff": "value"}, []any{map[string]any{"number": 1}},
		make(chan int), []byte("bytes"), strings.Repeat("x", 1<<20),
	} {
		if _, err := workflowCanonicalJSONV1(value); err == nil {
			t.Errorf("accepted invalid record value %T", value)
		}
	}
}

func TestPipelineResultFrameRejectsAmbiguousFrames(t *testing.T) {
	valid, err := encodePipelineResultFrame(map[string]any{"status": "ok", "output": map[string]any{"ready": true}})
	if err != nil {
		t.Fatal(err)
	}
	for name, frame := range map[string][]byte{
		"empty":            nil,
		"truncated header": valid[:4],
		"truncated body":   valid[:len(valid)-1],
		"trailing byte":    append(bytes.Clone(valid), ' '),
		"duplicate frame":  append(bytes.Clone(valid), valid...),
		"bad magic":        append([]byte("NOPE"), valid[4:]...),
		"oversized length": append([]byte("WFR1\xff\xff\xff\xff"), valid[8:]...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodePipelineResultFrame(frame); err == nil {
				t.Fatal("accepted ambiguous result channel")
			}
		})
	}
	if _, err := decodePipelineResultFrame(valid); err != nil {
		t.Fatal(err)
	}
}

type pipelineStoppedErrorFixture struct{}

func (pipelineStoppedErrorFixture) Name() string { return "stopped-error" }

func (pipelineStoppedErrorFixture) Execute(context.Context, *module.PipelineContext) (*module.StepResult, error) {
	return &module.StepResult{Output: map[string]any{"error": "private-canary"}, Stop: true}, nil
}

func TestPipelineProgressRejectsStoppedError(t *testing.T) {
	pipeline := &module.Pipeline{Name: "fixture", Steps: []module.PipelineStep{pipelineStoppedErrorFixture{}}}
	_, err := executePipelineWithProgress(context.Background(), pipeline, nil, false)
	if err == nil {
		t.Fatal("a stopped error must not be reported as pipeline success")
	}
	if strings.Contains(err.Error(), "private-canary") {
		t.Fatalf("error output must not be reflected: %v", err)
	}
}

func TestPipelineRunRecordFlagIsRecognized(t *testing.T) {
	stateHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("PATH", t.TempDir())
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
	_, err = captureStdout(t, func() error {
		return runPipelineRun([]string{"-c", path, "-p", "selected", "--output", "record",
			"--result-step", "result", "--record-prefix", "FIXTURE_V1", "--error-prefix", "FIXTURE_ERROR_V1"})
	})
	if err != nil && strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("record-mode contract flags are missing: %v", err)
	}
}
