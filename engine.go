package workflow

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
)

// WorkflowHandler interface for handling different workflow types
type WorkflowHandler interface {
	// CanHandle returns true if this handler can process the given workflow type
	CanHandle(workflowType string) bool

	// ConfigureWorkflow sets up the workflow from configuration
	ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error

	// ExecuteWorkflow executes a workflow with the given action and input data
	ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error)
}

// ModuleFactory is a function that creates a module from a name and configuration
type ModuleFactory func(name string, config map[string]interface{}) modular.Module

// StartStopModule extends the basic Module interface with lifecycle methods
type StartStopModule interface {
	modular.Module
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// StdEngine represents the workflow execution engine
type StdEngine struct {
	app              modular.Application
	workflowHandlers []WorkflowHandler
	moduleFactories  map[string]ModuleFactory
	logger           modular.Logger
	modules          []modular.Module
	triggers         []module.Trigger
	triggerRegistry  *module.TriggerRegistry
}

// NewStdEngine creates a new workflow engine
func NewStdEngine(app modular.Application, logger modular.Logger) *StdEngine {
	return &StdEngine{
		app:              app,
		workflowHandlers: make([]WorkflowHandler, 0),
		moduleFactories:  make(map[string]ModuleFactory),
		logger:           logger,
		modules:          make([]modular.Module, 0),
		triggers:         make([]module.Trigger, 0),
		triggerRegistry:  module.NewTriggerRegistry(),
	}
}

// RegisterWorkflowHandler adds a workflow handler to the engine
func (e *StdEngine) RegisterWorkflowHandler(handler WorkflowHandler) {
	e.workflowHandlers = append(e.workflowHandlers, handler)
}

// RegisterTrigger registers a trigger with the engine
func (e *StdEngine) RegisterTrigger(trigger module.Trigger) {
	e.triggers = append(e.triggers, trigger)
	e.triggerRegistry.RegisterTrigger(trigger)
}

// AddModuleType registers a factory function for a module type
func (e *StdEngine) AddModuleType(moduleType string, factory ModuleFactory) {
	e.moduleFactories[moduleType] = factory
}

// BuildFromConfig builds a workflow from configuration
func (e *StdEngine) BuildFromConfig(cfg *config.WorkflowConfig) error {
	// Register all modules from config
	for _, modCfg := range cfg.Modules {
		// Create modules based on type
		var mod modular.Module

		// First check in the custom module factories
		if factory, exists := e.moduleFactories[modCfg.Type]; exists {
			e.logger.Debug("Existing factory using module type: " + modCfg.Type)
			mod = factory(modCfg.Name, modCfg.Config)
		} else {
			// Use built-in module types
			switch modCfg.Type {
			case "http.server":
				address := ""
				if addr, ok := modCfg.Config["address"].(string); ok {
					address = addr
				}
				e.logger.Debug("Loading standard HTTP server module with address: " + address)
				mod = module.NewStandardHTTPServer(modCfg.Name, address)
			case "http.router":
				e.logger.Debug("Loading standard HTTP router module")
				mod = module.NewStandardHTTPRouter(modCfg.Name)
			case "http.handler":
				contentType := "application/json"
				if ct, ok := modCfg.Config["contentType"].(string); ok {
					contentType = ct
				}
				e.logger.Debug("Loading standard HTTP handler module with content type: " + contentType)
				mod = module.NewSimpleHTTPHandler(modCfg.Name, contentType)
			case "api.handler":
				resourceName := "resources"
				if rn, ok := modCfg.Config["resourceName"].(string); ok {
					resourceName = rn
				}
				e.logger.Debug("Loading REST API handler module with resource name: " + resourceName)
				mod = module.NewRESTAPIHandler(modCfg.Name, resourceName)
			case "http.middleware.auth":
				authType := "Bearer" // Default auth type
				if at, ok := modCfg.Config["authType"].(string); ok {
					authType = at
				}
				e.logger.Debug("Loading HTTP middleware auth module with auth type: " + authType)
				mod = module.NewAuthMiddleware(modCfg.Name, authType)
			case "http.middleware.logging":
				logLevel := "info" // Default log level
				if ll, ok := modCfg.Config["logLevel"].(string); ok {
					logLevel = ll
				}
				e.logger.Debug("Loading HTTP middleware logging module with log level: " + logLevel)
				mod = module.NewLoggingMiddleware(modCfg.Name, logLevel)
			case "messaging.broker":
				e.logger.Debug("Loading messaging broker module")
				mod = module.NewInMemoryMessageBroker(modCfg.Name)
			case "messaging.handler":
				e.logger.Debug("Loading messaging handler module")
				mod = module.NewSimpleMessageHandler(modCfg.Name)
			case "statemachine.engine":
				e.logger.Debug("Loading state machine engine module")
				mod = module.NewStateMachineEngine(modCfg.Name)
			case "state.tracker":
				e.logger.Debug("Loading state tracker module")
				mod = module.NewStateTracker(modCfg.Name)
			case "state.connector":
				e.logger.Debug("Loading state machine connector module")
				mod = module.NewStateMachineStateConnector(modCfg.Name)
			default:
				e.logger.Warn("Unknown module type: " + modCfg.Type)
				return fmt.Errorf("unknown module type: %s", modCfg.Type)
			}
		}

		e.app.RegisterModule(mod)
	}

	// Initialize all modules
	if err := e.app.Init(); err != nil {
		return fmt.Errorf("failed to initialize modules: %w", err)
	}

	// Log loaded services
	for name, _ := range e.app.SvcRegistry() {
		e.logger.Debug("Loaded service: " + name)
	}

	// Register config section for workflow
	e.app.RegisterConfigSection("workflow", modular.NewStdConfigProvider(cfg))

	// Handle each workflow configuration section
	for workflowType, workflowConfig := range cfg.Workflows {
		handled := false

		// Find a handler for this workflow type
		for _, handler := range e.workflowHandlers {
			if handler.CanHandle(workflowType) {
				if err := handler.ConfigureWorkflow(e.app, workflowConfig); err != nil {
					return fmt.Errorf("failed to configure %s workflow: %w", workflowType, err)
				}
				handled = true
				break
			}
		}

		if !handled {
			return fmt.Errorf("no handler found for workflow type: %s", workflowType)
		}
	}

	// Configure triggers (new section)
	if err := e.configureTriggers(cfg.Triggers); err != nil {
		return fmt.Errorf("failed to configure triggers: %w", err)
	}

	return nil
}

// Start starts all modules and triggers
func (e *StdEngine) Start(ctx context.Context) error {
	err := e.app.Start()
	if err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	// Start all triggers
	for _, trigger := range e.triggers {
		if err := trigger.Start(ctx); err != nil {
			return fmt.Errorf("failed to start trigger '%s': %w", trigger.Name(), err)
		}
	}

	return nil
}

// Stop stops all modules and triggers
func (e *StdEngine) Stop(ctx context.Context) error {
	var lastErr error

	// Stop all triggers first
	for _, trigger := range e.triggers {
		if err := trigger.Stop(ctx); err != nil {
			lastErr = fmt.Errorf("failed to stop trigger '%s': %w", trigger.Name(), err)
			e.logger.Error(lastErr.Error())
		}
	}

	err := e.app.Stop()
	if err != nil {
		lastErr = fmt.Errorf("failed to stop application: %w", err)
		e.logger.Error(lastErr.Error())
	}

	return lastErr
}

// TriggerWorkflow starts a workflow based on a trigger
func (e *StdEngine) TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error {
	// Find the appropriate workflow handler
	for _, handler := range e.workflowHandlers {
		if handler.CanHandle(workflowType) {
			// Log workflow execution
			e.logger.Info(fmt.Sprintf("Triggered workflow '%s' with action '%s'", workflowType, action))

			// Log the data in debug mode
			for k, v := range data {
				e.logger.Debug(fmt.Sprintf("  %s: %v", k, v))
			}

			// Execute the workflow using the handler
			results, err := handler.ExecuteWorkflow(ctx, workflowType, action, data)
			if err != nil {
				e.logger.Error(fmt.Sprintf("Failed to execute workflow '%s': %v", workflowType, err))
				return fmt.Errorf("workflow execution failed: %w", err)
			}

			// Log execution results in debug mode
			e.logger.Info(fmt.Sprintf("Workflow '%s' executed successfully", workflowType))
			for k, v := range results {
				e.logger.Debug(fmt.Sprintf("  Result %s: %v", k, v))
			}

			return nil
		}
	}

	return fmt.Errorf("no handler found for workflow type: %s", workflowType)
}

// buildModules creates and initializes all modules from configuration
func (e *StdEngine) buildModules(modulesConfig []map[string]interface{}) error {
	// ... existing code ...
	return nil
}

// configureWorkflows sets up all workflows from configuration
func (e *StdEngine) configureWorkflows(workflowConfigs map[string]interface{}) error {
	// ... existing code ...
	return nil
}

// configureTriggers sets up all triggers from configuration
func (e *StdEngine) configureTriggers(triggerConfigs map[string]interface{}) error {
	if len(triggerConfigs) == 0 {
		// No triggers configured, which is fine
		return nil
	}

	// Register this engine as a service so triggers can find it
	e.app.RegisterService("workflowEngine", e)

	// Configure each trigger type
	for triggerType, triggerConfig := range triggerConfigs {
		// Find a handler for this trigger type
		var handlerFound bool
		for _, trigger := range e.triggers {
			// If this trigger can handle the type, configure it
			if canHandleTrigger(trigger, triggerType) {
				if err := trigger.Configure(e.app, triggerConfig); err != nil {
					return fmt.Errorf("failed to configure trigger '%s': %w", triggerType, err)
				}
				handlerFound = true
				break
			}
		}

		if !handlerFound {
			return fmt.Errorf("no handler found for trigger type '%s'", triggerType)
		}
	}

	return nil
}

// canHandleTrigger determines if a trigger can handle a specific trigger type
// This is a simple implementation that could be expanded
func canHandleTrigger(trigger module.Trigger, triggerType string) bool {
	switch triggerType {
	case "http":
		return trigger.Name() == module.HTTPTriggerName
	case "schedule":
		return trigger.Name() == module.ScheduleTriggerName
	case "event":
		return trigger.Name() == module.EventTriggerName
	case "mock":
		// For tests - match the name of the trigger
		return trigger.Name() == "mock.trigger"
	default:
		return false
	}
}

type Engine interface {
	RegisterWorkflowHandler(handler WorkflowHandler)
	RegisterTrigger(trigger module.Trigger)
	AddModuleType(moduleType string, factory ModuleFactory)
	BuildFromConfig(cfg *config.WorkflowConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error
}
