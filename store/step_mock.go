package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Step mock types
// ---------------------------------------------------------------------------

// StepMock defines a mock response for a specific pipeline step.
type StepMock struct {
	ID            uuid.UUID      `json:"id"`
	PipelineName  string         `json:"pipeline_name"`
	StepName      string         `json:"step_name"`
	Response      map[string]any `json:"response"`
	ErrorResponse string         `json:"error_response,omitempty"`
	Delay         time.Duration  `json:"delay,omitempty"`
	Enabled       bool           `json:"enabled"`
	HitCount      int64          `json:"hit_count"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ---------------------------------------------------------------------------
// StepMockStore interface
// ---------------------------------------------------------------------------

// StepMockStore manages step mocks for pipeline testing.
type StepMockStore interface {
	// Set creates or updates a mock for a specific pipeline step.
	Set(ctx context.Context, mock *StepMock) error
	// Get retrieves a mock for a specific pipeline and step.
	Get(ctx context.Context, pipeline, step string) (*StepMock, error)
	// List returns all mocks for a given pipeline.
	List(ctx context.Context, pipeline string) ([]*StepMock, error)
	// Remove deletes a mock for a specific pipeline and step.
	Remove(ctx context.Context, pipeline, step string) error
	// ClearAll removes all mocks.
	ClearAll(ctx context.Context) error
	// IncrementHitCount increments the hit count for a mock.
	IncrementHitCount(ctx context.Context, pipeline, step string) error
}

// mockKey creates a unique key for a pipeline+step combination.
func mockKey(pipeline, step string) string {
	return pipeline + "::" + step
}

// ===========================================================================
// InMemoryStepMockStore
// ===========================================================================

// InMemoryStepMockStore is a thread-safe in-memory implementation of StepMockStore.
type InMemoryStepMockStore struct {
	mu    sync.RWMutex
	mocks map[string]*StepMock // key: "pipeline::step"
}

// NewInMemoryStepMockStore creates a new InMemoryStepMockStore.
func NewInMemoryStepMockStore() *InMemoryStepMockStore {
	return &InMemoryStepMockStore{
		mocks: make(map[string]*StepMock),
	}
}

func (s *InMemoryStepMockStore) Set(_ context.Context, mock *StepMock) error {
	if mock.ID == uuid.Nil {
		mock.ID = uuid.New()
	}
	mock.CreatedAt = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	key := mockKey(mock.PipelineName, mock.StepName)

	// Preserve hit count if updating an existing mock.
	if existing, ok := s.mocks[key]; ok {
		mock.HitCount = existing.HitCount
	}

	// Store a copy to prevent external mutation.
	cp := *mock
	if mock.Response != nil {
		cp.Response = make(map[string]any, len(mock.Response))
		for k, v := range mock.Response {
			cp.Response[k] = v
		}
	}
	s.mocks[key] = &cp
	return nil
}

func (s *InMemoryStepMockStore) Get(_ context.Context, pipeline, step string) (*StepMock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := mockKey(pipeline, step)
	mock, ok := s.mocks[key]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *mock
	if mock.Response != nil {
		cp.Response = make(map[string]any, len(mock.Response))
		for k, v := range mock.Response {
			cp.Response[k] = v
		}
	}
	return &cp, nil
}

func (s *InMemoryStepMockStore) List(_ context.Context, pipeline string) ([]*StepMock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*StepMock
	for _, mock := range s.mocks {
		if mock.PipelineName == pipeline {
			cp := *mock
			if mock.Response != nil {
				cp.Response = make(map[string]any, len(mock.Response))
				for k, v := range mock.Response {
					cp.Response[k] = v
				}
			}
			results = append(results, &cp)
		}
	}

	// Sort by step name for deterministic output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].StepName < results[j].StepName
	})

	return results, nil
}

func (s *InMemoryStepMockStore) Remove(_ context.Context, pipeline, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := mockKey(pipeline, step)
	if _, ok := s.mocks[key]; !ok {
		return ErrNotFound
	}
	delete(s.mocks, key)
	return nil
}

func (s *InMemoryStepMockStore) ClearAll(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mocks = make(map[string]*StepMock)
	return nil
}

func (s *InMemoryStepMockStore) IncrementHitCount(_ context.Context, pipeline, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := mockKey(pipeline, step)
	mock, ok := s.mocks[key]
	if !ok {
		return ErrNotFound
	}
	mock.HitCount++
	return nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ StepMockStore = (*InMemoryStepMockStore)(nil)
