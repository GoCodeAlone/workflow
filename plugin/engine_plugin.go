package plugin

import (
	"net/http"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// EnginePlugin extends NativePlugin with engine-level contributions:
// module type factories, step type factories, trigger factories,
// workflow handlers, capability contracts, and wiring hooks.
type EnginePlugin interface {
	NativePlugin

	// EngineManifest returns the extended plugin manifest with capability declarations.
	EngineManifest() *PluginManifest

	// Capabilities returns the capability contracts this plugin defines or satisfies.
	Capabilities() []capability.Contract

	// ModuleFactories returns module type factories.
	// Key is the module type string (e.g., "http.server").
	// Value is func(name string, cfg map[string]any) modular.Module
	ModuleFactories() map[string]ModuleFactory

	// StepFactories returns pipeline step type factories.
	// Key is the step type string (e.g., "step.validate").
	StepFactories() map[string]StepFactory

	// TriggerFactories returns trigger type constructors.
	// Key is the trigger type string (e.g., "http").
	TriggerFactories() map[string]TriggerFactory

	// WorkflowHandlers returns workflow handler factories.
	// Key is the workflow type string (e.g., "http", "messaging").
	WorkflowHandlers() map[string]WorkflowHandlerFactory

	// ModuleSchemas returns UI schema definitions for this plugin's module types.
	ModuleSchemas() []*schema.ModuleSchema

	// WiringHooks returns post-init wiring functions.
	WiringHooks() []WiringHook
}

// ModuleFactory creates a modular.Module from a name and config map.
type ModuleFactory func(name string, config map[string]any) modular.Module

// StepFactory creates a pipeline step from config.
// The returned value should implement the PipelineStep interface
// (module.PipelineStep). We use any here to avoid a circular import
// on the module package. The app parameter provides access to the
// modular.Application service registry for steps that need it
// (e.g., db_exec, db_query, delegate).
type StepFactory func(name string, config map[string]any, app modular.Application) (any, error)

// TriggerFactory creates a trigger instance.
// The returned value should implement the Trigger interface
// (module.Trigger: Name, Start, Stop, Configure).
type TriggerFactory func() any

// WorkflowHandlerFactory creates a workflow handler instance.
// The returned value should implement WorkflowHandler
// (CanHandle, ConfigureWorkflow, ExecuteWorkflow).
type WorkflowHandlerFactory func() any

// WiringHook is called after module initialization to wire cross-module integrations.
type WiringHook struct {
	Name     string
	Priority int // higher priority runs first
	Hook     func(app modular.Application, cfg *config.WorkflowConfig) error
}

// TriggerConfigWrapperFunc converts flat pipeline trigger config into the
// trigger's native configuration format (e.g., wrapping {path, method} into
// {routes: [{...}]} for HTTP triggers).
type TriggerConfigWrapperFunc func(pipelineName string, flatConfig map[string]any) map[string]any

// PipelineTriggerConfigProvider is optionally implemented by EnginePlugins that
// register triggers. It provides config wrapper functions that convert flat
// pipeline trigger config (e.g., {path, method}) into the trigger's native
// configuration format (e.g., {routes: [{...}]}).
type PipelineTriggerConfigProvider interface {
	PipelineTriggerConfigWrappers() map[string]TriggerConfigWrapperFunc
}

// NativePluginProvider is optionally implemented by EnginePlugins that also
// contribute NativePlugins (e.g., for Marketplace visibility, UI pages, or
// HTTP route handlers). The PluginContext provides shared resources (DB, logger).
type NativePluginProvider interface {
	NativePlugins(ctx PluginContext) []NativePlugin
}


// BaseNativePlugin provides no-op defaults for all NativePlugin methods.
// Embed this in concrete implementations to only override what you need.
type BaseNativePlugin struct {
	PluginName        string
	PluginVersion     string
	PluginDescription string
}

func (b *BaseNativePlugin) Name() string                     { return b.PluginName }
func (b *BaseNativePlugin) Version() string                  { return b.PluginVersion }
func (b *BaseNativePlugin) Description() string              { return b.PluginDescription }
func (b *BaseNativePlugin) Dependencies() []PluginDependency { return nil }
func (b *BaseNativePlugin) UIPages() []UIPageDef             { return nil }
func (b *BaseNativePlugin) RegisterRoutes(_ *http.ServeMux)  {}
func (b *BaseNativePlugin) OnEnable(_ PluginContext) error   { return nil }
func (b *BaseNativePlugin) OnDisable(_ PluginContext) error  { return nil }

// BaseEnginePlugin provides no-op defaults for all EnginePlugin methods.
// Embed this in concrete plugin implementations to only override what you need.
type BaseEnginePlugin struct {
	BaseNativePlugin
	Manifest PluginManifest
}

// EngineManifest returns the plugin manifest.
func (b *BaseEnginePlugin) EngineManifest() *PluginManifest {
	return &b.Manifest
}

// Capabilities returns an empty capability list.
func (b *BaseEnginePlugin) Capabilities() []capability.Contract {
	return nil
}

// ModuleFactories returns no module factories.
func (b *BaseEnginePlugin) ModuleFactories() map[string]ModuleFactory {
	return nil
}

// StepFactories returns no step factories.
func (b *BaseEnginePlugin) StepFactories() map[string]StepFactory {
	return nil
}

// TriggerFactories returns no trigger factories.
func (b *BaseEnginePlugin) TriggerFactories() map[string]TriggerFactory {
	return nil
}

// WorkflowHandlers returns no workflow handler factories.
func (b *BaseEnginePlugin) WorkflowHandlers() map[string]WorkflowHandlerFactory {
	return nil
}

// ModuleSchemas returns no module schemas.
func (b *BaseEnginePlugin) ModuleSchemas() []*schema.ModuleSchema {
	return nil
}

// WiringHooks returns no wiring hooks.
func (b *BaseEnginePlugin) WiringHooks() []WiringHook {
	return nil
}
