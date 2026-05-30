package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
	"gopkg.in/yaml.v3"
)

func TestRenderGitLabCI_ValidYAML(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}

	content, ok := files[".gitlab-ci.yml"]
	if !ok {
		t.Fatal("expected .gitlab-ci.yml in output")
	}

	var parsed any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Errorf(".gitlab-ci.yml is not valid YAML: %v\ncontent:\n%s", err, content)
	}
}

func TestRenderGitLabCI_Stages(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]

	for _, stage := range []string{"plan", "apply", "smoke"} {
		if !strings.Contains(content, "- "+stage) {
			t.Errorf("expected stage %q in output", stage)
		}
	}
}

func TestRenderGitLabCI_SecretRefs(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]

	for _, s := range plan.Secrets {
		if !strings.Contains(content, "$"+s.Name) {
			t.Errorf("expected $%s in output", s.Name)
		}
	}
}

func TestRenderGitLabCI_TwoPhaseNeeds(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]

	// Two-phase plan
	if !strings.Contains(content, "plan-prereq:") {
		t.Error("expected plan-prereq job")
	}
	if !strings.Contains(content, "plan-deploy:") {
		t.Error("expected plan-deploy job")
	}

	// Two-phase apply with needs
	if !strings.Contains(content, "apply-prereq:") {
		t.Error("expected apply-prereq job")
	}
	if !strings.Contains(content, "apply-deploy:") {
		t.Error("expected apply-deploy job")
	}
	if !strings.Contains(content, "job: apply-prereq") {
		t.Error("expected apply-deploy to need apply-prereq")
	}
}

func TestRenderGitLabCI_NilPlan(t *testing.T) {
	_, err := cigen.RenderGitLabCI(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRenderGitLabCI_NoDeprecatedOnlySyntax(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]

	if strings.Contains(content, "\nonly:") {
		t.Error(".gitlab-ci.yml uses deprecated 'only:' syntax")
	}
}
