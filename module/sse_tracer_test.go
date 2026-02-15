package module

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestTracer() *SSETracer {
	return NewSSETracer(slog.Default())
}

func TestSSESubscribePublish(t *testing.T) {
	tracer := newTestTracer()

	ch, unsub := tracer.Subscribe("exec-1")
	defer unsub()

	event := SSEEvent{
		ID:    "evt-1",
		Event: "step.started",
		Data:  `{"step":"validate"}`,
	}
	tracer.Publish("exec-1", event)

	select {
	case got := <-ch:
		if got.ID != event.ID {
			t.Errorf("expected ID %q, got %q", event.ID, got.ID)
		}
		if got.Event != event.Event {
			t.Errorf("expected Event %q, got %q", event.Event, got.Event)
		}
		if got.Data != event.Data {
			t.Errorf("expected Data %q, got %q", event.Data, got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSSEMultipleSubscribers(t *testing.T) {
	tracer := newTestTracer()

	ch1, unsub1 := tracer.Subscribe("exec-1")
	defer unsub1()

	ch2, unsub2 := tracer.Subscribe("exec-1")
	defer unsub2()

	event := SSEEvent{
		ID:    "evt-2",
		Event: "step.completed",
		Data:  `{"step":"process","duration_ms":42}`,
	}
	tracer.Publish("exec-1", event)

	for i, ch := range []<-chan SSEEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.ID != event.ID {
				t.Errorf("subscriber %d: expected ID %q, got %q", i, event.ID, got.ID)
			}
			if got.Event != event.Event {
				t.Errorf("subscriber %d: expected Event %q, got %q", i, event.Event, got.Event)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestSSEWildcardSubscriber(t *testing.T) {
	tracer := newTestTracer()

	// Subscribe to all events with wildcard
	chAll, unsubAll := tracer.Subscribe("*")
	defer unsubAll()

	// Subscribe to a specific execution
	chSpecific, unsubSpecific := tracer.Subscribe("exec-A")
	defer unsubSpecific()

	// Publish to exec-A — both subscribers should receive
	eventA := SSEEvent{ID: "1", Event: "step.started", Data: `{"exec":"A"}`}
	tracer.Publish("exec-A", eventA)

	// Publish to exec-B — only wildcard should receive
	eventB := SSEEvent{ID: "2", Event: "step.started", Data: `{"exec":"B"}`}
	tracer.Publish("exec-B", eventB)

	// Wildcard should get both events
	received := 0
	timeout := time.After(time.Second)
	for received < 2 {
		select {
		case got := <-chAll:
			received++
			if received == 1 && got.ID != "1" {
				t.Errorf("wildcard: expected first event ID '1', got %q", got.ID)
			}
			if received == 2 && got.ID != "2" {
				t.Errorf("wildcard: expected second event ID '2', got %q", got.ID)
			}
		case <-timeout:
			t.Fatalf("wildcard: timed out after receiving %d events, expected 2", received)
		}
	}

	// Specific should get only exec-A event
	select {
	case got := <-chSpecific:
		if got.ID != "1" {
			t.Errorf("specific: expected event ID '1', got %q", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("specific: timed out waiting for event")
	}

	// Specific should NOT get exec-B
	select {
	case got := <-chSpecific:
		t.Errorf("specific: should not have received event for exec-B, got %+v", got)
	case <-time.After(100 * time.Millisecond):
		// Expected: no event
	}
}

func TestSSEUnsubscribe(t *testing.T) {
	tracer := newTestTracer()

	ch, unsub := tracer.Subscribe("exec-1")

	// Should have 1 active subscriber
	if got := tracer.ActiveSubscribers(); got != 1 {
		t.Errorf("expected 1 active subscriber, got %d", got)
	}

	// Unsubscribe
	unsub()

	// Should have 0 active subscribers
	if got := tracer.ActiveSubscribers(); got != 0 {
		t.Errorf("expected 0 active subscribers after unsub, got %d", got)
	}

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel was not closed after unsubscribe")
	}

	// Double-unsubscribe should be safe
	unsub()
	if got := tracer.ActiveSubscribers(); got != 0 {
		t.Errorf("expected 0 active subscribers after double unsub, got %d", got)
	}

	// Publishing after unsubscribe should not panic
	tracer.Publish("exec-1", SSEEvent{ID: "x", Event: "step.started", Data: "{}"})
}

func TestSSEHandlerHTTP(t *testing.T) {
	tracer := newTestTracer()

	server := httptest.NewServer(tracer.Handler())
	defer server.Close()

	// Build a request URL simulating /api/v1/executions/{id}/stream
	url := server.URL + "/api/v1/executions/exec-42/stream"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify SSE headers
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got %q", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected Connection 'keep-alive', got %q", conn)
	}

	// Wait for the subscriber to be registered
	deadline := time.After(2 * time.Second)
	for tracer.ActiveSubscribers() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for subscriber registration")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Publish an event
	tracer.Publish("exec-42", SSEEvent{
		ID:    "evt-100",
		Event: "step.completed",
		Data:  `{"step":"transform","status":"ok"}`,
	})

	// Read SSE lines from the response body
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			// SSE events end with a blank line
			if line == "" && len(lines) > 1 {
				return
			}
		}
	}()

	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading SSE response")
	}

	// Verify the SSE output format
	found := struct {
		id, event, data bool
	}{}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "id: "):
			found.id = true
			if !strings.Contains(line, "evt-100") {
				t.Errorf("unexpected id line: %q", line)
			}
		case strings.HasPrefix(line, "event: "):
			found.event = true
			if !strings.Contains(line, "step.completed") {
				t.Errorf("unexpected event line: %q", line)
			}
		case strings.HasPrefix(line, "data: "):
			found.data = true
			dataStr := strings.TrimPrefix(line, "data: ")
			var parsed map[string]any
			if err := json.Unmarshal([]byte(dataStr), &parsed); err != nil {
				t.Errorf("data is not valid JSON: %v", err)
			}
			if parsed["step"] != "transform" {
				t.Errorf("expected step 'transform', got %v", parsed["step"])
			}
		}
	}

	if !found.id {
		t.Error("missing 'id:' field in SSE output")
	}
	if !found.event {
		t.Error("missing 'event:' field in SSE output")
	}
	if !found.data {
		t.Error("missing 'data:' field in SSE output")
	}
}

func TestSSEClientDisconnect(t *testing.T) {
	tracer := newTestTracer()

	server := httptest.NewServer(tracer.Handler())
	defer server.Close()

	url := server.URL + "/api/v1/executions/exec-disc/stream"

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Wait for subscriber to be registered
	deadline := time.After(2 * time.Second)
	for tracer.ActiveSubscribers() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for subscriber registration")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if tracer.ActiveSubscribers() != 1 {
		t.Fatalf("expected 1 active subscriber, got %d", tracer.ActiveSubscribers())
	}

	// Simulate client disconnect
	cancel()
	resp.Body.Close()

	// Wait for cleanup
	deadline = time.After(2 * time.Second)
	for tracer.ActiveSubscribers() > 0 {
		select {
		case <-deadline:
			t.Fatalf("subscriber not cleaned up after disconnect, still have %d", tracer.ActiveSubscribers())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSSEConcurrency(t *testing.T) {
	tracer := newTestTracer()

	const (
		numSubscribers      = 20
		numPublishers       = 10
		eventsPerPub        = 50
		totalExpectedPerSub = numPublishers * eventsPerPub
	)

	var wg sync.WaitGroup
	channels := make([]<-chan SSEEvent, numSubscribers)
	unsubs := make([]func(), numSubscribers)

	// Create subscribers
	for i := 0; i < numSubscribers; i++ {
		ch, unsub := tracer.Subscribe("exec-conc")
		channels[i] = ch
		unsubs[i] = unsub
	}
	defer func() {
		for _, unsub := range unsubs {
			unsub()
		}
	}()

	if got := tracer.ActiveSubscribers(); got != numSubscribers {
		t.Fatalf("expected %d active subscribers, got %d", numSubscribers, got)
	}

	// Concurrent publishers
	for p := 0; p < numPublishers; p++ {
		wg.Add(1)
		go func(pub int) {
			defer wg.Done()
			for e := 0; e < eventsPerPub; e++ {
				tracer.Publish("exec-conc", SSEEvent{
					ID:    fmt.Sprintf("pub%d-evt%d", pub, e),
					Event: "step.started",
					Data:  fmt.Sprintf(`{"publisher":%d,"event":%d}`, pub, e),
				})
			}
		}(p)
	}

	// Concurrent subscribers reading events
	counts := make([]int, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case _, ok := <-channels[idx]:
					if !ok {
						return
					}
					counts[idx]++
					if counts[idx] >= totalExpectedPerSub {
						return
					}
				case <-timeout:
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Each subscriber should have received most events (some may be dropped
	// if buffer fills, but we sized it at 64 so with sequential-ish delivery
	// most should arrive).
	for i, count := range counts {
		if count < 1 {
			t.Errorf("subscriber %d received 0 events", i)
		}
	}
}

func TestSSEHandlerMissingID(t *testing.T) {
	tracer := newTestTracer()

	handler := tracer.Handler()
	req := httptest.NewRequest("GET", "/api/v1/executions/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing execution ID, got %d", rec.Code)
	}
}

func TestSSEActiveSubscribers(t *testing.T) {
	tracer := newTestTracer()

	if got := tracer.ActiveSubscribers(); got != 0 {
		t.Errorf("expected 0 active subscribers initially, got %d", got)
	}

	_, unsub1 := tracer.Subscribe("a")
	_, unsub2 := tracer.Subscribe("b")
	_, unsub3 := tracer.Subscribe("*")

	if got := tracer.ActiveSubscribers(); got != 3 {
		t.Errorf("expected 3 active subscribers, got %d", got)
	}

	unsub1()
	if got := tracer.ActiveSubscribers(); got != 2 {
		t.Errorf("expected 2 active subscribers after one unsub, got %d", got)
	}

	unsub2()
	unsub3()
	if got := tracer.ActiveSubscribers(); got != 0 {
		t.Errorf("expected 0 active subscribers after all unsubs, got %d", got)
	}
}

func TestSSENilLogger(t *testing.T) {
	// Should not panic with nil logger
	tracer := NewSSETracer(nil)
	ch, unsub := tracer.Subscribe("test")
	defer unsub()

	tracer.Publish("test", SSEEvent{ID: "1", Event: "test", Data: "{}"})

	select {
	case got := <-ch:
		if got.ID != "1" {
			t.Errorf("expected ID '1', got %q", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestExtractExecutionID(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/v1/executions/abc-123/stream", "abc-123"},
		{"/api/v1/executions/exec-42/stream", "exec-42"},
		{"/api/v1/executions/test/stream/", "test"},
		{"/executions/quick/stream", "quick"},
		{"/api/v1/executions/", ""},
		{"/api/v1/workflows/123", ""},
		{"", ""},
		{"/executions", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractExecutionID(tt.path)
			if got != tt.expected {
				t.Errorf("extractExecutionID(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestSSEPublishNoSubscribers(t *testing.T) {
	tracer := newTestTracer()

	// Should not panic publishing with no subscribers
	tracer.Publish("nonexistent", SSEEvent{
		ID:    "x",
		Event: "step.started",
		Data:  "{}",
	})
}

func TestSSEIsolation(t *testing.T) {
	tracer := newTestTracer()

	chA, unsubA := tracer.Subscribe("exec-A")
	defer unsubA()

	chB, unsubB := tracer.Subscribe("exec-B")
	defer unsubB()

	// Publish to A only
	tracer.Publish("exec-A", SSEEvent{ID: "1", Event: "step.started", Data: `{"for":"A"}`})

	// A should receive
	select {
	case got := <-chA:
		if got.ID != "1" {
			t.Errorf("expected ID '1', got %q", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber A: timed out")
	}

	// B should NOT receive
	select {
	case got := <-chB:
		t.Errorf("subscriber B should not receive exec-A events, got %+v", got)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
