package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/modular"
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
	name          string
	provider      string
	submittedHash string
	specs         []interfaces.ResourceSpec
	app           modular.Application
	applyFn       func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error)
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
		submittedHash, _ := cfg["desired_hash"].(string)
		if submittedHash == "" {
			return nil, fmt.Errorf("iac_provider_apply step %q: 'desired_hash' is required", name)
		}

		specs, err := parseResourceSpecs(cfg["specs"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_apply step %q: parse specs: %w", name, err)
		}

		return &IaCProviderApplyStep{
			name:          name,
			provider:      providerName,
			submittedHash: submittedHash,
			specs:         specs,
			app:           app,
			applyFn:       applyFn,
		}, nil
	}
}

func (s *IaCProviderApplyStep) Name() string { return s.name }

func (s *IaCProviderApplyStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
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
	recomputedHash := computeDesiredStateHash(s.specs, current)

	// Phase 2: guard — reject if hashes diverge (state changed or plan tampered).
	if recomputedHash != s.submittedHash {
		return nil, fmt.Errorf("iac_provider_apply step %q: plan hash mismatch (state changed or plan tampered); re-plan", s.name)
	}

	// Phase 3: build the plan and dispatch via the injected apply function.
	plan, err := provider.Plan(ctx, s.specs, current)
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
