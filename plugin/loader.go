package plugin

import (
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/deploy"
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
	stepSchemaRegistry   *schema.StepSchemaRegistry
	plugins              []EnginePlugin
	licenseValidator     LicenseValidator
	cosignVerifier       *CosignVerifier
	deployTargets        map[string]deploy.DeployTarget
	sidecarProviders     map[string]deploy.SidecarProvider
	overridableTypes     map[string]bool // types declared overridable by any loaded plugin
	engineVersion        string          // running engine version for minEngineVersion checks
}

// NewPluginLoader creates a new PluginLoader backed by the given capability and schema registries.
func NewPluginLoader(capReg *capability.Registry, schemaReg *schema.ModuleSchemaRegistry, stepSchemaReg ...*schema.StepSchemaRegistry) *PluginLoader {
	var ssr *schema.StepSchemaRegistry
	if len(stepSchemaReg) > 0 && stepSchemaReg[0] != nil {
		ssr = stepSchemaReg[0]
	} else {
		ssr = schema.NewStepSchemaRegistry()
	}
	return &PluginLoader{
		capabilityReg:      capReg,
		moduleFactories:    make(map[string]ModuleFactory),
		stepFactories:      make(map[string]StepFactory),
		triggerFactories:   make(map[string]TriggerFactory),
		handlerFactories:   make(map[string]WorkflowHandlerFactory),
		schemaRegistry:     schemaReg,
		stepSchemaRegistry: ssr,
		deployTargets:      make(map[string]deploy.DeployTarget),
		sidecarProviders:   make(map[string]deploy.SidecarProvider),
		overridableTypes:   make(map[string]bool),
	}
}

// OverridableTypes returns a read-only copy of all type names that have been
// declared overridable by loaded plugins.
func (l *PluginLoader) OverridableTypes() map[string]bool {
	out := make(map[string]bool, len(l.overridableTypes))
	for k, v := range l.overridableTypes {
		out[k] = v
	}
	return out
}

// SetEngineVersion sets the running engine version used for minEngineVersion
// compatibility checks when loading plugins.
func (l *PluginLoader) SetEngineVersion(v string) {
	l.engineVersion = v
}

// SetLicenseValidator registers a license validator used for premium tier plugins.
func (l *PluginLoader) SetLicenseValidator(v LicenseValidator) {
	l.licenseValidator = v
}

// SetCosignVerifier registers a cosign verifier for binary signature verification
// of premium plugins. When set, LoadBinaryPlugin will verify the plugin binary
// before loading it.
func (l *PluginLoader) SetCosignVerifier(v *CosignVerifier) {
	l.cosignVerifier = v
}

// LoadBinaryPlugin verifies a plugin binary with cosign (for premium plugins) and
// then loads the plugin into the registry. binaryPath, sigPath, and certPath are
// paths to the plugin binary, cosign signature file, and certificate file
// respectively. If cosignVerifier is nil, verification is skipped.
func (l *PluginLoader) LoadBinaryPlugin(p EnginePlugin, binaryPath, sigPath, certPath string) error {
	manifest := p.EngineManifest()
	if manifest.Tier == TierPremium && l.cosignVerifier != nil {
		if err := l.cosignVerifier.Verify(binaryPath, sigPath, certPath); err != nil {
			return fmt.Errorf("plugin %q: binary verification failed: %w", manifest.Name, err)
		}
	}
	return l.LoadPlugin(p)
}

// LoadBinaryPluginWithOverride is the override-capable counterpart to
// LoadBinaryPlugin. It verifies the plugin binary with cosign (for premium
// plugins) and then loads the plugin, allowing it to override existing module,
// step, trigger, handler, deploy target, and sidecar provider registrations.
// When a duplicate type is encountered, the new factory replaces the previous
// one and a warning is logged instead of returning an error.
func (l *PluginLoader) LoadBinaryPluginWithOverride(p EnginePlugin, binaryPath, sigPath, certPath string) error {
	manifest := p.EngineManifest()
	if manifest.Tier == TierPremium && l.cosignVerifier != nil {
		if err := l.cosignVerifier.Verify(binaryPath, sigPath, certPath); err != nil {
			return fmt.Errorf("plugin %q: binary verification failed: %w", manifest.Name, err)
		}
	}
	return l.LoadPluginWithOverride(p)
}

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
	return l.loadPlugin(p, false)
}

// LoadPluginWithOverride is like LoadPlugin but allows the plugin to override
// existing module, step, trigger, handler, deploy target, and sidecar provider
// registrations. When a duplicate type is encountered, the new factory replaces
// the previous one and a warning is logged instead of returning an error.
// This is intended for external plugins that intentionally replace built-in
// defaults (e.g., replacing a mock authz step with a production implementation).
func (l *PluginLoader) LoadPluginWithOverride(p EnginePlugin) error {
	return l.loadPlugin(p, true)
}

func (l *PluginLoader) loadPlugin(p EnginePlugin, allowOverride bool) error {
	manifest := p.EngineManifest()
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("plugin %q: %w", manifest.Name, err)
	}

	// Warn if the engine version is older than the plugin's minimum requirement.
	checkEngineCompatibility(manifest, l.engineVersion)

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

	// Record any types this plugin declares as overridable.
	for _, t := range manifest.OverridableTypes {
		l.overridableTypes[t] = true
	}

	// Register module factories — conflict on duplicate type unless override allowed or type is overridable.
	for typeName, factory := range p.ModuleFactories() {
		if _, exists := l.moduleFactories[typeName]; exists {
			if !allowOverride && !l.overridableTypes[typeName] {
				return fmt.Errorf("plugin %q: module type %q already registered", manifest.Name, typeName)
			}
			if l.overridableTypes[typeName] {
				slog.Info("plugin replacing overridable module type", "plugin", manifest.Name, "type", typeName)
			} else {
				slog.Warn("plugin overriding existing module type", "plugin", manifest.Name, "type", typeName)
			}
		}
		l.moduleFactories[typeName] = factory
	}

	// Register step factories — conflict on duplicate type unless override allowed or type is overridable.
	for typeName, factory := range p.StepFactories() {
		if _, exists := l.stepFactories[typeName]; exists {
			if !allowOverride && !l.overridableTypes[typeName] {
				return fmt.Errorf("plugin %q: step type %q already registered", manifest.Name, typeName)
			}
			if l.overridableTypes[typeName] {
				slog.Info("plugin replacing overridable step type", "plugin", manifest.Name, "type", typeName)
			} else {
				slog.Warn("plugin overriding existing step type", "plugin", manifest.Name, "type", typeName)
			}
		}
		l.stepFactories[typeName] = factory
	}

	// Register trigger factories — conflict on duplicate type unless override allowed or type is overridable.
	for typeName, factory := range p.TriggerFactories() {
		if _, exists := l.triggerFactories[typeName]; exists {
			if !allowOverride && !l.overridableTypes[typeName] {
				return fmt.Errorf("plugin %q: trigger type %q already registered", manifest.Name, typeName)
			}
			if l.overridableTypes[typeName] {
				slog.Info("plugin replacing overridable trigger type", "plugin", manifest.Name, "type", typeName)
			} else {
				slog.Warn("plugin overriding existing trigger type", "plugin", manifest.Name, "type", typeName)
			}
		}
		l.triggerFactories[typeName] = factory
	}

	// Register workflow handler factories — conflict on duplicate type unless override allowed or type is overridable.
	for typeName, factory := range p.WorkflowHandlers() {
		if _, exists := l.handlerFactories[typeName]; exists {
			if !allowOverride && !l.overridableTypes[typeName] {
				return fmt.Errorf("plugin %q: workflow handler type %q already registered", manifest.Name, typeName)
			}
			if l.overridableTypes[typeName] {
				slog.Info("plugin replacing overridable workflow handler type", "plugin", manifest.Name, "type", typeName)
			} else {
				slog.Warn("plugin overriding existing workflow handler type", "plugin", manifest.Name, "type", typeName)
			}
		}
		l.handlerFactories[typeName] = factory
	}

	// Register module schemas.
	for _, s := range p.ModuleSchemas() {
		l.schemaRegistry.Register(s)
	}

	// Register step schemas.
	for _, s := range p.StepSchemas() {
		l.stepSchemaRegistry.Register(s)
	}
	// Also load step schemas from manifest (for external plugins with plugin.json).
	if manifest != nil {
		for _, s := range manifest.StepSchemas {
			l.stepSchemaRegistry.Register(s)
		}
	}

	// Collect wiring hooks.
	l.wiringHooks = append(l.wiringHooks, p.WiringHooks()...)

	// Collect config transform hooks.
	l.configTransformHooks = append(l.configTransformHooks, p.ConfigTransformHooks()...)

	// Register deploy targets — conflict on duplicate name unless override allowed or type is overridable.
	for name, target := range p.DeployTargets() {
		if _, exists := l.deployTargets[name]; exists {
			if !allowOverride && !l.overridableTypes[name] {
				return fmt.Errorf("plugin %q: deploy target %q already registered", manifest.Name, name)
			}
			if l.overridableTypes[name] {
				slog.Info("plugin replacing overridable deploy target", "plugin", manifest.Name, "target", name)
			} else {
				slog.Warn("plugin overriding existing deploy target", "plugin", manifest.Name, "target", name)
			}
		}
		l.deployTargets[name] = target
	}

	// Register sidecar providers — conflict on duplicate type unless override allowed or type is overridable.
	for typeName, provider := range p.SidecarProviders() {
		if _, exists := l.sidecarProviders[typeName]; exists {
			if !allowOverride && !l.overridableTypes[typeName] {
				return fmt.Errorf("plugin %q: sidecar provider %q already registered", manifest.Name, typeName)
			}
			if l.overridableTypes[typeName] {
				slog.Info("plugin replacing overridable sidecar provider", "plugin", manifest.Name, "type", typeName)
			} else {
				slog.Warn("plugin overriding existing sidecar provider", "plugin", manifest.Name, "type", typeName)
			}
		}
		l.sidecarProviders[typeName] = provider
	}

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

// StepSchemaRegistry returns the loader's step schema registry.
func (l *PluginLoader) StepSchemaRegistry() *schema.StepSchemaRegistry {
	return l.stepSchemaRegistry
}

// LoadedPlugins returns all successfully loaded plugins in load order.
func (l *PluginLoader) LoadedPlugins() []EnginePlugin {
	out := make([]EnginePlugin, len(l.plugins))
	copy(out, l.plugins)
	return out
}

// DeployTargets returns a defensive copy of all registered deploy targets.
func (l *PluginLoader) DeployTargets() map[string]deploy.DeployTarget {
	out := make(map[string]deploy.DeployTarget, len(l.deployTargets))
	for k, v := range l.deployTargets {
		out[k] = v
	}
	return out
}

// SidecarProviders returns a defensive copy of all registered sidecar providers.
func (l *PluginLoader) SidecarProviders() map[string]deploy.SidecarProvider {
	out := make(map[string]deploy.SidecarProvider, len(l.sidecarProviders))
	for k, v := range l.sidecarProviders {
		out[k] = v
	}
	return out
}

// checkEngineCompatibility warns to stderr if the running engine version is
// older than the plugin's declared minEngineVersion. This is a soft check only
// (no hard failure) to allow testing newer plugins against older engines.
// Skips the check when either version is empty or engineVersion is "dev".
func checkEngineCompatibility(manifest *PluginManifest, engineVersion string) {
	if manifest.MinEngineVersion == "" || engineVersion == "" || engineVersion == "dev" {
		return
	}
	minVer, err := ParseSemver(strings.TrimPrefix(manifest.MinEngineVersion, "v"))
	if err != nil {
		return // malformed minEngineVersion — skip silently
	}
	engVer, err := ParseSemver(strings.TrimPrefix(engineVersion, "v"))
	if err != nil {
		return // malformed engine version — skip silently
	}
	if engVer.Compare(minVer) < 0 {
		slog.Warn("plugin requires newer engine",
			"plugin", manifest.Name,
			"minVersion", manifest.MinEngineVersion,
			"engineVersion", engineVersion)
	}
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
