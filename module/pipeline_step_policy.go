package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// resolvePolicyEngine looks up a PolicyEngineModule from the service registry.
func resolvePolicyEngine(app modular.Application, engineName, stepName string) (*PolicyEngineModule, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[engineName]
	if !ok {
		return nil, fmt.Errorf("step %q: policy engine %q not found in registry", stepName, engineName)
	}
	eng, ok := svc.(*PolicyEngineModule)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement PolicyEngineModule (got %T)", stepName, engineName, svc)
	}
	return eng, nil
}

// ─── step.policy_evaluate ───────────────────────────────────────────────────

// PolicyEvaluateStep evaluates a policy decision from the pipeline context.
type PolicyEvaluateStep struct {
	name       string
	engineName string
	inputFrom  string
	app        modular.Application
}

// NewPolicyEvaluateStepFactory returns a StepFactory for step.policy_evaluate.
func NewPolicyEvaluateStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		engineName, _ := cfg["engine"].(string)
		if engineName == "" {
			return nil, fmt.Errorf("policy_evaluate step %q: 'engine' is required", name)
		}
		inputFrom, _ := cfg["input_from"].(string)
		if inputFrom == "" {
			inputFrom = "policy_input"
		}
		return &PolicyEvaluateStep{
			name:       name,
			engineName: engineName,
			inputFrom:  inputFrom,
			app:        app,
		}, nil
	}
}

func (s *PolicyEvaluateStep) Name() string { return s.name }

func (s *PolicyEvaluateStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	eng, err := resolvePolicyEngine(s.app, s.engineName, s.name)
	if err != nil {
		return nil, err
	}

	// Resolve input: from pipeline context key or use full context.
	var input map[string]any
	if raw, ok := pc.Current[s.inputFrom]; ok {
		if m, ok := raw.(map[string]any); ok {
			input = m
		}
	}
	if input == nil {
		input = pc.Current
	}

	decision, err := eng.Engine().Evaluate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("policy_evaluate step %q: evaluate: %w", s.name, err)
	}

	result := &StepResult{Output: map[string]any{
		"allowed":  decision.Allowed,
		"reasons":  decision.Reasons,
		"metadata": decision.Metadata,
		"engine":   s.engineName,
	}}

	// Stop the pipeline if the policy denies.
	if !decision.Allowed {
		result.Stop = true
	}

	return result, nil
}

// ─── step.policy_load ───────────────────────────────────────────────────────

// PolicyLoadStep loads a policy document into the engine.
type PolicyLoadStep struct {
	name       string
	engineName string
	policyName string
	content    string
	app        modular.Application
}

// NewPolicyLoadStepFactory returns a StepFactory for step.policy_load.
func NewPolicyLoadStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		engineName, _ := cfg["engine"].(string)
		if engineName == "" {
			return nil, fmt.Errorf("policy_load step %q: 'engine' is required", name)
		}
		policyName, _ := cfg["policy_name"].(string)
		if policyName == "" {
			return nil, fmt.Errorf("policy_load step %q: 'policy_name' is required", name)
		}
		content, _ := cfg["content"].(string)
		return &PolicyLoadStep{
			name:       name,
			engineName: engineName,
			policyName: policyName,
			content:    content,
			app:        app,
		}, nil
	}
}

func (s *PolicyLoadStep) Name() string { return s.name }

func (s *PolicyLoadStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	eng, err := resolvePolicyEngine(s.app, s.engineName, s.name)
	if err != nil {
		return nil, err
	}

	// Allow content override from pipeline context.
	content := s.content
	if raw, ok := pc.Current["policy_content"].(string); ok && raw != "" {
		content = raw
	}

	if err := eng.Engine().LoadPolicy(s.policyName, content); err != nil {
		return nil, fmt.Errorf("policy_load step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"loaded":      true,
		"policy_name": s.policyName,
		"engine":      s.engineName,
	}}, nil
}

// ─── step.policy_list ───────────────────────────────────────────────────────

// PolicyListStep lists all registered policies in the engine.
type PolicyListStep struct {
	name       string
	engineName string
	app        modular.Application
}

// NewPolicyListStepFactory returns a StepFactory for step.policy_list.
func NewPolicyListStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		engineName, _ := cfg["engine"].(string)
		if engineName == "" {
			return nil, fmt.Errorf("policy_list step %q: 'engine' is required", name)
		}
		return &PolicyListStep{name: name, engineName: engineName, app: app}, nil
	}
}

func (s *PolicyListStep) Name() string { return s.name }

func (s *PolicyListStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	eng, err := resolvePolicyEngine(s.app, s.engineName, s.name)
	if err != nil {
		return nil, err
	}

	policies := eng.Engine().ListPolicies()
	out := make([]map[string]any, len(policies))
	for i, p := range policies {
		out[i] = map[string]any{
			"name":    p.Name,
			"backend": p.Backend,
		}
	}

	return &StepResult{Output: map[string]any{
		"policies": out,
		"count":    len(policies),
		"engine":   s.engineName,
	}}, nil
}

// ─── step.policy_test ───────────────────────────────────────────────────────

// PolicyTestStep evaluates a policy against sample inputs (dry-run).
type PolicyTestStep struct {
	name        string
	engineName  string
	sampleInput map[string]any
	expectAllow bool
	app         modular.Application
}

// NewPolicyTestStepFactory returns a StepFactory for step.policy_test.
func NewPolicyTestStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		engineName, _ := cfg["engine"].(string)
		if engineName == "" {
			return nil, fmt.Errorf("policy_test step %q: 'engine' is required", name)
		}
		sampleInput, _ := cfg["sample_input"].(map[string]any)
		expectAllow := true
		if v, ok := cfg["expect_allow"].(bool); ok {
			expectAllow = v
		}
		return &PolicyTestStep{
			name:        name,
			engineName:  engineName,
			sampleInput: sampleInput,
			expectAllow: expectAllow,
			app:         app,
		}, nil
	}
}

func (s *PolicyTestStep) Name() string { return s.name }

func (s *PolicyTestStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	eng, err := resolvePolicyEngine(s.app, s.engineName, s.name)
	if err != nil {
		return nil, err
	}

	input := s.sampleInput
	if input == nil {
		input = map[string]any{}
	}

	decision, err := eng.Engine().Evaluate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("policy_test step %q: evaluate: %w", s.name, err)
	}

	passed := decision.Allowed == s.expectAllow
	result := &StepResult{Output: map[string]any{
		"test_passed":  passed,
		"allowed":      decision.Allowed,
		"expect_allow": s.expectAllow,
		"reasons":      decision.Reasons,
		"metadata":     decision.Metadata,
		"engine":       s.engineName,
		"sample_input": input,
	}}

	if !passed {
		result.Stop = true
	}

	return result, nil
}
