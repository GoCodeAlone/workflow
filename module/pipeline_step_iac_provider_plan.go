package module

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── step.iac_provider_plan ──────────────────────────────────────────────────

// IaCProviderPlanStep resolves an IaCProvider, fetches current state, computes
// a DesiredStateHash with a NO-OP env resolver, builds a plan, and returns the
// plan JSON plus the stable hash.
type IaCProviderPlanStep struct {
	name     string
	provider string
	env      string
	specs    []interfaces.ResourceSpec
	app      modular.Application
}

// NewIaCProviderPlanStepFactory returns a StepFactory for step.iac_provider_plan.
func NewIaCProviderPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_plan step %q: 'provider' is required", name)
		}
		env, _ := cfg["env"].(string)

		// Parse specs from config. Each spec is a map[string]any with at least
		// name and type fields; config sub-map is optional.
		specs, err := parseResourceSpecs(cfg["specs"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_plan step %q: parse specs: %w", name, err)
		}

		return &IaCProviderPlanStep{
			name:     name,
			provider: providerName,
			env:      env,
			specs:    specs,
			app:      app,
		}, nil
	}
}

// parseResourceSpecs converts a raw config value ([]any of map[string]any) into
// []interfaces.ResourceSpec. A nil or missing "specs" key is allowed (returns empty
// slice) for providers that derive specs internally.
// Thin wrapper around specparse.ParseResourceSpecs; kept private so call sites
// in iac_provider_plan and iac_provider_apply are unchanged.
func parseResourceSpecs(raw any) ([]interfaces.ResourceSpec, error) {
	return specparse.ParseResourceSpecs(raw)
}

// parseResourceRefs converts a raw config value to []interfaces.ResourceRef.
func parseResourceRefs(raw any) ([]interfaces.ResourceRef, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("refs must be a list, got %T", raw)
	}
	refs := make([]interfaces.ResourceRef, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("refs[%d] must be a map, got %T", i, item)
		}
		ref := interfaces.ResourceRef{}
		if n, ok := m["name"].(string); ok {
			ref.Name = n
		}
		if t, ok := m["type"].(string); ok {
			ref.Type = t
		}
		if pid, ok := m["provider_id"].(string); ok {
			ref.ProviderID = pid
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *IaCProviderPlanStep) Name() string { return s.name }

func (s *IaCProviderPlanStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_plan")
	if err != nil {
		return nil, err
	}

	// Get current resource states via Status with empty refs (list all).
	statuses, err := provider.Status(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_plan step %q: Status: %w", s.name, err)
	}

	// Convert statuses to ResourceState for hash computation.
	current := statusesToResourceStates(statuses)

	// Compute desired state hash with a NO-OP env resolver so that
	// ${ENV_VAR} and ${secret.*} placeholders hash as literal strings —
	// same hash at plan time and at apply time regardless of env values.
	desiredHash, err := computeDesiredStateHash(s.specs, current)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_plan step %q: compute desired hash: %w", s.name, err)
	}

	// Build the plan from the provider.
	plan, err := provider.Plan(ctx, s.specs, current)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_plan step %q: Plan: %w", s.name, err)
	}

	// Attach the hash to the plan.
	if plan != nil {
		plan.DesiredHash = desiredHash
	}

	// JSON-encode the plan for downstream consumers (step.json_response etc.).
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_plan step %q: marshal plan: %w", s.name, err)
	}
	var planAny any
	if err := json.Unmarshal(planJSON, &planAny); err != nil {
		return nil, fmt.Errorf("iac_provider_plan step %q: re-parse plan: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"plan":         planAny,
		"desired_hash": desiredHash,
		"provider":     s.provider,
	}}, nil
}

// computeDesiredStateHash returns a stable SHA-256 hex digest of the canonical
// desired-state inputs, exactly mirroring wfctlhelpers.DesiredStateHash but
// inlined here to avoid the module→wfctlhelpers→module import cycle
// (wfctlhelpers/state.go imports module/). The algorithm is identical:
//
//  1. Build syncedOutputs from current state (name → {id: providerID, ...}).
//  2. Resolve ONLY ${MODULE.field} refs using a no-op env lookup so that
//     ${ENV_VAR} and ${secret.*} placeholders hash as their literal template
//     strings — hash is stable across env-value changes.
//  3. Sort resolved specs by name for stable ordering.
//  4. SHA-256 over the canonical JSON.
//
// An error is returned if marshalling the resolved specs fails. Callers MUST
// treat this as a hard failure — a constant fallback would bypass the tamper/drift
// guard.
func computeDesiredStateHash(desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (string, error) {
	// Step 1: build syncedOutputs from current state.
	syncedOutputs := make(map[string]map[string]any, len(current))
	for i := range current {
		s := &current[i]
		m := make(map[string]any, len(s.Outputs)+1)
		for k, v := range s.Outputs {
			m[k] = v
		}
		if s.ProviderID != "" {
			m["id"] = s.ProviderID
		}
		syncedOutputs[s.Name] = m
	}

	// Step 2: resolve specs with no-op env lookup (preserves ${ENV_VAR} verbatim).
	noopEnv := func(string) (string, bool) { return "", false }
	resolved := make([]interfaces.ResourceSpec, 0, len(desired))
	for _, spec := range desired {
		r, _, err := jitsubst.TryResolveSpec(spec, nil, syncedOutputs, noopEnv)
		if err != nil {
			r = spec // malformed ref — use unresolved spec
		}
		resolved = append(resolved, r)
	}

	// Step 3: sort by name for stable ordering.
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})

	// Step 4: SHA-256 over the canonical JSON. A marshal error here is a hard
	// failure — returning a constant fallback would silently match across plan and
	// apply and bypass the tamper/drift guard.
	data, err := json.Marshal(resolved)
	if err != nil {
		return "", fmt.Errorf("marshal resolved specs: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// statusesToResourceStates converts []interfaces.ResourceStatus to
// []interfaces.ResourceState for use as the "current" input to Plan and
// DesiredStateHash. Only Name, Type, and ProviderID are populated; Outputs
// are carried over for hash stability.
func statusesToResourceStates(statuses []interfaces.ResourceStatus) []interfaces.ResourceState {
	states := make([]interfaces.ResourceState, 0, len(statuses))
	for _, st := range statuses {
		states = append(states, interfaces.ResourceState{
			Name:       st.Name,
			Type:       st.Type,
			ProviderID: st.ProviderID,
			Outputs:    st.Outputs,
		})
	}
	return states
}
