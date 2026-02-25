package module

import (
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// NetworkState holds the current state of a managed VPC network.
type NetworkState struct {
	VPCID            string            `json:"vpcId"`
	SubnetIDs        map[string]string `json:"subnetIds"`        // name → id
	SecurityGroupIDs map[string]string `json:"securityGroupIds"` // name → id
	NATGatewayID     string            `json:"natGatewayId"`
	Status           string            `json:"status"` // planned, active, destroying, destroyed
}

// VPCConfig describes the desired VPC configuration.
type VPCConfig struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
}

// SubnetConfig describes a single subnet.
type SubnetConfig struct {
	Name   string `json:"name"`
	CIDR   string `json:"cidr"`
	AZ     string `json:"az"`
	Public bool   `json:"public"`
}

// SecurityGroupRule describes a single inbound/outbound rule.
type SecurityGroupRule struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Source   string `json:"source"`
}

// SecurityGroupConfig describes a security group with its rules.
type SecurityGroupConfig struct {
	Name  string              `json:"name"`
	Rules []SecurityGroupRule `json:"rules"`
}

// NetworkPlan describes the changes a networking module intends to make.
type NetworkPlan struct {
	VPC            VPCConfig             `json:"vpc"`
	Subnets        []SubnetConfig        `json:"subnets"`
	NATGateway     bool                  `json:"natGateway"`
	SecurityGroups []SecurityGroupConfig `json:"securityGroups"`
	Changes        []string              `json:"changes"`
}

// networkBackend is the internal interface that provider backends implement.
type networkBackend interface {
	plan(m *PlatformNetworking) (*NetworkPlan, error)
	apply(m *PlatformNetworking) (*NetworkState, error)
	status(m *PlatformNetworking) (*NetworkState, error)
	destroy(m *PlatformNetworking) error
}

// PlatformNetworking manages VPC/subnet/security-group resources via pluggable backends.
// Config:
//
//	account:         name of a cloud.account module (optional for mock)
//	provider:        mock | aws | gcp | azure
//	vpc:             VPC config (cidr, name)
//	subnets:         list of subnet definitions
//	nat_gateway:     bool — provision a NAT gateway
//	security_groups: list of security group definitions
type PlatformNetworking struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *NetworkState
	backend  networkBackend
}

// NewPlatformNetworking creates a new PlatformNetworking module.
func NewPlatformNetworking(name string, cfg map[string]any) *PlatformNetworking {
	return &PlatformNetworking{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformNetworking) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformNetworking) Init(app modular.Application) error {
	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.networking %q: account service %q not found", m.name, accountName)
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.networking %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = provider
	}

	// Validate VPC config
	vpc := m.vpcConfig()
	if vpc.CIDR == "" {
		return fmt.Errorf("platform.networking %q: vpc.cidr is required", m.name)
	}

	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	switch providerType {
	case "mock":
		m.backend = &mockNetworkBackend{}
	case "aws":
		m.backend = &awsNetworkBackend{}
	default:
		return fmt.Errorf("platform.networking %q: unsupported provider %q", m.name, providerType)
	}

	m.state = &NetworkState{
		SubnetIDs:        make(map[string]string),
		SecurityGroupIDs: make(map[string]string),
		Status:           "planned",
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformNetworking) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Networking: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name, not declared.
func (m *PlatformNetworking) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the changes that would be made to bring the network to desired state.
func (m *PlatformNetworking) Plan() (*NetworkPlan, error) {
	return m.backend.plan(m)
}

// Apply provisions the VPC/subnets/security groups.
func (m *PlatformNetworking) Apply() (*NetworkState, error) {
	return m.backend.apply(m)
}

// Status returns the current network state.
func (m *PlatformNetworking) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy tears down the VPC and all associated resources.
func (m *PlatformNetworking) Destroy() error {
	return m.backend.destroy(m)
}

// vpcConfig parses the vpc config block.
func (m *PlatformNetworking) vpcConfig() VPCConfig {
	raw, ok := m.config["vpc"].(map[string]any)
	if !ok {
		return VPCConfig{}
	}
	cidr, _ := raw["cidr"].(string)
	name, _ := raw["name"].(string)
	if name == "" {
		name = m.name + "-vpc"
	}
	return VPCConfig{CIDR: cidr, Name: name}
}

// subnets parses the subnets config list.
func (m *PlatformNetworking) subnets() []SubnetConfig {
	raw, ok := m.config["subnets"].([]any)
	if !ok {
		return nil
	}
	var result []SubnetConfig
	for _, item := range raw {
		s, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := s["name"].(string)
		cidr, _ := s["cidr"].(string)
		az, _ := s["az"].(string)
		public, _ := s["public"].(bool)
		result = append(result, SubnetConfig{Name: name, CIDR: cidr, AZ: az, Public: public})
	}
	return result
}

// natGateway returns whether a NAT gateway should be provisioned.
func (m *PlatformNetworking) natGateway() bool {
	v, _ := m.config["nat_gateway"].(bool)
	return v
}

// securityGroups parses the security_groups config list.
func (m *PlatformNetworking) securityGroups() []SecurityGroupConfig {
	raw, ok := m.config["security_groups"].([]any)
	if !ok {
		return nil
	}
	var result []SecurityGroupConfig
	for _, item := range raw {
		sg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := sg["name"].(string)
		var rules []SecurityGroupRule
		if rawRules, ok := sg["rules"].([]any); ok {
			for _, r := range rawRules {
				rule, ok := r.(map[string]any)
				if !ok {
					continue
				}
				proto, _ := rule["protocol"].(string)
				source, _ := rule["source"].(string)
				port, _ := intFromAny(rule["port"])
				rules = append(rules, SecurityGroupRule{Protocol: proto, Port: port, Source: source})
			}
		}
		result = append(result, SecurityGroupConfig{Name: name, Rules: rules})
	}
	return result
}

// ─── mock backend ─────────────────────────────────────────────────────────────

// mockNetworkBackend implements networkBackend using in-memory state.
type mockNetworkBackend struct{}

func (b *mockNetworkBackend) plan(m *PlatformNetworking) (*NetworkPlan, error) {
	vpc := m.vpcConfig()
	subnets := m.subnets()
	sgs := m.securityGroups()
	nat := m.natGateway()

	plan := &NetworkPlan{
		VPC:            vpc,
		Subnets:        subnets,
		NATGateway:     nat,
		SecurityGroups: sgs,
	}

	switch m.state.Status {
	case "active":
		plan.Changes = []string{"noop: network already active"}
	default:
		plan.Changes = []string{
			fmt.Sprintf("create VPC %q (%s)", vpc.Name, vpc.CIDR),
		}
		for _, sn := range subnets {
			visibility := "private"
			if sn.Public {
				visibility = "public"
			}
			plan.Changes = append(plan.Changes, fmt.Sprintf("create %s subnet %q (%s) in %s", visibility, sn.Name, sn.CIDR, sn.AZ))
		}
		if nat {
			plan.Changes = append(plan.Changes, "create NAT gateway")
		}
		for _, sg := range sgs {
			plan.Changes = append(plan.Changes, fmt.Sprintf("create security group %q (%d rules)", sg.Name, len(sg.Rules)))
		}
	}

	return plan, nil
}

func (b *mockNetworkBackend) apply(m *PlatformNetworking) (*NetworkState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}

	vpc := m.vpcConfig()
	subnets := m.subnets()
	sgs := m.securityGroups()

	m.state.VPCID = fmt.Sprintf("vpc-mock-%s", vpc.Name)
	m.state.SubnetIDs = make(map[string]string)
	for _, sn := range subnets {
		m.state.SubnetIDs[sn.Name] = fmt.Sprintf("subnet-mock-%s", sn.Name)
	}
	m.state.SecurityGroupIDs = make(map[string]string)
	for _, sg := range sgs {
		m.state.SecurityGroupIDs[sg.Name] = fmt.Sprintf("sg-mock-%s", sg.Name)
	}
	if m.natGateway() {
		m.state.NATGatewayID = fmt.Sprintf("nat-mock-%s", m.name)
	}
	m.state.Status = "active"

	return m.state, nil
}

func (b *mockNetworkBackend) status(m *PlatformNetworking) (*NetworkState, error) {
	return m.state, nil
}

func (b *mockNetworkBackend) destroy(m *PlatformNetworking) error {
	if m.state.Status == "destroyed" {
		return nil
	}
	m.state.Status = "destroying"
	m.state.VPCID = ""
	m.state.SubnetIDs = make(map[string]string)
	m.state.SecurityGroupIDs = make(map[string]string)
	m.state.NATGatewayID = ""
	m.state.Status = "destroyed"
	return nil
}

// ─── AWS stub ─────────────────────────────────────────────────────────────────

// awsNetworkBackend is a stub for AWS VPC provisioning.
// Real implementation would use aws-sdk-go-v2/service/ec2 to:
//   - CreateVpc / DescribeVpcs / DeleteVpc
//   - CreateSubnet / DescribeSubnets / DeleteSubnet
//   - CreateNatGateway / DeleteNatGateway
//   - CreateSecurityGroup / AuthorizeSecurityGroupIngress / DeleteSecurityGroup
type awsNetworkBackend struct{}

func (b *awsNetworkBackend) plan(m *PlatformNetworking) (*NetworkPlan, error) {
	vpc := m.vpcConfig()
	return &NetworkPlan{
		VPC:            vpc,
		Subnets:        m.subnets(),
		NATGateway:     m.natGateway(),
		SecurityGroups: m.securityGroups(),
		Changes:        []string{fmt.Sprintf("create VPC %q (stub — use aws-sdk-go-v2/service/ec2)", vpc.Name)},
	}, nil
}

func (b *awsNetworkBackend) apply(m *PlatformNetworking) (*NetworkState, error) {
	return nil, fmt.Errorf("aws network backend: not implemented — use aws-sdk-go-v2/service/ec2")
}

func (b *awsNetworkBackend) status(m *PlatformNetworking) (*NetworkState, error) {
	m.state.Status = "unknown"
	return m.state, nil
}

func (b *awsNetworkBackend) destroy(m *PlatformNetworking) error {
	return fmt.Errorf("aws network backend: not implemented — use aws-sdk-go-v2/service/ec2")
}
