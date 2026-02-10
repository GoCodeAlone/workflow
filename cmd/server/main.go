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

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

	// Load workflow config
	var cfg *config.WorkflowConfig
	if *configFile != "" {
		var err error
		cfg, err = config.LoadFromFile(*configFile)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v", err)
		}
	} else {
		cfg = config.NewEmptyWorkflowConfig()
		logger.Info("No config file specified, using empty workflow config")
	}

	// Create modular application and workflow engine
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
		log.Fatalf("Failed to build workflow: %v", err)
	}

	// Initialize AI service
	aiSvc, deploySvc := initAIService(logger, registry, pool)

	// Create HTTP mux and register all handlers
	mux := http.NewServeMux()

	// AI handlers
	ai.NewHandler(aiSvc).RegisterRoutes(mux)
	ai.NewDeployHandler(deploySvc).RegisterRoutes(mux)

	// TODO: UI api.ts calls /api/dynamic/components but Go handler registers at /api/components
	// Dynamic component API
	dynamic.NewAPIHandler(loader, registry).RegisterRoutes(mux)

	// Workflow UI (static file catch-all MUST be last)
	module.NewWorkflowUIHandler(cfg).RegisterRoutes(mux)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the workflow engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start workflow engine: %v", err)
	}

	// Start HTTP server
	server := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Starting server", "addr", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	fmt.Printf("Workflow server started on %s\n", *addr)

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	cancel()

	if err := server.Shutdown(context.Background()); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		log.Printf("Engine shutdown error: %v", err)
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
