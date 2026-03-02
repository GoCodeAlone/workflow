package sidecars

import (
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
)

// TailscaleProvider implements deploy.SidecarProvider for Tailscale sidecars.
type TailscaleProvider struct{}

// NewTailscale creates a new Tailscale sidecar provider.
func NewTailscale() *TailscaleProvider {
	return &TailscaleProvider{}
}

func (p *TailscaleProvider) Type() string { return "sidecar.tailscale" }

func (p *TailscaleProvider) Validate(cfg config.SidecarConfig) error {
	if cfg.Config == nil {
		return fmt.Errorf("tailscale sidecar requires config")
	}
	if _, ok := cfg.Config["hostname"]; !ok {
		return fmt.Errorf("tailscale sidecar requires 'hostname' in config")
	}
	return nil
}

func (p *TailscaleProvider) Resolve(cfg config.SidecarConfig, platform string) (*deploy.SidecarSpec, error) {
	spec := &deploy.SidecarSpec{Name: cfg.Name}

	hostname, _ := cfg.Config["hostname"].(string)
	authKeySecret, _ := cfg.Config["auth_key_secret"].(string)
	if authKeySecret == "" {
		authKeySecret = "tailscale-auth"
	}
	image, _ := cfg.Config["image"].(string)
	if image == "" {
		image = "ghcr.io/tailscale/tailscale:latest"
	}

	// Extract serve config
	var servePort, backendPort int32
	if serveRaw, ok := cfg.Config["serve"].(map[string]any); ok {
		if p, ok := serveRaw["port"].(float64); ok {
			servePort = int32(p)
		} else if p, ok := serveRaw["port"].(int); ok {
			servePort = int32(p)
		}
		if p, ok := serveRaw["backend_port"].(float64); ok {
			backendPort = int32(p)
		} else if p, ok := serveRaw["backend_port"].(int); ok {
			backendPort = int32(p)
		}
	}
	if servePort == 0 {
		servePort = 443
	}
	if backendPort == 0 {
		backendPort = 8080
	}

	switch platform {
	case "kubernetes":
		spec.K8s = p.resolveK8s(hostname, authKeySecret, image, servePort, backendPort)
	case "ecs":
		spec.ECS = p.resolveECS(hostname, image, servePort)
	case "docker-compose":
		spec.Compose = p.resolveCompose(hostname, image, servePort)
	default:
		return nil, fmt.Errorf("unsupported platform %q for tailscale sidecar", platform)
	}

	return spec, nil
}

func (p *TailscaleProvider) resolveK8s(hostname, authKeySecret, image string, servePort, backendPort int32) *deploy.K8sSidecarSpec {
	// Build TS_SERVE_CONFIG JSON for ConfigMap
	useHTTPS := servePort == 443
	tcpConfig := map[string]any{}
	if useHTTPS {
		tcpConfig[fmt.Sprintf("%d", servePort)] = map[string]any{"HTTPS": true}
	}
	webKey := fmt.Sprintf("${TS_CERT_DOMAIN}:%d", servePort)
	serveConfig := map[string]any{
		"Web": map[string]any{
			webKey: map[string]any{
				"Handlers": map[string]any{
					"/": map[string]any{
						"Proxy": fmt.Sprintf("http://127.0.0.1:%d", backendPort),
					},
				},
			},
		},
	}
	if useHTTPS {
		serveConfig["TCP"] = tcpConfig
	}
	serveConfigJSON, _ := json.Marshal(serveConfig)

	return &deploy.K8sSidecarSpec{
		Image:           image,
		ImagePullPolicy: "IfNotPresent",
		Env: map[string]string{
			"TS_KUBE_SECRET":  "ts-state-" + hostname,
			"TS_USERSPACE":    "true",
			"TS_HOSTNAME":     hostname,
			"TS_SERVE_CONFIG": "/etc/ts-serve/serve-config.json",
		},
		SecretEnv: []deploy.SecretEnvVar{
			{EnvName: "TS_AUTHKEY", SecretName: authKeySecret, SecretKey: "TS_AUTHKEY"},
		},
		VolumeMounts: []deploy.VolumeMount{
			{Name: "ts-serve-config", MountPath: "/etc/ts-serve", ReadOnly: true},
		},
		Volumes: []deploy.Volume{
			{Name: "ts-serve-config", ConfigMap: hostname + "-ts-serve"},
		},
		ConfigMapData: map[string]string{
			"serve-config.json": string(serveConfigJSON),
		},
		ServiceAccountName: "tailscale",
		RequiredSecrets:     []string{authKeySecret},
		SecurityContext: &deploy.SecurityContext{
			Capabilities: &deploy.Capabilities{
				Add: []string{"NET_ADMIN"},
			},
		},
	}
}

func (p *TailscaleProvider) resolveECS(hostname, image string, servePort int32) *deploy.ECSSidecarSpec {
	return &deploy.ECSSidecarSpec{
		Image:     image,
		Essential: true,
		Env: map[string]string{
			"TS_HOSTNAME":  hostname,
			"TS_STATE_DIR": "/var/lib/tailscale",
			"TS_USERSPACE": "false",
		},
		PortMappings: []int32{servePort},
	}
}

func (p *TailscaleProvider) resolveCompose(hostname, image string, servePort int32) *deploy.ComposeSidecarSpec {
	return &deploy.ComposeSidecarSpec{
		Image: image,
		Environment: map[string]string{
			"TS_HOSTNAME":  hostname,
			"TS_STATE_DIR": "/var/lib/tailscale",
			"TS_USERSPACE": "false",
		},
		Ports:   []string{fmt.Sprintf("%d:%d", servePort, servePort)},
		Volumes: []string{"tailscale-state:/var/lib/tailscale"},
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
