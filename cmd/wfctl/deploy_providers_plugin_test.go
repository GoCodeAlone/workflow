package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// driverCallResult holds the outcome of one fake driver method call.
type driverCallResult struct {
	out *interfaces.ResourceOutput
	err error
}

// hcCallResult holds the outcome of one fake HealthCheck call.
type hcCallResult struct {
	result *interfaces.HealthResult
	err    error
}

type fakeResourceDriver struct {
	updateImage  string
	updateErr    error
	updateOut    *interfaces.ResourceOutput
	updateRef    interfaces.ResourceRef
	updateCalled bool
	// updateResults: if non-empty, each call pops the next entry (last is repeated).
	updateResults []driverCallResult
	updateCallN   int
	hcResult      *interfaces.HealthResult
	hcErr         error
	lastHCRef     interfaces.ResourceRef
	// hcResults: if non-empty, each call uses the next entry (last is repeated).
	hcResults []hcCallResult
	hcCallN   int
	createCalled  bool
	createSpec    interfaces.ResourceSpec
	createOut     *interfaces.ResourceOutput
	createErr     error
	createResults []driverCallResult
	createCallN   int
	readOut       *interfaces.ResourceOutput
	readErr       error
	readCalled    bool
	readResults   []driverCallResult
	readCallN     int
}

func (d *fakeResourceDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCalled = true
	d.createSpec = spec
	callIdx := d.createCallN
	d.createCallN++ // always count
	if len(d.createResults) > 0 {
		idx := callIdx
		if idx >= len(d.createResults) {
			idx = len(d.createResults) - 1
		}
		r := d.createResults[idx]
		return r.out, r.err
	}
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
	callIdx := d.readCallN
	d.readCallN++ // always count
	if len(d.readResults) > 0 {
		idx := callIdx
		if idx >= len(d.readResults) {
			idx = len(d.readResults) - 1
		}
		r := d.readResults[idx]
		return r.out, r.err
	}
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
	callIdx := d.updateCallN
	d.updateCallN++ // always count
	if len(d.updateResults) > 0 {
		idx := callIdx
		if idx >= len(d.updateResults) {
			idx = len(d.updateResults) - 1
		}
		r := d.updateResults[idx]
		return r.out, r.err
	}
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
	callIdx := d.hcCallN
	d.hcCallN++
	if len(d.hcResults) > 0 {
		idx := callIdx
		if idx >= len(d.hcResults) {
			idx = len(d.hcResults) - 1
		}
		r := d.hcResults[idx]
		return r.result, r.err
	}
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
func (f *fakeIaCProvider) SupportedCanonicalKeys() []string { return nil }

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
	zeroHealthPollIntervals(t)
	zeroHealthPollTimeout(t)
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
	// The refactored Deploy no longer joins the update-not-found error into the
	// create failure — the create error is surfaced directly.
	if !strings.Contains(err.Error(), "capacity unavailable") {
		t.Errorf("expected create error 'capacity unavailable' in message, got: %v", err)
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

// ── retry + already-exists ────────────────────────────────────────────────────

// noRetryDelays overrides deployRetryDelays for the duration of t so tests
// don't actually sleep. It resets the var after the test.
func noRetryDelays(t *testing.T) {
	t.Helper()
	orig := deployRetryDelays
	deployRetryDelays = []time.Duration{0, 0, 0, 0}
	t.Cleanup(func() { deployRetryDelays = orig })
}

// makeRetryProvider builds a pluginDeployProvider with the given fakeResourceDriver.
func makeRetryProvider(driver *fakeResourceDriver) *pluginDeployProvider {
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	return &pluginDeployProvider{
		provider:     fake,
		resourceName: "my-app",
		resourceType: "infra.container_service",
		resourceCfg:  map[string]any{},
	}
}

func retryCfg() DeployConfig {
	return DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
}

// TestDeploy_RateLimitRetries: Update returns ErrRateLimited twice then succeeds;
// Deploy must succeed and have called Update exactly 3 times.
func TestDeploy_RateLimitRetries(t *testing.T) {
	noRetryDelays(t)
	driver := &fakeResourceDriver{
		readOut: &interfaces.ResourceOutput{ProviderID: "pid-1"},
		updateResults: []driverCallResult{
			{err: interfaces.ErrRateLimited},
			{err: interfaces.ErrRateLimited},
			{out: &interfaces.ResourceOutput{ProviderID: "pid-1"}},
		},
	}
	p := makeRetryProvider(driver)
	if err := p.Deploy(context.Background(), retryCfg()); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if driver.updateCallN != 3 {
		t.Errorf("expected 3 Update calls, got %d", driver.updateCallN)
	}
	if p.lastProviderID != "pid-1" {
		t.Errorf("lastProviderID: want %q, got %q", "pid-1", p.lastProviderID)
	}
}

// TestDeploy_TransientRetries: Update returns ErrTransient twice then succeeds.
func TestDeploy_TransientRetries(t *testing.T) {
	noRetryDelays(t)
	driver := &fakeResourceDriver{
		readOut: &interfaces.ResourceOutput{ProviderID: "pid-2"},
		updateResults: []driverCallResult{
			{err: interfaces.ErrTransient},
			{err: interfaces.ErrTransient},
			{out: &interfaces.ResourceOutput{ProviderID: "pid-2"}},
		},
	}
	p := makeRetryProvider(driver)
	if err := p.Deploy(context.Background(), retryCfg()); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if driver.updateCallN != 3 {
		t.Errorf("expected 3 Update calls, got %d", driver.updateCallN)
	}
}

// TestDeploy_RetryCeiling: Update always returns ErrRateLimited; Deploy must fail
// after exhausting all retries and return a wrapped "exhausted retries" error.
func TestDeploy_RetryCeiling(t *testing.T) {
	noRetryDelays(t)
	driver := &fakeResourceDriver{
		readOut:    &interfaces.ResourceOutput{ProviderID: "pid-3"},
		updateResults: []driverCallResult{
			{err: interfaces.ErrRateLimited},
		}, // last entry is repeated for all calls
	}
	p := makeRetryProvider(driver)
	err := p.Deploy(context.Background(), retryCfg())
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected 'exhausted' in error, got: %v", err)
	}
	if !errors.Is(err, interfaces.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited wrapped in final error, got: %v", err)
	}
	wantCalls := len(deployRetryDelays)
	if driver.updateCallN != wantCalls {
		t.Errorf("expected %d Update calls (one per retry slot), got %d", wantCalls, driver.updateCallN)
	}
}

// TestDeploy_UnauthorizedFailsFast: Update returns ErrUnauthorized; Deploy must
// fail after a single call and surface an actionable auth message.
func TestDeploy_UnauthorizedFailsFast(t *testing.T) {
	noRetryDelays(t)
	driver := &fakeResourceDriver{
		readOut:    &interfaces.ResourceOutput{ProviderID: "pid-4"},
		updateResults: []driverCallResult{
			{err: interfaces.ErrUnauthorized},
		},
	}
	p := makeRetryProvider(driver)
	err := p.Deploy(context.Background(), retryCfg())
	if err == nil {
		t.Fatal("expected error for ErrUnauthorized")
	}
	if driver.updateCallN != 1 {
		t.Errorf("expected single Update call (no retry), got %d", driver.updateCallN)
	}
	if !errors.Is(err, interfaces.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in error chain, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "token") &&
		!strings.Contains(strings.ToLower(err.Error()), "auth") &&
		!strings.Contains(strings.ToLower(err.Error()), "permission") {
		t.Errorf("expected actionable auth hint in error, got: %v", err)
	}
}

// TestDeploy_AlreadyExistsFallsBackToUpdate: Read returns not-found, Create returns
// ErrResourceAlreadyExists (race condition), Deploy re-reads to get ProviderID, then
// Updates successfully.
func TestDeploy_AlreadyExistsFallsBackToUpdate(t *testing.T) {
	noRetryDelays(t)
	driver := &fakeResourceDriver{
		readResults: []driverCallResult{
			{err: fmt.Errorf("app not found: %w", interfaces.ErrResourceNotFound)},
			{out: &interfaces.ResourceOutput{ProviderID: "race-id"}},
		},
		createResults: []driverCallResult{
			{err: fmt.Errorf("app already exists: %w", interfaces.ErrResourceAlreadyExists)},
		},
		// Update uses fixed success
		updateOut: &interfaces.ResourceOutput{ProviderID: "race-id"},
	}
	p := makeRetryProvider(driver)
	if err := p.Deploy(context.Background(), retryCfg()); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if driver.createCallN != 1 {
		t.Errorf("expected 1 Create call, got %d", driver.createCallN)
	}
	if driver.readCallN != 2 {
		t.Errorf("expected 2 Read calls (initial + post-already-exists), got %d", driver.readCallN)
	}
	if driver.updateCallN != 1 {
		t.Errorf("expected 1 Update call (post-already-exists fallback), got %d", driver.updateCallN)
	}
	if p.lastProviderID != "race-id" {
		t.Errorf("lastProviderID: want %q, got %q", "race-id", p.lastProviderID)
	}
}

// ── HealthCheck polling ───────────────────────────────────────────────────────

// zeroHealthPollIntervals overrides poll intervals to zero so tests don't sleep.
func zeroHealthPollIntervals(t *testing.T) {
	t.Helper()
	origInitial := healthPollInitialInterval
	origBackoff := healthPollBackoffInterval
	origAfter := healthPollBackoffAfter
	healthPollInitialInterval = 0
	healthPollBackoffInterval = 0
	healthPollBackoffAfter = 0
	t.Cleanup(func() {
		healthPollInitialInterval = origInitial
		healthPollBackoffInterval = origBackoff
		healthPollBackoffAfter = origAfter
	})
}

// zeroHealthPollTimeout sets a very short default timeout (1ms) so tests that
// expect timeout failures finish quickly.
func zeroHealthPollTimeout(t *testing.T) {
	t.Helper()
	orig := healthPollDefaultTimeout
	healthPollDefaultTimeout = time.Millisecond
	t.Cleanup(func() { healthPollDefaultTimeout = orig })
}

func makeHealthPollProvider(driver *fakeResourceDriver, providerID string) *pluginDeployProvider {
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	return &pluginDeployProvider{
		provider:       fake,
		resourceName:   "my-app",
		resourceType:   "infra.container_service",
		resourceCfg:    map[string]any{},
		lastProviderID: providerID,
	}
}

func healthCfg() DeployConfig {
	return DeployConfig{
		Env: &config.CIDeployEnvironment{
			HealthCheck: &config.CIHealthCheck{Path: "/healthz"},
		},
	}
}

// TestHealthCheck_HealthyOnFirstCall: HealthCheck returns immediately when
// driver returns Healthy=true on the first poll.
func TestHealthCheck_HealthyOnFirstCall(t *testing.T) {
	zeroHealthPollIntervals(t)
	driver := &fakeResourceDriver{
		hcResult: &interfaces.HealthResult{Healthy: true},
	}
	p := makeHealthPollProvider(driver, "pid-hc-1")
	if err := p.HealthCheck(context.Background(), healthCfg()); err != nil {
		t.Fatalf("HealthCheck: unexpected error: %v", err)
	}
	if driver.hcCallN != 1 {
		t.Errorf("expected 1 HealthCheck call, got %d", driver.hcCallN)
	}
}

// TestHealthCheck_HealthyAfterNPolls: HealthCheck returns success after
// Healthy=false on the first two calls then Healthy=true on the third.
func TestHealthCheck_HealthyAfterNPolls(t *testing.T) {
	zeroHealthPollIntervals(t)
	driver := &fakeResourceDriver{
		hcResults: []hcCallResult{
			{result: &interfaces.HealthResult{Healthy: false, Message: "deploying"}},
			{result: &interfaces.HealthResult{Healthy: false, Message: "deploying"}},
			{result: &interfaces.HealthResult{Healthy: true}},
		},
	}
	p := makeHealthPollProvider(driver, "pid-hc-2")
	if err := p.HealthCheck(context.Background(), healthCfg()); err != nil {
		t.Fatalf("HealthCheck: unexpected error: %v", err)
	}
	if driver.hcCallN != 3 {
		t.Errorf("expected 3 HealthCheck calls, got %d", driver.hcCallN)
	}
}

// TestHealthCheck_TransientContinuesPolling: ErrTransient from driver is logged
// and polling continues; succeeds on the next call.
func TestHealthCheck_TransientContinuesPolling(t *testing.T) {
	zeroHealthPollIntervals(t)
	driver := &fakeResourceDriver{
		hcResults: []hcCallResult{
			{err: interfaces.ErrTransient},
			{result: &interfaces.HealthResult{Healthy: true}},
		},
	}
	p := makeHealthPollProvider(driver, "pid-hc-3")
	if err := p.HealthCheck(context.Background(), healthCfg()); err != nil {
		t.Fatalf("HealthCheck: unexpected error: %v", err)
	}
	if driver.hcCallN != 2 {
		t.Errorf("expected 2 HealthCheck calls (transient + success), got %d", driver.hcCallN)
	}
}

// TestHealthCheck_UnauthorizedFailsFast: ErrUnauthorized from driver causes
// HealthCheck to fail immediately without further polling.
func TestHealthCheck_UnauthorizedFailsFast(t *testing.T) {
	zeroHealthPollIntervals(t)
	driver := &fakeResourceDriver{
		hcResults: []hcCallResult{
			{err: interfaces.ErrUnauthorized},
		},
	}
	p := makeHealthPollProvider(driver, "pid-hc-4")
	err := p.HealthCheck(context.Background(), healthCfg())
	if err == nil {
		t.Fatal("expected error for ErrUnauthorized")
	}
	if !errors.Is(err, interfaces.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in error chain, got: %v", err)
	}
	if driver.hcCallN != 1 {
		t.Errorf("expected 1 HealthCheck call (no retry on auth error), got %d", driver.hcCallN)
	}
}

// TestHealthCheck_TimeoutExceeded: when the context/timeout expires before
// Healthy=true, HealthCheck returns an error containing the last status message.
func TestHealthCheck_TimeoutExceeded(t *testing.T) {
	zeroHealthPollIntervals(t)
	zeroHealthPollTimeout(t)
	driver := &fakeResourceDriver{
		hcResult: &interfaces.HealthResult{Healthy: false, Message: "still deploying"},
	}
	p := makeHealthPollProvider(driver, "pid-hc-5")
	err := p.HealthCheck(context.Background(), healthCfg())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "still deploying") {
		t.Errorf("expected last status in timeout error, got: %v", err)
	}
}
