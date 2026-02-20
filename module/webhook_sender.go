package module

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// WebhookConfig holds configuration for the webhook sender
type WebhookConfig struct {
	MaxRetries        int           `json:"maxRetries" yaml:"maxRetries"`
	InitialBackoff    time.Duration `json:"initialBackoff" yaml:"initialBackoff"`
	MaxBackoff        time.Duration `json:"maxBackoff" yaml:"maxBackoff"`
	BackoffMultiplier float64       `json:"backoffMultiplier" yaml:"backoffMultiplier"`
	Timeout           time.Duration `json:"timeout" yaml:"timeout"`
}

// WebhookDelivery tracks a webhook delivery attempt
type WebhookDelivery struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	Payload     []byte            `json:"payload"`
	Headers     map[string]string `json:"headers"`
	Status      string            `json:"status"` // "pending", "delivered", "failed", "dead_letter"
	Attempts    int               `json:"attempts"`
	LastError   string            `json:"lastError,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	DeliveredAt *time.Time        `json:"deliveredAt,omitempty"`
}

// WebhookSender sends webhooks with retry logic
type WebhookSender struct {
	name       string
	config     WebhookConfig
	client     *http.Client
	deadLetter map[string]*WebhookDelivery
	mu         sync.RWMutex
	idCounter  int
}

// NewWebhookSender creates a new WebhookSender with sensible defaults
func NewWebhookSender(name string, config WebhookConfig) *WebhookSender {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = time.Second
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = 60 * time.Second
	}
	if config.BackoffMultiplier <= 0 {
		config.BackoffMultiplier = 2.0
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	return &WebhookSender{
		name:   name,
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		deadLetter: make(map[string]*WebhookDelivery),
	}
}

// Name returns the module name
func (ws *WebhookSender) Name() string {
	return ws.name
}

// Init registers the webhook sender as a service
func (ws *WebhookSender) Init(app modular.Application) error {
	return app.RegisterService("webhook.sender", ws)
}

// SetClient sets a custom HTTP client (useful for testing)
func (ws *WebhookSender) SetClient(client *http.Client) {
	ws.client = client
}

// Send sends a webhook with retry logic
func (ws *WebhookSender) Send(ctx context.Context, url string, payload []byte, headers map[string]string) (*WebhookDelivery, error) {
	ws.mu.Lock()
	ws.idCounter++
	id := fmt.Sprintf("wh-%d-%d", time.Now().UnixNano(), ws.idCounter)
	ws.mu.Unlock()

	delivery := &WebhookDelivery{
		ID:        id,
		URL:       url,
		Payload:   payload,
		Headers:   headers,
		Status:    "pending",
		Attempts:  0,
		CreatedAt: time.Now(),
	}

	err := ws.sendWithRetry(ctx, delivery)
	if err != nil {
		// Move to dead letter queue
		delivery.Status = "dead_letter"
		delivery.LastError = err.Error()
		ws.mu.Lock()
		ws.deadLetter[delivery.ID] = delivery
		ws.mu.Unlock()
		return delivery, err
	}

	return delivery, nil
}

// sendWithRetry attempts to deliver a webhook with exponential backoff
func (ws *WebhookSender) sendWithRetry(ctx context.Context, delivery *WebhookDelivery) error {
	var lastErr error

	for attempt := 0; attempt <= ws.config.MaxRetries; attempt++ {
		delivery.Attempts = attempt + 1

		// Wait with backoff (skip wait on first attempt)
		if attempt > 0 {
			backoff := ws.calculateBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := ws.doSend(ctx, delivery)
		if err == nil {
			now := time.Now()
			delivery.Status = "delivered"
			delivery.DeliveredAt = &now
			return nil
		}

		lastErr = err
	}

	delivery.Status = "failed"
	return lastErr
}

// doSend performs a single webhook delivery attempt
func (ws *WebhookSender) doSend(ctx context.Context, delivery *WebhookDelivery) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set default content type
	req.Header.Set("Content-Type", "application/json")

	// Set custom headers
	for k, v := range delivery.Headers {
		req.Header.Set(k, v)
	}

	resp, err := ws.client.Do(req) //nolint:gosec // G704: SSRF via taint analysis
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

// calculateBackoff calculates the backoff duration for a given attempt
func (ws *WebhookSender) calculateBackoff(attempt int) time.Duration {
	backoff := float64(ws.config.InitialBackoff) * math.Pow(ws.config.BackoffMultiplier, float64(attempt-1))
	if backoff > float64(ws.config.MaxBackoff) {
		backoff = float64(ws.config.MaxBackoff)
	}
	return time.Duration(backoff)
}

// GetDeadLetters returns all dead letter deliveries
func (ws *WebhookSender) GetDeadLetters() []*WebhookDelivery {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	result := make([]*WebhookDelivery, 0, len(ws.deadLetter))
	for _, d := range ws.deadLetter {
		result = append(result, d)
	}
	return result
}

// RetryDeadLetter retries a dead letter delivery
func (ws *WebhookSender) RetryDeadLetter(ctx context.Context, id string) (*WebhookDelivery, error) {
	ws.mu.Lock()
	delivery, exists := ws.deadLetter[id]
	if !exists {
		ws.mu.Unlock()
		return nil, fmt.Errorf("dead letter '%s' not found", id)
	}
	// Remove from dead letter queue before retry
	delete(ws.deadLetter, id)
	ws.mu.Unlock()

	// Reset delivery state for retry
	delivery.Status = "pending"
	delivery.Attempts = 0

	err := ws.sendWithRetry(ctx, delivery)
	if err != nil {
		delivery.Status = "dead_letter"
		delivery.LastError = err.Error()
		ws.mu.Lock()
		ws.deadLetter[delivery.ID] = delivery
		ws.mu.Unlock()
		return delivery, err
	}

	return delivery, nil
}

// CalculateBackoff is exported for testing
func CalculateBackoff(initialBackoff time.Duration, multiplier float64, maxBackoff time.Duration, attempt int) time.Duration {
	backoff := float64(initialBackoff) * math.Pow(multiplier, float64(attempt-1))
	if backoff > float64(maxBackoff) {
		backoff = float64(maxBackoff)
	}
	return time.Duration(backoff)
}
