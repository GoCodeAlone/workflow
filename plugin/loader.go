package plugin

import (
	"fmt"
	"log/slog"
	"reflect"
	"sort"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/schema"
)

// LicenseValidator is an optional service that approves or denies premium plugin usage.
// If registered under the name "license-validator", the loader will call it during
// tier validation for premium plugins.
type LicenseValidator interface {
	// ValidatePlugin returns nil if the named plugin is licensed for use.
	ValidatePlugin(pluginName string) error
}

// PluginLoader loads EnginePlugins and populates registries.
type PluginLoader struct {
	capabilityReg        *capability.Registry
	moduleFactories      map[string]ModuleFactory
	stepFactories        map[string]StepFactory
	triggerFactories     map[string]TriggerFactory
	handlerFactories     map[string]WorkflowHandlerFactory
	wiringHooks          []WiringHook
	configTransformHooks []ConfigTransformHook
	schemaRegistry       *schema.ModuleSchemaRegistry
	plugins              []EnginePlugin
	licenseValidator     LicenseValidator
}

// NewPluginLoader creates a new PluginLoader backed by the given capability and schema registries.
func NewPluginLoader(capReg *capability.Registry, schemaReg *schema.ModuleSchemaRegistry) *PluginLoader {
	return &PluginLoader{
		capabilityReg:    capReg,
		moduleFactories:  make(map[string]ModuleFactory),
		stepFactories:    make(map[string]StepFactory),
		triggerFactories: make(map[string]TriggerFactory),
		handlerFactories: make(map[string]WorkflowHandlerFactory),
		schemaRegistry:   schemaReg,
	}
}

// SetLicenseValidator registers a license validator used for premium tier plugins.
func (l *PluginLoader) SetLicenseValidator(v LicenseValidator) {
	l.licenseValidator = v
}

// ValidateTier checks whether a plugin's tier is allowed given the current
// license validator configuration:
//   - Core and Community plugins are always allowed.
//   - Premium plugins are validated against the LicenseValidator if one is set.
//     If no validator is configured, a warning is logged and the plugin is allowed
//     (graceful degradation for self-hosted deployments without a license).
func (l *PluginLoader) ValidateTier(manifest *PluginManifest) error {
	switch manifest.Tier {
	case TierCore, TierCommunity, "":
		// Always allowed; empty tier treated as core.
		return nil
	case TierPremium:
		if l.licenseValidator == nil {
			slog.Warn("premium plugin loaded without license validator — allowing for self-hosted deployment",
				"plugin", manifest.Name)
			return nil
		}
		if err := l.licenseValidator.ValidatePlugin(manifest.Name); err != nil {
			return fmt.Errorf("plugin %q requires a valid license: %w", manifest.Name, err)
		}
		return nil
	default:
		return fmt.Errorf("plugin %q has unknown tier %q", manifest.Name, manifest.Tier)
	}
}

// LoadPlugin validates a plugin's manifest, registers its capabilities, factories,
// schemas, and wiring hooks. Returns an error if any factory type conflicts with
// an existing registration.
func (l *PluginLoader) LoadPlugin(p EnginePlugin) error {
	manifest := p.EngineManifest()
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("plugin %q: %w", manifest.Name, err)
	}

	// Validate plugin tier before proceeding.
	if err := l.ValidateTier(manifest); err != nil {
		return err
	}

	// Register capability contracts.
	for _, c := range p.Capabilities() {
		if err := l.capabilityReg.RegisterContract(c); err != nil {
			return fmt.Errorf("plugin %q: register contract %q: %w", manifest.Name, c.Name, err)
		}
	}

	// Register capability providers from manifest declarations.
	for _, decl := range manifest.Capabilities {
		if decl.Role == "provider" {
			if err := l.capabilityReg.RegisterProvider(decl.Name, manifest.Name, decl.Priority, reflect.TypeOf((*EnginePlugin)(nil)).Elem()); err != nil {
				return fmt.Errorf("plugin %q: register provider for %q: %w", manifest.Name, decl.Name, err)
			}
		}
	}

	// Register module factories — conflict on duplicate type.
	for typeName, factory := range p.ModuleFactories() {
		if _, exists := l.moduleFactories[typeName]; exists {
			return fmt.Errorf("plugin %q: module type %q already registered", manifest.Name, typeName)
		}
		l.moduleFactories[typeName] = factory
	}

	// Register step factories — conflict on duplicate type.
	for typeName, factory := range p.StepFactories() {
		if _, exists := l.stepFactories[typeName]; exists {
			return fmt.Errorf("plugin %q: step type %q already registered", manifest.Name, typeName)
		}
		l.stepFactories[typeName] = factory
	}

	// Register trigger factories — conflict on duplicate type.
	for typeName, factory := range p.TriggerFactories() {
		if _, exists := l.triggerFactories[typeName]; exists {
			return fmt.Errorf("plugin %q: trigger type %q already registered", manifest.Name, typeName)
		}
		l.triggerFactories[typeName] = factory
	}

	// Register workflow handler factories — conflict on duplicate type.
	for typeName, factory := range p.WorkflowHandlers() {
		if _, exists := l.handlerFactories[typeName]; exists {
			return fmt.Errorf("plugin %q: workflow handler type %q already registered", manifest.Name, typeName)
		}
		l.handlerFactories[typeName] = factory
	}

	// Register module schemas.
	for _, s := range p.ModuleSchemas() {
		l.schemaRegistry.Register(s)
	}

	// Collect wiring hooks.
	l.wiringHooks = append(l.wiringHooks, p.WiringHooks()...)

	// Collect config transform hooks.
	l.configTransformHooks = append(l.configTransformHooks, p.ConfigTransformHooks()...)

	l.plugins = append(l.plugins, p)
	return nil
}

// LoadPlugins performs a topological sort of plugins by their manifest dependencies,
// then loads each in order. Returns an error on circular dependencies or load failures.
func (l *PluginLoader) LoadPlugins(plugins []EnginePlugin) error {
	sorted, err := topoSortPlugins(plugins)
	if err != nil {
		return err
	}
	for _, p := range sorted {
		if err := l.LoadPlugin(p); err != nil {
			return err
		}
	}
	return nil
}

// ModuleFactories returns a defensive copy of all registered module factories.
func (l *PluginLoader) ModuleFactories() map[string]ModuleFactory {
	out := make(map[string]ModuleFactory, len(l.moduleFactories))
	for k, v := range l.moduleFactories {
		out[k] = v
	}
	return out
}

// StepFactories returns a defensive copy of all registered step factories.
func (l *PluginLoader) StepFactories() map[string]StepFactory {
	out := make(map[string]StepFactory, len(l.stepFactories))
	for k, v := range l.stepFactories {
		out[k] = v
	}
	return out
}

// TriggerFactories returns a defensive copy of all registered trigger factories.
func (l *PluginLoader) TriggerFactories() map[string]TriggerFactory {
	out := make(map[string]TriggerFactory, len(l.triggerFactories))
	for k, v := range l.triggerFactories {
		out[k] = v
	}
	return out
}

// WorkflowHandlerFactories returns a defensive copy of all registered workflow handler factories.
func (l *PluginLoader) WorkflowHandlerFactories() map[string]WorkflowHandlerFactory {
	out := make(map[string]WorkflowHandlerFactory, len(l.handlerFactories))
	for k, v := range l.handlerFactories {
		out[k] = v
	}
	return out
}

// WiringHooks returns all registered wiring hooks sorted by priority (highest first).
func (l *PluginLoader) WiringHooks() []WiringHook {
	hooks := make([]WiringHook, len(l.wiringHooks))
	copy(hooks, l.wiringHooks)
	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].Priority > hooks[j].Priority
	})
	return hooks
}

// ConfigTransformHooks returns all registered config transform hooks sorted by priority (highest first).
func (l *PluginLoader) ConfigTransformHooks() []ConfigTransformHook {
	hooks := make([]ConfigTransformHook, len(l.configTransformHooks))
	copy(hooks, l.configTransformHooks)
	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].Priority > hooks[j].Priority
	})
	return hooks
}

// CapabilityRegistry returns the loader's capability registry.
func (l *PluginLoader) CapabilityRegistry() *capability.Registry {
	return l.capabilityReg
}

// LoadedPlugins returns all successfully loaded plugins in load order.
func (l *PluginLoader) LoadedPlugins() []EnginePlugin {
	out := make([]EnginePlugin, len(l.plugins))
	copy(out, l.plugins)
	return out
}

// topoSortPlugins performs a topological sort of plugins based on manifest dependencies.
// Returns an error if a circular dependency is detected.
func topoSortPlugins(plugins []EnginePlugin) ([]EnginePlugin, error) {
	byName := make(map[string]EnginePlugin, len(plugins))
	for _, p := range plugins {
		byName[p.EngineManifest().Name] = p
	}

	// States: 0=unvisited, 1=visiting, 2=visited.
	state := make(map[string]int, len(plugins))
	var order []EnginePlugin

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case 2:
			return nil // already processed
		case 1:
			return fmt.Errorf("circular dependency detected involving plugin %q", name)
		}

		state[name] = 1 // mark visiting

		p, exists := byName[name]
		if !exists {
			// External dependency not in the provided set — skip (it may already be loaded).
			state[name] = 2
			return nil
		}

		for _, dep := range p.EngineManifest().Dependencies {
			if err := visit(dep.Name); err != nil {
				return err
			}
		}

		state[name] = 2 // mark visited
		order = append(order, p)
		return nil
	}

	for _, p := range plugins {
		if err := visit(p.EngineManifest().Name); err != nil {
			return nil, err
		}
	}

	return order, nil
}
