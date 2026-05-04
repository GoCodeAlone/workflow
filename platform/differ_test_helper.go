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
// Concurrency contract (round-4 review note): Two tests that BOTH call
// this helper concurrently (e.g., via t.Parallel) WILL race on the
// snapshot/restore pair — the second caller may snapshot the first's
// cache pointer and restore it on cleanup, masking the first's choice.
// Callers MUST NOT invoke this helper from a t.Parallel test. Nested
// calls in the same goroutine (e.g., an outer test that calls the
// helper, then runs subtests that also call it) are supported: each
// nested call snapshots the current pointer and restores it on its own
// t.Cleanup, which fires inside-out (Go test framework guarantee), so
// the outer test still sees its own cache when the nested cleanup
// completes. The conformance suite + platform's own _test.go files
// rely on this nested-but-sequential pattern (see
// scenarios_test.go:TestRun_ConsecutiveRunsObserveLiveDriverIndependently).
//
// Cleanup contract: t.Cleanup restores the prior pointer if there was
// one, or seeds a noop cache when no prior value existed (round-4
// review fix: previously seeded diffcache.New() which falls back to
// ~/.cache/wfctl/diff filesystem unless WFCTL_DIFFCACHE is set,
// leaking test state across runs and across users on shared CI
// runners). diffcache.NewNoop() is the safe default — every Get
// misses, so subsequent tests in the same binary do not observe stale
// cache entries from this test (a NewMemory fallback would persist
// in-memory, propagating Put results into later tests with matching
// cache keys). Tests that need a working cache install one explicitly
// via SetDiffCacheForTest. Marked t.Helper() so Cleanup-time failures
// attribute to the caller's line.
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
		// No prior value — seed a noop cache (every Get misses, no
		// filesystem state, no in-memory state shared across tests).
		// Avoids leaving the atomic at nil (would hit getDiffCache's
		// defensive noop-fallback anyway, but explicit-store is
		// clearer). Earlier candidates were rejected:
		//   - diffcache.New() falls back to ~/.cache/wfctl/diff when
		//     WFCTL_DIFFCACHE is unset (filesystem leak across test
		//     runs and across users on shared CI runners).
		//   - diffcache.NewMemory() persists Puts in-process for the
		//     remainder of the test binary, polluting later tests
		//     whose cache keys collide (observed: differ_replace_test
		//     siblings using the same desired+current shapes).
		// NewNoop() is the only hermetic choice for the
		// no-prior-cache cleanup case.
		fresh := diffcache.NewNoop()
		planDiffCachePtr.Store(&fresh)
	})
}
