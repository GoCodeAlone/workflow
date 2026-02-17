package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// RateLimitStep is a pipeline step that enforces rate limiting using a
// token bucket algorithm. Requests that exceed the limit are rejected
// with an error.
type RateLimitStep struct {
	name              string
	requestsPerMinute int
	burstSize         int
	keyFrom           string // template for per-client key
	tmpl              *TemplateEngine

	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

// tokenBucket implements a simple token bucket rate limiter.
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newTokenBucket(maxTokens float64, refillRate float64) *tokenBucket {
	return &tokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// NewRateLimitStepFactory returns a StepFactory that creates RateLimitStep instances.
func NewRateLimitStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		rpm := 60
		if v, ok := config["requests_per_minute"]; ok {
			switch val := v.(type) {
			case int:
				rpm = val
			case float64:
				rpm = int(val)
			}
		}
		if rpm <= 0 {
			return nil, fmt.Errorf("rate_limit step %q: requests_per_minute must be positive", name)
		}

		burst := 10
		if v, ok := config["burst_size"]; ok {
			switch val := v.(type) {
			case int:
				burst = val
			case float64:
				burst = int(val)
			}
		}
		if burst <= 0 {
			return nil, fmt.Errorf("rate_limit step %q: burst_size must be positive", name)
		}

		keyFrom := "global"
		if v, ok := config["key_from"].(string); ok && v != "" {
			keyFrom = v
		}

		return &RateLimitStep{
			name:              name,
			requestsPerMinute: rpm,
			burstSize:         burst,
			keyFrom:           keyFrom,
			tmpl:              NewTemplateEngine(),
			buckets:           make(map[string]*tokenBucket),
		}, nil
	}
}

// Name returns the step name.
func (s *RateLimitStep) Name() string { return s.name }

// Execute checks rate limiting for the resolved key and either allows or
// rejects the request.
func (s *RateLimitStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the rate limit key from template
	key := s.keyFrom
	if s.tmpl != nil && key != "global" {
		resolved, err := s.tmpl.Resolve(key, pc)
		if err == nil && resolved != "" {
			key = resolved
		}
	}

	s.mu.Lock()
	bucket, exists := s.buckets[key]
	if !exists {
		refillRate := float64(s.requestsPerMinute) / 60.0
		bucket = newTokenBucket(float64(s.burstSize), refillRate)
		s.buckets[key] = bucket
	}
	allowed := bucket.allow()
	s.mu.Unlock()

	if !allowed {
		return nil, fmt.Errorf("rate_limit step %q: rate limit exceeded for key %q", s.name, key)
	}

	return &StepResult{
		Output: map[string]any{
			"rate_limit": map[string]any{
				"allowed": true,
				"key":     key,
			},
		},
	}, nil
}
