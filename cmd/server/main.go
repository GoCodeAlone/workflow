package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/admin"
	"github.com/GoCodeAlone/workflow/ai"
	copilotai "github.com/GoCodeAlone/workflow/ai/copilot"
	"github.com/GoCodeAlone/workflow/ai/llm"
	"github.com/GoCodeAlone/workflow/audit"
	"github.com/GoCodeAlone/workflow/billing"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
	evstore "github.com/GoCodeAlone/workflow/store"
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
	dataDir      = flag.String("data-dir", "./data", "Directory for SQLite database and persistent data")
	restoreAdmin = flag.Bool("restore-admin", false, "Restore admin config to embedded default on startup")
)

// buildEngine creates the workflow engine with all handlers registered and built from config.
func buildEngine(cfg *config.WorkflowConfig, logger *slog.Logger) (*workflow.StdEngine, *dynamic.Loader, *dynamic.ComponentRegistry, *handlers.PipelineWorkflowHandler, error) {
	app := modular.NewStdApplication(nil, logger)
	engine := workflow.NewStdEngine(app, logger)

	// Register standard workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())

	// Register pipeline workflow handler
	pipelineHandler := handlers.NewPipelineWorkflowHandler()
	pipelineHandler.SetStepRegistry(engine.GetStepRegistry())
	pipelineHandler.SetLogger(logger)
	engine.RegisterWorkflowHandler(pipelineHandler)

	// Register built-in pipeline step types
	engine.AddStepType("step.validate", module.NewValidateStepFactory())
	engine.AddStepType("step.transform", module.NewTransformStepFactory())
	engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
	engine.AddStepType("step.publish", module.NewPublishStepFactory())
	engine.AddStepType("step.set", module.NewSetStepFactory())
	engine.AddStepType("step.log", module.NewLogStepFactory())
	engine.AddStepType("step.http_call", module.NewHTTPCallStepFactory())
	engine.AddStepType("step.delegate", module.NewDelegateStepFactory())
	engine.AddStepType("step.request_parse", module.NewRequestParseStepFactory())
	engine.AddStepType("step.db_query", module.NewDBQueryStepFactory())
	engine.AddStepType("step.db_exec", module.NewDBExecStepFactory())
	engine.AddStepType("step.json_response", module.NewJSONResponseStepFactory())
	engine.AddStepType("step.jq", module.NewJQStepFactory())

	// Register standard triggers
	engine.RegisterTrigger(module.NewHTTPTrigger())
	engine.RegisterTrigger(module.NewEventTrigger())
	engine.RegisterTrigger(module.NewScheduleTrigger())
	engine.RegisterTrigger(module.NewEventBusTrigger())

	// Set up dynamic component system
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	engine.SetDynamicRegistry(registry)
	engine.SetDynamicLoader(loader)

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

// serverApp holds the components needed to run the server.
type serverApp struct {
	engine          *workflow.StdEngine
	engineManager   *workflow.WorkflowEngineManager
	pipelineHandler *handlers.PipelineWorkflowHandler
	logger          *slog.Logger
	auditLogger     *audit.Logger
	v1Store         *module.V1Store           // v1 API SQLite store
	eventStore      *evstore.SQLiteEventStore // execution event store
	idempotencyDB   *sql.DB                   // idempotency store DB connection
	cleanupDirs     []string                  // temp directories to clean up on shutdown
	cleanupFiles    []string                  // temp files to clean up on shutdown
	postStartFuncs  []func() error            // functions to run after engine.Start
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

	// Wire all handlers through the engine's admin config modules.
	wireManagementHandler(logger, engine, cfg, app, aiSvc, deploySvc, loader, registry)
	wireV1Handler(logger, engine, app)

	// Initialize audit logger (writes structured JSON to stdout)
	app.auditLogger = audit.NewLogger(os.Stdout)
	app.auditLogger.LogConfigChange(context.Background(), "system", "server", "server started")

	return app, nil
}

// mergeAdminConfig loads the embedded admin config, extracts assets to temp
// locations, and merges admin modules/routes into the primary config.
// If the config already contains admin modules (e.g., the user passed the
// admin config directly), the merge is skipped to avoid duplicates.
func mergeAdminConfig(logger *slog.Logger, cfg *config.WorkflowConfig, app *serverApp) error {
	// Check if the config already contains admin modules
	for _, m := range cfg.Modules {
		if m.Name == "admin-server" {
			logger.Info("Config already contains admin modules, skipping merge")
			return nil
		}
	}

	adminCfg, err := admin.LoadConfig()
	if err != nil {
		return err
	}

	// Extract UI assets to temp directory for static.fileserver
	uiDir, err := admin.WriteUIAssets()
	if err != nil {
		return err
	}
	app.cleanupDirs = append(app.cleanupDirs, uiDir)
	admin.InjectUIRoot(adminCfg, uiDir)

	// Merge admin modules and routes into primary config
	admin.MergeInto(cfg, adminCfg)

	logger.Info("Admin UI enabled",
		"uiDir", uiDir,
	)
	return nil
}

// wireManagementHandler registers admin service objects as ServiceModules
// so that CQRS handlers can resolve them via their delegate config.
func wireManagementHandler(logger *slog.Logger, engine *workflow.StdEngine, cfg *config.WorkflowConfig, app *serverApp, aiSvc *ai.Service, deploySvc *ai.DeployService, loader *dynamic.Loader, registry *dynamic.ComponentRegistry) {
	// Workflow management handler (config, reload, validate, status)
	mgmtHandler := module.NewWorkflowUIHandler(cfg)
	mgmtHandler.SetReloadFunc(func(newCfg *config.WorkflowConfig) error {
		if stopErr := app.engine.Stop(context.Background()); stopErr != nil {
			logger.Warn("Error stopping engine during reload", "error", stopErr)
		}
		newEngine, _, _, newPipelineHandler, buildErr := buildEngine(newCfg, logger)
		if buildErr != nil {
			return fmt.Errorf("failed to rebuild engine: %w", buildErr)
		}
		if startErr := newEngine.Start(context.Background()); startErr != nil {
			return fmt.Errorf("failed to start reloaded engine: %w", startErr)
		}
		app.engine = newEngine
		app.pipelineHandler = newPipelineHandler
		logger.Info("Engine reloaded successfully via admin")
		return nil
	})
	mgmtHandler.SetStatusFunc(func() map[string]any {
		return map[string]any{"status": "running"}
	})
	mgmtHandler.SetServiceRegistry(func() map[string]any {
		return app.engine.App().SvcRegistry()
	})

	// AI handlers (combined into a single http.Handler)
	aiH := ai.NewHandler(aiSvc)
	deployH := ai.NewDeployHandler(deploySvc)
	combinedAI := ai.NewCombinedHandler(aiH, deployH)

	// Dynamic components handler
	dynH := dynamic.NewAPIHandler(loader, registry)

	// Schema handler
	schemaSvc := schema.NewSchemaService()

	// Register service modules — these are resolved by delegate config in admin/config.yaml
	svcModules := map[string]any{
		"admin-engine-mgmt":    mgmtHandler,
		"admin-schema-mgmt":    schemaSvc,
		"admin-ai-mgmt":        combinedAI,
		"admin-component-mgmt": dynH,
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

// wireV1Handler registers a post-start hook that discovers the WorkflowRegistry
// from the engine's service registry and wires the V1APIHandler to the
// admin-v1-queries and admin-v1-commands CQRS modules. This must run after
// engine.Start so that the WorkflowRegistry's V1Store is initialized.
func wireV1Handler(logger *slog.Logger, engine *workflow.StdEngine, app *serverApp) {
	app.postStartFuncs = append(app.postStartFuncs, func() error {
		return wireV1HandlerPostStart(logger, engine, app)
	})
}

// wireV1HandlerPostStart is called after engine.Start. It looks up the
// WorkflowRegistry from the service registry, gets its V1Store, and wires
// the V1APIHandler as fallback on the admin-v1-queries and admin-v1-commands
// CQRS handler modules.
func wireV1HandlerPostStart(logger *slog.Logger, engine *workflow.StdEngine, app *serverApp) error {
	// Resolve JWT secret from flag or env
	secret := envOrFlag("JWT_SECRET", jwtSecret)
	if secret == "" {
		logger.Warn("v1 API handler: no JWT secret configured; auth will fail")
	}

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

	v1Handler := module.NewV1APIHandler(store, secret)
	v1Handler.SetReloadFunc(func(configYAML string) error {
		logger.Info("System workflow deploy requested — engine reload not yet wired for v1 deploy")
		return nil
	})

	// Register V1 handler as a service module so delegate config can resolve it.
	engine.GetApp().RegisterModule(module.NewServiceModule("admin-v1-mgmt", v1Handler))

	// Re-initialize the newly registered module so it's in the service registry.
	// Then resolve delegates on any CQRS handlers that were waiting for it.
	if err := engine.GetApp().RegisterService("admin-v1-mgmt", v1Handler); err != nil {
		logger.Warn("Failed to register v1 service directly", "error", err)
	}

	// -----------------------------------------------------------------------
	// Phase 1-2 stores and handlers: timeline, replay, DLQ, backfill/mock/diff, billing
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
		app.idempotencyDB = idempotencyDB
		idempotencyStore, idErr := evstore.NewSQLiteIdempotencyStore(idempotencyDB)
		if idErr != nil {
			logger.Warn("Failed to create idempotency store", "error", idErr)
		} else {
			logger.Info("Opened idempotency store", "path", idempotencyDBPath)
			_ = idempotencyStore // registered for future pipeline integration
		}
	}

	// Wire EventRecorder adapter to the pipeline handler so pipeline
	// executions emit events to the event store.
	if eventStore != nil && app.pipelineHandler != nil {
		recorder := evstore.NewEventRecorderAdapter(eventStore)
		app.pipelineHandler.SetEventRecorder(recorder)
		logger.Info("Wired EventRecorder to PipelineWorkflowHandler")
	}

	// Register Phase 1-2 service modules for delegate dispatch.
	// Each handler gets an internal ServeMux so the CQRS delegate mechanism
	// (which needs http.Handler) can route requests to the correct method.

	if eventStore != nil {
		// Timeline handler (execution list, timeline, events)
		timelineHandler := evstore.NewTimelineHandler(eventStore, logger)
		timelineMux := http.NewServeMux()
		timelineHandler.RegisterRoutes(timelineMux)
		engine.GetApp().RegisterModule(module.NewServiceModule("admin-timeline-mgmt", timelineMux))
		if regErr := engine.GetApp().RegisterService("admin-timeline-mgmt", timelineMux); regErr != nil {
			logger.Warn("Failed to register timeline service", "error", regErr)
		}

		// Replay handler
		replayHandler := evstore.NewReplayHandler(eventStore, logger)
		replayMux := http.NewServeMux()
		replayHandler.RegisterRoutes(replayMux)
		engine.GetApp().RegisterModule(module.NewServiceModule("admin-replay-mgmt", replayMux))
		if regErr := engine.GetApp().RegisterService("admin-replay-mgmt", replayMux); regErr != nil {
			logger.Warn("Failed to register replay service", "error", regErr)
		}

		// Backfill / Mock / Diff handler
		backfillStore := evstore.NewInMemoryBackfillStore()
		mockStore := evstore.NewInMemoryStepMockStore()
		diffCalc := evstore.NewDiffCalculator(eventStore)
		bmdHandler := evstore.NewBackfillMockDiffHandler(backfillStore, mockStore, diffCalc, logger)
		bmdMux := http.NewServeMux()
		bmdHandler.RegisterRoutes(bmdMux)
		engine.GetApp().RegisterModule(module.NewServiceModule("admin-backfill-mgmt", bmdMux))
		if regErr := engine.GetApp().RegisterService("admin-backfill-mgmt", bmdMux); regErr != nil {
			logger.Warn("Failed to register backfill/mock/diff service", "error", regErr)
		}

		logger.Info("Registered timeline, replay, and backfill/mock/diff services")
	}

	// DLQ handler (in-memory store for now)
	dlqStore := evstore.NewInMemoryDLQStore()
	dlqHandler := evstore.NewDLQHandler(dlqStore, logger)
	dlqMux := http.NewServeMux()
	dlqHandler.RegisterRoutes(dlqMux)
	engine.GetApp().RegisterModule(module.NewServiceModule("admin-dlq-mgmt", dlqMux))
	if regErr := engine.GetApp().RegisterService("admin-dlq-mgmt", dlqMux); regErr != nil {
		logger.Warn("Failed to register DLQ service", "error", regErr)
	}

	// Billing handler (mock provider + in-memory meter for now)
	billingMeter := billing.NewInMemoryMeter()
	billingProvider := billing.NewMockBillingProvider()
	billingHandler := billing.NewHandler(billingMeter, billingProvider)
	billingMux := http.NewServeMux()
	billingHandler.RegisterRoutes(billingMux)
	engine.GetApp().RegisterModule(module.NewServiceModule("admin-billing-mgmt", billingMux))
	if regErr := engine.GetApp().RegisterService("admin-billing-mgmt", billingMux); regErr != nil {
		logger.Warn("Failed to register billing service", "error", regErr)
	}

	logger.Info("Registered DLQ and billing services")

	// Resolve delegates that couldn't be resolved during Init (because v1 wasn't registered yet)
	for _, svc := range engine.GetApp().SvcRegistry() {
		switch h := svc.(type) {
		case *module.QueryHandler:
			h.ResolveDelegatePostStart()
		case *module.CommandHandler:
			h.ResolveDelegatePostStart()
		}
	}

	logger.Info("Registered admin-v1-mgmt service for delegate dispatch")
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

	// Clean up temp files and directories
	for _, f := range app.cleanupFiles {
		os.Remove(f)
	}
	for _, d := range app.cleanupDirs {
		os.RemoveAll(d)
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
	envMap := map[string]string{
		"config":          "WORKFLOW_CONFIG",
		"addr":            "WORKFLOW_ADDR",
		"anthropic-key":   "WORKFLOW_AI_API_KEY",
		"anthropic-model": "WORKFLOW_AI_MODEL",
		"jwt-secret":      "WORKFLOW_JWT_SECRET",
		"data-dir":        "WORKFLOW_DATA_DIR",
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
