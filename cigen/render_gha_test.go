package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
	"gopkg.in/yaml.v3"
)

func TestRenderGitHubActions_ValidYAML(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one file in output")
	}

	for path, content := range files {
		if !strings.HasSuffix(path, ".yml") {
			t.Errorf("expected .yml extension, got %q", path)
		}
		var parsed any
		if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
			t.Errorf("file %q is not valid YAML: %v\ncontent:\n%s", path, err, content)
		}
	}
}

func TestRenderGitHubActions_TwoPhases(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}

	var content string
	for _, c := range files {
		content = c
		break
	}

	// Plan job
	if !strings.Contains(content, "if: github.event_name == 'pull_request'") {
		t.Error("expected plan job with pull_request condition")
	}

	// Two-phase apply jobs
	if !strings.Contains(content, "apply-prereq:") {
		t.Error("expected apply-prereq job for two-phase plan")
	}
	if !strings.Contains(content, "apply-deploy:") {
		t.Error("expected apply-deploy job for two-phase plan")
	}
	// apply-deploy needs apply-prereq
	if !strings.Contains(content, "needs: apply-prereq") {
		t.Error("expected apply-deploy to declare needs: apply-prereq")
	}
}

func TestRenderGitHubActions_MigrationsStep(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	if !strings.Contains(content, "migrate") {
		t.Error("expected migrations step in output")
	}
}

func TestRenderGitHubActions_SecretsEnv(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	for _, s := range plan.Secrets {
		marker := "secrets." + s.Name
		if !strings.Contains(content, marker) {
			t.Errorf("expected ${{ secrets.%s }} in output", s.Name)
		}
	}
}

func TestRenderGitHubActions_SmokeJob(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	if !strings.Contains(content, "smoke:") {
		t.Error("expected smoke job in output")
	}
	if !strings.Contains(content, plan.Smoke.URL) {
		t.Errorf("expected smoke URL %q in output", plan.Smoke.URL)
	}
	if !strings.Contains(content, "curl") {
		t.Error("expected curl command in smoke job")
	}
}

func TestRenderGitHubActions_NilPlan(t *testing.T) {
	_, err := cigen.RenderGitHubActions(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

// richCIPlan returns a CIPlan with 2 phases, migrations, smoke, and 3 secrets.
func richCIPlan() *cigen.CIPlan {
	return &cigen.CIPlan{
		Project:       "myapp",
		WfctlVersion:  "v0.66.0",
		DefaultBranch: "main",
		Runner:        "ubuntu-latest",
		PluginInstall: true,
		PlanGuard:     true,
		Phases: []cigen.DeployPhase{
			{Name: "prereq", ConfigPath: "deploy.prereq.yaml"},
			{Name: "deploy", ConfigPath: "deploy.yaml"},
		},
		Migrations: &cigen.MigrationsSpec{
			DBEnv:  "APP_DB_URL",
			Source: "migrations",
		},
		Smoke: &cigen.SmokeSpec{
			URL:  "https://myapp.example.com/healthz",
			Path: "/healthz",
		},
		Secrets: []cigen.SecretRef{
			{Name: "SECRET_ONE"},
			{Name: "SECRET_TWO"},
			{Name: "APP_DB_URL"},
		},
		Triggers: cigen.TriggerSpec{PR: true, PushMain: true, Dispatch: true},
		Warnings: []string{},
	}
}
