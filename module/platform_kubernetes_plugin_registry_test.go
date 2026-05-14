package module

import (
	"strings"
	"testing"
)

// TestKubernetesBackendClientRegistry exercises the engine-side registry that
// maps a plugin-served platform.kubernetes cluster type to a ResourceDriver
// gRPC client. Mirrors TestIaCStateBackendRegistry.
func TestKubernetesBackendClientRegistry(t *testing.T) {
	reg := newKubernetesBackendClientRegistry()
	if _, ok := reg.resolve("gke"); ok {
		t.Fatal("empty registry should not resolve gke")
	}
	fake := &fakeResourceDriverClient{}
	if err := reg.register("gke", fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := reg.resolve("gke")
	if !ok || got != fake {
		t.Fatalf("resolve gke: ok=%v got=%v", ok, got)
	}
	// Reserved in-core cluster types cannot be claimed by a plugin.
	for _, reserved := range []string{"kind", "k3s", "eks", "aks"} {
		if err := reg.register(reserved, fake); err == nil {
			t.Fatalf("register(%q) must fail — reserved in-core cluster type", reserved)
		}
	}
	// Empty / whitespace-only names are rejected.
	for _, bad := range []string{"", "   "} {
		if err := reg.register(bad, fake); err == nil {
			t.Fatalf("register(%q) must fail — empty name", bad)
		}
	}
	// A nil client is rejected.
	if err := reg.register("nilclient_type", nil); err == nil {
		t.Fatal("register with nil client must fail")
	}
	// A name surrounded by whitespace is trimmed and registers under the
	// trimmed key.
	if err := reg.register("  spaced_type  ", fake); err != nil {
		t.Fatalf("register trimmed name: %v", err)
	}
	if _, ok := reg.resolve("spaced_type"); !ok {
		t.Fatal("trimmed name must resolve under its trimmed key")
	}
}

// TestRegisterKubernetesBackendClient exercises the exported wrapper the engine
// calls at plugin-load: a non-reserved name registers into the package-level
// singleton and resolves; a reserved in-core type name is rejected.
func TestRegisterKubernetesBackendClient(t *testing.T) {
	const backend = "gke_wrapper_test"
	fake := &fakeResourceDriverClient{}
	if err := RegisterKubernetesBackendClient(backend, fake); err != nil {
		t.Fatalf("RegisterKubernetesBackendClient(%q): %v", backend, err)
	}
	defer func() {
		kubernetesBackendClientRegistryInstance.mu.Lock()
		delete(kubernetesBackendClientRegistryInstance.clients, backend)
		kubernetesBackendClientRegistryInstance.mu.Unlock()
	}()
	if got, ok := kubernetesBackendClientRegistryInstance.resolve(backend); !ok || got != fake {
		t.Fatalf("resolve(%q): ok=%v got=%v", backend, ok, got)
	}
	if err := RegisterKubernetesBackendClient("kind", fake); err == nil {
		t.Fatal(`RegisterKubernetesBackendClient("kind") must fail — reserved in-core cluster type`)
	}
}

// TestPlatformKubernetes_GKEDispatchToPluginClient exercises the real
// PlatformKubernetes.Init resolution path: a `type: gke` config — a type no
// in-core backend owns — resolves from the package-level client registry,
// yielding a *grpcKubernetesBackend.
func TestPlatformKubernetes_GKEDispatchToPluginClient(t *testing.T) {
	const clusterType = "gke"
	fake := &fakeResourceDriverClient{}
	if err := kubernetesBackendClientRegistryInstance.register(clusterType, fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() {
		kubernetesBackendClientRegistryInstance.mu.Lock()
		delete(kubernetesBackendClientRegistryInstance.clients, clusterType)
		kubernetesBackendClientRegistryInstance.mu.Unlock()
	}()

	m := NewPlatformKubernetes("gke-cluster", map[string]any{"type": "gke"})
	if err := m.Init(NewMockApplication()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, ok := m.backend.(*grpcKubernetesBackend); !ok {
		t.Fatalf("m.backend is %T, want *grpcKubernetesBackend", m.backend)
	}
}

// TestPlatformKubernetes_GKEWithoutPluginErrors verifies that `type: gke` with
// no plugin client registered fails Init with a clean error pointing the
// operator at workflow-plugin-gcp.
func TestPlatformKubernetes_GKEWithoutPluginErrors(t *testing.T) {
	if _, ok := kubernetesBackendClientRegistryInstance.resolve("gke"); ok {
		t.Skip("a gke client is registered by a concurrent test; skipping the negative case")
	}
	m := NewPlatformKubernetes("gke-cluster", map[string]any{"type": "gke"})
	err := m.Init(NewMockApplication())
	if err == nil {
		t.Fatal("Init must fail when no gke plugin client is registered")
	}
	if !strings.Contains(err.Error(), "workflow-plugin-gcp") {
		t.Fatalf("error must point the operator to workflow-plugin-gcp, got: %v", err)
	}
}
