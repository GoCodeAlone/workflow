package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// stubIaCProvider is a no-op IaCProvider used by R-A10 tests. By default it
// does NOT implement ProviderValidator. Embed this in a struct that adds
// ValidatePlan to opt in.
type stubIaCProvider struct{ name string }

func (s stubIaCProvider) Name() string                                      { return s.name }
func (stubIaCProvider) Version() string                                     { return "0.0.0" }
func (stubIaCProvider) Initialize(context.Context, map[string]any) error    { return nil }
func (stubIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (stubIaCProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (stubIaCProvider) Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (stubIaCProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (stubIaCProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (stubIaCProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (stubIaCProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (stubIaCProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (stubIaCProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (stubIaCProvider) SupportedCanonicalKeys() []string { return nil }
func (stubIaCProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (stubIaCProvider) Close() error { return nil }

// validatingStubProvider opts into ProviderValidator with canned diagnostics.
type validatingStubProvider struct {
	stubIaCProvider
	diags []interfaces.PlanDiagnostic
}

func (v validatingStubProvider) ValidatePlan(*interfaces.IaCPlan) []interfaces.PlanDiagnostic {
	return v.diags
}

// ── Unit tests for checkRA10_provider_validate_plan ──────────────────────────

func TestCheckRA10_NilPlan(t *testing.T) {
	providers := []interfaces.IaCProvider{
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "p"},
			diags: []interfaces.PlanDiagnostic{
				{Severity: interfaces.PlanDiagnosticError, Message: "boom"},
			},
		},
	}
	if got := checkRA10_provider_validate_plan(providers, nil); len(got) != 0 {
		t.Errorf("expected no findings when plan is nil, got: %+v", got)
	}
}

func TestCheckRA10_NoProviders(t *testing.T) {
	if got := checkRA10_provider_validate_plan(nil, &interfaces.IaCPlan{}); len(got) != 0 {
		t.Errorf("expected no findings when providers empty, got: %+v", got)
	}
}

func TestCheckRA10_NonValidatingProviderSkipped(t *testing.T) {
	providers := []interfaces.IaCProvider{stubIaCProvider{name: "plain"}}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 0 {
		t.Errorf("expected no findings when provider does not implement ProviderValidator, got: %+v", got)
	}
}

func TestCheckRA10_ErrorDiagnostic_BecomesFAIL(t *testing.T) {
	providers := []interfaces.IaCProvider{
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "do"},
			diags: []interfaces.PlanDiagnostic{
				{
					Severity: interfaces.PlanDiagnosticError,
					Resource: "db-staging",
					Field:    "vpc_ref",
					Message:  "vpc_ref points to unknown resource",
				},
			},
		},
	}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(got), got)
	}
	f := got[0]
	if f.Rule != "R-A10" {
		t.Errorf("Rule: got %q, want R-A10", f.Rule)
	}
	if f.Severity != "FAIL" {
		t.Errorf("Severity: got %q, want FAIL", f.Severity)
	}
	if f.Resource != "db-staging" {
		t.Errorf("Resource: got %q, want db-staging", f.Resource)
	}
	if !strings.Contains(f.Message, "vpc_ref points to unknown resource") {
		t.Errorf("Message: got %q, want substring 'vpc_ref points to unknown resource'", f.Message)
	}
	if !strings.Contains(f.Message, "vpc_ref") {
		t.Errorf("Message: expected to mention field path 'vpc_ref', got %q", f.Message)
	}
}

func TestCheckRA10_WarningDiagnostic_BecomesWARN(t *testing.T) {
	providers := []interfaces.IaCProvider{
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "do"},
			diags: []interfaces.PlanDiagnostic{
				{Severity: interfaces.PlanDiagnosticWarning, Resource: "vpc-prod", Message: "deprecated region"},
			},
		},
	}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Severity != "WARN" {
		t.Errorf("Severity: got %q, want WARN", got[0].Severity)
	}
}

func TestCheckRA10_InfoDiagnostic_BecomesWARN(t *testing.T) {
	// Info maps to WARN (advisory) since the existing align FAIL/WARN model
	// has no INFO tier; default-mode exit stays 0, --strict still flags.
	providers := []interfaces.IaCProvider{
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "do"},
			diags: []interfaces.PlanDiagnostic{
				{Severity: interfaces.PlanDiagnosticInfo, Resource: "lb", Message: "hint"},
			},
		},
	}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Severity != "WARN" {
		t.Errorf("Severity: got %q, want WARN", got[0].Severity)
	}
}

func TestCheckRA10_PlanLevelDiagnostic_UsesProviderName(t *testing.T) {
	// When Resource is empty (plan-level finding), the AlignFinding.Resource
	// falls back to "<provider-name>:plan" so renderers stay deterministic.
	providers := []interfaces.IaCProvider{
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "do"},
			diags: []interfaces.PlanDiagnostic{
				{Severity: interfaces.PlanDiagnosticError, Message: "plan-level constraint"},
			},
		},
	}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Resource != "do:plan" {
		t.Errorf("Resource: got %q, want %q", got[0].Resource, "do:plan")
	}
}

func TestCheckRA10_MultipleProviders_OnlyValidatorsContribute(t *testing.T) {
	providers := []interfaces.IaCProvider{
		stubIaCProvider{name: "plain"},
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "do"},
			diags: []interfaces.PlanDiagnostic{
				{Severity: interfaces.PlanDiagnosticError, Resource: "db", Message: "X"},
			},
		},
		validatingStubProvider{
			stubIaCProvider: stubIaCProvider{name: "aws"},
			// No diagnostics — provider implements ProviderValidator but
			// returns nil; verifies the rule handles empty slices.
		},
	}
	got := checkRA10_provider_validate_plan(providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding (from 'do' only), got %d: %+v", len(got), got)
	}
	if got[0].Resource != "db" {
		t.Errorf("Resource: got %q, want db", got[0].Resource)
	}
}

// ── Integration: align dispatch wires R-A10 through the seam ────────────────

func TestInfraAlign_RA10_FixtureProvider_Fires(t *testing.T) {
	// Override the alignLoadProviders seam with a fixture that returns a
	// provider implementing ProviderValidator with a fatal diagnostic.
	// Verifies end-to-end that runInfraAlignChecks surfaces R-A10 findings
	// when --plan is set, without touching the live plugin loader.
	orig := alignLoadProviders
	t.Cleanup(func() { alignLoadProviders = orig })
	alignLoadProviders = func(_ string, _ string, _ *interfaces.IaCPlan) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{
			validatingStubProvider{
				stubIaCProvider: stubIaCProvider{name: "fixture"},
				diags: []interfaces.PlanDiagnostic{
					{
						Severity: interfaces.PlanDiagnosticError,
						Resource: "db-staging",
						Field:    "vpc_ref",
						Message:  "vpc_ref points to unknown resource",
					},
				},
			},
		}, nil, nil
	}

	// Minimal config — the rule body doesn't actually look at the YAML; it
	// only needs runInfraAlignChecks to reach the R-A10 dispatch.
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "api:latest"
      http_port: 8080
`
	cfgFile := writeAlignYAML(t, yaml)

	plan := interfaces.IaCPlan{ID: "p1"}
	planFile := writeAlignPlanJSON(t, plan)

	opts := alignOptions{configFile: cfgFile, planFile: planFile}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("runInfraAlignChecks: %v", err)
	}
	if !findingsHaveRule(findings, "R-A10") {
		t.Fatalf("expected R-A10 finding, got: %+v", findings)
	}
	// Strict mode + a FAIL severity → non-zero exit.
	if alignExitCode(findings, true) == 0 {
		t.Errorf("expected non-zero exit code in strict mode with FAIL finding")
	}
}

func TestInfraAlign_RA10_NotInvokedWithoutPlan(t *testing.T) {
	// Without --plan, R-A10 should NOT call alignLoadProviders nor emit
	// findings — keeps align's plan-less mode lightweight.
	calls := 0
	orig := alignLoadProviders
	t.Cleanup(func() { alignLoadProviders = orig })
	alignLoadProviders = func(_ string, _ string, _ *interfaces.IaCPlan) ([]interfaces.IaCProvider, []io.Closer, error) {
		calls++
		return nil, nil, nil
	}

	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "api:latest"
      http_port: 8080
`
	cfgFile := writeAlignYAML(t, yaml)

	opts := alignOptions{configFile: cfgFile}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("runInfraAlignChecks: %v", err)
	}
	if calls != 0 {
		t.Errorf("alignLoadProviders called %d times without --plan; want 0", calls)
	}
	if findingsHaveRule(findings, "R-A10") {
		t.Errorf("expected no R-A10 findings without --plan, got: %+v", findings)
	}
}
