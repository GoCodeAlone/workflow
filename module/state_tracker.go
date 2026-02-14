package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// StateTrackerName is the standard name for the state tracker service
const StateTrackerName = "workflow.service.statetracker"

// StateInfo represents state information for a resource
type StateInfo struct {
	ID            string         `json:"id"`
	ResourceType  string         `json:"resourceType"`
	CurrentState  string         `json:"currentState"`
	PreviousState string         `json:"previousState,omitempty"`
	LastUpdate    time.Time      `json:"lastUpdate"`
	Data          map[string]any `json:"data,omitempty"`
}

// StateChangeListener is a function that gets called when state changes
type StateChangeListener func(previousState, newState string, resourceID string, data map[string]any)

// StateTracker provides a generic service for tracking state
type StateTracker struct {
	name      string
	states    map[string]StateInfo             // key is resourceType:resourceID
	listeners map[string][]StateChangeListener // key is resourceType
	mu        sync.RWMutex
	app       modular.Application
}

// NewStateTracker creates a new state tracker service
func NewStateTracker(name string) *StateTracker {
	if name == "" {
		name = StateTrackerName
	}

	return &StateTracker{
		name:      name,
		states:    make(map[string]StateInfo),
		listeners: make(map[string][]StateChangeListener),
	}
}

// Name returns the service name
func (s *StateTracker) Name() string {
	return s.name
}

// Init initializes the service
func (s *StateTracker) Init(app modular.Application) error {
	s.app = app
	return nil
}

// Start starts the service
func (s *StateTracker) Start(ctx context.Context) error {
	return nil // Nothing to start
}

// Stop stops the service
func (s *StateTracker) Stop(ctx context.Context) error {
	return nil // Nothing to stop
}

// GetState retrieves state information for a resource
func (s *StateTracker) GetState(resourceType, resourceID string) (StateInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", resourceType, resourceID)
	info, exists := s.states[key]
	return info, exists
}

// SetState updates the state for a resource
func (s *StateTracker) SetState(resourceType, resourceID, state string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s:%s", resourceType, resourceID)
	previousState := ""

	// Check if we have existing state
	if existing, exists := s.states[key]; exists {
		previousState = existing.CurrentState
	}

	// Update the state
	s.states[key] = StateInfo{
		ID:            resourceID,
		ResourceType:  resourceType,
		CurrentState:  state,
		PreviousState: previousState,
		LastUpdate:    time.Now(),
		Data:          data,
	}

	// Notify listeners if the state changed
	if previousState != state {
		s.notifyListeners(resourceType, previousState, state, resourceID, data)
	}
}

// AddStateChangeListener adds a listener for state changes of a specific resource type
func (s *StateTracker) AddStateChangeListener(resourceType string, listener StateChangeListener) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.listeners[resourceType] = append(s.listeners[resourceType], listener)
}

// notifyListeners calls all registered listeners for a resource type
func (s *StateTracker) notifyListeners(resourceType, previousState, newState, resourceID string, data map[string]any) {
	// Get the listeners - we need to copy the slice to avoid locking during callback
	var listeners []StateChangeListener
	if typeListeners, exists := s.listeners[resourceType]; exists {
		listeners = append(listeners, typeListeners...)
	}

	// Also notify wildcard listeners
	if wildcardListeners, exists := s.listeners["*"]; exists {
		listeners = append(listeners, wildcardListeners...)
	}

	// Call listeners without holding the lock
	for _, listener := range listeners {
		go listener(previousState, newState, resourceID, data)
	}
}

// ProvidesServices returns the services provided by this module
func (s *StateTracker) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        s.name,
			Description: "State tracking service for workflow resources",
			Instance:    s,
		},
	}
}

// RequiresServices returns the services required by this module
func (s *StateTracker) RequiresServices() []modular.ServiceDependency {
	return nil // No dependencies
}
