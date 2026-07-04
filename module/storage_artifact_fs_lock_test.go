package module

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestArtifactFSDownloadDoesNotBlockBehindStoreMutex(t *testing.T) {
	store := NewArtifactFSModule("artifacts", ArtifactFSConfig{BasePath: t.TempDir()})
	if err := store.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := store.Upload(context.Background(), "a.txt", strings.NewReader("hello"), nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	readDone := make(chan error, 1)
	go func() {
		rc, _, err := store.Download(context.Background(), "a.txt")
		if rc != nil {
			_ = rc.Close()
		}
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Download blocked behind store mutex")
	}
}
