package module

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackNotificationName(t *testing.T) {
	s := NewSlackNotification("slack-test")
	if s.Name() != "slack-test" {
		t.Errorf("expected name 'slack-test', got %q", s.Name())
	}
}

func TestSlackNotificationModuleInterface(t *testing.T) {
	s := NewSlackNotification("slack-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := s.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := s.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "slack-test" {
		t.Errorf("expected service name 'slack-test', got %q", services[0].Name)
	}

	// Test RequiresServices
	deps := s.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestSlackNotificationHandleMessageNoWebhook(t *testing.T) {
	s := NewSlackNotification("slack-test")
	err := s.HandleMessage([]byte("hello"))
	if err == nil {
		t.Fatal("expected error when webhook URL is not configured")
	}
}

func TestSlackNotificationHandleMessage(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := NewSlackNotification("slack-test")
	s.SetWebhookURL(server.URL)
	s.SetChannel("#test")
	s.SetUsername("test-bot")
	s.SetClient(server.Client())

	app, _ := NewTestApplication()
	_ = s.Init(app)

	err := s.HandleMessage([]byte("test notification"))
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Verify the payload
	var payload slackPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.Text != "test notification" {
		t.Errorf("expected text 'test notification', got %q", payload.Text)
	}
	if payload.Channel != "#test" {
		t.Errorf("expected channel '#test', got %q", payload.Channel)
	}
	if payload.Username != "test-bot" {
		t.Errorf("expected username 'test-bot', got %q", payload.Username)
	}
}

func TestSlackNotificationHandleMessageServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := NewSlackNotification("slack-test")
	s.SetWebhookURL(server.URL)
	s.SetClient(server.Client())

	err := s.HandleMessage([]byte("test"))
	if err == nil {
		t.Fatal("expected error on server error response")
	}
}
