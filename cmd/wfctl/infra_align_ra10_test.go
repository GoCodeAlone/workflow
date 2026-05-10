package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// stubIaCProvider returns a *typedIaCAdapter (interfaces.IaCProvider) backed
// by a bufconn-served pb.IaCProviderRequired-only server. Used by R-A10
// tests as a non-validator baseline — adapter.Validator() returns nil so the
// align rule's dispatch site treats it as a provider that does NOT implement
// ProviderValidator.
//
// Per ADR-0028 (Task 17 / PR 618 strict-contracts force-cutover): wfctl
// dispatch sites are pure typed-pb. Test fixtures must construct a real
// *typedIaCAdapter rather than injecting a custom interfaces.IaCProvider —
// the latter no longer satisfies the type-assert at the dispatch site.
func stubIaCProvider(t *testing.T, name string) interfaces.IaCProvider {
	t.Helper()
	return fixtureTypedAdapter{
		Required: &fixtureRequiredServer{name: name, version: "0.0.0"},
	}.build(t)
}

// validatingStubProvider returns a *typedIaCAdapter whose ProviderValidator
// service emits the supplied diagnostics on every ValidatePlan call. The
// pb-shape diagnostic is converted from the engine-side
// []interfaces.PlanDiagnostic so callers can keep declaring fixtures in the
// engine's types.
func validatingStubProvider(t *testing.T, name string, diags []interfaces.PlanDiagnostic) interfaces.IaCProvider {
	t.Helper()
	return fixtureTypedAdapter{
		Required:  &fixtureRequiredServer{name: name, version: "0.0.0"},
		Validator: &cannedValidatorServer{diagnostics: diags},
	}.build(t)
}

// cannedValidatorServer returns a fixed set of PlanDiagnostics on every
// ValidatePlan RPC. Fixture analogue of the legacy validatingStubProvider's
// inline ValidatePlan(plan) []PlanDiagnostic — diagnostics come back as the
// proto shape so the typed adapter's planDiagnosticSeverityFromPB conversion
// roundtrips identically to the production wire path.
type cannedValidatorServer struct {
	pb.UnimplementedIaCProviderValidatorServer
	diagnostics []interfaces.PlanDiagnostic
}

func (s *cannedValidatorServer) ValidatePlan(_ context.Context, _ *pb.ValidatePlanRequest) (*pb.ValidatePlanResponse, error) {
	out := make([]*pb.PlanDiagnostic, 0, len(s.diagnostics))
	for _, d := range s.diagnostics {
		out = append(out, &pb.PlanDiagnostic{
			Severity: planDiagnosticSeverityToPB(d.Severity),
			Resource: d.Resource,
			Field:    d.Field,
			Message:  d.Message,
		})
	}
	return &pb.ValidatePlanResponse{Diagnostics: out}, nil
}

// planDiagnosticSeverityToPB is the inverse of planDiagnosticSeverityFromPB
// in iac_typed_adapter.go — extracted here as a test helper so fixtures can
// declare engine-side severities and have the canned server emit the right
// proto enum.
func planDiagnosticSeverityToPB(s interfaces.PlanDiagnosticSeverity) pb.PlanDiagnosticSeverity {
	switch s {
	case interfaces.PlanDiagnosticWarning:
		return pb.PlanDiagnosticSeverity_PLAN_DIAGNOSTIC_WARNING
	case interfaces.PlanDiagnosticError:
		return pb.PlanDiagnosticSeverity_PLAN_DIAGNOSTIC_ERROR
	default:
		return pb.PlanDiagnosticSeverity_PLAN_DIAGNOSTIC_INFO
	}
}

// ── Unit tests for checkRA10_provider_validate_plan ──────────────────────────

func TestCheckRA10_NilPlan(t *testing.T) {
	providers := []interfaces.IaCProvider{
		validatingStubProvider(t, "p", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticError, Message: "boom"},
		}),
	}
	if got := checkRA10_provider_validate_plan(context.Background(), providers, nil); len(got) != 0 {
		t.Errorf("expected no findings when plan is nil, got: %+v", got)
	}
}

func TestCheckRA10_NoProviders(t *testing.T) {
	if got := checkRA10_provider_validate_plan(context.Background(), nil, &interfaces.IaCPlan{}); len(got) != 0 {
		t.Errorf("expected no findings when providers empty, got: %+v", got)
	}
}

func TestCheckRA10_NonValidatingProviderSkipped(t *testing.T) {
	providers := []interfaces.IaCProvider{stubIaCProvider(t, "plain")}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
	if len(got) != 0 {
		t.Errorf("expected no findings when provider does not implement ProviderValidator, got: %+v", got)
	}
}

func TestCheckRA10_ErrorDiagnostic_BecomesFAIL(t *testing.T) {
	providers := []interfaces.IaCProvider{
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{
				Severity: interfaces.PlanDiagnosticError,
				Resource: "db-staging",
				Field:    "vpc_ref",
				Message:  "vpc_ref points to unknown resource",
			},
		}),
	}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
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
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticWarning, Resource: "vpc-prod", Message: "deprecated region"},
		}),
	}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Severity != "WARN" {
		t.Errorf("Severity: got %q, want WARN", got[0].Severity)
	}
}

func TestCheckRA10_InfoDiagnostic_LogsAndEmitsNoFinding(t *testing.T) {
	// Plan T4.2 spec: "Info → logs". An Info diagnostic must be written to
	// the stderr-style log sink AND must NOT produce an AlignFinding —
	// otherwise --strict would exit non-zero on a purely informational hint,
	// defeating the tier's purpose.
	var buf strings.Builder
	origLog := ra10LogInfo
	t.Cleanup(func() { ra10LogInfo = origLog })
	ra10LogInfo = func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
	}

	providers := []interfaces.IaCProvider{
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticInfo, Resource: "lb", Field: "tier", Message: "hint about tier"},
		}),
	}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
	if len(got) != 0 {
		t.Fatalf("expected 0 findings for Info diagnostic, got %d: %+v", len(got), got)
	}

	logged := buf.String()
	if !strings.Contains(logged, "R-A10") {
		t.Errorf("log: expected to contain rule tag 'R-A10', got %q", logged)
	}
	if !strings.Contains(logged, "[info]") {
		t.Errorf("log: expected to mark severity as [info], got %q", logged)
	}
	if !strings.Contains(logged, "do/lb") {
		t.Errorf("log: expected to identify provider/resource as 'do/lb', got %q", logged)
	}
	if !strings.Contains(logged, "hint about tier") {
		t.Errorf("log: expected to include diagnostic message, got %q", logged)
	}
	if !strings.Contains(logged, "field: tier") {
		t.Errorf("log: expected to include field path 'field: tier', got %q", logged)
	}

	// Critical guarantee: even under --strict, an Info-only run exits 0.
	if alignExitCode(got, true) != 0 {
		t.Errorf("alignExitCode(strict=true) = %d, want 0 — Info must never affect exit code",
			alignExitCode(got, true))
	}
}

func TestCheckRA10_PlanLevelDiagnostic_UsesProviderName(t *testing.T) {
	// When Resource is empty (plan-level finding), the AlignFinding.Resource
	// falls back to "<provider-name>:plan" so renderers stay deterministic.
	providers := []interfaces.IaCProvider{
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticError, Message: "plan-level constraint"},
		}),
	}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].Resource != "do:plan" {
		t.Errorf("Resource: got %q, want %q", got[0].Resource, "do:plan")
	}
}

func TestCheckRA10_PlanLevelInfoDiagnostic_LogsAsProviderSlashPlan(t *testing.T) {
	// Plan-level Info diagnostics must log as `<provider>/plan` per the
	// documented `R-A10 [info] <provider>/<resource>: ...` format — not the
	// redundant `<provider>/<provider>:plan: ...` that fell out of using the
	// table label as the log identifier.
	var buf strings.Builder
	origLog := ra10LogInfo
	t.Cleanup(func() { ra10LogInfo = origLog })
	ra10LogInfo = func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
	}

	providers := []interfaces.IaCProvider{
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticInfo, Message: "plan-level hint"},
		}),
	}
	_ = checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
	logged := buf.String()
	if !strings.Contains(logged, "do/plan") {
		t.Errorf("log: expected `do/plan` for plan-level Info, got %q", logged)
	}
	if strings.Contains(logged, "do/do:plan") {
		t.Errorf("log: must not double-qualify provider name (got %q)", logged)
	}
}

func TestCheckRA10_MultipleProviders_OnlyValidatorsContribute(t *testing.T) {
	providers := []interfaces.IaCProvider{
		stubIaCProvider(t, "plain"),
		validatingStubProvider(t, "do", []interfaces.PlanDiagnostic{
			{Severity: interfaces.PlanDiagnosticError, Resource: "db", Message: "X"},
		}),
		// validator with no diagnostics — provider implements ProviderValidator
		// (Validator service is registered) but returns an empty slice; verifies
		// the rule handles empty slices.
		validatingStubProvider(t, "aws", nil),
	}
	got := checkRA10_provider_validate_plan(context.Background(), providers, &interfaces.IaCPlan{})
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
	alignLoadProviders = func(_ *alignContext, _ string, _ *interfaces.IaCPlan) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{
			validatingStubProvider(t, "fixture", []interfaces.PlanDiagnostic{
				{
					Severity: interfaces.PlanDiagnosticError,
					Resource: "db-staging",
					Field:    "vpc_ref",
					Message:  "vpc_ref points to unknown resource",
				},
			}),
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
	findings, err := runInfraAlignChecks(context.Background(), opts)
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
	alignLoadProviders = func(_ *alignContext, _ string, _ *interfaces.IaCPlan) ([]interfaces.IaCProvider, []io.Closer, error) {
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
	findings, err := runInfraAlignChecks(context.Background(), opts)
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
