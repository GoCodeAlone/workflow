package handler

import (
	"context"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DestroyResource implements the DestroyResource RPC.
//
// Security gates:
//  1. authzError: default-deny if evidence missing/unchecked.
//  2. Server-side Enforcer: Enforce(subject,"infra:destroy","allow").
//
// The destroy path calls provider.Destroy directly (no plan step —
// the caller supplies refs from the UI's resource list, which already
// came from the state store).
func DestroyResource(
	ctx context.Context,
	providers map[string]interfaces.IaCProvider,
	authz Enforcer,
	subject string,
	in *adminpb.AdminDestroyInput,
) (*adminpb.AdminDestroyOutput, error) {
	// Gate 1: default-deny.
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminDestroyOutput{Error: msg}, nil
	}

	// Gate 2: server-side RBAC.
	if authz != nil {
		ok, enforceErr := authz.Enforce(subject, "infra:destroy", "allow")
		if enforceErr != nil {
			return &adminpb.AdminDestroyOutput{Error: "destroy: authz enforce error"}, nil //nolint:nilerr
		}
		if !ok {
			return &adminpb.AdminDestroyOutput{Error: "destroy: infra:destroy denied for subject " + subject}, nil
		}
	}

	if len(providers) == 0 {
		return &adminpb.AdminDestroyOutput{Error: "destroy: no iac.provider registered"}, nil
	}

	// Select the first provider.
	var prov interfaces.IaCProvider
	for _, p := range providers {
		prov = p
		break
	}

	// Convert proto refs to interfaces.ResourceRef.
	refs := make([]interfaces.ResourceRef, 0, len(in.GetRefs()))
	for _, r := range in.GetRefs() {
		refs = append(refs, interfaces.ResourceRef{
			Name: r.GetName(),
			Type: r.GetType(),
		})
	}

	result, err := prov.Destroy(ctx, refs)
	if err != nil {
		return &adminpb.AdminDestroyOutput{Error: "destroy: " + redactCredentials(err.Error())}, nil //nolint:nilerr
	}
	if result == nil {
		return &adminpb.AdminDestroyOutput{}, nil
	}

	out := &adminpb.AdminDestroyOutput{
		Destroyed: result.Destroyed,
	}
	for i := range result.Errors {
		e := &result.Errors[i]
		out.Errors = append(out.Errors, &adminpb.AdminActionError{
			Resource: e.Resource,
			Action:   e.Action,
			Error:    redactCredentials(e.Error),
		})
	}
	return out, nil
}
