package main

// iac_typed_dispatch.go — typed-RPC dispatch helpers for the wfctl
// call sites converted in Task 17 of the strict-contracts force-cutover
// (docs/plans/2026-05-10-strict-contracts-force-cutover.md, rev5).
//
// Each helper wraps a single typed pb.IaC* client method behind a
// signature that matches the Go-interface contract the call site used
// to dispatch through. This keeps the conversion mechanical at the
// call site (`if cli := adapter.X(); cli != nil { typedRPC(...) }`)
// without leaking pb-message construction across infra_*.go boundaries.
//
// Why a separate file: the typed adapter (iac_typed_adapter.go from
// PR #605) defines the marshalling helpers (refsToPB, specToPB,
// driftsFromPB, etc.) at file-scope; reusing them here keeps a single
// source of truth for the proto/Go shape conversions while letting
// each call site stay focused on its dispatch logic.

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// detectDriftConfigTyped invokes IaCProviderDriftConfigDetector.DetectDriftConfig
// via the supplied typed client and converts the response into the
// engine-side []interfaces.DriftResult shape callers consume.
//
// Used by `wfctl infra status drift` and `wfctl infra apply --refresh`
// as the typed replacement for the legacy
// `provider.(interfaces.DriftConfigDetector).DetectDriftWithSpecs(...)`
// dispatch.
func detectDriftConfigTyped(ctx context.Context, cli pb.IaCProviderDriftConfigDetectorClient, refs []interfaces.ResourceRef, specs map[string]interfaces.ResourceSpec) ([]interfaces.DriftResult, error) {
	pbSpecs := make(map[string]*pb.ResourceSpec, len(specs))
	for k, s := range specs {
		ps, err := specToPB(s)
		if err != nil {
			// Per code-review MINOR-2 (PR 618 round 4): name the offending
			// spec key so post-mortem debugging doesn't require crashing
			// through the marshalling helpers to find which entry in the
			// per-resource map blew up.
			return nil, fmt.Errorf("specToPB %q: %w", k, err)
		}
		pbSpecs[k] = ps
	}
	resp, err := cli.DetectDriftConfig(ctx, &pb.DetectDriftConfigRequest{
		Refs:  refsToPB(refs),
		Specs: pbSpecs,
	})
	if err != nil {
		// Per code-review IMPORTANT-1 (PR 618 round 4): translate
		// codes.Unimplemented at the wire boundary to
		// interfaces.ErrProviderMethodUnimplemented so callers using
		// errors.Is to detect "optional capability absent at runtime"
		// keep the signal. Without this, a plugin that registered the
		// IaCProviderDriftConfigDetector service but returns Unimplemented
		// at the RPC level (e.g., a provider whose DriftConfigDetector
		// is wired but the underlying driver doesn't support the
		// resource type) would surface as a generic gRPC error rather
		// than the iterate-and-skip sentinel. ADR-0028 §Migration's
		// "Strict-mode invariant translation" depends on this.
		return nil, translateRPCErr(err)
	}
	return driftsFromPB(resp.GetDrifts())
}

// validatePlanTyped invokes IaCProviderValidator.ValidatePlan via the
// supplied typed client. Replaces the legacy
// `provider.(interfaces.ProviderValidator).ValidatePlan(plan)` dispatch
// in infra_align_rules.go (R-A10 cross-resource constraint validation).
//
// The Go interfaces.ProviderValidator.ValidatePlan signature returns
// only []PlanDiagnostic (no error); errors are swallowed and surfaced
// as nil-diagnostics so callers that type-asserted-then-iterated
// continue to behave identically to "provider does not implement
// validation". This helper preserves that contract to keep R-A10
// behavior stable across the cutover.
func validatePlanTyped(ctx context.Context, cli pb.IaCProviderValidatorClient, plan *interfaces.IaCPlan) []interfaces.PlanDiagnostic {
	pbPlan, err := planToPB(plan)
	if err != nil {
		return nil
	}
	resp, err := cli.ValidatePlan(ctx, &pb.ValidatePlanRequest{Plan: pbPlan})
	if err != nil {
		return nil
	}
	out := make([]interfaces.PlanDiagnostic, 0, len(resp.GetDiagnostics()))
	for _, d := range resp.GetDiagnostics() {
		out = append(out, interfaces.PlanDiagnostic{
			Severity: planDiagnosticSeverityFromPB(d.GetSeverity()),
			Resource: d.GetResource(),
			Field:    d.GetField(),
			Message:  d.GetMessage(),
		})
	}
	return out
}
