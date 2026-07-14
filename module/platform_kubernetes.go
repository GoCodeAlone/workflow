package module

import (
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

// KubernetesClusterState holds the current state of a managed Kubernetes cluster.
type KubernetesClusterState struct {
	Name       string           `json:"name"`
	Provider   string           `json:"provider"` // selected core or plugin-declared backend name
	Version    string           `json:"version"`
	Status     string           `json:"status"` // pending, creating, running, deleting, deleted
	Endpoint   string           `json:"endpoint"`
	NodeGroups []NodeGroupState `json:"nodeGroups"`
	CreatedAt  time.Time        `json:"createdAt"`
}

// NodeGroupState describes a node group within a cluster.
type NodeGroupState struct {
	Name         string `json:"name"`
	InstanceType string `json:"instanceType"`
	Min          int    `json:"min"`
	Max          int    `json:"max"`
	Current      int    `json:"current"`
}

// PlatformKubernetes manages Kubernetes clusters via pluggable backends.
// Config:
//
//	account:    name of a cloud.account module (resolved from service registry)
//	type:       kind, k3s, or an exact backend name declared by a loaded plugin
//	version:    Kubernetes version (e.g. "1.29")
//	nodeGroups: list of node group definitions
type PlatformKubernetes struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider // resolved from service registry
	state    *KubernetesClusterState
	backend  kubernetesBackend
}

// kubernetesBackend is the internal interface that cluster-type backends implement.
type kubernetesBackend interface {
	plan(k *PlatformKubernetes) (*PlatformPlan, error)
	apply(k *PlatformKubernetes) (*PlatformResult, error)
	status(k *PlatformKubernetes) (*KubernetesClusterState, error)
	destroy(k *PlatformKubernetes) error
}

// KubernetesBackendFactory creates a kubernetesBackend for a given cluster type config.
type KubernetesBackendFactory func(cfg map[string]any) (kubernetesBackend, error)

type kubernetesBackendResolver interface {
	ResolveKubernetesBackend(string) (KubernetesBackendBinding, string, bool)
}

// kubernetesBackendRegistry maps cluster type name to its factory.
var kubernetesBackendRegistry = map[string]KubernetesBackendFactory{}

// RegisterKubernetesBackend registers a KubernetesBackendFactory for the given cluster type.
func RegisterKubernetesBackend(clusterType string, factory KubernetesBackendFactory) {
	kubernetesBackendRegistry[clusterType] = factory
}

// NewPlatformKubernetes creates a new PlatformKubernetes module.
func NewPlatformKubernetes(name string, cfg map[string]any) *PlatformKubernetes {
	return &PlatformKubernetes{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformKubernetes) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformKubernetes) Init(app modular.Application) error {
	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.kubernetes %q: account service %q not found", m.name, accountName)
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.kubernetes %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = provider
	}

	clusterType, _ := m.config["type"].(string)
	if clusterType == "" {
		clusterType = "kind"
	}

	// kind/k3s are deliberately core-local and never consult plugin state.
	if _, coreLocal := reservedKubernetesBackendTypes[clusterType]; coreLocal {
		factory, ok := kubernetesBackendRegistry[clusterType]
		if !ok {
			return fmt.Errorf("platform.kubernetes %q: unsupported type %q", m.name, clusterType)
		}
		backend, err := factory(m.config)
		if err != nil {
			return fmt.Errorf("platform.kubernetes %q: creating backend: %w", m.name, err)
		}
		m.backend = backend
	} else {
		binding, scoped, err := resolveApplicationKubernetesBackend(app, clusterType)
		if err != nil {
			return fmt.Errorf("platform.kubernetes %q: %w", m.name, err)
		}
		if !scoped {
			// Compatibility for callers that initialize modules without a
			// StdEngine. Engine-built applications always publish a scoped
			// registry and never consult this singleton.
			binding, _ = kubernetesBackendClientRegistryInstance.resolve(clusterType)
		}
		if binding.Client != nil {
			m.backend = newGRPCKubernetesBackend(binding.Name, binding.ResourceType, binding.Client)
		} else if factory, ok := kubernetesBackendRegistry[clusterType]; ok {
			// Retained aks/eks compatibility backends apply only when the
			// current engine has no provider declaration for the exact name.
			backend, createErr := factory(m.config)
			if createErr != nil {
				return fmt.Errorf("platform.kubernetes %q: creating backend: %w", m.name, createErr)
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
		Name:     m.name,
		Provider: clusterType,
		Version:  version,
		Status:   "pending",
	}

	return app.RegisterService(m.name, m)
}

func resolveApplicationKubernetesBackend(app modular.Application, name string) (KubernetesBackendBinding, bool, error) {
	service, scoped := app.SvcRegistry()[KubernetesBackendRegistryServiceName]
	if !scoped {
		return KubernetesBackendBinding{}, false, nil
	}
	resolver, ok := service.(kubernetesBackendResolver)
	if !ok {
		return KubernetesBackendBinding{}, true, fmt.Errorf("service %q has incompatible type %T", KubernetesBackendRegistryServiceName, service)
	}
	binding, _, found := resolver.ResolveKubernetesBackend(name)
	if !found {
		return KubernetesBackendBinding{}, true, nil
	}
	return binding, true, nil
}

// ProvidesServices declares the service this module provides.
func (m *PlatformKubernetes) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Kubernetes cluster: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name, not declared.
func (m *PlatformKubernetes) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the changes that would be made to bring the cluster to desired state.
func (m *PlatformKubernetes) Plan() (*PlatformPlan, error) {
	return m.backend.plan(m)
}

// Apply makes the cluster match the desired configuration.
func (m *PlatformKubernetes) Apply() (*PlatformResult, error) {
	return m.backend.apply(m)
}

// Status returns the current cluster state.
func (m *PlatformKubernetes) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy tears down the cluster.
func (m *PlatformKubernetes) Destroy() error {
	return m.backend.destroy(m)
}

// clusterName returns the configured cluster name, falling back to the module name.
func (m *PlatformKubernetes) clusterName() string {
	if n, ok := m.config["clusterName"].(string); ok && n != "" {
		return n
	}
	return m.name
}

// nodeGroups parses the nodeGroups config into NodeGroupState slices.
func (m *PlatformKubernetes) nodeGroups() []NodeGroupState {
	raw, ok := m.config["nodeGroups"].([]any)
	if !ok {
		return nil
	}
	var groups []NodeGroupState
	for _, item := range raw {
		ng, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := ng["name"].(string)
		instanceType, _ := ng["instanceType"].(string)
		min, _ := intFromAny(ng["min"])
		max, _ := intFromAny(ng["max"])
		if min == 0 {
			min = 1
		}
		if max == 0 {
			max = min
		}
		groups = append(groups, NodeGroupState{
			Name:         name,
			InstanceType: instanceType,
			Min:          min,
			Max:          max,
			Current:      min,
		})
	}
	return groups
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
