package deploy

import (
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
)

// SidecarProvider translates platform-agnostic SidecarConfig into
// platform-specific container specs.
type SidecarProvider interface {
	// Type returns the sidecar type string (e.g., "sidecar.tailscale").
	Type() string

	// Validate checks the sidecar configuration.
	Validate(cfg config.SidecarConfig) error

	// Resolve produces a platform-specific SidecarSpec from config.
	Resolve(cfg config.SidecarConfig, platform string) (*SidecarSpec, error)
}

// SidecarSpec is the resolved, platform-specific sidecar output.
type SidecarSpec struct {
	Name    string
	K8s     *K8sSidecarSpec     `json:"k8s,omitempty"`
	ECS     *ECSSidecarSpec     `json:"ecs,omitempty"`
	Compose *ComposeSidecarSpec `json:"compose,omitempty"`
}

// K8sSidecarSpec contains Kubernetes-specific sidecar configuration.
type K8sSidecarSpec struct {
	Image              string            `json:"image"`
	Command            []string          `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	SecretEnv          []SecretEnvVar    `json:"secretEnv,omitempty"`
	Ports              []int32           `json:"ports,omitempty"`
	VolumeMounts       []VolumeMount     `json:"volumeMounts,omitempty"`
	Volumes            []Volume          `json:"volumes,omitempty"`
	ConfigMapData      map[string]string `json:"configMapData,omitempty"`
	ServiceAccountName string            `json:"serviceAccountName,omitempty"`
	RequiredSecrets    []string          `json:"requiredSecrets,omitempty"`
	SecurityContext    *SecurityContext  `json:"securityContext,omitempty"`
	ImagePullPolicy    string            `json:"imagePullPolicy,omitempty"`
}

// VolumeMount describes a volume mount for a sidecar container.
type VolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

// Volume describes a volume for a sidecar container.
type Volume struct {
	Name      string `json:"name"`
	EmptyDir  bool   `json:"emptyDir,omitempty"`
	Secret    string `json:"secret,omitempty"` //nolint:gosec // G117 — k8s secret name reference, not a secret value
	ConfigMap string `json:"configMap,omitempty"`
}

// SecurityContext holds security settings for a sidecar container.
type SecurityContext struct {
	RunAsUser    *int64        `json:"runAsUser,omitempty"`
	RunAsGroup   *int64        `json:"runAsGroup,omitempty"`
	Privileged   *bool         `json:"privileged,omitempty"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

// Capabilities describes Linux capabilities to add or drop.
type Capabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}

// SecretEnvVar describes an environment variable sourced from a Kubernetes secret.
type SecretEnvVar struct {
	EnvName    string `json:"envName"`
	SecretName string `json:"secretName"`
	SecretKey  string `json:"secretKey"`
}

// ECSSidecarSpec contains ECS-specific sidecar configuration.
type ECSSidecarSpec struct {
	Image        string            `json:"image"`
	Command      []string          `json:"command,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Essential    bool              `json:"essential"`
	PortMappings []int32           `json:"portMappings,omitempty"`
}

// ComposeSidecarSpec contains Docker Compose-specific sidecar configuration.
type ComposeSidecarSpec struct {
	Image       string            `json:"image"`
	Command     string            `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	DependsOn   []string          `json:"dependsOn,omitempty"`
}

// SidecarRegistry holds registered sidecar providers.
type SidecarRegistry struct {
	mu        sync.RWMutex
	providers map[string]SidecarProvider
}

// NewSidecarRegistry creates an empty sidecar provider registry.
func NewSidecarRegistry() *SidecarRegistry {
	return &SidecarRegistry{
		providers: make(map[string]SidecarProvider),
	}
}

// Register adds a sidecar provider to the registry.
func (r *SidecarRegistry) Register(p SidecarProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Type()] = p
}

// Get returns the sidecar provider for the given type.
func (r *SidecarRegistry) Get(typeName string) (SidecarProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[typeName]
	return p, ok
}

// Resolve resolves all sidecar configs into platform-specific specs.
func (r *SidecarRegistry) Resolve(sidecars []config.SidecarConfig, platform string) ([]*SidecarSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var specs []*SidecarSpec
	for _, sc := range sidecars {
		provider, ok := r.providers[sc.Type]
		if !ok {
			return nil, fmt.Errorf("unknown sidecar type %q for sidecar %q", sc.Type, sc.Name)
		}
		if err := provider.Validate(sc); err != nil {
			return nil, fmt.Errorf("sidecar %q: %w", sc.Name, err)
		}
		spec, err := provider.Resolve(sc, platform)
		if err != nil {
			return nil, fmt.Errorf("sidecar %q: resolve for %s: %w", sc.Name, platform, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}
