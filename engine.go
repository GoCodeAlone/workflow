package workflow

import (
	"context"
	"fmt"
	"os"
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
	"github.com/GoCodeAlone/workflow/schema"
)

// WorkflowHandler interface for handling different workflow types
type WorkflowHandler interface {
	// CanHandle returns true if this handler can process the given workflow type
	CanHandle(workflowType string) bool

	// ConfigureWorkflow sets up the workflow from configuration
	ConfigureWorkflow(app modular.Application, workflowConfig any) error

	// ExecuteWorkflow executes a workflow with the given action and input data
	ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error)
}

// ModuleFactory is a function that creates a module from a name and configuration
type ModuleFactory func(name string, config map[string]any) modular.Module

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
	dynamicLoader    *dynamic.Loader
	eventEmitter     *module.WorkflowEventEmitter
}

// SetDynamicRegistry sets the dynamic component registry on the engine.
func (e *StdEngine) SetDynamicRegistry(registry *dynamic.ComponentRegistry) {
	e.dynamicRegistry = registry
}

// SetDynamicLoader sets the dynamic component loader on the engine.
// When set, dynamic.component modules can load from source files via the "source" config key.
func (e *StdEngine) SetDynamicLoader(loader *dynamic.Loader) {
	e.dynamicLoader = loader
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
	// Validate configuration before building.
	// Allow empty modules (the engine handles that gracefully) and pass
	// registered custom module factory types so they are not rejected.
	// Workflow and trigger type validation is skipped here because the
	// engine dynamically resolves handlers and triggers at runtime.
	valOpts := []schema.ValidationOption{
		schema.WithAllowEmptyModules(),
		schema.WithSkipWorkflowTypeCheck(),
		schema.WithSkipTriggerTypeCheck(),
	}
	if len(e.moduleFactories) > 0 {
		extra := make([]string, 0, len(e.moduleFactories))
		for t := range e.moduleFactories {
			extra = append(extra, t)
		}
		valOpts = append(valOpts, schema.WithExtraModuleTypes(extra...))
	}
	if err := schema.ValidateConfig(cfg, valOpts...); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

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
				if it, ok := modCfg.Config["initialTransition"].(string); ok && it != "" {
					handler.SetInitialTransition(it)
				}
				if sf, ok := modCfg.Config["seedFile"].(string); ok && sf != "" {
					handler.SetSeedFile(cfg.ResolveRelativePath(sf))
				}
				if src, ok := modCfg.Config["sourceResourceName"].(string); ok && src != "" {
					handler.SetSourceResourceName(src)
				}
				if stf, ok := modCfg.Config["stateFilter"].(string); ok && stf != "" {
					handler.SetStateFilter(stf)
				}
				// Dynamic field mapping (optional YAML override of default field names)
				if fmCfg, ok := modCfg.Config["fieldMapping"].(map[string]any); ok {
					override := module.FieldMappingFromConfig(fmCfg)
					defaults := module.DefaultRESTFieldMapping()
					defaults.Merge(override)
					handler.SetFieldMapping(defaults)
				}
				// Custom sub-action to transition mapping
				if tmCfg, ok := modCfg.Config["transitionMap"].(map[string]any); ok {
					tm := module.DefaultTransitionMap()
					for action, trans := range tmCfg {
						if t, ok := trans.(string); ok {
							tm[action] = t
						}
					}
					handler.SetTransitionMap(tm)
				}
				// Custom summary fields
				if sfCfg, ok := modCfg.Config["summaryFields"].([]any); ok {
					fields := make([]string, 0, len(sfCfg))
					for _, f := range sfCfg {
						if s, ok := f.(string); ok {
							fields = append(fields, s)
						}
					}
					if len(fields) > 0 {
						handler.SetSummaryFields(fields)
					}
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
				if origins, ok := modCfg.Config["allowedOrigins"].([]any); ok {
					allowedOrigins = make([]string, len(origins))
					for i, origin := range origins {
						if str, ok := origin.(string); ok {
							allowedOrigins[i] = str
						}
					}
				}
				if methods, ok := modCfg.Config["allowedMethods"].([]any); ok {
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
			case "http.simple_proxy":
				e.logger.Debug("Loading simple reverse proxy module")
				sp := module.NewSimpleProxy(modCfg.Name)
				if targets, ok := modCfg.Config["targets"].(map[string]any); ok {
					ts := make(map[string]string, len(targets))
					for prefix, backend := range targets {
						if s, ok := backend.(string); ok {
							ts[prefix] = s
						}
					}
					if err := sp.SetTargets(ts); err != nil {
						return fmt.Errorf("simple proxy %q: %w", modCfg.Name, err)
					}
				}
				mod = sp
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
			case "http.middleware.securityheaders":
				e.logger.Debug("Loading security headers middleware module")
				secCfg := module.SecurityHeadersConfig{}
				if v, ok := modCfg.Config["contentSecurityPolicy"].(string); ok {
					secCfg.ContentSecurityPolicy = v
				}
				if v, ok := modCfg.Config["frameOptions"].(string); ok {
					secCfg.FrameOptions = v
				}
				if v, ok := modCfg.Config["contentTypeOptions"].(string); ok {
					secCfg.ContentTypeOptions = v
				}
				if v, ok := modCfg.Config["hstsMaxAge"].(int); ok {
					secCfg.HSTSMaxAge = v
				}
				if v, ok := modCfg.Config["referrerPolicy"].(string); ok {
					secCfg.ReferrerPolicy = v
				}
				if v, ok := modCfg.Config["permissionsPolicy"].(string); ok {
					secCfg.PermissionsPolicy = v
				}
				mod = module.NewSecurityHeadersMiddleware(modCfg.Name, secCfg)
			case "dynamic.component":
				e.logger.Debug("Loading dynamic component module: " + modCfg.Name)
				if e.dynamicRegistry == nil {
					return fmt.Errorf("dynamic registry not set, cannot load dynamic component %q", modCfg.Name)
				}
				componentID := modCfg.Name
				if id, ok := modCfg.Config["componentId"].(string); ok && id != "" {
					componentID = id
				}
				// Load from source file if a dynamic loader is available and a source path is configured
				if e.dynamicLoader != nil {
					if sourcePath, ok := modCfg.Config["source"].(string); ok && sourcePath != "" {
						resolvedPath := cfg.ResolveRelativePath(sourcePath)
						if _, err := e.dynamicLoader.LoadFromFile(componentID, resolvedPath); err != nil {
							return fmt.Errorf("load dynamic component %q from %s: %w", componentID, resolvedPath, err)
						}
					}
				}
				comp, ok := e.dynamicRegistry.Get(componentID)
				if !ok {
					return fmt.Errorf("dynamic component %q not found in registry", componentID)
				}
				adapter := dynamic.NewModuleAdapter(comp)
				// Always register the module name as a provided service so processing
				// steps and other modules can look it up by name.
				providesList := []string{modCfg.Name}
				if provides, ok := modCfg.Config["provides"].([]any); ok {
					for _, p := range provides {
						if s, ok := p.(string); ok {
							providesList = append(providesList, s)
						}
					}
				}
				adapter.SetProvides(providesList)
				if requires, ok := modCfg.Config["requires"].([]any); ok {
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
				kb := module.NewKafkaBroker(modCfg.Name)
				if brokers, ok := modCfg.Config["brokers"].([]any); ok {
					bs := make([]string, 0, len(brokers))
					for _, b := range brokers {
						if s, ok := b.(string); ok {
							bs = append(bs, s)
						}
					}
					if len(bs) > 0 {
						kb.SetBrokers(bs)
					}
				}
				if groupID, ok := modCfg.Config["groupId"].(string); ok && groupID != "" {
					kb.SetGroupID(groupID)
				}
				mod = kb
			case "observability.otel":
				e.logger.Debug("Loading OpenTelemetry tracing module")
				mod = module.NewOTelTracing(modCfg.Name)
			case "static.fileserver":
				root := ""
				if r, ok := modCfg.Config["root"].(string); ok {
					root = cfg.ResolveRelativePath(r)
				}
				prefix := "/"
				if p, ok := modCfg.Config["prefix"].(string); ok && p != "" {
					prefix = p
				}
				spaFallback := true
				if sf, ok := modCfg.Config["spaFallback"].(bool); ok {
					spaFallback = sf
				}
				cacheMaxAge := 3600
				if cma, ok := modCfg.Config["cacheMaxAge"].(int); ok {
					cacheMaxAge = cma
				} else if cma, ok := modCfg.Config["cacheMaxAge"].(float64); ok {
					cacheMaxAge = int(cma)
				}
				e.logger.Debug("Loading static file server module with root: " + root)
				mod = module.NewStaticFileServer(modCfg.Name, root, prefix, spaFallback, cacheMaxAge)
			case "persistence.store":
				e.logger.Debug("Loading persistence store module")
				dbServiceName := "database"
				if name, ok := modCfg.Config["database"].(string); ok && name != "" {
					dbServiceName = name
				}
				mod = module.NewPersistenceStore(modCfg.Name, dbServiceName)
			case "auth.jwt":
				secret := ""
				if s, ok := modCfg.Config["secret"].(string); ok {
					secret = os.ExpandEnv(s)
				}
				tokenExpiry := 24 * time.Hour
				if te, ok := modCfg.Config["tokenExpiry"].(string); ok && te != "" {
					if d, err := time.ParseDuration(te); err == nil {
						tokenExpiry = d
					}
				}
				issuer := "workflow"
				if iss, ok := modCfg.Config["issuer"].(string); ok && iss != "" {
					issuer = iss
				}
				e.logger.Debug("Loading JWT auth module")
				authMod := module.NewJWTAuthModule(modCfg.Name, secret, tokenExpiry, issuer)
				if sf, ok := modCfg.Config["seedFile"].(string); ok && sf != "" {
					authMod.SetSeedFile(cfg.ResolveRelativePath(sf))
				}
				mod = authMod
			case "processing.step":
				e.logger.Debug("Loading processing step module: " + modCfg.Name)
				stepConfig := module.ProcessingStepConfig{
					ComponentID:          getStringConfig(modCfg.Config, "componentId", ""),
					SuccessTransition:    getStringConfig(modCfg.Config, "successTransition", ""),
					CompensateTransition: getStringConfig(modCfg.Config, "compensateTransition", ""),
					MaxRetries:           getIntConfig(modCfg.Config, "maxRetries", 2),
					RetryBackoffMs:       getIntConfig(modCfg.Config, "retryBackoffMs", 1000),
					TimeoutSeconds:       getIntConfig(modCfg.Config, "timeoutSeconds", 30),
				}
				mod = module.NewProcessingStep(modCfg.Name, stepConfig)
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

	// Wire AuthProviders to AuthMiddleware instances (post-init because init order is alphabetical)
	var authMiddlewares []*module.AuthMiddleware
	var authProviders []module.AuthProvider
	for _, svc := range e.app.SvcRegistry() {
		if am, ok := svc.(*module.AuthMiddleware); ok {
			authMiddlewares = append(authMiddlewares, am)
		}
		if ap, ok := svc.(module.AuthProvider); ok {
			authProviders = append(authProviders, ap)
		}
	}
	for _, am := range authMiddlewares {
		for _, ap := range authProviders {
			am.RegisterProvider(ap)
		}
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

	// Wire static file servers as catch-all routes on any available router
	for _, svc := range e.app.SvcRegistry() {
		if sfs, ok := svc.(*module.StaticFileServer); ok {
			// Find a router to attach the static file server to
			for _, routerSvc := range e.app.SvcRegistry() {
				if router, ok := routerSvc.(module.HTTPRouter); ok {
					router.AddRoute("GET", sfs.Prefix()+"{path...}", sfs)
					e.logger.Debug("Registered static file server on router at prefix: " + sfs.Prefix())
					break
				}
			}
		}
	}

	// Wire health checker endpoints on any available router (only if not already configured via workflows)
	for _, svc := range e.app.SvcRegistry() {
		if hc, ok := svc.(*module.HealthChecker); ok {
			// Register persistence health checks if any persistence stores exist
			for svcName, innerSvc := range e.app.SvcRegistry() {
				if ps, ok := innerSvc.(*module.PersistenceStore); ok {
					checkName := "persistence." + svcName
					psRef := ps // capture for closure
					hc.RegisterCheck(checkName, func(ctx context.Context) module.HealthCheckResult {
						if err := psRef.Ping(ctx); err != nil {
							return module.HealthCheckResult{Status: "degraded", Message: "database unreachable: " + err.Error()}
						}
						return module.HealthCheckResult{Status: "healthy", Message: "database connected"}
					})
					e.logger.Debug("Registered persistence health check: " + checkName)
				}
			}

			// Auto-discover any HealthCheckable services (e.g., Kafka broker)
			hc.DiscoverHealthCheckables()

			for _, routerSvc := range e.app.SvcRegistry() {
				if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
					if !router.HasRoute("GET", "/healthz") {
						router.AddRoute("GET", "/healthz", &module.HealthHTTPHandler{Handler: hc.HealthHandler()})
						router.AddRoute("GET", "/readyz", &module.HealthHTTPHandler{Handler: hc.ReadyHandler()})
						router.AddRoute("GET", "/livez", &module.HealthHTTPHandler{Handler: hc.LiveHandler()})
						e.logger.Debug("Registered health check endpoints on router")
					}
					break
				}
			}
		}
	}

	// Wire metrics collector endpoint on any available router (only if not already configured)
	for _, svc := range e.app.SvcRegistry() {
		if mc, ok := svc.(*module.MetricsCollector); ok {
			for _, routerSvc := range e.app.SvcRegistry() {
				if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
					if !router.HasRoute("GET", "/metrics") {
						router.AddRoute("GET", "/metrics", &module.MetricsHTTPHandler{Handler: mc.Handler()})
						e.logger.Debug("Registered metrics endpoint on router")
					}
					break
				}
			}
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
func (e *StdEngine) TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error {
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
func (e *StdEngine) configureTriggers(triggerConfigs map[string]any) error {
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

// getStringConfig extracts a string value from a config map with a default.
func getStringConfig(cfg map[string]any, key, defaultVal string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return defaultVal
}

// getIntConfig extracts an int value from a config map with a default.
// Handles both int and float64 (YAML numbers are decoded as float64).
func getIntConfig(cfg map[string]any, key string, defaultVal int) int {
	if v, ok := cfg[key].(int); ok {
		return v
	}
	if v, ok := cfg[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

type Engine interface {
	RegisterWorkflowHandler(handler WorkflowHandler)
	RegisterTrigger(trigger module.Trigger)
	AddModuleType(moduleType string, factory ModuleFactory)
	BuildFromConfig(cfg *config.WorkflowConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
}
