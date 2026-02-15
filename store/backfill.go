package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Backfill types
// ---------------------------------------------------------------------------

// BackfillStatus represents the status of a backfill operation.
type BackfillStatus string

const (
	BackfillStatusPending   BackfillStatus = "pending"
	BackfillStatusRunning   BackfillStatus = "running"
	BackfillStatusCompleted BackfillStatus = "completed"
	BackfillStatusFailed    BackfillStatus = "failed"
	BackfillStatusCancelled BackfillStatus = "cancelled"
)

// BackfillRequest defines a request to replay historical events through a pipeline.
type BackfillRequest struct {
	ID           uuid.UUID      `json:"id"`
	PipelineName string         `json:"pipeline_name"`
	SourceQuery  string         `json:"source_query"`
	StartTime    *time.Time     `json:"start_time,omitempty"`
	EndTime      *time.Time     `json:"end_time,omitempty"`
	Status       BackfillStatus `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	TotalEvents  int64          `json:"total_events"`
	Processed    int64          `json:"processed"`
	Failed       int64          `json:"failed"`
	ErrorMsg     string         `json:"error_message,omitempty"`
}

// ---------------------------------------------------------------------------
// BackfillStore interface
// ---------------------------------------------------------------------------

// BackfillStore manages backfill operations.
type BackfillStore interface {
	// Create inserts a new backfill request.
	Create(ctx context.Context, req *BackfillRequest) error
	// Get retrieves a backfill request by ID.
	Get(ctx context.Context, id uuid.UUID) (*BackfillRequest, error)
	// List returns all backfill requests, ordered by creation time descending.
	List(ctx context.Context) ([]*BackfillRequest, error)
	// UpdateProgress updates the processed and failed counts for a backfill request.
	UpdateProgress(ctx context.Context, id uuid.UUID, processed, failed int64) error
	// UpdateStatus sets the status and optional error message for a backfill request.
	UpdateStatus(ctx context.Context, id uuid.UUID, status BackfillStatus, errMsg string) error
	// Cancel cancels a pending or running backfill request.
	Cancel(ctx context.Context, id uuid.UUID) error
}

// ===========================================================================
// InMemoryBackfillStore
// ===========================================================================

// InMemoryBackfillStore is a thread-safe in-memory implementation of BackfillStore.
type InMemoryBackfillStore struct {
	mu       sync.RWMutex
	requests map[uuid.UUID]*BackfillRequest
}

// NewInMemoryBackfillStore creates a new InMemoryBackfillStore.
func NewInMemoryBackfillStore() *InMemoryBackfillStore {
	return &InMemoryBackfillStore{
		requests: make(map[uuid.UUID]*BackfillRequest),
	}
}

func (s *InMemoryBackfillStore) Create(_ context.Context, req *BackfillRequest) error {
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	req.CreatedAt = time.Now()
	if req.Status == "" {
		req.Status = BackfillStatusPending
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.requests[req.ID]; exists {
		return ErrDuplicate
	}

	// Store a copy to prevent external mutation.
	cp := *req
	s.requests[cp.ID] = &cp
	return nil
}

func (s *InMemoryBackfillStore) Get(_ context.Context, id uuid.UUID) (*BackfillRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *req
	return &cp, nil
}

func (s *InMemoryBackfillStore) List(_ context.Context) ([]*BackfillRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*BackfillRequest, 0, len(s.requests))
	for _, req := range s.requests {
		cp := *req
		results = append(results, &cp)
	}

	// Sort by created_at descending (most recent first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results, nil
}

func (s *InMemoryBackfillStore) UpdateProgress(_ context.Context, id uuid.UUID, processed, failed int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return ErrNotFound
	}
	req.Processed = processed
	req.Failed = failed
	return nil
}

func (s *InMemoryBackfillStore) UpdateStatus(_ context.Context, id uuid.UUID, status BackfillStatus, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return ErrNotFound
	}
	req.Status = status
	req.ErrorMsg = errMsg

	if status == BackfillStatusCompleted || status == BackfillStatusFailed || status == BackfillStatusCancelled {
		now := time.Now()
		req.CompletedAt = &now
	}
	return nil
}

func (s *InMemoryBackfillStore) Cancel(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return ErrNotFound
	}

	if req.Status != BackfillStatusPending && req.Status != BackfillStatusRunning {
		return fmt.Errorf("cannot cancel backfill in status %q: %w", req.Status, ErrConflict)
	}

	now := time.Now()
	req.Status = BackfillStatusCancelled
	req.CompletedAt = &now
	return nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ BackfillStore = (*InMemoryBackfillStore)(nil)
