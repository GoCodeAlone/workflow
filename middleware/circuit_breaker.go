package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed is the normal operating state. Requests pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen indicates the circuit has tripped. Requests are rejected.
	CircuitOpen
	// CircuitHalfOpen indicates the circuit is testing whether the downstream
	// service has recovered.
	CircuitHalfOpen
)

// String returns a human-readable representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ErrCircuitOpen is returned when the circuit breaker is in the open state
// and refuses to execute the request.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerConfig holds the parameters for a circuit breaker.
type CircuitBreakerConfig struct {
	// Name identifies this circuit breaker instance.
	Name string `json:"name"`

	// FailureThreshold is the number of consecutive failures required to
	// trip the circuit from closed to open. Defaults to 5.
	FailureThreshold int `json:"failure_threshold"`

	// SuccessThreshold is the number of consecutive successes in the
	// half-open state required to close the circuit. Defaults to 2.
	SuccessThreshold int `json:"success_threshold"`

	// Timeout is the duration the circuit stays open before transitioning
	// to half-open to test recovery. Defaults to 30 seconds.
	Timeout time.Duration `json:"timeout"`

	// MaxConcurrent is the maximum number of concurrent probe requests
	// allowed in the half-open state. Defaults to 1.
	MaxConcurrent int `json:"max_concurrent"`
}

// withDefaults returns a copy of the config with zero-value fields replaced
// by sensible defaults.
func (c CircuitBreakerConfig) withDefaults() CircuitBreakerConfig {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = 2
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 1
	}
	return c
}

// CircuitBreaker implements the circuit breaker pattern for protecting calls
// to downstream services.
type CircuitBreaker struct {
	config          CircuitBreakerConfig
	state           CircuitState
	failures        int
	successes       int
	lastFailureTime time.Time
	halfOpenCount   int
	mu              sync.RWMutex
	onStateChange   func(from, to CircuitState)
	// now is a function that returns the current time, injectable for testing.
	now func() time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	config = config.withDefaults()
	return &CircuitBreaker{
		config: config,
		state:  CircuitClosed,
		now:    time.Now,
	}
}

// OnStateChange registers a callback invoked whenever the circuit transitions
// between states. The callback is called while the breaker's lock is held,
// so it must not call back into the breaker.
func (cb *CircuitBreaker) OnStateChange(fn func(from, to CircuitState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

// Execute runs fn through the circuit breaker. If the circuit is open, it
// returns ErrCircuitOpen without invoking fn. In the half-open state it
// limits concurrency to MaxConcurrent. Success and failure are recorded
// automatically based on the error returned by fn.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := cb.allowRequest(); err != nil {
		return err
	}

	err := fn(ctx)
	if err != nil {
		cb.RecordFailure()
	} else {
		cb.RecordSuccess()
	}
	return err
}

// allowRequest checks whether a request should be allowed through.
func (cb *CircuitBreaker) allowRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil

	case CircuitOpen:
		// Check if the timeout has elapsed; if so, transition to half-open.
		if cb.now().Sub(cb.lastFailureTime) >= cb.config.Timeout {
			cb.transitionTo(CircuitHalfOpen)
			cb.halfOpenCount++
			return nil
		}
		return ErrCircuitOpen

	case CircuitHalfOpen:
		if cb.halfOpenCount >= cb.config.MaxConcurrent {
			return ErrCircuitOpen
		}
		cb.halfOpenCount++
		return nil

	default:
		return ErrCircuitOpen
	}
}

// State returns the current state of the circuit breaker. If the circuit is
// open and the timeout has elapsed, this will report HalfOpen (but does not
// transition -- that occurs on the next Execute call).
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == CircuitOpen && cb.now().Sub(cb.lastFailureTime) >= cb.config.Timeout {
		return CircuitHalfOpen
	}
	return cb.state
}

// Reset manually resets the circuit breaker to the closed state, clearing
// all counters.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	old := cb.state
	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenCount = 0

	if old != CircuitClosed && cb.onStateChange != nil {
		cb.onStateChange(old, CircuitClosed)
	}
}

// RecordSuccess records a successful operation.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failures = 0 // reset consecutive failure count

	case CircuitHalfOpen:
		cb.successes++
		cb.halfOpenCount--
		if cb.halfOpenCount < 0 {
			cb.halfOpenCount = 0
		}
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(CircuitClosed)
		}

	case CircuitOpen:
		// Should not happen in normal flow, but handle gracefully.
	}
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.lastFailureTime = cb.now()
			cb.transitionTo(CircuitOpen)
		}

	case CircuitHalfOpen:
		cb.halfOpenCount--
		if cb.halfOpenCount < 0 {
			cb.halfOpenCount = 0
		}
		cb.lastFailureTime = cb.now()
		cb.transitionTo(CircuitOpen)

	case CircuitOpen:
		cb.lastFailureTime = cb.now()
	}
}

// transitionTo changes the circuit state and fires the callback. Caller must
// hold cb.mu.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	old := cb.state
	if old == newState {
		return
	}
	cb.state = newState
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenCount = 0

	if cb.onStateChange != nil {
		cb.onStateChange(old, newState)
	}
}

// Counts returns the current failure and success counters (useful for
// diagnostics and testing).
func (cb *CircuitBreaker) Counts() (failures, successes int) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures, cb.successes
}

// --- CircuitBreakerRegistry ---

// CircuitBreakerRegistry manages a collection of named circuit breakers.
type CircuitBreakerRegistry struct {
	breakers map[string]*CircuitBreaker
	mu       sync.RWMutex
}

// NewCircuitBreakerRegistry creates a new empty registry.
func NewCircuitBreakerRegistry() *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Get returns the circuit breaker with the given name, or nil if not found.
func (r *CircuitBreakerRegistry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.breakers[name]
}

// GetOrCreate returns the circuit breaker for name if it exists, otherwise
// creates one with the provided config, registers it, and returns it.
func (r *CircuitBreakerRegistry) GetOrCreate(config CircuitBreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok := r.breakers[config.Name]; ok {
		return cb
	}
	cb := NewCircuitBreaker(config)
	r.breakers[config.Name] = cb
	return cb
}

// Register adds a circuit breaker to the registry under its config name.
// If a breaker with the same name already exists it is replaced.
func (r *CircuitBreakerRegistry) Register(cb *CircuitBreaker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breakers[cb.config.Name] = cb
}

// Remove deletes the named circuit breaker from the registry.
func (r *CircuitBreakerRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, name)
}

// All returns a snapshot of all circuit breakers currently in the registry.
func (r *CircuitBreakerRegistry) All() map[string]*CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*CircuitBreaker, len(r.breakers))
	for k, v := range r.breakers {
		out[k] = v
	}
	return out
}

// ResetAll resets every circuit breaker in the registry to the closed state.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// --- HTTP Middleware ---

// CircuitBreakerMiddleware returns an HTTP middleware that wraps the next
// handler with circuit breaker protection. When the circuit is open, the
// fallbackHandler is invoked instead (typically returning 503 Service
// Unavailable with a Retry-After header).
//
// Usage:
//
//	cb := middleware.NewCircuitBreaker(config)
//	mux.Handle("/api/downstream",
//	    middleware.CircuitBreakerMiddleware(cb, fallback)(handler))
func CircuitBreakerMiddleware(cb *CircuitBreaker, fallbackHandler http.Handler) func(http.Handler) http.Handler {
	if fallbackHandler == nil {
		fallbackHandler = defaultCircuitBreakerFallback(cb)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := cb.Execute(r.Context(), func(_ context.Context) error {
				// Wrap the response writer to detect downstream errors.
				rw := &statusCapture{ResponseWriter: w}
				next.ServeHTTP(rw, r)
				if rw.status >= 500 {
					return fmt.Errorf("downstream returned status %d", rw.status)
				}
				return nil
			})

			if errors.Is(err, ErrCircuitOpen) {
				fallbackHandler.ServeHTTP(w, r)
			}
			// If err is non-nil but not ErrCircuitOpen, the downstream already
			// wrote its response (5xx), and we recorded the failure.
		})
	}
}

// defaultCircuitBreakerFallback returns a handler that responds with 503 and
// a Retry-After header based on the circuit breaker's timeout.
func defaultCircuitBreakerFallback(cb *CircuitBreaker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		retryAfter := int(cb.config.Timeout.Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		http.Error(w, "service unavailable (circuit breaker open)", http.StatusServiceUnavailable)
	})
}

// statusCapture is a thin wrapper around http.ResponseWriter that captures the
// status code written by the downstream handler.
type statusCapture struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sc *statusCapture) WriteHeader(code int) {
	if !sc.wrote {
		sc.status = code
		sc.wrote = true
	}
	sc.ResponseWriter.WriteHeader(code)
}

func (sc *statusCapture) Write(b []byte) (int, error) {
	if !sc.wrote {
		sc.status = http.StatusOK
		sc.wrote = true
	}
	return sc.ResponseWriter.Write(b)
}
