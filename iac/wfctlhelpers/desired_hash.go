package wfctlhelpers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DesiredStateHash returns a stable SHA-256 hex digest of the canonical
// desired-state inputs: specs resolved via plan-time JIT substitution
// (${MODULE.id} refs collapsed to current state ProviderIDs), then sorted
// by name and JSON-serialised.
//
// The function mirrors the CLI path in cmd/wfctl (resolveSpecsAgainstState
// followed by desiredStateHash) so that in-process plan and apply calls
// produce the same digest as the wfctl command-line tool. It is the stable
// seam for the two-phase (plan→apply) TOCTOU guard in infra.admin handlers.
//
// The cfg parameter is reserved for future secret-resolution parity with the
// CLI's buildResolvedSecretsFromState / buildRuntimeOnlySecretKeys path. When
// cfg is nil, only module-output refs (${MODULE.field}) are collapsed; env-var
// and secret refs fall through to os.LookupEnv unchanged, matching the
// CLI's lenient plan-time behavior for refs not present in current state.
//
// The empty-specs case hashes the JSON array "[]" (not the empty string); the
// returned empty string is the error sentinel (marshal failure) only.
func DesiredStateHash(cfg *config.WorkflowConfig, desired []interfaces.ResourceSpec, current []interfaces.ResourceState, _ string) string {
	// Step 1: build syncedOutputs from current state.
	// Maps module-name → {"id": providerID, <other outputs>}
	syncedOutputs := buildHashSyncedOutputs(current)

	// Step 2: apply plan-time JIT resolution to collapse ${MODULE.id} refs.
	// Unresolved refs (not in state) are left as-is (lenient / no error).
	resolved := make([]interfaces.ResourceSpec, 0, len(desired))
	for _, spec := range desired {
		r, _, err := jitsubst.TryResolveSpec(spec, nil, syncedOutputs, os.LookupEnv)
		if err != nil {
			// Malformed ref — use unresolved spec; hash will differ from a
			// clean spec (deterministic for same bad input).
			r = spec
		}
		resolved = append(resolved, r)
	}

	// Step 3: sort by name for stable ordering regardless of caller order.
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})

	// Step 4: SHA-256 over the canonical JSON.
	data, err := json.Marshal(resolved)
	if err != nil {
		// Should never happen for YAML-decoded structs; return the error
		// sentinel ("") so callers reject the plan with a clear message.
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// buildHashSyncedOutputs converts current ResourceState slice into the
// module-output map consumed by jitsubst.TryResolveSpec. The canonical
// "id" key is the ProviderID; any other resource Outputs are also included.
// This mirrors cmd/wfctl's buildSyncedOutputsFromState.
func buildHashSyncedOutputs(states []interfaces.ResourceState) map[string]map[string]any {
	out := make(map[string]map[string]any, len(states))
	for i := range states {
		s := &states[i]
		m := make(map[string]any, len(s.Outputs)+1)
		for k, v := range s.Outputs {
			m[k] = v
		}
		if s.ProviderID != "" {
			m["id"] = s.ProviderID
		}
		out[s.Name] = m
	}
	return out
}
