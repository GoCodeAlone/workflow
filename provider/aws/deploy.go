package aws

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/google/uuid"

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

	// Determine cluster and service name, preferring request config over provider config.
	// Both are validated early to avoid registering a task definition only to fail later.
	cluster := p.config.ECSCluster
	if c, ok := req.Config["cluster"].(string); ok && c != "" {
		cluster = c
	}
	if cluster == "" {
		return nil, fmt.Errorf("aws: ECS cluster name is required (set ecs_cluster in config or pass cluster in request config)")
	}

	service := p.config.Service
	if s, ok := req.Config["service"].(string); ok && s != "" {
		service = s
	}
	if service == "" {
		return nil, fmt.Errorf("aws: ECS service name is required (set service in config or pass service in request config)")
	}

	// Determine task family from config or service name.
	family := service
	if f, ok := req.Config["task_family"].(string); ok && f != "" {
		family = f
	}

	// Build container definition with Essential=true and optional environment variables.
	containerDef := ecstypes.ContainerDefinition{
		Name:      awsv2.String(family),
		Image:     awsv2.String(req.Image),
		Essential: awsv2.Bool(true),
	}
	// Map req.Config["env_vars"] (map[string]any{"KEY": "value"}) to ECS key-value pairs.
	if envVars, ok := req.Config["env_vars"].(map[string]any); ok {
		for k, v := range envVars {
			if sv, ok := v.(string); ok {
				containerDef.Environment = append(containerDef.Environment, ecstypes.KeyValuePair{
					Name:  awsv2.String(k),
					Value: awsv2.String(sv),
				})
			}
		}
	}

	// Build the task definition input.
	taskInput := &ecs.RegisterTaskDefinitionInput{
		Family:                  awsv2.String(family),
		ContainerDefinitions:    []ecstypes.ContainerDefinition{containerDef},
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
	// Tag the task definition with the deployment environment for traceability.
	if req.Environment != "" {
		taskInput.Tags = append(taskInput.Tags, ecstypes.Tag{
			Key:   awsv2.String("environment"),
			Value: awsv2.String(req.Environment),
		})
	}

	taskOut, err := client.RegisterTaskDefinition(ctx, taskInput)
	if err != nil {
		return nil, fmt.Errorf("aws: register task definition: %w", err)
	}
	taskDefARN := awsv2.ToString(taskOut.TaskDefinition.TaskDefinitionArn)

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

	// Use a UUID to guarantee a unique, unambiguous deploy ID regardless of the image name.
	deployID := fmt.Sprintf("eks:%s:%s", clusterName, uuid.New().String())
	return &provider.DeployResult{
		DeployID:  deployID,
		Status:    "pending",
		Message:   fmt.Sprintf("EKS cluster %s (endpoint: %s) is ready; deploy image %s to namespace %s via kubectl or your CD tool", clusterName, awsv2.ToString(out.Cluster.Endpoint), req.Image, namespace),
		StartedAt: time.Now(),
	}, nil
}
