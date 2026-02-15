package module

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockStep is a configurable test double for PipelineStep.
type mockStep struct {
	name    string
	execFn  func(ctx context.Context, pc *PipelineContext) (*StepResult, error)
	execLog []string // records each call
}

func newMockStep(name string, output map[string]any) *mockStep {
	return &mockStep{
		name: name,
		execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{Output: output}, nil
		},
	}
}

func newFailingStep(name string, err error) *mockStep {
	return &mockStep{
		name: name,
		execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return nil, err
		},
	}
}

func (m *mockStep) Name() string { return m.name }

func (m *mockStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	m.execLog = append(m.execLog, m.name)
	return m.execFn(ctx, pc)
}

func TestPipeline_SimpleTwoStepExecution(t *testing.T) {
	step1 := newMockStep("step1", map[string]any{"result1": "one"})
	step2 := newMockStep("step2", map[string]any{"result2": "two"})

	p := &Pipeline{
		Name:    "simple-pipeline",
		Steps:   []PipelineStep{step1, step2},
		OnError: ErrorStrategyStop,
	}

	pc, err := p.Execute(context.Background(), map[string]any{"input": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both steps should have executed
	if len(step1.execLog) != 1 {
		t.Errorf("expected step1 to execute once, got %d times", len(step1.execLog))
	}
	if len(step2.execLog) != 1 {
		t.Errorf("expected step2 to execute once, got %d times", len(step2.execLog))
	}

	// Context should have all data
	if pc.Current["input"] != "data" {
		t.Errorf("expected trigger data in Current")
	}
	if pc.Current["result1"] != "one" {
		t.Errorf("expected step1 output in Current")
	}
	if pc.Current["result2"] != "two" {
		t.Errorf("expected step2 output in Current")
	}

	// Step outputs should be individually accessible
	if pc.StepOutputs["step1"]["result1"] != "one" {
		t.Errorf("expected step1 output in StepOutputs")
	}
	if pc.StepOutputs["step2"]["result2"] != "two" {
		t.Errorf("expected step2 output in StepOutputs")
	}
}

func TestPipeline_ThreeStepExecution_OrderMatters(t *testing.T) {
	var executionOrder []string

	makeStep := func(name string) *mockStep {
		ms := &mockStep{name: name}
		ms.execFn = func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			executionOrder = append(executionOrder, name)
			return &StepResult{Output: map[string]any{name: true}}, nil
		}
		return ms
	}

	p := &Pipeline{
		Name:    "ordered-pipeline",
		Steps:   []PipelineStep{makeStep("a"), makeStep("b"), makeStep("c")},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executionOrder))
	}
	if executionOrder[0] != "a" || executionOrder[1] != "b" || executionOrder[2] != "c" {
		t.Errorf("expected execution order [a, b, c], got %v", executionOrder)
	}
}

func TestPipeline_StepOutputMergesIntoContextForNextStep(t *testing.T) {
	var capturedCurrent map[string]any

	step1 := newMockStep("step1", map[string]any{"from_step1": "hello"})
	step2 := &mockStep{
		name: "step2",
		execFn: func(_ context.Context, pc *PipelineContext) (*StepResult, error) {
			// Capture what step2 sees in Current
			capturedCurrent = make(map[string]any)
			for k, v := range pc.Current {
				capturedCurrent[k] = v
			}
			return &StepResult{Output: map[string]any{}}, nil
		},
	}

	p := &Pipeline{
		Name:    "merge-test",
		Steps:   []PipelineStep{step1, step2},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), map[string]any{"trigger_key": "trigger_val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedCurrent["trigger_key"] != "trigger_val" {
		t.Errorf("step2 should see trigger data in Current")
	}
	if capturedCurrent["from_step1"] != "hello" {
		t.Errorf("step2 should see step1's output in Current")
	}
}

func TestPipeline_ErrorStrategyStop_HaltsOnFirstError(t *testing.T) {
	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("validation failed"))
	step3 := newMockStep("step3", map[string]any{"never": "reached"})

	p := &Pipeline{
		Name:    "stop-pipeline",
		Steps:   []PipelineStep{step1, step2, step3},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from pipeline")
	}

	if !strings.Contains(err.Error(), "step \"step2\" failed") {
		t.Errorf("expected error to reference step2, got: %v", err)
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected original error in message, got: %v", err)
	}

	// step3 should NOT have executed
	if len(step3.execLog) != 0 {
		t.Errorf("step3 should not have executed after stop, but executed %d times", len(step3.execLog))
	}
}

func TestPipeline_ErrorStrategySkip_ContinuesPastErrors(t *testing.T) {
	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("transient error"))
	step3 := newMockStep("step3", map[string]any{"final": "result"})

	p := &Pipeline{
		Name:    "skip-pipeline",
		Steps:   []PipelineStep{step1, step2, step3},
		OnError: ErrorStrategySkip,
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error with skip strategy, got: %v", err)
	}

	// step3 should have executed
	if len(step3.execLog) != 1 {
		t.Errorf("step3 should have executed once, got %d", len(step3.execLog))
	}

	// The skipped step should have error metadata in its output
	step2Out, ok := pc.StepOutputs["step2"]
	if !ok {
		t.Fatal("expected StepOutputs to contain step2 even when skipped")
	}
	if step2Out["_error"] != "transient error" {
		t.Errorf("expected _error='transient error', got %v", step2Out["_error"])
	}
	if step2Out["_skipped"] != true {
		t.Errorf("expected _skipped=true, got %v", step2Out["_skipped"])
	}

	// Final result should be present
	if pc.Current["final"] != "result" {
		t.Errorf("expected final result in Current after skip")
	}
}

func TestPipeline_ErrorStrategyCompensate_RunsCompensationInReverse(t *testing.T) {
	var compensationOrder []string

	makeCompStep := func(name string) *mockStep {
		ms := &mockStep{name: name}
		ms.execFn = func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			compensationOrder = append(compensationOrder, name)
			return &StepResult{Output: map[string]any{}}, nil
		}
		return ms
	}

	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("payment failed"))

	comp1 := makeCompStep("comp1")
	comp2 := makeCompStep("comp2")
	comp3 := makeCompStep("comp3")

	p := &Pipeline{
		Name:         "compensate-pipeline",
		Steps:        []PipelineStep{step1, step2},
		OnError:      ErrorStrategyCompensate,
		Compensation: []PipelineStep{comp1, comp2, comp3},
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from pipeline with compensation")
	}

	if !strings.Contains(err.Error(), "compensation executed") {
		t.Errorf("expected 'compensation executed' in error, got: %v", err)
	}

	// Compensation should run in reverse order
	if len(compensationOrder) != 3 {
		t.Fatalf("expected 3 compensation steps, got %d", len(compensationOrder))
	}
	if compensationOrder[0] != "comp3" || compensationOrder[1] != "comp2" || compensationOrder[2] != "comp1" {
		t.Errorf("expected compensation order [comp3, comp2, comp1], got %v", compensationOrder)
	}
}

func TestPipeline_ErrorStrategyCompensate_CompensationAlsoFails(t *testing.T) {
	step1 := newFailingStep("step1", errors.New("main error"))
	comp1 := newFailingStep("comp1", errors.New("comp error"))

	p := &Pipeline{
		Name:         "double-fail",
		Steps:        []PipelineStep{step1},
		OnError:      ErrorStrategyCompensate,
		Compensation: []PipelineStep{comp1},
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "main error") {
		t.Errorf("expected original error in message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "compensation also failed") {
		t.Errorf("expected 'compensation also failed' in message, got: %v", err)
	}
}

func TestPipeline_ConditionalRouting_NextStep(t *testing.T) {
	var executionOrder []string
	makeStep := func(name string, nextStep string) *mockStep {
		ms := &mockStep{name: name}
		ms.execFn = func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			executionOrder = append(executionOrder, name)
			return &StepResult{
				Output:   map[string]any{name: true},
				NextStep: nextStep,
			}, nil
		}
		return ms
	}

	// step1 jumps to step3, skipping step2
	step1 := makeStep("step1", "step3")
	step2 := makeStep("step2", "")
	step3 := makeStep("step3", "")

	p := &Pipeline{
		Name:    "routing-pipeline",
		Steps:   []PipelineStep{step1, step2, step3},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// step2 should be skipped
	if len(executionOrder) != 2 {
		t.Fatalf("expected 2 steps executed, got %d: %v", len(executionOrder), executionOrder)
	}
	if executionOrder[0] != "step1" || executionOrder[1] != "step3" {
		t.Errorf("expected [step1, step3], got %v", executionOrder)
	}
}

func TestPipeline_StopSignal_HaltsPipeline(t *testing.T) {
	step1 := &mockStep{
		name: "step1",
		execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{"done": true},
				Stop:   true,
			}, nil
		},
	}
	step2 := newMockStep("step2", map[string]any{"unreachable": true})

	p := &Pipeline{
		Name:    "stop-signal",
		Steps:   []PipelineStep{step1, step2},
		OnError: ErrorStrategyStop,
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// step2 should not have run
	if len(step2.execLog) != 0 {
		t.Errorf("step2 should not execute after Stop signal")
	}

	// step1 output should be recorded
	if pc.Current["done"] != true {
		t.Errorf("expected step1 output in context")
	}
}

func TestPipeline_Timeout_CancelsExecution(t *testing.T) {
	step1 := &mockStep{
		name: "slow-step",
		execFn: func(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
			select {
			case <-time.After(5 * time.Second):
				return &StepResult{Output: map[string]any{}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	p := &Pipeline{
		Name:    "timeout-pipeline",
		Steps:   []PipelineStep{step1},
		OnError: ErrorStrategyStop,
		Timeout: 50 * time.Millisecond,
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected deadline exceeded error, got: %v", err)
	}
}

func TestPipeline_Timeout_ContextCancelledBetweenSteps(t *testing.T) {
	// The pipeline checks context cancellation before each step.
	// So a pre-cancelled context should stop immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	step1 := newMockStep("step1", map[string]any{})

	p := &Pipeline{
		Name:    "pre-cancelled",
		Steps:   []PipelineStep{step1},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(ctx, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected 'cancelled' in error, got: %v", err)
	}
	if len(step1.execLog) != 0 {
		t.Errorf("step1 should not execute with a pre-cancelled context")
	}
}

func TestPipeline_UnknownNextStep_ReturnsError(t *testing.T) {
	step1 := &mockStep{
		name: "step1",
		execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output:   map[string]any{},
				NextStep: "nonexistent",
			}, nil
		},
	}

	p := &Pipeline{
		Name:    "bad-route",
		Steps:   []PipelineStep{step1},
		OnError: ErrorStrategyStop,
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for unknown next step")
	}
	if !strings.Contains(err.Error(), "unknown step") {
		t.Errorf("expected 'unknown step' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected step name 'nonexistent' in error, got: %v", err)
	}
}

func TestPipeline_MetadataSetOnCompletion(t *testing.T) {
	p := &Pipeline{
		Name:    "meta-test",
		Steps:   []PipelineStep{newMockStep("step1", map[string]any{})},
		OnError: ErrorStrategyStop,
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pc.Metadata["pipeline"] != "meta-test" {
		t.Errorf("expected pipeline name in metadata, got %v", pc.Metadata["pipeline"])
	}
	if _, ok := pc.Metadata["started_at"]; !ok {
		t.Error("expected started_at in metadata")
	}
	if _, ok := pc.Metadata["completed_at"]; !ok {
		t.Error("expected completed_at in metadata")
	}
}

func TestPipeline_EmptySteps_Succeeds(t *testing.T) {
	p := &Pipeline{
		Name:    "empty",
		Steps:   []PipelineStep{},
		OnError: ErrorStrategyStop,
	}

	pc, err := p.Execute(context.Background(), map[string]any{"input": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc.Current["input"] != "val" {
		t.Errorf("expected trigger data preserved in empty pipeline")
	}
}

func TestPipeline_CompensateWithNoCompensationSteps(t *testing.T) {
	step1 := newFailingStep("step1", errors.New("fail"))

	p := &Pipeline{
		Name:         "no-comp",
		Steps:        []PipelineStep{step1},
		OnError:      ErrorStrategyCompensate,
		Compensation: nil,
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should include "compensation executed" since compensation ran (successfully, with 0 steps)
	if !strings.Contains(err.Error(), "compensation executed") {
		t.Errorf("expected 'compensation executed' in error, got: %v", err)
	}
}
