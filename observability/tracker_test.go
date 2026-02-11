package observability

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- Mock ExecutionStore ---

type mockExecutionStore struct {
	mu         sync.RWMutex
	executions map[uuid.UUID]*store.WorkflowExecution
	steps      []*store.ExecutionStep
	createErr  error
}

func newMockExecutionStore() *mockExecutionStore {
	return &mockExecutionStore{
		executions: make(map[uuid.UUID]*store.WorkflowExecution),
	}
}

func (s *mockExecutionStore) CreateExecution(_ context.Context, e *store.WorkflowExecution) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executions[e.ID] = e
	return nil
}

func (s *mockExecutionStore) GetExecution(_ context.Context, id uuid.UUID) (*store.WorkflowExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.executions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return e, nil
}

func (s *mockExecutionStore) UpdateExecution(_ context.Context, e *store.WorkflowExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executions[e.ID] = e
	return nil
}

func (s *mockExecutionStore) ListExecutions(_ context.Context, _ store.ExecutionFilter) ([]*store.WorkflowExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.WorkflowExecution
	for _, e := range s.executions {
		out = append(out, e)
	}
	return out, nil
}

func (s *mockExecutionStore) CreateStep(_ context.Context, step *store.ExecutionStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
	return nil
}

func (s *mockExecutionStore) UpdateStep(_ context.Context, _ *store.ExecutionStep) error {
	return nil
}

func (s *mockExecutionStore) ListSteps(_ context.Context, execID uuid.UUID) ([]*store.ExecutionStep, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.ExecutionStep
	for _, st := range s.steps {
		if st.ExecutionID == execID {
			out = append(out, st)
		}
	}
	return out, nil
}

func (s *mockExecutionStore) CountByStatus(_ context.Context, _ uuid.UUID) (map[store.ExecutionStatus]int, error) {
	return nil, nil
}

// --- Mock LogStore ---

type mockLogStore struct {
	mu      sync.Mutex
	entries []*store.ExecutionLog
}

func (s *mockLogStore) Append(_ context.Context, l *store.ExecutionLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, l)
	return nil
}

func (s *mockLogStore) Query(_ context.Context, _ store.LogFilter) ([]*store.ExecutionLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries, nil
}

func (s *mockLogStore) CountByLevel(_ context.Context, _ uuid.UUID) (map[store.LogLevel]int, error) {
	return nil, nil
}

// --- Tests ---

func TestExecutionTracker_StartExecution_Success(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	wfID := uuid.New()
	data := json.RawMessage(`{"key":"value"}`)
	execID, err := tracker.StartExecution(context.Background(), wfID, "http", data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if execID == uuid.Nil {
		t.Fatal("expected non-nil execution ID")
	}

	exec, err := es.GetExecution(context.Background(), execID)
	if err != nil {
		t.Fatalf("expected to find execution, got %v", err)
	}
	if exec.Status != store.ExecutionStatusRunning {
		t.Errorf("expected status running, got %s", exec.Status)
	}
	if exec.WorkflowID != wfID {
		t.Errorf("expected workflow ID %s, got %s", wfID, exec.WorkflowID)
	}
}

func TestExecutionTracker_StartExecution_StoreError(t *testing.T) {
	es := newMockExecutionStore()
	es.createErr = errors.New("db connection failed")
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	_, err := tracker.StartExecution(context.Background(), uuid.New(), "http", nil)
	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestExecutionTracker_RecordStep_Success(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID := uuid.New()
	stepID := uuid.New()
	step := &store.ExecutionStep{
		ID:       stepID,
		StepName: "validate-input",
		StepType: "validation",
		Status:   store.StepStatusCompleted,
	}

	err := tracker.RecordStep(context.Background(), execID, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if step.ExecutionID != execID {
		t.Errorf("expected execution ID %s, got %s", execID, step.ExecutionID)
	}
}

func TestExecutionTracker_RecordStep_AutoID(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	step := &store.ExecutionStep{
		StepName: "process",
		StepType: "action",
		Status:   store.StepStatusCompleted,
	}

	err := tracker.RecordStep(context.Background(), uuid.New(), step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if step.ID == uuid.Nil {
		t.Error("expected auto-generated step ID")
	}
}

func TestExecutionTracker_CompleteExecution_Success(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	wfID := uuid.New()
	execID, _ := tracker.StartExecution(context.Background(), wfID, "http", nil)

	output := json.RawMessage(`{"result":"ok"}`)
	err := tracker.CompleteExecution(context.Background(), execID, output)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusCompleted {
		t.Errorf("expected completed, got %s", exec.Status)
	}
}

func TestExecutionTracker_CompleteExecution_SetsStatus(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID, _ := tracker.StartExecution(context.Background(), uuid.New(), "cron", nil)
	_ = tracker.CompleteExecution(context.Background(), execID, nil)

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusCompleted {
		t.Errorf("expected status completed, got %s", exec.Status)
	}
}

func TestExecutionTracker_CompleteExecution_CalculatesDuration(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID, _ := tracker.StartExecution(context.Background(), uuid.New(), "http", nil)

	// Small sleep so duration is non-zero
	time.Sleep(5 * time.Millisecond)

	_ = tracker.CompleteExecution(context.Background(), execID, nil)

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.DurationMs == nil {
		t.Fatal("expected duration to be set")
	}
	if *exec.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", *exec.DurationMs)
	}
	if exec.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestExecutionTracker_FailExecution_Success(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID, _ := tracker.StartExecution(context.Background(), uuid.New(), "http", nil)
	err := tracker.FailExecution(context.Background(), execID, errors.New("something broke"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusFailed {
		t.Errorf("expected failed, got %s", exec.Status)
	}
}

func TestExecutionTracker_FailExecution_SetsErrorMessage(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID, _ := tracker.StartExecution(context.Background(), uuid.New(), "http", nil)
	_ = tracker.FailExecution(context.Background(), execID, errors.New("disk full"))

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.ErrorMessage != "disk full" {
		t.Errorf("expected error message 'disk full', got '%s'", exec.ErrorMessage)
	}
}

func TestExecutionTracker_CancelExecution_Success(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	execID, _ := tracker.StartExecution(context.Background(), uuid.New(), "http", nil)
	err := tracker.CancelExecution(context.Background(), execID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusCancelled {
		t.Errorf("expected cancelled, got %s", exec.Status)
	}
	if exec.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if exec.DurationMs == nil {
		t.Error("expected duration to be set")
	}
}

func TestExecutionTracker_LogWriter_WritesLog(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	wfID := uuid.New()
	execID := uuid.New()
	w := tracker.LogWriter(wfID, execID, store.LogLevelInfo)

	n, err := w.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if n != 11 {
		t.Errorf("expected 11 bytes written, got %d", n)
	}

	if len(ls.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(ls.entries))
	}
	if ls.entries[0].Message != "hello world" {
		t.Errorf("expected message 'hello world', got '%s'", ls.entries[0].Message)
	}
	if ls.entries[0].WorkflowID != wfID {
		t.Errorf("expected workflow ID %s, got %s", wfID, ls.entries[0].WorkflowID)
	}
}

func TestExecutionTracker_LogWriter_CorrectLevel(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	levels := []store.LogLevel{store.LogLevelDebug, store.LogLevelInfo, store.LogLevelWarn, store.LogLevelError}
	for _, level := range levels {
		w := tracker.LogWriter(uuid.New(), uuid.New(), level)
		_, _ = w.Write([]byte("test"))
	}

	if len(ls.entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(ls.entries))
	}
	for i, level := range levels {
		if ls.entries[i].Level != level {
			t.Errorf("entry %d: expected level %s, got %s", i, level, ls.entries[i].Level)
		}
	}
}

func TestExecutionTracker_FullLifecycle(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	wfID := uuid.New()
	execID, err := tracker.StartExecution(context.Background(), wfID, "http", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	step := &store.ExecutionStep{
		StepName: "step1",
		StepType: "action",
		Status:   store.StepStatusCompleted,
	}
	if err := tracker.RecordStep(context.Background(), execID, step); err != nil {
		t.Fatalf("record step failed: %v", err)
	}

	w := tracker.LogWriter(wfID, execID, store.LogLevelInfo)
	_, _ = w.Write([]byte("processing complete"))

	if err := tracker.CompleteExecution(context.Background(), execID, json.RawMessage(`{"done":true}`)); err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusCompleted {
		t.Errorf("expected completed, got %s", exec.Status)
	}
	if len(ls.entries) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(ls.entries))
	}
}

func TestExecutionTracker_FullLifecycle_WithFailure(t *testing.T) {
	es := newMockExecutionStore()
	ls := &mockLogStore{}
	tracker := NewExecutionTracker(es, ls)

	wfID := uuid.New()
	execID, _ := tracker.StartExecution(context.Background(), wfID, "http", nil)

	step := &store.ExecutionStep{
		StepName: "step1",
		StepType: "action",
		Status:   store.StepStatusFailed,
	}
	_ = tracker.RecordStep(context.Background(), execID, step)

	failErr := errors.New("step1 failed: timeout")
	if err := tracker.FailExecution(context.Background(), execID, failErr); err != nil {
		t.Fatalf("fail execution failed: %v", err)
	}

	exec, _ := es.GetExecution(context.Background(), execID)
	if exec.Status != store.ExecutionStatusFailed {
		t.Errorf("expected failed, got %s", exec.Status)
	}
	if exec.ErrorMessage != "step1 failed: timeout" {
		t.Errorf("unexpected error message: %s", exec.ErrorMessage)
	}
}
