//go:build aws

package drivers

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockELBv2Client struct {
	createFunc  func(ctx context.Context, params *elbv2.CreateLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.CreateLoadBalancerOutput, error)
	describeFunc func(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	deleteFunc  func(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error)
	modifyFunc  func(ctx context.Context, params *elbv2.ModifyLoadBalancerAttributesInput, optFns ...func(*elbv2.Options)) (*elbv2.ModifyLoadBalancerAttributesOutput, error)
}

func (m *mockELBv2Client) CreateLoadBalancer(ctx context.Context, params *elbv2.CreateLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.CreateLoadBalancerOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &elbv2.CreateLoadBalancerOutput{
		LoadBalancers: []elbtypes.LoadBalancer{
			{
				LoadBalancerName: params.Name,
				LoadBalancerArn:  awsv2.String("arn:aws:elasticloadbalancing:us-east-1:123456789:loadbalancer/app/test/123"),
				DNSName:          awsv2.String("test-123.us-east-1.elb.amazonaws.com"),
				Scheme:           params.Scheme,
				Type:             elbtypes.LoadBalancerTypeEnumApplication,
				State: &elbtypes.LoadBalancerState{
					Code: elbtypes.LoadBalancerStateEnumActive,
				},
			},
		},
	}, nil
}

func (m *mockELBv2Client) DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, params, optFns...)
	}
	name := "test-alb"
	if len(params.Names) > 0 {
		name = params.Names[0]
	}
	return &elbv2.DescribeLoadBalancersOutput{
		LoadBalancers: []elbtypes.LoadBalancer{
			{
				LoadBalancerName: awsv2.String(name),
				LoadBalancerArn:  awsv2.String("arn:aws:elasticloadbalancing:us-east-1:123456789:loadbalancer/app/test/123"),
				DNSName:          awsv2.String("test-123.us-east-1.elb.amazonaws.com"),
				Scheme:           elbtypes.LoadBalancerSchemeEnumInternetFacing,
				Type:             elbtypes.LoadBalancerTypeEnumApplication,
				VpcId:            awsv2.String("vpc-12345"),
				State: &elbtypes.LoadBalancerState{
					Code: elbtypes.LoadBalancerStateEnumActive,
				},
			},
		},
	}, nil
}

func (m *mockELBv2Client) DeleteLoadBalancer(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &elbv2.DeleteLoadBalancerOutput{}, nil
}

func (m *mockELBv2Client) ModifyLoadBalancerAttributes(ctx context.Context, params *elbv2.ModifyLoadBalancerAttributesInput, optFns ...func(*elbv2.Options)) (*elbv2.ModifyLoadBalancerAttributesOutput, error) {
	if m.modifyFunc != nil {
		return m.modifyFunc(ctx, params, optFns...)
	}
	return &elbv2.ModifyLoadBalancerAttributesOutput{}, nil
}

func TestALBDriver_ResourceType(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	if d.ResourceType() != "aws.alb" {
		t.Errorf("ResourceType() = %q, want aws.alb", d.ResourceType())
	}
}

func TestALBDriver_Create(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	out, err := d.Create(ctx, "test-alb", map[string]any{
		"scheme": "internet-facing",
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.ProviderType != "aws.alb" {
		t.Errorf("ProviderType = %q, want aws.alb", out.ProviderType)
	}
	if out.Endpoint != "test-123.us-east-1.elb.amazonaws.com" {
		t.Errorf("Endpoint = %q", out.Endpoint)
	}
	if out.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want active", out.Status)
	}
}

func TestALBDriver_CreateInternal(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	out, err := d.Create(ctx, "internal-alb", map[string]any{
		"scheme": "internal",
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out == nil {
		t.Fatal("Create() returned nil")
	}
}

func TestALBDriver_Read(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	out, err := d.Read(ctx, "test-alb")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Properties["vpc_id"] != "vpc-12345" {
		t.Errorf("vpc_id = %v, want vpc-12345", out.Properties["vpc_id"])
	}
}

func TestALBDriver_ReadNotFound(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{
		describeFunc: func(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
			return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: []elbtypes.LoadBalancer{}}, nil
		},
	})
	ctx := context.Background()

	_, err := d.Read(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ALB")
	}
}

func TestALBDriver_Delete(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	if err := d.Delete(ctx, "test-alb"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestALBDriver_Scale(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-alb", nil)
	if err == nil {
		t.Fatal("expected NotScalableError")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestALBDriver_HealthCheck(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "test-alb")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", health.Status)
	}
}

func TestALBDriver_Diff(t *testing.T) {
	d := NewALBDriverWithClient(&mockELBv2Client{})
	ctx := context.Background()

	diffs, err := d.Diff(ctx, "test-alb", map[string]any{
		"scheme": "internal",
	})
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected diffs for scheme change")
	}
}
