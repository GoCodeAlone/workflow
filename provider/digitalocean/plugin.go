package digitalocean

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/provider"
)

// DOConfig holds configuration for the DigitalOcean cloud provider.
type DOConfig struct {
	APIToken string `json:"api_token" yaml:"api_token"`
	Region   string `json:"region" yaml:"region"`
}

// DOProvider implements CloudProvider for DigitalOcean.
type DOProvider struct {
	config DOConfig
}

// Compile-time interface check.
var _ provider.CloudProvider = (*DOProvider)(nil)

// NewDOProvider creates a new DOProvider with the given configuration.
func NewDOProvider(config DOConfig) *DOProvider {
	return &DOProvider{config: config}
}

func (p *DOProvider) Name() string        { return "digitalocean" }
func (p *DOProvider) Version() string     { return "1.0.0" }
func (p *DOProvider) Description() string { return "DigitalOcean Cloud Provider (DOKS, App Platform)" }

func (p *DOProvider) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{
			ID:       "digitalocean-settings",
			Label:    "DigitalOcean Settings",
			Icon:     "cloud",
			Category: "cloud-providers",
		},
	}
}

func (p *DOProvider) Dependencies() []plugin.PluginDependency { return nil }
func (p *DOProvider) OnEnable(_ plugin.PluginContext) error    { return nil }
func (p *DOProvider) OnDisable(_ plugin.PluginContext) error   { return nil }

func (p *DOProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/providers/digitalocean/status", p.handleStatus)
	mux.HandleFunc("/api/v1/providers/digitalocean/regions", p.handleListRegions)
}

func (p *DOProvider) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"provider":"digitalocean","status":"available","version":"1.0.0"}`))
}

func (p *DOProvider) handleListRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"regions":["nyc1","sfo3","ams3","sgp1"]}`))
}

func (p *DOProvider) Deploy(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceType, _ := req.Config["service_type"].(string)
	switch serviceType {
	case "app-platform":
		return p.deployAppPlatform(ctx, req)
	default:
		return p.deployDOKS(ctx, req)
	}
}

func (p *DOProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	return nil, fmt.Errorf("digitalocean: GetDeploymentStatus not yet implemented for deploy %s", deployID)
}

func (p *DOProvider) Rollback(ctx context.Context, deployID string) error {
	return fmt.Errorf("digitalocean: Rollback not yet implemented for deploy %s", deployID)
}

func (p *DOProvider) TestConnection(ctx context.Context, config map[string]any) (*provider.ConnectionResult, error) {
	return nil, fmt.Errorf("digitalocean: TestConnection not yet implemented")
}

func (p *DOProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	return nil, fmt.Errorf("digitalocean: GetMetrics not yet implemented for deploy %s", deployID)
}
