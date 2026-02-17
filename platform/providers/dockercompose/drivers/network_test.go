package drivers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestNetworkDriverResourceType(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")
	if d.ResourceType() != "docker-compose.network" {
		t.Errorf("expected %q, got %q", "docker-compose.network", d.ResourceType())
	}
}

func TestNetworkDriverCreate(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	output, err := d.Create(context.Background(), "app-net", map[string]any{
		"driver": "bridge",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if output.Name != "app-net" {
		t.Errorf("expected name %q, got %q", "app-net", output.Name)
	}
	if output.Type != "network" {
		t.Errorf("expected type %q, got %q", "network", output.Type)
	}
	if output.Status != platform.ResourceStatusActive {
		t.Errorf("expected status %q, got %q", platform.ResourceStatusActive, output.Status)
	}
}

func TestNetworkDriverRead(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	output, err := d.Read(context.Background(), "app-net")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if output.Name != "app-net" {
		t.Errorf("expected name %q, got %q", "app-net", output.Name)
	}
}

func TestNetworkDriverUpdate(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	desired := map[string]any{"driver": "overlay"}
	output, err := d.Update(context.Background(), "app-net", nil, desired)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if output.Properties["driver"] != "overlay" {
		t.Error("expected updated driver property")
	}
}

func TestNetworkDriverDelete(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")
	err := d.Delete(context.Background(), "app-net")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestNetworkDriverHealthCheck(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	hs, err := d.HealthCheck(context.Background(), "app-net")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if hs.Status != "healthy" {
		t.Errorf("expected status %q, got %q", "healthy", hs.Status)
	}
}

func TestNetworkDriverScaleNotSupported(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	_, err := d.Scale(context.Background(), "app-net", map[string]any{})
	if err == nil {
		t.Error("expected error for scaling a network")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestNetworkDriverDiff(t *testing.T) {
	d := NewNetworkDriver(&mockExecutor{}, ".")

	diffs, err := d.Diff(context.Background(), "app-net", map[string]any{
		"driver": "overlay",
		"subnet": "172.28.0.0/16",
	})
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected non-empty diff")
	}
}
