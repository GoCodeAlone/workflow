package digitalocean

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// PushImage pushes a container image to DigitalOcean Container Registry.
func (p *DOProvider) PushImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("digitalocean: DOCR PushImage not yet implemented (image=%s)", image)
}

// PullImage pulls a container image from DigitalOcean Container Registry.
func (p *DOProvider) PullImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("digitalocean: DOCR PullImage not yet implemented (image=%s)", image)
}

// ListImages lists container images in a DigitalOcean Container Registry repository.
func (p *DOProvider) ListImages(ctx context.Context, repo string) ([]provider.ImageTag, error) {
	return nil, fmt.Errorf("digitalocean: DOCR ListImages not yet implemented (repo=%s)", repo)
}
