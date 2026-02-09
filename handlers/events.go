package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// EventWorkflowConfig represents event workflow configuration
type EventWorkflowConfig struct {
	Processor string               `json:"processor" yaml:"processor"`
	Patterns  []EventPatternConfig `json:"patterns" yaml:"patterns"`
	Handlers  []EventHandlerConfig `json:"handlers" yaml:"handlers"`
	Adapters  []EventAdapterConfig `json:"adapters,omitempty" yaml:"adapters,omitempty"`
}

// EventPatternConfig represents event pattern configuration
type EventPatternConfig struct {
	PatternID    string                 `json:"patternId" yaml:"patternId"`
	EventTypes   []string               `json:"eventTypes" yaml:"eventTypes"`
	WindowTime   string                 `json:"windowTime" yaml:"windowTime"`
	Condition    string                 `json:"condition" yaml:"condition"`
	MinOccurs    int                    `json:"minOccurs" yaml:"minOccurs"`
	MaxOccurs    int                    `json:"maxOccurs" yaml:"maxOccurs"`
	OrderMatters bool                   `json:"orderMatters" yaml:"orderMatters"`
	ExtraParams  map[string]interface{} `json:"extraParams,omitempty" yaml:"extraParams,omitempty"`
}

// EventHandlerConfig represents event handler configuration
type EventHandlerConfig struct {
	PatternID string                 `json:"patternId" yaml:"patternId"`
	Handler   string                 `json:"handler" yaml:"handler"`
	Config    map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// EventAdapterConfig represents an adapter between message broker and event processor
type EventAdapterConfig struct {
	Broker      string   `json:"broker" yaml:"broker"`
	Topics      []string `json:"topics" yaml:"topics"`
	EventType   string   `json:"eventType" yaml:"eventType"`
	SourceIdKey string   `json:"sourceIdKey,omitempty" yaml:"sourceIdKey,omitempty"`
	CorrelIdKey string   `json:"correlIdKey,omitempty" yaml:"correlIdKey,omitempty"`
}

// EventWorkflowHandler handles event-driven workflows with complex event processing
type EventWorkflowHandler struct{}

// NewEventWorkflowHandler creates a new event workflow handler
func NewEventWorkflowHandler() *EventWorkflowHandler {
	return &EventWorkflowHandler{}
}

// CanHandle returns true if this handler can process the given workflow type
func (h *EventWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "event"
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *EventWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
	// Convert the generic config to event-specific config
	eventConfig, ok := workflowConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid event workflow configuration format")
	}

	// Extract processor name
	processorName, _ := eventConfig["processor"].(string)
	if processorName == "" {
		return fmt.Errorf("processor name not specified in event workflow")
	}

	// Get the event processor
	var processor *module.EventProcessor
	err := app.GetService(processorName, &processor)
	if err != nil || processor == nil {
		return fmt.Errorf("service '%s' not found", processorName)
	}

	// Configure patterns
	patternsConfig, _ := eventConfig["patterns"].([]interface{})
	if len(patternsConfig) == 0 {
		return fmt.Errorf("no patterns defined in event workflow")
	}

	// Add patterns to the processor
	for i, pc := range patternsConfig {
		patternMap, ok := pc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid pattern configuration at index %d", i)
		}

		patternID, _ := patternMap["patternId"].(string)
		if patternID == "" {
			return fmt.Errorf("patternId not specified at index %d", i)
		}

		// Extract event types
		var eventTypes []string
		eventTypesList, _ := patternMap["eventTypes"].([]interface{})
		for _, et := range eventTypesList {
			if etStr, ok := et.(string); ok {
				eventTypes = append(eventTypes, etStr)
			}
		}

		// Parse window time
		windowTimeStr, _ := patternMap["windowTime"].(string)
		windowTime, err := time.ParseDuration(windowTimeStr)
		if err != nil {
			return fmt.Errorf("invalid window time for pattern '%s': %w", patternID, err)
		}

		condition, _ := patternMap["condition"].(string)
		minOccurs, _ := patternMap["minOccurs"].(int)
		maxOccurs, _ := patternMap["maxOccurs"].(int)
		orderMatters, _ := patternMap["orderMatters"].(bool)
		extraParams, _ := patternMap["extraParams"].(map[string]interface{})

		// Create the pattern
		pattern := &module.EventPattern{
			PatternID:    patternID,
			EventTypes:   eventTypes,
			WindowTime:   windowTime,
			Condition:    condition,
			MinOccurs:    minOccurs,
			MaxOccurs:    maxOccurs,
			OrderMatters: orderMatters,
			ExtraParams:  extraParams,
		}

		processor.AddPattern(pattern)
	}

	// Configure handlers
	handlersConfig, _ := eventConfig["handlers"].([]interface{})
	for i, hc := range handlersConfig {
		handlerMap, ok := hc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid handler configuration at index %d", i)
		}

		patternID, _ := handlerMap["patternId"].(string)
		handlerName, _ := handlerMap["handler"].(string)

		if patternID == "" || handlerName == "" {
			return fmt.Errorf("incomplete handler configuration at index %d", i)
		}

		// Get the handler service
		var handlerSvc interface{}
		_ = app.GetService(handlerName, &handlerSvc)
		if handlerSvc == nil {
			return fmt.Errorf("handler service '%s' not found", handlerName)
		}

		// Check if the service implements EventHandler
		eventHandler, ok := handlerSvc.(module.EventHandler)
		if !ok {
			// Try adapting a message handler to an event handler
			if msgHandler, ok := handlerSvc.(module.MessageHandler); ok {
				eventHandler = h.adaptMessageHandler(msgHandler)
			} else {
				return fmt.Errorf("service '%s' does not implement EventHandler interface", handlerName)
			}
		}

		// Register the handler
		if err := processor.RegisterHandler(patternID, eventHandler); err != nil {
			return fmt.Errorf("failed to register handler for pattern '%s': %w", patternID, err)
		}
	}

	// Configure message broker adapters if any
	adaptersConfig, _ := eventConfig["adapters"].([]interface{})
	for i, ac := range adaptersConfig {
		adapterMap, ok := ac.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid adapter configuration at index %d", i)
		}

		brokerName, _ := adapterMap["broker"].(string)
		eventType, _ := adapterMap["eventType"].(string)

		if brokerName == "" || eventType == "" {
			return fmt.Errorf("incomplete adapter configuration at index %d", i)
		}

		// Get topics
		var topics []string
		topicsList, _ := adapterMap["topics"].([]interface{})
		for _, t := range topicsList {
			if tStr, ok := t.(string); ok {
				topics = append(topics, tStr)
			}
		}

		// Get the broker service
		var brokerSvc interface{}
		_ = app.GetService(brokerName, &brokerSvc)
		if brokerSvc == nil {
			return fmt.Errorf("broker service '%s' not found", brokerName)
		}

		broker, ok := brokerSvc.(module.MessageBroker)
		if !ok {
			return fmt.Errorf("service '%s' is not a MessageBroker", brokerName)
		}

		// Source and correlation ID keys (optional)
		sourceIdKey, _ := adapterMap["sourceIdKey"].(string)
		correlIdKey, _ := adapterMap["correlIdKey"].(string)

		// Create a message handler that converts messages to events
		msgHandler := h.createMessageToEventAdapter(processor, eventType, sourceIdKey, correlIdKey)

		// Subscribe to all topics
		for _, topic := range topics {
			if err := broker.Subscribe(topic, msgHandler); err != nil {
				return fmt.Errorf("failed to subscribe to topic '%s': %w", topic, err)
			}
		}
	}

	return nil
}

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *EventWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	// For event workflows, the action represents the event processor or a specific pattern
	// Format: processor:pattern or just processor
	processorName := action

	if parts := strings.Split(action, ":"); len(parts) > 1 {
		processorName = parts[0]
	}

	// If no processor name specified, look for one in the data
	if processorName == "" {
		if procName, ok := data["processor"].(string); ok {
			processorName = procName
		}
	}

	// Get the application from context
	var app modular.Application
	if appVal := ctx.Value("application"); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	// Get the event processor
	var processor *module.EventProcessor
	err := app.GetService(processorName, &processor)
	if err != nil || processor == nil {
		return nil, fmt.Errorf("event processor '%s' not found: %v", processorName, err)
	}

	// Create an event from the data
	eventType := "custom.event"
	if evtType, ok := data["eventType"].(string); ok {
		eventType = evtType
	} else if evtType, ok = data["type"].(string); ok {
		eventType = evtType
	}

	sourceID := ""
	if srcID, ok := data["sourceId"].(string); ok {
		sourceID = srcID
	} else if srcID, ok = data["id"].(string); ok {
		sourceID = srcID
	} else if srcID, ok = data["userId"].(string); ok {
		sourceID = srcID
	}

	correlID := ""
	if corrID, ok := data["correlationId"].(string); ok {
		correlID = corrID
	}

	// Create the event
	event := module.EventData{
		EventType:  eventType,
		SourceID:   sourceID,
		CorrelID:   correlID,
		Timestamp:  time.Now(),
		Data:       data,
		RawMessage: nil, // We don't have raw message bytes here
	}

	// Process the event normally
	err = processor.ProcessEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("error processing event: %w", err)
	}

	return map[string]interface{}{
		"success":   true,
		"eventType": eventType,
		"sourceId":  sourceID,
		"correlId":  correlID,
		"processed": true,
	}, nil
}

// adaptMessageHandler adapts a message handler to handle event patterns
func (h *EventWorkflowHandler) adaptMessageHandler(msgHandler module.MessageHandler) module.EventHandler {
	return module.NewFunctionHandler(func(ctx context.Context, match module.PatternMatch) error {
		// Create a JSON payload from the pattern match
		payload := []byte(fmt.Sprintf(`{"patternId": "%s", "eventsCount": %d}`, match.PatternID, len(match.Events)))
		return msgHandler.HandleMessage(payload)
	})
}

// createMessageToEventAdapter creates a handler that converts messages to events
func (h *EventWorkflowHandler) createMessageToEventAdapter(processor *module.EventProcessor, eventType, sourceIdKey, correlIdKey string) module.MessageHandler {
	return module.NewFunctionMessageHandler(func(message []byte) error {
		// Parse message as JSON
		var data map[string]interface{}
		if err := json.Unmarshal(message, &data); err != nil {
			return fmt.Errorf("failed to parse message as JSON: %w", err)
		}

		// Extract source ID and correlation ID if keys are provided
		var sourceID, correlID string
		if sourceIdKey != "" {
			if src, ok := data[sourceIdKey].(string); ok {
				sourceID = src
			}
		}

		if correlIdKey != "" {
			if corr, ok := data[correlIdKey].(string); ok {
				correlID = corr
			}
		}

		// Create event from message
		event := &module.EventData{
			EventType:  eventType,
			SourceID:   sourceID,
			CorrelID:   correlID,
			Timestamp:  time.Now(),
			Data:       data,
			RawMessage: message,
		}

		// Process the event
		return processor.ProcessEvent(context.Background(), *event)
	})
}

// EventProcessorAdapter adapts an EventProcessor to ensure interface compatibility
type EventProcessorAdapter struct {
	Processor *module.EventProcessor
}

// HandleEvent implements the expected interface for EventProcessor
func (a *EventProcessorAdapter) HandleEvent(ctx context.Context, event interface{}) error {
	// Convert the event to EventData
	eventData := module.EventData{
		EventType: "generic.event",
		Timestamp: time.Now(),
		SourceID:  "unknown",
		Data:      make(map[string]interface{}),
	}

	// Try to extract data from different event formats
	switch e := event.(type) {
	case map[string]interface{}:
		eventData.Data = e
		// Try to extract event type and source ID if available
		if eventType, ok := e["eventType"].(string); ok {
			eventData.EventType = eventType
		}
		if sourceID, ok := e["sourceId"].(string); ok {
			eventData.SourceID = sourceID
		} else if sourceID, ok := e["userId"].(string); ok {
			eventData.SourceID = sourceID
		}
	case []byte:
		// Try to parse JSON
		var data map[string]interface{}
		if err := json.Unmarshal(e, &data); err != nil {
			return fmt.Errorf("failed to parse event as JSON: %w", err)
		}
		eventData.Data = data
		eventData.RawMessage = e
	case string:
		// Try to parse JSON
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(e), &data); err != nil {
			return fmt.Errorf("failed to parse event as JSON: %w", err)
		}
		eventData.Data = data
		eventData.RawMessage = []byte(e)
	//case module.PatternMatch:
	// This is a direct pattern match, forward it to any registered handlers
	//return a.Processor.NotifyPatternMatch(ctx, e)
	default:
		return fmt.Errorf("unsupported event type: %T", event)
	}

	// Process the event through the processor - this part was missing
	return a.Processor.ProcessEvent(ctx, eventData)
}

// RegisterEventProcessor creates and registers an EventProcessorAdapter for the given event processor
func RegisterEventProcessor(app modular.Application, processor *module.EventProcessor) error {
	adapter := &EventProcessorAdapter{
		Processor: processor,
	}

	// Register the adapter with the processor's name
	return app.RegisterService(processor.Name(), adapter)
}
