package config

import (
	"strings"
	"testing"
)

func TestValidateServices_Valid(t *testing.T) {
	services := map[string]*ServiceConfig{
		"api": {
			Binary: "./cmd/api",
			Scaling: &ScalingConfig{
				Replicas: 2,
				Min:      1,
				Max:      5,
			},
			Expose: []ExposeConfig{
				{Port: 8080, Protocol: "http"},
			},
		},
	}
	if err := ValidateServices(services); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateServices_ScalingMinExceedsMax(t *testing.T) {
	services := map[string]*ServiceConfig{
		"api": {
			Scaling: &ScalingConfig{Min: 10, Max: 2},
		},
	}
	err := ValidateServices(services)
	if err == nil {
		t.Fatal("expected error for min > max")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Errorf("expected 'min' in error, got %q", err.Error())
	}
}

func TestValidateServices_InvalidPort(t *testing.T) {
	services := map[string]*ServiceConfig{
		"api": {
			Expose: []ExposeConfig{
				{Port: 0},
			},
		},
	}
	err := ValidateServices(services)
	if err == nil {
		t.Fatal("expected error for port=0")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("expected 'port' in error, got %q", err.Error())
	}
}

func TestValidateMeshRoutes_UnknownService(t *testing.T) {
	mesh := &MeshConfig{
		Routes: []MeshRouteConfig{
			{From: "api", To: "missing-service", Via: "nats"},
		},
	}
	services := map[string]*ServiceConfig{
		"api": {},
	}
	warnings := ValidateMeshRoutes(mesh, services)
	if len(warnings) == 0 {
		t.Fatal("expected warning for unknown service in mesh route")
	}
	if !strings.Contains(warnings[0], "missing-service") {
		t.Errorf("expected 'missing-service' in warning: %q", warnings[0])
	}
}

func TestValidateMeshRoutes_InvalidVia(t *testing.T) {
	mesh := &MeshConfig{
		Routes: []MeshRouteConfig{
			{From: "api", To: "worker", Via: "kafka"},
		},
	}
	warnings := ValidateMeshRoutes(mesh, nil)
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid via transport")
	}
	if !strings.Contains(warnings[0], "kafka") {
		t.Errorf("expected 'kafka' in warning: %q", warnings[0])
	}
}

func TestValidateMeshRoutes_Nil(t *testing.T) {
	warnings := ValidateMeshRoutes(nil, nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for nil mesh, got %v", warnings)
	}
}

func TestValidateNetworking_UnknownService(t *testing.T) {
	networking := &NetworkingConfig{
		Ingress: []IngressConfig{
			{Service: "unknown", Port: 8080},
		},
	}
	services := map[string]*ServiceConfig{
		"api": {},
	}
	err := ValidateNetworking(networking, services)
	if err == nil {
		t.Fatal("expected error for unknown service in ingress")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected 'unknown' in error: %q", err.Error())
	}
}

func TestValidateNetworking_PortNotExposed(t *testing.T) {
	networking := &NetworkingConfig{
		Ingress: []IngressConfig{
			{Service: "api", Port: 9999},
		},
	}
	services := map[string]*ServiceConfig{
		"api": {
			Expose: []ExposeConfig{
				{Port: 8080},
			},
		},
	}
	err := ValidateNetworking(networking, services)
	if err == nil {
		t.Fatal("expected error for port not exposed by service")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("expected '9999' in error: %q", err.Error())
	}
}

func TestValidateNetworking_InvalidTLSProvider(t *testing.T) {
	networking := &NetworkingConfig{
		Ingress: []IngressConfig{
			{
				Service: "api",
				Port:    8080,
				TLS:     &TLSConfig{Provider: "badprovider"},
			},
		},
	}
	err := ValidateNetworking(networking, nil)
	if err == nil {
		t.Fatal("expected error for invalid TLS provider")
	}
	if !strings.Contains(err.Error(), "badprovider") {
		t.Errorf("expected 'badprovider' in error: %q", err.Error())
	}
}

func TestValidateNetworking_ValidIngress(t *testing.T) {
	networking := &NetworkingConfig{
		Ingress: []IngressConfig{
			{
				Service: "api",
				Port:    8080,
				TLS:     &TLSConfig{Provider: "letsencrypt"},
			},
		},
	}
	services := map[string]*ServiceConfig{
		"api": {
			Expose: []ExposeConfig{{Port: 8080}},
		},
	}
	if err := ValidateNetworking(networking, services); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateSecurity_InvalidTLSProvider(t *testing.T) {
	sec := &SecurityConfig{
		TLS: &SecurityTLSConfig{Provider: "unknown-provider"},
	}
	err := ValidateSecurity(sec)
	if err == nil {
		t.Fatal("expected error for invalid security TLS provider")
	}
	if !strings.Contains(err.Error(), "unknown-provider") {
		t.Errorf("expected 'unknown-provider' in error: %q", err.Error())
	}
}

func TestValidateSecurity_ValidProvider(t *testing.T) {
	for _, p := range []string{"letsencrypt", "manual", "acm", "cloudflare"} {
		sec := &SecurityConfig{
			TLS: &SecurityTLSConfig{Provider: p},
		}
		if err := ValidateSecurity(sec); err != nil {
			t.Errorf("provider %q: unexpected error: %v", p, err)
		}
	}
}

func TestCrossValidate_UnroutedPort(t *testing.T) {
	cfg := &WorkflowConfig{
		Services: map[string]*ServiceConfig{
			"api": {
				Expose: []ExposeConfig{{Port: 8080}},
			},
		},
		Networking: &NetworkingConfig{
			Ingress: []IngressConfig{
				{Service: "api", Port: 9090}, // different port
			},
		},
	}
	warnings := CrossValidate(cfg)
	if len(warnings) == 0 {
		t.Fatal("expected cross-validate warning for unrouted port")
	}
	if !strings.Contains(warnings[0], "8080") {
		t.Errorf("expected '8080' in warning: %q", warnings[0])
	}
}

func TestCrossValidate_AllRouted(t *testing.T) {
	cfg := &WorkflowConfig{
		Services: map[string]*ServiceConfig{
			"api": {
				Expose: []ExposeConfig{{Port: 8080}},
			},
		},
		Networking: &NetworkingConfig{
			Ingress: []IngressConfig{
				{Service: "api", Port: 8080},
			},
		},
	}
	warnings := CrossValidate(cfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}
