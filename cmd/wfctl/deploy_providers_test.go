package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// ── newDeployProvider ─────────────────────────────────────────────────────────

func TestNewDeployProvider_Kubernetes(t *testing.T) {
	for _, name := range []string{"kubernetes", "k8s"} {
		p, err := newDeployProvider(name)
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
		p, err := newDeployProvider(name)
		if err != nil {
			t.Fatalf("newDeployProvider(%q): unexpected error: %v", name, err)
		}
		if _, ok := p.(*dockerProvider); !ok {
			t.Fatalf("expected *dockerProvider, got %T", p)
		}
	}
}

func TestNewDeployProvider_AWSECS(t *testing.T) {
	p, err := newDeployProvider("aws-ecs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*awsECSProvider); !ok {
		t.Fatalf("expected *awsECSProvider, got %T", p)
	}
}

func TestNewDeployProvider_Unknown(t *testing.T) {
	_, err := newDeployProvider("unknown-provider")
	if err == nil {
		t.Fatal("expected error for unknown provider")
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

// ── DigitalOcean provider ─────────────────────────────────────────────────────

func TestDigitalOceanProvider_NewProvider(t *testing.T) {
	for _, name := range []string{"digitalocean", "do"} {
		p, err := newDeployProvider(name)
		if err != nil {
			t.Fatalf("newDeployProvider(%q): unexpected error: %v", name, err)
		}
		if _, ok := p.(*digitaloceanProvider); !ok {
			t.Fatalf("expected *digitaloceanProvider, got %T", p)
		}
	}
}

func TestDigitalOceanProvider_MissingToken(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "")
	p := &digitaloceanProvider{}
	err := p.Deploy(context.Background(), DeployConfig{
		AppName:  "myapp",
		ImageTag: "registry.digitalocean.com/myreg/myapp:sha",
		Env:      &config.CIDeployEnvironment{Region: "nyc3"},
	})
	if err == nil {
		t.Fatal("expected error when DIGITALOCEAN_TOKEN is unset")
	}
	if !strings.Contains(err.Error(), "DIGITALOCEAN_TOKEN") {
		t.Errorf("expected DIGITALOCEAN_TOKEN in error, got: %v", err)
	}
}

func TestDigitalOceanProvider_Deploy_CreatesNewApp(t *testing.T) {
	var postBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/apps"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(doListAppsResponse{Apps: []doApp{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/apps":
			postBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(doAppResponse{App: doApp{ID: "new-app-1"}})
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	p := &digitaloceanProvider{baseURL: srv.URL}
	cfg := DeployConfig{
		AppName:  "myapp",
		ImageTag: "registry.digitalocean.com/myreg/myapp:abc123",
		Env:      &config.CIDeployEnvironment{Region: "nyc3"},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(string(postBody), "myapp") {
		t.Errorf("expected app name in POST body, got: %s", postBody)
	}
	if p.appID != "new-app-1" {
		t.Errorf("expected appID 'new-app-1', got %q", p.appID)
	}
}

func TestDigitalOceanProvider_Deploy_UpdatesExistingApp(t *testing.T) {
	var putPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/apps"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(doListAppsResponse{Apps: []doApp{
				{ID: "existing-1", Spec: doAppSpec{Name: "myapp"}},
			}})
		case r.Method == http.MethodPut:
			putPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(doAppResponse{App: doApp{ID: "existing-1"}})
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	p := &digitaloceanProvider{baseURL: srv.URL}
	cfg := DeployConfig{
		AppName:  "myapp",
		ImageTag: "registry.digitalocean.com/myreg/myapp:newsha",
		Env:      &config.CIDeployEnvironment{Region: "nyc3"},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if putPath != "/v2/apps/existing-1" {
		t.Errorf("expected PUT /v2/apps/existing-1, got %s", putPath)
	}
}

func TestDigitalOceanProvider_HealthCheck(t *testing.T) {
	// Mock health check endpoint
	hcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer hcSrv.Close()

	// Mock DO API returning the live URL
	doSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doAppResponse{App: doApp{
			ID:      "app-hc",
			LiveURL: hcSrv.URL,
		}})
	}))
	defer doSrv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	p := &digitaloceanProvider{baseURL: doSrv.URL, appID: "app-hc"}
	cfg := DeployConfig{
		AppName: "myapp",
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{
				Path:    "/",
				Timeout: "5s",
			},
		},
	}
	if err := p.HealthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}
