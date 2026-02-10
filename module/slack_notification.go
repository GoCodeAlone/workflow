package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// SlackNotification sends messages to a Slack webhook URL.
// It implements both the MessageHandler and modular.Module interfaces.
type SlackNotification struct {
	name       string
	webhookURL string
	channel    string
	username   string
	client     *http.Client
	mu         sync.RWMutex
	logger     modular.Logger
}

// slackPayload is the JSON payload sent to Slack webhooks.
type slackPayload struct {
	Channel  string `json:"channel,omitempty"`
	Username string `json:"username,omitempty"`
	Text     string `json:"text"`
}

// NewSlackNotification creates a new Slack notification module.
func NewSlackNotification(name string) *SlackNotification {
	return &SlackNotification{
		name:   name,
		client: &http.Client{},
		logger: &noopLogger{},
	}
}

// Name returns the module name.
func (s *SlackNotification) Name() string {
	return s.name
}

// Init initializes the module with the application context.
func (s *SlackNotification) Init(app modular.Application) error {
	s.logger = app.Logger()
	return nil
}

// ProvidesServices returns the services provided by this module.
func (s *SlackNotification) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        s.name,
			Description: "Slack Notification Handler",
			Instance:    s,
		},
	}
}

// RequiresServices returns the services required by this module.
func (s *SlackNotification) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetClient sets a custom HTTP client (useful for testing).
func (s *SlackNotification) SetClient(client *http.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
}

// SetWebhookURL sets the Slack webhook URL.
func (s *SlackNotification) SetWebhookURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webhookURL = url
}

// SetChannel sets the Slack channel.
func (s *SlackNotification) SetChannel(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channel = channel
}

// SetUsername sets the Slack username.
func (s *SlackNotification) SetUsername(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.username = username
}

// HandleMessage sends a message to the configured Slack webhook.
func (s *SlackNotification) HandleMessage(message []byte) error {
	s.mu.RLock()
	webhookURL := s.webhookURL
	channel := s.channel
	username := s.username
	client := s.client
	s.mu.RUnlock()

	if webhookURL == "" {
		return fmt.Errorf("slack webhook URL not configured")
	}

	payload := slackPayload{
		Channel:  channel,
		Username: username,
		Text:     string(message),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	s.logger.Info("Slack notification sent", "channel", channel)
	return nil
}
