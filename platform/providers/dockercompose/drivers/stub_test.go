package drivers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestStubDriverResourceType(t *testing.T) {
	d := NewStubDriver()
	if d.ResourceType() != "docker-compose.stub" {
		t.Errorf("expected %q, got %q", "docker-compose.stub", d.ResourceType())
	}
}

func TestStubDriverCreate(t *testing.T) {
	d := NewStubDriver()

	output, err := d.Create(context.Background(), "k8s-cluster", map[string]any{
		"original_type": "kubernetes_cluster",
		"stub_reason":   "Docker Compose cannot provision a Kubernetes cluster",
		"fidelity":      "stub",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if output.Name != "k8s-cluster" {
		t.Errorf("expected name %q, got %q", "k8s-cluster", output.Name)
	}
	if output.ProviderType != "docker-compose.stub" {
		t.Errorf("expected provider type %q, got %q", "docker-compose.stub", output.ProviderType)
	}
	if output.Properties["stub"] != true {
		t.Error("expected stub property to be true")
	}
	if output.Properties["original_type"] != "kubernetes_cluster" {
		t.Error("expected original_type to be preserved")
	}
	if output.Status != platform.ResourceStatusActive {
		t.Errorf("expected status %q, got %q", platform.ResourceStatusActive, output.Status)
	}
}

func TestStubDriverRead(t *testing.T) {
	d := NewStubDriver()

	output, err := d.Read(context.Background(), "stub-resource")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if output.Properties["stub"] != true {
		t.Error("expected stub property to be true")
	}
}

func TestStubDriverUpdate(t *testing.T) {
	d := NewStubDriver()

	output, err := d.Update(context.Background(), "stub-resource", nil, map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if output.Properties["key"] != "val" {
		t.Error("expected desired properties to be returned")
	}
}

func TestStubDriverDelete(t *testing.T) {
	d := NewStubDriver()
	err := d.Delete(context.Background(), "stub-resource")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestStubDriverHealthCheck(t *testing.T) {
	d := NewStubDriver()

	hs, err := d.HealthCheck(context.Background(), "stub-resource")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if hs.Status != "healthy" {
		t.Errorf("expected status %q, got %q", "healthy", hs.Status)
	}
	if hs.Details["stub"] != true {
		t.Error("expected stub detail")
	}
}

func TestStubDriverScaleNotSupported(t *testing.T) {
	d := NewStubDriver()

	_, err := d.Scale(context.Background(), "stub-resource", map[string]any{})
	if err == nil {
		t.Error("expected error for scaling a stub resource")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestStubDriverDiff(t *testing.T) {
	d := NewStubDriver()

	diffs, err := d.Diff(context.Background(), "stub-resource", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected empty diff for stub, got %d entries", len(diffs))
	}
}
