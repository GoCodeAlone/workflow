package infra

import "context"

// ResourceProvider provisions and destroys a specific resource type.
type ResourceProvider interface {
	Provision(ctx context.Context, rc ResourceConfig) error
	Destroy(ctx context.Context, name string) error
	Supports(resourceType, provider string) bool
}

// MemoryProvider is an in-memory resource provider for testing and local dev.
type MemoryProvider struct{}

func (m *MemoryProvider) Provision(_ context.Context, _ ResourceConfig) error { return nil }
func (m *MemoryProvider) Destroy(_ context.Context, _ string) error           { return nil }
func (m *MemoryProvider) Supports(_, _ string) bool                           { return true }
