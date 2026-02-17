//go:build aws

package drivers

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/GoCodeAlone/workflow/platform"
)

// EKSClusterClient defines the EKS operations for cluster management.
type EKSClusterClient interface {
	CreateCluster(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error)
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	UpdateClusterVersion(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error)
	DeleteCluster(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error)
}

// EKSClusterDriver manages EKS cluster resources.
type EKSClusterDriver struct {
	client EKSClusterClient
}

// NewEKSClusterDriver creates a new EKS cluster driver.
func NewEKSClusterDriver(cfg aws.Config) *EKSClusterDriver {
	return &EKSClusterDriver{
		client: eks.NewFromConfig(cfg),
	}
}

// NewEKSClusterDriverWithClient creates an EKS cluster driver with a custom client (for testing).
func NewEKSClusterDriverWithClient(client EKSClusterClient) *EKSClusterDriver {
	return &EKSClusterDriver{client: client}
}

func (d *EKSClusterDriver) ResourceType() string { return "aws.eks_cluster" }

func (d *EKSClusterDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	version, _ := properties["version"].(string)
	if version == "" {
		version = "1.29"
	}

	roleARN, _ := properties["role_arn"].(string)
	subnetIDs := stringSliceProp(properties, "subnet_ids")
	securityGroupIDs := stringSliceProp(properties, "security_group_ids")

	input := &eks.CreateClusterInput{
		Name:    aws.String(name),
		Version: aws.String(version),
		ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
			SubnetIds:        subnetIDs,
			SecurityGroupIds: securityGroupIDs,
		},
	}
	if roleARN != "" {
		input.RoleArn = aws.String(roleARN)
	}

	out, err := d.client.CreateCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("eks: create cluster %q: %w", name, err)
	}

	return clusterToOutput(out.Cluster), nil
}

func (d *EKSClusterDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	out, err := d.client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("eks: describe cluster %q: %w", name, err)
	}
	return clusterToOutput(out.Cluster), nil
}

func (d *EKSClusterDriver) Update(ctx context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	version, _ := desired["version"].(string)
	if version != "" {
		_, err := d.client.UpdateClusterVersion(ctx, &eks.UpdateClusterVersionInput{
			Name:    aws.String(name),
			Version: aws.String(version),
		})
		if err != nil {
			return nil, fmt.Errorf("eks: update cluster version %q: %w", name, err)
		}
	}
	return d.Read(ctx, name)
}

func (d *EKSClusterDriver) Delete(ctx context.Context, name string) error {
	_, err := d.client.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("eks: delete cluster %q: %w", name, err)
	}
	return nil
}

func (d *EKSClusterDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	out, err := d.client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}

	status := "healthy"
	if out.Cluster.Status != ekstypes.ClusterStatusActive {
		status = "degraded"
	}
	return &platform.HealthStatus{
		Status:    status,
		Message:   string(out.Cluster.Status),
		CheckedAt: time.Now(),
	}, nil
}

func (d *EKSClusterDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "aws.eks_cluster"}
}

func (d *EKSClusterDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func clusterToOutput(cluster *ekstypes.Cluster) *platform.ResourceOutput {
	if cluster == nil {
		return nil
	}
	status := platform.ResourceStatusActive
	switch cluster.Status {
	case ekstypes.ClusterStatusCreating:
		status = platform.ResourceStatusCreating
	case ekstypes.ClusterStatusDeleting:
		status = platform.ResourceStatusDeleting
	case ekstypes.ClusterStatusFailed:
		status = platform.ResourceStatusFailed
	case ekstypes.ClusterStatusUpdating:
		status = platform.ResourceStatusUpdating
	}

	endpoint := ""
	if cluster.Endpoint != nil {
		endpoint = *cluster.Endpoint
	}

	props := map[string]any{
		"status": string(cluster.Status),
	}
	if cluster.Version != nil {
		props["version"] = *cluster.Version
	}
	if cluster.Arn != nil {
		props["arn"] = *cluster.Arn
	}

	name := ""
	if cluster.Name != nil {
		name = *cluster.Name
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "kubernetes_cluster",
		ProviderType: "aws.eks_cluster",
		Endpoint:     endpoint,
		Properties:   props,
		Status:       status,
		LastSynced:   time.Now(),
	}
}

var _ platform.ResourceDriver = (*EKSClusterDriver)(nil)
