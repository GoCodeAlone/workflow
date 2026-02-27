package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const watcherTestYAML = `
modules:
  - name: watcher-server
    type: http.server
    config:
      port: 8080
`

const watcherTestYAMLv2 = `
modules:
  - name: watcher-server
    type: http.server
    config:
      port: 9090
`

func TestConfigWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	var called atomic.Int32
	var mu sync.Mutex
	var lastEvt ConfigChangeEvent

	src := NewFileSource(fp)
	w := NewConfigWatcher(src, func(evt ConfigChangeEvent) {
		mu.Lock()
		lastEvt = evt
		mu.Unlock()
		called.Add(1)
	}, WithWatchDebounce(50*time.Millisecond))

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop() })

	// Modify the file.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(fp, []byte(watcherTestYAMLv2), 0644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	// Wait for debounce + processing.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if called.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if called.Load() == 0 {
		t.Fatal("onChange was not called after file modification")
	}

	mu.Lock()
	evt := lastEvt
	mu.Unlock()

	if evt.Config == nil {
		t.Fatal("onChange event has nil Config")
	}
	if evt.NewHash == "" || evt.OldHash == "" {
		t.Errorf("expected non-empty hashes, got old=%q new=%q", evt.OldHash, evt.NewHash)
	}
	if evt.OldHash == evt.NewHash {
		t.Error("expected old and new hashes to differ")
	}
}

func TestConfigWatcher_DebounceMultipleWrites(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	var called atomic.Int32

	src := NewFileSource(fp)
	w := NewConfigWatcher(src, func(evt ConfigChangeEvent) {
		called.Add(1)
	}, WithWatchDebounce(200*time.Millisecond))

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop() })

	time.Sleep(50 * time.Millisecond)

	// Rapid succession of writes — all within the debounce window.
	for i := 0; i < 5; i++ {
		content := watcherTestYAMLv2
		if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce to fire once.
	time.Sleep(600 * time.Millisecond)

	count := called.Load()
	if count == 0 {
		t.Fatal("expected at least one onChange call")
	}
	// Due to debounce, we expect far fewer calls than writes.
	// The debounce period is 200ms and we wait 10ms between writes,
	// so it's valid to have 1–2 calls but not 5.
	if count > 3 {
		t.Errorf("expected debounce to reduce calls (got %d, expected ≤3)", count)
	}
}

func TestConfigWatcher_SkipUnchangedContent(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	var called atomic.Int32

	src := NewFileSource(fp)
	w := NewConfigWatcher(src, func(evt ConfigChangeEvent) {
		called.Add(1)
	}, WithWatchDebounce(50*time.Millisecond))

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop() })

	time.Sleep(100 * time.Millisecond)

	// Rewrite the exact same content.
	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("rewrite same content: %v", err)
	}

	// Wait long enough for debounce to fire.
	time.Sleep(300 * time.Millisecond)

	if called.Load() != 0 {
		t.Errorf("expected onChange NOT to be called for unchanged content, got %d calls", called.Load())
	}
}

func TestConfigWatcher_StopCleanup(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	src := NewFileSource(fp)
	w := NewConfigWatcher(src, func(evt ConfigChangeEvent) {}, WithWatchDebounce(50*time.Millisecond))

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop should return quickly and cleanly.
	done := make(chan error, 1)
	go func() { done <- w.Stop() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() timed out — possible goroutine leak")
	}
}

func TestConfigWatcher_Hash_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(fp, []byte(watcherTestYAML), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	src := NewFileSource(fp)
	ctx := context.Background()
	h, err := src.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() error: %v", err)
	}
	if h == "" {
		t.Fatal("expected non-empty hash")
	}
}
