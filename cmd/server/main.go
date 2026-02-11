package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/ai"
	copilotai "github.com/GoCodeAlone/workflow/ai/copilot"
	"github.com/GoCodeAlone/workflow/ai/llm"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

var (
	configFile     = flag.String("config", "", "Path to workflow configuration YAML file")
	addr           = flag.String("addr", ":8080", "HTTP listen address")
	copilotCLI     = flag.String("copilot-cli", "", "Path to Copilot CLI binary")
	copilotModel   = flag.String("copilot-model", "", "Model to use with Copilot SDK")
	anthropicKey   = flag.String("anthropic-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env)")
	anthropicModel = flag.String("anthropic-model", "", "Anthropic model name")

	// Multi-workflow mode flags
	databaseDSN   = flag.String("database-dsn", "", "PostgreSQL connection string for multi-workflow mode")
	jwtSecret     = flag.String("jwt-secret", "", "JWT signing secret for API authentication")
	adminEmail    = flag.String("admin-email", "", "Initial admin user email (first-run bootstrap)")
	adminPassword = flag.String("admin-password", "", "Initial admin user password (first-run bootstrap)")
)

// buildEngine creates the workflow engine with all handlers registered and built from config.
func buildEngine(cfg *config.WorkflowConfig, logger *slog.Logger) (*workflow.StdEngine, *dynamic.Loader, *dynamic.ComponentRegistry, error) {
	app := modular.NewStdApplication(nil, logger)
	engine := workflow.NewStdEngine(app, logger)

	// Register standard workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())

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

	// Build engine from config
	if err := engine.BuildFromConfig(cfg); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to build workflow: %w", err)
	}

	return engine, loader, registry, nil
}

// buildMux creates the HTTP mux with all routes registered.
func buildMux(aiSvc *ai.Service, deploySvc *ai.DeployService, loader *dynamic.Loader, registry *dynamic.ComponentRegistry, uiHandler *module.WorkflowUIHandler) *http.ServeMux {
	mux := http.NewServeMux()
	ai.NewHandler(aiSvc).RegisterRoutes(mux)
	ai.NewDeployHandler(deploySvc).RegisterRoutes(mux)
	dynamic.NewAPIHandler(loader, registry).RegisterRoutes(mux)
	uiHandler.RegisterRoutes(mux)
	return mux
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
	engine        *workflow.StdEngine
	engineManager *workflow.WorkflowEngineManager
	mux           *http.ServeMux
	logger        *slog.Logger
}

// setup initializes all server components: engine, AI services, and HTTP mux.
func setup(logger *slog.Logger, cfg *config.WorkflowConfig) (*serverApp, error) {
	engine, loader, registry, err := buildEngine(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build engine: %w", err)
	}

	app := &serverApp{
		engine: engine,
		logger: logger,
	}

	// Create UI handler with live reload and status callbacks
	uiHandler := module.NewWorkflowUIHandler(cfg)

	uiHandler.SetReloadFunc(func(newCfg *config.WorkflowConfig) error {
		// Stop current engine
		if stopErr := app.engine.Stop(context.Background()); stopErr != nil {
			logger.Warn("Error stopping engine during reload", "error", stopErr)
		}

		// Build new engine from the updated config
		newEngine, _, _, buildErr := buildEngine(newCfg, logger)
		if buildErr != nil {
			return fmt.Errorf("failed to rebuild engine: %w", buildErr)
		}

		// Start new engine
		if startErr := newEngine.Start(context.Background()); startErr != nil {
			return fmt.Errorf("failed to start reloaded engine: %w", startErr)
		}

		app.engine = newEngine
		logger.Info("Engine reloaded successfully")
		return nil
	})

	uiHandler.SetStatusFunc(func() map[string]interface{} {
		return map[string]interface{}{
			"status": "running",
		}
	})

	pool := dynamic.NewInterpreterPool()
	aiSvc, deploySvc := initAIService(logger, registry, pool)
	app.mux = buildMux(aiSvc, deploySvc, loader, registry, uiHandler)

	return app, nil
}

// run starts the engine and HTTP server, blocking until ctx is canceled.
// It performs graceful shutdown when the context is done.
func run(ctx context.Context, app *serverApp, listenAddr string) error {
	// Start the workflow engine (single-config mode)
	if app.engine != nil {
		if err := app.engine.Start(ctx); err != nil {
			return fmt.Errorf("failed to start workflow engine: %w", err)
		}
	}

	server := &http.Server{
		Addr:    listenAddr,
		Handler: app.mux,
	}

	go func() {
		app.logger.Info("Starting server", "addr", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.logger.Error("Server failed", "error", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	if err := server.Shutdown(context.Background()); err != nil {
		app.logger.Error("HTTP server shutdown error", "error", err)
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

	return nil
}

func main() {
	flag.Parse()

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

	fmt.Printf("Workflow server started on %s\n", *addr)
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
