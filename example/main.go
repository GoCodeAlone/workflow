package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
)

func main() {
	// Parse command line arguments
	configFile := flag.String("config", "api-server-config.yaml", "Path to workflow configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadFromFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create application with basic logger
	logger := &simpleLogger{}
	app := modular.NewStdApplication(nil, logger)

	// Create workflow engine
	engine := workflow.NewEngine(app)

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	// Build and start the workflows
	if err := engine.BuildFromConfig(cfg); err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	// Create context that can be canceled on signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	fmt.Printf("Workflow started successfully using configuration from %s\n", *configFile)
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	if err := engine.Stop(ctx); err != nil {
		log.Fatalf("Error during shutdown: %v", err)
	}

	fmt.Println("Shutdown complete")
}

// Simple logger implementation
type simpleLogger struct{}

func (l *simpleLogger) Info(msg string, args ...any) {
	log.Printf("[INFO] %s %v\n", msg, args)
}

func (l *simpleLogger) Error(msg string, args ...any) {
	log.Printf("[ERROR] %s %v\n", msg, args)
}

func (l *simpleLogger) Warn(msg string, args ...any) {
	log.Printf("[WARN] %s %v\n", msg, args)
}

func (l *simpleLogger) Debug(msg string, args ...any) {
	log.Printf("[DEBUG] %s %v\n", msg, args)
}
