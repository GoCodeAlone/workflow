package workflow

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestEngine builds a minimal engine with the pipeline steps plugin loaded.
func newTestEngine(t *testing.T) *StdEngine {
	t.Helper()
	logger := discardSlogLogger()
	app := modular.NewStdApplication(nil, logger)
	engine := NewStdEngine(app, logger)
	if err := engine.LoadPlugin(pipelinesteps.New()); err != nil {
		t.Fatalf("LoadPlugin pipelinesteps: %v", err)
	}
	// Register the PipelineWorkflowHandler (normally done via LoadPlugin)
	engine.RegisterWorkflowHandler(handlers.NewPipelineWorkflowHandler())
	return engine
}

// writeTempYAML writes YAML content to a temp file and returns its path.
func writeTempYAML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTempYAML %s: %v", filename, err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Tests for config.IsApplicationConfig
// ---------------------------------------------------------------------------

func TestIsApplicationConfig_True(t *testing.T) {
	yaml := `
application:
  name: my-app
  workflows:
    - file: a.yaml
    - file: b.yaml
`
	if !config.IsApplicationConfig([]byte(yaml)) {
		t.Fatal("expected IsApplicationConfig to return true")
	}
}

func TestIsApplicationConfig_False_SingleWorkflow(t *testing.T) {
	yaml := `
modules: []
workflows: {}
triggers: {}
`
	if config.IsApplicationConfig([]byte(yaml)) {
		t.Fatal("expected IsApplicationConfig to return false for single-workflow config")
	}
}

func TestIsApplicationConfig_False_EmptyWorkflows(t *testing.T) {
	yaml := `
application:
  name: my-app
  workflows: []
`
	if config.IsApplicationConfig([]byte(yaml)) {
		t.Fatal("expected IsApplicationConfig to return false when workflows list is empty")
	}
}

// ---------------------------------------------------------------------------
// Tests for config.LoadApplicationConfig
// ---------------------------------------------------------------------------

func TestLoadApplicationConfig_Basic(t *testing.T) {
	dir := t.TempDir()
	appYAML := `
application:
  name: chat-platform
  workflows:
    - file: main-loop.yaml
    - file: queue-assignment.yaml
      name: queue-assign
`
	appPath := writeTempYAML(t, dir, "app.yaml", appYAML)

	cfg, err := config.LoadApplicationConfig(appPath)
	if err != nil {
		t.Fatalf("LoadApplicationConfig: %v", err)
	}
	if cfg.Application.Name != "chat-platform" {
		t.Errorf("application name = %q, want chat-platform", cfg.Application.Name)
	}
	if len(cfg.Application.Workflows) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(cfg.Application.Workflows))
	}
	if cfg.Application.Workflows[0].File != "main-loop.yaml" {
		t.Errorf("workflow 0 file = %q, want main-loop.yaml", cfg.Application.Workflows[0].File)
	}
	if cfg.Application.Workflows[1].Name != "queue-assign" {
		t.Errorf("workflow 1 name = %q, want queue-assign", cfg.Application.Workflows[1].Name)
	}
	if cfg.ConfigDir == "" {
		t.Error("expected ConfigDir to be set")
	}
}

func TestLoadApplicationConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadApplicationConfig("/nonexistent/path/app.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// Tests for StdEngine.BuildFromApplicationConfig
// ---------------------------------------------------------------------------

func TestBuildFromApplicationConfig_Nil(t *testing.T) {
	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil application config")
	}
}

func TestBuildFromApplicationConfig_EmptyWorkflows(t *testing.T) {
	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name:      "empty-app",
			Workflows: []config.WorkflowRef{},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty workflow list")
	}
}

func TestBuildFromApplicationConfig_MissingFile(t *testing.T) {
	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "broken-app",
			Workflows: []config.WorkflowRef{
				{File: "/nonexistent/workflow.yaml"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing workflow file")
	}
}

func TestBuildFromApplicationConfig_ModuleNameConflict(t *testing.T) {
	dir := t.TempDir()

	// Both workflow files define a module named "shared-cache"
	// The module type doesn't matter for conflict detection (it happens before build)
	wfA := `
modules:
  - name: shared-cache
    type: storage.sqlite
    config:
      dsn: ":memory:"
workflows: {}
triggers: {}
`
	wfB := `
modules:
  - name: shared-cache
    type: storage.sqlite
    config:
      dsn: ":memory:"
workflows: {}
triggers: {}
`
	writeTempYAML(t, dir, "a.yaml", wfA)
	writeTempYAML(t, dir, "b.yaml", wfB)

	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "conflict-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "a.yaml")},
				{File: filepath.Join(dir, "b.yaml")},
			},
		},
		ConfigDir: dir,
	})
	if err == nil {
		t.Fatal("expected error for module name conflict")
	}
}

func TestBuildFromApplicationConfig_TriggerNameConflict(t *testing.T) {
	dir := t.TempDir()

	wfA := `
modules: []
workflows: {}
triggers:
  my-trigger:
    type: http
pipelines: {}
`
	wfB := `
modules: []
workflows: {}
triggers:
  my-trigger:
    type: schedule
pipelines: {}
`
	writeTempYAML(t, dir, "a.yaml", wfA)
	writeTempYAML(t, dir, "b.yaml", wfB)

	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "trigger-conflict-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "a.yaml")},
				{File: filepath.Join(dir, "b.yaml")},
			},
		},
		ConfigDir: dir,
	})
	if err == nil {
		t.Fatal("expected error for trigger name conflict")
	}
	if !strings.Contains(err.Error(), "trigger name conflict") {
		t.Fatalf("expected 'trigger name conflict' in error, got: %v", err)
	}
}

func TestBuildFromApplicationConfig_PipelineNameConflict(t *testing.T) {
	dir := t.TempDir()

	wfA := `
modules: []
workflows: {}
triggers: {}
pipelines:
  shared-pipeline:
    steps:
      - name: step-a
        type: step.set
        config:
          values:
            msg: "from a"
`
	wfB := `
modules: []
workflows: {}
triggers: {}
pipelines:
  shared-pipeline:
    steps:
      - name: step-b
        type: step.set
        config:
          values:
            msg: "from b"
`
	writeTempYAML(t, dir, "a.yaml", wfA)
	writeTempYAML(t, dir, "b.yaml", wfB)

	engine := newTestEngine(t)
	err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "pipeline-conflict-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "a.yaml")},
				{File: filepath.Join(dir, "b.yaml")},
			},
		},
		ConfigDir: dir,
	})
	if err == nil {
		t.Fatal("expected error for pipeline name conflict")
	}
	if !strings.Contains(err.Error(), "pipeline name conflict") {
		t.Fatalf("expected 'pipeline name conflict' in error, got: %v", err)
	}
}

func TestBuildFromApplicationConfig_MultipleWorkflows_MergesPipelines(t *testing.T) {
	dir := t.TempDir()

	// Workflow A: defines an "echo" pipeline
	wfA := `
modules: []
workflows: {}
triggers: {}
pipelines:
  echo:
    steps:
      - name: set-msg
        type: step.set
        config:
          values:
            message: "hello from echo"
`
	// Workflow B: defines a "greet" pipeline
	wfB := `
modules: []
workflows: {}
triggers: {}
pipelines:
  greet:
    steps:
      - name: set-greeting
        type: step.set
        config:
          values:
            greeting: "hello world"
`
	writeTempYAML(t, dir, "wf-a.yaml", wfA)
	writeTempYAML(t, dir, "wf-b.yaml", wfB)

	engine := newTestEngine(t)
	if err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "merged-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "wf-a.yaml")},
				{File: filepath.Join(dir, "wf-b.yaml")},
			},
		},
		ConfigDir: dir,
	}); err != nil {
		t.Fatalf("BuildFromApplicationConfig: %v", err)
	}

	// Both pipelines should be reachable via the engine's workflow handler
	ctx := context.Background()

	// Trigger the echo pipeline
	result, err := engine.TriggerWorkflowResult(ctx, "pipeline:echo", "", nil)
	if err != nil {
		t.Fatalf("TriggerWorkflowResult echo: %v", err)
	}
	if result["message"] != "hello from echo" {
		t.Errorf("echo pipeline message = %v, want 'hello from echo'", result["message"])
	}

	// Trigger the greet pipeline
	result, err = engine.TriggerWorkflowResult(ctx, "pipeline:greet", "", nil)
	if err != nil {
		t.Fatalf("TriggerWorkflowResult greet: %v", err)
	}
	if result["greeting"] != "hello world" {
		t.Errorf("greet pipeline greeting = %v, want 'hello world'", result["greeting"])
	}
}

func TestBuildFromApplicationConfig_WorkflowCall_CrossPipelineInvocation(t *testing.T) {
	dir := t.TempDir()

	// Workflow A: a "validate" pipeline that uses step.workflow_call to invoke
	// the "enrich" pipeline defined in Workflow B.
	wfA := `
modules: []
workflows: {}
triggers: {}
pipelines:
  validate:
    steps:
      - name: call-enrich
        type: step.workflow_call
        config:
          workflow: enrich
          mode: sync
          input:
            raw_id: "{{ .order_id }}"
          output_mapping:
            enriched_id: enriched_id
`
	// Workflow B: defines the "enrich" pipeline that transforms an ID
	wfB := `
modules: []
workflows: {}
triggers: {}
pipelines:
  enrich:
    steps:
      - name: set-enriched
        type: step.set
        config:
          values:
            enriched_id: "ENRICHED-{{ .raw_id }}"
`
	writeTempYAML(t, dir, "wf-a.yaml", wfA)
	writeTempYAML(t, dir, "wf-b.yaml", wfB)

	engine := newTestEngine(t)
	if err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "cross-call-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "wf-a.yaml")},
				{File: filepath.Join(dir, "wf-b.yaml")},
			},
		},
		ConfigDir: dir,
	}); err != nil {
		t.Fatalf("BuildFromApplicationConfig: %v", err)
	}

	ctx := context.Background()
	result, err := engine.TriggerWorkflowResult(ctx, "pipeline:validate", "", map[string]any{
		"order_id": "42",
	})
	if err != nil {
		t.Fatalf("TriggerWorkflowResult validate: %v", err)
	}

	if result["enriched_id"] != "ENRICHED-42" {
		t.Errorf("enriched_id = %v, want ENRICHED-42", result["enriched_id"])
	}
}

func TestBuildFromApplicationConfig_NameOverride(t *testing.T) {
	dir := t.TempDir()

	wfYAML := `
modules: []
workflows: {}
triggers: {}
pipelines:
  my-pipe:
    steps:
      - name: noop
        type: step.set
        config:
          values:
            done: true
`
	filePath := writeTempYAML(t, dir, "some-workflow-file.yaml", wfYAML)

	// With explicit Name override, the file should still load correctly.
	engine := newTestEngine(t)
	if err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "named-app",
			Workflows: []config.WorkflowRef{
				{File: filePath, Name: "my-custom-name"},
			},
		},
		ConfigDir: dir,
	}); err != nil {
		t.Fatalf("BuildFromApplicationConfig: %v", err)
	}

	ctx := context.Background()
	result, err := engine.TriggerWorkflowResult(ctx, "pipeline:my-pipe", "", nil)
	if err != nil {
		t.Fatalf("TriggerWorkflowResult: %v", err)
	}
	if result["done"] != true {
		t.Errorf("done = %v, want true", result["done"])
	}
}

// ---------------------------------------------------------------------------
// TriggerWorkflowResult helper for tests
// ---------------------------------------------------------------------------

// TriggerWorkflowResult is a test helper that executes a workflow and returns
// the result map. It mirrors TriggerWorkflow but returns the result for
// assertions without requiring the engine to expose ExecuteWorkflow directly.
func (e *StdEngine) TriggerWorkflowResult(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	for _, handler := range e.workflowHandlers {
		if handler.CanHandle(workflowType) {
			if data == nil {
				data = map[string]any{}
			}
			return handler.ExecuteWorkflow(ctx, workflowType, action, data)
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Tests for step.workflow_call in pipeline registry
// ---------------------------------------------------------------------------

func TestEngine_PipelineRegistry_PopulatedAfterBuild(t *testing.T) {
	dir := t.TempDir()

	wfYAML := `
modules: []
workflows: {}
triggers: {}
pipelines:
  my-pipeline:
    steps:
      - name: noop
        type: step.set
        config:
          values:
            ok: true
`
	writeTempYAML(t, dir, "wf.yaml", wfYAML)

	engine := newTestEngine(t)
	if err := engine.BuildFromApplicationConfig(&config.ApplicationConfig{
		Application: config.ApplicationInfo{
			Name: "test-app",
			Workflows: []config.WorkflowRef{
				{File: filepath.Join(dir, "wf.yaml")},
			},
		},
		ConfigDir: dir,
	}); err != nil {
		t.Fatalf("BuildFromApplicationConfig: %v", err)
	}

	// The pipeline should be in the registry
	p, ok := engine.pipelineRegistry["my-pipeline"]
	if !ok {
		t.Fatal("expected 'my-pipeline' to be in pipeline registry")
	}
	if p == nil {
		t.Fatal("expected non-nil pipeline in registry")
	}
}

func TestEngine_WorkflowCallStep_RegistryLookup(t *testing.T) {
	// Test that step.workflow_call can look up pipelines via the engine's registry
	logger := discardSlogLogger()
	app := modular.NewStdApplication(nil, logger)
	engine := NewStdEngine(app, logger)

	// Manually add a pipeline to the registry
	targetPipeline := &module.Pipeline{
		Name: "target",
		Steps: []module.PipelineStep{
			&module.SetStep{},
		},
	}
	engine.pipelineRegistry["target"] = targetPipeline

	// The step.workflow_call factory should use the registry lookup
	step, err := engine.stepRegistry.Create("step.workflow_call", "test-call", map[string]any{
		"workflow": "target",
	}, app)
	if err != nil {
		t.Fatalf("stepRegistry.Create: %v", err)
	}
	if step == nil {
		t.Fatal("expected non-nil step")
	}
}
