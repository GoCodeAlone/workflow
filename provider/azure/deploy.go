package azure

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// deployAKS handles deployment to Azure Kubernetes Service.
func (p *AzureProvider) deployAKS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("azure: AKS deployment not yet implemented (resource_group=%s, image=%s, env=%s)",
		p.config.ResourceGroup, req.Image, req.Environment)
}

// deployACI handles deployment to Azure Container Instances.
func (p *AzureProvider) deployACI(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("azure: ACI deployment not yet implemented (resource_group=%s, image=%s, env=%s)",
		p.config.ResourceGroup, req.Image, req.Environment)
}
