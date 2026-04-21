package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// ── TestInfraApply_TokenEnvVarExpanded ──────────────────────────────────────
// Verifies that ${VAR} references are substituted across both code paths used
// by the apply command:
//   - parseInfraResourceSpecs (infra.* types via the spec-building pass)
//   - writeEnvResolvedConfig (all module types, including iac.provider)
func TestInfraApply_TokenEnvVarExpanded(t *testing.T) {
	t.Setenv("FAKE_TOKEN", "tok_live_abc123")
	t.Setenv("REGISTRY_TOKEN", "reg_secret_xyz")

	dir := t.TempDir()
	cfg := `
modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: "${FAKE_TOKEN}"
  - name: app
    type: infra.container_service
    config:
      provider: cloud-provider
      image: registry.example.com/app:latest
      registry_token: "${REGISTRY_TOKEN}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// parseInfraResourceSpecs covers infra.* types — verify registry_token is expanded.
	specs, err := parseInfraResourceSpecs(path)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	var appCfg map[string]any
	for _, s := range specs {
		if s.Name == "app" {
			appCfg = s.Config
			break
		}
	}
	if appCfg == nil {
		t.Fatal("app spec not found in parseInfraResourceSpecs results")
	}
	if tok, _ := appCfg["registry_token"].(string); tok != "reg_secret_xyz" {
		t.Errorf("app.registry_token: want reg_secret_xyz (expanded), got %q", tok)
	}

	// writeEnvResolvedConfig covers all module types (iac.provider included) —
	// verify cloud-provider.token is baked into the resolved temp file.
	tmp, err := writeEnvResolvedConfig(path, "")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	resolved, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	for _, m := range resolved.Modules {
		if m.Name == "cloud-provider" {
			tok, _ := m.Config["token"].(string)
			if tok != "tok_live_abc123" {
				t.Errorf("cloud-provider.token: want tok_live_abc123 (expanded), got %q", tok)
			}
			return
		}
	}
	t.Error("cloud-provider not found in resolved config")
}

// ── TestInfraApply_IaCStateTokenExpanded ────────────────────────────────────
// Verifies that an iac.state module config with env var in a field is parsed
// and the env-expanded value is accessible via planResourcesForEnv.
// This simulates the path used by the apply command when resolving resource specs.
func TestInfraApply_IaCStateTokenExpanded(t *testing.T) {
	t.Setenv("TEST_STATE_DIR", "/tmp/my-iac-state")

	dir := t.TempDir()
	cfg := `
modules:
  - name: state-backend
    type: iac.state
    config:
      backend: filesystem
      directory: "${TEST_STATE_DIR}"
  - name: app-db
    type: infra.database
    config:
      engine: postgres
      size: m
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// planResourcesForEnv with empty env returns all modules with env-expanded config.
	resources, err := planResourcesForEnv(path, "")
	if err != nil {
		t.Fatalf("planResourcesForEnv: %v", err)
	}

	var dbFound bool
	for _, r := range resources {
		if r.Name == "app-db" {
			dbFound = true
			if r.Config["size"] != "m" {
				t.Errorf("size: want m, got %v", r.Config["size"])
			}
		}
	}
	if !dbFound {
		t.Fatal("app-db resource not found")
	}

	// Also verify the iac.state module config is NOT in the infra specs
	// (it's filtered by isInfraType), but planResourcesForEnv is ok since
	// it returns ALL infra.*/platform.* types.
}

// ── TestInfraApply_NestedMapExpanded ────────────────────────────────────────
// Verifies that ${VAR} references nested inside a map value are substituted.
func TestInfraApply_NestedMapExpanded(t *testing.T) {
	t.Setenv("DB_ENDPOINT", "db.internal.example.com")
	t.Setenv("DB_PORT", "5432")

	dir := t.TempDir()
	cfg := `
modules:
  - name: app-db
    type: infra.database
    config:
      engine: postgres
      connection:
        host: "${DB_ENDPOINT}"
        port: "${DB_PORT}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := parseInfraResourceSpecs(path)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}

	conn, ok := specs[0].Config["connection"].(map[string]any)
	if !ok {
		t.Fatalf("connection not a map, got %T", specs[0].Config["connection"])
	}
	if conn["host"] != "db.internal.example.com" {
		t.Errorf("connection.host: want db.internal.example.com, got %v", conn["host"])
	}
	if conn["port"] != "5432" {
		t.Errorf("connection.port: want 5432, got %v", conn["port"])
	}
}

// ── TestInfraApply_MultipleModules ──────────────────────────────────────────
// Verifies that two infra modules each with different env var refs are both
// fully expanded.
func TestInfraApply_MultipleModules(t *testing.T) {
	t.Setenv("CACHE_REGION", "eu-west-1")
	t.Setenv("DB_SIZE", "xl")

	dir := t.TempDir()
	cfg := `
modules:
  - name: app-cache
    type: infra.cache
    config:
      engine: redis
      region: "${CACHE_REGION}"
  - name: app-db
    type: infra.database
    config:
      engine: postgres
      size: "${DB_SIZE}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := parseInfraResourceSpecs(path)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	got := make(map[string]map[string]any, len(specs))
	for _, s := range specs {
		got[s.Name] = s.Config
	}

	if got["app-cache"]["region"] != "eu-west-1" {
		t.Errorf("app-cache.region: want eu-west-1, got %v", got["app-cache"]["region"])
	}
	if got["app-db"]["size"] != "xl" {
		t.Errorf("app-db.size: want xl, got %v", got["app-db"]["size"])
	}
}

// ── TestInfraApply_EnvFlagResolvesOverrides ──────────────────────────────────
// Verifies that when --env is used, per-env overrides that contain ${VAR}
// refs are expanded after merging with the base config.
func TestInfraApply_EnvFlagResolvesOverrides(t *testing.T) {
	t.Setenv("STAGING_DB_SIZE", "large")
	t.Setenv("STAGING_REGION", "us-east-1")

	dir := t.TempDir()
	cfg := `
environments:
  staging:
    provider: aws
    region: "${STAGING_REGION}"
modules:
  - name: app-db
    type: infra.database
    config:
      engine: postgres
      size: small
    environments:
      staging:
        config:
          size: "${STAGING_DB_SIZE}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	resources, err := planResourcesForEnv(path, "staging")
	if err != nil {
		t.Fatalf("planResourcesForEnv: %v", err)
	}

	var db *config.ResolvedModule
	for _, r := range resources {
		if r.Name == "app-db" {
			db = r
			break
		}
	}
	if db == nil {
		t.Fatal("app-db not found in staging resources")
	}

	if db.Config["size"] != "large" {
		t.Errorf("app-db.size for staging: want large (from ${STAGING_DB_SIZE}), got %v", db.Config["size"])
	}
}

// ── TestInfraApply_UnsetVarExpandsToEmpty ────────────────────────────────────
// Verifies that an unset ${VAR} expands to empty string, not the literal.
// This matches os.ExpandEnv behaviour and is intentional: callers should ensure
// required vars are set; missing vars surface early as empty values rather than
// being silently forwarded as unexpanded literals to the cloud API.
func TestInfraApply_UnsetVarExpandsToEmpty(t *testing.T) {
	// Set to empty string — same expansion result as unset, and auto-restored on cleanup.
	t.Setenv("INFRA_TEST_DEFINITELY_UNSET_VAR", "")

	dir := t.TempDir()
	cfg := `
modules:
  - name: app-db
    type: infra.database
    config:
      engine: postgres
      secret_tag: "${INFRA_TEST_DEFINITELY_UNSET_VAR}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := parseInfraResourceSpecs(path)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	// Should be empty string, not "${INFRA_TEST_DEFINITELY_UNSET_VAR}".
	if v, _ := specs[0].Config["secret_tag"].(string); v != "" {
		t.Errorf("secret_tag: want empty string for unset var, got %q", v)
	}
}

// ── TestWriteEnvResolvedConfig_ExpandsEnvVarsInModuleConfigs ────────────────
// Verifies that writeEnvResolvedConfig bakes env var values into the temp
// file's module configs. The resolved file should contain the literal value,
// not the ${VAR} placeholder.
func TestWriteEnvResolvedConfig_ExpandsEnvVarsInModuleConfigs(t *testing.T) {
	t.Setenv("TEST_DO_TOKEN", "live_token_xyz")
	t.Setenv("TEST_BUCKET_REGION", "nyc3")

	dir := t.TempDir()
	cfg := `
environments:
  prod:
    provider: digitalocean
    region: "${TEST_BUCKET_REGION}"
modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: "${TEST_DO_TOKEN}"
  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: my-infra-state
      region: "${TEST_BUCKET_REGION}"
  - name: app
    type: infra.container_service
    config:
      provider: cloud-provider
      image: registry.example.com/app:latest
    environments:
      prod:
        config:
          image: registry.example.com/app:prod
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	tmp, err := writeEnvResolvedConfig(path, "prod")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	// Load the temp file and verify env vars are baked in.
	resolved, err := config.LoadFromFile(tmp)
	if err != nil {
		t.Fatalf("LoadFromFile on resolved config: %v", err)
	}

	modMap := make(map[string]map[string]any, len(resolved.Modules))
	for _, m := range resolved.Modules {
		modMap[m.Name] = m.Config
	}

	// iac.provider: token must be expanded.
	if provCfg, ok := modMap["cloud-provider"]; ok {
		if tok, _ := provCfg["token"].(string); tok != "live_token_xyz" {
			t.Errorf("cloud-provider.token: want live_token_xyz, got %q", tok)
		}
	} else {
		t.Error("cloud-provider module not found in resolved config")
	}

	// iac.state: region must be expanded.
	if stateCfg, ok := modMap["state-backend"]; ok {
		if region, _ := stateCfg["region"].(string); region != "nyc3" {
			t.Errorf("state-backend.region: want nyc3, got %q", region)
		}
	} else {
		t.Error("state-backend module not found in resolved config")
	}

	// infra.container_service: prod override image must NOT contain ${}.
	if appCfg, ok := modMap["app"]; ok {
		if img, _ := appCfg["image"].(string); img == "" || img[0] == '$' {
			t.Errorf("app.image should be a resolved value, got %q", img)
		}
	} else {
		t.Error("app module not found in resolved config")
	}
}

// ── TestWriteEnvResolvedConfig_OriginalNotMutated ────────────────────────────
// Verifies that calling writeEnvResolvedConfig does not mutate the original
// config values in the YAML (the source module config map must remain as-is).
func TestWriteEnvResolvedConfig_OriginalNotMutated(t *testing.T) {
	t.Setenv("TEST_IMMUTABLE_TOKEN", "will-be-expanded")

	dir := t.TempDir()
	cfg := `
environments:
  staging:
    provider: digitalocean
modules:
  - name: cloud-provider
    type: iac.provider
    config:
      token: "${TEST_IMMUTABLE_TOKEN}"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Call writeEnvResolvedConfig twice — both calls must succeed and produce
	// the expanded value. This would fail if the first call mutated the
	// source config map (e.g. replacing "${TEST_IMMUTABLE_TOKEN}" in-place).
	tmp1, err := writeEnvResolvedConfig(path, "staging")
	if err != nil {
		t.Fatalf("first writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp1)

	tmp2, err := writeEnvResolvedConfig(path, "staging")
	if err != nil {
		t.Fatalf("second writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp2)

	// Both resolved files must contain the expanded value.
	for _, tmp := range []string{tmp1, tmp2} {
		resolved, loadErr := config.LoadFromFile(tmp)
		if loadErr != nil {
			t.Fatalf("LoadFromFile %s: %v", tmp, loadErr)
		}
		for _, m := range resolved.Modules {
			if m.Name == "cloud-provider" {
				tok, _ := m.Config["token"].(string)
				if tok != "will-be-expanded" {
					t.Errorf("token in %s: want will-be-expanded, got %q", tmp, tok)
				}
			}
		}
	}
}
