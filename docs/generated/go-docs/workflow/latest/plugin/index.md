# package plugin

Import path: `github.com/GoCodeAlone/workflow/plugin`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/plugin

## Warnings

None

## Synopsis

Package plugin contains Workflow's plugin manifest, registry, loading, and
installation primitives.

Host-side code normally starts with PluginManifest values loaded from
plugin.json files, then uses Manager, Loader, or Registry implementations to
resolve plugin metadata and executables. Plugin authors usually consume the
higher-level packages under plugin/external/sdk and plugin/sdk instead.

## Functions

### func AutoFetchDeclaredPlugins

AutoFetchDeclaredPlugins iterates the declared external plugins and, for each
with AutoFetch enabled, calls AutoFetchPlugin. If wfctl is not on PATH, a warning
is logged and the plugin is skipped rather than failing startup. Other errors are
logged as warnings but do not abort the remaining plugins.

Callers should invoke this before plugin discovery/loading so that newly
fetched plugins are available in the current startup.

```go
func AutoFetchDeclaredPlugins(decls []AutoFetchDecl, pluginDir string, logger *slog.Logger)
```

### func AutoFetchPlugin

AutoFetchPlugin downloads a plugin from the registry if it's not already installed.
It shells out to wfctl for the actual download/install logic.
version is an optional semver constraint (e.g., ">=0.1.0" or "0.2.0").

```go
func AutoFetchPlugin(pluginName, version, pluginDir string) error
```

### func CheckVersion

CheckVersion checks if a version string satisfies a constraint string.

```go
func CheckVersion(version, constraint string) (bool, error)
```

### func RegisterNativePluginFactory

RegisterNativePluginFactory adds a factory to the global built-in NativePlugin registry.
Call this from init() in plugin packages that provide standalone NativePlugins.

```go
func RegisterNativePluginFactory(f NativePluginFactory)
```

### func SaveManifest

SaveManifest writes a manifest to a JSON file.

```go
func SaveManifest(path string, manifest *PluginManifest) error
```

### func VerifyPluginIntegrity

VerifyPluginIntegrity checks the plugin binary's SHA-256 against the lockfile.
Returns nil if no lockfile exists, no entry for this plugin, or no checksum pinned.

```go
func VerifyPluginIntegrity(pluginDir, pluginName string) error
```

## Types

### type APIHandler

APIHandler serves HTTP endpoints for the plugin registry.

```go
type APIHandler struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewAPIHandler

NewAPIHandler creates a new plugin API handler.

```go
func NewAPIHandler(registry *LocalRegistry, loader *dynamic.Loader) *APIHandler
```

## Methods

### func RegisterRoutes

RegisterRoutes registers the plugin API routes on the given mux.

```go
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux)
```

### type AutoFetchDecl

AutoFetchDecl is the minimum interface the engine passes per declared external plugin.

```go
type AutoFetchDecl struct {
	Name		string
	Version		string
	AutoFetch	bool
}
```

### type BaseEnginePlugin

BaseEnginePlugin provides no-op defaults for all EnginePlugin methods.
Embed this in concrete plugin implementations to only override what you need.

```go
type BaseEnginePlugin struct {
	BaseNativePlugin
	Manifest	PluginManifest
}
```

## Methods

### func Capabilities

Capabilities returns an empty capability list.

```go
func (b *BaseEnginePlugin) Capabilities() []capability.Contract
```

### func ConfigTransformHooks

ConfigTransformHooks returns no config transform hooks.

```go
func (b *BaseEnginePlugin) ConfigTransformHooks() []ConfigTransformHook
```

### func DeployTargets

DeployTargets returns no deploy targets.

```go
func (b *BaseEnginePlugin) DeployTargets() map[string]deploy.DeployTarget
```

### func EngineManifest

EngineManifest returns the plugin manifest.

```go
func (b *BaseEnginePlugin) EngineManifest() *PluginManifest
```

### func ModuleFactories

ModuleFactories returns no module factories.

```go
func (b *BaseEnginePlugin) ModuleFactories() map[string]ModuleFactory
```

### func ModuleSchemas

ModuleSchemas returns no module schemas.

```go
func (b *BaseEnginePlugin) ModuleSchemas() []*schema.ModuleSchema
```

### func SidecarProviders

SidecarProviders returns no sidecar providers.

```go
func (b *BaseEnginePlugin) SidecarProviders() map[string]deploy.SidecarProvider
```

### func StepFactories

StepFactories returns no step factories.

```go
func (b *BaseEnginePlugin) StepFactories() map[string]StepFactory
```

### func StepSchemas

StepSchemas returns no step schemas.

```go
func (b *BaseEnginePlugin) StepSchemas() []*schema.StepSchema
```

### func TriggerFactories

TriggerFactories returns no trigger factories.

```go
func (b *BaseEnginePlugin) TriggerFactories() map[string]TriggerFactory
```

### func WiringHooks

WiringHooks returns no wiring hooks.

```go
func (b *BaseEnginePlugin) WiringHooks() []WiringHook
```

### func WorkflowHandlers

WorkflowHandlers returns no workflow handler factories.

```go
func (b *BaseEnginePlugin) WorkflowHandlers() map[string]WorkflowHandlerFactory
```

### type BaseNativePlugin

BaseNativePlugin provides no-op defaults for all NativePlugin methods.
Embed this in concrete implementations to only override what you need.

```go
type BaseNativePlugin struct {
	PluginName		string
	PluginVersion		string
	PluginDescription	string
}
```

## Methods

### func Dependencies

```go
func (b *BaseNativePlugin) Dependencies() []PluginDependency
```

### func Description

```go
func (b *BaseNativePlugin) Description() string
```

### func Name

```go
func (b *BaseNativePlugin) Name() string
```

### func OnDisable

```go
func (b *BaseNativePlugin) OnDisable(_ PluginContext) error
```

### func OnEnable

```go
func (b *BaseNativePlugin) OnEnable(_ PluginContext) error
```

### func RegisterRoutes

```go
func (b *BaseNativePlugin) RegisterRoutes(_ *http.ServeMux)
```

### func UIPages

```go
func (b *BaseNativePlugin) UIPages() []UIPageDef
```

### func Version

```go
func (b *BaseNativePlugin) Version() string
```

### type CapabilityDecl

CapabilityDecl declares a capability relationship for a plugin in the manifest.

```go
type CapabilityDecl struct {
	Name		string	`json:"name" yaml:"name"`
	Role		string	`json:"role" yaml:"role"`	// "provider" or "consumer"
	Priority	int	`json:"priority,omitempty" yaml:"priority,omitempty"`
}
```

### type CompositeRegistry

CompositeRegistry combines a local registry with a remote registry,
searching both and allowing installation from the remote into local.

```go
type CompositeRegistry struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewCompositeRegistry

NewCompositeRegistry creates a composite registry from a local and remote registry.

```go
func NewCompositeRegistry(local *LocalRegistry, remote *RemoteRegistry) *CompositeRegistry
```

## Methods

### func CheckDependencies

CheckDependencies delegates to the local registry.

```go
func (c *CompositeRegistry) CheckDependencies(manifest *PluginManifest) error
```

### func Get

Get delegates to the local registry.

```go
func (c *CompositeRegistry) Get(name string) (*PluginEntry, bool)
```

### func Install

Install downloads a plugin from the remote registry and registers it locally.

```go
func (c *CompositeRegistry) Install(ctx context.Context, name, version string) error
```

### func List

List delegates to the local registry.

```go
func (c *CompositeRegistry) List() []*PluginEntry
```

### func Local

Local returns the underlying local registry.

```go
func (c *CompositeRegistry) Local() *LocalRegistry
```

### func Register

Register delegates to the local registry.

```go
func (c *CompositeRegistry) Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error
```

### func Remote

Remote returns the underlying remote registry.

```go
func (c *CompositeRegistry) Remote() *RemoteRegistry
```

### func Search

Search checks local first, then remote, merging results with no duplicates.

```go
func (c *CompositeRegistry) Search(ctx context.Context, query string) ([]*PluginManifest, error)
```

### func Unregister

Unregister delegates to the local registry.

```go
func (c *CompositeRegistry) Unregister(name string) error
```

### type ConfigTransformHook

ConfigTransformHook runs BEFORE module registration in BuildFromConfig,
allowing plugins to inject modules, workflows, and triggers into the config.

```go
type ConfigTransformHook struct {
	Name		string
	Priority	int	// higher priority runs first
	Hook		func(cfg *config.WorkflowConfig) error
}
```

### type Constraint

Constraint represents a semver constraint that can check version compatibility.

```go
type Constraint struct {
	Op	string
	Version	Semver
}
```

## Functions

### func ParseConstraint

ParseConstraint parses a constraint string like ">=1.0.0", "^2.1.0", "~1.2.0".

```go
func ParseConstraint(s string) (*Constraint, error)
```

## Methods

### func Check

Check returns true if the given version satisfies the constraint.

```go
func (c *Constraint) Check(v Semver) bool
```

### type CosignVerifier

CosignVerifier verifies plugin binaries using cosign keyless signatures.
It requires the cosign CLI to be installed; if not found, verification is
skipped with a warning to support environments without cosign installed.

```go
type CosignVerifier struct {
	OIDCIssuer		string
	AllowedIdentityRegexp	string
}
```

## Functions

### func NewCosignVerifier

NewCosignVerifier creates a CosignVerifier for the given OIDC issuer and
identity regexp (e.g. "https://github.com/GoCodeAlone/.*").

```go
func NewCosignVerifier(oidcIssuer, identityRegexp string) *CosignVerifier
```

## Methods

### func Verify

Verify runs `cosign verify-blob` to validate the signature of a plugin binary.
If cosign is not installed, a warning is logged and nil is returned so that
deployments without cosign are not broken.

```go
func (v *CosignVerifier) Verify(binaryPath, sigPath, certPath string) error
```

### type Dependency

Dependency declares a versioned dependency on another plugin.

```go
type Dependency struct {
	Name		string	`json:"name" yaml:"name"`
	Constraint	string	`json:"constraint" yaml:"constraint"`	// semver constraint, e.g. ">=1.0.0", "^2.1"
}
```

### type EmbeddedWorkflow

EmbeddedWorkflow describes a workflow contributed by a plugin.

```go
type EmbeddedWorkflow struct {
	Name		string					`json:"name"`
	Description	string					`json:"description"`
	Config		*config.WorkflowConfig			`json:"-"`
	ConfigYAML	string					`json:"configYaml,omitempty"`
	InputSchema	map[string]schema.ConfigFieldDef	`json:"inputSchema,omitempty"`
	OutputSchema	map[string]schema.ConfigFieldDef	`json:"outputSchema,omitempty"`
}
```

### type EnginePlugin

EnginePlugin extends NativePlugin with engine-level contributions:
module type factories, step type factories, trigger factories,
workflow handlers, capability contracts, and wiring hooks.

```go
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

	// StepSchemas returns schema definitions for this plugin's pipeline step types.
	StepSchemas() []*schema.StepSchema

	// WiringHooks returns post-init wiring functions.
	WiringHooks() []WiringHook

	// ConfigTransformHooks returns hooks that run before module registration in BuildFromConfig.
	ConfigTransformHooks() []ConfigTransformHook

	// DeployTargets returns deploy target implementations (e.g., "kubernetes", "ecs").
	DeployTargets() map[string]deploy.DeployTarget

	// SidecarProviders returns sidecar provider implementations (e.g., "sidecar.tailscale").
	SidecarProviders() map[string]deploy.SidecarProvider
}
```

### type EnginePluginManager

EnginePluginManager wraps the PluginLoader with lifecycle management,
allowing plugins to be registered, enabled, and disabled independently.

```go
type EnginePluginManager struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewEnginePluginManager

NewEnginePluginManager creates a new manager backed by the given capability and schema registries.

```go
func NewEnginePluginManager(capReg *capability.Registry, schemaReg *schema.ModuleSchemaRegistry) *EnginePluginManager
```

## Methods

### func Disable

Disable deactivates a plugin. The plugin remains registered but its factories
are no longer active. Note: a full rebuild of the loader is performed to
remove the plugin's contributions.

```go
func (m *EnginePluginManager) Disable(name string) error
```

### func Enable

Enable activates a registered plugin, loading it into the PluginLoader.
Returns an error if the plugin is not registered or is already enabled.

```go
func (m *EnginePluginManager) Enable(name string) error
```

### func Get

Get returns the plugin with the given name and a boolean indicating whether it exists.

```go
func (m *EnginePluginManager) Get(name string) (EnginePlugin, bool)
```

### func IsEnabled

IsEnabled returns true if the named plugin is currently enabled.

```go
func (m *EnginePluginManager) IsEnabled(name string) bool
```

### func List

List returns all registered plugins (enabled and disabled).

```go
func (m *EnginePluginManager) List() []EnginePlugin
```

### func Loader

Loader returns the underlying PluginLoader for accessing aggregated factories and hooks.

```go
func (m *EnginePluginManager) Loader() *PluginLoader
```

### func Register

Register adds a plugin to the manager without enabling it.
Returns an error if a plugin with the same name is already registered.

```go
func (m *EnginePluginManager) Register(p EnginePlugin) error
```

### func ResolveWorkflowDependencies

ResolveWorkflowDependencies checks that all capabilities required by a
workflow config are satisfied by loaded plugins. If cfg.Requires is nil,
returns nil (auto-detection will be added later). For each required
capability, checks capabilityReg.HasProvider(). Returns
MissingCapabilitiesError if any are missing.

```go
func (m *EnginePluginManager) ResolveWorkflowDependencies(cfg *config.WorkflowConfig) error
```

### type IaCProviderCapability

IaCProviderCapability declares IaC provider metadata in a plugin manifest.

```go
type IaCProviderCapability struct {
	Name			string		`json:"name,omitempty" yaml:"name,omitempty"`
	ResourceTypes		[]string	`json:"resourceTypes,omitempty" yaml:"resourceTypes,omitempty"`
	ComputePlanVersion	string		`json:"computePlanVersion,omitempty" yaml:"computePlanVersion,omitempty"`
}
```

### type IaCStateBackendProvider

IaCStateBackendProvider is the optional interface an external-plugin adapter
implements when its plugin serves one or more iac.state backends. The engine
type-asserts loaded plugins against it (same pattern as stepRegistrySetter)
and populates module's iac.state backend registry.

```go
type IaCStateBackendProvider interface {
	IaCStateBackendClients() (map[string]proto.IaCStateBackendClient, error)
}
```

### type KubernetesBackendProvider

KubernetesBackendProvider is the optional interface an external-plugin adapter
implements when its plugin serves one or more platform.kubernetes cluster-type
backends (e.g. "gke"). The engine type-asserts loaded plugins against it (same
pattern as IaCStateBackendProvider) and populates module's kubernetes backend
registry.

Per ADR 0037 a kubernetes backend is served over the existing ResourceDriver
contract — no new proto surface — so the returned clients are
proto.ResourceDriverClient values keyed by cluster type name.

```go
type KubernetesBackendProvider interface {
	KubernetesBackendClients() (map[string]proto.ResourceDriverClient, error)
}
```

### type LicenseValidator

LicenseValidator is an optional service that approves or denies premium plugin usage.
If registered under the name "license-validator", the loader will call it during
tier validation for premium plugins.

```go
type LicenseValidator interface {
	// ValidatePlugin returns nil if the named plugin is licensed for use.
	ValidatePlugin(pluginName string) error
}
```

### type LocalRegistry

LocalRegistry implements PluginRegistry by scanning local directories.

```go
type LocalRegistry struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewLocalRegistry

NewLocalRegistry creates a new empty local registry.

```go
func NewLocalRegistry() *LocalRegistry
```

## Methods

### func CheckDependencies

CheckDependencies verifies that all dependencies declared in the manifest
are satisfied by currently registered plugins.

```go
func (r *LocalRegistry) CheckDependencies(manifest *PluginManifest) error
```

### func Get

Get retrieves a plugin entry by name.

```go
func (r *LocalRegistry) Get(name string) (*PluginEntry, bool)
```

### func List

List returns all registered plugin entries.

```go
func (r *LocalRegistry) List() []*PluginEntry
```

### func Register

Register adds a plugin to the registry after validating its manifest
and checking version compatibility of declared dependencies.

```go
func (r *LocalRegistry) Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error
```

### func ScanDirectory

ScanDirectory scans a directory for plugin subdirectories.
Each subdirectory should contain a plugin.json manifest and a .go source file.

```go
func (r *LocalRegistry) ScanDirectory(dir string, loader *dynamic.Loader) ([]*PluginEntry, error)
```

### func Unregister

Unregister removes a plugin from the registry.

```go
func (r *LocalRegistry) Unregister(name string) error
```

### type MissingCapabilitiesError

MissingCapabilitiesError is returned when a workflow config requires capabilities
that no loaded plugin provides.

```go
type MissingCapabilitiesError struct {
	Missing []string
}
```

## Methods

### func Error

```go
func (e *MissingCapabilitiesError) Error() string
```

### type ModernizeRulesProvider

ModernizeRulesProvider is optionally implemented by EnginePlugins that
wish to supply custom modernize rules for the wfctl modernize command.
The rules returned by this interface are intended to be collected by the
engine or tooling and merged with the core built-in rules when
modernizing workflow configs that use this plugin's module/step types.

External (process-isolated) plugins declare their rules in plugin.json
under the "modernizeRules" key and have them loaded automatically via
modernize.LoadRulesFromDir. In-process Go plugins can implement this
interface to expose function-based rules that perform arbitrary YAML
transforms. At present this interface serves as an API/extension point;
integrating it into a particular engine or CLI is up to that consumer.

```go
type ModernizeRulesProvider interface {
	ModernizeRules() []modernize.Rule
}
```

### type ModuleFactory

ModuleFactory creates a modular.Module from a name and config map.

```go
type ModuleFactory func(name string, config map[string]any) modular.Module
```

### type NativeHandler

NativeHandler serves HTTP endpoints for native plugin discovery and route dispatch.
It delegates all behavior to the PluginManager.

```go
type NativeHandler struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewNativeHandler

NewNativeHandler creates a new handler backed by a PluginManager.

```go
func NewNativeHandler(manager *PluginManager) *NativeHandler
```

## Methods

### func ServeHTTP

ServeHTTP implements http.Handler by delegating to the PluginManager.

```go
func (h *NativeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

### type NativePlugin

NativePlugin is a compiled-in plugin that provides HTTP handlers, UI page metadata,
and lifecycle hooks with dependency declarations.

```go
type NativePlugin interface {
	Name() string
	Version() string
	Description() string
	Dependencies() []PluginDependency
	UIPages() []UIPageDef
	RegisterRoutes(mux *http.ServeMux)
	OnEnable(ctx PluginContext) error
	OnDisable(ctx PluginContext) error
}
```

## Functions

### func BuiltinNativePlugins

BuiltinNativePlugins creates all registered built-in NativePlugins.
The db parameter is the shared database connection. Additional dependencies
are passed as key-value pairs in the deps map. Plugins that return nil
are skipped.

```go
func BuiltinNativePlugins(db *sql.DB, deps map[string]any) []NativePlugin
```

### type NativePluginFactory

NativePluginFactory creates a NativePlugin given a database connection and
a set of optional dependencies keyed by name. The factory may return nil
if its prerequisites are not met (e.g., no database available).

```go
type NativePluginFactory func(db *sql.DB, deps map[string]any) NativePlugin
```

### type NativePluginProvider

NativePluginProvider is optionally implemented by EnginePlugins that also
contribute NativePlugins (e.g., for Marketplace visibility, UI pages, or
HTTP route handlers). The PluginContext provides shared resources (DB, logger).

```go
type NativePluginProvider interface {
	NativePlugins(ctx PluginContext) []NativePlugin
}
```

### type PipelineTriggerConfigProvider

PipelineTriggerConfigProvider is optionally implemented by EnginePlugins that
register triggers. It provides config wrapper functions that convert flat
pipeline trigger config (e.g., {path, method}) into the trigger's native
configuration format (e.g., {routes: [{...}]}).

```go
type PipelineTriggerConfigProvider interface {
	PipelineTriggerConfigWrappers() map[string]TriggerConfigWrapperFunc
}
```

### type PluginContext

PluginContext provides shared resources to plugins during lifecycle events.

```go
type PluginContext struct {
	App	interface{}	// modular.Application — use interface{} to avoid import cycle
	DB	*sql.DB
	Logger	*slog.Logger
	DataDir	string
}
```

### type PluginDependency

PluginDependency declares a dependency on another plugin.

```go
type PluginDependency struct {
	Name		string	`json:"name"`		// required plugin name
	MinVersion	string	`json:"minVersion"`	// semver constraint, empty = any version
}
```

### type PluginEntry

PluginEntry holds the manifest and component for a registered plugin.

```go
type PluginEntry struct {
	Manifest	*PluginManifest			`json:"manifest"`
	Component	*dynamic.DynamicComponent	`json:"-"`
	SourceDir	string				`json:"source_dir,omitempty"`
}
```

### type PluginInfo

PluginInfo is the JSON representation of a plugin for API responses.
JSON field names use camelCase to match the TypeScript UI conventions.

```go
type PluginInfo struct {
	Name		string			`json:"name"`
	Version		string			`json:"version"`
	Description	string			`json:"description"`
	Enabled		bool			`json:"enabled"`
	UIPages		[]UIPageDef		`json:"uiPages"`
	Dependencies	[]PluginDependency	`json:"dependencies"`
	EnabledAt	string			`json:"enabledAt,omitempty"`
	DisabledAt	string			`json:"disabledAt,omitempty"`
}
```

### type PluginInstaller

PluginInstaller handles installing plugins from remote or local sources.

```go
type PluginInstaller struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewPluginInstaller

NewPluginInstaller creates a new plugin installer.

```go
func NewPluginInstaller(remoteReg *RemoteRegistry, localReg *LocalRegistry, loader *dynamic.Loader, installDir string) *PluginInstaller
```

## Methods

### func Install

Install downloads and installs a plugin from the remote registry.

```go
func (i *PluginInstaller) Install(ctx context.Context, name, version string) error
```

### func InstallDir

InstallDir returns the configured plugin installation directory.

```go
func (i *PluginInstaller) InstallDir() string
```

### func InstallFromBundle

InstallFromBundle installs a plugin from a local bundle directory.
The bundle directory must contain a plugin.json manifest.

```go
func (i *PluginInstaller) InstallFromBundle(bundlePath string) error
```

### func IsInstalled

IsInstalled checks if a plugin is installed locally.

```go
func (i *PluginInstaller) IsInstalled(name string) bool
```

### func ScanInstalled

ScanInstalled loads all previously installed plugins from the install directory.

```go
func (i *PluginInstaller) ScanInstalled() ([]*PluginEntry, error)
```

### func Uninstall

Uninstall removes an installed plugin.

```go
func (i *PluginInstaller) Uninstall(name string) error
```

### type PluginLoader

PluginLoader loads EnginePlugins and populates registries.

```go
type PluginLoader struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewPluginLoader

NewPluginLoader creates a new PluginLoader backed by the given capability and schema registries.

```go
func NewPluginLoader(capReg *capability.Registry, schemaReg *schema.ModuleSchemaRegistry, stepSchemaReg ...*schema.StepSchemaRegistry) *PluginLoader
```

## Methods

### func CapabilityRegistry

CapabilityRegistry returns the loader's capability registry.

```go
func (l *PluginLoader) CapabilityRegistry() *capability.Registry
```

### func ConfigTransformHooks

ConfigTransformHooks returns all registered config transform hooks sorted by priority (highest first).

```go
func (l *PluginLoader) ConfigTransformHooks() []ConfigTransformHook
```

### func DeployTargets

DeployTargets returns a defensive copy of all registered deploy targets.

```go
func (l *PluginLoader) DeployTargets() map[string]deploy.DeployTarget
```

### func LoadBinaryPlugin

LoadBinaryPlugin verifies a plugin binary with cosign (for premium plugins) and
then loads the plugin into the registry. binaryPath, sigPath, and certPath are
paths to the plugin binary, cosign signature file, and certificate file
respectively. If cosignVerifier is nil, verification is skipped.

```go
func (l *PluginLoader) LoadBinaryPlugin(p EnginePlugin, binaryPath, sigPath, certPath string) error
```

### func LoadBinaryPluginWithOverride

LoadBinaryPluginWithOverride is the override-capable counterpart to
LoadBinaryPlugin. It verifies the plugin binary with cosign (for premium
plugins) and then loads the plugin, allowing it to override existing module,
step, trigger, handler, deploy target, and sidecar provider registrations.
When a duplicate type is encountered, the new factory replaces the previous
one and a warning is logged instead of returning an error.

```go
func (l *PluginLoader) LoadBinaryPluginWithOverride(p EnginePlugin, binaryPath, sigPath, certPath string) error
```

### func LoadPlugin

LoadPlugin validates a plugin's manifest, registers its capabilities, factories,
schemas, and wiring hooks. Returns an error if any factory type conflicts with
an existing registration.

```go
func (l *PluginLoader) LoadPlugin(p EnginePlugin) error
```

### func LoadPluginWithOverride

LoadPluginWithOverride is like LoadPlugin but allows the plugin to override
existing module, step, trigger, handler, deploy target, and sidecar provider
registrations. When a duplicate type is encountered, the new factory replaces
the previous one and a warning is logged instead of returning an error.
This is intended for external plugins that intentionally replace built-in
defaults (e.g., replacing a mock authz step with a production implementation).

```go
func (l *PluginLoader) LoadPluginWithOverride(p EnginePlugin) error
```

### func LoadPlugins

LoadPlugins performs a topological sort of plugins by their manifest dependencies,
then loads each in order. Returns an error on circular dependencies or load failures.

```go
func (l *PluginLoader) LoadPlugins(plugins []EnginePlugin) error
```

### func LoadedPlugins

LoadedPlugins returns all successfully loaded plugins in load order.

```go
func (l *PluginLoader) LoadedPlugins() []EnginePlugin
```

### func ModuleFactories

ModuleFactories returns a defensive copy of all registered module factories.

```go
func (l *PluginLoader) ModuleFactories() map[string]ModuleFactory
```

### func OverridableTypes

OverridableTypes returns a read-only copy of all type names that have been
declared overridable by loaded plugins.

```go
func (l *PluginLoader) OverridableTypes() map[string]bool
```

### func SetCosignVerifier

SetCosignVerifier registers a cosign verifier for binary signature verification
of premium plugins. When set, LoadBinaryPlugin will verify the plugin binary
before loading it.

```go
func (l *PluginLoader) SetCosignVerifier(v *CosignVerifier)
```

### func SetEngineVersion

SetEngineVersion sets the running engine version used for minEngineVersion
compatibility checks when loading plugins.

```go
func (l *PluginLoader) SetEngineVersion(v string)
```

### func SetLicenseValidator

SetLicenseValidator registers a license validator used for premium tier plugins.

```go
func (l *PluginLoader) SetLicenseValidator(v LicenseValidator)
```

### func SidecarProviders

SidecarProviders returns a defensive copy of all registered sidecar providers.

```go
func (l *PluginLoader) SidecarProviders() map[string]deploy.SidecarProvider
```

### func StepFactories

StepFactories returns a defensive copy of all registered step factories.

```go
func (l *PluginLoader) StepFactories() map[string]StepFactory
```

### func StepSchemaRegistry

StepSchemaRegistry returns the loader's step schema registry.

```go
func (l *PluginLoader) StepSchemaRegistry() *schema.StepSchemaRegistry
```

### func TriggerFactories

TriggerFactories returns a defensive copy of all registered trigger factories.

```go
func (l *PluginLoader) TriggerFactories() map[string]TriggerFactory
```

### func ValidateTier

license validator configuration:
  - Core and Community plugins are always allowed.
  - Premium plugins are validated against the LicenseValidator if one is set.
    If no validator is configured, a warning is logged and the plugin is allowed
    (graceful degradation for self-hosted deployments without a license).

```go
func (l *PluginLoader) ValidateTier(manifest *PluginManifest) error
```

### func WiringHooks

WiringHooks returns all registered wiring hooks sorted by priority (highest first).

```go
func (l *PluginLoader) WiringHooks() []WiringHook
```

### func WorkflowHandlerFactories

WorkflowHandlerFactories returns a defensive copy of all registered workflow handler factories.

```go
func (l *PluginLoader) WorkflowHandlerFactories() map[string]WorkflowHandlerFactory
```

### type PluginManager

PluginManager handles plugin registration, dependency resolution, lifecycle management,
enable/disable state persistence, and HTTP route dispatch.

```go
type PluginManager struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewPluginManager

NewPluginManager creates a new PluginManager with SQLite-backed state persistence.
It initializes the plugin_state table if it does not exist.

```go
func NewPluginManager(db *sql.DB, logger *slog.Logger) *PluginManager
```

## Methods

### func AllPlugins

AllPlugins returns info about all registered plugins sorted by name.

```go
func (pm *PluginManager) AllPlugins() []PluginInfo
```

### func Disable

Disable disables a plugin and all plugins that depend on it (reverse dependency order).
Returns an error if the plugin is not registered.

```go
func (pm *PluginManager) Disable(name string) error
```

### func Enable

Enable enables a plugin and all its unsatisfied dependencies (topological order).
Returns an error if the plugin is not registered, if a dependency is missing,
or if a circular dependency is detected.

```go
func (pm *PluginManager) Enable(name string) error
```

### func EnabledPlugins

EnabledPlugins returns all currently enabled plugins sorted by name.

```go
func (pm *PluginManager) EnabledPlugins() []NativePlugin
```

### func IsEnabled

IsEnabled returns whether a plugin is currently enabled.

```go
func (pm *PluginManager) IsEnabled(name string) bool
```

### func Register

Register adds a plugin to the known set. It does not enable the plugin.
Returns an error if a plugin with the same name is already registered.

```go
func (pm *PluginManager) Register(p NativePlugin) error
```

### func RestoreState

RestoreState re-enables all plugins that were previously enabled (from the plugin_state table).
This is called after an engine restart or reload.

```go
func (pm *PluginManager) RestoreState() error
```

### func ServeHTTP

ServeHTTP dispatches HTTP requests to the correct plugin's mux.
Route pattern: /api/v1/admin/plugins/{name}/{path...}
Returns 404 if the plugin is not found or not enabled.

```go
func (pm *PluginManager) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

### func SetContext

SetContext sets the shared PluginContext used for OnEnable/OnDisable calls.

```go
func (pm *PluginManager) SetContext(ctx PluginContext)
```

### type PluginManifest

PluginManifest describes a plugin's metadata, dependencies, and contract.

```go
type PluginManifest struct {
	Name		string			`json:"name" yaml:"name"`
	Version		string			`json:"version" yaml:"version"`
	Author		string			`json:"author" yaml:"author"`
	Description	string			`json:"description" yaml:"description"`
	License		string			`json:"license,omitempty" yaml:"license,omitempty"`
	Dependencies	[]Dependency		`json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Contract	*dynamic.FieldContract	`json:"contract,omitempty" yaml:"contract,omitempty"`
	Tags		[]string		`json:"tags,omitempty" yaml:"tags,omitempty"`
	Repository	string			`json:"repository,omitempty" yaml:"repository,omitempty"`
	Tier		PluginTier		`json:"tier,omitempty" yaml:"tier,omitempty"`

	// Engine plugin declarations
	Capabilities	[]CapabilityDecl	`json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ModuleTypes	[]string		`json:"moduleTypes,omitempty" yaml:"moduleTypes,omitempty"`
	StepTypes	[]string		`json:"stepTypes,omitempty" yaml:"stepTypes,omitempty"`
	TriggerTypes	[]string		`json:"triggerTypes,omitempty" yaml:"triggerTypes,omitempty"`
	WorkflowTypes	[]string		`json:"workflowTypes,omitempty" yaml:"workflowTypes,omitempty"`
	WiringHooks	[]string		`json:"wiringHooks,omitempty" yaml:"wiringHooks,omitempty"`

	// IaCStateBackends lists the iac.state backend names this plugin serves
	// (e.g. "azure_blob"). Authored in plugin.json either as a top-level
	// "iacStateBackends" key or nested under the legacy capabilities object
	// as capabilities.iacStateBackends (UnmarshalJSON's object branch promotes
	// the nested form here, same as ModuleTypes/StepTypes/etc.). The engine
	// cross-checks these against the plugin's ListBackendNames RPC. Amendment
	// A2 (decisions/0035).
	IaCStateBackends	[]string	`json:"iacStateBackends,omitempty" yaml:"iacStateBackends,omitempty"`

	// IaCServices lists the typed IaC service names this plugin serves
	// (fully-qualified gRPC service names, e.g.
	// "workflow.plugin.external.iac.IaCProviderRequired"). Authored in
	// plugin.json either as a top-level "iacServices" key OR nested under
	// "capabilities.iacServices" (UnmarshalJSON's object branch promotes
	// the nested form, same as IaCStateBackends). The engine cross-checks
	// these against the plugin's runtime ContractRegistry via wfctl plugin
	// verify-capabilities (workflow#767).
	//
	// Orthogonal to IaCStateBackends (which lists backend NAMES, not gRPC
	// service names). A plugin that registers the IaCStateBackend service
	// AND lists its backend names will appear in BOTH manifest fields.
	IaCServices	[]string	`json:"iacServices,omitempty" yaml:"iacServices,omitempty"`

	// IaCProvider declares provider-specific IaC metadata from plugin.json.
	// Older manifests may author this under capabilities.iacProvider; the
	// custom unmarshaller promotes that legacy shape here so callers can rely
	// on one field.
	IaCProvider	IaCProviderCapability	`json:"iacProvider,omitempty" yaml:"iacProvider,omitempty"`

	// StepSchemas provides schema definitions for step types registered by this plugin.
	// Used by MCP/LSP for hover docs, completions, and output documentation.
	StepSchemas	[]*schema.StepSchema	`json:"stepSchemas,omitempty" yaml:"stepSchemas,omitempty"`

	// ModernizeRules declares migration rules for the wfctl modernize command.
	// Each rule describes a common migration pattern (type renames, config key
	// renames) that users of this plugin may need to apply when upgrading.
	// These rules are loaded automatically when --plugin-dir is passed to
	// wfctl modernize or wfctl mcp.
	ModernizeRules	[]modernize.ManifestRule	`json:"modernizeRules,omitempty" yaml:"modernizeRules,omitempty"`

	// OverridableTypes lists type names (modules, steps, triggers, handlers) that may be
	// overridden by later-loaded plugins without requiring LoadPluginWithOverride.
	// Typically used for placeholder/mock implementations.
	OverridableTypes	[]string	`json:"overridableTypes,omitempty" yaml:"overridableTypes,omitempty"`

	// Config mutability and sample plugin support
	ConfigMutable	bool	`json:"configMutable,omitempty" yaml:"configMutable,omitempty"`
	SampleCategory	string	`json:"sampleCategory,omitempty" yaml:"sampleCategory,omitempty"`

	// MinEngineVersion declares the minimum engine version required to run this plugin.
	// A semver string without the "v" prefix, e.g. "0.3.30".
	MinEngineVersion	string	`json:"minEngineVersion,omitempty" yaml:"minEngineVersion,omitempty"`
}
```

## Functions

### func LoadManifest

LoadManifest reads a manifest from a JSON file.

```go
func LoadManifest(path string) (*PluginManifest, error)
```

## Methods

### func UnmarshalJSON

UnmarshalJSON implements custom JSON unmarshalling for PluginManifest that
handles both the canonical capabilities array format and the legacy object
format used by registry manifests and older plugin.json files.

Legacy format: "capabilities": {"configProvider": bool, "moduleTypes": [...], ...}
New format:    "capabilities": [{"name": "...", "role": "..."}]

When the legacy object format is detected, its type lists are merged into the
top-level ModuleTypes, StepTypes, and TriggerTypes fields so callers always
find types in a consistent location. Any other JSON type (string, number,
bool) is rejected with a descriptive error.

```go
func (m *PluginManifest) UnmarshalJSON(data []byte) error
```

### func Validate

Validate checks that a manifest has all required fields and valid semver.

```go
func (m *PluginManifest) Validate() error
```

### type PluginRegistry

PluginRegistry manages plugin registration and lookup.

```go
type PluginRegistry interface {
	Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error
	Unregister(name string) error
	Get(name string) (*PluginEntry, bool)
	List() []*PluginEntry
	CheckDependencies(manifest *PluginManifest) error
}
```

### type PluginTier

PluginTier indicates the support and licensing tier for a plugin.

```go
type PluginTier string
```

### type PluginWorkflowRegistry

PluginWorkflowRegistry stores embedded workflows contributed by plugins.
Workflows are keyed by qualified name: "plugin-name:workflow-name".

```go
type PluginWorkflowRegistry struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewPluginWorkflowRegistry

NewPluginWorkflowRegistry creates an empty PluginWorkflowRegistry.

```go
func NewPluginWorkflowRegistry() *PluginWorkflowRegistry
```

## Methods

### func Get

Get retrieves a workflow by its qualified name ("plugin:workflow").

```go
func (r *PluginWorkflowRegistry) Get(qualifiedName string) (*EmbeddedWorkflow, bool)
```

### func List

List returns all registered qualified workflow names, sorted.

```go
func (r *PluginWorkflowRegistry) List() []string
```

### func Register

Register adds an embedded workflow under the given plugin name.

```go
func (r *PluginWorkflowRegistry) Register(pluginName string, wf EmbeddedWorkflow) error
```

### func Unregister

Unregister removes all workflows belonging to the given plugin.

```go
func (r *PluginWorkflowRegistry) Unregister(pluginName string)
```

### type RegistryHandler

RegistryHandler provides HTTP API handlers for plugin management.

```go
type RegistryHandler struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewRegistryHandler

NewRegistryHandler creates a new registry handler backed by the given composite registry.

```go
func NewRegistryHandler(registry *CompositeRegistry) *RegistryHandler
```

## Methods

### func RegisterRoutes

RegisterRoutes registers plugin management HTTP routes on the given mux.

```go
func (h *RegistryHandler) RegisterRoutes(mux *http.ServeMux)
```

### type RemoteRegistry

RemoteRegistry discovers and downloads plugins from a remote HTTP registry.

```go
type RemoteRegistry struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewRemoteRegistry

NewRemoteRegistry creates a new remote registry client.

```go
func NewRemoteRegistry(baseURL string, opts ...RemoteRegistryOption) *RemoteRegistry
```

## Methods

### func ClearCache

ClearCache clears the in-memory manifest cache.

```go
func (r *RemoteRegistry) ClearCache()
```

### func Download

Download retrieves the plugin archive for a specific version.

```go
func (r *RemoteRegistry) Download(ctx context.Context, name, version string) (io.ReadCloser, error)
```

### func GetManifest

GetManifest retrieves the manifest for a specific plugin version from the remote registry.

```go
func (r *RemoteRegistry) GetManifest(ctx context.Context, name, version string) (*PluginManifest, error)
```

### func ListVersions

ListVersions retrieves available versions for a plugin from the remote registry.

```go
func (r *RemoteRegistry) ListVersions(ctx context.Context, name string) ([]string, error)
```

### func Search

Search queries the remote registry for plugins matching the given query string.

```go
func (r *RemoteRegistry) Search(ctx context.Context, query string) ([]*PluginManifest, error)
```

### type RemoteRegistryOption

RemoteRegistryOption configures a RemoteRegistry.

```go
type RemoteRegistryOption func(*RemoteRegistry)
```

## Functions

### func WithCacheTTL

WithCacheTTL sets how long cached manifests remain valid.

```go
func WithCacheTTL(ttl time.Duration) RemoteRegistryOption
```

### func WithHTTPClient

WithHTTPClient sets the HTTP client used by the remote registry.

```go
func WithHTTPClient(client *http.Client) RemoteRegistryOption
```

### type Semver

Semver represents a parsed semantic version.

```go
type Semver struct {
	Major	int
	Minor	int
	Patch	int
}
```

## Functions

### func ParseSemver

ParseSemver parses a version string like "1.2.3" into a Semver.

```go
func ParseSemver(v string) (Semver, error)
```

## Methods

### func Compare

Compare returns -1, 0, or 1.

```go
func (s Semver) Compare(other Semver) int
```

### func String

```go
func (s Semver) String() string
```

### type StepFactory

StepFactory creates a pipeline step from config.
The returned value should implement the PipelineStep interface
(module.PipelineStep). We use any here to avoid a circular import
on the module package. The app parameter provides access to the
modular.Application service registry for steps that need it
(e.g., db_exec, db_query, delegate).

```go
type StepFactory func(name string, config map[string]any, app modular.Application) (any, error)
```

### type TriggerConfigWrapperFunc

TriggerConfigWrapperFunc converts flat pipeline trigger config into the
trigger's native configuration format (e.g., wrapping {path, method} into
{routes: [{...}]} for HTTP triggers).

```go
type TriggerConfigWrapperFunc func(pipelineName string, flatConfig map[string]any) map[string]any
```

### type TriggerFactory

TriggerFactory creates a trigger instance.
The returned value should implement the Trigger interface
(module.Trigger: Name, Start, Stop, Configure).

```go
type TriggerFactory func() any
```

### type UIPageDef

UIPageDef describes a UI page contributed by a plugin.

```go
type UIPageDef struct {
	ID			string	`json:"id"`
	Label			string	`json:"label"`
	Icon			string	`json:"icon"`
	Category		string	`json:"category"`	// "global", "workflow", "plugin"
	Order			int	`json:"order"`
	RequiredRole		string	`json:"requiredRole,omitempty"`		// minimum role: "viewer", "editor", "admin", "operator"
	RequiredPermission	string	`json:"requiredPermission,omitempty"`	// specific permission key, e.g. "plugins.manage"
	APIEndpoint		string	`json:"apiEndpoint,omitempty"`		// JSON data source for template pages
	Template		string	`json:"template,omitempty"`		// predefined template: "data-table", "chart-dashboard", "form", "detail-view"
}
```

### type WiringHook

WiringHook is called after module initialization to wire cross-module integrations.

```go
type WiringHook struct {
	Name		string
	Priority	int	// higher priority runs first
	Hook		func(app modular.Application, cfg *config.WorkflowConfig) error
}
```

### type WorkflowHandlerFactory

WorkflowHandlerFactory creates a workflow handler instance.
The returned value should implement WorkflowHandler
(CanHandle, ConfigureWorkflow, ExecuteWorkflow).

```go
type WorkflowHandlerFactory func() any
```

### type WorkflowPlugin

WorkflowPlugin extends NativePlugin with the ability to contribute
embedded workflows that can be invoked as sub-workflows from pipelines.

```go
type WorkflowPlugin interface {
	NativePlugin
	EmbeddedWorkflows() []EmbeddedWorkflow
}
```

