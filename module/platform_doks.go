package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/digitalocean/godo"
)

// DOKSClusterState holds the current state of a managed DOKS cluster.
type DOKSClusterState struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Region    string              `json:"region"`
	Version   string              `json:"version"`
	Status    string              `json:"status"` // pending, creating, running, deleting, deleted
	NodePools []DOKSNodePoolState `json:"nodePools"`
	Endpoint  string              `json:"endpoint"`
	CreatedAt time.Time           `json:"createdAt"`
}

// DOKSNodePoolState describes a DOKS node pool.
type DOKSNodePoolState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      string `json:"size"`
	Count     int    `json:"count"`
	AutoScale bool   `json:"autoScale"`
	MinNodes  int    `json:"minNodes"`
	MaxNodes  int    `json:"maxNodes"`
}

// doksBackend is the internal interface DOKS backends implement.
type doksBackend interface {
	create(m *PlatformDOKS) (*DOKSClusterState, error)
	get(m *PlatformDOKS) (*DOKSClusterState, error)
	delete(m *PlatformDOKS) error
	listNodePools(m *PlatformDOKS) ([]DOKSNodePoolState, error)
}

// PlatformDOKS manages DigitalOcean Kubernetes (DOKS) clusters.
// Config:
//
//	account:      name of a cloud.account module (provider=digitalocean)
//	cluster_name: DOKS cluster name
//	region:       DO region slug (e.g. nyc3)
//	version:      Kubernetes version slug (e.g. 1.29.1-do.0)
//	node_pool:    node pool config (size, count, auto_scale, min_nodes, max_nodes)
type PlatformDOKS struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *DOKSClusterState
	backend  doksBackend
}

// NewPlatformDOKS creates a new PlatformDOKS module.
func NewPlatformDOKS(name string, cfg map[string]any) *PlatformDOKS {
	return &PlatformDOKS{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformDOKS) Name() string { return m.name }

// Init resolves the cloud.account service and initializes the backend.
func (m *PlatformDOKS) Init(app modular.Application) error {
	clusterName, _ := m.config["cluster_name"].(string)
	if clusterName == "" {
		clusterName = m.name
	}

	region, _ := m.config["region"].(string)
	if region == "" {
		region = "nyc3"
	}

	version, _ := m.config["version"].(string)
	if version == "" {
		version = "1.29.1-do.0"
	}

	accountName, _ := m.config["account"].(string)
	providerType := "mock"

	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.doks %q: account service %q not found", m.name, accountName)
		}
		prov, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.doks %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = prov
		providerType = prov.Provider()
	}

	m.state = &DOKSClusterState{
		Name:    clusterName,
		Region:  region,
		Version: version,
		Status:  "pending",
	}

	switch providerType {
	case "digitalocean":
		acc, ok := app.SvcRegistry()[accountName].(*CloudAccount)
		if !ok {
			return fmt.Errorf("platform.doks %q: account %q is not a *CloudAccount", m.name, accountName)
		}
		client, err := acc.doClient()
		if err != nil {
			return fmt.Errorf("platform.doks %q: %w", m.name, err)
		}
		m.backend = &doksRealBackend{client: client}
	default:
		m.backend = &doksMockBackend{}
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformDOKS) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DOKS cluster: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil.
func (m *PlatformDOKS) RequiresServices() []modular.ServiceDependency { return nil }

// Create creates the DOKS cluster.
func (m *PlatformDOKS) Create() (*DOKSClusterState, error) {
	return m.backend.create(m)
}

// Get returns the current cluster state.
func (m *PlatformDOKS) Get() (*DOKSClusterState, error) {
	return m.backend.get(m)
}

// Delete removes the DOKS cluster.
func (m *PlatformDOKS) Delete() error {
	return m.backend.delete(m)
}

// ListNodePools returns the node pools for the cluster.
func (m *PlatformDOKS) ListNodePools() ([]DOKSNodePoolState, error) {
	return m.backend.listNodePools(m)
}

// nodePoolConfig parses node pool config from module config.
func (m *PlatformDOKS) nodePoolConfig() DOKSNodePoolState {
	raw, ok := m.config["node_pool"].(map[string]any)
	if !ok {
		return DOKSNodePoolState{Name: "default", Size: "s-2vcpu-2gb", Count: 3}
	}
	name, _ := raw["name"].(string)
	if name == "" {
		name = "default"
	}
	size, _ := raw["size"].(string)
	if size == "" {
		size = "s-2vcpu-2gb"
	}
	count, _ := intFromAny(raw["count"])
	if count == 0 {
		count = 3
	}
	autoScale, _ := raw["auto_scale"].(bool)
	minNodes, _ := intFromAny(raw["min_nodes"])
	maxNodes, _ := intFromAny(raw["max_nodes"])
	return DOKSNodePoolState{
		Name:      name,
		Size:      size,
		Count:     count,
		AutoScale: autoScale,
		MinNodes:  minNodes,
		MaxNodes:  maxNodes,
	}
}

// ─── mock backend ──────────────────────────────────────────────────────────────

type doksMockBackend struct{}

func (b *doksMockBackend) create(m *PlatformDOKS) (*DOKSClusterState, error) {
	if m.state.Status == "running" {
		return m.state, nil
	}
	m.state.Status = "creating"
	m.state.ID = fmt.Sprintf("mock-doks-%s", m.state.Name)
	m.state.Endpoint = fmt.Sprintf("https://%s.k8s.ondigitalocean.com", m.state.Name)
	m.state.CreatedAt = time.Now()
	np := m.nodePoolConfig()
	np.ID = fmt.Sprintf("mock-pool-%s", np.Name)
	m.state.NodePools = []DOKSNodePoolState{np}
	m.state.Status = "running"
	return m.state, nil
}

func (b *doksMockBackend) get(m *PlatformDOKS) (*DOKSClusterState, error) {
	return m.state, nil
}

func (b *doksMockBackend) delete(m *PlatformDOKS) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.NodePools = nil
	return nil
}

func (b *doksMockBackend) listNodePools(m *PlatformDOKS) ([]DOKSNodePoolState, error) {
	return m.state.NodePools, nil
}

// ─── real backend ──────────────────────────────────────────────────────────────

// doksRealBackend uses godo to manage real DOKS clusters.
type doksRealBackend struct {
	client *godo.Client
}

func (b *doksRealBackend) create(m *PlatformDOKS) (*DOKSClusterState, error) {
	np := m.nodePoolConfig()
	nodePool := &godo.KubernetesNodePoolCreateRequest{
		Name:      np.Name,
		Size:      np.Size,
		Count:     np.Count,
		AutoScale: np.AutoScale,
		MinNodes:  np.MinNodes,
		MaxNodes:  np.MaxNodes,
	}

	req := &godo.KubernetesClusterCreateRequest{
		Name:        m.state.Name,
		RegionSlug:  m.state.Region,
		VersionSlug: m.state.Version,
		NodePools:   []*godo.KubernetesNodePoolCreateRequest{nodePool},
	}

	cluster, _, err := b.client.Kubernetes.Create(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("doks create: %w", err)
	}

	return doksClusterToState(cluster), nil
}

func (b *doksRealBackend) get(m *PlatformDOKS) (*DOKSClusterState, error) {
	if m.state.ID == "" {
		return m.state, nil
	}
	cluster, _, err := b.client.Kubernetes.Get(context.Background(), m.state.ID)
	if err != nil {
		return nil, fmt.Errorf("doks get: %w", err)
	}
	state := doksClusterToState(cluster)
	m.state = state
	return state, nil
}

func (b *doksRealBackend) delete(m *PlatformDOKS) error {
	if m.state.ID == "" {
		return nil
	}
	_, err := b.client.Kubernetes.Delete(context.Background(), m.state.ID)
	if err != nil {
		return fmt.Errorf("doks delete: %w", err)
	}
	m.state.Status = "deleted"
	m.state.NodePools = nil
	return nil
}

func (b *doksRealBackend) listNodePools(m *PlatformDOKS) ([]DOKSNodePoolState, error) {
	if m.state.ID == "" {
		return nil, nil
	}
	pools, _, err := b.client.Kubernetes.ListNodePools(context.Background(), m.state.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("doks list node pools: %w", err)
	}
	var result []DOKSNodePoolState
	for _, p := range pools {
		result = append(result, DOKSNodePoolState{
			ID:        p.ID,
			Name:      p.Name,
			Size:      p.Size,
			Count:     p.Count,
			AutoScale: p.AutoScale,
			MinNodes:  p.MinNodes,
			MaxNodes:  p.MaxNodes,
		})
	}
	return result, nil
}

// doksClusterToState converts a godo.KubernetesCluster to DOKSClusterState.
func doksClusterToState(c *godo.KubernetesCluster) *DOKSClusterState {
	state := &DOKSClusterState{
		ID:        c.ID,
		Name:      c.Name,
		Region:    c.RegionSlug,
		Version:   c.VersionSlug,
		Status:    string(c.Status.State),
		CreatedAt: c.CreatedAt,
	}
	if c.Endpoint != "" {
		state.Endpoint = c.Endpoint
	}
	for _, p := range c.NodePools {
		state.NodePools = append(state.NodePools, DOKSNodePoolState{
			ID:        p.ID,
			Name:      p.Name,
			Size:      p.Size,
			Count:     p.Count,
			AutoScale: p.AutoScale,
			MinNodes:  p.MinNodes,
			MaxNodes:  p.MaxNodes,
		})
	}
	return state
}
