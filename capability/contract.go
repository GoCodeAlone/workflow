package capability

import "reflect"

// Contract defines a capability category that plugins can provide.
// A contract specifies the Go interface that providers must implement
// and documents the required method signatures for validation.
type Contract struct {
	// Name is the capability identifier (e.g., "http-server", "message-broker").
	Name string

	// Description is a human-readable explanation of what this capability provides.
	Description string

	// InterfaceType is the reflect.Type of the Go interface that providers must implement.
	InterfaceType reflect.Type

	// RequiredMethods lists the method signatures for documentation and validation.
	RequiredMethods []MethodSignature
}

// MethodSignature describes a single method on a capability interface.
type MethodSignature struct {
	// Name is the method name.
	Name string

	// Params lists the parameter type names (excluding the receiver).
	Params []string

	// Returns lists the return type names.
	Returns []string
}
