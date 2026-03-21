package interfaces_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// Compile-time interface compliance checks
var _ interfaces.IaCProvider = (*mockProvider)(nil)
var _ interfaces.ResourceDriver = (*mockDriver)(nil)
var _ interfaces.IaCStateStore = (*mockState)(nil)

// mockProvider implements IaCProvider
type mockProvider struct{}

func (m *mockProvider) Name() string    { return "mock" }
func (m *mockProvider) Version() string { return "0.0.1" }
func (m *mockProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (m *mockProvider) Capabilities() []interfaces.CapabilityDeclaration { return nil }
func (m *mockProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.Plan, error) {
	return nil, nil
}
func (m *mockProvider) Apply(_ context.Context, _ *interfaces.Plan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (m *mockProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (m *mockProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (m *mockProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (m *mockProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (m *mockProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (m *mockProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) { return nil, nil }
func (m *mockProvider) Close() error                                                { return nil }

// mockDriver implements ResourceDriver
type mockDriver struct{}

func (d *mockDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *mockDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *mockDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *mockDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *mockDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *mockDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *mockDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// mockState implements IaCStateStore
type mockState struct{}

func (s *mockState) SaveResource(_ interfaces.ResourceState) error   { return nil }
func (s *mockState) GetResource(_ string) (*interfaces.ResourceState, error) { return nil, nil }
func (s *mockState) ListResources() ([]interfaces.ResourceState, error)      { return nil, nil }
func (s *mockState) DeleteResource(_ string) error                           { return nil }
func (s *mockState) SavePlan(_ interfaces.Plan) error                        { return nil }
func (s *mockState) GetPlan(_ string) (*interfaces.Plan, error)              { return nil, nil }
func (s *mockState) Lock(_ string) error                                     { return nil }
func (s *mockState) Unlock(_ string) error                                   { return nil }
func (s *mockState) Close() error                                            { return nil }

func TestSizeConstants(t *testing.T) {
	sizes := []interfaces.Size{
		interfaces.SizeXS, interfaces.SizeS, interfaces.SizeM,
		interfaces.SizeL, interfaces.SizeXL,
	}
	if len(sizes) != 5 {
		t.Fatal("expected 5 size tiers")
	}
}

func TestResourceSpecDependsOn(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:      "db",
		Type:      "infra.database",
		DependsOn: []string{"network"},
	}
	if len(spec.DependsOn) != 1 {
		t.Fatal("expected 1 dependency")
	}
}

func TestSizingMapCoversAllSizes(t *testing.T) {
	sizes := []interfaces.Size{
		interfaces.SizeXS, interfaces.SizeS, interfaces.SizeM,
		interfaces.SizeL, interfaces.SizeXL,
	}
	for _, sz := range sizes {
		if _, ok := interfaces.SizingMap[sz]; !ok {
			t.Fatalf("SizingMap missing entry for size %q", sz)
		}
	}
}
