// Package stubprovider supplies a minimal in-process interfaces.IaCProvider
// for use in integration tests and the scenario-92 demo stack.
//
// The Provider does NOT make real cloud API calls. Every lifecycle method
// returns a deterministic, non-error result:
//
//   - Plan:         compares desired vs current by name; emits "create" for
//     resources absent from current and "delete" for resources
//     absent from desired.
//   - ResourceDriver: returns a stub driver whose Create/Update/Delete all
//     succeed, enabling wfctlhelpers.ApplyPlanWithHooks to run
//     end-to-end without any external plugin subprocess.
//   - Destroy:      returns all supplied refs as Destroyed names (no-op).
//   - DetectDrift:  returns Drifted:false for every ref.
//
// This package imports only interfaces — no new import cycles.
package stubprovider

import (
	"context"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// Provider is the exported stub IaCProvider. Use New() to obtain an instance.
type Provider struct{}

// Compile-time conformance check.
var _ interfaces.IaCProvider = (*Provider)(nil)

// New returns an initialized stub Provider.
func New() *Provider { return &Provider{} }

// Name returns the stub provider identifier.
func (p *Provider) Name() string { return "stub" }

// Version returns the stub provider version.
func (p *Provider) Version() string { return "0.0.0-stub" }

// Initialize is a no-op for the stub.
func (p *Provider) Initialize(_ context.Context, _ map[string]any) error { return nil }

// Capabilities returns nil — the stub does not declare optional capabilities.
func (p *Provider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }

// Plan compares desired specs against current state by name and returns
// a plan with "create" for each new resource and "delete" for each
// resource present in current but absent from desired. Resources present
// in both are returned as "update" actions (no-op at apply time; the
// stub driver's Update returns success).
func (p *Provider) Plan(_ context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	currentByName := make(map[string]*interfaces.ResourceState, len(current))
	for i := range current {
		currentByName[current[i].Name] = &current[i]
	}

	desiredByName := make(map[string]struct{}, len(desired))
	for _, s := range desired {
		desiredByName[s.Name] = struct{}{}
	}

	plan := &interfaces.IaCPlan{}

	for _, spec := range desired {
		if _, exists := currentByName[spec.Name]; exists {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{
				Action:   "update",
				Resource: spec,
				Current:  currentByName[spec.Name],
			})
		} else {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{
				Action:   "create",
				Resource: spec,
			})
		}
	}

	for i := range current {
		st := &current[i]
		if _, wanted := desiredByName[st.Name]; !wanted {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{
				Action:   "delete",
				Resource: interfaces.ResourceSpec{Name: st.Name, Type: st.Type},
				Current:  st,
			})
		}
	}

	return plan, nil
}

// Destroy returns all supplied refs as Destroyed names.
func (p *Provider) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	destroyed := make([]string, 0, len(refs))
	for _, r := range refs {
		destroyed = append(destroyed, r.Name)
	}
	return &interfaces.DestroyResult{Destroyed: destroyed}, nil
}

// Status returns nil — the stub does not probe live cloud status.
func (p *Provider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}

// DetectDrift returns Drifted:false with DriftClassInSync for every ref.
func (p *Provider) DetectDrift(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	results := make([]interfaces.DriftResult, 0, len(refs))
	for _, r := range refs {
		results = append(results, interfaces.DriftResult{
			Name:    r.Name,
			Type:    r.Type,
			Drifted: false,
			Class:   interfaces.DriftClassInSync,
		})
	}
	return results, nil
}

// Import returns nil — the stub does not support resource import.
func (p *Provider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}

// ResolveSizing returns nil — the stub does not resolve sizing.
func (p *Provider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}

// ResourceDriver returns a stub driver for any resource type.
func (p *Provider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &stubDriver{}, nil
}

// SupportedCanonicalKeys returns nil.
func (p *Provider) SupportedCanonicalKeys() []string { return nil }

// BootstrapStateBackend returns nil — the stub does not manage state backends.
func (p *Provider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}

// Close is a no-op.
func (p *Provider) Close() error { return nil }

// stubDriver is an in-process ResourceDriver whose lifecycle methods all
// return success with a minimal ResourceOutput.
type stubDriver struct{}

// Compile-time conformance check.
var _ interfaces.ResourceDriver = (*stubDriver)(nil)

// Create returns a ResourceOutput with the spec's name and type.
func (d *stubDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: "stub-" + spec.Name,
	}, nil
}

// Read returns a ResourceOutput with the ref's name and type.
func (d *stubDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       ref.Name,
		Type:       ref.Type,
		ProviderID: ref.ProviderID,
	}, nil
}

// Update returns a ResourceOutput with the spec's name and type.
func (d *stubDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	pid := ref.ProviderID
	if pid == "" {
		pid = "stub-" + spec.Name
	}
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: pid,
	}, nil
}

// Delete is a no-op.
func (d *stubDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }

// Diff returns a DiffResult indicating no changes needed.
func (d *stubDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{
		NeedsUpdate:  false,
		NeedsReplace: false,
		Changes:      nil,
	}, nil
}

// HealthCheck returns nil — the stub does not probe health.
func (d *stubDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}

// Scale returns nil — the stub does not support scaling.
func (d *stubDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// SensitiveKeys returns nil.
func (d *stubDriver) SensitiveKeys() []string { return nil }
