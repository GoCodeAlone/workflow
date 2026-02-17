package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	logLevel := fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl run [options] <config.yaml>\n\nRun a workflow engine from a config file.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	cfg, err := config.LoadFromFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	app := modular.NewStdApplication(nil, logger)
	engine := workflow.NewStdEngine(app, logger)

	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())

	engine.RegisterTrigger(module.NewHTTPTrigger())
	engine.RegisterTrigger(module.NewEventTrigger())
	engine.RegisterTrigger(module.NewScheduleTrigger())
	engine.RegisterTrigger(module.NewEventBusTrigger())
	engine.RegisterTrigger(module.NewReconciliationTrigger())

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	engine.SetDynamicRegistry(registry)
	engine.SetDynamicLoader(loader)

	if err := engine.BuildFromConfig(cfg); err != nil {
		return fmt.Errorf("failed to build workflow: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	fmt.Println("Workflow engine started. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	cancel()
	if err := engine.Stop(context.Background()); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	fmt.Println("Shutdown complete")
	return nil
}
