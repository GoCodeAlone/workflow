package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validRegistrySource = `package module

var kubernetesBackendRegistry = map[string]any{}

func RegisterKubernetesBackend(name string, factory any) {
	kubernetesBackendRegistry[name] = factory
}
`

const validCoreSource = `package module

func init() {
	RegisterKubernetesBackend("kind", nil)
	RegisterKubernetesBackend("k3s", nil)
	RegisterKubernetesBackend("eks", nil)
	RegisterKubernetesBackend("aks", nil)
}
`

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
				canonical := strings.ReplaceAll(validRegistrySource, "func RegisterKubernetesBackend(name string, factory any) {\n\tkubernetesBackendRegistry[name] = factory\n}\n", "")
				writeTestFile(t, root, registryFile, canonical)
				writeTestFile(t, root, "module/provider_backend.go", "package module\n\nfunc RegisterKubernetesBackend(name string, factory any) {\n\tkubernetesBackendRegistry[name] = factory\n}\n")
			},
			expected: "RegisterKubernetesBackend declaration must be in module/platform_kubernetes.go",
		},
		{
			name: "moved registry declaration",
			mutate: func(t *testing.T, root string) {
				canonical := strings.ReplaceAll(validRegistrySource, "var kubernetesBackendRegistry = map[string]any{}\n\n", "")
				writeTestFile(t, root, registryFile, canonical)
				writeTestFile(t, root, "module/provider_backend.go", "package module\n\nvar kubernetesBackendRegistry = map[string]any{}\n")
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
