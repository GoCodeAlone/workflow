package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// WebhookSinkRetryConfig controls retry behavior for webhook delivery.
type WebhookSinkRetryConfig struct {
	MaxAttempts int           `json:"max_attempts" yaml:"max_attempts"`
	Backoff     time.Duration `json:"backoff" yaml:"backoff"`
}

// WebhookSink is an EventSink that delivers events to HTTP endpoints.
// It wraps the webhook delivery pattern used by the existing webhook.sender
// module, adding CloudEvents envelope support.
type WebhookSink struct {
	name    string
	url     string
	method  string
	headers map[string]string
	retry   WebhookSinkRetryConfig
	client  *http.Client
	healthy bool
	mu      sync.RWMutex
}

// NewWebhookSink creates a WebhookSink from a config map.
// Supported config keys: url, method, headers, retry (max_attempts, backoff).
func NewWebhookSink(name string, config map[string]any) (*WebhookSink, error) {
	url, _ := config["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("webhook sink %q: url is required", name)
	}

	method := http.MethodPost
	if m, ok := config["method"].(string); ok && m != "" {
		method = m
	}

	headers := make(map[string]string)
	if h, ok := config["headers"].(map[string]any); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	retryConfig := WebhookSinkRetryConfig{
		MaxAttempts: 3,
		Backoff:     time.Second,
	}
	if r, ok := config["retry"].(map[string]any); ok {
		if ma, ok := r["max_attempts"].(int); ok && ma > 0 {
			retryConfig.MaxAttempts = ma
		} else if ma, ok := r["max_attempts"].(float64); ok && ma > 0 {
			retryConfig.MaxAttempts = int(ma)
		}
		if b, ok := r["backoff"].(string); ok {
			if d, err := time.ParseDuration(b); err == nil {
				retryConfig.Backoff = d
			}
		}
	}

	return &WebhookSink{
		name:    name,
		url:     url,
		method:  method,
		headers: headers,
		retry:   retryConfig,
		client:  &http.Client{Timeout: 30 * time.Second},
		healthy: true,
	}, nil
}

// Name returns the connector instance name.
func (ws *WebhookSink) Name() string { return ws.name }

// Type returns the connector type identifier.
func (ws *WebhookSink) Type() string { return "webhook" }

// Deliver sends a single event to the configured HTTP endpoint with retry.
func (ws *WebhookSink) Deliver(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook sink %q: marshal event: %w", ws.name, err)
	}

	return ws.sendWithRetry(ctx, payload)
}

// DeliverBatch delivers multiple events individually, returning per-event errors.
// A nil entry in the returned slice means the corresponding event was delivered
// successfully.
func (ws *WebhookSink) DeliverBatch(ctx context.Context, events []Event) []error {
	errs := make([]error, len(events))
	for i := range events {
		errs[i] = ws.Deliver(ctx, events[i])
	}
	return errs
}

// Stop marks the sink as unhealthy. The underlying HTTP client does not
// require explicit shutdown.
func (ws *WebhookSink) Stop(_ context.Context) error {
	ws.mu.Lock()
	ws.healthy = false
	ws.mu.Unlock()
	return nil
}

// Healthy returns true when the sink is operational.
func (ws *WebhookSink) Healthy() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.healthy
}

// SetClient sets a custom HTTP client (useful for testing).
func (ws *WebhookSink) SetClient(client *http.Client) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.client = client
}

// sendWithRetry delivers a payload with exponential backoff.
func (ws *WebhookSink) sendWithRetry(ctx context.Context, payload []byte) error {
	var lastErr error

	for attempt := 0; attempt < ws.retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := ws.calculateBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if err := ws.doSend(ctx, payload); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	return fmt.Errorf("webhook sink %q: delivery failed after %d attempts: %w", ws.name, ws.retry.MaxAttempts, lastErr)
}

// doSend performs a single HTTP delivery attempt.
func (ws *WebhookSink) doSend(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, ws.method, ws.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/cloudevents+json")
	for k, v := range ws.headers {
		req.Header.Set(k, v)
	}

	ws.mu.RLock()
	client := ws.client
	ws.mu.RUnlock()

	resp, err := client.Do(req) //nolint:gosec // G704: URL is from configured webhook destination
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

// calculateBackoff returns the backoff duration for the given attempt
// using exponential backoff with a 2x multiplier.
func (ws *WebhookSink) calculateBackoff(attempt int) time.Duration {
	backoff := float64(ws.retry.Backoff) * math.Pow(2.0, float64(attempt-1))
	maxBackoff := float64(60 * time.Second)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return time.Duration(backoff)
}

// WebhookSinkFactory is a SinkFactory for creating WebhookSink instances.
func WebhookSinkFactory(name string, config map[string]any) (EventSink, error) {
	return NewWebhookSink(name, config)
}
