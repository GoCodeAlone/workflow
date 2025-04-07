package module

import (
	"context"
)

// MessageHandler interface for handling messages
type MessageHandler interface {
	HandleMessage(message []byte) error
}

// MessageProducer interface for producing messages
type MessageProducer interface {
	SendMessage(topic string, message []byte) error
}

// MessageConsumer interface for consuming messages
type MessageConsumer interface {
	Subscribe(topic string, handler MessageHandler) error
	Unsubscribe(topic string) error
}

// MessageBroker interface for message broker modules
type MessageBroker interface {
	Producer() MessageProducer
	Consumer() MessageConsumer
	Subscribe(topic string, handler MessageHandler) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
