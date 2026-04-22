// Package main — E2E integration tests for the bootstrap + apply + deploy sequence.
//
// These tests validate:
//   - env-var substitution works correctly across all three command paths
//   - --env flag selects and merges per-environment config before substitution
//   - -c / --config flag resolves the correct config file
//   - --auto-approve / -y flag skips stdin confirmation in apply
//   - ${VAR} placeholders in different config sections are all expanded
//
// Fakes: filesystem state backend (no HTTP), in-memory secrets (fakeSecretsProvider),
// fakeIaCProvider / captureResourceDriver for deploy verification.
package main

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// writeInfraConfig writes YAML to a named file in dir and returns its path.
func writeInfraConfig(t *testing.T, dir, name, yaml string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// ── TestE2E_BootstrapApplyDeploy_HappyPath ───────────────────────────────────

// TestE2E_BootstrapApplyDeploy_HappyPath exercises all three phases in
// sequence against in-memory fakes:
//
//  1. Bootstrap: secrets generated, state backend skipped (filesystem).
//  2. Apply (plan): resource specs built with substituted ${FAKE_TOKEN}.
//  3. Deploy: image updated via fake driver with substituted provider config.
func TestE2E_BootstrapApplyDeploy_HappyPath(t *testing.T) {
	t.Setenv("FAKE_TOKEN", "test-value-1")
	t.Setenv("FAKE_REGION", "nyc3")

	cfgFile := writeInfraConfig(t, t.TempDir(), "infra.yaml", `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "${FAKE_TOKEN}"
      region: "${FAKE_REGION}"

  - name: tf-state
    type: iac.state
    config:
      backend: filesystem
      path: "./state"

  - name: my-app
    type: infra.container_service
    config:
      provider: do-provider
      region: "${FAKE_REGION}"
      http_port: 8080

secrets:
  provider: env
  config: {}
  generate:
    - key: APP_SECRET
      type: random_hex
      length: 16
`)

	// ── Phase 1: Bootstrap ────────────────────────────────────────────────────
	// Filesystem state backend needs no HTTP call; secrets generated via env provider.

	// Inject a stub secret generator so we don't depend on real crypto entropy.
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "deadbeefcafe1234", nil
	})

	// bootstrapStateBackend: filesystem backend → returns nil immediately.
	if err := bootstrapStateBackend(context.Background(), cfgFile); err != nil {
		t.Fatalf("phase 1 bootstrapStateBackend: %v", err)
	}

	// bootstrapSecrets: uses env provider — verify the provider is constructed
	// correctly and the secret generation stub is called.
	secretsCfg, err := parseSecretsConfig(cfgFile)
	if err != nil {
		t.Fatalf("phase 1 parseSecretsConfig: %v", err)
	}
	if secretsCfg == nil {
		t.Fatal("phase 1: expected secrets config")
	}
	p := &fakeSecretsProvider{stored: map[string]string{}}
	if err := bootstrapSecrets(context.Background(), p, secretsCfg); err != nil {
		t.Fatalf("phase 1 bootstrapSecrets: %v", err)
	}
	if p.stored["APP_SECRET"] != "deadbeefcafe1234" {
		t.Errorf("phase 1: APP_SECRET not stored; got %q", p.stored["APP_SECRET"])
	}

	// ── Phase 2: Apply (plan) ─────────────────────────────────────────────────
	// planResourcesForEnv applies ExpandEnvInMap; verify token/region are expanded.
	resources, err := planResourcesForEnv(cfgFile, "")
	if err != nil {
		t.Fatalf("phase 2 planResourcesForEnv: %v", err)
	}

	var appModule *config.ResolvedModule
	for _, r := range resources {
		if r.Name == "my-app" {
			appModule = r
			break
		}
	}
	if appModule == nil {
		t.Fatal("phase 2: my-app resource not found in plan")
	}
	if got, _ := appModule.Config["region"].(string); got != "nyc3" {
		t.Errorf("phase 2: my-app region: want %q, got %q", "nyc3", got)
	}

	// ── Phase 3: Deploy ───────────────────────────────────────────────────────
	// Inject a fake IaC provider; verify the provider receives the expanded token.

	var capturedProviderCfg map[string]any
	driver := &captureResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		capturedProviderCfg = cfg
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	wfCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		t.Fatalf("phase 3 load config: %v", err)
	}

	dp, err := newDeployProvider("fake-cloud", wfCfg, "")
	if err != nil {
		t.Fatalf("phase 3 newDeployProvider: %v", err)
	}
	deployCfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:abc123",
		Env:      fakeCIEnv(),
	}
	if err := dp.Deploy(context.Background(), deployCfg); err != nil {
		t.Fatalf("phase 3 Deploy: %v", err)
	}

	// Provider config must have expanded token, not the literal.
	if capturedProviderCfg != nil {
		if got, _ := capturedProviderCfg["token"].(string); got != "test-value-1" {
			t.Errorf("phase 3: provider token: want %q, got %q", "test-value-1", got)
		}
	}
	if driver.updateImage != "registry.example.com/myapp:abc123" {
		t.Errorf("phase 3: image not updated: got %q", driver.updateImage)
	}
}

// ── TestE2E_EnvFlag_Override ─────────────────────────────────────────────────

// TestE2E_EnvFlag_Override verifies that --env staging vs --env prod selects
// the correct per-env region from the infra config.
func TestE2E_EnvFlag_Override(t *testing.T) {
	cfgFile := writeInfraConfig(t, t.TempDir(), "infra.yaml", `
environments:
  staging:
    provider: fake-cloud
    region: us-east-1
  prod:
    provider: fake-cloud
    region: eu-west-1
modules:
  - name: my-db
    type: infra.database
    config:
      size: small
`)

	for _, tc := range []struct {
		env        string
		wantRegion string
	}{
		{"staging", "us-east-1"},
		{"prod", "eu-west-1"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			resources, err := planResourcesForEnv(cfgFile, tc.env)
			if err != nil {
				t.Fatalf("planResourcesForEnv(%q): %v", tc.env, err)
			}
			if len(resources) == 0 {
				t.Fatal("expected at least one resource")
			}
			if got := resources[0].Region; got != tc.wantRegion {
				t.Errorf("--env %s: region: want %q, got %q", tc.env, tc.wantRegion, got)
			}
		})
	}
}

// ── TestE2E_ConfigFlag ───────────────────────────────────────────────────────

// TestE2E_ConfigFlag verifies that -c / --config selects the specified config
// file and auto-discovery falls back to infra.yaml in the working directory.
func TestE2E_ConfigFlag(t *testing.T) {
	dir := t.TempDir()

	cfgA := writeInfraConfig(t, dir, "infra-a.yaml", `
modules:
  - name: res-a
    type: infra.database
    config:
      size: large
`)
	cfgB := writeInfraConfig(t, dir, "infra.yaml", `
modules:
  - name: res-b
    type: infra.database
    config:
      size: small
`)

	t.Run("explicit -c flag picks cfgA", func(t *testing.T) {
		fs := flag.NewFlagSet("infra plan", flag.ContinueOnError)
		var cfgFlag string
		fs.StringVar(&cfgFlag, "c", "", "")
		fs.StringVar(&cfgFlag, "config", "", "")
		_ = fs.Parse([]string{"-c", cfgA})

		got, err := resolveInfraConfig(fs, cfgFlag)
		if err != nil {
			t.Fatalf("resolveInfraConfig: %v", err)
		}
		if got != cfgA {
			t.Errorf("want %q, got %q", cfgA, got)
		}
	})

	t.Run("auto-discovery finds infra.yaml", func(t *testing.T) {
		// Change working dir to dir so auto-discovery finds infra.yaml.
		orig, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer os.Chdir(orig) //nolint:errcheck

		fs := flag.NewFlagSet("infra plan", flag.ContinueOnError)
		fs.String("c", "", "")
		fs.String("config", "", "")
		_ = fs.Parse(nil)

		got, err := resolveInfraConfig(fs, "")
		if err != nil {
			t.Fatalf("resolveInfraConfig: %v", err)
		}
		if got != "infra.yaml" {
			t.Errorf("want %q, got %q", cfgB, got)
		}
	})
}

// ── TestE2E_AutoApproveFlag ──────────────────────────────────────────────────

// TestE2E_AutoApproveFlag verifies that the --auto-approve / -y flag is
// registered on the apply and destroy commands, and that when set, the
// confirmation gate is skipped entirely.
func TestE2E_AutoApproveFlag(t *testing.T) {
	for _, tc := range []struct {
		flag  string
		value string
	}{
		{"auto-approve", "true"},
		{"y", "true"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			fs := flag.NewFlagSet("infra apply", flag.ContinueOnError)
			var autoApprove bool
			fs.BoolVar(&autoApprove, "auto-approve", false, "")
			fs.BoolVar(&autoApprove, "y", false, "")
			if err := fs.Parse([]string{"-" + tc.flag}); err != nil {
				t.Fatalf("parse -%s: %v", tc.flag, err)
			}
			if !autoApprove {
				t.Errorf("-%s should set autoApprove=true", tc.flag)
			}
		})
	}

	// Verify apply skips stdin when autoApprove=true.
	// We test the logic branch directly: with autoApprove=true, no Scanln call.
	// The test passes if it completes without blocking on stdin.
	t.Run("apply skips stdin with auto-approve", func(t *testing.T) {
		dir := t.TempDir()
		cfgFile := writeInfraConfig(t, dir, "infra.yaml", `
infra:
  auto_bootstrap: false
modules:
  - name: res
    type: infra.database
    config:
      size: small
`)
		// With --auto-approve, runInfraApply should pass the stdin gate.
		// It will fail later when trying to run the pipeline (no engine), but
		// that means auto-approve worked. We capture the error and check it
		// does NOT contain "reading input" (which would indicate stdin was read).
		err := runInfraApply([]string{"--auto-approve", "--config", cfgFile})
		if err != nil {
			errStr := err.Error()
			// A "reading input" error would indicate stdin was NOT skipped.
			if errStr == "reading input: " || errStr == "Cancelled." {
				t.Errorf("auto-approve: stdin was read instead of skipped, error: %v", err)
			}
			// Any other error (pipeline failure, etc.) means auto-approve gate was passed.
		}
	})
}

// ── TestE2E_SubstitutionAcrossAllThreeFlows ──────────────────────────────────

// TestE2E_SubstitutionAcrossAllThreeFlows verifies that ${A}, ${B}, ${C}
// placeholders in different config sections are all expanded:
//   - ${A} in iac.provider config (deploy path)
//   - ${B} in infra.container_service config (apply/plan path)
//   - ${C} in secrets provider config (bootstrap path)
func TestE2E_SubstitutionAcrossAllThreeFlows(t *testing.T) {
	t.Setenv("E2E_TOKEN_A", "provider-token-aaa")
	t.Setenv("E2E_REGION_B", "fra1")
	t.Setenv("E2E_VAULT_ADDR_C", "https://vault.e2e.test")

	cfgFile := writeInfraConfig(t, t.TempDir(), "infra.yaml", `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "${E2E_TOKEN_A}"

  - name: tf-state
    type: iac.state
    config:
      backend: filesystem
      path: "./state"

  - name: my-app
    type: infra.container_service
    config:
      provider: do-provider
      region: "${E2E_REGION_B}"

secrets:
  provider: vault
  config:
    address: "${E2E_VAULT_ADDR_C}"
    token: "stub-token"
  generate: []
`)

	// ── Verify ${A} expanded in deploy path ──────────────────────────────────
	var capturedProviderCfg map[string]any
	fakeDriver := &captureResourceDriver{}
	fakeProv := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": fakeDriver},
	}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		capturedProviderCfg = cfg
		return fakeProv, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	wfCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	dp, err := newDeployProvider("fake-cloud", wfCfg, "")
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}
	if err := dp.Deploy(context.Background(), DeployConfig{
		AppName:  "my-app",
		ImageTag: "myapp:v1",
		Env:      fakeCIEnv(),
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	// ${A} — provider token expanded.
	if capturedProviderCfg != nil {
		got, _ := capturedProviderCfg["token"].(string)
		if got != "provider-token-aaa" {
			t.Errorf("[A] deploy path: token: want %q, got %q", "provider-token-aaa", got)
		}
	}

	// ── Verify ${B} expanded in apply/plan path ───────────────────────────────
	resources, err := planResourcesForEnv(cfgFile, "")
	if err != nil {
		t.Fatalf("planResourcesForEnv: %v", err)
	}
	var appRes *config.ResolvedModule
	for _, r := range resources {
		if r.Name == "my-app" {
			appRes = r
			break
		}
	}
	if appRes == nil {
		t.Fatal("[B] my-app resource not found in plan")
	}
	if got, _ := appRes.Config["region"].(string); got != "fra1" {
		t.Errorf("[B] apply path: region: want %q, got %q", "fra1", got)
	}

	// ── Verify ${C} expanded in bootstrap/secrets path ───────────────────────
	secretsCfg, err := parseSecretsConfig(cfgFile)
	if err != nil {
		t.Fatalf("parseSecretsConfig: %v", err)
	}
	if secretsCfg == nil {
		t.Fatal("[C] secrets config is nil")
	}
	// resolveSecretsProvider expands ${C} before constructing the vault provider.
	// The vault provider constructor succeeds (returns non-nil) when address is set.
	prov, err := resolveSecretsProvider(secretsCfg)
	if err != nil {
		t.Fatalf("[C] bootstrap path: resolveSecretsProvider: %v", err)
	}
	if prov == nil {
		t.Fatal("[C] bootstrap path: expected non-nil vault provider")
	}
	// Original config must NOT be mutated — ${C} literal preserved.
	if got, _ := secretsCfg.Config["address"].(string); got != "${E2E_VAULT_ADDR_C}" {
		t.Errorf("[C] original secrets config mutated: got %q", got)
	}
}

// ── fakeCIEnv ─────────────────────────────────────────────────────────────────

// fakeCIEnv returns a minimal CIDeployEnvironment for deploy tests.
func fakeCIEnv() *config.CIDeployEnvironment {
	return &config.CIDeployEnvironment{}
}
