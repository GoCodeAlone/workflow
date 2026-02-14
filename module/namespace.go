package module

import (
	"fmt"
	"regexp"
)

// ModuleNamespaceProvider defines the interface for module namespace functionality
type ModuleNamespaceProvider interface {
	// FormatName formats a module name with the namespace
	FormatName(baseName string) string

	// ResolveDependency formats a dependency name with the namespace
	ResolveDependency(dependencyName string) string

	// ResolveServiceName formats a service name with the namespace
	ResolveServiceName(serviceName string) string

	// ValidateModuleName checks if a module name conforms to namespace requirements
	ValidateModuleName(moduleName string) error
}

// StandardNamespace provides a standard implementation of ModuleNamespaceProvider
type StandardNamespace struct {
	prefix    string
	suffix    string
	nameRegex *regexp.Regexp
}

// NewStandardNamespace creates a new standard namespace with optional prefix and suffix
func NewStandardNamespace(prefix string, suffix string) *StandardNamespace {
	// Use a reasonable regex for valid module names
	validNameRegex := regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.]*[a-zA-Z0-9]$`)

	return &StandardNamespace{
		prefix:    prefix,
		suffix:    suffix,
		nameRegex: validNameRegex,
	}
}

// FormatName formats a module name with the namespace prefix/suffix
func (ns *StandardNamespace) FormatName(baseName string) string {
	switch {
	case ns.prefix != "" && ns.suffix != "":
		return ns.prefix + "-" + baseName + "-" + ns.suffix
	case ns.prefix != "":
		return ns.prefix + "-" + baseName
	case ns.suffix != "":
		return baseName + "-" + ns.suffix
	default:
		return baseName
	}
}

// ResolveDependency formats a dependency name with the namespace prefix/suffix
func (ns *StandardNamespace) ResolveDependency(dependencyName string) string {
	// Apply the same namespace to dependencies
	return ns.FormatName(dependencyName)
}

// ResolveServiceName formats a service name with the namespace prefix/suffix
func (ns *StandardNamespace) ResolveServiceName(serviceName string) string {
	// Apply the same namespace to service names
	return ns.FormatName(serviceName)
}

// ValidateModuleName checks if a module name conforms to namespace requirements
func (ns *StandardNamespace) ValidateModuleName(moduleName string) error {
	if !ns.nameRegex.MatchString(moduleName) {
		return fmt.Errorf("invalid module name '%s': must match pattern %s",
			moduleName, ns.nameRegex.String())
	}
	return nil
}

// WithValidation creates a validating namespace wrapper around any namespace provider
func WithValidation(base ModuleNamespaceProvider) *ValidatingNamespace {
	return &ValidatingNamespace{
		base: base,
	}
}

// ValidatingNamespace adds validation to any namespace provider
type ValidatingNamespace struct {
	base ModuleNamespaceProvider
}

// FormatName formats and validates a module name
func (vn *ValidatingNamespace) FormatName(baseName string) string {
	name := vn.base.FormatName(baseName)
	if err := vn.ValidateModuleName(name); err != nil {
		// Log the error but return the name anyway - we don't want to break existing code
		fmt.Printf("Warning: %v\n", err)
	}
	return name
}

// ResolveDependency formats and validates a dependency name
func (vn *ValidatingNamespace) ResolveDependency(dependencyName string) string {
	name := vn.base.ResolveDependency(dependencyName)
	if err := vn.ValidateModuleName(name); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}
	return name
}

// ResolveServiceName formats and validates a service name
func (vn *ValidatingNamespace) ResolveServiceName(serviceName string) string {
	name := vn.base.ResolveServiceName(serviceName)
	if err := vn.ValidateModuleName(name); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}
	return name
}

// ValidateModuleName validates a module name
func (vn *ValidatingNamespace) ValidateModuleName(moduleName string) error {
	return vn.base.ValidateModuleName(moduleName)
}

// Compatibility functions for existing code
// These will be deprecated once all code is migrated to use the interface

// ModuleNamespace represents the legacy struct (for backward compatibility)
type ModuleNamespace struct {
	provider ModuleNamespaceProvider
}

// NewModuleNamespace creates a new module namespace with optional prefix and suffix
func NewModuleNamespace(prefix string, suffix string) *ModuleNamespace {
	return &ModuleNamespace{
		provider: NewStandardNamespace(prefix, suffix),
	}
}

// FormatName formats a module name with the namespace prefix/suffix
func (ns *ModuleNamespace) FormatName(baseName string) string {
	return ns.provider.FormatName(baseName)
}

// ResolveDependency formats a dependency name with the namespace prefix/suffix
func (ns *ModuleNamespace) ResolveDependency(dependencyName string) string {
	return ns.provider.ResolveDependency(dependencyName)
}

// ResolveServiceName formats a service name with the namespace prefix/suffix
func (ns *ModuleNamespace) ResolveServiceName(serviceName string) string {
	return ns.provider.ResolveServiceName(serviceName)
}

// ModuleNamespaceProviderFunc provides a functional implementation of ModuleNamespaceProvider
type ModuleNamespaceProviderFunc struct {
	FormatNameFunc         func(baseName string) string
	ResolveDependencyFunc  func(dependencyName string) string
	ResolveServiceNameFunc func(serviceName string) string
	ValidateModuleNameFunc func(moduleName string) error
}

// FormatName formats a base name with the namespace
func (m ModuleNamespaceProviderFunc) FormatName(baseName string) string {
	if m.FormatNameFunc != nil {
		return m.FormatNameFunc(baseName)
	}
	return baseName
}

// ResolveDependency resolves a dependency name with the namespace
func (m ModuleNamespaceProviderFunc) ResolveDependency(dependencyName string) string {
	if m.ResolveDependencyFunc != nil {
		return m.ResolveDependencyFunc(dependencyName)
	}
	return m.FormatName(dependencyName)
}

// ResolveServiceName resolves a service name with the namespace
func (m ModuleNamespaceProviderFunc) ResolveServiceName(serviceName string) string {
	if m.ResolveServiceNameFunc != nil {
		return m.ResolveServiceNameFunc(serviceName)
	}
	return m.FormatName(serviceName)
}

// ValidateModuleName validates a module name
func (m ModuleNamespaceProviderFunc) ValidateModuleName(moduleName string) error {
	if m.ValidateModuleNameFunc != nil {
		return m.ValidateModuleNameFunc(moduleName)
	}
	return nil
}
