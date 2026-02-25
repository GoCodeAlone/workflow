package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newAPIGatewayConfig() map[string]any {
	return map[string]any{
		"provider": "mock",
		"name":     "test-api",
		"stage":    "prod",
		"cors": map[string]any{
			"allow_origins": []any{"*"},
			"allow_methods": []any{"GET", "POST"},
		},
		"routes": []any{
			map[string]any{
				"path":       "/api/v1/users",
				"method":     "*",
				"target":     "http://users-svc:8080",
				"rate_limit": 100,
			},
			map[string]any{
				"path":      "/api/v1/orders",
				"method":    "GET",
				"target":    "http://orders-svc:8080",
				"auth_type": "jwt",
			},
		},
	}
}

// TestPlatformAPIGateway_MockLifecycle tests the full plan→apply→status→destroy lifecycle.
func TestPlatformAPIGateway_MockLifecycle(t *testing.T) {
	gw := module.NewPlatformAPIGateway("my-gateway", newAPIGatewayConfig())
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Plan — fresh gateway should propose create.
	plan, err := gw.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change in plan")
	}
	if plan.Name != "test-api" {
		t.Errorf("expected plan.Name=test-api, got %q", plan.Name)
	}
	if plan.Stage != "prod" {
		t.Errorf("expected plan.Stage=prod, got %q", plan.Stage)
	}

	// Apply — should create the gateway in-memory.
	state, err := gw.Apply()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.Endpoint == "" {
		t.Error("expected non-empty endpoint after apply")
	}
	if state.ID == "" {
		t.Error("expected non-empty ID after apply")
	}

	// Status — should show active.
	st, err := gw.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	gwState, ok := st.(*module.PlatformGatewayState)
	if !ok {
		t.Fatalf("Status returned unexpected type %T", st)
	}
	if gwState.Status != "active" {
		t.Errorf("expected status=active, got %q", gwState.Status)
	}

	// Plan after apply — should show no changes needed.
	plan2, err := gw.Plan()
	if err != nil {
		t.Fatalf("second Plan failed: %v", err)
	}
	if len(plan2.Changes) == 0 {
		t.Fatal("expected at least one change entry after apply")
	}

	// Destroy.
	if err := gw.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
	st2, err := gw.Status()
	if err != nil {
		t.Fatalf("Status after destroy failed: %v", err)
	}
	gwState2 := st2.(*module.PlatformGatewayState)
	if gwState2.Status != "deleted" {
		t.Errorf("expected status=deleted after destroy, got %q", gwState2.Status)
	}
	if gwState2.Endpoint != "" {
		t.Errorf("expected empty endpoint after destroy, got %q", gwState2.Endpoint)
	}
}

// TestPlatformAPIGateway_RoutesAndCORS verifies routes and CORS are reflected in the plan.
func TestPlatformAPIGateway_RoutesAndCORS(t *testing.T) {
	gw := module.NewPlatformAPIGateway("cors-gw", newAPIGatewayConfig())
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	plan, err := gw.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(plan.Routes))
	}
	if plan.CORS == nil {
		t.Fatal("expected non-nil CORS config in plan")
	}
	if len(plan.CORS.AllowOrigins) == 0 {
		t.Error("expected at least one allow_origin")
	}
}

// TestPlatformAPIGateway_ApplyIdempotent verifies double-apply is safe.
func TestPlatformAPIGateway_ApplyIdempotent(t *testing.T) {
	gw := module.NewPlatformAPIGateway("idem-gw", newAPIGatewayConfig())
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := gw.Apply(); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	state, err := gw.Apply()
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected active after second apply, got %q", state.Status)
	}
}

// TestPlatformAPIGateway_InvalidProvider verifies Init rejects unknown providers.
func TestPlatformAPIGateway_InvalidProvider(t *testing.T) {
	gw := module.NewPlatformAPIGateway("bad-gw", map[string]any{"provider": "gcp"})
	app := module.NewMockApplication()
	if err := gw.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

// TestPlatformAPIGateway_InvalidAccount verifies Init fails when account is missing.
func TestPlatformAPIGateway_InvalidAccount(t *testing.T) {
	gw := module.NewPlatformAPIGateway("no-acc-gw", map[string]any{
		"provider": "mock",
		"account":  "nonexistent-account",
	})
	app := module.NewMockApplication()
	if err := gw.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// TestPlatformAPIGateway_AWSStubPlan verifies the AWS stub returns a plan.
func TestPlatformAPIGateway_AWSStubPlan(t *testing.T) {
	gw := module.NewPlatformAPIGateway("aws-gw", map[string]any{
		"provider": "aws",
		"name":     "aws-api",
		"stage":    "prod",
	})
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := gw.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change from AWS stub")
	}
}

// TestPlatformAPIGateway_AWSApplyNotImplemented verifies AWS Apply returns an error.
func TestPlatformAPIGateway_AWSApplyNotImplemented(t *testing.T) {
	gw := module.NewPlatformAPIGateway("aws-gw2", map[string]any{"provider": "aws"})
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := gw.Apply()
	if err == nil {
		t.Error("expected error from AWS Apply stub, got nil")
	}
}

// TestPlatformAPIGateway_ServiceRegistration verifies the module registers itself.
func TestPlatformAPIGateway_ServiceRegistration(t *testing.T) {
	gw := module.NewPlatformAPIGateway("reg-gw", map[string]any{"provider": "mock"})
	app := module.NewMockApplication()
	if err := gw.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	svc, ok := app.Services["reg-gw"]
	if !ok {
		t.Fatal("expected reg-gw in service registry")
	}
	if _, ok := svc.(*module.PlatformAPIGateway); !ok {
		t.Fatalf("expected *PlatformAPIGateway in registry, got %T", svc)
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func setupAPIGatewayApp(t *testing.T) (*module.MockApplication, *module.PlatformAPIGateway) {
	t.Helper()
	app := module.NewMockApplication()
	gw := module.NewPlatformAPIGateway("my-gateway", newAPIGatewayConfig())
	if err := gw.Init(app); err != nil {
		t.Fatalf("gateway Init: %v", err)
	}
	return app, gw
}

func TestApigwPlanStep(t *testing.T) {
	app, _ := setupAPIGatewayApp(t)
	factory := module.NewApigwPlanStepFactory()
	step, err := factory("plan", map[string]any{"gateway": "my-gateway"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["gateway"] != "my-gateway" {
		t.Errorf("expected gateway=my-gateway, got %v", result.Output["gateway"])
	}
	if result.Output["changes"] == nil {
		t.Error("expected changes in output")
	}
}

func TestApigwApplyStep(t *testing.T) {
	app, _ := setupAPIGatewayApp(t)
	factory := module.NewApigwApplyStepFactory()
	step, err := factory("apply", map[string]any{"gateway": "my-gateway"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
	if result.Output["endpoint"] == "" {
		t.Error("expected non-empty endpoint")
	}
}

func TestApigwStatusStep(t *testing.T) {
	app, gw := setupAPIGatewayApp(t)
	if _, err := gw.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	factory := module.NewApigwStatusStepFactory()
	step, err := factory("status", map[string]any{"gateway": "my-gateway"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["gateway"] != "my-gateway" {
		t.Errorf("expected gateway=my-gateway, got %v", result.Output["gateway"])
	}
	st := result.Output["status"].(*module.PlatformGatewayState)
	if st.Status != "active" {
		t.Errorf("expected status=active, got %q", st.Status)
	}
}

func TestApigwDestroyStep(t *testing.T) {
	app, gw := setupAPIGatewayApp(t)
	if _, err := gw.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	factory := module.NewApigwDestroyStepFactory()
	step, err := factory("destroy", map[string]any{"gateway": "my-gateway"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["destroyed"] != true {
		t.Errorf("expected destroyed=true, got %v", result.Output["destroyed"])
	}
}

func TestApigwPlanStep_MissingGateway(t *testing.T) {
	factory := module.NewApigwPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing gateway, got nil")
	}
}

func TestApigwPlanStep_GatewayNotFound(t *testing.T) {
	factory := module.NewApigwPlanStepFactory()
	step, err := factory("plan", map[string]any{"gateway": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing gateway service, got nil")
	}
}
