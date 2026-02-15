package store

import (
	"fmt"
	"path/filepath"
)

// WorkspaceManager manages project workspace directories and their storage.
type WorkspaceManager struct {
	dataDir string
}

// NewWorkspaceManager creates a new WorkspaceManager rooted at the given data directory.
func NewWorkspaceManager(dataDir string) *WorkspaceManager {
	return &WorkspaceManager{dataDir: dataDir}
}

// WorkspacePath returns the filesystem path for a project workspace.
func (wm *WorkspaceManager) WorkspacePath(projectID string) string {
	return filepath.Join(wm.dataDir, "workspaces", projectID)
}

// StorageForProject returns a LocalStorage provider scoped to a project workspace.
func (wm *WorkspaceManager) StorageForProject(projectID string) (*LocalStorage, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	return NewLocalStorage(wm.WorkspacePath(projectID))
}
