package cigen

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestAnalyze_PerPhaseScoping_PrereqExcludesDeploySecret(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig: "testdata/multisite/deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(plan.Phases))
	}
	prereq, deploy := plan.Phases[0], plan.Phases[1]
	if !prereq.Scoped || !deploy.Scoped {
		t.Fatalf("expected both phases Scoped, got prereq=%v deploy=%v", prereq.Scoped, deploy.Scoped)
	}
	if hasSecret(prereq.Secrets, "MULTISITE_DB_URL") {
		t.Errorf("prereq phase must NOT carry the deploy-only migration secret MULTISITE_DB_URL; got %v", names(prereq.Secrets))
	}
	if !hasSecret(deploy.Secrets, "MULTISITE_DB_URL") {
		t.Errorf("deploy (last) phase must carry MULTISITE_DB_URL; got %v", names(deploy.Secrets))
	}
	// prereq genuinely needs the provider token — sanity that scoping isn't empty.
	if !hasSecret(prereq.Secrets, "DIGITALOCEAN_TOKEN") {
		t.Errorf("prereq phase should carry DIGITALOCEAN_TOKEN; got %v", names(prereq.Secrets))
	}
}

func TestAnalyze_SinglePhase_NotScoped(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Scoped {
		t.Errorf("single-phase deploy must not be Scoped (union is its scope)")
	}
}

func TestAnalyze_PhaseConfigAliasOnly_FallsBackToUnion(t *testing.T) {
	plan, err := Analyze([]string{"testdata/multisite/deploy.yaml"}, Options{
		PhaseConfig:      "/nonexistent/deploy.prereq.yaml",
		PhaseConfigAlias: "deploy.prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.Phases[0].Scoped {
		t.Errorf("alias-only/unloadable phase config must fall back (Scoped=false)")
	}
	if !containsSubstr(plan.Warnings, "per-phase secret scoping unavailable") {
		t.Errorf("expected an unscopable warning; got %v", plan.Warnings)
	}
}

func TestDeriveMigrations_SingleEnvDerived(t *testing.T) {
	cfg, err := config.LoadFromFile("testdata/migrations-one-env.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := deriveMigrations(cfg)
	if m == nil || m.Env != "prod" {
		t.Fatalf("expected Env=prod, got %+v", m)
	}
}

func TestDeriveMigrations_TwoEnvsAmbiguous(t *testing.T) {
	cfg, err := config.LoadFromFile("testdata/migrations-two-envs.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := deriveMigrations(cfg)
	if m == nil || m.Env != "" {
		t.Fatalf("expected Env empty (ambiguous), got %+v", m)
	}
	w := deriveWarnings(cfg, m, deriveSecrets(cfg, m))
	if !containsSubstr(w, "migrations environment ambiguous") {
		t.Errorf("expected ambiguity warning; got %v", w)
	}
}

// test helpers
func hasSecret(s []SecretRef, name string) bool {
	for _, r := range s {
		if r.Name == name {
			return true
		}
	}
	return false
}
func names(s []SecretRef) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, r.Name)
	}
	return out
}
func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
