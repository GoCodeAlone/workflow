package sidecars

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestGenericProvider_Type(t *testing.T) {
	p := NewGeneric()
	if p.Type() != "sidecar.generic" {
		t.Errorf("got type %q, want %q", p.Type(), "sidecar.generic")
	}
}

func TestGenericProvider_ValidateOK(t *testing.T) {
	p := NewGeneric()
	cfg := config.SidecarConfig{
		Name:   "test",
		Type:   "sidecar.generic",
		Config: map[string]any{"image": "nginx:latest"},
	}
	if err := p.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenericProvider_ValidateMissingImage(t *testing.T) {
	p := NewGeneric()
	cfg := config.SidecarConfig{
		Name:   "test",
		Type:   "sidecar.generic",
		Config: map[string]any{},
	}
	if err := p.Validate(cfg); err == nil {
		t.Error("expected error for missing image")
	}
}

func TestGenericProvider_ResolveKubernetes(t *testing.T) {
	p := NewGeneric()
	cfg := config.SidecarConfig{
		Name: "otel",
		Type: "sidecar.generic",
		Config: map[string]any{
			"image":   "otel/opentelemetry-collector:latest",
			"command": []any{"/otelcol"},
			"args":    []any{"--config", "/etc/otel/config.yaml"},
			"env": map[string]any{
				"OTEL_LOG_LEVEL": "debug",
			},
			"ports": []any{float64(4317), float64(4318)},
		},
	}

	spec, err := p.Resolve(cfg, "kubernetes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.K8s == nil {
		t.Fatal("expected K8s spec")
	}
	if spec.K8s.Image != "otel/opentelemetry-collector:latest" {
		t.Errorf("got image %q", spec.K8s.Image)
	}
	if len(spec.K8s.Command) != 1 || spec.K8s.Command[0] != "/otelcol" {
		t.Errorf("unexpected command: %v", spec.K8s.Command)
	}
	if len(spec.K8s.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(spec.K8s.Args))
	}
	if spec.K8s.Env["OTEL_LOG_LEVEL"] != "debug" {
		t.Errorf("unexpected env: %v", spec.K8s.Env)
	}
	if len(spec.K8s.Ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(spec.K8s.Ports))
	}
}

func TestGenericProvider_ResolveECS(t *testing.T) {
	p := NewGeneric()
	cfg := config.SidecarConfig{
		Name:   "test",
		Type:   "sidecar.generic",
		Config: map[string]any{"image": "nginx:latest"},
	}
	spec, err := p.Resolve(cfg, "ecs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ECS == nil {
		t.Fatal("expected ECS spec")
	}
	if spec.ECS.Essential {
		t.Error("expected essential=false for generic sidecar")
	}
}

func TestGenericProvider_ResolveUnsupportedPlatform(t *testing.T) {
	p := NewGeneric()
	cfg := config.SidecarConfig{
		Name:   "test",
		Type:   "sidecar.generic",
		Config: map[string]any{"image": "nginx"},
	}
	_, err := p.Resolve(cfg, "unknown")
	if err == nil {
		t.Error("expected error for unsupported platform")
	}
}
