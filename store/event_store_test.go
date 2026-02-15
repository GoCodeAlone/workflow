package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// eventStoreFactory creates an EventStore implementation for testing.
type eventStoreFactory struct {
	name   string
	create func(t *testing.T) EventStore
}

func eventStoreFactories(t *testing.T) []eventStoreFactory {
	t.Helper()
	return []eventStoreFactory{
		{
			name:   "InMemory",
			create: func(_ *testing.T) EventStore { return NewInMemoryEventStore() },
		},
		{
			name: "SQLite",
			create: func(t *testing.T) EventStore {
				t.Helper()
				dir := t.TempDir()
				dbPath := filepath.Join(dir, "test_events.db")
				store, err := NewSQLiteEventStore(dbPath)
				if err != nil {
					t.Fatalf("NewSQLiteEventStore: %v", err)
				}
				t.Cleanup(func() { store.Close() })
				return store
			},
		},
	}
}

func appendStarted(t *testing.T, s EventStore, execID uuid.UUID, pipeline, tenantID string) {
	t.Helper()
	data := map[string]any{"pipeline": pipeline, "tenant_id": tenantID}
	if err := s.Append(context.Background(), execID, EventExecutionStarted, data); err != nil {
		t.Fatalf("Append execution.started: %v", err)
	}
}

func appendStepStarted(t *testing.T, s EventStore, execID uuid.UUID, stepName string) {
	t.Helper()
	data := map[string]any{"step_name": stepName, "step_type": "http"}
	if err := s.Append(context.Background(), execID, EventStepStarted, data); err != nil {
		t.Fatalf("Append step.started: %v", err)
	}
}

func appendStepCompleted(t *testing.T, s EventStore, execID uuid.UUID, stepName string) {
	t.Helper()
	data := map[string]any{"step_name": stepName}
	if err := s.Append(context.Background(), execID, EventStepCompleted, data); err != nil {
		t.Fatalf("Append step.completed: %v", err)
	}
}

func appendCompleted(t *testing.T, s EventStore, execID uuid.UUID) {
	t.Helper()
	if err := s.Append(context.Background(), execID, EventExecutionCompleted, map[string]any{}); err != nil {
		t.Fatalf("Append execution.completed: %v", err)
	}
}

func appendFailed(t *testing.T, s EventStore, execID uuid.UUID, errMsg string) {
	t.Helper()
	data := map[string]any{"error": errMsg}
	if err := s.Append(context.Background(), execID, EventExecutionFailed, data); err != nil {
		t.Fatalf("Append execution.failed: %v", err)
	}
}

// ===========================================================================
// TestAppendAndGetEvents
// ===========================================================================

func TestAppendAndGetEvents(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			// Append several events.
			appendStarted(t, s, execID, "order-pipeline", "tenant-1")
			appendStepStarted(t, s, execID, "validate")
			appendStepCompleted(t, s, execID, "validate")
			appendCompleted(t, s, execID)

			// Retrieve and verify.
			events, err := s.GetEvents(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetEvents: %v", err)
			}
			if len(events) != 4 {
				t.Fatalf("expected 4 events, got %d", len(events))
			}

			// Verify event types in order.
			expectedTypes := []string{
				EventExecutionStarted,
				EventStepStarted,
				EventStepCompleted,
				EventExecutionCompleted,
			}
			for i, et := range expectedTypes {
				if events[i].EventType != et {
					t.Errorf("event[%d]: expected type %q, got %q", i, et, events[i].EventType)
				}
			}

			// Verify all events have the correct execution ID.
			for i, ev := range events {
				if ev.ExecutionID != execID {
					t.Errorf("event[%d]: expected executionID %v, got %v", i, execID, ev.ExecutionID)
				}
				if ev.ID == uuid.Nil {
					t.Errorf("event[%d]: expected non-nil event ID", i)
				}
				if ev.CreatedAt.IsZero() {
					t.Errorf("event[%d]: expected non-zero CreatedAt", i)
				}
				if len(ev.EventData) == 0 {
					t.Errorf("event[%d]: expected non-empty EventData", i)
				}
			}
		})
	}
}

func TestAppendAndGetEvents_EmptyExecution(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			events, err := s.GetEvents(context.Background(), uuid.New())
			if err != nil {
				t.Fatalf("GetEvents: %v", err)
			}
			if len(events) != 0 {
				t.Errorf("expected nil or empty slice, got %d events", len(events))
			}
		})
	}
}

// ===========================================================================
// TestGetTimeline
// ===========================================================================

func TestGetTimeline(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			// Build a realistic execution event stream.
			appendStarted(t, s, execID, "order-pipeline", "tenant-1")

			appendStepStarted(t, s, execID, "validate")
			if err := s.Append(context.Background(), execID, EventStepInputRecorded, map[string]any{
				"step_name": "validate",
				"input":     map[string]any{"order_id": "123"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.Append(context.Background(), execID, EventStepOutputRecorded, map[string]any{
				"step_name": "validate",
				"output":    map[string]any{"valid": true},
			}); err != nil {
				t.Fatal(err)
			}
			appendStepCompleted(t, s, execID, "validate")

			appendStepStarted(t, s, execID, "process")
			appendStepCompleted(t, s, execID, "process")

			appendCompleted(t, s, execID)

			// Get timeline.
			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}

			if timeline.ExecutionID != execID {
				t.Errorf("expected executionID %v, got %v", execID, timeline.ExecutionID)
			}
			if timeline.Pipeline != "order-pipeline" {
				t.Errorf("expected pipeline 'order-pipeline', got %q", timeline.Pipeline)
			}
			if timeline.TenantID != "tenant-1" {
				t.Errorf("expected tenantID 'tenant-1', got %q", timeline.TenantID)
			}
			if timeline.Status != "completed" {
				t.Errorf("expected status 'completed', got %q", timeline.Status)
			}
			if timeline.StartedAt == nil {
				t.Error("expected non-nil StartedAt")
			}
			if timeline.CompletedAt == nil {
				t.Error("expected non-nil CompletedAt")
			}
			if len(timeline.Steps) != 2 {
				t.Fatalf("expected 2 steps, got %d", len(timeline.Steps))
			}

			// Verify first step.
			step0 := timeline.Steps[0]
			if step0.StepName != "validate" {
				t.Errorf("step[0]: expected name 'validate', got %q", step0.StepName)
			}
			if step0.Status != "completed" {
				t.Errorf("step[0]: expected status 'completed', got %q", step0.Status)
			}
			if step0.StepType != "http" {
				t.Errorf("step[0]: expected type 'http', got %q", step0.StepType)
			}
			if step0.InputData == nil {
				t.Error("step[0]: expected non-nil InputData")
			}
			if step0.OutputData == nil {
				t.Error("step[0]: expected non-nil OutputData")
			}

			// Verify second step.
			step1 := timeline.Steps[1]
			if step1.StepName != "process" {
				t.Errorf("step[1]: expected name 'process', got %q", step1.StepName)
			}
			if step1.Status != "completed" {
				t.Errorf("step[1]: expected status 'completed', got %q", step1.Status)
			}

			if timeline.EventCount != 8 {
				t.Errorf("expected 8 events, got %d", timeline.EventCount)
			}
		})
	}
}

func TestGetTimeline_FailedExecution(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			appendStarted(t, s, execID, "pipeline-x", "")

			appendStepStarted(t, s, execID, "step1")
			if err := s.Append(context.Background(), execID, EventStepFailed, map[string]any{
				"step_name": "step1",
				"error":     "connection timeout",
			}); err != nil {
				t.Fatal(err)
			}

			appendFailed(t, s, execID, "step1 failed: connection timeout")

			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}

			if timeline.Status != "failed" {
				t.Errorf("expected status 'failed', got %q", timeline.Status)
			}
			if timeline.Error != "step1 failed: connection timeout" {
				t.Errorf("expected error message, got %q", timeline.Error)
			}
			if len(timeline.Steps) != 1 {
				t.Fatalf("expected 1 step, got %d", len(timeline.Steps))
			}
			if timeline.Steps[0].Status != "failed" {
				t.Errorf("step: expected status 'failed', got %q", timeline.Steps[0].Status)
			}
			if timeline.Steps[0].Error != "connection timeout" {
				t.Errorf("step: expected error 'connection timeout', got %q", timeline.Steps[0].Error)
			}
		})
	}
}

func TestGetTimeline_SkippedAndRetry(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			appendStarted(t, s, execID, "retry-pipeline", "")

			// Step with retries.
			appendStepStarted(t, s, execID, "flaky")
			if err := s.Append(context.Background(), execID, EventRetryAttempted, map[string]any{
				"step_name": "flaky",
				"attempt":   1,
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.Append(context.Background(), execID, EventRetryAttempted, map[string]any{
				"step_name": "flaky",
				"attempt":   2,
			}); err != nil {
				t.Fatal(err)
			}
			appendStepCompleted(t, s, execID, "flaky")

			// Skipped step.
			if err := s.Append(context.Background(), execID, EventStepSkipped, map[string]any{
				"step_name": "optional",
				"reason":    "condition not met",
			}); err != nil {
				t.Fatal(err)
			}

			appendCompleted(t, s, execID)

			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}

			if len(timeline.Steps) != 2 {
				t.Fatalf("expected 2 steps, got %d", len(timeline.Steps))
			}

			// Flaky step should have 2 retries.
			if timeline.Steps[0].Retries != 2 {
				t.Errorf("expected 2 retries, got %d", timeline.Steps[0].Retries)
			}
			if timeline.Steps[0].Status != "completed" {
				t.Errorf("expected status 'completed', got %q", timeline.Steps[0].Status)
			}

			// Optional step should be skipped.
			if timeline.Steps[1].Status != "skipped" {
				t.Errorf("expected status 'skipped', got %q", timeline.Steps[1].Status)
			}
			if timeline.Steps[1].Error != "condition not met" {
				t.Errorf("expected reason 'condition not met', got %q", timeline.Steps[1].Error)
			}
		})
	}
}

func TestGetTimeline_SagaCompensation(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			appendStarted(t, s, execID, "saga-pipeline", "")
			appendStepStarted(t, s, execID, "charge")
			appendStepCompleted(t, s, execID, "charge")
			appendStepStarted(t, s, execID, "ship")
			if err := s.Append(context.Background(), execID, EventStepFailed, map[string]any{
				"step_name": "ship",
				"error":     "out of stock",
			}); err != nil {
				t.Fatal(err)
			}

			if err := s.Append(context.Background(), execID, EventSagaCompensating, map[string]any{}); err != nil {
				t.Fatal(err)
			}
			if err := s.Append(context.Background(), execID, EventStepCompensated, map[string]any{
				"step_name": "charge",
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.Append(context.Background(), execID, EventSagaCompensated, map[string]any{}); err != nil {
				t.Fatal(err)
			}

			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}

			if timeline.Status != "compensated" {
				t.Errorf("expected status 'compensated', got %q", timeline.Status)
			}
			if len(timeline.Steps) != 2 {
				t.Fatalf("expected 2 steps, got %d", len(timeline.Steps))
			}
			if timeline.Steps[0].Status != "compensated" {
				t.Errorf("charge step: expected status 'compensated', got %q", timeline.Steps[0].Status)
			}
			if timeline.Steps[1].Status != "failed" {
				t.Errorf("ship step: expected status 'failed', got %q", timeline.Steps[1].Status)
			}
		})
	}
}

func TestGetTimeline_NotFound(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			_, err := s.GetTimeline(context.Background(), uuid.New())
			if err != ErrNotFound {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestListExecutions
// ===========================================================================

func TestListExecutions(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)

			// Create several executions.
			exec1 := uuid.New()
			appendStarted(t, s, exec1, "pipeline-a", "tenant-1")
			appendCompleted(t, s, exec1)

			exec2 := uuid.New()
			appendStarted(t, s, exec2, "pipeline-b", "tenant-2")
			appendFailed(t, s, exec2, "something broke")

			exec3 := uuid.New()
			appendStarted(t, s, exec3, "pipeline-a", "tenant-1")
			appendCompleted(t, s, exec3)

			// List all.
			all, err := s.ListExecutions(context.Background(), ExecutionEventFilter{})
			if err != nil {
				t.Fatalf("ListExecutions (all): %v", err)
			}
			if len(all) != 3 {
				t.Fatalf("expected 3 executions, got %d", len(all))
			}

			// Filter by pipeline.
			byPipeline, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Pipeline: "pipeline-a"})
			if err != nil {
				t.Fatalf("ListExecutions (pipeline): %v", err)
			}
			if len(byPipeline) != 2 {
				t.Fatalf("expected 2 executions for pipeline-a, got %d", len(byPipeline))
			}

			// Filter by tenant.
			byTenant, err := s.ListExecutions(context.Background(), ExecutionEventFilter{TenantID: "tenant-2"})
			if err != nil {
				t.Fatalf("ListExecutions (tenant): %v", err)
			}
			if len(byTenant) != 1 {
				t.Fatalf("expected 1 execution for tenant-2, got %d", len(byTenant))
			}

			// Filter by status.
			byStatus, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Status: "failed"})
			if err != nil {
				t.Fatalf("ListExecutions (status): %v", err)
			}
			if len(byStatus) != 1 {
				t.Fatalf("expected 1 failed execution, got %d", len(byStatus))
			}
			if byStatus[0].ExecutionID != exec2 {
				t.Errorf("expected exec2, got %v", byStatus[0].ExecutionID)
			}
		})
	}
}

func TestListExecutions_Pagination(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)

			// Create 5 executions.
			for i := 0; i < 5; i++ {
				execID := uuid.New()
				appendStarted(t, s, execID, "pipeline", "")
				appendCompleted(t, s, execID)
			}

			// Limit.
			limited, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Limit: 2})
			if err != nil {
				t.Fatalf("ListExecutions (limit): %v", err)
			}
			if len(limited) != 2 {
				t.Fatalf("expected 2 executions, got %d", len(limited))
			}

			// Offset.
			offset, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Offset: 3})
			if err != nil {
				t.Fatalf("ListExecutions (offset): %v", err)
			}
			if len(offset) != 2 {
				t.Fatalf("expected 2 executions with offset 3, got %d", len(offset))
			}

			// Offset beyond range.
			beyond, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Offset: 100})
			if err != nil {
				t.Fatalf("ListExecutions (beyond): %v", err)
			}
			if len(beyond) != 0 {
				t.Fatalf("expected empty result, got %d", len(beyond))
			}
		})
	}
}

func TestListExecutions_TimeFilter(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)

			execID := uuid.New()
			appendStarted(t, s, execID, "pipeline", "")
			appendCompleted(t, s, execID)

			// Since in the far future should return nothing.
			future := time.Now().Add(time.Hour)
			results, err := s.ListExecutions(context.Background(), ExecutionEventFilter{Since: &future})
			if err != nil {
				t.Fatalf("ListExecutions (since future): %v", err)
			}
			if len(results) != 0 {
				t.Fatalf("expected 0 executions, got %d", len(results))
			}

			// Until in the far past should return nothing.
			past := time.Now().Add(-time.Hour)
			results, err = s.ListExecutions(context.Background(), ExecutionEventFilter{Until: &past})
			if err != nil {
				t.Fatalf("ListExecutions (until past): %v", err)
			}
			if len(results) != 0 {
				t.Fatalf("expected 0 executions, got %d", len(results))
			}

			// Broad range should include.
			since := time.Now().Add(-time.Minute)
			until := time.Now().Add(time.Minute)
			results, err = s.ListExecutions(context.Background(), ExecutionEventFilter{Since: &since, Until: &until})
			if err != nil {
				t.Fatalf("ListExecutions (broad range): %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 execution, got %d", len(results))
			}
		})
	}
}

// ===========================================================================
// TestSequenceOrdering
// ===========================================================================

func TestSequenceOrdering(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			// Append many events.
			for i := 0; i < 20; i++ {
				data := map[string]any{"index": i}
				var eventType string
				if i == 0 {
					eventType = EventExecutionStarted
					data["pipeline"] = "seq-test"
				} else if i == 19 {
					eventType = EventExecutionCompleted
				} else if i%2 == 1 {
					eventType = EventStepStarted
					data["step_name"] = "step"
				} else {
					eventType = EventStepCompleted
					data["step_name"] = "step"
				}
				if err := s.Append(context.Background(), execID, eventType, data); err != nil {
					t.Fatalf("Append[%d]: %v", i, err)
				}
			}

			events, err := s.GetEvents(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetEvents: %v", err)
			}
			if len(events) != 20 {
				t.Fatalf("expected 20 events, got %d", len(events))
			}

			// Verify strict monotonically increasing sequence numbers.
			for i := 0; i < len(events)-1; i++ {
				if events[i].SequenceNum >= events[i+1].SequenceNum {
					t.Errorf("sequence ordering violated at %d: %d >= %d",
						i, events[i].SequenceNum, events[i+1].SequenceNum)
				}
			}

			// Verify first sequence starts at 1.
			if events[0].SequenceNum != 1 {
				t.Errorf("expected first sequence to be 1, got %d", events[0].SequenceNum)
			}

			// Verify last sequence is 20.
			if events[19].SequenceNum != 20 {
				t.Errorf("expected last sequence to be 20, got %d", events[19].SequenceNum)
			}
		})
	}
}

func TestSequenceOrdering_IndependentExecutions(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)

			exec1 := uuid.New()
			exec2 := uuid.New()

			// Interleave appends across two executions.
			appendStarted(t, s, exec1, "p1", "")
			appendStarted(t, s, exec2, "p2", "")
			appendStepStarted(t, s, exec1, "step-a")
			appendStepStarted(t, s, exec2, "step-b")
			appendStepCompleted(t, s, exec1, "step-a")
			appendStepCompleted(t, s, exec2, "step-b")

			// Each execution should have independent sequence numbers starting at 1.
			events1, _ := s.GetEvents(context.Background(), exec1)
			events2, _ := s.GetEvents(context.Background(), exec2)

			if len(events1) != 3 || len(events2) != 3 {
				t.Fatalf("expected 3 events each, got %d and %d", len(events1), len(events2))
			}

			for _, events := range [][]ExecutionEvent{events1, events2} {
				if events[0].SequenceNum != 1 {
					t.Errorf("expected first seq 1, got %d", events[0].SequenceNum)
				}
				if events[1].SequenceNum != 2 {
					t.Errorf("expected second seq 2, got %d", events[1].SequenceNum)
				}
				if events[2].SequenceNum != 3 {
					t.Errorf("expected third seq 3, got %d", events[2].SequenceNum)
				}
			}
		})
	}
}

// ===========================================================================
// TestConcurrentAppend
// ===========================================================================

func TestConcurrentAppend(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			// First append a started event so we have valid state.
			appendStarted(t, s, execID, "concurrent-test", "")

			const goroutines = 10
			const eventsPerGoroutine = 20

			var wg sync.WaitGroup
			wg.Add(goroutines)

			for g := 0; g < goroutines; g++ {
				go func(gID int) {
					defer wg.Done()
					for i := 0; i < eventsPerGoroutine; i++ {
						data := map[string]any{
							"goroutine": gID,
							"index":     i,
							"step_name": "concurrent-step",
						}
						if err := s.Append(context.Background(), execID, EventStepStarted, data); err != nil {
							t.Errorf("goroutine %d, event %d: %v", gID, i, err)
							return
						}
					}
				}(g)
			}

			wg.Wait()

			events, err := s.GetEvents(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetEvents: %v", err)
			}

			// 1 started + goroutines*eventsPerGoroutine
			expectedCount := 1 + goroutines*eventsPerGoroutine
			if len(events) != expectedCount {
				t.Fatalf("expected %d events, got %d", expectedCount, len(events))
			}

			// Verify all sequence numbers are unique and monotonically increasing.
			seen := make(map[int64]bool)
			for i, ev := range events {
				if seen[ev.SequenceNum] {
					t.Errorf("duplicate sequence number %d at index %d", ev.SequenceNum, i)
				}
				seen[ev.SequenceNum] = true

				if i > 0 && ev.SequenceNum <= events[i-1].SequenceNum {
					t.Errorf("sequence ordering violated at %d: %d <= %d",
						i, ev.SequenceNum, events[i-1].SequenceNum)
				}
			}
		})
	}
}

func TestConcurrentAppend_MultipleExecutions(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)

			const execCount = 5
			const eventsPerExec = 10

			execIDs := make([]uuid.UUID, execCount)
			for i := range execIDs {
				execIDs[i] = uuid.New()
			}

			var wg sync.WaitGroup
			wg.Add(execCount)

			for e := 0; e < execCount; e++ {
				go func(idx int) {
					defer wg.Done()
					appendStarted(t, s, execIDs[idx], "concurrent-multi", "")
					for i := 0; i < eventsPerExec-1; i++ {
						data := map[string]any{"step_name": "step", "i": i}
						if err := s.Append(context.Background(), execIDs[idx], EventStepStarted, data); err != nil {
							t.Errorf("exec %d, event %d: %v", idx, i, err)
							return
						}
					}
				}(e)
			}

			wg.Wait()

			// Each execution should have exactly eventsPerExec events.
			for _, eid := range execIDs {
				events, err := s.GetEvents(context.Background(), eid)
				if err != nil {
					t.Fatalf("GetEvents(%v): %v", eid, err)
				}
				if len(events) != eventsPerExec {
					t.Errorf("execution %v: expected %d events, got %d", eid, eventsPerExec, len(events))
				}
			}

			// ListExecutions should find all of them.
			all, err := s.ListExecutions(context.Background(), ExecutionEventFilter{})
			if err != nil {
				t.Fatalf("ListExecutions: %v", err)
			}
			if len(all) != execCount {
				t.Errorf("expected %d executions, got %d", execCount, len(all))
			}
		})
	}
}

// ===========================================================================
// TestConditionalRouted
// ===========================================================================

func TestGetTimeline_ConditionalRouted(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			appendStarted(t, s, execID, "router-pipeline", "")
			appendStepStarted(t, s, execID, "router")
			if err := s.Append(context.Background(), execID, EventConditionalRouted, map[string]any{
				"step_name": "router",
				"route":     "branch-a",
			}); err != nil {
				t.Fatal(err)
			}
			appendStepCompleted(t, s, execID, "router")
			appendCompleted(t, s, execID)

			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}

			if len(timeline.Steps) != 1 {
				t.Fatalf("expected 1 step, got %d", len(timeline.Steps))
			}
			if timeline.Steps[0].Route != "branch-a" {
				t.Errorf("expected route 'branch-a', got %q", timeline.Steps[0].Route)
			}
		})
	}
}

// ===========================================================================
// TestSQLiteEventStore specific
// ===========================================================================

func TestSQLiteEventStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// Create store and add events.
	s1, err := NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore: %v", err)
	}

	execID := uuid.New()
	appendStarted(t, s1, execID, "persist-test", "")
	appendCompleted(t, s1, execID)
	s1.Close()

	// Reopen and verify events are persisted.
	s2, err := NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore (reopen): %v", err)
	}
	defer s2.Close()

	events, err := s2.GetEvents(context.Background(), execID)
	if err != nil {
		t.Fatalf("GetEvents after reopen: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after reopen, got %d", len(events))
	}
}

func TestSQLiteEventStore_FromDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fromdb.db") + "?_journal_mode=WAL&_busy_timeout=5000"

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	s, err := NewSQLiteEventStoreFromDB(db)
	if err != nil {
		t.Fatalf("NewSQLiteEventStoreFromDB: %v", err)
	}

	execID := uuid.New()
	appendStarted(t, s, execID, "from-db", "")

	events, err := s.GetEvents(context.Background(), execID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestSQLiteEventStore_BadPath(t *testing.T) {
	// Using a path that cannot exist.
	_, err := NewSQLiteEventStore(filepath.Join(os.DevNull, "impossible", "path.db"))
	if err == nil {
		t.Fatal("expected error for bad path, got nil")
	}
}

// ===========================================================================
// TestExecutionCancelled
// ===========================================================================

func TestGetTimeline_Cancelled(t *testing.T) {
	for _, f := range eventStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			execID := uuid.New()

			appendStarted(t, s, execID, "cancel-test", "")
			appendStepStarted(t, s, execID, "long-step")
			if err := s.Append(context.Background(), execID, EventExecutionCancelled, map[string]any{}); err != nil {
				t.Fatal(err)
			}

			timeline, err := s.GetTimeline(context.Background(), execID)
			if err != nil {
				t.Fatalf("GetTimeline: %v", err)
			}
			if timeline.Status != "cancelled" {
				t.Errorf("expected status 'cancelled', got %q", timeline.Status)
			}
			if timeline.CompletedAt == nil {
				t.Error("expected non-nil CompletedAt for cancelled execution")
			}
		})
	}
}

// ===========================================================================
// Compile-time interface check
// ===========================================================================

func TestEventStore_InterfaceCompliance(t *testing.T) {
	var _ EventStore = (*InMemoryEventStore)(nil)
	var _ EventStore = (*SQLiteEventStore)(nil)
}
