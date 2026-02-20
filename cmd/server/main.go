package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/admin"
	"github.com/GoCodeAlone/workflow/ai"
	copilotai "github.com/GoCodeAlone/workflow/ai/copilot"
	"github.com/GoCodeAlone/workflow/ai/llm"
	"github.com/GoCodeAlone/workflow/audit"
	"github.com/GoCodeAlone/workflow/billing"
	"github.com/GoCodeAlone/workflow/bundle"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/environment"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/observability"
	"github.com/GoCodeAlone/workflow/observability/tracing"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/docmanager"
	"github.com/GoCodeAlone/workflow/plugin/storebrowser"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
	"github.com/GoCodeAlone/workflow/provider/aws"
	"github.com/GoCodeAlone/workflow/provider/azure"
	"github.com/GoCodeAlone/workflow/provider/digitalocean"
	"github.com/GoCodeAlone/workflow/provider/gcp"
	"github.com/GoCodeAlone/workflow/schema"
	evstore "github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

var (
	configFile     = flag.String("config", "", "Path to workflow configuration YAML file")
	addr           = flag.String("addr", ":8080", "HTTP listen address (workflow engine)")
	copilotCLI     = flag.String("copilot-cli", "", "Path to Copilot CLI binary")
	copilotModel   = flag.String("copilot-model", "", "Model to use with Copilot SDK")
	anthropicKey   = flag.String("anthropic-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env)")
	anthropicModel = flag.String("anthropic-model", "", "Anthropic model name")

	// Multi-workflow mode flags
	databaseDSN   = flag.String("database-dsn", "", "PostgreSQL connection string for multi-workflow mode")
	jwtSecret     = flag.String("jwt-secret", "", "JWT signing secret for API authentication")
	adminEmail    = flag.String("admin-email", "", "Initial admin user email (first-run bootstrap)")
	adminPassword = flag.String("admin-password", "", "Initial admin user password (first-run bootstrap)")

	// v1 API flags
	dataDir       = flag.String("data-dir", "./data", "Directory for SQLite database and persistent data")
	restoreAdmin  = flag.Bool("restore-admin", false, "Restore admin config to embedded default on startup")
	loadWorkflows = flag.String("load-workflows", "", "Comma-separated paths to workflow YAML files or directories to load alongside admin")
	importBundle  = flag.String("import-bundle", "", "Comma-separated paths to .tar.gz workflow bundles to import and deploy on startup")
)

// buildEngine creates the workflow engine with all handlers registered and built from config.
func buildEngine(cfg *config.WorkflowConfig, logger *slog.Logger) (*workflow.StdEngine, *dynamic.Loader, *dynamic.ComponentRegistry, *handlers.PipelineWorkflowHandler, error) {
	app := modular.NewStdApplication(nil, logger)
	engine := workflow.NewStdEngine(app, logger)

	// Load all engine plugins — each registers its module factories, step factories,
	// trigger factories, and workflow handlers via engine.LoadPlugin.
	plugins := []plugin.EnginePlugin{
		pluginhttp.New(),
		pluginobs.New(),
		pluginmessaging.New(),
		pluginsm.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		pluginpipeline.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
	}
	for _, p := range plugins {
		if err := engine.LoadPlugin(p); err != nil {
			log.Fatalf("Failed to load plugin %s: %v", p.Name(), err)
		}
	}

	// Register handlers and steps not covered by plugins
	engine.RegisterWorkflowHandler(handlers.NewPlatformWorkflowHandler())

	pipelineHandler := handlers.NewPipelineWorkflowHandler()
	pipelineHandler.SetStepRegistry(engine.GetStepRegistry())
	pipelineHandler.SetLogger(logger)
	engine.RegisterWorkflowHandler(pipelineHandler)

	engine.AddStepType("step.platform_template", module.NewPlatformTemplateStepFactory())

	// Register reconciliation trigger (not provided by any plugin)
	engine.RegisterTrigger(module.NewReconciliationTrigger())

	// Set up dynamic component system
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	engine.SetDynamicRegistry(registry)
	engine.SetDynamicLoader(loader)

	// Set up plugin installer for auto-install during deploy
	pluginInstallDir := filepath.Join(*dataDir, "plugins")
	pluginLocalReg := plugin.NewLocalRegistry()
	pluginRemoteReg := plugin.NewRemoteRegistry("https://plugins.workflow.dev")
	installer := plugin.NewPluginInstaller(pluginRemoteReg, pluginLocalReg, loader, pluginInstallDir)

	// Scan previously installed plugins
	if _, scanErr := installer.ScanInstalled(); scanErr != nil {
		logger.Warn("Failed to scan installed plugins", "error", scanErr)
	}
	engine.SetPluginInstaller(installer)

	// Build engine from config
	if err := engine.BuildFromConfig(cfg); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to build workflow: %w", err)
	}

	return engine, loader, registry, pipelineHandler, nil
}

// loadConfig loads a workflow configuration from the configured file path,
// or returns an empty config if no path is set.
func loadConfig(logger *slog.Logger) (*config.WorkflowConfig, error) {
	if *configFile != "" {
		cfg, err := config.LoadFromFile(*configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load configuration: %w", err)
		}
		return cfg, nil
	}
	logger.Info("No config file specified, using empty workflow config")
	return config.NewEmptyWorkflowConfig(), nil
}

// serverApp holds all components needed to run the server. Persistent resources
// (stores, handlers, muxes) are stored here so they survive engine reloads.
// Only the engine itself (modules, handlers, triggers) is recreated on reload.
type serverApp struct {
	engine          *workflow.StdEngine
	engineManager   *workflow.WorkflowEngineManager
	pipelineHandler *handlers.PipelineWorkflowHandler
	logger          *slog.Logger
	auditLogger     *audit.Logger
	cleanupDirs     []string       // temp directories to clean up on shutdown
	cleanupFiles    []string       // temp files to clean up on shutdown
	postStartFuncs  []func() error // functions to run after engine.Start

	// -----------------------------------------------------------------------
	// Persistent stores — opened once, survive reloads
	// -----------------------------------------------------------------------
	v1Store       *module.V1Store           // v1 API SQLite store
	eventStore    *evstore.SQLiteEventStore // execution event store
	idempotencyDB *sql.DB                   // idempotency store DB connection
	envStore      *environment.SQLiteStore  // environment management store

	// -----------------------------------------------------------------------
	// Management service handlers — created once at startup, survive reloads.
	// These are http.Handlers or service objects that get registered with each
	// new Application instance after engine reload.
	// -----------------------------------------------------------------------

	// Pre-start services (registered before engine.Start)
	mgmtHandler *module.WorkflowUIHandler // engine config/reload/status
	schemaSvc   *schema.SchemaService     // module schema browsing
	combinedAI  http.Handler              // AI generation + deploy
	dynHandler  http.Handler              // dynamic components API

	// Post-start services (registered after engine.Start, need V1Store)
	v1Handler        *module.V1APIHandler     // V1 API handler (dashboard)
	timelineMux      http.Handler             // timeline handler mux
	replayMux        http.Handler             // replay handler mux
	backfillMux      http.Handler             // backfill/mock/diff handler mux
	dlqMux           http.Handler             // DLQ handler mux
	billingMux       http.Handler             // billing handler mux
	nativeHandler    http.Handler             // native plugin handler
	envMux           http.Handler             // environment management mux
	cloudMux         http.Handler             // cloud providers mux
	pluginRegMux     http.Handler             // plugin registry mux
	executionTracker *module.ExecutionTracker // execution tracking for CQRS
	runtimeManager   *module.RuntimeManager   // filesystem-loaded workflow instances
	runtimeMux       http.Handler             // runtime instances API
	ingestMux        http.Handler             // ingest API for remote workers
	reporter         *observability.Reporter  // reporter for sending data to admin (when worker)
}

// setup initializes all server components: engine, AI services, and HTTP mux.
func setup(logger *slog.Logger, cfg *config.WorkflowConfig) (*serverApp, error) {
	app := &serverApp{
		logger: logger,
	}

	// Merge admin config into primary config — admin UI is always enabled.
	// The admin config provides all management endpoints (auth, API, schema,
	// AI, dynamic components) via the engine's own modules and routes.
	if err := mergeAdminConfig(logger, cfg, app); err != nil {
		return nil, fmt.Errorf("failed to set up admin: %w", err)
	}

	engine, loader, registry, pipelineHandler, err := buildEngine(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build engine: %w", err)
	}
	app.engine = engine
	app.pipelineHandler = pipelineHandler

	// Initialize AI services and dynamic component pool
	pool := dynamic.NewInterpreterPool()
	aiSvc, deploySvc := initAIService(logger, registry, pool)

	// Create all management handlers (once, stored on serverApp).
	initManagementHandlers(logger, engine, cfg, app, aiSvc, deploySvc, loader, registry)

	// Register management services with the initial engine.
	registerManagementServices(logger, app)

	// Set up post-start hook to initialize stores and register post-start services.
	app.postStartFuncs = append(app.postStartFuncs, func() error {
		if err := app.initStores(logger); err != nil {
			return err
		}
		return app.registerPostStartServices(logger)
	}, func() error {
		return app.importBundles(logger)
	})

	// Initialize audit logger (writes structured JSON to stdout)
	app.auditLogger = audit.NewLogger(os.Stdout)
	app.auditLogger.LogConfigChange(context.Background(), "system", "server", "server started")

	return app, nil
}

// mergeAdminConfig loads the embedded admin config, extracts assets to temp
// locations, and merges admin modules/routes into the primary config.
// If the config already contains admin modules (e.g., the user passed the
// admin config directly), the merge is skipped to avoid duplicates — but
// UI assets are still extracted and injected so the static fileserver works.
func mergeAdminConfig(logger *slog.Logger, cfg *config.WorkflowConfig, app *serverApp) error {
	// Always extract UI assets so the static fileserver has correct paths
	uiDir, err := admin.WriteUIAssets()
	if err != nil {
		return err
	}
	app.cleanupDirs = append(app.cleanupDirs, uiDir)

	// Check if the config already contains admin modules
	for _, m := range cfg.Modules {
		if m.Name == "admin-server" {
			logger.Info("Config already contains admin modules, skipping merge")
			// Still inject the correct UI root into the existing config
			admin.InjectUIRoot(cfg, uiDir)
			logger.Info("Admin UI enabled", "uiDir", uiDir)
			return nil
		}
	}

	adminCfg, err := admin.LoadConfig()
	if err != nil {
		return err
	}

	admin.InjectUIRoot(adminCfg, uiDir)

	// Merge admin modules and routes into primary config
	admin.MergeInto(cfg, adminCfg)

	logger.Info("Admin UI enabled",
		"uiDir", uiDir,
	)
	return nil
}

// initManagementHandlers creates all management service handlers and stores
// them on the serverApp struct. These handlers are created once and persist
// across engine reloads. Only the service registrations need to be refreshed.
func initManagementHandlers(logger *slog.Logger, engine *workflow.StdEngine, cfg *config.WorkflowConfig, app *serverApp, aiSvc *ai.Service, deploySvc *ai.DeployService, loader *dynamic.Loader, registry *dynamic.ComponentRegistry) {
	// Workflow management handler (config, reload, validate, status)
	mgmtHandler := module.NewWorkflowUIHandler(cfg)
	mgmtHandler.SetReloadFunc(func(newCfg *config.WorkflowConfig) error {
		return app.reloadEngine(newCfg)
	})
	mgmtHandler.SetStatusFunc(func() map[string]any {
		return map[string]any{"status": "running"}
	})
	mgmtHandler.SetServiceRegistry(func() map[string]any {
		return app.engine.App().SvcRegistry()
	})
	app.mgmtHandler = mgmtHandler

	// AI handlers (combined into a single http.Handler)
	aiH := ai.NewHandler(aiSvc)
	deployH := ai.NewDeployHandler(deploySvc)
	app.combinedAI = ai.NewCombinedHandler(aiH, deployH)

	// Dynamic components handler
	app.dynHandler = dynamic.NewAPIHandler(loader, registry)

	// Schema handler
	app.schemaSvc = schema.NewSchemaService()
}

// registerManagementServices registers the pre-start management service handlers
// with the current engine's Application. This is called at startup and again
// after each engine reload. It is idempotent — the new Application starts with
// an empty service registry, so there are no duplicate registration errors.
func registerManagementServices(logger *slog.Logger, app *serverApp) {
	engine := app.engine

	// Register service modules — these are resolved by delegate config in admin/config.yaml
	svcModules := map[string]any{
		"admin-engine-mgmt":    app.mgmtHandler,
		"admin-schema-mgmt":    app.schemaSvc,
		"admin-ai-mgmt":        app.combinedAI,
		"admin-component-mgmt": app.dynHandler,
	}
	for name, svc := range svcModules {
		engine.GetApp().RegisterModule(module.NewServiceModule(name, svc))
		if err := engine.GetApp().RegisterService(name, svc); err != nil {
			logger.Warn("Failed to register service directly", "name", name, "error", err)
		}
	}

	// Re-resolve delegates on CQRS handlers now that services are available
	for _, svc := range engine.GetApp().SvcRegistry() {
		switch h := svc.(type) {
		case *module.QueryHandler:
			h.ResolveDelegatePostStart()
		case *module.CommandHandler:
			h.ResolveDelegatePostStart()
		}
	}
	logger.Info("Registered admin service modules for delegate dispatch")

	// Enrich OpenAPI spec via the service registry
	for _, svc := range engine.GetApp().SvcRegistry() {
		if gen, ok := svc.(*module.OpenAPIGenerator); ok {
			module.RegisterAdminSchemas(gen)
			gen.ApplySchemas()
			logger.Info("Registered typed OpenAPI schemas", "module", gen.Name())
		}
	}
}

// initStores opens all persistent databases and creates service handlers.
// This is called once after the first engine.Start. The stores and handlers
// are stored on serverApp and survive engine reloads.
func (app *serverApp) initStores(logger *slog.Logger) error {
	engine := app.engine

	// Resolve JWT secret from flag or env
	secret := envOrFlag("JWT_SECRET", jwtSecret)
	if secret == "" {
		logger.Warn("v1 API handler: no JWT secret configured; auth will fail")
	}

	// -----------------------------------------------------------------------
	// V1Store — the main workflow/company/project data store
	// -----------------------------------------------------------------------

	// Discover the WorkflowRegistry from the service registry
	var store *module.V1Store
	for _, svc := range engine.GetApp().SvcRegistry() {
		if reg, ok := svc.(*module.WorkflowRegistry); ok {
			store = reg.Store()
			logger.Info("Using WorkflowRegistry store", "module", reg.Name())
			break
		}
	}

	// Fallback: open a standalone V1Store if no WorkflowRegistry was found
	if store == nil {
		dbPath := filepath.Join(*dataDir, "workflow.db")
		var err error
		store, err = module.OpenV1Store(dbPath)
		if err != nil {
			return fmt.Errorf("open v1 store at %s: %w", dbPath, err)
		}
		logger.Info("Opened standalone v1 data store (no WorkflowRegistry found)", "path", dbPath)
	}
	app.v1Store = store

	// If --restore-admin, reset the system workflow to the embedded default
	if *restoreAdmin {
		adminCfgData, err := admin.LoadConfigRaw()
		if err != nil {
			logger.Warn("Failed to load embedded admin config for restore", "error", err)
		} else if resetErr := store.ResetSystemWorkflow(string(adminCfgData)); resetErr != nil {
			logger.Info("No system workflow to reset (first run)")
		} else {
			logger.Info("Restored admin config to embedded default")
		}
	}

	// Ensure the system hierarchy exists (Company -> Org -> Project -> Workflow).
	// This is idempotent -- if it already exists, it returns the existing IDs.
	adminCfgData, loadErr := admin.LoadConfigRaw()
	if loadErr != nil {
		logger.Warn("Failed to load embedded admin config for system hierarchy", "error", loadErr)
	} else {
		_, _, _, _, ensureErr := store.EnsureSystemHierarchy("system", string(adminCfgData))
		if ensureErr != nil {
			logger.Warn("Failed to ensure system hierarchy", "error", ensureErr)
		} else {
			logger.Info("System hierarchy ready")
		}
	}

	// Create V1 API handler
	v1Handler := module.NewV1APIHandler(store, secret)
	v1Handler.SetReloadFunc(func(configYAML string) error {
		cfg, parseErr := config.LoadFromString(configYAML)
		if parseErr != nil {
			return fmt.Errorf("invalid config: %w", parseErr)
		}
		return app.reloadEngine(cfg)
	})
	app.v1Handler = v1Handler

	// -----------------------------------------------------------------------
	// Feature Flag service — wire into the V1 API handler so the admin UI
	// can manage flags via /api/v1/admin/feature-flags.
	// -----------------------------------------------------------------------

	ffDBPath := filepath.Join(*dataDir, "featureflags.db")
	ffMod, ffErr := module.NewFeatureFlagModule("admin-feature-flags", module.FeatureFlagModuleConfig{
		Provider:   "generic",
		CacheTTL:   "30s",
		SSEEnabled: true,
		DBPath:     ffDBPath,
	})
	if ffErr != nil {
		logger.Warn("Failed to create feature flag module — feature flags disabled", "error", ffErr)
	} else {
		ffAdapter := module.NewFeatureFlagAdminAdapter(ffMod.Service(), ffMod.Store())
		v1Handler.SetFeatureFlagService(ffAdapter)
		logger.Info("Wired feature flag service to V1 API handler", "db", ffDBPath)
	}

	// -----------------------------------------------------------------------
	// Event store, idempotency store
	// -----------------------------------------------------------------------

	// Create SQLite event store for execution events
	eventsDBPath := filepath.Join(*dataDir, "events.db")
	eventStore, err := evstore.NewSQLiteEventStore(eventsDBPath)
	if err != nil {
		logger.Warn("Failed to create event store — timeline/replay/diff features disabled", "error", err)
	} else {
		app.eventStore = eventStore
		logger.Info("Opened event store", "path", eventsDBPath)
	}

	// Create SQLite idempotency store (separate DB connection, same data dir)
	idempotencyDBPath := filepath.Join(*dataDir, "idempotency.db")
	idempotencyDSN := idempotencyDBPath + "?_journal_mode=WAL&_busy_timeout=5000"
	idempotencyDB, err := sql.Open("sqlite", idempotencyDSN)
	if err != nil {
		logger.Warn("Failed to open idempotency DB", "error", err)
	} else {
		idempotencyDB.SetMaxOpenConns(1)
		app.idempotencyDB = idempotencyDB
		idempotencyStore, idErr := evstore.NewSQLiteIdempotencyStore(idempotencyDB)
		if idErr != nil {
			logger.Warn("Failed to create idempotency store", "error", idErr)
		} else {
			logger.Info("Opened idempotency store", "path", idempotencyDBPath)
			_ = idempotencyStore // registered for future pipeline integration
		}
	}

	// -----------------------------------------------------------------------
	// Timeline, replay, backfill handlers
	// -----------------------------------------------------------------------

	if eventStore != nil {
		// Timeline handler (execution list, timeline, events)
		timelineHandler := evstore.NewTimelineHandler(eventStore, logger)
		timelineMux := http.NewServeMux()
		timelineHandler.RegisterRoutes(timelineMux)
		app.timelineMux = timelineMux

		// Replay handler
		replayHandler := evstore.NewReplayHandler(eventStore, logger)
		replayMux := http.NewServeMux()
		replayHandler.RegisterRoutes(replayMux)
		app.replayMux = replayMux

		// Backfill / Mock / Diff handler
		backfillStore := evstore.NewInMemoryBackfillStore()
		mockStore := evstore.NewInMemoryStepMockStore()
		diffCalc := evstore.NewDiffCalculator(eventStore)
		bmdHandler := evstore.NewBackfillMockDiffHandler(backfillStore, mockStore, diffCalc, logger)
		bmdMux := http.NewServeMux()
		bmdHandler.RegisterRoutes(bmdMux)
		app.backfillMux = bmdMux

		logger.Info("Created timeline, replay, and backfill/mock/diff handlers")
	} else {
		// Create stub handlers so delegate routes return 503 instead of 500
		stubMsg := "event store unavailable — timeline/replay/backfill features disabled"
		app.timelineMux = featureDisabledHandler(stubMsg)
		app.replayMux = featureDisabledHandler(stubMsg)
		app.backfillMux = featureDisabledHandler(stubMsg)
		logger.Info("Created stub handlers for timeline/replay/backfill (event store unavailable)")
	}

	// -----------------------------------------------------------------------
	// DLQ handler
	// -----------------------------------------------------------------------

	dlqStore := evstore.NewInMemoryDLQStore()
	dlqHandler := evstore.NewDLQHandler(dlqStore, logger)
	dlqMux := http.NewServeMux()
	dlqHandler.RegisterRoutes(dlqMux)
	app.dlqMux = dlqMux

	// -----------------------------------------------------------------------
	// Billing handler
	// -----------------------------------------------------------------------

	billingMeter := billing.NewInMemoryMeter()
	billingProvider := billing.NewMockBillingProvider()
	billingHandler := billing.NewHandler(billingMeter, billingProvider)
	billingMux := http.NewServeMux()
	billingHandler.RegisterRoutes(billingMux)
	app.billingMux = billingMux

	logger.Info("Created DLQ and billing handlers")

	// -----------------------------------------------------------------------
	// Native plugins
	// -----------------------------------------------------------------------

	// Use the V1Store's DB for the PluginManager so plugin state is persisted
	// alongside workflow data (avoids a separate DB file).
	var pluginDB *sql.DB
	if store != nil {
		pluginDB = store.DB()
	}
	pluginMgr := plugin.NewPluginManager(pluginDB, logger)

	// Store Browser plugin — needs the V1Store's DB, event store, and DLQ store
	if store != nil {
		sbPlugin := storebrowser.New(store.DB(), eventStore, dlqStore)
		if err := pluginMgr.Register(sbPlugin); err != nil {
			logger.Warn("Failed to register store-browser plugin", "error", err)
		}
	}

	// Doc Manager plugin — needs the V1Store's DB for the workflow_docs table
	if store != nil {
		dmPlugin := docmanager.New(store.DB())
		if err := pluginMgr.Register(dmPlugin); err != nil {
			logger.Warn("Failed to register doc-manager plugin", "error", err)
		}
	}

	// Cloud provider plugins
	awsProvider := aws.NewAWSProvider(aws.AWSConfig{})
	gcpProvider := gcp.NewGCPProvider(gcp.GCPConfig{})
	azureProvider := azure.NewAzureProvider(azure.AzureConfig{})
	doProvider := digitalocean.NewDOProvider(digitalocean.DOConfig{})

	// Deploy executor with strategy registry
	strategyReg := deploy.NewStrategyRegistry(logger)
	deployExecutor := deploy.NewExecutor(strategyReg)
	deployExecutor.RegisterProvider("aws", awsProvider)
	deployExecutor.RegisterProvider("gcp", gcpProvider)
	deployExecutor.RegisterProvider("azure", azureProvider)
	deployExecutor.RegisterProvider("digitalocean", doProvider)

	// Register cloud provider plugins with PluginManager
	for _, p := range []plugin.NativePlugin{awsProvider, gcpProvider, azureProvider, doProvider} {
		if err := pluginMgr.Register(p); err != nil {
			logger.Warn("Failed to register cloud provider plugin", "plugin", p.Name(), "error", err)
		}
	}

	// Feature flags plugin — register with PluginManager for Marketplace discovery
	if err := pluginMgr.Register(pluginff.New()); err != nil {
		logger.Warn("Failed to register feature-flags plugin", "error", err)
	}

	// Enable all registered plugins so their routes are active
	allPlugins := pluginMgr.AllPlugins()
	for i := range allPlugins {
		if !allPlugins[i].Enabled {
			if err := pluginMgr.Enable(allPlugins[i].Name); err != nil {
				logger.Warn("Failed to enable plugin", "plugin", allPlugins[i].Name, "error", err)
			}
		}
	}

	// Plugin discovery + route handler (backed by PluginManager)
	nativeHandler := plugin.NewNativeHandler(pluginMgr)
	app.nativeHandler = nativeHandler

	// Register provider routes on the cloud mux
	cloudMux := http.NewServeMux()
	awsProvider.RegisterRoutes(cloudMux)
	gcpProvider.RegisterRoutes(cloudMux)
	azureProvider.RegisterRoutes(cloudMux)
	doProvider.RegisterRoutes(cloudMux)
	app.cloudMux = cloudMux

	logger.Info("Registered cloud providers", "providers", []string{"aws", "gcp", "azure", "digitalocean"})
	logger.Info("Registered native plugins", "count", len(pluginMgr.AllPlugins()))

	// -----------------------------------------------------------------------
	// Environment management
	// -----------------------------------------------------------------------

	envDBPath := filepath.Join(*dataDir, "environments.db")
	envStore, envErr := environment.NewSQLiteStore(envDBPath)
	if envErr != nil {
		logger.Warn("Failed to create environment store — environment management disabled", "error", envErr)
		app.envMux = featureDisabledHandler("environment store unavailable — environment management disabled")
	} else {
		app.envStore = envStore
		envHandler := environment.NewHandler(envStore)
		envMux := http.NewServeMux()
		envHandler.RegisterRoutes(envMux)
		app.envMux = envMux
		logger.Info("Registered environment management service", "path", envDBPath)
	}

	// -----------------------------------------------------------------------
	// Plugin composite registry
	// -----------------------------------------------------------------------

	pluginLocalReg := plugin.NewLocalRegistry()
	pluginRemoteReg := plugin.NewRemoteRegistry("https://plugins.workflow.dev")
	compositeReg := plugin.NewCompositeRegistry(pluginLocalReg, pluginRemoteReg)
	pluginHandler := plugin.NewRegistryHandler(compositeReg)
	pluginMux := http.NewServeMux()
	pluginHandler.RegisterRoutes(pluginMux)
	app.pluginRegMux = pluginMux

	logger.Info("Registered plugin composite registry (local + remote)")

	// -----------------------------------------------------------------------
	// Execution tracker
	// -----------------------------------------------------------------------

	workflowID := ""
	if sysWf, sysErr := store.GetSystemWorkflow(); sysErr == nil && sysWf != nil {
		workflowID = sysWf.ID
	}
	app.executionTracker = &module.ExecutionTracker{
		Store:      store,
		WorkflowID: workflowID,
		Tracer:     tracing.NewWorkflowTracer(nil), // uses global OTEL provider
	}

	// -----------------------------------------------------------------------
	// Ingest handler — receives observability data from remote workers
	// -----------------------------------------------------------------------

	ingestStore := observability.NewV1IngestStore(store.DB())
	ingestHandler := observability.NewIngestHandler(ingestStore, logger)
	ingestMux := http.NewServeMux()
	ingestHandler.RegisterRoutes(ingestMux)
	app.ingestMux = ingestMux
	logger.Info("Registered ingest handler for remote worker observability")

	// -----------------------------------------------------------------------
	// Reporter — if WORKFLOW_ADMIN_URL is set, report to admin server
	// -----------------------------------------------------------------------

	if reporter := observability.ReporterFromEnv(logger); reporter != nil {
		app.reporter = reporter
		reporter.Start(context.Background())
		logger.Info("Started observability reporter", "admin_url", os.Getenv("WORKFLOW_ADMIN_URL"))
	}

	// -----------------------------------------------------------------------
	// Runtime manager — load workflows from --load-workflows flag
	// -----------------------------------------------------------------------

	// Always create a RuntimeManager (returns empty list when no workflows loaded)
	runtimeBuilder := func(cfg *config.WorkflowConfig, lg *slog.Logger) (func(context.Context) error, error) {
		eng, _, _, _, buildErr := buildEngine(cfg, lg)
		if buildErr != nil {
			return nil, buildErr
		}
		if startErr := eng.Start(context.Background()); startErr != nil {
			return nil, startErr
		}
		return func(ctx context.Context) error {
			return eng.Stop(ctx)
		}, nil
	}

	rm := module.NewRuntimeManager(store, runtimeBuilder, logger)
	app.runtimeManager = rm
	v1Handler.SetRuntimeManager(rm)

	// Wire up port allocator for auto-port assignment on deployed workflows.
	// Start allocating at 8082 (admin is 8081, primary config uses 8080).
	pa := module.NewPortAllocator(8082)
	pa.ExcludePort(8080, "primary-config")
	pa.ExcludePort(8081, "admin-server")
	rm.SetPortAllocator(pa)

	// Create runtime handler
	runtimeHandler := module.NewRuntimeHandler(rm)
	app.runtimeMux = runtimeHandler

	if *loadWorkflows != "" {
		// Parse comma-separated paths
		paths := strings.Split(*loadWorkflows, ",")
		for i := range paths {
			paths[i] = strings.TrimSpace(paths[i])
		}

		if loadErr := rm.LoadFromPaths(context.Background(), paths); loadErr != nil {
			logger.Warn("Some workflows failed to load", "error", loadErr)
		}
	}

	return nil
}

// registerPostStartServices registers all post-start service handlers with
// the current engine's Application. This is called after engine.Start and
// after each engine reload. The handlers themselves persist across reloads;
// only the Application's service registry is re-populated.
func (app *serverApp) registerPostStartServices(logger *slog.Logger) error {
	engine := app.engine

	// Wire EventRecorder adapter to the pipeline handler so pipeline
	// executions emit events to the event store.
	if app.eventStore != nil && app.pipelineHandler != nil {
		recorder := evstore.NewEventRecorderAdapter(app.eventStore)
		app.pipelineHandler.SetEventRecorder(recorder)
		logger.Info("Wired EventRecorder to PipelineWorkflowHandler")
	}

	// Register V1 handler
	if app.v1Handler != nil {
		engine.GetApp().RegisterModule(module.NewServiceModule("admin-v1-mgmt", app.v1Handler))
		if err := engine.GetApp().RegisterService("admin-v1-mgmt", app.v1Handler); err != nil {
			logger.Warn("Failed to register v1 service", "error", err)
		}
	}

	// Register all delegate service modules with the new Application
	delegateServices := map[string]http.Handler{
		"admin-timeline-mgmt":   app.timelineMux,
		"admin-replay-mgmt":     app.replayMux,
		"admin-backfill-mgmt":   app.backfillMux,
		"admin-dlq-mgmt":        app.dlqMux,
		"admin-billing-mgmt":    app.billingMux,
		"admin-native-plugins":  app.nativeHandler,
		"admin-env-mgmt":        app.envMux,
		"admin-cloud-providers": app.cloudMux,
		"admin-plugin-registry": app.pluginRegMux,
		"admin-ingest-mgmt":     app.ingestMux,
		"admin-runtime-mgmt":    app.runtimeMux,
	}
	for name, handler := range delegateServices {
		if handler == nil {
			continue
		}
		engine.GetApp().RegisterModule(module.NewServiceModule(name, handler))
		if regErr := engine.GetApp().RegisterService(name, handler); regErr != nil {
			logger.Warn("Failed to register service", "name", name, "error", regErr)
		}
	}

	// Wire execution tracking on CQRS handlers
	if app.executionTracker != nil {
		// Also wire the event store recorder so CQRS pipelines emit events
		// to the event store (used by store browser and timeline features).
		if app.eventStore != nil {
			app.executionTracker.EventStoreRecorder = evstore.NewEventRecorderAdapter(app.eventStore)
			logger.Info("Wired EventStoreRecorder to execution tracker")
		}

		for _, svc := range engine.GetApp().SvcRegistry() {
			switch h := svc.(type) {
			case *module.QueryHandler:
				h.SetExecutionTracker(app.executionTracker)
			case *module.CommandHandler:
				h.SetExecutionTracker(app.executionTracker)
			}
		}
		logger.Info("Wired execution tracker to CQRS handlers", "workflow_id", app.executionTracker.WorkflowID)
	}

	// Resolve delegates that couldn't be resolved during Init
	for _, svc := range engine.GetApp().SvcRegistry() {
		switch h := svc.(type) {
		case *module.QueryHandler:
			h.ResolveDelegatePostStart()
		case *module.CommandHandler:
			h.ResolveDelegatePostStart()
		}
	}

	logger.Info("Registered all post-start services for delegate dispatch")
	return nil
}

// reloadEngine stops the current engine, builds a new one from the given config,
// starts it, and re-registers all persistent services with the new Application.
// This preserves all stores, handlers, and database connections across reloads.
func (app *serverApp) reloadEngine(newCfg *config.WorkflowConfig) error {
	logger := app.logger

	// Stop the current engine
	if stopErr := app.engine.Stop(context.Background()); stopErr != nil {
		logger.Warn("Error stopping engine during reload", "error", stopErr)
	}

	// Build and start a new engine
	newEngine, _, _, newPipelineHandler, buildErr := buildEngine(newCfg, logger)
	if buildErr != nil {
		return fmt.Errorf("failed to rebuild engine: %w", buildErr)
	}

	// Update the serverApp references BEFORE registering services,
	// since registerManagementServices reads app.engine.
	app.engine = newEngine
	app.pipelineHandler = newPipelineHandler

	// Re-register pre-start management services with the new Application
	registerManagementServices(logger, app)

	// Start the new engine
	if startErr := newEngine.Start(context.Background()); startErr != nil {
		return fmt.Errorf("failed to start reloaded engine: %w", startErr)
	}

	// Re-register post-start services (stores already initialized, just need
	// to be re-registered with the new Application's service registry).
	if app.v1Store != nil {
		if regErr := app.registerPostStartServices(logger); regErr != nil {
			return fmt.Errorf("failed to re-register post-start services: %w", regErr)
		}
	}

	logger.Info("Engine reloaded successfully — all services preserved")
	return nil
}

// importBundles imports and deploys workflow bundles specified via --import-bundle.
func (app *serverApp) importBundles(logger *slog.Logger) error {
	if *importBundle == "" {
		return nil
	}

	paths := strings.Split(*importBundle, ",")
	for i := range paths {
		paths[i] = strings.TrimSpace(paths[i])
	}

	for _, bundlePath := range paths {
		if bundlePath == "" {
			continue
		}

		logger.Info("Importing bundle", "path", bundlePath)

		f, err := os.Open(bundlePath)
		if err != nil {
			logger.Error("Failed to open bundle", "path", bundlePath, "error", err)
			continue
		}

		id := uuid.New().String()
		destDir := filepath.Join(*dataDir, "workspaces", id)

		manifest, workflowPath, importErr := bundle.Import(f, destDir)
		f.Close()
		if importErr != nil {
			logger.Error("Failed to import bundle", "path", bundlePath, "error", importErr)
			continue
		}

		// Read the extracted workflow.yaml
		yamlData, err := os.ReadFile(workflowPath) //nolint:gosec // G703: path from trusted bundle extraction
		if err != nil {
			logger.Error("Failed to read workflow.yaml", "path", workflowPath, "error", err)
			continue
		}
		yamlContent := string(yamlData)

		name := manifest.Name
		if name == "" {
			name = filepath.Base(bundlePath)
		}

		// Create a workflow record in the V1Store
		if app.v1Store != nil {
			sysWf, sysErr := app.v1Store.GetSystemWorkflow()
			projectID := ""
			if sysErr == nil && sysWf != nil {
				projectID = sysWf.ProjectID
			}

			wf, createErr := app.v1Store.CreateWorkflow(projectID, name, "", manifest.Description, yamlContent, "system")
			if createErr != nil {
				logger.Error("Failed to create workflow record", "name", name, "error", createErr)
			} else {
				// Update workspace dir on the record
				_, _ = app.v1Store.DB().Exec(
					`UPDATE workflows SET workspace_dir = ? WHERE id = ?`,
					destDir, wf.ID,
				)
				logger.Info("Created workflow record", "id", wf.ID, "name", name)
			}
		}

		// Deploy via RuntimeManager
		if app.runtimeManager != nil {
			if launchErr := app.runtimeManager.LaunchFromWorkspace(context.Background(), id, name, yamlContent, destDir); launchErr != nil {
				logger.Error("Failed to launch bundle workflow", "name", name, "error", launchErr)
				continue
			}
			logger.Info("Deployed bundle workflow", "id", id, "name", name, "dest", destDir)
		} else {
			logger.Warn("No runtime manager available, bundle extracted but not deployed", "name", name, "dest", destDir)
		}
	}

	return nil
}

// run starts the engine and HTTP server, blocking until ctx is canceled.
// It performs graceful shutdown when the context is done.
func run(ctx context.Context, app *serverApp, listenAddr string) error {
	// Start the workflow engine (single-config mode).
	// The engine may start its own HTTP server (via http.server module) on the
	// configured address. The management mux (AI, dynamic components, workflow UI)
	// listens on a separate management port to avoid conflicts.
	if app.engine != nil {
		if err := app.engine.Start(ctx); err != nil {
			return fmt.Errorf("failed to start workflow engine: %w", err)
		}
	}

	// Run post-start hooks (e.g., wiring handlers that depend on started modules)
	for _, fn := range app.postStartFuncs {
		if err := fn(); err != nil {
			return fmt.Errorf("post-start hook failed: %w", err)
		}
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Stop observability reporter (final flush)
	if app.reporter != nil {
		app.reporter.Stop()
		app.logger.Info("Stopped observability reporter")
	}

	if app.runtimeManager != nil {
		if err := app.runtimeManager.StopAll(context.Background()); err != nil {
			app.logger.Error("Runtime manager shutdown error", "error", err)
		}
	}
	if app.engineManager != nil {
		if err := app.engineManager.StopAll(context.Background()); err != nil {
			app.logger.Error("Engine manager shutdown error", "error", err)
		}
	}
	if app.engine != nil {
		if err := app.engine.Stop(context.Background()); err != nil {
			app.logger.Error("Engine shutdown error", "error", err)
		}
	}

	// Close v1 store
	if app.v1Store != nil {
		if err := app.v1Store.Close(); err != nil {
			app.logger.Error("V1 store close error", "error", err)
		}
	}

	// Close event store
	if app.eventStore != nil {
		if err := app.eventStore.Close(); err != nil {
			app.logger.Error("Event store close error", "error", err)
		}
	}

	// Close idempotency DB
	if app.idempotencyDB != nil {
		if err := app.idempotencyDB.Close(); err != nil {
			app.logger.Error("Idempotency DB close error", "error", err)
		}
	}

	// Close environment store
	if app.envStore != nil {
		if err := app.envStore.Close(); err != nil {
			app.logger.Error("Environment store close error", "error", err)
		}
	}

	// Clean up temp files and directories
	for _, f := range app.cleanupFiles {
		os.Remove(f) //nolint:gosec // G703: cleaning up server-managed temp files
	}
	for _, d := range app.cleanupDirs {
		os.RemoveAll(d) //nolint:gosec // G703: cleaning up server-managed temp dirs
	}

	return nil
}

// envOrFlag returns the environment variable value if set, otherwise the flag value.
func envOrFlag(envKey string, flagVal *string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if flagVal != nil {
		return *flagVal
	}
	return ""
}

// applyEnvOverrides sets flag values from environment variables when the
// corresponding flag was not explicitly provided on the command line.
func applyEnvOverrides() {
	envMap := map[string]string{ //nolint:gosec // G101: env var name mapping, not credentials
		"config":          "WORKFLOW_CONFIG",
		"addr":            "WORKFLOW_ADDR",
		"anthropic-key":   "WORKFLOW_AI_API_KEY",
		"anthropic-model": "WORKFLOW_AI_MODEL",
		"jwt-secret":      "WORKFLOW_JWT_SECRET",
		"data-dir":        "WORKFLOW_DATA_DIR",
		"load-workflows":  "WORKFLOW_LOAD_WORKFLOWS",
		"import-bundle":   "WORKFLOW_IMPORT_BUNDLE",
	}

	// Track which flags were explicitly set on the command line.
	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	for flagName, envKey := range envMap {
		if explicit[flagName] {
			continue
		}
		if v := os.Getenv(envKey); v != "" {
			_ = flag.Set(flagName, v)
		}
	}

	// WORKFLOW_AI_PROVIDER selects the default provider but does not map
	// directly to a single flag. We expose it as an env var for containers
	// and read it in initAIService.

	// WORKFLOW_ENCRYPTION_KEY is consumed directly via os.Getenv where
	// needed (e.g. crypto middleware) and has no flag equivalent.
}

func main() {
	flag.Parse()
	applyEnvOverrides()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

	if *databaseDSN != "" {
		// Multi-workflow mode
		logger.Info("Starting in multi-workflow mode")

		// TODO: Once the api package is implemented, this section will:
		// 1. Connect to PostgreSQL using *databaseDSN
		// 2. Run database migrations
		// 3. Create store instances (UserStore, CompanyStore, ProjectStore, WorkflowStore, etc.)
		// 4. Bootstrap admin user if *adminEmail and *adminPassword are set (first-run)
		// 5. Create WorkflowEngineManager with stores
		// 6. Create api.NewRouter() with stores, *jwtSecret, and engine manager
		// 7. Mount API router at /api/v1/ alongside existing routes

		// For now, log the configuration and fall through to single-config mode
		logger.Info("Multi-workflow mode configured",
			"database_dsn_set", *databaseDSN != "",
			"jwt_secret_set", *jwtSecret != "",
			"admin_email_set", *adminEmail != "",
		)

		// Suppress unused variable warnings until api package is ready
		_ = databaseDSN
		_ = jwtSecret
		_ = adminEmail
		_ = adminPassword

		logger.Warn("Multi-workflow mode requires the api package (not yet available); falling back to single-config mode")
	}

	// Existing single-config behavior
	cfg, err := loadConfig(logger)
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	app, err := setup(logger, cfg)
	if err != nil {
		log.Fatalf("Setup error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Wait for termination signal in a goroutine
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("Shutting down...")
		cancel()
	}()

	fmt.Println("Admin UI on http://localhost:8081")
	if *configFile != "" {
		fmt.Printf("Workflow engine on %s\n", *addr)
	}
	if err := run(ctx, app, *addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	fmt.Println("Shutdown complete")
}

func initAIService(logger *slog.Logger, registry *dynamic.ComponentRegistry, pool *dynamic.InterpreterPool) (*ai.Service, *ai.DeployService) {
	svc := ai.NewService()

	// Anthropic provider
	apiKey := *anthropicKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey != "" {
		client, err := llm.NewClient(llm.ClientConfig{
			APIKey: apiKey,
			Model:  *anthropicModel,
		})
		if err != nil {
			logger.Warn("Failed to create Anthropic client", "error", err)
		} else {
			svc.RegisterGenerator(ai.ProviderAnthropic, client)
			logger.Info("Registered Anthropic AI provider")
		}
	} else {
		logger.Warn("Anthropic provider unavailable: no API key configured")
	}

	// Copilot provider
	if *copilotCLI != "" {
		client, err := copilotai.NewClient(copilotai.ClientConfig{
			CLIPath: *copilotCLI,
			Model:   *copilotModel,
		})
		if err != nil {
			logger.Warn("Failed to create Copilot client", "error", err)
		} else {
			svc.RegisterGenerator(ai.ProviderCopilot, client)
			logger.Info("Registered Copilot AI provider")
		}
	} else {
		logger.Warn("Copilot provider unavailable: no CLI path configured")
	}

	deploy := ai.NewDeployService(svc, registry, pool)
	return svc, deploy
}

// featureDisabledHandler returns an http.Handler that responds with 503
// Service Unavailable and a JSON body explaining which feature is disabled.
// This is used as a stub for delegate services whose backing stores failed to
// initialize, preventing the delegate step from returning a hard 500 error
// ("service not found in registry").
func featureDisabledHandler(reason string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  reason,
			"status": "service_unavailable",
		})
	})
}
