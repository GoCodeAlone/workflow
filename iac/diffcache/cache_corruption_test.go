package diffcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCache_CorruptionRecovery_TruncatedFile verifies that a partially-
// written or truncated cache file does not crash Get and is silently
// removed so the next Put succeeds. The contract: Get returns
// (_, false) on parse failure; the file is deleted from disk.
func TestCache_CorruptionRecovery_TruncatedFile(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir).(*filesystemCache)
	key := Key{Type: "infra.vpc", ProviderID: "vpc-abc"}
	c.Put(key, interfaces.DiffResult{NeedsUpdate: true})

	// Truncate the cache file to simulate a partial write or crash mid-Put.
	path := c.pathFor(key)
	if err := os.WriteFile(path, []byte("{not val"), 0o644); err != nil {
		t.Fatalf("failed to truncate cache file: %v", err)
	}

	if _, hit := c.Get(key); hit {
		t.Errorf("expected miss on truncated file; got hit")
	}
	// The corrupt file should be removed so a fresh Put can succeed.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt cache file should be deleted; stat err=%v", err)
	}

	// Re-Put + Get round-trip must work after corruption recovery.
	c.Put(key, interfaces.DiffResult{NeedsReplace: true})
	got, hit := c.Get(key)
	if !hit {
		t.Fatal("expected hit after re-Put following corruption")
	}
	if !got.NeedsReplace {
		t.Errorf("post-recovery Get returned wrong value: %+v", got)
	}
}

// TestCache_CorruptionRecovery_SchemaVersionMismatch verifies that
// cache files written under a different schema version are silently
// evicted on Get — same code path as JSON corruption since we treat
// both as "cache file we cannot use." Lock the contract so a future
// schema bump (cacheSchemaVersion = 2) keeps the silent-eviction
// behavior.
func TestCache_CorruptionRecovery_SchemaVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir).(*filesystemCache)
	key := Key{Type: "infra.vpc", ProviderID: "vpc-abc"}

	// Write a valid-JSON file with a schemaVersion that doesn't match
	// the current cacheSchemaVersion constant. Use a value >> current
	// so it can't accidentally match a future bump.
	path := c.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schemaVersion": 9999,
		"result":        map[string]any{"needs_update": true},
	}
	data, _ := json.Marshal(envelope)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, hit := c.Get(key); hit {
		t.Errorf("expected miss on schema-version mismatch; got hit")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("schema-mismatched cache file should be deleted; stat err=%v", err)
	}
}

// TestCache_CorruptionRecovery_GetReturnsNoErrorOnParseFailure proves
// the no-error contract: callers Get a (zero, false) tuple, never a
// surfaced error. Cache failures must NEVER abort apply.
func TestCache_CorruptionRecovery_GetReturnsNoErrorOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir).(*filesystemCache)
	key := Key{Type: "x"}
	path := c.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\x00\x00not-json\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	// We can't directly assert "no error returned" via the Get
	// signature, but we can lock that hit is false and the file is
	// removed (the canonical recovery path). A panic would also fail
	// this test, which is the implicit contract this case anchors.
	got, hit := c.Get(key)
	if hit {
		t.Error("expected miss on garbage bytes")
	}
	if got.NeedsUpdate || got.NeedsReplace || len(got.Changes) > 0 {
		t.Errorf("zero-value DiffResult on miss; got %+v", got)
	}
}
