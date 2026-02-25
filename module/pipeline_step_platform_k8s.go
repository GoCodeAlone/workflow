package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── k8s_plan ────────────────────────────────────────────────────────────────

// K8sPlanStep calls Plan() on a named platform.kubernetes module.
type K8sPlanStep struct {
	name    string
	cluster string
	app     modular.Application
}

// NewK8sPlanStepFactory returns a StepFactory for step.k8s_plan.
func NewK8sPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		cluster, _ := cfg["cluster"].(string)
		if cluster == "" {
			return nil, fmt.Errorf("k8s_plan step %q: 'cluster' is required", name)
		}
		return &K8sPlanStep{name: name, cluster: cluster, app: app}, nil
	}
}

func (s *K8sPlanStep) Name() string { return s.name }

func (s *K8sPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	k, err := resolveK8sModule(s.app, s.cluster, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := k.Plan()
	if err != nil {
		return nil, fmt.Errorf("k8s_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":     plan,
		"cluster":  s.cluster,
		"provider": plan.Provider,
		"actions":  plan.Actions,
	}}, nil
}

// ─── k8s_apply ───────────────────────────────────────────────────────────────

// K8sApplyStep calls Apply() on a named platform.kubernetes module.
type K8sApplyStep struct {
	name    string
	cluster string
	app     modular.Application
}

// NewK8sApplyStepFactory returns a StepFactory for step.k8s_apply.
func NewK8sApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		cluster, _ := cfg["cluster"].(string)
		if cluster == "" {
			return nil, fmt.Errorf("k8s_apply step %q: 'cluster' is required", name)
		}
		return &K8sApplyStep{name: name, cluster: cluster, app: app}, nil
	}
}

func (s *K8sApplyStep) Name() string { return s.name }

func (s *K8sApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	k, err := resolveK8sModule(s.app, s.cluster, s.name)
	if err != nil {
		return nil, err
	}
	result, err := k.Apply()
	if err != nil {
		return nil, fmt.Errorf("k8s_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"result":  result,
		"cluster": s.cluster,
		"success": result.Success,
		"message": result.Message,
		"state":   result.State,
	}}, nil
}

// ─── k8s_status ──────────────────────────────────────────────────────────────

// K8sStatusStep calls Status() on a named platform.kubernetes module.
type K8sStatusStep struct {
	name    string
	cluster string
	app     modular.Application
}

// NewK8sStatusStepFactory returns a StepFactory for step.k8s_status.
func NewK8sStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		cluster, _ := cfg["cluster"].(string)
		if cluster == "" {
			return nil, fmt.Errorf("k8s_status step %q: 'cluster' is required", name)
		}
		return &K8sStatusStep{name: name, cluster: cluster, app: app}, nil
	}
}

func (s *K8sStatusStep) Name() string { return s.name }

func (s *K8sStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	k, err := resolveK8sModule(s.app, s.cluster, s.name)
	if err != nil {
		return nil, err
	}
	st, err := k.Status()
	if err != nil {
		return nil, fmt.Errorf("k8s_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"status":  st,
		"cluster": s.cluster,
	}}, nil
}

// ─── k8s_destroy ─────────────────────────────────────────────────────────────

// K8sDestroyStep calls Destroy() on a named platform.kubernetes module.
type K8sDestroyStep struct {
	name    string
	cluster string
	app     modular.Application
}

// NewK8sDestroyStepFactory returns a StepFactory for step.k8s_destroy.
func NewK8sDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		cluster, _ := cfg["cluster"].(string)
		if cluster == "" {
			return nil, fmt.Errorf("k8s_destroy step %q: 'cluster' is required", name)
		}
		return &K8sDestroyStep{name: name, cluster: cluster, app: app}, nil
	}
}

func (s *K8sDestroyStep) Name() string { return s.name }

func (s *K8sDestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	k, err := resolveK8sModule(s.app, s.cluster, s.name)
	if err != nil {
		return nil, err
	}
	if err := k.Destroy(); err != nil {
		return nil, fmt.Errorf("k8s_destroy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"destroyed": true,
		"cluster":   s.cluster,
	}}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func resolveK8sModule(app modular.Application, cluster, stepName string) (*PlatformKubernetes, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[cluster]
	if !ok {
		return nil, fmt.Errorf("step %q: cluster service %q not found in registry", stepName, cluster)
	}
	k, ok := svc.(*PlatformKubernetes)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformKubernetes (got %T)", stepName, cluster, svc)
	}
	return k, nil
}
