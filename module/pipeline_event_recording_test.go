package module

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// recordedEvent captures a single event recorded by mockEventRecorder.
type recordedEvent struct {
	ExecutionID string
	EventType   string
	Data        map[string]any
}

// mockEventRecorder is a test double for the EventRecorder interface.
type mockEventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
	err    error // if non-nil, RecordEvent returns this error
}

func (r *mockEventRecorder) RecordEvent(_ context.Context, executionID string, eventType string, data map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, recordedEvent{
		ExecutionID: executionID,
		EventType:   eventType,
		Data:        data,
	})
	return r.err
}

func (r *mockEventRecorder) getEvents() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := make([]recordedEvent, len(r.events))
	copy(cp, r.events)
	return cp
}

func (r *mockEventRecorder) eventTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	types := make([]string, len(r.events))
	for i, e := range r.events {
		types[i] = e.EventType
	}
	return types
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestPipeline_NilEventRecorder_NoEvents(t *testing.T) {
	// Pipeline without EventRecorder should work exactly as before.
	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newMockStep("step2", map[string]any{"done": true})

	p := &Pipeline{
		Name:    "no-recorder",
		Steps:   []PipelineStep{step1, step2},
		OnError: ErrorStrategyStop,
		// EventRecorder deliberately nil
	}

	pc, err := p.Execute(context.Background(), map[string]any{"input": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc.Current["ok"] != true || pc.Current["done"] != true {
		t.Error("expected both step outputs in context")
	}
}

func TestPipeline_EventRecorder_SuccessfulExecution(t *testing.T) {
	recorder := &mockEventRecorder{}

	step1 := newMockStep("step1", map[string]any{"v": 1})
	step2 := newMockStep("step2", map[string]any{"v": 2})

	p := &Pipeline{
		Name:          "record-test",
		Steps:         []PipelineStep{step1, step2},
		OnError:       ErrorStrategyStop,
		EventRecorder: recorder,
		ExecutionID:   "exec-123",
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc == nil {
		t.Fatal("expected non-nil pipeline context")
	}

	events := recorder.getEvents()
	types := recorder.eventTypes()

	// Expected event sequence:
	// execution.started, step.started, step.completed, step.started, step.completed, execution.completed
	expectedTypes := []string{
		"execution.started",
		"step.started",
		"step.completed",
		"step.started",
		"step.completed",
		"execution.completed",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}

	// Verify execution.started data
	startEvent := events[0]
	if startEvent.ExecutionID != "exec-123" {
		t.Errorf("expected executionID=exec-123, got %q", startEvent.ExecutionID)
	}
	if startEvent.Data["pipeline"] != "record-test" {
		t.Errorf("expected pipeline=record-test, got %v", startEvent.Data["pipeline"])
	}
	if startEvent.Data["step_count"] != len(p.Steps) {
		t.Errorf("expected step_count=%d, got %v", len(p.Steps), startEvent.Data["step_count"])
	}

	// Verify step.started data for step1
	step1Started := events[1]
	if step1Started.Data["step_name"] != "step1" {
		t.Errorf("expected step_name=step1, got %v", step1Started.Data["step_name"])
	}

	// Verify step.completed has elapsed duration
	step1Completed := events[2]
	if step1Completed.Data["step_name"] != "step1" {
		t.Errorf("expected step_name=step1 in completed, got %v", step1Completed.Data["step_name"])
	}
	if _, ok := step1Completed.Data["elapsed"]; !ok {
		t.Error("expected elapsed field in step.completed event data")
	}

	// Verify execution.completed has elapsed
	execCompleted := events[len(events)-1]
	if execCompleted.Data["pipeline"] != "record-test" {
		t.Errorf("expected pipeline=record-test in completed, got %v", execCompleted.Data["pipeline"])
	}
	if _, ok := execCompleted.Data["elapsed"]; !ok {
		t.Error("expected elapsed field in execution.completed event data")
	}
}

func TestPipeline_EventRecorder_StepFailed_StopStrategy(t *testing.T) {
	recorder := &mockEventRecorder{}

	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("boom"))

	p := &Pipeline{
		Name:          "fail-test",
		Steps:         []PipelineStep{step1, step2},
		OnError:       ErrorStrategyStop,
		EventRecorder: recorder,
		ExecutionID:   "exec-fail",
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}

	types := recorder.eventTypes()

	// Expected: execution.started, step.started(step1), step.completed(step1),
	//           step.started(step2), step.failed(step2), execution.failed
	expectedTypes := []string{
		"execution.started",
		"step.started",
		"step.completed",
		"step.started",
		"step.failed",
		"execution.failed",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}

	// Verify step.failed has error info
	events := recorder.getEvents()
	stepFailed := events[4]
	if stepFailed.Data["step_name"] != "step2" {
		t.Errorf("expected step_name=step2, got %v", stepFailed.Data["step_name"])
	}
	if stepFailed.Data["error"] != "boom" {
		t.Errorf("expected error=boom, got %v", stepFailed.Data["error"])
	}
}

func TestPipeline_EventRecorder_SkipStrategy(t *testing.T) {
	recorder := &mockEventRecorder{}

	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("transient"))
	step3 := newMockStep("step3", map[string]any{"final": true})

	p := &Pipeline{
		Name:          "skip-record",
		Steps:         []PipelineStep{step1, step2, step3},
		OnError:       ErrorStrategySkip,
		EventRecorder: recorder,
		ExecutionID:   "exec-skip",
	}

	_, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	types := recorder.eventTypes()

	// Expected: execution.started,
	//   step.started(1), step.completed(1),
	//   step.started(2), step.failed(2), step.skipped(2),
	//   step.started(3), step.completed(3),
	//   execution.completed
	expectedTypes := []string{
		"execution.started",
		"step.started",
		"step.completed",
		"step.started",
		"step.failed",
		"step.skipped",
		"step.started",
		"step.completed",
		"execution.completed",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}

	// Verify the skipped event has step_name and reason
	events := recorder.getEvents()
	skippedEvent := events[5]
	if skippedEvent.Data["step_name"] != "step2" {
		t.Errorf("expected step_name=step2 in skipped event, got %v", skippedEvent.Data["step_name"])
	}
	if skippedEvent.Data["reason"] != "transient" {
		t.Errorf("expected reason=transient, got %v", skippedEvent.Data["reason"])
	}
}

func TestPipeline_EventRecorder_CompensationStrategy(t *testing.T) {
	recorder := &mockEventRecorder{}

	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newFailingStep("step2", errors.New("payment failed"))

	comp1 := newMockStep("comp1", map[string]any{})
	comp2 := newMockStep("comp2", map[string]any{})

	p := &Pipeline{
		Name:          "compensate-record",
		Steps:         []PipelineStep{step1, step2},
		OnError:       ErrorStrategyCompensate,
		Compensation:  []PipelineStep{comp1, comp2},
		EventRecorder: recorder,
		ExecutionID:   "exec-comp",
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error with compensation")
	}

	types := recorder.eventTypes()

	// Expected: execution.started,
	//   step.started(step1), step.completed(step1),
	//   step.started(step2), step.failed(step2),
	//   execution.failed,
	//   saga.compensating,
	//   step.started(comp2), step.compensated(comp2),   (reverse order)
	//   step.started(comp1), step.compensated(comp1),
	//   saga.compensated
	expectedTypes := []string{
		"execution.started",
		"step.started",
		"step.completed",
		"step.started",
		"step.failed",
		"execution.failed",
		"saga.compensating",
		"step.started",
		"step.compensated",
		"step.started",
		"step.compensated",
		"saga.compensated",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}

	// Verify compensation steps ran in reverse order
	events := recorder.getEvents()
	compStep1 := events[7] // first comp step.started = comp2 (reverse)
	if compStep1.Data["step_name"] != "comp2" {
		t.Errorf("expected first compensation step to be comp2, got %v", compStep1.Data["step_name"])
	}
	compStep2 := events[9] // second comp step.started = comp1
	if compStep2.Data["step_name"] != "comp1" {
		t.Errorf("expected second compensation step to be comp1, got %v", compStep2.Data["step_name"])
	}
}

func TestPipeline_EventRecorder_CompensationFailed(t *testing.T) {
	recorder := &mockEventRecorder{}

	step1 := newFailingStep("step1", errors.New("main error"))
	comp1 := newFailingStep("comp1", errors.New("comp error"))

	p := &Pipeline{
		Name:          "comp-fail-record",
		Steps:         []PipelineStep{step1},
		OnError:       ErrorStrategyCompensate,
		Compensation:  []PipelineStep{comp1},
		EventRecorder: recorder,
		ExecutionID:   "exec-comp-fail",
	}

	_, err := p.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}

	types := recorder.eventTypes()

	// Expected: execution.started,
	//   step.started(step1), step.failed(step1),
	//   execution.failed,
	//   saga.compensating,
	//   step.started(comp1), step.failed(comp1)
	//   (no saga.compensated because comp failed)
	expectedTypes := []string{
		"execution.started",
		"step.started",
		"step.failed",
		"execution.failed",
		"saga.compensating",
		"step.started",
		"step.failed",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}

	// Verify compensation step.failed has step_type=compensation
	events := recorder.getEvents()
	compFailed := events[6]
	if compFailed.Data["step_type"] != "compensation" {
		t.Errorf("expected step_type=compensation, got %v", compFailed.Data["step_type"])
	}
}

func TestPipeline_EventRecorder_ErrorsDoNotBreakPipeline(t *testing.T) {
	// Even when the recorder returns errors, the pipeline should succeed.
	recorder := &mockEventRecorder{
		err: fmt.Errorf("storage unavailable"),
	}

	step1 := newMockStep("step1", map[string]any{"ok": true})
	step2 := newMockStep("step2", map[string]any{"done": true})

	p := &Pipeline{
		Name:          "recorder-error",
		Steps:         []PipelineStep{step1, step2},
		OnError:       ErrorStrategyStop,
		EventRecorder: recorder,
		ExecutionID:   "exec-err",
	}

	pc, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("pipeline should succeed despite recorder errors: %v", err)
	}

	// Pipeline output should still be correct
	if pc.Current["ok"] != true || pc.Current["done"] != true {
		t.Error("expected both step outputs in context despite recorder errors")
	}

	// Events were still attempted (the mock records them even when returning error)
	events := recorder.getEvents()
	if len(events) == 0 {
		t.Error("expected events to be recorded even when returning error")
	}
}

func TestPipeline_EventRecorder_ExecutionID_PassedThrough(t *testing.T) {
	recorder := &mockEventRecorder{}

	p := &Pipeline{
		Name:          "id-test",
		Steps:         []PipelineStep{newMockStep("step1", map[string]any{})},
		OnError:       ErrorStrategyStop,
		EventRecorder: recorder,
		ExecutionID:   "my-unique-id-42",
	}

	_, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := recorder.getEvents()
	for i, ev := range events {
		if ev.ExecutionID != "my-unique-id-42" {
			t.Errorf("event[%d]: expected executionID=my-unique-id-42, got %q", i, ev.ExecutionID)
		}
	}
}

func TestPipeline_EventRecorder_EmptyPipeline(t *testing.T) {
	recorder := &mockEventRecorder{}

	p := &Pipeline{
		Name:          "empty-recorded",
		Steps:         []PipelineStep{},
		OnError:       ErrorStrategyStop,
		EventRecorder: recorder,
		ExecutionID:   "exec-empty",
	}

	_, err := p.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	types := recorder.eventTypes()

	// Even an empty pipeline should record start and complete
	expectedTypes := []string{
		"execution.started",
		"execution.completed",
	}

	if len(types) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedTypes), len(types), types)
	}

	for i, expected := range expectedTypes {
		if types[i] != expected {
			t.Errorf("event[%d]: expected %q, got %q", i, expected, types[i])
		}
	}
}
