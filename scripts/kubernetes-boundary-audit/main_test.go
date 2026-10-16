package main

import (
	"errors"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectRootCurrentMainContract(t *testing.T) {
	root := os.Getenv("WORKFLOW_AUDIT_CURRENT_ROOT")
	if root == "" {
		var err error
		root, err = filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
	}
	assertClean(t, inspectRoot(root, false))
}

func legacyCurrentFixture(t *testing.T) string {
	t.Helper()
	root := writeFixture(t, false)
	start := strings.Index(validRegistrySource, "func (m *PlatformKubernetes) Init(")
	end := strings.Index(validRegistrySource[start:], "\n}") + start + 2
	if start < 0 || end <= start {
		t.Fatal("missing fixture Init boundary")
	}
	writeTestFile(t, root, registryFile, validRegistrySource[:start]+legacyCurrentInitSource+validRegistrySource[end:])
	writeTestFile(t, root, pluginRegistryFile, "package module\n"+legacyCurrentReservedMapSource+"\n"+legacyCurrentRegisterSource+"\n")
	return root
}

func TestInspectRootExactLegacyCurrentContract(t *testing.T) {
	assertClean(t, inspectRoot(legacyCurrentFixture(t), true))
}

func TestInspectRootRejectsLegacyCurrentContractDrift(t *testing.T) {
	for _, test := range []struct{ name, path, before, after string }{
		{"extra reserved provider", pluginRegistryFile, "\"aks\":  {},", "\"aks\":  {}, \"digitalocean\": {},"},
		{"missing reserved type", pluginRegistryFile, "\"k3s\":  {},", ""},
		{"registration guard removed", pluginRegistryFile, "if _, reserved := reservedKubernetesBackendTypes[name]; reserved {", "if false {"},
		{"normalization changed", pluginRegistryFile, "name = strings.TrimSpace(name)", "name = \"kind\""},
		{"early registration return", pluginRegistryFile, "r.mu.Lock()", "return nil\n\tr.mu.Lock()"},
		{"registration write changed", pluginRegistryFile, "r.clients[name] = client", "r.clients[\"kind\"] = client"},
		{"reserved route changed", registryFile, "reservedKubernetesBackendTypes[clusterType]", "reservedKubernetesBackendTypes[\"kind\"]"},
		{"core lookup key changed", registryFile, "kubernetesBackendRegistry[clusterType]", "kubernetesBackendRegistry[\"kind\"]"},
		{"provider constructor changed", registryFile, "newGRPCKubernetesBackend(client)", "newGRPCKubernetesBackend(otherClient)"},
		{"factory changed", coreFile, "return &kindBackend{}, nil", "return &aksBackend{}, nil"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := legacyCurrentFixture(t)
			path := filepath.Join(root, test.path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), test.before) {
				t.Fatal("mutation did not match its input")
			}
			writeTestFile(t, root, test.path, strings.Replace(string(data), test.before, test.after, 1))
			if result := inspectRoot(root, true); len(result.violations) == 0 {
				t.Fatal("current-main authority mutation accepted")
			}
		})
	}
	for _, test := range []struct{ name, source string }{
		{"reserved alias", "var escaped = reservedKubernetesBackendTypes"},
		{"reserved write", "func init() { reservedKubernetesBackendTypes[\"digitalocean\"] = struct{}{} }"},
		{"registration alias", "var escaped = RegisterKubernetesBackend"},
		{"registry alias", "var escaped = kubernetesBackendRegistry"},
		{"mixed scoped authority", "func isReservedKubernetesBackendType(name string) bool { return true }"},
		{"isolated scoped normalizer declaration", "func normalizeKubernetesBackendRegistration() {}"},
		{"isolated scoped normalizer alias", "var escaped = normalizeKubernetesBackendRegistration"},
		{"reserved linkname", "import _ \"unsafe\"\n//go:linkname escaped github.com/GoCodeAlone/workflow/module.reservedKubernetesBackendTypes\nvar escaped map[string]struct{}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := legacyCurrentFixture(t)
			writeTestFile(t, root, "module/extra_authority.go", "package module\n"+test.source+"\n")
			if result := inspectRoot(root, true); len(result.violations) == 0 {
				t.Fatal("escaped current-main authority accepted")
			}
		})
	}
}

const validRegistrySource = `package module

type kubernetesBackend interface{}

type KubernetesBackendFactory func(map[string]any) (kubernetesBackend, error)

type CloudCredentialProvider interface{}

type KubernetesClusterState struct {
	Name string
	Provider string
	Version string
	Status string
}

var kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}

func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) {
	kubernetesBackendRegistry[clusterType] = factory
}

type PlatformKubernetes struct {
	name string
	config map[string]any
	provider CloudCredentialProvider
	state *KubernetesClusterState
	backend kubernetesBackend
}

type KubernetesBackendBinding struct {
	Name string
	ResourceType string
	Client any
}

func (m *PlatformKubernetes) Init(app any) error {
	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("account service not found")
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("account service has wrong type")
		}
		m.provider = provider
	}

	clusterType, _ := m.config["type"].(string)
	if clusterType == "" {
		clusterType = "kind"
	}

	if isReservedKubernetesBackendType(clusterType) {
		factory, ok := kubernetesBackendRegistry[clusterType]
		if !ok {
			return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)
		}
		backend, err := factory(m.config)
		if err != nil {
			return err
		}
		m.backend = backend
	} else {
		binding, scoped, err := resolveApplicationKubernetesBackend(app, clusterType)
		if err != nil {
			return err
		}
		if !scoped {
			binding, _ = kubernetesBackendClientRegistryInstance.resolve(clusterType)
		}
		if binding.Client != nil {
			m.backend = newGRPCKubernetesBackend(binding.Name, binding.ResourceType, binding.Client)
		} else if factory, ok := kubernetesBackendRegistry[clusterType]; ok {
			backend, createErr := factory(m.config)
			if createErr != nil {
				return createErr
			}
			m.backend = backend
		} else {
			return fmt.Errorf("platform.kubernetes %q: cluster type %q is not built into workflow core "+
				"(in-core types: 'kind', 'k3s'; compatibility fallbacks: 'eks', 'aks'). If %q is a "+
				"plugin-provided backend, install and load the plugin that declares it",
				m.name, clusterType, clusterType)
		}
	}

	version, _ := m.config["version"].(string)
	m.state = &KubernetesClusterState{
		Name: m.name,
		Provider: clusterType,
		Version: version,
		Status: "pending",
	}
	return app.RegisterService(m.name, m)
}
`

const validReservedPredicateSource = `func isReservedKubernetesBackendType(name string) bool {
	switch name {
	case "kind", "k3s":
		return true
	default:
		return false
	}
}
`

const validReservedNormalizationGuardSource = `		if isReservedKubernetesBackendType(name) {
			return "", nil, fmt.Errorf("plugin registered reserved kubernetes backend type %q", name)
		}
`

const validPluginRegistrySource = `package module

` + validReservedPredicateSource + `
func normalizeKubernetesBackendRegistration(owner string, bindings []KubernetesBackendBinding) (string, map[string]KubernetesBackendBinding, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", nil, fmt.Errorf("kubernetes backend registration: owner must not be empty")
	}
	normalized := make(map[string]KubernetesBackendBinding, len(bindings))
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			return "", nil, fmt.Errorf("kubernetes backend registration: name must not be empty")
		}
` + validReservedNormalizationGuardSource + `		if _, duplicate := normalized[name]; duplicate {
			return "", nil, fmt.Errorf("kubernetes backend registration has duplicate normalized name %q", name)
		}
		resourceType := strings.TrimSpace(binding.ResourceType)
		if resourceType == "" {
			return "", nil, fmt.Errorf("kubernetes backend registration %q: resource type must not be empty", name)
		}
		if binding.Client == nil {
			return "", nil, fmt.Errorf("kubernetes backend registration %q: client must not be nil", name)
		}
		normalized[name] = KubernetesBackendBinding{
			Name:         name,
			ResourceType: resourceType,
			Client:       binding.Client,
		}
	}
	return owner, normalized, nil
}
`

const validCoreSource = `package module

type kindBackend struct{}
type eksErrorBackend struct{}
type aksBackend struct{}

func init() {
	RegisterKubernetesBackend("kind", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("k3s", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
		return &eksErrorBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
`

const kindFactorySource = `func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	}`

const eksFactorySource = `func(_ map[string]any) (kubernetesBackend, error) {
		return &eksErrorBackend{}, nil
	}`

func TestInspectRootRejectsHiddenRegisterReferences(t *testing.T) {
	tests := []struct {
		name     string
		hidden   string
		expected string
	}{
		{
			name:     "comment-separated call",
			hidden:   `func hidden() { RegisterKubernetesBackend /* hidden */ ("gke", nil) }`,
			expected: "must not contain interstitial comments or wrappers",
		},
		{
			name:     "parenthesized call",
			hidden:   `func hidden() { (RegisterKubernetesBackend)("gke", nil) }`,
			expected: "unsupported RegisterKubernetesBackend reference",
		},
		{
			name:     "alias assignment and call",
			hidden:   `func hidden() { register := RegisterKubernetesBackend; register("gke", nil) }`,
			expected: "unsupported RegisterKubernetesBackend reference",
		},
		{
			name:     "selector call",
			hidden:   `func hidden() { module.RegisterKubernetesBackend("gke", nil) }`,
			expected: "unsupported RegisterKubernetesBackend reference",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, "module/hidden.go", "package module\n\n"+test.hidden+"\n")
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRejectsMovedDeclarationsAndRegistryWrites(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testing.T, string)
		expected string
	}{
		{
			name: "moved function declaration",
			mutate: func(t *testing.T, root string) {
				canonical := strings.ReplaceAll(validRegistrySource, "func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) {\n\tkubernetesBackendRegistry[clusterType] = factory\n}\n", "")
				writeTestFile(t, root, registryFile, canonical)
				writeTestFile(t, root, "module/provider_backend.go", "package module\n\nfunc RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) {\n\tkubernetesBackendRegistry[clusterType] = factory\n}\n")
			},
			expected: "RegisterKubernetesBackend declaration must be in module/platform_kubernetes.go",
		},
		{
			name: "moved registry declaration",
			mutate: func(t *testing.T, root string) {
				canonical := strings.ReplaceAll(validRegistrySource, "var kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}\n\n", "")
				writeTestFile(t, root, registryFile, canonical)
				writeTestFile(t, root, "module/provider_backend.go", "package module\n\nvar kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}\n")
			},
			expected: "kubernetesBackendRegistry declaration must be in module/platform_kubernetes.go",
		},
		{
			name: "noncanonical registry write",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "module/provider_backend.go", "package module\n\nfunc hidden() {\n\tkubernetesBackendRegistry[\"gke\"] = nil\n}\n")
			},
			expected: "kubernetesBackendRegistry write must remain in RegisterKubernetesBackend",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			test.mutate(t, root)
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRejectsRegistryInitializerMutations(t *testing.T) {
	tests := []struct {
		name        string
		initializer string
	}{
		{name: "provider entry", initializer: `map[string]KubernetesBackendFactory{"gke": nil}`},
		{name: "positional entry", initializer: `map[string]KubernetesBackendFactory{nil}`},
		{name: "dynamic make", initializer: `make(map[string]KubernetesBackendFactory)`},
		{name: "alternate value type", initializer: `map[string]any{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			mutated := strings.Replace(validRegistrySource, "map[string]KubernetesBackendFactory{}", test.initializer, 1)
			writeTestFile(t, root, registryFile, mutated)
			assertViolation(t, inspectRoot(root, true), "kubernetesBackendRegistry must initialize an empty map literal")
		})
	}
}

func TestInspectRootRejectsNoncanonicalRegisterAssignment(t *testing.T) {
	tests := []struct {
		name       string
		assignment string
	}{
		{name: "hard-coded provider key", assignment: `kubernetesBackendRegistry["gke"] = factory`},
		{name: "alternate key identifier", assignment: `kubernetesBackendRegistry[backendName] = factory`},
		{name: "substituted right-hand side", assignment: `kubernetesBackendRegistry[clusterType] = providerFactory`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			mutated := strings.Replace(validRegistrySource, "kubernetesBackendRegistry[clusterType] = factory", test.assignment, 1)
			writeTestFile(t, root, registryFile, mutated)
			assertViolation(t, inspectRoot(root, true), "RegisterKubernetesBackend must directly assign kubernetesBackendRegistry[clusterType] = factory")
		})
	}
}

func TestInspectRootRejectsCanonicalRegistryReferenceEscapes(t *testing.T) {
	tests := []struct {
		name   string
		escape string
	}{
		{name: "alias read", escape: `func hidden() { alias := kubernetesBackendRegistry; _ = alias }`},
		{name: "direct read", escape: `func hidden() { _ = kubernetesBackendRegistry }`},
		{name: "tuple alias", escape: `func hidden() { alias, n := kubernetesBackendRegistry, 0; _, _ = alias, n }`},
		{name: "increment write", escape: `func hidden() { kubernetesBackendRegistry["gke"]++ }`},
		{name: "initializer reference", escape: `var hiddenRegistry = kubernetesBackendRegistry`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, registryFile, validRegistrySource+"\n"+test.escape+"\n")
			assertViolation(t, inspectRoot(root, true), "kubernetesBackendRegistry reference is only permitted in its declaration and RegisterKubernetesBackend write")
		})
	}
}

func TestInspectRootRequiresExactlyTwoCanonicalRegistryLookups(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(string) string
		expected string
	}{
		{
			name: "missing lookup",
			mutate: func(source string) string {
				return strings.Replace(source, "\t\tfactory, ok := kubernetesBackendRegistry[clusterType]\n", "\t\tfactory, ok := map[string]KubernetesBackendFactory{}[clusterType]\n", 1)
			},
			expected: "expected exactly two kubernetesBackendRegistry lookups in (*PlatformKubernetes).Init, found 1",
		},
		{
			name: "extra lookup",
			mutate: func(source string) string {
				return strings.Replace(source, "\tif isReservedKubernetesBackendType", "\t_, _ = kubernetesBackendRegistry[clusterType]\n\tif isReservedKubernetesBackendType", 1)
			},
			expected: "expected exactly two kubernetesBackendRegistry lookups in (*PlatformKubernetes).Init, found 3",
		},
		{
			name: "alternate key",
			mutate: func(source string) string {
				return strings.Replace(source, "\t\tfactory, ok := kubernetesBackendRegistry[clusterType]\n", "\t\tfactory, ok := kubernetesBackendRegistry[\"gke\"]\n", 1)
			},
			expected: "kubernetesBackendRegistry lookup in (*PlatformKubernetes).Init must index by clusterType",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, registryFile, test.mutate(validRegistrySource))
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRejectsNoncanonicalBackendFactories(t *testing.T) {
	tests := []struct {
		name        string
		replacement string
	}{
		{name: "nil factory", replacement: "nil"},
		{name: "provider factory identifier", replacement: "providerFactory"},
		{name: "parenthesized factory", replacement: "(" + kindFactorySource + ")"},
		{name: "wrong parameter shape", replacement: `func(cfg map[string]string) (kubernetesBackend, error) { return &kindBackend{}, nil }`},
		{name: "wrong result shape", replacement: `func(_ map[string]any) kubernetesBackend { return &kindBackend{} }`},
		{name: "extra statement", replacement: `func(_ map[string]any) (kubernetesBackend, error) { _ = 1; return &kindBackend{}, nil }`},
		{name: "extra return", replacement: `func(_ map[string]any) (kubernetesBackend, error) { return &kindBackend{}, nil; return &kindBackend{}, nil }`},
		{name: "mismatched backend", replacement: eksFactorySource},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			mutated := strings.Replace(validCoreSource, kindFactorySource, test.replacement, 1)
			writeTestFile(t, root, coreFile, mutated)
			assertViolation(t, inspectRoot(root, true), `backend "kind" factory must be the canonical kindBackend function literal`)
		})
	}
}

func TestInspectRootRejectsAllowedNameFactorySwaps(t *testing.T) {
	root := writeFixture(t, false)
	mutated := strings.Replace(validCoreSource, kindFactorySource, "FACTORY_SWAP", 1)
	mutated = strings.Replace(mutated, eksFactorySource, kindFactorySource, 1)
	mutated = strings.Replace(mutated, "FACTORY_SWAP", eksFactorySource, 1)
	writeTestFile(t, root, coreFile, mutated)
	result := inspectRoot(root, true)
	assertViolation(t, result, `backend "kind" factory must be the canonical kindBackend function literal`)
	assertViolation(t, result, `backend "eks" factory must be the canonical eksErrorBackend function literal`)
}

func TestInspectRootRequiresRegistrationsInCanonicalInit(t *testing.T) {
	root := writeFixture(t, false)
	mutated := strings.Replace(validCoreSource, "func init()", "func registerCoreBackends()", 1)
	writeTestFile(t, root, coreFile, mutated)
	assertViolation(t, inspectRoot(root, true), "calls must be direct statements of one top-level func init()")
}

func TestInspectRootRejectsConditionallySkippedRegistrations(t *testing.T) {
	root := writeFixture(t, false)
	mutated := strings.Replace(validCoreSource, "func init() {", "func init() {\n\tif disableCoreRegistration { return }", 1)
	writeTestFile(t, root, coreFile, mutated)
	assertViolation(t, inspectRoot(root, true), "calls must be direct statements of one top-level func init()")
}

func TestInspectRootRejectsNonsemanticRegistryLookups(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(string) string
		expected string
	}{
		{
			name: "dummy reads",
			mutate: func(source string) string {
				start := strings.Index(source, "func (m *PlatformKubernetes) Init")
				return source[:start] + `func (m *PlatformKubernetes) Init(clusterType string) error {
	_, _ = kubernetesBackendRegistry[clusterType]
	_, _ = kubernetesBackendRegistry[clusterType]
	return nil
}
`
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "core routing replacement",
			mutate: func(source string) string {
				return strings.Replace(source, "isReservedKubernetesBackendType(clusterType)", "isProviderKubernetesBackendType(clusterType)", 1)
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "earlier provider route preserves canonical branch",
			mutate: func(source string) string {
				return strings.Replace(source, "\tif isReservedKubernetesBackendType", "\tif providerRoute { return nil }\n\tif isReservedKubernetesBackendType", 1)
			},
			expected: "must remain the anchored routing decision",
		},
		{
			name: "provider route before cluster extraction",
			mutate: func(source string) string {
				return strings.Replace(source, "\tclusterType, _ :=", "\tif providerRoute { return initializeProvider(m) }\n\n\tclusterType, _ :=", 1)
			},
			expected: "must remain the anchored routing decision",
		},
		{
			name: "provider-first routing replacement",
			mutate: func(source string) string {
				return strings.Replace(source, "binding.Client != nil", "binding.Client == nil", 1)
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
		{
			name: "core lookup rejection swallowed",
			mutate: func(source string) string {
				return strings.Replace(source, `return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)`, "return nil", 1)
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "typed nil core lookup rejection",
			mutate: func(source string) string {
				return strings.Replace(source, `return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)`, "return unsupportedError", 1)
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "core factory error swallowed",
			mutate: func(source string) string {
				return strings.Replace(source, "\t\t\treturn err\n\t\t}\n\t\tm.backend = backend", "\t\t\treturn nil\n\t\t}\n\t\tm.backend = backend", 1)
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "core factory error hidden in nil-returning wrapper",
			mutate: func(source string) string {
				return strings.Replace(source, "\t\t\treturn err\n\t\t}\n\t\tm.backend = backend", "\t\t\treturn func(error) error { return nil }(err)\n\t\t}\n\t\tm.backend = backend", 1)
			},
			expected: "must preserve the core-local Kubernetes backend lookup and initialization branch",
		},
		{
			name: "fallback factory error swallowed",
			mutate: func(source string) string {
				return strings.Replace(source, "return createErr", "return nil", 1)
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
		{
			name: "provider branch shadows binding",
			mutate: func(source string) string {
				return strings.Replace(source, "if binding.Client != nil", "if binding := providerBinding; binding.Client != nil", 1)
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
		{
			name: "provider-specific binding source",
			mutate: func(source string) string {
				return strings.Replace(source, "resolveApplicationKubernetesBackend(app, clusterType)", "resolveProviderKubernetesBackend(app, clusterType)", 1)
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
		{
			name: "moved compatibility lookup",
			mutate: func(source string) string {
				return strings.Replace(source, "} else if factory, ok := kubernetesBackendRegistry[clusterType]; ok {", "}\n\t\tif factory, ok := kubernetesBackendRegistry[clusterType]; ok {", 1)
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
		{
			name: "provider backend in final fallback",
			mutate: func(source string) string {
				start := strings.Index(source, "\t\t} else {\n\t\t\treturn fmt.Errorf(\"platform.kubernetes %q: cluster type")
				end := strings.Index(source[start:], "\n\t\t}\n\t}\n") + start
				return source[:start] + "\t\t} else {\n\t\t\tm.backend = providerBackend\n\t\t\treturn nil" + source[end:]
			},
			expected: "must preserve the provider-first compatibility fallback lookup and initialization branch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, registryFile, test.mutate(validRegistrySource))
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRejectsReservedPredicateMutations(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testing.T, string)
		expected string
	}{
		{
			name: "removed map resurrection with provider member",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "module/hidden_reserved.go", `package module

var reservedKubernetesBackendTypes = map[string]struct{}{"digitalocean": {}}
`)
			},
			expected: "removed reservedKubernetesBackendTypes map must not exist",
		},
		{
			name: "provider predicate case",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource, `case "kind", "k3s":`, `case "kind", "k3s", "digitalocean":`, 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "isReservedKubernetesBackendType must be the canonical exact predicate",
		},
		{
			name: "default true",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource, "\t\treturn false\n", "\t\treturn true\n", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "isReservedKubernetesBackendType must be the canonical exact predicate",
		},
		{
			name: "body drift",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource, validReservedPredicateSource, `func isReservedKubernetesBackendType(name string) bool {
	return name == "kind"
}
`, 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "isReservedKubernetesBackendType must be the canonical exact predicate",
		},
		{
			name: "generic signature drift",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource,
					"func isReservedKubernetesBackendType(name string) bool",
					"func isReservedKubernetesBackendType[T any](name string) bool", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "isReservedKubernetesBackendType must be the canonical exact predicate",
		},
		{
			name: "predicate alias",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "module/hidden_reserved.go", "package module\n\nvar hiddenReserved = isReservedKubernetesBackendType\n")
			},
			expected: "isReservedKubernetesBackendType reference is only permitted in its declaration, core route, and normalization guard",
		},
		{
			name: "moved declaration",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, pluginRegistryFile, strings.Replace(validPluginRegistrySource, validReservedPredicateSource, "", 1))
				writeTestFile(t, root, "module/hidden_reserved.go", "package module\n\n"+validReservedPredicateSource)
			},
			expected: "isReservedKubernetesBackendType declaration must be in module/platform_kubernetes_plugin_registry.go",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			test.mutate(t, root)
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRequiresCanonicalReservedRouteAndNormalizationGuard(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testing.T, string)
		expected string
	}{
		{
			name: "replaced core route",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validRegistrySource, "isReservedKubernetesBackendType(clusterType)", "isProviderKubernetesBackendType(clusterType)", 1)
				writeTestFile(t, root, registryFile, mutated)
			},
			expected: "expected exactly one canonical isReservedKubernetesBackendType core route, found 0",
		},
		{
			name: "aliased core route",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validRegistrySource,
					"\tif isReservedKubernetesBackendType(clusterType) {",
					"\treserved := isReservedKubernetesBackendType\n\tif reserved(clusterType) {", 1)
				writeTestFile(t, root, registryFile, mutated)
			},
			expected: "expected exactly one canonical isReservedKubernetesBackendType core route, found 0",
		},
		{
			name: "missing normalization guard",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource, validReservedNormalizationGuardSource, "", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
		{
			name: "normalized name overwritten before guard",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource,
					"\t\tif name == \"\" {\n\t\t\treturn \"\", nil, fmt.Errorf(\"kubernetes backend registration: name must not be empty\")\n\t\t}\n",
					"\t\tname = \"digitalocean\"\n", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
		{
			name: "normalized name overwritten after reserved guard",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource,
					validReservedNormalizationGuardSource,
					validReservedNormalizationGuardSource+
						"\t\tif name == \"digitalocean\" {\n"+
						"\t\t\tname = \"kind\"\n"+
						"\t\t}\n", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
		{
			name: "reserved registration returned before guarded loop",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource,
					"\tnormalized := make(map[string]KubernetesBackendBinding, len(bindings))\n",
					"\tnormalized := make(map[string]KubernetesBackendBinding, len(bindings))\n"+
						"\tif len(bindings) == 1 {\n"+
						"\t\tnormalized[\"kind\"] = bindings[0]\n"+
						"\t\treturn owner, normalized, nil\n"+
						"\t}\n", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
		{
			name: "replaced normalization guard",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource, "isReservedKubernetesBackendType(name)", "isProviderKubernetesBackendType(name)", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
		{
			name: "aliased normalization guard",
			mutate: func(t *testing.T, root string) {
				mutated := strings.Replace(validPluginRegistrySource,
					"\t\tif isReservedKubernetesBackendType(name) {",
					"\t\treserved := isReservedKubernetesBackendType\n\t\tif reserved(name) {", 1)
				writeTestFile(t, root, pluginRegistryFile, mutated)
			},
			expected: "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			test.mutate(t, root)
			assertViolation(t, inspectRoot(root, true), test.expected)
		})
	}
}

func TestInspectRootRejectsSecurityNormalizerBodyDrift(t *testing.T) {
	const duplicateGuard = `		if _, duplicate := normalized[name]; duplicate {
			return "", nil, fmt.Errorf("kubernetes backend registration has duplicate normalized name %q", name)
		}
`
	const resourceTypeAssignment = "\t\tresourceType := strings.TrimSpace(binding.ResourceType)\n"
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{
			name: "missing statement",
			mutate: func(source string) string {
				return strings.Replace(source, resourceTypeAssignment, "", 1)
			},
		},
		{
			name: "reordered statements",
			mutate: func(source string) string {
				source = strings.Replace(source, duplicateGuard, "NORMALIZER_DUPLICATE_GUARD\n", 1)
				source = strings.Replace(source, resourceTypeAssignment, duplicateGuard, 1)
				return strings.Replace(source, "NORMALIZER_DUPLICATE_GUARD\n", resourceTypeAssignment, 1)
			},
		},
		{
			name: "resource type reassignment",
			mutate: func(source string) string {
				return strings.Replace(source, resourceTypeAssignment,
					resourceTypeAssignment+"\t\tresourceType = strings.TrimSpace(binding.Name)\n", 1)
			},
		},
		{
			name: "alternate normalized key",
			mutate: func(source string) string {
				return strings.Replace(source,
					"\t\tnormalized[name] = KubernetesBackendBinding{",
					"\t\tnormalized[resourceType] = KubernetesBackendBinding{", 1)
			},
		},
		{
			name: "alternate normalized value",
			mutate: func(source string) string {
				return strings.Replace(source, "\t\t\tName:         name", "\t\t\tName:         \"kind\"", 1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, pluginRegistryFile, test.mutate(validPluginRegistrySource))
			assertViolation(t, inspectRoot(root, true), "normalizeKubernetesBackendRegistration must directly guard normalized name with isReservedKubernetesBackendType")
		})
	}
}

func TestInspectRootIgnoresCommentsAndStrings(t *testing.T) {
	root := writeFixture(t, false)
	noise := "package module\n\n" +
		"// RegisterKubernetesBackend(\"gke\", nil)\n" +
		"/* RegisterKubernetesBackend(\"managed-cloud\", nil) */\n" +
		"const quotedRegistration = \"RegisterKubernetesBackend(\\\"gke\\\", nil)\"\n" +
		"const rawRegistration = `RegisterKubernetesBackend(\"managed-cloud\", nil)`\n"
	writeTestFile(t, root, "module/lexical_noise.go", noise)
	assertClean(t, inspectRoot(root, true))
}

func TestInspectRootRejectsBoundaryLinknameAliases(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "registration function",
			source: `package module

import _ "unsafe"

//go:linkname hiddenRegister github.com/GoCodeAlone/workflow/module.RegisterKubernetesBackend
func hiddenRegister(string, KubernetesBackendFactory)

func init() {
	hiddenRegister("digitalocean", nil)
}
`,
		},
		{
			name: "registry variable with directive whitespace",
			source: `package module

import _ "unsafe"

//go:linkname	hiddenRegistry	github.com/GoCodeAlone/workflow/module.kubernetesBackendRegistry
var hiddenRegistry map[string]KubernetesBackendFactory

func init() {
	hiddenRegistry["digitalocean"] = nil
}
`,
		},
		{
			name: "removed reserved map",
			source: `package module

import _ "unsafe"

//go:linkname hiddenReserved github.com/GoCodeAlone/workflow/module.reservedKubernetesBackendTypes
var hiddenReserved map[string]struct{}
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, false)
			writeTestFile(t, root, "module/linkname_alias.go", test.source)
			assertViolation(t, inspectRoot(root, true), "go:linkname must not reference Kubernetes backend boundary symbol")
		})
	}
}

func TestInspectRootProductionIdentityAndMarkers(t *testing.T) {
	t.Run("wrong module", func(t *testing.T) {
		root := writeFixture(t, true)
		writeTestFile(t, root, "go.mod", "module example.com/workflow-lookalike\n\ngo 1.26.5\n")
		assertViolation(t, inspectRoot(root, false), "module identity must be github.com/GoCodeAlone/workflow")
	})

	for _, marker := range []string{".phase-b-complete", ".phase-c-complete"} {
		t.Run("missing "+marker, func(t *testing.T) {
			root := writeFixture(t, true)
			if err := os.Remove(filepath.Join(root, marker)); err != nil {
				t.Fatal(err)
			}
			assertViolation(t, inspectRoot(root, false), "missing committed phase marker "+marker)
		})
	}
}

func TestInspectRootRejectsSymlinkedAuditInputs(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		root := writeFixture(t, false)
		link := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-link")
		createSymlink(t, root, link)
		assertViolation(t, inspectRoot(link, true), "Workflow root must not be a symlink")
	})

	tests := []struct {
		name        string
		path        string
		fixtureMode bool
	}{
		{name: "go.mod", path: "go.mod", fixtureMode: false},
		{name: "phase marker", path: ".phase-b-complete", fixtureMode: false},
		{name: "canonical registry", path: registryFile, fixtureMode: true},
		{name: "canonical core", path: coreFile, fixtureMode: true},
		{name: "canonical plugin registry", path: pluginRegistryFile, fixtureMode: true},
		{name: "scanned production Go file", path: "module/provider_backend.go", fixtureMode: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, !test.fixtureMode)
			path := filepath.Join(root, filepath.FromSlash(test.path))
			target := path + ".target"
			if test.path == "module/provider_backend.go" {
				writeTestFile(t, root, test.path+".target", "package module\n")
			} else {
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
			}
			createSymlink(t, filepath.Base(target), path)
			assertViolation(t, inspectRoot(root, test.fixtureMode), "symlink is not permitted")
		})
	}
}

func TestInspectRootFixtureModeRelaxesOnlyRepositoryIdentity(t *testing.T) {
	root := writeFixture(t, false)
	assertClean(t, inspectRoot(root, true))

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(coreFile))); err != nil {
		t.Fatal(err)
	}
	assertViolation(t, inspectRoot(root, true), "missing canonical Kubernetes registration file "+coreFile)
}

func writeFixture(t *testing.T, withIdentity bool) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, registryFile, validRegistrySource)
	writeTestFile(t, root, coreFile, validCoreSource)
	writeTestFile(t, root, pluginRegistryFile, validPluginRegistrySource)
	if withIdentity {
		writeTestFile(t, root, "go.mod", "module "+workflowModulePath+"\n\ngo 1.26.5\n")
		writeTestFile(t, root, ".phase-b-complete", "")
		writeTestFile(t, root, ".phase-c-complete", "")
	}
	return root
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func createSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation is not permitted by this OS: %v", err)
		}
		t.Fatalf("create symlink %s -> %s: %v", link, target, err)
	}
}

func assertViolation(t *testing.T, result auditResult, expected string) {
	t.Helper()
	for _, violation := range result.violations {
		if strings.Contains(violation, expected) {
			return
		}
	}
	t.Fatalf("expected violation containing %q, got %q", expected, result.violations)
}

func assertClean(t *testing.T, result auditResult) {
	t.Helper()
	if len(result.violations) != 0 {
		t.Fatalf("expected clean audit, got %q", result.violations)
	}
}

func TestInspectRootRejectsRegisterFunctionContractDrift(t *testing.T) {
	canonical := "func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) {\n\tkubernetesBackendRegistry[clusterType] = factory\n}"
	for _, profile := range []string{"current legacy", "scoped provider first"} {
		t.Run(profile, func(t *testing.T) {
			for _, mutation := range []struct{ name, function string }{
				{"key rebound", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tclusterType = \"gke\"\n\tkubernetesBackendRegistry", 1)},
				{"factory rebound", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tfactory = nil\n\tkubernetesBackendRegistry", 1)},
				{"early return", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\treturn\n\tkubernetesBackendRegistry", 1)},
				{"conditional return", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tif clusterType == \"kind\" { return }\n\tkubernetesBackendRegistry", 1)},
				{"side effect", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tproviderSideEffect()\n\tkubernetesBackendRegistry", 1)},
				{"deferred side effect", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tdefer providerSideEffect()\n\tkubernetesBackendRegistry", 1)},
				{"panic", strings.Replace(canonical, "\tkubernetesBackendRegistry", "\tpanic(\"stop\")\n\tkubernetesBackendRegistry", 1)},
				{"named result", strings.Replace(canonical, ") {", ") (err error) {", 1)},
				{"variadic factory", strings.Replace(canonical, "factory KubernetesBackendFactory", "factory ...KubernetesBackendFactory", 1)},
				{"wrong key type", strings.Replace(canonical, "clusterType string", "clusterType any", 1)},
				{"extra parameter", strings.Replace(canonical, "factory KubernetesBackendFactory", "factory KubernetesBackendFactory, ignored any", 1)},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					var root string
					if profile == "current legacy" {
						root = legacyCurrentFixture(t)
					} else {
						root = writeFixture(t, false)
					}
					path := filepath.Join(root, registryFile)
					source, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					changed := strings.Replace(string(source), canonical, mutation.function, 1)
					if changed == string(source) {
						t.Fatal("canonical registration function missing from fixture")
					}
					writeTestFile(t, root, registryFile, changed)
					assertViolation(t, inspectRoot(root, true), "RegisterKubernetesBackend must preserve its exact signature and sole direct registry assignment")
				})
			}
		})
	}
}

func TestInspectRootRejectsSymlinkedProductionDirectories(t *testing.T) {
	for _, name := range []string{"provider", "provider.go", "provider_test.go"} {
		t.Run(name, func(t *testing.T) {
			root := writeFixture(t, false)
			target := t.TempDir()
			writeTestFile(t, target, "backend.go", "package provider\nfunc init() { RegisterKubernetesBackend(\"gke\", nil) }\n")
			createSymlink(t, target, filepath.Join(root, name))
			assertViolation(t, inspectRoot(root, true), "symlink is not permitted")
		})
	}
}

func TestInspectRootRejectsConditionalCanonicalFiles(t *testing.T) {
	for _, profile := range []string{"current legacy", "scoped provider first"} {
		t.Run(profile, func(t *testing.T) {
			for _, file := range []string{registryFile, coreFile, pluginRegistryFile} {
				t.Run(file, func(t *testing.T) {
					for _, header := range []string{"//go:build audit_disabled && !audit_disabled\n\n", "//go:build windows\n\n", "// +build audit_disabled,!audit_disabled\n\n"} {
						t.Run(strings.TrimSpace(header), func(t *testing.T) {
							var root string
							if profile == "current legacy" {
								root = legacyCurrentFixture(t)
							} else {
								root = writeFixture(t, false)
							}
							path := filepath.Join(root, file)
							source, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							writeTestFile(t, root, file, header+string(source))
							context := build.Context{GOOS: "linux", GOARCH: "amd64", Compiler: "gc"}
							included, err := context.MatchFile(filepath.Dir(path), filepath.Base(path))
							if err != nil {
								t.Fatal(err)
							}
							if included {
								t.Fatal("negative fixture must be excluded by Go's build-constraint selection")
							}
							assertViolation(t, inspectRoot(root, true), "canonical Kubernetes boundary file must be unconditional")
						})
					}
				})
			}
		})
	}
}

func TestInspectRootAllowsCanonicalFileDocumentation(t *testing.T) {
	for _, profile := range []string{"current legacy", "scoped provider first"} {
		t.Run(profile, func(t *testing.T) {
			var root string
			if profile == "current legacy" {
				root = legacyCurrentFixture(t)
			} else {
				root = writeFixture(t, false)
			}
			for _, file := range []string{registryFile, coreFile, pluginRegistryFile} {
				path := filepath.Join(root, file)
				source, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, root, file, "// Boundary documentation mentions //go:build and // +build as text.\n\n"+string(source))
			}
			assertClean(t, inspectRoot(root, true))
		})
	}
}

func TestInspectRootRejectsCgoConditionalCanonicalFiles(t *testing.T) {
	for _, profile := range []string{"current legacy", "scoped provider first"} {
		t.Run(profile, func(t *testing.T) {
			for _, file := range []string{registryFile, coreFile, pluginRegistryFile} {
				t.Run(file, func(t *testing.T) {
					for _, imported := range []string{`"C"`, `"\x43"`, "`C`"} {
						t.Run(imported, func(t *testing.T) {
							var root string
							if profile == "current legacy" {
								root = legacyCurrentFixture(t)
							} else {
								root = writeFixture(t, false)
							}
							path := filepath.Join(root, file)
							source, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							changed := strings.Replace(string(source), "package module\n", "package module\n\nimport "+imported+"\n", 1)
							if changed == string(source) {
								t.Fatal("canonical package declaration missing")
							}
							writeTestFile(t, root, file, changed)
							context := build.Default
							context.GOOS, context.GOARCH, context.CgoEnabled = "linux", "amd64", false
							selected, err := context.ImportDir(filepath.Dir(path), 0)
							if err != nil {
								t.Fatal(err)
							}
							excluded := false
							for _, ignored := range selected.IgnoredGoFiles {
								if ignored == filepath.Base(path) {
									excluded = true
								}
							}
							if !excluded {
								t.Fatal("negative fixture must be excluded by Go when CGO_ENABLED=0")
							}
							assertViolation(t, inspectRoot(root, true), "canonical Kubernetes boundary file must be unconditional")
						})
					}
				})
			}
		})
	}
}

func TestRunAcceptsGeneratedUIDependencyBins(t *testing.T) {
	for _, profile := range []string{"current legacy", "scoped provider first"} {
		t.Run(profile, func(t *testing.T) {
			var root string
			if profile == "current legacy" {
				root = legacyCurrentFixture(t)
			} else {
				root = writeFixture(t, false)
			}
			writeTestFile(t, root, "go.mod", "module "+workflowModulePath+"\n\ngo 1.26.5\n")
			writeTestFile(t, root, ".phase-b-complete", "")
			writeTestFile(t, root, ".phase-c-complete", "")
			writeTestFile(t, root, "ui/node_modules/vite/bin/vite.js", "console.log('test');\n")
			bins := filepath.Join(root, "ui", "node_modules", ".bin")
			if err := os.MkdirAll(bins, 0o755); err != nil {
				t.Fatal(err)
			}
			createSymlink(t, "../vite/bin/vite.js", filepath.Join(bins, "vite"))
			var stdout, stderr strings.Builder
			if exit := run([]string{"--root", root}, &stdout, &stderr); exit != 0 {
				t.Fatalf("actual-root CLI must accept generated UI dependency bins: exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInspectRootRejectsLinksOutsideGeneratedUIDependencies(t *testing.T) {
	for _, path := range []string{"ui", "ui/node_modules", "module/node_modules", "node_modules", "module/provider"} {
		t.Run(path, func(t *testing.T) {
			root := writeFixture(t, false)
			target := t.TempDir()
			writeTestFile(t, target, "hidden_backend.go", "package provider\nfunc init() { RegisterKubernetesBackend(\"gke\", nil) }\n")
			link := filepath.Join(root, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			createSymlink(t, target, link)
			assertViolation(t, inspectRoot(root, true), "symlink is not permitted")
		})
	}
}

func TestInspectRootStillRejectsBoundaryDriftWithGeneratedUI(t *testing.T) {
	root := writeFixture(t, false)
	writeTestFile(t, root, "ui/node_modules/vite/bin/vite.js", "console.log('test');\n")
	bins := filepath.Join(root, "ui", "node_modules", ".bin")
	if err := os.MkdirAll(bins, 0o755); err != nil {
		t.Fatal(err)
	}
	createSymlink(t, "../vite/bin/vite.js", filepath.Join(bins, "vite"))
	source := strings.Replace(validRegistrySource, "\tkubernetesBackendRegistry[clusterType] = factory", "\treturn\n\tkubernetesBackendRegistry[clusterType] = factory", 1)
	writeTestFile(t, root, registryFile, source)
	assertViolation(t, inspectRoot(root, true), "RegisterKubernetesBackend must preserve its exact signature and sole direct registry assignment")
}
