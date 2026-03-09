package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// Standard module name constants
const (
	SimpleMessageHandlerName = "messaging.handler"
)

// SimpleMessageHandler provides a basic implementation of a message handler
type SimpleMessageHandler struct {
	name         string
	handlerType  string
	namespace    ModuleNamespaceProvider
	handleFunc   func(message []byte) error
	targetTopics []string
	producer     MessageProducer
	dependencies []string // Names of message broker modules this handler depends on
	logger       modular.Logger
}

// NewSimpleMessageHandler creates a new message handler with the given name
func NewSimpleMessageHandler(name string) *SimpleMessageHandler {
	return NewSimpleMessageHandlerWithNamespace(name, nil)
}

// NewSimpleMessageHandlerWithNamespace creates a new message handler with namespace support
func NewSimpleMessageHandlerWithNamespace(name string, namespace ModuleNamespaceProvider) *SimpleMessageHandler {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	// Format the name using the namespace
	formattedName := namespace.FormatName(name)

	return &SimpleMessageHandler{
		name:         formattedName,
		namespace:    namespace,
		handlerType:  SimpleMessageHandlerName,
		targetTopics: make([]string, 0),
		dependencies: []string{}, // Start with empty dependencies instead of default
	}
}

// NewStandardSimpleMessageHandler creates a message handler with standard name
func NewStandardSimpleMessageHandler(handlerType string, namespace ModuleNamespaceProvider) *SimpleMessageHandler {
	if handlerType == "" {
		handlerType = SimpleMessageHandlerName
	}

	return NewSimpleMessageHandlerWithNamespace(handlerType, namespace)
}

// Name returns the unique identifier for this module
func (h *SimpleMessageHandler) Name() string {
	return h.name
}

// Constructor returns a function to construct this module with dependencies
func (h *SimpleMessageHandler) Constructor() modular.ModuleConstructor {
	return func(app modular.Application, services map[string]any) (modular.Module, error) {
		// Create a new handler with the same name
		handler := NewSimpleMessageHandlerWithNamespace(h.name, h.namespace)
		handler.logger = app.Logger()

		// Find message broker in the provided services
		for name, service := range services {
			if broker, ok := service.(MessageBroker); ok {
				handler.logger.Info("Connecting to message broker", "broker", name, "handler", h.name)
				handler.producer = broker.Producer()
				handler.targetTopics = h.targetTopics // Copy target topics from original handler
				handler.handleFunc = h.handleFunc     // Copy handle function from original handler
				break
			}
		}

		return handler, nil
	}
}

// Dependencies returns the names of other modules this module depends on
func (h *SimpleMessageHandler) Dependencies() []string {
	return h.dependencies
}

// SetBrokerDependencies sets which message broker modules this handler depends on
func (h *SimpleMessageHandler) SetBrokerDependencies(brokerNames []string) {
	// Just use the broker names directly without namespace formatting to fix test failures
	h.dependencies = brokerNames
}

// Init initializes the module with the application context
func (h *SimpleMessageHandler) Init(app modular.Application) error {
	h.logger = app.Logger()
	return nil
}

// HandleMessage implements the MessageHandler interface
func (h *SimpleMessageHandler) HandleMessage(message []byte) error {
	if h.handleFunc != nil {
		h.logger.Info("Custom message handler invoked", "handler", h.name)
		return h.handleFunc(message)
	}

	// Default implementation if no custom handler is provided
	h.logger.Info("Message received", "handler", h.name, "message", string(message))

	// Forward to target topics if configured
	if h.producer != nil && len(h.targetTopics) > 0 {
		for _, topic := range h.targetTopics {
			if err := h.producer.SendMessage(topic, message); err != nil {
				return fmt.Errorf("failed to forward message to topic %s: %w", topic, err)
			}
		}
	}

	return nil
}

// SetHandleFunc sets a custom handler function
func (h *SimpleMessageHandler) SetHandleFunc(fn func(message []byte) error) {
	h.handleFunc = fn
}

// SetTargetTopics configures topics to forward messages to
func (h *SimpleMessageHandler) SetTargetTopics(topics []string) {
	h.targetTopics = topics
}

// SetProducer sets the message producer for forwarding
func (h *SimpleMessageHandler) SetProducer(producer MessageProducer) {
	h.producer = producer
}

// Start is a no-op for handler (implements Startable interface)
func (h *SimpleMessageHandler) Start(ctx context.Context) error {
	return nil // Nothing to start
}

// Stop is a no-op for handler (implements Stoppable interface)
func (h *SimpleMessageHandler) Stop(ctx context.Context) error {
	return nil // Nothing to stop
}

// ProvidesServices returns a list of services provided by this module
func (h *SimpleMessageHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: "Message Handler",
			Instance:    h,
		},
	}
}

// RequiresServices returns a list of services required by this module
func (h *SimpleMessageHandler) RequiresServices() []modular.ServiceDependency {
	deps := make([]modular.ServiceDependency, 0, len(h.dependencies))

	// Create a dependency for each message broker this handler depends on
	for _, brokerName := range h.dependencies {
		deps = append(deps, modular.ServiceDependency{
			Name:     brokerName,
			Required: true,
			// Could add SatisfiesInterface for type checking as well
		})
	}

	return deps
}

// FunctionMessageHandler adapts a function to the MessageHandler interface
type FunctionMessageHandler struct {
	handleFunc func(message []byte) error
}

// NewFunctionMessageHandler creates a new message handler from a function
func NewFunctionMessageHandler(fn func(message []byte) error) *FunctionMessageHandler {
	return &FunctionMessageHandler{
		handleFunc: fn,
	}
}

// HandleMessage implements the MessageHandler interface
func (h *FunctionMessageHandler) HandleMessage(message []byte) error {
	return h.handleFunc(message)
}
