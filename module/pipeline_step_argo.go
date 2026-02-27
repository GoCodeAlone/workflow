package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── argo_submit ─────────────────────────────────────────────────────────────

// ArgoSubmitStep submits an Argo Workflow built from pipeline step configs.
type ArgoSubmitStep struct {
	name    string
	service string
	wfName  string
	steps   []map[string]any
	app     modular.Application
}

// NewArgoSubmitStepFactory returns a StepFactory for step.argo_submit.
func NewArgoSubmitStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("argo_submit step %q: 'service' is required", name)
		}
		wfName, _ := cfg["workflow_name"].(string)
		if wfName == "" {
			wfName = name
		}
		var steps []map[string]any
		if raw, ok := cfg["steps"].([]any); ok {
			for _, item := range raw {
				if s, ok := item.(map[string]any); ok {
					steps = append(steps, s)
				}
			}
		}
		return &ArgoSubmitStep{name: name, service: service, wfName: wfName, steps: steps, app: app}, nil
	}
}

func (s *ArgoSubmitStep) Name() string { return s.name }

func (s *ArgoSubmitStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveArgoModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	spec := TranslatePipelineToArgo(s.wfName, m.namespace(), s.steps)
	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		return nil, fmt.Errorf("argo_submit step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"workflow_run":  runName,
		"service":       s.service,
		"workflow_name": s.wfName,
		"namespace":     spec.Namespace,
		"templates":     len(spec.Templates),
	}}, nil
}

// ─── argo_status ─────────────────────────────────────────────────────────────

// ArgoStatusStep checks the execution status of an Argo Workflow run.
type ArgoStatusStep struct {
	name        string
	service     string
	workflowRun string
	app         modular.Application
}

// NewArgoStatusStepFactory returns a StepFactory for step.argo_status.
func NewArgoStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("argo_status step %q: 'service' is required", name)
		}
		workflowRun, _ := cfg["workflow_run"].(string)
		return &ArgoStatusStep{name: name, service: service, workflowRun: workflowRun, app: app}, nil
	}
}

func (s *ArgoStatusStep) Name() string { return s.name }

func (s *ArgoStatusStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveArgoModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	wfRun := s.workflowRun
	if wfRun == "" {
		// Try to pick it up from the pipeline context (previous argo_submit output).
		if v, ok := pc.Current["workflow_run"].(string); ok {
			wfRun = v
		}
	}
	if wfRun == "" {
		return nil, fmt.Errorf("argo_status step %q: 'workflow_run' is required (set in config or from prior argo_submit)", s.name)
	}
	status, err := m.WorkflowStatus(wfRun)
	if err != nil {
		return nil, fmt.Errorf("argo_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"workflow_run":    wfRun,
		"workflow_status": status,
		"service":         s.service,
		"succeeded":       status == "Succeeded",
		"failed":          status == "Failed" || status == "Error",
	}}, nil
}

// ─── argo_logs ───────────────────────────────────────────────────────────────

// ArgoLogsStep retrieves log lines from an Argo Workflow run.
type ArgoLogsStep struct {
	name        string
	service     string
	workflowRun string
	app         modular.Application
}

// NewArgoLogsStepFactory returns a StepFactory for step.argo_logs.
func NewArgoLogsStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("argo_logs step %q: 'service' is required", name)
		}
		workflowRun, _ := cfg["workflow_run"].(string)
		return &ArgoLogsStep{name: name, service: service, workflowRun: workflowRun, app: app}, nil
	}
}

func (s *ArgoLogsStep) Name() string { return s.name }

func (s *ArgoLogsStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveArgoModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	wfRun := s.workflowRun
	if wfRun == "" {
		if v, ok := pc.Current["workflow_run"].(string); ok {
			wfRun = v
		}
	}
	if wfRun == "" {
		return nil, fmt.Errorf("argo_logs step %q: 'workflow_run' is required", s.name)
	}
	lines, err := m.WorkflowLogs(wfRun)
	if err != nil {
		return nil, fmt.Errorf("argo_logs step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"workflow_run": wfRun,
		"service":      s.service,
		"logs":         lines,
		"line_count":   len(lines),
	}}, nil
}

// ─── argo_delete ─────────────────────────────────────────────────────────────

// ArgoDeleteStep removes a completed or failed Argo Workflow run.
type ArgoDeleteStep struct {
	name        string
	service     string
	workflowRun string
	app         modular.Application
}

// NewArgoDeleteStepFactory returns a StepFactory for step.argo_delete.
func NewArgoDeleteStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("argo_delete step %q: 'service' is required", name)
		}
		workflowRun, _ := cfg["workflow_run"].(string)
		return &ArgoDeleteStep{name: name, service: service, workflowRun: workflowRun, app: app}, nil
	}
}

func (s *ArgoDeleteStep) Name() string { return s.name }

func (s *ArgoDeleteStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveArgoModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	wfRun := s.workflowRun
	if wfRun == "" {
		if v, ok := pc.Current["workflow_run"].(string); ok {
			wfRun = v
		}
	}
	if wfRun == "" {
		return nil, fmt.Errorf("argo_delete step %q: 'workflow_run' is required", s.name)
	}
	if err := m.DeleteWorkflow(wfRun); err != nil {
		return nil, fmt.Errorf("argo_delete step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"workflow_run": wfRun,
		"service":      s.service,
		"deleted":      true,
	}}, nil
}

// ─── argo_list ───────────────────────────────────────────────────────────────

// ArgoListStep lists Argo Workflow runs with an optional label selector.
type ArgoListStep struct {
	name          string
	service       string
	labelSelector string
	app           modular.Application
}

// NewArgoListStepFactory returns a StepFactory for step.argo_list.
func NewArgoListStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("argo_list step %q: 'service' is required", name)
		}
		labelSelector, _ := cfg["label_selector"].(string)
		return &ArgoListStep{name: name, service: service, labelSelector: labelSelector, app: app}, nil
	}
}

func (s *ArgoListStep) Name() string { return s.name }

func (s *ArgoListStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveArgoModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	workflows, err := m.ListWorkflows(s.labelSelector)
	if err != nil {
		return nil, fmt.Errorf("argo_list step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"workflows":      workflows,
		"service":        s.service,
		"label_selector": s.labelSelector,
		"count":          len(workflows),
	}}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func resolveArgoModule(app modular.Application, service, stepName string) (*ArgoWorkflowsModule, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[service]
	if !ok {
		return nil, fmt.Errorf("step %q: service %q not found in registry", stepName, service)
	}
	m, ok := svc.(*ArgoWorkflowsModule)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *ArgoWorkflowsModule (got %T)", stepName, service, svc)
	}
	return m, nil
}
