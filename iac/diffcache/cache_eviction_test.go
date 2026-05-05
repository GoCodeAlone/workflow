package diffcache

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCache_LRUEvictionByCount verifies the LRU eviction trigger when
// the entry count exceeds maxEntries. Per the rev2 lifecycle constraint
// (10% per-overflow batch), the eviction is amortized — for the test
// we drive it with a small cap (10 entries; 10% = 1 evicted per
// over-cap Put) to keep the test fast.
//
// Mtime-resolution assumption: the 1ms inter-Put sleep relies on the
// underlying filesystem providing mtime granularity ≤ 1ms (true on
// Linux ext4/btrfs/xfs, macOS APFS, Windows NTFS — the CI matrix).
// Coarse-mtime filesystems (FAT32 with 2s, SMB shares with ~1s) would
// produce indistinguishable mtimes for adjacent Puts and the
// secondary path-sort (by sha256 hash of the cache key) would decide
// eviction order — uncorrelated with insertion order. The package
// intentionally does not support such filesystems.
func TestCache_LRUEvictionByCount(t *testing.T) {
	dir := t.TempDir()
	c := &filesystemCache{
		dir:        dir,
		maxEntries: 10,
		maxBytes:   defaultMaxBytes, // not the trigger here
	}
	// Fill to the cap. Each Put with a different key creates a new file.
	// 1ms between Puts so mtimes are distinguishable on the supported
	// filesystems (see godoc above).
	for i := range 10 {
		c.Put(Key{Type: fmt.Sprintf("k%d", i)}, interfaces.DiffResult{})
		time.Sleep(time.Millisecond)
	}
	if got := countCacheFiles(t, dir); got != 10 {
		t.Fatalf("expected 10 entries pre-overflow; got %d", got)
	}
	// Trigger overflow: one more Put → cap exceeded → evict 1 (10% of 10).
	c.Put(Key{Type: "k_overflow"}, interfaces.DiffResult{})
	got := countCacheFiles(t, dir)
	// After eviction (10% of 10 = 1) + new entry: 10 - 1 + 1 = 10.
	if got != 10 {
		t.Errorf("post-overflow count: got %d entries, want 10 (cap holds)", got)
	}
	// Oldest key (k0) should have been evicted.
	if _, hit := c.Get(Key{Type: "k0"}); hit {
		t.Errorf("oldest key (k0) should be evicted")
	}
	// Newest key (k_overflow) should still be present.
	if _, hit := c.Get(Key{Type: "k_overflow"}); !hit {
		t.Errorf("newest key (k_overflow) should remain")
	}
}

// TestCache_LRUEvictionBatchOf10Percent verifies that when the cap is
// large enough to make 10% a multi-entry batch, multiple oldest
// entries are evicted in one pass. This locks the "amortized cost" /
// "evict oldest 10% in one pass" rev2 constraint.
func TestCache_LRUEvictionBatchOf10Percent(t *testing.T) {
	dir := t.TempDir()
	c := &filesystemCache{
		dir:        dir,
		maxEntries: 100,
		maxBytes:   defaultMaxBytes,
	}
	for i := range 100 {
		c.Put(Key{Type: fmt.Sprintf("k%03d", i)}, interfaces.DiffResult{})
		time.Sleep(time.Millisecond)
	}
	// Trigger overflow: one extra Put → evict 10 oldest (10% of 100) +
	// add 1 → 100 - 10 + 1 = 91.
	c.Put(Key{Type: "k_overflow"}, interfaces.DiffResult{})
	got := countCacheFiles(t, dir)
	if got < 88 || got > 92 {
		// Allow ±2 for filesystems with mtime resolution coarser than
		// the 1ms sleep; the central tendency must be ~91.
		t.Errorf("post-overflow count: got %d, want ~91 (allow ±2 for mtime precision)", got)
	}
	// First 5 oldest keys should be evicted.
	for i := range 5 {
		k := Key{Type: fmt.Sprintf("k%03d", i)}
		if _, hit := c.Get(k); hit {
			t.Errorf("k%03d should be in the evicted oldest 10%%", i)
		}
	}
}

// TestCache_LRURefreshesOnGet verifies that Get touches the cache
// file's mtime so that frequently-read entries are NOT evicted as if
// they were stale. The contract: an entry written first but Get'd
// after a newer entry was Put should outlive the newer entry under
// LRU eviction. Without the mtime-refresh in Get, eviction would
// behave as FIFO-by-write rather than true LRU.
func TestCache_LRURefreshesOnGet(t *testing.T) {
	dir := t.TempDir()
	c := &filesystemCache{
		dir:        dir,
		maxEntries: 10,
		maxBytes:   defaultMaxBytes,
	}
	// Fill exactly to the cap. k0 is the oldest by write order.
	for i := range 10 {
		c.Put(Key{Type: fmt.Sprintf("k%d", i)}, interfaces.DiffResult{})
		time.Sleep(time.Millisecond)
	}
	// Get k0 — this MUST refresh mtime so k0 is now the youngest by
	// access time, even though it was the oldest by write time. Without
	// the refresh, k0 retains its original (oldest) mtime and gets
	// evicted on the next over-cap Put.
	if _, hit := c.Get(Key{Type: "k0"}); !hit {
		t.Fatal("expected hit on k0 before eviction")
	}
	time.Sleep(time.Millisecond)
	// Trigger overflow: one extra Put → evict 1 (10% of 10). Under true
	// LRU (mtime refreshed on Get), k0 is now the freshest entry and k1
	// is the oldest, so k1 should be evicted, not k0.
	c.Put(Key{Type: "k_overflow"}, interfaces.DiffResult{})
	if _, hit := c.Get(Key{Type: "k0"}); !hit {
		t.Errorf("k0 was Get'd before eviction; LRU should retain it (Get must refresh mtime)")
	}
	if _, hit := c.Get(Key{Type: "k1"}); hit {
		t.Errorf("k1 was the oldest by access time; LRU should have evicted it")
	}
}

// countCacheFiles returns the number of *.json files under dir. Used
// to assert eviction behavior without depending on cache internals.
func countCacheFiles(t *testing.T, dir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return len(matches)
}

// TestCache_EvictionTouchesNothingWhenUnderCap verifies the no-op
// case: Put when count < maxEntries leaves all existing files alone.
func TestCache_EvictionTouchesNothingWhenUnderCap(t *testing.T) {
	dir := t.TempDir()
	c := &filesystemCache{
		dir:        dir,
		maxEntries: 100,
		maxBytes:   defaultMaxBytes,
	}
	c.Put(Key{Type: "a"}, interfaces.DiffResult{})
	c.Put(Key{Type: "b"}, interfaces.DiffResult{})
	c.Put(Key{Type: "c"}, interfaces.DiffResult{})
	if got := countCacheFiles(t, dir); got != 3 {
		t.Errorf("under-cap puts should not evict; got %d entries", got)
	}
	// All three should be retrievable.
	for _, k := range []string{"a", "b", "c"} {
		if _, hit := c.Get(Key{Type: k}); !hit {
			t.Errorf("under-cap key %q should be retained", k)
		}
	}
}
