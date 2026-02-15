package module

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// SSEEvent represents a Server-Sent Event for execution tracing.
type SSEEvent struct {
	ID    string `json:"id"`
	Event string `json:"event"` // "step.started", "step.completed", "step.failed", etc.
	Data  string `json:"data"`  // JSON-encoded event data
}

// SSETracer provides Server-Sent Events for live execution tracing.
// Clients subscribe to a specific execution ID (or "*" for all executions)
// and receive real-time events as the workflow progresses.
type SSETracer struct {
	mu          sync.RWMutex
	subscribers map[string][]chan SSEEvent // key: execution_id or "*" for all
	logger      *slog.Logger
	active      atomic.Int64 // count of active subscriber connections
}

// NewSSETracer creates a new SSETracer with the given logger.
func NewSSETracer(logger *slog.Logger) *SSETracer {
	if logger == nil {
		logger = slog.Default()
	}
	return &SSETracer{
		subscribers: make(map[string][]chan SSEEvent),
		logger:      logger,
	}
}

// Subscribe registers a new subscriber for the given execution ID.
// Use "*" as executionID to receive events for all executions.
// Returns a read-only channel of events and an unsubscribe function.
// The caller must invoke the unsubscribe function when done to prevent leaks.
func (t *SSETracer) Subscribe(executionID string) (<-chan SSEEvent, func()) {
	ch := make(chan SSEEvent, 64) // buffered to prevent slow consumers from blocking

	t.mu.Lock()
	t.subscribers[executionID] = append(t.subscribers[executionID], ch)
	t.mu.Unlock()

	t.active.Add(1)

	unsubscribed := sync.Once{}
	unsubscribe := func() {
		unsubscribed.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()

			subs := t.subscribers[executionID]
			for i, sub := range subs {
				if sub == ch {
					t.subscribers[executionID] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
			// Clean up empty slices
			if len(t.subscribers[executionID]) == 0 {
				delete(t.subscribers, executionID)
			}

			close(ch)
			t.active.Add(-1)
		})
	}

	return ch, unsubscribe
}

// Publish sends an event to all subscribers matching the given execution ID.
// Events are delivered to:
//   - subscribers registered for the specific executionID
//   - subscribers registered with the wildcard "*"
//
// If a subscriber's channel is full, the event is dropped for that subscriber
// (non-blocking send to prevent slow consumers from stalling the publisher).
func (t *SSETracer) Publish(executionID string, event SSEEvent) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Send to specific execution subscribers
	for _, ch := range t.subscribers[executionID] {
		select {
		case ch <- event:
		default:
			t.logger.Warn("SSE event dropped for slow subscriber",
				"execution_id", executionID,
				"event", event.Event,
			)
		}
	}

	// Send to wildcard subscribers (if executionID is not already "*")
	if executionID != "*" {
		for _, ch := range t.subscribers["*"] {
			select {
			case ch <- event:
			default:
				t.logger.Warn("SSE event dropped for slow wildcard subscriber",
					"execution_id", executionID,
					"event", event.Event,
				)
			}
		}
	}
}

// Handler returns an HTTP handler for SSE streaming at
// GET /api/v1/executions/{id}/stream.
//
// The handler:
//   - Sets SSE-appropriate headers (Content-Type, Cache-Control, Connection)
//   - Extracts the execution ID from the URL path
//   - Subscribes to events for that execution ID
//   - Writes events in SSE format: "id: ...\nevent: ...\ndata: ...\n\n"
//   - Cleans up on client disconnect (context cancellation)
func (t *SSETracer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract execution ID from path: /api/v1/executions/{id}/stream
		executionID := extractExecutionID(r.URL.Path)
		if executionID == "" {
			http.Error(w, `{"error":"missing execution_id"}`, http.StatusBadRequest)
			return
		}

		// Verify the response writer supports flushing
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Subscribe to events
		ch, unsubscribe := t.Subscribe(executionID)
		defer unsubscribe()

		ctx := r.Context()

		t.logger.Info("SSE client connected",
			"execution_id", executionID,
			"remote_addr", r.RemoteAddr,
		)

		for {
			select {
			case <-ctx.Done():
				t.logger.Info("SSE client disconnected",
					"execution_id", executionID,
					"remote_addr", r.RemoteAddr,
				)
				return
			case event, ok := <-ch:
				if !ok {
					// Channel closed — subscriber was removed
					return
				}
				// Write SSE-formatted event
				if event.ID != "" {
					fmt.Fprintf(w, "id: %s\n", event.ID)
				}
				if event.Event != "" {
					fmt.Fprintf(w, "event: %s\n", event.Event)
				}
				// Encode the data as a single-line JSON payload
				data := event.Data
				if data == "" {
					// If Data is empty, marshal the whole event as data
					b, _ := json.Marshal(event)
					data = string(b)
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

// ActiveSubscribers returns the count of currently active subscriber connections.
func (t *SSETracer) ActiveSubscribers() int {
	return int(t.active.Load())
}

// extractExecutionID parses the execution ID from a path like
// /api/v1/executions/{id}/stream. It returns the {id} segment.
func extractExecutionID(path string) string {
	// Normalize: remove trailing slash
	path = strings.TrimSuffix(path, "/")

	// Look for /executions/{id}/stream or /executions/{id}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "executions" && i+1 < len(parts) {
			id := parts[i+1]
			// Validate that the next part after id is "stream" or that id is the last part
			if i+2 < len(parts) && parts[i+2] == "stream" {
				return id
			}
			// Also support /executions/{id} without /stream suffix
			if i+1 == len(parts)-1 {
				return id
			}
		}
	}
	return ""
}
