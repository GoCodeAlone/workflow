package module_test

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── module lifecycle tests ───────────────────────────────────────────────────

func TestCodeBuildModule_InitAndRegister(t *testing.T) {
	m := module.NewCodeBuildModule("my-project", map[string]any{
		"region":       "us-east-1",
		"compute_type": "BUILD_GENERAL1_SMALL",
	})

	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	svc, ok := app.Services["my-project"]
	if !ok {
		t.Fatal("expected my-project in service registry")
	}
	if _, ok := svc.(*module.CodeBuildModule); !ok {
		t.Fatalf("registry entry is %T, want *CodeBuildModule", svc)
	}
}

func TestCodeBuildModule_DefaultsApplied(t *testing.T) {
	m := module.NewCodeBuildModule("defaults-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

func TestCodeBuildModule_MissingAccount(t *testing.T) {
	m := module.NewCodeBuildModule("fail-proj", map[string]any{
		"account": "nonexistent-account",
	})
	app := module.NewMockApplication()
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

func TestCodeBuildModule_AccountResolution(t *testing.T) {
	acc := module.NewCloudAccount("aws-account", map[string]any{
		"provider": "mock",
		"region":   "us-east-1",
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init: %v", err)
	}

	m := module.NewCodeBuildModule("build-proj", map[string]any{
		"account": "aws-account",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("CodeBuild Init: %v", err)
	}
}

// ─── project create/delete lifecycle ─────────────────────────────────────────

func TestCodeBuildModule_CreateAndDeleteProject(t *testing.T) {
	m := module.NewCodeBuildModule("build-proj", map[string]any{
		"region": "us-west-2",
	})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	builds, err := m.ListBuilds()
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(builds) != 0 {
		t.Errorf("expected 0 builds after create, got %d", len(builds))
	}

	if err := m.DeleteProject(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
}

func TestCodeBuildModule_DeleteIdempotent(t *testing.T) {
	m := module.NewCodeBuildModule("idempotent-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := m.DeleteProject(); err != nil {
		t.Fatalf("first DeleteProject: %v", err)
	}
	if err := m.DeleteProject(); err != nil {
		t.Errorf("second DeleteProject should be idempotent, got: %v", err)
	}
}

// ─── build start/status/logs lifecycle ───────────────────────────────────────

func TestCodeBuildModule_StartBuildAndStatus(t *testing.T) {
	m := module.NewCodeBuildModule("ci-project", map[string]any{
		"region": "us-east-1",
	})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	if build.ID == "" {
		t.Error("expected non-empty build ID")
	}
	if build.ProjectName != "ci-project" {
		t.Errorf("expected project=ci-project, got %q", build.ProjectName)
	}
	if build.Status != "SUCCEEDED" {
		t.Errorf("expected status=SUCCEEDED (mock), got %q", build.Status)
	}

	status, err := m.GetBuildStatus(build.ID)
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
	if status.Status != "SUCCEEDED" {
		t.Errorf("expected SUCCEEDED, got %q", status.Status)
	}
}

func TestCodeBuildModule_BuildLogs(t *testing.T) {
	m := module.NewCodeBuildModule("logs-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	logs, err := m.GetBuildLogs(build.ID)
	if err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("expected at least one log line")
	}
}

func TestCodeBuildModule_StartBuildWithEnvOverrides(t *testing.T) {
	m := module.NewCodeBuildModule("env-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	overrides := map[string]string{"DEPLOY_ENV": "staging", "VERSION": "1.2.3"}
	build, err := m.StartBuild(overrides)
	if err != nil {
		t.Fatalf("StartBuild with overrides: %v", err)
	}
	if build.EnvVars["DEPLOY_ENV"] != "staging" {
		t.Errorf("expected DEPLOY_ENV=staging, got %q", build.EnvVars["DEPLOY_ENV"])
	}
	if build.EnvVars["VERSION"] != "1.2.3" {
		t.Errorf("expected VERSION=1.2.3, got %q", build.EnvVars["VERSION"])
	}
}

func TestCodeBuildModule_StartBuildNotReady(t *testing.T) {
	m := module.NewCodeBuildModule("not-ready", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Don't call CreateProject — project is in "pending" state.
	_, err := m.StartBuild(nil)
	if err == nil {
		t.Error("expected error starting build on non-ready project")
	}
}

func TestCodeBuildModule_ListBuilds(t *testing.T) {
	m := module.NewCodeBuildModule("list-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := m.StartBuild(nil); err != nil {
			t.Fatalf("StartBuild %d: %v", i, err)
		}
	}

	builds, err := m.ListBuilds()
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(builds) != 3 {
		t.Errorf("expected 3 builds, got %d", len(builds))
	}
}

func TestCodeBuildModule_GetBuildStatusNotFound(t *testing.T) {
	m := module.NewCodeBuildModule("notfound-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := m.GetBuildStatus("nonexistent-build-id")
	if err == nil {
		t.Error("expected error for missing build ID")
	}
}

func TestCodeBuildModule_BuildIncrementingIDs(t *testing.T) {
	m := module.NewCodeBuildModule("seq-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	b1, _ := m.StartBuild(nil)
	b2, _ := m.StartBuild(nil)

	if b1.ID == b2.ID {
		t.Error("expected unique build IDs for sequential builds")
	}
	if b2.BuildNumber <= b1.BuildNumber {
		t.Errorf("expected build_number to increment: %d <= %d", b2.BuildNumber, b1.BuildNumber)
	}
}

// ─── buildspec generation ─────────────────────────────────────────────────────

func TestCodeBuildModule_GenerateBuildspec_Minimal(t *testing.T) {
	m := module.NewCodeBuildModule("buildspec-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{})
	if !strings.Contains(spec, "version: 0.2") {
		t.Error("expected version 0.2 in buildspec")
	}
	if !strings.Contains(spec, "phases:") {
		t.Error("expected phases section in buildspec")
	}
	if !strings.Contains(spec, "build:") {
		t.Error("expected build phase in buildspec")
	}
}

func TestCodeBuildModule_GenerateBuildspec_WithCommands(t *testing.T) {
	m := module.NewCodeBuildModule("cmd-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{
		"install_commands":    []string{"npm install"},
		"build_commands":      []string{"npm run build", "npm test"},
		"post_build_commands": []string{"echo Done"},
	})

	if !strings.Contains(spec, "npm install") {
		t.Error("expected npm install in install phase")
	}
	if !strings.Contains(spec, "npm run build") {
		t.Error("expected npm run build in build phase")
	}
	if !strings.Contains(spec, "npm test") {
		t.Error("expected npm test in build phase")
	}
	if !strings.Contains(spec, "echo Done") {
		t.Error("expected post_build echo in spec")
	}
	if !strings.Contains(spec, "install:") {
		t.Error("expected install phase header")
	}
	if !strings.Contains(spec, "post_build:") {
		t.Error("expected post_build phase header")
	}
}

func TestCodeBuildModule_GenerateBuildspec_WithPreBuild(t *testing.T) {
	m := module.NewCodeBuildModule("prebuild-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{
		"pre_build_commands": []string{"aws ecr get-login-password"},
	})

	if !strings.Contains(spec, "pre_build:") {
		t.Error("expected pre_build phase header")
	}
	if !strings.Contains(spec, "aws ecr get-login-password") {
		t.Error("expected pre_build command in spec")
	}
}

func TestCodeBuildModule_GenerateBuildspec_WithArtifacts(t *testing.T) {
	m := module.NewCodeBuildModule("artifact-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{
		"artifact_files": []string{"**/*"},
		"artifact_dir":   "dist",
	})

	if !strings.Contains(spec, "artifacts:") {
		t.Error("expected artifacts section")
	}
	if !strings.Contains(spec, "**/*") {
		t.Error("expected artifact file pattern")
	}
	if !strings.Contains(spec, "dist") {
		t.Error("expected artifact base-directory")
	}
}

func TestCodeBuildModule_GenerateBuildspec_WithCache(t *testing.T) {
	m := module.NewCodeBuildModule("cache-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{
		"cache_paths": []string{"/root/.npm", "node_modules"},
	})

	if !strings.Contains(spec, "cache:") {
		t.Error("expected cache section")
	}
	if !strings.Contains(spec, "/root/.npm") {
		t.Error("expected /root/.npm cache path")
	}
	if !strings.Contains(spec, "node_modules") {
		t.Error("expected node_modules cache path")
	}
}

func TestCodeBuildModule_GenerateBuildspec_WithEnvVars(t *testing.T) {
	m := module.NewCodeBuildModule("env-buildspec-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.GenerateBuildspec(map[string]any{
		"env_variables": map[string]any{"NODE_ENV": "production"},
	})

	if !strings.Contains(spec, "env:") {
		t.Error("expected env section")
	}
	if !strings.Contains(spec, "NODE_ENV") {
		t.Error("expected NODE_ENV in env vars")
	}
}

func TestCodeBuildModule_GenerateBuildspec_AnySliceCommands(t *testing.T) {
	m := module.NewCodeBuildModule("anyslice-proj", map[string]any{})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Simulate YAML-decoded commands as []any (common from YAML parsers).
	spec := m.GenerateBuildspec(map[string]any{
		"build_commands": []any{"go build ./...", "go test ./..."},
	})

	if !strings.Contains(spec, "go build ./...") {
		t.Error("expected go build command from []any slice")
	}
	if !strings.Contains(spec, "go test ./...") {
		t.Error("expected go test command from []any slice")
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func setupCodeBuildApp(t *testing.T) (*module.MockApplication, *module.CodeBuildModule) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewCodeBuildModule("test-project", map[string]any{
		"region": "us-east-1",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.CreateProject(); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return app, m
}

func TestCodeBuildCreateProjectStep(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewCodeBuildModule("step-proj", map[string]any{})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	factory := module.NewCodeBuildCreateProjectStepFactory()
	step, err := factory("create", map[string]any{"project": "step-proj"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["project"] != "step-proj" {
		t.Errorf("expected project=step-proj, got %v", result.Output["project"])
	}
	if result.Output["arn"] == "" {
		t.Error("expected non-empty ARN in output")
	}
	if result.Output["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", result.Output["status"])
	}
}

func TestCodeBuildStartStep(t *testing.T) {
	app, _ := setupCodeBuildApp(t)

	factory := module.NewCodeBuildStartStepFactory()
	step, err := factory("start", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["build_id"] == "" {
		t.Error("expected non-empty build_id in output")
	}
	if result.Output["status"] != "SUCCEEDED" {
		t.Errorf("expected status=SUCCEEDED, got %v", result.Output["status"])
	}
}

func TestCodeBuildStartStep_WithEnvVars(t *testing.T) {
	app, _ := setupCodeBuildApp(t)

	factory := module.NewCodeBuildStartStepFactory()
	step, err := factory("start", map[string]any{
		"project":  "test-project",
		"env_vars": map[string]any{"MY_VAR": "my-value"},
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["build_id"] == "" {
		t.Error("expected non-empty build_id")
	}
}

func TestCodeBuildStatusStep(t *testing.T) {
	app, m := setupCodeBuildApp(t)

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	factory := module.NewCodeBuildStatusStepFactory()
	step, err := factory("status", map[string]any{
		"project":  "test-project",
		"build_id": build.ID,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "SUCCEEDED" {
		t.Errorf("expected SUCCEEDED, got %v", result.Output["status"])
	}
	if result.Output["build_id"] != build.ID {
		t.Errorf("expected build_id=%q, got %v", build.ID, result.Output["build_id"])
	}
}

func TestCodeBuildStatusStep_BuildIDFromContext(t *testing.T) {
	app, m := setupCodeBuildApp(t)

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	factory := module.NewCodeBuildStatusStepFactory()
	// No build_id in step config — should read from pipeline context.
	step, err := factory("status", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{
		Current: map[string]any{"build_id": build.ID},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["build_id"] != build.ID {
		t.Errorf("expected build_id=%q, got %v", build.ID, result.Output["build_id"])
	}
}

func TestCodeBuildLogsStep(t *testing.T) {
	app, m := setupCodeBuildApp(t)

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	factory := module.NewCodeBuildLogsStepFactory()
	step, err := factory("logs", map[string]any{
		"project":  "test-project",
		"build_id": build.ID,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	logs, ok := result.Output["logs"].([]string)
	if !ok {
		t.Fatalf("expected []string logs, got %T", result.Output["logs"])
	}
	if len(logs) == 0 {
		t.Error("expected at least one log line")
	}
	if result.Output["line_count"].(int) != len(logs) {
		t.Error("expected line_count to match logs length")
	}
}

func TestCodeBuildLogsStep_BuildIDFromContext(t *testing.T) {
	app, m := setupCodeBuildApp(t)

	build, err := m.StartBuild(nil)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	factory := module.NewCodeBuildLogsStepFactory()
	step, err := factory("logs", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{
		Current: map[string]any{"build_id": build.ID},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["build_id"] != build.ID {
		t.Errorf("expected build_id=%q, got %v", build.ID, result.Output["build_id"])
	}
}

func TestCodeBuildDeleteProjectStep(t *testing.T) {
	app, _ := setupCodeBuildApp(t)

	factory := module.NewCodeBuildDeleteProjectStepFactory()
	step, err := factory("delete", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", result.Output["deleted"])
	}
}

func TestCodeBuildListBuildsStep(t *testing.T) {
	app, m := setupCodeBuildApp(t)

	for i := 0; i < 2; i++ {
		if _, err := m.StartBuild(nil); err != nil {
			t.Fatalf("StartBuild %d: %v", i, err)
		}
	}

	factory := module.NewCodeBuildListBuildsStepFactory()
	step, err := factory("list", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["build_count"].(int) != 2 {
		t.Errorf("expected build_count=2, got %v", result.Output["build_count"])
	}
}

// ─── error handling tests ─────────────────────────────────────────────────────

func TestCodeBuildCreateProjectStep_MissingProject(t *testing.T) {
	factory := module.NewCodeBuildCreateProjectStepFactory()
	_, err := factory("create", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestCodeBuildStartStep_MissingProject(t *testing.T) {
	factory := module.NewCodeBuildStartStepFactory()
	_, err := factory("start", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestCodeBuildStatusStep_MissingProject(t *testing.T) {
	factory := module.NewCodeBuildStatusStepFactory()
	_, err := factory("status", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestCodeBuildStartStep_ProjectNotFound(t *testing.T) {
	factory := module.NewCodeBuildStartStepFactory()
	step, err := factory("start", map[string]any{"project": "ghost-project"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing project in registry")
	}
}

func TestCodeBuildStatusStep_MissingBuildID(t *testing.T) {
	app, _ := setupCodeBuildApp(t)

	factory := module.NewCodeBuildStatusStepFactory()
	step, err := factory("status", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing build_id")
	}
	if !strings.Contains(err.Error(), "build_id") {
		t.Errorf("expected error to mention build_id, got: %v", err)
	}
}

func TestCodeBuildLogsStep_MissingBuildID(t *testing.T) {
	app, _ := setupCodeBuildApp(t)

	factory := module.NewCodeBuildLogsStepFactory()
	step, err := factory("logs", map[string]any{"project": "test-project"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing build_id")
	}
}

func TestCodeBuildDeleteProjectStep_MissingProject(t *testing.T) {
	factory := module.NewCodeBuildDeleteProjectStepFactory()
	_, err := factory("delete", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestCodeBuildListBuildsStep_MissingProject(t *testing.T) {
	factory := module.NewCodeBuildListBuildsStepFactory()
	_, err := factory("list", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestCodeBuildStepName(t *testing.T) {
	factory := module.NewCodeBuildCreateProjectStepFactory()
	step, err := factory("my-create-step", map[string]any{"project": "some-proj"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if step.Name() != "my-create-step" {
		t.Errorf("expected name=my-create-step, got %q", step.Name())
	}
}
