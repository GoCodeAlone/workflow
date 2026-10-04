package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
	"gopkg.in/yaml.v3"
)

func TestRenderCircleCI_ValidYAMLAndStructure(t *testing.T) {
	files, err := cigen.RenderCircleCI(richCIPlan())
	if err != nil {
		t.Fatalf("RenderCircleCI: %v", err)
	}
	content, ok := files[".circleci/config.yml"]
	if !ok {
		t.Fatal("expected .circleci/config.yml in output")
	}
	var parsed any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, content)
	}
	must := []string{
		"version: 2.1",
		"workflows:",
		"plan-prereq", "plan-deploy",
		"apply-prereq", "apply-deploy",
		"requires:", // CircleCI graph keyword (NOT GHA needs:)
		"wfctl migrations up", "--format json",
		"wfctl infra apply --config 'deploy.yaml' --auto-approve",
		"curl --fail --max-time 30 'https://myapp.example.com/healthz'",
	}
	for _, m := range must {
		if !strings.Contains(content, m) {
			t.Errorf(".circleci/config.yml missing %q\n---\n%s", m, content)
		}
	}
	if strings.Contains(content, "needs:") {
		t.Error("CircleCI uses requires:, not GHA needs:")
	}
	// Positive secret-wiring: each secret name must appear (referenced by an apply
	// job), so a renderer that emits NO secret wiring fails this.
	for _, s := range richCIPlan().Secrets {
		if !strings.Contains(content, s.Name) {
			t.Errorf("expected secret %s referenced in output", s.Name)
		}
		// CircleCI auto-injects project env vars; no redundant NAME: $NAME re-declare.
		if strings.Contains(content, "  "+s.Name+": $"+s.Name) {
			t.Errorf("redundant secret re-declare for %s", s.Name)
		}
	}
	if !strings.Contains(content, "exit 1") {
		t.Error("expected plan-guard exit 1")
	}
	for _, banned := range []string{"go test ./...", "wfctl deploy --image", "docker build", "wfctl ci run --phase migrate"} {
		if strings.Contains(content, banned) {
			t.Errorf("must NOT contain legacy %q", banned)
		}
	}
}

func TestRenderCircleCI_NilPlan(t *testing.T) {
	if _, err := cigen.RenderCircleCI(nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRenderCircleCI_GoVersionImages(t *testing.T) {
	files, err := cigen.RenderCircleCI(richCIPlan())
	if err != nil {
		t.Fatalf("RenderCircleCI: %v", err)
	}
	var parsed struct {
		Jobs map[string]struct {
			Docker []struct {
				Image string `yaml:"image"`
			} `yaml:"docker"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(files[".circleci/config.yml"]), &parsed); err != nil {
		t.Fatalf("parse CircleCI config: %v", err)
	}
	for _, name := range []string{"plan-prereq", "plan-deploy", "apply-prereq", "apply-deploy", "smoke"} {
		t.Run(name, func(t *testing.T) {
			job, ok := parsed.Jobs[name]
			if !ok {
				t.Fatalf("missing job %s", name)
			}
			if len(job.Docker) != 1 {
				t.Fatalf("job %s has %d Docker images, want 1", name, len(job.Docker))
			}
			want := "cimg/go:1.27.1"
			if name == "smoke" {
				want = "cimg/base:current"
			}
			if got := job.Docker[0].Image; got != want {
				t.Errorf("job %s image = %q, want %q", name, got, want)
			}
		})
	}
}

func TestRenderCircleCI_SinglePhase(t *testing.T) {
	p := richCIPlan()
	p.Phases = []cigen.DeployPhase{{Name: "deploy", ConfigPath: "deploy.yaml"}}
	files, err := cigen.RenderCircleCI(p)
	if err != nil {
		t.Fatalf("single-phase: %v", err)
	}
	content := files[".circleci/config.yml"]
	if !strings.Contains(content, "apply") {
		t.Error("expected an apply job for single phase")
	}
}
