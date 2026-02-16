package azure

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// PushImage pushes a container image to Azure Container Registry.
func (p *AzureProvider) PushImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("azure: ACR PushImage not yet implemented (image=%s)", image)
}

// PullImage pulls a container image from Azure Container Registry.
func (p *AzureProvider) PullImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("azure: ACR PullImage not yet implemented (image=%s)", image)
}

// ListImages lists container images in an Azure Container Registry repository.
func (p *AzureProvider) ListImages(ctx context.Context, repo string) ([]provider.ImageTag, error) {
	return nil, fmt.Errorf("azure: ACR ListImages not yet implemented (repo=%s)", repo)
}
