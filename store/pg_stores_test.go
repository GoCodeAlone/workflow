package store

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Shared integration test helper
// ---------------------------------------------------------------------------

// newTestPGPool opens a pgxpool connection using the PG_URL env var.
// The test is skipped when PG_URL is not set.
func newTestPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pgURL := os.Getenv("PG_URL")
	if pgURL == "" {
		t.Skip("PG_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ===========================================================================
// PGEventStore integration tests
// ===========================================================================

func TestPGEventStore_Integration(t *testing.T) {
	pool := newTestPGPool(t)
	ctx := context.Background()

	store, err := NewPGEventStore(pool)
	if err != nil {
		t.Fatalf("NewPGEventStore: %v", err)
	}

	execID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM execution_events WHERE execution_id = $1`, execID)
	})

	// Append events.
	appendStarted(t, store, execID, "pg-pipeline", "pg-tenant")
	appendStepStarted(t, store, execID, "step-a")
	if err := store.Append(ctx, execID, EventStepInputRecorded, map[string]any{
		"step_name": "step-a",
		"input":     map[string]any{"order_id": "pg-001"},
	}); err != nil {
		t.Fatalf("Append input: %v", err)
	}
	if err := store.Append(ctx, execID, EventStepOutputRecorded, map[string]any{
		"step_name": "step-a",
		"output":    map[string]any{"valid": true},
	}); err != nil {
		t.Fatalf("Append output: %v", err)
	}
	appendStepCompleted(t, store, execID, "step-a")
	appendCompleted(t, store, execID)

	// GetEvents.
	events, err := store.GetEvents(ctx, execID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(events))
	}
	for i, ev := range events {
		if ev.ExecutionID != execID {
			t.Errorf("event[%d]: wrong execution_id", i)
		}
		if ev.SequenceNum != int64(i+1) {
			t.Errorf("event[%d]: expected sequence %d, got %d", i, i+1, ev.SequenceNum)
		}
		if ev.CreatedAt.IsZero() {
			t.Errorf("event[%d]: zero created_at", i)
		}
		if len(ev.EventData) == 0 {
			t.Errorf("event[%d]: empty event_data", i)
		}
	}

	// GetTimeline.
	timeline, err := store.GetTimeline(ctx, execID)
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	if timeline.Pipeline != "pg-pipeline" {
		t.Errorf("Timeline.Pipeline = %q, want 'pg-pipeline'", timeline.Pipeline)
	}
	if timeline.TenantID != "pg-tenant" {
		t.Errorf("Timeline.TenantID = %q, want 'pg-tenant'", timeline.TenantID)
	}
	if timeline.Status != "completed" {
		t.Errorf("Timeline.Status = %q, want 'completed'", timeline.Status)
	}
	if len(timeline.Steps) != 1 || timeline.Steps[0].StepName != "step-a" {
		t.Errorf("Timeline.Steps = %v, want [{step-a}]", timeline.Steps)
	}
	if timeline.Steps[0].InputData == nil {
		t.Error("step InputData should not be nil")
	}

	// GetTimeline on unknown execution.
	_, err = store.GetTimeline(ctx, uuid.New())
	if err != ErrNotFound {
		t.Errorf("GetTimeline missing: got %v, want ErrNotFound", err)
	}

	// ListExecutions.
	list, err := store.ListExecutions(ctx, ExecutionEventFilter{Pipeline: "pg-pipeline"})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	found := false
	for _, m := range list {
		if m.ExecutionID == execID {
			found = true
		}
	}
	if !found {
		t.Error("expected to find execID in ListExecutions")
	}
}

func TestPGEventStore_ConcurrentAppend(t *testing.T) {
	pool := newTestPGPool(t)
	ctx := context.Background()

	store, err := NewPGEventStore(pool)
	if err != nil {
		t.Fatalf("NewPGEventStore: %v", err)
	}

	execID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM execution_events WHERE execution_id = $1`, execID)
	})

	const goroutines = 10
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = store.Append(ctx, execID, EventStepStarted, map[string]any{
				"step_name": "concurrent",
			})
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d Append error: %v", i, e)
		}
	}

	events, err := store.GetEvents(ctx, execID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != goroutines {
		t.Fatalf("expected %d events, got %d", goroutines, len(events))
	}

	// Verify sequence numbers are unique and consecutive.
	seen := make(map[int64]bool)
	for _, ev := range events {
		if seen[ev.SequenceNum] {
			t.Errorf("duplicate sequence_num %d", ev.SequenceNum)
		}
		seen[ev.SequenceNum] = true
	}
	for i := int64(1); i <= goroutines; i++ {
		if !seen[i] {
			t.Errorf("missing sequence_num %d", i)
		}
	}
}

// ===========================================================================
// PGAPIKeyStore integration tests
// ===========================================================================

func TestPGAPIKeyStore_Integration(t *testing.T) {
	pool := newTestPGPool(t)
	ctx := context.Background()

	store, err := NewPGAPIKeyStore(pool)
	if err != nil {
		t.Fatalf("NewPGAPIKeyStore: %v", err)
	}

	companyID := uuid.New()
	createdBy := uuid.New()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM api_keys WHERE company_id = $1`, companyID)
	})

	key := &APIKey{
		Name:        "pg-test-key",
		CompanyID:   companyID,
		Permissions: []string{"read", "write", "admin"},
		CreatedBy:   createdBy,
		IsActive:    true,
	}

	// Create.
	rawKey, err := store.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(rawKey) == 0 {
		t.Fatal("expected non-empty raw key")
	}

	// Get by ID.
	fetched, err := store.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Name != "pg-test-key" {
		t.Errorf("Name = %q, want 'pg-test-key'", fetched.Name)
	}
	if fetched.CompanyID != companyID {
		t.Errorf("CompanyID mismatch")
	}
	if fetched.CreatedBy != createdBy {
		t.Errorf("CreatedBy mismatch")
	}
	if fetched.IsActive != true {
		t.Errorf("IsActive = false, want true")
	}

	// JSONB permissions round-trip.
	if len(fetched.Permissions) != 3 {
		t.Fatalf("Permissions len = %d, want 3", len(fetched.Permissions))
	}
	if fetched.Permissions[0] != "read" || fetched.Permissions[1] != "write" || fetched.Permissions[2] != "admin" {
		t.Errorf("Permissions = %v, want [read write admin]", fetched.Permissions)
	}

	// Get by hash.
	h := hashKey(rawKey)
	byHash, err := store.GetByHash(ctx, h)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if byHash.ID != key.ID {
		t.Errorf("GetByHash ID mismatch")
	}

	// Validate with correct key.
	validated, err := store.Validate(ctx, rawKey)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if validated.ID != key.ID {
		t.Errorf("Validate ID mismatch")
	}

	// Validate with wrong key.
	_, err = store.Validate(ctx, "wf_0000000000000000000000000000dead")
	if err != ErrNotFound {
		t.Errorf("Validate wrong key: got %v, want ErrNotFound", err)
	}

	// List.
	list, err := store.List(ctx, companyID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: expected 1, got %d", len(list))
	}

	// UpdateLastUsed.
	if err := store.UpdateLastUsed(ctx, key.ID); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}
	after, err := store.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("Get after UpdateLastUsed: %v", err)
	}
	if after.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set")
	}

	// Expired key.
	past := time.Now().Add(-time.Hour)
	expiredKey := &APIKey{
		Name:        "expired-key",
		CompanyID:   companyID,
		Permissions: []string{},
		CreatedBy:   createdBy,
		IsActive:    true,
		ExpiresAt:   &past,
	}
	expiredRaw, err := store.Create(ctx, expiredKey)
	if err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	_, err = store.Validate(ctx, expiredRaw)
	if err != ErrKeyExpired {
		t.Errorf("Validate expired: got %v, want ErrKeyExpired", err)
	}

	// Inactive key.
	inactiveKey := &APIKey{
		Name:        "inactive-key",
		CompanyID:   companyID,
		Permissions: []string{},
		CreatedBy:   createdBy,
		IsActive:    false,
	}
	inactiveRaw, err := store.Create(ctx, inactiveKey)
	if err != nil {
		t.Fatalf("Create inactive: %v", err)
	}
	_, err = store.Validate(ctx, inactiveRaw)
	if err != ErrKeyInactive {
		t.Errorf("Validate inactive: got %v, want ErrKeyInactive", err)
	}

	// Delete.
	if err := store.Delete(ctx, key.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, key.ID)
	if err != ErrNotFound {
		t.Errorf("Get after Delete: got %v, want ErrNotFound", err)
	}

	// Delete non-existent.
	if err := store.Delete(ctx, uuid.New()); err != ErrNotFound {
		t.Errorf("Delete missing: got %v, want ErrNotFound", err)
	}
}

// ===========================================================================
// PGIdempotencyStore integration tests
// ===========================================================================

func TestPGIdempotencyStore_Integration(t *testing.T) {
	pool := newTestPGPool(t)

	// Reuse the shared idempotency test suite.
	runIdempotencyTests(t, "Postgres", func(t *testing.T) IdempotencyStore {
		t.Helper()
		ctx := context.Background()

		store, err := NewPGIdempotencyStore(pool)
		if err != nil {
			t.Fatalf("NewPGIdempotencyStore: %v", err)
		}

		// Ensure the idempotency_keys table is cleaned up after each sub-test so that
		// fixed keys used by runIdempotencyTests do not accumulate across runs.
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM idempotency_keys`)
		})

		return store
	})
}

// ===========================================================================
// PGDLQStore integration tests
// ===========================================================================

func TestPGDLQStore_Integration(t *testing.T) {
	pool := newTestPGPool(t)
	ctx := context.Background()

	store, err := NewPGDLQStore(pool)
	if err != nil {
		t.Fatalf("NewPGDLQStore: %v", err)
	}

	// Each test run uses entries with a unique pipeline name for isolation.
	pipeline := "pg-dlq-" + uuid.New().String()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM dlq_entries WHERE pipeline_name = $1`, pipeline)
	})

	// Add.
	entry := &DLQEntry{
		OriginalEvent: json.RawMessage(`{"type":"pg.test","data":{"k":"v"}}`),
		PipelineName:  pipeline,
		StepName:      "pg-step",
		ErrorMessage:  "pg error",
		ErrorType:     "pg_type",
		MaxRetries:    5,
		Status:        DLQStatusPending,
		Metadata:      map[string]any{"pg": true, "count": float64(42)},
	}
	if err := store.Add(ctx, entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Get.
	got, err := store.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PipelineName != pipeline {
		t.Errorf("PipelineName = %q, want %q", got.PipelineName, pipeline)
	}
	if got.Status != DLQStatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if got.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", got.MaxRetries)
	}
	if got.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if got.Metadata["pg"] != true {
		t.Errorf("Metadata[pg] = %v, want true", got.Metadata["pg"])
	}
	if string(got.OriginalEvent) == "" {
		t.Error("expected non-empty original_event")
	}
	// Verify JSON round-trip of original event.
	var oe map[string]any
	if err := json.Unmarshal(got.OriginalEvent, &oe); err != nil {
		t.Errorf("OriginalEvent not valid JSON: %v", err)
	}

	// Get not found.
	_, err = store.Get(ctx, uuid.New())
	if err != ErrNotFound {
		t.Errorf("Get missing: got %v, want ErrNotFound", err)
	}

	// List + Count.
	list, err := store.List(ctx, DLQFilter{PipelineName: pipeline})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: expected 1, got %d", len(list))
	}
	count, err := store.Count(ctx, DLQFilter{PipelineName: pipeline})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}

	// Filter by error_type.
	byType, err := store.List(ctx, DLQFilter{ErrorType: "pg_type"})
	if err != nil {
		t.Fatalf("List by error_type: %v", err)
	}
	found := false
	for _, e := range byType {
		if e.ID == entry.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected to find entry when filtering by error_type")
	}

	// Retry.
	if err := store.Retry(ctx, entry.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	retried, err := store.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("Get after Retry: %v", err)
	}
	if retried.Status != DLQStatusRetrying {
		t.Errorf("Status after Retry = %q, want retrying", retried.Status)
	}
	if retried.RetryCount != 1 {
		t.Errorf("RetryCount after Retry = %d, want 1", retried.RetryCount)
	}

	// UpdateStatus.
	if err := store.UpdateStatus(ctx, entry.ID, DLQStatusPending); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// Discard.
	if err := store.Discard(ctx, entry.ID); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	discarded, err := store.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("Get after Discard: %v", err)
	}
	if discarded.Status != DLQStatusDiscarded {
		t.Errorf("Status after Discard = %q, want discarded", discarded.Status)
	}

	// Resolve a second entry.
	entry2 := &DLQEntry{
		PipelineName: pipeline,
		StepName:     "pg-step-2",
		ErrorMessage: "err2",
		ErrorType:    "pg_type",
		Status:       DLQStatusPending,
	}
	if err := store.Add(ctx, entry2); err != nil {
		t.Fatalf("Add entry2: %v", err)
	}
	if err := store.Resolve(ctx, entry2.ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved, err := store.Get(ctx, entry2.ID)
	if err != nil {
		t.Fatalf("Get after Resolve: %v", err)
	}
	if resolved.Status != DLQStatusResolved {
		t.Errorf("Status after Resolve = %q, want resolved", resolved.Status)
	}
	if resolved.ResolvedAt == nil {
		t.Error("expected non-nil resolved_at after Resolve")
	}

	// Purge resolved/discarded entries older than zero duration (all of them).
	purged, err := store.Purge(ctx, 0)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if purged < 2 {
		t.Errorf("Purge: expected >= 2 removed, got %d", purged)
	}
}
