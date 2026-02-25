package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── do_deploy ────────────────────────────────────────────────────────────────

// DODeployStep deploys an app to DigitalOcean App Platform.
type DODeployStep struct {
	name string
	app  string
	svc  modular.Application
}

// NewDODeployStepFactory returns a StepFactory for step.do_deploy.
func NewDODeployStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("do_deploy step %q: 'app' is required", name)
		}
		return &DODeployStep{name: name, app: appName, svc: app}, nil
	}
}

func (s *DODeployStep) Name() string { return s.name }

func (s *DODeployStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDOAppModule(s.svc, s.app, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Deploy()
	if err != nil {
		return nil, fmt.Errorf("do_deploy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"app":           s.app,
		"id":            state.ID,
		"status":        state.Status,
		"live_url":      state.LiveURL,
		"deployment_id": state.DeploymentID,
	}}, nil
}

// ─── do_status ────────────────────────────────────────────────────────────────

// DOStatusStep checks the status of a DO App Platform app.
type DOStatusStep struct {
	name string
	app  string
	svc  modular.Application
}

// NewDOStatusStepFactory returns a StepFactory for step.do_status.
func NewDOStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("do_status step %q: 'app' is required", name)
		}
		return &DOStatusStep{name: name, app: appName, svc: app}, nil
	}
}

func (s *DOStatusStep) Name() string { return s.name }

func (s *DOStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDOAppModule(s.svc, s.app, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Status()
	if err != nil {
		return nil, fmt.Errorf("do_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"app":      s.app,
		"status":   state.Status,
		"live_url": state.LiveURL,
		"state":    state,
	}}, nil
}

// ─── do_logs ──────────────────────────────────────────────────────────────────

// DOLogsStep retrieves logs from a DO App Platform app.
type DOLogsStep struct {
	name string
	app  string
	svc  modular.Application
}

// NewDOLogsStepFactory returns a StepFactory for step.do_logs.
func NewDOLogsStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("do_logs step %q: 'app' is required", name)
		}
		return &DOLogsStep{name: name, app: appName, svc: app}, nil
	}
}

func (s *DOLogsStep) Name() string { return s.name }

func (s *DOLogsStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDOAppModule(s.svc, s.app, s.name)
	if err != nil {
		return nil, err
	}
	logs, err := m.Logs()
	if err != nil {
		return nil, fmt.Errorf("do_logs step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"app":  s.app,
		"logs": logs,
	}}, nil
}

// ─── do_scale ─────────────────────────────────────────────────────────────────

// DOScaleStep scales a DO App Platform app.
type DOScaleStep struct {
	name      string
	app       string
	instances int
	svc       modular.Application
}

// NewDOScaleStepFactory returns a StepFactory for step.do_scale.
func NewDOScaleStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("do_scale step %q: 'app' is required", name)
		}
		instances, _ := intFromAny(cfg["instances"])
		if instances <= 0 {
			instances = 1
		}
		return &DOScaleStep{name: name, app: appName, instances: instances, svc: app}, nil
	}
}

func (s *DOScaleStep) Name() string { return s.name }

func (s *DOScaleStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDOAppModule(s.svc, s.app, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Scale(s.instances)
	if err != nil {
		return nil, fmt.Errorf("do_scale step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"app":       s.app,
		"instances": s.instances,
		"status":    state.Status,
	}}, nil
}

// ─── do_destroy ───────────────────────────────────────────────────────────────

// DODestroyStep tears down a DO App Platform app.
type DODestroyStep struct {
	name string
	app  string
	svc  modular.Application
}

// NewDODestroyStepFactory returns a StepFactory for step.do_destroy.
func NewDODestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("do_destroy step %q: 'app' is required", name)
		}
		return &DODestroyStep{name: name, app: appName, svc: app}, nil
	}
}

func (s *DODestroyStep) Name() string { return s.name }

func (s *DODestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDOAppModule(s.svc, s.app, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.Destroy(); err != nil {
		return nil, fmt.Errorf("do_destroy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"app":       s.app,
		"destroyed": true,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveDOAppModule(app modular.Application, appName, stepName string) (*PlatformDOApp, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[appName]
	if !ok {
		return nil, fmt.Errorf("step %q: app %q not found in registry", stepName, appName)
	}
	m, ok := svc.(*PlatformDOApp)
	if !ok {
		return nil, fmt.Errorf("step %q: app %q is not a *PlatformDOApp (got %T)", stepName, appName, svc)
	}
	return m, nil
}
