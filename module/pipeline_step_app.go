package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// ─── app_deploy ───────────────────────────────────────────────────────────────

// AppDeployStep deploys an app.container module.
type AppDeployStep struct {
	name     string
	app      string
	rawSpec  any
	specFrom string
	appMod   modular.Application
}

// NewAppDeployStepFactory returns a StepFactory for step.app_deploy.
func NewAppDeployStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		appName, _ := cfg["app"].(string)
		if appName == "" {
			return nil, fmt.Errorf("app_deploy step %q: 'app' is required", name)
		}
		specFrom, _ := cfg["spec_from"].(string)
		rawSpec, hasSpec := cfg["spec"]
		if hasSpec && specFrom != "" {
			return nil, fmt.Errorf("app_deploy step %q: 'spec' and 'spec_from' are mutually exclusive", name)
		}
		return &AppDeployStep{name: name, app: appName, rawSpec: rawSpec, specFrom: specFrom, appMod: app}, nil
	}
}

func (s *AppDeployStep) Name() string { return s.name }

func (s *AppDeployStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveAppContainerModule(s.appMod, s.app, s.name)
	if err != nil {
		return nil, err
	}
	spec := m.Spec()
	if s.rawSpec != nil {
		spec, err = appContainerSpecWithOverrides(spec, s.rawSpec)
		if err != nil {
			return nil, fmt.Errorf("app_deploy step %q: parse spec: %w", s.name, err)
		}
	}
	if s.specFrom != "" {
		if pc == nil {
			return nil, fmt.Errorf("app_deploy step %q: spec_from %q requires a non-nil pipeline context", s.name, s.specFrom)
		}
		raw := resolveBodyFrom(s.specFrom, pc)
		if raw == nil {
			return nil, fmt.Errorf("app_deploy step %q: spec_from %q resolved to nil", s.name, s.specFrom)
		}
		spec, err = appContainerSpecWithOverrides(spec, raw)
		if err != nil {
			return nil, fmt.Errorf("app_deploy step %q: parse spec_from %q: %w", s.name, s.specFrom, err)
		}
	}

	result, err := m.DeployWithSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("app_deploy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"deployed": true,
		"result":   result,
		"app":      s.app,
		"status":   result.Status,
		"endpoint": result.Endpoint,
		"image":    result.Image,
		"replicas": result.Replicas,
		"platform": result.Platform,
	}}, nil
}

func appContainerSpecWithOverrides(base AppContainerSpec, raw any) (AppContainerSpec, error) {
	overrides, ok := raw.(map[string]any)
	if !ok {
		return base, fmt.Errorf("spec must be a map, got %T", raw)
	}
	spec := base
	_, hasHealthPortOverride := firstOverride(overrides, "health_port", "healthPort")
	if image, ok := overrides["image"].(string); ok {
		if image == "" {
			return base, fmt.Errorf("image must be non-empty")
		}
		spec.Image = image
	}
	if rawReplicas, ok := overrides["replicas"]; ok {
		replicas, ok := intFromAny(rawReplicas)
		if !ok || replicas <= 0 {
			return base, fmt.Errorf("replicas must be a positive integer")
		}
		spec.Replicas = replicas
	}
	if rawPorts, ok := overrides["ports"]; ok {
		ports, err := parseAppContainerPorts(rawPorts)
		if err != nil {
			return base, err
		}
		spec.Ports = ports
		if !hasHealthPortOverride && len(ports) > 0 {
			spec.HealthPort = ports[0]
		}
	}
	if cpu, ok := overrides["cpu"].(string); ok {
		if cpu == "" {
			return base, fmt.Errorf("cpu must be non-empty")
		}
		spec.CPU = cpu
	}
	if memory, ok := overrides["memory"].(string); ok {
		if memory == "" {
			return base, fmt.Errorf("memory must be non-empty")
		}
		spec.Memory = memory
	}
	if healthPath, ok := stringOverride(overrides, "health_path", "healthPath"); ok {
		if healthPath == "" {
			return base, fmt.Errorf("health_path must be non-empty")
		}
		spec.HealthPath = healthPath
	}
	if rawHealthPort, ok := firstOverride(overrides, "health_port", "healthPort"); ok {
		healthPort, ok := intFromAny(rawHealthPort)
		if !ok || healthPort <= 0 {
			return base, fmt.Errorf("health_port must be a positive integer")
		}
		spec.HealthPort = healthPort
	}
	if rawEnv, ok := overrides["env"]; ok {
		env, err := parseAppContainerEnv(rawEnv)
		if err != nil {
			return base, err
		}
		spec.Env = env
	}
	if spec.Image == "" {
		return base, fmt.Errorf("image is required")
	}
	return spec, nil
}

func parseAppContainerPorts(raw any) ([]int, error) {
	var items []any
	switch v := raw.(type) {
	case []any:
		items = v
	case []int:
		items = make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("ports must be a list, got %T", raw)
	}
	ports := make([]int, 0, len(items))
	for i, item := range items {
		port, ok := intFromAny(item)
		if !ok || port <= 0 {
			return nil, fmt.Errorf("ports[%d] must be a positive integer", i)
		}
		ports = append(ports, port)
	}
	return ports, nil
}

func parseAppContainerEnv(raw any) (map[string]string, error) {
	var items map[string]any
	switch v := raw.(type) {
	case map[string]any:
		items = v
	case map[string]string:
		items = make(map[string]any, len(v))
		for key, value := range v {
			items[key] = value
		}
	default:
		return nil, fmt.Errorf("env must be a map, got %T", raw)
	}
	env := make(map[string]string, len(items))
	for key, value := range items {
		env[key] = fmt.Sprintf("%v", value)
	}
	return env, nil
}

func firstOverride(overrides map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := overrides[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func stringOverride(overrides map[string]any, keys ...string) (string, bool) {
	raw, ok := firstOverride(overrides, keys...)
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	return value, ok
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
