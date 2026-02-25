package module

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRedisCache creates a RedisCache backed by a miniredis server.
func newTestRedisCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	cfg := RedisCacheConfig{
		Address:    mr.Addr(),
		Prefix:     "test:",
		DefaultTTL: time.Hour,
	}
	cache := NewRedisCacheWithClient("cache", cfg, client)
	return cache, mr
}

func TestRedisCacheGetSetDelete(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestRedisCache(t)

	// Set a value
	if err := cache.Set(ctx, "mykey", "myvalue", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get it back
	val, err := cache.Get(ctx, "mykey")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "myvalue" {
		t.Errorf("expected %q, got %q", "myvalue", val)
	}

	// Delete it
	if err := cache.Delete(ctx, "mykey"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Get after delete should return redis.Nil
	_, err = cache.Get(ctx, "mykey")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestRedisCacheKeyPrefix(t *testing.T) {
	ctx := context.Background()
	cache, mr := newTestRedisCache(t)

	if err := cache.Set(ctx, "hello", "world", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify prefix is stored in miniredis
	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if k == "test:hello" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected key %q in redis, got keys: %v", "test:hello", keys)
	}
}

func TestRedisCacheDefaultTTL(t *testing.T) {
	ctx := context.Background()
	cache, mr := newTestRedisCache(t)

	// Set with TTL=0 should use DefaultTTL (1 hour)
	if err := cache.Set(ctx, "ttlkey", "ttlval", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	ttl := mr.TTL("test:ttlkey")
	if ttl <= 0 {
		t.Errorf("expected positive TTL, got %v", ttl)
	}
}

func TestRedisCacheExplicitTTL(t *testing.T) {
	ctx := context.Background()
	cache, mr := newTestRedisCache(t)

	// Set with explicit TTL=30m
	if err := cache.Set(ctx, "short", "val", 30*time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	ttl := mr.TTL("test:short")
	// miniredis reports TTL in seconds-level precision; just verify it's set
	if ttl <= 0 {
		t.Errorf("expected positive TTL, got %v", ttl)
	}
	if ttl > time.Hour {
		t.Errorf("expected TTL <= 1h, got %v", ttl)
	}
}

func TestRedisCacheMiss(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestRedisCache(t)

	_, err := cache.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestRedisCacheNotStarted(t *testing.T) {
	ctx := context.Background()
	cfg := RedisCacheConfig{Address: "localhost:6379", Prefix: "wf:"}
	cache := NewRedisCache("cache", cfg)

	if _, err := cache.Get(ctx, "k"); err == nil {
		t.Error("expected error from Get when not started")
	}
	if err := cache.Set(ctx, "k", "v", 0); err == nil {
		t.Error("expected error from Set when not started")
	}
	if err := cache.Delete(ctx, "k"); err == nil {
		t.Error("expected error from Delete when not started")
	}
}

func TestRedisCacheInit(t *testing.T) {
	cfg := RedisCacheConfig{Address: "localhost:6379", Prefix: "wf:"}
	cache := NewRedisCache("cache", cfg)
	app := NewMockApplication()

	if err := cache.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestRedisCacheStop(t *testing.T) {
	ctx := context.Background()
	cache, _ := newTestRedisCache(t)

	if err := cache.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	// Stop when already nil is a no-op
	cache2 := NewRedisCache("cache2", RedisCacheConfig{})
	if err := cache2.Stop(ctx); err != nil {
		t.Fatalf("Stop on uninitialised cache failed: %v", err)
	}
}

func TestRedisCacheProvidesServices(t *testing.T) {
	cache := NewRedisCache("mycache", RedisCacheConfig{})
	svcs := cache.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "mycache" {
		t.Errorf("expected service name %q, got %q", "mycache", svcs[0].Name)
	}
}
