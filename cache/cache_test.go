package cache

import (
	"fmt"
	"testing"
	"time"
)

func TestCacheLayerSetGet(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: time.Minute,
	})

	c.Set("key1", "value1")

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}
}

func TestCacheLayerMiss(t *testing.T) {
	c := NewCacheLayer(DefaultCacheConfig())

	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCacheLayerTTLExpiration(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: 50 * time.Millisecond,
	})

	c.Set("key1", "value1")

	// Should be found immediately
	_, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("key1")
	if ok {
		t.Error("expected cache miss after TTL expiration")
	}
}

func TestCacheLayerCustomTTL(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: time.Hour,
	})

	c.SetWithTTL("key1", "value1", 50*time.Millisecond)

	_, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("key1")
	if ok {
		t.Error("expected cache miss after custom TTL expiration")
	}
}

func TestCacheLayerLRUEviction(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    3,
		DefaultTTL: time.Minute,
	})

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	// Access "a" to make it recently used
	c.Get("a")

	// Add "d" - should evict "b" (least recently used)
	c.Set("d", 4)

	_, ok := c.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted")
	}

	// "a" should still be present
	_, ok = c.Get("a")
	if !ok {
		t.Error("expected 'a' to be present")
	}

	// "c" and "d" should be present
	_, ok = c.Get("c")
	if !ok {
		t.Error("expected 'c' to be present")
	}
	_, ok = c.Get("d")
	if !ok {
		t.Error("expected 'd' to be present")
	}
}

func TestCacheLayerUpdateExisting(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: time.Minute,
	})

	c.Set("key1", "old")
	c.Set("key1", "new")

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if val != "new" {
		t.Errorf("expected 'new', got %v", val)
	}

	if c.Len() != 1 {
		t.Errorf("expected length 1, got %d", c.Len())
	}
}

func TestCacheLayerDelete(t *testing.T) {
	c := NewCacheLayer(DefaultCacheConfig())

	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected cache miss after delete")
	}

	// Delete nonexistent key should not panic
	c.Delete("nonexistent")
}

func TestCacheLayerClear(t *testing.T) {
	c := NewCacheLayer(DefaultCacheConfig())

	c.Set("a", 1)
	c.Set("b", 2)

	c.Clear()

	if c.Len() != 0 {
		t.Errorf("expected length 0 after clear, got %d", c.Len())
	}

	_, ok := c.Get("a")
	if ok {
		t.Error("expected miss after clear")
	}
}

func TestCacheLayerStats(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    5,
		DefaultTTL: time.Minute,
	})

	c.Set("a", 1)
	c.Set("b", 2)

	c.Get("a")       // hit
	c.Get("missing") // miss

	stats := c.Stats()
	if stats.Size != 2 {
		t.Errorf("expected size 2, got %d", stats.Size)
	}
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("expected 50%% hit rate, got %.2f", stats.HitRate)
	}
	if stats.MaxSize != 5 {
		t.Errorf("expected max size 5, got %d", stats.MaxSize)
	}
}

func TestCacheLayerStatsEmpty(t *testing.T) {
	c := NewCacheLayer(DefaultCacheConfig())
	stats := c.Stats()
	if stats.HitRate != 0 {
		t.Errorf("expected 0 hit rate for empty cache, got %f", stats.HitRate)
	}
}

func TestCacheLayerPurgeExpired(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: 50 * time.Millisecond,
	})

	c.Set("a", 1)
	c.Set("b", 2)
	c.SetWithTTL("c", 3, time.Hour) // this one won't expire

	time.Sleep(100 * time.Millisecond)

	purged := c.PurgeExpired()
	if purged != 2 {
		t.Errorf("expected 2 purged, got %d", purged)
	}

	if c.Len() != 1 {
		t.Errorf("expected 1 remaining, got %d", c.Len())
	}
}

func TestCacheLayerGetOrSet(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: time.Minute,
	})

	callCount := 0
	loader := func() (any, error) {
		callCount++
		return "loaded-value", nil
	}

	// First call should invoke loader
	val, err := c.GetOrSet("key1", loader)
	if err != nil {
		t.Fatalf("GetOrSet failed: %v", err)
	}
	if val != "loaded-value" {
		t.Errorf("expected loaded-value, got %v", val)
	}
	if callCount != 1 {
		t.Errorf("expected 1 loader call, got %d", callCount)
	}

	// Second call should use cache
	val, err = c.GetOrSet("key1", loader)
	if err != nil {
		t.Fatalf("GetOrSet failed: %v", err)
	}
	if val != "loaded-value" {
		t.Errorf("expected loaded-value, got %v", val)
	}
	if callCount != 1 {
		t.Errorf("expected loader not called again, got %d calls", callCount)
	}
}

func TestCacheLayerGetOrSetError(t *testing.T) {
	c := NewCacheLayer(DefaultCacheConfig())

	loader := func() (any, error) {
		return nil, fmt.Errorf("load error")
	}

	_, err := c.GetOrSet("key1", loader)
	if err == nil {
		t.Error("expected error from loader")
	}

	// Key should not be cached
	_, ok := c.Get("key1")
	if ok {
		t.Error("expected miss after loader error")
	}
}

func TestCacheLayerGetOrSetWithTTL(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    100,
		DefaultTTL: time.Hour,
	})

	val, err := c.GetOrSetWithTTL("key1", 50*time.Millisecond, func() (any, error) {
		return "short-lived", nil
	})
	if err != nil {
		t.Fatalf("GetOrSetWithTTL failed: %v", err)
	}
	if val != "short-lived" {
		t.Errorf("expected short-lived, got %v", val)
	}

	time.Sleep(100 * time.Millisecond)

	_, ok := c.Get("key1")
	if ok {
		t.Error("expected miss after custom TTL expiration")
	}
}

func TestDefaultCacheConfig(t *testing.T) {
	cfg := DefaultCacheConfig()
	if cfg.MaxSize <= 0 {
		t.Error("MaxSize should be positive")
	}
	if cfg.DefaultTTL <= 0 {
		t.Error("DefaultTTL should be positive")
	}
}

func TestCacheLayerEvictionStats(t *testing.T) {
	c := NewCacheLayer(CacheConfig{
		MaxSize:    2,
		DefaultTTL: time.Minute,
	})

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // evicts one

	stats := c.Stats()
	if stats.Evictions != 1 {
		t.Errorf("expected 1 eviction, got %d", stats.Evictions)
	}
}
