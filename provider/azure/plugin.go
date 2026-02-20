package azure

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
		return NewAzureProvider(AzureConfig{})
	})
}

// AzureConfig holds configuration for the Azure cloud provider.
type AzureConfig struct {
	SubscriptionID string `json:"subscription_id" yaml:"subscription_id"`
	TenantID       string `json:"tenant_id" yaml:"tenant_id"`
	ClientID       string `json:"client_id" yaml:"client_id"`
	ClientSecret   string `json:"client_secret" yaml:"client_secret"` //nolint:gosec // G117: Azure config field
	ResourceGroup  string `json:"resource_group" yaml:"resource_group"`
	Region         string `json:"region" yaml:"region"`
}

// AzureProvider implements CloudProvider for Microsoft Azure.
type AzureProvider struct {
	config AzureConfig
}

// Compile-time interface check.
var _ provider.CloudProvider = (*AzureProvider)(nil)

// NewAzureProvider creates a new AzureProvider with the given configuration.
func NewAzureProvider(config AzureConfig) *AzureProvider {
	return &AzureProvider{config: config}
}

func (p *AzureProvider) Name() string        { return "azure" }
func (p *AzureProvider) Version() string     { return "1.0.0" }
func (p *AzureProvider) Description() string { return "Azure Cloud Provider (AKS, ACI, ACR)" }

func (p *AzureProvider) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{
			ID:       "azure-settings",
			Label:    "Azure Settings",
			Icon:     "cloud",
			Category: "cloud-providers",
		},
	}
}

func (p *AzureProvider) Dependencies() []plugin.PluginDependency { return nil }
func (p *AzureProvider) OnEnable(_ plugin.PluginContext) error   { return nil }
func (p *AzureProvider) OnDisable(_ plugin.PluginContext) error  { return nil }

func (p *AzureProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/providers/azure/status", p.handleStatus)
	mux.HandleFunc("/api/v1/providers/azure/regions", p.handleListRegions)
}

func (p *AzureProvider) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"provider":"azure","status":"available","version":"1.0.0"}`))
}

func (p *AzureProvider) handleListRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"regions":["eastus","westus2","westeurope","southeastasia"]}`))
}

func (p *AzureProvider) Deploy(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceType, _ := req.Config["service_type"].(string)
	switch serviceType {
	case "aci":
		return p.deployACI(ctx, req)
	default:
		return p.deployAKS(ctx, req)
	}
}

func (p *AzureProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	return nil, fmt.Errorf("azure: GetDeploymentStatus not yet implemented for deploy %s", deployID)
}

func (p *AzureProvider) Rollback(ctx context.Context, deployID string) error {
	return fmt.Errorf("azure: Rollback not yet implemented for deploy %s", deployID)
}

func (p *AzureProvider) TestConnection(ctx context.Context, config map[string]any) (*provider.ConnectionResult, error) {
	return nil, fmt.Errorf("azure: TestConnection not yet implemented")
}

func (p *AzureProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	return nil, fmt.Errorf("azure: GetMetrics not yet implemented for deploy %s", deployID)
}
