package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── app_deploy ───────────────────────────────────────────────────────────────

// AppDeployStep deploys an app.container module.
type AppDeployStep struct {
	name   string
	app    string
	appMod modular.Application
}

// NewAppDeployStepFactory returns a StepFactory for step.app_deploy.
func NewAppDeployStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("app_deploy step %q: 'app' is required", name)
		}
		return &AppDeployStep{name: name, app: appName, appMod: app}, nil
	}
}

func (s *AppDeployStep) Name() string { return s.name }

func (s *AppDeployStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveAppContainerModule(s.appMod, s.app, s.name)
	if err != nil {
		return nil, err
	}
	result, err := m.Deploy()
	if err != nil {
		return nil, fmt.Errorf("app_deploy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"result":   result,
		"app":      s.app,
		"status":   result.Status,
		"endpoint": result.Endpoint,
		"image":    result.Image,
		"replicas": result.Replicas,
		"platform": result.Platform,
	}}, nil
}

// ─── app_status ───────────────────────────────────────────────────────────────

// AppStatusStep returns the current deployment status of an app.container module.
type AppStatusStep struct {
	name   string
	app    string
	appMod modular.Application
}

// NewAppStatusStepFactory returns a StepFactory for step.app_status.
func NewAppStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("app_status step %q: 'app' is required", name)
		}
		return &AppStatusStep{name: name, app: appName, appMod: app}, nil
	}
}

func (s *AppStatusStep) Name() string { return s.name }

func (s *AppStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveAppContainerModule(s.appMod, s.app, s.name)
	if err != nil {
		return nil, err
	}
	result, err := m.Status()
	if err != nil {
		return nil, fmt.Errorf("app_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"result":   result,
		"app":      s.app,
		"status":   result.Status,
		"platform": result.Platform,
		"replicas": result.Replicas,
		"image":    result.Image,
	}}, nil
}

// ─── app_rollback ─────────────────────────────────────────────────────────────

// AppRollbackStep rolls back an app.container module to the previous deployment state.
type AppRollbackStep struct {
	name   string
	app    string
	appMod modular.Application
}

// NewAppRollbackStepFactory returns a StepFactory for step.app_rollback.
func NewAppRollbackStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("app_rollback step %q: 'app' is required", name)
		}
		return &AppRollbackStep{name: name, app: appName, appMod: app}, nil
	}
}

func (s *AppRollbackStep) Name() string { return s.name }

func (s *AppRollbackStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveAppContainerModule(s.appMod, s.app, s.name)
	if err != nil {
		return nil, err
	}

	result, err := m.Rollback()
	if err != nil {
		return nil, fmt.Errorf("app_rollback step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"result":      result,
		"app":         s.app,
		"status":      result.Status,
		"rolled_back": true,
		"image":       result.Image,
		"platform":    result.Platform,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveAppContainerModule(app modular.Application, appName, stepName string) (*AppContainerModule, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[appName]
	if !ok {
		return nil, fmt.Errorf("step %q: app service %q not found in registry", stepName, appName)
	}
	m, ok := svc.(*AppContainerModule)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *AppContainerModule (got %T)", stepName, appName, svc)
	}
	return m, nil
}
