//go:build aws

package drivers

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockEKSNodeGroupClient struct {
	createFunc   func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error)
	describeFunc func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	updateFunc   func(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error)
	deleteFunc   func(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error)
}

func (m *mockEKSNodeGroupClient) CreateNodegroup(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &eks.CreateNodegroupOutput{
		Nodegroup: &ekstypes.Nodegroup{
			NodegroupName: params.NodegroupName,
			ClusterName:   params.ClusterName,
			Status:        ekstypes.NodegroupStatusCreating,
			ScalingConfig: params.ScalingConfig,
			InstanceTypes: params.InstanceTypes,
			NodegroupArn:  aws.String("arn:aws:eks:us-east-1:123456789:nodegroup/test"),
		},
	}, nil
}

func (m *mockEKSNodeGroupClient) DescribeNodegroup(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, params, optFns...)
	}
	return &eks.DescribeNodegroupOutput{
		Nodegroup: &ekstypes.Nodegroup{
			NodegroupName: params.NodegroupName,
			ClusterName:   params.ClusterName,
			Status:        ekstypes.NodegroupStatusActive,
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: aws.Int32(2),
				MinSize:     aws.Int32(1),
				MaxSize:     aws.Int32(4),
			},
			InstanceTypes: []string{"t3.medium"},
			NodegroupArn:  aws.String("arn:aws:eks:us-east-1:123456789:nodegroup/test"),
		},
	}, nil
}

func (m *mockEKSNodeGroupClient) UpdateNodegroupConfig(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, params, optFns...)
	}
	return &eks.UpdateNodegroupConfigOutput{}, nil
}

func (m *mockEKSNodeGroupClient) DeleteNodegroup(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &eks.DeleteNodegroupOutput{}, nil
}

func TestEKSNodeGroupDriver_ResourceType(t *testing.T) {
	d := NewEKSNodeGroupDriverWithClient(&mockEKSNodeGroupClient{})
	if d.ResourceType() != "aws.eks_nodegroup" {
		t.Errorf("ResourceType() = %q, want aws.eks_nodegroup", d.ResourceType())
	}
}

func TestEKSNodeGroupDriver_Create(t *testing.T) {
	mock := &mockEKSNodeGroupClient{}
	d := NewEKSNodeGroupDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Create(ctx, "test-nodes", map[string]any{
		"cluster_name":  "test-cluster",
		"node_count":    3,
		"instance_type": "m5.large",
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "test-nodes" {
		t.Errorf("Name = %q, want test-nodes", out.Name)
	}
	if out.Status != platform.ResourceStatusCreating {
		t.Errorf("Status = %q, want creating", out.Status)
	}
}

func TestEKSNodeGroupDriver_Read(t *testing.T) {
	mock := &mockEKSNodeGroupClient{}
	d := NewEKSNodeGroupDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Read(ctx, "test-cluster:test-nodes")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want active", out.Status)
	}
	if out.Properties["node_count"] != 2 {
		t.Errorf("node_count = %v, want 2", out.Properties["node_count"])
	}
}

func TestEKSNodeGroupDriver_Scale(t *testing.T) {
	mock := &mockEKSNodeGroupClient{}
	d := NewEKSNodeGroupDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Scale(ctx, "test-cluster:test-nodes", map[string]any{
		"node_count": 5,
	})
	if err != nil {
		t.Fatalf("Scale() error: %v", err)
	}
	if out == nil {
		t.Fatal("Scale() returned nil")
	}
}

func TestEKSNodeGroupDriver_ScaleInvalid(t *testing.T) {
	d := NewEKSNodeGroupDriverWithClient(&mockEKSNodeGroupClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-cluster:test-nodes", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing node_count")
	}
}

func TestEKSNodeGroupDriver_Delete(t *testing.T) {
	d := NewEKSNodeGroupDriverWithClient(&mockEKSNodeGroupClient{})
	ctx := context.Background()

	if err := d.Delete(ctx, "test-cluster:test-nodes"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestSplitNodeGroupName(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		ng      string
	}{
		{"cluster:nodegroup", "cluster", "nodegroup"},
		{"single", "single", "single"},
	}
	for _, tt := range tests {
		c, n := splitNodeGroupName(tt.name)
		if c != tt.cluster || n != tt.ng {
			t.Errorf("splitNodeGroupName(%q) = (%q, %q), want (%q, %q)", tt.name, c, n, tt.cluster, tt.ng)
		}
	}
}
