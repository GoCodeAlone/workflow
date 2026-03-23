package bdd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/wftest/bdd"
)

// writeFile creates path (and parent dirs) with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

const sampleAppYAML = `
pipelines:
  greet:
    steps:
      - name: hello
        type: step.set
        config:
          values:
            message: "world"
  users-list:
    trigger:
      type: http
      config:
        method: GET
        path: /api/users
    steps:
      - name: reply
        type: step.json_response
        config:
          status: 200
  users-create:
    trigger:
      type: http
      config:
        method: POST
        path: /api/users
    steps:
      - name: reply
        type: step.json_response
        config:
          status: 201
  health:
    trigger:
      type: http
      config:
        method: GET
        path: /health
    steps:
      - name: reply
        type: step.json_response
        config:
          status: 200
`

func TestCalculateCoverage_ExplicitTag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	writeFile(t, filepath.Join(dir, "features", "greet.feature"), `
Feature: Greeting
  @pipeline:greet
  Scenario: Say hello
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        greet: {}
      """
    When I execute pipeline "greet"
    Then the pipeline should succeed
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if report.TotalPipelines != 4 {
		t.Errorf("TotalPipelines = %d, want 4", report.TotalPipelines)
	}
	if report.TotalScenarios != 1 {
		t.Errorf("TotalScenarios = %d, want 1", report.TotalScenarios)
	}
	if report.ImplementedScenarios != 1 {
		t.Errorf("ImplementedScenarios = %d, want 1", report.ImplementedScenarios)
	}
	if report.UndefinedScenarios != 0 {
		t.Errorf("UndefinedScenarios = %d, want 0", report.UndefinedScenarios)
	}
	if len(report.CoveredPipelines) != 1 || report.CoveredPipelines[0].Pipeline != "greet" {
		t.Errorf("CoveredPipelines = %+v, want [{greet ...}]", report.CoveredPipelines)
	}
	if report.CoveredPipelines[0].Via != "tag" {
		t.Errorf("Via = %q, want \"tag\"", report.CoveredPipelines[0].Via)
	}
}

func TestCalculateCoverage_HTTPRouteMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	writeFile(t, filepath.Join(dir, "features", "api.feature"), `
Feature: Users API
  Scenario: List users
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        users-list: {}
      """
    When I GET "/api/users"
    Then the response status should be 200

  Scenario: Create user
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        users-create: {}
      """
    When I POST "/api/users" with JSON:
      """json
      {"name": "alice"}
      """
    Then the response status should be 201
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if report.TotalScenarios != 2 {
		t.Errorf("TotalScenarios = %d, want 2", report.TotalScenarios)
	}
	if report.ImplementedScenarios != 2 {
		t.Errorf("ImplementedScenarios = %d, want 2", report.ImplementedScenarios)
	}

	coveredNames := make(map[string]string)
	for _, e := range report.CoveredPipelines {
		coveredNames[e.Pipeline] = e.Via
	}
	if coveredNames["users-list"] != "route" {
		t.Errorf("users-list via = %q, want \"route\"", coveredNames["users-list"])
	}
	if coveredNames["users-create"] != "route" {
		t.Errorf("users-create via = %q, want \"route\"", coveredNames["users-create"])
	}
}

func TestCalculateCoverage_UncoveredPipelines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	// Only one scenario covering one pipeline.
	writeFile(t, filepath.Join(dir, "features", "partial.feature"), `
Feature: Partial coverage
  @pipeline:greet
  Scenario: Say hello
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        greet: {}
      """
    When I execute pipeline "greet"
    Then the pipeline should succeed
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if len(report.CoveredPipelines) != 1 {
		t.Errorf("CoveredPipelines = %d, want 1", len(report.CoveredPipelines))
	}
	// health, users-create, users-list should be uncovered.
	if len(report.UncoveredPipelines) != 3 {
		t.Errorf("UncoveredPipelines = %v, want 3 entries", report.UncoveredPipelines)
	}
}

func TestCalculateCoverage_EmptyFeatureDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	featureDir := filepath.Join(dir, "features")
	if err := os.MkdirAll(featureDir, 0o750); err != nil {
		t.Fatal(err)
	}

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), featureDir)
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if report.TotalPipelines != 4 {
		t.Errorf("TotalPipelines = %d, want 4", report.TotalPipelines)
	}
	if report.TotalScenarios != 0 {
		t.Errorf("TotalScenarios = %d, want 0", report.TotalScenarios)
	}
	if len(report.UncoveredPipelines) != 4 {
		t.Errorf("UncoveredPipelines = %v, want 4", report.UncoveredPipelines)
	}
}

func TestCalculateCoverage_NoPipelinesInConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), "modules: []\n")
	writeFile(t, filepath.Join(dir, "features", "dummy.feature"), `
Feature: Dummy
  Scenario: No pipelines defined
    Given the workflow engine is loaded with config:
      """yaml
      pipelines: {}
      """
    When I execute pipeline "greet"
    Then the pipeline should succeed
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if report.TotalPipelines != 0 {
		t.Errorf("TotalPipelines = %d, want 0", report.TotalPipelines)
	}
}

func TestCalculateCoverage_QueryStringStripped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	writeFile(t, filepath.Join(dir, "features", "qs.feature"), `
Feature: Query string ignored
  Scenario: List with filter
    When I GET "/api/users?page=1&limit=10"
    Then the response status should be 200
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	coveredNames := make(map[string]bool)
	for _, e := range report.CoveredPipelines {
		coveredNames[e.Pipeline] = true
	}
	if !coveredNames["users-list"] {
		t.Errorf("users-list should be covered via route (query string should be stripped)")
	}
}

func TestCalculateCoverage_MultipleFeatureFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.yaml"), sampleAppYAML)
	writeFile(t, filepath.Join(dir, "features", "a.feature"), `
Feature: A
  @pipeline:greet
  Scenario: Greet
    When I execute pipeline "greet"
    Then the pipeline should succeed
`)
	writeFile(t, filepath.Join(dir, "features", "b.feature"), `
Feature: B
  Scenario: List
    When I GET "/api/users"
    Then the response status should be 200
  Scenario: Health
    When I GET "/health"
    Then the response status should be 200
`)

	report, err := bdd.CalculateCoverage(filepath.Join(dir, "app.yaml"), filepath.Join(dir, "features"))
	if err != nil {
		t.Fatalf("CalculateCoverage: %v", err)
	}

	if report.TotalScenarios != 3 {
		t.Errorf("TotalScenarios = %d, want 3", report.TotalScenarios)
	}
	if report.ImplementedScenarios != 3 {
		t.Errorf("ImplementedScenarios = %d, want 3", report.ImplementedScenarios)
	}
	if len(report.CoveredPipelines) != 3 {
		t.Errorf("CoveredPipelines = %d, want 3", len(report.CoveredPipelines))
	}
	// users-create is uncovered.
	if len(report.UncoveredPipelines) != 1 || report.UncoveredPipelines[0] != "users-create" {
		t.Errorf("UncoveredPipelines = %v, want [users-create]", report.UncoveredPipelines)
	}
}
