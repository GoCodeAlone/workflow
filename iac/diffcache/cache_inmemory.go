package diffcache

import (
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
	return r, true
}

func (c *memoryCache) Put(k Key, result interfaces.DiffResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[keyFingerprint(k)] = result
}
