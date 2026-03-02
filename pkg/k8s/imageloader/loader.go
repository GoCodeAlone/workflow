package imageloader

import "fmt"

// Runtime identifies a Kubernetes cluster runtime.
type Runtime string

const (
	RuntimeMinikube      Runtime = "minikube"
	RuntimeKind          Runtime = "kind"
	RuntimeDockerDesktop Runtime = "docker-desktop"
	RuntimeK3d           Runtime = "k3d"
	RuntimeRemote        Runtime = "remote"
)

// LoadConfig holds configuration for loading an image into a cluster.
type LoadConfig struct {
	Image         string  // full image:tag reference
	Runtime       Runtime // target runtime
	Registry      string  // only used for RuntimeRemote (e.g. "ghcr.io/org")
	Cluster       string  // cluster name extracted from context
	ResolvedImage string  // set by RemoteLoader after push (registry-qualified name)
}

// ImageLoader loads a Docker image into a specific Kubernetes runtime.
type ImageLoader interface {
	Type() Runtime
	Validate() error
	Load(cfg *LoadConfig) error
}

// Registry holds registered image loaders keyed by runtime.
type Registry struct {
	loaders map[Runtime]ImageLoader
}

// NewRegistry creates an empty image loader registry.
func NewRegistry() *Registry {
	return &Registry{loaders: make(map[Runtime]ImageLoader)}
}

// Register adds an image loader to the registry.
func (r *Registry) Register(l ImageLoader) {
	if l != nil {
		r.loaders[l.Type()] = l
	}
}

// Load dispatches to the appropriate loader based on cfg.Runtime.
func (r *Registry) Load(cfg *LoadConfig) error {
	l, ok := r.loaders[cfg.Runtime]
	if !ok {
		return fmt.Errorf("no image loader registered for runtime %q", cfg.Runtime)
	}
	if err := l.Validate(); err != nil {
		return fmt.Errorf("runtime %q: %w", cfg.Runtime, err)
	}
	return l.Load(cfg)
}
