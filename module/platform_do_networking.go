package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/digitalocean/godo"
)

// DOVPCState holds the current state of a DigitalOcean VPC.
type DOVPCState struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Region      string            `json:"region"`
	IPRange     string            `json:"ipRange"`
	Status      string            `json:"status"` // pending, active, deleting, deleted
	FirewallIDs []string          `json:"firewallIds"`
	LBID        string            `json:"lbId"`
	Tags        map[string]string `json:"tags"`
}

// DOFirewallRule describes a single firewall rule (inbound or outbound).
type DOFirewallRule struct {
	Protocol string `json:"protocol"` // tcp, udp, icmp
	PortRange string `json:"portRange"` // e.g. "80" or "8000-9000"
	Sources   string `json:"sources"`   // CIDR, tag, or load_balancer_uid
}

// DOFirewallConfig describes a DigitalOcean firewall.
type DOFirewallConfig struct {
	Name         string           `json:"name"`
	InboundRules []DOFirewallRule `json:"inboundRules"`
	OutboundRules []DOFirewallRule `json:"outboundRules"`
}

// DONetworkPlan describes planned networking changes.
type DONetworkPlan struct {
	VPC       string             `json:"vpc"`
	Firewalls []DOFirewallConfig `json:"firewalls"`
	Changes   []string           `json:"changes"`
}

// doNetworkingBackend is the interface DO networking backends implement.
type doNetworkingBackend interface {
	plan(m *PlatformDONetworking) (*DONetworkPlan, error)
	apply(m *PlatformDONetworking) (*DOVPCState, error)
	status(m *PlatformDONetworking) (*DOVPCState, error)
	destroy(m *PlatformDONetworking) error
}

// PlatformDONetworking manages DigitalOcean VPCs, firewalls, and load balancers.
// Config:
//
//	account:   name of a cloud.account module (provider=digitalocean)
//	provider:  digitalocean | mock
//	vpc:       vpc config (name, region, ip_range)
//	firewalls: list of firewall configs
type PlatformDONetworking struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *DOVPCState
	backend  doNetworkingBackend
}

// NewPlatformDONetworking creates a new PlatformDONetworking module.
func NewPlatformDONetworking(name string, cfg map[string]any) *PlatformDONetworking {
	return &PlatformDONetworking{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformDONetworking) Name() string { return m.name }

// Init resolves the cloud.account service and initializes the backend.
func (m *PlatformDONetworking) Init(app modular.Application) error {
	accountName, _ := m.config["account"].(string)
	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.do_networking %q: account service %q not found", m.name, accountName)
		}
		prov, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.do_networking %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = prov
		if providerType == "mock" {
			providerType = prov.Provider()
		}
	}

	vpc := m.vpcConfig()
	m.state = &DOVPCState{
		Name:    vpc["name"],
		Region:  vpc["region"],
		IPRange: vpc["ip_range"],
		Status:  "pending",
	}

	switch providerType {
	case "mock":
		m.backend = &doNetworkingMockBackend{}
	case "digitalocean":
		acc, ok := app.SvcRegistry()[accountName].(*CloudAccount)
		if !ok {
			return fmt.Errorf("platform.do_networking %q: account %q is not a *CloudAccount", m.name, accountName)
		}
		client, err := acc.doClient()
		if err != nil {
			return fmt.Errorf("platform.do_networking %q: %w", m.name, err)
		}
		m.backend = &doNetworkingRealBackend{client: client}
	default:
		return fmt.Errorf("platform.do_networking %q: unsupported provider %q", m.name, providerType)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformDONetworking) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DO networking: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil.
func (m *PlatformDONetworking) RequiresServices() []modular.ServiceDependency { return nil }

// Plan returns the planned networking changes.
func (m *PlatformDONetworking) Plan() (*DONetworkPlan, error) { return m.backend.plan(m) }

// Apply creates or updates the VPC and firewalls.
func (m *PlatformDONetworking) Apply() (*DOVPCState, error) { return m.backend.apply(m) }

// Status returns the current VPC state.
func (m *PlatformDONetworking) Status() (*DOVPCState, error) { return m.backend.status(m) }

// Destroy deletes the VPC and associated resources.
func (m *PlatformDONetworking) Destroy() error { return m.backend.destroy(m) }

// vpcConfig parses VPC config from module config.
func (m *PlatformDONetworking) vpcConfig() map[string]string {
	result := map[string]string{
		"name":     m.name,
		"region":   "nyc3",
		"ip_range": "10.10.10.0/24",
	}
	raw, ok := m.config["vpc"].(map[string]any)
	if !ok {
		return result
	}
	if n, ok := raw["name"].(string); ok && n != "" {
		result["name"] = n
	}
	if r, ok := raw["region"].(string); ok && r != "" {
		result["region"] = r
	}
	if ip, ok := raw["ip_range"].(string); ok && ip != "" {
		result["ip_range"] = ip
	}
	return result
}

// firewallConfigs parses firewall configs from module config.
func (m *PlatformDONetworking) firewallConfigs() []DOFirewallConfig {
	raw, ok := m.config["firewalls"].([]any)
	if !ok {
		return nil
	}
	var fws []DOFirewallConfig
	for _, item := range raw {
		fw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := fw["name"].(string)
		fws = append(fws, DOFirewallConfig{Name: name})
	}
	return fws
}

// ─── mock backend ──────────────────────────────────────────────────────────────

type doNetworkingMockBackend struct{}

func (b *doNetworkingMockBackend) plan(m *PlatformDONetworking) (*DONetworkPlan, error) {
	if m.state.Status == "active" {
		return &DONetworkPlan{
			VPC:     m.state.Name,
			Changes: []string{"no changes"},
		}, nil
	}
	fws := m.firewallConfigs()
	changes := []string{fmt.Sprintf("create VPC %q in %s (%s)", m.state.Name, m.state.Region, m.state.IPRange)}
	for _, fw := range fws {
		changes = append(changes, fmt.Sprintf("create firewall %q", fw.Name))
	}
	return &DONetworkPlan{
		VPC:       m.state.Name,
		Firewalls: fws,
		Changes:   changes,
	}, nil
}

func (b *doNetworkingMockBackend) apply(m *PlatformDONetworking) (*DOVPCState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}
	m.state.ID = fmt.Sprintf("mock-vpc-%s", m.state.Name)
	m.state.Status = "active"
	fws := m.firewallConfigs()
	for i, fw := range fws {
		m.state.FirewallIDs = append(m.state.FirewallIDs, fmt.Sprintf("mock-fw-%d-%s", i, fw.Name))
	}
	return m.state, nil
}

func (b *doNetworkingMockBackend) status(m *PlatformDONetworking) (*DOVPCState, error) {
	return m.state, nil
}

func (b *doNetworkingMockBackend) destroy(m *PlatformDONetworking) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.FirewallIDs = nil
	m.state.LBID = ""
	return nil
}

// ─── real backend ──────────────────────────────────────────────────────────────

type doNetworkingRealBackend struct {
	client *godo.Client
}

func (b *doNetworkingRealBackend) plan(m *PlatformDONetworking) (*DONetworkPlan, error) {
	fws := m.firewallConfigs()
	changes := []string{fmt.Sprintf("create VPC %q in %s (%s)", m.state.Name, m.state.Region, m.state.IPRange)}
	for _, fw := range fws {
		changes = append(changes, fmt.Sprintf("create firewall %q", fw.Name))
	}
	return &DONetworkPlan{
		VPC:       m.state.Name,
		Firewalls: fws,
		Changes:   changes,
	}, nil
}

func (b *doNetworkingRealBackend) apply(m *PlatformDONetworking) (*DOVPCState, error) {
	req := &godo.VPCCreateRequest{
		Name:      m.state.Name,
		RegionSlug: m.state.Region,
		IPRange:   m.state.IPRange,
	}
	vpc, _, err := b.client.VPCs.Create(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("do_networking create VPC: %w", err)
	}
	m.state.ID = vpc.ID
	m.state.Status = "active"

	fws := m.firewallConfigs()
	for _, fw := range fws {
		fwReq := &godo.FirewallRequest{Name: fw.Name}
		created, _, fwErr := b.client.Firewalls.Create(context.Background(), fwReq)
		if fwErr != nil {
			return nil, fmt.Errorf("do_networking create firewall %q: %w", fw.Name, fwErr)
		}
		m.state.FirewallIDs = append(m.state.FirewallIDs, created.ID)
	}
	return m.state, nil
}

func (b *doNetworkingRealBackend) status(m *PlatformDONetworking) (*DOVPCState, error) {
	if m.state.ID == "" {
		return m.state, nil
	}
	vpc, _, err := b.client.VPCs.Get(context.Background(), m.state.ID)
	if err != nil {
		return nil, fmt.Errorf("do_networking get VPC: %w", err)
	}
	m.state.Name = vpc.Name
	m.state.Region = vpc.RegionSlug
	m.state.IPRange = vpc.IPRange
	return m.state, nil
}

func (b *doNetworkingRealBackend) destroy(m *PlatformDONetworking) error {
	for _, fwID := range m.state.FirewallIDs {
		if _, err := b.client.Firewalls.Delete(context.Background(), fwID); err != nil {
			return fmt.Errorf("do_networking delete firewall %q: %w", fwID, err)
		}
	}
	if m.state.ID != "" {
		if _, err := b.client.VPCs.Delete(context.Background(), m.state.ID); err != nil {
			return fmt.Errorf("do_networking delete VPC: %w", err)
		}
	}
	m.state.Status = "deleted"
	m.state.FirewallIDs = nil
	return nil
}
