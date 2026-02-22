package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/GoCodeAlone/workflow/provider"
)

// deployECS handles deployment to AWS ECS (Fargate or EC2 launch type).
// It registers a new task definition with the provided image and updates the ECS service.
// The returned DeployID has the format "<serviceARN>|<taskDefARN>".
func (p *AWSProvider) deployECS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	client, err := p.ensureECSClient(ctx)
	if err != nil {
		return nil, err
	}

	// Determine task family from config or service name.
	family := p.config.Service
	if f, ok := req.Config["task_family"].(string); ok && f != "" {
		family = f
	}
	if family == "" {
		family = "workflow-task"
	}

	// Build the task definition input.
	taskInput := &ecs.RegisterTaskDefinitionInput{
		Family: awsv2.String(family),
		ContainerDefinitions: []ecstypes.ContainerDefinition{
			{
				Name:  awsv2.String(family),
				Image: awsv2.String(req.Image),
			},
		},
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		Cpu:                     awsv2.String("256"),
		Memory:                  awsv2.String("512"),
	}
	if cpu, ok := req.Config["cpu"].(string); ok && cpu != "" {
		taskInput.Cpu = awsv2.String(cpu)
	}
	if mem, ok := req.Config["memory"].(string); ok && mem != "" {
		taskInput.Memory = awsv2.String(mem)
	}

	taskOut, err := client.RegisterTaskDefinition(ctx, taskInput)
	if err != nil {
		return nil, fmt.Errorf("aws: register task definition: %w", err)
	}
	taskDefARN := awsv2.ToString(taskOut.TaskDefinition.TaskDefinitionArn)

	// Determine cluster and service name, preferring request config over provider config.
	cluster := p.config.ECSCluster
	if c, ok := req.Config["cluster"].(string); ok && c != "" {
		cluster = c
	}
	service := p.config.Service
	if s, ok := req.Config["service"].(string); ok && s != "" {
		service = s
	}

	// Update the ECS service to use the new task definition.
	svcOut, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        awsv2.String(cluster),
		Service:        awsv2.String(service),
		TaskDefinition: awsv2.String(taskDefARN),
	})
	if err != nil {
		return nil, fmt.Errorf("aws: update service: %w", err)
	}

	serviceARN := awsv2.ToString(svcOut.Service.ServiceArn)
	return &provider.DeployResult{
		DeployID:  serviceARN + "|" + taskDefARN,
		Status:    "in_progress",
		Message:   fmt.Sprintf("ECS deployment initiated: service=%s task_definition=%s", service, taskDefARN),
		StartedAt: time.Now(),
	}, nil
}

// deployEKS handles deployment to AWS EKS (Elastic Kubernetes Service).
// It verifies the cluster is accessible via DescribeCluster and returns a pending
// deployment result. Full Kubernetes rollout requires kubectl or a k8s API client.
func (p *AWSProvider) deployEKS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	client, err := p.ensureEKSClient(ctx)
	if err != nil {
		return nil, err
	}

	clusterName := p.config.EKSCluster
	if c, ok := req.Config["cluster"].(string); ok && c != "" {
		clusterName = c
	}
	if clusterName == "" {
		return nil, fmt.Errorf("aws: EKS cluster name is required (set eks_cluster in config or pass cluster in request config)")
	}

	out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: awsv2.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("aws: describe EKS cluster %s: %w", clusterName, err)
	}

	namespace := "default"
	if ns, ok := req.Config["namespace"].(string); ok && ns != "" {
		namespace = ns
	}
	deployName := strings.ReplaceAll(req.Image, ":", "-")
	deployName = strings.ReplaceAll(deployName, "/", "-")

	deployID := fmt.Sprintf("eks:%s:%s", clusterName, deployName)
	return &provider.DeployResult{
		DeployID:  deployID,
		Status:    "pending",
		Message:   fmt.Sprintf("EKS cluster %s (endpoint: %s) is ready; deploy image %s to namespace %s via kubectl or your CD tool", clusterName, awsv2.ToString(out.Cluster.Endpoint), req.Image, namespace),
		StartedAt: time.Now(),
	}, nil
}
