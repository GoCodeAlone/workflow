package store

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestWorkspaceManager_WorkspacePath(t *testing.T) {
	wm := NewWorkspaceManager("/data")

	path := wm.WorkspacePath("proj-1")
	expected := filepath.Join("/data", "workspaces", "proj-1")
	if path != expected {
		t.Errorf("WorkspacePath: got %q, want %q", path, expected)
	}
}

func TestWorkspaceManager_StorageForProject(t *testing.T) {
	dir := t.TempDir()
	wm := NewWorkspaceManager(dir)

	storage, err := wm.StorageForProject("proj-1")
	if err != nil {
		t.Fatalf("StorageForProject: %v", err)
	}
	if storage == nil {
		t.Fatal("expected non-nil storage")
	}

	// Verify root is set correctly
	expected := filepath.Join(dir, "workspaces", "proj-1")
	if storage.Root() != expected {
		t.Errorf("Root: got %q, want %q", storage.Root(), expected)
	}
}

func TestWorkspaceManager_StorageForProject_EmptyID(t *testing.T) {
	wm := NewWorkspaceManager(t.TempDir())

	_, err := wm.StorageForProject("")
	if err == nil {
		t.Fatal("expected error for empty project ID, got nil")
	}
}

func TestWorkspaceManager_ProjectIsolation(t *testing.T) {
	dir := t.TempDir()
	wm := NewWorkspaceManager(dir)

	ctx := context.Background()

	// Write a file in project A
	storageA, err := wm.StorageForProject("proj-a")
	if err != nil {
		t.Fatalf("StorageForProject(proj-a): %v", err)
	}
	if err := storageA.Put(ctx, "data.txt", bytes.NewReader([]byte("project A"))); err != nil {
		t.Fatalf("Put in proj-a: %v", err)
	}

	// Write a file in project B
	storageB, err := wm.StorageForProject("proj-b")
	if err != nil {
		t.Fatalf("StorageForProject(proj-b): %v", err)
	}
	if err := storageB.Put(ctx, "data.txt", bytes.NewReader([]byte("project B"))); err != nil {
		t.Fatalf("Put in proj-b: %v", err)
	}

	// Verify project A has its own data
	rc, err := storageA.Get(ctx, "data.txt")
	if err != nil {
		t.Fatalf("Get from proj-a: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "project A" {
		t.Errorf("proj-a: got %q, want %q", string(got), "project A")
	}

	// Verify project B has its own data
	rc2, err := storageB.Get(ctx, "data.txt")
	if err != nil {
		t.Fatalf("Get from proj-b: %v", err)
	}
	defer rc2.Close()
	got2, _ := io.ReadAll(rc2)
	if string(got2) != "project B" {
		t.Errorf("proj-b: got %q, want %q", string(got2), "project B")
	}
}
