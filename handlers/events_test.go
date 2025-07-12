package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// Variable to track if pattern was matched in test
var patternMatchDetected bool



// TestEventWorkflow tests the event workflow handler
func TestEventWorkflow(t *testing.T) {
	// Reset the pattern detection flag
	patternMatchDetected = false

	// Create a new app for testing with logging
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create a simple message broker
	broker := module.NewInMemoryMessageBroker("event-broker")
	err = app.RegisterService("event-broker", broker)
	if err != nil {
		t.Fatalf("Failed to register broker: %v", err)
	}

	// Create and register event processor
	processor := module.NewEventProcessor("event-processor")
	err = app.RegisterService("event-processor", processor)
	if err != nil {
		t.Fatalf("Failed to register processor: %v", err)
	}

	// Add an event pattern directly to the processor
	pattern := &module.EventPattern{
		PatternID:    "test-pattern",
		EventTypes:   []string{"test.event"},
		WindowTime:   3 * time.Second, // Make the window larger for reliability
		Condition:    "count",
		MinOccurs:    3,
		MaxOccurs:    0,
		OrderMatters: false,
	}
	processor.AddPattern(pattern)

	// Initialize the modules
	err = app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app with modules: %v", err)
	}

	// Register our handler directly to catch pattern matches
	handlerCalled := make(chan struct{})
	handler := module.NewFunctionHandler(func(ctx context.Context, match module.PatternMatch) error {
		fmt.Printf("Pattern matched! Events count: %d\n", len(match.Events))
		for i, evt := range match.Events {
			fmt.Printf("  Event %d: type=%s, source=%s\n", i, evt.EventType, evt.SourceID)
		}
		patternMatchDetected = true
		close(handlerCalled)
		return nil
	})

	// Register the handler directly with the pattern ID
	err = processor.RegisterHandler("test-pattern", handler)
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Start all modules
	err = app.Start()
	if err != nil {
		t.Fatalf("Failed to start modules: %v", err)
	}

	// Create message-to-event adapter manually
	msgToEventAdapter := module.NewFunctionMessageHandler(func(message []byte) error {
		// Parse message
		var data map[string]interface{}
		if err := json.Unmarshal(message, &data); err != nil {
			return fmt.Errorf("invalid message format: %w", err)
		}

		// Extract source ID
		sourceID, _ := data["userId"].(string)

		// Create event
		event := module.EventData{
			EventType:  "test.event",
			Timestamp:  time.Now(),
			SourceID:   sourceID,
			Data:       data,
			RawMessage: message,
		}

		fmt.Printf("Processing event: %s, sourceID: %s\n", event.EventType, event.SourceID)

		// Send event to processor
		return processor.ProcessEvent(context.Background(), event)
	})

	// Subscribe handler to broker topic
	err = broker.Subscribe("test-topic", msgToEventAdapter)
	if err != nil {
		t.Fatalf("Failed to subscribe handler: %v", err)
	}

	// Send test events
	for i := 0; i < 3; i++ {
		msg := []byte(fmt.Sprintf(`{"userId":"123","eventType":"test.event","data":"test-%d"}`, i))
		err = broker.SendMessage("test-topic", msg)
		if err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}
		// Add small delay between messages
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for pattern match with timeout
	select {
	case <-handlerCalled:
		// Success! Pattern was detected
	case <-time.After(2 * time.Second):
		// Dump all events in the processor buffer for debugging
		fmt.Println("DEBUG - Pattern not detected in time. Current processor state:")
		dumpProcessorEvents(processor)
		t.Errorf("Expected event pattern to be detected")
	}

	// Stop all modules
	err = app.Stop()
	if err != nil {
		t.Fatalf("Failed to stop modules: %v", err)
	}
}

// Helper function to dump processor events for debugging
func dumpProcessorEvents(processor *module.EventProcessor) {
	// Access the internal event buffer for debugging
	value := reflect.ValueOf(processor).Elem()
	bufferField := value.FieldByName("eventBuffer")

	if bufferField.IsValid() {
		fmt.Println("Event buffer found in processor")
		// We can't directly access unexported fields, but in a real debug scenario
		// you would add an exported method to dump the state
	}

	// Also dump all patterns
	patternsField := value.FieldByName("patterns")
	if patternsField.IsValid() {
		fmt.Printf("Number of patterns: %d\n", patternsField.Len())
	}
}

// MockEventProcessor is a mock implementation of module.EventProcessor for testing
type MockEventProcessor struct {
	HandleEventFn func(ctx context.Context, event interface{}) error
	patterns      []*module.EventPattern
	handlers      map[string]module.EventHandler
}

// Name returns the name of the processor
func (p *MockEventProcessor) Name() string {
	return "event-processor"
}

// Init initializes the mock processor
func (p *MockEventProcessor) Init(app modular.Application) error {
	return app.RegisterService("event-processor", p)
}

// Start implements module.Module interface
func (p *MockEventProcessor) Start(ctx context.Context) error {
	return nil
}

// Stop implements module.Module interface
func (p *MockEventProcessor) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices implements module.Module interface
func (p *MockEventProcessor) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "event-processor",
			Description: "Mock Event Processor",
			Instance:    p,
		},
	}
}

// RequiresServices implements module.Module interface
func (p *MockEventProcessor) RequiresServices() []modular.ServiceDependency {
	return nil
}

// HandleEvent handles an event
func (p *MockEventProcessor) HandleEvent(ctx context.Context, event interface{}) error {
	if p.HandleEventFn != nil {
		return p.HandleEventFn(ctx, event)
	}
	return nil
}

// ProcessEvent processes a new event
func (p *MockEventProcessor) ProcessEvent(ctx context.Context, event module.EventData) error {
	return nil
}

// AddPattern adds a pattern to the processor
func (p *MockEventProcessor) AddPattern(pattern *module.EventPattern) {
	if p.patterns == nil {
		p.patterns = make([]*module.EventPattern, 0)
	}
	p.patterns = append(p.patterns, pattern)
}

// RegisterHandler registers a handler for a pattern
func (p *MockEventProcessor) RegisterHandler(patternID string, handler module.EventHandler) error {
	if p.handlers == nil {
		p.handlers = make(map[string]module.EventHandler)
	}
	p.handlers[patternID] = handler
	return nil
}

// Service provides access to a named service
func (p *MockEventProcessor) Service(name string) interface{} {
	if name == "event-processor" {
		return p
	}
	return nil
}

// GetService implements service lookup
func (p *MockEventProcessor) GetService(name string, out interface{}) error {
	if name == "event-processor" {
		switch outPtr := out.(type) {
		case **MockEventProcessor:
			*outPtr = p
		}
	}
	return nil
}

// Services returns all services
func (p *MockEventProcessor) Services() map[string]interface{} {
	services := make(map[string]interface{})
	services["event-processor"] = p
	return services
}

// EventProcessorAdapter code has been moved to events.go
// Do not redeclare it here
