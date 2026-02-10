package module

import (
	"testing"
)

func TestStandardNamespace(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		suffix         string
		baseName       string
		expectedResult string
	}{
		{
			name:           "NoNamespace",
			prefix:         "",
			suffix:         "",
			baseName:       "test-module",
			expectedResult: "test-module",
		},
		{
			name:           "PrefixOnly",
			prefix:         "dev",
			suffix:         "",
			baseName:       "test-module",
			expectedResult: "dev-test-module",
		},
		{
			name:           "SuffixOnly",
			prefix:         "",
			suffix:         "local",
			baseName:       "test-module",
			expectedResult: "test-module-local",
		},
		{
			name:           "BothPrefixAndSuffix",
			prefix:         "dev",
			suffix:         "local",
			baseName:       "test-module",
			expectedResult: "dev-test-module-local",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns := NewStandardNamespace(tc.prefix, tc.suffix)
			result := ns.FormatName(tc.baseName)
			if result != tc.expectedResult {
				t.Errorf("FormatName() = %v, want %v", result, tc.expectedResult)
			}
		})
	}
}

func TestValidatingNamespace(t *testing.T) {
	tests := []struct {
		name        string
		moduleName  string
		expectError bool
	}{
		{
			name:        "ValidName",
			moduleName:  "test-module",
			expectError: false,
		},
		{
			name:        "ValidNameWithNumbers",
			moduleName:  "test-module123",
			expectError: false,
		},
		{
			name:        "ValidNameWithUnderscore",
			moduleName:  "test_module",
			expectError: false,
		},
		{
			name:        "InvalidNameStartsWithHyphen",
			moduleName:  "-test-module",
			expectError: true,
		},
		{
			name:        "InvalidNameEndsWithHyphen",
			moduleName:  "test-module-",
			expectError: true,
		},
		{
			name:        "InvalidNameWithSpace",
			moduleName:  "test module",
			expectError: true,
		},
		{
			name:        "InvalidNameWithSpecialChars",
			moduleName:  "test@module",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a standard namespace for validation
			stdNs := NewStandardNamespace("", "")

			// Test direct validation
			err := stdNs.ValidateModuleName(tc.moduleName)
			if (err != nil) != tc.expectError {
				t.Errorf("ValidateModuleName() error = %v, expectError %v", err, tc.expectError)
			}

			// Test with validating namespace wrapper
			validateNs := WithValidation(stdNs)
			err = validateNs.ValidateModuleName(tc.moduleName)
			if (err != nil) != tc.expectError {
				t.Errorf("WithValidation().ValidateModuleName() error = %v, expectError %v", err, tc.expectError)
			}
		})
	}
}

func TestNamespaceIntegration(t *testing.T) {
	// Test with InMemoryMessageBroker
	t.Run("MessageBrokerWithNamespace", func(t *testing.T) {
		// Create a namespace with prefix
		ns := NewStandardNamespace("dev", "")

		// Create a broker with namespace
		broker := NewInMemoryMessageBrokerWithNamespace("message-broker", ns)

		// Verify the broker name is properly namespaced
		expected := "dev-message-broker"
		if broker.Name() != expected {
			t.Errorf("broker.Name() = %v, want %v", broker.Name(), expected)
		}
	})

	// Test backward compatibility
	t.Run("BackwardCompatibility", func(t *testing.T) {
		// Create legacy ModuleNamespace
		legacy := NewModuleNamespace("dev", "test")

		// Verify it still works
		expected := "dev-module-test"
		if result := legacy.FormatName("module"); result != expected {
			t.Errorf("legacy.FormatName() = %v, want %v", result, expected)
		}
	})
}

// TestCustomNamespaceImplementation tests that custom implementations of ModuleNamespaceProvider work correctly
func TestCustomNamespaceImplementation(t *testing.T) {
	// Define a custom namespace type for testing
	type CustomNamespace struct {
		environment string
	}

	// Define methods to implement ModuleNamespaceProvider interface
	FormatName := func(c *CustomNamespace, baseName string) string {
		return c.environment + ":" + baseName
	}

	ResolveDependency := func(c *CustomNamespace, dependencyName string) string {
		return FormatName(c, dependencyName)
	}

	ResolveServiceName := func(c *CustomNamespace, serviceName string) string {
		return FormatName(c, serviceName)
	}

	ValidateModuleName := func(c *CustomNamespace, moduleName string) error {
		// Simple validation - just check it's not empty
		if moduleName == "" {
			return &ScopeError{message: "module name cannot be empty"}
		}
		return nil
	}

	// Create the CustomNamespace and add the methods
	custom := &CustomNamespace{environment: "production"}

	// Create a wrapper that implements ModuleNamespaceProvider
	customNamespace := ModuleNamespaceProviderFunc{
		FormatNameFunc: func(baseName string) string {
			return FormatName(custom, baseName)
		},
		ResolveDependencyFunc: func(dependencyName string) string {
			return ResolveDependency(custom, dependencyName)
		},
		ResolveServiceNameFunc: func(serviceName string) string {
			return ResolveServiceName(custom, serviceName)
		},
		ValidateModuleNameFunc: func(moduleName string) error {
			return ValidateModuleName(custom, moduleName)
		},
	}

	// Test direct usage
	if result := customNamespace.FormatName("api"); result != "production:api" {
		t.Errorf("custom.FormatName() = %v, want %v", result, "production:api")
	}

	// Test with validation wrapper
	validated := WithValidation(customNamespace)
	if result := validated.FormatName("api"); result != "production:api" {
		t.Errorf("validated.FormatName() = %v, want %v", result, "production:api")
	}

	// Test with broker
	broker := NewInMemoryMessageBrokerWithNamespace("broker", customNamespace)
	if broker.Name() != "production:broker" {
		t.Errorf("broker.Name() = %v, want %v", broker.Name(), "production:broker")
	}
}

func TestStandardNamespace_ResolveDependency(t *testing.T) {
	ns := NewStandardNamespace("dev", "")
	result := ns.ResolveDependency("database")
	if result != "dev-database" {
		t.Errorf("expected 'dev-database', got %q", result)
	}
}

func TestModuleNamespace_ResolveDependency(t *testing.T) {
	ns := NewModuleNamespace("prod", "")
	result := ns.ResolveDependency("cache")
	if result != "prod-cache" {
		t.Errorf("expected 'prod-cache', got %q", result)
	}
}

func TestModuleNamespace_ResolveServiceName(t *testing.T) {
	ns := NewModuleNamespace("prod", "")
	result := ns.ResolveServiceName("auth")
	if result != "prod-auth" {
		t.Errorf("expected 'prod-auth', got %q", result)
	}
}

func TestValidatingNamespace_ResolveDependency(t *testing.T) {
	stdNs := NewStandardNamespace("dev", "")
	vn := WithValidation(stdNs)
	result := vn.ResolveDependency("service")
	if result != "dev-service" {
		t.Errorf("expected 'dev-service', got %q", result)
	}
}

func TestValidatingNamespace_ResolveServiceName(t *testing.T) {
	stdNs := NewStandardNamespace("dev", "")
	vn := WithValidation(stdNs)
	result := vn.ResolveServiceName("auth")
	if result != "dev-auth" {
		t.Errorf("expected 'dev-auth', got %q", result)
	}
}

func TestModuleNamespaceProviderFunc_Defaults(t *testing.T) {
	// Test with nil functions - should use defaults
	provider := ModuleNamespaceProviderFunc{}

	// FormatName with nil func returns baseName as-is
	if result := provider.FormatName("test"); result != "test" {
		t.Errorf("expected 'test', got %q", result)
	}

	// ResolveDependency with nil func delegates to FormatName
	if result := provider.ResolveDependency("dep"); result != "dep" {
		t.Errorf("expected 'dep', got %q", result)
	}

	// ResolveServiceName with nil func delegates to FormatName
	if result := provider.ResolveServiceName("svc"); result != "svc" {
		t.Errorf("expected 'svc', got %q", result)
	}

	// ValidateModuleName with nil func returns nil
	if err := provider.ValidateModuleName("anything"); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// ScopeError represents a namespace error
type ScopeError struct {
	message string
}

func (e *ScopeError) Error() string {
	return e.message
}
