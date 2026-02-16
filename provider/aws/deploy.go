package aws

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/provider"
)

// deployECS handles deployment to AWS ECS (Fargate or EC2 launch type).
func (p *AWSProvider) deployECS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("aws: ECS deployment not yet implemented (cluster=%s, image=%s, env=%s)",
		p.config.ECSCluster, req.Image, req.Environment)
}

// deployEKS handles deployment to AWS EKS (Elastic Kubernetes Service).
func (p *AWSProvider) deployEKS(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("aws: EKS deployment not yet implemented (image=%s, env=%s)",
		req.Image, req.Environment)
}
