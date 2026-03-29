package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestCollectExposedServices_MultiService(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api": {
				Expose: []config.ExposeConfig{
					{Port: 8080, Protocol: "http"},
				},
			},
			"grpc": {
				Expose: []config.ExposeConfig{
					{Port: 9090, Protocol: "grpc"},
				},
			},
		},
	}
	services := collectExposedServices(cfg)
	if len(services) != 2 {
		t.Fatalf("expected 2 exposed services, got %d: %+v", len(services), services)
	}
}

func TestCollectExposedServices_SingleService(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "api",
				Type:   "http.server",
				Config: map[string]any{"address": ":8080"},
			},
		},
	}
	services := collectExposedServices(cfg)
	if len(services) != 1 {
		t.Fatalf("expected 1 exposed service, got %d: %+v", len(services), services)
	}
	if services[0].Port != 8080 {
		t.Errorf("expected port 8080, got %d", services[0].Port)
	}
	if services[0].Name != "app" {
		t.Errorf("expected name 'app', got %q", services[0].Name)
	}
}

func TestCollectExposedServices_NoModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "database.postgres"},
		},
	}
	services := collectExposedServices(cfg)
	if len(services) != 0 {
		t.Errorf("expected 0 exposed services for non-http modules, got %d", len(services))
	}
}

func TestCollectExposedServices_NilService(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api": nil,
		},
	}
	services := collectExposedServices(cfg)
	if len(services) != 0 {
		t.Errorf("expected 0 services when service config is nil, got %d", len(services))
	}
}

func TestExposeNgrok_NoBinary(t *testing.T) {
	// ngrok is unlikely to be installed in CI; verify we get a clear error.
	t.Setenv("PATH", "/nonexistent")
	err := exposeNgrok([]ExposedService{{Name: "app", Port: 8080}})
	if err == nil {
		t.Fatal("expected error when ngrok binary not found")
	}
}

func TestExposeTailscale_NoBinary(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	err := exposeTailscale([]ExposedService{{Name: "app", Port: 8080}}, nil)
	if err == nil {
		t.Fatal("expected error when tailscale binary not found")
	}
}

func TestExposeCloudflare_NoBinary(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	err := exposeCloudflare([]ExposedService{{Name: "app", Port: 8080}}, nil)
	if err == nil {
		t.Fatal("expected error when cloudflared binary not found")
	}
}

func TestExposeNgrok_EmptyServices(t *testing.T) {
	err := exposeNgrok(nil)
	if err == nil {
		t.Fatal("expected error when no services provided")
	}
}

func TestExposeTailscale_EmptyServices(t *testing.T) {
	err := exposeTailscale(nil, nil)
	if err == nil {
		t.Fatal("expected error when no services provided")
	}
}

func TestExposeCloudflare_EmptyServices(t *testing.T) {
	err := exposeCloudflare(nil, nil)
	if err == nil {
		t.Fatal("expected error when no services provided")
	}
}
