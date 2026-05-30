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

	// Must emit the REAL migration runner. `wfctl ci run --phase migrate` is not
	// a valid phase (ci run only accepts build|test|deploy) and would fail at
	// runtime, so it must NOT appear.
	if !strings.Contains(content, "wfctl migrations up --config") {
		t.Errorf("expected migrations step to run 'wfctl migrations up --config', got:\n%s", content)
	}
	if strings.Contains(content, "--phase migrate") {
		t.Error("migrations step must NOT use 'wfctl ci run --phase migrate' (not a valid phase)")
	}

	// The DB secret must still be available to the migrations step via the
	// apply job's job-level `env:` block (deriveSecrets always adds DBEnv to
	// the union). The migrations step must NOT re-declare it with a redundant
	// step-level `env:`.
	if !strings.Contains(content, "      APP_DB_URL: ${{ secrets.APP_DB_URL }}") {
		t.Errorf("expected DBEnv secret in job-level env block, got:\n%s", content)
	}
	migIdx := strings.Index(content, "- name: Run migrations")
	if migIdx < 0 {
		t.Fatalf("expected a 'Run migrations' step, got:\n%s", content)
	}
	// Slice from the migrations step to the next step (Apply) and assert no
	// step-level env block appears inside it.
	rest := content[migIdx:]
	if nextIdx := strings.Index(rest[1:], "- name:"); nextIdx >= 0 {
		rest = rest[:nextIdx+1]
	}
	if strings.Contains(rest, "env:") {
		t.Errorf("migrations step must NOT carry a redundant step-level env: block, got:\n%s", rest)
	}
}

func TestRenderGitHubActions_MigrationsStep_WithEnv(t *testing.T) {
	plan := richCIPlan()
	plan.Migrations.Env = "prod"

	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	if !strings.Contains(content, "wfctl migrations up --config") {
		t.Errorf("expected 'wfctl migrations up --config' in output, got:\n%s", content)
	}
	if !strings.Contains(content, "--env prod") {
		t.Errorf("expected '--env prod' when Migrations.Env is set, got:\n%s", content)
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

func TestRenderGitHubActions_PlanGuardIsRealGate(t *testing.T) {
	plan := richCIPlan() // PlanGuard: true
	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	if !strings.Contains(content, "Plan guard") {
		t.Fatal("expected a Plan guard step when PlanGuard is set")
	}
	// The guard must be a REAL gate, not a no-op.
	if strings.Contains(content, "|| true") {
		t.Error("plan guard must not contain `|| true` (would never fail the job)")
	}
	// It must be able to fail the job.
	if !strings.Contains(content, "exit 1") {
		t.Error("plan guard must contain a failing-exit path (exit 1)")
	}
	// It must detect replace/destroy in the plan output.
	if !strings.Contains(content, "to replace") || !strings.Contains(content, "to destroy") {
		t.Error("plan guard should detect replace/destroy plans")
	}
	// Plan output should stay visible (tee), not silenced with -q.
	if !strings.Contains(content, "tee") {
		t.Error("plan guard should keep plan output visible (tee)")
	}
}

func TestRenderGitHubActions_NoPlanGuardWhenUnset(t *testing.T) {
	plan := richCIPlan()
	plan.PlanGuard = false
	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}
	if strings.Contains(content, "Plan guard") {
		t.Error("did not expect a Plan guard step when PlanGuard is unset")
	}
}

func TestRenderGitHubActions_RelativePathsFilter(t *testing.T) {
	// A plan whose phase config path is already relative must render a relative
	// `paths:` entry with no leading slash. (Analyze is responsible for
	// relativizing absolute paths; the renderer must emit whatever it is given
	// verbatim, and the path filter must not contain an absolute path.)
	plan := &cigen.CIPlan{
		Project:       "myapp",
		WfctlVersion:  "latest",
		DefaultBranch: "main",
		Runner:        "ubuntu-latest",
		Phases: []cigen.DeployPhase{
			{Name: "deploy", ConfigPath: "deploy.yaml"},
		},
		Secrets:  []cigen.SecretRef{},
		Triggers: cigen.TriggerSpec{PR: true, PushMain: true, Dispatch: true},
		Warnings: []string{},
	}
	files, err := cigen.RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("RenderGitHubActions: %v", err)
	}
	var content string
	for _, c := range files {
		content = c
		break
	}

	if !strings.Contains(content, "- 'deploy.yaml'") {
		t.Errorf("expected relative paths entry `- 'deploy.yaml'`, got:\n%s", content)
	}
	// No absolute path leaked into the paths: filter.
	if strings.Contains(content, "- '/") {
		t.Error("paths: filter must not contain an absolute path")
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
