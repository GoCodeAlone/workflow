package module

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// KubernetesBackendRegistryServiceName is the application service through
// which platform.kubernetes resolves the current engine's plugin backends.
const KubernetesBackendRegistryServiceName = "workflow.internal.kubernetes-backend-registry"

const legacyKubernetesBackendResourceType = "infra.k8s_cluster"

var reservedKubernetesBackendTypes = map[string]struct{}{
	"kind": {},
	"k3s":  {},
}

// KubernetesBackendBinding is the exact manifest/runtime route selected for a
// platform.kubernetes backend name.
type KubernetesBackendBinding struct {
	Name         string
	ResourceType string
	Client       pb.ResourceDriverClient
}

// KubernetesBackendRegistry maps backend names to complete bindings and plugin
// owners. Each engine owns one registry.
type KubernetesBackendRegistry struct {
	mu       sync.RWMutex
	bindings map[string]KubernetesBackendBinding
	owners   map[string]string
}

func NewKubernetesBackendRegistry() *KubernetesBackendRegistry {
	return &KubernetesBackendRegistry{
		bindings: make(map[string]KubernetesBackendBinding),
		owners:   make(map[string]string),
	}
}

// Preflight validates a complete owner batch and rejects cross-owner names
// without changing registry state. StdEngine serializes Preflight through the
// matching Register call so a rejected collision precedes generic loader
// mutation.
func (r *KubernetesBackendRegistry) Preflight(owner string, bindings []KubernetesBackendBinding) error {
	owner, normalized, err := normalizeKubernetesBackendRegistration(owner, bindings)
	if err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkCollisionsLocked(owner, normalized)
}

// Register atomically replaces one owner's complete binding set. A
// cross-owner collision rejects the full batch before mutation.
func (r *KubernetesBackendRegistry) Register(owner string, bindings []KubernetesBackendBinding) error {
	owner, normalized, err := normalizeKubernetesBackendRegistration(owner, bindings)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkCollisionsLocked(owner, normalized); err != nil {
		return err
	}

	nextBindings := make(map[string]KubernetesBackendBinding, len(r.bindings)+len(normalized))
	nextOwners := make(map[string]string, len(r.owners)+len(normalized))
	for name, binding := range r.bindings {
		if r.owners[name] == owner {
			continue
		}
		nextBindings[name] = binding
		nextOwners[name] = r.owners[name]
	}
	for name, binding := range normalized {
		nextBindings[name] = binding
		nextOwners[name] = owner
	}
	r.bindings = nextBindings
	r.owners = nextOwners
	return nil
}

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
		if _, reserved := reservedKubernetesBackendTypes[name]; reserved {
			return "", nil, fmt.Errorf("plugin registered reserved kubernetes backend type %q", name)
		}
		if _, duplicate := normalized[name]; duplicate {
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

func (r *KubernetesBackendRegistry) checkCollisionsLocked(owner string, bindings map[string]KubernetesBackendBinding) error {
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if existingOwner, exists := r.owners[name]; exists && existingOwner != owner {
			return fmt.Errorf("kubernetes backend %q is already owned by plugin %q; plugin %q cannot claim it", name, existingOwner, owner)
		}
	}
	return nil
}

// ResolveKubernetesBackend returns the selected complete binding, its plugin
// owner, and whether the name is registered in this engine.
func (r *KubernetesBackendRegistry) ResolveKubernetesBackend(name string) (KubernetesBackendBinding, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name = strings.TrimSpace(name)
	binding, ok := r.bindings[name]
	return binding, r.owners[name], ok
}

// Backward-compatible package-private shape used by existing module tests and
// the legacy exported single-registration API.
type kubernetesBackendClientRegistry = KubernetesBackendRegistry

func newKubernetesBackendClientRegistry() *kubernetesBackendClientRegistry {
	return NewKubernetesBackendRegistry()
}

func (r *KubernetesBackendRegistry) register(name string, client pb.ResourceDriverClient) error {
	name = strings.TrimSpace(name)
	bindings := []KubernetesBackendBinding{{
		Name:         name,
		ResourceType: legacyKubernetesBackendResourceType,
		Client:       client,
	}}
	_, normalized, err := normalizeKubernetesBackendRegistration("workflow.legacy-global", bindings)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[name] = normalized[name]
	r.owners[name] = "workflow.legacy-global"
	return nil
}

func (r *KubernetesBackendRegistry) resolve(name string) (KubernetesBackendBinding, bool) {
	binding, _, ok := r.ResolveKubernetesBackend(name)
	return binding, ok
}

// kubernetesBackendClientRegistryInstance is retained only for direct module
// callers using the legacy exported registration API.
var kubernetesBackendClientRegistryInstance = newKubernetesBackendClientRegistry()

// RegisterKubernetesBackendClient preserves the legacy package-global API for
// direct module callers. Engine-built applications use exact scoped bindings.
func RegisterKubernetesBackendClient(name string, client pb.ResourceDriverClient) error {
	return kubernetesBackendClientRegistryInstance.register(name, client)
}
