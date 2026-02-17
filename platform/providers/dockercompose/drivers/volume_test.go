package drivers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestVolumeDriverResourceType(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")
	if d.ResourceType() != "docker-compose.volume" {
		t.Errorf("expected %q, got %q", "docker-compose.volume", d.ResourceType())
	}
}

func TestVolumeDriverCreate(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	output, err := d.Create(context.Background(), "db-data", map[string]any{
		"driver": "local",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if output.Name != "db-data" {
		t.Errorf("expected name %q, got %q", "db-data", output.Name)
	}
	if output.Type != "persistent_volume" {
		t.Errorf("expected type %q, got %q", "persistent_volume", output.Type)
	}
	if output.Status != platform.ResourceStatusActive {
		t.Errorf("expected status %q, got %q", platform.ResourceStatusActive, output.Status)
	}
}

func TestVolumeDriverRead(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	output, err := d.Read(context.Background(), "db-data")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if output.Name != "db-data" {
		t.Errorf("expected name %q, got %q", "db-data", output.Name)
	}
}

func TestVolumeDriverUpdate(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	desired := map[string]any{"driver": "nfs"}
	output, err := d.Update(context.Background(), "db-data", nil, desired)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if output.Properties["driver"] != "nfs" {
		t.Error("expected updated driver property")
	}
}

func TestVolumeDriverDelete(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")
	err := d.Delete(context.Background(), "db-data")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestVolumeDriverHealthCheck(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	hs, err := d.HealthCheck(context.Background(), "db-data")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if hs.Status != "healthy" {
		t.Errorf("expected status %q, got %q", "healthy", hs.Status)
	}
}

func TestVolumeDriverScaleNotSupported(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	_, err := d.Scale(context.Background(), "db-data", map[string]any{})
	if err == nil {
		t.Error("expected error for scaling a volume")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestVolumeDriverDiff(t *testing.T) {
	d := NewVolumeDriver(&mockExecutor{}, ".")

	diffs, err := d.Diff(context.Background(), "db-data", map[string]any{
		"driver": "nfs",
	})
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected non-empty diff")
	}
}
