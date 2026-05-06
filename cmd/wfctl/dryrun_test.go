package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// stubProviderForDryRunTests overrides resolveIaCProvider to return a NoopProvider
// so tests don't require a real plugin binary.
func stubProviderForDryRunTests(t *testing.T) {
	t.Helper()
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return &iactest.NoopProvider{}, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })
}

func TestInfraApplyDryRun_PrintsPlanWithoutMutations(t *testing.T) {
	// Create a minimal infra config with an iac.provider and infra resource.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgContent := `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: mock
      region: nyc1

  - name: my-vpc
    type: infra.vpc
    config:
      provider: do-provider
      name: test-vpc
      region: nyc1
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	stubProviderForDryRunTests(t)
	platform.SetDiffCacheForTest(t, nil)

	// Capture stdout.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraApply([]string{"--dry-run", "--config", cfgPath})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	output := buf.String()

	// Verify it includes plan information.
	if !strings.Contains(output, "Dry Run") {
		t.Error("expected 'Dry Run' header in output")
	}
	if !strings.Contains(output, "infra.yaml") || !strings.Contains(output, "infra") {
		t.Errorf("expected config file name in output, got: %s", output)
	}
	if !strings.Contains(output, "No changes were applied") {
		t.Error("expected 'No changes were applied' message")
	}
}

func TestInfraApplyDryRun_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgContent := `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: mock
      region: nyc1

  - name: my-db
    type: infra.database
    config:
      provider: do-provider
      name: test-db
      engine: pg
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	stubProviderForDryRunTests(t)
	platform.SetDiffCacheForTest(t, nil)

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraApply([]string{"--dry-run", "--format", "json", "--config", cfgPath})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run json should not error: %v", err)
	}

	var result DryRunApplyPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output should be valid JSON: %v\nGot: %s", err, buf.String())
	}
	if result.Command != "infra apply" {
		t.Errorf("expected command 'infra apply', got %q", result.Command)
	}
	if result.Config != cfgPath {
		t.Errorf("expected config %q, got %q", cfgPath, result.Config)
	}
}

func TestInfraApplyDryRun_SharesPlanLogicWithApply(t *testing.T) {
	// This test verifies that dry-run uses the same planning functions
	// (parseInfraResourceSpecsForEnv + computePlanForInfraSpecs) as the
	// real apply path, by checking that env-resolved resource names
	// (like 'bmw-staging') are resolved in the plan.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgContent := `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: mock

  - name: bmw-staging
    type: infra.vpc
    config:
      provider: do-provider
      name: bmw-staging
      region: nyc1
    environments:
      staging:
        region: fra1

environments:
  staging:
    provider: digitalocean
    region: fra1
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	stubProviderForDryRunTests(t)
	platform.SetDiffCacheForTest(t, nil)

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraApply([]string{"--dry-run", "--env", "staging", "--config", cfgPath})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run with env should not error: %v", err)
	}

	output := buf.String()
	// The module name 'bmw-staging' should appear in the plan output
	// (it's the resource name in the plan table).
	if !strings.Contains(output, "bmw-staging") {
		t.Errorf("expected 'bmw-staging' in dry-run output — dry-run must use the same planning logic as apply, got: %s", output)
	}
}

func TestCIRunDeployDryRun_PrintsDeployPlan(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	cfgContent := `
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
        strategy: rolling
        preDeploy:
          - migrate-db
        healthCheck:
          path: /health
          timeout: 60s
secrets:
  entries:
    - name: DATABASE_URL
      store: vault
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("IMAGE_TAG", "myapp:abc1234")

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run deploy should not error: %v", err)
	}
	output := buf.String()

	// Verify key information is shown.
	for _, expected := range []string{
		"Dry Run",
		"staging",
		"do-app-platform",
		"rolling",
		"myapp:abc1234",
		"migrate-db",
		"/health",
		"DATABASE_URL",
		"No deployment was executed",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in dry-run output, got:\n%s", expected, output)
		}
	}
}

func TestCIRunDeployDryRun_JSONFormat(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	cfgContent := `
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: aws-ecs
        strategy: blue-green
        cluster: bmw-staging
        healthCheck:
          path: /api/health
          timeout: 30s
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("IMAGE_TAG", "api:v2.0.0")

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run", "--format", "json"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run json deploy should not error: %v", err)
	}

	var result DryRunDeployPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output should be valid JSON: %v\nGot: %s", err, buf.String())
	}

	if result.Environment != "staging" {
		t.Errorf("expected environment 'staging', got %q", result.Environment)
	}
	if result.Provider != "aws-ecs" {
		t.Errorf("expected provider 'aws-ecs', got %q", result.Provider)
	}
	if result.Strategy != "blue-green" {
		t.Errorf("expected strategy 'blue-green', got %q", result.Strategy)
	}
	if result.DeployTarget != "bmw-staging" {
		t.Errorf("expected deploy target 'bmw-staging', got %q", result.DeployTarget)
	}
	if result.ImageRef != "api:v2.0.0" {
		t.Errorf("expected image ref 'api:v2.0.0', got %q", result.ImageRef)
	}
	if result.HealthCheck == nil {
		t.Fatal("expected health check in output")
	}
	if result.HealthCheck.Path != "/api/health" {
		t.Errorf("expected health check path '/api/health', got %q", result.HealthCheck.Path)
	}
}

func TestCIRunDeployDryRun_NeverPrintsSecretValues(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	cfgContent := `
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
secrets:
  entries:
    - name: SECRET_API_KEY
      store: env
    - name: DB_PASSWORD
      store: vault
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set secret values in environment — they must NOT appear in output.
	t.Setenv("SECRET_API_KEY", "super-secret-key-12345")
	t.Setenv("DB_PASSWORD", "hunter2")

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	output := buf.String()

	// Secret VALUES must never appear.
	if strings.Contains(output, "super-secret-key-12345") {
		t.Error("secret value leaked in dry-run output")
	}
	if strings.Contains(output, "hunter2") {
		t.Error("secret value leaked in dry-run output")
	}

	// Secret KEYS should appear.
	if !strings.Contains(output, "SECRET_API_KEY") {
		t.Error("expected secret key 'SECRET_API_KEY' in output")
	}
	if !strings.Contains(output, "DB_PASSWORD") {
		t.Error("expected secret key 'DB_PASSWORD' in output")
	}
}

// TestDryRunSharesPlanningLogic proves that the dry-run deploy path
// resolves the same environment names, provider selection, and target
// resource naming as the real deploy path.
func TestDryRunSharesPlanningLogic(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {
				Provider:  "do-app-platform",
				Cluster:   "bmw-staging",
				Strategy:  "rolling",
				PreDeploy: []string{"run-migrations"},
				HealthCheck: &config.CIHealthCheck{
					Path:    "/health",
					Timeout: "60s",
				},
			},
		},
	}

	// The dry-run path uses the same env lookup and config resolution
	// that runDeployPhaseWithConfig uses.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDeployPhaseDryRun(deploy, "staging", nil, nil, "json", "")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result DryRunDeployPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// These are the same values that runDeployPhaseWithConfig would resolve.
	// When wfCfg is nil (no modules), deploy target falls back to env.Cluster.
	if result.DeployTarget != "bmw-staging" {
		t.Errorf("expected deploy target 'bmw-staging', got %q", result.DeployTarget)
	}
	if result.Provider != "do-app-platform" {
		t.Errorf("expected provider 'do-app-platform', got %q", result.Provider)
	}
	if result.Strategy != "rolling" {
		t.Errorf("expected strategy 'rolling', got %q", result.Strategy)
	}
	if len(result.PreDeploy) != 1 || result.PreDeploy[0] != "run-migrations" {
		t.Errorf("expected pre-deploy [run-migrations], got %v", result.PreDeploy)
	}
}

func TestDryRunInvalidFormat_InfraApply(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: p
    type: iac.provider
    config:
      provider: mock
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProviderForDryRunTests(t)
	platform.SetDiffCacheForTest(t, nil)

	err := runInfraApply([]string{"--dry-run", "--format", "jsno", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), "jsno") {
		t.Errorf("expected error to mention the bad value, got: %v", err)
	}
}

func TestDryRunInvalidFormat_CIDeploy(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run", "--format", "jsno"})
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), "jsno") {
		t.Errorf("expected error to mention the bad value, got: %v", err)
	}
}

func TestCIRunDeployDryRun_SecretStoreEnvOverride(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	cfgContent := `
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
secrets:
  defaultStore: github
  entries:
    - name: API_KEY
    - name: DB_PASS
      store: vault
environments:
  staging:
    secretsStoreOverride: doppler
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run", "--format", "json"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}

	var result DryRunDeployPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, buf.String())
	}

	// API_KEY: no per-secret store → env secretsStoreOverride = "doppler"
	// DB_PASS: per-secret store = "vault" (highest priority)
	stores := map[string]string{}
	for _, s := range result.Secrets {
		stores[s.Key] = s.Store
	}
	if stores["API_KEY"] != "doppler" {
		t.Errorf("expected API_KEY store 'doppler' (env override), got %q", stores["API_KEY"])
	}
	if stores["DB_PASS"] != "vault" {
		t.Errorf("expected DB_PASS store 'vault' (per-secret), got %q", stores["DB_PASS"])
	}
}

func TestDeployDryRun_ImageFromModuleConfig(t *testing.T) {
	// When IMAGE_TAG is unset, the dry-run should show the image from the
	// module config — matching what pluginDeployProvider.Deploy preserves.
	t.Setenv("IMAGE_TAG", "")

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name: "my-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
					"image":    "registry.example.com/my-app:v1.2.3",
				},
			},
		},
	}

	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "digitalocean"},
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDeployPhaseDryRun(deploy, "staging", wfCfg, nil, "json", "")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result DryRunDeployPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, buf.String())
	}

	if result.ImageRef != "registry.example.com/my-app:v1.2.3" {
		t.Errorf("expected image from module config, got %q", result.ImageRef)
	}
	if result.ImageTagSource != "module config image field" {
		t.Errorf("expected image tag source 'module config image field', got %q", result.ImageTagSource)
	}
}

func TestDeployDryRun_EnvResolvedModuleName(t *testing.T) {
	// Verify that the env-resolved infra module name is used as the deploy target,
	// matching the behavior of newPluginDeployProvider.
	t.Setenv("IMAGE_TAG", "api:latest")

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name:   "bmw-app",
				Type:   "infra.container_service",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{"name": "bmw-staging"},
					},
				},
			},
		},
	}

	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "digitalocean"},
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDeployPhaseDryRun(deploy, "staging", wfCfg, nil, "json", "")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result DryRunDeployPlan
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, buf.String())
	}

	// The env-resolved name comes from ResolveForEnv("staging") which lifts
	// the "name" field from environments.staging.config into resolved.Name
	// (for infra.* types). This matches exactly what newPluginDeployProvider
	// uses as resourceName — the env-overridden identity "bmw-staging".
	if result.DeployTarget != "bmw-staging" {
		t.Errorf("expected env-resolved deploy target 'bmw-staging', got %q", result.DeployTarget)
	}
}

func TestDryRunFollowUpCommand_IncludesConfigPath(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "myconfig.yaml")
	cfgContent := `
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging", "--dry-run"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	output := buf.String()
	// The suggested follow-up command must include the config file path.
	if !strings.Contains(output, "--config") || !strings.Contains(output, "myconfig.yaml") {
		t.Errorf("follow-up command should include --config <path>, got:\n%s", output)
	}
}
