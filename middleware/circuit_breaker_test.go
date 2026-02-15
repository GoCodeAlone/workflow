package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// errSynthetic is a placeholder error for test failures.
var errSynthetic = errors.New("synthetic failure")

func defaultTestConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		MaxConcurrent:    1,
	}
}

// --- State string ---

func TestCircuitStateString(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown(99)"},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", int(tc.state), got, tc.want)
		}
	}
}

// --- Closed state ---

func TestCircuitBreakerClosedState(t *testing.T) {
	cb := NewCircuitBreaker(defaultTestConfig())

	// Successful calls keep the breaker closed.
	for i := 0; i < 10; i++ {
		err := cb.Execute(context.Background(), func(_ context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected state Closed, got %s", cb.State())
	}

	failures, _ := cb.Counts()
	if failures != 0 {
		t.Errorf("expected 0 failures, got %d", failures)
	}
}

// --- Opens on failures ---

func TestCircuitBreakerOpens(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 3
	cb := NewCircuitBreaker(cfg)

	var transitions []struct{ from, to CircuitState }
	cb.OnStateChange(func(from, to CircuitState) {
		transitions = append(transitions, struct{ from, to CircuitState }{from, to})
	})

	// Record exactly threshold failures.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			return errSynthetic
		})
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected state Open after %d failures, got %s", cfg.FailureThreshold, cb.State())
	}

	// Verify state change callback was invoked.
	if len(transitions) != 1 || transitions[0].from != CircuitClosed || transitions[0].to != CircuitOpen {
		t.Errorf("expected transition Closed->Open, got %+v", transitions)
	}
}

// --- Failures below threshold keep circuit closed ---

func TestCircuitBreakerBelowThreshold(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 5
	cb := NewCircuitBreaker(cfg)

	// Record fewer failures than threshold.
	for i := 0; i < cfg.FailureThreshold-1; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			return errSynthetic
		})
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed with %d failures (threshold %d), got %s",
			cfg.FailureThreshold-1, cfg.FailureThreshold, cb.State())
	}
}

// --- Success resets failure counter in closed state ---

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 3
	cb := NewCircuitBreaker(cfg)

	// Two failures, then a success, then two more failures.
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}
	_ = cb.Execute(context.Background(), func(_ context.Context) error { return nil })
	for i := 0; i < 2; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}

	// Should still be closed because the success reset the counter.
	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed (success should reset failure count), got %s", cb.State())
	}
}

// --- Half-open after timeout ---

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker(cfg)

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Wait for the timeout to elapse.
	time.Sleep(cfg.Timeout + 10*time.Millisecond)

	// State() should now report half-open.
	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected HalfOpen after timeout, got %s", cb.State())
	}
}

// --- Half-open transitions to closed on enough successes ---

func TestCircuitBreakerCloses(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SuccessThreshold = 2
	cb := NewCircuitBreaker(cfg)

	// Use a controllable clock.
	currentTime := time.Now()
	cb.now = func() time.Time { return currentTime }

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Advance past timeout.
	currentTime = currentTime.Add(cfg.Timeout + time.Millisecond)

	// Execute enough successes to close the circuit.
	for i := 0; i < cfg.SuccessThreshold; i++ {
		err := cb.Execute(context.Background(), func(_ context.Context) error { return nil })
		if err != nil {
			t.Fatalf("half-open success %d: unexpected error: %v", i, err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed after %d successes in half-open, got %s", cfg.SuccessThreshold, cb.State())
	}
}

// --- Rejects when open ---

func TestCircuitBreakerRejectsWhenOpen(t *testing.T) {
	cfg := defaultTestConfig()
	cb := NewCircuitBreaker(cfg)

	// Use a controllable clock so the timeout never elapses.
	frozenTime := time.Now()
	cb.now = func() time.Time { return frozenTime }

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}

	// The next call should be rejected.
	err := cb.Execute(context.Background(), func(_ context.Context) error {
		t.Error("function should not be called when circuit is open")
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

// --- Half-open failure re-opens circuit ---

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SuccessThreshold = 3
	cb := NewCircuitBreaker(cfg)

	currentTime := time.Now()
	cb.now = func() time.Time { return currentTime }

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}

	// Advance past timeout.
	currentTime = currentTime.Add(cfg.Timeout + time.Millisecond)

	// One failure in half-open should re-open.
	_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })

	if cb.State() != CircuitOpen {
		t.Errorf("expected Open after failure in half-open, got %s", cb.State())
	}
}

// --- MaxConcurrent limits half-open probes ---

func TestCircuitBreakerHalfOpenMaxConcurrent(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.MaxConcurrent = 1
	cb := NewCircuitBreaker(cfg)

	currentTime := time.Now()
	cb.now = func() time.Time { return currentTime }

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}

	// Advance past timeout.
	currentTime = currentTime.Add(cfg.Timeout + time.Millisecond)

	// Use a channel to hold the first half-open request in flight.
	started := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = cb.Execute(context.Background(), func(_ context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started // first request is in flight

	// Second request should be rejected.
	err := cb.Execute(context.Background(), func(_ context.Context) error {
		t.Error("second request should not execute in half-open with max_concurrent=1")
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen for excess half-open request, got %v", err)
	}

	close(release)
	wg.Wait()
}

// --- Reset ---

func TestCircuitBreakerReset(t *testing.T) {
	cfg := defaultTestConfig()
	cb := NewCircuitBreaker(cfg)

	// Trip the breaker.
	frozenTime := time.Now()
	cb.now = func() time.Time { return frozenTime }
	for i := 0; i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	var gotTransition bool
	cb.OnStateChange(func(from, to CircuitState) {
		if from == CircuitOpen && to == CircuitClosed {
			gotTransition = true
		}
	})

	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed after Reset, got %s", cb.State())
	}
	if !gotTransition {
		t.Error("expected state change callback on Reset")
	}

	failures, successes := cb.Counts()
	if failures != 0 || successes != 0 {
		t.Errorf("expected zeroed counters, got failures=%d successes=%d", failures, successes)
	}
}

// --- Default config values ---

func TestCircuitBreakerDefaults(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{Name: "defaults"})

	if cb.config.FailureThreshold != 5 {
		t.Errorf("default FailureThreshold: got %d, want 5", cb.config.FailureThreshold)
	}
	if cb.config.SuccessThreshold != 2 {
		t.Errorf("default SuccessThreshold: got %d, want 2", cb.config.SuccessThreshold)
	}
	if cb.config.Timeout != 30*time.Second {
		t.Errorf("default Timeout: got %v, want 30s", cb.config.Timeout)
	}
	if cb.config.MaxConcurrent != 1 {
		t.Errorf("default MaxConcurrent: got %d, want 1", cb.config.MaxConcurrent)
	}
}

// --- Concurrency ---

func TestCircuitBreakerConcurrency(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 100
	cfg.Timeout = 10 * time.Millisecond
	cb := NewCircuitBreaker(cfg)

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if id%2 == 0 {
					_ = cb.Execute(context.Background(), func(_ context.Context) error {
						return nil
					})
				} else {
					_ = cb.Execute(context.Background(), func(_ context.Context) error {
						return errSynthetic
					})
				}
				// Occasional state reads.
				_ = cb.State()
				_, _ = cb.Counts()
			}
		}(g)
	}

	wg.Wait()

	// If we got here without the race detector complaining, the test passes.
	// Just verify the breaker is in a valid state.
	state := cb.State()
	if state != CircuitClosed && state != CircuitOpen && state != CircuitHalfOpen {
		t.Errorf("unexpected state after concurrent access: %s", state)
	}
}

// --- Registry ---

func TestCircuitBreakerRegistry(t *testing.T) {
	reg := NewCircuitBreakerRegistry()

	// Get from empty registry returns nil.
	if got := reg.Get("unknown"); got != nil {
		t.Error("expected nil for unknown breaker")
	}

	// GetOrCreate creates a new breaker.
	cfg1 := CircuitBreakerConfig{Name: "svc-a", FailureThreshold: 3}
	cb1 := reg.GetOrCreate(cfg1)
	if cb1 == nil {
		t.Fatal("expected non-nil breaker from GetOrCreate")
	}

	// GetOrCreate returns the same instance.
	cb1Again := reg.GetOrCreate(cfg1)
	if cb1 != cb1Again {
		t.Error("expected same instance on second GetOrCreate call")
	}

	// Register a second breaker.
	cfg2 := CircuitBreakerConfig{Name: "svc-b", FailureThreshold: 5}
	cb2 := NewCircuitBreaker(cfg2)
	reg.Register(cb2)

	// Get retrieves registered breaker.
	if got := reg.Get("svc-b"); got != cb2 {
		t.Error("expected to retrieve svc-b from registry")
	}

	// All returns both.
	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2 breakers, got %d", len(all))
	}

	// Remove.
	reg.Remove("svc-a")
	if got := reg.Get("svc-a"); got != nil {
		t.Error("expected nil after Remove")
	}

	// ResetAll.
	frozenTime := time.Now()
	cb2.now = func() time.Time { return frozenTime }
	for i := 0; i < 5; i++ {
		_ = cb2.Execute(context.Background(), func(_ context.Context) error { return errSynthetic })
	}
	if cb2.State() != CircuitOpen {
		t.Fatalf("expected svc-b Open, got %s", cb2.State())
	}

	reg.ResetAll()
	if cb2.State() != CircuitClosed {
		t.Errorf("expected svc-b Closed after ResetAll, got %s", cb2.State())
	}
}

// --- Registry concurrency ---

func TestCircuitBreakerRegistryConcurrency(t *testing.T) {
	reg := NewCircuitBreakerRegistry()

	var wg sync.WaitGroup
	const goroutines = 30

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := "breaker"
			cfg := CircuitBreakerConfig{Name: name, FailureThreshold: 3}
			cb := reg.GetOrCreate(cfg)
			_ = cb.State()
			_ = reg.Get(name)
			_ = reg.All()
		}(i)
	}
	wg.Wait()
}

// --- HTTP Middleware ---

func TestHTTPMiddleware(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 2
	cb := NewCircuitBreaker(cfg)

	// Use a controllable clock.
	frozenTime := time.Now()
	cb.now = func() time.Time { return frozenTime }

	// Downstream handler that can be toggled between success and failure.
	var downstreamStatus atomic.Int32
	downstreamStatus.Store(http.StatusOK)
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := int(downstreamStatus.Load())
		w.WriteHeader(status)
	})

	// Fallback handler.
	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("fallback"))
	})

	handler := CircuitBreakerMiddleware(cb, fallback)(downstream)

	// Successful requests pass through.
	t.Run("success_passes_through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	// Downstream failures trip the breaker.
	t.Run("downstream_failures_trip_breaker", func(t *testing.T) {
		downstreamStatus.Store(http.StatusInternalServerError)

		for i := 0; i < cfg.FailureThreshold; i++ {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			// The downstream 500 is forwarded to the client.
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("failure %d: expected 500, got %d", i, rec.Code)
			}
		}

		if cb.State() != CircuitOpen {
			t.Fatalf("expected Open after downstream failures, got %s", cb.State())
		}
	})

	// Open circuit serves fallback.
	t.Run("open_serves_fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 from fallback, got %d", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "30" {
			t.Errorf("expected Retry-After=30, got %q", got)
		}
	})
}

// --- HTTP Middleware with default fallback ---

func TestHTTPMiddlewareDefaultFallback(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 1
	cfg.Timeout = 5 * time.Second
	cb := NewCircuitBreaker(cfg)

	frozenTime := time.Now()
	cb.now = func() time.Time { return frozenTime }

	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	// Pass nil fallback to use the default.
	handler := CircuitBreakerMiddleware(cb, nil)(downstream)

	// Trip the breaker.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Now the circuit is open; next request should get the default fallback.
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 from default fallback, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("expected Retry-After=5, got %q", got)
	}
}

// --- HTTP Middleware success after recovery ---

func TestHTTPMiddlewareRecovery(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.FailureThreshold = 1
	cfg.SuccessThreshold = 1
	cfg.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker(cfg)

	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	handler := CircuitBreakerMiddleware(cb, fallback)(downstream)

	// Trip the breaker.
	failingDownstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	failHandler := CircuitBreakerMiddleware(cb, fallback)(failingDownstream)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	failHandler.ServeHTTP(rec, req)

	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Wait for timeout.
	time.Sleep(cfg.Timeout + 20*time.Millisecond)

	// Half-open probe should succeed and close the circuit.
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after recovery, got %d", rec.Code)
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed after successful probe, got %s", cb.State())
	}
}
