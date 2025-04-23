package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	workflowmodule "github.com/GoCodeAlone/workflow/module" // Alias to avoid conflict
)

func main() {
	configFile := "config.yaml" // Path relative to mcp_server directory

	// Setup logger first so we can see detailed output
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))

	// Register default modular tools
	logger.Info("Registering default MCP tools")
	RegisterDefaultTools()

	// Log registered tools for debugging
	tools := globalToolRegistry.ListTools()
	logger.Info("Tool registration complete", "toolCount", len(tools))

	// Log each tool for debugging
	for i, tool := range tools {
		logger.Info("Registered tool",
			"index", i,
			"id", tool.ID,
			"name", tool.Name,
			"category", tool.Category)
	}

	// Load configuration
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", configFile, err)
	}

	// Create application with logger
	app := modular.NewStdApplication(nil, logger)

	// Create workflow engine
	engine := workflow.NewStdEngine(app, logger)

	// Register the standard HTTP workflow handler
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	// Register custom module types for our handlers
	engine.AddModuleType("http.handler", func(name string, cfg map[string]interface{}) modular.Module {
		// Determine which custom handler to create based on the name
		switch name {
		case "rootHandler": // Add case for root handler
			logger.Debug("Loading custom RootHandler module")
			handler := NewRootHandler(name)

			// Pre-initialize handler for benefit of tools
			if err := handler.Init(app); err != nil {
				logger.Error("Failed to initialize RootHandler", "error", err)
			}

			return handler
		case "docsHandler":
			logger.Debug("Loading custom DocsHandler module")
			return NewDocsHandler(name)
		case "generateHandler":
			logger.Debug("Loading custom GenerateHandler module")
			return NewGenerateHandler(name)
		default:
			// Fallback to the workflow library's simple handler if needed, though config specifies ours
			logger.Warn("Unknown custom handler name, falling back to simple handler", "name", name)
			contentType := "application/json"
			if ct, ok := cfg["contentType"].(string); ok {
				contentType = ct
			}
			return workflowmodule.NewSimpleHTTPHandler(name, contentType)
		}
	})

	// Build and start the workflows
	if err = engine.BuildFromConfig(cfg); err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	// Create context that can be canceled on signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	// Create a more detailed startup message with tool information
	fmt.Printf("MCP Server started successfully using configuration from %s\n", configFile)
	fmt.Println("Serving on address specified in config (e.g., :8081)")
	fmt.Println("Endpoints:")
	fmt.Println("  GET /")
	fmt.Println("  POST /")
	fmt.Println("  OPTIONS /")
	fmt.Println("  GET /docs")
	fmt.Println("  POST /generate (Body: {\"module_name\": \"<name>\", \"path\": \"<optional_path>\"})")

	// Generate and display detailed tool information
	toolCount := len(tools)

	fmt.Printf("Registered %d tools for MCP client:\n", toolCount)
	for i, tool := range tools {
		fmt.Printf("  %d. %s (%s): %s\n", i+1, tool.Name, tool.Category, tool.ID)
	}

	// Prepare tool information for the VS Code client
	toolsForClient := make([]map[string]string, 0, len(tools))
	for _, tool := range tools {
		toolsForClient = append(toolsForClient, map[string]string{
			"id":          tool.ID,
			"name":        tool.Name,
			"description": tool.Description,
			"category":    tool.Category,
		})
	}

	// Log this in a format that can be easily examined
	toolsJSON, _ := json.MarshalIndent(toolsForClient, "", "  ")
	fmt.Println("\nTools JSON payload for VS Code client:")
	fmt.Println(string(toolsJSON))

	fmt.Println("\nPress Ctrl+C to stop...")

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down MCP server...")
	if err := engine.Stop(ctx); err != nil {
		log.Fatalf("Error during shutdown: %v", err)
	}

	fmt.Println("Shutdown complete")
}
