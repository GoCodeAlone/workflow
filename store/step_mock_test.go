package store

import (
	"context"
	"testing"
	"time"
)

// ===========================================================================
// TestMockSetAndGet
// ===========================================================================

func TestMockSetAndGet(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	mock := &StepMock{
		PipelineName: "order-pipeline",
		StepName:     "validate",
		Response:     map[string]any{"valid": true, "score": 95.5},
		Enabled:      true,
		Delay:        100 * time.Millisecond,
	}

	if err := s.Set(ctx, mock); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(ctx, "order-pipeline", "validate")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline_name %q, got %q", "order-pipeline", got.PipelineName)
	}
	if got.StepName != "validate" {
		t.Errorf("expected step_name %q, got %q", "validate", got.StepName)
	}
	if !got.Enabled {
		t.Error("expected enabled to be true")
	}
	if got.Response["valid"] != true {
		t.Errorf("expected response.valid=true, got %v", got.Response["valid"])
	}
	if got.Delay != 100*time.Millisecond {
		t.Errorf("expected delay 100ms, got %v", got.Delay)
	}
	if got.HitCount != 0 {
		t.Errorf("expected hit_count 0, got %d", got.HitCount)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	// Verify returned copy is independent.
	got.Response["valid"] = false
	got2, _ := s.Get(ctx, "order-pipeline", "validate")
	if got2.Response["valid"] != true {
		t.Error("Get should return independent copies")
	}

	// Update existing mock — should preserve hit count.
	if err := s.IncrementHitCount(ctx, "order-pipeline", "validate"); err != nil {
		t.Fatalf("IncrementHitCount: %v", err)
	}

	updatedMock := &StepMock{
		PipelineName: "order-pipeline",
		StepName:     "validate",
		Response:     map[string]any{"valid": false},
		Enabled:      false,
	}
	if err := s.Set(ctx, updatedMock); err != nil {
		t.Fatalf("Set (update): %v", err)
	}

	got3, _ := s.Get(ctx, "order-pipeline", "validate")
	if got3.Response["valid"] != false {
		t.Error("expected updated response")
	}
	if got3.HitCount != 1 {
		t.Errorf("expected hit_count to be preserved as 1, got %d", got3.HitCount)
	}

	// Set with error response.
	errMock := &StepMock{
		PipelineName:  "order-pipeline",
		StepName:      "charge",
		ErrorResponse: "payment failed",
		Enabled:       true,
	}
	if err := s.Set(ctx, errMock); err != nil {
		t.Fatalf("Set (error): %v", err)
	}

	gotErr, _ := s.Get(ctx, "order-pipeline", "charge")
	if gotErr.ErrorResponse != "payment failed" {
		t.Errorf("expected error_response %q, got %q", "payment failed", gotErr.ErrorResponse)
	}
}

// ===========================================================================
// TestMockList
// ===========================================================================

func TestMockList(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	// Empty list.
	list, err := s.List(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 mocks, got %d", len(list))
	}

	// Create mocks for two pipelines.
	mocks := []*StepMock{
		{PipelineName: "pipeline-a", StepName: "step1", Response: map[string]any{"ok": true}, Enabled: true},
		{PipelineName: "pipeline-a", StepName: "step2", Response: map[string]any{"ok": true}, Enabled: true},
		{PipelineName: "pipeline-b", StepName: "step1", Response: map[string]any{"ok": true}, Enabled: true},
	}
	for _, m := range mocks {
		if err := s.Set(ctx, m); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// List for pipeline-a.
	listA, err := s.List(ctx, "pipeline-a")
	if err != nil {
		t.Fatalf("List (pipeline-a): %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 mocks for pipeline-a, got %d", len(listA))
	}

	// Verify sorted by step name.
	if listA[0].StepName != "step1" || listA[1].StepName != "step2" {
		t.Errorf("expected mocks sorted by step name, got %q, %q", listA[0].StepName, listA[1].StepName)
	}

	// List for pipeline-b.
	listB, err := s.List(ctx, "pipeline-b")
	if err != nil {
		t.Fatalf("List (pipeline-b): %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected 1 mock for pipeline-b, got %d", len(listB))
	}
}

// ===========================================================================
// TestMockRemove
// ===========================================================================

func TestMockRemove(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	mock := &StepMock{
		PipelineName: "pipeline",
		StepName:     "step1",
		Response:     map[string]any{"ok": true},
		Enabled:      true,
	}
	if err := s.Set(ctx, mock); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify it exists.
	if _, err := s.Get(ctx, "pipeline", "step1"); err != nil {
		t.Fatalf("Get before remove: %v", err)
	}

	// Remove it.
	if err := s.Remove(ctx, "pipeline", "step1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone.
	_, err := s.Get(ctx, "pipeline", "step1")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after Remove, got %v", err)
	}

	// Remove again should fail.
	err = s.Remove(ctx, "pipeline", "step1")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for double Remove, got %v", err)
	}
}

// ===========================================================================
// TestMockClearAll
// ===========================================================================

func TestMockClearAll(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	// Create several mocks.
	for i := 0; i < 5; i++ {
		mock := &StepMock{
			PipelineName: "pipeline",
			StepName:     "step" + string(rune('0'+i)),
			Response:     map[string]any{"ok": true},
			Enabled:      true,
		}
		if err := s.Set(ctx, mock); err != nil {
			t.Fatalf("Set[%d]: %v", i, err)
		}
	}

	// Clear all.
	if err := s.ClearAll(ctx); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	// Verify all gone.
	list, err := s.List(ctx, "pipeline")
	if err != nil {
		t.Fatalf("List after ClearAll: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 mocks after ClearAll, got %d", len(list))
	}
}

// ===========================================================================
// TestMockHitCount
// ===========================================================================

func TestMockHitCount(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	mock := &StepMock{
		PipelineName: "pipeline",
		StepName:     "step1",
		Response:     map[string]any{"ok": true},
		Enabled:      true,
	}
	if err := s.Set(ctx, mock); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Initial hit count should be 0.
	got, _ := s.Get(ctx, "pipeline", "step1")
	if got.HitCount != 0 {
		t.Errorf("expected hit_count 0, got %d", got.HitCount)
	}

	// Increment multiple times.
	for i := 0; i < 5; i++ {
		if err := s.IncrementHitCount(ctx, "pipeline", "step1"); err != nil {
			t.Fatalf("IncrementHitCount[%d]: %v", i, err)
		}
	}

	got, _ = s.Get(ctx, "pipeline", "step1")
	if got.HitCount != 5 {
		t.Errorf("expected hit_count 5, got %d", got.HitCount)
	}
}

// ===========================================================================
// TestMockNotFound
// ===========================================================================

func TestMockNotFound(t *testing.T) {
	s := NewInMemoryStepMockStore()
	ctx := context.Background()

	_, err := s.Get(ctx, "nonexistent", "step")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for Get, got %v", err)
	}

	err = s.Remove(ctx, "nonexistent", "step")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for Remove, got %v", err)
	}

	err = s.IncrementHitCount(ctx, "nonexistent", "step")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for IncrementHitCount, got %v", err)
	}
}
