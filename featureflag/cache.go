package featureflag

import (
	"sync"
	"time"
)

// cacheEntry holds a cached flag value together with its expiration timestamp.
type cacheEntry struct {
	value     FlagValue
	expiresAt time.Time
}

// FlagCache is a thread-safe in-memory TTL cache for evaluated flag values.
// A TTL of zero disables caching (every Get returns a miss).
type FlagCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry // key = flagKey + "|" + userKey
	ttl     time.Duration
	now     func() time.Time // injectable clock for testing
}

// NewFlagCache creates a cache with the given TTL. Pass 0 to disable caching.
func NewFlagCache(ttl time.Duration) *FlagCache {
	return &FlagCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// cacheKey builds a composite lookup key.
func cacheKey(flagKey, userKey string) string {
	return flagKey + "|" + userKey
}

// Get returns the cached value and true if a non-expired entry exists.
func (c *FlagCache) Get(flagKey, userKey string) (FlagValue, bool) {
	if c.ttl == 0 {
		return FlagValue{}, false
	}

	c.mu.RLock()
	entry, ok := c.entries[cacheKey(flagKey, userKey)]
	c.mu.RUnlock()

	if !ok {
		return FlagValue{}, false
	}
	if c.now().After(entry.expiresAt) {
		// Expired — lazily remove on next Set; the caller will fetch fresh.
		return FlagValue{}, false
	}
	return entry.value, true
}

// Set stores a flag value in the cache. No-op when TTL is zero.
func (c *FlagCache) Set(flagKey, userKey string, val FlagValue) {
	if c.ttl == 0 {
		return
	}

	c.mu.Lock()
	c.entries[cacheKey(flagKey, userKey)] = cacheEntry{
		value:     val,
		expiresAt: c.now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Invalidate removes a single entry from the cache.
func (c *FlagCache) Invalidate(flagKey, userKey string) {
	c.mu.Lock()
	delete(c.entries, cacheKey(flagKey, userKey))
	c.mu.Unlock()
}

// InvalidateFlag removes all entries for the given flag key.
func (c *FlagCache) InvalidateFlag(flagKey string) {
	prefix := flagKey + "|"
	c.mu.Lock()
	for k := range c.entries {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// Flush removes all entries.
func (c *FlagCache) Flush() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

// Len returns the number of entries (including expired ones not yet evicted).
func (c *FlagCache) Len() int {
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	return n
}
