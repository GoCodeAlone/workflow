package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// RetryWithBackoffStep executes a sub-step with exponential backoff retry logic.
type RetryWithBackoffStep struct {
	name         string
	maxRetries   int
	initialDelay time.Duration
	maxDelay     time.Duration
	multiplier   float64
	subStep      PipelineStep
}

// NewRetryWithBackoffStepFactory returns a StepFactory for RetryWithBackoffStep.
// registryFn is called at creation time to resolve the sub-step factory.
func NewRetryWithBackoffStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		maxRetries := 3
		if v, ok := config["max_retries"]; ok {
			switch val := v.(type) {
			case int:
				maxRetries = val
			case float64:
				maxRetries = int(val)
			}
		}
		if maxRetries < 0 {
			maxRetries = 0
		}

		initialDelay := time.Second
		if s, ok := config["initial_delay"].(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("retry_with_backoff step %q: invalid initial_delay %q: %w", name, s, err)
			}
			initialDelay = d
		}

		maxDelay := 30 * time.Second
		if s, ok := config["max_delay"].(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("retry_with_backoff step %q: invalid max_delay %q: %w", name, s, err)
			}
			maxDelay = d
		}

		multiplier := 2.0
		if v, ok := config["multiplier"]; ok {
			switch val := v.(type) {
			case float64:
				multiplier = val
			case int:
				multiplier = float64(val)
			}
		}
		if multiplier <= 0 {
			multiplier = 2.0
		}

		stepCfg, ok := config["step"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("retry_with_backoff step %q: 'step' config is required", name)
		}

		subStep, err := buildSubStep(name, "step", stepCfg, registryFn, app)
		if err != nil {
			return nil, fmt.Errorf("retry_with_backoff step %q: %w", name, err)
		}

		return &RetryWithBackoffStep{
			name:         name,
			maxRetries:   maxRetries,
			initialDelay: initialDelay,
			maxDelay:     maxDelay,
			multiplier:   multiplier,
			subStep:      subStep,
		}, nil
	}
}

// Name returns the step name.
func (s *RetryWithBackoffStep) Name() string { return s.name }

// Execute runs the sub-step with exponential backoff retries on failure.
func (s *RetryWithBackoffStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	delay := s.initialDelay
	var lastErr error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		result, err := s.subStep.Execute(ctx, pc)
		if err == nil {
			result = ensureOutput(result)
			result.Output["retry_attempts"] = attempt
			return result, nil
		}
		lastErr = err

		if attempt < s.maxRetries {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("retry_with_backoff step %q: context cancelled after %d attempts: %w",
					s.name, attempt+1, ctx.Err())
			case <-time.After(delay):
			}
			delay = time.Duration(float64(delay) * s.multiplier)
			if delay > s.maxDelay {
				delay = s.maxDelay
			}
		}
	}

	return nil, fmt.Errorf("retry_with_backoff step %q: all %d attempts failed: %w",
		s.name, s.maxRetries+1, lastErr)
}

// ResilienceCircuitBreakerStep wraps a sub-step with circuit breaker protection.
// It tracks failures and opens the circuit when the threshold is reached,
// executing a fallback step (if configured) when the circuit is open.
type ResilienceCircuitBreakerStep struct {
	name             string
	failureThreshold int
	resetTimeout     time.Duration
	subStep          PipelineStep
	fallbackStep     PipelineStep // optional; executed when circuit is open

	mu               sync.Mutex
	state            CircuitState
	consecutiveFails int
	lastFailure      time.Time
}

// NewResilienceCircuitBreakerStepFactory returns a StepFactory for ResilienceCircuitBreakerStep.
// registryFn is called at creation time to resolve sub-step and fallback step factories.
func NewResilienceCircuitBreakerStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
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

		resetTimeout := 60 * time.Second
		if s, ok := config["reset_timeout"].(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("circuit_breaker step %q: invalid reset_timeout %q: %w", name, s, err)
			}
			resetTimeout = d
		}

		stepCfg, ok := config["step"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("circuit_breaker step %q: 'step' config is required", name)
		}

		subStep, err := buildSubStep(name, "step", stepCfg, registryFn, app)
		if err != nil {
			return nil, fmt.Errorf("circuit_breaker step %q: %w", name, err)
		}

		cb := &ResilienceCircuitBreakerStep{
			name:             name,
			failureThreshold: failThresh,
			resetTimeout:     resetTimeout,
			subStep:          subStep,
			state:            CircuitClosed,
		}

		if fallbackCfg, ok := config["fallback"].(map[string]any); ok {
			fb, err := buildSubStep(name, "fallback", fallbackCfg, registryFn, app)
			if err != nil {
				return nil, fmt.Errorf("circuit_breaker step %q: %w", name, err)
			}
			cb.fallbackStep = fb
		}

		return cb, nil
	}
}

// Name returns the step name.
func (s *ResilienceCircuitBreakerStep) Name() string { return s.name }

// Execute checks the circuit state and either runs the sub-step or the fallback.
func (s *ResilienceCircuitBreakerStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	s.mu.Lock()
	circuitOpen := s.isOpen()
	s.mu.Unlock()

	if circuitOpen {
		if s.fallbackStep != nil {
			result, err := s.fallbackStep.Execute(ctx, pc)
			if err != nil {
				return nil, fmt.Errorf("circuit_breaker step %q: fallback failed: %w", s.name, err)
			}
			result = ensureOutput(result)
			result.Output["circuit_breaker_open"] = true
			return result, nil
		}
		return nil, fmt.Errorf("circuit_breaker step %q: circuit is open", s.name)
	}

	result, err := s.subStep.Execute(ctx, pc)
	if err != nil {
		s.mu.Lock()
		s.recordFailure()
		s.mu.Unlock()
		return nil, err
	}

	s.mu.Lock()
	s.consecutiveFails = 0
	s.mu.Unlock()

	result = ensureOutput(result)
	result.Output["circuit_breaker_open"] = false
	return result, nil
}

// isOpen returns true if the circuit should reject requests. Must be called with mu held.
func (s *ResilienceCircuitBreakerStep) isOpen() bool {
	if s.state == CircuitClosed {
		return false
	}
	// After reset timeout, try half-open
	if s.state == CircuitOpen && time.Since(s.lastFailure) >= s.resetTimeout {
		s.state = CircuitHalfOpen
		return false
	}
	return s.state == CircuitOpen
}

// recordFailure increments the failure counter. Must be called with mu held.
func (s *ResilienceCircuitBreakerStep) recordFailure() {
	s.consecutiveFails++
	s.lastFailure = time.Now()
	if s.consecutiveFails >= s.failureThreshold {
		s.state = CircuitOpen
	}
	if s.state == CircuitHalfOpen {
		s.state = CircuitOpen
	}
}

// buildSubStep extracts type+name from a step config map and creates the step.
func buildSubStep(parentName, field string, cfg map[string]any, registryFn func() *StepRegistry, app modular.Application) (PipelineStep, error) {
	registry := registryFn()
	if registry == nil {
		return nil, fmt.Errorf("failed to build %s sub-step: registry not available", field)
	}

	stepType, _ := cfg["type"].(string)
	if stepType == "" {
		return nil, fmt.Errorf("failed to build %s sub-step for %q: 'type' is required", field, parentName)
	}

	stepName, _ := cfg["name"].(string)
	if stepName == "" {
		stepName = fmt.Sprintf("%s-%s", parentName, field)
	}

	subCfg := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if k != "type" && k != "name" {
			subCfg[k] = v
		}
	}

	return registry.Create(stepType, stepName, subCfg, app)
}

// ensureOutput ensures the result has an initialized Output map.
func ensureOutput(r *StepResult) *StepResult {
	if r == nil {
		return &StepResult{Output: make(map[string]any)}
	}
	if r.Output == nil {
		r.Output = make(map[string]any)
	}
	return r
}
