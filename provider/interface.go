package provider

import (
	"context"
	"time"

	"github.com/GoCodeAlone/workflow/plugin"
)

// CloudProvider extends NativePlugin with deployment, container registry,
// connectivity testing, and monitoring capabilities for a cloud platform.
type CloudProvider interface {
	plugin.NativePlugin

	// Deployment
	Deploy(ctx context.Context, req DeployRequest) (*DeployResult, error)
	GetDeploymentStatus(ctx context.Context, deployID string) (*DeployStatus, error)
	Rollback(ctx context.Context, deployID string) error

	// Container Registry
	PushImage(ctx context.Context, image string, auth RegistryAuth) error
	PullImage(ctx context.Context, image string, auth RegistryAuth) error
	ListImages(ctx context.Context, repo string) ([]ImageTag, error)

	// Connectivity
	TestConnection(ctx context.Context, config map[string]any) (*ConnectionResult, error)

	// Monitoring
	GetMetrics(ctx context.Context, deployID string, window time.Duration) (*Metrics, error)
}
