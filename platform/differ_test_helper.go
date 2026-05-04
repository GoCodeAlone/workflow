package platform

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
)

// SetDiffCacheForTest swaps the package-level diff cache for c and
// restores the previous instance via t.Cleanup. Lives in a non-_test.go
// file so external test packages (notably iac/conformance) can install a
// no-op cache without touching unexported symbols and without relying on
// the WFCTL_DIFFCACHE env var, which is honoured only on the very first
// call to getDiffCache thanks to sync.Once initialization.
//
// Why this exists: the package-level cache is sync.Once-initialized on
// the first ComputePlan call in a process. A CI run that exercises
// ComputePlan in another test first leaves a primed cache that masks
// live-driver regressions in subsequent conformance runs. The
// architecturally-correct fix is a public helper that Stores into the
// atomic cache pointer directly (Store is safe concurrently with
// production Loads) — the prior `t.Setenv("WFCTL_DIFFCACHE", "disabled")`
// workaround inside conformance scenarios was best-effort.
//
// Cleanup contract: t.Cleanup restores the prior pointer if there was
// one, or seeds a fresh default cache when no prior value existed (so
// subsequent production code paths in the same test binary still
// observe a working cache and don't trip getDiffCache's defensive
// noop-fallback). Marked t.Helper() so Cleanup-time failures attribute
// to the caller's line.
//
// Importing testing in this non-test file is intentional and consistent
// with sibling helpers (see wftest/, module/module_test_helpers.go).
// The Go linker drops unreferenced testing symbols from production
// binaries, so callers that import platform without invoking this
// helper pay no runtime cost.
func SetDiffCacheForTest(t *testing.T, c diffcache.Cache) {
	t.Helper()
	prev := planDiffCachePtr.Load() // may be nil if Once hasn't fired
	planDiffCachePtr.Store(&c)
	t.Cleanup(func() {
		if prev != nil {
			planDiffCachePtr.Store(prev)
			return
		}
		// No prior value — seed a fresh default so subsequent
		// production code paths in this test binary still observe a
		// working cache. Avoids leaving the atomic at nil, which would
		// hit getDiffCache's defensive noop-fallback.
		fresh := diffcache.New()
		planDiffCachePtr.Store(&fresh)
	})
}
