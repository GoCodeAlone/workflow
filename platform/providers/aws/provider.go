//go:build aws

package aws

import (
	"context"
	"fmt"
	"sync"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/GoCodeAlone/workflow/platform"
)

const (
	ProviderName    = "aws"
	ProviderVersion = "0.1.0"
)

// AWSProvider implements platform.Provider for Amazon Web Services.
type AWSProvider struct {
	mu               sync.RWMutex
	initialized      bool
	region           string
	capabilityMapper *AWSCapabilityMapper
	credBroker       *AWSCredentialBroker
	stateStore       *AWSS3StateStore
	drivers          map[string]platform.ResourceDriver
}

// NewProvider creates a new AWS provider instance.
func NewProvider() platform.Provider {
	return &AWSProvider{
		drivers: make(map[string]platform.ResourceDriver),
	}
}

func (p *AWSProvider) Name() string    { return ProviderName }
func (p *AWSProvider) Version() string { return ProviderVersion }

// Initialize configures the AWS SDK client from the provided config map.
// Expected config keys: "region", "access_key_id", "secret_access_key", "role_arn",
// "state_bucket", "state_table".
func (p *AWSProvider) Initialize(ctx context.Context, config map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	region, _ := config["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	p.region = region

	opts := []func(*awscfg.LoadOptions) error{
		awscfg.WithRegion(region),
	}

	accessKey, _ := config["access_key_id"].(string)
	secretKey, _ := config["secret_access_key"].(string)
	if accessKey != "" && secretKey != "" {
		opts = append(opts, awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("aws: load config: %w", err)
	}

	// Initialize capability mapper
	p.capabilityMapper = NewAWSCapabilityMapper()

	// Initialize credential broker
	roleARN, _ := config["role_arn"].(string)
	p.credBroker = NewAWSCredentialBroker(cfg, roleARN)

	// Initialize state store
	bucket, _ := config["state_bucket"].(string)
	table, _ := config["state_table"].(string)
	p.stateStore = NewAWSS3StateStore(cfg, bucket, table)

	// Register resource drivers
	p.registerDrivers(cfg)

	p.initialized = true
	return nil
}

func (p *AWSProvider) registerDrivers(cfg awsSDKConfig) {
	driverList := []platform.ResourceDriver{
		NewEKSClusterDriver(cfg),
		NewEKSNodeGroupDriver(cfg),
		NewVPCDriver(cfg),
		NewRDSDriver(cfg),
		NewSQSDriver(cfg),
		NewIAMDriver(cfg),
		NewALBDriver(cfg),
	}
	for _, d := range driverList {
		p.drivers[d.ResourceType()] = d
	}
}

func (p *AWSProvider) Capabilities() []platform.CapabilityType {
	return []platform.CapabilityType{
		{
			Name:        "kubernetes_cluster",
			Description: "EKS Kubernetes cluster with managed node groups",
			Tier:        platform.TierInfrastructure,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "version", Type: "string", Description: "Kubernetes version"},
				{Name: "node_count", Type: "int", Required: true, Description: "Number of worker nodes"},
				{Name: "instance_type", Type: "string", Description: "EC2 instance type for nodes", DefaultValue: "t3.medium"},
			},
		},
		{
			Name:        "network",
			Description: "VPC with subnets and routing",
			Tier:        platform.TierInfrastructure,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "cidr", Type: "string", Required: true, Description: "VPC CIDR block"},
				{Name: "availability_zones", Type: "list", Description: "AZs for subnets"},
				{Name: "enable_nat", Type: "bool", Description: "Enable NAT gateway", DefaultValue: true},
			},
		},
		{
			Name:        "database",
			Description: "RDS managed database instance",
			Tier:        platform.TierSharedPrimitive,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "engine", Type: "string", Required: true, Description: "Database engine (postgres, mysql)"},
				{Name: "engine_version", Type: "string", Description: "Engine version"},
				{Name: "instance_class", Type: "string", Description: "RDS instance class", DefaultValue: "db.t3.micro"},
				{Name: "allocated_storage", Type: "int", Description: "Storage in GB", DefaultValue: 20},
				{Name: "multi_az", Type: "bool", Description: "Multi-AZ deployment", DefaultValue: false},
			},
		},
		{
			Name:        "message_queue",
			Description: "SQS message queue",
			Tier:        platform.TierSharedPrimitive,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "fifo", Type: "bool", Description: "FIFO queue", DefaultValue: false},
				{Name: "visibility_timeout", Type: "int", Description: "Visibility timeout in seconds", DefaultValue: 30},
				{Name: "retention_period", Type: "int", Description: "Message retention in seconds", DefaultValue: 345600},
			},
		},
		{
			Name:        "container_runtime",
			Description: "EKS-based container deployment",
			Tier:        platform.TierApplication,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "image", Type: "string", Required: true, Description: "Container image"},
				{Name: "replicas", Type: "int", Description: "Number of replicas", DefaultValue: 1},
				{Name: "memory", Type: "string", Description: "Memory limit"},
				{Name: "cpu", Type: "string", Description: "CPU limit"},
			},
		},
		{
			Name:        "load_balancer",
			Description: "Application Load Balancer",
			Tier:        platform.TierInfrastructure,
			Fidelity:    platform.FidelityFull,
			Properties: []platform.PropertySchema{
				{Name: "scheme", Type: "string", Description: "internal or internet-facing", DefaultValue: "internet-facing"},
				{Name: "listeners", Type: "list", Description: "Listener configurations"},
			},
		},
	}
}

func (p *AWSProvider) MapCapability(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.initialized {
		return nil, platform.ErrProviderNotInitialized
	}
	if !p.capabilityMapper.CanMap(decl.Type) {
		return nil, &platform.CapabilityUnsupportedError{Capability: decl.Type, Provider: ProviderName}
	}
	return p.capabilityMapper.Map(decl, pctx)
}

func (p *AWSProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	d, ok := p.drivers[resourceType]
	if !ok {
		return nil, &platform.ResourceDriverNotFoundError{ResourceType: resourceType, Provider: ProviderName}
	}
	return d, nil
}

func (p *AWSProvider) CredentialBroker() platform.CredentialBroker {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.credBroker
}

func (p *AWSProvider) StateStore() platform.StateStore {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stateStore
}

func (p *AWSProvider) Healthy(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.initialized {
		return platform.ErrProviderNotInitialized
	}
	// STS GetCallerIdentity would be ideal, but for now just check init state
	return nil
}

func (p *AWSProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initialized = false
	return nil
}
