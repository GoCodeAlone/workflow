package wfctlhelpers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DesiredStateHash returns a stable SHA-256 hex digest of the canonical
// desired-state inputs: specs with ${MODULE.id} refs collapsed to current
// state ProviderIDs, then sorted by name and JSON-serialised.
//
// IMPORTANT — env/secret refs are preserved verbatim (NOT resolved):
// ${ENV_VAR} and ${secret.*} placeholders hash as their literal template
// strings. This is deliberate: these values may differ between processes
// (secret-gen vars, os.Getenv) but must produce the same hash at plan
// time and at apply time. Env drift is tracked separately via the
// plan's InputSnapshot / InputDriftReport mechanism, not the hash.
// Collapsing env refs here caused plan-hash ≠ apply-hash regressions
// (TestParseInfraResourceSpecs_Preserves*).
//
// Only ${MODULE.field} refs whose source is present in `current` are
// collapsed — these are stable ProviderIDs that will not change between
// plan and apply for the same desired configuration.
//
// cfg and env are reserved for a future extension that threads cfg+env
// through buildResolvedSecretsFromState to achieve full CLI parity for
// infra_output-typed secrets. They are intentionally unused today.
//
// The empty-specs case hashes "[]" (not the empty string); the
// returned "hash-error" string is the unreachable marshal-failure sentinel.
func DesiredStateHash(cfg *config.WorkflowConfig, desired []interfaces.ResourceSpec, current []interfaces.ResourceState, env string) string {
	_ = cfg // reserved: see godoc
	_ = env // reserved: see godoc

	// Step 1: build syncedOutputs from current state.
	// Maps module-name → {"id": providerID, <other outputs>}
	syncedOutputs := buildHashSyncedOutputs(current)

	// Step 2: collapse only ${MODULE.field} refs that are resolvable from
	// current state. Use a no-op env lookup so ${ENV_VAR} and ${secret.*}
	// placeholders are preserved verbatim — they must hash identically
	// at plan time and at apply time for plan↔apply stability.
	noopEnv := func(string) (string, bool) { return "", false }
	resolved := make([]interfaces.ResourceSpec, 0, len(desired))
	for _, spec := range desired {
		r, _, err := jitsubst.TryResolveSpec(spec, nil, syncedOutputs, noopEnv)
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
		// Unreachable for YAML-decoded ResourceSpec structs, but return a
		// non-matchable sentinel distinct from any valid hash so callers
		// treat it as "hash unavailable" rather than "empty desired set".
		return "hash-error"
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
