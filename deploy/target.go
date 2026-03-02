package deploy

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/manifest"
)

// DeployTarget is the interface that platform-specific deployers implement.
// Each target (kubernetes, ecs, docker-compose) provides Generate, Apply,
// Destroy, Status, Diff, and Logs operations.
type DeployTarget interface {
	// Name returns the target identifier (e.g., "kubernetes", "ecs", "docker-compose").
	Name() string

	// Generate produces platform-specific deployment artifacts from config.
	Generate(ctx context.Context, req *DeployRequest) (*DeployArtifacts, error)

	// Apply deploys artifacts to the target platform.
	Apply(ctx context.Context, artifacts *DeployArtifacts, opts ApplyOpts) (*DeployResult, error)

	// Destroy tears down all resources for the given app.
	Destroy(ctx context.Context, appName, namespace string) error

	// Status returns current deployment status.
	Status(ctx context.Context, appName, namespace string) (*DeployStatus, error)

	// Diff compares generated artifacts against live state.
	Diff(ctx context.Context, artifacts *DeployArtifacts) (string, error)

	// Logs streams logs from the deployed app.
	Logs(ctx context.Context, appName, namespace string, opts LogOpts) (io.ReadCloser, error)
}

// DeployRequest bundles everything a DeployTarget needs to generate or apply.
type DeployRequest struct {
	Config    *config.WorkflowConfig
	Manifest  *manifest.WorkflowManifest
	Sidecars  []*SidecarSpec
	Image     string
	Namespace string
	AppName   string
	Replicas  int
	ExtraEnv  map[string]string
	SecretRef string
	OutputDir string

	// Command overrides the container entrypoint.
	Command []string
	// Args overrides the default container arguments.
	Args []string
	// ImagePullPolicy sets the container image pull policy ("Never", "Always", "IfNotPresent").
	ImagePullPolicy string
	// Strategy sets the deployment strategy ("Recreate" or "RollingUpdate").
	Strategy string
	// ServiceAccount sets the pod service account name.
	ServiceAccount string
	// HealthPath overrides the default health check path ("/healthz").
	HealthPath string
	// ConfigMapName overrides the configmap name (default: AppName).
	ConfigMapName string
	// RunAsUser sets the pod security context runAsUser.
	RunAsUser *int64
	// RunAsNonRoot sets the pod security context runAsNonRoot.
	RunAsNonRoot *bool
	// FSGroup sets the pod security context fsGroup.
	FSGroup *int64
	// ConfigFileData is raw config YAML bytes (with env vars expanded).
	// If set, used for ConfigMap instead of marshaling Config.
	ConfigFileData []byte
}

// DeployTargetRegistry holds registered deploy targets.
type DeployTargetRegistry struct {
	mu      sync.RWMutex
	targets map[string]DeployTarget
}

// NewDeployTargetRegistry creates an empty deploy target registry.
func NewDeployTargetRegistry() *DeployTargetRegistry {
	return &DeployTargetRegistry{
		targets: make(map[string]DeployTarget),
	}
}

// Register adds a deploy target to the registry.
func (r *DeployTargetRegistry) Register(t DeployTarget) {
	if t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets[t.Name()] = t
}

// Get returns the deploy target with the given name.
func (r *DeployTargetRegistry) Get(name string) (DeployTarget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.targets[name]
	return t, ok
}

// List returns sorted names of all registered deploy targets.
func (r *DeployTargetRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.targets))
	for name := range r.targets {
		names = append(names, name)
	}
	return names
}

// Generate is a convenience method that looks up a target and generates artifacts.
func (r *DeployTargetRegistry) Generate(ctx context.Context, targetName string, req *DeployRequest) (*DeployArtifacts, error) {
	t, ok := r.Get(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown deploy target %q", targetName)
	}
	return t.Generate(ctx, req)
}
