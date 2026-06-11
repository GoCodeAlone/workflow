# package capability

Import path: `github.com/GoCodeAlone/workflow/capability`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/capability

## Warnings

None

## Functions

### func DetectRequired

DetectRequired scans a WorkflowConfig and returns the set of capabilities needed.
It inspects module types, trigger types, and workflow types and returns a
deduplicated, sorted list of required capabilities.

```go
func DetectRequired(cfg *config.WorkflowConfig) []string
```

### func RegisterModuleTypeMapping

RegisterModuleTypeMapping records that a module type requires certain capabilities.

```go
func RegisterModuleTypeMapping(moduleType string, capabilities ...string)
```

### func RegisterTriggerTypeMapping

RegisterTriggerTypeMapping records trigger type to capability mapping.

```go
func RegisterTriggerTypeMapping(triggerType string, capabilities ...string)
```

### func RegisterWorkflowTypeMapping

RegisterWorkflowTypeMapping records workflow type to capability mapping.

```go
func RegisterWorkflowTypeMapping(workflowType string, capabilities ...string)
```

### func ResetMappings

ResetMappings clears all registered type-to-capability mappings. Intended for testing.

```go
func ResetMappings()
```

## Types

### type Contract

Contract defines a capability category that plugins can provide.
A contract specifies the Go interface that providers must implement
and documents the required method signatures for validation.

```go
type Contract struct {
	// Name is the capability identifier (e.g., "http-server", "message-broker").
	Name	string

	// Description is a human-readable explanation of what this capability provides.
	Description	string

	// InterfaceType is the reflect.Type of the Go interface that providers must implement.
	InterfaceType	reflect.Type

	// RequiredMethods lists the method signatures for documentation and validation.
	RequiredMethods	[]MethodSignature
}
```

### type MethodSignature

MethodSignature describes a single method on a capability interface.

```go
type MethodSignature struct {
	// Name is the method name.
	Name	string

	// Params lists the parameter type names (excluding the receiver).
	Params	[]string

	// Returns lists the return type names.
	Returns	[]string
}
```

### type ProviderEntry

ProviderEntry represents a plugin that implements a capability.

```go
type ProviderEntry struct {
	// PluginName is the name of the plugin providing this capability.
	PluginName	string

	// Priority determines provider selection order; higher values win.
	Priority	int

	// InterfaceImpl is the reflect.Type of the concrete type implementing the capability.
	InterfaceImpl	reflect.Type
}
```

### type Registry

Registry manages capability contracts and their providers.
It is safe for concurrent use.

```go
type Registry struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewRegistry

NewRegistry creates a new empty capability registry.

```go
func NewRegistry() *Registry
```

## Methods

### func ContractFor

ContractFor returns the contract for a capability name.
Returns false if the capability is not registered.

```go
func (r *Registry) ContractFor(capabilityName string) (*Contract, bool)
```

### func HasProvider

HasProvider returns true if at least one provider is registered for the capability.

```go
func (r *Registry) HasProvider(capabilityName string) bool
```

### func ListCapabilities

ListCapabilities returns a sorted list of all registered capability names.

```go
func (r *Registry) ListCapabilities() []string
```

### func ListProviders

ListProviders returns all providers registered for a capability.
Returns nil if no providers are registered.

```go
func (r *Registry) ListProviders(capabilityName string) []ProviderEntry
```

### func RegisterContract

RegisterContract adds a capability contract to the registry.
Returns an error if a contract with the same name already exists
but has a different InterfaceType.

```go
func (r *Registry) RegisterContract(c Contract) error
```

### func RegisterProvider

RegisterProvider registers a plugin as a provider for a capability.
Returns an error if the capability has not been registered.

```go
func (r *Registry) RegisterProvider(capabilityName, pluginName string, priority int, implType reflect.Type) error
```

### func Resolve

Resolve returns the highest-priority provider for a capability.
Returns an error if no providers are registered for the capability.

```go
func (r *Registry) Resolve(capabilityName string) (*ProviderEntry, error)
```

