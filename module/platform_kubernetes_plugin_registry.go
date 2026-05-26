package module

import (
	"fmt"
	"strings"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ─────────────────────────────────────────────────────────────────────────────
// kubernetesBackendClientRegistry — engine-side registry mapping a
// platform.kubernetes cluster type name to a plugin-served ResourceDriver gRPC
// client.
//
// The engine populates the package-level singleton at plugin-load time;
// PlatformKubernetes.Init consults it for any cluster type not handled by an
// in-core backend. Reserved in-core type names (kind/k3s/eks/aks) — the
// SDK-free backends that stay in core — cannot be claimed by a plugin.
//
// Structurally identical to iacStateBackendRegistry
// (module/iac_state_plugin_registry.go); per ADR 0037 a kubernetes backend is
// served over the existing ResourceDriver contract — no new proto surface.
// ─────────────────────────────────────────────────────────────────────────────

// reservedKubernetesBackendTypes are the in-core cluster type names a plugin may
// never claim — the backends registered in platform_kubernetes_core.go. `gke`
// is intentionally absent: it is the cloud-SDK-bearing backend that moves to
// workflow-plugin-gcp and is therefore plugin-served.
var reservedKubernetesBackendTypes = map[string]struct{}{
	"kind": {},
	"k3s":  {},
	"eks":  {},
	"aks":  {},
}

// kubernetesBackendClientRegistry maps a cluster type name to a plugin gRPC
// client.
type kubernetesBackendClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]pb.ResourceDriverClient
}

// newKubernetesBackendClientRegistry constructs an empty registry.
func newKubernetesBackendClientRegistry() *kubernetesBackendClientRegistry {
	return &kubernetesBackendClientRegistry{clients: make(map[string]pb.ResourceDriverClient)}
}

// register associates a cluster type name with a plugin client. The name must
// be non-empty (after trimming) and the client must be non-nil. Reserved
// in-core type names are rejected. Re-registering a non-reserved name
// overwrites the previous client (last plugin loaded wins).
func (r *kubernetesBackendClientRegistry) register(name string, client pb.ResourceDriverClient) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("kubernetes backend registration: name must not be empty")
	}
	if client == nil {
		return fmt.Errorf("kubernetes backend registration %q: client must not be nil", name)
	}
	if _, reserved := reservedKubernetesBackendTypes[name]; reserved {
		return fmt.Errorf("plugin registered reserved kubernetes backend type %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = client
	return nil
}

// resolve returns the plugin client for a cluster type name, and whether one is
// registered.
func (r *kubernetesBackendClientRegistry) resolve(name string) (pb.ResourceDriverClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

// kubernetesBackendClientRegistryInstance is the package-level singleton the
// engine populates and PlatformKubernetes.Init consults.
var kubernetesBackendClientRegistryInstance = newKubernetesBackendClientRegistry()

// RegisterKubernetesBackendClient associates a platform.kubernetes cluster type
// with a plugin-served ResourceDriver gRPC client in the package-level
// registry. The engine calls this at plugin-load for each kubernetes backend a
// loaded plugin serves (e.g. `gke` via workflow-plugin-gcp);
// PlatformKubernetes.Init then resolves `type: <name>` configs against it.
// Reserved in-core type names (kind/k3s/eks/aks) and empty names / nil clients
// are rejected — see kubernetesBackendClientRegistry.register. Per ADR 0037.
func RegisterKubernetesBackendClient(name string, client pb.ResourceDriverClient) error {
	return kubernetesBackendClientRegistryInstance.register(name, client)
}
