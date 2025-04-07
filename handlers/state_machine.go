package handlers

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// Standard handler name
const (
	StateMachineWorkflowHandlerName = "workflow.handler.statemachine"
)

// StateMachineWorkflowConfig represents a state machine workflow configuration
type StateMachineWorkflowConfig struct {
	Engine      string                   `json:"engine" yaml:"engine"`
	Definitions []StateMachineDefinition `json:"definitions" yaml:"definitions"`
	Hooks       []StateMachineHookConfig `json:"hooks,omitempty" yaml:"hooks,omitempty"`
}

// StateMachineDefinition represents a state machine definition
type StateMachineDefinition struct {
	Name         string                            `json:"name" yaml:"name"`
	Description  string                            `json:"description,omitempty" yaml:"description,omitempty"`
	InitialState string                            `json:"initialState" yaml:"initialState"`
	States       map[string]StateMachineState      `json:"states" yaml:"states"`
	Transitions  map[string]StateMachineTransition `json:"transitions" yaml:"transitions"`
}

// StateMachineState represents a workflow state
type StateMachineState struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	IsFinal     bool                   `json:"isFinal" yaml:"isFinal"`
	IsError     bool                   `json:"isError" yaml:"isError"`
	Data        map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// StateMachineTransition represents a transition between states
type StateMachineTransition struct {
	FromState     string                 `json:"fromState" yaml:"fromState"`
	ToState       string                 `json:"toState" yaml:"toState"`
	Condition     string                 `json:"condition,omitempty" yaml:"condition,omitempty"`
	AutoTransform bool                   `json:"autoTransform" yaml:"autoTransform"`
	Data          map[string]interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

// StateMachineHookConfig represents a hook configuration for state transitions
type StateMachineHookConfig struct {
	WorkflowType string   `json:"workflowType" yaml:"workflowType"`
	Transitions  []string `json:"transitions,omitempty" yaml:"transitions,omitempty"`
	FromStates   []string `json:"fromStates,omitempty" yaml:"fromStates,omitempty"`
	ToStates     []string `json:"toStates,omitempty" yaml:"toStates,omitempty"`
	Handler      string   `json:"handler" yaml:"handler"`
}

// StateMachineWorkflowHandler handles state machine workflows
type StateMachineWorkflowHandler struct {
	name      string
	namespace module.ModuleNamespaceProvider
}

// NewStateMachineWorkflowHandler creates a new state machine workflow handler
func NewStateMachineWorkflowHandler() *StateMachineWorkflowHandler {
	return NewStateMachineWorkflowHandlerWithNamespace(nil)
}

// NewStateMachineWorkflowHandlerWithNamespace creates a state machine workflow handler with namespace support
func NewStateMachineWorkflowHandlerWithNamespace(namespace module.ModuleNamespaceProvider) *StateMachineWorkflowHandler {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = module.NewStandardNamespace("", "")
	}

	return &StateMachineWorkflowHandler{
		name:      namespace.FormatName(StateMachineWorkflowHandlerName),
		namespace: namespace,
	}
}

// Name returns the name of this handler
func (h *StateMachineWorkflowHandler) Name() string {
	return h.name
}

// CanHandle returns true if this handler can process the given workflow type
func (h *StateMachineWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "statemachine"
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *StateMachineWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
	// Convert the generic config to state machine-specific config
	config, ok := workflowConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid state machine workflow configuration format")
	}

	// Get the state machine engine name
	engineName, _ := config["engine"].(string)
	if engineName == "" {
		return fmt.Errorf("state machine engine name not specified")
	}

	// Apply namespace to engine name if needed
	if h.namespace != nil {
		engineName = h.namespace.ResolveDependency(engineName)
	}

	// Get the state machine engine
	var engineSvc interface{}
	err := app.GetService(engineName, &engineSvc)
	if err != nil || engineSvc == nil {
		return fmt.Errorf("state machine engine '%s' not found: %v", engineName, err)
	}

	engine, ok := engineSvc.(*module.StateMachineEngine)
	if !ok {
		return fmt.Errorf("service '%s' is not a StateMachineEngine", engineName)
	}

	// Configure workflow definitions
	definitions, _ := config["definitions"].([]interface{})
	if len(definitions) == 0 {
		return fmt.Errorf("no state machine definitions provided")
	}

	for i, defConfig := range definitions {
		defMap, ok := defConfig.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid definition at index %d", i)
		}

		// Extract basic definition properties
		name, _ := defMap["name"].(string)
		if name == "" {
			return fmt.Errorf("definition at index %d has no name", i)
		}

		description, _ := defMap["description"].(string)
		initialState, _ := defMap["initialState"].(string)
		if initialState == "" {
			return fmt.Errorf("definition '%s' has no initial state", name)
		}

		// Extract states
		statesConfig, _ := defMap["states"].(map[string]interface{})
		if len(statesConfig) == 0 {
			return fmt.Errorf("definition '%s' has no states", name)
		}

		states := make(map[string]*module.State)
		for stateName, stateConfig := range statesConfig {
			stateMap, ok := stateConfig.(map[string]interface{})
			if !ok {
				return fmt.Errorf("invalid state '%s' in definition '%s'", stateName, name)
			}

			stateDesc, _ := stateMap["description"].(string)
			isFinal, _ := stateMap["isFinal"].(bool)
			isError, _ := stateMap["isError"].(bool)

			state := &module.State{
				Name:        stateName,
				Description: stateDesc,
				IsFinal:     isFinal,
				IsError:     isError,
			}
			states[stateName] = state
		}

		// Extract transitions
		transitionsConfig, _ := defMap["transitions"].(map[string]interface{})
		if len(transitionsConfig) == 0 {
			return fmt.Errorf("definition '%s' has no transitions", name)
		}

		transitions := make(map[string]*module.Transition)

		for transName, transConfig := range transitionsConfig {
			transMap, ok := transConfig.(map[string]interface{})
			if !ok {
				return fmt.Errorf("invalid transition '%s' in definition '%s'", transName, name)
			}

			fromState, _ := transMap["fromState"].(string)
			toState, _ := transMap["toState"].(string)
			condition, _ := transMap["condition"].(string)
			autoTransform, _ := transMap["autoTransform"].(bool)

			if fromState == "" || toState == "" {
				return fmt.Errorf("transition '%s' has incomplete state information", transName)
			}

			transition := &module.Transition{
				Name:          transName,
				FromState:     fromState,
				ToState:       toState,
				Condition:     condition,
				AutoTransform: autoTransform,
			}
			transitions[transName] = transition
		}

		// Create the state machine definition
		definition := &module.StateMachineDefinition{
			Name:         name,
			Description:  description,
			States:       states,
			Transitions:  transitions,
			InitialState: initialState,
			Data:         make(map[string]interface{}),
		}

		// Register the definition with the engine
		err := engine.RegisterDefinition(definition)
		if err != nil {
			return fmt.Errorf("failed to register state machine definition '%s': %w", name, err)
		}
	}

	// Configure hooks
	hooks, _ := config["hooks"].([]interface{})
	for i, hookConfig := range hooks {
		hookMap, ok := hookConfig.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid hook at index %d", i)
		}

		// Get handler service name
		handlerName, _ := hookMap["handler"].(string)
		if handlerName == "" {
			return fmt.Errorf("hook at index %d has no handler", i)
		}

		// Apply namespace to handler name
		if h.namespace != nil {
			handlerName = h.namespace.ResolveDependency(handlerName)
		}

		// Get the handler service
		var handlerSvc interface{}
		err := app.GetService(handlerName, &handlerSvc)
		if err != nil || handlerSvc == nil {
			return fmt.Errorf("handler '%s' not found: %v", handlerName, err)
		}

		// Check if it's a TransitionHandler
		handler, ok := handlerSvc.(module.TransitionHandler)
		if !ok {
			return fmt.Errorf("service '%s' is not a TransitionHandler", handlerName)
		}

		// Register the handler with the engine
		engine.SetTransitionHandler(handler)
	}

	// If there are multiple hooks, create a composite handler
	if len(hooks) > 1 {
		compositeHandler := h.createCompositeTransitionHandler(app, hooks)
		engine.SetTransitionHandler(compositeHandler)
	}

	return nil
}

// createCompositeTransitionHandler creates a handler that manages multiple hooks
func (h *StateMachineWorkflowHandler) createCompositeTransitionHandler(
	app modular.Application,
	hooksConfig []interface{},
) module.TransitionHandler {
	return module.NewFunctionTransitionHandler(func(ctx context.Context, event module.TransitionEvent) error {
		for i, hookConfig := range hooksConfig {
			hookMap, ok := hookConfig.(map[string]interface{})
			if !ok {
				fmt.Printf("Invalid hook configuration at index %d\n", i)
				continue
			}

			// Check for workflow type match
			workflowType, _ := hookMap["workflowType"].(string)
			if workflowType != "" && workflowType != event.WorkflowID {
				// Skip hooks for other workflow types
				continue
			}

			// Check for transition match
			transMatch := false
			var transitions []string
			transList, _ := hookMap["transitions"].([]interface{})
			for _, t := range transList {
				if tStr, ok := t.(string); ok {
					transitions = append(transitions, tStr)
				}
			}

			if len(transitions) == 0 {
				transMatch = true // No restrictions
			} else {
				for _, t := range transitions {
					if t == event.TransitionID {
						transMatch = true
						break
					}
				}
			}

			if !transMatch {
				continue
			}

			// Check for from state match
			fromMatch := false
			var fromStates []string
			fromList, _ := hookMap["fromStates"].([]interface{})
			for _, f := range fromList {
				if fStr, ok := f.(string); ok {
					fromStates = append(fromStates, fStr)
				}
			}

			if len(fromStates) == 0 {
				fromMatch = true // No restrictions
			} else {
				for _, f := range fromStates {
					if f == event.FromState {
						fromMatch = true
						break
					}
				}
			}

			if !fromMatch {
				continue
			}

			// Check for to state match
			toMatch := false
			var toStates []string
			toList, _ := hookMap["toStates"].([]interface{})
			for _, t := range toList {
				if tStr, ok := t.(string); ok {
					toStates = append(toStates, tStr)
				}
			}

			if len(toStates) == 0 {
				toMatch = true // No restrictions
			} else {
				for _, t := range toStates {
					if t == event.ToState {
						toMatch = true
						break
					}
				}
			}

			if !toMatch {
				continue
			}

			// All criteria match - get the handler
			handlerName, _ := hookMap["handler"].(string)
			if handlerName == "" {
				continue
			}

			// Apply namespace to handler name
			if h.namespace != nil {
				handlerName = h.namespace.ResolveDependency(handlerName)
			}

			// Get the handler service
			var handlerSvc interface{}
			_ = app.GetService(handlerName, &handlerSvc)
			if handlerSvc == nil {
				fmt.Printf("Handler service '%s' not found\n", handlerName)
				continue
			}

			// Try different handler types
			if trHandler, ok := handlerSvc.(module.TransitionHandler); ok {
				// Direct transition handler
				if err := trHandler.HandleTransition(ctx, event); err != nil {
					return err
				}
			} else if msgHandler, ok := handlerSvc.(module.MessageHandler); ok {
				// Adapt message handler
				payload := []byte(fmt.Sprintf(`{"workflowId":"%s","transition":"%s","fromState":"%s","toState":"%s"}`,
					event.WorkflowID, event.TransitionID, event.FromState, event.ToState))
				if err := msgHandler.HandleMessage(payload); err != nil {
					return err
				}
			} else {
				fmt.Printf("Handler service '%s' does not implement any supported handler interface\n", handlerName)
				continue
			}
		}

		return nil
	})
}
