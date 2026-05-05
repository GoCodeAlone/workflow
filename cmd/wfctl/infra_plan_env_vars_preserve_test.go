package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInfraResourceSpecs_PreservesEnvVarRefs(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "actual-do-token")
	t.Setenv("IMAGE_REF", "registry.example.com/api:abc123")
	t.Setenv("AUTH_TOKEN", "would-be-resolved-secret")
	t.Setenv("DATABASE_URL", "postgres://would-be-resolved")

	specs, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}

	// Find the example-app spec (do-provider is iac.provider, not infra.*, so it's excluded).
	var appCfg map[string]any
	for _, s := range specs {
		if s.Name == "example-app" {
			appCfg = s.Config
			break
		}
	}
	if appCfg == nil {
		t.Fatal("example-app spec not found in parsed specs")
	}

	// services is a slice of service maps.
	servicesRaw, ok := appCfg["services"]
	if !ok {
		t.Fatal("example-app config missing 'services' key")
	}
	services, ok := servicesRaw.([]any)
	if !ok {
		t.Fatalf("services: expected []any, got %T", servicesRaw)
	}
	if len(services) == 0 {
		t.Fatal("services slice is empty")
	}
	api, ok := services[0].(map[string]any)
	if !ok {
		t.Fatalf("services[0]: expected map[string]any, got %T", services[0])
	}

	// Top-level image IS resolved (not in preserve list).
	if got := api["image"]; got != "registry.example.com/api:abc123" {
		t.Errorf("api.image: got %q, want resolved literal registry.example.com/api:abc123", got)
	}

	// env_vars contents are PRESERVED as ${VAR} literals.
	envVarsRaw, ok := api["env_vars"]
	if !ok {
		t.Fatal("api config missing 'env_vars' key")
	}
	envVars, ok := envVarsRaw.(map[string]any)
	if !ok {
		t.Fatalf("env_vars: expected map[string]any, got %T", envVarsRaw)
	}
	if got := envVars["AUTH_TOKEN"]; got != "${AUTH_TOKEN}" {
		t.Errorf("env_vars.AUTH_TOKEN: got %q, want literal ${AUTH_TOKEN} (should NOT have been resolved)", got)
	}
	if got := envVars["DEPLOY_ENV"]; got != "staging" {
		t.Errorf("env_vars.DEPLOY_ENV: got %q, want literal staging", got)
	}

	// env_vars_secret contents are PRESERVED as ${VAR} literals.
	envSecretRaw, ok := api["env_vars_secret"]
	if !ok {
		t.Fatal("api config missing 'env_vars_secret' key")
	}
	envSecret, ok := envSecretRaw.(map[string]any)
	if !ok {
		t.Fatalf("env_vars_secret: expected map[string]any, got %T", envSecretRaw)
	}
	if got := envSecret["DB_URL"]; got != "${DATABASE_URL}" {
		t.Errorf("env_vars_secret.DB_URL: got %q, want literal ${DATABASE_URL} (should NOT have been resolved)", got)
	}
}

// TestPlanEnvVarPreserveTestdataExists ensures the fixture file exists and
// has the env_vars_secret block required for the preservation test.
func TestPlanEnvVarPreserveTestdataExists(t *testing.T) {
	p := filepath.Join("testdata", "infra-with-env-var-refs.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("testdata fixture missing: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	if !strings.Contains(string(b), "env_vars_secret") {
		t.Errorf("fixture missing env_vars_secret block — needed for preservation test")
	}
}
