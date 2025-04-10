package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// StateMachineStateConnectorName is the standard service name
const StateMachineStateConnectorName = "workflow.connector.statemachine"

// ResourceStateMapping defines how a resource maps to a state machine
type ResourceStateMapping struct {
	ResourceType  string // Type of resource (e.g., "orders", "users")
	StateMachine  string // Name of the state machine
	InstanceIDKey string // Field in resource data that maps to state machine instance ID
}

// StateMachineStateConnector connects state machines to state tracking
type StateMachineStateConnector struct {
	name          string
	mappings      []ResourceStateMapping
	stateTracker  *StateTracker
	stateMachines map[string]*StateMachineEngine // name -> engine
	app           modular.Application
}

// NewStateMachineStateConnector creates a new connector
func NewStateMachineStateConnector(name string) *StateMachineStateConnector {
	if name == "" {
		name = StateMachineStateConnectorName
	}

	return &StateMachineStateConnector{
		name:          name,
		mappings:      make([]ResourceStateMapping, 0),
		stateMachines: make(map[string]*StateMachineEngine),
	}
}

// Name returns the service name
func (c *StateMachineStateConnector) Name() string {
	return c.name
}

// Init initializes the connector
func (c *StateMachineStateConnector) Init(app modular.Application) error {
	c.app = app
	return nil
}

// Configure sets up the connector with resource mappings
func (c *StateMachineStateConnector) Configure(mappings []ResourceStateMapping) error {
	c.mappings = mappings
	return nil
}

// RegisterMapping adds a resource mapping
func (c *StateMachineStateConnector) RegisterMapping(resourceType, stateMachine, instanceIDKey string) {
	c.mappings = append(c.mappings, ResourceStateMapping{
		ResourceType:  resourceType,
		StateMachine:  stateMachine,
		InstanceIDKey: instanceIDKey,
	})
}

// Start connects to state machines and sets up listeners
func (c *StateMachineStateConnector) Start(ctx context.Context) error {
	// Find all state machine engines
	for name, svc := range c.app.SvcRegistry() {
		if engine, ok := svc.(*StateMachineEngine); ok {
			c.stateMachines[name] = engine
		}
	}

	// Find the state tracker service
	var stateTrackerSvc interface{}
	err := c.app.GetService(StateTrackerName, &stateTrackerSvc)
	if err != nil || stateTrackerSvc == nil {
		// Try to find by scanning all services
		for _, svc := range c.app.SvcRegistry() {
			if tracker, ok := svc.(*StateTracker); ok {
				stateTrackerSvc = tracker
				break
			}
		}
	}

	if stateTrackerSvc == nil {
		return fmt.Errorf("state tracker service not found")
	}

	var ok bool
	c.stateTracker, ok = stateTrackerSvc.(*StateTracker)
	if !ok {
		return fmt.Errorf("invalid state tracker service type")
	}

	// Set up transition listeners for each state machine
	for engineName, engine := range c.stateMachines {
		// Create a listener for this engine
		engine.AddTransitionListener(func(event TransitionEvent) {
			// Find mappings that use this state machine
			for _, mapping := range c.mappings {
				if mapping.StateMachine == engineName ||
					strings.HasSuffix(engineName, "."+mapping.StateMachine) {
					// When a transition occurs, update the state tracker
					c.stateTracker.SetState(
						mapping.ResourceType,
						event.InstanceID(),
						event.ToState,
						event.Data,
					)
				}
			}
		})
	}

	// Set up initial state for all existing instances
	for _, mapping := range c.mappings {
		if engine, ok := c.findStateMachineByName(mapping.StateMachine); ok {
			// Get all instances for this engine
			instances, err := engine.GetAllInstances()
			if err == nil {
				for _, instance := range instances {
					// Set the initial state in the tracker
					c.stateTracker.SetState(
						mapping.ResourceType,
						instance.ID,
						instance.CurrentState,
						instance.Data,
					)
				}
			}
		}
	}

	return nil
}

// findStateMachineByName finds a state machine engine by name or suffix
func (c *StateMachineStateConnector) findStateMachineByName(name string) (*StateMachineEngine, bool) {
	// Try exact match first
	if engine, ok := c.stateMachines[name]; ok {
		return engine, true
	}

	// Try suffix match
	for engineName, engine := range c.stateMachines {
		if strings.HasSuffix(engineName, "."+name) {
			return engine, true
		}
	}

	return nil, false
}

// Stop stops the connector
func (c *StateMachineStateConnector) Stop(ctx context.Context) error {
	return nil // Nothing to stop
}

// UpdateResourceState gets the current state from the state machine and updates the tracker
func (c *StateMachineStateConnector) UpdateResourceState(resourceType, resourceID string) error {
	// Find mapping for this resource type
	var mapping *ResourceStateMapping
	for i, m := range c.mappings {
		if m.ResourceType == resourceType {
			mapping = &c.mappings[i]
			break
		}
	}

	if mapping == nil {
		return fmt.Errorf("no mapping found for resource type: %s", resourceType)
	}

	// Find the state machine
	engine, ok := c.findStateMachineByName(mapping.StateMachine)
	if !ok {
		return fmt.Errorf("state machine not found: %s", mapping.StateMachine)
	}

	// Get instance state
	instance, err := engine.GetInstance(resourceID)
	if err != nil {
		return fmt.Errorf("failed to get state machine instance: %w", err)
	}

	// Update the state tracker
	c.stateTracker.SetState(
		resourceType,
		resourceID,
		instance.CurrentState,
		instance.Data,
	)

	return nil
}

// GetResourceState gets the current state for a resource
func (c *StateMachineStateConnector) GetResourceState(resourceType, resourceID string) (string, map[string]interface{}, error) {
	// Check if we have state info in the tracker
	stateInfo, exists := c.stateTracker.GetState(resourceType, resourceID)
	if exists {
		return stateInfo.CurrentState, stateInfo.Data, nil
	}

	// If not in tracker, try to fetch from state machine
	err := c.UpdateResourceState(resourceType, resourceID)
	if err != nil {
		return "", nil, err
	}

	// Now it should be in the tracker
	stateInfo, exists = c.stateTracker.GetState(resourceType, resourceID)
	if exists {
		return stateInfo.CurrentState, stateInfo.Data, nil
	}

	return "", nil, fmt.Errorf("resource state not found")
}

// ProvidesServices returns the services provided by this module
func (c *StateMachineStateConnector) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        c.name,
			Description: "Connector between state machines and state tracking",
			Instance:    c,
		},
	}
}

// RequiresServices returns the services required by this module
func (c *StateMachineStateConnector) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{
			Name:     StateTrackerName,
			Required: true,
		},
	}
}
