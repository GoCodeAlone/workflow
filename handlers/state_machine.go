package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
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
	smConfig, ok := workflowConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid state machine workflow configuration")
	}

	// Extract engine name
	engineName, _ := smConfig["engine"].(string)
	if engineName == "" {
		return fmt.Errorf("state machine engine name not specified in configuration")
	}

	// Find the state machine engine
	var engineSvc interface{}
	err := app.GetService(engineName, &engineSvc)
	if err != nil {
		return fmt.Errorf("state machine engine '%s' not found: %w", engineName, err)
	}

	smEngine, ok := engineSvc.(*module.StateMachineEngine)
	if !ok {
		return fmt.Errorf("service '%s' is not a StateMachineEngine", engineName)
	}

	// Get the custom state tracker name if specified in the engine module config
	var stateTrackerName string
	if engines, ok := smConfig["modules"].([]interface{}); ok {
		for _, engineModule := range engines {
			if engineConfig, ok := engineModule.(map[string]interface{}); ok {
				if name, _ := engineConfig["name"].(string); name == engineName {
					if config, ok := engineConfig["config"].(map[string]interface{}); ok {
						if trackerRef, ok := config["stateTracker"].(string); ok && trackerRef != "" {
							stateTrackerName = trackerRef
							break
						}
					}
				}
			}
		}
	}

	// Create or find a state tracker
	var stateTracker *module.StateTracker
	var stateTrackerSvc interface{}

	if stateTrackerName != "" {
		// Try to find the custom state tracker by name
		err := app.GetService(stateTrackerName, &stateTrackerSvc)
		if err != nil || stateTrackerSvc == nil {
			return fmt.Errorf("specified state tracker '%s' not found", stateTrackerName)
		}

		var ok bool
		stateTracker, ok = stateTrackerSvc.(*module.StateTracker)
		if !ok {
			return fmt.Errorf("service '%s' is not a StateTracker", stateTrackerName)
		}
		// Use stateTracker here or later in the function
		_ = stateTracker
	} else {
		// Try to find the default state tracker
		err := app.GetService(module.StateTrackerName, &stateTrackerSvc)
		if err == nil && stateTrackerSvc != nil {
			// State tracker already exists, use it
			var ok bool
			stateTracker, ok = stateTrackerSvc.(*module.StateTracker)
			if !ok {
				return fmt.Errorf("service '%s' is not a StateTracker", module.StateTrackerName)
			}
			// Use stateTracker here or later in the function
			_ = stateTracker
		} else {
			// State tracker doesn't exist, create a new one
			stateTracker = module.NewStateTracker("")
			app.RegisterModule(stateTracker)

			// Initialize the newly created tracker
			if err = stateTracker.Init(app); err != nil {
				return fmt.Errorf("failed to initialize state tracker: %w", err)
			}

			// Register services from ProvidesServices (since app.Init already ran)
			if svcAware, ok := interface{}(stateTracker).(modular.ServiceAware); ok {
				for _, svc := range svcAware.ProvidesServices() {
					_ = app.RegisterService(svc.Name, svc.Instance)
				}
			}
		}
	}

	// Create a state connector if not already registered
	var stateConnector *module.StateMachineStateConnector
	var stateConnectorSvc interface{}
	err = app.GetService(module.StateMachineStateConnectorName, &stateConnectorSvc)
	if err == nil && stateConnectorSvc != nil {
		// State connector already exists, use it
		var ok bool
		stateConnector, ok = stateConnectorSvc.(*module.StateMachineStateConnector)
		if !ok {
			return fmt.Errorf("service '%s' is not a StateMachineStateConnector", module.StateMachineStateConnectorName)
		}
	} else {
		// State connector doesn't exist, create a new one
		stateConnector = module.NewStateMachineStateConnector("")
		app.RegisterModule(stateConnector)

		// Initialize the newly created connector
		if err = stateConnector.Init(app); err != nil {
			return fmt.Errorf("failed to initialize state connector: %w", err)
		}

		// Register services from ProvidesServices (since app.Init already ran)
		if svcAware, ok := interface{}(stateConnector).(modular.ServiceAware); ok {
			for _, svc := range svcAware.ProvidesServices() {
				_ = app.RegisterService(svc.Name, svc.Instance)
			}
		}
	}

	// Process resource mappings if provided
	if mappings, ok := smConfig["resourceMappings"].([]interface{}); ok {
		for _, mappingObj := range mappings {
			if mapping, ok := mappingObj.(map[string]interface{}); ok {
				resourceType, _ := mapping["resourceType"].(string)
				stateMachine, _ := mapping["stateMachine"].(string)
				instanceIDKey, _ := mapping["instanceIDKey"].(string)

				if resourceType != "" && stateMachine != "" {
					if instanceIDKey == "" {
						instanceIDKey = "id" // Default to "id"
					}

					stateConnector.RegisterMapping(resourceType, stateMachine, instanceIDKey)
				}
			}
		}
	} else {
		// Automatically create mappings based on workflow definitions
		if definitions, ok := smConfig["definitions"].([]interface{}); ok {
			for _, defObj := range definitions {
				if def, ok := defObj.(map[string]interface{}); ok {
					name, _ := def["name"].(string)
					if name != "" {
						// Create a default mapping using the workflow name as resource type
						resourceType := strings.ReplaceAll(name, "-", "_")
						stateConnector.RegisterMapping(resourceType, engineName, "id")
					}
				}
			}
		}
	}

	// Process workflow definitions
	definitions, ok := smConfig["definitions"].([]interface{})
	if !ok {
		return fmt.Errorf("workflow definitions not specified in configuration")
	}

	// Configure each workflow definition
	for i, defObj := range definitions {
		def, ok := defObj.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid workflow definition at index %d", i)
		}

		// Extract basic workflow properties
		name, _ := def["name"].(string)
		description, _ := def["description"].(string)
		initialState, _ := def["initialState"].(string)

		if name == "" || initialState == "" {
			return fmt.Errorf("workflow definition at index %d missing required fields", i)
		}

		// Process states
		states := make(map[string]module.StateMachineStateConfig)
		if stateConfigs, ok := def["states"].(map[string]interface{}); ok {
			for stateID, stateObj := range stateConfigs {
				stateMap, ok := stateObj.(map[string]interface{})
				if !ok {
					return fmt.Errorf("invalid state configuration for state '%s'", stateID)
				}

				// Create state config with default values
				stateConfig := module.StateMachineStateConfig{
					ID:          stateID,
					Description: "",    // Default empty description
					IsFinal:     false, // Default value
					IsError:     false, // Default value
				}

				// Safely extract description with a default empty string
				if desc, ok := stateMap["description"].(string); ok {
					stateConfig.Description = desc
				}

				// Safely extract boolean values with proper nil checks
				if isFinalVal, exists := stateMap["isFinal"]; exists {
					if isFinal, ok := isFinalVal.(bool); ok {
						stateConfig.IsFinal = isFinal
					}
				}

				if isErrorVal, exists := stateMap["isError"]; exists {
					if isError, ok := isErrorVal.(bool); ok {
						stateConfig.IsError = isError
					}
				}

				// Add custom data if provided
				if stateData, ok := stateMap["data"].(map[string]interface{}); ok {
					stateConfig.Data = stateData
				}

				states[stateID] = stateConfig
			}
		}

		// Process transitions
		transitions := make(map[string]module.StateMachineTransitionConfig)
		if transitionConfigs, ok := def["transitions"].(map[string]interface{}); ok {
			for transID, transObj := range transitionConfigs {
				transMap, ok := transObj.(map[string]interface{})
				if !ok {
					return fmt.Errorf("invalid transition configuration for transition '%s'", transID)
				}

				fromState, _ := transMap["fromState"].(string)
				toState, _ := transMap["toState"].(string)
				condition, _ := transMap["condition"].(string)

				// Safely handle the autoTransform boolean with nil check
				autoTransform := false // Default value
				if autoTransformVal, exists := transMap["autoTransform"]; exists {
					if at, ok := autoTransformVal.(bool); ok {
						autoTransform = at
					}
				}

				if fromState == "" || toState == "" {
					return fmt.Errorf("transition '%s' missing required fields", transID)
				}

				// Create transition config
				transConfig := module.StateMachineTransitionConfig{
					ID:            transID,
					FromState:     fromState,
					ToState:       toState,
					Condition:     condition,
					AutoTransform: autoTransform,
				}

				// Add custom data if provided
				if transData, ok := transMap["data"].(map[string]interface{}); ok {
					transConfig.Data = transData
				}

				transitions[transID] = transConfig
			}
		}

		// Create workflow definition
		workflowDef := module.ExternalStateMachineDefinition{
			ID:           name,
			Description:  description,
			InitialState: initialState,
			States:       states,
			Transitions:  transitions,
		}

		// Add the workflow definition to the engine
		if err := smEngine.RegisterWorkflow(workflowDef); err != nil {
			return fmt.Errorf("failed to register workflow '%s': %w", name, err)
		}
	}

	// Process transition hooks if any
	var hooksConfig []interface{}
	if hooks, ok := smConfig["hooks"].([]interface{}); ok && len(hooks) > 0 {
		hooksConfig = hooks
		// Create a single composite handler for all hooks
		compositeHandler := h.createCompositeTransitionHandler(app, hooks)

		// Register the handler with the engine
		smEngine.AddGlobalTransitionHandler(compositeHandler)
	}

	// Wire persistence (optional) — look up "persistence" from the service registry
	var persistenceSvc interface{}
	if err := app.GetService("persistence", &persistenceSvc); err == nil && persistenceSvc != nil {
		if ps, ok := persistenceSvc.(*module.PersistenceStore); ok {
			smEngine.SetPersistence(ps)

			// Restore workflow instances from a previous run
			if loadErr := smEngine.LoadAllPersistedInstances(); loadErr != nil {
				fmt.Printf("Warning: failed to load persisted instances: %v\n", loadErr)
			}

			// Recover instances stuck in processing states.
			// Extract processing states from hook config (states that have
			// processing.step handlers which represent intermediate states).
			processingStates := h.extractProcessingStates(hooksConfig, definitions)
			if len(processingStates) > 0 {
				go func() {
					// Small startup delay to let all modules finish initializing
					time.Sleep(2 * time.Second)
					recovered := smEngine.RecoverProcessingInstances(context.Background(), processingStates)
					if recovered > 0 {
						fmt.Printf("Recovered %d stuck workflow instances\n", recovered)
					}
				}()
			}
		}
	}

	return nil
}

// createCompositeTransitionHandler creates a handler that manages multiple hooks
func (h *StateMachineWorkflowHandler) createCompositeTransitionHandler(
	app modular.Application,
	hooksConfig []interface{},
) module.TransitionHandler {
	return module.NewFunctionTransitionHandler(func(ctx context.Context, event module.TransitionEvent) error {
		// Get the workflow instance to determine its type
		var workflowType string

		// Extract the state machine engine from context if available
		if engine, ok := ctx.Value("stateMachineEngine").(*module.StateMachineEngine); ok {
			// First try to get the instance directly from the engine in context
			instance, err := engine.GetInstance(event.WorkflowID)
			if err == nil && instance != nil {
				workflowType = instance.WorkflowType
			}
		}

		// If we couldn't get the workflow type from context, try to get the instance details
		// by scanning available state machine engines
		if workflowType == "" {
			for _, svc := range app.SvcRegistry() {
				if engine, ok := svc.(*module.StateMachineEngine); ok {
					instance, err := engine.GetInstance(event.WorkflowID)
					if err == nil && instance != nil {
						workflowType = instance.WorkflowType
						break
					}
				}
			}
		}

		// If we still couldn't get the workflow type, fall back to using the event WorkflowID
		if workflowType == "" {
			workflowType = event.WorkflowID
		}

		for i, hookConfig := range hooksConfig {
			hookMap, ok := hookConfig.(map[string]interface{})
			if !ok {
				fmt.Printf("Invalid hook configuration at index %d\n", i)
				continue
			}

			// Check for workflow type match
			hookWorkflowType, _ := hookMap["workflowType"].(string)
			if hookWorkflowType != "" && hookWorkflowType != workflowType {
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
				payload := []byte(fmt.Sprintf(`{"workflowId":"%s","instanceId":"%s","transition":"%s","fromState":"%s","toState":"%s"}`,
					workflowType, event.WorkflowID, event.TransitionID, event.FromState, event.ToState))
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

// extractProcessingStates identifies intermediate processing states by looking
// at hook handlers that reference processing steps. A state is considered a
// processing state if a hook targets transitions into it via a processing.step
// handler. Falls back to scanning transition definitions for auto-transform
// targets that aren't final states.
func (h *StateMachineWorkflowHandler) extractProcessingStates(
	hooksConfig []interface{},
	definitions []interface{},
) []string {
	stateSet := make(map[string]bool)

	// Scan hooks for processing step handlers — the toStates of such hooks
	// are intermediate processing states.
	for _, hookObj := range hooksConfig {
		hookMap, ok := hookObj.(map[string]interface{})
		if !ok {
			continue
		}
		handler, _ := hookMap["handler"].(string)
		if handler == "" {
			continue
		}

		// Check if the handler name suggests a processing step
		if !strings.Contains(handler, "processing") && !strings.Contains(handler, "step") {
			continue
		}

		// The toStates of this hook are processing states
		if toStates, ok := hookMap["toStates"].([]interface{}); ok {
			for _, s := range toStates {
				if sStr, ok := s.(string); ok {
					stateSet[sStr] = true
				}
			}
		}
	}

	// If no processing states found from hooks, try to infer from definitions.
	// Auto-transform transitions target intermediate states that may need recovery.
	if len(stateSet) == 0 {
		for _, defObj := range definitions {
			def, ok := defObj.(map[string]interface{})
			if !ok {
				continue
			}
			states, _ := def["states"].(map[string]interface{})
			transitions, _ := def["transitions"].(map[string]interface{})
			if transitions == nil {
				continue
			}
			for _, transObj := range transitions {
				transMap, ok := transObj.(map[string]interface{})
				if !ok {
					continue
				}
				autoTransform, _ := transMap["autoTransform"].(bool)
				if !autoTransform {
					continue
				}
				toState, _ := transMap["toState"].(string)
				if toState == "" {
					continue
				}
				// Check that the target is not a final state
				if states != nil {
					if stateObj, ok := states[toState].(map[string]interface{}); ok {
						if isFinal, _ := stateObj["isFinal"].(bool); isFinal {
							continue
						}
					}
				}
				stateSet[toState] = true
			}
		}
	}

	result := make([]string, 0, len(stateSet))
	for s := range stateSet {
		result = append(result, s)
	}
	return result
}

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *StateMachineWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	// For state machine workflows, the action represents a transition to trigger
	// and data should contain the workflow instance ID

	// Extract the workflow ID from the data
	instanceID, ok := data["instanceId"].(string)
	if !ok {
		// Try other common key names
		instanceID, ok = data["id"].(string)
		if !ok {
			return nil, fmt.Errorf("workflow instance ID not provided in data")
		}
	}

	// Extract the workflow definition name if provided
	workflowName, _ := data["workflowName"].(string)

	// Parse the engine and transition name from the action
	// Format: engine:transition or just transition
	engineName := ""
	transitionName := action

	if parts := strings.Split(action, ":"); len(parts) > 1 {
		engineName = parts[0]
		transitionName = parts[1]
	}

	// Get the state machine engine from the app context
	var app modular.Application
	if appVal := ctx.Value(applicationContextKey); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	// If no specific engine was provided, try to find one
	var engineSvc interface{}
	if engineName != "" {
		// Apply namespace if needed
		if h.namespace != nil {
			engineName = h.namespace.ResolveDependency(engineName)
		}

		// Get the named engine
		if err := app.GetService(engineName, &engineSvc); err != nil {
			return nil, fmt.Errorf("state machine engine '%s' not found: %v", engineName, err)
		}
	} else {
		// Try to find a state machine engine by scanning services
		for name, svc := range app.SvcRegistry() {
			if engine, ok := svc.(*module.StateMachineEngine); ok {
				engineSvc = engine
				engineName = name
				break
			}
		}

		if engineSvc == nil {
			return nil, fmt.Errorf("no state machine engine found")
		}
	}

	engine, ok := engineSvc.(*module.StateMachineEngine)
	if !ok {
		return nil, fmt.Errorf("service '%s' is not a StateMachineEngine", engineName)
	}

	// If workflow name is provided, check if we need to create an instance
	if workflowName != "" && instanceID == "" {
		// Create a new instance with the provided data
		instance, err := engine.CreateWorkflow(workflowType, workflowName, data)
		if err != nil {
			return nil, fmt.Errorf("failed to create workflow instance: %w", err)
		}

		instanceID = instance.ID

		// Return the instance without triggering a transition
		if transitionName == "" {
			result := map[string]interface{}{
				"instanceId":   instance.ID,
				"currentState": instance.CurrentState,
				"created":      true,
			}
			return result, nil
		}
	}

	// Execute the state machine transition
	err := engine.TriggerTransition(ctx, instanceID, transitionName, data)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger transition '%s' for instance '%s': %w",
			transitionName, instanceID, err)
	}

	// Get the updated instance state
	instance, err := engine.GetInstance(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow instance '%s' after transition: %w", instanceID, err)
	}

	// Return the result
	result := map[string]interface{}{
		"instanceId":   instanceID,
		"currentState": instance.CurrentState,
		"transition":   transitionName,
		"success":      true,
	}

	return result, nil
}
