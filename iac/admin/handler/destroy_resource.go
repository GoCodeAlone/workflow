package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DestroyResource implements the DestroyResource RPC.
//
// Security gates:
//  1. authzError: default-deny if evidence missing/unchecked.
//  2. Server-side Enforcer: Enforce(subject,"infra:destroy","allow").
//  3. TOCTOU: confirm_hash must match a server-computed hash of the refs
//     being destroyed. Clients compute this hash from the resource list
//     they obtained from ListResources/GetResource and echo it here;
//     a mismatch means the list changed between listing and destroying.
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

	// Gate 3: TOCTOU — confirm_hash must match server-computed hash of the refs.
	// An empty confirm_hash means the client skipped the TOCTOU step; reject.
	expectedHash := hashDestroyRefs(in.GetRefs())
	if in.GetConfirmHash() != expectedHash {
		return &adminpb.AdminDestroyOutput{Error: "destroy: confirm_hash mismatch — resource list has changed since this destroy was initiated"}, nil
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

// HashDestroyRefs computes a deterministic SHA-256 hex digest of the refs
// being destroyed. Exported so UI and tests can compute the expected hash
// before calling DestroyResource. The hash is over the refs sorted by Name,
// serialised as JSON [{name,type},...].
func HashDestroyRefs(refs []*adminpb.AdminResourceRef) string {
	return hashDestroyRefs(refs)
}

// hashDestroyRefs is the internal implementation. Sorted so order of refs
// in the request body doesn't affect the hash.
func hashDestroyRefs(refs []*adminpb.AdminResourceRef) string {
	type refKey struct{ Name, Type string }
	sorted := make([]refKey, 0, len(refs))
	for _, r := range refs {
		sorted = append(sorted, refKey{r.GetName(), r.GetType()})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	data, err := json.Marshal(sorted)
	if err != nil {
		// Use a non-matchable sentinel so an empty client confirm_hash
		// never accidentally satisfies the gate on a marshal failure.
		return fmt.Sprintf("hash-error-%d-refs", len(refs))
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
