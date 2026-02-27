package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockDBStore is an in-memory implementation of DBConfigStore for testing.
type mockDBStore struct {
	mu    sync.Mutex
	docs  map[string][]byte
	calls int // tracks number of GetConfigDocument calls
}

func newMockDBStore() *mockDBStore {
	return &mockDBStore{docs: make(map[string][]byte)}
}

func (m *mockDBStore) GetConfigDocument(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	data, ok := m.docs[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return data, nil
}

func (m *mockDBStore) GetConfigDocumentHash(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.docs[key]
	if !ok {
		return "", fmt.Errorf("not found: %s", key)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (m *mockDBStore) PutConfigDocument(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[key] = data
	return nil
}

func (m *mockDBStore) set(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[key] = data
}

func (m *mockDBStore) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

const testConfigYAML = `
modules:
  - name: server
    type: http.server
    config:
      port: 8080
  - name: router
    type: http.router
`

func TestDatabaseSource_Load(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	src := NewDatabaseSource(store)
	cfg, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "server" {
		t.Errorf("expected module name 'server', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[1].Name != "router" {
		t.Errorf("expected module name 'router', got %q", cfg.Modules[1].Name)
	}
}

func TestDatabaseSource_Load_CustomKey(t *testing.T) {
	store := newMockDBStore()
	store.set("prod", []byte(testConfigYAML))

	src := NewDatabaseSource(store, WithConfigKey("prod"))
	cfg, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(cfg.Modules))
	}
}

func TestDatabaseSource_CacheHit(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	// Long refresh interval so the cache never expires during the test.
	src := NewDatabaseSource(store, WithRefreshInterval(10*time.Minute))

	ctx := context.Background()

	_, err := src.Load(ctx)
	if err != nil {
		t.Fatalf("first Load failed: %v", err)
	}
	callsAfterFirst := store.getCalls()

	_, err = src.Load(ctx)
	if err != nil {
		t.Fatalf("second Load failed: %v", err)
	}
	callsAfterSecond := store.getCalls()

	if callsAfterFirst != 1 {
		t.Errorf("expected 1 DB call after first load, got %d", callsAfterFirst)
	}
	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no additional DB calls on cache hit, got %d total calls", callsAfterSecond)
	}
}

func TestDatabaseSource_CacheExpiry(t *testing.T) {
	store := newMockDBStore()
	store.set("default", []byte(testConfigYAML))

	// Very short refresh interval so the cache expires quickly.
	src := NewDatabaseSource(store, WithRefreshInterval(10*time.Millisecond))

	ctx := context.Background()

	_, err := src.Load(ctx)
	if err != nil {
		t.Fatalf("first Load failed: %v", err)
	}
	callsAfterFirst := store.getCalls()

	// Wait for cache to expire.
	time.Sleep(20 * time.Millisecond)

	_, err = src.Load(ctx)
	if err != nil {
		t.Fatalf("second Load failed: %v", err)
	}
	callsAfterSecond := store.getCalls()

	if callsAfterFirst != 1 {
		t.Errorf("expected 1 DB call after first load, got %d", callsAfterFirst)
	}
	if callsAfterSecond != 2 {
		t.Errorf("expected 2 DB calls total after cache expiry, got %d", callsAfterSecond)
	}
}

func TestDatabaseSource_Hash(t *testing.T) {
	store := newMockDBStore()
	data := []byte(testConfigYAML)
	store.set("default", data)

	src := NewDatabaseSource(store)
	ctx := context.Background()

	hash, err := src.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Verify hash is consistent with the raw bytes.
	sum := sha256.Sum256(data)
	expected := hex.EncodeToString(sum[:])
	if hash != expected {
		t.Errorf("hash mismatch: got %q, want %q", hash, expected)
	}

	// Calling Hash again should return the same value.
	hash2, err := src.Hash(ctx)
	if err != nil {
		t.Fatalf("second Hash failed: %v", err)
	}
	if hash != hash2 {
		t.Errorf("hash not stable: got %q then %q", hash, hash2)
	}
}

func TestDatabaseSource_Name(t *testing.T) {
	store := newMockDBStore()
	src := NewDatabaseSource(store)
	if src.Name() != "database:default" {
		t.Errorf("unexpected name: %q", src.Name())
	}

	src2 := NewDatabaseSource(store, WithConfigKey("prod"))
	if src2.Name() != "database:prod" {
		t.Errorf("unexpected name: %q", src2.Name())
	}
}

func TestDatabaseSource_Load_NotFound(t *testing.T) {
	store := newMockDBStore()
	src := NewDatabaseSource(store)
	_, err := src.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestDatabaseSource_ImplementsConfigSource(t *testing.T) {
	store := newMockDBStore()
	var _ ConfigSource = NewDatabaseSource(store)
}
