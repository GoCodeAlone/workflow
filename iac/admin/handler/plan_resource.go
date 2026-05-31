package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// PlanResource implements the PlanResource RPC: plans the in-process
// desired specs against the current state, returning the plan actions
// and a desired_hash for TOCTOU protection.
//
// Signature deviates from the v1 (ctx, state, providers, fieldCatalog, in)
// shape by adding cfg + desiredSpecs (plan-review I-2): the handler needs
// cfg to compute DesiredStateHash correctly and desiredSpecs to scope the
// plan without coupling the handler to the module's config loading.
//
// The providers map is keyed by module name; the first entry is used for
// planning (single-provider per-route model for v1.1).
//
// Evidence default-deny: if authzError is non-empty, the handler
// short-circuits with Output.error (HTTP stays 200; consumer sniffs tag-100).
func PlanResource(
	ctx context.Context,
	store interfaces.IaCStateStore, //nolint:revive // nil ok when no state needed (e.g. fresh deploy)
	providers map[string]interfaces.IaCProvider,
	cfg *config.WorkflowConfig, //nolint:revive // reserved for wfctlhelpers.DesiredStateHash secret-resolution
	desiredSpecs []interfaces.ResourceSpec,
	in *adminpb.AdminPlanInput,
) (*adminpb.AdminPlanOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminPlanOutput{Error: msg}, nil
	}
	if len(providers) == 0 {
		return &adminpb.AdminPlanOutput{Error: "plan: no iac.provider registered"}, nil
	}

	// Select the first provider (single-provider path for v1.1).
	var prov interfaces.IaCProvider
	for _, p := range providers {
		prov = p
		break
	}

	// Load current state for hash-input resolution and plan baseline.
	var current []interfaces.ResourceState
	if store != nil {
		var err error
		current, err = store.ListResources(ctx)
		if err != nil {
			return &adminpb.AdminPlanOutput{Error: "plan: list state: " + err.Error()}, nil //nolint:nilerr
		}
	}

	// Apply app_context / resource_filter scoping.
	filtered := filterPlanSpecs(desiredSpecs, in.GetAppContext(), in.GetResourceFilter())

	// Compute the desired hash before planning (hash is over desired
	// inputs, not the plan output — matches the CLI path).
	desiredHash := handlerDesiredHash(cfg, filtered, current)

	// Delegate planning to the provider.
	plan, err := prov.Plan(ctx, filtered, current)
	if err != nil {
		return &adminpb.AdminPlanOutput{Error: "plan: " + err.Error()}, nil //nolint:nilerr
	}
	if plan == nil {
		plan = &interfaces.IaCPlan{}
	}

	// Stamp hash for TOCTOU.
	plan.DesiredHash = desiredHash

	// Serialise plan to JSON for the plan_json opaque payload.
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return &adminpb.AdminPlanOutput{Error: "plan: marshal: " + err.Error()}, nil //nolint:nilerr
	}

	// Map plan actions to proto.
	actions := make([]*adminpb.AdminPlanAction, 0, len(plan.Actions))
	for i := range plan.Actions {
		a := &plan.Actions[i]
		actions = append(actions, &adminpb.AdminPlanAction{
			ActionType:    a.Action,
			ResourceName:  a.Resource.Name,
			Type:          a.Resource.Type,
			ChangeSummary: summariseChanges(a.Changes),
		})
	}

	return &adminpb.AdminPlanOutput{
		PlanId:      fmt.Sprintf("plan-%s", desiredHash[:16]),
		DesiredHash: desiredHash,
		Actions:     actions,
		PlanJson:    planJSON,
	}, nil
}

// filterPlanSpecs applies the optional app_context and resource_filter
// predicates from the PlanResource input to narrow the desired spec set.
// Both filters are AND-ed when non-empty; an empty filter matches everything.
// app_context is matched against the "app_context" label in spec.Config["labels"],
// following the same convention as stateToSummary. resource_filter is matched
// by resource Name.
func filterPlanSpecs(specs []interfaces.ResourceSpec, appCtx, resourceFilter string) []interfaces.ResourceSpec {
	if appCtx == "" && resourceFilter == "" {
		return specs
	}
	out := make([]interfaces.ResourceSpec, 0, len(specs))
	for i := range specs {
		s := &specs[i]
		if resourceFilter != "" && s.Name != resourceFilter {
			continue
		}
		if appCtx != "" {
			labels, _ := s.Config["labels"].(map[string]any)
			if ac, _ := labels["app_context"].(string); ac != appCtx {
				continue
			}
		}
		out = append(out, *s)
	}
	return out
}

// summariseChanges produces a short human-readable summary of the
// field-level changes in a plan action. Returns "" for create/delete
// where no diff is expected.
func summariseChanges(changes []interfaces.FieldChange) string {
	if len(changes) == 0 {
		return ""
	}
	return fmt.Sprintf("%d field(s) changed", len(changes))
}

// DesiredHash mirrors wfctlhelpers.DesiredStateHash but is defined here
// to avoid an import cycle (iac/wfctlhelpers → module → iac/admin/handler).
// Exported so iac/wfctlhelpers/desired_hash_test.go can assert both
// implementations produce identical digests for the same inputs, preventing
// silent copy-drift. cfg is reserved for future secret-resolution parity.
func DesiredHash(cfg *config.WorkflowConfig, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) string {
	return handlerDesiredHash(cfg, desired, current)
}

// handlerDesiredHash is the internal implementation; callers within the
// handler package use this directly; external callers use DesiredHash.
func handlerDesiredHash(_ *config.WorkflowConfig, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) string {
	// Build syncedOutputs from current state (module name → outputs + "id").
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

	// Resolve ${MODULE.id} refs; unresolved refs left as-is (lenient).
	resolved := make([]interfaces.ResourceSpec, 0, len(desired))
	for _, spec := range desired {
		r, _, err := jitsubst.TryResolveSpec(spec, nil, syncedOutputs, os.LookupEnv)
		if err != nil {
			r = spec
		}
		resolved = append(resolved, r)
	}

	// Sort by name for stable ordering.
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})

	data, err := json.Marshal(resolved)
	if err != nil {
		return "" // error sentinel — callers treat "" as "hash unavailable"
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
