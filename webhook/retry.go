package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// DeliveryStatus represents the status of a webhook delivery.
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
	StatusDeadLetter DeliveryStatus = "dead_letter"
)

// RetryConfig holds configuration for the retry manager.
type RetryConfig struct {
	MaxRetries        int           `json:"maxRetries" yaml:"maxRetries"`
	InitialBackoff    time.Duration `json:"initialBackoff" yaml:"initialBackoff"`
	MaxBackoff        time.Duration `json:"maxBackoff" yaml:"maxBackoff"`
	BackoffMultiplier float64       `json:"backoffMultiplier" yaml:"backoffMultiplier"`
	JitterFraction    float64       `json:"jitterFraction" yaml:"jitterFraction"`
	Timeout           time.Duration `json:"timeout" yaml:"timeout"`
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        5,
		InitialBackoff:    time.Second,
		MaxBackoff:        60 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFraction:    0.1,
		Timeout:           30 * time.Second,
	}
}

// Delivery tracks a single webhook delivery attempt.
type Delivery struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	Payload     []byte            `json:"payload"`
	Headers     map[string]string `json:"headers"`
	Status      DeliveryStatus    `json:"status"`
	Attempts    int               `json:"attempts"`
	MaxRetries  int               `json:"maxRetries"`
	LastError   string            `json:"lastError,omitempty"`
	StatusCode  int               `json:"statusCode,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	LastAttempt *time.Time        `json:"lastAttempt,omitempty"`
	DeliveredAt *time.Time        `json:"deliveredAt,omitempty"`
}

// RetryManager handles webhook delivery with configurable exponential backoff and jitter.
type RetryManager struct {
	config RetryConfig
	client *http.Client
	store  *DeadLetterStore
}

// NewRetryManager creates a new RetryManager with the given config and dead letter store.
func NewRetryManager(config RetryConfig, store *DeadLetterStore) *RetryManager {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 5
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
	if config.JitterFraction < 0 {
		config.JitterFraction = 0
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	return &RetryManager{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
		store:  store,
	}
}

// SetClient sets a custom HTTP client (useful for testing).
func (rm *RetryManager) SetClient(client *http.Client) {
	rm.client = client
}

// Send delivers a webhook with retry logic. On exhausting retries, the delivery
// is placed in the dead letter store.
func (rm *RetryManager) Send(ctx context.Context, url string, payload []byte, headers map[string]string) (*Delivery, error) {
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate delivery id: %w", err)
	}

	delivery := &Delivery{
		ID:         id,
		URL:        url,
		Payload:    payload,
		Headers:    headers,
		Status:     StatusPending,
		MaxRetries: rm.config.MaxRetries,
		CreatedAt:  time.Now(),
	}

	if err := rm.deliver(ctx, delivery); err != nil {
		delivery.Status = StatusDeadLetter
		delivery.LastError = err.Error()
		rm.store.Add(delivery)
		return delivery, err
	}

	return delivery, nil
}

// Replay retries a dead-lettered delivery.
func (rm *RetryManager) Replay(ctx context.Context, id string) (*Delivery, error) {
	delivery, ok := rm.store.Remove(id)
	if !ok {
		return nil, fmt.Errorf("dead letter %q not found", id)
	}

	delivery.Status = StatusPending
	delivery.Attempts = 0
	delivery.LastError = ""
	delivery.StatusCode = 0

	if err := rm.deliver(ctx, delivery); err != nil {
		delivery.Status = StatusDeadLetter
		delivery.LastError = err.Error()
		rm.store.Add(delivery)
		return delivery, err
	}

	return delivery, nil
}

func (rm *RetryManager) deliver(ctx context.Context, d *Delivery) error {
	var lastErr error

	for attempt := 0; attempt <= rm.config.MaxRetries; attempt++ {
		d.Attempts = attempt + 1
		now := time.Now()
		d.LastAttempt = &now

		if attempt > 0 {
			backoff := rm.backoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := rm.doSend(ctx, d)
		if err == nil {
			t := time.Now()
			d.Status = StatusDelivered
			d.DeliveredAt = &t
			return nil
		}
		lastErr = err
	}

	d.Status = StatusFailed
	return lastErr
}

func (rm *RetryManager) doSend(ctx context.Context, d *Delivery) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}

	resp, err := rm.client.Do(req) //nolint:gosec // G704: URL from configured webhook endpoint
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	d.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

func (rm *RetryManager) backoff(attempt int) time.Duration {
	base := float64(rm.config.InitialBackoff) * math.Pow(rm.config.BackoffMultiplier, float64(attempt-1))
	if base > float64(rm.config.MaxBackoff) {
		base = float64(rm.config.MaxBackoff)
	}
	if rm.config.JitterFraction > 0 {
		jitter := base * rm.config.JitterFraction * (cryptoFloat64()*2 - 1)
		base += jitter
		if base < 0 {
			base = 0
		}
	}
	return time.Duration(base)
}

// cryptoFloat64 returns a cryptographically random float64 in [0.0, 1.0).
func cryptoFloat64() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	// Use top 53 bits for a uniform float64 in [0, 1)
	return float64(binary.BigEndian.Uint64(b[:])>>(64-53)) / float64(1<<53)
}

func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "wh-" + hex.EncodeToString(b), nil
}
