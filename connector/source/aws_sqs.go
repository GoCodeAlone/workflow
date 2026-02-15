package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GoCodeAlone/workflow/connector"
	"github.com/google/uuid"
)

// SQSConfig holds the configuration for the AWS SQS source and sink.
type SQSConfig struct {
	QueueURL        string `json:"queue_url" yaml:"queue_url"`
	Region          string `json:"region" yaml:"region"`
	MaxMessages     int    `json:"max_messages" yaml:"max_messages"`
	WaitTimeSeconds int    `json:"wait_time_seconds" yaml:"wait_time_seconds"`
}

// SQSMessage represents a single message received from SQS.
type SQSMessage struct {
	MessageID     string
	ReceiptHandle string
	Body          string
	Attributes    map[string]string
}

// SQSClient abstracts the AWS SQS API for testability.
// In production, this would wrap the AWS SDK v2 SQS client; in tests, a mock is used.
type SQSClient interface {
	// ReceiveMessages polls the SQS queue for up to maxMessages.
	ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitTimeSeconds int) ([]SQSMessage, error)
	// DeleteMessage removes a message from the queue after successful processing.
	DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error
	// SendMessage sends a message to the SQS queue.
	SendMessage(ctx context.Context, queueURL, body string, attributes map[string]string) (messageID string, err error)
	// SendMessageBatch sends multiple messages to the SQS queue.
	SendMessageBatch(ctx context.Context, queueURL string, entries []SQSBatchEntry) ([]SQSBatchResult, error)
}

// SQSBatchEntry represents a single entry in a SendMessageBatch call.
type SQSBatchEntry struct {
	ID         string
	Body       string
	Attributes map[string]string
}

// SQSBatchResult represents the result of a single entry in a SendMessageBatch call.
type SQSBatchResult struct {
	ID        string
	MessageID string
	Error     error
}

// ---------------------------------------------------------------------------
// SQS Source
// ---------------------------------------------------------------------------

// SQSSource is an EventSource that polls an AWS SQS queue and emits
// CloudEvents-compatible events for each message received.
type SQSSource struct {
	name    string
	config  SQSConfig
	client  SQSClient
	logger  *slog.Logger
	cancel  context.CancelFunc
	done    chan struct{}
	healthy atomic.Bool
	mu      sync.Mutex
}

// NewSQSSource creates a new SQSSource from a config map.
// Supported config keys: queue_url, region, max_messages, wait_time_seconds.
func NewSQSSource(name string, config map[string]any) (*SQSSource, error) {
	cfg, err := parseSQSConfig(config)
	if err != nil {
		return nil, fmt.Errorf("sqs source %q: %w", name, err)
	}

	return &SQSSource{
		name:   name,
		config: cfg,
		logger: slog.Default().With("connector", "sqs", "role", "source", "name", name),
	}, nil
}

// NewSQSSourceWithClient creates an SQSSource with a custom SQS client.
// This is primarily used for testing with mock clients.
func NewSQSSourceWithClient(name string, config map[string]any, client SQSClient) (*SQSSource, error) {
	src, err := NewSQSSource(name, config)
	if err != nil {
		return nil, err
	}
	src.client = client
	return src, nil
}

// Name returns the connector instance name.
func (s *SQSSource) Name() string { return s.name }

// Type returns the connector type identifier.
func (s *SQSSource) Type() string { return "sqs" }

// Start begins polling the SQS queue and writing events to the output channel.
func (s *SQSSource) Start(ctx context.Context, output chan<- connector.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client == nil {
		return fmt.Errorf("sqs source %q: no SQSClient configured (set via NewSQSSourceWithClient)", s.name)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.healthy.Store(true)

	go s.pollLoop(loopCtx, output)

	s.logger.Info("started", "queue_url", s.config.QueueURL, "region", s.config.Region)
	return nil
}

// Stop gracefully shuts down the SQS poller.
func (s *SQSSource) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthy.Store(false)

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}

	if s.done != nil {
		<-s.done
	}

	return nil
}

// Healthy returns true when the source is polling.
func (s *SQSSource) Healthy() bool {
	return s.healthy.Load()
}

// Checkpoint is a no-op for SQS (messages are deleted after processing).
func (s *SQSSource) Checkpoint(_ context.Context) error {
	return nil
}

// pollLoop continuously long-polls SQS for messages.
func (s *SQSSource) pollLoop(ctx context.Context, output chan<- connector.Event) {
	defer close(s.done)

	maxMessages := s.config.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 10
	}
	waitTime := s.config.WaitTimeSeconds
	if waitTime <= 0 {
		waitTime = 20
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		messages, err := s.client.ReceiveMessages(ctx, s.config.QueueURL, maxMessages, waitTime)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Error("receive error", "error", err)
			s.healthy.Store(false)

			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			s.healthy.Store(true)
			continue
		}

		for _, msg := range messages {
			event := s.messageToEvent(msg)

			select {
			case output <- event:
			case <-ctx.Done():
				return
			}

			// Delete the message after delivering to the output channel.
			if err := s.client.DeleteMessage(ctx, s.config.QueueURL, msg.ReceiptHandle); err != nil {
				s.logger.Warn("delete message failed", "error", err, "message_id", msg.MessageID)
			}
		}
	}
}

// messageToEvent converts an SQS message to a CloudEvents Event.
func (s *SQSSource) messageToEvent(msg SQSMessage) connector.Event {
	// Attempt to use the body directly as JSON; if it fails, wrap as string.
	var data json.RawMessage
	if json.Valid([]byte(msg.Body)) {
		data = json.RawMessage(msg.Body)
	} else {
		data, _ = json.Marshal(msg.Body)
	}

	// Determine event type from message attributes or use default.
	eventType := "sqs.message"
	if t, ok := msg.Attributes["event_type"]; ok && t != "" {
		eventType = t
	}

	return connector.Event{
		ID:              uuid.New().String(),
		Source:          "sqs/" + s.name,
		Type:            eventType,
		Subject:         msg.MessageID,
		Time:            time.Now().UTC(),
		Data:            data,
		DataContentType: "application/json",
	}
}

// ---------------------------------------------------------------------------
// SQS Sink
// ---------------------------------------------------------------------------

// SQSSink is an EventSink that delivers events to an AWS SQS queue.
type SQSSink struct {
	name    string
	config  SQSConfig
	client  SQSClient
	logger  *slog.Logger
	healthy atomic.Bool
}

// NewSQSSink creates a new SQSSink from a config map.
// Supported config keys: queue_url, region.
func NewSQSSink(name string, config map[string]any) (*SQSSink, error) {
	cfg, err := parseSQSConfig(config)
	if err != nil {
		return nil, fmt.Errorf("sqs sink %q: %w", name, err)
	}

	sink := &SQSSink{
		name:   name,
		config: cfg,
		logger: slog.Default().With("connector", "sqs", "role", "sink", "name", name),
	}
	sink.healthy.Store(true)
	return sink, nil
}

// NewSQSSinkWithClient creates an SQSSink with a custom SQS client.
// This is primarily used for testing with mock clients.
func NewSQSSinkWithClient(name string, config map[string]any, client SQSClient) (*SQSSink, error) {
	sink, err := NewSQSSink(name, config)
	if err != nil {
		return nil, err
	}
	sink.client = client
	return sink, nil
}

// Name returns the connector instance name.
func (s *SQSSink) Name() string { return s.name }

// Type returns the connector type identifier.
func (s *SQSSink) Type() string { return "sqs" }

// Deliver sends a single event to the SQS queue.
func (s *SQSSink) Deliver(ctx context.Context, event connector.Event) error {
	if s.client == nil {
		return fmt.Errorf("sqs sink %q: no SQSClient configured", s.name)
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sqs sink %q: marshal event: %w", s.name, err)
	}

	attrs := map[string]string{
		"event_type":   event.Type,
		"event_source": event.Source,
	}
	if event.Subject != "" {
		attrs["event_subject"] = event.Subject
	}

	_, err = s.client.SendMessage(ctx, s.config.QueueURL, string(body), attrs)
	if err != nil {
		return fmt.Errorf("sqs sink %q: send: %w", s.name, err)
	}

	return nil
}

// DeliverBatch sends multiple events to the SQS queue. Returns per-event errors.
func (s *SQSSink) DeliverBatch(ctx context.Context, events []connector.Event) []error {
	errs := make([]error, len(events))

	if s.client == nil {
		for i := range errs {
			errs[i] = fmt.Errorf("sqs sink %q: no SQSClient configured", s.name)
		}
		return errs
	}

	// Build batch entries.
	entries := make([]SQSBatchEntry, len(events))
	for i := range events {
		body, err := json.Marshal(events[i])
		if err != nil {
			errs[i] = fmt.Errorf("marshal event %d: %w", i, err)
			continue
		}

		attrs := map[string]string{
			"event_type":   events[i].Type,
			"event_source": events[i].Source,
		}

		entries[i] = SQSBatchEntry{
			ID:         fmt.Sprintf("entry-%d", i),
			Body:       string(body),
			Attributes: attrs,
		}
	}

	results, err := s.client.SendMessageBatch(ctx, s.config.QueueURL, entries)
	if err != nil {
		// Wholesale failure: all events failed.
		for i := range errs {
			if errs[i] == nil { // don't overwrite marshal errors
				errs[i] = fmt.Errorf("sqs sink %q: batch send: %w", s.name, err)
			}
		}
		return errs
	}

	// Map results back to error slots by entry index.
	resultMap := make(map[string]error)
	for _, r := range results {
		if r.Error != nil {
			resultMap[r.ID] = r.Error
		}
	}

	for i := range events {
		entryID := fmt.Sprintf("entry-%d", i)
		if e, ok := resultMap[entryID]; ok {
			errs[i] = e
		}
	}

	return errs
}

// Stop marks the sink as unhealthy.
func (s *SQSSink) Stop(_ context.Context) error {
	s.healthy.Store(false)
	return nil
}

// Healthy returns true when the sink is operational.
func (s *SQSSink) Healthy() bool {
	return s.healthy.Load()
}

// ---------------------------------------------------------------------------
// Config parsing
// ---------------------------------------------------------------------------

// parseSQSConfig extracts SQSConfig from a generic map.
func parseSQSConfig(config map[string]any) (SQSConfig, error) {
	cfg := SQSConfig{
		MaxMessages:     10,
		WaitTimeSeconds: 20,
	}

	if qURL, ok := config["queue_url"].(string); ok && qURL != "" {
		cfg.QueueURL = qURL
	} else {
		return cfg, fmt.Errorf("queue_url is required")
	}

	if region, ok := config["region"].(string); ok {
		cfg.Region = region
	}

	if mm, ok := config["max_messages"].(int); ok && mm > 0 {
		cfg.MaxMessages = mm
	}
	if mm, ok := config["max_messages"].(float64); ok && mm > 0 {
		cfg.MaxMessages = int(mm)
	}

	if wt, ok := config["wait_time_seconds"].(int); ok && wt >= 0 {
		cfg.WaitTimeSeconds = wt
	}
	if wt, ok := config["wait_time_seconds"].(float64); ok && wt >= 0 {
		cfg.WaitTimeSeconds = int(wt)
	}

	return cfg, nil
}

// ---------------------------------------------------------------------------
// Factories
// ---------------------------------------------------------------------------

// SQSSourceFactory creates SQSSource instances from config maps.
// Note: The returned source requires an SQSClient to be injected before Start().
func SQSSourceFactory(name string, config map[string]any) (connector.EventSource, error) {
	return NewSQSSource(name, config)
}

// SQSSinkFactory creates SQSSink instances from config maps.
// Note: The returned sink requires an SQSClient to be injected before Deliver().
func SQSSinkFactory(name string, config map[string]any) (connector.EventSink, error) {
	return NewSQSSink(name, config)
}

// NewSQSSourceFactory returns a SourceFactory for AWS SQS.
func NewSQSSourceFactory() connector.SourceFactory {
	return SQSSourceFactory
}

// NewSQSSinkFactory returns a SinkFactory for AWS SQS.
func NewSQSSinkFactory() connector.SinkFactory {
	return SQSSinkFactory
}
