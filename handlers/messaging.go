package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// Standard handler name constants
const (
	MessagingWorkflowHandlerName = "workflow.handler.messaging"
)

// TopicHandlerConfig represents a topic handler configuration in messaging workflow
type TopicHandlerConfig struct {
	Topic   string         `json:"topic" yaml:"topic"`
	Handler string         `json:"handler" yaml:"handler"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// MessagingWorkflowHandler handles message-based workflows
type MessagingWorkflowHandler struct {
	name      string
	namespace workflowmodule.ModuleNamespaceProvider
}

// NewMessagingWorkflowHandler creates a new messaging workflow handler
func NewMessagingWorkflowHandler() *MessagingWorkflowHandler {
	return NewMessagingWorkflowHandlerWithNamespace(nil)
}

// NewMessagingWorkflowHandlerWithNamespace creates a messaging workflow handler with namespace support
func NewMessagingWorkflowHandlerWithNamespace(namespace workflowmodule.ModuleNamespaceProvider) *MessagingWorkflowHandler {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = workflowmodule.NewStandardNamespace("", "")
	}

	return &MessagingWorkflowHandler{
		name:      namespace.FormatName(MessagingWorkflowHandlerName),
		namespace: namespace,
	}
}

// Name returns the name of this handler
func (h *MessagingWorkflowHandler) Name() string {
	return h.name
}

// CanHandle returns true if this handler can process the given workflow type
func (h *MessagingWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "messaging"
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *MessagingWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	// Convert the generic config to messaging-specific config
	msgConfig, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid messaging workflow configuration format")
	}

	// Find message broker service using FixMessagingHandlerServices helper
	// Instead of directly accessing app.Services()
	services := FixMessagingHandlerServices(app)
	var broker workflowmodule.MessageBroker

	// Loop through available services to find a message broker
	for _, svc := range services {
		if b, ok := svc.(workflowmodule.MessageBroker); ok {
			broker = b
			break
		}
	}

	// If no broker was found in services, look in the registry
	if broker == nil {
		// Find the standard name broker first (with namespace)
		brokerName := h.namespace.FormatName(workflowmodule.InMemoryMessageBrokerName)
		var brokerSvc any

		err := app.GetService(brokerName, &brokerSvc)
		if err == nil && brokerSvc != nil {
			if b, ok := brokerSvc.(workflowmodule.MessageBroker); ok {
				broker = b
			}
		}

		// If still not found, look for any broker
		if broker == nil {
			// Loop through registry to find any message broker
			for _, svc := range app.SvcRegistry() {
				if b, ok := svc.(workflowmodule.MessageBroker); ok {
					broker = b
					break
				}
			}
		}
	}

	if broker == nil {
		return fmt.Errorf("no message broker service found")
	}

	// Configure subscriptions
	subscriptions, ok := msgConfig["subscriptions"].([]any)
	if !ok {
		return fmt.Errorf("subscriptions not found in messaging workflow configuration")
	}

	consumer := broker.Consumer()
	for i, sub := range subscriptions {
		subMap, ok := sub.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid subscription configuration at index %d", i)
		}

		topic, _ := subMap["topic"].(string)
		handlerName, _ := subMap["handler"].(string)

		if topic == "" || handlerName == "" {
			return fmt.Errorf("incomplete subscription configuration at index %d: topic and handler are required", i)
		}

		// Apply namespace to handler name
		if h.namespace != nil {
			handlerName = h.namespace.ResolveDependency(handlerName)
		}

		// Get handler service by name
		var handlerSvc any
		_ = app.GetService(handlerName, &handlerSvc)
		if handlerSvc == nil {
			return fmt.Errorf("handler service '%s' not found for topic %s", handlerName, topic)
		}

		messageHandler, ok := handlerSvc.(workflowmodule.MessageHandler)
		if !ok {
			return fmt.Errorf("service '%s' does not implement MessageHandler interface", handlerName)
		}

		// Subscribe to topic
		if err := consumer.Subscribe(topic, messageHandler); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}
	}

	// Configure producers (optional)
	if producers, ok := msgConfig["producers"].([]any); ok {
		for i, prod := range producers {
			prodMap, ok := prod.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid producer configuration at index %d", i)
			}

			// Log producer configuration for debugging
			name, _ := prodMap["name"].(string)
			topic, _ := prodMap["topic"].(string)
			if name != "" && topic != "" {
				fmt.Printf("Found producer configuration: %s -> %s\n", name, topic)
			}

			// Process producer configuration
			// This would depend on the specific requirements of your messaging system
		}
	}

	return nil
}

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *MessagingWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	// For messaging workflows, the action represents the broker:topic or just topic
	// The data will be the message payload

	// Parse the broker and topic from the action
	brokerName := ""
	topic := action

	// Parse broker:topic format if used
	if parts := strings.Split(action, ":"); len(parts) > 1 {
		brokerName = parts[0]
		topic = parts[1]
	}

	// Extract broker name from data if not in action
	if brokerName == "" {
		brokerName, _ = data["broker"].(string)
	}

	// Get the application from context
	var app modular.Application
	if appVal := ctx.Value(applicationContextKey); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	// Find the broker service
	var broker workflowmodule.MessageBroker
	services := FixMessagingHandlerServices(app)

	// If broker name specified, try to get that specific broker
	if brokerName != "" {
		// Apply namespace if needed
		if h.namespace != nil {
			brokerName = h.namespace.ResolveDependency(brokerName)
		}

		var brokerSvc any
		_ = app.GetService(brokerName, &brokerSvc)
		if brokerSvc != nil {
			if b, ok := brokerSvc.(workflowmodule.MessageBroker); ok {
				broker = b
			}
		}
	}

	// If no broker found yet, scan all services
	if broker == nil {
		// Look through available services
		for _, svc := range services {
			if b, ok := svc.(workflowmodule.MessageBroker); ok {
				broker = b
				break
			}
		}

		// If still not found, look through registry
		if broker == nil {
			for _, svc := range app.SvcRegistry() {
				if b, ok := svc.(workflowmodule.MessageBroker); ok {
					broker = b
					break
				}
			}
		}
	}

	if broker == nil {
		return nil, fmt.Errorf("no message broker found")
	}

	if topic == "" {
		return nil, fmt.Errorf("topic not specified")
	}

	// Extract the message payload
	var payload []byte
	var err error

	// Use the "message" field if present, otherwise use all data
	if msg, ok := data["message"]; ok {
		switch m := msg.(type) {
		case string:
			payload = []byte(m)
		case []byte:
			payload = m
		default:
			// Serialize the message object to JSON
			payload, err = json.Marshal(msg)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize message: %w", err)
			}
		}
	} else {
		// Use data as the message payload
		payload, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize message data: %w", err)
		}
	}

	// Send the message
	err = broker.Producer().SendMessage(topic, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send message to topic '%s': %w", topic, err)
	}

	// Return success
	return map[string]any{
		"success": true,
		"broker":  broker.(modular.Module).Name(),
		"topic":   topic,
		"length":  len(payload),
	}, nil
}
