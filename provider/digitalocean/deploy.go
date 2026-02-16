package digitalocean

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// deployDOKS handles deployment to DigitalOcean Kubernetes (DOKS).
func (p *DOProvider) deployDOKS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("digitalocean: DOKS deployment not yet implemented (region=%s, image=%s, env=%s)",
		p.config.Region, req.Image, req.Environment)
}

// deployAppPlatform handles deployment to DigitalOcean App Platform.
func (p *DOProvider) deployAppPlatform(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("digitalocean: App Platform deployment not yet implemented (region=%s, image=%s, env=%s)",
		p.config.Region, req.Image, req.Environment)
}
