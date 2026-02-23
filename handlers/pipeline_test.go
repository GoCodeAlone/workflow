package handlers

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// mockPipelineRunner is a minimal PipelineRunner mock used to verify that
// PipelineWorkflowHandler tests do not require concrete module types.
type mockPipelineRunner struct {
	runResult map[string]any
	runErr    error
	loggerSet bool
	recSet    bool
}

func (m *mockPipelineRunner) Run(_ context.Context, _ map[string]any) (map[string]any, error) {
	return m.runResult, m.runErr
}
func (m *mockPipelineRunner) SetLogger(_ *slog.Logger)                    { m.loggerSet = true }
func (m *mockPipelineRunner) SetEventRecorder(_ interfaces.EventRecorder) { m.recSet = true }

// mockEventRecorder is a no-op EventRecorder for tests.
type mockEventRecorder struct{}

func (mockEventRecorder) RecordEvent(_ context.Context, _, _ string, _ map[string]any) error {
	return nil
}

func TestPipelineHandler_CanHandle_PrefixFormat(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	// Register a pipeline
	h.AddPipeline("order-processing", &module.Pipeline{
		Name: "order-processing",
	})

	if !h.CanHandle("pipeline:order-processing") {
		t.Error("expected CanHandle to return true for 'pipeline:order-processing'")
	}
}

func TestPipelineHandler_CanHandle_ExactMatch(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	h.AddPipeline("my-pipeline", &module.Pipeline{
		Name: "my-pipeline",
	})

	if !h.CanHandle("my-pipeline") {
		t.Error("expected CanHandle to return true for exact pipeline name 'my-pipeline'")
	}
}

func TestPipelineHandler_CanHandle_UnknownName(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	h.AddPipeline("existing", &module.Pipeline{
		Name: "existing",
	})

	if h.CanHandle("nonexistent") {
		t.Error("expected CanHandle to return false for 'nonexistent'")
	}
	if h.CanHandle("pipeline:nonexistent") {
		t.Error("expected CanHandle to return false for 'pipeline:nonexistent'")
	}
	if h.CanHandle("http:something") {
		t.Error("expected CanHandle to return false for 'http:something'")
	}
}

func TestPipelineHandler_ExecuteWorkflow_RunsPipeline(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	// Build a real pipeline with a set step and a log step
	reg := module.NewStepRegistry()
	reg.Register("step.set", module.NewSetStepFactory())
	reg.Register("step.log", module.NewLogStepFactory())

	setStep, err := reg.Create("step.set", "set-result", map[string]any{
		"values": map[string]any{
			"greeting": "Hello, {{.name}}!",
			"done":     "true",
		},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create set step: %v", err)
	}

	logStep, err := reg.Create("step.log", "log-it", map[string]any{
		"level":   "info",
		"message": "Processing complete for {{.name}}",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create log step: %v", err)
	}

	pipeline := &module.Pipeline{
		Name:    "greet",
		Steps:   []module.PipelineStep{setStep, logStep},
		OnError: module.ErrorStrategyStop,
	}

	h.AddPipeline("greet", pipeline)

	// Execute via the handler
	result, err := h.ExecuteWorkflow(
		context.Background(),
		"pipeline:greet",
		"",
		map[string]any{"name": "World"},
	)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}

	if result["greeting"] != "Hello, World!" {
		t.Errorf("expected greeting 'Hello, World!', got %v", result["greeting"])
	}
	if result["done"] != "true" {
		t.Errorf("expected done 'true', got %v", result["done"])
	}
	// Trigger data should be in the result (Current contains merged data)
	if result["name"] != "World" {
		t.Errorf("expected trigger data 'name' in result, got %v", result["name"])
	}
}

func TestPipelineHandler_ExecuteWorkflow_ExactNameMatch(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	reg := module.NewStepRegistry()
	reg.Register("step.set", module.NewSetStepFactory())

	step, err := reg.Create("step.set", "mark", map[string]any{
		"values": map[string]any{"executed": "yes"},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	pipeline := &module.Pipeline{
		Name:    "simple",
		Steps:   []module.PipelineStep{step},
		OnError: module.ErrorStrategyStop,
	}
	h.AddPipeline("simple", pipeline)

	// Call without the "pipeline:" prefix
	result, err := h.ExecuteWorkflow(context.Background(), "simple", "", map[string]any{})
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}

	if result["executed"] != "yes" {
		t.Errorf("expected executed 'yes', got %v", result["executed"])
	}
}

func TestPipelineHandler_ExecuteWorkflow_UnknownPipeline(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	_, err := h.ExecuteWorkflow(context.Background(), "pipeline:missing", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown pipeline")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestPipelineHandler_ExecuteWorkflow_PipelineError(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	reg := module.NewStepRegistry()
	reg.Register("step.validate", module.NewValidateStepFactory())

	// A validate step that requires a field that won't be provided
	step, err := reg.Create("step.validate", "require-id", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"id"},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	pipeline := &module.Pipeline{
		Name:    "will-fail",
		Steps:   []module.PipelineStep{step},
		OnError: module.ErrorStrategyStop,
	}
	h.AddPipeline("will-fail", pipeline)

	_, err = h.ExecuteWorkflow(context.Background(), "pipeline:will-fail", "", map[string]any{})
	if err == nil {
		t.Fatal("expected error from failing pipeline")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Errorf("expected 'execution failed' in error, got: %v", err)
	}
}

func TestPipelineHandler_MultiplePipelines(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	reg := module.NewStepRegistry()
	reg.Register("step.set", module.NewSetStepFactory())

	// Pipeline A
	stepA, _ := reg.Create("step.set", "setA", map[string]any{
		"values": map[string]any{"source": "A"},
	}, nil)
	h.AddPipeline("pipeline-a", &module.Pipeline{
		Name:    "pipeline-a",
		Steps:   []module.PipelineStep{stepA},
		OnError: module.ErrorStrategyStop,
	})

	// Pipeline B
	stepB, _ := reg.Create("step.set", "setB", map[string]any{
		"values": map[string]any{"source": "B"},
	}, nil)
	h.AddPipeline("pipeline-b", &module.Pipeline{
		Name:    "pipeline-b",
		Steps:   []module.PipelineStep{stepB},
		OnError: module.ErrorStrategyStop,
	})

	// Execute each and verify correct routing
	resultA, err := h.ExecuteWorkflow(context.Background(), "pipeline:pipeline-a", "", map[string]any{})
	if err != nil {
		t.Fatalf("pipeline-a failed: %v", err)
	}
	if resultA["source"] != "A" {
		t.Errorf("expected source 'A', got %v", resultA["source"])
	}

	resultB, err := h.ExecuteWorkflow(context.Background(), "pipeline:pipeline-b", "", map[string]any{})
	if err != nil {
		t.Fatalf("pipeline-b failed: %v", err)
	}
	if resultB["source"] != "B" {
		t.Errorf("expected source 'B', got %v", resultB["source"])
	}

	// CanHandle should match both
	if !h.CanHandle("pipeline:pipeline-a") {
		t.Error("expected CanHandle true for pipeline-a")
	}
	if !h.CanHandle("pipeline:pipeline-b") {
		t.Error("expected CanHandle true for pipeline-b")
	}
}

// TestPipelineHandler_MockRunner verifies that PipelineWorkflowHandler works
// with a mock PipelineRunner without depending on concrete module types.
func TestPipelineHandler_MockRunner(t *testing.T) {
	h := NewPipelineWorkflowHandler()
	h.SetLogger(slog.Default())

	mock := &mockPipelineRunner{
		runResult: map[string]any{"mocked": true},
	}
	h.AddPipeline("mock-pipeline", mock)

	if !h.CanHandle("pipeline:mock-pipeline") {
		t.Fatal("expected CanHandle true for mock-pipeline")
	}

	result, err := h.ExecuteWorkflow(context.Background(), "pipeline:mock-pipeline", "", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["mocked"] != true {
		t.Errorf("expected mocked=true, got %v", result["mocked"])
	}
	if !mock.loggerSet {
		t.Error("expected SetLogger to be called on mock runner at configuration time")
	}
}

// TestPipelineHandler_InjectionAtConfigTime verifies that logger and recorder
// are injected into pipelines at configuration time, not on each execution.
func TestPipelineHandler_InjectionAtConfigTime(t *testing.T) {
	h := NewPipelineWorkflowHandler()

	// Add pipeline before logger/recorder are set.
	m1 := &mockPipelineRunner{runResult: map[string]any{}}
	h.AddPipeline("p1", m1)
	if m1.loggerSet || m1.recSet {
		t.Error("expected no injection before logger/recorder are set")
	}

	// SetLogger should propagate to the already-registered pipeline.
	h.SetLogger(slog.Default())
	if !m1.loggerSet {
		t.Error("expected SetLogger to propagate to existing pipeline p1")
	}

	// SetEventRecorder should propagate to the already-registered pipeline.
	h.SetEventRecorder(mockEventRecorder{})
	if !m1.recSet {
		t.Error("expected SetEventRecorder to propagate to existing pipeline p1")
	}

	// Pipeline added after both are set should receive them in AddPipeline.
	m2 := &mockPipelineRunner{runResult: map[string]any{}}
	h.AddPipeline("p2", m2)
	if !m2.loggerSet {
		t.Error("expected logger injected into p2 via AddPipeline")
	}
	if !m2.recSet {
		t.Error("expected recorder injected into p2 via AddPipeline")
	}
}
