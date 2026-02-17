package module

import (
	"context"
	"testing"
)

func TestRateLimitStepFactory_Defaults(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "rl" {
		t.Errorf("expected name %q, got %q", "rl", step.Name())
	}
}

func TestRateLimitStepFactory_CustomConfig(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{
		"requests_per_minute": 120,
		"burst_size":          20,
		"key_from":            "{{ .client_ip }}",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rls := step.(*RateLimitStep)
	if rls.requestsPerMinute != 120 {
		t.Errorf("expected rpm 120, got %d", rls.requestsPerMinute)
	}
	if rls.burstSize != 20 {
		t.Errorf("expected burst 20, got %d", rls.burstSize)
	}
	if rls.keyFrom != "{{ .client_ip }}" {
		t.Errorf("expected keyFrom %q, got %q", "{{ .client_ip }}", rls.keyFrom)
	}
}

func TestRateLimitStepFactory_FloatConfig(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{
		"requests_per_minute": float64(30),
		"burst_size":          float64(5),
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rls := step.(*RateLimitStep)
	if rls.requestsPerMinute != 30 {
		t.Errorf("expected rpm 30, got %d", rls.requestsPerMinute)
	}
	if rls.burstSize != 5 {
		t.Errorf("expected burst 5, got %d", rls.burstSize)
	}
}

func TestRateLimitStepFactory_InvalidRPM(t *testing.T) {
	factory := NewRateLimitStepFactory()
	_, err := factory("rl", map[string]any{
		"requests_per_minute": -1,
	}, nil)
	if err == nil {
		t.Fatal("expected error for negative rpm")
	}
}

func TestRateLimitStepFactory_InvalidBurst(t *testing.T) {
	factory := NewRateLimitStepFactory()
	_, err := factory("rl", map[string]any{
		"burst_size": -1,
	}, nil)
	if err == nil {
		t.Fatal("expected error for negative burst_size")
	}
}

func TestRateLimitStep_AllowsWithinLimit(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{
		"requests_per_minute": 60,
		"burst_size":          5,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	ctx := context.Background()

	// Should allow up to burst_size requests
	for i := 0; i < 5; i++ {
		result, execErr := step.Execute(ctx, pc)
		if execErr != nil {
			t.Fatalf("request %d should be allowed: %v", i+1, execErr)
		}
		rl := result.Output["rate_limit"].(map[string]any)
		if !rl["allowed"].(bool) {
			t.Fatalf("request %d: expected allowed=true", i+1)
		}
	}
}

func TestRateLimitStep_RejectsOverLimit(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{
		"requests_per_minute": 60,
		"burst_size":          2,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	ctx := context.Background()

	// Exhaust burst
	for i := 0; i < 2; i++ {
		_, _ = step.Execute(ctx, pc)
	}

	// Next should be rejected
	_, execErr := step.Execute(ctx, pc)
	if execErr == nil {
		t.Fatal("expected rate limit error")
	}
}

func TestRateLimitStep_PerKeyLimiting(t *testing.T) {
	factory := NewRateLimitStepFactory()
	step, err := factory("rl", map[string]any{
		"requests_per_minute": 60,
		"burst_size":          1,
		"key_from":            "{{ .client_id }}",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// Client A exhausts its bucket
	pcA := NewPipelineContext(map[string]any{"client_id": "client-a"}, nil)
	_, err = step.Execute(ctx, pcA)
	if err != nil {
		t.Fatalf("client-a first request should be allowed: %v", err)
	}
	_, err = step.Execute(ctx, pcA)
	if err == nil {
		t.Fatal("client-a second request should be rejected")
	}

	// Client B should still be allowed (separate bucket)
	pcB := NewPipelineContext(map[string]any{"client_id": "client-b"}, nil)
	_, err = step.Execute(ctx, pcB)
	if err != nil {
		t.Fatalf("client-b first request should be allowed: %v", err)
	}
}

func TestTokenBucket_AllowAndRefill(t *testing.T) {
	bucket := newTokenBucket(2, 100) // 100 tokens/sec refill

	if !bucket.allow() {
		t.Fatal("first request should be allowed")
	}
	if !bucket.allow() {
		t.Fatal("second request should be allowed")
	}
	if bucket.allow() {
		t.Fatal("third request should be rejected (bucket empty)")
	}
}
