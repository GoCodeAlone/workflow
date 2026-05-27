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

	detail := &adminpb.AdminResourceDetail{
		Summary:                  stateToSummary(state),
		AppliedConfigJson:        appliedJSON,
		OutputsJson:              outputsJSON,
		ConfigHash:               state.ConfigHash,
		SensitiveOutputsRedacted: redactedKeys,
	}
	// Guard against zero time.Time → year-1 BCE Unix epoch (per
	// code-reviewer T5 M-2). Resources that have never been drift-
	// checked carry a zero LastDriftCheck; leave the proto field at
	// 0 so the JS fmtTs helper's `!unix` check renders "—".
	if !state.LastDriftCheck.IsZero() {
		detail.LastDriftCheckUnix = state.LastDriftCheck.Unix()
	}
	return &adminpb.AdminGetResourceOutput{Resource: detail}, nil
}

// maskOutputsForWire returns the masked outputs map + the sorted list
// of keys that WERE masked. The masking itself is delegated to
// secrets.MaskSensitiveOutputs so the handler library and any other
// caller of that helper agree byte-for-byte on the redaction
// algorithm — single source of truth. The handler's own contract
// (per design §Secret redaction) is the additional
// `sensitive_outputs_redacted` list, which the helper does NOT
// surface; we compute it here via one extra pass over the map.
//
// Per code-reviewer T5 I-1 (commit 5fe88fe45): an earlier draft
// hand-rolled the masking with a duplicate `(sensitive)` literal,
// which would have silently drifted if secrets ever extended its
// helper to do partial-value masking. Routing through
// secrets.MaskSensitiveOutputs eliminates that drift surface — same
// bug class as T1's sanitizeStateID allowlist-vs-replacer
// divergence the same reviewer caught earlier.
//
// "Sensitive" means a key matches one of secrets.DefaultSensitiveKeys()
// — the host-side authoritative list. Future enhancement: merge
// driver-specific keys via secrets.MergeSensitiveKeys; v1 sticks to
// defaults so the masking surface is stable across providers.
func maskOutputsForWire(outputs map[string]any) (map[string]any, []string) {
	if len(outputs) == 0 {
		return outputs, nil
	}
	keys := secrets.DefaultSensitiveKeys()
	sensitiveSet := make(map[string]bool, len(keys))
	for _, k := range keys {
		sensitiveSet[k] = true
	}
	var redacted []string
	for k := range outputs {
		if sensitiveSet[k] {
			redacted = append(redacted, k)
		}
	}
	if len(redacted) == 0 {
		// No sensitive keys present — return original (no copy) so the
		// caller cannot accidentally mutate the original state's
		// outputs but we don't pay for an unused allocation either.
		return outputs, nil
	}
	sort.Strings(redacted) // deterministic ordering for snapshot tests + diff-friendly UI
	return secrets.MaskSensitiveOutputs(outputs, keys), redacted
}
