package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewLogger_DefaultWriter(t *testing.T) {
	l := NewLogger(nil)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestLogger_Log(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	ctx := context.Background()
	l.Log(ctx, Event{
		Type:    EventAuth,
		Action:  "login",
		Actor:   "user@example.com",
		Success: true,
		Detail:  "successful login",
	})

	output := buf.String()
	if output == "" {
		t.Fatal("expected output, got empty string")
	}

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &event); err != nil {
		t.Fatalf("failed to parse audit event JSON: %v", err)
	}

	if event.Type != EventAuth {
		t.Errorf("expected type %q, got %q", EventAuth, event.Type)
	}
	if event.Action != "login" {
		t.Errorf("expected action 'login', got %q", event.Action)
	}
	if event.Actor != "user@example.com" {
		t.Errorf("expected actor 'user@example.com', got %q", event.Actor)
	}
	if !event.Success {
		t.Error("expected success=true")
	}
	if event.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestLogger_Log_PreservesTimestamp(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	l.Log(context.Background(), Event{
		Timestamp: ts,
		Type:      EventAdminOp,
		Action:    "delete",
		Success:   true,
	})

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if !event.Timestamp.Equal(ts) {
		t.Errorf("expected timestamp %v, got %v", ts, event.Timestamp)
	}
}

func TestLogger_LogAuth_Success(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogAuth(context.Background(), "admin", "10.0.0.1", true, "password auth")

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventAuth {
		t.Errorf("expected type %q, got %q", EventAuth, event.Type)
	}
	if event.Actor != "admin" {
		t.Errorf("expected actor 'admin', got %q", event.Actor)
	}
	if event.SourceIP != "10.0.0.1" {
		t.Errorf("expected source IP '10.0.0.1', got %q", event.SourceIP)
	}
	if !event.Success {
		t.Error("expected success=true")
	}
}

func TestLogger_LogAuth_Failure(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogAuth(context.Background(), "attacker", "10.0.0.2", false, "bad credentials")

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventAuthFailure {
		t.Errorf("expected type %q, got %q", EventAuthFailure, event.Type)
	}
	if event.Success {
		t.Error("expected success=false")
	}
}

func TestLogger_LogAdminOp(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogAdminOp(context.Background(), "admin", "create_user", "users", "created user bob")

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventAdminOp {
		t.Errorf("expected type %q, got %q", EventAdminOp, event.Type)
	}
	if event.Action != "create_user" {
		t.Errorf("expected action 'create_user', got %q", event.Action)
	}
	if event.Resource != "users" {
		t.Errorf("expected resource 'users', got %q", event.Resource)
	}
}

func TestLogger_LogEscalation(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogEscalation(context.Background(), "user1", "role_change", "promoted to admin", true)

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventEscalation {
		t.Errorf("expected type %q, got %q", EventEscalation, event.Type)
	}
	if event.Detail != "promoted to admin" {
		t.Errorf("expected detail about promotion, got %q", event.Detail)
	}
}

func TestLogger_LogDataAccess(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogDataAccess(context.Background(), "user2", "customer_records", "queried 100 records")

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventDataAccess {
		t.Errorf("expected type %q, got %q", EventDataAccess, event.Type)
	}
	if event.Resource != "customer_records" {
		t.Errorf("expected resource 'customer_records', got %q", event.Resource)
	}
}

func TestLogger_LogConfigChange(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogConfigChange(context.Background(), "admin", "workflow-config", "updated rate limits")

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventConfigChange {
		t.Errorf("expected type %q, got %q", EventConfigChange, event.Type)
	}
	if event.Action != "config_change" {
		t.Errorf("expected action 'config_change', got %q", event.Action)
	}
}

func TestLogger_LogComponentOp(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.LogComponentOp(context.Background(), "admin", "deploy", "my-component", "deployed v2", true)

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Type != EventComponentOp {
		t.Errorf("expected type %q, got %q", EventComponentOp, event.Type)
	}
	if event.Resource != "my-component" {
		t.Errorf("expected resource 'my-component', got %q", event.Resource)
	}
	if !event.Success {
		t.Error("expected success=true")
	}
}

func TestLogger_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)
	ctx := context.Background()

	l.LogAuth(ctx, "user1", "10.0.0.1", true, "login")
	l.LogAdminOp(ctx, "admin", "create", "project", "new project")
	l.LogDataAccess(ctx, "user2", "reports", "exported data")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestLogger_WithMetadata(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Log(context.Background(), Event{
		Type:    EventAuth,
		Action:  "login",
		Actor:   "user@test.com",
		Success: true,
		Metadata: map[string]any{
			"method":     "oauth2",
			"session_id": "abc123",
		},
	})

	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &event); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if event.Metadata["method"] != "oauth2" {
		t.Errorf("expected metadata method 'oauth2', got %v", event.Metadata["method"])
	}
	if event.Metadata["session_id"] != "abc123" {
		t.Errorf("expected metadata session_id 'abc123', got %v", event.Metadata["session_id"])
	}
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)
	ctx := context.Background()

	done := make(chan struct{})
	for i := range 10 {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			l.LogAuth(ctx, "user", "10.0.0.1", true, "concurrent")
			_ = n
		}(i)
	}

	for range 10 {
		<-done
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines from concurrent writes, got %d", len(lines))
	}
}
