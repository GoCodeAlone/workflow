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

// EKSNodeGroupClient defines the EKS operations for node group management.
type EKSNodeGroupClient interface {
	CreateNodegroup(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error)
	DescribeNodegroup(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	UpdateNodegroupConfig(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error)
	DeleteNodegroup(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error)
}

// EKSNodeGroupDriver manages EKS node group resources.
type EKSNodeGroupDriver struct {
	client EKSNodeGroupClient
}

// NewEKSNodeGroupDriver creates a new EKS node group driver.
func NewEKSNodeGroupDriver(cfg aws.Config) *EKSNodeGroupDriver {
	return &EKSNodeGroupDriver{
		client: eks.NewFromConfig(cfg),
	}
}

// NewEKSNodeGroupDriverWithClient creates an EKS node group driver with a custom client.
func NewEKSNodeGroupDriverWithClient(client EKSNodeGroupClient) *EKSNodeGroupDriver {
	return &EKSNodeGroupDriver{client: client}
}

func (d *EKSNodeGroupDriver) ResourceType() string { return "aws.eks_nodegroup" }

func (d *EKSNodeGroupDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	clusterName, _ := properties["cluster_name"].(string)
	nodeCount := intPropDrivers(properties, "node_count", 2)
	instanceType, _ := properties["instance_type"].(string)
	if instanceType == "" {
		instanceType = "t3.medium"
	}
	nodeRole, _ := properties["node_role_arn"].(string)
	subnetIDs := stringSliceProp(properties, "subnet_ids")

	input := &eks.CreateNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(name),
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			DesiredSize: aws.Int32(int32(nodeCount)),
			MinSize:     aws.Int32(1),
			MaxSize:     aws.Int32(int32(nodeCount * 2)),
		},
		InstanceTypes: []string{instanceType},
		Subnets:       subnetIDs,
	}
	if nodeRole != "" {
		input.NodeRole = aws.String(nodeRole)
	}

	out, err := d.client.CreateNodegroup(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("eks: create nodegroup %q: %w", name, err)
	}
	return nodeGroupToOutput(out.Nodegroup), nil
}

func (d *EKSNodeGroupDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	// Name format: we need the cluster name. Try parsing from properties or use name.
	// In practice the cluster name would be stored in state. For the driver we receive
	// only the name, so we store cluster_name:nodegroup_name format.
	clusterName, ngName := splitNodeGroupName(name)

	out, err := d.client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(ngName),
	})
	if err != nil {
		return nil, fmt.Errorf("eks: describe nodegroup %q: %w", name, err)
	}
	return nodeGroupToOutput(out.Nodegroup), nil
}

func (d *EKSNodeGroupDriver) Update(ctx context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	clusterName, ngName := splitNodeGroupName(name)
	nodeCount := intPropDrivers(desired, "node_count", 0)

	if nodeCount > 0 {
		_, err := d.client.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
			ClusterName:   aws.String(clusterName),
			NodegroupName: aws.String(ngName),
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: aws.Int32(int32(nodeCount)),
				MinSize:     aws.Int32(1),
				MaxSize:     aws.Int32(int32(nodeCount * 2)),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("eks: update nodegroup %q: %w", name, err)
		}
	}
	return d.Read(ctx, name)
}

func (d *EKSNodeGroupDriver) Delete(ctx context.Context, name string) error {
	clusterName, ngName := splitNodeGroupName(name)
	_, err := d.client.DeleteNodegroup(ctx, &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(ngName),
	})
	if err != nil {
		return fmt.Errorf("eks: delete nodegroup %q: %w", name, err)
	}
	return nil
}

func (d *EKSNodeGroupDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	clusterName, ngName := splitNodeGroupName(name)
	out, err := d.client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(ngName),
	})
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}

	status := "healthy"
	if out.Nodegroup.Status != ekstypes.NodegroupStatusActive {
		status = "degraded"
	}
	return &platform.HealthStatus{
		Status:    status,
		Message:   string(out.Nodegroup.Status),
		CheckedAt: time.Now(),
	}, nil
}

func (d *EKSNodeGroupDriver) Scale(ctx context.Context, name string, scaleParams map[string]any) (*platform.ResourceOutput, error) {
	clusterName, ngName := splitNodeGroupName(name)
	nodeCount := intPropDrivers(scaleParams, "node_count", 0)
	if nodeCount <= 0 {
		return nil, fmt.Errorf("eks: scale nodegroup: node_count must be positive")
	}

	_, err := d.client.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(ngName),
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			DesiredSize: aws.Int32(int32(nodeCount)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eks: scale nodegroup %q: %w", name, err)
	}
	return d.Read(ctx, name)
}

func (d *EKSNodeGroupDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func nodeGroupToOutput(ng *ekstypes.Nodegroup) *platform.ResourceOutput {
	if ng == nil {
		return nil
	}
	status := platform.ResourceStatusActive
	switch ng.Status {
	case ekstypes.NodegroupStatusCreating:
		status = platform.ResourceStatusCreating
	case ekstypes.NodegroupStatusDeleting:
		status = platform.ResourceStatusDeleting
	case ekstypes.NodegroupStatusDegraded:
		status = platform.ResourceStatusDegraded
	case ekstypes.NodegroupStatusUpdating:
		status = platform.ResourceStatusUpdating
	}

	props := map[string]any{
		"status": string(ng.Status),
	}
	if ng.ScalingConfig != nil {
		if ng.ScalingConfig.DesiredSize != nil {
			props["node_count"] = int(*ng.ScalingConfig.DesiredSize)
		}
	}
	if len(ng.InstanceTypes) > 0 {
		props["instance_type"] = ng.InstanceTypes[0]
	}
	if ng.NodegroupArn != nil {
		props["arn"] = *ng.NodegroupArn
	}

	name := ""
	if ng.NodegroupName != nil {
		name = *ng.NodegroupName
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "kubernetes_cluster",
		ProviderType: "aws.eks_nodegroup",
		Properties:   props,
		Status:       status,
		LastSynced:   time.Now(),
	}
}

// splitNodeGroupName splits "cluster:nodegroup" format. Falls back to using
// name as both cluster and nodegroup if no separator found.
func splitNodeGroupName(name string) (string, string) {
	for i, c := range name {
		if c == ':' {
			return name[:i], name[i+1:]
		}
	}
	return name, name
}

var _ platform.ResourceDriver = (*EKSNodeGroupDriver)(nil)
