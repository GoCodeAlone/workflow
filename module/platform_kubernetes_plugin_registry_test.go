package module

import (
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

const testKubernetesBackendRegistryServiceName = "workflow.internal.kubernetes-backend-registry"

type testScopedKubernetesBackendRegistry struct {
	clients map[string]pb.ResourceDriverClient
}

func (r *testScopedKubernetesBackendRegistry) ResolveKubernetesBackend(name string) (KubernetesBackendBinding, string, bool) {
	client, ok := r.clients[name]
	return KubernetesBackendBinding{Name: name, ResourceType: "infra." + name, Client: client}, "test-provider", ok
}

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
	if !ok || got.Client != fake || got.ResourceType != legacyKubernetesBackendResourceType {
		t.Fatalf("resolve gke: ok=%v got=%v", ok, got)
	}
	// Only core-local cluster types cannot be claimed by a plugin.
	for _, reserved := range []string{"kind", "k3s"} {
		if err := reg.register(reserved, fake); err == nil {
			t.Fatalf("register(%q) must fail — reserved in-core cluster type", reserved)
		}
	}
	// Retained provider compatibility backends are plugin-overridable.
	for _, providerOwned := range []string{"aks", "eks"} {
		if err := reg.register(providerOwned, fake); err != nil {
			t.Fatalf("register(%q) must allow provider ownership: %v", providerOwned, err)
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

func TestKubernetesBackendRegistrySameOwnerReplacementDropsStaleNames(t *testing.T) {
	reg := NewKubernetesBackendRegistry()
	first := &fakeResourceDriverClient{}
	replacement := &fakeResourceDriverClient{}
	if err := reg.Register("provider-a", []KubernetesBackendBinding{
		{Name: "dropped", ResourceType: "infra.dropped", Client: first},
		{Name: "retained", ResourceType: "infra.retained", Client: first},
	}); err != nil {
		t.Fatalf("Register(initial): %v", err)
	}
	if err := reg.Register("provider-a", []KubernetesBackendBinding{
		{Name: "retained", ResourceType: "infra.retained.v2", Client: replacement},
		{Name: "added", ResourceType: "infra.added", Client: replacement},
	}); err != nil {
		t.Fatalf("Register(replacement): %v", err)
	}

	if client, owner, ok := reg.ResolveKubernetesBackend("dropped"); ok {
		t.Fatalf("dropped name survived replacement: (%v, %q, %v)", client, owner, ok)
	}
	for _, name := range []string{"retained", "added"} {
		binding, owner, ok := reg.ResolveKubernetesBackend(name)
		if !ok || owner != "provider-a" || binding.Client != replacement {
			t.Fatalf("%s = (%v, %q, %v), want provider-a replacement", name, binding, owner, ok)
		}
	}
}

// TestRegisterKubernetesBackendClient exercises the legacy exported wrapper: a
// non-reserved name registers into the package-level compatibility singleton
// and resolves; a reserved in-core type name is rejected.
func TestRegisterKubernetesBackendClient(t *testing.T) {
	const backend = "gke_wrapper_test"
	fake := &fakeResourceDriverClient{}
	if err := RegisterKubernetesBackendClient(backend, fake); err != nil {
		t.Fatalf("RegisterKubernetesBackendClient(%q): %v", backend, err)
	}
	defer func() {
		kubernetesBackendClientRegistryInstance.mu.Lock()
		delete(kubernetesBackendClientRegistryInstance.bindings, backend)
		delete(kubernetesBackendClientRegistryInstance.owners, backend)
		kubernetesBackendClientRegistryInstance.mu.Unlock()
	}()
	if got, ok := kubernetesBackendClientRegistryInstance.resolve(backend); !ok || got.Client != fake {
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
		delete(kubernetesBackendClientRegistryInstance.bindings, clusterType)
		delete(kubernetesBackendClientRegistryInstance.owners, clusterType)
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
// no plugin client registered fails Init with provider-neutral guidance.
func TestPlatformKubernetes_GKEWithoutPluginErrors(t *testing.T) {
	const clusterType = "gke"
	// Clear any registration left by a sibling test (Go test order within a
	// package is not guaranteed across files), then restore on cleanup. Doing
	// this under the registry mutex keeps the test deterministic instead of
	// skipping when a concurrent registration is present.
	kubernetesBackendClientRegistryInstance.mu.Lock()
	prev, hadPrev := kubernetesBackendClientRegistryInstance.bindings[clusterType]
	prevOwner := kubernetesBackendClientRegistryInstance.owners[clusterType]
	delete(kubernetesBackendClientRegistryInstance.bindings, clusterType)
	delete(kubernetesBackendClientRegistryInstance.owners, clusterType)
	kubernetesBackendClientRegistryInstance.mu.Unlock()
	defer func() {
		kubernetesBackendClientRegistryInstance.mu.Lock()
		if hadPrev {
			kubernetesBackendClientRegistryInstance.bindings[clusterType] = prev
			kubernetesBackendClientRegistryInstance.owners[clusterType] = prevOwner
		} else {
			delete(kubernetesBackendClientRegistryInstance.bindings, clusterType)
			delete(kubernetesBackendClientRegistryInstance.owners, clusterType)
		}
		kubernetesBackendClientRegistryInstance.mu.Unlock()
	}()

	m := NewPlatformKubernetes("gke-cluster", map[string]any{"type": "gke"})
	err := m.Init(NewMockApplication())
	if err == nil {
		t.Fatal("Init must fail when no gke plugin client is registered")
	}
	if !strings.Contains(err.Error(), "plugin-provided backend") {
		t.Fatalf("error must explain generic plugin ownership, got: %v", err)
	}
	if strings.Contains(err.Error(), "workflow-plugin-gcp") {
		t.Fatalf("error must not hardcode a provider plugin, got: %v", err)
	}
}

func TestPlatformKubernetes_ScopedRegistryDoesNotFallBackToGlobal(t *testing.T) {
	const clusterType = "scoped-miss"
	globalClient := &fakeResourceDriverClient{}
	if err := kubernetesBackendClientRegistryInstance.register(clusterType, globalClient); err != nil {
		t.Fatalf("register legacy global: %v", err)
	}
	defer func() {
		kubernetesBackendClientRegistryInstance.mu.Lock()
		delete(kubernetesBackendClientRegistryInstance.bindings, clusterType)
		delete(kubernetesBackendClientRegistryInstance.owners, clusterType)
		kubernetesBackendClientRegistryInstance.mu.Unlock()
	}()

	app := NewMockApplication()
	if err := app.RegisterService(testKubernetesBackendRegistryServiceName, &testScopedKubernetesBackendRegistry{
		clients: map[string]pb.ResourceDriverClient{},
	}); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	m := NewPlatformKubernetes("scoped-miss-cluster", map[string]any{"type": clusterType})
	err := m.Init(app)
	if err == nil {
		t.Fatal("Init must fail instead of resolving a package-global backend when a scoped registry exists")
	}
	if !strings.Contains(err.Error(), "install and load the plugin") {
		t.Fatalf("Init error = %v, want scoped missing-provider guidance", err)
	}
}

func TestPlatformKubernetes_PrefersScopedProviderForCompatibilityBackends(t *testing.T) {
	for _, clusterType := range []string{"aks", "eks"} {
		t.Run(clusterType, func(t *testing.T) {
			app := NewMockApplication()
			if err := app.RegisterService(testKubernetesBackendRegistryServiceName, &testScopedKubernetesBackendRegistry{
				clients: map[string]pb.ResourceDriverClient{clusterType: &fakeResourceDriverClient{}},
			}); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}

			m := NewPlatformKubernetes(clusterType+"-cluster", map[string]any{"type": clusterType})
			if err := m.Init(app); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if _, ok := m.backend.(*grpcKubernetesBackend); !ok {
				t.Fatalf("m.backend = %T, want provider-owned *grpcKubernetesBackend", m.backend)
			}
		})
	}
}

func TestPlatformKubernetes_NoProviderKeepsCompatibilityFallbacks(t *testing.T) {
	tests := []struct {
		clusterType string
		assert      func(t *testing.T, backend kubernetesBackend)
	}{
		{"aks", func(t *testing.T, backend kubernetesBackend) {
			if _, ok := backend.(*aksBackend); !ok {
				t.Fatalf("backend = %T, want *aksBackend", backend)
			}
		}},
		{"eks", func(t *testing.T, backend kubernetesBackend) {
			if _, ok := backend.(*eksErrorBackend); !ok {
				t.Fatalf("backend = %T, want *eksErrorBackend", backend)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.clusterType, func(t *testing.T) {
			app := NewMockApplication()
			if err := app.RegisterService(testKubernetesBackendRegistryServiceName, &testScopedKubernetesBackendRegistry{
				clients: map[string]pb.ResourceDriverClient{},
			}); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}

			m := NewPlatformKubernetes(tt.clusterType+"-cluster", map[string]any{"type": tt.clusterType})
			if err := m.Init(app); err != nil {
				t.Fatalf("Init: %v", err)
			}
			tt.assert(t, m.backend)
		})
	}
}

func TestPlatformKubernetes_CoreLocalBackendsCannotBeClaimed(t *testing.T) {
	for _, clusterType := range []string{"kind", "k3s"} {
		t.Run(clusterType, func(t *testing.T) {
			app := NewMockApplication()
			if err := app.RegisterService(testKubernetesBackendRegistryServiceName, &testScopedKubernetesBackendRegistry{
				clients: map[string]pb.ResourceDriverClient{clusterType: &fakeResourceDriverClient{}},
			}); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}

			m := NewPlatformKubernetes(clusterType+"-cluster", map[string]any{"type": clusterType})
			if err := m.Init(app); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if _, ok := m.backend.(*kindBackend); !ok {
				t.Fatalf("m.backend = %T, want unclaimable core-local *kindBackend", m.backend)
			}
		})
	}
}
