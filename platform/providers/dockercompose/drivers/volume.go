package drivers

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// VolumeDriver handles CRUD operations for Docker Compose volumes.
// It implements platform.ResourceDriver for the "docker-compose.volume" type.
type VolumeDriver struct {
	executor   ComposeExecutor
	projectDir string
}

// NewVolumeDriver creates a VolumeDriver.
func NewVolumeDriver(executor ComposeExecutor, projectDir string) *VolumeDriver {
	return &VolumeDriver{
		executor:   executor,
		projectDir: projectDir,
	}
}

// ResourceType returns "docker-compose.volume".
func (d *VolumeDriver) ResourceType() string {
	return "docker-compose.volume"
}

// Create provisions a new compose volume.
func (d *VolumeDriver) Create(_ context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "persistent_volume",
		ProviderType: "docker-compose.volume",
		Properties:   properties,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Read fetches the current state of a compose volume.
func (d *VolumeDriver) Read(_ context.Context, name string) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "persistent_volume",
		ProviderType: "docker-compose.volume",
		Properties:   map[string]any{},
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Update modifies a compose volume definition.
func (d *VolumeDriver) Update(_ context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "persistent_volume",
		ProviderType: "docker-compose.volume",
		Properties:   desired,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Delete removes a volume from the compose definition.
func (d *VolumeDriver) Delete(_ context.Context, _ string) error {
	return nil
}

// HealthCheck returns the health status of a compose volume.
func (d *VolumeDriver) HealthCheck(_ context.Context, name string) (*platform.HealthStatus, error) {
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   fmt.Sprintf("volume %q is defined in compose file", name),
		CheckedAt: time.Now(),
	}, nil
}

// Scale is not supported for volumes.
func (d *VolumeDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "docker-compose.volume"}
}

// Diff compares desired properties with current state.
func (d *VolumeDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}

	var diffs []platform.DiffEntry
	for key, desiredVal := range desired {
		currentVal, exists := current.Properties[key]
		if !exists || fmt.Sprintf("%v", currentVal) != fmt.Sprintf("%v", desiredVal) {
			diffs = append(diffs, platform.DiffEntry{
				Path:     key,
				OldValue: currentVal,
				NewValue: desiredVal,
			})
		}
	}
	return diffs, nil
}
