//go:build aws

package drivers

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/GoCodeAlone/workflow/platform"
)

// EC2VPCClient defines the EC2 operations for VPC management.
type EC2VPCClient interface {
	CreateVpc(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error)
	DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DeleteVpc(ctx context.Context, params *ec2.DeleteVpcInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error)
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
}

// VPCDriver manages AWS VPC resources.
type VPCDriver struct {
	client EC2VPCClient
}

// NewVPCDriver creates a new VPC driver.
func NewVPCDriver(cfg awsv2.Config) *VPCDriver {
	return &VPCDriver{
		client: ec2.NewFromConfig(cfg),
	}
}

// NewVPCDriverWithClient creates a VPC driver with a custom client.
func NewVPCDriverWithClient(client EC2VPCClient) *VPCDriver {
	return &VPCDriver{client: client}
}

func (d *VPCDriver) ResourceType() string { return "aws.vpc" }

func (d *VPCDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	cidr, _ := properties["cidr"].(string)
	if cidr == "" {
		cidr = "10.0.0.0/16"
	}

	out, err := d.client.CreateVpc(ctx, &ec2.CreateVpcInput{
		CidrBlock: awsv2.String(cidr),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeVpc,
				Tags: []ec2types.Tag{
					{Key: awsv2.String("Name"), Value: awsv2.String(name)},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("vpc: create %q: %w", name, err)
	}

	return vpcToOutput(name, out.Vpc), nil
}

func (d *VPCDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	out, err := d.client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   awsv2.String("tag:Name"),
				Values: []string{name},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("vpc: describe %q: %w", name, err)
	}
	if len(out.Vpcs) == 0 {
		return nil, &platform.ResourceNotFoundError{Name: name, Provider: "aws"}
	}
	return vpcToOutput(name, &out.Vpcs[0]), nil
}

func (d *VPCDriver) Update(ctx context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	// VPC CIDR cannot be changed after creation. Tags can be updated.
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}

	vpcID, _ := current.Properties["vpc_id"].(string)
	if vpcID != "" {
		_, err = d.client.CreateTags(ctx, &ec2.CreateTagsInput{
			Resources: []string{vpcID},
			Tags: []ec2types.Tag{
				{Key: awsv2.String("Name"), Value: awsv2.String(name)},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("vpc: update tags %q: %w", name, err)
		}
	}

	return d.Read(ctx, name)
}

func (d *VPCDriver) Delete(ctx context.Context, name string) error {
	current, err := d.Read(ctx, name)
	if err != nil {
		return err
	}

	vpcID, _ := current.Properties["vpc_id"].(string)
	if vpcID == "" {
		return fmt.Errorf("vpc: cannot delete %q: no vpc_id in state", name)
	}

	_, err = d.client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: awsv2.String(vpcID),
	})
	if err != nil {
		return fmt.Errorf("vpc: delete %q: %w", name, err)
	}
	return nil
}

func (d *VPCDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	out, err := d.Read(ctx, name)
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}
	status := "healthy"
	if out.Status != platform.ResourceStatusActive {
		status = "degraded"
	}
	return &platform.HealthStatus{
		Status:    status,
		Message:   string(out.Status),
		CheckedAt: time.Now(),
	}, nil
}

func (d *VPCDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "aws.vpc"}
}

func (d *VPCDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func vpcToOutput(name string, vpc *ec2types.Vpc) *platform.ResourceOutput {
	if vpc == nil {
		return nil
	}
	status := platform.ResourceStatusActive
	if vpc.State == ec2types.VpcStatePending {
		status = platform.ResourceStatusCreating
	}

	props := map[string]any{
		"state": string(vpc.State),
	}
	if vpc.VpcId != nil {
		props["vpc_id"] = *vpc.VpcId
	}
	if vpc.CidrBlock != nil {
		props["cidr"] = *vpc.CidrBlock
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "network",
		ProviderType: "aws.vpc",
		Properties:   props,
		Status:       status,
		LastSynced:   time.Now(),
	}
}

var _ platform.ResourceDriver = (*VPCDriver)(nil)
