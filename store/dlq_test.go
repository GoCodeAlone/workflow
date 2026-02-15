package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type dlqStoreFactory struct {
	name   string
	create func(t *testing.T) DLQStore
}

func dlqStoreFactories(t *testing.T) []dlqStoreFactory {
	t.Helper()
	return []dlqStoreFactory{
		{
			name:   "InMemory",
			create: func(_ *testing.T) DLQStore { return NewInMemoryDLQStore() },
		},
		{
			name: "SQLite",
			create: func(t *testing.T) DLQStore {
				t.Helper()
				dir := t.TempDir()
				dbPath := filepath.Join(dir, "test_dlq.db")
				store, err := NewSQLiteDLQStore(dbPath)
				if err != nil {
					t.Fatalf("NewSQLiteDLQStore: %v", err)
				}
				t.Cleanup(func() { store.Close() })
				return store
			},
		},
	}
}

func makeDLQEntry(pipeline, step, errMsg, errType string) *DLQEntry {
	return &DLQEntry{
		ID:            uuid.New(),
		OriginalEvent: json.RawMessage(`{"type":"test.event","data":{"key":"value"}}`),
		PipelineName:  pipeline,
		StepName:      step,
		ErrorMessage:  errMsg,
		ErrorType:     errType,
		MaxRetries:    3,
		Status:        DLQStatusPending,
		Metadata:      map[string]any{"source": "test"},
	}
}

// ===========================================================================
// TestDLQAdd
// ===========================================================================

func TestDLQAdd(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("order-pipeline", "validate", "validation failed", "validation")
			err := s.Add(ctx, entry)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			// Verify it was stored.
			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after Add: %v", err)
			}

			if got.PipelineName != "order-pipeline" {
				t.Errorf("expected pipeline 'order-pipeline', got %q", got.PipelineName)
			}
			if got.StepName != "validate" {
				t.Errorf("expected step 'validate', got %q", got.StepName)
			}
			if got.ErrorMessage != "validation failed" {
				t.Errorf("expected error 'validation failed', got %q", got.ErrorMessage)
			}
			if got.ErrorType != "validation" {
				t.Errorf("expected error_type 'validation', got %q", got.ErrorType)
			}
			if got.Status != DLQStatusPending {
				t.Errorf("expected status 'pending', got %q", got.Status)
			}
			if got.MaxRetries != 3 {
				t.Errorf("expected max_retries 3, got %d", got.MaxRetries)
			}
			if got.RetryCount != 0 {
				t.Errorf("expected retry_count 0, got %d", got.RetryCount)
			}
			if got.CreatedAt.IsZero() {
				t.Error("expected non-zero created_at")
			}
			if got.UpdatedAt.IsZero() {
				t.Error("expected non-zero updated_at")
			}
			if got.ResolvedAt != nil {
				t.Error("expected nil resolved_at")
			}
			if string(got.OriginalEvent) == "" {
				t.Error("expected non-empty original_event")
			}
		})
	}
}

// ===========================================================================
// TestDLQGet
// ===========================================================================

func TestDLQGet(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "timeout", "timeout")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.ID != entry.ID {
				t.Errorf("expected ID %v, got %v", entry.ID, got.ID)
			}
			if got.PipelineName != "pipeline-a" {
				t.Errorf("expected pipeline 'pipeline-a', got %q", got.PipelineName)
			}
		})
	}
}

// ===========================================================================
// TestDLQGetNotFound
// ===========================================================================

func TestDLQGetNotFound(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			_, err := s.Get(ctx, uuid.New())
			if err != ErrNotFound {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestDLQListFilter
// ===========================================================================

func TestDLQListFilter(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			// Add entries with different attributes.
			entries := []*DLQEntry{
				makeDLQEntry("pipeline-a", "step-1", "err1", "validation"),
				makeDLQEntry("pipeline-a", "step-2", "err2", "timeout"),
				makeDLQEntry("pipeline-b", "step-1", "err3", "validation"),
				makeDLQEntry("pipeline-b", "step-3", "err4", "system"),
			}
			for _, e := range entries {
				if err := s.Add(ctx, e); err != nil {
					t.Fatalf("Add: %v", err)
				}
			}

			// Filter by pipeline.
			byPipeline, err := s.List(ctx, DLQFilter{PipelineName: "pipeline-a"})
			if err != nil {
				t.Fatalf("List (pipeline): %v", err)
			}
			if len(byPipeline) != 2 {
				t.Fatalf("expected 2 entries for pipeline-a, got %d", len(byPipeline))
			}

			// Filter by status.
			byStatus, err := s.List(ctx, DLQFilter{Status: DLQStatusPending})
			if err != nil {
				t.Fatalf("List (status): %v", err)
			}
			if len(byStatus) != 4 {
				t.Fatalf("expected 4 pending entries, got %d", len(byStatus))
			}

			// Filter by error type.
			byErrorType, err := s.List(ctx, DLQFilter{ErrorType: "validation"})
			if err != nil {
				t.Fatalf("List (error_type): %v", err)
			}
			if len(byErrorType) != 2 {
				t.Fatalf("expected 2 validation entries, got %d", len(byErrorType))
			}

			// Filter by step.
			byStep, err := s.List(ctx, DLQFilter{StepName: "step-1"})
			if err != nil {
				t.Fatalf("List (step): %v", err)
			}
			if len(byStep) != 2 {
				t.Fatalf("expected 2 entries for step-1, got %d", len(byStep))
			}

			// Combined filter: pipeline + error_type.
			combined, err := s.List(ctx, DLQFilter{PipelineName: "pipeline-a", ErrorType: "timeout"})
			if err != nil {
				t.Fatalf("List (combined): %v", err)
			}
			if len(combined) != 1 {
				t.Fatalf("expected 1 entry for pipeline-a+timeout, got %d", len(combined))
			}

			// Limit/offset.
			limited, err := s.List(ctx, DLQFilter{Limit: 2})
			if err != nil {
				t.Fatalf("List (limit): %v", err)
			}
			if len(limited) != 2 {
				t.Fatalf("expected 2 entries with limit, got %d", len(limited))
			}

			offset, err := s.List(ctx, DLQFilter{Offset: 2})
			if err != nil {
				t.Fatalf("List (offset): %v", err)
			}
			if len(offset) != 2 {
				t.Fatalf("expected 2 entries with offset 2, got %d", len(offset))
			}

			// Empty result.
			empty, err := s.List(ctx, DLQFilter{PipelineName: "nonexistent"})
			if err != nil {
				t.Fatalf("List (nonexistent): %v", err)
			}
			if len(empty) != 0 {
				t.Fatalf("expected 0 entries, got %d", len(empty))
			}
		})
	}
}

// ===========================================================================
// TestDLQCount
// ===========================================================================

func TestDLQCount(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			// Empty store.
			count, err := s.Count(ctx, DLQFilter{})
			if err != nil {
				t.Fatalf("Count (empty): %v", err)
			}
			if count != 0 {
				t.Fatalf("expected 0, got %d", count)
			}

			// Add entries.
			for i := 0; i < 5; i++ {
				pipeline := "pipeline-a"
				if i >= 3 {
					pipeline = "pipeline-b"
				}
				e := makeDLQEntry(pipeline, "step", "error", "system")
				if err := s.Add(ctx, e); err != nil {
					t.Fatalf("Add: %v", err)
				}
			}

			// Total count.
			total, err := s.Count(ctx, DLQFilter{})
			if err != nil {
				t.Fatalf("Count (total): %v", err)
			}
			if total != 5 {
				t.Fatalf("expected 5, got %d", total)
			}

			// Filtered count.
			filtered, err := s.Count(ctx, DLQFilter{PipelineName: "pipeline-a"})
			if err != nil {
				t.Fatalf("Count (filtered): %v", err)
			}
			if filtered != 3 {
				t.Fatalf("expected 3, got %d", filtered)
			}
		})
	}
}

// ===========================================================================
// TestDLQRetry
// ===========================================================================

func TestDLQRetry(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "timeout", "timeout")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			// Retry once.
			if err := s.Retry(ctx, entry.ID); err != nil {
				t.Fatalf("Retry: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after Retry: %v", err)
			}
			if got.RetryCount != 1 {
				t.Errorf("expected retry_count 1, got %d", got.RetryCount)
			}
			if got.Status != DLQStatusRetrying {
				t.Errorf("expected status 'retrying', got %q", got.Status)
			}

			// Retry again.
			if err := s.Retry(ctx, entry.ID); err != nil {
				t.Fatalf("Retry (2nd): %v", err)
			}

			got2, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after 2nd Retry: %v", err)
			}
			if got2.RetryCount != 2 {
				t.Errorf("expected retry_count 2, got %d", got2.RetryCount)
			}

			// Retry nonexistent.
			if err := s.Retry(ctx, uuid.New()); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound for nonexistent retry, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestDLQDiscard
// ===========================================================================

func TestDLQDiscard(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "bad data", "validation")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			if err := s.Discard(ctx, entry.ID); err != nil {
				t.Fatalf("Discard: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after Discard: %v", err)
			}
			if got.Status != DLQStatusDiscarded {
				t.Errorf("expected status 'discarded', got %q", got.Status)
			}

			// Discard nonexistent.
			if err := s.Discard(ctx, uuid.New()); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound for nonexistent discard, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestDLQResolve
// ===========================================================================

func TestDLQResolve(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "transient error", "step_failure")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			if err := s.Resolve(ctx, entry.ID); err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after Resolve: %v", err)
			}
			if got.Status != DLQStatusResolved {
				t.Errorf("expected status 'resolved', got %q", got.Status)
			}
			if got.ResolvedAt == nil {
				t.Error("expected non-nil resolved_at after Resolve")
			}

			// Resolve nonexistent.
			if err := s.Resolve(ctx, uuid.New()); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound for nonexistent resolve, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestDLQPurge
// ===========================================================================

func TestDLQPurge(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			// Add entries: 2 resolved, 1 discarded, 1 pending.
			e1 := makeDLQEntry("pipeline-a", "step-1", "err1", "system")
			e2 := makeDLQEntry("pipeline-a", "step-2", "err2", "system")
			e3 := makeDLQEntry("pipeline-b", "step-1", "err3", "validation")
			e4 := makeDLQEntry("pipeline-b", "step-2", "err4", "timeout")

			for _, e := range []*DLQEntry{e1, e2, e3, e4} {
				if err := s.Add(ctx, e); err != nil {
					t.Fatalf("Add: %v", err)
				}
			}

			// Resolve e1 and e2, discard e3, leave e4 pending.
			if err := s.Resolve(ctx, e1.ID); err != nil {
				t.Fatalf("Resolve e1: %v", err)
			}
			if err := s.Resolve(ctx, e2.ID); err != nil {
				t.Fatalf("Resolve e2: %v", err)
			}
			if err := s.Discard(ctx, e3.ID); err != nil {
				t.Fatalf("Discard e3: %v", err)
			}

			// Purge with 0 duration should remove all resolved/discarded.
			purged, err := s.Purge(ctx, 0)
			if err != nil {
				t.Fatalf("Purge: %v", err)
			}
			if purged != 3 {
				t.Fatalf("expected 3 purged, got %d", purged)
			}

			// Only e4 (pending) should remain.
			remaining, err := s.List(ctx, DLQFilter{})
			if err != nil {
				t.Fatalf("List after Purge: %v", err)
			}
			if len(remaining) != 1 {
				t.Fatalf("expected 1 remaining, got %d", len(remaining))
			}
			if remaining[0].ID != e4.ID {
				t.Errorf("expected remaining entry to be e4, got %v", remaining[0].ID)
			}

			// Purge with large duration should not remove the pending entry.
			purged2, err := s.Purge(ctx, 0)
			if err != nil {
				t.Fatalf("Purge (2nd): %v", err)
			}
			if purged2 != 0 {
				t.Fatalf("expected 0 purged on 2nd run, got %d", purged2)
			}
		})
	}
}

func TestDLQPurge_OnlyOlderEntries(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			// Add and resolve an entry.
			entry := makeDLQEntry("pipeline-a", "step-1", "err", "system")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}
			if err := s.Resolve(ctx, entry.ID); err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			// Purge with a very large duration (entries are recent, so nothing should be purged).
			purged, err := s.Purge(ctx, 24*time.Hour)
			if err != nil {
				t.Fatalf("Purge: %v", err)
			}
			if purged != 0 {
				t.Fatalf("expected 0 purged (entries are recent), got %d", purged)
			}

			// Entry should still exist.
			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get after Purge: %v", err)
			}
			if got.Status != DLQStatusResolved {
				t.Errorf("expected status 'resolved', got %q", got.Status)
			}
		})
	}
}

// ===========================================================================
// TestDLQConcurrentAccess
// ===========================================================================

func TestDLQConcurrentAccess(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			const goroutines = 10
			const entriesPerGoroutine = 10

			var wg sync.WaitGroup
			wg.Add(goroutines)

			for g := 0; g < goroutines; g++ {
				go func(gID int) {
					defer wg.Done()
					for i := 0; i < entriesPerGoroutine; i++ {
						entry := makeDLQEntry(
							"concurrent-pipeline",
							"step",
							"concurrent error",
							"system",
						)
						if err := s.Add(ctx, entry); err != nil {
							t.Errorf("goroutine %d, entry %d: Add: %v", gID, i, err)
							return
						}
					}
				}(g)
			}

			wg.Wait()

			// Verify all entries were stored.
			total, err := s.Count(ctx, DLQFilter{})
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			expectedCount := int64(goroutines * entriesPerGoroutine)
			if total != expectedCount {
				t.Fatalf("expected %d entries, got %d", expectedCount, total)
			}

			// Concurrent reads and writes.
			entries, err := s.List(ctx, DLQFilter{Limit: 20})
			if err != nil {
				t.Fatalf("List: %v", err)
			}

			wg.Add(len(entries))
			for _, entry := range entries {
				go func(id uuid.UUID) {
					defer wg.Done()
					// Mix of operations.
					_, _ = s.Get(ctx, id)
					_ = s.Retry(ctx, id)
					_, _ = s.Count(ctx, DLQFilter{})
				}(entry.ID)
			}
			wg.Wait()

			// Everything should still be consistent.
			finalTotal, err := s.Count(ctx, DLQFilter{})
			if err != nil {
				t.Fatalf("final Count: %v", err)
			}
			if finalTotal != expectedCount {
				t.Fatalf("expected %d entries after concurrent ops, got %d", expectedCount, finalTotal)
			}
		})
	}
}

// ===========================================================================
// TestDLQUpdateStatus
// ===========================================================================

func TestDLQUpdateStatus(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "err", "system")
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			// Update to retrying.
			if err := s.UpdateStatus(ctx, entry.ID, DLQStatusRetrying); err != nil {
				t.Fatalf("UpdateStatus: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Status != DLQStatusRetrying {
				t.Errorf("expected status 'retrying', got %q", got.Status)
			}

			// Update nonexistent.
			if err := s.UpdateStatus(ctx, uuid.New(), DLQStatusResolved); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

// ===========================================================================
// TestDLQMetadata
// ===========================================================================

func TestDLQMetadata(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := makeDLQEntry("pipeline-a", "step-1", "err", "system")
			entry.Metadata = map[string]any{
				"trace_id":  "abc123",
				"tenant_id": "tenant-1",
				"count":     float64(42),
			}
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Metadata == nil {
				t.Fatal("expected non-nil metadata")
			}
			if got.Metadata["trace_id"] != "abc123" {
				t.Errorf("expected trace_id 'abc123', got %v", got.Metadata["trace_id"])
			}
			if got.Metadata["tenant_id"] != "tenant-1" {
				t.Errorf("expected tenant_id 'tenant-1', got %v", got.Metadata["tenant_id"])
			}
			if got.Metadata["count"] != float64(42) {
				t.Errorf("expected count 42, got %v", got.Metadata["count"])
			}
		})
	}
}

// ===========================================================================
// TestDLQ_AutoID
// ===========================================================================

func TestDLQAutoID(t *testing.T) {
	for _, f := range dlqStoreFactories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.create(t)
			ctx := context.Background()

			entry := &DLQEntry{
				OriginalEvent: json.RawMessage(`{}`),
				PipelineName:  "pipeline",
				StepName:      "step",
				ErrorMessage:  "err",
				ErrorType:     "system",
				MaxRetries:    3,
			}
			if err := s.Add(ctx, entry); err != nil {
				t.Fatalf("Add: %v", err)
			}

			if entry.ID == uuid.Nil {
				t.Error("expected auto-generated ID")
			}

			got, err := s.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.ID != entry.ID {
				t.Errorf("expected ID %v, got %v", entry.ID, got.ID)
			}
		})
	}
}

// ===========================================================================
// Compile-time interface assertions
// ===========================================================================

func TestDLQStore_InterfaceCompliance(t *testing.T) {
	var _ DLQStore = (*InMemoryDLQStore)(nil)
	var _ DLQStore = (*SQLiteDLQStore)(nil)
}
