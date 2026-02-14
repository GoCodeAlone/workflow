package workflow

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- Mock WorkflowStore for engine manager tests ---

type emMockWorkflowStore struct {
	mu      sync.RWMutex
	records map[uuid.UUID]*store.WorkflowRecord
}

func newEMMockWorkflowStore() *emMockWorkflowStore {
	return &emMockWorkflowStore{records: make(map[uuid.UUID]*store.WorkflowRecord)}
}

func (s *emMockWorkflowStore) Create(_ context.Context, w *store.WorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[w.ID] = w
	return nil
}

func (s *emMockWorkflowStore) Get(_ context.Context, id uuid.UUID) (*store.WorkflowRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return r, nil
}

func (s *emMockWorkflowStore) GetBySlug(_ context.Context, _ uuid.UUID, _ string) (*store.WorkflowRecord, error) {
	return nil, store.ErrNotFound
}

func (s *emMockWorkflowStore) Update(_ context.Context, w *store.WorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[w.ID] = w
	return nil
}

func (s *emMockWorkflowStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

func (s *emMockWorkflowStore) List(_ context.Context, _ store.WorkflowFilter) ([]*store.WorkflowRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.WorkflowRecord
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}

func (s *emMockWorkflowStore) GetVersion(_ context.Context, _ uuid.UUID, _ int) (*store.WorkflowRecord, error) {
	return nil, store.ErrNotFound
}

func (s *emMockWorkflowStore) ListVersions(_ context.Context, _ uuid.UUID) ([]*store.WorkflowRecord, error) {
	return nil, nil
}

// --- Mock CrossWorkflowLinkStore for engine manager tests ---

type emMockLinkStore struct {
	links []*store.CrossWorkflowLink
}

func (s *emMockLinkStore) Create(_ context.Context, l *store.CrossWorkflowLink) error {
	s.links = append(s.links, l)
	return nil
}

func (s *emMockLinkStore) Get(_ context.Context, id uuid.UUID) (*store.CrossWorkflowLink, error) {
	for _, l := range s.links {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *emMockLinkStore) Delete(_ context.Context, id uuid.UUID) error {
	for i, l := range s.links {
		if l.ID == id {
			s.links = append(s.links[:i], s.links[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *emMockLinkStore) List(_ context.Context, _ store.CrossWorkflowLinkFilter) ([]*store.CrossWorkflowLink, error) {
	return s.links, nil
}

// --- Engine builder helpers ---

func newTestEngineBuilder() EngineBuilderFunc {
	return func(_ *config.WorkflowConfig, _ *slog.Logger) (*StdEngine, modular.Application, error) {
		app := newMockApplication()
		app.services["svc1"] = "val1"
		engine := &StdEngine{
			app:    app,
			logger: app.logger,
		}
		return engine, app, nil
	}
}

func newFailingEngineBuilder() EngineBuilderFunc {
	return func(_ *config.WorkflowConfig, _ *slog.Logger) (*StdEngine, modular.Application, error) {
		return nil, nil, fmt.Errorf("builder error")
	}
}

const validConfigYAML = `
name: test-workflow
modules: []
`

func emSeedWorkflow(ws *emMockWorkflowStore, id uuid.UUID, configYAML string) {
	ws.records[id] = &store.WorkflowRecord{
		ID:         id,
		ConfigYAML: configYAML,
		Status:     store.WorkflowStatusDraft,
	}
}

func emTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Tests ---

func TestEngineManager_DeployWorkflow_Success(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	err := m.DeployWorkflow(context.Background(), id)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	status, err := m.GetStatus(id)
	if err != nil {
		t.Fatalf("expected status, got error %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected status running, got %s", status.Status)
	}
}

func TestEngineManager_DeployWorkflow_NotFound(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	err := m.DeployWorkflow(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for missing workflow")
	}
}

func TestEngineManager_DeployWorkflow_InvalidYAML(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, "{{{invalid yaml")

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	err := m.DeployWorkflow(context.Background(), id)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestEngineManager_DeployWorkflow_AlreadyRunning(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	if err := m.DeployWorkflow(context.Background(), id); err != nil {
		t.Fatalf("first deploy failed: %v", err)
	}

	err := m.DeployWorkflow(context.Background(), id)
	if err == nil {
		t.Fatal("expected error for already running workflow")
	}
}

func TestEngineManager_DeployWorkflow_BuilderError(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newFailingEngineBuilder())
	err := m.DeployWorkflow(context.Background(), id)
	if err == nil {
		t.Fatal("expected error from builder")
	}
}

func TestEngineManager_StopWorkflow_Success(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	if err := m.DeployWorkflow(context.Background(), id); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if err := m.StopWorkflow(context.Background(), id); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	_, err := m.GetStatus(id)
	if err == nil {
		t.Fatal("expected error after stop (not running)")
	}
}

func TestEngineManager_StopWorkflow_NotRunning(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	err := m.StopWorkflow(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for stopping non-running workflow")
	}
}

func TestEngineManager_ReloadWorkflow_Success(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	if err := m.DeployWorkflow(context.Background(), id); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if err := m.ReloadWorkflow(context.Background(), id); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	status, err := m.GetStatus(id)
	if err != nil {
		t.Fatalf("expected status after reload, got %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected status running after reload, got %s", status.Status)
	}
}

func TestEngineManager_ReloadWorkflow_NotRunning(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	// Reload on non-running deploys fresh
	if err := m.ReloadWorkflow(context.Background(), id); err != nil {
		t.Fatalf("reload (fresh deploy) failed: %v", err)
	}

	status, err := m.GetStatus(id)
	if err != nil {
		t.Fatalf("expected status, got %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected running, got %s", status.Status)
	}
}

func TestEngineManager_GetStatus_Running(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	if err := m.DeployWorkflow(context.Background(), id); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	status, err := m.GetStatus(id)
	if err != nil {
		t.Fatalf("expected status, got %v", err)
	}
	if status.WorkflowID != id {
		t.Errorf("expected workflow ID %s, got %s", id, status.WorkflowID)
	}
	if status.ModuleCount != 1 {
		t.Errorf("expected module count 1, got %d", status.ModuleCount)
	}
}

func TestEngineManager_GetStatus_NotFound(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	_, err := m.GetStatus(uuid.New())
	if err == nil {
		t.Fatal("expected error for non-existent workflow status")
	}
}

func TestEngineManager_ListActive_Empty(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	statuses := m.ListActive()
	if len(statuses) != 0 {
		t.Errorf("expected 0 active, got %d", len(statuses))
	}
}

func TestEngineManager_ListActive_Multiple(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id1, id2 := uuid.New(), uuid.New()
	emSeedWorkflow(ws, id1, validConfigYAML)
	emSeedWorkflow(ws, id2, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	if err := m.DeployWorkflow(context.Background(), id1); err != nil {
		t.Fatalf("deploy 1 failed: %v", err)
	}
	if err := m.DeployWorkflow(context.Background(), id2); err != nil {
		t.Fatalf("deploy 2 failed: %v", err)
	}

	statuses := m.ListActive()
	if len(statuses) != 2 {
		t.Errorf("expected 2 active, got %d", len(statuses))
	}
}

func TestEngineManager_StopAll_Success(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id1, id2 := uuid.New(), uuid.New()
	emSeedWorkflow(ws, id1, validConfigYAML)
	emSeedWorkflow(ws, id2, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())
	_ = m.DeployWorkflow(context.Background(), id1)
	_ = m.DeployWorkflow(context.Background(), id2)

	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("stop all failed: %v", err)
	}

	statuses := m.ListActive()
	if len(statuses) != 0 {
		t.Errorf("expected 0 active after StopAll, got %d", len(statuses))
	}
}

func TestEngineManager_StopAll_Empty(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("stop all on empty should not fail: %v", err)
	}
}

func TestEngineManager_ConcurrentDeploy(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}

	ids := make([]uuid.UUID, 10)
	for i := range ids {
		ids[i] = uuid.New()
		emSeedWorkflow(ws, ids[i], validConfigYAML)
	}

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(wfID uuid.UUID) {
			defer wg.Done()
			_ = m.DeployWorkflow(context.Background(), wfID)
		}(id)
	}
	wg.Wait()

	statuses := m.ListActive()
	if len(statuses) != 10 {
		t.Errorf("expected 10 active, got %d", len(statuses))
	}
}

func TestEngineManager_ConcurrentDeployStop(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	id := uuid.New()
	emSeedWorkflow(ws, id, validConfigYAML)

	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_ = m.DeployWorkflow(context.Background(), id)
		})
		wg.Go(func() {
			_ = m.StopWorkflow(context.Background(), id)
		})
	}
	wg.Wait()
	// No panics or data races is the assertion (run with -race)
}

func TestEngineManager_Router_NotNil(t *testing.T) {
	ws := newEMMockWorkflowStore()
	ls := &emMockLinkStore{}
	m := NewWorkflowEngineManager(ws, ls, emTestLogger(), newTestEngineBuilder())

	if m.Router() == nil {
		t.Fatal("expected non-nil router")
	}
}
