package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// ===========================================================================
// TestBackfillCreate
// ===========================================================================

func TestBackfillCreate(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	req := &BackfillRequest{
		PipelineName: "order-pipeline",
		SourceQuery:  "status=failed",
		TotalEvents:  100,
	}

	if err := s.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if req.ID == uuid.Nil {
		t.Error("expected non-nil ID after Create")
	}
	if req.Status != BackfillStatusPending {
		t.Errorf("expected status %q, got %q", BackfillStatusPending, req.Status)
	}
	if req.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if req.PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline_name %q, got %q", "order-pipeline", req.PipelineName)
	}
}

// ===========================================================================
// TestBackfillGet
// ===========================================================================

func TestBackfillGet(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	req := &BackfillRequest{
		PipelineName: "order-pipeline",
		SourceQuery:  "type=order",
		TotalEvents:  50,
	}
	if err := s.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != req.ID {
		t.Errorf("expected ID %v, got %v", req.ID, got.ID)
	}
	if got.PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline_name %q, got %q", "order-pipeline", got.PipelineName)
	}
	if got.SourceQuery != "type=order" {
		t.Errorf("expected source_query %q, got %q", "type=order", got.SourceQuery)
	}
	if got.TotalEvents != 50 {
		t.Errorf("expected total_events 50, got %d", got.TotalEvents)
	}

	// Verify returned copy is independent from stored copy.
	got.PipelineName = "mutated"
	got2, _ := s.Get(ctx, req.ID)
	if got2.PipelineName != "order-pipeline" {
		t.Error("Get should return independent copies")
	}
}

// ===========================================================================
// TestBackfillList
// ===========================================================================

func TestBackfillList(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	// Empty list.
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 requests, got %d", len(list))
	}

	// Create multiple.
	for i := 0; i < 3; i++ {
		req := &BackfillRequest{
			PipelineName: "pipeline",
			SourceQuery:  "all",
		}
		if err := s.Create(ctx, req); err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
	}

	list, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(list))
	}

	// Verify ordering: most recent first.
	for i := 0; i < len(list)-1; i++ {
		if list[i].CreatedAt.Before(list[i+1].CreatedAt) {
			t.Errorf("expected descending order at index %d", i)
		}
	}
}

// ===========================================================================
// TestBackfillUpdateProgress
// ===========================================================================

func TestBackfillUpdateProgress(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	req := &BackfillRequest{
		PipelineName: "pipeline",
		TotalEvents:  100,
	}
	if err := s.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.UpdateProgress(ctx, req.ID, 50, 5); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}

	got, _ := s.Get(ctx, req.ID)
	if got.Processed != 50 {
		t.Errorf("expected processed 50, got %d", got.Processed)
	}
	if got.Failed != 5 {
		t.Errorf("expected failed 5, got %d", got.Failed)
	}

	// Update again.
	if err := s.UpdateProgress(ctx, req.ID, 100, 10); err != nil {
		t.Fatalf("UpdateProgress (2): %v", err)
	}

	got, _ = s.Get(ctx, req.ID)
	if got.Processed != 100 {
		t.Errorf("expected processed 100, got %d", got.Processed)
	}
	if got.Failed != 10 {
		t.Errorf("expected failed 10, got %d", got.Failed)
	}
}

// ===========================================================================
// TestBackfillCancel
// ===========================================================================

func TestBackfillCancel(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	// Cancel a pending request.
	req := &BackfillRequest{
		PipelineName: "pipeline",
	}
	if err := s.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Cancel(ctx, req.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got, _ := s.Get(ctx, req.ID)
	if got.Status != BackfillStatusCancelled {
		t.Errorf("expected status %q, got %q", BackfillStatusCancelled, got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected non-nil CompletedAt after cancel")
	}

	// Cancel a running request.
	req2 := &BackfillRequest{
		PipelineName: "pipeline",
		Status:       BackfillStatusRunning,
	}
	if err := s.Create(ctx, req2); err != nil {
		t.Fatalf("Create (running): %v", err)
	}
	// Override status to running after creation.
	if err := s.UpdateStatus(ctx, req2.ID, BackfillStatusRunning, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := s.Cancel(ctx, req2.ID); err != nil {
		t.Fatalf("Cancel (running): %v", err)
	}

	got2, _ := s.Get(ctx, req2.ID)
	if got2.Status != BackfillStatusCancelled {
		t.Errorf("expected status %q, got %q", BackfillStatusCancelled, got2.Status)
	}

	// Cannot cancel a completed request.
	req3 := &BackfillRequest{
		PipelineName: "pipeline",
	}
	if err := s.Create(ctx, req3); err != nil {
		t.Fatalf("Create (completed): %v", err)
	}
	if err := s.UpdateStatus(ctx, req3.ID, BackfillStatusCompleted, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := s.Cancel(ctx, req3.ID); err == nil {
		t.Error("expected error when cancelling a completed backfill")
	}
}

// ===========================================================================
// TestBackfillNotFound
// ===========================================================================

func TestBackfillNotFound(t *testing.T) {
	s := NewInMemoryBackfillStore()
	ctx := context.Background()

	_, err := s.Get(ctx, uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	err = s.UpdateProgress(ctx, uuid.New(), 0, 0)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for UpdateProgress, got %v", err)
	}

	err = s.UpdateStatus(ctx, uuid.New(), BackfillStatusRunning, "")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for UpdateStatus, got %v", err)
	}

	err = s.Cancel(ctx, uuid.New())
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for Cancel, got %v", err)
	}
}
