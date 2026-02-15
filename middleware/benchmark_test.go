package middleware

import (
	"context"
	"testing"
	"time"
)

// BenchmarkCircuitBreakerDetection measures time to detect failure and open the circuit.
// Target: <1s detection (from PLATFORM_ROADMAP.md Phase 3).
func BenchmarkCircuitBreakerDetection(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cfg := CircuitBreakerConfig{
			Name:             "bench",
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Timeout:          100 * time.Millisecond,
			MaxConcurrent:    1,
		}
		cb := NewCircuitBreaker(cfg)

		// Trip the breaker
		for j := 0; j < cfg.FailureThreshold; j++ {
			_ = cb.Execute(context.Background(), func(_ context.Context) error {
				return errSynthetic
			})
		}

		if cb.State() != CircuitOpen {
			b.Fatalf("expected Open, got %s", cb.State())
		}
	}
}

// BenchmarkCircuitBreakerExecution measures overhead of circuit breaker wrapper on success path.
func BenchmarkCircuitBreakerExecution_Success(b *testing.B) {
	cfg := CircuitBreakerConfig{
		Name:             "bench-success",
		FailureThreshold: 100,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxConcurrent:    1,
	}
	cb := NewCircuitBreaker(cfg)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			return nil
		})
	}
}

func BenchmarkCircuitBreakerExecution_Failure(b *testing.B) {
	cfg := CircuitBreakerConfig{
		Name:             "bench-fail",
		FailureThreshold: 1000000, // very high so we stay closed
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxConcurrent:    1,
	}
	cb := NewCircuitBreaker(cfg)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			return errSynthetic
		})
	}
}

// TestCircuitBreakerDetectionTime measures the wall-clock time to detect failure.
// This is a timed test, not a benchmark, to validate the <1s target.
func TestCircuitBreakerDetectionTime(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Name:             "detection-time",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		MaxConcurrent:    1,
	}
	cb := NewCircuitBreaker(cfg)

	start := time.Now()

	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			return errSynthetic
		})
	}

	elapsed := time.Since(start)

	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Target: <1s detection
	if elapsed > time.Second {
		t.Errorf("circuit breaker detection took %v, target is <1s", elapsed)
	} else {
		t.Logf("PASS: Circuit breaker detection time: %v (target: <1s)", elapsed)
	}
}
