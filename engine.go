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
}

// ModuleFactory is a function that creates a module from a name and configuration
type ModuleFactory func(name string, config map[string]interface{}) modular.Module

// Engine represents the workflow execution engine
type Engine struct {
	app              modular.Application
	workflowHandlers []WorkflowHandler
	moduleFactories  map[string]ModuleFactory
}

// NewEngine creates a new workflow engine
func NewEngine(app modular.Application) *Engine {
	return &Engine{
		app:              app,
		workflowHandlers: make([]WorkflowHandler, 0),
		moduleFactories:  make(map[string]ModuleFactory),
	}
}

// RegisterWorkflowHandler adds a workflow handler to the engine
func (e *Engine) RegisterWorkflowHandler(handler WorkflowHandler) {
	e.workflowHandlers = append(e.workflowHandlers, handler)
}

// AddModuleType registers a factory function for a module type
func (e *Engine) AddModuleType(moduleType string, factory ModuleFactory) {
	e.moduleFactories[moduleType] = factory
}

// BuildFromConfig builds a workflow from configuration
func (e *Engine) BuildFromConfig(cfg *config.WorkflowConfig) error {
	// Register all modules from config
	for _, modCfg := range cfg.Modules {
		// Create modules based on type
		var mod modular.Module

		// First check in the custom module factories
		if factory, exists := e.moduleFactories[modCfg.Type]; exists {
			mod = factory(modCfg.Name, modCfg.Config)
		} else {
			// Use built-in module types
			switch modCfg.Type {
			case "http.server":
				address := ""
				if addr, ok := modCfg.Config["address"].(string); ok {
					address = addr
				}
				mod = module.NewStandardHTTPServer(modCfg.Name, address)
			case "http.router":
				mod = module.NewStandardHTTPRouter(modCfg.Name)
			case "http.handler":
				contentType := "application/json"
				if ct, ok := modCfg.Config["contentType"].(string); ok {
					contentType = ct
				}
				mod = module.NewSimpleHTTPHandler(modCfg.Name, contentType)
			case "api.handler":
				resourceName := "resources"
				if rn, ok := modCfg.Config["resourceName"].(string); ok {
					resourceName = rn
				}
				mod = module.NewRESTAPIHandler(modCfg.Name, resourceName)
			case "http.middleware.auth":
				authType := "Bearer" // Default auth type
				if at, ok := modCfg.Config["authType"].(string); ok {
					authType = at
				}
				mod = module.NewAuthMiddleware(modCfg.Name, authType)
			case "messaging.broker":
				mod = module.NewInMemoryMessageBroker(modCfg.Name)
			case "messaging.handler":
				mod = module.NewSimpleMessageHandler(modCfg.Name)
			case "statemachine.engine":
				mod = module.NewStandardStateMachineEngine(nil)
			default:
				return fmt.Errorf("unknown module type: %s", modCfg.Type)
			}
		}

		e.app.RegisterModule(mod)
	}

	// Initialize all modules
	if err := e.app.Init(); err != nil {
		return fmt.Errorf("failed to initialize modules: %w", err)
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

	return nil
}

// Start starts all modules
func (e *Engine) Start(ctx context.Context) error {
	return e.app.Start()
}

// Stop stops all modules
func (e *Engine) Stop(ctx context.Context) error {
	return e.app.Stop()
}
