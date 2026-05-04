package diffcache

import (
	"slices"
	"sync"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// memoryCache is the in-memory Cache implementation. Entries live for
// the current process; CI workflows in this repo set
// WFCTL_DIFFCACHE=:memory: explicitly so containerized runners never
// write cache data to disk.
//
// Concurrency: a single mutex guards the map. Diff results are small
// and operations are infrequent (one per resource per Plan), so a
// finer-grained scheme isn't justified.
//
// Mutation isolation: DiffResult contains a Changes slice ([]FieldChange)
// whose backing array would otherwise be shared between the cached
// value and the value returned to the caller. Both Put and Get
// deep-copy the Changes slice so a caller mutating the returned
// DiffResult cannot mutate the cached entry, and a caller mutating
// the original Put argument cannot mutate the cached entry either.
// FieldChange itself contains `Old`/`New` of type any — the package
// does not own those values; if a caller stores a pointer or
// mutable map there, the deep-copy stops at the slice level. By
// convention DiffResult.Changes carries scalar Old/New (strings,
// numbers, bools), so this is the right tradeoff between
// correctness and copy cost.
type memoryCache struct {
	mu      sync.Mutex
	entries map[string]interfaces.DiffResult
}

// NewMemory returns an in-memory Cache. The returned Cache does not
// enforce a size cap — process lifetime is the eviction horizon.
// This matches the rev2 lifecycle constraint that the in-memory mode
// is the CI default and is not relied on for correctness.
func NewMemory() Cache {
	return &memoryCache{entries: map[string]interfaces.DiffResult{}}
}

func (c *memoryCache) Get(k Key) (interfaces.DiffResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.entries[keyFingerprint(k)]
	if !ok {
		return interfaces.DiffResult{}, false
	}
	return cloneDiffResult(r), true
}

func (c *memoryCache) Put(k Key, result interfaces.DiffResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[keyFingerprint(k)] = cloneDiffResult(result)
}

// cloneDiffResult returns a deep copy of r at the slice level. The
// scalar fields are value-copied by struct assignment; Changes is
// cloned via slices.Clone so the cached and returned values do not
// share the backing array. See [memoryCache] godoc for the
// Old/New tradeoff (any-typed; not deep-cloned beyond the slice).
func cloneDiffResult(r interfaces.DiffResult) interfaces.DiffResult {
	r.Changes = slices.Clone(r.Changes)
	return r
}
