package aws

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

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
	Service         string `json:"service" yaml:"service"`
}

// AWSProvider implements CloudProvider for Amazon Web Services.
type AWSProvider struct {
	config AWSConfig
}

// Compile-time interface check.
var _ provider.CloudProvider = (*AWSProvider)(nil)

// NewAWSProvider creates a new AWSProvider with the given configuration.
func NewAWSProvider(config AWSConfig) *AWSProvider {
	return &AWSProvider{config: config}
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

func (p *AWSProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	return nil, fmt.Errorf("aws: GetDeploymentStatus not yet implemented for deploy %s", deployID)
}

func (p *AWSProvider) Rollback(ctx context.Context, deployID string) error {
	return fmt.Errorf("aws: Rollback not yet implemented for deploy %s", deployID)
}

func (p *AWSProvider) TestConnection(ctx context.Context, config map[string]any) (*provider.ConnectionResult, error) {
	return nil, fmt.Errorf("aws: TestConnection not yet implemented")
}

func (p *AWSProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	return nil, fmt.Errorf("aws: GetMetrics not yet implemented for deploy %s", deployID)
}
