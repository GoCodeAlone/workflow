package module

import (
	"context"
	"testing"
)

func TestOTelTracingName(t *testing.T) {
	o := NewOTelTracing("otel-test")
	if o.Name() != "otel-test" {
		t.Errorf("expected name 'otel-test', got %q", o.Name())
	}
}

func TestOTelTracingModuleInterface(t *testing.T) {
	o := NewOTelTracing("otel-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := o.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := o.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "otel-test" {
		t.Errorf("expected service name 'otel-test', got %q", services[0].Name)
	}

	// Test RequiresServices
	deps := o.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestOTelTracingConfig(t *testing.T) {
	o := NewOTelTracing("otel-test")

	// Test defaults
	if o.endpoint != "localhost:4318" {
		t.Errorf("expected default endpoint 'localhost:4318', got %q", o.endpoint)
	}
	if o.serviceName != "workflow" {
		t.Errorf("expected default service name 'workflow', got %q", o.serviceName)
	}

	// Test setters
	o.SetEndpoint("otel-collector:4318")
	if o.endpoint != "otel-collector:4318" {
		t.Errorf("expected endpoint 'otel-collector:4318', got %q", o.endpoint)
	}

	o.SetServiceName("my-service")
	if o.serviceName != "my-service" {
		t.Errorf("expected service name 'my-service', got %q", o.serviceName)
	}
}

func TestOTelTracingStopWithoutStart(t *testing.T) {
	o := NewOTelTracing("otel-test")
	app, _ := NewTestApplication()
	_ = o.Init(app)

	// Stop without Start should be safe
	if err := o.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
