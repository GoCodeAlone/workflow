package platform_test

import (
	"os"
	"testing"
)

// TestMain ensures the package-level diff cache initializes to the
// disabled (noop) backend across all platform tests. Without this,
// the cache would lazily resolve to the filesystem backend rooted at
// ~/.cache/wfctl/diff/ on the developer's machine — both polluting
// the user's cache and creating cross-test interference (later
// ComputePlan tests would observe DiffResults Put by earlier tests
// when their cache keys happen to align).
//
// Tests in this package that specifically exercise cache-hit
// behaviour (differ_cache_test.go, internal package) override the
// noop backend per-test via setDiffCacheForTest with a controlled
// in-memory cache, then restore the noop backend on cleanup.
func TestMain(m *testing.M) {
	if err := os.Setenv("WFCTL_DIFFCACHE", "disabled"); err != nil {
		panic("setenv WFCTL_DIFFCACHE: " + err.Error())
	}
	os.Exit(m.Run())
}
