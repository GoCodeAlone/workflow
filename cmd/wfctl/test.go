package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginactors "github.com/GoCodeAlone/workflow/plugins/actors"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginlicense "github.com/GoCodeAlone/workflow/plugins/license"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginopenapi "github.com/GoCodeAlone/workflow/plugins/openapi"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
	"gopkg.in/yaml.v3"
)

func runTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "Verbose output (print each assertion)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl test [options] <file_or_dir> [file_or_dir ...]

Run YAML-based workflow integration tests.

Each *_test.yaml file defines a workflow config and a set of named test cases.
Results are printed as PASS/FAIL with timing. Exit code is non-zero on failure.

Examples:
  wfctl test tests/
  wfctl test tests/pipeline_test.yaml
  wfctl test -v tests/

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	targets := fs.Args()
	if len(targets) == 0 {
		fs.Usage()
		return fmt.Errorf("at least one file or directory is required")
	}

	// Collect all *_test.yaml files from the given targets.
	var files []string
	for _, target := range targets {
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", target, err)
		}
		if info.IsDir() {
			matches, err := filepath.Glob(filepath.Join(target, "*_test.yaml"))
			if err != nil {
				return fmt.Errorf("glob %s: %w", target, err)
			}
			files = append(files, matches...)
		} else {
			files = append(files, target)
		}
	}

	if len(files) == 0 {
		fmt.Println("No *_test.yaml files found.")
		return nil
	}

	// Run all files and collect results.
	var (
		totalPass int
		totalFail int
	)

	for _, f := range files {
		pass, fail, err := runTestFile(f, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", f, err)
			totalFail++
			continue
		}
		totalPass += pass
		totalFail += fail
	}

	// Print summary when more than one file was processed.
	if len(files) > 1 {
		fmt.Printf("\n--- Summary ---\n")
		fmt.Printf("  %d passed, %d failed\n", totalPass, totalFail)
	}

	if totalFail > 0 {
		return fmt.Errorf("%d test(s) failed", totalFail)
	}
	return nil
}

// testFile mirrors wftest.TestFile without the testing.T dependency.
type testFile struct {
	Config string              `yaml:"config"`
	YAML   string              `yaml:"yaml"`
	Mocks  testMockConfig      `yaml:"mocks"`
	Tests  map[string]testCase `yaml:"tests"`
}

type testMockConfig struct {
	Steps map[string]map[string]any `yaml:"steps"`
}

type testCase struct {
	Description string         `yaml:"description"`
	Trigger     testTriggerDef `yaml:"trigger"`
	StopAfter   string         `yaml:"stop_after"`
	Mocks       *testMockConfig `yaml:"mocks"`
	Assertions  []testAssertion `yaml:"assertions"`
}

type testTriggerDef struct {
	Type    string            `yaml:"type"`
	Name    string            `yaml:"name"`
	Data    map[string]any    `yaml:"data"`
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

type testAssertion struct {
	Step     string         `yaml:"step"`
	Output   map[string]any `yaml:"output"`
	Executed *bool          `yaml:"executed"`
	Response *testResponseAssert `yaml:"response"`
}

type testResponseAssert struct {
	Status int    `yaml:"status"`
	Body   string `yaml:"body"`
}

// testResult holds the outcome of a single test case execution.
type testResult struct {
	name     string
	pass     bool
	failures []string
	duration time.Duration
}

func runTestFile(path string, verbose bool) (pass, fail int, err error) {
	// Suppress pipeline engine logs so test output is clean.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("read: %w", err)
	}

	var tf testFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return 0, 0, fmt.Errorf("parse: %w", err)
	}

	if len(tf.Tests) == 0 {
		fmt.Printf("%s: no tests\n", filepath.Base(path))
		return 0, 0, nil
	}

	// Resolve config path relative to the test file.
	if tf.Config != "" && !filepath.IsAbs(tf.Config) {
		tf.Config = filepath.Join(filepath.Dir(path), tf.Config)
	}

	fmt.Printf("%s\n", filepath.Base(path))

	for name, tc := range tf.Tests {
		r := runTestCase(name, &tf, &tc)
		if r.pass {
			pass++
			fmt.Printf("  PASS %-40s %s\n", name, r.duration.Round(time.Millisecond))
		} else {
			fail++
			fmt.Printf("  FAIL %-40s %s\n", name, r.duration.Round(time.Millisecond))
			for _, f := range r.failures {
				fmt.Printf("       %s\n", f)
			}
		}
		if verbose && r.pass {
			for _, f := range r.failures {
				fmt.Printf("       %s\n", f)
			}
		}
	}
	return pass, fail, nil
}

func runTestCase(name string, tf *testFile, tc *testCase) *testResult {
	r := &testResult{name: name}
	start := time.Now()

	// Merge file-level and per-test mocks.
	merged := mergeTestMocks(&tf.Mocks, tc.Mocks)

	// Build engine.
	eng, err := buildTestEngine(tf, merged)
	if err != nil {
		r.failures = append(r.failures, fmt.Sprintf("engine setup: %v", err))
		r.duration = time.Since(start)
		return r
	}

	// Execute the trigger.
	result, stepOutputs, err := executeTestTrigger(eng, tc)
	r.duration = time.Since(start)

	// Check assertions.
	for i, a := range tc.Assertions {
		label := fmt.Sprintf("[%d]", i)
		checkTestAssertion(label, a, result, stepOutputs, err, &r.failures)
	}

	r.pass = len(r.failures) == 0
	return r
}

// buildTestEngine creates a StdEngine from the TestFile config and mocks.
func buildTestEngine(tf *testFile, mocks *testMockConfig) (*workflow.StdEngine, error) {
	logger := &testDiscardLogger{}
	app := modular.NewStdApplication(nil, logger)
	eng := workflow.NewStdEngine(app, logger)

	// Load all built-in plugins.
	for _, p := range testBuiltinPlugins() {
		if err := eng.LoadPlugin(p); err != nil {
			return nil, fmt.Errorf("LoadPlugin(%s): %w", p.Name(), err)
		}
	}

	// Register mock step factories.
	if mocks != nil {
		for stepType, output := range mocks.Steps {
			output := output // capture
			eng.AddStepType(stepType, newTestMockStepFactory(output))
		}
	}

	// Load config.
	var cfg *config.WorkflowConfig
	var err error
	if tf.YAML != "" {
		cfg, err = config.LoadFromString(tf.YAML)
	} else if tf.Config != "" {
		cfg, err = config.LoadFromFile(tf.Config)
	} else {
		return nil, fmt.Errorf("test file must set 'yaml' or 'config'")
	}
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := eng.BuildFromConfig(cfg); err != nil {
		return nil, fmt.Errorf("BuildFromConfig: %w", err)
	}

	return eng, nil
}

// executeTestTrigger runs the trigger and returns output, step outputs, and any error.
func executeTestTrigger(eng *workflow.StdEngine, tc *testCase) (map[string]any, map[string]map[string]any, error) {
	trigType := strings.ToLower(tc.Trigger.Type)
	if trigType == "" {
		trigType = "pipeline"
	}

	switch trigType {
	case "pipeline":
		name := tc.Trigger.Name
		if name == "" {
			return nil, nil, fmt.Errorf("trigger.name is required for pipeline triggers")
		}
		if tc.StopAfter != "" {
			return executePipelineWithStopAfter(eng, name, tc.Trigger.Data, tc.StopAfter)
		}
		pc, err := eng.ExecutePipelineContext(context.Background(), name, tc.Trigger.Data)
		if err != nil {
			return nil, nil, err
		}
		output := pc.Current
		if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
			output = pipeOut
		}
		return output, pc.StepOutputs, nil

	default:
		return nil, nil, fmt.Errorf("unsupported trigger type %q (wfctl test only supports pipeline triggers)", tc.Trigger.Type)
	}
}

// executePipelineWithStopAfter injects a stop sentinel and runs the pipeline.
func executePipelineWithStopAfter(eng *workflow.StdEngine, name string, data map[string]any, stopAfter string) (map[string]any, map[string]map[string]any, error) {
	pipeline, ok := eng.GetPipeline(name)
	if !ok {
		return nil, nil, fmt.Errorf("pipeline %q not found", name)
	}

	stopIdx := -1
	for i, step := range pipeline.Steps {
		if step.Name() == stopAfter {
			stopIdx = i
			break
		}
	}
	if stopIdx == -1 {
		return nil, nil, fmt.Errorf("stop_after: step %q not found in pipeline %q", stopAfter, name)
	}

	sentinel := &testStopSentinel{}
	insertAt := stopIdx + 1
	pipeline.Steps = append(pipeline.Steps, nil)
	copy(pipeline.Steps[insertAt+1:], pipeline.Steps[insertAt:])
	pipeline.Steps[insertAt] = sentinel
	defer func() {
		pipeline.Steps = append(pipeline.Steps[:insertAt], pipeline.Steps[insertAt+1:]...)
	}()

	pc, err := pipeline.Execute(context.Background(), data)
	if err != nil {
		return nil, nil, err
	}

	output := pc.Current
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		output = pipeOut
	}
	if pc.StepOutputs != nil {
		delete(pc.StepOutputs, sentinel.Name())
	}
	return output, pc.StepOutputs, nil
}

type testStopSentinel struct{}

func (s *testStopSentinel) Name() string { return "__wfctl_test_stop__" }
func (s *testStopSentinel) Execute(_ context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	return &interfaces.StepResult{Stop: true}, nil
}

// checkTestAssertion evaluates one assertion and appends failures if needed.
func checkTestAssertion(label string, a testAssertion, output map[string]any, stepOutputs map[string]map[string]any, execErr error, failures *[]string) {
	// HTTP response assertions are not supported in wfctl test (no HTTP server).
	if a.Response != nil {
		*failures = append(*failures, fmt.Sprintf("%s: HTTP response assertions require go-based tests with WithServer()", label))
		return
	}

	// Executed assertion.
	if a.Executed != nil {
		_, executed := stepOutputs[a.Step]
		if *a.Executed && !executed {
			*failures = append(*failures, fmt.Sprintf("%s: expected step %q to have executed", label, a.Step))
		} else if !*a.Executed && executed {
			*failures = append(*failures, fmt.Sprintf("%s: expected step %q to NOT have executed", label, a.Step))
		}
	}

	// Output assertions.
	if len(a.Output) == 0 {
		return
	}

	var actual map[string]any
	if a.Step != "" {
		actual = stepOutputs[a.Step]
		if actual == nil {
			*failures = append(*failures, fmt.Sprintf("%s: step %q has no output (did it execute?)", label, a.Step))
			return
		}
	} else {
		if execErr != nil {
			*failures = append(*failures, fmt.Sprintf("%s: pipeline returned error: %v", label, execErr))
			return
		}
		actual = output
	}

	for key, want := range a.Output {
		got := actual[key]
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			*failures = append(*failures, fmt.Sprintf("%s: output[%q]: want %v, got %v", label, key, want, got))
		}
	}
}

// mergeTestMocks merges file-level mocks with per-test overrides.
func mergeTestMocks(base *testMockConfig, override *testMockConfig) *testMockConfig {
	if override == nil {
		return base
	}
	merged := &testMockConfig{Steps: make(map[string]map[string]any)}
	for k, v := range base.Steps {
		merged.Steps[k] = v
	}
	for k, v := range override.Steps {
		merged.Steps[k] = v
	}
	return merged
}

// newTestMockStepFactory creates a step factory that always returns a fixed output map.
func newTestMockStepFactory(output map[string]any) module.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
		return &testMockStep{name: name, output: output}, nil
	}
}

type testMockStep struct {
	name   string
	output map[string]any
}

func (m *testMockStep) Name() string { return m.name }
func (m *testMockStep) Execute(_ context.Context, pc *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	out := make(map[string]any, len(m.output))
	for k, v := range m.output {
		out[k] = v
	}
	return &interfaces.StepResult{Output: out}, nil
}

// testDiscardLogger silently drops all log output.
type testDiscardLogger struct{}

func (d *testDiscardLogger) Info(msg string, args ...any)  {}
func (d *testDiscardLogger) Error(msg string, args ...any) {}
func (d *testDiscardLogger) Warn(msg string, args ...any)  {}
func (d *testDiscardLogger) Debug(msg string, args ...any) {}

// testBuiltinPlugins returns all built-in engine plugins for test execution.
func testBuiltinPlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginhttp.New(),
		pluginobs.New(),
		pluginmessaging.New(),
		pluginsm.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		pluginpipeline.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
		pluginplatform.New(),
		pluginlicense.New(),
		pluginopenapi.New(),
		pluginactors.New(),
	}
}
