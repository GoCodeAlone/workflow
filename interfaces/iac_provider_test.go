package interfaces_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestDriftClass_Constants(t *testing.T) {
	cases := []struct {
		name     string
		c        interfaces.DriftClass
		expected string
	}{
		{"unknown", interfaces.DriftClassUnknown, ""},
		{"in-sync", interfaces.DriftClassInSync, "in-sync"},
		{"ghost", interfaces.DriftClassGhost, "ghost"},
		{"config", interfaces.DriftClassConfig, "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.c) != tc.expected {
				t.Errorf("got %q, want %q", string(tc.c), tc.expected)
			}
		})
	}
}

func TestDriftResult_ClassOmitEmpty(t *testing.T) {
	// Class="" (DriftClassUnknown) should be omitted from JSON for backwards compat
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: false}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"class"`) {
		t.Errorf("expected no class field with empty Class, got %s", b)
	}
}

func TestDriftResult_ClassPresent(t *testing.T) {
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"class":"ghost"`) {
		t.Errorf("expected class:ghost in JSON, got %s", b)
	}
}

func TestDriftResult_ClassRoundTrip(t *testing.T) {
	cases := []interfaces.DriftClass{
		interfaces.DriftClassInSync,
		interfaces.DriftClassGhost,
		interfaces.DriftClassConfig,
	}
	for _, c := range cases {
		orig := interfaces.DriftResult{Name: "res", Type: "infra.vpc", Drifted: true, Class: c}
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("marshal %q: %v", c, err)
		}
		var got interfaces.DriftResult
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", c, err)
		}
		if got.Class != c {
			t.Errorf("round-trip Class: got %q, want %q", got.Class, c)
		}
	}
}

// --- T4.1: ProviderValidator optional interface + PlanDiagnostic type ---

func TestPlanDiagnosticSeverity_Constants(t *testing.T) {
	// Severity constants follow iota Info(0), Warning(1), Error(2).
	if interfaces.PlanDiagnosticInfo == interfaces.PlanDiagnosticWarning {
		t.Errorf("PlanDiagnosticInfo and PlanDiagnosticWarning must differ")
	}
	if interfaces.PlanDiagnosticWarning == interfaces.PlanDiagnosticError {
		t.Errorf("PlanDiagnosticWarning and PlanDiagnosticError must differ")
	}
	if interfaces.PlanDiagnosticInfo == interfaces.PlanDiagnosticError {
		t.Errorf("PlanDiagnosticInfo and PlanDiagnosticError must differ")
	}
	if got, want := int(interfaces.PlanDiagnosticInfo), 0; got != want {
		t.Errorf("PlanDiagnosticInfo: got %d, want %d", got, want)
	}
	if got, want := int(interfaces.PlanDiagnosticWarning), 1; got != want {
		t.Errorf("PlanDiagnosticWarning: got %d, want %d", got, want)
	}
	if got, want := int(interfaces.PlanDiagnosticError), 2; got != want {
		t.Errorf("PlanDiagnosticError: got %d, want %d", got, want)
	}
}

func TestPlanDiagnostic_Fields(t *testing.T) {
	d := interfaces.PlanDiagnostic{
		Severity: interfaces.PlanDiagnosticError,
		Resource: "db",
		Field:    "vpc_ref",
		Message:  "vpc_ref points to unknown resource",
	}
	if d.Severity != interfaces.PlanDiagnosticError {
		t.Errorf("Severity: got %v, want Error", d.Severity)
	}
	if d.Resource != "db" {
		t.Errorf("Resource: got %q, want %q", d.Resource, "db")
	}
	if d.Field != "vpc_ref" {
		t.Errorf("Field: got %q, want %q", d.Field, "vpc_ref")
	}
	if !strings.Contains(d.Message, "unknown resource") {
		t.Errorf("Message: got %q, want substring 'unknown resource'", d.Message)
	}
}

// validatingProvider is a no-op IaCProvider that also implements
// ProviderValidator. Defined inline (anonymous-ish) since only the type
// assertion + method shape matter for this test.
type validatingProvider struct{ diags []interfaces.PlanDiagnostic }

func (validatingProvider) Name() string                                        { return "validating" }
func (validatingProvider) Version() string                                     { return "0.0.0" }
func (validatingProvider) Initialize(context.Context, map[string]any) error    { return nil }
func (validatingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (validatingProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (validatingProvider) Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (validatingProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (validatingProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (validatingProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (validatingProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (validatingProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (validatingProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) { return nil, nil }
func (validatingProvider) SupportedCanonicalKeys() []string                         { return nil }
func (validatingProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (validatingProvider) Close() error { return nil }
func (p validatingProvider) ValidatePlan(plan *interfaces.IaCPlan) []interfaces.PlanDiagnostic {
	return p.diags
}

// nonValidatingProvider is a no-op IaCProvider that does NOT implement
// ProviderValidator — confirms the interface is optional.
type nonValidatingProvider struct{}

func (nonValidatingProvider) Name() string                                        { return "plain" }
func (nonValidatingProvider) Version() string                                     { return "0.0.0" }
func (nonValidatingProvider) Initialize(context.Context, map[string]any) error    { return nil }
func (nonValidatingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (nonValidatingProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (nonValidatingProvider) Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (nonValidatingProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (nonValidatingProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (nonValidatingProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (nonValidatingProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (nonValidatingProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (nonValidatingProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (nonValidatingProvider) SupportedCanonicalKeys() []string { return nil }
func (nonValidatingProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (nonValidatingProvider) Close() error { return nil }

func TestProviderValidator_TypeAssertion_Implementor(t *testing.T) {
	want := []interfaces.PlanDiagnostic{
		{Severity: interfaces.PlanDiagnosticError, Resource: "db", Field: "vpc_ref", Message: "boom"},
	}
	var p interfaces.IaCProvider = validatingProvider{diags: want}

	v, ok := p.(interfaces.ProviderValidator)
	if !ok {
		t.Fatalf("validatingProvider must satisfy ProviderValidator")
	}
	got := v.ValidatePlan(&interfaces.IaCPlan{})
	if len(got) != len(want) {
		t.Fatalf("ValidatePlan: got %d diags, want %d", len(got), len(want))
	}
	if got[0] != want[0] {
		t.Errorf("ValidatePlan: got %+v, want %+v", got[0], want[0])
	}
}

func TestProviderValidator_TypeAssertion_NonImplementor(t *testing.T) {
	var p interfaces.IaCProvider = nonValidatingProvider{}
	if _, ok := p.(interfaces.ProviderValidator); ok {
		t.Errorf("nonValidatingProvider must NOT satisfy ProviderValidator (interface must remain optional)")
	}
}

// TestDriftConfigDetector_OptionalInterface verifies the optional-declarer
// pattern works as intended: an IaCProvider impl that does NOT implement
// DriftConfigDetector type-asserts to false (caller falls back to existence-
// only DetectDrift); an impl that DOES implement it type-asserts to true.
func TestDriftConfigDetector_OptionalInterface(t *testing.T) {
	var minimal interfaces.IaCProvider = nonValidatingProvider{}
	if _, ok := minimal.(interfaces.DriftConfigDetector); ok {
		t.Errorf("nonValidatingProvider should NOT satisfy DriftConfigDetector")
	}

	var capable interfaces.IaCProvider = &capableIaCProvider{}
	if _, ok := capable.(interfaces.DriftConfigDetector); !ok {
		t.Errorf("capableIaCProvider MUST satisfy DriftConfigDetector")
	}
}

// capableIaCProvider extends nonValidatingProvider with DriftConfigDetector.
type capableIaCProvider struct{ nonValidatingProvider }

func (*capableIaCProvider) DetectDriftWithSpecs(context.Context, []interfaces.ResourceRef, map[string]interfaces.ResourceSpec) ([]interfaces.DriftResult, error) {
	return nil, nil
}

// TestEnumeratorAll_InterfaceShape pins the EnumeratorAll signature so
// downstream plugins (e.g. workflow-plugin-digitalocean) can rely on it.
// Per ADR 0016: providers whose resource types do not support tagging
// (e.g. DO Spaces keys) implement EnumeratorAll instead of Enumerator.
func TestEnumeratorAll_InterfaceShape(t *testing.T) {
	// Compile-time assertion: any type implementing this interface
	// must have the exact method signature.
	var _ interfaces.EnumeratorAll = (*fakeEnumeratorAll)(nil)
}

type fakeEnumeratorAll struct{}

func (f *fakeEnumeratorAll) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	return nil, nil
}

// ─── IaCProviderRegionLister optional interface ───────────────────────────────

// regionListerProvider implements both IaCProvider and IaCProviderRegionLister.
type regionListerProvider struct{ nonValidatingProvider }

func (r *regionListerProvider) ListRegions(_ context.Context, env string) ([]string, error) {
	return []string{"us-east-1", "us-west-2"}, nil
}

// Compile-time assertion: regionListerProvider satisfies IaCProviderRegionLister.
var _ interfaces.IaCProviderRegionLister = (*regionListerProvider)(nil)

// TestIaCProviderRegionLister_ImplementorSatisfies verifies that a type
// implementing ListRegions(ctx, env) satisfies the new optional interface.
func TestIaCProviderRegionLister_ImplementorSatisfies(t *testing.T) {
	var p interfaces.IaCProvider = &regionListerProvider{}

	rl, ok := p.(interfaces.IaCProviderRegionLister)
	if !ok {
		t.Fatalf("regionListerProvider must satisfy IaCProviderRegionLister")
	}
	regions, err := rl.ListRegions(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListRegions returned unexpected error: %v", err)
	}
	if len(regions) == 0 {
		t.Errorf("ListRegions returned no regions")
	}
}

// TestIaCProviderRegionLister_NonImplementorFails verifies the interface is
// truly optional: a provider that does NOT implement ListRegions must fail
// the type-assert so callers can gate accordingly.
func TestIaCProviderRegionLister_NonImplementorFails(t *testing.T) {
	var p interfaces.IaCProvider = nonValidatingProvider{}
	if _, ok := p.(interfaces.IaCProviderRegionLister); ok {
		t.Errorf("nonValidatingProvider must NOT satisfy IaCProviderRegionLister (interface is optional)")
	}
}
