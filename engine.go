package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/auth"
	"github.com/CrisisTextLine/modular/modules/cache"
	"github.com/CrisisTextLine/modular/modules/chimux"
	"github.com/CrisisTextLine/modular/modules/database/v2"
	"github.com/CrisisTextLine/modular/modules/eventbus"
	"github.com/CrisisTextLine/modular/modules/eventlogger"
	"github.com/CrisisTextLine/modular/modules/httpclient"
	"github.com/CrisisTextLine/modular/modules/httpserver"
	"github.com/CrisisTextLine/modular/modules/jsonschema"
	"github.com/CrisisTextLine/modular/modules/reverseproxy/v2"
	"github.com/CrisisTextLine/modular/modules/scheduler"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
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
	dynamicRegistry  *dynamic.ComponentRegistry
	eventEmitter     *module.WorkflowEventEmitter
}

// SetDynamicRegistry sets the dynamic component registry on the engine.
func (e *StdEngine) SetDynamicRegistry(registry *dynamic.ComponentRegistry) {
	e.dynamicRegistry = registry
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
				handler := module.NewRESTAPIHandler(modCfg.Name, resourceName)
				if wt, ok := modCfg.Config["workflowType"].(string); ok && wt != "" {
					handler.SetWorkflowType(wt)
				}
				if we, ok := modCfg.Config["workflowEngine"].(string); ok && we != "" {
					handler.SetWorkflowEngine(we)
				}
				mod = handler
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
			case "http.middleware.ratelimit":
				requestsPerMinute := 60 // default
				burstSize := 10         // default
				if rpm, ok := modCfg.Config["requestsPerMinute"].(int); ok {
					requestsPerMinute = rpm
				} else if rpm, ok := modCfg.Config["requestsPerMinute"].(float64); ok {
					requestsPerMinute = int(rpm)
				}
				if bs, ok := modCfg.Config["burstSize"].(int); ok {
					burstSize = bs
				} else if bs, ok := modCfg.Config["burstSize"].(float64); ok {
					burstSize = int(bs)
				}
				e.logger.Debug("Loading HTTP middleware rate limit module")
				mod = module.NewRateLimitMiddleware(modCfg.Name, requestsPerMinute, burstSize)
			case "http.middleware.cors":
				allowedOrigins := []string{"*"}                                       // default
				allowedMethods := []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"} // default
				if origins, ok := modCfg.Config["allowedOrigins"].([]interface{}); ok {
					allowedOrigins = make([]string, len(origins))
					for i, origin := range origins {
						if str, ok := origin.(string); ok {
							allowedOrigins[i] = str
						}
					}
				}
				if methods, ok := modCfg.Config["allowedMethods"].([]interface{}); ok {
					allowedMethods = make([]string, len(methods))
					for i, method := range methods {
						if str, ok := method.(string); ok {
							allowedMethods[i] = str
						}
					}
				}
				e.logger.Debug("Loading HTTP middleware CORS module")
				mod = module.NewCORSMiddleware(modCfg.Name, allowedOrigins, allowedMethods)
			case "messaging.broker":
				e.logger.Debug("Loading messaging broker module")
				mod = module.NewInMemoryMessageBroker(modCfg.Name)
			case "messaging.broker.eventbus":
				e.logger.Debug("Loading EventBus bridge module")
				mod = module.NewEventBusBridge(modCfg.Name)
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
			case "http.proxy":
				e.logger.Debug("Loading reverse proxy module")
				mod = reverseproxy.NewModule()
			case "reverseproxy":
				e.logger.Debug("Loading reverse proxy module")
				mod = reverseproxy.NewModule()
			case "httpserver.modular":
				e.logger.Debug("Loading Modular HTTP server module")
				mod = httpserver.NewHTTPServerModule()
			case "scheduler.modular":
				e.logger.Debug("Loading Modular scheduler module")
				mod = scheduler.NewModule()
			case "auth.modular":
				e.logger.Debug("Loading Modular auth module")
				mod = auth.NewModule()
			case "eventbus.modular":
				e.logger.Debug("Loading Modular eventbus module")
				mod = eventbus.NewModule()
			case "cache.modular":
				e.logger.Debug("Loading Modular cache module")
				mod = cache.NewModule()
			case "chimux.router":
				e.logger.Debug("Loading Chi router module")
				mod = chimux.NewChiMuxModule()
			case "eventlogger.modular":
				e.logger.Debug("Loading Modular event logger module")
				mod = eventlogger.NewModule()
			case "httpclient.modular":
				e.logger.Debug("Loading Modular HTTP client module")
				mod = httpclient.NewHTTPClientModule()
			case "database.modular":
				e.logger.Debug("Loading Modular database module")
				mod = database.NewModule()
			case "jsonschema.modular":
				e.logger.Debug("Loading Modular JSON schema module")
				mod = jsonschema.NewModule()
			case "metrics.collector":
				e.logger.Debug("Loading metrics collector module")
				mod = module.NewMetricsCollector(modCfg.Name)
			case "health.checker":
				e.logger.Debug("Loading health checker module")
				mod = module.NewHealthChecker(modCfg.Name)
			case "http.middleware.requestid":
				e.logger.Debug("Loading request ID middleware module")
				mod = module.NewRequestIDMiddleware(modCfg.Name)
			case "dynamic.component":
				e.logger.Debug("Loading dynamic component module: " + modCfg.Name)
				if e.dynamicRegistry == nil {
					return fmt.Errorf("dynamic registry not set, cannot load dynamic component %q", modCfg.Name)
				}
				componentID := modCfg.Name
				if id, ok := modCfg.Config["componentId"].(string); ok {
					componentID = id
				}
				comp, ok := e.dynamicRegistry.Get(componentID)
				if !ok {
					return fmt.Errorf("dynamic component %q not found in registry", componentID)
				}
				adapter := dynamic.NewModuleAdapter(comp)
				if provides, ok := modCfg.Config["provides"].([]interface{}); ok {
					svcs := make([]string, 0, len(provides))
					for _, p := range provides {
						if s, ok := p.(string); ok {
							svcs = append(svcs, s)
						}
					}
					adapter.SetProvides(svcs)
				}
				if requires, ok := modCfg.Config["requires"].([]interface{}); ok {
					svcs := make([]string, 0, len(requires))
					for _, r := range requires {
						if s, ok := r.(string); ok {
							svcs = append(svcs, s)
						}
					}
					adapter.SetRequires(svcs)
				}
				mod = adapter
			case "database.workflow":
				e.logger.Debug("Loading workflow database module")
				dbConfig := module.DatabaseConfig{}
				if driver, ok := modCfg.Config["driver"].(string); ok {
					dbConfig.Driver = driver
				}
				if dsn, ok := modCfg.Config["dsn"].(string); ok {
					dbConfig.DSN = dsn
				}
				if maxOpen, ok := modCfg.Config["maxOpenConns"].(float64); ok {
					dbConfig.MaxOpenConns = int(maxOpen)
				}
				if maxIdle, ok := modCfg.Config["maxIdleConns"].(float64); ok {
					dbConfig.MaxIdleConns = int(maxIdle)
				}
				mod = module.NewWorkflowDatabase(modCfg.Name, dbConfig)
			case "data.transformer":
				e.logger.Debug("Loading data transformer module")
				mod = module.NewDataTransformer(modCfg.Name)
			case "webhook.sender":
				e.logger.Debug("Loading webhook sender module")
				webhookConfig := module.WebhookConfig{}
				if mr, ok := modCfg.Config["maxRetries"].(float64); ok {
					webhookConfig.MaxRetries = int(mr)
				}
				mod = module.NewWebhookSender(modCfg.Name, webhookConfig)
			case "notification.slack":
				e.logger.Debug("Loading Slack notification module")
				mod = module.NewSlackNotification(modCfg.Name)
			case "storage.s3":
				e.logger.Debug("Loading S3 storage module")
				mod = module.NewS3Storage(modCfg.Name)
			case "messaging.nats":
				e.logger.Debug("Loading NATS broker module")
				mod = module.NewNATSBroker(modCfg.Name)
			case "messaging.kafka":
				e.logger.Debug("Loading Kafka broker module")
				mod = module.NewKafkaBroker(modCfg.Name)
			case "observability.otel":
				e.logger.Debug("Loading OpenTelemetry tracing module")
				mod = module.NewOTelTracing(modCfg.Name)
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
	for name := range e.app.SvcRegistry() {
		e.logger.Debug("Loaded service: " + name)
	}

	// Initialize the workflow event emitter
	e.eventEmitter = module.NewWorkflowEventEmitter(e.app)

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
	startTime := time.Now()

	// Find the appropriate workflow handler
	for _, handler := range e.workflowHandlers {
		if handler.CanHandle(workflowType) {
			// Log workflow execution
			e.logger.Info(fmt.Sprintf("Triggered workflow '%s' with action '%s'", workflowType, action))

			// Log the data in debug mode
			for k, v := range data {
				e.logger.Debug(fmt.Sprintf("  %s: %v", k, v))
			}

			if e.eventEmitter != nil {
				e.eventEmitter.EmitWorkflowStarted(ctx, workflowType, action, data)
			}

			// Execute the workflow using the handler
			results, err := handler.ExecuteWorkflow(ctx, workflowType, action, data)
			if err != nil {
				e.logger.Error(fmt.Sprintf("Failed to execute workflow '%s': %v", workflowType, err))
				if e.eventEmitter != nil {
					e.eventEmitter.EmitWorkflowFailed(ctx, workflowType, action, time.Since(startTime), err)
				}
				e.recordWorkflowMetrics(workflowType, action, "error", time.Since(startTime))
				return fmt.Errorf("workflow execution failed: %w", err)
			}

			// Log execution results in debug mode
			e.logger.Info(fmt.Sprintf("Workflow '%s' executed successfully", workflowType))
			for k, v := range results {
				e.logger.Debug(fmt.Sprintf("  Result %s: %v", k, v))
			}

			if e.eventEmitter != nil {
				e.eventEmitter.EmitWorkflowCompleted(ctx, workflowType, action, time.Since(startTime), results)
			}
			e.recordWorkflowMetrics(workflowType, action, "success", time.Since(startTime))
			return nil
		}
	}

	return fmt.Errorf("no handler found for workflow type: %s", workflowType)
}

// recordWorkflowMetrics records workflow execution metrics if the metrics collector is available.
func (e *StdEngine) recordWorkflowMetrics(workflowType, action, status string, duration time.Duration) {
	var mc *module.MetricsCollector
	if err := e.app.GetService("metrics.collector", &mc); err == nil && mc != nil {
		mc.RecordWorkflowExecution(workflowType, action, status)
		mc.RecordWorkflowDuration(workflowType, action, duration)
	}
}

// configureTriggers sets up all triggers from configuration
func (e *StdEngine) configureTriggers(triggerConfigs map[string]interface{}) error {
	if len(triggerConfigs) == 0 {
		// No triggers configured, which is fine
		return nil
	}

	// Register this engine as a service so triggers can find it
	if err := e.app.RegisterService("workflowEngine", e); err != nil {
		return fmt.Errorf("failed to register workflow engine service: %w", err)
	}

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
	case "eventbus":
		return trigger.Name() == module.EventBusTriggerName
	case "mock":
		// For tests - match the name of the trigger
		return trigger.Name() == "mock.trigger"
	default:
		return false
	}
}

// GetApp returns the underlying modular Application.
func (e *StdEngine) GetApp() modular.Application {
	return e.app
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
