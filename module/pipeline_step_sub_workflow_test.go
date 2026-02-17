package module

import (
	"context"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

// mockStepBuilder builds a simple pipeline with an echo step for testing.
func mockStepBuilder(pipelineName string, _ *config.WorkflowConfig, _ modular.Application) (*Pipeline, error) {
	return &Pipeline{
		Name: pipelineName,
		Steps: []PipelineStep{
			&echoStep{name: "echo", output: map[string]any{"id": "pay_123", "status": "completed"}},
		},
	}, nil
}

// echoStep is a test helper that returns fixed output.
type echoStep struct {
	name   string
	output map[string]any
}

func (s *echoStep) Name() string { return s.name }
func (s *echoStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return &StepResult{Output: s.output}, nil
}

// inputCapturingStep captures the trigger data it receives for assertions.
type inputCapturingStep struct {
	name     string
	captured map[string]any
}

func (s *inputCapturingStep) Name() string { return s.name }
func (s *inputCapturingStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	s.captured = make(map[string]any)
	for k, v := range pc.TriggerData {
		s.captured[k] = v
	}
	return &StepResult{Output: map[string]any{"done": true}}, nil
}

func TestSubWorkflowStep_BasicExecution(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	_ = registry.Register("billing", plugin.EmbeddedWorkflow{
		Name:   "payment-flow",
		Config: &config.WorkflowConfig{},
	})

	factory := NewSubWorkflowStepFactory(registry, mockStepBuilder)
	step, err := factory("process-payment", map[string]any{
		"workflow": "billing:payment-flow",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"order_id": "ord_456"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// With no output mapping, result should contain all child outputs under "result"
	resultData, ok := result.Output["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to contain map, got %T", result.Output["result"])
	}
	if resultData["id"] != "pay_123" {
		t.Errorf("got id %v, want pay_123", resultData["id"])
	}
}

func TestSubWorkflowStep_InputMapping(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	captureStep := &inputCapturingStep{name: "capture"}

	builder := func(pipelineName string, _ *config.WorkflowConfig, _ modular.Application) (*Pipeline, error) {
		return &Pipeline{
			Name:  pipelineName,
			Steps: []PipelineStep{captureStep},
		}, nil
	}

	_ = registry.Register("test", plugin.EmbeddedWorkflow{
		Name:   "wf",
		Config: &config.WorkflowConfig{},
	})

	factory := NewSubWorkflowStepFactory(registry, builder)
	step, err := factory("mapped-step", map[string]any{
		"workflow": "test:wf",
		"input_mapping": map[string]any{
			"amount": "{{ .order_total }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"order_total": "99.99"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if captureStep.captured["amount"] != "99.99" {
		t.Errorf("captured amount = %v, want 99.99", captureStep.captured["amount"])
	}
}

func TestSubWorkflowStep_OutputMapping(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	_ = registry.Register("billing", plugin.EmbeddedWorkflow{
		Name:   "payment-flow",
		Config: &config.WorkflowConfig{},
	})

	factory := NewSubWorkflowStepFactory(registry, mockStepBuilder)
	step, err := factory("process-payment", map[string]any{
		"workflow": "billing:payment-flow",
		"output_mapping": map[string]any{
			"payment_id":     "id",
			"payment_status": "status",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["payment_id"] != "pay_123" {
		t.Errorf("payment_id = %v, want pay_123", result.Output["payment_id"])
	}
	if result.Output["payment_status"] != "completed" {
		t.Errorf("payment_status = %v, want completed", result.Output["payment_status"])
	}
}

func TestSubWorkflowStep_WorkflowNotFound(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()

	factory := NewSubWorkflowStepFactory(registry, mockStepBuilder)
	step, err := factory("missing", map[string]any{
		"workflow": "nonexistent:wf",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing workflow")
	}
}

func TestSubWorkflowStep_MissingWorkflowConfig(t *testing.T) {
	factory := NewSubWorkflowStepFactory(nil, nil)
	_, err := factory("no-wf", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when workflow is not specified")
	}
}

func TestSubWorkflowStep_Timeout(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	_ = registry.Register("slow", plugin.EmbeddedWorkflow{
		Name:   "slow-wf",
		Config: &config.WorkflowConfig{},
	})

	slowBuilder := func(pipelineName string, _ *config.WorkflowConfig, _ modular.Application) (*Pipeline, error) {
		return &Pipeline{
			Name: pipelineName,
			Steps: []PipelineStep{
				&sleepStep{name: "sleep", duration: 5 * time.Second},
			},
		}, nil
	}

	factory := NewSubWorkflowStepFactory(registry, slowBuilder)
	step, err := factory("timeout-step", map[string]any{
		"workflow": "slow:slow-wf",
		"timeout":  "50ms",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// sleepStep is a test helper that blocks for a duration.
type sleepStep struct {
	name     string
	duration time.Duration
}

func (s *sleepStep) Name() string { return s.name }
func (s *sleepStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	select {
	case <-time.After(s.duration):
		return &StepResult{Output: map[string]any{"slept": true}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSubWorkflowStep_ConfigYAMLFallback(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	_ = registry.Register("yaml-plugin", plugin.EmbeddedWorkflow{
		Name: "yaml-wf",
		ConfigYAML: `
modules: []
workflows: {}
triggers: {}
`,
	})

	factory := NewSubWorkflowStepFactory(registry, mockStepBuilder)
	step, err := factory("yaml-step", map[string]any{
		"workflow": "yaml-plugin:yaml-wf",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSubWorkflowStep_NestedOutputPath(t *testing.T) {
	registry := plugin.NewPluginWorkflowRegistry()
	_ = registry.Register("nested", plugin.EmbeddedWorkflow{
		Name:   "nested-wf",
		Config: &config.WorkflowConfig{},
	})

	nestedBuilder := func(pipelineName string, _ *config.WorkflowConfig, _ modular.Application) (*Pipeline, error) {
		return &Pipeline{
			Name: pipelineName,
			Steps: []PipelineStep{
				&echoStep{
					name: "echo",
					output: map[string]any{
						"result": map[string]any{
							"id":   "nested_123",
							"data": map[string]any{"key": "value"},
						},
					},
				},
			},
		}, nil
	}

	factory := NewSubWorkflowStepFactory(registry, nestedBuilder)
	step, err := factory("nested-step", map[string]any{
		"workflow": "nested:nested-wf",
		"output_mapping": map[string]any{
			"nested_id": "result.id",
			"deep_key":  "result.data.key",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["nested_id"] != "nested_123" {
		t.Errorf("nested_id = %v, want nested_123", result.Output["nested_id"])
	}
	if result.Output["deep_key"] != "value" {
		t.Errorf("deep_key = %v, want value", result.Output["deep_key"])
	}
}

func TestWalkPath(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": 42,
			},
		},
		"top": "level",
	}

	tests := []struct {
		path string
		want any
	}{
		{"top", "level"},
		{"a.b.c", 42},
		{"a.b", map[string]any{"c": 42}},
		{"missing", nil},
		{"a.missing", nil},
	}

	for _, tt := range tests {
		got := walkPath(data, tt.path)
		// Special case for map comparison
		if m, ok := tt.want.(map[string]any); ok {
			gotMap, gotOK := got.(map[string]any)
			if !gotOK {
				t.Errorf("walkPath(%q) = %v, want map", tt.path, got)
				continue
			}
			if len(gotMap) != len(m) {
				t.Errorf("walkPath(%q) map len = %d, want %d", tt.path, len(gotMap), len(m))
			}
			continue
		}
		if got != tt.want {
			t.Errorf("walkPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestSplitDotPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a", []string{"a"}},
		{"a.b.c", []string{"a", "b", "c"}},
		{"result.id", []string{"result", "id"}},
		{"single", []string{"single"}},
	}

	for _, tt := range tests {
		got := splitDotPath(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitDotPath(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitDotPath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
