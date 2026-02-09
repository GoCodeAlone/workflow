package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewWebhookSender(t *testing.T) {
	ws := NewWebhookSender("test-sender", WebhookConfig{})

	if ws.Name() != "test-sender" {
		t.Errorf("expected name 'test-sender', got %q", ws.Name())
	}
	// Check defaults were applied
	if ws.config.MaxRetries != 3 {
		t.Errorf("expected default MaxRetries 3, got %d", ws.config.MaxRetries)
	}
	if ws.config.InitialBackoff != time.Second {
		t.Errorf("expected default InitialBackoff 1s, got %v", ws.config.InitialBackoff)
	}
	if ws.config.MaxBackoff != 60*time.Second {
		t.Errorf("expected default MaxBackoff 60s, got %v", ws.config.MaxBackoff)
	}
	if ws.config.BackoffMultiplier != 2.0 {
		t.Errorf("expected default BackoffMultiplier 2.0, got %v", ws.config.BackoffMultiplier)
	}
	if ws.config.Timeout != 30*time.Second {
		t.Errorf("expected default Timeout 30s, got %v", ws.config.Timeout)
	}
}

func TestNewWebhookSender_CustomConfig(t *testing.T) {
	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        5,
		InitialBackoff:    2 * time.Second,
		MaxBackoff:        120 * time.Second,
		BackoffMultiplier: 3.0,
		Timeout:           10 * time.Second,
	})

	if ws.config.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", ws.config.MaxRetries)
	}
	if ws.config.InitialBackoff != 2*time.Second {
		t.Errorf("expected InitialBackoff 2s, got %v", ws.config.InitialBackoff)
	}
}

func TestWebhookSender_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	ws := NewWebhookSender("sender", WebhookConfig{})
	if err := ws.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestWebhookSender_SendSuccess(t *testing.T) {
	var receivedPayload map[string]interface{}
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	})

	payload, _ := json.Marshal(map[string]interface{}{"event": "test", "data": "hello"})
	headers := map[string]string{
		"X-Webhook-ID": "wh-123",
	}

	delivery, err := ws.Send(context.Background(), server.URL, payload, headers)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if delivery.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", delivery.Status)
	}
	if delivery.DeliveredAt == nil {
		t.Error("expected DeliveredAt to be set")
	}
	if delivery.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", delivery.Attempts)
	}
	if receivedPayload["event"] != "test" {
		t.Errorf("expected event 'test', got %v", receivedPayload["event"])
	}
	if receivedHeaders.Get("X-Webhook-ID") != "wh-123" {
		t.Errorf("expected X-Webhook-ID header, got %q", receivedHeaders.Get("X-Webhook-ID"))
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", receivedHeaders.Get("Content-Type"))
	}
}

func TestWebhookSender_SendWithRetry(t *testing.T) {
	var attemptCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        3,
		InitialBackoff:    time.Millisecond, // Fast for testing
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
	})

	payload := []byte(`{"event":"retry-test"}`)
	delivery, err := ws.Send(context.Background(), server.URL, payload, nil)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if delivery.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", delivery.Status)
	}
	if delivery.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", delivery.Attempts)
	}
}

func TestWebhookSender_SendAllRetriesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        2,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
	})

	payload := []byte(`{"event":"fail-test"}`)
	delivery, err := ws.Send(context.Background(), server.URL, payload, nil)

	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
	if delivery.Status != "dead_letter" {
		t.Errorf("expected status 'dead_letter', got %q", delivery.Status)
	}
	// Initial attempt (0) + 2 retries = 3 total attempts
	if delivery.Attempts != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", delivery.Attempts)
	}
	if delivery.LastError == "" {
		t.Error("expected LastError to be set")
	}
}

func TestWebhookSender_DeadLetterQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        0, // Will be set to default 3, override below
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        time.Millisecond,
		BackoffMultiplier: 1.0,
	})
	// Override to 0 retries for fast test
	ws.config.MaxRetries = 0

	payload := []byte(`{"event":"dead-letter-test"}`)
	_, _ = ws.Send(context.Background(), server.URL, payload, nil)

	deadLetters := ws.GetDeadLetters()
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(deadLetters))
	}

	dl := deadLetters[0]
	if dl.Status != "dead_letter" {
		t.Errorf("expected status 'dead_letter', got %q", dl.Status)
	}
	if dl.URL != server.URL {
		t.Errorf("expected URL %q, got %q", server.URL, dl.URL)
	}
}

func TestWebhookSender_RetryDeadLetter(t *testing.T) {
	var callCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		// First two calls fail (initial send), then succeed on retry
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        0, // Will be set to default 3
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        time.Millisecond,
		BackoffMultiplier: 1.0,
	})
	ws.config.MaxRetries = 0 // 0 retries = 1 attempt only

	// First send will fail
	payload := []byte(`{"event":"retry-dl-test"}`)
	delivery, _ := ws.Send(context.Background(), server.URL, payload, nil)
	if delivery.Status != "dead_letter" {
		t.Fatalf("expected dead_letter status, got %q", delivery.Status)
	}

	id := delivery.ID

	// Now retry - server will accept
	ws.config.MaxRetries = 1 // Allow 1 retry
	retried, err := ws.RetryDeadLetter(context.Background(), id)
	if err != nil {
		t.Fatalf("RetryDeadLetter failed: %v", err)
	}
	if retried.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", retried.Status)
	}

	// Dead letter queue should be empty now
	deadLetters := ws.GetDeadLetters()
	if len(deadLetters) != 0 {
		t.Errorf("expected 0 dead letters after successful retry, got %d", len(deadLetters))
	}
}

func TestWebhookSender_RetryDeadLetterNotFound(t *testing.T) {
	ws := NewWebhookSender("sender", WebhookConfig{})
	_, err := ws.RetryDeadLetter(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent dead letter")
	}
}

func TestWebhookSender_SendContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:        5,
		InitialBackoff:    time.Second, // Long backoff
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	payload := []byte(`{"event":"cancel-test"}`)
	delivery, err := ws.Send(ctx, server.URL, payload, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if delivery.Status != "dead_letter" {
		t.Errorf("expected 'dead_letter' status, got %q", delivery.Status)
	}
}

// Test backoff calculation

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name     string
		initial  time.Duration
		mult     float64
		max      time.Duration
		attempt  int
		expected time.Duration
	}{
		{
			name:     "first retry",
			initial:  time.Second,
			mult:     2.0,
			max:      60 * time.Second,
			attempt:  1,
			expected: time.Second, // 1s * 2^0 = 1s
		},
		{
			name:     "second retry",
			initial:  time.Second,
			mult:     2.0,
			max:      60 * time.Second,
			attempt:  2,
			expected: 2 * time.Second, // 1s * 2^1 = 2s
		},
		{
			name:     "third retry",
			initial:  time.Second,
			mult:     2.0,
			max:      60 * time.Second,
			attempt:  3,
			expected: 4 * time.Second, // 1s * 2^2 = 4s
		},
		{
			name:     "capped at max",
			initial:  time.Second,
			mult:     2.0,
			max:      5 * time.Second,
			attempt:  10,
			expected: 5 * time.Second, // Would be 512s, capped at 5s
		},
		{
			name:     "custom multiplier",
			initial:  500 * time.Millisecond,
			mult:     3.0,
			max:      60 * time.Second,
			attempt:  2,
			expected: 1500 * time.Millisecond, // 500ms * 3^1 = 1500ms
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateBackoff(tc.initial, tc.mult, tc.max, tc.attempt)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestWebhookSender_DeliveryHasUniqueIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ws := NewWebhookSender("sender", WebhookConfig{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	})

	d1, _ := ws.Send(context.Background(), server.URL, []byte(`{}`), nil)
	d2, _ := ws.Send(context.Background(), server.URL, []byte(`{}`), nil)

	if d1.ID == d2.ID {
		t.Errorf("expected unique IDs, both got %q", d1.ID)
	}
}
