package main

import (
	"encoding/json"
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
	t.Setenv("EXTERNAL_API_TOKEN", "would-be-resolved-required-secret")
	// POSTGRES_PASSWORD is in secrets.generate — leave it unset to simulate plan time.

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

// TestParseInfraResourceSpecs_PreservesSecretGenVarsInUserData is the regression
// test for the DesiredHash mismatch bug: a secret declared in secrets.generate
// that appears in a non-env_vars field (e.g. Droplet user_data) must be
// preserved as a literal ${VAR} so that desiredStateHash is identical whether
// or not the variable is present in the environment.
func TestParseInfraResourceSpecs_PreservesSecretGenVarsInUserData(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "actual-do-token")
	t.Setenv("IMAGE_REF", "registry.example.com/api:abc123")
	t.Setenv("AUTH_TOKEN", "would-be-resolved-secret")
	t.Setenv("DATABASE_URL", "postgres://would-be-resolved")

	// --- Plan-time: POSTGRES_PASSWORD is NOT set (as happens in CI).
	// Use t.Setenv so that the original value is restored by t.Cleanup.
	t.Setenv("POSTGRES_PASSWORD", "")
	specsAtPlan, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs (plan): %v", err)
	}
	hashAtPlan := desiredStateHash(specsAtPlan)

	// --- Apply-time: POSTGRES_PASSWORD IS set to the generated value ---
	t.Setenv("POSTGRES_PASSWORD", "deadbeef1234567890abcdef12345678")
	specsAtApply, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs (apply): %v", err)
	}
	hashAtApply := desiredStateHash(specsAtApply)

	if hashAtPlan != hashAtApply {
		// Serialize both spec lists for diagnostics.
		planJSON, _ := json.MarshalIndent(specsAtPlan, "", "  ")
		applyJSON, _ := json.MarshalIndent(specsAtApply, "", "  ")
		t.Errorf("desiredStateHash mismatch between plan and apply:\n"+
			"  plan hash:  %s\n  apply hash: %s\n\nplan specs:\n%s\n\napply specs:\n%s",
			hashAtPlan, hashAtApply, planJSON, applyJSON)
	}

	// Also verify that the user_data field in the droplet spec contains the
	// literal ${POSTGRES_PASSWORD} reference (not an empty string or the value).
	var dropletCfg map[string]any
	for _, s := range specsAtApply {
		if s.Name == "example-droplet" {
			dropletCfg = s.Config
			break
		}
	}
	if dropletCfg == nil {
		t.Fatal("example-droplet spec not found in parsed specs")
	}
	ud, _ := dropletCfg["user_data"].(string)
	if !strings.Contains(ud, "${POSTGRES_PASSWORD}") {
		t.Errorf("user_data should contain literal ${POSTGRES_PASSWORD}, got:\n%s", ud)
	}
	if strings.Contains(ud, "deadbeef") {
		t.Errorf("user_data should NOT contain the resolved secret value, got:\n%s", ud)
	}
}

func TestParseInfraResourceSpecs_PreservesRequiredSecretVarsInUserData(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "actual-do-token")
	t.Setenv("IMAGE_REF", "registry.example.com/api:abc123")
	t.Setenv("AUTH_TOKEN", "would-be-resolved-secret")
	t.Setenv("DATABASE_URL", "postgres://would-be-resolved")
	t.Setenv("POSTGRES_PASSWORD", "deadbeef1234567890abcdef12345678")

	t.Setenv("EXTERNAL_API_TOKEN", "")
	specsAtPlan, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs (plan): %v", err)
	}
	hashAtPlan := desiredStateHash(specsAtPlan)

	t.Setenv("EXTERNAL_API_TOKEN", "required-secret-value")
	specsAtApply, err := parseInfraResourceSpecs("testdata/infra-with-env-var-refs.yaml")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs (apply): %v", err)
	}
	hashAtApply := desiredStateHash(specsAtApply)

	if hashAtPlan != hashAtApply {
		planJSON, _ := json.MarshalIndent(specsAtPlan, "", "  ")
		applyJSON, _ := json.MarshalIndent(specsAtApply, "", "  ")
		t.Errorf("desiredStateHash mismatch between plan and apply:\n"+
			"  plan hash:  %s\n  apply hash: %s\n\nplan specs:\n%s\n\napply specs:\n%s",
			hashAtPlan, hashAtApply, planJSON, applyJSON)
	}

	var dropletCfg map[string]any
	for _, s := range specsAtApply {
		if s.Name == "example-droplet" {
			dropletCfg = s.Config
			break
		}
	}
	if dropletCfg == nil {
		t.Fatal("example-droplet spec not found in parsed specs")
	}
	ud, _ := dropletCfg["user_data"].(string)
	if !strings.Contains(ud, "${EXTERNAL_API_TOKEN}") {
		t.Errorf("user_data should contain literal ${EXTERNAL_API_TOKEN}, got:\n%s", ud)
	}
	if strings.Contains(ud, "required-secret-value") {
		t.Errorf("user_data should NOT contain the resolved required secret value, got:\n%s", ud)
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
	if !strings.Contains(string(b), "user_data") {
		t.Errorf("fixture missing user_data block — needed for secret-gen preservation test")
	}
	if !strings.Contains(string(b), "secrets:") {
		t.Errorf("fixture missing secrets: section — needed for secret-gen preservation test")
	}
	if !strings.Contains(string(b), "secrets.requires") {
		t.Errorf("fixture missing secrets.requires module — needed for required-secret preservation test")
	}
}
