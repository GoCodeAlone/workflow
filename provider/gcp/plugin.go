package gcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/provider"
)

// GCPConfig holds configuration for the GCP cloud provider.
type GCPConfig struct {
	ProjectID       string `json:"project_id" yaml:"project_id"`
	CredentialsJSON string `json:"credentials_json" yaml:"credentials_json"`
	Region          string `json:"region" yaml:"region"`
}

// GCPProvider implements CloudProvider for Google Cloud Platform.
type GCPProvider struct {
	config GCPConfig
}

// Compile-time interface check.
var _ provider.CloudProvider = (*GCPProvider)(nil)

// NewGCPProvider creates a new GCPProvider with the given configuration.
func NewGCPProvider(config GCPConfig) *GCPProvider {
	return &GCPProvider{config: config}
}

func (p *GCPProvider) Name() string        { return "gcp" }
func (p *GCPProvider) Version() string     { return "1.0.0" }
func (p *GCPProvider) Description() string { return "GCP Cloud Provider (GKE, Cloud Run, GCR)" }

func (p *GCPProvider) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{
			ID:       "gcp-settings",
			Label:    "GCP Settings",
			Icon:     "cloud",
			Category: "cloud-providers",
		},
	}
}

func (p *GCPProvider) Dependencies() []plugin.PluginDependency { return nil }
func (p *GCPProvider) OnEnable(_ plugin.PluginContext) error    { return nil }
func (p *GCPProvider) OnDisable(_ plugin.PluginContext) error   { return nil }

func (p *GCPProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/providers/gcp/status", p.handleStatus)
	mux.HandleFunc("/api/v1/providers/gcp/regions", p.handleListRegions)
}

func (p *GCPProvider) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"provider":"gcp","status":"available","version":"1.0.0"}`))
}

func (p *GCPProvider) handleListRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"regions":["us-central1","us-east1","europe-west1","asia-east1"]}`))
}

func (p *GCPProvider) Deploy(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceType, _ := req.Config["service_type"].(string)
	switch serviceType {
	case "cloud-run":
		return p.deployCloudRun(ctx, req)
	default:
		return p.deployGKE(ctx, req)
	}
}

func (p *GCPProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	return nil, fmt.Errorf("gcp: GetDeploymentStatus not yet implemented for deploy %s", deployID)
}

func (p *GCPProvider) Rollback(ctx context.Context, deployID string) error {
	return fmt.Errorf("gcp: Rollback not yet implemented for deploy %s", deployID)
}

func (p *GCPProvider) TestConnection(ctx context.Context, config map[string]any) (*provider.ConnectionResult, error) {
	return nil, fmt.Errorf("gcp: TestConnection not yet implemented")
}

func (p *GCPProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	return nil, fmt.Errorf("gcp: GetMetrics not yet implemented for deploy %s", deployID)
}
