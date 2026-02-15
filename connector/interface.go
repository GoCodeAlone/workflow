package connector

import (
	"context"
	"encoding/json"
	"time"
)

// Event is the universal event envelope (CloudEvents compatible).
type Event struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Subject         string          `json:"subject,omitempty"`
	Time            time.Time       `json:"time"`
	Data            json.RawMessage `json:"data"`
	DataSchema      string          `json:"dataschema,omitempty"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	// Internal metadata (not serialized to CloudEvents)
	TenantID       string `json:"-"`
	PipelineID     string `json:"-"`
	IdempotencyKey string `json:"-"`
}

// EventSource defines the interface for event ingress connectors.
type EventSource interface {
	Name() string
	Type() string
	Start(ctx context.Context, output chan<- Event) error
	Stop(ctx context.Context) error
	Healthy() bool
	Checkpoint(ctx context.Context) error
}

// EventSink defines the interface for event egress connectors.
type EventSink interface {
	Name() string
	Type() string
	Deliver(ctx context.Context, event Event) error
	DeliverBatch(ctx context.Context, events []Event) []error
	Stop(ctx context.Context) error
	Healthy() bool
}

// SourceFactory creates EventSource instances from config.
type SourceFactory func(name string, config map[string]any) (EventSource, error)

// SinkFactory creates EventSink instances from config.
type SinkFactory func(name string, config map[string]any) (EventSink, error)
