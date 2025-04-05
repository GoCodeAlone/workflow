package workflow

import (
	"fmt"
	//"github.com/GoCodeAlone/modular/config"
	//"github.com/GoCodeAlone/modular/factory"
	//"github.com/GoCodeAlone/modular/module"
	"github.com/GoCodeAlone/modular"
)

// WorkflowHandler interface for handling different workflow types
type WorkflowHandler interface {
	// CanHandle returns true if this handler can process the given workflow type
	CanHandle(workflowType string) bool

	// ConfigureWorkflow sets up the workflow from configuration
	ConfigureWorkflow(registry *module.Registry, workflowConfig interface{}) error
}

// Engine represents the workflow execution engine
type Engine struct {
	factory          *factory.Factory
	registry         *module.Registry
	workflowHandlers []WorkflowHandler
}

// NewEngine creates a new workflow engine
func NewEngine(factory *factory.Factory, registry *module.Registry) *Engine {
	return &Engine{
		factory:          factory,
		registry:         registry,
		workflowHandlers: make([]WorkflowHandler, 0),
	}
}

// RegisterWorkflowHandler adds a workflow handler to the engine
func (e *Engine) RegisterWorkflowHandler(handler WorkflowHandler) {
	e.workflowHandlers = append(e.workflowHandlers, handler)
}

// BuildFromConfig builds a workflow from configuration
func (e *Engine) BuildFromConfig(cfg *config.WorkflowConfig) error {
	// First, create all modules
	for _, modCfg := range cfg.Modules {
		_, err := e.factory.BuildModule(modCfg)
		if err != nil {
			return err
		}
	}

	// Handle each workflow configuration section
	for workflowType, workflowConfig := range cfg.Workflows {
		handled := false

		// Find a handler for this workflow type
		for _, handler := range e.workflowHandlers {
			if handler.CanHandle(workflowType) {
				if err := handler.ConfigureWorkflow(e.registry, workflowConfig); err != nil {
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

// Start starts all modules that implement the Startable interface
func (e *Engine) Start() error {
	var startErrors []error

	e.registry.Each(func(name string, mod module.Module) {
		if starter, ok := mod.(module.Startable); ok {
			if err := starter.Start(); err != nil {
				startErrors = append(startErrors, fmt.Errorf("failed to start module %s: %w", name, err))
			}
		}
	})

	if len(startErrors) > 0 {
		return fmt.Errorf("errors starting modules: %v", startErrors)
	}

	return nil
}

// Stop stops all modules that implement the Stoppable interface
func (e *Engine) Stop() error {
	var stopErrors []error

	e.registry.Each(func(name string, mod module.Module) {
		if stopper, ok := mod.(module.Stoppable); ok {
			if err := stopper.Stop(); err != nil {
				stopErrors = append(stopErrors, fmt.Errorf("failed to stop module %s: %w", name, err))
			}
		}
	})

	if len(stopErrors) > 0 {
		return fmt.Errorf("errors stopping modules: %v", stopErrors)
	}

	return nil
}
