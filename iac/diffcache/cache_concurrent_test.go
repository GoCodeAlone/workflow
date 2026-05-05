package diffcache

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCache_ConcurrentSameKeyPut verifies that multiple goroutines
// calling Put with the same Key do not race on a shared temp filename.
// Pre-fix, both Puts wrote to `<key>.json.tmp` and one would clobber
// the other's temp file mid-write, producing either a Rename failure
// or, worse, a half-written final file. With os.CreateTemp's per-call
// unique suffix, each Put has its own temp path; the final Rename is
// racy in the sense that one payload "wins," but both payloads are
// equivalent (same Key → same canonical input) so the outcome is
// deterministic from the caller's perspective.
//
// The test asserts: (a) no panic, (b) no leftover *.tmp files in the
// cache dir after all goroutines finish, (c) the final cache file is
// readable and decodes successfully. Run under -race to catch any
// shared-state mutation that slipped through.
func TestCache_ConcurrentSameKeyPut(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir).(*filesystemCache)
	key := Key{Type: "infra.vpc", ProviderID: "vpc-abc"}
	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			c.Put(key, interfaces.DiffResult{NeedsUpdate: true})
		}()
	}
	wg.Wait()

	// No leftover temp files: every Put either renamed successfully
	// (no orphan) or failed and cleaned up via os.Remove.
	tmps, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob tmp: %v", err)
	}
	if len(tmps) != 0 {
		t.Errorf("expected 0 leftover *.tmp files; found %d: %v", len(tmps), tmps)
	}

	// Final cache file is readable + decodes.
	got, hit := c.Get(key)
	if !hit {
		t.Fatal("expected hit after concurrent Puts of the same key")
	}
	if !got.NeedsUpdate {
		t.Errorf("Get returned wrong value after concurrent Puts: %+v", got)
	}
}
