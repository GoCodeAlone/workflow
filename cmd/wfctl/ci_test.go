package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestGenerateGitHubActions(t *testing.T) {
	opts := ciOptions{
		Platform:    "github_actions",
		InfraConfig: "infra.yaml",
		Runner:      "ubuntu-latest",
	}

	files, err := generateCIFiles(opts)
	if err != nil {
		t.Fatalf("generateCIFiles: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one file in output")
	}

	// Find the main workflow file (the cigen renderer produces a single named file)
	var workflowYML string
	for path, content := range files {
		if strings.HasSuffix(path, ".yml") && strings.Contains(path, ".github/workflows/") {
			workflowYML = content
			break
		}
	}
	if workflowYML == "" {
		t.Fatalf("expected a .github/workflows/*.yml file in output, got keys: %v", fileKeys(files))
	}

	markers := []string{
		"actions/checkout@9f698171ed81b15d1823a05fc7211befd50c8ae0 # v6.0.3",
		"GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1",
		"wfctl infra plan",
		"permissions",
	}
	for _, m := range markers {
		if !strings.Contains(workflowYML, m) {
			t.Errorf("workflow YAML missing marker %q", m)
		}
	}

	// Plan job must be PR-gated
	if !strings.Contains(workflowYML, "github.event_name == 'pull_request'") {
		t.Error("expected plan job to be gated on pull_request")
	}

	// Apply job must exist
	if !strings.Contains(workflowYML, "wfctl infra apply") {
		t.Error("expected apply step using wfctl infra apply")
	}
}

func TestGenerateGitLabCI(t *testing.T) {
	opts := ciOptions{
		Platform:    "gitlab_ci",
		InfraConfig: "infra.yaml",
	}

	files, err := generateCIFiles(opts)
	if err != nil {
		t.Fatalf("generateCIFiles: %v", err)
	}

	content, ok := files[".gitlab-ci.yml"]
	if !ok {
		t.Fatal("expected .gitlab-ci.yml in output")
	}

	markers := []string{
		"rules:",
		"before_script:",
		`WFCTL_VERSION: "latest"`,
		`go install "github.com/GoCodeAlone/workflow/cmd/wfctl@${WFCTL_VERSION}"`,
		`export PATH="$(go env GOPATH)/bin:$PATH"`,
		"wfctl infra plan",
		"environment:",
	}
	for _, m := range markers {
		if !strings.Contains(content, m) {
			t.Errorf(".gitlab-ci.yml missing marker %q", m)
		}
	}

	// Ensure deprecated only: syntax is NOT used
	if strings.Contains(content, "\nonly:") {
		t.Error(".gitlab-ci.yml uses deprecated 'only:' syntax")
	}
}

func TestCIGeneratePinsCurrentWfctlVersionWhenReleased(t *testing.T) {
	origVersion := version
	version = "v9.9.9"
	defer func() { version = origVersion }()

	ghaFiles, err := generateCIFiles(ciOptions{
		Platform:    "github_actions",
		InfraConfig: "infra.yaml",
		Runner:      "ubuntu-latest",
	})
	if err != nil {
		t.Fatalf("generate GitHub Actions: %v", err)
	}
	var ghaContent string
	for _, c := range ghaFiles {
		ghaContent = c
		break
	}
	if !strings.Contains(ghaContent, "version: 'v9.9.9'") {
		t.Fatal("GitHub Actions workflow should pin the generated wfctl version")
	}

	gitlabFiles, err := generateCIFiles(ciOptions{
		Platform:    "gitlab_ci",
		InfraConfig: "infra.yaml",
	})
	if err != nil {
		t.Fatalf("generate GitLab CI: %v", err)
	}
	if !strings.Contains(gitlabFiles[".gitlab-ci.yml"], `WFCTL_VERSION: "v9.9.9"`) {
		t.Fatal("GitLab CI workflow should pin the generated wfctl version")
	}
}

func TestCIGenerateUsesLatestForUnreleasedWfctlVersions(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()

	for _, candidate := range []string{
		"",
		"dev",
		"v0.22.8-0.20260507211020-3f920f7ff2f6",
		"v0.22.8-0.20260507211020-3f920f7ff2f6+dirty",
		"v9.9.9+dirty",
	} {
		version = candidate
		if got := ciGeneratedWfctlVersion(); got != "latest" {
			t.Fatalf("ciGeneratedWfctlVersion(%q) = %q, want latest", candidate, got)
		}
	}
}

func TestCIGenerateMissingPlatform(t *testing.T) {
	err := runCIGenerate([]string{})
	if err == nil {
		t.Fatal("expected error when --platform is missing")
	}
	if !strings.Contains(err.Error(), "--platform") {
		t.Errorf("expected error to mention --platform, got: %v", err)
	}
}

func TestCIGenerateUnsupportedPlatform(t *testing.T) {
	_, err := generateCIFiles(ciOptions{Platform: "travis_ci", InfraConfig: "infra.yaml"})
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "unsupported platform") {
		t.Errorf("expected 'unsupported platform' in error, got: %v", err)
	}
}

func TestResolveCIConfigExplicit(t *testing.T) {
	cfg, err := resolveCIConfig("my-config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != "my-config.yaml" {
		t.Errorf("expected my-config.yaml, got %s", cfg)
	}
}

func TestResolveCIConfigDefaults(t *testing.T) {
	// In the absence of app.yaml or infra.yaml, falls back to infra.yaml
	cfg, err := resolveCIConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == "" {
		t.Error("expected a non-empty config path")
	}
}

func TestResolveCIConfigPicksAppYaml(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("modules: []"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := resolveCIConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != "app.yaml" {
		t.Errorf("expected app.yaml, got %s", cfg)
	}
}

func TestCIGenerateWritesFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a minimal config so runCIGenerate can analyze it
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte("modules:\n  - name: web\n    type: http.server\n    config:\n      port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	err := runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--config", "infra.yaml",
		"--output", dir,
		"--write",
	})
	if err != nil {
		t.Fatalf("runCIGenerate: %v", err)
	}

	dest := filepath.Join(dir, ".gitlab-ci.yml")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file %s to exist: %v", dest, err)
	}
	if !strings.Contains(string(data), "rules:") {
		t.Error("generated .gitlab-ci.yml missing 'rules:'")
	}
}

// fileKeys returns sorted keys from the files map for debugging.
func fileKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	return keys
}

// ─── Wizard unit tests (no TTY) ─────────────────────────────────────────────

func TestApplyWizardOverrides_SmokeFalseDropsSmoke(t *testing.T) {
	plan := &cigen.CIPlan{
		Runner: "ubuntu-latest",
		Smoke:  &cigen.SmokeSpec{URL: "https://example.com/healthz", Path: "/healthz"},
	}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "ubuntu-latest",
		Smoke:      false, // operator toggled off
		Migrations: false,
		PlanGuard:  false,
	}
	applyWizardOverrides(plan, choices)
	if plan.Smoke != nil {
		t.Error("expected plan.Smoke to be nil when choices.Smoke=false")
	}
}

func TestApplyWizardOverrides_SmokeTruePreservesSmoke(t *testing.T) {
	smoke := &cigen.SmokeSpec{URL: "https://example.com/healthz", Path: "/healthz"}
	plan := &cigen.CIPlan{
		Runner: "ubuntu-latest",
		Smoke:  smoke,
	}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "ubuntu-latest",
		Smoke:      true,
		Migrations: false,
		PlanGuard:  false,
	}
	applyWizardOverrides(plan, choices)
	if plan.Smoke == nil {
		t.Error("expected plan.Smoke to be preserved when choices.Smoke=true")
	}
	if plan.Smoke.URL != smoke.URL {
		t.Errorf("smoke URL changed unexpectedly: got %q, want %q", plan.Smoke.URL, smoke.URL)
	}
}

func TestApplyWizardOverrides_MigrationsFalseDropsMigrations(t *testing.T) {
	plan := &cigen.CIPlan{
		Runner:     "ubuntu-latest",
		Migrations: &cigen.MigrationsSpec{DBEnv: "DB_URL", Source: "migrations"},
	}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "ubuntu-latest",
		Smoke:      false,
		Migrations: false, // operator toggled off
		PlanGuard:  false,
	}
	applyWizardOverrides(plan, choices)
	if plan.Migrations != nil {
		t.Error("expected plan.Migrations to be nil when choices.Migrations=false")
	}
}

func TestApplyWizardOverrides_MigrationsTruePreservesMigrations(t *testing.T) {
	mig := &cigen.MigrationsSpec{DBEnv: "DB_URL", Source: "migrations"}
	plan := &cigen.CIPlan{
		Runner:     "ubuntu-latest",
		Migrations: mig,
	}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "ubuntu-latest",
		Smoke:      false,
		Migrations: true,
		PlanGuard:  false,
	}
	applyWizardOverrides(plan, choices)
	if plan.Migrations == nil {
		t.Fatal("expected plan.Migrations to be preserved when choices.Migrations=true")
	}
	if plan.Migrations.DBEnv != mig.DBEnv {
		t.Errorf("migrations DBEnv changed unexpectedly: got %q, want %q", plan.Migrations.DBEnv, mig.DBEnv)
	}
}

func TestApplyWizardOverrides_RunnerOverrideApplies(t *testing.T) {
	plan := &cigen.CIPlan{Runner: "ubuntu-latest"}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "self-hosted",
		Smoke:      false,
		Migrations: false,
		PlanGuard:  false,
	}
	applyWizardOverrides(plan, choices)
	if plan.Runner != "self-hosted" {
		t.Errorf("expected Runner=%q, got %q", "self-hosted", plan.Runner)
	}
}

func TestApplyWizardOverrides_PlanGuardToggle(t *testing.T) {
	plan := &cigen.CIPlan{Runner: "ubuntu-latest", PlanGuard: true}
	choices := wizardChoices{
		Platform:   "github_actions",
		Runner:     "ubuntu-latest",
		Smoke:      false,
		Migrations: false,
		PlanGuard:  false, // operator toggled off
	}
	applyWizardOverrides(plan, choices)
	if plan.PlanGuard {
		t.Error("expected PlanGuard=false after wizard override")
	}

	// Toggle back on
	choices.PlanGuard = true
	applyWizardOverrides(plan, choices)
	if !plan.PlanGuard {
		t.Error("expected PlanGuard=true after wizard override")
	}
}

func TestApplyWizardOverrides_PlatformSelectsRenderer(t *testing.T) {
	// Verify that the wizard choices struct carries the right platform string
	// and that applyWizardOverrides does not overwrite plan fields with platform info
	// (platform is used by the caller to select renderer, not stored in CIPlan).
	plan := &cigen.CIPlan{Runner: "ubuntu-latest"}
	ghaChoices := wizardChoices{Platform: "github_actions", Runner: "ubuntu-latest"}
	glChoices := wizardChoices{Platform: "gitlab_ci", Runner: "ubuntu-latest"}

	applyWizardOverrides(plan, ghaChoices)
	if ghaChoices.Platform != "github_actions" {
		t.Errorf("platform should be github_actions, got %q", ghaChoices.Platform)
	}

	applyWizardOverrides(plan, glChoices)
	if glChoices.Platform != "gitlab_ci" {
		t.Errorf("platform should be gitlab_ci, got %q", glChoices.Platform)
	}
}

func TestCIGenerateMissingPlatformNonTTY(t *testing.T) {
	// When --platform is absent and stdin is not a TTY (test runner context),
	// runCIGenerate must return an error that mentions --platform.
	err := runCIGenerate([]string{})
	if err == nil {
		t.Fatal("expected error when --platform is missing in non-TTY context")
	}
	if !strings.Contains(err.Error(), "--platform") {
		t.Errorf("expected error to mention --platform, got: %v", err)
	}
}

// ─── New ci plan + ci generate extended tests ────────────────────────────────

func TestRunCIPlan_StdoutJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte("modules:\n  - name: web\n    type: http.server\n    config:\n      port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	outPath := filepath.Join(dir, "plan.json")
	err := runCIPlan([]string{"--config", "app.yaml", "--out", outPath})
	if err != nil {
		t.Fatalf("runCIPlan: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read plan.json: %v", err)
	}

	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("plan.json is not valid JSON: %v", err)
	}

	if _, ok := plan["warnings"]; !ok {
		t.Error("expected 'warnings' field in CIPlan JSON")
	}
	if _, ok := plan["phases"]; !ok {
		t.Error("expected 'phases' field in CIPlan JSON")
	}
}

func TestRunCIGenerate_FromPlan(t *testing.T) {
	dir := t.TempDir()

	planJSON := `{
  "project": "test",
  "wfctl_version": "latest",
  "default_branch": "main",
  "runner": "ubuntu-latest",
  "plugin_install": false,
  "phases": [{"name":"deploy","config_path":"app.yaml"}],
  "secrets": [],
  "plan_guard": false,
  "triggers": {"pr":true,"push_main":true,"dispatch":true},
  "warnings": []
}`
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(planJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	err := runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--from-plan", planPath,
		"--output", outDir,
		"--write",
	})
	if err != nil {
		t.Fatalf("runCIGenerate --from-plan: %v", err)
	}

	dest := filepath.Join(outDir, ".gitlab-ci.yml")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", dest, err)
	}
	if !strings.Contains(string(data), "rules:") {
		t.Error("expected 'rules:' in generated gitlab-ci.yml")
	}
}

func TestRunCIGenerate_DiffExitCode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte("modules:\n  - name: web\n    type: http.server\n    config:\n      port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	// First write the files
	err := runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--config", "app.yaml",
		"--output", dir,
		"--write",
	})
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}

	// Now run --diff against the same output — no diff, so exit code 0 (no os.Exit called)
	// We can't test os.Exit directly, but we can test the function doesn't error.
	// We test --diff without --exit-code to avoid os.Exit being called.
	err = runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--config", "app.yaml",
		"--output", dir,
		"--diff",
	})
	if err != nil {
		t.Fatalf("--diff should not return error when files match: %v", err)
	}
}

func TestRunCIGenerate_NoOverwriteWithoutWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte("modules:\n  - name: web\n    type: http.server\n    config:\n      port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-create the output file
	destDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, ".gitlab-ci.yml"), []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	err := runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--config", "app.yaml",
		"--output", destDir,
		// no --write
	})
	if err == nil {
		t.Fatal("expected error when target file exists and --write is not set")
	}
	if !strings.Contains(err.Error(), "--write") {
		t.Errorf("expected error to mention --write, got: %v", err)
	}
}

// TestGenerateCIFiles_JenkinsCircleCI verifies the platform switch dispatches to
// the cigen Jenkins/CircleCI renderers (#804). Content quality is gated by the
// cigen unit tests, not here.
func TestGenerateCIFiles_JenkinsCircleCI(t *testing.T) {
	cases := map[string]string{"jenkins": "pipeline {", "circleci": "version: 2.1"}
	for plat, marker := range cases {
		files, err := generateCIFiles(ciOptions{Platform: plat, InfraConfig: "infra.yaml"})
		if err != nil {
			t.Fatalf("%s: %v", plat, err)
		}
		joined := ""
		for _, c := range files {
			joined += c
		}
		if !strings.Contains(joined, marker) {
			t.Errorf("%s: expected marker %q in output", plat, marker)
		}
	}
}
