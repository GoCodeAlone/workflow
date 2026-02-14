package versioning

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// WorkflowVersion represents a versioned snapshot of a workflow configuration.
type WorkflowVersion struct {
	WorkflowName string    `json:"workflowName"`
	Version      int       `json:"version"`
	ConfigYAML   string    `json:"configYaml"`
	Description  string    `json:"description,omitempty"`
	CreatedBy    string    `json:"createdBy,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// VersionStore persists workflow versions in memory.
type VersionStore struct {
	mu       sync.RWMutex
	versions map[string][]*WorkflowVersion // workflowName -> versions (sorted by version asc)
}

// NewVersionStore creates a new VersionStore.
func NewVersionStore() *VersionStore {
	return &VersionStore{
		versions: make(map[string][]*WorkflowVersion),
	}
}

// Save stores a new version of a workflow. It auto-increments the version number.
func (s *VersionStore) Save(workflowName, configYAML, description, createdBy string) (*WorkflowVersion, error) {
	if workflowName == "" {
		return nil, fmt.Errorf("workflow name is required")
	}
	if configYAML == "" {
		return nil, fmt.Errorf("config YAML is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	versions := s.versions[workflowName]
	nextVersion := 1
	if len(versions) > 0 {
		nextVersion = versions[len(versions)-1].Version + 1
	}

	v := &WorkflowVersion{
		WorkflowName: workflowName,
		Version:      nextVersion,
		ConfigYAML:   configYAML,
		Description:  description,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now(),
	}

	s.versions[workflowName] = append(s.versions[workflowName], v)
	return v, nil
}

// Get retrieves a specific version of a workflow.
func (s *VersionStore) Get(workflowName string, version int) (*WorkflowVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions := s.versions[workflowName]
	for _, v := range versions {
		if v.Version == version {
			return v, true
		}
	}
	return nil, false
}

// Latest returns the latest version of a workflow.
func (s *VersionStore) Latest(workflowName string) (*WorkflowVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions := s.versions[workflowName]
	if len(versions) == 0 {
		return nil, false
	}
	return versions[len(versions)-1], true
}

// List returns all versions for a workflow, newest first.
func (s *VersionStore) List(workflowName string) []*WorkflowVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions := s.versions[workflowName]
	result := make([]*WorkflowVersion, len(versions))
	copy(result, versions)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Version > result[j].Version
	})
	return result
}

// ListWorkflows returns the names of all workflows that have versions.
func (s *VersionStore) ListWorkflows() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.versions))
	for name := range s.versions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of versions for a workflow.
func (s *VersionStore) Count(workflowName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.versions[workflowName])
}

// RollbackFunc is called to apply a restored configuration.
type RollbackFunc func(workflowName, configYAML string) error

// Rollback restores a previous version and saves it as a new version.
// The apply function is called to apply the config; if it returns an error the rollback is aborted.
func Rollback(store *VersionStore, workflowName string, targetVersion int, createdBy string, apply RollbackFunc) (*WorkflowVersion, error) {
	target, ok := store.Get(workflowName, targetVersion)
	if !ok {
		return nil, fmt.Errorf("version %d not found for workflow %q", targetVersion, workflowName)
	}

	if apply != nil {
		if err := apply(workflowName, target.ConfigYAML); err != nil {
			return nil, fmt.Errorf("apply rollback failed: %w", err)
		}
	}

	desc := fmt.Sprintf("Rollback to version %d", targetVersion)
	v, err := store.Save(workflowName, target.ConfigYAML, desc, createdBy)
	if err != nil {
		return nil, err
	}

	return v, nil
}

// Diff represents changes between two versions.
type Diff struct {
	WorkflowName string `json:"workflowName"`
	FromVersion  int    `json:"fromVersion"`
	ToVersion    int    `json:"toVersion"`
	FromConfig   string `json:"fromConfig"`
	ToConfig     string `json:"toConfig"`
	Changed      bool   `json:"changed"`
}

// Compare returns a diff between two versions.
func Compare(store *VersionStore, workflowName string, fromVersion, toVersion int) (*Diff, error) {
	from, ok := store.Get(workflowName, fromVersion)
	if !ok {
		return nil, fmt.Errorf("version %d not found", fromVersion)
	}
	to, ok := store.Get(workflowName, toVersion)
	if !ok {
		return nil, fmt.Errorf("version %d not found", toVersion)
	}

	return &Diff{
		WorkflowName: workflowName,
		FromVersion:  fromVersion,
		ToVersion:    toVersion,
		FromConfig:   from.ConfigYAML,
		ToConfig:     to.ConfigYAML,
		Changed:      from.ConfigYAML != to.ConfigYAML,
	}, nil
}
