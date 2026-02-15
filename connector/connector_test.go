package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestWebhookSourceStartStop
// ---------------------------------------------------------------------------

func TestWebhookSourceStartStop(t *testing.T) {
	src, err := NewWebhookSource("test-src", map[string]any{
		"address": "127.0.0.1:0", // OS-assigned port
		"path":    "/hook",
	})
	if err != nil {
		t.Fatalf("NewWebhookSource: %v", err)
	}

	if src.Name() != "test-src" {
		t.Errorf("expected name %q, got %q", "test-src", src.Name())
	}
	if src.Type() != "webhook" {
		t.Errorf("expected type %q, got %q", "webhook", src.Type())
	}

	output := make(chan Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(ctx) }()

	if !src.Healthy() {
		t.Error("expected source to be healthy after Start")
	}

	// Send a webhook via HTTP
	url := "http://" + src.Addr() + "/hook"
	body := `{"hello":"world"}`
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST webhook: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	// Read the emitted event
	select {
	case evt := <-output:
		if evt.Source != "webhook/test-src" {
			t.Errorf("expected source %q, got %q", "webhook/test-src", evt.Source)
		}
		if evt.Type != "webhook.received" {
			t.Errorf("expected type %q, got %q", "webhook.received", evt.Type)
		}
		var data map[string]any
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if data["hello"] != "world" {
			t.Errorf("expected data.hello == %q, got %v", "world", data["hello"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Stop and verify unhealthy
	if err := src.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if src.Healthy() {
		t.Error("expected source to be unhealthy after Stop")
	}

	// Checkpoint should be a no-op
	if err := src.Checkpoint(ctx); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
}

func TestWebhookSourceSignatureValidation(t *testing.T) {
	secret := "test-secret-key"
	src, err := NewWebhookSource("signed-src", map[string]any{
		"address": "127.0.0.1:0",
		"path":    "/signed",
		"secret":  secret,
	})
	if err != nil {
		t.Fatalf("NewWebhookSource: %v", err)
	}

	output := make(chan Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(ctx) }()

	url := "http://" + src.Addr() + "/signed"

	// Request without signature should fail
	resp, err := http.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without signature, got %d", resp.StatusCode)
	}

	// Request with invalid signature should fail
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", "sha256=deadbeef")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST with bad sig: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with bad signature, got %d", resp.StatusCode)
	}
}

func TestWebhookSourceMethodNotAllowed(t *testing.T) {
	src, err := NewWebhookSource("method-src", map[string]any{
		"address": "127.0.0.1:0",
		"path":    "/hook",
	})
	if err != nil {
		t.Fatalf("NewWebhookSource: %v", err)
	}

	output := make(chan Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(ctx) }()

	url := "http://" + src.Addr() + "/hook"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// TestWebhookSinkDeliver
// ---------------------------------------------------------------------------

func TestWebhookSinkDeliver(t *testing.T) {
	var received json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/cloudevents+json" {
			t.Errorf("expected Content-Type %q, got %q", "application/cloudevents+json", ct)
		}
		body, _ := readAll(r.Body)
		received = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink, err := NewWebhookSink("test-sink", map[string]any{
		"url": server.URL,
		"headers": map[string]any{
			"X-Custom": "value",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}

	if sink.Name() != "test-sink" {
		t.Errorf("expected name %q, got %q", "test-sink", sink.Name())
	}
	if sink.Type() != "webhook" {
		t.Errorf("expected type %q, got %q", "webhook", sink.Type())
	}
	if !sink.Healthy() {
		t.Error("expected sink to be healthy")
	}

	event := Event{
		ID:     "evt-001",
		Source: "test",
		Type:   "test.event",
		Time:   time.Now().UTC(),
		Data:   json.RawMessage(`{"key":"value"}`),
	}

	if err := sink.Deliver(context.Background(), event); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Verify the event was received
	var decoded Event
	if err := json.Unmarshal(received, &decoded); err != nil {
		t.Fatalf("unmarshal received: %v", err)
	}
	if decoded.ID != "evt-001" {
		t.Errorf("expected event ID %q, got %q", "evt-001", decoded.ID)
	}
	if decoded.Type != "test.event" {
		t.Errorf("expected event type %q, got %q", "test.event", decoded.Type)
	}

	// Stop and verify
	if err := sink.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if sink.Healthy() {
		t.Error("expected sink to be unhealthy after Stop")
	}
}

func TestWebhookSinkRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink, err := NewWebhookSink("retry-sink", map[string]any{
		"url": server.URL,
		"retry": map[string]any{
			"max_attempts": float64(5),
			"backoff":      "1ms",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}

	event := Event{
		ID:     "evt-retry",
		Source: "test",
		Type:   "test.retry",
		Time:   time.Now().UTC(),
		Data:   json.RawMessage(`{}`),
	}

	if err := sink.Deliver(context.Background(), event); err != nil {
		t.Fatalf("Deliver with retry: %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("expected 3 attempts, got %d", finalAttempts)
	}
}

func TestWebhookSinkAllRetiresFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sink, err := NewWebhookSink("fail-sink", map[string]any{
		"url": server.URL,
		"retry": map[string]any{
			"max_attempts": float64(2),
			"backoff":      "1ms",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}

	event := Event{
		ID:     "evt-fail",
		Source: "test",
		Type:   "test.fail",
		Time:   time.Now().UTC(),
		Data:   json.RawMessage(`{}`),
	}

	err = sink.Deliver(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
	if !strings.Contains(err.Error(), "delivery failed after 2 attempts") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWebhookSinkMissingURL(t *testing.T) {
	_, err := NewWebhookSink("no-url", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

// ---------------------------------------------------------------------------
// TestRegistryCreateSource
// ---------------------------------------------------------------------------

func TestRegistryCreateSource(t *testing.T) {
	reg := NewRegistry()

	// Register the webhook source factory
	if err := reg.RegisterSource("webhook", WebhookSourceFactory); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	// Duplicate registration should fail
	if err := reg.RegisterSource("webhook", WebhookSourceFactory); err == nil {
		t.Error("expected error on duplicate source registration")
	}

	// Create a source
	src, err := reg.CreateSource("webhook", "my-source", map[string]any{
		"address": "127.0.0.1:0",
		"path":    "/ingest",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if src.Name() != "my-source" {
		t.Errorf("expected name %q, got %q", "my-source", src.Name())
	}

	// Duplicate name should fail
	_, err = reg.CreateSource("webhook", "my-source", map[string]any{
		"address": "127.0.0.1:0",
	})
	if err == nil {
		t.Error("expected error on duplicate instance name")
	}

	// Unknown type should fail
	_, err = reg.CreateSource("unknown-type", "x", map[string]any{})
	if err == nil {
		t.Error("expected error on unknown source type")
	}

	// Verify instance tracking
	inst, ok := reg.GetInstance("my-source")
	if !ok {
		t.Fatal("expected instance to be tracked")
	}
	if inst.(EventSource).Name() != "my-source" {
		t.Error("instance mismatch")
	}

	// ListSources
	sources := reg.ListSources()
	if len(sources) != 1 || sources[0] != "webhook" {
		t.Errorf("expected [webhook], got %v", sources)
	}
}

// ---------------------------------------------------------------------------
// TestRegistryCreateSink
// ---------------------------------------------------------------------------

func TestRegistryCreateSink(t *testing.T) {
	reg := NewRegistry()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Register the webhook sink factory
	if err := reg.RegisterSink("webhook", WebhookSinkFactory); err != nil {
		t.Fatalf("RegisterSink: %v", err)
	}

	// Duplicate registration should fail
	if err := reg.RegisterSink("webhook", WebhookSinkFactory); err == nil {
		t.Error("expected error on duplicate sink registration")
	}

	// Create a sink
	sink, err := reg.CreateSink("webhook", "my-sink", map[string]any{
		"url": server.URL,
	})
	if err != nil {
		t.Fatalf("CreateSink: %v", err)
	}
	if sink.Name() != "my-sink" {
		t.Errorf("expected name %q, got %q", "my-sink", sink.Name())
	}

	// Duplicate name should fail
	_, err = reg.CreateSink("webhook", "my-sink", map[string]any{
		"url": server.URL,
	})
	if err == nil {
		t.Error("expected error on duplicate instance name")
	}

	// Unknown type should fail
	_, err = reg.CreateSink("unknown-type", "x", map[string]any{})
	if err == nil {
		t.Error("expected error on unknown sink type")
	}

	// Verify instance tracking
	inst, ok := reg.GetInstance("my-sink")
	if !ok {
		t.Fatal("expected instance to be tracked")
	}
	if inst.(EventSink).Name() != "my-sink" {
		t.Error("instance mismatch")
	}

	// ListSinks
	sinks := reg.ListSinks()
	if len(sinks) != 1 || sinks[0] != "webhook" {
		t.Errorf("expected [webhook], got %v", sinks)
	}
}

// ---------------------------------------------------------------------------
// TestEventCloudEventsFormat
// ---------------------------------------------------------------------------

func TestEventCloudEventsFormat(t *testing.T) {
	event := Event{
		ID:              "ce-001",
		Source:          "connector/test",
		Type:            "com.example.test",
		Subject:         "/resource/123",
		Time:            time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC),
		Data:            json.RawMessage(`{"foo":"bar"}`),
		DataSchema:      "https://example.com/schema",
		DataContentType: "application/json",
		TenantID:        "tenant-1",
		PipelineID:      "pipe-1",
		IdempotencyKey:  "idem-1",
	}

	// Serialize to JSON
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Internal fields should NOT appear in JSON output
	jsonStr := string(data)
	if strings.Contains(jsonStr, "tenant-1") {
		t.Error("TenantID should not appear in JSON (json:\"-\" tag)")
	}
	if strings.Contains(jsonStr, "pipe-1") {
		t.Error("PipelineID should not appear in JSON (json:\"-\" tag)")
	}
	if strings.Contains(jsonStr, "idem-1") {
		t.Error("IdempotencyKey should not appear in JSON (json:\"-\" tag)")
	}

	// Public fields should appear
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	checks := map[string]string{
		"id":              "ce-001",
		"source":          "connector/test",
		"type":            "com.example.test",
		"subject":         "/resource/123",
		"dataschema":      "https://example.com/schema",
		"datacontenttype": "application/json",
	}
	for field, expected := range checks {
		val, ok := decoded[field].(string)
		if !ok || val != expected {
			t.Errorf("field %q: expected %q, got %v", field, expected, decoded[field])
		}
	}

	// Data should roundtrip correctly
	var roundtripped Event
	if err := json.Unmarshal(data, &roundtripped); err != nil {
		t.Fatalf("Unmarshal into Event: %v", err)
	}
	if string(roundtripped.Data) != `{"foo":"bar"}` {
		t.Errorf("data roundtrip: expected %q, got %q", `{"foo":"bar"}`, string(roundtripped.Data))
	}
}

// ---------------------------------------------------------------------------
// TestBatchDelivery
// ---------------------------------------------------------------------------

func TestBatchDelivery(t *testing.T) {
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink, err := NewWebhookSink("batch-sink", map[string]any{
		"url": server.URL,
	})
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}

	events := []Event{
		{ID: "b-1", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-2", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-3", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
	}

	errs := sink.DeliverBatch(context.Background(), events)
	if len(errs) != 3 {
		t.Fatalf("expected 3 error slots, got %d", len(errs))
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("event %d: unexpected error: %v", i, err)
		}
	}

	total := atomic.LoadInt32(&count)
	if total != 3 {
		t.Errorf("expected 3 HTTP calls, got %d", total)
	}
}

func TestBatchDeliveryPartialFailure(t *testing.T) {
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		if n == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink, err := NewWebhookSink("batch-partial", map[string]any{
		"url": server.URL,
		"retry": map[string]any{
			"max_attempts": float64(1), // No retries
			"backoff":      "1ms",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}

	events := []Event{
		{ID: "b-1", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-2", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-3", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
	}

	errs := sink.DeliverBatch(context.Background(), events)
	if errs[0] != nil {
		t.Errorf("event 0: unexpected error: %v", errs[0])
	}
	if errs[1] == nil {
		t.Error("event 1: expected error for failed delivery")
	}
	if errs[2] != nil {
		t.Errorf("event 2: unexpected error: %v", errs[2])
	}
}

// ---------------------------------------------------------------------------
// TestRegistryStopAll
// ---------------------------------------------------------------------------

func TestRegistryStopAll(t *testing.T) {
	reg := NewRegistry()

	if err := reg.RegisterSource("webhook", WebhookSourceFactory); err != nil {
		t.Fatalf("RegisterSource: %v", err)
	}

	src, err := reg.CreateSource("webhook", "stop-src", map[string]any{
		"address": "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// Start the source
	output := make(chan Event, 10)
	if err := src.Start(context.Background(), output); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !src.Healthy() {
		t.Error("expected source healthy before StopAll")
	}

	// StopAll should shut everything down
	if err := reg.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	if src.Healthy() {
		t.Error("expected source unhealthy after StopAll")
	}

	// Instance map should be cleared
	if _, ok := reg.GetInstance("stop-src"); ok {
		t.Error("expected instances to be cleared after StopAll")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readAll(r interface{ Read([]byte) (int, error) }) (json.RawMessage, error) {
	data, err := io.ReadAll(r)
	return json.RawMessage(data), err
}
