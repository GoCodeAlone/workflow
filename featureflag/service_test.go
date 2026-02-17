package featureflag_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/featureflag"
)

// stubProvider is a minimal Provider for testing the Service layer.
type stubProvider struct {
	name  string
	flags map[string]featureflag.FlagValue
	mu    sync.Mutex
	subs  []func(featureflag.FlagChangeEvent)
}

func newStubProvider(name string) *stubProvider {
	return &stubProvider{
		name:  name,
		flags: make(map[string]featureflag.FlagValue),
	}
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Evaluate(_ context.Context, key string, _ featureflag.EvaluationContext) (featureflag.FlagValue, error) {
	v, ok := s.flags[key]
	if !ok {
		return featureflag.FlagValue{}, fmt.Errorf("flag %q not found", key)
	}
	return v, nil
}

func (s *stubProvider) AllFlags(_ context.Context, _ featureflag.EvaluationContext) ([]featureflag.FlagValue, error) {
	vals := make([]featureflag.FlagValue, 0, len(s.flags))
	for _, v := range s.flags {
		vals = append(vals, v)
	}
	return vals, nil
}

func (s *stubProvider) Subscribe(fn func(featureflag.FlagChangeEvent)) func() {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	idx := len(s.subs) - 1
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.subs[idx] = nil
		s.mu.Unlock()
	}
}

func (s *stubProvider) emit(evt featureflag.FlagChangeEvent) {
	s.mu.Lock()
	subs := append([]func(featureflag.FlagChangeEvent){}, s.subs...)
	s.mu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(evt)
		}
	}
}

// ---------- Tests ----------

func TestServiceCacheHit(t *testing.T) {
	provider := newStubProvider("test")
	provider.flags["dark-mode"] = featureflag.FlagValue{
		Key: "dark-mode", Value: true, Type: featureflag.FlagTypeBoolean, Source: "test",
	}

	cache := featureflag.NewFlagCache(30 * time.Second)
	svc := featureflag.NewService(provider, cache, nil)
	ctx := context.Background()
	evalCtx := featureflag.EvaluationContext{UserKey: "user-1"}

	// First call — cache miss, fetches from provider.
	v1, err := svc.Evaluate(ctx, "dark-mode", evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1.Value != true {
		t.Fatalf("expected true, got %v", v1.Value)
	}

	// Change the provider value — the cache should still return the old value.
	provider.flags["dark-mode"] = featureflag.FlagValue{
		Key: "dark-mode", Value: false, Type: featureflag.FlagTypeBoolean, Source: "test",
	}
	v2, err := svc.Evaluate(ctx, "dark-mode", evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.Value != true {
		t.Fatalf("expected cached true, got %v", v2.Value)
	}
}

func TestServiceCacheMiss(t *testing.T) {
	provider := newStubProvider("test")
	provider.flags["beta"] = featureflag.FlagValue{
		Key: "beta", Value: "on", Type: featureflag.FlagTypeString, Source: "test",
	}

	// TTL=0 means caching is disabled.
	cache := featureflag.NewFlagCache(0)
	svc := featureflag.NewService(provider, cache, nil)
	ctx := context.Background()
	evalCtx := featureflag.EvaluationContext{UserKey: "user-1"}

	v1, err := svc.Evaluate(ctx, "beta", evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1.Value != "on" {
		t.Fatalf("expected 'on', got %v", v1.Value)
	}

	// Update provider; since cache is disabled we should see the new value.
	provider.flags["beta"] = featureflag.FlagValue{
		Key: "beta", Value: "off", Type: featureflag.FlagTypeString, Source: "test",
	}
	v2, err := svc.Evaluate(ctx, "beta", evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.Value != "off" {
		t.Fatalf("expected 'off', got %v", v2.Value)
	}
}

func TestServiceCacheInvalidationOnChange(t *testing.T) {
	provider := newStubProvider("test")
	provider.flags["feat"] = featureflag.FlagValue{
		Key: "feat", Value: true, Type: featureflag.FlagTypeBoolean, Source: "test",
	}

	cache := featureflag.NewFlagCache(5 * time.Minute)
	svc := featureflag.NewService(provider, cache, nil)
	ctx := context.Background()
	evalCtx := featureflag.EvaluationContext{UserKey: "u1"}

	// Populate cache.
	_, _ = svc.Evaluate(ctx, "feat", evalCtx)

	// Update provider value and emit change event.
	provider.flags["feat"] = featureflag.FlagValue{
		Key: "feat", Value: false, Type: featureflag.FlagTypeBoolean, Source: "test",
	}
	provider.emit(featureflag.FlagChangeEvent{Key: "feat", Source: "test"})

	// Cache should be invalidated; next call should get fresh value.
	v, err := svc.Evaluate(ctx, "feat", evalCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Value != false {
		t.Fatalf("expected false after invalidation, got %v", v.Value)
	}
}

func TestServiceSSEBroadcast(t *testing.T) {
	provider := newStubProvider("test")
	provider.flags["x"] = featureflag.FlagValue{
		Key: "x", Value: true, Type: featureflag.FlagTypeBoolean, Source: "test",
	}

	cache := featureflag.NewFlagCache(0)
	svc := featureflag.NewService(provider, cache, nil)

	// Start an SSE request.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flags/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	// Run handler in background.
	done := make(chan struct{})
	go func() {
		svc.SSEHandler().ServeHTTP(rec, req)
		close(done)
	}()

	// Give handler time to subscribe.
	time.Sleep(50 * time.Millisecond)

	if svc.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", svc.SubscriberCount())
	}

	// Emit a change event.
	provider.emit(featureflag.FlagChangeEvent{
		Key: "x", Value: false, Type: featureflag.FlagTypeBoolean, Source: "test",
	})

	// Give time for event to be written.
	time.Sleep(50 * time.Millisecond)

	// Cancel the context to close the SSE connection.
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: flag.updated") {
		t.Fatalf("expected SSE event in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"key":"x"`) {
		t.Fatalf("expected flag key in body, got:\n%s", body)
	}
}

func TestServiceAllFlags(t *testing.T) {
	provider := newStubProvider("test")
	provider.flags["a"] = featureflag.FlagValue{Key: "a", Value: true, Type: featureflag.FlagTypeBoolean, Source: "test"}
	provider.flags["b"] = featureflag.FlagValue{Key: "b", Value: "hello", Type: featureflag.FlagTypeString, Source: "test"}

	cache := featureflag.NewFlagCache(0)
	svc := featureflag.NewService(provider, cache, nil)

	flags, err := svc.AllFlags(context.Background(), featureflag.EvaluationContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags))
	}
}
