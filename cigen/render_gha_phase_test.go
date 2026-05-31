package cigen

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// jobEnv returns the rendered `env:` map for a named job, parsed from YAML.
func jobEnv(t *testing.T, yml, job string) map[string]any {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			Env map[string]any `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(yml), &doc); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, yml)
	}
	j, ok := doc.Jobs[job]
	if !ok {
		t.Fatalf("job %q not found in:\n%s", job, yml)
	}
	return j.Env
}

func TestRenderGHA_PerPhaseEnvScoping(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig: "testdata/multisite/deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	files, err := RenderGitHubActions(plan)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var yml string
	for _, c := range files {
		yml = c
	}
	prereqEnv := jobEnv(t, yml, "apply-prereq") // also asserts valid YAML
	deployEnv := jobEnv(t, yml, "apply-deploy")
	if _, ok := prereqEnv["MULTISITE_DB_URL"]; ok {
		t.Errorf("apply-prereq env must NOT contain MULTISITE_DB_URL; got %v", prereqEnv)
	}
	if _, ok := deployEnv["MULTISITE_DB_URL"]; !ok {
		t.Errorf("apply-deploy env must contain MULTISITE_DB_URL; got %v", deployEnv)
	}
}

func TestMigrationsUpCommand_AlwaysFormatJSON(t *testing.T) {
	if got := migrationsUpCommand("deploy.yaml", ""); got != "wfctl migrations up --config 'deploy.yaml' --format json" {
		t.Errorf("no-env: got %q", got)
	}
	if got := migrationsUpCommand("deploy.yaml", "prod"); got != "wfctl migrations up --config 'deploy.yaml' --env prod --format json" {
		t.Errorf("with-env: got %q", got)
	}
}

// Ensure the _existing_ migration substring assertions still hold after adding --format json.
func TestMigrationsUpCommand_SubstringCompat(t *testing.T) {
	cmd := migrationsUpCommand("deploy.yaml", "")
	if !strings.Contains(cmd, "wfctl migrations up --config") {
		t.Errorf("expected wfctl migrations up --config substring; got %q", cmd)
	}
	cmd2 := migrationsUpCommand("deploy.yaml", "prod")
	if !strings.Contains(cmd2, "--env prod") {
		t.Errorf("expected --env prod substring; got %q", cmd2)
	}
}
