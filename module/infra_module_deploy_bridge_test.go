package module

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── Deploy-capable mock driver ───────────────────────────────────────────────

func newDeployCapableDriver(image string, replicas int) *deployCapableDriver {
	return &deployCapableDriver{
		infraMockDriver: &infraMockDriver{},
		image:           image,
		replicas:        replicas,
	}
}

// deployCapableDriver is a standalone DeployDriver (not interfaces.ResourceDriver)
// used as the provider-supplied driver in optional-interface tests.
type deployCapableDriver struct {
	*infraMockDriver
	image        string
	replicas     int
	healthErr    error
	updateCalled bool

	// bluegreen fields
	greenCreated    bool
	trafficSwitched bool
	blueDestroyed   bool

	// canary fields
	canaryCreated   bool
	routePercent    int
	lastGate        string
	gateErr         error
	promoted        bool
	canaryDestroyed bool
}

// DeployDriver methods.
func (d *deployCapableDriver) Update(_ context.Context, image string) error {
	d.image = image
	d.updateCalled = true
	return nil
}
func (d *deployCapableDriver) HealthCheck(_ context.Context, _ string) error { return d.healthErr }
func (d *deployCapableDriver) CurrentImage(_ context.Context) (string, error) { return d.image, nil }
func (d *deployCapableDriver) ReplicaCount(_ context.Context) (int, error)    { return d.replicas, nil }

// BlueGreenDriver methods.
func (d *deployCapableDriver) CreateGreen(_ context.Context, _ string) error {
	d.greenCreated = true
	return nil
}
func (d *deployCapableDriver) SwitchTraffic(_ context.Context) error {
	d.trafficSwitched = true
	return nil
}
func (d *deployCapableDriver) DestroyBlue(_ context.Context) error {
	d.blueDestroyed = true
	return nil
}
func (d *deployCapableDriver) GreenEndpoint(_ context.Context) (string, error) {
	return "green.example.com", nil
}

// CanaryDriver methods.
func (d *deployCapableDriver) CreateCanary(_ context.Context, _ string) error {
	d.canaryCreated = true
	return nil
}
func (d *deployCapableDriver) RoutePercent(_ context.Context, percent int) error {
	d.routePercent = percent
	return nil
}
func (d *deployCapableDriver) CheckMetricGate(_ context.Context, gate string) error {
	d.lastGate = gate
	return d.gateErr
}
func (d *deployCapableDriver) PromoteCanary(_ context.Context) error {
	d.promoted = true
	return nil
}
func (d *deployCapableDriver) DestroyCanary(_ context.Context) error {
	d.canaryDestroyed = true
	return nil
}

// ─── Provider with optional deploy interfaces ─────────────────────────────────

type deployProviderMock struct {
	*infraMockProvider
	deployDriver    *deployCapableDriver
	bgDriver        *deployCapableDriver
	canaryDriver    *deployCapableDriver
}

func (p *deployProviderMock) ProvideDeployDriver(_ string) DeployDriver {
	if p.deployDriver != nil {
		return p.deployDriver
	}
	return nil
}

func (p *deployProviderMock) ProvideBlueGreenDriver(_ string) BlueGreenDriver {
	if p.bgDriver != nil {
		return p.bgDriver
	}
	return nil
}

func (p *deployProviderMock) ProvideCanaryDriver(_ string) CanaryDriver {
	if p.canaryDriver != nil {
		return p.canaryDriver
	}
	return nil
}

// ─── Tests: infraDeployAdapter (generic fallback) ─────────────────────────────

func TestBridge_FallbackAdapter_RegisteredAtPlainName(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	svc, ok := app.services["my-svc"]
	if !ok {
		t.Fatal("expected 'my-svc' to be registered in SvcRegistry (fallback adapter)")
	}
	if _, ok := svc.(DeployDriver); !ok {
		t.Errorf("registered service is %T, want DeployDriver", svc)
	}
}

func TestBridge_FallbackAdapter_Update(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{
		"provider": "aws",
		"image":    "nginx:1.24",
	})
	app := initWithProvider(t, m, "aws", provider)

	dd := app.services["my-svc"].(DeployDriver)
	if err := dd.Update(context.Background(), "nginx:1.25"); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
}

func TestBridge_FallbackAdapter_HealthCheck_Healthy(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	initWithProvider(t, m, "aws", provider)

	// infraMockDriver.HealthCheck always returns Healthy: true
	adapter := &infraDeployAdapter{im: m}
	if err := adapter.HealthCheck(context.Background(), "/health"); err != nil {
		t.Fatalf("HealthCheck should pass: %v", err)
	}
}

func TestBridge_FallbackAdapter_HealthCheck_Unhealthy(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	// Patch the driver to return unhealthy.
	provider.drivers["infra.container_service"] = &unhealthyMockDriver{}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	initWithProvider(t, m, "aws", provider)

	adapter := &infraDeployAdapter{im: m}
	if err := adapter.HealthCheck(context.Background(), "/health"); err == nil {
		t.Fatal("HealthCheck should fail for unhealthy resource")
	}
}

func TestBridge_FallbackAdapter_CurrentImage(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	// Override Read to return an image in outputs.
	provider.drivers["infra.container_service"] = &imageAwareDriver{image: "nginx:1.24"}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	initWithProvider(t, m, "aws", provider)

	adapter := &infraDeployAdapter{im: m}
	img, err := adapter.CurrentImage(context.Background())
	if err != nil {
		t.Fatalf("CurrentImage: %v", err)
	}
	if img != "nginx:1.24" {
		t.Errorf("CurrentImage = %q, want %q", img, "nginx:1.24")
	}
}

func TestBridge_FallbackAdapter_ReplicaCount(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	provider.drivers["infra.container_service"] = &replicaAwareDriver{desiredCount: 3}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	initWithProvider(t, m, "aws", provider)

	adapter := &infraDeployAdapter{im: m}
	count, err := adapter.ReplicaCount(context.Background())
	if err != nil {
		t.Fatalf("ReplicaCount: %v", err)
	}
	if count != 3 {
		t.Errorf("ReplicaCount = %d, want 3", count)
	}
}

// ─── Tests: DeployDriverProvider optional interface ──────────────────────────

func TestBridge_DeployDriverProvider_UsedOverAdapter(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	dd := newDeployCapableDriver("nginx:1.24", 2)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		deployDriver:      dd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	svc, ok := app.services["my-svc"]
	if !ok {
		t.Fatal("expected 'my-svc' to be registered")
	}
	if svc != DeployDriver(dd) {
		t.Errorf("expected provider-supplied DeployDriver, got %T", svc)
	}
}

// ─── Tests: BlueGreenDriverProvider optional interface ───────────────────────

func TestBridge_BlueGreenDriverProvider_RegisteredAtPlainName(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	bgd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		bgDriver:          bgd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	svc, ok := app.services["my-svc"]
	if !ok {
		t.Fatal("expected 'my-svc' to be registered")
	}
	if _, ok := svc.(BlueGreenDriver); !ok {
		t.Errorf("expected BlueGreenDriver, got %T", svc)
	}
}

func TestBridge_BlueGreenDriverProvider_FullLifecycle(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	bgd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		bgDriver:          bgd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	bgDriver := app.services["my-svc"].(BlueGreenDriver)

	if err := bgDriver.CreateGreen(context.Background(), "nginx:1.25"); err != nil {
		t.Fatalf("CreateGreen: %v", err)
	}
	if err := bgDriver.SwitchTraffic(context.Background()); err != nil {
		t.Fatalf("SwitchTraffic: %v", err)
	}
	if err := bgDriver.DestroyBlue(context.Background()); err != nil {
		t.Fatalf("DestroyBlue: %v", err)
	}
	if !bgd.greenCreated || !bgd.trafficSwitched || !bgd.blueDestroyed {
		t.Error("expected full blue/green lifecycle to be executed")
	}
}

// ─── Tests: CanaryDriverProvider optional interface ──────────────────────────

func TestBridge_CanaryDriverProvider_RegisteredAtPlainName(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	cd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		canaryDriver:      cd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	svc, ok := app.services["my-svc"]
	if !ok {
		t.Fatal("expected 'my-svc' to be registered")
	}
	if _, ok := svc.(CanaryDriver); !ok {
		t.Errorf("expected CanaryDriver, got %T", svc)
	}
}

func TestBridge_CanaryDriverProvider_FullLifecycle(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	cd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		canaryDriver:      cd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	canary := app.services["my-svc"].(CanaryDriver)

	if err := canary.CreateCanary(context.Background(), "nginx:1.25"); err != nil {
		t.Fatalf("CreateCanary: %v", err)
	}
	if err := canary.RoutePercent(context.Background(), 20); err != nil {
		t.Fatalf("RoutePercent: %v", err)
	}
	if err := canary.CheckMetricGate(context.Background(), "error_rate"); err != nil {
		t.Fatalf("CheckMetricGate: %v", err)
	}
	if err := canary.PromoteCanary(context.Background()); err != nil {
		t.Fatalf("PromoteCanary: %v", err)
	}
	if !cd.canaryCreated || !cd.promoted || cd.routePercent != 20 {
		t.Error("expected full canary lifecycle to be executed")
	}
}

// ─── Tests: deploy steps resolve through infra module ─────────────────────────

func TestBridge_DeployStep_ResolvesViaInfraModule(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.container_service")
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	// resolveDeployDriver should find "my-svc" (the adapter).
	driver, err := resolveDeployDriver(app, "my-svc", "test-step")
	if err != nil {
		t.Fatalf("resolveDeployDriver: %v", err)
	}
	if driver == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestBridge_BlueGreenStep_ResolvesViaInfraModule(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	bgd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		bgDriver:          bgd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	driver, err := resolveBlueGreenDriver(app, "my-svc", "test-step")
	if err != nil {
		t.Fatalf("resolveBlueGreenDriver: %v", err)
	}
	if driver == nil {
		t.Fatal("expected non-nil BlueGreenDriver")
	}
}

func TestBridge_CanaryStep_ResolvesViaInfraModule(t *testing.T) {
	baseProvider, _ := newTestProvider("aws", "infra.container_service")
	cd := newDeployCapableDriver("nginx:1.24", 1)
	provider := &deployProviderMock{
		infraMockProvider: baseProvider,
		canaryDriver:      cd,
	}
	m := NewInfraModule("my-svc", "infra.container_service", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	driver, err := resolveCanaryDriver(app, "my-svc", "test-step")
	if err != nil {
		t.Fatalf("resolveCanaryDriver: %v", err)
	}
	if driver == nil {
		t.Fatal("expected non-nil CanaryDriver")
	}
}

// ─── Supporting mock types ────────────────────────────────────────────────────

// unhealthyMockDriver always returns Healthy: false.
type unhealthyMockDriver struct{ infraMockDriver }

func (d *unhealthyMockDriver) HealthCheck(_ context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: false, Message: "simulated failure"}, nil
}

// imageAwareDriver returns an image in outputs.
type imageAwareDriver struct {
	infraMockDriver
	image string
}

func (d *imageAwareDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:    ref.Name,
		Type:    ref.Type,
		Status:  "running",
		Outputs: map[string]any{"image": d.image},
	}, nil
}

// replicaAwareDriver returns desired_count in outputs.
type replicaAwareDriver struct {
	infraMockDriver
	desiredCount int32
}

func (d *replicaAwareDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:    ref.Name,
		Type:    ref.Type,
		Status:  "running",
		Outputs: map[string]any{"desired_count": d.desiredCount},
	}, nil
}
