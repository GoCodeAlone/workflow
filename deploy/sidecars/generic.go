package sidecars

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
)

// GenericProvider implements deploy.SidecarProvider for arbitrary container sidecars.
type GenericProvider struct{}

// NewGeneric creates a new generic sidecar provider.
func NewGeneric() *GenericProvider {
	return &GenericProvider{}
}

func (p *GenericProvider) Type() string { return "sidecar.generic" }

func (p *GenericProvider) Validate(cfg config.SidecarConfig) error {
	if cfg.Config == nil {
		return fmt.Errorf("generic sidecar requires config")
	}
	if _, ok := cfg.Config["image"]; !ok {
		return fmt.Errorf("generic sidecar requires 'image' in config")
	}
	return nil
}

func (p *GenericProvider) Resolve(cfg config.SidecarConfig, platform string) (*deploy.SidecarSpec, error) {
	spec := &deploy.SidecarSpec{Name: cfg.Name}

	image, _ := cfg.Config["image"].(string)

	// Extract optional fields
	var command []string
	if cmdRaw, ok := cfg.Config["command"].([]any); ok {
		for _, c := range cmdRaw {
			if s, ok := c.(string); ok {
				command = append(command, s)
			}
		}
	}

	var args []string
	if argsRaw, ok := cfg.Config["args"].([]any); ok {
		for _, a := range argsRaw {
			if s, ok := a.(string); ok {
				args = append(args, s)
			}
		}
	}

	env := make(map[string]string)
	if envRaw, ok := cfg.Config["env"].(map[string]any); ok {
		for k, v := range envRaw {
			if s, ok := v.(string); ok {
				env[k] = s
			}
		}
	}

	var ports []int32
	if portsRaw, ok := cfg.Config["ports"].([]any); ok {
		for _, p := range portsRaw {
			switch v := p.(type) {
			case float64:
				ports = append(ports, int32(v))
			case int:
				ports = append(ports, int32(v)) //nolint:gosec // G115 — port value bounded by config validation
			}
		}
	}

	switch platform {
	case "kubernetes":
		spec.K8s = &deploy.K8sSidecarSpec{
			Image:   image,
			Command: command,
			Args:    args,
			Env:     env,
			Ports:   ports,
		}
	case "ecs":
		spec.ECS = &deploy.ECSSidecarSpec{
			Image:        image,
			Command:      command,
			Env:          env,
			Essential:    false,
			PortMappings: ports,
		}
	case "docker-compose":
		var portStrings []string
		for _, p := range ports {
			portStrings = append(portStrings, fmt.Sprintf("%d:%d", p, p))
		}
		spec.Compose = &deploy.ComposeSidecarSpec{
			Image:       image,
			Environment: env,
			Ports:       portStrings,
		}
	default:
		return nil, fmt.Errorf("unsupported platform %q for generic sidecar", platform)
	}

	return spec, nil
}
