package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
)

func main() {
	// Parse command line arguments
	configFile := flag.String("config", "", "Path to workflow configuration file")
	flag.Parse()

	// If no configuration specified, provide a selection menu
	if *configFile == "" {
		selectedConfig, err := selectWorkflowConfig()
		if err != nil {
			log.Fatalf("Failed to select workflow configuration: %v", err)
		}
		*configFile = selectedConfig
	}

	// Load configuration
	cfg, err := config.LoadFromFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create application with basic logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	app := modular.NewStdApplication(nil, logger)

	// Create workflow engine
	engine := workflow.NewStdEngine(app, logger)

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())

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

// selectWorkflowConfig presents a list of available workflow configurations and returns the selected one
func selectWorkflowConfig() (string, error) {
	// Find all YAML and YML files in the current directory
	yamlFiles, err := findWorkflowConfigs()
	if err != nil {
		return "", fmt.Errorf("failed to find workflow configurations: %v", err)
	}

	if len(yamlFiles) == 0 {
		return "", fmt.Errorf("no workflow configurations found")
	}

	// Print the menu of available configurations
	fmt.Println("Available workflow configurations:")
	fmt.Println("----------------------------------")
	for i, file := range yamlFiles {
		// Get just the filename without path
		baseFile := filepath.Base(file)
		fmt.Printf("%d. %s\n", i+1, baseFile)
	}
	fmt.Println("----------------------------------")
	fmt.Print("Select a configuration (enter number): ")

	// Read user input
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %v", err)
	}

	// Trim whitespace and convert to integer
	input = strings.TrimSpace(input)
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(yamlFiles) {
		return "", fmt.Errorf("invalid selection: %s", input)
	}

	// Return the selected configuration file
	selectedConfig := yamlFiles[selection-1]
	fmt.Printf("Selected configuration: %s\n", selectedConfig)
	return selectedConfig, nil
}

// findWorkflowConfigs finds all YAML and YML files in the example directory
func findWorkflowConfigs() ([]string, error) {
	var configs []string

	// Get the current directory
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %v", err)
	}

	// Check if we're in the example directory
	dirName := filepath.Base(currentDir)
	searchDir := currentDir
	if dirName != "example" {
		// If not in example dir, see if there's an example subdirectory
		exampleDir := filepath.Join(currentDir, "example")
		if _, err := os.Stat(exampleDir); err == nil {
			searchDir = exampleDir
		}
	}

	// Find all YAML files
	yamlFiles, err := filepath.Glob(filepath.Join(searchDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	configs = append(configs, yamlFiles...)

	// Find all YML files
	ymlFiles, err := filepath.Glob(filepath.Join(searchDir, "*.yml"))
	if err != nil {
		return nil, err
	}
	configs = append(configs, ymlFiles...)

	// Skip README and other non-config files
	var filteredConfigs []string
	for _, cfg := range configs {
		baseName := strings.ToLower(filepath.Base(cfg))
		if strings.Contains(baseName, "readme") || strings.Contains(baseName, "license") {
			continue
		}
		filteredConfigs = append(filteredConfigs, cfg)
	}

	return filteredConfigs, nil
}
