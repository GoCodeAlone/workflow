package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ---------------------------------------------------------------------------
// Mock CloudWatch client
// ---------------------------------------------------------------------------

type mockCWClient struct {
	getMetricDataFunc func(ctx context.Context, params *cloudwatch.GetMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

func (m *mockCWClient) GetMetricData(ctx context.Context, params *cloudwatch.GetMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	if m.getMetricDataFunc != nil {
		return m.getMetricDataFunc(ctx, params, optFns...)
	}
	return &cloudwatch.GetMetricDataOutput{}, nil
}

// ---------------------------------------------------------------------------
// GetDeploymentStatus tests
// ---------------------------------------------------------------------------

func TestGetDeploymentStatus_ECSInProgress(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"
	ecsClient := &mockECSClient{
		describeServicesFunc: func(_ context.Context, params *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
			return &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						ServiceArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc"),
						Deployments: []ecstypes.Deployment{
							{
								Id:           awsv2.String("ecs-deploy-1"),
								Status:       awsv2.String("PRIMARY"),
								RolloutState: ecstypes.DeploymentRolloutStateInProgress,
								DesiredCount: 3,
								RunningCount: 1,
							},
						},
					},
				},
			}, nil
		},
	}
	cfg := AWSConfig{}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	status, err := p.GetDeploymentStatus(context.Background(), deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "in_progress" {
		t.Errorf("expected status=in_progress, got %q", status.Status)
	}
	if status.Progress < 0 || status.Progress > 100 {
		t.Errorf("progress out of range: %d", status.Progress)
	}
	if len(status.Instances) == 0 {
		t.Error("expected at least one instance")
	}
}

func TestGetDeploymentStatus_ECSCompleted(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"
	ecsClient := &mockECSClient{
		describeServicesFunc: func(_ context.Context, _ *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
			return &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Deployments: []ecstypes.Deployment{
							{
								Id:           awsv2.String("ecs-deploy-1"),
								Status:       awsv2.String("PRIMARY"),
								RolloutState: ecstypes.DeploymentRolloutStateCompleted,
								DesiredCount: 3,
								RunningCount: 3,
							},
						},
					},
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{}, ecsClient, nil, nil)

	status, err := p.GetDeploymentStatus(context.Background(), deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "succeeded" {
		t.Errorf("expected status=succeeded, got %q", status.Status)
	}
	if status.Progress != 100 {
		t.Errorf("expected progress=100, got %d", status.Progress)
	}
}

func TestGetDeploymentStatus_ECSFailed(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"
	ecsClient := &mockECSClient{
		describeServicesFunc: func(_ context.Context, _ *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
			return &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Deployments: []ecstypes.Deployment{
							{
								Id:                 awsv2.String("ecs-deploy-1"),
								Status:             awsv2.String("PRIMARY"),
								RolloutState:       ecstypes.DeploymentRolloutStateFailed,
								RolloutStateReason: awsv2.String("ECS tasks failed health check"),
							},
						},
					},
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{}, ecsClient, nil, nil)

	status, err := p.GetDeploymentStatus(context.Background(), deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "failed" {
		t.Errorf("expected status=failed, got %q", status.Status)
	}
}

func TestGetDeploymentStatus_EKSDeployID(t *testing.T) {
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, nil)

	status, err := p.GetDeploymentStatus(context.Background(), "eks:my-cluster:myapp-v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "pending" {
		t.Errorf("expected status=pending for EKS, got %q", status.Status)
	}
}

func TestGetDeploymentStatus_InvalidDeployID(t *testing.T) {
	p := NewAWSProviderWithClients(AWSConfig{}, &mockECSClient{}, nil, nil)

	_, err := p.GetDeploymentStatus(context.Background(), "invalid-id-without-pipe")
	if err == nil {
		t.Error("expected error for invalid deploy ID")
	}
}

func TestGetDeploymentStatus_OldFormatARN(t *testing.T) {
	// Old-format ARN: no cluster segment – cluster should fall back to p.config.ECSCluster.
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"
	var capturedCluster string
	ecsClient := &mockECSClient{
		describeServicesFunc: func(_ context.Context, params *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
			capturedCluster = awsv2.ToString(params.Cluster)
			return &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Deployments: []ecstypes.Deployment{
							{
								Id:           awsv2.String("deploy-1"),
								Status:       awsv2.String("PRIMARY"),
								RolloutState: ecstypes.DeploymentRolloutStateCompleted,
								DesiredCount: 2,
								RunningCount: 2,
							},
						},
					},
				},
			}, nil
		},
	}
	cfg := AWSConfig{ECSCluster: "fallback-cluster"}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	status, err := p.GetDeploymentStatus(context.Background(), deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "succeeded" {
		t.Errorf("expected status=succeeded, got %q", status.Status)
	}
	if capturedCluster != "fallback-cluster" {
		t.Errorf("expected cluster=%q from config fallback, got %q", "fallback-cluster", capturedCluster)
	}
}

func TestRollback_OldFormatARN(t *testing.T) {
	// Old-format ARN: no cluster segment – cluster should fall back to p.config.ECSCluster.
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:3"
	var capturedCluster string
	ecsClient := &mockECSClient{
		describeTaskDefFunc: func(_ context.Context, _ *ecs.DescribeTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
			return &ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					Family:   awsv2.String("my-task"),
					Revision: 3,
				},
			}, nil
		},
		updateServiceFunc: func(_ context.Context, params *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
			capturedCluster = awsv2.ToString(params.Cluster)
			return &ecs.UpdateServiceOutput{
				Service: &ecstypes.Service{
					ServiceArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:service/my-svc"),
				},
			}, nil
		},
	}
	cfg := AWSConfig{ECSCluster: "fallback-cluster"}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	if err := p.Rollback(context.Background(), deployID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCluster != "fallback-cluster" {
		t.Errorf("expected cluster=%q from config fallback, got %q", "fallback-cluster", capturedCluster)
	}
}

func TestRollback_Success(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"

	var updatedTaskDef string
	ecsClient := &mockECSClient{
		describeTaskDefFunc: func(_ context.Context, _ *ecs.DescribeTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
			return &ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					Family:   awsv2.String("my-task"),
					Revision: 5,
				},
			}, nil
		},
		updateServiceFunc: func(_ context.Context, params *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
			updatedTaskDef = awsv2.ToString(params.TaskDefinition)
			return &ecs.UpdateServiceOutput{
				Service: &ecstypes.Service{
					ServiceArn: awsv2.String("arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc"),
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{}, ecsClient, nil, nil)

	if err := p.Rollback(context.Background(), deployID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedTaskDef != "my-task:4" {
		t.Errorf("expected rollback to revision 4, got %q", updatedTaskDef)
	}
}

func TestRollback_NoRevisionAvailable(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1"
	ecsClient := &mockECSClient{
		describeTaskDefFunc: func(_ context.Context, _ *ecs.DescribeTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
			return &ecs.DescribeTaskDefinitionOutput{
				TaskDefinition: &ecstypes.TaskDefinition{
					Family:   awsv2.String("my-task"),
					Revision: 1,
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{}, ecsClient, nil, nil)

	err := p.Rollback(context.Background(), deployID)
	if err == nil {
		t.Error("expected error when there is no previous revision")
	}
}

func TestRollback_EKSReturnsError(t *testing.T) {
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, nil)

	err := p.Rollback(context.Background(), "eks:my-cluster:myapp-v1")
	if err == nil {
		t.Error("expected error for EKS rollback")
	}
}

func TestRollback_InvalidDeployID(t *testing.T) {
	p := NewAWSProviderWithClients(AWSConfig{}, &mockECSClient{}, nil, nil)

	err := p.Rollback(context.Background(), "no-pipe-here")
	if err == nil {
		t.Error("expected error for invalid deploy ID")
	}
}

// ---------------------------------------------------------------------------
// TestConnection tests
// ---------------------------------------------------------------------------

func TestTestConnection_Success(t *testing.T) {
	ecsClient := &mockECSClient{}
	cfg := AWSConfig{ECSCluster: "my-cluster", Region: "us-east-1"}
	p := NewAWSProviderWithClients(cfg, ecsClient, nil, nil)

	result, err := p.TestConnection(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false: %s", result.Message)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestTestConnection_ClusterOverride(t *testing.T) {
	var capturedCluster string
	ecsClient := &mockECSClient{
		describeClustersFunc: func(_ context.Context, params *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
			if len(params.Clusters) > 0 {
				capturedCluster = params.Clusters[0]
			}
			return &ecs.DescribeClustersOutput{}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{ECSCluster: "default-cluster"}, ecsClient, nil, nil)

	_, err := p.TestConnection(context.Background(), map[string]any{"cluster": "override-cluster"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCluster != "override-cluster" {
		t.Errorf("expected cluster %q, got %q", "override-cluster", capturedCluster)
	}
}

func TestTestConnection_Failure(t *testing.T) {
	ecsClient := &mockECSClient{
		describeClustersFunc: func(_ context.Context, _ *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{ECSCluster: "my-cluster"}, ecsClient, nil, nil)

	result, err := p.TestConnection(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error (TestConnection should not propagate errors): %v", err)
	}
	if result.Success {
		t.Error("expected Success=false on client error")
	}
}

// ---------------------------------------------------------------------------
// GetMetrics tests
// ---------------------------------------------------------------------------

func TestGetMetrics_ReturnsCloudWatchData(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"

	cwClient := &mockCWClient{
		getMetricDataFunc: func(_ context.Context, _ *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
			return &cloudwatch.GetMetricDataOutput{
				MetricDataResults: []cwtypes.MetricDataResult{
					{Id: awsv2.String("cpu"), Values: []float64{42.5}},
					{Id: awsv2.String("memory"), Values: []float64{68.0}},
				},
			}, nil
		},
	}
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, cwClient)

	metrics, err := p.GetMetrics(context.Background(), deployID, 5*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.CPU != 42.5 {
		t.Errorf("expected CPU=42.5, got %v", metrics.CPU)
	}
	if metrics.Memory != 68.0 {
		t.Errorf("expected Memory=68.0, got %v", metrics.Memory)
	}
}

func TestGetMetrics_EmptyResults(t *testing.T) {
	const deployID = "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-svc|arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:5"

	cwClient := &mockCWClient{} // returns empty results
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, cwClient)

	metrics, err := p.GetMetrics(context.Background(), deployID, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.CPU != 0 || metrics.Memory != 0 {
		t.Errorf("expected zero metrics for empty CloudWatch response, got CPU=%v Memory=%v", metrics.CPU, metrics.Memory)
	}
}

func TestGetMetrics_EKSDeployID(t *testing.T) {
	cwClient := &mockCWClient{}
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, cwClient)

	_, err := p.GetMetrics(context.Background(), "eks:my-cluster:myapp-v1", 5*time.Minute)
	if err == nil {
		t.Error("expected error: EKS metrics are not available via CloudWatch ECS namespace")
	}
}

func TestGetMetrics_InvalidDeployID(t *testing.T) {
	cwClient := &mockCWClient{}
	p := NewAWSProviderWithClients(AWSConfig{}, nil, nil, cwClient)

	_, err := p.GetMetrics(context.Background(), "bad-deploy-id", time.Minute)
	if err == nil {
		t.Error("expected error for invalid deploy ID")
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestParseServiceARN_NewFormat(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service"
	cluster, service, err := parseServiceARN(arn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "my-cluster" {
		t.Errorf("expected cluster=%q, got %q", "my-cluster", cluster)
	}
	if service != "my-service" {
		t.Errorf("expected service=%q, got %q", "my-service", service)
	}
}

func TestParseServiceARN_OldFormat(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:123456789012:service/my-service"
	cluster, service, err := parseServiceARN(arn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "" {
		t.Errorf("expected empty cluster for old ARN format, got %q", cluster)
	}
	if service != "my-service" {
		t.Errorf("expected service=%q, got %q", "my-service", service)
	}
}

func TestParseServiceARN_Invalid(t *testing.T) {
	_, _, err := parseServiceARN("not-an-arn")
	if err == nil {
		t.Error("expected error for invalid ARN")
	}
}

func TestECSServiceDeployStatus_InProgress(t *testing.T) {
	deployments := []ecstypes.Deployment{
		{
			Status:       awsv2.String("PRIMARY"),
			RolloutState: ecstypes.DeploymentRolloutStateInProgress,
			DesiredCount: 4,
			RunningCount: 2,
		},
	}
	status, progress, _ := ecsServiceDeployStatus(deployments)
	if status != "in_progress" {
		t.Errorf("expected in_progress, got %q", status)
	}
	if progress != 50 {
		t.Errorf("expected progress=50, got %d", progress)
	}
}

func TestECSServiceDeployStatus_NoPrimary(t *testing.T) {
	deployments := []ecstypes.Deployment{
		{Status: awsv2.String("ACTIVE"), RolloutState: ecstypes.DeploymentRolloutStateInProgress},
	}
	status, _, _ := ecsServiceDeployStatus(deployments)
	if status != "unknown" {
		t.Errorf("expected unknown, got %q", status)
	}
}

func TestECSDeploymentRoleToInstanceStatus(t *testing.T) {
	tests := []struct {
		role string
		want string
	}{
		{"PRIMARY", "running"},
		{"primary", "running"},
		{"ACTIVE", "pending"},
		{"active", "pending"},
		{"INACTIVE", "stopped"},
		{"", "stopped"},
	}
	for _, tc := range tests {
		got := ecsDeploymentRoleToInstanceStatus(awsv2.String(tc.role))
		if got != tc.want {
			t.Errorf("ecsDeploymentRoleToInstanceStatus(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestProviderMetadata(t *testing.T) {
	p := NewAWSProvider(AWSConfig{})
	if p.Name() != "aws" {
		t.Errorf("expected Name()=aws, got %q", p.Name())
	}
	if p.Version() == "" {
		t.Error("expected non-empty Version()")
	}
	if p.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}
