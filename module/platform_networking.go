package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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

// NetworkingBackendFactory creates a networkBackend for a given provider config.
type NetworkingBackendFactory func(cfg map[string]any) (networkBackend, error)

// networkingBackendRegistry maps provider name to its factory.
var networkingBackendRegistry = map[string]NetworkingBackendFactory{}

// RegisterNetworkingBackend registers a NetworkingBackendFactory for the given provider name.
func RegisterNetworkingBackend(provider string, factory NetworkingBackendFactory) {
	networkingBackendRegistry[provider] = factory
}

func init() {
	RegisterNetworkingBackend("mock", func(_ map[string]any) (networkBackend, error) {
		return &mockNetworkBackend{}, nil
	})
	RegisterNetworkingBackend("aws", func(_ map[string]any) (networkBackend, error) {
		return &awsNetworkBackend{}, nil
	})
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

	factory, ok := networkingBackendRegistry[providerType]
	if !ok {
		return fmt.Errorf("platform.networking %q: unsupported provider %q", m.name, providerType)
	}
	backend, err := factory(m.config)
	if err != nil {
		return fmt.Errorf("platform.networking %q: creating backend: %w", m.name, err)
	}
	m.backend = backend

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

// ─── AWS EC2 backend ──────────────────────────────────────────────────────────

// awsNetworkBackend manages AWS VPC networking using aws-sdk-go-v2/service/ec2.
type awsNetworkBackend struct{}

func (b *awsNetworkBackend) plan(m *PlatformNetworking) (*NetworkPlan, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	vpc := m.vpcConfig()
	if !ok {
		return &NetworkPlan{
			VPC:            vpc,
			Subnets:        m.subnets(),
			NATGateway:     m.natGateway(),
			SecurityGroups: m.securityGroups(),
			Changes:        []string{fmt.Sprintf("create VPC %q (%s)", vpc.Name, vpc.CIDR)},
		}, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("aws network plan: AWS config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)

	// Check if VPC already exists by Name tag
	descOut, err := client.DescribeVpcs(context.Background(), &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:Name"), Values: []string{vpc.Name}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws network plan: DescribeVpcs: %w", err)
	}

	plan := &NetworkPlan{
		VPC:            vpc,
		Subnets:        m.subnets(),
		NATGateway:     m.natGateway(),
		SecurityGroups: m.securityGroups(),
	}

	if len(descOut.Vpcs) > 0 {
		plan.Changes = []string{fmt.Sprintf("noop: VPC %q already exists", vpc.Name)}
	} else {
		plan.Changes = []string{fmt.Sprintf("create VPC %q (%s)", vpc.Name, vpc.CIDR)}
		for _, sn := range m.subnets() {
			plan.Changes = append(plan.Changes, fmt.Sprintf("create subnet %q (%s)", sn.Name, sn.CIDR))
		}
		for _, sg := range m.securityGroups() {
			plan.Changes = append(plan.Changes, fmt.Sprintf("create security group %q", sg.Name))
		}
		if m.natGateway() {
			plan.Changes = append(plan.Changes, "create NAT gateway")
		}
	}
	return plan, nil
}

func (b *awsNetworkBackend) apply(m *PlatformNetworking) (*NetworkState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return nil, fmt.Errorf("aws network apply: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("aws network apply: AWS config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)
	vpc := m.vpcConfig()

	// Create VPC
	vpcOut, err := client.CreateVpc(context.Background(), &ec2.CreateVpcInput{
		CidrBlock: aws.String(vpc.CIDR),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeVpc,
				Tags:         []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(vpc.Name)}},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws network apply: CreateVpc: %w", err)
	}

	vpcID := ""
	if vpcOut.Vpc != nil && vpcOut.Vpc.VpcId != nil {
		vpcID = *vpcOut.Vpc.VpcId
	}
	m.state.VPCID = vpcID

	// Create Internet Gateway and attach
	igwOut, err := client.CreateInternetGateway(context.Background(), &ec2.CreateInternetGatewayInput{})
	if err != nil {
		return nil, fmt.Errorf("aws network apply: CreateInternetGateway: %w", err)
	}
	if igwOut.InternetGateway != nil && igwOut.InternetGateway.InternetGatewayId != nil {
		_, _ = client.AttachInternetGateway(context.Background(), &ec2.AttachInternetGatewayInput{
			InternetGatewayId: igwOut.InternetGateway.InternetGatewayId,
			VpcId:             aws.String(vpcID),
		})
	}

	// Create subnets
	m.state.SubnetIDs = make(map[string]string)
	var firstPublicSubnetID string
	for _, sn := range m.subnets() {
		snOut, err := client.CreateSubnet(context.Background(), &ec2.CreateSubnetInput{
			VpcId:            aws.String(vpcID),
			CidrBlock:        aws.String(sn.CIDR),
			AvailabilityZone: optString(sn.AZ),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeSubnet,
					Tags:         []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(sn.Name)}},
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("aws network apply: CreateSubnet %q: %w", sn.Name, err)
		}
		if snOut.Subnet != nil && snOut.Subnet.SubnetId != nil {
			m.state.SubnetIDs[sn.Name] = *snOut.Subnet.SubnetId
			if sn.Public && firstPublicSubnetID == "" {
				firstPublicSubnetID = *snOut.Subnet.SubnetId
			}
		}
	}

	// Create NAT gateway if requested
	if m.natGateway() && firstPublicSubnetID != "" {
		// Allocate EIP for NAT gateway
		eipOut, err := client.AllocateAddress(context.Background(), &ec2.AllocateAddressInput{
			Domain: ec2types.DomainTypeVpc,
		})
		if err == nil && eipOut.AllocationId != nil {
			natOut, err := client.CreateNatGateway(context.Background(), &ec2.CreateNatGatewayInput{
				SubnetId:     aws.String(firstPublicSubnetID),
				AllocationId: eipOut.AllocationId,
			})
			if err != nil {
				return nil, fmt.Errorf("aws network apply: CreateNatGateway: %w", err)
			}
			if natOut.NatGateway != nil && natOut.NatGateway.NatGatewayId != nil {
				m.state.NATGatewayID = *natOut.NatGateway.NatGatewayId
			}
		}
	}

	// Create security groups
	m.state.SecurityGroupIDs = make(map[string]string)
	for _, sg := range m.securityGroups() {
		sgOut, err := client.CreateSecurityGroup(context.Background(), &ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(sg.Name),
			Description: aws.String(fmt.Sprintf("Security group: %s", sg.Name)),
			VpcId:       aws.String(vpcID),
		})
		if err != nil {
			return nil, fmt.Errorf("aws network apply: CreateSecurityGroup %q: %w", sg.Name, err)
		}
		if sgOut.GroupId != nil {
			m.state.SecurityGroupIDs[sg.Name] = *sgOut.GroupId

			// Authorize ingress rules
			var ipPerms []ec2types.IpPermission
			for _, rule := range sg.Rules {
				rulePort := safeIntToInt32(rule.Port)
				ipPerms = append(ipPerms, ec2types.IpPermission{
					IpProtocol: aws.String(rule.Protocol),
					FromPort:   aws.Int32(rulePort),
					ToPort:     aws.Int32(rulePort),
					IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(rule.Source)}},
				})
			}
			if len(ipPerms) > 0 {
				if _, err := client.AuthorizeSecurityGroupIngress(context.Background(), &ec2.AuthorizeSecurityGroupIngressInput{
					GroupId:       sgOut.GroupId,
					IpPermissions: ipPerms,
				}); err != nil {
					return nil, fmt.Errorf("aws network apply: AuthorizeSecurityGroupIngress %q: %w", sg.Name, err)
				}
			}
		}
	}

	m.state.Status = "active"
	return m.state, nil
}

func (b *awsNetworkBackend) status(m *PlatformNetworking) (*NetworkState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return m.state, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return m.state, fmt.Errorf("aws network status: AWS config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)

	if m.state.VPCID == "" {
		vpc := m.vpcConfig()
		descOut, err := client.DescribeVpcs(context.Background(), &ec2.DescribeVpcsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("tag:Name"), Values: []string{vpc.Name}},
			},
		})
		if err == nil && len(descOut.Vpcs) > 0 && descOut.Vpcs[0].VpcId != nil {
			m.state.VPCID = *descOut.Vpcs[0].VpcId
			m.state.Status = "active"
		} else {
			m.state.Status = "not-found"
		}
		return m.state, nil
	}

	descOut, err := client.DescribeVpcs(context.Background(), &ec2.DescribeVpcsInput{
		VpcIds: []string{m.state.VPCID},
	})
	if err != nil {
		return m.state, fmt.Errorf("aws network status: DescribeVpcs: %w", err)
	}
	if len(descOut.Vpcs) > 0 {
		m.state.Status = "active"
	} else {
		m.state.Status = "not-found"
	}
	return m.state, nil
}

func (b *awsNetworkBackend) destroy(m *PlatformNetworking) error {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return fmt.Errorf("aws network destroy: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return fmt.Errorf("aws network destroy: AWS config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)

	var destroyErrs []string

	// Delete security groups
	for name, sgID := range m.state.SecurityGroupIDs {
		if _, err := client.DeleteSecurityGroup(context.Background(), &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(sgID),
		}); err != nil {
			destroyErrs = append(destroyErrs, fmt.Sprintf("DeleteSecurityGroup %q: %v", name, err))
		}
	}

	// Delete subnets
	for name, snID := range m.state.SubnetIDs {
		if _, err := client.DeleteSubnet(context.Background(), &ec2.DeleteSubnetInput{
			SubnetId: aws.String(snID),
		}); err != nil {
			destroyErrs = append(destroyErrs, fmt.Sprintf("DeleteSubnet %q: %v", name, err))
		}
	}

	// Delete NAT gateway
	if m.state.NATGatewayID != "" {
		if _, err := client.DeleteNatGateway(context.Background(), &ec2.DeleteNatGatewayInput{
			NatGatewayId: aws.String(m.state.NATGatewayID),
		}); err != nil {
			destroyErrs = append(destroyErrs, fmt.Sprintf("DeleteNatGateway: %v", err))
		}
	}

	// Delete VPC
	if m.state.VPCID != "" {
		if _, err := client.DeleteVpc(context.Background(), &ec2.DeleteVpcInput{
			VpcId: aws.String(m.state.VPCID),
		}); err != nil {
			destroyErrs = append(destroyErrs, fmt.Sprintf("DeleteVpc: %v", err))
		}
	}

	if len(destroyErrs) > 0 {
		return fmt.Errorf("aws network destroy: %s", strings.Join(destroyErrs, "; "))
	}

	m.state.Status = "destroyed"
	m.state.VPCID = ""
	m.state.SubnetIDs = make(map[string]string)
	m.state.SecurityGroupIDs = make(map[string]string)
	m.state.NATGatewayID = ""
	return nil
}
