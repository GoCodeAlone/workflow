package aws

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/provider"
)

func init() {
	plugin.RegisterNativePluginFactory(func(_ *sql.DB, _ map[string]any) plugin.NativePlugin {
		return NewAWSProvider(AWSConfig{})
	})
}

// AWSConfig holds configuration for the AWS cloud provider.
type AWSConfig struct {
	Region          string `json:"region" yaml:"region"`
	AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key" yaml:"secret_access_key"`
	RoleARN         string `json:"role_arn" yaml:"role_arn"`
	ECSCluster      string `json:"ecs_cluster" yaml:"ecs_cluster"`
	EKSCluster      string `json:"eks_cluster" yaml:"eks_cluster"`
	Service         string `json:"service" yaml:"service"`
}

// AWSProvider implements CloudProvider for Amazon Web Services.
type AWSProvider struct {
	config    AWSConfig
	ecsClient ECSClient
	eksClient EKSClientIface
	cwClient  CloudWatchClient
}

// Compile-time interface check.
var _ provider.CloudProvider = (*AWSProvider)(nil)

// NewAWSProvider creates a new AWSProvider with the given configuration.
// AWS SDK clients are initialized lazily from the config on first use.
func NewAWSProvider(config AWSConfig) *AWSProvider {
	return &AWSProvider{config: config}
}

// NewAWSProviderWithClients creates an AWSProvider with pre-built clients (useful for testing).
func NewAWSProviderWithClients(config AWSConfig, ecsClient ECSClient, eksClient EKSClientIface, cwClient CloudWatchClient) *AWSProvider {
	return &AWSProvider{
		config:    config,
		ecsClient: ecsClient,
		eksClient: eksClient,
		cwClient:  cwClient,
	}
}

// buildAWSConfig builds an AWS SDK config from the provider configuration.
func (p *AWSProvider) buildAWSConfig(ctx context.Context) (awsv2.Config, error) {
	opts := []func(*awscfg.LoadOptions) error{
		awscfg.WithRegion(p.config.Region),
	}
	if p.config.AccessKeyID != "" && p.config.SecretAccessKey != "" {
		opts = append(opts, awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(p.config.AccessKeyID, p.config.SecretAccessKey, ""),
		))
	}
	return awscfg.LoadDefaultConfig(ctx, opts...)
}

// ensureECSClient lazily initializes the ECS client if not already set.
func (p *AWSProvider) ensureECSClient(ctx context.Context) (ECSClient, error) {
	if p.ecsClient != nil {
		return p.ecsClient, nil
	}
	cfg, err := p.buildAWSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws: build config: %w", err)
	}
	p.ecsClient = ecs.NewFromConfig(cfg)
	return p.ecsClient, nil
}

// ensureEKSClient lazily initializes the EKS client if not already set.
func (p *AWSProvider) ensureEKSClient(ctx context.Context) (EKSClientIface, error) {
	if p.eksClient != nil {
		return p.eksClient, nil
	}
	cfg, err := p.buildAWSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws: build config: %w", err)
	}
	p.eksClient = eks.NewFromConfig(cfg)
	return p.eksClient, nil
}

// ensureCWClient lazily initializes the CloudWatch client if not already set.
func (p *AWSProvider) ensureCWClient(ctx context.Context) (CloudWatchClient, error) {
	if p.cwClient != nil {
		return p.cwClient, nil
	}
	cfg, err := p.buildAWSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws: build config: %w", err)
	}
	p.cwClient = cloudwatch.NewFromConfig(cfg)
	return p.cwClient, nil
}

func (p *AWSProvider) Name() string        { return "aws" }
func (p *AWSProvider) Version() string     { return "1.0.0" }
func (p *AWSProvider) Description() string { return "AWS Cloud Provider (EC2, ECS, EKS, ECR)" }

func (p *AWSProvider) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{
			ID:       "aws-settings",
			Label:    "AWS Settings",
			Icon:     "cloud",
			Category: "cloud-providers",
		},
	}
}

func (p *AWSProvider) Dependencies() []plugin.PluginDependency { return nil }
func (p *AWSProvider) OnEnable(_ plugin.PluginContext) error   { return nil }
func (p *AWSProvider) OnDisable(_ plugin.PluginContext) error  { return nil }

func (p *AWSProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/providers/aws/status", p.handleStatus)
	mux.HandleFunc("/api/v1/providers/aws/regions", p.handleListRegions)
}

func (p *AWSProvider) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"provider":"aws","status":"available","version":"1.0.0"}`))
}

func (p *AWSProvider) handleListRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"regions":["us-east-1","us-west-2","eu-west-1","ap-southeast-1"]}`))
}

func (p *AWSProvider) Deploy(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceType, _ := req.Config["service_type"].(string)
	switch serviceType {
	case "eks":
		return p.deployEKS(ctx, req)
	default:
		return p.deployECS(ctx, req)
	}
}

// GetDeploymentStatus returns the current status of an ECS service deployment.
// The deployID must be in the format "<serviceARN>|<taskDefARN>" (as returned by Deploy).
// EKS deploy IDs (prefix "eks:") return a static pending status.
func (p *AWSProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	// EKS deployments do not have an ECS-backed status.
	if strings.HasPrefix(deployID, "eks:") {
		return &provider.DeployStatus{
			DeployID: deployID,
			Status:   "pending",
			Progress: 0,
			Message:  "EKS deployment status requires kubectl integration",
		}, nil
	}

	client, err := p.ensureECSClient(ctx)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(deployID, "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("aws: invalid deploy ID format: %q (expected \"<serviceARN>|<taskDefARN>\")", deployID)
	}
	serviceARN := parts[0]

	cluster, serviceName, err := parseServiceARN(serviceARN)
	if err != nil {
		return nil, err
	}
	// DescribeServices accepts both ARNs and names; use the full ARN for precision.
	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  awsv2.String(cluster),
		Services: []string{serviceARN},
	})
	if err != nil {
		return nil, fmt.Errorf("aws: describe services (cluster=%s, service=%s): %w", cluster, serviceName, err)
	}
	if len(out.Services) == 0 {
		return nil, fmt.Errorf("aws: service not found: %s", serviceARN)
	}

	svc := out.Services[0]
	status, progress, message := ecsServiceDeployStatus(svc.Deployments)

	instances := make([]provider.InstanceStatus, 0, len(svc.Deployments))
	for i := range svc.Deployments {
		d := &svc.Deployments[i]
		if d.Id == nil {
			continue
		}
		instances = append(instances, provider.InstanceStatus{
			ID:     awsv2.ToString(d.Id),
			Status: ecsDeploymentRoleToInstanceStatus(d.Status),
		})
	}

	return &provider.DeployStatus{
		DeployID:  deployID,
		Status:    status,
		Progress:  progress,
		Message:   message,
		Instances: instances,
	}, nil
}

// Rollback reverts an ECS service to the previous task definition revision.
// The deployID must be in the format "<serviceARN>|<taskDefARN>".
func (p *AWSProvider) Rollback(ctx context.Context, deployID string) error {
	if strings.HasPrefix(deployID, "eks:") {
		return fmt.Errorf("aws: EKS rollback requires kubectl integration")
	}

	client, err := p.ensureECSClient(ctx)
	if err != nil {
		return err
	}

	parts := strings.SplitN(deployID, "|", 2)
	if len(parts) != 2 {
		return fmt.Errorf("aws: invalid deploy ID format: %q (expected \"<serviceARN>|<taskDefARN>\")", deployID)
	}
	serviceARN := parts[0]
	taskDefARN := parts[1]

	cluster, service, err := parseServiceARN(serviceARN)
	if err != nil {
		return err
	}

	prevTaskDef, err := p.getPreviousTaskDef(ctx, client, taskDefARN)
	if err != nil {
		return err
	}

	_, err = client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        awsv2.String(cluster),
		Service:        awsv2.String(service),
		TaskDefinition: awsv2.String(prevTaskDef),
	})
	if err != nil {
		return fmt.Errorf("aws: rollback service %s to %s: %w", service, prevTaskDef, err)
	}
	return nil
}

// TestConnection verifies connectivity to the configured ECS cluster.
func (p *AWSProvider) TestConnection(ctx context.Context, config map[string]any) (*provider.ConnectionResult, error) {
	client, err := p.ensureECSClient(ctx)
	if err != nil {
		return &provider.ConnectionResult{
			Success: false,
			Message: fmt.Sprintf("failed to create ECS client: %v", err),
		}, nil
	}

	cluster := p.config.ECSCluster
	if c, ok := config["cluster"].(string); ok && c != "" {
		cluster = c
	}

	start := time.Now()
	_, err = client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{cluster},
	})
	latency := time.Since(start)

	if err != nil {
		return &provider.ConnectionResult{
			Success: false,
			Message: fmt.Sprintf("failed to describe ECS cluster %q: %v", cluster, err),
			Latency: latency,
		}, nil
	}

	return &provider.ConnectionResult{
		Success: true,
		Message: fmt.Sprintf("successfully connected to ECS cluster %q", cluster),
		Latency: latency,
		Details: map[string]any{
			"cluster": cluster,
			"region":  p.config.Region,
		},
	}, nil
}

// GetMetrics fetches CPU and memory utilisation for an ECS service from CloudWatch.
// The deployID must be in the format "<serviceARN>|<taskDefARN>" or "eks:<cluster>:<deploy>".
func (p *AWSProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	client, err := p.ensureCWClient(ctx)
	if err != nil {
		return nil, err
	}

	clusterName, serviceName, err := deployIDToClusterService(deployID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	start := now.Add(-window)
	period := int32(60)
	if window >= 5*time.Minute {
		period = int32(window.Seconds() / 5)
		if period < 60 {
			period = 60
		}
	}

	queries := []cwtypes.MetricDataQuery{
		{
			Id: awsv2.String("cpu"),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  awsv2.String("AWS/ECS"),
					MetricName: awsv2.String("CPUUtilization"),
					Dimensions: []cwtypes.Dimension{
						{Name: awsv2.String("ClusterName"), Value: awsv2.String(clusterName)},
						{Name: awsv2.String("ServiceName"), Value: awsv2.String(serviceName)},
					},
				},
				Period: awsv2.Int32(period),
				Stat:   awsv2.String("Average"),
			},
		},
		{
			Id: awsv2.String("memory"),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  awsv2.String("AWS/ECS"),
					MetricName: awsv2.String("MemoryUtilization"),
					Dimensions: []cwtypes.Dimension{
						{Name: awsv2.String("ClusterName"), Value: awsv2.String(clusterName)},
						{Name: awsv2.String("ServiceName"), Value: awsv2.String(serviceName)},
					},
				},
				Period: awsv2.Int32(period),
				Stat:   awsv2.String("Average"),
			},
		},
	}

	out, err := client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
		StartTime:         awsv2.Time(start),
		EndTime:           awsv2.Time(now),
		MetricDataQueries: queries,
	})
	if err != nil {
		return nil, fmt.Errorf("aws: get metric data (cluster=%s, service=%s): %w", clusterName, serviceName, err)
	}

	metrics := &provider.Metrics{CustomMetrics: make(map[string]any)}
	for _, result := range out.MetricDataResults {
		if result.Id == nil || len(result.Values) == 0 {
			continue
		}
		switch awsv2.ToString(result.Id) {
		case "cpu":
			metrics.CPU = result.Values[0]
		case "memory":
			metrics.Memory = result.Values[0]
		}
	}
	return metrics, nil
}

// parseServiceARN extracts the cluster and service name from an ECS service ARN.
// New format: arn:aws:ecs:<region>:<account>:service/<cluster>/<service>
// Old format: arn:aws:ecs:<region>:<account>:service/<service>
func parseServiceARN(serviceARN string) (cluster, service string, err error) {
	// Everything after the last colon contains "service/<cluster>/<service>" or "service/<service>".
	idx := strings.LastIndex(serviceARN, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("aws: invalid service ARN: %q", serviceARN)
	}
	path := serviceARN[idx+1:]            // e.g. "service/my-cluster/my-service"
	parts := strings.SplitN(path, "/", 3) // ["service", "my-cluster", "my-service"]
	if len(parts) == 3 && parts[0] == "service" {
		return parts[1], parts[2], nil
	}
	if len(parts) == 2 && parts[0] == "service" {
		// Old ARN format — no cluster segment; return empty cluster.
		return "", parts[1], nil
	}
	return "", "", fmt.Errorf("aws: invalid service ARN path %q in ARN %q", path, serviceARN)
}

// getPreviousTaskDef returns the family:revision identifier for the revision immediately
// before the one described by taskDefARN.
func (p *AWSProvider) getPreviousTaskDef(ctx context.Context, client ECSClient, taskDefARN string) (string, error) {
	out, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: awsv2.String(taskDefARN),
	})
	if err != nil {
		return "", fmt.Errorf("aws: describe task definition %s: %w", taskDefARN, err)
	}
	family := awsv2.ToString(out.TaskDefinition.Family)
	revision := out.TaskDefinition.Revision
	if revision <= 1 {
		return "", fmt.Errorf("aws: no previous revision available for task definition %s", taskDefARN)
	}
	return fmt.Sprintf("%s:%d", family, revision-1), nil
}

// ecsServiceDeployStatus derives a unified status from the list of ECS service deployments.
func ecsServiceDeployStatus(deployments []ecstypes.Deployment) (status string, progress int, message string) {
	for i := range deployments {
		d := &deployments[i]
		if awsv2.ToString(d.Status) != "PRIMARY" {
			continue
		}
		switch d.RolloutState {
		case ecstypes.DeploymentRolloutStateCompleted:
			return "succeeded", 100, "deployment completed"
		case ecstypes.DeploymentRolloutStateFailed:
			msg := awsv2.ToString(d.RolloutStateReason)
			if msg == "" {
				msg = "deployment failed"
			}
			return "failed", 0, msg
		default: // IN_PROGRESS or unset
			desired := d.DesiredCount
			running := d.RunningCount
			pct := 0
			if desired > 0 {
				pct = int(running * 100 / desired)
			}
			return "in_progress", pct, fmt.Sprintf("running %d/%d tasks", running, desired)
		}
	}
	return "unknown", 0, "no PRIMARY deployment found"
}

// ecsDeploymentRoleToInstanceStatus maps an ECS deployment role (PRIMARY/ACTIVE/INACTIVE)
// to a provider instance status string.
func ecsDeploymentRoleToInstanceStatus(role *string) string {
	switch strings.ToUpper(awsv2.ToString(role)) {
	case "PRIMARY":
		return "running"
	case "ACTIVE":
		return "pending"
	default:
		return "stopped"
	}
}

// deployIDToClusterService parses a deploy ID and returns the ECS cluster and service names.
func deployIDToClusterService(deployID string) (cluster, service string, err error) {
	if strings.HasPrefix(deployID, "eks:") {
		// "eks:<clusterName>:<deployName>"
		parts := strings.SplitN(deployID, ":", 3)
		if len(parts) < 2 || parts[1] == "" {
			return "", "", fmt.Errorf("aws: cannot parse EKS deploy ID for metrics: %q", deployID)
		}
		return parts[1], "", nil
	}
	// ECS: "<serviceARN>|<taskDefARN>"
	parts := strings.SplitN(deployID, "|", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("aws: invalid deploy ID format: %q", deployID)
	}
	cluster, service, err = parseServiceARN(parts[0])
	return cluster, service, err
}
