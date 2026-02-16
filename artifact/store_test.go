package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

func TestLocalStore_PutAndGet(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	content := []byte("hello world artifact content")
	err := store.Put(ctx, "exec-1", "build-output.tar.gz", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	rc, err := store.Get(ctx, "exec-1", "build-output.tar.gz")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestLocalStore_List(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	keys := []string{"artifact-a", "artifact-b", "artifact-c"}
	for _, key := range keys {
		err := store.Put(ctx, "exec-2", key, bytes.NewReader([]byte("data-"+key)))
		if err != nil {
			t.Fatalf("Put %q failed: %v", key, err)
		}
	}

	artifacts, err := store.List(ctx, "exec-2")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(artifacts) != len(keys) {
		t.Fatalf("expected %d artifacts, got %d", len(keys), len(artifacts))
	}

	// List returns sorted by key.
	for i, a := range artifacts {
		if a.Key != keys[i] {
			t.Errorf("artifact[%d].Key = %q, want %q", i, a.Key, keys[i])
		}
	}
}

func TestLocalStore_Delete(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	err := store.Put(ctx, "exec-3", "to-delete", bytes.NewReader([]byte("temporary")))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists.
	_, err = store.Get(ctx, "exec-3", "to-delete")
	if err != nil {
		t.Fatalf("Get before delete failed: %v", err)
	}

	err = store.Delete(ctx, "exec-3", "to-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone.
	_, err = store.Get(ctx, "exec-3", "to-delete")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}

	// Verify list is empty.
	artifacts, err := store.List(ctx, "exec-3")
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts after delete, got %d", len(artifacts))
	}
}

func TestLocalStore_Checksum(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	content := []byte("checksum verification content")

	// Compute expected SHA256.
	hasher := sha256.New()
	hasher.Write(content)
	expectedChecksum := hex.EncodeToString(hasher.Sum(nil))

	err := store.Put(ctx, "exec-4", "checksummed", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	artifacts, err := store.List(ctx, "exec-4")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	if artifacts[0].Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: got %q, want %q", artifacts[0].Checksum, expectedChecksum)
	}

	if artifacts[0].Size != int64(len(content)) {
		t.Errorf("size mismatch: got %d, want %d", artifacts[0].Size, len(content))
	}
}

func TestLocalStore_ListEmptyExecution(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	artifacts, err := store.List(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if artifacts != nil {
		t.Errorf("expected nil for nonexistent execution, got %v", artifacts)
	}
}

func TestLocalStore_GetNonexistent(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	_, err := store.Get(ctx, "no-such-exec", "no-such-key")
	if err == nil {
		t.Fatal("expected error for nonexistent artifact, got nil")
	}
}

func TestLocalStore_DeleteNonexistent(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	err := store.Delete(ctx, "no-such-exec", "no-such-key")
	if err == nil {
		t.Fatal("expected error for nonexistent artifact, got nil")
	}
}

func TestLocalStore_MultipleExecutions(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	// Store artifacts in two different executions.
	err := store.Put(ctx, "exec-a", "output", bytes.NewReader([]byte("exec-a-data")))
	if err != nil {
		t.Fatalf("Put exec-a failed: %v", err)
	}
	err = store.Put(ctx, "exec-b", "output", bytes.NewReader([]byte("exec-b-data")))
	if err != nil {
		t.Fatalf("Put exec-b failed: %v", err)
	}

	// Verify they are isolated.
	rc, err := store.Get(ctx, "exec-a", "output")
	if err != nil {
		t.Fatalf("Get exec-a failed: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "exec-a-data" {
		t.Errorf("exec-a content = %q, want %q", got, "exec-a-data")
	}

	rc, err = store.Get(ctx, "exec-b", "output")
	if err != nil {
		t.Fatalf("Get exec-b failed: %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if string(got) != "exec-b-data" {
		t.Errorf("exec-b content = %q, want %q", got, "exec-b-data")
	}
}
