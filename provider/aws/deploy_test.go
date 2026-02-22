package aws

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/GoCodeAlone/workflow/provider"
)

// ---------------------------------------------------------------------------
// Mock ECS client
// ---------------------------------------------------------------------------

type mockECSClient struct {
	registerTaskDefFunc  func(ctx context.Context, params *ecs.RegisterTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error)
	updateServiceFunc    func(ctx context.Context, params *ecs.UpdateServiceInput, optFns ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error)
	describeServicesFunc func(ctx context.Context, params *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	describeClustersFunc func(ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	describeTaskDefFunc  func(ctx context.Context, params *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error)
}

func (m *mockECSClient) RegisterTaskDefinition(ctx context.Context, params *ecs.RegisterTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
	if m.registerTaskDefFunc != nil {
		return m.registerTaskDefFunc(ctx, params, optFns...)
	}
	return &ecs.RegisterTaskDefinitionOutput{
		TaskDefinition: &ecstypes.TaskDefinition{
			TaskDefinitionArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"),
			Family:            awsv2.String("my-task"),
			Revision:          5,
		},
	}, nil
}

func (m *mockECSClient) UpdateService(ctx context.Context, params *ecs.UpdateServiceInput, optFns ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
	if m.updateServiceFunc != nil {
		return m.updateServiceFunc(ctx, params, optFns...)
	}
	return &ecs.UpdateServiceOutput{
		Service: &ecstypes.Service{
			ServiceArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service"),
		},
	}, nil
}

func (m *mockECSClient) DescribeServices(ctx context.Context, params *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	if m.describeServicesFunc != nil {
		return m.describeServicesFunc(ctx, params, optFns...)
	}
	return &ecs.DescribeServicesOutput{}, nil
}

func (m *mockECSClient) DescribeClusters(ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	if m.describeClustersFunc != nil {
		return m.describeClustersFunc(ctx, params, optFns...)
	}
	return &ecs.DescribeClustersOutput{}, nil
}

func (m *mockECSClient) DescribeTaskDefinition(ctx context.Context, params *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
	if m.describeTaskDefFunc != nil {
		return m.describeTaskDefFunc(ctx, params, optFns...)
	}
	return &ecs.DescribeTaskDefinitionOutput{
		TaskDefinition: &ecstypes.TaskDefinition{
			Family:   awsv2.String("my-task"),
			Revision: 5,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Mock EKS client
// ---------------------------------------------------------------------------

type mockEKSClient struct {
	describeClusterFunc func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

func (m *mockEKSClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if m.describeClusterFunc != nil {
		return m.describeClusterFunc(ctx, params, optFns...)
	}
	return &eks.DescribeClusterOutput{
		Cluster: &ekstypes.Cluster{
			Name:     awsv2.String("my-eks-cluster"),
			Endpoint: awsv2.String("https://eks.example.com"),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDeployECS_Success(t *testing.T) {
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	req := provider.DeployRequest{
		Image:       "myrepo/myapp:latest",
		Environment: "production",
		Config:      map[string]any{},
	}
	result, err := p.deployECS(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "in_progress" {
		t.Errorf("expected status=in_progress, got %q", result.Status)
	}
	if result.DeployID == "" {
		t.Error("expected non-empty DeployID")
	}
}

func TestDeployECS_RegistersTaskDefinitionWithImage(t *testing.T) {
	var capturedImage string
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{
		registerTaskDefFunc: func(_ context.Context, params *ecs.RegisterTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
			if len(params.ContainerDefinitions) > 0 {
				capturedImage = awsv2.ToString(params.ContainerDefinitions[0].Image)
			}
			return &ecs.RegisterTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					TaskDefinitionArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:task-definition/my-service:3"),
					Family:            awsv2.String("my-service"),
					Revision:          3,
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	req := provider.DeployRequest{
		Image:  "myrepo/myapp:v2",
		Config: map[string]any{},
	}
	_, err := p.deployECS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedImage != "myrepo/myapp:v2" {
		t.Errorf("expected image %q, got %q", "myrepo/myapp:v2", capturedImage)
	}
}

func TestDeployECS_UpdatesService(t *testing.T) {
	var capturedTaskDef string
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{
		updateServiceFunc: func(_ context.Context, params *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
			capturedTaskDef = awsv2.ToString(params.TaskDefinition)
			return &ecs.UpdateServiceOutput{
				Service: &ecstypes.Service{
					ServiceArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service"),
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	req := provider.DeployRequest{
		Image:  "myrepo/myapp:v2",
		Config: map[string]any{},
	}
	_, err := p.deployECS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTaskDef == "" {
		t.Error("expected UpdateService to be called with a task definition ARN")
	}
}

func TestDeployECS_DeployIDContainsARNs(t *testing.T) {
	const serviceARN = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service"
	const taskDefARN = "arn:aws:ecs:us-east-1:123456789012:task-definition/my-service:7"

	ecsClient := &mockECSClient{
		registerTaskDefFunc: func(_ context.Context, _ *ecs.RegisterTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
			return &ecs.RegisterTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					TaskDefinitionArn: awsv2.String(taskDefARN),
					Family:            awsv2.String("my-service"),
					Revision:          7,
				},
			}, nil
		},
		updateServiceFunc: func(_ context.Context, _ *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
			return &ecs.UpdateServiceOutput{
				Service: &ecstypes.Service{ServiceArn: awsv2.String(serviceARN)},
			}, nil
		},
	}
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	result, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedID := serviceARN + "|" + taskDefARN
	if result.DeployID != expectedID {
		t.Errorf("DeployID mismatch\ngot:  %q\nwant: %q", result.DeployID, expectedID)
	}
}

func TestDeployECS_MissingCluster(t *testing.T) {
	cfg := AWSConfig{Service: "my-service"} // no ECSCluster
	ecsClient := &mockECSClient{}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	_, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{}, // no cluster override either
	})
	if err == nil {
		t.Fatal("expected an error when cluster name is missing")
	}
}

func TestDeployECS_MissingService(t *testing.T) {
	cfg := AWSConfig{ECSCluster: "my-cluster"} // no Service
	ecsClient := &mockECSClient{}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	_, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{}, // no service override either
	})
	if err == nil {
		t.Fatal("expected an error when service name is missing")
	}
}

func TestDeployECS_SetsContainerEssential(t *testing.T) {
	var capturedEssential *bool
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{
		registerTaskDefFunc: func(_ context.Context, params *ecs.RegisterTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
			if len(params.ContainerDefinitions) > 0 {
				capturedEssential = params.ContainerDefinitions[0].Essential
			}
			return &ecs.RegisterTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					TaskDefinitionArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:task-definition/my-service:1"),
					Family:            awsv2.String("my-service"),
					Revision:          1,
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	_, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedEssential == nil || !*capturedEssential {
		t.Error("expected Essential=true on the container definition")
	}
}

func TestDeployECS_SetsEnvironmentTag(t *testing.T) {
	var capturedTags []ecstypes.Tag
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{
		registerTaskDefFunc: func(_ context.Context, params *ecs.RegisterTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
			capturedTags = params.Tags
			return &ecs.RegisterTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					TaskDefinitionArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:task-definition/my-service:1"),
					Family:            awsv2.String("my-service"),
					Revision:          1,
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	_, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image:       "img:latest",
		Environment: "production",
		Config:      map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, tag := range capturedTags {
		if awsv2.ToString(tag.Key) == "environment" && awsv2.ToString(tag.Value) == "production" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected environment=production tag, got: %v", capturedTags)
	}
}

func TestDeployECS_SetsEnvVarsFromConfig(t *testing.T) {
	var capturedEnv []ecstypes.KeyValuePair
	cfg := AWSConfig{ECSCluster: "my-cluster", Service: "my-service"}
	ecsClient := &mockECSClient{
		registerTaskDefFunc: func(_ context.Context, params *ecs.RegisterTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.RegisterTaskDefinitionOutput, error) {
			if len(params.ContainerDefinitions) > 0 {
				capturedEnv = params.ContainerDefinitions[0].Environment
			}
			return &ecs.RegisterTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					TaskDefinitionArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:task-definition/my-service:1"),
					Family:            awsv2.String("my-service"),
					Revision:          1,
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	_, err := p.deployECS(context.Background(), provider.DeployRequest{
		Image: "img:latest",
		Config: map[string]any{
			"env_vars": map[string]any{
				"APP_ENV": "prod",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, kv := range capturedEnv {
		if awsv2.ToString(kv.Name) == "APP_ENV" && awsv2.ToString(kv.Value) == "prod" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected APP_ENV=prod env var, got: %v", capturedEnv)
	}
}

func TestDeployEKS_DeployIDIsUnique(t *testing.T) {
	cfg := AWSConfig{EKSCluster: "my-eks-cluster"}
	eksClient := &mockEKSClient{}
	p := NewAWSProviderWithClients(cfg, nil, eksClient, nil)

	req := provider.DeployRequest{
		Image:  "myrepo/myapp:v1",
		Config: map[string]any{},
	}
	result1, err := p.deployEKS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result2, err := p.deployEKS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result1.DeployID == result2.DeployID {
		t.Error("expected unique deploy IDs for distinct EKS deployments of the same image")
	}
}
func TestDeployEKS_Success(t *testing.T) {
	cfg := AWSConfig{EKSCluster: "my-eks-cluster"}
	eksClient := &mockEKSClient{}
	p := NewAWSProviderWithClients(cfg, nil, eksClient, nil)

	req := provider.DeployRequest{
		Image:  "myrepo/myapp:v1",
		Config: map[string]any{},
	}
	result, err := p.deployEKS(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("expected status=pending, got %q", result.Status)
	}
	if result.DeployID == "" {
		t.Error("expected non-empty DeployID")
	}
}
func TestDeployEKS_MissingCluster(t *testing.T) {
	cfg := AWSConfig{} // no EKSCluster set
	eksClient := &mockEKSClient{}
	p := NewAWSProviderWithClients(cfg, nil, eksClient, nil)

	_, err := p.deployEKS(context.Background(), provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected an error when cluster name is missing")
	}
}

func TestDeployEKS_ClusterFromRequestConfig(t *testing.T) {
	var capturedCluster string
	cfg := AWSConfig{}
	eksClient := &mockEKSClient{
		describeClusterFunc: func(_ context.Context, params *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			capturedCluster = awsv2.ToString(params.Name)
			return &eks.DescribeClusterOutput{
				Cluster: &ekstypes.Cluster{
					Name:     params.Name,
					Endpoint: awsv2.String("https://eks.example.com"),
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(cfg, nil, eksClient, nil)

	req := provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{"cluster": "override-cluster"},
	}
	_, err := p.deployEKS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCluster != "override-cluster" {
		t.Errorf("expected cluster %q, got %q", "override-cluster", capturedCluster)
	}
}

func TestDeployDispatch_EKS(t *testing.T) {
	cfg := AWSConfig{EKSCluster: "my-eks"}
	eksClient := &mockEKSClient{}
	p := NewAWSProviderWithClients(cfg, nil, eksClient, nil)

	req := provider.DeployRequest{
		Image:  "img:latest",
		Config: map[string]any{"service_type": "eks"},
	}
	result, err := p.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("expected EKS status=pending, got %q", result.Status)
	}
}
