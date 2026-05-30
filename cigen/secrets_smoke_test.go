package cigen_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestAnalyze_SecretsUnion_Multisite(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/multisite.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	secretNames := make(map[string]bool, len(plan.Secrets))
	for _, s := range plan.Secrets {
		secretNames[s.Name] = true
	}

	// From secrets.entries
	wantFromEntries := []string{
		"DIGITALOCEAN_TOKEN",
		"RELEASES_TOKEN",
		"GHCR_CREDENTIALS",
		"SPACES_access_key",
		"SPACES_secret_key",
		"MULTISITE_DB_URL",
	}
	for _, name := range wantFromEntries {
		if !secretNames[name] {
			t.Errorf("expected secret %q from entries, not found in plan.Secrets", name)
		}
	}

	// From env_vars_secret ${VAR} refs
	wantFromEnvVarsSecret := []string{
		"MULTISITE_DB_URL", // also in entries, deduplicated
		"MULTISITE_INGEST_HMAC_SECRET",
		"MULTISITE_JWT_SECRET",
	}
	for _, name := range wantFromEnvVarsSecret {
		if !secretNames[name] {
			t.Errorf("expected secret %q from env_vars_secret refs, not found in plan.Secrets", name)
		}
	}

	// From iac.provider token/spaces fields
	wantFromIaC := []string{
		"DIGITALOCEAN_TOKEN",
		"SPACES_access_key",
		"SPACES_secret_key",
	}
	for _, name := range wantFromIaC {
		if !secretNames[name] {
			t.Errorf("expected secret %q from iac.provider config, not found in plan.Secrets", name)
		}
	}

	// From migrations DBEnv
	if !secretNames["MULTISITE_DB_URL"] {
		t.Error("expected MULTISITE_DB_URL from migrations.DBEnv in plan.Secrets")
	}

	// No duplicates
	if len(plan.Secrets) != len(secretNames) {
		t.Errorf("expected no duplicates: %d unique names but %d entries", len(secretNames), len(plan.Secrets))
	}
}

func TestAnalyze_Smoke_Multisite(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/multisite.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if plan.Smoke == nil {
		t.Fatal("expected Smoke to be non-nil")
	}
	if plan.Smoke.URL != "https://gocodealone.tech/healthz" {
		t.Errorf("Smoke.URL = %q, want %q", plan.Smoke.URL, "https://gocodealone.tech/healthz")
	}
	if plan.Smoke.Path != "/healthz" {
		t.Errorf("Smoke.Path = %q, want %q", plan.Smoke.Path, "/healthz")
	}
}

func TestAnalyze_Warnings_Multisite(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/multisite.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// Should have state-derived warning for MULTISITE_DB_URL (IaC output → migrations)
	hasStateWarning := false
	hasCaseWarning := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "MULTISITE_DB_URL") && strings.Contains(w, "IaC output") {
			hasStateWarning = true
		}
		// SPACES_access_key and SPACES_secret_key don't match ^[A-Z0-9_]+$
		if strings.Contains(w, "SPACES_access_key") || strings.Contains(w, "SPACES_secret_key") {
			hasCaseWarning = true
		}
	}

	if !hasStateWarning {
		t.Errorf("expected state-derived warning for MULTISITE_DB_URL; warnings: %v", plan.Warnings)
	}
	if !hasCaseWarning {
		t.Errorf("expected case-mismatch warning for SPACES_access_key/SPACES_secret_key; warnings: %v", plan.Warnings)
	}
}

func TestAnalyze_NoSmoke_MinimalConfig(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/minimal.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.Smoke != nil {
		t.Errorf("expected no Smoke for minimal config, got %+v", plan.Smoke)
	}
}
