package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// ── newDeployProvider ─────────────────────────────────────────────────────────

func TestNewDeployProvider_Kubernetes(t *testing.T) {
	for _, name := range []string{"kubernetes", "k8s"} {
		p, err := newDeployProvider(name, nil, "")
		if err != nil {
			t.Fatalf("newDeployProvider(%q): unexpected error: %v", name, err)
		}
		if _, ok := p.(*kubernetesProvider); !ok {
			t.Fatalf("expected *kubernetesProvider, got %T", p)
		}
	}
}

func TestNewDeployProvider_Docker(t *testing.T) {
	for _, name := range []string{"docker", "docker-compose"} {
		p, err := newDeployProvider(name, nil, "")
		if err != nil {
			t.Fatalf("newDeployProvider(%q): unexpected error: %v", name, err)
		}
		if _, ok := p.(*dockerProvider); !ok {
			t.Fatalf("expected *dockerProvider, got %T", p)
		}
	}
}

func TestNewDeployProvider_AWSECS(t *testing.T) {
	p, err := newDeployProvider("aws-ecs", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*awsECSProvider); !ok {
		t.Fatalf("expected *awsECSProvider, got %T", p)
	}
}

func TestNewDeployProvider_Unknown(t *testing.T) {
	_, err := newDeployProvider("unknown-provider", nil, "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// ── newPluginDeployProvider env-resolution ─────────────────────────────────────

// TestNewPluginDeployProvider_MergesEnvironmentConfig verifies that when envName
// is set, the per-env config overlay is merged into the top-level config before
// the provider is constructed, so environment-specific fields (like image) are
// visible in resourceCfg.
func TestNewPluginDeployProvider_MergesEnvironmentConfig(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "do-provider"},
			},
			{
				Name: "bmw-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider":  "do-provider",
					"http_port": 8080,
				},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{
							"image": "foo:bar",
						},
					},
				},
			},
		},
	}

	p, err := newDeployProvider("do-provider", wfCfg, "staging")
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}
	pdp, ok := p.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", p)
	}
	got, _ := pdp.resourceCfg["image"].(string)
	if got != "foo:bar" {
		t.Errorf("resourceCfg[image]: want %q, got %q", "foo:bar", got)
	}
}

// TestNewPluginDeployProvider_EnvOverridesTopLevel verifies that a per-env
// config value overrides the corresponding top-level value.
func TestNewPluginDeployProvider_EnvOverridesTopLevel(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "do-provider"},
			},
			{
				Name: "bmw-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
					"image":    "old:v1",
				},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{
							"image": "new:v2",
						},
					},
				},
			},
		},
	}

	p, err := newDeployProvider("do-provider", wfCfg, "staging")
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}
	pdp := p.(*pluginDeployProvider)
	got, _ := pdp.resourceCfg["image"].(string)
	if got != "new:v2" {
		t.Errorf("resourceCfg[image]: want %q, got %q", "new:v2", got)
	}
}

// TestNewPluginDeployProvider_NoEnv verifies that when envName is empty the
// top-level module config is used unchanged.
func TestNewPluginDeployProvider_NoEnv(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "do-provider"},
			},
			{
				Name: "bmw-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
					"image":    "top-level:tag",
				},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{
							"image": "staging:tag",
						},
					},
				},
			},
		},
	}

	p, err := newDeployProvider("do-provider", wfCfg, "")
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}
	pdp := p.(*pluginDeployProvider)
	got, _ := pdp.resourceCfg["image"].(string)
	if got != "top-level:tag" {
		t.Errorf("resourceCfg[image]: want %q, got %q", "top-level:tag", got)
	}
}

// TestNewPluginDeployProvider_EnvSubstitutionAfterMerge verifies that
// ExpandEnvInMap runs after the env-config merge, so ${VAR} placeholders in
// per-env config fields are expanded using the OS environment.
func TestNewPluginDeployProvider_EnvSubstitutionAfterMerge(t *testing.T) {
	t.Setenv("DEPLOY_RESOLVE_TEST_IMAGE_TAG", "registry/org/app:abc123")

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "do-provider"},
			},
			{
				Name: "bmw-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider": "do-provider",
				},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{
							"image": "${DEPLOY_RESOLVE_TEST_IMAGE_TAG}",
						},
					},
				},
			},
		},
	}

	p, err := newDeployProvider("do-provider", wfCfg, "staging")
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}
	pdp := p.(*pluginDeployProvider)
	got, _ := pdp.resourceCfg["image"].(string)
	want := "registry/org/app:abc123"
	if got != want {
		t.Errorf("resourceCfg[image]: want %q, got %q", want, got)
	}
}

// ── generateK8sManifests ──────────────────────────────────────────────────────

func TestGenerateK8sManifests_Single(t *testing.T) {
	cfg := DeployConfig{
		EnvName: "staging",
		Env: &config.CIDeployEnvironment{
			Provider:  "kubernetes",
			Namespace: "myns",
			Strategy:  "rolling",
		},
		AppName:  "myapp",
		ImageTag: "myapp:abc123",
	}

	manifests, err := generateK8sManifests(cfg)
	if err != nil {
		t.Fatalf("generateK8sManifests: %v", err)
	}
	if !strings.Contains(manifests, "name: myapp") {
		t.Errorf("expected deployment name 'myapp', got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "namespace: myns") {
		t.Errorf("expected namespace 'myns', got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "image: myapp:abc123") {
		t.Errorf("expected image 'myapp:abc123', got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "RollingUpdate") {
		t.Errorf("expected RollingUpdate strategy, got:\n%s", manifests)
	}
}

func TestGenerateK8sManifests_MultiService(t *testing.T) {
	cfg := DeployConfig{
		EnvName: "staging",
		Env: &config.CIDeployEnvironment{
			Provider:  "kubernetes",
			Namespace: "prod",
			Strategy:  "rolling",
		},
		Services: map[string]*config.ServiceConfig{
			"api": {
				Binary: "./cmd/api",
				Scaling: &config.ScalingConfig{
					Replicas: 3,
				},
				Expose: []config.ExposeConfig{
					{Port: 8080, Protocol: "HTTP"},
				},
			},
			"worker": {
				Binary: "./cmd/worker",
				Scaling: &config.ScalingConfig{
					Replicas: 2,
				},
			},
		},
	}

	manifests, err := generateK8sManifests(cfg)
	if err != nil {
		t.Fatalf("generateK8sManifests: %v", err)
	}
	if !strings.Contains(manifests, "name: api") {
		t.Errorf("expected 'api' deployment, got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "name: worker") {
		t.Errorf("expected 'worker' deployment, got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "replicas: 3") {
		t.Errorf("expected replicas: 3 for api, got:\n%s", manifests)
	}
	if !strings.Contains(manifests, "replicas: 2") {
		t.Errorf("expected replicas: 2 for worker, got:\n%s", manifests)
	}
}

func TestGenerateK8sManifests_WithSecrets(t *testing.T) {
	cfg := DeployConfig{
		EnvName: "prod",
		Env: &config.CIDeployEnvironment{
			Provider:  "kubernetes",
			Namespace: "default",
		},
		AppName: "myapp",
		Secrets: map[string]string{
			"DB_PASSWORD": "secret123",
		},
	}
	manifests, err := generateK8sManifests(cfg)
	if err != nil {
		t.Fatalf("generateK8sManifests: %v", err)
	}
	if !strings.Contains(manifests, "DB_PASSWORD") {
		t.Errorf("expected DB_PASSWORD in env vars, got:\n%s", manifests)
	}
}

// ── generateDockerCompose ─────────────────────────────────────────────────────

func TestGenerateDockerCompose_Single(t *testing.T) {
	cfg := DeployConfig{
		EnvName:  "local",
		Env:      &config.CIDeployEnvironment{Provider: "docker"},
		AppName:  "myapp",
		ImageTag: "myapp:v1",
	}
	compose, err := generateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("generateDockerCompose: %v", err)
	}
	if !strings.Contains(compose, "image: myapp:v1") {
		t.Errorf("expected image 'myapp:v1', got:\n%s", compose)
	}
	if !strings.Contains(compose, "myapp:") {
		t.Errorf("expected service name 'myapp', got:\n%s", compose)
	}
}

func TestGenerateDockerCompose_MultiService(t *testing.T) {
	cfg := DeployConfig{
		EnvName: "local",
		Env:     &config.CIDeployEnvironment{Provider: "docker"},
		Services: map[string]*config.ServiceConfig{
			"frontend": {
				Expose: []config.ExposeConfig{{Port: 3000, Protocol: "HTTP"}},
			},
			"backend": {
				Expose: []config.ExposeConfig{{Port: 8080, Protocol: "HTTP"}},
			},
		},
	}
	compose, err := generateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("generateDockerCompose: %v", err)
	}
	if !strings.Contains(compose, "frontend:") {
		t.Errorf("expected 'frontend' service, got:\n%s", compose)
	}
	if !strings.Contains(compose, "backend:") {
		t.Errorf("expected 'backend' service, got:\n%s", compose)
	}
}

// ── k8sStrategy ──────────────────────────────────────────────────────────────

func TestK8sStrategy(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"rolling", "RollingUpdate"},
		{"blue-green", "RollingUpdate"},
		{"canary", "RollingUpdate"},
		{"recreate", "Recreate"},
		{"unknown", "RollingUpdate"},
		{"", "RollingUpdate"},
	}
	for _, tc := range cases {
		got := k8sStrategy(tc.in)
		if got != tc.out {
			t.Errorf("k8sStrategy(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

// ── injectSecrets ─────────────────────────────────────────────────────────────

func TestInjectSecrets_Nil(t *testing.T) {
	secrets, err := injectSecrets(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("injectSecrets(nil): unexpected error: %v", err)
	}
	if secrets != nil {
		t.Errorf("expected nil secrets map, got: %v", secrets)
	}
}

func TestInjectSecrets_EnvProvider(t *testing.T) {
	t.Setenv("TEST_SECRET_KEY", "supersecret")

	wfCfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Provider: "env",
			Entries: []config.SecretEntry{
				{Name: "TEST_SECRET_KEY"},
			},
		},
	}
	secrets, err := injectSecrets(context.Background(), wfCfg, "")
	if err != nil {
		t.Fatalf("injectSecrets: %v", err)
	}
	if secrets["TEST_SECRET_KEY"] != "supersecret" {
		t.Errorf("expected 'supersecret', got %q", secrets["TEST_SECRET_KEY"])
	}
}

// ── runDeployPhaseWithConfig ──────────────────────────────────────────────────

func TestRunDeployPhaseWithConfig_NilDeploy(t *testing.T) {
	err := runDeployPhaseWithConfig(nil, "staging", nil, nil, false)
	if err == nil {
		t.Fatal("expected error for nil deploy config")
	}
}

func TestRunDeployPhaseWithConfig_RequireApproval(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"prod": {Provider: "kubernetes", RequireApproval: true},
		},
	}
	if err := runDeployPhaseWithConfig(deploy, "prod", nil, nil, false); err != nil {
		t.Fatalf("approval skip should not error: %v", err)
	}
}

func TestRunDeployPhaseWithConfig_UnknownEnv(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "kubernetes"},
		},
	}
	err := runDeployPhaseWithConfig(deploy, "production", nil, nil, false)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("expected 'production' in error, got: %v", err)
	}
}

func TestRunDeployPhaseWithConfig_AWSECS(t *testing.T) {
	// aws-ecs provider is a stub that always succeeds without network calls.
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {
				Provider: "aws-ecs",
				Region:   "us-east-1",
				Cluster:  "my-cluster",
				Strategy: "rolling",
			},
		},
	}
	if err := runDeployPhaseWithConfig(deploy, "staging", nil, nil, false); err != nil {
		t.Fatalf("aws-ecs stub deploy should not error: %v", err)
	}
}

// ── cmp helper ───────────────────────────────────────────────────────────────

func TestCmp(t *testing.T) {
	if cmp("a", "b") != "a" {
		t.Error("expected 'a'")
	}
	if cmp("", "b") != "b" {
		t.Error("expected 'b'")
	}
	if cmp("", "") != "" {
		t.Error("expected ''")
	}
}

// ── imageTagSuffix ────────────────────────────────────────────────────────────

func TestImageTagSuffix(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"myapp:v1.2.3", "v1.2.3"},
		{"registry.io/myapp:abc123", "abc123"},
		{"myapp", "myapp"},
		{"", ""},
	}
	for _, tc := range cases {
		got := imageTagSuffix(tc.in)
		if got != tc.out {
			t.Errorf("imageTagSuffix(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

// ── secretsToEnvVars ──────────────────────────────────────────────────────────

func TestSecretsToEnvVars_Empty(t *testing.T) {
	if secretsToEnvVars(nil) != nil {
		t.Error("expected nil for nil secrets")
	}
	if secretsToEnvVars(map[string]string{}) != nil {
		t.Error("expected nil for empty secrets")
	}
}

func TestSecretsToEnvVars_NonEmpty(t *testing.T) {
	result := secretsToEnvVars(map[string]string{"KEY": "val"})
	if len(result) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(result))
	}
	if result[0].Name != "KEY" || result[0].Value != "val" {
		t.Errorf("unexpected env var: %+v", result[0])
	}
}

// ── Ensure test cleanup doesn't leave temp files ──────────────────────────────

func TestDockerProvider_GeneratesAndRemovesComposeFile(t *testing.T) {
	// generateDockerCompose must produce valid YAML-ish content without error.
	cfg := DeployConfig{
		EnvName: "test",
		Env:     &config.CIDeployEnvironment{Provider: "docker"},
		AppName: "testapp",
	}
	compose, err := generateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("generateDockerCompose: %v", err)
	}
	if !strings.Contains(compose, "testapp") {
		t.Errorf("expected 'testapp' in compose output, got:\n%s", compose)
	}
	// Verify no leftover temp file exists.
	if _, err := os.Stat("docker-compose.wfctl.yml"); err == nil {
		os.Remove("docker-compose.wfctl.yml")
		t.Error("unexpected leftover docker-compose.wfctl.yml")
	}
}
