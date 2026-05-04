package refreshoutputs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeIaCProvider is a minimal IaCProvider stub that returns canned
// ResourceOutput values for Read via fakeResourceDriver. It only implements
// the methods that Refresh exercises (ResourceDriver); the rest panic to
// make accidental use during testing obvious.
type fakeIaCProvider struct {
	// readOutputs maps ProviderID → fake driver output Outputs map. Missing
	// or nil entries cause Read to return a ResourceOutput whose Outputs is
	// nil (not an empty map) — that's what map indexing into a
	// map[string]map[string]any returns for an absent key.
	readOutputs map[string]map[string]any
	// readErr, when non-nil, causes the driver Read to return the error
	// regardless of the resource ref.
	readErr error
}

func (f *fakeIaCProvider) Name() string    { panic("not used") }
func (f *fakeIaCProvider) Version() string { panic("not used") }
func (f *fakeIaCProvider) Initialize(context.Context, map[string]any) error {
	panic("not used")
}
func (f *fakeIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	panic("not used")
}
func (f *fakeIaCProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	panic("not used")
}
func (f *fakeIaCProvider) Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	panic("not used")
}
func (f *fakeIaCProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	panic("not used")
}
func (f *fakeIaCProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	panic("not used")
}
func (f *fakeIaCProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	panic("not used")
}
func (f *fakeIaCProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	panic("not used")
}
func (f *fakeIaCProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	panic("not used")
}
func (f *fakeIaCProvider) SupportedCanonicalKeys() []string { panic("not used") }
func (f *fakeIaCProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	panic("not used")
}
func (f *fakeIaCProvider) Close() error { return nil }

func (f *fakeIaCProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return &fakeResourceDriver{provider: f}, nil
}

// fakeResourceDriver answers Read from the parent fakeIaCProvider's
// readOutputs map. All other methods panic to make misuse loud.
type fakeResourceDriver struct {
	provider *fakeIaCProvider
}

func (d *fakeResourceDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *fakeResourceDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	if d.provider.readErr != nil {
		return nil, d.provider.readErr
	}
	out := d.provider.readOutputs[ref.ProviderID]
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       ref.Type,
		ProviderID: ref.ProviderID,
		Outputs:    out,
	}, nil
}
func (d *fakeResourceDriver) Update(context.Context, interfaces.ResourceRef, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *fakeResourceDriver) Delete(context.Context, interfaces.ResourceRef) error {
	panic("not used")
}
func (d *fakeResourceDriver) Diff(context.Context, interfaces.ResourceSpec, *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	panic("not used")
}
func (d *fakeResourceDriver) HealthCheck(context.Context, interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	panic("not used")
}
func (d *fakeResourceDriver) Scale(context.Context, interfaces.ResourceRef, int) (*interfaces.ResourceOutput, error) {
	panic("not used")
}
func (d *fakeResourceDriver) SensitiveKeys() []string { return nil }

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || bv != v {
			return false
		}
	}
	return true
}

func TestRefreshOutputs_ReadsEachResource_PersistsChangedOnly(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "vpc-1", Type: "infra.vpc", ProviderID: "uuid-1", Outputs: map[string]any{"ip_range": "10.0.0.0/16"}},
		{Name: "vpc-2", Type: "infra.vpc", ProviderID: "uuid-2", Outputs: map[string]any{"ip_range": "10.1.0.0/16"}},
	}
	fakeProvider := &fakeIaCProvider{readOutputs: map[string]map[string]any{
		"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"}, // new "id" field
		"uuid-2": {"ip_range": "10.1.0.0/16"},                 // unchanged
	}}
	refreshed, err := Refresh(context.Background(), fakeProvider, states, Options{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := refreshed[0].Outputs["id"]; got != "uuid-1" {
		t.Errorf("vpc-1 should have new 'id' output: %v", refreshed[0].Outputs)
	}
	if !mapsEqual(refreshed[1].Outputs, states[1].Outputs) {
		t.Errorf("vpc-2 should be unchanged: %v vs %v", refreshed[1].Outputs, states[1].Outputs)
	}
}

func TestRefreshOutputs_PartialFailure_ReturnsError(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "vpc-1", Type: "infra.vpc", ProviderID: "uuid-1"},
	}
	fakeProvider := &fakeIaCProvider{readErr: errors.New("network failure")}
	_, err := Refresh(context.Background(), fakeProvider, states, Options{Concurrency: 1})
	if err == nil {
		t.Fatalf("expected error on Read failure")
	}
	if !strings.Contains(err.Error(), "could not refresh") {
		t.Errorf("error should mention 'could not refresh'; got: %v", err)
	}
}
