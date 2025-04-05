package main

import (
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/handlers"
	"log"
)

func main() {
	// Load configuration
	cfg, err := config.LoadFromFile("multi-workflow-config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create registry and factory
	registry := module.NewRegistry()
	factory := factory.NewFactory(registry)
	factory.RegisterCommonModules()

	// Create workflow engine
	engine := workflow.NewEngine(factory, registry)

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	// Register other workflow handlers as needed

	// Build and start the workflows
	if err := engine.BuildFromConfig(cfg); err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.Start(); err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	// Wait for termination signal
	// ...
}
