package config

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const altConfigYAML = `
modules:
  - name: cache
    type: redis.cache
    config:
      addr: localhost:6379
`

func newTestPoller(store *mockDBStore, interval time.Duration, onChange func(ConfigChangeEvent)) *DatabasePoller {
	src := NewDatabaseSource(store,
		WithRefreshInterval(0), // disable caching so changes are seen immediately
	)
	return NewDatabasePoller(src, interval, onChange, slog.Default())
}

func TestDatabasePoller_DetectsChange(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	var (
		mu      sync.Mutex
		events  []ConfigChangeEvent
	)
	onChange := func(e ConfigChangeEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	poller := newTestPoller(store, 20*time.Millisecond, onChange)
	ctx := context.Background()
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer poller.Stop()

	// Update the store content after a short delay.
	time.Sleep(10 * time.Millisecond)
	store.set("default", []byte(altConfigYAML))

	// Wait long enough for at least one poll to detect the change.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatal("expected onChange to be called after config change, but it was not")
	}
	e := events[0]
	if e.Source != "database:default" {
		t.Errorf("unexpected source: %q", e.Source)
	}
	if e.OldHash == e.NewHash {
		t.Error("expected OldHash != NewHash after change")
	}
	if e.Config == nil {
		t.Error("expected non-nil Config in event")
	}
}

func TestDatabasePoller_SkipsUnchanged(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	var called atomic.Int32
	onChange := func(ConfigChangeEvent) { called.Add(1) }

	poller := newTestPoller(store, 20*time.Millisecond, onChange)
	ctx := context.Background()
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let several poll ticks run without changing the config.
	time.Sleep(100 * time.Millisecond)
	poller.Stop()

	if n := called.Load(); n != 0 {
		t.Errorf("onChange called %d times for unchanged config, expected 0", n)
	}
}

func TestDatabasePoller_Stop(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	poller := newTestPoller(store, 10*time.Millisecond, func(ConfigChangeEvent) {})
	ctx := context.Background()
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should return promptly (within a reasonable timeout).
	done := make(chan struct{})
	go func() {
		poller.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds")
	}
}

func TestDatabasePoller_ContextCancel(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	poller := newTestPoller(store, 10*time.Millisecond, func(ConfigChangeEvent) {})
	ctx, cancel := context.WithCancel(context.Background())
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()

	// After context cancellation the goroutine should stop; Stop() must still
	// be safe to call (close(done) only once).
	done := make(chan struct{})
	go func() {
		poller.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds after context cancel")
	}
}

func TestDatabasePoller_MultipleChanges(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	var (
		mu     sync.Mutex
		events []ConfigChangeEvent
	)
	onChange := func(e ConfigChangeEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	poller := newTestPoller(store, 20*time.Millisecond, onChange)
	ctx := context.Background()
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer poller.Stop()

	// Apply two successive changes.
	configs := []string{altConfigYAML, testConfigYAML}
	for _, c := range configs {
		time.Sleep(40 * time.Millisecond)
		store.set("default", []byte(c))
		// Wait for detection.
		time.Sleep(60 * time.Millisecond)
	}

	mu.Lock()
	n := len(events)
	mu.Unlock()

	if n < 2 {
		t.Errorf("expected at least 2 change events, got %d", n)
	}
}
