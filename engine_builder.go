package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
)

// DefaultHandlerFactory is a function that returns a slice of default
// WorkflowHandler instances. It is set by the setup package via an init
// function to break the import cycle between the root workflow package
// and the handlers package. Import "github.com/GoCodeAlone/workflow/setup"
// (typically as a blank import) to register the default factories.
var DefaultHandlerFactory func() []WorkflowHandler

// DefaultTriggerFactory is a function that returns a slice of default
// Trigger instances. It is set by the setup package.
var DefaultTriggerFactory func() []interfaces.Trigger

// EngineBuilder provides a fluent API for constructing a fully-configured
// StdEngine. It encapsulates the boilerplate of registering workflow handlers,
// triggers, dynamic components, and plugins so that CLI tools, MCP servers,
// and other consumers can initialise an engine in a few lines.
//
//	engine, err := workflow.NewEngineBuilder().
//	    WithAllDefaults().
//	    Build()
type EngineBuilder struct {
	// Required dependencies — set via constructor or With* helpers.
	app    modular.Application
	logger modular.Logger

	// Accumulator slices for deferred registration.
	workflowHandlers []WorkflowHandler
	triggers         []interfaces.Trigger
	plugins          []plugin.EnginePlugin

	// Feature flags
	useDynamicComponents bool
	useDefaultHandlers   bool
	useDefaultTriggers   bool

	// Optional overrides
	pluginLoader *plugin.PluginLoader
	configPath   string

	// Track if the caller set explicit app/logger so Build can create defaults.
	appSet    bool
	loggerSet bool
}

// NewEngineBuilder creates a new EngineBuilder with no defaults configured.
// Call With* methods to add capabilities, then Build() to create the engine.
func NewEngineBuilder() *EngineBuilder {
	return &EngineBuilder{
		workflowHandlers: make([]WorkflowHandler, 0),
		triggers:         make([]interfaces.Trigger, 0),
		plugins:          make([]plugin.EnginePlugin, 0),
	}
}

// WithApplication sets a custom modular.Application on the builder.
// If not called, Build() creates a default StdApplication.
func (b *EngineBuilder) WithApplication(app modular.Application) *EngineBuilder {
	b.app = app
	b.appSet = true
	return b
}

// WithLogger sets a custom logger on the builder.
// If not called, Build() creates a default slog.Logger writing to stdout.
func (b *EngineBuilder) WithLogger(logger modular.Logger) *EngineBuilder {
	b.logger = logger
	b.loggerSet = true
	return b
}

// WithDefaultHandlers registers all built-in workflow handlers:
// HTTP, Messaging, StateMachine, Scheduler, Integration, Pipeline, Event, Platform.
func (b *EngineBuilder) WithDefaultHandlers() *EngineBuilder {
	b.useDefaultHandlers = true
	return b
}

// WithDefaultTriggers registers all built-in triggers:
// HTTP, Event, Schedule, EventBus, Reconciliation.
func (b *EngineBuilder) WithDefaultTriggers() *EngineBuilder {
	b.useDefaultTriggers = true
	return b
}

// WithDynamicComponents sets up the dynamic interpreter pool, component
// registry, and loader for scripting support.
func (b *EngineBuilder) WithDynamicComponents() *EngineBuilder {
	b.useDynamicComponents = true
	return b
}

// WithAllDefaults is a convenience method that enables default handlers,
// default triggers, and dynamic components.
func (b *EngineBuilder) WithAllDefaults() *EngineBuilder {
	return b.WithDefaultHandlers().WithDefaultTriggers().WithDynamicComponents()
}

// WithHandler adds a custom workflow handler to the engine.
func (b *EngineBuilder) WithHandler(handler WorkflowHandler) *EngineBuilder {
	b.workflowHandlers = append(b.workflowHandlers, handler)
	return b
}

// WithTrigger adds a custom trigger to the engine.
func (b *EngineBuilder) WithTrigger(trigger interfaces.Trigger) *EngineBuilder {
	b.triggers = append(b.triggers, trigger)
	return b
}

// WithPlugin adds a plugin to be loaded during Build().
func (b *EngineBuilder) WithPlugin(p plugin.EnginePlugin) *EngineBuilder {
	b.plugins = append(b.plugins, p)
	return b
}

// WithPlugins adds multiple plugins to be loaded during Build().
func (b *EngineBuilder) WithPlugins(plugins ...plugin.EnginePlugin) *EngineBuilder {
	b.plugins = append(b.plugins, plugins...)
	return b
}

// WithPluginLoader sets a custom plugin loader on the engine.
func (b *EngineBuilder) WithPluginLoader(loader *plugin.PluginLoader) *EngineBuilder {
	b.pluginLoader = loader
	return b
}

// WithConfigPath stores a config file path for use with BuildAndConfigure().
func (b *EngineBuilder) WithConfigPath(path string) *EngineBuilder {
	b.configPath = path
	return b
}

// Build creates a fully-configured StdEngine from the builder's settings.
// It returns an error if any plugin fails to load.
func (b *EngineBuilder) Build() (*StdEngine, error) {
	// Create defaults for app and logger if not set
	if !b.loggerSet || b.logger == nil {
		b.logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if !b.appSet || b.app == nil {
		b.app = modular.NewStdApplication(nil, b.logger)
	}

	engine := NewStdEngine(b.app, b.logger)

	// Set custom plugin loader if provided
	if b.pluginLoader != nil {
		engine.SetPluginLoader(b.pluginLoader)
	}

	// Register default handlers via factory (set in engine_builder_defaults.go)
	if b.useDefaultHandlers && DefaultHandlerFactory != nil {
		for _, h := range DefaultHandlerFactory() {
			engine.RegisterWorkflowHandler(h)
		}
	}

	// Register custom handlers
	for _, handler := range b.workflowHandlers {
		engine.RegisterWorkflowHandler(handler)
	}

	// Register default triggers via factory (set in engine_builder_defaults.go)
	if b.useDefaultTriggers && DefaultTriggerFactory != nil {
		for _, t := range DefaultTriggerFactory() {
			engine.RegisterTrigger(t)
		}
	}

	// Register custom triggers
	for _, trigger := range b.triggers {
		engine.RegisterTrigger(trigger)
	}

	// Set up dynamic components
	if b.useDynamicComponents {
		pool := dynamic.NewInterpreterPool()
		registry := dynamic.NewComponentRegistry()
		loader := dynamic.NewLoader(pool, registry)
		engine.SetDynamicRegistry(registry)
		engine.SetDynamicLoader(loader)
	}

	// Load plugins
	for _, p := range b.plugins {
		if err := engine.LoadPlugin(p); err != nil {
			return nil, fmt.Errorf("failed to load plugin %q: %w", p.Name(), err)
		}
	}

	return engine, nil
}

// BuildFromConfig creates a fully-configured StdEngine and then loads
// and applies the configuration from the given WorkflowConfig.
func (b *EngineBuilder) BuildFromConfig(cfg *config.WorkflowConfig) (*StdEngine, error) {
	engine, err := b.Build()
	if err != nil {
		return nil, err
	}
	if err := engine.BuildFromConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to build from config: %w", err)
	}
	return engine, nil
}

// BuildAndConfigure creates a fully-configured StdEngine and loads
// configuration from the file path set via WithConfigPath(). Returns
// an error if no config path was set or the file cannot be loaded.
func (b *EngineBuilder) BuildAndConfigure() (*StdEngine, error) {
	if b.configPath == "" {
		return nil, fmt.Errorf("no config path set; call WithConfigPath() first")
	}
	cfg, err := config.LoadFromFile(b.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %q: %w", b.configPath, err)
	}
	return b.BuildFromConfig(cfg)
}

// RunUntilSignal is a convenience method for CLI tools that creates the
// engine, loads config, starts it, and blocks until a termination signal
// (SIGINT/SIGTERM) is received. It handles graceful shutdown automatically.
func (b *EngineBuilder) RunUntilSignal(cfg *config.WorkflowConfig) error {
	engine, err := b.BuildFromConfig(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	cancel()
	if err := engine.Stop(context.Background()); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	return nil
}
