package drivers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// StubDriver handles capabilities that Docker Compose cannot faithfully
// implement (e.g., Kubernetes clusters, IAM roles). It returns resource outputs
// with stub status and logs warnings to alert users of fidelity gaps.
type StubDriver struct{}

// NewStubDriver creates a StubDriver for unsupported capabilities.
func NewStubDriver() *StubDriver {
	return &StubDriver{}
}

// ResourceType returns "docker-compose.stub".
func (d *StubDriver) ResourceType() string {
	return "docker-compose.stub"
}

// Create logs a warning and returns a stub resource output.
func (d *StubDriver) Create(_ context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	originalType := getStringProperty(properties, "original_type")
	reason := getStringProperty(properties, "stub_reason")

	log.Printf("[WARN] stub resource %q (type: %s): %s", name, originalType, reason)

	return &platform.ResourceOutput{
		Name:         name,
		Type:         originalType,
		ProviderType: "docker-compose.stub",
		Properties: map[string]any{
			"stub":          true,
			"original_type": originalType,
			"stub_reason":   reason,
			"fidelity":      string(platform.FidelityStub),
		},
		Status:     platform.ResourceStatusActive,
		LastSynced: time.Now(),
	}, nil
}

// Read returns the stub resource state.
func (d *StubDriver) Read(_ context.Context, name string) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "stub",
		ProviderType: "docker-compose.stub",
		Properties: map[string]any{
			"stub": true,
		},
		Status:     platform.ResourceStatusActive,
		LastSynced: time.Now(),
	}, nil
}

// Update logs a warning and returns the stub output unchanged.
func (d *StubDriver) Update(_ context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	log.Printf("[WARN] update on stub resource %q is a no-op", name)
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "stub",
		ProviderType: "docker-compose.stub",
		Properties:   desired,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Delete is a no-op for stub resources.
func (d *StubDriver) Delete(_ context.Context, name string) error {
	log.Printf("[WARN] delete on stub resource %q is a no-op", name)
	return nil
}

// HealthCheck always returns healthy for stubs since they are no-ops.
func (d *StubDriver) HealthCheck(_ context.Context, name string) (*platform.HealthStatus, error) {
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   fmt.Sprintf("stub resource %q (no-op, always healthy)", name),
		CheckedAt: time.Now(),
		Details: map[string]any{
			"stub":     true,
			"fidelity": string(platform.FidelityStub),
		},
	}, nil
}

// Scale is not supported for stub resources.
func (d *StubDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "docker-compose.stub"}
}

// Diff always returns no differences for stub resources.
func (d *StubDriver) Diff(_ context.Context, _ string, _ map[string]any) ([]platform.DiffEntry, error) {
	return nil, nil
}

func getStringProperty(props map[string]any, key string) string {
	if val, ok := props[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
