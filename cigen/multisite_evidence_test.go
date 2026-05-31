package cigen

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMultisiteEvidence_Honest locks the committed demonstration-fidelity
// artifact. It reads the on-disk generated-infra.yml (NOT a freshly rendered
// plan) and asserts exactly the honest claims in GAP.md: apply-prereq is
// scoped (no deploy-only DB secret), apply-deploy keeps it, and the migrations
// step carries --format json but NOT --env (multisite declares no environments).
func TestMultisiteEvidence_Honest(t *testing.T) {
	b, err := os.ReadFile("testdata/multisite/generated-infra.yml")
	if err != nil {
		t.Fatalf("read committed evidence: %v", err)
	}
	yml := string(b)

	var doc struct {
		Jobs map[string]struct {
			Env map[string]any `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("committed evidence is not valid YAML: %v", err)
	}
	if _, ok := doc.Jobs["apply-prereq"].Env["MULTISITE_DB_URL"]; ok {
		t.Errorf("apply-prereq must NOT carry the deploy-only MULTISITE_DB_URL (scoping gap #3)")
	}
	if _, ok := doc.Jobs["apply-deploy"].Env["MULTISITE_DB_URL"]; !ok {
		t.Errorf("apply-deploy must carry MULTISITE_DB_URL")
	}
	if !strings.Contains(yml, "wfctl migrations up --config 'deploy.yaml' --format json") {
		t.Errorf("migrations step must emit --format json (gap #4)")
	}
	if strings.Contains(yml, "migrations up") && strings.Contains(yml, "--env ") {
		t.Errorf("multisite declares no ci.migrations.environments → --env must NOT be emitted (honest, design C1)")
	}
}
