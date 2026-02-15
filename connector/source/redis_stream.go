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

// RedisStreamConfig holds the configuration for the Redis Streams source.
type RedisStreamConfig struct {
	Addr      string `json:"addr" yaml:"addr"`
	Stream    string `json:"stream" yaml:"stream"`
	Group     string `json:"group" yaml:"group"`       // consumer group name
	Consumer  string `json:"consumer" yaml:"consumer"` // consumer instance name
	BatchSize int    `json:"batch_size" yaml:"batch_size"`
}

// RedisStreamMessage represents a single message read from a Redis Stream.
type RedisStreamMessage struct {
	ID     string            // stream message ID (e.g., "1645000000000-0")
	Fields map[string]string // key-value pairs from the stream entry
}

// RedisStreamClient abstracts the Redis Streams API for testability.
// In production, this would wrap go-redis; in tests, a mock implementation is used.
type RedisStreamClient interface {
	// Connect establishes the Redis connection.
	Connect(ctx context.Context, addr string) error
	// CreateGroup creates a consumer group if it does not already exist.
	// The "startID" parameter is typically "$" (latest) or "0" (all history).
	CreateGroup(ctx context.Context, stream, group, startID string) error
	// ReadGroup reads messages from the stream using the consumer group.
	// Returns up to count messages; blocks until messages are available or ctx is cancelled.
	ReadGroup(ctx context.Context, stream, group, consumer string, count int) ([]RedisStreamMessage, error)
	// Ack acknowledges processing of the given message IDs.
	Ack(ctx context.Context, stream, group string, ids ...string) error
	// Close closes the Redis connection.
	Close() error
}

// RedisStreamSource is an EventSource that reads from a Redis Stream
// using consumer groups. Each message is converted to a CloudEvents Event.
type RedisStreamSource struct {
	name    string
	config  RedisStreamConfig
	client  RedisStreamClient
	logger  *slog.Logger
	cancel  context.CancelFunc
	done    chan struct{}
	healthy atomic.Bool
	mu      sync.Mutex
}

// NewRedisStreamSource creates a new RedisStreamSource from a config map.
// Supported config keys: addr, stream, group, consumer, batch_size.
func NewRedisStreamSource(name string, config map[string]any) (*RedisStreamSource, error) {
	cfg, err := parseRedisStreamConfig(config)
	if err != nil {
		return nil, fmt.Errorf("redis.stream source %q: %w", name, err)
	}

	return &RedisStreamSource{
		name:   name,
		config: cfg,
		logger: slog.Default().With("connector", "redis.stream", "name", name),
	}, nil
}

// NewRedisStreamSourceWithClient creates a RedisStreamSource with a custom client.
// This is primarily used for testing with mock clients.
func NewRedisStreamSourceWithClient(name string, config map[string]any, client RedisStreamClient) (*RedisStreamSource, error) {
	src, err := NewRedisStreamSource(name, config)
	if err != nil {
		return nil, err
	}
	src.client = client
	return src, nil
}

// Name returns the connector instance name.
func (s *RedisStreamSource) Name() string { return s.name }

// Type returns the connector type identifier.
func (s *RedisStreamSource) Type() string { return "redis.stream" }

// Start begins reading from the Redis Stream and writing events to the output channel.
func (s *RedisStreamSource) Start(ctx context.Context, output chan<- connector.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client == nil {
		return fmt.Errorf("redis.stream source %q: no RedisStreamClient configured (set via NewRedisStreamSourceWithClient)", s.name)
	}

	if err := s.client.Connect(ctx, s.config.Addr); err != nil {
		return fmt.Errorf("redis.stream source %q: connect to %s: %w", s.name, s.config.Addr, err)
	}

	// Create consumer group (idempotent; ignores "already exists" errors).
	if s.config.Group != "" {
		if err := s.client.CreateGroup(ctx, s.config.Stream, s.config.Group, "0"); err != nil {
			s.logger.Warn("create consumer group", "error", err, "group", s.config.Group)
			// Non-fatal: group may already exist.
		}
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.healthy.Store(true)

	go s.readLoop(loopCtx, output)

	s.logger.Info("started", "stream", s.config.Stream, "group", s.config.Group, "consumer", s.config.Consumer)
	return nil
}

// Stop gracefully shuts down the Redis Stream reader.
func (s *RedisStreamSource) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthy.Store(false)

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}

	if s.done != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if s.client != nil {
		return s.client.Close()
	}

	return nil
}

// Healthy returns true when the source is connected and reading.
func (s *RedisStreamSource) Healthy() bool {
	return s.healthy.Load()
}

// Checkpoint is a no-op; Redis Streams tracks position via consumer groups.
func (s *RedisStreamSource) Checkpoint(_ context.Context) error {
	return nil
}

// readLoop continuously reads messages from the Redis Stream.
func (s *RedisStreamSource) readLoop(ctx context.Context, output chan<- connector.Event) {
	defer close(s.done)

	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		messages, err := s.client.ReadGroup(ctx, s.config.Stream, s.config.Group, s.config.Consumer, batchSize)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Error("read error", "error", err)
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

			// Acknowledge the message after successful delivery to the output channel.
			if err := s.client.Ack(ctx, s.config.Stream, s.config.Group, msg.ID); err != nil {
				s.logger.Warn("ack failed", "error", err, "message_id", msg.ID)
			}
		}
	}
}

// messageToEvent converts a Redis Stream message to a CloudEvents Event.
func (s *RedisStreamSource) messageToEvent(msg RedisStreamMessage) connector.Event {
	// Marshal the fields map as the event data.
	data, _ := json.Marshal(msg.Fields)

	// Determine event type from message fields or use default.
	eventType := "redis.stream.message"
	if t, ok := msg.Fields["type"]; ok && t != "" {
		eventType = t
	}

	// Use stream message ID as subject for traceability.
	subject := s.config.Stream + "/" + msg.ID

	return connector.Event{
		ID:              uuid.New().String(),
		Source:          "redis.stream/" + s.name,
		Type:            eventType,
		Subject:         subject,
		Time:            time.Now().UTC(),
		Data:            data,
		DataContentType: "application/json",
	}
}

// parseRedisStreamConfig extracts RedisStreamConfig from a generic map.
func parseRedisStreamConfig(config map[string]any) (RedisStreamConfig, error) {
	cfg := RedisStreamConfig{
		Addr:      "localhost:6379",
		BatchSize: 10,
	}

	if addr, ok := config["addr"].(string); ok && addr != "" {
		cfg.Addr = addr
	}

	if stream, ok := config["stream"].(string); ok && stream != "" {
		cfg.Stream = stream
	} else {
		return cfg, fmt.Errorf("stream is required")
	}

	if group, ok := config["group"].(string); ok {
		cfg.Group = group
	}

	if consumer, ok := config["consumer"].(string); ok {
		cfg.Consumer = consumer
	}

	if bs, ok := config["batch_size"].(int); ok && bs > 0 {
		cfg.BatchSize = bs
	}
	if bs, ok := config["batch_size"].(float64); ok && bs > 0 {
		cfg.BatchSize = int(bs)
	}

	return cfg, nil
}

// RedisStreamSourceFactory creates RedisStreamSource instances from config maps.
// Note: The returned source requires a RedisStreamClient to be injected before Start().
func RedisStreamSourceFactory(name string, config map[string]any) (connector.EventSource, error) {
	return NewRedisStreamSource(name, config)
}

// NewRedisStreamSourceFactory returns a SourceFactory for Redis Streams.
func NewRedisStreamSourceFactory() connector.SourceFactory {
	return RedisStreamSourceFactory
}
