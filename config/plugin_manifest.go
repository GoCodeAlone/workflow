package config

import (
	"fmt"
	"strings"
)

// CredentialConcurrencyMode declares how callers must serialize credential
// issuance operations for a provider-owned source.
type CredentialConcurrencyMode string

const (
	CredentialConcurrencyProviderIdempotent CredentialConcurrencyMode = "provider_idempotent"
	CredentialConcurrencySingleWriter       CredentialConcurrencyMode = "single_writer_required"
)

// ProviderDeclarations is the shared validation shape for optional
// provider-owned capabilities. Each manifest model uses these declaration
// types so registry installation and SDK parsing preserve them unchanged.
type ProviderDeclarations struct {
	CredentialSources   []CredentialSourceDecl   `json:"credentialSources,omitempty" yaml:"credentialSources,omitempty"`
	CredentialResolvers []CredentialResolverDecl `json:"credentialResolvers,omitempty" yaml:"credentialResolvers,omitempty"`
	KubernetesBackends  []KubernetesBackendDecl  `json:"kubernetesBackends,omitempty" yaml:"kubernetesBackends,omitempty"`
	ContainerRegistries []ContainerRegistryDecl  `json:"containerRegistries,omitempty" yaml:"containerRegistries,omitempty"`
	SecretStores        []SecretStoreDecl        `json:"secretStores,omitempty" yaml:"secretStores,omitempty"`
	ConsumesContracts   []ConsumedContractDecl   `json:"consumesContracts,omitempty" yaml:"consumesContracts,omitempty"`
}

// CredentialSourceDecl declares a provider-owned credential issuer.
type CredentialSourceDecl struct {
	Source          string                    `json:"source" yaml:"source"`
	ConcurrencyMode CredentialConcurrencyMode `json:"concurrencyMode" yaml:"concurrencyMode"`
	Outputs         []CredentialOutputDecl    `json:"outputs" yaml:"outputs"`
	IdentifierKey   string                    `json:"identifierKey" yaml:"identifierKey"`
}

// CredentialOutputDecl declares one value returned by a credential issuer.
// A nil Sensitive value semantically defaults to true; a pointer preserves the
// distinction between omitted and explicitly false across manifest round trips.
type CredentialOutputDecl struct {
	Key       string `json:"key" yaml:"key"`
	Sensitive *bool  `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
}

// IsSensitive returns the effective sensitivity, defaulting omitted values to
// true so callers fail closed.
func (d CredentialOutputDecl) IsSensitive() bool {
	return d.Sensitive == nil || *d.Sensitive
}

// CredentialResolverDecl declares credential encodings understood by a
// provider-owned resolver.
type CredentialResolverDecl struct {
	Provider        string   `json:"provider" yaml:"provider"`
	CredentialTypes []string `json:"credentialTypes" yaml:"credentialTypes"`
}

// KubernetesBackendDecl binds a backend name to its ResourceDriver resource
// type.
type KubernetesBackendDecl struct {
	Name         string `json:"name" yaml:"name"`
	ResourceType string `json:"resourceType" yaml:"resourceType"`
}

// ContainerRegistryDecl declares the operations supported for one exact
// container-registry type.
type ContainerRegistryDecl struct {
	Type       string   `json:"type" yaml:"type"`
	Operations []string `json:"operations" yaml:"operations"`
}

// SecretStoreDecl declares the read-only operations and scopes supported for
// one exact secret-store type.
type SecretStoreDecl struct {
	Type       string   `json:"type" yaml:"type"`
	Operations []string `json:"operations" yaml:"operations"`
	Scopes     []string `json:"scopes" yaml:"scopes"`
}

// ProtocolVersionRange is an inclusive compatible protocol range.
type ProtocolVersionRange struct {
	Min uint32 `json:"min" yaml:"min"`
	Max uint32 `json:"max" yaml:"max"`
}

// ConsumedContractDecl declares an external contract and the inclusive
// protocol versions a plugin can consume.
type ConsumedContractDecl struct {
	ID       string               `json:"id" yaml:"id"`
	Protocol ProtocolVersionRange `json:"protocol" yaml:"protocol"`
}

// Validate checks all manifest-local provider declaration invariants. Global
// uniqueness across installed plugins is enforced later by routing.
func (d ProviderDeclarations) Validate() error {
	if err := validateCredentialSources(d.CredentialSources); err != nil {
		return err
	}
	if err := validateCredentialResolvers(d.CredentialResolvers); err != nil {
		return err
	}
	if err := validateKubernetesBackends(d.KubernetesBackends); err != nil {
		return err
	}
	if err := validateContainerRegistries(d.ContainerRegistries); err != nil {
		return err
	}
	if err := validateSecretStores(d.SecretStores); err != nil {
		return err
	}
	return validateConsumedContracts(d.ConsumesContracts)
}

func validateCredentialSources(declarations []CredentialSourceDecl) error {
	seenSources := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Source) == "" {
			return fmt.Errorf("provider declarations: credential source is required")
		}
		if _, exists := seenSources[declaration.Source]; exists {
			return fmt.Errorf("provider declarations: duplicate credential source %q", declaration.Source)
		}
		seenSources[declaration.Source] = struct{}{}
		switch declaration.ConcurrencyMode {
		case CredentialConcurrencyProviderIdempotent, CredentialConcurrencySingleWriter:
		default:
			return fmt.Errorf("provider declarations: credential source %q has unsupported concurrency mode %q", declaration.Source, declaration.ConcurrencyMode)
		}
		if len(declaration.Outputs) == 0 {
			return fmt.Errorf("provider declarations: credential source %q output is required", declaration.Source)
		}
		outputs := make(map[string]struct{}, len(declaration.Outputs))
		for _, output := range declaration.Outputs {
			if strings.TrimSpace(output.Key) == "" {
				return fmt.Errorf("provider declarations: credential source %q output key is required", declaration.Source)
			}
			if _, exists := outputs[output.Key]; exists {
				return fmt.Errorf("provider declarations: credential source %q has duplicate output %q", declaration.Source, output.Key)
			}
			outputs[output.Key] = struct{}{}
		}
		if strings.TrimSpace(declaration.IdentifierKey) == "" {
			return fmt.Errorf("provider declarations: credential source %q identifierKey is required", declaration.Source)
		}
		if _, exists := outputs[declaration.IdentifierKey]; !exists {
			return fmt.Errorf("provider declarations: credential source %q identifierKey %q must name a declared output", declaration.Source, declaration.IdentifierKey)
		}
	}
	return nil
}

var resolverCredentialTypes = map[string]map[string]struct{}{
	"aws":   stringSet("static", "env", "profile", "role_arn"),
	"gcp":   stringSet("static", "env", "service_account_json", "service_account_key", "workload_identity", "application_default"),
	"azure": stringSet("static", "env", "client_credentials", "managed_identity", "cli"),
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func validateCredentialResolvers(declarations []CredentialResolverDecl) error {
	seenProviders := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Provider) == "" {
			return fmt.Errorf("provider declarations: credential resolver provider is required")
		}
		allowed, known := resolverCredentialTypes[declaration.Provider]
		if !known {
			return fmt.Errorf("provider declarations: unsupported resolver provider %q", declaration.Provider)
		}
		if _, exists := seenProviders[declaration.Provider]; exists {
			return fmt.Errorf("provider declarations: duplicate credential resolver provider %q", declaration.Provider)
		}
		seenProviders[declaration.Provider] = struct{}{}
		if len(declaration.CredentialTypes) == 0 {
			return fmt.Errorf("provider declarations: credential resolver %q credential type is required", declaration.Provider)
		}
		seenTypes := make(map[string]struct{}, len(declaration.CredentialTypes))
		for _, credentialType := range declaration.CredentialTypes {
			if _, exists := seenTypes[credentialType]; exists {
				return fmt.Errorf("provider declarations: credential resolver %q has duplicate credential type %q", declaration.Provider, credentialType)
			}
			seenTypes[credentialType] = struct{}{}
			if _, supported := allowed[credentialType]; !supported {
				return fmt.Errorf("provider declarations: credential resolver %q has unsupported credential type %q", declaration.Provider, credentialType)
			}
		}
	}
	return nil
}

func validateKubernetesBackends(declarations []KubernetesBackendDecl) error {
	seenNames := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Name) == "" {
			return fmt.Errorf("provider declarations: kubernetes backend name is required")
		}
		if declaration.Name == "kind" || declaration.Name == "k3s" {
			return fmt.Errorf("provider declarations: kubernetes backend name %q is reserved for core", declaration.Name)
		}
		if _, exists := seenNames[declaration.Name]; exists {
			return fmt.Errorf("provider declarations: duplicate kubernetes backend %q", declaration.Name)
		}
		seenNames[declaration.Name] = struct{}{}
		if strings.TrimSpace(declaration.ResourceType) == "" {
			return fmt.Errorf("provider declarations: kubernetes backend %q resourceType is required", declaration.Name)
		}
	}
	return nil
}

func validateContainerRegistries(declarations []ContainerRegistryDecl) error {
	allowedOperations := stringSet("login", "logout", "push", "prune")
	seenTypes := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Type) == "" {
			return fmt.Errorf("provider declarations: container registry type is required")
		}
		if _, exists := seenTypes[declaration.Type]; exists {
			return fmt.Errorf("provider declarations: duplicate container registry type %q", declaration.Type)
		}
		seenTypes[declaration.Type] = struct{}{}
		if err := validateOperations("container registry", declaration.Type, declaration.Operations, allowedOperations); err != nil {
			return err
		}
	}
	return nil
}

func validateSecretStores(declarations []SecretStoreDecl) error {
	allowedOperations := stringSet("get", "list", "stat_all", "check_access")
	seenTypes := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Type) == "" {
			return fmt.Errorf("provider declarations: secret store type is required")
		}
		if _, exists := seenTypes[declaration.Type]; exists {
			return fmt.Errorf("provider declarations: duplicate secret store type %q", declaration.Type)
		}
		seenTypes[declaration.Type] = struct{}{}
		if err := validateOperations("secret store", declaration.Type, declaration.Operations, allowedOperations); err != nil {
			return err
		}
		if len(declaration.Scopes) == 0 {
			return fmt.Errorf("provider declarations: secret store scope is required for %q", declaration.Type)
		}
		seenScopes := make(map[string]struct{}, len(declaration.Scopes))
		for _, scope := range declaration.Scopes {
			if strings.TrimSpace(scope) == "" {
				return fmt.Errorf("provider declarations: secret store scope is required for %q", declaration.Type)
			}
			if _, exists := seenScopes[scope]; exists {
				return fmt.Errorf("provider declarations: secret store %q has duplicate secret store scope %q", declaration.Type, scope)
			}
			seenScopes[scope] = struct{}{}
		}
	}
	return nil
}

func validateOperations(kind, declarationType string, operations []string, allowed map[string]struct{}) error {
	if len(operations) == 0 {
		return fmt.Errorf("provider declarations: %s operation is required for %q", kind, declarationType)
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if _, exists := seen[operation]; exists {
			return fmt.Errorf("provider declarations: %s %q has duplicate %s operation %q", kind, declarationType, kind, operation)
		}
		seen[operation] = struct{}{}
		if _, supported := allowed[operation]; !supported {
			return fmt.Errorf("provider declarations: %s %q has unsupported %s operation %q", kind, declarationType, kind, operation)
		}
	}
	return nil
}

func validateConsumedContracts(declarations []ConsumedContractDecl) error {
	seenIDs := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.ID) == "" {
			return fmt.Errorf("provider declarations: consumed contract id is required")
		}
		if _, exists := seenIDs[declaration.ID]; exists {
			return fmt.Errorf("provider declarations: duplicate consumed contract %q", declaration.ID)
		}
		seenIDs[declaration.ID] = struct{}{}
		if declaration.Protocol.Min == 0 {
			return fmt.Errorf("provider declarations: consumed contract %q protocol minimum must be greater than zero", declaration.ID)
		}
		if declaration.Protocol.Max == 0 {
			return fmt.Errorf("provider declarations: consumed contract %q protocol maximum must be greater than zero", declaration.ID)
		}
		if declaration.Protocol.Min > declaration.Protocol.Max {
			return fmt.Errorf("provider declarations: consumed contract %q protocol range minimum %d exceeds maximum %d", declaration.ID, declaration.Protocol.Min, declaration.Protocol.Max)
		}
	}
	return nil
}

// PluginInfraRequirements maps module types to their infrastructure needs.
type PluginInfraRequirements map[string]*ModuleInfraSpec

// PluginInfraRequirementsV2 maps module types to provider-neutral IaC
// requirements. The values intentionally use manifest-friendly strings; the
// iac/requirements package owns typed enum validation and protobuf conversion.
type PluginInfraRequirementsV2 map[string]*ModuleInfraSpecV2

// ModuleInfraSpec declares what a module type requires.
type ModuleInfraSpec struct {
	Requires []InfraRequirement `json:"requires" yaml:"requires"`
}

// ModuleInfraSpecV2 declares typed requirement metadata for a module type.
type ModuleInfraSpecV2 struct {
	Requires []ModuleInfraRequirementV2 `json:"requires" yaml:"requires"`
}

// InfraRequirement is a single infrastructure dependency.
type InfraRequirement struct {
	Type        string   `json:"type" yaml:"type"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	DockerImage string   `json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	Ports       []int    `json:"ports,omitempty" yaml:"ports,omitempty"`
	Secrets     []string `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Providers   []string `json:"providers,omitempty" yaml:"providers,omitempty"`
	Optional    bool     `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// ModuleInfraRequirementV2 is the plugin.json authoring shape for derived-IaC
// requirements. It mirrors the portable fields in plugin/external/proto/iac.proto
// using strings so manifests stay easy to read and preserve unknown future
// provider details under Parameters.
type ModuleInfraRequirementV2 struct {
	Key                   string         `json:"key" yaml:"key"`
	Kind                  string         `json:"kind" yaml:"kind"`
	Source                string         `json:"source,omitempty" yaml:"source,omitempty"`
	ResourceTypeHint      string         `json:"resourceTypeHint,omitempty" yaml:"resourceTypeHint,omitempty"`
	Environment           string         `json:"environment,omitempty" yaml:"environment,omitempty"`
	Runtimes              []string       `json:"runtimes,omitempty" yaml:"runtimes,omitempty"`
	TelemetrySignals      []string       `json:"telemetrySignals,omitempty" yaml:"telemetrySignals,omitempty"`
	ObservabilityBackends []string       `json:"observabilityBackends,omitempty" yaml:"observabilityBackends,omitempty"`
	DeploymentModes       []string       `json:"deploymentModes,omitempty" yaml:"deploymentModes,omitempty"`
	VendorFeatures        []string       `json:"vendorFeatures,omitempty" yaml:"vendorFeatures,omitempty"`
	Parameters            map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// PluginManifestFile represents the full plugin.json manifest.
type PluginManifestFile struct {
	Name                      string                    `json:"name" yaml:"name"`
	Version                   string                    `json:"version" yaml:"version"`
	Description               string                    `json:"description" yaml:"description"`
	Capabilities              PluginCapabilities        `json:"capabilities" yaml:"capabilities"`
	ModuleInfraRequirements   PluginInfraRequirements   `json:"moduleInfraRequirements,omitempty" yaml:"moduleInfraRequirements,omitempty"`
	ModuleInfraRequirementsV2 PluginInfraRequirementsV2 `json:"moduleInfraRequirementsV2,omitempty" yaml:"moduleInfraRequirementsV2,omitempty"`
	CredentialSources         []CredentialSourceDecl    `json:"credentialSources,omitempty" yaml:"credentialSources,omitempty"`
	CredentialResolvers       []CredentialResolverDecl  `json:"credentialResolvers,omitempty" yaml:"credentialResolvers,omitempty"`
	KubernetesBackends        []KubernetesBackendDecl   `json:"kubernetesBackends,omitempty" yaml:"kubernetesBackends,omitempty"`
	ContainerRegistries       []ContainerRegistryDecl   `json:"containerRegistries,omitempty" yaml:"containerRegistries,omitempty"`
	SecretStores              []SecretStoreDecl         `json:"secretStores,omitempty" yaml:"secretStores,omitempty"`
	ConsumesContracts         []ConsumedContractDecl    `json:"consumesContracts,omitempty" yaml:"consumesContracts,omitempty"`
}

// PluginCapabilities describes what module, step, trigger types, build hooks,
// and CLI commands a plugin provides.
type PluginCapabilities struct {
	ModuleTypes   []string               `json:"moduleTypes" yaml:"moduleTypes"`
	StepTypes     []string               `json:"stepTypes" yaml:"stepTypes"`
	TriggerTypes  []string               `json:"triggerTypes" yaml:"triggerTypes"`
	BuildHooks    []BuildHookDeclaration `json:"buildHooks,omitempty" yaml:"buildHooks,omitempty"`
	OnHookFailure string                 `json:"onHookFailure,omitempty" yaml:"onHookFailure,omitempty"` // fail | warn | skip
	// PortPaths is a list of dot-notation JSON paths into module config that
	// contain port values (e.g. ["config.api_port", "config.grpc_port"]).
	// The port introspector walks these paths for modules of any type declared by this plugin.
	PortPaths   []string                `json:"portPaths,omitempty" yaml:"portPaths,omitempty"`
	CLICommands []CLICommandDeclaration `json:"cliCommands,omitempty" yaml:"cliCommands,omitempty"`
}

// BuildHookDeclaration registers a plugin as a handler for a specific hook event.
type BuildHookDeclaration struct {
	Event          string `json:"event" yaml:"event"`
	Priority       int    `json:"priority" yaml:"priority"` // lower = runs first
	Description    string `json:"description,omitempty" yaml:"description,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"` // 0 = use global default
}

// CLICommandDeclaration registers a plugin as the handler for a top-level wfctl subcommand.
type CLICommandDeclaration struct {
	Name             string `json:"name" yaml:"name"`
	Description      string `json:"description,omitempty" yaml:"description,omitempty"`
	FlagsPassthrough bool   `json:"flagsPassthrough,omitempty" yaml:"flagsPassthrough,omitempty"`
}
