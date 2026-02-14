package module

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockExecutor is a test double for the Executor interface.
type mockExecutor struct {
	calls   int
	mu      sync.Mutex
	results []executorResult
}

type executorResult struct {
	result map[string]any
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.calls
	m.calls++
	if idx < len(m.results) {
		return m.results[idx].result, m.results[idx].err
	}
	return nil, fmt.Errorf("unexpected call %d", idx)
}

func (m *mockExecutor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockSMEngine tracks TriggerTransition calls for test assertions.
type mockSMEngine struct {
	mu          sync.Mutex
	transitions []smTriggerCall
}

type smTriggerCall struct {
	workflowID string
	transition string
	data       map[string]any
}

func (m *mockSMEngine) recordTransition(workflowID, transition string, data map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitions = append(m.transitions, smTriggerCall{
		workflowID: workflowID,
		transition: transition,
		data:       data,
	})
}

func (m *mockSMEngine) getTransitions() []smTriggerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]smTriggerCall, len(m.transitions))
	copy(cp, m.transitions)
	return cp
}

// newTestProcessingStep creates a ProcessingStep wired with test doubles.
// It returns the step and a mock SM engine tracker.
func newTestProcessingStep(executor Executor, config ProcessingStepConfig) (*ProcessingStep, *mockSMEngine) {
	ps := NewProcessingStep("test-step", config)
	ps.executor = executor

	// Create a real state machine engine with a definition so transitions work
	smEngine := NewStateMachineEngine("test-sm")
	tracker := &mockSMEngine{}

	// Register a workflow definition with all needed states and transitions
	_ = smEngine.RegisterDefinition(&StateMachineDefinition{
		Name:         "test-workflow",
		InitialState: "pending",
		States: map[string]*State{
			"pending":    {Name: "pending"},
			"processing": {Name: "processing"},
			"completed":  {Name: "completed"},
			"failed":     {Name: "failed", IsError: true},
		},
		Transitions: map[string]*Transition{
			"start":      {Name: "start", FromState: "pending", ToState: "processing"},
			"complete":   {Name: "complete", FromState: "processing", ToState: "completed"},
			"compensate": {Name: "compensate", FromState: "processing", ToState: "failed"},
		},
	})

	// Add a listener to track fired transitions
	smEngine.AddTransitionListener(func(event TransitionEvent) {
		tracker.recordTransition(event.WorkflowID, event.TransitionID, event.Data)
	})

	ps.smEngine = smEngine
	return ps, tracker
}

func TestProcessingStep_SuccessOnFirstTry(t *testing.T) {
	executor := &mockExecutor{
		results: []executorResult{
			{result: map[string]any{"status": "ok"}, err: nil},
		},
	}

	ps, tracker := newTestProcessingStep(executor, ProcessingStepConfig{
		ComponentID:       "test-component",
		SuccessTransition: "complete",
		MaxRetries:        2,
		RetryBackoffMs:    10,
		TimeoutSeconds:    5,
	})

	// Create a workflow instance in the "processing" state so the success
	// transition (processing -> completed) is valid
	_, err := ps.smEngine.CreateWorkflow("test-workflow", "wf-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ps.smEngine.TriggerTransition(context.Background(), "wf-1", "start", nil)

	event := TransitionEvent{
		WorkflowID:   "wf-1",
		TransitionID: "start",
		FromState:    "pending",
		ToState:      "processing",
		Timestamp:    time.Now(),
		Data:         map[string]any{"order_id": "123"},
	}

	err = ps.HandleTransition(context.Background(), event)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if executor.callCount() != 1 {
		t.Fatalf("expected 1 call to executor, got %d", executor.callCount())
	}

	// Wait briefly for the async transition goroutine
	time.Sleep(50 * time.Millisecond)

	transitions := tracker.getTransitions()
	found := false
	for _, tr := range transitions {
		if tr.transition == "complete" && tr.workflowID == "wf-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected success transition 'complete' to be fired")
	}
}

func TestProcessingStep_RetryOnTransientError(t *testing.T) {
	executor := &mockExecutor{
		results: []executorResult{
			{result: nil, err: fmt.Errorf("transient error 1")},
			{result: nil, err: fmt.Errorf("transient error 2")},
			{result: map[string]any{"status": "ok"}, err: nil},
		},
	}

	ps, _ := newTestProcessingStep(executor, ProcessingStepConfig{
		ComponentID:    "test-component",
		MaxRetries:     2,
		RetryBackoffMs: 10, // very short for testing
		TimeoutSeconds: 5,
	})

	event := TransitionEvent{
		WorkflowID:   "wf-1",
		TransitionID: "start",
		FromState:    "pending",
		ToState:      "processing",
		Timestamp:    time.Now(),
	}

	err := ps.HandleTransition(context.Background(), event)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if executor.callCount() != 3 {
		t.Fatalf("expected 3 calls (1 initial + 2 retries), got %d", executor.callCount())
	}
}

func TestProcessingStep_CompensationOnPermanentFailure(t *testing.T) {
	executor := &mockExecutor{
		results: []executorResult{
			{result: nil, err: fmt.Errorf("fail 1")},
			{result: nil, err: fmt.Errorf("fail 2")},
			{result: nil, err: fmt.Errorf("fail 3")},
		},
	}

	ps, tracker := newTestProcessingStep(executor, ProcessingStepConfig{
		ComponentID:          "test-component",
		CompensateTransition: "compensate",
		MaxRetries:           2,
		RetryBackoffMs:       10,
		TimeoutSeconds:       5,
	})

	// Create a workflow instance in "processing" state
	_, err := ps.smEngine.CreateWorkflow("test-workflow", "wf-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ps.smEngine.TriggerTransition(context.Background(), "wf-1", "start", nil)

	event := TransitionEvent{
		WorkflowID:   "wf-1",
		TransitionID: "start",
		FromState:    "pending",
		ToState:      "processing",
		Timestamp:    time.Now(),
	}

	err = ps.HandleTransition(context.Background(), event)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}

	// 1 initial + 2 retries = 3 total calls
	if executor.callCount() != 3 {
		t.Fatalf("expected 3 calls, got %d", executor.callCount())
	}

	// Wait for async compensate transition
	time.Sleep(50 * time.Millisecond)

	transitions := tracker.getTransitions()
	found := false
	for _, tr := range transitions {
		if tr.transition == "compensate" && tr.workflowID == "wf-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected compensate transition to be fired")
	}
}

func TestProcessingStep_TimeoutHonored(t *testing.T) {
	// Create an executor that blocks longer than the timeout
	executor := &mockExecutor{
		results: []executorResult{
			{result: nil, err: nil}, // won't be reached
		},
	}
	// Override execute to block
	slowExecutor := &slowMockExecutor{delay: 2 * time.Second}

	ps, _ := newTestProcessingStep(slowExecutor, ProcessingStepConfig{
		ComponentID:    "test-component",
		MaxRetries:     0, // no retries
		RetryBackoffMs: 10,
		TimeoutSeconds: 1, // 1 second timeout
	})

	_ = executor // suppress unused

	event := TransitionEvent{
		WorkflowID:   "wf-1",
		TransitionID: "start",
		FromState:    "pending",
		ToState:      "processing",
		Timestamp:    time.Now(),
	}

	start := time.Now()
	err := ps.HandleTransition(context.Background(), event)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Should complete near the timeout, not at the slow executor's delay
	if elapsed > 3*time.Second {
		t.Fatalf("expected to return near timeout (1s), took %v", elapsed)
	}
}

// slowMockExecutor blocks for a configurable delay, respecting context cancellation.
type slowMockExecutor struct {
	delay time.Duration
}

func (s *slowMockExecutor) Execute(ctx context.Context, _ map[string]any) (map[string]any, error) {
	select {
	case <-time.After(s.delay):
		return map[string]any{"status": "ok"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
