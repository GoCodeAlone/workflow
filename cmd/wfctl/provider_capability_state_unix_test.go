//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

func TestPersistCredentialOperationStateSyncsContainingDirectory(t *testing.T) {
	stateDir := t.TempDir()
	oldSync := syncCredentialOperationDirectory
	var syncedDirectory string
	syncCredentialOperationDirectory = func(path string) error {
		syncedDirectory = path
		return nil
	}
	t.Cleanup(func() { syncCredentialOperationDirectory = oldSync })

	state := &credentialOperationState{
		OperationID: "operation-1",
		Source:      "example.source",
		LogicalName: "deploy-key",
	}
	if err := persistCredentialOperationState(stateDir, state, credentialOperationStarted); err != nil {
		t.Fatal(err)
	}
	if syncedDirectory != filepath.Clean(stateDir) {
		t.Fatalf("synced directory=%q, want %q", syncedDirectory, filepath.Clean(stateDir))
	}
}
