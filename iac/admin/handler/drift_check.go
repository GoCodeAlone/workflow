package handler

import (
	"context"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DriftCheckResource implements the DriftCheckResource RPC: calls
// provider.DetectDrift on the supplied resource refs and returns
// per-resource drift results.
//
// The providers map is keyed by module name; the first entry is used
// (single-provider model for v1.1). When no providers are registered,
// Output.error is set so the client receives a typed diagnostic.
//
// Evidence default-deny: if authzError is non-empty, the handler
// short-circuits with Output.error (HTTP stays 200; consumer sniffs tag-100).
func DriftCheckResource(
	ctx context.Context,
	providers map[string]interfaces.IaCProvider,
	in *adminpb.AdminDriftInput,
) (*adminpb.AdminDriftOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminDriftOutput{Error: msg}, nil
	}
	if len(providers) == 0 {
		return &adminpb.AdminDriftOutput{Error: "drift: no iac.provider registered"}, nil
	}

	// Select the first provider.
	var prov interfaces.IaCProvider
	for _, p := range providers {
		prov = p
		break
	}

	// Convert proto refs → interfaces.ResourceRef.
	refs := make([]interfaces.ResourceRef, 0, len(in.GetRefs()))
	for _, r := range in.GetRefs() {
		refs = append(refs, interfaces.ResourceRef{
			Name: r.GetName(),
			Type: r.GetType(),
		})
	}

	results, err := prov.DetectDrift(ctx, refs)
	if err != nil {
		return &adminpb.AdminDriftOutput{Error: "drift: " + err.Error()}, nil //nolint:nilerr
	}

	drift := make([]*adminpb.AdminDriftResult, 0, len(results))
	for _, r := range results {
		drift = append(drift, &adminpb.AdminDriftResult{
			ResourceName: r.Name,
			Type:         r.Type,
			Drifted:      r.Drifted,
			Class:        string(r.Class),
			Fields:       r.Fields,
		})
	}
	return &adminpb.AdminDriftOutput{Drift: drift}, nil
}
