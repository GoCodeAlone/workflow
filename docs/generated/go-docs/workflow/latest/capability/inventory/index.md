# package inventory

Import path: `github.com/GoCodeAlone/workflow/capability/inventory`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/capability/inventory

## Warnings

None

## Synopsis

Package inventory builds generated capability inventories for Workflow
plugins, registries, and applications.

## Types

### type AppOptions

AppOptions controls application capability profile generation.

```go
type AppOptions struct {
	ManifestPath	string
	WorkflowPaths	[]string
	PluginDir	string
	LockfilePath	string
	TaxonomyPath	string
	GeneratedAt	time.Time
}
```

### type AppProfile

AppProfile is the top-level application capability report.

```go
type AppProfile struct {
	Metadata	Metadata	`json:"metadata"`
	Usage		[]Usage		`json:"usage"`
	Findings	[]Finding	`json:"findings,omitempty"`
}
```

## Functions

### func CollectApp

CollectApp builds a capability profile for one application from Workflow-owned files.

```go
func CollectApp(ctx context.Context, opts AppOptions) (*AppProfile, error)
```

### type Capability

Capability is a product-level capability row.

```go
type Capability struct {
	ID		string		`json:"id"`
	Category	string		`json:"category"`
	Name		string		`json:"name"`
	Description	string		`json:"description,omitempty"`
	Lifecycle	string		`json:"lifecycle,omitempty"`
	Tags		[]string	`json:"tags,omitempty"`
	Providers	[]Provider	`json:"providers,omitempty"`
	Evidence	[]Evidence	`json:"evidence,omitempty"`
	Findings	[]Finding	`json:"findings,omitempty"`
}
```

### type CapabilityCrossrefs

CapabilityCrossrefs indexes capability-to-plugin and plugin-to-capability links.

```go
type CapabilityCrossrefs struct {
	Metadata	Metadata			`json:"metadata"`
	Plugins		map[string]PluginReference	`json:"plugins"`
	Capabilities	map[string]CapabilityReference	`json:"capabilities"`
}
```

## Functions

### func BuildCapabilityCrossrefs

BuildCapabilityCrossrefs builds a graph index for docs and agents.

```go
func BuildCapabilityCrossrefs(inv *Inventory) *CapabilityCrossrefs
```

### type CapabilityReference

CapabilityReference describes one capability's provider names.

```go
type CapabilityReference struct {
	ID		string		`json:"id"`
	Category	string		`json:"category,omitempty"`
	Name		string		`json:"name,omitempty"`
	Providers	[]string	`json:"providers,omitempty"`
}
```

### type Catalog

Catalog is the docs-facing capability report derived from Inventory.

```go
type Catalog struct {
	Metadata	Metadata		`json:"metadata"`
	Capabilities	[]CatalogCapability	`json:"capabilities"`
	Findings	[]Finding		`json:"findings,omitempty"`
}
```

## Functions

### func BuildCatalog

BuildCatalog converts the evidence-rich ecosystem inventory into the public
docs catalog. Raw uncategorized rows stay in the ecosystem inventory and
maintainer counts instead of dominating user-facing docs.

```go
func BuildCatalog(inv *Inventory) *Catalog
```

### type CatalogCapability

CatalogCapability is a public capability row intended for docs navigation.

```go
type CatalogCapability struct {
	ID		string		`json:"id"`
	Category	string		`json:"category"`
	Name		string		`json:"name"`
	Description	string		`json:"description,omitempty"`
	Lifecycle	string		`json:"lifecycle,omitempty"`
	Tags		[]string	`json:"tags,omitempty"`
	Providers	[]Provider	`json:"providers,omitempty"`
}
```

### type EcosystemOptions

EcosystemOptions controls ecosystem capability inventory generation.

```go
type EcosystemOptions struct {
	RegistryDir	string
	RepoRoot	string
	TaxonomyPath	string
	GeneratedAt	time.Time
	WorkflowVersion	string
}
```

### type Evidence

Evidence points to the source that supports a capability or usage row.

```go
type Evidence struct {
	SourceKind	string	`json:"sourceKind"`
	SourcePath	string	`json:"sourcePath,omitempty"`
	JSONPath	string	`json:"jsonPath,omitempty"`
	Detail		string	`json:"detail,omitempty"`
}
```

### type Finding

Finding is a warning or error produced during inventory generation.

```go
type Finding struct {
	Level		string		`json:"level"`
	Code		string		`json:"code"`
	Message		string		`json:"message"`
	CapabilityID	string		`json:"capabilityId,omitempty"`
	Evidence	[]Evidence	`json:"evidence,omitempty"`
}
```

## Functions

### func CheckApp

CheckApp returns deterministic policy findings for an application profile.

```go
func CheckApp(profile *AppProfile) []Finding
```

### type Inventory

Inventory is the top-level ecosystem capability report.

```go
type Inventory struct {
	Metadata	Metadata	`json:"metadata"`
	Capabilities	[]Capability	`json:"capabilities"`
	Findings	[]Finding	`json:"findings,omitempty"`
}
```

## Functions

### func CollectEcosystem

CollectEcosystem reads registry and local plugin manifests into a capability inventory.

```go
func CollectEcosystem(opts EcosystemOptions) (*Inventory, error)
```

### type Metadata

Metadata describes how an inventory artifact was generated.

```go
type Metadata struct {
	Generator	string		`json:"generator"`
	GeneratedAt	string		`json:"generatedAt,omitempty"`
	WorkflowVersion	string		`json:"workflowVersion,omitempty"`
	TaxonomyVersion	string		`json:"taxonomyVersion,omitempty"`
	TaxonomyDigest	string		`json:"taxonomyDigest,omitempty"`
	RegistrySource	string		`json:"registrySource,omitempty"`
	LocalRepoRoot	string		`json:"localRepoRoot,omitempty"`
	Counts		map[string]int	`json:"counts,omitempty"`
}
```

### type PluginReference

PluginReference describes one plugin/provider in the cross-reference graph.

```go
type PluginReference struct {
	Name		string		`json:"name"`
	Kind		string		`json:"kind,omitempty"`
	Version		string		`json:"version,omitempty"`
	ReleaseStatus	string		`json:"releaseStatus,omitempty"`
	Source		string		`json:"source,omitempty"`
	Capabilities	[]string	`json:"capabilities,omitempty"`
	Dependencies	[]string	`json:"dependencies,omitempty"`
}
```

### type Provider

Provider describes a plugin, package, or provider that supplies a capability.

```go
type Provider struct {
	Name		string		`json:"name"`
	Kind		string		`json:"kind"`
	Version		string		`json:"version,omitempty"`
	ReleaseStatus	string		`json:"releaseStatus,omitempty"`
	Source		string		`json:"source,omitempty"`
	Capabilities	[]string	`json:"capabilities,omitempty"`
	Dependencies	[]string	`json:"dependencies,omitempty"`
}
```

### type Taxonomy

Taxonomy maps raw Workflow/plugin type declarations to product capabilities.

```go
type Taxonomy struct {
	Version		string			`json:"version" yaml:"version"`
	Capabilities	[]TaxonomyCapability	`json:"capabilities" yaml:"capabilities"`
	// contains filtered or unexported fields
}
```

## Functions

### func LoadTaxonomy

LoadTaxonomy reads and validates a taxonomy YAML file.

```go
func LoadTaxonomy(path string) (*Taxonomy, error)
```

## Methods

### func ByID

ByID returns a capability by stable taxonomy ID.

```go
func (t *Taxonomy) ByID(id string) (*TaxonomyCapability, bool)
```

### func Digest

Digest returns the SHA-256 digest of the taxonomy source file.

```go
func (t *Taxonomy) Digest() string
```

### func MatchType

MatchType returns the taxonomy capability for a raw type declaration.

```go
func (t *Taxonomy) MatchType(kind, value string) (*TaxonomyCapability, bool)
```

### type TaxonomyAliases

TaxonomyAliases lists raw declaration names that map to a capability.

```go
type TaxonomyAliases struct {
	ModuleTypes		[]string	`json:"moduleTypes,omitempty" yaml:"moduleTypes,omitempty"`
	BuiltinModuleTypes	[]string	`json:"builtinModuleTypes,omitempty" yaml:"builtinModuleTypes,omitempty"`
	StepTypes		[]string	`json:"stepTypes,omitempty" yaml:"stepTypes,omitempty"`
	BuiltinStepTypes	[]string	`json:"builtinStepTypes,omitempty" yaml:"builtinStepTypes,omitempty"`
	TriggerTypes		[]string	`json:"triggerTypes,omitempty" yaml:"triggerTypes,omitempty"`
	BuiltinTriggerTypes	[]string	`json:"builtinTriggerTypes,omitempty" yaml:"builtinTriggerTypes,omitempty"`
	WorkflowTypes		[]string	`json:"workflowTypes,omitempty" yaml:"workflowTypes,omitempty"`
	BuiltinWorkflowTypes	[]string	`json:"builtinWorkflowTypes,omitempty" yaml:"builtinWorkflowTypes,omitempty"`
	WiringHooks		[]string	`json:"wiringHooks,omitempty" yaml:"wiringHooks,omitempty"`
	BuiltinWiringHooks	[]string	`json:"builtinWiringHooks,omitempty" yaml:"builtinWiringHooks,omitempty"`
	IaCServices		[]string	`json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
	IaCStateBackends	[]string	`json:"iacStateBackends,omitempty" yaml:"iacStateBackends,omitempty"`
	BuiltinIaCStateBackends	[]string	`json:"builtinIaCStateBackends,omitempty" yaml:"builtinIaCStateBackends,omitempty"`
	Providers		[]string	`json:"providers,omitempty" yaml:"providers,omitempty"`
	Plugins			[]string	`json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Keywords		[]string	`json:"keywords,omitempty" yaml:"keywords,omitempty"`
}
```

### type TaxonomyCapability

TaxonomyCapability is one product capability declared in the taxonomy file.

```go
type TaxonomyCapability struct {
	ID		string		`json:"id" yaml:"id"`
	Category	string		`json:"category" yaml:"category"`
	Name		string		`json:"name" yaml:"name"`
	Description	string		`json:"description,omitempty" yaml:"description,omitempty"`
	Lifecycle	string		`json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Aliases		TaxonomyAliases	`json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Tags		[]string	`json:"tags,omitempty" yaml:"tags,omitempty"`
}
```

### type Usage

Usage describes a capability used by an application.

```go
type Usage struct {
	CapabilityID	string		`json:"capabilityId"`
	Category	string		`json:"category,omitempty"`
	Name		string		`json:"name,omitempty"`
	Mode		string		`json:"mode"`
	Confidence	string		`json:"confidence,omitempty"`
	Evidence	[]Evidence	`json:"evidence,omitempty"`
	Findings	[]Finding	`json:"findings,omitempty"`
}
```

