package sidecars

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestTailscaleProvider_Type(t *testing.T) {
	p := NewTailscale()
	if p.Type() != "sidecar.tailscale" {
		t.Errorf("got type %q, want %q", p.Type(), "sidecar.tailscale")
	}
}

func TestTailscaleProvider_ValidateOK(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name: "ts",
		Type: "sidecar.tailscale",
		Config: map[string]any{
			"hostname": "my-app",
		},
	}
	if err := p.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTailscaleProvider_ValidateMissingHostname(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name:   "ts",
		Type:   "sidecar.tailscale",
		Config: map[string]any{},
	}
	if err := p.Validate(cfg); err == nil {
		t.Error("expected error for missing hostname")
	}
}

func TestTailscaleProvider_ValidateNilConfig(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{Name: "ts", Type: "sidecar.tailscale"}
	if err := p.Validate(cfg); err == nil {
		t.Error("expected error for nil config")
	}
}

func TestTailscaleProvider_ResolveKubernetes(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name: "tailscale",
		Type: "sidecar.tailscale",
		Config: map[string]any{
			"hostname": "my-app",
			"serve": map[string]any{
				"port":         float64(443),
				"backend_port": float64(8080),
			},
		},
	}

	spec, err := p.Resolve(cfg, "kubernetes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Name != "tailscale" {
		t.Errorf("got name %q, want %q", spec.Name, "tailscale")
	}
	if spec.K8s == nil {
		t.Fatal("expected K8s spec")
	}
	if spec.K8s.Image == "" {
		t.Error("expected non-empty image")
	}
	if spec.K8s.Env["TS_HOSTNAME"] != "my-app" {
		t.Errorf("got TS_HOSTNAME %q, want %q", spec.K8s.Env["TS_HOSTNAME"], "my-app")
	}
	if len(spec.K8s.VolumeMounts) == 0 {
		t.Error("expected at least one volume mount")
	}
	if len(spec.K8s.ConfigMapData) == 0 {
		t.Error("expected configmap data for serve config")
	}
	if len(spec.K8s.RequiredSecrets) == 0 {
		t.Error("expected at least one required secret")
	}
	if len(spec.K8s.SecretEnv) == 0 {
		t.Error("expected secret env vars for TS_AUTHKEY")
	}
	if spec.K8s.ServiceAccountName != "tailscale" {
		t.Errorf("got service account %q, want %q", spec.K8s.ServiceAccountName, "tailscale")
	}
	if spec.K8s.SecurityContext == nil || spec.K8s.SecurityContext.Capabilities == nil {
		t.Fatal("expected security context with capabilities")
	}
	if len(spec.K8s.SecurityContext.Capabilities.Add) == 0 {
		t.Error("expected NET_ADMIN capability")
	}
	if spec.K8s.Env["TS_USERSPACE"] != "true" {
		t.Errorf("got TS_USERSPACE %q, want %q", spec.K8s.Env["TS_USERSPACE"], "true")
	}
	if spec.K8s.Env["TS_KUBE_SECRET"] != "ts-state-my-app" {
		t.Errorf("got TS_KUBE_SECRET %q, want %q", spec.K8s.Env["TS_KUBE_SECRET"], "ts-state-my-app")
	}
}

func TestTailscaleProvider_ResolveECS(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name:   "ts",
		Type:   "sidecar.tailscale",
		Config: map[string]any{"hostname": "my-app"},
	}

	spec, err := p.Resolve(cfg, "ecs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ECS == nil {
		t.Fatal("expected ECS spec")
	}
	if !spec.ECS.Essential {
		t.Error("expected essential=true for tailscale ECS sidecar")
	}
}

func TestTailscaleProvider_ResolveCompose(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name:   "ts",
		Type:   "sidecar.tailscale",
		Config: map[string]any{"hostname": "my-app"},
	}

	spec, err := p.Resolve(cfg, "docker-compose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Compose == nil {
		t.Fatal("expected Compose spec")
	}
}

func TestTailscaleProvider_ResolveUnsupportedPlatform(t *testing.T) {
	p := NewTailscale()
	cfg := config.SidecarConfig{
		Name:   "ts",
		Type:   "sidecar.tailscale",
		Config: map[string]any{"hostname": "my-app"},
	}
	_, err := p.Resolve(cfg, "unknown-platform")
	if err == nil {
		t.Error("expected error for unsupported platform")
	}
}
