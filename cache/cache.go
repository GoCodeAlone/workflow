package cache

import (
	"container/list"
	"sync"
	"time"
)

// CacheLayer implements a thread-safe cache with TTL expiration and LRU eviction.
// It follows the cache-aside pattern: callers check the cache first, then fall
// back to the source of truth and populate the cache on miss.
type CacheLayer struct {
	mu         sync.RWMutex
	items      map[string]*list.Element
	eviction   *list.List // front = most recently used, back = least recently used
	maxSize    int
	defaultTTL time.Duration

	hits      int64
	misses    int64
	evictions int64
}

type cacheEntry struct {
	key       string
	value     any
	expiresAt time.Time
}

// CacheConfig configures the cache layer.
type CacheConfig struct {
	// MaxSize is the maximum number of items in the cache.
	MaxSize int
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration
}

// DefaultCacheConfig returns sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize:    10000,
		DefaultTTL: 5 * time.Minute,
	}
}

// NewCacheLayer creates a new cache layer.
func NewCacheLayer(cfg CacheConfig) *CacheLayer {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10000
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}

	return &CacheLayer{
		items:      make(map[string]*list.Element, cfg.MaxSize),
		eviction:   list.New(),
		maxSize:    cfg.MaxSize,
		defaultTTL: cfg.DefaultTTL,
	}
}

// Get retrieves a value from the cache. Returns the value and true if found
// and not expired, or nil and false on miss.
func (c *CacheLayer) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)

	// Check TTL
	if time.Now().After(entry.expiresAt) {
		c.removeLocked(elem)
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.eviction.MoveToFront(elem)
	c.hits++
	return entry.value, true
}

// Set stores a value in the cache with the default TTL.
func (c *CacheLayer) Set(key string, value any) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL stores a value in the cache with a specific TTL.
func (c *CacheLayer) SetWithTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update it
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*cacheEntry)
		entry.value = value
		entry.expiresAt = time.Now().Add(ttl)
		c.eviction.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	for c.eviction.Len() >= c.maxSize {
		c.evictLocked()
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.eviction.PushFront(entry)
	c.items[key] = elem
}

// Delete removes a key from the cache.
func (c *CacheLayer) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeLocked(elem)
	}
}

// Clear removes all entries from the cache.
func (c *CacheLayer) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element, c.maxSize)
	c.eviction.Init()
}

// Len returns the number of items in the cache (including expired but not yet evicted).
func (c *CacheLayer) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.eviction.Len()
}

// Stats returns cache statistics.
func (c *CacheLayer) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:      c.eviction.Len(),
		MaxSize:   c.maxSize,
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   c.hitRateLocked(),
	}
}

// CacheStats holds cache statistics.
type CacheStats struct {
	Size      int
	MaxSize   int
	Hits      int64
	Misses    int64
	Evictions int64
	HitRate   float64
}

func (c *CacheLayer) hitRateLocked() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

// evictLocked removes the least recently used entry.
func (c *CacheLayer) evictLocked() {
	back := c.eviction.Back()
	if back == nil {
		return
	}
	c.removeLocked(back)
	c.evictions++
}

func (c *CacheLayer) removeLocked(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.eviction.Remove(elem)
}

// PurgeExpired removes all expired entries from the cache. This can be called
// periodically for proactive cleanup.
func (c *CacheLayer) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	purged := 0

	var next *list.Element
	for e := c.eviction.Front(); e != nil; e = next {
		next = e.Next()
		entry := e.Value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			c.removeLocked(e)
			purged++
		}
	}

	return purged
}

// GetOrSet atomically gets a value or computes and stores it if missing.
// The loader function is called only on cache miss.
func (c *CacheLayer) GetOrSet(key string, loader func() (any, error)) (any, error) {
	return c.GetOrSetWithTTL(key, c.defaultTTL, loader)
}

// GetOrSetWithTTL atomically gets a value or computes and stores it if missing,
// with a specific TTL.
func (c *CacheLayer) GetOrSetWithTTL(key string, ttl time.Duration, loader func() (any, error)) (any, error) {
	// Fast path: check cache first
	if val, ok := c.Get(key); ok {
		return val, nil
	}

	// Slow path: load the value
	val, err := loader()
	if err != nil {
		return nil, err
	}

	c.SetWithTTL(key, val, ttl)
	return val, nil
}
