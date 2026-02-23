package module

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fixedOutputPipeline returns a *Pipeline that always executes a single step
// producing the given output map.
func fixedOutputPipeline(name string, output map[string]any) *Pipeline {
	return &Pipeline{
		Name:  name,
		Steps: []PipelineStep{&echoStep{name: "echo", output: output}},
	}
}

// capturingPipeline returns a *Pipeline whose only step captures the trigger
// data it receives. The captured map is written to *captured after Execute.
func capturingPipeline(name string, captured *map[string]any) *Pipeline {
	cs := &inputCapturingStep{name: "capture"}
	p := &Pipeline{Name: name, Steps: []PipelineStep{cs}}
	// After Execute, copy step's captured data to the output pointer.
	_ = captured
	// We return a pipeline with a wrapper step that updates *captured.
	p.Steps = []PipelineStep{&triggerDataCapture{inner: cs, out: captured}}
	return p
}

// triggerDataCapture wraps inputCapturingStep and copies result to *out.
type triggerDataCapture struct {
	inner *inputCapturingStep
	out   *map[string]any
}

func (t *triggerDataCapture) Name() string { return t.inner.Name() }
func (t *triggerDataCapture) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	r, err := t.inner.Execute(ctx, pc)
	if t.out != nil {
		*t.out = t.inner.captured
	}
	return r, err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWorkflowCallStep_MissingWorkflowField(t *testing.T) {
	factory := NewWorkflowCallStepFactory(func(name string) (*Pipeline, bool) { return nil, false })
	_, err := factory("missing-wf", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when workflow field is missing")
	}
}

func TestWorkflowCallStep_PipelineNotFound(t *testing.T) {
	lookup := func(name string) (*Pipeline, bool) { return nil, false }
	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("call", map[string]any{"workflow": "nonexistent"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when pipeline not found")
	}
}

func TestWorkflowCallStep_NilLookup(t *testing.T) {
	factory := NewWorkflowCallStepFactory(nil)
	step, err := factory("call", map[string]any{"workflow": "some-pipeline"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when lookup is nil")
	}
}

func TestWorkflowCallStep_SyncMode_DefaultOutput(t *testing.T) {
	target := fixedOutputPipeline("target", map[string]any{"status": "ok", "id": "123"})
	lookup := func(name string) (*Pipeline, bool) {
		if name == "target" {
			return target, true
		}
		return nil, false
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("call-target", map[string]any{
		"workflow": "target",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"order_id": "ORD-001"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// With no output_mapping, all child outputs are returned under "result"
	resultMap, ok := result.Output["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be map[string]any, got %T", result.Output["result"])
	}
	if resultMap["status"] != "ok" {
		t.Errorf("result.status = %v, want ok", resultMap["status"])
	}
	if resultMap["id"] != "123" {
		t.Errorf("result.id = %v, want 123", resultMap["id"])
	}
}

func TestWorkflowCallStep_SyncMode_OutputMapping(t *testing.T) {
	target := fixedOutputPipeline("target", map[string]any{"responder_id": "agent-42", "queue": "tier1"})
	lookup := func(name string) (*Pipeline, bool) {
		return target, name == "target"
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("assign", map[string]any{
		"workflow": "target",
		"output_mapping": map[string]any{
			"assigned_responder": "responder_id",
			"assigned_queue":     "queue",
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

	if result.Output["assigned_responder"] != "agent-42" {
		t.Errorf("assigned_responder = %v, want agent-42", result.Output["assigned_responder"])
	}
	if result.Output["assigned_queue"] != "tier1" {
		t.Errorf("assigned_queue = %v, want tier1", result.Output["assigned_queue"])
	}
}

func TestWorkflowCallStep_SyncMode_InputMapping(t *testing.T) {
	var captured map[string]any
	target := capturingPipeline("target", &captured)
	lookup := func(name string) (*Pipeline, bool) {
		return target, name == "target"
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("call-with-input", map[string]any{
		"workflow": "target",
		"input": map[string]any{
			"conversation_id": "{{ .conv_id }}",
			"priority":        "{{ .urgency }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"conv_id": "CONV-99",
		"urgency": "high",
	}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if captured["conversation_id"] != "CONV-99" {
		t.Errorf("conversation_id = %v, want CONV-99", captured["conversation_id"])
	}
	if captured["priority"] != "high" {
		t.Errorf("priority = %v, want high", captured["priority"])
	}
}

func TestWorkflowCallStep_SyncMode_PassthroughInput(t *testing.T) {
	// When no input mapping is specified, all current context data is passed through
	var captured map[string]any
	target := capturingPipeline("target", &captured)
	lookup := func(name string) (*Pipeline, bool) {
		return target, name == "target"
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("passthrough", map[string]any{
		"workflow": "target",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"order_id": "ORD-007",
		"amount":   "49.99",
	}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if captured["order_id"] != "ORD-007" {
		t.Errorf("order_id passthrough = %v, want ORD-007", captured["order_id"])
	}
	if captured["amount"] != "49.99" {
		t.Errorf("amount passthrough = %v, want 49.99", captured["amount"])
	}
}

func TestWorkflowCallStep_AsyncMode(t *testing.T) {
	done := make(chan struct{}, 1)
	asyncPipeline := &Pipeline{
		Name: "async-target",
		Steps: []PipelineStep{
			&callbackStep{
				name: "notify",
				fn: func() {
					done <- struct{}{}
				},
			},
		},
	}
	lookup := func(name string) (*Pipeline, bool) {
		return asyncPipeline, name == "async-target"
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("fire-and-forget", map[string]any{
		"workflow": "async-target",
		"mode":     "async",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Should return immediately with dispatch confirmation
	if result.Output["dispatched"] != true {
		t.Errorf("expected dispatched=true, got %v", result.Output["dispatched"])
	}
	if result.Output["mode"] != "async" {
		t.Errorf("expected mode=async, got %v", result.Output["mode"])
	}

	// Wait for the async pipeline to actually run
	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("async pipeline did not execute within timeout")
	}
}

func TestWorkflowCallStep_Timeout(t *testing.T) {
	slowPipeline := &Pipeline{
		Name: "slow",
		Steps: []PipelineStep{
			&sleepStep{name: "sleep", duration: 5 * time.Second},
		},
	}
	lookup := func(name string) (*Pipeline, bool) {
		return slowPipeline, name == "slow"
	}

	factory := NewWorkflowCallStepFactory(lookup)
	step, err := factory("timeout-call", map[string]any{
		"workflow": "slow",
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

func TestWorkflowCallStep_Name(t *testing.T) {
	factory := NewWorkflowCallStepFactory(func(name string) (*Pipeline, bool) { return nil, false })
	step, err := factory("my-call-step", map[string]any{"workflow": "target"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "my-call-step" {
		t.Errorf("Name() = %q, want my-call-step", step.Name())
	}
}

func TestWorkflowCallStep_DefaultMode_IsSync(t *testing.T) {
	target := fixedOutputPipeline("tgt", map[string]any{"done": true})
	factory := NewWorkflowCallStepFactory(func(name string) (*Pipeline, bool) {
		return target, name == "tgt"
	})
	step, err := factory("no-mode", map[string]any{"workflow": "tgt"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	// Sync mode: result should contain child outputs, not dispatch info
	if _, ok := result.Output["dispatched"]; ok {
		t.Error("expected sync mode but got async dispatch confirmation")
	}
	if result.Output["result"] == nil {
		t.Error("expected sync result to have 'result' key")
	}
}

func TestWorkflowCallStep_ChildError_PropagatesInSync(t *testing.T) {
	failPipeline := &Pipeline{
		Name: "fail",
		Steps: []PipelineStep{
			&failStep{name: "boom", err: fmt.Errorf("child workflow error")},
		},
	}
	factory := NewWorkflowCallStepFactory(func(name string) (*Pipeline, bool) {
		return failPipeline, name == "fail"
	})
	step, err := factory("call-fail", map[string]any{"workflow": "fail"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from failing child workflow")
	}
}

// ---------------------------------------------------------------------------
// Additional test step helpers
// ---------------------------------------------------------------------------

// callbackStep calls a callback function when executed.
type callbackStep struct {
	name string
	fn   func()
}

func (s *callbackStep) Name() string { return s.name }
func (s *callbackStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	if s.fn != nil {
		s.fn()
	}
	return &StepResult{Output: map[string]any{"called": true}}, nil
}

// failStep always returns an error.
type failStep struct {
	name string
	err  error
}

func (s *failStep) Name() string { return s.name }
func (s *failStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return nil, s.err
}
