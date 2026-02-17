//go:build aws

package drivers

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockEC2VPCClient struct {
	createFunc   func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error)
	describeFunc func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	deleteFunc   func(ctx context.Context, params *ec2.DeleteVpcInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error)
	tagFunc      func(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
}

func (m *mockEC2VPCClient) CreateVpc(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &ec2.CreateVpcOutput{
		Vpc: &ec2types.Vpc{
			VpcId:     awsv2.String("vpc-12345"),
			CidrBlock: params.CidrBlock,
			State:     ec2types.VpcStateAvailable,
		},
	}, nil
}

func (m *mockEC2VPCClient) DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, params, optFns...)
	}
	return &ec2.DescribeVpcsOutput{
		Vpcs: []ec2types.Vpc{
			{
				VpcId:     awsv2.String("vpc-12345"),
				CidrBlock: awsv2.String("10.0.0.0/16"),
				State:     ec2types.VpcStateAvailable,
			},
		},
	}, nil
}

func (m *mockEC2VPCClient) DeleteVpc(ctx context.Context, params *ec2.DeleteVpcInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &ec2.DeleteVpcOutput{}, nil
}

func (m *mockEC2VPCClient) CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	if m.tagFunc != nil {
		return m.tagFunc(ctx, params, optFns...)
	}
	return &ec2.CreateTagsOutput{}, nil
}

func TestVPCDriver_ResourceType(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	if d.ResourceType() != "aws.vpc" {
		t.Errorf("ResourceType() = %q, want aws.vpc", d.ResourceType())
	}
}

func TestVPCDriver_Create(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	out, err := d.Create(ctx, "test-vpc", map[string]any{
		"cidr": "10.0.0.0/16",
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "test-vpc" {
		t.Errorf("Name = %q, want test-vpc", out.Name)
	}
	if out.ProviderType != "aws.vpc" {
		t.Errorf("ProviderType = %q, want aws.vpc", out.ProviderType)
	}
	if out.Properties["vpc_id"] != "vpc-12345" {
		t.Errorf("vpc_id = %v, want vpc-12345", out.Properties["vpc_id"])
	}
}

func TestVPCDriver_Read(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	out, err := d.Read(ctx, "test-vpc")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Properties["cidr"] != "10.0.0.0/16" {
		t.Errorf("cidr = %v, want 10.0.0.0/16", out.Properties["cidr"])
	}
}

func TestVPCDriver_ReadNotFound(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{
		describeFunc: func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
			return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
		},
	})
	ctx := context.Background()

	_, err := d.Read(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent VPC")
	}
	if _, ok := err.(*platform.ResourceNotFoundError); !ok {
		t.Errorf("expected ResourceNotFoundError, got %T", err)
	}
}

func TestVPCDriver_Delete(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	if err := d.Delete(ctx, "test-vpc"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestVPCDriver_Scale(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-vpc", nil)
	if err == nil {
		t.Fatal("expected NotScalableError")
	}
}

func TestVPCDriver_HealthCheck(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "test-vpc")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", health.Status)
	}
}

func TestVPCDriver_Diff(t *testing.T) {
	d := NewVPCDriverWithClient(&mockEC2VPCClient{})
	ctx := context.Background()

	diffs, err := d.Diff(ctx, "test-vpc", map[string]any{
		"cidr": "10.1.0.0/16",
	})
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected diffs for different CIDR")
	}
}
