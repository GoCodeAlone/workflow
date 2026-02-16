package gcp

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// deployGKE handles deployment to Google Kubernetes Engine.
func (p *GCPProvider) deployGKE(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("gcp: GKE deployment not yet implemented (project=%s, image=%s, env=%s)",
		p.config.ProjectID, req.Image, req.Environment)
}

// deployCloudRun handles deployment to Google Cloud Run.
func (p *GCPProvider) deployCloudRun(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("gcp: Cloud Run deployment not yet implemented (project=%s, image=%s, env=%s)",
		p.config.ProjectID, req.Image, req.Environment)
}
