package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// EventType classifies security-relevant audit events.
type EventType string

const (
	EventAuth         EventType = "auth"
	EventAuthFailure  EventType = "auth_failure"
	EventAdminOp      EventType = "admin_op"
	EventEscalation   EventType = "escalation"
	EventDataAccess   EventType = "data_access"
	EventConfigChange EventType = "config_change"
	EventComponentOp  EventType = "component_op"
)

// Event is a single audit log entry.
type Event struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      EventType      `json:"type"`
	Action    string         `json:"action"`
	Actor     string         `json:"actor,omitempty"`
	Resource  string         `json:"resource,omitempty"`
	Detail    string         `json:"detail,omitempty"`
	SourceIP  string         `json:"source_ip,omitempty"`
	Success   bool           `json:"success"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Logger records security-relevant audit events as structured JSON.
type Logger struct {
	mu     sync.Mutex
	writer io.Writer
	slog   *slog.Logger
}

// NewLogger creates an AuditLogger that writes JSON events to the given writer.
// If w is nil, it defaults to os.Stdout.
func NewLogger(w io.Writer) *Logger {
	if w == nil {
		w = os.Stdout
	}
	return &Logger{
		writer: w,
		slog:   slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

// Log records an audit event. It is safe for concurrent use.
func (l *Logger) Log(_ context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		l.slog.Error("failed to marshal audit event", "error", err)
		return
	}

	// Write one JSON line per event
	data = append(data, '\n')
	if _, err := l.writer.Write(data); err != nil {
		l.slog.Error("failed to write audit event", "error", err)
	}
}

// LogAuth records an authentication event.
func (l *Logger) LogAuth(ctx context.Context, actor, sourceIP string, success bool, detail string) {
	eventType := EventAuth
	if !success {
		eventType = EventAuthFailure
	}
	l.Log(ctx, Event{
		Type:     eventType,
		Action:   "authenticate",
		Actor:    actor,
		SourceIP: sourceIP,
		Success:  success,
		Detail:   detail,
	})
}

// LogAdminOp records an administrative operation.
func (l *Logger) LogAdminOp(ctx context.Context, actor, action, resource, detail string) {
	l.Log(ctx, Event{
		Type:     EventAdminOp,
		Action:   action,
		Actor:    actor,
		Resource: resource,
		Success:  true,
		Detail:   detail,
	})
}

// LogEscalation records a privilege escalation event.
func (l *Logger) LogEscalation(ctx context.Context, actor, action, detail string, success bool) {
	l.Log(ctx, Event{
		Type:    EventEscalation,
		Action:  action,
		Actor:   actor,
		Success: success,
		Detail:  detail,
	})
}

// LogDataAccess records a data access event.
func (l *Logger) LogDataAccess(ctx context.Context, actor, resource, detail string) {
	l.Log(ctx, Event{
		Type:     EventDataAccess,
		Action:   "access",
		Actor:    actor,
		Resource: resource,
		Success:  true,
		Detail:   detail,
	})
}

// LogConfigChange records a configuration change event.
func (l *Logger) LogConfigChange(ctx context.Context, actor, resource, detail string) {
	l.Log(ctx, Event{
		Type:     EventConfigChange,
		Action:   "config_change",
		Actor:    actor,
		Resource: resource,
		Success:  true,
		Detail:   detail,
	})
}

// LogComponentOp records a dynamic component lifecycle operation.
func (l *Logger) LogComponentOp(ctx context.Context, actor, action, componentID, detail string, success bool) {
	l.Log(ctx, Event{
		Type:     EventComponentOp,
		Action:   action,
		Actor:    actor,
		Resource: componentID,
		Success:  success,
		Detail:   detail,
	})
}
