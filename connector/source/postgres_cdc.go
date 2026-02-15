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

// PostgresCDCConfig holds the configuration for the PostgreSQL CDC source.
type PostgresCDCConfig struct {
	DSN          string        `json:"dsn" yaml:"dsn"`
	Tables       []string      `json:"tables" yaml:"tables"`
	Channel      string        `json:"channel" yaml:"channel"`             // LISTEN/NOTIFY channel name
	PollInterval time.Duration `json:"poll_interval" yaml:"poll_interval"` // fallback polling interval
}

// ChangeEvent represents a single row-level change from PostgreSQL.
type ChangeEvent struct {
	Table     string          `json:"table"`
	Operation string          `json:"operation"` // insert, update, delete
	Data      json.RawMessage `json:"data"`
	OldData   json.RawMessage `json:"old_data,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// PGListener abstracts the PostgreSQL LISTEN/NOTIFY mechanism for testability.
// In production, this would wrap pgx.Conn; in tests, a mock implementation is used.
type PGListener interface {
	// Connect establishes the database connection.
	Connect(ctx context.Context, dsn string) error
	// Listen starts listening on the given channel.
	Listen(ctx context.Context, channel string) error
	// WaitForNotification blocks until a notification arrives or context is cancelled.
	WaitForNotification(ctx context.Context) (payload string, err error)
	// Close closes the connection.
	Close(ctx context.Context) error
}

// PostgresCDCSource is an EventSource that watches PostgreSQL tables for changes
// using LISTEN/NOTIFY and optional polling. It converts row-level changes into
// CloudEvents-compatible Event structs.
type PostgresCDCSource struct {
	name     string
	config   PostgresCDCConfig
	listener PGListener
	logger   *slog.Logger
	cancel   context.CancelFunc
	done     chan struct{}
	healthy  atomic.Bool
	mu       sync.Mutex
}

// NewPostgresCDCSource creates a new PostgresCDCSource from a config map.
// Supported config keys: dsn, tables, channel, poll_interval.
func NewPostgresCDCSource(name string, config map[string]any) (*PostgresCDCSource, error) {
	cfg, err := parsePostgresCDCConfig(config)
	if err != nil {
		return nil, fmt.Errorf("postgres.cdc source %q: %w", name, err)
	}

	return &PostgresCDCSource{
		name:   name,
		config: cfg,
		logger: slog.Default().With("connector", "postgres.cdc", "name", name),
	}, nil
}

// NewPostgresCDCSourceWithListener creates a PostgresCDCSource with a custom PGListener.
// This is primarily used for testing with mock listeners.
func NewPostgresCDCSourceWithListener(name string, config map[string]any, listener PGListener) (*PostgresCDCSource, error) {
	src, err := NewPostgresCDCSource(name, config)
	if err != nil {
		return nil, err
	}
	src.listener = listener
	return src, nil
}

// Name returns the connector instance name.
func (s *PostgresCDCSource) Name() string { return s.name }

// Type returns the connector type identifier.
func (s *PostgresCDCSource) Type() string { return "postgres.cdc" }

// Start begins listening for PostgreSQL changes and writing events to the output channel.
func (s *PostgresCDCSource) Start(ctx context.Context, output chan<- connector.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return fmt.Errorf("postgres.cdc source %q: no PGListener configured (set via NewPostgresCDCSourceWithListener or inject before Start)", s.name)
	}

	if err := s.listener.Connect(ctx, s.config.DSN); err != nil {
		return fmt.Errorf("postgres.cdc source %q: connect: %w", s.name, err)
	}

	channel := s.config.Channel
	if channel == "" {
		channel = "cdc_changes"
	}

	if err := s.listener.Listen(ctx, channel); err != nil {
		_ = s.listener.Close(ctx)
		return fmt.Errorf("postgres.cdc source %q: listen on %q: %w", s.name, channel, err)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.healthy.Store(true)

	go s.listenLoop(loopCtx, output)

	s.logger.Info("started", "channel", channel, "tables", s.config.Tables)
	return nil
}

// Stop gracefully shuts down the CDC listener.
func (s *PostgresCDCSource) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthy.Store(false)

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}

	// Wait for the listen loop to finish.
	if s.done != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if s.listener != nil {
		return s.listener.Close(ctx)
	}

	return nil
}

// Healthy returns true when the source is connected and listening.
func (s *PostgresCDCSource) Healthy() bool {
	return s.healthy.Load()
}

// Checkpoint is a no-op for PostgreSQL LISTEN/NOTIFY (stateless notifications).
func (s *PostgresCDCSource) Checkpoint(_ context.Context) error {
	return nil
}

// listenLoop continuously waits for notifications and converts them to events.
func (s *PostgresCDCSource) listenLoop(ctx context.Context, output chan<- connector.Event) {
	defer close(s.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		payload, err := s.listener.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled, normal shutdown
			}
			s.logger.Error("notification error", "error", err)
			s.healthy.Store(false)

			// Back off before retrying.
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		event, err := s.parseNotification(payload)
		if err != nil {
			s.logger.Warn("failed to parse notification", "error", err, "payload", payload)
			continue
		}

		select {
		case output <- event:
		case <-ctx.Done():
			return
		}
	}
}

// parseNotification converts a NOTIFY payload string into a CloudEvents Event.
// Expected payload format is JSON: {"table":"users","operation":"insert","data":{...}}
func (s *PostgresCDCSource) parseNotification(payload string) (connector.Event, error) {
	var change ChangeEvent
	if err := json.Unmarshal([]byte(payload), &change); err != nil {
		return connector.Event{}, fmt.Errorf("unmarshal change event: %w", err)
	}

	// Determine the event type: postgres.row.insert, postgres.row.update, postgres.row.delete
	eventType := "postgres.row." + change.Operation

	// Build the data payload including old_data for updates/deletes.
	eventData, err := json.Marshal(change)
	if err != nil {
		return connector.Event{}, fmt.Errorf("marshal event data: %w", err)
	}

	ts := change.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	return connector.Event{
		ID:              uuid.New().String(),
		Source:          "postgres.cdc/" + s.name,
		Type:            eventType,
		Subject:         change.Table,
		Time:            ts,
		Data:            eventData,
		DataContentType: "application/json",
	}, nil
}

// parsePostgresCDCConfig extracts PostgresCDCConfig from a generic map.
func parsePostgresCDCConfig(config map[string]any) (PostgresCDCConfig, error) {
	cfg := PostgresCDCConfig{
		Channel:      "cdc_changes",
		PollInterval: 5 * time.Second,
	}

	if dsn, ok := config["dsn"].(string); ok {
		cfg.DSN = dsn
	}
	// DSN is not required at parse time (may be injected later or use mock)

	if tables, ok := config["tables"].([]any); ok {
		for _, t := range tables {
			if s, ok := t.(string); ok {
				cfg.Tables = append(cfg.Tables, s)
			}
		}
	}
	if tables, ok := config["tables"].([]string); ok {
		cfg.Tables = tables
	}

	if ch, ok := config["channel"].(string); ok && ch != "" {
		cfg.Channel = ch
	}

	if pi, ok := config["poll_interval"].(string); ok {
		d, err := time.ParseDuration(pi)
		if err != nil {
			return cfg, fmt.Errorf("invalid poll_interval %q: %w", pi, err)
		}
		cfg.PollInterval = d
	}

	return cfg, nil
}

// PostgresCDCSourceFactory creates PostgresCDCSource instances from config maps.
// Note: The returned source requires a PGListener to be injected before Start().
func PostgresCDCSourceFactory(name string, config map[string]any) (connector.EventSource, error) {
	return NewPostgresCDCSource(name, config)
}

// NewPostgresCDCSourceFactory returns a SourceFactory for PostgreSQL CDC.
func NewPostgresCDCSourceFactory() connector.SourceFactory {
	return PostgresCDCSourceFactory
}
