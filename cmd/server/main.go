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
	configFile    = flag.String("config", "", "Path to workflow configuration YAML file")
	addr          = flag.String("addr", ":8080", "HTTP listen address")
	copilotCLI    = flag.String("copilot-cli", "", "Path to Copilot CLI binary")
	copilotModel  = flag.String("copilot-model", "", "Model to use with Copilot SDK")
	anthropicKey  = flag.String("anthropic-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env)")
	anthropicModel = flag.String("anthropic-model", "", "Anthropic model name")
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
func buildMux(aiSvc *ai.Service, deploySvc *ai.DeployService, loader *dynamic.Loader, registry *dynamic.ComponentRegistry, cfg *config.WorkflowConfig) *http.ServeMux {
	mux := http.NewServeMux()
	ai.NewHandler(aiSvc).RegisterRoutes(mux)
	ai.NewDeployHandler(deploySvc).RegisterRoutes(mux)
	dynamic.NewAPIHandler(loader, registry).RegisterRoutes(mux)
	module.NewWorkflowUIHandler(cfg).RegisterRoutes(mux)
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
	engine *workflow.StdEngine
	mux    *http.ServeMux
	logger *slog.Logger
}

// setup initializes all server components: engine, AI services, and HTTP mux.
func setup(logger *slog.Logger, cfg *config.WorkflowConfig) (*serverApp, error) {
	engine, loader, registry, err := buildEngine(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build engine: %w", err)
	}

	pool := dynamic.NewInterpreterPool()
	aiSvc, deploySvc := initAIService(logger, registry, pool)
	mux := buildMux(aiSvc, deploySvc, loader, registry, cfg)

	return &serverApp{
		engine: engine,
		mux:    mux,
		logger: logger,
	}, nil
}

// run starts the engine and HTTP server, blocking until ctx is canceled.
// It performs graceful shutdown when the context is done.
func run(ctx context.Context, app *serverApp, listenAddr string) error {
	// Start the workflow engine
	if err := app.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start workflow engine: %w", err)
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
	if err := app.engine.Stop(context.Background()); err != nil {
		app.logger.Error("Engine shutdown error", "error", err)
	}

	return nil
}

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

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
