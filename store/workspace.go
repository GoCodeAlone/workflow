package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// The projectID is user-supplied (from a URL segment), so we verify the resolved
// workspace path stays inside wm.dataDir/workspaces/ to prevent path traversal
// (e.g. projectID="../../etc" must not escape the workspaces base directory).
func (wm *WorkspaceManager) StorageForProject(projectID string) (*LocalStorage, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}

	// Reject project IDs that are not a single, plain path segment. "." and ".."
	// would resolve to the workspaces root or its parent (breaking isolation /
	// escaping the base), and any path separator means the caller is trying to
	// address a nested or sibling location rather than a single project dir.
	if projectID == "." || projectID == ".." ||
		strings.ContainsRune(projectID, '/') ||
		strings.ContainsRune(projectID, os.PathSeparator) {
		return nil, fmt.Errorf("invalid project ID %q: must be a single path segment", projectID)
	}

	// Resolve the base directory (workspaces root) to an absolute path.
	workspacesBase, err := filepath.Abs(filepath.Join(wm.dataDir, "workspaces"))
	if err != nil {
		return nil, fmt.Errorf("resolve workspaces base: %w", err)
	}
	workspacesBase = filepath.Clean(workspacesBase) + string(os.PathSeparator)

	// Resolve the candidate workspace path.
	candidate, err := filepath.Abs(filepath.Join(wm.dataDir, "workspaces", projectID))
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}

	// Enforce containment: the workspace must live inside the workspaces base.
	if !strings.HasPrefix(candidate+string(os.PathSeparator), workspacesBase) {
		return nil, fmt.Errorf("project ID %q resolves outside the workspaces directory", projectID)
	}

	return NewLocalStorage(candidate)
}
