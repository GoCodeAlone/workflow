package module

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ─────────────────────────────────────────────────────────────────────────────
// kubernetesBackendClientRegistry — engine-side registry mapping a
// platform.kubernetes cluster type name to a plugin-served ResourceDriver gRPC
// client.
//
// Each engine owns one registry and exposes it through its modular.Application;
// PlatformKubernetes.Init consults that scoped service before retained core
// compatibility backends. The package-level singleton remains only for the
// legacy exported RegisterKubernetesBackendClient API.
//
// Structurally identical to iacStateBackendRegistry
// (module/iac_state_plugin_registry.go); per ADR 0037 a kubernetes backend is
// served over the existing ResourceDriver contract — no new proto surface.
// ─────────────────────────────────────────────────────────────────────────────

// KubernetesBackendRegistryServiceName is the application service through
// which platform.kubernetes resolves the current engine's plugin backends.
const KubernetesBackendRegistryServiceName = "workflow.internal.kubernetes-backend-registry"

// reservedKubernetesBackendTypes are the deliberately core-local cluster type
// names a plugin may never claim. aks/eks retain core compatibility factories,
// but a loaded provider declaration takes precedence for those names.
var reservedKubernetesBackendTypes = map[string]struct{}{
	"kind": {},
	"k3s":  {},
}

// KubernetesBackendRegistry maps cluster type names to plugin owners and gRPC
// clients. Register replaces one owner's complete declaration set atomically.
type KubernetesBackendRegistry struct {
	mu      sync.RWMutex
	clients map[string]pb.ResourceDriverClient
	owners  map[string]string
}

// NewKubernetesBackendRegistry constructs an isolated registry for one engine.
func NewKubernetesBackendRegistry() *KubernetesBackendRegistry {
	return &KubernetesBackendRegistry{
		clients: make(map[string]pb.ResourceDriverClient),
		owners:  make(map[string]string),
	}
}

// Register validates and replaces owner's complete backend set under one lock.
// A backend already owned by another plugin rejects the entire batch before
// any mutation. The same owner may replace its set within this registry.
func (r *KubernetesBackendRegistry) Register(owner string, clients map[string]pb.ResourceDriverClient) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("kubernetes backend registration: owner must not be empty")
	}
	normalized, err := normalizeKubernetesBackendClients(clients)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(normalized))
	for name := range normalized {
		names = append(names, name)
	}
	sort.Strings(names)

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		if existingOwner, exists := r.owners[name]; exists && existingOwner != owner {
			return fmt.Errorf("kubernetes backend %q is already owned by plugin %q; plugin %q cannot claim it", name, existingOwner, owner)
		}
	}

	nextClients := make(map[string]pb.ResourceDriverClient, len(r.clients)+len(normalized))
	nextOwners := make(map[string]string, len(r.owners)+len(normalized))
	for name, client := range r.clients {
		if r.owners[name] == owner {
			continue
		}
		nextClients[name] = client
		nextOwners[name] = r.owners[name]
	}
	for name, client := range normalized {
		nextClients[name] = client
		nextOwners[name] = owner
	}
	r.clients = nextClients
	r.owners = nextOwners
	return nil
}

func normalizeKubernetesBackendClients(clients map[string]pb.ResourceDriverClient) (map[string]pb.ResourceDriverClient, error) {
	normalized := make(map[string]pb.ResourceDriverClient, len(clients))
	for rawName, client := range clients {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, fmt.Errorf("kubernetes backend registration: name must not be empty")
		}
		if client == nil {
			return nil, fmt.Errorf("kubernetes backend registration %q: client must not be nil", name)
		}
		if _, reserved := reservedKubernetesBackendTypes[name]; reserved {
			return nil, fmt.Errorf("plugin registered reserved kubernetes backend type %q", name)
		}
		if _, duplicate := normalized[name]; duplicate {
			return nil, fmt.Errorf("kubernetes backend registration has duplicate normalized name %q", name)
		}
		normalized[name] = client
	}
	return normalized, nil
}

// ResolveKubernetesBackend returns the selected client, its plugin owner, and
// whether the name is registered in this engine.
func (r *KubernetesBackendRegistry) ResolveKubernetesBackend(name string) (pb.ResourceDriverClient, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name = strings.TrimSpace(name)
	client, ok := r.clients[name]
	return client, r.owners[name], ok
}

// Backward-compatible package-private shape used by existing module tests and
// the legacy exported single-registration API.
type kubernetesBackendClientRegistry = KubernetesBackendRegistry

func newKubernetesBackendClientRegistry() *kubernetesBackendClientRegistry {
	return NewKubernetesBackendRegistry()
}

func (r *KubernetesBackendRegistry) register(name string, client pb.ResourceDriverClient) error {
	normalized, err := normalizeKubernetesBackendClients(map[string]pb.ResourceDriverClient{name: client})
	if err != nil {
		return err
	}
	for normalizedName, normalizedClient := range normalized {
		r.mu.Lock()
		r.clients[normalizedName] = normalizedClient
		r.owners[normalizedName] = "workflow.legacy-global"
		r.mu.Unlock()
	}
	return nil
}

func (r *KubernetesBackendRegistry) resolve(name string) (pb.ResourceDriverClient, bool) {
	client, _, ok := r.ResolveKubernetesBackend(name)
	return client, ok
}

// kubernetesBackendClientRegistryInstance is retained only for direct module
// callers using the legacy exported registration API.
var kubernetesBackendClientRegistryInstance = newKubernetesBackendClientRegistry()

// RegisterKubernetesBackendClient preserves the legacy package-global API for
// direct module callers. StdEngine uses its own KubernetesBackendRegistry and
// publishes that registry through KubernetesBackendRegistryServiceName.
// Reserved in-core type names (kind/k3s), empty names, and nil clients are
// rejected.
func RegisterKubernetesBackendClient(name string, client pb.ResourceDriverClient) error {
	return kubernetesBackendClientRegistryInstance.register(name, client)
}
