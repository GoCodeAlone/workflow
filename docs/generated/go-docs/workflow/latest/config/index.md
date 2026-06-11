# package config

Import path: `github.com/GoCodeAlone/workflow/config`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/config

## Warnings

None

## Functions

### func CrossValidate

CrossValidate performs cross-section warnings between services, networking, and mesh.
It returns warnings (non-fatal) as a string slice.

```go
func CrossValidate(cfg *WorkflowConfig) []string
```

### func DesiredEnvironmentNames

DesiredEnvironmentNames returns the environment names declared as desired
state in workflow config. Runtime placeholders such as ${WORKFLOW_ENV} are
intentionally skipped because they are resolved outside static config.

```go
func DesiredEnvironmentNames(cfg *WorkflowConfig) []string
```

### func ExpandEnvInMap

ExpandEnvInMap returns a deep copy of m with every string value having
${VAR} and $VAR references resolved via os.ExpandEnv. Nested map[string]any
and []any values are walked recursively. Non-string values are preserved.
Nil input returns nil.

```go
func ExpandEnvInMap(m map[string]any) map[string]any
```

### func ExpandEnvInMapPreservingKeys

ExpandEnvInMapPreservingKeys is like ExpandEnvInMap but when a key in
preserveKeys is encountered, the corresponding value (and any nested
content inside it) is left untouched — ${VAR} / $VAR references are
preserved literally instead of being substituted from the process env.

Use case: plan-time serialization of resource specs where certain
submaps (env_vars, env_vars_secret, secret_env_vars) carry secret
references that should resolve only at apply time. Without this,
security-check rules see resolved literals and incorrectly flag them
as accidentally-pasted secret values.

preserveKeys is matched case-sensitively against the immediate map key
at every depth. An empty or nil preserveKeys list makes this function
behave identically to ExpandEnvInMap.

```go
func ExpandEnvInMapPreservingKeys(m map[string]any, preserveKeys []string) map[string]any
```

### func ExpandEnvInMapPreservingVars

ExpandEnvInMapPreservingVars is like ExpandEnvInMapPreservingKeys but adds
a second dimension of preservation: individual ${VAR} / $VAR references
whose variable name appears in preserveVarNames are emitted as the literal
"${name}" instead of being substituted from the process environment.

Use case: plan-time serialisation of resource specs where a known set of
secret variable names (e.g. cfg.Secrets.Generate keys) must produce
hash-identical output regardless of whether the variable is present in the
current environment.  Without this, fields such as user_data that contain
${SECRET_VAR} produce different hashes at plan time (var unset → empty
substitution) and apply time (var set → actual value), causing a spurious
"plan stale: config hash mismatch".

Precedence: preserveKeys takes priority — if a map key is in preserveKeys
the entire subtree is deep-copied as-is (no expansion at all, matching
ExpandEnvInMapPreservingKeys semantics). preserveVarNames only affects
string values in portions of the tree that are NOT inside a preserved-key
subtree.

```go
func ExpandEnvInMapPreservingVars(m map[string]any, preserveKeys []string, preserveVarNames []string) map[string]any
```

### func ExpandEnvInSlice

ExpandEnvInSlice parallels ExpandEnvInMap for []any.

```go
func ExpandEnvInSlice(s []any) []any
```

### func ExpandEnvInValue

ExpandEnvInValue handles a single any value — used by Map and Slice variants.

```go
func ExpandEnvInValue(v any) any
```

### func HasNonModuleChanges

HasNonModuleChanges returns true if workflows, triggers, pipelines,
platform config, or requirements changed between old and new
(requiring full reload).

```go
func HasNonModuleChanges(old, new *WorkflowConfig) bool
```

### func HashConfig

HashConfig returns the SHA256 hex digest of the YAML-serialized config.

```go
func HashConfig(cfg *WorkflowConfig) (string, error)
```

### func IsApplicationConfig

IsApplicationConfig returns true if the YAML data contains an application-level config
(i.e., has an "application" key with a "workflows" section).

```go
func IsApplicationConfig(data []byte) bool
```

### func IsRuntimeEnvironmentPlaceholder

IsRuntimeEnvironmentPlaceholder reports whether name is a runtime environment
variable reference rather than a static provider environment name.

```go
func IsRuntimeEnvironmentPlaceholder(name string) bool
```

### func MergeConfigs

MergeConfigs merges a config fragment into the primary config.
Modules are appended. Workflows and triggers are merged without
overwriting existing keys.

```go
func MergeConfigs(primary, fragment *WorkflowConfig)
```

### func ResolvePathInConfig

ResolvePathInConfig resolves a path relative to the _config_dir stored in
a module's config map. If the path is already absolute or no _config_dir
is present, the original path is returned.

```go
func ResolvePathInConfig(cfg map[string]any, path string) string
```

### func SaveWfctlLockfile

SaveWfctlLockfile writes a lockfile to disk with sorted plugin keys for determinism.

```go
func SaveWfctlLockfile(path string, lf *WfctlLockfile) error
```

### func SaveWfctlManifest

SaveWfctlManifest writes a manifest to disk in canonical YAML form.

```go
func SaveWfctlManifest(path string, m *WfctlManifest) error
```

### func ValidateMeshRoutes

ValidateMeshRoutes checks mesh.routes references against known service names.

```go
func ValidateMeshRoutes(mesh *MeshConfig, services map[string]*ServiceConfig) []string
```

### func ValidateNetworking

ValidateNetworking checks the networking: section for correctness.
It validates ingress references against services and their exposed ports.

```go
func ValidateNetworking(networking *NetworkingConfig, services map[string]*ServiceConfig) error
```

### func ValidateSecurity

ValidateSecurity checks the security: section.

```go
func ValidateSecurity(security *SecurityConfig) error
```

### func ValidateServices

ValidateServices checks the services: section for correctness.

```go
func ValidateServices(services map[string]*ServiceConfig) error
```

## Types

### type ApplicationConfig

ApplicationConfig is the top-level config for a multi-workflow application.
It references multiple workflow config files that share a module registry.

```go
type ApplicationConfig struct {
	// Application holds the application-level metadata and workflow references.
	Application	ApplicationInfo	`json:"application" yaml:"application"`
	// ConfigDir is the directory of the application config file, used for resolving relative paths.
	ConfigDir	string	`json:"-" yaml:"-"`
}
```

## Functions

### func LoadApplicationConfig

LoadApplicationConfig loads an application config from a YAML file.

```go
func LoadApplicationConfig(filepath string) (*ApplicationConfig, error)
```

### type ApplicationInfo

ApplicationInfo holds top-level metadata about a multi-workflow application.

```go
type ApplicationInfo struct {
	// Name is the application name.
	Name	string	`json:"name" yaml:"name"`
	// Workflows lists the workflow config files that make up this application.
	Workflows	[]WorkflowRef	`json:"workflows" yaml:"workflows"`
}
```

### type BuildHookDeclaration

BuildHookDeclaration registers a plugin as a handler for a specific hook event.

```go
type BuildHookDeclaration struct {
	Event		string	`json:"event" yaml:"event"`
	Priority	int	`json:"priority" yaml:"priority"`	// lower = runs first
	Description	string	`json:"description,omitempty" yaml:"description,omitempty"`
	TimeoutSeconds	int	`json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`	// 0 = use global default
}
```

### type CIAssetTarget

CIAssetTarget is a non-binary build artifact (e.g., frontend bundle).

```go
type CIAssetTarget struct {
	Name	string	`json:"name" yaml:"name"`
	Build	string	`json:"build" yaml:"build"`
	Path	string	`json:"path" yaml:"path"`
}
```

### type CIBaseImagePolicy

CIBaseImagePolicy restricts which base images may be used in container builds.

```go
type CIBaseImagePolicy struct {
	AllowPrefixes	[]string	`json:"allow_prefixes,omitempty" yaml:"allow_prefixes,omitempty"`
	DenyPrefixes	[]string	`json:"deny_prefixes,omitempty" yaml:"deny_prefixes,omitempty"`
}
```

### type CIBinaryTarget

CIBinaryTarget is a Go binary to compile.

```go
type CIBinaryTarget struct {
	Name	string			`json:"name" yaml:"name"`
	Path	string			`json:"path" yaml:"path"`
	OS	[]string		`json:"os,omitempty" yaml:"os,omitempty"`
	Arch	[]string		`json:"arch,omitempty" yaml:"arch,omitempty"`
	LDFlags	string			`json:"ldflags,omitempty" yaml:"ldflags,omitempty"`
	Env	map[string]string	`json:"env,omitempty" yaml:"env,omitempty"`
}
```

### type CIBuildConfig

CIBuildConfig defines what artifacts the build phase produces.
UnmarshalYAML is implemented in ci_target.go to handle the binaries:→targets: migration.

```go
type CIBuildConfig struct {
	// Targets is the canonical field (type-dispatched). Populated from binaries: (legacy) or targets:.
	Targets		[]CITarget		`json:"targets,omitempty" yaml:"targets,omitempty"`
	Containers	[]CIContainerTarget	`json:"containers,omitempty" yaml:"containers,omitempty"`
	Assets		[]CIAssetTarget		`json:"assets,omitempty" yaml:"assets,omitempty"`
	Security	*CIBuildSecurity	`json:"security,omitempty" yaml:"security,omitempty"`
}
```

## Methods

### func UnmarshalYAML

UnmarshalYAML implements the backcompat shim: if binaries: is present
(and targets: is absent), coerce each CIBinaryTarget entry to a CITarget
with type: go, emitting a deprecation warning.

```go
func (b *CIBuildConfig) UnmarshalYAML(value *yaml.Node) error
```

### type CIBuildSecurity

CIBuildSecurity configures supply-chain hardening for the build phase.

```go
type CIBuildSecurity struct {
	Hardened	bool			`json:"hardened" yaml:"hardened"`
	SBOM		bool			`json:"sbom" yaml:"sbom"`
	Provenance	string			`json:"provenance,omitempty" yaml:"provenance,omitempty"`
	Sign		bool			`json:"sign,omitempty" yaml:"sign,omitempty"`
	NonRoot		bool			`json:"non_root" yaml:"non_root"`
	BaseImagePolicy	*CIBaseImagePolicy	`json:"base_image_policy,omitempty" yaml:"base_image_policy,omitempty"`
	// contains filtered or unexported fields
}
```

## Methods

### func ApplyDefaults

ApplyDefaults returns a CIBuildSecurity with opinionated secure defaults applied.
If the receiver is nil, a fully-hardened default struct is returned.
If the receiver is non-nil, only the Provenance field is defaulted when empty;
all other fields are honored as-is (including explicit false values).

```go
func (s *CIBuildSecurity) ApplyDefaults() *CIBuildSecurity
```

### func UnmarshalYAML

```go
func (s *CIBuildSecurity) UnmarshalYAML(value *yaml.Node) error
```

### type CIConfig

CIConfig holds the ci: section of a workflow config — build, test, and deploy lifecycle.

```go
type CIConfig struct {
	Build		*CIBuildConfig		`json:"build,omitempty" yaml:"build,omitempty"`
	Test		*CITestConfig		`json:"test,omitempty" yaml:"test,omitempty"`
	Deploy		*CIDeployConfig		`json:"deploy,omitempty" yaml:"deploy,omitempty"`
	Infra		*CIInfraConfig		`json:"infra,omitempty" yaml:"infra,omitempty"`
	Registries	[]CIRegistry		`json:"registries,omitempty" yaml:"registries,omitempty"`
	Migrations	[]CIMigrationConfig	`json:"migrations,omitempty" yaml:"migrations,omitempty"`
}
```

## Methods

### func Validate

Validate checks the CIConfig for required fields.

```go
func (c *CIConfig) Validate() error
```

### func ValidateWithWarnings

ValidateWithWarnings runs Validate and additionally collects non-fatal
supply-chain warnings (e.g. security.hardened=false opt-out).

```go
func (c *CIConfig) ValidateWithWarnings() (warnings []string, err error)
```

### type CIContainerCache

CIContainerCache configures BuildKit layer cache import/export.

```go
type CIContainerCache struct {
	From	[]CIContainerCacheRef	`json:"from,omitempty" yaml:"from,omitempty"`
	To	[]CIContainerCacheRef	`json:"to,omitempty" yaml:"to,omitempty"`
}
```

### type CIContainerCacheRef

CIContainerCacheRef is a single cache reference (type + ref).

```go
type CIContainerCacheRef struct {
	Type	string	`json:"type,omitempty" yaml:"type,omitempty"`	// registry | local | gha
	Ref	string	`json:"ref,omitempty" yaml:"ref,omitempty"`
}
```

### type CIContainerSecret

CIContainerSecret passes a BuildKit secret into a docker build step.

```go
type CIContainerSecret struct {
	ID	string	`json:"id" yaml:"id"`
	Env	string	`json:"env,omitempty" yaml:"env,omitempty"`
	Src	string	`json:"src,omitempty" yaml:"src,omitempty"`
}
```

### type CIContainerTarget

CIContainerTarget is a container image to build.

```go
type CIContainerTarget struct {
	Name		string	`json:"name" yaml:"name"`
	Dockerfile	string	`json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Context		string	`json:"context,omitempty" yaml:"context,omitempty"`
	Registry	string	`json:"registry,omitempty" yaml:"registry,omitempty"`
	Tag		string	`json:"tag,omitempty" yaml:"tag,omitempty"`
	ExposePorts	[]int	`json:"expose_ports,omitempty" yaml:"expose_ports,omitempty"`

	// Method selects the build driver: "dockerfile" (default) or "ko".
	Method		string			`json:"method,omitempty" yaml:"method,omitempty"`
	KoPackage	string			`json:"ko_package,omitempty" yaml:"ko_package,omitempty"`
	KoBaseImage	string			`json:"ko_base_image,omitempty" yaml:"ko_base_image,omitempty"`
	KoBare		bool			`json:"ko_bare,omitempty" yaml:"ko_bare,omitempty"`
	Platforms	[]string		`json:"platforms,omitempty" yaml:"platforms,omitempty"`
	BuildArgs	map[string]string	`json:"build_args,omitempty" yaml:"build_args,omitempty"`
	Secrets		[]CIContainerSecret	`json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Cache		*CIContainerCache	`json:"cache,omitempty" yaml:"cache,omitempty"`
	Target		string			`json:"target,omitempty" yaml:"target,omitempty"`
	Labels		map[string]string	`json:"labels,omitempty" yaml:"labels,omitempty"`
	ExtraFlags	[]string		`json:"extra_flags,omitempty" yaml:"extra_flags,omitempty"`
	External	bool			`json:"external,omitempty" yaml:"external,omitempty"`
	Source		*CIExternalSource	`json:"source,omitempty" yaml:"source,omitempty"`
	PushTo		[]string		`json:"push_to,omitempty" yaml:"push_to,omitempty"`
}
```

### type CIDeployConfig

CIDeployConfig defines deployment environments.

```go
type CIDeployConfig struct {
	Environments map[string]*CIDeployEnvironment `json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

### type CIDeployEnvironment

CIDeployEnvironment is a single deployment target.

```go
type CIDeployEnvironment struct {
	Provider	string		`json:"provider" yaml:"provider"`
	Cluster		string		`json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Namespace	string		`json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Region		string		`json:"region,omitempty" yaml:"region,omitempty"`
	Strategy	string		`json:"strategy,omitempty" yaml:"strategy,omitempty"`
	RequireApproval	bool		`json:"requireApproval,omitempty" yaml:"requireApproval,omitempty"`
	PreDeploy	[]string	`json:"preDeploy,omitempty" yaml:"preDeploy,omitempty"`
	HealthCheck	*CIHealthCheck	`json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
}
```

### type CIExternalSource

CIExternalSource is an upstream image to pull and re-push rather than build locally.

```go
type CIExternalSource struct {
	Ref	string		`json:"ref" yaml:"ref"`
	TagFrom	[]TagFromEntry	`json:"tag_from,omitempty" yaml:"tag_from,omitempty"`
}
```

### type CIHealthCheck

CIHealthCheck defines how to verify a deployment is healthy.

```go
type CIHealthCheck struct {
	Path	string	`json:"path" yaml:"path"`
	Timeout	string	`json:"timeout,omitempty" yaml:"timeout,omitempty"`
}
```

### type CIInfraConfig

CIInfraConfig defines infrastructure provisioning for CI.

```go
type CIInfraConfig struct {
	Provision	bool			`json:"provision" yaml:"provision"`
	StateBackend	string			`json:"stateBackend,omitempty" yaml:"stateBackend,omitempty"`
	Resources	[]InfraResourceConfig	`json:"resources,omitempty" yaml:"resources,omitempty"`
}
```

### type CIMigrationBaselineConfig

```go
type CIMigrationBaselineConfig struct {
	Ref	string	`json:"ref,omitempty" yaml:"ref,omitempty"`
	Mode	string	`json:"mode,omitempty" yaml:"mode,omitempty"`
}
```

### type CIMigrationConfig

```go
type CIMigrationConfig struct {
	Name		string						`json:"name" yaml:"name"`
	Plugin		string						`json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Driver		string						`json:"driver,omitempty" yaml:"driver,omitempty"`
	SourceDir	string						`json:"source_dir" yaml:"source_dir"`
	Database	CIMigrationDatabaseConfig			`json:"database" yaml:"database"`
	Baseline	CIMigrationBaselineConfig			`json:"baseline,omitempty" yaml:"baseline,omitempty"`
	Validation	CIMigrationValidationConfig			`json:"validation,omitempty" yaml:"validation,omitempty"`
	Environments	map[string]*CIMigrationEnvironmentConfig	`json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

### type CIMigrationDatabaseConfig

```go
type CIMigrationDatabaseConfig struct {
	Env	string	`json:"env,omitempty" yaml:"env,omitempty"`
	DSN	string	`json:"dsn,omitempty" yaml:"dsn,omitempty"`
}
```

### type CIMigrationEnvironmentConfig

```go
type CIMigrationEnvironmentConfig struct {
	Plugin		string				`json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Driver		string				`json:"driver,omitempty" yaml:"driver,omitempty"`
	SourceDir	string				`json:"source_dir,omitempty" yaml:"source_dir,omitempty"`
	Database	CIMigrationDatabaseConfig	`json:"database,omitempty" yaml:"database,omitempty"`
	Baseline	CIMigrationBaselineConfig	`json:"baseline,omitempty" yaml:"baseline,omitempty"`
	Validation	CIMigrationValidationConfig	`json:"validation,omitempty" yaml:"validation,omitempty"`
	ValidationSet	bool				`json:"-" yaml:"-"`
}
```

## Methods

### func UnmarshalYAML

```go
func (c *CIMigrationEnvironmentConfig) UnmarshalYAML(value *yaml.Node) error
```

### type CIMigrationValidationConfig

```go
type CIMigrationValidationConfig struct {
	Lint			bool	`json:"lint,omitempty" yaml:"lint,omitempty"`
	FreshCycle		bool	`json:"fresh_cycle,omitempty" yaml:"fresh_cycle,omitempty"`
	BaselineCandidate	bool	`json:"baseline_candidate,omitempty" yaml:"baseline_candidate,omitempty"`
	ForbidDirty		bool	`json:"forbid_dirty,omitempty" yaml:"forbid_dirty,omitempty"`
}
```

### type CIRegistry

CIRegistry describes a container registry used by the build pipeline.

```go
type CIRegistry struct {
	Name		string			`json:"name" yaml:"name"`
	Type		string			`json:"type" yaml:"type"`
	Path		string			`json:"path" yaml:"path"`
	Auth		*CIRegistryAuth		`json:"auth,omitempty" yaml:"auth,omitempty"`
	Retention	*CIRegistryRetention	`json:"retention,omitempty" yaml:"retention,omitempty"`
	// APIBaseURL is the base URL for the registry provider's API.
	// Used by the GitLab provider to support self-managed instances.
	// Defaults to https://gitlab.com when unset.
	APIBaseURL	string	`json:"api_base_url,omitempty" yaml:"api_base_url,omitempty"`
}
```

### type CIRegistryAuth

CIRegistryAuth holds credentials for pushing/pulling from a registry.

```go
type CIRegistryAuth struct {
	Env		string			`json:"env,omitempty" yaml:"env,omitempty"`
	File		string			`json:"file,omitempty" yaml:"file,omitempty"`
	AWSProfile	string			`json:"aws_profile,omitempty" yaml:"aws_profile,omitempty"`
	Vault		*CIRegistryVaultAuth	`json:"vault,omitempty" yaml:"vault,omitempty"`
}
```

### type CIRegistryRetention

CIRegistryRetention configures automatic tag pruning for a registry.

```go
type CIRegistryRetention struct {
	KeepLatest	int	`json:"keep_latest,omitempty" yaml:"keep_latest,omitempty"`
	UntaggedTTL	string	`json:"untagged_ttl,omitempty" yaml:"untagged_ttl,omitempty"`
	Schedule	string	`json:"schedule,omitempty" yaml:"schedule,omitempty"`
}
```

### type CIRegistryVaultAuth

CIRegistryVaultAuth specifies a HashiCorp Vault path for registry credentials.

```go
type CIRegistryVaultAuth struct {
	Address	string	`json:"address" yaml:"address"`
	Path	string	`json:"path" yaml:"path"`
}
```

### type CITarget

CITarget is a generalized build target with a type: discriminator.
It supersedes the legacy CIBinaryTarget; old configs using binaries:
are automatically coerced via the CIBuildConfig.UnmarshalYAML shim.

```go
type CITarget struct {
	Name		string				`json:"name" yaml:"name"`
	Type		string				`json:"type" yaml:"type"`	// go | nodejs | rust | python | custom
	Path		string				`json:"path,omitempty" yaml:"path,omitempty"`
	Config		map[string]any			`json:"config,omitempty" yaml:"config,omitempty"`
	Environments	map[string]*CITargetOverride	`json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

### type CITargetOverride

CITargetOverride holds per-environment config overrides for a CITarget.

```go
type CITargetOverride struct {
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}
```

### type CITestConfig

CITestConfig defines test phases.

```go
type CITestConfig struct {
	Unit		*CITestPhase	`json:"unit,omitempty" yaml:"unit,omitempty"`
	Integration	*CITestPhase	`json:"integration,omitempty" yaml:"integration,omitempty"`
	E2E		*CITestPhase	`json:"e2e,omitempty" yaml:"e2e,omitempty"`
}
```

### type CITestPhase

CITestPhase is a single test phase.

```go
type CITestPhase struct {
	Command		string		`json:"command" yaml:"command"`
	Coverage	bool		`json:"coverage,omitempty" yaml:"coverage,omitempty"`
	Needs		[]string	`json:"needs,omitempty" yaml:"needs,omitempty"`
}
```

### type CLICommandDeclaration

CLICommandDeclaration registers a plugin as the handler for a top-level wfctl subcommand.

```go
type CLICommandDeclaration struct {
	Name			string	`json:"name" yaml:"name"`
	Description		string	`json:"description,omitempty" yaml:"description,omitempty"`
	FlagsPassthrough	bool	`json:"flagsPassthrough,omitempty" yaml:"flagsPassthrough,omitempty"`
}
```

### type CloudflareTunnelConfig

CloudflareTunnelConfig for Cloudflare Tunnel exposure.

```go
type CloudflareTunnelConfig struct {
	TunnelName	string	`json:"tunnelName,omitempty" yaml:"tunnelName,omitempty"`
	Domain		string	`json:"domain,omitempty" yaml:"domain,omitempty"`
}
```

### type CompositeSource

CompositeSource layers multiple ConfigSources. Later sources override earlier ones.
Module-level overrides are applied by name; map keys (workflows, triggers,
pipelines, platform) from later sources replace or add to those from earlier ones.

```go
type CompositeSource struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewCompositeSource

NewCompositeSource creates a CompositeSource from the given sources.
Sources are applied in order: sources[0] is the base, each subsequent source
overlays on top of the result.

```go
func NewCompositeSource(sources ...ConfigSource) *CompositeSource
```

## Methods

### func Hash

Hash loads the merged config and returns its hash.

```go
func (s *CompositeSource) Hash(ctx context.Context) (string, error)
```

### func Load

Load loads all sources and merges them into a single WorkflowConfig.

```go
func (s *CompositeSource) Load(ctx context.Context) (*WorkflowConfig, error)
```

### func Name

Name returns a human-readable identifier for this source.

```go
func (s *CompositeSource) Name() string
```

### type ConfigChangeEvent

ConfigChangeEvent is emitted when a ConfigSource detects a change.

```go
type ConfigChangeEvent struct {
	Source	string
	OldHash	string
	NewHash	string
	Config	*WorkflowConfig
	Time	time.Time
}
```

### type ConfigReloader

ConfigReloader coordinates config change detection and engine reload decisions.
It diffs old and new configs, performs partial per-module reconfiguration when
possible, and falls back to a full reload when non-module sections change or
modules are added/removed/non-reconfigurable.

```go
type ConfigReloader struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewConfigReloader

NewConfigReloader creates a ConfigReloader with the given initial config.
fullReloadFn is called when a full engine restart is required.
reconfigurer is optional; if nil, all module changes fall back to fullReloadFn.

```go
func NewConfigReloader(
	initial *WorkflowConfig,
	fullReloadFn func(*WorkflowConfig) error,
	reconfigurer ModuleReconfigurer,
	logger *slog.Logger,
) (*ConfigReloader, error)
```

## Methods

### func HandleChange

HandleChange processes a config change event. It diffs the old and new configs,
attempts per-module reconfiguration for module-only changes, and falls back
to a full reload when necessary.

The mutex is held only while reading/writing internal state, never during
external callbacks (fullReloadFn, ReconfigureModules) to avoid deadlocks.

```go
func (r *ConfigReloader) HandleChange(evt ConfigChangeEvent) error
```

### func SetReconfigurer

SetReconfigurer updates the ModuleReconfigurer used for partial (per-module)
reloads. This should be called after a successful full engine reload if the
underlying engine (and its reconfigurer) has changed.

```go
func (r *ConfigReloader) SetReconfigurer(reconfigurer ModuleReconfigurer)
```

### type ConfigSource

ConfigSource provides configuration from an arbitrary backend.
Implementations must be safe for concurrent use.

```go
type ConfigSource interface {
	// Load retrieves the current configuration.
	Load(ctx context.Context) (*WorkflowConfig, error)

	// Hash returns a content-addressable hash of the current config.
	// Used for change detection without full deserialization.
	Hash(ctx context.Context) (string, error)

	// Name returns a human-readable identifier for this source.
	Name() string
}
```

### type ConfigWatcher

ConfigWatcher monitors a config file for changes and invokes a callback.
It watches the directory containing the file for atomic-save compatibility.

```go
type ConfigWatcher struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewConfigWatcher

NewConfigWatcher creates a ConfigWatcher for the given FileSource.
onChange is called with a ConfigChangeEvent whenever the config changes.

```go
func NewConfigWatcher(source *FileSource, onChange func(ConfigChangeEvent), opts ...WatcherOption) *ConfigWatcher
```

## Methods

### func Start

Start begins watching the config file's directory for changes.

```go
func (w *ConfigWatcher) Start() error
```

### func Stop

Stop terminates the watcher and waits for the background goroutine to exit.
It is safe to call Stop multiple times.

```go
func (w *ConfigWatcher) Stop() error
```

### type DBConfigStore

DBConfigStore is the database interface needed by DatabaseSource.

```go
type DBConfigStore interface {
	GetConfigDocument(ctx context.Context, key string) ([]byte, error)
	GetConfigDocumentHash(ctx context.Context, key string) (string, error)
	PutConfigDocument(ctx context.Context, key string, data []byte) error
}
```

### type DNSConfig

DNSConfig defines DNS management.

```go
type DNSConfig struct {
	Provider	string		`json:"provider,omitempty" yaml:"provider,omitempty"`
	Zone		string		`json:"zone,omitempty" yaml:"zone,omitempty"`
	Records		[]DNSRecord	`json:"records,omitempty" yaml:"records,omitempty"`
}
```

### type DNSRecord

DNSRecord is a single DNS record.

```go
type DNSRecord struct {
	Name	string	`json:"name" yaml:"name"`
	Type	string	`json:"type" yaml:"type"`
	Target	string	`json:"target" yaml:"target"`
}
```

### type DatabasePoller

DatabasePoller periodically checks a DatabaseSource for config changes.

```go
type DatabasePoller struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewDatabasePoller

NewDatabasePoller creates a DatabasePoller that calls onChange whenever the
config stored in source changes.

```go
func NewDatabasePoller(source *DatabaseSource, interval time.Duration, onChange func(ConfigChangeEvent), logger *slog.Logger) *DatabasePoller
```

## Methods

### func Start

Start fetches the initial hash and launches the background polling goroutine.

```go
func (p *DatabasePoller) Start(ctx context.Context) error
```

### func Stop

Stop signals the polling goroutine to exit and waits for it to finish.
It is safe to call Stop multiple times.

```go
func (p *DatabasePoller) Stop()
```

### type DatabaseSource

DatabaseSource loads config from a database with caching.

```go
type DatabaseSource struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewDatabaseSource

NewDatabaseSource creates a DatabaseSource backed by the given store.

```go
func NewDatabaseSource(store DBConfigStore, opts ...DatabaseSourceOption) *DatabaseSource
```

## Methods

### func Hash

Hash returns the SHA256 hex digest of the stored config bytes. It first
tries the fast path of fetching the pre-computed hash from the database,
and falls back to loading the full document if that fails. The fallback
always fetches fresh data to ensure change detection is accurate.

```go
func (s *DatabaseSource) Hash(ctx context.Context) (string, error)
```

### func Load

Load retrieves the current configuration, returning a cached copy if still
within the refresh interval.

```go
func (s *DatabaseSource) Load(ctx context.Context) (*WorkflowConfig, error)
```

### func Name

Name returns a human-readable identifier for this source.

```go
func (s *DatabaseSource) Name() string
```

### type DatabaseSourceOption

DatabaseSourceOption configures a DatabaseSource.

```go
type DatabaseSourceOption func(*DatabaseSource)
```

## Functions

### func WithConfigKey

WithConfigKey sets the document key used to look up config in the database.

```go
func WithConfigKey(key string) DatabaseSourceOption
```

### func WithRefreshInterval

WithRefreshInterval sets the cache TTL for the DatabaseSource.

```go
func WithRefreshInterval(d time.Duration) DatabaseSourceOption
```

### type EngineConfig

EngineConfig holds engine-level runtime settings.

```go
type EngineConfig struct {
	Validation *EngineValidationConfig `json:"validation,omitempty" yaml:"validation,omitempty"`
}
```

### type EngineValidationConfig

EngineValidationConfig controls startup and execution-time validation behaviour.

```go
type EngineValidationConfig struct {
	// TemplateRefs controls template cross-reference validation at startup.
	// Allowed values: "off" (skip), "warn" (log warnings, default), "error" (fail on any validation issues).
	TemplateRefs string `json:"templateRefs,omitempty" yaml:"templateRefs,omitempty"`
}
```

### type EnvironmentConfig

EnvironmentConfig defines a deployment environment with its provider and overrides.

```go
type EnvironmentConfig struct {
	Provider	string			`json:"provider" yaml:"provider"`
	Region		string			`json:"region,omitempty" yaml:"region,omitempty"`
	EnvVars		map[string]string	`json:"envVars,omitempty" yaml:"envVars,omitempty"`
	SecretsProvider	string			`json:"secretsProvider,omitempty" yaml:"secretsProvider,omitempty"`
	SecretsPrefix	string			`json:"secretsPrefix,omitempty" yaml:"secretsPrefix,omitempty"`
	// SecretsStoreOverride forces all secrets in this environment to use a specific named store.
	// Overrides defaultStore but is itself overridden by a per-secret Store field.
	SecretsStoreOverride	string		`json:"secretsStoreOverride,omitempty" yaml:"secretsStoreOverride,omitempty"`
	ApprovalRequired	bool		`json:"approvalRequired,omitempty" yaml:"approvalRequired,omitempty"`
	Exposure		*ExposureConfig	`json:"exposure,omitempty" yaml:"exposure,omitempty"`
	// Build overrides ci.build values for this environment.
	Build	*CIBuildConfig	`json:"build,omitempty" yaml:"build,omitempty"`
}
```

### type ExposeConfig

ExposeConfig defines a port that the service exposes.

```go
type ExposeConfig struct {
	Port		int	`json:"port" yaml:"port"`
	Protocol	string	`json:"protocol,omitempty" yaml:"protocol,omitempty"`
}
```

### type ExposureConfig

ExposureConfig defines how a service is exposed to the network.

```go
type ExposureConfig struct {
	Method			string			`json:"method" yaml:"method"`
	Tailscale		*TailscaleConfig	`json:"tailscale,omitempty" yaml:"tailscale,omitempty"`
	CloudflareTunnel	*CloudflareTunnelConfig	`json:"cloudflareTunnel,omitempty" yaml:"cloudflareTunnel,omitempty"`
	PortForward		map[string]string	`json:"portForward,omitempty" yaml:"portForward,omitempty"`
}
```

### type ExternalPluginDecl

ExternalPluginDecl declares an external plugin that the engine should load.
When AutoFetch is true and the plugin is not found locally, the engine will
call wfctl to download it from the registry before loading.

```go
type ExternalPluginDecl struct {
	// Name is the plugin name as registered in the plugin registry.
	Name	string	`json:"name" yaml:"name"`
	// Version is an optional version specifier forwarded to wfctl plugin install
	// as name@version. Simple constraints (>=, ^, ~) are stripped to extract the
	// version; compound constraints fall back to installing the latest.
	// Used only when AutoFetch is true.
	Version	string	`json:"version,omitempty" yaml:"version,omitempty"`
	// AutoFetch controls whether the engine should download the plugin
	// automatically if it is not found in the local plugin directory.
	AutoFetch	bool	`json:"autoFetch,omitempty" yaml:"autoFetch,omitempty"`
}
```

### type FileSource

FileSource loads config from a YAML file on disk.

```go
type FileSource struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewFileSource

NewFileSource creates a FileSource that reads from the given path.

```go
func NewFileSource(path string) *FileSource
```

## Methods

### func Hash

Hash returns the SHA256 hex digest of the raw file bytes.

```go
func (s *FileSource) Hash(_ context.Context) (string, error)
```

### func Load

Load reads the config file and returns a parsed WorkflowConfig.
Supports both ApplicationConfig (multi-workflow) and WorkflowConfig formats.

```go
func (s *FileSource) Load(_ context.Context) (*WorkflowConfig, error)
```

### func Name

Name returns a human-readable identifier for this source.

```go
func (s *FileSource) Name() string
```

### func Path

Path returns the filesystem path this source reads from.

```go
func (s *FileSource) Path() string
```

### type InfraConfig

InfraConfig is the "infra:" top-level section of a workflow config file.
It lives in the config package so that WorkflowConfig.Infra is preserved
when a config round-trips through LoadFromFile / marshal.

```go
type InfraConfig struct {
	// AutoBootstrap controls whether `wfctl infra apply` automatically runs
	// `wfctl infra bootstrap` when no state backend exists yet.
	AutoBootstrap *bool `json:"auto_bootstrap,omitempty" yaml:"auto_bootstrap,omitempty"`
}
```

### type InfraConnectionConfig

InfraConnectionConfig holds connection details for an existing infrastructure resource.

```go
type InfraConnectionConfig struct {
	Host	string	`json:"host" yaml:"host"`
	Port	int	`json:"port,omitempty" yaml:"port,omitempty"`
	Auth	string	`json:"auth,omitempty" yaml:"auth,omitempty"`
}
```

### type InfraEnvironmentResolution

InfraEnvironmentResolution defines how an infrastructure resource is resolved
in a specific environment. A strategy of "container" means run it as a local
Docker container, "provision" means create it via a cloud provider, and
"existing" means connect to an already-running instance.

```go
type InfraEnvironmentResolution struct {
	// Strategy determines how the resource is obtained: container, provision, existing.
	Strategy	string	`json:"strategy" yaml:"strategy"`
	// DockerImage is used when Strategy is "container".
	DockerImage	string	`json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	// Port overrides the default service port when Strategy is "container".
	Port	int	`json:"port,omitempty" yaml:"port,omitempty"`
	// Provider names the cloud provider when Strategy is "provision".
	Provider	string	`json:"provider,omitempty" yaml:"provider,omitempty"`
	// Config holds provider-specific provisioning options.
	Config	map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
	// Connection holds connection details when Strategy is "existing".
	Connection	*InfraConnectionConfig	`json:"connection,omitempty" yaml:"connection,omitempty"`
}
```

### type InfraRequirement

InfraRequirement is a single infrastructure dependency.

```go
type InfraRequirement struct {
	Type		string		`json:"type" yaml:"type"`
	Name		string		`json:"name" yaml:"name"`
	Description	string		`json:"description" yaml:"description"`
	DockerImage	string		`json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	Ports		[]int		`json:"ports,omitempty" yaml:"ports,omitempty"`
	Secrets		[]string	`json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Providers	[]string	`json:"providers,omitempty" yaml:"providers,omitempty"`
	Optional	bool		`json:"optional,omitempty" yaml:"optional,omitempty"`
}
```

### type InfraResourceConfig

InfraResourceConfig describes a single infrastructure resource to provision.

```go
type InfraResourceConfig struct {
	Name		string		`json:"name" yaml:"name"`
	Type		string		`json:"type" yaml:"type"`
	Provider	string		`json:"provider,omitempty" yaml:"provider,omitempty"`
	Config		map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
	// Environments maps environment names to per-environment resolution strategies.
	Environments	map[string]*InfraEnvironmentResolution	`json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

### type InfrastructureConfig

InfrastructureConfig holds infrastructure resource declarations.

```go
type InfrastructureConfig struct {
	Resources []InfraResourceConfig `json:"resources" yaml:"resources"`
}
```

### type IngressConfig

IngressConfig defines an externally-accessible endpoint.

```go
type IngressConfig struct {
	Service		string		`json:"service,omitempty" yaml:"service,omitempty"`
	Port		int		`json:"port" yaml:"port"`
	ExternalPort	int		`json:"externalPort,omitempty" yaml:"externalPort,omitempty"`
	Protocol	string		`json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Path		string		`json:"path,omitempty" yaml:"path,omitempty"`
	TLS		*TLSConfig	`json:"tls,omitempty" yaml:"tls,omitempty"`
}
```

### type MeshConfig

MeshConfig defines inter-service communication.

```go
type MeshConfig struct {
	Transport	string			`json:"transport,omitempty" yaml:"transport,omitempty"`
	Discovery	string			`json:"discovery,omitempty" yaml:"discovery,omitempty"`
	NATS		*MeshNATSConfig		`json:"nats,omitempty" yaml:"nats,omitempty"`
	Routes		[]MeshRouteConfig	`json:"routes,omitempty" yaml:"routes,omitempty"`
}
```

### type MeshNATSConfig

MeshNATSConfig holds NATS-specific mesh configuration.

```go
type MeshNATSConfig struct {
	URL		string	`json:"url" yaml:"url"`
	ClusterID	string	`json:"clusterId,omitempty" yaml:"clusterId,omitempty"`
}
```

### type MeshRouteConfig

MeshRouteConfig declares a communication path between services.

```go
type MeshRouteConfig struct {
	From		string	`json:"from" yaml:"from"`
	To		string	`json:"to" yaml:"to"`
	Via		string	`json:"via" yaml:"via"`
	Subject		string	`json:"subject,omitempty" yaml:"subject,omitempty"`
	Endpoint	string	`json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}
```

### type ModuleConfig

ModuleConfig represents a single module configuration

```go
type ModuleConfig struct {
	Name		string					`json:"name" yaml:"name"`
	Type		string					`json:"type" yaml:"type"`
	Satisfies	[]string				`json:"satisfies,omitempty" yaml:"satisfies,omitempty"`
	Protected	bool					`json:"protected,omitempty" yaml:"protected,omitempty"`
	Config		map[string]any				`json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn	[]string				`json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Branches	map[string]string			`json:"branches,omitempty" yaml:"branches,omitempty"`
	Environments	map[string]*InfraEnvironmentResolution	`json:"environments,omitempty" yaml:"environments,omitempty"`
}
```

## Methods

### func ResolveForEnv

ResolveForEnv returns the effective module config for envName.
If m.Environments is empty or envName is not listed, the top-level fields are returned.
If m.Environments[envName] is explicitly nil, ok=false (resource skipped in this env).
Otherwise the per-env resolution is deep-merged over the top-level fields.
region and provider are written into the Config map so downstream ResourceSpec
construction (which reads only Config) picks them up.

```go
func (m *ModuleConfig) ResolveForEnv(envName string) (*ResolvedModule, bool)
```

### type ModuleConfigChange

ModuleConfigChange represents a change to a single module's config.

```go
type ModuleConfigChange struct {
	Name		string
	OldConfig	map[string]any
	NewConfig	map[string]any
}
```

### type ModuleConfigDiff

ModuleConfigDiff represents what changed between two configs.

```go
type ModuleConfigDiff struct {
	Added		[]ModuleConfig		// modules in new but not old
	Removed		[]ModuleConfig		// modules in old but not new
	Modified	[]ModuleConfigChange	// modules present in both with different config
	Unchanged	[]string		// module names with no config change
}
```

## Functions

### func DiffModuleConfigs

DiffModuleConfigs compares two configs and identifies module-level changes.

```go
func DiffModuleConfigs(old, new *WorkflowConfig) *ModuleConfigDiff
```

### type ModuleInfraRequirementV2

ModuleInfraRequirementV2 is the plugin.json authoring shape for derived-IaC
requirements. It mirrors the portable fields in plugin/external/proto/iac.proto
using strings so manifests stay easy to read and preserve unknown future
provider details under Parameters.

```go
type ModuleInfraRequirementV2 struct {
	Key			string		`json:"key" yaml:"key"`
	Kind			string		`json:"kind" yaml:"kind"`
	Source			string		`json:"source,omitempty" yaml:"source,omitempty"`
	ResourceTypeHint	string		`json:"resourceTypeHint,omitempty" yaml:"resourceTypeHint,omitempty"`
	Environment		string		`json:"environment,omitempty" yaml:"environment,omitempty"`
	Runtimes		[]string	`json:"runtimes,omitempty" yaml:"runtimes,omitempty"`
	TelemetrySignals	[]string	`json:"telemetrySignals,omitempty" yaml:"telemetrySignals,omitempty"`
	ObservabilityBackends	[]string	`json:"observabilityBackends,omitempty" yaml:"observabilityBackends,omitempty"`
	DeploymentModes		[]string	`json:"deploymentModes,omitempty" yaml:"deploymentModes,omitempty"`
	VendorFeatures		[]string	`json:"vendorFeatures,omitempty" yaml:"vendorFeatures,omitempty"`
	Parameters		map[string]any	`json:"parameters,omitempty" yaml:"parameters,omitempty"`
}
```

### type ModuleInfraSpec

ModuleInfraSpec declares what a module type requires.

```go
type ModuleInfraSpec struct {
	Requires []InfraRequirement `json:"requires" yaml:"requires"`
}
```

### type ModuleInfraSpecV2

ModuleInfraSpecV2 declares typed requirement metadata for a module type.

```go
type ModuleInfraSpecV2 struct {
	Requires []ModuleInfraRequirementV2 `json:"requires" yaml:"requires"`
}
```

### type ModuleReconfigurer

ModuleReconfigurer is implemented by the engine to support partial (per-module) reloads.
When a config change only affects module configs, the engine can apply changes surgically
rather than performing a full stop/rebuild/start cycle.

```go
type ModuleReconfigurer interface {
	// ReconfigureModules applies new configuration to specific running modules.
	// Returns the names of any modules that could not be reconfigured in-place
	// (requiring a full reload) and any hard error.
	ReconfigureModules(ctx context.Context, changes []ModuleConfigChange) (failedModules []string, err error)
}
```

### type NetworkPolicy

NetworkPolicy defines allowed communication between services.

```go
type NetworkPolicy struct {
	From	string		`json:"from" yaml:"from"`
	To	[]string	`json:"to" yaml:"to"`
}
```

### type NetworkingConfig

NetworkingConfig defines network exposure and policies.

```go
type NetworkingConfig struct {
	Ingress		[]IngressConfig	`json:"ingress,omitempty" yaml:"ingress,omitempty"`
	Policies	[]NetworkPolicy	`json:"policies,omitempty" yaml:"policies,omitempty"`
	DNS		*DNSConfig	`json:"dns,omitempty" yaml:"dns,omitempty"`
}
```

### type PipelineConfig

PipelineConfig represents a single composable pipeline definition.

```go
type PipelineConfig struct {
	Trigger		PipelineTriggerConfig	`json:"trigger" yaml:"trigger"`
	Steps		[]PipelineStepConfig	`json:"steps" yaml:"steps"`
	OnError		string			`json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Timeout		string			`json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Compensation	[]PipelineStepConfig	`json:"compensation,omitempty" yaml:"compensation,omitempty"`
	// Outputs declares the named output fields that this pipeline is expected
	// to produce. This is an optional, backwards-compatible declaration —
	// existing pipelines without an Outputs block continue to work unchanged.
	// When present, it enables:
	//   - wfctl contract test to include response schemas in endpoint contracts
	//   - tools that understand pipeline outputs (for example, step.workflow_call)
	//     to validate or document output_mapping and other response shapes
	Outputs	map[string]PipelineOutputDef	`json:"outputs,omitempty" yaml:"outputs,omitempty"`
	// StrictTemplates causes the pipeline to return an error when any template
	// expression references a missing map key, instead of silently using the zero
	// value. Useful for catching typos in step field references at runtime.
	// Default is false (missing keys produce a warning log and resolve to zero).
	StrictTemplates	bool	`json:"strict_templates,omitempty" yaml:"strict_templates,omitempty"`
}
```

### type PipelineOutputDef

PipelineOutputDef describes a single declared output field of a pipeline.
When a pipeline declares its outputs, callers (HTTP triggers,
step.workflow_call, wfctl contract test) can validate or document the
expected response shape.

```go
type PipelineOutputDef struct {
	Type		string	`json:"type" yaml:"type"`
	Description	string	`json:"description,omitempty" yaml:"description,omitempty"`
}
```

### type PipelineStepConfig

PipelineStepConfig defines a single step in a pipeline.

```go
type PipelineStepConfig struct {
	Name	string		`json:"name" yaml:"name"`
	Type	string		`json:"type" yaml:"type"`
	Config	map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
	OnError	string		`json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Timeout	string		`json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// SkipIf is an optional Go template expression. When it evaluates to a
	// truthy value (non-empty, not "false", not "0"), the step is skipped and
	// the pipeline continues with the next step. Falsy or absent → execute.
	SkipIf	string	`json:"skip_if,omitempty" yaml:"skip_if,omitempty"`
	// If is the logical inverse of SkipIf: the step executes only when the
	// template evaluates to truthy. Falsy or absent with no SkipIf → execute.
	// When both SkipIf and If are set, SkipIf takes precedence.
	If	string	`json:"if,omitempty" yaml:"if,omitempty"`
	// ErrorStatus overrides the HTTP response status code when this step fails.
	// Use 400 for bad requests, 422 for unprocessable entity, etc.
	// When set, the step error is wrapped in a ValidationError so the HTTP
	// handler returns the specified status code instead of 500.
	ErrorStatus	int	`json:"error_status,omitempty" yaml:"error_status,omitempty"`
}
```

### type PipelineTriggerConfig

PipelineTriggerConfig defines what starts a pipeline.

```go
type PipelineTriggerConfig struct {
	Type	string		`json:"type" yaml:"type"`
	Config	map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
}
```

### type PluginCapabilities

PluginCapabilities describes what module, step, trigger types, build hooks,
and CLI commands a plugin provides.

```go
type PluginCapabilities struct {
	ModuleTypes	[]string		`json:"moduleTypes" yaml:"moduleTypes"`
	StepTypes	[]string		`json:"stepTypes" yaml:"stepTypes"`
	TriggerTypes	[]string		`json:"triggerTypes" yaml:"triggerTypes"`
	BuildHooks	[]BuildHookDeclaration	`json:"buildHooks,omitempty" yaml:"buildHooks,omitempty"`
	OnHookFailure	string			`json:"onHookFailure,omitempty" yaml:"onHookFailure,omitempty"`	// fail | warn | skip
	// PortPaths is a list of dot-notation JSON paths into module config that
	// contain port values (e.g. ["config.api_port", "config.grpc_port"]).
	// The port introspector walks these paths for modules of any type declared by this plugin.
	PortPaths	[]string		`json:"portPaths,omitempty" yaml:"portPaths,omitempty"`
	CLICommands	[]CLICommandDeclaration	`json:"cliCommands,omitempty" yaml:"cliCommands,omitempty"`
}
```

### type PluginInfraRequirements

PluginInfraRequirements maps module types to their infrastructure needs.

```go
type PluginInfraRequirements map[string]*ModuleInfraSpec
```

### type PluginInfraRequirementsV2

PluginInfraRequirementsV2 maps module types to provider-neutral IaC
requirements. The values intentionally use manifest-friendly strings; the
iac/requirements package owns typed enum validation and protobuf conversion.

```go
type PluginInfraRequirementsV2 map[string]*ModuleInfraSpecV2
```

### type PluginManifestFile

PluginManifestFile represents the full plugin.json manifest.

```go
type PluginManifestFile struct {
	Name				string				`json:"name" yaml:"name"`
	Version				string				`json:"version" yaml:"version"`
	Description			string				`json:"description" yaml:"description"`
	Capabilities			PluginCapabilities		`json:"capabilities" yaml:"capabilities"`
	ModuleInfraRequirements		PluginInfraRequirements		`json:"moduleInfraRequirements,omitempty" yaml:"moduleInfraRequirements,omitempty"`
	ModuleInfraRequirementsV2	PluginInfraRequirementsV2	`json:"moduleInfraRequirementsV2,omitempty" yaml:"moduleInfraRequirementsV2,omitempty"`
}
```

### type PluginRequirement

PluginRequirement specifies a required plugin with optional version constraint.

```go
type PluginRequirement struct {
	Name	string			`json:"name" yaml:"name"`
	Version	string			`json:"version,omitempty" yaml:"version,omitempty"`
	Source	string			`json:"source,omitempty" yaml:"source,omitempty"`
	Auth	*PluginRequirementAuth	`json:"auth,omitempty" yaml:"auth,omitempty"`
	Verify	*PluginVerifyConfig	`json:"verify,omitempty" yaml:"verify,omitempty"`
}
```

### type PluginRequirementAuth

PluginRequirementAuth holds credentials for fetching a private plugin.

```go
type PluginRequirementAuth struct {
	Env string `json:"env,omitempty" yaml:"env,omitempty"`
}
```

### type PluginVerifyConfig

PluginVerifyConfig controls supply-chain verification for a plugin install.
Consumed by the install_verify hook handler in workflow-plugin-supply-chain.

```go
type PluginVerifyConfig struct {
	// Signature controls cosign signature verification: required | allow-missing | off
	Signature	string	`json:"signature,omitempty" yaml:"signature,omitempty"`
	// SBOM controls SBOM presence check: required | allow-missing | off
	SBOM	string	`json:"sbom,omitempty" yaml:"sbom,omitempty"`
	// VulnPolicy controls OSV vulnerability scan policy: block-critical | warn | off
	VulnPolicy	string	`json:"vuln_policy,omitempty" yaml:"vuln_policy,omitempty"`
}
```

### type PluginsConfig

PluginsConfig holds the top-level plugins configuration section.

```go
type PluginsConfig struct {
	// External lists external plugins that the engine should discover and load.
	External []ExternalPluginDecl `json:"external,omitempty" yaml:"external,omitempty"`
}
```

### type RequiresConfig

RequiresConfig declares what capabilities and plugins a workflow needs.

```go
type RequiresConfig struct {
	Capabilities	[]string		`json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Plugins		[]PluginRequirement	`json:"plugins,omitempty" yaml:"plugins,omitempty"`
}
```

### type ResolvedModule

ResolvedModule is the effective module config for a specific environment.

```go
type ResolvedModule struct {
	Name		string
	Type		string
	Provider	string
	Region		string
	Protected	bool
	Config		map[string]any
}
```

### type ScalingConfig

ScalingConfig defines how a service scales.

```go
type ScalingConfig struct {
	Replicas	int	`json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Min		int	`json:"min,omitempty" yaml:"min,omitempty"`
	Max		int	`json:"max,omitempty" yaml:"max,omitempty"`
	Metric		string	`json:"metric,omitempty" yaml:"metric,omitempty"`
	Target		int	`json:"target,omitempty" yaml:"target,omitempty"`
}
```

### type SecretEntry

SecretEntry declares a single secret the application needs.

```go
type SecretEntry struct {
	Name		string	`json:"name" yaml:"name"`
	Description	string	`json:"description,omitempty" yaml:"description,omitempty"`
	// Store names the store (from secretStores) this secret lives in.
	// Overrides defaultStore and environment secretsStoreOverride.
	Store		string			`json:"store,omitempty" yaml:"store,omitempty"`
	Rotation	*SecretsRotationConfig	`json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
```

### type SecretGen

SecretGen describes a secret to generate and store during bootstrap.
It lives here (rather than cmd/wfctl) so that WorkflowConfig.Secrets.Generate
is preserved when a config round-trips through config.LoadFromFile / marshal.

```go
type SecretGen struct {
	Key	string	`json:"key" yaml:"key"`
	Type	string	`json:"type" yaml:"type"`				// e.g. "random_hex", "provider_credential"
	Length	int	`json:"length,omitempty" yaml:"length,omitempty"`	// for random generators
	Source	string	`json:"source,omitempty" yaml:"source,omitempty"`	// for provider_credential
	Name	string	`json:"name,omitempty" yaml:"name,omitempty"`		// optional human-readable label
	Store	string	`json:"store,omitempty" yaml:"store,omitempty"`		// optional named store for infra_output sync
}
```

### type SecretStoreConfig

SecretStoreConfig defines a named secret storage backend.

```go
type SecretStoreConfig struct {
	Provider	string		`json:"provider" yaml:"provider"`
	Config		map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
}
```

### type SecretsConfig

SecretsConfig defines secret management for the application.

```go
type SecretsConfig struct {
	// DefaultStore names the store (from secretStores) to use when a secret has no explicit store.
	DefaultStore	string	`json:"defaultStore,omitempty" yaml:"defaultStore,omitempty"`
	// Entries lists the secrets this application requires.
	Entries	[]SecretEntry	`json:"entries,omitempty" yaml:"entries,omitempty"`
	// Provider is the legacy single-store provider name. Kept for backward compatibility.
	// Prefer secretStores + defaultStore for new configs.
	Provider	string			`json:"provider,omitempty" yaml:"provider,omitempty"`
	Config		map[string]any		`json:"config,omitempty" yaml:"config,omitempty"`
	Rotation	*SecretsRotationConfig	`json:"rotation,omitempty" yaml:"rotation,omitempty"`
	// Generate lists secrets to create during `wfctl infra bootstrap`.
	Generate	[]SecretGen	`json:"generate,omitempty" yaml:"generate,omitempty"`
}
```

### type SecretsRotationConfig

SecretsRotationConfig defines default rotation policy.

```go
type SecretsRotationConfig struct {
	Enabled		bool	`json:"enabled" yaml:"enabled"`
	Interval	string	`json:"interval,omitempty" yaml:"interval,omitempty"`
	Strategy	string	`json:"strategy,omitempty" yaml:"strategy,omitempty"`
}
```

### type SecurityConfig

SecurityConfig defines security policies for the application.

```go
type SecurityConfig struct {
	TLS		*SecurityTLSConfig	`json:"tls,omitempty" yaml:"tls,omitempty"`
	Network		*SecurityNetworkConfig	`json:"network,omitempty" yaml:"network,omitempty"`
	Identity	*SecurityIdentityConfig	`json:"identity,omitempty" yaml:"identity,omitempty"`
	Runtime		*SecurityRuntimeConfig	`json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Scanning	*SecurityScanningConfig	`json:"scanning,omitempty" yaml:"scanning,omitempty"`
}
```

### type SecurityIdentityConfig

SecurityIdentityConfig defines service identity management.

```go
type SecurityIdentityConfig struct {
	Provider	string	`json:"provider,omitempty" yaml:"provider,omitempty"`
	PerService	bool	`json:"perService,omitempty" yaml:"perService,omitempty"`
}
```

### type SecurityNetworkConfig

SecurityNetworkConfig defines network isolation policy.

```go
type SecurityNetworkConfig struct {
	DefaultPolicy string `json:"defaultPolicy,omitempty" yaml:"defaultPolicy,omitempty"`
}
```

### type SecurityRuntimeConfig

SecurityRuntimeConfig defines container runtime security.

```go
type SecurityRuntimeConfig struct {
	ReadOnlyFilesystem	bool		`json:"readOnlyFilesystem,omitempty" yaml:"readOnlyFilesystem,omitempty"`
	NoNewPrivileges		bool		`json:"noNewPrivileges,omitempty" yaml:"noNewPrivileges,omitempty"`
	RunAsNonRoot		bool		`json:"runAsNonRoot,omitempty" yaml:"runAsNonRoot,omitempty"`
	DropCapabilities	[]string	`json:"dropCapabilities,omitempty" yaml:"dropCapabilities,omitempty"`
	AddCapabilities		[]string	`json:"addCapabilities,omitempty" yaml:"addCapabilities,omitempty"`
}
```

### type SecurityScanningConfig

SecurityScanningConfig defines automated security scanning.

```go
type SecurityScanningConfig struct {
	ContainerScan	bool	`json:"containerScan,omitempty" yaml:"containerScan,omitempty"`
	DependencyScan	bool	`json:"dependencyScan,omitempty" yaml:"dependencyScan,omitempty"`
	SAST		bool	`json:"sast,omitempty" yaml:"sast,omitempty"`
}
```

### type SecurityTLSConfig

SecurityTLSConfig defines TLS requirements.

```go
type SecurityTLSConfig struct {
	Internal	bool	`json:"internal,omitempty" yaml:"internal,omitempty"`
	External	bool	`json:"external,omitempty" yaml:"external,omitempty"`
	Provider	string	`json:"provider,omitempty" yaml:"provider,omitempty"`
	MinVersion	string	`json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}
```

### type ServiceConfig

ServiceConfig defines a single service within a multi-service application.

```go
type ServiceConfig struct {
	Description	string		`json:"description,omitempty" yaml:"description,omitempty"`
	Binary		string		`json:"binary,omitempty" yaml:"binary,omitempty"`
	Scaling		*ScalingConfig	`json:"scaling,omitempty" yaml:"scaling,omitempty"`
	Modules		[]ModuleConfig	`json:"modules,omitempty" yaml:"modules,omitempty"`
	Workflows	map[string]any	`json:"workflows,omitempty" yaml:"workflows,omitempty"`
	Pipelines	map[string]any	`json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	Triggers	map[string]any	`json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Plugins		[]string	`json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Expose		[]ExposeConfig	`json:"expose,omitempty" yaml:"expose,omitempty"`
}
```

### type SidecarConfig

SidecarConfig defines a sidecar container to run alongside the workflow application.

```go
type SidecarConfig struct {
	Name		string		`json:"name" yaml:"name"`
	Type		string		`json:"type" yaml:"type"`
	Config		map[string]any	`json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn	[]string	`json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
}
```

### type TLSConfig

TLSConfig defines TLS termination.

```go
type TLSConfig struct {
	Provider	string	`json:"provider,omitempty" yaml:"provider,omitempty"`
	Domain		string	`json:"domain,omitempty" yaml:"domain,omitempty"`
	MinVersion	string	`json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}
```

### type TagFromEntry

TagFromEntry is one step in a tag resolution chain.
The first non-empty result wins; Command is run via sh -c.

```go
type TagFromEntry struct {
	Env	string	`json:"env,omitempty" yaml:"env,omitempty"`
	Command	string	`json:"command,omitempty" yaml:"command,omitempty"`
}
```

### type TailscaleConfig

TailscaleConfig for Tailscale Funnel exposure.

```go
type TailscaleConfig struct {
	Funnel		bool	`json:"funnel,omitempty" yaml:"funnel,omitempty"`
	Hostname	string	`json:"hostname,omitempty" yaml:"hostname,omitempty"`
}
```

### type WatcherOption

WatcherOption configures a ConfigWatcher.

```go
type WatcherOption func(*ConfigWatcher)
```

## Functions

### func WithWatchDebounce

WithWatchDebounce sets the debounce duration for file change events.

```go
func WithWatchDebounce(d time.Duration) WatcherOption
```

### func WithWatchLogger

WithWatchLogger sets the logger for the watcher.

```go
func WithWatchLogger(l *slog.Logger) WatcherOption
```

### type WfctlLockCompatibility

```go
type WfctlLockCompatibility struct {
	Mode		string	`yaml:"mode,omitempty"`
	Status		string	`yaml:"status,omitempty"`
	EngineVersion	string	`yaml:"engine_version,omitempty"`
	EvidenceDigest	string	`yaml:"evidence_digest,omitempty"`
	Forced		bool	`yaml:"forced,omitempty"`
	Reason		string	`yaml:"reason,omitempty"`
}
```

### type WfctlLockPlatform

WfctlLockPlatform holds platform-specific download info.

```go
type WfctlLockPlatform struct {
	URL		string			`yaml:"url"`
	SHA256		string			`yaml:"sha256"`
	Compatibility	*WfctlLockCompatibility	`yaml:"compatibility,omitempty"`
}
```

### type WfctlLockPluginEntry

WfctlLockPluginEntry is the locked record for a single plugin.

```go
type WfctlLockPluginEntry struct {
	Version	string	`yaml:"version"`
	Source	string	`yaml:"source"`
	// SHA256 is deprecated top-level metadata from early new-format lockfiles.
	// Platform archive checksums live under Platforms; old-format binary
	// checksums are handled by cmd/wfctl/plugin_lockfile.go.
	SHA256		string				`yaml:"sha256,omitempty"`
	Platforms	map[string]WfctlLockPlatform	`yaml:"platforms,omitempty"`
}
```

### type WfctlLockfile

WfctlLockfile is the structure of .wfctl-lock.yaml — the machine-generated lockfile.
It is derived from wfctl.yaml and must not be hand-edited.
Plugin keys are sorted alphabetically for deterministic git diffs.

```go
type WfctlLockfile struct {
	Version		int				`yaml:"version"`
	GeneratedAt	time.Time			`yaml:"generated_at"`
	Plugins		map[string]WfctlLockPluginEntry	`yaml:"plugins"`
}
```

## Functions

### func LoadWfctlLockfile

LoadWfctlLockfile reads and parses a .wfctl-lock.yaml file.

```go
func LoadWfctlLockfile(path string) (*WfctlLockfile, error)
```

### type WfctlManifest

WfctlManifest is the structure of wfctl.yaml — the human-editable plugin manifest.
It lists plugins with their declared versions and sources.
The machine-generated lockfile (.wfctl-lock.yaml) is derived from this manifest.

```go
type WfctlManifest struct {
	Version	int			`yaml:"version"`
	Plugins	[]WfctlPluginEntry	`yaml:"plugins"`
}
```

## Functions

### func LoadWfctlManifest

LoadWfctlManifest reads and parses a wfctl.yaml manifest file.

```go
func LoadWfctlManifest(path string) (*WfctlManifest, error)
```

### type WfctlPluginAuth

WfctlPluginAuth holds auth configuration for private plugin registries.

```go
type WfctlPluginAuth struct {
	Env string `yaml:"env"`
}
```

### type WfctlPluginEntry

WfctlPluginEntry is a single plugin declared in wfctl.yaml.

```go
type WfctlPluginEntry struct {
	Name	string			`yaml:"name"`
	Version	string			`yaml:"version,omitempty"`
	Source	string			`yaml:"source,omitempty"`
	Auth	*WfctlPluginAuth	`yaml:"auth,omitempty"`
	Verify	*WfctlPluginVerify	`yaml:"verify,omitempty"`
}
```

### type WfctlPluginVerify

WfctlPluginVerify holds sigstore/cosign identity for supply-chain verification.

```go
type WfctlPluginVerify struct {
	Identity string `yaml:"identity"`
}
```

### type WorkflowConfig

WorkflowConfig represents the overall configuration for the workflow engine

```go
type WorkflowConfig struct {
	Imports		[]string			`json:"imports,omitempty" yaml:"imports,omitempty"`
	Modules		[]ModuleConfig			`json:"modules" yaml:"modules"`
	Workflows	map[string]any			`json:"workflows" yaml:"workflows"`
	Triggers	map[string]any			`json:"triggers" yaml:"triggers"`
	Pipelines	map[string]any			`json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	Platform	map[string]any			`json:"platform,omitempty" yaml:"platform,omitempty"`
	Requires	*RequiresConfig			`json:"requires,omitempty" yaml:"requires,omitempty"`
	Plugins		*PluginsConfig			`json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Sidecars	[]SidecarConfig			`json:"sidecars,omitempty" yaml:"sidecars,omitempty"`
	Infrastructure	*InfrastructureConfig		`json:"infrastructure,omitempty" yaml:"infrastructure,omitempty"`
	Engine		*EngineConfig			`json:"engine,omitempty" yaml:"engine,omitempty"`
	CI		*CIConfig			`json:"ci,omitempty" yaml:"ci,omitempty"`
	Environments	map[string]*EnvironmentConfig	`json:"environments,omitempty" yaml:"environments,omitempty"`
	Secrets		*SecretsConfig			`json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Infra		*InfraConfig			`json:"infra,omitempty" yaml:"infra,omitempty"`
	SecretStores	map[string]*SecretStoreConfig	`json:"secretStores,omitempty" yaml:"secretStores,omitempty"`
	Services	map[string]*ServiceConfig	`json:"services,omitempty" yaml:"services,omitempty"`
	Mesh		*MeshConfig			`json:"mesh,omitempty" yaml:"mesh,omitempty"`
	Networking	*NetworkingConfig		`json:"networking,omitempty" yaml:"networking,omitempty"`
	Security	*SecurityConfig			`json:"security,omitempty" yaml:"security,omitempty"`
	ConfigDir	string				`json:"-" yaml:"-"`	// directory containing the config file, used for relative path resolution
}
```

## Functions

### func DeepMergeConfigs

DeepMergeConfigs merges override config on top of base config with override-wins semantics.
Unlike MergeConfigs (which uses primary-wins for fragment injection), this uses override-wins
for tenant config customization.

```go
func DeepMergeConfigs(base, override *WorkflowConfig) *WorkflowConfig
```

### func LoadFromBytes

LoadFromBytes loads a workflow configuration from a YAML byte slice.
This is useful for loading embedded configs (e.g. via //go:embed).
Note: imports are NOT processed because there is no file path context
to resolve relative import paths against.

```go
func LoadFromBytes(data []byte) (*WorkflowConfig, error)
```

### func LoadFromFile

LoadFromFile loads a workflow configuration from a YAML file.
If the config contains an "imports" field, referenced files are loaded
recursively and merged. The importing file's definitions take precedence
over imported ones for map-based fields (workflows, triggers, pipelines,
platform). Modules are concatenated with the main file's modules first.

```go
func LoadFromFile(filepath string) (*WorkflowConfig, error)
```

### func LoadFromString

LoadFromString loads a workflow configuration from a YAML string.
Note: imports are NOT processed when loading from a string because there is
no file path context to resolve relative import paths against.

```go
func LoadFromString(yamlContent string) (*WorkflowConfig, error)
```

### func MergeApplicationConfig

MergeApplicationConfig loads all workflow config files referenced by an
ApplicationConfig and merges them into a single WorkflowConfig. This is
useful for callers that need a single combined config (e.g., the server's
admin merge step) before passing it to the engine.

Module name conflicts across files are reported as errors.

```go
func MergeApplicationConfig(appCfg *ApplicationConfig) (*WorkflowConfig, error)
```

### func NewEmptyWorkflowConfig

NewEmptyWorkflowConfig creates a new empty workflow configuration

```go
func NewEmptyWorkflowConfig() *WorkflowConfig
```

## Methods

### func ResolveRelativePath

ResolveRelativePath resolves a path relative to the config file's directory.
If the path is absolute, it is returned as-is.

```go
func (c *WorkflowConfig) ResolveRelativePath(path string) string
```

### type WorkflowRef

WorkflowRef is a reference to a workflow config file within an application config.

```go
type WorkflowRef struct {
	// File is the path to the workflow YAML config file (relative to the application config).
	File	string	`json:"file" yaml:"file"`
	// Name is an optional override for the workflow's name within the application namespace.
	// If empty, the filename stem (without extension) is used.
	Name	string	`json:"name,omitempty" yaml:"name,omitempty"`
}
```

