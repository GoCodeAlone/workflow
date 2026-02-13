package module

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
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

// InstanceID returns the workflow instance ID
// This method is provided for backward compatibility with code that expects an InstanceID field
func (e TransitionEvent) InstanceID() string {
	return e.WorkflowID
}

// TransitionHandler handles workflow state transitions
type TransitionHandler interface {
	HandleTransition(ctx context.Context, event TransitionEvent) error
}

type TransitionTrigger interface {
	TriggerTransition(ctx context.Context, workflowID, transitionName string, data map[string]interface{}) error
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
	persistence       *PersistenceStore // optional write-through backend
	wg                sync.WaitGroup    // tracks in-flight goroutines
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

// Stop stops the state machine engine. It waits for in-flight goroutines to
// finish (or context to expire) and flushes all instances to persistence.
func (e *StateMachineEngine) Stop(ctx context.Context) error {
	// Wait for in-flight goroutines or context timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-ctx.Done():
		// Timeout — proceed with flush anyway
	}

	// Flush all instances to persistence
	if e.persistence != nil {
		e.mutex.RLock()
		instances := make([]*WorkflowInstance, 0, len(e.instances))
		for _, inst := range e.instances {
			instances = append(instances, inst)
		}
		e.mutex.RUnlock()

		for _, inst := range instances {
			_ = e.persistence.SaveWorkflowInstance(inst)
		}
	}

	return nil
}

// SetPersistence sets the optional write-through persistence backend.
func (e *StateMachineEngine) SetPersistence(ps *PersistenceStore) {
	e.persistence = ps
}

// TrackGoroutine spawns a goroutine tracked by the engine's WaitGroup so
// that Stop() can drain in-flight work before shutdown.
func (e *StateMachineEngine) TrackGoroutine(fn func()) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		fn()
	}()
}

// LoadAllPersistedInstances loads workflow instances from persistence for all
// registered definition types and populates the in-memory maps. Instances that
// already exist in memory are skipped.
func (e *StateMachineEngine) LoadAllPersistedInstances() error {
	if e.persistence == nil {
		return nil
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	for defName, def := range e.definitions {
		instances, err := e.persistence.LoadWorkflowInstances(defName)
		if err != nil {
			return fmt.Errorf("failed to load instances for %q: %w", defName, err)
		}

		for _, inst := range instances {
			// Skip instances that already exist in memory
			if _, exists := e.instances[inst.ID]; exists {
				continue
			}

			// Warn if the instance's current state doesn't exist in the definition
			if _, stateExists := def.States[inst.CurrentState]; !stateExists {
				log.Printf("WARNING: Orphaned workflow instance %s has state %q not in current %q definition", inst.ID, inst.CurrentState, defName)
			}

			e.instances[inst.ID] = inst

			if _, ok := e.instancesByType[inst.WorkflowType]; !ok {
				e.instancesByType[inst.WorkflowType] = make([]string, 0)
			}
			e.instancesByType[inst.WorkflowType] = append(e.instancesByType[inst.WorkflowType], inst.ID)
		}
	}

	return nil
}

// GetOrphanedInstances returns workflow instances whose current state does not
// exist in the corresponding state machine definition. This helps operators
// discover instances affected by configuration drift.
func (e *StateMachineEngine) GetOrphanedInstances() []*WorkflowInstance {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	var orphaned []*WorkflowInstance
	for _, inst := range e.instances {
		def, ok := e.definitions[inst.WorkflowType]
		if !ok {
			// No definition at all — consider it orphaned
			orphaned = append(orphaned, inst)
			continue
		}
		if _, stateExists := def.States[inst.CurrentState]; !stateExists {
			orphaned = append(orphaned, inst)
		}
	}
	return orphaned
}

// RecoverProcessingInstances finds instances stuck in intermediate processing
// states and re-triggers their transitions so processing can resume after a
// restart. It resets each stuck instance back to PreviousState and re-fires
// the transition that originally moved it into the processing state.
func (e *StateMachineEngine) RecoverProcessingInstances(ctx context.Context, processingStates []string) int {
	if len(processingStates) == 0 {
		return 0
	}

	stateSet := make(map[string]bool, len(processingStates))
	for _, s := range processingStates {
		stateSet[s] = true
	}

	e.mutex.RLock()
	// Collect instances that need recovery
	type recoveryItem struct {
		instanceID     string
		previousState  string
		currentState   string
		workflowType   string
		transitionName string
		data           map[string]interface{}
	}
	var toRecover []recoveryItem

	for _, inst := range e.instances {
		if inst.Completed || !stateSet[inst.CurrentState] || inst.PreviousState == "" {
			continue
		}

		// Find the transition that goes from PreviousState to CurrentState
		def, ok := e.definitions[inst.WorkflowType]
		if !ok {
			continue
		}

		var transName string
		for tName, trans := range def.Transitions {
			if trans.FromState == inst.PreviousState && trans.ToState == inst.CurrentState {
				transName = tName
				break
			}
		}
		if transName == "" {
			continue
		}

		dataCopy := make(map[string]interface{})
		for k, v := range inst.Data {
			dataCopy[k] = v
		}

		toRecover = append(toRecover, recoveryItem{
			instanceID:     inst.ID,
			previousState:  inst.PreviousState,
			currentState:   inst.CurrentState,
			workflowType:   inst.WorkflowType,
			transitionName: transName,
			data:           dataCopy,
		})
	}
	e.mutex.RUnlock()

	// Reset state and re-trigger transitions
	for _, item := range toRecover {
		e.mutex.Lock()
		if inst, ok := e.instances[item.instanceID]; ok {
			inst.CurrentState = item.previousState
			inst.LastUpdated = time.Now()
		}
		e.mutex.Unlock()

		// Persist the reset state
		if e.persistence != nil {
			if inst, ok := e.instances[item.instanceID]; ok {
				_ = e.persistence.SaveWorkflowInstance(inst)
			}
		}

		// Re-trigger the transition asynchronously
		instanceID := item.instanceID
		transName := item.transitionName
		data := item.data
		e.TrackGoroutine(func() {
			_ = e.TriggerTransition(ctx, instanceID, transName, data)
		})
	}

	return len(toRecover)
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
	e.instances[id] = instance

	// Add to type index
	if _, ok := e.instancesByType[workflowType]; !ok {
		e.instancesByType[workflowType] = make([]string, 0)
	}
	e.instancesByType[workflowType] = append(e.instancesByType[workflowType], id)
	e.mutex.Unlock()

	// Write-through to persistence
	if e.persistence != nil {
		_ = e.persistence.SaveWorkflowInstance(instance)
	}

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

	// Record the old state before any changes
	oldState := instance.CurrentState

	// Create a transition event (uses FromState/ToState so handlers see
	// the intended transition without needing instance updated first)
	now := time.Now()
	event := TransitionEvent{
		WorkflowID:   workflowID,
		TransitionID: transitionName,
		FromState:    oldState,
		ToState:      transition.ToState,
		Timestamp:    now,
		Data:         data,
	}

	// Call the transition handler BEFORE committing the state change.
	// If the handler fails, the instance state remains unchanged.
	if e.transitionHandler != nil {
		// Call handler outside of the mutex lock to prevent deadlocks
		e.mutex.Unlock()
		err := e.transitionHandler.HandleTransition(ctx, event)
		e.mutex.Lock() // Re-acquire lock
		if err != nil {
			return fmt.Errorf("transition handler failed: %w", err)
		}
	}

	// Handler succeeded (or none set) — now commit the state change
	instance.PreviousState = oldState
	instance.CurrentState = transition.ToState
	instance.LastUpdated = now

	// Merge data if provided
	for k, v := range data {
		instance.Data[k] = v
	}

	// Check if the workflow is now in a final state
	if state, ok := def.States[transition.ToState]; ok && state.IsFinal {
		instance.Completed = true
		if state.IsError {
			instance.Error = "Workflow ended in error state"
		}
	}

	// Write-through to persistence after state commit
	if e.persistence != nil {
		_ = e.persistence.SaveWorkflowInstance(instance)
	}

	// Check for auto-transform transitions from the new state.
	// If any transition has AutoTransform=true and its FromState matches
	// the current state, fire it asynchronously to continue the pipeline.
	if !instance.Completed {
		for autoName, autoTrans := range def.Transitions {
			if autoTrans.AutoTransform && autoTrans.FromState == instance.CurrentState {
				autoTransName := autoName
				autoData := make(map[string]interface{})
				for k, v := range instance.Data {
					autoData[k] = v
				}
				e.TrackGoroutine(func() {
					_ = e.TriggerTransition(ctx, workflowID, autoTransName, autoData)
				})
				break // Only fire one auto-transition per state entry
			}
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

// TransitionListener is a function that gets called when a transition occurs
type TransitionListener func(event TransitionEvent)

// AddTransitionListener registers a function to be called on every transition
func (e *StateMachineEngine) AddTransitionListener(listener TransitionListener) {
	// Create a transition handler that will call our listener
	if !e.HasTransitionHandler() {
		// Create a composite handler if there isn't one already
		e.SetTransitionHandler(NewCompositeTransitionHandler())
	}

	// Get the existing handler and cast to composite if possible
	handler := e.GetTransitionHandler()
	if composite, ok := handler.(*CompositeTransitionHandler); ok {
		// Add our listener adapter to the composite handler
		composite.AddHandler(NewListenerAdapter(listener))
	} else {
		// Create a new composite handler with the existing handler and our listener
		composite := NewCompositeTransitionHandler()
		composite.AddHandler(handler)                      // Add the existing handler
		composite.AddHandler(NewListenerAdapter(listener)) // Add our listener
		e.SetTransitionHandler(composite)
	}
}

// GetTransitionHandler returns the current transition handler
func (e *StateMachineEngine) GetTransitionHandler() TransitionHandler {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.transitionHandler
}

// AddGlobalTransitionHandler adds a handler for all transitions
func (e *StateMachineEngine) AddGlobalTransitionHandler(handler TransitionHandler) {
	if !e.HasTransitionHandler() {
		// If no handler exists, just set this one
		e.SetTransitionHandler(handler)
		return
	}

	// Get the existing handler
	existingHandler := e.GetTransitionHandler()

	// If it's already a composite, add to it
	if composite, ok := existingHandler.(*CompositeTransitionHandler); ok {
		composite.AddHandler(handler)
	} else {
		// Create a new composite with both handlers
		composite := NewCompositeTransitionHandler()
		composite.AddHandler(existingHandler)
		composite.AddHandler(handler)
		e.SetTransitionHandler(composite)
	}
}

// ListenerAdapter adapts a TransitionListener function to a TransitionHandler
type ListenerAdapter struct {
	listener TransitionListener
}

// NewListenerAdapter creates a new adapter for a transition listener
func NewListenerAdapter(listener TransitionListener) *ListenerAdapter {
	return &ListenerAdapter{
		listener: listener,
	}
}

// HandleTransition implements the TransitionHandler interface
func (a *ListenerAdapter) HandleTransition(ctx context.Context, event TransitionEvent) error {
	// Call the listener function
	a.listener(event)
	// Listeners don't return errors
	return nil
}

// CompositeTransitionHandler combines multiple transition handlers
type CompositeTransitionHandler struct {
	handlers []TransitionHandler
	mutex    sync.RWMutex
}

// NewCompositeTransitionHandler creates a new composite handler
func NewCompositeTransitionHandler() *CompositeTransitionHandler {
	return &CompositeTransitionHandler{
		handlers: make([]TransitionHandler, 0),
	}
}

// AddHandler adds a handler to the composite
func (c *CompositeTransitionHandler) AddHandler(handler TransitionHandler) {
	if handler == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.handlers = append(c.handlers, handler)
}

// HandleTransition calls all handlers in sequence
func (c *CompositeTransitionHandler) HandleTransition(ctx context.Context, event TransitionEvent) error {
	c.mutex.RLock()
	handlers := make([]TransitionHandler, len(c.handlers))
	copy(handlers, c.handlers)
	c.mutex.RUnlock()

	// Call all handlers in sequence
	for _, handler := range handlers {
		if err := handler.HandleTransition(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

// GetAllInstances returns all workflow instances
func (e *StateMachineEngine) GetAllInstances() ([]*WorkflowInstance, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// Create a slice with all instances
	instances := make([]*WorkflowInstance, 0, len(e.instances))
	for _, instance := range e.instances {
		instances = append(instances, instance)
	}

	return instances, nil
}

// RegisterWorkflow registers a workflow definition
func (e *StateMachineEngine) RegisterWorkflow(def ExternalStateMachineDefinition) error {
	// Convert from the external configuration struct to our internal representation
	internalDef := &StateMachineDefinition{
		Name:         def.ID,
		Description:  def.Description,
		InitialState: def.InitialState,
		States:       make(map[string]*State),
		Transitions:  make(map[string]*Transition),
		Data:         make(map[string]interface{}),
	}

	// Process states
	for stateID, stateConfig := range def.States {
		internalDef.States[stateID] = &State{
			Name:        stateID,
			Description: stateConfig.Description,
			IsFinal:     stateConfig.IsFinal,
			IsError:     stateConfig.IsError,
			Data:        stateConfig.Data,
		}
	}

	// Process transitions
	for transID, transConfig := range def.Transitions {
		internalDef.Transitions[transID] = &Transition{
			Name:          transID,
			FromState:     transConfig.FromState,
			ToState:       transConfig.ToState,
			Condition:     transConfig.Condition,
			AutoTransform: transConfig.AutoTransform,
			Data:          transConfig.Data,
		}
	}

	// Register the definition
	return e.RegisterDefinition(internalDef)
}

// StateMachineStateConfig represents configuration for a state machine state
type StateMachineStateConfig struct {
	ID          string                 `json:"id" yaml:"id"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	IsFinal     bool                   `json:"isFinal" yaml:"isFinal"`
	IsError     bool                   `json:"isError" yaml:"isError"`
	Data        map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// StateMachineTransitionConfig represents configuration for a state transition
type StateMachineTransitionConfig struct {
	ID            string                 `json:"id" yaml:"id"`
	FromState     string                 `json:"fromState" yaml:"fromState"`
	ToState       string                 `json:"toState" yaml:"toState"`
	Condition     string                 `json:"condition,omitempty" yaml:"condition,omitempty"`
	AutoTransform bool                   `json:"autoTransform" yaml:"autoTransform"`
	Data          map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// ExternalStateMachineDefinition is used for registering state machines from configuration
type ExternalStateMachineDefinition struct {
	ID           string                                  `json:"id" yaml:"id"`
	Description  string                                  `json:"description,omitempty" yaml:"description,omitempty"`
	InitialState string                                  `json:"initialState" yaml:"initialState"`
	States       map[string]StateMachineStateConfig      `json:"states" yaml:"states"`
	Transitions  map[string]StateMachineTransitionConfig `json:"transitions" yaml:"transitions"`
}
