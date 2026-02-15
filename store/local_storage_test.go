package store

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorage_PutGetDelete(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Put a file
	content := []byte("hello workspace")
	if err := ls.Put(ctx, "test.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get the file
	rc, err := ls.Get(ctx, "test.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	// Stat the file
	info, err := ls.Stat(ctx, "test.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name != "test.txt" {
		t.Errorf("name mismatch: got %q", info.Name)
	}
	if info.Size != int64(len(content)) {
		t.Errorf("size mismatch: got %d, want %d", info.Size, len(content))
	}

	// Delete the file
	if err := ls.Delete(ctx, "test.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify deletion
	_, err = ls.Stat(ctx, "test.txt")
	if err == nil {
		t.Fatal("expected error after deletion, got nil")
	}
}

func TestLocalStorage_List(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Create some files
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := ls.Put(ctx, name, bytes.NewReader([]byte("data"))); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}

	// Create a subdirectory with a file
	if err := ls.Put(ctx, "sub/c.txt", bytes.NewReader([]byte("sub-data"))); err != nil {
		t.Fatalf("Put sub/c.txt: %v", err)
	}

	// List root
	files, err := ls.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 3 { // a.txt, b.txt, sub/
		t.Errorf("expected 3 entries, got %d", len(files))
	}

	// List subdirectory
	subFiles, err := ls.List(ctx, "sub")
	if err != nil {
		t.Fatalf("List sub: %v", err)
	}
	if len(subFiles) != 1 {
		t.Errorf("expected 1 entry in sub, got %d", len(subFiles))
	}
}

func TestLocalStorage_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	files, err := ls.List(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("List nonexistent: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 entries, got %d", len(files))
	}
}

func TestLocalStorage_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Attempting to escape root should fail
	_, err = ls.Get(ctx, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}

	err = ls.Put(ctx, "../../../tmp/evil.txt", bytes.NewReader([]byte("bad")))
	if err == nil {
		t.Fatal("expected error for path traversal in Put, got nil")
	}
}

func TestLocalStorage_NestedPut(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Put into nested directories that don't exist yet
	if err := ls.Put(ctx, "a/b/c/file.txt", bytes.NewReader([]byte("nested"))); err != nil {
		t.Fatalf("Put nested: %v", err)
	}

	// Verify the file exists on disk
	full := filepath.Join(dir, "a", "b", "c", "file.txt")
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
