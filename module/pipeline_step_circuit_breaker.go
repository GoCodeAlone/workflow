package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

// CircuitBreakerStep implements the circuit breaker pattern as a pipeline step.
// It tracks failures per service and opens the circuit when the failure
// threshold is reached, preventing further calls until recovery.
type CircuitBreakerStep struct {
	name             string
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	serviceName      string

	mu               sync.Mutex
	state            CircuitState
	consecutiveFails int
	consecutiveOK    int
	lastFailure      time.Time
}

// NewCircuitBreakerStepFactory returns a StepFactory that creates CircuitBreakerStep instances.
func NewCircuitBreakerStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		failThresh := 5
		if v, ok := config["failure_threshold"]; ok {
			switch val := v.(type) {
			case int:
				failThresh = val
			case float64:
				failThresh = int(val)
			}
		}
		if failThresh <= 0 {
			return nil, fmt.Errorf("circuit_breaker step %q: failure_threshold must be positive", name)
		}

		successThresh := 3
		if v, ok := config["success_threshold"]; ok {
			switch val := v.(type) {
			case int:
				successThresh = val
			case float64:
				successThresh = int(val)
			}
		}
		if successThresh <= 0 {
			return nil, fmt.Errorf("circuit_breaker step %q: success_threshold must be positive", name)
		}

		timeout := 30 * time.Second
		if ts, ok := config["timeout"].(string); ok && ts != "" {
			d, err := time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("circuit_breaker step %q: invalid timeout %q: %w", name, ts, err)
			}
			timeout = d
		}

		svcName, _ := config["service_name"].(string)
		if svcName == "" {
			svcName = name
		}

		return &CircuitBreakerStep{
			name:             name,
			failureThreshold: failThresh,
			successThreshold: successThresh,
			timeout:          timeout,
			serviceName:      svcName,
			state:            CircuitClosed,
		}, nil
	}
}

// Name returns the step name.
func (s *CircuitBreakerStep) Name() string { return s.name }

// Execute checks the circuit state. When closed or half-open the request is
// allowed through. When open the request is rejected unless the timeout has
// elapsed, in which case the circuit transitions to half-open.
func (s *CircuitBreakerStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.state {
	case CircuitOpen:
		if time.Since(s.lastFailure) >= s.timeout {
			s.state = CircuitHalfOpen
			s.consecutiveOK = 0
			return &StepResult{
				Output: map[string]any{
					"circuit_breaker": map[string]any{
						"state":        string(CircuitHalfOpen),
						"service":      s.serviceName,
						"allowed":      true,
						"transitioned": true,
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("circuit_breaker step %q: circuit is open for service %q", s.name, s.serviceName)

	case CircuitHalfOpen:
		return &StepResult{
			Output: map[string]any{
				"circuit_breaker": map[string]any{
					"state":   string(CircuitHalfOpen),
					"service": s.serviceName,
					"allowed": true,
				},
			},
		}, nil

	default: // closed
		return &StepResult{
			Output: map[string]any{
				"circuit_breaker": map[string]any{
					"state":   string(CircuitClosed),
					"service": s.serviceName,
					"allowed": true,
				},
			},
		}, nil
	}
}

// RecordSuccess records a successful call through the circuit breaker.
func (s *CircuitBreakerStep) RecordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveFails = 0

	if s.state == CircuitHalfOpen {
		s.consecutiveOK++
		if s.consecutiveOK >= s.successThreshold {
			s.state = CircuitClosed
			s.consecutiveOK = 0
		}
	}
}

// RecordFailure records a failed call. If the failure threshold is reached
// the circuit opens.
func (s *CircuitBreakerStep) RecordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveFails++
	s.consecutiveOK = 0
	s.lastFailure = time.Now()

	if s.consecutiveFails >= s.failureThreshold {
		s.state = CircuitOpen
	}

	if s.state == CircuitHalfOpen {
		s.state = CircuitOpen
	}
}

// State returns the current circuit state.
func (s *CircuitBreakerStep) State() CircuitState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}
