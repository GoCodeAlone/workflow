package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeResourceDriver struct {
	updateImage  string
	updateErr    error
	updateOut    *interfaces.ResourceOutput
	updateRef    interfaces.ResourceRef
	updateCalled bool
	hcResult     *interfaces.HealthResult
	hcErr        error
	lastHCRef    interfaces.ResourceRef
	createCalled bool
	createSpec   interfaces.ResourceSpec
	createOut    *interfaces.ResourceOutput
	createErr    error
	readOut      *interfaces.ResourceOutput
	readErr      error
	readCalled   bool
}

func (d *fakeResourceDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCalled = true
	d.createSpec = spec
	if d.createErr != nil {
		return nil, d.createErr
	}
	if d.createOut != nil {
		return d.createOut, nil
	}
	return &interfaces.ResourceOutput{}, nil
}
func (d *fakeResourceDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	d.readCalled = true
	if d.readErr != nil {
		return nil, d.readErr
	}
	if d.readOut != nil {
		return d.readOut, nil
	}
	return nil, nil
}
func (d *fakeResourceDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCalled = true
	d.updateRef = ref
	d.updateImage, _ = spec.Config["image"].(string)
	if d.updateErr != nil {
		return nil, d.updateErr
	}
	if d.updateOut != nil {
		return d.updateOut, nil
	}
	return &interfaces.ResourceOutput{}, nil
}
func (d *fakeResourceDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeResourceDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *fakeResourceDriver) HealthCheck(_ context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	d.lastHCRef = ref
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
		p, err := newDeployProvider(name, nil, "")
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
	p, err := newDeployProvider("fake-cloud", cfg, "")
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
	_, err := newDeployProvider("nonexistent-cloud", nil, "")
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
		provider:       fake,
		resourceName:   "my-app",
		resourceType:   "infra.container_service",
		resourceCfg:    map[string]any{},
		lastProviderID: "pre-existing-id",
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
		provider:       fake,
		resourceName:   "my-app",
		resourceType:   "infra.container_service",
		resourceCfg:    map[string]any{},
		lastProviderID: "pre-existing-id",
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

func TestPluginDeployProvider_Deploy_FallsBackToCreateOnNotFound(t *testing.T) {
	driver := &fakeResourceDriver{
		updateErr: interfaces.ErrResourceNotFound,
	}
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
		ImageTag: "registry.example.com/myapp:new123",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if !driver.createCalled {
		t.Error("expected Create to be called after Update returned ErrResourceNotFound")
	}
	if driver.createSpec.Config["image"] != "registry.example.com/myapp:new123" {
		t.Errorf("expected image %q in Create spec, got %v", "registry.example.com/myapp:new123", driver.createSpec.Config["image"])
	}
}

func TestPluginDeployProvider_Deploy_WrappedNotFoundFallsBack(t *testing.T) {
	driver := &fakeResourceDriver{
		updateErr: fmt.Errorf("service layer: %w", interfaces.ErrResourceNotFound),
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:wrapped",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if !driver.createCalled {
		t.Error("expected Create to be called for wrapped ErrResourceNotFound")
	}
}

func TestPluginDeployProvider_Deploy_OtherUpdateErrorNotRetried(t *testing.T) {
	driver := &fakeResourceDriver{
		updateErr: fmt.Errorf("quota exceeded"),
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	err := p.Deploy(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for non-not-found update failure")
	}
	if driver.createCalled {
		t.Error("expected Create NOT to be called for non-not-found update error")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("expected original error in message, got: %v", err)
	}
}

func TestPluginDeployProvider_Deploy_CreateFailureReturnsError(t *testing.T) {
	driver := &fakeResourceDriver{
		updateErr: interfaces.ErrResourceNotFound,
		createErr: fmt.Errorf("capacity unavailable"),
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	err := p.Deploy(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when Create fails")
	}
	if !strings.Contains(err.Error(), "capacity unavailable") {
		t.Errorf("expected create error in message, got: %v", err)
	}
	if !errors.Is(err, interfaces.ErrResourceNotFound) {
		t.Errorf("expected update error (ErrResourceNotFound) also joined into returned error, got: %v", err)
	}
}

// ── ProviderID propagation ────────────────────────────────────────────────────

func TestPluginDeployProvider_HealthCheck_UsesCreatedProviderID(t *testing.T) {
	driver := &fakeResourceDriver{
		updateErr: interfaces.ErrResourceNotFound, // force Create path
		createOut: &interfaces.ResourceOutput{ProviderID: "abc-123"},
		hcResult:  &interfaces.HealthResult{Healthy: true},
	}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	p := &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{"image": "app:v1"},
	}

	if err := p.Deploy(context.Background(), DeployConfig{
		AppName:  "my-app",
		ImageTag: "app:v1",
		Env:      &config.CIDeployEnvironment{},
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	cfg := DeployConfig{
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{Path: "/healthz"},
		},
	}
	if err := p.HealthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if driver.lastHCRef.ProviderID != "abc-123" {
		t.Errorf("HealthCheck ref.ProviderID: want %q, got %q", "abc-123", driver.lastHCRef.ProviderID)
	}
}

func TestPluginDeployProvider_HealthCheck_UsesUpdatedProviderID(t *testing.T) {
	driver := &fakeResourceDriver{
		updateOut: &interfaces.ResourceOutput{ProviderID: "upd-456"},
		hcResult:  &interfaces.HealthResult{Healthy: true},
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

	if err := p.Deploy(context.Background(), DeployConfig{
		AppName:  "my-app",
		ImageTag: "app:v2",
		Env:      &config.CIDeployEnvironment{},
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	cfg := DeployConfig{
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{Path: "/healthz"},
		},
	}
	if err := p.HealthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if driver.lastHCRef.ProviderID != "upd-456" {
		t.Errorf("HealthCheck ref.ProviderID: want %q, got %q", "upd-456", driver.lastHCRef.ProviderID)
	}
}

func TestPluginDeployProvider_HealthCheck_WithoutDeploy(t *testing.T) {
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
	err := p.HealthCheck(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when HealthCheck called without prior Deploy")
	}
	if !strings.Contains(err.Error(), "no ProviderID") {
		t.Errorf("expected 'no ProviderID' in error, got: %v", err)
	}
}

func TestPluginDeployProvider_Deploy_LogsImageAndID(t *testing.T) {
	driver := &fakeResourceDriver{
		updateOut: &interfaces.ResourceOutput{ProviderID: "log-999"},
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

	// Redirect stdout to capture log output.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	deployErr := p.Deploy(context.Background(), DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry/myapp:abc",
		Env:      &config.CIDeployEnvironment{},
	})

	w.Close()
	os.Stdout = oldStdout

	if deployErr != nil {
		t.Fatalf("Deploy: %v", deployErr)
	}

	var buf strings.Builder
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatalf("reading captured stdout: %v", copyErr)
	}
	output := buf.String()

	if !strings.Contains(output, "registry/myapp:abc") {
		t.Errorf("expected image in log output, got: %q", output)
	}
	if !strings.Contains(output, "log-999") {
		t.Errorf("expected ProviderID in log output, got: %q", output)
	}
}

// ── Read-then-upsert ──────────────────────────────────────────────────────────

// TestPluginDeployProvider_Deploy_ReadsExistingBeforeUpdate verifies that when
// Read returns a resource with a non-empty ProviderID, Deploy passes that
// ProviderID to Update and does not call Create.
func TestPluginDeployProvider_Deploy_ReadsExistingBeforeUpdate(t *testing.T) {
	driver := &fakeResourceDriver{
		readOut: &interfaces.ResourceOutput{ProviderID: "abc"},
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if !driver.readCalled {
		t.Error("expected Read to be called before Update")
	}
	if !driver.updateCalled {
		t.Error("expected Update to be called")
	}
	if driver.updateRef.ProviderID != "abc" {
		t.Errorf("expected Update called with ref.ProviderID=%q, got %q", "abc", driver.updateRef.ProviderID)
	}
	if driver.createCalled {
		t.Error("expected Create NOT to be called when Read finds an existing resource")
	}
}

// TestPluginDeployProvider_Deploy_ReadNotFoundCreates verifies that when Read
// returns ErrResourceNotFound, Deploy skips Update and goes straight to Create.
func TestPluginDeployProvider_Deploy_ReadNotFoundCreates(t *testing.T) {
	driver := &fakeResourceDriver{
		readErr: fmt.Errorf("app not found: %w", interfaces.ErrResourceNotFound),
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:new",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), cfg); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if !driver.createCalled {
		t.Error("expected Create to be called when Read returns ErrResourceNotFound")
	}
	if driver.updateCalled {
		t.Error("expected Update NOT to be called when Read returns ErrResourceNotFound")
	}
}

// TestPluginDeployProvider_Deploy_ReadErrorPropagates verifies that when Read
// returns a non-not-found error (e.g. permission denied), Deploy surfaces it
// immediately without calling Update or Create.
func TestPluginDeployProvider_Deploy_ReadErrorPropagates(t *testing.T) {
	driver := &fakeResourceDriver{
		readErr: fmt.Errorf("permission denied"),
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
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	err := p.Deploy(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when Read returns a non-not-found error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected 'permission denied' in error, got: %v", err)
	}
	if driver.updateCalled {
		t.Error("expected Update NOT to be called when Read returns an error")
	}
	if driver.createCalled {
		t.Error("expected Create NOT to be called when Read returns an error")
	}
}
