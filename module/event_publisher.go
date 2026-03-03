package module

import "context"

// EventPublisher is a generic interface for publishing structured events.
// It provides a high-level abstraction over various messaging backends
// (Kafka, NATS, Kinesis, SQS, in-memory, etc.) and external plugins
// such as the Bento plugin (workflow-plugin-bento).
//
// Services implementing this interface can be registered with the application
// and referenced by name in step.event_publish configurations via the
// "provider" or "broker" config fields.
type EventPublisher interface {
	// PublishEvent publishes a structured event to the given topic/stream.
	// The event map typically follows the CloudEvents envelope format with
	// fields like specversion, type, source, id, time, and data.
	PublishEvent(ctx context.Context, topic string, event map[string]any) error
}
