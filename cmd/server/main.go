package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/ai"
	copilotai "github.com/GoCodeAlone/workflow/ai/copilot"
	"github.com/GoCodeAlone/workflow/ai/llm"
	apihandler "github.com/GoCodeAlone/workflow/api"
	"github.com/GoCodeAlone/workflow/audit"
	"github.com/GoCodeAlone/workflow/billing"
	"github.com/GoCodeAlone/workflow/bundle"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/environment"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/observability"
	"github.com/GoCodeAlone/workflow/observability/tracing"
	"github.com/GoCodeAlone/workflow/plugin"
	_ "github.com/GoCodeAlone/workflow/plugin/admincore"
	_ "github.com/GoCodeAlone/workflow/plugin/docmanager"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
	_ "github.com/GoCodeAlone/workflow/plugin/storebrowser"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	plugindlq "github.com/GoCodeAlone/workflow/plugins/dlq"
	pluginevstore "github.com/GoCodeAlone/workflow/plugins/eventstore"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginlicense "github.com/GoCodeAlone/workflow/plugins/license"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	plugincloud "github.com/GoCodeAlone/workflow/plugins/cloud"
	plugindatastores "github.com/GoCodeAlone/workflow/plugins/datastores"
	plugingitlab "github.com/GoCodeAlone/workflow/plugins/gitlab"
	pluginmarketplace "github.com/GoCodeAlone/workflow/plugins/marketplace"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
	pluginpolicy "github.com/GoCodeAlone/workflow/plugins/policy"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
	plugintimeline "github.com/GoCodeAlone/workflow/plugins/timeline"
	"github.com/GoCodeAlone/workflow/provider"
	_ "github.com/GoCodeAlone/workflow/provider/aws"
	_ "github.com/GoCodeAlone/workflow/provider/azure"
	_ "github.com/GoCodeAlone/workflow/provider/digitalocean"
	_ "github.com/GoCodeAlone/workflow/provider/gcp"
	"github.com/GoCodeAlone/workflow/schema"
	evstore "github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
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
	multiWorkflowAddr = flag.String("multi-workflow-addr", ":8080", "Listen address for the multi-workflow API server")
	databaseDSN       = flag.String("database-dsn", "", "PostgreSQL connection string for multi-workflow mode")
	jwtSecret         = flag.String("jwt-secret", "", "JWT signing secret for API authentication")
	adminEmail        = flag.String("admin-email", "", "Initial admin user email (first-run bootstrap)")
	adminPassword     = flag.String("admin-password", "", "Initial admin user password (first-run bootstrap)")

	// License flags
	licenseKey = flag.String("license-key", "", "License key for the workflow engine (or set WORKFLOW_LICENSE_KEY env var)")

	// v1 API flags
	dataDir       = flag.String("data-dir", "./data", "Directory for SQLite database and persistent data")
	loadWorkflows = flag.String("load-workflows", "", "Comma-separated paths to workflow YAML files or directories to load alongside admin")
	importBundle = flag.String("import-bundle", "", "Comma-separated paths to .tar.gz workflow bundles to import and deploy on startup")
	// Deprecated: admin UI is now served by the external workflow-plugin-admin binary.
	// This flag is accepted for backwards compatibility but has no effect.
	_ = flag.String("admin-ui-dir", "", "Deprecated: admin UI is now served by the external workflow-plugin-admin binary")
)

// defaultEnginePlugins returns the standard set of engine plugins used by all engine instances.
// Centralising the list here avoids duplication between buildEngine and runMultiWorkflow.
func defaultEnginePlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginlicense.New(),
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
		pluginevstore.New(),
		plugintimeline.New(),
		plugindlq.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
		pluginplatform.New(),
		plugincloud.New(),
		plugingitlab.New(),
		plugindatastores.New(),
		pluginpolicy.New(),
		pluginmarketplace.New(),
	}
}

// buildEngine creates the workflow engine with all handlers registered and built from config.
func buildEngine(cfg *config.WorkflowConfig, logger *slog.Logger) (*workflow.StdEngine, *dynamic.Loader, *dynamic.ComponentRegistry, error) {
	app := modular.NewStdApplication(nil, logger)
	engine := workflow.NewStdEngine(app, logger)

	// Load all engine plugins — each registers its module factories, step factories,
	// trigger factories, and workflow handlers via engine.LoadPlugin.
	for _, p := range defaultEnginePlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			log.Fatalf("Failed to load plugin %s: %v", p.Name(), err)
		}
	}

	// Discover and load external plugins from data/plugins/ directory.
	// External plugins run as separate processes communicating over gRPC.
	// Failures are non-fatal — the engine works fine with only builtin plugins.
	extPluginDir := filepath.Join(*dataDir, "plugins")
	extMgr := pluginexternal.NewExternalPluginManager(extPluginDir, log.Default())
	discovered, discoverErr := extMgr.DiscoverPlugins()
	if discoverErr != nil {
		logger.Warn("Failed to discover external plugins", "error", discoverErr)
	}
	if len(discovered) > 0 {
		logger.Info("Discovered external plugins", "count", len(discovered), "plugins", discovered)
		for _, name := range discovered {
			adapter, loadErr := extMgr.LoadPlugin(name)
			if loadErr != nil {
				logger.Warn("Failed to load external plugin", "plugin", name, "error", loadErr)
				continue
			}
			if err := engine.LoadPlugin(adapter); err != nil {
				logger.Warn("Failed to register external plugin", "plugin", name, "error", err)
				continue
			}
			logger.Info("Loaded external plugin", "plugin", name)
		}
	}

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
		return nil, nil, nil, fmt.Errorf("failed to build workflow: %w", err)
	}

	return engine, loader, registry, nil
}

// loadConfig loads a workflow configuration from the configured file path,
// or returns an empty config if no path is set.
// If the config file contains an application-level config (multi-workflow),
// the returned WorkflowConfig will be nil and the ApplicationConfig will be set.
func loadConfig(logger *slog.Logger) (*config.WorkflowConfig, *config.ApplicationConfig, error) {
	if *configFile != "" {
		// Peek at the file to detect whether it is an application config.
		data, err := os.ReadFile(*configFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read configuration file: %w", err)
		}

		if config.IsApplicationConfig(data) {
			logger.Info("Detected multi-workflow application config", "file", *configFile)
			appCfg, err := config.LoadApplicationConfig(*configFile)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load application configuration: %w", err)
			}
			return nil, appCfg, nil
		}

		cfg, err := config.LoadFromFile(*configFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
		}
		return cfg, nil, nil
	}
	logger.Info("No config file specified, using empty workflow config")
	return config.NewEmptyWorkflowConfig(), nil, nil
}

// ---------------------------------------------------------------------------
// Minimal interfaces — only the methods actually called by the server are
// listed here. Holding interfaces instead of concrete pointers keeps
// serverApp decoupled from every module implementation.
// ---------------------------------------------------------------------------

// v1StoreIface is the minimal interface over *module.V1Store used by the server.
type v1StoreIface interface {
	GetSystemWorkflow() (*module.V1Workflow, error)
	ResetSystemWorkflow(configYAML string) error
	EnsureSystemHierarchy(ownerID, adminConfigYAML string) (string, string, string, string, error)
	DB() *sql.DB
	CreateWorkflow(projectID, name, slug, description, configYAML, createdBy string) (*module.V1Workflow, error)
	Close() error
}

// ioCloser wraps any store that only needs to be closed on shutdown.
type ioCloser interface {
	Close() error
}

// closableEventStore is an EventStore that can also be closed on shutdown.
type closableEventStore interface {
	evstore.EventStore
	Close() error
}

// executionTrackerIface is the minimal interface over *module.ExecutionTracker.
type executionTrackerIface interface {
	module.ExecutionTrackerProvider
	SetEventStoreRecorder(r module.EventRecorder)
}

// ExecutionTrackerSetter is implemented by any module that accepts an
// ExecutionTrackerProvider. Using this interface in place of a concrete-type
// switch means the server does not need to be modified when new module types
// require execution tracking.
type ExecutionTrackerSetter interface {
	SetExecutionTracker(module.ExecutionTrackerProvider)
}

// runtimeLifecycle manages the lifecycle of running workflow instances.
type runtimeLifecycle interface {
	StopAll(ctx context.Context) error
	LoadFromPaths(ctx context.Context, paths []string) error
	LaunchFromWorkspace(ctx context.Context, id, name, yamlContent, workspaceDir string) error
}

// observabilityReporter is implemented by the background observability reporter.
type observabilityReporter interface {
	Stop()
}

// ---------------------------------------------------------------------------
// Domain-scoped sub-structs grouping related serverApp fields
// ---------------------------------------------------------------------------

// storeComponents holds all persistent data stores opened at startup.
type storeComponents struct {
	v1Store       v1StoreIface       // v1 API workflow data store
	eventStore    closableEventStore // execution event store
	idempotencyDB *sql.DB            // idempotency store DB connection
	envStore      ioCloser           // environment management store
}

// mgmtComponents holds management HTTP service handlers created at startup
// that survive engine reloads.
type mgmtComponents struct {
	auditLogger *audit.Logger
	mgmtHandler http.Handler // engine config/reload/status
	schemaSvc   http.Handler // module schema browsing
	combinedAI  http.Handler // AI generation + deploy
	dynHandler  http.Handler // dynamic components API
}

// serviceComponents holds post-start service handlers and the pipeline/
// execution subsystem. These are registered with each new Application
// instance after an engine reload.
type serviceComponents struct {
	v1Handler        http.Handler          // V1 API handler (dashboard)
	executionTracker executionTrackerIface // CQRS execution tracking
	runtimeManager   runtimeLifecycle      // filesystem-loaded workflow instances
	reporter         observabilityReporter // background observability reporter
	timelineMux      http.Handler          // timeline handler mux
	replayMux        http.Handler          // replay handler mux
	backfillMux      http.Handler          // backfill/mock/diff handler mux
	dlqMux           http.Handler          // DLQ handler mux
	billingMux       http.Handler          // billing handler mux
	nativeHandler    http.Handler          // native plugin handler
	envMux           http.Handler          // environment management mux
	cloudMux         http.Handler          // cloud providers mux
	pluginRegMux     http.Handler          // plugin registry mux
	runtimeMux       http.Handler          // runtime instances API
	ingestMux        http.Handler          // ingest API for remote workers
}

// serverApp holds all components needed to run the server. Persistent resources
// (stores, handlers, muxes) are stored here so they survive engine reloads.
// Only the engine itself (modules, handlers, triggers) is recreated on reload.
type serverApp struct {
	engine         *workflow.StdEngine
	engineManager  *workflow.WorkflowEngineManager
	pgStore        *evstore.PGStore // multi-workflow mode PG connection
	logger         *slog.Logger
	cleanupDirs    []string       // temp directories to clean up on shutdown
	cleanupFiles   []string       // temp files to clean up on shutdown
	postStartFuncs []func() error // functions to run after engine.Start
	stores         storeComponents
	mgmt           mgmtComponents
	services       serviceComponents
}

// setup initializes all server components: engine, AI services, and HTTP mux.
func setup(logger *slog.Logger, cfg *config.WorkflowConfig) (*serverApp, error) {
	app := &serverApp{
		logger: logger,
	}

	engine, loader, registry, err := buildEngine(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build engine: %w", err)
	}
	app.engine = engine

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
	app.mgmt.auditLogger = audit.NewLogger(os.Stdout)
	app.mgmt.auditLogger.LogConfigChange(context.Background(), "system", "server", "server started")

	return app, nil
}

// setupFromAppConfig initializes all server components from a multi-workflow
// application config. It merges all workflow files into a combined WorkflowConfig,
// applies the admin config overlay, then builds the engine using
// BuildFromApplicationConfig so cross-workflow pipeline calls are wired up.
func setupFromAppConfig(logger *slog.Logger, appCfg *config.ApplicationConfig) (*serverApp, error) {
	// Merge all workflow files into a combined config.
	combined, err := config.MergeApplicationConfig(appCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to merge application config: %w", err)
	}

	engine, loader, registry, err := buildEngine(combined, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build engine: %w", err)
	}

	sApp := &serverApp{
		engine: engine,
		logger: logger,
	}

	pool := dynamic.NewInterpreterPool()
	aiSvc, deploySvc := initAIService(logger, registry, pool)
	initManagementHandlers(logger, engine, combined, sApp, aiSvc, deploySvc, loader, registry)
	registerManagementServices(logger, sApp)

	sApp.postStartFuncs = append(sApp.postStartFuncs, func() error {
		if err := sApp.initStores(logger); err != nil {
			return err
		}
		return sApp.registerPostStartServices(logger)
	}, func() error {
		return sApp.importBundles(logger)
	})

	sApp.mgmt.auditLogger = audit.NewLogger(os.Stdout)
	sApp.mgmt.auditLogger.LogConfigChange(context.Background(), "system", "server",
		"server started with application config: "+appCfg.Application.Name)

	return sApp, nil
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
		return app.engine.GetApp().SvcRegistry()
	})
	app.mgmt.mgmtHandler = mgmtHandler

	// AI handlers (combined into a single http.Handler)
	aiH := ai.NewHandler(aiSvc)
	deployH := ai.NewDeployHandler(deploySvc)
	app.mgmt.combinedAI = ai.NewCombinedHandler(aiH, deployH)

	// Dynamic components handler
	app.mgmt.dynHandler = dynamic.NewAPIHandler(loader, registry)

	// Schema handler
	app.mgmt.schemaSvc = schema.NewSchemaService()
}

// registerManagementServices registers the pre-start management service handlers
// with the current engine's Application. This is called at startup and again
// after each engine reload. It is idempotent — the new Application starts with
// an empty service registry, so there are no duplicate registration errors.
func registerManagementServices(logger *slog.Logger, app *serverApp) {
	engine := app.engine

	// Register service modules — these are resolved by delegate config in admin/config.yaml
	svcModules := map[string]any{
		"admin-engine-mgmt":    app.mgmt.mgmtHandler,
		"admin-schema-mgmt":    app.mgmt.schemaSvc,
		"admin-ai-mgmt":        app.mgmt.combinedAI,
		"admin-component-mgmt": app.mgmt.dynHandler,
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
		if gen, ok := svc.(interfaces.SchemaRegistrar); ok {
			gen.RegisterAdminSchemas()
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
		if provider, ok := svc.(interfaces.WorkflowStoreProvider); ok {
			if s, ok := provider.WorkflowStore().(*module.V1Store); ok {
				store = s
				logger.Info("Using WorkflowRegistry store", "module", provider.Name())
				break
			}
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
	app.stores.v1Store = store

	// Ensure the system hierarchy exists (Company -> Org -> Project -> Workflow).
	// This is idempotent — if it already exists, it returns the existing IDs.
	// The admin plugin (external binary) manages its own workflow config.
	if _, _, _, _, ensureErr := store.EnsureSystemHierarchy("system", ""); ensureErr != nil {
		logger.Warn("Failed to ensure system hierarchy", "error", ensureErr)
	} else {
		logger.Info("System hierarchy ready")
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
	app.services.v1Handler = v1Handler

	// -----------------------------------------------------------------------
	// Event store, idempotency store
	// -----------------------------------------------------------------------

	// Try to discover the event store from the service registry (registered
	// by an eventstore.service module declared in config). Fall back to
	// creating one directly if no module was configured.
	var eventStore *evstore.SQLiteEventStore
	for _, svc := range engine.GetApp().SvcRegistry() {
		if es, ok := svc.(*evstore.SQLiteEventStore); ok {
			eventStore = es
			logger.Info("Discovered event store from service registry")
			break
		}
	}
	if eventStore == nil {
		eventsDBPath := filepath.Join(*dataDir, "events.db")
		var esErr error
		eventStore, esErr = evstore.NewSQLiteEventStore(eventsDBPath)
		if esErr != nil {
			logger.Warn("Failed to create event store — timeline/replay/diff features disabled", "error", esErr)
		} else {
			logger.Info("Opened event store (fallback)", "path", eventsDBPath)
		}
	}
	if eventStore != nil {
		app.stores.eventStore = eventStore
	}

	// Create SQLite idempotency store (separate DB connection, same data dir)
	idempotencyDBPath := filepath.Join(*dataDir, "idempotency.db")
	idempotencyDSN := idempotencyDBPath + "?_journal_mode=WAL&_busy_timeout=5000"
	idempotencyDB, err := sql.Open("sqlite", idempotencyDSN)
	if err != nil {
		logger.Warn("Failed to open idempotency DB", "error", err)
	} else {
		idempotencyDB.SetMaxOpenConns(1)
		app.stores.idempotencyDB = idempotencyDB
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

	// Discover timeline/replay/backfill mux services registered by ProvidesServices
	// (registered by a timeline.service module). Fall back to direct creation.
	timelineDiscovered := false
	var (
		discoveredTimelineMux http.Handler
		discoveredReplayMux   http.Handler
		discoveredBackfillMux http.Handler
	)
	for svcName, svc := range engine.GetApp().SvcRegistry() {
		if strings.HasSuffix(svcName, ".timeline") {
			if h, ok := svc.(http.Handler); ok {
				discoveredTimelineMux = h
				logger.Info("Discovered timeline mux from service registry", "service", svcName)
			}
		}
		if strings.HasSuffix(svcName, ".replay") {
			if h, ok := svc.(http.Handler); ok {
				discoveredReplayMux = h
			}
		}
		if strings.HasSuffix(svcName, ".backfill") {
			if h, ok := svc.(http.Handler); ok {
				discoveredBackfillMux = h
			}
		}
	}
	if discoveredTimelineMux != nil && discoveredReplayMux != nil && discoveredBackfillMux != nil {
		app.services.timelineMux = discoveredTimelineMux
		app.services.replayMux = discoveredReplayMux
		app.services.backfillMux = discoveredBackfillMux
		timelineDiscovered = true
		logger.Info("Discovered timeline, replay, and backfill muxes from service registry")
	}
	if !timelineDiscovered {
		if eventStore != nil {
			timelineHandler := evstore.NewTimelineHandler(eventStore, logger)
			timelineMux := http.NewServeMux()
			timelineHandler.RegisterRoutes(timelineMux)
			app.services.timelineMux = timelineMux

			replayHandler := evstore.NewReplayHandler(eventStore, logger)
			replayMux := http.NewServeMux()
			replayHandler.RegisterRoutes(replayMux)
			app.services.replayMux = replayMux

			backfillStore := evstore.NewInMemoryBackfillStore()
			mockStore := evstore.NewInMemoryStepMockStore()
			diffCalc := evstore.NewDiffCalculator(eventStore)
			bmdHandler := evstore.NewBackfillMockDiffHandler(backfillStore, mockStore, diffCalc, logger)
			bmdMux := http.NewServeMux()
			bmdHandler.RegisterRoutes(bmdMux)
			app.services.backfillMux = bmdMux

			logger.Info("Created timeline, replay, and backfill/mock/diff handlers (fallback)")
		} else {
			stubMsg := "event store unavailable — timeline/replay/backfill features disabled"
			app.services.timelineMux = featureDisabledHandler(stubMsg)
			app.services.replayMux = featureDisabledHandler(stubMsg)
			app.services.backfillMux = featureDisabledHandler(stubMsg)
			logger.Info("Created stub handlers for timeline/replay/backfill (event store unavailable)")
		}
	}

	// -----------------------------------------------------------------------
	// DLQ handler
	// -----------------------------------------------------------------------

	// Discover DLQ mux and store from the service registry (registered by a
	// dlq.service module). Fall back to direct creation.
	dlqDiscovered := false
	var dlqStore evstore.DLQStore
	var discoveredDLQMux http.Handler
	for svcName, svc := range engine.GetApp().SvcRegistry() {
		if strings.HasSuffix(svcName, ".store") {
			if ds, ok := svc.(*evstore.InMemoryDLQStore); ok {
				dlqStore = ds
			}
		}
		if strings.HasSuffix(svcName, ".admin") {
			if h, ok := svc.(http.Handler); ok && strings.Contains(svcName, "dlq") {
				discoveredDLQMux = h
				logger.Info("Discovered DLQ mux from service registry", "service", svcName)
			}
		}
	}
	if discoveredDLQMux != nil && dlqStore != nil {
		app.services.dlqMux = discoveredDLQMux
		dlqDiscovered = true
		logger.Info("Discovered DLQ service from service registry")
	}
	if !dlqDiscovered {
		inMemDLQStore := evstore.NewInMemoryDLQStore()
		dlqStore = inMemDLQStore
		dlqHandler := evstore.NewDLQHandler(inMemDLQStore, logger)
		dlqMux := http.NewServeMux()
		dlqHandler.RegisterRoutes(dlqMux)
		app.services.dlqMux = dlqMux
		logger.Info("Created DLQ handler (fallback)")
	}

	// -----------------------------------------------------------------------
	// Billing handler
	// -----------------------------------------------------------------------

	billingMeter := billing.NewInMemoryMeter()
	var billingProvider billing.BillingProvider
	if stripeKey := os.Getenv("STRIPE_API_KEY"); stripeKey != "" {
		webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
		billingProvider = billing.NewStripeProvider(stripeKey, webhookSecret, billing.StripePlanIDs{})
		logger.Info("Billing: using Stripe provider")
	} else {
		logger.Warn("STRIPE_API_KEY not set — billing is using MockBillingProvider; set STRIPE_API_KEY to enable real billing")
		billingProvider = billing.NewMockBillingProvider()
	}
	billingHandler := billing.NewHandler(billingMeter, billingProvider)
	billingMux := http.NewServeMux()
	billingHandler.RegisterRoutes(billingMux)
	app.services.billingMux = billingMux

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

	// Auto-register all loaded EnginePlugins that have UIPages as NativePlugins.
	// This eliminates the need for duplicate per-plugin registration.
	for _, ep := range engine.LoadedPlugins() {
		if pages := ep.UIPages(); len(pages) > 0 {
			if err := pluginMgr.Register(ep); err != nil {
				logger.Debug("EnginePlugin not registered as NativePlugin (may already exist)", "plugin", ep.Name(), "error", err)
			}
		}
	}

	// Register NativePlugins contributed via the NativePluginProvider interface.
	// This allows EnginePlugins to contribute dependency-requiring NativePlugins
	// (e.g., store-browser needs DB, doc-manager needs DB).
	nativeCtx := plugin.PluginContext{
		DB:     pluginDB,
		Logger: logger,
	}
	for _, ep := range engine.LoadedPlugins() {
		if npp, ok := ep.(plugin.NativePluginProvider); ok {
			for _, np := range npp.NativePlugins(nativeCtx) {
				if err := pluginMgr.Register(np); err != nil {
					logger.Debug("NativePlugin from provider not registered", "plugin", np.Name(), "error", err)
				}
			}
		}
	}

	// Register standalone NativePlugins from the plugin registry.
	// These are non-EnginePlugin NativePlugins (store-browser, doc-manager,
	// cloud providers) that contribute UI pages and HTTP routes.
	builtinDeps := map[string]any{
		"eventStore": eventStore,
		"dlqStore":   dlqStore,
	}
	for _, np := range plugin.BuiltinNativePlugins(pluginDB, builtinDeps) {
		if err := pluginMgr.Register(np); err != nil {
			logger.Debug("Builtin NativePlugin not registered", "plugin", np.Name(), "error", err)
		}
	}

	// Deploy executor with strategy registry — discover cloud providers from
	// the PluginManager rather than constructing them explicitly.
	strategyReg := deploy.NewStrategyRegistry(logger)
	deployExecutor := deploy.NewExecutor(strategyReg)
	cloudMux := http.NewServeMux()
	for _, np := range pluginMgr.EnabledPlugins() {
		if cp, ok := np.(provider.CloudProvider); ok {
			deployExecutor.RegisterProvider(cp.Name(), cp)
			cp.RegisterRoutes(cloudMux)
		}
	}
	app.services.cloudMux = cloudMux

	// Enable all registered plugins so their routes are active
	allPlugins := pluginMgr.AllPlugins()
	for i := range allPlugins {
		if !allPlugins[i].Enabled {
			if err := pluginMgr.Enable(allPlugins[i].Name); err != nil {
				logger.Warn("Failed to enable plugin", "plugin", allPlugins[i].Name, "error", err)
			}
		}
	}

	// Re-discover cloud providers now that all are enabled
	cloudMux = http.NewServeMux()
	for _, np := range pluginMgr.EnabledPlugins() {
		if cp, ok := np.(provider.CloudProvider); ok {
			deployExecutor.RegisterProvider(cp.Name(), cp)
			cp.RegisterRoutes(cloudMux)
		}
	}
	app.services.cloudMux = cloudMux

	// Plugin discovery + route handler (backed by PluginManager)
	nativeHandler := plugin.NewNativeHandler(pluginMgr)
	app.services.nativeHandler = nativeHandler

	logger.Info("Registered native plugins", "count", len(pluginMgr.AllPlugins()))

	// -----------------------------------------------------------------------
	// Environment management
	// -----------------------------------------------------------------------

	envDBPath := filepath.Join(*dataDir, "environments.db")
	envStore, envErr := environment.NewSQLiteStore(envDBPath)
	if envErr != nil {
		logger.Warn("Failed to create environment store — environment management disabled", "error", envErr)
		app.services.envMux = featureDisabledHandler("environment store unavailable — environment management disabled")
	} else {
		app.stores.envStore = envStore
		envHandler := environment.NewHandler(envStore)
		envMux := http.NewServeMux()
		envHandler.RegisterRoutes(envMux)
		app.services.envMux = envMux
		logger.Info("Registered environment management service", "path", envDBPath)
	}

	// -----------------------------------------------------------------------
	// External plugin management handler
	// -----------------------------------------------------------------------

	extPluginDir2 := filepath.Join(*dataDir, "plugins")
	extPluginMgr := pluginexternal.NewExternalPluginManager(extPluginDir2, log.Default())
	extPluginHandler := pluginexternal.NewPluginHandler(extPluginMgr)
	extPluginMux := http.NewServeMux()
	extPluginHandler.RegisterRoutes(extPluginMux)

	engine.GetApp().RegisterModule(module.NewServiceModule("admin-external-plugins", extPluginMux))
	if regErr := engine.GetApp().RegisterService("admin-external-plugins", extPluginMux); regErr != nil {
		logger.Warn("Failed to register external plugin service", "error", regErr)
	}
	logger.Info("Registered external plugin management API")

	// -----------------------------------------------------------------------
	// Plugin composite registry
	// -----------------------------------------------------------------------

	pluginLocalReg := plugin.NewLocalRegistry()
	pluginRemoteReg := plugin.NewRemoteRegistry("https://plugins.workflow.dev")
	compositeReg := plugin.NewCompositeRegistry(pluginLocalReg, pluginRemoteReg)
	pluginHandler := plugin.NewRegistryHandler(compositeReg)
	pluginMux := http.NewServeMux()
	pluginHandler.RegisterRoutes(pluginMux)
	app.services.pluginRegMux = pluginMux

	logger.Info("Registered plugin composite registry (local + remote)")

	// -----------------------------------------------------------------------
	// Execution tracker
	// -----------------------------------------------------------------------

	workflowID := ""
	if sysWf, sysErr := store.GetSystemWorkflow(); sysErr == nil && sysWf != nil {
		workflowID = sysWf.ID
	}
	app.services.executionTracker = &module.ExecutionTracker{
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
	app.services.ingestMux = ingestMux
	logger.Info("Registered ingest handler for remote worker observability")

	// -----------------------------------------------------------------------
	// Reporter — if WORKFLOW_ADMIN_URL is set, report to admin server
	// -----------------------------------------------------------------------

	if reporter := observability.ReporterFromEnv(logger); reporter != nil {
		app.services.reporter = reporter
		reporter.Start(context.Background())
		logger.Info("Started observability reporter", "admin_url", os.Getenv("WORKFLOW_ADMIN_URL"))
	}

	// -----------------------------------------------------------------------
	// Runtime manager — load workflows from --load-workflows flag
	// -----------------------------------------------------------------------

	// Always create a RuntimeManager (returns empty list when no workflows loaded)
	runtimeBuilder := func(cfg *config.WorkflowConfig, lg *slog.Logger) (func(context.Context) error, error) {
		eng, _, _, buildErr := buildEngine(cfg, lg)
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
	app.services.runtimeManager = rm
	v1Handler.SetRuntimeManager(rm)

	// Wire up port allocator for auto-port assignment on deployed workflows.
	// Start allocating at 8082 (admin is 8081, primary config uses 8080).
	pa := module.NewPortAllocator(8082)
	pa.ExcludePort(8080, "primary-config")
	pa.ExcludePort(8081, "admin-server")
	rm.SetPortAllocator(pa)

	// Create runtime handler
	runtimeHandler := module.NewRuntimeHandler(rm)
	app.services.runtimeMux = runtimeHandler

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
	// executions emit events to the event store. The handler is discovered
	// via the service registry (registered by the pipelinesteps plugin wiring hook).
	if app.stores.eventStore != nil {
		type eventRecorderSetter interface {
			SetEventRecorder(r module.EventRecorder)
		}
		if svc, ok := engine.GetApp().SvcRegistry()[pluginpipeline.PipelineHandlerServiceName]; ok {
			if ph, ok := svc.(eventRecorderSetter); ok {
				recorder := evstore.NewEventRecorderAdapter(app.stores.eventStore)
				ph.SetEventRecorder(recorder)
				logger.Info("Wired EventRecorder to PipelineWorkflowHandler")
			}
		}
	}

	// Register V1 handler
	if app.services.v1Handler != nil {
		engine.GetApp().RegisterModule(module.NewServiceModule("admin-v1-mgmt", app.services.v1Handler))
		if err := engine.GetApp().RegisterService("admin-v1-mgmt", app.services.v1Handler); err != nil {
			logger.Warn("Failed to register v1 service", "error", err)
		}
	}

	// Register all delegate service modules with the new Application
	delegateServices := map[string]http.Handler{
		"admin-timeline-mgmt":   app.services.timelineMux,
		"admin-replay-mgmt":     app.services.replayMux,
		"admin-backfill-mgmt":   app.services.backfillMux,
		"admin-dlq-mgmt":        app.services.dlqMux,
		"admin-billing-mgmt":    app.services.billingMux,
		"admin-native-plugins":  app.services.nativeHandler,
		"admin-env-mgmt":        app.services.envMux,
		"admin-cloud-providers": app.services.cloudMux,
		"admin-plugin-registry": app.services.pluginRegMux,
		"admin-ingest-mgmt":     app.services.ingestMux,
		"admin-runtime-mgmt":    app.services.runtimeMux,
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

	// Auto-discover FeatureFlagAdmin from the service registry and wire to the
	// V1 API handler. The featureflag.service module (admin-feature-flags in
	// admin/config.yaml) registers its admin adapter under a well-known name.
	type featureFlagSetter interface {
		SetFeatureFlagService(module.FeatureFlagAdmin)
	}
	const featureFlagAdminSvcName = "admin-feature-flags.admin"
	if ffSetter, ok := app.services.v1Handler.(featureFlagSetter); ok {
		svcRegistry := engine.GetApp().SvcRegistry()
		if svc, ok := svcRegistry[featureFlagAdminSvcName]; ok {
			if ffAdmin, ok := svc.(module.FeatureFlagAdmin); ok {
				ffSetter.SetFeatureFlagService(ffAdmin)
				logger.Info("Auto-wired FeatureFlagAdmin to V1 API handler from service registry", "service", featureFlagAdminSvcName)
			} else {
				logger.Warn("Service registered under feature flag admin name does not implement FeatureFlagAdmin", "service", featureFlagAdminSvcName)
			}
		} else {
			logger.Debug("FeatureFlagAdmin service not found in service registry; feature flags disabled", "service", featureFlagAdminSvcName)
		}
	}

	// Wire execution tracking on CQRS handlers
	if app.services.executionTracker != nil {
		// Also wire the event store recorder so CQRS pipelines emit events
		// to the event store (used by store browser and timeline features).
		if app.stores.eventStore != nil {
			app.services.executionTracker.SetEventStoreRecorder(evstore.NewEventRecorderAdapter(app.stores.eventStore))
			logger.Info("Wired EventStoreRecorder to execution tracker")
		}

		for _, svc := range engine.GetApp().SvcRegistry() {
			if h, ok := svc.(ExecutionTrackerSetter); ok {
				h.SetExecutionTracker(app.services.executionTracker)
			}
		}
		logger.Info("Wired execution tracker to CQRS handlers")
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
	newEngine, _, _, buildErr := buildEngine(newCfg, logger)
	if buildErr != nil {
		return fmt.Errorf("failed to rebuild engine: %w", buildErr)
	}

	// Update the serverApp reference BEFORE registering services,
	// since registerManagementServices reads app.engine.
	app.engine = newEngine

	// Re-register pre-start management services with the new Application
	registerManagementServices(logger, app)

	// Start the new engine
	if startErr := newEngine.Start(context.Background()); startErr != nil {
		return fmt.Errorf("failed to start reloaded engine: %w", startErr)
	}

	// Re-register post-start services (stores already initialized, just need
	// to be re-registered with the new Application's service registry).
	if app.stores.v1Store != nil {
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

		// Ensure the extracted workflow.yaml path is within the expected destination directory
		absDestDir, absDestErr := filepath.Abs(destDir)
		if absDestErr != nil {
			logger.Error("Failed to resolve destination directory", "destDir", destDir, "error", absDestErr)
			continue
		}

		absWorkflowPath, absWorkflowErr := filepath.Abs(workflowPath)
		if absWorkflowErr != nil {
			logger.Error("Failed to resolve workflow path", "path", workflowPath, "error", absWorkflowErr)
			continue
		}

		rel, relErr := filepath.Rel(absDestDir, absWorkflowPath)
		if relErr != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			logger.Error("Workflow path is outside destination directory; possible path traversal", "path", absWorkflowPath, "destDir", absDestDir, "error", relErr)
			continue
		}

		// Read the extracted workflow.yaml
		yamlData, err := os.ReadFile(absWorkflowPath) //nolint:gosec // G703: path validated to be within destDir
		if err != nil {
			logger.Error("Failed to read workflow.yaml", "path", absWorkflowPath, "error", err)
			continue
		}
		yamlContent := string(yamlData)

		name := manifest.Name
		if name == "" {
			name = filepath.Base(bundlePath)
		}

		// Create a workflow record in the V1Store
		if app.stores.v1Store != nil {
			sysWf, sysErr := app.stores.v1Store.GetSystemWorkflow()
			projectID := ""
			if sysErr == nil && sysWf != nil {
				projectID = sysWf.ProjectID
			}

			wf, createErr := app.stores.v1Store.CreateWorkflow(projectID, name, "", manifest.Description, yamlContent, "system")
			if createErr != nil {
				logger.Error("Failed to create workflow record", "name", name, "error", createErr)
			} else {
				// Update workspace dir on the record
				_, _ = app.stores.v1Store.DB().Exec(
					`UPDATE workflows SET workspace_dir = ? WHERE id = ?`,
					destDir, wf.ID,
				)
				logger.Info("Created workflow record", "id", wf.ID, "name", name)
			}
		}

		// Deploy via RuntimeManager
		if app.services.runtimeManager != nil {
			if launchErr := app.services.runtimeManager.LaunchFromWorkspace(context.Background(), id, name, yamlContent, destDir); launchErr != nil {
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
	if app.services.reporter != nil {
		app.services.reporter.Stop()
		app.logger.Info("Stopped observability reporter")
	}

	if app.services.runtimeManager != nil {
		if err := app.services.runtimeManager.StopAll(context.Background()); err != nil {
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
	if app.stores.v1Store != nil {
		if err := app.stores.v1Store.Close(); err != nil {
			app.logger.Error("V1 store close error", "error", err)
		}
	}

	// Close event store
	if app.stores.eventStore != nil {
		if err := app.stores.eventStore.Close(); err != nil {
			app.logger.Error("Event store close error", "error", err)
		}
	}

	// Close idempotency DB
	if app.stores.idempotencyDB != nil {
		if err := app.stores.idempotencyDB.Close(); err != nil {
			app.logger.Error("Idempotency DB close error", "error", err)
		}
	}

	// Close environment store
	if app.stores.envStore != nil {
		if err := app.stores.envStore.Close(); err != nil {
			app.logger.Error("Environment store close error", "error", err)
		}
	}

	// Close PG store (multi-workflow mode)
	if app.pgStore != nil {
		app.pgStore.Close()
	}

	// Clean up temp files and directories
	for _, f := range app.cleanupFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) { //nolint:gosec // G703: cleaning up server-managed temp files
			app.logger.Error("Temp file cleanup error", "path", f, "error", err)
		}
	}
	for _, d := range app.cleanupDirs {
		if err := os.RemoveAll(d); err != nil && !os.IsNotExist(err) { //nolint:gosec // G703: cleaning up server-managed temp dirs
			app.logger.Error("Temp directory cleanup error", "path", d, "error", err)
		}
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
		"load-workflows": "WORKFLOW_LOAD_WORKFLOWS",
		"import-bundle":  "WORKFLOW_IMPORT_BUNDLE",
		"license-key":    "WORKFLOW_LICENSE_KEY",
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

	// Propagate --license-key flag to WORKFLOW_LICENSE_KEY so that the
	// license.validator module (and any other component) can read it via os.Getenv.
	if *licenseKey != "" && os.Getenv("WORKFLOW_LICENSE_KEY") == "" {
		_ = os.Setenv("WORKFLOW_LICENSE_KEY", *licenseKey)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

	if *databaseDSN != "" {
		// Multi-workflow mode: delegates to runMultiWorkflow which connects to
		// PostgreSQL, runs migrations, starts the REST API, and blocks until shutdown.
		if err := runMultiWorkflow(logger); err != nil {
			log.Fatalf("Multi-workflow error: %v", err)
		}
		fmt.Println("Shutdown complete")
		return
	}

	// Load configuration — supports both single-workflow and multi-workflow application configs.
	cfg, appCfg, err := loadConfig(logger)
	if err != nil {
		log.Fatalf("Configuration error: %v", err) //nolint:gocritic // exitAfterDefer: intentional, cleanup is best-effort
	}

	var app *serverApp
	if appCfg != nil {
		// Multi-workflow application config: build engine from application config
		app, err = setupFromAppConfig(logger, appCfg)
	} else {
		// Single-workflow config (backward-compatible)
		app, err = setup(logger, cfg)
	}
	if err != nil {
		log.Fatalf("Setup error: %v", err) //nolint:gocritic // exitAfterDefer: intentional, cleanup is best-effort
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

// runMultiWorkflow implements multi-workflow mode: connects to PostgreSQL,
// runs migrations, creates an engine manager, mounts the REST API, and
// optionally seeds an initial workflow from -config.
func runMultiWorkflow(logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Connect to PostgreSQL
	pgCfg := evstore.PGConfig{URL: *databaseDSN}
	pg, err := evstore.NewPGStore(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pg.Close()
	logger.Info("Connected to PostgreSQL")

	// 2. Run database migrations
	migrator := evstore.NewMigrator(pg.Pool())
	if err := migrator.Migrate(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("Database migrations applied")

	// 3. Bootstrap admin user if credentials provided
	var adminUserID uuid.UUID
	if *adminEmail != "" && *adminPassword != "" {
		var err error
		adminUserID, err = bootstrapAdmin(ctx, pg.Users(), *adminEmail, *adminPassword, logger)
		if err != nil {
			return fmt.Errorf("bootstrap admin: %w", err)
		}
	}

	// 4. Create WorkflowEngineManager
	engineBuilder := func(cfg *config.WorkflowConfig, l *slog.Logger) (*workflow.StdEngine, modular.Application, error) {
		app := modular.NewStdApplication(nil, l)
		engine := workflow.NewStdEngine(app, l)
		for _, p := range defaultEnginePlugins() {
			if loadErr := engine.LoadPlugin(p); loadErr != nil {
				return nil, nil, fmt.Errorf("load plugin %s: %w", p.Name(), loadErr)
			}
		}
		if err := engine.BuildFromConfig(cfg); err != nil {
			return nil, nil, fmt.Errorf("build from config: %w", err)
		}
		return engine, app, nil
	}

	mgr := workflow.NewWorkflowEngineManager(
		pg.Workflows(),
		pg.CrossWorkflowLinks(),
		logger,
		engineBuilder,
	)

	// 5. Seed initial workflow from -config if provided
	if *configFile != "" {
		if adminUserID == uuid.Nil {
			logger.Warn("Skipping workflow seed: -admin-email and -admin-password are required for seeding")
		} else if err := seedWorkflow(ctx, pg, *configFile, adminUserID, logger); err != nil {
			logger.Warn("Failed to seed workflow from config", "file", *configFile, "error", err)
		}
	}

	// 6. Create API router
	secret := envOrFlag("JWT_SECRET", jwtSecret)
	if secret == "" {
		secret = "dev-secret-change-me"
		logger.Error("No JWT secret configured — using insecure default; set JWT_SECRET env var or -jwt-secret flag")
	}
	stores := apihandler.Stores{
		Users:       pg.Users(),
		Sessions:    pg.Sessions(),
		Companies:   pg.Companies(),
		Projects:    pg.Projects(),
		Workflows:   pg.Workflows(),
		Memberships: pg.Memberships(),
		Links:       pg.CrossWorkflowLinks(),
		Executions:  pg.Executions(),
		Logs:        pg.Logs(),
		Audit:       pg.Audit(),
		IAM:         pg.IAM(),
	}
	apiCfg := apihandler.Config{
		JWTSecret:  secret,
		JWTIssuer:  "workflow-server",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 7 * 24 * time.Hour,
	}
	apiRouter := apihandler.NewRouter(stores, apiCfg)

	// 7. Set up admin UI and management infrastructure for workflow management
	singleCfg, _, err := loadConfig(logger)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	app, err := setup(logger, singleCfg)
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	app.engineManager = mgr
	app.pgStore = pg

	// 8. Mount API router on the same HTTP mux
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", apiRouter)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"multi-workflow","status":"ok"}`))
	}))

	srv := &http.Server{
		Addr:              *multiWorkflowAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start admin engine (background — handles admin UI on :8081)
	if app.engine != nil {
		if err := app.engine.Start(ctx); err != nil {
			return fmt.Errorf("start admin engine: %w", err)
		}
	}
	for _, fn := range app.postStartFuncs {
		if err := fn(); err != nil {
			logger.Warn("Post-start hook failed", "error", err)
		}
	}

	// Start API server; propagate failures back so we can initiate shutdown.
	srvErrCh := make(chan error, 1)
	go func() {
		logger.Info("Multi-workflow API listening", "addr", *multiWorkflowAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("API server error", "error", err)
			srvErrCh <- err
		}
	}()

	// Build display address: if the host part is empty or 0.0.0.0/::/[::], use "localhost".
	displayAddr := *multiWorkflowAddr
	if host, port, splitErr := net.SplitHostPort(*multiWorkflowAddr); splitErr == nil &&
		(host == "" || host == "0.0.0.0" || host == "::" || host == "[::]") {
		displayAddr = ":" + port
	}
	fmt.Printf("Multi-workflow API on http://localhost%s/api/v1/\n", displayAddr)
	fmt.Println("Admin UI on http://localhost:8081")

	// Wait for termination signal or server failure.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		fmt.Println("Shutting down...")
	case <-srvErrCh:
		logger.Error("API server failed; initiating shutdown")
	}
	cancel()

	// Graceful shutdown
	shutdownCtx := context.Background()
	if err := mgr.StopAll(shutdownCtx); err != nil {
		logger.Error("Engine manager shutdown error", "error", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("API server shutdown error", "error", err)
	}
	if app.engine != nil {
		if err := app.engine.Stop(shutdownCtx); err != nil {
			logger.Error("Admin engine shutdown error", "error", err)
		}
	}

	return nil
}

// bootstrapAdmin creates an admin user if one doesn't already exist.
// It returns the admin user's UUID so callers can associate resources with them.
func bootstrapAdmin(ctx context.Context, users evstore.UserStore, email, password string, logger *slog.Logger) (uuid.UUID, error) {
	existing, err := users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, evstore.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("check existing admin: %w", err)
	}
	if err == nil && existing != nil {
		logger.Info("Admin user already exists", "email", email)
		return existing.ID, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now()
	admin := &evstore.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  "Admin",
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := users.Create(ctx, admin); err != nil {
		return uuid.Nil, fmt.Errorf("create admin user: %w", err)
	}
	logger.Info("Bootstrapped admin user", "email", email)
	return admin.ID, nil
}

// slugify converts a string into a URL-friendly slug: lowercase, ASCII alphanumeric
// characters and hyphens only, with consecutive hyphens collapsed and leading/trailing
// hyphens trimmed.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

// ensureSystemProject finds or creates the "system" company and "default" project
// used to associate seed workflows with the required database entities.
func ensureSystemProject(ctx context.Context, pg *evstore.PGStore, ownerID uuid.UUID) (*evstore.Project, error) {
	const companySlug = "system"
	const projectSlug = "default"

	company, err := pg.Companies().GetBySlug(ctx, companySlug)
	if errors.Is(err, evstore.ErrNotFound) {
		company = &evstore.Company{Name: "System", Slug: companySlug, OwnerID: ownerID}
		if createErr := pg.Companies().Create(ctx, company); createErr != nil {
			if !errors.Is(createErr, evstore.ErrDuplicate) {
				return nil, fmt.Errorf("create system company: %w", createErr)
			}
			// Another process created it concurrently; fetch it.
			if company, err = pg.Companies().GetBySlug(ctx, companySlug); err != nil {
				return nil, fmt.Errorf("get system company: %w", err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("get system company: %w", err)
	}

	project, err := pg.Projects().GetBySlug(ctx, company.ID, projectSlug)
	if errors.Is(err, evstore.ErrNotFound) {
		project = &evstore.Project{CompanyID: company.ID, Name: "Default", Slug: projectSlug}
		if createErr := pg.Projects().Create(ctx, project); createErr != nil {
			if !errors.Is(createErr, evstore.ErrDuplicate) {
				return nil, fmt.Errorf("create default project: %w", createErr)
			}
			if project, err = pg.Projects().GetBySlug(ctx, company.ID, projectSlug); err != nil {
				return nil, fmt.Errorf("get default project: %w", err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("get default project: %w", err)
	}

	return project, nil
}

// seedWorkflow imports a YAML config as the initial workflow into the database.
func seedWorkflow(ctx context.Context, pg *evstore.PGStore, configPath string, adminUserID uuid.UUID, logger *slog.Logger) error {
	// Validate the config is loadable
	if _, err := config.LoadFromFile(configPath); err != nil {
		return fmt.Errorf("load config file: %w", err)
	}

	yamlBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	name := filepath.Base(configPath)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	slug := slugify(name)

	// Check if a workflow with this slug already exists in any project.
	existing, err := pg.Workflows().List(ctx, evstore.WorkflowFilter{})
	if err != nil {
		return fmt.Errorf("list existing workflows: %w", err)
	}
	for _, wf := range existing {
		if wf.Slug == slug {
			logger.Info("Seed workflow already exists", "slug", slug)
			return nil
		}
	}

	project, err := ensureSystemProject(ctx, pg, adminUserID)
	if err != nil {
		return fmt.Errorf("ensure system project: %w", err)
	}

	now := time.Now()
	record := &evstore.WorkflowRecord{
		ID:          uuid.New(),
		ProjectID:   project.ID,
		Name:        name,
		Slug:        slug,
		Description: "Seeded from " + configPath,
		ConfigYAML:  string(yamlBytes),
		Version:     1,
		Status:      evstore.WorkflowStatusDraft,
		CreatedBy:   adminUserID,
		UpdatedBy:   adminUserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := pg.Workflows().Create(ctx, record); err != nil {
		return fmt.Errorf("create seed workflow: %w", err)
	}
	logger.Info("Seeded workflow from config", "name", name, "id", record.ID)
	return nil
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
