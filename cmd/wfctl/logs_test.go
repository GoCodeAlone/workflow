package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type fakeLogProvider struct {
	applyCapture
	req interfaces.LogCaptureRequest
}

func (p *fakeLogProvider) CaptureLogs(_ context.Context, req interfaces.LogCaptureRequest, sink interfaces.LogCaptureSink) error {
	p.req = req
	return sink.WriteLogChunk(interfaces.LogChunk{Data: []byte("line one\n"), Source: "historic"})
}

func TestLogsCaptureUsesConfiguredProviderAndWritesOutput(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "app.yaml")
	if err := os.WriteFile(cfg, []byte(`
version: "1"
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: test-token
  - name: web
    type: infra.container_service
    config:
      provider: do
      app_name: bmw-staging
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := &fakeLogProvider{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != "digitalocean" {
			t.Fatalf("providerType = %q, want digitalocean", providerType)
		}
		if cfg["token"] != "test-token" {
			t.Fatalf("token = %v, want test-token", cfg["token"])
		}
		return provider, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	var out bytes.Buffer
	err := runLogsWithOutput([]string{
		"capture",
		"--config", cfg,
		"--resource", "web",
		"--component", "api",
		"--type", "RUN",
		"--tail", "12",
	}, &out)
	if err != nil {
		t.Fatalf("runLogsWithOutput: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "line one\n") {
		t.Fatalf("output = %q, want captured log line", got)
	}
	if provider.req.ResourceName != "bmw-staging" {
		t.Fatalf("ResourceName = %q, want bmw-staging", provider.req.ResourceName)
	}
	if provider.req.ComponentName != "api" {
		t.Fatalf("ComponentName = %q, want api", provider.req.ComponentName)
	}
	if provider.req.LogType != "RUN" {
		t.Fatalf("LogType = %q, want RUN", provider.req.LogType)
	}
	if provider.req.TailLines != 12 {
		t.Fatalf("TailLines = %d, want 12", provider.req.TailLines)
	}
	if provider.req.DurationSeconds != 0 {
		t.Fatalf("DurationSeconds = %d, want 0 when --follow is false", provider.req.DurationSeconds)
	}
}

func TestLogsCaptureFollowSetsDuration(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "app.yaml")
	if err := os.WriteFile(cfg, []byte(`
version: "1"
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
  - name: web
    type: infra.container_service
    config:
      provider: do
      app_name: bmw-staging
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := &fakeLogProvider{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return provider, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	var out bytes.Buffer
	err := runLogsWithOutput([]string{
		"capture",
		"--config", cfg,
		"--resource", "web",
		"--follow",
		"--duration", "5s",
	}, &out)
	if err != nil {
		t.Fatalf("runLogsWithOutput: %v", err)
	}
	if provider.req.DurationSeconds != 5 {
		t.Fatalf("DurationSeconds = %d, want 5 when --follow is true", provider.req.DurationSeconds)
	}
}

func TestLogsCaptureRejectsUnknownType(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "app.yaml")
	if err := os.WriteFile(cfg, []byte(`
version: "1"
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
  - name: web
    type: infra.container_service
    config:
      provider: do
      app_name: bmw-staging
`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runLogsWithOutput([]string{
		"capture",
		"--config", cfg,
		"--resource", "web",
		"--type", "typo",
	}, io.Discard)
	if err == nil {
		t.Fatal("expected unsupported type error")
	}
	if !strings.Contains(err.Error(), "unsupported --type") {
		t.Fatalf("error = %q, want unsupported --type", err.Error())
	}
}

func TestResolveLogCaptureResourcePreservesSecretEnvVars(t *testing.T) {
	t.Setenv("APP_NAME", "bmw-staging")
	t.Setenv("DATABASE_URL", "postgres://secret")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{
			Name: "web",
			Type: "infra.container_service",
			Config: map[string]any{
				"provider": "do",
				"app_name": "${APP_NAME}",
				"env_vars_secret": map[string]any{
					"DATABASE_URL": "${DATABASE_URL}",
				},
			},
		}},
	}

	spec, providerRef, err := resolveLogCaptureResource(cfg, "", "web")
	if err != nil {
		t.Fatalf("resolveLogCaptureResource: %v", err)
	}
	if providerRef != "do" {
		t.Fatalf("providerRef = %q, want do", providerRef)
	}
	if got := spec.Config["app_name"]; got != "bmw-staging" {
		t.Fatalf("app_name = %v, want expanded app name", got)
	}
	secrets, ok := spec.Config["env_vars_secret"].(map[string]any)
	if !ok {
		t.Fatalf("env_vars_secret = %T, want map[string]any", spec.Config["env_vars_secret"])
	}
	if got := secrets["DATABASE_URL"]; got != "${DATABASE_URL}" {
		t.Fatalf("env_vars_secret DATABASE_URL = %v, want literal placeholder", got)
	}
}

func TestResolveLogCaptureResourceUsesEnvResolvedName(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{
			Name: "web",
			Type: "infra.container_service",
			Config: map[string]any{
				"provider": "do",
			},
			Environments: map[string]*config.InfraEnvironmentResolution{
				"staging": {Config: map[string]any{"name": "web-staging"}},
			},
		}},
	}

	spec, _, err := resolveLogCaptureResource(cfg, "staging", "web")
	if err != nil {
		t.Fatalf("resolveLogCaptureResource: %v", err)
	}
	if spec.Name != "web-staging" {
		t.Fatalf("spec.Name = %q, want env-resolved cloud name", spec.Name)
	}
	if got := logCaptureResourceCloudName(spec); got != "web-staging" {
		t.Fatalf("cloud name = %q, want env-resolved cloud name", got)
	}
}
