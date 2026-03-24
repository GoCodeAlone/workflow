package wftest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// RunYAMLTests parses a *_test.yaml file and runs each test case as a subtest.
// testFilePath may be absolute or relative to the working directory.
func RunYAMLTests(t *testing.T, testFilePath string) {
	t.Helper()

	data, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("RunYAMLTests: read %s: %v", testFilePath, err)
	}

	var tf TestFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		t.Fatalf("RunYAMLTests: parse %s: %v", testFilePath, err)
	}

	if len(tf.Tests) == 0 {
		t.Logf("RunYAMLTests: no tests found in %s", testFilePath)
		return
	}

	// Resolve relative config paths relative to the test file's directory.
	if tf.Config != "" && !filepath.IsAbs(tf.Config) {
		tf.Config = filepath.Join(filepath.Dir(testFilePath), tf.Config)
	}

	for name := range tf.Tests {
		tc := tf.Tests[name]
		t.Run(name, func(t *testing.T) {
			t.Helper()
			if tc.Description != "" {
				t.Log(tc.Description)
			}
			runYAMLTestCase(t, &tf, &tc)
		})
	}
}

// RunAllYAMLTests discovers all *_test.yaml files in dir and runs them.
func RunAllYAMLTests(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*_test.yaml"))
	if err != nil {
		t.Fatalf("RunAllYAMLTests: glob %s: %v", dir, err)
	}
	if len(matches) == 0 {
		t.Logf("RunAllYAMLTests: no *_test.yaml files found in %s", dir)
		return
	}
	for _, path := range matches {
		path := path
		// Use the base filename (without extension) as the subtest name.
		base := strings.TrimSuffix(filepath.Base(path), ".yaml")
		t.Run(base, func(t *testing.T) {
			RunYAMLTests(t, path)
		})
	}
}

// runYAMLTestCase builds a harness, fires the trigger, and checks assertions.
func runYAMLTestCase(t *testing.T, tf *TestFile, tc *TestCase) {
	t.Helper()

	// Merge file-level and test-level mocks.
	merged := mergeMockConfigs(&tf.Mocks, tc.Mocks)

	// Build harness options.
	opts := buildHarnessOptions(t, tf, merged)

	// Enable state store if the test uses state or sequence.
	if tc.State != nil || len(tc.Sequence) > 0 {
		opts = append(opts, WithState())
	}

	h := New(t, opts...)

	// Seed state before execution.
	if tc.State != nil {
		for store, data := range tc.State.Seed {
			h.State().Seed(store, data)
		}
		for _, fix := range tc.State.Fixtures {
			if err := h.State().LoadFixture(fix.File, fix.Target); err != nil {
				t.Fatalf("state fixture %s: %v", fix.File, err)
			}
		}
	}

	if len(tc.Sequence) > 0 {
		// Multi-step sequence: each step fires its own trigger, then checks assertions.
		// State persists across all steps (same harness).
		for i := range tc.Sequence {
			step := &tc.Sequence[i]
			label := fmt.Sprintf("sequence[%d](%s)", i, step.Name)
			result := fireSequenceStep(t, h, step)
			for j := range step.Assertions {
				applyAssertion(t, fmt.Sprintf("%s.assertion[%d]", label, j), result, &step.Assertions[j], h)
			}
		}
		return
	}

	// Legacy single-trigger path.
	result := fireTrigger(t, h, tc)
	for i, a := range tc.Assertions {
		applyAssertion(t, fmt.Sprintf("[%d]", i), result, &a, h)
	}
}

// buildHarnessOptions constructs New() options from the test file and mocks.
func buildHarnessOptions(t *testing.T, tf *TestFile, mocks *MockConfig) []Option {
	t.Helper()
	var opts []Option

	switch {
	case tf.YAML != "":
		opts = append(opts, WithYAML(tf.YAML))
	case tf.Config != "":
		opts = append(opts, WithConfig(tf.Config))
	default:
		t.Fatal("RunYAMLTests: either 'yaml' or 'config' must be set in the test file")
	}

	// Install mock steps.
	if mocks != nil {
		for stepType, output := range mocks.Steps {
			output := output // capture
			opts = append(opts, MockStep(stepType, Returns(output)))
		}
	}

	return opts
}

// mergeMockConfigs merges file-level mocks with per-test overrides.
// Per-test step mocks take precedence over file-level mocks.
func mergeMockConfigs(base *MockConfig, override *MockConfig) *MockConfig {
	if override == nil {
		return base
	}
	merged := &MockConfig{
		Steps: make(map[string]map[string]any),
	}
	for k, v := range base.Steps {
		merged.Steps[k] = v
	}
	for k, v := range override.Steps {
		merged.Steps[k] = v
	}
	return merged
}

// fireTrigger dispatches to the right harness method based on the trigger type.
func fireTrigger(t *testing.T, h *Harness, tc *TestCase) *Result {
	t.Helper()
	switch strings.ToLower(tc.Trigger.Type) {
	case "pipeline", "":
		name := tc.Trigger.Name
		if name == "" {
			t.Fatal("RunYAMLTests: trigger.name is required for pipeline triggers")
		}
		if tc.StopAfter != "" {
			return h.ExecutePipelineOpts(name, tc.Trigger.Data, StopAfter(tc.StopAfter))
		}
		return h.ExecutePipeline(name, tc.Trigger.Data)

	case "http", "http.get", "get":
		path := tc.Trigger.Path
		if path == "" {
			t.Fatal("RunYAMLTests: trigger.path is required for http triggers")
		}
		var reqOpts []RequestOption
		for k, v := range tc.Trigger.Headers {
			reqOpts = append(reqOpts, Header(k, v))
		}
		return h.GET(path, reqOpts...)

	case "http.post", "post":
		body := ""
		if tc.Trigger.Data != nil {
			b, _ := json.Marshal(tc.Trigger.Data)
			body = string(b)
		}
		var reqOpts []RequestOption
		for k, v := range tc.Trigger.Headers {
			reqOpts = append(reqOpts, Header(k, v))
		}
		return h.POST(tc.Trigger.Path, body, reqOpts...)

	default:
		t.Fatalf("RunYAMLTests: unsupported trigger type %q", tc.Trigger.Type)
		return nil
	}
}

// fireSequenceStep fires the trigger for one SequenceStep.
// If Trigger.Name is empty, Pipeline is used as the pipeline name.
func fireSequenceStep(t *testing.T, h *Harness, step *SequenceStep) *Result {
	t.Helper()
	td := step.Trigger
	if td.Name == "" && step.Pipeline != "" {
		td.Name = step.Pipeline
		if td.Type == "" {
			td.Type = "pipeline"
		}
	}
	tc := &TestCase{Trigger: td}
	return fireTrigger(t, h, tc)
}

// applyAssertion checks one assertion against the result.
// h is optional (may be nil); it is only needed for state assertions.
func applyAssertion(t *testing.T, label string, result *Result, a *Assertion, h *Harness) {
	t.Helper()

	// Check HTTP response assertions.
	if a.Response != nil {
		if a.Response.Status != 0 && result.StatusCode != a.Response.Status {
			t.Errorf("assertion %s: expected status %d, got %d", label, a.Response.Status, result.StatusCode)
		}
		if a.Response.Body != "" && !strings.Contains(string(result.RawBody), a.Response.Body) {
			t.Errorf("assertion %s: body %q not found in %q", label, a.Response.Body, string(result.RawBody))
		}
		return
	}

	// Check step-executed assertion.
	if a.Executed != nil {
		if *a.Executed && !result.StepExecuted(a.Step) {
			t.Errorf("assertion %s: expected step %q to have executed", label, a.Step)
		} else if !*a.Executed && result.StepExecuted(a.Step) {
			t.Errorf("assertion %s: expected step %q to NOT have executed", label, a.Step)
		}
	}

	// Check output assertions.
	if len(a.Output) > 0 {
		// Select target output map.
		var actual map[string]any
		if a.Step != "" {
			actual = result.StepOutput(a.Step)
			if actual == nil {
				t.Errorf("assertion %s: step %q has no output (did it execute?)", label, a.Step)
				return
			}
		} else {
			if result.Error != nil {
				t.Errorf("assertion %s: pipeline returned error: %v", label, result.Error)
				return
			}
			actual = result.Output
		}

		for key, want := range a.Output {
			got := actual[key]
			// Compare via JSON to handle numeric type differences.
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if !bytes.Equal(wantJSON, gotJSON) {
				t.Errorf("assertion %s: output[%q]: want %v, got %v", label, key, want, got)
			}
		}
	}

	// Check state assertions.
	if len(a.State) > 0 {
		if h == nil || h.state == nil {
			t.Errorf("assertion %s: state assertions require WithState()", label)
			return
		}
		for store, expected := range a.State {
			if err := h.state.Assert(store, expected); err != nil {
				t.Errorf("assertion %s: %v", label, err)
			}
		}
	}
}
