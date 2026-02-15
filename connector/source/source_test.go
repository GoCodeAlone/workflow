package source

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/connector"
)

// ---------------------------------------------------------------------------
// Mock PGListener
// ---------------------------------------------------------------------------

type mockPGListener struct {
	connected     bool
	listening     bool
	channel       string
	notifications chan string
	mu            sync.Mutex
	connectErr    error
	listenErr     error
}

func newMockPGListener() *mockPGListener {
	return &mockPGListener{
		notifications: make(chan string, 100),
	}
}

func (m *mockPGListener) Connect(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockPGListener) Listen(_ context.Context, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listenErr != nil {
		return m.listenErr
	}
	m.channel = channel
	m.listening = true
	return nil
}

func (m *mockPGListener) WaitForNotification(ctx context.Context) (string, error) {
	select {
	case payload := <-m.notifications:
		return payload, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (m *mockPGListener) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.listening = false
	return nil
}

func (m *mockPGListener) sendNotification(payload string) {
	m.notifications <- payload
}

// ---------------------------------------------------------------------------
// Mock RedisStreamClient
// ---------------------------------------------------------------------------

type mockRedisStreamClient struct {
	connected  bool
	messages   chan []RedisStreamMessage
	ackedIDs   []string
	mu         sync.Mutex
	connectErr error
}

func newMockRedisStreamClient() *mockRedisStreamClient {
	return &mockRedisStreamClient{
		messages: make(chan []RedisStreamMessage, 100),
	}
}

func (m *mockRedisStreamClient) Connect(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockRedisStreamClient) CreateGroup(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockRedisStreamClient) ReadGroup(ctx context.Context, _, _, _ string, _ int) ([]RedisStreamMessage, error) {
	select {
	case msgs := <-m.messages:
		return msgs, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockRedisStreamClient) Ack(_ context.Context, _, _ string, ids ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ackedIDs = append(m.ackedIDs, ids...)
	return nil
}

func (m *mockRedisStreamClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *mockRedisStreamClient) sendMessages(msgs []RedisStreamMessage) {
	m.messages <- msgs
}

// ---------------------------------------------------------------------------
// Mock SQSClient
// ---------------------------------------------------------------------------

type mockSQSClient struct {
	messages       chan []SQSMessage
	sentMessages   []string
	deletedHandles []string
	batchResults   []SQSBatchResult
	batchErr       error
	mu             sync.Mutex
	sendCount      atomic.Int32
}

func newMockSQSClient() *mockSQSClient {
	return &mockSQSClient{
		messages: make(chan []SQSMessage, 100),
	}
}

func (m *mockSQSClient) ReceiveMessages(ctx context.Context, _ string, _, _ int) ([]SQSMessage, error) {
	select {
	case msgs := <-m.messages:
		return msgs, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockSQSClient) DeleteMessage(_ context.Context, _, receiptHandle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedHandles = append(m.deletedHandles, receiptHandle)
	return nil
}

func (m *mockSQSClient) SendMessage(_ context.Context, _, body string, _ map[string]string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, body)
	count := m.sendCount.Add(1)
	return fmt.Sprintf("msg-%d", count), nil
}

func (m *mockSQSClient) SendMessageBatch(_ context.Context, _ string, entries []SQSBatchEntry) ([]SQSBatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.batchErr != nil {
		return nil, m.batchErr
	}

	if m.batchResults != nil {
		return m.batchResults, nil
	}

	// Default: all succeed
	results := make([]SQSBatchResult, len(entries))
	for i, e := range entries {
		m.sentMessages = append(m.sentMessages, e.Body)
		results[i] = SQSBatchResult{
			ID:        e.ID,
			MessageID: fmt.Sprintf("batch-msg-%d", i),
		}
	}
	return results, nil
}

func (m *mockSQSClient) sendMessages(msgs []SQSMessage) {
	m.messages <- msgs
}

// ===========================================================================
// PostgreSQL CDC Tests
// ===========================================================================

func TestPostgresCDCSourceInterface(t *testing.T) {
	src, err := NewPostgresCDCSource("pg-test", map[string]any{
		"dsn":     "postgres://localhost/test",
		"channel": "changes",
		"tables":  []any{"users", "orders"},
	})
	if err != nil {
		t.Fatalf("NewPostgresCDCSource: %v", err)
	}

	// Verify interface compliance.
	var _ connector.EventSource = src

	if src.Name() != "pg-test" {
		t.Errorf("expected name %q, got %q", "pg-test", src.Name())
	}
	if src.Type() != "postgres.cdc" {
		t.Errorf("expected type %q, got %q", "postgres.cdc", src.Type())
	}
	if src.Healthy() {
		t.Error("expected unhealthy before start")
	}
}

func TestPostgresCDCConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
		check   func(t *testing.T, cfg PostgresCDCConfig)
	}{
		{
			name: "full config",
			config: map[string]any{
				"dsn":           "postgres://user:pass@host/db",
				"tables":        []any{"t1", "t2"},
				"channel":       "my_channel",
				"poll_interval": "10s",
			},
			check: func(t *testing.T, cfg PostgresCDCConfig) {
				if cfg.DSN != "postgres://user:pass@host/db" {
					t.Errorf("DSN: got %q", cfg.DSN)
				}
				if len(cfg.Tables) != 2 || cfg.Tables[0] != "t1" {
					t.Errorf("Tables: got %v", cfg.Tables)
				}
				if cfg.Channel != "my_channel" {
					t.Errorf("Channel: got %q", cfg.Channel)
				}
				if cfg.PollInterval != 10*time.Second {
					t.Errorf("PollInterval: got %v", cfg.PollInterval)
				}
			},
		},
		{
			name:   "defaults",
			config: map[string]any{},
			check: func(t *testing.T, cfg PostgresCDCConfig) {
				if cfg.Channel != "cdc_changes" {
					t.Errorf("expected default channel %q, got %q", "cdc_changes", cfg.Channel)
				}
				if cfg.PollInterval != 5*time.Second {
					t.Errorf("expected default poll_interval 5s, got %v", cfg.PollInterval)
				}
			},
		},
		{
			name: "invalid poll_interval",
			config: map[string]any{
				"poll_interval": "not-a-duration",
			},
			wantErr: true,
		},
		{
			name: "string slice tables",
			config: map[string]any{
				"tables": []string{"a", "b", "c"},
			},
			check: func(t *testing.T, cfg PostgresCDCConfig) {
				if len(cfg.Tables) != 3 {
					t.Errorf("expected 3 tables, got %d", len(cfg.Tables))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parsePostgresCDCConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePostgresCDCConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestPostgresCDCStartStop(t *testing.T) {
	listener := newMockPGListener()

	src, err := NewPostgresCDCSourceWithListener("pg-lifecycle", map[string]any{
		"dsn":     "postgres://mock/test",
		"channel": "test_changes",
	}, listener)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !src.Healthy() {
		t.Error("expected healthy after Start")
	}

	if !listener.listening {
		t.Error("expected listener to be in listening state")
	}
	if listener.channel != "test_changes" {
		t.Errorf("expected channel %q, got %q", "test_changes", listener.channel)
	}

	// Stop
	if err := src.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if src.Healthy() {
		t.Error("expected unhealthy after Stop")
	}

	// Checkpoint should be no-op
	if err := src.Checkpoint(ctx); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
}

func TestPostgresCDCEventConversion(t *testing.T) {
	listener := newMockPGListener()

	src, err := NewPostgresCDCSourceWithListener("pg-events", map[string]any{
		"dsn":     "postgres://mock/test",
		"channel": "changes",
	}, listener)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send a simulated INSERT notification.
	insertPayload := `{"table":"users","operation":"insert","data":{"id":1,"name":"Alice"},"timestamp":"2026-02-15T12:00:00Z"}`
	listener.sendNotification(insertPayload)

	select {
	case evt := <-output:
		if evt.Type != "postgres.row.insert" {
			t.Errorf("expected type %q, got %q", "postgres.row.insert", evt.Type)
		}
		if evt.Subject != "users" {
			t.Errorf("expected subject %q, got %q", "users", evt.Subject)
		}
		if evt.Source != "postgres.cdc/pg-events" {
			t.Errorf("expected source %q, got %q", "postgres.cdc/pg-events", evt.Source)
		}
		if evt.DataContentType != "application/json" {
			t.Errorf("expected content type %q, got %q", "application/json", evt.DataContentType)
		}
		if evt.ID == "" {
			t.Error("expected non-empty event ID")
		}

		// Verify the data contains the change event.
		var change ChangeEvent
		if err := json.Unmarshal(evt.Data, &change); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if change.Table != "users" {
			t.Errorf("expected table %q, got %q", "users", change.Table)
		}
		if change.Operation != "insert" {
			t.Errorf("expected operation %q, got %q", "insert", change.Operation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Send a simulated UPDATE notification.
	updatePayload := `{"table":"users","operation":"update","data":{"id":1,"name":"Bob"},"old_data":{"id":1,"name":"Alice"}}`
	listener.sendNotification(updatePayload)

	select {
	case evt := <-output:
		if evt.Type != "postgres.row.update" {
			t.Errorf("expected type %q, got %q", "postgres.row.update", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for update event")
	}

	// Send a simulated DELETE notification.
	deletePayload := `{"table":"orders","operation":"delete","data":{"id":42}}`
	listener.sendNotification(deletePayload)

	select {
	case evt := <-output:
		if evt.Type != "postgres.row.delete" {
			t.Errorf("expected type %q, got %q", "postgres.row.delete", evt.Type)
		}
		if evt.Subject != "orders" {
			t.Errorf("expected subject %q, got %q", "orders", evt.Subject)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for delete event")
	}
}

func TestPostgresCDCInvalidNotification(t *testing.T) {
	listener := newMockPGListener()

	src, err := NewPostgresCDCSourceWithListener("pg-invalid", map[string]any{
		"dsn":     "postgres://mock/test",
		"channel": "changes",
	}, listener)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send invalid JSON; should be skipped, not crash.
	listener.sendNotification("not valid json")

	// Send a valid notification after the invalid one.
	validPayload := `{"table":"t","operation":"insert","data":{}}`
	listener.sendNotification(validPayload)

	select {
	case evt := <-output:
		if evt.Type != "postgres.row.insert" {
			t.Errorf("expected valid event after invalid one, got type %q", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event after invalid notification")
	}
}

func TestPostgresCDCConnectError(t *testing.T) {
	listener := newMockPGListener()
	listener.connectErr = fmt.Errorf("connection refused")

	src, err := NewPostgresCDCSourceWithListener("pg-fail-connect", map[string]any{
		"dsn":     "postgres://bad/host",
		"channel": "changes",
	}, listener)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	err = src.Start(context.Background(), output)
	if err == nil {
		t.Fatal("expected error on connect failure")
	}
}

func TestPostgresCDCNoListener(t *testing.T) {
	src, err := NewPostgresCDCSource("pg-no-listener", map[string]any{
		"dsn": "postgres://mock/test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	err = src.Start(context.Background(), output)
	if err == nil {
		t.Fatal("expected error when no listener configured")
	}
}

// ===========================================================================
// Redis Stream Tests
// ===========================================================================

func TestRedisStreamSourceInterface(t *testing.T) {
	src, err := NewRedisStreamSource("redis-test", map[string]any{
		"addr":       "localhost:6379",
		"stream":     "events",
		"group":      "workers",
		"consumer":   "w1",
		"batch_size": float64(5),
	})
	if err != nil {
		t.Fatalf("NewRedisStreamSource: %v", err)
	}

	// Verify interface compliance.
	var _ connector.EventSource = src

	if src.Name() != "redis-test" {
		t.Errorf("expected name %q, got %q", "redis-test", src.Name())
	}
	if src.Type() != "redis.stream" {
		t.Errorf("expected type %q, got %q", "redis.stream", src.Type())
	}
	if src.Healthy() {
		t.Error("expected unhealthy before start")
	}
}

func TestRedisStreamConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
		check   func(t *testing.T, cfg RedisStreamConfig)
	}{
		{
			name: "full config",
			config: map[string]any{
				"addr":       "redis.example.com:6380",
				"stream":     "my-stream",
				"group":      "my-group",
				"consumer":   "consumer-1",
				"batch_size": float64(25),
			},
			check: func(t *testing.T, cfg RedisStreamConfig) {
				if cfg.Addr != "redis.example.com:6380" {
					t.Errorf("Addr: got %q", cfg.Addr)
				}
				if cfg.Stream != "my-stream" {
					t.Errorf("Stream: got %q", cfg.Stream)
				}
				if cfg.Group != "my-group" {
					t.Errorf("Group: got %q", cfg.Group)
				}
				if cfg.Consumer != "consumer-1" {
					t.Errorf("Consumer: got %q", cfg.Consumer)
				}
				if cfg.BatchSize != 25 {
					t.Errorf("BatchSize: got %d", cfg.BatchSize)
				}
			},
		},
		{
			name: "defaults",
			config: map[string]any{
				"stream": "events",
			},
			check: func(t *testing.T, cfg RedisStreamConfig) {
				if cfg.Addr != "localhost:6379" {
					t.Errorf("expected default addr %q, got %q", "localhost:6379", cfg.Addr)
				}
				if cfg.BatchSize != 10 {
					t.Errorf("expected default batch_size 10, got %d", cfg.BatchSize)
				}
			},
		},
		{
			name:    "missing stream",
			config:  map[string]any{},
			wantErr: true,
		},
		{
			name: "int batch_size",
			config: map[string]any{
				"stream":     "events",
				"batch_size": 42,
			},
			check: func(t *testing.T, cfg RedisStreamConfig) {
				if cfg.BatchSize != 42 {
					t.Errorf("expected batch_size 42, got %d", cfg.BatchSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseRedisStreamConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRedisStreamConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestRedisStreamStartStop(t *testing.T) {
	client := newMockRedisStreamClient()

	src, err := NewRedisStreamSourceWithClient("redis-lifecycle", map[string]any{
		"stream":   "events",
		"group":    "workers",
		"consumer": "w1",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !src.Healthy() {
		t.Error("expected healthy after Start")
	}

	if !client.connected {
		t.Error("expected client to be connected")
	}

	if err := src.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if src.Healthy() {
		t.Error("expected unhealthy after Stop")
	}

	if err := src.Checkpoint(ctx); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
}

func TestRedisStreamEventConversion(t *testing.T) {
	client := newMockRedisStreamClient()

	src, err := NewRedisStreamSourceWithClient("redis-events", map[string]any{
		"stream":   "orders",
		"group":    "processors",
		"consumer": "p1",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send messages with a custom type field.
	client.sendMessages([]RedisStreamMessage{
		{
			ID: "1645000000000-0",
			Fields: map[string]string{
				"type":     "order.created",
				"order_id": "123",
				"amount":   "99.99",
			},
		},
	})

	select {
	case evt := <-output:
		if evt.Type != "order.created" {
			t.Errorf("expected type %q, got %q", "order.created", evt.Type)
		}
		if evt.Source != "redis.stream/redis-events" {
			t.Errorf("expected source %q, got %q", "redis.stream/redis-events", evt.Source)
		}
		if evt.Subject != "orders/1645000000000-0" {
			t.Errorf("expected subject %q, got %q", "orders/1645000000000-0", evt.Subject)
		}

		// Verify data contains the fields.
		var fields map[string]string
		if err := json.Unmarshal(evt.Data, &fields); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if fields["order_id"] != "123" {
			t.Errorf("expected order_id %q, got %q", "123", fields["order_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Verify the message was acknowledged.
	time.Sleep(50 * time.Millisecond) // small delay for ack goroutine
	client.mu.Lock()
	acked := len(client.ackedIDs)
	client.mu.Unlock()
	if acked != 1 {
		t.Errorf("expected 1 acked message, got %d", acked)
	}
}

func TestRedisStreamDefaultEventType(t *testing.T) {
	client := newMockRedisStreamClient()

	src, err := NewRedisStreamSourceWithClient("redis-default-type", map[string]any{
		"stream":   "events",
		"group":    "g",
		"consumer": "c",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send message without a "type" field.
	client.sendMessages([]RedisStreamMessage{
		{
			ID:     "1-0",
			Fields: map[string]string{"key": "value"},
		},
	})

	select {
	case evt := <-output:
		if evt.Type != "redis.stream.message" {
			t.Errorf("expected default type %q, got %q", "redis.stream.message", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestRedisStreamNoClient(t *testing.T) {
	src, err := NewRedisStreamSource("redis-no-client", map[string]any{
		"stream": "events",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	err = src.Start(context.Background(), output)
	if err == nil {
		t.Fatal("expected error when no client configured")
	}
}

func TestRedisStreamConnectError(t *testing.T) {
	client := newMockRedisStreamClient()
	client.connectErr = fmt.Errorf("connection refused")

	src, err := NewRedisStreamSourceWithClient("redis-fail-connect", map[string]any{
		"stream": "events",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	err = src.Start(context.Background(), output)
	if err == nil {
		t.Fatal("expected error on connect failure")
	}
}

// ===========================================================================
// AWS SQS Tests
// ===========================================================================

func TestSQSSourceInterface(t *testing.T) {
	src, err := NewSQSSource("sqs-test", map[string]any{
		"queue_url":         "https://sqs.us-east-1.amazonaws.com/123456789/test-queue",
		"region":            "us-east-1",
		"max_messages":      float64(5),
		"wait_time_seconds": float64(10),
	})
	if err != nil {
		t.Fatalf("NewSQSSource: %v", err)
	}

	// Verify interface compliance.
	var _ connector.EventSource = src

	if src.Name() != "sqs-test" {
		t.Errorf("expected name %q, got %q", "sqs-test", src.Name())
	}
	if src.Type() != "sqs" {
		t.Errorf("expected type %q, got %q", "sqs", src.Type())
	}
	if src.Healthy() {
		t.Error("expected unhealthy before start")
	}
}

func TestSQSSinkInterface(t *testing.T) {
	sink, err := NewSQSSink("sqs-sink-test", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789/test-queue",
		"region":    "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewSQSSink: %v", err)
	}

	// Verify interface compliance.
	var _ connector.EventSink = sink

	if sink.Name() != "sqs-sink-test" {
		t.Errorf("expected name %q, got %q", "sqs-sink-test", sink.Name())
	}
	if sink.Type() != "sqs" {
		t.Errorf("expected type %q, got %q", "sqs", sink.Type())
	}
	if !sink.Healthy() {
		t.Error("expected healthy after creation")
	}
}

func TestSQSConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
		check   func(t *testing.T, cfg SQSConfig)
	}{
		{
			name: "full config",
			config: map[string]any{
				"queue_url":         "https://sqs.us-east-1.amazonaws.com/123/q",
				"region":            "us-west-2",
				"max_messages":      float64(5),
				"wait_time_seconds": float64(10),
			},
			check: func(t *testing.T, cfg SQSConfig) {
				if cfg.QueueURL != "https://sqs.us-east-1.amazonaws.com/123/q" {
					t.Errorf("QueueURL: got %q", cfg.QueueURL)
				}
				if cfg.Region != "us-west-2" {
					t.Errorf("Region: got %q", cfg.Region)
				}
				if cfg.MaxMessages != 5 {
					t.Errorf("MaxMessages: got %d", cfg.MaxMessages)
				}
				if cfg.WaitTimeSeconds != 10 {
					t.Errorf("WaitTimeSeconds: got %d", cfg.WaitTimeSeconds)
				}
			},
		},
		{
			name: "defaults",
			config: map[string]any{
				"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
			},
			check: func(t *testing.T, cfg SQSConfig) {
				if cfg.MaxMessages != 10 {
					t.Errorf("expected default max_messages 10, got %d", cfg.MaxMessages)
				}
				if cfg.WaitTimeSeconds != 20 {
					t.Errorf("expected default wait_time_seconds 20, got %d", cfg.WaitTimeSeconds)
				}
			},
		},
		{
			name:    "missing queue_url",
			config:  map[string]any{},
			wantErr: true,
		},
		{
			name: "int max_messages",
			config: map[string]any{
				"queue_url":    "https://sqs.us-east-1.amazonaws.com/123/q",
				"max_messages": 7,
			},
			check: func(t *testing.T, cfg SQSConfig) {
				if cfg.MaxMessages != 7 {
					t.Errorf("expected max_messages 7, got %d", cfg.MaxMessages)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseSQSConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSQSConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestSQSSourceStartStop(t *testing.T) {
	client := newMockSQSClient()

	src, err := NewSQSSourceWithClient("sqs-lifecycle", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
		"region":    "us-east-1",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx := context.Background()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !src.Healthy() {
		t.Error("expected healthy after Start")
	}

	if err := src.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if src.Healthy() {
		t.Error("expected unhealthy after Stop")
	}

	if err := src.Checkpoint(ctx); err != nil {
		t.Errorf("Checkpoint: %v", err)
	}
}

func TestSQSSourceEventConversion(t *testing.T) {
	client := newMockSQSClient()

	src, err := NewSQSSourceWithClient("sqs-events", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send messages with JSON body.
	client.sendMessages([]SQSMessage{
		{
			MessageID:     "msg-001",
			ReceiptHandle: "handle-001",
			Body:          `{"order_id":"123","status":"pending"}`,
			Attributes: map[string]string{
				"event_type": "order.created",
			},
		},
	})

	select {
	case evt := <-output:
		if evt.Type != "order.created" {
			t.Errorf("expected type %q, got %q", "order.created", evt.Type)
		}
		if evt.Source != "sqs/sqs-events" {
			t.Errorf("expected source %q, got %q", "sqs/sqs-events", evt.Source)
		}
		if evt.Subject != "msg-001" {
			t.Errorf("expected subject %q, got %q", "msg-001", evt.Subject)
		}

		// Verify data is the raw JSON body.
		var data map[string]string
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if data["order_id"] != "123" {
			t.Errorf("expected order_id %q, got %q", "123", data["order_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Verify message was deleted.
	time.Sleep(50 * time.Millisecond)
	client.mu.Lock()
	deleted := len(client.deletedHandles)
	client.mu.Unlock()
	if deleted != 1 {
		t.Errorf("expected 1 deleted message, got %d", deleted)
	}
}

func TestSQSSourceNonJSONBody(t *testing.T) {
	client := newMockSQSClient()

	src, err := NewSQSSourceWithClient("sqs-nonjson", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := src.Start(ctx, output); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	// Send a plain text message.
	client.sendMessages([]SQSMessage{
		{
			MessageID:     "msg-text",
			ReceiptHandle: "handle-text",
			Body:          "hello world",
			Attributes:    map[string]string{},
		},
	})

	select {
	case evt := <-output:
		if evt.Type != "sqs.message" {
			t.Errorf("expected default type %q, got %q", "sqs.message", evt.Type)
		}
		// Non-JSON body should be wrapped as a JSON string.
		var text string
		if err := json.Unmarshal(evt.Data, &text); err != nil {
			t.Fatalf("unmarshal non-json data: %v", err)
		}
		if text != "hello world" {
			t.Errorf("expected %q, got %q", "hello world", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestSQSSourceNoClient(t *testing.T) {
	src, err := NewSQSSource("sqs-no-client", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	output := make(chan connector.Event, 10)
	err = src.Start(context.Background(), output)
	if err == nil {
		t.Fatal("expected error when no client configured")
	}
}

// ---------------------------------------------------------------------------
// SQS Sink Tests
// ---------------------------------------------------------------------------

func TestSQSSinkDeliver(t *testing.T) {
	client := newMockSQSClient()

	sink, err := NewSQSSinkWithClient("sqs-deliver", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	event := connector.Event{
		ID:     "evt-001",
		Source: "test",
		Type:   "test.event",
		Time:   time.Now().UTC(),
		Data:   json.RawMessage(`{"key":"value"}`),
	}

	if err := sink.Deliver(context.Background(), event); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	client.mu.Lock()
	sentCount := len(client.sentMessages)
	client.mu.Unlock()

	if sentCount != 1 {
		t.Errorf("expected 1 sent message, got %d", sentCount)
	}

	// Verify the sent body is valid JSON containing the event.
	client.mu.Lock()
	body := client.sentMessages[0]
	client.mu.Unlock()

	var decoded connector.Event
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if decoded.ID != "evt-001" {
		t.Errorf("expected event ID %q, got %q", "evt-001", decoded.ID)
	}
	if decoded.Type != "test.event" {
		t.Errorf("expected event type %q, got %q", "test.event", decoded.Type)
	}
}

func TestSQSSinkDeliverBatch(t *testing.T) {
	client := newMockSQSClient()

	sink, err := NewSQSSinkWithClient("sqs-batch", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events := []connector.Event{
		{ID: "b-1", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-2", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-3", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
	}

	errs := sink.DeliverBatch(context.Background(), events)
	for i, err := range errs {
		if err != nil {
			t.Errorf("event %d: unexpected error: %v", i, err)
		}
	}

	client.mu.Lock()
	sentCount := len(client.sentMessages)
	client.mu.Unlock()

	if sentCount != 3 {
		t.Errorf("expected 3 sent messages, got %d", sentCount)
	}
}

func TestSQSSinkDeliverBatchPartialFailure(t *testing.T) {
	client := newMockSQSClient()
	client.batchResults = []SQSBatchResult{
		{ID: "entry-0", MessageID: "msg-0"},
		{ID: "entry-1", Error: fmt.Errorf("throttled")},
		{ID: "entry-2", MessageID: "msg-2"},
	}

	sink, err := NewSQSSinkWithClient("sqs-batch-partial", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events := []connector.Event{
		{ID: "b-1", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-2", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-3", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
	}

	errs := sink.DeliverBatch(context.Background(), events)
	if errs[0] != nil {
		t.Errorf("event 0: unexpected error: %v", errs[0])
	}
	if errs[1] == nil {
		t.Error("event 1: expected error for throttled message")
	}
	if errs[2] != nil {
		t.Errorf("event 2: unexpected error: %v", errs[2])
	}
}

func TestSQSSinkDeliverBatchWholesaleFailure(t *testing.T) {
	client := newMockSQSClient()
	client.batchErr = fmt.Errorf("service unavailable")

	sink, err := NewSQSSinkWithClient("sqs-batch-fail", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	}, client)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events := []connector.Event{
		{ID: "b-1", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
		{ID: "b-2", Source: "test", Type: "batch.test", Time: time.Now().UTC(), Data: json.RawMessage(`{}`)},
	}

	errs := sink.DeliverBatch(context.Background(), events)
	for i, err := range errs {
		if err == nil {
			t.Errorf("event %d: expected error on wholesale failure", i)
		}
	}
}

func TestSQSSinkNoClient(t *testing.T) {
	sink, err := NewSQSSink("sqs-no-client", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = sink.Deliver(context.Background(), connector.Event{
		ID:   "evt-1",
		Data: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error when no client configured")
	}
}

func TestSQSSinkStop(t *testing.T) {
	sink, err := NewSQSSink("sqs-stop", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if !sink.Healthy() {
		t.Error("expected healthy before stop")
	}

	if err := sink.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if sink.Healthy() {
		t.Error("expected unhealthy after stop")
	}
}

func TestSQSSinkMissingQueueURL(t *testing.T) {
	_, err := NewSQSSink("no-url", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing queue_url")
	}
}

// ===========================================================================
// Factory Registration Tests
// ===========================================================================

func TestSourceFactoryRegistration(t *testing.T) {
	reg := connector.NewRegistry()

	if err := RegisterBuiltinSources(reg); err != nil {
		t.Fatalf("RegisterBuiltinSources: %v", err)
	}

	// Verify sources are registered.
	sources := reg.ListSources()
	expectedSources := map[string]bool{
		"postgres.cdc": false,
		"redis.stream": false,
		"sqs":          false,
	}
	for _, s := range sources {
		if _, ok := expectedSources[s]; ok {
			expectedSources[s] = true
		}
	}
	for name, found := range expectedSources {
		if !found {
			t.Errorf("expected source %q to be registered", name)
		}
	}

	// Verify sinks are registered.
	sinks := reg.ListSinks()
	expectedSinks := map[string]bool{
		"sqs": false,
	}
	for _, s := range sinks {
		if _, ok := expectedSinks[s]; ok {
			expectedSinks[s] = true
		}
	}
	for name, found := range expectedSinks {
		if !found {
			t.Errorf("expected sink %q to be registered", name)
		}
	}

	// Verify double registration fails.
	err := RegisterBuiltinSources(reg)
	if err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestPostgresCDCSourceFactory(t *testing.T) {
	factory := NewPostgresCDCSourceFactory()

	src, err := factory("pg-factory-test", map[string]any{
		"dsn":     "postgres://localhost/test",
		"channel": "changes",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if src.Name() != "pg-factory-test" {
		t.Errorf("expected name %q, got %q", "pg-factory-test", src.Name())
	}
	if src.Type() != "postgres.cdc" {
		t.Errorf("expected type %q, got %q", "postgres.cdc", src.Type())
	}
}

func TestRedisStreamSourceFactory(t *testing.T) {
	factory := NewRedisStreamSourceFactory()

	src, err := factory("redis-factory-test", map[string]any{
		"stream":   "events",
		"group":    "workers",
		"consumer": "w1",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if src.Name() != "redis-factory-test" {
		t.Errorf("expected name %q, got %q", "redis-factory-test", src.Name())
	}
	if src.Type() != "redis.stream" {
		t.Errorf("expected type %q, got %q", "redis.stream", src.Type())
	}
}

func TestSQSSourceFactory(t *testing.T) {
	factory := NewSQSSourceFactory()

	src, err := factory("sqs-factory-test", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if src.Name() != "sqs-factory-test" {
		t.Errorf("expected name %q, got %q", "sqs-factory-test", src.Name())
	}
	if src.Type() != "sqs" {
		t.Errorf("expected type %q, got %q", "sqs", src.Type())
	}
}

func TestSQSSinkFactory(t *testing.T) {
	factory := NewSQSSinkFactory()

	sink, err := factory("sqs-sink-factory-test", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if sink.Name() != "sqs-sink-factory-test" {
		t.Errorf("expected name %q, got %q", "sqs-sink-factory-test", sink.Name())
	}
	if sink.Type() != "sqs" {
		t.Errorf("expected type %q, got %q", "sqs", sink.Type())
	}
}

func TestRegistryCreateFromFactory(t *testing.T) {
	reg := connector.NewRegistry()
	if err := RegisterBuiltinSources(reg); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Create instances via the registry.
	pgSrc, err := reg.CreateSource("postgres.cdc", "my-pg", map[string]any{
		"dsn":     "postgres://localhost/test",
		"channel": "changes",
	})
	if err != nil {
		t.Fatalf("CreateSource postgres.cdc: %v", err)
	}
	if pgSrc.Name() != "my-pg" {
		t.Errorf("expected name %q, got %q", "my-pg", pgSrc.Name())
	}

	redisSrc, err := reg.CreateSource("redis.stream", "my-redis", map[string]any{
		"stream": "events",
	})
	if err != nil {
		t.Fatalf("CreateSource redis.stream: %v", err)
	}
	if redisSrc.Name() != "my-redis" {
		t.Errorf("expected name %q, got %q", "my-redis", redisSrc.Name())
	}

	sqsSrc, err := reg.CreateSource("sqs", "my-sqs-src", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("CreateSource sqs: %v", err)
	}
	if sqsSrc.Name() != "my-sqs-src" {
		t.Errorf("expected name %q, got %q", "my-sqs-src", sqsSrc.Name())
	}

	sqsSink, err := reg.CreateSink("sqs", "my-sqs-sink", map[string]any{
		"queue_url": "https://sqs.us-east-1.amazonaws.com/123/q",
	})
	if err != nil {
		t.Fatalf("CreateSink sqs: %v", err)
	}
	if sqsSink.Name() != "my-sqs-sink" {
		t.Errorf("expected name %q, got %q", "my-sqs-sink", sqsSink.Name())
	}
}
