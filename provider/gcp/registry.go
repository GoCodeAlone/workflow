package gcp

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// PushImage pushes a container image to Google Container Registry / Artifact Registry.
func (p *GCPProvider) PushImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("gcp: GCR PushImage not yet implemented (image=%s)", image)
}

// PullImage pulls a container image from Google Container Registry / Artifact Registry.
func (p *GCPProvider) PullImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("gcp: GCR PullImage not yet implemented (image=%s)", image)
}

// ListImages lists container images in a Google Container Registry / Artifact Registry repository.
func (p *GCPProvider) ListImages(ctx context.Context, repo string) ([]provider.ImageTag, error) {
	return nil, fmt.Errorf("gcp: GCR ListImages not yet implemented (repo=%s)", repo)
}
