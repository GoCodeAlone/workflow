package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeResourceDriver struct {
	updateImage string
	hcResult    *interfaces.HealthResult
	hcErr       error
}

func (d *fakeResourceDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeResourceDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeResourceDriver) Update(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateImage, _ = spec.Config["image"].(string)
	return &interfaces.ResourceOutput{}, nil
}
func (d *fakeResourceDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeResourceDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *fakeResourceDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	if d.hcResult != nil {
		return d.hcResult, d.hcErr
	}
	return &interfaces.HealthResult{Healthy: true}, nil
}
func (d *fakeResourceDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeResourceDriver) SensitiveKeys() []string { return nil }

type fakeIaCProvider struct {
	name    string
	drivers map[string]interfaces.ResourceDriver
}

func (f *fakeIaCProvider) Name() string                          { return f.name }
func (f *fakeIaCProvider) Version() string                       { return "0.0.0" }
func (f *fakeIaCProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *fakeIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *fakeIaCProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeIaCProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeIaCProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeIaCProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeIaCProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeIaCProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeIaCProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeIaCProvider) ResourceDriver(rt string) (interfaces.ResourceDriver, error) {
	d, ok := f.drivers[rt]
	if !ok {
		return nil, fmt.Errorf("no driver for %q", rt)
	}
	return d, nil
}
func (f *fakeIaCProvider) Close() error { return nil }

// makePluginTestConfig builds a WorkflowConfig with an iac.provider + infra.container_service.
func makePluginTestConfig(providerName, moduleName string) *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: moduleName,
				Type: "iac.provider",
				Config: map[string]any{
					"provider":    providerName,
					"credentials": "env",
				},
			},
			{
				Name: "my-app",
				Type: "infra.container_service",
				Config: map[string]any{
					"provider":  moduleName,
					"http_port": 8080,
				},
			},
		},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestNewDeployProvider_BuiltIns(t *testing.T) {
	cases := map[string]interface{}{
		"kubernetes":     (*kubernetesProvider)(nil),
		"k8s":            (*kubernetesProvider)(nil),
		"docker":         (*dockerProvider)(nil),
		"docker-compose": (*dockerProvider)(nil),
		"aws-ecs":        (*awsECSProvider)(nil),
	}
	for name := range cases {
		p, err := newDeployProvider(name, nil)
		if err != nil {
			t.Errorf("newDeployProvider(%q): unexpected error: %v", name, err)
			continue
		}
		if p == nil {
			t.Errorf("newDeployProvider(%q): got nil provider", name)
		}
	}
}

func TestNewDeployProvider_PluginProvider_Resolves(t *testing.T) {
	driver := &fakeResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}

	orig := resolveIaCProvider
	defer func() { resolveIaCProvider = orig }()
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}

	cfg := makePluginTestConfig("fake-cloud", "fake-provider")
	p, err := newDeployProvider("fake-cloud", cfg)
	if err != nil {
		t.Fatalf("newDeployProvider: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if _, ok := p.(*pluginDeployProvider); !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", p)
	}
}

func TestNewDeployProvider_UnknownProvider_ErrorsClearly(t *testing.T) {
	_, err := newDeployProvider("nonexistent-cloud", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "kubernetes") {
		t.Errorf("error should mention built-in providers, got: %v", err)
	}
	if !strings.Contains(errStr, "iac.provider") {
		t.Errorf("error should hint at iac.provider module declaration, got: %v", err)
	}
}

func TestPluginDeployProvider_DeployCallsDriverUpdate(t *testing.T) {
	driver := &fakeResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"http_port": 8080},
	}
	cfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:abc123",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if driver.updateImage != "registry.example.com/myapp:abc123" {
		t.Errorf("expected image %q passed to driver.Update, got %q", "registry.example.com/myapp:abc123", driver.updateImage)
	}
}

func TestPluginDeployProvider_HealthCheck(t *testing.T) {
	driver := &fakeResourceDriver{
		hcResult: &interfaces.HealthResult{Healthy: true},
	}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{},
	}
	cfg := DeployConfig{
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{Path: "/healthz"},
		},
	}
	if err := p.HealthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("HealthCheck: unexpected error: %v", err)
	}
}

func TestPluginDeployProvider_HealthCheck_Unhealthy(t *testing.T) {
	driver := &fakeResourceDriver{
		hcResult: &interfaces.HealthResult{Healthy: false, Message: "not ready"},
	}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{},
	}
	cfg := DeployConfig{
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{Path: "/healthz"},
		},
	}
	err := p.HealthCheck(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unhealthy resource")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("expected 'not ready' in error, got: %v", err)
	}
}
