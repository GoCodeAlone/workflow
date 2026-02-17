package drivers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockExecutor struct {
	psFn func(ctx context.Context, projectDir string, files ...string) (string, error)
}

func (m *mockExecutor) Ps(ctx context.Context, projectDir string, files ...string) (string, error) {
	if m.psFn != nil {
		return m.psFn(ctx, projectDir, files...)
	}
	return "", nil
}

func TestServiceDriverResourceType(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")
	if d.ResourceType() != "docker-compose.service" {
		t.Errorf("expected %q, got %q", "docker-compose.service", d.ResourceType())
	}
}

func TestServiceDriverCreate(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	output, err := d.Create(context.Background(), "web", map[string]any{
		"image":    "nginx:latest",
		"replicas": 3,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if output.Name != "web" {
		t.Errorf("expected name %q, got %q", "web", output.Name)
	}
	if output.Status != platform.ResourceStatusActive {
		t.Errorf("expected status %q, got %q", platform.ResourceStatusActive, output.Status)
	}
	if output.ProviderType != "docker-compose.service" {
		t.Errorf("expected provider type %q, got %q", "docker-compose.service", output.ProviderType)
	}
}

func TestServiceDriverRead(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	output, err := d.Read(context.Background(), "web")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if output.Name != "web" {
		t.Errorf("expected name %q, got %q", "web", output.Name)
	}
}

func TestServiceDriverUpdate(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	desired := map[string]any{"image": "nginx:v2", "replicas": 5}
	output, err := d.Update(context.Background(), "web", nil, desired)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if output.Properties["image"] != "nginx:v2" {
		t.Errorf("expected updated image")
	}
}

func TestServiceDriverDelete(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")
	err := d.Delete(context.Background(), "web")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestServiceDriverHealthCheckHealthy(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{
		psFn: func(ctx context.Context, projectDir string, files ...string) (string, error) {
			return `[{"Name":"web","State":"running"}]`, nil
		},
	}, ".")

	hs, err := d.HealthCheck(context.Background(), "web")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if hs.Status != "healthy" {
		t.Errorf("expected status %q, got %q", "healthy", hs.Status)
	}
}

func TestServiceDriverHealthCheckNotFound(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{
		psFn: func(ctx context.Context, projectDir string, files ...string) (string, error) {
			return `[{"Name":"other","State":"running"}]`, nil
		},
	}, ".")

	hs, err := d.HealthCheck(context.Background(), "web")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if hs.Status != "unhealthy" {
		t.Errorf("expected status %q, got %q", "unhealthy", hs.Status)
	}
}

func TestServiceDriverScale(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	output, err := d.Scale(context.Background(), "web", map[string]any{"replicas": 5})
	if err != nil {
		t.Fatalf("Scale failed: %v", err)
	}
	if output.Properties["replicas"] != 5 {
		t.Errorf("expected replicas 5, got %v", output.Properties["replicas"])
	}
}

func TestServiceDriverScaleMissingParam(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	_, err := d.Scale(context.Background(), "web", map[string]any{})
	if err == nil {
		t.Error("expected error when replicas param is missing")
	}
}

func TestServiceDriverDiff(t *testing.T) {
	d := NewServiceDriver(&mockExecutor{}, ".")

	diffs, err := d.Diff(context.Background(), "web", map[string]any{
		"image":    "nginx:v2",
		"replicas": 5,
	})
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(diffs) == 0 {
		t.Error("expected non-empty diff")
	}
}
