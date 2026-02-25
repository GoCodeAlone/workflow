package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newTestMultiRegionConfig() map[string]any {
	return map[string]any{
		"provider": "mock",
		"regions": []any{
			map[string]any{
				"name":     "us-east-1",
				"provider": "aws",
				"endpoint": "https://us-east-1.example.com",
				"priority": "primary",
				"health_check": map[string]any{
					"interval":  float64(30),
					"timeout":   float64(5),
					"path":      "/health",
					"threshold": float64(3),
				},
			},
			map[string]any{
				"name":     "us-west-2",
				"provider": "aws",
				"endpoint": "https://us-west-2.example.com",
				"priority": "secondary",
				"health_check": map[string]any{
					"interval":  float64(30),
					"timeout":   float64(5),
					"path":      "/health",
					"threshold": float64(3),
				},
			},
			map[string]any{
				"name":     "eu-west-1",
				"provider": "aws",
				"endpoint": "https://eu-west-1.example.com",
				"priority": "dr",
				"health_check": map[string]any{
					"interval":  float64(60),
					"timeout":   float64(10),
					"path":      "/health",
					"threshold": float64(5),
				},
			},
		},
	}
}

func setupMultiRegionApp(t *testing.T) (*module.MockApplication, *module.MultiRegionModule) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewMultiRegionModule("prod-regions", newTestMultiRegionConfig())
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module tests ─────────────────────────────────────────────────────────────

func TestMultiRegionModule_Init(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewMultiRegionModule("prod-regions", newTestMultiRegionConfig())
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if m.Name() != "prod-regions" {
		t.Errorf("expected name=prod-regions, got %q", m.Name())
	}
	if _, ok := app.Services["prod-regions"]; !ok {
		t.Error("expected prod-regions in service registry")
	}
}

func TestMultiRegionModule_Init_NoRegions(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewMultiRegionModule("empty", map[string]any{"provider": "mock"})
	if err := m.Init(app); err == nil {
		t.Error("expected error for no regions, got nil")
	}
}

func TestMultiRegionModule_Init_NoPrimaryRegion(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewMultiRegionModule("no-primary", map[string]any{
		"provider": "mock",
		"regions": []any{
			map[string]any{"name": "us-east-1", "priority": "secondary"},
		},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for missing primary region, got nil")
	}
}

func TestMultiRegionModule_Init_InvalidProvider(t *testing.T) {
	app := module.NewMockApplication()
	cfg := newTestMultiRegionConfig()
	cfg["provider"] = "unknown-cloud"
	m := module.NewMultiRegionModule("bad", cfg)
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestMultiRegionModule_Deploy(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	if err := m.Deploy("us-east-1"); err != nil {
		t.Fatalf("Deploy us-east-1: %v", err)
	}
	if err := m.Deploy("us-west-2"); err != nil {
		t.Fatalf("Deploy us-west-2: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "active" {
		t.Errorf("expected status=active, got %q", st.Status)
	}
}

func TestMultiRegionModule_Deploy_UnknownRegion(t *testing.T) {
	_, m := setupMultiRegionApp(t)
	if err := m.Deploy("ap-southeast-1"); err == nil {
		t.Error("expected error for unknown region, got nil")
	}
}

func TestMultiRegionModule_Failover_Lifecycle(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	// Deploy to both regions first
	if err := m.Deploy("us-east-1"); err != nil {
		t.Fatalf("Deploy primary: %v", err)
	}
	if err := m.Deploy("us-west-2"); err != nil {
		t.Fatalf("Deploy secondary: %v", err)
	}

	// Trigger failover from primary to secondary
	if err := m.Failover("us-east-1", "us-west-2"); err != nil {
		t.Fatalf("Failover: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status after failover: %v", err)
	}
	if st.ActiveRegion != "us-west-2" {
		t.Errorf("expected activeRegion=us-west-2, got %q", st.ActiveRegion)
	}
	if st.Status != "active" {
		t.Errorf("expected status=active after failover completion, got %q", st.Status)
	}

	// Verify source region is marked failed
	var sourceHealth *module.RegionHealth
	for i := range st.Regions {
		if st.Regions[i].Name == "us-east-1" {
			sourceHealth = &st.Regions[i]
			break
		}
	}
	if sourceHealth == nil {
		t.Fatal("us-east-1 not found in regions")
	}
	if sourceHealth.Status != "failed" {
		t.Errorf("expected us-east-1 status=failed, got %q", sourceHealth.Status)
	}
}

func TestMultiRegionModule_Failover_InvalidRegion(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	if err := m.Failover("nonexistent", "us-west-2"); err == nil {
		t.Error("expected error for unknown source region, got nil")
	}
	if err := m.Failover("us-east-1", "nonexistent"); err == nil {
		t.Error("expected error for unknown target region, got nil")
	}
}

func TestMultiRegionModule_Promote(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	if err := m.Promote("us-west-2"); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.PrimaryRegion != "us-west-2" {
		t.Errorf("expected primaryRegion=us-west-2, got %q", st.PrimaryRegion)
	}
	if st.ActiveRegion != "us-west-2" {
		t.Errorf("expected activeRegion=us-west-2, got %q", st.ActiveRegion)
	}
}

func TestMultiRegionModule_TrafficWeightAdjustment(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	if err := m.SetWeight("us-east-1", 80); err != nil {
		t.Fatalf("SetWeight us-east-1=80: %v", err)
	}
	if err := m.SetWeight("us-west-2", 20); err != nil {
		t.Fatalf("SetWeight us-west-2=20: %v", err)
	}

	weights := m.Weights()
	if weights["us-east-1"] != 80 {
		t.Errorf("expected us-east-1 weight=80, got %d", weights["us-east-1"])
	}
	if weights["us-west-2"] != 20 {
		t.Errorf("expected us-west-2 weight=20, got %d", weights["us-west-2"])
	}
}

func TestMultiRegionModule_SetWeight_OutOfRange(t *testing.T) {
	_, m := setupMultiRegionApp(t)
	if err := m.SetWeight("us-east-1", 150); err == nil {
		t.Error("expected error for weight > 100, got nil")
	}
	if err := m.SetWeight("us-east-1", -1); err == nil {
		t.Error("expected error for weight < 0, got nil")
	}
}

func TestMultiRegionModule_CheckHealth(t *testing.T) {
	_, m := setupMultiRegionApp(t)

	healths, err := m.CheckHealth()
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if len(healths) != 3 {
		t.Errorf("expected 3 region healths, got %d", len(healths))
	}
	for _, h := range healths {
		if h.Status != "healthy" {
			t.Errorf("expected %q to be healthy, got %q", h.Name, h.Status)
		}
	}
}

func TestMultiRegionModule_Sync(t *testing.T) {
	_, m := setupMultiRegionApp(t)
	if err := m.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestMultiRegionModule_InitialWeights(t *testing.T) {
	_, m := setupMultiRegionApp(t)
	weights := m.Weights()
	if len(weights) != 3 {
		t.Fatalf("expected 3 weight entries, got %d", len(weights))
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	if total != 100 {
		t.Errorf("expected total weight=100, got %d", total)
	}
}

// ─── region router tests ──────────────────────────────────────────────────────

func TestMultiRegionRoutingModule_LatencyRoute(t *testing.T) {
	router := module.NewMultiRegionRoutingModule("router", map[string]any{"mode": "latency"})
	router.SetRegions([]module.RegionDeployConfig{
		{Name: "us-east-1", Priority: "primary", Endpoint: "https://us-east-1.example.com"},
		{Name: "us-west-2", Priority: "secondary", Endpoint: "https://us-west-2.example.com"},
	})

	// Set different weights
	router.Failover("us-west-2", "us-east-1") // reset weights
	_ = router

	r2 := module.NewMultiRegionRoutingModule("router2", map[string]any{"mode": "latency"})
	regions := []module.RegionDeployConfig{
		{Name: "us-east-1", Priority: "primary"},
		{Name: "eu-west-1", Priority: "secondary"},
	}
	r2.SetRegions(regions)

	chosen, err := r2.RouteRequest(context.Background())
	if err != nil {
		t.Fatalf("RouteRequest: %v", err)
	}
	if chosen.Name == "" {
		t.Error("expected a region to be chosen")
	}
}

func TestMultiRegionRoutingModule_GeoRoute(t *testing.T) {
	router := module.NewMultiRegionRoutingModule("geo-router", map[string]any{"mode": "geo"})
	router.SetRegions([]module.RegionDeployConfig{
		{Name: "eu-west-1", Priority: "dr"},
		{Name: "us-east-1", Priority: "primary"},
		{Name: "us-west-2", Priority: "secondary"},
	})

	chosen, err := router.RouteRequest(context.Background())
	if err != nil {
		t.Fatalf("RouteRequest: %v", err)
	}
	// Geo routing should prefer primary
	if chosen.Priority != "primary" {
		t.Errorf("expected primary region, got priority=%q (%q)", chosen.Priority, chosen.Name)
	}
}

func TestMultiRegionRoutingModule_Failover_StateMachine(t *testing.T) {
	router := module.NewMultiRegionRoutingModule("router", map[string]any{"mode": "latency"})
	router.SetRegions([]module.RegionDeployConfig{
		{Name: "us-east-1", Priority: "primary"},
		{Name: "us-west-2", Priority: "secondary"},
	})

	// Initial state: both healthy
	s, _ := router.State("us-east-1")
	if s != module.RegionStateHealthy {
		t.Errorf("expected healthy, got %v", s)
	}

	// Trigger failover
	if err := router.Failover("us-east-1", "us-west-2"); err != nil {
		t.Fatalf("Failover: %v", err)
	}

	// Source should be failed
	s, _ = router.State("us-east-1")
	if s != module.RegionStateFailed {
		t.Errorf("expected failed for source, got %v", s)
	}

	// Target should be healthy (recovered)
	s, _ = router.State("us-west-2")
	if s != module.RegionStateHealthy {
		t.Errorf("expected healthy for target after recovery, got %v", s)
	}
}

func TestMultiRegionRoutingModule_Weights(t *testing.T) {
	router := module.NewMultiRegionRoutingModule("router", map[string]any{})
	router.SetRegions([]module.RegionDeployConfig{
		{Name: "us-east-1", Priority: "primary"},
		{Name: "us-west-2", Priority: "secondary"},
	})

	weights := router.Weights()
	if len(weights) != 2 {
		t.Fatalf("expected 2 weight entries, got %d", len(weights))
	}
}

func TestMultiRegionRoutingModule_NoHealthyRegion(t *testing.T) {
	router := module.NewMultiRegionRoutingModule("router", map[string]any{"mode": "latency"})
	router.SetRegions([]module.RegionDeployConfig{
		{Name: "us-east-1", Priority: "primary"},
	})
	// Mark only region as failed
	if err := router.SetState("us-east-1", module.RegionStateFailed); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	_, err := router.RouteRequest(context.Background())
	if err == nil {
		t.Error("expected error when no healthy region available, got nil")
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func TestRegionDeployStep(t *testing.T) {
	app, _ := setupMultiRegionApp(t)
	factory := module.NewRegionDeployStepFactory()
	step, err := factory("deploy", map[string]any{
		"module": "prod-regions",
		"region": "us-east-1",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["region"] != "us-east-1" {
		t.Errorf("expected region=us-east-1, got %v", result.Output["region"])
	}
}

func TestRegionDeployStep_MissingModule(t *testing.T) {
	factory := module.NewRegionDeployStepFactory()
	_, err := factory("deploy", map[string]any{"region": "us-east-1"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing module, got nil")
	}
}

func TestRegionDeployStep_MissingRegion(t *testing.T) {
	factory := module.NewRegionDeployStepFactory()
	_, err := factory("deploy", map[string]any{"module": "prod-regions"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing region, got nil")
	}
}

func TestRegionFailoverStep(t *testing.T) {
	app, m := setupMultiRegionApp(t)
	// Deploy to both regions first
	_ = m.Deploy("us-east-1")
	_ = m.Deploy("us-west-2")

	factory := module.NewRegionFailoverStepFactory()
	step, err := factory("failover", map[string]any{
		"module": "prod-regions",
		"from":   "us-east-1",
		"to":     "us-west-2",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["activeRegion"] != "us-west-2" {
		t.Errorf("expected activeRegion=us-west-2, got %v", result.Output["activeRegion"])
	}
}

func TestRegionFailoverStep_MissingFields(t *testing.T) {
	factory := module.NewRegionFailoverStepFactory()
	if _, err := factory("f", map[string]any{"from": "a", "to": "b"}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing module")
	}
	if _, err := factory("f", map[string]any{"module": "m", "to": "b"}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing from")
	}
	if _, err := factory("f", map[string]any{"module": "m", "from": "a"}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing to")
	}
}

func TestRegionPromoteStep(t *testing.T) {
	app, _ := setupMultiRegionApp(t)
	factory := module.NewRegionPromoteStepFactory()
	step, err := factory("promote", map[string]any{
		"module": "prod-regions",
		"region": "us-west-2",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["promoted"] != "us-west-2" {
		t.Errorf("expected promoted=us-west-2, got %v", result.Output["promoted"])
	}
}

func TestRegionStatusStep(t *testing.T) {
	app, _ := setupMultiRegionApp(t)
	factory := module.NewRegionStatusStepFactory()
	step, err := factory("status", map[string]any{"module": "prod-regions"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["regions"] == nil {
		t.Error("expected regions in output")
	}
	if result.Output["primaryRegion"] != "us-east-1" {
		t.Errorf("expected primaryRegion=us-east-1, got %v", result.Output["primaryRegion"])
	}
}

func TestRegionWeightStep(t *testing.T) {
	app, _ := setupMultiRegionApp(t)
	factory := module.NewRegionWeightStepFactory()
	step, err := factory("weight", map[string]any{
		"module": "prod-regions",
		"region": "us-east-1",
		"weight": float64(75),
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["weight"] != 75 {
		t.Errorf("expected weight=75, got %v", result.Output["weight"])
	}
}

func TestRegionWeightStep_MissingFields(t *testing.T) {
	factory := module.NewRegionWeightStepFactory()
	if _, err := factory("w", map[string]any{"region": "r", "weight": 50}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing module")
	}
	if _, err := factory("w", map[string]any{"module": "m", "weight": 50}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing region")
	}
	if _, err := factory("w", map[string]any{"module": "m", "region": "r"}, module.NewMockApplication()); err == nil {
		t.Error("expected error for missing weight")
	}
}

func TestRegionSyncStep(t *testing.T) {
	app, _ := setupMultiRegionApp(t)
	factory := module.NewRegionSyncStepFactory()
	step, err := factory("sync", map[string]any{"module": "prod-regions"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["synced"] != true {
		t.Errorf("expected synced=true, got %v", result.Output["synced"])
	}
}

func TestRegionStepModuleNotFound(t *testing.T) {
	app := module.NewMockApplication()

	factories := []struct {
		name    string
		factory module.StepFactory
		cfg     map[string]any
	}{
		{"deploy", module.NewRegionDeployStepFactory(), map[string]any{"module": "ghost", "region": "us-east-1"}},
		{"failover", module.NewRegionFailoverStepFactory(), map[string]any{"module": "ghost", "from": "a", "to": "b"}},
		{"promote", module.NewRegionPromoteStepFactory(), map[string]any{"module": "ghost", "region": "r"}},
		{"status", module.NewRegionStatusStepFactory(), map[string]any{"module": "ghost"}},
		{"sync", module.NewRegionSyncStepFactory(), map[string]any{"module": "ghost"}},
	}

	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			step, err := f.factory(f.name, f.cfg, app)
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
			if err == nil {
				t.Error("expected error for missing module service, got nil")
			}
		})
	}
}
