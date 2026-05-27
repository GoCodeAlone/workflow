package handler

import (
	"context"
	"encoding/json"
	"sort"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// GetResource implements InfraAdminService.GetResource by reading
// the named ResourceState from the host's iac.state backend and
// projecting it into an AdminResourceDetail. AppliedConfig is
// JSON-encoded into applied_config_json verbatim; Outputs are
// MASKED via secrets.MaskSensitiveOutputs against
// secrets.DefaultSensitiveKeys() and then JSON-encoded into
// outputs_json. The masked-key names are surfaced in
// sensitive_outputs_redacted so the UI can render a "redacted"
// affordance per design §Secret redaction:
//
//	"GetResource.outputs_json redacts keys matching
//	 secrets.DefaultSensitiveKeys()."
//
// Per design §Authz row: default-deny when evidence is missing or
// either authz_checked / authz_allowed is false; refusal surfaces
// via Output.error rather than a Go-level error.
//
// Not-found surfaces via Output.error too — the design treats
// missing resources as a non-exceptional condition the UI must
// handle (e.g. a stale URL after a destroy).
func GetResource(
	ctx context.Context,
	store interfaces.IaCStateStore,
	in *adminpb.AdminGetResourceInput,
) (*adminpb.AdminGetResourceOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminGetResourceOutput{Error: msg}, nil
	}

	state, err := store.GetResource(ctx, in.GetName())
	if err != nil {
		// Intentional nilerr — see list_resources.go::ListResources for
		// the proto tag-100 rationale (errors surface via Output.error,
		// not Go-level errors, so HTTP transport returns 200 OK + typed
		// payload).
		return &adminpb.AdminGetResourceOutput{Error: "get state: " + err.Error()}, nil //nolint:nilerr
	}
	if state == nil {
		return &adminpb.AdminGetResourceOutput{Error: "resource not found: " + in.GetName()}, nil
	}

	appliedJSON, err := json.Marshal(state.AppliedConfig)
	if err != nil {
		return &adminpb.AdminGetResourceOutput{Error: "marshal applied_config: " + err.Error()}, nil //nolint:nilerr
	}

	maskedOutputs, redactedKeys := maskOutputsForWire(state.Outputs)
	outputsJSON, err := json.Marshal(maskedOutputs)
	if err != nil {
		return &adminpb.AdminGetResourceOutput{Error: "marshal outputs: " + err.Error()}, nil //nolint:nilerr
	}

	return &adminpb.AdminGetResourceOutput{
		Resource: &adminpb.AdminResourceDetail{
			Summary:                  stateToSummary(state),
			AppliedConfigJson:        appliedJSON,
			OutputsJson:              outputsJSON,
			ConfigHash:               state.ConfigHash,
			LastDriftCheckUnix:       state.LastDriftCheck.Unix(),
			SensitiveOutputsRedacted: redactedKeys,
		},
	}, nil
}

// maskOutputsForWire returns a copy of outputs with sensitive values
// masked + the sorted list of keys that WERE masked. Implementation
// is independent of secrets.MaskSensitiveOutputs (which doesn't
// surface the list of redacted keys); see design §Secret redaction.
//
// "Sensitive" means a key matches one of secrets.DefaultSensitiveKeys()
// — the host-side authoritative list. Future enhancement: merge
// driver-specific keys via secrets.MergeSensitiveKeys; v1 sticks to
// defaults so the masking surface is stable across providers.
//
// Returns the same underlying map reference when outputs has no
// sensitive keys (no copy needed); on any redaction it returns a
// new map so the caller cannot accidentally mutate the original
// state's outputs.
func maskOutputsForWire(outputs map[string]any) (map[string]any, []string) {
	if len(outputs) == 0 {
		return outputs, nil
	}
	sensitive := map[string]struct{}{}
	for _, k := range secrets.DefaultSensitiveKeys() {
		sensitive[k] = struct{}{}
	}
	var redacted []string
	masked := make(map[string]any, len(outputs))
	for k, v := range outputs {
		if _, ok := sensitive[k]; ok {
			masked[k] = "(sensitive)"
			redacted = append(redacted, k)
		} else {
			masked[k] = v
		}
	}
	sort.Strings(redacted) // deterministic ordering for snapshot tests + diff-friendly UI
	if len(redacted) == 0 {
		// No redaction happened — return original to avoid an unnecessary copy.
		return outputs, nil
	}
	return masked, redacted
}
