package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── step.iac_provider_apply ─────────────────────────────────────────────────

// IaCProviderApplyStep implements the stateless two-phase apply:
//
//  1. Resolve the IaCProvider.
//  2. Recompute the desired-state hash from current live state.
//  3. Compare against the client-submitted desired_hash — mismatch → reject.
//  4. Dispatch via the injected applyFn (wfctlhelpers.ApplyPlanWithHooks in prod).
type IaCProviderApplyStep struct {
	name            string
	provider        string
	submittedHash   string
	desiredHashFrom string // dotted context path; mutually exclusive with submittedHash
	specs           []interfaces.ResourceSpec
	specsFrom       string // dotted context path; mutually exclusive with specs
	resources       []string
	app             modular.Application
	applyFn         func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error)
}

// NewIaCProviderApplyStepFactory returns a StepFactory for step.iac_provider_apply.
// applyFn is the apply dispatch function — pass wfctlhelpers.ApplyPlanWithHooks
// (with a nil-hooks wrapper) from the registration site in plugins/platform/plugin.go.
// Tests may inject a stub. The factory panics if applyFn is nil.
func NewIaCProviderApplyStepFactory(applyFn func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error)) StepFactory {
	if applyFn == nil {
		panic("NewIaCProviderApplyStepFactory: applyFn must not be nil")
	}
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_apply step %q: 'provider' is required", name)
		}

		specsFrom, _ := cfg["specs_from"].(string)
		_, hasStaticSpecs := cfg["specs"]
		rawResources, hasResourcesKey := cfg["resources"]
		resources, hasResources, err := parseResourceNames(rawResources, hasResourcesKey)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_apply step %q: parse resources: %w", name, err)
		}
		inputSources := 0
		if hasStaticSpecs {
			inputSources++
		}
		if specsFrom != "" {
			inputSources++
		}
		if hasResources {
			inputSources++
		}
		if inputSources > 1 {
			return nil, fmt.Errorf("iac_provider_apply step %q: 'specs', 'specs_from', and 'resources' are mutually exclusive", name)
		}

		desiredHashFrom, _ := cfg["desired_hash_from"].(string)
		submittedHash, _ := cfg["desired_hash"].(string)
		// Use key-presence (not value) for the one-of check, mirroring specs/specs_from,
		// so a config carrying both keys is rejected even if one decodes to null/"".
		_, hasStaticHash := cfg["desired_hash"]
		_, hasHashFrom := cfg["desired_hash_from"]
		if hasHashFrom && hasStaticHash {
			return nil, fmt.Errorf("iac_provider_apply step %q: 'desired_hash' and 'desired_hash_from' are mutually exclusive", name)
		}
		// Require exactly one hash source.
		if !hasHashFrom && !hasStaticHash {
			return nil, fmt.Errorf("iac_provider_apply step %q: one of 'desired_hash' or 'desired_hash_from' is required", name)
		}

		// Parse static specs (skipped when specs_from is set).
		var specs []interfaces.ResourceSpec
		if hasStaticSpecs {
			var err error
			specs, err = parseResourceSpecs(cfg["specs"])
			if err != nil {
				return nil, fmt.Errorf("iac_provider_apply step %q: parse specs: %w", name, err)
			}
		}

		return &IaCProviderApplyStep{
			name:            name,
			provider:        providerName,
			submittedHash:   submittedHash,
			desiredHashFrom: desiredHashFrom,
			specs:           specs,
			specsFrom:       specsFrom,
			resources:       resources,
			app:             app,
			applyFn:         applyFn,
		}, nil
	}
}

func (s *IaCProviderApplyStep) Name() string { return s.name }

func (s *IaCProviderApplyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve specs: dynamic path takes precedence when specsFrom is configured.
	specs := s.specs
	if s.specsFrom != "" {
		raw := resolveBodyFrom(s.specsFrom, pc)
		var err error
		specs, err = specparse.ParseResourceSpecs(raw)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_apply step %q: resolve specs_from %q: %w", s.name, s.specsFrom, err)
		}
		// Guard against zero specs: ParseResourceSpecs returns a non-nil empty
		// slice for []any{}, so a len check (not a nil check) is required —
		// applying over zero specs is a destroy-everything footgun.
		if len(specs) == 0 {
			return nil, fmt.Errorf("iac_provider_apply step %q: specs_from %q resolved to empty/zero specs", s.name, s.specsFrom)
		}
	}
	if len(s.resources) > 0 {
		var err error
		specs, err = resolveResourceSpecs(s.app, s.resources)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_apply step %q: resolve resources: %w", s.name, err)
		}
	}

	// Resolve submitted hash: dynamic path takes precedence when desiredHashFrom is configured.
	submittedHash := s.submittedHash
	if s.desiredHashFrom != "" {
		raw := resolveBodyFrom(s.desiredHashFrom, pc)
		var ok bool
		submittedHash, ok = raw.(string)
		if !ok || submittedHash == "" {
			return nil, fmt.Errorf("iac_provider_apply step %q: desired_hash_from %q did not resolve to a non-empty string (got %T)", s.name, s.desiredHashFrom, raw)
		}
	}

	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_apply")
	if err != nil {
		return nil, err
	}

	// Phase 1: recompute hash from current live state.
	statuses, err := provider.Status(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: Status: %w", s.name, err)
	}
	current := statusesToResourceStates(statuses)
	recomputedHash, err := computeDesiredStateHash(specs, current)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: compute desired hash: %w", s.name, err)
	}

	// Phase 2: guard — reject if hashes diverge (state changed or plan tampered).
	if recomputedHash != submittedHash {
		return nil, fmt.Errorf("iac_provider_apply step %q: plan hash mismatch (state changed or plan tampered); re-plan", s.name)
	}

	// Phase 3: build the plan and dispatch via the injected apply function.
	plan, err := provider.Plan(ctx, specs, current)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: Plan: %w", s.name, err)
	}
	if plan == nil {
		plan = &interfaces.IaCPlan{}
	}
	plan.DesiredHash = recomputedHash

	applyResult, err := s.applyFn(ctx, provider, plan)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: apply: %w", s.name, err)
	}

	// JSON-encode apply result for downstream consumers.
	resultJSON, err := json.Marshal(applyResult)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: marshal result: %w", s.name, err)
	}
	var resultAny any
	if err := json.Unmarshal(resultJSON, &resultAny); err != nil {
		return nil, fmt.Errorf("iac_provider_apply step %q: re-parse result: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"apply_result": resultAny,
		"desired_hash": recomputedHash,
		"provider":     s.provider,
		"action_count": len(plan.Actions),
	}}, nil
}
