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

func TestRenderGitLabCI_NoRedundantSecretVars(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]
	globalVars := gitLabTopLevelBlock(content, "variables")

	// Project-level CI/CD variables (secrets) are auto-injected by GitLab into
	// every job, so the renderer must NOT re-declare the plan-wide union as
	// `NAME: $NAME` no-ops in the global variables block.
	for _, s := range plan.Secrets {
		redundant := "  " + s.Name + ": $" + s.Name
		if strings.Contains(globalVars, redundant) {
			t.Errorf("expected no redundant `%s: $%s` declaration in variables block", s.Name, s.Name)
		}
	}

	// The only declared pipeline variable should be the wfctl version pin.
	if !strings.Contains(content, "WFCTL_VERSION:") {
		t.Error("expected WFCTL_VERSION variable in output")
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

func TestRenderGitLabCI_PlanGuardIsRealGate(t *testing.T) {
	plan := richCIPlan()

	files, err := cigen.RenderGitLabCI(plan)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]

	if !strings.Contains(content, "plan-guard.txt") {
		t.Fatal("expected a plan guard when PlanGuard is set")
	}
	if strings.Contains(content, "|| true") {
		t.Error("plan guard must not contain `|| true`")
	}
	if !strings.Contains(content, "exit 1") {
		t.Error("plan guard must contain a failing-exit path")
	}
	if !strings.Contains(content, "to replace") || !strings.Contains(content, "to destroy") {
		t.Error("plan guard should detect replace/destroy plans")
	}
	if !strings.Contains(content, "tee plan-guard.txt") {
		t.Error("plan guard should keep plan output visible")
	}
	deploy := gitLabJobBlock(content, "apply-deploy")
	guardIndex := strings.Index(deploy, "plan-guard.txt")
	migrateIndex := strings.Index(deploy, "wfctl migrations up")
	if guardIndex < 0 || migrateIndex < 0 {
		t.Fatalf("expected plan guard and migrations in deploy job\n%s", deploy)
	}
	if guardIndex > migrateIndex {
		t.Fatalf("plan guard must run before migrations\n%s", deploy)
	}
}

func TestRenderGitLabCI_ScopedPhase(t *testing.T) {
	p := &cigen.CIPlan{
		DefaultBranch: "main",
		Secrets:       []cigen.SecretRef{{Name: "UNION_ONLY"}},
		Phases: []cigen.DeployPhase{
			{Name: "prereq", ConfigPath: "prereq.yaml", Scoped: true, Secrets: []cigen.SecretRef{{Name: "PREREQ_ONLY"}}},
			{Name: "deploy", ConfigPath: "deploy.yaml", Scoped: true, Secrets: []cigen.SecretRef{{Name: "DEPLOY_ONLY"}}},
		},
	}

	files, err := cigen.RenderGitLabCI(p)
	if err != nil {
		t.Fatalf("RenderGitLabCI: %v", err)
	}
	content := files[".gitlab-ci.yml"]
	prereq := gitLabJobBlock(content, "apply-prereq")
	deploy := gitLabJobBlock(content, "apply-deploy")

	if !strings.Contains(prereq, "PREREQ_ONLY") {
		t.Errorf("prereq job must reference PREREQ_ONLY\n%s", prereq)
	}
	if strings.Contains(prereq, "DEPLOY_ONLY") || strings.Contains(prereq, "UNION_ONLY") {
		t.Errorf("prereq job must not reference other phases' / union secrets\n%s", prereq)
	}
	if !strings.Contains(deploy, "DEPLOY_ONLY") {
		t.Errorf("deploy job must reference DEPLOY_ONLY\n%s", deploy)
	}
	if strings.Contains(deploy, "PREREQ_ONLY") || strings.Contains(deploy, "UNION_ONLY") {
		t.Errorf("deploy job must not reference other phases' / union secrets\n%s", deploy)
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

func gitLabJobBlock(content, jobName string) string {
	start := strings.Index(content, jobName+":\n")
	if start < 0 {
		return ""
	}
	rest := content[start+len(jobName)+2:]
	if next := strings.Index(rest, "\napply-"); next >= 0 {
		return content[start : start+len(jobName)+2+next]
	}
	if next := strings.Index(rest, "\nsmoke:"); next >= 0 {
		return content[start : start+len(jobName)+2+next]
	}
	return content[start:]
}

func gitLabTopLevelBlock(content, name string) string {
	start := strings.Index(content, name+":\n")
	if start < 0 {
		return ""
	}
	rest := content[start+len(name)+2:]
	if next := strings.Index(rest, "\n\n"); next >= 0 {
		return content[start : start+len(name)+2+next]
	}
	return content[start:]
}
