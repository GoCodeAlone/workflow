package dockercompose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// FileStateStore persists platform state as JSON files in a local directory.
// It implements platform.StateStore for local Docker Compose development.
type FileStateStore struct {
	mu      sync.RWMutex
	baseDir string
	locks   map[string]*fileLock
}

// NewFileStateStore creates a FileStateStore rooted at baseDir.
// The directory is created if it does not exist.
func NewFileStateStore(baseDir string) (*FileStateStore, error) {
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	return &FileStateStore{
		baseDir: baseDir,
		locks:   make(map[string]*fileLock),
	}, nil
}

// SaveResource persists a resource output to the state directory.
func (s *FileStateStore) SaveResource(_ context.Context, contextPath string, output *platform.ResourceOutput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.contextDir(contextPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create context directory: %w", err)
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal resource output: %w", err)
	}

	path := filepath.Join(dir, "resource-"+output.Name+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write resource state: %w", err)
	}
	return nil
}

// GetResource retrieves a resource's state from the state directory.
func (s *FileStateStore) GetResource(_ context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.contextDir(contextPath), "resource-"+resourceName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &platform.ResourceNotFoundError{Name: resourceName, Provider: "docker-compose"}
		}
		return nil, fmt.Errorf("read resource state: %w", err)
	}

	var output platform.ResourceOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("unmarshal resource state: %w", err)
	}
	return &output, nil
}

// ListResources returns all resources in a context path.
func (s *FileStateStore) ListResources(_ context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.contextDir(contextPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list resources: %w", err)
	}

	var resources []*platform.ResourceOutput
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) <= 14 || name[:9] != "resource-" || name[len(name)-5:] != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read resource %s: %w", name, err)
		}

		var output platform.ResourceOutput
		if err := json.Unmarshal(data, &output); err != nil {
			return nil, fmt.Errorf("unmarshal resource %s: %w", name, err)
		}
		resources = append(resources, &output)
	}
	return resources, nil
}

// DeleteResource removes a resource from state.
func (s *FileStateStore) DeleteResource(_ context.Context, contextPath, resourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.contextDir(contextPath), "resource-"+resourceName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete resource state: %w", err)
	}
	return nil
}

// SavePlan persists an execution plan.
func (s *FileStateStore) SavePlan(_ context.Context, plan *platform.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.baseDir, "plans")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create plans directory: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	path := filepath.Join(dir, "plan-"+plan.ID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

// GetPlan retrieves an execution plan by ID.
func (s *FileStateStore) GetPlan(_ context.Context, planID string) (*platform.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Validate planID to prevent path traversal and unexpected file access.
	// Plan IDs are expected to be simple identifiers, not paths.
	if strings.Contains(planID, "/") || strings.Contains(planID, "\\") || strings.Contains(planID, "..") {
		return nil, fmt.Errorf("invalid plan ID %q", planID)
	}

	dir := filepath.Join(s.baseDir, "plans")
	path := filepath.Join(dir, "plan-"+planID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("plan %q not found", planID)
		}
		return nil, fmt.Errorf("read plan: %w", err)
	}

	var plan platform.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &plan, nil
}

// ListPlans lists plans for a context path, ordered by creation time descending.
func (s *FileStateStore) ListPlans(_ context.Context, _ string, limit int) ([]*platform.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.baseDir, "plans")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list plans: %w", err)
	}

	var plans []*platform.Plan
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var plan platform.Plan
		if err := json.Unmarshal(data, &plan); err != nil {
			continue
		}
		plans = append(plans, &plan)
	}

	// Sort by creation time descending
	for i := 0; i < len(plans); i++ {
		for j := i + 1; j < len(plans); j++ {
			if plans[j].CreatedAt.After(plans[i].CreatedAt) {
				plans[i], plans[j] = plans[j], plans[i]
			}
		}
	}

	if limit > 0 && len(plans) > limit {
		plans = plans[:limit]
	}
	return plans, nil
}

// Lock acquires an advisory lock for a context path.
func (s *FileStateStore) Lock(_ context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.locks[contextPath]
	if ok && time.Now().Before(existing.expiresAt) {
		return nil, &platform.LockConflictError{ContextPath: contextPath}
	}

	lock := &fileLock{
		store:       s,
		contextPath: contextPath,
		expiresAt:   time.Now().Add(ttl),
	}
	s.locks[contextPath] = lock
	return lock, nil
}

// Dependencies returns dependency references for resources that depend on the given resource.
func (s *FileStateStore) Dependencies(_ context.Context, contextPath, _ string) ([]platform.DependencyRef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.contextDir(contextPath), "dependencies.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dependencies: %w", err)
	}

	var deps []platform.DependencyRef
	if err := json.Unmarshal(data, &deps); err != nil {
		return nil, fmt.Errorf("unmarshal dependencies: %w", err)
	}
	return deps, nil
}

// AddDependency records a cross-resource dependency.
func (s *FileStateStore) AddDependency(_ context.Context, dep platform.DependencyRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.contextDir(dep.SourceContext)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create context directory: %w", err)
	}

	path := filepath.Join(dir, "dependencies.json")
	var deps []platform.DependencyRef

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &deps); err != nil {
			return fmt.Errorf("unmarshal existing dependencies: %w", err)
		}
	}

	deps = append(deps, dep)
	out, err := json.MarshalIndent(deps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dependencies: %w", err)
	}

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write dependencies: %w", err)
	}
	return nil
}

func (s *FileStateStore) contextDir(contextPath string) string {
	return filepath.Join(s.baseDir, filepath.FromSlash(contextPath))
}

// fileLock implements platform.LockHandle.
type fileLock struct {
	store       *FileStateStore
	contextPath string
	expiresAt   time.Time
}

// Unlock releases the advisory lock.
func (l *fileLock) Unlock(_ context.Context) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	delete(l.store.locks, l.contextPath)
	return nil
}

// Refresh extends the lock TTL.
func (l *fileLock) Refresh(_ context.Context, ttl time.Duration) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	l.expiresAt = time.Now().Add(ttl)
	return nil
}
