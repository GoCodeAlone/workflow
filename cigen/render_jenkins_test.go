package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestRenderJenkins_ConfigDerived(t *testing.T) {
	files, err := cigen.RenderJenkins(richCIPlan())
	if err != nil {
		t.Fatalf("RenderJenkins: %v", err)
	}
	content, ok := files["Jenkinsfile"]
	if !ok {
		t.Fatal("expected Jenkinsfile in output")
	}
	must := []string{
		"pipeline {",
		"// Requires a Jenkins Multibranch Pipeline",                          // C2 header
		"// Required Jenkins credentials: APP_DB_URL, SECRET_ONE, SECRET_TWO", // union, SORTED
		"stage('Apply prereq')",
		"stage('Apply deploy')",
		"environment {",
		"when { changeRequest() }", // plan gate
		"when { branch 'main' }",   // apply gate
		"wfctl migrations up",      // real runner
		"--format json",
		"curl --fail --max-time 30 'https://myapp.example.com/healthz'", // smoke
		"wfctl infra apply --config 'deploy.yaml' --auto-approve",
	}
	for _, m := range must {
		if !strings.Contains(content, m) {
			t.Errorf("Jenkinsfile missing %q\n---\n%s", m, content)
		}
	}
	// Each secret wired individually (robust against header-sort regressions):
	// richCIPlan phases are NOT Scoped, so both apply stages use the p.Secrets union.
	for _, name := range []string{"APP_DB_URL", "SECRET_ONE", "SECRET_TWO"} {
		if !strings.Contains(content, "credentials('"+name+"')") {
			t.Errorf("expected credentials('%s') binding", name)
		}
	}
	if !strings.Contains(content, "exit 1") {
		t.Error("expected plan-guard exit 1")
	}
	for _, banned := range []string{"go test ./...", "wfctl deploy --image", "docker build", "docker push", "wfctl ci run --phase migrate"} {
		if strings.Contains(content, banned) {
			t.Errorf("Jenkinsfile must NOT contain legacy %q", banned)
		}
	}
	if strings.Index(content, "stage('Apply prereq')") > strings.Index(content, "stage('Apply deploy')") {
		t.Error("expected Apply prereq stage before Apply deploy stage")
	}
}

func TestRenderJenkins_NilPlan(t *testing.T) {
	if _, err := cigen.RenderJenkins(nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

// TestRenderJenkins_ScopedPhase locks in the phase.Scoped branch: a scoped phase
// must bind ONLY its own secrets, not the plan-wide union.
func TestRenderJenkins_ScopedPhase(t *testing.T) {
	p := &cigen.CIPlan{
		DefaultBranch: "main",
		Secrets:       []cigen.SecretRef{{Name: "UNION_ONLY"}},
		Phases: []cigen.DeployPhase{
			{Name: "prereq", ConfigPath: "prereq.yaml", Scoped: true, Secrets: []cigen.SecretRef{{Name: "PREREQ_ONLY"}}},
			{Name: "deploy", ConfigPath: "deploy.yaml", Scoped: true, Secrets: []cigen.SecretRef{{Name: "DEPLOY_ONLY"}}},
		},
	}
	content := mustJenkins(t, p)
	prereq := stageBlock(content, "Apply prereq")
	deploy := stageBlock(content, "Apply deploy")
	if !strings.Contains(prereq, "credentials('PREREQ_ONLY')") {
		t.Errorf("prereq stage must bind PREREQ_ONLY\n%s", prereq)
	}
	if strings.Contains(prereq, "credentials('DEPLOY_ONLY')") || strings.Contains(prereq, "credentials('UNION_ONLY')") {
		t.Errorf("prereq stage must NOT bind other phases' / union secrets\n%s", prereq)
	}
	if !strings.Contains(deploy, "credentials('DEPLOY_ONLY')") {
		t.Errorf("deploy stage must bind DEPLOY_ONLY\n%s", deploy)
	}
	// Header union spans both scoped phases.
	if !strings.Contains(content, "// Required Jenkins credentials: DEPLOY_ONLY, PREREQ_ONLY, UNION_ONLY") {
		t.Errorf("expected sorted union header\n%s", content)
	}
}

func mustJenkins(t *testing.T, p *cigen.CIPlan) string {
	t.Helper()
	files, err := cigen.RenderJenkins(p)
	if err != nil {
		t.Fatalf("RenderJenkins: %v", err)
	}
	return files["Jenkinsfile"]
}

// stageBlock returns the text of the named stage up to the next stage or EOF.
func stageBlock(content, stageName string) string {
	start := strings.Index(content, "stage('"+stageName+"')")
	if start < 0 {
		return ""
	}
	rest := content[start+1:]
	if next := strings.Index(rest, "    stage('"); next >= 0 {
		return content[start : start+1+next]
	}
	return content[start:]
}

func TestRenderJenkins_SinglePhase(t *testing.T) {
	p := richCIPlan()
	p.Phases = []cigen.DeployPhase{{Name: "deploy", ConfigPath: "deploy.yaml"}}
	files, err := cigen.RenderJenkins(p)
	if err != nil {
		t.Fatalf("RenderJenkins single-phase: %v", err)
	}
	if !strings.Contains(files["Jenkinsfile"], "stage('Apply deploy')") {
		t.Error("expected single Apply deploy stage")
	}
}
