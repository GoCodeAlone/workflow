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
