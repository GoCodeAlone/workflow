package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── codebuild_create_project ─────────────────────────────────────────────────

// CodeBuildCreateProjectStep creates or updates a CodeBuild project.
type CodeBuildCreateProjectStep struct {
	name    string
	project string
	app     modular.Application
}

// NewCodeBuildCreateProjectStepFactory returns a StepFactory for step.codebuild_create_project.
func NewCodeBuildCreateProjectStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_create_project step %q: 'project' is required", name)
		}
		return &CodeBuildCreateProjectStep{name: name, project: project, app: app}, nil
	}
}

func (s *CodeBuildCreateProjectStep) Name() string { return s.name }

func (s *CodeBuildCreateProjectStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.CreateProject(); err != nil {
		return nil, fmt.Errorf("codebuild_create_project step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"project": s.project,
		"status":  m.state.Status,
		"arn":     m.state.ARN,
	}}, nil
}

// ─── codebuild_start ──────────────────────────────────────────────────────────

// CodeBuildStartStep starts a new build on a CodeBuild project.
type CodeBuildStartStep struct {
	name    string
	project string
	envVars map[string]string
	app     modular.Application
}

// NewCodeBuildStartStepFactory returns a StepFactory for step.codebuild_start.
func NewCodeBuildStartStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_start step %q: 'project' is required", name)
		}

		envVars := make(map[string]string)
		if raw, ok := cfg["env_vars"].(map[string]any); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					envVars[k] = s
				}
			}
		}

		return &CodeBuildStartStep{name: name, project: project, envVars: envVars, app: app}, nil
	}
}

func (s *CodeBuildStartStep) Name() string { return s.name }

func (s *CodeBuildStartStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}
	build, err := m.StartBuild(s.envVars)
	if err != nil {
		return nil, fmt.Errorf("codebuild_start step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"build_id":     build.ID,
		"project":      s.project,
		"status":       build.Status,
		"build_number": build.BuildNumber,
	}}, nil
}

// ─── codebuild_status ─────────────────────────────────────────────────────────

// CodeBuildStatusStep checks the status of a CodeBuild build.
type CodeBuildStatusStep struct {
	name    string
	project string
	buildID string
	app     modular.Application
}

// NewCodeBuildStatusStepFactory returns a StepFactory for step.codebuild_status.
func NewCodeBuildStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_status step %q: 'project' is required", name)
		}
		buildID, _ := cfg["build_id"].(string)
		return &CodeBuildStatusStep{name: name, project: project, buildID: buildID, app: app}, nil
	}
}

func (s *CodeBuildStatusStep) Name() string { return s.name }

func (s *CodeBuildStatusStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}

	// Allow build_id to come from pipeline context if not configured statically.
	buildID := s.buildID
	if buildID == "" {
		if id, ok := pc.Current["build_id"].(string); ok {
			buildID = id
		}
	}
	if buildID == "" {
		return nil, fmt.Errorf("codebuild_status step %q: 'build_id' is required (config or pipeline context)", s.name)
	}

	build, err := m.GetBuildStatus(buildID)
	if err != nil {
		return nil, fmt.Errorf("codebuild_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"build_id": build.ID,
		"project":  s.project,
		"status":   build.Status,
		"phase":    build.Phase,
	}}, nil
}

// ─── codebuild_logs ───────────────────────────────────────────────────────────

// CodeBuildLogsStep retrieves logs for a CodeBuild build.
type CodeBuildLogsStep struct {
	name    string
	project string
	buildID string
	app     modular.Application
}

// NewCodeBuildLogsStepFactory returns a StepFactory for step.codebuild_logs.
func NewCodeBuildLogsStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_logs step %q: 'project' is required", name)
		}
		buildID, _ := cfg["build_id"].(string)
		return &CodeBuildLogsStep{name: name, project: project, buildID: buildID, app: app}, nil
	}
}

func (s *CodeBuildLogsStep) Name() string { return s.name }

func (s *CodeBuildLogsStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}

	buildID := s.buildID
	if buildID == "" {
		if id, ok := pc.Current["build_id"].(string); ok {
			buildID = id
		}
	}
	if buildID == "" {
		return nil, fmt.Errorf("codebuild_logs step %q: 'build_id' is required (config or pipeline context)", s.name)
	}

	logs, err := m.GetBuildLogs(buildID)
	if err != nil {
		return nil, fmt.Errorf("codebuild_logs step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"build_id":   buildID,
		"project":    s.project,
		"logs":       logs,
		"line_count": len(logs),
	}}, nil
}

// ─── codebuild_delete_project ─────────────────────────────────────────────────

// CodeBuildDeleteProjectStep removes a CodeBuild project.
type CodeBuildDeleteProjectStep struct {
	name    string
	project string
	app     modular.Application
}

// NewCodeBuildDeleteProjectStepFactory returns a StepFactory for step.codebuild_delete_project.
func NewCodeBuildDeleteProjectStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_delete_project step %q: 'project' is required", name)
		}
		return &CodeBuildDeleteProjectStep{name: name, project: project, app: app}, nil
	}
}

func (s *CodeBuildDeleteProjectStep) Name() string { return s.name }

func (s *CodeBuildDeleteProjectStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.DeleteProject(); err != nil {
		return nil, fmt.Errorf("codebuild_delete_project step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"project": s.project,
		"deleted": true,
	}}, nil
}

// ─── codebuild_list_builds ────────────────────────────────────────────────────

// CodeBuildListBuildsStep lists builds for a CodeBuild project.
type CodeBuildListBuildsStep struct {
	name    string
	project string
	app     modular.Application
}

// NewCodeBuildListBuildsStepFactory returns a StepFactory for step.codebuild_list_builds.
func NewCodeBuildListBuildsStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		project, _ := cfg["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("codebuild_list_builds step %q: 'project' is required", name)
		}
		return &CodeBuildListBuildsStep{name: name, project: project, app: app}, nil
	}
}

func (s *CodeBuildListBuildsStep) Name() string { return s.name }

func (s *CodeBuildListBuildsStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveCodeBuildModule(s.app, s.project, s.name)
	if err != nil {
		return nil, err
	}
	builds, err := m.ListBuilds()
	if err != nil {
		return nil, fmt.Errorf("codebuild_list_builds step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"project":     s.project,
		"builds":      builds,
		"build_count": len(builds),
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveCodeBuildModule(app modular.Application, project, stepName string) (*CodeBuildModule, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[project]
	if !ok {
		return nil, fmt.Errorf("step %q: project %q not found in registry", stepName, project)
	}
	m, ok := svc.(*CodeBuildModule)
	if !ok {
		return nil, fmt.Errorf("step %q: project %q is not a *CodeBuildModule (got %T)", stepName, project, svc)
	}
	return m, nil
}
