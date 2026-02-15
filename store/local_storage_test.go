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

	err = ls.Delete(ctx, "../../../tmp/evil.txt")
	if err == nil {
		t.Fatal("expected error for path traversal in Delete, got nil")
	}

	_, err = ls.Stat(ctx, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal in Stat, got nil")
	}

	_, err = ls.List(ctx, "../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal in List, got nil")
	}

	err = ls.MkdirAll(ctx, "../../../tmp/evil")
	if err == nil {
		t.Fatal("expected error for path traversal in MkdirAll, got nil")
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

func TestLocalStorage_MkdirAll(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Create nested directories
	if err := ls.MkdirAll(ctx, "a/b/c"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Verify directory exists
	info, err := ls.Stat(ctx, "a/b/c")
	if err != nil {
		t.Fatalf("Stat after MkdirAll: %v", err)
	}
	if !info.IsDir {
		t.Error("expected directory, got file")
	}
	if info.Name != "c" {
		t.Errorf("expected name 'c', got %q", info.Name)
	}

	// Should be idempotent
	if err := ls.MkdirAll(ctx, "a/b/c"); err != nil {
		t.Fatalf("MkdirAll idempotent: %v", err)
	}

	// Create a file in the directory
	if err := ls.Put(ctx, "a/b/c/test.txt", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatalf("Put in created dir: %v", err)
	}

	// List the directory
	files, err := ls.List(ctx, "a/b/c")
	if err != nil {
		t.Fatalf("List created dir: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestLocalStorage_ContentType(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name        string
		wantTypeSet bool // true if we expect a non-empty content type
	}{
		{"document.json", true},
		{"style.css", true},
		{"page.html", true},
		{"noextension", false},
	}

	for _, tt := range tests {
		if err := ls.Put(ctx, tt.name, bytes.NewReader([]byte("data"))); err != nil {
			t.Fatalf("Put %s: %v", tt.name, err)
		}

		info, err := ls.Stat(ctx, tt.name)
		if err != nil {
			t.Fatalf("Stat %s: %v", tt.name, err)
		}

		if tt.wantTypeSet && info.ContentType == "" {
			t.Errorf("Stat(%s): expected non-empty ContentType", tt.name)
		}
		if !tt.wantTypeSet && info.ContentType != "" {
			t.Errorf("Stat(%s): expected empty ContentType, got %q", tt.name, info.ContentType)
		}
	}

	// Also verify List returns content types
	files, err := ls.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	foundJSON := false
	for _, f := range files {
		if f.Name == "document.json" && f.ContentType != "" {
			foundJSON = true
		}
	}
	if !foundJSON {
		t.Error("List: expected document.json to have ContentType set")
	}
}

func TestLocalStorage_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	_, err = ls.Get(context.Background(), "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLocalStorage_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	err = ls.Delete(context.Background(), "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent file, got nil")
	}
}

func TestLocalStorage_ImplementsStorageProvider(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	// Compile-time check that LocalStorage implements StorageProvider
	var _ StorageProvider = ls
}

func TestLocalStorage_Root(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	root := ls.Root()
	if root == "" {
		t.Fatal("Root() returned empty string")
	}

	// Root should be an absolute path
	if !filepath.IsAbs(root) {
		t.Errorf("Root() returned non-absolute path: %q", root)
	}
}

func TestLocalStorage_OverwriteFile(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	ctx := context.Background()

	// Write file
	if err := ls.Put(ctx, "file.txt", bytes.NewReader([]byte("original"))); err != nil {
		t.Fatalf("Put original: %v", err)
	}

	// Overwrite
	if err := ls.Put(ctx, "file.txt", bytes.NewReader([]byte("updated"))); err != nil {
		t.Fatalf("Put updated: %v", err)
	}

	// Read back
	rc, err := ls.Get(ctx, "file.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "updated" {
		t.Errorf("expected 'updated', got %q", string(got))
	}
}
