package workflow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/infra"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/GoCodeAlone/workflow/validation"
	"gopkg.in/yaml.v3"
)

// WorkflowHandler interface for handling different workflow types
type WorkflowHandler interface {
	// CanHandle returns true if this handler can process the given workflow type
	CanHandle(workflowType string) bool

	// ConfigureWorkflow sets up the workflow from configuration
	ConfigureWorkflow(app modular.Application, workflowConfig any) error

	// ExecuteWorkflow executes a workflow with the given action and input data
	ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error)
}

// PipelineAdder is implemented by workflow handlers that can receive named pipelines.
// This allows the engine to add pipelines without importing the handlers package.
type PipelineAdder interface {
	AddPipeline(name string, p interfaces.PipelineRunner)
}

// ModuleFactory is a function that creates a module from a name and configuration
type ModuleFactory func(name string, config map[string]any) modular.Module

// StartStopModule extends the basic Module interface with lifecycle methods
type StartStopModule interface {
	modular.Module
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// StdEngine represents the workflow execution engine
type StdEngine struct {
	app              modular.Application
	workflowHandlers []WorkflowHandler
	enginePlugins    []plugin.EnginePlugin
	pluginLoader     *plugin.PluginLoader
	moduleFactories  map[string]ModuleFactory
	logger           modular.Logger
	modules          []modular.Module
	triggers         []interfaces.Trigger
	triggerRegistry  interfaces.TriggerRegistrar
	dynamicRegistry  *dynamic.ComponentRegistry
	dynamicLoader    *dynamic.Loader
	eventEmitter     interfaces.EventEmitter
	secretsResolver  *secrets.MultiResolver
	// stepRegistry holds the pipeline step registry. The field is typed as
	// interfaces.StepRegistrar so the engine depends on the abstraction rather
	// than the concrete *module.StepRegistry. Registration (AddStepType and
	// registerPluginSteps) still calls the concrete type via type assertion.
	stepRegistry    interfaces.StepRegistrar
	pluginInstaller *plugin.PluginInstaller
	configDir       string // directory of the config file, for resolving relative paths

	// triggerTypeMap maps trigger config type keys (e.g., "http", "schedule")
	// to trigger names (e.g., "trigger.http", "trigger.schedule"). Populated
	// during LoadPlugin() from TriggerFactories() keys.
	triggerTypeMap map[string]string

	// triggerConfigWrappers maps trigger type keys to functions that convert
	// flat pipeline trigger config into the trigger's native format.
	triggerConfigWrappers map[string]plugin.TriggerConfigWrapperFunc

	// pipelineRegistry holds all registered pipelines by name, enabling
	// step.workflow_call to look up sibling pipelines at execution time.
	pipelineRegistry map[string]*module.Pipeline

	// provisioner holds the infrastructure provisioner when an infrastructure
	// block is declared in the config. Nil when no infrastructure is declared.
	provisioner *infra.Provisioner

	// configHash is the SHA-256 hash of the last config built via BuildFromConfig.
	// Format: "sha256:<hex>". Empty until BuildFromConfig is called.
	configHash string
}

// App returns the underlying modular.Application.
func (e *StdEngine) App() modular.Application {
	return e.app
}

// ConfigHash returns the SHA-256 hash of the most recently loaded config.
// Format: "sha256:<hex>". Empty until BuildFromConfig is called.
func (e *StdEngine) ConfigHash() string {
	return e.configHash
}

// SetDynamicRegistry sets the dynamic component registry on the engine
// and propagates it to any loaded plugins that support it.
func (e *StdEngine) SetDynamicRegistry(registry *dynamic.ComponentRegistry) {
	e.dynamicRegistry = registry
	// Propagate to plugins that accept a dynamic registry
	type dynamicRegistrySetter interface {
		SetDynamicRegistry(*dynamic.ComponentRegistry)
	}
	for _, p := range e.enginePlugins {
		if setter, ok := p.(dynamicRegistrySetter); ok {
			setter.SetDynamicRegistry(registry)
		}
	}
}

// SetDynamicLoader sets the dynamic component loader on the engine
// and propagates it to any loaded plugins that support it.
func (e *StdEngine) SetDynamicLoader(loader *dynamic.Loader) {
	e.dynamicLoader = loader
	type dynamicLoaderSetter interface {
		SetDynamicLoader(*dynamic.Loader)
	}
	for _, p := range e.enginePlugins {
		if setter, ok := p.(dynamicLoaderSetter); ok {
			setter.SetDynamicLoader(loader)
		}
	}
}

// SetPluginInstaller sets the plugin installer on the engine, enabling
// auto-installation of required plugins during validateRequirements.
func (e *StdEngine) SetPluginInstaller(installer *plugin.PluginInstaller) {
	e.pluginInstaller = installer
}

// NewStdEngine creates a new workflow engine
func NewStdEngine(app modular.Application, logger modular.Logger) *StdEngine {
	e := &StdEngine{
		app:                   app,
		workflowHandlers:      make([]WorkflowHandler, 0),
		moduleFactories:       make(map[string]ModuleFactory),
		logger:                logger,
		modules:               make([]modular.Module, 0),
		triggers:              make([]interfaces.Trigger, 0),
		triggerRegistry:       newTriggerRegistrar(), // bridge: returns *module.TriggerRegistry
		secretsResolver:       secrets.NewMultiResolver(),
		stepRegistry:          newStepRegistry(), // bridge: returns *module.StepRegistry
		triggerTypeMap:        make(map[string]string),
		triggerConfigWrappers: make(map[string]plugin.TriggerConfigWrapperFunc),
		pipelineRegistry:      make(map[string]*module.Pipeline),
	}
	// Register the step.workflow_call factory with a closure that looks up
	// pipelines from this engine's registry at execution time.
	if r, ok := e.stepRegistry.(*module.StepRegistry); ok {
		r.Register("step.workflow_call", module.NewWorkflowCallStepFactory(
			func(name string) (*module.Pipeline, bool) {
				p, ok := e.pipelineRegistry[name]
				return p, ok
			},
		))
	}
	return e
}

// SecretsResolver returns the engine's multi-provider secrets resolver.
func (e *StdEngine) SecretsResolver() *secrets.MultiResolver {
	return e.secretsResolver
}

// RegisterWorkflowHandler adds a workflow handler to the engine
func (e *StdEngine) RegisterWorkflowHandler(handler WorkflowHandler) {
	e.workflowHandlers = append(e.workflowHandlers, handler)
}

// RegisterTrigger registers a trigger with the engine
func (e *StdEngine) RegisterTrigger(trigger interfaces.Trigger) {
	e.triggers = append(e.triggers, trigger)
	e.triggerRegistry.RegisterTrigger(trigger)
}

// Triggers returns the engine's registered triggers.
func (e *StdEngine) Triggers() []interfaces.Trigger {
	return e.triggers
}

// RegisterTriggerType registers a mapping from a trigger config type key
// (e.g., "reconciliation") to a trigger Name() (e.g., "trigger.reconciliation").
// This is used for triggers registered directly via RegisterTrigger() rather
// than through a plugin's TriggerFactories().
func (e *StdEngine) RegisterTriggerType(triggerType string, triggerName string) {
	e.triggerTypeMap[triggerType] = triggerName
}

// RegisterTriggerConfigWrapper registers a function that converts flat pipeline
// trigger config into the native format for a given trigger type.
func (e *StdEngine) RegisterTriggerConfigWrapper(triggerType string, wrapper plugin.TriggerConfigWrapperFunc) {
	e.triggerConfigWrappers[triggerType] = wrapper
}

// AddModuleType registers a factory function for a module type
func (e *StdEngine) AddModuleType(moduleType string, factory ModuleFactory) {
	e.moduleFactories[moduleType] = factory
}

// AddStepType registers a pipeline step factory for the given step type.
func (e *StdEngine) AddStepType(stepType string, factory module.StepFactory) {
	if r, ok := e.stepRegistry.(*module.StepRegistry); ok {
		r.Register(stepType, factory)
	}
}

// GetStepRegistry returns the engine's pipeline step registry.
func (e *StdEngine) GetStepRegistry() interfaces.StepRegistrar {
	return e.stepRegistry
}

// PluginLoader returns the engine's plugin loader, creating it lazily if needed.
func (e *StdEngine) PluginLoader() *plugin.PluginLoader {
	if e.pluginLoader == nil {
		e.pluginLoader = plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	}
	return e.pluginLoader
}

// SetPluginLoader sets a custom plugin loader on the engine.
func (e *StdEngine) SetPluginLoader(loader *plugin.PluginLoader) {
	e.pluginLoader = loader
}

// LoadPlugin loads an EnginePlugin into the engine.
func (e *StdEngine) LoadPlugin(p plugin.EnginePlugin) error {
	return e.loadPluginInternal(p, false)
}

// LoadPluginWithOverride loads an EnginePlugin into the engine, allowing it to
// override existing module, step, trigger, handler, deploy target, and sidecar
// provider registrations. When a duplicate type is encountered, the new factory
// replaces the previous one and a warning is logged, instead of returning an
// error. This is intended for external plugins that intentionally replace
// built-in defaults (e.g., replacing a mock step with a production
// implementation, or swapping out deploy targets/sidecar providers).
func (e *StdEngine) LoadPluginWithOverride(p plugin.EnginePlugin) error {
	return e.loadPluginInternal(p, true)
}

func (e *StdEngine) loadPluginInternal(p plugin.EnginePlugin, allowOverride bool) error {
	transportSnapshot := module.SnapshotDefaultTransport()
	loader := e.PluginLoader()
	var err error
	if allowOverride {
		err = loader.LoadPluginWithOverride(p)
	} else {
		err = loader.LoadPlugin(p)
	}
	if err != nil {
		return fmt.Errorf("load plugin: %w", err)
	}
	if module.DetectTransportMutation(transportSnapshot) {
		pluginName := p.EngineManifest().Name
		e.logger.Warn(fmt.Sprintf("plugin %q mutated http.DefaultTransport; restoring original to prevent cross-plugin interference", pluginName))
		module.RestoreDefaultTransport(transportSnapshot)
	}
	for typeName, factory := range p.ModuleFactories() {
		pluginFactory := factory
		e.moduleFactories[typeName] = func(name string, cfg map[string]any) modular.Module {
			return pluginFactory(name, cfg)
		}
		schema.RegisterModuleType(typeName)
	}
	for typeName, factory := range p.StepFactories() {
		// Delegate to the bridge helper which type-asserts to module.PipelineStep
		// so that engine.go need not reference that concrete type directly.
		e.registerPluginSteps(typeName, factory)
	}
	// Register triggers from plugin. The factory map key is the trigger
	// config type (e.g., "http", "schedule") used in YAML configs.
	for triggerType, factory := range p.TriggerFactories() {
		// Delegate to the bridge helper; triggers are interfaces.Trigger values
		// (module.Trigger is a type alias for interfaces.Trigger).
		if err := e.registerPluginTrigger(triggerType, factory); err != nil {
			return fmt.Errorf("load plugin: %w", err)
		}
	}

	// Register pipeline trigger config wrappers from plugin (optional interface).
	if provider, ok := p.(plugin.PipelineTriggerConfigProvider); ok {
		for triggerType, wrapper := range provider.PipelineTriggerConfigWrappers() {
			e.triggerConfigWrappers[triggerType] = wrapper
		}
	}
	// Register workflow handlers from plugin.
	for _, factory := range p.WorkflowHandlers() {
		result := factory()
		if handler, ok := result.(WorkflowHandler); ok {
			e.RegisterWorkflowHandler(handler)
		}
	}
	// Inject step registry and logger into the plugin via optional setter
	// interfaces, following the same pattern as SetDynamicRegistry.
	type stepRegistrySetter interface {
		SetStepRegistry(interfaces.StepRegistryProvider)
	}
	if setter, ok := p.(stepRegistrySetter); ok {
		setter.SetStepRegistry(e.stepRegistry)
	}
	// Inject *slog.Logger if the engine's logger is backed by one.
	// Plugins declare SetLogger(*slog.Logger) to receive a structured logger.
	type slogLoggerSetter interface {
		SetLogger(logger *slog.Logger)
	}
	if setter, ok := p.(slogLoggerSetter); ok {
		if sl, ok := e.logger.(*slog.Logger); ok {
			setter.SetLogger(sl)
		}
	}
	e.enginePlugins = append(e.enginePlugins, p)
	return nil
}

// validateRequirements checks declared capabilities and plugin versions.
func (e *StdEngine) validateRequirements(req *config.RequiresConfig) error {
	if e.pluginLoader != nil && len(req.Capabilities) > 0 {
		capReg := e.pluginLoader.CapabilityRegistry()
		var missing []string
		for _, capName := range req.Capabilities {
			if !capReg.HasProvider(capName) {
				missing = append(missing, capName)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("unsatisfied capabilities: %s", strings.Join(missing, ", "))
		}
	}
	if e.pluginLoader != nil && len(req.Plugins) > 0 {
		loaded := make(map[string]string)
		for _, lp := range e.pluginLoader.LoadedPlugins() {
			loaded[lp.EngineManifest().Name] = lp.EngineManifest().Version
		}
		for _, pr := range req.Plugins {
			version, ok := loaded[pr.Name]
			if !ok {
				// Attempt auto-install if an installer is configured
				if e.pluginInstaller != nil {
					installCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					installErr := e.pluginInstaller.Install(installCtx, pr.Name, pr.Version)
					cancel()
					if installErr != nil {
						return fmt.Errorf("required plugin %q is not loaded and auto-install failed: %w", pr.Name, installErr)
					}
					// Plugin files were installed but it still needs to be loaded
					// by the caller; report as not loaded.
					if e.logger != nil {
						e.logger.Info("Plugin %s installed to %s; restart or reload required to activate", pr.Name, e.pluginInstaller.InstallDir())
					}
				}
				return fmt.Errorf("required plugin %q is not loaded", pr.Name)
			}
			if pr.Version != "" {
				compatible, err := plugin.CheckVersion(version, pr.Version)
				if err != nil {
					return fmt.Errorf("plugin %q version check error: %w", pr.Name, err)
				}
				if !compatible {
					return fmt.Errorf("plugin %q version %s does not satisfy constraint %q", pr.Name, version, pr.Version)
				}
			}
		}
	}
	return nil
}

// BuildFromConfig builds a workflow from configuration
func (e *StdEngine) BuildFromConfig(cfg *config.WorkflowConfig) error {
	// Validate configuration before building.
	// Allow empty modules (the engine handles that gracefully) and pass
	// registered custom module factory types so they are not rejected.
	// Workflow and trigger type validation is skipped here because the
	// engine dynamically resolves handlers and triggers at runtime.
	valOpts := []schema.ValidationOption{
		schema.WithAllowEmptyModules(),
		schema.WithSkipWorkflowTypeCheck(),
		schema.WithSkipTriggerTypeCheck(),
	}
	if len(e.moduleFactories) > 0 {
		extra := make([]string, 0, len(e.moduleFactories))
		for t := range e.moduleFactories {
			extra = append(extra, t)
		}
		valOpts = append(valOpts, schema.WithExtraModuleTypes(extra...))
	}
	if err := schema.ValidateConfig(cfg, valOpts...); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Run pipeline template cross-reference validation.
	// Mode is controlled by engine.validation.templateRefs in the config:
	//   "off"   — skip entirely (preserves previous behaviour)
	//   "warn"  — log warnings for suspicious references (default)
	//   "error" — return an error when validation finds problems
	if len(cfg.Pipelines) > 0 {
		mode := "warn" // default
		if cfg.Engine != nil && cfg.Engine.Validation != nil && cfg.Engine.Validation.TemplateRefs != "" {
			mode = cfg.Engine.Validation.TemplateRefs
		}
		if mode != "off" {
			vr := validation.ValidatePipelineTemplateRefs(cfg.Pipelines)
			if vr.HasIssues() {
				allMessages := make([]string, 0, len(vr.Warnings)+len(vr.Errors))
				allMessages = append(allMessages, vr.Warnings...)
				allMessages = append(allMessages, vr.Errors...)
				if mode == "error" {
					return fmt.Errorf("pipeline template validation failed: %s", strings.Join(allMessages, "; "))
				}
				for _, msg := range allMessages {
					e.logger.Warn(msg)
				}
			}
		}
	}

	// Validate plugin requirements if declared
	if cfg.Requires != nil {
		if err := e.validateRequirements(cfg.Requires); err != nil {
			return fmt.Errorf("requirements check failed: %w", err)
		}
	}

	// Provision infrastructure resources declared in the config.
	if cfg.Infrastructure != nil && len(cfg.Infrastructure.Resources) > 0 {
		var infraLogger *slog.Logger
		if sl, ok := e.logger.(*slog.Logger); ok {
			infraLogger = sl
		}
		p := infra.NewProvisioner(infraLogger)
		p.AddProvider(&infra.MemoryProvider{})
		resources := make([]infra.ResourceConfig, len(cfg.Infrastructure.Resources))
		for i, r := range cfg.Infrastructure.Resources {
			resources[i] = infra.ResourceConfig{
				Name: r.Name, Type: r.Type, Provider: r.Provider, Config: r.Config,
			}
		}
		plan, err := p.Plan(infra.InfraConfig{Resources: resources})
		if err != nil {
			return fmt.Errorf("infrastructure plan failed: %w", err)
		}
		if err := p.Apply(context.Background(), plan); err != nil {
			return fmt.Errorf("infrastructure provisioning failed: %w", err)
		}
		e.provisioner = p
	}

	// Store config directory for consistent path resolution in pipeline steps
	e.configDir = cfg.ConfigDir

	// Run plugin config transform hooks BEFORE module registration.
	if e.pluginLoader != nil {
		for _, hook := range e.pluginLoader.ConfigTransformHooks() {
			if err := hook.Hook(cfg); err != nil {
				return fmt.Errorf("config transform hook %q failed: %w", hook.Name, err)
			}
		}
	}

	// Compute config hash after transform hooks so the hash reflects the effective
	// runtime config (hooks may mutate cfg before modules are registered).
	// Reset first so a marshal failure never leaves a stale hash from a previous build.
	e.configHash = ""
	if configBytes, err := yaml.Marshal(cfg); err == nil {
		h := sha256.Sum256(configBytes)
		e.configHash = fmt.Sprintf("sha256:%x", h)
	}

	// Register all modules from config
	for _, modCfg := range cfg.Modules {
		// Expand secret references in all string config values before module instantiation.
		// This replaces ${vault:...}, ${aws-sm:...}, ${env:...}, and ${VAR_NAME} patterns.
		expandConfigStrings(e.secretsResolver, modCfg.Config)

		// Inject config directory for relative path resolution in module factories
		if e.configDir != "" {
			if modCfg.Config == nil {
				modCfg.Config = make(map[string]any)
			}
			modCfg.Config["_config_dir"] = e.configDir
		}

		// Create modules based on type
		var mod modular.Module

		// Look up the module factory from the registry (populated by LoadPlugin)
		factory, exists := e.moduleFactories[modCfg.Type]
		if !exists {
			return fmt.Errorf("unknown module type %q for module %q — ensure the required plugin is loaded", modCfg.Type, modCfg.Name)
		}
		e.logger.Debug("Using factory for module type: " + modCfg.Type)
		mod = factory(modCfg.Name, modCfg.Config)
		if mod == nil {
			return fmt.Errorf("factory for module type %q returned nil for module %q", modCfg.Type, modCfg.Name)
		}

		e.app.RegisterModule(mod)
	}

	// Initialize all modules
	if err := e.app.Init(); err != nil {
		return fmt.Errorf("failed to initialize modules: %w", err)
	}

	// Log loaded services
	for name := range e.app.SvcRegistry() {
		e.logger.Debug("Loaded service: " + name)
	}

	// Initialize the workflow event emitter via bridge (avoids direct module dep).
	e.eventEmitter = newEventEmitter(e.app)

	// Register config section for workflow
	e.app.RegisterConfigSection("workflow", modular.NewStdConfigProvider(cfg))

	// Handle each workflow configuration section
	for workflowType, workflowConfig := range cfg.Workflows {
		handled := false

		// Find a handler for this workflow type
		for _, handler := range e.workflowHandlers {
			if handler.CanHandle(workflowType) {
				if err := handler.ConfigureWorkflow(e.app, workflowConfig); err != nil {
					return fmt.Errorf("failed to configure %s workflow: %w", workflowType, err)
				}
				handled = true
				break
			}
		}

		if !handled {
			return fmt.Errorf("no handler found for workflow type: %s", workflowType)
		}
	}

	// Wire route-level pipelines from HTTP workflow route configs
	if err := e.configureRoutePipelines(cfg); err != nil {
		return fmt.Errorf("failed to configure route pipelines: %w", err)
	}

	// Configure triggers (new section)
	if err := e.configureTriggers(cfg.Triggers); err != nil {
		return fmt.Errorf("failed to configure triggers: %w", err)
	}

	// Configure pipelines (composable step-based workflows)
	if len(cfg.Pipelines) > 0 {
		if err := e.configurePipelines(cfg.Pipelines); err != nil {
			return fmt.Errorf("failed to configure pipelines: %w", err)
		}
	}

	// Run plugin wiring hooks (e.g., wire AuthProviders to AuthMiddleware)
	if e.pluginLoader != nil {
		for _, hook := range e.pluginLoader.WiringHooks() {
			if err := hook.Hook(e.app, cfg); err != nil {
				return fmt.Errorf("wiring hook %q failed: %w", hook.Name, err)
			}
		}
	}

	return nil
}

// BuildFromApplicationConfig loads a multi-workflow application config and builds
// the engine from all referenced workflow configs. Each workflow config file is
// parsed independently, and their modules are merged into the shared module
// registry. Module name conflicts across workflow files produce a clear error.
//
// This is the entry point for the application-level multi-workflow feature:
//
//	application:
//	  name: chat-platform
//	  workflows:
//	    - file: workflows/main-loop.yaml
//	    - file: workflows/queue-assignment.yaml
//
// All pipelines defined across workflow files share a single engine and can
// call each other using step.workflow_call.
func (e *StdEngine) BuildFromApplicationConfig(appCfg *config.ApplicationConfig) error {
	if appCfg == nil {
		return fmt.Errorf("application config is nil")
	}
	if len(appCfg.Application.Workflows) == 0 {
		return fmt.Errorf("application %q has no workflow files defined", appCfg.Application.Name)
	}

	e.logger.Info(fmt.Sprintf("Building application %q from %d workflow files",
		appCfg.Application.Name, len(appCfg.Application.Workflows)))

	// Use the shared MergeApplicationConfig helper (also used by the server's
	// admin config merge step) to load and validate all workflow files.
	combined, err := config.MergeApplicationConfig(appCfg)
	if err != nil {
		return fmt.Errorf("application %q: %w", appCfg.Application.Name, err)
	}

	for _, ref := range appCfg.Application.Workflows {
		wfName := ref.Name
		if wfName == "" {
			base := filepath.Base(ref.File)
			wfName = strings.TrimSuffix(base, filepath.Ext(base))
		}
		e.logger.Info(fmt.Sprintf("Application %q: loaded workflow %q from %q",
			appCfg.Application.Name, wfName, ref.File))
	}

	return e.BuildFromConfig(combined)
}

// Start starts all modules and triggers
func (e *StdEngine) Start(ctx context.Context) error {
	err := e.app.Start()
	if err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	// Start all triggers
	for _, trigger := range e.triggers {
		if err := trigger.Start(ctx); err != nil {
			return fmt.Errorf("failed to start trigger '%s': %w", trigger.Name(), err)
		}
	}

	return nil
}

// Stop stops all modules and triggers
func (e *StdEngine) Stop(ctx context.Context) error {
	var lastErr error

	// Stop all triggers first
	for _, trigger := range e.triggers {
		if err := trigger.Stop(ctx); err != nil {
			lastErr = fmt.Errorf("failed to stop trigger '%s': %w", trigger.Name(), err)
			e.logger.Error(lastErr.Error())
		}
	}

	err := e.app.Stop()
	if err != nil {
		lastErr = fmt.Errorf("failed to stop application: %w", err)
		e.logger.Error(lastErr.Error())
	}

	return lastErr
}

// TriggerWorkflow starts a workflow based on a trigger
func (e *StdEngine) TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error {
	startTime := time.Now()

	// Find the appropriate workflow handler
	for _, handler := range e.workflowHandlers {
		if handler.CanHandle(workflowType) {
			// Log workflow execution
			e.logger.Info(fmt.Sprintf("Triggered workflow '%s' with action '%s'", workflowType, action))

			// Log the data in debug mode
			for k, v := range data {
				e.logger.Debug(fmt.Sprintf("  %s: %v", k, v))
			}

			if e.eventEmitter != nil {
				e.eventEmitter.EmitWorkflowStarted(ctx, workflowType, action, data)
			}

			// Execute the workflow using the handler
			results, err := handler.ExecuteWorkflow(ctx, workflowType, action, data)
			if err != nil {
				e.logger.Error(fmt.Sprintf("Failed to execute workflow '%s': %v", workflowType, err))
				if e.eventEmitter != nil {
					e.eventEmitter.EmitWorkflowFailed(ctx, workflowType, action, time.Since(startTime), err)
				}
				e.recordWorkflowMetrics(workflowType, action, "error", time.Since(startTime))
				return fmt.Errorf("workflow execution failed: %w", err)
			}

			// Log execution results in debug mode
			e.logger.Info(fmt.Sprintf("Workflow '%s' executed successfully", workflowType))
			for k, v := range results {
				e.logger.Debug(fmt.Sprintf("  Result %s: %v", k, v))
			}

			// If the caller stored a PipelineResultHolder in the context, populate it
			// so HTTP trigger handlers can read response_status/body/headers without
			// requiring the WorkflowEngine interface to return a result map.
			if holder, ok := ctx.Value(module.PipelineResultContextKey).(*module.PipelineResultHolder); ok && holder != nil {
				holder.Set(results)
			}

			if e.eventEmitter != nil {
				e.eventEmitter.EmitWorkflowCompleted(ctx, workflowType, action, time.Since(startTime), results)
			}
			e.recordWorkflowMetrics(workflowType, action, "success", time.Since(startTime))
			return nil
		}
	}

	return fmt.Errorf("no handler found for workflow type: %s", workflowType)
}

// ExecutePipeline runs a named pipeline synchronously and returns its
// structured output. For use by Go callers (gRPC servers, tests) that
// don't need HTTP request/response threading.
//
// If the pipeline uses step.pipeline_output, the explicitly marked output
// is returned. Otherwise, the pipeline's merged Current state is returned.
//
// This goes through TriggerWorkflow so all trigger paths share the same
// execution code, including StepOutputs preservation via PipelineContextHolder.
func (e *StdEngine) ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error) {
	holder := &module.PipelineContextHolder{}
	ctx = context.WithValue(ctx, module.PipelineContextKey, holder)

	if err := e.TriggerWorkflow(ctx, "pipeline:"+name, "", data); err != nil {
		return nil, err
	}

	pc := holder.Get()
	if pc == nil {
		return nil, fmt.Errorf("pipeline %q: no context returned", name)
	}

	// Prefer explicit pipeline output if step.pipeline_output was used.
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		return pipeOut, nil
	}

	// Fallback: return the full merged pipeline state. Note that Current
	// contains all step outputs merged flat, including internal markers like
	// _response_handled. Pipelines intended for Go callers should use
	// step.pipeline_output to define an explicit return contract.
	return pc.Current, nil
}

// ExecutePipelineContext runs a named pipeline and returns the full PipelineContext,
// including StepOutputs for each step. This is intended for test harnesses that need
// per-step output inspection. For production callers, use ExecutePipeline instead.
//
// This goes through TriggerWorkflow so all trigger paths share the same
// execution code. The PipelineContextHolder captures the full context.
func (e *StdEngine) ExecutePipelineContext(ctx context.Context, name string, data map[string]any) (*interfaces.PipelineContext, error) {
	holder := &module.PipelineContextHolder{}
	ctx = context.WithValue(ctx, module.PipelineContextKey, holder)

	if err := e.TriggerWorkflow(ctx, "pipeline:"+name, "", data); err != nil {
		return nil, err
	}

	pc := holder.Get()
	if pc == nil {
		return nil, fmt.Errorf("pipeline %q: no context returned", name)
	}

	return pc, nil
}

// recordWorkflowMetrics is defined in engine_module_bridge.go.
// It records execution metrics via interfaces.MetricsRecorder so that engine.go
// need not reference the concrete *module.MetricsCollector type.

// configureTriggers sets up all triggers from configuration
func (e *StdEngine) configureTriggers(triggerConfigs map[string]any) error {
	// Always register the engine as a service so inline pipeline triggers can find it
	if err := e.app.RegisterService("workflowEngine", e); err != nil {
		return fmt.Errorf("failed to register workflow engine service: %w", err)
	}

	if len(triggerConfigs) == 0 {
		// No triggers configured, which is fine
		return nil
	}

	// Configure each trigger type
	for triggerType, triggerConfig := range triggerConfigs {
		// Find a handler for this trigger type
		var handlerFound bool
		for _, trigger := range e.triggers {
			// If this trigger can handle the type, configure it
			if e.canHandleTrigger(trigger, triggerType) {
				if err := trigger.Configure(e.app, triggerConfig); err != nil {
					return fmt.Errorf("failed to configure trigger '%s': %w", triggerType, err)
				}
				handlerFound = true
				break
			}
		}

		if !handlerFound {
			return fmt.Errorf("no handler found for trigger type '%s'", triggerType)
		}
	}

	return nil
}

// canHandleTrigger determines if a trigger can handle a specific trigger type
// by looking up the trigger type in the engine's registry. Falls back to
// matching the trigger name directly (e.g., trigger type "mock" matches
// trigger name "mock.trigger" via "trigger.<type>" convention).
func (e *StdEngine) canHandleTrigger(trigger interfaces.Trigger, triggerType string) bool {
	// Check the trigger type registry first (populated by LoadPlugin and RegisterTriggerType)
	if expectedName, ok := e.triggerTypeMap[triggerType]; ok {
		return trigger.Name() == expectedName
	}
	// Fallback: try convention "trigger.<type>" (supports test mocks and
	// triggers registered outside the plugin system)
	return trigger.Name() == "trigger."+triggerType || trigger.Name() == triggerType+".trigger" || trigger.Name() == triggerType
}

// configurePipelines creates Pipeline objects from config and registers them
// with the PipelineWorkflowHandler.
func (e *StdEngine) configurePipelines(pipelineCfg map[string]any) error {
	// Find the PipelineAdder among registered workflow handlers
	var adder PipelineAdder
	for _, handler := range e.workflowHandlers {
		if a, ok := handler.(PipelineAdder); ok {
			adder = a
			break
		}
	}
	if adder == nil {
		return fmt.Errorf("no PipelineWorkflowHandler registered; cannot configure pipelines")
	}

	for pipelineName, rawCfg := range pipelineCfg {
		// Marshal to YAML then unmarshal into PipelineConfig to leverage struct tags
		yamlBytes, err := yaml.Marshal(rawCfg)
		if err != nil {
			return fmt.Errorf("pipeline %q: failed to marshal config: %w", pipelineName, err)
		}
		var pipeCfg config.PipelineConfig
		if err := yaml.Unmarshal(yamlBytes, &pipeCfg); err != nil {
			return fmt.Errorf("pipeline %q: failed to parse config: %w", pipelineName, err)
		}

		// Build steps
		steps, err := e.buildPipelineSteps(pipelineName, pipeCfg.Steps)
		if err != nil {
			return fmt.Errorf("pipeline %q: %w", pipelineName, err)
		}

		// Build compensation steps
		compSteps, err := e.buildPipelineSteps(pipelineName, pipeCfg.Compensation)
		if err != nil {
			return fmt.Errorf("pipeline %q compensation: %w", pipelineName, err)
		}

		// Parse error strategy
		onError := module.ErrorStrategyStop
		switch pipeCfg.OnError {
		case "skip":
			onError = module.ErrorStrategySkip
		case "compensate":
			onError = module.ErrorStrategyCompensate
		}

		// Parse timeout
		var timeout time.Duration
		if pipeCfg.Timeout != "" {
			timeout, err = time.ParseDuration(pipeCfg.Timeout)
			if err != nil {
				return fmt.Errorf("pipeline %q: invalid timeout %q: %w", pipelineName, pipeCfg.Timeout, err)
			}
		}

		pipeline := &module.Pipeline{
			Name:            pipelineName,
			Steps:           steps,
			OnError:         onError,
			Timeout:         timeout,
			Compensation:    compSteps,
			StrictTemplates: pipeCfg.StrictTemplates,
		}

		// Propagate the engine's logger to the pipeline so that execution logs
		// (Pipeline started, Step completed, etc.) use the same logger instance
		// as the rest of the engine rather than falling back to slog.Default().
		// This ensures that callers who pass a discard logger via WithLogger get
		// full suppression without needing to mutate the global slog default.
		if sl, ok := e.logger.(*slog.Logger); ok {
			pipeline.Logger = sl
		}

		// Set RoutePattern from inline HTTP trigger path so that step.request_parse
		// can extract path parameters via _route_pattern in the pipeline context.
		if pipeCfg.Trigger.Type == "http" {
			if path, _ := pipeCfg.Trigger.Config["path"].(string); path != "" {
				pipeline.RoutePattern = path
			}
		}

		adder.AddPipeline(pipelineName, pipeline)
		// Register in the engine's pipeline registry so step.workflow_call can
		// look up this pipeline at execution time.
		e.pipelineRegistry[pipelineName] = pipeline
		e.logger.Info(fmt.Sprintf("Configured pipeline: %s (%d steps)", pipelineName, len(steps)))

		// Create trigger from inline trigger config if present.
		// Pipeline triggers are best-effort: if no matching trigger handler is
		// registered or the handler can't configure (e.g. missing router service),
		// the pipeline is still usable via the API — so we warn rather than fail.
		if pipeCfg.Trigger.Type != "" {
			triggerCfg := pipeCfg.Trigger.Config
			if triggerCfg == nil {
				triggerCfg = make(map[string]any)
			}

			// Pipeline trigger configs use a flat format (e.g. {path, method})
			// but trigger handlers expect their native format. Wrap as needed.
			wrappedCfg := e.wrapPipelineTriggerConfig(pipeCfg.Trigger.Type, pipelineName, triggerCfg)

			// Find a matching trigger and configure it
			configured := false
			for _, trigger := range e.triggers {
				if e.canHandleTrigger(trigger, pipeCfg.Trigger.Type) {
					if err := trigger.Configure(e.app, wrappedCfg); err != nil {
						e.logger.Warn(fmt.Sprintf("Pipeline %q: could not configure %s trigger (pipeline still usable via API): %v",
							pipelineName, pipeCfg.Trigger.Type, err))
					} else {
						configured = true
					}
					break
				}
			}
			if !configured {
				e.logger.Debug(fmt.Sprintf("Pipeline %q: no handler registered for trigger type %q", pipelineName, pipeCfg.Trigger.Type))
			}
		}
	}

	return nil
}

// wrapPipelineTriggerConfig converts a flat pipeline trigger config into the
// format expected by the corresponding trigger handler. Pipeline triggers use a
// simple format (e.g. {path, method}) while trigger handlers expect their native
// config schema (e.g. HTTPTrigger expects {routes: [{...}]}).
//
// Wrapper functions are registered by plugins via PipelineTriggerConfigProvider
// or RegisterTriggerConfigWrapper. If no wrapper is registered for the trigger
// type, the config is passed through with a workflowType field injected.
func (e *StdEngine) wrapPipelineTriggerConfig(triggerType, pipelineName string, cfg map[string]any) map[string]any {
	if wrapper, ok := e.triggerConfigWrappers[triggerType]; ok {
		return wrapper(pipelineName, cfg)
	}
	// Default: pass config as-is with workflow type injected
	cfg["workflowType"] = "pipeline:" + pipelineName
	return cfg
}

// buildPipelineSteps creates PipelineStep instances from step configurations.
// RoutePipelineSetter is implemented by handlers (QueryHandler, CommandHandler) that support per-route pipelines.
type RoutePipelineSetter interface {
	SetRoutePipeline(routePath string, pipeline interfaces.PipelineRunner)
}

// configureRoutePipelines scans HTTP workflow routes for inline pipeline steps
// and attaches them to the appropriate CQRS handlers.
func (e *StdEngine) configureRoutePipelines(cfg *config.WorkflowConfig) error {
	for _, workflowConfig := range cfg.Workflows {
		httpConfig, ok := workflowConfig.(map[string]any)
		if !ok {
			continue
		}

		routesConfig, ok := httpConfig["routes"].([]any)
		if !ok {
			continue
		}

		for _, rc := range routesConfig {
			routeMap, ok := rc.(map[string]any)
			if !ok {
				continue
			}

			handlerName, _ := routeMap["handler"].(string)
			if handlerName == "" {
				continue
			}

			// Check for inline pipeline steps on this route
			var stepCfgs []config.PipelineStepConfig

			if pipelineCfg, ok := routeMap["pipeline"].(map[string]any); ok {
				if stepsRaw, ok := pipelineCfg["steps"].([]any); ok {
					stepCfgs = parseRoutePipelineSteps(stepsRaw)
				}
			} else if stepsRaw, ok := routeMap["steps"].([]any); ok {
				stepCfgs = parseRoutePipelineSteps(stepsRaw)
			}

			if len(stepCfgs) == 0 {
				continue
			}

			// Build route key from method + path for unique pipeline lookup.
			// Using the full "METHOD /path" pattern avoids collisions when
			// multiple routes share the same last segment (e.g.,
			// POST /projects/{id}/workflows vs POST /workflows).
			path, _ := routeMap["path"].(string)
			method, _ := routeMap["method"].(string)
			routeKey := method + " " + path
			pipelineName := handlerName + ":" + lastRouteSegment(path)

			steps, err := e.buildPipelineSteps(pipelineName, stepCfgs)
			if err != nil {
				return fmt.Errorf("route pipeline %q: %w", pipelineName, err)
			}

			pipeline := &module.Pipeline{
				Name:         pipelineName,
				Steps:        steps,
				RoutePattern: path,
			}

			// Find the handler service and attach the pipeline
			svc, ok := e.app.SvcRegistry()[handlerName]
			if !ok {
				e.logger.Warn("Handler service not found for route pipeline", "handler", handlerName)
				continue
			}

			if setter, ok := svc.(RoutePipelineSetter); ok {
				setter.SetRoutePipeline(routeKey, pipeline)
				e.logger.Info("Attached route pipeline", "handler", handlerName, "route", routeKey, "steps", len(steps))
			}
		}
	}
	return nil
}

// parseRoutePipelineSteps converts raw YAML step configs to PipelineStepConfig.
func parseRoutePipelineSteps(stepsRaw []any) []config.PipelineStepConfig {
	var cfgs []config.PipelineStepConfig
	for _, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := stepMap["name"].(string)
		stepType, _ := stepMap["type"].(string)
		stepConfig, _ := stepMap["config"].(map[string]any)
		if name == "" || stepType == "" {
			continue
		}
		skipIf, _ := stepMap["skip_if"].(string)
		ifExpr, _ := stepMap["if"].(string)
		cfgs = append(cfgs, config.PipelineStepConfig{
			Name:   name,
			Type:   stepType,
			Config: stepConfig,
			SkipIf: skipIf,
			If:     ifExpr,
		})
	}
	return cfgs
}

// lastRouteSegment extracts the last segment of a URL path.
func lastRouteSegment(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func (e *StdEngine) buildPipelineSteps(pipelineName string, stepCfgs []config.PipelineStepConfig) ([]module.PipelineStep, error) {
	if len(stepCfgs) == 0 {
		return nil, nil
	}

	steps := make([]module.PipelineStep, 0, len(stepCfgs))
	for _, sc := range stepCfgs {
		stepConfig := sc.Config
		if stepConfig == nil {
			stepConfig = make(map[string]any)
		}

		// Inject config directory so steps can resolve relative paths
		// consistently with module configs (relative to config file, not CWD).
		if e.configDir != "" {
			stepConfig["_config_dir"] = e.configDir
		}

		step, err := e.stepRegistry.Create(sc.Type, sc.Name, stepConfig, e.app)
		if err != nil {
			return nil, fmt.Errorf("step %q (type %s): %w", sc.Name, sc.Type, err)
		}

		// Wrap the step with skip_if / if guard when either field is set.
		if sc.SkipIf != "" || sc.If != "" {
			step = module.NewSkippableStep(step, sc.SkipIf, sc.If)
		}

		// Wrap the step with an error_status mapper when set so that step
		// failures are surfaced as ValidationErrors and the HTTP handler
		// returns the configured 4xx status instead of 500.
		if sc.ErrorStatus != 0 {
			step = module.NewErrorStatusStep(step, sc.ErrorStatus)
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// GetApp returns the underlying modular Application.
func (e *StdEngine) GetApp() modular.Application {
	return e.app
}

// ReconfigureModules attempts to reconfigure only the specified modules
// without a full engine restart. Returns modules that could not be reconfigured.
func (e *StdEngine) ReconfigureModules(ctx context.Context, changes []config.ModuleConfigChange) ([]string, error) {
	var failedModules []string

	for _, change := range changes {
		mod := e.app.GetModule(change.Name)
		if mod == nil {
			e.logger.Warn(fmt.Sprintf("Module %q not found for reconfiguration", change.Name))
			failedModules = append(failedModules, change.Name)
			continue
		}

		reconf, ok := mod.(interfaces.Reconfigurable)
		if !ok {
			e.logger.Info(fmt.Sprintf("Module %q does not support runtime reconfiguration", change.Name))
			failedModules = append(failedModules, change.Name)
			continue
		}

		if err := reconf.Reconfigure(ctx, change.NewConfig); err != nil {
			e.logger.Error(fmt.Sprintf("Failed to reconfigure module %q: %v", change.Name, err))
			failedModules = append(failedModules, change.Name)
		} else {
			e.logger.Info(fmt.Sprintf("Reconfigured module %q in-place", change.Name))
		}
	}

	return failedModules, nil
}

// GetPipeline returns the named pipeline from the engine's pipeline registry.
// Returns nil and false if no pipeline with the given name exists.
// This is useful for CLI tools (e.g., wfctl pipeline run) that need to
// execute a pipeline directly without starting the HTTP server.
func (e *StdEngine) GetPipeline(name string) (*module.Pipeline, bool) {
	p, ok := e.pipelineRegistry[name]
	return p, ok
}

// LoadedPlugins returns all engine plugins that were loaded via LoadPlugin.
func (e *StdEngine) LoadedPlugins() []plugin.EnginePlugin {
	out := make([]plugin.EnginePlugin, len(e.enginePlugins))
	copy(out, e.enginePlugins)
	return out
}

// Compile-time interface check: StdEngine must satisfy PipelineExecutor.
var _ interfaces.PipelineExecutor = (*StdEngine)(nil)

type Engine interface {
	RegisterWorkflowHandler(handler WorkflowHandler)
	RegisterTrigger(trigger interfaces.Trigger)
	AddModuleType(moduleType string, factory ModuleFactory)
	BuildFromConfig(cfg *config.WorkflowConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
}

// expandConfigStrings walks a config map and expands all ${...} secret
// references in string values using the given MultiResolver. Errors are
// silently ignored — if a reference cannot be resolved, the original value
// is preserved, maintaining backward compatibility.
func expandConfigStrings(resolver *secrets.MultiResolver, cfg map[string]any) {
	if resolver == nil || cfg == nil {
		return
	}
	ctx := context.Background()
	for k, v := range cfg {
		switch val := v.(type) {
		case string:
			if expanded, err := resolver.Expand(ctx, val); err == nil {
				cfg[k] = expanded
			}
		case map[string]any:
			expandConfigStrings(resolver, val)
		case []any:
			expandConfigSlice(resolver, val)
		}
	}
}

// expandConfigSlice expands ${...} placeholders in a slice, recursing into nested
// maps and slices to support arbitrary nesting depth.
func expandConfigSlice(resolver *secrets.MultiResolver, items []any) {
	ctx := context.Background()
	for i, item := range items {
		switch v := item.(type) {
		case string:
			if expanded, err := resolver.Expand(ctx, v); err == nil {
				items[i] = expanded
			}
		case map[string]any:
			expandConfigStrings(resolver, v)
		case []any:
			expandConfigSlice(resolver, v)
		}
	}
}
