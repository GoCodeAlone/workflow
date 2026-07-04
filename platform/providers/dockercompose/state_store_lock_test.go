package dockercompose

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestFileStateStoreGetResourceDoesNotBlockBehindStoreMutex(t *testing.T) {
	store, err := NewFileStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStateStore: %v", err)
	}
	ctx := context.Background()
	if err := store.SaveResource(ctx, "ctx-a", &platform.ResourceOutput{
		Name:       "resource-a",
		Type:       "service",
		Properties: map[string]any{},
	}); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	readDone := make(chan error, 1)
	go func() {
		_, err := store.GetResource(ctx, "ctx-a", "resource-a")
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("GetResource: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("GetResource blocked behind store mutex")
	}
}
