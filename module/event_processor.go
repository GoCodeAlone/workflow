package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoCodeAlone/modular"
)

// EventPattern defines a pattern for matching complex event sequences
type EventPattern struct {
	PatternID    string                 `json:"patternId" yaml:"patternId"`
	EventTypes   []string               `json:"eventTypes" yaml:"eventTypes"`
	WindowTime   time.Duration          `json:"windowTime" yaml:"windowTime"`
	Condition    string                 `json:"condition" yaml:"condition"`
	MinOccurs    int                    `json:"minOccurs" yaml:"minOccurs"`
	MaxOccurs    int                    `json:"maxOccurs" yaml:"maxOccurs"`
	OrderMatters bool                   `json:"orderMatters" yaml:"orderMatters"`
	ExtraParams  map[string]interface{} `json:"extraParams,omitempty" yaml:"extraParams,omitempty"`
}

// EventData represents an event in the system
type EventData struct {
	EventType  string                 `json:"eventType"`
	Timestamp  time.Time              `json:"timestamp"`
	SourceID   string                 `json:"sourceId"`
	CorrelID   string                 `json:"correlId,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	RawMessage []byte                 `json:"-"`
}

// PatternMatch represents a successful pattern match
type PatternMatch struct {
	PatternID   string      `json:"patternId"`
	Events      []EventData `json:"events"`
	MatchedTime time.Time   `json:"matchedTime"`
}

// EventHandler processes matched event patterns
type EventHandler interface {
	HandlePattern(ctx context.Context, match PatternMatch) error
}

// EventProcessor processes complex event patterns
type EventProcessor struct {
	name           string
	patterns       []*EventPattern
	eventBuffer    map[string][]EventData // correlID -> events
	handlers       map[string]EventHandler
	bufferLock     sync.RWMutex
	processingLock sync.Mutex
	err            error               // Add an error field to support the error interface
	appContext     modular.Application // Store application context for service access
}

// NewEventProcessor creates a new complex event processor
func NewEventProcessor(name string) *EventProcessor {
	return &EventProcessor{
		name:        name,
		patterns:    make([]*EventPattern, 0),
		eventBuffer: make(map[string][]EventData),
		handlers:    make(map[string]EventHandler),
		bufferLock:  sync.RWMutex{},
	}
}

// Name returns the module name
func (p *EventProcessor) Name() string {
	return p.name
}

// Init initializes the event processor
func (p *EventProcessor) Init(app modular.Application) error {
	// Store the application context
	p.appContext = app
	// Register ourselves as a service
	return app.RegisterService(p.name, p)
}

// Start starts the event processor
func (p *EventProcessor) Start(ctx context.Context) error {
	// Start background cleanup of old events
	go p.periodicCleanup()
	return nil
}

// Stop stops the event processor
func (p *EventProcessor) Stop(ctx context.Context) error {
	// Nothing to do for now
	return nil
}

// AddPattern adds a new event pattern to monitor
func (p *EventProcessor) AddPattern(pattern *EventPattern) {
	p.patterns = append(p.patterns, pattern)
}

// RegisterHandler registers a handler for a specific pattern
func (p *EventProcessor) RegisterHandler(patternID string, handler EventHandler) error {
	// Check if pattern exists
	found := false
	for _, pattern := range p.patterns {
		if pattern.PatternID == patternID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("pattern with ID '%s' not found", patternID)
	}

	p.handlers[patternID] = handler
	return nil
}

// ProcessEvent processes a new event and checks for pattern matches
func (p *EventProcessor) ProcessEvent(ctx context.Context, event EventData) error {
	correlID := event.CorrelID
	if correlID == "" {
		correlID = event.SourceID
	}

	// Store the event
	p.bufferLock.Lock()
	if _, exists := p.eventBuffer[correlID]; !exists {
		p.eventBuffer[correlID] = make([]EventData, 0)
	}
	p.eventBuffer[correlID] = append(p.eventBuffer[correlID], event)
	p.bufferLock.Unlock()

	// Process patterns
	return p.processPatterns(ctx, correlID)
}

// processPatterns checks for pattern matches
func (p *EventProcessor) processPatterns(ctx context.Context, correlID string) error {
	p.processingLock.Lock()
	defer p.processingLock.Unlock()

	p.bufferLock.RLock()
	events, exists := p.eventBuffer[correlID]
	p.bufferLock.RUnlock()

	if !exists || len(events) == 0 {
		return nil
	}

	// For each pattern, check for matches
	for _, pattern := range p.patterns {
		matches := p.checkPatternMatch(events, pattern)

		// Debug info
		if len(matches) > 0 {
			fmt.Printf("Found %d matches for pattern %s\n", len(matches), pattern.PatternID)
		}

		// Process matches with handlers
		for _, match := range matches {
			if handler, exists := p.handlers[pattern.PatternID]; exists {
				fmt.Printf("Calling handler for pattern %s with %d events\n",
					pattern.PatternID, len(match.Events))
				if err := handler.HandlePattern(ctx, match); err != nil {
					return fmt.Errorf("error handling pattern match: %w", err)
				}
			} else {
				fmt.Printf("No handler registered for pattern %s\n", pattern.PatternID)
			}
		}
	}

	return nil
}

// checkPatternMatch checks if a sequence of events matches a pattern
func (p *EventProcessor) checkPatternMatch(events []EventData, pattern *EventPattern) []PatternMatch {
	var matches []PatternMatch

	// Build a sliding window based on the pattern's time window
	now := time.Now()
	windowStart := now.Add(-pattern.WindowTime)

	// Filter events by time window and type
	var windowedEvents []EventData
	for _, event := range events {
		if event.Timestamp.After(windowStart) && p.eventMatchesType(event, pattern.EventTypes) {
			windowedEvents = append(windowedEvents, event)
		}
	}

	// Basic count check (for simple patterns)
	eventCount := len(windowedEvents)

	// Check if we meet the minimal occurrence requirement
	if eventCount >= pattern.MinOccurs && (pattern.MaxOccurs == 0 || eventCount <= pattern.MaxOccurs) {
		// Handle different condition types
		if pattern.Condition == "count" || pattern.Condition == "all" || pattern.Condition == "sequence" || pattern.Condition == "" {
			// For count condition, we just need enough events of the specified types
			match := PatternMatch{
				PatternID:   pattern.PatternID,
				Events:      windowedEvents,
				MatchedTime: now,
			}
			matches = append(matches, match)
		}
	}

	return matches
}

// eventMatchesType checks if an event matches any of the specified types
func (p *EventProcessor) eventMatchesType(event EventData, types []string) bool {
	for _, t := range types {
		if event.EventType == t {
			return true
		}
	}
	return false
}

// periodicCleanup removes old events from the buffer
func (p *EventProcessor) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		p.cleanupOldEvents()
	}
}

// cleanupOldEvents removes events older than the longest pattern window
func (p *EventProcessor) cleanupOldEvents() {
	// Find the longest window time among all patterns
	var maxWindow time.Duration
	for _, pattern := range p.patterns {
		if pattern.WindowTime > maxWindow {
			maxWindow = pattern.WindowTime
		}
	}

	// Add some buffer time
	maxWindow = maxWindow + (5 * time.Minute)
	cutoffTime := time.Now().Add(-maxWindow)

	p.bufferLock.Lock()
	defer p.bufferLock.Unlock()

	// Remove old events
	for correlID, events := range p.eventBuffer {
		var newEvents []EventData
		for _, event := range events {
			if event.Timestamp.After(cutoffTime) {
				newEvents = append(newEvents, event)
			}
		}

		if len(newEvents) > 0 {
			p.eventBuffer[correlID] = newEvents
		} else {
			delete(p.eventBuffer, correlID)
		}
	}
}

// FunctionHandler is a simple EventHandler that executes a function
type FunctionHandler struct {
	handleFunc func(ctx context.Context, match PatternMatch) error
}

// NewFunctionHandler creates a new function-based event handler
func NewFunctionHandler(fn func(ctx context.Context, match PatternMatch) error) *FunctionHandler {
	return &FunctionHandler{
		handleFunc: fn,
	}
}

// HandlePattern handles a pattern match by calling the function
func (h *FunctionHandler) HandlePattern(ctx context.Context, match PatternMatch) error {
	return h.handleFunc(ctx, match)
}

// ProvidesServices returns services provided by this processor
func (p *EventProcessor) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        p.name,
			Description: "Complex Event Processor",
			Instance:    p,
		},
	}
}

// RequiresServices returns services required by this processor
func (p *EventProcessor) RequiresServices() []modular.ServiceDependency {
	// No external dependencies
	return nil
}

// Error returns the last error from the processor - implements the error interface
func (p *EventProcessor) Error() string {
	if p.err != nil {
		return p.err.Error()
	}
	return ""
}

// SetError sets the processor error
func (p *EventProcessor) SetError(err error) {
	p.err = err
}

// GetService implements the service functionality expected by handlers.
// It follows the modular.Application interface signature
func (p *EventProcessor) GetService(name string, out interface{}) error {
	// Get the service from the application context
	if p.appContext != nil {
		_ = p.appContext.GetService(name, out)
	}

	if name == p.name && out != nil {
		// If someone is asking for us by name, return ourselves
		switch outPtr := out.(type) {
		case **EventProcessor:
			*outPtr = p
		}
	}

	return nil
}

// Service provides access to a named service
func (p *EventProcessor) Service(name string) interface{} {
	var result interface{}
	if err := p.GetService(name, &result); err != nil {
		// Return nil if service not found - this is expected behavior
		return nil
	}
	return result
}

// Services returns a map of all available services
func (p *EventProcessor) Services() map[string]interface{} {
	// Create a map of services that are registered
	services := make(map[string]interface{})
	// Add self to the services map
	services[p.name] = p
	return services
}
