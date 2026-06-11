# package sdk

Import path: `github.com/GoCodeAlone/workflow/plugin/sdk`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/plugin/sdk

## Warnings

None

## Synopsis

Package sdk contains authoring tools for Workflow plugins.

The package is used by wfctl plugin init and by tests that need to generate
realistic plugin repositories. TemplateGenerator creates a buildable plugin
project with plugin.json, Go code, release workflows, GoReleaser metadata,
and contract descriptor files. Runtime plugin binaries should use
github.com/GoCodeAlone/workflow/plugin/external/sdk.

This file hosts the plugin SDK manifest schema and helpers used by wfctl to
discover plugin capabilities.

The SDK manifest is intentionally additive over [plugin.PluginManifest];
it captures only the fields that wfctl validates before typed runtime
capability discovery. After the strict lifecycle cutover, the typed
CapabilitiesResponse.compute_plan_version declaration is authoritative and
wfctl accepts "v2" only.

## Functions

### func DiscoverWorkflowModuleRoot

```go
func DiscoverWorkflowModuleRoot(start string) string
```

### func ManifestSchemaJSON

ManifestSchemaJSON returns the raw JSON Schema bytes used to validate
SDK manifests. Exported for plugin authors and external tooling that
want to validate plugin.json without depending on this package's
ParseManifest entry point.

Returns a copy so callers cannot mutate the embedded schema; the
underlying slice from //go:embed is technically writable.

```go
func ManifestSchemaJSON() []byte
```

## Types

### type DocGenerator

DocGenerator produces markdown documentation from plugin manifests and contracts.

```go
type DocGenerator struct{}
```

## Functions

### func NewDocGenerator

NewDocGenerator creates a new DocGenerator.

```go
func NewDocGenerator() *DocGenerator
```

## Methods

### func GenerateContractDoc

GenerateContractDoc produces a markdown section documenting a field contract.

```go
func (g *DocGenerator) GenerateContractDoc(contract *dynamic.FieldContract) string
```

### func GeneratePluginDoc

GeneratePluginDoc produces a complete markdown document for a plugin.

```go
func (g *DocGenerator) GeneratePluginDoc(manifest *plugin.PluginManifest) string
```

### type GenerateOptions

GenerateOptions configures a generated plugin project.

The committed plugin.json version is always the stable "0.0.0" sentinel.
Release versions are injected into the generated binary from Git tags via
GoReleaser ldflags and surfaced at runtime through sdk.ResolveBuildVersion.

```go
type GenerateOptions struct {
	// Name is the plugin manifest name. It may include the workflow-plugin-
	// prefix; generated type names and step names use the normalized suffix.
	Name	string
	// Version is deprecated. Generated plugin.json files use the "0.0.0"
	// sentinel and release builds inject the real tag into the binary.
	Version	string
	// Author is required and is also used to build the default Go module path.
	Author	string
	// Description is written to plugin.json and README.md. When empty, a
	// generic description is used.
	Description	string
	// License is copied into plugin.json when supplied.
	License	string
	// OutputDir is the directory to create. It defaults to Name.
	OutputDir	string
	// WithContract adds the legacy dynamic field contract to plugin.json.
	WithContract	bool
	// LegacyContracts forces the older map-based step scaffold. By default the
	// generator emits strict typed contracts when it can resolve a local
	// workflow source checkout.
	LegacyContracts	bool
	// GoModule overrides the default github.com/<author>/workflow-plugin-<name>
	// module path.
	GoModule	string
	// WorkflowReplace is an optional local replace path for
	// github.com/GoCodeAlone/workflow.
	WorkflowReplace	string
	// MessageContracts are descriptor-only protobuf contracts to include in
	// plugin.contracts.json for plugins that publish messages without serving a
	// step for them.
	MessageContracts	[]MessageContract
}
```

### type IaCProvider

IaCProvider describes IaC-provider-specific manifest fields.

```go
type IaCProvider struct {
	// ComputePlanVersion is parse-time manifest metadata retained for plugin
	// authors and validation tooling. Runtime selection is strict: the typed
	// CapabilitiesResponse.compute_plan_version gate accepts "v2" only and
	// routes through wfctlhelpers.ApplyPlanWithHooks.
	// Schema-validated against the enum ["v1","v2"]; "" passes validation for
	// older manifests, but load-time typed capability validation rejects non-v2
	// providers.
	ComputePlanVersion string `json:"computePlanVersion,omitempty"`
}
```

### type Manifest

Manifest captures the SDK-level fields wfctl reads from plugin.json.
It is a strict subset of the full plugin.PluginManifest — only fields
that gate apply-time dispatch live here.

```go
type Manifest struct {
	// Name is the plugin name. Carried for diagnostics; the SDK schema
	// does not enforce shape (lowercase/hyphen rules live in plugin.PluginManifest).
	Name	string	`json:"name"`

	// IaCProvider holds IaC-provider-specific manifest fields. Empty
	// (zero-valued) when the plugin does not implement IaCProvider.
	IaCProvider	IaCProvider	`json:"iacProvider"`
}
```

## Functions

### func ParseManifest

ParseManifest validates raw plugin.json bytes against the SDK schema and
decodes them into a Manifest. Returns an error if the JSON is malformed
or violates the schema (e.g., iacProvider.computePlanVersion not in
{"v1","v2"}). Pure-additive: existing plugin.json files without an
iacProvider key parse cleanly with a zero-valued IaCProvider.

```go
func ParseManifest(data []byte) (*Manifest, error)
```

### type MessageContract

MessageContract describes a descriptor-only protobuf message contract.

```go
type MessageContract struct {
	ContractType	string		`json:"contractType"`
	ProtoPackage	string		`json:"protoPackage"`
	MessageNames	[]string	`json:"messageNames"`
	GoImportPath	string		`json:"goImportPath,omitempty"`
	SchemaDigest	string		`json:"schemaDigest,omitempty"`
	ProtocolVersion	string		`json:"protocolVersion,omitempty"`
}
```

## Methods

### func ContractDescriptor

ContractDescriptor converts c into the shared plugin contract descriptor.

```go
func (c MessageContract) ContractDescriptor() (*pb.ContractDescriptor, error)
```

### type TemplateGenerator

TemplateGenerator scaffolds new plugin projects with a manifest and component skeleton.

```go
type TemplateGenerator struct{}
```

## Functions

### func NewTemplateGenerator

NewTemplateGenerator creates a new TemplateGenerator.

```go
func NewTemplateGenerator() *TemplateGenerator
```

## Methods

### func Generate

Generate creates a new plugin directory with manifest and component skeleton,
plus a full project structure (cmd/, internal/, CI workflows, Makefile, README).

```go
func (g *TemplateGenerator) Generate(opts GenerateOptions) error
```

