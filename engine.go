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
	"github.com/GoCodeAlone/workflow/secrets"
	"gopkg.in/yaml.v3"
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

// PipelineAdder is implemented by workflow handlers that can receive named pipelines.
// This allows the engine to add pipelines without importing the handlers package.
type PipelineAdder interface {
	AddPipeline(name string, p *module.Pipeline)
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
	secretsResolver  *secrets.MultiResolver
	stepRegistry     *module.StepRegistry
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
		secretsResolver:  secrets.NewMultiResolver(),
		stepRegistry:     module.NewStepRegistry(),
	}
}

// SecretsResolver returns the engine's multi-provider secrets resolver.
func (e *StdEngine) SecretsResolver() *secrets.MultiResolver {
	return e.secretsResolver
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

// AddStepType registers a pipeline step factory for the given step type.
func (e *StdEngine) AddStepType(stepType string, factory module.StepFactory) {
	e.stepRegistry.Register(stepType, factory)
}

// GetStepRegistry returns the engine's pipeline step registry.
func (e *StdEngine) GetStepRegistry() *module.StepRegistry {
	return e.stepRegistry
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
			case "log.collector":
				e.logger.Debug("Loading log collector module")
				lcCfg := module.LogCollectorConfig{}
				if v, ok := modCfg.Config["logLevel"].(string); ok {
					lcCfg.LogLevel = v
				}
				if v, ok := modCfg.Config["outputFormat"].(string); ok {
					lcCfg.OutputFormat = v
				}
				if v, ok := modCfg.Config["retentionDays"].(int); ok {
					lcCfg.RetentionDays = v
				}
				mod = module.NewLogCollector(modCfg.Name, lcCfg)
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
				s3Mod := module.NewS3Storage(modCfg.Name)
				if bucket, ok := modCfg.Config["bucket"].(string); ok {
					s3Mod.SetBucket(bucket)
				}
				if region, ok := modCfg.Config["region"].(string); ok {
					s3Mod.SetRegion(region)
				}
				if endpoint, ok := modCfg.Config["endpoint"].(string); ok {
					s3Mod.SetEndpoint(endpoint)
				}
				mod = s3Mod
			case "storage.local":
				e.logger.Debug("Loading local storage module")
				rootDir := "./data/storage"
				if rd, ok := modCfg.Config["rootDir"].(string); ok {
					rootDir = cfg.ResolveRelativePath(rd)
				}
				mod = module.NewLocalStorageModule(modCfg.Name, rootDir)
			case "storage.gcs":
				e.logger.Debug("Loading GCS storage module")
				gcsMod := module.NewGCSStorage(modCfg.Name)
				if bucket, ok := modCfg.Config["bucket"].(string); ok {
					gcsMod.SetBucket(bucket)
				}
				if project, ok := modCfg.Config["project"].(string); ok {
					gcsMod.SetProject(project)
				}
				if creds, ok := modCfg.Config["credentialsFile"].(string); ok {
					gcsMod.SetCredentialsFile(cfg.ResolveRelativePath(creds))
				}
				mod = gcsMod
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
				routerName := ""
				if rn, ok := modCfg.Config["router"].(string); ok {
					routerName = rn
				}
				e.logger.Debug("Loading static file server module with root: " + root)
				sfs := module.NewStaticFileServer(modCfg.Name, root, prefix, spaFallback, cacheMaxAge)
				if routerName != "" {
					sfs.SetRouterName(routerName)
				}
				mod = sfs
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
					if expanded, err := e.secretsResolver.Expand(context.Background(), s); err == nil {
						secret = expanded
					} else {
						secret = os.ExpandEnv(s)
					}
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
				if rf, ok := modCfg.Config["responseFormat"].(string); ok && rf != "" {
					authMod.SetResponseFormat(rf)
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
			case "secrets.vault":
				e.logger.Debug("Loading Vault secrets provider module")
				vm := module.NewSecretsVaultModule(modCfg.Name)
				if addr, ok := modCfg.Config["address"].(string); ok {
					vm.SetAddress(addr)
				}
				if token, ok := modCfg.Config["token"].(string); ok {
					if expanded, err := e.secretsResolver.Expand(context.Background(), token); err == nil {
						vm.SetToken(expanded)
					} else {
						vm.SetToken(token)
					}
				}
				if mp, ok := modCfg.Config["mountPath"].(string); ok && mp != "" {
					vm.SetMountPath(mp)
				}
				if ns, ok := modCfg.Config["namespace"].(string); ok && ns != "" {
					vm.SetNamespace(ns)
				}
				mod = vm
			case "secrets.aws":
				e.logger.Debug("Loading AWS Secrets Manager module")
				am := module.NewSecretsAWSModule(modCfg.Name)
				if region, ok := modCfg.Config["region"].(string); ok && region != "" {
					am.SetRegion(region)
				}
				if akid, ok := modCfg.Config["accessKeyId"].(string); ok {
					if expanded, err := e.secretsResolver.Expand(context.Background(), akid); err == nil {
						am.SetAccessKeyID(expanded)
					} else {
						am.SetAccessKeyID(akid)
					}
				}
				if sak, ok := modCfg.Config["secretAccessKey"].(string); ok {
					if expanded, err := e.secretsResolver.Expand(context.Background(), sak); err == nil {
						am.SetSecretAccessKey(expanded)
					} else {
						am.SetSecretAccessKey(sak)
					}
				}
				mod = am
			case "storage.sqlite":
				e.logger.Debug("Loading SQLite storage module: " + modCfg.Name)
				dbPath := "data/workflow.db"
				if p, ok := modCfg.Config["dbPath"].(string); ok && p != "" {
					dbPath = cfg.ResolveRelativePath(p)
				}
				sqliteStorage := module.NewSQLiteStorage(modCfg.Name, dbPath)
				if mc, ok := modCfg.Config["maxConnections"].(float64); ok && mc > 0 {
					sqliteStorage.SetMaxConnections(int(mc))
				}
				if wal, ok := modCfg.Config["walMode"].(bool); ok {
					sqliteStorage.SetWALMode(wal)
				}
				mod = sqliteStorage
			case "auth.user-store":
				e.logger.Debug("Loading user store module: " + modCfg.Name)
				mod = module.NewUserStore(modCfg.Name)
			case "workflow.registry":
				e.logger.Debug("Loading workflow registry module: " + modCfg.Name)
				storageBackend := ""
				if sb, ok := modCfg.Config["storageBackend"].(string); ok && sb != "" {
					storageBackend = sb
				}
				mod = module.NewWorkflowRegistry(modCfg.Name, storageBackend)
			case "openapi.generator":
				e.logger.Debug("Loading OpenAPI generator module")
				genConfig := module.OpenAPIGeneratorConfig{}
				if title, ok := modCfg.Config["title"].(string); ok {
					genConfig.Title = title
				}
				if version, ok := modCfg.Config["version"].(string); ok {
					genConfig.Version = version
				}
				if desc, ok := modCfg.Config["description"].(string); ok {
					genConfig.Description = desc
				}
				if servers, ok := modCfg.Config["servers"].([]any); ok {
					for _, s := range servers {
						if str, ok := s.(string); ok {
							genConfig.Servers = append(genConfig.Servers, str)
						}
					}
				}
				mod = module.NewOpenAPIGenerator(modCfg.Name, genConfig)
			case "openapi.consumer":
				e.logger.Debug("Loading OpenAPI consumer module")
				consConfig := module.OpenAPIConsumerConfig{}
				if specURL, ok := modCfg.Config["specUrl"].(string); ok {
					consConfig.SpecURL = specURL
				}
				if specFile, ok := modCfg.Config["specFile"].(string); ok {
					consConfig.SpecFile = cfg.ResolveRelativePath(specFile)
				}
				consumer := module.NewOpenAPIConsumer(modCfg.Name, consConfig)
				if fmCfg, ok := modCfg.Config["fieldMapping"].(map[string]any); ok {
					override := module.FieldMappingFromConfig(fmCfg)
					consumer.SetFieldMapping(override)
				}
				mod = consumer
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

	// Build lookup maps from config for intelligent static file server wiring.
	routerNames := make(map[string]bool)      // set of router module names
	serverToRouter := make(map[string]string) // server name → router name (via router's dependsOn)
	sfsDeps := make(map[string][]string)      // static file server name → dependsOn list
	for _, modCfg := range cfg.Modules {
		switch modCfg.Type {
		case "http.router":
			routerNames[modCfg.Name] = true
			// Track which server this router depends on (reverse map: server→router)
			for _, dep := range modCfg.DependsOn {
				serverToRouter[dep] = modCfg.Name
			}
		case "static.fileserver":
			sfsDeps[modCfg.Name] = modCfg.DependsOn
		}
	}

	// Wire static file servers as catch-all routes on their associated router.
	// Priority: 1) explicit router config, 2) dependsOn referencing a router,
	// 3) dependsOn referencing a server → find that server's router, 4) first available.
	for _, svc := range e.app.SvcRegistry() {
		if sfs, ok := svc.(*module.StaticFileServer); ok {
			var targetRouter module.HTTPRouter
			targetName := sfs.RouterName()

			// 1) Explicit router name from config
			if targetName != "" {
				for svcName, routerSvc := range e.app.SvcRegistry() {
					if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == targetName {
						targetRouter = router
						break
					}
				}
			}

			// 2) Check dependsOn for a direct router reference
			if targetRouter == nil {
				for _, dep := range sfsDeps[sfs.Name()] {
					if routerNames[dep] {
						for svcName, routerSvc := range e.app.SvcRegistry() {
							if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == dep {
								targetRouter = router
								targetName = dep
								break
							}
						}
						if targetRouter != nil {
							break
						}
					}
				}
			}

			// 3) Check dependsOn for a server reference, then find that server's router
			if targetRouter == nil {
				for _, dep := range sfsDeps[sfs.Name()] {
					if rName, ok := serverToRouter[dep]; ok {
						for svcName, routerSvc := range e.app.SvcRegistry() {
							if router, ok := routerSvc.(module.HTTPRouter); ok && svcName == rName {
								targetRouter = router
								targetName = rName
								break
							}
						}
						if targetRouter != nil {
							break
						}
					}
				}
			}

			// 4) Fall back to first available router
			if targetRouter == nil {
				for _, routerSvc := range e.app.SvcRegistry() {
					if router, ok := routerSvc.(module.HTTPRouter); ok {
						targetRouter = router
						break
					}
				}
			}

			if targetRouter != nil {
				targetRouter.AddRoute("GET", sfs.Prefix()+"{path...}", sfs)
				routerLabel := ""
				if targetName != "" {
					routerLabel = " " + targetName
				}
				e.logger.Debug("Registered static file server " + sfs.Name() + " on router" + routerLabel + " at prefix: " + sfs.Prefix())
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

	// Wire log collector endpoint on any available router (only if not already configured)
	for _, svc := range e.app.SvcRegistry() {
		if lc, ok := svc.(*module.LogCollector); ok {
			for _, routerSvc := range e.app.SvcRegistry() {
				if router, ok := routerSvc.(*module.StandardHTTPRouter); ok {
					if !router.HasRoute("GET", "/logs") {
						router.AddRoute("GET", "/logs", &module.LogHTTPHandler{Handler: lc.LogHandler()})
						e.logger.Debug("Registered log collector endpoint on router")
					}
					break
				}
			}
		}
	}

	// Wire OpenAPI generators: build spec from workflow route definitions
	for _, svc := range e.app.SvcRegistry() {
		if gen, ok := svc.(*module.OpenAPIGenerator); ok {
			gen.BuildSpec(cfg.Workflows)
			e.logger.Debug("Built OpenAPI spec for generator: " + gen.Name())
		}
	}

	// Configure triggers (new section)
	if err := e.configureTriggers(cfg.Triggers); err != nil {
		return fmt.Errorf("failed to configure triggers: %w", err)
	}

	// Configure pipelines (composable step-based workflows)
	if len(cfg.Pipelines) > 0 {
		if err := e.configurePipelines(cfg.Pipelines); err != nil {
			return fmt.Errorf("failed to configure pipelines: %w", err)
		}
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

// configurePipelines creates Pipeline objects from config and registers them
// with the PipelineWorkflowHandler.
func (e *StdEngine) configurePipelines(pipelineCfg map[string]any) error {
	// Find the PipelineAdder among registered workflow handlers
	var adder PipelineAdder
	for _, handler := range e.workflowHandlers {
		if a, ok := handler.(PipelineAdder); ok {
			adder = a
			break
		}
	}
	if adder == nil {
		return fmt.Errorf("no PipelineWorkflowHandler registered; cannot configure pipelines")
	}

	for pipelineName, rawCfg := range pipelineCfg {
		// Marshal to YAML then unmarshal into PipelineConfig to leverage struct tags
		yamlBytes, err := yaml.Marshal(rawCfg)
		if err != nil {
			return fmt.Errorf("pipeline %q: failed to marshal config: %w", pipelineName, err)
		}
		var pipeCfg config.PipelineConfig
		if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
			return fmt.Errorf("pipeline %q: failed to parse config: %w", pipelineName, err)
		}

		// Build steps
		steps, err := e.buildPipelineSteps(pipelineName, pipeCfg.Steps)
		if err != nil {
			return fmt.Errorf("pipeline %q: %w", pipelineName, err)
		}

		// Build compensation steps
		compSteps, err := e.buildPipelineSteps(pipelineName, pipeCfg.Compensation)
		if err != nil {
			return fmt.Errorf("pipeline %q compensation: %w", pipelineName, err)
		}

		// Parse error strategy
		onError := module.ErrorStrategyStop
		switch pipeCfg.OnError {
		case "skip":
			onError = module.ErrorStrategySkip
		case "compensate":
			onError = module.ErrorStrategyCompensate
		}

		// Parse timeout
		var timeout time.Duration
		if pipeCfg.Timeout != "" {
			timeout, err = time.ParseDuration(pipeCfg.Timeout)
			if err != nil {
				return fmt.Errorf("pipeline %q: invalid timeout %q: %w", pipelineName, pipeCfg.Timeout, err)
			}
		}

		pipeline := &module.Pipeline{
			Name:         pipelineName,
			Steps:        steps,
			OnError:      onError,
			Timeout:      timeout,
			Compensation: compSteps,
		}

		adder.AddPipeline(pipelineName, pipeline)
		e.logger.Info(fmt.Sprintf("Configured pipeline: %s (%d steps)", pipelineName, len(steps)))

		// Create trigger from inline trigger config if present
		if pipeCfg.Trigger.Type != "" {
			triggerCfg := pipeCfg.Trigger.Config
			if triggerCfg == nil {
				triggerCfg = make(map[string]any)
			}
			// Inject the pipeline name as the workflow type for the trigger
			triggerCfg["workflowType"] = "pipeline:" + pipelineName

			// Find a matching trigger and configure it
			for _, trigger := range e.triggers {
				if canHandleTrigger(trigger, pipeCfg.Trigger.Type) {
					if err := trigger.Configure(e.app, triggerCfg); err != nil {
						return fmt.Errorf("pipeline %q trigger: %w", pipelineName, err)
					}
					break
				}
			}
		}
	}

	return nil
}

// buildPipelineSteps creates PipelineStep instances from step configurations.
func (e *StdEngine) buildPipelineSteps(pipelineName string, stepCfgs []config.PipelineStepConfig) ([]module.PipelineStep, error) {
	if len(stepCfgs) == 0 {
		return nil, nil
	}

	steps := make([]module.PipelineStep, 0, len(stepCfgs))
	for _, sc := range stepCfgs {
		stepConfig := sc.Config
		if stepConfig == nil {
			stepConfig = make(map[string]any)
		}

		step, err := e.stepRegistry.Create(sc.Type, sc.Name, stepConfig, e.app)
		if err != nil {
			return nil, fmt.Errorf("step %q (type %s): %w", sc.Name, sc.Type, err)
		}
		steps = append(steps, step)
	}

	return steps, nil
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
