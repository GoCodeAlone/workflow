package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoCodeAlone/modular"
)

// Standard module name constants
const (
	StateMachineEngineName = "statemachine.engine"
)

// State represents a workflow state
type State struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
	IsFinal     bool                   `json:"isFinal" yaml:"isFinal"`
	IsError     bool                   `json:"isError" yaml:"isError"`
}

// Transition defines a possible state transition
type Transition struct {
	Name          string                 `json:"name" yaml:"name"`
	FromState     string                 `json:"fromState" yaml:"fromState"`
	ToState       string                 `json:"toState" yaml:"toState"`
	Condition     string                 `json:"condition,omitempty" yaml:"condition,omitempty"`
	AutoTransform bool                   `json:"autoTransform" yaml:"autoTransform"`
	Data          map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// TransitionEvent represents a state transition event
type TransitionEvent struct {
	WorkflowID   string                 `json:"workflowId"`
	TransitionID string                 `json:"transitionId"`
	FromState    string                 `json:"fromState"`
	ToState      string                 `json:"toState"`
	Timestamp    time.Time              `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

// TransitionHandler handles workflow state transitions
type TransitionHandler interface {
	HandleTransition(ctx context.Context, event TransitionEvent) error
}

// WorkflowInstance represents an instance of a state machine workflow
type WorkflowInstance struct {
	ID            string                 `json:"id"`
	WorkflowType  string                 `json:"workflowType"`
	CurrentState  string                 `json:"currentState"`
	PreviousState string                 `json:"previousState"`
	Data          map[string]interface{} `json:"data"`
	StartTime     time.Time              `json:"startTime"`
	LastUpdated   time.Time              `json:"lastUpdated"`
	Completed     bool                   `json:"completed"`
	Error         string                 `json:"error,omitempty"`
}

// StateMachineDefinition defines a state machine workflow
type StateMachineDefinition struct {
	Name         string                 `json:"name" yaml:"name"`
	Description  string                 `json:"description,omitempty" yaml:"description,omitempty"`
	States       map[string]*State      `json:"states" yaml:"states"`
	Transitions  map[string]*Transition `json:"transitions" yaml:"transitions"`
	InitialState string                 `json:"initialState" yaml:"initialState"`
	Data         map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// StateMachineEngine implements a workflow state machine engine
type StateMachineEngine struct {
	name              string
	namespace         ModuleNamespaceProvider
	definitions       map[string]*StateMachineDefinition
	instances         map[string]*WorkflowInstance
	instancesByType   map[string][]string // workflowType -> []instanceID
	transitionHandler TransitionHandler
	mutex             sync.RWMutex
}

// NewStateMachineEngine creates a new state machine engine
func NewStateMachineEngine(name string) *StateMachineEngine {
	return NewStateMachineEngineWithNamespace(name, nil)
}

// NewStateMachineEngineWithNamespace creates a new state machine engine with namespace support
func NewStateMachineEngineWithNamespace(name string, namespace ModuleNamespaceProvider) *StateMachineEngine {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	// Format the name using the namespace
	formattedName := namespace.FormatName(name)

	return &StateMachineEngine{
		name:            formattedName,
		namespace:       namespace,
		definitions:     make(map[string]*StateMachineDefinition),
		instances:       make(map[string]*WorkflowInstance),
		instancesByType: make(map[string][]string),
	}
}

// NewStandardStateMachineEngine creates a state machine engine with the standard name
func NewStandardStateMachineEngine(namespace ModuleNamespaceProvider) *StateMachineEngine {
	return NewStateMachineEngineWithNamespace(StateMachineEngineName, namespace)
}

// Name returns the module name
func (e *StateMachineEngine) Name() string {
	return e.name
}

// Init initializes the state machine engine
func (e *StateMachineEngine) Init(app modular.Application) error {
	return nil
}

// Start starts the state machine engine
func (e *StateMachineEngine) Start(ctx context.Context) error {
	return nil
}

// Stop stops the state machine engine
func (e *StateMachineEngine) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns services provided by this module
func (e *StateMachineEngine) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        e.name,
			Description: "State Machine Engine",
			Instance:    e,
		},
	}
}

// RequiresServices returns services required by this module
func (e *StateMachineEngine) RequiresServices() []modular.ServiceDependency {
	return nil
}

// RegisterDefinition registers a state machine definition
func (e *StateMachineEngine) RegisterDefinition(def *StateMachineDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("state machine definition must have a name")
	}

	if len(def.States) == 0 {
		return fmt.Errorf("state machine definition must have at least one state")
	}

	if _, ok := def.States[def.InitialState]; !ok {
		return fmt.Errorf("initial state '%s' not found in states definition", def.InitialState)
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.definitions[def.Name] = def
	return nil
}

// SetTransitionHandler sets the handler for all state transitions
func (e *StateMachineEngine) SetTransitionHandler(handler TransitionHandler) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.transitionHandler = handler
}

// HasTransitionHandler checks if a transition handler is set
func (e *StateMachineEngine) HasTransitionHandler() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.transitionHandler != nil
}

// CreateWorkflow creates a new workflow instance
func (e *StateMachineEngine) CreateWorkflow(
	workflowType string,
	id string,
	initialData map[string]interface{},
) (*WorkflowInstance, error) {
	// Find the definition
	e.mutex.RLock()
	def, ok := e.definitions[workflowType]
	e.mutex.RUnlock()

	if !ok {
		return nil, fmt.Errorf("workflow type '%s' not found", workflowType)
	}

	// Create the instance
	now := time.Now()
	instance := &WorkflowInstance{
		ID:           id,
		WorkflowType: workflowType,
		CurrentState: def.InitialState,
		StartTime:    now,
		LastUpdated:  now,
		Data:         make(map[string]interface{}),
	}

	// Copy initial data
	for k, v := range initialData {
		instance.Data[k] = v
	}

	// Store the instance
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.instances[id] = instance

	// Add to type index
	if _, ok := e.instancesByType[workflowType]; !ok {
		e.instancesByType[workflowType] = make([]string, 0)
	}
	e.instancesByType[workflowType] = append(e.instancesByType[workflowType], id)

	return instance, nil
}

// GetInstance retrieves a workflow instance by ID
func (e *StateMachineEngine) GetInstance(id string) (*WorkflowInstance, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	instance, ok := e.instances[id]
	if !ok {
		return nil, fmt.Errorf("workflow instance with ID '%s' not found", id)
	}

	return instance, nil
}

// GetInstancesByType retrieves workflow instances by type
func (e *StateMachineEngine) GetInstancesByType(workflowType string) ([]*WorkflowInstance, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	ids, ok := e.instancesByType[workflowType]
	if !ok {
		return nil, fmt.Errorf("no instances found for workflow type '%s'", workflowType)
	}

	instances := make([]*WorkflowInstance, 0, len(ids))
	for _, id := range ids {
		if instance, ok := e.instances[id]; ok {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// TriggerTransition attempts to transition a workflow's state
func (e *StateMachineEngine) TriggerTransition(
	ctx context.Context,
	workflowID string,
	transitionName string,
	data map[string]interface{},
) error {
	// Get the workflow instance
	e.mutex.Lock()
	defer e.mutex.Unlock()

	instance, ok := e.instances[workflowID]
	if !ok {
		return fmt.Errorf("workflow instance '%s' not found", workflowID)
	}

	// Find the workflow definition
	def, ok := e.definitions[instance.WorkflowType]
	if !ok {
		return fmt.Errorf("workflow definition '%s' not found", instance.WorkflowType)
	}

	// Find the transition
	transition, ok := def.Transitions[transitionName]
	if !ok {
		return fmt.Errorf("transition '%s' not found in workflow '%s'", transitionName, instance.WorkflowType)
	}

	// Check if the current state matches the transition's from state
	if instance.CurrentState != transition.FromState {
		return fmt.Errorf("cannot trigger transition '%s' from state '%s', expected '%s'",
			transitionName, instance.CurrentState, transition.FromState)
	}

	// Apply the transition
	oldState := instance.CurrentState
	instance.PreviousState = oldState
	instance.CurrentState = transition.ToState
	instance.LastUpdated = time.Now()

	// Merge data if provided
	if data != nil {
		for k, v := range data {
			instance.Data[k] = v
		}
	}

	// Create a transition event
	event := TransitionEvent{
		WorkflowID:   workflowID,
		TransitionID: transitionName,
		FromState:    oldState,
		ToState:      transition.ToState,
		Timestamp:    instance.LastUpdated,
		Data:         data,
	}

	// Call the transition handler if one exists
	if e.transitionHandler != nil {
		// Call handler outside of the mutex lock to prevent deadlocks
		e.mutex.Unlock()
		err := e.transitionHandler.HandleTransition(ctx, event)
		e.mutex.Lock() // Re-acquire lock
		if err != nil {
			return fmt.Errorf("transition handler failed: %w", err)
		}
	}

	// Check if the workflow is now in a final state
	if state, ok := def.States[transition.ToState]; ok && state.IsFinal {
		instance.Completed = true
		if state.IsError {
			instance.Error = "Workflow ended in error state"
		}
	}

	return nil
}

// FunctionTransitionHandler is a simple TransitionHandler that executes a function
type FunctionTransitionHandler struct {
	handlerFunc func(ctx context.Context, event TransitionEvent) error
}

// NewFunctionTransitionHandler creates a new function-based transition handler
func NewFunctionTransitionHandler(fn func(ctx context.Context, event TransitionEvent) error) *FunctionTransitionHandler {
	return &FunctionTransitionHandler{
		handlerFunc: fn,
	}
}

// HandleTransition handles a state transition by calling the function
func (h *FunctionTransitionHandler) HandleTransition(ctx context.Context, event TransitionEvent) error {
	return h.handlerFunc(ctx, event)
}
