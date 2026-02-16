package aws

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// PushImage pushes a container image to Amazon ECR.
func (p *AWSProvider) PushImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("aws: ECR PushImage not yet implemented (image=%s)", image)
}

// PullImage pulls a container image from Amazon ECR.
func (p *AWSProvider) PullImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return fmt.Errorf("aws: ECR PullImage not yet implemented (image=%s)", image)
}

// ListImages lists container images in an Amazon ECR repository.
func (p *AWSProvider) ListImages(ctx context.Context, repo string) ([]provider.ImageTag, error) {
	return nil, fmt.Errorf("aws: ECR ListImages not yet implemented (repo=%s)", repo)
}
