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

type mockEKSClusterClient struct {
	createFunc  func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error)
	describeFunc func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	updateFunc  func(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error)
	deleteFunc  func(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error)
}

func (m *mockEKSClusterClient) CreateCluster(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &eks.CreateClusterOutput{
		Cluster: &ekstypes.Cluster{
			Name:     params.Name,
			Version:  params.Version,
			Status:   ekstypes.ClusterStatusCreating,
			Endpoint: aws.String("https://eks.example.com"),
			Arn:      aws.String("arn:aws:eks:us-east-1:123456789:cluster/test"),
		},
	}, nil
}

func (m *mockEKSClusterClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, params, optFns...)
	}
	return &eks.DescribeClusterOutput{
		Cluster: &ekstypes.Cluster{
			Name:     params.Name,
			Version:  aws.String("1.29"),
			Status:   ekstypes.ClusterStatusActive,
			Endpoint: aws.String("https://eks.example.com"),
			Arn:      aws.String("arn:aws:eks:us-east-1:123456789:cluster/test"),
		},
	}, nil
}

func (m *mockEKSClusterClient) UpdateClusterVersion(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, params, optFns...)
	}
	return &eks.UpdateClusterVersionOutput{}, nil
}

func (m *mockEKSClusterClient) DeleteCluster(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &eks.DeleteClusterOutput{}, nil
}

func TestEKSClusterDriver_ResourceType(t *testing.T) {
	d := NewEKSClusterDriverWithClient(&mockEKSClusterClient{})
	if d.ResourceType() != "aws.eks_cluster" {
		t.Errorf("ResourceType() = %q, want %q", d.ResourceType(), "aws.eks_cluster")
	}
}

func TestEKSClusterDriver_Create(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Create(ctx, "test-cluster", map[string]any{
		"version": "1.28",
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "test-cluster" {
		t.Errorf("Name = %q, want test-cluster", out.Name)
	}
	if out.ProviderType != "aws.eks_cluster" {
		t.Errorf("ProviderType = %q, want aws.eks_cluster", out.ProviderType)
	}
	if out.Endpoint != "https://eks.example.com" {
		t.Errorf("Endpoint = %q, want https://eks.example.com", out.Endpoint)
	}
	if out.Status != platform.ResourceStatusCreating {
		t.Errorf("Status = %q, want creating", out.Status)
	}
}

func TestEKSClusterDriver_Read(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Read(ctx, "test-cluster")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want active", out.Status)
	}
	if out.Properties["version"] != "1.29" {
		t.Errorf("version = %v, want 1.29", out.Properties["version"])
	}
}

func TestEKSClusterDriver_Update(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	out, err := d.Update(ctx, "test-cluster", nil, map[string]any{
		"version": "1.29",
	})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if out == nil {
		t.Fatal("Update() returned nil")
	}
}

func TestEKSClusterDriver_Delete(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	if err := d.Delete(ctx, "test-cluster"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestEKSClusterDriver_HealthCheck(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "test-cluster")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health status = %q, want healthy", health.Status)
	}
}

func TestEKSClusterDriver_Scale(t *testing.T) {
	d := NewEKSClusterDriverWithClient(&mockEKSClusterClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-cluster", nil)
	if err == nil {
		t.Fatal("expected NotScalableError")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestEKSClusterDriver_Diff(t *testing.T) {
	mock := &mockEKSClusterClient{}
	d := NewEKSClusterDriverWithClient(mock)
	ctx := context.Background()

	diffs, err := d.Diff(ctx, "test-cluster", map[string]any{
		"version": "1.30",
	})
	if err != nil {
		t.Fatalf("Diff() error: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected diffs for version change")
	}
}
